package protocol

import (
	"encoding/json"
	"slices"

	"formal-gates/internal/engine/coverage"
)

// Offers describe current obligations; decisions own the selected remedy and reason.
func (s *State) validationOffer(r ValidationRequest) (ValidationRequest, []AskOption) {
	options := []AskOption{{ID: "approve", Label: "Approve one bound action"}}
	if s.Validation != nil {
		r.Binding.RequirementRevision = s.Validation.RequirementRevision
		r.Binding.Generation = s.Validation.Generation
		r.Binding.RouteScope = s.validationRouteDigest()
		if set := s.Validation.Sets[r.Mode]; set != nil {
			r.Binding.Task, r.Binding.CaseVersion = set.DesignTask, set.Version
			r.Binding.Candidate = s.Validation.DevelopmentSnapshot
		}
	}
	switch r.Kind {
	case KindDispatchBudgetDisposition:
		return r, dispatchBudgetOptions()
	case "QA_REVIEW":
		options[0].Label = "Fresh review of this case-set"
		options = append(options, AskOption{ID: "redesign", Label: "Redesign and review once"})
		if r.Binding.Candidate != "" {
			options = append(options, AskOption{ID: "waive", Label: "Waive this case-set on the bound fixed candidate inputs"})
		}
		r.Remedies = []string{"FRESH_REVIEW", "REDESIGN", "WAIVE", "REQUEST_REQUIREMENT_CHANGE", "ABORT"}
	case "REPAIR":
		options[0].Label = "One additional repair"
		r.Remedies = []string{"REPAIR"}
		if work := s.nextRepairWork(); work != nil {
			r.Obligations = s.validationObligations(work.wave)
			r.Binding.WaveID = work.wave.ID
			r.Binding.Task = work.task
			r.Binding.Candidate = work.wave.Candidate.Identity
			if work.task != work.wave.Repair.Task {
				r.Binding.Candidate = s.Validation.DevelopmentSnapshot
			}
			if targets, err := s.retryableValidationQA(work.wave, s.currentValidationWave()); err == nil && len(targets) > 0 {
				options = append(options, AskOption{ID: "retry_qa", Label: "Retry selected QA on this candidate"})
				r.Remedies = append(r.Remedies, "RETRY_QA")
			}
			if (work.wave.Status == "REPAIR" || work.wave.Status == "ADJUDICATING") && len(r.Obligations) > 0 {
				options = append(options, AskOption{ID: "waive", Label: "Waive selected exact obligations"})
				r.Remedies = append(r.Remedies, "WAIVE")
			}
		}
		r.Remedies = append(r.Remedies, "REQUEST_REQUIREMENT_CHANGE", "ABORT")
	case "QA_RETRY":
		w := s.currentValidationWave()
		if w == nil || r.Target != w.ID || r.Binding.WaveID != w.ID {
			options = nil
		} else if targets, err := s.retryableValidationQA(w, w); err != nil || len(targets) == 0 {
			options = nil
		}
	case "DISPOSE":
		if w := s.currentValidationWave(); w != nil {
			r.Binding.WaveID = w.ID
			r.Binding.Candidate = w.Candidate.Identity
			r.Obligations = []string{r.Target}
		}
	case "RETRY":
		options[0].Label = "One additional execution attempt"
		if intent, ok := s.PendingHostActions[r.Target]; ok && intent.Adapter != nil && nativeOperation(intent.Adapter.Operation) {
			if s.candidateRetryGrantTarget(intent.Correlation) != "" {
				options = nil
			}
			r.Remedies = []string{"RETRY", "REQUEST_REQUIREMENT_CHANGE", "ABORT"}
			break
		}
		for key, a := range s.EndedAttempts {
			if key.String() == r.Target {
				if a.Work != nil {
					r.Binding = *a.Work
				}
				if s.retryExtensionGranted(key.String()) {
					options = nil
				}
				if a.Work != nil && a.Work.WaveID != "" {
					options = append(options, AskOption{ID: "waive", Label: "Skip this unavailable validation target"})
					r.Obligations = []string{key.String()}
				}
			}
		}
		if r.Mode != "" && r.Binding.WaveID == "" {
			if set := s.Validation.Sets[r.Mode]; set != nil {
				r.Binding.Task = set.DesignTask
				r.Binding.CaseVersion = set.Version
				r.Binding.Candidate = s.Validation.DevelopmentSnapshot
				options = append(options, AskOption{ID: "redesign", Label: "Redesign this case-set and review once"})
				if r.Binding.Candidate != "" {
					options = append(options, AskOption{ID: "waive", Label: "Waive this case-set on fixed candidate inputs"})
				}
			}
		}
		r.Remedies = []string{"RETRY", "WAIVE", "REQUEST_REQUIREMENT_CHANGE", "ABORT"}
	}
	if r.Kind == "QA_REVIEW" || r.Kind == "REPAIR" || r.Kind == "RETRY" {
		options = append(options, AskOption{ID: "requirement", Label: "Revise the requirement"}, AskOption{ID: "abort", Label: "Abort this run"})
	}
	options = append(options, AskOption{ID: "decline", Label: "Keep the obligation blocked"})
	if s.abortRequested() && s.cleanupRetryRequest(&r) {
		options = slices.DeleteFunc(options, func(o AskOption) bool { return o.ID != "approve" && o.ID != "decline" })
		r.Remedies = []string{"RETRY"}
	}
	return r, options
}

