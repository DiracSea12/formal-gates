package validate

import (
	"os"
	"path/filepath"

	"formal-gates/internal/lifecycle"
)

// FindingVerification is intentionally per finding. One batch-level operator
// paragraph cannot stand in for independent scope, severity, causality,
// reproduction, and (for Start Readiness) classification checks.
type FindingVerification struct {
	Scope          string `json:"scope,omitempty"`
	Severity       string `json:"severity,omitempty"`
	Causality      string `json:"causality,omitempty"`
	Reproduction   string `json:"reproduction,omitempty"`
	Classification string `json:"classification,omitempty"`
}

func RunDir(root, runID string) string {
	return filepath.Join(lifecycle.CleanRoot(root), ".gates", "tmp", runID)
}

func RunStatePath(root, runID string) string {
	return filepath.Join(RunDir(root, runID), "state.json")
}

func RunSummaryPath(root, runID string) string {
	return filepath.Join(lifecycle.CleanRoot(root), ".gates", "results", runID+".json")
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceCompletedFile(tmpName, path)
}
