package facade

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/host"
	"formal-gates/internal/settings"
)

type executionSelectionPlatform struct {
	descriptor host.Descriptor
	options    func(string, string, map[string]string, string) (host.SelectionCatalogue, error)
}

func ensureEditableConfiguration(t *testing.T, root string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	effective, err := settings.Load(root, home)
	if err != nil {
		t.Fatal(err)
	}
	if effective.ConfigurationName != settings.DefaultConfigurationName {
		return
	}
	if _, err := settings.SaveConfiguration(root, home, "project", "验证", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := settings.UseConfiguration(root, home, "project", "验证", nil); err != nil {
		t.Fatal(err)
	}
}

func (p *executionSelectionPlatform) HostDescriptor() *host.Descriptor { return &p.descriptor }
func (p *executionSelectionPlatform) SelectionOptions(root, runtimeTarget string, native map[string]string, model string) (host.SelectionCatalogue, error) {
	return p.options(root, runtimeTarget, native, model)
}

func selectionPlatformFixture(t *testing.T, suffix string, defaults bool) *executionSelectionPlatform {
	t.Helper()
	id := strings.ToLower(t.Name()) + "-" + suffix
	p := &executionSelectionPlatform{descriptor: host.Descriptor{ID: id, InstallName: id, Installable: true}}
	p.options = func(root, runtimeTarget string, native map[string]string, model string) (host.SelectionCatalogue, error) {
		catalogue := host.SelectionCatalogue{Source: "native fixture metadata", Models: []host.ModelOption{{Value: "opaque-fast", Label: "Fast", Efforts: []string{"low", "high"}}, {Value: "opaque-careful", Label: "Careful", Efforts: []string{"medium", "high"}}}, Current: host.ModelSelection{Model: "opaque-fast"}}
		if defaults {
			catalogue.Default = host.ModelSelection{Model: "opaque-fast", Effort: "low"}
			catalogue.Models[0].DefaultEffort = "low"
			if model == "opaque-careful" {
				catalogue.Models[1].DefaultEffort = "high"
			}
		}
		return catalogue, nil
	}
	if err := host.Register(p); err != nil {
		t.Fatal(err)
	}
	return p
}

// saveExecution resolves a native choice as the settings command does and saves
// it in the run's bound configuration.
func saveExecution(t *testing.T, f *Facade, input ExecutionSelectionInput) protocol.ExecutionChoice {
	t.Helper()
	snap, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	var native map[string]string
	if snap.State.Settings != nil {
		native = snap.State.Settings.Values.Native
	}
	var saved protocol.ExecutionChoice
	if _, err := settings.EditConfiguration(f.root, home, "project", snap.State.ConfigurationName(), nil, func(configuration *settings.Configuration) error {
		node, err := executionNode(input.Node)
		if err != nil {
			return err
		}
		current := configuration.EffectiveExecutionChoice(node.ID, snap.State.EffectiveExecutionChoice(""))
		selection, err := PrepareExecutionSelection(f.root, f.admission.Target, native, current, input)
		if err != nil {
			return err
		}
		if configuration.Execution == nil {
			configuration.Execution = map[string]settings.ExecutionChoice{}
		}
		configuration.Execution[selection.Node], saved = selection.Choice, selection.Choice
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return saved
}

func TestExecutionOptionsReadRunNativeContextWithoutWriting(t *testing.T) {
	f, _ := workerMaterialFacade(t)
	platform := selectionPlatformFixture(t, "defaults", true)
	t.Parallel()
	before, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	query := platform.options
	platform.options = func(root, runtimeTarget string, native map[string]string, model string) (host.SelectionCatalogue, error) {
		if root != f.root || runtimeTarget != f.admission.Target || !reflect.DeepEqual(native, before.State.Settings.Values.Native) {
			t.Fatal("selection query lost frozen native paths, runtime target or run root", root, runtimeTarget, native)
		}
		return query(root, runtimeTarget, native, model)
	}
	statePath := filepath.Join(f.stateDir, "state.json")
	original := readWorkerFile(t, statePath)
	options, err := f.QueryExecutionOptions(ExecutionSelectionInput{Node: "1", Platform: platform.descriptor.ID, Model: "Careful"})
	if err != nil || options.Catalogue.Models[1].DefaultEffort != "high" || options.Catalogue.Default.Effort != "low" || !bytes.Equal(original, readWorkerFile(t, statePath)) {
		t.Fatal("native option query changed state or lost selected model defaults", options, err)
	}
}

func TestSavedPartialExecutionChoicesKeepNativeOmissions(t *testing.T) {
	f, _ := workerMaterialFacade(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	platform := selectionPlatformFixture(t, "defaults", true)
	for _, step := range []struct {
		input ExecutionSelectionInput
		want  protocol.ExecutionChoice
	}{
		{ExecutionSelectionInput{Node: "产品审查", Platform: platform.descriptor.ID, Model: "2"}, protocol.ExecutionChoice{Provider: platform.descriptor.ID, Model: "opaque-careful"}},
		{ExecutionSelectionInput{Node: "product-review", Effort: "high"}, protocol.ExecutionChoice{Provider: platform.descriptor.ID, Model: "opaque-careful", ReasoningEffort: "high"}},
		{ExecutionSelectionInput{Node: "product-review", Model: "Fast"}, protocol.ExecutionChoice{Provider: platform.descriptor.ID, Model: "opaque-fast", ReasoningEffort: "high"}},
		{ExecutionSelectionInput{Node: "product-review", Effort: "1"}, protocol.ExecutionChoice{Provider: platform.descriptor.ID, Model: "opaque-fast", ReasoningEffort: "low"}},
	} {
		if got := saveExecution(t, f, step.input); got != step.want {
			t.Fatalf("saved choice for %+v = %+v, want %+v", step.input, got, step.want)
		}
	}
	run, err := f.Drive()
	if err != nil || run.ExecutionNodes[0].Execution != (protocol.ExecutionChoice{Provider: platform.descriptor.ID, Model: "opaque-fast", ReasoningEffort: "low"}) {
		t.Fatal("run did not read the saved partial choices", run.ExecutionNodes, err)
	}
	withoutDefault := selectionPlatformFixture(t, "unreported-default", false)
	withoutDefault.descriptor.Installable = false
	if got := saveExecution(t, f, ExecutionSelectionInput{Node: "product-review", Platform: withoutDefault.descriptor.ID}); got != (protocol.ExecutionChoice{Provider: withoutDefault.descriptor.ID}) {
		t.Fatal("unreported native defaults were invented", got)
	}
	defaultOutsideList := selectionPlatformFixture(t, "native-default", true)
	defaultOutsideList.options = func(root, runtimeTarget string, native map[string]string, model string) (host.SelectionCatalogue, error) {
		return host.SelectionCatalogue{Default: host.ModelSelection{Model: "native-default-outside-list", Effort: "native-default-effort"}, Models: []host.ModelOption{{Value: "listed-model", Label: "Listed"}}}, nil
	}
	if got := saveExecution(t, f, ExecutionSelectionInput{Node: "product-review", Platform: defaultOutsideList.descriptor.ID}); got != (protocol.ExecutionChoice{Provider: defaultOutsideList.descriptor.ID}) {
		t.Fatal("native default outside the choice list was materialized or replaced", got)
	}
}

func TestExecutionSelectionRejectsUnavailableNativeChoices(t *testing.T) {
	f, _ := workerMaterialFacade(t)
	platform := selectionPlatformFixture(t, "available", true)
	t.Parallel()
	snap, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	native, current := snap.State.Settings.Values.Native, snap.State.EffectiveExecutionChoice("product-review")
	prepare := func(input ExecutionSelectionInput) error {
		_, err := PrepareExecutionSelection(f.root, f.admission.Target, native, current, input)
		return err
	}
	for _, input := range []ExecutionSelectionInput{
		{Node: "product-review", Platform: "missing-native-platform"},
		{Node: "product-review", Platform: host.Default},
		{Node: "product-review", Platform: platform.descriptor.ID, Model: "missing-model"},
		{Node: "product-review", Platform: platform.descriptor.ID, Model: "99"},
		{Node: "product-review", Platform: platform.descriptor.ID, Effort: "ultra"},
		{Node: "product-review", Platform: platform.descriptor.ID, Effort: "99"},
	} {
		if err := prepare(input); err == nil || !strings.Contains(err.Error(), "available") && !strings.Contains(err.Error(), "adapter") && !strings.Contains(err.Error(), "native options") {
			t.Fatalf("unavailable execution choice was not precise: %+v %v", input, err)
		}
	}
	input := ExecutionSelectionInput{Node: "product-review", Platform: platform.descriptor.ID, Model: "Careful", Effort: "high"}
	for _, test := range []struct {
		efforts []string
		problem string
	}{
		{nil, "did not report effort options"},
		{[]string{}, "no selectable reasoning effort"},
		{[]string{"medium"}, "native options: [medium]"},
	} {
		platform.options = func(root, runtimeTarget string, native map[string]string, model string) (host.SelectionCatalogue, error) {
			return host.SelectionCatalogue{Models: []host.ModelOption{{Value: "opaque-careful", Label: "Careful", Efforts: test.efforts}}}, nil
		}
		if err := prepare(input); err == nil || !strings.Contains(err.Error(), test.problem) {
			t.Fatal("effort report did not preserve its native meaning", test.problem, err)
		}
	}
	platform.options = func(root, runtimeTarget string, native map[string]string, model string) (host.SelectionCatalogue, error) {
		return host.SelectionCatalogue{}, fmt.Errorf("native executable is not installed: fixture-command")
	}
	if _, err := f.QueryExecutionOptions(input); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatal("native installation failure was hidden", err)
	}
}

func TestSavedExecutionRefreshesReadyAndPreservesClaim(t *testing.T) {
	f, root := workerMaterialFacade(t)
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	var ready WorkerMaterial
	for _, material := range run.WorkerMaterials {
		ready = material
	}
	originalBytes := readWorkerFile(t, ready.Path)
	platform := selectionPlatformFixture(t, "selected", true)
	saveExecution(t, f, ExecutionSelectionInput{Node: "product-review", Platform: platform.descriptor.ID, Model: "Careful", Effort: "high"})
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	request, _, directory, selected, err := f.WorkerExecutionRequest(ready, "")
	if err != nil || selected == ready || selected.ActionID != ready.ActionID || selected.AttemptID != ready.AttemptID || request["provider"] != platform.descriptor.InstallName || request["model"] != "opaque-careful" || request["reasoning_effort"] != "high" {
		t.Fatalf("Ready reference was not refreshed from the current attempt: %+v %+v %v", selected, request, err)
	}
	if !bytes.Equal(originalBytes, readWorkerFile(t, ready.Path)) {
		t.Fatal("new Ready rendering overwrote original material bytes")
	}
	request["binding"] = map[string]any{"fixture": "original native binding"}
	request["permission_mode"] = "original permission"
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONAtomic(filepath.Join(directory, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	release, err := AdmitExecution(directory, request)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(directory, "owner"), []byte("native owner"), 0600)
	release()
	if err != nil {
		t.Fatal(err)
	}
	next := selectionPlatformFixture(t, "future", true)
	saveExecution(t, f, ExecutionSelectionInput{Node: "product-review", Platform: next.descriptor.ID})
	reopened, err := Open(root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.Drive(); err != nil {
		t.Fatal(err)
	}
	resumed, _, sameDirectory, actual, err := reopened.WorkerExecutionRequest(ready, "")
	if err != nil || sameDirectory != directory || actual != selected || !jsonValuesEqual(request, resumed) {
		t.Fatal("claimed worker did not retain its original native request", resumed, err)
	}
	snap, err := reopened.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	choice, ok := snap.State.ActionExecutionChoice(ready.ActionID)
	if !ok || choice.Provider != platform.descriptor.ID || choice.Model != "opaque-careful" || choice.ReasoningEffort != "high" || snap.State.EffectiveExecutionChoice("product-review").Provider != next.descriptor.ID {
		t.Fatal("a later saved value replaced original action responsibility", choice)
	}
}

func TestQAExecutionGroupsSelectAndRetainTheirWholePipeline(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "whitebox", "quality")
	blackPlatform := selectionPlatformFixture(t, "black", true)
	whitePlatform := selectionPlatformFixture(t, "white", true)
	t.Parallel()
	saveExecution(t, d.f, ExecutionSelectionInput{Node: "4", Platform: blackPlatform.descriptor.ID, Model: "Careful", Effort: "high"})
	saveExecution(t, d.f, ExecutionSelectionInput{Node: "白盒 QA 的设计、独立审查与执行", Platform: whitePlatform.descriptor.ID, Model: "Fast", Effort: "low"})
	saveExecution(t, d.f, ExecutionSelectionInput{Node: "qa-blackbox", Effort: "medium"})
	want := map[coverage.ReviewKind]protocol.ExecutionChoice{
		coverage.ReviewBlackbox: {Provider: blackPlatform.descriptor.ID, Model: "opaque-careful", ReasoningEffort: "medium"},
		coverage.ReviewWhitebox: {Provider: whitePlatform.descriptor.ID, Model: "opaque-fast", ReasoningEffort: "low"},
	}
	if _, err := d.f.QueryExecutionOptions(ExecutionSelectionInput{Node: "qa"}); err == nil {
		t.Fatal("public options accepted an ambiguous shared QA group")
	}
	d.reopen()
	seen := map[coverage.ReviewKind]map[string]bool{coverage.ReviewBlackbox: {}, coverage.ReviewWhitebox: {}}
	check := func(id string, input protocol.ValidationInput) {
		t.Helper()
		mode := input.Manifest.Binding.ReviewKind
		state := driverState(t, d)
		worker, ok := state.WorkerInput(id, d.f.definition)
		if !ok || worker.Execution != want[mode] || state.ExecutionNode(input.Binding.Task, d.f.definition) != "qa-"+strings.ToLower(string(mode)) {
			t.Fatal("QA worker lost its group's complete tuple", input.Handler, mode, worker.Execution, want[mode])
		}
		entry := protocol.ConfirmedRevision{StateRevision: 100, Revision: "revised-requirement", WorkDecisions: []protocol.RequirementWorkDecision{{Work: protocol.RequirementWork{Task: input.Binding.Task}, Disposition: protocol.RequirementWorkRedo}}}
		state.RequirementRevisions = append(state.RequirementRevisions, entry)
		replacement := input.Binding.Task
		replacement.Scope = "requirement:100:0"
		if state.ExecutionNode(replacement, d.f.definition) != state.ExecutionNode(input.Binding.Task, d.f.definition) {
			t.Fatal("authorized replacement lost its group's source task", mode, input.Handler)
		}
		seen[mode][input.Handler] = true
	}
	business := func(id string, input protocol.ValidationInput, result protocol.ValidationResult) {
		t.Helper()
		check(id, input)
		original := d.provider
		d.provider = want[input.Manifest.Binding.ReviewKind].Provider
		d.receipt(id)
		d.provider = original
		d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: want[input.Manifest.Binding.ReviewKind].Provider, Outcome: protocol.OutcomePass, PayloadDigest: string(d.eventID()), Validation: &result}})
		if d.mockNative && result.Development != nil {
			d.drive()
		}
	}
	dev, devInput := d.action("engine.development.batch.v6")
	d.business(dev, protocol.ValidationResult{Binding: devInput.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implemented command"}})
	d.materialize()
	d.materialize()
	black, blackInput := qaModeAction(d, "engine.qa.design.v6", coverage.ReviewBlackbox)
	blackManifest := simpleManifest(blackInput)
	business(black, blackInput, protocol.ValidationResult{Binding: blackInput.Binding, Status: "PASS", Manifest: &blackManifest})
	black, blackInput = qaModeAction(d, "engine.qa.review.rationale.v2", coverage.ReviewBlackbox)
	blackReview := approvedReview(blackManifest)
	business(black, blackInput, protocol.ValidationResult{Binding: blackInput.Binding, Status: "PASS", Review: &blackReview})
	white, whiteInput := qaModeAction(d, "engine.qa.design.v6", coverage.ReviewWhitebox)
	whiteManifest := whiteInput.Manifest
	whiteManifest.Cases = []coverage.AcceptanceCase{{CaseID: "white-valid", Mode: coverage.CaseWhitebox, Scenario: "SUCCESS", TestRef: "app_test.go::TestValid", Oracle: "valid input succeeds"}, {CaseID: "white-invalid", Mode: coverage.CaseWhitebox, Scenario: "FAILURE", TestRef: "app_test.go::TestInvalid", Oracle: "invalid input is rejected"}}
	whiteManifest.Edges = []coverage.CoverageEdge{{SourceID: "REQ-001", PointID: "AC-001", CaseID: "white-valid"}, {SourceID: "REQ-001", PointID: "AC-001", CaseID: "white-invalid"}}
	business(white, whiteInput, protocol.ValidationResult{Binding: whiteInput.Binding, Status: "PASS", Manifest: &whiteManifest, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go"}}})
	d.materialize()
	white, whiteInput = qaModeAction(d, "engine.qa.review.rationale.v2", coverage.ReviewWhitebox)
	whiteReview := approvedReview(whiteManifest)
	business(white, whiteInput, protocol.ValidationResult{Binding: whiteInput.Binding, Status: "PASS", Review: &whiteReview})
	d.materialize()
	for _, mode := range []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox} {
		id, input := qaModeAction(d, "engine.qa.execution.v4", mode)
		check(id, input)
		if len(seen[mode]) != 3 {
			t.Fatal("QA group fixture did not cover design, independent review and execution", mode, seen[mode])
		}
	}
	d.reopen()
	for mode, choice := range want {
		if got := driverState(t, d).EffectiveExecutionChoice("qa-" + strings.ToLower(string(mode))); got != choice {
			t.Fatal("restart changed one group's saved tuple", mode, got, choice)
		}
	}
}
