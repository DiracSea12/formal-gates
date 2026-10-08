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
	return protocol.WorkerInput{RunID: "issued-run", Handler: "engine.qa.design", Provider: "native-provider", Pending: protocol.PendingAction{ActionID: "issued-action", AttemptID: "attempt"}, Validation: &protocol.ValidationInput{Binding: protocol.WorkBinding{Task: runtime.TaskKey{Node: "qa", Step: "work", Scope: "issued-scope"}, RequirementRevision: "original-revision", Generation: 7}, Manifest: manifest, SourcePoints: []protocol.SourcePoint{{SourceID: "alpha", PointID: "one"}, {SourceID: "beta", PointID: "one"}, {SourceID: "beta", PointID: "two"}}, ReviewScope: coverage.ScopeAffected}}
}
func decodeSemantic(t *testing.T, input protocol.WorkerInput, result Result) protocol.WorkerResultPayload {
	t.Helper()
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatal(err)
	}
	event, err := DecodeResult(input, "result", raw, "original-raw-digest")
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
	value := Result{Outcome: "PASS", Validation: &Validation{Status: "PASS", Checkpoint: true, Manifest: &Manifest{Cases: []Case{{CaseID: "new", Scenario: "MISUSE", Oracle: "actual oracle", Operations: []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{"./app", ""}, Stdin: "input"}, {Kind: coverage.OperationShell, Command: "./app --error"}}}, {CaseID: "prior", Scenario: "SUCCESS", Oracle: "other oracle"}}, Coverage: map[string][]string{"one": {"new", "prior"}, "two": {"new"}}}}}
	payload := decodeSemantic(t, input, value)
	manifest := payload.Validation.Manifest
	if payload.Validation.Binding != input.Validation.Binding || manifest.Binding != input.Validation.Manifest.Binding || !reflect.DeepEqual(manifest.Sources, input.Validation.Manifest.Sources) || !reflect.DeepEqual(manifest.Points, input.Validation.Manifest.Points) {
		t.Fatal("original issued input not preserved")
	}
	expected := []coverage.CoverageEdge{{ReviewKind: coverage.ReviewBlackbox, SourceID: "alpha", PointID: "one", CaseID: "new"}, {ReviewKind: coverage.ReviewBlackbox, SourceID: "beta", PointID: "one", CaseID: "new"}, {ReviewKind: coverage.ReviewBlackbox, SourceID: "alpha", PointID: "one", CaseID: "prior"}, {ReviewKind: coverage.ReviewBlackbox, SourceID: "beta", PointID: "one", CaseID: "prior"}, {ReviewKind: coverage.ReviewBlackbox, SourceID: "beta", PointID: "two", CaseID: "new"}}
	if !reflect.DeepEqual(manifest.Edges, expected) || manifest.Cases[0].Mode != coverage.CaseBlackbox || !payload.Validation.Checkpoint {
		t.Fatalf("semantic mapping or checkpoint lost: %+v", manifest)
	}
	if !reflect.DeepEqual(manifest.Cases[0].Operations, value.Validation.Manifest.Cases[0].Operations) {
		t.Fatal("semantic operation recipe changed")
	}
	after, _ := json.Marshal(input)
	if string(before) != string(after) {
		t.Fatal("decoder mutated issued input")
	}
	value.Validation.Manifest.Coverage["unknown"] = []string{"new"}
	raw, _ := json.Marshal(value)
	if _, err := DecodeResult(input, "result", raw, "digest"); err == nil {
		t.Fatal("unknown semantic point accepted")
	}
}
func TestReviewInheritancePreservesIndependentJudgments(t *testing.T) {
	input := semanticInput()
	input.Handler = "engine.qa.review.rationale"
	input.Validation.InheritableReviews = []coverage.ReviewDecision{{ID: "prior", Status: coverage.StatusPass, Reason: "original approved rationale"}}
	input.Validation.InheritableSharedReviews = []coverage.ReviewDecision{{ID: "setup", Status: coverage.StatusPass, Reason: "original shared rationale"}}
	value := Result{Outcome: "PASS", Validation: &Validation{Status: "FAIL", Review: &CaseReview{SourceDecisions: map[string]Decision{"alpha": {Status: coverage.StatusFail, Reason: "actual coverage gap"}, "beta": {Status: coverage.StatusPass, Reason: "all beta obligations covered"}}, PointDecisions: map[string]Decision{"one": {Status: coverage.StatusFail, Reason: "incorrect oracle"}, "two": {Status: coverage.StatusPass, Reason: "second oracle sufficient"}}, CaseDecisions: map[string]Decision{"new": {Status: coverage.StatusPass, Reason: "new case valid"}}}}}
	result := decodeSemantic(t, input, value).Validation.Review
	if result.Scope != coverage.ScopeAffected || result.Binding != input.Validation.Manifest.Binding || result.SourceDecisions[0].Status != coverage.StatusFail || result.PointDecisions[0].Status != coverage.StatusFail || !result.CaseDecisions[1].Inherited || result.CaseDecisions[1].Reason != "original approved rationale" || !result.SharedStepDecisions[0].Inherited {
		t.Fatal("inheritance replaced current semantic judgments")
	}
	value.Validation.Review.CaseDecisions["prior"] = Decision{Status: coverage.StatusFail, Reason: "new independently reproduced defect"}
	result = decodeSemantic(t, input, value).Validation.Review
	if result.CaseDecisions[1].Inherited || result.CaseDecisions[1].Status != coverage.StatusFail {
		t.Fatal("explicit new judgment swallowed by inheritance")
	}
	delete(value.Validation.Review.SourceDecisions, "alpha")
	raw, _ := json.Marshal(value)
	if _, err := DecodeResult(input, "result", raw, "digest"); err == nil {
		t.Fatal("inherited case granted aggregate approval")
	}
}
func TestExecutionUsesPendingObservationsAndOriginalInheritance(t *testing.T) {
	input := semanticInput()
	input.Handler = "engine.qa.execution"
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
	value := Result{Outcome: "PASS", Validation: &Validation{Status: "FAIL", Checkpoint: true, Execution: &Execution{Records: map[string]Observation{"new": {Result: coverage.ExecutedFail, Comparison: "compared against expected result"}}}}}
	result := decodeSemantic(t, input, value).Validation
	if result.Tool != "real-tool" || result.Environment != "real-environment" || !result.Checkpoint || len(result.Execution.Records) != 3 || !reflect.DeepEqual(result.Execution.Records[2], inherited) || !reflect.DeepEqual(result.Execution.Binding.ActualCaseIDs, []string{"new", "prior"}) || len(result.Execution.NotExecuted) != 1 || result.Execution.NotExecuted[0].CaseID != "remaining" {
		t.Fatalf("execution partition changed: %+v", result.Execution)
	}
	if result.Execution.Binding.Candidate != input.Validation.Execution.Candidate || result.Execution.Binding.WhitelistDigest != "whitelist" || result.Execution.Records[0].Result != coverage.ExecutedFail || result.Execution.Records[1].Result != coverage.ExecutedFail {
		t.Fatal("expanded observations lost real result or issued identity")
	}
	value.Validation.Execution.Records["done"] = Observation{Result: coverage.ExecutedPass}
	raw, _ := json.Marshal(value)
	if _, err := DecodeResult(input, "result", raw, "digest"); err == nil {
		t.Fatal("completed evidence resubmitted")
	}
	delete(value.Validation.Execution.Records, "done")
	value.Validation.Execution.Records["prior"] = Observation{Result: coverage.ExecutedPass}
	raw, _ = json.Marshal(value)
	if _, err := DecodeResult(input, "result", raw, "digest"); err == nil {
		t.Fatal("model granted inherited execution")
	}
}
func TestExecutionCheckpointDoesNotResubmitCompletedCaseEdges(t *testing.T) {
	input := semanticInput()
	input.Handler = "engine.qa.execution"
	input.Validation.Execution = &coverage.ExecutionBinding{Candidate: coverage.ValidationCandidate{Identity: "candidate"}}
	input.Validation.PendingCases, input.Validation.RequiredCases = []string{"new"}, []string{"new"}
	input.Validation.Whitelist = &coverage.ApprovedWhitelist{Entries: []coverage.WhitelistEntry{{ReviewKind: coverage.ReviewBlackbox, SourceID: "alpha", PointID: "one", CaseID: "new"}, {ReviewKind: coverage.ReviewBlackbox, SourceID: "beta", PointID: "one", CaseID: "new"}}}
	input.Validation.Completed = []coverage.ExecutionRecord{{ReviewKind: coverage.ReviewBlackbox, SourceID: "alpha", PointID: "one", CaseID: "new", Result: coverage.ExecutedFail}}
	input.Validation.ExecutionFacts = []protocol.ExecutionFact{{CaseID: "new", Operations: []protocol.ExecutionOperationFact{{ActionID: "operation", Parameters: protocol.CheckParameters{QA: &protocol.QACheckParameters{Source: "case:new"}}, Evidence: protocol.CheckEvidence{Tool: "actual-tool", Environment: "actual-environment"}}}}}
	result := Result{Outcome: "PASS", Validation: &Validation{Status: "FAIL", Execution: &Execution{Records: map[string]Observation{"new": {Result: coverage.ExecutedFail, Comparison: "actual mismatch"}}}}}
	body := decodeSemantic(t, input, result).Validation
	if len(body.Execution.Records) != 1 || body.Execution.Records[0].SourceID != "beta" || body.Execution.Records[0].Result != coverage.ExecutedFail {
		t.Fatal("partial case comparison resubmitted its completed edge")
	}
}

