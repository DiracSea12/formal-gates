package worker

import (
	"encoding/json"
	"strings"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

func TestCompletedSemanticFailureHasSuccessfulTransport(t *testing.T) {
	input := semanticInput()
	input.Handler = "engine.gate.v2"
	result := decodeSemantic(t, input, Gate{Findings: []Finding{{Severity: "P1", Message: "Actual public contract failure"}}})
	if result.Outcome != protocol.OutcomePass || result.FailureClass != "" || result.Validation.Status != "FAIL" || len(result.Validation.Findings) != 1 {
		t.Fatalf("semantic defect became a transport failure: %+v", result)
	}
	input.Handler = "engine.review.worker.v2"
	input.Validation = nil
	input.Review = &protocol.WorkerReviewInput{Action: review.ProductReview}
	body := Review{Findings: []Finding{{Severity: "P1", Message: "Requirement contradicts public behavior"}}, Verification: []string{}}
	for _, key := range review.RequiredVerificationChecks() {
		body.Verification = append(body.Verification, "Observed "+key)
	}
	result = decodeSemantic(t, input, body)
	if result.Outcome != protocol.OutcomePass || result.Review.Status != "FAIL" || result.FailureClass != "" {
		t.Fatal("review verdict controls transport")
	}
}

func TestSemanticInabilityNeedsNoCompletedRolePlaceholders(t *testing.T) {
	for _, handler := range []string{"engine.review.worker.v2", "engine.readiness.plan.v3", "engine.development.batch.v3", "engine.repair.v3", "engine.merge.resolve.v2", "engine.qa.design.v2", "engine.qa.review.rationale.v2", "engine.qa.execution.v2", "engine.repair.verify.v2", "engine.gate.v2"} {
		t.Run(handler, func(t *testing.T) {
			input := semanticInput()
			input.Handler = handler
			if handler == "engine.review.worker.v2" || handler == "engine.readiness.plan.v3" {
				input.Validation = nil
				input.Review = &protocol.WorkerReviewInput{Action: review.ProductReview}
				if handler == "engine.readiness.plan.v3" {
					input.Review.Action = review.StartReadiness
				}
			}
			if handler == "engine.repair.verify.v2" {
				input.Validation.Repair = &protocol.RepairInput{}
			}
			result := decodeSemantic(t, input, Interruption{InterruptionReason: "The issued source cannot be read, so the duty could not be completed"})
			if result.Outcome != protocol.OutcomeRuntimeError || result.FailureClass != authoring.FailureUserActionRequired || result.InterruptionReason == "" || result.Review != nil || result.Validation != nil {
				t.Fatalf("inability became a completed judgment: %+v", result)
			}
			schema, err := ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			fields := schema["anyOf"].([]any)[1].(map[string]any)["properties"].(map[string]any)
			if fields["outcome"] != nil || fields["failureClass"] != nil {
				t.Fatal("model classifies native transport")
			}
		})
	}
}

func TestExecutionInabilityRetainsActualCompletedComparisons(t *testing.T) {
	input := semanticInput()
	input.Handler = "engine.qa.execution.v2"
	input.Validation.Execution = &coverage.ExecutionBinding{}
	input.Validation.Whitelist = &coverage.ApprovedWhitelist{Entries: []coverage.WhitelistEntry{{ReviewKind: coverage.ReviewBlackbox, SourceID: "alpha", PointID: "one", CaseID: "new"}, {ReviewKind: coverage.ReviewBlackbox, SourceID: "beta", PointID: "two", CaseID: "remaining"}}}
	input.Validation.RequiredCases = []string{"new", "remaining"}
	input.Validation.PendingCases = []string{"new", "remaining"}
	input.Validation.ExecutionFacts = []protocol.ExecutionFact{{CaseID: "new", Operations: []protocol.ExecutionOperationFact{{ActionID: "original-op", Parameters: protocol.CheckParameters{QA: &protocol.QACheckParameters{Source: "case:new"}}, Evidence: protocol.CheckEvidence{Tool: "native-tool", Environment: "native-env", StdoutText: "actual output"}}}}}
	result := decodeSemantic(t, input, ExecutionInterruption{InterruptionReason: "Remaining oracle needs an unavailable fixture", Records: []*Observation{{Result: coverage.ExecutedFail, Comparison: "Actual output does not match the approved oracle"}}})
	if result.Outcome != protocol.OutcomeRuntimeError || !result.Validation.Checkpoint || result.Validation.Status != "FAIL" || len(result.Validation.Execution.Records) != 1 || result.Validation.Execution.Records[0].Provenance != coverage.ProvenanceExecuted || len(result.Validation.Execution.NotExecuted) != 1 {
		t.Fatalf("partial evidence lost: %+v", result)
	}
	if result.Validation.Tool != "native-tool" || result.Validation.Environment != "native-env" {
		t.Fatal("native identities were model authored")
	}
	raw, _ := json.Marshal(ExecutionJudgments{Execution: Execution{Records: []*Observation{{Result: coverage.ExecutedPass, Comparison: "matches"}}}})
	event, err := DecodeResultWithPaths(input, "partial", raw, "raw-digest", []string{})
	if err != nil {
		t.Fatal(err)
	}
	if !event.Result.Validation.Checkpoint || event.Result.Validation.Status == "PASS" {
		t.Fatal("unfinished coverage gained aggregate PASS")
	}
}

func TestSemanticRoleBodyRejectsMechanicalEnvelopeAndMixedInability(t *testing.T) {
	input := semanticInput()
	input.Handler = "engine.gate.v2"
	for _, raw := range []string{`{"outcome":"PASS","validation":{"status":"FAIL","findings":[]}}`, `{"status":"FAIL","findings":[],"failureClass":"BUSINESS_REJECT"}`, `{"status":"FAIL","findings":[],"interruptionReason":"cannot finish"}`, `{"interruptionReason":" "}`, `{"status":"PASS"}`, `{"status":"PASS","findings":[]} {"interruptionReason":"extra"}`} {
		if _, err := DecodeResultWithPaths(input, "result", []byte(raw), "actual-native-digest", []string{}); err == nil {
			t.Fatalf("invalid body accepted: %s", raw)
		}
	}
	input.Handler = "engine.qa.execution.v2"
	input.Validation.Execution = &coverage.ExecutionBinding{}
	input.Validation.Whitelist = &coverage.ApprovedWhitelist{}
	input.Validation.PendingCases = []string{"new"}
	input.Validation.RequiredCases = []string{"new"}
	input.Validation.ExecutionFacts = []protocol.ExecutionFact{{CaseID: "new", Operations: []protocol.ExecutionOperationFact{{ActionID: "op", Parameters: protocol.CheckParameters{QA: &protocol.QACheckParameters{Source: "case:new"}}, Evidence: protocol.CheckEvidence{Tool: "native", Environment: "native"}}}}}
	if _, err := DecodeResultWithPaths(input, "result", []byte(`{"records":[{"result":"UNKNOWN","comparison":"No actual judgment"}]}`), "actual-native-digest", []string{}); err == nil || !strings.Contains(err.Error(), "complete oracle comparison") {
		t.Fatal("unknown result accepted without native facts", err)
	}
}
