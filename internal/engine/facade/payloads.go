package facade

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

func OriginalPayloadPaths(directory string) ([]string, error) {
	return filepath.Glob(filepath.Join(directory, "payload-*"))
}

func SaveOriginalPayload(directory string, body []byte) (string, int, error) {
	if err := os.MkdirAll(directory, 0700); err != nil {
		return "", 0, err
	}
	paths, err := OriginalPayloadPaths(directory)
	if err != nil {
		return "", 0, err
	}
	for i, path := range paths {
		original, err := os.ReadFile(path)
		if err != nil {
			return "", 0, err
		}
		if string(original) == string(body) {
			return path, i + 1, nil
		}
	}
	path := ""
	for ordinal := len(paths) + 1; ; ordinal++ {
		path = filepath.Join(directory, fmt.Sprintf("payload-%09d", ordinal))
		err = WriteOriginalPayload(path, body, true)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", 0, err
		}
		break
	}
	paths, err = OriginalPayloadPaths(directory)
	for i, original := range paths {
		if original == path {
			return path, i + 1, err
		}
	}
	return "", 0, err
}

func SaveOriginalCandidate(directory string, value any) (int, error) {
	body, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return 0, err
	}
	_, position, err := SaveOriginalPayload(directory, append(body, '\n'))
	return position, err
}

// WriteOriginalPayload preserves exact-save and exclusive numbered publication.
func WriteOriginalPayload(path string, body []byte, exclusive bool) error {
	return publishFileAtomic(path, body, 0o600, exclusive)
}
