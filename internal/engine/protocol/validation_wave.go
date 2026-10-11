package protocol

import (
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
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
				task := decision.RequiredTask{Task: runtime.TaskKey{Node: step.Header.NodeID, Step: step.Header.ID, Scope: intent.ActionID}}
				if _, executable := s.ExecutableHostActions(cd)[intent.ActionID]; !executable || !s.NativeActionCurrent(intent, cd) {
					task.BlockedBy = decision.WaitDependencies
				}
				view.RequiredTasks[step.Header.ID] = append(view.RequiredTasks[step.Header.ID], task)
			}
		}
	}

	if fact := s.candidateFailureFact(cd); fact != nil {
		view.OperatorFacts = append(view.OperatorFacts, *fact)
	}
	// Accepted abort retires ordinary delivery obligations; native failure
	// facts above remain authoritative until their effects are reconciled.
	if s.abortRequested() || !s.dualReviewPass() {
		return
	}
	message := ""
	// 开发计划提示只对普通 run 成立：普通 run 的计划经路线决定引用
	// readiness 提案（无待确认提案即提示）；child 的片内计划随路线决定内
	// 联确认（development_plan.go），选完路线即进入需求文档/波次边界。
	developmentPlanPending := func() bool {
		_, p, _ := s.confirmedDevelopmentPlan()
		return p == nil || !s.developmentPlanCurrent(p)
	}
	if s.Topology != nil && s.RouteMode == routeModeMerge && s.Validation != nil && s.Validation.DevelopmentSnapshot == "" {
		// split 绑定锁定合并路线：保留总任务实例不涉常规路线与开发计划，
		// 停等 children（合并门＋合并 QA 属收尾）。
		ready, expected := s.sliceReceiptProgress()
		message = fmt.Sprintf("the retained master run waits for slice receipts: %d/%d accepted", ready, expected)
		if ready == expected {
			message = ""
		}
	} else if s.SliceChild != nil && s.RouteMode == "" {
		message = "select the slice route (the topology is inherited from the master)"
	} else if s.SliceChild == nil && (s.Topology == nil || s.RouteMode == "") {
		message = "confirm topology and select route"
	} else if s.SliceChild == nil && !s.mergeMaster() && developmentPlanPending() {
		message = "confirm the current readiness development plan with the route"
	} else if !s.validationRequirementCurrent() {
		message = "submit confirmed REQUIREMENT document via OPERATOR_OBSERVATION.validation"
	} else if w := s.currentValidationWave(); w != nil {
		switch w.Status {
		case "ADJUDICATING":
			message = "submit per-finding ADJUDICATE observations"
			if s.validationFindingsAdjudicated(w) && len(s.validationObligations(w)) > 0 {
				message = "validation obligations remain unresolved; request VALIDATION_DETAIL_DISPOSITION with a justified legal remedy"
			}
		case "COMPLETE":
			if s.promotionPending(cd) {
				message = "organize the final candidate with engine candidate promotion before terminal completion"
			}
		}
	}
	if s.validationRequirementCurrent() && s.Validation.DevelopmentSnapshot != "" && s.Validation.Candidate == nil {
		frozen := true
		for _, mode := range s.validationKinds() {
			if !s.caseSetReady(mode) {
				frozen = false
			}
		}
		if frozen {
			if err := s.validateFinalCoverage(); err != nil {
				message = err.Error()
			}
		}
	}
	if s.validationRequirementCurrent() && s.currentValidationWave() != nil {
		if err := s.validateCoverageRelations(); err != nil {
			message = err.Error()
		} else if waiting := s.executionScopePending(); waiting != "" {
			message = waiting
		} else if waiting := s.qaOperationPending(cd); waiting != "" {
			message = waiting
		} else if waiting := s.gateImpactPending(); waiting != "" {
			message = waiting
		}
	}
	if message != "" {
		view.OperatorFacts = append(view.OperatorFacts, decision.Fact{Source: decision.SourceReceipt, Key: "validation", Value: message})
	}
}

