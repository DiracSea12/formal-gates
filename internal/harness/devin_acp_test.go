package harness

// Devin's ACP execute sequence over the scripted fake peer: the new-session
// turn through RunNative (the narrow command itself), the probe path, the
// inline host-permission answer, the catalogue subprocess, the resume
// unavailability classification and supervised cancellation. The peer
// machinery is platform-neutral; the peerExecutable launcher differs per
// platform only.

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/testkit"
	"formal-gates/internal/host"
)

const devinTestPrompt = "Review the requirement."

var devinTestSchema = `{"properties": {"findings": {"type": "array"}, "status": {"type": "string"}}, "required": ["status", "summary"], "type": "object"}`

var devinTestAnswer = map[string]any{"status": "PASS", "summary": "observed", "findings": []any{}, "check_reviews": map[string]any{}}

const devinTestAnswerJSON = `{"check_reviews":{},"findings":[],"status":"PASS","summary":"observed"}`

// devinScenario wires one execute run: a scripted peer bound as the devin
// executable (the peer itself asserts the argv is exactly ["acp"]), a
// workspace, an evidence directory, and the canonical native-request.json
// bytes on disk.
type devinScenario struct {
	request   map[string]any
	directory string
	workspace string
	echoPath  string
}

// stepsFor builds the script once the scenario's resolved workspace and
// evidence directory are known. The customize hook mutates the request map
// before it is serialised.
func newDevinScenario(t *testing.T, stepsFor func(workspace, directory string) []peerStep, customize func(request map[string]any)) *devinScenario {
	t.Helper()
	// The authenticate credential lookup stays off the developer's real
	// store: DEVIN_API_KEY is empty and every root os.UserConfigDir consults
	// points at an empty directory. Tests that exercise the step set their
	// own source.
	t.Setenv("DEVIN_API_KEY", "")
	configRoot := t.TempDir()
	t.Setenv("AppData", configRoot)
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("HOME", configRoot)
	workspace := resolveNativePath(t.TempDir())
	directory := resolveNativePath(t.TempDir())
	steps := stepsFor(workspace, directory)
	for i := range steps {
		steps[i].SemanticJob = directory
		for j := range steps[i].Files {
			steps[i].Files[j].Path = filepath.Join(directory, steps[i].Files[j].Path)
		}
	}
	script := peerScript{Args: []string{"acp"}, Workspace: workspace, Steps: steps}
	executable, echoPath := startPeer(t, script)
	proof, err := fileProofOf(executable)
	if err != nil {
		t.Fatal(err)
	}
	request := map[string]any{
		"provider": "devin",
		"binding": map[string]any{"provider": "devin",
			"launch": map[string]any{"executable": executable},
			"files":  map[string]any{executable: proof}},
		"workspace":        workspace,
		"read_only":        true,
		"identity":         "",
		"model":            "",
		"reasoning_effort": "",
		"prompt":           devinTestPrompt,
		"schema":           decodeTestJSON(t, devinTestSchema),
	}
	if customize != nil {
		customize(request)
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	return &devinScenario{request: request, directory: directory, workspace: workspace, echoPath: echoPath}
}

// devinExpectedServers is the wire shape the adapter must put in
// session/new.mcpServers: the mounted semantic-tools server under ACP's
// name/command/args/env contract.
func devinExpectedServers(t *testing.T, directory string) []any {
	t.Helper()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return []any{map[string]any{
		"name":    "formal-gates",
		"command": binary,
		"args":    []any{"harness", "native-tools", directory},
		"env":     []any{},
	}}
}

// devinSuccessSteps is the happy ACP sequence: capability handshake,
// session/new with the mounted tools server, the read-only session mode, and
// one session/update notification before the completed prompt result. The
// prompt's frame id is the client's deterministic counter: initialize=1,
// session/new=2, session/set_mode=3, session/prompt=4.
func devinSuccessSteps(t *testing.T) func(workspace, directory string) []peerStep {
	return func(workspace, directory string) []peerStep {
		return []peerStep{
			{On: `"method":"initialize"`,
				Params: map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}},
				Send:   `{"id":"@ID@","result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true,"promptCapabilities":{"image":true}}}}`},
			{On: `"method":"session/new"`,
				Params: map[string]any{"cwd": workspace, "mcpServers": devinExpectedServers(t, directory)},
				Send: `{"id":"@ID@","result":{"sessionId":"d-1","modes":{"currentModeId":"accept-edits","availableModes":[{"id":"accept-edits"},{"id":"ask"},{"id":"plan"}]},` +
					`"configOptions":[{"id":"model","currentValue":"test-model"},{"id":"reasoning_effort","currentValue":"high"}]}}`},
			{On: `"method":"session/set_mode"`,
				Params: map[string]any{"sessionId": "d-1", "modeId": "ask"},
				Send:   `{"id":"@ID@","result":{}}`},
			{On: `"method":"session/prompt"`,
				Semantics: devinTestAnswer,
				Params:    map[string]any{"sessionId": "d-1", "prompt": []any{map[string]any{"type": "text", "text": devinTestPrompt}}},
				Send: `{"method":"session/update","params":{"sessionId":"d-1","update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"working"}}}}` + "\n" +
					`{"id":4,"result":{"stopReason":"end_turn","usage":{"totalTokens":15,"inputTokens":12,"outputTokens":3}}}`},
		}
	}
}

