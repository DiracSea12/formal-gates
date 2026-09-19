package harness

// The shared mechanics every native host transport uses, the port of
// formal_gates_harness/native_transport.py plus its decoded-value contract. A
// native transport is always the same machine: spawn one host process, speak
// line-delimited JSON over its stdio, keep a routing-only trace, answer the
// host's own client-directed requests and write the same four evidence files.
// Only the launch arguments and the protocol sequence belong to an adapter
// (native_dsh.go today, zcode in G2c), supplied as plain hook functions on
// nativeHooks - the Go spelling of the Python base class's overridable methods.
// No interface, no registry, no assembly injection: the adapter and the
// transport live in this one package.
//
// Byte contracts. Everything that lands on disk atomically is canonical
// (sorted keys, compact) through writeNativeJSON; answer.raw.txt keeps the
// original bytes; the JSON embedded in error texts and the schema embedded in
// the prompt keep Python's json.dumps(ensure_ascii=False) default shape through
// pyDumps/PyDumpsRaw. The outbound protocol lines, protocol.jsonl and stdout
// use the canonical compact form (registered bounded differences: JSON
// equivalence, consumers parse by value).

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
)

// nativeUnbounded is the request timeout marker for "the host decides how long
// its own turn takes": no per-message wait and no total deadline. It is not an
// infinity - the host's own turn length is the only limit.
const nativeUnbounded time.Duration = -1

// nativeUnsupportedMethod is the compatibility answer for optional client
// capabilities: the host defines this code as "client method not implemented".
const nativeUnsupportedMethod = -32601

// nativeSourceFiles is the evidence a native run must leave behind, in the
// order the receipt proofs them (native_transport.SOURCE_FILES).
var nativeSourceFiles = []string{"native-source.json", "native-events.jsonl", "answer.raw.txt", "answer.json"}

// --- typed errors (the Python exception families) -----------------------------

// pyHarnessError is the refusal family the reference implementation raises for
// binding-class failures (Python formal_gates_harness.infra.HarnessError).
type pyHarnessError struct{ message string }

func (e *pyHarnessError) Error() string { return e.message }

// pyRuntimeBlocked is the blocked-run refusal family (Python RuntimeBlocked).
type pyRuntimeBlocked struct{ message string }

func (e *pyRuntimeBlocked) Error() string { return e.message }

// pySessionUnavailable is the subclass of pyRuntimeBlocked for "the host
// explicitly rejected the persistent session identity". Identity and code keep
// the host's original JSON values (number, string or null) verbatim.
type pySessionUnavailable struct {
	pyRuntimeBlocked
	identity any
	method   string
	code     any
}

// pyError carries a Python exception the reference implementation would have
// raised verbatim (KeyError, FileNotFoundError, JSONDecodeError, ...); name is
// type(exc).__name__ and message is str(exc) without the class prefix.
type pyError struct {
	name    string
	message string
}

func (e *pyError) Error() string { return e.name + ": " + e.message }

// nativeErrorTypeName maps an error to the Python class name the native error
// file records. Unexpected Go failures land on OSError, which is what the
// reference implementation's OSError paths would have produced.
func nativeErrorTypeName(err error) string {
	switch typed := err.(type) {
	case *pyError:
		return typed.name
	case *pySessionUnavailable:
		return "SessionUnavailable"
	case *pyRuntimeBlocked:
		return "RuntimeBlocked"
	case *pyHarnessError:
		return "HarnessError"
	default:
		return "OSError"
	}
}

// nativeErrorText is f'{type(exc).__name__}: {exc}': the class name once, then
// the message. A pyError's own Error() already carries its class name, so it is
// not prefixed a second time.
func nativeErrorText(err error) string {
	if typed, ok := err.(*pyError); ok {
		return typed.name + ": " + typed.message
	}
	return nativeErrorTypeName(err) + ": " + err.Error()
}

// pyFileErrorParts renders a file access failure the way CPython raises it,
// from the same errno tables the supervisor uses (no second mapping). The
// reachable classes for reads are ENOENT (FileNotFoundError), EACCES
// (PermissionError), EISDIR (IsADirectoryError) and ENOTDIR
// (NotADirectoryError).
func pyFileErrorParts(err error, path string) (string, string) {
	var errno syscall.Errno
	if errors.As(err, &errno) {
		name := exceptionName[int(errno)]
		if name == "" {
			name = "OSError"
		}
		message := strerrorText[int(errno)]
		if message == "" {
			message = errno.Error()
		}
		return name, fmt.Sprintf("[Errno %d] %s: '%s'", int(errno), message, path)
	}
	return "OSError", err.Error()
}

// pyFileError is pyFileErrorParts as a pyError.
func pyFileError(err error, path string) error {
	name, message := pyFileErrorParts(err, path)
	return &pyError{name: name, message: message}
}

