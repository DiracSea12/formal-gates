package harness

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readCLIFile(t *testing.T, path string) string {
	t.Helper()
	b, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	return string(b)
}
func writePendingsFile(t *testing.T, directory string, records ...string) {
	t.Helper()
	list := []any{}
	for _, body := range records {
		list = append(list, decodeTestJSON(t, body))
	}
	if err := writeAtomicJSON(nativePendingPath(directory), list); err != nil {
		t.Fatal(err)
	}
}

const cliPendingPermission = `{"connection":1,"method":"session/request_permission","options":[{"optionId":"allow-once","name":"Allow once","kind":"allow_once"},{"optionId":"reject-once","name":"Reject","kind":"reject_once"}],"request_id":"41","status":"pending","summary":"permission for tool call call-9","updated_at":"2026-09-20T10:00:00Z"}`

func respondCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var errors bytes.Buffer
	code := NativeRespond(args, &errors)
	return code, errors.String()
}

func respondWithHost(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var candidate nativeAnswerCandidate
	decision := ""
	for i, arg := range args {
		if arg == "--decision-file" && i+1 < len(args) {
			decision = args[i+1]
		}
	}
	if body, err := os.ReadFile(decision); err == nil {
		if err := StrictDecode(body, &candidate); err != nil {
			t.Fatal(err)
		}
	} else {
		t.Fatal(err)
	}
	expected, err := json.MarshalIndent(candidate, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	input, client, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { input.Close(); client.Close() })
	host, output, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { host.Close(); output.Close() })
	var errors, transportErrors bytes.Buffer
	code := -1
	done := make(chan int, 1)
	go func() {
		served := ServeTools(input, output, &transportErrors, nil, func(name string, arguments json.RawMessage, confirm func(any) error, _ json.RawMessage) (any, error) {
			var identity any
			if name != "answer" || json.Unmarshal(arguments, &identity) != nil || !valuesEqual(identity, candidate.Pending["request_id"]) {
				return nil, fmt.Errorf("unexpected original answer call: %s %s", name, arguments)
			}
			code = NativeRespondWithConfirmation(args, &errors, confirm)
			return code, nil
		}, nil)
		output.Close()
		done <- served
	}()
	encoder, decoder := json.NewEncoder(client), json.NewDecoder(host)
	if err := encoder.Encode(map[string]any{"id": 1, "method": "initialize", "params": map[string]any{"protocolVersion": "2025-11-25", "capabilities": map[string]any{"elicitation": map[string]any{"form": map[string]any{}}}}}); err != nil {
		t.Fatal(err)
	}
	var initialized any
	if err := decoder.Decode(&initialized); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Encode(map[string]any{"id": 2, "method": "tools/call", "params": map[string]any{"name": "answer", "arguments": candidate.Pending["request_id"]}}); err != nil {
		t.Fatal(err)
	}
	for {
		var frame map[string]any
		if err := decoder.Decode(&frame); err != nil {
			t.Fatal(err)
		}
		if frame["method"] == nil {
			break
		}
		params := objectOrEmpty(frame["params"])
		schema := objectOrEmpty(params["requestedSchema"])
		property := objectOrEmpty(getOf(schema["properties"], "confirmOperation"))
		if frame["method"] != "elicitation/create" || frame["id"] == nil || params["mode"] != "form" || params["message"] != "Confirm this exact operation:\n"+string(expected) || !reflect.DeepEqual(schema["required"], []any{"confirmOperation"}) || property["type"] != "boolean" || property["default"] != nil {
			t.Fatal("confirmation differs from the original native answer", frame)
		}
		if err := encoder.Encode(map[string]any{"id": frame["id"], "result": map[string]any{"action": "accept", "content": map[string]any{"confirmOperation": true}}}); err != nil {
			t.Fatal(err)
		}
	}
	client.Close()
	if served := <-done; served != 0 {
		t.Fatal(served, transportErrors.String())
	}

	return code, errors.String()
}
func prepareHeldTest(t *testing.T, directory string, position int, selection nativeSelection) (nativeAnswerCandidate, error) {
	t.Helper()
	records, err := readNativePendings(directory)
	if err != nil {
		return nativeAnswerCandidate{}, err
	}
	pending := []map[string]any{}
	for _, raw := range records {
		record := objectOrEmpty(raw)
		if record["status"] == "pending" {
			pending = append(pending, record)
		}
	}
	if position < 1 || position > len(pending) {
		return nativeAnswerCandidate{}, fmt.Errorf("select a current pending question")
	}
	return prepareNativeAnswer(nativeHeldQuestion{directory, pending[position-1]}, selection)
}
func confirmHeldQuestion(t *testing.T, directory string, position int, selection nativeSelection, errout *bytes.Buffer) int {
	t.Helper()
	candidate, err := prepareHeldTest(t, directory, position, selection)
	if err != nil {
		errout.WriteString(err.Error())
		return 1
	}
	path := filepath.Join(directory, "decision.json")
	if err := writeAtomicJSON(path, candidate); err != nil {
		t.Fatal(err)
	}
	code, message := respondWithHost(t, "--decision-file", path)
	errout.WriteString(message)
	return code
}
func TestNativeRespondPreparesAndConfirmsOriginalAnswer(t *testing.T) {
	run := t.TempDir()
	job := filepath.Join(run, "jobs", "original")
	if err := os.MkdirAll(job, 0700); err != nil {
		t.Fatal(err)
	}
	writePendingsFile(t, job, cliPendingPermission)
	file := filepath.Join(run, "decision.json")
	if code, errors := respondCLI(t, "--run", run, "--question", "1", "--choice", "1", "--save-decision", file); code != 0 {
		t.Fatal(code, errors)
	}
	if _, err := os.Stat(nativeAnswerPath(job)); !os.IsNotExist(err) {
		t.Fatal("preparation delivered a vote")
	}
	if code, errors := respondCLI(t, "--decision-file", file); code != 1 || !strings.Contains(errors, "NATIVE_CONFIRMATION_UNAVAILABLE") {
		t.Fatal("CLI approved its own candidate", code, errors)
	}
	if _, err := os.Stat(nativeAnswerPath(job)); !os.IsNotExist(err) {
		t.Fatal("unconfirmed CLI delivered an answer")
	}
	if code, errors := respondWithHost(t, "--decision-file", file); code != 0 {
		t.Fatal(code, errors)
	}
	answer := objectOrEmpty(decodeTestJSON(t, readCLIFile(t, nativeAnswerPath(job))))
	if answer["request_id"] != "41" || answer["option"] != "allow-once" || answer["cancel"] != false || answer["written_at"] == nil {
		t.Fatal(answer)
	}
	if code, errors := respondWithHost(t, "--decision-file", file); code != 1 || !strings.Contains(errors, "previous answer") {
		t.Fatal("duplicate vote", code, errors)
	}
}