func devinFrameByID(t *testing.T, echo string, id any) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(echo), "\n") {
		frame, err := decodeNativeJSON([]byte(line))
		if err != nil {
			continue
		}
		if valuesEqual(getOf(frame, "id"), id) {
			return objectOrEmpty(frame)
		}
	}
	t.Fatalf("no frame for id %v in echo:\n%s", id, echo)
	return nil
}

// TestDevinRunNativeSuccessTurn runs the whole narrow command: the canonical
// stdout receipt, identity/source/answer files, the events transcript the
// received hook accumulated, and the outbound frame sequence - including what
// is absent (session/close, session/resume are never sent to this host).
func TestDevinRunNativeSuccessTurn(t *testing.T) {
	scenario := newDevinScenario(t, devinSuccessSteps(t), nil)
	var errout bytes.Buffer
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
	value := decodeTestJSON(t, stdout)
	if textOf(getOf(value, "provider")) != "devin" || textOf(getOf(value, "identity")) != "d-1" {
		t.Fatalf("receipt = %v", value)
	}
	if !valuesEqual(getOf(value, "selection"), map[string]any{"model": "test-model", "reasoning_effort": "high"}) {
		t.Fatalf("selection = %v", getOf(value, "selection"))
	}
	result := objectOrEmpty(getOf(value, "native_result"))
	if textOf(result["stopReason"]) != "end_turn" {
		t.Fatalf("native_result = %v", result)
	}
	usage := listOf(getOf(value, "usage_evidence"))
	if len(usage) != 1 || !valuesEqual(getOf(usage[0], "usage"), map[string]any{"totalTokens": 15, "inputTokens": 12, "outputTokens": 3}) {
		t.Fatalf("usage_evidence = %v", usage)
	}
	if !valuesEqual(getOf(value, "files"), proofsOf(t, scenario.directory,
		append(append([]string{}, nativeSourceFiles...), nativeToolCallsFile, nativeToolContextFile))) {
		t.Fatalf("files = %v", getOf(value, "files"))
	}

	// identity.json binds the observed session to its own acknowledgement.
	identityRecord, err := readJSONFile(filepath.Join(scenario.directory, "identity.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(identityRecord, map[string]any{
		"identity":  "d-1",
		"workspace": scenario.workspace,
		"selection": map[string]any{"model": "test-model", "reasoning_effort": "high"},
		"acknowledgement": map[string]any{
			"sessionId": "d-1",
			"modes":     map[string]any{"currentModeId": "accept-edits", "availableModes": []any{map[string]any{"id": "accept-edits"}, map[string]any{"id": "ask"}, map[string]any{"id": "plan"}}},
			"configOptions": []any{
				map[string]any{"id": "model", "currentValue": "test-model"},
				map[string]any{"id": "reasoning_effort", "currentValue": "high"},
			},
		},
	}) {
		t.Fatalf("identity.json = %v", identityRecord)
	}
	// native-source.json binds the issued workspace and final selection.
	sourceRecord, err := readJSONFile(filepath.Join(scenario.directory, "native-source.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(sourceRecord, map[string]any{
		"sessionId": "d-1", "workspace": scenario.workspace,
		"selection": map[string]any{"model": "test-model", "reasoning_effort": "high"},
	}) {
		t.Fatalf("native-source.json = %v", sourceRecord)
	}
	// source.json is the stdout receipt verbatim.
	receiptRecord, err := readJSONFile(filepath.Join(scenario.directory, "source.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(receiptRecord, value) {
		t.Fatalf("source.json = %v, stdout = %v", receiptRecord, value)
	}
	if body := readTestFileBytes(t, filepath.Join(scenario.directory, "answer.json")); string(body) != devinTestAnswerJSON {
		t.Fatalf("answer.json = %s", body)
	}
	// The transcript is the raw inbound traffic: the session/update
	// notification and the terminal result frame both landed verbatim.
	events := readTestFile(t, filepath.Join(scenario.directory, "native-events.jsonl"))
	if !strings.Contains(events, `"sessionUpdate":"agent_message_chunk"`) {
		t.Fatalf("events missing the update notification:\n%s", events)
	}
	if !strings.Contains(events, `"stopReason":"end_turn"`) {
		t.Fatalf("events missing the terminal frame:\n%s", events)
	}
	// The outbound sequence: initialize, session/new (with the mounted tools
	// server), session/set_mode, session/prompt. session/close and
	// session/resume never leave this client.
	echo := readTestFile(t, scenario.echoPath)
	for _, want := range []string{
		`"method":"initialize"`,
		`"method":"session/new"`,
		`"method":"session/set_mode"`,
		`"method":"session/prompt"`,
	} {
		if !strings.Contains(echo, want) {
			t.Fatalf("echo missing %s:\n%s", want, echo)
		}
	}
	for _, absent := range []string{`"method":"session/close"`, `"method":"session/resume"`, `"method":"session/set_config_option"`} {
		if strings.Contains(echo, absent) {
			t.Fatalf("echo must not contain %s:\n%s", absent, echo)
		}
	}
	newFrame := devinFrameByID(t, echo, 2)
	if !strings.Contains(textOf(getOf(newFrame, "method")), "session/new") {
		t.Fatalf("frame 2 = %v", newFrame)
	}
	// No interaction ran: the status log is either absent or an empty list.
	if _, statErr := os.Stat(filepath.Join(scenario.directory, "interaction-status.json")); statErr == nil {
		if records, err := readJSONFile(filepath.Join(scenario.directory, "interaction-status.json")); err != nil || len(listOf(records)) != 0 {
			t.Fatalf("interaction-status must be empty: %v", records)
		}
	}
}

// The probe answers with the handshake's own report: executable, host
// capabilities and the session's advertised selection; no prompt, no mode
// change, no turn evidence.
func TestDevinRunNativeProbePath(t *testing.T) {
	stepsFor := func(workspace, directory string) []peerStep {
		return []peerStep{
			{On: `"method":"initialize"`,
				Params: map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}},
				Send:   `{"id":"@ID@","result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}`},
			{On: `"method":"session/new"`,
				Params: map[string]any{"cwd": workspace, "mcpServers": []any{}},
				Send:   `{"id":"@ID@","result":{"sessionId":"d-9","configOptions":[{"id":"model","currentValue":"probe-model"}]}}`},
		}
	}
	scenario := newDevinScenario(t, stepsFor, func(request map[string]any) {
		request["probe"] = true
	})
	var errout bytes.Buffer
	code := 0
	stdout := captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errout.String())
	}
	value := decodeTestJSON(t, stdout)
	if textOf(getOf(value, "model_calls")) != "0" || textOf(getOf(value, "provider")) != "devin" {
		t.Fatalf("report = %v", value)
	}
	if !valuesEqual(getOf(value, "selection"), map[string]any{"model": "probe-model"}) {
		t.Fatalf("selection = %v", getOf(value, "selection"))
	}
	if !valuesEqual(getOf(value, "capabilities"), map[string]any{"loadSession": true}) {
		t.Fatalf("capabilities = %v", getOf(value, "capabilities"))
	}
	executable := textOf(getOf(getOf(getOf(scenario.request, "binding"), "launch"), "executable"))
	if textOf(getOf(value, "executable")) != resolveNativePath(executable) {
		t.Fatalf("executable = %v", getOf(value, "executable"))
	}
	echo := readTestFile(t, scenario.echoPath)
	for _, absent := range []string{`"method":"session/prompt"`, `"method":"session/set_mode"`, `"method":"session/close"`} {
		if strings.Contains(echo, absent) {
			t.Fatalf("a probe must not send %s:\n%s", absent, echo)
		}
	}
	for _, name := range []string{"source.json", "native-source.json", "answer.json"} {
		if _, err := os.Stat(filepath.Join(scenario.directory, name)); !os.IsNotExist(err) {
			t.Fatalf("a probe must not land %s", name)
		}
	}
}

