package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func settingPosition(t *testing.T, root, home, name string) int {
	t.Helper()
	effective, err := Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := EditableFields(effective)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range fields {
		if field.Name == name {
			return field.Position
		}
	}
	t.Fatalf("unpublished field %s", name)
	return 0
}

func TestEditFieldPreservesOtherValuesAndInheritsOverlay(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	userPath := filepath.Join(home, ".config", "formal-gates", "config.json")
	projectPath := filepath.Join(root, ".gates", "config.json")
	for _, path := range []string{userPath, projectPath} {
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(userPath, []byte(`{"model":"user-model","capacity":8}`), 0600); err != nil {
		t.Fatal(err)
	}
	before := map[string]any{"capacity": float64(5), "qaCapacity": float64(3), "tokenCap": float64(42), "model": "项目模型", "reasoningEffort": "high", "codexExecutable": "a \"command\"\n中文"}
	data, err := json.Marshal(before)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectPath, data, 0600); err != nil {
		t.Fatal(err)
	}
	position := settingPosition(t, root, home, "model")
	effective, err := EditField(root, home, "project", position, "new \"model\"\n中文", false)
	if err != nil || effective.Values.Model != "new \"model\"\n中文" {
		t.Fatalf("edit: %+v %v", effective, err)
	}
	actual, err := os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	var after map[string]any
	if err := json.Unmarshal(actual, &after); err != nil {
		t.Fatal(err)
	}
	before["model"] = "new \"model\"\n中文"
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("unselected values changed: %v", after)
	}
	effective, err = EditField(root, home, "project", position, "", true)
	if err != nil || effective.Values.Model != "user-model" || effective.Sources["model"] != userPath {
		t.Fatalf("field removal did not inherit: %+v %v", effective, err)
	}
}

func TestEditFieldUsesLoaderValidationBeforeWriting(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	for _, bad := range []struct{ name, value string }{{"capacity", "0"}, {"qaCapacity", "65"}, {"tokenCap", "-1"}, {"reasoningEffort", "unsupported"}, {"codexExecutable", ""}} {
		if _, err := EditField(root, home, "project", settingPosition(t, root, home, bad.name), bad.value, false); err == nil {
			t.Fatalf("loader-invalid %s accepted", bad.name)
		}
		if _, err := os.Stat(filepath.Join(root, ".gates", "config.json")); !os.IsNotExist(err) {
			t.Fatalf("invalid candidate changed project: %v", err)
		}
	}
	effective, err := EditField(root, home, "project", settingPosition(t, root, home, "tokenCap"), "0", false)
	if err != nil || effective.Values.TokenCap == nil || *effective.Values.TokenCap != 0 {
		t.Fatalf("valid zero cap: %+v %v", effective, err)
	}
}
