package validate

// Independent QA design for dispatch-cb42af60f339a817f5c648d7. These tests
// exercise the owning rules with private fixtures; they do not use earlier QA
// helpers. Persisted fixture states describe prerequisites, never target results.
import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/cost"
	"formal-gates/internal/lifecycle"
)

func qa35cIndependentState() RunState {
	return RunState{RunID: "qa35c-independent", Flow: "formal", Status: "ACTIVE", VCS: "git", BaseSnapshot: "base", CurrentSnapshot: "candidate", RequirementRevision: "requirement-v1", RequirementConfirmed: true, RouteMode: "custom",
		Actions: map[string]ActionResult{}, Gates: map[string]GateResult{}, Carry: map[string]CarryResult{}, Dispatches: map[string]PreparedDispatch{},
		QACasesByMode: map[string][]QACase{}, QACaseSets: map[string]QACaseSetState{}, QADesignByMode: map[string]ActionResult{}, QAReviewByMode: map[string]ActionResult{}, QAExecutionByMode: map[string]QAExecutionResult{},
		PriorQAExecutionByMode: map[string]*QAExecutionResult{}, ExecutionScopes: map[string]QAExecutionScope{}, PreDevelopmentReviewSeries: newPreDevelopmentReviewSeries()}
}

func qa35cIndependentOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func qa35cIndependentWrite(t *testing.T, path, value string) {
	t.Helper()
	qa35cIndependentOK(t, os.MkdirAll(filepath.Dir(path), 0755))
	qa35cIndependentOK(t, os.WriteFile(path, []byte(value), 0600))
}

func qa35cIndependentGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	c := exec.Command("git", args...)
	c.Dir = root
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Independent QA", "GIT_COMMITTER_NAME=Independent QA", "GIT_AUTHOR_EMAIL=independent@example.invalid", "GIT_COMMITTER_EMAIL=independent@example.invalid")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func qa35cIndependentStore(t *testing.T, root string, s RunState) (string, RunState) {
	t.Helper()
	pkg, err := filepath.Abs("../..")
	qa35cIndependentOK(t, err)
	qa35cIndependentWrite(t, filepath.Join(root, "requirement.md"), "Independent structural fixture requirement.\n")
	s.RequirementSource = "requirement.md"
	s.RequirementArtifacts, err = requirementArtifactSet(root, s.RequirementSource, nil)
	qa35cIndependentOK(t, err)
	catalog, err := LoadPromptCatalog(pkg)
	qa35cIndependentOK(t, err)
	s.CatalogRevision, s.BasePromptRevision, s.PromptHashes = catalog.CatalogRevision, catalog.BaseRevision, catalogPromptHashes(catalog)
	qa35cIndependentOK(t, SaveRunState(root, s))
	s, err = LoadRunState(root, s.RunID)
	qa35cIndependentOK(t, err)
	return pkg, s
}

func TestQA35CIndependentSharedApprovalInvalidation(t *testing.T) {
	s := qa35cIndependentState()
	s.QACaseSets["whitebox"] = QACaseSetState{SharedSteps: []QASharedStep{{ID: "prepare", Procedure: "create clean fixture", ReviewStatus: "PASS", ReviewReason: "checked once"}, {ID: "other", Procedure: "read separate fixture", ReviewStatus: "PASS"}}}
	cases := []QACase{{ID: "A", SharedSteps: []string{"prepare"}, ReviewStatus: "PASS", ApprovedSource: "prior"}, {ID: "B", SharedSteps: []string{"prepare"}, ReviewStatus: "PASS", ApprovedSource: "prior"}, {ID: "C", SharedSteps: []string{"other"}, ReviewStatus: "PASS", ApprovedSource: "prior"}}
	before := append([]QACase(nil), cases...)
	changed, err := mergeQASharedSteps(&s, "whitebox", []QASharedStep{{ID: "prepare", Procedure: "create clean fixture"}}, cases)
	qa35cIndependentOK(t, err)
	if changed || !reflect.DeepEqual(cases, before) {
		t.Fatal("unchanged shared preparation revoked approval")
	}
	changed, err = mergeQASharedSteps(&s, "whitebox", []QASharedStep{{ID: "prepare", Procedure: "create clean fixture with prerequisite"}}, cases)
	qa35cIndependentOK(t, err)
	if !changed || len(s.QACaseSets["whitebox"].SharedSteps) != 2 || cases[0].ReviewStatus != "PENDING" || cases[1].ReviewStatus != "PENDING" || cases[0].ApprovedSource != "" || !reflect.DeepEqual(cases[2], before[2]) {
		t.Fatalf("wrong impact set: %+v", cases)
	}
	if sharedStep(s, "whitebox", "prepare").ReviewStatus != "PENDING" {
		t.Fatal("changed shared record kept approval")
	}
	if validateSharedStepReferences(s, "whitebox", QACase{ID: "D", SharedSteps: []string{"missing"}}) == nil {
		t.Fatal("unknown shared reference accepted")
	}
}

