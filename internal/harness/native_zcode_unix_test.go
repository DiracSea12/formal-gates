//go:build !windows

package harness

// The app-server execute sequence over a scripted fake peer: the full
// new-session turn through RunNative (the narrow command itself), the probe
// report, the setMode boundary (declare-once for NEW sessions, never for a
// resume), read-only tool policy, the account provider delivery, the
// interaction boundaries, the bounded turn wait and the refusal ladder.
// POSIX-only because the peer runs behind a #!/bin/sh wrapper.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

const zcodeTestPrompt = "Review the requirement."

// The scenario schema carries additionalProperties at the top level and
// nested inside an array item, so the prompt rendering's reorder is exercised
// recursively (the fixture's own key order is irrelevant; the renderer sorts).
var zcodeTestSchema = `{"additionalProperties": false, "properties": {"findings": {"items": {"additionalProperties": true, "type": "object"}, "type": "array"}, "status": {"type": "string"}}, "required": ["status", "summary"], "type": "object"}`

// zcodeTestSchemaPrompt is the scenario schema as the prompt embeds it:
// canonical, with each object's additionalProperties presented last.
const zcodeTestSchemaPrompt = `{"properties":{"findings":{"items":{"type":"object","additionalProperties":true},"type":"array"},` +
	`"status":{"type":"string"}},"required":["status","summary"],"type":"object","additionalProperties":false}`

// zcodeScenario wires one execute run: a scripted peer bound as the
// app-server launch (executable plus a proofed dummy script), a workspace and
// an evidence directory (resolved the way RunNative resolves them), and the
// canonical native-request.json bytes on disk. The steps factory receives the
// input id the run will bind, because the turn evidence has to name it.
type zcodeScenario struct {
	request   map[string]any
	directory string
	workspace string
	echoPath  string
	inputID   string
}