func (e *Engine) advanceValidation(s *State, nextRevision uint64) (changed bool, err error) {
	defer func() {
		if err == nil {
			retired, retireErr := e.retireSupersededObservations(s, nextRevision)
			changed, err = changed || retired, retireErr
		}
	}()
	if s.abortRequested() {
		return e.advanceAbort(s, nextRevision)
	}
	if s.resetPending() {
		return e.advanceReset(s, nextRevision)
	}
	if !s.parentReviewReady() || s.SliceChild != nil && s.Phase == runtime.PhaseSliceReady {
		return false, nil
	}
	changed = e.ensureRetryAsks(s, nextRevision)
	changed = s.reconcileValidationRemedyAsks(nextRevision) || changed
	// token cap 到达＝预算边界：落账 DISPATCH_BUDGET_DISPOSITION Ask（零签发由
	// 签发闸执行、Ask 经本收尾单点）。cap 属派发预算包络——包络未就位（受理
	// 前）不产生该边界；无配置时 cap 维度不参与判定。
	if e.guardEngaged(s) && e.tokenCapExceeded(s) {
		changed = s.ensureDispatchBudgetAsk(budgetDispositionTarget, nextRevision) || changed
	}
	if made, err := e.advanceCandidateFailures(s, nextRevision); err != nil {
		return changed, err
	} else if made {
		return true, nil
	}
	if !pipelineEnabled(s, e.cfg.Definition) || !s.validationRequirementCurrent() || s.requirementChangePending() || s.Topology == nil || s.RouteMode == "" {
		return changed, nil
	}
	if s.validationConcluded(e.cfg.Definition) {
		return changed, nil
	}
	for _, ask := range s.PendingAsks {
		if !ask.Resolved && ask.Validation != nil && ask.Validation.Kind == "VALIDATION_DETAIL_DISPOSITION" {
			changed = s.ensureValidationAsk(*ask.Validation, nextRevision) || changed
		}
	}
	if made, err := e.advanceQAOperations(s, nextRevision); err != nil || made {
		return changed || made, err
	}
	if made, err := e.advanceRepairComparisons(s, nextRevision); err != nil || made {
		return changed || made, err
	}
	if made, err := e.advanceDevelopmentChecks(s, nextRevision); err != nil || made {
		return changed || made, err
	}
	if made, err := e.advanceWorkspaces(s, nextRevision); err != nil || made {
		return changed || made, err
	}
	v := s.Validation
	changed = s.carryCaseBindings() || changed
	if prior := s.currentValidationWave(); v.Candidate != nil && prior != nil && prior.Generation != v.Generation {
		sources, _, _, complete := s.developmentSources(e.cfg.Definition)
		if complete && s.validateFinalCoverage() == nil {
			changed = e.freezeValidationWave(s, prior.Scope, "", sources) || changed
		}
	}
	if w := s.currentValidationWave(); w != nil && w.RouteDigest != s.validationRouteDigest() && w.Status != "INVALIDATED" && w.Status != "REPAIRED" {
		e.rebindValidationRoute(s)
		changed = true
	}
	for _, mode := range s.validationKinds() {
		set := v.Sets[mode]
		if set == nil {
			continue
		}
		changed = advanceCaseReview(set) || changed
		// A frozen blackbox set retires its design-role projection; its resource
		// remains registered until native cleanup proves removal.
		if mode == coverage.ReviewBlackbox && set.Status == "FROZEN" && s.QAWorktree != "" {
			s.QAWorktree = ""
			changed = true
		}
		if s.caseSetWaiver(mode) == "" && (set.Status == "DRAFT" || set.Status == "THAWED" || set.Status == "REVIEW") && s.prewaveBlocked(mode, "engine.qa.review.rationale.v2", e.cfg.Definition) {
			changed = s.ensureValidationAsk(ValidationRequest{Kind: "QA_REVIEW", Mode: mode, Target: s.qaReviewTarget(mode)}, nextRevision) || changed
		}
	}
	w := s.currentValidationWave()
	changed = s.resolveValidationProblems(w, e.cfg.Definition) || changed
	if w != nil && w.Status == "COLLECTING" {
		changed = s.carryRequirementTargets(w) || changed
		complete := true
		for _, t := range w.Targets {
			if t.Status != "PASS" && t.Status != "FAIL" && t.Status != "DISPOSED" && t.Status != "AUTHORIZED_SKIP" {
				complete = false
			}
		}
		if complete {
			w.Status = "ADJUDICATING"
			changed = true
		}
	}
	for i := range v.Waves {
		settled, err := e.settleValidationWave(s, &v.Waves[i])
		if err != nil {
			return changed, err
		}
		changed = settled || changed
	}
	changed = s.ensureRepairGates(e.cfg.Definition) || changed
	if s.validationRepairPending(w) && s.repairCompleted(w, e.cfg.Definition) {
		w.Status = "REPAIRED"
		changed = true
	}
	if p, ready := s.nextEditSource(e.cfg.Definition); ready {
		for _, intent := range s.PendingHostActions {
			if intent.Adapter != nil && intent.Adapter.Operation == "op.candidate" && intent.Correlation == candidateCorrelation(p) {
				return changed, nil
			}
		}
		intent, err := e.prepareAdapterIntent(s, nextRevision, "op.candidate", p, "")
		if err != nil {
			return changed, err
		}
		s.PendingHostActions[intent.ActionID] = intent
		return true, nil
	}
	if _, ready := s.repairCandidateParameters(e.cfg.Definition); ready {
		return e.advanceIntegration(s, nextRevision)
	}
	if work := s.nextRepairWork(); work != nil {
		if s.validationDispositionBlocked(work.wave) {
			return changed, nil
		}
		if s.completedValidationWaves() >= automaticValidationWaveLimit && !s.repairExtraAuthorized(work) && !work.wave.Repair.PureP2 && !work.pureP2 {
			changed = s.ensureValidationAsk(ValidationRequest{Kind: "REPAIR", Target: work.wave.ID}, nextRevision) || changed
		}
		if s.repairReady(work) && s.developmentEditResult(work.task) == nil {
			return changed, nil
		}
	}
	if _, _, _, complete := s.developmentSources(e.cfg.Definition); !complete {
		if _, integration := s.integrationParameters(e.cfg.Definition); integration {
			return e.advanceIntegration(s, nextRevision)
		}
		return changed, nil
	}
	for _, intent := range s.PendingHostActions {
		if intent.Adapter != nil && intent.Adapter.Operation == "op.candidate" && s.candidateInputsCurrent(intent, e.cfg.Definition) {
			return changed, nil
		}
	}
	set := v.Sets[coverage.ReviewWhitebox]
	if s.NativeRoot == "" && set != nil && set.Status == "REVIEW" && set.Candidate == "" && len(set.Paths) == 0 {
		set.Candidate = v.DevelopmentSnapshot
		changed = true
	}
	params, ready, err := s.currentCandidateParameters(e.cfg.Definition)
	if err != nil || !ready {
		return changed, err
	}
	if s.NativeRoot != "" {
		if params.Stage == "QA_ARTIFACT" {
			base := s.resource("candidate", params.Base, "ownership:"+candidateCorrelation(params)+":"+strconv.FormatUint(s.Validation.Generation, 10))
			if r, ok := s.NativeResources[base.ID]; !ok || r.Status != "READY" || s.ResourceCleanupPending(base.ID) {
				made, err := e.ensureWorkspace(s, base, nextRevision)
				return changed || made, err
			}
		}
		r := s.candidateResource(params, e.cfg.Definition)
		if registered, ok := s.NativeResources[r.ID]; !ok || registered.Status != "READY" || !s.resourceUsable(r.ID) {
			made, err := e.ensureWorkspace(s, r, nextRevision)
			return changed || made, err
		}
	}
	intent, err := e.prepareAdapterIntent(s, nextRevision, "op.candidate", params, "")
	if err != nil {
		return changed, err
	}
	s.PendingHostActions[intent.ActionID] = intent
	return true, nil
}

