package facade

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/settings"
)

func passDevelopmentBatchGates(d *validationDriver) {
	d.t.Helper()
	var inputs = map[string]protocol.ValidationInput{}
	for id, input := range d.run.ValidationInputs {
		if input.Gate != nil && input.Binding.BatchID != "" {
			inputs[id] = input
		}
	}
	if len(inputs) == 0 {
		d.t.Fatal("expected separately issued ordinary batch gates")
	}
	for id, input := range inputs {
		if len(input.Gate.Batches) != 1 || input.Gate.Batches[0].ID != input.Binding.BatchID {
			d.t.Fatal("batch gate lost its exact original contribution contract")
		}
		d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
	}
	d.drive()
}

// A delivery batch's ordinary gate must pass before its dependent batch starts.
func TestDevelopmentGatePrecedesDependentBatch(t *testing.T) {
	p := commandPlan()
	consumer := commandPlan().Batches[0]
	consumer.ID = "consumer"
	consumer.Subtasks = []protocol.DevelopmentSubtask{{ID: "consumer-body", Description: "Consume the predecessor command contract"}}
	consumer.DependsOn = []string{p.Batches[0].ID}
	consumer.Artifacts = []string{"consumer.go"}
	p.Batches = append(p.Batches, consumer)
	d := nativeMasterDriver(t)
	passPreDevelopmentReviews(t, d, p)
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "Two dependent deliveries share the command workspace"}})
	d.confirmRoute(routeDecisionForGates([]string{"quality"}))
	d.drive()
	id, in := planBatchAction(d, p.Batches[0].ID)
	if err := os.WriteFile(filepath.Join(in.Workspace.Path, "app.go"), []byte("package main\nfunc main() { println(\"ready\") }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: batchEvidence(in)})
	d.drive()
	for _, input := range d.run.ValidationInputs {
		if input.Development != nil && input.Development.Batch.ID == consumer.ID {
			t.Fatal("dependent batch started before its predecessor's selected ordinary gate passed")
		}
	}
	d.reopen()
	d.drive()
	gate, input := d.gateAction("quality")
	d.business(gate, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
	d.drive()
	_, next := planBatchAction(d, consumer.ID)
	if next.Development == nil {
		t.Fatal("passing the predecessor gate did not release the dependent batch")
	}
}

func TestDevelopmentGatesKeepIndependentBatchesAndQADesignParallel(t *testing.T) {
	p := commandPlan()
	independent := commandPlan().Batches[0]
	independent.ID = "independent"
	independent.Subtasks = []protocol.DevelopmentSubtask{{ID: "utility", Description: "Deliver the independent utility boundary"}}
	independent.Artifacts = []string{"independent.go"}
	consumer := independent
	consumer.ID, consumer.Artifacts = "consumer", []string{"consumer.go"}
	consumer.Subtasks = []protocol.DevelopmentSubtask{{ID: "consume", Description: "Consume the verified command boundary"}}
	consumer.DependsOn = []string{p.Batches[0].ID}
	p.Batches = append(p.Batches, independent, consumer)
	d := nativeMasterDriver(t)
	passPreDevelopmentReviews(t, d, p)
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "Independent command and utility with a command consumer"}})
	d.confirmRoute(routeDecisionForGates([]string{"quality", "blackbox"}))
	d.drive()
	first, a := planBatchAction(d, "command")
	other, b := planBatchAction(d, "independent")
	d.action("engine.qa.design.v6")
	for _, input := range []protocol.ValidationInput{a, b} {
		body := "package main\n"
		if input.Binding.BatchID == "command" {
			body += "func main() { println(\"ready\") }\n"
		}
		if err := os.WriteFile(filepath.Join(input.Workspace.Path, input.Development.Batch.Artifacts[0]), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	d.business(first, protocol.ValidationResult{Binding: a.Binding, Status: "PASS", Development: batchEvidence(a)})
	d.business(other, protocol.ValidationResult{Binding: b.Binding, Status: "PASS", Development: batchEvidence(b)})
	d.drive()
	gateInputs := map[string]protocol.ValidationInput{}
	for id, input := range d.run.ValidationInputs {
		if input.Handler == "engine.qa.execution.v4" {
			t.Fatal("FINAL QA execution was pulled into an intermediate batch gate")
		}
		if input.Gate != nil {
			gateInputs[id] = input
		}
	}
	if len(gateInputs) != 2 {
		t.Fatalf("independent batches need two real gate obligations, got %d", len(gateInputs))
	}
	var candidate string
	for _, input := range gateInputs {
		if candidate != "" && candidate != input.Binding.Candidate {
			t.Fatal("parallel batch gates lost their shared frozen editing frontier")
		}
		candidate = input.Binding.Candidate
		if len(input.Gate.Batches) != 1 || input.Gate.Batches[0].ID != input.Binding.BatchID {
			t.Fatal("separate batch gate did not retain its exact contribution")
		}
	}
	d.reopen()
	d.drive()
	for id, input := range gateInputs {
		if input.Binding.BatchID == "command" {
			d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
		}
	}
	d.drive()
	planBatchAction(d, "consumer")
	for _, input := range d.run.ValidationInputs {
		if input.Gate != nil && input.Binding.BatchID == "independent" {
			return
		}
	}
	t.Fatal("a ready dependent batch incorrectly consumed its independent peer's gate")
}

func TestDevelopmentGateDeselectionReleasesTheDependencyAndReplays(t *testing.T) {
	p := commandPlan()
	consumer := commandPlan().Batches[0]
	consumer.ID = "consumer"
	consumer.Subtasks = []protocol.DevelopmentSubtask{{ID: "consume", Description: "Consume the command boundary"}}
	consumer.DependsOn, consumer.Artifacts = []string{"command"}, []string{"consumer.go"}
	p.Batches = append(p.Batches, consumer)
	d := nativeMasterDriver(t)
	passPreDevelopmentReviews(t, d, p)
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "Dependent command deliveries"}})
	d.confirmRoute(routeDecisionForGates([]string{"quality"}))
	d.drive()
	id, input := planBatchAction(d, "command")
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main\nfunc main() { println(\"ready\") }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: batchEvidence(input)})
	d.drive()
	d.gateAction("quality")
	route := routeDecisionForGates([]string{})
	d.configureRoute(&route)
	d.drive()
	planBatchAction(d, "consumer")
	snapshot := driverState(t, d)
	history := len(snapshot.AppliedConfigurations)
	for _, batch := range d.run.FlowProgress.DevelopmentBatches {
		if batch.Gate == nil || batch.Gate.Status != "DISABLED" {
			t.Fatal("empty ordinary gate selection did not project a disabled batch gate")
		}
	}
	d.reopen()
	d.drive()
	d.drive()
	if got := len(driverState(t, d).AppliedConfigurations); got != history {
		t.Fatalf("repeat drive/reopen reapplied the same selected gates: %d -> %d", history, got)
	}
}

func TestSliceDevelopmentGatePrecedesItsDependentBatch(t *testing.T) {
	p := splitCommandPlan()
	consumer := p.Batches[0]
	consumer.ID, consumer.Artifacts = "consumer", []string{"consumer.go"}
	consumer.Subtasks = []protocol.DevelopmentSubtask{{ID: "consume", Description: "Consume the original slice command"}}
	consumer.DependsOn = []string{p.Batches[0].ID}
	p.Batches = append(p.Batches, consumer)
	master := nativeMasterDriver(t)
	passPreDevelopmentReviews(t, master, p)
	master.control(protocol.ControlConfirmTopology, "split", protocol.ReviewDecisionPayload{Topology: splitTopologyDecision(protocol.ACOwner{AC: "AC-001", Owner: "lane-core"})})
	master.drive()
	parent := driverState(t, master)
	f, err := Open(master.f.root, parent.SliceChildren["lane-core"])
	if err != nil {
		t.Fatal(err)
	}
	run, err := f.Show()
	if err != nil {
		t.Fatal(err)
	}
	child := &validationDriver{t: t, f: f, run: run, revision: master.revision, git: f.vcs}
	plan, err := parent.SliceDevelopmentPlan("lane-core")
	if err != nil {
		t.Fatal(err)
	}
	child.confirmSliceRoute(protocol.RouteDecision{Gates: []string{"quality"}, Selected: []string{"quality"}}, plan)
	child.drive()
	id, input := planBatchAction(child, "command")
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main\nfunc main() { println(\"ready\") }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	child.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: batchEvidence(input)})
	child.drive()
	for _, input := range child.run.ValidationInputs {
		if input.Development != nil && input.Binding.BatchID == "consumer" {
			t.Fatal("slice dependent batch bypassed its ordinary gate")
		}
	}
	child.reopen()
	child.drive()
	passDevelopmentBatchGates(child)
	planBatchAction(child, "consumer")
}

func TestDevelopmentGatesPreserveUnequalDiamondSourcesThroughFinalQA(t *testing.T) {
	p := commandPlan()
	p.Batches = nil
	for _, id := range []string{"a", "b", "c", "d", "e"} {
		batch := commandPlan().Batches[0]
		batch.ID = id
		batch.Subtasks = []protocol.DevelopmentSubtask{{ID: id + "-body", Description: "Implement the " + id + " command contribution"}}
		batch.Artifacts = []string{id + ".go"}
		switch id {
		case "b", "c":
			batch.DependsOn = []string{"a"}
		case "d":
			batch.DependsOn = []string{"c"}
		case "e":
			batch.DependsOn = []string{"b", "d"}
		}
		p.Batches = append(p.Batches, batch)
	}
	d := nativeMasterDriver(t)
	passPreDevelopmentReviews(t, d, p)
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "A command contribution fans out into unequal branches before a two-input join"}})
	d.confirmRoute(routeDecisionForGates([]string{"quality", "blackbox"}))
	d.drive()
	design, input := d.action("engine.qa.design.v6")
	manifest := simpleManifest(input)
	d.business(design, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
	d.drive()
	reviewer, input := d.action("engine.qa.review.rationale.v2")
	review := approvedReview(manifest)
	d.business(reviewer, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &review})
	d.drive()
	results := map[string]string{}
	contents := map[string]string{}
	complete := func(batch string) protocol.ValidationInput {
		t.Helper()
		id, input := planBatchAction(d, batch)
		body := fmt.Sprintf("package main\nvar %sReady = true\n", batch)
		path := input.Development.Batch.Artifacts[0]
		if err := os.WriteFile(filepath.Join(input.Workspace.Path, path), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
		contents[path] = body
		d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: batchEvidence(input)})
		d.drive()
		results[id] = driverState(t, d).Results[id].Digest
		return input
	}
	gate := func(batch string) (string, protocol.ValidationInput) {
		t.Helper()
		for id, input := range d.run.ValidationInputs {
			if input.Gate != nil && input.Binding.BatchID == batch {
				if len(input.Gate.Batches) != 1 || input.Gate.Batches[0].ID != batch || input.Gate.Candidate.Identity != input.Binding.Candidate {
					t.Fatal("batch gate lost its exact contribution or immutable candidate")
				}
				return id, input
			}
		}
		t.Fatalf("missing gate for completed batch %s", batch)
		return "", protocol.ValidationInput{}
	}
	pass := func(batch string) {
		t.Helper()
		id, input := gate(batch)
		d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
		d.drive()
	}
	joinPending := func() {
		t.Helper()
		for _, binding := range driverState(t, d).DispatchBindings {
			if binding.Task.Step == "development.worker" && binding.BatchID == "e" {
				t.Fatal("the join started before both predecessor batch gates passed")
			}
		}
		for _, input := range d.run.ValidationInputs {
			if input.Handler == "engine.qa.execution.v4" {
				t.Fatal("FINAL QA execution started before the full diamond delivery")
			}
		}
	}
	complete("a")
	pass("a")
	planBatchAction(d, "b")
	planBatchAction(d, "c")
	b := complete("b")
	complete("c")
	bGate, before := gate("b")
	_, peer := gate("c")
	if before.Binding.Candidate != peer.Binding.Candidate {
		t.Fatal("independent sibling gates did not share their frozen editing frontier")
	}
	pass("c")
	dInput := complete("d")
	joinPending()
	d.reopen()
	d.drive()
	joinPending()
	current, issued := driverState(t, d).WorkerInput(bGate, d.f.definition)
	if !issued || current.Validation == nil || current.Validation.Binding != before.Binding {
		t.Fatal("the unfinished short-branch gate lost its original candidate across the long-branch edit and reopen")
	}
	pass("b")
	_, dGate := gate("d")
	if dGate.Binding.Candidate == before.Binding.Candidate {
		t.Fatal("the long-branch gate reused a candidate frozen before its edit")
	}
	joinPending()
	pass("d")
	_, joined := planBatchAction(d, "e")
	if len(joined.Development.Dependencies) != 2 || joined.Development.Dependencies["b"].Validation.Binding != b.Binding || joined.Development.Dependencies["d"].Validation.Binding != dInput.Binding {
		t.Fatal("the two-input join did not receive both original predecessor results")
	}
	complete("e")
	qa, qaInput := d.action("engine.qa.execution.v4")
	finalGate, finalInput := d.gateAction("quality")
	if finalInput.Binding.BatchID != "" || len(finalInput.Gate.Batches) != 0 || len(d.run.Validation.Waves[len(d.run.Validation.Waves)-1].SourceTasks) != len(p.Batches) {
		t.Fatal("final whole-project validation lost its full responsibility or original diamond sources")
	}
	if len(qaInput.ExecutionFacts) < 2 {
		t.Fatal("final QA did not execute the native approved valid/invalid input operations")
	}
	d.business(qa, mainlineResult(qaInput))
	d.business(finalGate, protocol.ValidationResult{Binding: finalInput.Binding, Status: "PASS"})
	d.drive()
	d.reopen()
	d.drive()
	state := driverState(t, d)
	if !state.FinalValidationComplete(d.f.definition) {
		t.Fatal("the fully gated diamond did not reach complete final QA and whole-project validation")
	}
	for action, digest := range results {
		if state.Results[action].Digest != digest {
			t.Fatal("a later branch, gate or reopening rewrote an original developer result")
		}
	}
	for path, want := range contents {
		actual, err := (vcs.ExecRunner{}).RunRaw(d.f.root, "git", nil, "show", state.Validation.Candidate.Identity+":"+path)
		if err != nil || string(actual) != want {
			t.Fatalf("final immutable candidate lost %s: %q, %v", path, actual, err)
		}
	}
	if !slices.Contains(state.SelectedGates, "quality") || !slices.Contains(state.SelectedGates, "blackbox") {
		t.Fatal("a branch gate changed the shared configuration selection")
	}
}

