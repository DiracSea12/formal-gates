package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/lifecycle"
)

// TempRunCleanup reports the outcome of a temp-run sweep.
type TempRunCleanup struct {
	Cleaned []string `json:"cleaned"`
	Active  []string `json:"active"`
	Unknown []string `json:"unknown"`
}

// CleanupTempRuns removes residual temp directories of terminated runs and
// lists every other temp directory without touching it. A run is terminated
// when its state ledger says SEALED or ABORTED; its temp directory should
// already have been removed by Seal or Abort, so any residue is junk. Active
// runs are never removed automatically: a live run may sit without lifecycle
// events for hours while waiting on reviewers, and deleting it would destroy
// resumable flow state. Directories without a readable state ledger
// (corrupted or foreign state) are listed and left alone.
// packageRoot is accepted so the workflow command surface stays uniform (every
// workflow subcommand carries --package-root); the temp sweep is scoped to root
// only, so packageRoot is not consulted here.
func CleanupTempRuns(root, packageRoot string) (TempRunCleanup, error) {
	result := TempRunCleanup{}
	dirs, err := os.ReadDir(filepath.Join(lifecycle.CleanRoot(root), ".gates", "tmp"))
	if os.IsNotExist(err) {
		return result, nil
	}
	if err != nil {
		return result, err
	}
	for _, dir := range dirs {
		if !dir.IsDir() {
			continue
		}
		runID := dir.Name()
		status, err := readTempRunStatus(root, runID)
		switch {
		case err != nil:
			result.Unknown = append(result.Unknown, runID)
		case status == "SEALED" || status == "ABORTED":
			if err := os.RemoveAll(RunDir(root, runID)); err != nil {
				return result, fmt.Errorf("cleanup %s: %w", runID, err)
			}
			result.Cleaned = append(result.Cleaned, runID)
		default:
			result.Active = append(result.Active, runID)
		}
	}
	return result, nil
}

// CleanupTempRun deletes one named run temp directory, terminated or not.
// This is the explicit escape hatch for abandoned active runs; it never
// touches .gates/results. Unsafe names (path separators, "." and "..",
// empty) are rejected. packageRoot is accepted for the uniform workflow
// command surface; the single-run temp deletion is scoped to root only.
func CleanupTempRun(root, packageRoot, runID string) (bool, error) {
	runID = strings.TrimSpace(runID)
	if runID == "" || runID == "." || runID == ".." ||
		strings.ContainsAny(runID, `/\`) {
		return false, fmt.Errorf("unsafe run id %q", runID)
	}
	dir := RunDir(root, runID)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return false, nil
	} else if err != nil {
		return false, err
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, err
	}
	return true, nil
}

func readTempRunStatus(root, runID string) (string, error) {
	state, err := LoadRunState(root, runID)
	if err != nil {
		return "", err
	}
	return state.Status, nil
}