func TestNativeRespondHostStreamPreservesJSONReadBoundary(t *testing.T) {
	for _, size := range []int{511, 1535} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			requestID := ""
			frame, err := json.Marshal(map[string]any{"id": 2, "method": "tools/call", "params": map[string]any{"name": "answer", "arguments": requestID}})
			if err != nil {
				t.Fatal(err)
			}
			requestID = strings.Repeat("q", size-len(frame))
			frame, err = json.Marshal(map[string]any{"id": 2, "method": "tools/call", "params": map[string]any{"name": "answer", "arguments": requestID}})
			if err != nil || len(frame) != size {
				t.Fatal(len(frame), err)
			}
			job := t.TempDir()
			pending := objectOrEmpty(decodeTestJSON(t, cliPendingPermission))
			pending["request_id"] = requestID
			if err := writeAtomicJSON(nativePendingPath(job), []any{pending}); err != nil {
				t.Fatal(err)
			}
			var errors bytes.Buffer
			if code := confirmHeldQuestion(t, job, 1, nativeSelection{Choice: 1}, &errors); code != 0 {
				t.Fatal(code, errors.String())
			}
			answer := objectOrEmpty(decodeTestJSON(t, readCLIFile(t, nativeAnswerPath(job))))
			if answer["request_id"] != requestID || answer["option"] != "allow-once" || answer["cancel"] != false || answer["written_at"] == nil {
				t.Fatal(answer)
			}
		})
	}
}

