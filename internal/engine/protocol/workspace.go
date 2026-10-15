package protocol

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/vcs"
)

// AdmitPromotion applies the shared final validation qualification before
// opening or committing a promotion. Terminal and child checkpoints are closed.
func (s *State) AdmitPromotion(cd *compiler.CompiledDefinition) error {
	if s.Phase == runtime.PhaseTerminal || s.Phase == runtime.PhaseSliceReady {
		return &RejectedError{Code: CodeEventNotCurrent, Detail: "candidate promotion requires an active run"}
	}
	if s.requirementChangePending() {
		return &RejectedError{Code: CodeChangeBarrierActive, Detail: "candidate promotion is frozen while a requirement change ask is pending"}
	}
	if s.abortRequested() {
		return &RejectedError{Code: CodeEventNotCurrent, Detail: "candidate promotion requires a run without an abort decision"}
	}
	if !s.FinalValidationComplete(cd) {
		return &RejectedError{Code: CodeEventNotCurrent, Detail: "candidate promotion requires the final validation wave to be COMPLETE with current evidence for the complete development plan"}
	}
	return nil
}

// OpenPromotion signs the current verified source and the exact observed
// target prefix. A superseded responsibility remains pending until completion.
func (e *Engine) OpenPromotion(p PromotionParameters, input Observation) error {
	e = e.observing(input)
	revision, s, err := e.load()
	if err != nil {
		return err
	}
	if s == nil {
		return &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	if err := s.AdmitPromotion(e.cfg.Definition); err != nil {
		return err
	}
	source := s.currentValidationWave().Candidate
	if p.Source != source.Identity || p.SourceDigest != source.Digest || p.TargetBase != s.BaseSnapshot || p.TargetRoot == "" || s.NativeRoot != "" && p.TargetRoot != s.NativeRoot || p.TargetStart.Head == "" || p.TargetStart.Tree == "" || p.TargetStart.IndexTree == "" && p.TargetStart.PendingFingerprint == "" {
		return validationError("promotion requires the exact verified source, fixed target and native prefix")
	}
	if latest, pending := s.PromotionIntent(); pending {
		params, err := candidateParameters(latest)
		if err != nil {
			return err
		}
		if params.Promotion.Source == p.Source && params.Promotion.SourceDigest == p.SourceDigest && len(s.HostActionFailures[latest.ActionID]) == 0 {
			return nil
		}
		if latest.ActionID != p.SupersedesActionID {
			return validationError("promotion must account for its unresolved predecessor")
		}
		chain, err := s.PromotionChain(latest)
		if err != nil {
			return err
		}
		if params.Promotion.TargetRoot != p.TargetRoot || params.Promotion.TargetBase != p.TargetBase {
			return validationError("superseded promotion belongs to a different target")
		}
		for _, pending := range s.PendingHostActions {
			candidate, err := candidateParameters(pending)
			if err != nil || candidate.Promotion == nil {
				continue
			}
			if !slices.ContainsFunc(chain, func(original HostActionIntent) bool { return original.ActionID == pending.ActionID }) {
				return validationError("promotion has an unrelated unresolved writer")
			}
		}
	} else if p.SupersedesActionID != "" {
		return validationError("superseded promotion is not pending")
	}
	v := s.Validation
	params := CandidateParameters{Stage: "PROMOTION", Promotion: &p, WorkspaceID: p.SourceWorkspace, RequirementRevision: v.RequirementRevision, Generation: v.Generation, RouteScope: s.validationRouteDigest(), SourceTasks: slices.Clone(s.currentValidationWave().SourceTasks)}
	intent, err := e.prepareAdapterIntent(s, revision+1, "op.candidate", params, "")
	if err != nil {
		return err
	}
	s.PendingHostActions[intent.ActionID] = intent
	_, err = e.commit(s, revision, input)
	return err
}

// RecordCandidatePromotion submits a completed native proof through ordinary
// typed receipt admission. It does not write delivery facts directly.
func (e *Engine) RecordCandidatePromotion(actionID string, proof CandidatePromotionReceipt, input Observation) error {
	e = e.observing(input)
	_, s, err := e.load()
	if err != nil {
		return err
	}
	if s == nil {
		return &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	intent, ok := s.hostIntent(actionID)
	if !ok {
		return validationError("promotion requires its original intent")
	}
	evidence := CandidateEvidence{ParametersDigest: intent.PayloadDigest, WorkspaceID: proof.SourceWorkspace, Identity: proof.Final, Digest: proof.FinalDigest, Promotion: &proof}
	values, err := normalizeHostActionParams(evidence)
	if err != nil {
		return err
	}
	event, err := NewAdapterHostActionReceiptEvent(EventID("native:"+actionID), actionID, "op.candidate", s.RunProvider, intent.Correlation, intent.PayloadDigest, HostActionStatusExecuted, "", values)
	if err != nil {
		return err
	}
	_, err = e.Submit(event, input)
	return err
}

// SealTerminal seals an ordinary regular run after verified native delivery.
// A child cannot seal independently. Native identity enters from the facade;
// current validation, promotion, barrier and abort facts are checked here.
// A terminal replay with the same promoted identity performs no write.
func (e *Engine) SealTerminal(delivered string, input Observation) error {
	e = e.observing(input)
	delivered = strings.TrimSpace(delivered)
	revision, state, err := e.load()
	if err != nil {
		return err
	}
	if state == nil {
		return &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	if state.Route != "regular" || state.SliceChild != nil {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "terminal sealing requires an ordinary regular run; a slice child uses its recoverable checkpoint"}
	}
	if state.Phase == runtime.PhaseTerminal {
		if delivered == state.PromotedSnapshot {
			return nil
		}
		return &RejectedError{Code: CodeEventNotCurrent, Detail: "terminal sealing does not match the promoted snapshot"}
	}
	if err := state.errWriteBarrier("terminal sealing"); err != nil {
		return err
	}
	if state.abortRequested() {
		return &RejectedError{Code: CodeEventNotCurrent, Detail: "terminal sealing requires a run without an abort decision"}
	}
	if delivered == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "terminal sealing requires the delivered snapshot identity"}
	}
	if delivered != state.PromotedSnapshot {
		return &RejectedError{Code: CodeEventNotCurrent, Detail: fmt.Sprintf("native VCS identity %s does not match the promoted snapshot %s", delivered, state.PromotedSnapshot)}
	}
	if !state.DeliveryComplete(e.cfg.Definition) {
		return &RejectedError{Code: CodeEventNotCurrent, Detail: "terminal sealing requires the concluded validation pipeline"}
	}
	if state.pendingControl() {
		return validationError("terminal sealing requires all control asks resolved")
	}
	if state.nativeDebt() || len(state.PendingHostActions) != 0 || len(state.PendingActions) != 0 {
		return validationError("terminal sealing requires native and worker reconciliation")
	}
	for _, attempt := range state.EndedAttempts {
		if attempt.AwaitingResult {
			return validationError("terminal sealing requires worker reconciliation")
		}
	}
	if !state.ResourcesCleaned() {
		return validationError("terminal sealing requires native resource cleanup")
	}
	if err := state.TransitionPhase(runtime.PhaseTerminal); err != nil {
		return err
	}
	_, err = e.commit(state, revision, input)
	return err
}

