package whitebox_qa

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/shadow"
)

// 用例：Shadow 对 ACTIVE legacy run 输出完整 telemetry——状态摘要、投影
// （phase/completed/facts/unavailable sources）、决策内核预测（三类 digest +
// PlanDigest + 初始 frontier + WAIT 原因）、实际下一步与差异判定；预测与
// 独立复算的 Decide 结果逐字段一致；reportDigest 绑定报告内容。
func TestShadowRunReportsProjectionPredictionAndVerdict(t *testing.T) {
	root := t.TempDir()
	stateJSON := legacyRunJSON("r-1", "ACTIVE", allPassActions(),
		map[string]string{"gate.build": "PASS", "gate.qa": ""}, "gate.build", "gate.qa")
	statePath := writeLegacyRun(t, root, "r-1", stateJSON)
	stateBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	report, err := shadow.Run(shadow.Options{Root: root, RunID: "r-1", OutputDir: t.TempDir()})
	if err != nil {
		t.Fatalf("shadow run: %v", err)
	}

	// 被观测状态摘要。
	if report.Schema != "formal-gates.shadow-report/1" || report.RunID != "r-1" ||
		report.LegacyStatus != "ACTIVE" || report.LegacyFlow != "no-split" {
		t.Fatalf("report header = %+v", report)
	}
	if report.ObservedStatePath != ".gates/tmp/r-1/state.json" {
		t.Fatalf("observed path = %q, want relative .gates/tmp layout", report.ObservedStatePath)
	}
	if got, want := report.ObservedStateSHA256, encoder.Digest(stateBytes); got != want {
		t.Fatalf("observed sha = %s, want %s", got, want)
	}

	// 投影：phase 按文档化映射表；完成度恒为空（不猜测）。
	if report.ProjectedPhase != string(runtime.PhaseSnapshotReady) {
		t.Fatalf("projected phase = %s, want SNAPSHOT_READY", report.ProjectedPhase)
	}
	if len(report.ProjectedCompleted) != 0 {
		t.Fatalf("projected completed = %v, want empty (legacy state carries no engine progress)", report.ProjectedCompleted)
	}

	// 事实：VCS/FILE 可映射字段，规范排序；不可用来源固定标注四类。
	wantFacts := []struct{ source, key, value string }{
		{"FILE", "basePromptRevision", "bp1"},
		{"FILE", "catalogRevision", "cat1"},
		{"FILE", "requirementRevision", "rev1"},
		{"FILE", "requirementSource", "req.md"},
		{"VCS", "baseSnapshot", "b0"},
		{"VCS", "currentSnapshot", "c1"},
		{"VCS", "vcs", "git"},
	}
	if len(report.Facts) != len(wantFacts) {
		t.Fatalf("facts = %+v, want %+v", report.Facts, wantFacts)
	}
	for i, w := range wantFacts {
		f := report.Facts[i]
		if f.Source != w.source || f.Key != w.key || f.Value != w.value {
			t.Fatalf("facts[%d] = %+v, want %+v", i, f, w)
		}
	}
	if len(report.UnavailableSources) != 4 ||
		report.UnavailableSources[0].Source != "HOST" || report.UnavailableSources[1].Source != "LIFECYCLE" ||
		report.UnavailableSources[2].Source != "RECEIPT" || report.UnavailableSources[3].Source != "CAPACITY" {
		t.Fatalf("unavailable sources = %+v, want HOST/LIFECYCLE/RECEIPT/CAPACITY", report.UnavailableSources)
	}

	// 预测：与独立复算的 Decide 逐字段一致（digest 绑定 definition/state/obs/plan）。
	compiled, err := compiler.Compile(definition.Workflow(), definition.Registry())
	if err != nil {
		t.Fatal(err)
	}
	freshState, err := decision.NewState(definition.Version, runtime.PhaseSnapshotReady)
	if err != nil {
		t.Fatal(err)
	}
	obsFacts := make([]decision.Fact, 0, len(wantFacts))
	for _, w := range wantFacts {
		obsFacts = append(obsFacts, decision.Fact{Source: decision.FactSource(w.source), Key: w.key, Value: w.value})
	}
	obs := decision.Observation{Facts: obsFacts}
	plan, err := decision.Decide(freshState, obs, compiled)
	if err != nil {
		t.Fatal(err)
	}
	planDigest, err := plan.Digest()
	if err != nil {
		t.Fatal(err)
	}
	pred := report.Prediction
	if pred.DefinitionDigest != definition.WorkflowDefinitionDigest ||
		pred.StateDigest != plan.StateDigest || pred.ObservationDigest != plan.ObservationDigest ||
		pred.PlanDigest != planDigest {
		t.Fatalf("prediction digests = {%s %s %s %s}, want definition identity + recomputed decide", pred.DefinitionDigest, pred.StateDigest, pred.ObservationDigest, pred.PlanDigest)
	}
	if pred.NextKind != string(decision.KindWait) || pred.NextReason != string(decision.WaitEngineInternal) {
		t.Fatalf("prediction next = %s/%s, want WAIT/ENGINE_INTERNAL", pred.NextKind, pred.NextReason)
	}
	if len(pred.Frontier) != 1 || pred.Frontier[0].Step != "entry.parse" ||
		pred.Frontier[0].Ordinal != 0 || pred.Frontier[0].Kind != string(compiler.KindLocal) {
		t.Fatalf("prediction frontier = %+v, want [entry.parse(0, LOCAL)]", pred.Frontier)
	}

	// 实际下一步与差异判定（预测 WAIT 不表达外部边界 → 不可比较）。
	if !report.Actual.Inferable || report.Actual.Step != "gate:gate.qa" || string(report.Actual.Boundary) != "HOST" {
		t.Fatalf("actual = %+v, want gate:gate.qa/HOST", report.Actual)
	}
	if report.Verdict != shadow.VerdictIncomparable {
		t.Fatalf("verdict = %s, want INCOMPARABLE (WAIT expresses no external boundary)", report.Verdict)
	}

	// reportDigest 绑定报告内容（digest 置空后的 canonical 编码字节）。
	rep := *report
	rep.ReportDigest = ""
	rep.OutPath = ""
	base := canonicalEncode(t, &rep)
	if got, want := report.ReportDigest, encoder.Digest(base); got != want {
		t.Fatalf("reportDigest = %s, want %s", got, want)
	}
}

