package validate

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestComposeChangedFilesUsesExplicitSortedPaths(t *testing.T) {
	root := t.TempDir()
	runDir := filepath.Join(root, ".gates", "runs", "wf")
	ref, result := ComposeChangedFiles(ComposeChangedFilesOptions{Root: root, RunDir: runDir, WorkflowID: "wf", ChangeSnapshot: "git:base..target", Paths: []string{"old.txt", "new.txt", "old.txt"}, Output: "restricted/changed.txt"})
	if !result.OK() {
		t.Fatalf("changed-files composition failed: %#v", result.Failures)
	}
	data, err := os.ReadFile(filepath.Join(runDir, filepath.FromSlash(ref.Path)))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new.txt\nold.txt\n" {
		t.Fatalf("unexpected explicit path list: %q", data)
	}
}

func TestComposeChangedFilesRejectsEveryUnsafeRepositoryPathClassWithoutOutput(t *testing.T) {
	for name, path := range map[string]string{
		"drive absolute":  "C:/repo/file.go",
		"drive relative":  "C:file.go",
		"scheme":          "https://example.test/file.go",
		"absolute":        "/repo/file.go",
		"UNC":             `\\server\share\file.go`,
		"backslash":       `internal\file.go`,
		"parent":          "../file.go",
		"embedded parent": "internal/../file.go",
		"workflow lower":  ".gates/state.json",
		"workflow case":   ".GaTeS/state.json",
		"control":         "file\nname.go",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			runDir := filepath.Join(root, ".gates", "runs", "wf")
			output := filepath.Join(runDir, "restricted", "changed.txt")
			_, result := ComposeChangedFiles(ComposeChangedFilesOptions{Root: root, RunDir: runDir, WorkflowID: "wf", ChangeSnapshot: "snap", Paths: []string{path}, Output: "restricted/changed.txt"})
			if result.OK() {
				t.Fatalf("unsafe path was accepted: %q", path)
			}
			if _, err := os.Lstat(output); !os.IsNotExist(err) {
				t.Fatalf("rejected path wrote output: %v", err)
			}
			proofs, err := filepath.Glob(filepath.Join(runDir, "restricted", "proofs", "compositions", "*.json"))
			if err != nil || len(proofs) != 0 {
				t.Fatalf("rejected path wrote proof: paths=%v err=%v", proofs, err)
			}
		})
	}
}

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

