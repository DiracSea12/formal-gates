package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCodexHookCanaryUsesUniqueCaseIDs(t *testing.T) {
	worktree := t.TempDir()
	missingCodex := filepath.Join(worktree, "missing-codex")
	options := CodexHookCanaryOptions{
		Worktree:     worktree,
		CodexCommand: missingCodex,
		KeepTemp:     true,
	}

	// 未证明原生拦截的两次检查必须分别保留失败收据。
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

func TestCodexHookCanaryDoesNotQualifyUnrelatedPayloadArtifacts(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".gates", "tmp", "codex-hook-canary", "unrelated", "payloads")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"hook_event_name":"PreToolUse","tool_name":"exec","tool_input":{"command":"pwd"}}`)
	if err := os.WriteFile(filepath.Join(dir, "hook-PreToolUse-exec.json"), payload, 0o600); err != nil {
		t.Fatal(err)
	}
	summary, result := CodexHookCanary(CodexHookCanaryOptions{Worktree: root, CodexCommand: filepath.Join(root, "missing-codex")})
	if result.OK() || summary.Status != "FAIL" || summary.Attempts != 0 {
		t.Fatalf("unrelated hook event cannot qualify interception: %+v %+v", summary, result)
	}
	data, err := os.ReadFile(summary.Summary)
	if err != nil {
		t.Fatal(err)
	}
	var saved CodexHookCanarySummary
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Status != "FAIL" || saved.FailureReason == "" {
		t.Fatalf("receipt must report the unproven capability: %+v", saved)
	}
}
