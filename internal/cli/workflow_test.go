package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/lifecycle"
	"formal-gates/internal/validate"
)

func TestCLIWorkflowUsesDispatchesKindsAndNativeSnapshots(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, "cli-run")
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--confirmed")
	state = cliRecordAction(t, root, pkg, state, "product-review", "PASS")
	state = cliRecordAction(t, root, pkg, state, "start-readiness", "PASS")
	runCLI(t, "workflow", "slicing", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--decision", "no-split", "--note", "single coherent bounded unit")
	runCLI(t, "workflow", "route", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--mode", "full")
	state, _ = validate.LoadRunState(root, state.RunID)

	designDispatch := cliPrepareAction(t, root, pkg, state.RunID, "qa-design")
	runCLI(t, "workflow", "qa-design", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", designDispatch,
		"--case", "direct rules", "--mode", "whitebox", "--procedure", "go test ./...", "--oracle", "tests pass", "--test", "whitebox_delivered_test.go::TestWhiteboxDirectRules",
		"--case", "public workflow", "--mode", "blackbox", "--procedure", "run documented CLI", "--oracle", "observable success")
	reviewDispatch := cliPrepareAction(t, root, pkg, state.RunID, "qa-review")
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", reviewDispatch, "--reviewer", "qa-session")
	runCLI(t, "workflow", "qa-review", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", reviewDispatch,
		"--case", "CASE-001", "--outcome", "PASS", "--case", "CASE-002", "--outcome", "PASS")

	developmentDispatch := cliPrepareAction(t, root, pkg, state.RunID, "development-worker")
	mustWriteCLI(t, filepath.Join(root, "delivery.txt"), "delivery\n")
	cliGit(t, root, "add", "--all")
	cliGit(t, root, "commit", "-m", "delivery")
	runCLI(t, "workflow", "snapshot", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", developmentDispatch)

	executionDispatch := cliPrepareAction(t, root, pkg, state.RunID, "qa-execution")
	runCLI(t, "workflow", "qa-execution", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", executionDispatch,
		"--case-result", "CASE-001", "--outcome", "PASS", "--procedure", "ran tests", "--observation", "passed", "--oracle-result", "matched",
		"--case-result", "CASE-002", "--outcome", "PASS", "--procedure", "ran CLI", "--observation", "succeeded", "--oracle-result", "matched")

	gateDispatch := cliPrepareGate(t, root, pkg, state.RunID, "quality")
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", gateDispatch, "--reviewer", "gate-session")
	state, _ = validate.LoadRunState(root, state.RunID)
	runCLI(t, "workflow", "record-gate", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--gate", "quality", "--dispatch", gateDispatch, "--status", "PASS", "--compared", state.BaseSnapshot+".."+state.CurrentSnapshot)

	out := runCLI(t, "workflow", "show", "--root", root, "--run-id", state.RunID)
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		t.Fatal(err)
	}
	if state.CurrentSnapshot != cliGit(t, root, "rev-parse", "HEAD") || state.QAExecutionByMode[""].Cases[0].Mode != "whitebox" || state.Gates["quality"].DispatchID != gateDispatch {
		t.Fatalf("CLI state lost native bindings: %s", out)
	}
	summary := runCLI(t, "workflow", "seal", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--squash-message", "squashed delivery")
	if !strings.Contains(summary, `"status": "SEALED"`) {
		t.Fatalf("seal output=%s", summary)
	}
}

// TestCLIQARerunScopeDecision verifies through the CLI: a QA execution
// rerun is refused at prepare-action until a scope decision is recorded with
// workflow qa-execution-scope, and invalid decisions are rejected.
func TestCLIQARerunScopeDecision(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, "cli-rerun-scope")
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--confirmed")
	state = cliRecordAction(t, root, pkg, state, "product-review", "PASS")
	state = cliRecordAction(t, root, pkg, state, "start-readiness", "PASS")
	runCLI(t, "workflow", "slicing", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--decision", "no-split", "--note", "single coherent bounded unit")
	runCLI(t, "workflow", "route", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--mode", "full")
	state, _ = validate.LoadRunState(root, state.RunID)

	designDispatch := cliPrepareAction(t, root, pkg, state.RunID, "qa-design")
	runCLI(t, "workflow", "qa-design", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", designDispatch,
		"--case", "direct rules", "--mode", "whitebox", "--procedure", "go test ./...", "--oracle", "tests pass", "--test", "whitebox_delivered_test.go::TestWhiteboxDirectRules",
		"--case", "public workflow", "--mode", "blackbox", "--procedure", "run documented CLI", "--oracle", "observable success")
	reviewDispatch := cliPrepareAction(t, root, pkg, state.RunID, "qa-review")
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", reviewDispatch, "--reviewer", "qa-session")
	runCLI(t, "workflow", "qa-review", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", reviewDispatch,
		"--case", "CASE-001", "--outcome", "PASS", "--case", "CASE-002", "--outcome", "PASS")

	developmentDispatch := cliPrepareAction(t, root, pkg, state.RunID, "development-worker")
	mustWriteCLI(t, filepath.Join(root, "delivery.txt"), "delivery\n")
	cliGit(t, root, "add", "--all")
	cliGit(t, root, "commit", "-m", "delivery")
	runCLI(t, "workflow", "snapshot", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", developmentDispatch)

	// 首轮：黑盒用例 FAIL → 进入修复。
	executionDispatch := cliPrepareAction(t, root, pkg, state.RunID, "qa-execution")
	runCLI(t, "workflow", "qa-execution", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", executionDispatch,
		"--case-result", "CASE-001", "--outcome", "PASS", "--procedure", "ran tests", "--observation", "passed", "--oracle-result", "matched",
		"--case-result", "CASE-002", "--outcome", "FAIL", "--procedure", "ran CLI", "--observation", "output mismatched", "--oracle-result", "expected success")
	state, _ = validate.LoadRunState(root, state.RunID)
	gateDispatch := cliPrepareGate(t, root, pkg, state.RunID, "quality")
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", gateDispatch, "--reviewer", "gate-quality")
	runCLI(t, "workflow", "record-gate", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--gate", "quality", "--dispatch", gateDispatch, "--status", "FAIL", "--compared", state.BaseSnapshot+".."+state.CurrentSnapshot, "--finding", "normal workflow fails", "--severity", "P1")

	// 修复快照（无先通过的已选门，无需 Carry 处置）。开发/修复派发的认领身份一次性，
	// 修复派发用独立身份手动准备并认领。
	prompt := runCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "development-worker")
	state, _ = validate.LoadRunState(root, state.RunID)
	repairDispatch := cliOpenDispatch(state, "action", "development-worker")
	if repairDispatch == "" || !strings.Contains(prompt, repairDispatch) {
		t.Fatalf("repair dispatch not prepared: %s", prompt)
	}
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", repairDispatch, "--reviewer", "cli-rerun-scope-repair")
	mustWriteCLI(t, filepath.Join(root, "repair.txt"), "repair\n")
	cliGit(t, root, "add", "--all")
	cliGit(t, root, "commit", "-m", "repair")
	runCLI(t, "workflow", "snapshot", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", repairDispatch)

	// 重跑未记录 scope → prepare-action 拒绝。
	var stderr bytes.Buffer
	code := Run("formal-gates", []string{"workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "qa-execution"}, IO{Stderr: &stderr})
	if code == 0 || !strings.Contains(stderr.String(), "requires a scope decision") {
		t.Fatalf("rerun without a scope decision was accepted: code=%d err=%s", code, stderr.String())
	}
	// 非法的 scope 决策被拒。
	stderr.Reset()
	code = Run("formal-gates", []string{"workflow", "qa-execution-scope", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--decision", "BOGUS"}, IO{Stderr: &stderr})
	if code == 0 || !strings.Contains(stderr.String(), "must be FULL or AFFECTED") {
		t.Fatalf("invalid scope decision was accepted: code=%d err=%s", code, stderr.String())
	}
	// 记录 FULL scope 后重跑放行。
	runCLI(t, "workflow", "qa-execution-scope", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--decision", "FULL", "--reason", "rerun the complete set")
	runCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "qa-execution")
	state, _ = validate.LoadRunState(root, state.RunID)
	if sc := state.ExecutionScopes[""]; sc.Decision != "FULL" || sc.Origin != "USER" || sc.Source != "PREPARE" {
		t.Fatalf("recorded scope=%#v", sc)
	}
}

func TestCLIResumeAdoptsExternalChange(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, "cli-adopt")
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--confirmed")
	state = cliRecordAction(t, root, pkg, state, "product-review", "PASS")
	state = cliRecordAction(t, root, pkg, state, "start-readiness", "PASS")
	runCLI(t, "workflow", "slicing", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--decision", "no-split", "--note", "single coherent bounded unit")
	runCLI(t, "workflow", "route", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--mode", "custom", "--gate", "quality")
	developmentDispatch := cliPrepareAction(t, root, pkg, state.RunID, "development-worker")
	mustWriteCLI(t, filepath.Join(root, "delivery.txt"), "delivery\n")
	cliGit(t, root, "add", "--all")
	cliGit(t, root, "commit", "-m", "delivery")
	runCLI(t, "workflow", "snapshot", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", developmentDispatch)

	mustWriteCLI(t, filepath.Join(root, "external.txt"), "external\n")
	cliGit(t, root, "add", "--all")
	cliGit(t, root, "commit", "-m", "external work")
	out := runCLI(t, "workflow", "resume", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--adopt-external", "--reason", "adopt unrelated external work")
	var adopted validate.RunState
	if err := json.Unmarshal([]byte(out), &adopted); err != nil {
		t.Fatal(err)
	}
	adoptedADOPT := false
	for _, record := range adopted.Carry {
		if record.Origin == "ADOPT" {
			adoptedADOPT = true
			break
		}
	}
	if adopted.CurrentSnapshot != cliGit(t, root, "rev-parse", "HEAD") || !adoptedADOPT {
		t.Fatalf("CLI adoption did not rebind with provenance: %s", out)
	}
}

func TestCLIRejectsRemovedSourceAndLiveSnapshotFlags(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, "removed-bindings")
	dispatchID := cliPrepareAction(t, root, pkg, state.RunID, "requirements-clarification")
	before := cliStateBytes(t, root, state.RunID)
	for _, args := range [][]string{
		{"workflow", "record-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "requirements-clarification", "--dispatch", dispatchID, "--status", "PASS", "--source-revision", state.RequirementRevision},
		{"workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "requirements-clarification", "--live-snapshot", state.CurrentSnapshot},
	} {
		var stderr bytes.Buffer
		if code := Run("formal-gates", args, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "flag provided but not defined") {
			t.Fatalf("removed flag was accepted: code=%d err=%s", code, stderr.String())
		}
	}
	if cliStateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected removed binding flag changed state")
	}
}

func TestCLIRequirementArtifactsAreRegisteredAndSorted(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	mustWriteCLI(t, filepath.Join(root, "z-solution.md"), "solution\n")
	cliGit(t, root, "add", "--all")
	cliGit(t, root, "commit", "-m", "solution")
	var stdout, stderr bytes.Buffer
	code := Run("formal-gates", []string{"workflow", "start", "--root", root, "--package-root", pkg, "--run-id", "artifact-cli", "--requirement", "requirements.md", "--requirement-artifact", "z-solution.md", "--vcs", "git", "--split", "no"}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("start failed: %s", stderr.String())
	}
	var state validate.RunState
	if err := json.Unmarshal(stdout.Bytes(), &state); err != nil {
		t.Fatal(err)
	}
	if len(state.RequirementArtifacts) != 2 || state.RequirementArtifacts[0].Path != "requirements.md" || state.RequirementArtifacts[1].Path != "z-solution.md" {
		t.Fatalf("artifacts=%#v", state.RequirementArtifacts)
	}
}

func TestCLIGroupedQAReviewFieldsRequireCurrentCase(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "qa-review", "--outcome", "PASS"}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "must follow --case") {
		t.Fatalf("misordered QA Review field accepted: code=%d err=%s", code, stderr.String())
	}
}

func TestCLISlicingDecisionGatesDevelopment(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, "cli-slicing")
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--confirmed")

	// product-review 未 PASS 时不可记录拆分决定；没有拆分决定时路线被拒。
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "slicing", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--decision", "no-split", "--note", "reason"}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "Product Review must pass before the slicing decision") {
		t.Fatalf("slicing recorded before product-review: code=%d err=%s", code, stderr.String())
	}
	stderr.Reset()
	if code := Run("formal-gates", []string{"workflow", "route", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--mode", "full"}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "slicing decision must be recorded before the route") {
		t.Fatalf("route accepted before slicing decision: code=%d err=%s", code, stderr.String())
	}

	state = cliRecordAction(t, root, pkg, state, "product-review", "PASS")
	// start-readiness 未 PASS 时不可记录拆分决定。
	stderr.Reset()
	if code := Run("formal-gates", []string{"workflow", "slicing", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--decision", "no-split", "--note", "reason"}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "Start Readiness must pass") {
		t.Fatalf("slicing recorded before start-readiness: code=%d err=%s", code, stderr.String())
	}
	state = cliRecordAction(t, root, pkg, state, "start-readiness", "PASS")
	runCLI(t, "workflow", "slicing", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--decision", "no-split", "--note", "single coherent bounded unit")
	runCLI(t, "workflow", "route", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--mode", "custom", "--gate", "quality")
	out := runCLI(t, "workflow", "show", "--root", root, "--run-id", state.RunID)
	var loaded validate.RunState
	if err := json.Unmarshal([]byte(out), &loaded); err != nil {
		t.Fatal(err)
	}
	if loaded.Slicing == nil || loaded.Slicing.Decision != "no-split" || loaded.Slicing.Note == "" {
		t.Fatalf("CLI slicing decision not recorded: %#v", loaded.Slicing)
	}
	runCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "development-worker")
}

// TestCLIRouteCandidatesReportsConfirmedRunCandidates verifies the workflow
// route-candidates CLI command: it is refused before requirement confirmation
// and prints the live catalog's normal route candidates after confirmation.
func TestCLIRouteCandidatesReportsConfirmedRunCandidates(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, "cli-route-candidates")

	// 需求未确认：route-candidates 拒绝。
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "route-candidates", "--root", root, "--package-root", pkg, "--run-id", state.RunID}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "requirement is not confirmed") {
		t.Fatalf("route-candidates before requirement confirmation: code=%d err=%s", code, stderr.String())
	}

	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--confirmed")

	out := runCLI(t, "workflow", "route-candidates", "--root", root, "--package-root", pkg, "--run-id", state.RunID)
	var got []string
	if err := json.Unmarshal([]byte(out), &got); err != nil {
		t.Fatalf("route-candidates output=%s err=%v", out, err)
	}
	want, err := validate.PackageRouteCandidates(pkg)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("route-candidates=%v want %v", got, want)
	}
}

func TestCLIHelpListsDispatchAndQAReviewCommands(t *testing.T) {
	var stdout bytes.Buffer
	if code := Run("formal-gates", []string{"--help"}, IO{Stdout: &stdout}); code != 0 {
		t.Fatalf("help code=%d", code)
	}
	for _, want := range []string{"claim-dispatch", "qa-review", "lifecycle capture|verify"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("help omitted %s: %s", want, stdout.String())
		}
	}
}

func TestCLISubcommandHelpPrintsUsage(t *testing.T) {
	// 二层 --help 粒度统一：每个组的 --help 打印该组自己的 usage，而非顶层清单。
	for _, group := range []string{"workflow", "lifecycle", "canary", "hook"} {
		for _, flag := range []string{"-h", "--help", "help"} {
			var stdout, stderr bytes.Buffer
			code := Run("formal-gates", []string{group, flag}, IO{Stdout: &stdout, Stderr: &stderr})
			if code != 0 {
				t.Fatalf("%s %s code=%d err=%s", group, flag, code, stderr.String())
			}
			want := map[string]string{
				"workflow":  "Usage: formal-gates workflow <subcommand>",
				"lifecycle": "Usage: formal-gates lifecycle <subcommand>",
				"canary":    "Usage: formal-gates canary <subcommand>",
				"hook":      "Usage: formal-gates hook decide",
			}[group]
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("%s %s omitted group usage (%s): %s", group, flag, want, stdout.String())
			}
		}
	}
}

func TestCLIInstalledHostsResolveLifecycleEventsToActiveWorkflowRoot(t *testing.T) {
	tests := []struct {
		name       string
		provider   string
		startEvent string
		stopEvent  string
		payloads   func(root, alternateRoot, identity string) (string, string, string, []string)
	}{
		{
			name:       "claude-subdirectory",
			provider:   lifecycle.ProviderClaude,
			startEvent: "SubagentStart",
			stopEvent:  "SubagentStop",
			payloads: func(root, _ string, identity string) (string, string, string, []string) {
				workdir := filepath.Join(root, "services", "worker")
				if err := os.MkdirAll(workdir, 0o700); err != nil {
					t.Fatal(err)
				}
				payload := fmt.Sprintf(`{"cwd":%q,"agent_id":%q}`, workdir, identity)
				return payload, payload, workdir, []string{"CLAUDE_PROJECT_DIR=" + root}
			},
		},
		{
			name:       "cursor-multi-root",
			provider:   lifecycle.ProviderCursor,
			startEvent: "subagentStart",
			stopEvent:  "subagentStop",
			payloads: func(root, alternateRoot, identity string) (string, string, string, []string) {
				common := fmt.Sprintf(`"conversation_id":"conversation-1","parent_conversation_id":"conversation-1","generation_id":"generation-1","subagent_type":"generalPurpose","task":%q,"workspace_roots":[%q,%q]`, "prepared formal-gates dispatch for "+identity, alternateRoot, root)
				return fmt.Sprintf(`{"subagent_id":%q,%s}`, identity, common), "{" + common + "}", root, nil
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			root, pkg := cliWorkflowFixture(t)
			alternateRoot := t.TempDir()
			binary := buildInstalledHostCLI(t, tc.provider)
			runID := strings.ReplaceAll(tc.name, "_", "-")
			runInstalledCLI(t, binary, root, nil, "", "workflow", "start", "--root", root, "--package-root", pkg, "--run-id", runID, "--requirement", "requirements.md", "--requirement-artifact", "design.md", "--vcs", "git", "--split", "no")

			requirementsDispatch := prepareInstalledAction(t, binary, root, pkg, runID, "requirements-clarification")
			runInstalledCLI(t, binary, root, nil, "", "workflow", "record-action", "--root", root, "--package-root", pkg, "--run-id", runID, "--action", "requirements-clarification", "--dispatch", requirementsDispatch, "--status", "PASS")
			runInstalledCLI(t, binary, root, nil, "", "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", runID, "--confirmed")

			productDispatch := prepareInstalledAction(t, binary, root, pkg, runID, "product-review")
			productIdentity := tc.name + "-product-review"
			productStartPayload, productStopPayload, productCaptureDir, productEnvironment := tc.payloads(root, alternateRoot, productIdentity)
			runInstalledCLI(t, binary, productCaptureDir, productEnvironment, productStartPayload, "lifecycle", "capture", "--provider", tc.provider, "--event", tc.startEvent)
			runInstalledCLI(t, binary, root, nil, "", "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", runID, "--dispatch", productDispatch, "--reviewer", productIdentity)
			runInstalledCLI(t, binary, productCaptureDir, productEnvironment, productStopPayload, "lifecycle", "capture", "--provider", tc.provider, "--event", tc.stopEvent)
			runInstalledCLI(t, binary, root, nil, "", "workflow", "record-action", "--root", root, "--package-root", pkg, "--run-id", runID, "--action", "product-review", "--dispatch", productDispatch, "--status", "PASS")

			dispatchID := prepareInstalledAction(t, binary, root, pkg, runID, "start-readiness")
			identity := tc.name + "-agent"
			startPayload, stopPayload, captureDir, environment := tc.payloads(root, alternateRoot, identity)
			runInstalledCLI(t, binary, captureDir, environment, startPayload, "lifecycle", "capture", "--provider", tc.provider, "--event", tc.startEvent)
			runInstalledCLI(t, binary, root, nil, "", "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", runID, "--dispatch", dispatchID, "--reviewer", identity)
			runInstalledCLI(t, binary, captureDir, environment, stopPayload, "lifecycle", "capture", "--provider", tc.provider, "--event", tc.stopEvent)

			verification := runInstalledCLI(t, binary, root, nil, "", "lifecycle", "verify", "--root", root, "--run-id", runID, "--dispatch", dispatchID)
			if !strings.Contains(verification, `"outcome": "VERIFIED"`) || !strings.Contains(verification, `"provider": "`+tc.provider+`"`) {
				t.Fatalf("unexpected lifecycle verification: %s", verification)
			}
			runInstalledCLI(t, binary, root, nil, "", "workflow", "record-action", "--root", root, "--package-root", pkg, "--run-id", runID, "--action", "start-readiness", "--dispatch", dispatchID, "--status", "PASS")
			runInstalledCLI(t, binary, root, nil, "", "workflow", "slicing", "--root", root, "--package-root", pkg, "--run-id", runID, "--decision", "no-split", "--note", "single coherent bounded unit")
			runInstalledCLI(t, binary, root, nil, "", "workflow", "route", "--root", root, "--package-root", pkg, "--run-id", runID, "--mode", "custom", "--gate", "quality")
			state := installedWorkflowState(t, binary, root, runID)
			if state.Actions["start-readiness"].Status != "PASS" {
				t.Fatalf("verified lifecycle did not permit public workflow recording: %#v", state.Actions["start-readiness"])
			}
		})
	}
}

func buildInstalledHostCLI(t *testing.T, provider string) string {
	t.Helper()
	relative := map[string]string{
		lifecycle.ProviderClaude: filepath.Join(".claude", "skills", "formal-gates", "bin", installTestBinaryName()),
		lifecycle.ProviderCursor: filepath.Join(".cursor", "formal-gates", "bin", installTestBinaryName()),
	}[provider]
	path := filepath.Join(t.TempDir(), relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "build", "-o", path, "./cmd/formal-gates")
	cmd.Dir = repoRoot
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build installed %s CLI: %v: %s", provider, err, output)
	}
	return path
}

func prepareInstalledAction(t *testing.T, binary, root, pkg, runID, action string) string {
	t.Helper()
	runInstalledCLI(t, binary, root, nil, "", "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", runID, "--action", action)
	state := installedWorkflowState(t, binary, root, runID)
	dispatchID := cliOpenDispatch(state, "action", action)
	if dispatchID == "" {
		t.Fatalf("prepared action %s has no open dispatch", action)
	}
	return dispatchID
}

func installedWorkflowState(t *testing.T, binary, root, runID string) validate.RunState {
	t.Helper()
	out := runInstalledCLI(t, binary, root, nil, "", "workflow", "show", "--root", root, "--run-id", runID)
	var state validate.RunState
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func runInstalledCLI(t *testing.T, binary, workdir string, environment []string, stdin string, args ...string) string {
	t.Helper()
	cmd := exec.Command(binary, args...)
	cmd.Dir = workdir
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Env = hostFilteredEnv(environment)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v: %s", binary, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func startCLIWorkflow(t *testing.T, root, pkg, id string) validate.RunState {
	t.Helper()
	out := runCLI(t, "workflow", "start", "--root", root, "--package-root", pkg, "--run-id", id, "--requirement", "requirements.md", "--requirement-artifact", "design.md", "--vcs", "git", "--split", "no")
	var state validate.RunState
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func cliRecordAction(t *testing.T, root, pkg string, state validate.RunState, action, status string) validate.RunState {
	t.Helper()
	dispatchID := cliPrepareAction(t, root, pkg, state.RunID, action)
	out := runCLI(t, "workflow", "record-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", action, "--dispatch", dispatchID, "--status", status)
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		t.Fatal(err)
	}
	return state
}

func cliPrepareAction(t *testing.T, root, pkg, runID, action string) string {
	t.Helper()
	prompt := runCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", runID, "--action", action)
	state, err := validate.LoadRunState(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	id := cliOpenDispatch(state, "action", action)
	if id == "" || !strings.Contains(prompt, id) {
		t.Fatalf("prepared prompt missing dispatch: %s", prompt)
	}
	if action != "requirements-clarification" && action != "qa-review" {
		runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", runID, "--dispatch", id, "--reviewer", runID+"-"+action)
	}
	return id
}

func cliPrepareGate(t *testing.T, root, pkg, runID, gate string) string {
	t.Helper()
	prompt := runCLI(t, "workflow", "prepare-gate", "--root", root, "--package-root", pkg, "--run-id", runID, "--gate", gate)
	state, err := validate.LoadRunState(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	id := cliOpenDispatch(state, "gate", gate)
	if id == "" || !strings.Contains(prompt, id) {
		t.Fatalf("prepared gate missing dispatch: %s", prompt)
	}
	return id
}

func cliOpenDispatch(state validate.RunState, kind, target string) string {
	// prepare 不再作废旧派发，同功能旧 CLAIMED 派发可能仍在途，且同功能可能同时
	// 存在多张 OPEN 空票；取新派发必须取 Attempt 最大的 OPEN 票（最新准备），无 OPEN 票时
	// 才回退到 CLAIMED（在途旧票）。
	bestID := ""
	bestAttempt := 0
	for id, dispatch := range state.Dispatches {
		if dispatch.TargetKind != kind || dispatch.Target != target || dispatch.Status != "OPEN" {
			continue
		}
		if dispatch.Attempt >= bestAttempt {
			bestID, bestAttempt = id, dispatch.Attempt
		}
	}
	if bestID != "" {
		return bestID
	}
	for id, dispatch := range state.Dispatches {
		if dispatch.TargetKind == kind && dispatch.Target == target && dispatch.Status == "CLAIMED" {
			return id
		}
	}
	return ""
}

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	clearHostEnv(t)
	var stdout, stderr bytes.Buffer
	if code := Run("formal-gates", args, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
		t.Fatalf("%s failed: %s", strings.Join(args, " "), stderr.String())
	}
	return stdout.String()
}

func cliWorkflowFixture(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	mustWriteCLI(t, filepath.Join(root, "requirements.md"), "requirement\n")
	mustWriteCLI(t, filepath.Join(root, "design.md"), "design\n")
	// 白盒设计者交付的结构测试代码——测试仓库带一个测试文件，作为白盒用例测试
	// 引用（<文件>::<函数>）的定位目标。与 internal/validate 的 whiteboxDeliveredTestCode
	// 同源。
	mustWriteCLI(t, filepath.Join(root, "whitebox_delivered_test.go"), "package whiteboxfixture\n\nimport \"testing\"\n\nfunc TestWhiteboxDirectRules(t *testing.T) {}\n\nfunc TestWhiteboxStructure(t *testing.T) {}\n\nfunc TestWhiteboxDirectBehavior(t *testing.T) {}\n")
	cliGit(t, root, "init")
	cliGit(t, root, "config", "user.email", "tests@example.invalid")
	cliGit(t, root, "config", "user.name", "Formal Gates CLI Tests")
	cliGit(t, root, "add", "--all")
	cliGit(t, root, "commit", "-m", "base")
	pkg := t.TempDir()
	mustWriteCLI(t, filepath.Join(pkg, "prompts", "reviewer-base.md"), "shared contract\n")
	for _, id := range []string{"requirements-clarification", "product-review", "start-readiness", "qa-design", "qa-review", "qa-execution", "carry", "development-worker"} {
		mustWriteCLI(t, filepath.Join(pkg, "prompts", "actions", id+".md"), id+" instructions\n")
	}
	mustWriteCLI(t, filepath.Join(pkg, "gates", "quality.md"), "quality checks\n")
	return root, pkg
}

func cliGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = root
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v: %s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func cliStateBytes(t *testing.T, root, runID string) string {
	t.Helper()
	data, err := os.ReadFile(validate.RunStatePath(root, runID))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// 片 1 CLI 行为修复的 CLI 层直接测试。

// Fix 1：show/abort/cleanup 接受 --package-root（参数面与其余子命令一致；cleanup
// 不再静默忽略，值传递到 validate）。
func TestCLIWorkflowSurfaceAcceptsPackageRoot(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, "pkg-surface")
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "show", "--root", root, "--package-root", pkg, "--run-id", state.RunID}, IO{Stderr: &stderr}); code != 0 {
		t.Fatalf("show rejected --package-root: code=%d err=%s", code, stderr.String())
	}
	stderr.Reset()
	if code := Run("formal-gates", []string{"workflow", "abort", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--user-confirm"}, IO{Stderr: &stderr}); code != 0 {
		t.Fatalf("abort rejected --package-root: code=%d err=%s", code, stderr.String())
	}
	stderr.Reset()
	if code := Run("formal-gates", []string{"workflow", "cleanup", "--root", root, "--package-root", pkg}, IO{Stderr: &stderr}); code != 0 {
		t.Fatalf("cleanup rejected --package-root: code=%d err=%s", code, stderr.String())
	}
}

// Fix 4：show 对已终止/不存在的 run 给友好提示（而非裸 open state.json）。
func TestCLIShowMissingRunFriendly(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "show", "--root", t.TempDir(), "--run-id", "never-existed"}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "was not found or is already terminated") {
		t.Fatalf("show missing run did not hint: code=%d err=%s", code, stderr.String())
	}
}

// Fix 5：workflow 无子命令紧跟 flags 时提示子命令必填。
func TestCLIWorkflowFlagsWithoutSubcommandHintsRequired(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "--root", "."}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "workflow subcommand is required") {
		t.Fatalf("workflow --root accepted without a subcommand: code=%d err=%s", code, stderr.String())
	}
}

// Fix 7a：authorize-repair --qa-scope blackbox=（空决策）给明确错误。
func TestCLIAuthorizeRepairRejectsEmptyScopeDecision(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "authorize-repair", "--qa-scope", "blackbox="}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "requires a decision value") {
		t.Fatalf("empty qa-scope decision was not rejected clearly: code=%d err=%s", code, stderr.String())
	}
}

