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
	"unicode"

	"formal-gates/internal/validate"
)

func TestRunArtifactComposeRequirements(t *testing.T) {
	root := t.TempDir()
	workflowID, snapshot := "wf-cli-compose-requirements", "snapshot-1"
	runDir := filepath.Join(root, ".claude", "gates", "runs", workflowID)

	var stdout, stderr bytes.Buffer
	code := Run("formal-gates", validCLIRequirementsArgs(root, workflowID, snapshot, "restricted/generated/requirements"), IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("compose-requirements failed: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var output validate.ComposeRequirementsOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("invalid compose output %q: %v", stdout.String(), err)
	}
	if output.Alignment.Path != "restricted/generated/requirements/alignment.json" || output.Decision.Path != "restricted/generated/requirements/decision.json" || output.Requirements.Path != "restricted/generated/requirements/requirements.json" {
		t.Fatalf("unexpected generated references: %+v", output)
	}
	data, err := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(output.Requirements.Path)))
	if err != nil {
		t.Fatal(err)
	}
	var envelope validate.FormalGateEvidence
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.ArtifactRole != "REQUIREMENTS_PASS" || envelope.WorkflowID != workflowID || envelope.ChangeSnapshot != snapshot || envelope.Gate != "requirements-clarification-gate" || envelope.Verdict != "PASS" {
		t.Fatalf("CLI did not generate the requirements envelope: %+v", envelope)
	}
	var payload validate.RequirementsPayload
	if err := json.Unmarshal(envelope.Payload, &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RequirementSource != "openspec/changes/phase-2-5" || len(payload.CoveredTargets) != 1 || payload.CoveredTargets[0] != "openspec/changes/phase-2-5/design.md" {
		t.Fatalf("CLI did not project static requirement bindings: %+v", payload)
	}
	alignmentData, err := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(output.Alignment.Path)))
	if err != nil {
		t.Fatal(err)
	}
	var alignment validate.AlignmentArtifact
	if err := json.Unmarshal(alignmentData, &alignment); err != nil {
		t.Fatal(err)
	}
	if len(alignment.Items) != 2 || alignment.Items[0].ID != "RQ-064" || alignment.Items[1].ID != "RQ-065" {
		t.Fatalf("CLI did not project the alignment id: %+v", alignment.Items)
	}
	stdout.Reset()
	stderr.Reset()
	code = Run("formal-gates", []string{
		"artifact", "validate", "--root", root, "--file", filepath.Join(runDir, filepath.FromSlash(output.Requirements.Path)),
		"--gate", "requirements-clarification-gate", "--workflow-id", workflowID, "--change-snapshot", snapshot,
	}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("generated requirements artifact was rejected by public validation: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunArtifactComposeRequirementsRequiresStaticBindings(t *testing.T) {
	root := t.TempDir()
	workflowID := "wf-cli-compose-requirements-missing-bindings"
	runDir := filepath.Join(root, ".claude", "gates", "runs", workflowID)

	var stdout bytes.Buffer
	code := Run("formal-gates", []string{
		"artifact", "compose-requirements", "--root", root, "--workflow-id", workflowID, "--change-snapshot", "snapshot-1",
		"--output-dir", "restricted/generated/requirements",
	}, IO{Stdout: &stdout})
	if code == 0 || !bytes.Contains(stdout.Bytes(), []byte("--requirement-source")) {
		t.Fatalf("missing static bindings were accepted: code=%d stdout=%q", code, stdout.String())
	}
	if _, err := os.Lstat(filepath.Join(runDir, "restricted", "generated", "requirements")); !os.IsNotExist(err) {
		t.Fatalf("rejected composition created an output directory: %v", err)
	}
}

func TestRunArtifactComposeRequirementsRejectsLegacyJSONAndPositionErrors(t *testing.T) {
	tests := []struct {
		name string
		args func(root, workflowID string) []string
	}{
		{name: "legacy-semantic-input", args: func(root, workflowID string) []string {
			return []string{"artifact", "compose-requirements", "--root", root, "--workflow-id", workflowID, "--change-snapshot", "snapshot", "--semantic-input", "restricted/semantic.json", "--output-dir", "restricted/generated/requirements"}
		}},
		{name: "missing-dimension", args: func(root, workflowID string) []string {
			args := validCLIRequirementsArgs(root, workflowID, "snapshot", "restricted/generated/requirements")
			return args[:len(args)-14]
		}},
		{name: "duplicate-dimension", args: func(root, workflowID string) []string {
			args := validCLIRequirementsArgs(root, workflowID, "snapshot", "restricted/generated/requirements")
			setNthFlagValue(args, "--dimension", 13, "12")
			return args
		}},
		{name: "orphan-dimension-reference", args: func(root, workflowID string) []string {
			args := validCLIRequirementsArgs(root, workflowID, "snapshot", "restricted/generated/requirements")
			return append(args, "--dimension-ref", "14", "--dimension-ref-item", "1")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			workflowID := "wf-cli-requirements-" + test.name
			var stdout, stderr bytes.Buffer
			if code := Run("formal-gates", test.args(root, workflowID), IO{Stdout: &stdout, Stderr: &stderr}); code == 0 {
				t.Fatalf("invalid requirements input passed: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			if _, err := os.Lstat(filepath.Join(root, ".claude", "gates", "runs", workflowID, "restricted", "generated", "requirements")); !os.IsNotExist(err) {
				t.Fatalf("rejected requirements command created output: %v", err)
			}
		})
	}
}

func TestRunArtifactComposeRequirementsRequiresExplicitDroppedApproval(t *testing.T) {
	root := t.TempDir()
	workflowID := "wf-cli-requirements-dropped-approval"
	runDir := filepath.Join(root, ".claude", "gates", "runs", workflowID)
	previous := validate.AlignmentArtifact{
		SchemaVersion: 2, WorkflowID: workflowID, ChangeSnapshot: "snapshot-0",
		Items: []validate.AlignmentItem{{ID: "RQ-063", RequirementOrQuestion: "old", Source: "user", WhyItMatters: "old", Status: "WITHDRAWN", UserAnswer: "withdrawn", DownstreamEffect: "remove", DocumentImpact: "openspec/changes/phase-2-5/design.md", EvidenceNeeded: "continuity"}, {ID: "RQ-064", RequirementOrQuestion: "current", Source: "user", WhyItMatters: "current", Status: "CONFIRMED", UserAnswer: "approved", DownstreamEffect: "keep", DocumentImpact: "openspec/changes/phase-2-5/design.md", EvidenceNeeded: "coverage"}, {ID: "RQ-065", RequirementOrQuestion: "current", Source: "user", WhyItMatters: "current", Status: "CONFIRMED", UserAnswer: "approved", DownstreamEffect: "keep", DocumentImpact: "openspec/changes/phase-2-5/design.md", EvidenceNeeded: "coverage"}},
	}
	previousBytes, err := json.Marshal(previous)
	if err != nil {
		t.Fatal(err)
	}
	mustWriteCLI(t, filepath.Join(runDir, "restricted", "previous-alignment.json"), string(append(previousBytes, '\n')))
	missingApprovalArgs := append(validCLIRequirementsArgs(root, workflowID, "snapshot", "restricted/generated/missing"), "--previous-alignment", "restricted/previous-alignment.json")
	var stdout, stderr bytes.Buffer
	if code := Run("formal-gates", missingApprovalArgs, IO{Stdout: &stdout, Stderr: &stderr}); code == 0 {
		t.Fatalf("missing dropped approval was accepted: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	approvedArgs := append(validCLIRequirementsArgs(root, workflowID, "snapshot", "restricted/generated/approved"), "--previous-alignment", "restricted/previous-alignment.json", "--approved-dropped-id", "RQ-063")
	stdout.Reset()
	stderr.Reset()
	if code := Run("formal-gates", approvedArgs, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
		t.Fatalf("explicit dropped approval was rejected: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
}

func TestRunArtifactComposeQAExecution(t *testing.T) {
	root := t.TempDir()
	workflowID, snapshot := "wf-cli-compose-qa", "snapshot-qa"
	runDir := filepath.Join(root, ".claude", "gates", "runs", workflowID)
	inputs := writeCLIQAEvidence(t, runDir, workflowID, snapshot)

	var stdout, stderr bytes.Buffer
	code := Run("formal-gates", []string{
		"artifact", "compose-qa-execution", "--root", root, "--workflow-id", workflowID, "--change-snapshot", snapshot,
		"--output", "restricted/generated/qa-execution.json",
		"--approved-case-set", inputs.ApprovedCaseSet.Path,
		"--design-review", inputs.DesignReview.Path,
		"--qa-owned-results", inputs.QAOwnedResults.Path,
		"--case-result-binding", inputs.CaseResultBinding.Path,
		"--changed-files", inputs.ChangedFiles.Path,
		"--verification", inputs.Verification.Path,
	}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("compose-qa-execution failed: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var output validate.EvidenceRef
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatalf("invalid compose output %q: %v", stdout.String(), err)
	}
	if output.Path != "restricted/generated/qa-execution.json" || output.SHA256 == "" {
		t.Fatalf("unexpected generated reference: %+v", output)
	}
	data, err := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(output.Path)))
	if err != nil {
		t.Fatal(err)
	}
	var envelope validate.FormalGateEvidence
	if err := json.Unmarshal(data, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.ArtifactRole != "QA_EXECUTION" || envelope.WorkflowID != workflowID || envelope.ChangeSnapshot != snapshot || envelope.Gate != "qa-test-gate" || envelope.Stage != "Execution" || envelope.Verdict != "PASS" {
		t.Fatalf("CLI did not generate the QA Execution envelope: %+v", envelope)
	}
}

func TestRunArtifactComposeHelp(t *testing.T) {
	for _, verb := range []string{"compose-requirements", "compose-qa-execution", "compose-context-bundle", "compose-transition-chain", "compose-qa-owned-evidence", "compose-changed-files"} {
		var stdout bytes.Buffer
		if code := Run("formal-gates", []string{"artifact", verb, "--help"}, IO{Stdout: &stdout}); code != 0 {
			t.Fatalf("%s help failed: code=%d stdout=%q", verb, code, stdout.String())
		}
	}
}

func TestRunArtifactComposeRejectsPositionalArguments(t *testing.T) {
	t.Run("context bundle", func(t *testing.T) {
		root := t.TempDir()
		workflowID := "wf-cli-context-positional"
		runDir := filepath.Join(root, ".claude", "gates", "runs", workflowID)
		mustWriteCLI(t, filepath.Join(runDir, "restricted", "input.txt"), "input\n")
		var stdout, stderr bytes.Buffer
		code := Run("formal-gates", []string{"artifact", "compose-context-bundle", "--root", root, "--workflow-id", workflowID, "--change-snapshot", "snapshot", "--output", "restricted/generated/context.json", "--input", "restricted/input.txt", "unexpected"}, IO{Stdout: &stdout, Stderr: &stderr})
		if code == 0 || !strings.Contains(stderr.String(), "does not accept positional arguments") {
			t.Fatalf("context positional argument was accepted: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	})
	t.Run("transition chain", func(t *testing.T) {
		root := t.TempDir()
		workflowID := "wf-cli-transition-positional"
		runDir := filepath.Join(root, ".claude", "gates", "runs", workflowID)
		for _, name := range []string{"changed.txt", "verification.txt", "repair.txt"} {
			mustWriteCLI(t, filepath.Join(runDir, "restricted", name), name+"\n")
		}
		var stdout, stderr bytes.Buffer
		code := Run("formal-gates", []string{"artifact", "compose-transition-chain", "--root", root, "--workflow-id", workflowID, "--target-snapshot", "target", "--output", "restricted/generated/chain.json", "--hop-from", "source", "--hop-to", "target", "--hop-changed-files", "restricted/changed.txt", "--hop-verification", "restricted/verification.txt", "--hop-repair", "restricted/repair.txt", "unexpected"}, IO{Stdout: &stdout, Stderr: &stderr})
		if code == 0 || !strings.Contains(stderr.String(), "does not accept positional arguments") {
			t.Fatalf("transition positional argument was accepted: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	})
	t.Run("changed files", func(t *testing.T) {
		root := initCLIGitRepo(t)
		workflowID := "wf-cli-changed-files-positional"
		var stdout, stderr bytes.Buffer
		code := Run("formal-gates", []string{"artifact", "compose-changed-files", "--root", root, "--workflow-id", workflowID, "--change-snapshot", "snapshot", "--base-ref", "HEAD", "--output", "restricted/changed-files.txt", "unexpected"}, IO{Stdout: &stdout, Stderr: &stderr})
		if code == 0 || !strings.Contains(stderr.String(), "does not accept positional arguments") {
			t.Fatalf("changed-files positional argument was accepted: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
		}
	})
}

func TestRunArtifactComposeChangedFilesIncludesOnlyExplicitUntracked(t *testing.T) {
	root := initCLIGitRepo(t)
	mustWriteCLI(t, filepath.Join(root, ".gitignore"), "ignored.tmp\n")
	command := exec.Command("git", "add", ".gitignore")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add failed: %v\n%s", err, output)
	}
	command = exec.Command("git", "commit", "-m", "ignore test output")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git commit failed: %v\n%s", err, output)
	}
	mustWriteCLI(t, filepath.Join(root, "README.md"), "edited\n")
	mustWriteCLI(t, filepath.Join(root, "feature.go"), "package feature\n")
	mustWriteCLI(t, filepath.Join(root, "docs", "selected.md"), "selected\n")
	mustWriteCLI(t, filepath.Join(root, "not-selected.go"), "package notselected\n")
	mustWriteCLI(t, filepath.Join(root, "ignored.tmp"), "ignored\n")

	workflowID := "wf-cli-changed-files"
	runDir := filepath.Join(root, ".claude", "gates", "runs", workflowID)
	if err := os.MkdirAll(filepath.Join(runDir, "restricted"), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run("formal-gates", []string{
		"artifact", "compose-changed-files", "--root", root, "--workflow-id", workflowID,
		"--change-snapshot", "snapshot", "--base-ref", "HEAD", "--head-ref", "HEAD",
		"--include-working-tree", "--include-untracked", "feature.go", "--include-untracked", "docs/selected.md",
		"--output", "restricted/changed-files.txt",
	}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("compose changed-files failed: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var ref validate.EvidenceRef
	if err := json.Unmarshal(stdout.Bytes(), &ref); err != nil {
		t.Fatalf("invalid changed-files reference: %v\n%s", err, stdout.String())
	}
	outputPath := filepath.Join(runDir, filepath.FromSlash(ref.Path))
	data, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "README.md\ndocs/selected.md\nfeature.go\n" {
		t.Fatalf("unexpected changed-files output: %q", data)
	}
	if ref.SHA256 != cliFileHash(t, outputPath) {
		t.Fatalf("changed-files hash does not match output: %+v", ref)
	}
	proofs, err := filepath.Glob(filepath.Join(runDir, "restricted", "proofs", "compositions", "*.json"))
	if err != nil || len(proofs) != 1 {
		t.Fatalf("expected one composition proof, paths=%v err=%v", proofs, err)
	}
	var proof validate.CompositionProof
	proofData, err := os.ReadFile(proofs[0])
	if err != nil || json.Unmarshal(proofData, &proof) != nil {
		t.Fatalf("cannot read composition proof: %v", err)
	}
	if proof.Composer != "changed-files.v1" || proof.WorkflowID != workflowID || proof.ChangeSnapshot != "snapshot" || !reflect.DeepEqual(proof.Outputs, []validate.EvidenceRef{ref}) {
		t.Fatalf("composition proof does not match output: %+v", proof)
	}
}

func TestRunArtifactComposeChangedFilesExcludesStagedRunArtifacts(t *testing.T) {
	root := initCLIGitRepo(t)
	source := filepath.Join(root, "source.go")
	mustWriteCLI(t, source, "package source\n")
	stagedRunArtifact := filepath.Join(root, ".claude", "gates", "runs", "staged", "restricted", "artifact.json")
	mustWriteCLI(t, stagedRunArtifact, "run artifact\n")
	command := exec.Command("git", "add", "-A")
	command.Dir = root
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git add -A failed: %v\n%s", err, output)
	}

	workflowID := "wf-cli-changed-files-staged-run"
	if err := os.MkdirAll(filepath.Join(root, ".claude", "gates", "runs", workflowID, "restricted"), 0o700); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	code := Run("formal-gates", []string{
		"artifact", "compose-changed-files", "--root", root, "--workflow-id", workflowID,
		"--change-snapshot", "snapshot", "--base-ref", "HEAD", "--head-ref", "HEAD",
		"--include-working-tree", "--output", "restricted/changed-files.txt",
	}, IO{Stdout: &stdout, Stderr: &stderr})
	if code != 0 {
		t.Fatalf("compose changed-files with staged run artifact failed: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	var ref validate.EvidenceRef
	if err := json.Unmarshal(stdout.Bytes(), &ref); err != nil {
		t.Fatalf("invalid changed-files reference: %v\n%s", err, stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(root, ".claude", "gates", "runs", workflowID, filepath.FromSlash(ref.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "source.go\n" {
		t.Fatalf("staged run artifact leaked into changed-files output: %q", data)
	}
}

func TestRunHandoffComposeValidateAndExecuteGeneratedComplexityCommand(t *testing.T) {
	root := initCLIGitRepoWithSpaces(t)
	workflowID := "wf-cli-handoff-command"
	runDir := filepath.Join(".claude", "gates", "runs", "custom-run")
	output := filepath.Join(runDir, "restricted", "handoff.md")
	composeArgs := []string{
		"handoff", "compose", "--root", root, "--run-dir", runDir,
		"--workflow-id", workflowID, "--change-snapshot", "snapshot",
		"--output", "restricted/handoff.md", "--requirement-target", "openspec/changes/example",
		"--verification-requirements", "go test ./...", "--budget-stop-triggers", "stop on growth",
		"--budget-expansion-approval-path", "agents/anti-complexity-review.md", "--forbidden-context", "prior findings",
		"--formal-flow-mode", "none", "--trigger-source", "user", "--task-type", "small-feature",
		"--max-net", "250", "--max-new-prod-files", "2", "--max-prod-insertions", "300",
	}
	var stdout, stderr bytes.Buffer
	if code := Run("formal-gates", composeArgs, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
		t.Fatalf("handoff compose failed: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := Run("formal-gates", []string{
		"handoff", "validate", "--root", root, "--file", output,
		"--workflow-id", workflowID, "--change-snapshot", "snapshot",
	}, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
		t.Fatalf("generated handoff did not validate: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	data, err := os.ReadFile(filepath.Join(root, output))
	if err != nil {
		t.Fatal(err)
	}
	var generated string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "Complexity check command: ") {
			generated = strings.TrimPrefix(line, "Complexity check command: ")
			break
		}
	}
	fields, err := parseGeneratedShellCommand(generated)
	if err != nil {
		t.Fatalf("invalid generated complexity command: %q: %v", generated, err)
	}
	if len(fields) < 3 || fields[0] != "formal-gates" {
		t.Fatalf("invalid generated complexity command: %q", generated)
	}
	binary := filepath.Join(t.TempDir(), "formal-gates")
	build := exec.Command("go", "build", "-o", binary, "./cmd/formal-gates")
	build.Dir = repoRoot(t)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build formal-gates failed: %v\n%s", err, output)
	}
	complexity := exec.Command(binary, fields[1:]...)
	if got := fields[fieldsIndex(t, fields, "--worktree")+1]; got != root {
		t.Fatalf("generated command split worktree path: got %q want %q; command=%q", got, root, generated)
	}
	if output, err := complexity.CombinedOutput(); err != nil {
		t.Fatalf("generated complexity command failed: %v command=%q\n%s", err, generated, output)
	}
}

func initCLIGitRepoWithSpaces(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "repo with spaces")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{{"init"}, {"config", "user.email", "test@example.com"}, {"config", "user.name", "Test User"}} {
		command := exec.Command("git", args...)
		command.Dir = dir
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, output)
		}
	}
	mustWriteCLI(t, filepath.Join(dir, "README.md"), "initial\n")
	for _, args := range [][]string{{"add", "README.md"}, {"commit", "-m", "initial"}} {
		command := exec.Command("git", args...)
		command.Dir = dir
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, output)
		}
	}
	return dir
}

func parseGeneratedShellCommand(command string) ([]string, error) {
	var fields []string
	var field strings.Builder
	inQuotes := false
	escaped := false
	flush := func() {
		if field.Len() > 0 {
			fields = append(fields, field.String())
			field.Reset()
		}
	}
	for _, r := range command {
		if escaped {
			field.WriteRune(r)
			escaped = false
			continue
		}
		if inQuotes && r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			inQuotes = !inQuotes
			continue
		}
		if !inQuotes && unicode.IsSpace(r) {
			flush()
			continue
		}
		field.WriteRune(r)
	}
	if escaped || inQuotes {
		return nil, fmt.Errorf("unterminated quoted argument")
	}
	flush()
	return fields, nil
}

func fieldsIndex(t *testing.T, fields []string, value string) int {
	t.Helper()
	for i, field := range fields {
		if field == value {
			if i+1 >= len(fields) {
				t.Fatalf("missing value after %s in %v", value, fields)
			}
			return i
		}
	}
	t.Fatalf("missing %s in %v", value, fields)
	return -1
}

func TestRunHandoffComposeRejectsTaskTypeWithoutOutputOrProof(t *testing.T) {
	root := initCLIGitRepo(t)
	workflowID := "wf-cli-handoff-invalid-task"
	var stdout, stderr bytes.Buffer
	code := Run("formal-gates", []string{
		"handoff", "compose", "--root", root, "--workflow-id", workflowID, "--change-snapshot", "snapshot",
		"--output", "restricted/handoff.md", "--requirement-target", "openspec/changes/example",
		"--verification-requirements", "go test ./...", "--budget-stop-triggers", "stop on growth",
		"--budget-expansion-approval-path", "agents/anti-complexity-review.md", "--forbidden-context", "prior findings",
		"--formal-flow-mode", "none", "--trigger-source", "user", "--task-type", "code-implementation",
		"--max-net", "250", "--max-new-prod-files", "2", "--max-prod-insertions", "300",
	}, IO{Stdout: &stdout, Stderr: &stderr})
	if code == 0 || !strings.Contains(stdout.String(), "--task-type must be one of") {
		t.Fatalf("invalid task type was not rejected: code=%d stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	output := filepath.Join(root, ".claude", "gates", "runs", workflowID, "restricted", "handoff.md")
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatalf("rejected handoff left output: %v", err)
	}
	proofs, err := filepath.Glob(filepath.Join(root, ".claude", "gates", "runs", workflowID, "restricted", "proofs", "compositions", "*.json"))
	if err != nil || len(proofs) != 0 {
		t.Fatalf("rejected handoff left composition proof: paths=%v err=%v", proofs, err)
	}
}

func TestRunArtifactComposeStaticIntermediateEvidence(t *testing.T) {
	root := t.TempDir()
	workflowID, snapshot := "wf-cli-static", "target"
	runDir := filepath.Join(root, ".claude", "gates", "runs", workflowID)
	for _, name := range []string{"requirement.md", "changed-1.txt", "verification-1.txt", "repair-1.txt", "changed-2.txt", "verification-2.txt", "repair-2.txt"} {
		mustWriteCLI(t, filepath.Join(runDir, "restricted", name), name+"\n")
	}
	run := func(args ...string) string {
		var stdout, stderr bytes.Buffer
		if code := Run("formal-gates", args, IO{Stdout: &stdout, Stderr: &stderr}); code != 0 {
			t.Fatalf("command failed: args=%v stdout=%q stderr=%q", args, stdout.String(), stderr.String())
		}
		return stdout.String()
	}
	bundleOut := run(
		"artifact", "compose-context-bundle", "--root", root, "--workflow-id", workflowID, "--change-snapshot", snapshot,
		"--output", "restricted/generated/context.json", "--input", "restricted/requirement.md",
	)
	var bundleRef validate.EvidenceRef
	if err := json.Unmarshal([]byte(bundleOut), &bundleRef); err != nil || bundleRef.Path != "restricted/generated/context.json" {
		t.Fatalf("unexpected context composer output: %q err=%v", bundleOut, err)
	}
	chainOut := run(
		"artifact", "compose-transition-chain", "--root", root, "--workflow-id", workflowID, "--target-snapshot", snapshot,
		"--output", "restricted/generated/chain.json",
		"--hop-from", "source", "--hop-to", "middle", "--hop-changed-files", "restricted/changed-1.txt", "--hop-verification", "restricted/verification-1.txt", "--hop-repair", "restricted/repair-1.txt",
		"--hop-from", "middle", "--hop-to", "target", "--hop-changed-files", "restricted/changed-2.txt", "--hop-verification", "restricted/verification-2.txt", "--hop-repair", "restricted/repair-2.txt",
	)
	var chainRef validate.EvidenceRef
	if err := json.Unmarshal([]byte(chainOut), &chainRef); err != nil || chainRef.Path != "restricted/generated/chain.json" {
		t.Fatalf("unexpected transition composer output: %q err=%v", chainOut, err)
	}

	mustWriteCLI(t, filepath.Join(runDir, "restricted", "cases.md"), "Case ID: P25-001\n")
	qaOut := run(
		"artifact", "compose-qa-owned-evidence", "--root", root, "--workflow-id", workflowID, "--change-snapshot", snapshot,
		"--approved-case-set", "restricted/cases.md", "--output-dir", "restricted/generated/qa",
		"--case", "1", "--outcome", "PASS", "--procedure", "run test", "--observation", "passed", "--oracle-result", "approved behavior observed",
	)
	var qaRefs validate.ComposeQAOwnedEvidenceOutput
	if err := json.Unmarshal([]byte(qaOut), &qaRefs); err != nil || qaRefs.Results.Path == "" || qaRefs.Binding.Path == "" {
		t.Fatalf("unexpected QA-owned composer output: %q err=%v", qaOut, err)
	}
}

func TestRunArtifactComposeTransitionChainRejectsLegacyAndMismatchedInputsAtomically(t *testing.T) {
	root := t.TempDir()
	workflowID := "wf-cli-transition-reject"
	runDir := filepath.Join(root, ".claude", "gates", "runs", workflowID)
	for _, name := range []string{"changed.txt", "verification.txt", "repair.txt"} {
		mustWriteCLI(t, filepath.Join(runDir, "restricted", name), name+"\n")
	}
	base := []string{"artifact", "compose-transition-chain", "--root", root, "--workflow-id", workflowID, "--target-snapshot", "target", "--output", "restricted/generated/chain.json"}
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "legacy hop DSL", args: []string{"--hop", "from=source,to=target,changed=restricted/changed.txt,verification=restricted/verification.txt,repair=restricted/repair.txt"}},
		{name: "mismatched scalar counts", args: []string{"--hop-from", "source", "--hop-to", "target", "--hop-changed-files", "restricted/changed.txt", "--hop-verification", "restricted/verification.txt"}},
		{name: "duplicate evidence path", args: []string{"--hop-from", "source", "--hop-to", "target", "--hop-changed-files", "restricted/changed.txt", "--hop-verification", "restricted/changed.txt", "--hop-repair", "restricted/repair.txt"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run("formal-gates", append(append([]string{}, base...), test.args...), IO{Stdout: &stdout, Stderr: &stderr}); code == 0 {
				t.Fatalf("invalid transition inputs passed: stdout=%q stderr=%q", stdout.String(), stderr.String())
			}
			if _, err := os.Lstat(filepath.Join(runDir, "restricted", "generated", "chain.json")); !os.IsNotExist(err) {
				t.Fatalf("rejected transition input wrote output: %v", err)
			}
			proofs, err := filepath.Glob(filepath.Join(runDir, "restricted", "proofs", "compositions", "*.json"))
			if err != nil || len(proofs) != 0 {
				t.Fatalf("rejected transition input wrote proof: paths=%v err=%v", proofs, err)
			}
		})
	}
}

func TestRunArtifactComposeQAOwnedEvidenceRejectsMissingAndDuplicateCases(t *testing.T) {
	for _, test := range []struct {
		name      string
		positions []string
	}{
		{name: "missing", positions: []string{"1"}},
		{name: "duplicate", positions: []string{"1", "1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			workflowID := "wf-cli-qa-" + test.name
			runDir := filepath.Join(root, ".claude", "gates", "runs", workflowID)
			mustWriteCLI(t, filepath.Join(runDir, "restricted", "cases.md"), "Case ID: P25-001\nCase ID: P25-002\n")
			args := []string{"artifact", "compose-qa-owned-evidence", "--root", root, "--workflow-id", workflowID, "--change-snapshot", "snapshot", "--approved-case-set", "restricted/cases.md", "--output-dir", "restricted/generated/qa"}
			for _, position := range test.positions {
				args = append(args, "--case", position, "--outcome", "PASS", "--procedure", "run", "--observation", "passed", "--oracle-result", "matched")
			}
			var stdout bytes.Buffer
			if code := Run("formal-gates", args, IO{Stdout: &stdout}); code == 0 {
				t.Fatalf("invalid QA case positions passed: %q", stdout.String())
			}
			if _, err := os.Lstat(filepath.Join(runDir, "restricted", "generated", "qa")); !os.IsNotExist(err) {
				t.Fatalf("rejected QA command created output: %v", err)
			}
		})
	}
}

func TestRunArtifactComposeQAOwnedEvidenceRejectsLegacySemanticJSON(t *testing.T) {
	root := t.TempDir()
	workflowID := "wf-cli-qa-legacy-json"
	runDir := filepath.Join(root, ".claude", "gates", "runs", workflowID)
	mustWriteCLI(t, filepath.Join(runDir, "restricted", "cases.md"), "Case ID: P25-001\n")
	var stdout, stderr bytes.Buffer
	code := Run("formal-gates", []string{
		"artifact", "compose-qa-owned-evidence", "--root", root, "--workflow-id", workflowID, "--change-snapshot", "snapshot",
		"--approved-case-set", "restricted/cases.md", "--semantic-input", "restricted/qa.json", "--output-dir", "restricted/generated/qa",
	}, IO{Stdout: &stdout, Stderr: &stderr})
	if code == 0 {
		t.Fatalf("legacy QA semantic JSON input passed: stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
	if _, err := os.Lstat(filepath.Join(runDir, "restricted", "generated", "qa")); !os.IsNotExist(err) {
		t.Fatalf("rejected legacy QA input created output: %v", err)
	}
}

func validCLIRequirementsArgs(root, workflowID, snapshot, outputDir string) []string {
	args := []string{
		"artifact", "compose-requirements", "--root", root, "--workflow-id", workflowID, "--change-snapshot", snapshot,
		"--output-dir", outputDir, "--requirement-source", "openspec/changes/phase-2-5",
		"--alignment-id", "RQ-064", "--alignment-id", "RQ-065",
		"--covered-target", "openspec/changes/phase-2-5/design.md", "--user-original", "all static content must be script generated",
		"--coverage-scan", "PASS", "--scope-status", "PASS", "--scope-message", "scope preserved",
		"--task-status", "PASS", "--task-message", "task proof present",
	}
	items := [][]string{
		{"Generate static formal fields", "user", "prevents omissions", "CONFIRMED", "approved", "compose with CLI", "openspec/changes/phase-2-5/design.md", "direct composition tests"},
		{"Submit only scalar semantics", "user", "prevents formatting errors", "CONFIRMED", "approved", "use positions", "openspec/changes/phase-2-5/tasks.md", "public CLI tests"},
	}
	for index, values := range items {
		args = append(args, "--alignment", fmt.Sprint(index+1))
		for _, value := range values {
			args = append(args, "--alignment-value", value)
		}
	}
	for position := 1; position <= 13; position++ {
		value := fmt.Sprint(position)
		args = append(args, "--dimension", value, "--dimension-status", "COVERED", "--dimension-message", "confirmed coverage")
		args = append(args, "--dimension-ref", value, "--dimension-ref-item", "1", "--dimension-ref", value, "--dimension-ref-item", "2")
	}
	return args
}

func setNthFlagValue(args []string, flag string, occurrence int, value string) {
	seen := 0
	for index := 0; index+1 < len(args); index++ {
		if args[index] != flag {
			continue
		}
		seen++
		if seen == occurrence {
			args[index+1] = value
			return
		}
	}
}
