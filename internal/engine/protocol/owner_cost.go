package protocol

import (
	"strings"

	"formal-gates/internal/cost"
)

// OwnerCostBinding 是受理 bootstrap 一次写入的 owner 计量绑定：来源稳定
// （transcript＋provider）时携带启动基线与绑定时点（REQ-012 AC-050；缺源不
// 承诺 owner 计量，AC-051 零数字）。基线捕获与重叠检测的 I/O 归 facade（与
// CostSourceResolver 同一注入边界），本结构只承载持久值。StartedAt 是区间事实
// 的本方端点（wall clock，观察点在协调锁内写入），收口重扫、终态摘要与 child
// checkpoint 消费它。
type OwnerCostBinding struct {
	Transcript string                   `json:"transcript"`
	Session    string                   `json:"session,omitempty"`
	Provider   string                   `json:"provider"`
	StartedAt  string                   `json:"startedAt,omitempty"`
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
	if (state.OwnerTranscript != "" && state.OwnerTranscript != owner.Transcript) || (state.OwnerSession != "" && state.OwnerSession != owner.Session) || state.TokenCapMaxTokens != nil {
		return &RejectedError{Code: CodeReceiptConflict, Detail: "run cost sources are already bound"}
	}
	if revision > 1 {
		return &RejectedError{Code: CodeEventNotCurrent, Detail: "cost sources bind only at run bootstrap"}
	}
	state.OwnerSession = strings.TrimSpace(owner.Session)
	if strings.TrimSpace(owner.Transcript) != "" {
		state.OwnerTranscript = owner.Transcript
		state.OwnerProvider = owner.Provider
		state.OwnerStartedAt = owner.StartedAt
		state.OwnerBaseline = owner.Baseline
		state.OwnerOverlapped = owner.Overlapped
	}
	state.TokenCapMaxTokens = tokenCap
	_, err = e.commit(state, revision, expectedFingerprint)
	return err
}

// MarkOwnerOverlapped 把 owner 区间重叠事实持久写进本 run 的 owner 投影（幂
// 等：无 owner 绑定＝无操作；已标记＝零变化，AC-052 同形状的幂等记账）。重叠
// 是区间事实、双边持久（legacy beginOwnerCost 的双边标记语义——任一方可能先
// 终态并清理状态，先启动 run 的启动布尔不随后续重叠更新）：后启动 run 检测到
// 共享 owner transcript 的活动 engine peer 时，facade 经本入口补写 peer。
// 写入走 engine 自有事务＋CAS；直改 peer 的 state.json 文件会破坏持久化完整
// 性，禁止。
func (e *Engine) MarkOwnerOverlapped(expectedFingerprint string) error {
	revision, state, err := e.load()
	if err != nil {
		return err
	}
	if state == nil {
		return &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	if state.OwnerTranscript == "" || state.OwnerOverlapped {
		return nil
	}
	state.OwnerOverlapped = true
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

// OwnerCostFacts 导出 owner 终态差值量取与区间重扫所需的持久绑定事实（facade
// 据此调 cost.MeasureOwner／区间观察；缺绑定时 ok=false）。
func (e *Engine) OwnerCostFacts() (transcript, provider string, startedAt string, baseline *cost.TranscriptSnapshot, overlapped, ok bool) {
	_, state, err := e.load()
	if err != nil || state == nil || state.OwnerTranscript == "" {
		return "", "", "", nil, false, false
	}
	return state.OwnerTranscript, state.OwnerProvider, state.OwnerStartedAt, state.OwnerBaseline, state.OwnerOverlapped, true
}
