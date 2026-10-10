//go:build !windows

package harness

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"formal-gates/internal/prompts"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/requirement"
)

func TestProductionWorkerNativeInputAndCoreResult(t *testing.T) {
	s := newCodexScenario(t)
	home := s.home
	workspace := filepath.Join(home, ".formal-gates-resources", "project")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	s.workspace = workspace
	s.peer.Workspace = workspace
	s.request["workspace"] = workspace
	catalogRoot, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := prompts.LoadPromptCatalog(catalogRoot)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := requirement.ParseRequirementProjection("requirements.md", []byte(canaryRequirementDocument))
	if err != nil {
		t.Fatal(err)
	}
	input := protocol.WorkerInput{RunID: "native-worker", Provider: "codex", VCS: "git", Handler: "engine.review.worker", Pending: protocol.PendingAction{ActionID: "review-action", AttemptID: "review-attempt", Task: runtime.TaskKey{Node: "review", Step: "review.worker"}, ReviewSeries: review.ProductReview}, RequirementDocument: doc, Review: &protocol.WorkerReviewInput{Action: review.ProductReview, RequirementRevision: doc.ContentDigest}}
	material, err := worker.Render(input, catalog)
	if err != nil {
		t.Fatal(err)
	}
	s.request["prompt"], s.request["schema"], s.request["read_only"] = material.Prompt, material.ResultSchema, true
	payload := worker.Review{Findings: []worker.Finding{}, Verification: []string{}}
	for _, key := range review.RequiredVerificationChecks() {
		payload.Verification = append(payload.Verification, "read complete confirmed delivery document: "+key)
	}
	answer, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	var domain any
	if err := json.Unmarshal(answer, &domain); err != nil {
		t.Fatal(err)
	}
	s.peer.Answer = codexPeerAnswer(codexPlain(t, material.ResultSchema), domain).(map[string]any)
	s.peer.StrictSchema = true
	source, err := s.execute(t)
	if err != nil {
		t.Fatal(err)
	}
	answerBytes, digest, _, err := NativeResultEvidence("codex", s.directory, source)
	if err != nil {
		t.Fatal(err)
	}
	event, err := worker.DecodeResult(input, "original-native-worker-result", answerBytes, digest)
	if err != nil {
		t.Fatal(err)
	}
	if event.Result == nil || event.Result.ActionID != input.Pending.ActionID || event.Result.Review.DispatchID != input.Pending.ActionID || event.Result.Review.RequirementRevision != doc.ContentDigest {
		t.Fatal("core native result was converted or rebound")
	}
	raw := readTestFileBytes(t, filepath.Join(s.directory, "answer.raw.txt"))
	rawHash := sha256.Sum256(raw)
	if event.Result.PayloadDigest != "sha256:"+hex.EncodeToString(rawHash[:]) {
		t.Fatal("final event used a shape/decoded digest instead of original native bytes")
	}
	expectedEvidence := []string{}
	for index, key := range review.RequiredVerificationChecks() {
		expectedEvidence = append(expectedEvidence, key+": "+payload.Verification[index])
	}
	if event.Result.Review.Verification.Evidence != strings.Join(expectedEvidence, "\n") || !reflect.DeepEqual(event.Result.Review.Verification.Checks, review.RequiredVerificationChecks()) {
		t.Fatal("native core evidence was rewritten")
	}
}

func TestWorkerRoleSchemasUseNativeStrictTransport(t *testing.T) {
	for _, handler := range []string{"engine.development.batch", "engine.repair", "engine.merge.resolve", "engine.qa.design", "engine.qa.review.rationale", "engine.qa.execution", "engine.gate"} {
		t.Run(handler, func(t *testing.T) {
			input := protocol.WorkerInput{Handler: handler, Pending: protocol.PendingAction{ActionID: "issued"}, Provider: "codex", Validation: &protocol.ValidationInput{}}
			schema, err := worker.ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(schema)
			wire, err := codexOutputSchema(schema)
			if err != nil {
				t.Fatal(err)
			}
			if err := codexPeerStrictSchema(codexPlain(t, wire)); err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(schema)
			if string(before) != string(after) {
				t.Fatal("native adapter changed domain result schema")
			}
		})
	}
}

