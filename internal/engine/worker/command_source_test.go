package worker

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/prompts"
	"formal-gates/internal/requirement"
)

func TestReadinessReusesApprovedChecksAndProposesVerificationGoals(t *testing.T) {
	command := "./checks/project-check --label \"原命令 two words\" '' \\\n\t--next 'second line'\n"
	facts := []LocationFact{{Kind: "listing", Paths: []string{"README.md"}}, {Kind: "read", Paths: []string{"README.md"}, Content: []byte("Project check:\n" + command + "Other text\n")}}
	input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: "engine.readiness.plan.v6", RunID: "project", Provider: "codex", Pending: protocol.PendingAction{ActionID: "readiness", AttemptID: "issued"}, RequirementDocument: requirement.FrozenRequirementProjection{Content: "Confirmed behavior"}, Review: &protocol.WorkerReviewInput{Action: review.StartReadiness, RequirementRevision: "confirmed"}}
	input.ApprovedChecks = []protocol.DevelopmentCheck{{ID: "project-edit", Purpose: protocol.CheckEdit, Command: command}}
	value := Review{Findings: []Finding{}, GranularityReview: &Granularity{Reason: "One cohesive duty", VerificationTier: "FINAL", Batches: []Batch{{Artifacts: []Location{{Read: 2}}, Subtasks: []Subtask{{Description: "Implement public behavior"}}, DoD: []Criterion{{Description: "Public behavior holds"}}, Deliverable: "Confirmed behavior", PublicEntry: "Product entry", Interfaces: "Public interface", StateBoundary: "Project files", Risk: "Local change", Rollback: "Restore base", Recovery: "Resume duty", Handoff: "Ready for validation", ContextCost: "One duty", Checks: []Check{{Reuse: 1}, {Goal: "Complete project build", Purpose: protocol.CheckFullBuild}, {Goal: "Complete project tests", Purpose: protocol.CheckFullTests}}}}}}
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	original, err := DecodeResultWithFacts(input, "result", raw, "verified-native-source", nil, facts)
	if err != nil {
		t.Fatal(err)
	}
	plan := original.Result.Review.GranularityReview
	if err := protocol.ValidateGranularity(plan); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(plan.Batches[0].Checks[0], input.ApprovedChecks[0]) {
		t.Fatalf("original command bytes changed: %q", plan.Batches[0].Checks[0].Command)
	}
	if plan.Batches[0].Checks[1].Command != "" || plan.Batches[0].Checks[1].Goal != "Complete project build" || plan.Batches[0].Checks[2].Command != "" || plan.Batches[0].Checks[2].Goal != "Complete project tests" {
		t.Fatal("review prepared calls instead of preserving verification duties")
	}
	ordinary, err := DecodeGranularity(input, &Granularity{Batches: []Batch{{Checks: []Check{{Goal: "Run the project regression"}}}}}, nil, facts, false)
	if err != nil || ordinary.Batches[0].Checks[0].Purpose != "" || ordinary.Batches[0].Checks[0].Command != "" || ordinary.Batches[0].Checks[0].Goal != "Run the project regression" {
		t.Fatal("ordinary regression goal lost its existing preparation duty", err)
	}
	stored, _ := json.Marshal(struct {
		Input protocol.WorkerInput
		Facts []LocationFact
	}{input, facts})
	var recovered struct {
		Input protocol.WorkerInput
		Facts []LocationFact
	}
	if err := json.Unmarshal(stored, &recovered); err != nil {
		t.Fatal(err)
	}
	replay, err := DecodeResultWithFacts(recovered.Input, "result", raw, "verified-native-source", nil, recovered.Facts)
	if err != nil || !reflect.DeepEqual(original, replay) {
		t.Fatalf("recovery changed approved reuse or verification goals: %v", err)
	}
	input.ApprovedPlan, input.ApprovedChecks = plan, plan.Batches[0].Checks
	for _, update := range []string{`{"batches":[{"reuse":1,"risk":"Changed boundary"}]}`, `{"batches":[{"reuse":1,"checks":[{"reuse":1},{"reuse":2},{"reuse":3}]}]}`} {
		var change Granularity
		if err := json.Unmarshal([]byte(update), &change); err != nil {
			t.Fatal(err)
		}
		selected, err := DecodeGranularity(input, &change, nil, facts, false)
		if err != nil || !reflect.DeepEqual(selected.Batches[0].Checks, plan.Batches[0].Checks) {
			t.Fatalf("partial approved plan lost command identities or bytes: %v", err)
		}
	}
	input.ApprovedChecks = nil
	var change Granularity
	err = json.Unmarshal([]byte(`{"batches":[{"reuse":1,"checks":[{"read":2,"from":2,"to":3,"purpose":"EDIT"}]}]}`), &change)
	if err == nil {
		_, err = DecodeGranularity(input, &change, nil, facts, false)
	}
	if err == nil {
		t.Fatal("approved batch change admitted reviewer command coordinates")
	}
}

