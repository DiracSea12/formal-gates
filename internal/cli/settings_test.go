package cli

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/host"
	"formal-gates/internal/settings"
	"formal-gates/internal/validate"
)

func settingsCommand(t *testing.T, root, home string, args ...string) []byte {
	t.Helper()
	var out, stderr bytes.Buffer
	command := append([]string{args[0], "--root", root, "--home", home}, args[1:]...)
	if code, err := runSettings(command, IO{Stdout: &out, Stderr: &stderr, Stdin: strings.NewReader("")}); code != 0 || err != nil {
		t.Fatalf("settings %v: %d %v %s", args, code, err, stderr.String())
	}
	return out.Bytes()
}

func TestSettingsScalarNamesUseThePublishedFieldEditor(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	for _, test := range []struct{ name, value string }{{"panelSelection", "false"}, {"panelSelection", "true"}, {"panelSelection", "false"}, {"panelIdleTimeout", "42"}} {
		var out, stderr bytes.Buffer
		code, err := runSettings([]string{"set", test.name, test.value, "--root", root, "--home", home}, IO{Stdout: &out, Stderr: &stderr, Stdin: strings.NewReader("")})
		if err != nil || code != 0 {
			t.Fatal("named scalar edit failed", test, code, err)
		}
		written, err := settings.Load(root, home)
		if err != nil {
			t.Fatal(err)
		}
		if test.name == "panelSelection" && strconv.FormatBool(written.Values.PanelSelection) != test.value ||
			test.name == "panelIdleTimeout" && strconv.Itoa(written.Values.PanelIdleTimeout) != test.value {
			t.Fatalf("named scalar value was not persisted: %s=%s, got %+v", test.name, test.value, written.Values)
		}
	}
	effective, err := settings.Load(root, home)
	if err != nil || effective.Values.PanelSelection || effective.Values.PanelIdleTimeout != 42 {
		t.Fatal("named scalar edit did not use Settings Values", effective.Values, err)
	}
	fields, err := settings.EditableFields(effective)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range fields {
		if field.Name == "panelSelection" {
			settingsCommand(t, root, home, "set", "--field", strconv.Itoa(field.Position), "--value", "true")
		}
	}
	effective, err = settings.Load(root, home)
	if err != nil || !effective.Values.PanelSelection || effective.Values.PanelIdleTimeout != 42 {
		t.Fatal("position and name editors did not share one owner", effective.Values, err)
	}
}

func TestSettingsQAModesFollowDisabledAndNormalizeEdits(t *testing.T) {
	type shownQA struct {
		QAModes []struct {
			Name    string `json:"name"`
			Enabled bool   `json:"enabled"`
		} `json:"qaModes"`
	}
	root, home := t.TempDir(), t.TempDir()
	path := filepath.Join(root, ".gates", "config.json")
	check := func(data []byte, modes []string, disabled bool) settings.Configuration {
		t.Helper()
		var shown shownQA
		if err := json.Unmarshal(data, &shown); err != nil {
			t.Fatal(err)
		}
		for _, mode := range shown.QAModes {
			if mode.Enabled != slices.Contains(modes, mode.Name) {
				t.Errorf("displayed QA mode %s disagrees with the enabled choices: %s", mode.Name, data)
			}
		}
		_, options, err := settings.LoadConfigurations(root, home)
		selected, selectErr := settings.SelectConfiguration(options, "choice")
		if err != nil || selectErr != nil {
			t.Fatal(err, selectErr)
		}
		configuration := selected.Configuration
		qaDisabled := configuration.FlowSelection != nil && slices.Contains(configuration.FlowSelection.Disabled, "qa")
		if qaDisabled != disabled {
			t.Errorf("QA node disabled=%t, want %t: %+v", qaDisabled, disabled, configuration)
		}
		return configuration
	}
	settingsCommand(t, root, home, "save", "--name", "choice")
	check(settingsCommand(t, root, home, "show", "--configuration", "choice"), nil, false)
	for _, step := range []struct {
		mode     string
		enabled  string
		modes    []string
		disabled bool
	}{
		{"blackbox", "true", []string{"blackbox"}, false},
		{"whitebox", "true", []string{"blackbox", "whitebox"}, false},
		{"blackbox", "false", []string{"whitebox"}, false},
		{"whitebox", "false", nil, true},
		{"blackbox", "true", []string{"blackbox"}, false},
	} {
		configuration := check(settingsCommand(t, root, home, "set", "--configuration", "choice", "--qa-mode", step.mode, "--enabled", step.enabled), step.modes, step.disabled)
		if !slices.Equal(configuration.QAModes, step.modes) {
			t.Fatalf("saved QA modes disagree with the selected choices: %+v", configuration)
		}
	}
	disabled := check(settingsCommand(t, root, home, "set", "--configuration", "choice", "--flow-node", "qa", "--enabled", "false"), nil, true)
	if len(disabled.QAModes) != 0 {
		t.Fatalf("aggregate QA disable kept selected modes: %+v", disabled)
	}
	configuration := check(settingsCommand(t, root, home, "set", "--configuration", "choice", "--flow-node", "qa", "--enabled", "true"), []string{"blackbox", "whitebox"}, false)
	if !slices.Equal(configuration.QAModes, []string{"blackbox", "whitebox"}) {
		t.Fatalf("aggregate QA enable did not persist both choices: %+v", configuration)
	}
	for _, mode := range []string{"blackbox", "whitebox"} {
		mustWriteCLI(t, path, `{"configurations":{"choice":{"qaModes":["blackbox","whitebox"],"flowSelection":{"disabled":["qa","repair"]}}}}`)
		check(settingsCommand(t, root, home, "show", "--configuration", "choice"), nil, true)
		check(settingsCommand(t, root, home, "set", "--configuration", "choice", "--qa-mode", mode, "--enabled", "true"), []string{mode}, false)
		data, err := os.ReadFile(path)
		var raw struct {
			Configurations map[string]settings.Configuration `json:"configurations"`
		}
		if err != nil || json.Unmarshal(data, &raw) != nil || !reflect.DeepEqual(raw.Configurations["choice"].QAModes, []string{mode}) || !reflect.DeepEqual(raw.Configurations["choice"].FlowSelection.Disabled, []string{"repair"}) {
			t.Fatalf("enabling one mode restored stale choices or changed unrelated nodes: %s %v", data, err)
		}
	}
}