// Fix 8：缺 run-id 时友好提示（不裸报 state.json.lock）。
func TestCLIShowMissingRunIDFriendly(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "show", "--root", t.TempDir()}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "run id is required") {
		t.Fatalf("show without run-id did not hint: code=%d err=%s", code, stderr.String())
	}
}

// Fix 10：gate run 位置参数在 flags 前时提示 flag 前置。
func TestCLIGateRunFlagsAfterIDsHintsFlagFirst(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"gate", "run", "quality", "--scope", "s"}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "flags must precede") {
		t.Fatalf("gate run flag-after-id did not hint flag-first: code=%d err=%s", code, stderr.String())
	}
}

// Fix 12：hook decide --provider 非 codex 等非法值报错。
func TestCLIHookDecideRejectsInvalidProvider(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"hook", "decide", "--provider", "banana"}, IO{Stdin: strings.NewReader(`{"command":"x"}`), Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "unsupported hook provider") {
		t.Fatalf("invalid hook provider was accepted: code=%d err=%s", code, stderr.String())
	}
}

// Fix 13：canary portable --format 非 text/json 报错。
func TestCLICanaryPortableRejectsInvalidFormat(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"canary", "portable", "--format", "xml", "--root", t.TempDir()}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "must be text or json") {
		t.Fatalf("invalid portable format was accepted: code=%d err=%s", code, stderr.String())
	}
}

