package facade

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestZCodeNativeCommandAndRequests(t *testing.T) {
	cmd, err := zcodeCommand("/runtime/node", "/Applications/Z Code/zcode.cjs", "/work dir")
	if err != nil {
		t.Fatal(err)
	}
	if cmd.Dir != "/work dir" || !reflect.DeepEqual(cmd.Args, []string{"/runtime/node", "/Applications/Z Code/zcode.cjs", "app-server", "--cwd", "/work dir"}) {
		t.Fatalf("%+v", cmd)
	}
	for _, identity := range []string{"", "native-session"} {
		r, err := zcodeSessionRequest("stable-request", "/work", identity, []string{})
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
	if _, err := zcodeSessionRequest("r", "/work", "", nil); err == nil {
		t.Fatal("implicit tool grant accepted")
	}
	if _, err := zcodeCommand("node", "/script", "/work"); err == nil {
		t.Fatal("unbound executable accepted")
	}
}
