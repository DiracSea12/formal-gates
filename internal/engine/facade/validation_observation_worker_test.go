package facade

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/worker"
)

func TestSemanticObservationFixesAffectedScopeAndGateImpactTogether(t *testing.T) {
	for _, scenario := range []string{"complete", "mandatory-and-selected", "illegal-impact"} {
		t.Run(scenario, func(t *testing.T) {
			d := gateImpactBoundary(t)
			id, input := d.action("engine.validation.observation")
			issued := input.Observation
			if issued == nil || len(issued.Scopes) != 1 || len(issued.GateImpacts) != 1 || issued.Scopes[0].Mode != coverage.ReviewBlackbox || !slices.Equal(issued.Scopes[0].MustExecute, []string{"invalid"}) || !slices.Contains(issued.Scopes[0].ApprovedCases, "normal") {
				t.Fatalf("affected scope lost actual legal complement: %#v", issued)
			}
			impact := issued.GateImpacts[0]
			if impact.Gate != "security" || impact.Range == nil || impact.Evidence.Validation == nil || impact.Evidence.Validation.Status != "PASS" || impact.Evidence.Validation.Execution != nil || impact.Evidence.Validation.Manifest != nil {
				t.Fatalf("impact does not preserve its original gate range/evidence: %#v", impact)
			}
			snapshot, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			original := snapshot.State.WorkerInputs(d.f.definition)[id]
			judgment := worker.ObservationJudgments{Findings: []worker.FindingJudgment{}, Scopes: []worker.ScopeJudgment{{Cases: []int{}, Reason: "Only rejection diagnostic changed; original normal behavior remains unaffected"}}, GateImpacts: []worker.ImpactJudgment{{Decision: "INHERIT", Reason: "Original authorization security responsibility and reviewed range remain unaffected"}}}
			if scenario == "mandatory-and-selected" {
				judgment.Scopes[0].Cases = []int{slices.Index(issued.Scopes[0].ApprovedCases, "normal") + 1}
			}
			if scenario == "illegal-impact" {
				judgment.GateImpacts[0].Decision = "PASS"
			}
			raw, err := json.Marshal(judgment)
			if err != nil {
				t.Fatal(err)
			}
			event, err := worker.DecodeResult(original, d.eventID(), raw, "actual-semantic-native-output")
			if err != nil {
				t.Fatal(err)
			}
			d.receipt(id)
			before, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			accepted := d.submit(event)
			if scenario == "illegal-impact" {
				if accepted.Status != "REJECTED_INVALID_RESULT" {
					t.Fatalf("original validator did not reject invalid %s: %#v", scenario, accepted)
				}
				after, err := d.f.engine.Load()
				if err != nil {
					t.Fatal(err)
				}
				if !reflect.DeepEqual(before.State.Validation, after.State.Validation) {
					t.Fatal("failed package partly fixed scope/impact")
				}
				return
			}
			if accepted.Status != "ACCEPTED" {
				t.Fatalf("legal original scope/impact rejected: %#v", accepted)
			}
			after, err := d.f.engine.Load()
			if err != nil {
				t.Fatal(err)
			}
			wave := after.State.Validation.Waves[len(after.State.Validation.Waves)-1]
			required := []string{"invalid"}
			if scenario == "mandatory-and-selected" {
				required = append(required, "normal")
			}
			for _, target := range wave.Targets {
				if target.Kind == "QA" && (!slices.Equal(target.RequiredCases, required) || target.ScopeReason == "") {
					t.Fatal("program did not retain exact affected subset")
				}
				if target.Gate == "security" && (target.GateImpact.Decision != "INHERIT" || target.ResultAction != impact.Evidence.ActionID) {
					t.Fatal("impact rewrote original PASS evidence")
				}
			}
			if after.State.Results[id].PayloadDigest != "actual-semantic-native-output" {
				t.Fatal("semantic worker lost original output evidence")
			}
			repeated, _, err := d.f.Submit(event)
			if err != nil || !reflect.DeepEqual(accepted, repeated) {
				t.Fatalf("repeat changed original acceptance: %#v %v", repeated, err)
			}
			// The same result completion first releases the approved native recipe.
			released := false
			for _, intent := range after.State.ExecutableHostActions(d.f.definition) {
				if intent.Adapter == nil || intent.Adapter.Operation != "op.check" {
					continue
				}
				raw, err := json.Marshal(intent.Adapter.Params)
				if err != nil {
					t.Fatal(err)
				}
				var params protocol.CheckParameters
				if err := json.Unmarshal(raw, &params); err != nil {
					t.Fatal(err)
				}
				if params.QA != nil && params.QA.CaseID == "invalid" && params.QA.Execution.Candidate == issued.Candidate {
					released = true
				}
			}
			if !released {
				t.Fatal("semantic completion did not refill the original approved native recipe")
			}
			d.drive()
			_, qa := d.action("engine.qa.execution")
			if !slices.Equal(qa.RequiredCases, required) {
				t.Fatal("scope dropped required or selected case")
			}
			if scenario == "mandatory-and-selected" {
				if len(qa.Inherited) != 0 {
					t.Fatal("executed selected case was falsely inherited")
				}
			} else if len(qa.Inherited) != 1 || qa.Inherited[0].CaseID != "normal" || qa.Inherited[0].Result != coverage.ExecutedPass {
				t.Fatal("scope bypassed original complementary inheritance")
			}

		})
	}
}

