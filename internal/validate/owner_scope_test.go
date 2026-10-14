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
	for _, name := range []string{"default root", "relative root", "absolute root"} {
		t.Run(name, func(t *testing.T) {
			cwd := t.TempDir()
			root := cwd
			command := "formal-gates workflow start --run-id owner --intake 1"
			switch name {
			case "relative root":
				root = filepath.Join(cwd, "project")
				command += " --root project"
			case "absolute root":
				root = t.TempDir()
				command += fmt.Sprintf(" --root=%q", root)
			}
			payload, err := json.Marshal(map[string]any{
				"cwd":             cwd,
				"tool_name":       "Bash",
				"tool_input":      map[string]any{"command": command},
				"transcript_path": "/t/owner.jsonl",
				"session_id":      "owner-session",
			})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := Hook(payload, "codex"); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(ownerSidecarPath(root, "owner"))
			if err != nil {
				t.Fatal(err)
			}
			var owner map[string]string
			if err := json.Unmarshal(data, &owner); err != nil {
				t.Fatal(err)
			}
			if len(owner) != 3 || owner["transcript"] != "/t/owner.jsonl" || owner["session"] != "owner-session" || owner["provider"] != "codex" {
				t.Fatalf("bootstrap owner mismatch: %s", data)
			}
			tr, se, provider := ConsumeStartOwner(root, "owner")
			if tr != "/t/owner.jsonl" || se != "owner-session" || provider != "codex" {
				t.Fatalf("capture/consume mismatch: got (%q,%q,%q)", tr, se, provider)
			}
			if _, err := os.Stat(ownerSidecarPath(root, "owner")); !os.IsNotExist(err) {
				t.Fatalf("sidecar not removed after consume")
			}
			if tr, se, provider := ConsumeStartOwner(root, "owner"); tr != "" || se != "" || provider != "" {
				t.Fatalf("repeated consume retained owner: got (%q,%q,%q)", tr, se, provider)
			}
		})
	}
}

func TestCaptureStartOwnerSkipsUnownedCommands(t *testing.T) {
	for _, name := range []string{"non-start", "subagent", "no identity"} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			payload := map[string]any{
				"cwd":             root,
				"tool_name":       "Bash",
				"tool_input":      map[string]any{"command": "formal-gates workflow start --run-id owner --intake 1"},
				"transcript_path": "/t/owner.jsonl",
				"session_id":      "owner-session",
			}
			switch name {
			case "non-start":
				payload["tool_input"] = map[string]any{"command": "echo hi"}
			case "subagent":
				payload["agent_type"] = "dev-worker"
			case "no identity":
				delete(payload, "transcript_path")
				delete(payload, "session_id")
			}
			captureStartOwner(payload)
			if _, err := os.Stat(ownerSidecarPath(root, "owner")); !os.IsNotExist(err) {
				t.Fatalf("sidecar written for %s", name)
			}
		})
	}
}

func TestPrecheckDoesNotCaptureOrReplaceBootstrapOwner(t *testing.T) {
	root := t.TempDir()
	bootstrap := map[string]any{"cwd": root, "command": "formal-gates workflow start --run-id owner --intake 1", "session_id": "bootstrap-owner"}
	captureStartOwner(bootstrap, "codex")
	before, err := os.ReadFile(ownerSidecarPath(root, "owner"))
	if err != nil {
		t.Fatal(err)
	}
	for _, flag := range []string{"--precheck", "-precheck", "--precheck=true", "-precheck=TRUE", "--precheck=false -precheck"} {
		payload, _ := json.Marshal(map[string]any{"cwd": root, "command": fmt.Sprintf("formal-gates workflow start %s --run-id owner --document 1", flag), "session_id": "preview-owner"})
		if _, err := Hook(payload, "codex"); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(ownerSidecarPath(root, "owner"))
		if err != nil || string(before) != string(after) {
			t.Fatal("read-only precheck replaced the real bootstrap owner payload")
		}
	}
}