func TestQA35CIndependentSharedReviewExactSet(t *testing.T) {
	makeState := func() RunState {
		s := qa35cIndependentState()
		s.QACaseSets["whitebox"] = QACaseSetState{SharedSteps: []QASharedStep{{ID: "prepare", Procedure: "prepare", ReviewStatus: "PENDING"}, {ID: "approved", Procedure: "separate", ReviewStatus: "PASS", ReviewReason: "prior decision"}}}
		return s
	}
	for name, inputs := range map[string][]QAReviewInput{"missing": nil, "unknown": {{CaseID: "unknown", Outcome: "PASS", Reason: "checked"}}, "empty reason": {{CaseID: "prepare", Outcome: "PASS"}}, "duplicate": {{CaseID: "prepare", Outcome: "PASS", Reason: "checked"}, {CaseID: "prepare", Outcome: "PASS", Reason: "again"}}, "approved redecision": {{CaseID: "approved", Outcome: "PASS", Reason: "again"}}} {
		t.Run(name, func(t *testing.T) {
			s := makeState()
			if reviewQASharedSteps(&s, "whitebox", inputs) == nil {
				t.Fatal("invalid shared decision set accepted")
			}
		})
	}
	s := makeState()
	qa35cIndependentOK(t, reviewQASharedSteps(&s, "whitebox", []QAReviewInput{{CaseID: "prepare", Outcome: "FAIL", Reason: "required prerequisite absent"}}))
	if sharedStep(s, "whitebox", "prepare").ReviewStatus != "FAIL" || sharedStep(s, "whitebox", "approved").ReviewReason != "prior decision" {
		t.Fatal("per-record outcome or inheritance lost")
	}
}

func qa35cIndependentProgress() (RunState, []QACase, QAExecutionRecordOptions) {
	s := qa35cIndependentState()
	cases := []QACase{{ID: "A", Mode: "whitebox", Description: "first observation", Procedure: "invoke owner", Oracle: "value is 1", Test: "file::A", ReviewStatus: "PASS", SharedSteps: []string{"prepare"}}, {ID: "B", Mode: "whitebox", Description: "second observation", Procedure: "invoke owner", Oracle: "value is 2", Test: "file::B", ReviewStatus: "PASS"}, {ID: "C", Mode: "whitebox", Description: "unfinished observation", Procedure: "invoke owner", Oracle: "value is 3", Test: "file::C", ReviewStatus: "PASS"}}
	s.QACaseSets["whitebox"] = QACaseSetState{SharedSteps: []QASharedStep{{ID: "prepare", Procedure: "clean prerequisite", ReviewStatus: "PASS"}}}
	o := QAExecutionRecordOptions{Checkpoint: true, Tool: "go-test-v1", Environment: "fixture-v1"}
	r := QAExecutionResult{Status: "RUNTIME_ERROR", Tool: o.Tool, Environment: o.Environment, Message: "executor interrupted"}
	for i, c := range cases[:2] {
		outcome := "PASS"
		if i == 1 {
			outcome = "FAIL"
		}
		r.Cases = append(r.Cases, QAResultRecord{CaseID: c.ID, Outcome: outcome, Procedure: "invoke owner with isolated input", Observation: "captured return value and exit code", OracleResult: "compare value to exact expected integer", Binding: qaExecutionBinding(s, "whitebox", c, o.Tool, o.Environment)})
	}
	s.QAExecutionByMode["whitebox"] = r
	return s, cases, o
}