func TestNativeRespondCancelAndChangedQuestion(t *testing.T) {
	job := t.TempDir()
	writePendingsFile(t, job, cliPendingPermission)
	candidate, err := prepareHeldTest(t, job, 1, nativeSelection{Cancel: true})
	if err != nil {
		t.Fatal(err)
	}
	records, _ := readNativePendings(job)
	record := objectOrEmpty(records[0])
	record["updated_at"] = "later reannouncement"
	if err := writeAtomicJSON(nativePendingPath(job), records); err != nil {
		t.Fatal(err)
	}
	if err := submitNativeAnswer(candidate); err != nil {
		t.Fatal("reannouncement lost original binding", err)
	}
	answer := objectOrEmpty(decodeTestJSON(t, readCLIFile(t, nativeAnswerPath(job))))
	if answer["cancel"] != true || answer["option"] != nil {
		t.Fatal(answer)
	}
	if err := os.Remove(nativeAnswerPath(job)); err != nil {
		t.Fatal(err)
	}
	record["connection"] = 2
	if err := writeAtomicJSON(nativePendingPath(job), records); err != nil {
		t.Fatal(err)
	}
	if err := submitNativeAnswer(candidate); err == nil {
		t.Fatal("different connection accepted")
	}
}
func TestNativeRespondSelectsOnlyDisplayedOptions(t *testing.T) {
	job := t.TempDir()
	writePendingsFile(t, job, cliPendingPermission)
	for _, selection := range []nativeSelection{{Choice: 3}, {Choice: 1, Cancel: true}, {}} {
		if _, err := prepareHeldTest(t, job, 1, selection); err == nil {
			t.Fatal(selection)
		}
	}
	if _, err := prepareHeldTest(t, job, 2, nativeSelection{Choice: 1}); err == nil {
		t.Fatal("unknown question accepted")
	}
	for _, args := range [][]string{{}, {"--request", "41"}, {"--option", "allow-once"}, {"--answers", `{"known-id":"choice"}`}, {"--job", job}, {"--decision-file", "x", "--choice", "1"}} {
		if code, _ := respondCLI(t, args...); code != 2 {
			t.Fatal(args, code)
		}
	}
}
func TestNativeShowUsesSemanticQuestionNumbers(t *testing.T) {
	run := t.TempDir()
	job := filepath.Join(run, "jobs", "original")
	if err := os.MkdirAll(job, 0700); err != nil {
		t.Fatal(err)
	}
	writePendingsFile(t, job, cliPendingPermission)
	var output, errors bytes.Buffer
	if code := NativeShow([]string{"--run", run}, &output, &errors); code != 0 {
		t.Fatal(code, &errors)
	}
	if strings.Contains(output.String(), "request_id") || strings.Contains(output.String(), "allow-once") || strings.Contains(output.String(), "call-9") || !strings.Contains(output.String(), "Allow once") {
		t.Fatal(output.String())
	}
	if _, err := os.Stat(nativeAnswerPath(job)); !os.IsNotExist(err) {
		t.Fatal("show wrote an answer")
	}
}

