package facade

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
)

// OwnerBinding 是受理方声明的主代理（owner）计量来源：transcript 路径＋
// provider 身份，两者同时非空才承诺 owner 计量（REQ-012 AC-050；缺源＝
// UNAVAILABLE 零数字）。
type OwnerBinding struct {
	Transcript string
	Provider   string
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

// engineOverlapPeers 返回同 root 下与本 run 共享 owner transcript 的活动
// engine peer runID（persistence 信封形状：非终态 phase、owner transcript 经
// canonical 相等）。resolvable=false 表示存在无法排除的读取失败（Glob 错误/
// 读失败/坏信封——重叠不可否定），调用方按重叠处理。
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
				Phase           string `json:"phase"`
				OwnerTranscript string `json:"ownerTranscript"`
			} `json:"content"`
		}
		if err := json.Unmarshal(data, &envelope); err != nil {
			return nil, false
		}
		projection := envelope.Content
		if projection.Phase != string(runtime.PhaseTerminal) && strings.TrimSpace(projection.OwnerTranscript) != "" &&
			filepath.Clean(projection.OwnerTranscript) == canonical {
			peers = append(peers, filepath.Base(filepath.Dir(path)))
		}
	}
	return peers, true
}

// legacyOwnerOverlap 镜像 legacy beginOwnerCost 的 legacy 命名空间检测：同一
// owner transcript 上存在 ACTIVE legacy run（.gates/tmp 平坦形状 state.json）
// 时区间无法精确拆分。判定语义逐字镜像 internal/validate/cost.go 的
// beginOwnerCost：跳过自身、读失败＝重叠、status/ownerTranscript/canonical 路
// 径相等。legacy 侧不可写（engine 不跨代互写），其双向标记是注册表内部行为。
func legacyOwnerOverlap(root, runID, transcript string) bool {
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
		}
		if err := json.Unmarshal(data, &legacy); err != nil {
			return true
		}
		if legacy.Status == "ACTIVE" && legacy.OwnerTranscript != "" &&
			filepath.Clean(legacy.OwnerTranscript) == canonical {
			return true
		}
	}
	return false
}

// ownerOverlap 报告本 run 的 owner 区间是否与活动 peer 重叠（两个命名空间：
// engine run 与 legacy validate run，迁移期两代并存是产品支持形态）。
func ownerOverlap(root, runID, transcript string) bool {
	peers, resolvable := engineOverlapPeers(root, runID, transcript)
	if len(peers) > 0 || !resolvable {
		return true
	}
	return legacyOwnerOverlap(root, runID, transcript)
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

// captureOwnerBinding 在来源稳定时取 owner 启动基线（无法建立精确基线时不承
// 诺计量：baseline 为 nil，终态差值记 UNAVAILABLE）。
func captureOwnerBinding(root, runID string, owner OwnerBinding) protocol.OwnerCostBinding {
	binding := protocol.OwnerCostBinding{Transcript: strings.TrimSpace(owner.Transcript), Provider: strings.TrimSpace(owner.Provider)}
	if binding.Transcript == "" || binding.Provider == "" {
		return protocol.OwnerCostBinding{}
	}
	binding.Overlapped = ownerOverlap(root, runID, binding.Transcript)
	if !binding.Overlapped {
		binding.Baseline, _ = cost.CaptureBaseline(binding.Provider, binding.Transcript)
	}
	return binding
}

// finishOwnerCost 量取 owner 终态差值并经引擎落账（幂等；REQ-012 AC-052）。
// 差值只作报告：无法精确核对时记 UNAVAILABLE 零数字（AC-051）。
func (f *Facade) finishOwnerCost() error {
	transcript, provider, baseline, overlapped, bound := f.engine.OwnerCostFacts()
	if !bound {
		return nil
	}
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
	return f.engine.RecordOwnerCost(entry, emptyFingerprint())
}
