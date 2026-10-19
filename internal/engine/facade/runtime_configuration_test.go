package facade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
)

func TestRunsBoundToOneConfigurationReadItsSavedValueAtDispatch(t *testing.T) {
	first, root := workerMaterialFacade(t)
	req, err := first.request()
	if err != nil {
		t.Fatal(err)
	}
	req.RunID = "shared-second"
	second, _, err := Start(StartOptions{Root: root, Request: req, Admission: &first.admission})
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := first.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	frozen := map[string]string{}
	for _, f := range []*Facade{first, second} {
		snapshot, err = f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(snapshot.State.Settings)
		frozen[f.runID] = string(data)
	}
	config := filepath.Join(root, ".gates", "config.json")
	original, err := os.ReadFile(config)
	if err != nil {
		t.Fatal(err)
	}
	hold := func() {
		t.Helper()
		run, err := first.Drive()
		if err != nil || len(run.PendingActions) != 0 || run.Next.Wait == nil || run.Next.Wait.Reason != decision.WaitConfigurationUnavailable || run.Next.Wait.Detail == "" {
			t.Fatalf("configuration fault did not stop and report: %+v %v", run.Next, err)
		}
		before, err := os.ReadFile(filepath.Join(first.stateDir, "state.json"))
		if err != nil {
			t.Fatal(err)
		}
		shown, err := first.Show()
		after, _ := os.ReadFile(filepath.Join(first.stateDir, "state.json"))
		if err != nil || shown.Next.Wait == nil || shown.Next.Wait.Detail == "" || string(before) != string(after) {
			t.Fatal("Show failed to report without writing", err)
		}
	}
	if err := os.WriteFile(config, []byte(`{"configurations":`), 0600); err != nil {
		t.Fatal(err)
	}
	hold()
	if err := os.WriteFile(config, []byte(`{"configuration":"工作"}`), 0600); err != nil {
		t.Fatal(err)
	}
	hold()
	if err := os.WriteFile(config, original, 0600); err != nil {
		t.Fatal(err)
	}
	choice := settings.ExecutionChoice{Model: "saved-model", ReasoningEffort: "high"}
	if _, err := settings.EditConfiguration(root, home, "project", "工作", nil, func(value *settings.Configuration) error {
		value.Execution = map[string]settings.ExecutionChoice{"product-review": choice}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []*Facade{first, second} {
		run, err := f.Drive()
		if err != nil {
			t.Fatal(err)
		}
		snap, err := f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		actual, _ := json.Marshal(snap.State.Settings)
		if string(actual) != frozen[f.runID] {
			t.Fatal("library edit changed frozen Values", f.runID)
		}
		if run.Configuration.Name != "工作" || run.Configuration.Configuration.Execution["product-review"] != choice || len(snap.State.AppliedConfigurations) != 2 {
			t.Fatal("run did not read the shared named configuration", f.runID, run.Configuration)
		}
		if len(run.PendingActions) != 1 {
			t.Fatal("next dispatch was not issued", f.runID)
		}
		for id := range run.PendingActions {
			execution, known := snap.State.ActionExecutionChoice(id)
			if !known || execution.Model != choice.Model || execution.ReasoningEffort != choice.ReasoningEffort {
				t.Fatal("dispatch record lost the actual saved value", execution)
			}
		}
		if !slices.Equal(snap.State.AppliedConfigurations[1].Changed, []string{"execution.product-review"}) {
			t.Fatal("field-level history lost the execution change")
		}
	}
}

func TestBoundLibraryRequiresExactNamesAndReportsParsingFailure(t *testing.T) {
	f, root := workerMaterialFacade(t)
	if _, err := configurationLibrary(root)("2"); err == nil {
		t.Fatal("run binding accepted a displayed position")
	}
	path := filepath.Join(root, ".gates", "config.json")
	if err := os.WriteFile(path, []byte(`{"unknown-setting":true}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	run, err := f.Show()
	if err != nil || run.Next.Wait == nil || !strings.Contains(run.Next.Wait.Detail, "unknown") {
		t.Fatal("unknown library fields did not report their cause", run.Next, err)
	}
}

func TestReopenKeepsFrozenValuesAndAppliedConfiguration(t *testing.T) {
	f, root := workerMaterialFacade(t)
	before, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := reopened.engine.Load()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("Open wrote or reset configuration facts", err)
	}
	if after.State.Settings.ConfigurationSource != "" || !reflect.DeepEqual(after.State.Settings.Configuration, settings.Configuration{}) || after.State.ConfigurationName() != "工作" || len(after.State.AppliedConfigurations) != 1 {
		t.Fatal("binding and initial applied snapshot do not have separate owners")
	}
	if after.State.AppliedConfigurations[0].Preview == nil || after.State.AppliedConfigurations[0].Effect == nil {
		t.Fatal("initial snapshot lost its applied content")
	}
}

func TestChildSettingsCarryTheBoundNameWithoutAFrozenSuite(t *testing.T) {
	parent := &protocol.State{Settings: &settings.Effective{ConfigurationName: "工作", Values: settings.Values{Model: "frozen-default"}}}
	inherited := stateSettings(parent)
	if inherited.ConfigurationName != parent.ConfigurationName() || !reflect.DeepEqual(inherited.Configuration, settings.Configuration{}) || inherited.Values.Model != "frozen-default" {
		t.Fatal("child settings did not preserve the sole binding and frozen defaults")
	}
}

func TestChildReadsMasterBoundConfigurationAfterCreation(t *testing.T) {
	master := nativeMasterDriver(t)
	platform := selectionPlatformFixture(t, "shared-child", true)
	first := saveExecution(t, master.f, ExecutionSelectionInput{Node: "development", Platform: platform.descriptor.ID, Model: "Careful"})
	passPreDevelopmentReviews(t, master, splitCommandPlan())
	master.control(protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{Topology: splitTopologyDecision(protocol.ACOwner{AC: "AC-001", Owner: "lane-core"})})
	child, _, err := createdSliceFacade(t, master.f, "lane-core", OwnerBinding{})
	if err != nil {
		t.Fatal(err)
	}
	before, err := child.engine.Load()
	if err != nil || before.State.ConfigurationName() != driverState(t, master).ConfigurationName() || before.State.EffectiveExecutionChoice("development") != first {
		t.Fatal("child did not inherit the master's binding", err)
	}
	saved := saveExecution(t, master.f, ExecutionSelectionInput{Node: "development", Model: "Fast"})
	reopened, err := Open(child.root, child.runID)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := reopened.engine.Drive(protocol.Observation{}); err != nil {
		t.Fatal(err)
	}
	after, err := reopened.engine.Load()
	if err != nil || after.State.EffectiveExecutionChoice("development") != saved || after.State.ConfigurationName() != before.State.ConfigurationName() {
		t.Fatal("child kept a creation execution snapshot instead of reading the bound name", err)
	}
}