func (s *State) validateFinalCoverage() error {
	if s.routeHasNoQAModes() {
		return nil
	}
	if err := s.coverageContract(s.Validation.Candidate).Validate(); err != nil {
		return err
	}
	for _, mode := range s.validationKinds() {
		if s.caseSetWaiver(mode) != "" {
			continue
		}
		set := s.Validation.Sets[mode]
		if set == nil || set.Review == nil {
			return fmt.Errorf("QA %s has no complete review", mode)
		}
		c := modeCoverageContract(s.coverageContract(s.Validation.Candidate), mode)
		if _, err := c.ValidateReviews([]coverage.ReviewResult{*set.Review}); err != nil {
			return err
		}
	}
	return s.validateCoverageRelations()
}

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

// validateCandidateReceipt checks the completed operation independently of
// whether its input still applies, before any requirement barrier can hold it.
func validateCandidateReceipt(provider string, intent HostActionIntent, receipt HostActionReceipt) (CandidateParameters, CandidateEvidence, error) {
	var params CandidateParameters
	data, _ := json.Marshal(intent.Adapter.Params)
	if err := json.Unmarshal(data, &params); err != nil {
		return params, CandidateEvidence{}, err
	}
	evidence, err := decodeCandidateEvidence(receipt)
	if err != nil {
		return params, CandidateEvidence{}, err
	}
	if evidence.WorkspaceID != params.WorkspaceID || evidence.ParametersDigest != intent.PayloadDigest || evidence.Identity == "" || evidence.Digest == "" {
		return params, CandidateEvidence{}, validationError("candidate receipt lacks exact intent digest and native identity/digest")
	}
	expected := slices.Clone(params.Paths)
	actual := slices.Clone(evidence.Paths)
	slices.Sort(expected)
	slices.Sort(actual)
	if !slices.Equal(expected, actual) {
		return params, CandidateEvidence{}, validationError("candidate receipt does not account for every declared path")
	}
	for id, view := range evidence.ReviewViews {
		if view.TestRef == "" || params.ReviewCases[id] != view.TestRef || len(view.Inputs) == 0 {
			return params, CandidateEvidence{}, validationError("case review view must bind an issued test and contain its input evidence")
		}
		for path, digest := range view.Inputs {
			if strings.TrimSpace(path) == "" || strings.TrimSpace(digest) == "" {
				return params, CandidateEvidence{}, validationError("case review view requires nonempty input paths and content digests")
			}
		}
	}
	if evidence.GateView != nil && params.Stage != "REVIEW_READY" && !(params.Stage == "DEVELOPMENT" && params.RepairTask != nil) {
		return params, CandidateEvidence{}, validationError("gate ownership evidence belongs to an issued final or repair source range")
	}
	switch params.Stage {
	case "PROMOTION":
		p, proof := params.Promotion, evidence.Promotion
		if p == nil || proof == nil || proof.OperationID != intent.ActionID || proof.Provider != provider || proof.SourceWorkspace != p.SourceWorkspace || proof.Source != p.Source || proof.SourceDigest != p.SourceDigest || proof.TargetRoot != p.TargetRoot || proof.TargetBase != p.TargetBase || proof.Final != evidence.Identity || proof.FinalDigest != evidence.Digest || proof.FinalDigest != p.SourceDigest || proof.SupersedesActionID != p.SupersedesActionID || digestOfCanonical(proof.TargetStart) != digestOfCanonical(p.TargetStart) {
			return params, CandidateEvidence{}, validationError("promotion receipt lacks the signed native source, target prefix and final mapping")
		}
	case "MERGE_START", "MERGE_FINISH":
		if err := sValidateIntegrationReceipt(params, evidence); err != nil {
			return params, CandidateEvidence{}, err
		}
	case "DEVELOPMENT", "AUTHORING", "CHECK_SOURCE":
	case "QA_ARTIFACT":
		// The isolated immutable artifact supplies the original independent
		// review. Test-only judgment is admitted when that review is accepted.
	case "REVIEW_READY":
		if len(params.QAArtifacts) > 0 && (!evidence.TestOnly || !evidence.ProductionViewEqual) {
			return params, CandidateEvidence{}, validationError("final QA artifact requires bound independent review and exact production comparison")
		}
		if strings.TrimSpace(params.ReviewBase) == "" || evidence.GateView == nil {
			return params, CandidateEvidence{}, validationError("final candidate requires its native review base and complete gate ownership view")
		}
		owned := map[string]bool{}
		for _, path := range evidence.GateView.TestOwnedPaths {
			if strings.TrimSpace(path) == "" || owned[path] {
				return params, CandidateEvidence{}, validationError("gate ownership view requires unique nonempty paths")
			}
			owned[path] = true
		}
		if !params.Finalize {
			return params, CandidateEvidence{}, validationError("final candidate must include commit organization before validation")
		}
	default:
		return params, CandidateEvidence{}, validationError("unknown candidate stage")
	}
	return params, evidence, nil
}