func TestReadinessRejectsCommandTextAndCoordinates(t *testing.T) {
	input := protocol.WorkerInput{Handler: "engine.readiness.plan.v6", Review: &protocol.WorkerReviewInput{Action: review.StartReadiness}}
	facts := []LocationFact{{Kind: "listing"}, {Kind: "requirement", Content: []byte(strings.Repeat("Confirmed context\n", 18) + "Complete build uses project-build; the project tests use project-tests.\n")}}
	for _, raw := range []string{
		`{"read":2,"from":19,"to":19,"purpose":"EDIT"}`,
		`{"read":2,"from":19,"to":19,"purpose":"FULL_BUILD"}`,
		`{"read":2,"from":19,"to":19,"purpose":"FULL_TESTS"}`,
		`{"read":2,"from":19,"to":19,"fromColumn":1,"toColumn":1,"purpose":"FULL_BUILD"}`,
		`{"parts":["project-build"]}`,
		`{"command":"project-build","purpose":"FULL_BUILD"}`,
	} {
		for _, completed := range []bool{false, true} {
			var check Check
			err := json.Unmarshal([]byte(raw), &check)
			if err == nil {
				_, err = DecodeGranularity(input, &Granularity{Batches: []Batch{{Checks: []Check{check}}}}, nil, facts, completed)
			}
			if err == nil {
				t.Fatalf("review authored a call or selected its text: %s", raw)
			}
		}
	}
}

func TestChecksRejectMechanicalCommandSources(t *testing.T) {
	for _, raw := range []string{
		`{"read":1,"from":2,"to":3}`,
		`{"read":1,"from":2,"to":2,"fromColumn":1,"toColumn":4}`,
		`{"parts":[{"read":1,"from":2,"to":2},"/new/"]}`,
		`{"reuse":1,"command":"copied"}`,
	} {
		var check Check
		if err := json.Unmarshal([]byte(raw), &check); err == nil {
			t.Fatal("check accepted mechanical command source fields", raw)
		}
	}
}

func TestOriginalTextColumnsAndPartsPreservePublishedBytes(t *testing.T) {
	facts := []LocationFact{{Kind: "requirement", Content: []byte("前🙂 命令 尾\r\n甲\r\n末🙂")}}
	for _, sample := range []struct {
		name, raw, want string
	}{
		{"whole-line", `{"read":1,"from":1,"to":1}`, "前🙂 命令 尾\r\n"},
		{"inline", `{"read":1,"from":1,"to":1,"fromColumn":4,"toColumn":5}`, "命令"},
		{"multibyte", `{"read":1,"from":1,"to":1,"fromColumn":1,"toColumn":2}`, "前🙂"},
		{"line-endings", `{"read":1,"from":1,"to":2,"fromColumn":7,"toColumn":2}`, "尾\r\n甲\r"},
		{"no-final-newline", `{"read":1,"from":3,"to":3,"fromColumn":2,"toColumn":2}`, "🙂"},
		{"parts", `{"parts":[{"read":1,"from":1,"to":1,"fromColumn":4,"toColumn":5},"/new/",{"read":1,"from":3,"to":3}]}`, "命令/new/末🙂"},
		{"original-empty-field", `{"parts":[{"reuse":1},{"read":1,"from":3,"to":3}]}`, "末🙂"},
		{"empty-parts", `{"parts":[]}`, ""},
	} {
		t.Run(sample.name, func(t *testing.T) {
			var change StringChange
			if err := json.Unmarshal([]byte(sample.raw), &change); err != nil {
				t.Fatal(err)
			}
			got, err := change.text([]string{""}, facts)
			if err != nil || got != sample.want {
				t.Fatalf("original byte selection: %q %v", got, err)
			}
			body, err := json.Marshal(change)
			if err != nil {
				t.Fatal(err)
			}
			var recovered StringChange
			if err := json.Unmarshal(body, &recovered); err != nil {
				t.Fatal(err)
			}
			if replayed, err := recovered.text([]string{""}, facts); err != nil || replayed != got {
				t.Fatal("recovery changed original fragment bytes", err)
			}
		})
	}
	for _, raw := range []string{
		`{"read":1,"from":1,"to":1,"fromColumn":4}`,
		`{"read":1,"from":1,"to":1,"fromColumn":null,"toColumn":null}`,
		`{"read":1,"from":1,"to":1,"fromColumn":0,"toColumn":1}`,
		`{"read":1,"from":1,"to":1,"fromColumn":5,"toColumn":4}`,
		`{"read":1,"from":1,"to":1,"fromColumn":1,"toColumn":99}`,
		`{"read":99,"from":1,"to":1,"fromColumn":1,"toColumn":1}`,
		`{"parts":[{"reuse":2}]}`,
		`{"parts":[{"parts":[]}]}`,
		`{"parts":[],"read":1,"from":1,"to":1}`,
	} {
		var change StringChange
		err := json.Unmarshal([]byte(raw), &change)
		if err == nil {
			_, err = change.text([]string{""}, facts)
		}
		if err == nil {
			t.Fatal("invalid original fragment accepted", raw)
		}
	}
}

