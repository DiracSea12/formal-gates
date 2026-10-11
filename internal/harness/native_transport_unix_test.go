//go:build !windows

package harness

// Process-spawning transport tests over a scripted fake ACP peer (helper-
// process pattern: the test binary re-execs itself behind a #!/bin/sh wrapper,
// so nothing depends on a real dsh being installed). Behind a build tag after
// the supervise_unix_test.go precedent: the wrapper and the signal/process-
// group assertions are POSIX machinery.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

// peerStep is one scripted action of the fake host peer. A step with an empty
// On fires at startup; otherwise it fires (once) when a received line contains
// the On substring. In Send, the token "@ID@" (bare, unquoted) is replaced by
// the received frame's id, so one script answers whatever id the client used.
type peerStep struct {
	Params    any        `json:"params,omitempty"`
	On        string     `json:"on,omitempty"`
	Send      string     `json:"send,omitempty"`
	Garbage   string     `json:"garbage,omitempty"`
	Stderr    string     `json:"stderr,omitempty"`
	SleepSecs float64    `json:"sleep,omitempty"`
	Close     bool       `json:"close,omitempty"`
	IgnoreEOF bool       `json:"ignore_eof,omitempty"`
	Files     []peerFile `json:"files,omitempty"`
}

// peerFile is a file the peer writes into the evidence directory when its step
// fires (the fake host's native evidence).
type peerFile struct {
	Path string `json:"path"`
	Body string `json:"body"`
}

type peerScript struct {
	Args        []string          `json:"args,omitempty"`
	Environment map[string]string `json:"environment,omitempty"`
	Workspace   string            `json:"workspace,omitempty"`
	Steps       []peerStep        `json:"steps"`
	Echo        string            `json:"echo"`
}

// TestScriptedPeer is the helper-process entry: when
// FORMAL_GATES_NATIVE_PEER_SCRIPT is set the test binary becomes the fake host
// peer instead of running the suite.
func TestScriptedPeer(t *testing.T) {
	scriptPath := os.Getenv("FORMAL_GATES_NATIVE_PEER_SCRIPT")
	if scriptPath == "" {
		return
	}
	os.Exit(runScriptedPeer(scriptPath))
}

func runScriptedPeer(scriptPath string) int {
	body, err := os.ReadFile(scriptPath)
	if err != nil {
		return 2
	}
	var script peerScript
	if err := json.Unmarshal(body, &script); err != nil {
		return 2
	}
	if script.Args != nil {
		args := os.Args
		for len(args) > 0 && args[0] != "--" {
			args = args[1:]
		}
		if len(args) > 0 {
			args = args[1:]
		}
		if !reflect.DeepEqual(args, script.Args) {
			fmt.Fprintf(os.Stderr, "native argv differs: %q != %q\n", args, script.Args)
			return 91
		}
	}
	for key, expected := range script.Environment {
		if os.Getenv(key) != expected {
			fmt.Fprintf(os.Stderr, "native env %s differs\n", key)
			return 91
		}
	}
	if script.Workspace != "" {
		cwd, err := os.Getwd()
		if err != nil || resolveNativePath(cwd) != resolveNativePath(script.Workspace) {
			fmt.Fprintf(os.Stderr, "native cwd differs: %s != %s\n", cwd, script.Workspace)
			return 91
		}
	}
	var echo *os.File
	if script.Echo != "" {
		echo, _ = os.OpenFile(script.Echo, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	}
	ignoreEOF := false
	for i := range script.Steps {
		step := script.Steps[i]
		if step.On != "" {
			continue
		}
		if step.IgnoreEOF {
			ignoreEOF = true
		}
		applyPeerStep(step, "")
	}
	reader := bufio.NewReader(os.Stdin)
	for {
		line, err := reader.ReadString('\n')
		if line == "" && err != nil {
			break
		}
		if echo != nil {
			echo.WriteString(line)
		}
		for i := range script.Steps {
			step := script.Steps[i]
			if step.On == "" || !strings.Contains(line, step.On) {
				continue
			}
			if step.Params != nil {
				frame, decodeErr := decodeNativeJSON([]byte(line))
				if decodeErr != nil || !valuesEqual(getOf(frame, "params"), step.Params) {
					fmt.Fprintf(os.Stderr, "native params differ: %s != %s\n", line, textOf(step.Params))
					return 91
				}
			}
			script.Steps[i].On = "" // each step fires at most once
			if step.IgnoreEOF {
				ignoreEOF = true
			}
			applyPeerStep(step, line)
		}
		if err != nil {
			break
		}
	}
	if ignoreEOF {
		time.Sleep(60 * time.Second)
	}
	return 0
}

func applyPeerStep(step peerStep, line string) {
	if step.SleepSecs > 0 {
		time.Sleep(time.Duration(step.SleepSecs * float64(time.Second)))
	}
	if step.Stderr != "" {
		fmt.Fprint(os.Stderr, step.Stderr)
	}
	for _, file := range step.Files {
		if err := os.WriteFile(file.Path, []byte(file.Body), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "peer cannot write %s: %v\n", file.Path, err)
		}
	}
	send := step.Send
	if strings.Contains(send, "@ID@") && line != "" {
		if message, err := decodeNativeJSON([]byte(line)); err == nil {
			if idJSON, err := CanonicalJSON(getOf(message, "id")); err == nil {
				send = strings.ReplaceAll(send, `"@ID@"`, string(idJSON))
			}
		}
	}
	if step.Garbage != "" {
		os.Stdout.WriteString(step.Garbage + "\n")
	}
	if send != "" {
		os.Stdout.WriteString(send + "\n")
	}
	if step.Close {
		os.Stdout.Close()
		os.Exit(0)
	}
}