// promotionPending 报告 validation 边界是否在等待终态晋升：最终 wave 已
// COMPLETE 且尚未绑定晋升快照。
func (s *State) promotionPending(cd *compiler.CompiledDefinition) bool {
	return s.FinalValidationComplete(cd) && !s.validationPromotionCurrent()
}

// AcceptedNativeSnapshot is the physical target accepted by native receipt
// admission. Current candidate and validation qualification are derived separately.
func (s *State) AcceptedNativeSnapshot() string {
	if s.CandidatePromotion != nil {
		return s.CandidatePromotion.Final
	}
	if reset, ok := s.resetDecision(); ok && reset.Reset.Applied {
		preview := s.PendingAsks[reset.RequestID].Reset
		if preview == nil && s.SliceChild != nil && s.SliceChild.ParentReset != nil {
			preview = &s.SliceChild.ParentReset.Preview
		}
		return preview.Current
	}
	return s.InitialSnapshot
}

// FinalValidationComplete derives delivery qualification from the current
// confirmed plan, complete source set and approved execution evidence. A
// checkpoint only releases later development.
func (s *State) FinalValidationComplete(cd *compiler.CompiledDefinition) bool {
	if !s.parentReviewReady() || !pipelineEnabled(s, cd) || !s.validationRequirementCurrent() || s.requirementChangePending() || s.Topology == nil || s.RouteMode == "" {
		return false
	}
	_, plan, _ := s.confirmedDevelopmentPlan()
	if (s.FlowNodeEnabled("development") && !s.mergeMaster() && (plan == nil || !s.developmentPlanCurrent(plan))) || s.abortRequested() || s.nextRepairWork() != nil {
		return false
	}
	for _, task := range s.planTasks(cd) {
		if s.developmentResult(task) == nil {
			return false
		}
	}
	w := s.currentValidationWave()
	if w == nil || w.Status != "COMPLETE" || w.RequirementRevision != s.Validation.RequirementRevision || w.Generation != s.Validation.Generation || w.RouteDigest != s.validationRouteDigest() || s.Validation.Candidate == nil || w.Candidate != *s.Validation.Candidate {
		return false
	}
	sources, _, _, complete := s.developmentSources(cd)
	return complete && sameTaskSet(w.SourceTasks, sources) && s.validateFinalCoverage() == nil && digestOfCanonical(w.Coverage) == digestOfCanonical(s.coverageContract(s.Validation.Candidate)) && s.validationEvidenceComplete(w)
}

