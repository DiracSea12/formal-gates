package validate

import (
	"encoding/json"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/encoder"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCurrentArtifactDeclaresEnginePublicAuthority(t *testing.T) {
	path := filepath.Join("../..", "definitions", "workflow.json")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var artifact struct {
		Version     string   `json:"version"`
		Schema      string   `json:"stateSchemaVersion"`
		Owner       string   `json:"transactionOwner"`
		Entrypoints []string `json:"entrypoints"`
	}
	if err := json.Unmarshal(body, &artifact); err != nil {
		t.Fatal(err)
	}
	want := []string{"workflow start", "workflow drive", "workflow submit", "workflow show", "workflow status", "workflow next", "workflow diagnose", "workflow guard-preview"}
	if artifact.Owner != "engine" || artifact.Version != definition.WorkflowDefinitionVersion || artifact.Schema != encoder.StateSchemaVersion || !reflect.DeepEqual(artifact.Entrypoints, want) {
		t.Fatalf("current artifact authority=%+v", artifact)
	}
}
