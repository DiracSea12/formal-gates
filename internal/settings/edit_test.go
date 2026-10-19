package settings

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"formal-gates/internal/coordination"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
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
	effective, err := EditField(root, home, "project", position, "new \"model\"\n中文", false, nil)
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
	effective, err = EditField(root, home, "project", position, "", true, nil)
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
					_, err := EditField(root, home, scope, position, value, false, nil)
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
		if _, err := EditField(root, home, "project", settingPosition(t, root, home, bad.name), bad.value, false, nil); err == nil {
			t.Fatalf("loader-invalid %s accepted", bad.name)
		}
		if _, err := os.Stat(filepath.Join(root, ".gates", "config.json")); !os.IsNotExist(err) {
			t.Fatalf("invalid candidate changed project: %v", err)
		}
	}
	effective, err := EditField(root, home, "project", settingPosition(t, root, home, "tokenCap"), "0", false, nil)
	if err != nil || effective.Values.TokenCap == nil || *effective.Values.TokenCap != 0 {
		t.Fatalf("valid zero cap: %+v %v", effective, err)
	}
}

func TestPanelSettingsUseDefaultsRangeAndScalarEdits(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	effective, err := Load(root, home)
	if err != nil || effective.Values.PanelIdleTimeout != 300 || !effective.Values.PanelSelection || effective.Sources["panelIdleTimeout"] != "default" || effective.Sources["panelSelection"] != "default" {
		t.Fatalf("panel defaults: %+v %v", effective, err)
	}
	idle, selection := settingPosition(t, root, home, "panelIdleTimeout"), settingPosition(t, root, home, "panelSelection")
	for _, bad := range []struct {
		position int
		value    string
	}{{idle, "-1"}, {idle, "86401"}, {idle, "ten"}, {selection, "maybe"}} {
		if _, err := EditField(root, home, "project", bad.position, bad.value, false, nil); err == nil {
			t.Fatalf("invalid panel value %q accepted", bad.value)
		}
	}
	path := overlayPaths(root, home)[1]
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("invalid panel value changed project: %v", err)
	}
	for _, value := range []string{"0", "86400"} {
		if effective, err := EditField(root, home, "project", idle, value, false, nil); err != nil || strconv.Itoa(effective.Values.PanelIdleTimeout) != value {
			t.Fatalf("panelIdleTimeout %s: %+v %v", value, effective.Values, err)
		}
	}
	effective, err = EditField(root, home, "project", selection, "false\n", false, nil)
	if err != nil || effective.Values.PanelSelection || effective.Sources["panelSelection"] != path {
		t.Fatalf("panelSelection edit: %+v %v", effective, err)
	}
	data, err := os.ReadFile(path)
	var raw map[string]json.RawMessage
	if err != nil || json.Unmarshal(data, &raw) != nil || string(raw["panelSelection"]) != "false" || string(raw["panelIdleTimeout"]) != "86400" {
		t.Fatalf("panel values were not stored as JSON boolean and integer: %s %v", data, err)
	}
	putConfig(t, path, `{"panelSelection":"false"}`)
	if _, err := Load(root, home); err == nil {
		t.Fatal("string panelSelection accepted")
	}
}

func TestOverlayRevisionsRejectWritesAfterAnotherChange(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	user, project := overlayPaths(root, home)[0], overlayPaths(root, home)[1]
	revisions, err := Revisions(root, home)
	if err != nil || !reflect.DeepEqual(revisions, map[string]string{"user": "", "project": ""}) {
		t.Fatalf("missing files revisions: %v %v", revisions, err)
	}
	if _, err := SaveConfiguration(root, home, "project", "甲", "", revisions); err != nil {
		t.Fatal(err)
	}
	revisions, err = Revisions(root, home)
	data, readErr := os.ReadFile(project)
	if err != nil || readErr != nil || revisions["project"] != fmt.Sprintf("%x", sha256.Sum256(data)) || revisions["user"] != "" {
		t.Fatalf("revisions do not identify file content: %v %v %v", revisions, err, readErr)
	}
	putConfig(t, user, `{"capacity":5}`)
	position := settingPosition(t, root, home, "capacity")
	stale := map[string]string{"user": revisions["user"], "project": revisions["project"]}
	for operation, run := range map[string]func() error{
		"field": func() error { _, err := EditField(root, home, "project", position, "6", false, stale); return err },
		"save":  func() error { _, err := SaveConfiguration(root, home, "project", "乙", "", stale); return err },
		"set": func() error {
			_, err := EditConfiguration(root, home, "project", "甲", stale, func(*Configuration) error { return nil })
			return err
		},
		"use": func() error { _, err := UseConfiguration(root, home, "project", "甲", stale); return err },
		"delete": func() error {
			_, err := DeleteConfiguration(root, home, "project", "甲", configurationBindings(nil), stale)
			return err
		},
		"rename": func() error {
			_, err := RenameConfiguration(root, home, "project", "甲", "乙", configurationBindings(nil), stale)
			return err
		},
	} {
		if err := run(); err == nil || !strings.Contains(err.Error(), "user configuration file changed") || !strings.Contains(err.Error(), "reload") {
			t.Fatalf("%s with a stale revision was not rejected: %v", operation, err)
		}
	}
	if after, _ := os.ReadFile(project); !bytes.Equal(after, data) {
		t.Fatalf("stale write changed the project file: %s", after)
	}
	if _, err := RenameConfiguration(root, home, "project", "甲", "乙", configurationBindings(nil), map[string]string{"project": revisions["project"]}); err != nil {
		t.Fatalf("matching project revision was rejected: %v", err)
	}
}

