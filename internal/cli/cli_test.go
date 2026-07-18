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
	"sort"
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
	dispatch := filepath.Join(run, "complexity", "dispatch.txt")
	prompt := filepath.Join(run, "complexity", "prompt.txt")
	bundle := filepath.Join(run, "complexity", "bundle.json")
	mustWriteCLI(t, dispatch, validCLIReviewerDispatch+"Worktree: "+root+"\nBase commit or snapshot: base..snapshot\nOutput path: .claude/gates/runs/wf/restricted/complexity/review.json\nOutput format: schema-version-2 JSON\n")
	mustWriteCLI(t, bundle, "{}\n")
	var stdout, stderr bytes.Buffer
	code := Run("formal-gates", []string{
		"prompt", "prepare", "--root", root,
		"--dispatch", dispatch,
		"--output", prompt,
		"--binding", "contextBundle=" + bundle,
	}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("prepare failed: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(prompt)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "contextBundle=restricted/complexity/bundle.json sha256=") || strings.Contains(string(data), "static-validation=PASS sha256=") {
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

	mustWriteCLI(t, handoff, validHandoffText())
	stdout.Reset()
	code = Run("formal-gates", []string{
		"handoff", "validate",
		"--root", dir,
		"--file", "handoff.md",
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
	var stdout bytes.Buffer
	code := Run("formal-gates", []string{"complexity", "check", "--task-type", "refactor", "--worktree", dir, "--vcs", "git", "--json"}, IO{Stdout: &stdout})
	if code != 0 {
		t.Fatalf("complexity producer failed, code=%d stdout=%q", code, stdout.String())
	}
	produced := stdout.String()
	if !strings.Contains(produced, `"failures": []`) || !strings.Contains(produced, `"review_required": []`) {
		t.Fatalf("complexity producer must encode empty result slices as arrays: %s", produced)
	}
	mustWriteCLI(t, filepath.Join(restrictedDir, "statistics.json"), produced)
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
			check.EvidenceRefs = []validate.EvidenceRef{ref("statistics.json")}
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

func TestRunWorkflowRecordTransitionUsesOnlyTypedCarryArtifact(t *testing.T) {
	dir, workflowID, source, target := t.TempDir(), "wf", "source", "target"
	runRel := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", workflowID))
	runDir := filepath.Join(dir, filepath.FromSlash(runRel))
	sourceArtifact := writeCLIArtifact(t, dir, "qa-test-gate", "Execution", workflowID, source)
	var stdout bytes.Buffer
	if code := Run("formal-gates", []string{"workflow", "record-stage", "--worktree", dir, "--gate", "qa-test-gate", "--stage", "Execution", "--mode", "formal", "--verdict", "PASS", "--artifact", sourceArtifact, "--workflow-id", workflowID, "--change-snapshot", source}, IO{Stdout: &stdout}); code != 0 {
		t.Fatalf("source QA record failed: %s", stdout.String())
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
	chain := validate.TransitionChain{SchemaVersion: 2, WorkflowID: workflowID, TargetSnapshot: target, ProposedCarriedGates: []string{"qa-test-gate"}, Hops: []validate.TransitionHop{{FromSnapshot: source, ToSnapshot: target, ChangedFiles: ref(base + "/changed.txt"), Verification: ref(base + "/verification.txt"), RepairEvidence: ref(base + "/repair-evidence.txt")}}}
	writeCLIJSON(t, filepath.Join(runDir, filepath.FromSlash(base+"/chain.json")), chain)
	sourceClosure := state.Gates["qa-test-gate"]
	payload := validate.CarryPayload{ContextBundle: ref(base + "/context.json"), ReviewPolicyID: "carry.arbiter.v2", TransitionChain: ref(base + "/chain.json"), Decisions: []validate.CarryDecision{{Gate: "qa-test-gate", SourceSnapshot: source, SourceGateEvidence: validate.EvidenceRef{Path: strings.TrimPrefix(sourceClosure.Artifact, runRel+"/"), SHA256: sourceClosure.ArtifactHash}, Decision: "ACCEPT_CARRY", RerunFromGate: "", Reason: "The gate remains valid."}}}
	artifact := filepath.ToSlash(filepath.Join(runRel, base, "carry.json"))
	registration, result := validate.ReceiptRegisterDispatch(withCLIReceiptBundle(t, validate.ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: workflowID, ChangeSnapshot: target, Gate: "qa-test-gate", Stage: "Carry", Artifact: artifact, ContextBundle: base + "/context.json"}))
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	raw, _ := json.Marshal(map[string]any{"workflowId": workflowID, "changeSnapshot": target, "gate": "qa-test-gate", "stage": "Carry", "subagentId": "carry-agent", "dispatchId": registration.DispatchID, "dispatchRegistrationArtifact": registration.DispatchRegistrationArtifact})
	if _, result = validate.ReceiptCapture(validate.ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: raw}); !result.OK() {
		t.Fatal(result.Failures)
	}
	payloadData, _ := json.Marshal(payload)
	writeCLIJSON(t, filepath.Join(runDir, filepath.FromSlash(base+"/carry.json")), validate.FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "CARRY_ARBITER", WorkflowID: workflowID, ChangeSnapshot: target, Gate: "qa-test-gate", Stage: "Carry", Verdict: "PASS", Payload: payloadData})
	if _, result = validate.ReceiptCapture(validate.ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStop", Payload: raw}); !result.OK() {
		t.Fatal(result.Failures)
	}
	if _, result = validate.ReceiptFinalize(validate.ReceiptFinalizeOptions{Worktree: dir, Provider: "codex", WorkflowID: workflowID, Gate: "qa-test-gate", Stage: "Carry", Artifact: artifact}); !result.OK() {
		t.Fatal(result.Failures)
	}
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
	payload.Checks[0].Message = "review result is not a PASS; static-validation=PASS binding was checked"
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
	attempts := `[{"status":"PASS","accepted":true,"artifact":"` + attemptRel + `","artifactHash":"` + cliFileHash(t, attemptPath) + `"}]`
	commands := [][]string{
		{"workflow", "record-stage", "--worktree", dir, "--state", "gate-state.json", "--gate", "qa-test-gate", "--verdict", "PASS", "--mode", "formal", "--stage", "Execution", "--artifact", artifact, "--workflow-id", "wf", "--change-snapshot", "snap"},
		{"workflow", "verify-admission", "--worktree", dir, "--state", "gate-state.json", "--gate", "complexity-gate", "--workflow-id", "wf", "--change-snapshot", "snap"},
		{"workflow", "final-verification", "--worktree", dir, "--state", "gate-state.json", "--attempts-json", attempts, "--output", filepath.ToSlash(filepath.Join(runRel, "restricted", "final-verification.json")), "--record-final-qa", "--final-qa-artifact", filepath.ToSlash(filepath.Join(runRel, "restricted", "final-execution.json")), "--workflow-id", "wf", "--change-snapshot", "snap"},
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
	attemptsRel := filepath.ToSlash(filepath.Join(runRel, "restricted", "attempts.json"))
	attempts := filepath.Join(dir, filepath.FromSlash(attemptsRel))
	mustWriteCLI(t, attempts, `[{"status":"PASS","accepted":true,"artifact":"`+attemptRel+`","artifactHash":"`+cliFileHash(t, attemptPath)+`"}]`)
	output := filepath.ToSlash(filepath.Join(runRel, "restricted", "final-verification.json"))
	var stdout bytes.Buffer

	code := Run("formal-gates", []string{
		"workflow", "final-verification",
		"--worktree", dir,
		"--attempts-file", attemptsRel,
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
		"--attempts-json", `[{"status":"PASS","accepted":true,"artifact":"` + attemptRel + `","artifactHash":"` + cliFileHash(t, attemptPath) + `"}]`,
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
	stateBefore, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	closuresBefore, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(runRel), "closures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	finalExecutionRel := filepath.ToSlash(filepath.Join(runRel, "restricted", "final-execution.md"))
	finalExecutionPath := filepath.Join(dir, filepath.FromSlash(finalExecutionRel))
	mustWriteCLI(t, finalExecutionPath, "caller-authored bytes\n")
	var stdout bytes.Buffer
	args := []string{
		"workflow", "final-verification",
		"--worktree", dir,
		"--run-dir", runRel,
		"--attempts-json", `[{"status":"PASS","accepted":true,"artifact":"` + attemptRel + `","artifactHash":"` + cliFileHash(t, attemptPath) + `"}]`,
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

	if err := os.WriteFile(statePath, stateBefore, 0o600); err != nil {
		t.Fatal(err)
	}
	mustWriteCLI(t, finalExecutionPath, "different caller-authored bytes\n")
	stdout.Reset()
	if code = Run("formal-gates", args, IO{Stdout: &stdout}); code != 0 {
		t.Fatalf("expected repeated final-verification to pass, code=%d stdout=%q", code, stdout.String())
	}
	second, err := os.ReadFile(finalExecutionPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("generated FinalExecution bytes are not deterministic\nfirst=%s\nsecond=%s", first, second)
	}
	closuresAfter, err := filepath.Glob(filepath.Join(dir, filepath.FromSlash(runRel), "closures", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(closuresAfter) != len(closuresBefore) {
		t.Fatalf("FinalExecution created a fifth closure: before=%v after=%v", closuresBefore, closuresAfter)
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
	if code == 0 || !strings.Contains(stdout.String(), "requiredMode=post-development") || !strings.Contains(stdout.String(), "mode=finalization") {
		t.Fatalf("FinalExecution satisfied the QA Execution prerequisite, code=%d stdout=%q", code, stdout.String())
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
		"--attempts-json", `[{"status":"PASS","accepted":true,"artifact":"` + runRel + `/restricted/attempt.json","artifactHash":"` + cliFileHash(t, attemptPath) + `"}]`,
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
		"--attempts-json", `[{"status":"PASS","accepted":true,"artifact":"` + missing + `"}]`,
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

func TestRunWorkflowFinalVerificationValidatesAcceptedAttemptArtifactHash(t *testing.T) {
	tests := []struct {
		name        string
		hash        func(string) string
		changeBytes bool
		wantPass    bool
	}{
		{name: "valid", hash: func(hash string) string { return hash }, wantPass: true},
		{name: "missing", hash: func(string) string { return "" }},
		{name: "malformed", hash: func(string) string { return "not-a-sha256" }},
		{name: "bytes changed after hash captured", hash: func(hash string) string { return hash }, changeBytes: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			runRel := ".claude/gates/runs/W"
			runAbs := filepath.Join(dir, filepath.FromSlash(runRel))
			recordCLIFourGatePrerequisites(t, dir, "W", "S")
			_, result := validate.GateShow(validate.GateShowOptions{Worktree: dir, StatePath: runRel + "/restricted/gate-state.json"})
			if !result.OK() {
				t.Fatal(result.Failures)
			}
			statePath := filepath.Join(runAbs, "restricted", "gate-state.json")
			stateBefore, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}

			attemptPath := filepath.Join(runAbs, "restricted", "attempt.json")
			mustWriteCLI(t, attemptPath, `{"ok":true}`+"\n")
			capturedHash := cliFileHash(t, attemptPath)
			if test.changeBytes {
				mustWriteCLI(t, attemptPath, `{"ok":false}`+"\n")
			}
			attempt := map[string]any{
				"status":   "PASS",
				"accepted": true,
				"artifact": filepath.ToSlash(filepath.Join(runRel, "restricted", "attempt.json")),
			}
			if hash := test.hash(capturedHash); hash != "" {
				attempt["artifactHash"] = hash
			}
			attemptsJSON, err := json.Marshal([]map[string]any{attempt})
			if err != nil {
				t.Fatal(err)
			}

			finalExecutionPath := filepath.Join(runAbs, "restricted", "final-execution.json")
			mustWriteCLI(t, finalExecutionPath, "existing final execution\n")
			finalExecutionBefore, err := os.ReadFile(finalExecutionPath)
			if err != nil {
				t.Fatal(err)
			}
			var stdout bytes.Buffer
			code := Run("formal-gates", []string{
				"workflow", "final-verification",
				"--worktree", dir,
				"--run-dir", runRel,
				"--attempts-json", string(attemptsJSON),
				"--output", runRel + "/restricted/final-verification.json",
				"--record-final-qa",
				"--final-qa-artifact", runRel + "/restricted/final-execution.json",
				"--workflow-id", "W",
				"--change-snapshot", "S",
			}, IO{Stdout: &stdout})

			if test.wantPass {
				if code != 0 {
					t.Fatalf("valid hash failed, code=%d stdout=%q", code, stdout.String())
				}
				return
			}
			if code == 0 || !strings.Contains(stdout.String(), "artifactHash") {
				t.Fatalf("invalid hash was accepted, code=%d stdout=%q", code, stdout.String())
			}
			stateAfter, err := os.ReadFile(statePath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(stateBefore, stateAfter) {
				t.Fatalf("rejection changed authoritative state\nbefore=%s\nafter=%s", stateBefore, stateAfter)
			}
			finalExecutionAfter, err := os.ReadFile(finalExecutionPath)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(finalExecutionBefore, finalExecutionAfter) {
				t.Fatalf("rejection generated FinalExecution\nbefore=%s\nafter=%s", finalExecutionBefore, finalExecutionAfter)
			}
		})
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
	if _, err := os.Stat(filepath.Join(dir, filepath.FromSlash(artifact))); !os.IsNotExist(err) {
		t.Fatalf("registration must not create reviewer output, err=%v", err)
	}
	stdout.Reset()
	if code := Run("formal-gates", args, IO{Stdout: &stdout}); code == 0 || !strings.Contains(stdout.String(), "already reserved") {
		t.Fatalf("expected duplicate reservation rejection, code=%d stdout=%q", code, stdout.String())
	}
	runDir := filepath.Join(dir, ".claude", "gates", "runs", "wf")
	rebindTemplate := filepath.Join(runDir, "restricted", "receipt-rebind-template.txt")
	mustWriteCLI(t, rebindTemplate, "formal_gate_dispatch: complexity-gate\nCurrent requirement: requirements/current.md\nCurrent diff or proposed change: git diff base --\nWorktree: "+dir+"\nBase commit or snapshot: snap\nOutput path: "+artifact+"\nOutput format: closed  schema-version-2 COMPLEXITY_REVIEW JSON for complexity.post-development.v2\n")
	if _, result := validate.PrepareDispatchPrompt(validate.PrepareDispatchPromptOptions{Root: dir, DispatchFile: rebindTemplate, OutputFile: filepath.Join(runDir, filepath.FromSlash(fixture.Prompt)), Bindings: []validate.DispatchPromptBinding{{Name: "contextBundle", Path: filepath.Join(runDir, filepath.FromSlash(fixture.ContextBundle))}}}); !result.OK() {
		t.Fatal(result.Failures)
	}
	stdout.Reset()
	if code := Run("formal-gates", args, IO{Stdout: &stdout}); code != 0 || !strings.Contains(stdout.String(), `"status": "rebound"`) {
		t.Fatalf("expected changed prompt to rebind the unstarted reservation, code=%d stdout=%q", code, stdout.String())
	}
	stdout.Reset()
	args = args[:len(args)-2]
	if code := Run("formal-gates", args, IO{Stdout: &stdout}); code == 0 || !strings.Contains(stdout.String(), "--change-snapshot") {
		t.Fatalf("expected missing snapshot rejection, code=%d stdout=%q", code, stdout.String())
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

func writeCLIArtifact(t *testing.T, dir, gate, stage, workflowID, snapshot string) string {
	t.Helper()
	runRel := filepath.ToSlash(filepath.Join(".claude", "gates", "runs", workflowID))
	runDir := filepath.Join(dir, filepath.FromSlash(runRel))
	restrictedDir := filepath.Join(runDir, "restricted")
	logical := func(name string) string { return filepath.ToSlash(filepath.Join("restricted", name)) }
	prefix := strings.TrimSuffix(gate, "-gate")
	if gate == "qa-test-gate" {
		payload := writeCLIQAEvidence(t, runDir, workflowID, snapshot)
		payloadData, _ := json.Marshal(payload)
		path := filepath.Join(restrictedDir, gate+".md")
		writeCLIJSON(t, path, validate.FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "QA_EXECUTION", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: gate, Stage: stage, Verdict: "PASS", Payload: payloadData})
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
	writeCLIJSON(t, filepath.Join(restrictedDir, statsName), validate.ComplexityReport{Status: "PASS", VCS: "git", Worktree: dir, TaskType: "refactor", BudgetSource: "none", BudgetOverrides: validate.ComplexityBudgetOverride{}, Summary: validate.ComplexitySummary{}, Failures: []string{}, ReviewRequired: []string{}, Warnings: []string{}, LargestFiles: []validate.ComplexityFileChange{}})
	for _, id := range policy.RequiredCheckIDs {
		check := validate.ReviewCheck{ID: id, Status: "PASS", Message: cliReviewCheckMessage(id), EvidenceRefs: []validate.EvidenceRef{}, Findings: []validate.Finding{}}
		if id == "complexity.statistics" {
			check.EvidenceRefs = []validate.EvidenceRef{ref(statsName)}
		}
		checks = append(checks, check)
	}
	changed, verification := ref(prefix+"-changed.txt"), ref(prefix+"-verification.txt")
	payload := validate.ReviewerPayload{ContextBundle: ref(bundleName), ReviewPolicyID: policy.ID, Checks: checks, ChangedFiles: &changed, Verification: &verification}
	payloadData, _ := json.Marshal(payload)
	artifact := filepath.ToSlash(filepath.Join(runRel, "restricted", gate+".md"))
	registration, rr := validate.ReceiptRegisterDispatch(withCLIReceiptBundle(t, validate.ReceiptRegisterOptions{Worktree: dir, Provider: "codex", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: gate, Stage: stage, Artifact: artifact, ContextBundle: logical(bundleName)}))
	if !rr.OK() {
		t.Fatal(rr.Failures)
	}
	raw, _ := json.Marshal(map[string]any{"workflowId": workflowID, "changeSnapshot": snapshot, "gate": gate, "stage": stage, "subagentId": prefix + "-agent", "dispatchId": registration.DispatchID, "dispatchRegistrationArtifact": registration.DispatchRegistrationArtifact})
	if _, r := validate.ReceiptCapture(validate.ReceiptCaptureOptions{Worktree: dir, Provider: "codex", Event: "SubagentStart", Payload: raw}); !r.OK() {
		t.Fatal(r.Failures)
	}
	writeCLIJSON(t, filepath.Join(restrictedDir, gate+".md"), validate.FormalGateEvidence{SchemaVersion: 2, ArtifactRole: policy.ArtifactRole, WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: gate, Stage: stage, Verdict: "PASS", Payload: payloadData})
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
	writeCLIJSON(t, filepath.Join(options.Worktree, "hooks", "pollution-patterns.json"), map[string]any{"english": map[string]any{"patternGroups": []any{}}, "chinese": map[string]any{"termGroups": []any{}}})
	if cliReviewJudgment(options.Gate, options.Stage) && options.Prompt == "" {
		name := strings.NewReplacer(".", "-", " ", "-", "/", "-").Replace(filepath.Base(options.Artifact) + "-" + options.Stage)
		promptName := filepath.ToSlash(filepath.Join("restricted", "receipt-final-send-"+name+".txt"))
		templateName := filepath.ToSlash(filepath.Join("restricted", "receipt-dispatch-template-"+name+".txt"))
		dispatchRole := options.Gate
		if options.Gate == "qa-test-gate" && options.Stage == "Carry" {
			dispatchRole = "carry-forward-arbiter"
		}
		role, policyID := cliReviewOutputContract(options.Gate, options.Stage)
		template := "formal_gate_dispatch: " + dispatchRole + "\nCurrent requirement: requirements/current.md\nCurrent diff or proposed change: git diff base --\nWorktree: " + options.Worktree + "\nBase commit or snapshot: " + options.ChangeSnapshot + "\nOutput path: " + options.Artifact + "\nOutput format: closed schema-version-2 " + role + " JSON for " + policyID + "\n"
		mustWriteCLI(t, filepath.Join(runDir, filepath.FromSlash(templateName)), template)
		prepared, result := validate.PrepareDispatchPrompt(validate.PrepareDispatchPromptOptions{Root: options.Worktree, DispatchFile: filepath.Join(runDir, filepath.FromSlash(templateName)), OutputFile: filepath.Join(runDir, filepath.FromSlash(promptName)), Bindings: []validate.DispatchPromptBinding{{Name: "contextBundle", Path: filepath.Join(runDir, filepath.FromSlash(options.ContextBundle))}}})
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
	if id == "review.prompt-fields" {
		return "checked static-validation=PASS binding and all required prompt fields"
	}
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
	ref := func(name string) validate.EvidenceRef {
		return validate.EvidenceRef{Path: logical(name), SHA256: cliFileHash(t, filepath.Join(restrictedDir, name))}
	}
	approved, designReview := writeCLIDesignReviewClosure(t, worktree, dir, workflowID, snapshot+"-design")
	writeCLIJSON(t, filepath.Join(restrictedDir, "qa-results.json"), map[string]any{
		"owner": "QA", "workflowId": workflowID, "changeSnapshot": snapshot, "stage": "Execution", "status": "COMPLETE", "overallOutcome": "PASS",
		"executions":  []any{map[string]any{"id": "E-001", "outcome": "PASS", "procedure": "Run the approved case", "result": "The case passed"}},
		"caseResults": []any{map[string]any{"caseId": "P1-001", "status": "PASS", "procedures": []string{"E-001"}, "oracle": "The approved behavior is observed"}},
	})
	results := ref("qa-results.json")
	writeCLIJSON(t, filepath.Join(restrictedDir, "qa-case-binding.json"), map[string]any{
		"workflowId": workflowID, "changeSnapshot": snapshot, "approvedCaseSet": approved, "qaOwnedResults": results, "complete": true,
		"bindings": []any{map[string]any{"caseId": "P1-001", "resultPointer": "/caseResults/0", "status": "PASS", "executionRefs": []string{"E-001"}, "procedures": []string{"E-001"}, "oracle": "The approved behavior is observed"}},
	})
	return validate.QAExecutionPayload{
		ApprovedCaseSet: approved, DesignReview: designReview, QAOwnedResults: results, CaseResultBinding: ref("qa-case-binding.json"),
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
	receiptBound := func(stage, artifact, subagent string, write func()) (validate.EvidenceRef, cliReceiptDeps) {
		registration, result := validate.ReceiptRegisterDispatch(withCLIReceiptBundle(t, validate.ReceiptRegisterOptions{Worktree: worktree, RunDir: runDir, Provider: "codex", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: "qa-test-gate", Stage: stage, Artifact: artifact, ContextBundle: logicalName(bundleName)}))
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
	designReceipt, designDeps := receiptBound("Design", caseArtifact, "design-agent", func() {
		mustWriteCLI(t, filepath.Join(restrictedDir, "approved-cases.md"), "# Cases\n\nCase ID: P1-001\n\nOracle: approved behavior\n")
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
	payloadData, _ := json.Marshal(validate.ReviewerPayload{ContextBundle: ref(bundleName), ReviewPolicyID: policy.ID, Checks: checks})
	reviewReceipt, reviewDeps := receiptBound("Design Review", reviewArtifact, "design-review-agent", func() {
		writeCLIJSON(t, filepath.Join(restrictedDir, "design-review.json"), validate.FormalGateEvidence{SchemaVersion: 2, ArtifactRole: "QA_REVIEW", WorkflowID: workflowID, ChangeSnapshot: snapshot, Gate: "qa-test-gate", Stage: "Design Review", Verdict: "PASS", Payload: payloadData})
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
