package validate

import (
	"strings"
	"testing"
)

// TestPrepareQAExecutionRejectsWithoutUserRequest asserts the "must have a new
// commit to rerun" hard limit: when a QA mode already has an authoritative FAIL
// result at the current snapshot, preparing qa-execution without --user-requested
// is rejected with "already has an authoritative".
func TestPrepareQAExecutionRejectsWithoutUserRequest(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "qaexec-rerun-reject")
	blackboxCases := state.qaModeCases("blackbox")
	if len(blackboxCases) == 0 {
		t.Fatalf("no blackbox cases in the ready delivery: %#v", state.qaModeCases(""))
	}
	failing := map[string]bool{blackboxCases[0].ID: true}
	blackboxDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	var err error
	state, err = RecordQAExecution(root, pkg, state.RunID, blackboxDispatch, executionOutcomes(blackboxCases, failing), "")
	if err != nil {
		t.Fatal(err)
	}
	if !qaExecutionModeResulted(state, "blackbox") {
		t.Fatalf("blackbox FAIL result is not authoritative at the current snapshot: %#v", state.qaExecution("blackbox"))
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "blackbox", false, ""); err == nil || !strings.Contains(err.Error(), "already has an authoritative") {
		t.Fatalf("same-mode re-prepare without --user-requested was accepted: %v", err)
	}
}

// TestPrepareQAExecutionUserRequestedRerunWithoutCommit asserts the confirmed
// mechanism: with an explicit user authorization (--user-requested), a QA mode
// that already has an authoritative FAIL result at the current snapshot may be
// re-prepared for a no-commit rerun (execution-environment repair such as
// rebuilding / reinstalling the binary produces no git commit). The new result
// overwrites the old authoritative one, and the authorization source is recorded
// in ReviewOverrides for audit.
func TestPrepareQAExecutionUserRequestedRerunWithoutCommit(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "qaexec-rerun-user")
	blackboxCases := state.qaModeCases("blackbox")
	if len(blackboxCases) == 0 {
		t.Fatalf("no blackbox cases in the ready delivery: %#v", state.qaModeCases(""))
	}
	failing := map[string]bool{blackboxCases[0].ID: true}
	blackboxDispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	var err error
	state, err = RecordQAExecution(root, pkg, state.RunID, blackboxDispatch, executionOutcomes(blackboxCases, failing), "")
	if err != nil {
		t.Fatal(err)
	}
	const reason = "execution environment rebuilt; rerun with no new commit"
	prompt, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "blackbox", true, reason)
	if err != nil {
		t.Fatalf("user-requested no-commit rerun was rejected: %v", err)
	}
	if strings.TrimSpace(prompt) == "" {
		t.Fatal("user-requested rerun returned an empty dispatch prompt")
	}
	state, err = LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if state.ReviewOverrides["qa-execution"] != reason {
		t.Fatalf("user-requested rerun authorization source not recorded: %#v", state.ReviewOverrides)
	}
}
