package validate

import

// Independent structural QA for dispatch-2fe71c878cf4bb6c672661a8.
// Fixtures describe documented intermediate states at their owning layer.
// Persisted fixtures use the production writer only in t.TempDir; no real run,
// manually edited state file, prior test helper or prior test suite is consumed.
(
	"encoding/json"

	"os"
	"os/exec"
	"path/filepath"

	"strings"
	"testing"
)

func qa35bJSON(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func qa35bErr(t *testing.T, err error, part string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), part) {
		t.Fatalf("want error containing %q, got %v", part, err)
	}
}
func qa35bOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func qa35bPackage(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("../..")
	qa35bOK(t, err)
	return p
}
func qa35bRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	qa35bOK(t, err)
	return string(b)
}
func qa35bWrite(t *testing.T, path, data string) {
	t.Helper()
	qa35bOK(t, os.WriteFile(path, []byte(data), 0600))
}

func qa35bFinding(t *testing.T) (string, FindingInput) {
	t.Helper()
	root := t.TempDir()
	qa35bWrite(t, filepath.Join(root, "entry.go"), "package example\n// public entry\n")
	return root, FindingInput{Severity: "P1", SeverityRationale: "REQ-001: the documented record command cannot accept valid evidence, so every normal gate result is blocked; no low-cost P2 workaround exists", Requirement: "REQ-001", NormalPath: "workflow record-gate with complete normal result", ActualError: "complete result is rejected", DeferralImpact: "delivery cannot record its required gate result", Message: "complete gate record is rejected", Locations: []string{"entry.go:2"},
		Verification: FindingVerification{Scope: "documented record command", Severity: "core acceptance blocked without workaround", Causality: "record validator rejects the valid branch", Reproduction: "invoke the documented record command with a complete result", Classification: "core acceptance entry is missing"}}
}

// qa35bGit creates a private native snapshot for snapshot/requirement API tests.
func qa35bGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=QA Design", "GIT_AUTHOR_EMAIL=qa@example.invalid", "GIT_COMMITTER_NAME=QA Design", "GIT_COMMITTER_EMAIL=qa@example.invalid")
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
