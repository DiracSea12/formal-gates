package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestLifecycleCaptureAndVerification(t *testing.T) {
	root := t.TempDir()
	useProvider(t, ProviderClaude)
	if err := BeginRun(root, "run-1"); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"payload":{"agent_id":"agent-1"}}`)
	if _, err := Capture(root, ProviderClaude, "SubagentStop", payload); err != nil {
		t.Fatal(err)
	}
	start, err := Capture(root, ProviderClaude, "SubagentStart", payload)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := Capture(root, ProviderClaude, "SubagentStart", payload)
	if err != nil {
		t.Fatal(err)
	}
	if start.Event != eventStart || !duplicate.Duplicate {
		t.Fatalf("unexpected capture results: start=%+v duplicate=%+v", start, duplicate)
	}
	if err := BindDispatch(root, "run-1", "dispatch-1", "agent-1"); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyDispatch(root, "run-1", "dispatch-1")
	if err != nil {
		t.Fatal(err)
	}
	if verification.Outcome != Verified || !verification.StartObserved || !verification.StopObserved {
		t.Fatalf("expected VERIFIED, got %+v", verification)
	}
	assertRunOwnedEvents(t, root, "run-1", "dispatch-1")
}

func TestCursorCorrelatesNormalStopWithoutIdentityAndDerivesDistinctRoots(t *testing.T) {
	root, otherRoot := t.TempDir(), t.TempDir()
	useProvider(t, ProviderCursor)
	if err := BeginRun(root, "run-1"); err != nil {
		t.Fatal(err)
	}
	if err := BeginRun(otherRoot, "run-2"); err != nil {
		t.Fatal(err)
	}
	stop := cursorPayload(root, false)
	start := cursorPayload(root, true)
	if result, err := Capture("", ProviderCursor, "subagentStop", stop); err != nil {
		t.Fatal(err)
	} else if result.Identity != "" {
		t.Fatalf("normal Cursor stop unexpectedly reported identity: %+v", result)
	}
	if _, err := Capture("", ProviderCursor, "subagentStart", start); err != nil {
		t.Fatal(err)
	}
	if err := BindDispatch(root, "run-1", "dispatch-1", "cursor-agent-1"); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyDispatch(root, "run-1", "dispatch-1")
	if err != nil {
		t.Fatal(err)
	}
	if verification.Outcome != Verified {
		t.Fatalf("expected normal Cursor payloads to verify, got %+v", verification)
	}

	if _, err := Capture("", ProviderCursor, "subagentStart", cursorPayload(otherRoot, true)); err != nil {
		t.Fatal(err)
	}
	if err := BindDispatch(otherRoot, "run-2", "dispatch-2", "cursor-agent-1"); err != nil {
		t.Fatal(err)
	}
	other, err := VerifyDispatch(otherRoot, "run-2", "dispatch-2")
	if err != nil {
		t.Fatal(err)
	}
	if other.Outcome != Rejected || !other.StartObserved || other.StopObserved {
		t.Fatalf("expected project roots to remain isolated, got %+v", other)
	}
}

func TestLifecycleRequiredProviderRejectsMissingOrMismatchedEvents(t *testing.T) {
	root := t.TempDir()
	useProvider(t, ProviderCursor)
	if err := BindDispatch(root, "run-1", "dispatch-1", "agent-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(root, ProviderCursor, "subagentStart", []byte(`{"subagent_id":"other-agent","conversation_id":"conversation-1","generation_id":"generation-1","subagent_type":"generalPurpose","task":"different task"}`)); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyDispatch(root, "run-1", "dispatch-1")
	if err != nil {
		t.Fatal(err)
	}
	if verification.Outcome != Rejected || verification.StartObserved || verification.StopObserved {
		t.Fatalf("expected missing matching events to reject, got %+v", verification)
	}
}

func TestLifecycleUnclaimedObservationsRetireWithRun(t *testing.T) {
	root := t.TempDir()
	if err := BeginRun(root, "run-1"); err != nil {
		t.Fatal(err)
	}
	record := eventRecord{Provider: ProviderClaude, Event: eventStart, Identity: "unclaimed-agent"}
	if _, err := Capture(root, ProviderClaude, "SubagentStart", []byte(`{"agent_id":"unclaimed-agent"}`)); err != nil {
		t.Fatal(err)
	}
	path := pendingEventPath(root, "run-1", record)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected observation beneath its active run: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(root, ".gates", "tmp", "run-1")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("run cleanup did not retire its unclaimed lifecycle observation: %v", err)
	}
}

func TestLifecycleHookDefinitionsOwnProviderEventsAndCommands(t *testing.T) {
	tests := map[string][]HookDefinition{
		"claude": {
			{Event: "SubagentStart", Command: []string{"lifecycle", "capture", "--provider", ProviderClaude, "--event", "SubagentStart"}},
			{Event: "SubagentStop", Command: []string{"lifecycle", "capture", "--provider", ProviderClaude, "--event", "SubagentStop"}},
		},
		"codex": {
			{Event: "SubagentStart", Command: []string{"lifecycle", "capture", "--provider", ProviderCodex, "--event", "SubagentStart"}},
			{Event: "SubagentStop", Command: []string{"lifecycle", "capture", "--provider", ProviderCodex, "--event", "SubagentStop"}},
		},
		"cursor": {
			{Event: "subagentStart", Command: []string{"lifecycle", "capture", "--provider", ProviderCursor, "--event", "subagentStart"}},
			{Event: "subagentStop", Command: []string{"lifecycle", "capture", "--provider", ProviderCursor, "--event", "subagentStop"}},
		},
	}
	for host, want := range tests {
		t.Run(host, func(t *testing.T) {
			got, err := HookDefinitions(host)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("HookDefinitions(%q)=%#v want %#v", host, got, want)
			}
		})
	}
}

func TestLifecycleStopCaptureStoresTranscriptPath(t *testing.T) {
	root := t.TempDir()
	useProvider(t, ProviderCodex)
	if err := BeginRun(root, "run-1"); err != nil {
		t.Fatal(err)
	}
	if err := BindDispatch(root, "run-1", "dispatch-1", "agent-1"); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"subagent_id":"agent-1","agent_transcript_path":"/tmp/sessions/agent-1.jsonl"}`)
	if _, err := Capture(root, ProviderCodex, "SubagentStop", payload); err != nil {
		t.Fatal(err)
	}
	provider, path, err := DispatchTranscriptPath(root, "run-1", "dispatch-1")
	if err != nil {
		t.Fatal(err)
	}
	if provider != ProviderCodex || path != "/tmp/sessions/agent-1.jsonl" {
		t.Fatalf("DispatchTranscriptPath=%q,%q", provider, path)
	}
	if _, _, err := DispatchTranscriptPath(root, "run-1", "no-such-dispatch"); err != nil {
		t.Fatalf("missing binding must not error: %v", err)
	}
}

