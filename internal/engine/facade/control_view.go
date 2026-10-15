package facade

import (
	"fmt"
	"path/filepath"
	"sort"

	"formal-gates/internal/engine/protocol"
)

// controlRun carries an actual query owner and its read-only projection.
type controlRun struct {
	owner *Facade
	run   Run
}

func (f *Facade) acquireControlReadWindow() (func(), error) {
	request, err := f.request()
	if err != nil {
		return nil, err
	}
	if request.MasterRunID == "" {
		return f.acquireNativeWindow()
	}
	master, err := Open(f.root, request.MasterRunID, f.runtimeFiles)
	if err != nil {
		return nil, err
	}
	parentUnlock, err := master.acquireNativeWindow()
	if err != nil {
		return nil, err
	}
	childUnlock, err := f.acquireNativeWindow()
	if err != nil {
		parentUnlock()
		return nil, err
	}
	return func() { childUnlock(); parentUnlock() }, nil
}

func (f *Facade) controlRuns() ([]controlRun, error) {
	run, err := f.Show()
	if err != nil {
		return nil, err
	}
	runs := []controlRun{{owner: f, run: run}}
	if run.Status == "COMPLETE" || run.Status == "ABORTED" || run.Status == "SLICE_READY" {
		return runs, nil
	}
	ids := make([]string, 0, len(run.SliceChildren))
	for _, id := range run.SliceChildren {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		established, err := runDirectoryEstablished(filepath.Join(f.root, EngineNamespace, id))
		if err != nil {
			return nil, err
		}
		if !established {
			continue
		}
		child, err := Open(f.root, id, f.runtimeFiles)
		if err != nil {
			return nil, err
		}
		current, err := child.Show()
		if err != nil {
			return nil, err
		}
		runs = append(runs, controlRun{owner: child, run: current})
	}
	return runs, nil
}

// ControlRuns reads the caller and established current children in the same
// order used to select an Ask. It does not create or synchronize child state.
func (f *Facade) ControlRuns() ([]Run, error) {
	unlock, err := f.acquireControlReadWindow()
	if err != nil {
		return nil, err
	}
	defer unlock()
	current, err := f.controlRuns()
	if err != nil {
		return nil, err
	}
	runs := make([]Run, len(current))
	for i, item := range current {
		runs[i] = item.run
	}
	return runs, nil
}

// PrepareCurrentControl selects only an existing Ask by its displayed number.
// The original target, request, token and semantic assembly own the candidate.
func (f *Facade) PrepareCurrentControl(input ControlInput) (ControlCandidate, error) {
	if input.Execution != nil && input.Request == "" {
		input.Request = protocol.ControlSelectExecution
	}
	if input.Node != 0 && input.Request == "" {
		input.Request = protocol.ControlContinueNode
	}
	unlock, err := f.acquireControlReadWindow()
	if err != nil {
		return ControlCandidate{}, err
	}
	defer unlock()
	current, err := f.controlRuns()
	if err != nil {
		return ControlCandidate{}, err
	}
	if input.ReviewVerification != nil {
		remaining := input.ReviewVerification.Result
		for _, item := range current {
			if remaining > 0 && remaining <= len(item.run.ReviewVerifications) {
				local := input
				value := *input.ReviewVerification
				value.Result = remaining
				local.ReviewVerification = &value
				candidate, err := item.owner.prepareControl(local, item.run)
				candidate.Input = input
				return candidate, err
			}
			remaining -= len(item.run.ReviewVerifications)
		}
		return ControlCandidate{}, fmt.Errorf("review verification position %d is outside the current %d choices", input.ReviewVerification.Result, input.ReviewVerification.Result-remaining)
	}
	if input.Request == protocol.ControlContinueNode {
		remaining := input.Node
		for _, item := range current {
			if remaining > 0 && remaining <= len(item.run.BusinessNodes) {
				local := input
				local.Node = remaining
				candidate, err := item.owner.prepareControl(local, item.run)
				candidate.Input = input
				return candidate, err
			}
			remaining -= len(item.run.BusinessNodes)
		}
		return ControlCandidate{}, fmt.Errorf("业务节点 position %d is outside the current %d choices", input.Node, input.Node-remaining)
	}
	if input.Request != "" && (input.Request != protocol.ControlRequestRequirementChange || input.Requirement == nil || input.Ask == 0) {
		return f.prepareControl(input, current[0].run)
	}
	remaining := input.Ask
	for _, item := range current {
		if remaining > 0 && remaining <= len(item.run.PendingAsks) {
			local := input
			local.Ask = remaining
			candidate, err := item.owner.prepareControl(local, item.run)
			candidate.Input = input
			return candidate, err
		}
		remaining -= len(item.run.PendingAsks)
	}
	return ControlCandidate{}, fmt.Errorf("Ask position %d is outside the current %d choices", input.Ask, input.Ask-remaining)
}