// applyCandidateReceipt settles completed receipts through the same
// applicability check; superseded output remains an observed external fact.
func (e *Engine) applyCandidateReceipt(s *State, intent HostActionIntent, receipt HostActionReceipt) error {
	if intent.Adapter == nil || intent.Adapter.Operation != "op.candidate" {
		return nil
	}
	params, evidence, err := validateCandidateReceipt(s.NativeVCS(), intent, receipt)
	if err != nil {
		return err
	}
	if params.Stage == "PROMOTION" {
		return e.applyPromotionReceipt(s, intent, params, evidence, receipt)
	}
	if effect := s.ReconciledEffects[intent.ActionID]; effect.Status == "SUPERSEDED" || effect.Status == "WITHDRAWN" {
		return nil
	}
	v := s.Validation
	if !s.nativeEffectApplicable(intent, e.cfg.Definition) {
		return nil
	}
	switch params.Stage {
	case "MERGE_START", "MERGE_FINISH":
		if params.Stage == "MERGE_FINISH" {
			r := s.NativeResources[params.WorkspaceID]
			r.Identity = evidence.Identity
			s.NativeResources[r.ID] = r
		}
		return nil
	case "CHECK_SOURCE":
		r := s.NativeResources[params.WorkspaceID]
		r.Identity = evidence.Identity
		s.NativeResources[r.ID] = r
		return nil
	case "DEVELOPMENT":
		if params.RepairTask != nil {
			r := s.NativeResources[params.WorkspaceID]
			r.Identity = evidence.Identity
			s.NativeResources[r.ID] = r
			if work := s.repairMember(*params.RepairTask); work != nil && len(s.repairMembers(work.wave)) == 1 {
				v.DevelopmentSnapshot = evidence.Identity
			}
			return nil
		}
		if v.DevelopmentSnapshot != "" {
			return nil
		}
	case "AUTHORING":
		if v.AuthoringWorkspace != "" {
			return nil
		}
	case "QA_ARTIFACT":
		if v.Sets[coverage.ReviewWhitebox].Candidate != "" {
			return nil
		}
	case "REVIEW_READY":
		if v.Candidate != nil {
			return nil
		}
	}
	if params.WorkspaceID != "" {
		r := s.NativeResources[params.WorkspaceID]
		r.Identity = evidence.Identity
		s.NativeResources[r.ID] = r
	}
	switch params.Stage {
	case "DEVELOPMENT":
		v.DevelopmentSnapshot = evidence.Identity
	case "AUTHORING":
		v.AuthoringWorkspace = evidence.Identity
	case "QA_ARTIFACT":
		v.Sets[coverage.ReviewWhitebox].Candidate = evidence.Identity
	case "REVIEW_READY":
		if err := s.validateFinalCoverage(); err != nil {
			return validationReject(err)
		}
		v.Candidate = &coverage.ValidationCandidate{Identity: evidence.Identity, Digest: evidence.Digest}
		scope := coverage.ScopeFull
		if prior := s.currentValidationWave(); prior != nil {
			if prior.Status == "REPAIRED" {
				scope = coverage.ScopeAffected
			} else if prior.Generation != v.Generation && prior.Candidate == *v.Candidate {
				scope = prior.Scope
			}
		}
		e.freezeValidationWave(s, scope, "", params.SourceTasks)
	}
	return nil
}
func (e *Engine) freezeValidationWave(s *State, scope coverage.ScopeMode, authorization string, sources []runtime.TaskKey) bool {
	if s.validateCoverageRelations() != nil {
		return false
	}
	v := s.Validation
	versions := map[coverage.ReviewKind]string{}
	for _, mode := range s.validationKinds() {
		versions[mode] = v.Sets[mode].Version
	}
	w := ValidationWave{SourceTasks: slices.Clone(sources), AuthorizationRequest: authorization, Candidate: *v.Candidate, RequirementRevision: v.RequirementRevision, Generation: v.Generation, RouteDigest: s.validationRouteDigest(), Versions: versions, Status: "COLLECTING", Scope: scope, Targets: map[string]*ValidationTarget{}}
	if _, plan, _ := s.confirmedDevelopmentPlan(); plan != nil {
		if tasks, checkpoint := s.planCheckpoint(e.cfg.Definition, plan); checkpoint {
			w.CheckpointTasks = tasks
		}
	}
	w.Coverage = s.coverageContract(v.Candidate)
	for _, mode := range s.validationKinds() {
		if v.Sets[mode].Review != nil {
			w.Reviews = append(w.Reviews, *v.Sets[mode].Review)
		}
	}
	// Retrying an obligation keeps its TaskKey and frozen expected set.
	w.ID = digestOfCanonical(struct {
		Candidate     coverage.ValidationCandidate
		Versions      map[coverage.ReviewKind]string
		Route         string
		Scope         coverage.ScopeMode
		Generation    uint64
		Authorization string
		Checkpoint    []runtime.TaskKey
		Sources       []runtime.TaskKey
	}{w.Candidate, versions, w.RouteDigest, scope, v.Generation, authorization, w.CheckpointTasks, w.SourceTasks})
	w.ID = strings.TrimPrefix(w.ID, "sha256:")
	prior := s.currentValidationWave()
	if prior != nil && prior.Generation != w.Generation && prior.Candidate != w.Candidate {
		e.retireValidationWave(s, prior.ID, "candidate changed after requirement confirmation")
	}
	if prior != nil {
		if prior.Candidate == w.Candidate {
			w.ExtraAuthorized = prior.ExtraAuthorized
		}
	}

	for _, gate := range s.SelectedGates {
		mode := coverage.ReviewKind("")
		switch gate {
		case "blackbox":
			mode = coverage.ReviewBlackbox
		case "whitebox":
			mode = coverage.ReviewWhitebox
		case mergeQAGateID:
			mode = coverage.ReviewMerge
		}
		target := &ValidationTarget{Status: "PENDING", Gate: gate, Kind: "GATE"}
		handler := "engine.gate.v2"
		part := gate
		if mode != "" {
			handler = "engine.qa.execution.v2"
			part = string(mode)
			target.Mode = mode
			target.Kind = "QA"
			target.Gate = ""
		}
		target.Task = s.validationRequiredTask(pipelineTask(e.cfg.Definition, handler, w.ID+":"+part), &w, e.cfg.Definition)
		if prior != nil {
			if prior.Generation != w.Generation {
				s.carryRequirementTarget(target, prior, &w)
			} else if prior.Candidate == w.Candidate && prior.RouteDigest != w.RouteDigest {
				s.carryRouteTarget(target, prior, &w)
			}
			if mode == "" && target.Status == "PENDING" {
				s.carryGateResult(target, prior, &w, e.cfg.Definition)
			}
		}
		if prior != nil && authorization == "" {
			s.carryExecutionScope(target, prior, &w)
		}
		if mode != "" {
			if request := s.caseSetWaiver(mode); request != "" {
				target.Status = "AUTHORIZED_SKIP"
				target.DispositionReason = s.Decisions[request].Validation.Reason
			}
		}
		w.Targets[target.Task.String()] = target
	}
	s.addChildValidationTargets(&w, e.cfg.Definition)
	w.ExpectedDigest = validationExpectedDigest(&w)
	v.Waves = append(v.Waves, w)
	return true
}

