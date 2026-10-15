package facade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
)

func TestEffectiveSettingsSurviveReopenAndTerminalWithoutReload(t *testing.T) {
	root := t.TempDir()
	// The admission fixture builds the CLI binary once per process; keep that
	// build on the real module cache before HOME is isolated for the overlay.
	admission := testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	config := filepath.Join(root, ".gates", "config.json")
	if err := os.MkdirAll(filepath.Dir(config), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(config, []byte(`{"capacity":2,"qaCapacity":1,"model":"chosen-model","reasoningEffort":"high","tokenCap":77}`), 0600); err != nil {
		t.Fatal(err)
	}
	request := StartRequest{RunID: "configured-run", Route: "lightweight", DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: testReceipt()}
	f, run, err := Start(StartOptions{Root: root, Request: request, Admission: admission})
	if err != nil {
		t.Fatal(err)
	}
	if run.Settings == nil || run.Settings.Values.Capacity != 2 || run.Settings.Values.QACapacity != 1 || *run.Settings.Values.TokenCap != 77 || run.Settings.Sources["model"] != config {
		t.Fatalf("effective=%+v", run.Settings)
	}
	snap, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if snap.State.Settings.Values.TokenCap != nil || snap.State.TokenCapMaxTokens == nil || *snap.State.TokenCapMaxTokens != 77 {
		t.Fatal("cap must retain its sole cost owner")
	}
	before, _ := json.Marshal(snap.State.Settings)
	if err := os.WriteFile(config, []byte(`invalid edited config`), 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if reopened.capacity != 2 {
		t.Fatalf("reopen capacity=%d", reopened.capacity)
	}
	after, err := reopened.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(after.State.Settings)
	if string(before) != string(got) || *after.State.TokenCapMaxTokens != 77 {
		t.Fatal("reopen reset settings or cap")
	}
	terminal, err := reopened.Drive()
	if err != nil {
		t.Fatal(err)
	}
	if terminal.Settings == nil || terminal.Settings.Values.Model != "chosen-model" || *terminal.Settings.Values.TokenCap != 77 {
		t.Fatalf("terminal settings=%+v", terminal.Settings)
	}
	shown, err := reopened.Show()
	if err != nil || shown.Settings == nil || shown.Settings.Sources["model"] != config {
		t.Fatalf("terminal show=%+v err=%v", shown.Settings, err)
	}
}

func TestNamedConfigurationsFreezeFlowExecutionAndGateSelections(t *testing.T) {
	root := t.TempDir()
	admission := testAdmission(t, root, "target-1", "sha256:package", DefaultProvider)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	platform := selectionPlatformFixture(t, "configured", true)
	if err := os.WriteFile(filepath.Join(root, "requirements.md"), []byte(validationDocument), 0600); err != nil {
		t.Fatal(err)
	}
	intake, err := PrepareIntake(root, "requirements.md", "", nil, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"甲", "乙"} {
		if _, err := settings.SaveConfiguration(root, home, "project", name, "默认"); err != nil {
			t.Fatal(err)
		}
	}
	gates := []string{"quality"}
	choice := settings.ExecutionChoice{Provider: platform.descriptor.ID, Model: "opaque-careful", ReasoningEffort: "high"}
	if _, err := settings.EditConfiguration(root, home, "project", "甲", func(configuration *settings.Configuration) error {
		configuration.FlowSelection = &settings.FlowSelection{Disabled: []string{"product-review", "start-readiness", "development"}}
		configuration.QAModes, configuration.Gates = []string{"blackbox"}, &gates
		configuration.Execution = map[string]settings.ExecutionChoice{"qa-blackbox": choice, "qa-whitebox": {Provider: platform.descriptor.ID, Model: "opaque-fast", ReasoningEffort: "low"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(root, home, "project", "甲"); err != nil {
		t.Fatal(err)
	}
	start := func(id string, selection *protocol.FlowSelection, configuration ...string) (*Facade, Run) {
		t.Helper()
		name := ""
		if len(configuration) != 0 {
			name = configuration[0]
		}
		f, run, err := Start(StartOptions{Root: root, Configuration: name, Admission: admission, VCSRunner: newFrozenGitRepo(), Request: StartRequest{RunID: id, Route: "regular", BaseSnapshot: frozenSnapshot, FlowSelection: selection, IntakeConfirmationReceipt: intake.IntakeCandidate}})
		if err != nil {
			t.Fatal(err)
		}
		return f, run
	}
	f, run := start("configured-one", nil)
	if run.Settings.ConfigurationName != "甲" || !reflect.DeepEqual(run.FlowSelection, run.Settings.Configuration.FlowSelection) || run.Settings.ConfigurationSource != filepath.Join(root, ".gates", "config.json") {
		t.Fatal("start did not freeze the selected configuration and its source", run.Settings)
	}
	snapshot, err := f.engine.Load()
	if err != nil || snapshot.State.EffectiveExecutionChoice("qa-blackbox") != choice || snapshot.State.EffectiveExecutionChoice("qa-whitebox").Model != "opaque-fast" {
		t.Fatal("configuration did not retain separate QA execution groups", err)
	}
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	if requested, err := f.RequestWorkflowChoice(); err != nil || !requested {
		t.Fatal("topology confirmation was not requested", err)
	}
	topology, err := f.PrepareCurrentControl(ControlInput{Ask: 1, Choice: "no-split", Reason: "one configured responsibility"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := f.SubmitControl(topology); err != nil {
		t.Fatal(err)
	}
	if requested, err := f.RequestWorkflowChoice(); err != nil || !requested {
		t.Fatal("gate confirmation was not requested", err)
	}
	configured, err := f.PrepareCurrentControl(ControlInput{Ask: 1, Choice: "approve"})
	if err != nil || !slices.Equal(configured.Event.Decide.Review.Route.Selected, []string{"blackbox", "quality"}) {
		t.Fatal("frozen configuration did not preselect QA and ordinary gates", configured.Event, err)
	}
	empty, err := f.PrepareCurrentControl(ControlInput{Ask: 1, Choice: "approve", Gates: []int{}})
	if err != nil || len(empty.Event.Decide.Review.Route.Selected) != 0 || !slices.Equal(empty.Event.Decide.Review.Route.Gates, configured.Event.Decide.Review.Route.Gates) {
		t.Fatal("explicit empty selection lost the original candidate catalogue", err)
	}
	for _, retired := range []string{"default", "full", "custom"} {
		if _, err := f.PrepareCurrentControl(ControlInput{Ask: 1, Choice: retired}); err == nil {
			t.Fatal("retired route choice remains available", retired)
		}
	}
	if _, _, err := f.SubmitControl(configured); err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(snapshot.State.Settings)
	if _, err := settings.EditConfiguration(root, home, "project", "甲", func(configuration *settings.Configuration) error {
		*configuration = settings.DefaultConfiguration()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(root, home, "project", "乙"); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := reopened.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := json.Marshal(after.State.Settings)
	if string(actual) != string(before) || !slices.Equal(after.State.SelectedGates, []string{"blackbox", "quality"}) {
		t.Fatal("editing or switching saved configurations changed an existing run")
	}
	explicit := &protocol.FlowSelection{Disabled: []string{"qa"}}
	_, second := start("configured-two", explicit)
	if second.Settings.ConfigurationName != "乙" || !reflect.DeepEqual(second.FlowSelection, explicit) || len(second.Settings.Configuration.QAModes) != 0 {
		t.Fatal("new run did not use the selected configuration and explicit start selection")
	}
	configPath := filepath.Join(root, ".gates", "config.json")
	saved, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatal(err)
	}
	_, oneRun := start("configured-once", nil, "甲")
	unchanged, err := os.ReadFile(configPath)
	if err != nil || oneRun.Settings.ConfigurationName != "甲" || string(saved) != string(unchanged) {
		t.Fatal("a named start changed the saved active configuration", err)
	}
}

func TestChildKeepsItsParentsFrozenConfigurationAfterSavedDefaultChanges(t *testing.T) {
	root := t.TempDir()
	base := snapshotRepoCommits(t, root)[0]
	_ = testAdmission(t, root, "candidate", "sha256:package", DefaultProvider)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	platform := selectionPlatformFixture(t, "inherited", true)
	gates := []string{"security"}
	configuration := settings.Configuration{
		QAModes: []string{"blackbox"}, Gates: &gates,
		Execution: map[string]settings.ExecutionChoice{
			"development": {Provider: platform.descriptor.ID, Model: "opaque-careful", ReasoningEffort: "high"},
			"qa-blackbox": {Provider: platform.descriptor.ID, Model: "opaque-fast", ReasoningEffort: "low"},
		},
	}
	if _, err := settings.EditConfiguration(root, home, "project", "默认", func(value *settings.Configuration) error {
		*value = configuration
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	master := nativeMasterAt(t, root, base, nil)
	original := driverState(t, master)
	if _, err := settings.EditConfiguration(root, home, "project", "默认", func(value *settings.Configuration) error {
		*value = settings.DefaultConfiguration()
		value.FlowSelection = &settings.FlowSelection{Disabled: []string{"development"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	passPreDevelopmentReviews(t, master, splitCommandPlan())
	master.control(protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{Topology: splitTopologyDecision(protocol.ACOwner{AC: "AC-001", Owner: "lane-core"})})
	child, run, err := createdSliceFacade(t, master.f, "lane-core", OwnerBinding{})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(run.Settings, stateSettings(original)) || !reflect.DeepEqual(run.FlowSelection, original.FlowSelection) {
		t.Fatal("child creation reloaded the saved default configuration")
	}
	if _, err := child.Drive(); err != nil {
		t.Fatal(err)
	}
	if requested, err := child.RequestWorkflowChoice(); err != nil || !requested {
		t.Fatal("child did not request its own gate and plan confirmation", err)
	}
	candidate, err := child.PrepareCurrentControl(ControlInput{Ask: 1, Choice: "approve"})
	if err != nil || !slices.Equal(candidate.Event.Decide.Review.Route.Selected, gates) || slices.Contains(candidate.Event.Decide.Review.Route.Gates, "blackbox") || candidate.DevelopmentPlan == nil {
		t.Fatal("child lost inherited gate selection or its independent exact plan confirmation", candidate.Event, err)
	}
	if _, _, err := child.SubmitControl(candidate); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, child.runID)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := reopened.engine.Load()
	if err != nil || !reflect.DeepEqual(snapshot.State.Settings, original.Settings) || snapshot.State.EffectiveExecutionChoice("development") != configuration.Execution["development"] || !slices.Equal(snapshot.State.SelectedGates, gates) {
		t.Fatal("child reopen changed its frozen settings, tuple or selected gates", err)
	}
}
