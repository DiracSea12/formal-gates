package facade

import (
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/settings"
)

func saveVerificationRoute(t *testing.T, d *validationDriver, gates ...string) {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	state := driverState(t, d)
	if _, err := settings.EditConfiguration(d.f.root, home, "project", state.ConfigurationName(), nil, func(value *settings.Configuration) error {
		selection := protocol.ConfigurationSelection{Configuration: *value}
		selection.SetSelectedGates(gates)
		value.Gates, value.QAModes = selection.Configuration.Gates, selection.Configuration.QAModes
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func completeVerificationDesign(t *testing.T, d *validationDriver, mode coverage.ReviewKind) {
	t.Helper()
	id, input := d.action("engine.qa.design.v6")
	if input.Manifest.Binding.ReviewKind != mode {
		t.Fatalf("design mode = %s, want %s", input.Manifest.Binding.ReviewKind, mode)
	}
	manifest := simpleManifest(input)
	result := protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &manifest}
	if mode == coverage.ReviewWhitebox {
		for i := range manifest.Cases {
			manifest.Cases[i].Mode = coverage.CaseWhitebox
			manifest.Cases[i].PublicEntry, manifest.Cases[i].Preconditions, manifest.Cases[i].Steps = "", "", ""
		}
		result.Development = &protocol.DevelopmentEvidence{Paths: []string{}}
	}
	d.business(id, result)
	d.drive()
	if mode == coverage.ReviewWhitebox {
		d.materialize()
	}
	id, input = d.action("engine.qa.review.rationale.v2")
	if input.Manifest.Binding.ReviewKind != mode {
		t.Fatalf("review mode = %s, want %s", input.Manifest.Binding.ReviewKind, mode)
	}
	approved := approvedReview(input.Manifest)
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &approved})
	d.drive()
	d.materialize()
}

func completedVerificationDevelopment(t *testing.T, gates ...string) *validationDriver {
	t.Helper()
	d := newValidationDriver(t, gates...)
	if slices.Contains(gates, "blackbox") {
		d.reviewReady()
		return d
	}
	id, input := d.action("engine.development.batch.v6")
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implemented input checks"}})
	d.materialize()
	d.materialize()
	if slices.Contains(gates, "whitebox") {
		completeVerificationDesign(t, d, coverage.ReviewWhitebox)
	}
	return d
}

func assertNoConfigurationWithdrawal(t *testing.T, state *protocol.State) {
	t.Helper()
	for _, intent := range state.PendingHostActions {
		if intent.Terminate != nil {
			t.Fatal("configuration requested worker termination", intent)
		}
	}
}