func TestQA35CIndependentCheckpointResumePreservesResults(t *testing.T) {
	s, cases, o := qa35cIndependentProgress()
	rows, p, err := mergeQAExecutionProgress(s, "whitebox", cases, nil, o)
	qa35cIndependentOK(t, err)
	if len(rows) != 2 || rows[1].Outcome != "FAIL" || !reflect.DeepEqual(p.PendingCases, []string{"C"}) || len(p.ReuseReasons) != 2 {
		t.Fatalf("partial state misreported: %+v %+v", rows, p)
	}
	rows, p, err = mergeQAExecutionProgress(s, "whitebox", cases, []QAResultInput{{CaseID: "C", Outcome: "PASS", Procedure: "run third owner input", Observation: "third return is 3", OracleResult: "third result equals 3"}}, o)
	qa35cIndependentOK(t, err)
	if len(rows) != 3 || rows[1].Outcome != "FAIL" || len(p.PendingCases) != 0 {
		t.Fatal("resume erased FAIL or missed a row")
	}
	for name, input := range map[string][]QAResultInput{"already complete": {{CaseID: "A", Outcome: "PASS"}}, "unknown": {{CaseID: "D", Outcome: "PASS"}}, "duplicate new": {{CaseID: "C", Outcome: "PASS"}, {CaseID: "C", Outcome: "PASS"}}} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := mergeQAExecutionProgress(s, "whitebox", cases, input, o); err == nil {
				t.Fatal("invalid expected-set input accepted")
			}
		})
	}
	for _, status := range []string{"PENDING", "AUTHORIZED_SKIP"} {
		bad, cc, oo := qa35cIndependentProgress()
		r := bad.QAExecutionByMode["whitebox"]
		r.Cases[0].Outcome = status
		bad.QAExecutionByMode["whitebox"] = r
		rows, p, err := mergeQAExecutionProgress(bad, "whitebox", cc, nil, oo)
		qa35cIndependentOK(t, err)
		if len(rows) != 1 || rows[0].CaseID != "B" || len(p.InvalidationReasons) != 1 {
			t.Fatal("unfinished/skip row was reused as completed")
		}
	}
}

func TestQA35CIndependentProgressBindingsAndEvidence(t *testing.T) {
	for name, mutate := range map[string]func(*RunState, []QACase, *QAExecutionRecordOptions){
		"candidate":   func(s *RunState, _ []QACase, _ *QAExecutionRecordOptions) { s.CurrentSnapshot = "next" },
		"requirement": func(s *RunState, _ []QACase, _ *QAExecutionRecordOptions) { s.RequirementRevision = "next" },
		"tool":        func(_ *RunState, _ []QACase, o *QAExecutionRecordOptions) { o.Tool = "next" },
		"environment": func(_ *RunState, _ []QACase, o *QAExecutionRecordOptions) { o.Environment = "next" },
		"one case":    func(_ *RunState, c []QACase, _ *QAExecutionRecordOptions) { c[0].Oracle = "value is 9" },
		"shared prerequisite": func(s *RunState, _ []QACase, _ *QAExecutionRecordOptions) {
			set := s.QACaseSets["whitebox"]
			set.SharedSteps[0].Procedure = "new prerequisite"
			s.QACaseSets["whitebox"] = set
		},
		"incomplete evidence": func(s *RunState, _ []QACase, _ *QAExecutionRecordOptions) {
			r := s.QAExecutionByMode["whitebox"]
			r.Cases[0].Observation = ""
			s.QAExecutionByMode["whitebox"] = r
		},
		"sentinel evidence": func(s *RunState, _ []QACase, _ *QAExecutionRecordOptions) {
			r := s.QAExecutionByMode["whitebox"]
			r.Cases[0].Observation = "PASS"
			s.QAExecutionByMode["whitebox"] = r
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, c, o := qa35cIndependentProgress()
			mutate(&s, c, &o)
			rows, p, err := mergeQAExecutionProgress(s, "whitebox", c, nil, o)
			qa35cIndependentOK(t, err)
			want := 0
			if name == "one case" || name == "shared prerequisite" || name == "incomplete evidence" || name == "sentinel evidence" {
				want = 1
			}
			if len(rows) != want || len(p.InvalidationReasons) != 2-want || (want == 1 && rows[0].CaseID != "B") {
				t.Fatalf("incorrect reuse boundary: %+v %+v", rows, p)
			}
		})
	}
	s, c, o := qa35cIndependentProgress()
	c[0].ReviewStatus = "PENDING"
	c[0].ApprovedSource = "new review metadata"
	set := s.QACaseSets["whitebox"]
	set.SharedSteps[0].ReviewReason = "fresh reviewer evidence"
	s.QACaseSets["whitebox"] = set
	rows, _, err := mergeQAExecutionProgress(s, "whitebox", c, nil, o)
	qa35cIndependentOK(t, err)
	if len(rows) != 2 {
		t.Fatal("review-only metadata changed execution meaning")
	}
	o.Tool = ""
	if _, _, err := mergeQAExecutionProgress(s, "whitebox", c, nil, o); err == nil {
		t.Fatal("resume without tool identity accepted")
	}
}

