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

// OwnerBinding 是受理方声明的主代理（owner）计量来源：transcript 路径＋
// provider 身份，两者同时非空才承诺 owner 计量（REQ-012 AC-050；缺源＝
// UNAVAILABLE 零数字）。
type OwnerBinding struct {
	Transcript string
	Provider   string
}

// lockOwnerCost 是两代 runtime 共同的 owner 区间协调锁（与 legacy 同路径＝同
// 一内核锁对象：legacy 的快速失败 acquire 与本处的等待 acquire 互相互斥）。
// engine 的区间操作段（启动：扫描＋自身绑定＋peer 标记；终态：重扫＋自身标
// 记＋peer 标记＋量取）全部在锁内执行且冲突时等待——并行启动的区间判定必须
// 串行闭合，不能把并发竞争当失败上抛。锁序＝协调锁→各 run 自身 store 锁
// （与 legacy mutateRun 同序，不得反序；等待不构成环：无人持有 store/state
// 锁的同时等待本锁）。
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

// legacyOwnerOverlap 检测 legacy runtime（internal/validate）侧与本 run 的区间
// 相交，读两类只读工件（engine 不跨代写，legacy 的事实由其合法 writer 落盘）：
//   - .gates/tmp/*/state.json（活动状态，终态留存仅出现在终态中断残留）：
//     ACTIVE＝相交；终态残留只在其活动始于我方区间内（StartedAt 晚于我方绑定
//     时点）时相交——早于我方启动而已终态的区间与我不相交。legacy 的启动身份
//     与终态摘要都在其 owner 协调锁内落盘，StartedAt 因此是锁段内时点。
//   - .gates/results/*.json（终态留存摘要，run 目录完成后即删）：OwnerTranscript
//     相同且 StartedAt 晚于我方绑定时点＝其整个生命周期在我方区间内启动并于我
//     方终态重扫前完成，必相交；无法解析的时间不可排除，按相交处理。
//
// 启动观察点传空 startedAt 时只判 ACTIVE（摘要与终态残留的 StartedAt 不可能晚
// 于同锁段内刚盖下的本方时点，规则空转但语义一致）。
func legacyOwnerOverlap(root, runID, transcript, startedAt string) bool {
	canonical := filepath.Clean(transcript)
	legacyPaths, err := filepath.Glob(filepath.Join(root, ".gates", "tmp", "*", "state.json"))
	if err != nil {
		return true
	}
	for _, path := range legacyPaths {
		if other := filepath.Base(filepath.Dir(path)); other == runID {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			// An unreadable active interval cannot be excluded from attribution.
			return true
		}
		var legacy struct {
			Status          string `json:"status"`
			OwnerTranscript string `json:"ownerTranscript"`
			StartedAt       string `json:"startedAt"`
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return true
		}
		if legacy.OwnerTranscript == "" || filepath.Clean(legacy.OwnerTranscript) != canonical {
			continue
		}
		if legacy.Status == "ACTIVE" {
			return true
		}
		if startedAt != "" && legacyStartedWithin(legacy.StartedAt, startedAt) {
			return true
		}
	}
	return legacySummaryOverlap(root, canonical, startedAt)
}

// legacySummaryOverlap 扫描 legacy 终态留存摘要（.gates/results/*.json，含
// ABORTED）：OwnerTranscript 相同且 StartedAt 晚于我方绑定时点＝该 run 在我方
// 区间内启动并完成，区间相交。缺失/无法解析的 StartedAt 不可排除，按相交处理。
func legacySummaryOverlap(root, canonical, startedAt string) bool {
	if startedAt == "" {
		return false
	}
	summaryPaths, err := filepath.Glob(filepath.Join(root, ".gates", "results", "*.json"))
	if err != nil {
		return true
	}
	for _, path := range summaryPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			return true
		}
		var summary struct {
			OwnerTranscript string `json:"ownerTranscript"`
			StartedAt       string `json:"startedAt"`
		}
		if err := json.Unmarshal(data, &summary); err != nil {
			return true
		}
		if summary.OwnerTranscript == "" || filepath.Clean(summary.OwnerTranscript) != canonical {
			continue
		}
		if legacyStartedWithin(summary.StartedAt, startedAt) {
			return true
		}
	}
	return false
}

// legacyStartedWithin 报告 legacy peer 的活动是否始于我方区间内（其 StartedAt
// 晚于我方绑定时点）。缺失或无法解析的时间按相交处理（不可排除）。
func legacyStartedWithin(peerStartedAt, myStartedAt string) bool {
	peer, err := time.Parse(time.RFC3339Nano, peerStartedAt)
	if err != nil {
		return true
	}
	mine, err := time.Parse(time.RFC3339Nano, myStartedAt)
	if err != nil {
		return true
	}
	return peer.After(mine)
}

// ownerOverlap 报告本 run 的 owner 区间是否与 peer 相交（两个命名空间：engine
// run 与 legacy validate run，迁移期两代并存是产品支持形态）。终态观察点额外
// 携带本方绑定时点，接纳「peer 在我方区间内启动且先于我方终态重扫消失」的区间
// 事实；启动观察点传空串只判开放区间（活动或终态未发布摘要）。
func ownerOverlap(root, runID, transcript, startedAt string) bool {
	peers, resolvable := engineOverlapPeers(root, runID, transcript)
	if len(peers) > 0 || !resolvable {
		return true
	}
	if engineSummaryOverlap(root, runID, transcript, startedAt) {
		return true
	}
	return legacyOwnerOverlap(root, runID, transcript, startedAt)
}

// markOverlappingEnginePeers 在自身来源绑定后把重叠事实补写进每个重叠 engine
// peer 的 owner 投影（区间事实双边持久：先启动 run 的启动布尔不随后续重叠更
// 新，其终态报告也必须携带重叠）。写失败上抛——重叠事实是 peer 终态成本报告
// 正确性的保证，静默跳过会让 peer 记出精确的假数字；与 legacy beginOwnerCost
// 失败即中止启动同一形状。peer 标记幂等（无绑定＝无操作、已标记＝零变化）。
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
	binding := protocol.OwnerCostBinding{Transcript: strings.TrimSpace(owner.Transcript), Provider: strings.TrimSpace(owner.Provider)}
	if binding.Transcript == "" || binding.Provider == "" {
		return protocol.OwnerCostBinding{}
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

// finishOwnerCost 是 engine 的终态观察点：在 lockOwnerCost 内重扫两个命名空间
// （ACTIVE peer ＋先于我方终态消失的 peer 留存工件），检出先经引擎事务持久
// OwnerOverlapped 再量取，并把重叠补写进每个重叠 engine peer（对端终态报告同
// 样不可记精确假数字）；量取后经引擎落账（幂等；REQ-012 AC-052）。差值只作
// 报告：无法精确核对时记 UNAVAILABLE 零数字（AC-051）。
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
	if !overlapped {
		if usage, err := cost.MeasureOwner(provider, transcript, baseline); err == nil {
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