// The customize hook mutates the request map before it is serialised.
func newZcodeScenario(t *testing.T, steps func(inputID string) []peerStep, customize func(request map[string]any)) *zcodeScenario {
	t.Helper()
	t.Setenv("FORMAL_GATES_HARNESS_ROOT", t.TempDir())
	workspace := resolveNativePath(t.TempDir())
	directory := resolveNativePath(t.TempDir())
	inputID := "formal-gates-" + filepath.Base(directory)
	var script []peerStep
	if steps != nil {
		script = steps(inputID)
	}
	executable, echoPath := startPeer(t, peerScript{Steps: script})
	appServer := filepath.Join(filepath.Dir(executable), "zcode.cjs")
	if err := os.WriteFile(appServer, []byte("// fake app-server bundle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	executableProof, err := fileProofOf(executable)
	if err != nil {
		t.Fatal(err)
	}
	scriptProof, err := fileProofOf(appServer)
	if err != nil {
		t.Fatal(err)
	}
	request := map[string]any{
		"provider": "zcode",
		"binding": map[string]any{
			"provider": "zcode",
			"launch":   map[string]any{"executable": executable, "script": appServer},
			"files":    map[string]any{executable: executableProof, appServer: scriptProof},
		},
		"workspace":        workspace,
		"read_only":        true,
		"identity":         "",
		"model":            "",
		"reasoning_effort": "",
		"prompt":           zcodeTestPrompt,
		"schema":           decodeTestJSON(t, zcodeTestSchema),
	}
	if customize != nil {
		customize(request)
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	return &zcodeScenario{
		request:   request,
		directory: directory,
		workspace: workspace,
		echoPath:  echoPath,
		inputID:   inputID,
	}
}

// zcodeSettings is a configured host: a current selection m1 with advertised
// reasoning levels, a thought level and a mode.
const zcodeSettings = `{"model": {"current": {"providerId": "p1", "modelId": "m1"}, "available": [` +
	`{"ref": {"providerId": "p1", "modelId": "m1"}, "reasoning": {"levels": [{"value": "high"}, {"value": "low"}], "defaultLevel": "low"}}]}, ` +
	`"thoughtLevel": {"current": "low"}, "mode": {"current": "default"}}`

// zcodeSnapshotFrame is one create/resume response carrying the handshake, the
// session id and the settings block.
func zcodeSnapshotFrame(sessionID, settings string) string {
	return `{"id": "@ID@", "result": {"protocol": {"name": "ZCode Protocol", "version": 1}, ` +
		`"session": {"sessionId": "` + sessionID + `"}, "settings": ` + settings + `}}`
}

// zcodeEventLine renders one session/event notification for the fake host.
func zcodeEventLine(sessionID string, seq int, eventType, turnID, payload string) string {
	return `{"method": "session/event", "params": {"sessionId": "` + sessionID +
		`", "seq": ` + strconv.Itoa(seq) + `, "type": "` + eventType + `", "turnId": "` + turnID +
		`", "payload": ` + payload + `}}`
}

// zcodeTurnPayload builds one payload with the given input id.
func zcodeTurnPayload(inputID, extra string) string {
	return `{"inputId": "` + inputID + `"` + extra + `}`
}

// zcodeSuccessSteps is the happy new-session sequence after the snapshot: the
// subscribe acknowledgement, the yolo mode readback, the accepted input and
// this input's turn events.
func zcodeSuccessSteps(inputID string) []peerStep {
	return []peerStep{
		{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("s-1", zcodeSettings)},
		{On: `"method":"session/subscribe"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`},
		{On: `"method":"session/setMode"`, Send: `{"id": "@ID@", "result": {"settings": {"mode": {"current": "yolo"}}}}`},
		{On: `"method":"session/send"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1", "accepted": true}}`},
		{On: `"method":"session/send"`, Send: zcodeEventLine("s-1", 1, "turn.started", "t-1",
			zcodeTurnPayload(inputID, ""))},
		{On: `"method":"session/send"`, Send: zcodeEventLine("s-1", 2, "assistant/message", "",
			`{"inputId": "other-input"}`)},
		{On: `"method":"session/send"`, Send: zcodeEventLine("s-1", 3, "turn.completed", "t-1",
			zcodeTurnPayload(inputID, `, "resultType": "success", "response": `+zcodeTestResponseJSON+
				`, "usage": {"inputTokens": 9, "outputTokens": 2}`))},
		{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
	}
}

// zcodeWithStep replaces one step of a factory's script.
func zcodeWithStep(factory func(string) []peerStep, index int, step peerStep) func(string) []peerStep {
	return func(inputID string) []peerStep {
		steps := factory(inputID)
		steps[index] = step
		return steps
	}
}

// TestZcodeExecuteNewSessionSuccessTurn runs the whole narrow command: the
// canonical stdout receipt, the identity/tool-policy/native-source evidence,
// the mode declared once before the first turn, the prompt with the schema
// embedded canonically (additionalProperties last, value equal), and the
// turn's committed answer.
func TestZcodeExecuteNewSessionSuccessTurn(t *testing.T) {
	scenario := newZcodeScenario(t, zcodeSuccessSteps, func(request map[string]any) {
		request["read_only"] = false
	})
	var errout strings.Builder
	code := 0
	stdout := captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errout.String())
	}
	if errout.Len() != 0 {
		t.Fatalf("stderr = %q", errout.String())
	}
	if !strings.HasSuffix(stdout, "\n") || strings.Count(stdout, "\n") != 1 {
		t.Fatalf("stdout = %q", stdout)
	}
	value := objectOrEmpty(decodeTestJSON(t, stdout))
	if textOf(value["provider"]) != "zcode" || textOf(value["identity"]) != "s-1" ||
		textOf(value["native_turn"]) != "t-1" || textOf(value["input_id"]) != scenario.inputID {
		t.Fatalf("receipt = %v", value)
	}
	if !valuesEqual(value["selection"], map[string]any{
		"model":            `{"modelId":"m1","providerId":"p1"}`,
		"reasoning_effort": "low",
	}) {
		t.Fatalf("selection = %v", value["selection"])
	}
	if !valuesEqual(value["usage_evidence"], map[string]any{
		"inputTokens": jsonNumber("9"), "outputTokens": jsonNumber("2")}) {
		t.Fatalf("usage = %v", value["usage_evidence"])
	}
	if !valuesEqual(value["files"], proofsOf(t, scenario.directory, nativeSourceFiles)) {
		t.Fatalf("files = %v", value["files"])
	}

	// identity.json carries the applied selection and the provider.
	identityRecord, err := readJSONFile(filepath.Join(scenario.directory, "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(identityRecord, map[string]any{
		"identity": "s-1", "provider": "zcode", "workspace": scenario.workspace,
		"selection": map[string]any{
			"model":            `{"modelId":"m1","providerId":"p1"}`,
			"reasoning_effort": "low"},
	}) {
		t.Fatalf("identity.json = %v", identityRecord)
	}
	// tool-policy.json states the mode in force; native-source.json binds the
	// turn and carries the same mode.
	if policy := readTestFileBytes(t, filepath.Join(scenario.directory, "tool-policy.json")); string(policy) !=
		`{"permission_mode":"yolo","read_only":false}` {
		t.Fatalf("tool-policy.json = %s", policy)
	}
	sourceValue, err := readJSONFile(filepath.Join(scenario.directory, "native-source.json"))
	if err != nil {
		t.Fatal(err)
	}
	source := objectOrEmpty(sourceValue)
	if !valuesEqual(source, map[string]any{
		"sessionId": "s-1", "inputId": scenario.inputID,
		"selection": map[string]any{
			"model":            `{"modelId":"m1","providerId":"p1"}`,
			"reasoning_effort": "low"},
		"permission_mode": "yolo",
	}) {
		t.Fatalf("native-source.json = %v", source)
	}
	if record := readTestFileBytes(t, filepath.Join(scenario.directory, "source.json")); !valuesEqual(
		decodeTestJSON(t, string(record)), value) {
		t.Fatalf("source.json = %s", record)
	}
	if body := readTestFileBytes(t, filepath.Join(scenario.directory, "answer.raw.txt")); string(body) != zcodeTestResponse {
		t.Fatalf("answer.raw.txt = %q", body)
	}
	if body := readTestFileBytes(t, filepath.Join(scenario.directory, "answer.json")); string(body) != `{"status":"PASS","summary":"observed"}` {
		t.Fatalf("answer.json = %s", body)
	}

	echo := readTestFile(t, scenario.echoPath)
	if !strings.Contains(echo, `"persistence":"immediate"`) {
		t.Fatalf("create must persist immediately:\n%s", echo)
	}
	if strings.Contains(echo, "toolAllowlist") || strings.Contains(echo, "titleGenerationEnabled") {
		t.Fatalf("a plain new session carries neither allowlist nor probe flag:\n%s", echo)
	}
	if !strings.Contains(echo, `"deliveryKind":"desktop-continuous"`) {
		t.Fatalf("subscribe frame:\n%s", echo)
	}
	// The mode is declared once, before the first turn.
	setModeAt := strings.Index(echo, `"method":"session/setMode"`)
	sendAt := strings.Index(echo, `"method":"session/send"`)
	if setModeAt < 0 || sendAt < 0 || setModeAt > sendAt {
		t.Fatalf("setMode must precede send:\n%s", echo)
	}
	if strings.Count(echo, `"method":"session/setMode"`) != 1 {
		t.Fatalf("setMode must be declared exactly once:\n%s", echo)
	}
	// The prompt text: the request prompt, the fixed middle line, and the
	// schema embedded canonically with each object's additionalProperties
	// presented last.
	wantPrompt := zcodeTestPrompt + "\nReturn ONLY one JSON object matching this schema:\n" +
		zcodeTestSchemaPrompt
	send := frameByMethod(t, echo, "session/send")
	got := textOf(objectOrEmpty(send["params"])["content"])
	if got != wantPrompt {
		t.Fatalf("prompt:\n got %q\nwant %q", got, wantPrompt)
	}
	// The embedded schema is value-equal to the request's: only the key order
	// the directive is presented in differs.
	embedded := decodeTestJSON(t, got[strings.LastIndex(got, "\n")+1:])
	if !valuesEqual(embedded, scenario.request["schema"]) {
		t.Fatalf("embedded schema value = %v", embedded)
	}
	// The notification evidence: canonical lines, only the bound session.
	events := readTestFile(t, filepath.Join(scenario.directory, "native-events.jsonl"))
	if strings.Count(events, "\n") != 3 {
		t.Fatalf("native-events.jsonl = %q", events)
	}
	for _, line := range strings.Split(strings.TrimSpace(events), "\n") {
		if decoded := decodeTestJSON(t, line); !valuesEqual(objectOrEmpty(decoded)["sessionId"], "s-1") {
			t.Fatalf("foreign event recorded: %s", line)
		}
	}
}

// TestZcodeExecuteReadOnly pins the read-only role: the native tool allowlist
// on the session, the tool policy evidence, and no mode declaration at all.
func TestZcodeExecuteReadOnly(t *testing.T) {
	steps := func(inputID string) []peerStep {
		return []peerStep{
			{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("s-1", zcodeSettings)},
			{On: `"method":"session/subscribe"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`},
			{On: `"method":"session/send"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1", "accepted": true}}`},
			{On: `"method":"session/send"`, Send: zcodeEventLine("s-1", 1, "turn.started", "t-1",
				zcodeTurnPayload(inputID, ""))},
			{On: `"method":"session/send"`, Send: zcodeEventLine("s-1", 2, "turn.completed", "t-1",
				zcodeTurnPayload(inputID, `, "resultType": "success", "response": `+zcodeTestResponseJSON))},
			{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
		}
	}
	scenario := newZcodeScenario(t, steps, nil)
	var errout strings.Builder
	code := 0
	captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errout.String())
	}
	if policy := readTestFileBytes(t, filepath.Join(scenario.directory, "tool-policy.json")); string(policy) !=
		`{"allowed":["Read","Glob","Grep","WebSearch","WebFetch"],"read_only":true}` {
		t.Fatalf("tool-policy.json = %s", policy)
	}
	sourceValue, err := readJSONFile(filepath.Join(scenario.directory, "native-source.json"))
	if err != nil {
		t.Fatal(err)
	}
	source := objectOrEmpty(sourceValue)
	if _, present := source["permission_mode"]; present {
		t.Fatalf("read-only source must not carry a mode: %v", source)
	}
	echo := readTestFile(t, scenario.echoPath)
	if !strings.Contains(echo, `"toolAllowlist":["Read","Glob","Grep","WebSearch","WebFetch"]`) {
		t.Fatalf("create must carry the allowlist:\n%s", echo)
	}
	if strings.Contains(echo, `"method":"session/setMode"`) {
		t.Fatalf("a read-only session must not declare a mode:\n%s", echo)
	}
}

// TestZcodeExecuteProbe pins the probe path: no prompt, the close before the
// report, model_calls 0, and mcp_auth taken from the recorded interactions.
func TestZcodeExecuteProbe(t *testing.T) {
	steps := func(string) []peerStep {
		return []peerStep{
			// The host asks for the desktop authentication port before anything
			// else; the probe report names that answer.
			{Send: `{"id": "auth-1", "method": "interaction/requestOfficialMcpAuthHeaders", "params": {}}`},
			{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("s-1", zcodeSettings)},
			{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
		}
	}
	scenario := newZcodeScenario(t, steps, func(request map[string]any) {
		request["probe"] = true
	})
	var errout strings.Builder
	code := 0
	stdout := captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errout.String())
	}
	value := objectOrEmpty(decodeTestJSON(t, stdout))
	if textOf(value["model_calls"]) != "0" || textOf(value["native_tools"]) != "not_verified" ||
		textOf(value["mcp_auth"]) != "unavailable" || textOf(value["provider"]) != "zcode" {
		t.Fatalf("probe report = %v", value)
	}
	if !valuesEqual(value["protocol"], map[string]any{"name": "ZCode Protocol", "version": jsonNumber("1")}) {
		t.Fatalf("protocol = %v", value["protocol"])
	}
	if !valuesEqual(value["interactions"], []any{map[string]any{
		"method": "interaction/requestOfficialMcpAuthHeaders", "status": "official_auth_unavailable"}}) {
		t.Fatalf("interactions = %v", value["interactions"])
	}
	echo := readTestFile(t, scenario.echoPath)
	if strings.Contains(echo, `"method":"session/send"`) {
		t.Fatalf("a probe must not send a prompt:\n%s", echo)
	}
	if strings.Index(echo, `"method":"session/close"`) < strings.Index(echo, `"method":"session/create"`) {
		t.Fatalf("close must follow create:\n%s", echo)
	}
	if !strings.Contains(echo, `"titleGenerationEnabled":false`) {
		t.Fatalf("create must disable title generation:\n%s", echo)
	}
	if _, statErr := os.Stat(filepath.Join(scenario.directory, "source.json")); !os.IsNotExist(statErr) {
		t.Fatal("a probe must not land source.json")
	}
}

