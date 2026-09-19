package harness

// Pure-mechanics tests for the shared native transport: the unbounded queue,
// the wait() budget rules, the universal-newline readers, the Python
// decoded-value semantics and the file-backed evidence helpers. The tests that
// spawn a host process live in native_transport_unix_test.go (and the execute
// sequence in native_dsh_unix_test.go), after the supervise_unix_test.go
// precedent of keeping POSIX-only machinery behind a build tag.

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

// TestPythonValueSemantics pins the decoded-value semantics the adapter relies
// on: truthiness, == (bool is an int subclass, int==float exactly, containers
// deeply), isinstance(v, int) and str(v) for the f-string interpolations the
// reference implementation's error texts embed (合同保真要点 3).
func TestPythonValueSemantics(t *testing.T) {
	falsy := []any{nil, false, json.Number("0"), json.Number("0.0"), json.Number("-0"),
		json.Number("0e0"), "", dict(), []any{}}
	for _, v := range falsy {
		if pyTruthy(v) {
			t.Fatalf("pyTruthy(%v) = true", v)
		}
	}
	truthy := []any{json.Number("NaN"), "x", json.Number("0.1"), []any{0}, dict("a", nil), true}
	for _, v := range truthy {
		if !pyTruthy(v) {
			t.Fatalf("pyTruthy(%v) = false", v)
		}
	}

	equals := []struct {
		a, b any
		want bool
	}{
		{json.Number("1"), json.Number("1.0"), true},
		{true, json.Number("1"), true},
		{false, json.Number("0"), true},
		{json.Number("1"), "1", false},
		{json.Number("2"), json.Number("2.0000000000000001"), false},
		{json.Number("NaN"), json.Number("NaN"), false},
		{[]any{json.Number("1")}, []any{json.Number("1.0")}, true},
		{dict("a", json.Number("1"), "b", nil), dict("b", nil, "a", json.Number("1.0")), true},
		{dict("a", 1), dict("a", 1, "b", nil), false},
		{nil, nil, true},
		{"s", "s", true},
	}
	for _, tc := range equals {
		if got := pyEquals(tc.a, tc.b); got != tc.want {
			t.Fatalf("pyEquals(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
		if got := pyEquals(tc.b, tc.a); got != tc.want {
			t.Fatalf("pyEquals(%v, %v) = %v, want %v (symmetry)", tc.b, tc.a, got, tc.want)
		}
	}

	ints := map[any]bool{
		json.Number("1"): true, json.Number("-5"): true,
		json.Number("123456789012345678901234567890"): true,
		true: true, json.Number("1.0"): false, json.Number("1e2"): false,
	}
	for v, want := range ints {
		if got := pyIsInt(v); got != want {
			t.Fatalf("pyIsInt(%v) = %v, want %v", v, got, want)
		}
	}

	strs := []struct {
		v    any
		want string
	}{
		{nil, "None"}, {true, "True"}, {false, "False"}, {"raw", "raw"},
		{json.Number("5"), "5"}, {json.Number("1e2"), "100.0"},
		{dict("b", json.Number("1e2"), "a", "x"), "{'b': 100.0, 'a': 'x'}"},
		{[]any{nil, "s"}, "[None, 's']"},
	}
	for _, tc := range strs {
		if got := pyStr(tc.v); got != tc.want {
			t.Fatalf("pyStr(%v) = %q, want %q", tc.v, got, tc.want)
		}
	}
}

// TestSplitLinesUniversalNewlines pins the events-file line iteration: \n,
// \r\n and a lone \r all end lines, a final unterminated line still counts and
// an empty file has none.
func TestSplitLinesUniversalNewlines(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"a\nb", []string{"a", "b"}},
		{"a\nb\n", []string{"a", "b"}},
		{"a\r\nb", []string{"a", "b"}},
		{"a\rb", []string{"a", "b"}},
		{"a\n\nb", []string{"a", "", "b"}},
		{"a", []string{"a"}},
	}
	for _, tc := range cases {
		got := splitLines(tc.in)
		if len(got) != len(tc.want) {
			t.Fatalf("splitLines(%q) = %v, want %v", tc.in, got, tc.want)
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Fatalf("splitLines(%q) = %v, want %v", tc.in, got, tc.want)
			}
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
// channel，不用有界 channel).
func TestNativeQueueUnbounded(t *testing.T) {
	queue := newNativeQueue()
	wait := 30 * time.Millisecond
	if _, ok := queue.get(&wait); ok {
		t.Fatal("empty queue must time out")
	}
	for i := 0; i < 100; i++ {
		queue.put(i)
	}
	for i := 0; i < 100; i++ {
		value, ok := queue.get(&wait)
		if !ok || value.(int) != i {
			t.Fatalf("get %d = %v %v", i, value, ok)
		}
	}
	// A nil wait blocks until a producer arrives.
	arrived := make(chan any, 1)
	go func() {
		value, _ := queue.get(nil)
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

// TestNativeWaitDuration pins wait(): per-message budget without a deadline,
// unlimited for UNBOUNDED, and the 0.1s floor against a passing deadline
// (mirrors the reference's wait() unit assertions).
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

// TestReadJSONFileSemantics pins infra.read: duplicate keys are refused, a
// missing file is the Python FileNotFoundError text, and document key order
// survives (native-error.json/identity.json/native-request.json readers).
func TestReadJSONFileSemantics(t *testing.T) {
	directory := t.TempDir()

	path := filepath.Join(directory, "duplicate.json")
	if err := os.WriteFile(path, []byte(`{"a": 1, "b": 2, "a": 3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := readJSONFile(path)
	if _, ok := err.(*pyHarnessError); !ok || err.Error() != "duplicate JSON field: a" {
		t.Fatalf("duplicate keys: %v (%T)", err, err)
	}

	_, err = readJSONFile(filepath.Join(directory, "absent.json"))
	want := "FileNotFoundError: [Errno 2] No such file or directory: '" + filepath.Join(directory, "absent.json") + "'"
	if err == nil || err.Error() != want {
		t.Fatalf("missing file: %v", err)
	}

	path = filepath.Join(directory, "malformed.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = readJSONFile(path)
	if err == nil || err.Error() != "JSONDecodeError: Expecting value: line 1 column 1 (char 0)" {
		t.Fatalf("malformed file: %v", err)
	}

	path = filepath.Join(directory, "ordered.json")
	if err := os.WriteFile(path, []byte(`{"z": 1, "a": 2}`), 0o644); err != nil {
		t.Fatal(err)
	}
	value, err := readJSONFile(path)
	if err != nil {
		t.Fatal(err)
	}
	object, _ := value.(ordObj)
	if keys := object.Keys(); len(keys) != 2 || keys[0] != "z" || keys[1] != "a" {
		t.Fatalf("document order lost: %v", keys)
	}
}

// TestNativeErrorTexts pins the "<TypeName>: <msg>" rendering the error file
// records, including SessionUnavailable's family name.
func TestNativeErrorTexts(t *testing.T) {
	cases := []struct {
		err  error
		want string
	}{
		{&pyError{name: "KeyError", message: "'prompt'"}, "KeyError: 'prompt'"},
		{&pyRuntimeBlocked{message: "DSH_STOP: aborted"}, "RuntimeBlocked: DSH_STOP: aborted"},
		{&pyHarnessError{message: "HOST_BINDING: provider differs"}, "HarnessError: HOST_BINDING: provider differs"},
		{&pySessionUnavailable{pyRuntimeBlocked: pyRuntimeBlocked{message: "DSH_SESSION_UNAVAILABLE: gone"}},
			"SessionUnavailable: DSH_SESSION_UNAVAILABLE: gone"},
		{io.EOF, "OSError: EOF"},
	}
	for _, tc := range cases {
		if got := nativeErrorText(tc.err); got != tc.want {
			t.Fatalf("nativeErrorText = %q, want %q", got, tc.want)
		}
	}
}

// TestSessionEventsRejects pins the shared evidence scan: mixed-session
// evidence is refused and a malformed line is the Python JSONDecodeError.
func TestSessionEventsRejects(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "native-events.jsonl")
	if err := os.WriteFile(path, []byte("{\"sessionId\": \"s-1\"}\n{\"sessionId\": \"s-2\"}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := sessionEvents(directory, "s-1")
	if err == nil || err.Error() != "BINDING: mixed session evidence" {
		t.Fatalf("mixed sessions: %v", err)
	}

	if err := os.WriteFile(path, []byte("not json\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = sessionEvents(directory, "s-1")
	if err == nil || err.Error() != "JSONDecodeError: Expecting value: line 1 column 1 (char 0)" {
		t.Fatalf("malformed line: %v", err)
	}
}

// TestWriteAnswerSemantics pins write_answer: the bytes are kept, a re-run is
// idempotent, a real difference is refused while text-mode-equal line endings
// are not, and a framing refusal passes through with its NATIVE_RESULT prefix.
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

	// Text-mode equal line endings are not a difference (read_text translates
	// \r\n and \r to \n before comparing).
	crlf := t.TempDir()
	if err := os.WriteFile(filepath.Join(crlf, "answer.raw.txt"), []byte("{\r\n\"ok\": true\r\n}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := writeNativeAnswer(crlf, "{\n\"ok\": true\n}", "DSH"); err != nil {
		t.Fatalf("CRLF-equivalent bytes must not be refused: %v", err)
	}
	if body, _ := os.ReadFile(filepath.Join(crlf, "answer.raw.txt")); string(body) != "{\r\n\"ok\": true\r\n}" {
		t.Fatalf("saved bytes rewritten: %q", body)
	}

	err = writeNativeAnswer(t.TempDir(), "not json", "DSH")
	if err == nil || err.Error() != "NATIVE_RESULT: no unambiguous result object" {
		t.Fatalf("framing refusal: %v", err)
	}
}

// TestEvidenceSourceShape pins the receipt: provider, identity, the caller's
// extra keys in order, then the four proofs; usage_evidence is null when empty.
func TestEvidenceSourceShape(t *testing.T) {
	directory := t.TempDir()
	for _, name := range nativeSourceFiles {
		if err := os.WriteFile(filepath.Join(directory, name), []byte("evidence:"+name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	receipt, err := evidenceSource(directory, "dsh", "s-1", dict(
		"native_turn", json.Number("1"),
		"transcript", "/t/transcript.jsonl",
		"usage_evidence", nil,
	))
	if err != nil {
		t.Fatal(err)
	}
	if keys := receipt.Keys(); keys[len(keys)-1] != "files" {
		t.Fatalf("files must be the last key: %v", keys)
	}
	text, err := pyDumps(receipt)
	if err != nil {
		t.Fatal(err)
	}
	wantPrefix := `{"provider": "dsh", "identity": "s-1", "native_turn": 1, "transcript": "/t/transcript.jsonl", "usage_evidence": null, "files": {`
	if len(text) < len(wantPrefix) || text[:len(wantPrefix)] != wantPrefix {
		t.Fatalf("receipt = %s", text)
	}
}

// TestReconcileCompletedOutputGate pins the shared gate: exit_code==1 (Python
// equality: 1.0 and true both count), a falsy error, the error file existing
// and the caller's eligibility predicate, checked before any recovery runs.
func TestReconcileCompletedOutputGate(t *testing.T) {
	eligible := func(original string) bool { return original == "LEGACY" }
	recover := func() (ordObj, error) {
		return dict("provider", "dsh"), nil
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
	if err := writeNativeJSON(filepath.Join(directory, "native-error.json"), dict("error", "LEGACY")); err != nil {
		t.Fatal(err)
	}
	source, err := reconcileCompletedOutput(directory, dict("exit_code", 1, "error", ""), "X", eligible, recover)
	if err != nil {
		t.Fatal(err)
	}
	reconciliation, _ := source.Get("reconciliation")
	if !pyEquals(reconciliation, dict(
		"kind", "completed-output-format",
		"original_exit_code", 1,
		"original_error", "LEGACY",
		"new_model_calls", 0,
	)) {
		t.Fatalf("reconciliation = %v", reconciliation)
	}

	// Passers: Python equality (1 == 1.0) and falsy error values.
	for name, process := range map[string]any{
		"exit_code 1.0": dict("exit_code", json.Number("1.0"), "error", ""),
		"error falsy 0": dict("exit_code", 1, "error", 0),
	} {
		if _, err := reconcileCompletedOutput(directory, process, "X", eligible, recover); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
	}
	for name, process := range map[string]any{
		"exit code 0":    dict("exit_code", 0, "error", ""),
		"exit code 2":    dict("exit_code", 2, "error", ""),
		"error nonempty": dict("exit_code", 1, "error", "killed"),
	} {
		if _, err := reconcileCompletedOutput(directory, process, "X", eligible, recover); err == nil ||
			err.Error() != "X_RECONCILE: not a completed formatting-only failure" {
			t.Fatalf("%s: %v", name, err)
		}
	}

	empty := t.TempDir()
	_, err = reconcileCompletedOutput(empty, dict("exit_code", 1, "error", ""), "X", eligible, recover)
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
}
