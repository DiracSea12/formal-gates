package harness

// The shared mechanics every native host transport uses. A native
// transport is always the
// same machine: spawn one host process, speak line-delimited JSON over its
// stdio, keep a routing-only trace, answer the host's own client-directed
// requests and write the same four evidence files. Only the launch arguments
// and the protocol sequence belong to an adapter, supplied as plain hooks. No
// interface, no registry,
// no assembly injection: the adapter and the transport live in this one
// package.
//
// Every JSON surface this file touches is canonical (sorted keys, compact):
// everything that lands on disk goes through the single atomic writer, and
// the outbound protocol lines and stdout use CanonicalJSON directly. Consumers
// on both sides parse by value.
//
// Decoded values are plain Go values (map[string]any, []any, string, bool,
// nil) with number literals kept as json.Number so the canonical encoder can
// re-render them exactly. Decision points state their business meaning
// explicitly (an integer turn counter, a non-empty session id, a binding with
// entries) rather than emulating a Python runtime.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
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

// --- typed errors (the reference exception families) ---------------------------

// bindingError is the refusal family for binding-class failures (the reference
// implementation's HarnessError): the harness cannot even start the work.
type bindingError struct{ message string }

func (e *bindingError) Error() string { return e.message }

// blockedError is the blocked-run refusal family (the reference RuntimeBlocked).
type blockedError struct{ message string }

func (e *blockedError) Error() string { return e.message }

// sessionUnavailableError is the subclass of blockedError for "the host
// explicitly rejected the persistent session identity". Identity and code keep
// the host's original JSON values (number, string or null) verbatim.
type sessionUnavailableError struct {
	blockedError
	identity any
	method   string
	code     any
}

// duplicateKeyError is a repeated object key in a document decoded with
// infra.read semantics.
type duplicateKeyError struct{ key string }

func (e *duplicateKeyError) Error() string { return "duplicate JSON field: " + e.key }

// nativeErrorText is the text the native error file and stderr carry: the
// reference exception family name once, then the message, so the Python driver
// can strip the prefix (its _message_without_type_prefix). A plain Go error -
// a malformed request, an unreadable file, a failed spawn - has no family and
// is recorded as its own text.
func nativeErrorText(err error) string {
	switch typed := err.(type) {
	case *sessionUnavailableError:
		return "SessionUnavailable: " + typed.message
	case *blockedError:
		return "RuntimeBlocked: " + typed.message
	case *bindingError:
		return "HarnessError: " + typed.message
	default:
		return err.Error()
	}
}

// --- JSON decoding and decoded-value access -----------------------------------

// readJSONFile reads and decodes one evidence/request file (infra.read): a
// repeated object key is refused, never silently collapsed.
func readJSONFile(path string) (any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	value, err := decodeUniqueKeys(data)
	if err != nil {
		var duplicate *duplicateKeyError
		if errors.As(err, &duplicate) {
			return nil, &bindingError{message: err.Error()}
		}
		return nil, fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	return value, nil
}

// decodeNativeJSON decodes one JSON document: number literals keep their text
// (json.Number) so the canonical encoder re-renders them exactly, and
// anything after the document is an error.
func decodeNativeJSON(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if err := expectDocumentEnd(decoder); err != nil {
		return nil, err
	}
	return value, nil
}

// decodeUniqueKeys is decodeNativeJSON with infra.read semantics: a repeated
// object key is refused instead of collapsed.
func decodeUniqueKeys(raw []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	value, err := decodeUniqueValue(decoder)
	if err != nil {
		return nil, err
	}
	if err := expectDocumentEnd(decoder); err != nil {
		return nil, err
	}
	return value, nil
}

func expectDocumentEnd(decoder *json.Decoder) error {
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return errors.New("extra data after the JSON document")
	}
	return nil
}

// decodeUniqueValue walks one decoder token stream building plain values and
// refusing repeated object keys.
func decodeUniqueValue(decoder *json.Decoder) (any, error) {
	token, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	delim, isDelim := token.(json.Delim)
	if !isDelim {
		return token, nil // string, json.Number, bool or nil
	}
	switch delim {
	case '{':
		object := map[string]any{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyToken.(string)
			if !ok {
				return nil, fmt.Errorf("invalid object key %v", keyToken)
			}
			if _, exists := object[key]; exists {
				return nil, &duplicateKeyError{key: key}
			}
			value, err := decodeUniqueValue(decoder)
			if err != nil {
				return nil, err
			}
			object[key] = value
		}
		if _, err := decoder.Token(); err != nil { // consume '}'
			return nil, err
		}
		return object, nil
	case '[':
		array := []any{}
		for decoder.More() {
			item, err := decodeUniqueValue(decoder)
			if err != nil {
				return nil, err
			}
			array = append(array, item)
		}
		if _, err := decoder.Token(); err != nil { // consume ']'
			return nil, err
		}
		return array, nil
	}
	return nil, fmt.Errorf("unexpected delimiter %v", delim)
}

