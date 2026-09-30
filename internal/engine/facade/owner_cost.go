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

// ownerOverlap 镜像 legacy beginOwnerCost 的重叠检测：同一 owner transcript 上
// 存在其他活动 run 时，区间无法精确拆分，owner 记 UNAVAILABLE。扫描两个命名空
// 间（迁移期两代并存是产品支持形态）：engine run 的 persistence 信封形状与
// legacy validate run 的平坦形状（判定语义逐字镜像 internal/validate/cost.go 的
// beginOwnerCost：跳过自身、读失败＝重叠、status/ownerTranscript/canonical 路径
// 相等）。engine 侧只读检测，不跨代互写。
func ownerOverlap(root, runID, transcript string) bool {
	canonical := filepath.Clean(transcript)
	// engine 命名空间：state.json 是 persistence 信封，权威状态嵌套 content 下。
	enginePaths, err := filepath.Glob(filepath.Join(root, EngineNamespace, "*", "state.json"))
	if err != nil {
		return true // 不可排除的重叠区间按无法精确核对处理
	}
	for _, path := range enginePaths {
		if other := filepath.Base(filepath.Dir(path)); other == runID {
			continue
		}
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
		projection := envelope.Content
		if projection.Phase != string(runtime.PhaseTerminal) && strings.TrimSpace(projection.OwnerTranscript) != "" &&
			filepath.Clean(projection.OwnerTranscript) == canonical {
			return true
		}
	}
	// legacy 命名空间：validate run 的平坦形状 state.json。
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
