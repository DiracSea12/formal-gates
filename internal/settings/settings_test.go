package settings

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/host"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func putConfig(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
}
func TestConfigurationOverlaysPreserveProjectCapAndSources(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	user := filepath.Join(home, ".config", "formal-gates", "config.json")
	project := filepath.Join(root, ".gates", "config.json")
	capPath := filepath.Join(root, ".gates", "token-cap.json")
	putConfig(t, user, `{"capacity":3,"qaCapacity":2,"model":"chosen","reasoningEffort":"high","tokenCap":900,"workbuddyExecutable":"cbc"}`)
	value, err := Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	if value.Values.TokenCap == nil || *value.Values.TokenCap != 900 || value.Sources["tokenCap"] != user {
		t.Fatalf("user cap=%+v", value)
	}
	putConfig(t, capPath, `{"engineDispatchMaxTotalTokens":40}`)
	value, err = Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	if value.Values.TokenCap == nil || *value.Values.TokenCap != 40 || value.Values.Capacity != 3 || value.Values.QACapacity != 2 || value.Values.Model != "chosen" || value.Values.ReasoningEffort != "high" || value.Values.NativeString("workbuddyExecutable") != "cbc" {
		t.Fatalf("effective=%+v", value)
	}
	for _, field := range []string{"capacity", "qaCapacity", "model", "reasoningEffort", "workbuddyExecutable"} {
		if value.Sources[field] != user {
			t.Fatalf("%s source=%q, want %q", field, value.Sources[field], user)
		}
	}
	if value.Sources["tokenCap"] != capPath {
		t.Fatalf("cap source=%q, want %q", value.Sources["tokenCap"], capPath)
	}
	putConfig(t, project, `{"capacity":5,"tokenCap":60,"codexExecutable":"/native/codex"}`)
	value, err = Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	if value.Values.Capacity != 5 || value.Values.QACapacity != 2 || value.Values.TokenCap == nil || *value.Values.TokenCap != 60 || value.Values.Model != "chosen" || value.Values.ReasoningEffort != "high" || value.Values.NativeString("workbuddyExecutable") != "cbc" || value.Values.NativeString("codexExecutable") != "/native/codex" {
		t.Fatalf("effective=%+v", value)
	}
	for _, field := range []string{"capacity", "tokenCap", "codexExecutable"} {
		if value.Sources[field] != project {
			t.Fatalf("%s source=%q, want %q", field, value.Sources[field], project)
		}
	}
	for _, field := range []string{"qaCapacity", "model", "reasoningEffort", "workbuddyExecutable"} {
		if value.Sources[field] != user {
			t.Fatalf("%s source=%q, want %q", field, value.Sources[field], user)
		}
	}
	putConfig(t, project, `{"capacity":5}`)
	value, err = Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	if value.Values.TokenCap == nil || *value.Values.TokenCap != 40 || value.Sources["tokenCap"] != capPath || value.Values.Capacity != 5 || value.Sources["capacity"] != project {
		t.Fatalf("removed project cap did not inherit=%+v", value)
	}
	if err := os.Remove(capPath); err != nil {
		t.Fatal(err)
	}
	value, err = Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	if value.Values.TokenCap == nil || *value.Values.TokenCap != 900 || value.Sources["tokenCap"] != user || value.Values.Capacity != 5 || value.Sources["capacity"] != project {
		t.Fatalf("removed cap input did not inherit=%+v", value)
	}
}

func TestProjectCapRejectsInvalidInput(t *testing.T) {
	for _, test := range []struct{ name, body, problem string }{
		{"unknown field", `{"engineDispatchMaxTotalTokens":40,"qaCapacity":1}`, `unknown field "qaCapacity"`},
		{"null field", `{"engineDispatchMaxTotalTokens":null}`, "engineDispatchMaxTotalTokens must be non-negative"},
		{"null object", `null`, "engineDispatchMaxTotalTokens must be non-negative"},
		{"missing cap", `{}`, "engineDispatchMaxTotalTokens must be non-negative"},
		{"negative cap", `{"engineDispatchMaxTotalTokens":-1}`, "engineDispatchMaxTotalTokens must be non-negative"},
		{"trailing object", `{"engineDispatchMaxTotalTokens":40} {}`, "trailing JSON data"},
		{"trailing invalid data", `{"engineDispatchMaxTotalTokens":40} text`, "trailing JSON data"},
	} {
		t.Run(test.name, func(t *testing.T) {
			home, root := t.TempDir(), t.TempDir()
			path := filepath.Join(root, ".gates", "token-cap.json")
			putConfig(t, path, test.body)
			putConfig(t, filepath.Join(root, ".gates", "config.json"), `{"tokenCap":60}`)
			if _, err := Load(root, home); err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), test.problem) {
				t.Fatalf("invalid cap input did not identify its file: %v", err)
			}
		})
	}
}
func TestConfigurationRejectsInvalidLowerPriorityCap(t *testing.T) {
	home, root := t.TempDir(), t.TempDir()
	putConfig(t, filepath.Join(home, ".config", "formal-gates", "config.json"), `{"tokenCap":-1}`)
	putConfig(t, filepath.Join(root, ".gates", "token-cap.json"), `{"engineDispatchMaxTotalTokens":40}`)
	if _, err := Load(root, home); err == nil {
		t.Fatal("negative user cap was ignored")
	}
}

