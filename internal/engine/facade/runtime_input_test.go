package facade

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
)

// Build an isolated historical-state fixture through the normal envelope
// writer. This never edits a real run or backfills its missing instructions.
func removeFixtureInstructions(t *testing.T, f *Facade, change func(*protocol.RegularState)) {
	t.Helper()
	snap, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	change(snap.State.Regular)
	_, err = f.store.Save(persistence.Transaction{
		ExpectedRevision: snap.Revision, ExpectedFingerprint: "fixture-state",
		CollectFingerprint: func() (string, error) { return "fixture-state", nil },
		Content:            snap.State,
	})
	if err != nil {
		t.Fatal(err)
	}
}

type inputTurnSpy struct {
	codexRuntimeAdapter
	turns, probes int
}

func (s *inputTurnSpy) turn(nativeTurn) (string, error) {
	s.turns++
	return "", fmt.Errorf("unexpected fixture native turn")
}
func (s *inputTurnSpy) probe(nativeProbe) (string, json.RawMessage, error) {
	s.probes++
	return "", nil, fmt.Errorf("unexpected fixture native probe")
}

// Unclaimed input failures must reach neither a turn nor a recovery probe.
func assertInputFailureBeforeClaim(t *testing.T, f *Facade, x protocol.RegularExecution) {
	t.Helper()
	before, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	spy := &inputTurnSpy{}
	for i := 0; i < 2; i++ {
		_, err := f.executeNativeTransport(x.Claim.ActionID, false, spy)
		if err == nil || !strings.Contains(err.Error(), "PROMPT_CONFIG_MISSING") {
			t.Fatalf("expected prompt preflight: %v", err)
		}
	}
	after, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if spy.turns != 0 || spy.probes != 0 || before.Revision != after.Revision ||
		!reflect.DeepEqual(before.State.Attempts, after.State.Attempts) ||
		!reflect.DeepEqual(before.State.RegularExecutions, after.State.RegularExecutions) {
		t.Fatalf("missing input consumed claim/attempt or called adapter: turns=%d probes=%d", spy.turns, spy.probes)
	}
	if _, err := os.Stat(f.codexJob(x.Claim.ActionID)); !os.IsNotExist(err) {
		t.Fatalf("preclaim created job: %v", err)
	}
}

func whiteboxInputFixture(t *testing.T) (*Facade, protocol.RegularExecution) {
	t.Helper()
	f := facadePreparation(t, []string{"blackbox", "whitebox"})
	r, _ := f.Show()
	dev := r.Regular.Preparation.Resources["development"]
	writePreparationFile(t, dev, "go.mod", "module example.invalid/fixture\n\ngo 1.23\n")
	writePreparationFile(t, dev, "main.go", "package main\nfunc main(){}\n")
	facadeResult(t, f, facadeExecution(t, f, "development-worker"), protocol.PreparationResult{Manifest: []string{"go.mod", "main.go"}, Validation: []protocol.BatchValidation{{SubtaskID: "S1", Procedure: "local fixture", Observation: "prepared", Result: "PASS"}}, Handoff: "none"})
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	r, _ = f.Show()
	d := facadeDesign(r, "blackbox")
	facadeResult(t, f, facadeExecution(t, f, "blackbox-design"), protocol.PreparationResult{Design: &d})
	r, _ = f.Show()
	facadeResult(t, f, facadeExecution(t, f, "blackbox-review"), protocol.PreparationResult{Review: facadeReview(r, "blackbox")})
	if _, err := f.Drive(); err != nil {
		t.Fatal(err)
	}
	return f, facadeExecution(t, f, "whitebox-design")
}

func TestNativeMandatoryInputsBeforeClaim(t *testing.T) {
	t.Run("qa-review-role", func(t *testing.T) {
		f, _, _ := acceptedCapturedDesign(t)
		x := facadeExecution(t, f, "blackbox-review")
		removeFixtureInstructions(t, f, func(r *protocol.RegularState) { delete(r.ActionInstructions, "actions/qa-review.md") })
		assertInputFailureBeforeClaim(t, f, x)
	})
	t.Run("tools-only", func(t *testing.T) {
		f := readyRegularFacade(t)
		x := facadeExecution(t, f, "product-review")
		removeFixtureInstructions(t, f, func(r *protocol.RegularState) {
			text := r.ActionInstructions["actions/development-worker.md"]
			text = strings.Replace(text, "# 工具说明\n", "# Removed tool section\n", 1)
			r.ActionInstructions["actions/development-worker.md"] = text
		})
		assertInputFailureBeforeClaim(t, f, x)
	})
	t.Run("coverage-only", func(t *testing.T) {
		f, x := whiteboxInputFixture(t)
		removeFixtureInstructions(t, f, func(r *protocol.RegularState) {
			text := r.ActionInstructions["actions/qa-design.md"]
			r.ActionInstructions["actions/qa-design.md"] = strings.Replace(text, "# 白盒覆盖判断\n", "# Removed coverage section\n", 1)
		})
		assertInputFailureBeforeClaim(t, f, x)
	})
	t.Run("gate-instructions", func(t *testing.T) {
		f := frozenVerificationFixture(t, false)
		x := facadeExecution(t, f, "implementation-quality-gate")
		removeFixtureInstructions(t, f, func(r *protocol.RegularState) { delete(r.CatalogGateInstructions, "implementation-quality-gate") })
		assertInputFailureBeforeClaim(t, f, x)
	})
}

func TestContinuousHistoricalMissingInstructionsBlocksWithoutAttempts(t *testing.T) {
	f, a := continuousFixture(t)
	turns := 0
	a.answer = func(nativeTurn) any { turns++; return map[string]string{"message": "unexpected fixture turn"} }
	removeFixtureInstructions(t, f, func(r *protocol.RegularState) { r.ActionInstructions = nil })
	before, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	var id string
	for action, x := range before.State.RegularExecutions {
		if !x.Finished {
			id = action
		}
	}
	if id == "" {
		t.Fatal("no issued action")
	}
	for i := 0; i < 3; i++ {
		f, err = Open(f.root, f.runID)
		if err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		result, err := f.DriveContinuous(ctx)
		cancel()
		if err != nil || result.Boundary != "BLOCKED" || !strings.Contains(result.Blocked[id], "PROMPT_CONFIG_MISSING") {
			t.Fatalf("boundary=%s blocked=%v err=%v", result.Boundary, result.Blocked, err)
		}
	}
	after, err := f.engine.Load()
	if err != nil {
		t.Fatal(err)
	}
	if turns != 0 || !reflect.DeepEqual(before.State.Attempts, after.State.Attempts) ||
		!reflect.DeepEqual(before.State.RegularExecutions, after.State.RegularExecutions) ||
		after.State.Regular.Ask != nil || after.State.Regular.ActionInstructions != nil {
		t.Fatal("blocked replay claimed, retried, spawned, raised retryExhausted, or backfilled instructions")
	}
	if _, err := os.Stat(f.codexJob(id)); !os.IsNotExist(err) {
		t.Fatalf("created native job: %v", err)
	}

}
