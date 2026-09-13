package facade

import (
	"fmt"

	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
)

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
	if q.Increment.Kind == "optimize" {
		if err := gitprep.CheckManifest(workspace, head, nil); err != nil {
			return fmt.Errorf("DEVELOPMENT_INCREMENT: finish the current Batch before reopening another; uncommitted work is preserved: %w", err)
		}
	}
	view, err := gitprep.WorkingView(workspace, head)
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