// 用例：projectPhase/inferActual 的文档化映射表——requirement 动作顺序、
// 门 FAIL→POST_REVIEW、门待执行→SNAPSHOT_READY、全部通过→引擎自动 Seal
// 投影为 TERMINAL（与 legacy 待 seal 的模型差异以 MISMATCH 呈现）、
// SEALED/ABORTED→TERMINAL/MATCH；输入缺陷（缺文件、run id 不符、非法
// 状态、路径分隔符、空 run id/root）拒绝。
func TestShadowPhaseProjectionTableAndInputDefects(t *testing.T) {
	rows := []struct {
		name          string
		stateJSON     func() string
		projected     string
		actualStep    string
		actualBound   string
		verdict       shadow.Verdict
		nextKindCheck bool
	}{
		{name: "first requirement action pending",
			stateJSON: func() string { return legacyRunJSON("r-1", "ACTIVE", nil, nil) },
			projected: "INTAKE_REGISTERED", actualStep: "action:requirements-clarification", actualBound: "AGENT",
			verdict: shadow.VerdictIncomparable},
		{name: "product review pending",
			stateJSON: func() string {
				return legacyRunJSON("r-1", "ACTIVE", map[string]string{"requirements-clarification": "PASS"}, nil)
			},
			projected: "PRODUCT_REVIEW", actualStep: "action:product-review", actualBound: "AGENT",
			verdict: shadow.VerdictIncomparable},
		{name: "start readiness pending",
			stateJSON: func() string {
				return legacyRunJSON("r-1", "ACTIVE", map[string]string{
					"requirements-clarification": "PASS", "product-review": "PASS"}, nil)
			},
			projected: "START_READINESS", actualStep: "action:start-readiness", actualBound: "AGENT",
			verdict: shadow.VerdictIncomparable},
		{name: "development worker pending",
			stateJSON: func() string {
				return legacyRunJSON("r-1", "ACTIVE", map[string]string{
					"requirements-clarification": "PASS", "product-review": "PASS", "start-readiness": "PASS"}, nil)
			},
			projected: "DEVELOPMENT_PARALLEL", actualStep: "action:development-worker", actualBound: "AGENT",
			verdict: shadow.VerdictIncomparable},
		{name: "gate FAIL lands on post review",
			stateJSON: func() string {
				return legacyRunJSON("r-1", "ACTIVE", allPassActions(),
					map[string]string{"gate.build": "FAIL"}, "gate.build", "gate.qa")
			},
			projected: "POST_REVIEW", actualStep: "gate:gate.build:repair", actualBound: "AGENT",
			verdict: shadow.VerdictIncomparable},
		{name: "all actions and gates pass: engine auto-seal vs legacy manual seal",
			stateJSON: func() string {
				return legacyRunJSON("r-1", "ACTIVE", allPassActions(),
					map[string]string{"gate.build": "PASS", "gate.qa": "PASS"}, "gate.build", "gate.qa")
			},
			projected: "TERMINAL", actualStep: "seal", actualBound: "HUMAN",
			verdict: shadow.VerdictMismatch, nextKindCheck: true},
	}
	for _, row := range rows {
		root := t.TempDir()
		writeLegacyRun(t, root, "r-1", row.stateJSON())
		report, err := shadow.Run(shadow.Options{Root: root, RunID: "r-1", OutputDir: t.TempDir()})
		if err != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		if report.ProjectedPhase != row.projected {
			t.Errorf("%s: projected phase = %s, want %s", row.name, report.ProjectedPhase, row.projected)
		}
		if report.Actual.Step != row.actualStep || string(report.Actual.Boundary) != row.actualBound {
			t.Errorf("%s: actual = %s/%s, want %s/%s", row.name, report.Actual.Step, report.Actual.Boundary, row.actualStep, row.actualBound)
		}
		if report.Verdict != row.verdict {
			t.Errorf("%s: verdict = %s, want %s", row.name, report.Verdict, row.verdict)
		}
		if row.nextKindCheck && report.Prediction.NextKind != string(decision.KindComplete) {
			t.Errorf("%s: projected TERMINAL must predict COMPLETE, got %s", row.name, report.Prediction.NextKind)
		}
	}

	// SEALED/ABORTED → TERMINAL/MATCH。
	for _, status := range []string{"SEALED", "ABORTED"} {
		root := t.TempDir()
		writeLegacyRun(t, root, "r-1", legacyRunJSON("r-1", status, allPassActions(),
			map[string]string{"gate.build": "PASS", "gate.qa": "PASS"}, "gate.build", "gate.qa"))
		report, err := shadow.Run(shadow.Options{Root: root, RunID: "r-1", OutputDir: t.TempDir()})
		if err != nil {
			t.Fatalf("%s: %v", status, err)
		}
		if report.ProjectedPhase != "TERMINAL" || report.Verdict != shadow.VerdictMatch ||
			report.Prediction.NextKind != string(decision.KindComplete) {
			t.Errorf("%s: projected = %s verdict = %s next = %s, want TERMINAL/MATCH/COMPLETE",
				status, report.ProjectedPhase, report.Verdict, report.Prediction.NextKind)
		}
	}

	// 输入缺陷。
	root := t.TempDir()
	if _, err := shadow.Run(shadow.Options{Root: root, RunID: "ghost", OutputDir: t.TempDir()}); err == nil {
		t.Error("missing legacy state must be an error")
	} else {
		wantErrContaining(t, err, "read legacy state")
	}
	writeLegacyRun(t, root, "r-mismatch", legacyRunJSON("other", "ACTIVE", nil, nil))
	if _, err := shadow.Run(shadow.Options{Root: root, RunID: "r-mismatch", OutputDir: t.TempDir()}); err == nil {
		t.Error("run id mismatch must be rejected")
	} else {
		wantErrContaining(t, err, "does not match")
	}
	writeLegacyRun(t, root, "r-paused", legacyRunJSON("r-paused", "PAUSED", nil, nil))
	if _, err := shadow.Run(shadow.Options{Root: root, RunID: "r-paused", OutputDir: t.TempDir()}); err == nil {
		t.Error("unknown legacy status must be rejected")
	} else {
		wantErrContaining(t, err, "not ACTIVE/SEALED/ABORTED")
	}
	if _, err := shadow.Run(shadow.Options{Root: root, RunID: "../escape", OutputDir: t.TempDir()}); err == nil {
		t.Error("run id with path separators must be rejected")
	} else {
		wantErrContaining(t, err, "path separators")
	}
	if _, err := shadow.Run(shadow.Options{Root: root, RunID: "  ", OutputDir: t.TempDir()}); err == nil {
		t.Error("empty run id must be rejected")
	}
	if _, err := shadow.Run(shadow.Options{Root: "", RunID: "r-1", OutputDir: t.TempDir()}); err == nil {
		t.Error("empty root must be rejected")
	}
}

