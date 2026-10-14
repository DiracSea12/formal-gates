package facade

import (
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"slices"
	"testing"
)

func TestExecutionCheckpointKeepsCasesPendingUntilEveryEdgeCompletes(t *testing.T) {
	for _, status := range []coverage.ExecutionStatus{coverage.ExecutedPass, coverage.ExecutedFail} {
		t.Run(string(status), func(t *testing.T) {
			inventory := &protocol.CoverageInventory{
				Sources: []coverage.RequiredSource{{SourceID: "SOL-001", Category: coverage.SolutionConstraint, Applicability: coverage.QAApplicability}},
				Points:  []coverage.AcceptancePoint{{PointID: "SOL-AC-001", ObservableBehavior: "input validation preserves state", Oracle: "state digest unchanged"}, {PointID: "SOL-AC-002", ObservableBehavior: "input validation preserves file contents", Oracle: "input file bytes unchanged"}},
				Links:   []protocol.SourcePoint{{SourceID: "SOL-001", PointID: "SOL-AC-001"}, {SourceID: "SOL-001", PointID: "SOL-AC-002"}},
			}
			d := configuredValidationDriver(t, inventory, "blackbox")
			_, input := d.action("engine.qa.design.v6")
			manifest := simpleManifest(input)
			for _, c := range manifest.Cases {
				for _, link := range inventory.Links {
					manifest.Edges = append(manifest.Edges, coverage.CoverageEdge{SourceID: link.SourceID, PointID: link.PointID, CaseID: c.CaseID})
				}
			}
			d.reviewReadyWithManifest(manifest)
			id, input := d.action("engine.qa.execution.v4")
			full := executionFor(input)
			parts := make([][]coverage.ExecutionRecord, 3)
			for _, row := range full.Records {
				if row.CaseID == "invalid" {
					row.Result = status
				}
				part := 0
				if row.CaseID == "normal" && row.SourceID == "SOL-001" {
					part = 1
					if row.PointID == "SOL-AC-002" {
						part = 2
					}
				}
				parts[part] = append(parts[part], row)
			}
			d.receipt(id)
			completed := 0
			for index, rows := range parts {
				report := coverage.ExecutionReport{Binding: full.Binding, Records: rows}
				report.Binding.ActualCaseIDs = nil
				for _, row := range rows {
					if !slices.Contains(report.Binding.ActualCaseIDs, row.CaseID) {
						report.Binding.ActualCaseIDs = append(report.Binding.ActualCaseIDs, row.CaseID)
					}
				}
				checkpoint := index < 2
				result := protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project", Checkpoint: checkpoint}
				event := protocol.Event{ID: d.eventID(), Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: string(d.eventID()), Validation: &result}}
				if accepted := d.submit(event); accepted.Status != "ACCEPTED" {
					t.Fatalf("evidence rejected: %+v", accepted)
				}
				d.submit(event)
				d.reopen()
				d.drive()
				completed += len(rows)
				if checkpoint {
					resumedID, resumed := d.action("engine.qa.execution.v4")
					if resumedID != id || resumed.Binding != input.Binding || !slices.Equal(resumed.PendingCases, []string{"normal"}) || len(resumed.Completed) != completed {
						t.Fatal("partially executed case lost its remaining edges or execution identity")
					}
					for _, row := range resumed.Completed {
						if row.CaseID == "invalid" && row.Result != status {
							t.Fatal("saved result changed during checkpoint resume")
						}
					}
					if d.run.Validation.Waves[0].Status != "COLLECTING" {
						t.Fatal("partial execution completed the wave")
					}
				}
			}
			target := d.run.Validation.Waves[0].Targets[input.Binding.Task.String()]
			expected := "PASS"
			if status == coverage.ExecutedFail {
				expected = "FAIL"
			}
			if target.Status != expected || len(target.Execution.Records) != len(full.Records) || !slices.Equal(target.Execution.Binding.ActualCaseIDs, target.Execution.Binding.ExpectedCaseIDs) {
				t.Fatal("final execution did not account for the full approved edge set")
			}
		})
	}
}