// Configuration changes preserve a repaired batch duty independently of the
// frozen FINAL QA design and the later dependent delivery.
func TestRepairedDevelopmentGateKeepsItsResponsibilityAcrossConfiguration(t *testing.T) {
	for _, change := range []string{"unchanged", "add", "remove", "empty", "disabled", "restore"} {
		t.Run(change, func(t *testing.T) {
			selected := []string{"quality", "blackbox"}
			if change == "remove" {
				selected = append(selected, "security")
			}
			d := repairedDevelopmentGateDriver(t, selected)
			before := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			candidate := *d.run.Validation.Candidate
			if len(before.CheckpointTasks) != 1 || before.Status != "COLLECTING" {
				t.Fatal("fixture did not reach the repaired original batch gate")
			}
			switch change {
			case "add":
				selected = append(selected, "security")
			case "remove":
				selected = []string{"quality", "blackbox"}
			case "empty":
				selected = []string{"blackbox"}
			case "disabled":
				home, err := os.UserHomeDir()
				if err != nil {
					t.Fatal(err)
				}
				if _, err := settings.EditConfiguration(d.f.root, home, "project", driverState(t, d).ConfigurationName(), nil, func(value *settings.Configuration) error {
					value.FlowSelection = &settings.FlowSelection{Disabled: []string{"gate"}}
					return nil
				}); err != nil {
					t.Fatal(err)
				}
			}
			if change == "restore" {
				for _, selection := range [][]string{{"quality", "security", "blackbox"}, {"quality", "blackbox"}, {"quality", "security", "blackbox"}, {"quality", "blackbox"}} {
					t.Logf("restore batch route %v", selection)
					route := routeDecisionForGates(selection)
					d.configureRoute(&route)
					d.drive()
					d.reopen()
					d.drive()
				}
				t.Log("restored batch routes")
			} else if change != "unchanged" {
				route := routeDecisionForGates(selected)
				d.configureRoute(&route)
			}
			d.drive()
			d.reopen()
			d.drive()
			wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			waveIDs := map[string]bool{}
			for _, original := range d.run.Validation.Waves {
				if waveIDs[original.ID] {
					t.Errorf("new validation responsibility reused a retired wave identity: %s", original.ID)
				}
				waveIDs[original.ID] = true
			}
			for _, target := range wave.Targets {
				if target.Kind == "QA" {
					t.Error("configuration promoted a repaired batch obligation to FINAL QA before consumer development")
				}
			}
			if change != "empty" && change != "disabled" {
				if !slices.Equal(wave.CheckpointTasks, before.CheckpointTasks) || wave.Candidate != candidate {
					t.Error("configuration lost the original batch tasks or repaired native candidate")
				}
				gates := 0
				for _, input := range d.run.ValidationInputs {
					if input.Gate == nil {
						continue
					}
					gates++
					if input.Binding.BatchID != "command" || len(input.Gate.Batches) != 1 || input.Gate.Batches[0].ID != "command" || input.Gate.Candidate != candidate {
						t.Error("rebound ordinary gate lost its original command contribution contract")
					}
				}
				want := 1
				if change == "add" {
					want = 2
				}
				if gates != want {
					t.Errorf("ordinary gate selection = %d, want %d", gates, want)
				}
				if t.Failed() {
					return
				}
				passDevelopmentBatchGates(d)
			}
			_, consumer := planBatchAction(d, "consumer")
			body, err := os.ReadFile(filepath.Join(consumer.Workspace.Path, "app.go"))
			if err != nil || string(body) != "package main\nfunc main() { println(\"corrected\") }\n" {
				t.Fatalf("dependent development did not receive the repaired command: %q, %v", body, err)
			}
			history := len(driverState(t, d).AppliedConfigurations)
			d.reopen()
			d.drive()
			d.drive()
			if len(driverState(t, d).AppliedConfigurations) != history {
				t.Fatal("reopening a stable configuration reapplied its history")
			}
			if change == "restore" {
				t.Log("complete consumer after restored batch gates")
				id, input := planBatchAction(d, "consumer")
				if err := os.WriteFile(filepath.Join(input.Workspace.Path, "consumer.go"), []byte("package main\nvar consumerReady = true\n"), 0600); err != nil {
					t.Fatal(err)
				}
				d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: batchEvidence(input)})
				d.drive()
				t.Log("consumer complete; restore final route")
				for _, selection := range [][]string{{"quality", "security", "blackbox"}, {"quality", "blackbox"}} {
					t.Logf("restore final route %v", selection)
					route := routeDecisionForGates(selection)
					d.configureRoute(&route)
					d.drive()
					d.reopen()
					d.drive()
				}
				t.Log("restored final routes")
				gate, gateInput := d.gateAction("quality")
				if gateInput.Binding.BatchID != "" || len(gateInput.Gate.Batches) != 0 {
					t.Fatal("restoring the final route changed its whole-project gate into a batch gate")
				}
				final := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
				for _, target := range final.Targets {
					if target.Kind == "QA" && len(target.RequiredCases) == 0 && final.Scope == "AFFECTED" {
						d.recordExecutionScope(target.Mode, "normal", "invalid")
						d.drive()
					}
				}
				qa, qaInput := d.action("engine.qa.execution.v4")
				d.business(gate, protocol.ValidationResult{Binding: gateInput.Binding, Status: "PASS"})
				d.business(qa, mainlineResult(qaInput))
				d.drive()
				d.reopen()
				d.drive()
				if !driverState(t, d).FinalValidationComplete(d.f.definition) {
					t.Fatal("restored batch and final routes did not complete their original QA and gate duties")
				}
			}
		})
	}
}

