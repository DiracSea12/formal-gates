package protocol

import (
	"reflect"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/decision"
)

func startedCostDispatch(t *testing.T, probe *costSourceProbe) (*Engine, *State, uint64, PendingAction) {
	t.Helper()
	e := costEngineFixture(t, nil)
	plan, _, err := e.Plan(Observation{})
	if err != nil {
		t.Fatal(err)
	}
	issued, _, err := e.IssueFromPlan(plan, decision.Admission{Capacity: 1}, Observation{})
	if err != nil || len(issued) != 1 {
		t.Fatalf("issue: %+v %v", issued, err)
	}
	if _, err := e.Submit(spawnReceipt(t, "spawn", issued[0].ActionID, "original-worker"), Observation{}); err != nil {
		t.Fatal(err)
	}
	revision, state, err := e.load()
	if err != nil {
		t.Fatal(err)
	}
	pending := state.PendingActions[issued[0].ActionID]
	e.cfg.CostSource = continuationCostResolver(t, state, pending, probe)
	return e, state, revision, pending
}

func TestRetiredDispatchCostSettlesOriginalSourceOnce(t *testing.T) {
	for _, scenario := range []struct {
		name, source string
		known, final cost.Usage
		want         cost.Usage
	}{
		{"ready", CostSourceReady, cost.Usage{}, cost.Usage{InputCacheHitTokens: 1, InputCacheMissTokens: 5, OutputTokens: 2}, cost.Usage{InputCacheHitTokens: 1, InputCacheMissTokens: 5, OutputTokens: 2}},
		{"continuation-final", CostSourceReady, cost.Usage{InputCacheMissTokens: 10, OutputTokens: 2}, cost.Usage{InputCacheMissTokens: 25, OutputTokens: 7}, cost.Usage{InputCacheMissTokens: 25, OutputTokens: 7}},
		{"incomplete-lower-bound", CostSourceUnavailable, cost.Usage{InputCacheMissTokens: 10, OutputTokens: 2}, cost.Usage{InputCacheMissTokens: 5}, cost.Usage{InputCacheMissTokens: 10, OutputTokens: 2}},
		{"unavailable-known-usage", CostSourceUnavailable, cost.Usage{}, cost.Usage{InputCacheMissTokens: 5, OutputTokens: 2}, cost.Usage{InputCacheMissTokens: 5, OutputTokens: 2}},
		{"pending-known-usage", CostSourcePending, cost.Usage{InputCacheMissTokens: 10, OutputTokens: 2}, cost.Usage{}, cost.Usage{InputCacheMissTokens: 10, OutputTokens: 2}},
		{"pending-unmeasured", CostSourcePending, cost.Usage{}, cost.Usage{}, cost.Usage{}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			probe := &costSourceProbe{state: CostSourceReady, usage: scenario.known}
			e, state, revision, pending := startedCostDispatch(t, probe)
			if scenario.known != (cost.Usage{}) {
				e.dispatchCostHeld(state, pending.ActionID)
			}
			// Superseded work can be marked obsolete before its native execution stops.
			state.ObsoleteActions[pending.ActionID] = ObsoleteAction{ActionID: pending.ActionID, Task: pending.Task, AttemptID: pending.AttemptID}
			var err error
			revision, err = e.commit(state, revision, Observation{})
			if err != nil {
				t.Fatal(err)
			}
			probe.state, probe.usage, probe.reason = scenario.source, scenario.final, "original invocation has no complete source"
			called := probe.called
			if _, retired := state.retireAction(pending.ActionID, "", "user cancelled unfinished work"); !retired {
				t.Fatal("original execution did not retire")
			}
			if err := state.terminalizeTask(pending.Task); err != nil {
				t.Fatal(err)
			}
			if _, err = e.commit(state, revision, Observation{}); err != nil {
				t.Fatal(err)
			}
			revision, state, err = e.load()
			if err != nil {
				t.Fatal(err)
			}
			// The original source is sampled once and re-read before and after Save.
			if probe.called != called+3 || state.dispatchTokenTotal() != scenario.want.InputCacheHitTokens+scenario.want.InputCacheMissTokens+scenario.want.OutputTokens {
				t.Fatalf("final source call/total: %d %+v", probe.called-called, state.Cost)
			}
			if scenario.source == CostSourcePending && scenario.known == (cost.Usage{}) {
				if state.Cost != nil {
					t.Fatal("pending retirement fabricated measured cost")
				}
			} else {
				entry := state.Cost.Dispatches[pending.ActionID]
				if entry.Target != pending.Task.String() || entry.InputCacheHitTokens != scenario.want.InputCacheHitTokens || entry.InputCacheMissTokens != scenario.want.InputCacheMissTokens || entry.OutputTokens != scenario.want.OutputTokens {
					t.Fatalf("original identity/usage: %+v", entry)
				}
				if scenario.source == CostSourceUnavailable && (entry.Source != cost.SourceUnavailable || entry.Reason != probe.reason) {
					t.Fatalf("incomplete original source: %+v", entry)
				}
			}
			before, err := cloneState(state)
			if err != nil {
				t.Fatal(err)
			}
			called = probe.called
			probe.state, probe.usage = CostSourceReady, cost.Usage{InputCacheMissTokens: 100}
			if _, err := e.commit(state, revision, Observation{}); err != nil {
				t.Fatal(err)
			}
			if probe.called != called || !reflect.DeepEqual(before.Cost, state.Cost) {
				t.Fatal("reopened retirement changed frozen cost")
			}
		})
	}
}

