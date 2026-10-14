package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/facade"
)

func TestWorkflowDiagnoseEnumeratesOriginalStates(t *testing.T) {
	root := filepath.Join(t.TempDir(), "workspace[active]")
	states := []struct {
		namespace, run, owner, status, schema string
	}{
		{"engine", "current", "current-owner", "ACTIVE", "current-schema"},
		{"tmp", "residual", "residual-owner", "UNVERIFIED", "residual-schema"},
	}
	paths := make([]string, len(states))
	originals := make([][]byte, len(states))
	for index, state := range states {
		paths[index] = filepath.Join(root, ".gates", state.namespace, state.run, "state.json")
		body, err := json.Marshal(map[string]any{
			"ownerSession": state.owner, "status": state.status,
			"stateSchemaVersion": state.schema,
		})
		if err != nil {
			t.Fatal(err)
		}
		originals[index] = body
		mustWriteCLI(t, paths[index], string(body))
	}
	var reports []facade.DiagnosticReport
	if err := json.Unmarshal([]byte(runCLI(t, "workflow", "diagnose", "--root", root)), &reports); err != nil {
		t.Fatal(err)
	}
	if len(reports) != len(states) {
		t.Fatalf("diagnostic reports = %+v", reports)
	}
	for index, state := range states {
		report := reports[index]
		if report.Path != paths[index] || !report.JSONReadable ||
			report.Summary["ownerSession"] != state.owner || report.Summary["status"] != state.status ||
			report.DetectedVersions["stateSchemaVersion"] != state.schema {
			t.Fatalf("original diagnostic report = %+v", report)
		}
		body, err := os.ReadFile(paths[index])
		if err != nil || !bytes.Equal(body, originals[index]) {
			t.Fatalf("diagnose changed original state %s: %v", paths[index], err)
		}
		entries, err := os.ReadDir(filepath.Dir(paths[index]))
		if err != nil || len(entries) != 1 || entries[0].Name() != "state.json" {
			t.Fatalf("diagnose created run artifacts: %v %v", entries, err)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, ".gates"))
	if err != nil || len(entries) != 2 || entries[0].Name() != "engine" || entries[1].Name() != "tmp" {
		t.Fatalf("diagnose created workflow artifacts: %v %v", entries, err)
	}
}

func TestWorkflowDiagnoseEmptyProjectIsReadOnly(t *testing.T) {
	root := t.TempDir()
	var reports []facade.DiagnosticReport
	if err := json.Unmarshal([]byte(runCLI(t, "workflow", "diagnose", "--root", root)), &reports); err != nil {
		t.Fatal(err)
	}
	if reports == nil || len(reports) != 0 {
		t.Fatalf("empty diagnostic reports = %+v", reports)
	}
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		t.Fatalf("diagnose wrote to an empty project: %v %v", entries, err)
	}
}