// Fix 14：canary codex-hook-probe 成功时输出载荷文件路径/字节数。
func TestCLICodexHookProbePrintsPayloadInfo(t *testing.T) {
	var stdout bytes.Buffer
	code := Run("formal-gates", []string{"canary", "codex-hook-probe", "--payload-dir", t.TempDir()}, IO{Stdin: strings.NewReader(`{"event":"PreToolUse","tool_name":"x"}`), Stdout: &stdout})
	if code != 0 {
		t.Fatalf("codex-hook-probe failed: code=%d out=%s", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "codex-hook-probe: wrote") || !strings.Contains(stdout.String(), "payload to") {
		t.Fatalf("codex-hook-probe did not print payload info: %q", stdout.String())
	}
}

// Fix 18：hook decide 空 stdin 给友好错误。
func TestCLIHookDecideEmptyStdinFriendly(t *testing.T) {
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"hook", "decide"}, IO{Stdin: strings.NewReader(""), Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "requires a JSON decision payload") {
		t.Fatalf("empty hook stdin did not hint: code=%d err=%s", code, stderr.String())
	}
}

// Fix 18：--version 给出可用提示而非 unknown command。
func TestCLIVersionFlagPrintsVersionHint(t *testing.T) {
	var stdout bytes.Buffer
	if code := Run("formal-gates", []string{"--version"}, IO{Stdout: &stdout}); code != 0 || !strings.Contains(stdout.String(), "development build") {
		t.Fatalf("--version did not print a version hint: code=%d out=%s", code, stdout.String())
	}
}

