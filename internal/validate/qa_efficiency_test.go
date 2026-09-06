package validate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQAEfficiencySharedFallbackVersionsAndProgress(t *testing.T) {
	for _, storageKey := range []string{"", "blackbox"} {
		t.Run("storage="+storageKey, func(t *testing.T) {
			c := QACase{ID: "CASE-001", Mode: "blackbox", Description: "public read", Procedure: "read public output", Oracle: "exact value", ReviewStatus: "PASS", SharedSteps: []string{"fixture"}}
			state := RunState{CurrentSnapshot: "candidate", RequirementRevision: "requirement",
				QACasesByMode:     map[string][]QACase{storageKey: {c}},
				QACaseSets:        map[string]QACaseSetState{storageKey: {SharedSteps: []QASharedStep{{ID: "fixture", Procedure: "copy clean fixture", ReviewStatus: "PASS"}}}},
				QAExecutionByMode: map[string]QAExecutionResult{}}
			if storageKey == "blackbox" {
				// A later per-mode design takes precedence even when the old merged
				// design retains a shared step with the same id.
				state.QACasesByMode[""] = []QACase{c}
				state.QACaseSets[""] = QACaseSetState{SharedSteps: []QASharedStep{{ID: "fixture", Procedure: "stale merged fixture", ReviewStatus: "PENDING"}}}
			}
			options := QAExecutionRecordOptions{Checkpoint: true, Tool: "script-v1", Environment: "fixture-v1"}
			state.QAExecutionByMode["blackbox"] = QAExecutionResult{Status: "PENDING", Tool: options.Tool, Environment: options.Environment,
				Cases: []QAResultRecord{{CaseID: c.ID, Outcome: "PASS", Procedure: "ran public read", Observation: "captured exact value", OracleResult: "matches expected value", Binding: qaExecutionBinding(state, "blackbox", c, options.Tool, options.Environment)}}}
			beforeVersion, beforeDispatch := caseSetVersion(state, "blackbox"), qaCaseSetVersion(state, "blackbox")
			rows, _, err := mergeQAExecutionProgress(state, "blackbox", []QACase{c}, nil, options)
			if err != nil || len(rows) != 1 {
				t.Fatalf("unchanged common preparation did not retain its completed row: %v %+v", err, rows)
			}
			if storageKey == "blackbox" {
				state.QACaseSets[""].SharedSteps[0].Procedure = "changed stale fixture"
				if caseSetVersion(state, "blackbox") != beforeVersion || qaCaseSetVersion(state, "blackbox") != beforeDispatch {
					t.Fatal("superseded merged definitions changed the concrete mode's version")
				}
			}
			state.QACaseSets[storageKey].SharedSteps[0].Procedure = "copy updated clean fixture"
			if caseSetVersion(state, "blackbox") == beforeVersion || qaCaseSetVersion(state, "blackbox") == beforeDispatch {
				t.Fatal("the actual shared definition was missing from the case-set or dispatch version")
			}
			rows, progress, err := mergeQAExecutionProgress(state, "blackbox", []QACase{c}, nil, options)
			if err != nil || len(rows) != 0 || len(progress.PendingCases) != 1 || len(progress.InvalidationReasons) != 1 {
				t.Fatalf("changed common preparation reused old execution: %v %+v %+v", err, rows, progress)
			}
		})
	}
}

func TestQAEfficiencySharedDefinitionsBeforeCases(t *testing.T) {
	state := RunState{QACaseSets: map[string]QACaseSetState{
		"blackbox": {SharedSteps: []QASharedStep{{ID: "fixture", Procedure: "copy clean fixture", ReviewStatus: "PENDING"}}},
	}}
	before := qaCaseSetVersion(state, "blackbox")
	state.QACaseSets["blackbox"].SharedSteps[0].Procedure = "copy updated clean fixture"
	if qaCaseSetVersion(state, "blackbox") == before {
		t.Fatal("a definition-only design lost its own shared steps or version binding")
	}
}