func TestRepairReusesApprovedChecksAndAuthorsLocalCalls(t *testing.T) {
	approved := []protocol.DevelopmentCheck{{ID: "build", Purpose: protocol.CheckFullBuild, Command: "build\n"}, {ID: "tests", Purpose: protocol.CheckFullTests, Command: "tests\n"}, {ID: "edit", Purpose: protocol.CheckEdit, Command: "edit ''\n"}, {ID: "local", Command: "approved-local\n"}}
	facts := []LocationFact{{Kind: "read", Content: []byte("Local check:\nnew-local '' \"中文\"\n")}, {Kind: "read", Content: []byte(approved[3].Command)}}
	input := protocol.WorkerInput{InputCodec: protocol.WorkerInputCodec, OutputCodec: OutputCodec, Handler: "engine.repair.v7", Provider: "codex", Pending: protocol.PendingAction{ActionID: "repair"}, ApprovedChecks: approved, Validation: &protocol.ValidationInput{Repair: &protocol.RepairInput{}}}
	continued, err := DecodeResultWithFacts(input, "partial", []byte(`{"summary":"Mapped repair","remainingWork":"Implement the approved change"}`), "verified-partial-source", []string{}, facts)
	if err != nil || continued.Result.Validation.Status != "CONTINUE" {
		t.Fatalf("repair continuation failed: %v", err)
	}
	input.Validation.Repair.Result = &protocol.WorkerResult{ActionID: "repair", Digest: "accepted-partial", Outcome: protocol.OutcomePass, Validation: continued.Result.Validation}
	for _, completed := range []bool{false, true} {
		value := Repair{Summary: "Restored behavior", GranularityReview: &Granularity{Reason: "One cohesive repair", VerificationTier: "FINAL", Batches: []Batch{{DoD: []Criterion{{Description: "Behavior restored"}}, Checks: []Check{{Reuse: 4}, {Command: "new-local '' \"中文\"\n"}}}}}}
		if completed {
			value.Completion = &Completion{DoD: []string{"Behavior restored"}, Handoff: "Ready for checks"}
		}
		raw, _ := json.Marshal(value)
		event, err := DecodeResultWithFacts(input, "result", raw, "verified-source", []string{"source.ext"}, facts)
		if err != nil {
			t.Fatal(err)
		}
		checks := event.Result.Validation.GranularityReview.Batches[0].Checks
		if len(checks) != 5 || !reflect.DeepEqual(checks[:4], approved) || checks[4].Command != "new-local '' \"中文\"\n" || checks[4].Purpose != "" {
			t.Fatalf("source selection changed repair inheritance: %+v", checks)
		}
		if event.Result.SourceResult != "accepted-partial" {
			t.Fatal("source selection lost the same repair continuation identity")
		}
		stored, _ := json.Marshal(struct {
			Input protocol.WorkerInput
			Facts []LocationFact
		}{input, facts})
		var recovered struct {
			Input protocol.WorkerInput
			Facts []LocationFact
		}
		if err := json.Unmarshal(stored, &recovered); err != nil {
			t.Fatal(err)
		}
		replay, err := DecodeResultWithFacts(recovered.Input, "result", raw, "verified-source", []string{"source.ext"}, recovered.Facts)
		if err != nil || !reflect.DeepEqual(event, replay) {
			t.Fatalf("repair recovery changed original selected bytes: %v", err)
		}
		for _, check := range []Check{{Command: "new-local", Purpose: protocol.CheckFullTests}, {Command: "new-local", Purpose: protocol.CheckEdit}, {Command: approved[3].Command}} {
			value.GranularityReview.Batches[0].Checks = []Check{check}
			raw, _ := json.Marshal(value)
			if _, err := DecodeResultWithFacts(input, "invalid", raw, "verified-source", []string{"source.ext"}, facts); err == nil {
				t.Fatalf("repair policy was relaxed for %+v", check)
			}
		}
	}
}