// 需求 4：workflow start 未声明 --split 拒绝启动；--split yes 需钉死 retained-overall 或
// master。
func TestCLIStartRequiresSplitDeclaration(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "start", "--root", root, "--package-root", pkg, "--run-id", "no-split", "--requirement", "requirements.md", "--vcs", "git"}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "--split") {
		t.Fatalf("start without --split was accepted: code=%d err=%s", code, stderr.String())
	}
	stderr.Reset()
	if code := Run("formal-gates", []string{"workflow", "start", "--root", root, "--package-root", pkg, "--run-id", "yes-naked", "--requirement", "requirements.md", "--vcs", "git", "--split", "yes"}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "--retained-overall") {
		t.Fatalf("start --split yes without a pin was accepted: code=%d err=%s", code, stderr.String())
	}
	stderr.Reset()
	if code := Run("formal-gates", []string{"workflow", "start", "--root", root, "--package-root", pkg, "--run-id", "split-yes", "--requirement", "requirements.md", "--vcs", "git", "--split", "yes", "--retained-overall"}, IO{Stderr: &stderr}); code != 0 {
		t.Fatalf("start --split yes --retained-overall failed: code=%d err=%s", code, stderr.String())
	}
}

// 轻量路线（V2）：start --route lightweight 免拆分声明与拆分决定，需求登记后 Seal 三步
// 直达，跳过全部验证；封板摘要标注「本 run 未经任何验证」。--route lightweight 与
// --split yes 组合被拒。
func TestCLILightweightRouteStartToSeal(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	out := runCLI(t, "workflow", "start", "--root", root, "--package-root", pkg, "--run-id", "lightweight-cli", "--requirement", "requirements.md", "--vcs", "git", "--route", "lightweight")
	var state validate.RunState
	if err := json.Unmarshal([]byte(out), &state); err != nil {
		t.Fatal(err)
	}
	if state.RouteMode != "lightweight" {
		t.Fatalf("expected lightweight route mode, got %q", state.RouteMode)
	}
	if state.SplitDeclaration != "" {
		t.Fatalf("lightweight start must not record a split declaration, got %q", state.SplitDeclaration)
	}
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "start", "--root", root, "--package-root", pkg, "--run-id", "lightweight-bad", "--requirement", "requirements.md", "--vcs", "git", "--route", "lightweight", "--split", "yes"}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "--route lightweight") {
		t.Fatalf("start --route lightweight --split yes was accepted: code=%d err=%s", code, stderr.String())
	}
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--confirmed")
	summary := runCLI(t, "workflow", "seal", "--root", root, "--package-root", pkg, "--run-id", state.RunID)
	if !strings.Contains(summary, `"status": "SEALED"`) {
		t.Fatalf("lightweight seal output=%s", summary)
	}
	if !strings.Contains(summary, "本 run 未经任何验证") {
		t.Fatalf("lightweight seal summary must be marked unverified: %s", summary)
	}
}