// pyExecErrorParts renders Go's process start failure using CPython's exception
// name and message, separately, mirroring mapExecError's tables (the reachable
// errnos are ENOENT, EACCES and ENOEXEC; anything else stays a generic OSError
// with the same {Name}: [{Errno}] ... shape).
func pyExecErrorParts(err error, argv0 string) (string, string) {
	var notFound *exec.Error
	if errors.As(err, &notFound) {
		// CPython's Popen reports an unresolvable command (bare name missing
		// from PATH, or a missing absolute path) as FileNotFoundError.
		return "FileNotFoundError", fmt.Sprintf("[Errno 2] No such file or directory: '%s'", argv0)
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		if name, ok := exceptionName[int(errno)]; ok {
			return name, fmt.Sprintf("[Errno %d] %s: '%s'", int(errno), strerrorText[int(errno)], argv0)
		}
		return "OSError", fmt.Sprintf("[Errno %d] %s: '%s'", int(errno), errno.Error(), argv0)
	}
	return "OSError", err.Error()
}

// --- reading files and paths the way the reference does -----------------------

// readJSONFile mirrors infra.read: read the file as UTF-8 text, decode one JSON
// document in document order, refuse duplicate keys. Failure texts are the
// Python ones (FileNotFoundError / IsADirectoryError / JSONDecodeError /
// duplicate field HarnessError) because they surface in native-error.json.
func readJSONFile(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, pyFileError(err, path)
	}
	value, err := decodeOrderedUnique(data)
	if err != nil {
		var syntax *jsonSyntaxError
		if errors.As(err, &syntax) {
			return nil, &pyError{name: "JSONDecodeError", message: syntax.Error()}
		}
		return nil, &pyHarnessError{message: err.Error()}
	}
	return value, nil
}

// resolveNativePath mirrors Path.resolve(): an absolute, symlink-resolved,
// lexically cleaned path. Non-existent trailing components keep the longest
// existing prefix resolved, the way Python's non-strict resolve does.
func resolveNativePath(path string) string {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	if resolved, err := filepath.EvalSymlinks(absolute); err == nil {
		return resolved
	}
	parent, base := filepath.Split(absolute)
	if resolved, err := filepath.EvalSymlinks(filepath.Clean(parent)); err == nil {
		return filepath.Join(resolved, base)
	}
	return absolute
}

// --- decoded-value access with Python semantics -------------------------------

// entryOf is v[key]; the second result reports presence (null counts).
func entryOf(v any, key string) (any, bool) {
	if object, ok := v.(ordObj); ok {
		return object.Get(key)
	}
	return nil, false
}

// getOf is v.get(key): nil when absent.
func getOf(v any, key string) any {
	value, _ := entryOf(v, key)
	return value
}

// getOrOf is v.get(key, default): the default only when the key is absent.
func getOrOf(v any, key string, fallback any) any {
	if value, ok := entryOf(v, key); ok {
		return value
	}
	return fallback
}

// mustEntry is v[key]: a Python KeyError (whose str is the repr of the key).
func mustEntry(v any, key string) (any, error) {
	if value, ok := entryOf(v, key); ok {
		return value, nil
	}
	return nil, &pyError{name: "KeyError", message: pyReprOf(key)}
}

// pySubscriptError renders CPython's message for subscripting a non-object with
// a string key, which is what read()ing a scalar native-request.json produces.
func pySubscriptError(v any) error {
	switch v.(type) {
	case []any:
		return &pyError{name: "TypeError", message: "list indices must be integers or slices, not str"}
	case string:
		return &pyError{name: "TypeError", message: "string indices must be integers"}
	case json.Number:
		return &pyError{name: "TypeError", message: "'float' object is not subscriptable"}
	case bool:
		return &pyError{name: "TypeError", message: "'bool' object is not subscriptable"}
	case nil:
		return &pyError{name: "TypeError", message: "'NoneType' object is not subscriptable"}
	default:
		return &pyError{name: "TypeError", message: fmt.Sprintf("'%T' object is not subscriptable", v)}
	}
}

// pyAttrError renders the AttributeError Python raises for .get on a non-dict.
func pyAttrError(v any, attribute string) error {
	return &pyError{name: "AttributeError", message: pyReprTypeName(v) + " object has no attribute '" + attribute + "'"}
}

func pyReprTypeName(v any) string {
	switch typed := v.(type) {
	case []any:
		return "'list'"
	case string:
		return "'str'"
	case json.Number:
		// Python's own literal split: an integer literal is an int, anything
		// with a fraction or exponent is a float.
		if !strings.ContainsAny(typed.String(), ".eE") {
			return "'int'"
		}
		return "'float'"
	case bool:
		return "'bool'"
	case nil:
		return "'NoneType'"
	default:
		return fmt.Sprintf("'%T'", v)
	}
}

// --- Python truthiness, equality and str() ------------------------------------

// pyTruthy is Python bool(v) over decoded JSON values: null, false, zero
// numbers, empty strings, empty arrays and empty objects are falsy.
func pyTruthy(v any) bool {
	switch typed := v.(type) {
	case nil:
		return false
	case bool:
		return typed
	case string:
		return typed != ""
	case []any:
		return len(typed) > 0
	case ordObj:
		return typed.Len() > 0
	case json.Number:
		return !numberIsZero(typed.String())
	case int:
		return typed != 0
	case int64:
		return typed != 0
	case float64:
		return typed != 0 // NaN is truthy, and NaN != 0 is true
	default:
		return true
	}
}