func TestQA35CIndependentSuggestionDispositionAndFreeze(t *testing.T) {
	makeState := func() RunState {
		s := qa35cIndependentState()
		s.QACasesByMode["whitebox"] = []QACase{{ID: "A", Mode: "whitebox", ReviewStatus: "PASS"}, {ID: "B", Mode: "whitebox", ReviewStatus: "PASS"}}
		s.QAReviewByMode["whitebox"] = ActionResult{Status: "PASS"}
		s.QACaseSets["whitebox"] = QACaseSetState{Status: caseSetReviewed, Suggestions: []QASuggestion{{ID: "suggestion-a", Status: "PENDING"}, {ID: "suggestion-b", Status: "PENDING"}}}
		return s
	}
	for _, decision := range []string{"ACCEPT", "DEFER", "DECLINE"} {
		t.Run(decision, func(t *testing.T) {
			root := t.TempDir()
			pkg, s := qa35cIndependentStore(t, root, makeState())
			s, err := RecordQASuggestion(root, pkg, s.RunID, "whitebox", "suggestion-a", decision, "optional wording is accounted for", nil)
			qa35cIndependentOK(t, err)
			if s.QACaseSets["whitebox"].Status == caseSetFrozen {
				t.Fatal("undisposed suggestion ignored")
			}
			s, err = RecordQASuggestion(root, pkg, s.RunID, "whitebox", "suggestion-b", "DEFER", "outside current acceptance", nil)
			qa35cIndependentOK(t, err)
			if s.QACaseSets["whitebox"].Status != caseSetFrozen || s.QACaseSets["whitebox"].BoundSnapshot != s.CurrentSnapshot {
				t.Fatal("disposed optional suggestions did not freeze")
			}
		})
	}
	for name, fields := range map[string][]string{"unknown": {"missing", "DEFER", "reason"}, "missing reason": {"suggestion-a", "DEFER", ""}, "invalid decision": {"suggestion-a", "IGNORE", "reason"}} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			pkg, s := qa35cIndependentStore(t, root, makeState())
			_, err := RecordQASuggestion(root, pkg, s.RunID, "whitebox", fields[0], fields[1], fields[2], nil)
			if err == nil {
				t.Fatal("invalid disposition accepted")
			}
		})
	}
	root := t.TempDir()
	pkg, s := qa35cIndependentStore(t, root, makeState())
	s, err := RecordQASuggestion(root, pkg, s.RunID, "whitebox", "suggestion-a", "ACCEPT", "changes A prerequisite", []string{"A"})
	qa35cIndependentOK(t, err)
	if s.QACasesByMode["whitebox"][0].ReviewStatus != "PENDING" || s.QACasesByMode["whitebox"][1].ReviewStatus != "PASS" || s.QACaseSets["whitebox"].Status != caseSetDraft || s.QAReviewByMode["whitebox"].Status != "PENDING" {
		t.Fatal("accepted semantic change did not reopen exact impact set")
	}
	set := s.QACaseSets["whitebox"]
	set.Suggestions = []QASuggestion{{Status: "DECLINE", Reason: "optional"}}
	if freezeQACaseSet(&s, "whitebox", &set, false) == nil {
		t.Fatal("suggestion disposition bypassed unapproved case")
	}
}

func TestQA35CIndependentWhiteboxSnapshotReuse(t *testing.T) {
	root := t.TempDir()
	qa35cIndependentGit(t, root, "init", "-q")
	qa35cIndependentWrite(t, filepath.Join(root, "existing_test.go"), "package existing\n")
	qa35cIndependentGit(t, root, "add", ".")
	qa35cIndependentGit(t, root, "commit", "-qm", "existing tests")
	s := qa35cIndependentState()
	s.CurrentSnapshot = qa35cIndependentGit(t, root, "rev-parse", "HEAD")
	qa35cIndependentOK(t, requireWhiteboxReviewSnapshot(root, s, "whitebox"))
	if qa35cIndependentGit(t, root, "rev-parse", "HEAD") != s.CurrentSnapshot {
		t.Fatal("reuse advanced snapshot")
	}
	qa35cIndependentWrite(t, filepath.Join(root, "existing_test.go"), "package changed\n")
	if requireWhiteboxReviewSnapshot(root, s, "whitebox") == nil {
		t.Fatal("uncommitted changed tests accepted")
	}
	qa35cIndependentGit(t, root, "add", ".")
	qa35cIndependentGit(t, root, "commit", "-qm", "correct assertion")
	if requireWhiteboxReviewSnapshot(root, s, "whitebox") == nil {
		t.Fatal("unregistered test snapshot accepted")
	}
	s.CurrentSnapshot = qa35cIndependentGit(t, root, "rev-parse", "HEAD")
	qa35cIndependentOK(t, requireWhiteboxReviewSnapshot(root, s, "whitebox"))
}

