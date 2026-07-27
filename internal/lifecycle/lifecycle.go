package lifecycle

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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

func BeginRun(root, runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("run id is required")
	}
	path := runLifecycleRoot(root, runID)
	if err := os.MkdirAll(path, 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(filepath.Join(path, "active"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return file.Close()
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
	root, err = captureRoot(root, adapter, decoded)
	if err != nil {
		return CaptureResult{}, err
	}
	identity := adapter.identity(event, decoded)
	correlation := adapter.correlation(event, decoded)
	if event == eventStart && identity == "" && adapter.required {
		return CaptureResult{}, fmt.Errorf("%s %s payload is missing a host agent identity", adapter.name, eventName)
	}
	if event == eventStop && identity == "" && correlation == "" && adapter.required {
		return CaptureResult{}, fmt.Errorf("%s %s payload cannot be correlated to a host agent", adapter.name, eventName)
	}
	result := CaptureResult{Provider: adapter.name, Event: event, Identity: identity}
	if identity == "" && correlation == "" {
		return result, nil
	}
	duplicate, err := recordEvent(root, eventRecord{Provider: adapter.name, Event: event, Identity: identity, Correlation: correlation})
	if err != nil {
		return CaptureResult{}, err
	}
	result.Duplicate = duplicate
	return result, nil
}

func captureRoot(root string, adapter providerAdapter, payload any) (string, error) {
	if root = strings.TrimSpace(root); root != "" {
		return root, nil
	}
	candidates := adapter.projectRoots(payload)
	if len(candidates) == 0 {
		candidates = []string{"."}
	}
	activeRoots := []string{}
	for _, candidate := range candidates {
		activeRoot, found, err := activeRunRoot(candidate)
		if err != nil {
			return "", err
		}
		if found {
			activeRoots = appendUniqueStrings(activeRoots, activeRoot)
		}
	}
	if len(activeRoots) == 1 {
		return activeRoots[0], nil
	}
	if len(activeRoots) > 1 {
		return "", fmt.Errorf("%s lifecycle payload matches multiple project roots with active formal runs", adapter.name)
	}
	return candidates[0], nil
}

func activeRunRoot(candidate string) (string, bool, error) {
	root, err := filepath.Abs(strings.TrimSpace(candidate))
	if err != nil {
		return "", false, err
	}
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		root = resolved
	}
	for {
		runIDs, err := activeRuns(root)
		if err != nil {
			return "", false, err
		}
		if len(runIDs) > 0 {
			return root, true, nil
		}
		parent := filepath.Dir(root)
		if parent == root {
			return "", false, nil
		}
		root = parent
	}
}

func BindDispatch(root, runID, dispatchID, identity string) error {
	provider, err := currentProvider()
	if err != nil {
		return err
	}
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
	if err := BeginRun(root, runID); err != nil {
		return err
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
	result.StartObserved, err = eventExists(root, binding.RunID, binding.DispatchID, binding.Provider, binding.Identity, eventStart)
	if err != nil {
		return Verification{}, err
	}
	result.StopObserved, err = eventExists(root, binding.RunID, binding.DispatchID, binding.Provider, binding.Identity, eventStop)
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
