package protocol

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
)

func (s *State) projectValidationBoundary(view *decision.State, cd *compiler.CompiledDefinition) {
	if !pipelineEnabled(s, cd) {
		return
	}

	for _, step := range cd.Steps {
		p, ok := step.Payload.(compiler.CompiledHostActionStep)
		if !ok || p.Operation != "op.candidate" {
			continue
		}
		view.RequiredTasks[step.Header.ID] = nil
		for _, intent := range s.PendingHostActions {
			if intent.Adapter != nil && intent.Adapter.Operation == p.Operation {
				view.RequiredTasks[step.Header.ID] = []decision.RequiredTask{{Task: runtime.TaskKey{Node: step.Header.NodeID, Step: step.Header.ID, Scope: intent.ActionID}}}
			}
		}
	}

	if !s.dualReviewPass() {
		return
	}
	message := ""
	if s.Topology == nil || s.RouteMode == "" {
		message = "confirm topology and select route"
	} else if s.Validation == nil || s.Validation.RequirementRevision != s.RequirementRevision() {
		message = "submit confirmed REQUIREMENT document via OPERATOR_OBSERVATION.validation"
	} else if w := s.currentValidationWave(); w != nil {
		switch w.Status {
		case "ADJUDICATING":
			message = "submit per-finding ADJUDICATE observations"
			if s.validationFindingsAdjudicated(w) && w.hasFailedQA() {
				message = "QA failure remains unresolved; request QA_ARTIFACT_REPAIR or VALIDATION_DETAIL_DISPOSITION with a justified remedy"
			}
		case "COMPLETE":
			message = "validation complete; candidate promotion provider is not installed"
		}
	}
	if s.Validation != nil && s.Validation.DevelopmentSnapshot != "" && s.Validation.Candidate == nil {
		frozen := true
		for _, mode := range s.validationKinds() {
			if set := s.Validation.Sets[mode]; set == nil || set.Status != "FROZEN" {
				frozen = false
			}
		}
		if frozen {
			if err := s.validateFinalCoverage(); err != nil {
				message = err.Error()
			}
		}
	}
	if message != "" {
		view.OperatorFacts = []decision.Fact{{Source: decision.SourceReceipt, Key: "validation", Value: message}}
	}
}

