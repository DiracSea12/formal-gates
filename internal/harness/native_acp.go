package harness

// The shared ACP session layer. Every ACP host adapter runs the same
// sequence over the plain transport: read the dispatch's common inputs,
// spawn the host child, initialize gated by the platform's capability check,
// the adapter's optional authenticate step, session/new (or the platform's
// resume method for a carried identity), bind the negotiated configOptions
// selection through set_config_option readback, then run one unbounded
// session/prompt turn gated on the host's end_turn.
// An adapter supplies only its real deltas as spec fields: the launch argv,
// the transport hooks, the resume method, the error-family prefix, whether it
// drives session/close, the create's additional-server list, the capability
// gate and the authenticate step. Everything else is this file.

import (
	"fmt"
	"path/filepath"
	"time"
)

// acpEnvelope is the JSON-RPC 2.0 frame shape every ACP host speaks.
func acpEnvelope(value map[string]any) map[string]any {
	framed := map[string]any{"jsonrpc": "2.0"}
	for key, item := range value {
		framed[key] = item
	}
	return framed
}

// acpSelection maps an ACP configOptions list to {id: currentValue}, a missing
// currentValue becoming null.
func acpSelection(raw any) (map[string]any, error) {
	out := map[string]any{}
	for _, option := range listOf(getOf(raw, "configOptions")) {
		id, err := mustEntry(option, "id")
		if err != nil {
			return nil, err
		}
		out[textOf(id)] = getOf(option, "currentValue")
	}
	return out, nil
}

// acpConfiguredIs reports whether the selection currently holds the requested
// value for one option; selection values are strings or null.
func acpConfiguredIs(configured map[string]any, option, requested string) bool {
	current, _ := configured[option].(string)
	return current == requested
}

// acpVerifyStart is verify_start for ACP adapters: the persisted session
// acknowledgement must name the recorded identity.
func acpVerifyStart(_ any, acknowledgement any, identity string) error {
	if getOf(acknowledgement, "sessionId") != identity {
		return fmt.Errorf("native session acknowledgement differs")
	}
	return nil
}

// acpDispatch is the shared front of an ACP execute request: the platform
// dispatch plus the carried identity an ACP adapter resumes.
type acpDispatch struct {
	*nativeDispatch
	identity string
}

// newACPDispatch reads the execute request's shared inputs through the common
// dispatch and adds the request's carried identity.
func newACPDispatch(request any, directory string, capture func() (map[string]any, error)) (*acpDispatch, error) {
	dispatch, err := newNativeDispatch(request, directory, capture)
	if err != nil {
		return nil, err
	}
	return &acpDispatch{nativeDispatch: dispatch, identity: textOf(getOrOf(request, "identity", ""))}, nil
}

// acpSessionSpec is one ACP platform's session contract: how the host child
// launches (argv plus the transport's hooks), which method continues a
// carried identity, the error-family prefix on the run's refusals, the
// create's additional-server list (nil is the empty list), whether the
// adapter drives session/close, the capability gate over the host's
// initialize answer, and the optional authenticate step between the
// handshake and the create.
type acpSessionSpec struct {
	prefix       string
	argv         []string
	hooks        nativeHooks
	resume       string
	servers      []any
	sessionClose bool
	capable      func(initialize any) error
	authenticate func(client *nativeClient) error
}

// acpSession is one live ACP host session: the client transport, the observed
// identity, the host's own handshake and create answers, and the negotiated
// selection.
type acpSession struct {
	client     *nativeClient
	spec       acpSessionSpec
	dispatch   *acpDispatch
	attached   bool
	identity   string
	initialize any
	created    map[string]any
	configured map[string]any
}

// connect spawns the host child under the platform's transport contract. The
// session is not protocol-started: defer close and call open.
func (d *acpDispatch) connect(spec acpSessionSpec) (*acpSession, error) {
	client, err := newNativeClientWithEnv(spec.hooks, spec.argv, d.workspace, d.directory, d.environment)
	if err != nil {
		return nil, err
	}
	return &acpSession{client: client, spec: spec, dispatch: d}, nil
}

