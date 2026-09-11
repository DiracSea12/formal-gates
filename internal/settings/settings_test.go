package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditableOverlayAndErrors(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	install := filepath.Join(home, ".config", "formal-gates")
	project := filepath.Join(root, ".gates")
	os.MkdirAll(install, 0700)
	os.MkdirAll(project, 0700)
	os.WriteFile(filepath.Join(install, "config.json"), []byte(`{"tokenCap":1000,"capacity":2}`), 0600)
	os.WriteFile(filepath.Join(project, "config.json"), []byte(`{"capacity":3}`), 0600)
	e, err := Load(root, home)
	if err != nil || *e.Values.TokenCap != 1000 || e.Values.Capacity != 3 || e.Sources["capacity"] != filepath.Join(project, "config.json") {
		t.Fatalf("%+v %v", e, err)
	}
	for _, data := range []string{`{"tokenCap":0}`, `{"tokenCap":null}`, `{"tokenCap":"100"}`, `{"tokenCaps":100}`, `{"tokencap":1000}`, `{"TokenCap":1000}`, `{"capacity":0}`, `{"attemptAllowance":999}`} {
		os.WriteFile(filepath.Join(project, "config.json"), []byte(data), 0600)
		if _, err = Load(root, home); err == nil || !strings.Contains(err.Error(), filepath.Join(project, "config.json")) {
			t.Fatalf("%s accepted or unlocatable: %v", data, err)
		}
	}
}