func TestQA35CIndependentPrepareCandidateIdentity(t *testing.T) {
	root := t.TempDir()
	qa35cIndependentGit(t, root, "init", "-q")
	qa35cIndependentWrite(t, filepath.Join(root, ".gitignore"), ".gates/\nrequirement.md\n")
	qa35cIndependentGit(t, root, "add", ".")
	qa35cIndependentGit(t, root, "commit", "-qm", "base")
	s := qa35cIndependentState()
	s.BaseSnapshot = qa35cIndependentGit(t, root, "rev-parse", "HEAD")
	for _, file := range []string{"product.go", "product_test.go"} {
		qa35cIndependentWrite(t, filepath.Join(root, file), "package product\n")
		qa35cIndependentGit(t, root, "add", ".")
		qa35cIndependentGit(t, root, "commit", "-qm", file)
	}
	s.CurrentSnapshot = qa35cIndependentGit(t, root, "rev-parse", "HEAD")
	old := s.CurrentSnapshot
	tree := qa35cIndependentGit(t, root, "rev-parse", "HEAD^{tree}")
	s.Actions["development-worker"] = ActionResult{Status: developmentComplete}
	s.SelectedGates = []string{"whitebox"}
	s.QACasesByMode["whitebox"] = []QACase{{ID: "A", Mode: "whitebox", ReviewStatus: "PASS"}}
	s.QACaseSets["whitebox"] = QACaseSetState{Status: caseSetFrozen, Version: "case-v1", BoundSnapshot: old}
	s.QAExecutionByMode["whitebox"] = QAExecutionResult{Status: "PASS", Snapshot: old}
	s.Gates["quality"] = GateResult{Status: "PASS", Snapshot: old}
	pkg, s := qa35cIndependentStore(t, root, s)
	if _, err := PrepareCandidate(root, pkg, s.RunID, ""); err == nil {
		t.Fatal("multiple commits without message accepted")
	}
	if qa35cIndependentGit(t, root, "rev-parse", "HEAD") != old {
		t.Fatal("rejected prepare changed candidate")
	}
	s, err := PrepareCandidate(root, pkg, s.RunID, "final QA candidate")
	qa35cIndependentOK(t, err)
	if s.CurrentSnapshot == old || qa35cIndependentGit(t, root, "rev-list", "--count", s.BaseSnapshot+"..HEAD") != "1" || qa35cIndependentGit(t, root, "rev-parse", "HEAD^{tree}") != tree {
		t.Fatal("candidate organization lost tree or did not squash")
	}
	if s.QAExecutionByMode["whitebox"].Status != "PENDING" || s.Gates["quality"].Status != "PENDING" || s.QACaseSets["whitebox"].BoundSnapshot != s.CurrentSnapshot || s.QACaseSets["whitebox"].Version != "case-v1" {
		t.Fatal("old verification reused or case identity unnecessarily changed")
	}
	id := s.CurrentSnapshot
	s, err = PrepareCandidate(root, pkg, s.RunID, "")
	qa35cIndependentOK(t, err)
	if s.CurrentSnapshot != id {
		t.Fatal("already organized candidate advanced")
	}
}