// The frozen expected set includes the current candidate and every native
// child reuse proof. Result status and execution progress remain mutable.
func validationExpectedDigest(w *ValidationWave) string {
	reuse := map[string]*ValidationReuse{}
	for key, target := range w.Targets {
		if target.Reuse != nil {
			reuse[key] = target.Reuse
		}
	}
	return digestOfCanonical(struct {
		Candidate coverage.ValidationCandidate
		Versions  map[coverage.ReviewKind]string
		Route     string
		Targets   []string
		Scope     coverage.ScopeMode
		Reuse     map[string]*ValidationReuse `json:"reuse,omitempty"`
	}{w.Candidate, w.Versions, w.RouteDigest, sortedTargetKeys(w), w.Scope, reuse})
}

// A disposition applies to its candidate. P2 completion can carry only genuine
// passes or failures whose verified problems were covered by that repair.
func (s *State) carryGateResult(target *ValidationTarget, prior, current *ValidationWave, cd *compiler.CompiledDefinition) {
	if slices.Contains(s.requiredWorkTasks(), target.Task) {
		return
	}
	for _, previous := range prior.Targets {
		if !sameTarget(previous, target) {
			continue
		}
		sourceWave, responsibility := s.targetResponsibility(prior, previous)
		if prior.Generation != current.Generation && !s.validationWorkCarryAuthorized(responsibility.ResultAction, responsibility.Task) {
			return
		}
		if prior.Candidate == current.Candidate && (previous.GateImpact != nil || previous.Status == "PASS" || previous.Status == "FAIL" || previous.Status == "DISPOSED" || previous.Status == "AUTHORIZED_SKIP") {
			s.carryValidationTarget(target, previous, current)
			return
		}
		repairs, repairOnly := s.candidateRepairChanges(prior, current)
		if !repairOnly || len(repairs) == 0 {
			return
		}
		for _, repair := range repairs {
			if !repair.wave.Repair.PureP2 || !s.repairCompleted(repair.wave, cd) {
				return
			}
		}
		if previous.Status != "PASS" && previous.Status != "FAIL" {
			return
		}
		covered := false
		var completion *WorkerResult
		for _, finding := range s.Validation.Findings {
			if s.validationFindingTarget(sourceWave, finding) != responsibility || len(finding.History) == 0 {
				continue
			}
			resolution := finding.History[len(finding.History)-1]
			if resolution.Decision == "DISMISSED" || resolution.Severity == "P3" {
				continue
			}
			if resolution.Severity != "P2" {
				return
			}
			var matched *WorkerResult
			for _, repair := range repairs {
				if repair.result != nil && repair.wave.Repair.PureP2 && slices.Contains(repair.wave.Repair.Problems, resolution.ProblemID) {
					matched = repair.result
					break
				}
			}
			if matched == nil {
				return
			}
			if completion == nil || matched.ActionID < completion.ActionID {
				completion = matched
			}
			covered = true
		}
		if covered {
			target.Status, target.ResultAction = "PASS", completion.ActionID
			return
		}
		if previous.Status == "PASS" && !s.mergeMaster() {
			target.GateImpact = &GateImpact{SourceWave: prior.ID}
		}
		return
	}
}

