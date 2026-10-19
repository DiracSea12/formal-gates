package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/host"
)

func TestNamedConfigurationsPreserveWorkflowChoicesAndScope(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	effective, options, err := LoadConfigurations(root, home)
	if err != nil || len(options) != 1 || options[0].Name != DefaultConfigurationName || !options[0].Active || options[0].Source != "default" || effective.Configuration.FlowSelection != nil || effective.Configuration.Gates != nil || len(effective.Configuration.QAModes) != 0 || len(effective.Configuration.Execution) != 0 {
		t.Fatalf("built-in configuration: %+v %+v %v", effective, options, err)
	}
	user := filepath.Join(home, ".config", "formal-gates", "config.json")
	project := filepath.Join(root, ".gates", "config.json")
	putConfig(t, user, `{"capacity":7,"tokenCap":29,"codexExecutable":"configured-cli"}`)
	if _, err := SaveConfiguration(root, home, "user", "我的 配置", "1", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := EditConfiguration(root, home, "user", "我的 配置", nil, func(configuration *Configuration) error {
		configuration.FlowSelection = &FlowSelection{Disabled: []string{"repair"}}
		configuration.QAModes = []string{"blackbox", "whitebox"}
		gates := []string{"quality"}
		configuration.Gates = &gates
		configuration.Execution = map[string]ExecutionChoice{"qa-blackbox": {Provider: host.Codex, Model: "selected", ReasoningEffort: "high"}, "qa-whitebox": {Provider: host.WorkBuddy, Model: "other", ReasoningEffort: "medium"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := UseConfiguration(root, home, "user", "2", nil); err != nil {
		t.Fatal(err)
	}
	effective, _, err = LoadConfigurations(root, home)
	if err != nil || effective.ConfigurationName != "我的 配置" || effective.ConfigurationSource != user || effective.Values.Capacity != 7 || *effective.Values.TokenCap != 29 || effective.Values.NativeString("codexExecutable") != "configured-cli" {
		t.Fatalf("selected user configuration: %+v %v", effective, err)
	}
	frozen, err := json.Marshal(effective)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveConfiguration(root, home, "project", "另一套", "我的 配置", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := EditConfiguration(root, home, "project", "我的 配置", nil, func(configuration *Configuration) error {
		configuration.QAModes = []string{"whitebox"}
		empty := []string{}
		configuration.Gates = &empty
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	effective, options, err = LoadConfigurations(root, home)
	if err != nil || len(options) != 3 || effective.ConfigurationSource != project || !reflect.DeepEqual(effective.Configuration.QAModes, []string{"whitebox"}) || effective.Configuration.Gates == nil || len(*effective.Configuration.Gates) != 0 {
		t.Fatalf("project override: %+v %v", effective, err)
	}
	var original Effective
	if err := json.Unmarshal(frozen, &original); err != nil || len(original.Configuration.QAModes) != 2 || !reflect.DeepEqual(*original.Configuration.Gates, []string{"quality"}) || original.Configuration.Execution["qa-blackbox"].Provider != host.Codex || original.Configuration.Execution["qa-whitebox"].Provider != host.WorkBuddy || bytes.Contains(frozen, []byte("另一套")) {
		t.Fatalf("frozen selected values changed or included other named configurations: %s %v", frozen, err)
	}
	if _, err := DeleteConfiguration(root, home, "project", "我的 配置", configurationBindings(nil), nil); err != nil {
		t.Fatal(err)
	}
	effective, err = Load(root, home)
	if err != nil || effective.ConfigurationSource != user || len(effective.Configuration.QAModes) != 2 {
		t.Fatalf("deletion did not reveal user value: %+v %v", effective, err)
	}
	if _, err := UseConfiguration(root, home, "project", "另一套", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteConfiguration(root, home, "project", "另一套", configurationBindings(nil), nil); err != nil {
		t.Fatal(err)
	}
	effective, err = Load(root, home)
	if err != nil || effective.ConfigurationName != DefaultConfigurationName {
		t.Fatalf("active deletion did not return to default: %+v %v", effective, err)
	}
	data, err := os.ReadFile(user)
	var raw map[string]json.RawMessage
	if err != nil || json.Unmarshal(data, &raw) != nil || string(raw["capacity"]) != "7" || string(raw["tokenCap"]) != "29" || string(raw["codexExecutable"]) != `"configured-cli"` {
		t.Fatalf("workflow edits changed scalar settings: %s %v", data, err)
	}
	fields, err := EditableFields(effective)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range fields {
		if field.Name == "configuration" || field.Name == "configurations" {
			t.Fatal("named configurations appeared as scalar fields")
		}
	}
}

func TestBuiltinConfigurationCannotBeSavedOverEditedRenamedOrDeleted(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	if _, err := SaveConfiguration(root, home, "project", "甲", "", nil); err != nil {
		t.Fatal(err)
	}
	path := overlayPaths(root, home)[1]
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for operation, run := range map[string]func() error{
		"save": func() error {
			_, err := SaveConfiguration(root, home, "project", DefaultConfigurationName, "甲", nil)
			return err
		},
		"set": func() error {
			_, err := EditConfiguration(root, home, "project", DefaultConfigurationName, nil, func(*Configuration) error { return nil })
			return err
		},
		"set active": func() error {
			_, err := EditConfiguration(root, home, "project", "", nil, func(*Configuration) error { return nil })
			return err
		},
		"delete": func() error {
			_, err := DeleteConfiguration(root, home, "project", "1", configurationBindings(nil), nil)
			return err
		},
		"rename from": func() error {
			_, err := RenameConfiguration(root, home, "project", DefaultConfigurationName, "乙", configurationBindings(nil), nil)
			return err
		},
		"rename onto": func() error {
			_, err := RenameConfiguration(root, home, "project", "甲", DefaultConfigurationName, configurationBindings(nil), nil)
			return err
		},
		"user save": func() error {
			_, err := SaveConfiguration(root, home, "user", DefaultConfigurationName, "", nil)
			return err
		},
		"user delete": func() error {
			_, err := DeleteConfiguration(root, home, "user", DefaultConfigurationName, configurationBindings(nil), nil)
			return err
		},
	} {
		if err := run(); err == nil || !strings.Contains(err.Error(), "built-in configuration") {
			t.Fatalf("%s of the built-in configuration was not rejected: %v", operation, err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("rejected built-in change wrote the project file: %s %v", after, err)
	}
	if _, err := os.Stat(overlayPaths(root, home)[0]); !os.IsNotExist(err) {
		t.Fatalf("rejected built-in change wrote the user file: %v", err)
	}
}

func TestSavedEntriesNamedLikeTheBuiltinAreIgnoredAndReported(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	user, project := overlayPaths(root, home)[0], overlayPaths(root, home)[1]
	putConfig(t, user, `{"configurations":{"默认":{"qaModes":["blackbox"]}}}`)
	putConfig(t, project, `{"configuration":"默认","configurations":{"默认":{"qaModes":["unknown"]},"甲":{"qaModes":["whitebox"]}}}`)
	effective, options, err := LoadConfigurations(root, home)
	if err != nil || effective.ConfigurationName != DefaultConfigurationName || effective.ConfigurationSource != "default" || len(effective.Configuration.QAModes) != 0 {
		t.Fatalf("shadowing entry replaced the built-in configuration: %+v %v", effective, err)
	}
	if len(options) != 2 || options[0].Source != "default" || !reflect.DeepEqual(options[0].Shadowed, []string{user, project}) || options[1].Name != "甲" || options[1].Shadowed != nil {
		t.Fatalf("shadowing sources were not reported on the built-in option: %+v", options)
	}
}

func TestNamedConfigurationInputUsesLoaderValidationBeforeWriting(t *testing.T) {
	for _, body := range []string{
		`{"configuration":"missing"}`,
		`{"configurations":{" ":{}}}`,
		`{"configurations":{"name":null}}`,
		`{"configurations":{"name":{"capacity":8}}}`,
		`{"configurations":{"name":{"qaModes":null}}}`,
		`{"configurations":{"name":{"qaModes":["blackbox","blackbox"]}}}`,
		`{"configurations":{"name":{"qaModes":["other"]}}}`,
		`{"configurations":{"name":{"flowSelection":{"disabled":["unknown"]}}}}`,
		`{"configurations":{"name":{"execution":{"qa":{"model":"chosen"}}}}}`,
		`{"configurations":{"name":{"execution":{"development":{"model":null}}}}}`,
		`{"configurations":{"name":{"execution":{"development":{"provider":"unknown"}}}}}`,
		`{"configurations":{"name":{"gates":["blackbox"]}}}`,
		`{"configurations":{"name":{"gates":["quality","quality"]}}}`,
	} {
		root, home := t.TempDir(), t.TempDir()
		path := filepath.Join(root, ".gates", "config.json")
		putConfig(t, path, body)
		if _, err := Load(root, home); err == nil {
			t.Fatalf("invalid configuration accepted: %s", body)
		}
	}
	root, home := t.TempDir(), t.TempDir()
	if _, err := SaveConfiguration(root, home, "project", " \t", "", nil); err == nil {
		t.Fatal("blank user name accepted")
	}
	if _, err := SaveConfiguration(root, home, "project", "中文 名称", "", nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".gates", "config.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveConfiguration(root, home, "project", "中文 名称", "", nil); err == nil {
		t.Fatal("save silently replaced an existing name")
	}
	if _, err := EditConfiguration(root, home, "project", "中文 名称", nil, func(configuration *Configuration) error {
		configuration.FlowSelection = &FlowSelection{Disabled: []string{"invalid"}}
		return nil
	}); err == nil {
		t.Fatal("invalid edited candidate accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("rejected candidate changed persisted values: %s %v", after, err)
	}
	if strings.Contains(string(after), "tokenCap") || strings.Contains(string(after), "capacity") {
		t.Fatal("configuration save copied unrelated settings")
	}
}

func TestConfigurationExecutionDefaultsKeepPlatformBoundaries(t *testing.T) {
	defaults := ExecutionChoice{Provider: host.Codex, Model: "run-model", ReasoningEffort: "high"}
	configuration := Configuration{Execution: map[string]ExecutionChoice{"development": {Model: "chosen"}, "qa-blackbox": {Provider: host.WorkBuddy, ReasoningEffort: "medium"}}}
	if got := configuration.EffectiveExecutionChoice("development", defaults); got != (ExecutionChoice{Provider: host.Codex, Model: "chosen", ReasoningEffort: "high"}) {
		t.Fatal("same-platform configuration lost run defaults", got)
	}
	if got := configuration.EffectiveExecutionChoice("qa-blackbox", defaults); got != (ExecutionChoice{Provider: host.WorkBuddy, ReasoningEffort: "medium"}) {
		t.Fatal("platform change retained another platform's model", got)
	}
	if got := configuration.EffectiveExecutionChoice("qa-whitebox", defaults); got != defaults {
		t.Fatal("unconfigured execution group changed run defaults", got)
	}
}

func TestNamedConfigurationAndScalarEditsShareOverlayLock(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	position := settingPosition(t, root, home, "capacity")
	if _, err := SaveConfiguration(root, home, "project", "并发", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := UseConfiguration(root, home, "project", "并发", nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".gates", "config.json")
	release, err := coordination.AcquirePathWaiting(path+".lock", "configuration test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	results := make(chan error, 2)
	go func() {
		_, err := EditConfiguration(root, home, "project", "并发", nil, func(configuration *Configuration) error {
			configuration.QAModes = []string{"whitebox"}
			return nil
		})
		results <- err
	}()
	go func() {
		_, err := EditField(root, home, "project", position, "8", false, nil)
		results <- err
	}()
	select {
	case err := <-results:
		t.Fatalf("edit escaped the shared overlay lock: %v", err)
	case <-time.After(100 * time.Millisecond):
	}
	release()
	for i := 0; i < 2; i++ {
		if err := <-results; err != nil {
			t.Fatal(err)
		}
	}
	effective, err := Load(root, home)
	if err != nil || effective.Values.Capacity != 8 || !reflect.DeepEqual(effective.Configuration.QAModes, []string{"whitebox"}) {
		t.Fatalf("concurrent edits lost scalar or workflow choices: %+v %v", effective, err)
	}
}

func TestConfigurationUseChangesOnlyThePointerAcrossScopes(t *testing.T) {
	for _, targetScope := range []string{"user", "project"} {
		t.Run(targetScope, func(t *testing.T) {
			root, home := t.TempDir(), t.TempDir()
			sourceScope := "project"
			if targetScope == "project" {
				sourceScope = "user"
			}
			name := "用户 命名"
			if _, err := SaveConfiguration(root, home, sourceScope, name, "", nil); err != nil {
				t.Fatal(err)
			}
			gates := []string{"quality"}
			want := Configuration{FlowSelection: &FlowSelection{Disabled: []string{"repair"}}, QAModes: []string{"whitebox"}, Gates: &gates, Execution: map[string]ExecutionChoice{"development": {Provider: host.Codex, Model: "chosen", ReasoningEffort: "high"}}}
			if _, err := EditConfiguration(root, home, sourceScope, name, nil, func(configuration *Configuration) error {
				*configuration = want
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := UseConfiguration(root, home, targetScope, name, nil); err != nil {
				t.Fatal(err)
			}
			paths := overlayPaths(root, home)
			targetPath, sourcePath := paths[0], paths[1]
			if targetScope == "project" {
				targetPath, sourcePath = paths[1], paths[0]
			}
			data, err := os.ReadFile(targetPath)
			var raw struct {
				Active string                   `json:"configuration"`
				Named  map[string]Configuration `json:"configurations"`
			}
			if err != nil || json.Unmarshal(data, &raw) != nil || raw.Active != name || len(raw.Named) != 0 {
				t.Fatalf("activation copied a configuration instead of changing only the pointer: %s %v", data, err)
			}
			effective, err := Load(root, home)
			if err != nil || effective.ConfigurationName != name || effective.ConfigurationSource != sourcePath || !reflect.DeepEqual(effective.Configuration, want) {
				t.Fatalf("activation stopped resolving the source layer: %+v %v", effective, err)
			}
			if _, err := UseConfiguration(root, home, targetScope, DefaultConfigurationName, nil); err != nil {
				t.Fatalf("ordinary configuration changes were blocked after cross-scope activation: %v", err)
			}
		})
	}
}

func TestConfigurationUseKeepsSameScopeAndBuiltinValues(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	path := overlayPaths(root, home)[1]
	putConfig(t, path, `{"configurations":{"本项目":{"flowSelection":{},"qaModes":["whitebox"]}}}`)
	if _, err := UseConfiguration(root, home, "project", "本项目", nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	var raw struct {
		Named map[string]map[string]json.RawMessage `json:"configurations"`
	}
	if err != nil || json.Unmarshal(data, &raw) != nil || string(raw.Named["本项目"]["flowSelection"]) != "{}" || len(raw.Named) != 1 {
		t.Fatalf("same-scope activation rewrote its saved value: %s %v", data, err)
	}
	if _, err := UseConfiguration(root, home, "user", DefaultConfigurationName, nil); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(overlayPaths(root, home)[0])
	var fields map[string]json.RawMessage
	if err != nil || json.Unmarshal(data, &fields) != nil || len(fields) != 1 || string(fields["configuration"]) != `"默认"` {
		t.Fatalf("built-in activation materialized a configuration copy: %s %v", data, err)
	}
}

func TestRoundLimitsDefaultToThreeAndAcceptOnlyKnownNodesWithinRange(t *testing.T) {
	for _, node := range RoundLimitNodes {
		if limit := DefaultConfiguration().RoundLimit(node); limit != 3 {
			t.Fatalf("omitted %s round limit = %d, want 3", node, limit)
		}
	}
	for _, body := range []string{
		`{"configurations":{"甲":{"roundLimits":{"development":3}}}}`,
		`{"configurations":{"甲":{"roundLimits":{"repair":0}}}}`,
		`{"configurations":{"甲":{"roundLimits":{"qa-whitebox":65}}}}`,
		`{"configurations":{"甲":{"roundLimits":{"tech-review":null}}}}`,
	} {
		root, home := t.TempDir(), t.TempDir()
		putConfig(t, overlayPaths(root, home)[1], body)
		if _, err := Load(root, home); err == nil {
			t.Fatalf("invalid round limit accepted: %s", body)
		}
	}
	root, home := t.TempDir(), t.TempDir()
	putConfig(t, overlayPaths(root, home)[1], `{"configuration":"甲","configurations":{"甲":{"roundLimits":{"product-review":1,"repair":64}}}}`)
	effective, err := Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, node := range RoundLimitNodes {
		got[node] = effective.Configuration.RoundLimit(node)
	}
	if want := map[string]int{"product-review": 1, "tech-review": 3, "qa-blackbox": 3, "qa-whitebox": 3, "repair": 64}; !reflect.DeepEqual(got, want) {
		t.Fatalf("effective round limits = %v, want %v", got, want)
	}
	clone := CloneConfiguration(effective.Configuration)
	clone.RoundLimits["repair"] = 2
	if effective.Configuration.RoundLimit("repair") != 64 {
		t.Fatal("cloned round limits share the original map")
	}
}

func TestRenameConfigurationChangesOnlyItsScopeAndFollowsTheActivePointer(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	user, project := overlayPaths(root, home)[0], overlayPaths(root, home)[1]
	if _, err := SaveConfiguration(root, home, "project", "甲", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := EditConfiguration(root, home, "project", "甲", nil, func(configuration *Configuration) error {
		configuration.QAModes = []string{"whitebox"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := UseConfiguration(root, home, "project", "甲", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := SaveConfiguration(root, home, "user", "丙", "", nil); err != nil {
		t.Fatal(err)
	}
	userBefore, err := os.ReadFile(user)
	if err != nil {
		t.Fatal(err)
	}
	effective, err := RenameConfiguration(root, home, "project", "甲", "乙", configurationBindings(nil), nil)
	if err != nil || effective.ConfigurationName != "乙" || !reflect.DeepEqual(effective.Configuration.QAModes, []string{"whitebox"}) {
		t.Fatalf("rename did not keep the active value under its new name: %+v %v", effective, err)
	}
	data, err := os.ReadFile(project)
	var raw struct {
		Active string                   `json:"configuration"`
		Named  map[string]Configuration `json:"configurations"`
	}
	if err != nil || json.Unmarshal(data, &raw) != nil || raw.Active != "乙" || len(raw.Named) != 1 || !reflect.DeepEqual(raw.Named["乙"].QAModes, []string{"whitebox"}) {
		t.Fatalf("project file after rename: %s %v", data, err)
	}
	projectBefore := data
	for reason, run := range map[string]func() error{
		"merged-view duplicate": func() error {
			_, err := RenameConfiguration(root, home, "project", "乙", "丙", configurationBindings(nil), nil)
			return err
		},
		"other scope": func() error {
			_, err := RenameConfiguration(root, home, "project", "丙", "丁", configurationBindings(nil), nil)
			return err
		},
		"empty name": func() error {
			_, err := RenameConfiguration(root, home, "project", "乙", " ", configurationBindings(nil), nil)
			return err
		},
	} {
		if err := run(); err == nil {
			t.Fatalf("rename with %s accepted", reason)
		}
	}
	if _, err := RenameConfiguration(root, home, "project", "丙", "丁", configurationBindings(nil), nil); err == nil || !strings.Contains(err.Error(), `configuration "丙" source is `) || !strings.Contains(err.Error(), "not the project scope") {
		t.Fatalf("rename outside its scope did not report the scope: %v", err)
	}
	projectAfter, _ := os.ReadFile(project)
	userAfter, _ := os.ReadFile(user)
	if !bytes.Equal(projectBefore, projectAfter) || !bytes.Equal(userBefore, userAfter) {
		t.Fatal("rejected rename changed a configuration file")
	}
}

func TestDeleteAndRenameRejectConfigurationsBoundToUnfinishedRuns(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	if _, err := SaveConfiguration(root, home, "project", "甲", "", nil); err != nil {
		t.Fatal(err)
	}
	path := overlayPaths(root, home)[1]
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	bound := configurationBindings(map[string][]string{"甲": {"run-one", "run-two"}})
	_, deleteErr := DeleteConfiguration(root, home, "project", "甲", bound, nil)
	_, renameErr := RenameConfiguration(root, home, "project", "2", "乙", bound, nil)
	for _, err := range []error{deleteErr, renameErr} {
		if err == nil || !strings.Contains(err.Error(), "run-one") || !strings.Contains(err.Error(), "run-two") {
			t.Fatalf("bound configuration change was not rejected with its runs: %v", err)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatalf("rejected bound change wrote the file: %s %v", after, err)
	}
	if _, err := RenameConfiguration(root, home, "project", "甲", "乙", configurationBindings(map[string][]string{"丙": {"run-one"}}), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteConfiguration(root, home, "project", "乙", configurationBindings(map[string][]string{"甲": {"run-one"}}), nil); err != nil {
		t.Fatal(err)
	}
}

func configurationBindings(bound map[string][]string) func() (map[string][]string, error) {
	return func() (map[string][]string, error) { return bound, nil }
}

func TestConfigurationCopiesPreserveValuesAndGatesShape(t *testing.T) {
	empty, gates := []string{}, []string{"quality", "complexity"}
	for shape, selection := range map[string]*[]string{"all": nil, "none": &empty, "subset": &gates} {
		t.Run(shape, func(t *testing.T) {
			root, home := t.TempDir(), t.TempDir()
			if _, err := SaveConfiguration(root, home, "user", "A", "", nil); err != nil {
				t.Fatal(err)
			}
			if _, err := EditConfiguration(root, home, "user", "A", nil, func(configuration *Configuration) error {
				configuration.FlowSelection = &FlowSelection{Disabled: []string{"repair"}}
				configuration.QAModes = []string{"blackbox", "whitebox"}
				configuration.Gates = selection
				configuration.Execution = map[string]ExecutionChoice{}
				for i, node := range definition.ExecutionNodes() {
					configuration.Execution[node.ID] = ExecutionChoice{Provider: host.Codex, Model: fmt.Sprintf("model-%d", i), ReasoningEffort: "high"}
				}
				configuration.RoundLimits = map[string]int{}
				for i, node := range RoundLimitNodes {
					configuration.RoundLimits[node] = i + 1
				}
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := UseConfiguration(root, home, "user", "A", nil); err != nil {
				t.Fatal(err)
			}
			before, options, err := LoadConfigurations(root, home)
			if err != nil {
				t.Fatal(err)
			}
			original, err := SelectConfiguration(options, "A")
			if err != nil {
				t.Fatal(err)
			}
			userBefore, err := os.ReadFile(before.Files[0])
			if err != nil {
				t.Fatal(err)
			}
			name, err := SaveConfiguration(root, home, "project", "", "A", before.Revisions)
			if err != nil || name != "A 副本" {
				t.Fatalf("copy name: %q %v", name, err)
			}
			after, options, err := LoadConfigurations(root, home)
			if err != nil {
				t.Fatal(err)
			}
			copied, err := SelectConfiguration(options, name)
			if err != nil || copied.Active || copied.Source != after.Files[1] || after.ConfigurationName != "A" || !reflect.DeepEqual(original.Configuration, copied.Configuration) {
				t.Fatalf("copy changed choices, shape or activation: %+v %+v %v", original, copied, err)
			}
			userAfter, err := os.ReadFile(after.Files[0])
			if err != nil || !bytes.Equal(userBefore, userAfter) {
				t.Fatalf("copy changed its source file: %s %v", userAfter, err)
			}
			cloned := CloneConfiguration(copied.Configuration)
			cloned.FlowSelection.Disabled[0] = "development"
			cloned.QAModes[0] = "whitebox"
			cloned.Execution["development"] = ExecutionChoice{Model: "different"}
			cloned.RoundLimits["repair"] = 64
			if cloned.Gates != nil {
				if len(*cloned.Gates) != 0 {
					(*cloned.Gates)[0] = "other"
				}
				*cloned.Gates = append(*cloned.Gates, "extra")
			}
			if !reflect.DeepEqual(original.Configuration, copied.Configuration) {
				t.Fatal("cloning aliased a saved configuration's slices, pointers or maps")
			}
		})
	}
}

func TestConfigurationCopiesSerializeNamesAcrossLayers(t *testing.T) {
	for _, checked := range []bool{false, true} {
		t.Run(fmt.Sprintf("revisions=%t", checked), func(t *testing.T) {
			root, home := t.TempDir(), t.TempDir()
			if _, err := SaveConfiguration(root, home, "user", "A", "", nil); err != nil {
				t.Fatal(err)
			}
			var expected map[string]string
			if checked {
				var err error
				expected, err = Revisions(root, home)
				if err != nil {
					t.Fatal(err)
				}
			}
			type result struct {
				name string
				err  error
			}
			const copies = 6
			start, results := make(chan struct{}), make(chan result, copies)
			for i := 0; i < copies; i++ {
				scope := []string{"user", "project"}[i%2]
				go func() {
					<-start
					name, err := SaveConfiguration(root, home, scope, "", "A", expected)
					results <- result{name, err}
				}()
			}
			close(start)
			names := map[string]bool{}
			for i := 0; i < copies; i++ {
				got := <-results
				if got.err != nil {
					if !checked || !strings.Contains(got.err.Error(), "changed") || got.name != "" {
						t.Fatalf("copy failed or returned an uncommitted name: %+v", got)
					}
					continue
				}
				if strings.TrimSpace(got.name) == "" || names[got.name] {
					t.Fatalf("concurrent copies reused a name: %q", got.name)
				}
				names[got.name] = true
			}
			count := copies
			if checked {
				count = 1
			}
			effective, options, err := LoadConfigurations(root, home)
			if err != nil || len(names) != count || len(options) != count+2 || effective.ConfigurationName != DefaultConfigurationName {
				t.Fatalf("concurrent copies lost writes or bypassed revisions: %v %+v %v", names, options, err)
			}
			for i := 1; i <= count; i++ {
				name := "A 副本"
				if i > 1 {
					name += " " + strconv.Itoa(i)
				}
				option, err := SelectConfiguration(options, name)
				if err != nil || !names[name] || !reflect.DeepEqual(option.Configuration, DefaultConfiguration()) {
					t.Fatalf("committed copy %q: %+v %v", name, option, err)
				}
			}
		})
	}
}

func TestUseRepairsADanglingActivePointer(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	user, project := overlayPaths(root, home)[0], overlayPaths(root, home)[1]
	capacity := settingPosition(t, root, home, "capacity")
	putConfig(t, user, `{"configurations":{"用户层":{"qaModes":["blackbox"]}}}`)
	userBefore, err := os.ReadFile(user)
	if err != nil {
		t.Fatal(err)
	}
	dangling := `{"capacity":5,"configuration":"已删除","configurations":{"甲":{"qaModes":["whitebox"]}}}`
	putConfig(t, project, dangling)
	if _, err := Load(root, home); !errors.Is(err, ErrActiveConfigurationMissing) || err.Error() != `configuration "已删除" does not exist` {
		t.Fatalf("dangling pointer error = %v", err)
	}
	if _, err := EditField(root, home, "project", capacity, "6", false, nil); !errors.Is(err, ErrActiveConfigurationMissing) {
		t.Fatalf("an edit that keeps the pointer dangling was accepted: %v", err)
	}
	if _, err := UseConfiguration(root, home, "project", "不存在", nil); err == nil {
		t.Fatal("repair accepted a name saved in no layer")
	}
	if data, _ := os.ReadFile(project); string(data) != dangling {
		t.Fatalf("rejected repair changed the file: %s", data)
	}
	for _, target := range []string{"甲", "用户层", DefaultConfigurationName} {
		putConfig(t, project, dangling)
		read, _, err := LoadConfigurations(root, home)
		if !errors.Is(err, ErrActiveConfigurationMissing) {
			t.Fatalf("repair did not start at a dangling pointer: %v", err)
		}
		effective, err := UseConfiguration(root, home, "project", target, read.Revisions)
		if err != nil || effective.ConfigurationName != target || effective.Values.Capacity != 5 {
			t.Fatalf("repair to %s: %+v %v", target, effective, err)
		}
		var want, got map[string]any
		data, err := os.ReadFile(project)
		if err != nil || json.Unmarshal([]byte(dangling), &want) != nil || json.Unmarshal(data, &got) != nil {
			t.Fatalf("repair file content: %s %v", data, err)
		}
		want["configuration"] = target
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("pointer repair copied or changed configuration values: %s", data)
		}
		if after, err := os.ReadFile(user); err != nil || !bytes.Equal(after, userBefore) {
			t.Fatalf("pointer repair rewrote its source overlay: %s %v", after, err)
		}
	}
	for _, broken := range []string{
		`{"configuration":"已删除","configurations":{"甲":{}}`,
		`{"configuration":"已删除","configurations":{"甲":{}},"unknown":1}`,
	} {
		putConfig(t, project, broken)
		if _, err := UseConfiguration(root, home, "project", "甲", nil); err == nil || errors.Is(err, ErrActiveConfigurationMissing) {
			t.Fatalf("load failure %s took the pointer repair path: %v", broken, err)
		}
		if data, _ := os.ReadFile(project); string(data) != broken {
			t.Fatalf("load failure was rewritten: %s", data)
		}
	}
}

func TestConfigurationQATogglesNormalizeEffectiveSelection(t *testing.T) {
	configuration := Configuration{
		FlowSelection: &FlowSelection{Disabled: []string{"repair", "qa"}},
		QAModes:       []string{"blackbox", "whitebox"},
	}
	if configuration.QAModeEnabled("blackbox") || configuration.QAModeEnabled("whitebox") {
		t.Fatal("disabled QA exposed latent modes")
	}
	configuration.SetQAMode("whitebox", true)
	if !reflect.DeepEqual(configuration.QAModes, []string{"whitebox"}) || !reflect.DeepEqual(configuration.FlowSelection.Disabled, []string{"repair"}) {
		t.Fatalf("one QA mode restored a latent mode or changed another node: %+v", configuration)
	}
	configuration.SetFlowNodeEnabled("qa", false)
	if len(configuration.QAModes) != 0 || !reflect.DeepEqual(configuration.FlowSelection.Disabled, []string{"repair", "qa"}) {
		t.Fatalf("aggregate QA disable kept selected modes: %+v", configuration)
	}
	configuration.SetFlowNodeEnabled("qa", false)
	if len(configuration.QAModes) != 0 || !reflect.DeepEqual(configuration.FlowSelection.Disabled, []string{"repair", "qa"}) {
		t.Fatalf("repeated aggregate disable changed the selection: %+v", configuration)
	}
	configuration.SetFlowNodeEnabled("qa", true)
	if !reflect.DeepEqual(configuration.QAModes, []string{"blackbox", "whitebox"}) || !reflect.DeepEqual(configuration.FlowSelection.Disabled, []string{"repair"}) {
		t.Fatalf("aggregate enable did not select both QA groups: %+v", configuration)
	}
	configuration.SetFlowNodeEnabled("gate", false)
	if !reflect.DeepEqual(configuration.QAModes, []string{"blackbox", "whitebox"}) || !reflect.DeepEqual(configuration.FlowSelection.Disabled, []string{"repair", "gate"}) {
		t.Fatalf("another node changed QA selection: %+v", configuration)
	}
	configuration = DefaultConfiguration()
	configuration.SetQAMode("blackbox", false)
	if len(configuration.QAModes) != 0 || !reflect.DeepEqual(configuration.FlowSelection.Disabled, []string{"qa"}) {
		t.Fatalf("closing an empty selection did not disable QA: %+v", configuration)
	}
}
