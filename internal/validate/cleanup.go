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
func CleanupTempRuns(root string) (TempRunCleanup, error) {
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
		state, err := LoadRunState(root, runID)
		switch {
		case err != nil:
			result.Unknown = append(result.Unknown, runID)
		case state.Status == "SEALED" || state.Status == "ABORTED":
			// Non-slice terminal runs are disposable only after their retained
			// summary exists. A missing summary means finalization failed after a
			// terminal state write; preserve the run and any slice-cost sidecars.
			if strings.TrimSpace(state.SplitMasterRunID) == "" {
				if _, err := os.Stat(RunSummaryPath(root, runID)); err != nil {
					result.Unknown = append(result.Unknown, runID)
					continue
				}
			}
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
// empty) are rejected. Deleting a run directory destroys workflow state, so
// the same candidate admission gate as every other mutating workflow
// entrypoint is enforced first: a run with a recorded registry binding may
// only be deleted by an invocation that still admits that binding, and a
// legacy run requires the registered stable launcher. A missing or unreadable
// state file carries no binding, so it keeps the legacy stable-launcher
// check.
func CleanupTempRun(root, runID string) (bool, error) {
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
	if state, err := LoadRunState(root, runID); err != nil {
		if err := verifyLegacyStableLauncher(); err != nil {
			return false, err
		}
	} else if err := requireRunWriteAdmission(root, state); err != nil {
		return false, err
	}
	if err := os.RemoveAll(dir); err != nil {
		return false, err
	}
	return true, nil
}
