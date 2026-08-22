package shadow

import (
	"formal-gates/internal/engine/decision"
)

// Boundary 是预测与 legacy 实际下一步共享的外部边界类型词表：把两侧的
// "下一步"都投影到 agent 派发 / 宿主机械动作 / 用户决定 / 终态四类可比
// 边界上，差异报告才有一致的比较轴。
type Boundary string

const (
	// BoundaryAgent：下一步是向 agent 的派发（legacy 的 requirement 动作与
	// 门 FAIL 后的返修派发；引擎侧对应 KindReady）。
	BoundaryAgent Boundary = "AGENT"
	// BoundaryHost：下一步是宿主/CLI 的机械动作（legacy 的待执行门；引擎侧
	// 对应 KindHostAction）。
	BoundaryHost Boundary = "HOST"
	// BoundaryHuman：下一步是用户拥有的决定（legacy 全部完成后待用户 seal；
	// 引擎侧对应 KindAsk）。
	BoundaryHuman Boundary = "HUMAN"
	// BoundaryTerminal：run 已关闭（SEALED/ABORTED），无下一步；引擎侧对应
	// KindComplete。
	BoundaryTerminal Boundary = "TERMINAL"
)

// ActualNext 是从 legacy 状态机械推断出的"实际下一步"。Inferable 为 false
// 表示该状态推不出实际下一步（不可比较的依据之一）；Step 是人可读的下一步
// 描述（如 "action:product-review"、"gate:gate.build"、"seal"、"terminal"）。
type ActualNext struct {
	Inferable bool     `json:"inferable"`
	Step      string   `json:"step,omitempty"`
	Boundary  Boundary `json:"boundary,omitempty"`
}

// Verdict 是差异报告的三类判定（master-requirements §6：输出 eligible
// frontier 预测与差异）。
type Verdict string

const (
	// VerdictMatch：可比较且一致（run 终态性一致，或下一外部边界类型一致）。
	VerdictMatch Verdict = "MATCH"
	// VerdictMismatch：可比较且不一致（终态性相反，或双方都表达了下一外部
	// 边界但类型不同）。
	VerdictMismatch Verdict = "MISMATCH"
	// VerdictIncomparable：不可比较——实际下一步不可推断，或预测为 Wait/
	// Operator（未表达任何外部边界，而实际侧有下一步，两侧词汇无共享轴）。
	VerdictIncomparable Verdict = "INCOMPARABLE"
)

// Classify 把预测的 NextResult 与 legacy 实际下一步分类为三类判定。规则
// （按优先级）：
//
//  1. 实际不可推断 → 不可比较；
//  2. 任一侧为终态（实际 TERMINAL 或预测 COMPLETE）：两侧同为终态 → 一致，
//     恰一侧为终态 → 不一致；
//  3. 双方都非终态：预测表达外部边界（READY/HOST_ACTION/ASK 分别对应
//     AGENT/HOST/HUMAN）时按边界类型比对，相同 → 一致、不同 → 不一致；
//     预测为 Wait/Operator（无外部边界）→ 不可比较。
func Classify(next decision.NextResult, actual ActualNext) Verdict {
	if !actual.Inferable {
		return VerdictIncomparable
	}
	predictedTerminal := next.Kind == decision.KindComplete
	if actual.Boundary == BoundaryTerminal || predictedTerminal {
		if actual.Boundary == BoundaryTerminal && predictedTerminal {
			return VerdictMatch
		}
		return VerdictMismatch
	}
	boundary, ok := predictedBoundary(next)
	if !ok {
		return VerdictIncomparable
	}
	if boundary == actual.Boundary {
		return VerdictMatch
	}
	return VerdictMismatch
}

// predictedBoundary 把 NextResult 的 Kind 映射到共享边界词表；Wait/Operator
// 不表达外部边界，返回 ok=false。
func predictedBoundary(next decision.NextResult) (Boundary, bool) {
	switch next.Kind {
	case decision.KindReady:
		return BoundaryAgent, true
	case decision.KindHostAction:
		return BoundaryHost, true
	case decision.KindAsk:
		return BoundaryHuman, true
	}
	return "", false
}
