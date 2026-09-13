package facade

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"formal-gates/internal/settings"
)

func TestWorkBuddyNativeHomeDoesNotMoveEngineInstallation(t *testing.T) {
	engineHome, nativeHome, project := t.TempDir(), t.TempDir(), t.TempDir()
	t.Setenv("HOME", engineHome)
	t.Setenv("USERPROFILE", engineHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(engineHome, "config"))
	t.Setenv("WORKBUDDY_CONFIG_DIR", "")
	t.Setenv("ACC_PRODUCT_CONFIG_PATH", "")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	product := filepath.Join(nativeHome, ".workbuddy", "cache", "acc-product-config-v3.json")
	for _, path := range []string{filepath.Dir(product), filepath.Join(project, ".gates")} {
		if err := os.MkdirAll(path, 0700); err != nil {
			t.Fatal(err)
		}
	}
	marker := "fixture-authentication-must-stay-native"
	if err := os.WriteFile(product, []byte(`{"fixture":"`+marker+`"}`), 0600); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]string{"workbuddyExecutable": executable, "workbuddyHome": nativeHome})
	if err := os.WriteFile(filepath.Join(project, ".gates", "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	config, err := settings.Load(project, engineHome)
	if err != nil {
		t.Fatal(err)
	}
	adapter := workBuddyRuntimeAdapter{}
	binding, err := adapter.capture(config.Values)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Environment["HOME"] != nativeHome || binding.Environment["ACC_PRODUCT_CONFIG_PATH"] != product || binding.Environment["WORKBUDDY_CONFIG_DIR"] != filepath.Join(nativeHome, ".workbuddy") {
		t.Fatal("native home and product file were not selected together", binding)
	}
	if runtime.GOOS == "windows" && binding.Environment["USERPROFILE"] != nativeHome {
		t.Fatal("Windows native homedir lost its profile binding")
	}
	mcp, err := workBuddyMCPConfig(executable, project, "fixture-run", "fixture-action", "fixture-native-id")
	if err != nil {
		t.Fatal(err)
	}
	server := mcp["mcpServers"].(map[string]any)["formal-gates"].(map[string]any)
	engineEnv := server["env"].(map[string]string)
	if !reflect.DeepEqual(engineEnv, map[string]string{"HOME": engineHome, "USERPROFILE": engineHome, "XDG_CONFIG_HOME": filepath.Join(engineHome, "config")}) {
		t.Fatal("native MCP tool would resolve a different engine installation", engineEnv)
	}
	serialized, _ := json.Marshal(binding)
	if strings.Contains(string(serialized), marker) || os.Getenv("HOME") != engineHome {
		t.Fatal("capture copied native contents or changed the owner environment")
	}
	if _, err := os.Stat(filepath.Join(engineHome, ".workbuddy")); !os.IsNotExist(err) {
		t.Fatal("capture copied native configuration into the isolated engine home", err)
	}
	// The saved native binding, not the next caller's HOME, owns resume.
	t.Setenv("HOME", t.TempDir())
	cmd := exec.Command(executable)
	if err := runtimeCommandContext(cmd, binding); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, value := range cmd.Env {
		if value == "HOME="+nativeHome {
			found = true
		}
	}
	if !found || engineEnv["HOME"] != engineHome {
		t.Fatal("resume drifted native home or altered the already-built engine tool config")
	}
	// Losing a bound directory is a normal environment error, not a reason to
	// fall back to the next user's home or invent another native identity.
	if err := os.Rename(nativeHome, nativeHome+"-unavailable"); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(nativeHome+"-unavailable", nativeHome)
	if err := adapter.validate(binding); err == nil || !strings.Contains(err.Error(), "native home directory unavailable") {
		t.Fatal("resume silently fell back after losing its native home", err)
	}
	if _, err := adapter.capture(config.Values); err == nil {
		t.Fatal("missing explicitly selected native home was accepted")
	}
}