func TestComposeTransitionChainGeneratesEvidenceRefs(t *testing.T) {
	root, runDir := composeTestRun(t, "wf-transition")
	changed1, verification1 := composeTransitionHopEvidenceFixture(t, root, runDir, "wf-transition", "middle", "hop-1")
	changed2, verification2 := composeTransitionHopEvidenceFixture(t, root, runDir, "wf-transition", "target", "hop-2")
	options := ComposeTransitionChainOptions{
		Root: root, RunDir: runDir, WorkflowID: "wf-transition", TargetSnapshot: "target", Output: "restricted/generated/transition.json",
		Hops: []TransitionHopSource{
			{FromSnapshot: "source", ToSnapshot: "middle", ChangedFiles: changed1, Verification: verification1},
			{FromSnapshot: "middle", ToSnapshot: "target", ChangedFiles: changed2, Verification: verification2},
		},
	}
	ref, result := ComposeTransitionChain(options)
	if !result.OK() {
		t.Fatalf("compose transition failed: %#v", result.Failures)
	}
	var chain TransitionChain
	readComposeJSON(t, filepath.Join(runDir, filepath.FromSlash(ref.Path)), &chain)
	if chain.SchemaVersion != 2 || chain.WorkflowID != "wf-transition" || chain.TargetSnapshot != "target" || len(chain.Hops) != 2 || chain.Hops[0].ChangedFiles != testRef(t, runDir, changed1) {
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

func TestComposeTransitionChainRequiresTypedCurrentHopEvidence(t *testing.T) {
	tests := []struct {
		name  string
		input func(*testing.T, string, string) (string, string)
	}{
		{name: "wrong composer", input: func(t *testing.T, root, runDir string) (string, string) {
			changed, _ := composeTransitionHopEvidenceFixture(t, root, runDir, "wf-transition-proof", "target", "current")
			otherChanged, _ := composeTransitionHopEvidenceFixture(t, root, runDir, "wf-transition-proof", "target", "wrong-role")
			return changed, otherChanged
		}},
		{name: "stale changed-files snapshot", input: func(t *testing.T, root, runDir string) (string, string) {
			staleChanged, _ := composeTransitionHopEvidenceFixture(t, root, runDir, "wf-transition-proof", "stale", "stale-changed")
			_, verification := composeTransitionHopEvidenceFixture(t, root, runDir, "wf-transition-proof", "target", "current-verification")
			return staleChanged, verification
		}},
		{name: "stale verification snapshot", input: func(t *testing.T, root, runDir string) (string, string) {
			changed, _ := composeTransitionHopEvidenceFixture(t, root, runDir, "wf-transition-proof", "target", "current-changed")
			_, staleVerification := composeTransitionHopEvidenceFixture(t, root, runDir, "wf-transition-proof", "stale", "stale-verification")
			return changed, staleVerification
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root, runDir := composeTestRun(t, "wf-transition-proof")
			changed, verification := test.input(t, root, runDir)
			output := "restricted/generated/rejected-transition.json"
			_, result := ComposeTransitionChain(ComposeTransitionChainOptions{
				Root: root, RunDir: runDir, WorkflowID: "wf-transition-proof", TargetSnapshot: "target", Output: output,
				Hops: []TransitionHopSource{{FromSnapshot: "source", ToSnapshot: "target", ChangedFiles: changed, Verification: verification}},
			})
			if result.OK() || !strings.Contains(resultSummary(result), "composition proof") {
				t.Fatalf("invalid typed hop evidence was accepted: %#v", result.Failures)
			}
			outputPath := filepath.Join(runDir, filepath.FromSlash(output))
			if _, err := os.Lstat(outputPath); !os.IsNotExist(err) {
				t.Fatalf("rejected transition left output: %v", err)
			}
			proofPath := filepath.Join(receiptProofDir(root, runDir, "compositions"), sha256Bytes([]byte("transition-chain.v1\n"+output))+".json")
			if _, err := os.Lstat(proofPath); !os.IsNotExist(err) {
				t.Fatalf("rejected transition left proof: %v", err)
			}
		})
	}
}

func composeTransitionHopEvidenceFixture(t *testing.T, root, runDir, workflowID, snapshot, prefix string) (string, string) {
	t.Helper()
	changedOutput := filepath.ToSlash(filepath.Join("restricted", "generated", prefix+"-changed.txt"))
	changed, result := ComposeChangedFiles(ComposeChangedFilesOptions{
		Root: root, RunDir: runDir, WorkflowID: workflowID, ChangeSnapshot: snapshot,
		Output: changedOutput, Paths: []string{"internal/" + prefix + ".go"},
	})
	if !result.OK() {
		t.Fatalf("cannot compose changed-files fixture: %#v", result.Failures)
	}
	attemptLogical := filepath.ToSlash(filepath.Join("restricted", "generated", prefix+"-attempt.txt"))
	attemptPath := filepath.Join(runDir, filepath.FromSlash(attemptLogical))
	mustWrite(t, attemptPath, "PASS\n")
	verificationLogical := filepath.ToSlash(filepath.Join("restricted", "generated", prefix+"-verification.json"))
	verificationPath := filepath.Join(runDir, filepath.FromSlash(verificationLogical))
	_, verificationResult := WorkflowFinalVerification(WorkflowFinalVerificationOptions{
		Worktree: root, RunDir: runDir, AttemptArtifacts: []string{relativePath(root, attemptPath)},
		OutputArtifact: relativePath(root, verificationPath), WorkflowID: workflowID, ChangeSnapshot: snapshot,
	})
	if !verificationResult.OK() {
		t.Fatalf("cannot compose verification fixture: %#v", verificationResult.Failures)
	}
	return changed.Path, verificationLogical
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
