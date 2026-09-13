package facade

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"

	"formal-gates/internal/settings"
	"reflect"
	"testing"
)

func TestZCodeNativeCommandAndRequests(t *testing.T) {
	cmd, err := zcodeCommand("/runtime/node", "/Applications/Z Code/zcode.cjs", "/work dir")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Dir != "/work dir" || !reflect.DeepEqual(cmd.Args, []string{"/runtime/node", "/Applications/Z Code/zcode.cjs", "--cwd", "/work dir", "app-server"}) {
		t.Fatalf("%+v", cmd)
	}
	for _, identity := range []string{"", "native-session"} {
		r, err := zcodeSessionRequest("stable-request", "/work", identity, "yolo", []string{})
		if err != nil {
			t.Fatal(err)
		}
		raw, _ := json.Marshal(r)
		var envelope map[string]json.RawMessage
		if err := json.Unmarshal(raw, &envelope); err != nil {
			t.Fatal(err)
		}
		if _, ok := envelope["jsonrpc"]; ok {
			t.Fatal("wrong protocol envelope")
		}
		if identity == "" {
			if r.Method != "session/create" || r.Params["sessionId"] != nil {
				t.Fatal("caller invented native identity")
			}
		} else if r.Method != "session/resume" || r.Params["sessionId"] != identity {
			t.Fatal("resume lost identity")
		}
	}
	if _, err := zcodeSessionRequest("r", "/work", "", "yolo", nil); err == nil {
		t.Fatal("implicit tool grant accepted")
	}
	if _, err := zcodeCommand("node", "/script", "/work"); err == nil {
		t.Fatal("unbound executable accepted")
	}
}

func TestZCodeNativeHomeBinding(t *testing.T) {
	root, nativeHome := t.TempDir(), t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(root, "zcode.cjs")
	path := filepath.Join(nativeHome, ".zcode", "cli", "config.json")
	native := []byte(`{"opaqueNativeValue":"not-an-engine-field"}`)
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{script: []byte("// fixture"), path: native} {
		if err := os.WriteFile(name, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, ".gates"), 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]string{"zcodeExecutable": executable, "zcodeScript": script, "zcodeHome": nativeHome})
	if err := os.WriteFile(filepath.Join(root, ".gates", "config.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	config, err := settings.Load(root, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	adapter := zcodeRuntimeAdapter{}
	binding, err := adapter.capture(config.Values)
	if err != nil {
		t.Fatal(err)
	}
	if binding.Environment["HOME"] != nativeHome {
		t.Fatal("native home was not captured")
	}
	if err := adapter.validate(binding); err != nil {
		t.Fatal(err)
	}
	cmd, err := zcodeCommand(binding.Executable, binding.Environment[zcodeScriptBinding], root)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cmd.Args, []string{binding.Executable, script, "--cwd", root, "app-server"}) {
		t.Fatalf("%v", cmd.Args)
	}
	serialized, _ := json.Marshal(binding)
	if bytes.Contains(serialized, []byte("not-an-engine-field")) {
		t.Fatal("native configuration content leaked into engine context")
	}
	actual, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(actual, native) {
		t.Fatal("native configuration was rewritten", err)
	}
	if err := os.Rename(nativeHome, nativeHome+"-unavailable"); err != nil {
		t.Fatal(err)
	}
	defer os.Rename(nativeHome+"-unavailable", nativeHome)
	if err := adapter.validate(binding); err == nil {
		t.Fatal("lost native binding silently fell back")
	}
	if _, err := adapter.capture(config.Values); err == nil {
		t.Fatal("missing explicitly configured native home accepted")
	}
}