// startPeer writes the script and returns the executable (a shell wrapper that
// re-execs this test binary with only the helper test selected). The client's
// argv therefore keeps the real dsh shape: [executable, "--profile", "acp",
// "--patch", patch].
func startPeer(t *testing.T, script peerScript) (executable, echoPath string) {
	t.Helper()
	dir := t.TempDir()
	echoPath = filepath.Join(dir, "echo.txt")
	script.Echo = echoPath
	body, err := json.Marshal(script)
	if err != nil {
		t.Fatal(err)
	}
	scriptPath := filepath.Join(dir, "peer.json")
	if err := os.WriteFile(scriptPath, body, 0o644); err != nil {
		t.Fatal(err)
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("FORMAL_GATES_NATIVE_PEER_SCRIPT", scriptPath)
	t.Setenv("PEER_BIN", testBinary)
	wrapper := filepath.Join(dir, "dsh")
	wrapperBody := "#!/bin/sh\nexec \"$PEER_BIN\" -test.run='^TestScriptedPeer$' -- \"$@\"\n"
	if err := os.WriteFile(wrapper, []byte(wrapperBody), 0o755); err != nil {
		t.Fatal(err)
	}
	return wrapper, echoPath
}

// spawnClient starts a client over the scripted peer with DSH-shaped hooks
// (unless overridden) in a fresh evidence directory.
func spawnClient(t *testing.T, script peerScript, customize func(*nativeHooks)) (*nativeClient, string, string) {
	t.Helper()
	executable, echoPath := startPeer(t, script)
	directory := t.TempDir()
	workspace := t.TempDir()
	hooks := nativeHooks{
		transportPrefix: "DSH_TRANSPORT",
		requestPrefix:   "DSH_NATIVE",
		defaultTimeout:  dshStartupTimeout,
		bounded:         false,
		closedKey:       "transport_error",
		closedMessage:   "native ACP process closed its output",
		invalidMessage:  "non-JSON ACP output",
		identify:        func(c *nativeClient) any { return c.nextCounter() },
		envelope: func(value map[string]any) map[string]any {
			framed := map[string]any{"jsonrpc": "2.0"}
			for key, item := range value {
				framed[key] = item
			}
			return framed
		},
	}
	if customize != nil {
		customize(&hooks)
	}
	client, err := newNativeClient(hooks, []string{executable, "--profile", "acp", "--patch", filepath.Join(directory, "profile.json")}, workspace, directory)
	if err != nil {
		t.Fatalf("newNativeClient: %v", err)
	}
	return client, directory, echoPath
}

func readTestFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(body)
}

