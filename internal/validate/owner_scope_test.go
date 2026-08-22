package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestSameOwnerIdentity(t *testing.T) {
	for _, tc := range []struct {
		name                string
		owners              []ownerIdentity
		payloadT, payloadS  string
		wantKnown, wantSame bool
	}{
		{"transcript match", []ownerIdentity{{"t1", "s1"}}, "t1", "s1", true, true},
		{"transcript differ", []ownerIdentity{{"t1", "s1"}}, "t2", "s1", true, false},
		{"session match no transcript", []ownerIdentity{{"", "s1"}}, "", "s1", true, true},
		{"session differ no transcript", []ownerIdentity{{"", "s1"}}, "", "s2", true, false},
		{"owner unknown", []ownerIdentity{{"", ""}}, "t2", "s2", false, false},
		{"session fallback when payload transcript missing", []ownerIdentity{{"t1", "s1"}}, "", "s1", true, true},
		{"multi-owner any match", []ownerIdentity{{"t1", "s1"}, {"t2", "s2"}}, "t2", "s2", true, true},
		{"multi-owner none match", []ownerIdentity{{"t1", "s1"}, {"t2", "s2"}}, "t3", "s3", true, false},
		{"mixed known and unknown owner conservative", []ownerIdentity{{"t1", "s1"}, {"", ""}}, "t9", "s9", false, false},
	} {
		known, same := sameOwnerIdentity(tc.owners, tc.payloadT, tc.payloadS)
		if known != tc.wantKnown || same != tc.wantSame {
			t.Errorf("%s: got known=%v same=%v, want known=%v same=%v", tc.name, known, same, tc.wantKnown, tc.wantSame)
		}
	}
}

func TestHookOwnerIdentity(t *testing.T) {
	var payload map[string]any
	if err := json.Unmarshal([]byte(`{"transcript_path":"/t/x.jsonl","session_id":"s1"}`), &payload); err != nil {
		t.Fatal(err)
	}
	tr, se := hookOwnerIdentity(payload)
	if tr != "/t/x.jsonl" || se != "s1" {
		t.Fatalf("claude identity: got (%q,%q)", tr, se)
	}
	payload = nil
	if err := json.Unmarshal([]byte(`{"conversation_id":"c1"}`), &payload); err != nil {
		t.Fatal(err)
	}
	_, se = hookOwnerIdentity(payload)
	if se != "c1" {
		t.Fatalf("cursor conversation_id fallback: got %q", se)
	}
}

func TestCaptureAndConsumeStartOwner(t *testing.T) {
	root := t.TempDir()
	payload := map[string]any{
		"tool_name": "Bash",
		"tool_input": map[string]any{
			"command": "formal-gates workflow start --root " + filepath.ToSlash(root) + " --split no",
		},
		"transcript_path": "/t/owner.jsonl",
		"session_id":      "owner-session",
	}
	captureStartOwner(payload)
	tr, se := consumeStartOwner(root)
	if tr != "/t/owner.jsonl" || se != "owner-session" {
		t.Fatalf("capture/consume mismatch: got (%q,%q)", tr, se)
	}
	if _, err := os.Stat(ownerSidecarPath(root)); !os.IsNotExist(err) {
		t.Fatalf("sidecar not removed after consume")
	}
}

func TestCaptureStartOwnerSkipsNonStart(t *testing.T) {
	root := t.TempDir()
	// 非 workflow start 命令不应写 sidecar。
	captureStartOwner(map[string]any{
		"tool_name":       "Bash",
		"tool_input":      map[string]any{"command": "echo hi"},
		"transcript_path": "/t/owner.jsonl",
		"session_id":      "owner-session",
	})
	if _, err := os.Stat(ownerSidecarPath(root)); !os.IsNotExist(err) {
		t.Fatalf("sidecar written for non-start command")
	}
}

// writeOwnerRunState 写一个进入开发阶段（dev-worker PREPARED）且带 owner 的活动 run 状态。
func writeOwnerRunState(t *testing.T, root, ownerTranscript, ownerSession string) {
	t.Helper()
	runDir := filepath.Join(root, ".gates", "tmp", "owner-test")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	state := map[string]any{
		"status":          "ACTIVE",
		"runId":           "owner-test",
		"actions":         map[string]any{"development-worker": map[string]any{"status": "PREPARED"}},
		"ownerTranscript": ownerTranscript,
		"ownerSession":    ownerSession,
	}
	data, _ := json.Marshal(state)
	if err := os.WriteFile(filepath.Join(runDir, "state.json"), append(data, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestWriteBlockOwnerScope verifies the write-wall only blocks the conversation that
// started the run: a different conversation is allowed, the owner conversation is
// blocked, and an owner-less (legacy) run stays conservative (blocked).
func TestWriteBlockOwnerScope(t *testing.T) {
	root := t.TempDir()
	codePath := filepath.ToSlash(filepath.Join(root, "internal", "code.go"))
	writeOwnerRunState(t, root, "/t/owner.jsonl", "owner-session")

	for _, tc := range []struct {
		name           string
		transcript     string
		session        string
		wantPermission string
	}{
		{"different conversation allowed", "/t/other.jsonl", "other-session", "allow"},
		{"owner conversation blocked", "/t/owner.jsonl", "owner-session", "deny"},
	} {
		payload := fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q},"transcript_path":%q,"session_id":%q}`, root, codePath, tc.transcript, tc.session)
		decision, err := Hook([]byte(payload))
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if decision.PermissionDecision != tc.wantPermission {
			t.Fatalf("%s: got %q, want %q", tc.name, decision.PermissionDecision, tc.wantPermission)
		}
	}
}

// TestWriteBlockOwnerUnknownConservative verifies an owner-less legacy run still
// blocks the main thread (no regression of existing protection).
func TestWriteBlockOwnerUnknownConservative(t *testing.T) {
	root := t.TempDir()
	codePath := filepath.ToSlash(filepath.Join(root, "internal", "code.go"))
	writeOwnerRunState(t, root, "", "")
	payload := fmt.Sprintf(`{"cwd":%q,"tool_name":"Write","tool_input":{"file_path":%q},"transcript_path":"/t/whatever.jsonl","session_id":"whatever"}`, root, codePath)
	decision, err := Hook([]byte(payload))
	if err != nil {
		t.Fatal(err)
	}
	if decision.PermissionDecision != "deny" {
		t.Fatalf("owner-less run should stay conservative: got %q", decision.PermissionDecision)
	}
}