func (s *State) validationPromotionCurrent() bool {
	w := s.currentValidationWave()
	p := s.CandidatePromotion
	return p != nil && p.Source == w.Candidate.Identity && p.SourceDigest == w.Candidate.Digest && p.TargetBase == s.BaseSnapshot && (s.NativeRoot == "" || p.TargetRoot == s.NativeRoot) && p.Final == s.PromotedSnapshot && p.FinalDigest == s.PromotedDigest && p.FinalDigest == w.Candidate.Digest && !s.nonCleanupNativeDebt()
}

// CandidatePromotionReceipt binds the observed source and final native trees.
// The signed candidate intent owns execution; this value retains its proof.
type PromotionObservation struct {
	Head               string       `json:"head"`
	Tree               string       `json:"tree"`
	PendingFingerprint string       `json:"pendingFingerprint,omitempty"`
	IndexTree          string       `json:"indexTree"`
	Changes            []vcs.Change `json:"changes,omitempty"`
	Clean              bool         `json:"clean"`
	StagedOnly         bool         `json:"stagedOnly"`
}

type PromotionParameters struct {
	SourceWorkspace    string               `json:"sourceWorkspace,omitempty"`
	Source             string               `json:"source"`
	SourceDigest       string               `json:"sourceDigest"`
	TargetRoot         string               `json:"targetRoot"`
	TargetBase         string               `json:"targetBase"`
	TargetStart        PromotionObservation `json:"targetStart"`
	SupersedesActionID string               `json:"supersedesActionId,omitempty"`
	Message            string               `json:"message"`
}

type CandidatePromotionReceipt struct {
	TargetStart        PromotionObservation `json:"targetStart"`
	SupersedesActionID string               `json:"supersedesActionId,omitempty"`
	OperationID        string               `json:"operationId"`
	Provider           string               `json:"provider"`
	SourceWorkspace    string               `json:"sourceWorkspace"`
	Source             string               `json:"source"`
	SourceDigest       string               `json:"sourceDigest"`
	TargetRoot         string               `json:"targetRoot"`
	TargetBase         string               `json:"targetBase"`
	Final              string               `json:"final"`
	FinalDigest        string               `json:"finalDigest"`
}

func (s *State) DeliveryComplete(cd *compiler.CompiledDefinition) bool {
	return s.FinalValidationComplete(cd) && s.validationPromotionCurrent()
}
func (s *State) nonCleanupNativeDebt() bool {
	for _, intent := range s.PendingHostActions {
		if intent.Adapter != nil && nativeOperation(intent.Adapter.Operation) && intent.Adapter.Operation != "op.cleanup" {
			return true
		}
	}
	return false
}

func (s *State) nativeDebt() bool {
	for _, intent := range s.PendingHostActions {
		if intent.Adapter != nil && nativeOperation(intent.Adapter.Operation) {
			return true
		}
	}
	return false
}

