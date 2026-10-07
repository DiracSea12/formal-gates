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
	"testing"
	"time"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/requirement"
	"reflect"
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
	payload.Review.Findings = []review.Finding{}
	payload.Review.Verification = review.OperatorVerification{Checks: []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"}, Evidence: "read complete confirmed delivery document"}
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
	if event.Result.Review.Verification.Evidence != payload.Review.Verification.Evidence {
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
			payload := protocol.WorkerResultPayload{ActionID: input.Pending.ActionID, Provider: input.Provider, Outcome: protocol.OutcomePass, Review: &protocol.ReviewResultEvidence{Action: review.StartReadiness, DispatchID: input.Pending.ActionID, RequirementRevision: "confirmed", Status: "PASS", GranularityReview: plan, Verification: review.OperatorVerification{Checks: review.RequiredVerificationChecks(), Evidence: "checked current signed inputs"}}}
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
			input := protocol.WorkerInput{Handler: handler, Provider: "codex", Pending: protocol.PendingAction{ActionID: "issued"}, Validation: &protocol.ValidationInput{Binding: protocol.WorkBinding{Task: runtime.TaskKey{Node: "qa", Step: "worker"}, RequirementRevision: "r"}, Manifest: coverage.AcceptanceManifest{Binding: binding}}}
			schema, err := worker.ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			result := protocol.ValidationResult{Binding: input.Validation.Binding, Status: "PASS"}
			switch handler {
			case "engine.qa.design":
				result.Manifest = &coverage.AcceptanceManifest{Binding: binding, Sources: []string{"REQ"}, Points: []coverage.AcceptancePoint{{PointID: "AC", ObservableBehavior: "public behavior", Oracle: "outcome"}}, Cases: []coverage.AcceptanceCase{{CaseID: "case", Mode: coverage.CaseBlackbox, Scenario: "BOUNDARY", PublicEntry: "app ''", Preconditions: "built app", Steps: "run empty word", Oracle: "empty result"}}, Edges: []coverage.CoverageEdge{{SourceID: "REQ", PointID: "AC", CaseID: "case"}}}
			case "engine.qa.review.rationale":
				result.Review = &coverage.ReviewResult{Binding: binding, Scope: coverage.ScopeFull, SourceDecisions: []coverage.ReviewDecision{{ID: "REQ", Status: coverage.StatusPass, Reason: "covered"}}, PointDecisions: []coverage.ReviewDecision{{ID: "AC", Status: coverage.StatusPass, Reason: "observed"}}, CaseDecisions: []coverage.ReviewDecision{{ID: "case", Status: coverage.StatusPass, Reason: "oracle valid"}}}
			case "engine.qa.execution":
				result.Execution = &coverage.ExecutionReport{Binding: coverage.ExecutionBinding{RequiredSourcesDigest: "sources", ManifestDigest: "manifest", MapDigest: "map", WhitelistDigest: "whitelist", Candidate: coverage.ValidationCandidate{Identity: "candidate"}, Scope: coverage.ScopeFull, ExpectedCaseIDs: []string{"case"}, ActualCaseIDs: []string{"case"}}, Records: []coverage.ExecutionRecord{{ReviewKind: coverage.ReviewBlackbox, SourceID: "REQ", PointID: "AC", CaseID: "case", Result: coverage.ExecutedPass, Provenance: coverage.ProvenanceExecuted, Procedure: "run app", Observation: "empty", Comparison: "matches"}}}
				result.Tool = "app"
				result.Environment = "issued workspace"
			}
			payload := protocol.WorkerResultPayload{ActionID: "issued", Provider: "codex", Outcome: protocol.OutcomePass, Validation: &result}
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
			if err != nil || !reflect.DeepEqual(event.Result.Validation, &result) {
				t.Fatal("typed role/issued binding lost", err)
			}
		})
	}
}