func TestLifecycleCursorAndPathlessStopsStayWithoutTranscriptPath(t *testing.T) {
	root := t.TempDir()
	useProvider(t, ProviderCodex)
	if err := BeginRun(root, "run-1"); err != nil {
		t.Fatal(err)
	}
	if err := BindDispatch(root, "run-1", "dispatch-1", "agent-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(root, ProviderCodex, "SubagentStop", []byte(`{"subagent_id":"agent-1"}`)); err != nil {
		t.Fatal(err)
	}
	if _, path, err := DispatchTranscriptPath(root, "run-1", "dispatch-1"); err != nil || path != "" {
		t.Fatalf("pathless stop reported path %q err %v", path, err)
	}
	useProvider(t, ProviderCursor)
	if err := BindDispatch(root, "run-1", "dispatch-cursor", "cursor-agent"); err != nil {
		t.Fatal(err)
	}
	cursor := []byte(`{"subagent_id":"cursor-agent","agent_transcript_path":"/tmp/sessions/cursor.jsonl","conversation_id":"conversation-1","generation_id":"generation-1","subagent_type":"generalPurpose","task":"prepared formal-gates dispatch"}`)
	if _, err := Capture(root, ProviderCursor, "subagentStop", cursor); err != nil {
		t.Fatal(err)
	}
	if _, path, err := DispatchTranscriptPath(root, "run-1", "dispatch-cursor"); err != nil || path != "" {
		t.Fatalf("Cursor stop stored a transcript path %q err %v", path, err)
	}
}

func TestLifecycleCodexIsUnavailable(t *testing.T) {
	root := t.TempDir()
	useProvider(t, ProviderCodex)
	if err := BindDispatch(root, "run-1", "dispatch-1", "agent-1"); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyDispatch(root, "run-1", "dispatch-1")
	if err != nil {
		t.Fatal(err)
	}
	if verification.Outcome != Unavailable {
		t.Fatalf("expected UNAVAILABLE, got %+v", verification)
	}
}

func TestProviderFromExecutable(t *testing.T) {
	tests := map[string]string{
		filepath.Join("tmp", ".claude", "skills", "formal-gates", "bin", "formal-gates"): ProviderClaude,
		filepath.Join("tmp", ".codex", "skills", "formal-gates", "bin", "formal-gates"):  ProviderCodex,
		filepath.Join("tmp", ".cursor", "formal-gates", "bin", "formal-gates"):           ProviderCursor,
		filepath.Join("tmp", "source", "formal-gates"):                                   ProviderCodex,
	}
	for path, want := range tests {
		t.Run(fmt.Sprintf("%s", want), func(t *testing.T) {
			if got := providerFromExecutable(path); got != want {
				t.Fatalf("providerFromExecutable(%q)=%q want %q", path, got, want)
			}
		})
	}
}

