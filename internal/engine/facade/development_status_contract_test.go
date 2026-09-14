package facade

import (
	"errors"
	"formal-gates/internal/engine/protocol"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestDevelopmentStatusContract(t *testing.T) {
	schema := preparationSchema("development-worker", Run{}, protocol.ExecutionClaim{})
	fields := schema["properties"].(map[string]any)
	if got := fields["status"].(map[string]any)["enum"]; !reflect.DeepEqual(got, []string{"PASS", "CONTINUE"}) {
		t.Fatalf("task statuses: %v", got)
	}
	validation := fields["validation"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)
	if got := validation["result"].(map[string]any)["enum"]; !reflect.DeepEqual(got, []string{"PASS", "FAIL"}) {
		t.Fatalf("check statuses: %v", got)
	}
}

func TestDevelopmentDeliveryAndInterruptionUseExistingContinuation(t *testing.T) {
	for _, interrupted := range []bool{false, true} {
		t.Run(map[bool]string{false: "manifest", true: "interrupted"}[interrupted], func(t *testing.T) {
			f := facadePreparation(t, nil)
			x := facadeExecution(t, f, "development-worker")
			source := nativeFixtureClaim(t, f, x)
			before, err := f.Show()
			if err != nil {
				t.Fatal(err)
			}
			writePreparationFile(t, before.Regular.Preparation.Resources["development"], "unlisted.txt", "fixture change")
			raw := protocol.PreparationResult{ActionID: x.Claim.ActionID, AttemptID: x.Claim.AttemptID, Provider: x.Claim.Provider, RequirementRevision: x.Claim.RequirementRevision, Candidate: x.Claim.Candidate, Status: "PASS", Manifest: []string{}, Handoff: "fixture"}
			answer := filepath.Join(t.TempDir(), "answer.json")
			if err := writeJSONAtomic(answer, raw); err != nil {
				t.Fatal(err)
			}
			source.Status, source.Reason = "UNAVAILABLE", "fixture source"
			var waitErr error
			want := "CONTINUE"
			if interrupted {
				waitErr = errors.New("fixture transport interrupted")
				want = "RUNTIME_ERROR"
			}
			got, err := f.finishNativeResult(x.Claim.ActionID, x.Claim, source.Identity, answer, waitErr, source)
			if err != nil {
				t.Fatal(err)
			}
			result := got.Executions[x.Claim.ActionID].PreparationResult
			if result == nil || result.Status != want || len(got.Regular.Preparation.Batches) != 0 {
				t.Fatalf("result=%+v", result)
			}
			if interrupted && !strings.Contains(result.Message, waitErr.Error()) {
				t.Fatal("original interruption evidence missing")
			}
			next := facadeExecution(t, f, "development-worker")
			if next.Claim.Task != x.Claim.Task || next.Identity != source.Identity || next.Claim.Operation != "resume" {
				t.Fatal("lost batch or native continuation")
			}
		})
	}
}
