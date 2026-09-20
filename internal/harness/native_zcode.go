package harness

// The ZCode app-server adapter, the port of
// formal_gates_harness/native_zcode.py. Only the ZCode protocol lives here;
// process handling, the request/reply loop, bindings and evidence come from
// native_transport. No graph imports, workflow callbacks, global configuration
// writes or model substitution. Only read-only roles narrow native tools.
// Terminals require exact session/input/turn IDs.
//
// The app-server speaks "ZCode Protocol v1". It applies no selection of its
// own: a client names an exact provider/model reference and reads the result
// back.
//
// A launcher that starts the app-server itself is also the side that has to
// hand over the host's account provider configuration, because the agent keeps
// no registry of its own: without it the app-server reports an empty provider
// list and no current model at all. zcodeAccountProviderConfiguration rebuilds
// that overlay from the host's own provisioning index.
//
// Handing over the overlay is not enough to finish a turn. The same host asks
// its client for the request credential before every model request and
// abandons the turn when none comes back, so respond answers that one
// interaction from the same provisioning index -- see zcodeRequestAuth().
//
// Serialization is value-level (the G2b v2 ruling, no CPython emulation):
// prompt schemas and embedded error bodies are rendered with the canonical
// encoder (prompt objects present additionalProperties last; see
// schemaForPrompt), and the ZCODE_*/RuntimeBlocked:/SessionUnavailable:
// protocol and family prefix texts are kept verbatim.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// App-server has no read-only mode. Its native allowlist also governs MCP;
// arbitrary command/plugin executors cannot enforce this role's write boundary.
var zcodeReadOnlyTools = []string{"Read", "Glob", "Grep", "WebSearch", "WebFetch"}

// zcodeReadOnlyToolsValue is READ_ONLY_TOOLS as an evidence value.
func zcodeReadOnlyToolsValue() []any {
	value := make([]any, len(zcodeReadOnlyTools))
	for index, name := range zcodeReadOnlyTools {
		value[index] = name
	}
	return value
}

// The handshake every snapshot carries. A different one means the app-server
// no longer speaks the protocol this transport was accepted against.
const zcodeProtocolName = "ZCode Protocol"

const zcodeProtocolVersion = 1

// The host's own name for "this session is gone", used on resume and on any
// call against a session the host no longer holds.
const zcodeSessionUnavailable = -32004

const zcodeDefaultScript = "/Applications/ZCode.app/Contents/Resources/glm/zcode.cjs"

// The app-server reads its built-in provider catalogue from a file, or from
// one of two paths it derives from its own script location. A launcher may
// name that file instead; see zcodeBundledProviderCatalogue.
const zcodeBuiltinCatalogueEnv = "ZCODE_BUILTIN_PROVIDER_CONFIG_FILE"

// The app-server's own bootstrap recomputes the variable above from
// ~/.zcode/v2/runtime/provider/... unless this one is named as well, so a
// launcher that wants the agent to resolve a catalogue it named has to name
// both.
const zcodePersonalConfigEnv = "ZCODE_PERSONAL_PROVIDER_CONFIG_FILE"

const zcodePersonalConfig = "provider_config.json"

// The host's own credential store. Its key names are the host's provisioning
// index for account providers and are plaintext; its values are encrypted
// account secrets, read only by zcode_credential.mjs and never recorded.
const zcodeCredentialsFile = "credentials.json"

const zcodeAccountCredentialPrefix = "account-provider:coding-plan:"

const zcodeAccountCredentialSuffix = ":api-key"

const zcodeAccountIdentitySeparator = ":account:"

// Reading one credential out of that store. The host's cipher is local, so
// this needs neither keychain access nor a user decision.
const zcodeCredentialHelper = "zcode_credential.mjs"

const zcodeCredentialHelperEnv = "FORMAL_GATES_ZCODE_CREDENTIALS"

// The host asks its client for request auth before every model request. A
// launcher that started the app-server is that client, and a turn cannot reach
// a provider without an answer. See zcodeRequestAuth.
const zcodeRuntimeHeadersMethod = "interaction/requestProviderRuntimeHeaders"

// The account provider overlay a host delivers once provider/model are ready.
const zcodeAccountConfigMethod = "provider/updateAccountConfig"

const zcodeAccountAccessType = "zhipu-account"

// The host records its own selected plan here, as a family plus a plan kind.
const zcodeSettingFile = "setting.json"

const zcodeSettingFamilyKey = "providerFamilyDomain"

const zcodeSettingPlansKey = "providerFamilyConnectionSelections"

const zcodeAccessFamilyKey = "accountType"

const zcodeAccessModeKey = "mode"

// Result-framing failures only. Runtime and transport failures are never
// laundered into a successful worker result.
const zcodeResultFrameError = "RuntimeBlocked: NATIVE_RESULT:"

// zcodeTurnBudget is the reference implementation's total bound on one turn's
// wait: 900 seconds of host-processing time from the send acknowledgement to
// the terminal event, paused while a user decision is held (G2d). A variable
// only so the bounded-wait tests can shorten it.
var zcodeTurnBudget = 900 * time.Second

// zcodeIsRegularFile mirrors Path.is_file(): follows symlinks, refuses
// directories and anything unreadable.
func zcodeIsRegularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// zcodeExpandUser expands a leading "~" the way Path.expanduser does for the
// paths this adapter reads from configuration.
func zcodeExpandUser(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}

// zcodeContainsString reports exact membership in a string list.
func zcodeContainsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// --- the provider catalogue (contract item 2) ----------------------------------

// zcodeBundledProviderCatalogue names the app-server's bundled provider
// catalogue when the host cannot (bundled_provider_catalogue).
//
// The app-server resolves that catalogue from the script's own directory:
// `<script dir>/provider/zcode-builtin.json`, then a source-tree-relative
// `../../../../../config/provider/zcode-builtin.json`. The 3.12.3 bundle ships
// it at `<Resources>/config/provider/zcode-builtin.json`, which neither
// candidate reaches, so the app-server exits 1 before emitting any protocol.
//
// The host defines ZCODE_BUILTIN_PROVIDER_CONFIG_FILE so a launcher can name
// that file; the host still computes the *active* catalogue it maintains and
// refreshes itself, and takes ours as the bundled seed. It is returned only
// when the host's own candidates are absent, so a host whose lookup already
// works keeps its current behaviour unchanged. Nothing is written anywhere.
func zcodeBundledProviderCatalogue(script string) string {
	directory := filepath.Dir(script)
	for _, candidate := range []string{
		filepath.Join(directory, "provider", "zcode-builtin.json"),
		filepath.Join(directory, "..", "..", "..", "..", "..", "config", "provider", "zcode-builtin.json"),
	} {
		if zcodeIsRegularFile(candidate) {
			return ""
		}
	}
	resources := filepath.Join(filepath.Dir(directory), "config", "provider", "zcode-builtin.json")
	if zcodeIsRegularFile(resources) {
		return resources
	}
	return ""
}

// zcodeHostDataDirectory is the base directory the host resolves its own
// configuration against (host_data_directory).
func zcodeHostDataDirectory() string {
	if configured := strings.TrimSpace(os.Getenv("ZCODE_DATA_BASE_DIR")); configured != "" {
		return configured
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return ""
}

// zcodeProviderEnvironment names the provider configuration variables a
// launcher must name together (provider_environment). Returns nil when the
// host resolves its own catalogue, so such a host keeps its current behaviour.
func zcodeProviderEnvironment(script string) map[string]string {
	catalogue := zcodeBundledProviderCatalogue(script)
	if catalogue == "" {
		return nil
	}
	personal := filepath.Join(zcodeHostDataDirectory(), ".zcode", "v2", zcodePersonalConfig)
	return map[string]string{
		zcodeBuiltinCatalogueEnv: catalogue,
		zcodePersonalConfigEnv:   personal,
	}
}

// zcodePinnedCatalogue is the catalogue a launcher pinned this binding to, or
// "" for a host's own (pinned_catalogue).
func zcodePinnedCatalogue(binding map[string]any) string {
	environment := objectOrEmpty(getOf(binding, "environment"))
	catalogue, _ := getOrOf(environment, zcodeBuiltinCatalogueEnv, "").(string)
	return catalogue
}

// --- capture, credentials and the client hooks (contract items 3-5) -----------

// zcodeCaptureBinding binds zcode to the node binary and the app-server script
// it will execute (capture_binding). The catalogue is host configuration the
// host refreshes on its own, so it is named in the launch environment but not
// proofed like an executed binary.
func zcodeCaptureBinding() (map[string]any, error) {
	node, lookErr := exec.LookPath("node")
	script, hasScript := os.LookupEnv("FORMAL_GATES_ZCODE_SCRIPT")
	if !hasScript {
		script = zcodeDefaultScript
	}
	script = resolveNativePath(zcodeExpandUser(script))
	if lookErr != nil || !zcodeIsRegularFile(script) {
		return nil, &blockedError{message: "ZCODE_UNAVAILABLE: node or app-server script missing"}
	}
	launch := map[string]any{"executable": resolveNativePath(node), "script": script}
	binding, err := buildCaptureBinding("zcode", launch)
	if err != nil {
		return nil, err
	}
	if environment := zcodeProviderEnvironment(script); environment != nil {
		bound := map[string]any{}
		for key, value := range environment {
			bound[key] = value
		}
		binding["environment"] = bound
	}
	return binding, nil
}

// zcodeLaunchEnvironment is the bound launch environment (launch_environment);
// zcode passes no extra child variables.
func zcodeLaunchEnvironment(binding map[string]any) map[string]string {
	environment := map[string]string{}
	for key, value := range objectOrEmpty(getOf(binding, "environment")) {
		if text, isString := value.(string); isString {
			environment[key] = text
		}
	}
	return environment
}

// zcodeCredentialStore is the host's own account credential store, as the host
// resolves it (credential_store).
func zcodeCredentialStore() string {
	if override := strings.TrimSpace(os.Getenv(zcodeCredentialHelperEnv)); override != "" {
		return override
	}
	return filepath.Join(zcodeHostDataDirectory(), ".zcode", "v2", zcodeCredentialsFile)
}

// zcodeCredentialHelperPath locates the host-side credential helper next to
// the production harness package (Path(__file__).with_name).
func zcodeCredentialHelperPath() (string, error) {
	root, err := dshHarnessRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "runtime", "langgraph", "formal_gates_harness", zcodeCredentialHelper), nil
}

