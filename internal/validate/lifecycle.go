package validate

import (
	"fmt"

	"formal-gates/internal/lifecycle"
)

type workflowLifecycleVerifier interface {
	Bind(root, runID, dispatchID, provider, identity string) error
	Verify(root, runID, dispatchID string) (lifecycle.Verification, error)
}

type nativeWorkflowLifecycle struct{}

func (nativeWorkflowLifecycle) Bind(root, runID, dispatchID, provider, identity string) error {
	return lifecycle.BindDispatch(root, runID, dispatchID, provider, identity)
}

func (nativeWorkflowLifecycle) Verify(root, runID, dispatchID string) (lifecycle.Verification, error) {
	return lifecycle.VerifyDispatch(root, runID, dispatchID)
}

var workflowLifecycle workflowLifecycleVerifier = nativeWorkflowLifecycle{}
var currentLifecycleProvider = lifecycle.CurrentProvider

func requireLifecycleVerification(root string, state RunState, dispatch PreparedDispatch) error {
	verification, err := workflowLifecycle.Verify(root, state.RunID, dispatch.ID)
	if err != nil {
		return err
	}
	switch verification.Outcome {
	case lifecycle.Verified, lifecycle.Unavailable:
		return nil
	case lifecycle.Rejected:
		return fmt.Errorf("lifecycle verification REJECTED for dispatch %s: %s", dispatch.ID, verification.Diagnostic)
	default:
		return fmt.Errorf("lifecycle verification returned invalid outcome %q", verification.Outcome)
	}
}
