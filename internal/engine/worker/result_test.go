package worker

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/runtime"
)

func semanticInput() protocol.WorkerInput {
	binding := coverage.ManifestBinding{SourceBinding: coverage.SourceBinding{RunID: "issued-run", RequirementRevision: "original-revision", SolutionRevision: "solution"}, ReviewKind: coverage.ReviewBlackbox, RouteScope: "approved-route", TopologyScope: "original-topology"}
	manifest := coverage.AcceptanceManifest{Binding: binding, Sources: []string{"alpha", "beta"}, Points: []coverage.AcceptancePoint{{PointID: "one", ObservableBehavior: "first behavior", Oracle: "first oracle"}, {PointID: "two", ObservableBehavior: "second behavior", Oracle: "second oracle"}}, Cases: []coverage.AcceptanceCase{{CaseID: "new"}, {CaseID: "prior"}}, SharedSteps: []coverage.SharedStep{{ID: "setup", Procedure: "real setup"}}}
	return protocol.WorkerInput{RunID: "issued-run", Handler: "engine.qa.design.v5", Provider: "native-provider", Pending: protocol.PendingAction{ActionID: "issued-action", AttemptID: "attempt"}, Validation: &protocol.ValidationInput{Binding: protocol.WorkBinding{Task: runtime.TaskKey{Node: "qa", Step: "work", Scope: "issued-scope"}, RequirementRevision: "original-revision", Generation: 7}, Manifest: manifest, Contract: &coverage.CoverageContract{Manifests: []coverage.AcceptanceManifest{manifest}}, SourcePoints: []protocol.SourcePoint{{SourceID: "alpha", PointID: "one"}, {SourceID: "beta", PointID: "one"}, {SourceID: "beta", PointID: "two"}}, ReviewScope: coverage.ScopeAffected}}
}
func decodeSemantic(t *testing.T, input protocol.WorkerInput, result any) protocol.WorkerResultPayload {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	event, err := DecodeResultWithPaths(input, "result", raw, "original-raw-digest", []string{})
	if err != nil {
		t.Fatal(err)
	}
	if event.Result.ActionID != input.Pending.ActionID || event.Result.Provider != input.Provider || event.Result.PayloadDigest != "original-raw-digest" {
		t.Fatal("issued transport identity lost")
	}
	return *event.Result
}
func TestDesignAssemblesAllSourcePointRelations(t *testing.T) {
	input := semanticInput()
	before, _ := json.Marshal(input)
	value := Design{DesignManifest: DesignManifest{Replace: true, Cases: []DesignedCase{{Scenario: "MISUSE", Oracle: "actual oracle", Operations: []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{"./app", ""}, Stdin: "input"}, {Kind: coverage.OperationShell, Command: "./app --error"}}}, {Scenario: "SUCCESS", Oracle: "other oracle"}}, Coverage: [][]int{{1, 2}, {1}}}, Checkpoint: true}
	payload := decodeSemantic(t, input, value)
	manifest := payload.Validation.Manifest
	if payload.Validation.Binding != input.Validation.Binding || manifest.Binding != input.Validation.Manifest.Binding || !reflect.DeepEqual(manifest.Sources, input.Validation.Manifest.Sources) || !reflect.DeepEqual(manifest.Points, input.Validation.Manifest.Points) {
		t.Fatal("original issued input not preserved")
	}
	expected := []coverage.CoverageEdge{{ReviewKind: coverage.ReviewBlackbox, SourceID: "alpha", PointID: "one", CaseID: manifest.Cases[0].CaseID}, {ReviewKind: coverage.ReviewBlackbox, SourceID: "beta", PointID: "one", CaseID: manifest.Cases[0].CaseID}, {ReviewKind: coverage.ReviewBlackbox, SourceID: "alpha", PointID: "one", CaseID: manifest.Cases[1].CaseID}, {ReviewKind: coverage.ReviewBlackbox, SourceID: "beta", PointID: "one", CaseID: manifest.Cases[1].CaseID}, {ReviewKind: coverage.ReviewBlackbox, SourceID: "beta", PointID: "two", CaseID: manifest.Cases[0].CaseID}}
	if !reflect.DeepEqual(manifest.Edges, expected) || manifest.Cases[0].Mode != coverage.CaseBlackbox || !payload.Validation.Checkpoint {
		t.Fatalf("semantic mapping or checkpoint lost: %+v", manifest)
	}
	if !reflect.DeepEqual(manifest.Cases[0].Operations, value.DesignManifest.Cases[0].Operations) {
		t.Fatal("semantic operation recipe changed")
	}
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("decoder mutated issued input")
	}
	value.DesignManifest.Coverage = append(value.DesignManifest.Coverage, []int{1})
	raw, _ := json.Marshal(value)
	if _, err := DecodeResultWithPaths(input, "result", raw, "digest", []string{}); err == nil {
		t.Fatal("unknown semantic point accepted")
	}
}
func TestReviewInheritancePreservesIndependentJudgments(t *testing.T) {
	input := semanticInput()
	input.Handler = "engine.qa.review.rationale.v2"
	input.Validation.InheritableReviews = []coverage.ReviewDecision{{ID: "prior", Status: coverage.StatusPass, Reason: "original approved rationale"}}
	input.Validation.InheritableSharedReviews = []coverage.ReviewDecision{{ID: "setup", Status: coverage.StatusPass, Reason: "original shared rationale"}}
	value := CaseReview{SourceDecisions: []*Decision{{Status: coverage.StatusFail, Reason: "actual coverage gap"}, {Status: coverage.StatusPass, Reason: "all beta obligations covered"}}, PointDecisions: []*Decision{{Status: coverage.StatusFail, Reason: "incorrect oracle"}, {Status: coverage.StatusPass, Reason: "second oracle sufficient"}}, CaseDecisions: []*Decision{{Status: coverage.StatusPass, Reason: "new case valid"}}}
	result := decodeSemantic(t, input, value).Validation.Review
	if result.Scope != coverage.ScopeAffected || result.Binding != input.Validation.Manifest.Binding || result.SourceDecisions[0].Status != coverage.StatusFail || result.PointDecisions[0].Status != coverage.StatusFail || !result.CaseDecisions[1].Inherited || result.CaseDecisions[1].Reason != "original approved rationale" || !result.SharedStepDecisions[0].Inherited {
		t.Fatal("inheritance replaced current semantic judgments")
	}
	value.CaseDecisions = append(value.CaseDecisions, &Decision{Status: coverage.StatusFail, Reason: "new judgment outside issued affected scope"})
	raw, _ := json.Marshal(value)
	if _, err := DecodeResultWithPaths(input, "result", raw, "digest", []string{}); err == nil {
		t.Fatal("model re-listed an inherited decision")
	}
	value.CaseDecisions = value.CaseDecisions[:1]

	value.SourceDecisions[0] = nil
	raw, _ = json.Marshal(value)
	if _, err := DecodeResultWithPaths(input, "result", raw, "digest", []string{}); err == nil {
		t.Fatal("inherited case granted aggregate approval")
	}
}
func TestExecutionUsesPendingObservationsAndOriginalInheritance(t *testing.T) {
	input := semanticInput()
	input.Handler = "engine.qa.execution.v3"
	input.Validation.Execution = &coverage.ExecutionBinding{RequiredSourcesDigest: "sources", ManifestDigest: "manifest", MapDigest: "map", WhitelistDigest: "whitelist", Candidate: coverage.ValidationCandidate{Identity: "candidate", Digest: "candidate-digest"}, Scope: coverage.ScopeAffected, ExpectedCaseIDs: []string{"new", "prior", "done", "remaining"}}
	input.Validation.RequiredCases = []string{"new", "done", "remaining"}
	input.Validation.PendingCases = []string{"new", "prior", "remaining"}
	edges := []coverage.WhitelistEntry{{ReviewKind: coverage.ReviewBlackbox, SourceID: "alpha", PointID: "one", CaseID: "new"}, {ReviewKind: coverage.ReviewBlackbox, SourceID: "beta", PointID: "one", CaseID: "new"}, {ReviewKind: coverage.ReviewBlackbox, SourceID: "beta", PointID: "two", CaseID: "prior"}, {ReviewKind: coverage.ReviewBlackbox, SourceID: "alpha", PointID: "one", CaseID: "done"}, {ReviewKind: coverage.ReviewBlackbox, SourceID: "beta", PointID: "two", CaseID: "remaining"}}
	input.Validation.Whitelist = &coverage.ApprovedWhitelist{Entries: edges}
	inherited := coverage.ExecutionRecord{ReviewKind: coverage.ReviewBlackbox, SourceID: "beta", PointID: "two", CaseID: "prior", Procedure: "original procedure", Observation: "original observation", Comparison: "original oracle comparison", Result: coverage.ExecutedPass, Provenance: coverage.ProvenanceInherited, InheritedFromCandidate: "prior-candidate", ReuseEvidence: "proved identical inputs"}
	completed := coverage.ExecutionRecord{ReviewKind: coverage.ReviewBlackbox, SourceID: "alpha", PointID: "one", CaseID: "done", Result: coverage.ExecutedPass, Provenance: coverage.ProvenanceExecuted}
	input.Validation.Inherited = []coverage.ExecutionRecord{inherited}
	input.Validation.Completed = []coverage.ExecutionRecord{completed}
	input.Validation.ExecutionFacts = []protocol.ExecutionFact{{CaseID: "new", Operations: []protocol.ExecutionOperationFact{{ActionID: "actual-op", Parameters: protocol.CheckParameters{QA: &protocol.QACheckParameters{Source: "case:new", Operation: coverage.Operation{Kind: coverage.OperationShell, Command: "./app --negative"}, Directory: "."}}, Evidence: protocol.CheckEvidence{ExitCode: 2, StdoutText: "rejected", Tool: "real-tool", Environment: "real-environment"}}}}}
	value := ExecutionJudgments{Execution: Execution{Records: []*Observation{{Result: coverage.ExecutedFail, Comparison: "compared against expected result"}}}}
	result := decodeSemantic(t, input, value).Validation
	if result.Tool != "real-tool" || result.Environment != "real-environment" || !result.Checkpoint || len(result.Execution.Records) != 3 || !reflect.DeepEqual(result.Execution.Records[2], inherited) || !reflect.DeepEqual(result.Execution.Binding.ActualCaseIDs, []string{"new", "prior"}) || len(result.Execution.NotExecuted) != 1 || result.Execution.NotExecuted[0].CaseID != "remaining" {
		t.Fatalf("execution partition changed: %+v", result.Execution)
	}
	if result.Execution.Binding.Candidate != input.Validation.Execution.Candidate || result.Execution.Binding.WhitelistDigest != "whitelist" || result.Execution.Records[0].Result != coverage.ExecutedFail || result.Execution.Records[1].Result != coverage.ExecutedFail {
		t.Fatal("expanded observations lost real result or issued identity")
	}
	value.Execution.Records = append(value.Execution.Records, &Observation{Result: coverage.ExecutedPass})
	raw, _ := json.Marshal(value)
	if _, err := DecodeResultWithPaths(input, "result", raw, "digest", []string{}); err == nil {
		t.Fatal("unissued additional case comparison accepted")
	}

}
func TestExecutionCheckpointDoesNotResubmitCompletedCaseEdges(t *testing.T) {
	input := semanticInput()
	input.Handler = "engine.qa.execution.v3"
	input.Validation.Execution = &coverage.ExecutionBinding{Candidate: coverage.ValidationCandidate{Identity: "candidate"}}
	input.Validation.PendingCases, input.Validation.RequiredCases = []string{"new"}, []string{"new"}
	input.Validation.Whitelist = &coverage.ApprovedWhitelist{Entries: []coverage.WhitelistEntry{{ReviewKind: coverage.ReviewBlackbox, SourceID: "alpha", PointID: "one", CaseID: "new"}, {ReviewKind: coverage.ReviewBlackbox, SourceID: "beta", PointID: "one", CaseID: "new"}}}
	input.Validation.Completed = []coverage.ExecutionRecord{{ReviewKind: coverage.ReviewBlackbox, SourceID: "alpha", PointID: "one", CaseID: "new", Result: coverage.ExecutedFail}}
	input.Validation.ExecutionFacts = []protocol.ExecutionFact{{CaseID: "new", Operations: []protocol.ExecutionOperationFact{{ActionID: "operation", Parameters: protocol.CheckParameters{QA: &protocol.QACheckParameters{Source: "case:new"}}, Evidence: protocol.CheckEvidence{Tool: "actual-tool", Environment: "actual-environment"}}}}}
	result := ExecutionJudgments{Execution: Execution{Records: []*Observation{{Result: coverage.ExecutedFail, Comparison: "actual mismatch"}}}}
	body := decodeSemantic(t, input, result).Validation
	if len(body.Execution.Records) != 1 || body.Execution.Records[0].SourceID != "beta" || body.Execution.Records[0].Result != coverage.ExecutedFail {
		t.Fatal("partial case comparison resubmitted its completed edge")
	}
}

