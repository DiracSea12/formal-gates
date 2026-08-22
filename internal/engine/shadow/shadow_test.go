// shadow_test.go 是 Shadow harness 的端到端验收测试（对应黑盒用例
// CASE-016：只读 E2E——只读 legacy 状态、预测与 fixture 期望 frontier 直接
// 比对、不写权威 state、telemetry 只落显式输出目录、同输入输出字节稳定）。
// fixture 由 legacy writer（validate.NewRunState + SaveRunState）在隔离项目
// 根构造，保证与现有 state.json 格式逐字段一致。
package shadow_test

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"formal-gates/internal/engine/shadow"
	"formal-gates/internal/validate"
)

// newLegacyRun 在隔离项目根写入一个真实 legacy run 状态；mutate 允许用例
// 推进动作/门/状态。返回状态文件路径。
func newLegacyRun(t *testing.T, root, runID string, mutate func(*validate.RunState)) string {
	t.Helper()
	state := validate.NewRunState(runID, "formal-gates", "req/source.md", "sha256:req", "git",
		"base-snap", "curr-snap", "bp-rev", "cat-rev", true,
		[]string{"gate.build", "gate.test"}, nil)
	if mutate != nil {
		mutate(&state)
	}
	if err := validate.SaveRunState(root, state); err != nil {
		t.Fatalf("save legacy run state: %v", err)
	}
	return validate.RunStatePath(root, runID)
}

func passAllActions(s *validate.RunState) {
	for id := range s.Actions {
		s.Actions[id] = validate.ActionResult{Status: "PASS"}
	}
}

// selectGates 复刻 legacy run 走完 route 选择后的形态：SelectedGates 记录
// 选定的门顺序（NewRunState 初始为空，真实 run 由 SetRoute 填写），Gates
// 按每个门登记状态。
func selectGates(s *validate.RunState, ids ...string) {
	s.SelectedGates = append([]string{}, ids...)
	for _, id := range ids {
		s.Gates[id] = validate.GateResult{Status: "PENDING"}
	}
}

func passAllGates(s *validate.RunState) {
	for id := range s.Gates {
		s.Gates[id] = validate.GateResult{Status: "PASS"}
	}
}

// fileMeta 记录一个路径的完整观测摘要：类型、大小、权限、mtime 与内容
// sha256（目录无内容摘要）。前后两次快照逐项相等即证明被观测树未被写
// 入（含 mtime 不变）。
type fileMeta struct {
	IsDir   bool
	Size    int64
	Mode    os.FileMode
	ModTime time.Time
	SHA256  string
}

func snapshotTree(t *testing.T, root string) map[string]fileMeta {
	t.Helper()
	out := map[string]fileMeta{}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		key := filepath.ToSlash(rel)
		meta := fileMeta{IsDir: info.IsDir(), Size: info.Size(), Mode: info.Mode(), ModTime: info.ModTime()}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			sum := sha256.Sum256(data)
			meta.SHA256 = hex.EncodeToString(sum[:])
		}
		out[key] = meta
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot tree %s: %v", root, err)
	}
	return out
}

// TestShadowReadOnlyObservesWithoutWriting 断言只读保证：Shadow 执行前后，
// 被观测项目全树（含 .gates/tmp/<run-id>/state.json 的 sha256 与 mtime）
// 逐项不变；telemetry 恰好一个文件且落在显式指定的外部输出目录。
func TestShadowReadOnlyObservesWithoutWriting(t *testing.T) {
	root := t.TempDir()
	runID := "shadow-readonly"
	newLegacyRun(t, root, runID, nil)
	before := snapshotTree(t, root)

	outDir := filepath.Join(t.TempDir(), "telemetry")
	report, err := shadow.Run(shadow.Options{Root: root, RunID: runID, OutputDir: outDir})
	if err != nil {
		t.Fatalf("shadow run: %v", err)
	}
	after := snapshotTree(t, root)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("observed project tree changed by shadow run")
	}
	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("read telemetry dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != runID+".shadow.json" {
		t.Fatalf("telemetry dir must hold exactly one report %q, got %v", runID+".shadow.json", entries)
	}
	if want := filepath.Join(outDir, runID+".shadow.json"); report.OutPath != want {
		t.Fatalf("report path = %q, want %q", report.OutPath, want)
	}
}

