package facade

import (
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/protocol"
)

func TestRecoveryBudgetRetryPreservesOriginalEffectAndReplay(t *testing.T) {
	for _, cap := range []bool{false, true} {
		t.Run(map[bool]string{false: "finite", true: "finite-and-token-cap"}[cap], func(t *testing.T) {
			root := t.TempDir()
			if cap {
				writeTokenCap(t, root, `{"engineDispatchMaxTotalTokens":0}`)
			}
			d := startValidationDriverAt(t, root, DefaultProvider, nil)
			var id string
			for action := range d.run.PendingActions {
				id = action
			}
			if id == "" {
				t.Fatal("missing original product review")
			}
			d.receipt(id)
			started, err := protocol.NewLifecycleEventEvent(d.eventID(), d.host(), id, protocol.LifecycleStart)
			if err != nil {
				t.Fatal(err)
			}
			d.submit(started)
			if cap {
				if _, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, protocol.Observation{}); err != nil {
					t.Fatal(err)
				}
				d.run, err = d.f.Show()
				if err != nil {
					t.Fatal(err)
				}
				blocked, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				for _, intent := range blocked.State.PendingHostActions {
					if intent.Operation == protocol.HostActionResumeAgent {
						t.Fatalf("budget boundary signed a resume: %+v", intent)
					}
				}
				initialAsk := budgetAsk(d)
				for _, option := range initialAsk.Options {
					if option.ID == "retry" {
						t.Fatal("token-only boundary offers ineffective retry")
					}
				}
				if err := submitDecide(d, budgetAsk(d), "continue", "one initial bounded continuation"); err != nil {
					t.Fatal(err)
				}
			}
			if err := resumeAndComplete(d, id); err != nil {
				t.Fatal(err)
			}
			plan, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "USER_ACTION_REQUIRED", InterruptionReason: "invalid native result"}, protocol.Observation{})
			if err != nil {
				t.Fatal(err)
			}
			token, err := d.f.Freshness(plan.RequestID)
			if err != nil {
				t.Fatal(err)
			}
			event, err := protocol.NewDecideEvent(d.eventID(), protocol.RequestID(plan.RequestID), token, "resume")
			if err != nil {
				t.Fatal(err)
			}
			d.submit(event)
			ask := budgetAsk(d)
			for _, option := range ask.Options {
				if option.ID == "continue" {
					t.Fatal("finite boundary offers ineffective continue")
				}
			}
			before, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if err := submitDecide(d, ask, "continue", "current Ready only"); err == nil {
				t.Fatal("finite boundary accepted ineffective continue")
			}
			after, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, after) {
				t.Fatal("rejected continue changed original state")
			}
			d.reopen()
			ask = budgetAsk(d)
			retry := protocol.Event{ID: d.eventID(), Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "retry", Validation: &protocol.ValidationDecision{Reason: "one retry slot", Digest: ask.Validation.Digest, StateRevision: d.run.Revision}}}
			accepted := d.submit(retry)
			if repeated := d.submit(retry); !reflect.DeepEqual(accepted, repeated) {
				t.Fatal("retry decision replay changed acceptance")
			}
			if cap {
				ask = budgetAsk(d)
				for _, option := range ask.Options {
					if option.ID == "retry" {
						t.Fatal("token boundary offers another retry extension")
					}
				}
				if err := submitDecide(d, ask, "continue", "one token cap continuation"); err != nil {
					t.Fatal(err)
				}
			}
			snapshot, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			var resume protocol.HostActionIntent
			for _, intent := range snapshot.State.PendingHostActions {
				if intent.Operation == protocol.HostActionResumeAgent {
					if resume.ActionID != "" {
						t.Fatal("more than one resume signed")
					}
					resume = intent
				}
			}
			if resume.Resume == nil || resume.Resume.WorkerActionID != id || resume.Resume.Identity != id || resume.Correlation != plan.RequestID {
				t.Fatalf("original resume binding lost: %+v", resume)
			}
			originalTask := snapshot.State.PendingActions[id].Task
			attempt := snapshot.State.Attempts[originalTask]
			if attempt.Attempts != 1 || attempt.MaxAttempts != 3 {
				t.Fatalf("retry changed execution facts: %+v", attempt)
			}
			d.reopen()
			d.drive()
			replayed, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(replayed.State.PendingHostActions, snapshot.State.PendingHostActions) {
				t.Fatal("Drive/reopen repeated the signed effect")
			}
			receipt := d.resumeReceipt(resume)
			first := d.submit(receipt)
			revision := d.run.Revision
			if repeated := d.submit(receipt); !reflect.DeepEqual(first, repeated) || d.run.Revision != revision {
				t.Fatal("resume receipt replay changed counting")
			}
			done, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if final := done.State.Attempts[originalTask]; final.Attempts != 2 || final.MaxAttempts != 3 {
				t.Fatalf("resume not counted once: %+v", final)
			}
			if raw, _ := json.Marshal(done.State.PendingHostActions); string(raw) != "{}" {
				t.Fatalf("completed resume retained active intent: %s", raw)
			}
			if _, _, err := d.f.engine.RecoverAttempt(id, protocol.Interruption{Class: "TRANSIENT_ENGINE_ERROR"}, protocol.Observation{}); err != nil {
				t.Fatal(err)
			}
			d.run, err = d.f.Show()
			if err != nil {
				t.Fatal(err)
			}
			blocked, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			for _, intent := range blocked.State.PendingHostActions {
				if intent.Operation == protocol.HostActionResumeAgent {
					t.Fatalf("budget boundary signed a resume: %+v", intent)
				}
			}
			for _, option := range budgetAsk(d).Options {
				if option.ID == "continue" || option.ID == "retry" {
					t.Fatal("spent finite extension offers another effect")
				}
			}
		})
	}
}
