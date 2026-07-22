package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEvidencePathRulesArePlatformNeutral(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "nested", "evidence.txt"), "ok")
	if _, err := safeEvidencePath(dir, "nested/evidence.txt"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", ".", "../outside", "a//b", "/tmp/file", `C:/file`, `\\server\\share`, "https://example.test/file", `a\\b`} {
		if _, err := safeEvidencePath(dir, path); err == nil {
			t.Fatalf("unsafe path accepted: %q", path)
		}
	}
	outside := filepath.Join(t.TempDir(), "outside.txt")
	mustWrite(t, outside, "outside")
	if err := os.Symlink(outside, filepath.Join(dir, "escape")); err == nil {
		if _, err := safeEvidencePath(dir, "escape"); err == nil {
			t.Fatal("symlink escape accepted")
		}
	}
}

func TestReviewerNonPassDoesNotWriteFormalState(t *testing.T) {
	dir := t.TempDir()
	restricted := filepath.Join(dir, "restricted")
	for name, value := range map[string]string{"input.txt": "input", "changed.txt": "changed", "verification.txt": "verified"} {
		mustWrite(t, filepath.Join(restricted, name), value)
	}
	writeJSONTest(t, filepath.Join(restricted, "bundle.json"), ContextBundle{BundleVersion: 1, WorkflowID: "wf", ChangeSnapshot: "snap", Inputs: []EvidenceRef{testRef(t, dir, "restricted/input.txt")}})
	policy, _ := policyByID("code-quality.post-development.v2")
	checks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for i, id := range policy.RequiredCheckIDs {
		status := "PASS"
		if i == 0 {
			status = "FAIL"
		}
		checks = append(checks, ReviewCheck{ID: id, Status: status, Message: reviewerCheckMessage(id), EvidenceRefs: []EvidenceRef{}, Findings: []Finding{}})
	}
	payload := ReviewerPayload{ContextBundle: testRef(t, dir, "restricted/bundle.json"), ReviewPolicyID: policy.ID, Checks: checks, ChangedFiles: ptrRef(testRef(t, dir, "restricted/changed.txt")), Verification: ptrRef(testRef(t, dir, "restricted/verification.txt"))}
	writeEnvelopeTest(t, filepath.Join(dir, "review.json"), FormalGateEvidence{SchemaVersion: 2, ArtifactRole: policy.ArtifactRole, WorkflowID: "wf", ChangeSnapshot: "snap", Gate: policy.Gate, Stage: policy.Stage, Verdict: "FAIL"}, payload)
	result := GateRecord(GateRecordOptions{Worktree: dir, Gate: policy.Gate, Verdict: "FAIL", Artifact: "review.json", WorkflowID: "wf", ChangeSnapshot: "snap"})
	if !result.OK() {
		t.Fatal(result.Failures)
	}
	if isFile(filepath.Join(dir, ".gates", "gate-state.json")) {
		t.Fatal("reviewer non-PASS wrote authoritative state")
	}
}

func TestOldGateStateRequiresNewWorkflow(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".gates", "gate-state.json")
	mustWrite(t, path, `{"schemaVersion":1,"gates":{},"history":[]}`)
	_, result := GateShow(GateShowOptions{Worktree: dir})
	if result.OK() || !strings.Contains(result.Failures[0].Message, "start a new workflow") {
		t.Fatalf("old state accepted: %#v", result.Failures)
	}
}

func TestMechanicalJSONBytesAreDeterministic(t *testing.T) {
	first, err := json.MarshalIndent(Policy(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	second, err := json.MarshalIndent(Policy(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatal("typed mechanical output drifted")
	}
}

func mustWrite(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func repoRootForCanaryTest(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

func writePollutionPatternsForTest(t *testing.T, dir string) {
	t.Helper()
	mustWrite(t, filepath.Join(dir, "hooks", "pollution-patterns.json"), `{"patterns":[]}`)
}
