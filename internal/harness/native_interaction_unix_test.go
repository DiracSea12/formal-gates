//go:build !windows

package harness

// The held-interaction flows over the scripted fake hosts, covering the
// contract's 测试与门 column end to end: pending→答复→同 turn 继续出结果、
// 拒绝选项、取消（按宿主协议的取消答复形状）、错误 request_id/非宿主选项
// 被拒、断连失效、双请求竞态（答 A 不误 B）、zcode 预算暂停与答复后续算、
// 无关流量不伪造 observed、问题正文呈递。POSIX-only:
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
		peerStep{On: continuationOn, Semantics: map[string]any{"status": "PASS", "summary": "observed", "findings": []any{}, "check_reviews": map[string]any{}}, Files: []peerFile{
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
// interaction becomes observed once the host's own answer to that prompt
// arrives.
func TestInteractionDSHAnswerContinuesSameTurn(t *testing.T) {
	code, errout, directory := runDSHWithAnswerer(t,
		dshPermissionSteps(`"outcome":"selected"`), "41",
		func(directory string, errout *bytes.Buffer) int {
			return confirmHeldQuestion(t, directory, 1, nativeSelection{Choice: 1}, errout)
		})
	if code != 0 || errout != "" {
		t.Fatalf("code = %d stderr = %q", code, errout)
	}
	record := pendingStatus(t, directory, "41")
	if record == nil || textOf(record["status"]) != "observed" {
		t.Fatalf("pending record = %v", record)
	}
	if body := readTestFile(t, filepath.Join(directory, "answer.json")); body != `{"check_reviews":{},"findings":[],"status":"PASS","summary":"observed"}` {
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
			return confirmHeldQuestion(t, directory, 1, nativeSelection{Choice: 2}, errout)
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
			return confirmHeldQuestion(t, directory, 1, nativeSelection{Cancel: true}, errout)
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
			return confirmHeldQuestion(t, scenario.directory, 1, nativeSelection{Choice: 1}, errout)
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
		if code := confirmHeldQuestion(t, scenario.directory, 1, nativeSelection{Choice: 1}, &errout); code != 0 {
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

// TestInteractionDSHMetadataMustNotConfirm pins the metadata side of the
// attribution rule end to end: after the answer, an available_commands_update
// of the held session keeps the decision at delivered, and the tool card's own
// progress (the update naming the held tool call) confirms it before the
// prompt response returns.
func TestInteractionDSHMetadataMustNotConfirm(t *testing.T) {
	steps := dshPermissionSteps(`"outcome":"selected"`)
	extra := []peerStep{
		{On: `"outcome":"selected"`, Send: `{"jsonrpc": "2.0", "method": "session/update", "params": {"sessionId": "s-1", "update": {"sessionUpdate": "available_commands_update", "availableCommands": []}}}`},
		{On: `"outcome":"selected"`, SleepSecs: 1},
		{On: `"outcome":"selected"`, Send: `{"jsonrpc": "2.0", "method": "session/update", "params": {"sessionId": "s-1", "update": {"sessionUpdate": "tool_call", "toolCallId": "call-9", "title": "Write marker", "kind": "other", "status": "in_progress", "rawInput": {"path": "marker.txt"}}}}`},
	}
	steps = append(steps[:3:3], append(extra, steps[3:]...)...)
	code, _, directory := runDSHWithAnswerer(t, steps, "41",
		func(directory string, errout *bytes.Buffer) int {
			code := confirmHeldQuestion(t, directory, 1, nativeSelection{Choice: 1}, errout)
			// The trace records routing only, so the first session/update line
			// is the metadata one by the script's order.
			for deadline := time.Now().Add(30 * time.Second); ; {
				if body, err := os.ReadFile(filepath.Join(directory, "protocol.jsonl")); err == nil &&
					strings.Contains(string(body), `"method":"session/update"`) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("metadata update never reached the transport")
				}
				time.Sleep(20 * time.Millisecond)
			}
			time.Sleep(150 * time.Millisecond)
			if record := pendingStatus(t, directory, "41"); record == nil || textOf(record["status"]) != "delivered" {
				t.Fatalf("available_commands_update must not confirm the decision: %v", record)
			}
			// The tool card's own progress completes the fact, before the
			// prompt response returns.
			waitStatusContent(t, directory, `"status":"observed"`)
			return code
		})
	if code != 0 {
		t.Fatalf("code = %d", code)
	}
	if record := pendingStatus(t, directory, "41"); record == nil || textOf(record["status"]) != "observed" {
		t.Fatalf("pending record = %v", record)
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
// The turnId is the FBi schema's own optional member: the host names the turn
// the question belongs to, which is what attributes the hold's continuation.
func zcodeUserInputFrame(frameID, requestID, question string) string {
	return `{"id": "` + frameID + `", "method": "interaction/requestUserInput", "params": {"requestId": "` + requestID +
		`", "sessionId": "s-1", "turnId": "t-1", "prompt": "Pick one", "questions": [{"question": "` + question +
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
			{On: continuationOn, Semantics: map[string]any{"status": "PASS", "summary": "observed"}, Send: zcodeEventLine("s-1", 2, "turn.completed", "t-1",
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
			return confirmHeldQuestion(t, directory, 1, nativeSelection{Choice: 1}, errout)
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
			return confirmHeldQuestion(t, directory, 1, nativeSelection{Cancel: true}, errout)
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
	t.Run("free text", func(t *testing.T) {
		frame := zcodeUserInputFrame("server-9", "ui-1", "Explain the choice")
		frame = strings.Replace(frame, `[{"value": "yes", "label": "Yes"}, {"value": "no", "label": "No"}]`, `[]`, 1)
		steps := zcodeInteractionSteps(frame, `"action":"accept"`)
		code, errors, scenario := runZcodeWithAnswerer(t, steps, "ui-1", func(directory string, errout *bytes.Buffer) int {
			return confirmHeldQuestion(t, directory, 1, nativeSelection{Responses: []nativeQuestionResponse{{Text: "Original explanation"}}}, errout)
		})
		if code != 0 || errors != "" {
			t.Fatal(code, errors)
		}
		if !strings.Contains(readTestFile(t, scenario.echoPath), `{"id":"server-9","result":{"action":"accept","content":{"answer":"Original explanation"}}}`) {
			t.Fatal("free text did not preserve the native reply shape")
		}
	})
	t.Run("option accept", func(t *testing.T) {
		steps := zcodeInteractionSteps(zcodeUserInputFrame("server-9", "ui-1", "Proceed?"),
			`"action":"accept"`)
		code, errout, scenario := runZcodeWithAnswerer(t, steps, "ui-1",
			func(directory string, errout *bytes.Buffer) int {
				return confirmHeldQuestion(t, directory, 1, nativeSelection{Choice: 1}, errout)
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
				return confirmHeldQuestion(t, directory, 1, nativeSelection{Cancel: true}, errout)
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
			{On: `"decision":"allow"`, Semantics: map[string]any{"status": "PASS", "summary": "observed"}, Send: zcodeEventLine("s-1", 2, "turn.completed", "t-1",
				zcodeTurnPayload(inputID, `, "resultType": "success", "response": `+zcodeTestResponseJSON))},
			{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
		}
	}
	code, errout, scenario := runZcodeWithAnswerer(t, steps, "req-1",
		func(directory string, errout *bytes.Buffer) int {
			return confirmHeldQuestion(t, directory, 1, nativeSelection{Choice: 1}, errout)
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
			{On: `"id":"server-1","result"`, Semantics: map[string]any{"status": "PASS", "summary": "observed"}, Send: zcodeEventLine("s-1", 2, "turn.completed", "t-1",
				zcodeTurnPayload(inputID, `, "resultType": "success", "response": `+zcodeTestResponseJSON))},
			{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
		}
	}
	code, errout, scenario := runZcodeWithAnswerer(t, steps, "b-2",
		func(directory string, errout *bytes.Buffer) int {
			// Both are pending by now; answer A only.
			return confirmHeldQuestion(t, directory, 1, nativeSelection{Choice: 1}, errout)
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

// The same native turn waits for its explicit user decision and subsequent
// terminal event while preserving the interaction and raw answer evidence.
func TestInteractionZcodeWaitsForUserAndTerminal(t *testing.T) {
	steps := func(inputID string) []peerStep {
		return []peerStep{
			{Send: zcodePermissionFrame("server-7", "req-1", "Bash")},
			{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("s-1", zcodeSettings)},
			{On: `"method":"session/subscribe"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`},
			{On: `"method":"session/send"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1", "accepted": true}}`},
			{On: `"decision":"allow"`, SleepSecs: 0.1, Send: zcodeEventLine("s-1", 1, "turn.started", "t-1",
				zcodeTurnPayload(inputID, ""))},
			{On: `"decision":"allow"`, Semantics: map[string]any{"status": "PASS", "summary": "observed"}, Send: zcodeEventLine("s-1", 2, "turn.completed", "t-1",
				zcodeTurnPayload(inputID, `, "resultType": "success", "response": `+zcodeTestResponseJSON))},
			{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
		}
	}
	scenario := newZcodeScenario(t, steps, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		answerWhenPending(t, scenario.directory, "req-1", func(errout *bytes.Buffer) int {
			// The host resumes only after this explicit user answer.
			time.Sleep(100 * time.Millisecond)
			return confirmHeldQuestion(t, scenario.directory, 1, nativeSelection{Choice: 1}, errout)
		})
	}()
	var errout strings.Builder
	code := 0
	captureNativeStdout(t, func() { code = RunNative(scenario.directory, &errout) })
	<-done
	if code != 0 {
		t.Fatalf("the original turn must survive the held decision: %d %s", code, errout.String())
	}
	if record := pendingStatus(t, scenario.directory, "req-1"); record == nil || textOf(record["status"]) != "observed" {
		t.Fatalf("pending record = %v", record)
	}
	if body := readTestFile(t, filepath.Join(scenario.directory, "answer.json")); body != `{"status":"PASS","summary":"observed"}` {
		t.Fatalf("answer.json = %s", body)
	}
}

// TestInteractionZcodeUnrelatedTrafficMustNotConfirm pins the attribution
// rule: after the answer, host telemetry that names no session and same-session
// metadata (a title update) are consumed but never advance the decision past
// delivered; only the held request's own confirmation events do, once they
// arrive.
func TestInteractionZcodeUnrelatedTrafficMustNotConfirm(t *testing.T) {
	steps := func(inputID string) []peerStep {
		script := zcodeInteractionSteps(zcodePermissionFrame("server-1", "req-1", "Bash"),
			`"decision":"allow"`)(inputID)
		// After the reply the host first emits an MCP telemetry heartbeat and a
		// same-session title update, then holds the real turn events back for
		// two more seconds.
		unrelated := []peerStep{
			{On: `"decision":"allow"`, Send: `{"method": "process/mcpTelemetry", "params": {"event": "heartbeat"}}`},
			{On: `"decision":"allow"`, Send: zcodeEventLine("s-1", 0, "session.titleUpdated", "",
				`{"title": "Background title update"}`)},
			{On: `"decision":"allow"`, SleepSecs: 2},
		}
		return append(script[:4:4], append(unrelated, script[4:]...)...)
	}
	code, errout, scenario := runZcodeWithAnswerer(t, steps, "req-1",
		func(directory string, errout *bytes.Buffer) int {
			code := confirmHeldQuestion(t, directory, 1, nativeSelection{Choice: 1}, errout)
			// Wait until the transport has consumed both unrelated lines, then
			// stay inside the two-second window they bought: the decision must
			// still be delivered, not observed.
			events := filepath.Join(directory, "native-events.jsonl")
			for deadline := time.Now().Add(30 * time.Second); ; {
				trace, traceErr := os.ReadFile(filepath.Join(directory, "protocol.jsonl"))
				body, eventsErr := os.ReadFile(events)
				if traceErr == nil && strings.Contains(string(trace), "process/mcpTelemetry") &&
					eventsErr == nil && strings.Contains(string(body), "session.titleUpdated") {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("unrelated traffic never reached the transport: %s %s", traceErr, eventsErr)
				}
				time.Sleep(20 * time.Millisecond)
			}
			time.Sleep(500 * time.Millisecond)
			if record := pendingStatus(t, directory, "req-1"); record == nil || textOf(record["status"]) != "delivered" {
				t.Fatalf("unrelated traffic must not confirm the decision: %v", record)
			}
			// The held request's own turn events land after the window, and
			// only they complete the fact.
			waitStatusContent(t, directory, `"status":"observed"`)
			return code
		})
	if code != 0 || errout != "" {
		t.Fatalf("code = %d stderr = %q", code, errout)
	}
	if record := pendingStatus(t, scenario.directory, "req-1"); record == nil || textOf(record["status"]) != "observed" {
		t.Fatalf("pending record = %v", record)
	}
}

// TestInteractionZcodeReceiptConfirms pins the receipt path of the attribution
// rule with the real host vocabulary: after the answer, a same-session title
// update keeps the decision at delivered, and the host's own
// permission.resolved receipt (the T4 trace's shape) confirms it before any
// turn event has arrived.
func TestInteractionZcodeReceiptConfirms(t *testing.T) {
	steps := func(inputID string) []peerStep {
		return []peerStep{
			{Send: zcodePermissionFrame("server-1", "req-1", "Bash")},
			{On: `"method":"session/create"`, Send: zcodeSnapshotFrame("s-1", zcodeSettings)},
			{On: `"method":"session/subscribe"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1"}}`},
			{On: `"method":"session/send"`, Send: `{"id": "@ID@", "result": {"sessionId": "s-1", "accepted": true}}`},
			{On: `"decision":"allow"`, Send: zcodeEventLine("s-1", 0, "session.titleUpdated", "",
				`{"title": "Background title update"}`)},
			{On: `"decision":"allow"`, SleepSecs: 1},
			{On: `"decision":"allow"`, Send: zcodeEventLine("s-1", 1, "permission.resolved", "t-1",
				`{"decision": "allow", "reason": "Approved once", "requestId": "req-1", "toolCallId": "tc-1"}`)},
			{On: `"decision":"allow"`, SleepSecs: 2},
			{On: `"decision":"allow"`, Send: zcodeEventLine("s-1", 2, "turn.started", "t-1",
				zcodeTurnPayload(inputID, ""))},
			{On: `"decision":"allow"`, Semantics: map[string]any{"status": "PASS", "summary": "observed"}, Send: zcodeEventLine("s-1", 3, "turn.completed", "t-1",
				zcodeTurnPayload(inputID, `, "resultType": "success", "response": `+zcodeTestResponseJSON))},
			{On: `"method":"session/close"`, Send: `{"id": "@ID@", "result": {}}`},
		}
	}
	code, errout, _ := runZcodeWithAnswerer(t, steps, "req-1",
		func(directory string, errout *bytes.Buffer) int {
			code := confirmHeldQuestion(t, directory, 1, nativeSelection{Choice: 1}, errout)
			events := filepath.Join(directory, "native-events.jsonl")
			for deadline := time.Now().Add(30 * time.Second); ; {
				if body, err := os.ReadFile(events); err == nil &&
					strings.Contains(string(body), "session.titleUpdated") {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("title update never reached the transport: %s", readTestFile(t, events))
				}
				time.Sleep(20 * time.Millisecond)
			}
			time.Sleep(150 * time.Millisecond)
			if record := pendingStatus(t, directory, "req-1"); record == nil || textOf(record["status"]) != "delivered" {
				t.Fatalf("a title update must not confirm the decision: %v", record)
			}
			// The receipt alone - before any turn event - completes the fact.
			waitStatusContent(t, directory, `"status":"observed"`)
			return code
		})
	if code != 0 || errout != "" {
		t.Fatalf("code = %d stderr = %q", code, errout)
	}
}

// TestInteractionZcodeQuestionBodyShown pins the display contract of a held
// user-input request: the summary is the actual question text (the generic
// prompt only titles it) and the pending record carries the host's questions
// and prompt verbatim, so native-show lets the user see what they are
// answering before native-respond speaks for them.
func TestInteractionZcodeQuestionBodyShown(t *testing.T) {
	steps := zcodeInteractionSteps(zcodeUserInputFrame("server-9", "ui-1", "Which database should be used?"),
		`"action":"accept"`)
	code, errout, scenario := runZcodeWithAnswerer(t, steps, "ui-1",
		func(directory string, errout *bytes.Buffer) int {
			var out bytes.Buffer
			view := nativeQuestionView(pendingStatus(t, directory, "ui-1"), 1)
			body, err := CanonicalJSON(view)
			if err != nil {
				t.Fatal(err)
			}
			out.Write(body)
			if !strings.Contains(out.String(), "Which database should be used?") {
				t.Fatalf("the actual question must be visible: %s", out.String())
			}
			record := pendingStatus(t, directory, "ui-1")
			if record == nil {
				t.Fatal("pending record missing")
			}
			if summary := textOf(record["summary"]); summary != "Pick one: Which database should be used?" {
				t.Fatalf("summary = %q", summary)
			}
			if !valuesEqual(record["questions"], []any{map[string]any{
				"question": "Which database should be used?", "header": "Choice",
				"options": []any{
					map[string]any{"value": "yes", "label": "Yes"},
					map[string]any{"value": "no", "label": "No"},
				}}}) {
				t.Fatalf("questions must persist verbatim: %s", mustCanonical(t, record["questions"]))
			}
			if textOf(record["prompt"]) != "Pick one" {
				t.Fatalf("prompt = %v", record["prompt"])
			}
			return confirmHeldQuestion(t, directory, 1, nativeSelection{Choice: 1}, errout)
		})
	if code != 0 || errout != "" {
		t.Fatalf("code = %d stderr = %q", code, errout)
	}
	if record := pendingStatus(t, scenario.directory, "ui-1"); record == nil || textOf(record["status"]) != "observed" {
		t.Fatalf("pending record = %v", record)
	}
}
