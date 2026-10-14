package protocol

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

func extensionDefinition(t *testing.T) (*compiler.CompiledDefinition, *compiler.Registry) {
	t.Helper()
	registry := compiler.NewRegistry()
	for _, id := range []authoring.HandlerID{"engine.message.prepare", "engine.message.publish"} {
		if err := registry.RegisterHandler(id, map[authoring.HandlerID]authoring.RunnerKind{"engine.message.prepare": authoring.RunnerEngineLocal, "engine.message.publish": authoring.RunnerDurableActivity}[id]); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []authoring.CodecID{"codec.message.in", "codec.message.out"} {
		if err := registry.RegisterCodec(id); err != nil {
			t.Fatal(err)
		}
	}
	if err := registry.RegisterReconciler("reconcile.message.publish"); err != nil {
		t.Fatal(err)
	}
	steps := []authoring.Step{}
	for _, name := range []string{"prepare", "publish"} {
		header := authoring.Header{ID: authoring.StepID(name), NodeID: "message", DefinitionVersion: "1"}
		input := authoring.IO{InputCodec: "codec.message.in", OutputCodec: "codec.message.out"}
		if name == "publish" {
			header.Dependencies = []authoring.StepID{"prepare"}
		}
		var step authoring.Step
		var err error
		if name == "prepare" {
			step, err = authoring.NewLocalStep(header, input, authoring.LocalSpec{Handler: "engine.message.prepare"})
		} else {
			step, err = authoring.NewDurableStep(header, input, authoring.DurableSpec{Handler: "engine.message.publish", Idempotency: authoring.IdempotencyDeterministicInput, Reconcile: "reconcile.message.publish", Timeout: time.Second, Retry: authoring.RetryPolicy{MaxAttempts: 1}})
		}
		if err != nil {
			t.Fatal(err)
		}
		steps = append(steps, step)
	}
	compiled, err := compiler.Compile(&compiler.Definition{Version: "1", EntryNode: "message", Steps: steps}, registry)
	if err != nil {
		t.Fatal(err)
	}
	return compiled, registry
}

func extensionHandlers() map[authoring.HandlerID]InternalStep {
	bindings := make(map[authoring.HandlerID]InternalStep)
	for _, name := range []string{"prepare", "publish"} {
		bindings[authoring.HandlerID("engine.message."+name)] = InternalStep{Runner: map[string]authoring.RunnerKind{"prepare": authoring.RunnerEngineLocal, "publish": authoring.RunnerDurableActivity}[name], Run: func(s *State, step compiler.CompiledStep) (bool, error) {
			if step.Header.ID == "publish" && !s.stepCompleted("prepare") {
				return false, errors.New("publish ran before prepare")
			}
			s.RecoveryRecords = append(s.RecoveryRecords, RecoveryRecord{Detail: string(step.Header.ID)})
			return true, nil
		}}
	}
	return bindings
}

func TestInternalStepBindingRequired(t *testing.T) {
	compiled, registry := extensionDefinition(t)
	store := mustStore(t, t.TempDir())
	for _, bindings := range []map[authoring.HandlerID]InternalStep{nil, {"engine.message.prepare": {Runner: authoring.RunnerEngineLocal}}} {
		if _, err := New(store, Config{Definition: compiled, Registry: registry, InternalSteps: bindings}); err == nil {
			t.Fatal("missing implementation admitted")
		}
	}
	bindings := extensionHandlers()
	bindings["engine.message.prepare"] = InternalStep{Runner: authoring.RunnerDurableActivity, Run: bindings["engine.message.prepare"].Run}
	if _, err := New(store, Config{Definition: compiled, Registry: registry, InternalSteps: bindings}); err == nil {
		t.Fatal("runner mismatch admitted")
	}
}

func TestInternalStepExecutionPersistsOnceAcrossRecovery(t *testing.T) {
	for _, fault := range []persistence.FaultPoint{persistence.FaultIntentBefore, persistence.FaultReplaceAfter, persistence.FaultCommitResponseLost} {
		t.Run(string(fault), func(t *testing.T) {
			compiled, registry := extensionDefinition(t)
			dir := t.TempDir()
			inject := false
			store, err := persistence.NewStore(dir, persistence.Config{PackageDigest: "sha256:test-package", FaultInjector: func(point persistence.FaultPoint) error {
				if inject && point == fault {
					inject = false
					return persistence.ErrInjectedCrash
				}
				return nil
			}})
			if err != nil {
				t.Fatal(err)
			}
			config := Config{Definition: compiled, Registry: registry, InternalSteps: extensionHandlers()}
			engine, err := New(store, config)
			if err != nil {
				t.Fatal(err)
			}
			view, err := decision.NewState("1", runtime.PhaseIntakeRegistered)
			if err != nil {
				t.Fatal(err)
			}
			if err := engine.InitWithMetadata(view, testProvider, Observation{}, "", "", nil, "", "", nil); err != nil {
				t.Fatal(err)
			}
			inject = true
			if _, _, err := engine.Drive(Observation{}); !errors.Is(err, persistence.ErrInjectedCrash) {
				t.Fatalf("fault result = %v", err)
			}
			reopenedStore := mustStore(t, dir)
			if _, err := reopenedStore.Recover(); err != nil {
				t.Fatal(err)
			}
			reopened, err := New(reopenedStore, config)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := reopened.Drive(Observation{}); err != nil {
				t.Fatal(err)
			}
			state, err := reopened.Load()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(state.State.Completed, []authoring.StepID{"prepare", "publish"}) {
				t.Fatalf("completed = %v", state.State.Completed)
			}
			records := state.State.RecoveryRecords
			if len(records) != 2 || records[0].Detail != "prepare" || records[1].Detail != "publish" {
				t.Fatalf("persisted transformation = %+v", records)
			}
			before := stateBytesOf(t, dir)
			if _, _, err := reopened.Drive(Observation{}); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(before, stateBytesOf(t, dir)) {
				t.Fatal("repeat drive wrote or ran completed steps")
			}
		})
	}
}

func TestInternalStepWaitAndErrorDoNotComplete(t *testing.T) {
	for _, wantError := range []bool{false, true} {
		t.Run(map[bool]string{false: "wait", true: "error"}[wantError], func(t *testing.T) {
			compiled, registry := extensionDefinition(t)
			bindings := extensionHandlers()
			bindings["engine.message.prepare"] = InternalStep{Runner: authoring.RunnerEngineLocal, Run: func(*State, compiler.CompiledStep) (bool, error) {
				if wantError {
					return false, errors.New("cannot prepare message")
				}
				return false, nil
			}}
			dir := t.TempDir()
			engine, err := New(mustStore(t, dir), Config{Definition: compiled, Registry: registry, InternalSteps: bindings})
			if err != nil {
				t.Fatal(err)
			}
			view, err := decision.NewState("1", runtime.PhaseIntakeRegistered)
			if err != nil {
				t.Fatal(err)
			}
			if err := engine.InitWithMetadata(view, testProvider, Observation{}, "", "", nil, "", "", nil); err != nil {
				t.Fatal(err)
			}
			before := stateBytesOf(t, dir)
			_, _, err = engine.Drive(Observation{})
			if wantError && err == nil {
				t.Fatal("handler error was ignored")
			}
			if !wantError && err != nil {
				t.Fatalf("waiting step returned error: %v", err)
			}
			if !bytes.Equal(before, stateBytesOf(t, dir)) {
				t.Fatal("waiting or failed step persisted completion")
			}
		})
	}
}

func TestInternalStepErrorRollsBackUpstreamTransformation(t *testing.T) {
	compiled, registry := extensionDefinition(t)
	bindings := extensionHandlers()
	bindings["engine.message.publish"] = InternalStep{Runner: authoring.RunnerDurableActivity, Run: func(s *State, step compiler.CompiledStep) (bool, error) {
		if !s.stepCompleted("prepare") || len(s.RecoveryRecords) != 1 {
			t.Fatal("upstream transformation did not run")
		}
		return false, errors.New("cannot publish message")
	}}
	dir := t.TempDir()
	engine, err := New(mustStore(t, dir), Config{Definition: compiled, Registry: registry, InternalSteps: bindings})
	if err != nil {
		t.Fatal(err)
	}
	view, err := decision.NewState("1", runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.InitWithMetadata(view, testProvider, Observation{}, "", "", nil, "", "", nil); err != nil {
		t.Fatal(err)
	}
	before := stateBytesOf(t, dir)
	if _, _, err := engine.Drive(Observation{}); err == nil {
		t.Fatal("downstream error was ignored")
	}
	if !bytes.Equal(before, stateBytesOf(t, dir)) {
		t.Fatal("partial upstream transformation committed")
	}
}

func TestInternalDependencyViewRefreshesAfterHandlerAndCompletion(t *testing.T) {
	for _, waiting := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "waiting-state-change"}[waiting], func(t *testing.T) {
			registry := compiler.NewRegistry()
			for _, id := range []authoring.CodecID{"codec.message.in", "codec.message.out"} {
				if err := registry.RegisterCodec(id); err != nil {
					t.Fatal(err)
				}
			}
			var steps []authoring.Step
			for i, name := range []string{"seed", "available", "consume"} {
				handler := authoring.HandlerID("engine.message." + name)
				if err := registry.RegisterHandler(handler, authoring.RunnerEngineLocal); err != nil {
					t.Fatal(err)
				}
				header := authoring.Header{ID: authoring.StepID(name), NodeID: "message", DefinitionVersion: "1"}
				if i > 0 {
					header.Dependencies = []authoring.StepID{[]authoring.StepID{"seed", "available"}[i-1]}
				}
				input := authoring.IO{InputCodec: "codec.message.in", OutputCodec: "codec.message.out"}
				step, err := authoring.NewLocalStep(header, input, authoring.LocalSpec{Handler: handler})
				if err != nil {
					t.Fatal(err)
				}
				steps = append(steps, step)
			}
			compiled, err := compiler.Compile(&compiler.Definition{Version: "1", EntryNode: "message", Steps: steps}, registry)
			if err != nil {
				t.Fatal(err)
			}
			view, err := decision.NewState("1", runtime.PhaseIntakeRegistered)
			if err != nil {
				t.Fatal(err)
			}
			s := State{State: *view}
			bindings := map[authoring.HandlerID]InternalStep{}
			for _, name := range []string{"seed", "available", "consume"} {
				name := name
				bindings[authoring.HandlerID("engine.message."+name)] = InternalStep{Runner: authoring.RunnerEngineLocal, Run: func(s *State, step compiler.CompiledStep) (bool, error) {
					if name == "consume" && !s.stepCompleted("available") {
						return false, errors.New("consumer missed the current dependency")
					}
					s.RecoveryRecords = append(s.RecoveryRecords, RecoveryRecord{Detail: name})
					if name == "available" && waiting {
						if err := s.CompleteStep(step.Header.ID, compiled); err != nil {
							return false, err
						}
						return false, nil
					}
					return true, nil
				}}
			}
			if err = s.settleFrontierSteps(compiled, bindings); err != nil {
				t.Fatal(err)
			}
			order := []string{}
			for _, record := range s.RecoveryRecords {
				order = append(order, record.Detail)
			}
			if !reflect.DeepEqual(s.Completed, []authoring.StepID{"available", "consume", "seed"}) || !reflect.DeepEqual(order, []string{"seed", "available", "consume"}) {
				t.Fatalf("dependency execution differs from completed set: completed=%v order=%v", s.Completed, order)
			}
			before := append([]RecoveryRecord{}, s.RecoveryRecords...)
			if err = s.settleFrontierSteps(compiled, bindings); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(before, s.RecoveryRecords) {
				t.Fatal("repeat settlement reran completed work")
			}
		})
	}
}

