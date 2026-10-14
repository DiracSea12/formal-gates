package protocol

import (
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"slices"
)

// ResetPreview is the exact destructive scope owned by the pending Reset Ask.
// Accepted development and its native target survive; only registered scratch
// resources are candidates for cleanup.
type ResetPreview struct {
	Intake              *IntakeConfirmationReceipt `json:"intake,omitempty"`
	RunID               string                     `json:"runId"`
	RequirementRevision string                     `json:"requirementRevision"`
	Base                string                     `json:"base"`
	Current             string                     `json:"current"`
	Development         string                     `json:"development,omitempty"`
	Topology            *TopologyBinding           `json:"topology,omitempty"`
	RouteMode           string                     `json:"routeMode"`
	Gates               []string                   `json:"gates"`
	Children            []string                   `json:"children"`
	ChildPreviews       []ResetPreview             `json:"childPreviews,omitempty"`
	Preserve            []string                   `json:"preserve"`
	Clear               []string                   `json:"clear"`
	Resources           []NativeResource           `json:"resources"`
	Work                []RequirementWork          `json:"work"`
	Digest              string                     `json:"digest"`
}

func (p ResetPreview) CanonicalDigest() string { p.Digest = ""; return digestOfCanonical(p) }

type ResetDecision struct {
	PreviewDigest string `json:"previewDigest"`
	Reason        string `json:"reason"`
	Applied       bool   `json:"applied,omitempty"`
	Confirmed     bool   `json:"confirmed,omitempty"`
}
type ResetAuthority struct {
	Decision RecordedDecision `json:"decision"`
	Event    EventRecord      `json:"event"`
	Preview  ResetPreview     `json:"preview"`
}

func (s *State) ResetPreview() ResetPreview {
	p := ResetPreview{RunID: s.RunID, RequirementRevision: s.RequirementRevision(), Base: s.BaseSnapshot, Current: s.AcceptedNativeSnapshot(), Topology: s.Topology, RouteMode: s.RouteMode, Gates: slices.Clone(s.SelectedGates), Preserve: []string{"native target bytes and committed content", "base/current/development and pre-repair identities", "topology, child origin and route obligations", "accepted development and same-revision completed batches", "intake, event and result audit, cumulative costs"}, Clear: []string{"current review conclusions and automatic grants", "effective QA/gate results, carry and candidates", "waves, problems, waivers and repair obligations", "unaccepted dispatch eligibility and pending controls", "slice checkpoint eligibility", "requirement confirmation"}}
	if s.Validation != nil {
		p.Development = s.Validation.DevelopmentSnapshot
	}
	if s.ChildCreation != nil {
		for _, c := range s.ChildCreation.Children {
			p.Children = append(p.Children, c.RunID)
		}
	}
	// The signed development freeze names the retained workspace. Equal native
	// content in a gate or candidate workspace does not give that resource the
	// development role or its preservation authority.
	developmentResources := map[string]bool{}
	if p.Development != "" {
		for action, receipt := range s.HostActionReceipts {
			intent, exists := s.hostIntent(action)
			params, err := candidateParameters(intent)
			if !exists || err != nil || params.Stage != "DEVELOPMENT" || receipt.Status != HostActionStatusExecuted {
				continue
			}
			evidence, err := decodeCandidateEvidence(receipt)
			resource := s.NativeResources[params.WorkspaceID]
			if err == nil && evidence.Identity == p.Development && resource.Owner == s.RunID && (resource.Role == "development" || resource.Role == "merge") {
				developmentResources[resource.ID] = true
			}
		}
	}
	for _, r := range s.NativeResources {
		if r.Status != "CLEANED" && r.Role != "child-target" && r.Path != s.NativeRoot && !developmentResources[r.ID] {
			p.Resources = append(p.Resources, r)
		}
	}
	slices.SortFunc(p.Resources, func(a, b NativeResource) int {
		if a.ID < b.ID {
			return -1
		}
		if a.ID > b.ID {
			return 1
		}
		return 0
	})
	p.Digest = p.CanonicalDigest()
	return p
}
func (e *Engine) acceptResetDecision(s *State, ask PendingAsk, d *DecidePayload) error {
	if ask.Control != ControlReset {
		return validationError("reset payload requires the reset Ask")
	}
	if d.Choice == "decline" {
		if d.Reset != nil {
			return validationError("declining reset carries no payload")
		}
		return nil
	}
	if d.Choice != "proceed" || d.Reset == nil || ask.Reset == nil || d.Reset.PreviewDigest != ask.Reset.Digest || d.Reset.Reason == "" || d.Reset.Applied || d.Reset.Confirmed {
		return validationError("reset requires the exact preservation/cleanup preview and reason")
	}
	current := s.ResetPreview()
	current.Work = s.requirementWork(e.cfg.Definition)
	current.RequirementRevision = ask.Reset.RequirementRevision
	current.Intake = ask.Reset.Intake
	current.Children = ask.Reset.Children
	current.ChildPreviews = ask.Reset.ChildPreviews
	current.Digest = current.CanonicalDigest()
	if current.Digest != ask.Reset.Digest {
		return validationError("reset preview is superseded; request a fresh preview")
	}
	return nil
}
func (s *State) resetDecision() (RecordedDecision, bool) {
	var found RecordedDecision
	for _, d := range s.Decisions {
		if d.Control == ControlReset && d.Choice == "proceed" && d.Reset != nil && d.Revision > found.Revision {
			found = d
		}
	}
	return found, found.Reset != nil
}
func (s *State) resetPending() bool {
	d, ok := s.resetDecision()
	return ok && !d.Reset.Confirmed && !s.abortRequested()
}
func (s *State) resetRequestID() string {
	d, ok := s.resetDecision()
	if ok {
		return d.RequestID
	}
	return ""
}

