package protocol

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/coverage"
)

// allowanceFace 把三值可用性投影成摘要字段值：engine allowance 读权威
// Guard 投影（AllowanceSummary，nil＝包络未就位）；owner cost 与 token cap
// 按各自来源/配置独立判定（owner 终态差值只有可核对来源才 AVAILABLE；token
// cap 三值读 tokenCapStatus 单点派生，不在此重述规则）。
func (s *State) allowanceFaces() (engineAllowance, ownerCost, tokenCap string) {
	ownerCost = "UNAVAILABLE"
	if s.Cost != nil && s.Cost.Owner != nil && s.Cost.Owner.Source == cost.SourceTranscript {
		ownerCost = "AVAILABLE"
	}
	return s.Guard.AllowanceSummary(), ownerCost, s.tokenCapStatus()
}

// repairAskFace 是 REPAIR Ask 摘要的专用 Detail：完整 findings/problems（含
// 逐条理由/归并与历史裁决）、执行失败、QA scope（当前 wave 的逐 target 状态与
// 必跑用例）、已用/剩余轮数、dispatch signature/TaskKey 与需求/快照/wave 上下文。
type repairAskFace struct {
	RequirementRevision string
	DevelopmentSnapshot string
	Wave                string
	WaveScope           string
	Repair              *RepairInput
	Targets             []repairAskTarget
	WavesUsed           int
	RemainingAutomatic  int
	TaskKey             string
	DispatchActionID    string
}

type repairAskTarget struct {
	Task          string
	Kind          string
	Mode          string
	Gate          string
	Status        string
	RequiredCases []string
}

func (s *State) validationRequestBasis(r ValidationRequest) json.RawMessage {
	var detail any
	switch r.Kind {
	case "RETRY":
		if intent, ok := s.PendingHostActions[r.Target]; ok && intent.Adapter != nil && nativeOperation(intent.Adapter.Operation) {
			_, _, max := s.candidateFailureRoute(intent)
			detail = struct {
				Intent      HostActionIntent
				Failures    []HostActionReceipt
				MaxAttempts int
			}{intent, s.candidateFailures(intent.Correlation), max}
			break
		}
		for key, a := range s.EndedAttempts {
			if key.String() == r.Target {
				var set *CaseSet
				if s.Validation != nil {
					set = s.Validation.Sets[r.Mode]
				}
				detail = struct {
					Attempt   Attempt
					Rejection RejectedResult
					CaseSet   *CaseSet
				}{a, s.RejectedResults[a.ActionID], set}
			}
		}
	case "QA_REVIEW":
		if s.Validation != nil {
			detail = s.Validation.Sets[r.Mode]
		}
	case "REPAIR":
		detail = s.repairAskDetail(r)
	case KindDispatchBudgetDisposition:
		// 预算处置摘要：绑定 basis digest 与额度/owner/token cap 三面（各自
		// 来源独立判定）；state revision 绑定由 Ask 自身的 StateRevision 与
		// DECIDE 三方相等校验持有，不进内容摘要。
		engineAllowance, ownerCost, tokenCap := s.allowanceFaces()
		detail = struct {
			Kind                       string
			Target                     string
			BasisDigest                string
			EngineAllowance, OwnerCost string
			TokenCap                   string
		}{r.Kind, r.Target, s.Guard.AllowanceSummary(), engineAllowance, ownerCost, tokenCap}
	default:
		detail = s.Validation
	}
	r.Digest = ""
	r.StateRevision = 0
	r.Summary = nil
	engineAllowance, ownerCost, tokenCap := s.allowanceFaces()
	value, _ := json.Marshal(struct {
		Request                              ValidationRequest
		Kind, Target                         string
		Mode                                 coverage.ReviewKind
		Detail                               any
		EngineAllowance, OwnerCost, TokenCap string
	}{r, r.Kind, r.Target, r.Mode, detail, engineAllowance, ownerCost, tokenCap})
	return value
}

