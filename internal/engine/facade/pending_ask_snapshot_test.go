package facade

import (
	"encoding/json"
	"fmt"
	"reflect"
	"syscall"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
)

// Pending Ask content and token belong to one snapshot across an intervening
// receipt; accepted native retry authorization extends its allowance once.
func TestPendingAskProjectionKeepsOneSnapshot(t *testing.T) {
	root := t.TempDir()
	identities := snapshotRepoCommits(t, root, "implementation", "validated output")
	runner := &transientOrganizationRunner{inner: vcs.ExecRunner{}, base: identities[0]}
	d := completePromotionPipeline(t, runner, root, identities[0])
	var correlation string
	failure := func() {
		t.Helper()
		if _, err := d.f.PrepareCandidate("deliver within approved allowance"); err == nil {
			t.Fatal("declared transport failure was not reached")
		}
		snapshot, err := d.f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		intent, exists := snapshot.State.PromotionIntent()
		if !exists {
			t.Fatal("failed execution has no signed responsibility")
		}
		if correlation == "" {
			correlation = intent.Correlation
		} else if intent.Correlation != correlation {
			t.Fatal("same native responsibility changed its allowance identity")
		}
		event, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), intent.ActionID, "op.candidate", d.host(), intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusFailed, authoring.FailureTransientEngine, map[string]any{})
		if err != nil {
			t.Fatal(err)
		}
		var projectionBefore protocol.Snapshot
		var faceBefore PendingAskFace
		if runner.calls == 2 {
			projectionBefore, err = d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			for _, face := range d.f.pendingAskFaces(projectionBefore) {
				if face.Validation != nil && face.Validation.Kind == "RETRY" {
					faceBefore = face
					break
				}
			}
			if faceBefore.Token == "" {
				t.Fatal("current exhausted retry snapshot has no token")
			}
		}
		accepted := d.submit(event)
		beforeReceiptReplay, err := d.f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		replayedReceipt := d.submit(event)
		afterReceiptReplay, err := d.f.engine.Load()
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(accepted, replayedReceipt) || beforeReceiptReplay.Revision != afterReceiptReplay.Revision || !reflect.DeepEqual(beforeReceiptReplay.State, afterReceiptReplay.State) {
			t.Fatal("same EventID receipt replay changed its acceptance or persisted state")
		}
		if runner.calls == 2 {
			var projected PendingAskFace
			for _, face := range d.f.pendingAskFaces(projectionBefore) {
				if face.RequestID == faceBefore.RequestID {
					projected = face
					break
				}
			}
			current, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			proof, err := json.Marshal(map[string]any{"snapshotRevision": projectionBefore.Revision, "currentRevision": current.Revision, "snapshotAsk": faceBefore, "sameSnapshotProjectedAsk": projected, "interveningInput": event, "interveningAcceptance": accepted, "nativeCalls": runner.calls})
			if err != nil {
				t.Fatal(err)
			}
			t.Logf("pending Ask snapshot proof: %s", proof)
			if projected.Token != faceBefore.Token || !reflect.DeepEqual(projected.Validation, faceBefore.Validation) {
				t.Fatal("read-only projection combined the original snapshot Ask with a token from a different commit")
			}
			stored := current.State.PendingAsks[faceBefore.RequestID]
			if accepted.Status != "DUPLICATE" || current.Revision != projectionBefore.Revision+1 || stored.Resolved || stored.Validation.StateRevision != current.Revision || stored.Validation.Digest != faceBefore.Validation.Digest {
				t.Fatal("new EventID same-value receipt lost current authorization or stable obligation identity")
			}
			fresh, err := d.f.Freshness(faceBefore.RequestID)
			if err != nil {
				t.Fatal(err)
			}
			stale := protocol.Event{ID: d.eventID(), Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(faceBefore.RequestID), Token: fresh, Choice: "approve", Validation: &protocol.ValidationDecision{Reason: "original snapshot authorization", Digest: faceBefore.Validation.Digest, StateRevision: projectionBefore.Revision}}}
			if _, _, err := d.f.Submit(stale); err == nil {
				t.Fatal("current token accepted a stale validation revision")
			}
			unchanged, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			if current.Revision != unchanged.Revision || !reflect.DeepEqual(current.State, unchanged.State) {
				t.Fatal("stale validation rejection changed persisted state")
			}
		}
	}
	failure()
	failure()
	if runner.calls != 2 {
		t.Fatalf("automatic allowance premise: calls=%d", runner.calls)
	}
	ask := d.validationAsk("RETRY")
	if ask.Validation == nil || ask.Token == "" || ask.Validation.Target == "" {
		t.Fatalf("no exact exhausted native responsibility Ask: %+v", ask)
	}
	decision := protocol.Event{ID: d.eventID(), Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{
		Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "approve",
		Validation: &protocol.ValidationDecision{Reason: "authorize one additional native execution", Digest: ask.Validation.Digest, StateRevision: d.run.Revision},
	}}
	accepted := d.submit(decision)
	beforeReplay, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	replayed := d.submit(decision)
	afterReplay, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(accepted, replayed) || beforeReplay.Revision != afterReplay.Revision || !reflect.DeepEqual(beforeReplay.State, afterReplay.State) || runner.calls != 2 {
		t.Fatal("replayed accepted authorization changed receipt, State or native execution count")
	}
	failure()
	snapshot, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	var records []protocol.HostActionReceipt
	for _, values := range snapshot.State.HostActionFailures {
		for _, value := range values {
			if value.Correlation == correlation {
				records = append(records, value)
			}
		}
	}
	if runner.calls != 3 || len(records) != 3 {
		t.Fatalf("one extra native allowance premise: calls=%d records=%+v", runner.calls, records)
	}
	if !reflect.DeepEqual(afterReplay.State.PendingAsks[ask.RequestID], snapshot.State.PendingAsks[ask.RequestID]) || !reflect.DeepEqual(afterReplay.State.Decisions[ask.RequestID], snapshot.State.Decisions[ask.RequestID]) || !reflect.DeepEqual(afterReplay.State.Events[string(decision.ID)], snapshot.State.Events[string(decision.ID)]) {
		t.Fatal("later failure rewrote the resolved Ask or accepted authorization evidence")
	}
	before, err := d.f.observePromotion()
	if err != nil {
		t.Fatal(err)
	}
	_, rejected := d.f.PrepareCandidate("deliver within approved allowance")
	after, err := d.f.observePromotion()
	if err != nil {
		t.Fatal(err)
	}
	rejection := ""
	if rejected != nil {
		rejection = rejected.Error()
	}
	// JSON is the raw evidence boundary; judgments remain the business assertions.
	proof, err := json.Marshal(map[string]any{
		"correlation": correlation, "ask": ask, "decisionInput": decision,
		"accepted": accepted, "replayed": replayed,
		"beforeReplayRevision": beforeReplay.Revision, "afterReplayRevision": afterReplay.Revision,
		"storedGrant": afterReplay.State.Decisions[ask.RequestID], "failureReceipts": records,
		"fourthCallError": rejection, "nativeCalls": runner.calls,
		"nativeBefore": before, "nativeAfter": after,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("native retry boundary evidence: %s", proof)
	if rejected == nil || runner.calls != 3 || !reflect.DeepEqual(before, after) {
		t.Fatal("one exact retry approval allowed further native execution after its additional slot was consumed")
	}
}

// The fixture fails only the exact organization command with a typed transient
// error; all other native operations execute through the real Git runner.
type transientOrganizationRunner struct {
	inner vcs.Runner
	base  string
	calls int
}

func (r *transientOrganizationRunner) Run(dir, command string, args ...string) (string, error) {
	if command == "git" && len(args) == 3 && args[0] == "reset" && args[1] == "--soft" && args[2] == r.base {
		r.calls++
		return "", fmt.Errorf("transient organization fixture: %w", syscall.EAGAIN)
	}
	return r.inner.Run(dir, command, args...)
}

func TestControlAskProjectionsKeepTheirSnapshot(t *testing.T) {
	d := newValidationDriver(t, "quality")
	pkg := protocol.RequirementDecisionPackage{CandidateRevision: "sha256:clarified-requirement", DeltaDigest: "sha256:formatting", ImpactDigest: "sha256:unchanged-behavior", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved}
	for _, work := range d.run.RequirementWork {
		pkg.WorkDecisions = append(pkg.WorkDecisions, protocol.RequirementWorkDecision{Work: work, Disposition: protocol.RequirementWorkCarry, Reason: "unchanged behavior retains this dispatch"})
	}
	request, err := protocol.NewRequirementRequestEvent(d.eventID(), pkg, protocol.AskOption{ID: "abort", Label: "Keep requirement"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(request)
	abort, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlAbort, protocol.AskOption{ID: "cancel", Label: "Continue"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(abort)
	old, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	oldFaces := d.f.pendingAskFaces(old)
	oldRequirement := d.f.pendingRequirementAskFace(old)
	if len(oldFaces) != 2 || oldRequirement == nil || oldRequirement.Token == "" {
		t.Fatalf("missing control Ask premise: %+v %+v", oldFaces, oldRequirement)
	}
	advance, err := protocol.NewRequestEvent(d.eventID(), protocol.ControlAbort, protocol.AskOption{ID: "cancel", Label: "Continue"})
	if err != nil {
		t.Fatal(err)
	}
	d.submit(advance)
	before, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if before.Revision <= old.Revision {
		t.Fatal("intervening commit did not advance revision")
	}
	if !reflect.DeepEqual(oldFaces, d.f.pendingAskFaces(old)) || !reflect.DeepEqual(oldRequirement, d.f.pendingRequirementAskFace(old)) {
		t.Fatal("control projection combined content and token from different snapshots")
	}
	stale, err := protocol.NewDecideEvent(d.eventID(), protocol.RequestID(oldRequirement.RequestID), oldRequirement.Token, "abort")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.f.Submit(stale); err == nil {
		t.Fatal("old requirement snapshot authorized a current decision")
	}
	after, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if before.Revision != after.Revision || !reflect.DeepEqual(before.State, after.State) {
		t.Fatal("read-only projection or stale decision changed state")
	}
	controlFound := false
	for _, face := range d.run.PendingAsks {
		if face.RequestID == string(abort.ID) {
			current, err := protocol.NewDecideEvent(d.eventID(), protocol.RequestID(face.RequestID), face.Token, "cancel")
			if err != nil {
				t.Fatal(err)
			}
			accepted := d.submit(current)
			if accepted.Status != "ACCEPTED" || accepted.RequestID != string(abort.ID) {
				t.Fatal("current control choice did not accept the original request identity")
			}
			controlFound = true
			break
		}
	}
	if !controlFound {
		t.Fatal("original control request has no current choice")
	}
	d.reopen()
	current := d.run.PendingRequirementAsk
	if current == nil || current.Token == oldRequirement.Token || !reflect.DeepEqual(current.Package, oldRequirement.Package) {
		t.Fatal("reopen lost requirement content or retained its old token")
	}
	decision, err := protocol.NewDecideEvent(d.eventID(), protocol.RequestID(current.RequestID), current.Token, "abort")
	if err != nil {
		t.Fatal(err)
	}
	accepted := d.submit(decision)
	resolved, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	d.reopen()
	replay := d.submit(decision)
	final, err := d.f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(accepted, replay) || resolved.Revision != final.Revision || !reflect.DeepEqual(resolved.State, final.State) {
		t.Fatal("accepted requirement replay changed its persisted decision")
	}
}
