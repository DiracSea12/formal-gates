package protocol

import (
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

// RecordCandidatePromotion 绑定候选晋升后的快照（镜像 legacy PrepareCandidate
// 对 CurrentSnapshot 的自由移动：区间单提交＝确认当前 HEAD，多提交＝squash 后
// 重解析；晋升后区间再度累积提交时可重复组织、重复绑定）。前置＝run 尚在活
// 动期（终态 run 不得再改写）；就绪检查、区间计数与 squash 归 facade 经注入的
// Git provider 完成，本入口只落权威事实。绑定同一快照幂等。晋升事实同时解除
// validation 边界的终态晋升等待并收口 op.candidate 步骤。
func (e *Engine) RecordCandidatePromotion(snapshot, expectedFingerprint string) error {
	snapshot = strings.TrimSpace(snapshot)
	revision, state, err := e.load()
	if err != nil {
		return err
	}
	if state == nil {
		return &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	if state.Phase == runtime.PhaseTerminal {
		return &RejectedError{Code: CodeEventNotCurrent, Detail: "candidate promotion requires an active run"}
	}
	if snapshot == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "candidate promotion requires the promoted snapshot identity"}
	}
	if state.PromotedSnapshot == snapshot {
		return nil
	}
	state.PromotedSnapshot = snapshot
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

// promotionPending 报告 validation 边界是否在等待终态晋升：最终 wave 已
// COMPLETE 且尚未绑定晋升快照。
func (s *State) promotionPending() bool {
	if s.PromotedSnapshot != "" {
		return false
	}
	w := s.currentValidationWave()
	return w != nil && w.Status == "COMPLETE"
}
