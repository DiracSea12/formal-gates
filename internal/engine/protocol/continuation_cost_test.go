package protocol

import (
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/runtime"
)

func TestContinuationCostUsesOriginalCumulativeSourceAndAdmission(t *testing.T) {
	e, s, pending := unfinishedDevelopmentState(t, "engine.development.batch.v5")
	probe := &costSourceProbe{state: CostSourceReady}
	e.cfg.CostSource = continuationCostResolver(t, s, pending, probe)
	partial := WorkerResultPayload{ActionID: pending.ActionID, Provider: s.RunProvider, Outcome: OutcomePass, PayloadDigest: "actual-first", Validation: &ValidationResult{Binding: s.DispatchBindings[pending.ActionID], Status: "CONTINUE", Development: &DevelopmentEvidence{Paths: []string{}, Summary: "Read the actual contract", RemainingWork: "Deliver app.txt"}}}
	vectors := []cost.Usage{{InputCacheMissTokens: 19655, OutputTokens: 119}, {InputCacheMissTokens: 40544, OutputTokens: 238}, {InputCacheMissTokens: 61532, OutputTokens: 450}}
	admit := func(payload *WorkerResultPayload, event string, want string) {
		t.Helper()
		accepted, _, err := e.admitWorkerResultRecord(s, pending.ActionID, payload, event, "", 1, 2, Observation{})
		if err != nil || accepted.Status != want {
			t.Fatalf("%s: %#v %v", event, accepted, err)
		}
	}
	originalReceipt := s.SpawnReceipts[pending.ActionID]
	for index, usage := range vectors {
		probe.usage = usage
		if index > 0 {
			partial.SourceResult = s.Results[pending.ActionID].Digest
			partial.PayloadDigest = map[int]string{1: "actual-second", 2: "actual-final"}[index]
			body, progress := *partial.Validation, *partial.Validation.Development
			progress.Summary = "Delivered current behavior"
			if index == 2 {
				body.Status = "PASS"
				progress.Paths = []string{"app.txt"}
				progress.RemainingWork = ""
				progress.Completion = &DevelopmentCompletion{DoD: map[string]string{}, Handoff: "Ready for original approved checks"}
			}
			body.Development = &progress
			partial.Validation = &body
		}
		admit(&partial, partial.PayloadDigest, "ACCEPTED")
		entry := s.Cost.Dispatches[pending.ActionID]
		if len(s.Cost.Dispatches) != 1 || entry.TotalInputTokens != usage.InputCacheMissTokens || entry.OutputTokens != usage.OutputTokens || s.Cost.TotalInputTokens != usage.InputCacheMissTokens || s.Cost.OutputTokens != usage.OutputTokens || !reflect.DeepEqual(s.SpawnReceipts[pending.ActionID], originalReceipt) {
			t.Fatalf("turn %d did not replace original cumulative entry: %#v", index, s.Cost)
		}
		resolvedCount := probe.called
		admit(&partial, "repeat:"+partial.PayloadDigest, "DUPLICATE")
		if probe.called != resolvedCount {
			t.Fatal("duplicate completion re-resolved cumulative usage")
		}
		if index == 0 {
			data, err := json.Marshal(s)
			if err != nil {
				t.Fatal(err)
			}
			var reopened State
			if err := json.Unmarshal(data, &reopened); err != nil {
				t.Fatal(err)
			}
			s = &reopened
		}
	}
	if probe.called != 3 || s.Results[pending.ActionID].Validation.Status != "PASS" || s.Attempts[pending.Task].Attempts != 1 {
		t.Fatal("continuation changed source resolution, completion or retry policy")
	}

	stale := partial
	stale.PayloadDigest = "actual-stale"
	stale.SourceResult = "not-current"
	if _, _, err := e.admitWorkerResultRecord(s, pending.ActionID, &stale, "stale", "", 2, 3, Observation{}); err == nil || probe.called != 3 {
		t.Fatal("invalid source chain changed metering")
	}
	cap := int64(61982)
	s.TokenCapMaxTokens = &cap
	if s.dispatchTokenTotal() != 61982 || s.tokenCapStatus() != TokenCapExceeded || !e.tokenCapExceeded(s) {
		t.Fatal("guard did not see exact cumulative native consumption")
	}
}