// zcodeRequestAuth is the request credential the host asks its client for, or
// "" (request_auth).
//
// The app-server holds no account credential it can spend on its own: in host
// mode its provider runtime headers port reports
// shouldRefreshBeforeModelRequest() === true unconditionally, so before every
// model request it asks the client for the request auth and abandons the turn
// when the reply carries none. A launcher that spawned the app-server is that
// client, so supplying the credential the host itself provisioned is part of
// starting it, not a substitution of its choice.
//
// Only this account's own provisioned value is read, through the host's
// cipher, which is local (see zcode_credential.mjs): no keychain access and no
// user decision are involved. The value is never returned into evidence --
// answers go through the reply loop, which records the method and the status
// only -- and is never logged, written or reported by this transport.
func zcodeRequestAuth(provider string) string {
	if provider == "" {
		return ""
	}
	node, lookErr := exec.LookPath("node")
	if lookErr != nil {
		return ""
	}
	helper, helperErr := zcodeCredentialHelperPath()
	if helperErr != nil || !zcodeIsRegularFile(helper) {
		return ""
	}
	var stdout bytes.Buffer
	cmd := exec.Command(node, helper, provider, zcodeCredentialStore())
	cmd.Stdout = &stdout
	// stderr is captured and dropped by the reference; it never carries the
	// secret and is never recorded here either.
	if err := cmd.Start(); err != nil {
		return ""
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		// The reference's 60-second subprocess timeout: killed and answered
		// with nothing, never a partial credential.
		_ = cmd.Process.Kill()
		<-done
		return ""
	}
	return strings.TrimSpace(stdout.String())
}

// zcodeClient is ZCode's adapter state around the shared transport: the bound
// session identity and the events its notification hook committed (the Python
// Client's identity and events attributes). The events slice is a receive
// buffer, not a transcript: the execution loop clears each slot as it reads
// the event and resets the drained buffer before waiting for more, and
// native-events.jsonl is the only durable record of the whole turn.
type zcodeClient struct {
	client    *nativeClient
	directory string
	identity  string
	events    []map[string]any
}

// zcodePermissionMethod and zcodeUserInputMethod are the two client-directed
// requests this transport holds for a user decision (G2d): the tool-call
// permission gate and the structured user input (AskUserQuestion, plan-mode
// approval). Neither is ever a process approval.
const zcodePermissionMethod = "interaction/requestPermission"

const zcodeUserInputMethod = "interaction/requestUserInput"

// zcodeInteractionRequestID is the shared request-identity rule for ZCode's
// held interactions: the requestId param. Reverse-read from the app-server
// bundle (/Applications/ZCode.app/Contents/Resources/glm/zcode.cjs, 2026-09
// build): requestClient's re-announce path (bootstrap.zcode_protocol,
// `requestClient(t,n,o,s)`) re-sends an unanswered request on a doubling
// 1s..N timer under a FRESH `server-<n>` frame id with the SAME params, so
// the frame id is not stable; requestId is (the params object is captured
// once), and the host's own interaction registry keys by it.
func zcodeInteractionRequestID(params map[string]any, frameID any) (string, error) {
	if id, isString := getOf(params, "requestId").(string); isString && id != "" {
		return id, nil
	}
	return "", &blockedError{message: "ZCODE_INTERACTION: reverse request carries no requestId"}
}

// zcodePermissionInteraction declares the permission request's frame contract,
// reverse-read from the app-server bundle:
//
//   - Request params (zcode.cjs protocol schema LBi): {requestId, sessionId,
//     turnId?, toolCallId, toolName, reason, riskLevel: low|medium|high|
//     critical, input, origin?, options: [{optionId, kind, name,
//     description?, response}].min(1)} - each option carries its own response,
//     the decision object that option stands for (schema XZe declares
//     response: jL).
//   - Reply (the request's result schema jL, exported as
//     zcodePermissionResponseSchema): a STRICT {decision:
//     allow|deny|escalate|modify, reason?, modifiedInput?,
//     permissionUpdates?} - no optionId, no freeText member exists at this
//     boundary (the optionId/freeText mapping is the host's internal
//     v4AnswerToPermissionResponse for its desktop registry, not the wire).
//     So the protocol-conformant "chose this option" reply is that option's
//     own response object, verbatim.
//   - Cancel: jL has no cancelled member. The protocol's cancellation is the
//     error reply - resolveClientRequest routes an {id, error} frame to
//     rejectClientRequest, and the host's own abort path uses code -32021
//     ("Client request cancelled: <method>"); this reply answers with exactly
//     that code family.
var zcodePermissionInteraction = nativeInteractionSpec{
	method:    zcodePermissionMethod,
	requestID: zcodeInteractionRequestID,
	summary: func(params map[string]any) string {
		toolName := textOf(getOf(params, "toolName"))
		reason := textOf(getOf(params, "reason"))
		if toolName != "" && toolName != "null" && reason != "" && reason != "null" {
			return toolName + ": " + reason
		}
		if reason != "" && reason != "null" {
			return reason
		}
		return "permission request"
	},
	options: func(params map[string]any) any { return getOf(params, "options") },
	body: func(params map[string]any) map[string]any {
		// The operation being approved: the host's own tool name and input
		// record, so the displayed request says what a vote authorizes (the
		// reason already rides the summary).
		return nativePickParams(params, "toolName", "input")
	},
	reply: func(params map[string]any, choice nativeAnswerChoice) (map[string]any, error) {
		if choice.Cancel {
			return map[string]any{"error": map[string]any{
				"code":    -32021,
				"message": "Client request cancelled: interaction/requestPermission"}}, nil
		}
		for _, option := range listOf(getOf(params, "options")) {
			if !valuesEqual(getOf(option, "optionId"), choice.OptionID) {
				continue
			}
			response := getOf(option, "response")
			if response == nil {
				return nil, &blockedError{message: "ZCODE_INTERACTION: the host option carries no response object"}
			}
			return map[string]any{"result": response}, nil
		}
		return nil, &blockedError{message: "ZCODE_INTERACTION: option is not one the host offered: " + choice.OptionID}
	},
}