// stopControlledWorkers retains responsibility until exact lifecycle evidence
// permits retirement. Both reset and requirement REDO use this existing typed
// termination protocol; UNKNOWN spawn receipts remain for ordinary reconcile.
func (e *Engine) stopControlledWorkers(s *State, request, reason string, revision uint64, all bool) (bool, error) {
	ids := []string{}
	for id := range s.PendingActions {
		if _, obsolete := s.ObsoleteActions[id]; all || obsolete {
			ids = append(ids, id)
		}
	}
	for _, a := range s.EndedAttempts {
		_, obsolete := s.ObsoleteActions[a.ActionID]
		if a.AwaitingResult && (all || obsolete) && !slices.Contains(ids, a.ActionID) {
			ids = append(ids, a.ActionID)
		}
	}
	slices.Sort(ids)
	changed := false
	for _, id := range ids {
		receipt, known := s.SpawnReceipts[id]
		stopped := known && (receipt.Status == SpawnStatusFailed || s.hasLifecycleEvent(receipt.agentIdentity(), LifecycleStop))
		if !known {
			failures := s.SpawnFailures[id]
			stopped = len(failures) > 0 && failures[len(failures)-1].FailureClass != authoring.FailureSideEffectUnknown
			if !stopped && e.cfg.WorkerExecution != nil {
				pending, attempt, _ := s.workerActionTarget(id)
				observed, err := e.cfg.WorkerExecution(pending, attempt)
				if err != nil {
					return changed, err
				}
				if observed != nil {
					event, err := NewSpawnReceiptEvent(EventID("native-spawn:"+id), observed.ActionID, observed.Provider, observed.Correlation, observed.Status)
					if err != nil {
						return changed, err
					}
					digest, err := event.Digest()
					if err != nil {
						return changed, err
					}
					accepted, _, _, err := e.admit(event, digest, s, revision-1, revision, Observation{})
					if err != nil {
						return changed, err
					}
					s.Events[string(event.ID)] = EventRecord{Digest: digest, Acceptance: accepted}
					receipt, known = s.SpawnReceipts[id]
					changed = true
					stopped = known && (receipt.Status == SpawnStatusFailed || s.hasLifecycleEvent(receipt.agentIdentity(), LifecycleStop))
				} else {
					_, result := s.Results[id]
					_, staged := s.StagedResults[id]
					stopped = !result && !staged && len(s.ObsoleteResults[id]) == 0
				}
			}
		}
		if stopped {
			p, retired := s.retireAction(id, "", reason+request)
			if retired {
				if err := s.terminalizeTask(p.Task); err != nil {
					return changed, err
				}
				s.routingReleasedCapacity = true
			}
			delete(s.EndedAttempts, p.Task)
			changed = changed || retired
			continue
		}
		if !known || receipt.Status == SpawnStatusUnknown {
			continue
		}
		exists := false
		for _, intent := range s.PendingHostActions {
			exists = exists || intent.Terminate != nil && intent.Terminate.WorkerActionID == id
		}
		if exists {
			continue
		}
		intent, err := e.agentHostIntent(s, HostActionTerminateAgent, id, receipt.agentIdentity(), reason+request, "", revision)
		if err != nil {
			return changed, err
		}
		s.PendingHostActions[intent.ActionID] = intent
		return true, nil
	}
	return changed, nil
}
func (e *Engine) advanceReset(s *State, nextRevision uint64) (bool, error) {
	d, ok := s.resetDecision()
	if !ok || d.Reset.Confirmed {
		return false, nil
	}
	changed, err := e.stopControlledWorkers(s, d.RequestID, "Reset authorized by ", nextRevision, true)
	if err != nil {
		return changed, err
	}
	settled, err := e.executeHeldSettles(s, nextRevision)
	if err != nil {
		return changed, err
	}
	changed = changed || settled
	if len(s.PendingActions) != 0 || len(s.PendingHostActions) != 0 {
		return changed, nil
	}
	for _, a := range s.EndedAttempts {
		if a.AwaitingResult {
			return changed, nil
		}
	}
	if d.Reset.Applied {
		return changed, nil
	}
	preview := s.PendingAsks[d.RequestID].Reset
	if preview == nil && s.SliceChild != nil && s.SliceChild.ParentReset != nil {
		preview = &s.SliceChild.ParentReset.Preview
	}
	if preview == nil {
		return changed, validationError("reset has no accepted preview")
	}
	var resources []NativeResource
	for _, r := range preview.Resources {
		if s.NativeResources[r.ID].Status != "CLEANED" {
			resources = append(resources, s.NativeResources[r.ID])
		}
	}
	if len(resources) > 0 {
		intent, err := e.prepareAdapterIntent(s, nextRevision, "op.cleanup", CleanupParameters{Resources: resources}, "")
		if err != nil {
			return changed, err
		}
		s.PendingHostActions[intent.ActionID] = intent
		return true, nil
	}
	// The confirmed development plan still reads its signed readiness payload.
	// ReviewSeries owns the review conclusion and quota, which reset rebuilds;
	// retaining this one plan source does not carry that review conclusion.
	planSource := ""
	planRequest, _, _ := s.confirmedDevelopmentPlan()
	if confirmation := decisionPlanConfirmation(s.Decisions[planRequest]); confirmation != nil {
		planSource = confirmation.ReviewActionID
	}
	for id := range s.Results {
		binding := s.DispatchBindings[id]
		if id != planSource && s.validationRole(binding.Task, e.cfg.Definition) != "engine.development.batch.v5" {
			s.archiveDispatchResults(id)
		}
	}
	for id, receipt := range s.SpawnReceipts {
		if s.validationRole(receipt.Task, e.cfg.Definition) != "engine.development.batch.v5" {
			s.retireAction(id, "", "Reset authorized by "+d.RequestID)
		}
	}
	for id := range s.StagedResults {
		s.archiveDispatchResults(id)
	}
	for _, result := range s.QuarantinedResults {
		s.archiveDispatchResults(result.ActionID)
	}
	for id, ask := range s.PendingAsks {
		if !ask.Resolved {
			ask.Resolved = true
			s.PendingAsks[id] = ask
		}
	}
	s.ReviewSeries = review.NewSet()
	s.SliceReceipt = nil
	s.PromotedSnapshot = ""
	s.PromotedDigest = ""
	s.CandidatePromotion = nil
	s.Expected = nil
	s.Completed = nil
	keptTasks := map[runtime.TaskKey]runtime.TaskStatus{}
	for id, result := range s.Results {
		if result.Outcome == OutcomePass && (result.Validation == nil || result.Validation.Status != "CONTINUE") {
			if binding, ok := s.DispatchBindings[id]; ok {
				keptTasks[binding.Task] = runtime.TaskTerminal
			}
		}
	}
	s.Tasks = keptTasks
	s.Attempts = map[runtime.TaskKey]Attempt{}
	if s.Validation != nil {
		v := s.Validation
		s.Validation = &Validation{Document: v.Document, RequirementRevision: v.RequirementRevision, Generation: nextRevision, DevelopmentSnapshot: v.DevelopmentSnapshot, Sets: map[coverage.ReviewKind]*CaseSet{}, Findings: map[string]*ValidationFinding{}, Problems: map[string]*ValidationProblem{}}
	}
	if s.Phase != runtime.PhasePreparing {
		s.Phase = runtime.PhaseIntakeRegistered
	}
	d.Reset.Applied = true
	s.Decisions[d.RequestID] = d
	request := "reset-confirm:" + d.RequestID
	pkg := &RequirementDecisionPackage{Intake: preview.Intake, Coverage: s.confirmedCoverage(), CandidateRevision: preview.RequirementRevision, DeltaDigest: preview.Digest, ImpactDigest: preview.Digest, Source: RequirementSourceReset, Classification: RequirementClassificationPreserved, RedoProductReview: s.SliceChild == nil, RedoStartReadiness: s.SliceChild == nil, ResetRequestID: d.RequestID}
	for _, work := range s.requirementWork(e.cfg.Definition) {
		disposition := RequirementWorkRedo
		if result, ok := s.Results[work.ActionID]; ok && result.Outcome == OutcomePass && (result.Validation == nil || result.Validation.Status != "CONTINUE") && s.validationRole(work.Task, e.cfg.Definition) == "engine.development.batch.v5" {
			disposition = RequirementWorkCarry
		}
		pkg.WorkDecisions = append(pkg.WorkDecisions, RequirementWorkDecision{Work: work, Disposition: disposition, Reason: d.Reset.Reason})
	}
	s.PendingAsks[request] = PendingAsk{RequestID: request, Control: ControlRequestRequirementChange, BaseRevision: s.RequirementRevision(), BaseGeneration: s.requirementGeneration(), Package: pkg, ResetRequestID: d.RequestID, Options: []AskOption{{ID: "proceed", Label: "Confirm the complete current requirement and solution"}, {ID: "abort", Label: "Leave requirements unconfirmed"}}}
	return true, nil
}
func (e *Engine) SyncParentReset(parent *State, input Observation) error {
	e = e.observing(input)
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	d, ok := parent.resetDecision()
	if !ok || s.SliceChild == nil || s.Phase == runtime.PhaseTerminal {
		return nil
	}
	preview := parent.PendingAsks[d.RequestID].Reset
	if preview == nil || !slices.Contains(preview.Children, s.RunID) {
		return nil
	}
	if s.SliceChild.ParentReset != nil && s.SliceChild.ParentReset.Decision.RequestID == d.RequestID {
		local := s.Decisions[d.RequestID]
		if !d.Reset.Confirmed || local.Reset == nil || !local.Reset.Applied || local.Reset.Confirmed {
			return nil
		}
		request := "reset-confirm:" + d.RequestID
		ask := s.PendingAsks[request]
		pkg := ask.Package
		if pkg == nil {
			return validationError("cascaded reset has no local re-registration")
		}
		payload := &RequirementDecisionPayload{Intake: pkg.Intake, Coverage: pkg.Coverage, BaseRevision: s.RequirementRevision(), CandidateRevision: pkg.CandidateRevision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: pkg.WorkDecisions, ResetRequestID: d.RequestID, Reason: d.Reset.Reason}
		document := ""
		if parent.validationRequirementCurrent() {
			document = parent.Validation.Document.Content
		}
		if err := e.admitRequirementDecision(s, ask, request, &DecidePayload{Choice: requirementProceedOption, Requirement: payload}, revision+1, input, document); err != nil {
			return err
		}
		ask.Resolved = true
		s.PendingAsks[request] = ask
		s.SliceChild.ParentReset.Decision = d
		_, err = e.commit(s, revision, input)
		return err
	}
	local := s.ResetPreview()
	for _, approved := range preview.ChildPreviews {
		if approved.RunID == s.RunID {
			local = approved
			break
		}
	}
	local.RequirementRevision = preview.RequirementRevision
	local.Digest = local.CanonicalDigest()
	copy := d
	copy.Revision = revision + 1
	copy.Reset = &ResetDecision{PreviewDigest: local.Digest, Reason: d.Reset.Reason}
	s.SliceChild.ParentReset = &ResetAuthority{Decision: d, Event: parent.Events[d.EventID], Preview: local}
	s.Decisions[d.RequestID] = copy
	s.PendingAsks[d.RequestID] = PendingAsk{RequestID: d.RequestID, Control: ControlReset, Reset: &local, Resolved: true}
	_, err = e.commit(s, revision, input)
	return err
}

