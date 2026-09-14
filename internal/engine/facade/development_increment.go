package facade

import (
	"encoding/hex"
	"fmt"
	"strings"

	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
)

// Select by the issued task, not the latest receipt: older tasks can still be
// resumed after another increment has been confirmed. Do not copy the previous
// plan or other batches' task/disposition maps into this role's material.
func developmentIncrementInput(run Run, claim protocol.ExecutionClaim) (map[string]any, string, error) {
	for _, receipt := range run.Regular.DevelopmentIncrements {
		for batch, task := range receipt.Tasks {
			if task != claim.Task {
				continue
			}
			return map[string]any{
				"id": receipt.ID, "decisionEvent": receipt.DecisionEvent,
				"revision": receipt.Revision, "requirementRevision": receipt.RequirementRevision,
				"kind": receipt.Kind, "batchId": batch, "task": task,
				"reason": receipt.Reason,
			}, batch, nil
		}
	}
	// The protocol appends a SHA-256 decision digest to increment task scopes.
	// Ordinary Batch names may themselves contain the word "increment".
	if i := strings.LastIndex(claim.Task.Scope, "-increment-"); i >= 0 {
		digest := claim.Task.Scope[i+len("-increment-"):]
		if _, err := hex.DecodeString(digest); err == nil && len(digest) == 64 {
			return nil, "", fmt.Errorf("DEVELOPMENT_INCREMENT: confirmed receipt missing for task %s", claim.Task)
		}
	}
	return nil, "", nil
}

// The ordinary Facade boundary freezes the actual workspace in the Ask. The
// engine remains the only writer of plan, lineage and incremental authorization.
func (f *Facade) verifyDevelopmentIncrement(event *protocol.Event) error {
	v := event.Regular
	if v == nil || (v.Operation != "REQUEST" && v.Operation != "DECIDE") {
		return nil
	}
	if v.Operation == "REQUEST" && (v.Request == nil || v.Request.Action != "development-increment") {
		return nil
	}
	run, err := f.Show()
	if err != nil {
		return err
	}
	var q *protocol.RegularRequest
	capture := v.Operation == "REQUEST"
	if capture {
		q = v.Request
	} else if v.Decision != nil && v.Decision.Choice != "cancel" && run.Regular != nil && run.Regular.Ask != nil && run.Regular.Ask.Kind == "development-increment" {
		q = &run.Regular.Ask.Request
	}
	if q == nil {
		return nil
	}
	if q.Increment == nil || run.Regular == nil || run.Regular.Preparation == nil {
		return fmt.Errorf("DEVELOPMENT_INCREMENT: increment and active development workspace required")
	}
	p := run.Regular.Preparation
	workspace := p.Resources["development"]
	head, err := gitprep.Head(workspace)
	if err != nil {
		return err
	}
	if head != p.DevelopmentSnapshot {
		return fmt.Errorf("DEVELOPMENT_INCREMENT: reconcile the development snapshot first")
	}
	concurrent := false
	for _, x := range run.Executions {
		if x.Claim.Task.Step == "development-worker" && x.Claimed && !x.Finished {
			concurrent = true
		}
	}
	if q.Increment.Kind == "optimize" && !concurrent {
		if err := gitprep.CheckManifest(workspace, head, nil); err != nil {
			return fmt.Errorf("DEVELOPMENT_INCREMENT: finish the current Batch before reopening another; uncommitted work is preserved: %w", err)
		}
	}
	var view gitprep.View
	if concurrent {
		// The new worker starts from committed inputs in its own worktree.
		// Another authorized worker may keep editing its existing files.
		view, err = gitprep.Snapshot(workspace, head)
	} else {
		view, err = gitprep.WorkingView(workspace, head)
	}
	if err != nil {
		return err
	}
	if capture {
		q.Increment.WorkspaceView = view.Digest
	} else if q.Increment.WorkspaceView != view.Digest {
		return fmt.Errorf("STALE_FRESHNESS: development workspace changed after increment preview")
	}
	return nil
}