func TestNativeFormEnumsUseOriginalFieldAndOptionValues(t *testing.T) {
	record := map[string]any{"request_id": "original", "method": "mcpServer/elicitation/request", "mode": "form", "options": []any{map[string]any{"value": "accept"}}, "requestedSchema": map[string]any{"properties": map[string]any{"original-field": map[string]any{"type": "string", "title": "Choose a color", "enum": []any{"red", "blue"}}}}}
	answer, err := assembleNativeAnswer(record, nativeSelection{Choice: 1, Values: []any{map[string]any{"choice": 2}}})
	if err != nil || !valuesEqual(answer["answers"], map[string]any{"original-field": "blue"}) {
		t.Fatal(answer, err)
	}
	if _, err := assembleNativeAnswer(record, nativeSelection{Choice: 1, Values: []any{"blue"}}); err == nil {
		t.Fatal("raw known form value accepted")
	}
	view := nativeQuestionView(record, 1)
	field := objectOrEmpty(listOf(view["fields"])[0])
	if field["label"] != "Choose a color" || !valuesEqual(field["options"], []any{map[string]any{"choice": 1, "label": "red"}, map[string]any{"choice": 2, "label": "blue"}}) {
		t.Fatal("form choices were not displayed", view)
	}
	run := t.TempDir()
	job := filepath.Join(run, "jobs", "original")
	if err := os.MkdirAll(job, 0700); err != nil {
		t.Fatal(err)
	}
	record["status"], record["connection"] = "pending", 1
	if err := writeAtomicJSON(nativePendingPath(job), []any{record}); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(run, "decision.json")
	if code, errors := respondCLI(t, "--run", run, "--choice", "1", "--form-field", "1", "--form-choice", "2", "--save-decision", file); code != 0 {
		t.Fatal(code, errors)
	}
	if code, errors := respondWithHost(t, "--decision-file", file); code != 0 {
		t.Fatal(code, errors)
	}
	confirmed := objectOrEmpty(decodeTestJSON(t, readCLIFile(t, nativeAnswerPath(job))))
	if !valuesEqual(confirmed["answers"], map[string]any{"original-field": "blue"}) {
		t.Fatal("numeric form transport lost original value", confirmed)
	}
}

// TestNativeSerializeID pins the request-identity serialization: string ids
// verbatim (ZCode's requestIds stay CLI-friendly), other values as canonical
// JSON (DSH's integer frame ids). A number 3 and a string "3" render the same
// text; they cannot meet in one job, whose host uses one id kind only.
func TestNativeSerializeID(t *testing.T) {
	if got := nativeSerializeID("server-7"); got != "server-7" {
		t.Fatalf("string id = %q", got)
	}
	if got := nativeSerializeID(jsonNumber("3")); got != "3" {
		t.Fatalf("number id = %q", got)
	}
	if got := nativeSerializeID("3"); got != "3" {
		t.Fatalf("string 3 = %q", got)
	}
}