func TestSemanticResultRejectsIssuedAndGeneratedFields(t *testing.T) {
	input := semanticInput()
	for _, raw := range []string{
		`{"outcome":"PASS","actionId":"wrong","validation":{"status":"PASS"}}`,
		`{"outcome":"PASS","provider":"wrong","validation":{"status":"PASS"}}`,
		`{"outcome":"PASS","payloadDigest":"wrong","validation":{"status":"PASS"}}`,
		`{"outcome":"PASS","validation":{"status":"PASS","binding":{"generation":9}}}`,
		`{"outcome":"PASS","validation":{"status":"PASS","manifest":{"binding":{"topologyScope":"wrong"},"cases":[],"coverage":{}}}}`,
		`{"outcome":"PASS","validation":{"status":"PASS","manifest":{"sources":["wrong"],"cases":[],"coverage":{}}}}`,
		`{"outcome":"PASS","validation":{"status":"PASS","manifest":{"cases":[{"caseId":"new","mode":"WHITEBOX","oracle":"actual"}],"coverage":{}}}}`,
		`{"outcome":"PASS","validation":{"status":"PASS","findings":[{"id":"wrong","severity":"P3","message":"real finding"}]}}`,
		`{"outcome":"PASS","validation":{"status":"PASS","suggestions":[{"id":"wrong","severity":"P3","message":"actual suggestion"}]}}`,
		`{"outcome":"PASS","validation":{"status":"PASS","suggestions":[{"decision":"ACCEPT","severity":"P3","message":"actual suggestion"}]}}`,
		`{"outcome":"PASS","validation":{"status":"PASS","review":{"scope":"FULL","sourceDecisions":{},"pointDecisions":{},"caseDecisions":{}}}}`,
		`{"outcome":"PASS","validation":{"status":"PASS","review":{"sourceDecisions":{"alpha":{"id":"wrong","status":"PASS","reason":"actual"}},"pointDecisions":{},"caseDecisions":{}}}}`,
		`{"outcome":"PASS","validation":{"status":"PASS","granularityReview":{"batches":[{"id":"batch","route":{"mode":"full","gates":[],"developmentPlan":{"digest":"forged"}}}]}}}`,
		`{"outcome":"PASS","validation":{"status":"PASS","execution":{"binding":{"candidate":{"identity":"wrong"}},"records":{}}}}`,
		`{"outcome":"PASS","validation":{"status":"PASS","execution":{"records":{"new":{"sourceId":"wrong","result":"EXECUTED_PASS"}}}}}`,
	} {
		_, err := DecodeResult(input, "result", []byte(raw), "digest")
		if err == nil || !strings.Contains(err.Error(), "unknown field") {
			t.Fatalf("fixed field accepted or hidden: %s: %v", raw, err)
		}
	}
}

