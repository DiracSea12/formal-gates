package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCodexHookCanaryFailureReason(t *testing.T) {
	timedOut := CodexHookCanarySummary{TimedOut: true, TimeoutSeconds: 7}
	if got := codexHookCanaryFailureReason(timedOut); !strings.Contains(got, "7 seconds") {
		t.Fatalf("expected timeout reason, got %q", got)
	}

	noPayload := CodexHookCanarySummary{}
	if got := codexHookCanaryFailureReason(noPayload); !strings.Contains(got, "no PreToolUse") {
		t.Fatalf("expected payload reason, got %q", got)
	}

	marker := CodexHookCanarySummary{PreToolUsePayloadCount: 1, MarkerExists: true}
	if got := codexHookCanaryFailureReason(marker); !strings.Contains(got, "marker file") {
		t.Fatalf("expected marker reason, got %q", got)
	}

	noDeny := CodexHookCanarySummary{PreToolUsePayloadCount: 1}
	if got := codexHookCanaryFailureReason(noDeny); !strings.Contains(got, "canary did not satisfy") {
		t.Fatalf("expected proof reason, got %q", got)
	}
}

func TestCodexCanaryProfileUsesNativeHookAndPassiveRecorder(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "formal-gates.config.toml")
	binary := filepath.Join(dir, "formal-gates")
	payloadDir := filepath.Join(dir, "payloads")
	if err := writeCodexCanaryProfile(profile, binary, payloadDir); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	preStart := strings.Index(content, "[[hooks.PreToolUse]]")
	postStart := strings.Index(content, "[[hooks.PostToolUse]]")
	if preStart < 0 || postStart <= preStart {
		t.Fatalf("PreToolUse profile section missing: %q", content)
	}
	preToolUse := content[preStart:postStart]
	if strings.Count(preToolUse, "hook decide --provider codex") != 1 {
		t.Fatalf("expected exactly one native Codex hook command in PreToolUse, profile=%q", content)
	}
	if !strings.Contains(preToolUse, "codex-hook-probe") {
		t.Fatalf("PreToolUse recorder missing: %q", preToolUse)
	}
	if strings.Contains(preToolUse, "--formal-hook-output") {
		t.Fatalf("PreToolUse recorder must not synthesize hook output: %q", preToolUse)
	}
}

// CASE-012 回归：Codex 的 matcher 是正则表达式，glob "*" 是不匹配任何工具的
// 非法模式。canary profile 里任何 matcher 都必须是 ".*"，
// 否则对应事件的载荷永远捕获不到（QA 观察到的"有 hook payload、无 PreToolUse
// payload"形态之一）。
func TestCodexCanaryProfileMatchersAreRegexes(t *testing.T) {
	dir := t.TempDir()
	profile := filepath.Join(dir, "formal-gates.config.toml")
	if err := writeCodexCanaryProfile(profile, filepath.Join(dir, "formal-gates"), filepath.Join(dir, "payloads")); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "matcher") {
			continue
		}
		if !strings.Contains(trimmed, `".*"`) {
			t.Fatalf("canary profile matcher is not the Codex regex \".*\": %q", trimmed)
		}
	}
}

func TestCodexHookCanaryUsesUniqueCaseIDs(t *testing.T) {
	worktree := t.TempDir()
	missingCodex := filepath.Join(worktree, "missing-codex")
	options := CodexHookCanaryOptions{
		Worktree:     worktree,
		CodexCommand: missingCodex,
		KeepTemp:     true,
	}

	// 缺失 codex 可执行文件：两次 canary 都必须失败（错误被记录），而不是被丢弃。
	first, firstResult := CodexHookCanary(options)
	if firstResult.OK() {
		t.Fatalf("canary with a missing codex command must fail: %#v", firstResult.Failures)
	}
	second, secondResult := CodexHookCanary(options)
	if secondResult.OK() {
		t.Fatalf("canary with a missing codex command must fail: %#v", secondResult.Failures)
	}
	if first.Case == "" || second.Case == "" || first.Case == second.Case {
		t.Fatalf("canary case IDs must be unique: first=%q second=%q", first.Case, second.Case)
	}
	if first.ArtifactDir == second.ArtifactDir || first.Summary == second.Summary {
		t.Fatalf("canary outputs must be isolated: first=%#v second=%#v", first, second)
	}
}

func TestCodexHookPromptRequestsARealToolCall(t *testing.T) {
	prompt := codexHookPrompt("formal-gates", "/tmp/case", "/tmp/case/marker.txt")
	// 提示词必须强制真实 shell 工具调用并禁止纯文本回复：纯文本回复不产生
	// PreToolUse 载荷，正是 QA CASE-012 观察到的失败形态。
	if !strings.Contains(prompt, "must call the shell tool now") || !strings.Contains(prompt, "do not answer in text") {
		t.Fatalf("Codex canary prompt does not require a real shell tool call: %q", prompt)
	}
}
