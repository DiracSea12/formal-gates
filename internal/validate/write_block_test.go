package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// TestWriteBlockDecisionMatrix covers the write-block decision matrix via
// the real Hook entry: after an active formal run enters development, the main thread and
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
		name     string
		payload  string
		wantDeny bool
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

// TestWriteBlockPreDevelopmentAllows verifies the phase boundary: merely creating
// an ACTIVE run must not enable the role write wall. Product Review / Start
// Readiness and their document revisions happen while development-worker is still
// PENDING, so writes by the main thread and reviewer-class agents are not
// adjudicated by this guard until development is prepared.
func TestWriteBlockPreDevelopmentAllows(t *testing.T) {
	root := t.TempDir()
	writePreDevelopmentRunState(t, root)
	codePath := filepath.ToSlash(filepath.Join(root, "internal", "code.go"))
	for _, tc := range []struct {
		name  string
		agent string
	}{
		{name: "main thread"},
		{name: "product review", agent: "product-review"},
		{name: "start readiness", agent: "start-readiness"},
	} {
		payload := fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q},"agent_type":%q}`, root, codePath, tc.agent)
		decision, err := Hook([]byte(payload))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if decision.PermissionDecision != "allow" {
			t.Fatalf("%s write was blocked before development: %#v", tc.name, decision)
		}
	}
}

// TestWriteBlockTerminalRunAllows verifies the other side of the interval:
// once a run is SEALED or ABORTED, a leftover terminal state file must not keep
// the repository write-blocked while terminal cleanup is pending or retrying.
func TestWriteBlockTerminalRunAllows(t *testing.T) {
	for _, status := range []string{"SEALED", "ABORTED"} {
		t.Run(status, func(t *testing.T) {
			root := t.TempDir()
			writeRunStateForWriteBlock(t, root, status, developmentVerified)
			payload := fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q}}`, root, filepath.ToSlash(filepath.Join(root, "internal", "code.go")))
			decision, err := Hook([]byte(payload))
			if err != nil {
				t.Fatal(err)
			}
			if decision.PermissionDecision != "allow" {
				t.Fatalf("terminal %s run kept the repository write-blocked: %#v", status, decision)
			}
		})
	}
}

