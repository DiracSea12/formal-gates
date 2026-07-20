package validate

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestComposeContextBundleGeneratesSortedRefs(t *testing.T) {
	root, runDir := composeTestRun(t, "wf-context")
	mustWrite(t, filepath.Join(runDir, "restricted", "z.txt"), "z\n")
	mustWrite(t, filepath.Join(runDir, "restricted", "a.txt"), "a\n")
	options := ComposeContextBundleOptions{
		Root: root, RunDir: runDir, WorkflowID: "wf-context", ChangeSnapshot: "snapshot",
		Output: "restricted/generated/context.json", Inputs: []string{"restricted/z.txt", "restricted/a.txt"},
	}
	ref, result := ComposeContextBundle(options)
	if !result.OK() {
		t.Fatalf("compose context bundle failed: %#v", result.Failures)
	}
	var bundle ContextBundle
	readComposeJSON(t, filepath.Join(runDir, filepath.FromSlash(ref.Path)), &bundle)
	want := []EvidenceRef{testRef(t, runDir, "restricted/a.txt"), testRef(t, runDir, "restricted/z.txt")}
	if bundle.BundleVersion != 1 || bundle.WorkflowID != "wf-context" || bundle.ChangeSnapshot != "snapshot" || !reflect.DeepEqual(bundle.Inputs, want) {
		t.Fatalf("unexpected generated bundle: %#v", bundle)
	}
	if err := validateStandaloneCompositionProof(root, runDir, "context-bundle.v1", "wf-context", "snapshot", ref); err != nil {
		t.Fatal(err)
	}
	if _, result := ComposeContextBundle(options); result.OK() {
		t.Fatal("context composition overwrote existing output")
	}
}

func TestComposeChangedFilesGeneratesGitListAndProof(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, output)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	mustWrite(t, filepath.Join(root, "tracked.txt"), "before\n")
	mustWrite(t, filepath.Join(root, ".gitignore"), "ignored.tmp\n")
	run("add", "tracked.txt", ".gitignore")
	run("commit", "-m", "base")
	mustWrite(t, filepath.Join(root, "tracked.txt"), "after\n")
	mustWrite(t, filepath.Join(root, "new.go"), "package newfile\n")
	mustWrite(t, filepath.Join(root, "not-selected.go"), "package notselected\n")
	mustWrite(t, filepath.Join(root, "ignored.tmp"), "ignored\n")
	runDir := filepath.Join(root, ".claude", "gates", "runs", "wf-changed")
	if err := os.MkdirAll(filepath.Join(runDir, "restricted"), 0o700); err != nil {
		t.Fatal(err)
	}
	ref, result := ComposeChangedFiles(ComposeChangedFilesOptions{
		Root: root, RunDir: runDir, WorkflowID: "wf-changed", ChangeSnapshot: "snapshot",
		BaseRef: "HEAD", HeadRef: "HEAD", IncludeWorkingTree: true, IncludeUntracked: []string{"new.go"}, Output: "restricted/generated/changed.txt",
	})
	if !result.OK() {
		t.Fatalf("compose changed files failed: %#v", result.Failures)
	}
	data, err := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(ref.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new.go\ntracked.txt\n" {
		t.Fatalf("unexpected changed-files output: %q", data)
	}
	if err := validateStandaloneCompositionProof(root, runDir, "changed-files.v1", "wf-changed", "snapshot", ref); err != nil {
		t.Fatal(err)
	}
}