// repairAskDetail 展开当前返修责任的完整授权摘要；请求的 wave 必须由
// nextRepairWork 持有。
func (s *State) repairAskDetail(r ValidationRequest) *repairAskFace {
	work := s.nextRepairWork()
	if work == nil || work.wave.ID != r.Target {
		return nil
	}
	remaining := automaticValidationWaveLimit - s.completedValidationWaves()
	if remaining < 0 {
		remaining = 0
	}
	face := &repairAskFace{
		RequirementRevision: s.RequirementRevision(),
		Wave:                work.wave.ID,
		WaveScope:           string(work.wave.Scope),
		Repair:              s.repairInput(work.task),
		WavesUsed:           s.completedValidationWaves(),
		RemainingAutomatic:  remaining,
		TaskKey:             work.task.String(),
	}
	if s.Validation != nil {
		face.DevelopmentSnapshot = s.Validation.DevelopmentSnapshot
		for _, key := range sortedTargetKeys(work.wave) {
			target := work.wave.Targets[key]
			face.Targets = append(face.Targets, repairAskTarget{
				Task: target.Task.String(), Kind: target.Kind, Mode: string(target.Mode),
				Gate: target.Gate, Status: target.Status, RequiredCases: target.RequiredCases,
			})
		}
	}
	if attempt, ok := s.Attempts[work.task]; ok {
		face.DispatchActionID = attempt.ActionID
	}
	if attempt, ok := s.EndedAttempts[work.task]; ok && face.DispatchActionID == "" {
		face.DispatchActionID = attempt.ActionID
	}
	return face
}

// rebindValidationAsks binds every unresolved validation Ask to the committed
// revision and its decision basis after dispatch and allowance projection.
func rebindValidationAsks(s *State, revision uint64) {
	s.reconcileRepairAsks()
	for id, ask := range s.PendingAsks {
		if ask.Resolved || ask.Validation == nil {
			continue
		}
		summary := s.validationRequestBasis(*ask.Validation)
		ask.Validation.StateRevision = revision
		ask.Validation.Summary = summary
		ask.Validation.Digest = digestOfCanonical(summary)
		s.PendingAsks[id] = ask
	}
}

// reconcileRepairAsks retires requests outside the current repair responsibility.
// Request evidence remains intact; retirement grants no decision or task outcome.
func (s *State) reconcileRepairAsks() bool {
	work := s.nextRepairWork()
	changed := false
	for id, ask := range s.PendingAsks {
		r := ask.Validation
		if ask.Resolved || r == nil || r.Kind != "REPAIR" {
			continue
		}
		if work != nil && r.Target == work.wave.ID && r.Binding.Task == work.task && !s.repairExtraAuthorized(work) {
			continue
		}
		ask.Resolved = true
		s.PendingAsks[id] = ask
		changed = true
	}
	return changed
}

func (s *State) ensureValidationAsk(r ValidationRequest, nextRevision uint64) bool {
	var options []AskOption
	r, options = s.validationOffer(r)
	r.Summary = s.validationRequestBasis(r)
	r.Digest = digestOfCanonical(r.Summary)
	r.StateRevision = nextRevision
	var superseded []string
	for id, a := range s.PendingAsks {
		if a.Control != ControlValidation || a.Validation == nil || a.Validation.Kind != r.Kind || a.Validation.Target != r.Target {
			continue
		}
		if _, decided := s.Decisions[id]; decided && a.Validation.Digest == r.Digest && r.Kind != "VALIDATION_DETAIL_DISPOSITION" {
			return false
		}
		if a.Resolved {
			continue
		}
		if a.Validation.Digest == r.Digest {
			return false
		}
		superseded = append(superseded, id)
	}
	for _, id := range superseded {
		a := s.PendingAsks[id]
		a.Resolved = true
		s.PendingAsks[id] = a
	}
	id := fmt.Sprintf("validation:%s:%s:%d", r.Kind, r.Target, nextRevision)
	s.PendingAsks[id] = PendingAsk{RequestID: id, Control: ControlValidation, Validation: &r, Options: options}
	return true
}

// reconcileWorkerRetryAsks retires offers whose original duty has ended.
// Decisions and failure evidence remain immutable; host retries retain their
// separate intent owner.
func (e *Engine) reconcileWorkerRetryAsks(s *State) bool {
	current := map[string]bool{}
	for key, a := range s.EndedAttempts {
		if s.endedWorkCurrent(a, e.cfg.Definition) && a.RetryExhausted && !a.AwaitingResult {
			current[key.String()] = true
		}
	}
	changed := false
	for id, ask := range s.PendingAsks {
		r := ask.Validation
		if ask.Resolved || r == nil || r.Kind != "RETRY" || current[r.Target] {
			continue
		}
		if intent, ok := s.PendingHostActions[r.Target]; ok && intent.Adapter != nil && nativeOperation(intent.Adapter.Operation) {
			continue
		}
		ask.Resolved = true
		s.PendingAsks[id] = ask
		changed = true
	}
	return changed
}