func TestSemanticResultRejectsIssuedAndGeneratedFields(t *testing.T) {
	for _, sample := range []struct{ handler, raw string }{
		{"engine.qa.design.v5", `{"actionId":"wrong","cases":[],"coverage":{}}`},
		{"engine.qa.design.v5", `{"provider":"wrong","cases":[],"coverage":{}}`},
		{"engine.qa.design.v5", `{"payloadDigest":"wrong","cases":[],"coverage":{}}`},
		{"engine.qa.design.v5", `{"binding":{"generation":9},"cases":[],"coverage":{}}`},
		{"engine.qa.design.v5", `{"sources":["wrong"],"cases":[],"coverage":{}}`},
		{"engine.qa.design.v5", `{"cases":[{"caseId":"new","mode":"WHITEBOX","oracle":"actual"}],"coverage":{}}`},
		{"engine.gate.v2", `{"status":"PASS","findings":[{"id":"wrong","severity":"P3","message":"real finding"}]}`},
		{"engine.qa.review.rationale.v2", `{"sourceDecisions":[],"pointDecisions":[],"caseDecisions":[],"suggestions":[{"id":"wrong","severity":"P3","message":"actual suggestion"}]}`},
		{"engine.qa.review.rationale.v2", `{"sourceDecisions":[],"pointDecisions":[],"caseDecisions":[],"suggestions":[{"decision":"ACCEPT","severity":"P3","message":"actual suggestion"}]}`},
		{"engine.qa.review.rationale.v2", `{"scope":"FULL","sourceDecisions":[],"pointDecisions":[],"caseDecisions":[]}`},
		{"engine.qa.review.rationale.v2", `{"sourceDecisions":[{"id":"wrong","status":"PASS","reason":"actual"}],"pointDecisions":[],"caseDecisions":[]}`},
		{"engine.repair.v5", `{"granularityReview":{"batches":[{"id":"batch","route":{"mode":"full","gates":[],"developmentPlan":{"digest":"forged"}}}]}}`},
		{"engine.qa.execution.v3", `{"binding":{"candidate":{"identity":"wrong"}},"records":{}}`},
		{"engine.qa.execution.v3", `{"records":[{"sourceId":"wrong","result":"EXECUTED_PASS"}]}`},
	} {
		input := semanticInput()
		input.Handler = sample.handler
		_, err := DecodeResultWithPaths(input, "result", []byte(sample.raw), "digest", []string{})
		if err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("fixed field accepted or hidden: %s: %v", sample.raw, err)
		}
	}
}