func TestReadinessNativeTransportPreservesTierContracts(t *testing.T) {
	input := protocol.WorkerInput{Handler: "engine.readiness.plan", Provider: "codex", Pending: protocol.PendingAction{ActionID: "readiness", AttemptID: "attempt"}, Review: &protocol.WorkerReviewInput{Action: review.StartReadiness, RequirementRevision: "confirmed"}}
	schema, err := worker.ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	before, _ := json.Marshal(schema)
	nativeSchema, err := codexOutputSchema(schema)
	if err != nil {
		t.Fatal(err)
	}
	if err := codexPeerStrictSchema(codexPlain(t, nativeSchema)); err != nil {
		t.Fatal(err)
	}
	for _, tier := range []string{"FINAL", "CHECKPOINT"} {
		t.Run(tier, func(t *testing.T) {
			plan := nativeWorkerGranularityFixture()
			plan.VerificationTier = tier
			plan.CheckpointReason = ""
			plan.CheckpointGroups = nil
			if tier == "CHECKPOINT" {
				second := plan.Batches[0]
				second.Subtasks = []worker.Subtask{{Description: "consume the first artifact"}}
				second.DependsOn = []int{1}
				plan.Batches = append(plan.Batches, second)
				plan.CheckpointReason = "independent batches preserve frozen interfaces"
				plan.CheckpointGroups = [][]int{{1}, {2}}
			}
			payload := worker.Review{GranularityReview: plan, Findings: []worker.Finding{}, Verification: []string{}}
			for _, key := range review.RequiredVerificationChecks() {
				payload.Verification = append(payload.Verification, "checked current signed input: "+key)
			}
			body, _ := json.Marshal(payload)
			domain, err := decodeNativeJSON(body)
			if err != nil {
				t.Fatal(err)
			}
			wire := codexPeerAnswer(codexPlain(t, schema), domain)
			if getOf(getOf(getOf(wire, "result"), "granularityReview"), "verificationTier") != tier || len(listOf(getOf(getOf(getOf(wire, "result"), "granularityReview"), "batches"))) != len(plan.Batches) {
				t.Fatal("peer erased native plan fields")
			}
			projected, err := codexDomainResult(schema, wire)
			if err != nil || !valuesEqual(projected, domain) {
				t.Fatalf("tier transport changed domain evidence: %v", err)
			}
			raw, _ := json.Marshal(projected)
			event, err := worker.DecodeResult(input, "result", raw, "sha256:original")
			if err != nil {
				t.Fatal(err)
			}
			if err := protocol.ValidateGranularity(event.Result.Review.GranularityReview); err != nil {
				t.Fatal(err)
			}
		})
	}
	after, _ := json.Marshal(schema)
	if !bytes.Equal(before, after) {
		t.Fatal("native schema mutated domain contract")
	}
}

