//go:build !windows

package harness

// The ACP execute sequence over the scripted fake peer: the full new-session
// turn through RunNative (the narrow command itself), the probe path, and the
// refusal ladder (capability, identity, config readback, stop reason, session
// unavailability, the permission boundary). POSIX-only because the peer runs
// behind a #!/bin/sh wrapper.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const dshTestPrompt = "Review the requirement."

var dshTestSchema = `{"properties": {"findings": {"type": "array"}, "status": {"type": "string"}}, "required": ["status", "summary"], "type": "object"}`

const dshTestAnswerJSON = `{"status": "PASS", "summary": "observed", "findings": [], "check_reviews": {}}`

// dshScenario wires one execute run: a scripted peer bound as the dsh
// executable, a workspace and an evidence directory (resolved the way RunNative
// resolves it), and the canonical native-request.json bytes on disk.
type dshScenario struct {
	request   map[string]any
	directory string
	workspace string
	echoPath  string
}

// The customize hook mutates the request map before it is serialised.
func newDSHScenario(t *testing.T, steps []peerStep, customize func(request map[string]any)) *dshScenario {
	t.Helper()
	workspace := resolveNativePath(t.TempDir())
	directory := resolveNativePath(t.TempDir())
	// The evidence files the peer writes are resolved into the directory the
	// command itself will resolve, before the script is serialised.
	for i := range steps {
		for j := range steps[i].Files {
			steps[i].Files[j].Path = filepath.Join(directory, steps[i].Files[j].Path)
		}
	}
	executable, echoPath := startPeer(t, peerScript{Steps: steps})
	proof, err := fileProofOf(executable)
	if err != nil {
		t.Fatal(err)
	}
	request := map[string]any{
		"provider": "dsh",
		"binding": map[string]any{"provider": "dsh",
			"launch": map[string]any{"executable": executable},
			"files":  map[string]any{executable: proof}},
		"workspace":        workspace,
		"read_only":        true,
		"identity":         "",
		"model":            "",
		"reasoning_effort": "",
		"prompt":           dshTestPrompt,
		"schema":           decodeTestJSON(t, dshTestSchema),
	}
	if customize != nil {
		customize(request)
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	return &dshScenario{request: request, directory: directory, workspace: workspace, echoPath: echoPath}
}

// successSteps is the happy ACP sequence: capability handshake, session/new
// with one unconfigured option, a set_config_option readback, a completed
// turn's evidence and the close.
func successSteps() []peerStep {
	events := peerEvent("s-1", "assistant/message", map[string]any{"turn": 1, "step": 0,
		"usage": map[string]any{"inputTokens": 12, "outputTokens": 3},
		"message": map[string]any{"content": []any{
			map[string]any{"type": "text", "text": "```json\n"},
			map[string]any{"type": "tool_use", "id": "t-1"},
			map[string]any{"type": "text", "text": dshTestAnswerJSON},
			map[string]any{"type": "text", "text": "\n```"},
		}}}) + "\n" +
		peerEvent("s-1", "turn/end", map[string]any{"turn": 1, "reason": map[string]any{"kind": "completed"}}) + "\n"
	return []peerStep{
		{On: `"method":"initialize"`, Send: `{"id": "@ID@", "result": {"protocolVersion": 1, "agentCapabilities": {"sessionCapabilities": {"resume": {}, "close": {}}}}}`},
		{On: `"method":"session/new"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1", "configOptions": [{"id": "model"}, {"id": "reasoning_effort", "currentValue": "high"}]}}`},
		{On: `"method":"session/set_config_option"`, Send: `{"id": "@ID@", "result": {"configOptions": [{"id": "model", "currentValue": "test-model"}, {"id": "reasoning_effort", "currentValue": "high"}]}}`},
		{On: `"method":"session/prompt"`, Files: []peerFile{
			{Path: "native-source.json", Body: `{"sessionId": "s-1", "inputId": "in-1", "turn": 1, "transcriptPath": "/t/root.jsonl"}`},
			{Path: "native-events.jsonl", Body: events},
		}, Send: `{"id": "@ID@", "result": {"stopReason": "end_turn"}}`},
		{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
	}
}

// TestRunNativeNewSessionSuccessTurn runs the whole narrow command: canonical
// stdout line, identity.json/source.json/answer files landed canonically, the
// prompt text with the schema embedded in its canonical JSON form (registered
// bounded difference: value-equal to the reference's default-separator
// rendering), and the config inheritance applied through set_config_option
// (合同 ACP 时序全表＋必需用例 "新建成功回合" 的 Go 侧).
func TestRunNativeNewSessionSuccessTurn(t *testing.T) {
	scenario := newDSHScenario(t, successSteps(), func(request map[string]any) {
		request["model"] = "test-model"
		request["reasoning_effort"] = "high"
	})
	var errout bytes.Buffer
	code := 0
	stdout := captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errout.String())
	}
	if errout.Len() != 0 {
		t.Fatalf("stderr = %q", errout.String())
	}

	// stdout: one canonical line plus newline.
	if !strings.HasSuffix(stdout, "\n") || strings.Count(stdout, "\n") != 1 {
		t.Fatalf("stdout = %q", stdout)
	}
	value := decodeTestJSON(t, stdout)
	if provider := textOf(objectOrEmpty(value)["provider"]); provider != "dsh" {
		t.Fatalf("provider = %v", provider)
	}
	if identity := textOf(objectOrEmpty(value)["identity"]); identity != "s-1" {
		t.Fatalf("identity = %v", identity)
	}
	if turn := textOf(objectOrEmpty(value)["native_turn"]); turn != "1" {
		t.Fatalf("native_turn = %v", turn)
	}
	selection := objectOrEmpty(value)["selection"]
	if !valuesEqual(selection, map[string]any{"model": "test-model", "reasoning_effort": "high"}) {
		t.Fatalf("selection = %v", selection)
	}
	if !valuesEqual(objectOrEmpty(value)["files"], proofsOf(t, scenario.directory, nativeSourceFiles)) {
		t.Fatalf("files = %v", objectOrEmpty(value)["files"])
	}
	usage := listOf(objectOrEmpty(value)["usage_evidence"])
	if len(usage) != 1 {
		t.Fatalf("usage_evidence = %v", usage)
	}
	if event := textOf(objectOrEmpty(usage[0])["event"]); event != "assistant/message" {
		t.Fatalf("usage_evidence[0].event = %v", event)
	}

	// identity.json: written before set_config_option, so the model option is
	// still the null the host reported at session/new.
	identityRecord, err := readJSONFile(filepath.Join(scenario.directory, "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(identityRecord, map[string]any{
		"identity":  "s-1",
		"workspace": scenario.workspace,
		"selection": map[string]any{"model": nil, "reasoning_effort": "high"},
		"acknowledgement": map[string]any{"sessionId": "s-1", "configOptions": []any{
			map[string]any{"id": "model"}, map[string]any{"id": "reasoning_effort", "currentValue": "high"},
		}},
	}) {
		t.Fatalf("identity.json = %v", identityRecord)
	}
	// source.json: the receipt plus the applied selection, canonical bytes.
	sourceRecord, err := readJSONFile(filepath.Join(scenario.directory, "source.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(sourceRecord, value) {
		t.Fatalf("source.json = %v, stdout = %v", sourceRecord, value)
	}
	// answer.raw.txt keeps the original text; answer.json is the framed result.
	if body := readTestFileBytes(t, filepath.Join(scenario.directory, "answer.raw.txt")); string(body) != "```json\n"+dshTestAnswerJSON+"\n```" {
		t.Fatalf("answer.raw.txt = %q", body)
	}
	if body := readTestFileBytes(t, filepath.Join(scenario.directory, "answer.json")); string(body) != `{"check_reviews":{},"findings":[],"status":"PASS","summary":"observed"}` {
		t.Fatalf("answer.json = %s", body)
	}

	// The outbound frames: initialize, session/new, set_config_option for the
	// unconfigured model only, the prompt, and session/close.
	echo := readTestFile(t, scenario.echoPath)
	for _, want := range []string{
		`"method":"initialize"`,
		`"method":"session/new"`,
		`"method":"session/set_config_option"`,
		`"method":"session/prompt"`,
		`"method":"session/close"`,
	} {
		if !strings.Contains(echo, want) {
			t.Fatalf("echo missing %s:\n%s", want, echo)
		}
	}
	if strings.Count(echo, `"method":"session/set_config_option"`) != 1 {
		t.Fatalf("only the unconfigured option may be set:\n%s", echo)
	}
	// session/new params key set and order-insensitive value checks.
	if !strings.Contains(echo, `"mcpServers":[]`) {
		t.Fatalf("session/new must carry the empty additional-server list:\n%s", echo)
	}

	// The prompt text: the request prompt, the fixed middle line, and the
	// schema re-encoded canonically (value-equal to the reference's
	// default-separator form).
	wantPrompt := dshTestPrompt + "\nReturn ONLY one JSON object matching this result schema:\n" +
		mustCanonical(t, scenario.request["schema"])
	promptFrame := frameByMethod(t, echo, "session/prompt")
	promptParts := listOf(objectOrEmpty(promptFrame["params"])["prompt"])
	if len(promptParts) != 1 {
		t.Fatalf("prompt parts = %v", promptParts)
	}
	if got := textOf(objectOrEmpty(promptParts[0])["text"]); got != wantPrompt {
		t.Fatalf("prompt text:\n got %q\nwant %q", got, wantPrompt)
	}
}

func frameByMethod(t *testing.T, echo, method string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(echo), "\n") {
		if !strings.Contains(line, `"method":"`+method+`"`) {
			continue
		}
		frame, err := decodeNativeJSON([]byte(line))
		if err != nil {
			t.Fatalf("echo line not JSON: %v (%s)", err, line)
		}
		return objectOrEmpty(frame)
	}
	t.Fatalf("no %s frame in echo:\n%s", method, echo)
	return nil
}