// TestZcodeExecuteSetModeBoundary pins the mode discipline: the NEW session
// declares yolo once and a mismatching readback is refused, with no tool
// policy landed; a resumed session is never switched.
func TestZcodeExecuteSetModeBoundary(t *testing.T) {
	t.Run("readback refused", func(t *testing.T) {
		steps := func(string) []peerStep {
			return []peerStep{
				{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("s-1", zcodeSettings)},
				{On: `"method":"session/subscribe"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`},
				{On: `"method":"session/setMode"`, Send: `{"id": "@ID@", "result": {"settings": {"mode": {"current": "build"}}}}`},
				{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
			}
		}
		scenario := newZcodeScenario(t, steps, func(request map[string]any) {
			request["read_only"] = false
		})
		var errout strings.Builder
		code := 0
		captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
		if code != 1 {
			t.Fatalf("code = %d", code)
		}
		if got := nativeRecordError(t, scenario.directory); got !=
			"RuntimeBlocked: ZCODE_CONFIG: permission mode was not applied" {
			t.Fatalf("error = %q", got)
		}
		if _, statErr := os.Stat(filepath.Join(scenario.directory, "tool-policy.json")); !os.IsNotExist(statErr) {
			t.Fatal("a refused mode must not land tool-policy.json")
		}
	})

	t.Run("resume keeps the snapshot mode", func(t *testing.T) {
		steps := func(inputID string) []peerStep {
			return []peerStep{
				{On: `"method":"session/resume"`, Send: zcodeSnapshotFrame("s-1",
					`{"model": {"current": {"providerId": "p1", "modelId": "m1"}}, "mode": {"current": "paused"}}`)},
				{On: `"method":"session/subscribe"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`},
				{On: `"method":"session/send"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1", "accepted": true}}`},
				{On: `"method":"session/send"`, Send: zcodeEventLine("s-1", 1, "turn.started", "t-9",
					zcodeTurnPayload(inputID, ""))},
				{On: `"method":"session/send"`, Send: zcodeEventLine("s-1", 2, "turn.completed", "t-9",
					zcodeTurnPayload(inputID, `, "resultType": "success", "response": `+zcodeTestResponseJSON))},
				{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
			}
		}
		scenario := newZcodeScenario(t, steps, func(request map[string]any) {
			request["read_only"] = false
			request["identity"] = "s-1"
		})
		var errout strings.Builder
		code := 0
		captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
		if code != 0 {
			t.Fatalf("code = %d, stderr = %q", code, errout.String())
		}
		echo := readTestFile(t, scenario.echoPath)
		if strings.Contains(echo, `"method":"session/setMode"`) {
			t.Fatalf("a resumed session must never switch modes:\n%s", echo)
		}
		if policy := readTestFileBytes(t, filepath.Join(scenario.directory, "tool-policy.json")); string(policy) !=
			`{"permission_mode":"paused","read_only":false}` {
			t.Fatalf("tool-policy.json = %s", policy)
		}
		sourceValue, err := readJSONFile(filepath.Join(scenario.directory, "native-source.json"))
		if err != nil {
			t.Fatal(err)
		}
		source := objectOrEmpty(sourceValue)
		if !valuesEqual(source["permission_mode"], "paused") {
			t.Fatalf("native-source mode = %v", source["permission_mode"])
		}
		if textOf(source["inputId"]) != scenario.inputID {
			t.Fatalf("inputId = %v", source["inputId"])
		}
	})
}

// TestZcodeExecuteSelection pins the selection mechanism through the peer:
// the requested exact reference is applied with its advertised reasoning
// level, and every unapplied or unavailable value is refused without
// fallback (合同选型机制各拒绝分支).
func TestZcodeExecuteSelection(t *testing.T) {
	appliedSettings := `{"model": {"current": {"providerId": "p1", "modelId": "m0"}, "available": [` +
		`{"ref": {"providerId": "p1", "modelId": "m0"}}, ` +
		`{"ref": {"providerId": "p1", "modelId": "m1"}, "reasoning": {"levels": [{"value": "high"}, {"value": "low"}], "defaultLevel": "low"}}]}, ` +
		`"thoughtLevel": {"current": "low"}, "mode": {"current": "default"}}`
	appliedSteps := func(setModelReply, setThoughtReply string) func(string) []peerStep {
		return func(inputID string) []peerStep {
			return []peerStep{
				{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("s-1", appliedSettings)},
				{On: `"method":"session/setModel"`, Send: setModelReply},
				{On: `"method":"session/setThoughtLevel"`, Send: setThoughtReply},
				{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
				{On: `"method":"session/subscribe"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`},
				{On: `"method":"session/setMode"`, Send: `{"id": "@ID@", "result": {"settings": {"mode": {"current": "yolo"}}}}`},
				{On: `"method":"session/send"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1", "accepted": true}}`},
				{On: `"method":"session/send"`, Send: zcodeEventLine("s-1", 1, "turn.started", "t-1",
					zcodeTurnPayload(inputID, ""))},
				{On: `"method":"session/send"`, Send: zcodeEventLine("s-1", 2, "turn.completed", "t-1",
					zcodeTurnPayload(inputID, `, "resultType": "success", "response": `+zcodeTestResponseJSON))},
				{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
			}
		}
	}
	// The real host answers every readback with the full settings block.
	setModelApplied := `{"id": "@ID@", "result": {"settings": {"model": {"current": {"providerId": "p1", "modelId": "m1"}}, "thoughtLevel": {"current": "low"}}}}`
	setThoughtApplied := `{"id": "@ID@", "result": {"settings": {"model": {"current": {"providerId": "p1", "modelId": "m1"}}, "thoughtLevel": {"current": "high"}}}}`
	setModelIgnored := `{"id": "@ID@", "result": {"settings": {"model": {"current": {"providerId": "p1", "modelId": "m0"}}, "thoughtLevel": {"current": "low"}}}}`
	setThoughtIgnored := `{"id": "@ID@", "result": {"settings": {"model": {"current": {"providerId": "p1", "modelId": "m1"}}, "thoughtLevel": {"current": "low"}}}}`
	noCurrentSettings := `{"model": {"available": [{"ref": {"providerId": "p1", "modelId": "m1"}}]}, "mode": {"current": "default"}}`
	noCurrentSteps := func(string) []peerStep {
		return []peerStep{
			{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("s-1", noCurrentSettings)},
			{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
		}
	}
	// An empty advertised list is an empty provider registry on the 2026-09-19
	// host: setModel cannot apply anything there, so the change is refused
	// before the frame is fired (probed; the failing G2c run jobs hit this
	// state after their account overlay had not been delivered).
	nothingAdvertisedSettings := `{"model": {"current": {"providerId": "p1", "modelId": "m0"}, "available": []}, ` +
		`"thoughtLevel": {"current": "low"}, "mode": {"current": "default"}}`
	nothingAdvertisedSteps := func(string) []peerStep {
		return []peerStep{
			{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("s-1", nothingAdvertisedSettings)},
			{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
		}
	}

	t.Run("applied with the advertised level", func(t *testing.T) {
		scenario := newZcodeScenario(t, appliedSteps(setModelApplied, setThoughtApplied),
			func(request map[string]any) {
				request["read_only"] = false
				request["model"] = `{"providerId": "p1", "modelId": "m1"}`
				request["reasoning_effort"] = "high"
			})
		var errout strings.Builder
		code := 0
		stdout := captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
		if code != 0 {
			t.Fatalf("code = %d, stderr = %q", code, errout.String())
		}
		if !valuesEqual(objectOrEmpty(decodeTestJSON(t, stdout))["selection"], map[string]any{
			"model":            `{"modelId":"m1","providerId":"p1"}`,
			"reasoning_effort": "high",
		}) {
			t.Fatalf("selection = %s", stdout)
		}
		echo := readTestFile(t, scenario.echoPath)
		setModel := frameByMethod(t, echo, "session/setModel")
		model := objectOrEmpty(setModel["params"])["model"]
		// The requested effort rides with the exact reference; the advertised
		// default applies only when none was asked for (unit-pinned above).
		// This object with an advertised options.reasoningLevel is the only
		// model shape the 2026-09-19 host accepts (probed: a string model is
		// rejected by the method's params schema and an object without
		// options by the registry itself).
		if !valuesEqual(model, decodeTestJSON(t, `{"modelId": "m1", "options": {"reasoningLevel": "high"}, "providerId": "p1"}`)) {
			t.Fatalf("setModel model = %v", model)
		}
		if !strings.Contains(echo, `"persistAsWorkspaceLastUsed":false`) {
			t.Fatalf("setModel must not persist:\n%s", echo)
		}
		setThought := frameByMethod(t, echo, "session/setThoughtLevel")
		if textOf(objectOrEmpty(setThought["params"])["thoughtLevel"]) != "high" {
			t.Fatalf("setThoughtLevel params = %v", setThought["params"])
		}
	})

	cases := []struct {
		name      string
		steps     func(string) []peerStep
		customize func(request map[string]any)
		want      string
		noFrame   string
	}{
		{"not advertised", appliedSteps(setModelApplied, setThoughtApplied),
			func(request map[string]any) {
				request["read_only"] = false
				request["model"] = `{"providerId": "p9", "modelId": "m9"}`
			},
			"RuntimeBlocked: ZCODE_CONFIG: requested exact provider/model is not advertised; no fallback",
			"session/setModel"},
		{"nothing advertised is the same refusal", nothingAdvertisedSteps,
			func(request map[string]any) {
				request["read_only"] = false
				request["model"] = `{"providerId": "p1", "modelId": "m1"}`
			},
			"RuntimeBlocked: ZCODE_CONFIG: requested exact provider/model is not advertised; no fallback",
			"session/setModel"},
		{"model not applied", appliedSteps(setModelIgnored, setThoughtApplied),
			func(request map[string]any) {
				request["read_only"] = false
				request["model"] = `{"providerId": "p1", "modelId": "m1"}`
			},
			"RuntimeBlocked: ZCODE_CONFIG: requested selection was not applied", ""},
		{"thought level not applied", appliedSteps(setModelApplied, setThoughtIgnored),
			func(request map[string]any) {
				request["read_only"] = false
				request["model"] = `{"providerId": "p1", "modelId": "m1"}`
				request["reasoning_effort"] = "high"
			},
			"RuntimeBlocked: ZCODE_CONFIG: reasoning setting was not applied", ""},
		{"native selected model missing", noCurrentSteps,
			func(request map[string]any) { request["read_only"] = false },
			"RuntimeBlocked: ZCODE_CONFIG: native selected model missing", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scenario := newZcodeScenario(t, tc.steps, tc.customize)
			var errout strings.Builder
			code := 0
			stdout := captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
			if code != 1 || stdout != "" {
				t.Fatalf("code = %d stdout = %q", code, stdout)
			}
			if got := nativeRecordError(t, scenario.directory); got != tc.want {
				t.Fatalf("error = %q, want %q", got, tc.want)
			}
			if tc.noFrame != "" {
				if echo := readTestFile(t, scenario.echoPath); strings.Contains(
					echo, `"method":"`+tc.noFrame+`"`) {
					t.Fatalf("a refused selection must not fire %s:\n%s", tc.noFrame, echo)
				}
			}
		})
	}
}

// TestZcodeExecuteDeliverAccountProviders pins the overlay handover: the
// delivery goes before the session, the host's answer is read back and
// recorded, and an answer that does not confirm the overlay is refused.
func TestZcodeExecuteDeliverAccountProviders(t *testing.T) {
	data := t.TempDir()
	if err := os.MkdirAll(filepath.Join(data, ".zcode", "v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, ".zcode", "v2", "credentials.json"),
		[]byte(`{"account-provider:coding-plan:p1:account:u1:api-key": "cipher"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	catalogue := filepath.Join(t.TempDir(), "zcode-builtin.json")
	if err := os.WriteFile(catalogue, []byte(`{"revision": 7, "config": {"providerConfigRules": {"providerRules": [
		{"providerId": "p1", "config": {"access": {"type": "zhipu-account", "accountType": "zhipu", "mode": "coding-plan"}}}]}}}`),
		0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ZCODE_DATA_BASE_DIR", data)

	baseSteps := func(reply string) func(string) []peerStep {
		return func(inputID string) []peerStep {
			return []peerStep{
				{On: `"method":"provider/updateAccountConfig"`, Send: reply},
				{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("s-1", zcodeSettings)},
				{On: `"method":"session/subscribe"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`},
				{On: `"method":"session/setMode"`, Send: `{"id": "@ID@", "result": {"settings": {"mode": {"current": "yolo"}}}}`},
				{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
				{On: `"method":"session/send"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1", "accepted": true}}`},
				{On: `"method":"session/send"`, Send: zcodeEventLine("s-1", 1, "turn.started", "t-1",
					zcodeTurnPayload(inputID, ""))},
				{On: `"method":"session/send"`, Send: zcodeEventLine("s-1", 2, "turn.completed", "t-1",
					zcodeTurnPayload(inputID, `, "resultType": "success", "response": `+zcodeTestResponseJSON))},
				{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
			}
		}
	}
	pin := func(request map[string]any) {
		request["read_only"] = false
		binding := objectOrEmpty(request["binding"])
		binding["environment"] = map[string]any{zcodeBuiltinCatalogueEnv: catalogue}
	}

	scenario := newZcodeScenario(t, baseSteps(`{"id": "@ID@", "result": {"status": "received", "providerCount": 1}}`), pin)
	var errout strings.Builder
	code := 0
	captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errout.String())
	}
	record, err := readJSONFile(filepath.Join(scenario.directory, "account-provider.json"))
	if err != nil {
		t.Fatal(err)
	}
	resolved, err := filepath.Abs(catalogue)
	if err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(record, map[string]any{
		"catalogue": catalogue, "based_on": "zcode-builtin:7:" + zcodePathHash(t, resolved),
		"providers": []any{"p1"}, "current": "p1", "current_source": "provisioning-order",
		"status": "received", "provider_count": jsonNumber("1"), "source": "host credential index",
	}) {
		t.Fatalf("account-provider.json = %v", record)
	}
	echo := readTestFile(t, scenario.echoPath)
	if strings.Index(echo, `"method":"provider/updateAccountConfig"`) > strings.Index(echo, `"method":"session/create"`) {
		t.Fatalf("the overlay must be delivered before the session:\n%s", echo)
	}
	if !strings.Contains(echo, `"basedOnZCodeBuiltinRevision":"zcode-builtin:7:`+
		zcodePathHash(t, resolved)+`"`) {
		t.Fatalf("the delivery must carry the revision:\n%s", echo)
	}

	scenario = newZcodeScenario(t, baseSteps(`{"id": "@ID@", "result": {"status": "received", "providerCount": 2}}`), pin)
	errout.Reset()
	captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	if code != 1 {
		t.Fatalf("code = %d", code)
	}
	if got := nativeRecordError(t, scenario.directory); got != "RuntimeBlocked: ZCODE_ACCOUNT_CONFIG: the host did not "+
		`accept the account provider configuration: {"providerCount":2,"status":"received"}` {
		t.Fatalf("error = %q", got)
	}
}

// TestZcodeExecuteResumeUnavailable pins the SessionUnavailable
// classification through the whole command: the typed record lands and no
// identity does.
func TestZcodeExecuteResumeUnavailable(t *testing.T) {
	steps := func(string) []peerStep {
		return []peerStep{
			{On: `"method":"session/resume"`, Send: `{"id": "@ID@", "error": {"code": -32004, "message": "Session not found: s-1"}}`},
		}
	}
	scenario := newZcodeScenario(t, steps, func(request map[string]any) {
		request["identity"] = "s-1"
	})
	var errout strings.Builder
	code := 0
	stdout := captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	if code != 1 || stdout != "" {
		t.Fatalf("code = %d stdout = %q", code, stdout)
	}
	record, err := readJSONFile(filepath.Join(scenario.directory, "native-error.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCanonical(t, record); got != `{"error":"SessionUnavailable: ZCODE_SESSION_UNAVAILABLE: Session not found: s-1",`+
		`"error_kind":"SESSION_UNAVAILABLE","identity":"s-1","method":"session/resume","native_code":-32004}` {
		t.Fatalf("record = %s", got)
	}
	if _, statErr := os.Stat(filepath.Join(scenario.directory, "identity.json")); !os.IsNotExist(statErr) {
		t.Fatal("a refused resume must not land identity.json")
	}
}

// TestZcodeExecuteInteractionRequired pins the user-decision boundary: the
// reverse permission request raises the refusal and no reply frame is sent.
func TestZcodeExecuteInteractionRequired(t *testing.T) {
	steps := func(string) []peerStep {
		return []peerStep{
			{Send: `{"id": "perm-1", "method": "interaction/requestPermission", "params": {"title": "run a tool"}}`},
			{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("s-1", zcodeSettings)},
			{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
		}
	}
	scenario := newZcodeScenario(t, steps, nil)
	var errout strings.Builder
	code := 0
	captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	if code != 1 {
		t.Fatalf("code = %d", code)
	}
	if got := nativeRecordError(t, scenario.directory); got != "RuntimeBlocked: ZCODE_INTERACTION_REQUIRED: "+
		"interaction/requestPermission needs a native interactive client; no user decision was sent" {
		t.Fatalf("error = %q", got)
	}
	echo := readTestFile(t, scenario.echoPath)
	if strings.Contains(echo, `"id":"perm-1"`) {
		t.Fatalf("a user decision was answered:\n%s", echo)
	}
}

// TestZcodeExecuteRuntimeHeadersCredential pins the one machine-answered
// interaction through a real peer: the credential reaches the protocol reply
// and no evidence file. Skipped when node is absent.
func TestZcodeExecuteRuntimeHeadersCredential(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node is not available")
	}
	steps := func(string) []peerStep {
		return []peerStep{
			{Send: `{"id": "hdr-1", "method": "interaction/requestProviderRuntimeHeaders", "params": {"providerId": "p1"}}`},
			{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("s-1", zcodeSettings)},
			{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
		}
	}
	scenario := newZcodeScenario(t, steps, func(request map[string]any) { request["probe"] = true })
	// The scenario owns FORMAL_GATES_HARNESS_ROOT; the fake helper and the
	// store it reads land there before the run starts.
	packageDir := filepath.Join(os.Getenv("FORMAL_GATES_HARNESS_ROOT"),
		"runtime", "langgraph", "formal_gates_harness")
	if err := os.MkdirAll(packageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(packageDir, zcodeCredentialHelper),
		[]byte("process.stdout.write('key-for-' + process.argv[2]);\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := filepath.Join(t.TempDir(), "credentials.json")
	if err := os.WriteFile(store, []byte(`{"account-provider:coding-plan:p1:account:u1:api-key": "cipher"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(zcodeCredentialHelperEnv, store)
	var errout strings.Builder
	code := 0
	stdout := captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errout.String())
	}
	value := objectOrEmpty(decodeTestJSON(t, stdout))
	if !valuesEqual(value["interactions"], []any{map[string]any{
		"method": zcodeRuntimeHeadersMethod, "status": "provider-runtime-headers-applied"}}) {
		t.Fatalf("interactions = %v", value["interactions"])
	}
	echo := readTestFile(t, scenario.echoPath)
	if !strings.Contains(echo, `{"id":"hdr-1","result":{"headersApplied":true,"requestAuth":{"apiKey":"key-for-p1"}}}`) {
		t.Fatalf("the credential reply is missing from the echo:\n%s", echo)
	}
	for _, name := range []string{"protocol.jsonl", "host.stderr", "interaction-status.json"} {
		if body := readTestFileBytes(t, filepath.Join(scenario.directory, name)); strings.Contains(string(body), "key-for-p1") {
			t.Fatalf("the credential leaked into %s", name)
		}
	}
}

// TestZcodeExecuteBoundedWait pins the 900-second bound's two ends: a silent
// host ends the wait through the receive timeout (whose text states the
// session is not assumed lost), and a host that keeps talking without a
// terminal event ends it at the deadline itself.
func TestZcodeExecuteBoundedWait(t *testing.T) {
	shorten := func(t *testing.T) {
		t.Helper()
		saved := zcodeTurnBudget
		zcodeTurnBudget = 2 * time.Second
		t.Cleanup(func() { zcodeTurnBudget = saved })
	}
	preamble := func(string) []peerStep {
		return []peerStep{
			{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("s-1", zcodeSettings)},
			{On: `"method":"session/subscribe"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`},
			{On: `"method":"session/setMode"`, Send: `{"id": "@ID@", "result": {"settings": {"mode": {"current": "yolo"}}}}`},
			{On: `"method":"session/send"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1", "accepted": true}}`},
			{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
		}
	}

	t.Run("silent host", func(t *testing.T) {
		shorten(t)
		scenario := newZcodeScenario(t, preamble, func(request map[string]any) {
			request["read_only"] = false
		})
		var errout strings.Builder
		code := 0
		captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
		if code != 1 {
			t.Fatalf("code = %d", code)
		}
		if got := nativeRecordError(t, scenario.directory); got !=
			"RuntimeBlocked: ZCODE_NATIVE_TIMEOUT: no native message; session is not assumed lost" {
			t.Fatalf("error = %q", got)
		}
	})

	t.Run("no terminal event", func(t *testing.T) {
		shorten(t)
		steps := func(inputID string) []peerStep {
			keepAlive := make([]peerStep, 0, 9)
			for i := 1; i <= 8; i++ {
				keepAlive = append(keepAlive, peerStep{
					On: `"method":"session/send"`, SleepSecs: 0.3,
					Send: zcodeEventLine("s-1", i, "assistant/message", "", `{"inputId": "other-input"}`),
				})
			}
			return append(preamble(inputID), keepAlive...)
		}
		scenario := newZcodeScenario(t, steps, func(request map[string]any) {
			request["read_only"] = false
		})
		var errout strings.Builder
		code := 0
		captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
		if code != 1 {
			t.Fatalf("code = %d", code)
		}
		if got := nativeRecordError(t, scenario.directory); got !=
			"RuntimeBlocked: ZCODE_TIMEOUT: turn incomplete; inspect the original invocation" {
			t.Fatalf("error = %q", got)
		}
	})
}

// TestZcodeExecuteRefusals pins the remaining refusal ladder of the sequence
// (合同错误文本表).
func TestZcodeExecuteRefusals(t *testing.T) {
	wrongProtocol := func(string) []peerStep {
		return []peerStep{
			{On: `"method":"session/create"`, Send: `{"id": "@ID@", "result": {"protocol": {"name": "Other Protocol", "version": 1}, "session": {"sessionId": "s-1"}}}`},
		}
	}
	missingIdentity := func(string) []peerStep {
		return []peerStep{
			{On: `"method":"session/create"`, Send: `{"id": "@ID@", "result": {"protocol": {"name": "ZCode Protocol", "version": 1}, "session": {}}}`},
		}
	}
	changedIdentity := func(string) []peerStep {
		return []peerStep{
			{On: `"method":"session/resume"`, Send: zcodeSnapshotFrame("s-9", zcodeSettings)},
			{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
		}
	}
	subscribeDiffers := zcodeWithStep(zcodeSuccessSteps, 1,
		peerStep{On: `"method":"session/subscribe"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-9"}}`})
	inputRefused := zcodeWithStep(zcodeSuccessSteps, 3,
		peerStep{On: `"method":"session/send"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1", "accepted": false}}`})

	cases := []struct {
		name      string
		steps     func(string) []peerStep
		customize func(request map[string]any)
		want      string
	}{
		{"protocol mismatch", wrongProtocol, nil,
			`RuntimeBlocked: ZCODE_PROTOCOL: the app-server does not speak ZCode Protocol v1: {"name":"Other Protocol","version":1}`},
		{"identity missing", missingIdentity, nil,
			"RuntimeBlocked: ZCODE_BINDING: missing or changed identity"},
		{"identity changed on resume", changedIdentity, func(request map[string]any) {
			request["identity"] = "s-1"
		}, "RuntimeBlocked: ZCODE_BINDING: missing or changed identity"},
		{"subscription identity differs", subscribeDiffers, func(request map[string]any) {
			request["read_only"] = false
		}, "RuntimeBlocked: ZCODE_BINDING: subscription identity differs"},
		{"input not accepted", inputRefused, func(request map[string]any) {
			request["read_only"] = false
		}, "RuntimeBlocked: ZCODE_BINDING: input not accepted"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scenario := newZcodeScenario(t, tc.steps, tc.customize)
			var errout strings.Builder
			code := 0
			stdout := captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
			if code != 1 || stdout != "" {
				t.Fatalf("code = %d stdout = %q", code, stdout)
			}
			if got := nativeRecordError(t, scenario.directory); got != tc.want {
				t.Fatalf("error = %q, want %q", got, tc.want)
			}
		})
	}
}