// retireNative preserves the original signed responsibility and its actual
// settlement evidence. It never fabricates a receipt or discards a failure.
func (s *State) retireNative(intent HostActionIntent, status, digest string, observation *HostActionObservation, revision uint64) {
	if _, pending := s.PendingHostActions[intent.ActionID]; !pending {
		return
	}
	effect := ReconciledEffect{UnknownReceipt: s.ReconciledEffects[intent.ActionID].UnknownReceipt, Intent: &intent, ActionID: intent.ActionID, Operation: intent.Operation, ObservationDigest: digest, Status: status, Revision: revision}
	if intent.Adapter != nil {
		effect.AdapterOperation = intent.Adapter.Operation
	}
	if observation != nil {
		effect.Promotion, effect.Resource = observation.Promotion, observation.Resource
	}
	if receipt, ok := s.HostActionReceipts[intent.ActionID]; ok && receipt.Status == HostActionStatusUnknown {
		effect.UnknownReceipt = &receipt
	}
	s.ReconciledEffects[intent.ActionID] = effect
	delete(s.PendingHostActions, intent.ActionID)
}

// validateCurrentPromotion is signing qualification; historical receipt
// authorization remains tied to the original complete validation wave.
func (s *State) validateCurrentPromotion(p CandidateParameters, cd *compiler.CompiledDefinition) error {
	if err := s.AdmitPromotion(cd); err != nil {
		return err
	}
	v, w := s.Validation, s.currentValidationWave()
	if p.Promotion == nil || p.RequirementRevision != v.RequirementRevision || p.Generation != v.Generation || p.RouteScope != s.validationRouteDigest() || !sameTaskSet(p.SourceTasks, w.SourceTasks) {
		return validationError("promotion differs from current validation qualification")
	}
	target := p.Promotion
	if target.Source != w.Candidate.Identity || target.SourceDigest != w.Candidate.Digest || target.TargetBase != s.BaseSnapshot || target.TargetRoot == "" || s.NativeRoot != "" && target.TargetRoot != s.NativeRoot || target.TargetStart.Head == "" || target.TargetStart.Tree == "" || target.TargetStart.IndexTree == "" && target.TargetStart.PendingFingerprint == "" || !target.TargetStart.Clean && !target.TargetStart.StagedOnly && !s.confirmedRequirementPromotionPrefix(target) {
		return validationError("promotion differs from current source and exact target prefix")
	}
	if s.NativeRoot != "" {
		r := s.NativeResources[target.SourceWorkspace]
		if p.WorkspaceID != r.ID || r.Role != "candidate" || r.Identity != target.Source || r.Status != "READY" || !s.resourceUsable(r.ID) {
			return validationError("promotion source is not a current registered workspace")
		}
	}
	if latest, pending := s.PromotionIntent(); pending {
		if latest.ActionID != target.SupersedesActionID {
			return validationError("promotion must bind its original predecessor")
		}
		old, err := candidateParameters(latest)
		if err != nil || old.Promotion.TargetRoot != target.TargetRoot || old.Promotion.TargetBase != target.TargetBase {
			return validationError("promotion predecessor belongs to another target")
		}
		if receipt, ok := s.HostActionReceipts[latest.ActionID]; ok && receipt.Status == HostActionStatusUnknown && candidateCorrelation(old) == candidateCorrelation(CandidateParameters{Stage: "PROMOTION", Promotion: target}) {
			return validationError("promotion predecessor is UNKNOWN")
		}
	} else if target.SupersedesActionID != "" {
		return validationError("promotion predecessor is not pending")
	}
	return nil
}

func (s *State) hostIntent(actionID string) (HostActionIntent, bool) {
	if intent, ok := s.PendingHostActions[actionID]; ok {
		return intent, true
	}
	effect, ok := s.ReconciledEffects[actionID]
	if ok && effect.Intent != nil {
		return *effect.Intent, true
	}
	return HostActionIntent{}, false
}
func (s *State) PromotionIntent() (HostActionIntent, bool) {
	var selected HostActionIntent
	found := false
	for _, intent := range s.PendingHostActions {
		p, err := candidateParameters(intent)
		if err != nil || p.Promotion == nil {
			continue
		}
		if !found || intent.Revision > selected.Revision {
			selected, found = intent, true
		}
	}
	return selected, found
}

// PromotionChain follows the signed predecessor identities for one target.
func (s *State) PromotionChain(intent HostActionIntent) ([]HostActionIntent, error) {
	first, err := candidateParameters(intent)
	if err != nil || first.Promotion == nil {
		return nil, validationError("promotion chain requires its original intent")
	}
	var chain []HostActionIntent
	for {
		p, err := candidateParameters(intent)
		if err != nil || p.Promotion == nil || p.Promotion.TargetRoot != first.Promotion.TargetRoot || p.Promotion.TargetBase != first.Promotion.TargetBase {
			return nil, validationError("promotion predecessor belongs to a different target")
		}
		chain = append(chain, intent)
		if p.Promotion.SupersedesActionID == "" {
			return chain, nil
		}
		previous, ok := s.hostIntent(p.Promotion.SupersedesActionID)
		if !ok || previous.Revision >= intent.Revision {
			return nil, validationError("promotion predecessor has no original earlier intent")
		}
		intent = previous
	}
}

