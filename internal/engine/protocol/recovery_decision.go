package protocol

import (
	"fmt"

	"formal-gates/internal/engine/runtime"
)

func (s *State) recoveryRequest(request string) (RecoveryRecord, bool) {
	for _, record := range s.RecoveryRecords {
		if record.Action == RecoveryAsk && record.RequestID == request {
			return record, true
		}
	}
	return RecoveryRecord{}, false
}

// recoveryAttempt keeps authorization on the original, still-owed execution.
func (e *Engine) recoveryAttempt(s *State, record RecoveryRecord) (PendingAction, Attempt, bool) {
	pending, exists := s.PendingActions[record.ActionID]
	attempt := s.Attempts[pending.Task]
	if !exists {
		attempt = s.EndedAttempts[record.Task]
		if !attempt.AwaitingResult {
			return PendingAction{}, Attempt{}, false
		}
		pending = PendingAction{ActionID: attempt.ActionID, Task: attempt.Task, Step: string(attempt.Step), AttemptID: attempt.ID}
	}
	if pending.ActionID != record.ActionID || attempt.ID != record.AttemptID || attempt.ActionID != record.ActionID {
		return PendingAction{}, Attempt{}, false
	}
	if attempt.Business && !s.endedWorkCurrent(attempt, e.cfg.Definition) {
		return PendingAction{}, Attempt{}, false
	}
	return pending, attempt, true
}

// advanceRecoveries applies original routes and user decisions through the
// same native host effects. Their durable receipts, not a second progress flag,
// determine whether the original session can resume or be replaced.
func (e *Engine) advanceRecoveries(s *State, revision uint64) error {
	for index, record := range s.RecoveryRecords {
		action := record.Action
		if record.RequestID == "" && action == RecoveryAsk && !s.abortRequested() && s.Phase != runtime.PhaseTerminal {
			if _, _, current := e.recoveryAttempt(s, record); current && s.registerRecoveryAsk(&record, revision) {
				s.RecoveryRecords[index].RequestID = record.RequestID
			}
		}
		if record.RequestID == "" || (action != RecoveryAsk && action != RecoveryResumeAttempt && action != RecoveryNewAttempt) {
			continue
		}
		pending, attempt, current := e.recoveryAttempt(s, record)
		if action == RecoveryAsk {
			ask := s.PendingAsks[record.RequestID]
			if !current && !ask.Resolved {
				ask.Resolved = true
				s.PendingAsks[record.RequestID] = ask
			}
			d, decided := s.Decisions[record.RequestID]
			if !decided {
				continue
			}
			switch d.Choice {
			case "resume":
				action = RecoveryResumeAttempt
			case "fresh":
				action = RecoveryNewAttempt
			default:
				continue
			}
		}
		if !current {
			continue
		}
		correlation := record.RequestID
		if err := e.applyAttemptRecovery(s, pending, attempt, action, correlation, revision); err != nil {
			return err
		}
	}
	return nil
}

// accountResumeExecution 把一次恢复执行计入目标任务的 Attempt 账本：resumed
// execution 的唯一计数点（公开 ResumeAgent 与恢复决定共用），Attempts 达到
// MaxAttempts 时置耗尽标记——与瞬态结果/FAILED spawn 的计数同一形状。目标身
// 份用与恢复准入同一个解析（workerActionTarget）；计数写进承载该条目的账本
// （当前账或已终待果账，按条目身份定位，TASK_PROGRESS TERMINAL 清理后的执行
// 照常计数）。
func (s *State) accountResumeExecution(workerActionID string) {
	pending, attempt, ok := s.workerActionTarget(workerActionID)
	if !ok {
		return
	}
	if current, held := s.Attempts[pending.Task]; held && current.ID == attempt.ID {
		current.Attempts++
		if current.MaxAttempts > 0 && current.Attempts >= current.MaxAttempts {
			current.RetryExhausted = true
		}
		s.Attempts[pending.Task] = current
		return
	}
	if ended, held := s.EndedAttempts[pending.Task]; held && ended.ID == attempt.ID {
		ended.Attempts++
		if ended.MaxAttempts > 0 && ended.Attempts >= ended.MaxAttempts {
			ended.RetryExhausted = true
		}
		s.EndedAttempts[pending.Task] = ended
	}
}

