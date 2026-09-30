package facade

import (
	"strings"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/lifecycle"
)

// CostSourceBridge 是 engine run 的派发计量来源解析器：typed
// (runID, actionID, provider, correlation) 引用 → READY/PENDING/UNAVAILABLE。
// 它只负责 lifecycle-sidecar 查找（internal/lifecycle 的既有 store）；
// transcript 解析归 internal/cost，引擎只消费解析后的投影：
//   - 无绑定/错读/provider 与 run 绑定不符 → UNAVAILABLE（明确缺源）；
//   - 绑定在账而 stop 证据未到 → PENDING（engine 把结果留在回执边界等待）；
//   - 宿主不暴露可用 lifecycle 事件（Verification.Outcome=Unavailable）、
//     stop 到达而无 transcript 路径、或解析失败 → UNAVAILABLE；
//   - stop 到达且可解析 → READY＋精确用量。
func CostSourceBridge(root, runID string) protocol.CostSourceResolver {
	return func(ref protocol.CostSourceRef) protocol.ResolvedCostSource {
		provider, transcriptPath, err := lifecycle.DispatchTranscriptPath(root, ref.RunID, ref.ActionID)
		if err != nil || strings.TrimSpace(provider) == "" || provider != ref.Provider {
			return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable}
		}
		verification, err := lifecycle.VerifyDispatch(root, ref.RunID, ref.ActionID)
		if err != nil {
			return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable}
		}
		if !verification.StopObserved {
			if verification.Outcome == lifecycle.Unavailable {
				return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable}
			}
			return protocol.ResolvedCostSource{State: protocol.CostSourcePending}
		}
		if strings.TrimSpace(transcriptPath) == "" {
			return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable}
		}
		usage, parseErr := cost.ParseTranscript(provider, transcriptPath)
		if parseErr != nil {
			return protocol.ResolvedCostSource{State: protocol.CostSourceUnavailable}
		}
		return protocol.ResolvedCostSource{State: protocol.CostSourceReady, Usage: usage}
	}
}