// TestShadowTelemetryDefaultDir 断言默认落点约束：OutputDir 缺省时报告写
// <root>/.gates/shadow/<run-id>.shadow.json；被观测项目树前后只新增这一个
// 文件，.gates/tmp/ 下无任何新增或改动。
func TestShadowTelemetryDefaultDir(t *testing.T) {
	root := t.TempDir()
	runID := "shadow-default"
	newLegacyRun(t, root, runID, nil)
	before := snapshotTree(t, root)

	report, err := shadow.Run(shadow.Options{Root: root, RunID: runID})
	if err != nil {
		t.Fatalf("shadow run: %v", err)
	}
	after := snapshotTree(t, root)
	var added []string
	for key, meta := range after {
		if _, ok := before[key]; !ok {
			added = append(added, key)
		} else if after[key] != meta {
			t.Fatalf("pre-existing path %q changed by shadow run", key)
		}
	}
	// 新增只允许默认 telemetry 目录本身与其中恰好一个报告文件。
	wantAdded := []string{".gates/shadow", ".gates/shadow/" + runID + ".shadow.json"}
	if len(added) != len(wantAdded) {
		t.Fatalf("shadow must add only the default telemetry dir + report, added=%v", added)
	}
	for _, key := range added {
		found := false
		for _, want := range wantAdded {
			if key == want {
				found = true
			}
		}
		if !found {
			t.Fatalf("unexpected new path %q under observed root", key)
		}
	}
	if want := filepath.Join(root, ".gates", "shadow", runID+".shadow.json"); report.OutPath != want {
		t.Fatalf("report path = %q, want %q", report.OutPath, want)
	}
}

// TestShadowDeterministicBytes 断言确定性：同输入两次执行（不同输出目录）
// 的报告字节逐字节相同、digest 相同；同目录重跑仍稳定。
func TestShadowDeterministicBytes(t *testing.T) {
	root := t.TempDir()
	runID := "shadow-stable"
	newLegacyRun(t, root, runID, nil)

	first, err := shadow.Run(shadow.Options{Root: root, RunID: runID, OutputDir: filepath.Join(t.TempDir(), "a")})
	if err != nil {
		t.Fatalf("shadow run 1: %v", err)
	}
	second, err := shadow.Run(shadow.Options{Root: root, RunID: runID, OutputDir: filepath.Join(t.TempDir(), "b")})
	if err != nil {
		t.Fatalf("shadow run 2: %v", err)
	}
	firstBytes, err := os.ReadFile(first.OutPath)
	if err != nil {
		t.Fatalf("read report 1: %v", err)
	}
	secondBytes, err := os.ReadFile(second.OutPath)
	if err != nil {
		t.Fatalf("read report 2: %v", err)
	}
	if string(firstBytes) != string(secondBytes) {
		t.Fatalf("report bytes differ across identical inputs:\n%s\n%s", firstBytes, secondBytes)
	}
	if first.ReportDigest != second.ReportDigest {
		t.Fatalf("report digest differs: %q vs %q", first.ReportDigest, second.ReportDigest)
	}
	third, err := shadow.Run(shadow.Options{Root: root, RunID: runID, OutputDir: filepath.Dir(first.OutPath)})
	if err != nil {
		t.Fatalf("shadow run 3: %v", err)
	}
	thirdBytes, err := os.ReadFile(third.OutPath)
	if err != nil {
		t.Fatalf("read report 3: %v", err)
	}
	if string(thirdBytes) != string(firstBytes) {
		t.Fatalf("re-run into same dir changed report bytes")
	}
}