// TestInteractionReplyBuilders pins the adapters' reply contracts as pure
// functions: the exact payload each host protocol reads, option validation
// against the host's own option set, and the cancel shapes.
func TestInteractionReplyBuilders(t *testing.T) {
	allow := nativeAnswerChoice{OptionID: "allow-once"}
	cancel := nativeAnswerChoice{Cancel: true}

	dshParams := map[string]any{"options": []any{
		map[string]any{"optionId": "allow-once", "name": "Allow once", "kind": "allow_once"},
		map[string]any{"optionId": "reject-once", "name": "Reject", "kind": "reject_once"},
	}}
	payload, err := dshPermissionInteraction.reply(dshParams, allow)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCanonical(t, payload); got != `{"result":{"outcome":{"optionId":"allow-once","outcome":"selected"}}}` {
		t.Fatalf("dsh option reply = %s", got)
	}
	payload, err = dshPermissionInteraction.reply(dshParams, cancel)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCanonical(t, payload); got != `{"result":{"outcome":{"outcome":"cancelled"}}}` {
		t.Fatalf("dsh cancel reply = %s", got)
	}
	if _, err := dshPermissionInteraction.reply(dshParams, nativeAnswerChoice{OptionID: "allow-always"}); err == nil ||
		err.Error() != "DSH_INTERACTION: option is not one the host offered: allow-always" {
		t.Fatalf("dsh non-host option: %v", err)
	}
	if id, err := dshPermissionInteraction.requestID(map[string]any{}, jsonNumber("41")); err != nil || id != "41" {
		t.Fatalf("dsh request id: %v %v", id, err)
	}
	if _, err := dshPermissionInteraction.requestID(map[string]any{}, nil); err == nil {
		t.Fatal("a missing frame id must be refused")
	}

	zcParams := map[string]any{"options": []any{
		map[string]any{"optionId": "allow_once", "kind": "allow_once", "name": "Allow once",
			"response": map[string]any{"decision": "allow", "reason": "Approved once"}},
		map[string]any{"optionId": "deny", "kind": "deny", "name": "Deny",
			"response": map[string]any{"decision": "deny"}},
	}}
	payload, err = zcodePermissionInteraction.reply(zcParams, nativeAnswerChoice{OptionID: "allow_once"})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCanonical(t, payload); got != `{"result":{"decision":"allow","reason":"Approved once"}}` {
		t.Fatalf("zcode option reply must echo the option's own response object: %s", got)
	}
	payload, err = zcodePermissionInteraction.reply(zcParams, cancel)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCanonical(t, payload); got != `{"error":{"code":-32021,"message":"Client request cancelled: interaction/requestPermission"}}` {
		t.Fatalf("zcode cancel reply = %s", got)
	}
	if _, err := zcodePermissionInteraction.reply(zcParams, nativeAnswerChoice{OptionID: "allowOnce"}); err == nil ||
		!strings.Contains(err.Error(), "not one the host offered") {
		t.Fatalf("zcode non-host option: %v", err)
	}
	broken := map[string]any{"options": []any{map[string]any{"optionId": "x", "kind": "deny", "name": "Deny"}}}
	if _, err := zcodePermissionInteraction.reply(broken, nativeAnswerChoice{OptionID: "x"}); err == nil ||
		!strings.Contains(err.Error(), "no response object") {
		t.Fatalf("zcode option without a response: %v", err)
	}

	uiParams := map[string]any{"questions": []any{map[string]any{
		"question": "Proceed?", "header": "Choice",
		"options": []any{
			map[string]any{"value": "yes", "label": "Yes"},
			map[string]any{"value": "no", "label": "No"},
		}}}}
	payload, err = zcodeUserInputInteraction.reply(uiParams, nativeAnswerChoice{OptionID: "yes"})
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCanonical(t, payload); got != `{"result":{"action":"accept","content":{"answer":"yes"}}}` {
		t.Fatalf("zcode user-input option reply = %s", got)
	}
	payload, err = zcodeUserInputInteraction.reply(uiParams, cancel)
	if err != nil {
		t.Fatal(err)
	}
	if got := mustCanonical(t, payload); got != `{"result":{"action":"cancel"}}` {
		t.Fatalf("zcode user-input cancel reply = %s", got)
	}
	if _, err := zcodeUserInputInteraction.reply(map[string]any{}, nativeAnswerChoice{OptionID: "yes"}); err == nil ||
		!strings.Contains(err.Error(), "exactly one question") {
		t.Fatalf("zcode user-input without a single question: %v", err)
	}
	if id, err := zcodeInteractionRequestID(map[string]any{"requestId": "req-1"}, "server-7"); err != nil || id != "req-1" {
		t.Fatalf("zcode request id: %v %v", id, err)
	}
	if _, err := zcodeInteractionRequestID(map[string]any{}, "server-7"); err == nil {
		t.Fatal("a missing requestId must be refused")
	}
}

// TestInteractionUserInputPresentation pins the presented body of a held
// user-input request as pure spec functions: the summary is the question text
// (the generic prompt only titles it), a multi-question request keeps every
// question verbatim with its own options, and such a hold offers no single
// option list for a single numeric choice.
func TestInteractionUserInputPresentation(t *testing.T) {
	params := objectOrEmpty(decodeTestJSON(t, `{"requestId": "ui-2", "prompt": "Pick two", "questions": [`+
		`{"question": "First?", "header": "A", "options": [{"value": "1", "label": "One"}]},`+
		`{"question": "Second?", "header": "B", "options": [{"value": "2", "label": "Two"}]}]}`))
	if got := zcodeUserInputInteraction.summary(params); got != "Pick two: First?; Second?" {
		t.Fatalf("summary = %q", got)
	}
	if !valuesEqual(zcodeUserInputInteraction.body(params), map[string]any{
		"prompt": "Pick two", "questions": getOf(params, "questions")}) {
		t.Fatalf("body = %s", mustCanonical(t, zcodeUserInputInteraction.body(params)))
	}
	if options := listOf(zcodeUserInputInteraction.options(params)); len(options) != 0 {
		t.Fatalf("a multi-question hold answers no single option list: %s", mustCanonical(t, options))
	}
	empty := map[string]any{}
	if got := zcodeUserInputInteraction.summary(empty); got != "user input request" {
		t.Fatalf("empty summary = %q", got)
	}
}
