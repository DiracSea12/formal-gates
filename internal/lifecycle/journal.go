package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/host"
)

type eventRecord struct {
	Provider string `json:"provider"`
	Event    string `json:"event"`
	Identity string `json:"identity,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type bindingRecord struct {
	RunID      string `json:"runId"`
	DispatchID string `json:"dispatchId"`
	Provider   string `json:"provider"`
	Identity   string `json:"identity"`
}

// ObserveNativeCompletion records verified native start/terminal facts for an
// already bound dispatch in the journal used by lifecycle and cost checks.
func ObserveNativeCompletion(root, runID, dispatchID, provider, identity string) error {
	return observeNativeEvents(root, runID, dispatchID, provider, identity, eventStart, eventStop)
}

func ObserveNativeStart(root, runID, dispatchID, provider, identity string) error {
	return observeNativeEvents(root, runID, dispatchID, provider, identity, eventStart)
}

func observeNativeEvents(root, runID, dispatchID, provider, identity string, events ...string) error {
	adapter, err := host.Lookup(provider)
	if err != nil {
		return err
	}
	provider = adapter.ID
	binding, found, err := readBinding(root, runID, dispatchID)
	if err != nil {
		return err
	}
	if !found || binding.Provider != provider || binding.Identity != identity {
		return fmt.Errorf("native completion differs from its original dispatch binding")
	}
	for _, event := range events {
		if existing, readErr := readEvent(runEventPath(root, runID, dispatchID, event)); readErr == nil {
			if existing.Provider != provider || existing.Identity != identity {
				return fmt.Errorf("native completion differs from the original lifecycle fact")
			}
			continue
		} else if !os.IsNotExist(readErr) {
			return readErr
		}
		record := eventRecord{Provider: provider, Event: event, Identity: identity}
		if err := writeEventAt(runEventPath(root, binding.RunID, binding.DispatchID, record.Event), record); err != nil {
			return err
		}
	}
	return nil
}

func writeBinding(root string, record bindingRecord) error {
	path := bindingPath(root, record.RunID, record.DispatchID)
	if existing, found, err := readBinding(root, record.RunID, record.DispatchID); err != nil {
		return err
	} else if found {
		if existing != record {
			return fmt.Errorf("lifecycle provider and identity are immutable for dispatch %s", record.DispatchID)
		}
		return nil
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
		return nil
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
	return nil
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

func writeEventAt(path string, record eventRecord) error {
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
		existing, readErr := readEvent(path)
		if readErr != nil {
			return readErr
		}
		if existing != record {
			return fmt.Errorf("lifecycle event journal entry is inconsistent")
		}
		return nil
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
	return file.Close()
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