func (e *Engine) advanceValidation(s *State, nextRevision uint64) (bool, error) {
	changed := e.ensureRetryAsks(s, nextRevision)
	if !pipelineEnabled(s, e.cfg.Definition) || s.Validation == nil || s.Validation.RequirementRevision != s.RequirementRevision() || s.requirementChangePending() || s.Topology == nil || s.RouteMode == "" {
		return changed, nil
	}
	v := s.Validation
	if w := s.currentValidationWave(); w != nil && w.RouteDigest != s.validationRouteDigest() && w.Status != "INVALIDATED" && w.Status != "REPAIRED" {
		e.rebindValidationRoute(s)
		changed = true
	}
	for _, mode := range s.validationKinds() {
		set := v.Sets[mode]
		if set != nil && set.Status == "REVIEW" && len(set.ReviewHistory) >= s.qaReviewLimit(mode) {
			changed = s.ensureValidationAsk(ValidationRequest{Kind: "QA_REVIEW", Mode: mode, Target: fmt.Sprintf("%d:%s:%d", v.Generation, mode, len(set.ReviewHistory))}, nextRevision) || changed
		}
	}
	w := s.currentValidationWave()
	if w != nil && w.Status == "COLLECTING" {
		complete := true
		for _, t := range w.Targets {
			if t.Status != "PASS" && t.Status != "FAIL" && t.Status != "DISPOSED" {
				complete = false
			}
		}
		if complete {
			w.Status = "ADJUDICATING"
			changed = true
			e.settleValidationWave(s)
		}
	}
	if s.validationRepairPending(w) {
		if w.Repair.Evidence != nil {
			v.Development = w.Repair.Evidence
			v.DevelopmentSnapshot = ""
			v.Candidate = nil
			v.AuthoringWorkspace = ""
			w.Status = "REPAIRED"
			changed = true
		} else if s.completedValidationWaves() >= 3 && !w.ExtraAuthorized && !w.Repair.PureP2 {
			changed = s.ensureValidationAsk(ValidationRequest{Kind: "REPAIR", Target: w.ID}, nextRevision) || changed
		}
	}
	if w != nil && w.Status == "COMPLETE" && w.Scope == coverage.ScopeAffected {
		e.freezeValidationWave(s, coverage.ScopeFull)
		return true, nil
	}
	if v.Development == nil {
		return changed, nil
	}
	for _, intent := range s.PendingHostActions {
		if intent.Adapter != nil && intent.Adapter.Operation == "op.candidate" {
			return changed, nil
		}
	}
	params := CandidateParameters{RouteScope: s.validationRouteDigest(), RequirementRevision: v.RequirementRevision, Generation: v.Generation, Paths: slices.Clone(v.Development.Paths)}
	switch {
	case v.DevelopmentSnapshot == "":
		params.Stage = "DEVELOPMENT"
	case slices.Contains(s.validationKinds(), coverage.ReviewWhitebox) && v.Sets[coverage.ReviewWhitebox] == nil && v.AuthoringWorkspace == "":
		params.Stage = "AUTHORING"
		params.Base = v.DevelopmentSnapshot
	default:
		set := v.Sets[coverage.ReviewWhitebox]
		if set != nil && set.Status == "REVIEW" && set.Candidate == "" && len(set.Paths) == 0 {
			set.Candidate = v.DevelopmentSnapshot
			changed = true
		}
		if set != nil && set.Status == "REVIEW" && set.Candidate == "" {
			params.Stage = "QA_ARTIFACT"
			params.Base = v.DevelopmentSnapshot
			params.Paths = slices.Clone(set.Paths)
		} else {
			if v.Candidate != nil {
				return changed, nil
			}
			for _, mode := range s.validationKinds() {
				if set := v.Sets[mode]; set == nil || set.Status != "FROZEN" {
					return changed, nil
				}
			}
			if err := s.validateFinalCoverage(); err != nil {
				return changed, nil
			}
			params.Stage = "REVIEW_READY"
			params.QAArtifacts = map[coverage.ReviewKind]CandidateArtifact{}
			for mode, artifact := range v.Sets {
				if len(artifact.Paths) > 0 {
					if artifact.Candidate == "" {
						return changed, validationError("approved QA artifact has no immutable candidate")
					}
					params.QAArtifacts[mode] = CandidateArtifact{Identity: artifact.Candidate, Paths: slices.Clone(artifact.Paths)}
					for _, path := range artifact.Paths {
						if !slices.Contains(params.Paths, path) {
							params.Paths = append(params.Paths, path)
						}
					}
				}
			}
			slices.Sort(params.Paths)
			params.Base = v.DevelopmentSnapshot
			params.Finalize = true
		}
	}
	params.CaseVersions = map[coverage.ReviewKind]string{}
	for mode, set := range v.Sets {
		params.CaseVersions[mode] = set.Version
	}
	intent, err := e.prepareAdapterIntent(s, nextRevision, "op.candidate", params, fmt.Sprintf("candidate:%d", nextRevision))
	if err != nil {
		return changed, err
	}
	s.PendingHostActions[intent.ActionID] = intent
	return true, nil
}

func (s *State) validateFinalCoverage() error {
	if len(s.validationKinds()) == 0 {
		return fmt.Errorf("confirmed AC guarantee requires selected QA")
	}
	if _, err := s.validationWhitelist(); err != nil {
		return err
	}
	covered := map[string]bool{}
	for _, set := range s.Validation.Sets {
		for _, edge := range set.Manifest.Edges {
			covered[edge.PointID] = true
		}
	}
	points, _ := s.coveragePoints()
	for _, point := range points {
		if !covered[point.PointID] {
			return fmt.Errorf("confirmed point %s has no approved case", point.PointID)
		}
	}

	return nil
}

// applyCandidateReceipt is the common settlement of immediate and held adapter
// receipts. A receipt supplies observed facts for an already issued intent.
func decodeCandidateEvidence(receipt HostActionReceipt) (CandidateEvidence, error) {
	var evidence CandidateEvidence
	if receipt.AdapterEvidence == nil {
		return evidence, validationError("candidate receipt needs typed adapter evidence")
	}
	data, err := json.Marshal(receipt.AdapterEvidence.Values)
	if err != nil {
		return evidence, err
	}
	err = json.Unmarshal(data, &evidence)
	return evidence, err
}