func (e *Engine) adjudicateValidation(s *State, subject string, items []FindingResolution, revision uint64) error {
	w := s.validationWave(subject)
	if w == nil || !slices.Contains([]string{"ADJUDICATING", "REPAIR", "REPAIRED", "COMPLETE"}, w.Status) {
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
		if item.Reason == "" || item.Requirement == "" || item.NormalPath == "" || item.Module == "" || item.RootCause == "" || item.Consequence == "" || item.Verification.Scope == "" || item.Verification.Causality == "" || item.Verification.Reproduction == "" || item.Verification.Classification == "" {
			return validationError("each finding needs independent dimensions and verification")
		}
		if f.ExecutionFailure != nil {
			if len(s.executionFailureRecords(f)) == 0 {
				return validationError("execution failure does not reference actual failed case evidence")
			}
			if item.Severity != "" || item.SeverityRationale != "" || item.Verification.Severity != "" {
				return validationError("case execution failures do not have severity")
			}
		} else if !slices.Contains([]string{"P0", "P1", "P2", "P3"}, item.Severity) || item.SeverityRationale == "" || item.Verification.Severity == "" {
			return validationError("finding needs effective severity, rationale and severity verification")
		}
		switch item.Decision {
		case "NEW":
			if item.ProblemID != "" {
				return validationError("NEW problem identity is assigned by the engine")
			}
			item.ProblemID = FindingProblemID(*f)
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
		item.AcceptedRevision = revision
		f.History = append(f.History, item)
	}
	return nil
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
	return w != nil && (s.validationRepairPending(w) || w.Status == "ADJUDICATING" && len(s.validationObligations(w)) > 0 && s.validationFindingsAdjudicated(w))
}

func (e *Engine) settleValidationWave(s *State, w *ValidationWave) (bool, error) {
	if w == nil {
		return false, nil
	}
	// Settlement belongs to the current adjudicating projection or an unfinished
	// original repair. Replacement tasks retain their source wave as evidence.
	currentProjection := w == s.currentValidationWave() && w.Generation == s.Validation.Generation && w.RequirementRevision == s.Validation.RequirementRevision && (w.Status == "ADJUDICATING" || w.Status == "REPAIR" || w.Status == "COMPLETE")
	pending := s.validationRepairPending(w) && !s.repairCompleted(w, e.cfg.Definition)
	if !currentProjection && !pending || !s.validationFindingsAdjudicated(w) {
		return false, nil
	}
	problems := []string{}
	pureP2 := true
	for _, obligation := range s.validationObligationDetails(w) {
		if obligation.id == obligation.target.Task.String() {
			continue
		}
		if !slices.Contains(problems, obligation.id) {
			problems = append(problems, obligation.id)
		}
		pureP2 = pureP2 && obligation.pureP2
	}
	slices.Sort(problems)
	if len(problems) == 0 {
		changed := false
		if s.validationRepairPending(w) {
			for _, work := range s.repairMembers(w) {
				if err := s.retireRepairWork(work, "candidate repair obligations were explicitly waived"); err != nil {
					return false, err
				}
			}
			w.Status = "ADJUDICATING"
			changed = true
		}
		if !s.validationEvidenceComplete(w) || s.validateCoverageRelations() != nil {
			return changed, nil
		}
		if w.Status == "COMPLETE" {
			return changed, nil
		}
		if w.Repair == nil {
			w.Counted = s.validationWaveCountable(w)
		}
		w.Status = "COMPLETE"
		return true, nil
	}
	// A candidate's signed plan is immutable across projections of its wave.
	// New obligations still need disposition when that plan can no longer run.
	if s.signedCandidateRepair(w) != nil {
		if w.Status == "COMPLETE" {
			w.Status = "ADJUDICATING"
			return true, nil
		}
		return false, nil
	}

	task := pipelineTask(e.cfg.Definition, "engine.repair.v3", w.Candidate.Identity)
	// Native identities may contain separators; the logical snapshot key uses
	// its content digest, never a platform path embedded in TaskKey syntax.
	task.Scope = strings.TrimPrefix(digestOfCanonical(w.Candidate), "sha256:")
	current := s.replacementFold(task)
	_, issued := s.repairIssue(current)
	requeued := issued == 0 && s.developmentResultRecord(current) == nil && s.repairTaskFinished(current)
	if requeued {
		// New findings continue the unique unsigned plan. No issued execution or
		// ended retry is reopened; the waiver decision retains its original scope.
		s.Tasks[current] = runtime.TaskQueued
	}
	changed := requeued || w.Status != "REPAIR" || w.Repair == nil || !slices.Equal(w.Repair.Problems, problems) || w.Repair.PureP2 != pureP2
	if !changed {
		return false, nil
	}
	w.Counted = w.Counted || s.validationWaveCountable(w)
	w.Status = "REPAIR"
	w.Repair = &ValidationRepair{Task: task, Problems: problems, PureP2: pureP2}
	return changed, nil
}

// Validation counting follows the review-ready snapshot, not execution scope.
func (s *State) validationWaveCountable(w *ValidationWave) bool {
	var baseline *ValidationWave
	for i := range s.Validation.Waves {
		prior := &s.Validation.Waves[i]
		if prior.ID == w.ID {
			break
		}
		if prior.Candidate != w.Candidate || !sameTaskSet(prior.SourceTasks, w.SourceTasks) {
			baseline = prior
		}
	}
	repairs, repairOnly := s.candidateRepairChanges(baseline, w)
	if len(w.CheckpointTasks) > 0 && len(repairs) == 0 {
		return false
	}
	pureP2 := repairOnly && len(repairs) > 0
	for _, work := range repairs {
		pureP2 = pureP2 && work.wave.Repair.PureP2
	}
	if pureP2 {
		for _, obligation := range s.validationObligationDetails(w) {
			if !obligation.pureP2 {
				pureP2 = false
				break
			}
		}
	}
	if pureP2 {
		return false
	}

	for i := range s.Validation.Waves {
		prior := &s.Validation.Waves[i]
		if prior.ID == w.ID {
			break
		}
		if prior.Candidate == w.Candidate && prior.Counted {
			return false
		}

	}
	return true
}

func (e *Engine) applyPromotionReceipt(s *State, intent HostActionIntent, params CandidateParameters, evidence CandidateEvidence, receipt HostActionReceipt) error {
	if effect := s.ReconciledEffects[intent.ActionID]; effect.Status == "SUPERSEDED" || effect.Status == "WITHDRAWN" {
		return nil
	}
	if !s.promotionAuthorized(params) {
		return validationError("promotion receipt has no originally qualified validation source")
	}
	p := evidence.Promotion
	chain, err := s.PromotionChain(intent)
	if err != nil {
		return err
	}
	for _, previous := range chain[1:] {
		s.retireNative(previous, "SUPERSEDED", receipt.Digest, nil, intent.Revision)
	}
	if s.CandidatePromotion != nil {
		if latest, ok := s.hostIntent(s.CandidatePromotion.OperationID); ok && latest.Revision > intent.Revision {
			return nil
		}
	}
	s.CandidatePromotion = p
	s.PromotedSnapshot, s.PromotedDigest = p.Final, p.FinalDigest
	for _, step := range e.cfg.Definition.Steps {
		if payload, ok := step.Payload.(compiler.CompiledHostActionStep); ok && payload.Operation == "op.candidate" && !slices.Contains(s.Completed, step.Header.ID) {
			if err := s.CompleteStep(step.Header.ID, e.cfg.Definition); err != nil {
				return err
			}
		}
	}
	return nil
}
