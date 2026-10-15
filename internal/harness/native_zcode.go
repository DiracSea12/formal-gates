package harness

// The ZCode app-server adapter. Only the ZCode protocol lives here;
// process handling, the request/reply loop, bindings and evidence come from
// native_transport. No graph imports, workflow callbacks, global configuration
// writes or model substitution. Issued roles use the registered workspace tool.
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
// Native protocol documents use the canonical encoder; family error texts
// retain the host protocol values.

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"formal-gates/internal/host"
	"formal-gates/internal/settings"
	"maps"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// The native allowlist restricts builtin and MCP tools. The registered handler
// takes execution authority from the original issued request.
var zcodeManagedTools = []any{"mcp__formal-gates__workspace", "mcp__formal-gates__submit_semantics", "AskUserQuestion"}

// ZCode exposes root properties to models; the original closed alternatives
// retain all semantic requirements and acceptance constraints.
func zcodeSemanticToolSchema(schema map[string]any) map[string]any {
	alternatives := listOf(schema["anyOf"])
	if len(alternatives) == 0 {
		alternatives = listOf(schema["oneOf"])
	}
	if len(alternatives) == 0 {
		return schema
	}
	properties := map[string]any{}
	for _, alternative := range alternatives {
		for name, field := range objectOrEmpty(getOf(alternative, "properties")) {
			if prior, exists := properties[name]; exists && !valuesEqual(prior, field) {
				properties[name] = map[string]any{"anyOf": []any{prior, field}}
			} else {
				properties[name] = field
			}
		}
	}
	projected := maps.Clone(schema)
	projected["properties"] = properties
	return projected
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

const zcodeAccountIdentityPrefix = "account-provider:"

const zcodeAccountIdentitySuffix = ":identity"

// The host's retained per-account credential key form. The family segment is
// the bundle's own literal; the account identity segment is URL-encoded, so it
// carries no separator, while the provider id itself may contain colons.
const zcodeAccountKeyPrefix = "account-provider:coding-plan:"

const zcodeAccountKeyInfix = ":account:"

const zcodeAccountKeySuffix = ":api-key"

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

// --- the provider catalogue ---------------------------------------------------

// zcodeBundledProviderCatalogue names the launcher's bundled seed only when
// neither script-relative host catalogue candidate exists. The host owns its
// active catalogue and refreshes it itself; this lookup writes no files.
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
func zcodeHostDataDirectory(configured ...string) string {
	if len(configured) != 0 && configured[0] != "" {
		return configured[0]
	}
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
func zcodeProviderEnvironment(script string, data ...string) map[string]string {
	catalogue := zcodeBundledProviderCatalogue(script)
	if catalogue == "" {
		return nil
	}
	personal := filepath.Join(zcodeHostDataDirectory(data...), ".zcode", "v2", zcodePersonalConfig)
	return map[string]string{
		zcodeBuiltinCatalogueEnv: catalogue,
		zcodePersonalConfigEnv:   personal,
	}
}

// zcodePinnedCatalogue is the catalogue a launcher pinned this binding to, or
// "" for a host's own (pinned_catalogue). Only a frozen launcher catalogue
// receives the account overlay; unpinned host paths do not.
func zcodePinnedCatalogue(binding map[string]any) string {
	if getOf(binding, "cataloguePinned") != true {
		return ""
	}
	environment := objectOrEmpty(getOf(binding, "environment"))
	catalogue, _ := getOrOf(environment, zcodeBuiltinCatalogueEnv, "").(string)
	return catalogue
}

// --- capture, credentials and the client hooks --------------------------------

// zcodeCaptureBinding binds zcode to the node binary and the app-server script
// it will execute (capture_binding). The catalogue is host configuration the
// host refreshes on its own, so it is named in the launch environment but not
// proofed like an executed binary.
func zcodeCaptureBinding() (map[string]any, error) {
	native, sources := map[string]string{}, map[string]string{}
	if err := zcodeResolvePaths(native, sources); err != nil {
		return nil, err
	}
	return zcodeConfiguredBinding(settings.Values{Native: native})
}

func zcodeResolvePaths(native, sources map[string]string) error {
	if _, frozen := native["zcodeCataloguePinned"]; frozen {
		return nil
	}
	config := settings.Values{Native: native}
	executable := config.NativeString("zcodeExecutable")
	node, err := exec.LookPath(executable)
	if err != nil {
		return &blockedError{message: "ZCODE_UNAVAILABLE: node or app-server script missing"}
	}
	executableSource := "PATH"
	if filepath.IsAbs(executable) || strings.ContainsAny(executable, `/\`) {
		executableSource = "configured"
	}
	script, hasScript := os.LookupEnv("FORMAL_GATES_ZCODE_SCRIPT")
	scriptSource := "FORMAL_GATES_ZCODE_SCRIPT"
	if configured := config.NativeString("zcodeScript"); configured != "" {
		script, hasScript, scriptSource = configured, true, "configured"
	}
	if !hasScript {
		script, scriptSource = zcodeDefaultScript, "platform default"
	}
	script = resolveNativePath(zcodeExpandUser(script))
	if !zcodeIsRegularFile(script) {
		return &blockedError{message: "ZCODE_UNAVAILABLE: node or app-server script missing"}
	}
	home := config.NativeString("zcodeHome")
	homeSource := "configured"
	explicitHome := home != ""
	if !explicitHome {
		home, err = os.UserHomeDir()
		if err != nil {
			return err
		}
		homeSource = "HOME"
		if runtime.GOOS == "windows" {
			homeSource = "USERPROFILE"
		}
	}
	home = resolveNativePath(home)
	data := home
	dataSource := "zcodeHome"
	if !explicitHome {
		if configured := strings.TrimSpace(os.Getenv("ZCODE_DATA_BASE_DIR")); configured != "" {
			data, dataSource = resolveNativePath(configured), "ZCODE_DATA_BASE_DIR"
		}
	}
	catalogue := strings.TrimSpace(os.Getenv(zcodeBuiltinCatalogueEnv))
	personal := strings.TrimSpace(os.Getenv(zcodePersonalConfigEnv))
	catalogueSource, personalSource := "host resolves catalogue", "host resolves catalogue"
	pinned := "false"
	if catalogue != "" {
		catalogueSource = zcodeBuiltinCatalogueEnv
	}
	if personal != "" {
		personalSource = zcodePersonalConfigEnv
	}
	if environment := zcodeProviderEnvironment(script, data); environment != nil {
		// The native bootstrap accepts a complete provider pair as its runtime
		// paths. A bundled seed fills only a missing pair.
		if catalogue == "" || personal == "" {
			catalogue = resolveNativePath(environment[zcodeBuiltinCatalogueEnv])
			personal = environment[zcodePersonalConfigEnv]
			catalogueSource, personalSource = "zcodeScript", "zcodeDataBaseDir"
		}
		pinned = "true"
	}
	credentials := strings.TrimSpace(os.Getenv(zcodeCredentialHelperEnv))
	credentialSource := zcodeCredentialHelperEnv
	if credentials == "" {
		credentials, credentialSource = filepath.Join(data, ".zcode", "v2", zcodeCredentialsFile), "zcodeDataBaseDir"
	}
	for name, value := range map[string]string{
		"zcodeExecutable": resolveNativePath(node), "zcodeScript": script, "zcodeHome": home,
		"zcodeDataBaseDir": data, "zcodeBuiltinCatalogue": catalogue,
		"zcodePersonalConfig": personal, "zcodeCredentialStore": resolveNativePath(credentials), "zcodeCataloguePinned": pinned,
	} {
		native[name] = value
	}
	for name, source := range map[string]string{
		"zcodeExecutable": executableSource, "zcodeScript": scriptSource, "zcodeHome": homeSource,
		"zcodeDataBaseDir": dataSource, "zcodeBuiltinCatalogue": catalogueSource,
		"zcodePersonalConfig": personalSource, "zcodeCredentialStore": credentialSource, "zcodeCataloguePinned": "zcodeScript",
	} {
		if sources[name] == "" || sources[name] == "default" {
			sources[name] = source
		}
	}
	return nil
}

func zcodeConfiguredBinding(config settings.Values) (map[string]any, error) {
	for _, name := range []string{"zcodeExecutable", "zcodeScript", "zcodeHome", "zcodeDataBaseDir", "zcodeCredentialStore"} {
		if config.Native[name] == "" {
			return nil, &blockedError{message: "ZCODE_CONFIG: frozen native paths required"}
		}
	}
	for _, name := range []string{"zcodeBuiltinCatalogue", "zcodePersonalConfig"} {
		if _, exists := config.Native[name]; !exists {
			return nil, &blockedError{message: "ZCODE_CONFIG: frozen catalogue paths required"}
		}
	}
	pinned := config.Native["zcodeCataloguePinned"]
	if pinned != "true" && pinned != "false" {
		return nil, &blockedError{message: "ZCODE_CONFIG: frozen catalogue selection required"}
	}
	launch := map[string]any{"executable": config.Native["zcodeExecutable"], "script": config.Native["zcodeScript"]}
	binding, err := buildCaptureBinding("zcode", launch)
	if err != nil {
		return nil, err
	}
	binding["cataloguePinned"] = pinned == "true"
	binding["environment"] = map[string]any{
		"HOME": config.Native["zcodeHome"], "ZCODE_DATA_BASE_DIR": config.Native["zcodeDataBaseDir"],
		zcodeBuiltinCatalogueEnv: config.Native["zcodeBuiltinCatalogue"],
		zcodePersonalConfigEnv:   config.Native["zcodePersonalConfig"],
		zcodeCredentialHelperEnv: config.Native["zcodeCredentialStore"],
	}
	if runtime.GOOS == "windows" {
		binding["environment"].(map[string]any)["USERPROFILE"] = config.Native["zcodeHome"]
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

// zcodeCredentialHelperPath resolves the package-owned host credential helper.
func zcodeCredentialHelperPath() (string, error) {
	root, err := nativePackageRoot()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "internal", "harness", zcodeCredentialHelper), nil
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
// The account is the host's own current one: the identity entry a login
// wrote, or, when no identity entry exists, the desktop OAuth login's current
// family and user_info id, for which accountType -- the provider's
// access.accountType, or the request's own accountAccess value -- has to be
// carried. Only this account's own provisioned value is read, through the
// host's cipher, which is local (see zcode_credential.mjs): no keychain access
// and no user decision are involved. The value is never returned into
// evidence -- answers go through the reply loop, which records the method and
// the status only -- and is never logged, written or reported by this
// transport.
func zcodeRequestAuth(node, provider, accountType string, environment map[string]string) string {
	if provider == "" || environment[zcodeCredentialHelperEnv] == "" {
		return ""
	}
	helper, helperErr := zcodeCredentialHelperPath()
	if helperErr != nil || !zcodeIsRegularFile(helper) {
		return ""
	}
	var stdout bytes.Buffer
	cmd := exec.Command(node, helper, provider, environment[zcodeCredentialHelperEnv], accountType)
	cmd.Env = nativeChildEnvironment(environment)
	cmd.Stdout = &stdout
	// Credential helper stderr is discarded and never recorded.
	if err := cmd.Start(); err != nil {
		return ""
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(60 * time.Second):
		// A timed-out credential helper is killed and returns no credential.
		_ = cmd.Process.Kill()
		<-done
		return ""
	}
	return strings.TrimSpace(stdout.String())
}

// zcodeClient is ZCode's adapter state around the shared transport: the bound
// session identity and the events its notification hook committed. The events slice is a receive
// buffer, not a transcript: the execution loop clears each slot as it reads
// the event and resets the drained buffer before waiting for more, and
// native-events.jsonl is the only durable record of the whole turn.
type zcodeClient struct {
	executable  string
	environment map[string]string
	hostData    string
	client      *nativeClient
	directory   string
	identity    string
	events      []map[string]any
}

// zcodePermissionMethod and zcodeUserInputMethod are the two client-directed
// requests this transport holds for a user decision: the tool-call
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
//     content.answers maps original question text to its value for multiple
//     questions. Multi-select values are arrays of original option values.
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
		if choice.Answers != nil {
			return map[string]any{"result": map[string]any{"action": "accept", "content": choice.Answers}}, nil
		}
		questions := listOf(getOf(params, "questions"))
		if len(questions) != 1 {
			return nil, &blockedError{message: "ZCODE_INTERACTION: an option answer needs exactly one question"}
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
// about the held request may confirm it. Three event kinds carry that identity:
//
//   - permission.resolved, the host's own receipt for the answered request:
//     payload {decision, reason, requestId, toolCallId}, confirmed
//     when payload.requestId is the held request's requestId.
//   - tool.updated, the tool call's progress: payload names the executing
//     toolCallId, confirmed when it is the held request's
//     toolCallId.
//   - turn.completed / turn.failed, the turn's terminal events,
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
// argv [node, script, --cwd, workspace, app-server], 60-second RPC
// acknowledgements, and the plain {id, method, params} shape. Turn completion
// waits without a deadline. The two user-decision interactions are
// held; the machine-answerable ones stay in respond.
func newZcodeClient(launch map[string]any, workspace, directory string, environment map[string]string) (*zcodeClient, error) {
	executableValue, err := mustEntry(launch, "executable")
	if err != nil {
		return nil, err
	}
	scriptValue, err := mustEntry(launch, "script")
	if err != nil {
		return nil, err
	}
	z := &zcodeClient{executable: textOf(executableValue), hostData: environment["ZCODE_DATA_BASE_DIR"], environment: environment, directory: directory}
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
// - and every other method keeps the default -32601 refusal.
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
		// accountAccess carries the provider's family the same way the
		// catalogue's access rules do; without it only an identity entry names
		// the account.
		accountType, _ := getOf(getOf(params, "accountAccess"), "accountType").(string)
		secret := zcodeRequestAuth(z.executable, provider, accountType, z.environment)
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

// --- the selection mechanism --------------------------------------------------

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
// for one exact reference (advertised_reasoning). session/setModel requires an
// object with options.reasoningLevel and refuses unadvertised levels. When the
// host names no default, it selects the first advertised level.
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

// --- the account provider overlay ---------------------------------------------

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

// zcodeHostSelectedAccountProvider is the provisioned account provider the
// host itself has selected, or "" (host_selected_account_provider).
//
// The host records its plan choice as a family and a plan kind, and the
// catalogue declares each account provider's own family and mode, so the two
// are matched on those declared fields -- never on the spelling of a provider
// id. The setting is host user-interface state that the agent does not depend
// on, so a missing or unreadable one means only that this host expresses no
// preference.
func zcodeHostSelectedAccountProvider(catalogue string, provisioned []string, home ...string) (string, error) {
	if len(provisioned) == 0 {
		return "", nil
	}
	settingPath := filepath.Join(zcodeHostDataDirectory(home...), ".zcode", "v2", zcodeSettingFile)
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

// zcodeStoreAccountProvider is the account provider a credential-store key
// names, or "": the current identity a login recorded, or a retained
// per-account credential. The store's own key forms are the host's index, so
// a provider is never synthesized from an id's spelling.
func zcodeStoreAccountProvider(key string) string {
	if strings.HasPrefix(key, zcodeAccountIdentityPrefix) && strings.HasSuffix(key, zcodeAccountIdentitySuffix) {
		return strings.TrimSuffix(strings.TrimPrefix(key, zcodeAccountIdentityPrefix), zcodeAccountIdentitySuffix)
	}
	if !strings.HasPrefix(key, zcodeAccountKeyPrefix) || !strings.HasSuffix(key, zcodeAccountKeySuffix) {
		return ""
	}
	body := strings.TrimSuffix(strings.TrimPrefix(key, zcodeAccountKeyPrefix), zcodeAccountKeySuffix)
	if at := strings.LastIndex(body, zcodeAccountKeyInfix); at >= 0 {
		return body[:at]
	}
	return ""
}

// zcodeProvisionedAccountProviders is the account providers the host's own
// credential store says are provisioned (provisioned_account_providers).
//
// The store's key names are the host's provisioning index: the identity a
// login recorded and every retained per-account credential both name their
// provider, and the first mention in document order is the provider's place.
// The bound request credential reader confirms that the current account has
// its exact secret; retained secrets for other accounts do not grant provider
// availability.
func zcodeProvisionedAccountProviders(catalogue string, client *zcodeClient) ([]string, error) {
	store := client.environment[zcodeCredentialHelperEnv]
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
	candidates := []string{}
	for _, key := range keys {
		if provider := zcodeStoreAccountProvider(key); provider != "" && !zcodeContainsString(candidates, provider) {
			candidates = append(candidates, provider)
		}
	}
	if len(candidates) == 0 {
		return []string{}, nil
	}
	known, err := zcodeCatalogueAccountRules(catalogue)
	if err != nil {
		return nil, err
	}
	provisioned := []string{}
	for _, provider := range candidates {
		access, declared := known[provider]
		if !declared {
			continue
		}
		family, _ := objectOrEmpty(access)[zcodeAccessFamilyKey].(string)
		if zcodeRequestAuth(client.executable, provider, family, client.environment) != "" {
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
func zcodeAccountProviderConfiguration(catalogue string, wanted map[string]any, client *zcodeClient) (map[string]any, error) {
	providers, err := zcodeProvisionedAccountProviders(catalogue, client)
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
	} else if selected, err := zcodeHostSelectedAccountProvider(catalogue, providers, client.hostData); err != nil {
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
	delivered, err := zcodeAccountProviderConfiguration(catalogue, wanted, client)
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

// --- applying the selection ---------------------------------------------------

// applySelection applies the bound selection and reads the host back; never
// substitutes. A host may silently ignore a setting it does
// not support (an unavailable thought level is dropped with a warning), so
// every value is confirmed from the host's own report before it is recorded.
// Nothing is persisted to the workspace's last-used selection.
//
// A model change requires a non-empty advertised model list. The transport
// refuses an empty provider registry without sending session/setModel.
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
		"identity":        z.identity,
		"provider":        "zcode",
		"workspace":       workspace,
		"selection":       configured,
		"acknowledgement": map[string]any{"protocol": getOf(snapshot, "protocol"), "session": getOf(snapshot, "session")},
	}); err != nil {
		return nil, err
	}
	return configured, nil
}

// zcodeHandshake checks the agreed protocol and renders an incompatible
// protocol body as canonical JSON.
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

// --- evidence materialization and reconciliation ------------------------------

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
	if err := writeSubmittedAnswer(directory); err != nil {
		return nil, err
	}
	return evidenceSource(directory, "zcode", identity, map[string]any{
		"native_turn":    turn,
		"input_id":       inputID,
		"selection":      selection,
		"transcript":     filepath.Join(directory, "native-events.jsonl"),
		"usage_evidence": getOf(endPayload, "usage"),
	})
}

func zcodeReconcile(directory string, process any, services ...ExecutionServices) (map[string]any, error) {
	return recoverNativeSource(directory, process, "ZCODE", func() (map[string]any, error) { return zcodeMaterializeSource(directory) })
}

// --- the execute sequence -----------------------------------------------------

// zcodeExecute runs ZCode's app-server sequence for one request
// (native_zcode.execute): parse the request, establish the session, prepare
// the turn, send it, wait it out, settle the evidence.
func zcodeExecute(request any, directory string, probe bool, services ...ExecutionServices) (map[string]any, error) {
	directory = resolveNativePath(directory)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return nil, err
	}
	req, err := zcodeParseRequest(request)
	if err != nil {
		return nil, err
	}
	session, err := zcodeOpenSession(req, directory, probe)
	if err != nil {
		return nil, err
	}
	defer session.close()
	if probe {
		result, err := session.probeResult(req.binding)
		if err == nil && getOf(request, "selection_options") == true {
			result["selection_options"] = zcodeSelectionOptions(session.snapshot, nativeSelectionString(getOf(request, "selection_model")))
		}
		return result, err
	}
	if err := session.prepareTurn(req); err != nil {
		return nil, err
	}
	if err := session.sendTurn(request, services...); err != nil {
		return nil, err
	}
	if err := session.awaitTurn(); err != nil {
		return nil, err
	}
	return session.settleTurn()
}

func zcodeSelectionOptions(snapshot any, target string) host.SelectionCatalogue {
	current := nativeModelSelection(zcodeSelection(snapshot))
	catalogue := host.SelectionCatalogue{Current: current, Default: current, Source: "ZCode session settings"}
	if target == "" {
		target = current.Model
	}
	for _, entry := range listOf(getOf(getOf(getOf(snapshot, "settings"), "model"), "available")) {
		ref, ok := getOf(entry, "ref").(map[string]any)
		if !ok {
			continue
		}
		encoded, err := CanonicalJSON(ref)
		if err != nil {
			continue
		}
		option := host.ModelOption{Value: string(encoded), Label: nativeSelectionString(getOf(entry, "name"))}
		if option.Label == "" {
			option.Label = nativeSelectionString(getOf(entry, "label"))
		}
		if option.Value == target {
			reasoning := zcodeAdvertisedReasoning(snapshot, ref)
			option.Efforts = nativeStringValues(getOf(reasoning, "levels"), "")
			option.DefaultEffort = nativeSelectionString(getOf(reasoning, "default"))
		}
		catalogue.Models = append(catalogue.Models, option)
	}
	return catalogue
}

// zcodeRequest is one execution's parsed request: the workspace, the binding
// (captured now when the request carried none), the resume identity, the
// wanted selection, the reasoning effort, the permission mode a new
// non-read-only session is switched to, and the read-only flag. The prompt
// and schema stay unparsed until sendTurn: their absence surfaces only there,
// after the session evidence exists.
type zcodeRequest struct {
	workspace  string
	binding    map[string]any
	previous   string
	wanted     map[string]any
	effort     string
	wantedMode string
	readOnly   bool
}

// zcodeParseRequest validates and resolves the request's own fields, in the
// order the sequence reads them.
func zcodeParseRequest(request any) (zcodeRequest, error) {
	workspaceValue, err := mustEntry(request, "workspace")
	if err != nil {
		return zcodeRequest{}, err
	}
	var req zcodeRequest
	req.workspace = resolveNativePath(textOf(workspaceValue))
	// A binding with entries is used as captured; an absent or empty one means
	// nothing was captured yet and the provider is bound now.
	req.binding = objectOrEmpty(getOf(request, "binding"))
	if len(req.binding) == 0 {
		req.binding, err = zcodeCaptureBinding()
		if err != nil {
			return zcodeRequest{}, err
		}
	}
	req.previous, _ = getOrOf(request, "identity", "").(string)
	modelValue, _ := getOrOf(request, "model", "").(string)
	req.wanted, err = zcodeReference(modelValue)
	if err != nil {
		return zcodeRequest{}, err
	}
	req.effort, _ = getOrOf(request, "reasoning_effort", "").(string)
	// An optional permission_mode names the mode a NEW non-read-only session
	// is switched to before its first turn. Absent, the driver switches
	// nothing: the session runs in the host's own default mode, and the
	// approvals that mode can raise are answered through the interaction
	// hold, not by forcing a permissive mode. The
	// name is not validated here: the host's own setMode rejects modes it does
	// not know. Resumed and read-only sessions never apply it.
	req.wantedMode, _ = getOrOf(request, "permission_mode", "").(string)
	readOnlyValue, err := mustEntry(request, "read_only")
	if err != nil {
		return zcodeRequest{}, err
	}
	readOnly, isBool := readOnlyValue.(bool)
	if !isBool {
		return zcodeRequest{}, fmt.Errorf("read_only must be a boolean")
	}
	req.readOnly = readOnly
	return req, nil
}

// zcodeSession is one execution's established app-server session: the
// connected client plus the identity, handshake protocol, creation snapshot
// and confirmed selection the turn steps consume. attached tracks the
// session's own lifetime: close() ends an attached session on the host
// before the transport closes.
type zcodeSession struct {
	client     *zcodeClient
	directory  string
	inputID    string
	identity   string
	snapshot   any
	protocol   map[string]any
	configured map[string]any
	attached   bool
}

// close is the deferred cleanup of every zcodeExecute path.
func (s *zcodeSession) close() {
	if s.attached {
		closeNativeSession(s.client.client, s.identity)
	}
	s.client.client.close()
}

// zcodeOpenSession launches the app-server and establishes the session the
// request names: the account provider overlay a launcher-started app-server
// cannot start without, the create-or-resume call, the handshake, the
// identity binding, the selection application and the read-only tool policy.
// On failure the client this function launched is closed here — zcodeExecute
// owns the cleanup only of a session it was handed.
func zcodeOpenSession(req zcodeRequest, directory string, probe bool) (*zcodeSession, error) {
	location := map[string]any{"workspacePath": req.workspace, "workspaceKey": req.workspace}
	// A new session is persisted immediately; a resume addresses the recorded
	// one.
	params := map[string]any{"workspace": location}
	if req.previous != "" {
		params["sessionId"] = req.previous
	} else {
		params["persistence"] = "immediate"
	}
	if !probe {
		server, err := nativeToolServer(directory)
		if err != nil {
			return nil, err
		}
		params["mcpServers"] = []any{server}
		params["toolAllowlist"] = zcodeManagedTools
	}
	if probe && req.previous == "" {
		params["titleGenerationEnabled"] = false
	}
	launch, err := mustEntry(req.binding, "launch")
	if err != nil {
		return nil, err
	}
	client, err := newZcodeClient(objectOrEmpty(launch), req.workspace, directory, zcodeLaunchEnvironment(req.binding))
	if err != nil {
		return nil, err
	}
	s := &zcodeSession{client: client, directory: directory}
	established := false
	defer func() {
		if !established {
			s.close()
		}
	}()
	// The protocol serves requests after native provider bootstrap has settled
	// the active catalogue. Cross that boundary before reading its revision;
	// the account overlay must then precede the session's selection snapshot.
	catalogue := zcodePinnedCatalogue(req.binding)
	if catalogue != "" {
		if _, err := client.request("runtime/capabilities", map[string]any{}); err != nil {
			return nil, err
		}
		if !filepath.IsAbs(catalogue) {
			catalogue = filepath.Join(req.workspace, catalogue)
		}
		if _, err := zcodeDeliverAccountProviders(client, catalogue, req.wanted, directory); err != nil {
			return nil, err
		}
	}
	method := "session/create"
	if req.previous != "" {
		method = "session/resume"
	}
	snapshot, err := client.request(method, params)
	if err != nil {
		return nil, err
	}
	s.snapshot = snapshot
	protocol, err := zcodeHandshake(snapshot)
	if err != nil {
		return nil, err
	}
	s.protocol = protocol
	sessionIdentity, _ := getOrOf(objectOrEmpty(getOf(snapshot, "session")), "sessionId", "").(string)
	if sessionIdentity == "" || (req.previous != "" && sessionIdentity != req.previous) {
		return nil, &blockedError{message: "ZCODE_BINDING: missing or changed identity"}
	}
	s.identity = sessionIdentity
	client.identity = s.identity
	s.attached = true
	if err := writeAtomicJSON(filepath.Join(directory, "identity.json"), map[string]any{
		"identity":        s.identity,
		"provider":        "zcode",
		"workspace":       req.workspace,
		"selection":       zcodeSelection(snapshot),
		"acknowledgement": map[string]any{"protocol": getOf(snapshot, "protocol"), "session": getOf(snapshot, "session")},
	}); err != nil {
		return nil, err
	}
	configured, err := client.applySelection(snapshot, req.wanted, req.effort, directory, req.workspace)
	if err != nil {
		return nil, err
	}
	s.configured = configured
	if req.readOnly {
		if err := writeAtomicJSON(filepath.Join(directory, "tool-policy.json"), map[string]any{
			"read_only": true,
			"allowed":   zcodeManagedTools,
		}); err != nil {
			return nil, err
		}
	}
	established = true
	return s, nil
}

// probeResult closes the fresh session and reports the probe answer: the
// handshake protocol, the confirmed selection, the binding and the two
// interaction facts a probe can verify.
func (s *zcodeSession) probeResult(binding map[string]any) (map[string]any, error) {
	if _, err := s.client.request("session/close", map[string]any{"sessionId": s.identity}); err != nil {
		return nil, err
	}
	s.attached = false
	mcpAuth := "not_verified"
	for _, interaction := range s.client.client.interactions {
		if textOf(getOf(objectOrEmpty(interaction), "status")) == "official_auth_unavailable" {
			mcpAuth = "unavailable"
			break
		}
	}
	return map[string]any{
		"provider":     "zcode",
		"protocol":     s.protocol,
		"selection":    s.configured,
		"binding":      binding,
		"model_calls":  0,
		"interactions": s.client.client.interactions,
		"native_tools": "not_verified",
		"mcp_auth":     mcpAuth,
	}, nil
}

// prepareTurn settles the turn's fixed preconditions: the event-stream
// subscription, the permission mode and tool policy of a non-read-only
// session, and the native source binding the turn's evidence is matched by.
func (s *zcodeSession) prepareTurn(req zcodeRequest) error {
	subscribed, err := s.client.request("session/subscribe", map[string]any{
		"sessionId":       s.identity,
		"deliveryKind":    "desktop-continuous",
		"includeSnapshot": false,
	})
	if err != nil {
		return err
	}
	if !valuesEqual(getOf(subscribed, "sessionId"), s.identity) {
		return &blockedError{message: "ZCODE_BINDING: subscription identity differs"}
	}
	permissionMode := any("")
	if !req.readOnly {
		// Evidence states the mode the host itself reports, never a value this
		// driver asked for: a session nothing switches records the mode its own
		// snapshot carried (a resumed session, or a new one with no requested
		// permission_mode); a session the request did switch records the host's
		// setMode readback. A host that reports no mode is recorded as empty,
		// never invented.
		if req.previous != "" || req.wantedMode == "" {
			permissionMode = getOrOf(objectOrEmpty(getOf(objectOrEmpty(
				getOf(s.snapshot, "settings")), "mode")), "current", "")
		} else {
			changed, err := s.client.request("session/setMode", map[string]any{
				"sessionId": s.identity,
				"mode":      req.wantedMode,
			})
			if err != nil {
				return err
			}
			// settings is the host's own confirmation; the projection block in
			// this response can be a stale snapshot (observed with sessionId
			// "unknown" and the pre-set mode on the real host).
			permissionMode = getOrOf(objectOrEmpty(getOf(objectOrEmpty(
				getOf(changed, "settings")), "mode")), "current", "")
			if !valuesEqual(permissionMode, req.wantedMode) {
				return &blockedError{message: "ZCODE_CONFIG: permission mode was not applied"}
			}
		}
		if err := writeAtomicJSON(filepath.Join(s.directory, "tool-policy.json"), map[string]any{
			"read_only":       false,
			"permission_mode": permissionMode,
		}); err != nil {
			return err
		}
	}
	s.inputID = "formal-gates-" + filepath.Base(s.directory)
	turnBinding := map[string]any{"sessionId": s.identity, "inputId": s.inputID, "selection": s.configured}
	if !req.readOnly {
		turnBinding["permission_mode"] = permissionMode
	}
	return writeAtomicJSON(filepath.Join(s.directory, "native-source.json"), turnBinding)
}

// sendTurn builds the prompt and sends it as this turn's input.
func (s *zcodeSession) sendTurn(request any, services ...ExecutionServices) error {
	promptValue, err := mustEntry(request, "prompt")
	if err != nil {
		return err
	}
	promptText, isString := promptValue.(string)
	if !isString {
		return fmt.Errorf("prompt must be a string")
	}
	ack, err := s.client.request("session/send", map[string]any{
		"sessionId": s.identity,
		"inputId":   s.inputID,
		"content":   promptText,
	})
	if err != nil {
		return err
	}
	// The host's acknowledgement of the input: its own boolean, and true.
	accepted, _ := getOf(ack, "accepted").(bool)
	if !valuesEqual(getOf(ack, "sessionId"), s.identity) || !accepted {
		return &blockedError{message: "ZCODE_BINDING: input not accepted"}
	}
	return settleQueuedMessages(s.directory, request, nil, services)
}

// awaitTurn consumes events until this input's terminal event or a transport
// error. A quiet or streaming host retains the same execution responsibility.
func (s *zcodeSession) awaitTurn() error {
	position := 0
	terminal := false
	for !terminal {
		for position < len(s.client.events) {
			event := s.client.events[position]
			// The evidence copy in native-events.jsonl is the durable record;
			// this retained slot is released the moment the event is consumed.
			s.client.events[position] = nil
			position++
			if !valuesEqual(getOf(event, "sessionId"), s.identity) {
				return &blockedError{message: "ZCODE_BINDING: event from another session"}
			}
			eventType, err := mustEntry(event, "type")
			if err != nil {
				return err
			}
			payload := objectOrEmpty(getOrOf(event, "payload", map[string]any{}))
			if valuesEqual(getOf(payload, "inputId"), s.inputID) &&
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
		s.client.events = s.client.events[:0]
		position = 0
		if _, err := s.client.client.receive(nil); err != nil {
			return err
		}
	}
	return nil
}

// settleTurn ends a completed turn: the session is closed on the host, the
// committed events are materialized into the bound source receipt, and the
// receipt is the execution's result.
func (s *zcodeSession) settleTurn() (map[string]any, error) {
	if _, err := s.client.request("session/close", map[string]any{"sessionId": s.identity}); err != nil {
		return nil, err
	}
	s.attached = false
	// Finalize proofs only after native shutdown messages have been drained.
	source, err := zcodeMaterializeSource(s.directory)
	if err != nil {
		return nil, err
	}
	if err := writeAtomicJSON(filepath.Join(s.directory, "source.json"), source); err != nil {
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
func zcodeVerifyStart(_ any, acknowledgement any, identity string) error {
	if getOf(getOf(acknowledgement, "session"), "sessionId") != identity {
		return fmt.Errorf("native session acknowledgement differs")
	}
	_, err := zcodeHandshake(acknowledgement)
	return err
}

func zcodeVerifySource(directory string, source any) error {
	return verifyNativeSource(directory, source, "zcode")
}
