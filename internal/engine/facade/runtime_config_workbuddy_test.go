package facade

import (
	"encoding/json"
	"reflect"
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
