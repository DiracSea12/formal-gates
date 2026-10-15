package facade

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
)

// Gate-only routes keep their confirmed identity and execute exactly their ordinary gates.
func TestOrdinaryGateRoutesCompleteWithoutQA(t *testing.T) {
	for _, selected := range [][]string{{"quality", "security"}, {"quality"}} {
		t.Run(strings.Join(selected, ","), func(t *testing.T) {
			root := t.TempDir()
			base := snapshotRepoCommits(t, root)[0]
			d := startValidationDriverAtVCS(t, root, DefaultProvider, nil, base, nil)
			passPreDevelopmentReviews(t, d, commandPlan())
			d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one command"}})
			route := protocol.RouteDecision{Gates: []string{"blackbox", "quality", "whitebox", "security"}, Selected: selected}
			d.confirmRoute(route)
			d.reopen()
			if !d.run.Review.RouteConfirmed || !slices.Equal(d.run.Review.SelectedGates, selected) {
				t.Fatalf("confirmed route = %t %v", d.run.Review.RouteConfirmed, d.run.Review.SelectedGates)
			}
			d.drive()
			id, in := d.action("engine.development.batch.v6")
			author := filepath.Join(t.TempDir(), "author")
			if err := d.git.AddWorktree(author, driverState(t, d).InitialSnapshot); err != nil {
				t.Fatal(err)
			}
			d.git = vcs.New(author, nil)
			snapshotCommitOne(t, author, "implement public command")
			d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implement command"}})
			d.materialize()
			d.materialize()
			d.reopen()
			d.drive()
			if len(d.run.Validation.Sets) != 0 {
				t.Fatal("gate-only route created QA sets")
			}
			wave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
			if len(wave.Targets) != len(selected) {
				t.Fatalf("validation targets = %+v", wave.Targets)
			}
			for _, target := range wave.Targets {
				if target.Kind != "GATE" || !slices.Contains(selected, target.Gate) {
					t.Fatalf("unconfirmed validation target: %+v", target)
				}
			}
			for _, gate := range selected {
				id, in = d.gateAction(gate)
				if in.Gate == nil || in.Gate.Gate != gate {
					t.Fatalf("dispatch for %s has another gate identity: %+v", gate, in)
				}
				d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
				d.reopen()
			}
			d.drive()
			if d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Status != "COMPLETE" {
				t.Fatal("selected gates did not complete")
			}
			d.reopen()
			if d.run.Status != "COMPLETE" {
				t.Fatalf("gate-only run status = %s", d.run.Status)
			}
			state := driverState(t, d)
			if !state.RouteConfirmed || !slices.Equal(state.SelectedGates, selected) {
				t.Fatalf("persisted completed route = %t %v", state.RouteConfirmed, state.SelectedGates)
			}
			for id, entry := range state.Cost.Dispatches {
				if strings.Contains(entry.Target, "qa.") {
					t.Fatalf("unselected QA cost entry %s: %+v", id, entry)
				}
			}
			for id, binding := range state.DispatchBindings {
				if strings.Contains(string(binding.Task.Step), "qa.") {
					t.Fatalf("unselected QA dispatch %s: %+v", id, binding)
				}
			}
			before := d.run.Revision
			d.drive()
			d.reopen()
			if d.run.Revision != before || len(d.run.ValidationInputs) != 0 || len(d.run.AdapterIntents) != 0 {
				t.Fatal("completed route generated more work")
			}
		})
	}
}

func TestGateSelectionRequiresConfirmedDevelopmentPlan(t *testing.T) {
	d := startValidationDriver(t, nil)
	passPreDevelopmentReviews(t, d, commandPlan())
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one command"}})
	route := protocol.RouteDecision{Gates: []string{"blackbox", "whitebox", "quality"}, Selected: []string{"quality"}}
	if err := controlError(t, d, protocol.ControlSelectRoute, "approve", protocol.ReviewDecisionPayload{Route: &route}); err == nil || !strings.Contains(err.Error(), "plan") {
		t.Fatalf("unconfirmed development plan = %v", err)
	}
	d.reopen()
	state := driverState(t, d)
	if state.RouteConfirmed || len(state.SelectedGates) != 0 {
		t.Fatalf("unconfirmed plan bound gate selection: %t %v", state.RouteConfirmed, state.SelectedGates)
	}
}