func numberIsZero(literal string) bool {
	rational, ok := new(big.Rat).SetString(literal)
	if ok {
		return rational.Sign() == 0
	}
	// NaN (or anything unparseable) is never zero; bool(float('nan')) is true.
	return false
}

func isJSONNumber(v any) bool {
	switch v.(type) {
	case json.Number, int, int64, float64:
		return true
	}
	return false
}

func boolNumber(v bool) json.Number {
	if v {
		return json.Number("1")
	}
	return json.Number("0")
}

// pyEquals is Python == over decoded JSON values. bool is an int subclass
// (true == 1), and integers compare numerically with floats (1 == 1.0), both
// exactly rather than after a float64 round-trip; other type pairs are unequal
// unless deeply equal.
func pyEquals(a, b any) bool {
	if boolean, ok := a.(bool); ok {
		if other, isBool := b.(bool); isBool {
			return boolean == other
		}
		return isJSONNumber(b) && literalsEqual(boolNumber(boolean), b)
	}
	if boolean, ok := b.(bool); ok {
		return isJSONNumber(a) && literalsEqual(boolNumber(boolean), a)
	}
	if isJSONNumber(a) || isJSONNumber(b) {
		return isJSONNumber(a) && isJSONNumber(b) && literalsEqual(a, b)
	}
	switch typed := a.(type) {
	case nil:
		return b == nil
	case string:
		other, ok := b.(string)
		return ok && typed == other
	case ordObj:
		return pyDictEqual(typed, b)
	case []any:
		other, ok := b.([]any)
		if !ok || len(typed) != len(other) {
			return false
		}
		for i := range typed {
			if !pyEquals(typed[i], other[i]) {
				return false
			}
		}
		return true
	}
	return false
}

// pyDictEqual is Python dict ==: same key set, order-insensitive.
func pyDictEqual(a ordObj, b any) bool {
	other, ok := b.(ordObj)
	if !ok || a.Len() != other.Len() {
		return false
	}
	for _, key := range a.Keys() {
		right, present := other.Get(key)
		if !present {
			return false
		}
		left, _ := a.Get(key)
		if !pyEquals(left, right) {
			return false
		}
	}
	return true
}

func literalsEqual(a, b any) bool {
	left, leftOK := rationalOf(a)
	right, rightOK := rationalOf(b)
	if leftOK && rightOK {
		return left.Cmp(right) == 0
	}
	// NaN (or an unparseable literal) never equals anything, itself included.
	return false
}

func rationalOf(v any) (*big.Rat, bool) {
	switch typed := v.(type) {
	case json.Number:
		rational, ok := new(big.Rat).SetString(typed.String())
		return rational, ok
	case int:
		return new(big.Rat).SetInt64(int64(typed)), true
	case int64:
		return new(big.Rat).SetInt64(typed), true
	case float64:
		rational := new(big.Rat).SetFloat64(typed)
		return rational, rational != nil
	}
	return nil, false
}

// pyIsInt is isinstance(v, int): bool passes (bool subclasses int), a float
// literal does not, an integer literal of any magnitude does.
func pyIsInt(v any) bool {
	switch typed := v.(type) {
	case bool:
		return true
	case json.Number:
		return !strings.ContainsAny(typed.String(), ".eE")
	case int, int64:
		return true
	}
	return false
}