func TestQA35CIndependentCodexSourceProjection(t *testing.T) {
	root := t.TempDir()
	s := qa35cIndependentState()
	_, s = qa35cIndependentStore(t, root, s)
	qa35cIndependentOK(t, lifecycle.BeginRun(root, s.RunID))
	qa35cIndependentOK(t, lifecycle.BindDispatchWithProvider(root, s.RunID, "dispatch-meter", "native-thread", "codex"))
	path := filepath.Join(root, "rollout.jsonl")
	qa35cIndependentWrite(t, path, "{\"timestamp\":\"2026-09-01T00:00:00Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\",\"turn_id\":\"t\"}}\n{\"type\":\"token_usage_record\",\"payload\":{\"response_id\":\"response-1\",\"usage\":{\"input_tokens\":90,\"cached_input_tokens\":30,\"output_tokens\":8}}}\n{\"timestamp\":\"2026-09-01T00:00:04Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\",\"turn_id\":\"t\"}}\n")
	for _, event := range []string{"SubagentStart", "SubagentStop"} {
		payload, err := json.Marshal(map[string]string{"thread_id": "native-thread", "cwd": root, "rollout_path": path})
		qa35cIndependentOK(t, err)
		_, err = lifecycle.Capture(root, "codex", event, payload)
		qa35cIndependentOK(t, err)
	}
	d := PreparedDispatch{ID: "dispatch-meter", TargetKind: "action", Target: "qa-execution"}
	backfillDispatchCost(root, &s, d)
	backfillDispatchCost(root, &s, d)
	e := s.Cost.Dispatches[d.ID]
	if e.Source != cost.SourceTranscript || e.TranscriptPath != path || e.TotalInputTokens != 90 || e.InputCacheHitTokens != 30 || e.InputCacheMissTokens != 60 || e.OutputTokens != 8 || e.Timing == nil || e.Timing.WallTimeMillis != 4000 || s.Cost.TotalInputTokens != 90 {
		t.Fatalf("native source projection mismatch: %+v", e)
	}
	backfillDispatchCost(root, &s, PreparedDispatch{ID: "unbound", TargetKind: "action", Target: "qa-design"})
	missing := s.Cost.Dispatches["unbound"]
	if missing.Source != cost.SourceUnavailable || missing.Timing != nil {
		t.Fatal("missing source fabricated completed usage/timing")
	}
}

// The historical checkpoint test is itself the changed product of REQ-010.
// Execute that exact function in a tiny private module with fixture commit IDs;
// do not duplicate its rule in a newly invented checker or alter real history.
func TestQA35CIndependentHistoricalCheckpointOracle(t *testing.T) {
	data, err := os.ReadFile("qa35b_contract_2fe71c_test.go")
	qa35cIndependentOK(t, err)
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "checkpoint.go", data, 0)
	qa35cIndependentOK(t, err)
	var function string
	for _, decl := range parsed.Decls {
		if f, ok := decl.(*ast.FuncDecl); ok && f.Name.Name == "TestQA35BCheckpointDoesNotImplementStage4OrRewriteHistory" {
			function = string(data[fset.Position(f.Pos()).Offset:fset.Position(f.End()).Offset])
		}
	}
	if function == "" {
		t.Fatal("historical checkpoint owner not found")
	}
	for _, scenario := range []struct{ name, path, document, wantError string }{
		{"append with later worktree rewrite", "internal/engine/guard/check.go", "historical base\ncheckpoint append\n", ""},
		{"stage boundary", "internal/engine/runtime/check.go", "historical base\ncheckpoint append\n", "stage 4 runtime touched"},
		{"ADR boundary", "refactor-plan/ADR-001.md", "historical base\ncheckpoint append\n", "ADR changed"},
		{"historical rewrite", "notes.md", "rewritten historical base\ncheckpoint append\n", "stage record history changed"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			root := t.TempDir()
			qa35cIndependentGit(t, root, "init", "-q")
			qa35cIndependentWrite(t, filepath.Join(root, "refactor-plan/stage-records.md"), "historical base\n")
			qa35cIndependentGit(t, root, "add", ".")
			qa35cIndependentGit(t, root, "commit", "-qm", "base")
			base := qa35cIndependentGit(t, root, "rev-parse", "HEAD")
			qa35cIndependentWrite(t, filepath.Join(root, scenario.path), "checkpoint change\n")
			qa35cIndependentWrite(t, filepath.Join(root, "refactor-plan/stage-records.md"), scenario.document)
			qa35cIndependentGit(t, root, "add", ".")
			qa35cIndependentGit(t, root, "commit", "-qm", "delivered")
			delivered := qa35cIndependentGit(t, root, "rev-parse", "HEAD")
			qa35cIndependentWrite(t, filepath.Join(root, "refactor-plan/stage-records.md"), "later current document rewrite\n")
			body := strings.ReplaceAll(strings.ReplaceAll(function, "860ac5eefe1c501aa9caae42de788af05a7f6809", base), "b7eab3b16036a0a5fdeb32efc33c9d93d740392c", delivered)
			harness := fmt.Sprintf("package checkpoint\nimport (\"testing\";\"strings\";\"os/exec\")\nfunc qa35bPackage(t *testing.T) string{return %q}\nfunc qa35bGit(t *testing.T,root string,args ...string)string{t.Helper();c:=exec.Command(\"git\",args...);c.Dir=root;b,e:=c.CombinedOutput();if e!=nil{t.Fatalf(\"git: %%v %%s\",e,b)};return strings.TrimSpace(string(b))}\n", root) + body + "\n"
			module := t.TempDir()
			qa35cIndependentWrite(t, filepath.Join(module, "go.mod"), "module checkpoint-fixture\ngo 1.22\n")
			qa35cIndependentWrite(t, filepath.Join(module, "checkpoint_test.go"), harness)
			c := exec.Command("go", "test", "-count=1", "-run", "^TestQA35BCheckpointDoesNotImplementStage4OrRewriteHistory$", "-v", ".")
			c.Dir = module
			out, runErr := c.CombinedOutput()
			if scenario.wantError == "" {
				if runErr != nil {
					t.Fatalf("valid fixed pair rejected: %v\n%s", runErr, out)
				}
			} else if runErr == nil || !strings.Contains(string(out), scenario.wantError) {
				t.Fatalf("known bad checkpoint not rejected by owning rule %q: %v\n%s", scenario.wantError, runErr, out)
			}
			t.Logf("owning checkpoint execution: %s", out)
		})
	}
}