func (s *State) validationObligations(w *ValidationWave) []string {
	var ids []string
	for _, obligation := range s.validationObligationDetails(w) {
		if !slices.Contains(ids, obligation.id) {
			ids = append(ids, obligation.id)
		}
	}
	slices.Sort(ids)
	return ids
}

func mandatoryValidationFinding(target *ValidationTarget, finding *ValidationFinding, a FindingResolution) bool {
	return a.Decision != "DISMISSED" && (finding.ExecutionFailure != nil || a.Severity == "P0" || a.Severity == "P1" || target.Kind == "GATE" && a.Severity == "P2")
}

func (s *State) validationObligationWaived(w *ValidationWave, target *ValidationTarget, id string) bool {
	for request, decision := range s.Decisions {
		ask := s.PendingAsks[request]
		if ask.Validation == nil || decision.Validation == nil || (decision.Choice != "waive" && decision.Validation.Remedy != "DISMISS") {
			continue
		}
		selected := decision.Validation.Obligations
		if len(selected) == 0 && decision.Validation.Remedy == "DISMISS" {
			selected = ask.Validation.Obligations
		}
		if !slices.Contains(selected, id) {
			continue
		}
		source := s.validationWave(ask.Validation.Binding.WaveID)
		if source == nil || source.Candidate != w.Candidate {
			continue
		}
		for _, original := range source.Targets {
			if original.Kind != target.Kind || original.Mode != target.Mode || original.Gate != target.Gate || original.ResultAction == "" || original.ResultAction != target.ResultAction {
				continue
			}
			if target.Mode != "" && !sameWaveCases(source, w, target.Mode) {
				continue
			}
			if source.Generation != w.Generation && !s.validationCarryAuthorized(original.ResultAction) {
				continue
			}
			if original.Task.String() == id {
				return true
			}
			for _, finding := range s.Validation.Findings {
				if s.validationFindingResponsibility(source, finding) == original && len(finding.History) > 0 && finding.History[len(finding.History)-1].ProblemID == id {
					return true
				}
			}
		}
	}
	return false
}

func (s *State) unresolvedQA(w *ValidationWave) bool {
	for _, obligation := range s.validationObligationDetails(w) {
		if obligation.target.Kind == "QA" {
			return true
		}
	}
	return false
}

// A case-set waiver never approves a review. It applies only to the accepted
// design and fixed development source described in its original request.
func (s *State) caseSetWaiver(mode coverage.ReviewKind) string {
	if s.Validation == nil || s.Validation.DevelopmentSnapshot == "" {
		return ""
	}
	set := s.Validation.Sets[mode]
	if set == nil {
		return ""
	}
	for id, d := range s.Decisions {
		r := s.PendingAsks[id].Validation
		if r == nil || (r.Kind != "QA_REVIEW" && !(r.Kind == "RETRY" && r.Binding.WaveID == "")) || r.Mode != mode || d.Choice != "waive" || d.Validation == nil || r.Binding.Candidate != s.Validation.DevelopmentSnapshot || r.Binding.Task != set.DesignTask {
			continue
		}
		originalSet := requestCaseSet(r)
		if originalSet == nil {
			continue
		}
		if len(set.AffectedCases) > 0 && !slices.Equal(set.AffectedCases, originalSet.AffectedCases) {
			continue
		}
		invalidated := false
		for otherID, later := range s.Decisions {
			request := s.PendingAsks[otherID].Validation
			if later.Revision > d.Revision && request != nil && request.Mode == mode && later.Choice == "approve" && (request.Kind == "THAW" || request.Kind == "QA_ARTIFACT_REPAIR" || request.Kind == "VALIDATION_DETAIL_DISPOSITION" && later.Validation != nil && later.Validation.Remedy == "QA_ARTIFACT_REPAIR") {
				invalidated = true
			}
		}
		if invalidated {
			continue
		}
		current, original := set.Manifest, originalSet.Manifest
		current.Binding, original.Binding = coverage.ManifestBinding{}, coverage.ManifestBinding{}
		if digestOfCanonical(current) != digestOfCanonical(original) {
			continue
		}
		if r.Binding.Generation != s.Validation.Generation {
			carried := false
			for action, result := range s.Results {
				if result.Validation != nil && result.Validation.Binding.Task == r.Binding.Task && s.validationCarryAuthorized(action) {
					carried = true
				}
			}
			if !carried {
				continue
			}
		}
		return id
	}
	return ""
}