func blockedText(t *testing.T, err error) string {
	t.Helper()
	blocked, ok := err.(*blockedError)
	if !ok {
		if unavailable, isUnavailable := err.(*sessionUnavailableError); isUnavailable {
			return unavailable.message
		}
		t.Fatalf("expected blockedError, got %T: %v", err, err)
	}
	return blocked.message
}

// TestTransportOutgoingFrameBytes pins the outgoing byte contract: the jsonrpc
// envelope, the id counter starting at 1, the session params key set, and the
// canonical compact form.
func TestTransportOutgoingFrameBytes(t *testing.T) {
	client, _, echoPath := spawnClient(t, peerScript{Steps: []peerStep{
		{On: `"method":"session/new"`, Send: `{"id": 1, "result": {"sessionId": "s-1"}}`},
	}}, nil)
	defer client.close()
	if _, err := client.request("session/new", map[string]any{
		"cwd": "/w", "mcpServers": []any{}, "sessionId": "s"}); err != nil {
		t.Fatal(err)
	}
	want := `{"id":1,"jsonrpc":"2.0","method":"session/new","params":{"cwd":"/w","mcpServers":[],"sessionId":"s"}}` + "\n"
	if got := readTestFile(t, echoPath); got != want {
		t.Fatalf("outgoing frame:\n got %q\nwant %q", got, want)
	}
}