func (e *Engine) applyAttemptRecovery(s *State, pending PendingAction, attempt Attempt, action RecoveryAction, correlation string, revision uint64) error {
	// 显式换新决定走 terminate 路线（用户指定的路线，不是预算边界）；resume
	// 类动作的 retry 边界由统一预算判定拦截并落 typed Ask，不再静默转 terminate。
	operation := HostActionResumeAgent
	if action == RecoveryNewAttempt {
		operation = HostActionTerminateAgent
	}
	// An effect already signed for this recovery owns both its operation and cost.
	for _, intent := range s.PendingHostActions {
		if intent.Correlation == correlation {
			return nil
		}
	}
	var executed bool
	for _, receipt := range s.HostActionReceipts {
		if receipt.Correlation != correlation {
			continue
		}
		if receipt.Status != HostActionStatusExecuted || receipt.LifecycleEvidence == nil {
			return nil
		}
		operation = receipt.Operation
		executed = true
	}
	if !executed {
		for _, history := range s.HostActionFailures {
			for _, receipt := range history {
				if receipt.Correlation == correlation {
					return nil
				}
			}
		}
	}
	if executed && operation == HostActionResumeAgent {
		return nil
	}
	receipt, known := s.SpawnReceipts[pending.ActionID]
	if !known || receipt.Status == SpawnStatusUnknown {
		return nil
	}
	identity := receipt.agentIdentity()
	if !executed && (operation == HostActionResumeAgent || !s.hasLifecycleEvent(identity, LifecycleStop)) {
		if operation == HostActionResumeAgent && e.guardEffectBudget(s, pending.ActionID, revision) != nil {
			// 预算边界＝Ask 已落账（本函数运行在事务收尾内，随外层提交），
			// 效果不创建、返回 nil——Ask 即进展，不是错误。
			return nil
		}
		reason := "Recover original attempt by " + string(action)
		intent, err := e.agentHostIntent(s, operation, pending.ActionID, identity, reason, correlation, revision)
		if err != nil {
			return err
		}
		s.PendingHostActions[intent.ActionID] = intent
		return nil
	}
	// Fresh execution follows observed termination and retains the original
	// finite policy and binding when the normal retry owner signs a replacement.
	// 一次宿主效果执行的统一预算判定（retry 余量＋token cap 两维，与公开
	// ResumeAgent 同一函数）；治理任务的换新仍经派发 guard（与 issueInto 漏斗
	// 同一单任务判定）。
	if e.guardEffectBudget(s, pending.ActionID, revision) != nil {
		return nil
	}
	if s.validationRole(pending.Task, e.cfg.Definition) != "" && e.guardEngaged(s) {
		basis, err := e.guardBasis(s)
		if err != nil {
			return &RejectedError{Code: CodeGuardBlockedBug, Detail: err.Error()}
		}
		if err := e.guardTaskAuthorized(s, basis, pending.Task); err != nil {
			return err
		}
	}
	attempt.Attempts++
	attempt.RetryExhausted = attempt.Attempts >= attempt.MaxAttempts
	attempt.AwaitingResult = false
	if attempt.Business {
		s.endBusinessAttempt(pending, attempt, fmt.Sprintf("interrupted execution resolved by %s", correlation), revision)
		return nil
	}
	retired, _ := s.retireAction(pending.ActionID, "", "interrupted execution resolved by "+correlation)
	if attempt.RetryExhausted {
		s.EndedAttempts[pending.Task] = attempt
		s.Tasks[pending.Task] = runtime.TaskQueued
		return nil
	}
	replacement, err := s.installReplacement(retired, revision, attempt.Bindings, attempt.Plan, attempt.Attempts, attempt.MaxAttempts)
	if err != nil {
		return err
	}
	replacement.Work, replacement.PreviousAction = attempt.Work, attempt.ActionID
	s.Attempts[pending.Task] = replacement
	e.bindBusinessAttempt(s, pending.Task)
	obsolete := s.ObsoleteActions[pending.ActionID]
	obsolete.ReplacedBy = replacement.ActionID
	s.ObsoleteActions[pending.ActionID] = obsolete
	return nil
}
