package cli

import (
	"encoding/json"
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
	type runChoice struct {
		id      string
		view    map[string]any
		facade  *facade.Facade
		openErr error
	}
	var runs []runChoice
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		directory := filepath.Join(root, facade.EngineNamespace, entry.Name())
		statePath := filepath.Join(directory, "state.json")
		if _, err := os.Stat(statePath); os.IsNotExist(err) {
			statePath = filepath.Join(directory, "terminal-summary.json")
			if _, err := os.Stat(statePath); os.IsNotExist(err) {
				continue
			} else if err != nil {
				return nil, true, err
			}
		} else if err != nil {
			return nil, true, err
		}
		f, _, openErr := openEngineIfPresent(root, entry.Name())
		err := openErr
		var run facade.Run
		if err == nil {
			run, err = f.Show()
		}
		choice := map[string]any{"runId": entry.Name()}
		if err != nil {
			// An unavailable run remains a visible choice. Its immutable request
			// identifies children; only raw Diagnose may inspect its old envelope.
			body, readErr := os.ReadFile(filepath.Join(directory, "request.json"))
			var request facade.StartRequest
			if readErr != nil || json.Unmarshal(body, &request) != nil {
				return nil, true, err
			}
			if request.MasterRunID != "" {
				continue
			}
			diagnostic, diagnosticErr := facade.Diagnose(statePath, "", "")
			if diagnosticErr != nil {
				return nil, true, err
			}
			choice["route"], choice["status"] = request.Route, diagnostic.Summary["status"]
			choice["available"], choice["error"] = false, err.Error()
		} else {
			if run.MasterRunID != "" {
				continue
			}
			choice["status"], choice["route"] = run.Status, run.Route
			if run.IntakeConfirmationReceipt != nil {
				choice["requirement"] = run.IntakeConfirmationReceipt.RequirementSource
			}
		}
		runs = append(runs, runChoice{id: entry.Name(), view: choice, facade: f, openErr: openErr})
	}
	if position > len(runs) {
		return nil, true, fmt.Errorf("run number is not in the current choices")
	}
	if position == 0 && len(runs) > 1 {
		choices := make([]map[string]any, 0, len(runs))
		for index, run := range runs {
			run.view["run"] = index + 1
			choices = append(choices, run.view)
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
	selected := runs[position-1]
	*runID = selected.id
	return selected.facade, true, selected.openErr
}