// TestTransportTraceIsRoutingOnly pins protocol.jsonl: {id?, method?},
// present keys only (a null id counts as present), canonical compact form,
// and never a payload. The trace is inbound-only (collect writes it; the
// outbound side never does), so our own initialize never appears.
func TestTransportTraceIsRoutingOnly(t *testing.T) {
	client, directory, _ := spawnClient(t, peerScript{Steps: []peerStep{
		{Send: `{"method": "session/event", "params": {"Authorization": "TEST_SECRET"}}`},
		{Send: `{"id": null, "method": "session/ping"}`},
		{Send: `{"id": 0, "method": "session/ping2"}`},
	}}, func(h *nativeHooks) { h.defaultTimeout = 2 * time.Second })
	defer client.close()
	// The first inbound line is a notification; the two reverse requests are
	// answered with the default refusal (recorded), none of them a response.
	_, err := client.request("initialize", map[string]any{"protocolVersion": 1})
	if err == nil || !strings.Contains(err.Error(), "DSH_NATIVE_TIMEOUT: initialize") {
		t.Fatalf("expected the request to still be pending (timeout), got %v", err)
	}
	want := "{\"method\":\"session/event\"}\n" +
		"{\"id\":null,\"method\":\"session/ping\"}\n" +
		"{\"id\":0,\"method\":\"session/ping2\"}\n"
	// The reader goroutine may lag the request under load; poll for the trace
	// instead of assuming it has landed the moment the timeout fires.
	var trace string
	for deadline := time.Now().Add(10 * time.Second); ; {
		trace = readTestFile(t, filepath.Join(directory, "protocol.jsonl"))
		if trace == want || time.Now().After(deadline) {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if trace != want {
		t.Fatalf("trace:\n got %q\nwant %q", trace, want)
	}
	if strings.Contains(trace, "TEST_SECRET") {
		t.Fatal("trace must never contain payloads")
	}
}

// TestTransportNonJSONLineClosesTransport pins the non-JSON boundary: the
// invalid-output closure is injected immediately and reading stops.
func TestTransportNonJSONLineClosesTransport(t *testing.T) {
	client, _, _ := spawnClient(t, peerScript{Steps: []peerStep{
		{On: "initialize", Garbage: "not json at all"},
	}}, nil)
	defer client.close()
	_, err := client.request("initialize", map[string]any{})
	if blockedText(t, err) != "DSH_TRANSPORT: non-JSON ACP output" {
		t.Fatalf("err = %v", err)
	}
}

// TestTransportEOFClosesOutput pins the stdout-EOF boundary text.
func TestTransportEOFClosesOutput(t *testing.T) {
	client, _, _ := spawnClient(t, peerScript{Steps: []peerStep{
		{On: "initialize", Close: true},
	}}, nil)
	defer client.close()
	_, err := client.request("initialize", map[string]any{})
	if blockedText(t, err) != "DSH_TRANSPORT: native ACP process closed its output" {
		t.Fatalf("err = %v", err)
	}
}

// TestTransportTimeoutText pins the timeout text (per-message timeout and the
// bounded total deadline share it).
func TestTransportTimeoutText(t *testing.T) {
	client, _, _ := spawnClient(t, peerScript{}, func(h *nativeHooks) {
		h.defaultTimeout = 200 * time.Millisecond
	})
	defer client.close()
	start := time.Now()
	_, err := client.request("initialize", map[string]any{})
	if blockedText(t, err) != "DSH_NATIVE_TIMEOUT: initialize" {
		t.Fatalf("err = %v", err)
	}
	if time.Since(start) > 5*time.Second {
		t.Fatalf("timeout took %v", time.Since(start))
	}
}

// TestTransportPerMessageWaitHasNoTotalDeadline pins bounded=false: continuous
// notifications keep the wait alive well past the nominal budget and a late
// response is still accepted; the same exchange under bounded=true times out
// at the total deadline. The supervisor's total-deadline discipline applies
// only to bounded transports (合同保真要点 4).
//
// The peer's startup notification is consumed before the request budget begins.
func TestTransportPerMessageWaitHasNoTotalDeadline(t *testing.T) {
	notifications := func() []peerStep {
		steps := []peerStep{{Send: `{"method": "peer/ready"}`}}
		for i := 0; i < 12; i++ {
			steps = append(steps, peerStep{On: "initialize", SleepSecs: 0.1, Send: `{"method": "session/event"}`})
		}
		steps = append(steps, peerStep{On: "initialize", Send: `{"id": 1, "result": {"ok": true}}`})
		return steps
	}
	readyClient := func(bounded bool) *nativeClient {
		var frames []any
		client, directory, echoPath := spawnClient(t, peerScript{Steps: notifications()}, func(h *nativeHooks) {
			h.defaultTimeout = 800 * time.Millisecond
			h.bounded = bounded
			h.received = func(message any) { frames = append(frames, message) }
		})
		t.Cleanup(func() {
			client.close()
			if t.Failed() {
				body, _ := json.Marshal(frames)
				t.Logf("bounded=%v received=%s sent=%s stderr=%s", bounded, body, readTestFile(t, echoPath), readTestFile(t, filepath.Join(directory, "host.stderr")))
			}
		})
		message, err := client.receive(nil)
		if err != nil || objectOrEmpty(message)["method"] != "peer/ready" {
			t.Fatalf("peer startup: %v %v", message, err)
		}
		return client
	}
	client := readyClient(false)
	result, err := client.request("initialize", map[string]any{})
	client.close()
	if err != nil {
		t.Fatalf("unbounded transport must accept the late response: %v", err)
	}
	if objectOrEmpty(result)["ok"] != true {
		t.Fatalf("result = %v", result)
	}

	client2 := readyClient(true)
	_, err = client2.request("initialize", map[string]any{})
	if blockedText(t, err) != "DSH_NATIVE_TIMEOUT: initialize" {
		t.Fatalf("bounded transport must hit the total deadline, got %v", err)
	}
}

// TestTransportUnboundedWaitsForTheHostsOwnTurn pins UNBOUNDED: a response
// arriving long after any nominal budget is still accepted.
func TestTransportUnboundedWaitsForTheHostsOwnTurn(t *testing.T) {
	client, _, _ := spawnClient(t, peerScript{Steps: []peerStep{
		{On: "session/prompt", SleepSecs: 1.0, Send: `{"id": 1, "result": {"stopReason": "end_turn"}}`},
	}}, func(h *nativeHooks) {
		h.defaultTimeout = 100 * time.Millisecond
	})
	defer client.close()
	_, err := client.request("session/prompt", map[string]any{}, nativeUnbounded)
	if err != nil {
		t.Fatalf("UNBOUNDED request failed: %v", err)
	}
}

// TestTransportReverseRequestDefaultReply pins the -32601 answer: the id
// round-trips its original value (string or number), the frame carries the
// jsonrpc envelope, and interaction-status.json records the status
// canonically.
func TestTransportReverseRequestDefaultReply(t *testing.T) {
	client, directory, echoPath := spawnClient(t, peerScript{Steps: []peerStep{
		{Send: `{"id": "r1", "method": "session/ping", "params": {}}`},
		{Send: `{"id": 7, "method": "session/ping2", "params": {}}`},
		{On: "initialize", Send: `{"id": 1, "result": {}}`},
	}}, nil)
	defer client.close()
	if _, err := client.request("initialize", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	echo := readTestFile(t, echoPath)
	wantReply1 := `{"error":{"code":-32601,"message":"Client method not supported"},"id":"r1","jsonrpc":"2.0"}` + "\n"
	wantReply2 := `{"error":{"code":-32601,"message":"Client method not supported"},"id":7,"jsonrpc":"2.0"}` + "\n"
	if !strings.Contains(echo, wantReply1) || !strings.Contains(echo, wantReply2) {
		t.Fatalf("replies missing from echo:\n%s", echo)
	}
	status := readTestFile(t, filepath.Join(directory, "interaction-status.json"))
	// interaction-status.json lands canonically (sorted keys, compact), like
	// every other atomic evidence file.
	wantStatus := `[{"method":"session/ping","status":"unsupported-method"},{"method":"session/ping2","status":"unsupported-method"}]`
	if status != wantStatus {
		t.Fatalf("interaction-status = %q, want %q", status, wantStatus)
	}
}

// TestTransportRequestPermissionIsHeld pins the reverse permission hold at the
// transport: the reverse permission request sends no frame and fails nothing,
// the pending record lands with the host's original options, and the
// interactions log gains the request's pending entry. A re-announced request
// (same logical id, fresh frame id) re-aims the held reply frame without a
// second record.
func TestTransportRequestPermissionIsHeld(t *testing.T) {
	client, directory, echoPath := spawnClient(t, peerScript{Steps: []peerStep{
		{Send: `{"jsonrpc": "2.0", "id": 3, "method": "session/request_permission", "params": {"sessionId": "s", "toolCall": {"toolCallId": "call-1"}, "options": [{"optionId": "allow-once", "name": "Allow once", "kind": "allow_once"}, {"optionId": "reject-once", "name": "Reject", "kind": "reject_once"}]}}`},
		{On: "initialize", Send: `{"id": 1, "result": {}}`},
	}}, func(h *nativeHooks) {
		h.interactions = []nativeInteractionSpec{dshPermissionInteraction}
	})
	defer client.close()
	if _, err := client.request("initialize", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	echo := readTestFile(t, echoPath)
	if strings.Contains(echo, `"id":3`) || strings.Contains(echo, "Client method not supported") {
		t.Fatalf("a held user decision must never be answered:\n%s", echo)
	}
	pendings, err := readJSONFile(filepath.Join(directory, "interaction-pending.json"))
	if err != nil {
		t.Fatal(err)
	}
	records := listOf(pendings)
	if len(records) != 1 {
		t.Fatalf("one pending record expected, got %s", mustCanonical(t, pendings))
	}
	record := objectOrEmpty(records[0])
	if textOf(record["request_id"]) != "3" || textOf(record["method"]) != "session/request_permission" ||
		textOf(record["status"]) != "pending" || textOf(record["summary"]) != "permission for tool call call-1" {
		t.Fatalf("pending record = %s", mustCanonical(t, record))
	}
	if !isIntegerLiteral(record["connection"]) {
		t.Fatalf("connection generation missing: %s", mustCanonical(t, record))
	}
	if !valuesEqual(record["options"], []any{
		map[string]any{"optionId": "allow-once", "name": "Allow once", "kind": "allow_once"},
		map[string]any{"optionId": "reject-once", "name": "Reject", "kind": "reject_once"},
	}) {
		t.Fatalf("options must be the host's originals verbatim: %s", mustCanonical(t, record["options"]))
	}
	// The toolCall being approved persists verbatim, so the displayed record
	// names the operation, not just its id.
	if !valuesEqual(record["toolCall"], map[string]any{"toolCallId": "call-1"}) {
		t.Fatalf("toolCall must persist verbatim: %s", mustCanonical(t, record))
	}
	// ACP has no re-announce and the frame id is the request identity, so
	// one request is exactly one hold (the re-announce rule is pinned on the
	// zcode host, where the requestId param is the identity).
	client.stateMu.Lock()
	holds := len(client.holdings)
	client.stateMu.Unlock()
	if holds != 1 {
		t.Fatalf("one request is one hold, got %d", holds)
	}
	status := readTestFile(t, filepath.Join(directory, "interaction-status.json"))
	if !strings.Contains(status, `"method":"session/request_permission"`) ||
		!strings.Contains(status, `"request_id":"3"`) || !strings.Contains(status, `"status":"pending"`) {
		t.Fatalf("interaction-status = %s", status)
	}
}

// TestTransportClosureKeysInAnyMessage pins the inherited closure rule: the
// shared 'closed' key is checked first, then the host's closed_key
// (transport_error), in ANY inbound message; the value must be a non-empty
// reason string.
func TestTransportClosureKeysInAnyMessage(t *testing.T) {
	cases := []struct {
		line string
		want string
	}{
		{`{"id": 1, "result": {}, "closed": "weird close"}`, "DSH_TRANSPORT: weird close"},
		{`{"id": 1, "result": {}, "transport_error": "boom"}`, "DSH_TRANSPORT: boom"},
		{`{"id": 1, "result": {}, "closed": "first", "transport_error": "second"}`, "DSH_TRANSPORT: first"},
		{`{"id": 1, "result": {}, "closed": ""}`, ""},
	}
	for _, tc := range cases {
		client, _, _ := spawnClient(t, peerScript{Steps: []peerStep{
			{On: "initialize", Send: tc.line},
		}}, nil)
		_, err := client.request("initialize", map[string]any{})
		client.close()
		if tc.want == "" {
			// An empty closure value does not close the transport: the line is
			// the response itself and the request completes.
			if err != nil {
				t.Fatalf("line %s must be a plain response, got %v", tc.line, err)
			}
			continue
		}
		if blockedText(t, err) != tc.want {
			t.Fatalf("line %s:\n got %v\nwant %s", tc.line, err, tc.want)
		}
	}
}

// TestTransportResponseMatching pins the response match rules. A live method
// (a non-empty string) makes the line a reverse request or notification
// (consumed, never matched); an inert method (absent/null/""/false/0) makes it
// a response, exactly like native_transport.request's method test. Id equality
// is value equality, so the literal 1.0 matches our int ident 1, and a wrong
// id is skipped.
func TestTransportResponseMatching(t *testing.T) {
	client, _, _ := spawnClient(t, peerScript{Steps: []peerStep{
		{On: "initialize", Send: `{"id": 1, "method": "notify", "params": {}}`},
		{On: "initialize", Send: `{"id": 2, "result": {"wrong": true}}`},
		{On: "initialize", Send: `{"id": 1.0, "result": {"right": true}}`},
	}}, nil)
	defer client.close()
	result, err := client.request("initialize", map[string]any{})
	if err != nil {
		t.Fatalf("live-method lines must be consumed and wrong ids skipped, 1.0 must match 1: %v", err)
	}
	if objectOrEmpty(result)["right"] != true {
		t.Fatalf("result = %v", result)
	}

	// An inert method value is a response in its own right: the request
	// completes with that line, and a missing result defaults to {}.
	for _, line := range []string{
		`{"id": 1, "method": null}`,
		`{"id": 1, "method": ""}`,
		`{"id": 1, "method": false}`,
		`{"id": 1, "method": 0}`,
	} {
		client, _, _ := spawnClient(t, peerScript{Steps: []peerStep{{On: "initialize", Send: line}}}, nil)
		result, err := client.request("initialize", map[string]any{})
		client.close()
		if err != nil {
			t.Fatalf("%s must complete the request: %v", line, err)
		}
		if obj, ok := result.(map[string]any); !ok || len(obj) != 0 {
			t.Fatalf("%s must default the result to {}, got %v", line, result)
		}
	}
}

// TestTransportErrorEmbedding pins the error branch: an absent or null error
// member is success (the result defaults to {}), any other error body is a
// refusal whose text embeds the body as canonical JSON.
func TestTransportErrorEmbedding(t *testing.T) {
	for _, line := range []string{
		`{"id": 1, "result": {"ok": true}}`,
		`{"id": 1, "error": null, "result": {"ok": true}}`,
	} {
		client, _, _ := spawnClient(t, peerScript{Steps: []peerStep{{On: "initialize", Send: line}}}, nil)
		result, err := client.request("initialize", map[string]any{"protocolVersion": 1})
		client.close()
		if err != nil {
			t.Fatalf("%s must be success, got %v", line, err)
		}
		if objectOrEmpty(result)["ok"] != true {
			t.Fatalf("result = %v", result)
		}
	}
	client, _, _ := spawnClient(t, peerScript{Steps: []peerStep{
		{On: "session/prompt", Send: `{"id": 1, "error": {"code": -32000, "message": "café ☕ failed", "nested": {"b": 1e2, "a": true}}}`},
	}}, nil)
	defer client.close()
	_, err := client.request("session/prompt", map[string]any{"sessionId": "s"})
	want := `DSH_NATIVE session/prompt: {"code":-32000,"message":"café ☕ failed","nested":{"a":true,"b":100.0}}`
	if blockedText(t, err) != want {
		t.Fatalf("err:\n got %v\nwant %s", err, want)
	}
}

// TestTransportProcessRecordAndStderr pins native-process.json (pid/pgid
// present, pgid the spawn group on unix, null on Windows) and host.stderr
// appending the child's stderr.
func TestTransportProcessRecordAndStderr(t *testing.T) {
	client, directory, _ := spawnClient(t, peerScript{Steps: []peerStep{
		{Stderr: "peer-started\n"},
		{On: "initialize", Send: `{"id": 1, "result": {}}`},
	}}, nil)
	defer client.close()
	if _, err := client.request("initialize", map[string]any{}); err != nil {
		t.Fatal(err)
	}
	record, err := readJSONFile(filepath.Join(directory, "native-process.json"))
	if err != nil {
		t.Fatal(err)
	}
	pid := objectOrEmpty(record)["pid"]
	if !isIntegerLiteral(pid) || textOf(pid) == "0" {
		t.Fatalf("pid = %v", pid)
	}
	pgid := objectOrEmpty(record)["pgid"]
	if runtime.GOOS == "windows" {
		if pgid != nil {
			t.Fatalf("pgid = %v, want null on Windows", pgid)
		}
	} else {
		if !isIntegerLiteral(pgid) || textOf(pgid) == "0" {
			t.Fatalf("pgid = %v", pgid)
		}
		group, groupErr := nativeProcessGroupID(client.cmd.Process.Pid)
		if groupErr != nil || fmt.Sprintf("%d", group) != textOf(pgid) {
			t.Fatalf("pgid %v is not the spawn group (%d, %v)", pgid, group, groupErr)
		}
	}
	if stderr := readTestFile(t, filepath.Join(directory, "host.stderr")); !strings.Contains(stderr, "peer-started") {
		t.Fatalf("host.stderr = %q", stderr)
	}
}

// TestTransportCloseTerminatesPeerSurvivingEOF pins the close sequence: a peer
// that ignores stdin EOF is SIGTERMed after the 8s grace window and the close
// returns (the kill escalation itself is exercised by the timeout).
func TestTransportCloseTerminatesPeerSurvivingEOF(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("signals are a POSIX concern; the windows build only compiles this suite")
	}
	client, _, _ := spawnClient(t, peerScript{Steps: []peerStep{{IgnoreEOF: true}}}, nil)
	done := make(chan struct{})
	go func() {
		client.close()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("close did not return")
	}
	// The peer must actually be gone.
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if err := client.cmd.Process.Signal(syscall.Signal(0)); err != nil {
			return // reaped
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("peer still alive after close")
}

// TestTransportSpawnFailures pins the failure texts of a host that cannot be
// spawned at all (missing executable / missing workspace), in Go style.
func TestTransportSpawnFailures(t *testing.T) {
	directory := t.TempDir()
	workspace := t.TempDir()
	absent := filepath.Join(directory, "absent-dsh")
	_, err := newNativeClient(dshHooks(), []string{absent, "--profile", "acp"}, workspace, directory)
	if err == nil || err.Error() != "cannot start "+absent+": fork/exec "+absent+": no such file or directory" {
		t.Fatalf("missing executable: %v", err)
	}
	absentWorkspace := filepath.Join(directory, "absent-workspace")
	_, err = newNativeClient(dshHooks(), []string{"/bin/echo", "hi"}, absentWorkspace, directory)
	if err == nil || err.Error() != "workspace does not exist: "+absentWorkspace {
		t.Fatalf("missing cwd: %v", err)
	}
}
