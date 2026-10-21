package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/settings"
)

func TestPlanBindsAvailabilityObservationWithoutWritingState(t *testing.T) {
	t.Parallel()
	engine, dir, ready, _ := preparedEngine(t)
	issued, _, err := engine.IssueFromPlan(ready, decision.Admission{Capacity: 1}, Observation{})
	if err != nil || len(issued) != 1 {
		t.Fatal(issued, err)
	}
	before := engine.LoadMustSucceed(t)
	pending := before.State.PendingActions[issued[0].ActionID]
	attempt := before.State.Attempts[pending.Task]
	engine.cfg.Configuration = func(string) (settings.Configuration, error) {
		return settings.DefaultConfiguration(), nil
	}
	reads := 0
	engine.cfg.WorkerExecution = func(p PendingAction, a Attempt) (*SpawnReceiptPayload, error) {
		if p != pending || a.ID != attempt.ID {
			t.Fatal("availability sampled another worker", p, a)
		}
		reads++
		return nil, nil
	}
	var key bytes.Buffer
	enc := json.NewEncoder(&key)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(struct{ Action, Attempt string }{pending.ActionID, attempt.ID}); err != nil {
		t.Fatal(err)
	}
	expected := decision.Observation{Facts: []decision.Fact{{
		Source: decision.SourceHost,
		Key:    fmt.Sprintf("execution:sha256:%x", sha256.Sum256(key.Bytes())),
		Value:  fmt.Sprintf("sha256:%x", sha256.Sum256([]byte("null\n"))),
	}}}
	digest, err := expected.Digest()
	if err != nil {
		t.Fatal(err)
	}
	raw := stateBytesOf(t, dir)
	local := engine.observing(Observation{})
	for i := 0; i < 2; i++ {
		plan, revision, err := local.Plan(Observation{})
		if err != nil || revision != before.Revision || plan == nil || plan.ObservationDigest != digest {
			t.Fatalf("plan omitted availability facts: %+v revision=%d error=%v want=%s", plan, revision, err, digest)
		}
		if reads != 1 || !reflect.DeepEqual(local.observation.Facts, expected) || engine.observation != nil {
			t.Fatal("operation resampled, duplicated or leaked facts", reads, local.observation.Facts)
		}
		if !bytes.Equal(raw, stateBytesOf(t, dir)) {
			t.Fatal("read-only plan wrote state")
		}
	}
	if actual, err := local.observation.Fingerprint(); err != nil || actual != digest || reads != 2 {
		t.Fatal("save fingerprint did not recheck the original worker reader", actual, err, reads)
	}
}

func TestOperationObservationSharesSamplesAndRechecksOriginalReaders(t *testing.T) {
	t.Parallel()
	engine, _, _, _ := preparedEngine(t)
	refs := []CostSourceRef{{RunID: "run", ActionID: "one", Provider: testProvider, Correlation: "first"}, {RunID: "run", ActionID: "two", Provider: testProvider, Correlation: "second"}}
	costs := []ResolvedCostSource{{State: CostSourceReady, Usage: cost.Usage{OutputTokens: 3}}, {State: CostSourcePending, Reason: "second execution has no stop"}}
	calls := [2]int{}
	engine.cfg.CostSource = func(ref CostSourceRef) ResolvedCostSource {
		for i, expected := range refs {
			if ref == expected {
				calls[i]++
				return costs[i]
			}
		}
		t.Fatalf("unexpected source reference: %+v", ref)
		return ResolvedCostSource{}
	}
	pending := PendingAction{ActionID: "native", AttemptID: "attempt"}
	attempt := Attempt{ID: "attempt"}
	workers := 0
	var worker *SpawnReceiptPayload
	engine.cfg.WorkerExecution = func(p PendingAction, a Attempt) (*SpawnReceiptPayload, error) {
		if p.ActionID != pending.ActionID || a.ID != attempt.ID {
			t.Fatalf("worker source identity changed: %+v %+v", p, a)
		}
		workers++
		return worker, nil
	}
	local := engine.observing(Observation{})
	for i, ref := range refs {
		first := local.cfg.CostSource(ref)
		second := local.cfg.CostSource(ref)
		if first != costs[i] || second != first {
			t.Fatalf("source %d did not share its actual value", i)
		}
	}
	for i := 0; i < 2; i++ {
		value, err := local.cfg.WorkerExecution(pending, attempt)
		if err != nil || value != nil {
			t.Fatal(value, err)
		}
	}
	if calls != [2]int{1, 1} || workers != 1 || engine.observation != nil {
		t.Fatalf("samples leaked or repeated: cost=%v workers=%d", calls, workers)
	}
	expected, err := local.observation.Facts.Digest()
	if err != nil {
		t.Fatal(err)
	}
	observed, err := local.observation.Fingerprint()
	if err != nil || observed != expected || calls != [2]int{2, 2} || workers != 2 {
		t.Fatalf("original refs were not re-read: %s %s %v %v %d", expected, observed, err, calls, workers)
	}
	costs[0].Usage.OutputTokens = 7
	worker = &SpawnReceiptPayload{ActionID: pending.ActionID, Provider: testProvider, Correlation: "native-directory", Status: SpawnStatusUnknown}
	if got := local.cfg.CostSource(refs[0]); got.Usage.OutputTokens != 3 {
		t.Fatalf("same operation resampled: %+v", got)
	}
	if got, err := local.cfg.WorkerExecution(pending, attempt); err != nil || got != nil {
		t.Fatalf("same operation resampled worker: %+v %v", got, err)
	}
	observed, err = local.observation.Fingerprint()
	if err != nil || observed == expected || calls != [2]int{3, 3} || workers != 3 {
		t.Fatalf("changes were not observed at original sources: %s %v %v %d", observed, err, calls, workers)
	}
	state := engine.LoadMustSucceed(t).State
	// Decide uses the already-observed values without reading any source.
	beforeCalls, beforeWorkers := calls, workers
	plan, err := local.decide(state)
	if err != nil || plan.ObservationDigest != expected {
		t.Fatalf("plan did not consume the sampled facts: %+v %v", plan, err)
	}
	if calls != beforeCalls || workers != beforeWorkers {
		t.Fatal("Decide performed external I/O")
	}
}