// TestRunNativeProbePath pins the probe: no prompt frame, session/close before
// the report, model_calls 0, and the session-title-llm disable in profile.json.
func TestRunNativeProbePath(t *testing.T) {
	steps := []peerStep{
		{On: `"method":"initialize"`, Send: `{"id": "@ID@", "result": {"protocolVersion": 1, "agentCapabilities": {"sessionCapabilities": {"resume": {}, "close": {}}}}}`},
		{On: `"method":"session/new"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1", "configOptions": [{"id": "model", "currentValue": "probe-model"}]}}`},
		{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
	}
	scenario := newDSHScenario(t, steps, func(request map[string]any) {
		request["probe"] = true
	})
	var errout bytes.Buffer
	code := 0
	stdout := captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errout.String())
	}
	value := decodeTestJSON(t, stdout)
	if calls := textOf(objectOrEmpty(value)["model_calls"]); calls != "0" {
		t.Fatalf("model_calls = %v", calls)
	}
	selection := objectOrEmpty(value)["selection"]
	if !valuesEqual(selection, map[string]any{"model": "probe-model"}) {
		t.Fatalf("selection = %v", selection)
	}
	executable := textOf(objectOrEmpty(objectOrEmpty(scenario.request["binding"])["launch"])["executable"])
	if got := textOf(objectOrEmpty(value)["executable"]); got != resolveNativePath(executable) {
		t.Fatalf("executable = %v, want %v", got, resolveNativePath(executable))
	}
	echo := readTestFile(t, scenario.echoPath)
	if strings.Contains(echo, `"method":"session/prompt"`) {
		t.Fatalf("a probe must not send a prompt:\n%s", echo)
	}
	profile := readTestFileBytes(t, filepath.Join(scenario.directory, "profile.json"))
	if !strings.HasSuffix(string(profile), `,{"disabled":true,"id":"session-title-llm"}]`) {
		t.Fatalf("profile.json missing the probe disable: %s", profile)
	}
	// No turn evidence is expected: the probe returns before any prompt.
	if _, statErr := os.Stat(filepath.Join(scenario.directory, "source.json")); !os.IsNotExist(statErr) {
		t.Fatal("a probe must not land source.json")
	}
}

