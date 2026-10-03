package protocol

import (
	"fmt"
	"slices"
	"strings"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/runtime"
)

// RecordQAWorktree 把已通过 facade 校验（worktree resolve==BaseSnapshot＋
// 隔离区内需求产物修订核对）的 QA 隔离工作区登记为持久事实（镜像 legacy
// RegisterQAWorktree 的落点；物理建区归宿主，engine 只持登记）。重复登记同
// 一工作区幂等；每轮黑盒设计可重建工作区，换路径覆盖登记。
func (e *Engine) RecordQAWorktree(worktree, expectedFingerprint string) error {
	worktree = strings.TrimSpace(worktree)
	revision, state, err := e.load()
	if err != nil {
		return err
	}
	if state == nil {
		return &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	if state.BaseSnapshot == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "QA worktree registration requires a verified base snapshot"}
	}
	if worktree == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "QA worktree path is required"}
	}
	if state.QAWorktree == worktree {
		return nil
	}
	state.QAWorktree = worktree
	_, err = e.commit(state, revision, expectedFingerprint)
	return err
}

// admitPromotion 是候选晋升两段（意图 OpenPromotion／提交
// RecordCandidatePromotion）共用的接纳判定：run 在成功收口之前（TERMINAL 与
// SLICE_READY 拒）、无未决
// 需求变化屏障（Phase A 停止 candidate promotion）、无 abort、当前 wave 存在
// 且 Status==COMPLETE（晋升是解除 promotion-wait 边界的动作，验证收集期不存
// 在可晋升的终候选）。
func (s *State) admitPromotion() error {
	if s.Phase == runtime.PhaseTerminal || s.Phase == runtime.PhaseSliceReady {
		return &RejectedError{Code: CodeEventNotCurrent, Detail: "candidate promotion requires an active run"}
	}
	if s.requirementChangePending() {
		return &RejectedError{Code: CodeChangeBarrierActive, Detail: "candidate promotion is frozen while a requirement change ask is pending"}
	}
	if s.abortRequested() {
		return &RejectedError{Code: CodeEventNotCurrent, Detail: "candidate promotion requires a run without an abort decision"}
	}
	if w := s.currentValidationWave(); w == nil || w.Status != "COMPLETE" {
		return &RejectedError{Code: CodeEventNotCurrent, Detail: "candidate promotion requires the final validation wave to be COMPLETE"}
	}
	return nil
}

// OpenPromotion 落账候选晋升的执行前意图（intent→execute→observe→commit 的
// intent 段）：准入在先——被拒时调用方不得执行任何 Git 副作用。无意图＝幂等
// 记录；同 target 重开幂等；不同 target 覆盖（前次执行已被超越）。
func (e *Engine) OpenPromotion(target, expectedFingerprint string) error {
	target = strings.TrimSpace(target)
	revision, state, err := e.load()
	if err != nil {
		return err
	}
	if state == nil {
		return &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	if err := state.admitPromotion(); err != nil {
		return err
	}
	if target == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "candidate promotion intent requires the target snapshot identity"}
	}
	if state.PendingPromotion == target {
		return nil
	}
	state.PendingPromotion = target
	_, err = e.commit(state, revision, expectedFingerprint)
	return err
}

