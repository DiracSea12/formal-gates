package harness

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/worker"
)

func zcodeSchemaRoleInputs() map[string]protocol.WorkerInput {
	inputs := map[string]protocol.WorkerInput{}
	for _, handler := range []string{"engine.development.batch.v4", "engine.repair.v5", "engine.merge.resolve.v3", "engine.qa.design.v4", "engine.qa.review.rationale.v2", "engine.qa.execution.v3", "engine.validation.observation.v2", "engine.repair.verify.v2", "engine.gate.v2"} {
		inputs[handler] = protocol.WorkerInput{Handler: handler, Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}, Observation: &protocol.ObservationInput{}}}
	}
	for _, action := range []review.Action{review.ProductReview, review.StartReadiness} {
		inputs[string(action)] = protocol.WorkerInput{Handler: "engine.review.worker.v2", Review: &protocol.WorkerReviewInput{Action: action}}
	}
	inputs[string(review.StartReadiness)] = protocol.WorkerInput{Handler: "engine.readiness.plan.v4", Review: &protocol.WorkerReviewInput{Action: review.StartReadiness}, ApprovedPlan: canaryLocalPlan(false), AvailableRouteCandidates: []string{"quality"}}
	for _, handler := range []string{"engine.development.batch.v4", "engine.repair.v5"} {
		inputs[handler+"/issued"] = protocol.WorkerInput{Handler: handler, Validation: &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "owned", DoD: []protocol.DevelopmentCriterion{{ID: "behavior", Description: "public behavior"}}}}, Repair: &protocol.RepairInput{}}}
	}
	inputs["execution-commands"] = protocol.WorkerInput{Handler: "engine.qa.execution.v3", Validation: &protocol.ValidationInput{CommandCases: []string{"case"}}}
	inputs["repair-case-impact"] = protocol.WorkerInput{Handler: "engine.repair.v5", ApprovedChecks: []protocol.DevelopmentCheck{{ID: "build", Purpose: protocol.CheckFullBuild, Command: "project-build"}}, Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}, Contract: &coverage.CoverageContract{Manifests: []coverage.AcceptanceManifest{{Cases: []coverage.AcceptanceCase{{CaseID: "case"}}}}}}}
	inputs["observation-issued"] = protocol.WorkerInput{Handler: "engine.validation.observation.v2", Validation: &protocol.ValidationInput{Observation: &protocol.ObservationInput{Findings: []protocol.ObservationFinding{{Finding: protocol.ValidationFinding{ID: "finding"}}}, Scopes: []protocol.ObservationScope{{Mode: coverage.ReviewBlackbox, ApprovedCases: []string{"case"}}}, GateImpacts: []protocol.ObservationGateImpact{{Gate: "quality"}}, Suggestions: []protocol.ObservationSuggestion{{Suggestion: protocol.QASuggestion{ID: "suggestion"}, Manifest: coverage.AcceptanceManifest{Cases: []coverage.AcceptanceCase{{CaseID: "case"}}}}}}}}
	for _, kind := range []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox} {
		for _, affected := range []bool{false, true} {
			manifest := coverage.AcceptanceManifest{Binding: coverage.ManifestBinding{ReviewKind: kind}, Cases: []coverage.AcceptanceCase{{CaseID: "case", Operations: []coverage.Operation{{Kind: coverage.OperationArgv, Argv: []string{"app"}}}}}}
			validation := &protocol.ValidationInput{Manifest: manifest}
			name := string(kind) + "-design"
			if affected {
				name += "-affected"
				validation.AffectedCases = []string{"case"}
			}
			inputs[name] = protocol.WorkerInput{Handler: "engine.qa.design.v4", Validation: validation}
		}
	}
	return inputs
}

