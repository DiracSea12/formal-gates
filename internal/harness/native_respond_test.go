package harness

// The G2d interaction CLI: native-respond's validation ladder (pending 现存、
// 选项合法、一票防重) and native-show's read-only listing. Pure filesystem:
// no host process is involved.

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readCLIFile reads one file the CLI tests inspect (readTestFile is behind
// the unix build tag).
func readCLIFile(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

// writePendingsFile lands one interaction-pending.json for the CLI tests.
func writePendingsFile(t *testing.T, directory string, records ...string) {
	t.Helper()
	list := []any{}
	for _, body := range records {
		list = append(list, decodeTestJSON(t, body))
	}
	if err := writeAtomicJSON(filepath.Join(directory, "interaction-pending.json"), list); err != nil {
		t.Fatal(err)
	}
}

const cliPendingPermission = `{"connection": 1, "method": "session/request_permission", "options": [` +
	`{"optionId": "allow-once", "name": "Allow once", "kind": "allow_once"},` +
	`{"optionId": "reject-once", "name": "Reject", "kind": "reject_once"}],` +
	`"request_id": "41", "status": "pending", "summary": "permission for tool call call-9", "updated_at": "2026-09-20T10:00:00Z"}`

// respondCLI runs NativeRespond and returns its exit code plus stderr.
func respondCLI(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var errout bytes.Buffer
	code := NativeRespond(args, &errout)
	return code, errout.String()
}

// TestNativeRespondWritesAnswer pins the happy path: a pending request and a
// host-offered option produce exactly the canonical answer file.
func TestNativeRespondWritesAnswer(t *testing.T) {
	directory := t.TempDir()
	writePendingsFile(t, directory, cliPendingPermission)
	code, errout := respondCLI(t, "--job", directory, "--request", "41", "--option", "allow-once")
	if code != 0 || errout != "" {
		t.Fatalf("code = %d stderr = %q", code, errout)
	}
	body := readCLIFile(t, filepath.Join(directory, "interaction-answer.json"))
	value := decodeTestJSON(t, body)
	record := objectOrEmpty(value)
	if textOf(record["request_id"]) != "41" || textOf(record["option"]) != "allow-once" ||
		getOf(record, "cancel") != false || textOf(record["written_at"]) == "" {
		t.Fatalf("answer = %s", body)
	}
}

// TestNativeRespondCancel pins the cancel answer: no option member at all.
func TestNativeRespondCancel(t *testing.T) {
	directory := t.TempDir()
	writePendingsFile(t, directory, cliPendingPermission)
	code, errout := respondCLI(t, "--job", directory, "--request", "41", "--cancel")
	if code != 0 || errout != "" {
		t.Fatalf("code = %d stderr = %q", code, errout)
	}
	record := objectOrEmpty(decodeTestJSON(t,
		readCLIFile(t, filepath.Join(directory, "interaction-answer.json"))))
	if getOf(record, "cancel") != true {
		t.Fatalf("answer = %v", record)
	}
	if _, present := entryOf(record, "option"); present {
		t.Fatalf("a cancel answer carries no option: %v", record)
	}
}

// TestNativeRespondValidation pins the refusal ladder: unknown request id,
// non-pending status, an option the host never offered, a missing pending
// file, an unconsumed previous answer, and the usage errors.
func TestNativeRespondValidation(t *testing.T) {
	directory := t.TempDir()
	writePendingsFile(t, directory, cliPendingPermission,
		`{"connection": 1, "method": "session/request_permission", "options": [], "request_id": "42", "status": "invalidated", "summary": "gone", "updated_at": "2026-09-20T10:00:00Z"}`)

	if code, errout := respondCLI(t, "--job", directory, "--request", "999", "--option", "allow-once"); code != 1 ||
		!strings.Contains(errout, "no pending interaction with request id 999") {
		t.Fatalf("unknown id: %d %q", code, errout)
	}
	if code, _ := respondCLI(t, "--job", directory, "--request", "42", "--option", "allow-once"); code != 1 {
		t.Fatalf("non-pending request must be refused, got %d", code)
	}
	code, errout := respondCLI(t, "--job", directory, "--request", "41", "--option", "allow-always")
	if code != 1 || !strings.Contains(errout, "not one the host offered") ||
		!strings.Contains(errout, "allow-once, reject-once") {
		t.Fatalf("non-host option: %d %q", code, errout)
	}
	empty := t.TempDir()
	if code, _ := respondCLI(t, "--job", empty, "--request", "41", "--option", "allow-once"); code != 1 {
		t.Fatalf("missing pending file must be refused, got %d", code)
	}
	if code, _ := respondCLI(t, "--job", filepath.Join(directory, "absent"), "--request", "41", "--cancel"); code != 1 {
		t.Fatalf("missing job directory must be refused, got %d", code)
	}

	// One answer at a time: an unconsumed answer file blocks the next vote.
	if code, _ := respondCLI(t, "--job", directory, "--request", "41", "--option", "reject-once"); code != 0 {
		t.Fatal("first answer must land")
	}
	code, errout = respondCLI(t, "--job", directory, "--request", "41", "--option", "reject-once")
	if code != 1 || !strings.Contains(errout, "has not consumed the previous answer") {
		t.Fatalf("double vote: %d %q", code, errout)
	}

	// Usage errors exit 2.
	for _, args := range [][]string{
		{},
		{"--job", directory},
		{"--request", "41", "--option", "allow-once"},
		{"--job", directory, "--request", "41"},
		{"--job", directory, "--request", "41", "--option", "allow-once", "--cancel"},
		{"--job", directory, "--request", "41", "--cancel", "--extra"},
	} {
		if code, _ := respondCLI(t, args...); code != 2 {
			t.Fatalf("args %v: exit %d, want 2", args, code)
		}
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

// TestNativePendingOffers pins the host-shape-agnostic option membership the
// CLI validates with: optionId (ACP/ZCode permissions) and value (ZCode
// question options).
func TestNativePendingOffers(t *testing.T) {
	options := []any{
		map[string]any{"optionId": "allow-once", "kind": "allow_once"},
		map[string]any{"value": "yes", "label": "Yes"},
	}
	if !nativePendingOffers(options, "allow-once") || !nativePendingOffers(options, "yes") {
		t.Fatal("both host spellings must match")
	}
	if nativePendingOffers(options, "nope") || nativePendingOffers(nil, "allow-once") {
		t.Fatal("nothing else may match")
	}
}

// TestNativeShowListsPendings pins the read-only listing: the records
// verbatim, an empty list when nothing was held, and nothing written.
func TestNativeShowListsPendings(t *testing.T) {
	directory := t.TempDir()
	var out, errout bytes.Buffer
	if code := NativeShow([]string{"--job", directory}, &out, &errout); code != 0 || out.String() != "[]\n" {
		t.Fatalf("empty: %d %q %q", code, out.String(), errout.String())
	}
	writePendingsFile(t, directory, cliPendingPermission)
	out.Reset()
	if code := NativeShow([]string{"--job", directory}, &out, &errout); code != 0 {
		t.Fatalf("code = %d", code)
	}
	want := mustCanonical(t, []any{decodeTestJSON(t, cliPendingPermission)}) + "\n"
	if out.String() != want {
		t.Fatalf("listing:\n got %q\nwant %q", out.String(), want)
	}
	if _, statErr := os.Stat(filepath.Join(directory, "interaction-answer.json")); !os.IsNotExist(statErr) {
		t.Fatal("native-show must never write an answer")
	}
	if code := NativeShow([]string{"--job", filepath.Join(directory, "absent")}, &out, &errout); code != 1 {
		t.Fatalf("missing directory: %d", code)
	}
	if code := NativeShow([]string{}, &out, &errout); code != 2 {
		t.Fatalf("usage: %d", code)
	}
}