func TestRetirementWithoutSourceOrDispatchDoesNotReadStore(t *testing.T) {
	e := &Engine{}
	state := &State{}
	if err := e.recordRetiredDispatchCosts(state, 1); err != nil {
		t.Fatal(err)
	}
	e.cfg.CostSource = func(CostSourceRef) ResolvedCostSource {
		t.Fatal("no real dispatch source")
		return ResolvedCostSource{}
	}
	state.ObsoleteActions = map[string]ObsoleteAction{"unstarted": {ActionID: "unstarted"}}
	if err := e.recordRetiredDispatchCosts(state, 1); err != nil {
		t.Fatal(err)
	}
	state.SpawnReceipts = map[string]SpawnReceipt{"unstarted": {ActionID: "unstarted", Status: SpawnStatusUnknown}}
	if err := e.recordRetiredDispatchCosts(state, 1); err != nil {
		t.Fatal(err)
	}
}

func TestFreshExecutionUsesFinalOriginalCostBeforeBudget(t *testing.T) {
	for _, handoff := range []bool{false, true} {
		t.Run(map[bool]string{false: "fresh", true: "handoff"}[handoff], func(t *testing.T) {
			e, state, pending := unfinishedDevelopmentState(t, "engine.development.batch.v5")
			state.Topology = nil
			state.PendingAsks = map[string]PendingAsk{}
			original := state.Attempts[pending.Task]
			original.MaxAttempts = 3
			state.Attempts[pending.Task] = original
			probe := &costSourceProbe{state: CostSourceReady, usage: cost.Usage{InputCacheMissTokens: 10, OutputTokens: 2}}
			e.cfg.CostSource = continuationCostResolver(t, state, pending, probe)
			e.dispatchCostHeld(state, pending.ActionID)
			state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{ActionID: pending.ActionID, AttemptID: pending.AttemptID, Task: pending.Task, Action: RecoveryAsk, RequestID: "recover", Handoff: handoff})
			state.LifecycleEvents = append(state.LifecycleEvents, LifecycleEventRecord{Provider: state.RunProvider, Identity: state.SpawnReceipts[pending.ActionID].agentIdentity(), Event: LifecycleStop})
			cap := int64(20)
			state.TokenCapMaxTokens = &cap
			probe.usage = cost.Usage{InputCacheMissTokens: 25, OutputTokens: 7}
			attempt := state.Attempts[pending.Task]
			if err := e.applyAttemptRecovery(state, pending, attempt, RecoveryNewAttempt, "recover", 3); err != nil {
				t.Fatal(err)
			}
			if state.dispatchTokenTotal() != 32 || !state.hasPendingBudgetAsk() || !reflect.DeepEqual(state.Attempts[pending.Task], attempt) || len(state.PendingActions) != 1 || probe.called != 2 {
				t.Fatalf("fresh cost did not constrain original guard: %+v", state.Cost)
			}
		})
	}
}

func TestHandoffRetainsOriginalCostAndMetersReplacementSeparately(t *testing.T) {
	e, state, pending := unfinishedDevelopmentState(t, "engine.development.batch.v5")
	state.Topology = nil
	state.PendingAsks = map[string]PendingAsk{}
	probe := &costSourceProbe{state: CostSourceReady, usage: cost.Usage{InputCacheMissTokens: 25, OutputTokens: 7}}
	e.cfg.CostSource = continuationCostResolver(t, state, pending, probe)
	state.RecoveryRecords = append(state.RecoveryRecords, RecoveryRecord{ActionID: pending.ActionID, AttemptID: pending.AttemptID, Task: pending.Task, Action: RecoveryAsk, RequestID: "handoff", Handoff: true})
	state.LifecycleEvents = append(state.LifecycleEvents, LifecycleEventRecord{Provider: state.RunProvider, Identity: state.SpawnReceipts[pending.ActionID].agentIdentity(), Event: LifecycleStop})
	attempt := state.Attempts[pending.Task]
	if err := e.applyAttemptRecovery(state, pending, attempt, RecoveryNewAttempt, "handoff", 3); err != nil {
		t.Fatal(err)
	}
	replacement := state.Attempts[pending.Task]
	if replacement.ActionID == pending.ActionID || replacement.Attempts != attempt.Attempts || replacement.MaxAttempts != attempt.MaxAttempts || replacement.PreviousAction != pending.ActionID || !reflect.DeepEqual(replacement.Work, attempt.Work) || state.dispatchTokenTotal() != 32 {
		t.Fatalf("handoff lost original work/cost or charged retry: %+v", replacement)
	}
	state.SpawnReceipts[replacement.ActionID] = SpawnReceipt{ActionID: replacement.ActionID, Task: replacement.Task, Correlation: "replacement-worker", Status: SpawnStatusSpawned}
	expected := CostSourceRef{RunID: state.RunID, ActionID: replacement.ActionID, Provider: state.RunProvider, Correlation: "replacement-worker"}
	e.cfg.CostSource = func(ref CostSourceRef) ResolvedCostSource {
		if ref != expected {
			t.Fatalf("replacement source identity: %+v", ref)
		}
		return ResolvedCostSource{State: CostSourceReady, Usage: cost.Usage{InputCacheMissTokens: 3, OutputTokens: 1}}
	}
	e.dispatchCostHeld(state, replacement.ActionID)
	if len(state.Cost.Dispatches) != 2 || state.dispatchTokenTotal() != 36 || state.Cost.Dispatches[pending.ActionID].InputCacheMissTokens != 25 || state.Cost.Dispatches[replacement.ActionID].InputCacheMissTokens != 3 {
		t.Fatalf("handoff mixed original and replacement usage: %+v", state.Cost)
	}
}
