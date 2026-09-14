package facade

import (
	"encoding/json"
	"reflect"
	"strings"
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
	for _, step := range []string{"development-worker", "repair", "whitebox-design", "product-review", "start-readiness", "blackbox-execution", "whitebox-execution", "blackbox-design"} {
		cmd, err := workBuddyCommand("/bin/workbuddy", "/work", "", "", "", json.RawMessage(`{"type":"object"}`), "/work/mcp.json")
		if err != nil {
			t.Fatal(err)
		}
		workBuddyRoleTools(cmd, step)
		flags := map[string]string{}
		for i := 1; i+1 < len(cmd.Args); i++ {
			flags[cmd.Args[i]] = cmd.Args[i+1]
		}
		if flags["--tools"] != "default" {
			t.Fatal("native defaults replaced", cmd.Args)
		}
		denied := "," + flags["--disallowedTools"] + ","
		for _, tool := range []string{"Read", "Glob", "Grep", "WebFetch", "WebSearch", "StructuredOutput"} {
			if strings.Contains(denied, ","+tool+",") {
				t.Fatal("ordinary native capability denied", tool)
			}
		}
		if strings.Contains(denied, ",Bash,") != (nativeRoleAccess(step) == nativeOperationOnly) || strings.Contains(denied, ",Edit,") != (nativeRoleAccess(step) != nativeWorkspaceWrite) {
			t.Fatal("role boundary", step, denied)
		}
		if !strings.Contains(denied, ",Task,") {
			t.Fatal("native delegation exposed")
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

func TestWorkBuddyReviewNativePermissionsFreshAndResume(t *testing.T) {
	for _, step := range []string{"product-review", "start-readiness", "blackbox-design", "blackbox-review", "whitebox-review", "complexity-gate", "implementation-quality-gate"} {
		for _, resume := range []bool{false, true} {
			cmd, err := workBuddySessionCommand("/bin/workbuddy", "/work", "", "", "session", resume, json.RawMessage(`{"type":"object"}`), "/work/mcp.json")
			if err != nil {
				t.Fatal(err)
			}
			workBuddyRoleTools(cmd, step)
			flags := map[string]string{}
			for i := 1; i+1 < len(cmd.Args); i++ {
				flags[cmd.Args[i]] = cmd.Args[i+1]
			}
			if flags["--permission-mode"] != "dontAsk" {
				t.Fatalf("%s resume=%v: permission bypass: %q", step, resume, cmd.Args)
			}
			if flags["--tools"] != "default" {
				t.Fatalf("%s: unexpected workspace tools: %q", step, cmd.Args)
			}
			allowed := "StructuredOutput,Read,Glob,Grep"
			if nativeAllowsOperation(step) {
				allowed += ",mcp__formal-gates__formal_gates_operation"
			}
			if flags["--allowedTools"] != allowed {
				t.Fatalf("%s: Bash must use native command checks; reads and result delivery must remain allowed: %q", step, cmd.Args)
			}
			if flags["--strict-mcp-config"] != "--mcp-config" || flags["--setting-sources"] != "" {
				t.Fatalf("%s: ambient configuration enabled: %q", step, cmd.Args)
			}
		}
	}
}

func TestWorkBuddyNonReviewPermissionsUnchanged(t *testing.T) {
	for _, step := range []string{"development-worker", "repair", "whitebox-design", "blackbox-execution", "whitebox-execution"} {
		cmd, err := workBuddyCommand("/bin/workbuddy", "/work", "", "", "", json.RawMessage(`{"type":"object"}`), "/work/mcp.json")
		if err != nil {
			t.Fatal(err)
		}
		workBuddyRoleTools(cmd, step)
		for i, arg := range cmd.Args {
			if arg == "--permission-mode" && cmd.Args[i+1] != "bypassPermissions" {
				t.Fatalf("%s: non-review permission changed", step)
			}
			if arg == "--allowedTools" {
				t.Fatalf("%s: review permission rules leaked", step)
			}
		}
	}
}

func TestWorkBuddyCoverageKeepsReadOnlyShell(t *testing.T) {
	for _, identity := range []string{"", "native-session"} {
		cmd, err := workBuddyCommand("/bin/workbuddy", "/work", "", "", identity, json.RawMessage(`{"type":"object"}`), "/work/mcp.json")
		if err != nil {
			t.Fatal(err)
		}
		workBuddyRoleTools(cmd, "whitebox-design", true)
		flags := map[string]string{}
		for i := 1; i+1 < len(cmd.Args); i++ {
			flags[cmd.Args[i]] = cmd.Args[i+1]
		}
		if flags["--tools"] != "default" || flags["--permission-mode"] != "dontAsk" || strings.Contains(flags["--disallowedTools"], "Bash") || strings.Contains(flags["--allowedTools"], "Bash") {
			t.Fatalf("coverage lost native read-only shell policy: %v", cmd.Args)
		}
		for _, forbidden := range []string{"Write", "Edit", "mcp__formal-gates__formal_gates_operation"} {
			if !strings.Contains(flags["--disallowedTools"], forbidden) {
				t.Fatal("coverage exposes", forbidden)
			}
		}
	}
}
