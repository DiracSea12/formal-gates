package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type eventRecord struct {
	Provider       string `json:"provider"`
	Event          string `json:"event"`
	Identity       string `json:"identity,omitempty"`
	Correlation    string `json:"correlation,omitempty"`
	TranscriptPath string `json:"transcriptPath,omitempty"`
	// Reason 是宿主 stop/error 事件携带的中断原因（含 HTTP 错误码 429/503/402 等），
	// 由 lifecycle capture 在子代理中断时自动提取并记录；宿主未提供时记
	// 录"未知"。供续用判定（三分支）与中断派发的生命周期凭证读取。
	Reason string `json:"reason,omitempty"`
}

type bindingRecord struct {
	RunID      string `json:"runId"`
	DispatchID string `json:"dispatchId"`
	Provider   string `json:"provider"`
	Identity   string `json:"identity"`
}

func recordEvent(root string, record eventRecord) (bool, error) {
	bindings, err := matchingBindings(root, record)
	if err != nil {
		return false, err
	}
	if len(bindings) > 1 {
		return false, fmt.Errorf("lifecycle event matches more than one active dispatch")
	}
	if len(bindings) == 0 {
		runIDs, err := activeRuns(root)
		if err != nil {
			return false, err
		}
		duplicate := len(runIDs) > 0
		for _, runID := range runIDs {
			alreadyExists, err := writeEventAt(pendingEventPath(root, runID, record), record)
			if err != nil {
				return false, err
			}
			duplicate = duplicate && alreadyExists
		}
		return duplicate, nil
	}
	binding := bindings[0]
	duplicate, err := writeEventAt(runEventPath(root, binding.RunID, binding.DispatchID, record.Event), record)
	if err != nil {
		return false, err
	}
	// stop 事件带中断原因时写入派发级原因记录，供"start + 已记录中断原因"
	// 作为被中断派发的中断凭证读取（即使 stop 配对事件缺失也能读到原因）。
	if record.Event == eventStop && record.Reason != "" {
		if err := writeInterruptionReason(root, binding.RunID, binding.DispatchID, record.Reason); err != nil {
			return false, err
		}
	}
	if record.Event == eventStart && record.Correlation != "" {
		if err := bindPendingStops(root, binding, record.Correlation); err != nil {
			return false, err
		}
	}
	return duplicate, nil
}

func writeBinding(root string, record bindingRecord) error {
	path := bindingPath(root, record.RunID, record.DispatchID)
	if existing, found, err := readBinding(root, record.RunID, record.DispatchID); err != nil {
		return err
	} else if found {
		if existing != record {
			return fmt.Errorf("lifecycle provider and identity are immutable for dispatch %s", record.DispatchID)
		}
		return bindPending(root, existing)
	}
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		existing, found, readErr := readBinding(root, record.RunID, record.DispatchID)
		if readErr != nil {
			return readErr
		}
		if !found || existing != record {
			return fmt.Errorf("lifecycle provider and identity are immutable for dispatch %s", record.DispatchID)
		}
		return bindPending(root, existing)
	}
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return bindPending(root, record)
}

