package shadow

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/runtime"
)

// 本文件是 Shadow 的只读 legacy 观测段：以 os.ReadFile 读取现有格式的
// run 状态文件（internal/validate/runstate.go 的 state.json，字段结构以该
// 文件为准），把可投影的字段映射为 decision 内核输入。Shadow 只消费一个
// 稳定子集，未列出的字段（admission、QA 按 mode 存储等）被 json 解码忽略，
// 不做完整性校验（StateIntegrity 校验属 legacy CLI 的写路径，Shadow 只观测
// 不鉴权）。本包不导入 internal/validate：只读保证是结构性的，投影逻辑
// 全部建立在自己解码的快照上。

// legacyState 是 Shadow 消费的 legacy run 状态子集（字段名与
// runstate.go 的 json tag 一致）。
type legacyState struct {
	RunID               string                   `json:"runId"`
	Flow                string                   `json:"flow"`
	Status              string                   `json:"status"`
	RequirementSource   string                   `json:"requirementSource"`
	RequirementRevision string                   `json:"requirementRevision"`
	BasePromptRevision  string                   `json:"basePromptRevision"`
	CatalogRevision     string                   `json:"catalogRevision"`
	VCS                 string                   `json:"vcs"`
	BaseSnapshot        string                   `json:"baseSnapshot"`
	CurrentSnapshot     string                   `json:"currentSnapshot"`
	SelectedGates       []string                 `json:"selectedGates"`
	Actions             map[string]legacyOutcome `json:"actions"`
	Gates               map[string]legacyOutcome `json:"gates"`
}

// legacyOutcome 只取 ActionResult/GateResult 的 Status 字段：Shadow 的全部
// 投影只依赖各动作/门的三态结果。
type legacyOutcome struct {
	Status string `json:"status"`
}

// legacyStatePath 返回 legacy run 状态文件路径（与 validate.RunStatePath
// 同一布局：<root>/.gates/tmp/<run-id>/state.json）。
func legacyStatePath(root, runID string) string {
	return filepath.Join(root, ".gates", "tmp", runID, "state.json")
}

// readLegacyState 只以 os.ReadFile 打开被观测状态文件，解码快照并原样返回
// 文件字节（供报告计算观测摘要）。状态内的 runId 必须与请求的 run id 一致
// （与 legacy LoadRunState 同款拒绝，防止在错误目录下观测到无关 run）。
func readLegacyState(root, runID string) (*legacyState, []byte, error) {
	data, err := os.ReadFile(legacyStatePath(root, runID))
	if err != nil {
		return nil, nil, fmt.Errorf("shadow: read legacy state: %w", err)
	}
	var state legacyState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, nil, fmt.Errorf("shadow: legacy state JSON is invalid: %w", err)
	}
	if state.RunID != runID {
		return nil, nil, fmt.Errorf("shadow: state run id %q does not match %q", state.RunID, runID)
	}
	return &state, data, nil
}

// actionStatus 返回动作状态；键缺失（旧状态或未初始化）按空串处理，由调用
// 方按"非 PASS 即待办"机械判定，不猜测额外含义。
func (s *legacyState) actionStatus(id string) string {
	if s.Actions == nil {
		return ""
	}
	return s.Actions[id].Status
}

// gateStatus 返回门状态；键缺失按空串（待执行）处理。
func (s *legacyState) gateStatus(id string) string {
	if s.Gates == nil {
		return ""
	}
	return s.Gates[id].Status
}

// requirementActionPhases 是四个 requirement 动作到引擎 run phase 的文档化
// 投影表（动作顺序即 legacy 流程顺序）。Actions 的其余键与引擎 phase 无
// 文档化对应，不参与投影（不猜测）。
var requirementActionPhases = []struct {
	action string
	phase  runtime.RunPhase
}{
	{"requirements-clarification", runtime.PhaseIntakeRegistered},
	{"product-review", runtime.PhaseProductReview},
	{"start-readiness", runtime.PhaseStartReadiness},
	{"development-worker", runtime.PhaseDevelopmentParallel},
}