func (e *Engine) applyCandidateReceipt(s *State, intent HostActionIntent, receipt HostActionReceipt) error {
	if intent.Adapter == nil || intent.Adapter.Operation != "op.candidate" {
		return nil
	}
	var params CandidateParameters
	data, _ := json.Marshal(intent.Adapter.Params)
	if err := json.Unmarshal(data, &params); err != nil {
		return err
	}
	evidence, err := decodeCandidateEvidence(receipt)
	if err != nil {
		return err
	}
	if evidence.ParametersDigest != intent.PayloadDigest || evidence.Identity == "" || evidence.Digest == "" {
		return validationError("candidate receipt lacks exact intent digest and native identity/digest")
	}
	expected := slices.Clone(params.Paths)
	actual := slices.Clone(evidence.Paths)
	slices.Sort(expected)
	slices.Sort(actual)
	if !slices.Equal(expected, actual) {
		return validationError("candidate receipt does not account for every declared path")
	}
	v := s.Validation
	if v == nil || params.Generation != v.Generation || params.RequirementRevision != v.RequirementRevision {
		return nil
	}
	switch params.Stage {
	case "DEVELOPMENT":
		v.DevelopmentSnapshot = evidence.Identity
	case "AUTHORING":
		if params.Base != v.DevelopmentSnapshot {
			return validationError("authoring workspace base changed")
		}
		v.AuthoringWorkspace = evidence.Identity
	case "QA_ARTIFACT":
		if !evidence.TestOnly || !evidence.ProductionViewEqual {
			return validationError("whitebox changes must be proved test-owned and production-equivalent; submit production edits through development/repair")
		}
		set := v.Sets[coverage.ReviewWhitebox]
		if set == nil || params.CaseVersions[coverage.ReviewWhitebox] != digestOfCanonical(manifestAtRoute(set.Manifest, params.RouteScope)) {
			return validationError("whitebox case version changed")
		}
		set.Candidate = evidence.Identity
	case "REVIEW_READY":
		if !params.Finalize {
			return validationError("final candidate must include commit organization before validation")
		}
		if err := s.validateFinalCoverage(); err != nil {
			return validationReject(err)
		}
		for mode, set := range v.Sets {
			if params.CaseVersions[mode] != digestOfCanonical(manifestAtRoute(set.Manifest, params.RouteScope)) {
				return validationError("case version changed while materializing candidate")
			}
			set.Candidate = evidence.Identity
		}
		v.Candidate = &coverage.ValidationCandidate{Identity: evidence.Identity, Digest: evidence.Digest}
		scope := coverage.ScopeFull
		if prior := s.currentValidationWave(); prior != nil && prior.Status == "REPAIRED" {
			scope = coverage.ScopeAffected
		}
		e.freezeValidationWave(s, scope)
	default:
		return validationError("unknown candidate stage")
	}
	return nil
}
func (e *Engine) freezeValidationWave(s *State, scope coverage.ScopeMode) {
	v := s.Validation
	versions := map[coverage.ReviewKind]string{}
	for _, mode := range s.validationKinds() {
		versions[mode] = v.Sets[mode].Version
	}
	w := ValidationWave{Candidate: *v.Candidate, RequirementRevision: v.RequirementRevision, Generation: v.Generation, RouteDigest: s.validationRouteDigest(), Versions: versions, Status: "COLLECTING", Scope: scope, Targets: map[string]*ValidationTarget{}}
	w.Coverage = s.coverageContract(v.Candidate)
	for _, mode := range s.validationKinds() {
		w.Reviews = append(w.Reviews, *v.Sets[mode].Review)
	}
	// A FULL obligation after AFFECTED on the same candidate is distinct, while
	// retrying either obligation keeps its TaskKey and frozen expected set.
	w.ID = digestOfCanonical(struct {
		Candidate  coverage.ValidationCandidate
		Versions   map[coverage.ReviewKind]string
		Route      string
		Scope      coverage.ScopeMode
		Generation uint64
	}{w.Candidate, versions, w.RouteDigest, scope, v.Generation})
	w.ID = strings.TrimPrefix(w.ID, "sha256:")
	prior := s.currentValidationWave()
	if prior != nil {
		if prior.Candidate == w.Candidate {
			w.RepairSource = prior.RepairSource
		} else if prior.Repair != nil && prior.Repair.Evidence != nil {
			w.RepairSource = prior.Repair.Task.String()
		}
	}
	for _, gate := range s.SelectedGates {
		mode := coverage.ReviewKind("")
		switch gate {
		case "blackbox":
			mode = coverage.ReviewBlackbox
		case "whitebox":
			mode = coverage.ReviewWhitebox
		case "merge":
			mode = coverage.ReviewMerge
		}
		target := &ValidationTarget{Status: "PENDING", Gate: gate, Kind: "GATE"}
		handler := "engine.gate"
		part := gate
		if mode != "" {
			handler = "engine.qa.execution"
			part = string(mode)
			target.Mode = mode
			target.Kind = "QA"
			target.Gate = ""
		}
		target.Task = s.validationRequiredTask(pipelineTask(e.cfg.Definition, handler, w.ID+":"+part), e.cfg.Definition)
		if prior != nil {
			if prior.Candidate == w.Candidate && prior.RouteDigest != w.RouteDigest {
				s.carryRouteTarget(target, prior, &w)
			} else if mode == "" {
				s.carryGateResult(target, prior, w.Candidate)
			}
		}
		w.Targets[target.Task.String()] = target
	}
	w.ExpectedDigest = digestOfCanonical(struct {
		Candidate coverage.ValidationCandidate
		Versions  map[coverage.ReviewKind]string
		Route     string
		Targets   []string
		Scope     coverage.ScopeMode
	}{w.Candidate, w.Versions, w.RouteDigest, sortedTargetKeys(&w), scope})
	v.Waves = append(v.Waves, w)
}

