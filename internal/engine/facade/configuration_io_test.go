package facade

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/protocol"
)

func TestConfigurationIOOpenAndShowPreserveFailureAndRecover(t *testing.T) {
	f, root := workerMaterialFacade(t)
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(root, ".gates", "config.json")
	saved := config + ".saved"
	if err := os.Rename(config, saved); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(config, 0700); err != nil {
		t.Fatal(err)
	}
	// A library path being replaced is a file-read failure, not invalid JSON.
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	statePath := filepath.Join(f.stateDir, "state.json")
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	for _, read := range []func() (decision.NextResult, error){func() (decision.NextResult, error) { run, err := reopened.Show(); return run.Next, err }, reopened.Next} {
		next, err := read()
		if err != nil || next.Wait == nil || next.Wait.Reason != decision.WaitConfigurationUnavailable || !strings.Contains(next.Wait.Detail, config) {
			t.Fatal("read-only view hid the persistent file-read error", next, err)
		}
		after, err := os.ReadFile(statePath)
		if err != nil || !bytes.Equal(before, after) {
			t.Fatal("read-only Show/Next wrote the read failure", err)
		}
	}
	if _, err := reopened.Drive(); err != nil {
		t.Fatal(err)
	}
	stopped, err := reopened.engine.Load()
	if err != nil || stopped.State.ConfigurationReadFailure() == nil {
		t.Fatal("reopening lost the prior failure", err)
	}
	decisions := len(stopped.State.Decisions)
	if err := os.Remove(config); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(saved, config); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Drive(); err != nil {
		t.Fatal(err)
	}
	recovered, err := reopened.engine.Load()
	if err != nil || recovered.State.ConfigurationReadFailure() != nil || len(recovered.State.Decisions) != decisions {
		t.Fatal("recovery required a new user decision or retained the failure", err)
	}
	projected, err := reopened.engine.Project(protocol.Observation{})
	if err != nil || projected.Plan.Next.Wait != nil && projected.Plan.Next.Wait.Reason == decision.WaitConfigurationUnavailable {
		t.Fatal("repaired library stayed stopped", err)
	}
}