func TestDesignFilesInheritOnlyWhenOmitted(t *testing.T) {
	input := semanticInput()
	input.Validation.Manifest.Cases[0].Files = []string{"original.case"}
	input.Validation.Manifest.SharedSteps[0].Files = []string{"original.shared"}
	input.Validation.Manifest.Cases[0].SharedSteps = []string{"setup"}
	input.Validation.AffectedCases = []string{"new"}
	omitted := CaseRevisions{Cases: []CaseChange{{}}}
	inherited := decodeSemantic(t, input, omitted).Validation.Manifest
	if !reflect.DeepEqual(inherited.Cases[0].Files, []string{"original.case"}) || !reflect.DeepEqual(inherited.SharedSteps[0].Files, []string{"original.shared"}) {
		t.Fatal("omission lost original issued files")
	}
	empty := []Location{}
	omitted.Cases[0].Files = &empty
	omitted.SharedSteps = []SharedStepChange{{Files: &empty}}

	raw, err := json.Marshal(omitted)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"files":[]`) {
		t.Fatal("explicit empty file relation disappeared in model JSON")
	}
	cleared := decodeSemantic(t, input, omitted).Validation.Manifest
	if len(cleared.Cases[0].Files) != 0 || len(cleared.SharedSteps[0].Files) != 0 {
		t.Fatal("program inherited a deliberately cleared relation")
	}
}

func TestWhiteboxDesignPreservesCapturedDeliveryAndMaterialRelations(t *testing.T) {
	input := semanticInput()
	input.Validation.Manifest.Binding.ReviewKind = coverage.ReviewWhitebox
	files := []Location{{Listing: 1, Entry: 2}}
	sharedFiles := []Location{{Listing: 1, Entry: 3}}
	unused := []Location{{Listing: 1, Entry: 4}}
	value := Design{DesignManifest: DesignManifest{Cases: []DesignedCase{{Scenario: "SUCCESS", Test: &TestSelection{Read: 2, Symbol: 1}, Oracle: "exact output", Files: &files, SharedSteps: []int{2}}}, SharedSteps: []DesignedSharedStep{{Procedure: "prepare test material", Files: &sharedFiles}, {Procedure: "not referenced", Files: &unused}}, Coverage: [][]int{{len(input.Validation.Manifest.Cases) + 1}, nil}}}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	paths := []string{"analytics_test.go", "frob_test.go", "fixture.data", "unrelated.go", "helper.data"}
	facts := []LocationFact{{Kind: "listing", Paths: paths}, {Kind: "read", Paths: paths[:1], Content: []byte("package analytics\nimport \"testing\"\nfunc TestUsage(t *testing.T) {}\n")}}
	event, err := DecodeResultWithFacts(input, "result", raw, "original-raw-digest", paths, facts)
	if err != nil {
		t.Fatal(err)
	}
	result := event.Result.Validation
	if result.Development == nil || !reflect.DeepEqual(result.Development.Paths, paths) || result.Development.Summary != "" {
		t.Fatalf("captured native delivery paths lost: %+v", result.Development)
	}
	created := result.Manifest.Cases[len(input.Validation.Manifest.Cases)]
	if !reflect.DeepEqual(result.Manifest.Cases[:len(input.Validation.Manifest.Cases)], input.Validation.Manifest.Cases) || !reflect.DeepEqual(created.Files, paths[:2]) || created.TestRef != "analytics_test.go::TestUsage" || !reflect.DeepEqual(result.Manifest.TestFiles(), []string{"analytics_test.go", "fixture.data", "frob_test.go"}) || result.Binding != input.Validation.Binding {
		t.Fatal("file semantics or original binding changed")
	}
	raw = append(raw[:len(raw)-1], []byte(`,"development":{"paths":["analytics_test.go"],"summary":"duplicate path declaration"}}`)...)
	if _, err = DecodeResultWithPaths(input, "result", raw, "original-raw-digest", []string{}); err == nil {
		t.Fatal("QA model supplied duplicate development fields")
	}
}
