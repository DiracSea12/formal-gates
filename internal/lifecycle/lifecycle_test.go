package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/host"
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
	// 独立期望：每个宿主定义自己的生命周期捕获事件集合。
	wantEvents := map[string][]string{
		ProviderClaude:   {"SubagentStart", "SubagentStop"},
		ProviderCodex:    {"SubagentStart", "SubagentStop"},
		ProviderCursor:   {"subagentStart", "subagentStop"},
		ProviderDeepSeek: {"SubagentStart", "SubagentStop"},
		ProviderZCode:    {"PreToolUse", "PostToolUse", "PostToolUseFailure"},
	}
	for _, host := range []string{ProviderClaude, ProviderCodex, ProviderCursor, ProviderDeepSeek, ProviderZCode} {
		t.Run(host, func(t *testing.T) {
			// 对照实际定义来源：host registry 注入宿主事件集合，定义须一一对应。
			adapter, err := adapterFor(host)
			if err != nil {
				t.Fatal(err)
			}
			hooks, err := HookDefinitions(host)
			if err != nil {
				t.Fatal(err)
			}
			if len(hooks) != len(adapter.hookEvents) {
				t.Fatalf("HookDefinitions(%q) produced %d definitions, want one per adapter event (%d)", host, len(hooks), len(adapter.hookEvents))
			}
			gotEvents := make([]string, 0, len(hooks))
			for index, hook := range hooks {
				if hook.Event != adapter.hookEvents[index] {
					t.Fatalf("HookDefinitions(%q)[%d].Event=%q want %q", host, index, hook.Event, adapter.hookEvents[index])
				}
				gotEvents = append(gotEvents, hook.Event)
				// 命令形状为独立期望：capture 子命令携带宿主与事件，命令尾参即定义事件。
				wantCommand := []string{"lifecycle", "capture", "--provider", host, "--event", hook.Event}
				if !reflect.DeepEqual(hook.Command, wantCommand) {
					t.Fatalf("HookDefinitions(%q)[%d].Command=%#v want %#v", host, index, hook.Command, wantCommand)
				}
			}
			if !reflect.DeepEqual(gotEvents, wantEvents[host]) {
				t.Fatalf("HookDefinitions(%q) events=%v want %v", host, gotEvents, wantEvents[host])
			}
		})
	}
	// 未支持宿主拒绝。
	if _, err := HookDefinitions("unsupported-host"); err == nil {
		t.Fatal("unsupported lifecycle host was accepted")
	}
}

func TestLifecycleFactoryRegistryCoversEveryInstallableHost(t *testing.T) {
	for _, descriptor := range host.All() {
		if !descriptor.Installable {
			continue
		}
		adapter, err := adapterFor(descriptor.ID)
		if err != nil {
			t.Fatalf("installable host %q has no lifecycle adapter: %v", descriptor.ID, err)
		}
		if adapter.name != descriptor.ID {
			t.Fatalf("adapter for %q reports name %q", descriptor.ID, adapter.name)
		}
	}
}