// A saved route replaces unclaimed verification without reopening completed development.
func TestSavedDisjointVerificationRouteAppliesAtDispatch(t *testing.T) {
	for _, pair := range [][2]string{{"blackbox", "whitebox"}, {"blackbox", "quality"}, {"whitebox", "blackbox"}, {"quality", "security"}} {
		t.Run(strings.Join(pair[:], "-to-"), func(t *testing.T) {
			d := completedVerificationDevelopment(t, pair[0])
			before := driverState(t, d)
			wave := before.Validation.Waves[len(before.Validation.Waves)-1]
			var old string
			var oldInput protocol.ValidationInput
			if pair[0] == "quality" {
				old, oldInput = d.gateAction(pair[0])
			} else {
				old, oldInput = d.action("engine.qa.execution.v4")
			}
			if _, claimed := before.SpawnReceipts[old]; claimed {
				t.Fatal("verification must be unclaimed")
			}
			saveVerificationRoute(t, d, pair[1])
			// Show evaluates the same obligations as dispatch, without writing history.
			shown, err := d.f.Show()
			if err != nil {
				t.Fatal(err)
			}
			wantNode := "gate"
			if pair[1] == "blackbox" || pair[1] == "whitebox" {
				wantNode = "qa-" + pair[1]
			}
			found := false
			for _, node := range shown.NodeStates {
				if node.Node == wantNode {
					found = node.Owed && node.Dispatchable && node.Editable && !node.Completed
				}
			}
			if !found {
				t.Fatalf("saved responsibility is missing from projection: %+v", shown.NodeStates)
			}
			if !reflect.DeepEqual(driverState(t, d), before) {
				t.Fatal("read-only configuration projection changed persistent facts")
			}
			d.drive()
			applied := driverState(t, d)
			entry := applied.AppliedConfigurations[len(applied.AppliedConfigurations)-1]
			if entry.Effect == nil || entry.Effect.AppliedRevision == 0 || !slices.Equal(entry.SelectedGates, []string{pair[1]}) || !slices.Equal(applied.SelectedGates, []string{pair[1]}) {
				t.Fatal("saved route was not applied on the first dispatch evaluation", entry, applied.SelectedGates)
			}
			if _, pending := applied.PendingActions[old]; pending || applied.TaskStatusOf(oldInput.Binding.Task) != runtime.TaskTerminal {
				t.Fatal("unclaimed verification remained executable")
			}
			if _, returned := applied.Results[old]; returned || applied.Validation.Waves[0].ID != wave.ID || applied.Validation.Waves[0].Status != "INVALIDATED" {
				t.Fatal("route replacement fabricated a result or overwrote the original wave")
			}
			if applied.Validation.DevelopmentSnapshot != before.Validation.DevelopmentSnapshot || applied.Validation.Generation != before.Validation.Generation || !reflect.DeepEqual(applied.RequirementRevisions, before.RequirementRevisions) {
				t.Fatal("route replacement changed completed source work or requirement")
			}
			for id, result := range before.Results {
				if !reflect.DeepEqual(applied.Results[id], result) {
					t.Fatal("completed result changed", id)
				}
			}
			assertNoConfigurationWithdrawal(t, applied)
			d.reopen()
			d.drive()
			for _, node := range d.run.NodeStates {
				if node.Node == "development" && (!node.Completed || node.Editable || node.Owed) {
					t.Fatal("completed development was reopened", node)
				}
				if pair[0] != "quality" && node.Node == "qa-"+pair[0] && (node.Owed || node.Dispatchable || node.Completed || !node.Editable) {
					t.Fatal("retired verification kept an obsolete dispatch obligation", node)
				}
			}
			if pair[1] == "whitebox" || pair[1] == "blackbox" {
				// Preparation is required before execution on the newly selected QA.
				for _, input := range d.run.ValidationInputs {
					if input.Handler == "engine.qa.execution.v4" {
						t.Fatal("new QA executed without design and review")
					}
				}
				if pair[1] == "whitebox" {
					d.materialize()
				}
				mode := coverage.ReviewBlackbox
				if pair[1] == "whitebox" {
					mode = coverage.ReviewWhitebox
				}
				completeVerificationDesign(t, d, mode)
				id, input := d.action("engine.qa.execution.v4")
				if id == old || input.Manifest.Binding.ReviewKind != mode || input.Binding.WaveID == wave.ID {
					t.Fatal("new QA reused the retired execution", input.Binding)
				}
				report := executionFor(input)
				d.business(id, nativeExecutionResult(input, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &report}))
			} else {
				for _, intent := range d.run.AdapterIntents {
					if intent.Adapter != nil && intent.Adapter.Operation == "op.candidate" && intent.Adapter.Params["stage"] == "REVIEW_READY" {
						d.materialize()
						break
					}
				}
				id, input := d.gateAction(pair[1])
				if id == old || input.Binding.WaveID == wave.ID {
					t.Fatal("new gate reused the retired execution")
				}
				d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS"})
			}
			d.drive()
			if d.run.Next.Wait != nil && d.run.Next.Wait.Reason == decision.WaitConfigurationUnavailable {
				t.Fatal("valid route stopped after its replacement duty completed")
			}
			if !driverState(t, d).FinalValidationComplete(d.f.definition) {
				t.Fatal("replacement route did not finish its real validation")
			}
		})
	}
}

