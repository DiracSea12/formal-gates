package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"formal-gates/internal/validate"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

const validCLIReviewerDispatch = "formal_gate_dispatch: complexity-gate\nCurrent requirement: requirements/current.md\nCurrent diff or proposed change: git diff base --\n"

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
	if !strings.Contains(stdout.String(), `"permissionDecision":"deny"`) {
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

func TestRunPromptPrepareWritesExactValidatedMessage(t *testing.T) {
	root := t.TempDir()
	mustWriteCLI(t, filepath.Join(root, "hooks", "pollution-patterns.json"), `{"english":{"patternGroups":[]},"chinese":{"termGroups":[]}}`)
	run := filepath.Join(root, ".claude", "gates", "runs", "wf", "restricted")
	prompt := filepath.Join(run, "complexity", "prompt.txt")
	bundle := filepath.Join(run, "complexity", "bundle.json")
	mustWriteCLI(t, bundle, "{}\n")
	var stdout, stderr bytes.Buffer
	code := Run("formal-gates", []string{
		"prompt", "prepare", "--root", root,
		"--output", prompt,
		"--gate", "complexity-gate",
		"--current-requirement", "requirements/current.md",
		"--current-diff", "git diff base --",
		"--worktree", root,
		"--change-snapshot", "base..snapshot",
		"--review-artifact", ".claude/gates/runs/wf/restricted/complexity/review.json",
		"--policy-id", "complexity.post-development.v2",
		"--context-bundle", bundle,
	}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("prepare failed: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(prompt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "contextBundle=restricted/complexity/bundle.json sha256=") {
		t.Fatalf("prepared prompt missing binding: %s", data)
	}
	var prepared validate.PreparedDispatchPrompt
	if err := json.Unmarshal(stdout.Bytes(), &prepared); err != nil {
		t.Fatalf("invalid prepare result %q: %v", stdout.String(), err)
	}
	sum := sha256.Sum256(data)
	wantHash := hex.EncodeToString(sum[:])
	if prepared.SHA256 != wantHash {
		t.Fatalf("prepared hash mismatch: got %s want %s", prepared.SHA256, wantHash)
	}
}

func TestRunQADesignReviewLifecycleUsesBoundCaseSet(t *testing.T) {
	root := t.TempDir()
	workflowID, snapshot := "wf", "snap"
	run := filepath.Join(root, ".claude", "gates", "runs", workflowID)
	mustWriteCLI(t, filepath.Join(root, "hooks", "pollution-patterns.json"), `{"english":{"patternGroups":[]},"chinese":{"termGroups":[]}}`)
	mustWriteCLI(t, filepath.Join(run, "restricted", "qa-design", "requirement.txt"), "requirement\n")
	runCLI := func(args []string, input string) bytes.Buffer {
		t.Helper()
		var stdout, stderr bytes.Buffer
		if code := Run("formal-gates", args, IO{Stdin: strings.NewReader(input), Stdout: &stdout, Stderr: &stderr}); code != 0 {
			t.Fatalf("command failed: %v\nstdout=%s\nstderr=%s", args, stdout.String(), stderr.String())
		}
		return stdout
	}
	runCLI([]string{"artifact", "compose-context-bundle", "--root", root, "--run-dir", run, "--workflow-id", workflowID, "--change-snapshot", snapshot, "--output", "restricted/qa-design/context-bundle.json", "--input", "restricted/qa-design/requirement.txt"}, "")
	designArtifact := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", workflowID, "restricted", "qa-design", "cases.md"))
	registrationOut := runCLI([]string{"receipt", "register", "--provider", "codex", "--worktree", root, "--run-dir", run, "--context-bundle", "restricted/qa-design/context-bundle.json", "--qa-case-count", "1", "--artifact", designArtifact, "--gate", "qa-test-gate", "--stage", "Design", "--workflow-id", workflowID, "--change-snapshot", snapshot}, "")
	var registration validate.ReceiptRegistration
	if err := json.Unmarshal(registrationOut.Bytes(), &registration); err != nil {
		t.Fatal(err)
	}
	designPath := filepath.Join(root, filepath.FromSlash(designArtifact))
	beforeInvalidSubmit, err := os.ReadFile(designPath)
	if err != nil {
		t.Fatal(err)
	}
	if code := Run("formal-gates", []string{
		"receipt", "submit", "--worktree", root, "--artifact", designArtifact,
		"--design-case", "1",
		"--case-value", "claim", "--case-value", "source", "--case-value", "action",
		"--case-value", "oracle", "--case-value", "failure signal", "--case-value", "evidence",
	}, IO{}); code == 0 {
		t.Fatal("incomplete QA Design semantic group was accepted")
	}
	afterInvalidSubmit, err := os.ReadFile(designPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeInvalidSubmit, afterInvalidSubmit) {
		t.Fatal("rejected CLI QA Design submission changed the generated artifact")
	}
	payload, _ := json.Marshal(map[string]string{"workflowId": workflowID, "changeSnapshot": snapshot, "gate": "qa-test-gate", "stage": "Design", "subagentId": "designer", "dispatchId": registration.DispatchID, "dispatchRegistrationArtifact": registration.DispatchRegistrationArtifact})
	runCLI([]string{"receipt", "capture", "--provider", "codex", "--event", "SubagentStart", "--worktree", root, "--run-dir", run}, string(payload))
	runCLI([]string{"receipt", "submit", "--worktree", root, "--artifact", designArtifact, "--design-case", "1", "--case-value", "claim", "--case-value", "source", "--case-value", "action", "--case-value", "oracle", "--case-value", "failure signal", "--case-value", "evidence", "--case-value", "gap"}, "")
	runCLI([]string{"receipt", "capture", "--provider", "codex", "--event", "SubagentStop", "--worktree", root, "--run-dir", run}, string(payload))
	designReceiptOut := runCLI([]string{"receipt", "finalize", "--provider", "codex", "--worktree", root, "--run-dir", run, "--artifact", designArtifact, "--gate", "qa-test-gate", "--stage", "Design", "--workflow-id", workflowID}, "")
	var designReceipt validate.ReceiptFinalizeOutput
	if err := json.Unmarshal(designReceiptOut.Bytes(), &designReceipt); err != nil {
		t.Fatal(err)
	}
	prompt := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", workflowID, "restricted", "qa-design", "design-review-prompt.txt"))
	reviewArtifact := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", workflowID, "restricted", "qa-design", "design-review.json"))
	runCLI([]string{"prompt", "prepare", "--root", root, "--output", prompt, "--gate", "qa-test-gate", "--stage", "Design Review", "--current-requirement", "requirements/current.md", "--current-diff", designArtifact, "--worktree", root, "--change-snapshot", snapshot, "--review-artifact", reviewArtifact, "--policy-id", "qa.design-review.v2", "--context-bundle", filepath.ToSlash(filepath.Join(".claude", "gates", "runs", workflowID, "restricted", "qa-design", "context-bundle.json"))}, "")
	caseLogical := "restricted/qa-design/cases.md"
	designReceiptLogical := strings.TrimPrefix(filepath.ToSlash(designReceipt.ReceiptArtifact), filepath.ToSlash(filepath.Join(".claude", "gates", "runs", workflowID))+"/")
	reviewRegistrationOut := runCLI([]string{"receipt", "register", "--provider", "codex", "--worktree", root, "--run-dir", run, "--context-bundle", "restricted/qa-design/context-bundle.json", "--prompt", "restricted/qa-design/design-review-prompt.txt", "--qa-design-case-set", caseLogical, "--qa-design-receipt", designReceiptLogical, "--artifact", reviewArtifact, "--gate", "qa-test-gate", "--stage", "Design Review", "--workflow-id", workflowID, "--change-snapshot", snapshot}, "")
	var reviewRegistration validate.ReceiptRegistration
	if err := json.Unmarshal(reviewRegistrationOut.Bytes(), &reviewRegistration); err != nil {
		t.Fatal(err)
	}
	reviewEvent, _ := json.Marshal(map[string]string{"workflowId": workflowID, "changeSnapshot": snapshot, "gate": "qa-test-gate", "stage": "Design Review", "subagentId": "reviewer", "dispatchId": reviewRegistration.DispatchID, "dispatchRegistrationArtifact": reviewRegistration.DispatchRegistrationArtifact})
	runCLI([]string{"receipt", "capture", "--provider", "codex", "--event", "SubagentStart", "--worktree", root, "--run-dir", run}, string(reviewEvent))
	reviewPath := filepath.Join(root, filepath.FromSlash(reviewArtifact))
	reviewBytes, err := os.ReadFile(reviewPath)
	if err != nil {
		t.Fatal(err)
	}
	var reviewEnvelope validate.FormalGateEvidence
	if err := json.Unmarshal(reviewBytes, &reviewEnvelope); err != nil {
		t.Fatal(err)
	}
	var reviewPayload validate.ReviewerPayload
	if err := json.Unmarshal(reviewEnvelope.Payload, &reviewPayload); err != nil {
		t.Fatal(err)
	}
	submitArgs := []string{"receipt", "submit", "--worktree", root, "--artifact", reviewArtifact}
	for index := range reviewPayload.Checks {
		submitArgs = append(submitArgs, "--check", strconv.Itoa(index+1), "--status", "PASS", "--message", "reviewed")
	}
	runCLI(submitArgs, "")
	runCLI([]string{"receipt", "capture", "--provider", "codex", "--event", "SubagentStop", "--worktree", root, "--run-dir", run}, string(reviewEvent))
	reviewReceiptOut := runCLI([]string{"receipt", "finalize", "--provider", "codex", "--worktree", root, "--run-dir", run, "--artifact", reviewArtifact, "--gate", "qa-test-gate", "--stage", "Design Review", "--workflow-id", workflowID}, "")
	var reviewReceipt validate.ReceiptFinalizeOutput
	if err := json.Unmarshal(reviewReceiptOut.Bytes(), &reviewReceipt); err != nil {
		t.Fatal(err)
	}
	runCLI([]string{"receipt", "validate", "--worktree", root, "--receipt", reviewReceipt.ReceiptArtifact, "--artifact", reviewArtifact, "--gate", "qa-test-gate", "--stage", "Design Review", "--workflow-id", workflowID, "--change-snapshot", snapshot}, "")
	runCLI([]string{"artifact", "validate", "--root", root, "--file", reviewArtifact, "--gate", "qa-test-gate", "--stage", "Design Review", "--workflow-id", workflowID, "--change-snapshot", snapshot}, "")
}

func TestRunHelpCommandsExitZero(t *testing.T) {
	cases := [][]string{
		{"--help"},
		{"package", "--help"},
		{"artifact", "--help"},
		{"handoff", "--help"},
		{"install", "--help"},
		{"prompt", "--help"},
		{"prompt", "prepare", "--help"},
		{"hook", "--help"},
		{"hook", "decide", "--help"},
		{"canary", "portable", "--help"},
		{"canary", "codex-hook", "--help"},
		{"canary", "codex-hook-probe", "--help"},
		{"behavior", "evaluate", "--help"},
		{"policy", "show", "--help"},
		{"workflow", "snapshot", "--help"},
		{"workflow", "record-stage", "--help"},
		{"workflow", "record-transition", "--help"},
		{"workflow", "verify-admission", "--help"},
		{"workflow", "final-verification", "--help"},
		{"workflow", "cleanup", "--help"},
		{"workflow", "show", "--help"},
		{"gate", "record", "--help"},
		{"gate", "verify-admission", "--help"},
		{"gate", "show", "--help"},
		{"receipt", "register", "--help"},
		{"receipt", "submit", "--help"},
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

func TestRunHelpExposesOnlyRoleSpecificStaticInputs(t *testing.T) {
	var stdout bytes.Buffer
	if code := Run("formal-gates", []string{"--help"}, IO{Stdout: &stdout}); code != 0 {
		t.Fatalf("help failed: code=%d stdout=%q", code, stdout.String())
	}
	help := stdout.String()
	for _, removed := range []string{"--check-evidence", "--carry-source <", "--hop <"} {
		if strings.Contains(help, removed) {
			t.Fatalf("help still exposes removed static mini-language %q: %s", removed, help)
		}
	}
	for _, required := range []string{"--qa-design-case-set", "--qa-design-receipt", "--complexity-statistics", "--carry-source-closure", "--hop-from", "--hop-repair"} {
		if !strings.Contains(help, required) {
			t.Fatalf("help is missing role-specific input %q: %s", required, help)
		}
	}
}

func TestRunTopLevelHelpDocumentsCurrentRecordingContract(t *testing.T) {
	var stdout bytes.Buffer
	code := Run("formal-gates", []string{"--help"}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected top-level help to pass, code=%d stdout=%q", code, stdout.String())
	}
	help := stdout.String()
	for _, want := range []string{
		"artifact validate --root <repo> --file <artifact> --gate <gate-id> --workflow-id <id> --change-snapshot <snapshot> [--stage <stage>]",
		"gate record       --worktree <repo> --gate <gate-id> --verdict <verdict> --artifact <artifact>",
		"gate show         --worktree <repo> --state <active-run-json>",
		"workflow record-stage --worktree <repo> [--run-dir <dir>] --gate <gate-id> --verdict <verdict> --artifact <artifact>",
		"workflow record-transition --worktree <repo> [--run-dir <dir>] --artifact <carry-arbiter.json>",
		"[--mode <mode>] [--stage <stage>] [--state <active-run-json>]",
		"workflow verify-admission --worktree <repo> [--run-dir <dir>] --gate <gate-id>",
		"workflow final-verification --worktree <repo> [--run-dir <dir>]",
		"[--state <active-run-json>] [--record-final-qa --final-qa-artifact <artifact> --actor <actor>]",
	} {
		if !strings.Contains(help, want) {
			t.Fatalf("top-level help is missing %q:\n%s", want, help)
		}
	}
	if strings.Contains(help, "[--artifact <artifact>]") {
		t.Fatalf("top-level help still describes record artifacts as optional:\n%s", help)
	}
}

func TestRunStateHelpDocumentsActiveWorkflowRun(t *testing.T) {
	for _, args := range [][]string{{"gate", "record"}, {"gate", "verify-admission"}, {"workflow", "record-stage"}, {"workflow", "record-transition"}, {"workflow", "verify-admission"}, {"workflow", "final-verification"}} {
		var stdout bytes.Buffer
		if code := Run("formal-gates", append(args, "--help"), IO{Stdout: &stdout}); code != 0 {
			t.Fatalf("help failed for %v: code=%d stdout=%q", args, code, stdout.String())
		}
		if help := stdout.String(); !strings.Contains(help, "defaults to gate-state.json in the active workflow run") || strings.Contains(help, "defaults to .claude/gates/gate-state.json under --worktree") {
			t.Fatalf("stale state help for %v:\n%s", args, help)
		}
	}
}

func TestRunGateShowRequiresExplicitActiveRunState(t *testing.T) {
	var stdout bytes.Buffer
	code := Run("formal-gates", []string{"gate", "show", "--worktree", t.TempDir()}, IO{Stdout: &stdout})
	if code == 0 {
		t.Fatalf("gate show accepted the removed global default: code=%d stdout=%q", code, stdout.String())
	}
}

func TestRunHandoffValidateRequiresDevelopmentBudget(t *testing.T) {
	dir := t.TempDir()
	handoff := filepath.Join(dir, "handoff.md")
	mustWriteCLI(t, handoff, `Gate Handoff Request
WorkflowId: wf
Change snapshot: snap
Worktree: repo
Requirement document target or OpenSpec change: openspec/changes/example
Verification requirements: go test ./...
Forbidden context: no prior findings
`)
	var stdout bytes.Buffer
	code := Run("formal-gates", []string{
		"handoff", "validate",
		"--root", dir,
		"--file", "handoff.md",
		"--workflow-id", "wf",
		"--change-snapshot", "snap",
	}, IO{Stdout: &stdout})
	if code == 0 {
		t.Fatalf("expected handoff without complexity budget to fail, stdout=%q", stdout.String())
	}

	stdout.Reset()
	code = Run("formal-gates", []string{
		"handoff", "compose", "--root", dir, "--workflow-id", "wf", "--change-snapshot", "snap",
		"--output", "restricted/handoff.md", "--requirement-target", "openspec/changes/example",
		"--verification-requirements", "go test ./...", "--budget-stop-triggers", "stop on growth",
		"--budget-expansion-approval-path", "agents/anti-complexity-review.md", "--forbidden-context", "prior findings",
		"--formal-flow-mode", "none", "--trigger-source", "user", "--task-type", "small-feature", "--max-net", "250",
		"--max-new-prod-files", "0", "--max-prod-insertions", "300",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected handoff compose to pass, code=%d stdout=%q", code, stdout.String())
	}
	stdout.Reset()
	code = Run("formal-gates", []string{
		"handoff", "validate",
		"--root", dir,
		"--file", ".claude/gates/runs/wf/restricted/handoff.md",
		"--workflow-id", "wf",
		"--change-snapshot", "snap",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected valid handoff to pass, code=%d stdout=%q", code, stdout.String())
	}
}

func TestRunPolicyShowJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := Run("formal-gates", []string{"policy", "show", "--format", "json"}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("expected policy show to pass, code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"postDevelopmentGateOrder"`) || !strings.Contains(stdout.String(), `"requirements.pass.v2"`) || strings.Contains(stdout.String(), `"structuredJsonEvidenceRequired"`) {
		t.Fatalf("unexpected policy JSON: %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run("formal-gates", []string{"policy", "show", "--format", "yaml"}, IO{Stdout: &stdout, Stderr: &stderr})
	if code == 0 || !strings.Contains(stderr.String(), "unsupported --format") {
		t.Fatalf("expected unsupported format failure, code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunComplexityJSONRoundTripsToArtifactValidation(t *testing.T) {
	dir := initCLIGitRepo(t)
	runRel := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", "wf"))
	runDir := filepath.Join(dir, filepath.FromSlash(runRel))
	restrictedDir := filepath.Join(runDir, "restricted")
	logical := func(name string) string { return filepath.ToSlash(filepath.Join("restricted", name)) }
	mustWriteCLI(t, filepath.Join(dir, "feature.go"), "package feature\n")
	if err := os.MkdirAll(restrictedDir, 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout bytes.Buffer
	code := Run("formal-gates", []string{
		"complexity", "check", "--task-type", "refactor", "--worktree", dir, "--vcs", "git",
		"--run-dir", runDir, "--workflow-id", "wf", "--change-snapshot", "snap", "--output", "restricted/statistics.json",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("complexity producer failed, code=%d stdout=%q", code, stdout.String())
	}
	produced, err := os.ReadFile(filepath.Join(restrictedDir, "statistics.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(produced), `"failures": []`) || !strings.Contains(string(produced), `"review_required": []`) {
		t.Fatalf("complexity producer must encode empty result slices as arrays: %s", produced)
	}
	var statisticsRef validate.EvidenceRef
	if err := json.Unmarshal(stdout.Bytes(), &statisticsRef); err != nil {
		t.Fatal(err)
	}
	proofsBefore, err := filepath.Glob(filepath.Join(runDir, "restricted", "proofs", "compositions", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if code := Run("formal-gates", []string{
		"complexity", "check", "--task-type", "refactor", "--worktree", dir, "--vcs", "git",
		"--max-net", "10", "--max-new-prod-files", "1", "--max-prod-insertions", "10",
		"--run-dir", runDir, "--workflow-id", "wf", "--change-snapshot", "snap", "--output", "restricted/budget-statistics.json",
	}, IO{Stdout: &stdout}); code == 0 {
		t.Fatal("formal complexity statistics accepted development budget flags")
	}
	if _, err := os.Stat(filepath.Join(restrictedDir, "budget-statistics.json")); !os.IsNotExist(err) {
		t.Fatalf("rejected budget-bearing formal statistics wrote output: %v", err)
	}
	proofsAfter, err := filepath.Glob(filepath.Join(runDir, "restricted", "proofs", "compositions", "*.json"))
	if err != nil || !reflect.DeepEqual(proofsBefore, proofsAfter) {
		t.Fatalf("rejected budget-bearing formal statistics changed proofs: before=%v after=%v err=%v", proofsBefore, proofsAfter, err)
	}
	stdout.Reset()
	if code := Run("formal-gates", []string{
		"complexity", "check", "--task-type", "refactor", "--worktree", dir, "--vcs", "git", "--workflow-id", "wf",
	}, IO{Stdout: &stdout}); code == 0 {
		t.Fatal("partial formal complexity output flags were accepted")
	}
	for name, text := range map[string]string{"input.txt": "input", "changed.txt": "changed", "verification.txt": "verified"} {
		mustWriteCLI(t, filepath.Join(restrictedDir, name), text)
	}
	ref := func(name string) validate.EvidenceRef {
		return validate.EvidenceRef{Path: logical(name), SHA256: cliFileHash(t, filepath.Join(restrictedDir, name))}
	}
	writeCLIJSON(t, filepath.Join(restrictedDir, "bundle.json"), validate.ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []validate.EvidenceRef{ref("input.txt")}})
	var policy validate.ArtifactPolicy
	for _, candidate := range validate.Policy().ArtifactPolicies {
		if candidate.ID == "complexity.post-development.v2" {
			policy = candidate
		}
	}
	checks := make([]validate.ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		check := validate.ReviewCheck{ID: id, Status: "PASS", Message: cliReviewCheckMessage(id), EvidenceRefs: []validate.EvidenceRef{}, Findings: []validate.Finding{}}
		if id == "complexity.statistics" {
			check.EvidenceRefs = []validate.EvidenceRef{statisticsRef}
		}
		checks = append(checks, check)
	}
	changed, verification := ref("changed.txt"), ref("verification.txt")
	payloadData, _ := json.Marshal(validate.ReviewerPayload{ContextBundle: ref("bundle.json"), ReviewPolicyID: policy.ID, Checks: checks, ChangedFiles: &changed, Verification: &verification})
	artifact := filepath.ToSlash(filepath.Join(runRel, "restricted", "review.json"))
	writeCLIJSON(t, filepath.Join(runDir, "restricted", "review.json"), validate.FormalGateEvidence{SchemaVersion: 2, ArtifactRole: policy.ArtifactRole, WorkflowID: "wf", ChangeSnapshot: "snap", Gate: policy.Gate, Verdict: "PASS", Payload: payloadData})

	stdout.Reset()
	code = Run("formal-gates", []string{"artifact", "validate", "--root", dir, "--file", artifact, "--gate", policy.Gate, "--workflow-id", "wf", "--change-snapshot", "snap"}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("artifact consumer rejected public complexity JSON, code=%d stdout=%q", code, stdout.String())
	}
}

func TestRunArtifactValidateInfersCustomRunDirectory(t *testing.T) {
	root := t.TempDir()
	runRel := ".claude/gates/runs/custom-run-name"
	runDir := filepath.Join(root, filepath.FromSlash(runRel))
	restrictedDir := filepath.Join(runDir, "restricted")
	logical := func(name string) string { return filepath.ToSlash(filepath.Join("restricted", name)) }
	for name, text := range map[string]string{"input.txt": "input", "changed.txt": "changed", "verification.txt": "verified"} {
		mustWriteCLI(t, filepath.Join(restrictedDir, name), text)
	}
	ref := func(name string) validate.EvidenceRef {
		return validate.EvidenceRef{Path: logical(name), SHA256: cliFileHash(t, filepath.Join(restrictedDir, name))}
	}
	writeCLIJSON(t, filepath.Join(restrictedDir, "bundle.json"), validate.ContextBundle{BundleVersion: 1, WorkflowID: "W", ChangeSnapshot: "S", Inputs: []validate.EvidenceRef{ref("input.txt")}})
	var policy validate.ArtifactPolicy
	for _, candidate := range validate.Policy().ArtifactPolicies {
		if candidate.ID == "architecture.post-development.v2" {
			policy = candidate
		}
	}
	checks := make([]validate.ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		checks = append(checks, validate.ReviewCheck{ID: id, Status: "PASS", Message: cliReviewCheckMessage(id), EvidenceRefs: []validate.EvidenceRef{}, Findings: []validate.Finding{}})
	}
	changed, verification := ref("changed.txt"), ref("verification.txt")
	payload, err := json.Marshal(validate.ReviewerPayload{ContextBundle: ref("bundle.json"), ReviewPolicyID: policy.ID, Checks: checks, ChangedFiles: &changed, Verification: &verification})
	if err != nil {
		t.Fatal(err)
	}
	artifact := runRel + "/restricted/architecture-review.json"
	writeCLIJSON(t, filepath.Join(root, filepath.FromSlash(artifact)), validate.FormalGateEvidence{SchemaVersion: 2, ArtifactRole: policy.ArtifactRole, WorkflowID: "W", ChangeSnapshot: "S", Gate: policy.Gate, Stage: policy.Stage, Verdict: "PASS", Payload: payload})

	var stdout bytes.Buffer
	code := Run("formal-gates", []string{"artifact", "validate", "--root", root, "--file", artifact, "--gate", policy.Gate, "--workflow-id", "W", "--change-snapshot", "S"}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("custom run directory was not used as the evidence root, code=%d stdout=%q", code, stdout.String())
	}

	copiedRoot := t.TempDir()
	entries, err := os.ReadDir(runDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(runDir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		mustWriteCLI(t, filepath.Join(copiedRoot, entry.Name()), string(data))
	}
	stdout.Reset()
	code = Run("formal-gates", []string{"artifact", "validate", "--root", copiedRoot, "--file", "architecture-review.json", "--gate", policy.Gate, "--workflow-id", "W", "--change-snapshot", "S"}, IO{Stdout: &stdout})
	if code == 0 || !strings.Contains(stdout.String(), "artifact must be under .claude/gates/runs") {
		t.Fatalf("copied run contents validated from repository root, code=%d stdout=%q", code, stdout.String())
	}
}

func TestTopLevelHelpShowsAllCommands(t *testing.T) {
	var stdout, stderr bytes.Buffer

	code := Run("formal-gates", []string{"--help"}, IO{Stdout: &stdout, Stderr: &stderr})

	if code != 0 {
		t.Fatalf("expected top-level help to exit 0, code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "formal-gates workflow snapshot") || !strings.Contains(stdout.String(), "formal-gates behavior evaluate") {
		t.Fatalf("expected global usage, got %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "Usage of package:") {
		t.Fatalf("top-level help must not show package-only help: %q", stdout.String())
	}
}

func TestRunBehaviorEvaluatePendingAndFailingAnswers(t *testing.T) {
	root := t.TempDir()
	mustWriteCLI(t, filepath.Join(root, "cases.json"), `[
		{"id":"FG-BEH-001","must_include":["artifact"],"must_avoid":["self-approved"]},
		{"id":"FG-BEH-002","must_include":["independent"],"must_avoid":[]}
	]`)
	mustWriteCLI(t, filepath.Join(root, "answers.json"), `[{"id":"FG-BEH-001","answer":"self-approved"}]`)
	mustWriteCLI(t, filepath.Join(root, "answers-missing.json"), `[{"id":"FG-BEH-001","answer":"artifact"}]`)
	var stdout, stderr bytes.Buffer

	code := Run("formal-gates", []string{
		"behavior", "evaluate",
		"--root", root,
		"--cases", "cases.json",
	}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("expected pending behavior report to pass, code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), `"pending": 2`) {
		t.Fatalf("unexpected pending report: %q", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run("formal-gates", []string{
		"behavior", "evaluate",
		"--root", root,
		"--cases", "cases.json",
		"--answers", "answers.json",
	}, IO{Stdout: &stdout, Stderr: &stderr})
	if code == 0 {
		t.Fatal("expected failing behavior answer to fail")
	}
	if !strings.Contains(stdout.String(), `"fail": 2`) || !strings.Contains(stderr.String(), "behavior evaluate failed") {
		t.Fatalf("unexpected failing behavior output stdout=%q stderr=%q", stdout.String(), stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	code = Run("formal-gates", []string{
		"behavior", "evaluate",
		"--root", root,
		"--cases", "cases.json",
		"--answers", "answers-missing.json",
	}, IO{Stdout: &stdout, Stderr: &stderr})
	if code == 0 {
		t.Fatal("expected missing behavior answer to fail")
	}
	if !strings.Contains(stdout.String(), `"fail": 1`) || !strings.Contains(stdout.String(), `"pending": 0`) {
		t.Fatalf("unexpected missing-answer behavior output stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRunGateRecordShowAndVerifyAdmission(t *testing.T) {
	dir := t.TempDir()
	runRel := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", "wf"))
	stateRel := filepath.ToSlash(filepath.Join(runRel, "restricted", "gate-state.json"))
	artifact := writeCLIArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
	var stdout bytes.Buffer

	code := Run("formal-gates", []string{
		"gate", "record",
		"--worktree", dir,
		"--gate", "qa-test-gate",
		"--verdict", "PASS",
		"--mode", "formal",
		"--stage", "Execution",
		"--artifact", artifact,
		"--workflow-id", "wf",
		"--change-snapshot", "snap",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected gate record to pass, code=%d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "GATE_STATE_RECORDED gate=qa-test-gate verdict=PASS workflowId=wf") {
		t.Fatalf("unexpected record stdout: %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".claude", "gates", "gate-state.json")); !os.IsNotExist(err) {
		t.Fatalf("gate record wrote repository-level state: %v", err)
	}
	stateData, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(stateRel)))
	if err != nil {
		t.Fatal(err)
	}
	var state validate.GateState
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	if recorded := state.Gates["qa-test-gate"].Artifact; !strings.HasPrefix(recorded, runRel+"/") {
		t.Fatalf("gate record stored artifact outside active run: %q", recorded)
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
	code = Run("formal-gates", []string{"gate", "show", "--worktree", dir, "--state", stateRel, "--format", "text"}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected gate show to pass, code=%d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "gate=qa-test-gate verdict=PASS workflowId=wf changeSnapshot=snap") {
		t.Fatalf("unexpected show stdout: %q", stdout.String())
	}
}

func TestRunGateRejectsOutOfRunArtifactAndState(t *testing.T) {
	dir := t.TempDir()
	artifact := writeCLIArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(artifact)))
	if err != nil {
		t.Fatal(err)
	}
	mustWriteCLI(t, filepath.Join(dir, "qa-test-gate.json"), string(data))
	recordArgs := []string{"gate", "record", "--worktree", dir, "--gate", "qa-test-gate", "--verdict", "PASS", "--mode", "formal", "--stage", "Execution", "--workflow-id", "wf", "--change-snapshot", "snap"}

	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "artifact", args: append(append([]string{}, recordArgs...), "--artifact", "qa-test-gate.json")},
		{name: "record state", args: append(append([]string{}, recordArgs...), "--artifact", artifact, "--state", "gate-state.json")},
		{name: "admission state", args: []string{"gate", "verify-admission", "--worktree", dir, "--state", "gate-state.json", "--gate", "complexity-gate", "--workflow-id", "wf", "--change-snapshot", "snap"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout bytes.Buffer
			if code := Run("formal-gates", test.args, IO{Stdout: &stdout}); code == 0 || !strings.Contains(stdout.String(), "must be under the active run restricted directory") {
				t.Fatalf("out-of-run path was accepted, code=%d stdout=%q", code, stdout.String())
			}
		})
	}
	if _, err := os.Stat(filepath.Join(dir, "gate-state.json")); !os.IsNotExist(err) {
		t.Fatalf("rejected state path was written: %v", err)
	}
}

func TestRunWorkflowSnapshotRecordStageAndAdmission(t *testing.T) {
	dir := t.TempDir()
	mustWriteCLI(t, filepath.Join(dir, "src.txt"), "source\n")
	artifact := writeCLIArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
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
		"--artifact", artifact,
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

func TestRunWorkflowRecordStageAllowsIndependentGateOrder(t *testing.T) {
	dir, workflowID, snapshot := t.TempDir(), "wf", "snap"
	for _, item := range []struct {
		gate  string
		stage string
		mode  string
	}{
		{gate: "code-quality-gate"},
		{gate: "architecture-health-gate"},
		{gate: "complexity-gate"},
		{gate: "qa-test-gate", stage: "Execution", mode: "formal"},
	} {
		artifact := writeCLIArtifact(t, dir, item.gate, item.stage, workflowID, snapshot)
		args := []string{"workflow", "record-stage", "--worktree", dir, "--gate", item.gate, "--verdict", "PASS", "--artifact", artifact, "--workflow-id", workflowID, "--change-snapshot", snapshot}
		if item.stage != "" {
			args = append(args, "--stage", item.stage)
		}
		if item.mode != "" {
			args = append(args, "--mode", item.mode)
		}
		var stdout bytes.Buffer
		if code := Run("formal-gates", args, IO{Stdout: &stdout}); code != 0 {
			t.Fatalf("independent %s record failed: code=%d stdout=%q", item.gate, code, stdout.String())
		}
	}
}

func TestRunWorkflowRecordTransitionUsesOnlyTypedCarryArtifact(t *testing.T) {
	dir, workflowID, source, target := t.TempDir(), "wf", "source", "target"
	runRel := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", workflowID))
	runDir := filepath.Join(dir, filepath.FromSlash(runRel))
	sourceArtifact := writeCLIArtifact(t, dir, "qa-test-gate", "Execution", workflowID, source)
	var stdout bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "record-stage", "--worktree", dir, "--gate", "qa-test-gate", "--stage", "Execution", "--mode", "formal", "--verdict", "PASS", "--artifact", sourceArtifact, "--workflow-id", workflowID, "--change-snapshot", source}, IO{Stdout: &stdout}); code != 0 {
		t.Fatalf("source QA record failed: %s", stdout.String())
	}
	sourceComplexityArtifact := writeCLIArtifact(t, dir, "complexity-gate", "", workflowID, source)
	stdout.Reset()
	if code := Run("formal-gates", []string{"workflow", "record-stage", "--worktree", dir, "--gate", "complexity-gate", "--mode", "formal", "--verdict", "PASS", "--artifact", sourceComplexityArtifact, "--workflow-id", workflowID, "--change-snapshot", source}, IO{Stdout: &stdout}); code != 0 {
		t.Fatalf("source complexity record failed: %s", stdout.String())
	}
	var state validate.GateState
	stateData, _ := os.ReadFile(filepath.Join(runDir, "restricted", "gate-state.json"))
	if err := json.Unmarshal(stateData, &state); err != nil {
		t.Fatal(err)
	}
	ref := func(name string) validate.EvidenceRef {
		return validate.EvidenceRef{Path: name, SHA256: cliFileHash(t, filepath.Join(runDir, filepath.FromSlash(name)))}
	}
	base := "restricted/target"
	for name, text := range map[string]string{base + "/repair.txt": "repair history", base + "/changed.txt": "changed files", base + "/verification.txt": "verified", base + "/repair-evidence.txt": "repair evidence"} {
		mustWriteCLI(t, filepath.Join(runDir, filepath.FromSlash(name)), text)
	}
	writeCLIJSON(t, filepath.Join(runDir, filepath.FromSlash(base+"/context.json")), validate.ContextBundle{BundleVersion: 1, WorkflowID: workflowID, ChangeSnapshot: target, Inputs: []validate.EvidenceRef{ref(base + "/repair.txt")}})
	chain := validate.TransitionChain{SchemaVersion: 2, WorkflowID: workflowID, TargetSnapshot: target, Hops: []validate.TransitionHop{{FromSnapshot: source, ToSnapshot: target, ChangedFiles: ref(base + "/changed.txt"), Verification: ref(base + "/verification.txt"), RepairEvidence: ref(base + "/repair-evidence.txt")}}}
	writeCLIJSON(t, filepath.Join(runDir, filepath.FromSlash(base+"/chain.json")), chain)
	sourceClosure := state.Gates["qa-test-gate"]
	complexityClosure := state.Gates["complexity-gate"]
	decision := func(gate string, closure validate.GateStateEntry) validate.CarryDecision {
		return validate.CarryDecision{Gate: gate, SourceSnapshot: source, SourceGateEvidence: validate.EvidenceRef{Path: strings.TrimPrefix(closure.Artifact, runRel+"/"), SHA256: closure.ArtifactHash}, Decision: "ACCEPT_CARRY", Reason: "The gate remains valid."}
	}
	basePayload := validate.CarryPayload{ContextBundle: ref(base + "/context.json"), ReviewPolicyID: "carry.arbiter.v2", TransitionChain: ref(base + "/chain.json")}
	finalizeCarry := func(name string, payload validate.CarryPayload) string {
		artifact := filepath.ToSlash(filepath.Join(runRel, base, name))
		sources := []string{}
		for _, carryDecision := range payload.Decisions {
			sources = append(sources, carryDecision.SourceGateEvidence.Path)
		}
		options := withCLIReceiptBundle(t, validate.ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: workflowID, ChangeSnapshot: target, Gate: "qa-test-gate", Stage: "Carry", Artifact: artifact, ContextBundle: base + "/context.json", TransitionChain: payload.TransitionChain.Path, CarrySourceClosures: sources})
		args := []string{"receipt", "register", "--worktree", dir, "--provider", "codex", "--context-bundle", options.ContextBundle, "--prompt", options.Prompt, "--transition-chain", options.TransitionChain, "--artifact", artifact, "--gate", "qa-test-gate", "--stage", "Carry", "--workflow-id", workflowID, "--change-snapshot", target}
		for _, closure := range sources {
			args = append(args, "--carry-source-closure", closure)
		}
		var registrationOut bytes.Buffer
		if code := Run("formal-gates", args, IO{Stdout: &registrationOut}); code != 0 {
			t.Fatalf("Carry registration failed: code=%d stdout=%q", code, registrationOut.String())
		}
		var registration validate.ReceiptRegistration
		if err := json.Unmarshal(registrationOut.Bytes(), &registration); err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(map[string]any{"workflowId": workflowID, "changeSnapshot": target, "gate": "qa-test-gate", "stage": "Carry", "subagentId": "carry-agent", "dispatchId": registration.DispatchID, "dispatchRegistrationArtifact": registration.DispatchRegistrationArtifact})
		if _, result := validate.ReceiptCapture(validate.ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: raw}); !result.OK() {
			t.Fatal(result.Failures)
		}
		semantics := make([]validate.ReceiptSemanticCarryDecision, 0, len(payload.Decisions))
		for index, decision := range payload.Decisions {
			semantics = append(semantics, validate.ReceiptSemanticCarryDecision{GatePosition: index + 1, Decision: decision.Decision, Reason: decision.Reason})
		}
		if _, result := validate.ReceiptSubmit(validate.ReceiptSubmitOptions{Worktree: dir, Artifact: artifact, CarryDecisions: semantics}); !result.OK() {
			t.Fatal(result.Failures)
		}
		if _, result := validate.ReceiptCapture(validate.ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: raw}); !result.OK() {
			t.Fatal(result.Failures)
		}
		if _, result := validate.ReceiptFinalize(validate.ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: workflowID, Gate: "qa-test-gate", Stage: "Carry", Artifact: artifact}); !result.OK() {
			t.Fatal(result.Failures)
		}
		return artifact
	}
	incompleteArtifact := finalizeCarry("carry-incomplete.json", validate.CarryPayload{ContextBundle: basePayload.ContextBundle, ReviewPolicyID: basePayload.ReviewPolicyID, TransitionChain: basePayload.TransitionChain, Decisions: []validate.CarryDecision{decision("qa-test-gate", sourceClosure)}})
	stdout.Reset()
	if code := Run("formal-gates", []string{"workflow", "record-transition", "--worktree", dir, "--artifact", incompleteArtifact, "--workflow-id", workflowID, "--change-snapshot", target}, IO{Stdout: &stdout}); code == 0 || !strings.Contains(stdout.String(), "eligible prior PASS gate=complexity-gate") {
		t.Fatalf("incomplete eligible Carry decision set was accepted: code=%d output=%s", code, stdout.String())
	}
	basePayload.Decisions = []validate.CarryDecision{decision("qa-test-gate", sourceClosure), decision("complexity-gate", complexityClosure)}
	artifact := finalizeCarry("carry.json", basePayload)
	stdout.Reset()
	if code := Run("formal-gates", []string{"workflow", "record-transition", "--worktree", dir, "--artifact", artifact, "--workflow-id", workflowID, "--change-snapshot", target}, IO{Stdout: &stdout}); code != 0 || !strings.Contains(stdout.String(), "GATE_WORKFLOW_TRANSITION_RECORDED") {
		t.Fatalf("typed transition CLI failed: code=%d output=%s", code, stdout.String())
	}
	stdout.Reset()
	if code := Run("formal-gates", []string{"workflow", "record-transition", "--help"}, IO{Stdout: &stdout}); code != 0 {
		t.Fatal("record-transition help failed")
	}
	for _, legacy := range []string{"from-snapshot", "to-snapshot", "rerun-from-gate", "reason"} {
		if strings.Contains(stdout.String(), legacy) {
			t.Fatalf("record-transition exposes legacy truth %q: %s", legacy, stdout.String())
		}
	}
}

func TestRunWorkflowNonPassReviewerResultIsNotReportedAsRecorded(t *testing.T) {
	dir := t.TempDir()
	artifact := writeCLIArtifact(t, dir, "complexity-gate", "", "wf", "snap")
	artifactPath := filepath.Join(dir, filepath.FromSlash(artifact))
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	var envelope validate.FormalGateEvidence
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	var payload validate.ReviewerPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	payload.Checks[0].Status = "REVIEW"
	payload.Checks[0].Message = "review result is not a PASS"
	envelope.Verdict = "REVIEW"
	envelope.Payload, err = json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	writeCLIJSON(t, artifactPath, envelope)

	var stdout bytes.Buffer
	code := Run("formal-gates", []string{
		"workflow", "record-stage",
		"--worktree", dir,
		"--gate", "complexity-gate",
		"--verdict", "REVIEW",
		"--mode", "formal",
		"--artifact", artifact,
		"--workflow-id", "wf",
		"--change-snapshot", "snap",
	}, IO{Stdout: &stdout})
	if code != 0 || !strings.Contains(stdout.String(), "GATE_WORKFLOW_NOT_RECORDED gate=complexity-gate verdict=REVIEW") {
		t.Fatalf("non-PASS result was not reported accurately, code=%d stdout=%q", code, stdout.String())
	}
	if strings.Contains(stdout.String(), "GATE_WORKFLOW_RECORDED") {
		t.Fatalf("non-PASS result claimed to be recorded: %q", stdout.String())
	}
	statePath := filepath.Join(dir, ".claude", "gates", "runs", "wf", "restricted", "gate-state.json")
	if _, err := os.Stat(statePath); !os.IsNotExist(err) {
		t.Fatalf("non-PASS result changed authoritative state: %v", err)
	}
}

func TestRunWorkflowRequirementsRejectsIncompatibleModesWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	runRel := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", "wf"))
	artifact := filepath.ToSlash(filepath.Join(runRel, "restricted", "requirements.json"))
	mustWriteCLI(t, filepath.Join(dir, filepath.FromSlash(artifact)), "{}\n")
	stateRel := filepath.ToSlash(filepath.Join(runRel, "restricted", "gate-state.json"))
	statePath := filepath.Join(dir, filepath.FromSlash(stateRel))
	mustWriteCLI(t, statePath, `{"schemaVersion":2,"gates":{},"history":[]}`+"\n")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"formal", "start-readiness"} {
		var stdout bytes.Buffer
		code := Run("formal-gates", []string{
			"workflow", "record-stage",
			"--worktree", dir,
			"--state", stateRel,
			"--gate", "requirements-clarification-gate",
			"--verdict", "PASS",
			"--mode", mode,
			"--artifact", artifact,
			"--workflow-id", "wf",
			"--change-snapshot", "snap",
		}, IO{Stdout: &stdout})
		if code == 0 || !strings.Contains(stdout.String(), "accepts only --mode requirements") {
			t.Fatalf("mode %s was not rejected, code=%d stdout=%q", mode, code, stdout.String())
		}
		after, err := os.ReadFile(statePath)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(before, after) {
			t.Fatalf("mode %s changed authoritative state", mode)
		}
	}
}

func TestRunWorkflowRejectsStateOutsideActiveRun(t *testing.T) {
	dir := t.TempDir()
	runRel := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", "wf"))
	artifact := writeCLIArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
	attemptRel := filepath.ToSlash(filepath.Join(runRel, "restricted", "attempt.json"))
	attemptPath := filepath.Join(dir, filepath.FromSlash(attemptRel))
	mustWriteCLI(t, attemptPath, "{}\n")
	commands := [][]string{
		{"workflow", "record-stage", "--worktree", dir, "--state", "gate-state.json", "--gate", "qa-test-gate", "--verdict", "PASS", "--mode", "formal", "--stage", "Execution", "--artifact", artifact, "--workflow-id", "wf", "--change-snapshot", "snap"},
		{"workflow", "verify-admission", "--worktree", dir, "--state", "gate-state.json", "--gate", "complexity-gate", "--workflow-id", "wf", "--change-snapshot", "snap"},
		{"workflow", "final-verification", "--worktree", dir, "--state", "gate-state.json", "--attempt-artifact", attemptRel, "--output", filepath.ToSlash(filepath.Join(runRel, "restricted", "final-verification.json")), "--record-final-qa", "--final-qa-artifact", filepath.ToSlash(filepath.Join(runRel, "restricted", "final-execution.json")), "--workflow-id", "wf", "--change-snapshot", "snap"},
	}
	for _, args := range commands {
		var stdout bytes.Buffer
		if code := Run("formal-gates", args, IO{Stdout: &stdout}); code == 0 || !strings.Contains(stdout.String(), "state must be under the active run restricted directory") {
			t.Fatalf("out-of-run workflow state was accepted, code=%d stdout=%q args=%v", code, stdout.String(), args[:2])
		}
	}
}

func TestRunArtifactValidateRejectsMismatchedQAEvidence(t *testing.T) {
	for _, field := range []string{"approvedCaseSet", "designReview", "qaOwnedResults", "caseResultBinding"} {
		t.Run(field, func(t *testing.T) {
			dir := t.TempDir()
			artifact := writeCLIArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
			artifactPath := filepath.Join(dir, filepath.FromSlash(artifact))
			data, err := os.ReadFile(artifactPath)
			if err != nil {
				t.Fatal(err)
			}
			var envelope validate.FormalGateEvidence
			if err := json.Unmarshal(data, &envelope); err != nil {
				t.Fatal(err)
			}
			var payload map[string]any
			if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
				t.Fatal(err)
			}
			payload[field] = payload[map[string]string{"approvedCaseSet": "qaOwnedResults", "designReview": "approvedCaseSet", "qaOwnedResults": "caseResultBinding", "caseResultBinding": "approvedCaseSet"}[field]]
			envelope.Payload, _ = json.Marshal(payload)
			writeCLIJSON(t, artifactPath, envelope)
			var stdout bytes.Buffer
			code := Run("formal-gates", []string{"artifact", "validate", "--root", dir, "--file", artifact, "--gate", "qa-test-gate", "--stage", "Execution", "--workflow-id", "wf", "--change-snapshot", "snap"}, IO{Stdout: &stdout})
			if code == 0 {
				t.Fatalf("mismatched %s evidence was accepted: %q", field, stdout.String())
			}
		})
	}
}

func TestRunWorkflowRecordQAExecutionWithRelativeWorktree(t *testing.T) {
	dir := t.TempDir()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Error(err)
		}
	})

	runDir := ".claude/gates/runs/wf"
	runAbs := filepath.Join(dir, filepath.FromSlash(runDir))
	artifact := runDir + "/restricted/qa-execution.json"
	payload := writeCLIQAEvidence(t, runAbs, "wf", "snap")
	payloadData, _ := json.Marshal(payload)
	writeCLIJSON(t, filepath.Join(dir, filepath.FromSlash(artifact)), validate.FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "QA_EXECUTION", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "qa-test-gate", Stage: "Execution", Verdict: "PASS", Payload: payloadData})
	writeCLICompositionProof(t, dir, runAbs, "wf", "snap", "qa-execution.v1", "restricted/qa-execution.json")

	var stdout bytes.Buffer
	code := Run("formal-gates", []string{"workflow", "record-stage", "--worktree", ".", "--run-dir", runDir, "--gate", "qa-test-gate", "--verdict", "PASS", "--mode", "formal", "--stage", "Execution", "--artifact", artifact, "--workflow-id", "wf", "--change-snapshot", "snap"}, IO{Stdout: &stdout})
	if code != 0 || !strings.Contains(stdout.String(), "GATE_WORKFLOW_RECORDED") {
		t.Fatalf("record-stage rejected mechanical QA Execution evidence, code=%d stdout=%q", code, stdout.String())
	}
}

func TestRunWorkflowRejectsQABindingMismatchWithoutMutation(t *testing.T) {
	dir := t.TempDir()
	runRel := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", "wf"))
	runDir := filepath.Join(dir, filepath.FromSlash(runRel))
	artifact := writeCLIArtifact(t, dir, "qa-test-gate", "Execution", "wf", "snap")
	stateRel := filepath.ToSlash(filepath.Join(runRel, "restricted", "gate-state.json"))
	statePath := filepath.Join(dir, filepath.FromSlash(stateRel))
	mustWriteCLI(t, statePath, `{"schemaVersion":2,"gates":{},"history":[]}`+"\n")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	bindingPath := filepath.Join(runDir, "restricted", "qa-case-binding.json")
	data, err := os.ReadFile(bindingPath)
	if err != nil {
		t.Fatal(err)
	}
	var binding map[string]any
	if err := json.Unmarshal(data, &binding); err != nil {
		t.Fatal(err)
	}
	binding["bindings"].([]any)[0].(map[string]any)["oracle"] = "changed oracle"
	writeCLIJSON(t, bindingPath, binding)

	artifactPath := filepath.Join(dir, filepath.FromSlash(artifact))
	data, err = os.ReadFile(artifactPath)
	if err != nil {
		t.Fatal(err)
	}
	var envelope validate.FormalGateEvidence
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	var payload validate.QAExecutionPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	payload.CaseResultBinding = validate.EvidenceRef{Path: "restricted/qa-case-binding.json", SHA256: cliFileHash(t, bindingPath)}
	envelope.Payload, _ = json.Marshal(payload)
	writeCLIJSON(t, artifactPath, envelope)

	var stdout bytes.Buffer
	code := Run("formal-gates", []string{"workflow", "record-stage", "--worktree", dir, "--state", stateRel, "--gate", "qa-test-gate", "--verdict", "PASS", "--mode", "formal", "--stage", "Execution", "--artifact", artifact, "--workflow-id", "wf", "--change-snapshot", "snap"}, IO{Stdout: &stdout})
	if code == 0 || !strings.Contains(stdout.String(), "binding") {
		t.Fatalf("mismatched oracle was not rejected, code=%d stdout=%q", code, stdout.String())
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("rejected QA evidence changed authoritative state\nbefore=%s\nafter=%s", before, after)
	}
}

func TestRunWorkflowFinalVerification(t *testing.T) {
	dir := t.TempDir()
	runRel := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", "wf"))
	attemptRel := filepath.ToSlash(filepath.Join(runRel, "restricted", "attempt.json"))
	attemptPath := filepath.Join(dir, filepath.FromSlash(attemptRel))
	mustWriteCLI(t, attemptPath, `{"ok":true}`+"\n")
	output := filepath.ToSlash(filepath.Join(runRel, "restricted", "final-verification.json"))
	var stdout bytes.Buffer

	code := Run("formal-gates", []string{
		"workflow", "final-verification",
		"--worktree", dir,
		"--attempt-artifact", attemptRel,
		"--output", output,
		"--workflow-id", "wf",
		"--change-snapshot", "snap",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected final-verification to pass, code=%d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), "GATE_WORKFLOW_FINAL_VERIFICATION status=PASS accepted=1 attempts=1") {
		t.Fatalf("unexpected final-verification stdout: %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(output))); err != nil {
		t.Fatal(err)
	}
}

func TestRunWorkflowFinalVerificationRejectsFailedCommandOutput(t *testing.T) {
	dir := t.TempDir()
	runRel := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", "wf"))
	attemptRel := filepath.ToSlash(filepath.Join(runRel, "restricted", "failed-command.txt"))
	attemptPath := filepath.Join(dir, filepath.FromSlash(attemptRel))
	mustWriteCLI(t, attemptPath, "FAIL: command exited with status 1\n")
	output := filepath.ToSlash(filepath.Join(runRel, "restricted", "final-verification.json"))
	var stdout bytes.Buffer

	code := Run("formal-gates", []string{
		"workflow", "final-verification",
		"--worktree", dir,
		"--attempt-artifact", attemptRel,
		"--output", output,
		"--workflow-id", "wf",
		"--change-snapshot", "snap",
	}, IO{Stdout: &stdout})
	if code == 0 {
		t.Fatalf("expected failed command output to be rejected, stdout=%q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "contains a FAIL result") {
		t.Fatalf("unexpected final-verification failure stdout: %q", stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(output)))
	if err != nil {
		t.Fatal(err)
	}
	var artifact validate.WorkflowFinalVerificationArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	if artifact.Status != "FAIL" || len(artifact.AcceptedAttempts) != 0 {
		t.Fatalf("failed command output generated a passing aggregate: %#v", artifact)
	}
}

func TestRunRejectsCallerAuthoredFinalExecutionAtGenericRecordEntrypoints(t *testing.T) {
	dir := t.TempDir()
	runRel := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", "wf"))
	attemptRel := filepath.ToSlash(filepath.Join(runRel, "restricted", "attempt.json"))
	attemptPath := filepath.Join(dir, filepath.FromSlash(attemptRel))
	mustWriteCLI(t, attemptPath, `{"ok":true}`+"\n")
	recordCLIFourGatePrerequisites(t, dir, "wf", "snap")
	stateRel := filepath.ToSlash(filepath.Join(runRel, "restricted", "gate-state.json"))
	statePath := filepath.Join(dir, filepath.FromSlash(stateRel))
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	code := Run("formal-gates", []string{
		"workflow", "final-verification",
		"--worktree", dir,
		"--run-dir", runRel,
		"--attempt-artifact", attemptRel,
		"--output", filepath.ToSlash(filepath.Join(runRel, "restricted", "final-verification.json")),
		"--record-final-qa",
		"--final-qa-artifact", filepath.ToSlash(filepath.Join(runRel, "restricted", "generated-final.json")),
		"--workflow-id", "wf",
		"--change-snapshot", "snap",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("mechanical FinalExecution generation failed, code=%d stdout=%q", code, stdout.String())
	}
	generated, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(runRel), "restricted", "generated-final.json"))
	if err != nil {
		t.Fatal(err)
	}
	callerFinal := filepath.ToSlash(filepath.Join(runRel, "restricted", "caller-final.json"))
	mustWriteCLI(t, filepath.Join(dir, filepath.FromSlash(callerFinal)), string(generated)+" \n")

	for _, entrypoint := range []struct {
		name string
		args []string
	}{
		{name: "gate-record", args: []string{"gate", "record"}},
		{name: "workflow-record-stage", args: []string{"workflow", "record-stage", "--run-dir", runRel}},
	} {
		t.Run(entrypoint.name, func(t *testing.T) {
			if err := os.WriteFile(statePath, stateBefore, 0o600); err != nil {
				t.Fatal(err)
			}
			stdout.Reset()
			args := append([]string{}, entrypoint.args...)
			args = append(args,
				"--worktree", dir,
				"--state", stateRel,
				"--gate", "qa-test-gate",
				"--verdict", "PASS",
				"--mode", "formal",
				"--stage", "FinalExecution",
				"--artifact", callerFinal,
				"--workflow-id", "wf",
				"--change-snapshot", "snap",
			)
			code := Run("formal-gates", args, IO{Stdout: &stdout})
			if code == 0 || !strings.Contains(stdout.String(), "FinalExecution can only be recorded by workflow final-verification --record-final-qa") {
				t.Fatalf("caller-authored FinalExecution was not rejected, code=%d stdout=%q", code, stdout.String())
			}
			stateAfter, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stateBefore, stateAfter) {
				t.Fatalf("rejection mutated gate state\nbefore=%s\nafter=%s", stateBefore, stateAfter)
			}
		})
	}
}

func TestRunWorkflowFinalVerificationRecordsFinalQA(t *testing.T) {
	dir := t.TempDir()
	runRel := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", "wf"))
	attemptRel := filepath.ToSlash(filepath.Join(runRel, "restricted", "attempt.json"))
	attemptPath := filepath.Join(dir, filepath.FromSlash(attemptRel))
	mustWriteCLI(t, attemptPath, `{"ok":true}`+"\n")
	recordCLIFourGatePrerequisites(t, dir, "wf", "snap")
	stateRel := filepath.ToSlash(filepath.Join(runRel, "restricted", "gate-state.json"))
	statePath := filepath.Join(dir, filepath.FromSlash(stateRel))
	closuresBefore, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(runRel), "closures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	finalExecutionRel := filepath.ToSlash(filepath.Join(runRel, "restricted", "final-execution.md"))
	finalExecutionPath := filepath.Join(dir, filepath.FromSlash(finalExecutionRel))
	var stdout bytes.Buffer
	args := []string{
		"workflow", "final-verification",
		"--worktree", dir,
		"--run-dir", runRel,
		"--attempt-artifact", attemptRel,
		"--output", filepath.ToSlash(filepath.Join(runRel, "restricted", "final-verification.json")),
		"--record-final-qa",
		"--final-qa-artifact", finalExecutionRel,
		"--actor", "gate-workflow",
		"--workflow-id", "wf",
		"--change-snapshot", "snap",
	}
	code := Run("formal-gates", args, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected final-verification final QA record to pass, code=%d stdout=%q", code, stdout.String())
	}
	first, err := os.ReadFile(finalExecutionPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(first), "caller-authored") {
		t.Fatalf("final-verification imported caller-authored FinalExecution bytes: %s", first)
	}
	var envelope validate.FormalGateEvidence
	if err := json.Unmarshal(first, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.ArtifactRole != "FINAL_EXECUTION" || envelope.Gate != "qa-test-gate" || envelope.Stage != "FinalExecution" || envelope.Verdict != "PASS" {
		t.Fatalf("unexpected generated FinalExecution envelope: %#v", envelope)
	}
	beforeRerun, err := os.ReadFile(finalExecutionPath)
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	rerunArgs := append([]string{}, args...)
	for index := range rerunArgs {
		if strings.HasSuffix(rerunArgs[index], "/final-verification.json") {
			rerunArgs[index] = strings.TrimSuffix(rerunArgs[index], "final-verification.json") + "final-verification-rerun.json"
			break
		}
	}
	if code := Run("formal-gates", rerunArgs, IO{Stdout: &stdout}); code == 0 || !strings.Contains(stdout.String(), "final QA artifact already exists") {
		t.Fatalf("final-verification rerun overwrote FinalExecution: code=%d stdout=%q", code, stdout.String())
	}
	afterRerun, err := os.ReadFile(finalExecutionPath)
	if err != nil || !bytes.Equal(beforeRerun, afterRerun) {
		t.Fatalf("rejected final-verification rerun changed FinalExecution: err=%v", err)
	}
	var payload validate.FinalExecutionPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	wantGates := []string{"qa-test-gate", "complexity-gate", "architecture-health-gate", "code-quality-gate"}
	if len(payload.GateMatrix) != len(wantGates) {
		t.Fatalf("expected four generated gate rows, got %#v", payload.GateMatrix)
	}
	for i, want := range wantGates {
		row := payload.GateMatrix[i]
		if row.Gate != want || row.ResultKind != "FRESH_PASS" || row.SourceSnapshot != "snap" || row.TargetSnapshot != "snap" || row.CarryDecision != nil || row.GateEvidence.Path == "" || row.GateEvidence.SHA256 == "" {
			t.Fatalf("unexpected generated gate row %d: %#v", i, payload.GateMatrix[i])
		}
	}
	if payload.FinalVerification.Path != "restricted/final-verification.json" || payload.FinalVerification.SHA256 == "" {
		t.Fatalf("unexpected final-verification binding: %#v", payload.FinalVerification)
	}
	verificationPath := filepath.Join(dir, filepath.FromSlash(runRel), "restricted", "final-verification.json")
	verificationBefore, err := os.ReadFile(verificationPath)
	if err != nil {
		t.Fatal(err)
	}
	stateAfterFirst, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	closurePathsAfterFirst, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(runRel), "closures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	closureBytesAfterFirst := make(map[string][]byte, len(closurePathsAfterFirst))
	for _, path := range closurePathsAfterFirst {
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		closureBytesAfterFirst[path] = data
	}
	stdout.Reset()
	if code = Run("formal-gates", args, IO{Stdout: &stdout}); code == 0 || !strings.Contains(stdout.String(), "generated final verification output already exists") {
		t.Fatalf("expected repeated final-verification to reject existing output, code=%d stdout=%q", code, stdout.String())
	}
	second, err := os.ReadFile(finalExecutionPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("rejected repeat changed FinalExecution\nfirst=%s\nsecond=%s", first, second)
	}
	verificationAfter, err := os.ReadFile(verificationPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(verificationBefore, verificationAfter) {
		t.Fatalf("rejected repeat changed final verification\nbefore=%s\nafter=%s", verificationBefore, verificationAfter)
	}
	stateAfterRepeat, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(stateAfterFirst, stateAfterRepeat) {
		t.Fatalf("rejected repeat changed gate state\nbefore=%s\nafter=%s", stateAfterFirst, stateAfterRepeat)
	}
	closuresAfter, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(runRel), "closures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(closuresAfter) != len(closuresBefore) {
		t.Fatalf("FinalExecution created a fifth closure: before=%v after=%v", closuresBefore, closuresAfter)
	}
	if len(closuresAfter) != len(closurePathsAfterFirst) {
		t.Fatalf("rejected repeat changed closure set: first=%v after=%v", closurePathsAfterFirst, closuresAfter)
	}
	for _, path := range closuresAfter {
		after, readErr := os.ReadFile(path)
		if readErr != nil {
			t.Fatal(readErr)
		}
		before, ok := closureBytesAfterFirst[path]
		if !ok || !bytes.Equal(before, after) {
			t.Fatalf("rejected repeat changed closure %s\nbefore=%s\nafter=%s", path, before, after)
		}
	}
	state, result := validate.GateShow(validate.GateShowOptions{Worktree: dir, StatePath: stateRel})
	if !result.OK() {
		t.Fatalf("expected gate state to show, got %#v", result.Failures)
	}
	entry := state.Gates["qa-test-gate"]
	if entry.Stage != "FinalExecution" || entry.Mode != "finalization" || entry.Actor != "gate-workflow" || entry.ArtifactHash != cliFileHash(t, finalExecutionPath) {
		t.Fatalf("unexpected final QA entry: %#v", entry)
	}

	stdout.Reset()
	code = Run("formal-gates", []string{
		"workflow", "verify-admission",
		"--worktree", dir,
		"--gate", "complexity-gate",
		"--workflow-id", "wf",
		"--change-snapshot", "snap",
	}, IO{Stdout: &stdout})
	if code != 0 || !strings.Contains(stdout.String(), "GATE_WORKFLOW_ADMISSION_PASS gate=complexity-gate") {
		t.Fatalf("independent complexity admission was blocked after finalization, code=%d stdout=%q", code, stdout.String())
	}
}

func TestRunWorkflowFinalVerificationSealsRunLocalClosuresWithoutMirrors(t *testing.T) {
	dir := t.TempDir()
	runRel := ".claude/gates/runs/W"
	runAbs := filepath.Join(dir, filepath.FromSlash(runRel))
	recordCLIFourGatePrerequisites(t, dir, "W", "S")
	attemptPath := filepath.Join(runAbs, "restricted", "attempt.json")
	mustWriteCLI(t, attemptPath, `{"ok":true}`+"\n")

	var stdout bytes.Buffer
	code := Run("formal-gates", []string{
		"workflow", "final-verification",
		"--worktree", dir,
		"--run-dir", runRel,
		"--state", runRel + "/restricted/gate-state.json",
		"--attempt-artifact", runRel + "/restricted/attempt.json",
		"--output", runRel + "/restricted/final-verification.json",
		"--record-final-qa",
		"--final-qa-artifact", runRel + "/restricted/final-execution.json",
		"--workflow-id", "W",
		"--change-snapshot", "S",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("run-local FinalExecution failed, code=%d stdout=%q", code, stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(runAbs, "restricted", "final-execution.json"))
	if err != nil {
		t.Fatal(err)
	}
	var envelope validate.FormalGateEvidence
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	var payload validate.FinalExecutionPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	for _, row := range payload.GateMatrix {
		if !strings.HasPrefix(row.GateEvidence.Path, "restricted/closures/") || strings.Contains(row.GateEvidence.Path, runRel) {
			t.Fatalf("gate evidence is not run-local: %#v", row.GateEvidence)
		}
	}
	if payload.FinalVerification.Path != "restricted/final-verification.json" {
		t.Fatalf("final verification is not run-local: %#v", payload.FinalVerification)
	}
	if _, err := os.Stat(filepath.Join(dir, "closures")); !os.IsNotExist(err) {
		t.Fatalf("closeout relied on a worktree-root closure mirror, err=%v", err)
	}
}

func TestRunWorkflowFinalVerificationBlocksMissingArtifact(t *testing.T) {
	dir := t.TempDir()
	runRel := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", "wf"))
	missing := filepath.ToSlash(filepath.Join(runRel, "restricted", "missing.json"))
	output := filepath.ToSlash(filepath.Join(runRel, "restricted", "final-verification.json"))
	var stdout bytes.Buffer

	code := Run("formal-gates", []string{
		"workflow", "final-verification",
		"--worktree", dir,
		"--attempt-artifact", missing,
		"--output", output,
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

func TestRunWorkflowFinalVerificationGeneratesAcceptedAttemptHash(t *testing.T) {
	dir := t.TempDir()
	runRel := ".claude/gates/runs/W"
	runAbs := filepath.Join(dir, filepath.FromSlash(runRel))
	attemptRel := runRel + "/restricted/attempt.json"
	attemptPath := filepath.Join(runAbs, "restricted", "attempt.json")
	mustWriteCLI(t, attemptPath, `{"ok":true}`+"\n")
	var stdout bytes.Buffer
	code := Run("formal-gates", []string{
		"workflow", "final-verification", "--worktree", dir, "--run-dir", runRel,
		"--attempt-artifact", attemptRel, "--output", runRel + "/restricted/final-verification.json",
		"--workflow-id", "W", "--change-snapshot", "S",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("final verification failed, code=%d stdout=%q", code, stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(runAbs, "restricted", "final-verification.json"))
	if err != nil {
		t.Fatal(err)
	}
	var artifact validate.WorkflowFinalVerificationArtifact
	if err := json.Unmarshal(data, &artifact); err != nil {
		t.Fatal(err)
	}
	if len(artifact.Attempts) != 1 || artifact.Attempts[0].Artifact != attemptRel || artifact.Attempts[0].ArtifactHash != cliFileHash(t, attemptPath) || !artifact.Attempts[0].Accepted || artifact.Attempts[0].Status != "PASS" {
		t.Fatalf("attempt binding was not script-generated: %#v", artifact.Attempts)
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

func TestRunWorkflowCompactExecuteRemovesRunGarbage(t *testing.T) {
	dir := t.TempDir()
	runDir := ".claude/gates/runs/wf"
	runAbs := filepath.Join(dir, filepath.FromSlash(runDir))
	artifact := filepath.Join(runAbs, "qa-test-gate.md")
	mustWriteCLI(t, artifact, "{}\n")
	writeCLIJSON(t, filepath.Join(runAbs, "restricted", "gate-state.json"), validate.GateState{SchemaVersion: 2, Gates: map[string]validate.GateStateEntry{}, History: []validate.GateStateEntry{}})
	garbage := filepath.Join(runAbs, "tmp", "garbage.txt")
	mustWriteCLI(t, garbage, "garbage\n")

	var stdout bytes.Buffer
	code := Run("formal-gates", []string{
		"workflow", "compact",
		"--worktree", dir,
		"--run-dir", runDir,
		"--workflow-id", "wf",
		"--change-snapshot", "snap",
		"--execute",
	}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("expected compact execute to pass, code=%d stdout=%q", code, stdout.String())
	}
	if !strings.Contains(stdout.String(), `"dryRun": false`) || !strings.Contains(stdout.String(), `"status": "removed"`) {
		t.Fatalf("unexpected compact stdout: %q", stdout.String())
	}
	if _, err := os.Stat(filepath.Join(runAbs, "restricted", "formal-gates-workflow-archive.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(artifact); !os.IsNotExist(err) {
		t.Fatalf("expected source artifact removed, err=%v", err)
	}
	if _, err := os.Stat(garbage); !os.IsNotExist(err) {
		t.Fatalf("expected run garbage removed, err=%v", err)
	}
}

func TestRunReceiptCaptureAndPreflight(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", "wf", "restricted", "review.json"))
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, filepath.FromSlash(artifact))), 0o700); err != nil {
		t.Fatal(err)
	}
	dispatch, result := validate.ReceiptRegisterDispatch(withCLIReceiptBundle(t, validate.ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact}))
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	payload := `{"workflowId":"wf","gate":"complexity-gate","subagentId":"subagent-1","dispatchId":"` + dispatch.DispatchID + `","dispatchRegistrationArtifact":"` + dispatch.DispatchRegistrationArtifact + `"}`
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
	if !strings.Contains(stdout.String(), `"status": "HOST_AUTO_CAPTURE_UNPROVEN"`) {
		t.Fatalf("unexpected preflight stdout: %q", stdout.String())
	}
}

func TestRunReceiptRegisterRequiresSnapshotAndAbsentOutput(t *testing.T) {
	dir := t.TempDir()
	artifact := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", "wf", "restricted", "review.json"))
	if err := os.MkdirAll(filepath.Dir(filepath.Join(dir, filepath.FromSlash(artifact))), 0o700); err != nil {
		t.Fatal(err)
	}
	fixture := withCLIReceiptBundle(t, validate.ReceiptRegisterOptions{Worktree: dir, WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact})
	args := []string{"receipt", "register", "--worktree", dir, "--provider", "codex", "--context-bundle", fixture.ContextBundle, "--prompt", fixture.Prompt, "--artifact", artifact, "--gate", "complexity-gate", "--workflow-id", "wf", "--change-snapshot", "snap"}
	var stdout bytes.Buffer
	if code := Run("formal-gates", args, IO{Stdout: &stdout}); code != 0 {
		t.Fatalf("expected absent output registration to pass, code=%d stdout=%q", code, stdout.String())
	}
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(artifact))); err != nil {
		t.Fatalf("registration did not create reviewer template: %v", err)
	}
	stdout.Reset()
	if code := Run("formal-gates", args, IO{Stdout: &stdout}); code == 0 || !strings.Contains(stdout.String(), "already exists") {
		t.Fatalf("expected duplicate reservation rejection, code=%d stdout=%q", code, stdout.String())
	}
	stdout.Reset()
	args = args[:len(args)-2]
	if code := Run("formal-gates", args, IO{Stdout: &stdout}); code == 0 || !strings.Contains(stdout.String(), "--change-snapshot") {
		t.Fatalf("expected missing snapshot rejection, code=%d stdout=%q", code, stdout.String())
	}
}

func TestRunReceiptRegisterRejectsRemovedBindingDSLFlags(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "check evidence", args: []string{"--check-evidence", "complexity.statistics=restricted/statistics.json"}},
		{name: "Carry source", args: []string{"--carry-source", "qa-test-gate=restricted/closure.json"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			var stdout, stderr bytes.Buffer
			code := Run("formal-gates", append([]string{"receipt", "register", "--worktree", root}, test.args...), IO{Stdout: &stdout, Stderr: &stderr})
			if code == 0 || !strings.Contains(stderr.String(), "flag provided but not defined") {
				t.Fatalf("removed binding syntax was not explicitly rejected: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
			}
			if _, err := os.Lstat(filepath.Join(root, ".claude")); !os.IsNotExist(err) {
				t.Fatalf("rejected legacy registration wrote workflow state: %v", err)
			}
		})
	}
}

func TestRunReceiptRegisterRejectsMissingRoleEvidenceBeforeDispatch(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, ".claude", "gates", "runs", "wf")
	mustWriteCLI(t, filepath.Join(runDir, "restricted", "changed.txt"), "changed\n")
	mustWriteCLI(t, filepath.Join(runDir, "restricted", "verification.txt"), "verified\n")
	artifact := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", "wf", "restricted", "review.json"))
	fixture := withCLIReceiptBundle(t, validate.ReceiptRegisterOptions{Worktree: root, WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact, ChangedFiles: "restricted/changed.txt", Verification: "restricted/verification.txt"})
	args := []string{
		"receipt", "register", "--worktree", root, "--provider", "codex",
		"--context-bundle", fixture.ContextBundle, "--prompt", fixture.Prompt,
		"--changed-files", fixture.ChangedFiles, "--verification", fixture.Verification,
		"--artifact", artifact, "--gate", "complexity-gate", "--workflow-id", "wf", "--change-snapshot", "snap",
	}
	var stdout bytes.Buffer
	if code := Run("formal-gates", args, IO{Stdout: &stdout}); code == 0 || !strings.Contains(stdout.String(), "--complexity-statistics is required") {
		t.Fatalf("missing role evidence was not rejected: code=%d stdout=%q", code, stdout.String())
	}
	if _, err := os.Lstat(filepath.Join(root, filepath.FromSlash(artifact))); !os.IsNotExist(err) {
		t.Fatalf("rejected registration wrote reviewer artifact: %v", err)
	}
	dispatches, err := filepath.Glob(filepath.Join(runDir, "restricted", "proofs", "dispatch", "*.json"))
	if err != nil || len(dispatches) != 0 {
		t.Fatalf("rejected registration wrote dispatch proof: paths=%v err=%v", dispatches, err)
	}
}

func TestRunReceiptSubmitCreatesFinalizableReviewAndRejectsBadTypeWithoutWriting(t *testing.T) {
	dir := initCLIGitRepo(t)
	artifact := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", "wf", "restricted", "submitted-review.json"))
	runDir := filepath.Join(dir, ".claude", "gates", "runs", "wf")
	if err := os.MkdirAll(filepath.Join(runDir, "restricted"), 0o700); err != nil {
		t.Fatal(err)
	}
	var statisticsOut bytes.Buffer
	if code := Run("formal-gates", []string{
		"complexity", "check", "--task-type", "refactor", "--worktree", dir, "--vcs", "git",
		"--run-dir", runDir, "--workflow-id", "wf", "--change-snapshot", "snap", "--output", "restricted/receipt-statistics.json",
	}, IO{Stdout: &statisticsOut}); code != 0 {
		t.Fatalf("formal complexity statistics generation failed: code=%d stdout=%q", code, statisticsOut.String())
	}
	var statisticsRef validate.EvidenceRef
	if err := json.Unmarshal(statisticsOut.Bytes(), &statisticsRef); err != nil {
		t.Fatal(err)
	}
	mustWriteCLI(t, filepath.Join(runDir, "restricted", "changed.txt"), "changed\n")
	mustWriteCLI(t, filepath.Join(runDir, "restricted", "verification.txt"), "verified\n")
	fixture := withCLIReceiptBundle(t, validate.ReceiptRegisterOptions{Worktree: dir, WorkflowID: "wf", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: artifact, ChangedFiles: "restricted/changed.txt", Verification: "restricted/verification.txt", ComplexityStatistics: statisticsRef.Path})
	registerArgs := []string{
		"receipt", "register", "--worktree", dir, "--provider", "codex",
		"--context-bundle", fixture.ContextBundle, "--prompt", fixture.Prompt,
		"--changed-files", fixture.ChangedFiles, "--verification", fixture.Verification,
		"--complexity-statistics", fixture.ComplexityStatistics,
		"--artifact", artifact, "--gate", "complexity-gate", "--workflow-id", "wf", "--change-snapshot", "snap",
	}
	var stdout bytes.Buffer
	if code := Run("formal-gates", registerArgs, IO{Stdout: &stdout}); code != 0 {
		t.Fatalf("receipt register failed: code=%d stdout=%q", code, stdout.String())
	}
	var registration validate.ReceiptRegistration
	if err := json.Unmarshal(stdout.Bytes(), &registration); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, filepath.FromSlash(artifact))
	template, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var envelope validate.FormalGateEvidence
	if err := json.Unmarshal(template, &envelope); err != nil {
		t.Fatal(err)
	}
	var payload validate.ReviewerPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	submitArgs := []string{"receipt", "submit", "--worktree", dir, "--artifact", artifact}
	for index := range payload.Checks {
		submitArgs = append(submitArgs, "--check", strconv.Itoa(index+1), "--status", "PASS", "--message", "Semantic check completed.")
	}
	submitArgs = append(submitArgs,
		"--finding-check", "1", "--finding-message", "Semantic note with two source locations.",
		"--location-finding", "1", "--location-path", "internal/cli/cli.go", "--location-start", "1", "--location-end", "2",
		"--location-finding", "1", "--location-path", "internal/validate/receipt.go", "--location-start", "3", "--location-end", "4",
	)
	beforeMixedArtifact, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dispatchPath := filepath.Join(dir, filepath.FromSlash(registration.DispatchRegistrationArtifact))
	beforeMixedDispatch, err := os.ReadFile(dispatchPath)
	if err != nil {
		t.Fatal(err)
	}
	mixedSubmitArgs := append(append([]string{}, submitArgs...),
		"--design-case", "1",
		"--case-value", "claim", "--case-value", "source", "--case-value", "action",
		"--case-value", "oracle", "--case-value", "failure signal", "--case-value", "evidence", "--case-value", "gap",
	)
	stdout.Reset()
	if code := Run("formal-gates", mixedSubmitArgs, IO{Stdout: &stdout}); code == 0 {
		t.Fatal("public receipt submit accepted reviewer and QA Design semantics together")
	}
	afterMixedArtifact, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	afterMixedDispatch, err := os.ReadFile(dispatchPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(beforeMixedArtifact, afterMixedArtifact) || !bytes.Equal(beforeMixedDispatch, afterMixedDispatch) {
		t.Fatal("rejected public cross-role submission changed artifact or dispatch")
	}
	stdout.Reset()
	if code := Run("formal-gates", submitArgs, IO{Stdout: &stdout}); code != 0 || !strings.Contains(stdout.String(), `"status": "submitted"`) {
		t.Fatalf("receipt submit failed: code=%d stdout=%q", code, stdout.String())
	}
	lifecycle := []byte(`{"workflowId":"wf","changeSnapshot":"snap","gate":"complexity-gate","subagentId":"reviewer","dispatchId":"` + registration.DispatchID + `","dispatchRegistrationArtifact":"` + registration.DispatchRegistrationArtifact + `"}`)
	if _, result := validate.ReceiptCapture(validate.ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: lifecycle}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result := validate.ReceiptCapture(validate.ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: lifecycle}); !result.OK() {
		t.Fatal(result.Failures)
	}
	stdout.Reset()
	if code := Run("formal-gates", []string{"receipt", "finalize", "--worktree", dir, "--provider", "codex", "--artifact", artifact, "--gate", "complexity-gate", "--workflow-id", "wf"}, IO{Stdout: &stdout}); code != 0 || !strings.Contains(stdout.String(), "receiptArtifact") {
		t.Fatalf("submitted review was not finalizable: code=%d stdout=%q", code, stdout.String())
	}
	var finalizedReceipt validate.ReceiptFinalizeOutput
	if err := json.Unmarshal(stdout.Bytes(), &finalizedReceipt); err != nil {
		t.Fatal(err)
	}
	finalized, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(finalized, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Verdict != "PASS" {
		t.Fatalf("finalize did not derive PASS from submitted semantics: %q", envelope.Verdict)
	}
	stdout.Reset()
	if code := Run("formal-gates", []string{
		"receipt", "validate", "--worktree", dir, "--receipt", finalizedReceipt.ReceiptArtifact,
		"--artifact", artifact, "--gate", "complexity-gate", "--workflow-id", "wf", "--change-snapshot", "snap",
	}, IO{Stdout: &stdout}); code != 0 {
		t.Fatalf("public finalized complexity receipt did not validate: code=%d stdout=%q", code, stdout.String())
	}
	stdout.Reset()
	if code := Run("formal-gates", []string{
		"artifact", "validate", "--root", dir, "--file", artifact,
		"--gate", "complexity-gate", "--workflow-id", "wf", "--change-snapshot", "snap",
	}, IO{Stdout: &stdout}); code != 0 {
		t.Fatalf("public finalized complexity artifact did not validate: code=%d stdout=%q", code, stdout.String())
	}

	invalidArtifact := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", "wf2", "restricted", "invalid-type.json"))
	invalidFixture := withCLIReceiptBundle(t, validate.ReceiptRegisterOptions{Worktree: dir, WorkflowID: "wf2", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: invalidArtifact})
	if _, result := validate.ReceiptRegisterDispatch(validate.ReceiptRegisterOptions{
		Worktree: dir, Provider: "codex", WorkflowID: "wf2", ChangeSnapshot: "snap", Gate: "complexity-gate", Artifact: invalidArtifact,
		ContextBundle: invalidFixture.ContextBundle, Prompt: invalidFixture.Prompt,
	}); !result.OK() {
		t.Fatal(result.Failures)
	}
	invalidPath := filepath.Join(dir, filepath.FromSlash(invalidArtifact))
	before, err := os.ReadFile(invalidPath)
	if err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if code := Run("formal-gates", []string{
		"receipt", "submit", "--worktree", dir, "--artifact", invalidArtifact,
		"--location-finding", "1", "--location-path", "README.md", "--location-start", "not-a-line", "--location-end", "1",
	}, IO{Stdout: &stdout}); code == 0 {
		t.Fatal("receipt submit accepted a non-integer location line")
	}
	after, err := os.ReadFile(invalidPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("CLI parse failure changed the assigned artifact")
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
	if !strings.Contains(stdout.String(), `"permissionDecision":"deny"`) {
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

func initCLIGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test User"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, output)
		}
	}
	mustWriteCLI(t, filepath.Join(dir, "README.md"), "initial\n")
	for _, args := range [][]string{{"add", "README.md"}, {"commit", "-m", "initial"}} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, output)
		}
	}
	return dir
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

func writeCLIComplexityStatisticsFixture(t *testing.T, root, runDir, workflowID, snapshot, output string) validate.EvidenceRef {
	t.Helper()
	ref, _, result := validate.ComplexityStatistics(validate.ComplexityStatisticsOptions{
		ComplexityOptions: validate.ComplexityOptions{Worktree: root, VCS: "none", TaskType: "refactor"},
		RunDir:            runDir, WorkflowID: workflowID, ChangeSnapshot: snapshot, Output: output,
	})
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	return ref
}

func writeCLIArtifact(t *testing.T, dir, gate, stage, workflowID, snapshot string) string {
	t.Helper()
	runRel := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", workflowID))
	runDir := filepath.Join(dir, filepath.FromSlash(runRel))
	restrictedDir := filepath.Join(runDir, "restricted")
	logical := func(name string) string { return filepath.ToSlash(filepath.Join("restricted", name)) }
	prefix := strings.TrimSuffix(gate, "-gate")
	if gate == "qa-test-gate" {
		payload := writeCLIQAEvidence(t, runDir, workflowID, snapshot)
		output := filepath.ToSlash(filepath.Join("restricted", gate+".md"))
		if _, result := validate.ComposeQAExecution(validate.ComposeQAExecutionOptions{
			Root: dir, RunDir: runDir, WorkflowID: workflowID, ChangeSnapshot: snapshot, Output: output,
			ApprovedCaseSet: payload.ApprovedCaseSet.Path, DesignReview: payload.DesignReview.Path,
			QAOwnedResults: payload.QAOwnedResults.Path, CaseResultBinding: payload.CaseResultBinding.Path,
			ChangedFiles: payload.ChangedFiles.Path, Verification: payload.Verification.Path,
		}); !result.OK() {
			t.Fatal(result.Failures)
		}
		return filepath.ToSlash(filepath.Join(runRel, "restricted", gate+".md"))
	}
	bundleName := prefix + "-bundle.json"
	for name, text := range map[string]string{prefix + "-input.txt": "input", prefix + "-changed.txt": "changed", prefix + "-verification.txt": "verified"} {
		mustWriteCLI(t, filepath.Join(restrictedDir, name), text)
	}
	ref := func(name string) validate.EvidenceRef {
		return validate.EvidenceRef{Path: logical(name), SHA256: cliFileHash(t, filepath.Join(restrictedDir, name))}
	}
	writeCLIJSON(t, filepath.Join(restrictedDir, bundleName), validate.ContextBundle{BundleVersion: 1, WorkflowID: workflowID, ChangeSnapshot: snapshot, Inputs: []validate.EvidenceRef{ref(prefix + "-input.txt")}})
	policyID := map[string]string{"qa-test-gate": "qa.execution.v2", "complexity-gate": "complexity.post-development.v2", "architecture-health-gate": "architecture.post-development.v2", "code-quality-gate": "code-quality.post-development.v2"}[gate]
	var policy validate.ArtifactPolicy
	for _, candidate := range validate.Policy().ArtifactPolicies {
		if candidate.ID == policyID {
			policy = candidate
		}
	}
	checks := make([]validate.ReviewCheck, 0, len(policy.RequiredCheckIDs))
	statsName := prefix + "-statistics.json"
	statisticsRef := writeCLIComplexityStatisticsFixture(t, dir, runDir, workflowID, snapshot, logical(statsName))
	for _, id := range policy.RequiredCheckIDs {
		check := validate.ReviewCheck{ID: id, Status: "PASS", Message: cliReviewCheckMessage(id), EvidenceRefs: []validate.EvidenceRef{}, Findings: []validate.Finding{}}
		if id == "complexity.statistics" {
			check.EvidenceRefs = []validate.EvidenceRef{statisticsRef}
		}
		checks = append(checks, check)
	}
	changed, verification := ref(prefix+"-changed.txt"), ref(prefix+"-verification.txt")
	artifact := filepath.ToSlash(filepath.Join(runRel, "restricted", gate+".md"))
	complexityStatistics := ""
	if gate == "complexity-gate" {
		complexityStatistics = ref(statsName).Path
	}
	registration, rr := validate.ReceiptRegisterDispatch(withCLIReceiptBundle(t, validate.ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: gate, Stage: stage, Artifact: artifact, ContextBundle: logical(bundleName), ChangedFiles: changed.Path, Verification: verification.Path, ComplexityStatistics: complexityStatistics}))
	if !rr.OK() {
		t.Fatal(rr.Failures)
	}
	raw, _ := json.Marshal(map[string]any{"workflowId": workflowID, "changeSnapshot": snapshot, "gate": gate, "stage": stage, "subagentId": prefix + "-agent", "dispatchId": registration.DispatchID, "dispatchRegistrationArtifact": registration.DispatchRegistrationArtifact})
	if _, r := validate.ReceiptCapture(validate.ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: raw}); !r.OK() {
		t.Fatal(r.Failures)
	}
	semanticChecks := make([]validate.ReceiptSemanticCheck, 0, len(checks))
	for index, check := range checks {
		semanticChecks = append(semanticChecks, validate.ReceiptSemanticCheck{Position: index + 1, Status: check.Status, Message: check.Message})
	}
	if _, result := validate.ReceiptSubmit(validate.ReceiptSubmitOptions{Worktree: dir, Artifact: artifact, Checks: semanticChecks}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, r := validate.ReceiptCapture(validate.ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: raw}); !r.OK() {
		t.Fatal(r.Failures)
	}
	if _, r := validate.ReceiptFinalize(validate.ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: workflowID, Gate: gate, Stage: stage, Artifact: artifact}); !r.OK() {
		t.Fatal(r.Failures)
	}
	return artifact
}

func withCLIReceiptBundle(t *testing.T, options validate.ReceiptRegisterOptions) validate.ReceiptRegisterOptions {
	t.Helper()
	runDir := options.RunDir
	if runDir == "" {
		runDir = filepath.Join(options.Worktree, ".claude", "gates", "runs", options.WorkflowID)
	}
	if options.ContextBundle == "" {
		inputName := filepath.ToSlash(filepath.Join("restricted", "receipt-context.txt"))
		bundleName := filepath.ToSlash(filepath.Join("restricted", "receipt-context-bundle.json"))
		inputPath := filepath.Join(runDir, filepath.FromSlash(inputName))
		mustWriteCLI(t, inputPath, "context\n")
		writeCLIJSON(t, filepath.Join(runDir, filepath.FromSlash(bundleName)), validate.ContextBundle{
			BundleVersion:  1,
			WorkflowID:     options.WorkflowID,
			ChangeSnapshot: options.ChangeSnapshot,
			Inputs:         []validate.EvidenceRef{{Path: inputName, SHA256: cliFileHash(t, inputPath)}},
		})
		options.ContextBundle = bundleName
	}
	writeCLICompositionProof(t, options.Worktree, runDir, options.WorkflowID, options.ChangeSnapshot, "context-bundle.v1", options.ContextBundle)
	if options.Gate == "qa-test-gate" && options.Stage == "Design" && options.QACaseCount == 0 {
		options.QACaseCount = 1
	}
	if options.Gate == "qa-test-gate" && options.Stage == "Carry" && options.TransitionChain == "" {
		logical := func(name string) string { return filepath.ToSlash(filepath.Join("restricted", name)) }
		ref := func(name string) validate.EvidenceRef {
			return validate.EvidenceRef{Path: logical(name), SHA256: cliFileHash(t, filepath.Join(runDir, "restricted", name))}
		}
		for _, name := range []string{"carry-changed.txt", "carry-verification.txt", "carry-repair.txt"} {
			mustWriteCLI(t, filepath.Join(runDir, "restricted", name), name+"\n")
		}
		options.TransitionChain = logical("carry-transition.json")
		writeCLIJSON(t, filepath.Join(runDir, filepath.FromSlash(options.TransitionChain)), validate.TransitionChain{SchemaVersion: 2, WorkflowID: options.WorkflowID, TargetSnapshot: options.ChangeSnapshot, Hops: []validate.TransitionHop{{FromSnapshot: "source", ToSnapshot: options.ChangeSnapshot, ChangedFiles: ref("carry-changed.txt"), Verification: ref("carry-verification.txt"), RepairEvidence: ref("carry-repair.txt")}}})
		closure := logical("carry-source-qa.json")
		rootArtifact := logical("source.json")
		mustWriteCLI(t, filepath.Join(runDir, filepath.FromSlash(rootArtifact)), "{}\n")
		writeCLIJSON(t, filepath.Join(runDir, filepath.FromSlash(closure)), validate.EvidenceClosure{SchemaVersion: 2, WorkflowID: options.WorkflowID, ChangeSnapshot: "source", Gate: "qa-test-gate", Stage: "Execution", Verdict: "PASS", RootRole: "QA_EXECUTION", RootArtifact: rootArtifact, Entries: []validate.ClosureEntry{{Path: rootArtifact, SHA256: cliFileHash(t, filepath.Join(runDir, filepath.FromSlash(rootArtifact))), References: []string{}}}})
		options.CarrySourceClosures = []string{closure}
	}
	if options.Gate == "qa-test-gate" && options.Stage == "Carry" && options.TransitionChain != "" {
		writeCLICompositionProof(t, options.Worktree, runDir, options.WorkflowID, options.ChangeSnapshot, "transition-chain.v1", options.TransitionChain)
	}
	role, policyID := cliReviewOutputContract(options.Gate, options.Stage)
	_ = role
	if (options.Gate == "complexity-gate" || options.Gate == "architecture-health-gate") && options.ChangedFiles == "" && options.Verification == "" {
		policyID = map[string]string{"complexity-gate": "complexity.start-readiness.v2", "architecture-health-gate": "architecture.start-readiness.v2"}[options.Gate]
	}
	var policy validate.ArtifactPolicy
	for _, candidate := range validate.Policy().ArtifactPolicies {
		if candidate.ID == policyID {
			policy = candidate
		}
	}
	if policy.ChangedFilesRequired && options.ChangedFiles == "" {
		options.ChangedFiles = filepath.ToSlash(filepath.Join("restricted", "receipt-changed.txt"))
		mustWriteCLI(t, filepath.Join(runDir, filepath.FromSlash(options.ChangedFiles)), "changed\n")
	}
	if policy.VerificationRequired && options.Verification == "" {
		options.Verification = filepath.ToSlash(filepath.Join("restricted", "receipt-verification.txt"))
		mustWriteCLI(t, filepath.Join(runDir, filepath.FromSlash(options.Verification)), "verified\n")
	}
	if policyID == "complexity.post-development.v2" && options.ComplexityStatistics == "" {
		statistics := filepath.ToSlash(filepath.Join("restricted", "receipt-statistics.json"))
		writeCLIComplexityStatisticsFixture(t, options.Worktree, runDir, options.WorkflowID, options.ChangeSnapshot, statistics)
		options.ComplexityStatistics = statistics
	}
	for logical, composer := range map[string]string{options.ChangedFiles: "changed-files.v1", options.Verification: "verification.v1"} {
		if logical != "" {
			writeCLICompositionProof(t, options.Worktree, runDir, options.WorkflowID, options.ChangeSnapshot, composer, logical)
		}
	}
	writeCLIJSON(t, filepath.Join(options.Worktree, "hooks", "pollution-patterns.json"), map[string]any{"english": map[string]any{"patternGroups": []any{}}, "chinese": map[string]any{"termGroups": []any{}}})
	if cliReviewJudgment(options.Gate, options.Stage) && options.Prompt == "" {
		name := strings.NewReplacer(".", "-", " ", "-", "/", "-").Replace(filepath.Base(options.Artifact) + "-" + options.Stage)
		promptName := filepath.ToSlash(filepath.Join("restricted", "receipt-final-send-"+name+".txt"))
		currentDiff := "git diff base --"
		if options.Gate == "qa-test-gate" && options.Stage == "Design Review" {
			if strings.HasSuffix(options.QADesignCaseSet, ".md") {
				currentDiff = filepath.ToSlash(filepath.Join(".claude", "gates", "runs", options.WorkflowID, options.QADesignCaseSet))
			}
		}
		prepared, result := validate.PrepareDispatchPrompt(validate.PrepareDispatchPromptOptions{
			Root: options.Worktree, OutputFile: filepath.Join(runDir, filepath.FromSlash(promptName)), Gate: options.Gate, Stage: options.Stage,
			CurrentRequirement: "requirements/current.md", CurrentDiff: currentDiff, Worktree: options.Worktree,
			ChangeSnapshot: options.ChangeSnapshot, ReviewArtifact: options.Artifact, PolicyID: policyID,
			ContextBundle: filepath.Join(runDir, filepath.FromSlash(options.ContextBundle)),
		})
		if !result.OK() {
			t.Fatal(result.Failures)
		}
		if prepared.SHA256 == "" {
			t.Fatal("prepared prompt hash is empty")
		}
		options.Prompt = promptName
	}
	return options
}

func writeCLICompositionProof(t *testing.T, root, runDir, workflowID, snapshot, composer, logical string) {
	t.Helper()
	path := filepath.Join(runDir, filepath.FromSlash(logical))
	ref := validate.EvidenceRef{Path: logical, SHA256: cliFileHash(t, path)}
	writeCLICompositionProofOutputs(t, runDir, workflowID, snapshot, composer, logical, []validate.EvidenceRef{ref})
}

func writeCLICompositionProofOutputs(t *testing.T, runDir, workflowID, snapshot, composer, anchorLogical string, outputs []validate.EvidenceRef) {
	t.Helper()
	sum := sha256.Sum256([]byte(composer + "\n" + anchorLogical))
	proof := filepath.Join(runDir, "restricted", "proofs", "compositions", hex.EncodeToString(sum[:])+".json")
	writeCLIJSON(t, proof, validate.CompositionProof{ProofVersion: 1, Composer: composer, WorkflowID: workflowID, ChangeSnapshot: snapshot, Outputs: outputs})
}

func cliReviewJudgment(gate, stage string) bool {
	return gate == "complexity-gate" || gate == "architecture-health-gate" || gate == "code-quality-gate" || (gate == "qa-test-gate" && (stage == "Design Review" || stage == "Carry"))
}

func cliReviewOutputContract(gate, stage string) (string, string) {
	if gate == "qa-test-gate" && stage == "Carry" {
		return "CARRY_ARBITER", "carry.arbiter.v2"
	}
	if gate == "qa-test-gate" && stage == "Design Review" {
		return "QA_REVIEW", "qa.design-review.v2"
	}
	return map[string]string{"complexity-gate": "COMPLEXITY_REVIEW", "architecture-health-gate": "ARCHITECTURE_REVIEW", "code-quality-gate": "CODE_QUALITY_REVIEW"}[gate], map[string]string{"complexity-gate": "complexity.post-development.v2", "architecture-health-gate": "architecture.post-development.v2", "code-quality-gate": "code-quality.post-development.v2"}[gate]
}

func cliReviewCheckMessage(id string) string {
	return "checked"
}

func validCLIFinalPrompt(worktree, gate, artifact string) string {
	return "formal_gate_dispatch: " + gate + "\nCurrent requirement: requirements/current.md\nCurrent diff or proposed change: git diff base --\nWorktree: " + worktree + "\nBase commit or snapshot: base..snapshot\nOutput path: " + artifact + "\nOutput format: schema-version-2 JSON\n"
}

func writeCLIQAEvidence(t *testing.T, dir, workflowID, snapshot string) validate.QAExecutionPayload {
	t.Helper()
	worktree := filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(dir))))
	restrictedDir := filepath.Join(dir, "restricted")
	logical := func(name string) string { return filepath.ToSlash(filepath.Join("restricted", name)) }
	for name, text := range map[string]string{"qa-changed.txt": "changed", "qa-verification.txt": "verified"} {
		mustWriteCLI(t, filepath.Join(restrictedDir, name), text)
	}
	writeCLICompositionProof(t, worktree, dir, workflowID, snapshot, "changed-files.v1", logical("qa-changed.txt"))
	writeCLICompositionProof(t, worktree, dir, workflowID, snapshot, "verification.v1", logical("qa-verification.txt"))
	ref := func(name string) validate.EvidenceRef {
		return validate.EvidenceRef{Path: logical(name), SHA256: cliFileHash(t, filepath.Join(restrictedDir, name))}
	}
	approved, designReview := writeCLIDesignReviewClosure(t, worktree, dir, workflowID, snapshot+"-design")
	writeCLIJSON(t, filepath.Join(restrictedDir, "qa-results.json"), map[string]any{
		"owner": "QA", "workflowId": workflowID, "changeSnapshot": snapshot, "stage": "Execution", "status": "COMPLETE", "overallOutcome": "PASS",
		"executions":  []any{map[string]any{"id": "E-001", "outcome": "PASS", "procedure": "Run the approved case", "result": "The case passed"}},
		"caseResults": []any{map[string]any{"caseId": "CASE-001", "status": "PASS", "procedures": []string{"E-001"}, "oracle": "The approved behavior is observed"}},
	})
	results := ref("qa-results.json")
	writeCLIJSON(t, filepath.Join(restrictedDir, "qa-case-binding.json"), map[string]any{
		"workflowId": workflowID, "changeSnapshot": snapshot, "approvedCaseSet": approved, "qaOwnedResults": results, "complete": true,
		"bindings": []any{map[string]any{"caseId": "CASE-001", "resultPointer": "/caseResults/0", "status": "PASS", "executionRefs": []string{"E-001"}, "procedures": []string{"E-001"}, "oracle": "The approved behavior is observed"}},
	})
	binding := ref("qa-case-binding.json")
	writeCLICompositionProofOutputs(t, dir, workflowID, snapshot, "qa-owned-evidence.v1", results.Path, []validate.EvidenceRef{results, binding})
	return validate.QAExecutionPayload{
		ApprovedCaseSet: approved, DesignReview: designReview, QAOwnedResults: results, CaseResultBinding: binding,
		ChangedFiles: ref("qa-changed.txt"), Verification: ref("qa-verification.txt"),
	}
}

type cliReceiptDeps struct {
	DispatchRegistrationArtifact string `json:"dispatchRegistrationArtifact"`
	DispatchRegistrationSha256   string `json:"dispatchRegistrationSha256"`
	StartEventArtifact           string `json:"startEventArtifact"`
	StartEventSha256             string `json:"startEventSha256"`
	StopEventArtifact            string `json:"stopEventArtifact"`
	StopEventSha256              string `json:"stopEventSha256"`
	PromptArtifact               string `json:"promptArtifact"`
	PromptSha256                 string `json:"promptSha256"`
}

func writeCLIDesignReviewClosure(t *testing.T, worktree, runDir, workflowID, snapshot string) (validate.EvidenceRef, validate.EvidenceRef) {
	t.Helper()
	inputName, bundleName := "design-input.txt", "design-bundle.json"
	restrictedDir := filepath.Join(runDir, "restricted")
	logicalName := func(name string) string { return filepath.ToSlash(filepath.Join("restricted", name)) }
	mustWriteCLI(t, filepath.Join(restrictedDir, inputName), "requirements\n")
	ref := func(name string) validate.EvidenceRef {
		return validate.EvidenceRef{Path: logicalName(name), SHA256: cliFileHash(t, filepath.Join(restrictedDir, name))}
	}
	writeCLIJSON(t, filepath.Join(restrictedDir, bundleName), validate.ContextBundle{BundleVersion: 1, WorkflowID: workflowID, ChangeSnapshot: snapshot, Inputs: []validate.EvidenceRef{ref(inputName)}})
	receiptBound := func(stage, artifact, subagent, qaDesignCaseSet, qaDesignReceipt string, write func()) (validate.EvidenceRef, cliReceiptDeps) {
		registration, result := validate.ReceiptRegisterDispatch(withCLIReceiptBundle(t, validate.ReceiptRegisterOptions{Worktree: worktree, RunDir: runDir, Provider: "codex", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: "qa-test-gate", Stage: stage, Artifact: artifact, ContextBundle: logicalName(bundleName), QADesignCaseSet: qaDesignCaseSet, QADesignReceipt: qaDesignReceipt}))
		if !result.OK() {
			t.Fatal(result.Failures)
		}
		raw, _ := json.Marshal(map[string]any{"workflowId": workflowID, "changeSnapshot": snapshot, "gate": "qa-test-gate", "stage": stage, "subagentId": subagent, "dispatchId": registration.DispatchID, "dispatchRegistrationArtifact": registration.DispatchRegistrationArtifact})
		if _, result = validate.ReceiptCapture(validate.ReceiptCaptureOptions{Worktree: worktree, Provider: "codex", Event: "SubagentStart", Payload: raw}); !result.OK() {
			t.Fatal(result.Failures)
		}
		write()
		if _, result = validate.ReceiptCapture(validate.ReceiptCaptureOptions{Worktree: worktree, Provider: "codex", Event: "SubagentStop", Payload: raw}); !result.OK() {
			t.Fatal(result.Failures)
		}
		output, result := validate.ReceiptFinalize(validate.ReceiptFinalizeOptions{Worktree: worktree, Provider: "codex", WorkflowID: workflowID, Gate: "qa-test-gate", Stage: stage, Artifact: artifact})
		if !result.OK() {
			t.Fatal(result.Failures)
		}
		logical, err := filepath.Rel(runDir, filepath.Join(worktree, filepath.FromSlash(output.ReceiptArtifact)))
		if err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(filepath.Join(worktree, filepath.FromSlash(output.ReceiptArtifact)))
		if err != nil {
			t.Fatal(err)
		}
		var deps cliReceiptDeps
		if err := json.Unmarshal(data, &deps); err != nil {
			t.Fatal(err)
		}
		return validate.EvidenceRef{Path: filepath.ToSlash(logical), SHA256: output.ReceiptSha256}, deps
	}
	caseArtifact := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", workflowID, "restricted", "approved-cases.md"))
	designReceipt, designDeps := receiptBound("Design", caseArtifact, "design-agent", "", "", func() {
		if _, result := validate.ReceiptSubmit(validate.ReceiptSubmitOptions{Worktree: worktree, Artifact: caseArtifact, DesignCases: []validate.ReceiptSemanticDesignCase{{Position: 1, Values: []string{"claim", "source", "action", "oracle", "failure signal", "evidence", "gap"}}}}); !result.OK() {
			t.Fatal(result.Failures)
		}
	})
	approved := ref("approved-cases.md")
	var policy validate.ArtifactPolicy
	for _, candidate := range validate.Policy().ArtifactPolicies {
		if candidate.ID == "qa.design-review.v2" {
			policy = candidate
		}
	}
	checks := make([]validate.ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		check := validate.ReviewCheck{ID: id, Status: "PASS", Message: cliReviewCheckMessage(id), EvidenceRefs: []validate.EvidenceRef{}, Findings: []validate.Finding{}}
		if id == "qa.design.case-set-binding" {
			check.EvidenceRefs = []validate.EvidenceRef{approved, designReceipt}
		}
		checks = append(checks, check)
	}
	reviewArtifact := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", workflowID, "restricted", "design-review.json"))
	reviewReceipt, reviewDeps := receiptBound("Design Review", reviewArtifact, "design-review-agent", approved.Path, designReceipt.Path, func() {
		semantics := make([]validate.ReceiptSemanticCheck, 0, len(checks))
		for index, check := range checks {
			semantics = append(semantics, validate.ReceiptSemanticCheck{Position: index + 1, Status: check.Status, Message: check.Message})
		}
		if _, result := validate.ReceiptSubmit(validate.ReceiptSubmitOptions{Worktree: worktree, Artifact: reviewArtifact, Checks: semantics}); !result.OK() {
			t.Fatal(result.Failures)
		}
	})
	logical := func(repoPath string) string {
		rel, err := filepath.Rel(runDir, filepath.Join(worktree, filepath.FromSlash(repoPath)))
		if err != nil {
			t.Fatal(err)
		}
		return filepath.ToSlash(rel)
	}
	depsRefs := func(deps cliReceiptDeps) []validate.EvidenceRef {
		refs := []validate.EvidenceRef{{Path: logical(deps.DispatchRegistrationArtifact), SHA256: deps.DispatchRegistrationSha256}, {Path: logical(deps.StartEventArtifact), SHA256: deps.StartEventSha256}, {Path: logical(deps.StopEventArtifact), SHA256: deps.StopEventSha256}}
		if deps.PromptArtifact != "" {
			refs = append(refs, validate.EvidenceRef{Path: logical(deps.PromptArtifact), SHA256: deps.PromptSha256})
		}
		return refs
	}
	rootRefs := []string{approved.Path, designReceipt.Path, ref(bundleName).Path}
	sort.Strings(rootRefs)
	entries := []validate.ClosureEntry{
		{Path: logicalName("design-review.json"), SHA256: cliFileHash(t, filepath.Join(restrictedDir, "design-review.json")), References: rootRefs},
		{Path: approved.Path, SHA256: approved.SHA256, References: []string{}},
		{Path: ref(bundleName).Path, SHA256: ref(bundleName).SHA256, References: []string{ref(inputName).Path}},
		{Path: ref(inputName).Path, SHA256: ref(inputName).SHA256, References: []string{}},
	}
	for receipt, deps := range map[validate.EvidenceRef][]validate.EvidenceRef{designReceipt: depsRefs(designDeps), reviewReceipt: depsRefs(reviewDeps)} {
		refs := make([]string, 0, len(deps))
		for _, dep := range deps {
			refs = append(refs, dep.Path)
			entries = append(entries, validate.ClosureEntry{Path: dep.Path, SHA256: dep.SHA256, References: []string{}})
		}
		sort.Strings(refs)
		entries = append(entries, validate.ClosureEntry{Path: receipt.Path, SHA256: receipt.SHA256, References: refs})
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	closureName := "design-review-closure.json"
	writeCLIJSON(t, filepath.Join(restrictedDir, closureName), validate.EvidenceClosure{SchemaVersion: 2, WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: "qa-test-gate", Stage: "Design Review", Verdict: "PASS", RootRole: "QA_REVIEW", RootArtifact: logicalName("design-review.json"), Receipt: reviewReceipt.Path, Entries: entries})
	return approved, ref(closureName)
}

func writeCLIJSON(t *testing.T, path string, value any) {
	t.Helper()
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	mustWriteCLI(t, path, string(data)+"\n")
}
func cliFileHash(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func recordCLIFourGatePrerequisites(t *testing.T, dir, workflowID, snapshot string) {
	t.Helper()
	for _, item := range []struct {
		gate  string
		stage string
		mode  string
	}{
		{gate: "qa-test-gate", stage: "Execution", mode: "formal"},
		{gate: "complexity-gate"},
		{gate: "architecture-health-gate"},
		{gate: "code-quality-gate"},
	} {
		artifact := writeCLIArtifact(t, dir, item.gate, item.stage, workflowID, snapshot)
		result := validate.GateRecord(validate.GateRecordOptions{
			Worktree:       dir,
			StatePath:      filepath.ToSlash(filepath.Join(".claude", "gates", "runs", workflowID, "restricted", "gate-state.json")),
			RunDir:         filepath.Join(dir, ".claude", "gates", "runs", workflowID),
			Gate:           item.gate,
			Verdict:        "PASS",
			Mode:           item.mode,
			Stage:          item.stage,
			Artifact:       artifact,
			Actor:          item.gate,
			WorkflowID:     workflowID,
			ChangeSnapshot: snapshot,
		})
		if !result.OK() {
			t.Fatalf("expected prerequisite %s to record, got %#v", item.gate, result.Failures)
		}
	}
}

func validHandoffText() string {
	return `Gate Handoff Request
Reason: formal development handoff
Skill source path: SKILL.md
Copied skill path: .claude/skills/formal-gates/SKILL.md
WorkflowId: wf
Change snapshot: snap
Worktree: repo
Base commit: abc123
Snapshot id: snap
Requirement document target or OpenSpec change: openspec/changes/example
Required independent gates: qa-test-gate, complexity-gate, architecture-health-gate, code-quality-gate
Artifacts to provide: implementation evidence
Bundle or manifest path: bundle.md sha256=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
Verification requirements: go test ./...
Development-time complexity budget: max-net 250, max-new-prod-files 0, max-prod-insertions 300
Complexity check command: bin/formal-gates complexity check --task-type bugfix --max-net 250 --max-new-prod-files 0 --max-prod-insertions 300 --worktree repo --vcs auto
Budget stop triggers: stop if any complexity check exits non-zero or new subsystem names appear
Budget expansion approval path: .claude/gates/artifacts/anti-complexity-approval.md, required before continuing if exceeded
Forbidden context: no prior findings
	Formal flow mode: none
Trigger source: user
QA case design artifact: qa-design.md
Approved QA case set: approved-cases.md
Continue after: handoff validation PASS
`
}