func TestQA35CIndependentFixtureBuildAndCopy(t *testing.T) {
	script, err := filepath.Abs("../../scripts/phase3-qa-fixture.sh")
	qa35cIndependentOK(t, err)
	source := t.TempDir()
	qa35cIndependentGit(t, source, "init", "-q")
	qa35cIndependentWrite(t, filepath.Join(source, "go.mod"), "module fixture-product\ngo 1.22\n")
	qa35cIndependentWrite(t, filepath.Join(source, "cmd/formal-gates/main.go"), "package main\nimport \"fmt\"\nfunc main(){fmt.Println(\"immutable candidate\")}\n")
	qa35cIndependentWrite(t, filepath.Join(source, "state.txt"), "clean prerequisite\n")
	qa35cIndependentGit(t, source, "add", ".")
	qa35cIndependentGit(t, source, "commit", "-qm", "candidate")
	identity := qa35cIndependentGit(t, source, "rev-parse", "HEAD")
	qa35cIndependentWrite(t, filepath.Join(source, "state.txt"), "dirty development state\n")
	stage := filepath.Join(t.TempDir(), "stage")
	call := func(args ...string) ([]byte, error) {
		return exec.Command("sh", append([]string{script}, args...)...).CombinedOutput()
	}
	out, err := call("stage", "--source", source, "--snapshot", identity, "--output", stage)
	if err != nil {
		t.Fatalf("stage: %v\n%s", err, out)
	}
	var metadata struct {
		Snapshot   string `json:"snapshot"`
		BuildCount int    `json:"buildCount"`
	}
	qa35cIndependentOK(t, json.Unmarshal(out, &metadata))
	if metadata.Snapshot != identity || metadata.BuildCount != 1 {
		t.Fatalf("wrong stage identity/count: %s", out)
	}
	for _, suffix := range []string{"first", "second"} {
		copy := filepath.Join(t.TempDir(), suffix)
		out, err := call("copy", "--source", stage, "--output", copy)
		if err != nil {
			t.Fatalf("copy: %v\n%s", err, out)
		}
		state, err := os.ReadFile(filepath.Join(copy, "state.txt"))
		qa35cIndependentOK(t, err)
		if string(state) != "clean prerequisite\n" {
			t.Fatal("copy inherited mutable development or previous-case state")
		}
		out, err = exec.Command(filepath.Join(copy, "bin/formal-gates")).CombinedOutput()
		if err != nil || string(out) != "immutable candidate\n" {
			t.Fatalf("copied candidate is not independently runnable: %v %s", err, out)
		}
		qa35cIndependentWrite(t, filepath.Join(copy, "state.txt"), "case changed state\n")
		if out, err = call("copy", "--source", stage, "--output", copy); err == nil || !strings.Contains(string(out), "output must be a new directory") {
			t.Fatal("copy silently reused a dirty destination")
		}
	}
	if _, err := call("stage", "--source", source, "--snapshot", "missing-snapshot", "--output", filepath.Join(t.TempDir(), "bad")); err == nil {
		t.Fatal("invalid archive source masked by successful later command")
	}
	qa35cIndependentWrite(t, filepath.Join(source, "cmd/formal-gates/main.go"), "known invalid Go source\n")
	qa35cIndependentGit(t, source, "add", ".")
	qa35cIndependentGit(t, source, "commit", "-qm", "known build failure")
	bad := qa35cIndependentGit(t, source, "rev-parse", "HEAD")
	out, err = call("stage", "--source", source, "--snapshot", bad, "--output", filepath.Join(t.TempDir(), "bad-build"))
	if err == nil || strings.Contains(string(out), "\"buildCount\": 1") {
		t.Fatalf("known compile failure claimed prepared candidate: %v %s", err, out)
	}
}