func TestComposeChangedFilesRejectsInvalidExplicitUntrackedPaths(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, output)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	mustWrite(t, filepath.Join(root, ".gitignore"), "ignored.tmp\n")
	mustWrite(t, filepath.Join(root, "tracked.txt"), "tracked\n")
	run("add", ".gitignore", "tracked.txt")
	run("commit", "-m", "base")
	mustWrite(t, filepath.Join(root, "untracked.go"), "package untracked\n")
	mustWrite(t, filepath.Join(root, "ignored.tmp"), "ignored\n")
	mustWrite(t, filepath.Join(root, ".claude", "gates", "runs", "other", "restricted", "artifact.txt"), "run artifact\n")

	tests := []struct {
		name               string
		includeWorkingTree bool
		paths              []string
		want               string
	}{
		{name: "tracked", includeWorkingTree: true, paths: []string{"tracked.txt"}, want: "path is tracked"},
		{name: "ignored", includeWorkingTree: true, paths: []string{"ignored.tmp"}, want: "path is ignored"},
		{name: "missing", includeWorkingTree: true, paths: []string{"missing.go"}, want: "path does not exist"},
		{name: "path traversal", includeWorkingTree: true, paths: []string{"../outside.go"}, want: "path must remain under the worktree"},
		{name: "duplicate", includeWorkingTree: true, paths: []string{"untracked.go", "./untracked.go"}, want: "path must not be repeated"},
		{name: "workflow run artifact", includeWorkingTree: true, paths: []string{".claude/gates/runs/other/restricted/artifact.txt"}, want: "workflow-run artifacts cannot be included"},
		{name: "without working tree", paths: []string{"untracked.go"}, want: "requires --include-working-tree"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			workflowID := "wf-invalid-" + strings.ReplaceAll(tc.name, " ", "-")
			runDir := filepath.Join(root, ".claude", "gates", "runs", workflowID)
			if err := os.MkdirAll(filepath.Join(runDir, "restricted"), 0o700); err != nil {
				t.Fatal(err)
			}
			_, result := ComposeChangedFiles(ComposeChangedFilesOptions{
				Root: root, RunDir: runDir, WorkflowID: workflowID, ChangeSnapshot: "snapshot",
				BaseRef: "HEAD", HeadRef: "HEAD", IncludeWorkingTree: tc.includeWorkingTree,
				IncludeUntracked: tc.paths, Output: "restricted/changed.txt",
			})
			if result.OK() || !strings.Contains(resultSummary(result), tc.want) {
				t.Fatalf("invalid explicit untracked path was accepted: %#v", result.Failures)
			}
			if _, err := os.Lstat(filepath.Join(runDir, "restricted", "changed.txt")); !os.IsNotExist(err) {
				t.Fatalf("rejected composition wrote output: %v", err)
			}
		})
	}
}

func TestComposeChangedFilesWithoutWorkingTreeKeepsRangeOnly(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		command := exec.Command("git", args...)
		command.Dir = root
		if output, err := command.CombinedOutput(); err != nil {
			t.Fatalf("git %v failed: %v\n%s", args, err, output)
		}
	}
	run("init")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	mustWrite(t, filepath.Join(root, "range.txt"), "before\n")
	run("add", "range.txt")
	run("commit", "-m", "base")
	base, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(root, "range.txt"), "after\n")
	run("add", "range.txt")
	run("commit", "-m", "range")
	mustWrite(t, filepath.Join(root, "untracked.go"), "package untracked\n")
	runDir := filepath.Join(root, ".claude", "gates", "runs", "wf-range")
	if err := os.MkdirAll(filepath.Join(runDir, "restricted"), 0o700); err != nil {
		t.Fatal(err)
	}
	ref, result := ComposeChangedFiles(ComposeChangedFilesOptions{
		Root: root, RunDir: runDir, WorkflowID: "wf-range", ChangeSnapshot: "snapshot",
		BaseRef: strings.TrimSpace(string(base)), HeadRef: "HEAD", Output: "restricted/changed.txt",
	})
	if !result.OK() {
		t.Fatalf("compose changed files failed: %#v", result.Failures)
	}
	data, err := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(ref.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "range.txt\n" {
		t.Fatalf("working-tree file leaked without --include-working-tree: %q", data)
	}
}