func TestSavedVerificationRouteWaitsForClaimedExecution(t *testing.T) {
	d := completedVerificationDevelopment(t, "blackbox")
	old, input := d.action("engine.qa.execution.v4")
	d.receipt(old)
	before := driverState(t, d)
	choice, known := before.ActionExecutionChoice(old)
	if !known {
		t.Fatal("claimed execution has no original configuration")
	}
	pending := before.PendingActions[old]
	saveVerificationRoute(t, d, "quality")
	d.drive()
	d.reopen()
	d.drive()
	closing := driverState(t, d)
	entry := closing.AppliedConfigurations[len(closing.AppliedConfigurations)-1]
	if entry.Effect == nil || entry.Effect.AppliedRevision != 0 || !slices.Contains(entry.PendingNodes, "qa-blackbox") || !slices.Equal(closing.SelectedGates, []string{"blackbox"}) {
		t.Fatal("claimed execution was not retained on its original route", entry)
	}
	if !reflect.DeepEqual(closing.PendingActions[old], pending) || !reflect.DeepEqual(closing.Attempts[pending.Task], before.Attempts[pending.Task]) {
		t.Fatal("route change rewrote the claimed call")
	}
	if _, obsolete := closing.ObsoleteActions[old]; obsolete {
		t.Fatal("configuration withdrew the claimed execution")
	}
	actual, known := closing.ActionExecutionChoice(old)
	if !known || actual != choice {
		t.Fatal("claimed execution lost its issued configuration", actual, choice)
	}
	assertNoConfigurationWithdrawal(t, closing)
	report := executionFor(input)
	d.business(old, nativeExecutionResult(input, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &report}))
	d.drive()
	after := driverState(t, d)
	entry = after.AppliedConfigurations[len(after.AppliedConfigurations)-1]
	if entry.Effect.AppliedRevision == 0 || len(entry.PendingNodes) != 0 || !slices.Equal(after.SelectedGates, []string{"quality"}) || after.Results[old].Validation == nil || after.Results[old].Validation.Binding != input.Binding {
		t.Fatal("normal completion did not accept the old result then apply the new route")
	}
	if after.Validation.Waves[0].ID != input.Binding.WaveID {
		t.Fatal("normal completion lost the original result's wave")
	}
	assertNoConfigurationWithdrawal(t, after)
	id, gate := d.gateAction("quality")
	d.business(id, protocol.ValidationResult{Binding: gate.Binding, Status: "PASS"})
	d.drive()
	if !driverState(t, d).FinalValidationComplete(d.f.definition) {
		t.Fatal("deferred route did not complete normally")
	}
}

func TestClosedVerificationObligationsWaitAndResumeWithoutReopeningCompletedNodes(t *testing.T) {
	d := completedVerificationDevelopment(t, "blackbox", "quality")
	qa, input := d.action("engine.qa.execution.v4")
	report := executionFor(input)
	d.business(qa, nativeExecutionResult(input, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &report}))
	d.drive()
	before := driverState(t, d)
	old, gate := d.gateAction("quality")
	saveVerificationRoute(t, d)
	for i := 0; i < 2; i++ {
		d.drive()
		d.reopen()
		if d.run.Next.Wait == nil || d.run.Next.Wait.Reason != decision.WaitConfigurationUnavailable || d.run.Next.Ready != nil {
			t.Fatal("closing every remaining obligation did not stop dispatch")
		}
		for _, node := range d.run.NodeStates {
			if node.Node == "development" && (!node.Completed || node.Owed || node.Editable) {
				t.Fatal("completed responsibility was used to bypass the stop guard", node)
			}
			if node.Node == "qa-blackbox" && (node.Owed || node.Dispatchable || node.Completed || !node.Editable) {
				t.Fatal("closed verification kept a dispatch obligation", node)
			}
		}
	}
	stopped := driverState(t, d)
	if !reflect.DeepEqual(stopped.Results[qa], before.Results[qa]) || !reflect.DeepEqual(stopped.Validation.Waves, before.Validation.Waves) || !reflect.DeepEqual(stopped.PendingActions[old], before.PendingActions[old]) {
		t.Fatal("recoverable stop changed issued work, completed QA or frozen waves")
	}
	assertNoConfigurationWithdrawal(t, stopped)
	saveVerificationRoute(t, d, "blackbox", "quality")
	d.drive()
	d.reopen()
	restored := driverState(t, d)
	if d.run.Next.Wait != nil && d.run.Next.Wait.Reason == decision.WaitConfigurationUnavailable {
		t.Fatal("restoring the owed gate did not resume")
	}
	if _, pending := restored.PendingActions[old]; !pending || !reflect.DeepEqual(restored.Results[qa], before.Results[qa]) {
		t.Fatal("recovery reran completed QA or replaced unchanged gate work")
	}
	d.business(old, protocol.ValidationResult{Binding: gate.Binding, Status: "PASS"})
	d.drive()
	if !driverState(t, d).FinalValidationComplete(d.f.definition) {
		t.Fatal("restored route did not complete")
	}
}
