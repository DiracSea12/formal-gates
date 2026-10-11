package harness

// Pure-mechanics tests for the shared native transport: the decoded-value
// semantics the adapter relies on, the unbounded queue, the wait() budget
// rules, the universal-newline reader and the file-backed evidence helpers.
// The tests that spawn a host process live in native_transport_unix_test.go
// (and the execute sequence in native_dsh_unix_test.go), after the
// supervise_unix_test.go precedent of keeping POSIX-only machinery behind a
// build tag.

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type immediateReplyWriter struct {
	write func([]byte) (int, error)
}

func (w immediateReplyWriter) Write(body []byte) (int, error) { return w.write(body) }
func (immediateReplyWriter) Close() error                     { return nil }

// mustCanonical is the canonical JSON text of value, for byte-shape asserts.
func mustCanonical(t *testing.T, value any) string {
	t.Helper()
	encoded, err := CanonicalJSON(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(encoded)
}

// decodeTestJSON decodes one JSON document or fails the test.
func decodeTestJSON(t *testing.T, raw string) any {
	t.Helper()
	value, err := decodeNativeJSON([]byte(raw))
	if err != nil {
		t.Fatalf("decode %s: %v", raw, err)
	}
	return value
}

// TestNativeValueSemantics pins the decoded-value rules the adapter relies on:
// equality is value equality (numbers exactly, across this package's own ints
// and decoded literals; a bool never equals a number), and a turn is an
// integer literal.
func TestNativeValueSemantics(t *testing.T) {
	equals := []struct {
		a, b any
		want bool
	}{
		{json.Number("1"), json.Number("1.0"), true},
		{1, json.Number("1"), true}, // our counter meets the host's echoed literal
		{int64(3), json.Number("3"), true},
		{true, true, true},
		{true, json.Number("1"), false}, // a bool is never a number
		{false, json.Number("0"), false},
		{json.Number("1"), "1", false},
		{json.Number("2"), json.Number("2.0000000000000001"), false},
		{[]any{json.Number("1")}, []any{json.Number("1.0")}, true},
		{map[string]any{"a": json.Number("1"), "b": nil},
			map[string]any{"b": nil, "a": json.Number("1.0")}, true},
		{map[string]any{"a": 1}, map[string]any{"a": 1, "b": nil}, false},
		{nil, nil, true},
		{"s", "s", true},
	}
	for _, tc := range equals {
		if got := valuesEqual(tc.a, tc.b); got != tc.want {
			t.Fatalf("valuesEqual(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
		if got := valuesEqual(tc.b, tc.a); got != tc.want {
			t.Fatalf("valuesEqual(%v, %v) = %v, want %v (symmetry)", tc.b, tc.a, got, tc.want)
		}
	}

	integers := map[any]bool{
		json.Number("1"): true, json.Number("-5"): true,
		json.Number("123456789012345678901234567890"): true,
		json.Number("1.0"):                            false, json.Number("1e2"): false,
		true: false, "1": false, nil: false,
	}
	for v, want := range integers {
		if got := isIntegerLiteral(v); got != want {
			t.Fatalf("isIntegerLiteral(%v) = %v, want %v", v, got, want)
		}
	}
}

// TestTextOf pins how a host-controlled value is rendered inside an error
// text: strings verbatim, every other value as canonical JSON.
func TestTextOf(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{nil, "null"},
		{true, "true"},
		{"raw", "raw"},
		{json.Number("5"), "5"},
		{json.Number("1e2"), "100.0"},
		{map[string]any{"kind": "errored"}, `{"kind":"errored"}`},
		{[]any{nil, "s"}, `[null,"s"]`},
	}
	for _, tc := range cases {
		if got := textOf(tc.v); got != tc.want {
			t.Fatalf("textOf(%v) = %q, want %q", tc.v, got, tc.want)
		}
	}
}

// TestReadNativeLine pins the stdout reader: universal newlines, buffer
// refills on very long lines, and a final unterminated line still yielded
// (with io.EOF, which collect treats as end-of-stream, not as a line).
func TestReadNativeLine(t *testing.T) {
	long := strings.Repeat("x", 200_000)
	reader := bufio.NewReader(strings.NewReader("a\r\nb\rc\n" + long))
	for _, want := range []string{"a", "b", "c", long} {
		got, err := readNativeLine(reader)
		if got != want {
			t.Fatalf("readNativeLine = %q (len %d), want %q (len %d)", got, len(got), want, len(want))
		}
		if want == long && err == nil {
			t.Fatal("the final unterminated line must report io.EOF")
		}
	}
	if _, err := readNativeLine(reader); err == nil {
		t.Fatal("expected EOF after the last line")
	}
}

// TestNativeQueueUnbounded pins the queue mechanics: producers never block,
// consumers time out or block forever (合同：无界消息队列，mutex＋切片＋通知
// channel，不用有界 channel), and every item carries its arrival sequence.
func TestNativeQueueUnbounded(t *testing.T) {
	queue := newNativeQueue()
	wait := 30 * time.Millisecond
	if _, _, ok := queue.get(&wait); ok {
		t.Fatal("empty queue must time out")
	}
	for i := 0; i < 100; i++ {
		queue.put(i)
	}
	for i := 0; i < 100; i++ {
		value, seq, ok := queue.get(&wait)
		if !ok || value.(int) != i || seq != int64(i+1) {
			t.Fatalf("get %d = %v %d %v", i, value, seq, ok)
		}
	}
	if got := queue.arrived(); got != 100 {
		t.Fatalf("arrived = %d, want 100", got)
	}
	// A nil wait blocks until a producer arrives.
	arrived := make(chan any, 1)
	go func() {
		value, _, _ := queue.get(nil)
		arrived <- value
	}()
	select {
	case <-arrived:
		t.Fatal("blocking get must not return before a put")
	case <-time.After(50 * time.Millisecond):
	}
	queue.put("late")
	select {
	case value := <-arrived:
		if value != "late" {
			t.Fatalf("got %v", value)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocking get never woke")
	}
}

// TestNativeHeldAnswerObservationBoundary pins answer delivery's arrival
// boundary. Messages already queued before the answer cannot acknowledge it,
// while a continuation emitted synchronously from the answer write can.
func TestNativeHeldAnswerObservationBoundary(t *testing.T) {
	newHold := func() *nativePendingHold {
		return &nativePendingHold{
			requestID: "reply-1",
			method:    "session/request_permission",
			frameID:   "frame-1",
			params:    map[string]any{},
			spec: &nativeInteractionSpec{
				reply: func(map[string]any, nativeAnswerChoice) (map[string]any, error) {
					return map[string]any{"result": map[string]any{"outcome": "selected"}}, nil
				},
			},
			status: "pending",
		}
	}
	writeAnswer := func(t *testing.T, directory string) {
		t.Helper()
		body := []byte(`{"request_id":"reply-1","option":"allow","cancel":false}`)
		if err := os.WriteFile(filepath.Join(directory, "interaction-answer.json"), body, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("immediate continuation is observed", func(t *testing.T) {
		directory := t.TempDir()
		queue := newNativeQueue()
		hold := newHold()
		var replyChoice nativeAnswerChoice
		hold.spec.reply = func(_ map[string]any, choice nativeAnswerChoice) (map[string]any, error) {
			replyChoice = choice
			return map[string]any{"result": map[string]any{"outcome": "selected"}}, nil
		}
		client := &nativeClient{
			directory: directory,
			messages:  queue,
			holdings:  []*nativePendingHold{hold},
			hooks: nativeHooks{observes: func(*nativePendingHold, map[string]any) bool {
				return true
			}},
		}
		queue.put(map[string]any{"kind": "before-answer"})
		var sent []byte
		client.stdin = immediateReplyWriter{write: func(body []byte) (int, error) {
			sent = append(sent, body...)
			queue.put(map[string]any{"kind": "immediate-continuation"})
			return len(body), nil
		}}
		writeAnswer(t, directory)

		if !client.deliverOneAnswer() {
			t.Fatal("answer file was not consumed")
		}
		if hold.status != "delivered" || hold.deliveredArrival != 1 {
			t.Fatalf("after delivery: status = %q boundary = %d, want delivered at 1", hold.status, hold.deliveredArrival)
		}
		wantFrame := mustCanonical(t, map[string]any{
			"id": "frame-1", "result": map[string]any{"outcome": "selected"},
		}) + "\n"
		if string(sent) != wantFrame {
			t.Fatalf("answer frame = %q, want %q", sent, wantFrame)
		}
		if replyChoice.OptionID != "allow" || replyChoice.Cancel {
			t.Fatalf("reply choice = %+v, want option allow without cancel", replyChoice)
		}

		value, arrival, ok := queue.get(nil)
		if !ok {
			t.Fatal("pre-answer message missing")
		}
		client.observePendings(value.(map[string]any), arrival)
		if hold.status != "delivered" {
			t.Fatalf("pre-answer message changed status to %q", hold.status)
		}

		value, arrival, ok = queue.get(nil)
		if !ok {
			t.Fatal("immediate continuation missing")
		}
		client.observePendings(value.(map[string]any), arrival)
		if hold.status != "observed" {
			t.Fatalf("immediate continuation left status at %q", hold.status)
		}
		pending, err := readJSONFile(filepath.Join(directory, "interaction-pending.json"))
		if err != nil {
			t.Fatal(err)
		}
		records := listOf(pending)
		if len(records) != 1 {
			t.Fatalf("pending records = %v, want one", pending)
		}
		record := objectOrEmpty(records[0])
		if textOf(record["request_id"]) != "reply-1" || textOf(record["status"]) != "observed" {
			t.Fatalf("pending record = %v, want reply-1 observed", record)
		}
	})

	t.Run("failed send remains pending", func(t *testing.T) {
		directory := t.TempDir()
		hold := newHold()
		client := &nativeClient{
			directory: directory,
			messages:  newNativeQueue(),
			holdings:  []*nativePendingHold{hold},
			stdin: immediateReplyWriter{write: func([]byte) (int, error) {
				return 0, io.ErrClosedPipe
			}},
		}
		writeAnswer(t, directory)

		if !client.deliverOneAnswer() {
			t.Fatal("answer file was not consumed")
		}
		if hold.status != "pending" || hold.deliveredArrival != 0 {
			t.Fatalf("failed send: status = %q boundary = %d, want pending at 0", hold.status, hold.deliveredArrival)
		}
	})
}

// TestNativeWaitDuration pins wait(): per-message budget without a deadline,
// unlimited for UNBOUNDED, and the 0.1s floor against a passing deadline.
func TestNativeWaitDuration(t *testing.T) {
	if got := nativeWaitDuration(40*time.Second, time.Time{}, false); got == nil || *got != 40*time.Second {
		t.Fatalf("wait(40, None) = %v, want 40s", got)
	}
	if got := nativeWaitDuration(nativeUnbounded, time.Time{}, false); got != nil {
		t.Fatalf("wait(UNBOUNDED, None) = %v, want nil", got)
	}
	deadline := time.Now().Add(-5 * time.Second) // already passed
	if got := nativeWaitDuration(40*time.Second, deadline, true); got == nil || *got < 100*time.Millisecond {
		t.Fatalf("floor = %v, want >= 0.1s", got)
	}
}

// TestReadJSONFileSemantics pins infra.read: duplicate keys are refused with
// the HarnessError family, a missing file keeps the OS error text, a malformed
// document is an invalid-JSON error, and trailing data after the document is
// refused.
func TestReadJSONFileSemantics(t *testing.T) {
	directory := t.TempDir()

	path := filepath.Join(directory, "duplicate.json")
	if err := os.WriteFile(path, []byte(`{"a": 1, "b": 2, "a": 3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := readJSONFile(path)
	if _, ok := err.(*bindingError); !ok || err.Error() != "duplicate JSON field: a" {
		t.Fatalf("duplicate keys: %v (%T)", err, err)
	}

	absent := filepath.Join(directory, "absent.json")
	_, err = readJSONFile(absent)
	pathErr, ok := err.(*os.PathError)
	if !ok || pathErr.Op != "open" || pathErr.Path != absent || !os.IsNotExist(pathErr.Err) {
		t.Fatalf("missing file: %v", err)
	}

	path = filepath.Join(directory, "malformed.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = readJSONFile(path)
	if err == nil || !strings.HasPrefix(err.Error(), "invalid JSON in "+path+": ") {
		t.Fatalf("malformed file: %v", err)
	}

	path = filepath.Join(directory, "trailing.json")
	if err := os.WriteFile(path, []byte(`{"z": 1, "a": 2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	value, err := readJSONFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(value, map[string]any{"z": json.Number("1"), "a": json.Number("2")}) {
		t.Fatalf("value = %v", value)
	}
	if id, ok := value.(map[string]any); !ok {
		t.Fatalf("decoded object = %T", value)
	} else if _, present := id["z"]; !present {
		t.Fatal("decoded object lost a key")
	}

	for name, raw := range map[string]string{
		"extra data": `{"a": 1} {"b": 2}`,
		"junk tail":  `{"a": 1} xx`,
	} {
		path := filepath.Join(directory, name+".json")
		if err := os.WriteFile(path, []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := readJSONFile(path); err == nil {
			t.Fatalf("%s must be refused", name)
		}
	}
}

// TestNativeErrorTexts pins the family-prefixed rendering the error file
// records, and that a plain Go error
// is recorded as its own text.
func TestNativeErrorTexts(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&blockedError{message: "DSH_STOP: aborted"}, "RuntimeBlocked: DSH_STOP: aborted"},
		{&bindingError{message: "HOST_BINDING: provider differs"}, "HarnessError: HOST_BINDING: provider differs"},
		{&sessionUnavailableError{blockedError: blockedError{message: "DSH_SESSION_UNAVAILABLE: gone"}},
			"SessionUnavailable: DSH_SESSION_UNAVAILABLE: gone"},
		{io.EOF, "EOF"},
	}
	for _, tc := range cases {
		if got := nativeErrorText(tc.err); got != tc.want {
			t.Fatalf("nativeErrorText = %q, want %q", got, tc.want)
		}
	}
}

// TestForEachSessionEvent pins the shared evidence scan: mixed-session
// evidence is refused, a malformed line is an invalid-JSON error, and every
// record of the session is streamed to the handler in file order.
func TestForEachSessionEvent(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "native-events.jsonl")
	if err := os.WriteFile(path, []byte("{\"sessionId\": \"s-1\"}\n{\"sessionId\": \"s-2\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := forEachSessionEvent(directory, "s-1", func(map[string]any) error { return nil })
	if err == nil || err.Error() != "BINDING: mixed session evidence" {
		t.Fatalf("mixed sessions: %v", err)
	}

	if err := os.WriteFile(path, []byte("not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	err = forEachSessionEvent(directory, "s-1", func(map[string]any) error { return nil })
	if err == nil || !strings.HasPrefix(err.Error(), "invalid JSON in "+path+": ") {
		t.Fatalf("malformed line: %v", err)
	}

	if err := os.WriteFile(path, []byte("{\"sessionId\": \"s-1\", \"n\": 1}\n{\"sessionId\": \"s-1\", \"n\": 2}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var seen []any
	if err := forEachSessionEvent(directory, "s-1", func(record map[string]any) error {
		seen = append(seen, record["n"])
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != json.Number("1") || seen[1] != json.Number("2") {
		t.Fatalf("records = %v", seen)
	}

	if err := os.WriteFile(path, []byte("{\"sessionId\": \"s-1\"}\r\n{\"sessionId\": \"s-1\"}\r"), 0o644); err != nil {
		t.Fatal(err)
	}
	count := 0
	if err := forEachSessionEvent(directory, "s-1", func(map[string]any) error {
		count++
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("CRLF and lone-CR terminators must both split lines: %d", count)
	}
}

// TestWriteAnswerSemantics pins write_answer: the bytes are kept, a re-run is
// idempotent, a real difference is refused while line-ending-equal bytes are
// not, and a framing refusal passes through with its NATIVE_RESULT prefix.
func TestWriteAnswerSemantics(t *testing.T) {
	directory := t.TempDir()
	if err := writeNativeAnswer(directory, `{"ok": true}`, "DSH"); err != nil {
		t.Fatal(err)
	}
	if body, _ := os.ReadFile(filepath.Join(directory, "answer.raw.txt")); string(body) != `{"ok": true}` {
		t.Fatalf("answer.raw.txt = %q", body)
	}
	if body, _ := os.ReadFile(filepath.Join(directory, "answer.json")); string(body) != `{"ok":true}` {
		t.Fatalf("answer.json = %q", body)
	}
	if err := writeNativeAnswer(directory, `{"ok": true}`, "DSH"); err != nil {
		t.Fatalf("idempotent re-run: %v", err)
	}

	err := writeNativeAnswer(directory, `{"ok": false}`, "DSH")
	if err == nil || err.Error() != "DSH_RESULT: saved output differs" {
		t.Fatalf("saved output differs: %v", err)
	}

	err = writeNativeAnswer(t.TempDir(), "not json", "DSH")
	if err == nil || err.Error() != "NATIVE_RESULT: no unambiguous result object" {
		t.Fatalf("framing refusal: %v", err)
	}
}

func TestWriteAnswerLineEndings(t *testing.T) {
	for _, newline := range []struct{ name, text string }{{"LF", "\n"}, {"CRLF", "\r\n"}, {"CR", "\r"}} {
		t.Run(newline.name, func(t *testing.T) {
			directory := t.TempDir()
			text := strings.ReplaceAll("{\n\"ok\": true\n}", "\n", newline.text)
			for _, answer := range []string{text, text, "{\n\"ok\": true\n}"} {
				if err := writeNativeAnswer(directory, answer, "DSH"); err != nil {
					t.Fatal(err)
				}
				if body := readBytes(t, filepath.Join(directory, "answer.raw.txt")); string(body) != text {
					t.Fatalf("native bytes changed: %q", body)
				}
				if body := readBytes(t, filepath.Join(directory, "answer.json")); string(body) != `{"ok":true}` {
					t.Fatalf("business result changed: %s", body)
				}
			}
			err := writeNativeAnswer(directory, strings.ReplaceAll(text, "true", "false"), "DSH")
			if err == nil || err.Error() != "DSH_RESULT: saved output differs" {
				t.Fatalf("different result accepted: %v", err)
			}
		})
	}
}

// TestEvidenceSourceShape pins the receipt: provider, identity, the caller's
// extra keys, then proofs for the four files; usage_evidence is null when
// empty.
func TestEvidenceSourceShape(t *testing.T) {
	directory := t.TempDir()
	for _, name := range nativeSourceFiles {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("evidence:"+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	receipt, err := evidenceSource(directory, "dsh", "s-1", map[string]any{
		"native_turn":    json.Number("1"),
		"transcript":     "/t/transcript.jsonl",
		"usage_evidence": nil,
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{
		"provider": "dsh", "identity": "s-1",
		"native_turn": json.Number("1"), "transcript": "/t/transcript.jsonl",
		"usage_evidence": nil,
		"files":          proofsOf(t, directory, nativeSourceFiles),
	}
	if !valuesEqual(receipt, want) {
		t.Fatalf("receipt = %s", mustCanonical(t, receipt))
	}
}

// proofsOf builds {name: proof} for the given evidence files.
func proofsOf(t *testing.T, directory string, names []string) map[string]any {
	t.Helper()
	out := map[string]any{}
	for _, name := range names {
		proof, err := fileProofOf(filepath.Join(directory, name))
		if err != nil {
			t.Fatal(err)
		}
		out[name] = proof
	}
	return out
}

// TestReconcileCompletedOutputGate pins the shared gate: exit_code==1 (numbers
// compare by value: this package's 1 meets the record's literal), an empty
// process error text, the error file existing and the caller's eligibility
// predicate, checked before any recovery runs.
func TestReconcileCompletedOutputGate(t *testing.T) {
	eligible := func(original string) bool { return original == "LEGACY" }
	recover := func() (map[string]any, error) {
		return map[string]any{"provider": "dsh"}, nil
	}
	base := func(t *testing.T) string {
		t.Helper()
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, "answer.raw.txt"), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
		return directory
	}

	directory := base(t)
	if err := writeAtomicJSON(filepath.Join(directory, "native-error.json"),
		map[string]any{"error": "LEGACY"}); err != nil {
		t.Fatal(err)
	}
	source, err := reconcileCompletedOutput(directory,
		map[string]any{"exit_code": 1, "error": ""}, "X", eligible, recover)
	if err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(source["reconciliation"], map[string]any{
		"kind":               "completed-output-format",
		"original_exit_code": 1,
		"original_error":     "LEGACY",
		"new_model_calls":    0,
	}) {
		t.Fatalf("reconciliation = %v", source["reconciliation"])
	}

	// Passers: numeric equality (the record's 1.0 literal) and non-string
	// error values (recorded without a message).
	for name, process := range map[string]any{
		"exit_code 1.0":  map[string]any{"exit_code": json.Number("1.0"), "error": ""},
		"error non-text": map[string]any{"exit_code": 1, "error": 0},
	} {
		if _, err := reconcileCompletedOutput(directory, process, "X", eligible, recover); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for name, process := range map[string]any{
		"exit code 0":    map[string]any{"exit_code": 0, "error": ""},
		"exit code 2":    map[string]any{"exit_code": 2, "error": ""},
		"exit code true": map[string]any{"exit_code": true, "error": ""},
		"error nonempty": map[string]any{"exit_code": 1, "error": "killed"},
	} {
		if _, err := reconcileCompletedOutput(directory, process, "X", eligible, recover); err == nil ||
			err.Error() != "X_RECONCILE: not a completed formatting-only failure" {
			t.Fatalf("%s: %v", name, err)
		}
	}

	empty := t.TempDir()
	_, err = reconcileCompletedOutput(empty, map[string]any{"exit_code": 1, "error": ""}, "X", eligible, recover)
	if err == nil || err.Error() != "X_RECONCILE: not a completed formatting-only failure" {
		t.Fatalf("no error file: %v", err)
	}
}

// TestResolveNativePath pins Path.resolve(): symlinks are resolved (including
// a symlinked prefix such as macOS /var) and a non-existent tail keeps the
// longest existing prefix resolved.
func TestResolveNativePath(t *testing.T) {
	directory := t.TempDir()
	real := filepath.Join(directory, "real")
	if err := os.Mkdir(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(directory, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	resolvedReal := resolveNativePath(real)
	if got := resolveNativePath(link); got != resolvedReal {
		t.Fatalf("resolveNativePath(%q) = %q, want %q", link, got, resolvedReal)
	}
	if got := resolveNativePath(filepath.Join(link, "child")); got != filepath.Join(resolvedReal, "child") {
		t.Fatalf("resolveNativePath = %q, want %q", got, filepath.Join(resolvedReal, "child"))
	}
	missing := filepath.Join(link, "nested", "child", "leaf")
	want := filepath.Join(resolvedReal, "nested", "child", "leaf")
	if got := resolveNativePath(missing); got != want {
		t.Fatalf("multi-level missing path = %q, want %q", got, want)
	}
	if err := os.MkdirAll(missing, 0755); err != nil {
		t.Fatal(err)
	}
	if got := resolveNativePath(missing); got != want {
		t.Fatalf("created path identity changed = %q, want %q", got, want)
	}
}