func TestQAEfficiencySharedReviewAndIncrementalInvalidation(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "shared-steps"), "full", nil)
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	cases := []QACaseInput{
		{Mode: "blackbox", Description: "first", Procedure: "call first public command", Oracle: "exact first output", SharedSteps: []string{"fixture"}},
		{Mode: "blackbox", Description: "second", Procedure: "call second public command", Oracle: "wrong second output", SharedSteps: []string{"fixture"}},
		{Mode: "blackbox", Description: "independent", Procedure: "call independent public command", Oracle: "exact independent output"},
	}
	state, err := RecordQADesign(root, pkg, state.RunID, design, cases, "", QADesignRecordOptions{SharedSteps: []QASharedStep{{ID: "fixture", Procedure: "copy clean fixture and capture public command output"}}})
	if err != nil {
		t.Fatal(err)
	}
	review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "shared-review")
	decisions := []QAReviewInput{{CaseID: "CASE-001", Outcome: "PASS"}, {CaseID: "CASE-002", Outcome: "FAIL", Reason: "oracle contradicts the second public command contract"}, {CaseID: "CASE-003", Outcome: "PASS"}}
	state, err = RecordQAReview(root, pkg, state.RunID, review, decisions, "", nil, QAReviewRecordOptions{SharedSteps: []QAReviewInput{{CaseID: "fixture", Outcome: "PASS", Reason: "clean fixture and command capture are executable"}}})
	if err != nil {
		t.Fatal(err)
	}
	if state.qaReview("").Status != "FAIL" || len(state.QACaseSets[""].SharedSteps) != 1 {
		t.Fatal("shared review hid erroneous oracle")
	}
	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	cases[1].CaseID, cases[1].Oracle = "CASE-002", "exact second output"
	state, err = RecordQADesign(root, pkg, state.RunID, design, cases[1:2], "")
	if err != nil {
		t.Fatal(err)
	}
	if len(pendingQACases(state.qaCases(""), "")) != 1 || state.QACaseSets[""].SharedSteps[0].ReviewStatus != "PASS" {
		t.Fatal("unaffected approvals lost")
	}
	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	state, err = RecordQADesign(root, pkg, state.RunID, design, nil, "", QADesignRecordOptions{SharedSteps: []QASharedStep{{ID: "fixture", Procedure: "copy updated clean fixture and capture public command output"}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(pendingQACases(state.qaCases(""), "")) != 2 || state.qaCases("")[2].ReviewStatus != "PASS" {
		t.Fatal("shared edit did not invalidate precisely its references")
	}
}

func TestQAEfficiencyCheckpointRetainsFailAndSameTask(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "qa-checkpoint")
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	rows := passingExecution(state.allQACases())
	rows[0].Outcome, rows[0].Observation = "FAIL", "real command returned the known incorrect output"
	options := QAExecutionRecordOptions{Checkpoint: true, Tool: "script-v1", Environment: "fixture-v1/go-runtime"}
	state, err := RecordQAExecution(root, pkg, state.RunID, dispatch, rows[:1], "", options)
	if err != nil {
		t.Fatal(err)
	}
	task := state.Dispatches[dispatch].TaskKey
	if state.qaExecution("").Status != "PENDING" || len(state.qaExecution("").PendingCases) != len(rows)-1 || state.qaExecution("").Cases[0].Outcome != "FAIL" {
		t.Fatal("partial result lost completeness or FAIL")
	}
	options.Checkpoint = false
	if _, err := RecordQAExecution(root, pkg, state.RunID, dispatch, nil, "", options); err == nil {
		t.Fatal("unfinished execution passed")
	}
	state, err = RecordQAExecution(root, pkg, state.RunID, dispatch, rows[1:], "", options)
	if err != nil {
		t.Fatal(err)
	}
	if state.qaExecution("").Status != "FAIL" || state.Dispatches[dispatch].TaskKey != task || state.DispatchObligations[task].AttemptsUsed != 1 || len(state.qaExecution("").ReuseReasons) != 1 {
		t.Fatal("checkpoint continuation changed task/attempt or lost FAIL")
	}
}

func TestQAEfficiencyProgressBindingChanges(t *testing.T) {
	base := RunState{CurrentSnapshot: "candidate-1", RequirementRevision: "req", QACaseSets: map[string]QACaseSetState{}, QAExecutionByMode: map[string]QAExecutionResult{}}
	cases := []QACase{{ID: "A", Mode: "blackbox", Oracle: "exact first output"}, {ID: "B", Mode: "blackbox", Oracle: "exact second output"}}
	options := QAExecutionRecordOptions{Checkpoint: true, Tool: "tool-v1", Environment: "env-v1"}
	for _, c := range cases {
		r := base.qaExecution("")
		r.Tool = options.Tool
		r.Environment = options.Environment
		r.Status = "RUNTIME_ERROR"
		r.Cases = append(r.Cases, QAResultRecord{CaseID: c.ID, Mode: c.Mode, Outcome: "FAIL", Procedure: "ran public command", Observation: "captured exit code 1", OracleResult: "expected exit code 0", Binding: qaExecutionBinding(base, "", c, options.Tool, options.Environment)})
		base.setQAExecution("", r)
	}
	for _, kind := range []string{"unchanged", "candidate", "case", "tool", "environment"} {
		t.Run(kind, func(t *testing.T) {
			state := base
			current := append([]QACase{}, cases...)
			opts := options
			switch kind {
			case "candidate":
				state.CurrentSnapshot = "candidate-2"
			case "case":
				current[0].Oracle = "new expected output"
			case "tool":
				opts.Tool = "tool-v2"
			case "environment":
				opts.Environment = "env-v2"
			}
			rows, result, err := mergeQAExecutionProgress(state, "", current, nil, opts)
			if err != nil {
				t.Fatal(err)
			}
			want := 0
			if kind == "unchanged" {
				want = 2
			}
			if kind == "case" {
				want = 1
			}
			if len(rows) != want || len(result.PendingCases) != 2-want || len(result.InvalidationReasons) != 2-want {
				t.Fatalf("invalid reuse accounting: %#v %#v", rows, result)
			}
		})
	}
	for _, inputs := range [][]QAResultInput{{{CaseID: "UNKNOWN"}}, {{CaseID: "A"}}} {
		if _, _, err := mergeQAExecutionProgress(base, "", cases, inputs, options); err == nil {
			t.Fatal("unknown/duplicate result accepted")
		}
	}
}

func TestQAEfficiencyOptionalSuggestionDispositions(t *testing.T) {
	for _, decision := range []string{"ACCEPT", "DEFER", "DECLINE"} {
		t.Run(decision, func(t *testing.T) {
			root, pkg := workflowFixture(t)
			state := beginQA(t, root, pkg, "suggestion-"+strings.ToLower(decision))
			review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "suggestion-review")
			state, err := RecordQAReview(root, pkg, state.RunID, review, passingReviewDecisions(state), "", []FindingInput{{Severity: "P3", Message: "optional wording improvement"}})
			if err != nil {
				t.Fatal(err)
			}
			id := state.QACaseSets[""].Suggestions[0].ID
			if _, err := RecordQASuggestion(root, pkg, state.RunID, "", "UNKNOWN", decision, "optional", nil); err == nil {
				t.Fatal("unknown id accepted")
			}
			if _, err := RecordQASuggestion(root, pkg, state.RunID, "", id, decision, "", nil); err == nil {
				t.Fatal("missing reason accepted")
			}
			state, err = RecordQASuggestion(root, pkg, state.RunID, "", id, decision, "covered behavior already sufficient; no acceptance change", nil)
			if err != nil {
				t.Fatal(err)
			}
			if state.QACaseSets[""].Status != caseSetFrozen || state.qaCases("")[0].ReviewStatus != "PASS" {
				t.Fatal("optional suggestion prevented freeze")
			}
		})
	}
}