func TestResultPredicateImplementationBinding(t *testing.T) {
	for _, condition := range []string{"supported", "unknown", "negated", "precondition", "local", "host"} {
		t.Run(condition, func(t *testing.T) {
			source, registry := definition.Workflow(), definition.Registry()
			if err := registry.RegisterPredicate("pred.unimplemented.post"); err != nil {
				t.Fatal(err)
			}
			for i, value := range source.Steps {
				switch step := value.(type) {
				case authoring.AgentStep:
					if condition == "local" || condition == "host" {
						continue
					}
					switch condition {
					case "unknown":
						step.IO.Postconditions = []authoring.PredicateRef{{ID: "pred.unimplemented.post"}}
					case "negated":
						step.IO.Postconditions[0].Negated = true
					case "precondition":
						step.IO.Preconditions = []authoring.PredicateRef{{ID: "pred.review.post"}}
					}
					source.Steps[i] = step
				case authoring.LocalStep:
					if condition != "local" {
						continue
					}
					step.IO.Postconditions = []authoring.PredicateRef{{ID: "pred.review.post"}}
					source.Steps[i] = step
				case authoring.HostActionStep:
					if condition != "host" {
						continue
					}
					step.IO.Postconditions = []authoring.PredicateRef{{ID: "pred.review.post"}}
					source.Steps[i] = step
				default:
					continue
				}
				break
			}
			candidate, err := compiler.Compile(source, registry)
			if err != nil {
				t.Fatal("candidate must reach implementation binding", err)
			}
			_, err = New(mustStore(t, t.TempDir()), Config{Definition: candidate, Registry: registry})
			if condition == "supported" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			var rejected *compiler.Error
			if !errors.As(err, &rejected) || rejected.Class != authoring.FailureBlockedBug || !strings.Contains(rejected.Msg, "predicate") {
				t.Fatalf("unimplemented condition activated: %v", err)
			}
		})
	}
}