func TestSettingsShowRevisionsDescribeDisplayedBytes(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	path := filepath.Join(root, ".gates", "config.json")
	changed := false
	position := 0
	platform := &host.Descriptor{ID: "settings-snapshot-reader", Configuration: []host.ConfigField{{
		Name: "settingsSnapshotInput",
		Validate: func(string) error {
			if changed {
				return nil
			}
			changed = true
			_, err := settings.EditField(root, home, "project", position, "after", false, nil)
			return err
		},
	}}}
	if err := host.Register(platform); err != nil {
		t.Fatal(err)
	}
	effective, err := settings.Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	fields, err := settings.EditableFields(effective)
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range fields {
		if field.Name == "model" {
			position = field.Position
		}
	}
	before := `{"model":"before","settingsSnapshotInput":"read"}`
	mustWriteCLI(t, path, before)
	var shown struct {
		Effective settings.Effective `json:"effective"`
		Revisions map[string]string  `json:"revisions"`
	}
	// The adapter validator schedules a regular field edit after show reads
	// the file but before it finishes parsing those bytes.
	data := settingsCommand(t, root, home, "show")
	if err := json.Unmarshal(data, &shown); err != nil {
		t.Fatal(err)
	}
	after, err := settings.Load(root, home)
	if err != nil || !changed || after.Values.Model != "after" {
		t.Fatalf("concurrent regular edit did not run: %+v %v", after.Values, err)
	}
	if shown.Effective.Values.Model != "before" || shown.Revisions["project"] != fmt.Sprintf("%x", sha256.Sum256([]byte(before))) || shown.Revisions["user"] != "" {
		t.Fatalf("show paired old values with a different file's revision: %s", data)
	}
	emptyRoot, emptyHome := t.TempDir(), t.TempDir()
	settingsCommand(t, emptyRoot, emptyHome, "show")
	for _, dir := range []string{filepath.Join(emptyRoot, ".gates"), filepath.Join(emptyHome, ".config")} {
		if _, err := os.Stat(dir); !os.IsNotExist(err) {
			t.Fatalf("settings show created state: %s %v", dir, err)
		}
	}
}