// RecordChildControl carries original child controls into the existing parent
// receipt qualification under the same native window. Checkpoint bytes stay frozen.
func (e *Engine) RecordChildControl(child *State, input Observation) error {
	e = e.observing(input)
	d, reset := child.resetDecision()
	authority := child.nodeContinuationAuthority()
	if !reset && authority == nil {
		return nil
	}
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	if s.SliceChildren[child.SliceChild.SliceID] != child.RunID {
		return validationError("child control has no current master claim")
	}
	changed := false
	if reset {
		if prior := s.SliceResetAuthorities[child.RunID]; prior.Decision.RequestID != d.RequestID {
			if s.SliceResetAuthorities == nil {
				s.SliceResetAuthorities = map[string]ResetAuthority{}
			}
			preview := child.PendingAsks[d.RequestID].Reset
			if preview == nil {
				return validationError("child reset preview is missing")
			}
			authority := ResetAuthority{Decision: d, Event: child.Events[d.EventID], Preview: *preview}
			if child.SliceChild.ParentReset != nil && child.SliceChild.ParentReset.Decision.RequestID == d.RequestID {
				authority = *child.SliceChild.ParentReset
			}
			s.SliceResetAuthorities[child.RunID] = authority
			changed = true
		}
	}
	if authority != nil {
		if receipt, known := s.SliceReceipts[child.RunID]; known && (receipt.Continuation == nil || receipt.Continuation.Decision.Revision < authority.Decision.Revision) {
			receipt.Continuation = authority
			s.SliceReceipts[child.RunID] = receipt
			changed = true
		}
	}
	if !changed {
		return nil
	}
	_, err = e.commit(s, revision, input)
	return err
}

func (s *State) ResetPending() bool { return s.resetPending() }

// A reset retires the prior requirement-work projection, while the immutable
// confirmation ledger still identifies original duties and result evidence.
// Preserved unfinished development is explicitly decided again by the reset
// confirmation; fresh validation comes from the route and candidate owners.
func (s *State) requirementWorkRevisionCurrent(revision uint64) bool {
	d, ok := s.resetDecision()
	return !ok || !d.Reset.Applied || revision > d.Revision
}

func (s *State) beforeResetBoundary(actionID string) bool {
	d, ok := s.resetDecision()
	if !ok || !d.Reset.Applied {
		return false
	}
	if result, ok := s.Results[actionID]; ok && result.Outcome == OutcomePass && (result.Validation == nil || result.Validation.Status != "CONTINUE") {
		return false
	}
	return s.issuedRevisionOf(actionID) <= d.Revision
}