func TestRepeatedResultPredicateKeepsSemanticFailureAndEvidence(t *testing.T) {
	cd, registry := reviewStepsFixture(t, "product.review", "start.readiness")
	for i := range cd.Steps {
		if _, agent := cd.Steps[i].Payload.(compiler.CompiledAgentStep); agent {
			cd.Steps[i].IO.Postconditions = append(cd.Steps[i].IO.Postconditions, authoring.PredicateRef{ID: reviewResultPredicate})
		}
	}
	engine, _ := reviewEngine(t, cd, registry)
	actionID := issueReady(t, engine)[0]
	submitSpawnReceipt(t, engine, "spawn", actionID)
	evidence := reviewEvidence(review.ProductReview, actionID, "", "FAIL", "bounded behavior fails", []review.Finding{{Severity: "P2", Message: "bounded behavior fails"}})
	event, err := NewReviewWorkerResultEvent("result", actionID, testProvider, "actual-semantic-failure", evidence)
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := engine.Submit(event, Observation{})
	if err != nil || accepted.Status != "ACCEPTED" {
		t.Fatalf("valid FAIL was rejected: %+v %v", accepted, err)
	}
	snapshot := engine.LoadMustSucceed(t)
	series := snapshot.State.ReviewSeries.ProductReview
	result := snapshot.State.Results[actionID]
	if series.Completed != 1 || len(series.RawCandidates) != 1 || result.Review == nil || !reflect.DeepEqual(*result.Review, evidence) || len(snapshot.State.RejectedResults) != 0 {
		t.Fatalf("result predicate did not preserve its original evidence and one semantic round: %+v %+v", series, result)
	}
	event.ID = "repeat-result"
	duplicate, err := engine.Submit(event, Observation{})
	if err != nil || duplicate.Status != "DUPLICATE" {
		t.Fatalf("repeated result: %+v %v", duplicate, err)
	}
	after := engine.LoadMustSucceed(t)
	if !reflect.DeepEqual(after.State.ReviewSeries, snapshot.State.ReviewSeries) || !reflect.DeepEqual(after.State.Results, snapshot.State.Results) {
		t.Fatal("duplicate changed accepted evidence")
	}
}