func TestSettingsNamedConfigurationCommandsUsePublishedChoices(t *testing.T) {
	t.Parallel()
	root, home := t.TempDir(), t.TempDir()
	shown := settingsCommand(t, root, home, "show")
	var view struct {
		Effective      settings.Effective             `json:"effective"`
		Fields         []settings.EditableField       `json:"fields"`
		QAModes        []json.RawMessage              `json:"qaModes"`
		RoundLimits    []json.RawMessage              `json:"roundLimits"`
		Configurations []settings.ConfigurationOption `json:"configurations"`
		FlowNodes      []struct {
			Node int `json:"node"`
		} `json:"flowNodes"`
		ExecutionNodes []struct {
			Node int `json:"node"`
		} `json:"executionNodes"`
	}
	if err := json.Unmarshal(shown, &view); err != nil || !view.Effective.Values.PanelSelection || len(view.Fields) == 0 || len(view.QAModes) == 0 || len(view.RoundLimits) != len(settings.RoundLimitNodes) || len(view.Configurations) != 2 || view.Configurations[0].Name != "默认" || len(view.FlowNodes) != len(definition.MainFlowNodes()) || len(view.ExecutionNodes) != len(definition.ExecutionNodes()) {
		t.Fatalf("initial named choices: %s %v", shown, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gates")); !os.IsNotExist(err) {
		t.Fatal("settings show created state", err)
	}
	settingsCommand(t, root, home, "save", "--scope", "user", "--name", "中文 名称", "--configuration", "1")
	settingsCommand(t, root, home, "set", "--configuration", "2", "--flow-node", strconv.Itoa(view.FlowNodes[0].Node), "--enabled", "false")
	settingsCommand(t, root, home, "set", "--configuration", "中文 名称", "--qa-mode", "1", "--enabled", "true")
	settingsCommand(t, root, home, "set", "--configuration", "中文 名称", "--qa-mode", "whitebox", "--enabled", "true")
	settingsCommand(t, root, home, "set", "--configuration", "中文 名称", "--no-gates")
	settingsCommand(t, root, home, "use", "--configuration", "2")
	effective, err := settings.Load(root, home)
	if err != nil || !effective.Values.PanelSelection || effective.ConfigurationName != "中文 名称" || !reflect.DeepEqual(effective.Configuration.FlowSelection.Disabled, []string{definition.MainFlowNodes()[0].ID}) || !reflect.DeepEqual(effective.Configuration.QAModes, []string{"blackbox", "whitebox"}) || effective.Configuration.Gates == nil || len(*effective.Configuration.Gates) != 0 {
		t.Fatalf("configuration choices: %+v %v", effective, err)
	}
	settingsCommand(t, root, home, "delete", "--configuration", "中文 名称")
	effective, err = settings.Load(root, home)
	if err != nil || effective.ConfigurationName != "默认" {
		t.Fatalf("delete did not select default: %+v %v", effective, err)
	}
	for _, args := range [][]string{
		{"set", "--configuration", "默认", "--flow-node", "1", "--enabled", "false"},
		{"delete", "--configuration", "默认"},
		{"rename", "--configuration", "默认", "--name", "改名"},
		{"save", "--name", "默认"},
	} {
		if err := settingsCommandError(root, home, args...); err == nil || !strings.Contains(err.Error(), "built-in configuration") {
			t.Fatalf("settings %v changed the built-in configuration: %v", args, err)
		}
	}
	effective, err = settings.Load(root, home)
	if err != nil || effective.Configuration.FlowSelection != nil {
		t.Fatalf("built-in configuration changed: %+v %v", effective, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gates", "engine")); !os.IsNotExist(err) {
		t.Fatal("default configuration commands created workflow state", err)
	}
}

func settingsCommandError(root, home string, args ...string) error {
	var out, stderr bytes.Buffer
	command := append([]string{args[0], "--root", root, "--home", home}, args[1:]...)
	_, err := runSettings(command, IO{Stdout: &out, Stderr: &stderr, Stdin: strings.NewReader("")})
	return err
}

func TestSettingsDeleteAndRenameRequireTheSelectedSource(t *testing.T) {
	for _, operation := range []string{"delete", "rename"} {
		for _, selection := range []string{"A", "2"} {
			t.Run(operation+"/"+selection, func(t *testing.T) {
				root, home := t.TempDir(), t.TempDir()
				settingsCommand(t, root, home, "save", "--scope", "user", "--name", "A")
				settingsCommand(t, root, home, "use", "--scope", "user", "--configuration", "A")
				settingsCommand(t, root, home, "set", "--configuration", "A", "--round-limit", "repair", "--value", "5")
				settingsCommand(t, root, home, "use", "--configuration", "A")
				effective, options, err := settings.LoadConfigurations(root, home)
				if err != nil {
					t.Fatal(err)
				}
				selected, err := settings.SelectConfiguration(options, selection)
				if err != nil || selected.Source != effective.Files[1] || selected.Configuration.RoundLimit("repair") != 5 {
					t.Fatalf("selected project configuration: %+v %v", selected, err)
				}
				before := make([][]byte, len(effective.Files))
				for i, path := range effective.Files {
					before[i], err = os.ReadFile(path)
					if err != nil {
						t.Fatal(err)
					}
				}
				args := []string{operation, "--scope", "user", "--configuration", selection,
					"--revision", "user=" + effective.Revisions["user"], "--revision", "project=" + effective.Revisions["project"]}
				if operation == "rename" {
					args = append(args, "--name", "B")
				}
				if err := settingsCommandError(root, home, args...); err == nil || !strings.Contains(err.Error(), "source") || !strings.Contains(err.Error(), "user") {
					t.Fatalf("wrong-scope %s did not identify the source mismatch: %v", operation, err)
				}
				for i, path := range effective.Files {
					after, err := os.ReadFile(path)
					if err != nil || !bytes.Equal(before[i], after) {
						t.Fatalf("rejected %s changed %s: %s %v", operation, path, after, err)
					}
				}
				args[2] = "project"
				settingsCommand(t, root, home, args...)
				effective, options, err = settings.LoadConfigurations(root, home)
				if err != nil {
					t.Fatal(err)
				}
				revealed, err := settings.SelectConfiguration(options, "A")
				if err != nil || revealed.Source != effective.Files[0] || revealed.Configuration.RoundLimit("repair") != 3 {
					t.Fatalf("source change did not reveal the user configuration: %+v %v", revealed, err)
				}
				wantActive := settings.DefaultConfigurationName
				if operation == "rename" {
					wantActive = "B"
					renamed, err := settings.SelectConfiguration(options, "B")
					if err != nil || renamed.Source != effective.Files[1] || renamed.Configuration.RoundLimit("repair") != 5 {
						t.Fatalf("rename lost the selected project value: %+v %v", renamed, err)
					}
				}
				userAfter, err := os.ReadFile(effective.Files[0])
				if err != nil || !bytes.Equal(before[0], userAfter) || effective.ConfigurationName != wantActive {
					t.Fatalf("source change altered user bytes or the active pointer: %+v %v", effective, err)
				}
			})
		}
	}
}

func TestSettingsCopyUsesUniqueNamesWithoutActivatingThem(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	settingsCommand(t, root, home, "save", "--scope", "user", "--name", "A")
	settingsCommand(t, root, home, "use", "--configuration", "A")
	type view struct {
		Effective      settings.Effective             `json:"effective"`
		Name           string                         `json:"configuration"`
		Configurations []settings.ConfigurationOption `json:"configurations"`
	}
	for _, copy := range []struct{ scope, from, name string }{
		{"user", "A", "A 副本"},
		{"project", "A", "A 副本 2"},
		{"user", "A", "A 副本 3"},
		{"project", "1", "默认 副本"},
		{"user", "默认", "默认 副本 2"},
		{"project", "", "A 副本 4"},
	} {
		args := []string{"save", "--scope", copy.scope}
		if copy.from != "" {
			args = append(args, "--configuration", copy.from)
		}
		data := settingsCommand(t, root, home, args...)
		var shown view
		if err := json.Unmarshal(data, &shown); err != nil {
			t.Fatal(err)
		}
		selected, err := settings.SelectConfiguration(shown.Configurations, shown.Name)
		if err != nil || shown.Name != copy.name || selected.Active || shown.Effective.ConfigurationName != "A" {
			t.Fatalf("copy display or activation: %s", data)
		}
	}
	effective, err := settings.Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	before := map[string][]byte{}
	for _, path := range effective.Files {
		before[path], err = os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, name := range []string{"", " \t", "A", "A 副本"} {
		if err := settingsCommandError(root, home, "save", "--configuration", "A", "--name", name); err == nil {
			t.Fatalf("explicit blank or duplicate name %q accepted", name)
		}
	}
	for path, want := range before {
		after, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(want, after) {
			t.Fatalf("rejected copy changed %s: %s %v", path, after, err)
		}
	}
}

func TestSettingsRoundLimitsPanelFieldsRenameAndRevisions(t *testing.T) {
	t.Parallel()
	root, home := t.TempDir(), t.TempDir()
	type view struct {
		Effective   settings.Effective       `json:"effective"`
		Fields      []settings.EditableField `json:"fields"`
		Revisions   map[string]string        `json:"revisions"`
		RoundLimits []struct {
			Limit   int    `json:"limit"`
			Name    string `json:"name"`
			Value   int    `json:"value"`
			Default bool   `json:"default"`
		} `json:"roundLimits"`
	}
	show := func(args ...string) view {
		t.Helper()
		var shown view
		data := settingsCommand(t, root, home, append([]string{"show"}, args...)...)
		if err := json.Unmarshal(data, &shown); err != nil {
			t.Fatal(err)
		}
		return shown
	}
	initial := show()
	if len(initial.RoundLimits) != len(settings.RoundLimitNodes) || !reflect.DeepEqual(initial.Revisions, map[string]string{"user": "", "project": ""}) || initial.Effective.Values.PanelIdleTimeout != 300 || !initial.Effective.Values.PanelSelection {
		t.Fatalf("initial view: %+v", initial)
	}
	for i, limit := range initial.RoundLimits {
		if limit.Limit != i+1 || limit.Name != settings.RoundLimitNodes[i] || limit.Value != 3 || !limit.Default {
			t.Fatalf("default round limit view: %+v", initial.RoundLimits)
		}
	}
	settingsCommand(t, root, home, "save", "--name", "甲", "--revision", "project=")
	settingsCommand(t, root, home, "set", "--configuration", "甲", "--round-limit", "5", "--value", "7")
	settingsCommand(t, root, home, "set", "--configuration", "甲", "--round-limit", "product-review", "--value", "1")
	if err := settingsCommandError(root, home, "set", "--configuration", "甲", "--round-limit", "repair", "--value", "65"); err == nil {
		t.Fatal("round limit above 64 accepted")
	}
	limits := map[string][2]any{}
	for _, limit := range show("--configuration", "甲").RoundLimits {
		limits[limit.Name] = [2]any{limit.Value, limit.Default}
	}
	if limits["repair"] != [2]any{7, false} || limits["product-review"] != [2]any{1, false} || limits["tech-review"] != [2]any{3, true} {
		t.Fatalf("saved round limits view: %v", limits)
	}
	settingsCommand(t, root, home, "remove", "--configuration", "甲", "--round-limit", "repair")
	for _, limit := range show("--configuration", "甲").RoundLimits {
		if limit.Name == "repair" && (limit.Value != 3 || !limit.Default) {
			t.Fatalf("removed round limit did not return to its default: %+v", limit)
		}
	}
	for _, field := range initial.Fields {
		if field.Name == "panelSelection" {
			settingsCommand(t, root, home, "set", "--field", strconv.Itoa(field.Position), "--value", "false")
		}
	}
	current := show()
	if current.Effective.Values.PanelSelection {
		t.Fatal("panelSelection was not set through its field position")
	}
	if err := settingsCommandError(root, home, "rename", "--configuration", "甲", "--name", "乙", "--revision", "project=stale"); err == nil || !strings.Contains(err.Error(), "reload") {
		t.Fatalf("stale revision accepted: %v", err)
	}
	settingsCommand(t, root, home, "rename", "--configuration", "甲", "--name", "乙", "--revision", "project="+current.Revisions["project"], "--revision", "user=")
	_, options, err := settings.LoadConfigurations(root, home)
	if err != nil || len(options) != 3 || options[1].Name != "乙" || options[1].Configuration.RoundLimit("product-review") != 1 {
		t.Fatalf("renamed configuration: %+v %v", options, err)
	}
}

type settingsSelectionPlatform struct {
	descriptor host.Descriptor
	root       string
	target     string
	models     []string
}

func (platform *settingsSelectionPlatform) HostDescriptor() *host.Descriptor {
	return &platform.descriptor
}

func (platform *settingsSelectionPlatform) SelectionOptions(root, target string, native map[string]string, model string) (host.SelectionCatalogue, error) {
	if root != platform.root || target != platform.target || native["codexExecutable"] != "configured-cli" {
		return host.SelectionCatalogue{}, fmt.Errorf("unexpected native options identity: root=%q target=%q executable=%q", root, target, native["codexExecutable"])
	}
	platform.models = append(platform.models, model)
	switch model {
	case "":
		return host.SelectionCatalogue{Models: []host.ModelOption{{Value: "careful", Label: "Careful", Efforts: []string{"medium", "high"}}, {Value: "fast", Label: "Fast", Efforts: []string{"low"}}}, Source: "native fixture"}, nil
	case "careful":
		return host.SelectionCatalogue{Models: []host.ModelOption{{Value: model, Label: "Careful", Efforts: []string{"medium", "high"}}}, Source: "native careful fixture"}, nil
	case "fast":
		return host.SelectionCatalogue{Models: []host.ModelOption{{Value: model, Label: "Fast", Efforts: []string{"low"}}}, Source: "native fast fixture"}, nil
	default:
		return host.SelectionCatalogue{}, fmt.Errorf("unexpected model %q", model)
	}
}

func TestSettingsExecutionChoicesUseNativeOptionsWithoutChangingRun(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	clearHostEnv(t)
	root := t.TempDir()
	startAdmittedEngineRun(t, root)
	admission, err := validate.ResolveEngineAdmission(root, "")
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	platform := &settingsSelectionPlatform{descriptor: host.Descriptor{ID: "settings-native-options"}, root: root, target: admission.Target}
	if err := host.Register(platform); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, ".gates", "config.json")
	mustWriteCLI(t, path, `{"codexExecutable":"configured-cli"}`)
	before := snapshotEngineRunTree(t, root, "writer-exclusive")
	settingsCommand(t, root, home, "save", "--configuration", "默认")
	settingsCommand(t, root, home, "save", "--name", "用户方案")
	settingsCommand(t, root, home, "set", "--configuration", "用户方案", "--qa-mode", "2", "--enabled", "true")
	if len(platform.models) != 0 {
		t.Fatal("QA selection queried native models")
	}
	shown := settingsCommand(t, root, home, "execution-options", "--configuration", "用户方案", "--execution-node", "qa-blackbox", "--platform", platform.descriptor.ID)
	var queried struct {
		Platforms []struct {
			Position int    `json:"number"`
			Value    string `json:"label"`
		} `json:"platforms"`
	}
	if err := json.Unmarshal(shown, &queried); err != nil {
		t.Fatal(err)
	}
	platformPosition := ""
	for _, choice := range queried.Platforms {
		if choice.Value == platform.descriptor.ID {
			platformPosition = strconv.Itoa(choice.Position)
		}
	}
	if platformPosition == "" {
		t.Fatalf("native platform position missing: %s", shown)
	}
	settingsCommand(t, root, home, "set", "--configuration", "用户方案", "--execution-node", "qa-blackbox", "--platform", platformPosition, "--model", "1", "--effort", "2")
	settingsCommand(t, root, home, "set", "--configuration", "用户方案", "--execution-node", "qa-whitebox", "--platform", platform.descriptor.ID, "--model", "Fast", "--effort", "low")
	_, choices, err := settings.LoadConfigurations(root, home)
	selected, selectErr := settings.SelectConfiguration(choices, "用户方案")
	if err != nil || selectErr != nil || selected.Configuration.Execution["qa-blackbox"].Model != "careful" || selected.Configuration.Execution["qa-blackbox"].ReasoningEffort != "high" || selected.Configuration.Execution["qa-whitebox"].Model != "fast" || selected.Configuration.Execution["qa-whitebox"].ReasoningEffort != "low" || !reflect.DeepEqual(platform.models, []string{"", "", "careful", "", "fast"}) {
		t.Fatalf("native model identity or independent QA choices lost: %+v calls=%v %v %v", selected, platform.models, err, selectErr)
	}
	prior, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out, stderr bytes.Buffer
	if code, err := runSettings([]string{"set", "--root", root, "--home", home, "--configuration", "用户方案", "--execution-node", "qa-whitebox", "--effort", "high"}, IO{Stdout: &out, Stderr: &stderr}); code == 0 || err == nil {
		t.Fatal("unsupported native effort accepted")
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(prior, after) || !reflect.DeepEqual(before, snapshotEngineRunTree(t, root, "writer-exclusive")) {
		t.Fatal("settings candidate changed rejected bytes or the running workflow", err)
	}
	settingsCommand(t, root, home, "remove", "--configuration", "用户方案", "--execution-node", "qa-blackbox")
	_, choices, err = settings.LoadConfigurations(root, home)
	selected, selectErr = settings.SelectConfiguration(choices, "用户方案")
	if err != nil || selectErr != nil || selected.Configuration.Execution["qa-blackbox"] != (settings.ExecutionChoice{}) || selected.Configuration.Execution["qa-whitebox"].Model != "fast" {
		t.Fatal("execution removal changed another group", selected, err, selectErr)
	}
}
