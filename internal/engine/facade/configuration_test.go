package facade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
)

func confirmConfiguration(t *testing.T, f *Facade, input ConfigurationInput) (ControlCandidate, Run) {
	t.Helper()
	request, err := f.PrepareCurrentControl(ControlInput{Configuration: &input})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err := f.SubmitControl(request)
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := f.PrepareCurrentControl(ControlInput{Ask: askPosition(t, run, protocol.ControlSelectRoute), Choice: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	_, run, err = f.SubmitControl(candidate)
	if err != nil {
		t.Fatal(err)
	}
	return candidate, run
}

func TestConfigurationControlPinsSavedSuiteDuringReviewBeforeTopology(t *testing.T) {
	f, root := workerMaterialFacade(t)
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	if _, err := settings.SaveConfiguration(root, home, "project", "工作", "默认"); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.EditConfiguration(root, home, "project", "工作", func(configuration *settings.Configuration) error {
		configuration.FlowSelection = &settings.FlowSelection{Disabled: []string{"product-review", "start-readiness", "development"}}
		configuration.QAModes = []string{"blackbox"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	before, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	frozen, _ := json.Marshal(before.State.Settings)
	request, err := f.PrepareCurrentControl(ControlInput{Configuration: &ConfigurationInput{Name: "2"}})
	if err != nil || request.Event.Request.Configuration.Name != "工作" || request.DevelopmentPlan != nil {
		t.Fatal("pre-topology configuration request lost its saved identity or required a plan", request.Event, err)
	}
	_, run, err := f.SubmitControl(request)
	if err != nil {
		t.Fatal(err)
	}
	ask := run.PendingAsks[askPosition(t, run, protocol.ControlSelectRoute)-1]
	if !reflect.DeepEqual(ask.Configuration, request.Event.Request.Configuration) || ask.Continuation == nil || ask.Continuation.To.Kind != "configuration" {
		t.Fatal("Ask did not pin the suite and program-generated work preview", ask)
	}
	if _, err := settings.EditConfiguration(root, home, "project", "工作", func(configuration *settings.Configuration) error {
		*configuration = settings.DefaultConfiguration()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	candidate, err := f.PrepareCurrentControl(ControlInput{Ask: askPosition(t, run, protocol.ControlSelectRoute), Choice: "approve"})
	if err != nil || !reflect.DeepEqual(candidate.Event.Decide.Review.Route.Configuration, ask.Configuration) || candidate.DevelopmentPlan != nil {
		t.Fatal("confirmation reloaded the saved suite or fabricated an initial plan", candidate.Event, err)
	}
	if _, _, err := f.SubmitControl(candidate); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := reopened.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := json.Marshal(after.State.Settings)
	if string(actual) != string(frozen) || after.State.Topology != nil || after.State.RouteConfirmed || after.State.FlowNodeEnabled("product-review") || after.State.FlowNodeEnabled("development") || after.State.EffectiveConfiguration().Name != "工作" {
		t.Fatal("runtime configuration changed the frozen defaults or fabricated topology/route confirmation")
	}
	if err := os.WriteFile(filepath.Join(root, ".gates", "config.json"), []byte("saved defaults temporarily invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	on := true
	_, run = confirmConfiguration(t, reopened, ConfigurationInput{FlowNode: "product-review", Enabled: &on})
	if !slices.Contains(run.Configuration.Configuration.QAModes, "blackbox") || !run.FlowNodes[0].Enabled {
		t.Fatal("editing a current node reloaded defaults or lost other selected choices")
	}
}

func TestConfigurationNodeEditPreservesLaterTemporaryExecutionChoice(t *testing.T) {
	f, _ := workerMaterialFacade(t)
	platform := selectionPlatformFixture(t, "current", true)
	selectExecution(t, f, ExecutionSelectionInput{Node: "development", Platform: platform.descriptor.ID, Model: "opaque-careful", Effort: "high"})
	before, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := before.State.EffectiveExecutionChoice("development")
	off := false
	_, run := confirmConfiguration(t, f, ConfigurationInput{FlowNode: "repair", Enabled: &off})
	after, err := f.engine.Load()
	if err != nil || after.State.EffectiveExecutionChoice("development") != want || run.Configuration.Configuration.Execution["development"] != want || after.State.FlowNodeEnabled("repair") {
		t.Fatal("editing only repair replaced the user's current development tuple", after.State.EffectiveExecutionChoice("development"), want, err)
	}
}

func TestConfigurationNodeEditPreservesConfirmedGateAndQASelection(t *testing.T) {
	d := plannedValidationDriverAt(t, t.TempDir(), "zcode", nil, commandPlan(), "blackbox", "quality")
	off := false
	_, _ = confirmConfiguration(t, d.f, ConfigurationInput{FlowNode: "repair", Enabled: &off})
	after, err := d.f.engine.Load()
	if err != nil || !slices.Equal(after.State.SelectedGates, []string{"blackbox", "quality"}) {
		t.Fatal("editing only repair replaced the current gate and QA selection", after.State.SelectedGates, err)
	}
}

func TestConfigurationDisablingCompletedReadinessRetainsConfirmedPlan(t *testing.T) {
	d := plannedValidationDriverAt(t, t.TempDir(), "zcode", nil, commandPlan(), "quality")
	before := driverState(t, d)
	original, err := d.f.DevelopmentPlan(routeDecisionForGates([]string{"quality"}))
	if err != nil || original == nil {
		t.Fatal("the accepted route has no original development plan", err)
	}
	action, input := d.action("engine.development.batch.v6")
	off := false
	candidate, run := confirmConfiguration(t, d.f, ConfigurationInput{FlowNode: "start-readiness", Enabled: &off})
	if candidate.DevelopmentPlan == nil || !reflect.DeepEqual(candidate.DevelopmentPlan.Proposal, original.Proposal) || candidate.DevelopmentPlan.Confirmation.ReviewActionID != original.Confirmation.ReviewActionID {
		t.Fatal("disabling the completed review discarded its real confirmed plan")
	}
	reopened, err := Open(d.f.root, d.f.runID)
	if err != nil {
		t.Fatal(err)
	}
	d.f, d.run = reopened, run
	d.drive()
	after := driverState(t, d)
	if after.FlowNodeEnabled("start-readiness") || !after.RouteConfirmed || !reflect.DeepEqual(after.ReadinessProposal(), before.ReadinessProposal()) || !reflect.DeepEqual(after.RequirementRevisions, before.RequirementRevisions) {
		t.Fatal("the configuration lost the original proposal or its confirmed requirement")
	}
	if current, ok := after.PendingActions[action]; !ok || current.Task != input.Binding.Task {
		t.Fatal("disabling a completed review replaced the current development responsibility")
	}
	for _, pending := range d.run.ValidationInputs {
		if pending.Handler == "engine.development.plan.v1" {
			t.Fatal("the confirmed plan was needlessly prepared again")
		}
	}
}

func TestConfigurationEnablingSkippedReadinessWaitsForItsOriginalReview(t *testing.T) {
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	intake, err := PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	f, run, err := Start(StartOptions{Root: root, ArtifactRoot: root, Request: StartRequest{
		RunID: "selected-readiness", Route: "regular", BaseSnapshot: base,
		FlowSelection: &protocol.FlowSelection{Disabled: []string{"product-review", "start-readiness"}}, IntakeConfirmationReceipt: intake.IntakeCandidate,
	}, Admission: testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)})
	if err != nil {
		t.Fatal(err)
	}
	d := &validationDriver{t: t, f: f, run: run}
	d.drive()
	action, input := d.action("engine.development.plan.v1")
	d.business(action, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", GranularityReview: commandPlan()})
	for _, choice := range []string{"no-split", "approve"} {
		if requested, err := f.RequestWorkflowChoice(); err != nil || !requested {
			t.Fatal("the prepared plan did not reach its original confirmation", choice, err)
		}
		candidate, err := f.PrepareCurrentControl(ControlInput{Ask: 1, Choice: choice, Reason: "one cohesive command"})
		if err != nil {
			t.Fatal(err)
		}
		if _, run, err = f.SubmitControl(candidate); err != nil {
			t.Fatal(err)
		}
	}
	before, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	on := true
	candidate, _ := confirmConfiguration(t, f, ConfigurationInput{FlowNode: "start-readiness", Enabled: &on})
	if candidate.DevelopmentPlan != nil || candidate.Event.Decide.Review.Route.DevelopmentPlan != nil {
		t.Fatal("the new review was confirmed using another preparation owner's plan")
	}
	reopened, err := Open(root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := reopened.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !after.State.FlowNodeEnabled("start-readiness") || after.State.ReviewSeries.StartReadiness.Completed != 0 || after.State.ReadinessProposal() != nil || !reflect.DeepEqual(after.State.Results[action], before.State.Results[action]) {
		t.Fatal("the enabled review fabricated a result or discarded the original prepared plan")
	}
}