// A resolvable API key rides the host's own authenticate step between
// initialize and session/new: the peer asserts the exact methodId and
// meta.api_key, the echo proves the wire order, and the session opens.
func TestDevinExecuteAuthenticatesBeforeSessionNew(t *testing.T) {
	run := func(t *testing.T, key string) *devinScenario {
		t.Helper()
		stepsFor := func(workspace, directory string) []peerStep {
			steps := devinSuccessSteps(t)(workspace, directory)
			authenticate := peerStep{On: `"method":"authenticate"`,
				Params: map[string]any{"methodId": "devin-api-key", "meta": map[string]any{"api_key": key}},
				Send:   `{"id":"@ID@","result":{}}`}
			steps = append(steps[:1], append([]peerStep{authenticate}, steps[1:]...)...)
			// The authenticate frame takes counter id 2, so the prompt's
			// hardcoded response id shifts from 4 to 5.
			steps[len(steps)-1].Send = strings.Replace(steps[len(steps)-1].Send, `"id":4`, `"id":5`, 1)
			return steps
		}
		return newDevinScenario(t, stepsFor, nil)
	}
	assert := func(t *testing.T, scenario *devinScenario) {
		t.Helper()
		var errout bytes.Buffer
		code := 0
		stdout := captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
		if code != 0 {
			t.Fatalf("code = %d, stderr = %q", code, errout.String())
		}
		if textOf(getOf(decodeTestJSON(t, stdout), "identity")) != "d-1" {
			t.Fatalf("receipt = %v", stdout)
		}
		echo := readTestFile(t, scenario.echoPath)
		initializeAt := strings.Index(echo, `"method":"initialize"`)
		authenticateAt := strings.Index(echo, `"method":"authenticate"`)
		createAt := strings.Index(echo, `"method":"session/new"`)
		if initializeAt < 0 || !(initializeAt < authenticateAt && authenticateAt < createAt) {
			t.Fatalf("wire order must be initialize, authenticate, session/new:\n%s", echo)
		}
	}
	t.Run("environment key", func(t *testing.T) {
		scenario := run(t, "peer-key")
		t.Setenv("DEVIN_API_KEY", "peer-key")
		assert(t, scenario)
	})
	t.Run("credentials file key", func(t *testing.T) {
		scenario := run(t, "file-key")
		// The scenario pins os.UserConfigDir to an empty root; the file the
		// lookup reads is the fixture written there.
		configDir, err := os.UserConfigDir()
		if err != nil {
			t.Fatal(err)
		}
		credentials := filepath.Join(configDir, "devin", "credentials.toml")
		if err := os.MkdirAll(filepath.Dir(credentials), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(credentials, []byte("windsurf_api_key = \"file-key\"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		assert(t, scenario)
	})
}

// With no resolvable key the authenticate step is skipped entirely: the host
// sees initialize then session/new and its own answer carries the outcome -
// no fabricated credential and no pre-empted refusal.
func TestDevinExecuteSkipsAuthenticateWithoutKey(t *testing.T) {
	stepsFor := func(workspace, directory string) []peerStep {
		return []peerStep{
			{On: `"method":"initialize"`, Send: `{"id":"@ID@","result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}`},
			{On: `"method":"session/new"`,
				Params: map[string]any{"cwd": workspace, "mcpServers": []any{}},
				Send:   `{"id":"@ID@","result":{"sessionId":"d-9","configOptions":[{"id":"model","currentValue":"probe-model"}]}}`},
		}
	}
	scenario := newDevinScenario(t, stepsFor, func(request map[string]any) {
		request["probe"] = true
	})
	var errout bytes.Buffer
	code := 0
	stdout := captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errout.String())
	}
	if textOf(getOf(decodeTestJSON(t, stdout), "provider")) != "devin" {
		t.Fatalf("receipt = %v", stdout)
	}
	echo := readTestFile(t, scenario.echoPath)
	for _, want := range []string{`"method":"initialize"`, `"method":"session/new"`} {
		if !strings.Contains(echo, want) {
			t.Fatalf("echo missing %s:\n%s", want, echo)
		}
	}
	if strings.Contains(echo, "authenticate") {
		t.Fatalf("a keyless run must not send authenticate:\n%s", echo)
	}
}

// The credentials file is simple key = value lines: the named key's value is
// returned with its quotes stripped, whether other fields share the file or
// not, and a file without the line yields nothing.
func TestDevinCredentialReadsSimpleKeyLines(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{"only the key line", "windsurf_api_key = \"file-key\"\n", "file-key"},
		{"key line missing", "api_server_url = \"https://example\"\ndevin_api_url = \"https://api\"\n", ""},
		{"other fields coexist", "api_server_url = \"https://example\"\nwindsurf_api_key = \"file-key\"\ndevin_api_url = \"https://api\"\n", "file-key"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := devinCredential(tc.body, "windsurf_api_key"); got != tc.want {
				t.Fatalf("devinCredential = %q, want %q", got, tc.want)
			}
		})
	}
}

