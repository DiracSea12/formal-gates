package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
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

func TestSettingsNamedConfigurationCommandsUsePublishedChoices(t *testing.T) {
	t.Parallel()
	root, home := t.TempDir(), t.TempDir()
	shown := settingsCommand(t, root, home, "show")
	var view struct {
		Configurations []settings.ConfigurationOption `json:"configurations"`
		FlowNodes      []struct {
			Node int `json:"node"`
		} `json:"flowNodes"`
		ExecutionNodes []struct {
			Node int `json:"node"`
		} `json:"executionNodes"`
	}
	if err := json.Unmarshal(shown, &view); err != nil || len(view.Configurations) != 1 || view.Configurations[0].Name != "默认" || len(view.FlowNodes) != len(definition.MainFlowNodes()) || len(view.ExecutionNodes) != len(definition.ExecutionNodes()) {
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
	if err != nil || effective.ConfigurationName != "中文 名称" || !reflect.DeepEqual(effective.Configuration.FlowSelection.Disabled, []string{definition.MainFlowNodes()[0].ID}) || !reflect.DeepEqual(effective.Configuration.QAModes, []string{"blackbox", "whitebox"}) || effective.Configuration.Gates == nil || len(*effective.Configuration.Gates) != 0 {
		t.Fatalf("configuration choices: %+v %v", effective, err)
	}
	settingsCommand(t, root, home, "delete", "--configuration", "中文 名称")
	effective, err = settings.Load(root, home)
	if err != nil || effective.ConfigurationName != "默认" {
		t.Fatalf("delete did not select default: %+v %v", effective, err)
	}
	settingsCommand(t, root, home, "set", "--configuration", "默认", "--flow-node", "1", "--enabled", "false")
	settingsCommand(t, root, home, "delete", "--configuration", "默认")
	effective, err = settings.Load(root, home)
	if err != nil || effective.Configuration.FlowSelection != nil {
		t.Fatalf("default override did not return to inherited defaults: %+v %v", effective, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gates", "engine")); !os.IsNotExist(err) {
		t.Fatal("default configuration commands created workflow state", err)
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