// TestWriteBlockOutsideRepoAllowed verifies that an active development run is a
// repository-local write wall, not a global lock for another file/window. Both
// file tools and common simple Bash writes to explicit outside paths are allowed;
// an absolute path inside the active root remains blocked.
func TestWriteBlockOutsideRepoAllowed(t *testing.T) {
	root := t.TempDir()
	writeActiveRunState(t, root)
	outside := t.TempDir()
	outsideCode := filepath.ToSlash(filepath.Join(outside, "other.go"))
	relativeOutside, err := filepath.Rel(root, outsideCode)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name    string
		payload string
	}{
		{name: "main thread file tool absolute outside", payload: fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q}}`, root, outsideCode)},
		{name: "reviewer file tool relative outside", payload: fmt.Sprintf(`{"cwd":%q,"tool_name":"Edit","tool_input":{"file_path":%q},"agent_type":"qa-review"}`, root, filepath.ToSlash(relativeOutside))},
		{name: "main thread redirect outside", payload: fmt.Sprintf(`{"cwd":%q,"tool_name":"Bash","tool_input":{"command":%q}}`, root, "echo x > "+outsideCode)},
		{name: "reviewer touch outside", payload: fmt.Sprintf(`{"cwd":%q,"tool_name":"Bash","tool_input":{"command":%q},"agent_type":"product-review"}`, root, "touch "+outsideCode)},
		{name: "main thread copy to outside", payload: fmt.Sprintf(`{"cwd":%q,"tool_name":"Bash","tool_input":{"command":%q}}`, root, "cp internal/code.go "+outsideCode)},
	} {
		decision, err := Hook([]byte(tc.payload))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if decision.PermissionDecision != "allow" {
			t.Fatalf("%s was blocked by an unrelated active root: %#v", tc.name, decision)
		}
	}

	insideCode := filepath.ToSlash(filepath.Join(root, "internal", "code.go"))
	payload := fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q}}`, root, insideCode)
	decision, err := Hook([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if decision.PermissionDecision != "deny" {
		t.Fatalf("absolute path inside active root escaped the write wall: %#v", decision)
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

// TestWriteBlockGatesReadOnlyMentionAllowed covers 改动 3（RQ-011 实现偏差修正）：
// 命令文本提到 .gates 但只是只读查询（grep/ls/cat/find/python3 读、只读 git 查询）不再被
// 拦——审查类代理与主线程的活动 run 下都放行。
func TestWriteBlockGatesReadOnlyMentionAllowed(t *testing.T) {
	root := t.TempDir()
	writeActiveRunState(t, root)
	for _, tc := range []struct {
		name    string
		command string
		agent   string
	}{
		{name: "reviewer grep mentions gates", command: "grep -rn gates .gates/results/", agent: "qa-review"},
		{name: "reviewer cat gates file", command: "cat .gates/cases/blackbox.md", agent: "product-review"},
		{name: "reviewer ls gates dir", command: "ls -la .gates/", agent: "start-readiness"},
		{name: "reviewer git log over gates path", command: "git log --oneline -- .gates/results/", agent: "carry"},
		{name: "main thread python3 read gates", command: "python3 read.py .gates/cases/blackbox.md"},
		{name: "main thread find in gates", command: "find .gates -name '*.md'"},
		{name: "main thread read-only git status", command: "git status --short .gates/"},
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
			t.Fatalf("%s: read-only command mentioning .gates was blocked: %#v", tc.name, decision)
		}
	}
}

// TestWriteBlockGatesRealWritesStillDenied covers 改动 3 的真写仍拦：git add/commit、
// 输出重定向到 .gates、tee 指向 .gates 等真实写入在活动 run 下仍被拦（审查类与主线程
// 都是）。
func TestWriteBlockGatesRealWritesStillDenied(t *testing.T) {
	root := t.TempDir()
	writeActiveRunState(t, root)
	for _, tc := range []struct {
		name    string
		command string
		agent   string
	}{
		{name: "main thread git add gates", command: "git add .gates/results/x.json"},
		{name: "main thread redirect to gates", command: "echo x > .gates/cases/blackbox.md"},
		{name: "main thread append to gates", command: "echo x >> .gates/tmp/state.json"},
		{name: "main thread tee to gates", command: "tee .gates/results/x.md"},
		{name: "main thread git commit", command: "git commit -m 'delivery'"},
		{name: "reviewer redirect to gates", command: "echo x > .gates/results/x.md", agent: "qa-review"},
		{name: "reviewer tee to code file", command: "tee internal/code.go", agent: "product-review"},
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
		if decision.PermissionDecision != "deny" {
			t.Fatalf("%s: real write to .gates/code was allowed: %#v", tc.name, decision)
		}
	}
}

// TestGatesMentionNotTreatedAsWrite covers 改动 3 的最低层：commandWritesFiles 与
// bashWriteTargetsCodeOrState 不再以命令文本含 .gates 子串判写入——只读查询（提 .gates）
// 不算写，真实写目标（git/tee/重定向到 .gates）仍算写。
func TestGatesMentionNotTreatedAsWrite(t *testing.T) {
	readOnly := []string{
		"grep -rn gates .gates/",
		"ls -la .gates/",
		"cat .gates/cases/blackbox.md",
		"git log --oneline -- .gates/results/",
		"find .gates -name '*.md'",
		"python3 read.py .gates/cases/blackbox.md",
	}
	for _, cmd := range readOnly {
		if commandWritesFiles(cmd) {
			t.Fatalf("read-only command mentioning .gates misjudged as a write: %q", cmd)
		}
		if bashWriteTargetsCodeOrState(cmd) {
			t.Fatalf("read-only command mentioning .gates misjudged as a code/state write: %q", cmd)
		}
	}
	writing := []string{
		"git add .gates/results/x.json",
		"git commit -m x",
		"echo x > .gates/cases/blackbox.md",
		"tee .gates/results/x.md",
		"echo x > internal/code.go",
	}
	for _, cmd := range writing {
		if !commandWritesFiles(cmd) {
			t.Fatalf("real write to .gates/code not detected: %q", cmd)
		}
		if !bashWriteTargetsCodeOrState(cmd) {
			t.Fatalf("real write to .gates/code not detected by main-thread judge: %q", cmd)
		}
	}
}

// writeActiveRunState writes a minimal active run state carrying a registered
// requirement artifact set (requirements.md, design.md) at root, matching the
// shape the hook probes.
func writeActiveRunState(t *testing.T, root string) {
	writeRunStateForWriteBlock(t, root, "ACTIVE", developmentPrepared)
}

func writePreDevelopmentRunState(t *testing.T, root string) {
	writeRunStateForWriteBlock(t, root, "ACTIVE", developmentPending)
}

func writeRunStateForWriteBlock(t *testing.T, root, runStatus, developmentStatus string) {
	t.Helper()
	runDir := filepath.Join(root, ".gates", "tmp", "wb-test")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"status": runStatus,
		"runId":  "wb-test",
		"actions": map[string]any{
			"development-worker": map[string]any{"status": developmentStatus},
		},
		"gates":              map[string]any{},
		"carry":              map[string]any{},
		"dispatches":         map[string]any{},
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
