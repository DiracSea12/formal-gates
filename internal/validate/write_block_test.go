package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteBlockDecisionMatrix covers the write-block decision matrix via
// the real Hook entry: with an active formal run, the main thread and
// reviewer-class agents are blocked from direct code/run-state writes;
// development-worker and qa-design are allowed; the main agent editing a
// registered requirement/design document is allowed; and with no active run the
// same write is allowed.
func TestWriteBlockDecisionMatrix(t *testing.T) {
	root := t.TempDir()
	writeActiveRunState(t, root)
	codePath := filepath.ToSlash(filepath.Join(root, "internal", "code.go"))
	docPath := filepath.ToSlash(filepath.Join(root, "requirements.md"))

	for _, tc := range []struct {
		name      string
		payload   string
		wantDeny  bool
	}{
		{name: "main thread code write blocked", payload: fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q}}`, root, codePath), wantDeny: true},
		{name: "reviewer product-review blocked", payload: fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q},"agent_type":"product-review"}`, root, codePath), wantDeny: true},
		{name: "reviewer qa-review blocked", payload: fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q},"agent_type":"qa-review"}`, root, codePath), wantDeny: true},
		{name: "reviewer start-readiness blocked", payload: fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q},"agent_type":"start-readiness"}`, root, codePath), wantDeny: true},
		{name: "development-worker allowed", payload: fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q},"agent_type":"development-worker"}`, root, codePath), wantDeny: false},
		{name: "qa-design allowed", payload: fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q},"agent_type":"qa-design"}`, root, codePath), wantDeny: false},
		{name: "main thread registered doc allowed", payload: fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q}}`, root, docPath), wantDeny: false},
	} {
		decision, err := Hook([]byte(tc.payload))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if tc.wantDeny && decision.PermissionDecision != "deny" {
			t.Fatalf("%s: expected deny, got %#v", tc.name, decision)
		}
		if !tc.wantDeny && decision.PermissionDecision != "allow" {
			t.Fatalf("%s: expected allow, got %#v", tc.name, decision)
		}
	}
}

// TestWriteBlockNoActiveRunAllows covers scope boundary: without an
// active formal run the same main-thread code write is allowed (the hook must
// not interfere with ordinary projects).
func TestWriteBlockNoActiveRunAllows(t *testing.T) {
	root := t.TempDir()
	payload := fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q}}`, root, filepath.ToSlash(filepath.Join(root, "main.go")))
	decision, err := Hook([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if decision.PermissionDecision != "allow" {
		t.Fatalf("write without an active run was blocked: %#v", decision)
	}
}

// TestWriteBlockGitCommitCoversBashWrites verifies the write detection
// covers Bash git commits by a main-thread agent while an active run exists.
func TestWriteBlockGitCommitCoversBashWrites(t *testing.T) {
	root := t.TempDir()
	writeActiveRunState(t, root)
	payload := fmt.Sprintf(`{"cwd":%q,"tool_name":"Bash","tool_input":{"command":"git commit -m 'delivery'"}}`, root)
	decision, err := Hook([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if decision.PermissionDecision != "deny" {
		t.Fatalf("main-thread git commit was not blocked: %#v", decision)
	}
	// formal-gates CLI 命令放行（run 状态唯一合法写入者）。
	cliPayload := fmt.Sprintf(`{"cwd":%q,"tool_name":"Bash","tool_input":{"command":"formal-gates workflow snapshot --root . --run-id x --dispatch d"}}`, root)
	decision, err = Hook([]byte(cliPayload))
	if err != nil {
		t.Fatal(err)
	}
	if decision.PermissionDecision != "allow" {
		t.Fatalf("formal-gates CLI snapshot command was blocked: %#v", decision)
	}
}

// TestWriteBlockReadOnlyAllowed verifies read-only tools are never adjudicated
// by the write-block guard (they fall through to the ordinary allow path).
func TestWriteBlockReadOnlyAllowed(t *testing.T) {
	root := t.TempDir()
	writeActiveRunState(t, root)
	payload := fmt.Sprintf(`{"cwd":%q,"tool_name":"Read","tool_input":{"file_path":%q}}`, root, filepath.ToSlash(filepath.Join(root, "main.go")))
	decision, err := Hook([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if decision.PermissionDecision != "allow" {
		t.Fatalf("read-only tool was blocked: %#v", decision)
	}
}

// TestCommandWritesFilesReadOnlyIdioms covers the P1 fix at the lowest layer:
// read-only redirect idioms (2>&1 stderr merge、> /dev/null 丢弃输出) are not treated
// as file writes, while real redirects, file-mutation utilities, and VCS writes
// still are. The naive substring matching previously misjudged 2>&1 as a 2> write
// and > /dev/null as a > write.
func TestCommandWritesFilesReadOnlyIdioms(t *testing.T) {
	readOnly := []string{
		"go test ./... 2>&1 | head",
		"go test ./... > /dev/null",
		"go test ./... 2>/dev/null",
		"echo hi > /dev/null 2>&1",
		"cat requirements.md | grep -i req > /dev/null",
	}
	for _, cmd := range readOnly {
		if commandWritesFiles(cmd) {
			t.Fatalf("read-only command misjudged as a write: %q", cmd)
		}
	}
	writing := []string{
		"go test ./... 2>&1 | tee out.log",
		"echo x > main.go",
		"echo x > notes.md",
		"cmd 2> err.log",
		"cat file >> log.txt",
		"git commit -m delivery",
		"touch marker.go",
		"cp a.go b.go",
	}
	for _, cmd := range writing {
		if !commandWritesFiles(cmd) {
			t.Fatalf("file-writing command not detected: %q", cmd)
		}
	}
}

// TestWriteBlockReadOnlyBashAllowed covers read-only Bash allow path with
// an active run (P1 fix): read-only idioms such as stderr merge (2>&1) and output
// discard (> /dev/null) must not be misjudged as writes, so the main thread and
// reviewer-class agents (e.g. qa-execution running tests, per its documented role)
// may run them.
func TestWriteBlockReadOnlyBashAllowed(t *testing.T) {
	root := t.TempDir()
	writeActiveRunState(t, root)
	for _, tc := range []struct {
		name    string
		command string
		agent   string
	}{
		{name: "main thread go test with stderr merge", command: "go test ./... 2>&1 | head"},
		{name: "qa-execution go test with stderr merge", command: "go test ./... 2>&1 | head", agent: "qa-execution"},
		{name: "product-review go test output discarded", command: "go test ./... > /dev/null 2>&1", agent: "product-review"},
	} {
		agentPart := ""
		if tc.agent != "" {
			agentPart = fmt.Sprintf(`,"agent_type":%q`, tc.agent)
		}
		payload := fmt.Sprintf(`{"cwd":%q,"tool_name":"Bash","tool_input":{"command":%q}%s}`, root, tc.command, agentPart)
		decision, err := Hook([]byte(payload))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if decision.PermissionDecision != "allow" {
			t.Fatalf("%s: read-only Bash was blocked: %#v", tc.name, decision)
		}
	}
}

// TestWriteBlockMainThreadNonCodeAllowed covers the P2-2 scope narrowing:
// the main thread may write non-code, non-run-state files (P2-BACKLOG.md etc.),
// while code and run-state writes stay blocked.
func TestWriteBlockMainThreadNonCodeAllowed(t *testing.T) {
	root := t.TempDir()
	writeActiveRunState(t, root)
	// 非代码、非状态文档：Edit 与 Bash 重定向均放行。
	for _, payload := range []string{
		fmt.Sprintf(`{"cwd":%q,"tool_name":"Edit","tool_input":{"file_path":%q}}`, root, filepath.ToSlash(filepath.Join(root, "P2-BACKLOG.md"))),
		fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q}}`, root, filepath.ToSlash(filepath.Join(root, "notes.md"))),
		fmt.Sprintf(`{"cwd":%q,"tool_name":"Bash","tool_input":{"command":%q}}`, root, "echo note >> P2-BACKLOG.md"),
	} {
		decision, err := Hook([]byte(payload))
		if err != nil {
			t.Fatal(err)
		}
		if decision.PermissionDecision != "allow" {
			t.Fatalf("main-thread non-code write was blocked: %#v", decision)
		}
	}
	// 代码与 run 状态：仍阻断。
	for _, tc := range []struct {
		name    string
		payload string
	}{
		{name: "code file edit blocked", payload: fmt.Sprintf(`{"cwd":%q,"tool_name":"Edit","tool_input":{"file_path":%q}}`, root, filepath.ToSlash(filepath.Join(root, "internal", "code.go")))},
		{name: "bash redirect to code file blocked", payload: fmt.Sprintf(`{"cwd":%q,"tool_name":"Bash","tool_input":{"command":%q}}`, root, "echo x > internal/code.go")},
		{name: "run-state file edit blocked", payload: fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q}}`, root, filepath.ToSlash(filepath.Join(root, ".gates", "tmp", "wb-test", "state.json")))},
	} {
		decision, err := Hook([]byte(tc.payload))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if decision.PermissionDecision != "deny" {
			t.Fatalf("%s: expected deny, got %#v", tc.name, decision)
		}
	}
}

// writeActiveRunState writes a minimal active run state carrying a registered
// requirement artifact set (requirements.md, design.md) at root, matching the
// shape the hook probes.
func writeActiveRunState(t *testing.T, root string) {
	t.Helper()
	runDir := filepath.Join(root, ".gates", "tmp", "wb-test")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"status":     "ACTIVE",
		"runId":      "wb-test",
		"actions":    map[string]any{},
		"gates":      map[string]any{},
		"carry":      map[string]any{},
		"dispatches": map[string]any{},
		"skipAuthorizations": map[string]any{},
		"selectedGates":      []string{},
		"requirementArtifacts": []map[string]string{
			{"path": "requirements.md", "revision": "r1"},
			{"path": "design.md", "revision": "r2"},
		},
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, "state.json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}