func TestQAEfficiencyAcceptedChangeRequiresAffectedReview(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := beginQA(t, root, pkg, "suggestion-recheck")
	review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "suggestion-review")
	state, err := RecordQAReview(root, pkg, state.RunID, review, passingReviewDecisions(state), "", []FindingInput{{Severity: "P2", Message: "optional clearer boundary scenario"}})
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordQASuggestion(root, pkg, state.RunID, "", state.QACaseSets[""].Suggestions[0].ID, "ACCEPT", "accept changes the second case oracle", []string{"CASE-002"})
	if err != nil {
		t.Fatal(err)
	}
	if state.qaCases("")[0].ReviewStatus != "PASS" || state.qaCases("")[1].ReviewStatus != "PENDING" || state.QACaseSets[""].Status == caseSetFrozen {
		t.Fatal("accepted semantic change bypassed review")
	}
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design")
	changed := baselineCases()[1]
	changed.CaseID, changed.Oracle = "CASE-002", "the command reports the explicit boundary result"
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{changed}, "")
	if err != nil {
		t.Fatal(err)
	}
	review = prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "suggestion-affected-review")
	state, err = RecordQAReview(root, pkg, state.RunID, review, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	set := state.QACaseSets[""]
	if set.Status != caseSetFrozen || set.Version == "" || len(set.Suggestions) != 1 || set.Suggestions[0].Status != "ACCEPT" || set.Suggestions[0].Reason == "" {
		t.Fatalf("disposed suggestion prevented the revised approved set from freezing: %#v", set)
	}
}