// pyStr is str(v) as an f-string embeds it: strings raw, None/True/False
// spelled out, numbers rendered by the shared encoders, containers via repr.
func pyStr(v any) string {
	switch typed := v.(type) {
	case nil:
		return "None"
	case bool:
		if typed {
			return "True"
		}
		return "False"
	case string:
		return typed
	case json.Number, int, int64, float64:
		text, err := pyDumps(v)
		if err == nil {
			return text
		}
		return fmt.Sprintf("%v", v)
	case ordObj:
		var out strings.Builder
		writePyRepr(&out, typed)
		return out.String()
	case []any:
		var out strings.Builder
		writePyRepr(&out, typed)
		return out.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}

// pyReprOf is repr(v) for a decoded JSON value.
func pyReprOf(v any) string {
	var builder strings.Builder
	writePyRepr(&builder, v)
	return builder.String()
}

// writePyRepr renders a decoded JSON value like Python's repr(): single-quoted
// strings, dict/list display in insertion order.
func writePyRepr(out *strings.Builder, v any) {
	switch typed := v.(type) {
	case nil:
		out.WriteString("None")
	case bool:
		if typed {
			out.WriteString("True")
		} else {
			out.WriteString("False")
		}
	case string:
		out.WriteByte('\'')
		for _, r := range typed {
			switch r {
			case '\\':
				out.WriteString(`\\`)
			case '\'':
				out.WriteString(`\'`)
			case '\n':
				out.WriteString(`\n`)
			case '\r':
				out.WriteString(`\r`)
			case '\t':
				out.WriteString(`\t`)
			default:
				if r < 0x20 || r == 0x7f {
					fmt.Fprintf(out, `\x%02x`, r)
					continue
				}
				out.WriteRune(r)
			}
		}
		out.WriteByte('\'')
	case json.Number, int, int64, float64:
		out.WriteString(pyStr(typed))
	case ordObj:
		out.WriteByte('{')
		for i, key := range typed.Keys() {
			if i > 0 {
				out.WriteString(", ")
			}
			writePyRepr(out, key)
			out.WriteString(": ")
			value, _ := typed.Get(key)
			writePyRepr(out, value)
		}
		out.WriteByte('}')
	case []any:
		out.WriteByte('[')
		for i, item := range typed {
			if i > 0 {
				out.WriteString(", ")
			}
			writePyRepr(out, item)
		}
		out.WriteByte(']')
	default:
		fmt.Fprintf(out, "%v", typed)
	}
}

// --- shared evidence helpers (native_transport) --------------------------------

// proofObj shapes one content proof like Python's file_proof:
// {"sha256": ..., "size": ...} in that insertion order.
func proofObj(proof ContentProof) ordObj {
	return dict("sha256", proof.SHA256, "size", proof.Size)
}

// fileProofOf is file_proof: the content proof, or a RuntimeBlocked carrying
// the refusal text for a missing/unreadable file.
func fileProofOf(path string) (ordObj, error) {
	proof, refusal := FileProof(path)
	if refusal != "" {
		return ordObj{}, &pyRuntimeBlocked{message: refusal}
	}
	return proofObj(proof), nil
}

// buildCaptureBinding is capture_binding: {'provider', 'launch', 'files':
// {path: proof}} for every executed file the launch names.
func buildCaptureBinding(provider string, launch ordObj) (ordObj, error) {
	files := dict()
	for _, key := range launch.Keys() {
		value, _ := launch.Get(key)
		target := pyStr(value)
		proof, err := fileProofOf(target)
		if err != nil {
			return ordObj{}, err
		}
		files.Set(target, proof)
	}
	return dict("provider", provider, "launch", launch, "files", files), nil
}

// splitLines splits text like Python's universal-newline line iteration: \n,
// \r\n and lone \r all end lines, and a final unterminated line is still a
// line. Terminators are not part of the returned lines.
func splitLines(text string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '\n':
			lines = append(lines, text[start:i])
			start = i + 1
		case '\r':
			lines = append(lines, text[start:i])
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

// sessionEvents yields this session's evidence events, rejecting mixed-session
// evidence (native_transport.session_events). Lines are decoded with plain
// json.loads semantics: document order, duplicate keys collapse.
func sessionEvents(directory string, identity any) ([]any, error) {
	path := filepath.Join(directory, "native-events.jsonl")
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, pyFileError(err, path)
	}
	var events []any
	for _, line := range splitLines(string(body)) {
		value, err := decodeOrdered([]byte(line))
		if err != nil {
			var syntax *jsonSyntaxError
			if errors.As(err, &syntax) {
				return nil, &pyError{name: "JSONDecodeError", message: syntax.Error()}
			}
			return nil, err
		}
		if !pyEquals(getOf(value, "sessionId"), identity) {
			return nil, &pyRuntimeBlocked{message: "BINDING: mixed session evidence"}
		}
		events = append(events, value)
	}
	return events, nil
}

// writeNativeAnswer is write_answer: keep the native bytes, then decode them
// into the business result. The saved file is compared the way Python's
// text-mode read does it: universal newlines first (\r\n and a lone \r both
// become \n), so a byte difference that text mode cannot observe is not a
// refusal. A framing refusal is a RuntimeBlocked whose text already carries
// the NATIVE_RESULT prefix.
func writeNativeAnswer(directory, text, prefix string) error {
	raw := filepath.Join(directory, "answer.raw.txt")
	if _, statErr := os.Stat(raw); statErr == nil {
		body, err := os.ReadFile(raw)
		if err != nil {
			return pyFileError(err, raw)
		}
		if universalNewlines(string(body)) != text {
			return &pyRuntimeBlocked{message: prefix + "_RESULT: saved output differs"}
		}
	} else if os.IsNotExist(statErr) {
		if err := os.WriteFile(raw, []byte(text), 0o644); err != nil {
			return pyFileError(err, raw)
		}
	} else {
		return pyFileError(statErr, raw)
	}
	result, refusal := FrameResult(text)
	if refusal != "" {
		return &pyRuntimeBlocked{message: refusal}
	}
	return writeNativeJSON(filepath.Join(directory, "answer.json"), result)
}

// universalNewlines is text-mode reading in Python: every line ending (\n,
// \r\n or a lone \r) becomes \n.
func universalNewlines(text string) string {
	if !strings.ContainsRune(text, '\r') {
		return text
	}
	var out strings.Builder
	out.Grow(len(text))
	for i := 0; i < len(text); i++ {
		if text[i] == '\r' {
			out.WriteByte('\n')
			if i+1 < len(text) && text[i+1] == '\n' {
				i++
			}
			continue
		}
		out.WriteByte(text[i])
	}
	return out.String()
}

// evidenceSource is the receipt every host returns: provider, identity, the
// caller's extra keys in order, then proofs for the four evidence files.
func evidenceSource(directory, provider string, identity any, extra ordObj) (ordObj, error) {
	receipt := dict("provider", provider, "identity", identity)
	for _, key := range extra.Keys() {
		value, _ := extra.Get(key)
		receipt.Set(key, value)
	}
	files := dict()
	for _, name := range nativeSourceFiles {
		proof, err := fileProofOf(filepath.Join(directory, name))
		if err != nil {
			return ordObj{}, err
		}
		files.Set(name, proof)
	}
	receipt.Set("files", files)
	return receipt, nil
}

// reconcileCompletedOutput recovers only a completed native turn that the old
// result parser rejected (native_transport.reconcile_completed_output). The
// original process record is never rewritten; the caller-supplied recover
// re-observes the evidence.
func reconcileCompletedOutput(directory string, process any, prefix string,
	eligible func(original string) bool, recover func() (ordObj, error)) (ordObj, error) {
	errorPath := filepath.Join(directory, "native-error.json")
	exists := true
	original := ""
	if _, statErr := os.Stat(errorPath); statErr == nil {
		record, err := readJSONFile(errorPath)
		if err != nil {
			return ordObj{}, err
		}
		original = pyStr(getOrOf(record, "error", ""))
	} else if os.IsNotExist(statErr) {
		exists = false
	} else {
		return ordObj{}, pyFileError(statErr, errorPath)
	}
	// Only a result-framing failure of an otherwise completed process can be
	// replayed. A runtime or transport failure is never laundered into a result.
	if !pyEquals(getOf(process, "exit_code"), 1) || pyTruthy(getOf(process, "error")) || !exists || !eligible(original) {
		return ordObj{}, &pyRuntimeBlocked{message: prefix + "_RECONCILE: not a completed formatting-only failure"}
	}
	source, err := recover()
	if err != nil {
		return ordObj{}, err
	}
	source.Set("reconciliation", dict(
		"kind", "completed-output-format",
		"original_exit_code", 1,
		"original_error", original,
		"new_model_calls", 0,
	))
	if err := writeNativeJSON(filepath.Join(directory, "source.json"), source); err != nil {
		return ordObj{}, err
	}
	return source, nil
}

// --- the transport client ------------------------------------------------------

// nativeReply is a client-directed request's answer: the payload plus its
// recorded status.
type nativeReply struct {
	payload ordObj
	status  string
}

// nativeHooks is everything an adapter supplies on top of the shared transport
// mechanics (native_transport.Client's overridables). The zero value is the
// plain {id, method, params} JSON-RPC shape with a 60s bounded default.
type nativeHooks struct {
	// Transport failures are prefixed so evidence names the transport.
	transportPrefix string
	requestPrefix   string
	// How long one request waits, and whether that budget is total or per
	// message. A bounded request must not outlive its total deadline.
	defaultTimeout time.Duration
	bounded        bool
	// The session lifecycle call every host exposes under the same name.
	closeMethod string
	// Reader key and wording: hosts name their own "output ended" signal.
	closedKey      string
	closedMessage  string
	invalidMessage string

	identify     func(c *nativeClient) any
	envelope     func(value ordObj) ordObj
	respond      func(method string, params any) (*nativeReply, error)
	classify     func(method string, errValue any, params ordObj) error
	notification func(message any)
}

func (h nativeHooks) closedKeyOr() string {
	if h.closedKey != "" {
		return h.closedKey
	}
	return "closed"
}

func (h nativeHooks) closedMessageOr() string {
	if h.closedMessage != "" {
		return h.closedMessage
	}
	return "Native output closed"
}

func (h nativeHooks) invalidMessageOr() string {
	if h.invalidMessage != "" {
		return h.invalidMessage
	}
	return "Invalid native protocol output"
}

func (h nativeHooks) closeMethodOr() string {
	if h.closeMethod != "" {
		return h.closeMethod
	}
	return "session/close"
}

func (h nativeHooks) defaultTimeoutOr() time.Duration {
	if h.defaultTimeout > 0 {
		return h.defaultTimeout
	}
	return 60 * time.Second
}

// nativeClient is one host process and the JSON request/reply loop over its
// stdio (the port of native_transport.Client).
type nativeClient struct {
	hooks        nativeHooks
	directory    string
	counter      int
	interactions []ordObj
	messages     *nativeQueue
	errorsFile   *os.File
	traceFile    *os.File
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	readerDone   chan struct{}
	closeOnce    sync.Once
}

// newNativeClient spawns the host process: cwd is the workspace, the
// environment is the caller's own (never trimmed or extended), stderr appends
// to host.stderr, native-process.json is written atomically with the child's
// pid/pgid (pgid is null on Windows; the spawn never creates a new group so
// the pgid is the caller's), and the read loop starts immediately.
func newNativeClient(hooks nativeHooks, argv []string, workspace, directory string) (*nativeClient, error) {
	c := &nativeClient{
		hooks:      hooks,
		directory:  directory,
		messages:   newNativeQueue(),
		readerDone: make(chan struct{}),
	}
	var err error
	c.errorsFile, err = os.OpenFile(filepath.Join(directory, "host.stderr"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, pyFileError(err, filepath.Join(directory, "host.stderr"))
	}
	c.traceFile, err = os.OpenFile(filepath.Join(directory, "protocol.jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		c.errorsFile.Close()
		return nil, pyFileError(err, filepath.Join(directory, "protocol.jsonl"))
	}
	// CPython's Popen reports a bad cwd with the cwd itself in the error.
	if workspace != "" {
		info, statErr := os.Stat(workspace)
		if statErr != nil {
			if os.IsNotExist(statErr) {
				c.errorsFile.Close()
				c.traceFile.Close()
				return nil, &pyError{name: "FileNotFoundError",
					message: fmt.Sprintf("[Errno 2] No such file or directory: '%s'", workspace)}
			}
			c.errorsFile.Close()
			c.traceFile.Close()
			return nil, pyFileError(statErr, workspace)
		}
		if !info.IsDir() {
			c.errorsFile.Close()
			c.traceFile.Close()
			return nil, &pyError{name: "NotADirectoryError",
				message: fmt.Sprintf("[Errno 20] Not a directory: '%s'", workspace)}
		}
	}
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		c.errorsFile.Close()
		c.traceFile.Close()
		return nil, pyFileError(err, "stdio")
	}
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		stdinRead.Close()
		stdinWrite.Close()
		c.errorsFile.Close()
		c.traceFile.Close()
		return nil, pyFileError(err, "stdio")
	}
	c.stdin = stdinWrite
	c.cmd = exec.Command(argv[0], argv[1:]...)
	c.cmd.Dir = workspace
	c.cmd.Env = os.Environ()
	c.cmd.Stdin = stdinRead
	c.cmd.Stdout = stdoutWrite
	c.cmd.Stderr = c.errorsFile
	if startErr := c.cmd.Start(); startErr != nil {
		stdinRead.Close()
		stdinWrite.Close()
		stdoutRead.Close()
		stdoutWrite.Close()
		c.errorsFile.Close()
		c.traceFile.Close()
		name, message := pyExecErrorParts(startErr, argv[0])
		return nil, &pyError{name: name, message: message}
	}
	// The child owns the other ends now.
	stdinRead.Close()
	stdoutWrite.Close()

	var pgid any
	if runtime.GOOS != "windows" {
		group, groupErr := nativeProcessGroupID(c.cmd.Process.Pid)
		if groupErr != nil {
			// The reference implementation's constructor raises here and leaks
			// the just-spawned child; reaping it is observably identical and
			// avoids a zombie. The error text is the observable part.
			c.stdin.Close()
			stdoutRead.Close()
			_ = c.cmd.Process.Kill()
			_, _ = c.cmd.Process.Wait()
			c.errorsFile.Close()
			c.traceFile.Close()
			return nil, groupErr
		}
		pgid = group
	}
	if err := writeNativeJSON(filepath.Join(directory, "native-process.json"),
		dict("pid", c.cmd.Process.Pid, "pgid", pgid)); err != nil {
		return nil, err
	}
	go func() {
		defer close(c.readerDone)
		c.collect(stdoutRead)
	}()
	return c, nil
}

// nextCounter is the default ident hook's counter, starting at 1.
func (c *nativeClient) nextCounter() int {
	c.counter++
	return c.counter
}

// collect is the read loop: every stdout line is decoded and queued, a routing
// -only trace line is written first, a non-JSON line injects the invalid-output
// closure and stops the loop, and EOF (or a read error, which behaves as EOF)
// injects the output-closed closure behind whatever came before it. The queue
// is unbounded.
func (c *nativeClient) collect(stdout io.Reader) {
	defer c.messages.put(c.closureMessage(c.hooks.closedMessageOr()))
	reader := bufio.NewReaderSize(stdout, 64*1024)
	for {
		line, readErr := readNativeLine(reader)
		// A yielded line is handled even when it is empty after terminator
		// stripping (Python's json.loads('\n') is a non-JSON failure); the
		// empty read at clean EOF is not a line at all.
		if line != "" || readErr == nil {
			if !c.handleLine(line) {
				return
			}
		}
		if readErr != nil {
			return
		}
	}
}

// handleLine traces and queues one inbound line; false means stop reading.
func (c *nativeClient) handleLine(line string) bool {
	value, err := decodeOrdered([]byte(line))
	if err != nil {
		// A non-JSON line is a transport failure; reading stops here (the
		// deferred EOF closure still lands behind this one, as in Python).
		c.messages.put(c.closureMessage(c.hooks.invalidMessageOr()))
		return false
	}
	c.traceLine(value)
	c.messages.put(value)
	return true
}

// traceLine appends the routing-only record: {id?, method?}, present keys only
// (a null id counts as present), canonical compact form. Payloads,
// configuration and authentication material never appear.
func (c *nativeClient) traceLine(value any) {
	record := map[string]any{}
	if object, ok := value.(ordObj); ok {
		if id, present := object.Get("id"); present {
			record["id"] = id
		}
		if method, present := object.Get("method"); present {
			record["method"] = method
		}
	}
	text, err := CanonicalJSON(record)
	if err != nil {
		return
	}
	c.traceFile.WriteString(string(text) + "\n")
}

// closureMessage is the synthetic queue entry that ends the transport.
func (c *nativeClient) closureMessage(reason string) ordObj {
	return dict(c.hooks.closedKeyOr(), reason)
}

// closureOf is the reason this transport cannot continue, or nil. Keys are
// checked in order: the shared 'closed' key first, then this host's own
// closed_key; either one present with a truthy value in ANY inbound message
// counts (Python's `if closed:` truthiness test).
func (c *nativeClient) closureOf(message ordObj) any {
	for _, key := range []string{"closed", c.hooks.closedKeyOr()} {
		if value, present := message.Get(key); present && pyTruthy(value) {
			return value
		}
	}
	return nil
}

// send writes one outbound frame as a canonical compact JSON line (registered
// bounded difference: JSON-equivalent to the reference's default-separator
// shape, parsed by value on the host side).
func (c *nativeClient) send(value ordObj) error {
	encoded, err := CanonicalJSON(canonicalValue(value))
	if err != nil {
		return err
	}
	if _, err := c.stdin.Write(append(encoded, '\n')); err != nil {
		// CPython surfaces a dead reader as BrokenPipeError without a filename.
		var errno syscall.Errno
		if errors.As(err, &errno) && errno == syscall.EPIPE {
			return &pyError{name: "BrokenPipeError", message: "[Errno 32] Broken pipe"}
		}
		return pyFileError(err, "stdin")
	}
	return nil
}

// frame builds {id, method, params} and applies the host envelope.
func (c *nativeClient) frame(ident any, method string, params ordObj) ordObj {
	value := dict("id", ident, "method", method, "params", params)
	if c.hooks.envelope != nil {
		value = c.hooks.envelope(value)
	}
	return value
}

// errNativeTimedOut is the internal "queue stayed empty" sentinel; request
// converts it into the observable timeout error.
var errNativeTimedOut = errors.New("native message wait timed out")

// receive pops one message, raising the closure error when the transport ended
// and answering host→client requests inline (a reverse request is consumed,
// never matched against a pending ident).
func (c *nativeClient) receive(wait *time.Duration) (any, error) {
	message, ok := c.messages.get(wait)
	if !ok {
		return nil, errNativeTimedOut
	}
	if object, isObject := message.(ordObj); isObject {
		if reason := c.closureOf(object); reason != nil {
			return nil, c.blocked(pyStr(reason))
		}
		methodValue, hasMethod := object.Get("method")
		_, hasID := object.Get("id")
		if hasMethod && pyTruthy(methodValue) && hasID {
			if err := c.answerReverse(object); err != nil {
				return nil, err
			}
		} else if c.hooks.notification != nil {
			c.hooks.notification(message)
		}
	}
	return message, nil
}

// answerReverse replies to a host request and records what was answered. A
// respond hook error (a required user decision) propagates without any reply;
// a nil reply is the base class's default refusal.
func (c *nativeClient) answerReverse(message ordObj) error {
	method := pyStr(getOf(message, "method"))
	params := getOf(message, "params")
	if !pyTruthy(params) {
		params = dict()
	}
	var reply *nativeReply
	if c.hooks.respond != nil {
		var err error
		reply, err = c.hooks.respond(method, params)
		if err != nil {
			return err
		}
	}
	if reply == nil {
		reply = &nativeReply{
			payload: dict("error", dict(
				"code", nativeUnsupportedMethod,
				"message", "Client method not supported")),
			status: "unsupported-method",
		}
	}
	frame := dict("id", getOf(message, "id"))
	for _, key := range reply.payload.Keys() {
		value, _ := reply.payload.Get(key)
		frame.Set(key, value)
	}
	if c.hooks.envelope != nil {
		frame = c.hooks.envelope(frame)
	}
	if err := c.send(frame); err != nil {
		return err
	}
	c.interactions = append(c.interactions, dict("method", method, "status", reply.status))
	return writeNativeJSON(filepath.Join(c.directory, "interaction-status.json"), c.interactions)
}

// nativeWaitDuration is the next receive's blocking budget: none for UNBOUNDED,
// the request timeout without a deadline, otherwise the remaining budget with
// the 0.1s floor that keeps a bounded request from spinning.
func nativeWaitDuration(timeout time.Duration, deadline time.Time, hasDeadline bool) *time.Duration {
	if timeout == nativeUnbounded {
		return nil
	}
	if !hasDeadline {
		budget := timeout
		return &budget
	}
	remaining := time.Until(deadline)
	if remaining < 100*time.Millisecond {
		remaining = 100 * time.Millisecond
	}
	return &remaining
}

// request sends one frame and waits for the matching response: same id (Python
// equality: 1 == 1.0, true == 1) and no truthy method. An error body is
// classified by the host hook or embedded verbatim with Python's default
// separators and the inbound key order.
func (c *nativeClient) request(method string, params ordObj, timeout ...time.Duration) (any, error) {
	budget := c.hooks.defaultTimeoutOr()
	if len(timeout) > 0 {
		budget = timeout[0]
	}
	// The ident is either the host's own hook or the base class's "native-<n>"
	// string default; never both, or the counter would advance twice.
	var ident any
	if c.hooks.identify != nil {
		ident = c.hooks.identify(c)
	} else {
		ident = fmt.Sprintf("native-%d", c.nextCounter())
	}
	if err := c.send(c.frame(ident, method, params)); err != nil {
		return nil, err
	}
	var deadline time.Time
	hasDeadline := budget != nativeUnbounded && c.hooks.bounded
	if hasDeadline {
		deadline = time.Now().Add(budget)
	}
	for {
		// A bounded request must not outlive its total budget: continuous
		// notifications keep the queue non-empty, so per-message waits alone
		// would never starve and a late response would still be accepted.
		if hasDeadline && !time.Now().Before(deadline) {
			return nil, c.timeoutError(method)
		}
		message, err := c.receive(nativeWaitDuration(budget, deadline, hasDeadline))
		if err != nil {
			if errors.Is(err, errNativeTimedOut) {
				return nil, c.timeoutError(method)
			}
			return nil, err
		}
		object, isObject := message.(ordObj)
		if !isObject || !pyEquals(getOf(object, "id"), ident) || pyTruthy(getOf(object, "method")) {
			continue
		}
		if errorValue := getOf(object, "error"); pyTruthy(errorValue) {
			var specific error
			if c.hooks.classify != nil {
				specific = c.hooks.classify(method, errorValue, params)
			}
			if specific != nil {
				return nil, specific
			}
			embedded, err := pyDumps(errorValue)
			if err != nil {
				return nil, err
			}
			return nil, &pyRuntimeBlocked{message: fmt.Sprintf("%s %s: %s", c.hooks.requestPrefix, method, embedded)}
		}
		if result, present := object.Get("result"); present {
			return result, nil
		}
		return dict(), nil
	}
}

// blocked prefixes a transport failure so evidence names the transport.
func (c *nativeClient) blocked(text string) error {
	message := text
	if c.hooks.transportPrefix != "" {
		message = c.hooks.transportPrefix + ": " + text
	}
	return &pyRuntimeBlocked{message: message}
}

// timeoutError is the observable per-message/total timeout text.
func (c *nativeClient) timeoutError(method string) error {
	return &pyRuntimeBlocked{message: fmt.Sprintf("%s_TIMEOUT: %s", c.hooks.requestPrefix, method)}
}

// close ends the transport: close stdin, give the host 8s to exit on EOF,
// then SIGTERM with 5s, then SIGKILL; join the read loop (2s) and close the
// evidence file handles. It is idempotent and never fails the caller.
func (c *nativeClient) close() {
	c.closeOnce.Do(func() {
		c.stdin.Close()
		waited := make(chan struct{}, 1)
		go func() {
			c.cmd.Wait()
			waited <- struct{}{}
		}()
		select {
		case <-waited:
		case <-time.After(8 * time.Second):
			_ = c.cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-waited:
			case <-time.After(5 * time.Second):
				_ = c.cmd.Process.Kill()
				<-waited
			}
		}
		select {
		case <-c.readerDone:
		case <-time.After(2 * time.Second):
		}
		c.traceFile.Close()
		c.errorsFile.Close()
	})
}