func bindPending(root string, binding bindingRecord) error {
	records, err := pendingEvents(root, binding.RunID, binding.Provider)
	if err != nil {
		return err
	}
	correlation := ""
	for path, record := range records {
		if record.Event != eventStart || record.Identity != binding.Identity {
			continue
		}
		if _, err := writeEventAt(runEventPath(root, binding.RunID, binding.DispatchID, eventStart), record); err != nil {
			return err
		}
		correlation = record.Correlation
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	for path, record := range records {
		if record.Event != eventStop {
			continue
		}
		if record.Identity != binding.Identity && (correlation == "" || record.Correlation != correlation) {
			continue
		}
		if _, err := writeEventAt(runEventPath(root, binding.RunID, binding.DispatchID, eventStop), record); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func bindPendingStops(root string, binding bindingRecord, correlation string) error {
	records, err := pendingEvents(root, binding.RunID, binding.Provider)
	if err != nil {
		return err
	}
	for path, record := range records {
		if record.Event != eventStop || record.Correlation != correlation {
			continue
		}
		if _, err := writeEventAt(runEventPath(root, binding.RunID, binding.DispatchID, eventStop), record); err != nil {
			return err
		}
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

func matchingBindings(root string, record eventRecord) ([]bindingRecord, error) {
	paths, err := filepath.Glob(filepath.Join(CleanRoot(root), ".gates", "tmp", "*", "lifecycle", "*.json"))
	if err != nil {
		return nil, err
	}
	matches := []bindingRecord{}
	for _, path := range paths {
		binding, err := readBindingPath(path)
		if err != nil {
			return nil, err
		}
		if binding.Provider != record.Provider {
			continue
		}
		if record.Identity != "" && binding.Identity == record.Identity {
			matches = append(matches, binding)
			continue
		}
		if record.Event != eventStop || record.Correlation == "" {
			continue
		}
		start, err := readEvent(runEventPath(root, binding.RunID, binding.DispatchID, eventStart))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if start.Provider == record.Provider && start.Correlation == record.Correlation {
			matches = append(matches, binding)
		}
	}
	return matches, nil
}

func readBinding(root, runID, dispatchID string) (bindingRecord, bool, error) {
	path := bindingPath(root, runID, dispatchID)
	record, err := readBindingPath(path)
	if os.IsNotExist(err) {
		return bindingRecord{}, false, nil
	}
	return record, err == nil, err
}

func readBindingPath(path string) (bindingRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return bindingRecord{}, err
	}
	var record bindingRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return bindingRecord{}, fmt.Errorf("lifecycle dispatch binding is invalid: %w", err)
	}
	return record, nil
}

// DispatchTranscriptPath returns the provider and stored subagent transcript
// path for a dispatch's stop event. A dispatch without a binding or without
// a stop event carrying a transcript path yields empty values.
func DispatchTranscriptPath(root, runID, dispatchID string) (string, string, error) {
	binding, found, err := readBinding(root, runID, dispatchID)
	if err != nil || !found {
		return "", "", err
	}
	record, err := readEvent(runEventPath(root, runID, dispatchID, eventStop))
	if os.IsNotExist(err) {
		return binding.Provider, "", nil
	}
	if err != nil {
		return "", "", err
	}
	if record.Provider != binding.Provider {
		return "", "", fmt.Errorf("lifecycle event journal entry is inconsistent")
	}
	return binding.Provider, record.TranscriptPath, nil
}

// writeInterruptionReason persists a dispatch-level interruption reason record
// The record lives next to the dispatch's event journal so the reason
// is readable even when the stop event pairing is missing or was cleaned up.
func writeInterruptionReason(root, runID, dispatchID, reason string) error {
	data, err := json.MarshalIndent(eventRecord{Reason: reason}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := interruptionReasonPath(root, runID, dispatchID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		return nil // 首个原因已记录，保留最初的客观原因
	}
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}

func interruptionReasonPath(root, runID, dispatchID string) string {
	return filepath.Join(runLifecycleRoot(root, runID), "events", digest(dispatchID), "interruption.json")
}

func eventExists(root, runID, dispatchID, provider, identity, event string) (bool, error) {
	path := runEventPath(root, runID, dispatchID, event)
	record, err := readEvent(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if record.Provider != provider || record.Event != event {
		return false, fmt.Errorf("lifecycle event journal entry is inconsistent")
	}
	if event == eventStart && record.Identity != identity {
		return false, fmt.Errorf("lifecycle event journal entry is inconsistent")
	}
	return true, nil
}

func writeEventAt(path string, record eventRecord) (bool, error) {
	data, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return false, err
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return false, err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if os.IsExist(err) {
		existing, readErr := readEvent(path)
		if readErr != nil {
			return false, readErr
		}
		if existing != record {
			return false, fmt.Errorf("lifecycle event journal entry is inconsistent")
		}
		return true, nil
	}
	if err != nil {
		return false, err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return false, err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return false, err
	}
	return false, file.Close()
}

func readEvent(path string) (eventRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return eventRecord{}, err
	}
	var record eventRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return eventRecord{}, fmt.Errorf("lifecycle event journal entry is invalid: %w", err)
	}
	return record, nil
}

func pendingEvents(root, runID, provider string) (map[string]eventRecord, error) {
	paths, err := filepath.Glob(filepath.Join(pendingRoot(root, runID), provider, "*", "*.json"))
	if err != nil {
		return nil, err
	}
	records := make(map[string]eventRecord, len(paths))
	for _, path := range paths {
		record, err := readEvent(path)
		if err != nil {
			return nil, err
		}
		records[path] = record
	}
	return records, nil
}

func pendingEventPath(root, runID string, record eventRecord) string {
	key := record.Provider + "\x00" + record.Event + "\x00" + record.Identity + "\x00" + record.Correlation
	return filepath.Join(pendingRoot(root, runID), record.Provider, record.Event, digest(key)+".json")
}

func pendingRoot(root, runID string) string {
	return filepath.Join(runLifecycleRoot(root, runID), "pending")
}

func activeRuns(root string) ([]string, error) {
	paths, err := filepath.Glob(filepath.Join(CleanRoot(root), ".gates", "tmp", "*", "lifecycle", "active"))
	if err != nil {
		return nil, err
	}
	runIDs := make([]string, 0, len(paths))
	for _, path := range paths {
		runIDs = append(runIDs, filepath.Base(filepath.Dir(filepath.Dir(path))))
	}
	return runIDs, nil
}

func runLifecycleRoot(root, runID string) string {
	return filepath.Join(CleanRoot(root), ".gates", "tmp", runID, "lifecycle")
}

func runEventPath(root, runID, dispatchID, event string) string {
	return filepath.Join(runLifecycleRoot(root, runID), "events", digest(dispatchID), event+".json")
}

func bindingPath(root, runID, dispatchID string) string {
	return filepath.Join(runLifecycleRoot(root, runID), digest(dispatchID)+".json")
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func CleanRoot(root string) string {
	if strings.TrimSpace(root) == "" {
		return "."
	}
	return root
}