func TestQAEfficiencyThawRetainsUnchangedCheckpointRows(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "thaw-checkpoint"), "custom", []string{blackboxQAID})
	state, err := RegisterQAWorktree(root, pkg, state.RunID, createQAWorktree(t, root, state))
	if err != nil {
		t.Fatal(err)
	}
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	cases := []QACaseInput{
		{Mode: "blackbox", Description: "first command", Procedure: "run first public command", Oracle: "first output is exact"},
		{Mode: "blackbox", Description: "second command", Procedure: "run second public command", Oracle: "second output is exact"},
		{Mode: "blackbox", Description: "third command", Procedure: "run third public command", Oracle: "third output is exact"},
	}
	state, err = RecordQADesign(root, pkg, state.RunID, design, cases, "")
	if err != nil {
		t.Fatal(err)
	}
	review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "thaw-initial-review", "blackbox")
	state, err = RecordQAReview(root, pkg, state.RunID, review, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	development := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, "thaw-delivery.txt"), "delivery\n")
	commitAll(t, root, "thaw delivery")
	state, err = AdvanceSnapshot(root, pkg, state.RunID, development, false, "")
	if err != nil {
		t.Fatal(err)
	}
	execution := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	options := QAExecutionRecordOptions{Checkpoint: true, Tool: "same-script-v1", Environment: "same-clean-fixture"}
	state, err = RecordQAExecution(root, pkg, state.RunID, execution, passingExecution(state.allQACases())[:2], "execution worker stopped after two complete cases", options)
	if err != nil {
		t.Fatal(err)
	}
	unchanged := state.qaExecution("blackbox").Cases[0]
	state, err = ThawQACaseSet(root, pkg, state.RunID, "blackbox", "SUCCESS_FAILURE_CRITERIA", "clarify only the second oracle", state.RequirementRevision, []string{"CASE-002"})
	if err != nil {
		t.Fatal(err)
	}
	progress := state.qaExecution("blackbox")
	if progress.Status != "PENDING" || len(progress.Cases) != 1 || progress.Cases[0] != unchanged || len(progress.InvalidationReasons) != 1 {
		t.Fatalf("thaw discarded unrelated completed evidence or retained affected evidence: %#v", progress)
	}
	cases[1].CaseID, cases[1].Oracle = "CASE-002", "second output includes the explicit boundary value"
	state, err = RegisterQAWorktree(root, pkg, state.RunID, createQAWorktree(t, root, state))
	if err != nil {
		t.Fatal(err)
	}
	design = prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, cases[1:2], "")
	if err != nil {
		t.Fatal(err)
	}
	review = prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "thaw-affected-review", "blackbox")
	state, err = RecordQAReview(root, pkg, state.RunID, review, passingReviewDecisions(state), "", nil)
	if err != nil {
		t.Fatal(err)
	}
	execution = prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	options.Checkpoint = false
	state, err = RecordQAExecution(root, pkg, state.RunID, execution, passingExecution(state.allQACases())[1:], "", options)
	if err != nil {
		t.Fatal(err)
	}
	progress = state.qaExecution("blackbox")
	if progress.Status != "PASS" || len(progress.Cases) != 3 || progress.Cases[0] != unchanged || len(progress.ReuseReasons) != 1 || !strings.Contains(progress.ReuseReasons[0], "CASE-001") || len(progress.PendingCases) != 0 {
		t.Fatalf("resume did not reuse the unchanged first result and finish the changed/unfinished cases: %#v", progress)
	}
}