func TestOverlayEditsHoldBothLayersThroughWrite(t *testing.T) {
	for _, scope := range []string{"user", "project"} {
		for _, checked := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/revisions=%t", scope, checked), func(t *testing.T) {
				root, home := t.TempDir(), t.TempDir()
				paths := overlayPaths(root, home)
				for _, path := range paths {
					putConfig(t, path, `{"capacity":5,"configurations":{"shared":{"qaModes":[]}}}`)
				}
				position := settingPosition(t, root, home, "capacity")
				var expected map[string]string
				if checked {
					var err error
					expected, err = Revisions(root, home)
					if err != nil {
						t.Fatal(err)
					}
				}
				entered, proceed := make(chan struct{}), make(chan struct{})
				first := make(chan error, 1)
				go func() {
					_, err := EditConfiguration(root, home, scope, "shared", expected, func(configuration *Configuration) error {
						close(entered)
						<-proceed
						configuration.QAModes = []string{"whitebox"}
						return nil
					})
					first <- err
				}()
				select {
				case <-entered:
				case err := <-first:
					t.Fatalf("first edit did not enter its change: %v", err)
				case <-time.After(5 * time.Second):
					t.Fatal("first edit deadlocked")
				}
				// The callback runs after the revision check and before the write.
				// Probe kernel ownership here instead of guessing thread timing.
				for _, path := range paths {
					release, err := coordination.AcquirePath(path+".lock", "overlay ownership test")
					if err == nil {
						release()
						t.Errorf("overlay was writable between revision check and commit: %s", path)
					} else {
						var held *coordination.LockHeldError
						if !errors.As(err, &held) {
							t.Errorf("checking overlay lock: %v", err)
						}
					}
				}
				otherScope := "project"
				firstIndex, otherIndex := 0, 1
				if scope == "project" {
					otherScope, firstIndex, otherIndex = "user", 1, 0
				}
				second := make(chan error, 1)
				go func() {
					_, err := EditField(root, home, otherScope, position, "8", false, expected)
					second <- err
				}()
				close(proceed)
				for i, result := range []chan error{first, second} {
					select {
					case err := <-result:
						if i == 1 && checked {
							if err == nil || !strings.Contains(err.Error(), scope+" configuration file changed") {
								t.Errorf("cross-layer stale write was not rejected: %v", err)
							}
						} else if err != nil {
							t.Errorf("edit %d failed: %v", i, err)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("cross-layer edits deadlocked")
					}
				}
				for i, path := range paths {
					data, err := os.ReadFile(path)
					var got struct {
						Capacity       int                      `json:"capacity"`
						Configurations map[string]Configuration `json:"configurations"`
					}
					if err != nil || json.Unmarshal(data, &got) != nil {
						t.Fatalf("overlay content: %s %v", data, err)
					}
					wantCapacity := 5
					if i == otherIndex && !checked {
						wantCapacity = 8
					}
					wantModes := []string{}
					if i == firstIndex {
						wantModes = []string{"whitebox"}
					}
					if got.Capacity != wantCapacity || !reflect.DeepEqual(got.Configurations["shared"].QAModes, wantModes) {
						t.Fatalf("cross-layer writes lost or overwrote values: %s", data)
					}
				}
			})
		}
	}
}

func TestConfigurationBindingResolverRunsInsideOverlayWindow(t *testing.T) {
	for _, operation := range []string{"delete", "rename"} {
		for _, failRead := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/read-error=%t", operation, failRead), func(t *testing.T) {
				root, home := t.TempDir(), t.TempDir()
				if _, err := SaveConfiguration(root, home, "project", "工作", "", nil); err != nil {
					t.Fatal(err)
				}
				path := overlayPaths(root, home)[1]
				before, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				revisions, err := Revisions(root, home)
				if err != nil {
					t.Fatal(err)
				}
				readErr := errors.New("bindings unavailable")
				calls := 0
				resolveBindings := func() (map[string][]string, error) {
					calls++
					for _, path := range overlayPaths(root, home) {
						release, err := coordination.AcquirePath(path+".lock", "observe binding validation")
						if err == nil {
							release()
							t.Fatal("binding resolver ran outside the overlay window")
						}
						var held *coordination.LockHeldError
						if !errors.As(err, &held) {
							t.Fatal(err)
						}
					}
					if failRead {
						return nil, readErr
					}
					return configurationBindings(map[string][]string{"工作": {"active-run"}})()
				}
				if operation == "delete" {
					_, err = DeleteConfiguration(root, home, "project", "工作", resolveBindings, revisions)
				} else {
					_, err = RenameConfiguration(root, home, "project", "工作", "新名称", resolveBindings, revisions)
				}
				if calls != 1 || failRead && !errors.Is(err, readErr) ||
					!failRead && (err == nil || !strings.Contains(err.Error(), "active-run")) {
					t.Fatalf("binding validation did not refuse the change: calls=%d error=%v", calls, err)
				}
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(before, after) {
					t.Fatalf("failed binding validation wrote the overlay: %s %v", after, err)
				}
			})
		}
	}
}
