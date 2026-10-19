package facade

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
)

func TestStartRejectsPerRunFlowOverridesBeforeWriting(t *testing.T) {
	for _, disabled := range [][]string{{"product-review"}, {"unknown"}} {
		root := t.TempDir()
		_, _, err := Start(StartOptions{Root: root, Request: StartRequest{RunID: "overridden-start", FlowSelection: &protocol.FlowSelection{Disabled: disabled}}})
		if err == nil || !strings.Contains(err.Error(), "overrides") {
			t.Fatal("per-run flow override was not rejected", err)
		}
		if _, err := os.Stat(filepath.Join(root, EngineNamespace)); !os.IsNotExist(err) {
			t.Fatal("rejected override wrote run state", err)
		}
	}
}

func TestResetKeepsTheAppliedNodeSelection(t *testing.T) {
	f, root := workerMaterialFacade(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	want := &settings.FlowSelection{Disabled: []string{"product-review", "start-readiness"}}
	if _, err := settings.EditConfiguration(root, home, "project", "工作", nil, func(configuration *settings.Configuration) error { configuration.FlowSelection = want; return nil }); err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.engine.Drive(protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	request, err := f.PrepareCurrentControl(ControlInput{Request: protocol.ControlReset})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := submitControlWithHost(t, f, request); err != nil {
		t.Fatal(err)
	}
	confirmation, err := f.PrepareCurrentControl(ControlInput{Ask: 1, Choice: "proceed", Reason: "Reset the displayed work while keeping configuration"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := submitControlWithHost(t, f, confirmation); err != nil {
		t.Fatal(err)
	}
	snapshot, err := f.engine.Load()
	if err != nil || !reflect.DeepEqual(snapshot.State.FlowSelection, want) || !reflect.DeepEqual(snapshot.State.AppliedConfiguration().FlowSelection, want) {
		t.Fatal("reset introduced another configuration owner", err)
	}
}

func TestLibraryDisablingCompletedReadinessRetainsConfirmedPlan(t *testing.T) {
	d := newValidationDriver(t, "quality")
	before := driverState(t, d)
	original, err := d.f.DevelopmentPlan(routeDecisionForGates([]string{"quality"}))
	if err != nil || original == nil {
		t.Fatal("confirmed development plan is missing", err)
	}
	action, input := d.action("engine.development.batch.v6")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.EditConfiguration(d.f.root, home, "project", before.ConfigurationName(), nil, func(configuration *settings.Configuration) error {
		configuration.FlowSelection = &settings.FlowSelection{Disabled: []string{"start-readiness"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.f.engine.Drive(protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(d.f.root, d.f.runID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := reopened.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if after.State.FlowNodeEnabled("start-readiness") || !after.State.RouteConfirmed || !reflect.DeepEqual(after.State.ReadinessProposal(), before.ReadinessProposal()) || !reflect.DeepEqual(after.State.RequirementRevisions, before.RequirementRevisions) {
		t.Fatal("closing a completed review discarded the confirmed plan")
	}
	if current, ok := after.State.PendingActions[action]; !ok || current.Task != input.Binding.Task {
		t.Fatal("closing completed readiness replaced existing development work")
	}
}

func TestLibraryEnablingSkippedReadinessWaitsForItsReview(t *testing.T) {
	root := t.TempDir()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := settings.SaveConfiguration(root, home, "project", "准备", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.EditConfiguration(root, home, "project", "准备", nil, func(configuration *settings.Configuration) error {
		configuration.FlowSelection = &settings.FlowSelection{Disabled: []string{"product-review", "start-readiness"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(root, home, "project", "准备", nil); err != nil {
		t.Fatal(err)
	}
	d := startValidationDriverAt(t, root, DefaultProvider, nil)
	action, input := d.action("engine.development.plan.v1")
	d.business(action, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", GranularityReview: commandPlan()})
	d.drive()
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "One cohesive command"}})
	d.confirmRoute(routeDecisionForGates([]string{"quality"}))
	if _, err := settings.EditConfiguration(root, home, "project", "准备", nil, func(configuration *settings.Configuration) error {
		configuration.FlowSelection = &settings.FlowSelection{Disabled: []string{"product-review"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.f.engine.Drive(protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	state := driverState(t, d)
	if state.ReadinessProposal() != nil || state.ReviewSeries.StartReadiness.Completed != 0 {
		t.Fatal("enabling readiness accepted another preparation owner's plan")
	}
	found := false
	for _, pending := range state.PendingActions {
		found = found || pending.ReviewSeries == "start-readiness"
	}
	if !found {
		t.Fatal("enabling the skipped review did not issue its own review duty")
	}
}