// entryOf is v[key] over a decoded object; the second result reports presence
// (null counts).
func entryOf(v any, key string) (any, bool) {
	if object, ok := v.(map[string]any); ok {
		value, present := object[key]
		return value, present
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

// mustEntry is a required field of a decoded object; a missing key is a
// malformed document, not a business refusal.
func mustEntry(v any, key string) (any, error) {
	if value, ok := entryOf(v, key); ok {
		return value, nil
	}
	return nil, fmt.Errorf("missing JSON field %q", key)
}

// listOf is v as an array; anything else (absent included) is empty.
func listOf(v any) []any {
	if list, ok := v.([]any); ok {
		return list
	}
	return nil
}

// objectOrEmpty coerces a decoded value to an object: anything but an object
// (absent and null included) is empty. The live host always answers objects
// where this is used.
func objectOrEmpty(v any) map[string]any {
	if object, ok := v.(map[string]any); ok {
		return object
	}
	return map[string]any{}
}

// textOf renders a decoded JSON value as text: a string verbatim, every other
// value as its canonical JSON. Error texts embed host-controlled values
// (a stop reason, a turn-end reason), and canonical JSON is this
// implementation's value rendering.
func textOf(v any) string {
	switch typed := v.(type) {
	case nil:
		return "null"
	case string:
		return typed
	default:
		encoded, err := CanonicalJSON(typed)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(encoded)
	}
}

// Equality at decision points is verify.go's valuesEqual: two decoded JSON
// values are equal when they denote the same value, numbers compared exactly
// across representations (this package's int counters and proof sizes meet the
// host's decoded literals). A bool is never equal to a number.

// isIntegerLiteral reports a decoded integer literal of any magnitude. The
// native turn number is an integer counter; a float literal, a string or a
// bool is not a turn.
func isIntegerLiteral(v any) bool {
	number, ok := v.(json.Number)
	return ok && !strings.ContainsAny(number.String(), ".eE")
}

// --- paths ---------------------------------------------------------------------

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

// --- shared evidence helpers (native_transport) --------------------------------

// proofObj shapes one content proof like Python's file_proof:
// {"sha256": ..., "size": ...}.
func proofObj(proof ContentProof) map[string]any {
	return map[string]any{"sha256": proof.SHA256, "size": proof.Size}
}

// fileProofOf is file_proof: the content proof, or a blockedError carrying the
// refusal text for a missing/unreadable file.
func fileProofOf(path string) (map[string]any, error) {
	proof, refusal := FileProof(path)
	if refusal != "" {
		return nil, &blockedError{message: refusal}
	}
	return proofObj(proof), nil
}

// buildCaptureBinding is capture_binding: {'provider', 'launch', 'files':
// {path: proof}} for every executed file the launch names.
func buildCaptureBinding(provider string, launch map[string]any) (map[string]any, error) {
	files := map[string]any{}
	for _, target := range launch {
		path := textOf(target)
		proof, err := fileProofOf(path)
		if err != nil {
			return nil, err
		}
		files[path] = proof
	}
	return map[string]any{"provider": provider, "launch": launch, "files": files}, nil
}

// validateNativeBinding is the proof-checking body behind every adapter's
// validate_binding (dshValidateBinding, zcodeValidateBinding): the provider
// must match the adapter's own name and every bound file proof must still
// hold. Only the provider name differs between the adapters.
func validateNativeBinding(binding any, provider string) error {
	object, ok := binding.(map[string]any)
	if !ok {
		return fmt.Errorf("binding must be a JSON object")
	}
	if !valuesEqual(getOf(object, "provider"), provider) {
		return &bindingError{message: "HOST_BINDING: provider differs"}
	}
	files := objectOrEmpty(getOrOf(object, "files", nil))
	for name, bound := range files {
		proof, err := fileProofOf(textOf(name))
		if err != nil {
			return err
		}
		if !valuesEqual(proof, bound) {
			return &blockedError{
				message: "HOST_CHANGED: bound native executable/script changed: " + textOf(name),
			}
		}
	}
	return nil
}

// verifyNativeSource is the proof-checking body behind every adapter's
// verify_source (dshVerifySource, zcodeVerifySource): provider/identity must
// match, the proof set must be exactly the four evidence files, and each proof
// must still hold against the directory. Only the provider name differs
// between the adapters.
func verifyNativeSource(directory string, source any, provider string) error {
	object, ok := source.(map[string]any)
	if !ok {
		return fmt.Errorf("source must be a JSON object")
	}
	identity, _ := getOf(object, "identity").(string)
	if !valuesEqual(getOf(object, "provider"), provider) || identity == "" {
		return &blockedError{message: "NATIVE_BINDING: missing or different provider/identity"}
	}
	proofs := objectOrEmpty(getOrOf(object, "files", nil))
	if !sameKeySet(proofs, nativeSourceFiles) {
		return &blockedError{message: "EVIDENCE_MISSING: native source proofs absent"}
	}
	for name, bound := range proofs {
		proof, err := fileProofOf(filepath.Join(directory, textOf(name)))
		if err != nil {
			return err
		}
		if !valuesEqual(proof, bound) {
			return &blockedError{message: "EVIDENCE_CHANGED: " + textOf(name)}
		}
	}
	return nil
}

// forEachSessionEvent streams this session's evidence events one line at a
// time, rejecting mixed-session evidence (native_transport.session_events).
// Only one decoded line is alive at a time: native-events.jsonl reaches tens
// of megabytes on a long turn, and the whole turn is never held in memory.
func forEachSessionEvent(directory string, identity any, handle func(record map[string]any) error) error {
	path := filepath.Join(directory, "native-events.jsonl")
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 64*1024)
	for {
		line, readErr := readNativeLine(reader)
		// A yielded line is handled even when it is empty after terminator
		// stripping (an empty line is a non-JSON failure); the empty read at
		// clean EOF is not a line at all.
		if line != "" || readErr == nil {
			if err := handleSessionLine(path, line, identity, handle); err != nil {
				return err
			}
		}
		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				return nil
			}
			return readErr
		}
	}
}