func TestQA35CIndependentSealPreservesVerifiedIdentity(t *testing.T) {
	for _, commits := range []int{1, 2} {
		t.Run(fmt.Sprintf("%d commits", commits), func(t *testing.T) {
			root := t.TempDir()
			qa35cIndependentGit(t, root, "init", "-q")
			qa35cIndependentWrite(t, filepath.Join(root, ".gitignore"), ".gates/\nrequirement.md\n")
			qa35cIndependentGit(t, root, "add", ".")
			qa35cIndependentGit(t, root, "commit", "-qm", "base")
			s := qa35cIndependentState()
			s.BaseSnapshot = qa35cIndependentGit(t, root, "rev-parse", "HEAD")
			for i := 0; i < commits; i++ {
				qa35cIndependentWrite(t, filepath.Join(root, fmt.Sprintf("file%d.txt", i)), "committed change\n")
				qa35cIndependentGit(t, root, "add", ".")
				qa35cIndependentGit(t, root, "commit", "-qm", fmt.Sprintf("change %d", i))
			}
			s.CurrentSnapshot = qa35cIndependentGit(t, root, "rev-parse", "HEAD")
			s.Actions["development-worker"] = ActionResult{Status: developmentComplete}
			s.SelectedGates = []string{"blackbox"}
			s.QACasesByMode["blackbox"] = []QACase{{ID: "A", Mode: "blackbox", Description: "materialized independent case", Procedure: "real operation", Oracle: "observed result", ReviewStatus: "PASS"}}
			s.QADesignByMode["blackbox"] = ActionResult{Status: "PASS"}
			s.QAReviewByMode["blackbox"] = ActionResult{Status: "PASS"}
			s.QAExecutionByMode["blackbox"] = QAExecutionResult{Status: "PASS", Snapshot: s.CurrentSnapshot, Cases: []QAResultRecord{{CaseID: "A", Mode: "blackbox", Outcome: "PASS"}}}
			s.QACaseSets["blackbox"] = QACaseSetState{Status: caseSetFrozen, Version: "case-v1", BoundSnapshot: s.CurrentSnapshot}
			pkg, s := qa35cIndependentStore(t, root, s)
			// A documented multi-artifact run retains the pre-envelope route;
			// this fixture isolates Seal from the separately tested REQ/AC owner.
			qa35cIndependentWrite(t, filepath.Join(root, "solution.md"), "Independent fixture solution.\n")
			artifacts, err := requirementArtifactSet(root, s.RequirementSource, []string{"solution.md"})
			qa35cIndependentOK(t, err)
			s.RequirementArtifacts = artifacts
			qa35cIndependentOK(t, SaveRunState(root, s))
			verified := s.CurrentSnapshot
			_, err = Seal(root, pkg, s.RunID, nil, false, "must not be used for squash")
			if commits == 2 {
				if err == nil || !strings.Contains(err.Error(), "prepare-candidate") {
					t.Fatalf("unorganized candidate was not rejected: %v", err)
				}
			} else {
				qa35cIndependentOK(t, err)
				artifact, err := os.ReadFile(filepath.Join(root, ".gates/results", s.RunID+".blackbox-cases.md"))
				qa35cIndependentOK(t, err)
				if !strings.Contains(string(artifact), "materialized independent case") {
					t.Fatal("Seal lost existing case artifact")
				}
			}
			if qa35cIndependentGit(t, root, "rev-parse", "HEAD") != verified {
				t.Fatal("Seal changed candidate identity")
			}
		})
	}
}