// A disposition applies to its candidate. P2 completion can carry only genuine
// passes or failures whose verified problems were covered by that repair.
func (s *State) carryGateResult(target *ValidationTarget, prior *ValidationWave, candidate coverage.ValidationCandidate) {
	for _, previous := range prior.Targets {
		if previous.Gate != target.Gate {
			continue
		}
		if prior.Candidate == candidate && (previous.Status == "PASS" || previous.Status == "DISPOSED") {
			target.Status = previous.Status
			target.ResultAction = previous.ResultAction
			target.DispositionReason = previous.DispositionReason
			return
		}
		if prior.Repair == nil || !prior.Repair.PureP2 || prior.Repair.Evidence == nil {
			return
		}
		if previous.Status == "PASS" {
			target.Status = "PASS"
			target.ResultAction = previous.ResultAction
			return
		}
		if previous.Status != "FAIL" {
			return
		}
		for _, finding := range s.Validation.Findings {
			if s.validationFindingTarget(prior, finding) != previous || len(finding.History) == 0 {
				continue
			}
			resolution := finding.History[len(finding.History)-1]
			if resolution.Decision != "DISMISSED" && resolution.Severity == "P2" && slices.Contains(prior.Repair.Problems, resolution.ProblemID) {
				target.Status = "PASS"
				target.ResultAction = prior.Repair.ResultAction
				return
			}
		}
		return
	}
}

