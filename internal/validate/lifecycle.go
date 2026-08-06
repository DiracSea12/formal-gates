package validate

import (
	"fmt"

	"formal-gates/internal/lifecycle"
)

type workflowLifecycleVerifier interface {
	Begin(root, runID string) error
	Bind(root, runID, dispatchID, identity string) error
	Verify(root, runID, dispatchID string) (lifecycle.Verification, error)
	TranscriptPath(root, runID, dispatchID string) (string, string, error)
	ResolveClaimIdentity(root, runID, preferred string) (string, error)
}

type nativeWorkflowLifecycle struct{}

func (nativeWorkflowLifecycle) Begin(root, runID string) error {
	return lifecycle.BeginRun(root, runID)
}

func (nativeWorkflowLifecycle) Bind(root, runID, dispatchID, identity string) error {
	return lifecycle.BindDispatch(root, runID, dispatchID, identity)
}

func (nativeWorkflowLifecycle) Verify(root, runID, dispatchID string) (lifecycle.Verification, error) {
	return lifecycle.VerifyDispatch(root, runID, dispatchID)
}

func (nativeWorkflowLifecycle) TranscriptPath(root, runID, dispatchID string) (string, string, error) {
	return lifecycle.DispatchTranscriptPath(root, runID, dispatchID)
}

func (nativeWorkflowLifecycle) ResolveClaimIdentity(root, runID, preferred string) (string, error) {
	return lifecycle.ResolveClaimIdentity(root, runID, preferred)
}

var workflowLifecycle workflowLifecycleVerifier = nativeWorkflowLifecycle{}

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
