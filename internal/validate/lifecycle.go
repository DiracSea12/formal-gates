package validate

import (
	"fmt"
	"strings"

	"formal-gates/internal/lifecycle"
)

type workflowLifecycleVerifier interface {
	Begin(root, runID string) error
	Bind(root, runID, dispatchID, identity string) error
	Verify(root, runID, dispatchID string) (lifecycle.Verification, error)
	TranscriptPath(root, runID, dispatchID string) (string, string, error)
	ResolveClaimIdentity(root, runID, preferred string) (string, error)
	// InterruptionReason 读取派发已记录的中断原因，供续用判定（三分支）。
	InterruptionReason(root, runID, dispatchID string) (string, error)
}

type nativeWorkflowLifecycle struct{}

func (nativeWorkflowLifecycle) Begin(root, runID string) error {
	return lifecycle.BeginRun(root, runID)
}

func (nativeWorkflowLifecycle) Bind(root, runID, dispatchID, identity string) error {
	return lifecycle.BindDispatch(root, runID, dispatchID, identity)
}

func (nativeWorkflowLifecycle) BindWithProvider(root, runID, dispatchID, identity, provider string) error {
	return lifecycle.BindDispatchWithProvider(root, runID, dispatchID, identity, provider)
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

func (nativeWorkflowLifecycle) ResolveClaimIdentityWithProvider(root, runID, preferred, provider string) (string, error) {
	return lifecycle.ResolveClaimIdentityWithProvider(root, runID, preferred, provider)
}

func (nativeWorkflowLifecycle) InterruptionReason(root, runID, dispatchID string) (string, error) {
	return lifecycle.DispatchInterruptionReason(root, runID, dispatchID)
}

var workflowLifecycle workflowLifecycleVerifier = nativeWorkflowLifecycle{}

func requireLifecycleVerification(root string, state RunState, dispatch PreparedDispatch) error {
	verification, err := workflowLifecycle.Verify(root, state.RunID, dispatch.ID)
	if err != nil {
		return err
	}
	switch verification.Outcome {
	case lifecycle.Verified, lifecycle.Unavailable, lifecycle.Interrupted:
		return nil
	case lifecycle.Rejected:
		// 托管 agent 环境无 capture hooks 时，子代理派发没有任何 start/stop 事件可配对，
		// 裸 "missing matching start and stop event" 对主代理不可行动。给出可行动提示：review
		// 记录依赖 capture hooks（lifecycle capture）或非托管环境。
		if strings.Contains(verification.Diagnostic, "missing matching") {
			return fmt.Errorf("lifecycle verification REJECTED for dispatch %s: %s (no matching lifecycle events were captured; review/gate recording depends on the host capture hooks, e.g. `lifecycle capture --provider <host> --event ...`, or a non-managed environment without lifecycle verification)", dispatch.ID, verification.Diagnostic)
		}
		return fmt.Errorf("lifecycle verification REJECTED for dispatch %s: %s", dispatch.ID, verification.Diagnostic)
	default:
		return fmt.Errorf("lifecycle verification returned invalid outcome %q", verification.Outcome)
	}
}