// zcodeUserInputInteraction declares the structured user-input request's frame
// contract, reverse-read from the app-server bundle:
//
//   - Request params (schema FBi): {requestId, sessionId, turnId?,
//     toolCallId?, toolName?, prompt?, questions: [{question, header,
//     options: [{value, label, description?, preview?}], multiSelect?}],
//     input?, origin?, schema?}. The question options carry a `value`, not an
//     optionId, at this boundary.
//   - Reply (the request's result schema rYe): {action:
//     accept|decline|cancel, content?, reason?}.strict(). Accepting a single
//     question's option is {action: "accept", content: {answer: <value>}} -
//     the host's own answer normalizer reads content.answer for a
//     single-question prompt (and content.answers per question otherwise).
//   - Cancel is first-class here: {action: "cancel"} (the host maps a
//     cancelled AskUserQuestion to a denied tool outcome itself).
var zcodeUserInputInteraction = nativeInteractionSpec{
	method:    zcodeUserInputMethod,
	requestID: zcodeInteractionRequestID,
	summary: func(params map[string]any) string {
		// The question bodies are what the user actually answers, so they are
		// the summary; the generic prompt only titles them and never replaces
		// them. Every question is kept.
		questions := []string{}
		for _, question := range listOf(getOf(params, "questions")) {
			if text := textOf(getOf(question, "question")); text != "" && text != "null" {
				questions = append(questions, text)
			}
		}
		prompt := textOf(getOf(params, "prompt"))
		if prompt == "null" {
			prompt = ""
		}
		switch {
		case len(questions) == 0 && prompt != "":
			// The host named no questions; its prompt is all it said.
			return prompt
		case len(questions) == 0:
			return "user input request"
		case prompt != "":
			return prompt + ": " + strings.Join(questions, "; ")
		default:
			return strings.Join(questions, "; ")
		}
	},
	options: func(params map[string]any) any {
		questions := listOf(getOf(params, "questions"))
		if len(questions) != 1 {
			return []any{}
		}
		options := listOf(getOf(questions[0], "options"))
		if options == nil {
			return []any{}
		}
		return options
	},
	body: func(params map[string]any) map[string]any {
		// The questions (each with its own options) are the request's actual
		// body and persist verbatim; the prompt persists as the title above
		// them.
		return nativePickParams(params, "prompt", "questions")
	},
	reply: func(params map[string]any, choice nativeAnswerChoice) (map[string]any, error) {
		if choice.Cancel {
			return map[string]any{"result": map[string]any{"action": "cancel"}}, nil
		}
		questions := listOf(getOf(params, "questions"))
		if len(questions) != 1 {
			return nil, &blockedError{message: "ZCODE_INTERACTION: an option answer needs exactly one question; cancel otherwise"}
		}
		for _, option := range listOf(getOf(questions[0], "options")) {
			if valuesEqual(getOf(option, "value"), choice.OptionID) {
				return map[string]any{"result": map[string]any{
					"action":  "accept",
					"content": map[string]any{"answer": choice.OptionID}}}, nil
			}
		}
		return nil, &blockedError{message: "ZCODE_INTERACTION: option is not one the host offered: " + choice.OptionID}
	},
}

// zcodeInteractionObserved is zcode's attribution rule for a delivered hold's
// continuation (hooks.observes): only a session/event that is itself a fact
// about the held request may confirm it. Three kinds are, with the field
// spellings of the real T4 trace,
// .harness/validation/g2d-zcode-probe-20260920T4/native-events.jsonl:
//
//   - permission.resolved, the host's own receipt for the answered request:
//     payload {decision, reason, requestId, toolCallId} (T4 seq 57), confirmed
//     when payload.requestId is the held request's requestId.
//   - tool.updated, the tool call's progress: payload names the executing
//     toolCallId (T4 seq 58-60), confirmed when it is the held request's
//     toolCallId.
//   - turn.completed / turn.failed, the turn's terminal events (T4 seq 71),
//     confirmed when the event's turnId is the held request's turnId.
//
// Everything else - session.titleUpdated and session.updated metadata,
// available-command changes, model streaming, telemetry - is same-session
// traffic that names no held request, tool call or turn outcome, and leaves
// the decision honestly at delivered.
func zcodeInteractionObserved(hold *nativePendingHold, message map[string]any) bool {
	if !valuesEqual(message["method"], "session/event") {
		return false
	}
	event := objectOrEmpty(message["params"])
	payload := objectOrEmpty(getOrOf(event, "payload", map[string]any{}))
	// owns matches one of the held request's own identity values (a non-empty
	// string in the request params) against the event's value for it.
	owns := func(held, event any) bool {
		id, isString := held.(string)
		return isString && id != "" && valuesEqual(event, id)
	}
	switch textOf(getOf(event, "type")) {
	case "permission.resolved":
		return owns(getOf(hold.params, "requestId"), getOf(payload, "requestId"))
	case "tool.updated":
		return owns(getOf(hold.params, "toolCallId"), getOf(payload, "toolCallId"))
	case "turn.completed", "turn.failed":
		return owns(getOf(hold.params, "turnId"), getOf(event, "turnId"))
	}
	return false
}

// newZcodeClient spawns the app-server with ZCode's transport configuration:
// argv [node, script, --cwd, workspace, app-server], every request bounded by
// a 60-second total budget (the turn itself is waited out by execute's receive
// loop, never by an unbounded request), and the plain {id, method, params}
// shape with the base framing defaults. The two user-decision interactions are
// held (G2d); the machine-answerable ones stay in respond.
func newZcodeClient(launch map[string]any, workspace, directory string, environment map[string]string) (*zcodeClient, error) {
	executableValue, err := mustEntry(launch, "executable")
	if err != nil {
		return nil, err
	}
	scriptValue, err := mustEntry(launch, "script")
	if err != nil {
		return nil, err
	}
	z := &zcodeClient{directory: directory}
	z.client, err = newNativeClientWithEnv(nativeHooks{
		transportPrefix: "ZCODE_TRANSPORT",
		requestPrefix:   "ZCODE_NATIVE",
		defaultTimeout:  60 * time.Second,
		bounded:         true,
		interactions:    []nativeInteractionSpec{zcodePermissionInteraction, zcodeUserInputInteraction},
		observes:        zcodeInteractionObserved,
		respond:         z.respond,
		classify:        zcodeClassify,
		notification:    z.notification,
	}, []string{textOf(executableValue), textOf(scriptValue), "--cwd", workspace, "app-server"},
		workspace, directory, environment)
	if err != nil {
		return nil, err
	}
	return z, nil
}

// request delegates to the shared request/reply loop.
func (z *zcodeClient) request(method string, params map[string]any) (any, error) {
	return z.client.request(method, params)
}

// respond is ZCode's client-directed reply table (Client.respond): the three
// machine-answerable interactions. The two user decisions
// (interaction/requestPermission, interaction/requestUserInput) never reach
// here - the transport's interaction specs hold them for a user decision
// (G2d) - and every other method keeps the default -32601 refusal.
func (z *zcodeClient) respond(method string, params any) (*nativeReply, error) {
	switch method {
	case "session/requestRuntimePreferences":
		// ZCode defines this response as its native compatibility fallback: the
		// host then keeps its own preferences instead of ours.
		return &nativeReply{
			payload: map[string]any{"error": map[string]any{
				"code":    nativeUnsupportedMethod,
				"message": "Use native runtime preferences fallback"}},
			status: "native-default-fallback"}, nil
	case "interaction/requestOfficialMcpAuthHeaders":
		// No desktop authentication port is available on this connection.
		return &nativeReply{
			payload: map[string]any{"result": map[string]any{
				"ok": false, "reason": "official_auth_unavailable"}},
			status: "official_auth_unavailable"}, nil
	case zcodeRuntimeHeadersMethod:
		// The one interaction with a machine answer: the credential this host
		// provisioned for the provider it is about to call. Answered from the
		// host's own index rather than refused, because in host mode the host
		// cannot reach a provider without it. When the host has no such
		// credential the reply says so and the host reports the failure itself,
		// so a missing credential is never laundered into a denied turn.
		provider, _ := getOf(params, "providerId").(string)
		secret := zcodeRequestAuth(provider)
		if secret == "" {
			return &nativeReply{
				payload: map[string]any{"result": map[string]any{
					"headersApplied": false,
					"errorMessage":   "no provisioned request credential for " + provider}},
				status: "provider-runtime-headers-unavailable"}, nil
		}
		return &nativeReply{
			payload: map[string]any{"result": map[string]any{
				"headersApplied": true,
				"requestAuth":    map[string]any{"apiKey": secret}}},
			status: "provider-runtime-headers-applied"}, nil
	}
	return nil, nil
}

// zcodeClassify is ZCode's error classification (Client.classify): a resume
// the host rejected with its own "session is gone" code and text.
func zcodeClassify(method string, errValue any, params map[string]any) error {
	if method != "session/resume" {
		return nil
	}
	if !valuesEqual(getOf(errValue, "code"), zcodeSessionUnavailable) {
		return nil
	}
	message, _ := getOrOf(errValue, "message", "").(string)
	if !strings.HasPrefix(message, "Session not found:") {
		return nil
	}
	return &sessionUnavailableError{
		blockedError: blockedError{message: "ZCODE_SESSION_UNAVAILABLE: " + message},
		identity:     getOrOf(params, "sessionId", ""),
		method:       "session/resume",
		code:         getOf(errValue, "code"),
	}
}

