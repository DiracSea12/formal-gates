package cli

import (
	"bytes"
	"formal-gates/internal/validate"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunSupportsFormalGatesPackageValidate(t *testing.T) {
	root := repoRoot(t)
	var stdout, stderr bytes.Buffer

	code := Run("formal-gates", []string{"package", "validate", "--root", root}, IO{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	if code != 0 {
		t.Fatalf("expected package validate to pass, code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "PASS formal-gates package validation") {
		t.Fatalf("unexpected stdout: %q", stdout.String())
	}
}

func TestRunAllowsTransitionValidateEntrypoint(t *testing.T) {
	root := repoRoot(t)
	var stdout bytes.Buffer

	code := Run("formal-gates-validate", []string{"package", "--root", root}, IO{Stdout: &stdout})

	if code != 0 {
		t.Fatalf("expected transition entrypoint to pass, code=%d stdout=%q", code, stdout.String())
	}
}

func TestRunHookDecideDeniesPassWithoutArtifact(t *testing.T) {
	payload := `{"command":"formal-gates workflow record-stage --gate complexity-gate --verdict PASS --workflow-id wf --change-snapshot snap"}`
	var stdout bytes.Buffer

	code := Run("formal-gates", []string{"hook", "decide"}, IO{
		Stdin:  strings.NewReader(payload),
		Stdout: &stdout,
	})

	if code != 2 {
		t.Fatalf("expected deny exit code 2, got %d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"decision":"deny"`) {
		t.Fatalf("expected deny JSON, got %q", stdout.String())
	}
}

func TestRunPromptValidateJSON(t *testing.T) {
	root := repoRoot(t)
	var stdout bytes.Buffer

	code := Run("formal-gates", []string{
		"prompt", "validate",
		"--root", root,
		"--text", "The previous findings say this should pass",
		"--format", "json",
	}, IO{Stdout: &stdout})

	if code == 0 {
		t.Fatalf("expected contaminated prompt to fail")
	}
	if !strings.Contains(stdout.String(), `"label"`) {
		t.Fatalf("expected JSON violation output, got %q", stdout.String())
	}
}

func TestRunHelpCommandsExitZero(t *testing.T) {
	cases := [][]string{
		{"--help"},
		{"package", "--help"},
		{"artifact", "--help"},
		{"install", "--help"},
		{"prompt", "--help"},
		{"hook", "--help"},
		{"hook", "decide", "--help"},
		{"canary", "portable", "--help"},
		{"canary", "codex-hook", "--help"},
		{"canary", "codex-hook-probe", "--help"},
		{"workflow", "snapshot", "--help"},
		{"workflow", "record-stage", "--help"},
		{"workflow", "verify-admission", "--help"},
		{"workflow", "final-verification", "--help"},
		{"workflow", "cleanup", "--help"},
		{"workflow", "show", "--help"},
		{"gate", "record", "--help"},
		{"gate", "verify-admission", "--help"},
		{"gate", "show", "--help"},
		{"receipt", "register", "--help"},
		{"receipt", "capture", "--help"},
		{"receipt", "finalize", "--help"},
		{"receipt", "validate", "--help"},
		{"receipt", "preflight", "--help"},
		{"complexity", "check", "--help"},
	}

	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			code := Run("formal-gates", args, IO{Stdout: &stdout, Stderr: &stderr})
			if code != 0 {
				t.Fatalf("expected help to exit 0, code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if !strings.Contains(stdout.String(), "Usage") {
				t.Fatalf("expected usage text on stdout, got %q", stdout.String())
			}
		})
	}
}

func TestRunGateRecordShowAndVerifyAdmission(t *testing.T) {
	dir := t.TempDir()
	writeCLIArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
	var stdout bytes.Buffer

	code := Run("formal-gates", []string{
		"gate", "record",
		"--worktree", dir,
		"--gate", "qa-test-gate",
		"--verdict", "PASS",
		"--mode", "formal",
		"--stage", "Execution",
		"--artifact", "qa-test-gate.md",
		"--workflow-id", "wf",
		"--change-snapshot", "snap",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected gate record to pass, code=%d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "GATE_STATE_RECORDED gate=qa-test-gate verdict=PASS workflowId=wf") {
		t.Fatalf("unexpected record stdout: %q", stdout.String())
	}

	stdout.Reset()
	code = Run("formal-gates", []string{
		"gate", "verify-admission",
		"--worktree", dir,
		"--gate", "complexity-gate",
		"--workflow-id", "wf",
		"--change-snapshot", "snap",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected gate admission to pass, code=%d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "GATE_STATE_ADMISSION_PASS gate=complexity-gate") {
		t.Fatalf("unexpected admission stdout: %q", stdout.String())
	}

	stdout.Reset()
	code = Run("formal-gates", []string{"gate", "show", "--worktree", dir, "--format", "text"}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected gate show to pass, code=%d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "gate=qa-test-gate verdict=PASS workflowId=wf changeSnapshot=snap") {
		t.Fatalf("unexpected show stdout: %q", stdout.String())
	}
}

func TestRunWorkflowSnapshotRecordStageAndAdmission(t *testing.T) {
	dir := t.TempDir()
	mustWriteCLI(t, filepath.Join(dir, "src.txt"), "source\n")
	writeCLIArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
	var stdout bytes.Buffer

	code := Run("formal-gates", []string{
		"workflow", "snapshot",
		"--worktree", dir,
		"--vcs", "file-hash",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected workflow snapshot to pass, code=%d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"changeSnapshot": "files.`) {
		t.Fatalf("unexpected snapshot stdout: %q", stdout.String())
	}

	stdout.Reset()
	code = Run("formal-gates", []string{
		"workflow", "record-stage",
		"--worktree", dir,
		"--gate", "qa-test-gate",
		"--verdict", "PASS",
		"--mode", "formal",
		"--stage", "Execution",
		"--artifact", "qa-test-gate.md",
		"--workflow-id", "wf",
		"--change-snapshot", "snap",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected workflow record-stage to pass, code=%d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "GATE_WORKFLOW_RECORDED gate=qa-test-gate verdict=PASS workflowId=wf") {
		t.Fatalf("unexpected record-stage stdout: %q", stdout.String())
	}

	stdout.Reset()
	code = Run("formal-gates", []string{
		"workflow", "verify-admission",
		"--worktree", dir,
		"--gate", "complexity-gate",
		"--workflow-id", "wf",
		"--change-snapshot", "snap",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected workflow admission to pass, code=%d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "GATE_WORKFLOW_ADMISSION_PASS gate=complexity-gate") {
		t.Fatalf("unexpected admission stdout: %q", stdout.String())
	}
}

func TestRunWorkflowFinalVerification(t *testing.T) {
	dir := t.TempDir()
	mustWriteCLI(t, filepath.Join(dir, ".claude", "gates", "artifacts", "attempt.json"), `{"ok":true}`+"\n")
	attempts := filepath.Join(dir, "attempts.json")
	mustWriteCLI(t, attempts, `[{"status":"PASS","accepted":true,"artifact":".claude/gates/artifacts/attempt.json"}]`)
	var stdout bytes.Buffer

	code := Run("formal-gates", []string{
		"workflow", "final-verification",
		"--worktree", dir,
		"--attempts-file", attempts,
		"--output", ".claude/gates/artifacts/final-verification.json",
		"--workflow-id", "wf",
		"--change-snapshot", "snap",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected final-verification to pass, code=%d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "GATE_WORKFLOW_FINAL_VERIFICATION status=PASS accepted=1 attempts=1") {
		t.Fatalf("unexpected final-verification stdout: %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "gates", "artifacts", "final-verification.json")); err != nil {
		t.Fatal(err)
	}
}

func TestRunWorkflowFinalVerificationRecordsFinalQA(t *testing.T) {
	dir := t.TempDir()
	mustWriteCLI(t, filepath.Join(dir, ".claude", "gates", "artifacts", "attempt.json"), `{"ok":true}`+"\n")
	writeCLIArtifact(t, dir, "qa-test-gate", "FinalExecution", "wf", "snap")
	var stdout bytes.Buffer

	code := Run("formal-gates", []string{
		"workflow", "final-verification",
		"--worktree", dir,
		"--attempts-json", `[{"status":"PASS","accepted":true,"artifact":".claude/gates/artifacts/attempt.json"}]`,
		"--output", ".claude/gates/artifacts/final-verification.json",
		"--record-final-qa",
		"--final-qa-artifact", "qa-test-gate.md",
		"--actor", "qa-reviewer",
		"--workflow-id", "wf",
		"--change-snapshot", "snap",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected final-verification final QA record to pass, code=%d stdout=%q", code, stdout.String())
	}
	state, result := validate.GateShow(validate.GateShowOptions{Worktree: dir})
	if !result.OK() {
		t.Fatalf("expected gate state to show, got %#v", result.Failures)
	}
	entry := state.Gates["qa-test-gate"]
	if entry.Stage != "FinalExecution" || entry.Actor != "qa-reviewer" {
		t.Fatalf("unexpected final QA entry: %#v", entry)
	}
}

func TestRunWorkflowFinalVerificationBlocksMissingArtifact(t *testing.T) {
	dir := t.TempDir()
	var stdout bytes.Buffer

	code := Run("formal-gates", []string{
		"workflow", "final-verification",
		"--worktree", dir,
		"--attempts-json", `[{"status":"PASS","accepted":true,"artifact":".claude/gates/artifacts/missing.json"}]`,
		"--output", ".claude/gates/artifacts/final-verification.json",
		"--workflow-id", "wf",
		"--change-snapshot", "snap",
	}, IO{Stdout: &stdout})
	if code == 0 {
		t.Fatal("expected missing accepted artifact to fail")
	}
	if !strings.Contains(stdout.String(), "GATE_WORKFLOW_BLOCKED") || !strings.Contains(stdout.String(), "does not exist") {
		t.Fatalf("unexpected failure stdout: %q", stdout.String())
	}
}

func TestRunWorkflowCleanupDryRunAndExecute(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, ".artifacts", "scratch", "run", "scratch.txt")
	mustWriteCLI(t, target, "scratch\n")
	var stdout bytes.Buffer

	code := Run("formal-gates", []string{
		"workflow", "cleanup",
		"--worktree", dir,
		"--dry-run",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected cleanup dry-run to pass, code=%d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"dryRun": true`) || !strings.Contains(stdout.String(), `"status": "would-remove"`) {
		t.Fatalf("unexpected dry-run stdout: %q", stdout.String())
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatal("dry-run removed scratch file")
	}

	stdout.Reset()
	code = Run("formal-gates", []string{
		"workflow", "cleanup",
		"--worktree", dir,
		"--path", ".artifacts/scratch/run/scratch.txt",
		"--execute",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected cleanup execute to pass, code=%d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"dryRun": false`) || !strings.Contains(stdout.String(), `"status": "removed"`) {
		t.Fatalf("unexpected execute stdout: %q", stdout.String())
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("expected scratch file removed, err=%v", err)
	}
}

func TestRunReceiptCaptureAndPreflight(t *testing.T) {
	dir := t.TempDir()
	payload := `{"workflowId":"wf","gate":"complexity-gate","subagentId":"subagent-1","dispatchId":"dispatch-1"}`
	var stdout bytes.Buffer

	code := Run("formal-gates", []string{
		"receipt", "capture",
		"--worktree", dir,
		"--provider", "codex",
		"--event", "SubagentStart",
	}, IO{Stdin: strings.NewReader(payload), Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected receipt capture to pass, code=%d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"normalizedEvent": "subagent_start"`) {
		t.Fatalf("unexpected capture stdout: %q", stdout.String())
	}

	stdout.Reset()
	code = Run("formal-gates", []string{
		"receipt", "preflight",
		"--worktree", dir,
		"--host", "codex",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected receipt preflight to produce diagnostic JSON, code=%d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"status": "UNSUPPORTED_HOST_RECEIPT"`) {
		t.Fatalf("unexpected preflight stdout: %q", stdout.String())
	}
}

func TestRunCanaryPortable(t *testing.T) {
	root := repoRoot(t)
	var stdout bytes.Buffer

	code := Run("formal-gates", []string{
		"canary", "portable",
		"--root", root,
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected portable canary to pass, code=%d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "PASS package-validate") || !strings.Contains(stdout.String(), "PASS install-cursor-native-runtime") {
		t.Fatalf("unexpected canary stdout: %q", stdout.String())
	}
}

func TestRunCanaryCodexHookProbe(t *testing.T) {
	dir := t.TempDir()
	payload := `{"hook_event_name":"PreToolUse","tool_name":"Shell","input":{"command":"formal-gates workflow record-stage --gate complexity-gate --verdict PASS --workflow-id wf --change-snapshot snap"}}`
	var stdout bytes.Buffer

	code := Run("formal-gates", []string{
		"canary", "codex-hook-probe",
		"--payload-dir", dir,
	}, IO{Stdin: strings.NewReader(payload), Stdout: &stdout})
	if code != 2 {
		t.Fatalf("expected denied hook probe exit code 2, got %d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"decision":"deny"`) {
		t.Fatalf("unexpected hook probe stdout: %q", stdout.String())
	}
}

func TestRunComplexityCheckReviewExitCode(t *testing.T) {
	dir := t.TempDir()
	var stdout bytes.Buffer

	code := Run("formal-gates", []string{
		"complexity", "check",
		"--worktree", dir,
		"--vcs", "none",
		"--task-type", "bugfix",
	}, IO{Stdout: &stdout})
	if code != 2 {
		t.Fatalf("expected REVIEW exit code 2, got %d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "Complexity Gate: REVIEW") {
		t.Fatalf("unexpected complexity stdout: %q", stdout.String())
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			t.Fatal("go.mod not found")
		}
		dir = next
	}
}

func mustWriteCLI(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func writeCLIArtifact(t *testing.T, dir, gate, stage, workflowID, snapshot string) {
	t.Helper()
	lines := []string{
		"Review mode: ZERO_CONTEXT_FORMAL",
		"Prompt contamination check: PASS",
		"Semantic anti-anchor check: PASS",
		"Prompt source: agents/" + gate + ".md",
		"Zero-context reviewer: YES",
		"Independent agent: YES",
		"Context bundle: bundle.md sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"Dispatch prompt artifact: prompt.md sha256=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"No-anchor prompt: YES",
	}
	if gate == "qa-test-gate" {
		lines = append(lines,
			"Approved case set: cases.md",
			"QA-owned evidence: qa-evidence.md",
			"Case-to-artifact binding: bound",
		)
	}
	lines = append(lines,
		"gate_route:",
		"  workflow_id: "+workflowID,
		"  change_snapshot: "+snapshot,
		"  next_action: proceed",
		"  rework_owner: none",
		"  rerun_from: none",
	)
	if err := os.WriteFile(filepath.Join(dir, gate+".md"), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}