// open runs the shared protocol front: the initialize handshake gated by the
// platform's capability check, the adapter's optional authenticate step,
// then session/new — or the platform's resume method when the dispatch
// carries an identity — and the observed-identity binding: the host's answer
// must name the session it opened or the exact identity it was asked to
// continue.
func (s *acpSession) open() error {
	initialize, err := s.client.request("initialize",
		map[string]any{"protocolVersion": 1, "clientCapabilities": map[string]any{}})
	if err != nil {
		return err
	}
	s.initialize = initialize
	if err := s.spec.capable(initialize); err != nil {
		return err
	}
	if s.spec.authenticate != nil {
		if err := s.spec.authenticate(s.client); err != nil {
			return err
		}
	}
	servers := s.spec.servers
	if servers == nil {
		servers = []any{}
	}
	// ACP's create params carry the additional-server list even when the
	// platform mounts its tools elsewhere: an absent list is not an empty one.
	params := map[string]any{"cwd": s.dispatch.workspace, "mcpServers": servers}
	method := "session/new"
	if s.dispatch.identity != "" {
		params["sessionId"] = s.dispatch.identity
		method = s.spec.resume
	}
	created, err := s.client.request(method, params)
	if err != nil {
		return err
	}
	observed, _ := getOrOf(created, "sessionId", any(s.dispatch.identity)).(string)
	if observed == "" || (s.dispatch.identity != "" && observed != s.dispatch.identity) {
		return &blockedError{message: s.spec.prefix + "_BINDING: missing or changed identity"}
	}
	s.identity = observed
	s.attached = true
	// The acknowledgement is the host's own create answer; the observed
	// identity stays bound to it even when the answer names none.
	s.created = objectOrEmpty(created)
	s.created["sessionId"] = observed
	return nil
}

// configure binds the negotiated selection: identity.json records the
// session's acknowledgement and the selection it already holds, then each
// requested option the session lacks is applied through set_config_option and
// read back.
func (s *acpSession) configure() error {
	configured, err := acpSelection(s.created)
	if err != nil {
		return err
	}
	s.configured = configured
	if err := writeAtomicJSON(filepath.Join(s.dispatch.directory, "identity.json"), map[string]any{
		"identity": s.identity, "workspace": s.dispatch.workspace,
		"selection": configured, "acknowledgement": s.created}); err != nil {
		return err
	}
	for _, name := range []string{"model", "reasoning_effort"} {
		requested, _ := getOrOf(s.dispatch.request, name, "").(string)
		if requested == "" || acpConfiguredIs(configured, name, requested) {
			continue
		}
		changed, err := s.client.request("session/set_config_option",
			map[string]any{"sessionId": s.identity, "configId": name, "value": requested})
		if err != nil {
			return err
		}
		updated, err := acpSelection(changed)
		if err != nil {
			return err
		}
		for key, value := range updated {
			configured[key] = value
		}
		if !acpConfiguredIs(configured, name, requested) {
			return &blockedError{message: s.spec.prefix + "_CONFIG: " + name + " selection was not applied"}
		}
	}
	return nil
}

// turn issues the prompt text as one unbounded session/prompt turn and
// settles the invocation's queued messages; the raw prompt result is the
// caller's to gate through endTurn.
func (s *acpSession) turn(services []ExecutionServices) (any, error) {
	promptValue, err := mustEntry(s.dispatch.request, "prompt")
	if err != nil {
		return nil, err
	}
	promptText, isString := promptValue.(string)
	if !isString {
		return nil, fmt.Errorf("prompt must be a string")
	}
	response, err := s.client.request("session/prompt",
		map[string]any{"sessionId": s.identity,
			"prompt": []any{map[string]any{"type": "text", "text": promptText}}},
		nativeUnbounded)
	if err != nil {
		return nil, err
	}
	if err := settleQueuedMessages(s.dispatch.directory, s.dispatch.request, nil, services); err != nil {
		return nil, err
	}
	return response, nil
}

// endTurn is the host's turn-end verdict: anything but its own end_turn stop
// reason is the run's refusal.
func (s *acpSession) endTurn(response any) error {
	if !valuesEqual(getOf(response, "stopReason"), "end_turn") {
		return &blockedError{message: s.spec.prefix + "_STOP: " + textOf(getOf(response, "stopReason"))}
	}
	return nil
}

// detach asks the host to close the session now; a platform whose host owns
// session/close calls it where its semantics say the session ends.
func (s *acpSession) detach(timeout ...time.Duration) error {
	if _, err := s.client.request(s.client.hooks.closeMethodOr(),
		map[string]any{"sessionId": s.identity}, timeout...); err != nil {
		return err
	}
	s.attached = false
	return nil
}

// close is the deferred teardown: a platform that drives session/close
// releases a still-open session first (its failure never hides a result); the
// transport always closes.
func (s *acpSession) close() {
	if s.spec.sessionClose && s.attached {
		closeNativeSession(s.client, s.identity)
	}
	s.attached = false
	_ = s.client.close()
}
