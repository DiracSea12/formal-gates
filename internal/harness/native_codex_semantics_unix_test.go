//go:build !windows

package harness

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/worker"
	"formal-gates/internal/requirement"
)

func TestCodexNativeObservationPreservesSemanticContract(t *testing.T) {
	input := protocol.WorkerInput{RunID: "run", Provider: "codex", Handler: "engine.validation.observation.v2", Pending: protocol.PendingAction{ActionID: "action", AttemptID: "attempt"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "confirmed requirement"}, Validation: &protocol.ValidationInput{Observation: &protocol.ObservationInput{WaveID: "wave", Findings: []protocol.ObservationFinding{{Finding: protocol.ValidationFinding{ID: "finding"}}}, Scopes: []protocol.ObservationScope{{Mode: coverage.ReviewBlackbox, ApprovedCases: []string{"case"}}}, GateImpacts: []protocol.ObservationGateImpact{{Gate: "quality"}}}}}
	schema, err := worker.ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	domain := map[string]any{"findings": []any{map[string]any{"decision": "DISMISSED", "requirement": "scope", "normalPath": "normal", "module": "command", "rootCause": "outside requirement", "consequence": "none", "severity": "P3", "severityRationale": "outside scope", "verification": map[string]any{"scope": "checked", "severity": "checked", "causality": "checked", "reproduction": "checked", "classification": "checked"}, "reason": "outside scope"}}, "scopes": []any{map[string]any{"cases": []any{1}, "reason": "The changed public behavior affects the original case"}}, "gateImpacts": []any{map[string]any{"decision": "RERUN", "reason": "The changed implementation affects this gate"}}}
	s := newCodexScenario(t)
	s.request["schema"] = schema
	s.peer.StrictSchema = true
	s.peer.Answer = codexPlain(t, domain).(map[string]any)
	source, err := s.execute(t)
	if err != nil {
		t.Fatal(err)
	}
	raw, digest, _, err := NativeResultEvidence("codex", s.directory, source)
	if err != nil {
		t.Fatal(err)
	}
	var actual map[string]any
	if err := json.Unmarshal(raw, &actual); err != nil {
		t.Fatal(err)
	}
	if !valuesEqual(actual, domain) {
		t.Fatalf("native semantic submission changed semantic values: %s", raw)
	}
	event, err := worker.DecodeResult(input, "result", raw, digest)
	if err != nil {
		t.Fatal(err)
	}
	rows := event.Result.Validation.Observations
	if len(rows) != 3 || rows[0].Validation.Findings[0].FindingID != "finding" || rows[1].Validation.Mode != coverage.ReviewBlackbox || rows[2].Validation.Gate != "quality" {
		t.Fatal("semantic submission lost the original issued obligations")
	}
	before, err := os.ReadFile(filepath.Join(s.config, "rpc.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := NativeJobSource("codex", s.directory); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(s.config, "rpc.jsonl"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("source observation repeated the semantic native call")
	}
}

func TestCodexNativeSemanticSubmissionsAndRecovery(t *testing.T) {
	plan := nativeWorkerGranularityFixture()
	completedPlan := nativeWorkerGranularityFixture()
	completedPlan.Batches[0].NewArtifacts = nil
	batch := protocol.DevelopmentBatch{ID: "issued-batch", DoD: []protocol.DevelopmentCriterion{{ID: "issued-condition", Description: "declared behavior"}}}
	verification := []string{}
	for _, name := range review.RequiredVerificationChecks() {
		verification = append(verification, "read exact current materials: "+name)
	}
	for _, sample := range []struct {
		name   string
		input  protocol.WorkerInput
		domain any
	}{
		{"readiness", protocol.WorkerInput{Handler: "engine.readiness.plan.v2", Provider: "codex", Pending: protocol.PendingAction{ActionID: "readiness", AttemptID: "readiness-attempt"}, Review: &protocol.WorkerReviewInput{Action: review.StartReadiness, RequirementRevision: "confirmed"}}, worker.Review{Findings: []worker.Finding{}, Verification: verification, GranularityReview: plan}},
		{"confirmed-batch", protocol.WorkerInput{Handler: "engine.development.batch.v2", Provider: "codex", Pending: protocol.PendingAction{ActionID: "development", AttemptID: "development-attempt"}, Validation: &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Batch: batch}}}, worker.Development{Summary: "implemented declared behavior", Completion: &worker.Completion{DoD: []string{"complete"}, Handoff: "ready for native integration"}}},
		{"autonomous-repair", protocol.WorkerInput{Handler: "engine.repair.v2", Provider: "codex", Pending: protocol.PendingAction{ActionID: "repair", AttemptID: "repair-attempt"}, Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}}}, worker.Repair{Summary: "implemented declared behavior", Completion: &worker.Completion{DoD: []string{"complete"}, Handoff: "ready for approved checks"}, GranularityReview: completedPlan}},
	} {
		t.Run(sample.name, func(t *testing.T) {
			schema, err := worker.ResultSchema(sample.input)
			if err != nil {
				t.Fatal(err)
			}
			domain := codexPlain(t, sample.domain).(map[string]any)
			scenario := newCodexScenario(t)
			scenario.request["schema"] = schema
			if sample.name == "autonomous-repair" {
				scenario.request["read_only"] = false
				scenario.peer.Reverse = []any{
					map[string]any{"method": "item/tool/call", "params": map[string]any{"callId": "list", "tool": "workspace", "arguments": map[string]any{"action": "list"}}},
					map[string]any{"method": "item/tool/call", "params": map[string]any{"callId": "create", "tool": "workspace", "arguments": map[string]any{"action": "create", "name": "app.go", "content": "package app\n"}}},
				}
				scenario.peer.Replies = []any{
					map[string]any{"success": true, "contentItems": []any{map[string]any{"type": "inputText", "text": `{"entries":[],"listing":1}`}}},
					map[string]any{"success": true, "contentItems": []any{map[string]any{"type": "inputText", "text": `{"written":true}`}}},
				}
			}
			scenario.peer.StrictSchema = true
			scenario.peer.Answer = codexPlain(t, domain).(map[string]any)
			before, _ := json.Marshal(schema)
			source, err := scenario.execute(t)
			if err != nil {
				t.Fatal(err)
			}
			after, _ := json.Marshal(schema)
			if !bytes.Equal(before, after) {
				t.Fatal("request domain schema mutated")
			}
			answer, err := readJSONFile(filepath.Join(scenario.directory, "answer.json"))
			if err != nil || !valuesEqual(answer, domain) {
				t.Fatalf("domain result = %v %v", answer, err)
			}
			expectedSemantic, _ := json.Marshal(scenario.peer.Answer)
			semantic, err := os.ReadFile(filepath.Join(scenario.directory, "answer.json"))
			if err != nil || !bytes.Equal(semantic, expectedSemantic) {
				t.Fatal("canonical submitted semantics were rewritten")
			}
			normalized, digest, _, err := NativeResultEvidence("codex", scenario.directory, source)
			if err != nil {
				t.Fatal(err)
			}
			paths := []string{}
			if sample.name == "autonomous-repair" {
				entries, err := os.ReadDir(scenario.workspace)
				if err != nil {
					t.Fatal(err)
				}
				for _, entry := range entries {
					if !entry.IsDir() {
						paths = append(paths, entry.Name())
					}
				}
				if !bytes.Equal(readTestFileBytes(t, filepath.Join(scenario.workspace, "app.go")), []byte("package app\n")) {
					t.Fatal("original workspace tool did not create the reported artifact")
				}
			}
			event, err := worker.DecodeResultWithPaths(sample.input, "native-result", normalized, digest, paths)
			if err != nil {
				t.Fatal(err)
			}
			switch sample.name {
			case "readiness":
				if err := protocol.ValidateGranularity(event.Result.Review.GranularityReview); err != nil {
					t.Fatal(err)
				}
			case "confirmed-batch":
				if event.Result.Validation.Development.Completion.DoD[batch.DoD[0].ID] != "complete" {
					t.Fatal("completion lost the original issued DoD binding")
				}
			case "autonomous-repair":
				plan := event.Result.Validation.GranularityReview
				if err := protocol.ValidateRepairPlan(plan, true); err != nil {
					t.Fatal(err)
				}
				if event.Result.Validation.Development.Completion.DoD[plan.Batches[0].DoD[0].ID] != "complete" {
					t.Fatal("single repair plan completion lost its program-assigned criterion")
				}
			}
			rpc, err := os.ReadFile(filepath.Join(scenario.config, "rpc.jsonl"))
			if err != nil {
				t.Fatal(err)
			}
			recovered, err := codexReconcile(scenario.directory, map[string]any{"exit_code": 0})
			if err != nil || !valuesEqual(recovered, source) {
				t.Fatalf("recovered source = %v %v", recovered, err)
			}
			repeated, _ := os.ReadFile(filepath.Join(scenario.config, "rpc.jsonl"))
			repeatedSemantic, _ := os.ReadFile(filepath.Join(scenario.directory, "answer.json"))
			if !bytes.Equal(rpc, repeated) || !bytes.Equal(semantic, repeatedSemantic) {
				t.Fatal("recovery replayed work or changed canonical semantics")
			}
			if getOf(source, "usage_evidence") == nil {
				t.Fatal("original native usage evidence missing")
			}
		})
	}
}