func (e *Engine) adjudicateValidation(s *State, subject string, items []FindingResolution) error {
	w := s.currentValidationWave()
	if w == nil || w.ID != subject || w.Status != "ADJUDICATING" {
		return validationError("findings can be adjudicated only after the wave is collected")
	}
	seen := map[string]bool{}
	for _, item := range items {
		f := s.Validation.Findings[item.FindingID]
		if f == nil || s.validationFindingTarget(w, f) == nil || seen[item.FindingID] {
			return validationError("unknown or duplicate finding")
		}
		seen[item.FindingID] = true
		if item.Supersedes != len(f.History) {
			return validationError("finding adjudication history changed")
		}
		if item.Reason == "" || item.Requirement == "" || item.NormalPath == "" || item.Module == "" || item.RootCause == "" || item.Consequence == "" || item.SeverityRationale == "" || item.Verification.Scope == "" || item.Verification.Severity == "" || item.Verification.Causality == "" || item.Verification.Reproduction == "" || item.Verification.Classification == "" {
			return validationError("each finding needs independent dimensions, severity rationale and verification")
		}
		if !slices.Contains([]string{"P0", "P1", "P2", "P3"}, item.Severity) {
			return validationError("invalid effective severity")
		}
		switch item.Decision {
		case "NEW":
			if item.ProblemID != "" {
				return validationError("NEW problem identity is assigned by the engine")
			}
			item.ProblemID = digestOfCanonical(struct {
				Finding  string
				Revision int
			}{f.ID, len(f.History) + 1})
			s.Validation.Problems[item.ProblemID] = &ValidationProblem{ID: item.ProblemID, FindingIDs: []string{f.ID}}
		case "SAME_AS":
			p := s.Validation.Problems[item.ProblemID]
			if p == nil {
				return validationError("SAME_AS references an unknown problem")
			}
			if !slices.Contains(p.FindingIDs, f.ID) {
				p.FindingIDs = append(p.FindingIDs, f.ID)
			}
		case "DISMISSED":
			if item.ProblemID != "" {
				return validationError("dismissed finding cannot create a problem")
			}
		default:
			return validationError("finding decision must be NEW/SAME_AS/DISMISSED")
		}
		f.History = append(f.History, item)
	}
	e.settleValidationWave(s)
	return nil
}
func (w *ValidationWave) hasFailedQA() bool {
	for _, target := range w.Targets {
		if target.Kind == "QA" && target.Status == "FAIL" {
			return true
		}
	}
	return false
}

func (s *State) validationFindingsAdjudicated(w *ValidationWave) bool {
	for _, finding := range s.Validation.Findings {
		if s.validationFindingTarget(w, finding) != nil && len(finding.History) == 0 {
			return false
		}
	}
	return true
}

func (s *State) validationDispositionReady(w *ValidationWave) bool {
	return w != nil && (s.validationRepairPending(w) || w.Status == "ADJUDICATING" && w.hasFailedQA() && s.validationFindingsAdjudicated(w))
}

func (e *Engine) settleValidationWave(s *State) {
	w := s.currentValidationWave()
	if w == nil || w.Status != "ADJUDICATING" || !s.validationFindingsAdjudicated(w) {
		return
	}
	problems := []string{}
	pureP2 := true
	for _, f := range s.Validation.Findings {
		target := s.validationFindingTarget(w, f)
		if target == nil {
			continue
		}
		a := f.History[len(f.History)-1]
		if a.Decision == "DISMISSED" {
			continue
		}
		if target.Status == "DISPOSED" {
			continue
		}
		obligation := target.Kind == "QA" && target.Status == "FAIL" || a.Severity == "P0" || a.Severity == "P1" || target.Kind == "GATE" && a.Severity == "P2"
		if obligation {
			if !slices.Contains(problems, a.ProblemID) {
				problems = append(problems, a.ProblemID)
			}
			if target.Kind != "GATE" || a.Severity != "P2" {
				pureP2 = false
			}
		}
	}
	slices.Sort(problems)
	if len(problems) == 0 {
		if w.hasFailedQA() {
			return
		}
		w.Status = "COMPLETE"
		w.Counted = s.validationWaveCountable(w)
		return
	}
	w.Counted = s.validationWaveCountable(w)
	w.Status = "REPAIR"
	w.Repair = &ValidationRepair{Task: pipelineTask(e.cfg.Definition, "engine.repair", w.Candidate.Identity), Problems: problems, PureP2: pureP2}
	// Native identities may contain separators; the logical snapshot key uses
	// its content digest, never a platform path embedded in TaskKey syntax.
	w.Repair.Task.Scope = strings.TrimPrefix(digestOfCanonical(w.Candidate), "sha256:")
}

// Validation counting follows the review-ready snapshot, not execution scope.
func (s *State) validationWaveCountable(w *ValidationWave) bool {
	for i := range s.Validation.Waves {
		prior := &s.Validation.Waves[i]
		if prior.ID == w.ID {
			break
		}
		if prior.Candidate == w.Candidate && prior.Counted {
			return false
		}
		if prior.Repair != nil && prior.Repair.Task.String() == w.RepairSource && prior.Repair.PureP2 {
			return false
		}
	}
	return true
}