func (e *Engine) ensureRetryAsks(s *State, nextRevision uint64) bool {
	changed := e.reconcileWorkerRetryAsks(s)
	for key, a := range s.EndedAttempts {
		if s.endedWorkCurrent(a, e.cfg.Definition) && a.RetryExhausted && !a.AwaitingResult {
			request := ValidationRequest{Kind: "RETRY", Target: key.String()}
			if a.Work != nil && a.Work.WaveID == "" && (s.validationRole(key, e.cfg.Definition) == "engine.qa.design" || s.validationRole(key, e.cfg.Definition) == "engine.qa.review.rationale") {
				request.Mode = s.validationMode(key)
			}
			changed = s.ensureValidationAsk(request, nextRevision) || changed
		}
	}
	return changed
}

// Pending disposition suspends execution without replacing an already issued
// Attempt. Request and decision records remain the authority for that pause.
func (s *State) validationDispositionBlocked(w *ValidationWave) bool {
	if w == nil {
		return false
	}
	var latest RecordedDecision
	for id, ask := range s.PendingAsks {
		if ask.Control != ControlValidation || ask.Validation == nil || ask.Validation.Kind != "VALIDATION_DETAIL_DISPOSITION" || ask.Validation.Target != w.ID {
			continue
		}
		if !ask.Resolved {
			return true
		}
		if decision := s.Decisions[id]; decision.Revision > latest.Revision {
			latest = decision
		}
	}
	if latest.Revision != 0 {
		return latest.Choice != "approve"
	}
	return false
}

func (s *State) validationDispatchBlocked(action string) bool {
	binding, ok := s.DispatchBindings[action]
	if !ok {
		return false
	}
	for _, work := range s.repairWork() {
		if work.task == binding.Task {
			return s.validationDispositionBlocked(work.wave)
		}
	}
	return false
}

