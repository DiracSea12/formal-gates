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
	// Interrupted 是被中断派发的中断凭证：宿主只有 start 事件但已记录中断原因时，
	// 生命周期校验接受该派发为"已中断"而非 REJECTED。
	Interrupted = "INTERRUPTED"
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
	// 被中断派发接受"start 事件 + 已记录中断原因"作为中断凭证（而非 REJECTED）。
	// 子代理被中断（如 API 瞬时 429/503）时宿主可能只有 start 事件 + 中断原因；该派发
	// 仍须可经生命周期验证继续处置，而不是被当成无配对事件拒绝。
	if result.StartObserved {
		reason, reasonErr := DispatchInterruptionReason(root, binding.RunID, binding.DispatchID)
		if reasonErr != nil {
			return Verification{}, reasonErr
		}
		if strings.TrimSpace(reason) != "" {
			result.Outcome = Interrupted
			result.Diagnostic = "start event observed with recorded interruption reason: " + reason
			return result, nil
		}
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

// DispatchInterruptionReason reads a dispatch reason from its journal. Supported
// disk formats include the dedicated interruption record and the stop event.
func DispatchInterruptionReason(root, runID, dispatchID string) (string, error) {
	path := interruptionReasonPath(root, runID, dispatchID)
	if record, err := readEvent(path); err == nil && record.Reason != "" {
		return record.Reason, nil
	} else if err != nil && !os.IsNotExist(err) {
		return "", err
	}
	record, err := readEvent(runEventPath(root, runID, dispatchID, eventStop))
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return record.Reason, nil
}
