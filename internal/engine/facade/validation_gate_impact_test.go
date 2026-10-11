package facade

import (
	"bytes"
	"encoding/json"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"reflect"
	"testing"
)

func TestGateImpactKeepsItsCandidateAndOriginalResponsibility(t *testing.T) {
	for _, choice := range []string{"INHERIT", "RERUN"} {
		for _, route := range []bool{false, true} {
			name := choice
			if route {
				name += "/route"
			} else {
				name += "/same-route"
			}
			t.Run(name, func(t *testing.T) {
				d := gateImpactBoundary(t)
				original := d.run.Validation.Waves[0]
				var originalTarget protocol.ValidationTarget
				for _, target := range original.Targets {
					if target.Gate == "security" {
						originalTarget = *target
					}
				}
				pendingWave := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
				var pendingTarget protocol.ValidationTarget
				for _, target := range pendingWave.Targets {
					if target.Gate == "security" {
						pendingTarget = *target
					}
				}
				if pendingTarget.GateImpact == nil || pendingTarget.GateImpact.SourceWave != original.ID || pendingTarget.GateImpact.Decision != "" {
					t.Fatal("missing exact pending gate impact boundary")
				}
				if route {
					unlockRouteByRequirementRedo(d)
					d.confirmRoute(routeDecisionForGates([]string{"blackbox", "quality", "security", "performance"}))
					d.reopen()
					d.drive()
					id, in := d.gateAction("performance")
					d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
					d.drive()
				}
				d.recordExecutionScope(coverage.ReviewBlackbox, "invalid")
				d.reopen()
				d.drive()
				current := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
				for _, target := range current.Targets {
					if target.Gate == "security" {
						if !reflect.DeepEqual(target.GateImpact, pendingTarget.GateImpact) {
							t.Fatal("same candidate changed pending impact fact")
						}
						for _, in := range d.run.ValidationInputs {
							if in.Binding.Task == target.Task {
								t.Fatal("impact-free gate rerun issued")
							}
						}
					}
				}
				d.action("engine.qa.execution.v2") // Independent QA is eligible while this gate awaits a judgment.
				event := protocol.Event{ID: d.eventID(), Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: current.ID, Validation: &protocol.ValidationObservation{Kind: "GATE_IMPACT", Gate: "security", Decision: choice, Reason: "Independently checked authorization changes against the security gate's original responsibility"}}}
				accepted := d.submit(event)
				before, _ := d.f.engine.Load()
				replayed, _, err := d.f.Submit(event)
				if err != nil {
					t.Fatal(err)
				}
				after, _ := d.f.engine.Load()
				a, _ := json.Marshal(before)
				b, _ := json.Marshal(after)
				if !reflect.DeepEqual(accepted, replayed) || !bytes.Equal(a, b) {
					t.Fatal("impact replay changed acceptance or persistent facts")
				}
				if route {
					for _, w := range d.run.Validation.Waves {
						if w.ID == pendingWave.ID {
							for _, target := range w.Targets {
								if target.Gate == "security" && !reflect.DeepEqual(*target, pendingTarget) {
									t.Fatal("new-wave judgment mutated old frozen impact evidence")
								}
							}
						}
					}
				}
				if choice == "RERUN" {
					id, in := d.gateAction("security")
					if id == originalTarget.ResultAction || in.Binding.Candidate != current.Candidate.Identity {
						t.Fatal("gate rerun reused the old dispatch")
					}
					d.business(id, protocol.ValidationResult{Binding: in.Binding, Status: "PASS"})
				} else {
					for _, target := range d.run.Validation.Waves[len(d.run.Validation.Waves)-1].Targets {
						if target.Gate == "security" && (target.Status != "PASS" || target.ResultAction != originalTarget.ResultAction) {
							t.Fatal("inheritance rewrote original gate evidence")
						}
					}
				}
				id, in := d.action("engine.qa.execution.v2")
				r := executionFor(in)
				d.business(id, nativeExecutionResult(in, protocol.ValidationResult{Binding: in.Binding, Status: "PASS", Execution: &r}))
				d.drive()
				last := d.run.Validation.Waves[len(d.run.Validation.Waves)-1]
				if last.Status != "COMPLETE" || last.Scope != coverage.ScopeAffected {
					t.Fatal("required affected evidence did not complete")
				}
				for _, target := range last.Targets {
					if target.Gate == "security" && target.GateImpact != nil && target.GateImpact.Decision != choice {
						t.Fatal("scope change lost impact judgment")
					}
				}
			})
		}
	}
}