// projectPhase 把 legacy 状态映射为引擎 run phase（decision.State.Phase 的
// 唯一投影来源）。规则：
//
//   - SEALED/ABORTED → TERMINAL（run 已关闭，两个模型一致）；
//   - ACTIVE：按流程顺序找第一个非 PASS 的 requirement 动作 → 对应 phase；
//   - 全部动作 PASS 后看门：第一个非 PASS 门为 FAIL → POST_REVIEW（发现项
//     处置点），为待执行 → SNAPSHOT_READY（验证组执行中）；
//   - 动作与门全部 PASS → TERMINAL：新模型在 POST_REVIEW 无义务时自动 Seal
//     （runtime 迁移表 POST_REVIEW→TERMINAL 边），引擎投影据此视为终态；
//     legacy 仍待用户显式 seal——这一模型差异由差异报告以 MISMATCH 呈现。
//
// TECHNICAL_REVIEW/TOPOLOGY_AND_ROUTE/REPAIR 等其余 phase 在 legacy 状态中
// 无承载字段，恒不产出（不猜测）。
func projectPhase(s *legacyState) (runtime.RunPhase, error) {
	switch s.Status {
	case "SEALED", "ABORTED":
		return runtime.PhaseTerminal, nil
	case "ACTIVE":
		for _, ap := range requirementActionPhases {
			if s.actionStatus(ap.action) != "PASS" {
				return ap.phase, nil
			}
		}
		for _, gate := range s.SelectedGates {
			switch s.gateStatus(gate) {
			case "PASS":
				continue
			case "FAIL":
				return runtime.PhasePostReview, nil
			default:
				return runtime.PhaseSnapshotReady, nil
			}
		}
		return runtime.PhaseTerminal, nil
	}
	return "", fmt.Errorf("shadow: legacy run status %q is not ACTIVE/SEALED/ABORTED", s.Status)
}

// inferActual 从 legacy 状态机械推断"实际下一步"（观察侧）。规则与
// projectPhase 同源但独立表述：
//
//   - SEALED/ABORTED → terminal（无下一步）；
//   - 第一个非 PASS 的 requirement 动作 → action:<id>（agent 派发；FAIL 即
//     重派，同为 agent 边界）；
//   - 动作全部 PASS 后按 SelectedGates 顺序：门待执行 → gate:<id>（宿主
//     机械动作）、门 FAIL → gate:<id>:repair（返修 agent 派发）；
//   - 动作与门全部 PASS → seal（用户拥有的收口决定）；
//   - 状态非法（不可能由 legacy CLI 写出）→ 不可推断。
func inferActual(s *legacyState) ActualNext {
	switch s.Status {
	case "SEALED", "ABORTED":
		return ActualNext{Inferable: true, Step: "terminal", Boundary: BoundaryTerminal}
	case "ACTIVE":
		for _, ap := range requirementActionPhases {
			if s.actionStatus(ap.action) != "PASS" {
				return ActualNext{Inferable: true, Step: "action:" + ap.action, Boundary: BoundaryAgent}
			}
		}
		for _, gate := range s.SelectedGates {
			switch s.gateStatus(gate) {
			case "PASS":
				continue
			case "FAIL":
				return ActualNext{Inferable: true, Step: "gate:" + gate + ":repair", Boundary: BoundaryAgent}
			default:
				return ActualNext{Inferable: true, Step: "gate:" + gate, Boundary: BoundaryHost}
			}
		}
		return ActualNext{Inferable: true, Step: "seal", Boundary: BoundaryHuman}
	}
	return ActualNext{}
}

// legacyVCS / legacyFile 是两个只读收集器：把 legacy 状态中可映射到封闭
// 事实来源的字段（VCS 快照身份；需求/prompt/catalog 工件版本）交给
// decision.Observe 汇成规范 Observation。字段为空时不产生事实（不猜测值）。
type legacyVCS struct{ state *legacyState }

func (c *legacyVCS) Source() decision.FactSource { return decision.SourceVCS }

func (c *legacyVCS) Collect(*decision.State) ([]decision.Fact, error) {
	return factsOf(decision.SourceVCS,
		"vcs", c.state.VCS,
		"baseSnapshot", c.state.BaseSnapshot,
		"currentSnapshot", c.state.CurrentSnapshot,
	), nil
}

type legacyFile struct{ state *legacyState }

func (c *legacyFile) Source() decision.FactSource { return decision.SourceFile }

func (c *legacyFile) Collect(*decision.State) ([]decision.Fact, error) {
	return factsOf(decision.SourceFile,
		"requirementSource", c.state.RequirementSource,
		"requirementRevision", c.state.RequirementRevision,
		"basePromptRevision", c.state.BasePromptRevision,
		"catalogRevision", c.state.CatalogRevision,
	), nil
}

// factsOf 组装单一来源的事实，跳过空值字段（legacy 状态不承载该事实时
// 不虚构）。
func factsOf(source decision.FactSource, pairs ...string) []decision.Fact {
	var facts []decision.Fact
	for i := 0; i+1 < len(pairs); i += 2 {
		if pairs[i+1] != "" {
			facts = append(facts, decision.Fact{Source: source, Key: pairs[i], Value: pairs[i+1]})
		}
	}
	return facts
}
