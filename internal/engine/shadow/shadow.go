// Package shadow 是阶段 1 批 4 的只读 Shadow harness（master-requirements
// §6、design §6）：只读观测一个 legacy run，运行决策内核输出 eligible
// frontier 预测与差异报告，不写权威 state、不触发副作用。
//
// 观测对象是现有格式的 legacy run 状态文件（<root>/.gates/tmp/<run-id>/
// state.json，结构以 internal/validate/runstate.go 为准）。Shadow 把它投影
// 为 decision 内核输入：State（phase 由文档化映射表从 Status/Actions/Gates
// 派生；引擎步骤完成度恒为空——legacy 状态不承载引擎步骤进度，不猜测）与
// Observation（VCS 快照身份与需求工件版本映射为封闭事实来源
// VCS/FILE；HOST/LIFECYCLE/RECEIPT/CAPACITY 在 legacy 状态中无承载字段，
// 报告中标注为不可用）。预测由 definition.Workflow() 的编译产物驱动
// decision.Decide 得出；与从 legacy 状态机械推断的实际下一步（若可推断）
// 经 Classify 得出一致/不一致/不可比较三类差异判定。
//
// 只读保证是结构性的：全路径只以 os.ReadFile 打开被观测状态文件，唯一的
// 写入是 telemetry 报告，且只落调用方显式指定的输出目录（默认
// <root>/.gates/shadow/），绝不写 .gates/tmp/。公开 CLI 面冻结——本包不
// 新增子命令；文档化的收口调用方式：
//
//	go test ./internal/engine/shadow -run 'TestShadow' -v
//
// 包级 API（测试/收口集成用）：
//
//	report, err := shadow.Run(shadow.Options{Root: root, RunID: runID})
//
// 报告字节确定：同输入两次执行产出逐字节相同的报告（无时间、无路径遍历
// 序），reportDigest 是报告内容（digest 字段置空后 canonical 编码）的
// SHA-256 摘要，绑定本次观测字节。
package shadow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
)

// reportSchema 是 telemetry 报告的 schema 标识。
const reportSchema = "formal-gates.shadow-report/1"

// Options 是 Shadow 的全部输入。Root 是被观测项目根（包含 .gates/）；
// RunID 是 .gates/tmp/ 下的 legacy run id；OutputDir 为空时取
// DefaultOutputDir(Root)。
type Options struct {
	Root      string
	RunID     string
	OutputDir string
}

// DefaultOutputDir 返回 telemetry 默认输出目录 <root>/.gates/shadow/。
// Shadow 绝不写 .gates/tmp/（被观测状态的宿主目录）。
func DefaultOutputDir(root string) string {
	return filepath.Join(root, ".gates", "shadow")
}

// Report 是一次 Shadow 观测的完整 telemetry：被观测状态摘要、投影结果、
// 预测、实际下一步与差异判定。OutPath 是报告落盘路径（不进入 JSON，保持
// 报告字节与输出目录无关）。
type Report struct {
	Schema              string `json:"schema"`
	RunID               string `json:"runId"`
	LegacyFlow          string `json:"legacyFlow"`
	LegacyStatus        string `json:"legacyStatus"`
	ObservedStatePath   string `json:"observedStatePath"`
	ObservedStateSHA256 string `json:"observedStateSha256"`
	ProjectedPhase      string `json:"projectedPhase"`
	// ProjectedCompleted 恒为空：legacy 状态不承载引擎定义的步骤完成度，
	// 投影始终是"全新引擎 run"（预测 frontier 恒为定义的初始 frontier）。
	ProjectedCompleted []string            `json:"projectedCompleted"`
	Facts              []ReportFact        `json:"facts"`
	UnavailableSources []UnavailableSource `json:"unavailableSources"`
	Prediction         PredictionReport    `json:"prediction"`
	Actual             ActualNext          `json:"actual"`
	Verdict            Verdict             `json:"verdict"`
	// ReportDigest 绑定报告内容：对 digest 字段置空后的 canonical 编码字节
	// 取 SHA-256（sha256: 前缀）。同输入恒得同摘要。
	ReportDigest string `json:"reportDigest"`
	OutPath      string `json:"-"`
}

// ReportFact 是投影进 Observation 的一条事实（与 decision.Fact 同构的
// wire 形态）。
type ReportFact struct {
	Source string `json:"source"`
	Key    string `json:"key"`
	Value  string `json:"value"`
}

// UnavailableSource 标注一个未能从 legacy 状态映射的封闭事实来源及原因
// （不猜测、不虚构空事实）。
type UnavailableSource struct {
	Source string `json:"source"`
	Reason string `json:"reason"`
}

// PredictionReport 是决策内核的预测：三类输入 digest + PlanDigest + 完整
// eligible frontier + NextResult。
type PredictionReport struct {
	DefinitionDigest  string          `json:"definitionDigest"`
	StateDigest       string          `json:"stateDigest"`
	ObservationDigest string          `json:"observationDigest"`
	PlanDigest        string          `json:"planDigest"`
	Frontier          []FrontierEntry `json:"frontier"`
	NextKind          string          `json:"nextKind"`
	NextReason        string          `json:"nextReason,omitempty"`
}

// FrontierEntry 是预测 frontier 的单个条目（与 decision.FrontierEntry
// 同构的 wire 形态）。
type FrontierEntry struct {
	Step    string `json:"step"`
	Node    string `json:"node"`
	Ordinal int    `json:"ordinal"`
	Kind    string `json:"kind"`
}