func TestQANativeTransportPreservesFiniteMachineValues(t *testing.T) {
	for _, handler := range []string{"engine.qa.design", "engine.qa.review.rationale", "engine.qa.execution"} {
		t.Run(handler, func(t *testing.T) {
			binding := coverage.ManifestBinding{SourceBinding: coverage.SourceBinding{RunID: "run", RequirementRevision: "r", SolutionRevision: "s"}, ReviewKind: coverage.ReviewBlackbox, RouteScope: "route", TopologyScope: "topology"}
			manifest := coverage.AcceptanceManifest{Binding: binding, Sources: []string{"REQ"}, Points: []coverage.AcceptancePoint{{PointID: "AC", ObservableBehavior: "public behavior", Oracle: "outcome"}}, Cases: []coverage.AcceptanceCase{{CaseID: "case", Mode: coverage.CaseBlackbox}}, Edges: []coverage.CoverageEdge{{ReviewKind: coverage.ReviewBlackbox, SourceID: "REQ", PointID: "AC", CaseID: "case"}}}
			execution := coverage.ExecutionBinding{RequiredSourcesDigest: "sources", ManifestDigest: "manifest", MapDigest: "map", WhitelistDigest: "whitelist", Candidate: coverage.ValidationCandidate{Identity: "candidate"}, Scope: coverage.ScopeFull, ExpectedCaseIDs: []string{"case"}}
			input := protocol.WorkerInput{Handler: handler, Provider: "codex", Pending: protocol.PendingAction{ActionID: "issued"}, Validation: &protocol.ValidationInput{Binding: protocol.WorkBinding{Task: runtime.TaskKey{Node: "qa", Step: "worker"}, RequirementRevision: "r"}, Manifest: manifest, ReviewScope: coverage.ScopeFull, SourcePoints: []protocol.SourcePoint{{SourceID: "REQ", PointID: "AC"}}, Execution: &execution, Whitelist: &coverage.ApprovedWhitelist{Entries: []coverage.WhitelistEntry{{ReviewKind: coverage.ReviewBlackbox, SourceID: "REQ", PointID: "AC", CaseID: "case"}}}, RequiredCases: []string{"case"}, PendingCases: []string{"case"}}}
			input.Validation.ExecutionFacts = []protocol.ExecutionFact{{CaseID: "case", Operations: []protocol.ExecutionOperationFact{{ActionID: "original-op", Parameters: protocol.CheckParameters{QA: &protocol.QACheckParameters{Source: "case:case", Operation: coverage.Operation{Kind: coverage.OperationArgv, Argv: []string{"app", ""}}, Directory: "."}}, Evidence: protocol.CheckEvidence{StdoutText: "empty", Tool: "app", Environment: "issued environment"}}}}}
			schema, err := worker.ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			var payload any
			switch handler {
			case "engine.qa.design":
				payload = &worker.Design{DesignManifest: worker.DesignManifest{Cases: []worker.DesignedCase{{Scenario: "BOUNDARY", PublicEntry: "app ''", Preconditions: "built app", Steps: "run empty word", Oracle: "empty result"}}, Coverage: [][]int{{1}}}}
			case "engine.qa.review.rationale":
				payload = &worker.CaseReview{SourceDecisions: []*worker.Decision{{Status: coverage.StatusPass, Reason: "covered"}}, PointDecisions: []*worker.Decision{{Status: coverage.StatusPass, Reason: "observed"}}, CaseDecisions: []*worker.Decision{{Status: coverage.StatusPass, Reason: "oracle valid"}}}
			case "engine.qa.execution":
				payload = &worker.ExecutionJudgments{Execution: worker.Execution{Records: []*worker.Observation{{Result: coverage.ExecutedPass, Comparison: "matches"}}}, Findings: []worker.Finding{}}
			}
			expected := protocol.ValidationResult{Binding: input.Validation.Binding, Status: "PASS"}
			switch handler {
			case "engine.qa.design":
				expected.Manifest = &coverage.AcceptanceManifest{Binding: binding, Sources: []string{"REQ"}, Points: manifest.Points, Cases: []coverage.AcceptanceCase{{CaseID: "case", Mode: coverage.CaseBlackbox, Scenario: "BOUNDARY", SharedSteps: []string{}, PublicEntry: "app ''", Preconditions: "built app", Steps: "run empty word", Oracle: "empty result"}}, Edges: manifest.Edges}
			case "engine.qa.review.rationale":
				expected.Review = &coverage.ReviewResult{Binding: binding, Scope: coverage.ScopeFull, SourceDecisions: []coverage.ReviewDecision{{ID: "REQ", Status: coverage.StatusPass, Reason: "covered"}}, PointDecisions: []coverage.ReviewDecision{{ID: "AC", Status: coverage.StatusPass, Reason: "observed"}}, CaseDecisions: []coverage.ReviewDecision{{ID: "case", Status: coverage.StatusPass, Reason: "oracle valid"}}, SharedStepDecisions: []coverage.ReviewDecision{}, SetStatus: coverage.StatusPass}
			case "engine.qa.execution":
				expected.Execution = &coverage.ExecutionReport{Binding: execution, Records: []coverage.ExecutionRecord{{ReviewKind: coverage.ReviewBlackbox, SourceID: "REQ", PointID: "AC", CaseID: "case", Result: coverage.ExecutedPass, Provenance: coverage.ProvenanceExecuted, Procedure: "run app", Observation: "empty", Comparison: "matches"}}}
				expected.Execution.Binding.ActualCaseIDs = []string{"case"}
				expected.Tool, expected.Environment = "app", "issued environment"
				var renderErr error
				expected.Execution.Records[0].Procedure, expected.Execution.Records[0].Observation, renderErr = input.Validation.ExecutionFacts[0].Render()
				if renderErr != nil {
					t.Fatal(renderErr)
				}
			}
			body, _ := json.Marshal(payload)
			domain, err := decodeNativeJSON(body)
			if err != nil {
				t.Fatal(err)
			}
			native, err := codexOutputSchema(schema)
			if err != nil {
				t.Fatal(err)
			}
			if err := codexPeerStrictSchema(codexPlain(t, native)); err != nil {
				t.Fatal(err)
			}
			wire := codexPeerAnswer(codexPlain(t, schema), domain)
			projected, err := codexDomainResult(schema, wire)
			if err != nil || !valuesEqual(projected, domain) {
				t.Fatalf("native machine values changed: got=%#v expected=%#v error=%v", projected, domain, err)
			}
			actual, _ := json.Marshal(projected)
			event, err := worker.DecodeResult(input, "result", actual, "sha256:original")
			if err != nil {
				t.Fatal(err)
			}
			if handler == "engine.qa.design" {
				actual := event.Result.Validation.Manifest
				if actual == nil || len(actual.Cases) != 1 || actual.Cases[0].CaseID == "" || actual.Cases[0].CaseID == "case" {
					t.Fatal("design did not issue a new program identity")
				}
				expected.Manifest.Cases[0].CaseID = actual.Cases[0].CaseID
				expected.Manifest.Edges = append([]coverage.CoverageEdge{}, manifest.Edges...)
				expected.Manifest.Edges[0].CaseID = actual.Cases[0].CaseID
			}
			if !reflect.DeepEqual(event.Result.Validation, &expected) {
				t.Fatalf("typed role evidence lost: got=%#v expected=%#v; manifest got=%#v expected=%#v", event.Result.Validation, expected, event.Result.Validation.Manifest, expected.Manifest)
				t.Fatal("typed role/issued binding lost", err)
			}
		})
	}
}