func TestClaudeCaptureResolvesActiveRunAboveSubagentWorkingDirectory(t *testing.T) {
	root := t.TempDir()
	subdirectory := filepath.Join(root, "services", "worker")
	if err := os.MkdirAll(subdirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := BeginRun(root, "run-1"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_PROJECT_DIR", root)
	useProvider(t, ProviderClaude)
	for _, event := range []string{"SubagentStart", "SubagentStop"} {
		payload := []byte(fmt.Sprintf(`{"cwd":%q,"agent_id":"claude-subdirectory-agent"}`, subdirectory))
		if _, err := Capture("", ProviderClaude, event, payload); err != nil {
			t.Fatal(err)
		}
	}
	if err := BindDispatch(root, "run-1", "dispatch-1", "claude-subdirectory-agent"); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyDispatch(root, "run-1", "dispatch-1")
	if err != nil {
		t.Fatal(err)
	}
	if verification.Outcome != Verified {
		t.Fatalf("expected Claude subdirectory events to verify at the active run root, got %+v", verification)
	}
	if _, err := os.Stat(filepath.Join(subdirectory, ".gates")); !os.IsNotExist(err) {
		t.Fatalf("Claude capture wrote lifecycle data beneath mutable cwd: %v", err)
	}
}

func TestCursorCaptureStagesForAllActiveWorkspaceRoots(t *testing.T) {
	firstRoot, runRoot := t.TempDir(), t.TempDir()
	if err := BeginRun(firstRoot, "run-2"); err != nil {
		t.Fatal(err)
	}
	if err := BeginRun(runRoot, "run-1"); err != nil {
		t.Fatal(err)
	}
	useProvider(t, ProviderCursor)
	payload := func(includeIdentity bool) []byte {
		identity := ""
		if includeIdentity {
			identity = `"subagent_id":"cursor-multi-root-agent",`
		}
		return []byte(fmt.Sprintf(`{%s"conversation_id":"conversation-1","parent_conversation_id":"conversation-1","generation_id":"generation-1","subagent_type":"generalPurpose","task":"prepared formal-gates dispatch","workspace_roots":[%q,%q]}`, identity, firstRoot, runRoot))
	}
	if _, err := Capture("", ProviderCursor, "subagentStart", payload(true)); err != nil {
		t.Fatal(err)
	}
	if err := BindDispatch(runRoot, "run-1", "dispatch-1", "cursor-multi-root-agent"); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture("", ProviderCursor, "subagentStop", payload(false)); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyDispatch(runRoot, "run-1", "dispatch-1")
	if err != nil {
		t.Fatal(err)
	}
	if verification.Outcome != Verified {
		t.Fatalf("expected Cursor multi-root events to verify at the active run root, got %+v", verification)
	}
	pending, err := pendingEvents(firstRoot, "run-2", ProviderCursor)
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Fatalf("expected unclaimed observations to remain available to the other active workspace root, got %d", len(pending))
	}
}

func cursorPayload(root string, includeIdentity bool) []byte {
	identity := ""
	if includeIdentity {
		identity = `"subagent_id":"cursor-agent-1",`
	}
	return []byte(fmt.Sprintf(`{%s"conversation_id":"conversation-1","parent_conversation_id":"conversation-1","generation_id":"generation-1","subagent_type":"generalPurpose","task":"prepared formal-gates dispatch","workspace_roots":[%q]}`, identity, root))
}

func assertRunOwnedEvents(t *testing.T, root, runID, dispatchID string) {
	t.Helper()
	for _, event := range []string{eventStart, eventStop} {
		if _, err := os.Stat(runEventPath(root, runID, dispatchID, event)); err != nil {
			t.Fatalf("expected run-owned %s event: %v", event, err)
		}
	}
	pending, err := filepath.Glob(filepath.Join(pendingRoot(root, runID), "*", "*", "*.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("claimed observations remained outside the run: %v", pending)
	}
}

func useProvider(t *testing.T, provider string) {
	t.Helper()
	prior := executablePath
	path := map[string]string{
		ProviderClaude: filepath.Join(t.TempDir(), ".claude", "skills", "formal-gates", "bin", "formal-gates"),
		ProviderCodex:  filepath.Join(t.TempDir(), ".codex", "skills", "formal-gates", "bin", "formal-gates"),
		ProviderCursor: filepath.Join(t.TempDir(), ".cursor", "formal-gates", "bin", "formal-gates"),
	}[provider]
	executablePath = func() (string, error) { return path, nil }
	t.Cleanup(func() { executablePath = prior })
}