// TestDSHExecuteRefusals pins the refusal ladder of the sequence (合同错误文本
// 逐字段).
func TestDSHExecuteRefusals(t *testing.T) {
	capabilitySteps := []peerStep{
		{On: `"method":"initialize"`, Send: `{"id": "@ID@", "result": {"protocolVersion": 1, "agentCapabilities": {"sessionCapabilities": {"resume": {}}}}}`},
	}
	identitySteps := []peerStep{
		{On: `"method":"initialize"`, Send: `{"id": "@ID@", "result": {"protocolVersion": 1, "agentCapabilities": {"sessionCapabilities": {"resume": {}, "close": {}}}}}`},
		{On: `"method":"session/new"`, Send: `{"id": "@ID@", "result": {"sessionId": ""}}`},
	}
	configSteps := []peerStep{
		{On: `"method":"initialize"`, Send: `{"id": "@ID@", "result": {"protocolVersion": 1, "agentCapabilities": {"sessionCapabilities": {"resume": {}, "close": {}}}}}`},
		{On: `"method":"session/new"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1", "configOptions": [{"id": "model"}]}}`},
		// The host does not apply the requested value.
		{On: `"method":"session/set_config_option"`, Send: `{"id": "@ID@", "result": {"configOptions": [{"id": "model", "currentValue": "other-model"}]}}`},
	}
	stopSteps := func() []peerStep {
		steps := successSteps()
		for i := range steps {
			if steps[i].On == `"method":"session/prompt"` {
				steps[i].Send = `{"id": "@ID@", "result": {"stopReason": "max_tokens"}}`
			}
		}
		return steps
	}()

	cases := []struct {
		name      string
		steps     []peerStep
		customize func(request map[string]any)
		want      string
	}{
		{"capability", capabilitySteps, nil, "RuntimeBlocked: DSH_CAPABILITY: ACP v1 resume/close required"},
		{"identity missing", identitySteps, nil, "RuntimeBlocked: DSH_BINDING: missing or changed identity"},
		{"config readback", configSteps, func(request map[string]any) { request["model"] = "test-model" },
			"RuntimeBlocked: DSH_CONFIG: model selection was not applied"},
		{"stop reason", stopSteps, nil, "RuntimeBlocked: DSH_STOP: max_tokens"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scenario := newDSHScenario(t, tc.steps, tc.customize)
			var errout bytes.Buffer
			code := 0
			stdout := captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
			if code != 1 || stdout != "" {
				t.Fatalf("code = %d stdout = %q", code, stdout)
			}
			if got := nativeRecordError(t, scenario.directory); got != tc.want {
				t.Fatalf("error = %q, want %q", got, tc.want)
			}
			if errout.String() != tc.want+"\n" {
				t.Fatalf("stderr = %q", errout.String())
			}
		})
	}
}