func TestNativeCompletionSchemaPreservesIssuedKeys(t *testing.T) {
	batch := protocol.DevelopmentBatch{ID: "owned", DoD: []protocol.DevelopmentCriterion{{ID: "behavior", Description: "declared behavior"}}, Checks: []protocol.DevelopmentCheck{{ID: "build", Command: "go build ./..."}, {ID: "behavior-test", Command: "go test ./..."}}}
	for _, role := range []string{"engine.development.batch", "engine.repair"} {
		t.Run(role, func(t *testing.T) {
			input := protocol.WorkerInput{Handler: role, Provider: "codex", Pending: protocol.PendingAction{ActionID: "issued"}, Validation: &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Batch: batch}}}
			schema, err := worker.ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			original, _ := json.Marshal(schema)
			native, err := codexOutputSchema(schema)
			if err != nil {
				t.Fatal(err)
			}
			plain := codexPlain(t, native)
			if err = codexPeerStrictSchema(plain); err != nil {
				t.Fatal(err)
			}
			completion := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)["completion"].(map[string]any)
			nativeCompletion, err := codexOutputSchema(completion)
			if err != nil {
				t.Fatal(err)
			}
			fields := objectOrEmpty(getOf(nativeCompletion, "properties"))
			dod := objectOrEmpty(fields["dod"])
			if dod["type"] != "array" || len(listOf(dod["prefixItems"])) != len(batch.DoD) || !valuesEqual(dod["minItems"], len(batch.DoD)) || !valuesEqual(dod["maxItems"], len(batch.DoD)) {
				t.Fatal("native completion must carry exactly one ordered rationale per issued DoD")
			}
			payload := worker.Development{Summary: "completed owned duty", Completion: &worker.Completion{DoD: []string{"verified"}, Handoff: "ready"}}
			body, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			domain, err := decodeNativeJSON(body)
			if err != nil {
				t.Fatal(err)
			}
			wire := codexPeerAnswer(schema, domain)
			projected, err := codexDomainResult(schema, wire)
			if err != nil || !valuesEqual(projected, domain) {
				t.Fatalf("native projection changed completion evidence: %v", err)
			}
			after, _ := json.Marshal(schema)
			if !bytes.Equal(original, after) {
				t.Fatal("native translation mutated issued schema")
			}
		})
	}
}