func (s *State) promotionAuthorized(p CandidateParameters) bool {
	if p.Promotion == nil || s.Validation == nil {
		return false
	}
	for _, w := range s.Validation.Waves {
		if w.Status == "COMPLETE" && w.Generation == p.Generation && w.RequirementRevision == p.RequirementRevision && w.RouteDigest == p.RouteScope && w.Candidate.Identity == p.Promotion.Source && w.Candidate.Digest == p.Promotion.SourceDigest && sameTaskSet(w.SourceTasks, p.SourceTasks) && s.validationEvidenceComplete(&w) {
			return true
		}
	}
	return false
}

func (o PromotionObservation) Digest() string { return digestOfCanonical(o) }

func (s *State) pendingControl() bool {
	return s.hasPendingAsks() || s.nodeContinuationPending()
}

func (s *State) hasPendingAsks() bool {
	for _, ask := range s.PendingAsks {
		if s.PendingAskCurrent(ask) {
			return true
		}
	}
	return false
}

// nativeReplacementAuthorized requires the signed exact output or a confirmed
// work replacement. The caller supplies a genuinely completed current effect.
func (s *State) nativeReplacementAuthorized(old, current HostActionIntent, cd *compiler.CompiledDefinition) bool {
	if old.Adapter == nil || current.Adapter == nil || old.Adapter.Operation != current.Adapter.Operation {
		return false
	}
	if old.PayloadDigest == current.PayloadDigest && old.Correlation == current.Correlation {
		return true
	}
	switch old.Adapter.Operation {
	case "op.workspace":
		if old.Revision >= current.Revision {
			return false
		}
		var before, after WorkspaceParameters
		a, _ := json.Marshal(old.Adapter.Params)
		b, _ := json.Marshal(current.Adapter.Params)
		if json.Unmarshal(a, &before) != nil || json.Unmarshal(b, &after) != nil {
			return false
		}
		if before.Task != nil && after.Task != nil {
			replacement, authorized := s.requirementWorkReplacement(*before.Task)
			if authorized && replacement != *before.Task && replacement == *after.Task && SameWorkspaceInput(s.NativeResources[before.Resource.ID], before.Resource) && SameWorkspaceInput(s.taskResource(replacement, cd), after.Resource) {
				return true
			}
		}
		for _, binding := range s.DispatchBindings {
			if binding.WorkspaceID != before.Resource.ID {
				continue
			}
			replacement, authorized := s.requirementWorkReplacement(binding.Task)
			if authorized && s.taskResource(replacement, cd).ID == after.Resource.ID {
				return true
			}
		}
	case "op.candidate":
		before, aerr := candidateParameters(old)
		after, berr := candidateParameters(current)
		if aerr != nil || berr != nil || before.Promotion != nil || after.Promotion != nil || before.Stage != after.Stage || len(before.SourceTasks) == 0 {
			return false
		}
		if candidateCorrelation(before) == candidateCorrelation(after) && before.WorkspaceID == after.WorkspaceID && s.candidateContextCurrent(before) && s.candidateContextCurrent(after) {
			return true
		}
		if old.Revision >= current.Revision {
			return false
		}
		for _, source := range before.SourceTasks {
			replacement, authorized := s.requirementWorkReplacement(source)
			if !authorized || !slices.Contains(after.SourceTasks, replacement) {
				return false
			}
		}
		for kind, task := range before.DesignTasks {
			replacement, authorized := s.requirementWorkReplacement(task)
			if task != after.DesignTasks[kind] && (!authorized || replacement != after.DesignTasks[kind]) {
				return false
			}
		}
		return true
	}
	return false
}

func (s *State) confirmedRequirementPromotionPrefix(p *PromotionParameters) bool {
	if p.TargetStart.Head != s.AcceptedNativeSnapshot() || len(p.TargetStart.Changes) != 1 || p.TargetStart.Changes[0].Path != s.RequirementArtifactPath() || p.TargetStart.Changes[0].Kind != "M" {
		return false
	}
	return true
}
