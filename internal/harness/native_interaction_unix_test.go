//go:build !windows

package harness

// The G2d held-interaction flows over the scripted fake hosts, covering the
// contract's 测试与门 column end to end: pending→答复→同 turn 继续出结果、
// 拒绝选项、取消（按宿主协议的取消答复形状）、错误 request_id/非宿主选项
// 被拒、断连失效、双请求竞态（答 A 不误 B）、zcode 预算暂停。POSIX-only:
// the peers run behind #!/bin/sh wrappers.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// answerWhenPending blocks until one request id is pending in the job's
// evidence directory, then answers it once through the real CLI entry.
func answerWhenPending(t *testing.T, directory, requestID string, respond func(errout *bytes.Buffer) int) {
	t.Helper()
	pendingPath := filepath.Join(directory, "interaction-pending.json")
	for deadline := time.Now().Add(30 * time.Second); ; {
		if body, err := os.ReadFile(pendingPath); err == nil {
			if strings.Contains(string(body), `"request_id":"`+requestID+`"`) &&
				strings.Contains(string(body), `"status":"pending"`) {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("request %s never became pending in %s", requestID, directory)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var errout bytes.Buffer
	if code := respond(&errout); code != 0 {
		t.Fatalf("native-respond for %s exited %d: %s", requestID, code, errout.String())
	}
}

// waitStatusContent blocks until the interactions log contains the text.
func waitStatusContent(t *testing.T, directory, want string) {
	t.Helper()
	path := filepath.Join(directory, "interaction-status.json")
	for deadline := time.Now().Add(30 * time.Second); ; {
		if body, err := os.ReadFile(path); err == nil && strings.Contains(string(body), want) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s never recorded %q", path, want)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// pendingStatus reads one request id's pending record.
func pendingStatus(t *testing.T, directory, requestID string) map[string]any {
	t.Helper()
	value, err := readJSONFile(filepath.Join(directory, "interaction-pending.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range listOf(value) {
		entry := objectOrEmpty(record)
		if textOf(getOf(entry, "request_id")) == requestID {
			return entry
		}
	}
	return nil
}

// handAnswer writes an answer file the way only a bypassing hand would.
func handAnswer(t *testing.T, directory, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, "interaction-answer.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// dshPermissionSteps is the DSH sequence whose prompt turn first hits one or
// two permission requests; the continuation fires when the chosen reply frame
// arrives. The prompt request is the transport's third (id 3), so the
// continuation response names it explicitly - @ID@ would resolve to the answer
// frame that triggered the step, not the prompt request.
func dshPermissionSteps(continuationOn string, extraPermissions ...string) []peerStep {
	events := peerEvent("s-1", "assistant/message", map[string]any{"turn": 1, "step": 0,
		"message": map[string]any{"content": []any{
			map[string]any{"type": "text", "text": "```json\n"},
			map[string]any{"type": "text", "text": dshTestAnswerJSON},
			map[string]any{"type": "text", "text": "\n```"},
		}}}) + "\n" +
		peerEvent("s-1", "turn/end", map[string]any{"turn": 1, "reason": map[string]any{"kind": "completed"}}) + "\n"
	steps := []peerStep{
		{On: `"method":"initialize"`, Send: `{"id": "@ID@", "result": {"protocolVersion": 1, "agentCapabilities": {"sessionCapabilities": {"resume": {}, "close": {}}}}}`},
		{On: `"method":"session/new"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`},
		{On: `"method":"session/prompt"`, Send: `{"jsonrpc": "2.0", "id": 41, "method": "session/request_permission", "params": {"sessionId": "s-1", "toolCall": {"toolCallId": "call-9"}, "options": [{"optionId": "allow-once", "name": "Allow once", "kind": "allow_once"}, {"optionId": "reject-once", "name": "Reject", "kind": "reject_once"}]}}`},
	}
	for _, extra := range extraPermissions {
		steps = append(steps, peerStep{On: `"method":"session/prompt"`, Send: extra})
	}
	return append(steps,
		peerStep{On: continuationOn, Files: []peerFile{
			{Path: "native-source.json", Body: `{"sessionId": "s-1", "inputId": "in-1", "turn": 1, "transcriptPath": "/t/root.jsonl"}`},
			{Path: "native-events.jsonl", Body: events},
		}, Send: `{"id": 3, "result": {"stopReason": "end_turn"}}`},
		peerStep{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
	)
}

// runDSHWithAnswerer wires one DSH scenario whose answer arrives while the
// job runs; the answerer knows the evidence directory only after the scenario
// exists, so it receives a resolver.
func runDSHWithAnswerer(t *testing.T, steps []peerStep, requestID string, respond func(directory string, errout *bytes.Buffer) int) (int, string, string) {
	t.Helper()
	scenario := newDSHScenario(t, steps, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		answerWhenPending(t, scenario.directory, requestID, func(errout *bytes.Buffer) int {
			return respond(scenario.directory, errout)
		})
	}()
	var errout strings.Builder
	code := 0
	captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	<-done
	return code, errout.String(), scenario.directory
}

// TestInteractionDSHAnswerContinuesSameTurn pins the whole hold→answer→
// continue chain: the permission is held, the CLI answer is delivered under
// the original frame id in the ACP reply shape, and the SAME prompt turn
// completes and produces the committed answer evidence. The delivered
// interaction becomes observed once the host's turn traffic resumes.
func TestInteractionDSHAnswerContinuesSameTurn(t *testing.T) {
	code, errout, directory := runDSHWithAnswerer(t,
		dshPermissionSteps(`"outcome":"selected"`), "41",
		func(directory string, errout *bytes.Buffer) int {
			return NativeRespond([]string{"--job", directory, "--request", "41", "--option", "allow-once"}, errout)
		})
	if code != 0 || errout != "" {
		t.Fatalf("code = %d stderr = %q", code, errout)
	}
	record := pendingStatus(t, directory, "41")
	if record == nil || textOf(record["status"]) != "observed" {
		t.Fatalf("pending record = %v", record)
	}
	if body := readTestFile(t, filepath.Join(directory, "answer.json")); body != `{"check_ids":[],"findings":[],"status":"PASS","summary":"observed"}` {
		t.Fatalf("answer.json = %s", body)
	}
	status := readTestFile(t, filepath.Join(directory, "interaction-status.json"))
	if !strings.Contains(status, `"request_id":"41"`) || !strings.Contains(status, `"status":"observed"`) {
		t.Fatalf("interaction-status = %s", status)
	}
	// Fact one of the three: the user's decision stayed persisted in the
	// consumed answer file; facts two and three are the observed status.
	consumed := objectOrEmpty(decodeTestJSON(t,
		readTestFile(t, filepath.Join(directory, "interaction-answer.consumed.json"))))
	if textOf(getOf(consumed, "request_id")) != "41" || textOf(getOf(consumed, "option")) != "allow-once" {
		t.Fatalf("consumed answer = %s", mustCanonical(t, consumed))
	}
}

// TestInteractionDSHRejectOption pins the reject branch: the host's reject
// option answers with the selected-outcome reply naming reject-once, and the
// turn still completes (the host records the rejection itself).
func TestInteractionDSHRejectOption(t *testing.T) {
	code, _, directory := runDSHWithAnswerer(t,
		dshPermissionSteps(`"optionId":"reject-once"`), "41",
		func(directory string, errout *bytes.Buffer) int {
			return NativeRespond([]string{"--job", directory, "--request", "41", "--option", "reject-once"}, errout)
		})
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if record := pendingStatus(t, directory, "41"); record == nil || textOf(record["status"]) != "observed" {
		t.Fatalf("pending record = %v", record)
	}
}

// TestInteractionDSHCancel pins the cancel reply shape the ACP host reads
// (outcome.outcome === "cancelled"): the turn continues and completes.
func TestInteractionDSHCancel(t *testing.T) {
	code, _, directory := runDSHWithAnswerer(t,
		dshPermissionSteps(`"outcome":"cancelled"`), "41",
		func(directory string, errout *bytes.Buffer) int {
			return NativeRespond([]string{"--job", directory, "--request", "41", "--cancel"}, errout)
		})
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if record := pendingStatus(t, directory, "41"); record == nil || textOf(record["status"]) != "observed" {
		t.Fatalf("pending record = %v", record)
	}
}

// TestInteractionDSHDualRequest pins the pairing rule: with two permission
// requests pending, answering A leaves B untouched - B's frame id is never
// replied to and B is invalidated when the connection closes.
func TestInteractionDSHDualRequest(t *testing.T) {
	steps := dshPermissionSteps(`"outcome":"selected"`,
		`{"jsonrpc": "2.0", "id": 42, "method": "session/request_permission", "params": {"sessionId": "s-1", "toolCall": {"toolCallId": "call-10"}, "options": [{"optionId": "allow-once", "name": "Allow once", "kind": "allow_once"}]}}`)
	scenario := newDSHScenario(t, steps, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		answerWhenPending(t, scenario.directory, "42", func(errout *bytes.Buffer) int {
			// Both are pending by now; answer A only.
			return NativeRespond([]string{"--job", scenario.directory, "--request", "41", "--option", "allow-once"}, errout)
		})
	}()
	var errout strings.Builder
	code := 0
	captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	<-done
	if code != 0 {
		t.Fatalf("code = %d stderr = %s", code, errout.String())
	}
	if record := pendingStatus(t, scenario.directory, "41"); record == nil || textOf(record["status"]) != "observed" {
		t.Fatalf("A record = %v", record)
	}
	if record := pendingStatus(t, scenario.directory, "42"); record == nil || textOf(record["status"]) != "invalidated" {
		t.Fatalf("B must stay unanswered and become invalidated: %v", record)
	}
	echo := readTestFile(t, scenario.echoPath)
	if strings.Contains(echo, `"id":42`) {
		t.Fatalf("B was answered:\n%s", echo)
	}
	if !strings.Contains(echo, `"result":{"outcome":{"optionId":"allow-once","outcome":"selected"}}`) {
		t.Fatalf("A's reply frame missing:\n%s", echo)
	}
}

// TestInteractionDSHJobRefusesBadAnswers pins the job-side refusals: a
// hand-written answer naming an unknown request is refused (and the wait
// continues), an option the host never offered is refused, and the correct
// answer afterwards still delivers.
func TestInteractionDSHJobRefusesBadAnswers(t *testing.T) {
	steps := dshPermissionSteps(`"outcome":"selected"`)
	scenario := newDSHScenario(t, steps, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		pending := filepath.Join(scenario.directory, "interaction-pending.json")
		for deadline := time.Now().Add(30 * time.Second); ; {
			if body, err := os.ReadFile(pending); err == nil &&
				strings.Contains(string(body), `"request_id":"41"`) {
				break
			}
			if time.Now().After(deadline) {
				t.Errorf("request 41 never became pending")
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		handAnswer(t, scenario.directory, `{"request_id": "nope", "option": "allow-once", "cancel": false}`)
		waitStatusContent(t, scenario.directory, "answer-refused-no-pending-request")
		handAnswer(t, scenario.directory, `{"request_id": "41", "option": "made-up", "cancel": false}`)
		waitStatusContent(t, scenario.directory, "answer-refused-invalid-choice")
		var errout bytes.Buffer
		if code := NativeRespond([]string{"--job", scenario.directory, "--request", "41", "--option", "allow-once"}, &errout); code != 0 {
			t.Errorf("correct answer refused: %d %s", code, errout.String())
		}
	}()
	var errout strings.Builder
	code := 0
	captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	<-done
	if code != 0 {
		t.Fatalf("code = %d stderr = %s", code, errout.String())
	}
	if record := pendingStatus(t, scenario.directory, "41"); record == nil || textOf(record["status"]) != "observed" {
		t.Fatalf("pending record = %v", record)
	}
	status := readTestFile(t, filepath.Join(scenario.directory, "interaction-status.json"))
	if !strings.Contains(status, "answer-refused-no-pending-request") ||
		!strings.Contains(status, "answer-refused-invalid-choice") {
		t.Fatalf("interaction-status = %s", status)
	}
}

// zcodePermissionFrame is the reverse permission request the fake app-server
// sends; the option responses are the decision objects the host itself
// declared for them (reverse-read shape: each option carries its response).
func zcodePermissionFrame(frameID, requestID, toolName string) string {
	return `{"id": "` + frameID + `", "method": "interaction/requestPermission", "params": {"requestId": "` + requestID +
		`", "sessionId": "s-1", "turnId": "t-1", "toolCallId": "tc-1", "toolName": "` + toolName +
		`", "reason": "run a tool", "riskLevel": "medium", "input": {"command": "ls"}, "options": [` +
		`{"optionId": "allow_once", "kind": "allow_once", "name": "Allow once", "response": {"decision": "allow", "reason": "Approved once"}},` +
		`{"optionId": "deny", "kind": "deny", "name": "Deny", "response": {"decision": "deny", "reason": "Denied by user"}}]}}`
}

// zcodeUserInputFrame is the reverse structured user-input request with one
// single question whose options carry a value (the reverse-read FBi shape).
func zcodeUserInputFrame(frameID, requestID, question string) string {
	return `{"id": "` + frameID + `", "method": "interaction/requestUserInput", "params": {"requestId": "` + requestID +
		`", "sessionId": "s-1", "prompt": "Pick one", "questions": [{"question": "` + question +
		`", "header": "Choice", "options": [{"value": "yes", "label": "Yes"}, {"value": "no", "label": "No"}]}]}}`
}

// zcodeInteractionSteps is the read-only zcode turn whose send hits one held
// interaction; the continuation fires when the reply frame arrives.
func zcodeInteractionSteps(heldFrame, continuationOn string) func(string) []peerStep {
	return func(inputID string) []peerStep {
		return []peerStep{
			{Send: heldFrame},
			{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("s-1", zcodeSettings)},
			{On: `"method":"session/subscribe"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`},
			{On: `"method":"session/send"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1", "accepted": true}}`},
			{On: continuationOn, Send: zcodeEventLine("s-1", 1, "turn.started", "t-1",
				zcodeTurnPayload(inputID, ""))},
			{On: continuationOn, Send: zcodeEventLine("s-1", 2, "turn.completed", "t-1",
				zcodeTurnPayload(inputID, `, "resultType": "success", "response": `+zcodeTestResponseJSON))},
			{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
		}
	}
}

// runZcodeWithAnswerer wires one zcode scenario whose answer arrives while
// the job runs.
func runZcodeWithAnswerer(t *testing.T, steps func(string) []peerStep, requestID string, respond func(directory string, errout *bytes.Buffer) int) (int, string, *zcodeScenario) {
	t.Helper()
	scenario := newZcodeScenario(t, steps, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		answerWhenPending(t, scenario.directory, requestID, func(errout *bytes.Buffer) int {
			return respond(scenario.directory, errout)
		})
	}()
	var errout strings.Builder
	code := 0
	captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	<-done
	return code, errout.String(), scenario
}

// TestInteractionZcodePermissionAnswer pins the zcode permission reply
// contract: choosing the host's allow_once option answers with that option's
// own response object verbatim under the original frame id, and the SAME turn
// completes without any re-sent prompt.
func TestInteractionZcodePermissionAnswer(t *testing.T) {
	steps := zcodeInteractionSteps(zcodePermissionFrame("server-7", "req-1", "Bash"),
		`"decision":"allow"`)
	code, errout, scenario := runZcodeWithAnswerer(t, steps, "req-1",
		func(directory string, errout *bytes.Buffer) int {
			return NativeRespond([]string{"--job", directory, "--request", "req-1", "--option", "allow_once"}, errout)
		})
	if code != 0 || errout != "" {
		t.Fatalf("code = %d stderr = %q", code, errout)
	}
	echo := readTestFile(t, scenario.echoPath)
	wantReply := `{"id":"server-7","result":{"decision":"allow","reason":"Approved once"}}` + "\n"
	if !strings.Contains(echo, wantReply) {
		t.Fatalf("the option's own response object must be echoed verbatim:\n%s", echo)
	}
	if strings.Count(echo, `"method":"session/send"`) != 1 {
		t.Fatalf("the prompt must never be re-sent:\n%s", echo)
	}
	if record := pendingStatus(t, scenario.directory, "req-1"); record == nil || textOf(record["status"]) != "observed" {
		t.Fatalf("pending record = %v", record)
	}
	if body := readTestFile(t, filepath.Join(scenario.directory, "answer.json")); body != `{"status":"PASS","summary":"observed"}` {
		t.Fatalf("answer.json = %s", body)
	}
}

// TestInteractionZcodePermissionCancel pins the zcode permission cancel
// shape: the error reply carrying the host's own client-request-cancelled
// code (-32021), after which the same turn continues to completion.
func TestInteractionZcodePermissionCancel(t *testing.T) {
	steps := zcodeInteractionSteps(zcodePermissionFrame("server-7", "req-1", "Bash"),
		`"code":-32021`)
	code, errout, scenario := runZcodeWithAnswerer(t, steps, "req-1",
		func(directory string, errout *bytes.Buffer) int {
			return NativeRespond([]string{"--job", directory, "--request", "req-1", "--cancel"}, errout)
		})
	if code != 0 || errout != "" {
		t.Fatalf("code = %d stderr = %q", code, errout)
	}
	echo := readTestFile(t, scenario.echoPath)
	wantReply := `{"error":{"code":-32021,"message":"Client request cancelled: interaction/requestPermission"},"id":"server-7"}` + "\n"
	if !strings.Contains(echo, wantReply) {
		t.Fatalf("cancel reply missing:\n%s", echo)
	}
	if record := pendingStatus(t, scenario.directory, "req-1"); record == nil || textOf(record["status"]) != "observed" {
		t.Fatalf("pending record = %v", record)
	}
}

// TestInteractionZcodeUserInput pins the requestUserInput contract: an option
// answer is the accept action with the chosen value as the single question's
// answer content, and cancel is the protocol's first-class cancel action.
func TestInteractionZcodeUserInput(t *testing.T) {
	t.Run("option accept", func(t *testing.T) {
		steps := zcodeInteractionSteps(zcodeUserInputFrame("server-9", "ui-1", "Proceed?"),
			`"action":"accept"`)
		code, errout, scenario := runZcodeWithAnswerer(t, steps, "ui-1",
			func(directory string, errout *bytes.Buffer) int {
				return NativeRespond([]string{"--job", directory, "--request", "ui-1", "--option", "yes"}, errout)
			})
		if code != 0 || errout != "" {
			t.Fatalf("code = %d stderr = %q", code, errout)
		}
		echo := readTestFile(t, scenario.echoPath)
		wantReply := `{"id":"server-9","result":{"action":"accept","content":{"answer":"yes"}}}` + "\n"
		if !strings.Contains(echo, wantReply) {
			t.Fatalf("accept reply missing:\n%s", echo)
		}
		if record := pendingStatus(t, scenario.directory, "ui-1"); record == nil || textOf(record["status"]) != "observed" {
			t.Fatalf("pending record = %v", record)
		}
	})
	t.Run("cancel", func(t *testing.T) {
		steps := zcodeInteractionSteps(zcodeUserInputFrame("server-9", "ui-1", "Proceed?"),
			`"action":"cancel"`)
		code, errout, scenario := runZcodeWithAnswerer(t, steps, "ui-1",
			func(directory string, errout *bytes.Buffer) int {
				return NativeRespond([]string{"--job", directory, "--request", "ui-1", "--cancel"}, errout)
			})
		if code != 0 || errout != "" {
			t.Fatalf("code = %d stderr = %q", code, errout)
		}
		echo := readTestFile(t, scenario.echoPath)
		wantReply := `{"id":"server-9","result":{"action":"cancel"}}` + "\n"
		if !strings.Contains(echo, wantReply) {
			t.Fatalf("cancel reply missing:\n%s", echo)
		}
	})
}

// TestInteractionZcodeReannounceReAimsFrame pins the re-announce rule on the
// zcode host: the same logical request re-sent under a fresh frame id is one
// hold, and the reply answers the FRESHEST frame id.
func TestInteractionZcodeReannounceReAimsFrame(t *testing.T) {
	steps := func(inputID string) []peerStep {
		return []peerStep{
			{Send: zcodePermissionFrame("server-7", "req-1", "Bash")},
			{Send: zcodePermissionFrame("server-12", "req-1", "Bash")},
			{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("s-1", zcodeSettings)},
			{On: `"method":"session/subscribe"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`},
			{On: `"method":"session/send"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1", "accepted": true}}`},
			{On: `"decision":"allow"`, Send: zcodeEventLine("s-1", 1, "turn.started", "t-1",
				zcodeTurnPayload(inputID, ""))},
			{On: `"decision":"allow"`, Send: zcodeEventLine("s-1", 2, "turn.completed", "t-1",
				zcodeTurnPayload(inputID, `, "resultType": "success", "response": `+zcodeTestResponseJSON))},
			{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
		}
	}
	code, errout, scenario := runZcodeWithAnswerer(t, steps, "req-1",
		func(directory string, errout *bytes.Buffer) int {
			return NativeRespond([]string{"--job", directory, "--request", "req-1", "--option", "allow_once"}, errout)
		})
	if code != 0 || errout != "" {
		t.Fatalf("code = %d stderr = %q", code, errout)
	}
	echo := readTestFile(t, scenario.echoPath)
	if !strings.Contains(echo, `{"id":"server-12","result":{"decision":"allow","reason":"Approved once"}}`) {
		t.Fatalf("the reply must answer the freshest re-announced frame:\n%s", echo)
	}
	if strings.Contains(echo, `"id":"server-7","result"`) {
		t.Fatalf("the stale frame must not be answered:\n%s", echo)
	}
	pendings, err := readJSONFile(filepath.Join(scenario.directory, "interaction-pending.json"))
	if err != nil {
		t.Fatal(err)
	}
	if len(listOf(pendings)) != 1 {
		t.Fatalf("a re-announced request is one hold: %s", mustCanonical(t, pendings))
	}
}

// TestInteractionZcodeDualRequest pins the zcode pairing rule: answering A
// leaves B held, and B is invalidated when the transport closes.
func TestInteractionZcodeDualRequest(t *testing.T) {
	steps := func(inputID string) []peerStep {
		return []peerStep{
			{Send: zcodePermissionFrame("server-1", "a-1", "Bash")},
			{Send: zcodePermissionFrame("server-2", "b-2", "Write")},
			{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("s-1", zcodeSettings)},
			{On: `"method":"session/subscribe"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`},
			{On: `"method":"session/send"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1", "accepted": true}}`},
			{On: `"id":"server-1","result"`, Send: zcodeEventLine("s-1", 1, "turn.started", "t-1",
				zcodeTurnPayload(inputID, ""))},
			{On: `"id":"server-1","result"`, Send: zcodeEventLine("s-1", 2, "turn.completed", "t-1",
				zcodeTurnPayload(inputID, `, "resultType": "success", "response": `+zcodeTestResponseJSON))},
			{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
		}
	}
	code, errout, scenario := runZcodeWithAnswerer(t, steps, "b-2",
		func(directory string, errout *bytes.Buffer) int {
			// Both are pending by now; answer A only.
			return NativeRespond([]string{"--job", directory, "--request", "a-1", "--option", "allow_once"}, errout)
		})
	if code != 0 || errout != "" {
		t.Fatalf("code = %d stderr = %q", code, errout)
	}
	if record := pendingStatus(t, scenario.directory, "a-1"); record == nil || textOf(record["status"]) != "observed" {
		t.Fatalf("A record = %v", record)
	}
	if record := pendingStatus(t, scenario.directory, "b-2"); record == nil || textOf(record["status"]) != "invalidated" {
		t.Fatalf("B must stay unanswered and become invalidated: %v", record)
	}
	echo := readTestFile(t, scenario.echoPath)
	if strings.Contains(echo, `"id":"server-2"`) {
		t.Fatalf("B was answered:\n%s", echo)
	}
}

// TestInteractionZcodeBudgetPausesWhileHeld pins the 900s turn budget's
// remaining-budget semantics: the user's decision time does not consume it.
// With the budget shortened to 1.5s and the answer held back 3s, an unpaused
// budget would already have expired; the paused run completes instead.
func TestInteractionZcodeBudgetPausesWhileHeld(t *testing.T) {
	saved := zcodeTurnBudget
	zcodeTurnBudget = 1500 * time.Millisecond
	t.Cleanup(func() { zcodeTurnBudget = saved })
	steps := func(inputID string) []peerStep {
		return []peerStep{
			{Send: zcodePermissionFrame("server-7", "req-1", "Bash")},
			{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("s-1", zcodeSettings)},
			{On: `"method":"session/subscribe"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`},
			{On: `"method":"session/send"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1", "accepted": true}}`},
			{On: `"decision":"allow"`, Send: zcodeEventLine("s-1", 1, "turn.started", "t-1",
				zcodeTurnPayload(inputID, ""))},
			{On: `"decision":"allow"`, Send: zcodeEventLine("s-1", 2, "turn.completed", "t-1",
				zcodeTurnPayload(inputID, `, "resultType": "success", "response": `+zcodeTestResponseJSON))},
			{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
		}
	}
	scenario := newZcodeScenario(t, steps, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		answerWhenPending(t, scenario.directory, "req-1", func(errout *bytes.Buffer) int {
			// Twice the budget, spent entirely while the request is held.
			time.Sleep(3 * time.Second)
			return NativeRespond([]string{"--job", scenario.directory, "--request", "req-1", "--option", "allow_once"}, errout)
		})
	}()
	var errout strings.Builder
	code := 0
	captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	<-done
	if code != 0 {
		t.Fatalf("the paused budget must survive the held decision: %d %s", code, errout.String())
	}
	if body := readTestFile(t, filepath.Join(scenario.directory, "answer.json")); body != `{"status":"PASS","summary":"observed"}` {
		t.Fatalf("answer.json = %s", body)
	}
}