// Missing shared values use the original read assembly and ordered model wire.
func TestSharedInvocationInputRetainsProceduresAndOriginalValueSelections(t *testing.T) {
	input := semanticInput()
	input.Handler = "engine.qa.execution.v4"
	input.RequirementDocument.Content = "Complete confirmed requirement"
	input.Pending.AttemptID = "current-attempt"
	v := input.Validation
	v.CommandCases = []string{"approved", "approved", "approved"}
	v.CommandSources = []string{"shared:first", "shared:second", "case:approved"}
	v.RequiredCases = []string{"approved"}
	v.PendingCases = []string{"approved"}
	v.Manifest.Cases = []coverage.AcceptanceCase{{CaseID: "approved", Oracle: "approved oracle", SharedSteps: []string{"first", "second"}}}
	v.Manifest.SharedSteps = []coverage.SharedStep{{ID: "first", Procedure: "first original procedure"}, {ID: "second", Procedure: "second original procedure"}}
	facts := qaRecipeFacts(t)
	raw := []byte(`{"commands":[` + qaInvocationOperations + `,` + qaInvocationOperations + `,` + qaInvocationOperations + `]}`)
	assertQARecipeReplay(t, input, raw, facts, func(event protocol.Event) {
		got := event.Result.Validation.QACommands
		if len(got) != 3 {
			t.Fatal("duties changed")
		}
		for i, command := range got {
			want := []string{"shared:first", "shared:second", ""}[i]
			if command.CaseID != "approved" || command.Source != want || !reflect.DeepEqual(command.Operations, qaExpectedOperations()) {
				t.Fatalf("source/read bytes changed %+v", command)
			}
		}
	})
	schema, err := ResultSchema(input)
	if err != nil {
		t.Fatal(err)
	}
	fields := schema["anyOf"].([]any)[0].(map[string]any)["properties"].(map[string]any)
	entries := fields["commands"].(map[string]any)["prefixItems"].([]any)
	if len(entries) != 3 || !strings.Contains(entries[0].(map[string]any)["description"].(string), "shared preparation first") || !strings.Contains(entries[2].(map[string]any)["description"].(string), "approved case approved") {
		t.Fatal("schema duties do not follow actual missing plans")
	}
	for _, forbidden := range []string{"caseId", "source", "stepId", "revision"} {
		if fields[forbidden] != nil {
			t.Fatal("model schema requests engine identity", forbidden)
		}
	}
	catalog, err := prompts.LoadPromptCatalog(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	material, projection := comparisonMaterial(t, input, catalog)
	if !reflect.DeepEqual(projection.Validation.CommandCases, v.CommandCases) || !reflect.DeepEqual(projection.Validation.CommandSources, v.CommandSources) || !reflect.DeepEqual(projection.Validation.Manifest.SharedSteps, v.Manifest.SharedSteps) {
		t.Fatal("material lost original missing responsibilities/procedures")
	}
	if !strings.Contains(material.Prompt, "first original procedure") || !strings.Contains(material.Prompt, "second original procedure") || !strings.Contains(material.Prompt, "Supply only these missing invocation values") {
		t.Fatal("missing invocation author cannot read original shared procedures")
	}
}
