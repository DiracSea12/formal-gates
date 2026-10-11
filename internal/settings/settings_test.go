package settings

import (
	"os"
	"path/filepath"
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
	putConfig(t, capPath, `{"engineDispatchMaxTotalTokens":40}`)
	value, err := Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	if *value.Values.TokenCap != 40 || value.Values.Capacity != 3 || value.Values.QACapacity != 2 || value.Values.Model != "chosen" || value.Sources["tokenCap"] != capPath || value.Sources["model"] != user || value.Values.NativeString("workbuddyExecutable") != "cbc" {
		t.Fatalf("effective=%+v", value)
	}
	putConfig(t, project, `{"capacity":5,"tokenCap":60,"codexExecutable":"/native/codex"}`)
	value, err = Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	if value.Values.Capacity != 5 || value.Values.QACapacity != 2 || *value.Values.TokenCap != 60 || value.Sources["tokenCap"] != project || value.Sources["qaCapacity"] != user || value.Values.NativeString("codexExecutable") != "/native/codex" {
		t.Fatalf("effective=%+v", value)
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