func zcodeSchemaToolFixture(t *testing.T, directory, provider string, input protocol.WorkerInput) string {
	t.Helper()
	if directory == "" {
		directory = filepath.Join(t.TempDir(), "action", "call")
	}
	if err := os.MkdirAll(directory, 0700); err != nil {
		t.Fatal(err)
	}
	workspace := filepath.Join(filepath.Dir(filepath.Dir(directory)), "workspace")
	if err := os.MkdirAll(workspace, 0700); err != nil {
		t.Fatal(err)
	}
	input.Provider = provider
	input.Pending = protocol.PendingAction{ActionID: "original-action", AttemptID: "original-attempt"}
	schema, err := worker.ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	materialPath := filepath.Join(directory, "material.json")
	if err := writeAtomicJSON(materialPath, worker.Material{Input: input, ReadOnly: true, ResultSchema: schema}); err != nil {
		t.Fatal(err)
	}
	request := map[string]any{"provider": provider, "workspace": workspace, "read_only": true, "schema": schema, "engine": map[string]any{"workerMaterial": map[string]any{"path": materialPath, "actionId": input.Pending.ActionID, "attemptId": input.Pending.AttemptID}}}
	if err := writeAtomicJSON(filepath.Join(directory, "native-request.json"), request); err != nil {
		t.Fatal(err)
	}
	return directory
}

func zcodeSchemaToolRequest(t *testing.T, directory, method string, params any) any {
	t.Helper()
	request, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		t.Fatal(err)
	}
	var out, errout bytes.Buffer
	services := ExecutionServices{Admit: nativeToolAdmission}
	if code := NativeTools(directory, bytes.NewReader(request), &out, &errout, services); code != 0 {
		t.Fatalf("original MCP exit %d: %s", code, &errout)
	}
	reply, err := decodeNativeJSON(out.Bytes())
	if err != nil || getOf(reply, "error") != nil {
		t.Fatalf("original MCP response: %s %v", out.Bytes(), err)
	}
	return getOf(reply, "result")
}

func TestZcodeSemanticToolDiscoveryPreservesEveryRoleContract(t *testing.T) {
	for name, input := range zcodeSchemaRoleInputs() {
		t.Run(name, func(t *testing.T) {
			schema, err := worker.ResultSchema(input)
			if err != nil {
				t.Fatal(err)
			}
			original, _ := json.Marshal(schema)
			for _, provider := range []string{"zcode", "codex", "claude", "cursor", "dsh", "workbuddy", ""} {
				t.Run(provider, func(t *testing.T) {
					directory := zcodeSchemaToolFixture(t, "", provider, input)
					materialBefore := readTestFileBytes(t, filepath.Join(directory, "material.json"))
					requestBefore := readTestFileBytes(t, filepath.Join(directory, "native-request.json"))
					declared := listOf(getOf(zcodeSchemaToolRequest(t, directory, "tools/list", nil), "tools"))
					if len(declared) != 2 || getOf(declared[0], "name") != nativeWorkspaceTool || getOf(declared[1], "name") != nativeSemanticTool {
						t.Fatal("original tool surface changed")
					}
					wire := objectOrEmpty(getOf(declared[1], "inputSchema"))
					if provider != "zcode" {
						actual, _ := json.Marshal(wire)
						if !bytes.Equal(original, actual) {
							t.Fatal("another provider's semantic schema bytes changed")
						}
					} else {
						loaded, err := newNativeWorkspaceTools(directory)
						if err != nil {
							t.Fatal(err)
						}
						before, _ := json.Marshal(loaded.material.ResultSchema)
						loaded.schemas()
						after, _ := json.Marshal(loaded.material.ResultSchema)
						if !bytes.Equal(before, after) {
							t.Fatal("discovery mutated the in-memory original material schema")
						}
						fields := objectOrEmpty(wire["properties"])
						originalBranches, _ := json.Marshal(schema["anyOf"])
						declaredBranches, _ := json.Marshal(wire["anyOf"])
						if len(fields) == 0 || !bytes.Equal(originalBranches, declaredBranches) || !valuesEqual(wire["required"], schema["required"]) || wire["additionalProperties"] != schema["additionalProperties"] {
							t.Fatal("original semantic alternatives or root constraints changed")
						}
						wantNames := map[string]bool{}
						for _, branch := range listOf(schema["anyOf"]) {
							if getOf(branch, "additionalProperties") != false {
								t.Fatal("role branch is not a closed original object")
							}
							for field := range objectOrEmpty(getOf(branch, "properties")) {
								wantNames[field] = true
								if fields[field] == nil {
									t.Fatalf("model tool catalogue omitted original %s", field)
								}
								originalField := getOf(getOf(branch, "properties"), field)
								if !zcodeSchemaContainsChoice(fields[field], codexPlain(t, originalField)) {
									t.Fatalf("model field %s narrowed an original branch option", field)
								}
							}
						}
						if len(fields) != len(wantNames) {
							t.Fatal("model tool catalogue gained unowned fields")
						}
					}
					if !bytes.Equal(materialBefore, readTestFileBytes(t, filepath.Join(directory, "material.json"))) || !bytes.Equal(requestBefore, readTestFileBytes(t, filepath.Join(directory, "native-request.json"))) {
						t.Fatal("discovery mutated original material or request")
					}
					if _, err := os.Stat(filepath.Join(directory, nativeToolCallsFile)); !os.IsNotExist(err) {
						t.Fatal("discovery manufactured a tool-call fact", err)
					}
				})
			}
		})
	}
}