// TestShadowFixturePredictions 是 CASE-016 的核心比对：已知 legacy state
// fixture 的预测 frontier 与测试中显式声明的期望 frontier 直接比对。投影
// 不承载引擎步骤完成度（legacy 状态无此数据），预测 frontier 恒为定义的
// 初始 frontier（entry.parse，compiled ordinal 0，LOCAL）；各 fixture 同时
// 钉死 phase 投影、NextResult、实际下一步推断与三类差异判定。
func TestShadowFixturePredictions(t *testing.T) {
	initialFrontier := []shadow.FrontierEntry{
		{Step: "entry.parse", Node: "entry", Ordinal: 0, Kind: "LOCAL"},
	}
	cases := []struct {
		name       string
		mutate     func(*validate.RunState)
		phase      string
		nextKind   string
		nextReason string
		actual     shadow.ActualNext
		verdict    shadow.Verdict
	}{
		{
			name:       "active intake",
			mutate:     nil,
			phase:      "INTAKE_REGISTERED",
			nextKind:   "WAIT",
			nextReason: "ENGINE_INTERNAL",
			actual:     shadow.ActualNext{Inferable: true, Step: "action:requirements-clarification", Boundary: shadow.BoundaryAgent},
			verdict:    shadow.VerdictIncomparable,
		},
		{
			name: "active snapshot ready",
			mutate: func(s *validate.RunState) {
				passAllActions(s)
				selectGates(s, "gate.build", "gate.test")
			},
			phase:      "SNAPSHOT_READY",
			nextKind:   "WAIT",
			nextReason: "ENGINE_INTERNAL",
			actual:     shadow.ActualNext{Inferable: true, Step: "gate:gate.build", Boundary: shadow.BoundaryHost},
			verdict:    shadow.VerdictIncomparable,
		},
		{
			name: "active gate failed",
			mutate: func(s *validate.RunState) {
				passAllActions(s)
				selectGates(s, "gate.build", "gate.test")
				s.Gates["gate.build"] = validate.GateResult{Status: "FAIL"}
			},
			phase:      "POST_REVIEW",
			nextKind:   "WAIT",
			nextReason: "ENGINE_INTERNAL",
			actual:     shadow.ActualNext{Inferable: true, Step: "gate:gate.build:repair", Boundary: shadow.BoundaryAgent},
			verdict:    shadow.VerdictIncomparable,
		},
		{
			name: "active seal pending",
			mutate: func(s *validate.RunState) {
				passAllActions(s)
				selectGates(s, "gate.build", "gate.test")
				passAllGates(s)
			},
			phase:    "TERMINAL",
			nextKind: "COMPLETE",
			actual:   shadow.ActualNext{Inferable: true, Step: "seal", Boundary: shadow.BoundaryHuman},
			verdict:  shadow.VerdictMismatch,
		},
		{
			name: "sealed",
			mutate: func(s *validate.RunState) {
				passAllActions(s)
				selectGates(s, "gate.build", "gate.test")
				passAllGates(s)
				s.Status = "SEALED"
			},
			phase:    "TERMINAL",
			nextKind: "COMPLETE",
			actual:   shadow.ActualNext{Inferable: true, Step: "terminal", Boundary: shadow.BoundaryTerminal},
			verdict:  shadow.VerdictMatch,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			runID := "shadow-fixture"
			newLegacyRun(t, root, runID, tc.mutate)
			report, err := shadow.Run(shadow.Options{Root: root, RunID: runID, OutputDir: t.TempDir()})
			if err != nil {
				t.Fatalf("shadow run: %v", err)
			}
			if report.ProjectedPhase != tc.phase {
				t.Fatalf("projected phase = %q, want %q", report.ProjectedPhase, tc.phase)
			}
			if !reflect.DeepEqual(report.Prediction.Frontier, initialFrontier) {
				t.Fatalf("predicted frontier = %v, want %v", report.Prediction.Frontier, initialFrontier)
			}
			if len(report.ProjectedCompleted) != 0 {
				t.Fatalf("projected completed steps must be empty, got %v", report.ProjectedCompleted)
			}
			if report.Prediction.NextKind != tc.nextKind {
				t.Fatalf("next kind = %q, want %q", report.Prediction.NextKind, tc.nextKind)
			}
			if report.Prediction.NextReason != tc.nextReason {
				t.Fatalf("next reason = %q, want %q", report.Prediction.NextReason, tc.nextReason)
			}
			if !reflect.DeepEqual(report.Actual, tc.actual) {
				t.Fatalf("actual next = %+v, want %+v", report.Actual, tc.actual)
			}
			if report.Verdict != tc.verdict {
				t.Fatalf("verdict = %q, want %q", report.Verdict, tc.verdict)
			}
		})
	}
}