// handleSessionLine decodes one event line and checks its session binding
// before the caller sees it.
func handleSessionLine(path, line string, identity any, handle func(map[string]any) error) error {
	value, err := decodeNativeJSON([]byte(line))
	if err != nil {
		return fmt.Errorf("invalid JSON in %s: %w", path, err)
	}
	record, ok := value.(map[string]any)
	if !ok {
		return fmt.Errorf("invalid event record in %s: not a JSON object", path)
	}
	if !valuesEqual(record["sessionId"], identity) {
		return &blockedError{message: "BINDING: mixed session evidence"}
	}
	return handle(record)
}

// writeNativeAnswer is write_answer: keep the native bytes, then decode them
// into the business result. A saved file is compared after line-ending
// normalisation (CRLF and a lone CR both read as LF), so a byte difference a
// text read cannot observe is not a refusal. A framing refusal is a
// blockedError whose text already carries the NATIVE_RESULT prefix.
func writeNativeAnswer(directory, text, prefix string) error {
	raw := filepath.Join(directory, "answer.raw.txt")
	if _, statErr := os.Stat(raw); statErr == nil {
		body, err := os.ReadFile(raw)
		if err != nil {
			return err
		}
		if normalizeNewlines(string(body)) != text {
			return &blockedError{message: prefix + "_RESULT: saved output differs"}
		}
	} else if os.IsNotExist(statErr) {
		if err := os.WriteFile(raw, []byte(text), 0o644); err != nil {
			return err
		}
	} else {
		return statErr
	}
	result, refusal := FrameResult(text)
	if refusal != "" {
		return &blockedError{message: refusal}
	}
	return writeAtomicJSON(filepath.Join(directory, "answer.json"), result)
}

