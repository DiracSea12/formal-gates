package lifecycle

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/host"
)

const (
	Verified    = "VERIFIED"
	Rejected    = "REJECTED"
	Unavailable = "UNAVAILABLE"
)

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

// EndRun removes the run's lifecycle active marker (idempotent: a missing marker
// is success). After a run reaches its terminal state, active workspace observation
// no longer selects the completed run.
func EndRun(root, runID string) error {
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return fmt.Errorf("run id is required")
	}
	if err := os.Remove(filepath.Join(runLifecycleRoot(root, runID), "active")); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ActiveRunIDs resolves active run markers for native workspace observation.
func ActiveRunIDs(root string) ([]string, error) {
	return activeRuns(root)
}

// BindDispatchWithProvider is the explicit host-context entrypoint used by a
// shared stable launcher. Provider identity comes from the claim context,
// never from a launcher path plus an ambiguous working-directory heuristic.
func BindDispatchWithProvider(root, runID, dispatchID, identity, provider string) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return fmt.Errorf("lifecycle provider is required for an explicit dispatch claim")
	}
	adapter, err := host.Lookup(provider)
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
	return writeBinding(root, bindingRecord{RunID: runID, DispatchID: dispatchID, Provider: adapter.ID, Identity: identity})
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
	adapter, err := host.Lookup(binding.Provider)
	if err != nil {
		return Verification{}, err
	}
	result := Verification{Provider: binding.Provider, DispatchID: dispatchID, Identity: binding.Identity}
	if !adapter.LifecycleRequired {
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
