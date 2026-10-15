package settings

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/coordination"
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
	if _, err := SaveConfiguration(root, home, "user", "我的 配置", "1"); err != nil {
		t.Fatal(err)
	}
	if _, err := EditConfiguration(root, home, "user", "我的 配置", func(configuration *Configuration) error {
		configuration.FlowSelection = &FlowSelection{Disabled: []string{"repair"}}
		configuration.QAModes = []string{"blackbox", "whitebox"}
		gates := []string{"quality"}
		configuration.Gates = &gates
		configuration.Execution = map[string]ExecutionChoice{"qa-blackbox": {Provider: host.Codex, Model: "selected", ReasoningEffort: "high"}, "qa-whitebox": {Provider: host.WorkBuddy, Model: "other", ReasoningEffort: "medium"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := UseConfiguration(root, home, "user", "2"); err != nil {
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
	if _, err := SaveConfiguration(root, home, "project", "另一套", "我的 配置"); err != nil {
		t.Fatal(err)
	}
	if _, err := EditConfiguration(root, home, "project", "我的 配置", func(configuration *Configuration) error {
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
	if _, err := DeleteConfiguration(root, home, "project", "我的 配置"); err != nil {
		t.Fatal(err)
	}
	effective, err = Load(root, home)
	if err != nil || effective.ConfigurationSource != user || len(effective.Configuration.QAModes) != 2 {
		t.Fatalf("deletion did not reveal user value: %+v %v", effective, err)
	}
	if _, err := UseConfiguration(root, home, "project", "另一套"); err != nil {
		t.Fatal(err)
	}
	if _, err := DeleteConfiguration(root, home, "project", "另一套"); err != nil {
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

func TestDefaultConfigurationOverrideCanBeRemoved(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	for _, scope := range []string{"user", "project"} {
		if _, err := EditConfiguration(root, home, scope, DefaultConfigurationName, func(configuration *Configuration) error {
			configuration.QAModes = []string{"blackbox"}
			if scope == "project" {
				configuration.QAModes = []string{"whitebox"}
			}
			return nil
		}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := DeleteConfiguration(root, home, "project", "1"); err != nil {
		t.Fatal(err)
	}
	effective, err := Load(root, home)
	if err != nil || !reflect.DeepEqual(effective.Configuration.QAModes, []string{"blackbox"}) {
		t.Fatalf("removed project default did not inherit: %+v %v", effective, err)
	}
	if _, err := DeleteConfiguration(root, home, "user", DefaultConfigurationName); err != nil {
		t.Fatal(err)
	}
	effective, err = Load(root, home)
	if err != nil || effective.ConfigurationSource != "default" || len(effective.Configuration.QAModes) != 0 {
		t.Fatalf("removed user default did not inherit built-in: %+v %v", effective, err)
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
	if _, err := SaveConfiguration(root, home, "project", "", ""); err == nil {
		t.Fatal("missing user name accepted")
	}
	if _, err := SaveConfiguration(root, home, "project", "中文 名称", ""); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".gates", "config.json")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := SaveConfiguration(root, home, "project", "中文 名称", ""); err == nil {
		t.Fatal("save silently replaced an existing name")
	}
	if _, err := EditConfiguration(root, home, "project", "中文 名称", func(configuration *Configuration) error {
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
	path := filepath.Join(root, ".gates", "config.json")
	release, err := coordination.AcquirePathWaiting(path+".lock", "configuration test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(release)
	results := make(chan error, 2)
	go func() {
		_, err := EditConfiguration(root, home, "project", DefaultConfigurationName, func(configuration *Configuration) error {
			configuration.QAModes = []string{"whitebox"}
			return nil
		})
		results <- err
	}()
	go func() {
		_, err := EditField(root, home, "project", position, "8", false)
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

func TestConfigurationUseCopiesCrossScopeValue(t *testing.T) {
	for _, targetScope := range []string{"user", "project"} {
		t.Run(targetScope, func(t *testing.T) {
			root, otherRoot, home := t.TempDir(), t.TempDir(), t.TempDir()
			sourceScope := "project"
			if targetScope == "project" {
				sourceScope = "user"
			}
			name := "用户 命名"
			if _, err := SaveConfiguration(root, home, sourceScope, name, ""); err != nil {
				t.Fatal(err)
			}
			gates := []string{"quality"}
			want := Configuration{FlowSelection: &FlowSelection{Disabled: []string{"repair"}}, QAModes: []string{"whitebox"}, Gates: &gates, Execution: map[string]ExecutionChoice{"development": {Provider: host.Codex, Model: "chosen", ReasoningEffort: "high"}}}
			if _, err := EditConfiguration(root, home, sourceScope, name, func(configuration *Configuration) error {
				*configuration = want
				return nil
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := UseConfiguration(root, home, targetScope, name); err != nil {
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
			if err != nil || json.Unmarshal(data, &raw) != nil || raw.Active != name || !reflect.DeepEqual(raw.Named[name], want) {
				t.Fatalf("activation did not persist its selected complete value in the requested scope: %s %v", data, err)
			}
			if err := os.Remove(sourcePath); err != nil {
				t.Fatal(err)
			}
			selectedRoot := root
			if targetScope == "user" {
				selectedRoot = otherRoot
			}
			effective, err := Load(selectedRoot, home)
			if err != nil || effective.ConfigurationName != name || effective.ConfigurationSource != targetPath || !reflect.DeepEqual(effective.Configuration, want) {
				t.Fatalf("activated configuration still depends on its source scope: %+v %v", effective, err)
			}
			if _, err := UseConfiguration(selectedRoot, home, targetScope, DefaultConfigurationName); err != nil {
				t.Fatalf("ordinary configuration changes were blocked after cross-scope activation: %v", err)
			}
		})
	}
}

func TestConfigurationUseKeepsSameScopeAndBuiltinValues(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	path := overlayPaths(root, home)[1]
	putConfig(t, path, `{"configurations":{"本项目":{"flowSelection":{},"qaModes":["whitebox"]}}}`)
	if _, err := UseConfiguration(root, home, "project", "本项目"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	var raw struct {
		Named map[string]map[string]json.RawMessage `json:"configurations"`
	}
	if err != nil || json.Unmarshal(data, &raw) != nil || string(raw.Named["本项目"]["flowSelection"]) != "{}" || len(raw.Named) != 1 {
		t.Fatalf("same-scope activation rewrote its saved value: %s %v", data, err)
	}
	if _, err := UseConfiguration(root, home, "user", DefaultConfigurationName); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(overlayPaths(root, home)[0])
	var fields map[string]json.RawMessage
	if err != nil || json.Unmarshal(data, &fields) != nil || len(fields) != 1 || string(fields["configuration"]) != `"默认"` {
		t.Fatalf("built-in activation materialized a configuration copy: %s %v", data, err)
	}
}