type pathResolverFixture struct {
	*host.Descriptor
	calls int
}

func (p *pathResolverFixture) ResolveNativePaths(native, sources map[string]string) error {
	p.calls++
	native["fixtureRoot"] = native["fixtureHome"] + "/native-config"
	sources["fixtureRoot"] = "fixtureHome"
	if native["fixtureHome"] == "unavailable" {
		return fmt.Errorf("fixture home unavailable")
	}
	return nil
}

func TestSelectedNativePathFreezeUsesRegisteredOwner(t *testing.T) {
	provider := &pathResolverFixture{Descriptor: &host.Descriptor{ID: "settings-path-owner", Installable: true, Aliases: []string{"settings-path-alias"}}}
	if err := host.Register(provider); err != nil {
		t.Fatal(err)
	}
	cap := int64(21)
	effective := Effective{Values: Values{Native: map[string]string{"fixtureHome": "/selected", "otherExecutable": "/unavailable"}, QACapacity: 2, Capacity: 3, TokenCap: &cap, Model: "chosen", ReasoningEffort: "high"}, Sources: map[string]string{"fixtureHome": "project.json", "model": "user.json"}, Files: []string{"user.json", "project.json"}}
	frozen, err := Freeze("settings-path-alias", effective)
	if err != nil || provider.calls != 1 || frozen.Values.Native["fixtureRoot"] != "/selected/native-config" || frozen.Sources["fixtureRoot"] != "fixtureHome" || frozen.Sources["fixtureHome"] != "project.json" || effective.Values.Native["fixtureRoot"] != "" || effective.Sources["fixtureRoot"] != "" {
		t.Fatalf("selected mapper facts: %+v calls=%d err=%v", frozen, provider.calls, err)
	}
	frozen.Values.Native = effective.Values.Native
	frozen.Sources = effective.Sources
	if !reflect.DeepEqual(frozen, effective) {
		t.Fatalf("common settings/files changed: %+v", frozen)
	}
	if _, err := Freeze("default", effective); err != nil || provider.calls != 1 {
		t.Fatalf("engine-only provider invoked mapper: %v calls=%d", err, provider.calls)
	}
	if _, err := Freeze("claude", effective); err == nil || !strings.Contains(err.Error(), "native path resolver unavailable") {
		t.Fatalf("missing mapper was silently accepted: %v", err)
	}
	effective.Values.Native["fixtureHome"] = "unavailable"
	failed, err := Freeze(provider.ID, effective)
	if err == nil || effective.Values.Native["fixtureRoot"] != "" || failed.Values.Native["fixtureRoot"] != "" {
		t.Fatalf("failed mapper published partial paths: %+v %v", failed, err)
	}
}

func TestNativePayloadRoundTripKeepsResolvedStringsAndEditableBoundary(t *testing.T) {
	cap := int64(19)
	original := Effective{Values: Values{Native: map[string]string{"workbuddyExecutable": "/native/cli", "workbuddyHome": "/home", "workbuddyConfigRoot": "/independent/root", "workbuddyProductConfig": "/independent/product.json"}, QACapacity: 3, Capacity: 7, TokenCap: &cap, Model: "model", ReasoningEffort: "high"}, Sources: map[string]string{"workbuddyHome": "HOME", "workbuddyConfigRoot": "WORKBUDDY_CONFIG_DIR", "workbuddyProductConfig": "project.json"}, Files: []string{"user.json", "project.json"}}
	body, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var restored Effective
	if err := json.Unmarshal(body, &restored); err != nil || !reflect.DeepEqual(original, restored) {
		t.Fatalf("resolved payload/common/source roundtrip: %+v err=%v", restored, err)
	}
	var fields map[string]any
	if err := json.Unmarshal(body, &fields); err != nil || fields["values"].(map[string]any)["workbuddyConfigRoot"] != "/independent/root" {
		t.Fatalf("flat wire omitted independent root: %s err=%v", body, err)
	}
	home, root := t.TempDir(), t.TempDir()
	putConfig(t, filepath.Join(root, ".gates", "config.json"), `{"workbuddyConfigRoot":"/editable"}`)
	if _, err := Load(root, home); err == nil || !strings.Contains(err.Error(), `unknown configuration field "workbuddyConfigRoot"`) {
		t.Fatalf("internal payload enlarged editable schema: %v", err)
	}
}
