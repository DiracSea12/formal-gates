package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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
		"--case", "direct rules", "--kind", "STATIC", "--procedure", "go test ./...", "--oracle", "tests pass",
		"--case", "public workflow", "--kind", "LIVE", "--procedure", "run documented CLI", "--oracle", "observable success")
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
	if state.CurrentSnapshot != cliGit(t, root, "rev-parse", "HEAD") || state.QAExecution.Cases[0].Kind != "STATIC" || state.Gates["quality"].DispatchID != gateDispatch {
		t.Fatalf("CLI state lost native bindings: %s", out)
	}
	summary := runCLI(t, "workflow", "seal", "--root", root, "--package-root", pkg, "--run-id", state.RunID)
	if !strings.Contains(summary, `"status": "SEALED"`) {
		t.Fatalf("seal output=%s", summary)
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
	code := Run("formal-gates", []string{"workflow", "start", "--root", root, "--package-root", pkg, "--run-id", "artifact-cli", "--requirement", "requirements.md", "--requirement-artifact", "z-solution.md", "--vcs", "git"}, IO{Stdout: &stdout, Stderr: &stderr})
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
	for _, group := range []string{"workflow", "lifecycle", "canary", "hook"} {
		for _, flag := range []string{"-h", "--help", "help"} {
			var stdout, stderr bytes.Buffer
			code := Run("formal-gates", []string{group, flag}, IO{Stdout: &stdout, Stderr: &stderr})
			if code != 0 {
				t.Fatalf("%s %s code=%d err=%s", group, flag, code, stderr.String())
			}
			if !strings.Contains(stdout.String(), "Usage: formal-gates <command>") {
				t.Fatalf("%s %s omitted usage: %s", group, flag, stdout.String())
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
			runInstalledCLI(t, binary, root, nil, "", "workflow", "start", "--root", root, "--package-root", pkg, "--run-id", runID, "--requirement", "requirements.md", "--requirement-artifact", "design.md", "--vcs", "git")

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
	cmd.Env = append(os.Environ(), environment...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s failed: %v: %s", binary, strings.Join(args, " "), err, output)
	}
	return string(output)
}

func startCLIWorkflow(t *testing.T, root, pkg, id string) validate.RunState {
	t.Helper()
	out := runCLI(t, "workflow", "start", "--root", root, "--package-root", pkg, "--run-id", id, "--requirement", "requirements.md", "--requirement-artifact", "design.md", "--vcs", "git")
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
	for id, dispatch := range state.Dispatches {
		if dispatch.TargetKind == kind && dispatch.Target == target && (dispatch.Status == "OPEN" || dispatch.Status == "CLAIMED") {
			return id
		}
	}
	return ""
}

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
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