// unavailableFactSources 是六类封闭事实来源中 legacy 状态不承载、Shadow
// 阶段 1 无法映射的来源（真实收集器属后续批次）；顺序固定，进入报告。
var unavailableFactSources = []UnavailableSource{
	{Source: string(decision.SourceHost), Reason: "legacy run 状态不承载宿主/bridge 可用性或 provider 配对事实"},
	{Source: string(decision.SourceLifecycle), Reason: "legacy run 状态不承载 agent lifecycle 事件"},
	{Source: string(decision.SourceReceipt), Reason: "legacy run 状态不承载 SpawnReceipt/HostAction receipt"},
	{Source: string(decision.SourceCapacity), Reason: "legacy run 状态不承载可用签发容量"},
}

// Run 执行一次只读 Shadow 观测：读取 legacy 状态 → 投影内核输入 → 编译
// definition.Workflow() 并 Decide → 推断实际下一步并分类 → 写 telemetry
// 报告。除 OutputDir（默认 <root>/.gates/shadow/）下的报告文件外不发生
// 任何写入。
func Run(opts Options) (*Report, error) {
	runID := strings.TrimSpace(opts.RunID)
	if runID == "" {
		return nil, fmt.Errorf("shadow: run id required")
	}
	// 常见操作失误防线：run id 含路径分隔符会把 telemetry 写出输出目录或
	// 观测到目录外的状态文件，直接拒绝。
	if strings.ContainsAny(runID, `/\`) {
		return nil, fmt.Errorf("shadow: run id %q must not contain path separators", runID)
	}
	// 同族防线：run id ".." 是父目录引用，会把观测路径越出 .gates/tmp
	//（封板后审计 H6）。
	if runID == ".." {
		return nil, fmt.Errorf("shadow: run id %q must not be a parent-directory reference", runID)
	}
	if strings.TrimSpace(opts.Root) == "" {
		return nil, fmt.Errorf("shadow: root required")
	}

	legacy, stateBytes, err := readLegacyState(opts.Root, runID)
	if err != nil {
		return nil, err
	}

	phase, err := projectPhase(legacy)
	if err != nil {
		return nil, err
	}
	state, err := decision.NewState(definition.Version, phase)
	if err != nil {
		return nil, fmt.Errorf("shadow: %w", err)
	}
	obs, err := decision.Observe(state, []decision.Collector{
		&legacyVCS{state: legacy},
		&legacyFile{state: legacy},
	})
	if err != nil {
		return nil, fmt.Errorf("shadow: %w", err)
	}
	compiled, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		return nil, fmt.Errorf("shadow: compile definition: %w", err)
	}
	plan, err := decision.Decide(state, obs, compiled)
	if err != nil {
		return nil, fmt.Errorf("shadow: decide: %w", err)
	}
	planDigest, err := plan.Digest()
	if err != nil {
		return nil, fmt.Errorf("shadow: %w", err)
	}

	actual := inferActual(legacy)
	report := &Report{
		Schema:              reportSchema,
		RunID:               legacy.RunID,
		LegacyFlow:          legacy.Flow,
		LegacyStatus:        legacy.Status,
		ObservedStatePath:   filepath.ToSlash(filepath.Join(".gates", "tmp", runID, "state.json")),
		ObservedStateSHA256: encoder.Digest(stateBytes),
		ProjectedPhase:      string(phase),
		ProjectedCompleted:  []string{},
		Facts:               make([]ReportFact, 0, len(obs.Facts)),
		UnavailableSources:  unavailableFactSources,
		Prediction: PredictionReport{
			DefinitionDigest:  plan.DefinitionDigest,
			StateDigest:       plan.StateDigest,
			ObservationDigest: plan.ObservationDigest,
			PlanDigest:        planDigest,
			Frontier:          make([]FrontierEntry, 0, len(plan.Frontier)),
			NextKind:          string(plan.Next.Kind),
		},
		Actual:  actual,
		Verdict: Classify(plan.Next, actual),
	}
	for _, f := range obs.Facts {
		report.Facts = append(report.Facts, ReportFact{Source: string(f.Source), Key: f.Key, Value: f.Value})
	}
	for _, e := range plan.Frontier {
		report.Prediction.Frontier = append(report.Prediction.Frontier,
			FrontierEntry{Step: string(e.Step), Node: string(e.Node), Ordinal: e.Ordinal, Kind: string(e.Kind)})
	}
	if plan.Next.Wait != nil {
		report.Prediction.NextReason = string(plan.Next.Wait.Reason)
	}
	if err := writeReport(opts, runID, report); err != nil {
		return nil, err
	}
	return report, nil
}

// writeReport 把报告 canonical 编码后写入输出目录（默认
// <root>/.gates/shadow/）下的 <run-id>.shadow.json。digest 先对置空自身
// 的 canonical 字节计算，再回填写盘（与 legacy StateIntegrity 同一模式）。
func writeReport(opts Options, runID string, report *Report) error {
	outDir := opts.OutputDir
	if outDir == "" {
		outDir = DefaultOutputDir(opts.Root)
	}
	base, err := canonicalJSON(report)
	if err != nil {
		return err
	}
	report.ReportDigest = encoder.Digest(base)
	data, err := canonicalJSON(report)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(outDir, 0o700); err != nil {
		return fmt.Errorf("shadow: telemetry output dir: %w", err)
	}
	path := filepath.Join(outDir, runID+".shadow.json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("shadow: write telemetry report: %w", err)
	}
	report.OutPath = path
	return nil
}

// canonicalJSON 是与 decision/encoder 一致的 canonical 编码：JSON、2 空格
// 缩进、不转义 HTML、恰一个尾随换行。Report 只含 string/int/bool 与有序
// 切片，字节输出只是数据的函数。
func canonicalJSON(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		return nil, fmt.Errorf("shadow: canonical encode: %w", err)
	}
	return buf.Bytes(), nil
}
