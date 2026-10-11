package worker

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/prompts"
)

func TestRoleMaterialsKeepObservationsWithoutPhysicalWorkspaceBindings(t *testing.T) {
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	const workspace = "/program owned/workspace"
	const receipts = "/program owned/receipts"
	const stdout = "actual output mentions /semantic/path and preserves \"quotes\"\n"
	const command = `printf '%s' '/authored literal/path'`
	evidence := protocol.CheckEvidence{Directory: receipts, WorkingDirectory: workspace, ExitCode: 1, Error: "actual execution error", StdoutText: stdout, StderrText: "actual stderr\n", Tool: "actual executable sha256:tool", Environment: "actual environment", Invocation: "original invocation", Stdout: protocol.CheckStreamProof{SHA256: "stdout proof", Size: 41}}
	fact := protocol.ExecutionFact{CaseID: "failed", Operations: []protocol.ExecutionOperationFact{{Parameters: protocol.CheckParameters{QA: &protocol.QACheckParameters{WorkspaceRoot: workspace, Operation: coverage.Operation{Kind: coverage.OperationShell, Command: command}}}, Evidence: evidence}}}
	procedure, observation, err := fact.Render()
	if err != nil {
		t.Fatal(err)
	}
	record := coverage.ExecutionRecord{CaseID: fact.CaseID, Procedure: procedure, Observation: observation, Comparison: "actual comparison", Result: coverage.ExecutedFail}
	report := &coverage.ExecutionReport{Records: []coverage.ExecutionRecord{record}}
	finding := protocol.ObservationFinding{Target: protocol.ValidationTarget{Execution: report}, Evidence: protocol.WorkerResult{Validation: &protocol.ValidationResult{Execution: report}}, FailedCases: []coverage.ExecutionRecord{record}}
	for _, handler := range []string{"engine.development.batch.v2", "engine.qa.execution.v2", "engine.repair.v2", "engine.repair.verify.v2", "engine.validation.observation.v2", "engine.qa.design.v2", "engine.qa.review.rationale.v2", "engine.gate.v2"} {
		t.Run(handler, func(t *testing.T) {
			input := semanticInput()
			input.Handler = handler
			input.Pending.AttemptID = "issued attempt"
			input.RequirementDocument.Content = "confirmed requirement"
			if handler == "engine.repair.v2" {
				input.AvailableRouteCandidates = slices.DeleteFunc(catalog.RouteCandidates(), func(id string) bool { return id == "blackbox" || id == "whitebox" })
			}
			v := input.Validation
			v.Workspace = &protocol.NativeResource{Path: workspace}
			v.Development = &protocol.DevelopmentInput{Checks: []protocol.CheckEvidence{evidence}}
			v.ExecutionFacts = []protocol.ExecutionFact{fact}
			v.Completed, v.Inherited = []coverage.ExecutionRecord{record}, []coverage.ExecutionRecord{record}
			v.Repair = &protocol.RepairInput{ExecutionFailures: []protocol.RepairExecutionFailure{{Records: []coverage.ExecutionRecord{record}}}}
			v.Gate = &protocol.GateInput{Gate: catalog.Gates[0].ID}
			v.Observation = &protocol.ObservationInput{
				Findings:    []protocol.ObservationFinding{finding},
				Problems:    []protocol.ObservationProblem{{Findings: []protocol.ObservationFinding{finding}}},
				Scopes:      []protocol.ObservationScope{{Baseline: &protocol.ValidationTarget{Execution: report}}},
				GateImpacts: []protocol.ObservationGateImpact{{Evidence: finding.Evidence}},
				Suggestions: []protocol.ObservationSuggestion{{Workspace: v.Workspace}},
			}
			before, _ := json.Marshal(input)
			material, err := Render(input, catalog)
			if err != nil {
				t.Fatal(err)
			}
			for _, physical := range []string{workspace, receipts, "workspaceRoot", "workingDirectory", "parametersDigest", "requestDigest"} {
				if strings.Contains(material.Prompt, physical) {
					t.Fatalf("%s actively sent a mechanical binding %q", handler, physical)
				}
			}
			if handler == "engine.development.batch.v2" || handler == "engine.qa.execution.v2" || handler == "engine.repair.v2" || handler == "engine.repair.verify.v2" || handler == "engine.validation.observation.v2" {
				for _, needed := range []string{"actual execution error", "actual stderr", "stdout proof", "actual executable sha256:tool", "actual environment", "original invocation", "actual output mentions /semantic/path"} {
					if !strings.Contains(material.Prompt, needed) {
						t.Fatalf("%s lost actual failure evidence %q", handler, needed)
					}
				}
			}
			if handler == "engine.qa.execution.v2" || handler == "engine.repair.v2" || handler == "engine.repair.verify.v2" || handler == "engine.validation.observation.v2" {
				if !strings.Contains(material.Prompt, command) {
					// JSON escaping does not alter the approved value.
					encoded, _ := json.Marshal(command)
					if !strings.Contains(material.Prompt, string(encoded)) {
						t.Fatal("approved shell text changed")
					}
				}
			}
			after, _ := json.Marshal(input)
			internal, _ := json.Marshal(material.Input)
			if !bytes.Equal(before, after) || !bytes.Equal(before, internal) {
				t.Fatal("model projection changed original program facts")
			}
		})
	}
}

func TestRepairMaterialPreservesPlainSemanticFailureText(t *testing.T) {
	for _, original := range []string{"real error mentions /required/path, not a serialized execution record", `{"message":"actual semantic JSON /required/path","exitCode":1}`} {
		value := promptRecords([]coverage.ExecutionRecord{{Observation: original, Procedure: original}}, true)[0].(map[string]any)
		if value["observation"] != original || value["procedure"] != original {
			t.Fatal("plain semantic text was interpreted or rewritten")
		}
	}
}
