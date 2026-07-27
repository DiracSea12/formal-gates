package lifecycle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	Verified    = "VERIFIED"
	Rejected    = "REJECTED"
	Unavailable = "UNAVAILABLE"
)

type CaptureResult struct {
	Provider  string `json:"provider"`
	Event     string `json:"event"`
	Identity  string `json:"identity"`
	Duplicate bool   `json:"duplicate"`
}

type Verification struct {
	Outcome       string `json:"outcome"`
	Provider      string `json:"provider,omitempty"`
	DispatchID    string `json:"dispatchId"`
	Identity      string `json:"identity,omitempty"`
	StartObserved bool   `json:"startObserved"`
	StopObserved  bool   `json:"stopObserved"`
	Diagnostic    string `json:"diagnostic"`
}

func Capture(root, provider, eventName string, payload []byte) (CaptureResult, error) {
	adapter, err := adapterFor(provider)
	if err != nil {
		return CaptureResult{}, err
	}
	event, err := adapter.normalizeEvent(eventName)
	if err != nil {
		return CaptureResult{}, err
	}
	var decoded any
	if err := json.Unmarshal(bytes.TrimPrefix(payload, []byte{0xef, 0xbb, 0xbf}), &decoded); err != nil {
		return CaptureResult{}, fmt.Errorf("host lifecycle payload is not valid JSON: %w", err)
	}
	identity := adapter.identity(decoded)
	if identity == "" {
		return CaptureResult{}, fmt.Errorf("%s %s payload is missing a host agent identity", adapter.name, eventName)
	}
	duplicate, err := recordEvent(root, eventRecord{Provider: adapter.name, Event: event, Identity: identity})
	if err != nil {
		return CaptureResult{}, err
	}
	return CaptureResult{Provider: adapter.name, Event: event, Identity: identity, Duplicate: duplicate}, nil
}

func BindDispatch(root, runID, dispatchID, provider, identity string) error {
	adapter, err := adapterFor(provider)
	if err != nil {
		return err
	}
	runID, dispatchID, identity = strings.TrimSpace(runID), strings.TrimSpace(dispatchID), strings.TrimSpace(identity)
	for name, value := range map[string]string{"run id": runID, "dispatch id": dispatchID, "host agent identity": identity} {
		if value == "" {
			return fmt.Errorf("%s is required", name)
		}
	}
	return writeBinding(root, bindingRecord{RunID: runID, DispatchID: dispatchID, Provider: adapter.name, Identity: identity})
}

func VerifyDispatch(root, runID, dispatchID string) (Verification, error) {
	runID, dispatchID = strings.TrimSpace(runID), strings.TrimSpace(dispatchID)
	if runID == "" || dispatchID == "" {
		return Verification{}, fmt.Errorf("run id and dispatch id are required")
	}
	binding, found, err := readBinding(root, runID, dispatchID)
	if err != nil {
		return Verification{}, err
	}
	if !found {
		return Verification{Outcome: Rejected, DispatchID: dispatchID, Diagnostic: "dispatch has no lifecycle binding"}, nil
	}
	adapter, err := adapterFor(binding.Provider)
	if err != nil {
		return Verification{}, err
	}
	result := Verification{Provider: binding.Provider, DispatchID: dispatchID, Identity: binding.Identity}
	if !adapter.required {
		result.Outcome = Unavailable
		result.Diagnostic = "provider does not expose usable lifecycle events; dispatch identity checks remain authoritative"
		return result, nil
	}
	result.StartObserved, err = eventExists(root, binding.Provider, binding.Identity, eventStart)
	if err != nil {
		return Verification{}, err
	}
	result.StopObserved, err = eventExists(root, binding.Provider, binding.Identity, eventStop)
	if err != nil {
		return Verification{}, err
	}
	if result.StartObserved && result.StopObserved {
		result.Outcome = Verified
		result.Diagnostic = "matching start and stop events observed"
		return result, nil
	}
	result.Outcome = Rejected
	missing := []string{}
	if !result.StartObserved {
		missing = append(missing, "start")
	}
	if !result.StopObserved {
		missing = append(missing, "stop")
	}
	result.Diagnostic = "missing matching " + strings.Join(missing, " and ") + " event"
	return result, nil
}