// RecordCandidatePromotion 提交候选晋升（intent→execute→observe→commit 的
// commit 段）：原子重检接纳（活动/屏障/abort/wave COMPLETE）＋须消费未决意图
// （PendingPromotion 非空；无意图的直达调用拒绝——不存在绕过 OpenPromotion 的
// 第二入口）＋内容绑定（digest 必须等于当前 wave 已验证最终候选的 digest：晋
// 升证明的是已验证内容，不是任意原生身份）。落账：清意图、写
// PromotedSnapshot/PromotedDigest、收口 op.candidate 步骤。同值重放幂等。
// 就绪检查、区间计数与 squash 归 facade 经注入的 Git provider 完成，本入口只
// 落权威事实。
func (e *Engine) RecordCandidatePromotion(snapshot, digest, expectedFingerprint string) error {
	snapshot = strings.TrimSpace(snapshot)
	digest = strings.TrimSpace(digest)
	revision, state, err := e.load()
	if err != nil {
		return err
	}
	if state == nil {
		return &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	if err := state.admitPromotion(); err != nil {
		return err
	}
	if snapshot == "" || digest == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "candidate promotion requires the promoted snapshot identity and content digest"}
	}
	if state.PromotedSnapshot == snapshot && state.PromotedDigest == digest {
		return nil
	}
	if state.PendingPromotion == "" {
		return &RejectedError{Code: CodeEventNotCurrent, Detail: "candidate promotion requires an open promotion intent"}
	}
	if digest != state.currentValidationWave().Candidate.Digest {
		return &RejectedError{Code: CodeReceiptConflict, Detail: fmt.Sprintf("candidate promotion content %s does not match the verified final candidate %s", digest, state.currentValidationWave().Candidate.Digest)}
	}
	state.PendingPromotion = ""
	state.PromotedSnapshot = snapshot
	state.PromotedDigest = digest
	// 晋升绑定是 op.candidate 步骤的收口事实（settle 对该操作保持开放）：
	// 终审回执已在账时完成该步骤，终态边界随之解除，run 可推进到 terminal。
	for i := range e.cfg.Definition.Steps {
		step := e.cfg.Definition.Steps[i]
		payload, ok := step.Payload.(compiler.CompiledHostActionStep)
		if !ok || payload.Operation != "op.candidate" {
			continue
		}
		if !slices.Contains(state.Completed, step.Header.ID) && state.hasSettledHostActionForStep(step.Header.ID) {
			if err := state.CompleteStep(step.Header.ID, e.cfg.Definition); err != nil {
				return err
			}
		}
	}
	_, err = e.commit(state, revision, expectedFingerprint)
	return err
}

// SealTerminal seals an ordinary regular run after verified native delivery.
// A child cannot seal independently. Native identity enters from the facade;
// current validation, promotion, barrier and abort facts are checked here.
// A terminal replay with the same promoted identity performs no write.
func (e *Engine) SealTerminal(delivered, expectedFingerprint string) error {
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
	if !state.validationConcluded(e.cfg.Definition) {
		return &RejectedError{Code: CodeEventNotCurrent, Detail: "terminal sealing requires the concluded validation pipeline"}
	}
	if err := state.TransitionPhase(runtime.PhaseTerminal); err != nil {
		return err
	}
	_, err = e.commit(state, revision, expectedFingerprint)
	return err
}

// promotionPending 报告 validation 边界是否在等待终态晋升：最终 wave 已
// COMPLETE 且尚未绑定晋升快照。
func (s *State) promotionPending() bool {
	if s.PromotedSnapshot != "" {
		return false
	}
	w := s.currentValidationWave()
	return w != nil && w.Status == "COMPLETE"
}

// validationConcluded 报告验证管线是否已收口——验证期 agent 步的过程级完成
// 事实。前置门与 advanceValidation 的管线门同形（门未开时收口事实不存在）；
// 收口只消费两个既有权威：终 wave COMPLETE（obligations 全解，由
// settleValidationWave 单点定义）＋候选晋升已绑定且内容与该 wave 已验证最终候
// 选相等（由 RecordCandidatePromotion 单点写入，谓词在此复核当前 wave——单一
// 谓词仍是唯一判定位）。
func (s *State) validationConcluded(cd *compiler.CompiledDefinition) bool {
	if !pipelineEnabled(s, cd) || s.Validation == nil || s.Validation.RequirementRevision != s.RequirementRevision() || s.requirementChangePending() || s.Topology == nil || s.RouteMode == "" {
		return false
	}
	w := s.currentValidationWave()
	return w != nil && w.Status == "COMPLETE" && s.PromotedSnapshot != "" && s.PromotedDigest == w.Candidate.Digest
}
