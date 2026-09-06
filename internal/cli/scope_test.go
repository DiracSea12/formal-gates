package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/validate"
)

// TestQAScopeValueParsing verifies the inline authorize-repair --qa-scope /
// --qa-cases / --qa-reason <mode>=<value> parsing (产品审 P2): an empty
// <mode> (a leading '=') selects the merged / single-dispatch QA set, values group
// per mode, and malformed input is rejected.
func TestQAScopeValueParsing(t *testing.T) {
	byMode := map[string]*validate.QAScopeInput{}
	scope := &qaScopeValue{byMode: byMode, field: "scope"}
	if err := scope.Set("blackbox=FULL"); err != nil {
		t.Fatalf("scope.Set(blackbox=FULL): %v", err)
	}
	// 合并空 mode：--qa-scope =AFFECTED。
	if err := scope.Set("=AFFECTED"); err != nil {
		t.Fatalf("scope.Set(=AFFECTED): %v", err)
	}
	cases := &qaScopeValue{byMode: byMode, field: "cases"}
	if err := cases.Set("=CASE-002,CASE-003"); err != nil {
		t.Fatalf("cases.Set(=CASE-002,CASE-003): %v", err)
	}
	reason := &qaScopeValue{byMode: byMode, field: "reason"}
	if err := reason.Set("=rerun the merged set"); err != nil {
		t.Fatalf("reason.Set(=...): %v", err)
	}
	if item := byMode["blackbox"]; item == nil || item.Decision != "FULL" || item.Mode != "blackbox" {
		t.Fatalf("blackbox item=%#v", byMode["blackbox"])
	}
	merged := byMode[""]
	if merged == nil || merged.Decision != "AFFECTED" || len(merged.CaseIDs) != 2 || merged.CaseIDs[0] != "CASE-002" || merged.Reason != "rerun the merged set" {
		t.Fatalf("merged item=%#v", merged)
	}
	// 缺少 '=' 或 scope 空 decision → 拒绝。
	if err := scope.Set("blackbox"); err == nil {
		t.Fatalf("missing '=' was accepted")
	}
	if err := scope.Set("="); err == nil {
		t.Fatalf("empty scope decision was accepted")
	}
	if err := scope.Set("blackbox="); err == nil {
		t.Fatalf("empty scope decision value was accepted")
	}
}

// cliRepairDispatch prepares, claims, and snapshots a repair development round.
func cliRepairDispatch(t *testing.T, root, pkg, runID, suffix string) {
	t.Helper()
	prompt := runCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", runID, "--action", "development-worker")
	state, _ := validate.LoadRunState(root, runID)
	repairDispatch := cliOpenDispatch(state, "action", "development-worker")
	if repairDispatch == "" || !strings.Contains(prompt, repairDispatch) {
		t.Fatalf("repair dispatch not prepared: %s", prompt)
	}
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", runID, "--dispatch", repairDispatch, "--reviewer", "cli-repair-"+suffix)
	mustWriteCLI(t, filepath.Join(root, "repair-"+suffix+".txt"), "repair\n")
	cliGit(t, root, "add", "--all")
	cliGit(t, root, "commit", "-m", "repair "+suffix)
	runCLI(t, "workflow", "snapshot", "--root", root, "--package-root", pkg, "--run-id", runID, "--dispatch", repairDispatch)
}

