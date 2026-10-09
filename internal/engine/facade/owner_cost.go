package facade

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"formal-gates/internal/coordination"
	"formal-gates/internal/cost"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/lifecycle"
)

// OwnerBinding carries bootstrap conversation scope and metering source.
// Session alone identifies scope; metering still requires transcript and provider.
type OwnerBinding struct {
	Transcript string
	Session    string
	Provider   string
}

// lockOwnerCost coordinates transcript intervals for engine runs at this root.
// Lock order is interval coordination before the run store transaction.
func lockOwnerCost(root string) (func(), error) {
	return coordination.AcquirePathWaiting(filepath.Join(lifecycle.CleanRoot(root), ".gates", "tmp", "owner-cost.lock"), "owner cost")
}

// tokenCapConfigPath 是项目级 token cap 配置的固定位置（3.5b：只接受项目/安装
// 级维护配置，不按任务临场估算）。
func tokenCapConfigPath(root string) string {
	return filepath.Join(root, ".gates", "token-cap.json")
}

// readTokenCap 读取项目级 token cap：缺文件＝不可用（nil）；文件存在但非法
// （坏 JSON、缺字段、负数）＝明确报错，不静默。
func readTokenCap(root string) (*int64, error) {
	data, err := os.ReadFile(tokenCapConfigPath(root))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var doc struct {
		EngineDispatchMaxTotalTokens *int64 `json:"engineDispatchMaxTotalTokens"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("token cap config %s is invalid: %w", tokenCapConfigPath(root), err)
	}
	if doc.EngineDispatchMaxTotalTokens == nil {
		return nil, fmt.Errorf("token cap config %s is invalid: engineDispatchMaxTotalTokens is required", tokenCapConfigPath(root))
	}
	if *doc.EngineDispatchMaxTotalTokens < 0 {
		return nil, fmt.Errorf("token cap config %s is invalid: engineDispatchMaxTotalTokens must be non-negative", tokenCapConfigPath(root))
	}
	return doc.EngineDispatchMaxTotalTokens, nil
}

// engineOverlapPeers 返回同 root 下与本 run 共享 owner transcript 且区间无法
// 排除相交的 engine peer runID（persistence 信封形状）。闭合区间来自成功
// child 的 state receipt 或普通 run 的终态摘要；证据未落盘或端点不可解析时
// 区间仍开放。闭合区间交由 engineSummaryOverlap 判断实际相交。
// resolvable=false 表示存在无法排除的读取失败（Glob 错误/读失
// 败/坏信封——重叠不可否定），调用方按重叠处理。
func engineOverlapPeers(root, runID, transcript string) (peers []string, resolvable bool) {
	canonical := filepath.Clean(transcript)
	paths, err := filepath.Glob(filepath.Join(root, EngineNamespace, "*", "state.json"))
	if err != nil {
		return nil, false
	}
	resolvable = true
	for _, path := range paths {
		if other := filepath.Base(filepath.Dir(path)); other == runID {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, false
		}
		var envelope struct {
			Content struct {
				Phase           string                 `json:"phase"`
				OwnerTranscript string                 `json:"ownerTranscript"`
				SliceReceipt    *protocol.SliceReceipt `json:"sliceReceipt"`
			} `json:"content"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return nil, false
		}
		projection := envelope.Content
		if strings.TrimSpace(projection.OwnerTranscript) == "" || filepath.Clean(projection.OwnerTranscript) != canonical {
			continue
		}
		if projection.Phase != string(runtime.PhaseTerminal) && projection.Phase != string(runtime.PhaseSliceReady) || !enginePeerIntervalClosed(filepath.Dir(path), projection.SliceReceipt) {
			peers = append(peers, filepath.Base(filepath.Dir(path)))
		}
	}
	return peers, true
}