// TestDSHExecuteResumeUnavailable pins the SessionUnavailable classification:
// the -32004 "Session not found" refusal becomes the typed exception, the
// five-key native-error.json lands, and identity.json does not.
func TestDSHExecuteResumeUnavailable(t *testing.T) {
	steps := []peerStep{
		{On: `"method":"initialize"`, Send: `{"id": "@ID@", "result": {"protocolVersion": 1, "agentCapabilities": {"sessionCapabilities": {"resume": {}, "close": {}}}}}`},
		{On: `"method":"session/resume"`, Send: `{"id": "@ID@", "error": {"code": -32004, "message": "Session not found: s-1"}}`},
	}
	scenario := newDSHScenario(t, steps, func(request map[string]any) {
		request["identity"] = "s-1"
	})
	request := decodeTestJSON(t, string(readTestFileBytes(t, filepath.Join(scenario.directory, "native-request.json"))))
	_, err := dshExecute(request, scenario.directory, false)
	unavailable, ok := err.(*sessionUnavailableError)
	if !ok {
		t.Fatalf("expected sessionUnavailableError, got %T: %v", err, err)
	}
	if unavailable.message != "DSH_SESSION_UNAVAILABLE: Session not found: s-1" ||
		textOf(unavailable.identity) != "s-1" || unavailable.method != "session/resume" ||
		textOf(unavailable.code) != "-32004" {
		t.Fatalf("unavailable = %+v", unavailable)
	}

	var errout bytes.Buffer
	code := 0
	stdout := captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	if code != 1 || stdout != "" {
		t.Fatalf("code = %d stdout = %q", code, stdout)
	}
	record, rerr := readJSONFile(filepath.Join(scenario.directory, "native-error.json"))
	if rerr != nil {
		t.Fatal(rerr)
	}
	if got := mustCanonical(t, record); got != `{"error":"SessionUnavailable: DSH_SESSION_UNAVAILABLE: Session not found: s-1","error_kind":"SESSION_UNAVAILABLE","identity":"s-1","method":"session/resume","native_code":-32004}` {
		t.Fatalf("record = %s", got)
	}
	if _, statErr := os.Stat(filepath.Join(scenario.directory, "identity.json")); !os.IsNotExist(statErr) {
		t.Fatal("a refused resume must not land identity.json")
	}
}

