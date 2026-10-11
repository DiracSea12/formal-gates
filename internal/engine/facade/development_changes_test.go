package facade

import (
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/protocol"
)

func TestSameHostHandoffPreservesWorkAndDoesNotConsumeFailureBudget(t *testing.T) {
	d := newValidationDriver(t, "blackbox")
	action, _ := d.action("engine.development.batch.v2")
	d.receipt(action)
	before, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	pending := before.State.PendingActions[action]
	original := before.State.Attempts[pending.Task]
	run, err := d.f.RequestWorkerHandoff(workerNumberForAction(d.run, action), "user requests another developer for unfinished work")
	if err != nil {
		t.Fatal(err)
	}
	d.run = run
	ask := askPosition(t, d.run, protocol.ControlRecovery)
	candidate, err := d.f.PrepareControl(ControlInput{Ask: ask, Choice: "fresh", Reason: "confirm same-host transfer"})
	if err != nil {
		t.Fatal(err)
	}
	_, d.run, err = d.f.SubmitControl(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.run.AdapterIntents) == 0 {
		t.Fatal("handoff bypassed native termination")
	}
	reopen, err := Open(d.f.root, d.f.runID)
	if err != nil {
		t.Fatal(err)
	}
	d.f = reopen
	d.drive()
	d.completeWorkerTerminations()
	d.drive()
	after, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	next, exists := after.State.Attempts[pending.Task]
	if !exists || next.ActionID == action || next.PreviousAction != action || next.Attempts != original.Attempts || next.MaxAttempts != original.MaxAttempts || !reflect.DeepEqual(next.Work, original.Work) {
		t.Fatalf("handoff changed work/budget: original=%+v next=%+v", original, next)
	}
	if after.State.RunProvider != before.State.RunProvider || after.State.Guard.UsedRetries != before.State.Guard.UsedRetries || len(after.State.RejectedResults) != len(before.State.RejectedResults) {
		t.Fatal("handoff fabricated failure, host change or retry use")
	}
	for _, record := range after.State.RecoveryRecords {
		if record.ActionID == action && record.Class != "" {
			t.Fatalf("handoff fabricated failure class: %+v", record)
		}
	}
}
func TestReplanRequiresUnchangedSourceAndExactCarryRedoThenUsesOriginalConfirmation(t *testing.T) {
	d := newValidationDriver(t, "blackbox")
	before, err := d.f.Show()
	if err != nil {
		t.Fatal(err)
	}
	choices := make([]WorkChoice, len(before.RequirementWork))
	for i := range choices {
		choices[i] = WorkChoice{Disposition: protocol.RequirementWorkCarry, Reason: "existing unfinished obligation remains valid"}
	}
	candidate, err := d.f.PrepareReplan(RequirementChangeInput{Work: choices, Impact: protocol.RequirementImpact{ChangedItems: []string{"implementation plan"}, CarryReasons: []string{"confirmed product requirement remains identical"}}})
	if err != nil {
		t.Fatal(err)
	}
	pkg := candidate.Event.Request.Package
	if strings.TrimPrefix(pkg.CandidateRevision, "sha256:") != strings.TrimPrefix(before.Review.RequirementRevision, "sha256:") || pkg.RedoProductReview || pkg.RedoTopology || !pkg.RedoStartReadiness || !pkg.RedoRoute {
		t.Fatalf("replan scope=%+v", pkg)
	}
	if _, _, err := d.f.SubmitControl(candidate); err != nil {
		t.Fatal(err)
	}
	d.drive()
	confirm, err := d.f.PrepareControl(ControlInput{Ask: askPosition(t, d.run, protocol.ControlRequestRequirementChange), Choice: "proceed", Reason: "confirm unchanged requirement and plan review with exact work carry"})
	if err != nil {
		t.Fatal(err)
	}
	_, d.run, err = d.f.SubmitControl(confirm)
	if err != nil {
		t.Fatal(err)
	}
	if d.run.Review.RequirementRevision != before.Review.RequirementRevision || !reflect.DeepEqual(d.run.Review.Topology, before.Review.Topology) || d.run.Review.Actions[0].EffectiveStatus != before.Review.Actions[0].EffectiveStatus {
		t.Fatal("replan changed product/source/topology")
	}
}