func TestZCodeToolHooksCorrelateAgentCallLifecycle(t *testing.T) {
	root := t.TempDir()
	useProvider(t, ProviderZCode)
	if err := BeginRun(root, "run-zcode"); err != nil {
		t.Fatal(err)
	}
	payload := []byte(fmt.Sprintf(`{"cwd":%q,"tool_name":"Agent","tool_use_id":"call-1"}`, root))
	start, err := Capture("", ProviderZCode, "PreToolUse", payload)
	if err != nil {
		t.Fatal(err)
	}
	if start.Event != eventStart || start.Identity != "call-1" || len(start.Roots) != 1 {
		t.Fatalf("unexpected ZCode start capture: %+v", start)
	}
	stop, err := Capture("", ProviderZCode, "PostToolUse", payload)
	if err != nil {
		t.Fatal(err)
	}
	if stop.Event != eventStop || stop.Identity != "call-1" {
		t.Fatalf("unexpected ZCode stop capture: %+v", stop)
	}
	if err := BindDispatchWithProvider(root, "run-zcode", "dispatch-zcode", "call-1", ProviderZCode); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyDispatch(root, "run-zcode", "dispatch-zcode")
	if err != nil {
		t.Fatal(err)
	}
	if verification.Outcome != Verified || !verification.StartObserved || !verification.StopObserved {
		t.Fatalf("expected correlated ZCode Agent tool call to verify, got %+v", verification)
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

// TestLifecycleStopCaptureStoresInterruptionReason covers lifecycle capture
// extracts the interruption reason (including HTTP error codes) from host stop
// events and records it on the stop event; a host without a reason records "未知".
func TestLifecycleStopCaptureStoresInterruptionReason(t *testing.T) {
	root := t.TempDir()
	useProvider(t, ProviderClaude)
	if err := BeginRun(root, "run-1"); err != nil {
		t.Fatal(err)
	}
	if err := BindDispatch(root, "run-1", "dispatch-1", "agent-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(root, ProviderClaude, "SubagentStop", []byte(`{"subagent_id":"agent-1","stop_reason":"HTTP 429 rate limited"}`)); err != nil {
		t.Fatal(err)
	}
	reason, err := DispatchInterruptionReason(root, "run-1", "dispatch-1")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reason, "429") {
		t.Fatalf("capture did not record the HTTP interruption reason: %q", reason)
	}
	// 先绑定再捕获：让 stop 事件直接落到派发记录（含"未知"原因）。
	if err := BindDispatch(root, "run-1", "dispatch-2", "agent-2"); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(root, ProviderClaude, "SubagentStop", []byte(`{"subagent_id":"agent-2"}`)); err != nil {
		t.Fatal(err)
	}
	unknown, err := DispatchInterruptionReason(root, "run-1", "dispatch-2")
	if err != nil {
		t.Fatal(err)
	}
	if unknown != "未知" {
		t.Fatalf("host without a reason was not recorded as 未知: %q", unknown)
	}
}

// TestPayloadHTTPErrorCodeStandaloneToken covers payloadHTTPErrorCode
// only classifies a code when it appears as a standalone numeric token or in a
// dedicated error-code field — a substring inside a longer number or word must
// not be misclassified as an objective API interruption reason.
func TestPayloadHTTPErrorCodeStandaloneToken(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value any
		want  string
	}{
		{"standalone code in prose", map[string]any{"detail": "request throttled with 429 errors"}, "HTTP 429"},
		{"dedicated error code field", map[string]any{"status_code": "the http 503 code"}, "HTTP 503"},
		{"code at string edge", "429", "HTTP 429"},
		{"substring inside longer number ignored", map[string]any{"detail": "model id 4290 rejected"}, ""},
		{"substring inside word ignored", map[string]any{"detail": "http429proxy"}, ""},
		{"code as part of decimal ignored", map[string]any{"detail": "value 3.429 in logs"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := payloadHTTPErrorCode(tc.value); got != tc.want {
				t.Fatalf("payloadHTTPErrorCode(%v)=%q want %q", tc.value, got, tc.want)
			}
		})
	}
}

// TestLifecycleInterruptedDispatchVerification covers a dispatch with a
// start event and a recorded interruption reason but no paired stop is accepted
// as an interruption credential (Interrupted), not REJECTED.
func TestLifecycleInterruptedDispatchVerification(t *testing.T) {
	root := t.TempDir()
	useProvider(t, ProviderClaude)
	if err := BeginRun(root, "run-1"); err != nil {
		t.Fatal(err)
	}
	if err := BindDispatch(root, "run-1", "dispatch-1", "agent-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(root, ProviderClaude, "SubagentStart", []byte(`{"subagent_id":"agent-1"}`)); err != nil {
		t.Fatal(err)
	}
	// 写入带原因的 stop 事件后，把 stop 配对事件移除，模拟中断时只有 start + 已记录原因。
	if _, err := Capture(root, ProviderClaude, "SubagentStop", []byte(`{"subagent_id":"agent-1","stop_reason":"HTTP 503 overloaded"}`)); err != nil {
		t.Fatal(err)
	}
	stopPath := runEventPath(root, "run-1", "dispatch-1", eventStop)
	if err := os.Remove(stopPath); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyDispatch(root, "run-1", "dispatch-1")
	if err != nil {
		t.Fatal(err)
	}
	if verification.Outcome != Interrupted || !verification.StartObserved {
		t.Fatalf("interrupted dispatch was not accepted as Interrupted: %+v", verification)
	}
}