func TestStartOwnerPayloadsAreIsolatedByRun(t *testing.T) {
	root := t.TempDir()
	for _, runID := range []string{"alpha", "beta"} {
		payload, err := json.Marshal(map[string]any{
			"cwd": root, "command": "formal-gates workflow start --run-id " + runID + " --intake 1",
			"transcript_path": "/" + runID + ".jsonl", "session_id": runID,
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := Hook(payload, "codex"); err != nil {
			t.Fatal(err)
		}
	}
	for _, runID := range []string{"alpha", "beta"} {
		transcript, session, provider := ConsumeStartOwner(root, runID)
		if transcript != "/"+runID+".jsonl" || session != runID || provider != "codex" {
			t.Fatalf("run %s consumed another owner's payload: %q %q %q", runID, transcript, session, provider)
		}
		if transcript, session, provider := ConsumeStartOwner(root, runID); transcript != "" || session != "" || provider != "" {
			t.Fatalf("run %s retained a consumed owner payload", runID)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, ".gates", "tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("bootstrap payloads remain after consumption: %v %v", entries, err)
	}
}

func TestStartOwnerUsesFinalRootAndRunFlags(t *testing.T) {
	cwd, root := t.TempDir(), t.TempDir()
	command := fmt.Sprintf("formal-gates workflow start --root ignored -root=%q --run-id ignored -run-id=owner --precheck=true -precheck=0 --intake 1", root)
	payload, err := json.Marshal(map[string]any{"cwd": cwd, "command": command, "session_id": "owner-session"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Hook(payload, "codex"); err != nil {
		t.Fatal(err)
	}
	_, session, provider := ConsumeStartOwner(root, "owner")
	if session != "owner-session" || provider != "codex" {
		t.Fatalf("final native flag values lost: %q %q", session, provider)
	}
	if _, err := os.Stat(filepath.Join(cwd, "ignored")); !os.IsNotExist(err) {
		t.Fatalf("a superseded root was written: %v", err)
	}
}

// writeOwnerRunState 写一个进入开发阶段（dev-worker PREPARED）且带 owner 的活动 run 状态。
func writeOwnerRunState(t *testing.T, root, transcript, session string) {
	writeCurrentHookFixture(t, root, "owner-test", true, true, transcript, session)
}

// TestWriteBlockOwnerScope verifies the write-wall only blocks the conversation that
// started the run: a different conversation is allowed, the owner conversation is
// blocked, and an owner-less current run stays conservative (blocked).
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

// TestWriteBlockOwnerUnknownConservative verifies an owner-less current run still
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

func TestStartOwnerCapturesOnlyActualStartArguments(t *testing.T) {
	root := filepath.Join(t.TempDir(), "example repo")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	command := fmt.Sprintf("printf '%%s\\n' formal-gates workflow start --run-id example --root %q", root)
	payload := map[string]any{"cwd": root, "session_id": "owner-session", "transcript_path": "/owner.jsonl", "tool_name": "Bash", "tool_input": map[string]any{"command": command}}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Hook(raw, "codex"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, ".gates")); !os.IsNotExist(err) {
		t.Fatalf("printing usage created .gates material: %v", err)
	}
	command = fmt.Sprintf("formal-gates workflow start --root ignored -root=%q --run-id ignored -run-id=alpha --precheck=true -precheck=0 --intake 1; printf '%%s\\n' --root ignored --run-id example --precheck; env PROVIDER=codex formal-gates workflow start --root %q --run-id beta --precheck=false --intake 1", root, root)
	payload["tool_input"] = map[string]any{"command": command}
	raw, err = json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Hook(raw, "codex"); err != nil {
		t.Fatal(err)
	}
	for _, runID := range []string{"alpha", "beta"} {
		transcript, session, provider := ConsumeStartOwner(root, runID)
		if transcript != "/owner.jsonl" || session != "owner-session" || provider != "codex" {
			t.Fatalf("run %s start facts lost: %q %q %q", runID, transcript, session, provider)
		}
	}
	entries, err := os.ReadDir(filepath.Join(root, ".gates", "tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("unrelated arguments created payloads: %v %v", entries, err)
	}
	if _, err := os.Stat(filepath.Join(root, "ignored")); !os.IsNotExist(err) {
		t.Fatalf("unrelated/superseded root created materials: %v", err)
	}
}