// 需求 5 / 需求 6 的 CLI 层直接测试。

// TestCLIAbortRequiresUserConfirm verifies requirement 6 item 2: workflow abort
// is refused without the user-level --user-confirm signal, nothing is recorded on
// rejection, and only a user-confirmed abort terminates the run.
func TestCLIAbortRequiresUserConfirm(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, "cli-abort-confirm")
	before := cliStateBytes(t, root, state.RunID)
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "abort", "--root", root, "--package-root", pkg, "--run-id", state.RunID}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "requires --user-confirm") {
		t.Fatalf("abort without user confirm was accepted: code=%d err=%s", code, stderr.String())
	}
	if cliStateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected abort changed state")
	}
	out := runCLI(t, "workflow", "abort", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--user-confirm")
	if !strings.Contains(out, `"status": "ABORTED"`) {
		t.Fatalf("user-confirmed abort did not terminate the run: %s", out)
	}
}

// TestCLIResetRequiresUserApprove verifies requirement 5: workflow reset is
// refused without the user-level --user-approve authorization, nothing is
// recorded on rejection, and an approved reset returns a clean re-registrable
// state whose output explains what was kept and what was reset.
func TestCLIResetRequiresUserApprove(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, "cli-reset-approve")
	before := cliStateBytes(t, root, state.RunID)
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "reset", "--root", root, "--package-root", pkg, "--run-id", state.RunID}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "requires --user-approve") {
		t.Fatalf("reset without user approval was accepted: code=%d err=%s", code, stderr.String())
	}
	if cliStateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected reset changed state")
	}
	out := runCLI(t, "workflow", "reset", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--user-approve")
	var result validate.ResetResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("reset output=%s err=%v", out, err)
	}
	if result.State.RequirementConfirmed || result.State.Actions["product-review"].Status != "PENDING" || len(result.State.Dispatches) != 0 {
		t.Fatalf("approved reset did not reach a clean re-registrable state: %s", out)
	}
	if len(result.Kept) == 0 || len(result.Reset) == 0 {
		t.Fatalf("reset output missing kept/reset lists: %s", out)
	}
}