// normalizeNewlines maps every line ending (LF, CRLF or a lone CR) to LF: the
// saved answer is compared as text, not as bytes.
func normalizeNewlines(text string) string {
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
// caller's extra keys, then proofs for the four evidence files.
func evidenceSource(directory, provider string, identity any, extra map[string]any) (map[string]any, error) {
	receipt := map[string]any{"provider": provider, "identity": identity}
	for key, value := range extra {
		receipt[key] = value
	}
	files := map[string]any{}
	for _, name := range nativeSourceFiles {
		proof, err := fileProofOf(filepath.Join(directory, name))
		if err != nil {
			return nil, err
		}
		files[name] = proof
	}
	receipt["files"] = files
	return receipt, nil
}

// reconcileCompletedOutput recovers only a completed native turn whose
// result framing was refused. The
// original process record is never rewritten; the caller-supplied recover
// re-observes the evidence.
func reconcileCompletedOutput(directory string, process any, prefix string,
	eligible func(original string) bool, recover func() (map[string]any, error)) (map[string]any, error) {
	errorPath := filepath.Join(directory, "native-error.json")
	exists := true
	original := ""
	if _, statErr := os.Stat(errorPath); statErr == nil {
		record, err := readJSONFile(errorPath)
		if err != nil {
			return nil, err
		}
		original, _ = getOrOf(record, "error", "").(string)
	} else if os.IsNotExist(statErr) {
		exists = false
	} else {
		return nil, statErr
	}
	// Only a result-framing failure of an otherwise completed process can be
	// replayed. A runtime or transport failure is never laundered into a result.
	// The process record's error is a text that is empty when the supervisor
	// recorded no message.
	processError, _ := getOf(process, "error").(string)
	if !valuesEqual(getOf(process, "exit_code"), 1) || processError != "" || !exists || !eligible(original) {
		return nil, &blockedError{message: prefix + "_RECONCILE: not a completed formatting-only failure"}
	}
	source, err := recover()
	if err != nil {
		return nil, err
	}
	source["reconciliation"] = map[string]any{
		"kind":               "completed-output-format",
		"original_exit_code": 1,
		"original_error":     original,
		"new_model_calls":    0,
	}
	if err := writeAtomicJSON(filepath.Join(directory, "source.json"), source); err != nil {
		return nil, err
	}
	return source, nil
}

// --- the transport client ------------------------------------------------------

// nativeReply is a client-directed request's answer: the payload plus its
// recorded status.
type nativeReply struct {
	payload map[string]any
	status  string
}

// nativeAnswerChoice is one delivered user decision for a held native
// interaction: a chosen host-offered option id, or a cancel.
type nativeAnswerChoice struct {
	Cancel   bool
	OptionID string
	Answers  any
}

// nativeInteractionSpec is one host's declaration of a client-directed request
// this transport holds for a user decision instead of answering inline:
// each adapter declares only its permission method names and reply-frame
// contract. Every function is
// pure: the spec never touches client state, so the shared mechanics can hold,
// re-announce-track, validate and deliver on any transport.
type nativeInteractionSpec struct {
	// method is the client-directed method name this spec holds.
	method string
	// requestID derives the stable logical identity of the request. DSH's ACP
	// has no id inside the params, so the frame id is it; ZCode re-announces
	// the same request under fresh frame ids, so its requestId param is it.
	requestID func(params map[string]any, frameID any) (string, error)
	// summary renders the request body for the pending listing.
	summary func(params map[string]any) string
	// options returns the host's original options verbatim for the pending
	// record (nil becomes an empty list on disk).
	options func(params map[string]any) any
	// body picks the params members the pending record persists verbatim next
	// to the options, under the host's own field names (the same "host payload
	// verbatim" rule): what the user must see to decide. Present keys only;
	// nil persists nothing beyond the summary and options.
	body func(params map[string]any) map[string]any
	// reply builds the reply payload (result/error members, no envelope, no
	// id) for one validated choice, or refuses a choice the host never
	// offered. The frame shape is each host's own, reverse-read from its
	// protocol implementation and cited in the adapter.
	reply func(params map[string]any, choice nativeAnswerChoice) (map[string]any, error)
}

// spec returns the held-interaction declaration for one method, or nil.
func (h nativeHooks) interactionSpec(method string) *nativeInteractionSpec {
	for i := range h.interactions {
		if h.interactions[i].method == method {
			return &h.interactions[i]
		}
	}
	return nil
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
	// The client-directed requests this transport holds for a user decision
	// instead of answering inline.
	interactions []nativeInteractionSpec
	// observes reports whether one inbound non-request message is
	// attributable to a delivered hold's continuation on this host - the fact
	// that advances a delivered vote to observed. This is host knowledge the
	// shared transport never guesses; nil keeps every hold honestly at
	// delivered. Called with the interaction state locked, so it must not
	// touch client state.
	observes func(hold *nativePendingHold, message map[string]any) bool

	identify     func(c *nativeClient) any
	envelope     func(value map[string]any) map[string]any
	respond      func(method string, params any) (*nativeReply, error)
	classify     func(method string, errValue any, params map[string]any) error
	notification func(message any)
	// received records each original inbound frame, including held requests.
	received func(message any)
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

// nativePendingHold is one held native interaction's live state: the record
// the evidence file carries plus everything a later answer needs to reply in
// the host's own frame shape.
type nativePendingHold struct {
	requestID string
	method    string
	// frameID is the inbound frame id the reply will answer: the latest one
	// seen for this logical request, so a re-announcing host is answered on
	// its freshest frame.
	frameID any
	params  map[string]any
	spec    *nativeInteractionSpec
	status  string // pending | delivered | observed | invalidated
	summary string
	options any
	// body is the spec-picked params payload the pending record persists
	// verbatim (nil: none).
	body map[string]any
	// deliveredArrival is the message-arrival sequence at delivery time:
	// only messages that arrived under a higher sequence are facts after
	// the vote. Zero until delivered.
	deliveredArrival int64
	updatedAt        time.Time
}

// nativeAnswerPollInterval is how often a job process polls the answer file
// while a native interaction is held. A variable only so tests can shorten it.
var nativeAnswerPollInterval = 200 * time.Millisecond

// nativeConnectionGeneration counts native transports this process created:
// the "连接代次" every pending record carries as the label of the connection
// that held it. The answer file carries no generation, so this labels the
// record's connection; it is not a cross-connection isolation guarantee.
var nativeConnectionGeneration int64

// nativeClient is one host process and the JSON request/reply loop over its
// stdio.
type nativeClient struct {
	hooks        nativeHooks
	directory    string
	generation   int
	counter      int
	interactions []any
	messages     *nativeQueue
	errorsFile   *os.File
	traceFile    *os.File
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	readerDone   chan struct{}
	closeOnce    sync.Once
	sendMu       sync.Mutex
	// stateMu guards the held-interaction state and the interactions log.
	stateMu       sync.Mutex
	holdings      []*nativePendingHold
	priorPendings []any
	pendingsRead  bool
	answerPoller  sync.Once
	answerStop    chan struct{}
	waitErr       error
}

// newNativeClient spawns the host process: cwd is the workspace, the
// environment is the caller's own (never trimmed or extended), stderr appends
// to host.stderr, native-process.json is written atomically with the child's
// pid/pgid (pgid is null on Windows; the spawn never creates a new group so
// the pgid is the caller's), and the read loop starts immediately.
func newNativeClient(hooks nativeHooks, argv []string, workspace, directory string) (*nativeClient, error) {
	return newNativeClientWithEnv(hooks, argv, workspace, directory, nil)
}

// newNativeClientWithEnv is newNativeClient with additional launch environment
// variables layered over the caller's own (the Python constructor's environment
// parameter, {**os.environ, **environment}). Adapters pin native configuration
// paths in this environment. Duplicate keys resolve in the adapter's favour,
// like the dict merge.
func newNativeClientWithEnv(hooks nativeHooks, argv []string, workspace, directory string, environment map[string]string) (*nativeClient, error) {
	c := &nativeClient{
		hooks:      hooks,
		directory:  directory,
		generation: int(atomic.AddInt64(&nativeConnectionGeneration, 1)),
		messages:   newNativeQueue(),
		readerDone: make(chan struct{}),
		answerStop: make(chan struct{}),
	}
	var err error
	c.errorsFile, err = os.OpenFile(filepath.Join(directory, "host.stderr"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("cannot open %s: %w", filepath.Join(directory, "host.stderr"), err)
	}
	c.traceFile, err = os.OpenFile(filepath.Join(directory, "protocol.jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		c.errorsFile.Close()
		return nil, fmt.Errorf("cannot open %s: %w", filepath.Join(directory, "protocol.jsonl"), err)
	}
	if workspace != "" {
		info, statErr := os.Stat(workspace)
		if statErr != nil {
			c.errorsFile.Close()
			c.traceFile.Close()
			if os.IsNotExist(statErr) {
				return nil, fmt.Errorf("workspace does not exist: %s", workspace)
			}
			return nil, fmt.Errorf("cannot stat workspace %s: %w", workspace, statErr)
		}
		if !info.IsDir() {
			c.errorsFile.Close()
			c.traceFile.Close()
			return nil, fmt.Errorf("workspace is not a directory: %s", workspace)
		}
	}
	stdinRead, stdinWrite, err := os.Pipe()
	if err != nil {
		c.errorsFile.Close()
		c.traceFile.Close()
		return nil, fmt.Errorf("cannot create the stdio pipes: %w", err)
	}
	stdoutRead, stdoutWrite, err := os.Pipe()
	if err != nil {
		stdinRead.Close()
		stdinWrite.Close()
		c.errorsFile.Close()
		c.traceFile.Close()
		return nil, fmt.Errorf("cannot create the stdio pipes: %w", err)
	}
	c.stdin = stdinWrite
	c.cmd = exec.Command(argv[0], argv[1:]...)
	c.cmd.Dir = workspace
	childEnv := os.Environ()
	for key, value := range environment {
		filtered := childEnv[:0]
		for _, entry := range childEnv {
			if !strings.HasPrefix(entry, key+"=") {
				filtered = append(filtered, entry)
			}
		}
		childEnv = append(filtered, key+"="+value)
	}
	c.cmd.Env = childEnv
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
		return nil, fmt.Errorf("cannot start %s: %w", argv[0], startErr)
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
			// avoids a zombie.
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
	if err := writeAtomicJSON(filepath.Join(directory, "native-process.json"),
		map[string]any{"pid": c.cmd.Process.Pid, "pgid": pgid}); err != nil {
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
		// stripping (an empty line is a non-JSON failure); the empty read at
		// clean EOF is not a line at all.
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
	value, err := decodeNativeJSON([]byte(line))
	if err != nil {
		// A non-JSON line is a transport failure; reading stops here (the
		// deferred EOF closure still lands behind this one).
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
	if object, ok := value.(map[string]any); ok {
		if id, present := object["id"]; present {
			record["id"] = id
		}
		if method, present := object["method"]; present {
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
func (c *nativeClient) closureMessage(reason string) map[string]any {
	return map[string]any{c.hooks.closedKeyOr(): reason}
}

// closureOf is the reason this transport cannot continue, or nil. Keys are
// checked in order: the shared 'closed' key first, then this host's own
// closed_key; either one present with a non-empty reason string in ANY inbound
// message counts.
func (c *nativeClient) closureOf(message map[string]any) string {
	for _, key := range []string{"closed", c.hooks.closedKeyOr()} {
		if reason, ok := message[key].(string); ok && reason != "" {
			return reason
		}
	}
	return ""
}

// send writes one outbound frame as a canonical compact JSON line; the host
// parses by value. The write is serialized: the answer poller delivers a held
// interaction's reply from its own goroutine while the request loop may be
// sending, and two interleaved lines would corrupt the stream.
func (c *nativeClient) send(value map[string]any) error {
	encoded, err := CanonicalJSON(value)
	if err != nil {
		return err
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if _, err := c.stdin.Write(append(encoded, '\n')); err != nil {
		return fmt.Errorf("cannot write to the host process stdin: %w", err)
	}
	return nil
}

// frame builds {id, method, params} and applies the host envelope.
func (c *nativeClient) frame(ident any, method string, params map[string]any) map[string]any {
	value := map[string]any{"id": ident, "method": method, "params": params}
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
// never matched against a pending ident). A held interaction's request is
// consumed the same way - no frame leaves, the pending record does - and a
// later message advances a delivered interaction to observed only when it
// arrived after the delivery and the host's own rule attributes it to that
// hold (hooks.observes).
func (c *nativeClient) receive(wait *time.Duration) (any, error) {
	message, arrival, ok := c.messages.get(wait)
	if !ok {
		return nil, errNativeTimedOut
	}
	if object, isObject := message.(map[string]any); isObject {
		if reason := c.closureOf(object); reason != "" {
			// The connection the pending requests belonged to is gone: they
			// are invalidated before the closure propagates, so the old
			// option ids stop being authorized answers.
			c.invalidatePendings()
			return nil, c.blocked(reason)
		}
		if c.hooks.received != nil {
			c.hooks.received(message)
		}
		if method, hasMethod := object["method"]; hasMethod && isLiveMethod(method) {
			if _, hasID := object["id"]; hasID {
				if err := c.answerReverse(object); err != nil {
					return nil, err
				}
				return message, nil
			}
		}
		// Everything that is not a reverse request goes to the notification
		// hook: notifications (a live method without an id) and plain
		// responses alike, exactly like the reference receive()'s else branch.
		c.observePendings(object, arrival)
		if c.hooks.notification != nil {
			c.hooks.notification(message)
		}
	}
	return message, nil
}

// isLiveMethod reports a method value that names a client-directed request: a
// non-empty string. An absent, null, empty or otherwise inert method makes the
// line a plain response.
func isLiveMethod(v any) bool {
	text, ok := v.(string)
	return ok && text != ""
}

// answerReverse replies to a host request and records what was answered. A
// method one of this transport's interaction specs claims is held for a user
// decision instead: no frame leaves now, the pending record lands, and
// the answer poller delivers the user's reply later under the original
// request id. A respond hook error (a required user decision) propagates
// without any reply; a nil reply is the base class's default refusal.
func (c *nativeClient) answerReverse(message map[string]any) error {
	method := textOf(message["method"])
	params := objectOrEmpty(message["params"])
	if spec := c.hooks.interactionSpec(method); spec != nil {
		return c.holdInteraction(spec, message, params)
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
			payload: map[string]any{"error": map[string]any{
				"code":    nativeUnsupportedMethod,
				"message": "Client method not supported"}},
			status: "unsupported-method",
		}
	}
	frame := map[string]any{"id": message["id"]}
	for key, value := range reply.payload {
		frame[key] = value
	}
	if c.hooks.envelope != nil {
		frame = c.hooks.envelope(frame)
	}
	if err := c.send(frame); err != nil {
		return err
	}
	c.appendInteraction(map[string]any{"method": method, "status": reply.status})
	return nil
}

// --- held native interactions -------------------------------------------------

// nativeSerializeID renders a request identity as the stable text the pending
// record and the answer file share: a string id verbatim (ZCode's requestId
// strings stay CLI-friendly), any other decoded value as its canonical JSON
// (DSH's integer frame ids). A number 3 and a string "3" render the same text
// and cannot meet: one job speaks one host, whose ids are one kind.
func nativeSerializeID(value any) string {
	if text, ok := value.(string); ok {
		return text
	}
	if encoded, err := CanonicalJSON(value); err == nil {
		return string(encoded)
	}
	return fmt.Sprintf("%v", value)
}

// holdInteraction records one client-directed request this transport holds for
// a user decision: the pending file gains the request (status pending), the
// interactions log gains its pending entry, the answer poller starts, and no
// reply frame is sent. A re-announced request (same logical id, fresh frame
// id) only re-aims the frame the eventual reply will answer.
func (c *nativeClient) holdInteraction(spec *nativeInteractionSpec, message map[string]any, params map[string]any) error {
	frameID, hasFrameID := message["id"]
	if !hasFrameID {
		frameID = nil
	}
	requestID, err := spec.requestID(params, frameID)
	if err != nil {
		return err
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	if hold := c.holdByIDLocked(requestID); hold != nil {
		hold.frameID = frameID
		hold.updatedAt = time.Now()
		return c.writePendingsLocked()
	}
	options := spec.options(params)
	if options == nil {
		options = []any{}
	}
	var body map[string]any
	if spec.body != nil {
		body = spec.body(params)
	}
	hold := &nativePendingHold{
		requestID: requestID,
		method:    textOf(message["method"]),
		frameID:   frameID,
		params:    params,
		spec:      spec,
		status:    "pending",
		summary:   spec.summary(params),
		options:   options,
		body:      body,
		updatedAt: time.Now(),
	}
	c.holdings = append(c.holdings, hold)
	if err := c.writePendingsLocked(); err != nil {
		return err
	}
	if err := c.appendInteractionLocked(map[string]any{
		"request_id": requestID, "method": hold.method, "status": "pending"}); err != nil {
		return err
	}
	// One poller per transport, started when there is finally something to
	// poll for; close stops it.
	c.answerPoller.Do(func() { go c.pollAnswers() })
	return nil
}

// holdByIDLocked finds this connection's hold for one logical request id.
func (c *nativeClient) holdByIDLocked(requestID string) *nativePendingHold {
	for _, hold := range c.holdings {
		if hold.requestID == requestID {
			return hold
		}
	}
	return nil
}

// pendingHeld reports whether any held interaction of this connection is still
// waiting for a user decision (the state a turn budget must pause for).
func (c *nativeClient) pendingHeld() bool {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	for _, hold := range c.holdings {
		if hold.status == "pending" {
			return true
		}
	}
	return false
}

// pollAnswers polls the answer file until the transport closes. The wait path
// itself never blocks on the poll: a host that sits silent waiting for its
// permission reply (DSH's ACP has no re-announce) still gets its answer, because
// the poller delivers it from here.
func (c *nativeClient) pollAnswers() {
	ticker := time.NewTicker(nativeAnswerPollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-c.answerStop:
			return
		case <-ticker.C:
			for {
				if !c.deliverOneAnswer() {
					break
				}
			}
		}
	}
}

// deliverOneAnswer consumes at most one answer file and delivers it; false
// means there was nothing (more) to consume.
func (c *nativeClient) deliverOneAnswer() bool {
	answerPath := filepath.Join(c.directory, "interaction-answer.json")
	consumedPath := filepath.Join(c.directory, "interaction-answer.consumed.json")
	// Consume by rename first: the decision is persisted in the answer file,
	// and the rename makes one answer deliverable exactly once even if the
	// poll tick races a second file.
	if err := os.Rename(answerPath, consumedPath); err != nil {
		return false
	}
	body, err := os.ReadFile(consumedPath)
	if err != nil {
		return true
	}
	value, err := decodeUniqueKeys(body)
	if err != nil {
		c.stateMu.Lock()
		c.appendInteractionLocked(map[string]any{
			"method": "native-answer", "status": "answer-refused-malformed"})
		c.stateMu.Unlock()
		return true
	}
	record, isObject := value.(map[string]any)
	if !isObject {
		c.stateMu.Lock()
		c.appendInteractionLocked(map[string]any{
			"method": "native-answer", "status": "answer-refused-malformed"})
		c.stateMu.Unlock()
		return true
	}
	requestID, _ := getOrOf(record, "request_id", "").(string)
	option, _ := getOrOf(record, "option", "").(string)
	choice := nativeAnswerChoice{Cancel: getOf(record, "cancel") == true, OptionID: option, Answers: getOf(record, "answers")}

	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	hold := c.holdByIDLocked(requestID)
	if hold == nil || hold.status != "pending" {
		// An answer names a request this connection never held, or one that
		// already got its single vote: refused, recorded, and the wait for the
		// real user decision continues.
		c.appendInteractionLocked(map[string]any{
			"request_id": requestID, "method": "native-answer",
			"status": "answer-refused-no-pending-request"})
		return true
	}
	payload, err := hold.spec.reply(hold.params, choice)
	if err != nil {
		c.appendInteractionLocked(map[string]any{
			"request_id": requestID, "method": hold.method,
			"status": "answer-refused-invalid-choice"})
		return true
	}
	frame := map[string]any{"id": hold.frameID}
	for key, value := range payload {
		frame[key] = value
	}
	if c.hooks.envelope != nil {
		frame = c.hooks.envelope(frame)
	}
	if err := c.send(frame); err != nil {
		// The reply could not be delivered: the transport is dying and the
		// next receive reports why. The hold stays pending, so the failure
		// state is visible rather than laundered into a delivered vote.
		c.appendInteractionLocked(map[string]any{
			"request_id": requestID, "method": hold.method,
			"status": "answer-send-failed"})
		return true
	}
	// Fact two of three: the vote was delivered to the original connection.
	// (Fact one was the answer file; fact three - observed - lands when a
	// message the host attributes to the held request arrives afterwards.)
	hold.status = "delivered"
	hold.deliveredArrival = c.messages.arrived()
	hold.updatedAt = time.Now()
	if err := c.writePendingsLocked(); err != nil {
		return true
	}
	c.setInteractionStatusLocked(requestID, "pending", "delivered")
	_ = c.writeInteractionsLocked()
	return true
}

// observePendings advances a delivered interaction to observed when the
// inbound message both arrived after the delivery (a strictly later arrival
// sequence) and is attributable to that hold's continuation by the host's own
// rule (hooks.observes). The shared transport never guesses attribution, and
// without a rule nothing becomes observed. A no-op unless something was
// delivered.
func (c *nativeClient) observePendings(message map[string]any, arrival int64) {
	if c.hooks.observes == nil {
		return
	}
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	changed := false
	for _, hold := range c.holdings {
		if hold.status == "delivered" && arrival > hold.deliveredArrival &&
			c.hooks.observes(hold, message) {
			hold.status = "observed"
			hold.updatedAt = time.Now()
			c.setInteractionStatusLocked(hold.requestID, "delivered", "observed")
			changed = true
		}
	}
	if changed {
		_ = c.writePendingsLocked()
		_ = c.writeInteractionsLocked()
	}
}

// invalidatePendings invalidates every still-pending interaction: the
// connection its request belonged to is gone (output closed, host timed out,
// transport closed), so the host will never read a reply and the old option
// ids are no longer authorized answers. Delivered and observed records keep
// their status - those votes did reach their connection.
func (c *nativeClient) invalidatePendings() {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	changed := false
	for _, hold := range c.holdings {
		if hold.status == "pending" {
			hold.status = "invalidated"
			hold.updatedAt = time.Now()
			c.setInteractionStatusLocked(hold.requestID, "pending", "invalidated")
			changed = true
		}
	}
	if changed {
		_ = c.writePendingsLocked()
		_ = c.writeInteractionsLocked()
	}
}

// appendInteraction records one inline-answered reverse request (the frozen
// {method, status} shape the reference transport writes).
func (c *nativeClient) appendInteraction(entry map[string]any) {
	c.stateMu.Lock()
	defer c.stateMu.Unlock()
	c.appendInteractionLocked(entry)
}

func (c *nativeClient) appendInteractionLocked(entry map[string]any) error {
	c.interactions = append(c.interactions, entry)
	return writeAtomicJSON(filepath.Join(c.directory, "interaction-status.json"), c.interactions)
}

// setInteractionStatusLocked advances one held request's lifecycle entry in
// this process's interactions log (the pending path's extension of the
// mechanism: one entry per request, advanced through its lifecycle). Only an
// entry in the expected prior stage matches, so a refused answer recorded for
// the same request id is never rewritten into a lifecycle status.
func (c *nativeClient) setInteractionStatusLocked(requestID, prior, status string) {
	for _, entry := range c.interactions {
		object, isObject := entry.(map[string]any)
		if !isObject || !valuesEqual(object["request_id"], requestID) {
			continue
		}
		if valuesEqual(object["status"], prior) {
			object["status"] = status
		}
	}
}

// pendingRecord is the on-disk shape of one held interaction
// (interaction-pending.json): the stable request id, the method, the in-process
// connection generation, a body summary, the host's original options verbatim,
// the host payload members the spec's body picks (also verbatim, under the
// host's own field names), the lifecycle status and the last update time.
func (h *nativePendingHold) pendingRecord(generation int) map[string]any {
	options := h.options
	if options == nil {
		options = []any{}
	}
	record := map[string]any{
		"request_id": h.requestID,
		"method":     h.method,
		"connection": generation,
		"summary":    h.summary,
		"options":    options,
		"status":     h.status,
		"updated_at": h.updatedAt.UTC().Format(time.RFC3339),
	}
	for key, value := range h.body {
		record[key] = value
	}
	return record
}

// nativePickParams picks the named members out of one request's params,
// present keys only: the host payload a spec's body persists verbatim, under
// the host's own field names and never renamed or invented here.
func nativePickParams(params map[string]any, keys ...string) map[string]any {
	picked := map[string]any{}
	for _, key := range keys {
		if _, present := entryOf(params, key); present {
			picked[key] = params[key]
		}
	}
	return picked
}

// writePendingsLocked rewrites interaction-pending.json: the records earlier
// processes left behind stay as evidence, this connection's holds follow in
// their current state.
func (c *nativeClient) writePendingsLocked() error {
	if !c.pendingsRead {
		c.pendingsRead = true
		if value, err := readJSONFile(filepath.Join(c.directory, "interaction-pending.json")); err == nil {
			if records, isList := value.([]any); isList {
				c.priorPendings = records
			}
		}
	}
	records := make([]any, 0, len(c.priorPendings)+len(c.holdings))
	records = append(records, c.priorPendings...)
	for _, hold := range c.holdings {
		records = append(records, hold.pendingRecord(c.generation))
	}
	return writeAtomicJSON(filepath.Join(c.directory, "interaction-pending.json"), records)
}

// writeInteractionsLocked flushes the interactions log (kept for symmetry with
// the append path; both write the same file).
func (c *nativeClient) writeInteractionsLocked() error {
	return writeAtomicJSON(filepath.Join(c.directory, "interaction-status.json"), c.interactions)
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

// request sends one frame and waits for the matching response: the same id
// (numbers compare numerically: this package's int counter meets the host's
// echoed literal) and no live method. An error body is classified by the host
// hook or embedded verbatim as canonical JSON.
func (c *nativeClient) request(method string, params map[string]any, timeout ...time.Duration) (any, error) {
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
		object, isObject := message.(map[string]any)
		if !isObject || !valuesEqual(object["id"], ident) || isLiveMethod(object["method"]) {
			continue
		}
		// JSON-RPC: an error member that is present and non-null is an error
		// response, whatever its (degenerate) body.
		if errorValue, present := object["error"]; present && errorValue != nil {
			var specific error
			if c.hooks.classify != nil {
				specific = c.hooks.classify(method, errorValue, params)
			}
			if specific != nil {
				return nil, specific
			}
			embedded, err := CanonicalJSON(errorValue)
			if err != nil {
				return nil, err
			}
			return nil, &blockedError{message: fmt.Sprintf(
				"%s %s: %s", c.hooks.requestPrefix, method, string(embedded))}
		}
		if result, present := object["result"]; present {
			return result, nil
		}
		return map[string]any{}, nil
	}
}

// blocked prefixes a transport failure so evidence names the transport.
func (c *nativeClient) blocked(text string) error {
	message := text
	if c.hooks.transportPrefix != "" {
		message = c.hooks.transportPrefix + ": " + text
	}
	return &blockedError{message: message}
}

// timeoutError is the observable per-message/total timeout text.
func (c *nativeClient) timeoutError(method string) error {
	return &blockedError{message: fmt.Sprintf("%s_TIMEOUT: %s", c.hooks.requestPrefix, method)}
}

// close ends the transport: close stdin, give the host 8s to exit on EOF,
// then SIGTERM with 5s, then SIGKILL; join the read loop (2s) and close the
// evidence file handles. It is idempotent and returns the original Wait error.
// Held interactions that never received a user decision are invalidated here too:
// their connection is gone, so their old option ids stop being authorized.
func (c *nativeClient) close() error {
	c.closeOnce.Do(func() {
		close(c.answerStop)
		c.invalidatePendings()
		c.stdin.Close()
		waited := make(chan error, 1)
		go func() { waited <- c.cmd.Wait() }()
		select {
		case c.waitErr = <-waited:
		case <-time.After(8 * time.Second):
			_ = c.cmd.Process.Signal(syscall.SIGTERM)
			select {
			case c.waitErr = <-waited:
			case <-time.After(5 * time.Second):
				_ = c.cmd.Process.Kill()
				c.waitErr = <-waited
			}
		}
		select {
		case <-c.readerDone:
		case <-time.After(2 * time.Second):
		}
		c.traceFile.Close()
		c.errorsFile.Close()
	})
	return c.waitErr
}

// closeNativeSession asks the host to close a session; a failed close never
// hides a real result, so every error is swallowed
// (native_transport.close_session).
func closeNativeSession(client *nativeClient, identity string) {
	if identity == "" {
		return
	}
	_, _ = client.request(client.hooks.closeMethodOr(), map[string]any{"sessionId": identity}, 10*time.Second)
}

// readNativeLine reads one universal-newline line the way a text-mode reader
// does: \n, \r\n and a lone \r all end lines. A final unterminated line is
// still returned, with io.EOF as the error.
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
// drops or blocks a producer. Every item is stamped with the sequence it
// arrived under, so a consumer can tell messages that arrived after a given
// moment from ones already queued before it.
type nativeQueue struct {
	mu     sync.Mutex
	items  []nativeQueueItem
	notify chan struct{}
	seq    int64
}

// nativeQueueItem is one queued message with its arrival sequence.
type nativeQueueItem struct {
	value any
	seq   int64
}

func newNativeQueue() *nativeQueue {
	return &nativeQueue{notify: make(chan struct{}, 1)}
}

func (q *nativeQueue) put(value any) {
	q.mu.Lock()
	q.seq++
	q.items = append(q.items, nativeQueueItem{value: value, seq: q.seq})
	q.mu.Unlock()
	select {
	case q.notify <- struct{}{}:
	default:
	}
}

// arrived is the sequence of the newest message so far: a message queued
// under this number was already queued (or arriving with it) when the caller
// looked, and only a higher sequence is strictly later.
func (q *nativeQueue) arrived() int64 {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.seq
}

func (q *nativeQueue) get(wait *time.Duration) (any, int64, bool) {
	for {
		q.mu.Lock()
		if len(q.items) > 0 {
			item := q.items[0]
			q.items = q.items[1:]
			q.mu.Unlock()
			return item.value, item.seq, true
		}
		q.mu.Unlock()
		if wait == nil {
			<-q.notify
			continue
		}
		select {
		case <-q.notify:
		case <-time.After(*wait):
			return nil, 0, false
		}
	}
}