func TestResultPredicateRejectsChangedValidationBindingWithOriginalRetry(t *testing.T) {
	engine, state, pending := unfinishedDevelopmentState(t, "engine.development.batch.v5")
	state.RejectedResults = map[string]RejectedResult{}
	state.EndedAttempts = map[runtime.TaskKey]Attempt{}
	binding := state.DispatchBindings[pending.ActionID]
	binding.Generation++
	payload := WorkerResultPayload{ActionID: pending.ActionID, Provider: state.RunProvider, Outcome: OutcomePass, PayloadDigest: "original-invalid-output", Validation: &ValidationResult{Binding: binding, Status: "CONTINUE", Development: &DevelopmentEvidence{Summary: "Actual progress", RemainingWork: "Finish original work"}}}
	originalValidation := *state.Validation
	originalAttempt := state.Attempts[pending.Task]
	accepted, _, err := engine.admitWorkerResultRecord(state, pending.ActionID, &payload, "invalid-binding", "", 1, 2, Observation{})
	if err != nil || accepted.Status != "REJECTED_INVALID_RESULT" {
		t.Fatalf("validation result did not close its invalid attempt: %+v %v", accepted, err)
	}
	rejected := state.RejectedResults[pending.ActionID]
	if rejected.Reason != CodeValidationEvidenceRejected+": business result binding differs from its dispatch" || !reflect.DeepEqual(rejected.Payload, workerResultRecord(&payload)) || !reflect.DeepEqual(*state.Validation, originalValidation) {
		t.Fatalf("validation rejection changed its native error or evidence: %+v", rejected)
	}
	ended := state.EndedAttempts[pending.Task]
	if ended.ID != originalAttempt.ID || ended.Attempts != originalAttempt.Attempts+1 || !ended.RetryExhausted || state.Tasks[pending.Task] != runtime.TaskQueued || len(state.PendingActions) != 0 || state.Results[pending.ActionID].ActionID != "" {
		t.Fatal("invalid result changed logical duty or recorded completion")
	}
}
