package lifecycle

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type eventRecord struct {
	Provider string `json:"provider"`
	Event    string `json:"event"`
	Identity string `json:"identity"`
}

type bindingRecord struct {
	RunID      string `json:"runId"`
	DispatchID string `json:"dispatchId"`
	Provider   string `json:"provider"`
	Identity   string `json:"identity"`
}

func recordEvent(root string, record eventRecord) (bool, error) {
	path := eventPath(root, record.Provider, record.Identity, record.Event)
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
	return file.Close()
}

func readBinding(root, runID, dispatchID string) (bindingRecord, bool, error) {
	path := bindingPath(root, runID, dispatchID)
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return bindingRecord{}, false, nil
	}
	if err != nil {
		return bindingRecord{}, false, err
	}
	var record bindingRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return bindingRecord{}, false, fmt.Errorf("lifecycle dispatch binding is invalid: %w", err)
	}
	return record, true, nil
}

func eventExists(root, provider, identity, event string) (bool, error) {
	path := eventPath(root, provider, identity, event)
	record, err := readEvent(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if record.Provider != provider || record.Identity != identity || record.Event != event {
		return false, fmt.Errorf("lifecycle event journal entry is inconsistent")
	}
	return true, nil
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

func eventPath(root, provider, identity, event string) string {
	return filepath.Join(cleanRoot(root), ".gates", "tmp", "lifecycle", "events", provider, digest(identity), event+".json")
}

func bindingPath(root, runID, dispatchID string) string {
	return filepath.Join(cleanRoot(root), ".gates", "tmp", runID, "lifecycle", digest(dispatchID)+".json")
}

func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func cleanRoot(root string) string {
	if root == "" {
		return "."
	}
	return filepath.Clean(root)
}
