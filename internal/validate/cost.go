package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"formal-gates/internal/coordination"
	"formal-gates/internal/cost"
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
)

// backfillDispatchCost records the dispatch's exact token cost at result
// recording, right after lifecycle verification. It reads the transcript
// path stored on the dispatch's stop event and parses it through the
// provider adapter. A missing path, a parse failure, or a host without a
// parseable transcript (Cursor) yields an entry with
// source "unavailable" and zero numbers. Idempotence is owned by
// cost.Record alone: the first completion records the dispatch and repeats
// are no-ops. Cost data is display-only and never affects the recording
// outcome.
func backfillDispatchCost(root string, state *RunState, dispatch PreparedDispatch) {
	if state.Cost == nil {
		state.Cost = &cost.RunCost{}
	}
	entry := cost.DispatchCost{Target: dispatch.Target, Kind: dispatch.TargetKind, Source: cost.SourceUnavailable}
	provider, transcriptPath, err := workflowLifecycle.TranscriptPath(root, state.RunID, dispatch.ID)
	if err == nil && strings.TrimSpace(transcriptPath) != "" {
		if usage, parseErr := cost.ParseTranscript(provider, transcriptPath); parseErr == nil {
			entry = cost.DispatchCost{
				Target:               dispatch.Target,
				Kind:                 dispatch.TargetKind,
				InputCacheHitTokens:  usage.InputCacheHitTokens,
				InputCacheMissTokens: usage.InputCacheMissTokens,
				OutputTokens:         usage.OutputTokens,
				TotalInputTokens:     usage.InputCacheHitTokens + usage.InputCacheMissTokens,
				Source:               cost.SourceTranscript,
			}
		}
	}
	// Codex source and native timing are optional display metadata. Missing
	// timing never invents zero model time or changes another provider's behavior.
	if descriptor, lookupErr := host.Lookup(provider); lookupErr == nil && descriptor.CostProvider == host.Codex && transcriptPath != "" {
		entry.TranscriptPath = transcriptPath
		if timing, timingErr := cost.CodexTaskTiming(transcriptPath); timingErr == nil {
			entry.Timing = &timing
		}
	}
	cost.Record(state.Cost, dispatch.ID, entry)
}

// This uses the shared coordination kernel file lock, not a second cost
// store; a holder that dies has its lock released by the kernel. Always take
// it before terminal state/admission locks.
func lockOwnerCost(root string) (func(), error) {
	return coordination.AcquirePath(filepath.Join(lifecycle.CleanRoot(root), ".gates", "tmp", "owner-cost.lock"), "owner cost")
}

