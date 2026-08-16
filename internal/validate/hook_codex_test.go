package validate

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestHookResponseCodexFormat(t *testing.T) {
	// Codex 的 allow 是 no-op：输出空（nil），不能输出 permissionDecision:"allow"。
	if resp := HookResponse("codex", allowHook("ok")); resp != nil {
		t.Fatalf("codex allow should be nil (no output), got %#v", resp)
	}

	deny, _ := json.Marshal(HookResponse("codex", denyWrite("blocked")))
	if !strings.Contains(string(deny), `"permissionDecision":"deny"`) ||
		!strings.Contains(string(deny), `"permissionDecisionReason":"blocked"`) {
		t.Fatalf("codex deny should carry permissionDecision:deny + reason, got %s", deny)
	}
}

func TestIsWriteToolCodexExec(t *testing.T) {
	// Codex 跑 shell 命令用 tool_name:"exec"，写墙必须认得它。
	if !isWriteTool("exec", "git add -A") {
		t.Fatalf("exec + git add should be a write tool")
	}
	if isWriteTool("exec", "ls -la") {
		t.Fatalf("exec + read-only ls should not be a write")
	}
	if !isWriteTool("apply_patch", "") {
		t.Fatalf("apply_patch should be a write tool")
	}
	// 兜底：未知工具名只要带 command，也按 shell 命令判定。
	if !isWriteTool("unknown-tool", "git add -A") {
		t.Fatalf("unknown tool with git add should be a write tool (fallback)")
	}
	if isWriteTool("unknown-tool", "ls -la") {
		t.Fatalf("unknown tool with read-only ls should not be a write (fallback)")
	}
}

func TestFileChangeTargetsCodeOrRunStatePrefixed(t *testing.T) {
	// 带前缀/全路径的文件变更命令也要被识别，不能只认裸首 token。
	if !fileChangeTargetsCodeOrRunState("sudo cp internal/code.go") {
		t.Fatalf("sudo cp code.go should be detected")
	}
	if !fileChangeTargetsCodeOrRunState("/usr/bin/cp internal/code.go") {
		t.Fatalf("/usr/bin/cp code.go should be detected")
	}
	if fileChangeTargetsCodeOrRunState("npm install -g pkg") {
		t.Fatalf("npm install should not be detected as file change")
	}
}

func TestQuoteCommandArg(t *testing.T) {
	if got := quoteCommandArg("C:/no/space.exe"); got != "C:/no/space.exe" {
		t.Fatalf("no-space path should be unquoted, got %q", got)
	}
	if got := quoteCommandArg("C:/with space/formal-gates.exe"); got != `"C:/with space/formal-gates.exe"` {
		t.Fatalf("space path should be quoted, got %q", got)
	}
}
