package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/engine/facade"
)

// selectWorkflowRun resolves a public reentry from existing engine authority.
// Multiple top-level runs require a displayed position; no run pointer is saved.
func selectWorkflowRun(root string, runID *string, position int, out io.Writer) (*facade.Facade, bool, error) {
	if position < 0 || position != 0 && strings.TrimSpace(*runID) != "" {
		return nil, true, fmt.Errorf("select a run number or an exact run-id, not both")
	}
	if strings.TrimSpace(*runID) != "" {
		f, _, err := openEngineIfPresent(root, *runID)
		return f, true, err
	}
	entries, err := os.ReadDir(filepath.Join(root, facade.EngineNamespace))
	if os.IsNotExist(err) {
		if position != 0 {
			return nil, true, fmt.Errorf("run number is not in the current choices")
		}
		return nil, true, nil
	}
	if err != nil {
		return nil, true, err
	}
	var runs []facade.Run
	var opened []*facade.Facade
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(root, facade.EngineNamespace, entry.Name())
		if _, err := os.Stat(filepath.Join(directory, "state.json")); os.IsNotExist(err) {
			if _, err := os.Stat(filepath.Join(directory, "terminal-summary.json")); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return nil, true, err
			}
		} else if err != nil {
			return nil, true, err
		}
		f, _, err := openEngineIfPresent(root, entry.Name())
		if err != nil {
			return nil, true, err
		}
		run, err := f.Show()
		if err != nil {
			return nil, true, err
		}
		if run.MasterRunID != "" {
			continue
		}
		runs, opened = append(runs, run), append(opened, f)
	}
	if position > len(runs) {
		return nil, true, fmt.Errorf("run number is not in the current choices")
	}
	if position == 0 && len(runs) > 1 {
		choices := make([]map[string]any, 0, len(runs))
		for index, run := range runs {
			choice := map[string]any{"run": index + 1, "runId": run.RunID, "status": run.Status, "route": run.Route}
			if run.IntakeConfirmationReceipt != nil {
				choice["requirement"] = run.IntakeConfirmationReceipt.RequirementSource
			}
			choices = append(choices, choice)
		}
		_, err := printJSON(out, map[string]any{"runs": choices, "action": "Select the target with --run and its displayed number"})
		return nil, false, err
	}
	if len(runs) == 0 {
		return nil, true, nil
	}
	if position == 0 {
		position = 1
	}
	*runID = runs[position-1].RunID
	return opened[position-1], true, nil
}
