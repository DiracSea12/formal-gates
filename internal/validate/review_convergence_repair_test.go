package validate

import (
	"os"
	"path/filepath"

	"strings"
	"testing"
)

func repairDocumentJSON(t *testing.T, marker string, bindings map[string]string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "references", "formal-flow.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, tail, ok := strings.Cut(string(data), "<!-- "+marker+" -->")
	if !ok {
		t.Fatal("document example marker missing: " + marker)
	}
	_, tail, ok = strings.Cut(tail, "```json\n")
	if !ok {
		t.Fatal("JSON example missing")
	}
	body, _, ok := strings.Cut(tail, "\n```")
	if !ok {
		t.Fatal("JSON example is not closed")
	}
	for key, value := range bindings {
		body = strings.ReplaceAll(body, "${"+key+"}", value)
	}
	path := filepath.Join(t.TempDir(), "input.json")
	writeTestFile(t, path, body)
	return path
}