// TestShadowFactMapping 钉死 legacy 字段到封闭事实来源的映射：VCS 三条
// （vcs/baseSnapshot/currentSnapshot）与 FILE 四条（需求源/需求版本/prompt
// 版本/catalog 版本）按 (source, key) 规范排序；HOST/LIFECYCLE/RECEIPT/
// CAPACITY 标注不可用；字段为空时不产生事实（不猜测值）。
func TestShadowFactMapping(t *testing.T) {
	root := t.TempDir()
	runID := "shadow-facts"
	newLegacyRun(t, root, runID, nil)
	report, err := shadow.Run(shadow.Options{Root: root, RunID: runID, OutputDir: t.TempDir()})
	if err != nil {
		t.Fatalf("shadow run: %v", err)
	}
	wantFacts := []shadow.ReportFact{
		{Source: "FILE", Key: "basePromptRevision", Value: "bp-rev"},
		{Source: "FILE", Key: "catalogRevision", Value: "cat-rev"},
		{Source: "FILE", Key: "requirementRevision", Value: "sha256:req"},
		{Source: "FILE", Key: "requirementSource", Value: "req/source.md"},
		{Source: "VCS", Key: "baseSnapshot", Value: "base-snap"},
		{Source: "VCS", Key: "currentSnapshot", Value: "curr-snap"},
		{Source: "VCS", Key: "vcs", Value: "git"},
	}
	if !reflect.DeepEqual(report.Facts, wantFacts) {
		t.Fatalf("mapped facts = %+v, want %+v", report.Facts, wantFacts)
	}
	wantUnavailable := map[string]bool{"HOST": false, "LIFECYCLE": false, "RECEIPT": false, "CAPACITY": false}
	for _, u := range report.UnavailableSources {
		if _, ok := wantUnavailable[u.Source]; !ok {
			t.Fatalf("unexpected unavailable source %q", u.Source)
		}
		wantUnavailable[u.Source] = true
		if u.Reason == "" {
			t.Fatalf("unavailable source %q must carry a reason", u.Source)
		}
	}
	for source, seen := range wantUnavailable {
		if !seen {
			t.Fatalf("source %q missing from unavailable list", source)
		}
	}

	// 空字段不产生事实：清空 VCS 字段后 vcs 事实消失，其余不变。
	emptyRoot := t.TempDir()
	newLegacyRun(t, emptyRoot, runID, func(s *validate.RunState) { s.VCS = "" })
	emptyReport, err := shadow.Run(shadow.Options{Root: emptyRoot, RunID: runID, OutputDir: t.TempDir()})
	if err != nil {
		t.Fatalf("shadow run (empty vcs): %v", err)
	}
	if len(emptyReport.Facts) != len(wantFacts)-1 {
		t.Fatalf("empty vcs field must drop exactly one fact, got %+v", emptyReport.Facts)
	}
	for _, f := range emptyReport.Facts {
		if f.Key == "vcs" {
			t.Fatalf("empty legacy field must not produce a guessed fact")
		}
	}
}