func (s *State) caseSetReady(mode coverage.ReviewKind) bool {
	set := s.Validation.Sets[mode]
	return set != nil && (set.Status == "FROZEN" || s.caseSetWaiver(mode) != "")
}

func (e *Engine) waiveValidation(s *State, ask PendingAsk, d *ValidationDecision, w *ValidationWave) error {
	r := ask.Validation
	if r.Kind == "QA_REVIEW" || r.Kind == "RETRY" && r.Mode != "" && r.Binding.WaveID == "" {
		set := s.Validation.Sets[r.Mode]
		if set == nil || r.Kind == "QA_REVIEW" && r.Target != s.qaReviewTarget(r.Mode) || r.Binding.Candidate == "" || r.Binding.Candidate != s.Validation.DevelopmentSnapshot || r.Binding.Task != set.DesignTask {
			return validationError("case-set waiver requires the current design and fixed development candidate")
		}
		if len(d.Obligations) != 0 {
			return validationError("case-set waiver binds the complete offered case-set")
		}
		if len(set.Paths) > 0 {
			if len(set.ReviewHistory) == 0 {
				return validationError("case-set artifact must be materialized before waiver")
			}
			set.Candidate = set.ReviewHistory[len(set.ReviewHistory)-1].Candidate
			if set.Candidate == "" {
				return validationError("case-set artifact has no fixed candidate")
			}
		}
		return nil
	}
	if w == nil || w.ID != r.Binding.WaveID || len(d.Obligations) == 0 {
		return validationError("waiver requires exact current obligations")
	}
	seen := map[string]bool{}
	for _, id := range d.Obligations {
		if seen[id] || !slices.Contains(r.Obligations, id) {
			return validationError("waiver obligation is not in the bound offer")
		}
		seen[id] = true
	}
	if r.Kind == "RETRY" || r.Kind == "DISPOSE" {
		for _, id := range d.Obligations {
			target := w.Targets[id]
			if target == nil || target.Status != "RUNTIME_ERROR" && target.Status != "RETRY_EXHAUSTED" {
				return validationError("skip requires an unavailable validation target")
			}
			target.Status = "AUTHORIZED_SKIP"
			if r.Kind == "DISPOSE" {
				target.Status = "DISPOSED"
			}
			target.DispositionReason = d.Reason
			for action, binding := range s.DispatchBindings {
				if binding.Task == target.Task && s.liveDispatch(action) {
					s.retireAction(action, "", "user authorized skip: "+d.Reason)
				}
			}
			delete(s.EndedAttempts, target.Task)
		}
	} else if w.Status == "REPAIR" || w.Status == "ADJUDICATING" {
		for _, id := range d.Obligations {
			if target := w.Targets[id]; target != nil {
				target.Status = "AUTHORIZED_SKIP"
				if d.Remedy == "DISMISS" {
					target.Status = "DISPOSED"
				}
				target.DispositionReason = d.Reason
			}
		}
	} else {
		return validationError("semantic waiver requires collected and adjudicated findings")
	}
	return nil
}