// notification commits this session's events (Client.notification). Child
// sessions are separate evidence; only the bound root is recorded.
func (z *zcodeClient) notification(message any) {
	object, isObject := message.(map[string]any)
	if !isObject || !valuesEqual(object["method"], "session/event") {
		return
	}
	event, isDict := object["params"].(map[string]any)
	if z.identity == "" || !isDict || !valuesEqual(event["sessionId"], z.identity) {
		return
	}
	z.events = append(z.events, event)
	line, err := CanonicalJSON(event)
	if err != nil {
		return
	}
	file, err := os.OpenFile(filepath.Join(z.directory, "native-events.jsonl"),
		os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer file.Close()
	file.WriteString(string(line) + "\n")
}

// --- the selection mechanism (contract item 6) ---------------------------------

// zcodeReference is the exact provider/model reference a bound selection names
// (reference). A bare model name cannot be resolved: this version exposes no
// catalogue to resolve it against, so borrowing another provider's identity is
// not possible and would be a substitution anyway.
func zcodeReference(value string) (map[string]any, error) {
	if value == "" {
		return nil, nil
	}
	decoded, err := decodeNativeJSON([]byte(value))
	if err != nil {
		return nil, &blockedError{message: "ZCODE_CONFIG: a bound selection must name a provider and a model"}
	}
	ref, isObject := decoded.(map[string]any)
	if !isObject || !sameKeySet(ref, []string{"providerId", "modelId"}) {
		return nil, &blockedError{message: "ZCODE_CONFIG: a bound selection must name exactly one provider and model"}
	}
	return ref, nil
}

// zcodeSelectedModel is the exact native selection the host reports, or nil if
// it reports none (selected_model). The identity this transport binds -- and
// hands back to a later run -- is exactly one provider and one model; a
// selection the host does not report stays absent rather than being filled in
// from its defaults.
func zcodeSelectedModel(snapshot any) map[string]any {
	settings := objectOrEmpty(getOf(snapshot, "settings"))
	current, isObject := getOrOf(objectOrEmpty(getOf(settings, "model")), "current", nil).(map[string]any)
	if !isObject {
		return nil
	}
	ref := map[string]any{"providerId": current["providerId"], "modelId": current["modelId"]}
	for _, value := range ref {
		text, isString := value.(string)
		if !isString || text == "" {
			return nil
		}
	}
	return ref
}

// zcodeSelection is the recorded selection: the host's own reported values,
// never our wish (selection).
func zcodeSelection(snapshot any) map[string]any {
	model := zcodeSelectedModel(snapshot)
	modelText := ""
	if model != nil {
		if encoded, err := CanonicalJSON(model); err == nil {
			modelText = string(encoded)
		}
	}
	settings := objectOrEmpty(getOf(snapshot, "settings"))
	thought := objectOrEmpty(getOrOf(settings, "thoughtLevel", nil))
	// The effort is the host's own level name (a string the likes of "low"),
	// or nothing: a host that reports no level -- or a value that is not a
	// level name -- expresses no preference here.
	effort := any("")
	if current, isString := getOrOf(thought, "current", "").(string); isString && current != "" {
		effort = current
	}
	return map[string]any{"model": modelText, "reasoning_effort": effort}
}

// zcodeAdvertisedModels is every exact reference the host currently
// advertises to this client (advertised_models).
func zcodeAdvertisedModels(snapshot any) []string {
	settings := objectOrEmpty(getOf(snapshot, "settings"))
	available := listOf(getOrOf(objectOrEmpty(getOf(settings, "model")), "available", []any{}))
	advertised := []string{}
	for _, entry := range available {
		object, isObject := entry.(map[string]any)
		if !isObject {
			continue
		}
		ref, isDict := object["ref"].(map[string]any)
		if !isDict || !sameKeySet(ref, []string{"providerId", "modelId"}) {
			continue
		}
		encoded, err := CanonicalJSON(ref)
		if err != nil {
			continue
		}
		advertised = append(advertised, string(encoded))
	}
	return advertised
}

// zcodeAdvertisedReasoning is the reasoning levels the host itself advertises
// for one exact reference (advertised_reasoning). session/setModel takes an
// object and refuses one without options.reasoningLevel, and the level has to
// be one of this model's own values (probed 2026-09-19 against the host that
// updated that morning: an object without options is refused with "Reasoning
// level is required for <provider>/<model>", a level the model does not
// advertise with `Reasoning effort "<level>" is not supported by ...`, and a
// string "providerId/modelId" never reaches that far -- the method's params
// schema rejects it as "expected object, received string", so the object with
// an advertised options.reasoningLevel is the only accepted shape). The host
// already reports the levels, so they are read from it: a level this transport
// picked itself would either be refused or silently describe a capability the
// model does not have. The fallback to the first advertised value mirrors the
// host's own default when it reports no default at all.
func zcodeAdvertisedReasoning(snapshot any, ref map[string]any) map[string]any {
	settings := objectOrEmpty(getOf(snapshot, "settings"))
	available := listOf(getOrOf(objectOrEmpty(getOf(settings, "model")), "available", []any{}))
	for _, entry := range available {
		object, isObject := entry.(map[string]any)
		if !isObject || !valuesEqual(object["ref"], ref) {
			continue
		}
		reasoning, _ := object["reasoning"].(map[string]any)
		levels := []any{}
		for _, level := range listOf(getOrOf(reasoning, "levels", nil)) {
			levelObject, isDict := level.(map[string]any)
			if !isDict {
				continue
			}
			value, isString := levelObject["value"].(string)
			if isString && value != "" {
				levels = append(levels, value)
			}
		}
		advertisedDefault := getOf(reasoning, "defaultLevel")
		chosen := any("")
		switch {
		case zcodeContainsValue(levels, advertisedDefault):
			chosen = advertisedDefault
		case len(levels) > 0:
			chosen = levels[0]
		}
		return map[string]any{"levels": levels, "default": chosen}
	}
	return map[string]any{"levels": []any{}, "default": ""}
}

// zcodeContainsValue reports value membership in a decoded list.
func zcodeContainsValue(values []any, want any) bool {
	for _, value := range values {
		if valuesEqual(value, want) {
			return true
		}
	}
	return false
}

// zcodeBoundModel is the exact model selection to apply, carrying the
// reasoning level with it (bound_model). A bound selection names one exact
// provider/model; the level is not part of that identity, so it comes from the
// request when one was asked for and from the host's own advertised values
// otherwise. A model that advertises no levels is left exactly as it was
// bound.
func zcodeBoundModel(snapshot any, wanted map[string]any, effort string) map[string]any {
	if wanted == nil {
		return nil
	}
	reported := zcodeAdvertisedReasoning(snapshot, wanted)
	// The level is the requested effort, else the host's own advertised
	// default; either way it is a level name, and "" names none.
	level := effort
	if level == "" {
		level, _ = reported["default"].(string)
	}
	out := map[string]any{}
	for key, value := range wanted {
		out[key] = value
	}
	if level != "" {
		out["options"] = map[string]any{"reasoningLevel": level}
	}
	return out
}

// --- the account provider overlay (contract item 7) ----------------------------

// zcodeBuiltinProviderRevision is the revision the app-server answers with for
// this catalogue (builtin_provider_revision). It is `<catalogue revision>`
// plus the sha256 of the catalogue's resolved path, not of its contents.
// ProviderRegistryService abandons an account refresh whose
// basedOnZCodeBuiltinRevision differs from it, which is why a launcher that
// hands over account configuration has to own the path the agent resolves.
func zcodeBuiltinProviderRevision(catalogue string) (string, error) {
	resolved := zcodeExpandUser(catalogue)
	if absolute, err := filepath.Abs(resolved); err == nil {
		resolved = absolute
	}
	data, err := os.ReadFile(resolved)
	if err != nil {
		return "", &blockedError{message: "ZCODE_ACCOUNT_CONFIG: cannot read the named provider catalogue " + resolved}
	}
	document, err := decodeNativeJSON(data)
	if err != nil {
		return "", &blockedError{message: "ZCODE_ACCOUNT_CONFIG: cannot read the named provider catalogue " + resolved}
	}
	revision := getOf(document, "revision")
	isRevision := false
	switch typed := revision.(type) {
	case string:
		isRevision = typed != ""
	case json.Number:
		isRevision = isIntegerLiteral(typed)
	}
	if !isRevision {
		return "", &blockedError{message: "ZCODE_ACCOUNT_CONFIG: the named provider catalogue has no revision: " + resolved}
	}
	sum := sha256.Sum256([]byte(resolved))
	return fmt.Sprintf("zcode-builtin:%s:%s", textOf(revision), hex.EncodeToString(sum[:])), nil
}

// zcodeCatalogueAccountRules is every account provider the named catalogue
// declares, mapped to its access rule (catalogue_account_rules).
func zcodeCatalogueAccountRules(catalogue string) (map[string]any, error) {
	data, err := os.ReadFile(catalogue)
	if err != nil {
		return nil, &blockedError{message: "ZCODE_ACCOUNT_CONFIG: cannot read provider rules from " + catalogue}
	}
	document, err := decodeNativeJSON(data)
	if err != nil {
		return nil, &blockedError{message: "ZCODE_ACCOUNT_CONFIG: cannot read provider rules from " + catalogue}
	}
	rules, err := mustEntry(getOf(document, "config"), "providerConfigRules")
	if err != nil {
		return nil, &blockedError{message: "ZCODE_ACCOUNT_CONFIG: cannot read provider rules from " + catalogue}
	}
	rules, err = mustEntry(rules, "providerRules")
	if err != nil {
		return nil, &blockedError{message: "ZCODE_ACCOUNT_CONFIG: cannot read provider rules from " + catalogue}
	}
	declared := map[string]any{}
	for _, rule := range listOf(rules) {
		object, isObject := rule.(map[string]any)
		if !isObject {
			continue
		}
		provider, isString := object["providerId"].(string)
		access, isDict := getOrOf(objectOrEmpty(getOf(rule, "config")), "access", nil).(map[string]any)
		if isString && provider != "" && isDict && valuesEqual(access["type"], zcodeAccountAccessType) {
			declared[provider] = access
		}
	}
	return declared, nil
}

// zcodeCatalogueAccountProviders is every account provider the named catalogue
// declares, in its own order (catalogue_account_providers).
func zcodeCatalogueAccountProviders(catalogue string) ([]string, error) {
	declared, err := zcodeCatalogueAccountRules(catalogue)
	if err != nil {
		return nil, err
	}
	providers := make([]string, 0, len(declared))
	for provider := range declared {
		providers = append(providers, provider)
	}
	return providers, nil
}

// zcodeHostSelectedAccountProvider is the provisioned account provider the
// host itself has selected, or "" (host_selected_account_provider).
//
// The host records its plan choice as a family and a plan kind, and the
// catalogue declares each account provider's own family and mode, so the two
// are matched on those declared fields -- never on the spelling of a provider
// id. The setting is host user-interface state that the agent does not depend
// on, so a missing or unreadable one means only that this host expresses no
// preference.
func zcodeHostSelectedAccountProvider(catalogue string, provisioned []string) (string, error) {
	if len(provisioned) == 0 {
		return "", nil
	}
	settingPath := filepath.Join(zcodeHostDataDirectory(), ".zcode", "v2", zcodeSettingFile)
	setting := any(nil)
	if data, err := os.ReadFile(settingPath); err == nil {
		setting, _ = decodeNativeJSON(data)
	}
	settingObject, isObject := setting.(map[string]any)
	if !isObject {
		return "", nil
	}
	family, familyIsString := settingObject[zcodeSettingFamilyKey].(string)
	if !familyIsString {
		return "", nil
	}
	kind := any(nil)
	if plans, isDict := settingObject[zcodeSettingPlansKey].(map[string]any); isDict {
		kind = getOf(objectOrEmpty(getOf(plans, family)), "kind")
	}
	kindText, kindIsString := kind.(string)
	if !kindIsString {
		return "", nil
	}
	declared, err := zcodeCatalogueAccountRules(catalogue)
	if err != nil {
		return "", err
	}
	for _, provider := range provisioned {
		access := objectOrEmpty(declared[provider])
		if valuesEqual(access[zcodeAccessFamilyKey], family) && valuesEqual(access[zcodeAccessModeKey], kindText) {
			return provider, nil
		}
	}
	return "", nil
}

// zcodeStoreKeyOrder lists the credential store's key names in document order.
// The store's values are encrypted account secrets and are never read.
func zcodeStoreKeyOrder(path string) ([]string, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, false, err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	token, err := decoder.Token()
	if err != nil {
		return nil, false, err
	}
	if delim, isDelim := token.(json.Delim); !isDelim || delim != '{' {
		return nil, true, nil
	}
	seen := map[string]bool{}
	keys := []string{}
	for decoder.More() {
		keyToken, err := decoder.Token()
		if err != nil {
			return nil, false, err
		}
		key, isString := keyToken.(string)
		if !isString {
			return nil, false, fmt.Errorf("invalid object key %v", keyToken)
		}
		var ignored any
		if err := decoder.Decode(&ignored); err != nil {
			return nil, false, err
		}
		if !seen[key] {
			seen[key] = true
			keys = append(keys, key)
		}
	}
	return keys, false, nil
}

// zcodeProvisionedAccountProviders is the account providers the host's own
// credential store says are provisioned (provisioned_account_providers).
//
// The store's key names are the host's provisioning index and are plaintext
// (`account-provider:coding-plan:<provider>:account:<identity>:api-key`); its
// values are encrypted account secrets, which are never read. Only providers
// the named catalogue also declares are reported, so the overlay cannot name a
// provider the agent has no configuration for.
func zcodeProvisionedAccountProviders(catalogue string) ([]string, error) {
	store := filepath.Join(zcodeHostDataDirectory(), ".zcode", "v2", zcodeCredentialsFile)
	if !zcodeIsRegularFile(store) {
		return []string{}, nil
	}
	keys, notAnIndex, err := zcodeStoreKeyOrder(store)
	if err != nil {
		return nil, &blockedError{message: "ZCODE_ACCOUNT_CONFIG: cannot read the host credential store " + store}
	}
	if notAnIndex {
		return nil, &blockedError{message: "ZCODE_ACCOUNT_CONFIG: the host credential store is not an index: " + store}
	}
	entries := []string{}
	for _, key := range keys {
		if strings.HasPrefix(key, zcodeAccountCredentialPrefix) && strings.HasSuffix(key, zcodeAccountCredentialSuffix) {
			entries = append(entries, key)
		}
	}
	if len(entries) == 0 {
		return []string{}, nil
	}
	known, err := zcodeCatalogueAccountProviders(catalogue)
	if err != nil {
		return nil, err
	}
	knownSet := map[string]bool{}
	for _, provider := range known {
		knownSet[provider] = true
	}
	provisioned := []string{}
	for _, key := range entries {
		body := strings.TrimSuffix(strings.TrimPrefix(key, zcodeAccountCredentialPrefix), zcodeAccountCredentialSuffix)
		provider := strings.Split(body, zcodeAccountIdentitySeparator)[0]
		if knownSet[provider] && !zcodeContainsString(provisioned, provider) {
			provisioned = append(provisioned, provider)
		}
	}
	return provisioned, nil
}

// zcodeAccountProviderConfiguration rebuilds the account provider overlay the
// host delivers, or nil (account_provider_configuration).
//
// A launcher-started app-server keeps no provider registry of its own, so the
// side that starts it has to hand one over. This reports the account state the
// host already holds -- its own provisioning index -- and never grants an
// entitlement the host does not have, so a host with no provisioned account
// provider stays empty and the transport reports that as it is.
func zcodeAccountProviderConfiguration(catalogue string, wanted map[string]any) (map[string]any, error) {
	providers, err := zcodeProvisionedAccountProviders(catalogue)
	if err != nil {
		return nil, err
	}
	if len(providers) == 0 {
		return nil, nil
	}
	// The preferred provider is the one that was asked for, else the plan this
	// host itself selected, and only then the order its own credential index
	// lists.
	var current string
	var chosen string
	named, _ := getOf(wanted, "providerId").(string)
	if zcodeContainsString(providers, named) {
		current, chosen = named, "requested"
	} else if selected, err := zcodeHostSelectedAccountProvider(catalogue, providers); err != nil {
		return nil, err
	} else if selected != "" {
		current, chosen = selected, "host-selected-plan"
	} else {
		current, chosen = providers[0], "provisioning-order"
	}
	base, err := zcodeBuiltinProviderRevision(catalogue)
	if err != nil {
		return nil, err
	}
	providerList := make([]any, len(providers))
	configurationProviders := map[string]any{}
	states := map[string]any{}
	for index, provider := range providers {
		providerList[index] = provider
		configurationProviders[provider] = map[string]any{"access": map[string]any{
			"type": zcodeAccountAccessType, "entitled": true}}
		states[provider] = map[string]any{
			"availability": "available", "entitled": true, "current": provider == current}
	}
	return map[string]any{
		"based_on":       base,
		"providers":      providerList,
		"current":        current,
		"current_source": chosen,
		"configuration": map[string]any{
			"revision":                    "account-config:" + base + ":" + strings.Join(providers, ","),
			"basedOnZCodeBuiltinRevision": base,
			"providers":                   configurationProviders,
			"states":                      states,
		},
	}, nil
}

// zcodeDeliverAccountProviders hands the host's account provider configuration
// to the app-server (deliver_account_providers).
//
// Only a pinned catalogue is accepted: without it the launcher does not own
// the path whose hash is the revision, and a delivery whose
// basedOnZCodeBuiltinRevision is even slightly off is silently ignored by the
// agent rather than refused. The answer is read back and recorded, because a
// launcher that cannot fill the registry has to be visible as that and not as
// a missing model later on.
func zcodeDeliverAccountProviders(client *zcodeClient, catalogue string, wanted map[string]any, directory string) (map[string]any, error) {
	delivered, err := zcodeAccountProviderConfiguration(catalogue, wanted)
	if err != nil {
		return nil, err
	}
	if delivered == nil {
		return nil, nil
	}
	configuration, _ := getOf(delivered, "configuration").(map[string]any)
	reply, err := client.request(zcodeAccountConfigMethod, configuration)
	if err != nil {
		return nil, err
	}
	replyObject, isObject := reply.(map[string]any)
	var status any
	var count any
	if isObject {
		status = getOf(replyObject, "status")
		count = getOf(replyObject, "providerCount")
	}
	statusText, _ := status.(string)
	if (statusText != "received" && statusText != "unchanged") ||
		!valuesEqual(count, len(listOf(getOf(delivered, "providers")))) {
		embedded, err := CanonicalJSON(reply)
		if err != nil {
			return nil, err
		}
		return nil, &blockedError{message: "ZCODE_ACCOUNT_CONFIG: the host did not accept the account " +
			"provider configuration: " + string(embedded)}
	}
	if err := writeAtomicJSON(filepath.Join(directory, "account-provider.json"), map[string]any{
		"catalogue":      catalogue,
		"based_on":       getOf(delivered, "based_on"),
		"providers":      getOf(delivered, "providers"),
		"current":        getOf(delivered, "current"),
		"current_source": getOf(delivered, "current_source"),
		"status":         status,
		"provider_count": count,
		"source":         "host credential index",
	}); err != nil {
		return nil, err
	}
	return delivered, nil
}

// --- applying the selection (contract items 8-9) -------------------------------

// applySelection applies the bound selection and reads the host back; never
// substitutes (apply_selection). A host may silently ignore a setting it does
// not support (an unavailable thought level is dropped with a warning), so
// every value is confirmed from the host's own report before it is recorded.
// Nothing is persisted to the workspace's last-used selection.
//
// A change is only asked of a host that advertises models at all: on the
// 2026-09-19 host a snapshot whose available list is empty means an empty
// provider registry, where session/setModel cannot apply any reference and
// answers with its own opaque "Provider Registry 中不存在 Model" error
// (probed; the failing G2c run jobs hit exactly that state after their
// account overlay had not been delivered). The refusal is this transport's
// own instead, and the frame is never fired. Registered bounded difference:
// the frozen reference still sends the frame and reports the host's error.
func (z *zcodeClient) applySelection(snapshot any, wanted map[string]any, effort, directory, workspace string) (map[string]any, error) {
	configured := zcodeSelection(snapshot)
	if wanted != nil {
		wantedJSON, err := CanonicalJSON(wanted)
		if err != nil {
			return nil, err
		}
		wantedText := string(wantedJSON)
		advertised := zcodeAdvertisedModels(snapshot)
		if len(advertised) > 0 && !zcodeContainsString(advertised, wantedText) {
			return nil, &blockedError{message: "ZCODE_CONFIG: requested exact provider/model is not advertised; no fallback"}
		}
		if textOf(configured["model"]) != wantedText {
			if len(advertised) == 0 {
				return nil, &blockedError{message: "ZCODE_CONFIG: requested exact provider/model is not advertised; no fallback"}
			}
			changed, err := z.request("session/setModel", map[string]any{
				"sessionId":                  z.identity,
				"model":                      zcodeBoundModel(snapshot, wanted, effort),
				"persistAsWorkspaceLastUsed": false,
			})
			if err != nil {
				return nil, err
			}
			// The readback the host confirms with: a settings block with
			// entries. An absent or empty one keeps the snapshot's selection.
			if settings, isObject := getOf(changed, "settings").(map[string]any); isObject && len(settings) > 0 {
				configured = zcodeSelection(changed)
			}
			if textOf(configured["model"]) != wantedText {
				return nil, &blockedError{message: "ZCODE_CONFIG: requested selection was not applied"}
			}
		}
	} else if configured["model"] == "" {
		return nil, &blockedError{message: "ZCODE_CONFIG: native selected model missing"}
	}
	if effort != "" && !valuesEqual(configured["reasoning_effort"], effort) {
		changed, err := z.request("session/setThoughtLevel", map[string]any{
			"sessionId":                  z.identity,
			"thoughtLevel":               effort,
			"persistAsWorkspaceLastUsed": false,
		})
		if err != nil {
			return nil, err
		}
		// The readback the host confirms with: a settings block with entries.
		if settings, isObject := getOf(changed, "settings").(map[string]any); isObject && len(settings) > 0 {
			configured = zcodeSelection(changed)
		}
		if !valuesEqual(configured["reasoning_effort"], effort) {
			return nil, &blockedError{message: "ZCODE_CONFIG: reasoning setting was not applied"}
		}
	}
	if err := writeAtomicJSON(filepath.Join(directory, "identity.json"), map[string]any{
		"identity":  z.identity,
		"provider":  "zcode",
		"workspace": workspace,
		"selection": configured,
	}); err != nil {
		return nil, err
	}
	return configured, nil
}

// zcodeHandshake checks the protocol both sides agreed on, or refuses to guess
// (handshake). The embedded protocol body is rendered canonically (registered
// bounded difference: the reference embeds it with Python's default
// separators).
func zcodeHandshake(snapshot any) (map[string]any, error) {
	protocol := objectOrEmpty(getOf(snapshot, "protocol"))
	if !valuesEqual(protocol["name"], zcodeProtocolName) || !valuesEqual(protocol["version"], zcodeProtocolVersion) {
		embedded, err := CanonicalJSON(protocol)
		if err != nil {
			return nil, err
		}
		return nil, &blockedError{message: fmt.Sprintf(
			"ZCODE_PROTOCOL: the app-server does not speak %s v%d: %s",
			zcodeProtocolName, zcodeProtocolVersion, string(embedded))}
	}
	return protocol, nil
}

// --- evidence materialization and reconciliation (contract items 10-11) --------

// zcodeMaterializeSource turns committed native turn evidence into the bound
// source receipt (materialize_source): events are matched by inputId/turnId,
// the sequence must be monotonic, and only turn.completed with
// resultType=success completes a turn.
func zcodeMaterializeSource(directory string) (map[string]any, error) {
	binding, err := readJSONFile(filepath.Join(directory, "native-source.json"))
	if err != nil {
		return nil, err
	}
	identity, err := mustEntry(binding, "sessionId")
	if err != nil {
		return nil, err
	}
	inputID, err := mustEntry(binding, "inputId")
	if err != nil {
		return nil, err
	}
	selection, err := mustEntry(binding, "selection")
	if err != nil {
		return nil, err
	}
	turn := any("")
	var end map[string]any
	seq := new(big.Int).SetInt64(-1)
	err = forEachSessionEvent(directory, identity, func(event map[string]any) error {
		number, isNumber := getOf(event, "seq").(json.Number)
		if !isNumber || !isIntegerLiteral(number) {
			return &blockedError{message: "ZCODE_BINDING: invalid sequence"}
		}
		value, valid := new(big.Int).SetString(number.String(), 10)
		if !valid || value.Sign() < 0 {
			return &blockedError{message: "ZCODE_BINDING: invalid sequence"}
		}
		if value.Cmp(seq) <= 0 {
			return nil
		}
		seq = value
		payload := objectOrEmpty(getOrOf(event, "payload", map[string]any{}))
		if !valuesEqual(getOf(payload, "inputId"), inputID) {
			return nil
		}
		eventType, err := mustEntry(event, "type")
		if err != nil {
			return err
		}
		if valuesEqual(eventType, "turn.started") {
			// turn is this input's turn id -- a host string; "" means no start
			// was recorded yet.
			if turn != "" || getOf(event, "turnId") == nil {
				return &blockedError{message: "ZCODE_BINDING: duplicate/missing start"}
			}
			turn = getOf(event, "turnId")
		}
		if valuesEqual(eventType, "turn.failed") || valuesEqual(eventType, "turn.completed") {
			if turn == "" || !valuesEqual(turn, getOf(event, "turnId")) || end != nil {
				return &blockedError{message: "ZCODE_BINDING: terminal mismatch"}
			}
			if !valuesEqual(eventType, "turn.completed") || !valuesEqual(getOf(payload, "resultType"), "success") {
				return &blockedError{message: "ZCODE_EXECUTION: turn failed"}
			}
			end = event
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if end == nil {
		return nil, &blockedError{message: "ZCODE_RESULT: matching native completion is missing"}
	}
	endPayload := objectOrEmpty(getOrOf(end, "payload", map[string]any{}))
	response, _ := getOrOf(endPayload, "response", "").(string)
	if err := writeNativeAnswer(directory, response, "ZCODE"); err != nil {
		return nil, err
	}
	return evidenceSource(directory, "zcode", identity, map[string]any{
		"native_turn":    turn,
		"input_id":       inputID,
		"selection":      selection,
		"usage_evidence": getOf(endPayload, "usage"),
	})
}

// zcodeReconcile recovers only a completed native turn that the result parser
// rejected (reconcile_completed_output). Only a result-framing error of an
// otherwise completed process can be replayed; a runtime or transport failure
// is never laundered into a successful worker result.
func zcodeReconcile(directory string, process any) (map[string]any, error) {
	return reconcileCompletedOutput(directory, process, "ZCODE",
		func(original string) bool { return strings.HasPrefix(original, zcodeResultFrameError) },
		func() (map[string]any, error) { return zcodeMaterializeSource(directory) })
}

// --- the execute sequence (contract item 12) -----------------------------------

// zcodeSchemaDirectiveKey is the one schema key whose prompt position is
// presentation, not value: the canonical sort keeps it at the head of every
// object that carries it.
const zcodeSchemaDirectiveKey = "additionalProperties"

// schemaForPrompt renders one decoded JSON value for embedding in a prompt:
// the canonical encoding, except that an object's additionalProperties key,
// when present, is written last instead of first. Arrays and nested objects
// are handled recursively; no value is changed, only the order an object's
// keys are presented in.
//
// Behavioural rationale (2026-09): GLM serving repeatedly echoed the schema's
// leading directive keyword -- additionalProperties, which the canonical sort
// keeps at the head of every object -- back into the answer as if it were a
// field, and the strict answer validator rejects such answers. The validator's
// strictness is independent of the prompt and stays exactly as it is;
// presenting the directive key last instead of first removes the trigger
// without touching the value.
//
// Registered bounded difference: the frozen reference embeds the schema with
// Python's default separators in dict order, so the two sides' prompt bytes
// already differ in separators and now also in this key's position; the
// values are equal. This rendering is for the prompt only -- identity,
// tool-policy, source and every other evidence serialization keep
// CanonicalJSON.
func schemaForPrompt(value any) ([]byte, error) {
	var out strings.Builder
	if err := encodeSchemaForPrompt(value, &out); err != nil {
		return nil, err
	}
	return []byte(out.String()), nil
}

// encodeSchemaForPrompt is schemaForPrompt's recursive worker: canonical
// leaves, canonical key order inside objects except that
// zcodeSchemaDirectiveKey is emitted after every other key of its object.
func encodeSchemaForPrompt(value any, out *strings.Builder) error {
	switch typed := value.(type) {
	case []any:
		out.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				out.WriteByte(',')
			}
			if err := encodeSchemaForPrompt(item, out); err != nil {
				return err
			}
		}
		out.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			if key != zcodeSchemaDirectiveKey {
				keys = append(keys, key)
			}
		}
		sort.Strings(keys)
		if _, present := typed[zcodeSchemaDirectiveKey]; present {
			keys = append(keys, zcodeSchemaDirectiveKey)
		}
		out.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				out.WriteByte(',')
			}
			writeCanonicalString(key, out)
			out.WriteByte(':')
			if err := encodeSchemaForPrompt(typed[key], out); err != nil {
				return err
			}
		}
		out.WriteByte('}')
	default:
		// Leaves keep the canonical encoding: the reorder touches key order
		// inside objects only, never a scalar's or a value's rendering.
		return encodeCanonical(value, out)
	}
	return nil
}