func TestCostObservationBeforeSaveRejectsChangeWithoutStateEffects(t *testing.T) {
	t.Parallel()
	for _, changed := range []bool{false, true} {
		t.Run(map[bool]string{false: "unchanged", true: "changed"}[changed], func(t *testing.T) {
			engine, dir := newTestEngine(t)
			cd, reg := costAgentFixture(t)
			engine.cfg.Definition, engine.cfg.Registry, engine.cfg.Capacity = cd, reg, 1
			view, err := decision.NewState(cd.Version, runtime.PhaseIntakeRegistered)
			if err != nil {
				t.Fatal(err)
			}
			if err = view.CompleteStep("prep.parse", cd); err != nil {
				t.Fatal(err)
			}
			if err = engine.InitWithMetadata(view, testProvider, Observation{}, "cost-run", nil, "", "", nil); err != nil {
				t.Fatal(err)
			}
			plan, _, err := engine.Plan(Observation{})
			if err != nil {
				t.Fatal(err)
			}
			issued, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 1}, Observation{})
			if err != nil || len(issued) != 1 {
				t.Fatal(issued, err)
			}
			action := issued[0].ActionID
			result, err := NewWorkerResultEvent("result-before-receipt", action, testProvider, OutcomePass, "sha256:output", "")
			if err != nil {
				t.Fatal(err)
			}
			if acceptance, err := engine.Submit(result, Observation{}); err != nil || acceptance.Status != "STAGED" {
				t.Fatal(acceptance, err)
			}
			claimed := false
			usage := cost.Usage{OutputTokens: 5}
			reads := 0
			engine.cfg.ClaimDispatch = func(run, id, identity, provider string) error {
				if run != "cost-run" || id != action || identity != "actual-agent" || provider != testProvider {
					t.Fatal("wrong claim identity")
				}
				claimed = true
				return nil
			}
			engine.cfg.CostSource = func(ref CostSourceRef) ResolvedCostSource {
				if !claimed || ref != (CostSourceRef{RunID: "cost-run", ActionID: action, Provider: testProvider, Correlation: "actual-agent"}) {
					t.Fatalf("source read before its own claim or with wrong ref: %+v", ref)
				}
				reads++
				return ResolvedCostSource{State: CostSourceReady, Usage: usage}
			}
			before := stateBytesOf(t, dir)
			original := engine.LoadMustSucceed(t)
			store, err := persistence.NewStore(dir, persistence.Config{PackageDigest: "sha256:test-package", AcquireAdmission: func() (func(), error) {
				if changed {
					usage.OutputTokens = 8
				}
				return func() {}, nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			engine.store = store
			event := spawnReceipt(t, "receipt", action, "actual-agent")
			acceptance, err := engine.Submit(event, Observation{})
			after := engine.LoadMustSucceed(t)
			if changed {
				var drift *persistence.FingerprintChangedError
				if !errors.As(err, &drift) || drift.Committed || !reflect.DeepEqual(before, stateBytesOf(t, dir)) || after.Revision != original.Revision {
					t.Fatalf("changed source wrote effects: %+v %v", acceptance, err)
				}
				return
			}
			if err != nil || reads != 3 || after.State.Cost.Dispatches[action].OutputTokens != 5 || after.State.TaskStatusOf(issued[0].Task) != runtime.TaskTerminal {
				t.Fatalf("claim/sample/recheck/completion: %v reads=%d %+v", err, reads, after.State.Cost)
			}
			priorReads := reads
			if replay, err := engine.Submit(event, Observation{Read: func() (decision.Observation, error) {
				t.Fatal("replay read external facts")
				return decision.Observation{}, nil
			}}); err != nil || !reflect.DeepEqual(acceptance, replay) {
				t.Fatal(replay, err)
			}
			if _, _, err := engine.Drive(Observation{}); err != nil {
				t.Fatal(err)
			}
			if reads != priorReads {
				t.Fatalf("completed cost was resampled: %d -> %d", priorReads, reads)
			}
		})
	}
}

func TestWorkerExecutionChangeBeforeSaveRejectsResetWithoutStateEffects(t *testing.T) {
	t.Parallel()
	engine, dir, plan, _ := preparedEngine(t)
	issued, _, err := engine.IssueFromPlan(plan, decision.Admission{Capacity: 1}, Observation{})
	if err != nil || len(issued) != 1 {
		t.Fatal(issued, err)
	}
	action := issued[0].ActionID
	request, err := NewRequestEvent("reset-unclaimed", ControlReset, AskOption{ID: "proceed", Label: "Reset run"})
	if err != nil {
		t.Fatal(err)
	}
	request = request.WithResetPreview(engine.LoadMustSucceed(t).State.ResetPreview())
	if _, err = engine.Submit(request, Observation{}); err != nil {
		t.Fatal(err)
	}
	token, err := engine.Freshness(RequestID(request.ID))
	if err != nil {
		t.Fatal(err)
	}
	event, err := NewDecideEvent("confirm-reset-unclaimed", RequestID(request.ID), token, "proceed")
	if err != nil {
		t.Fatal(err)
	}
	state := engine.LoadMustSucceed(t).State
	event.Decide.Reset = &ResetDecision{PreviewDigest: state.PendingAsks[string(request.ID)].Reset.Digest, Reason: "Reset unfinished work"}
	target := state.PendingActions[action]
	var live *SpawnReceiptPayload
	reads := 0
	engine.cfg.WorkerExecution = func(p PendingAction, a Attempt) (*SpawnReceiptPayload, error) {
		if p != target || a.ID != target.AttemptID {
			t.Fatalf("control read a different worker owner: %+v %+v", p, a)
		}
		reads++
		return live, nil
	}
	before := stateBytesOf(t, dir)
	store, err := persistence.NewStore(dir, persistence.Config{PackageDigest: "sha256:test-package", AcquireAdmission: func() (func(), error) {
		live = &SpawnReceiptPayload{ActionID: action, Provider: testProvider, Correlation: "native-job", Status: SpawnStatusUnknown}
		return func() {}, nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	engine.store = store
	_, err = engine.Submit(event, Observation{})
	var drift *persistence.FingerprintChangedError
	if !errors.As(err, &drift) || drift.Committed || reads != 2 || !reflect.DeepEqual(before, stateBytesOf(t, dir)) {
		t.Fatalf("worker changed before control Save: err=%v reads=%d", err, reads)
	}
	if engine.observation != nil {
		t.Fatal("observation leaked into shared engine")
	}
}

func TestWorkerStoppedObservationSharesSampleWithoutChangingSourceBindings(t *testing.T) {
	engine, _, _, _ := preparedEngine(t)
	pending := PendingAction{ActionID: "native", AttemptID: "attempt"}
	attempt := Attempt{ID: "attempt"}
	stopped, reads := true, 0
	engine.cfg.WorkerExecutionStopped = func(p PendingAction, a Attempt) (bool, error) {
		if p != pending || a.ID != attempt.ID {
			t.Fatal("wrong stopped worker", p, a)
		}
		reads++
		return stopped, nil
	}
	source := decision.Observation{Facts: []decision.Fact{{Source: decision.SourceHost, Key: "binding", Value: "fixed"}}}
	local := engine.observing(Observation{Facts: source, Read: func() (decision.Observation, error) { return source, nil }})
	for i := 0; i < 2; i++ {
		value, err := local.cfg.WorkerExecutionStopped(pending, attempt)
		if err != nil || !value {
			t.Fatal(value, err)
		}
	}
	if reads != 1 || !reflect.DeepEqual(local.sourceFacts, source) || len(source.Facts) != 1 {
		t.Fatal("stop sampling changed bindings or repeated source read", reads, local.sourceFacts)
	}
	expected, err := local.observation.Facts.Digest()
	if err != nil {
		t.Fatal(err)
	}
	observed, err := local.observation.Fingerprint()
	if err != nil || observed != expected || reads != 2 {
		t.Fatal("stopped fact not rechecked", observed, expected, reads, err)
	}
	stopped = false
	value, err := local.cfg.WorkerExecutionStopped(pending, attempt)
	if err != nil || !value || reads != 2 {
		t.Fatal("operation resampled stopped fact", value, reads, err)
	}
	observed, err = local.observation.Fingerprint()
	if err != nil || observed == expected || reads != 3 {
		t.Fatal("changed source not observed", observed, reads, err)
	}
}