func repairedDevelopmentGateDriver(t *testing.T, selected []string) *validationDriver {
	t.Helper()
	p := commandPlan()
	consumer := commandPlan().Batches[0]
	consumer.ID, consumer.Artifacts = "consumer", []string{"consumer.go"}
	consumer.Subtasks = []protocol.DevelopmentSubtask{{ID: "consume", Description: "Consume the corrected command contract"}}
	consumer.DependsOn = []string{"command"}
	p.Batches = append(p.Batches, consumer)
	d := nativeMasterDriver(t)
	passPreDevelopmentReviews(t, d, p)
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "A corrected command feeds its dependent delivery"}})
	d.confirmRoute(routeDecisionForGates(selected))
	d.drive()
	design, input := d.action("engine.qa.design.v6")
	manifest := simpleManifest(input)
	d.business(design, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest})
	d.drive()
	reviewer, input := d.action("engine.qa.review.rationale.v2")
	reviewResult := approvedReview(manifest)
	d.business(reviewer, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &reviewResult})
	d.drive()
	id, input := planBatchAction(d, "command")
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main\nfunc main() { println(\"incorrect\") }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: batchEvidence(input)})
	d.drive()
	for id, input := range d.run.ValidationInputs {
		if input.Gate != nil && input.Gate.Gate != "quality" {
			d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
		}
	}
	gate, input := d.gateAction("quality")
	d.business(gate, protocol.ValidationResult{Binding: input.Binding, Status: "FAIL", Findings: []review.Finding{{Severity: "P1", SeverityRationale: "The supported command emits the wrong required output", Requirement: "AC-001", NormalPath: "app run valid", ActualError: "incorrect output", DeferralImpact: "command contract unusable", Locations: []string{"app.go:2"}, Message: "The public command emits incorrect output"}}})
	d.drive()
	wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
	var finding string
	for id, item := range d.run.Validation.Findings {
		if item.WaveID == wave.ID {
			finding = id
		}
	}
	resolution := protocol.FindingResolution{FindingID: finding, Decision: "NEW", Requirement: "AC-001", NormalPath: "app run valid", Module: "command", RootCause: "wrong output literal", Consequence: "required output absent", Severity: "P1", SeverityRationale: "supported command result unusable", Verification: review.FindingVerification{Scope: "confirmed command contract", Severity: "public result unusable", Causality: "wrong literal", Reproduction: "app run valid", Classification: "implementation"}, Reason: "Observed the command's incorrect output"}
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
	d.drive()
	repair, input := d.action("engine.repair.v7")
	if err := os.WriteFile(filepath.Join(input.Workspace.Path, "app.go"), []byte("package main\nfunc main() { println(\"corrected\") }\n"), 0600); err != nil {
		t.Fatal(err)
	}
	d.business(repair, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "Correct the supported command output"}})
	d.drive()
	return d
}
