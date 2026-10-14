package facade

import (
	"testing"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

func TestTransientRecoveryRetainsWorkAfterPreviousDispatchCost(t *testing.T) {
	d := nativeMasterDriver(t)
	passPreDevelopmentReviews(t, d, commandPlan(), review.ProductReview)
	var action string
	for id, pending := range d.run.PendingActions {
		if pending.ReviewSeries == review.StartReadiness {
			action = id
		}
	}
	if action == "" {
		t.Fatal("product PASS did not issue start-readiness")
	}
	d.receipt(action)
	start, err := protocol.NewLifecycleEventEvent(d.eventID(), d.host(), action, protocol.LifecycleStart)
	if err != nil {
		t.Fatal(err)
	}
	d.submit(start)
	before, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	pending := before.State.PendingActions[action]
	attempt := before.State.Attempts[pending.Task]
	if before.State.Cost == nil || len(before.State.Cost.Dispatches) == 0 || attempt.Plan.ObservationDigest == attempt.Bindings.Snapshot {
		t.Fatal("product cost was not retained in the decision observation separately from work identity")
	}
	head, err := d.git.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	plan, _, err := d.f.engine.RecoverAttempt(action, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, protocol.Observation{})
	if err != nil || plan.Action != protocol.RecoveryResumeAttempt {
		t.Fatalf("unchanged transient recovery = %+v, err=%v", plan, err)
	}
	after, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	current := after.State.Attempts[pending.Task]
	if after.State.PendingActions[action] != pending || current.ID != attempt.ID || current.Bindings != attempt.Bindings {
		t.Fatal("resume changed the original task or attempt bindings")
	}
	resumed := false
	for _, intent := range after.State.PendingHostActions {
		if intent.Correlation == plan.RequestID && intent.Resume != nil && intent.Resume.WorkerActionID == action && intent.Resume.AttemptID == attempt.ID {
			resumed = true
		}
		if intent.Correlation == plan.RequestID && intent.Terminate != nil {
			t.Fatal("unchanged source requested termination")
		}
	}
	if !resumed {
		t.Fatal("resume did not retain the original native execution")
	}
	currentHead, err := d.git.Resolve()
	if err != nil || currentHead != head {
		t.Fatalf("recovery changed the native snapshot: %s -> %s, err=%v", head, currentHead, err)
	}
}