// Rerunning QA creates a new semantic obligation on the same snapshot. The
// original request identifies it; attempts and repairs keep their own identities.
func (e *Engine) retryValidationQA(s *State, ask PendingAsk, d *ValidationDecision, source *ValidationWave) error {
	if source == nil || source.ID != ask.Validation.Target || len(d.Obligations) == 0 {
		return validationError("QA retry needs selected targets on the current wave")
	}
	projection := s.currentValidationWave()
	eligible, err := s.retryableValidationQA(source, projection)
	if err != nil {
		return err
	}
	selected := map[string]bool{}
	for _, id := range d.Obligations {
		target := source.Targets[id]
		if selected[id] || target == nil || !slices.Contains(eligible, target) {
			return validationError("QA retry target must be a selected approved QA obligation")
		}
		if _, _, err := s.executionContract(target.Mode, source); err != nil {
			return err
		}
		selected[id] = true
		for key, current := range projection.Targets {
			if current.Kind == target.Kind && current.Mode == target.Mode {
				selected[key] = true
			}
		}
	}
	for action, binding := range s.DispatchBindings {
		if selected[binding.Task.String()] && s.liveDispatch(action) {
			s.retireAction(action, "", "user requested fresh QA on the same snapshot")
		}
	}
	for _, wave := range []*ValidationWave{source, projection} {
		for key, target := range wave.Targets {
			if selected[key] {
				delete(s.EndedAttempts, target.Task)
			}
		}
	}
	projectionID := projection.ID
	if !e.freezeValidationWave(s, projection.Scope, ask.RequestID, projection.SourceTasks) {
		return validationError("confirmed coverage relations are incomplete")
	}
	current := s.currentValidationWave()
	// Appending can relocate the wave slice; source identity stays explicit.
	projection = s.validationWave(projectionID)
	for key, target := range current.Targets {
		for oldKey, previous := range projection.Targets {
			if target.Kind != previous.Kind || target.Mode != previous.Mode || target.Gate != previous.Gate {
				continue
			}
			if selected[oldKey] {
				break
			}
			delete(current.Targets, key)
			copy := *previous
			current.Targets[copy.Task.String()] = &copy
			break
		}
	}
	current.ExpectedDigest = digestOfCanonical(struct {
		Candidate coverage.ValidationCandidate
		Versions  map[coverage.ReviewKind]string
		Route     string
		Targets   []string
		Scope     coverage.ScopeMode
	}{current.Candidate, current.Versions, current.RouteDigest, sortedTargetKeys(current), current.Scope})
	return nil
}

// Eligibility compares frozen prospective duties, without granting carry or
// validating contracts again. Accepted results keep their physical bindings;
// contract errors are returned by the existing selected-action acceptor.
func (s *State) retryableValidationQA(source, projection *ValidationWave) ([]*ValidationTarget, error) {
	if source == nil || projection == nil || projection.Status == "INVALIDATED" || projection.Status == "REPAIRED" {
		return nil, validationError("QA retry needs selected targets on the current wave")
	}
	for _, work := range s.repairWork() {
		if work.result != nil {
			continue
		}
		for action, binding := range s.DispatchBindings {
			if binding.Task == work.task && (s.liveDispatch(action) || s.EndedAttempts[binding.Task].AwaitingResult) {
				return nil, validationError("wait for the issued candidate repair before QA retry")
			}
		}
	}
	var targets []*ValidationTarget
	for _, target := range source.Targets {
		if target.Kind != "QA" || target.Status == "AUTHORIZED_SKIP" || target.Status == "DISPOSED" {
			continue
		}
		if !s.sameValidationDuty(WorkBinding{WaveID: source.ID, Task: target.Task}, projection) {
			continue
		}
		for _, current := range projection.Targets {
			if current.Kind == target.Kind && current.Mode == target.Mode && current.Status != "AUTHORIZED_SKIP" && current.Status != "DISPOSED" {
				targets = append(targets, target)
				break
			}
		}
	}
	return targets, nil
}

// ValidationNotGuaranteed reports current explicit exceptions without converting
// their underlying review or execution evidence into PASS.
func (s *State) ValidationNotGuaranteed() bool {
	if s.Validation == nil {
		return false
	}
	if s.routeHasNoQAModes() {
		return true
	}
	for _, mode := range s.validationKinds() {
		if s.caseSetWaiver(mode) != "" {
			return true
		}
	}
	if w := s.currentValidationWave(); w != nil {
		for _, target := range w.Targets {
			if target.Status == "AUTHORIZED_SKIP" || target.Status == "DISPOSED" {
				return true
			}
		}
		for _, finding := range s.Validation.Findings {
			if target := s.validationFindingResponsibility(w, finding); target != nil && len(finding.History) > 0 && s.validationObligationWaived(w, target, finding.History[len(finding.History)-1].ProblemID) {
				return true
			}
		}
	}
	return false
}

func requestCaseSet(r *ValidationRequest) *CaseSet {
	if r.Kind == "RETRY" {
		var basis struct{ Detail struct{ CaseSet *CaseSet } }
		if json.Unmarshal(r.Summary, &basis) != nil {
			return nil
		}
		return basis.Detail.CaseSet
	}
	if r.Kind != "QA_REVIEW" {
		var basis struct{ Detail Validation }
		if json.Unmarshal(r.Summary, &basis) != nil {
			return nil
		}
		return basis.Detail.Sets[r.Mode]
	}
	var basis struct{ Detail *CaseSet }
	if json.Unmarshal(r.Summary, &basis) != nil {
		return nil
	}
	return basis.Detail
}
