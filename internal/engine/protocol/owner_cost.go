package protocol

import (
	"strings"

	"formal-gates/internal/cost"
)

// OwnerCostBinding 是受理 bootstrap 一次写入的 owner 计量绑定：来源稳定
// （transcript＋provider）时携带启动基线（REQ-012 AC-050）；缺源不承诺 owner
// 计量（AC-051 零数字）。基线捕获与重叠检测的 I/O 归 facade（与
// CostSourceResolver 同一注入边界），本结构只承载持久值。
type OwnerCostBinding struct {
	Transcript string                   `json:"transcript"`
	Provider   string                   `json:"provider"`
	Baseline   *cost.TranscriptSnapshot `json:"baseline,omitempty"`
	Overlapped bool                     `json:"overlapped,omitempty"`
}

// BindCostSources 在受理时把 owner 计量绑定与项目级 token cap 配置一次写入
// 同一权威 state（单一 writer 经既有提交点；token cap「配置读取、持久化」归接
// 线）。只在 bootstrap 调用：已绑定或 run 已推进时拒绝，防止运行中改写来源。
func (e *Engine) BindCostSources(owner OwnerCostBinding, tokenCap *int64, expectedFingerprint string) error {
	revision, state, err := e.load()
	if err != nil {
		return err
	}
	if state == nil {
		return &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	if (state.OwnerTranscript != "" && state.OwnerTranscript != owner.Transcript) || state.TokenCapMaxTokens != nil {
		return &RejectedError{Code: CodeReceiptConflict, Detail: "run cost sources are already bound"}
	}
	if revision > 1 {
		return &RejectedError{Code: CodeEventNotCurrent, Detail: "cost sources bind only at run bootstrap"}
	}
	if strings.TrimSpace(owner.Transcript) != "" {
		state.OwnerTranscript = owner.Transcript
		state.OwnerProvider = owner.Provider
		state.OwnerBaseline = owner.Baseline
		state.OwnerOverlapped = owner.Overlapped
	}
	state.TokenCapMaxTokens = tokenCap
	_, err = e.commit(state, revision, expectedFingerprint)
	return err
}

// RecordOwnerCost 把 owner 终态差值记入 cost 投影（幂等：Owner 已在即零变化返
// 回，AC-052）。差值的量取（transcript I/O）归 facade，本入口只落账；只作终态
// 报告，不参与 in-flight guard。
func (e *Engine) RecordOwnerCost(entry cost.OwnerCost, expectedFingerprint string) error {
	revision, state, err := e.load()
	if err != nil {
		return err
	}
	if state == nil {
		return &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	if state.OwnerTranscript == "" {
		return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "run has no owner cost binding"}
	}
	if state.Cost != nil && state.Cost.Owner != nil {
		return nil
	}
	cost.RecordOwner(state.costLedger(), entry)
	_, err = e.commit(state, revision, expectedFingerprint)
	return err
}

// OwnerCostFacts 导出 owner 终态差值量取所需的持久绑定事实（facade 据此调
// cost.MeasureOwner；缺绑定时 ok=false）。
func (e *Engine) OwnerCostFacts() (transcript, provider string, baseline *cost.TranscriptSnapshot, overlapped, ok bool) {
	_, state, err := e.load()
	if err != nil || state == nil || state.OwnerTranscript == "" {
		return "", "", nil, false, false
	}
	return state.OwnerTranscript, state.OwnerProvider, state.OwnerBaseline, state.OwnerOverlapped, true
}
