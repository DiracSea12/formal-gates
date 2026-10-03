package whitebox_qa

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/testkit"
)

func fixtureQAReceipt() facade.IntakeConfirmationReceipt {
	return facade.IntakeConfirmationReceipt{
		Source:              facade.DefaultIntakeSource,
		Authority:           facade.DefaultIntakeAuthority,
		Transport:           facade.DefaultIntakeTransport,
		RequirementSource:   "requirements.md",
		RequirementRevision: "req-fixture",
		Artifacts: []facade.IntakeArtifact{
			{Path: "requirements.md", Revision: "req-fixture"},
			{Path: "design.md", Revision: "sol-fixture"},
		},
		SolutionRevision: "sol-fixture",
		SolutionDigest:   "sha256:sol-fixture",
	}
}

func fixtureStartRequest(runID string) facade.StartRequest {
	return facade.StartRequest{
		RunID:                     runID,
		Route:                     "lightweight",
		Provider:                  "engine",
		DefinitionSource:          facade.DefaultDefinitionSource,
		DefinitionDigest:          definition.WorkflowDefinitionDigest,
		IntakeConfirmationReceipt: fixtureQAReceipt(),
	}
}

func fixtureAdmission(t *testing.T, root, packageDigest, recordID string) *facade.Admission {
	t.Helper()
	registryPath, target, generation, lease, token := testkit.Installation(t, root, recordID, packageDigest, "engine")
	return &facade.Admission{RegistryPath: registryPath, Target: target, Root: root, Provider: "engine",
		PackageDigest: packageDigest, InstalledTargetIdentity: recordID, Generation: generation, Lease: lease, Token: token}
}

// TestFacadeOpenMissingReceiptHidesFilesystemPath covers the normal
// missing-receipt error boundary. Open must return the stable intake error and
// must not expose the private engine state path from os.ReadFile.
func TestFacadeOpenMissingReceiptHidesFilesystemPath(t *testing.T) {
	root := t.TempDir()
	runID := "missing-intake-receipt"
	stateDir := filepath.Join(root, filepath.FromSlash(facade.EngineNamespace), runID)
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}

	_, err := facade.Open(root, runID)
	if err == nil {
		t.Fatal("open without request receipt unexpectedly succeeded")
	}
	want := facade.InvalidIntakeConfirmation + ": intake receipt is unavailable"
	if err.Error() != want {
		t.Fatalf("missing receipt error = %q, want %q", err, want)
	}
	for _, leaked := range []string{root, stateDir, "request.json"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("missing receipt error leaked filesystem detail %q: %q", leaked, err)
		}
	}
}

// TestCleanupTerminalCommitsReceiptAndClearsIntent checks the
// terminal cleanup commit boundary directly: active/protocol files are gone,
// the reconciled receipt remains, and cleanup intent is cleared only after the
// receipt has been published.
func TestCleanupTerminalCommitsReceiptAndClearsIntent(t *testing.T) {
	stateDir := t.TempDir()
	store, err := persistence.NewStore(stateDir, persistence.Config{PackageDigest: "sha256:fixture-package"})
	if err != nil {
		t.Fatalf("new store: %v", err)
	}
	for name, data := range map[string]string{
		"state.json":                   "active projection\n",
		"state.json.intent":            "commit intent\n",
		".state.json.fixture-dead.tmp": "staged projection\n",
	} {
		if err := os.WriteFile(filepath.Join(stateDir, name), []byte(data), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	if err := store.CleanupTerminal(); err != nil {
		t.Fatalf("cleanup terminal: %v", err)
	}
	for _, name := range []string{"state.json", "state.json.intent", ".state.json.fixture-dead.tmp", "cleanup.intent.json"} {
		if _, err := os.Stat(filepath.Join(stateDir, name)); !os.IsNotExist(err) {
			t.Fatalf("terminal cleanup retained %s: %v", name, err)
		}
	}
	receiptPath := filepath.Join(stateDir, "cleanup.receipt.json")
	receiptBytes, err := os.ReadFile(receiptPath)
	if err != nil {
		t.Fatalf("read cleanup receipt: %v", err)
	}
	var receipt struct {
		Status  string `json:"status"`
		Residue bool   `json:"residue"`
	}
	if err := json.Unmarshal(receiptBytes, &receipt); err != nil {
		t.Fatalf("decode cleanup receipt: %v", err)
	}
	if receipt.Status != "reconciled" || receipt.Residue {
		t.Fatalf("cleanup receipt = %+v, want reconciled with residue=false", receipt)
	}
}

// TestCandidateStartRejectsRetainedLegacyResult proves the runtime
// namespace fence also covers a legacy run whose active directory is already
// gone but whose retained result still owns the run id.
func TestCandidateStartRejectsRetainedLegacyResult(t *testing.T) {
	root := t.TempDir()
	runID := "retained-legacy-result"
	resultPath := filepath.Join(root, ".gates", "results", runID+".json")
	if err := os.MkdirAll(filepath.Dir(resultPath), 0o700); err != nil {
		t.Fatal(err)
	}
	resultBytes := []byte("{\"runId\":\"retained-legacy-result\",\"status\":\"SEALED\"}\n")
	if err := os.WriteFile(resultPath, resultBytes, 0o600); err != nil {
		t.Fatal(err)
	}

	_, _, err := facade.Start(facade.StartOptions{
		Root: root, Request: fixtureStartRequest(runID), Admission: fixtureAdmission(t, root, "sha256:fixture-package", "fixture-target"),
	})
	if err == nil || !strings.Contains(err.Error(), "already has a retained legacy result") {
		t.Fatalf("candidate start retained-result error = %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(root, filepath.FromSlash(facade.EngineNamespace))); !os.IsNotExist(statErr) {
		t.Fatalf("rejected candidate start created engine namespace: %v", statErr)
	}
	after, err := os.ReadFile(resultPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(resultBytes) {
		t.Fatal("rejected candidate start changed the retained legacy result")
	}
}