func TestTypedObservationRetiresOnlySupersededExecution(t *testing.T) {
	for _, execution := range []string{"unclaimed", "owned", "spawned", "unknown"} {
		t.Run(execution, func(t *testing.T) {
			d := gateImpactBoundary(t)
			id, input := d.action("engine.validation.observation")
			state := driverState(t, d)
			original := state.WorkerInputs(d.f.definition)[id]
			refs, err := d.f.workerMaterials(state, true)
			if err != nil {
				t.Fatal(err)
			}
			ref := refs[id]
			request, _, directory, err := d.f.WorkerExecutionRequest(ref, "")
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(directory, 0700); err != nil {
				t.Fatal(err)
			}
			if err := writeJSONAtomic(filepath.Join(directory, "native-request.json"), request); err != nil {
				t.Fatal(err)
			}
			if execution == "owned" {
				release, err := AdmitExecution(directory, request)
				if err != nil {
					t.Fatal(err)
				}
				owner, err := os.OpenFile(filepath.Join(directory, "owner"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if err == nil {
					err = owner.Close()
				}
				release()
				if err != nil {
					t.Fatal(err)
				}
			} else if execution == "spawned" {
				d.receipt(id)
			} else if execution == "unknown" {
				receipt, err := protocol.NewSpawnReceiptEvent(d.eventID(), id, d.host(), id, protocol.SpawnStatusUnknown)
				if err != nil {
					t.Fatal(err)
				}
				d.submit(receipt)
			}
			scope := input.Observation.Scopes[0]
			event := protocol.Event{ID: d.eventID(), Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: input.Observation.WaveID, Validation: &protocol.ValidationObservation{Kind: "EXECUTION_SCOPE", Mode: scope.Mode, AffectedCases: scope.MustExecute, Reason: "Only the failed rejection case changes"}}}
			accepted := d.submit(event)
			if accepted.Status != "ACCEPTED" {
				t.Fatal("original typed scope was rejected", accepted)
			}
			state = driverState(t, d)
			_, pending := state.PendingActions[id]
			if pending != (execution != "unclaimed") || state.ObsoleteActions[id].AttemptID != ref.AttemptID {
				t.Fatal("retirement lost actual execution ownership", state.PendingActions, state.ObsoleteActions[id])
			}
			if len(state.ObsoleteResults[id]) != 0 || state.Results[id].ActionID != "" {
				t.Fatal("retirement fabricated worker completion")
			}
			if execution == "unclaimed" {
				if release, err := AdmitExecution(directory, request); err == nil {
					release()
					t.Fatal("prepared superseded work passed original launch admission")
				}
			} else if execution == "unknown" {
				for _, edge := range []string{protocol.LifecycleStart, protocol.LifecycleStop} {
					observed, err := protocol.NewCorrelatedLifecycleEvent(d.eventID(), d.host(), id, "original-worker", edge)
					if err != nil {
						t.Fatal(err)
					}
					d.submit(observed)
				}
				if _, _, err := d.f.engine.ReconcileUnknownReceipt(id, emptyFingerprint()); err != nil {
					t.Fatal(err)
				}
				d.run, err = d.f.Show()
				if err != nil {
					t.Fatal(err)
				}
			} else {
				if execution == "owned" {
					d.receipt(id)
				}
				d.completeWorkerTerminations()
			}
			state = driverState(t, d)
			if _, pending := state.PendingActions[id]; pending {
				t.Fatal("observed stop did not retire the original execution")
			}
			d.reopen()
			if repeated := d.submit(event); !reflect.DeepEqual(accepted, repeated) {
				t.Fatal("reopen replay changed typed acceptance", repeated)
			}
			d.drive()
			_, remaining := d.action("engine.validation.observation")
			if len(remaining.Observation.Scopes) != 0 || len(remaining.Observation.GateImpacts) != 1 {
				t.Fatal("partial typed facts lost the remaining issued duty", remaining.Observation)
			}
			judgment := worker.ObservationJudgments{Findings: []worker.FindingJudgment{}, Scopes: []worker.ScopeJudgment{{Cases: []int{}, Reason: "Only the original failed case changes"}}, GateImpacts: []worker.ImpactJudgment{{Decision: "INHERIT", Reason: "The independently verified authorization boundary remains unchanged"}}}
			raw, _ := json.Marshal(judgment)
			late, err := worker.DecodeResult(original, d.eventID(), raw, "original-observation-output")
			if err != nil {
				t.Fatal(err)
			}
			before := driverState(t, d)
			if received := d.submit(late); received.Status != "OBSOLETE_RESULT" {
				t.Fatal("late output bypassed original obsolete admission", received)
			}
			after := driverState(t, d)
			if !reflect.DeepEqual(before.Validation, after.Validation) || len(after.ObsoleteResults[id]) != 1 {
				t.Fatal("late output changed accepted scope or was lost")
			}
		})
	}
}
