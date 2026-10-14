package settings

import (
	"encoding/json"
	"formal-gates/internal/coordination"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
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
	userData := []byte(`{"model":"user-model","capacity":8}`)
	if err := os.WriteFile(userPath, userData, 0600); err != nil {
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
	actual, err = os.ReadFile(projectPath)
	if err != nil {
		t.Fatal(err)
	}
	after = nil
	if err := json.Unmarshal(actual, &after); err != nil {
		t.Fatal(err)
	}
	delete(before, "model")
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("removal changed unselected values: %v", after)
	}
	actual, err = os.ReadFile(userPath)
	if err != nil || string(actual) != string(userData) {
		t.Fatalf("editing project changed user overlay bytes: %q %v", actual, err)
	}
}

func TestEditFieldSerializesSelectedOverlay(t *testing.T) {
	for _, scope := range []string{"project", "user"} {
		t.Run(scope, func(t *testing.T) {
			root, home := t.TempDir(), t.TempDir()
			otherRoot := root
			path := overlayPaths(root, home)[1]
			if scope == "user" {
				otherRoot = t.TempDir()
				path = overlayPaths(root, home)[0]
			}
			positions := []int{settingPosition(t, root, home, "capacity"), settingPosition(t, otherRoot, home, "model")}
			release, err := coordination.AcquirePathWaiting(path+".lock", "settings test")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(release)
			lockInfo, err := os.Stat(path + ".lock")
			if err != nil {
				t.Fatal(err)
			}
			results := make(chan error, 2)
			started := make(chan struct{}, 2)
			for i, edit := range []struct{ root, value string }{{root, "8"}, {otherRoot, "new-model"}} {
				go func(position int, root, value string) {
					started <- struct{}{}
					_, err := EditField(root, home, scope, position, value, false)
					results <- err
				}(positions[i], edit.root, edit.value)
			}
			<-started
			<-started
			select {
			case err := <-results:
				t.Fatalf("edit completed while its overlay lock was held: %v", err)
			case <-time.After(100 * time.Millisecond):
			}
			if err := os.WriteFile(path, []byte(`{"qaCapacity":3}`), 0600); err != nil {
				t.Fatal(err)
			}
			release()
			for i := 0; i < 2; i++ {
				if err := <-results; err != nil {
					t.Fatal(err)
				}
			}
			actual, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var values map[string]any
			if err := json.Unmarshal(actual, &values); err != nil {
				t.Fatal(err)
			}
			want := map[string]any{"capacity": float64(8), "model": "new-model", "qaCapacity": float64(3)}
			if !reflect.DeepEqual(values, want) {
				t.Fatalf("concurrent edits did not preserve all values: %s", actual)
			}
			currentLock, err := os.Stat(path + ".lock")
			if err != nil || !os.SameFile(lockInfo, currentLock) {
				t.Fatalf("edit replaced its stable lock file: %v", err)
			}
		})
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