func TestZcodeSemanticToolSubmissionKeepsOriginalRoleAcceptance(t *testing.T) {
	for name, input := range zcodeSchemaRoleInputs() {
		t.Run(name, func(t *testing.T) {
			directory := zcodeSchemaToolFixture(t, "", "zcode", input)
			zcodeSchemaToolRequest(t, directory, "tools/list", nil)
			for _, raw := range []string{`{"status":"PASS"}`, `{"interruptionReason":"source unavailable","actionId":"fabricated"}`, `{"interruptionReason":" "}`} {
				var value any
				if err := json.Unmarshal([]byte(raw), &value); err != nil {
					t.Fatal(err)
				}
				if err := worker.ValidateSemanticBody(input, []byte(raw)); err == nil {
					t.Fatal("negative control is valid in the original role")
				}
				result := zcodeSchemaToolRequest(t, directory, "tools/call", map[string]any{"name": nativeSemanticTool, "arguments": value})
				if getOf(result, "isError") != true {
					t.Fatal("transport bypassed original role rejection")
				}
			}
			value := map[string]any{"interruptionReason": "The issued original source is unavailable"}
			body, _ := json.Marshal(value)
			if err := worker.ValidateSemanticBody(input, body); err != nil {
				t.Fatal("positive control rejected by original worker", err)
			}
			for range 2 {
				result := zcodeSchemaToolRequest(t, directory, "tools/call", map[string]any{"name": nativeSemanticTool, "arguments": value})
				if getOf(result, "isError") != false {
					t.Fatal("original inability or identical replay rejected", result)
				}
			}
			stored, err := submittedSemantics(directory)
			if err != nil || !bytes.Equal(stored, body) {
				t.Fatal("original semantic body bytes changed", err)
			}
			before := readTestFileBytes(t, filepath.Join(directory, nativeToolCallsFile))
			zcodeSchemaToolRequest(t, directory, "tools/list", nil)
			if !bytes.Equal(before, readTestFileBytes(t, filepath.Join(directory, nativeToolCallsFile))) {
				t.Fatal("discovery changed original accepted call evidence")
			}
		})
	}
}

