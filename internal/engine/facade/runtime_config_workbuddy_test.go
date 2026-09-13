package facade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"formal-gates/internal/settings"
	"testing"
)

func TestWorkBuddyMCPConfigUsesBoundCLI(t *testing.T) {
	config, err := workBuddyMCPConfig("/engine path/formal-gates", "/assigned work", "run", "action:1", "session:1")
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(config)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		Servers map[string]struct {
			Command string   `json:"command"`
			Args    []string `json:"args"`
		} `json:"mcpServers"`
	}
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	server := decoded.Servers["formal-gates"]
	want := []string{"workflow", "drive", "--root", "/assigned work", "--run-id", "run", "--action-id", "action:1", "--operation-mcp", "--native-identity", "session:1"}
	if len(decoded.Servers) != 1 || server.Command != "/engine path/formal-gates" || !reflect.DeepEqual(server.Args, want) {
		t.Fatalf("config=%s", data)
	}
	for _, args := range [][5]string{
		{"relative", "/work", "r", "a", "s"},
		{"/engine", "relative", "r", "a", "s"},
		{"/engine", "/work", "", "a", "s"},
		{"/engine", "/work", "r", "", "s"},
		{"/engine", "/work", "r", "a", ""},
	} {
		if _, err := workBuddyMCPConfig(args[0], args[1], args[2], args[3], args[4]); err == nil {
			t.Fatalf("accepted missing binding %q", args)
		}
	}
}

func TestWorkBuddyCaptureBindsNativeProductFileWithoutCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("WORKBUDDY_CONFIG_DIR", dir)
	t.Setenv("ACC_PRODUCT_CONFIG_PATH", "")
	path := filepath.Join(dir, "cache", "acc-product-config-v3.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	secret := "fixture-only-secret-that-must-not-enter-engine-state"
	if err := os.WriteFile(path, []byte(`{"authentication":{"type":"custom-token","attributes":{"token":"`+secret+`"}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	adapter := workBuddyRuntimeAdapter{}
	binding, err := adapter.capture(settings.Values{Native: map[string]string{"workbuddyExecutable": executable}})
	if err != nil {
		t.Fatal(err)
	}
	if binding.Environment["ACC_PRODUCT_CONFIG_PATH"] != path || binding.Environment["CODEBUDDY_CONFIG_DIR"] != dir || binding.Environment["WORKBUDDY_CONFIG_DIR"] != dir {
		t.Fatal("native desktop context was not captured", binding)
	}
	raw, _ := json.Marshal(binding)
	if strings.Contains(string(raw), secret) {
		t.Fatal("native authentication copied into the engine binding")
	}
	if err := adapter.validate(binding); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := adapter.validate(binding); err == nil || !strings.Contains(err.Error(), "WORKBUDDY_CONFIG") {
		t.Fatal("lost native configuration did not block resume", err)
	}
	if _, err := adapter.capture(settings.Values{Native: map[string]string{"workbuddyExecutable": executable, "workbuddyProductConfig": path}}); err == nil {
		t.Fatal("explicit missing native configuration silently fell back")
	}
}
