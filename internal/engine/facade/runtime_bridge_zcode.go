package facade

import (
	"encoding/json"
	"fmt"
	"io"
)

type zcodeSessionOptions struct {
	CaptureUsage bool
	Mode         string
	Model        string
	Effort       string
	PrepareTools func(string) ([]any, error)
}

// runZCodeSession uses the platform's native request/event protocol. The caller
// must own a claimed process and journal its streams. Identity attachment is
// committed before sending any model input; this function never issues work.
func runZCodeSession(reader io.Reader, writer io.Writer, session zcodeRequest, inputID, prompt string, attached func(string) error, options ...zcodeSessionOptions) (string, json.RawMessage, error) {
	if session.ID == "" || inputID == "" || attached == nil || (session.Method != "session/create" && session.Method != "session/resume") {
		return "", nil, fmt.Errorf("ZCODE_BINDING: incomplete native session request")
	}
	decoder, encoder := json.NewDecoder(reader), json.NewEncoder(writer)
	var observer *zcodeTurnObservation
	var identity string
	read := func(expected string) (json.RawMessage, error) {
		for {
			var msg struct {
				ID     string          `json:"id"`
				Method string          `json:"method"`
				Params json.RawMessage `json:"params"`
				Result json.RawMessage `json:"result"`
				Error  json.RawMessage `json:"error"`
			}
			if err := decoder.Decode(&msg); err != nil {
				return nil, fmt.Errorf("ZCODE_TRANSPORT: %w", err)
			}
			if msg.Method != "" {
				if msg.ID != "" {
					if msg.Method == "session/requestRuntimePreferences" {
						var preferences struct {
							SessionID string `json:"sessionId"`
							Scope     string `json:"scope"`
						}
						if json.Unmarshal(msg.Params, &preferences) != nil || preferences.SessionID == "" || (preferences.Scope != "runtime-materialization" && preferences.Scope != "user-execution") || (identity != "" && preferences.SessionID != identity) || (session.Method == "session/resume" && preferences.SessionID != session.Params["sessionId"]) {
							return nil, fmt.Errorf("ZCODE_BINDING: runtime preferences do not match the native session")
						}
						// This native setup request is not a tool grant or a user decision.
						// Keep independent roles free of ambient memory, implicit search
						// and automatic answers to questions owned by the engine/user.
						response := map[string]any{"id": msg.ID, "result": map[string]any{
							"nativeSearchEnhancementsEnabled": false, "memoryEnabled": false,
							"askUserQuestionAutoResolutionEnabled": false, "modelContextBudgetStrategy": "preflight-v1",
						}}
						if err := encoder.Encode(response); err != nil {
							return nil, err
						}
						continue
					}
					// A host interaction request is not an engine grant. Fail instead of
					// silently approving a tool or asking a second model to decide.
					return nil, fmt.Errorf("ZCODE_INTERACTION: unhandled native request %s", msg.Method)
				}
				if msg.Method == "session/event" && observer != nil {
					if err := observer.observe(msg.Params); err != nil {
						return nil, err
					}
					if expected == "" && observer.completed {
						return nil, nil
					}
				}
				continue
			}
			if msg.ID != expected || expected == "" {
				return nil, fmt.Errorf("ZCODE_TRANSPORT: response identity differs")
			}
			if len(msg.Error) > 0 && string(msg.Error) != "null" {
				return nil, fmt.Errorf("ZCODE_NATIVE: %s", msg.Error)
			}
			if len(msg.Result) == 0 {
				return nil, fmt.Errorf("ZCODE_TRANSPORT: missing response result")
			}
			return msg.Result, nil
		}
	}
	request := func(id, method string, params map[string]any) (json.RawMessage, error) {
		if err := encoder.Encode(zcodeRequest{ID: id, Method: method, Params: params}); err != nil {
			return nil, err
		}
		return read(id)
	}
	raw, err := request(session.ID, session.Method, session.Params)
	if err != nil {
		return "", nil, err
	}
	var snapshot struct {
		Session struct {
			ID string `json:"sessionId"`
		} `json:"session"`
	}
	if err := json.Unmarshal(raw, &snapshot); err != nil || snapshot.Session.ID == "" {
		return "", nil, fmt.Errorf("ZCODE_BINDING: native session identity missing")
	}
	identity = snapshot.Session.ID
	if session.Method == "session/resume" && session.Params["sessionId"] != identity {
		return "", nil, fmt.Errorf("ZCODE_BINDING: resumed session identity differs")
	}
	if err := attached(identity); err != nil {
		return identity, nil, err
	}
	var option zcodeSessionOptions
	if len(options) > 0 {
		option = options[0]
	}
	if option.PrepareTools != nil {
		servers, err := option.PrepareTools(identity)
		if err != nil {
			return identity, nil, err
		}
		if _, err := request(session.ID+":configure-close", "session/close", map[string]any{"sessionId": identity}); err != nil {
			return identity, nil, err
		}
		params := map[string]any{"sessionId": identity, "workspace": session.Params["workspace"], "mcpServers": servers, "toolAllowlist": session.Params["toolAllowlist"]}
		raw, err = request(session.ID+":configure-resume", "session/resume", params)
		if err != nil {
			return identity, nil, err
		}
		var resumed struct {
			Session struct {
				ID string `json:"sessionId"`
			} `json:"session"`
		}
		if json.Unmarshal(raw, &resumed) != nil || resumed.Session.ID != identity {
			return identity, nil, fmt.Errorf("ZCODE_BINDING: tool configuration resumed another session")
		}
	}
	if option.Mode != "" {
		if _, err := request(session.ID+":mode", "session/setMode", map[string]any{"sessionId": identity, "mode": option.Mode}); err != nil {
			return identity, nil, err
		}
	}
	if option.Model != "" {
		var settings struct {
			Settings struct {
				Model struct {
					Current struct {
						Provider string `json:"providerId"`
					} `json:"current"`
				} `json:"model"`
			} `json:"settings"`
		}
		if json.Unmarshal(raw, &settings) != nil || settings.Settings.Model.Current.Provider == "" {
			return identity, nil, fmt.Errorf("ZCODE_CONFIG: native model provider unavailable for explicit model")
		}
		if _, err := request(session.ID+":model", "session/setModel", map[string]any{"sessionId": identity, "model": map[string]any{"providerId": settings.Settings.Model.Current.Provider, "modelId": option.Model}, "persistAsWorkspaceLastUsed": false}); err != nil {
			return identity, nil, err
		}
	}
	if option.Effort != "" {
		if _, err := request(session.ID+":effort", "session/setThoughtLevel", map[string]any{"sessionId": identity, "thoughtLevel": option.Effort, "persistAsWorkspaceLastUsed": false}); err != nil {
			return identity, nil, err
		}
	}
	observer, err = newZCodeTurnObservation(identity, inputID)
	if err != nil {
		return identity, nil, err
	}
	raw, err = request(session.ID+":subscribe", "session/subscribe", map[string]any{"sessionId": identity, "deliveryKind": "desktop-continuous", "includeSnapshot": false})
	if err != nil {
		return identity, nil, err
	}
	var subscription struct {
		SessionID string            `json:"sessionId"`
		Events    []json.RawMessage `json:"events"`
	}
	if err := json.Unmarshal(raw, &subscription); err != nil || subscription.SessionID != identity {
		return identity, nil, fmt.Errorf("ZCODE_BINDING: subscription identity differs")
	}
	for _, event := range subscription.Events {
		if err := observer.observe(event); err != nil {
			return identity, nil, err
		}
	}
	raw, err = request(session.ID+":send", "session/send", map[string]any{"sessionId": identity, "inputId": inputID, "content": prompt})
	if err != nil {
		return identity, nil, err
	}
	var accepted struct {
		SessionID string `json:"sessionId"`
		Accepted  bool   `json:"accepted"`
	}
	if err := json.Unmarshal(raw, &accepted); err != nil || accepted.SessionID != identity || !accepted.Accepted {
		return identity, nil, fmt.Errorf("ZCODE_BINDING: input was not accepted by bound session")
	}
	if !observer.completed {
		if _, err := read(""); err != nil {
			return identity, nil, err
		}
	}
	answer, err := observer.result()
	if err != nil {
		return identity, nil, err
	}
	if option.CaptureUsage {
		_, _ = request(session.ID+":usage", "session/usage", map[string]any{"sessionId": identity})
	}
	if _, err := request(session.ID+":close", "session/close", map[string]any{"sessionId": identity}); err != nil {
		return identity, nil, err
	}
	return identity, answer, nil
}