// enginePeerIntervalClosed reads the existing owner endpoint evidence. A child
// receipt closes the interval in state even before its sidecar is published.
func enginePeerIntervalClosed(stateDir string, receipt *protocol.SliceReceipt) bool {
	if receipt != nil {
		_, startErr := time.Parse(time.RFC3339Nano, receipt.Receipt.OwnerStartedAt)
		_, endErr := time.Parse(time.RFC3339Nano, receipt.Receipt.OwnerEndedAt)
		return startErr == nil && endErr == nil
	}
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

// engineSummaryOverlap consumes closed intervals from terminal summaries and
// state-owned child receipts. OwnerTranscript canonical 相等且 [OwnerStartedAt,
// OwnerEndedAt] 与本方区间 [startedAt, 量取时点] 相交。startedAt 为空＝启动观
// 察点，已闭合区间必先于本方锁段结束，不判相交；端点不可解析＝不可排除，按
// 相交处理。
func engineSummaryOverlap(root, runID, transcript, startedAt string) bool {
	if startedAt == "" {
		return false
	}
	canonical := filepath.Clean(transcript)
	summaryPaths, err := filepath.Glob(filepath.Join(root, EngineNamespace, "*", "terminal-summary.json"))
	if err != nil {
		return true
	}
	statePaths, err := filepath.Glob(filepath.Join(root, EngineNamespace, "*", "state.json"))
	if err != nil {
		return true
	}
	myStart, myStartErr := time.Parse(time.RFC3339Nano, startedAt)
	now := time.Now().UTC()
	for _, path := range append(summaryPaths, statePaths...) {
		if filepath.Base(filepath.Dir(path)) == runID {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return true
		}
		var summary struct {
			OwnerTranscript string `json:"ownerTranscript"`
			OwnerStartedAt  string `json:"ownerStartedAt"`
			OwnerEndedAt    string `json:"ownerEndedAt"`
		}
		if filepath.Base(path) == "state.json" {
			var envelope struct {
				Content struct {
					SliceReceipt *protocol.SliceReceipt `json:"sliceReceipt"`
				} `json:"content"`
			}
			if err := json.Unmarshal(data, &envelope); err != nil {
				return true
			}
			if envelope.Content.SliceReceipt == nil {
				continue
			}
			r := envelope.Content.SliceReceipt.Receipt
			summary.OwnerTranscript, summary.OwnerStartedAt, summary.OwnerEndedAt = r.OwnerTranscript, r.OwnerStartedAt, r.OwnerEndedAt
		} else if err := json.Unmarshal(data, &summary); err != nil {
			return true
		}
		if strings.TrimSpace(summary.OwnerTranscript) == "" || filepath.Clean(summary.OwnerTranscript) != canonical {
			continue
		}
		peerStart, startErr := time.Parse(time.RFC3339Nano, summary.OwnerStartedAt)
		peerEnd, endErr := time.Parse(time.RFC3339Nano, summary.OwnerEndedAt)
		if startErr != nil || endErr != nil || myStartErr != nil {
			return true
		}
		if !peerStart.After(now) && !peerEnd.Before(myStart) {
			return true
		}
	}
	return false
}

// ownerOverlap observes open peer intervals and retained engine summaries.
// startedAt bounds closed peer intervals relative to this owner's binding.
func ownerOverlap(root, runID, transcript, startedAt string) bool {
	peers, resolvable := engineOverlapPeers(root, runID, transcript)
	if len(peers) > 0 || !resolvable {
		return true
	}
	if engineSummaryOverlap(root, runID, transcript, startedAt) {
		return true
	}
	return false
}

// markOverlappingEnginePeers 在自身来源绑定后把重叠事实补写进每个重叠 engine
// peer 的 owner 投影（区间事实双边持久：先启动 run 的启动布尔不随后续重叠更
// 新，其终态报告也必须携带重叠）。写失败上抛——重叠事实是 peer 终态成本报告
// 正确性的保证。已被安装 fence 的 peer 不能再写，当前 run 的相交事实仍
// 由自身 Overlapped 保守报告 UNAVAILABLE；其余错误上抛。peer 标记幂等
// （无绑定＝无操作、已标记＝零变化）。
func markOverlappingEnginePeers(root, runID, transcript string) error {
	peers, resolvable := engineOverlapPeers(root, runID, transcript)
	if !resolvable {
		return fmt.Errorf("engine start: owner overlap peers at %s are unreadable; the shared interval cannot be attributed", filepath.Join(root, EngineNamespace))
	}
	for _, peer := range peers {
		peerFacade, err := Open(root, peer)
		if err != nil {
			return fmt.Errorf("engine start: open overlapping peer run %q: %w", peer, err)
		}
		if err := peerFacade.engine.MarkOwnerOverlapped(emptyFingerprint()); err != nil {
			if errors.Is(err, errStaleFencingToken) {
				continue
			}
			return fmt.Errorf("engine start: mark overlapping peer run %q: %w", peer, err)
		}
	}
	return nil
}

// captureOwnerBinding 在来源稳定时取 owner 启动基线与区间端点（无法建立精确
// 基线时不承诺计量：baseline 为 nil，终态差值记 UNAVAILABLE）。StartedAt 与
// 扫描同处调用方的 lockOwnerCost 段内：锁内盖下的端点使对端的终态重扫可依
// 时序闭合「对端先消失」的交错。
func captureOwnerBinding(root, runID string, owner OwnerBinding) protocol.OwnerCostBinding {
	binding := protocol.OwnerCostBinding{Transcript: strings.TrimSpace(owner.Transcript), Session: strings.TrimSpace(owner.Session), Provider: strings.TrimSpace(owner.Provider)}
	if binding.Transcript == "" || binding.Provider == "" {
		return protocol.OwnerCostBinding{Session: binding.Session}
	}
	binding.StartedAt = time.Now().UTC().Format(time.RFC3339Nano)
	binding.Overlapped = ownerOverlap(root, runID, binding.Transcript, binding.StartedAt)
	if !binding.Overlapped {
		binding.Baseline, _ = cost.CaptureBaseline(binding.Provider, binding.Transcript)
	}
	return binding
}

// ownerInterval 是终态摘要携带的 owner 区间端点的内存形态：
// endedAt 在终态协调锁段内与量取同时点盖下。
type ownerInterval struct {
	transcript string
	startedAt  string
	endedAt    string
}

// finishOwnerCost rescans engine owner intervals under the coordination lock,
// records overlap through the core, then measures the bound terminal interval.
// Missing or overlapping metering sources retain UNAVAILABLE without numbers.
func (f *Facade) finishOwnerCost() (ownerInterval, error) {
	if _, _, _, _, _, bound := f.engine.OwnerCostFacts(); !bound {
		return ownerInterval{}, nil
	}
	release, err := lockOwnerCost(f.root)
	if err != nil {
		return ownerInterval{}, err
	}
	defer release()
	interval, entry, err := f.observeOwnerCost()
	if err != nil {
		return ownerInterval{}, err
	}
	if entry != nil {
		if err := f.engine.RecordOwnerCost(*entry, emptyFingerprint()); err != nil {
			return ownerInterval{}, err
		}
	}
	return interval, nil
}

// observeOwnerCost uses the bound owner interval under lockOwnerCost. It leaves
// cost and the endpoint uncommitted so its caller can freeze them with either
// the child checkpoint or the ordinary terminal report.
func (f *Facade) observeOwnerCost() (ownerInterval, *cost.OwnerCost, error) {
	transcript, provider, startedAt, baseline, overlapped, bound := f.engine.OwnerCostFacts()
	if !bound {
		return ownerInterval{}, nil, nil
	}
	if !overlapped && ownerOverlap(f.root, f.runID, transcript, startedAt) {
		if err := f.engine.MarkOwnerOverlapped(emptyFingerprint()); err != nil {
			return ownerInterval{}, nil, err
		}
		overlapped = true
	}
	if err := markOverlappingEnginePeers(f.root, f.runID, transcript); err != nil {
		return ownerInterval{}, nil, err
	}
	interval := ownerInterval{transcript: transcript, startedAt: startedAt, endedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	entry := cost.OwnerCost{Source: cost.SourceUnavailable}
	if overlapped {
		entry.Reason = "owner usage interval overlaps another run"
	} else {
		if usage, err := cost.MeasureOwner(provider, transcript, baseline); err != nil {
			entry.Reason = err.Error()
		} else {
			entry = cost.OwnerCost{
				InputCacheHitTokens:  usage.InputCacheHitTokens,
				InputCacheMissTokens: usage.InputCacheMissTokens,
				OutputTokens:         usage.OutputTokens,
				TotalInputTokens:     usage.InputCacheHitTokens + usage.InputCacheMissTokens,
				Source:               cost.SourceTranscript,
			}
		}
	}
	return interval, &entry, nil
}
