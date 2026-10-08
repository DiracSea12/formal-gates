//go:build !windows

package harness

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"formal-gates/internal/prompts"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/facade"
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
	body, err := json.MarshalIndent(material, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	body = append(body, '\n')
	hash := sha256.Sum256(body)
	materialPath := filepath.Join(home, "material.json")
	if err := os.WriteFile(materialPath, body, 0600); err != nil {
		t.Fatal(err)
	}
	ref := facade.WorkerMaterial{ActionID: input.Pending.ActionID, AttemptID: input.Pending.AttemptID, Path: materialPath, Digest: "sha256:" + hex.EncodeToString(hash[:]), ReadOnly: true}
	r, w := io.Pipe()
	defer r.Close()
	defer w.Close()
	c := &engineCanary{t: t, nativeHost: "codex", localPeer: true, home: home, project: workspace, jobs: t.TempDir(), worker: true, binding: objectOrEmpty(s.request["binding"]), deadline: time.Now().Add(time.Minute), run: facade.Run{RunID: input.RunID, PendingActions: map[string]protocol.PendingAction{input.Pending.ActionID: input.Pending}, WorkerMaterials: map[string]facade.WorkerMaterial{input.Pending.ActionID: ref}}, approvalInput: bufio.NewReader(r), approvalOutput: canaryLocalApproval{w}}
	payload := material.ResultTemplate
	payload.Outcome = protocol.OutcomePass
	payload.Review.Status = "PASS"
	payload.Review.Findings = []worker.Finding{}
	for _, key := range review.RequiredVerificationChecks() {
		payload.Review.Verification[key] = "read complete confirmed delivery document: " + key
	}
	answer, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(answer, &s.peer.Answer); err != nil {
		t.Fatal(err)
	}
	calls := 0
	c.execute = func(request any, directory string, probe bool) (map[string]any, error) {
		calls++
		if probe || getOf(request, "prompt") != material.Prompt || !valuesEqual(getOf(request, "schema"), codexPlain(t, material.ResultSchema)) || getOf(request, "workspace") != workspace || getOf(request, "read_only") != true {
			t.Fatal("native call did not consume exact production prompt/schema/role input")
		}
		s.request = request.(map[string]any)
		s.directory = directory
		s.peer.StrictSchema = true
		return s.execute(t)
	}
	event := c.dispatchProduction(input.Pending.ActionID, material)
	if calls != 1 || event.Result == nil || event.Result.ActionID != input.Pending.ActionID || event.Result.Review.DispatchID != input.Pending.ActionID || event.Result.Review.RequirementRevision != doc.ContentDigest {
		t.Fatal("complete core native payload was converted or rebound")
	}
	raw := readTestFileBytes(t, filepath.Join(c.transcript, "answer.raw.txt"))
	rawHash := sha256.Sum256(raw)
	if event.Result.PayloadDigest != "sha256:"+hex.EncodeToString(rawHash[:]) {
		t.Fatal("final event used a shape/decoded digest instead of original native bytes")
	}
	expectedEvidence := []string{}
	for _, key := range review.RequiredVerificationChecks() {
		expectedEvidence = append(expectedEvidence, key+": "+payload.Review.Verification[key])
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

func bindCanaryReviewMaterial(t *testing.T, c *engineCanary, pending protocol.PendingAction, content []byte) {
	t.Helper()
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	catalog, err := prompts.LoadPromptCatalog(root)
	if err != nil {
		t.Fatal(err)
	}
	doc, err := requirement.ParseRequirementProjection(filepath.Join(c.project, "requirements.md"), content)
	if err != nil {
		t.Fatal(err)
	}
	input := protocol.WorkerInput{RunID: c.run.RunID, Provider: c.provider(), VCS: "git", Handler: "engine.review.worker", Pending: pending, RequirementDocument: doc, Review: &protocol.WorkerReviewInput{Action: pending.ReviewSeries, RequirementRevision: doc.ContentDigest}}
	material, err := worker.Render(input, catalog)
	if err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(material)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "worker-material.json")
	if err := os.WriteFile(path, body, 0600); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(body)
	c.run.WorkerMaterials = map[string]facade.WorkerMaterial{pending.ActionID: {ActionID: pending.ActionID, AttemptID: pending.AttemptID, Path: path, Digest: "sha256:" + hex.EncodeToString(hash[:]), ReadOnly: material.ReadOnly}}
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
			plan := canaryLocalPlan(false)
			plan.VerificationTier = tier
			plan.CheckpointReason = ""
			plan.CheckpointGroups = nil
			if tier == "CHECKPOINT" {
				second := plan.Batches[0]
				second.ID = "second"
				second.Subtasks = []protocol.DevelopmentSubtask{{ID: "second-task", Description: "consume the first artifact"}}
				second.DependsOn = []string{plan.Batches[0].ID}
				plan.Batches = append(plan.Batches, second)
				plan.CheckpointReason = "independent batches preserve frozen interfaces"
				plan.CheckpointGroups = [][]string{{plan.Batches[0].ID}, {second.ID}}
			}
			payload := worker.Result{Outcome: protocol.OutcomePass, Review: &worker.Review{Status: "PASS", GranularityReview: canarySemanticPlan(plan), Findings: []worker.Finding{}, Verification: map[string]string{}}}
			for _, key := range review.RequiredVerificationChecks() {
				payload.Review.Verification[key] = "checked current signed input: " + key
			}
			body, _ := json.Marshal(payload)
			domain, err := decodeNativeJSON(body)
			if err != nil {
				t.Fatal(err)
			}
			wire := codexPeerAnswer(codexPlain(t, schema), domain)
			if getOf(getOf(getOf(wire, "review"), "granularityReview"), "verificationTier") != tier || len(listOf(getOf(getOf(getOf(wire, "review"), "granularityReview"), "batches"))) != len(plan.Batches) {
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
			result := worker.Validation{Status: "PASS"}
			switch handler {
			case "engine.qa.design":
				result.Manifest = &worker.Manifest{Cases: []worker.Case{{CaseID: "case", Scenario: "BOUNDARY", PublicEntry: "app ''", Preconditions: "built app", Steps: "run empty word", Oracle: "empty result"}}, Coverage: map[string][]string{"AC": {"case"}}}
			case "engine.qa.review.rationale":
				result.Review = &worker.CaseReview{SourceDecisions: map[string]worker.Decision{"REQ": {Status: coverage.StatusPass, Reason: "covered"}}, PointDecisions: map[string]worker.Decision{"AC": {Status: coverage.StatusPass, Reason: "observed"}}, CaseDecisions: map[string]worker.Decision{"case": {Status: coverage.StatusPass, Reason: "oracle valid"}}}
			case "engine.qa.execution":
				result.Execution = &worker.Execution{Records: map[string]worker.Observation{"case": {Result: coverage.ExecutedPass, Comparison: "matches"}}}
			}
			expected := protocol.ValidationResult{Binding: input.Validation.Binding, Status: "PASS"}
			switch handler {
			case "engine.qa.design":
				expected.Manifest = &coverage.AcceptanceManifest{Binding: binding, Sources: []string{"REQ"}, Points: manifest.Points, Cases: []coverage.AcceptanceCase{{CaseID: "case", Mode: coverage.CaseBlackbox, Scenario: "BOUNDARY", PublicEntry: "app ''", Preconditions: "built app", Steps: "run empty word", Oracle: "empty result"}}, Edges: manifest.Edges}
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
			payload := worker.Result{Outcome: protocol.OutcomePass, Validation: &result}
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
				t.Fatal("native machine values changed", err)
			}
			actual, _ := json.Marshal(projected)
			event, err := worker.DecodeResult(input, "result", actual, "sha256:original")
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(event.Result.Validation, &expected) {
				t.Fatalf("typed role evidence lost: got=%+v expected=%+v", event.Result.Validation, expected)
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
			completion := schema["properties"].(map[string]any)["validation"].(map[string]any)["properties"].(map[string]any)["development"].(map[string]any)["properties"].(map[string]any)["completion"].(map[string]any)
			nativeCompletion, err := codexOutputSchema(completion)
			if err != nil {
				t.Fatal(err)
			}
			fields := objectOrEmpty(getOf(nativeCompletion, "properties"))
			for name, ids := range map[string][]string{"dod": {"behavior"}} {
				field := objectOrEmpty(fields[name])
				properties := objectOrEmpty(field["properties"])
				if field["type"] != "object" || field["additionalProperties"] != false || len(properties) != len(ids) || len(listOf(field["required"])) != len(ids) {
					t.Fatalf("native %s did not retain its closed object", name)
				}
				for _, id := range ids {
					if properties[id] == nil {
						t.Fatalf("native %s omitted %s", name, id)
					}
				}
			}
			payload := worker.Result{Outcome: protocol.OutcomePass, Validation: &worker.Validation{Status: "PASS", Development: &protocol.DevelopmentEvidence{Paths: []string{"command.go"}, Summary: "completed owned duty", Completion: &protocol.DevelopmentCompletion{DoD: map[string]string{"behavior": "verified"}, Handoff: "ready"}}}}
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
	for _, role := range []struct {
		handler  string
		statuses []string
	}{{"engine.development.batch", []string{"PASS"}}, {"engine.repair", []string{"PASS"}}, {"engine.merge.resolve", []string{"PASS"}}, {"engine.qa.design", []string{"PASS"}}, {"engine.gate", []string{"PASS", "FAIL"}}, {"engine.qa.review.rationale", nil}, {"engine.qa.execution", nil}} {
		t.Run(role.handler, func(t *testing.T) {
			input := protocol.WorkerInput{Handler: role.handler, Provider: "codex", Pending: protocol.PendingAction{ActionID: "issued"}, Validation: &protocol.ValidationInput{}}
			schema, err := worker.ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			before, _ := json.Marshal(schema)
			native, err := codexOutputSchema(schema)
			if err != nil {
				t.Fatal(err)
			}
			plain := objectOrEmpty(codexPlain(t, native))
			if err = codexPeerStrictSchema(plain); err != nil {
				t.Fatal(err)
			}
			validation := objectOrEmpty(getOf(getOf(plain, "properties"), "validation"))
			branches := listOf(validation["anyOf"])
			if len(branches) != 2 {
				t.Fatal("optional validation transport shape changed")
			}
			status := objectOrEmpty(getOf(getOf(branches[0], "properties"), "status"))
			expected := []any{}
			for _, literal := range role.statuses {
				expected = append(expected, literal)
			}
			if role.statuses == nil {
				if _, present := status["enum"]; present {
					t.Fatal("native adapter invented a business status restriction")
				}
			} else if !valuesEqual(status["enum"], expected) {
				t.Fatalf("native status domain differs: %v", status)
			}
			values := role.statuses
			if values == nil {
				values = []string{"COMPLETED"}
			}
			for _, literal := range values {
				payload := worker.Result{Outcome: protocol.OutcomePass, Validation: &worker.Validation{Status: literal}}
				body, err := json.Marshal(payload)
				if err != nil {
					t.Fatal(err)
				}
				domain, err := decodeNativeJSON(body)
				if err != nil {
					t.Fatal(err)
				}
				projected, err := codexDomainResult(schema, codexPeerAnswer(schema, domain))
				if err != nil || !valuesEqual(projected, domain) {
					t.Fatalf("native projection rewrote status: %v", err)
				}
			}
			after, _ := json.Marshal(schema)
			if !bytes.Equal(before, after) {
				t.Fatal("native translation mutated role schema")
			}
		})
	}
}