func beginOwnerCost(root string, state *RunState) error {
	if state.OwnerTranscript == "" {
		return nil
	}
	// Keep the original host identity: the write wall also compares that exact
	// string. Resolve aliases only for the cost interval comparison below.
	state.OwnerCostBaseline, _ = cost.CaptureBaseline(state.OwnerProvider, state.OwnerTranscript)
	paths, err := filepath.Glob(filepath.Join(lifecycle.CleanRoot(root), ".gates", "tmp", "*", "state.json"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		runID := filepath.Base(filepath.Dir(path))
		if runID == state.RunID {
			continue
		}
		other, err := LoadRunState(root, runID)
		if err != nil {
			// An unreadable active interval cannot be excluded from attribution.
			state.OwnerCostOverlapped = true
			continue
		}
		if other.Status != "ACTIVE" || other.OwnerTranscript == "" || canonicalRegistryPath(other.OwnerTranscript) != canonicalRegistryPath(state.OwnerTranscript) {
			continue
		}
		state.OwnerCostOverlapped = true
		// Persist on both runs now: either one may finish and delete its state
		// before the other reaches Seal/Abort. This changes only cost metadata.
		if _, err := mutateRun(root, runID, func(peer *RunState) error {
			peer.OwnerCostOverlapped = true
			return nil
		}); err != nil {
			return err
		}
	}
	return beginEngineOwnerCost(root, state)
}

// beginEngineOwnerCost 补扫 engine 命名空间（.gates/engine，候选代 runtime，
// 迁移期两代并存）：同一 owner transcript 上区间无法排除相交的 engine run 使
// 计量不可精确拆分。可排除条件与 engine 侧 engineOverlapPeers 同形：state.json
// 非终态＝区间开放；终态但 terminal-summary.json 缺失或区间端点不可解析＝区间
// 仍开放（终态落盘与摘要发布之间的中断窗口）；终态且端点可解析＝区间已先于
// 本方锁段闭合（本观察点在协调锁内，闭合区间的 EndedAt 早于本方 StartedAt），
// 可排除。只读自标记——engine 侧的接纳由 engine 自身观察点完成，本 writer 不
// 修改任何 engine 文件。state.json 为 persistence 信封形状，phase 字面值是
// engine 的终态 phase（internal/engine/runtime.PhaseTerminal）。
func beginEngineOwnerCost(root string, state *RunState) error {
	paths, err := filepath.Glob(filepath.Join(lifecycle.CleanRoot(root), ".gates", "engine", "*", "state.json"))
	if err != nil {
		return err
	}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			// An unreadable active interval cannot be excluded from attribution.
			state.OwnerCostOverlapped = true
			continue
		}
		var envelope struct {
			Content struct {
				Phase           string `json:"phase"`
				OwnerTranscript string `json:"ownerTranscript"`
			} `json:"content"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			state.OwnerCostOverlapped = true
			continue
		}
		if envelope.Content.OwnerTranscript == "" ||
			canonicalRegistryPath(envelope.Content.OwnerTranscript) != canonicalRegistryPath(state.OwnerTranscript) {
			continue
		}
		if envelope.Content.Phase != "TERMINAL" || !enginePeerIntervalClosed(filepath.Dir(path)) {
			state.OwnerCostOverlapped = true
		}
	}
	return nil
}

// enginePeerIntervalClosed 报告终态 engine run 的区间是否已由其终态摘要闭合
// （terminal-summary.json 的 OwnerStartedAt/OwnerEndedAt 可解析）。摘要缺失或
// 端点不可解析＝区间仍开放：区间闭合的持久事实就是终态摘要，发布前的中断窗
// 口内无人认领该区间，不可排除相交。
func enginePeerIntervalClosed(stateDir string) bool {
	data, err := os.ReadFile(filepath.Join(stateDir, "terminal-summary.json"))
	if err != nil {
		return false
	}
	var summary struct {
		OwnerStartedAt string `json:"ownerStartedAt"`
		OwnerEndedAt   string `json:"ownerEndedAt"`
	}
	if err := json.Unmarshal(data, &summary); err != nil {
		return false
	}
	_, startErr := time.Parse(time.RFC3339Nano, summary.OwnerStartedAt)
	_, endErr := time.Parse(time.RFC3339Nano, summary.OwnerEndedAt)
	return startErr == nil && endErr == nil
}

// finishOwnerCost 记录 owner 终态差值（幂等）。调用方（Seal/finishRun）已持有
// lockOwnerCost，量取前在同一锁段内终态重扫 engine 命名空间：本 run 启动后才
// 绑定、且先于本 run 终态消失的 engine run 只在此处可检出（ACTIVE 状态，或终
// 态摘要携带的区间端点与本 run 区间 [StartedAt, now] 相交）。只读接纳，engine
// 文件不被本 writer 修改。
func finishOwnerCost(root string, state *RunState) {
	if state.OwnerCostCompleted {
		return
	}
	if !state.OwnerCostOverlapped && state.OwnerTranscript != "" && engineOwnerOverlap(root, state) {
		state.OwnerCostOverlapped = true
	}
	if state.Cost == nil {
		state.Cost = &cost.RunCost{}
	}
	entry := cost.OwnerCost{Source: cost.SourceUnavailable}
	if !state.OwnerCostOverlapped {
		if usage, err := cost.MeasureOwner(state.OwnerProvider, state.OwnerTranscript, state.OwnerCostBaseline); err == nil {
			entry = cost.OwnerCost{
				InputCacheHitTokens:  usage.InputCacheHitTokens,
				InputCacheMissTokens: usage.InputCacheMissTokens,
				OutputTokens:         usage.OutputTokens,
				TotalInputTokens:     usage.InputCacheHitTokens + usage.InputCacheMissTokens,
				Source:               cost.SourceTranscript,
			}
		}
	}
	cost.RecordOwner(state.Cost, entry)
	state.OwnerCostCompleted = true
}

// engineOwnerOverlap 报告 engine 命名空间中是否有与本 run owner 区间相交的
// peer：区间开放的 engine run（非终态 phase 的 state.json，或终态但其终态摘
// 要缺失/端点不可解析——与 beginEngineOwnerCost 同一可排除条件）直接相交；
// 已闭合的 peer 以终态摘要（terminal-summary.json 的区间端点载荷，与活动状态
// 目录随终态清理不同、摘要留存）判定 [OwnerStartedAt, OwnerEndedAt] 与本 run
// 区间 [StartedAt, 重扫时点] 相交。读失败与无法解析的端点不可排除，按相交处
// 理。
func engineOwnerOverlap(root string, state *RunState) bool {
	statePaths, err := filepath.Glob(filepath.Join(lifecycle.CleanRoot(root), ".gates", "engine", "*", "state.json"))
	if err != nil {
		return true
	}
	for _, path := range statePaths {
		data, err := os.ReadFile(path)
		if err != nil {
			return true
		}
		var envelope struct {
			Content struct {
				Phase           string `json:"phase"`
				OwnerTranscript string `json:"ownerTranscript"`
			} `json:"content"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return true
		}
		if envelope.Content.OwnerTranscript == "" ||
			canonicalRegistryPath(envelope.Content.OwnerTranscript) != canonicalRegistryPath(state.OwnerTranscript) {
			continue
		}
		if envelope.Content.Phase != "TERMINAL" {
			return true
		}
		// 终态但摘要未发布＝区间仍开放；摘要已发布则由下方摘要扫描做相交
		// 判定（同一份 terminal-summary.json）。
		if !enginePeerIntervalClosed(filepath.Dir(path)) {
			return true
		}
	}
	summaryPaths, err := filepath.Glob(filepath.Join(lifecycle.CleanRoot(root), ".gates", "engine", "*", "terminal-summary.json"))
	if err != nil {
		return true
	}
	now := time.Now().UTC()
	myStart, myStartErr := time.Parse(time.RFC3339Nano, state.StartedAt)
	for _, path := range summaryPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			return true
		}
		var summary struct {
			OwnerTranscript string `json:"ownerTranscript"`
			OwnerStartedAt  string `json:"ownerStartedAt"`
			OwnerEndedAt    string `json:"ownerEndedAt"`
		}
		if err := json.Unmarshal(data, &summary); err != nil {
			return true
		}
		if summary.OwnerTranscript == "" ||
			canonicalRegistryPath(summary.OwnerTranscript) != canonicalRegistryPath(state.OwnerTranscript) {
			continue
		}
		peerStart, startErr := time.Parse(time.RFC3339Nano, summary.OwnerStartedAt)
		peerEnd, endErr := time.Parse(time.RFC3339Nano, summary.OwnerEndedAt)
		if startErr != nil || endErr != nil || myStartErr != nil {
			// 区间端点不可解析时无法否定相交。
			return true
		}
		if !peerStart.After(now) && !peerEnd.Before(myStart) {
			return true
		}
	}
	return false
}
