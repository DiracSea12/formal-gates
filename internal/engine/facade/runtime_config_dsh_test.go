package facade

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDSHNativeOperationProfile(t *testing.T) {
	patch, err := dshOperationPatch("/resources/plugin.mjs", "/engine", "/work", "run", "action", "/resources/requests", "blackbox-execution", "Bound operations", []string{})
	if err != nil {
		t.Fatal(err)
	}
	acp := patch[2].(map[string]any)
	if !reflect.DeepEqual(acp["inject"], []string{"acpAppStartup", "formalGatesNativeReady"}) || acp["config"] != nil {
		t.Fatal("profile replaced native model configuration or lost startup ordering")
	}
	if !reflect.DeepEqual(patch[3], map[string]any{"id": "formal-gates-dsh", "disabled": true}) {
		t.Fatal("native child must disable only the owner's duplicate lifecycle hook")
	}
	plugin := patch[0].(map[string]any)["insert"].([]any)[0].(map[string]any)
	config := plugin["config"].(map[string]any)
	schema := config["tool"].(map[string]any)["inputSchema"].(map[string]any)
	props := schema["properties"].(map[string]any)
	if props["requestId"] == nil {
		t.Fatal("stable native request identity missing")
	}
	if _, err := json.Marshal(patch); err != nil {
		t.Fatal(err)
	}
	cmd, err := dshCommand("/bin/dsh", "/work space", "/resources/profile.json")
	if err != nil || cmd.Dir != "/work space" || !reflect.DeepEqual(cmd.Args, []string{"/bin/dsh", "--profile", "acp", "--patch", "/resources/profile.json"}) {
		t.Fatalf("%+v %v", cmd, err)
	}
	if _, err := dshOperationPatch("relative", "/engine", "/work", "run", "action", "/resources/requests", "blackbox-execution", "", []string{}); err == nil {
		t.Fatal("unbound plugin path accepted")
	}
}