// 用例：Shadow 只读且确定——默认 telemetry 只写 <root>/.gates/shadow/，
// 绝不写 .gates/tmp/（被观测状态宿主目录）；同输入两次执行产出逐字节相同
// 的报告文件。
func TestShadowIsReadOnlyAndDeterministic(t *testing.T) {
	root := t.TempDir()
	stateJSON := legacyRunJSON("rw", "ACTIVE", allPassActions(),
		map[string]string{"gate.build": "PASS"}, "gate.build", "gate.qa")
	writeLegacyRun(t, root, "rw", stateJSON)
	before := snapshotTree(t, filepath.Join(root, ".gates", "tmp"))

	report, err := shadow.Run(shadow.Options{Root: root, RunID: "rw"}) // OutputDir 留空 → 默认目录
	if err != nil {
		t.Fatalf("shadow run: %v", err)
	}
	wantPath := filepath.Join(root, ".gates", "shadow", "rw.shadow.json")
	if report.OutPath != wantPath {
		t.Fatalf("out path = %s, want default %s", report.OutPath, wantPath)
	}
	if bytes.Contains([]byte(report.OutPath), []byte(".gates/tmp")) {
		t.Fatal("telemetry must never land under .gates/tmp")
	}

	after := snapshotTree(t, filepath.Join(root, ".gates", "tmp"))
	if len(before) != 1 || len(after) != 1 {
		t.Fatalf(".gates/tmp layout changed: before %v after %v", before, after)
	}
	for name, sum := range before {
		if after[name] != sum {
			t.Fatalf("observed state %s mutated by shadow", name)
		}
	}
	first, err := os.ReadFile(wantPath)
	if err != nil {
		t.Fatal(err)
	}

	// 第二次执行（显式输出目录）：报告文件逐字节相同。
	secondDir := t.TempDir()
	report2, err := shadow.Run(shadow.Options{Root: root, RunID: "rw", OutputDir: secondDir})
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.ReadFile(filepath.Join(secondDir, "rw.shadow.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("same input must produce byte-identical shadow reports")
	}
	if report2.ReportDigest != report.ReportDigest {
		t.Fatal("report digest must be stable across runs")
	}
}

// snapshotTree 记录目录下全部文件的相对路径 → 内容摘要。
func snapshotTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		out[rel] = encoder.Digest(data)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
