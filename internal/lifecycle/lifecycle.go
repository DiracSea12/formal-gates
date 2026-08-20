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
	// Interrupted 是被中断派发的中断凭证：宿主只有 start 事件但已记录中断原因时，
	// 生命周期校验接受该派发为"已中断"而非 REJECTED。
	Interrupted = "INTERRUPTED"
)

type CaptureResult struct {
	Provider  string `json:"provider"`
	Event     string `json:"event"`
	Identity  string `json:"identity"`
	Duplicate bool   `json:"duplicate"`
	// Roots 是本次事件实际落盘的仓库根路径（宿主载荷派生的 project roots 或显式 --root），
	// 供生命周期触发面定位活动 run 以运行并行检查。
	Roots []string `json:"roots,omitempty"`
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
	roots, err := captureRoots(root, adapter, decoded)
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
	result := CaptureResult{Provider: adapter.name, Event: event, Identity: identity, Roots: roots}
	if identity == "" && correlation == "" {
		return result, nil
	}
	transcriptPath := ""
	if adapter.transcriptPath != nil {
		transcriptPath = adapter.transcriptPath(decoded)
	}
	// 从宿主 stop/error 事件提取中断原因（含 HTTP 错误码）写入事件记录；宿主
	// 未提供原因时记录"未知"。仅 stop/error 事件承载原因，start 事件不记录。
	reason := ""
	if event == eventStop && adapter.reason != nil {
		reason = adapter.reason(decoded)
		if reason == "" {
			reason = "未知"
		}
	}
	record := eventRecord{Provider: adapter.name, Event: event, Identity: identity, Correlation: correlation, TranscriptPath: transcriptPath, Reason: reason}
	duplicate := true
	for _, root := range roots {
		alreadyExists, err := recordEvent(root, record)
		if err != nil {
			return CaptureResult{}, err
		}
		duplicate = duplicate && alreadyExists
	}
	result.Duplicate = duplicate
	return result, nil
}

func captureRoots(root string, adapter providerAdapter, payload any) ([]string, error) {
	if root = strings.TrimSpace(root); root != "" {
		activeRoot, found, err := activeRunRoot(root)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, nil
		}
		return []string{activeRoot}, nil
	}
	candidates := adapter.projectRoots(payload)
	if len(candidates) == 0 {
		candidates = []string{"."}
	}
	activeRoots := []string{}
	for _, candidate := range candidates {
		activeRoot, found, err := activeRunRoot(candidate)
		if err != nil {
			return nil, err
		}
		if found {
			activeRoots = appendUniqueStrings(activeRoots, activeRoot)
		}
	}
	if len(activeRoots) > 0 {
		return activeRoots, nil
	}
	// Host lifecycle hooks are also installed outside formal runs. Do not
	// create an orphan event tree in that normal no-op case.
	return nil, nil
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

// ActiveRunIDs returns the ids of runs currently active at root (those whose
// lifecycle active marker exists). Used by the parallel-check trigger on
// the lifecycle hook path to locate the run state to read.
func ActiveRunIDs(root string) ([]string, error) {
	return activeRuns(root)
}

func BindDispatch(root, runID, dispatchID, identity string) error {
	provider, err := currentProvider()
	if err != nil {
		return err
	}
	return BindDispatchWithProvider(root, runID, dispatchID, identity, provider)
}

// BindDispatchWithProvider is the explicit host-context entrypoint used by a
// shared stable launcher. A host=both install intentionally has one launcher;
// provider identity therefore comes from the claim context, never from a
// launcher path plus an ambiguous working-directory heuristic.
func BindDispatchWithProvider(root, runID, dispatchID, identity, provider string) error {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return fmt.Errorf("lifecycle provider is required for an explicit dispatch claim")
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

// ResolveClaimIdentity returns the effective identity to bind for a dispatch
// claim. A preferred identity that matches an observed or pending subagent
// start observation is authoritative. Otherwise, when the host provider has
// exactly one pending start observation, that observation's identity is
// derived automatically so a claim with a missing or mismatched reviewer
// identity still binds the running subagent (common operator mistake
// compatibility). Zero pending observations fall back to the preferred
// identity because events may arrive after the claim; multiple pending
// observations require the exact preferred identity to avoid binding the
// wrong subagent.
func ResolveClaimIdentity(root, runID, preferred string) (string, error) {
	provider, err := currentProvider()
	if err != nil {
		return "", err
	}
	return ResolveClaimIdentityWithProvider(root, runID, preferred, provider)
}

func ResolveClaimIdentityWithProvider(root, runID, preferred, provider string) (string, error) {
	provider = strings.TrimSpace(provider)
	if provider == "" {
		return "", fmt.Errorf("lifecycle provider is required for an explicit dispatch claim")
	}
	if _, err := adapterFor(provider); err != nil {
		return "", err
	}
	preferred = strings.TrimSpace(preferred)
	records, err := pendingEvents(root, runID, provider)
	if err != nil {
		return "", err
	}
	starts := []string{}
	for _, record := range records {
		if record.Event == eventStart && record.Identity != "" {
			starts = appendUniqueStrings(starts, record.Identity)
		}
	}
	for _, identity := range starts {
		if identity == preferred {
			return preferred, nil
		}
	}
	switch len(starts) {
	case 0:
		if preferred != "" {
			return preferred, nil
		}
		return "", fmt.Errorf("reviewer identity is required when no subagent start observation exists")
	case 1:
		return starts[0], nil
	default:
		return "", fmt.Errorf("multiple pending subagent start observations (%d); claim with the exact reviewer identity", len(starts))
	}
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

// DispatchInterruptionReason returns the recorded interruption reason for a
// dispatch. The reason is captured from the host stop/error event at
// capture time and persisted both on the stop event record and in a dedicated
// dispatch-level reason record. The dedicated record takes precedence so the
// reason stays readable even when the stop event pairing is missing. A dispatch
// without a recorded reason yields an empty string.
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
