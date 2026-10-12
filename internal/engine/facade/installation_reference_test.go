package facade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/protocol"
)

func globalReferenceAdmission(t *testing.T, root string) *Admission {
	t.Helper()
	a := testAdmission(t, root, "global-target", "sha256:package", DefaultProvider)
	data, err := os.ReadFile(a.RegistryPath)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err = json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	record := doc["records"].([]any)[0].(map[string]any)
	resources := filepath.Join(t.TempDir(), "resources")
	record["scope"], record["resourceRoot"] = "global", resources
	record["canonicalPaths"].(map[string]any)["resourceRoot"] = resources
	if err = writeJSONAtomic(a.RegistryPath, doc); err != nil {
		t.Fatal(err)
	}
	return a
}

func TestStartRecoveryRetiresOriginalRequestReference(t *testing.T) {
	root := t.TempDir()
	a := globalReferenceAdmission(t, root)
	id := "start-recovery"
	stateDir := filepath.Join(root, EngineNamespace, id)
	if err := a.write(func() error {
		if err := os.MkdirAll(stateDir, 0700); err != nil {
			return err
		}
		if err := writeJSONAtomic(filepath.Join(stateDir, "request.json"), StartRequest{RunID: id}); err != nil {
			return err
		}
		if err := writeJSONAtomic(startIntentPath(root, id), startIntent{RunID: id, Admission: *a}); err != nil {
			return err
		}
		path, err := a.runReferencePath(stateDir)
		if err != nil {
			return err
		}
		return writeJSONAtomic(path, map[string]string{"request": filepath.Join(stateDir, "request.json")})
	}); err != nil {
		t.Fatal(err)
	}
	reference, err := a.runReferencePath(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := recoverStartIntent(root, id); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{stateDir, startIntentPath(root, id), reference} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("half-start recovery retained original prefix", path, err)
		}
	}
	if _, _, err := Start(StartOptions{Root: root, Request: StartRequest{RunID: id, IntakeConfirmationReceipt: testReceipt()}, Admission: a}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(reference)
	if err != nil {
		t.Fatal(err)
	}
	var location map[string]string
	if err = json.Unmarshal(data, &location); err != nil || len(location) != 1 || location["request"] != filepath.Join(stateDir, "request.json") {
		t.Fatal("restarted reference changed its original payload", location, err)
	}
}

func TestTerminalRecoveryRetiresOriginalRequestReference(t *testing.T) {
	root := t.TempDir()
	a := globalReferenceAdmission(t, root)
	id := "terminal-recovery"
	f, _, err := Start(StartOptions{Root: root, Request: StartRequest{RunID: id, IntakeConfirmationReceipt: testReceipt()}, Admission: a})
	if err != nil {
		t.Fatal(err)
	}
	receipt := testReceipt()
	digest, err := IntakeDigest(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.engine.RecordIntakeReceipt(protocol.IntakeReceipt{Confirmation: receipt, IntakeDigest: digest}, emptyFingerprint()); err != nil {
		t.Fatal(err)
	}
	if _, err = f.engine.CompleteAll(emptyFingerprint()); err != nil {
		t.Fatal(err)
	}
	run, err := f.project()
	if err != nil {
		t.Fatal(err)
	}
	if err = f.admission.write(func() error {
		return writeJSONAtomic(terminalIntentPath(f.stateDir), summaryForRun(run, ownerInterval{}))
	}); err != nil {
		t.Fatal(err)
	}
	if err = f.store.CleanupTerminal(); err != nil {
		t.Fatal(err)
	}
	reference, err := a.runReferencePath(f.stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(reference); err != nil {
		t.Fatal("interrupted terminal lost original locator", err)
	}
	reopened, err := Open(root, id)
	if err != nil {
		t.Fatal(err)
	}
	if run, err = reopened.Drive(); err != nil || run.Status != "COMPLETE" {
		t.Fatal("original terminal recovery", run.Status, err)
	}
	if _, err = os.Stat(reference); !os.IsNotExist(err) {
		t.Fatal("terminal recovery retained locator", err)
	}
	summary, err := os.ReadFile(terminalSummaryPath(f.stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = reopened.Drive(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(terminalSummaryPath(f.stateDir))
	if err != nil || string(summary) != string(after) {
		t.Fatal("terminal replay changed original summary bytes", err)
	}
}