func TestDesignFilesInheritOnlyWhenOmitted(t *testing.T) {
	input := semanticInput()
	input.Validation.Manifest.Cases[0].Files = []string{"original.case"}
	input.Validation.Manifest.SharedSteps[0].Files = []string{"original.shared"}
	omitted := Result{Outcome: "PASS", Validation: &Validation{Status: "PASS", Manifest: &Manifest{Cases: []Case{{CaseID: "new"}}, SharedSteps: []SharedStep{{ID: "setup", Procedure: "setup"}}}}}
	inherited := decodeSemantic(t, input, omitted).Validation.Manifest
	if !reflect.DeepEqual(inherited.Cases[0].Files, []string{"original.case"}) || !reflect.DeepEqual(inherited.SharedSteps[0].Files, []string{"original.shared"}) {
		t.Fatal("omission lost original issued files")
	}
	empty := []string{}
	omitted.Validation.Manifest.Cases[0].Files = &empty
	omitted.Validation.Manifest.SharedSteps[0].Files = &empty
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

func TestWhiteboxDesignDerivesDeliveryFromMaterialRelations(t *testing.T) {
	input := semanticInput()
	input.Validation.Manifest.Binding.ReviewKind = coverage.ReviewWhitebox
	files := []string{"analytics_test.go", "frob_test.go"}
	sharedFiles := []string{"fixture.data"}
	unused := []string{"unrelated.go"}
	value := Result{Outcome: "PASS", Validation: &Validation{Status: "PASS", Manifest: &Manifest{Cases: []Case{{CaseID: "new", Scenario: "SUCCESS", TestRef: "analytics_test.go::TestUsage", Oracle: "exact output", Files: &files, SharedSteps: []string{"setup"}}}, SharedSteps: []SharedStep{{ID: "setup", Procedure: "prepare test material", Files: &sharedFiles}, {ID: "unused", Procedure: "not referenced", Files: &unused}}, Coverage: map[string][]string{"one": {"new"}}}}}
	result := decodeSemantic(t, input, value).Validation
	if result.Development == nil || !reflect.DeepEqual(result.Development.Paths, []string{"analytics_test.go", "fixture.data", "frob_test.go"}) || result.Development.Summary != "" {
		t.Fatalf("manifest-only delivery paths lost: %+v", result.Development)
	}
	if !reflect.DeepEqual(result.Manifest.Cases[0].Files, files) || result.Binding != input.Validation.Binding {
		t.Fatal("file semantics or original binding changed")
	}
	value.Validation.Development = &Development{Paths: files, Summary: "duplicate path declaration"}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = DecodeResult(input, "result", raw, "original-raw-digest"); err == nil {
		t.Fatal("QA model supplied duplicate development fields")
	}
}