func TestCodexNativeRepairCompletionAlternatives(t *testing.T) {
	input := protocol.WorkerInput{Handler: "engine.repair.v2", Provider: "codex", Pending: protocol.PendingAction{ActionID: "repair"}, ApprovedChecks: []protocol.DevelopmentCheck{{ID: "approved-build", Purpose: protocol.CheckFullBuild, Command: "go build ./..."}, {ID: "approved-tests", Purpose: protocol.CheckFullTests, Command: "go test -timeout=0 ./..."}, {ID: "approved-local", Command: "original-local-oracle"}}, Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}}}
	schema, err := worker.ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"small-repair", "single-completion", "split-plan"} {
		t.Run(name, func(t *testing.T) {
			result := worker.Repair{Summary: "Implemented the confirmed command"}
			if name != "small-repair" {
				plan := nativeWorkerGranularityFixture()
				plan.Batches[0].Checks = []worker.Check{{Reuse: 3}, {Command: "new-local-oracle"}}
				if name == "split-plan" {
					second := plan.Batches[0]
					second.NewArtifacts = []string{"utility.go"}
					plan.Batches = append(plan.Batches, second)
					result.Summary = ""
				} else {
					plan.Batches[0].NewArtifacts = nil
					result.Completion = &worker.Completion{DoD: []string{"Implemented"}, Handoff: "Ready for approved checks"}
				}
				result.GranularityReview = plan
			}
			domain := codexPlain(t, result).(map[string]any)
			s := newCodexScenario(t)
			s.request["schema"], s.peer.StrictSchema = schema, true
			s.peer.Answer = codexPlain(t, domain).(map[string]any)
			source, err := s.execute(t)
			if err != nil {
				t.Fatal(err)
			}
			answer, err := readJSONFile(filepath.Join(s.directory, "answer.json"))
			if err != nil || !valuesEqual(answer, domain) {
				t.Fatalf("native repair altered its semantic branch: %v %v", answer, err)
			}
			normalized, digest, _, err := NativeResultEvidence("codex", s.directory, source)
			if err != nil {
				t.Fatal(err)
			}
			event, err := worker.DecodeResultWithPaths(input, "result", normalized, digest, []string{})
			if err != nil {
				t.Fatalf("normalized repair no longer satisfies the domain contract: %v", err)
			}
			if name != "small-repair" {
				batch := event.Result.Validation.GranularityReview.Batches[0]
				if !reflect.DeepEqual(batch.Checks[:3], input.ApprovedChecks) || batch.Checks[3].Command != "new-local-oracle" {
					t.Fatal("native check union changed original approved or authored values")
				}
				if name == "single-completion" && len(batch.Artifacts) != 0 {
					t.Fatal("native repair invented artifacts outside its supplied original facts")
				}
			}
			if err := codexVerifySource(s.directory, source); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(filepath.Join(s.directory, "answer.json"))
			if err != nil {
				t.Fatal(err)
			}
			recovered, err := codexReconcile(s.directory, map[string]any{"exit_code": 0})
			if err != nil || !valuesEqual(recovered, source) {
				t.Fatalf("native repair recovery changed source: %v %v", recovered, err)
			}
			after, err := os.ReadFile(filepath.Join(s.directory, "answer.json"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("repair recovery rewrote the native original output")
			}
		})
	}
}
