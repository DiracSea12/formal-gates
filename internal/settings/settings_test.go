package settings

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditableOverlayAndErrors(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	install := filepath.Join(home, ".config", "formal-gates")
	project := filepath.Join(root, ".gates")
	os.MkdirAll(install, 0700)
	os.MkdirAll(project, 0700)
	os.WriteFile(filepath.Join(install, "config.json"), []byte(`{"tokenCap":1000,"capacity":2}`), 0600)
	os.WriteFile(filepath.Join(project, "config.json"), []byte(`{"capacity":3,"qaCapacity":2}`), 0600)
	e, err := Load(root, home)
	if err != nil || *e.Values.TokenCap != 1000 || e.Values.Capacity != 3 || e.Values.QACapacity != 2 || e.Sources["capacity"] != filepath.Join(project, "config.json") {
		t.Fatalf("%+v %v", e, err)
	}
	for _, data := range []string{`{"tokenCap":0}`, `{"tokenCap":null}`, `{"tokenCap":"100"}`, `{"tokenCaps":100}`, `{"tokencap":1000}`, `{"TokenCap":1000}`, `{"capacity":0}`, `{"qaCapacity":0}`, `{"qaCapacity":65}`, `{"qaCapacity":null}`, `{"qaCapacity":1.5}`, `{"attemptAllowance":999}`} {
		os.WriteFile(filepath.Join(project, "config.json"), []byte(data), 0600)
		if _, err = Load(root, home); err == nil || !strings.Contains(err.Error(), filepath.Join(project, "config.json")) {
			t.Fatalf("%s accepted or unlocatable: %v", data, err)
		}
	}
}

func TestNativeExecutableSettings(t *testing.T) {
	root, home := t.TempDir(), t.TempDir()
	e, err := Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	if e.Values.NativeString("workbuddyExecutable") != "codebuddy" || e.Values.NativeString("zcodeExecutable") != "node" || e.Values.NativeString("zcodeScript") != "" || e.Values.NativeString("dshExecutable") != "dsh" {
		t.Fatalf("defaults=%+v", e.Values)
	}
	user := filepath.Join(home, ".config", "formal-gates")
	project := filepath.Join(root, ".gates")
	if err := os.MkdirAll(user, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(project, 0700); err != nil {
		t.Fatal(err)
	}
	userFile := filepath.Join(user, "config.json")
	projectFile := filepath.Join(project, "config.json")
	if err := os.WriteFile(userFile, []byte(`{"workbuddyExecutable":"/Applications/WorkBuddy.app/codebuddy","zcodeExecutable":"node","zcodeScript":"/Applications/ZCode.app/zcode.cjs","dshExecutable":"/bin/dsh"}`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(projectFile, []byte(`{"dshExecutable":"/project/dsh"}`), 0600); err != nil {
		t.Fatal(err)
	}
	e, err = Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	if e.Values.NativeString("dshExecutable") != "/project/dsh" || e.Sources["dshExecutable"] != projectFile || e.Sources["zcodeScript"] != userFile {
		t.Fatalf("overlay=%+v", e)
	}
	for _, field := range []string{"workbuddyExecutable", "zcodeExecutable", "zcodeScript", "dshExecutable"} {
		for _, value := range []string{`""`, `"  "`, `null`, `42`} {
			if err := os.WriteFile(projectFile, []byte(`{"`+field+`":`+value+`}`), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := Load(root, home); err == nil || !strings.Contains(err.Error(), projectFile) {
				t.Fatalf("accepted invalid %s=%s: %v", field, value, err)
			}
		}
	}
}
