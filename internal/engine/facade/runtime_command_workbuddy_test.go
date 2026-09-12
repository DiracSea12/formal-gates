package facade

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestWorkBuddyCommandContract(t *testing.T) {
	schema := json.RawMessage(`{"type":"object"}`)
	fresh, err := workBuddyCommand("/bin/workbuddy", "/work", "chosen-model", "high", "", schema, "/work/mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := workBuddyCommand("/bin/workbuddy", "/work", "chosen-model", "high", "session-1", schema, "/work/mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	if fresh.Dir != "/work" || fresh.Path != "/bin/workbuddy" {
		t.Fatalf("wrong invocation: %+v", fresh)
	}
	want := []string{"/bin/workbuddy", "--print", "--verbose", "--output-format", "stream-json", "--input-format", "text", "--json-schema", string(schema), "--tools", "StructuredOutput", "--strict-mcp-config", "--mcp-config", "/work/mcp.json", "--setting-sources", "", "--settings", `{"disableAllHooks":true}`, "--permission-mode", "bypassPermissions", "--model", "chosen-model", "--effort", "high"}
	if !reflect.DeepEqual(fresh.Args, want) {
		t.Fatalf("args=%q", fresh.Args)
	}
	if !reflect.DeepEqual(resumed.Args, append(want, "--resume", "session-1")) {
		t.Fatalf("resume args=%q", resumed.Args)
	}
	for _, tc := range []struct {
		executable, workspace, model, effort, mcp string
		schema                                    json.RawMessage
	}{
		{"workbuddy", "/work", "m", "", "/work/mcp.json", schema},
		{"/bin/workbuddy", "relative", "m", "", "/work/mcp.json", schema},
		{"/bin/workbuddy", "/work", "m", "ultra", "/work/mcp.json", schema},
		{"/bin/workbuddy", "/work", "m", "", "relative", schema},
		{"/bin/workbuddy", "/work", "m", "", "/work/mcp.json", json.RawMessage(`null`)},
	} {
		if _, err := workBuddyCommand(tc.executable, tc.workspace, tc.model, tc.effort, "", tc.schema, tc.mcp); err == nil {
			t.Fatalf("accepted invalid config %+v", tc)
		}
	}
}

func TestWorkBuddyFreshIdentityIsNotResume(t *testing.T) {
	for _, resume := range []bool{false, true} {
		cmd, err := workBuddySessionCommand("/bin/workbuddy", "/work", "model", "", "session", resume, json.RawMessage(`{"type":"object"}`), "/work/mcp.json")
		if err != nil {
			t.Fatal(err)
		}
		want := "--session-id"
		if resume {
			want = "--resume"
		}
		if !reflect.DeepEqual(cmd.Args[len(cmd.Args)-2:], []string{want, "session"}) {
			t.Fatalf("args=%q", cmd.Args)
		}
	}
}

func TestWorkBuddyRoleTools(t *testing.T) {
	for _, tc := range []struct{ step, want string }{
		{"development-worker", "StructuredOutput,Bash,Edit,Read"}, {"repair-worker", "StructuredOutput,Bash,Edit,Read"},
		{"whitebox-design", "StructuredOutput,Bash,Edit,Read"}, {"product-review", "StructuredOutput"},
		{"start-readiness", "StructuredOutput"}, {"blackbox-execution", "StructuredOutput"},
		{"whitebox-execution", "StructuredOutput"}, {"blackbox-design", "StructuredOutput,Read"},
	} {
		cmd, err := workBuddyCommand("/bin/workbuddy", "/work", "m", "", "", json.RawMessage(`{"type":"object"}`), "/work/mcp.json")
		if err != nil {
			t.Fatal(err)
		}
		workBuddyRoleTools(cmd, tc.step)
		got := "missing"
		for i := 1; i+1 < len(cmd.Args); i++ {
			if cmd.Args[i] == "--tools" {
				got = cmd.Args[i+1]
			}
		}
		if got != tc.want {
			t.Fatalf("%s: tools=%q want=%q", tc.step, got, tc.want)
		}
	}
}

func TestWorkBuddyDefaultModel(t *testing.T) {
	cmd, err := workBuddyCommand("/bin/workbuddy", "/work", "", "", "", json.RawMessage(`{"type":"object"}`), "/work/mcp.json")
	if err != nil {
		t.Fatal(err)
	}
	for _, arg := range cmd.Args {
		if arg == "--model" || arg == "--fallback-model" {
			t.Fatalf("host default replaced: %q", cmd.Args)
		}
	}
}