// TestDSHExecutePermissionDisconnectInvalidates pins the held-interaction
// boundary inside
// the sequence: the reverse session/request_permission is held pending (no
// reply frame ever), and when the host closes its output the pending request
// is invalidated before the transport error propagates - the old option ids
// stop being authorized answers.
func TestDSHExecutePermissionDisconnectInvalidates(t *testing.T) {
	steps := []peerStep{
		{On: `"method":"initialize"`, Send: `{"id": "@ID@", "result": {"protocolVersion": 1, "agentCapabilities": {"sessionCapabilities": {"resume": {}, "close": {}}}}}`},
		{On: `"method":"session/new"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`},
		{On: `"method":"session/prompt"`, Send: `{"jsonrpc": "2.0", "id": 41, "method": "session/request_permission", "params": {"sessionId": "s-1", "toolCall": {"toolCallId": "call-9"}, "options": [{"optionId": "allow-once", "name": "Allow once", "kind": "allow_once"}, {"optionId": "reject-once", "name": "Reject", "kind": "reject_once"}]}}`},
		{On: `"method":"session/prompt"`, Close: true},
	}
	scenario := newDSHScenario(t, steps, nil)
	var errout bytes.Buffer
	code := 0
	captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	if code != 1 {
		t.Fatalf("code = %d", code)
	}
	if got := nativeRecordError(t, scenario.directory); got != "RuntimeBlocked: DSH_TRANSPORT: native ACP process closed its output" {
		t.Fatalf("error = %q", got)
	}
	echo := readTestFile(t, scenario.echoPath)
	for _, line := range strings.Split(strings.TrimSpace(echo), "\n") {
		frame := decodeTestJSON(t, line)
		if _, has := objectOrEmpty(frame)["error"]; has {
			t.Fatalf("a user decision was answered:\n%s", echo)
		}
	}
	pendings, err := readJSONFile(filepath.Join(scenario.directory, "interaction-pending.json"))
	if err != nil {
		t.Fatal(err)
	}
	records := listOf(pendings)
	if len(records) != 1 || textOf(objectOrEmpty(records[0])["status"]) != "invalidated" ||
		textOf(objectOrEmpty(records[0])["request_id"]) != "41" {
		t.Fatalf("pending records = %s", mustCanonical(t, pendings))
	}
}

