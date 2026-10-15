package facade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
)

func TestStartFlowSelectionPersistsAndProjectsOriginalChoices(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument), 0600); err != nil {
		t.Fatal(err)
	}
	intake, err := PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	selection := &protocol.FlowSelection{Disabled: []string{"product-review", "development"}}
	f, run, err := Start(StartOptions{Root: root, Request: StartRequest{
		RunID: "selected-flow", Route: "regular", BaseSnapshot: frozenSnapshot,
		FlowSelection: selection, IntakeConfirmationReceipt: intake.IntakeCandidate,
	}, VCSRunner: newFrozenGitRepo(), Admission: testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)})
	if err != nil {
		t.Fatal(err)
	}
	want := &protocol.FlowSelection{Disabled: append([]string{}, selection.Disabled...)}
	selection.Disabled[0] = "repair"
	snapshot, err := f.engine.Load()
	if err != nil || !reflect.DeepEqual(snapshot.State.FlowSelection, want) {
		t.Fatal("state did not own an independent original choice", err)
	}
	if !reflect.DeepEqual(run.FlowSelection, want) {
		t.Fatal("start did not project the accepted choices")
	}
	reopened, err := Open(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	current, err := reopened.Show()
	if err != nil || !reflect.DeepEqual(current.FlowSelection, want) || !reflect.DeepEqual(current.FlowNodes, run.FlowNodes) {
		t.Fatal("reopen changed the configured choices", err)
	}
	for _, node := range current.FlowNodes {
		if node.Enabled != snapshot.State.FlowNodeEnabled(node.ID) {
			t.Fatalf("node %s did not project authoritative selection", node.ID)
		}
	}
	data, err := os.ReadFile(filepath.Join(root, EngineNamespace, run.RunID, "request.json"))
	var request StartRequest
	if err != nil || json.Unmarshal(data, &request) != nil || !reflect.DeepEqual(request.FlowSelection, want) {
		t.Fatal("bootstrap request lost the original selection", err)
	}
}

func TestStartRejectsUnknownFlowNodeBeforeRunWrite(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	_, _, err := Start(StartOptions{Root: root, Request: StartRequest{
		RunID: "selected-flow", Route: "lightweight", FlowSelection: &protocol.FlowSelection{Disabled: []string{"missing-node"}},
	}})
	if err == nil || !strings.Contains(err.Error(), "unknown main-flow node") {
		t.Fatalf("invalid selection error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, EngineNamespace)); !os.IsNotExist(err) {
		t.Fatal("invalid selection created workflow state", err)
	}
}

func TestDisabledDevelopmentRouteConfirmsWithoutPlan(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument), 0600); err != nil {
		t.Fatal(err)
	}
	intake, err := PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	f, _, err := Start(StartOptions{Root: root, Request: StartRequest{
		RunID: "selected-route", Route: "regular", BaseSnapshot: frozenSnapshot,
		FlowSelection: &protocol.FlowSelection{Disabled: []string{"product-review", "start-readiness", "development"}}, IntakeConfirmationReceipt: intake.IntakeCandidate,
	}, VCSRunner: newFrozenGitRepo(), Admission: testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	for _, choice := range []string{"no-split", "approve"} {
		if requested, err := f.RequestWorkflowChoice(); err != nil || !requested {
			t.Fatal("original topology/route Ask was not requested", choice, err)
		}
		input := ControlInput{Ask: 1, Choice: choice, Reason: "user selected the displayed scope"}
		if choice == "approve" {
			input.Gates = []int{}
		}
		candidate, err := f.PrepareCurrentControl(input)
		if err != nil || candidate.DevelopmentPlan != nil {
			t.Fatal("disabled development required a proposal", choice, err)
		}
		if _, _, err := submitControlWithHost(t, f, candidate); err != nil {
			t.Fatal("original confirmation did not accept the selection", choice, err)
		}
	}
	snapshot, err := f.engine.Load()
	if err != nil || !snapshot.State.RouteConfirmed || len(snapshot.State.SelectedGates) != 0 {
		t.Fatal("empty route did not persist through original confirmation", err)
	}
	if requested, err := f.RequestWorkflowChoice(); err != nil || requested {
		t.Fatal("disabled development reopened the route Ask", err)
	}
	if snapshot.State.ReadinessProposal() != nil {
		t.Fatal("disabled development manufactured a proposal")
	}
}

func TestTerminalViewRetainsFlowSelection(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	want := &protocol.FlowSelection{Disabled: []string{"product-review"}}
	f, _, err := Start(StartOptions{Root: root, Request: StartRequest{
		RunID: "recorded-flow", Route: "lightweight", FlowSelection: want, IntakeConfirmationReceipt: testReceipt(),
	}, Admission: testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)})
	if err != nil {
		t.Fatal(err)
	}
	completed, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	current, err := f.Show()
	if err != nil || !reflect.DeepEqual(current.FlowSelection, want) || !reflect.DeepEqual(current.FlowNodes, completed.FlowNodes) {
		t.Fatal("terminal summary did not preserve configured choices", err)
	}
}

func TestResetRetainsDisabledReviewSelection(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument), 0600); err != nil {
		t.Fatal(err)
	}
	intake, err := PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	want := &protocol.FlowSelection{Disabled: []string{"product-review", "start-readiness", "development"}}
	f, _, err := Start(StartOptions{Root: root, Request: StartRequest{
		RunID: "reset-selected-flow", Route: "regular", BaseSnapshot: frozenSnapshot,
		FlowSelection: want, IntakeConfirmationReceipt: intake.IntakeCandidate,
	}, VCSRunner: newFrozenGitRepo(), Admission: testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)})
	if err != nil {
		t.Fatal(err)
	}
	request, err := f.PrepareCurrentControl(ControlInput{Request: protocol.ControlReset})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := submitControlWithHost(t, f, request); err != nil {
		t.Fatal(err)
	}
	confirmation, err := f.PrepareCurrentControl(ControlInput{Ask: 1, Choice: "proceed", Reason: "user requests a reset with the displayed preservation"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := submitControlWithHost(t, f, confirmation); err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	pkg := snapshot.State.PendingAsks["reset-confirm:"+string(request.Event.ID)].Package
	if pkg == nil || pkg.RedoProductReview || pkg.RedoStartReadiness || !reflect.DeepEqual(snapshot.State.FlowSelection, want) {
		t.Fatal("reset recreated disabled review work or changed selection", pkg)
	}
}
