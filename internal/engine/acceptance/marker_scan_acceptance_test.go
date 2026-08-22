// marker_scan_acceptance_test.go 覆盖批次 3 验收的 marker 扫描：
//
//   - executable 路径：checked-in 定义的编译产物经源级全步骤扫描
//     （CompileDiagnostic + 完整 registry → 零诊断）与
//     acceptance.ScanNoMissingEngineAdapter 双重确认无 MISSING_ENGINE_ADAPTER；
//   - diagnostic fixture：registry 缺一个注册时 CompileDiagnostic 产出的
//     带 marker 定义只在本测试内持有，扫描拒绝它，encoder 与 Decide 也拒绝
//     它——fixture 在结构上进不了 executable 路径。
//
// 收口段调用方式见 scan.go 的函数文档：测试内直接调用
// acceptance.ScanNoMissingEngineAdapter(cd)，或命令行
//
//	go test ./internal/engine/acceptance -run TestAcceptanceMarkerScan -v
package acceptance_test

import (
	"strings"
	"testing"

	"formal-gates/internal/engine/acceptance"
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/runtime"
)

func TestAcceptanceMarkerScan(t *testing.T) {
	// executable 路径：完整 registry 编译，源级全步骤断言零诊断、无 marker。
	t.Run("executable definition passes scan", func(t *testing.T) {
		cd, _, _ := compileWorkflow(t, definition.Workflow(), workflowRegistry(t))
		if err := acceptance.ScanNoMissingEngineAdapter(cd); err != nil {
			t.Fatalf("scan rejected a fully registered definition: %v", err)
		}
		dr, err := compiler.CompileDiagnostic(definition.Workflow(), workflowRegistry(t))
		if err != nil {
			t.Fatalf("diagnostic compile: %v", err)
		}
		if len(dr.Diagnostics) != 0 || dr.Definition.MissingEngineAdapter {
			t.Fatalf("complete registry must yield zero diagnostics, got %+v", dr.Diagnostics)
		}
	})

	// diagnostic fixture：只在本测试持有（registry 缺 op.fan.transport 注册），
	// 正常 compile 以 BLOCKED_BUG 拒绝，diagnostic compile 产出带 marker 定义。
	t.Run("diagnostic fixture rejected by scan and executable path", func(t *testing.T) {
		reg := workflowRegistry(t, "op.fan.transport")

		if _, err := compiler.Compile(definition.Workflow(), reg); err == nil {
			t.Fatal("normal compile must reject the incomplete definition")
		}

		dr, err := compiler.CompileDiagnostic(definition.Workflow(), reg)
		if err != nil {
			t.Fatalf("diagnostic compile must load incomplete definitions: %v", err)
		}
		if !dr.Definition.MissingEngineAdapter || len(dr.Diagnostics) != 1 {
			t.Fatalf("fixture must carry the marker and one diagnostic, got %+v", dr.Diagnostics)
		}
		want := compiler.Diagnostic{Step: "fan.transport", Ref: "op.fan.transport", Want: compiler.KindOperation}
		if dr.Diagnostics[0] != want {
			t.Fatalf("diagnostic = %+v, want %+v", dr.Diagnostics[0], want)
		}

		// 扫描入口拒绝 fixture（收口段对候选调用的同一判定）。
		err = acceptance.ScanNoMissingEngineAdapter(dr.Definition)
		wantErr(t, err, "MISSING_ENGINE_ADAPTER")

		// fixture 进不了 executable 路径：canonical encoder 与决策核心都硬拒绝。
		_, err = encoder.Encode(dr.Definition)
		wantErr(t, err, "MISSING_ENGINE_ADAPTER marker")
		s, err := decision.NewState(dr.Definition.Version, runtime.PhaseDevelopmentParallel)
		if err != nil {
			t.Fatalf("new state: %v", err)
		}
		if _, err = decision.Decide(s, decision.Observation{}, dr.Definition); err == nil ||
			!strings.Contains(err.Error(), "MISSING_ENGINE_ADAPTER") {
			t.Fatalf("decide on marker definition err = %v, want MISSING_ENGINE_ADAPTER rejection", err)
		}
	})

	// 扫描入口对空输入的机械拒绝（收口段误传 nil 时必须报错而非放行）。
	t.Run("nil definition rejected", func(t *testing.T) {
		wantErr(t, acceptance.ScanNoMissingEngineAdapter(nil), "nil definition")
	})
}

// TestAcceptanceWorkflowStepKindsFixed 是扫描的补充钉定：checked-in 定义九步
// 的 kind/authority/runner 物化值固定（marker 扫描所依据的 compiled IR 形态
// 不随批漂移）。
func TestAcceptanceWorkflowStepKindsFixed(t *testing.T) {
	cd, _, _ := compileWorkflow(t, definition.Workflow(), workflowRegistry(t))
	want := map[string]struct {
		kind compiler.StepKind
		auth authoring.DecisionAuthority
		run  authoring.RunnerKind
	}{
		"entry.parse":   {compiler.KindLocal, authoring.AuthorityEngine, authoring.RunnerEngineLocal},
		"entry.persist": {compiler.KindDurable, authoring.AuthorityEngine, authoring.RunnerDurableActivity},
		"review.worker": {compiler.KindAgent, authoring.AuthorityAgent, authoring.RunnerAgentWorker},
		"ask.decide":    {compiler.KindHumanAsk, authoring.AuthorityHuman, authoring.RunnerHostAdapter},
		"fan.split":     {compiler.KindParallel, authoring.AuthorityEngine, authoring.RunnerEngineLocal},
		"fan.slice":     {compiler.KindLocal, authoring.AuthorityEngine, authoring.RunnerEngineLocal},
		"fan.transport": {compiler.KindHostAction, authoring.AuthorityEngine, authoring.RunnerHostAdapter},
		"fan.join":      {compiler.KindLocal, authoring.AuthorityEngine, authoring.RunnerEngineLocal},
		"report.cost":   {compiler.KindLocal, authoring.AuthorityEngine, authoring.RunnerEngineLocal},
	}
	if len(cd.Steps) != len(want) {
		t.Fatalf("step count = %d, want %d", len(cd.Steps), len(want))
	}
	for _, cs := range cd.Steps {
		w, ok := want[string(cs.Header.ID)]
		if !ok {
			t.Fatalf("unexpected step %q", cs.Header.ID)
		}
		if cs.Header.Kind != w.kind || cs.Header.Authority != w.auth || cs.Header.Runner != w.run {
			t.Errorf("step %q kind/auth/runner = %s/%s/%s, want %s/%s/%s",
				cs.Header.ID, cs.Header.Kind, cs.Header.Authority, cs.Header.Runner, w.kind, w.auth, w.run)
		}
	}
}