// DEVIN_API_KEY wins over the credentials file; the file applies only when
// the environment carries no key, and neither source yields no key.
func TestDevinAPIKeyPrefersEnvironment(t *testing.T) {
	t.Setenv("DEVIN_API_KEY", "")
	configRoot := t.TempDir()
	t.Setenv("AppData", configRoot)
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("HOME", configRoot)
	configDir, err := os.UserConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	credentials := filepath.Join(configDir, "devin", "credentials.toml")
	if err := os.MkdirAll(filepath.Dir(credentials), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(credentials, []byte("windsurf_api_key = \"file-key\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("DEVIN_API_KEY", "env-key")
	if got := devinAPIKey(); got != "env-key" {
		t.Fatalf("environment key must win: %q", got)
	}
	t.Setenv("DEVIN_API_KEY", "")
	if got := devinAPIKey(); got != "file-key" {
		t.Fatalf("credentials file key = %q", got)
	}
	if err := os.Remove(credentials); err != nil {
		t.Fatal(err)
	}
	if got := devinAPIKey(); got != "" {
		t.Fatalf("no source must yield no key: %q", got)
	}
}

// The host-permission request is answered inline with the host's own allow
// option and recorded in interaction-status.json; the turn completes. A
// request offering only reject options is cancelled the same way.
func TestDevinPermissionAnsweredInline(t *testing.T) {
	build := func(options string, wantOutcome, wantStatus string) func(*testing.T) {
		return func(t *testing.T) {
			stepsFor := func(workspace, directory string) []peerStep {
				return []peerStep{
					{On: `"method":"initialize"`,
						Send: `{"id":"@ID@","result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}`},
					{On: `"method":"session/new"`,
						Send: `{"id":"@ID@","result":{"sessionId":"d-1","modes":{"currentModeId":"accept-edits","availableModes":[{"id":"accept-edits"},{"id":"ask"}]},"configOptions":[{"id":"model","currentValue":"test-model"}]}}`},
					{On: `"method":"session/set_mode"`,
						Params: map[string]any{"sessionId": "d-1", "modeId": "ask"},
						Send:   `{"id":"@ID@","result":{}}`},
					// The reverse request arrives inside the turn; the
					// terminal result only follows a valid outcome reply.
					{On: `"method":"session/prompt"`,
						Send: `{"jsonrpc":"2.0","id":41,"method":"session/request_permission","params":{"sessionId":"d-1","toolCall":{"toolCallId":"call-9"},"options":` + options + `}}`},
					{On: `"outcome":"` + wantOutcome + `"`,
						Semantics: devinTestAnswer,
						Send:      `{"id":4,"result":{"stopReason":"end_turn","usage":{"totalTokens":5}}}`},
				}
			}
			scenario := newDevinScenario(t, stepsFor, nil)
			var errout bytes.Buffer
			code := 0
			stdout := captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
			if code != 0 {
				t.Fatalf("code = %d, stderr = %q", code, errout.String())
			}
			value := decodeTestJSON(t, stdout)
			if textOf(getOf(value, "identity")) != "d-1" {
				t.Fatalf("receipt = %v", value)
			}
			reply := devinFrameByID(t, readTestFile(t, scenario.echoPath), 41)
			outcome := objectOrEmpty(getOf(getOf(reply, "result"), "outcome"))
			if !valuesEqual(outcome["outcome"], wantOutcome) {
				t.Fatalf("reply outcome = %v, want %s", outcome, wantOutcome)
			}
			if wantOutcome == "selected" && !valuesEqual(outcome["optionId"], "allow-1") {
				t.Fatalf("reply optionId = %v", outcome)
			}
			status, err := readJSONFile(filepath.Join(scenario.directory, "interaction-status.json"))
			if err != nil {
				t.Fatal(err)
			}
			if !valuesEqual(status, []any{map[string]any{"method": "session/request_permission", "status": wantStatus}}) {
				t.Fatalf("interaction-status = %v", status)
			}
		}
	}
	t.Run("allow selected", build(
		`[{"optionId":"reject-1","name":"Reject","kind":"reject_once"},{"optionId":"allow-1","name":"Allow once","kind":"allow_once"}]`,
		"selected", "permission-selected"))
	t.Run("no allow option cancels", build(
		`[{"optionId":"reject-1","name":"Reject","kind":"reject_once"}]`,
		"cancelled", "permission-cancelled"))
}

// A carried identity is attempted through session/load; the host's own
// session_not_found (or an unimplemented load) becomes the typed unavailable
// record, never a fabricated resume.
func TestDevinExecuteResumeUnavailable(t *testing.T) {
	cases := []struct {
		name    string
		errBody string
		code    any
	}{
		{"session not found", `{"code":-32016,"message":"Session not found: d-old"}`, -32016},
		{"load unimplemented", `{"code":-32601,"message":"Method not found"}`, -32601},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stepsFor := func(workspace, directory string) []peerStep {
				return []peerStep{
					{On: `"method":"initialize"`,
						Send: `{"id":"@ID@","result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}`},
					{On: `"method":"session/load"`,
						Params: map[string]any{"cwd": workspace, "mcpServers": devinExpectedServers(t, directory), "sessionId": "d-old"},
						Send:   `{"id":"@ID@","error":` + tc.errBody + `}`},
				}
			}
			scenario := newDevinScenario(t, stepsFor, func(request map[string]any) {
				request["identity"] = "d-old"
			})
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
			if textOf(getOf(record, "error_kind")) != "SESSION_UNAVAILABLE" ||
				textOf(getOf(record, "identity")) != "d-old" ||
				textOf(getOf(record, "method")) != "session/load" ||
				!valuesEqual(getOf(record, "native_code"), tc.code) {
				t.Fatalf("record = %v", record)
			}
			if !strings.HasPrefix(textOf(getOf(record, "error")), "SessionUnavailable: DEVIN_SESSION_UNAVAILABLE: ") {
				t.Fatalf("error = %v", getOf(record, "error"))
			}
			if _, statErr := os.Stat(filepath.Join(scenario.directory, "identity.json")); !os.IsNotExist(statErr) {
				t.Fatal("a refused load must not land identity.json")
			}
		})
	}
}

// A silent host is a per-message timeout on the request in flight, not a
// guessed session state.
func TestDevinExecuteStartupTimeout(t *testing.T) {
	original := devinStartupTimeout
	devinStartupTimeout = 200 * time.Millisecond
	t.Cleanup(func() { devinStartupTimeout = original })
	stepsFor := func(workspace, directory string) []peerStep {
		return []peerStep{
			{On: `"method":"initialize"`, Send: `{"id":"@ID@","result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}`},
		}
	}
	scenario := newDevinScenario(t, stepsFor, nil)
	var errout bytes.Buffer
	code := 0
	captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	if code != 1 {
		t.Fatalf("code = %d", code)
	}
	if got := nativeRecordError(t, scenario.directory); got != "RuntimeBlocked: DEVIN_NATIVE_TIMEOUT: session/new" {
		t.Fatalf("error = %q", got)
	}
}

// The catalogue is the CLI's own models list output; a failing command is an
// honest empty catalogue whose source names the observed reason.
func TestDevinSelectionCatalogue(t *testing.T) {
	run := func(t *testing.T, script peerScript) map[string]any {
		t.Helper()
		workspace := resolveNativePath(t.TempDir())
		directory := resolveNativePath(t.TempDir())
		script.Workspace = workspace
		executable, _ := startPeer(t, script)
		proof, err := fileProofOf(executable)
		if err != nil {
			t.Fatal(err)
		}
		request := map[string]any{
			"provider":          "devin",
			"binding":           map[string]any{"provider": "devin", "launch": map[string]any{"executable": executable}, "files": map[string]any{executable: proof}},
			"workspace":         workspace,
			"read_only":         true,
			"probe":             true,
			"selection_options": true,
		}
		result, err := devinExecute(request, directory, true)
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	t.Run("models reported", func(t *testing.T) {
		catalogue := `{"families":[` +
			`{"family_label":"SWE","models":[{"model_uid":"swe-2-high","label":"SWE 2 High"},{"model_uid":"swe-2-medium","label":"SWE 2 Medium"}]},` +
			`{"family_label":"Adaptive","models":[{"model_uid":"adaptive","label":"Adaptive","variants":[{"model_uid":"adaptive","label":"Adaptive"}]}]}` +
			`]}`
		result := run(t, peerScript{
			Args:  []string{"models", "list", "--format", "json"},
			Steps: []peerStep{{Send: catalogue, Close: true}},
		})
		options, ok := result["selection_options"].(host.SelectionCatalogue)
		if !ok {
			t.Fatalf("selection_options = %T", result["selection_options"])
		}
		if len(options.Models) != 3 {
			t.Fatalf("catalogue models = %v", options.Models)
		}
		if options.Models[0].Value != "swe-2-high" || options.Models[0].Label != "SWE 2 High" {
			t.Fatalf("model[0] = %+v", options.Models[0])
		}
		if options.Source != "devin models list --format json" {
			t.Fatalf("source = %v", options.Source)
		}
	})
	t.Run("command fails", func(t *testing.T) {
		result := run(t, peerScript{
			Args:  []string{"models", "list", "--format", "json"},
			Steps: []peerStep{{Stderr: "authentication required\n", ExitCode: 1}},
		})
		options, _ := result["selection_options"].(host.SelectionCatalogue)
		if len(options.Models) != 0 {
			t.Fatalf("a failed list must report no models: %+v", options)
		}
		if !strings.Contains(options.Source, "authentication required") {
			t.Fatalf("source must name the observed reason: %v", options.Source)
		}
	})
	t.Run("unparseable output", func(t *testing.T) {
		result := run(t, peerScript{
			Args:  []string{"models", "list", "--format", "json"},
			Steps: []peerStep{{Send: "not json", Close: true}},
		})
		options, _ := result["selection_options"].(host.SelectionCatalogue)
		if len(options.Models) != 0 || !strings.Contains(options.Source, "unparseable output") {
			t.Fatalf("catalogue = %+v", options)
		}
	})
}

// The refusal ladder: protocol version, identity binding, config readback, an
// unadvertised mode and a non-terminal stop reason each fail with the host's
// own evidence, never a fabricated result.
func TestDevinExecuteRefusals(t *testing.T) {
	if testkit.RunNativePackageTest(t) {
		return
	}
	capabilitySteps := func(workspace, directory string) []peerStep {
		return []peerStep{
			{On: `"method":"initialize"`, Send: `{"id":"@ID@","result":{"protocolVersion":2,"agentCapabilities":{}}}`},
		}
	}
	identitySteps := func(workspace, directory string) []peerStep {
		return []peerStep{
			{On: `"method":"initialize"`, Send: `{"id":"@ID@","result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}`},
			{On: `"method":"session/new"`, Send: `{"id":"@ID@","result":{"sessionId":""}}`},
		}
	}
	configSteps := func(workspace, directory string) []peerStep {
		return []peerStep{
			{On: `"method":"initialize"`, Send: `{"id":"@ID@","result":{"protocolVersion":1,"agentCapabilities":{"loadSession":true}}}`},
			{On: `"method":"session/new"`, Send: `{"id":"@ID@","result":{"sessionId":"d-1","configOptions":[{"id":"model","currentValue":"other-model"}]}}`},
			// The host does not apply the requested value.
			{On: `"method":"session/set_config_option"`, Send: `{"id":"@ID@","result":{"configOptions":[{"id":"model","currentValue":"other-model"}]}}`},
		}
	}
	modeSteps := func(workspace, directory string) []peerStep {
		steps := devinSuccessSteps(t)(workspace, directory)
		// The host advertises no "ask" mode at all.
		steps[1].Send = `{"id":"@ID@","result":{"sessionId":"d-1","modes":{"currentModeId":"accept-edits","availableModes":[{"id":"accept-edits"}]},"configOptions":[{"id":"model","currentValue":"test-model"},{"id":"reasoning_effort","currentValue":"high"}]}}`
		return steps[:3]
	}
	stopSteps := func(workspace, directory string) []peerStep {
		steps := devinSuccessSteps(t)(workspace, directory)
		for i := range steps {
			if steps[i].On == `"method":"session/prompt"` {
				steps[i].Send = `{"id":4,"result":{"stopReason":"max_tokens"}}`
			}
		}
		return steps
	}
	cases := []struct {
		name      string
		stepsFor  func(workspace, directory string) []peerStep
		customize func(request map[string]any)
		want      string
	}{
		{"capability", capabilitySteps, nil, "RuntimeBlocked: DEVIN_CAPABILITY: ACP protocol version 1 required"},
		{"identity missing", identitySteps, nil, "RuntimeBlocked: DEVIN_BINDING: missing or changed identity"},
		{"config readback", configSteps, func(request map[string]any) { request["model"] = "test-model" },
			"RuntimeBlocked: DEVIN_CONFIG: model selection was not applied"},
		{"mode not offered", modeSteps, nil, "RuntimeBlocked: DEVIN_CONFIG: host does not offer session mode ask"},
		{"stop reason", stopSteps, nil, "RuntimeBlocked: DEVIN_STOP: max_tokens"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			scenario := newDevinScenario(t, tc.stepsFor, tc.customize)
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

// A completed run's evidence reconciles without replay: source.json is
// rebuilt from the original session files alone.
func TestDevinReconcileObservesTheOriginalTurn(t *testing.T) {
	scenario := newDevinScenario(t, devinSuccessSteps(t), nil)
	var errout bytes.Buffer
	code := 0
	stdout := captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	if code != 0 {
		t.Fatalf("code = %d, stderr = %q", code, errout.String())
	}
	original := decodeTestJSON(t, stdout)
	if err := os.Remove(filepath.Join(scenario.directory, "source.json")); err != nil {
		t.Fatal(err)
	}
	if err := writeAtomicJSON(filepath.Join(scenario.directory, "exit.json"), map[string]any{"exit_code": 0}); err != nil {
		t.Fatal(err)
	}
	recovered, err := NativeJobSource("devin", scenario.directory)
	if err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(recovered, original) {
		t.Fatalf("recovered = %v\noriginal = %v", recovered, original)
	}
}

// A hung turn dies with the supervisor's cancellation: the receipt is the
// cancelled one, the peer process is gone, and the request file was never
// rewritten.
func TestDevinSupervisedCancellation(t *testing.T) {
	binary := supervisorBinary(t)
	pidFile := ""
	stepsFor := func(workspace, directory string) []peerStep {
		steps := devinSuccessSteps(t)(workspace, directory)
		pidFile = filepath.Join(directory, "peer.pid")
		for i := range steps {
			switch steps[i].On {
			case `"method":"session/new"`:
				// harness native runs in the built binary, so its tools-server
				// command is that binary, not this test image; the argv the
				// peer asserts still proves the wire.
				steps[i].Params = nil
			case `"method":"session/prompt"`:
				steps[i] = peerStep{On: `"method":"session/prompt"`, IgnoreEOF: true}
			}
		}
		return steps
	}
	scenario := newDevinScenario(t, stepsFor, nil)
	// The peer writes its own pid so the leaf can be asserted dead, not just
	// the .cmd wrapper the transport started.
	scriptPath := filepath.Join(filepath.Dir(scenario.echoPath), "peer.json")
	var script peerScript
	if err := json.Unmarshal(readTestFileBytes(t, scriptPath), &script); err != nil {
		t.Fatal(err)
	}
	script.PeerPID = pidFile
	if err := writeAtomicJSON(scriptPath, script); err != nil {
		t.Fatal(err)
	}
	writeRequest(t, scenario.directory, map[string]any{
		"kind": "native", "argv": []string{binary, "harness", "native", scenario.directory},
		"cwd": scenario.workspace, "stdin": "",
	})
	command := exec.Command(binary, "harness", "supervise", scenario.directory)
	command.SysProcAttr = supervisorSysProcAttr()
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	waitForFile(t, filepath.Join(scenario.directory, "identity.json"))
	if err := terminateJob(command, command.Process.Pid); err != nil {
		t.Fatal(err)
	}
	if err := command.Wait(); err != nil {
		t.Fatal(err)
	}
	receipt := readReceipt(t, scenario.directory)
	if !valuesEqual(receipt["exit_code"], 130) || receipt["error"] != "owned job was cancelled" {
		t.Fatalf("cancel receipt: %v", receipt)
	}
	if _, err := settleNativeReceipt(scenario.directory, receipt); err == nil {
		t.Fatal("cancelled native output admitted")
	}
	process, err := readJSONFile(filepath.Join(scenario.directory, "native-process.json"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(textOf(getOf(process, "pid")))
	if err != nil {
		t.Fatal(err)
	}
	assertPeerDead(t, pid)
	waitForFile(t, pidFile)
	peerPID, err := strconv.Atoi(strings.TrimSpace(readTestFile(t, pidFile)))
	if err != nil {
		t.Fatal(err)
	}
	assertPeerDead(t, peerPID)
	if got, err := readJSONFile(filepath.Join(scenario.directory, "native-request.json")); err != nil || !valuesEqual(got, scenario.request) {
		t.Fatalf("cancel rewrote request: %v %v", got, err)
	}
}