func TestComposeTransitionChainGeneratesEvidenceRefs(t *testing.T) {
	root, runDir := composeTestRun(t, "wf-transition")
	for _, name := range []string{"changed-1.txt", "verify-1.txt", "repair-1.txt", "changed-2.txt", "verify-2.txt", "repair-2.txt"} {
		mustWrite(t, filepath.Join(runDir, "restricted", name), name+"\n")
	}
	options := ComposeTransitionChainOptions{
		Root: root, RunDir: runDir, WorkflowID: "wf-transition", TargetSnapshot: "target", Output: "restricted/generated/transition.json",
		Hops: []TransitionHopSource{
			{FromSnapshot: "source", ToSnapshot: "middle", ChangedFiles: "restricted/changed-1.txt", Verification: "restricted/verify-1.txt", RepairEvidence: "restricted/repair-1.txt"},
			{FromSnapshot: "middle", ToSnapshot: "target", ChangedFiles: "restricted/changed-2.txt", Verification: "restricted/verify-2.txt", RepairEvidence: "restricted/repair-2.txt"},
		},
	}
	ref, result := ComposeTransitionChain(options)
	if !result.OK() {
		t.Fatalf("compose transition failed: %#v", result.Failures)
	}
	var chain TransitionChain
	readComposeJSON(t, filepath.Join(runDir, filepath.FromSlash(ref.Path)), &chain)
	if chain.SchemaVersion != 2 || chain.WorkflowID != "wf-transition" || chain.TargetSnapshot != "target" || len(chain.Hops) != 2 || chain.Hops[0].ChangedFiles != testRef(t, runDir, "restricted/changed-1.txt") {
		t.Fatalf("unexpected generated transition chain: %#v", chain)
	}
	if err := validateStandaloneCompositionProof(root, runDir, "transition-chain.v1", "wf-transition", "target", ref); err != nil {
		t.Fatal(err)
	}
	bad := options
	bad.Output = "restricted/generated/bad-transition.json"
	bad.Hops[1].FromSnapshot = "gap"
	if _, result := ComposeTransitionChain(bad); result.OK() {
		t.Fatal("non-contiguous transition chain was generated")
	}
	if _, err := os.Lstat(filepath.Join(runDir, "restricted", "generated", "bad-transition.json")); !os.IsNotExist(err) {
		t.Fatalf("failed transition composition left output: %v", err)
	}
}

func TestComposeQAOwnedEvidenceGeneratesStaticResultsAndBinding(t *testing.T) {
	root, runDir := composeTestRun(t, "wf-qa-owned")
	mustWrite(t, filepath.Join(runDir, "restricted", "cases.md"), "# Cases\n\nCase ID: P25-001\n\nCase ID: P25-002\n")
	output, result := ComposeQAOwnedEvidence(ComposeQAOwnedEvidenceOptions{
		Root: root, RunDir: runDir, WorkflowID: "wf-qa-owned", ChangeSnapshot: "snapshot",
		ApprovedCaseSet: "restricted/cases.md", OutputDir: "restricted/generated/qa",
		Cases: []QAExecutionCaseSubmission{
			{Position: 2, Outcome: "PASS", Procedure: "run second case", Observation: "second case passed", OracleResult: "second behavior passed"},
			{Position: 1, Outcome: "PASS", Procedure: "run first case", Observation: "first case passed", OracleResult: "first behavior passed"},
		},
	})
	if !result.OK() {
		t.Fatalf("compose QA-owned evidence failed: %#v", result.Failures)
	}
	var results QAResultsArtifact
	readComposeJSON(t, filepath.Join(runDir, filepath.FromSlash(output.Results.Path)), &results)
	if results.Owner != "QA" || results.WorkflowID != "wf-qa-owned" || results.ChangeSnapshot != "snapshot" || results.Stage != "Execution" || results.Status != "COMPLETE" || results.OverallOutcome != "PASS" {
		t.Fatalf("QA results static envelope was not generated: %#v", results)
	}
	if got := []string{results.CaseResults[0].CaseID, results.CaseResults[1].CaseID}; !reflect.DeepEqual(got, []string{"P25-001", "P25-002"}) {
		t.Fatalf("case catalog did not follow approved case order: %v", got)
	}
	var binding QACaseBindingArtifact
	readComposeJSON(t, filepath.Join(runDir, filepath.FromSlash(output.Binding.Path)), &binding)
	if binding.WorkflowID != "wf-qa-owned" || binding.ChangeSnapshot != "snapshot" || !binding.Complete || binding.QAOwnedResults != output.Results || len(binding.Bindings) != 2 || binding.Bindings[0].ResultPointer != "/caseResults/0" || binding.Bindings[0].ExecutionRefs[0] != "EXEC-001" || results.Executions[1].ID != "EXEC-002" {
		t.Fatalf("QA binding was not mechanically generated: %#v", binding)
	}
	if err := validateCompositionProofOutputs(root, runDir, "qa-owned-evidence.v1", "wf-qa-owned", "snapshot", output.Results, []EvidenceRef{output.Results, output.Binding}); err != nil {
		t.Fatalf("generated QA-owned evidence pair proof is invalid: %v", err)
	}
}