func (e *Engine) acceptValidationRequest(s *State, r *ValidationRequest, nextRevision uint64) (*ValidationRequest, error) {
	if r == nil || s.Validation == nil {
		return nil, validationError("validation request needs an active pipeline and typed payload")
	}
	if r.Kind != "THAW" && r.Kind != "QA_ARTIFACT_REPAIR" && r.Kind != "DISPOSE" && r.Kind != "QA_RETRY" && r.Kind != "VALIDATION_DETAIL_DISPOSITION" {
		return nil, validationError("retry and extra-round offers are generated from engine obligations")
	}
	copy := *r
	if copy.Kind == "QA_RETRY" {
		w := s.currentValidationWave()
		if w == nil || w.Status == "INVALIDATED" || w.Status == "REPAIRED" {
			return nil, validationError("QA retry requires the current fixed validation candidate")
		}
		copy.Target = w.ID
		copy.Binding = WorkBinding{RequirementRevision: w.RequirementRevision, Generation: w.Generation, Candidate: w.Candidate.Identity, WaveID: w.ID}
	}
	copy, _ = s.validationOffer(copy)
	copy.Summary = s.validationRequestBasis(copy)
	copy.Digest = digestOfCanonical(copy.Summary)
	copy.StateRevision = nextRevision
	if copy.Kind == "THAW" || copy.Kind == "QA_ARTIFACT_REPAIR" || copy.Kind == "VALIDATION_DETAIL_DISPOSITION" && slices.Contains(copy.Remedies, "QA_ARTIFACT_REPAIR") {
		if !slices.Contains([]string{"REQUIREMENT_MEANING", "EXTERNAL_BEHAVIOR", "SUCCESS_FAILURE_CRITERIA", "CASE_EXECUTABILITY"}, copy.Category) {
			return nil, validationError("THAW category is not a stable semantic/executability change")
		}
		if s.Validation.Sets[copy.Mode] == nil {
			return nil, validationError("unknown case-set")
		}
	}
	if copy.Kind == "VALIDATION_DETAIL_DISPOSITION" {
		wave := s.currentValidationWave()
		if !s.validationDispositionReady(wave) || wave.ID != copy.Target || len(copy.Remedies) < 2 {
			return nil, validationError("detail disposition requires adjudicated findings and multiple concrete remedies")
		}
		copy.Binding.WaveID = wave.ID
		copy.Binding.Candidate = wave.Candidate.Identity
		available := s.validationObligations(wave)
		if len(copy.Obligations) == 0 {
			copy.Obligations = available
		}
		for _, id := range copy.Obligations {
			if !slices.Contains(available, id) {
				return nil, validationError("detail remedy names an obligation outside this wave")
			}
		}
		seen := map[string]bool{}
		for _, remedy := range copy.Remedies {
			if seen[remedy] || !slices.Contains([]string{"REPAIR", "QA_ARTIFACT_REPAIR", "REQUEST_REQUIREMENT_CHANGE", "DISMISS"}, remedy) {
				return nil, validationError("invalid or duplicate remedy")
			}
			if remedy == "REPAIR" && !s.candidateRepairPending(wave) {
				return nil, validationError("implementation repair requires current executable work")
			}
			seen[remedy] = true
		}
	}
	if copy.Kind == "DISPOSE" {
		w := s.currentValidationWave()
		if w == nil || w.Status != "COLLECTING" {
			return nil, validationError("error disposition requires a collecting wave")
		}
		t := w.Targets[copy.Target]
		if t == nil || (t.Status != "RUNTIME_ERROR" && t.Status != "RETRY_EXHAUSTED") {
			return nil, validationError("only an unresolved runtime/invalid-result target can be disposed")
		}
	}
	copy.Summary = s.validationRequestBasis(copy)
	copy.Digest = digestOfCanonical(copy.Summary)
	return &copy, nil
}
func (e *Engine) acceptValidationDecision(s *State, ask PendingAsk, p *DecidePayload, revision, nextRevision uint64) error {
	if ask.Control != ControlValidation || ask.Validation == nil {
		return validationError("decision is not for a validation request")
	}
	if p.Choice == "decline" {
		if p.Validation != nil {
			return validationError("decline has no typed action")
		}
		return nil
	}
	r := ask.Validation
	d := p.Validation
	if d == nil || strings.TrimSpace(d.Reason) == "" || d.StateRevision != revision || r.StateRevision != revision || d.Digest != r.Digest || digestOfCanonical(s.validationRequestBasis(*r)) != r.Digest {
		return validationError("authorization needs the current state revision, exact summary digest and non-empty reason")
	}
	if p.Choice == "requirement" {
		id := fmt.Sprintf("requirement:%s:%d", r.Target, nextRevision)
		s.PendingAsks[id] = PendingAsk{RequestID: id, Control: ControlRequestRequirementChange, BaseRevision: s.RequirementRevision(), BaseGeneration: s.requirementGeneration(), Options: []AskOption{{ID: requirementProceedOption, Label: "Confirm revised requirement"}, {ID: requirementAbortOption, Label: "Keep current requirement"}}}
		return nil
	}
	if p.Choice == "abort" {
		return nil
	}
	if p.Choice == "retry_qa" || r.Kind == "QA_RETRY" {
		return e.retryValidationQA(s, ask, d)
	}
	if p.Choice == "waive" {
		return e.waiveValidation(s, ask, d)
	}
	switch r.Kind {
	case KindDispatchBudgetDisposition:
		// 固定四选项（decline/abort 已在通用分支处理）：continue＝一次有限
		// 继续授权（budgetContinueGranted 从本决定账目推导，不累积）；retry＝
		// 目标逻辑动作一次性追加一个 retry slot；skip＝仅留痕不解锁。
		switch p.Choice {
		case "continue", "skip":
			return nil
		case "retry":
			return s.extendBudgetRetry(r.Target)
		}
		return validationError("budget disposition accepts only continue, retry, skip or abort")
	case "RETRY":
		if intent, ok := s.PendingHostActions[r.Target]; ok && intent.Adapter != nil && nativeOperation(intent.Adapter.Operation) {
			route, used, max := s.candidateFailureRoute(intent)
			if (route.Action != RecoveryAsk && (route.Action != RecoveryResumeAttempt || used < max)) || s.candidateRetryGrantTarget(intent.Correlation) != "" {
				return validationError("candidate retry is not an unused exhausted responsibility")
			}
			return nil
		}
		if p.Choice == "redesign" {
			set := s.Validation.Sets[r.Mode]
			if set == nil || set.DesignTask != r.Binding.Task {
				return validationError("redesign source is no longer current")
			}
			for key := range s.EndedAttempts {
				if key.String() == r.Target {
					delete(s.EndedAttempts, key)
				}
			}
			set.reopen(nil)
			return nil
		}
		found := false
		for key, a := range s.EndedAttempts {
			if key.String() == r.Target {
				if !s.endedWorkCurrent(a, e.cfg.Definition) || !a.RetryExhausted || s.retryExtensionGranted(key.String()) {
					return validationError("retry already extended or not exhausted")
				}
				a.MaxAttempts++
				a.RetryExhausted = false
				s.EndedAttempts[key] = a
				found = true
			}
		}
		if !found {
			return validationError("retry obligation no longer exists")
		}
	case "QA_REVIEW":
		set := s.Validation.Sets[r.Mode]
		if set == nil || r.Target != s.qaReviewTarget(r.Mode) {
			return validationError("QA review obligation no longer exists")
		}
		if p.Choice == "redesign" {
			set.reopen(nil)
		} else if p.Choice == "approve" {
			set.Status = "REVIEW"
			if len(set.ReviewHistory) > 0 {
				set.Candidate = set.ReviewHistory[len(set.ReviewHistory)-1].Candidate
			}
		} else {
			return validationError("unsupported pre-wave remedy")
		}
	case "REPAIR":
		work := s.nextRepairWork()
		if work == nil || work.wave.ID != r.Target || work.task != r.Binding.Task || s.repairExtraAuthorized(work) {
			return validationError("repair offer is no longer current")
		}
		if work.task == work.wave.Repair.Task {
			work.wave.ExtraAuthorized = true
		}
	case "THAW", "QA_ARTIFACT_REPAIR":
		set := s.Validation.Sets[r.Mode]
		if set == nil {
			return validationError("case-set missing")
		}
		if err := reopenCases(set, r.AffectedCases); err != nil {
			return err
		}
		set.Status = "THAWED"
		for _, work := range s.repairWork() {
			if work.result == nil {
				if err := s.retireRepairWork(work, "authorized QA artifact repair"); err != nil {
					return err
				}
			}
		}
		s.Validation.Candidate = nil
		if w := s.currentValidationWave(); w != nil {
			w.Status = "INVALIDATED"
			e.retireValidationWave(s, w.ID, "authorized QA artifact repair")
		}
	case "DISPOSE":
		copy := *d
		copy.Obligations = []string{r.Target}
		return e.waiveValidation(s, ask, &copy)
	case "VALIDATION_DETAIL_DISPOSITION":
		if !slices.Contains(r.Remedies, d.Remedy) {
			return validationError("selected remedy is not in the bound offer")
		}
		w := s.currentValidationWave()
		if !s.validationDispositionReady(w) || w.ID != r.Target {
			return validationError("detail obligation is no longer current")
		}
		switch d.Remedy {
		case "REPAIR":
			if !s.candidateRepairPending(w) {
				return validationError("implementation repair requires current executable work")
			}
		case "QA_ARTIFACT_REPAIR":
			set := s.Validation.Sets[r.Mode]
			if err := reopenCases(set, r.AffectedCases); err != nil {
				return err
			}
			set.Status = "THAWED"
			s.Validation.Candidate = nil
			w.Status = "INVALIDATED"
			e.retireValidationWave(s, w.ID, "authorized QA artifact repair")
		case "REQUEST_REQUIREMENT_CHANGE":
			id := fmt.Sprintf("requirement:%s:%d", w.ID, nextRevision)
			s.PendingAsks[id] = PendingAsk{RequestID: id, Control: ControlRequestRequirementChange, BaseRevision: s.RequirementRevision(), BaseGeneration: s.requirementGeneration(), Options: []AskOption{{ID: requirementProceedOption, Label: "Confirm revised requirement"}, {ID: requirementAbortOption, Label: "Keep current requirement"}}}
		case "DISMISS":
			copy := *d
			if len(copy.Obligations) == 0 {
				copy.Obligations = slices.Clone(r.Obligations)
			}
			return e.waiveValidation(s, ask, &copy)
		}

	default:
		return validationError("unknown validation control")
	}
	return nil
}
func (e *Engine) retireValidationWave(s *State, id, reason string) {
	for action, b := range s.DispatchBindings {
		if b.WaveID == id {
			if s.liveDispatch(action) {
				s.retireAction(action, "", reason)
			}
			delete(s.EndedAttempts, b.Task)
		}
	}
}