func TestNativeValidationStatusPreservesRoleDomains(t *testing.T) {
	for _, handler := range []string{"engine.development.batch", "engine.repair", "engine.merge.resolve", "engine.qa.design", "engine.qa.review.rationale", "engine.qa.execution", "engine.gate"} {
		t.Run(handler, func(t *testing.T) {
			input := protocol.WorkerInput{Handler: handler, Provider: "codex", Pending: protocol.PendingAction{ActionID: "issued"}, Validation: &protocol.ValidationInput{}}
			schema, err := worker.ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(schema)
			native, err := codexOutputSchema(schema)
			if err != nil {
				t.Fatal(err)
			}
			if err := codexPeerStrictSchema(codexPlain(t, native)); err != nil {
				t.Fatal(err)
			}
			body := objectOrEmpty(listOf(codexPlain(t, schema).(map[string]any)["anyOf"])[0])
			fields := objectOrEmpty(body["properties"])
			if fields["status"] != nil {
				t.Fatal("model owns a program-derived role status")
			}
			domain := map[string]any{"interruptionReason": "Required source is not readable"}
			projected, err := codexDomainResult(schema, codexPeerAnswer(codexPlain(t, schema), domain))
			if err != nil || !valuesEqual(projected, domain) {
				t.Fatal("native inability altered", err)
			}
			after, _ := json.Marshal(schema)
			if !bytes.Equal(before, after) {
				t.Fatal("native translation mutated role schema")
			}
		})
	}
}

func nativeWorkerGranularityFixture() *worker.Granularity {
	return &worker.Granularity{Reason: "one cohesive public behavior", VerificationTier: "FINAL", Batches: []worker.Batch{{Subtasks: []worker.Subtask{{Description: "implement the documented public behavior"}}, Deliverable: "public behavior matches the confirmed input contract", PublicEntry: "app", Artifacts: []string{"app.go"}, Interfaces: "existing input and output", StateBoundary: "no persistent state", Risk: "invalid inputs must be rejected", DoD: []worker.Criterion{{Description: "normal and invalid inputs satisfy their outcomes"}}, Checks: []worker.Check{{Purpose: protocol.CheckFullBuild, Command: "go build ./..."}, {Purpose: protocol.CheckFullTests, Command: "go test -timeout=0 ./..."}}, Rollback: "revert the behavior change", Recovery: "repeat from the accepted baseline", Handoff: "implementation ready for independent validation", ContextCost: "one public interface fits the implementation context"}}}
}