// zcodeExecute runs ZCode's app-server sequence for one request
// (native_zcode.execute).
func zcodeExecute(request any, directory string, probe bool) (result map[string]any, err error) {
	directory = resolveNativePath(directory)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	workspaceValue, err := mustEntry(request, "workspace")
	if err != nil {
		return nil, err
	}
	workspace := resolveNativePath(textOf(workspaceValue))
	// A binding with entries is used as captured; an absent or empty one means
	// nothing was captured yet and the provider is bound now.
	binding := objectOrEmpty(getOf(request, "binding"))
	if len(binding) == 0 {
		binding, err = zcodeCaptureBinding()
		if err != nil {
			return nil, err
		}
	}
	previous, _ := getOrOf(request, "identity", "").(string)
	modelValue, _ := getOrOf(request, "model", "").(string)
	wanted, err := zcodeReference(modelValue)
	if err != nil {
		return nil, err
	}
	effort, _ := getOrOf(request, "reasoning_effort", "").(string)
	// An optional permission_mode names the mode a NEW non-read-only session
	// is switched to before its first turn. Absent, the driver switches
	// nothing: the session runs in the host's own default mode, and the
	// approvals that mode can raise are answered through the G2d interaction
	// hold, not by forcing a permissive mode (the pre-G2d workaround). The
	// name is not validated here: the host's own setMode rejects modes it does
	// not know. Resumed and read-only sessions never apply it.
	wantedMode, _ := getOrOf(request, "permission_mode", "").(string)
	readOnlyValue, err := mustEntry(request, "read_only")
	if err != nil {
		return nil, err
	}
	readOnly, isBool := readOnlyValue.(bool)
	if !isBool {
		return nil, fmt.Errorf("read_only must be a boolean")
	}
	location := map[string]any{"workspacePath": workspace, "workspaceKey": workspace}
	// A new session is persisted immediately; a resume addresses the recorded
	// one.
	params := map[string]any{"workspace": location}
	if previous != "" {
		params["sessionId"] = previous
	} else {
		params["persistence"] = "immediate"
	}
	if readOnly {
		params["toolAllowlist"] = zcodeReadOnlyToolsValue()
	}
	if probe && previous == "" {
		params["titleGenerationEnabled"] = false
	}
	launch, err := mustEntry(binding, "launch")
	if err != nil {
		return nil, err
	}
	client, err := newZcodeClient(objectOrEmpty(launch), workspace, directory, zcodeLaunchEnvironment(binding))
	if err != nil {
		return nil, err
	}
	identity := ""
	attached := false
	defer func() {
		if attached {
			closeNativeSession(client.client, identity)
		}
		client.client.close()
	}()
	// The app-server starts with an empty provider registry and only the side
	// that launched it can fill it, so the account provider configuration goes
	// first: a snapshot taken before it reports no current model at all.
	catalogue := zcodePinnedCatalogue(binding)
	if catalogue != "" {
		if _, err := zcodeDeliverAccountProviders(client, catalogue, wanted, directory); err != nil {
			return nil, err
		}
	}
	method := "session/create"
	if previous != "" {
		method = "session/resume"
	}
	snapshot, err := client.request(method, params)
	if err != nil {
		return nil, err
	}
	protocol, err := zcodeHandshake(snapshot)
	if err != nil {
		return nil, err
	}
	sessionIdentity, _ := getOrOf(objectOrEmpty(getOf(snapshot, "session")), "sessionId", "").(string)
	if sessionIdentity == "" || (previous != "" && sessionIdentity != previous) {
		return nil, &blockedError{message: "ZCODE_BINDING: missing or changed identity"}
	}
	identity = sessionIdentity
	client.identity = identity
	attached = true
	if err := writeAtomicJSON(filepath.Join(directory, "identity.json"), map[string]any{
		"identity":  identity,
		"provider":  "zcode",
		"workspace": workspace,
		"selection": zcodeSelection(snapshot),
	}); err != nil {
		return nil, err
	}
	configured, err := client.applySelection(snapshot, wanted, effort, directory, workspace)
	if err != nil {
		return nil, err
	}
	if readOnly {
		if err := writeAtomicJSON(filepath.Join(directory, "tool-policy.json"), map[string]any{
			"read_only": true,
			"allowed":   zcodeReadOnlyToolsValue(),
		}); err != nil {
			return nil, err
		}
	}
	if probe {
		if _, err := client.request("session/close", map[string]any{"sessionId": identity}); err != nil {
			return nil, err
		}
		attached = false
		mcpAuth := "not_verified"
		for _, interaction := range client.client.interactions {
			if textOf(getOf(objectOrEmpty(interaction), "status")) == "official_auth_unavailable" {
				mcpAuth = "unavailable"
				break
			}
		}
		return map[string]any{
			"provider":     "zcode",
			"protocol":     protocol,
			"selection":    configured,
			"binding":      binding,
			"model_calls":  0,
			"interactions": client.client.interactions,
			"native_tools": "not_verified",
			"mcp_auth":     mcpAuth,
		}, nil
	}
	subscribed, err := client.request("session/subscribe", map[string]any{
		"sessionId":       identity,
		"deliveryKind":    "desktop-continuous",
		"includeSnapshot": false,
	})
	if err != nil {
		return nil, err
	}
	if !valuesEqual(getOf(subscribed, "sessionId"), identity) {
		return nil, &blockedError{message: "ZCODE_BINDING: subscription identity differs"}
	}
	permissionMode := any("")
	if !readOnly {
		// Evidence states the mode the host itself reports, never a value this
		// driver asked for: a session nothing switches records the mode its own
		// snapshot carried (a resumed session, or a new one with no requested
		// permission_mode); a session the request did switch records the host's
		// setMode readback. A host that reports no mode is recorded as empty,
		// never invented.
		if previous != "" || wantedMode == "" {
			permissionMode = getOrOf(objectOrEmpty(getOf(objectOrEmpty(
				getOf(snapshot, "settings")), "mode")), "current", "")
		} else {
			changed, err := client.request("session/setMode", map[string]any{
				"sessionId": identity,
				"mode":      wantedMode,
			})
			if err != nil {
				return nil, err
			}
			// settings is the host's own confirmation; the projection block in
			// this response can be a stale snapshot (observed with sessionId
			// "unknown" and the pre-set mode on the real host).
			permissionMode = getOrOf(objectOrEmpty(getOf(objectOrEmpty(
				getOf(changed, "settings")), "mode")), "current", "")
			if !valuesEqual(permissionMode, wantedMode) {
				return nil, &blockedError{message: "ZCODE_CONFIG: permission mode was not applied"}
			}
		}
		if err := writeAtomicJSON(filepath.Join(directory, "tool-policy.json"), map[string]any{
			"read_only":       false,
			"permission_mode": permissionMode,
		}); err != nil {
			return nil, err
		}
	}
	inputID := "formal-gates-" + filepath.Base(directory)
	turnBinding := map[string]any{"sessionId": identity, "inputId": inputID, "selection": configured}
	if !readOnly {
		turnBinding["permission_mode"] = permissionMode
	}
	if err := writeAtomicJSON(filepath.Join(directory, "native-source.json"), turnBinding); err != nil {
		return nil, err
	}
	promptValue, err := mustEntry(request, "prompt")
	if err != nil {
		return nil, err
	}
	promptText, isString := promptValue.(string)
	if !isString {
		return nil, fmt.Errorf("prompt must be a string")
	}
	schemaValue, err := mustEntry(request, "schema")
	if err != nil {
		return nil, err
	}
	// The schema is embedded through schemaForPrompt: the canonical form with
	// the one presentation-order change (registered bounded difference:
	// value-equal to the reference's default-separator rendering).
	schema, err := schemaForPrompt(schemaValue)
	if err != nil {
		return nil, err
	}
	prompt := promptText + "\nReturn ONLY one JSON object matching this schema:\n" + string(schema)
	ack, err := client.request("session/send", map[string]any{
		"sessionId": identity,
		"inputId":   inputID,
		"content":   prompt,
	})
	if err != nil {
		return nil, err
	}
	// The host's acknowledgement of the input: its own boolean, and true.
	accepted, _ := getOf(ack, "accepted").(bool)
	if !valuesEqual(getOf(ack, "sessionId"), identity) || !accepted {
		return nil, &blockedError{message: "ZCODE_BINDING: input not accepted"}
	}
	// The bounded turn wait: events are consumed as they arrive, one receive at
	// a time, and the loop ends only on this input's terminal event or on the
	// deadline. A silent host ends it through the receive timeout, whose text
	// states the session is not assumed lost.
	//
	// While a native interaction is held (G2d) the budget pauses: the deadline
	// is a remaining-budget, not a wall-clock moment, so the time the user
	// takes to decide does not consume the turn. A silent host during a hold is
	// therefore not a failure either - the loop keeps waiting for the answer.
	// The paused time is folded back in whenever no hold is pending anymore,
	// including in the receive-timeout branch below: a wait that expired after
	// the answer's delivery was derived from the stale deadline and must not
	// be read as the host going silent.
	position := 0
	terminal := false
	deadline := time.Now().Add(zcodeTurnBudget)
	var pausedSince time.Time
	// remainingBudget folds the paused hold time back into the deadline (once
	// no hold is pending anymore) and reports what is left of the turn.
	remainingBudget := func() time.Duration {
		if !client.client.pendingHeld() && !pausedSince.IsZero() {
			deadline = deadline.Add(time.Since(pausedSince))
			pausedSince = time.Time{}
		}
		return time.Until(deadline)
	}
	for !terminal {
		if client.client.pendingHeld() {
			if pausedSince.IsZero() {
				pausedSince = time.Now()
			}
		} else if remainingBudget() <= 0 {
			break
		}
		for position < len(client.events) {
			event := client.events[position]
			// The evidence copy in native-events.jsonl is the durable record;
			// this retained slot is released the moment the event is consumed.
			client.events[position] = nil
			position++
			if !valuesEqual(getOf(event, "sessionId"), identity) {
				return nil, &blockedError{message: "ZCODE_BINDING: event from another session"}
			}
			eventType, err := mustEntry(event, "type")
			if err != nil {
				return nil, err
			}
			payload := objectOrEmpty(getOrOf(event, "payload", map[string]any{}))
			if valuesEqual(getOf(payload, "inputId"), inputID) &&
				(valuesEqual(eventType, "turn.completed") || valuesEqual(eventType, "turn.failed")) {
				terminal = true
			}
		}
		if terminal {
			break
		}
		// The buffer is fully drained here: the loop above only leaves
		// position == len(events), and new events arrive only inside
		// receive -- the notification hook appends them in this same
		// goroutine -- so nothing can land between the drain and this
		// reset. Releasing the drained buffer before waiting keeps a long
		// streaming turn retaining only the events that have arrived and
		// not yet been read.
		client.events = client.events[:0]
		position = 0
		wait := time.Until(deadline)
		if wait > 120*time.Second {
			wait = 120 * time.Second
		}
		if wait < time.Second {
			wait = time.Second
		}
		if _, err := client.client.receive(&wait); err != nil {
			if errors.Is(err, errNativeTimedOut) {
				if client.client.pendingHeld() {
					// The host waits for its user-directed request; so does
					// this loop, without spending the turn budget.
					continue
				}
				// The expired wait may have spanned the answer's delivery:
				// recompute the remaining budget before calling the silence
				// a timeout.
				if remainingBudget() > 0 {
					continue
				}
				return nil, &blockedError{
					message: "ZCODE_NATIVE_TIMEOUT: no native message; session is not assumed lost"}
			}
			return nil, err
		}
	}
	if !terminal {
		return nil, &blockedError{message: "ZCODE_TIMEOUT: turn incomplete; inspect the original invocation"}
	}
	if _, err := client.request("session/close", map[string]any{"sessionId": identity}); err != nil {
		return nil, err
	}
	attached = false
	// Finalize proofs only after native shutdown messages have been drained.
	source, err := zcodeMaterializeSource(directory)
	if err != nil {
		return nil, err
	}
	if err := writeAtomicJSON(filepath.Join(directory, "source.json"), source); err != nil {
		return nil, err
	}
	return source, nil
}

