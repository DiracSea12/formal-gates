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
)

type executionSelectionPlatform struct {
	descriptor host.Descriptor
	options    func(string, string, map[string]string, string) (host.SelectionCatalogue, error)
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

func selectExecution(t *testing.T, f *Facade, input ExecutionSelectionInput) (ControlCandidate, Run) {
	t.Helper()
	candidate, err := f.PrepareControl(ControlInput{Execution: &input})
	if err != nil {
		t.Fatal(err)
	}
	accepted, run, err := submitControlWithHost(t, f, candidate)
	if err != nil || accepted.Status != "ACCEPTED" || accepted.Kind != string(protocol.KindRequestControl) {
		t.Fatalf("execution request did not create its typed Ask: %+v %v", accepted, err)
	}
	confirmation, err := f.PrepareControl(ControlInput{Ask: askPosition(t, run, protocol.ControlSelectExecution), Choice: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	accepted, run, err = submitControlWithHost(t, f, confirmation)
	if err != nil || accepted.Kind != string(protocol.KindDecide) {
		t.Fatal("execution confirmation did not accept its original tuple", accepted, err)
	}
	return candidate, run
}

func TestExecutionSelectionPartialChoicesAndReplay(t *testing.T) {
	f, root := workerMaterialFacade(t)
	platform := selectionPlatformFixture(t, "defaults", true)
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
	candidate, _ := selectExecution(t, f, ExecutionSelectionInput{Node: "产品审查", Platform: platform.descriptor.ID, Model: "2"})
	want := protocol.ExecutionChoice{Provider: platform.descriptor.ID, Model: "opaque-careful"}
	if candidate.Event.Request.Execution.Choice != want {
		t.Fatalf("platform change did not leave omitted effort to the native model default: %+v", candidate.Event.Request.Execution)
	}
	acceptedState := readWorkerFile(t, statePath)
	if _, _, err := submitControlWithHost(t, f, candidate); err != nil || !bytes.Equal(acceptedState, readWorkerFile(t, statePath)) {
		t.Fatal("original selection replay changed state", err)
	}
	selectExecution(t, f, ExecutionSelectionInput{Node: "product-review", Effort: "high"})
	modelOnly, _ := selectExecution(t, f, ExecutionSelectionInput{Node: "product-review", Model: "Fast"})
	if modelOnly.Event.Request.Execution.Choice.ReasoningEffort != "high" {
		t.Fatal("same-platform model-only selection did not inherit effort")
	}
	selectExecution(t, f, ExecutionSelectionInput{Node: "product-review", Effort: "1"})
	want.Model, want.ReasoningEffort = "opaque-fast", "low"
	if err := os.WriteFile(filepath.Join(root, ".gates", "config.json"), []byte(`{"model":"later-config","reasoningEffort":"ultra"}`), 0600); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	after, err := reopened.engine.Load()
	if err != nil || after.State.EffectiveExecutionChoice("product-review") != want || !reflect.DeepEqual(before.State.Settings, after.State.Settings) {
		t.Fatal("partial selection or reopen lost frozen execution/configuration facts", err)
	}
	withoutDefault := selectionPlatformFixture(t, "unreported-default", false)
	withoutDefault.descriptor.Installable = false
	_, run := selectExecution(t, reopened, ExecutionSelectionInput{Node: "product-review", Platform: withoutDefault.descriptor.ID})
	if run.ExecutionNodes[0].Execution != (protocol.ExecutionChoice{Provider: withoutDefault.descriptor.ID}) {
		t.Fatal("unreported native defaults were invented", run.ExecutionNodes[0].Execution)
	}
	defaultOutsideList := selectionPlatformFixture(t, "native-default", true)
	defaultOutsideList.options = func(root, runtimeTarget string, native map[string]string, model string) (host.SelectionCatalogue, error) {
		return host.SelectionCatalogue{Default: host.ModelSelection{Model: "native-default-outside-list", Effort: "native-default-effort"}, Models: []host.ModelOption{{Value: "listed-model", Label: "Listed"}}}, nil
	}
	_, run = selectExecution(t, reopened, ExecutionSelectionInput{Node: "product-review", Platform: defaultOutsideList.descriptor.ID})
	if run.ExecutionNodes[0].Execution != (protocol.ExecutionChoice{Provider: defaultOutsideList.descriptor.ID}) {
		t.Fatal("native default outside the choice list was materialized or replaced", run.ExecutionNodes[0].Execution)
	}
	if data := readWorkerFile(t, filepath.Join(root, ".gates", "config.json")); string(data) != `{"model":"later-config","reasoningEffort":"ultra"}` {
		t.Fatal("temporary selection modified project defaults", string(data))
	}
}

func TestExecutionSelectionResumesItsOriginalTypedRequest(t *testing.T) {
	f, root := workerMaterialFacade(t)
	platform := selectionPlatformFixture(t, "pending", true)
	input := ExecutionSelectionInput{Node: "development", Platform: platform.descriptor.ID}
	candidate, err := f.PrepareControl(ControlInput{Execution: &input})
	if err != nil {
		t.Fatal(err)
	}
	requested, _, err := f.Submit(candidate.Event)
	if err != nil || requested.Kind != string(protocol.KindRequestControl) {
		t.Fatal("original typed request was not persisted", requested, err)
	}
	reopened, err := Open(root, f.runID)
	if err != nil {
		t.Fatal(err)
	}
	accepted, pending, err := submitControlWithHost(t, reopened, candidate)
	if err != nil || accepted.Kind != string(protocol.KindRequestControl) || accepted.RequestID != requested.RequestID {
		t.Fatal("reopen did not preserve the original pending request", accepted, err)
	}
	confirmation, err := reopened.PrepareControl(ControlInput{Ask: askPosition(t, pending, protocol.ControlSelectExecution), Choice: "approve"})
	if err != nil {
		t.Fatal(err)
	}
	accepted, _, err = submitControlWithHost(t, reopened, confirmation)
	if err != nil || accepted.Kind != string(protocol.KindDecide) || accepted.RequestID != requested.RequestID {
		t.Fatal("reopen confirmation did not finish the original request", accepted, err)
	}
	state, err := reopened.engine.Load()
	if err != nil || state.State.EffectiveExecutionChoice("development") != candidate.Event.Request.Execution.Choice {
		t.Fatal("reopen changed the original prepared tuple", err)
	}
}

func TestExecutionSelectionRejectsUnavailableChoicesBeforeWrite(t *testing.T) {
	f, _ := workerMaterialFacade(t)
	platform := selectionPlatformFixture(t, "available", true)
	statePath := filepath.Join(f.stateDir, "state.json")
	original := readWorkerFile(t, statePath)
	for _, input := range []ExecutionSelectionInput{
		{Node: "product-review", Platform: "missing-native-platform"},
		{Node: "product-review", Platform: host.Default},
		{Node: "product-review", Platform: platform.descriptor.ID, Model: "missing-model"},
		{Node: "product-review", Platform: platform.descriptor.ID, Model: "99"},
		{Node: "product-review", Platform: platform.descriptor.ID, Effort: "ultra"},
		{Node: "product-review", Platform: platform.descriptor.ID, Effort: "99"},
	} {
		if _, err := f.PrepareControl(ControlInput{Execution: &input}); err == nil || !strings.Contains(err.Error(), "available") && !strings.Contains(err.Error(), "adapter") && !strings.Contains(err.Error(), "native options") {
			t.Fatalf("unavailable execution choice was not precise: %+v %v", input, err)
		}
		if !bytes.Equal(original, readWorkerFile(t, statePath)) {
			t.Fatal("invalid preparation changed state")
		}
	}
	input := ExecutionSelectionInput{Node: "product-review", Platform: platform.descriptor.ID, Model: "Careful", Effort: "high"}
	candidate, err := f.PrepareControl(ControlInput{Execution: &input})
	if err != nil {
		t.Fatal(err)
	}
	platform.options = func(root, runtimeTarget string, native map[string]string, model string) (host.SelectionCatalogue, error) {
		return host.SelectionCatalogue{Models: []host.ModelOption{{Value: "opaque-fast", Label: "Fast", Efforts: []string{"low"}}}}, nil
	}
	if _, _, err := submitControlWithHost(t, f, candidate); err == nil || !strings.Contains(err.Error(), "opaque-fast") {
		t.Fatal("update did not recheck original native options before writing", err)
	}
	if !bytes.Equal(original, readWorkerFile(t, statePath)) {
		t.Fatal("invalid update wrote a request or changed an existing decision")
	}
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
		if _, _, err := submitControlWithHost(t, f, candidate); err == nil || !strings.Contains(err.Error(), test.problem) || !bytes.Equal(original, readWorkerFile(t, statePath)) {
			t.Fatal("effort report did not preserve its native meaning and existing state", test.problem, err)
		}
	}
	platform.options = func(root, runtimeTarget string, native map[string]string, model string) (host.SelectionCatalogue, error) {
		return host.SelectionCatalogue{}, fmt.Errorf("native executable is not installed: fixture-command")
	}
	if _, err := f.QueryExecutionOptions(input); err == nil || !strings.Contains(err.Error(), "not installed") {
		t.Fatal("native installation failure was hidden", err)
	}
}

func TestExecutionSelectionRefreshesReadyAndPreservesClaim(t *testing.T) {
	f, root := workerMaterialFacade(t)
	run, err := f.Drive()
	if err != nil {
		t.Fatal(err)
	}
	var ready WorkerMaterial
	for _, material := range run.WorkerMaterials {
		ready = material
	}
	originalBytes := readWorkerFile(t, ready.Path)
	platform := selectionPlatformFixture(t, "selected", true)
	selectExecution(t, f, ExecutionSelectionInput{Node: "product-review", Platform: platform.descriptor.ID, Model: "Careful", Effort: "high"})
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
	selectExecution(t, f, ExecutionSelectionInput{Node: "product-review", Platform: next.descriptor.ID})
	reopened, err := Open(root, f.runID)
	if err != nil {
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
		t.Fatal("future selection replaced original action responsibility", choice)
	}
}

func TestExecutionSelectionChildInheritanceIsCreationSnapshot(t *testing.T) {
	master := nativeMasterDriver(t)
	platform := selectionPlatformFixture(t, "inherited", true)
	selectExecution(t, master.f, ExecutionSelectionInput{Node: "development", Platform: platform.descriptor.ID, Model: "Careful"})
	selectExecution(t, master.f, ExecutionSelectionInput{Node: "qa-blackbox", Platform: platform.descriptor.ID, Model: "Careful", Effort: "medium"})
	selectExecution(t, master.f, ExecutionSelectionInput{Node: "qa-whitebox", Platform: platform.descriptor.ID, Model: "Fast", Effort: "low"})
	passPreDevelopmentReviews(t, master, splitCommandPlan())
	master.control(protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{Topology: splitTopologyDecision(protocol.ACOwner{AC: "AC-001", Owner: "lane-core"})})
	child, _, err := createdSliceFacade(t, master.f, "lane-core", OwnerBinding{})
	if err != nil {
		t.Fatal(err)
	}
	selectExecution(t, master.f, ExecutionSelectionInput{Node: "development", Effort: "medium"})
	selectExecution(t, master.f, ExecutionSelectionInput{Node: "qa-blackbox", Effort: "high"})
	reopened, err := Open(child.root, child.runID)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := reopened.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	want := protocol.ExecutionChoice{Provider: platform.descriptor.ID, Model: "opaque-careful"}
	if snap.State.EffectiveExecutionChoice("development") != want || snap.State.SliceChild == nil {
		t.Fatal("existing child followed a later parent choice", snap.State.EffectiveExecutionChoice("development"))
	}
	want.ReasoningEffort = "medium"
	if snap.State.EffectiveExecutionChoice("qa-blackbox") != want || snap.State.EffectiveExecutionChoice("qa-whitebox") != (protocol.ExecutionChoice{Provider: platform.descriptor.ID, Model: "opaque-fast", ReasoningEffort: "low"}) {
		t.Fatal("child did not preserve both QA creation choices")
	}
}

func TestQAExecutionGroupsSelectAndRetainTheirWholePipeline(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "whitebox", "quality")
	blackPlatform := selectionPlatformFixture(t, "black", true)
	whitePlatform := selectionPlatformFixture(t, "white", true)
	selectExecution(t, d.f, ExecutionSelectionInput{Node: "4", Platform: blackPlatform.descriptor.ID, Model: "Careful", Effort: "high"})
	selectExecution(t, d.f, ExecutionSelectionInput{Node: "白盒 QA 的设计、独立审查与执行", Platform: whitePlatform.descriptor.ID, Model: "Fast", Effort: "low"})
	selectExecution(t, d.f, ExecutionSelectionInput{Node: "qa-blackbox", Effort: "medium"})
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
			t.Fatal("restart changed one group's accepted tuple", mode, got, choice)
		}
	}
}
