package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexHookProbeRecordsWithoutMakingHookDecision(t *testing.T) {
	dir := t.TempDir()
	payload := []byte(`{"hook_event_name":"PreToolUse","tool_name":"Shell","input":{"command":"formal-gates workflow record-gate --gate complexity-gate --status PASS --run-id wf"}}`)

	probe, result := CodexHookProbe(CodexHookProbeOptions{
		PayloadDir: dir,
		Payload:    payload,
	})
	if !result.OK() {
		t.Fatalf("expected hook probe to pass, got %#v", result.Failures)
	}
	if probe.ExitCode != 0 {
		t.Fatalf("expected passive recorder to exit successfully, got %#v", probe)
	}
	if _, err := os.Stat(filepath.FromSlash(probe.PayloadPath)); err != nil {
		t.Fatalf("expected payload artifact: %v", err)
	}
}

func TestCanaryExecutionResultExecutesCommittedChecks(t *testing.T) {
	root := t.TempDir()
	if err := initializeCanaryGit(root); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "delivery.txt"), []byte(canaryDeliveryContents), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "whitebox_delivered_test.go"), []byte(whiteboxDeliveredTestCode), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := commitCanaryGit(root, "candidate fixtures"); err != nil {
		t.Fatal(err)
	}

	blackbox, err := canaryExecutionResult(root, QACase{ID: "CASE-BLACKBOX", Mode: "blackbox", Procedure: canaryBlackboxProcedure, Oracle: canaryBlackboxOracle})
	if err != nil {
		t.Fatal(err)
	}
	if blackbox.Observation != `git show HEAD:delivery.txt stdout="delivery"` || blackbox.OracleResult != `expected stdout="delivery"; actual stdout="delivery"; equal=true` {
		t.Fatalf("blackbox result did not record the actual git output and comparison: %#v", blackbox)
	}

	whitebox, err := canaryExecutionResult(root, QACase{ID: "CASE-WHITEBOX", Mode: "whitebox", Procedure: canaryWhiteboxProcedure, Oracle: canaryWhiteboxOracle})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(whitebox.Observation, canaryWhiteboxMarker) || whitebox.OracleResult != `expected stdout to contain "func TestWhiteboxDirectBehavior(t *testing.T) {}"; contains=true` {
		t.Fatalf("whitebox result did not record the actual git output and comparison: %#v", whitebox)
	}
}

func TestCanaryExecutionResultRejectsCommandAndOutputFailures(t *testing.T) {
	missing := t.TempDir()
	if err := initializeCanaryGit(missing); err != nil {
		t.Fatal(err)
	}
	if _, err := canaryExecutionResult(missing, QACase{ID: "CASE-MISSING", Mode: "blackbox", Procedure: canaryBlackboxProcedure, Oracle: canaryBlackboxOracle}); err == nil {
		t.Fatal("missing committed delivery file was accepted")
	}

	mismatched := t.TempDir()
	if err := initializeCanaryGit(mismatched); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mismatched, "delivery.txt"), []byte("unexpected delivery\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mismatched, "whitebox_delivered_test.go"), []byte("package whiteboxfixture\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := commitCanaryGit(mismatched, "mismatched fixtures"); err != nil {
		t.Fatal(err)
	}
	if _, err := canaryExecutionResult(mismatched, QACase{ID: "CASE-BLACKBOX", Mode: "blackbox", Procedure: canaryBlackboxProcedure, Oracle: canaryBlackboxOracle}); err == nil || !strings.Contains(err.Error(), "expected \"delivery\", got \"unexpected delivery\"") {
		t.Fatalf("blackbox output mismatch was not rejected with the actual comparison: %v", err)
	}
	if _, err := canaryExecutionResult(mismatched, QACase{ID: "CASE-WHITEBOX", Mode: "whitebox", Procedure: canaryWhiteboxProcedure, Oracle: canaryWhiteboxOracle}); err == nil || !strings.Contains(err.Error(), canaryWhiteboxMarker) {
		t.Fatalf("whitebox output mismatch was not rejected with the expected declaration: %v", err)
	}
}