// --- provider dispatch and the registry operations ------------------------------

// zcodeValidateBinding is the registry's validate_binding for zcode: the
// provider must match and every bound file proof must still hold.
func zcodeValidateBinding(binding any) error {
	return validateNativeBinding(binding, "zcode")
}

// zcodeVerifySource is the registry's verify_source for zcode.
func zcodeVerifySource(directory string, source any) error {
	return verifyNativeSource(directory, source, "zcode")
}

// nativeImplementation is one execution adapter's Go entry points: the Go
// spelling of one native_hosts._ADAPTERS entry. dshDispatch keeps the
// refusal (and the adapters list it names); this table routes a dispatched
// provider to its code, so adding a host stays one adapter file plus one row.
type nativeImplementation struct {
	capture         func() (map[string]any, error)
	validateBinding func(binding any) error
	verifySource    func(directory string, source any) error
	reconcile       func(directory string, process any) (map[string]any, error)
	execute         func(request any, directory string, probe bool) (map[string]any, error)
}

var nativeImplementations = map[string]nativeImplementation{
	"dsh": {
		capture:         dshCaptureBinding,
		validateBinding: dshValidateBinding,
		verifySource:    dshVerifySource,
		reconcile:       dshReconcile,
		execute:         dshExecute,
	},
	"zcode": {
		capture:         zcodeCaptureBinding,
		validateBinding: zcodeValidateBinding,
		verifySource:    zcodeVerifySource,
		reconcile:       zcodeReconcile,
		execute:         zcodeExecute,
	},
}

// nativeImplementationFor refuses every provider this binary does not
// implement and routes an implemented one to its adapter.
func nativeImplementationFor(provider string) (nativeImplementation, error) {
	if err := dshDispatch(provider); err != nil {
		return nativeImplementation{}, err
	}
	impl, ok := nativeImplementations[provider]
	if !ok {
		// The adapters list and the routing table diverged; the refusal text
		// stays the one dshDispatch names.
		return nativeImplementation{}, &blockedError{message: fmt.Sprintf(
			"UNSUPPORTED_NATIVE_HOST: %s; execution adapters: %s", provider, strings.Join(executionAdapters, ", "))}
	}
	return impl, nil
}
