package protocol

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/persistence"
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
			input.Inputs = []authoring.InputBinding{{From: "prepare", OutputField: "message", ToField: "message"}}
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
		if _, err := New(store, Config{Definition: compiled, Registry: registry, InternalSteps: bindings}, nil); err == nil {
			t.Fatal("missing implementation admitted")
		}
	}
	bindings := extensionHandlers()
	bindings["engine.message.prepare"] = InternalStep{Runner: authoring.RunnerDurableActivity, Run: bindings["engine.message.prepare"].Run}
	if _, err := New(store, Config{Definition: compiled, Registry: registry, InternalSteps: bindings}, nil); err == nil {
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
			engine, err := New(store, config, nil)
			if err != nil {
				t.Fatal(err)
			}
			view, err := decision.NewState("1", runtime.PhaseIntakeRegistered)
			if err != nil {
				t.Fatal(err)
			}
			if err := engine.Init(view, testProvider, fingerprintOf(t, engine)); err != nil {
				t.Fatal(err)
			}
			inject = true
			if _, _, err := engine.Drive(fingerprintOf(t, engine)); !errors.Is(err, persistence.ErrInjectedCrash) {
				t.Fatalf("fault result = %v", err)
			}
			reopenedStore := mustStore(t, dir)
			if _, err := reopenedStore.Recover(); err != nil {
				t.Fatal(err)
			}
			reopened, err := New(reopenedStore, config, nil)
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err := reopened.Drive(fingerprintOf(t, reopened)); err != nil {
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
			if _, _, err := reopened.Drive(fingerprintOf(t, reopened)); err != nil {
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
			engine, err := New(mustStore(t, dir), Config{Definition: compiled, Registry: registry, InternalSteps: bindings}, nil)
			if err != nil {
				t.Fatal(err)
			}
			view, err := decision.NewState("1", runtime.PhaseIntakeRegistered)
			if err != nil {
				t.Fatal(err)
			}
			if err := engine.Init(view, testProvider, fingerprintOf(t, engine)); err != nil {
				t.Fatal(err)
			}
			before := stateBytesOf(t, dir)
			_, _, err = engine.Drive(fingerprintOf(t, engine))
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
	engine, err := New(mustStore(t, dir), Config{Definition: compiled, Registry: registry, InternalSteps: bindings}, nil)
	if err != nil {
		t.Fatal(err)
	}
	view, err := decision.NewState("1", runtime.PhaseIntakeRegistered)
	if err != nil {
		t.Fatal(err)
	}
	if err := engine.Init(view, testProvider, fingerprintOf(t, engine)); err != nil {
		t.Fatal(err)
	}
	before := stateBytesOf(t, dir)
	if _, _, err := engine.Drive(fingerprintOf(t, engine)); err == nil {
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
				if i > 0 {
					input.Inputs = []authoring.InputBinding{{From: header.Dependencies[0], OutputField: "message", ToField: "message"}}
				}
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