func TestResumedDispatchCostUsesExistingUnfinishedExecutionOwner(t *testing.T) {
	for _, location := range []string{"current", "ended-awaiting-result"} {
		t.Run(location, func(t *testing.T) {
			e, s, pending := unfinishedDevelopmentState(t, "engine.development.batch.v5")
			attempt := s.Attempts[pending.Task]
			attempt.Attempts = 0
			s.Attempts[pending.Task] = attempt
			probe := &costSourceProbe{state: CostSourceReady, usage: cost.Usage{InputCacheMissTokens: 10, OutputTokens: 2}}
			e.cfg.CostSource = continuationCostResolver(t, s, pending, probe)
			e.dispatchCostHeld(s, pending.ActionID)
			runtimeResult := WorkerResult{ActionID: pending.ActionID, Outcome: OutcomeRuntimeError, FailureClass: authoring.FailureUserActionRequired, PayloadDigest: "original-inability"}
			if _, err := e.completeResult(s, runtimeResult, 2, Observation{}); err != nil {
				t.Fatal(err)
			}
			if len(s.RecoverableResults[pending.ActionID]) != 1 {
				t.Fatal("runtime/inability did not retain original recovery evidence")
			}
			if _, seen := s.Results[pending.ActionID]; seen {
				t.Fatal("inability fixture invented a CONTINUE result")
			}
			if location == "ended-awaiting-result" {
				attempt := s.Attempts[pending.Task]
				attempt.AwaitingResult = true
				s.EndedAttempts = map[runtime.TaskKey]Attempt{pending.Task: attempt}
				s.removeTaskBookkeeping(pending.Task)
				s.Tasks[pending.Task] = runtime.TaskTerminal
			}
			intent, err := e.agentHostIntent(s, HostActionResumeAgent, pending.ActionID, s.SpawnReceipts[pending.ActionID].agentIdentity(), "", "", 3)
			if err != nil || intent.Resume == nil || intent.Resume.WorkerActionID != pending.ActionID {
				t.Fatalf("original signed resume fixture: %#v %v", intent, err)
			}
			s.PendingHostActions[intent.ActionID] = intent
			s.accountResumeExecution(pending.ActionID)
			probe.usage = cost.Usage{InputCacheMissTokens: 25, OutputTokens: 7}
			e.dispatchCostHeld(s, pending.ActionID)
			if len(s.Cost.Dispatches) != 1 || s.Cost.TotalInputTokens != 25 || s.Cost.OutputTokens != 7 {
				t.Fatal("signed original resume remained frozen at first native consumption")
			}
			delete(s.PendingActions, pending.ActionID)
			delete(s.EndedAttempts, pending.Task)
			probe.usage = cost.Usage{InputCacheMissTokens: 50, OutputTokens: 20}
			e.dispatchCostHeld(s, pending.ActionID)
			if s.Cost.TotalInputTokens != 25 || s.Cost.OutputTokens != 7 {
				t.Fatal("completed original dispatch did not freeze")
			}
		})
	}
}

func TestContinuationUnavailableKeepsGuardKnownLowerBound(t *testing.T) {
	e, s, pending := unfinishedDevelopmentState(t, "engine.development.batch.v5")
	probe := &costSourceProbe{state: CostSourceReady, usage: cost.Usage{InputCacheHitTokens: 10, InputCacheMissTokens: 20, OutputTokens: 5}}
	e.cfg.CostSource = continuationCostResolver(t, s, pending, probe)
	cap := int64(35)
	s.TokenCapMaxTokens = &cap
	e.dispatchCostHeld(s, pending.ActionID)
	probe.state = CostSourceUnavailable
	probe.reason = "actual native continuation has no complete usage"
	e.dispatchCostHeld(s, pending.ActionID)
	entry := s.Cost.Dispatches[pending.ActionID]
	if entry.Source != cost.SourceUnavailable || entry.Reason != probe.reason || entry.InputCacheHitTokens != 10 || entry.InputCacheMissTokens != 20 || entry.OutputTokens != 5 || s.tokenCapStatus() != TokenCapUnavailable || s.dispatchTokenTotal() != 35 || !e.tokenCapExceeded(s) {
		t.Fatalf("missing source erased actual cost or fabricated reliable capacity: %#v", s.Cost)
	}
	data, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var reopened State
	if err := json.Unmarshal(data, &reopened); err != nil {
		t.Fatal(err)
	}
	if reopened.dispatchTokenTotal() != 35 || reopened.tokenCapStatus() != TokenCapUnavailable || !e.tokenCapExceeded(&reopened) {
		t.Fatal("recovery changed measured lower bound/source qualification")
	}
}

func TestResumedUnavailableDispatchRecordsKnownNativeUsage(t *testing.T) {
	e, s, pending := unfinishedDevelopmentState(t, "engine.development.batch.v5")
	probe := &costSourceProbe{state: CostSourceUnavailable, reason: "original invocation has no provider usage"}
	e.cfg.CostSource = continuationCostResolver(t, s, pending, probe)
	e.dispatchCostHeld(s, pending.ActionID)
	probe.usage = cost.Usage{InputCacheHitTokens: 15104, InputCacheMissTokens: 20726, OutputTokens: 10780}
	for range 2 {
		e.dispatchCostHeld(s, pending.ActionID)
	}
	entry := s.Cost.Dispatches[pending.ActionID]
	if entry.Source != cost.SourceUnavailable || entry.Reason != probe.reason || entry.TotalInputTokens != 35830 || entry.InputCacheHitTokens != 15104 || entry.InputCacheMissTokens != 20726 || entry.OutputTokens != 10780 || s.Cost.TotalInputTokens != 35830 || s.Cost.OutputTokens != 10780 {
		t.Fatalf("resumed measured usage was lost, duplicated or marked complete: %#v", s.Cost)
	}
	delete(s.PendingActions, pending.ActionID)
	probe.usage = cost.Usage{InputCacheHitTokens: 20000, InputCacheMissTokens: 40000, OutputTokens: 16000}
	e.dispatchCostHeld(s, pending.ActionID)
	if s.Cost.Dispatches[pending.ActionID] != entry || s.Cost.TotalInputTokens != 35830 || s.Cost.OutputTokens != 10780 {
		t.Fatal("a completed dispatch changed its frozen accounting")
	}
}

func continuationCostResolver(t *testing.T, s *State, pending PendingAction, probe *costSourceProbe) CostSourceResolver {
	t.Helper()
	expected := CostSourceRef{RunID: s.RunID, ActionID: pending.ActionID, Provider: s.RunProvider, Correlation: s.SpawnReceipts[pending.ActionID].Correlation}
	return func(ref CostSourceRef) ResolvedCostSource {
		if ref != expected {
			t.Fatalf("native source crossed original identity: %#v != %#v", ref, expected)
		}
		probe.called++
		return ResolvedCostSource{State: probe.state, Usage: probe.usage, Reason: probe.reason}
	}
}