func TestZcodeSemanticToolShowsDistinctRepairSummaryChoices(t *testing.T) {
	input := protocol.WorkerInput{Handler: "engine.repair.v5", Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}}}
	directory := zcodeSchemaToolFixture(t, "", "zcode", input)
	tools := listOf(getOf(zcodeSchemaToolRequest(t, directory, "tools/list", nil), "tools"))
	schema := getOf(tools[1], "inputSchema")
	field := getOf(getOf(schema, "properties"), "summary")
	if !zcodeSchemaContainsChoice(field, map[string]any{"type": "string"}) || !zcodeSchemaContainsChoice(field, map[string]any{"type": "string", "minLength": 1}) {
		t.Fatal("repair proposal lost its original unconstrained summary choice")
	}
	for _, body := range []string{`{"summary":""}`, `{"summary":"progress","remainingWork":"complete the original duty"}`} {
		if err := worker.ValidateSemanticBody(input, []byte(body)); err != nil {
			t.Fatal("original repair summary witness rejected", err)
		}
	}
}

func zcodeSchemaContainsChoice(advertised, original any) bool {
	if valuesEqual(advertised, original) {
		return true
	}
	for _, choice := range listOf(getOf(advertised, "anyOf")) {
		if zcodeSchemaContainsChoice(choice, original) {
			return true
		}
	}
	return false
}

func TestZcodeSemanticToolContinuationRetainsOriginalFactsAndCompletedBody(t *testing.T) {
	for _, handler := range []string{"engine.development.batch.v4", "engine.repair.v5"} {
		t.Run(handler, func(t *testing.T) {
			input := protocol.WorkerInput{RunID: "run", Provider: "zcode", Handler: handler, Pending: protocol.PendingAction{ActionID: "original-action", AttemptID: "original-attempt"}, Validation: &protocol.ValidationInput{Development: &protocol.DevelopmentInput{Batch: protocol.DevelopmentBatch{ID: "owned"}}, Repair: &protocol.RepairInput{}}}
			directory := zcodeSchemaToolFixture(t, "", "zcode", input)
			partial := map[string]any{"summary": "Mapped the public command", "remainingWork": "Complete its original behavior"}
			partialBytes, _ := json.Marshal(partial)
			zcodeSchemaToolRequest(t, directory, "tools/list", nil)
			result := zcodeSchemaToolRequest(t, directory, "tools/call", map[string]any{"name": nativeSemanticTool, "arguments": partial})
			if getOf(result, "isError") != false {
				t.Fatal("original continuation rejected", result)
			}
			accepted, err := worker.DecodeResultWithPaths(input, "partial", partialBytes, "original-partial", []string{})
			if err != nil || accepted.Result.Validation.Status != "CONTINUE" {
				t.Fatal("continuation result changed", err)
			}
			prior := &protocol.WorkerResult{Digest: "original-partial", Validation: accepted.Result.Validation}
			if handler == "engine.development.batch.v4" {
				input.Validation.Development.Result = prior
			} else {
				input.Validation.Repair.Result = prior
			}
			continued := zcodeSchemaToolFixture(t, filepath.Join(filepath.Dir(directory), "continued"), "zcode", input)
			zcodeSchemaToolRequest(t, continued, "tools/list", nil)
			final := map[string]any{"summary": "Completed the public command", "completion": map[string]any{"dod": []any{}, "handoff": "Ready for approved checks"}}
			finalBytes, _ := json.Marshal(final)
			result = zcodeSchemaToolRequest(t, continued, "tools/call", map[string]any{"name": nativeSemanticTool, "arguments": final})
			if getOf(result, "isError") != false {
				t.Fatal("completed original continuation rejected", result)
			}
			event, err := worker.DecodeResultWithPaths(input, "final", finalBytes, "original-final", []string{})
			if err != nil || event.Result.SourceResult != prior.Digest || event.Result.Validation.Status != "PASS" || event.Result.Validation.Development.RemainingWork != "" {
				t.Fatal("completed continuation lost original source or status", err)
			}
			facts, err := newNativeWorkspaceTools(continued)
			if err != nil || len(facts.facts) != 2 || !bytes.Equal(facts.facts[0].Content, partialBytes) || !bytes.Equal(facts.facts[1].Content, finalBytes) {
				t.Fatal("recovery lost original submitted facts", err)
			}
		})
	}
}