// closeNativeSession asks the host to close a session; a failed close never
// hides a real result, so every error is swallowed
// (native_transport.close_session).
func closeNativeSession(client *nativeClient, identity string) {
	if identity == "" {
		return
	}
	_, _ = client.request(client.hooks.closeMethodOr(), dict("sessionId", identity), 10*time.Second)
}

// readNativeLine reads one universal-newline line the way Python's text-mode
// iteration does: \n, \r\n and a lone \r all end lines. A final unterminated
// line is still returned, with io.EOF as the error.
func readNativeLine(reader *bufio.Reader) (string, error) {
	var line []byte
	for {
		ch, err := reader.ReadByte()
		if err != nil {
			return string(line), err
		}
		if ch == '\n' {
			return string(line), nil
		}
		if ch == '\r' {
			// \r\n is one terminator; a lone \r is a terminator in its own
			// right, so a following non-\n byte belongs to the next line.
			if next, readErr := reader.ReadByte(); readErr == nil && next != '\n' {
				reader.UnreadByte()
			}
			return string(line), nil
		}
		line = append(line, ch)
	}
}

// nativeQueue is an unbounded FIFO: the host may emit arbitrarily many
// notifications while a response is pending, and Python's queue.Queue never
// drops or blocks a producer.
type nativeQueue struct {
	mu     sync.Mutex
	items  []any
	notify chan struct{}
}

func newNativeQueue() *nativeQueue {
	return &nativeQueue{notify: make(chan struct{}, 1)}
}

func (q *nativeQueue) put(value any) {
	q.mu.Lock()
	q.items = append(q.items, value)
	q.mu.Unlock()
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

func (q *nativeQueue) get(wait *time.Duration) (any, bool) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			value := q.items[0]
			q.items = q.items[1:]
			q.mu.Unlock()
			return value, true
		}
		q.mu.Unlock()
		if wait == nil {
			<-q.notify
			continue
		}
		select {
		case <-q.notify:
		case <-time.After(*wait):
			return nil, false
		}
	}
}