// TestCLIAuthorizeRepairInlineQAScope verifies the end-to-end CLI wiring of the
// inline authorize-repair QA scope flags (产品审 P2): after the shared wave
// limit is exhausted, a bound preview without an affected subset is rejected.
// Inline scope for the selected blackbox mode records Source AUTHORIZE_REPAIR
// and grants exactly one extra wave. Empty-mode flag parsing is covered above.
func TestCLIAuthorizeRepairInlineQAScope(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, "cli-auth-scope")
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--confirmed")
	state = cliRecordAction(t, root, pkg, state, "product-review", "PASS")
	state = cliRecordAction(t, root, pkg, state, "start-readiness", "PASS")
	runCLI(t, "workflow", "slicing", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--decision", "no-split", "--note", "single coherent bounded unit")
	runCLI(t, "workflow", "route", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--mode", "custom", "--gate", "quality", "--gate", "blackbox")
	state, _ = validate.LoadRunState(root, state.RunID)
	qaTree := filepath.Join(t.TempDir(), "qa")
	cliGit(t, root, "worktree", "add", "--detach", qaTree, state.BaseSnapshot)
	runCLI(t, "workflow", "qa-worktree", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--worktree", qaTree)

	designDispatch := cliPrepareAction(t, root, pkg, state.RunID, "qa-design", "blackbox")
	runCLI(t, "workflow", "qa-design", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", designDispatch,
		"--case", "first public workflow", "--mode", "blackbox", "--procedure", "run the first documented CLI", "--oracle", "observable success",
		"--case", "public workflow", "--mode", "blackbox", "--procedure", "run documented CLI", "--oracle", "observable success")
	reviewDispatch := cliPrepareAction(t, root, pkg, state.RunID, "qa-review", "blackbox")
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", reviewDispatch, "--reviewer", "qa-session")
	runCLI(t, "workflow", "qa-review", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", reviewDispatch,
		"--case", "CASE-001", "--outcome", "PASS", "--case", "CASE-002", "--outcome", "PASS")
	developmentDispatch := cliPrepareAction(t, root, pkg, state.RunID, "development-worker")
	mustWriteCLI(t, filepath.Join(root, "delivery.txt"), "delivery\n")
	cliGit(t, root, "add", "--all")
	cliGit(t, root, "commit", "-m", "delivery")
	runCLI(t, "workflow", "snapshot", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", developmentDispatch)

	// 每波：黑盒 QA 执行 FAIL（CASE-002）+ 质量门 FAIL → 裁决后进入修复。QA 执行派发逐波
	// 用独立审查身份认领（身份一次性，跨波不得复用）。
	recordFailingWave := func(suffix string) {
		runCLI(t, "workflow", "prepare-review-wave", "--root", root, "--package-root", pkg, "--run-id", state.RunID)
		runCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "qa-execution", "--mode", "blackbox")
		state, _ = validate.LoadRunState(root, state.RunID)
		executionDispatch := cliOpenDispatch(state, "action", "qa-execution")
		runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", executionDispatch, "--reviewer", "cli-auth-exec-"+suffix)
		runCLI(t, "workflow", "qa-execution", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", executionDispatch,
			"--case-result", "CASE-001", "--outcome", "PASS", "--procedure", "ran the first documented CLI on the current candidate", "--observation", "the command completed with exit status 0", "--oracle-result", "the output matched the approved oracle",
			"--case-result", "CASE-002", "--outcome", "FAIL", "--procedure", "ran CLI", "--observation", "output mismatched", "--oracle-result", "expected success")
		state, _ = validate.LoadRunState(root, state.RunID)
		gateDispatch := cliPrepareGate(t, root, pkg, state.RunID, "quality")
		runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", gateDispatch, "--reviewer", "cli-auth-quality-"+suffix)
		gateArgs := []string{"workflow", "record-gate", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--gate", "quality", "--dispatch", gateDispatch, "--status", "FAIL", "--compared", state.BaseSnapshot + ".." + state.CurrentSnapshot, "--finding", "still blocked", "--severity", "P1"}
		gateArgs = append(gateArgs, cliP1FindingEvidence("delivery.txt:1", true)...)
		state = decodeSemanticState(t, runCLI(t, gateArgs...))
		state = repairCLIAdjudicate(t, root, pkg, state, "NEW", "")
	}
	// 波 1：首次执行不问 scope。
	recordFailingWave("1")
	// 波 2..3：每波普通修复后记录固定 AFFECTED scope 再重跑。
	for wave := 2; wave <= 3; wave++ {
		cliRepairDispatch(t, root, pkg, state.RunID, fmt.Sprintf("%d", wave))
		runCLI(t, "workflow", "qa-execution-scope", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--mode", "blackbox", "--decision", "AFFECTED", "--cases", "CASE-001,CASE-002", "--reason", "both paths are affected by this repair")
		recordFailingWave(fmt.Sprintf("%d", wave))
	}
	// 上限用尽：先绑定只读预览；scope 与预览都未给受影响子集时仍然拒绝。
	previewArgs := []string{"workflow", "repair-preview", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--build-target", "go build ./...", "--test-target", "go test ./..."}
	var preview validate.RepairAuthorizationPreview
	if err := json.Unmarshal([]byte(runCLI(t, previewArgs...)), &preview); err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "authorize-repair", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--cycles", "1", "--preview-digest", preview.Digest, "--state-revision", preview.StateRevision, "--build-target", "go build ./...", "--test-target", "go test ./..."}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "requires a non-empty case subset") {
		t.Fatalf("authorize-repair without a rerun scope was accepted: code=%d err=%s", code, stderr.String())
	}
	state, _ = validate.LoadRunState(root, state.RunID)
	if state.ExtraReviewWaves != 0 || state.StateIntegrity != preview.StateRevision {
		t.Fatal("rejected scope changed state or granted an extra wave")
	}
	previewArgs = append(previewArgs, "--qa-case", "blackbox=CASE-001,CASE-002")
	if err := json.Unmarshal([]byte(runCLI(t, previewArgs...)), &preview); err != nil {
		t.Fatal(err)
	}
	// 内联已选 mode 的 AFFECTED scope：记录 Source=AUTHORIZE_REPAIR、授权一轮。
	runCLI(t, "workflow", "authorize-repair", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--cycles", "1", "--qa-scope", "blackbox=AFFECTED", "--qa-cases", "blackbox=CASE-001,CASE-002", "--qa-reason", "blackbox=both paths remain affected", "--preview-digest", preview.Digest, "--state-revision", preview.StateRevision, "--build-target", "go build ./...", "--test-target", "go test ./...", "--qa-case", "blackbox=CASE-001,CASE-002")
	state, _ = validate.LoadRunState(root, state.RunID)
	if state.ExtraReviewWaves != 1 {
		t.Fatalf("extra waves=%d want=1", state.ExtraReviewWaves)
	}
	sc := state.ExecutionScopes["blackbox"]
	if sc.Decision != "AFFECTED" || sc.Source != "AUTHORIZE_REPAIR" || sc.Origin != "MAIN_AGENT" || sc.BaseSnapshot != state.CurrentSnapshot {
		t.Fatalf("blackbox inline scope=%#v", sc)
	}
}
