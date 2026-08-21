//go:build phase0whitebox

package validate

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWhiteboxPhase0Round11VersionEnvelopeRejectsBeforeWrite(t *testing.T) {
	valid := VersionEnvelope{
		Writer:                    "engine",
		StateSchemaVersion:        CurrentStateSchemaVersion,
		WorkflowDefinitionVersion: CurrentWorkflowDefinitionVersion,
		DefinitionSource:          CurrentWorkflowDefinitionSource,
		DefinitionDigest:          CurrentWorkflowDefinitionDigest,
		PackageDigest:             "sha256:round11-package",
	}

	validPath := filepath.Join(t.TempDir(), "state.json")
	if err := WriteVersionedState(validPath, valid, map[string]any{"status": "ACTIVE"}); err != nil {
		t.Fatalf("current complete envelope was rejected: %v", err)
	}
	data, err := os.ReadFile(validPath)
	if err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatal(err)
	}
	for field, want := range map[string]string{
		"writer":                    valid.Writer,
		"stateSchemaVersion":        valid.StateSchemaVersion,
		"workflowDefinitionVersion": valid.WorkflowDefinitionVersion,
		"definitionSource":          valid.DefinitionSource,
		"definitionDigest":          valid.DefinitionDigest,
	} {
		if got, ok := document[field].(string); !ok || got != want {
			t.Fatalf("valid write lost %s: got %#v, want %q", field, document[field], want)
		}
	}

	mutations := []struct {
		name   string
		mutate func(*VersionEnvelope)
	}{
		{"missing writer", func(value *VersionEnvelope) { value.Writer = "" }},
		{"wrong writer", func(value *VersionEnvelope) { value.Writer = "legacy" }},
		{"missing schema", func(value *VersionEnvelope) { value.StateSchemaVersion = "" }},
		{"older schema", func(value *VersionEnvelope) { value.StateSchemaVersion = "0" }},
		{"newer schema", func(value *VersionEnvelope) { value.StateSchemaVersion = "2" }},
		{"missing definition version", func(value *VersionEnvelope) { value.WorkflowDefinitionVersion = "" }},
		{"older definition version", func(value *VersionEnvelope) { value.WorkflowDefinitionVersion = "0" }},
		{"newer definition version", func(value *VersionEnvelope) { value.WorkflowDefinitionVersion = "2" }},
		{"missing definition source", func(value *VersionEnvelope) { value.DefinitionSource = "" }},
		{"wrong definition source", func(value *VersionEnvelope) { value.DefinitionSource = "definitions/other.json" }},
		{"missing definition digest", func(value *VersionEnvelope) { value.DefinitionDigest = "" }},
		{"wrong definition digest", func(value *VersionEnvelope) { value.DefinitionDigest = "sha256:stale" }},
	}
	for _, mutation := range mutations {
		t.Run(mutation.name, func(t *testing.T) {
			candidate := valid
			mutation.mutate(&candidate)
			target := filepath.Join(t.TempDir(), "state.json")
			sentinel := []byte("existing state must remain unchanged\n")
			if err := os.WriteFile(target, sentinel, 0o600); err != nil {
				t.Fatal(err)
			}

			err := WriteVersionedState(target, candidate, map[string]any{"status": "MUST_NOT_WRITE"})
			var unsupported *UnsupportedRunVersionError
			if !errors.As(err, &unsupported) || !IsUnsupportedRunVersion(err) || !strings.Contains(err.Error(), UnsupportedRunVersionCode) {
				t.Fatalf("incompatible envelope did not return typed %s: %v", UnsupportedRunVersionCode, err)
			}
			after, readErr := os.ReadFile(target)
			if readErr != nil {
				t.Fatal(readErr)
			}
			if !bytes.Equal(after, sentinel) {
				t.Fatalf("write barrier modified the destination: got %q, want %q", after, sentinel)
			}

			absentTarget := filepath.Join(t.TempDir(), "nested", "state.json")
			err = WriteVersionedState(absentTarget, candidate, map[string]any{"status": "MUST_NOT_CREATE"})
			if !errors.As(err, &unsupported) || !IsUnsupportedRunVersion(err) || !strings.Contains(err.Error(), UnsupportedRunVersionCode) {
				t.Fatalf("incompatible envelope did not reject an absent destination with typed %s: %v", UnsupportedRunVersionCode, err)
			}
			if _, statErr := os.Stat(absentTarget); !os.IsNotExist(statErr) {
				t.Fatalf("write barrier created an absent destination: %v", statErr)
			}
		})
	}
}