func TestComposeQAOwnedEvidenceDerivesFailOutcome(t *testing.T) {
	root, runDir := composeTestRun(t, "wf-qa-fail")
	mustWrite(t, filepath.Join(runDir, "restricted", "cases.md"), "Case ID: P25-001\n")
	output, result := ComposeQAOwnedEvidence(ComposeQAOwnedEvidenceOptions{
		Root: root, RunDir: runDir, WorkflowID: "wf-qa-fail", ChangeSnapshot: "snapshot",
		ApprovedCaseSet: "restricted/cases.md", OutputDir: "restricted/generated/qa",
		Cases: []QAExecutionCaseSubmission{{Position: 1, Outcome: "FAIL", Procedure: "run case", Observation: "observed mismatch", OracleResult: "expected behavior was absent"}},
	})
	if !result.OK() {
		t.Fatalf("FAIL semantics should produce QA-owned diagnostic evidence: %#v", result.Failures)
	}
	var results QAResultsArtifact
	readComposeJSON(t, filepath.Join(runDir, filepath.FromSlash(output.Results.Path)), &results)
	if results.OverallOutcome != "FAIL" || results.Executions[0].Outcome != "FAIL" || results.CaseResults[0].Status != "FAIL" {
		t.Fatalf("FAIL outcome was not mechanically projected: %#v", results)
	}
}

func TestComposeQAOwnedEvidenceRejectsIncompleteAndInvalidCasesWithoutOutput(t *testing.T) {
	tests := []struct {
		name  string
		cases []QAExecutionCaseSubmission
	}{
		{name: "missing", cases: []QAExecutionCaseSubmission{{Position: 1, Outcome: "PASS", Procedure: "run", Observation: "passed", OracleResult: "matched"}}},
		{name: "duplicate", cases: []QAExecutionCaseSubmission{{Position: 1, Outcome: "PASS", Procedure: "run", Observation: "passed", OracleResult: "matched"}, {Position: 1, Outcome: "PASS", Procedure: "run", Observation: "passed", OracleResult: "matched"}}},
		{name: "out-of-range", cases: []QAExecutionCaseSubmission{{Position: 1, Outcome: "PASS", Procedure: "run", Observation: "passed", OracleResult: "matched"}, {Position: 3, Outcome: "PASS", Procedure: "run", Observation: "passed", OracleResult: "matched"}}},
		{name: "pending", cases: []QAExecutionCaseSubmission{{Position: 1, Outcome: "PASS", Procedure: "run", Observation: "passed", OracleResult: "matched"}, {Position: 2, Outcome: "PENDING", Procedure: "run", Observation: "pending", OracleResult: "matched"}}},
		{name: "empty", cases: []QAExecutionCaseSubmission{{Position: 1, Outcome: "PASS", Procedure: "run", Observation: "passed", OracleResult: "matched"}, {Position: 2, Outcome: "PASS", Procedure: "", Observation: "passed", OracleResult: "matched"}}},
		{name: "illegal-enum", cases: []QAExecutionCaseSubmission{{Position: 1, Outcome: "PASS", Procedure: "run", Observation: "passed", OracleResult: "matched"}, {Position: 2, Outcome: "UNKNOWN", Procedure: "run", Observation: "passed", OracleResult: "matched"}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, runDir := composeTestRun(t, "wf-qa-invalid-"+test.name)
			caseSetPath := filepath.Join(runDir, "restricted", "cases.md")
			caseSetBytes := []byte("Case ID: P25-001\nCase ID: P25-002\n")
			mustWrite(t, caseSetPath, string(caseSetBytes))
			outputDir := "restricted/generated/qa"
			_, result := ComposeQAOwnedEvidence(ComposeQAOwnedEvidenceOptions{
				Root: root, RunDir: runDir, WorkflowID: "wf-qa-invalid-" + test.name, ChangeSnapshot: "snapshot",
				ApprovedCaseSet: "restricted/cases.md", OutputDir: outputDir, Cases: test.cases,
			})
			if result.OK() {
				t.Fatal("invalid positioned QA submission was accepted")
			}
			for _, name := range []string{"qa-results.json", "case-result-binding.json"} {
				if _, err := os.Lstat(filepath.Join(runDir, filepath.FromSlash(outputDir), name)); !os.IsNotExist(err) {
					t.Fatalf("rejected QA composition left %s: %v", name, err)
				}
			}
			proofDir := receiptProofDir(root, runDir, "compositions")
			if entries, err := os.ReadDir(proofDir); err == nil && len(entries) != 0 {
				t.Fatalf("rejected QA composition left proofs: %v", entries)
			} else if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if after, err := os.ReadFile(caseSetPath); err != nil || !reflect.DeepEqual(after, caseSetBytes) {
				t.Fatalf("rejected QA composition changed approved case set: err=%v bytes=%q", err, after)
			}
		})
	}
}