// TestDSHExecuteMissingPromptKey pins the unreachable missing-field error for
// a non-probe request without a prompt (workers always writes one).
func TestDSHExecuteMissingPromptKey(t *testing.T) {
	steps := []peerStep{
		{On: `"method":"initialize"`, Send: `{"id": "@ID@", "result": {"protocolVersion": 1, "agentCapabilities": {"sessionCapabilities": {"resume": {}, "close": {}}}}}`},
		{On: `"method":"session/new"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`},
		{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
	}
	scenario := newDSHScenario(t, steps, nil)
	// Rebuild the request file without prompt/schema at all.
	request := map[string]any{}
	for key, value := range scenario.request {
		if key == "prompt" || key == "schema" {
			continue
		}
		request[key] = value
	}
	if err := writeAtomicJSON(filepath.Join(scenario.directory, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	var errout bytes.Buffer
	code := 0
	captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	if code != 1 {
		t.Fatalf("code = %d", code)
	}
	if got := nativeRecordError(t, scenario.directory); got != `missing JSON field "prompt"` {
		t.Fatalf("error = %q", got)
	}
}

// Continuation keeps the explicit native session, model and current prompt.
func TestDSHNativeContinuation(t *testing.T) {
	first := newDSHScenario(t, successSteps(), func(request map[string]any) {
		request["model"], request["reasoning_effort"] = "test-model", "high"
	})
	original, err := runNativeJob(first.directory)
	if err != nil {
		t.Fatal(err)
	}
	before := readTestFileBytes(t, filepath.Join(first.directory, "source.json"))
	steps := successSteps()
	steps[1].On = `"method":"session/resume"`
	steps[1].Send = `{"id":"@ID@","result":{"sessionId":"s-1","configOptions":[{"id":"model","currentValue":"test-model"},{"id":"reasoning_effort","currentValue":"high"}]}}`
	steps = append(steps[:2], steps[3:]...)
	steps[2].Files[0].Body = `{"sessionId":"s-1","inputId":"in-2","turn":2}`
	steps[2].Files[1].Body = peerEvent("s-1", "assistant/message", map[string]any{"turn": 2, "step": 0, "usage": map[string]any{"inputTokens": 23, "outputTokens": 5}, "message": map[string]any{"content": []any{map[string]any{"type": "text", "text": `{"status":"PASS","summary":"continued exact task","findings":[],"check_reviews":{}}`}}}}) + "\n" + peerEvent("s-1", "turn/end", map[string]any{"turn": 2, "reason": map[string]any{"kind": "completed"}}) + "\n"
	continued := newDSHScenario(t, steps, func(request map[string]any) {
		request["workspace"], request["identity"], request["prompt"] = first.workspace, original["identity"], "Continue the exact second task."
		request["model"], request["reasoning_effort"] = "test-model", "high"
	})
	source, err := runNativeJob(continued.directory)
	if err != nil {
		t.Fatal(err)
	}
	if source["identity"] != original["identity"] || !valuesEqual(source["native_turn"], 2) || valuesEqual(source["usage_evidence"], original["usage_evidence"]) || !bytes.Equal(before, readTestFileBytes(t, filepath.Join(first.directory, "source.json"))) {
		t.Fatalf("continuation: %v", source)
	}
	answer, err := readJSONFile(filepath.Join(continued.directory, "answer.json"))
	if err != nil || getOf(answer, "summary") != "continued exact task" {
		t.Fatalf("continued answer: %v %v", answer, err)
	}
	echo := readTestFile(t, continued.echoPath)
	resume := frameByMethod(t, echo, "session/resume")
	if !valuesEqual(getOf(resume, "params"), map[string]any{"cwd": first.workspace, "sessionId": original["identity"], "mcpServers": []any{}}) || strings.Contains(echo, `"method":"session/new"`) || strings.Contains(echo, `"method":"session/set_config_option"`) {
		t.Fatalf("continued input: %s", echo)
	}
	if err := dshVerifySource(continued.directory, source); err != nil {
		t.Fatal(err)
	}
}