// TestCLIRequirementRebindRejectedWithInFlightDispatch verifies requirement 6
// item 5 through the CLI: workflow requirement --meaning is refused while a
// review dispatch is in flight (claimed, result not recorded).
func TestCLIRequirementRebindRejectedWithInFlightDispatch(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, "cli-rebind-inflight")
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--confirmed")
	// 准备并认领一轮 product-review，不记录结果。
	dispatchID := cliPrepareAction(t, root, pkg, state.RunID, "product-review")
	if _, err := validate.LoadRunState(root, state.RunID); err != nil {
		t.Fatal(err)
	}
	_ = dispatchID
	// 需求文档已改动，但存在在途审查派发 → 重绑被拒。
	mustWriteCLI(t, filepath.Join(root, "requirements.md"), "changed requirement\n")
	cliGit(t, root, "add", "--all")
	cliGit(t, root, "commit", "-m", "change requirement")
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--meaning", "changed"}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "has not recorded its result") {
		t.Fatalf("requirement rebind with an in-flight review dispatch was accepted: code=%d err=%s", code, stderr.String())
	}
}

// TestCLIQADesignIncrementalFlags verifies through the CLI the incremental QA
// design contract (改动 2): a second qa-design round returns only changes —
// --case-id modifies an existing case (id must exist), a bare --case adds a new
// one, --remove-case deletes by id — and unmentioned cases with their PASS status
// are retained automatically. Unknown --case-id is rejected.
func TestCLIQADesignIncrementalFlags(t *testing.T) {
	root, pkg := cliWorkflowFixture(t)
	state := startCLIWorkflow(t, root, pkg, "cli-incremental-flags")
	state = cliRecordAction(t, root, pkg, state, "requirements-clarification", "PASS")
	runCLI(t, "workflow", "requirement", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--confirmed")
	state = cliRecordAction(t, root, pkg, state, "product-review", "PASS")
	state = cliRecordAction(t, root, pkg, state, "start-readiness", "PASS")
	runCLI(t, "workflow", "slicing", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--decision", "no-split", "--note", "single coherent bounded unit")
	runCLI(t, "workflow", "route", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--mode", "full")
	state, _ = validate.LoadRunState(root, state.RunID)

	// 首次设计 2 个用例。
	designDispatch := cliPrepareAction(t, root, pkg, state.RunID, "qa-design")
	runCLI(t, "workflow", "qa-design", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", designDispatch,
		"--case", "direct rules", "--mode", "whitebox", "--procedure", "go test ./...", "--oracle", "tests pass", "--test", "whitebox_delivered_test.go::TestWhiteboxDirectRules",
		"--case", "public workflow", "--mode", "blackbox", "--procedure", "run documented CLI", "--oracle", "observable success")
	state, _ = validate.LoadRunState(root, state.RunID)
	if len(state.QACasesByMode[""]) != 2 {
		t.Fatalf("first design not recorded: %#v", state.QACasesByMode[""])
	}

	// 第二轮：修改 CASE-001（--case-id）、新增一个用例、删除 CASE-002（--remove-case）。
	// 用不同 reviewer 认领，避免与首轮设计的 reviewer 身份冲突。
	runCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "qa-design")
	state, _ = validate.LoadRunState(root, state.RunID)
	designDispatch = cliOpenDispatch(state, "action", "qa-design")
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", designDispatch, "--reviewer", "incremental-round-2")
	runCLI(t, "workflow", "qa-design", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", designDispatch,
		"--case", "direct rules v2", "--mode", "whitebox", "--procedure", "go test -run TestDirect ./...", "--oracle", "tests pass", "--test", "whitebox_delivered_test.go::TestWhiteboxDirectRules", "--case-id", "CASE-001",
		"--case", "recovery workflow", "--mode", "blackbox", "--procedure", "trigger recovery", "--oracle", "state restored",
		"--remove-case", "CASE-002")
	state, _ = validate.LoadRunState(root, state.RunID)
	if len(state.QACasesByMode[""]) != 2 {
		t.Fatalf("incremental merge did not retain exactly the expected set: %#v", state.QACasesByMode[""])
	}
	cases := state.QACasesByMode[""]
	byID := map[string]validate.QACase{}
	for _, testCase := range cases {
		byID[testCase.ID] = testCase
	}
	first, ok := byID["CASE-001"]
	if !ok || first.Description != "direct rules v2" || first.ReviewStatus != "PENDING" {
		t.Fatalf("--case-id modify not applied: %#v", cases)
	}
	if _, leaked := byID["CASE-002"]; leaked {
		t.Fatalf("--remove-case did not delete CASE-002: %#v", cases)
	}
	added, ok := byID["CASE-003"]
	if !ok || added.Mode != "blackbox" || added.ReviewStatus != "PENDING" {
		t.Fatalf("new case not added: %#v", cases)
	}

	// 引用不存在的 id 被拒：--case-id CASE-999 报错，状态不变。
	runCLI(t, "workflow", "prepare-action", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--action", "qa-design")
	state, _ = validate.LoadRunState(root, state.RunID)
	designDispatch = cliOpenDispatch(state, "action", "qa-design")
	runCLI(t, "workflow", "claim-dispatch", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", designDispatch, "--reviewer", "incremental-round-3")
	before := cliStateBytes(t, root, state.RunID)
	var stderr bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "qa-design", "--root", root, "--package-root", pkg, "--run-id", state.RunID, "--dispatch", designDispatch,
		"--case", "x", "--mode", "blackbox", "--procedure", "y", "--oracle", "z", "--case-id", "CASE-999"}, IO{Stderr: &stderr}); code == 0 || !strings.Contains(stderr.String(), "references unknown id") {
		t.Fatalf("unknown --case-id was accepted: code=%d err=%s", code, stderr.String())
	}
	if cliStateBytes(t, root, state.RunID) != before {
		t.Fatal("rejected unknown --case-id changed state")
	}
}