func TestLifecycleUninstalledProviderIsUnavailable(t *testing.T) {
	root := t.TempDir()
	useProvider(t, ProviderDefault)
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

// TestLifecycleInstalledCodexRequiresPairedEvents locks in the flip of the
// installed Codex provider to required: an uninstalled test/canary context stays
// UNAVAILABLE (see TestLifecycleUninstalledProviderIsUnavailable), while a real
// Codex install with no paired lifecycle events now REJECTS like Claude and
// Cursor.
func TestLifecycleInstalledCodexRequiresPairedEvents(t *testing.T) {
	root := t.TempDir()
	useProvider(t, ProviderCodex)
	if err := BindDispatch(root, "run-1", "dispatch-1", "agent-1"); err != nil {
		t.Fatal(err)
	}
	verification, err := VerifyDispatch(root, "run-1", "dispatch-1")
	if err != nil {
		t.Fatal(err)
	}
	if verification.Outcome != Rejected || verification.StartObserved || verification.StopObserved {
		t.Fatalf("expected installed Codex without paired events to reject, got %+v", verification)
	}
}

func TestProviderFromExecutable(t *testing.T) {
	tests := map[string]string{
		filepath.Join("tmp", ".claude", "skills", "formal-gates", "bin", "formal-gates"): ProviderClaude,
		filepath.Join("tmp", ".codex", "skills", "formal-gates", "bin", "formal-gates"):  ProviderCodex,
		filepath.Join("tmp", ".cursor", "formal-gates", "bin", "formal-gates"):           ProviderCursor,
		filepath.Join("tmp", ".zcode", "skills", "formal-gates", "bin", "formal-gates"):  ProviderDefault,
		filepath.Join("tmp", "source", "formal-gates"):                                   ProviderDefault,
	}
	for path, want := range tests {
		t.Run(fmt.Sprintf("%s", want), func(t *testing.T) {
			if got := providerFromExecutable(path); got != want {
				t.Fatalf("providerFromExecutable(%q)=%q want %q", path, got, want)
			}
		})
	}
}

func TestProviderFromEnvironmentRecognizesZCode(t *testing.T) {
	for _, key := range []string{"AI_AGENT", "ZCODE_PLUGIN_ROOT", "ZCODE_PLUGIN_ID", "ZCODE_PLUGIN_NAME"} {
		t.Setenv(key, "")
	}
	t.Setenv("AI_AGENT", "zcode")
	if got := providerFromEnvironment(); got != ProviderZCode {
		t.Fatalf("AI_AGENT=zcode resolved to %q", got)
	}
	t.Setenv("AI_AGENT", "")
	t.Setenv("ZCODE_PLUGIN_ROOT", "/tmp/zcode-plugin")
	if got := providerFromEnvironment(); got != ProviderZCode {
		t.Fatalf("ZCODE_PLUGIN_ROOT resolved to %q", got)
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
	deepseekHome := t.TempDir()
	path := map[string]string{
		ProviderClaude:   filepath.Join(t.TempDir(), ".claude", "skills", "formal-gates", "bin", "formal-gates"),
		ProviderCodex:    filepath.Join(t.TempDir(), ".codex", "skills", "formal-gates", "bin", "formal-gates"),
		ProviderCursor:   filepath.Join(t.TempDir(), ".cursor", "formal-gates", "bin", "formal-gates"),
		ProviderZCode:    filepath.Join(t.TempDir(), ".zcode", "skills", "formal-gates", "bin", "formal-gates"),
		ProviderDeepSeek: filepath.Join(deepseekHome, "skills", "formal-gates", "bin", "formal-gates"),
		ProviderDefault:  filepath.Join(t.TempDir(), "source", "formal-gates"),
	}[provider]
	// Neutralize host environment detection so the stubbed executable path is
	// the only provider signal in these tests.
	for _, key := range ProviderEnvironmentKeys() {
		t.Setenv(key, "")
	}
	if provider == ProviderDeepSeek {
		t.Setenv("DSH_HOME", deepseekHome)
	}
	executablePath = func() (string, error) { return path, nil }
	t.Cleanup(func() { executablePath = prior })
}