func TestQAEfficiencyPrepareCandidateThenSealIdentity(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "prepare-final")
	// Reuse the normal whitebox test snapshot advancement before final verification.
	writeTestFile(t, filepath.Join(root, "whitebox-addition.txt"), "additional committed test fixture\n")
	commitAll(t, root, "additional test fixture")
	// Construct only the normal completed-development precondition; the target
	// operation and Seal below run for real against the actual repository.
	state.CurrentSnapshot = gitHead(t, root)
	if err := SaveRunState(root, state); err != nil {
		t.Fatal(err)
	}
	before := state.CurrentSnapshot
	state, err := PrepareCandidate(root, pkg, state.RunID, "organized final delivery")
	if err != nil {
		t.Fatal(err)
	}
	if state.CurrentSnapshot == before {
		t.Fatal("multiple commits were not organized")
	}
	final := state.CurrentSnapshot
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err = RecordQAExecution(root, pkg, state.RunID, dispatch, passingExecution(state.allQACases()), "")
	if err != nil {
		t.Fatal(err)
	}
	state = recordGateResult(t, root, pkg, state, "quality", "quality-final", "PASS", "", nil)
	state = recordGateResult(t, root, pkg, state, "architecture", "architecture-final", "PASS", "", nil)
	summary, err := Seal(root, pkg, state.RunID, nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if summary.CurrentSnapshot != final || gitHead(t, root) != final || summary.QA.Snapshot != final {
		t.Fatal("Seal changed validated candidate identity")
	}
}

func TestQAEfficiencyRuntimeRetryRetainsCompleteRows(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "qa-runtime-progress")
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	rows := passingExecution(state.allQACases())
	rows[0].Outcome, rows[0].Observation = "FAIL", "captured known incorrect output before the worker interruption"
	options := QAExecutionRecordOptions{Tool: "script-v1", Environment: "fixture-v1"}
	state, err := RecordQAExecution(root, pkg, state.RunID, dispatch, rows[:1], "worker interrupted after complete first evidence", options)
	if err != nil {
		t.Fatal(err)
	}
	task := state.Dispatches[dispatch].TaskKey
	if state.qaExecution("").Status != "RUNTIME_ERROR" || len(state.qaExecution("").Cases) != 1 {
		t.Fatal("runtime interruption lost completed evidence")
	}
	retry := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err = RecordQAExecution(root, pkg, state.RunID, retry, rows[1:], "", options)
	if err != nil {
		t.Fatal(err)
	}
	if state.qaExecution("").Status != "FAIL" || state.Dispatches[retry].TaskKey != task || state.DispatchObligations[task].AttemptsUsed != 2 || len(state.qaExecution("").ReuseReasons) != 1 {
		t.Fatal("runtime retry lost FAIL, changed logical task, or evaded its attempt limit")
	}
}

func TestCodexCheckpointMetersOnlyCompletedDispatch(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := readyDelivery(t, root, pkg, "codex-checkpoint-cost")
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	transcript := filepath.Join(t.TempDir(), "rollout.jsonl")
	first := `{"type":"token_usage_record","payload":{"response_id":"first","usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":20},"thread_token_usage":{"input_tokens":100,"cached_input_tokens":40,"output_tokens":20}}}` + "\n"
	if err := os.WriteFile(transcript, []byte(first), 0600); err != nil {
		t.Fatal(err)
	}
	stubCodexTranscript(t, transcript)
	rows := passingExecution(state.allQACases())
	options := QAExecutionRecordOptions{Checkpoint: true, Tool: "same-tool", Environment: "same-env"}
	state, err := RecordQAExecution(root, pkg, state.RunID, dispatch, rows[:1], "", options)
	if err != nil {
		t.Fatal(err)
	}
	if state.Cost != nil {
		if _, exists := state.Cost.Dispatches[dispatch]; exists {
			t.Fatal("checkpoint froze incomplete Codex cost")
		}
	}
	second := `{"type":"token_usage_record","payload":{"response_id":"second","usage":{"input_tokens":50,"cached_input_tokens":10,"output_tokens":7},"thread_token_usage":{"input_tokens":150,"cached_input_tokens":50,"output_tokens":27}}}` + "\n"
	if err := os.WriteFile(transcript, []byte(first+second), 0600); err != nil {
		t.Fatal(err)
	}
	options.Checkpoint = false
	state, err = RecordQAExecution(root, pkg, state.RunID, dispatch, rows[1:], "", options)
	if err != nil {
		t.Fatal(err)
	}
	entry := state.Cost.Dispatches[dispatch]
	if entry.TotalInputTokens != 150 || entry.InputCacheHitTokens != 50 || entry.InputCacheMissTokens != 100 || entry.OutputTokens != 27 {
		t.Fatalf("final usage=%+v", entry)
	}
}
