package validate

// Independent structural tests for the QA review rules. These tests exercise
// the owning rules with private fixtures; they do not use the shared QA test
// helpers. Persisted fixture states describe prerequisites, never target results.
import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/prompts"
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

func independentState() RunState {
	return RunState{RunID: "independent-fixture", Flow: "formal", Status: "ACTIVE", VCS: "git", BaseSnapshot: "base", CurrentSnapshot: "candidate", RequirementRevision: "requirement-v1", RequirementConfirmed: true, RouteMode: "custom",
		Actions: map[string]ActionResult{}, Gates: map[string]GateResult{}, Carry: map[string]CarryResult{}, Dispatches: map[string]PreparedDispatch{},
		QACasesByMode: map[string][]QACase{}, QACaseSets: map[string]QACaseSetState{}, QADesignByMode: map[string]ActionResult{}, QAReviewByMode: map[string]ActionResult{}, QAExecutionByMode: map[string]QAExecutionResult{},
		PriorQAExecutionByMode: map[string]*QAExecutionResult{}, ExecutionScopes: map[string]QAExecutionScope{}, PreDevelopmentReviewSeries: newPreDevelopmentReviewSeries()}
}

func independentOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

func independentWrite(t *testing.T, path, value string) {
	t.Helper()
	independentOK(t, os.MkdirAll(filepath.Dir(path), 0755))
	independentOK(t, os.WriteFile(path, []byte(value), 0600))
}

func independentGit(t *testing.T, root string, args ...string) string {
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

func independentStore(t *testing.T, root string, s RunState) (string, RunState) {
	t.Helper()
	pkg, err := filepath.Abs("../..")
	independentOK(t, err)
	independentWrite(t, filepath.Join(root, "requirement.md"), "Independent structural fixture requirement.\n")
	s.RequirementSource = "requirement.md"
	s.RequirementArtifacts, err = requirementArtifactSet(root, s.RequirementSource, nil)
	independentOK(t, err)
	catalog, err := prompts.LoadPromptCatalog(pkg)
	independentOK(t, err)
	s.CatalogRevision, s.BasePromptRevision, s.PromptHashes = catalog.CatalogRevision, catalog.BaseRevision, catalogPromptHashes(catalog)
	independentOK(t, SaveRunState(root, s))
	s, err = LoadRunState(root, s.RunID)
	independentOK(t, err)
	return pkg, s
}

func TestIndependentSharedApprovalInvalidation(t *testing.T) {
	s := independentState()
	s.QACaseSets["whitebox"] = QACaseSetState{SharedSteps: []QASharedStep{{ID: "prepare", Procedure: "create clean fixture", ReviewStatus: "PASS", ReviewReason: "checked once"}, {ID: "other", Procedure: "read separate fixture", ReviewStatus: "PASS"}}}
	cases := []QACase{{ID: "A", SharedSteps: []string{"prepare"}, ReviewStatus: "PASS", ApprovedSource: "prior"}, {ID: "B", SharedSteps: []string{"prepare"}, ReviewStatus: "PASS", ApprovedSource: "prior"}, {ID: "C", SharedSteps: []string{"other"}, ReviewStatus: "PASS", ApprovedSource: "prior"}}
	before := append([]QACase(nil), cases...)
	changed, err := mergeQASharedSteps(&s, "whitebox", []QASharedStep{{ID: "prepare", Procedure: "create clean fixture"}}, cases)
	independentOK(t, err)
	if changed || !reflect.DeepEqual(cases, before) {
		t.Fatal("unchanged shared preparation revoked approval")
	}
	changed, err = mergeQASharedSteps(&s, "whitebox", []QASharedStep{{ID: "prepare", Procedure: "create clean fixture with prerequisite"}}, cases)
	independentOK(t, err)
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

func TestIndependentSharedReviewExactSet(t *testing.T) {
	makeState := func() RunState {
		s := independentState()
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
	independentOK(t, reviewQASharedSteps(&s, "whitebox", []QAReviewInput{{CaseID: "prepare", Outcome: "FAIL", Reason: "required prerequisite absent"}}))
	if sharedStep(s, "whitebox", "prepare").ReviewStatus != "FAIL" || sharedStep(s, "whitebox", "approved").ReviewReason != "prior decision" {
		t.Fatal("per-record outcome or inheritance lost")
	}
}

func independentProgress() (RunState, []QACase, QAExecutionRecordOptions) {
	s := independentState()
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

func TestIndependentCheckpointResumePreservesResults(t *testing.T) {
	s, cases, o := independentProgress()
	rows, p, err := mergeQAExecutionProgress(s, "whitebox", cases, nil, o)
	independentOK(t, err)
	if len(rows) != 2 || rows[1].Outcome != "FAIL" || !reflect.DeepEqual(p.PendingCases, []string{"C"}) || len(p.ReuseReasons) != 2 {
		t.Fatalf("partial state misreported: %+v %+v", rows, p)
	}
	rows, p, err = mergeQAExecutionProgress(s, "whitebox", cases, []QAResultInput{{CaseID: "C", Outcome: "PASS", Procedure: "run third owner input", Observation: "third return is 3", OracleResult: "third result equals 3"}}, o)
	independentOK(t, err)
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
		bad, cc, oo := independentProgress()
		r := bad.QAExecutionByMode["whitebox"]
		r.Cases[0].Outcome = status
		bad.QAExecutionByMode["whitebox"] = r
		rows, p, err := mergeQAExecutionProgress(bad, "whitebox", cc, nil, oo)
		independentOK(t, err)
		if len(rows) != 1 || rows[0].CaseID != "B" || len(p.InvalidationReasons) != 1 {
			t.Fatal("unfinished/skip row was reused as completed")
		}
	}
}

func TestIndependentProgressBindingsAndEvidence(t *testing.T) {
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
			s, c, o := independentProgress()
			mutate(&s, c, &o)
			rows, p, err := mergeQAExecutionProgress(s, "whitebox", c, nil, o)
			independentOK(t, err)
			want := 0
			if name == "one case" || name == "shared prerequisite" || name == "incomplete evidence" || name == "sentinel evidence" {
				want = 1
			}
			if len(rows) != want || len(p.InvalidationReasons) != 2-want || (want == 1 && rows[0].CaseID != "B") {
				t.Fatalf("incorrect reuse boundary: %+v %+v", rows, p)
			}
		})
	}
	s, c, o := independentProgress()
	c[0].ReviewStatus = "PENDING"
	c[0].ApprovedSource = "new review metadata"
	set := s.QACaseSets["whitebox"]
	set.SharedSteps[0].ReviewReason = "fresh reviewer evidence"
	s.QACaseSets["whitebox"] = set
	rows, _, err := mergeQAExecutionProgress(s, "whitebox", c, nil, o)
	independentOK(t, err)
	if len(rows) != 2 {
		t.Fatal("review-only metadata changed execution meaning")
	}
	o.Tool = ""
	if _, _, err := mergeQAExecutionProgress(s, "whitebox", c, nil, o); err == nil {
		t.Fatal("resume without tool identity accepted")
	}
}

func TestIndependentSuggestionDispositionAndFreeze(t *testing.T) {
	makeState := func() RunState {
		s := independentState()
		s.QACasesByMode["whitebox"] = []QACase{{ID: "A", Mode: "whitebox", ReviewStatus: "PASS"}, {ID: "B", Mode: "whitebox", ReviewStatus: "PASS"}}
		s.QAReviewByMode["whitebox"] = ActionResult{Status: "PASS"}
		s.QACaseSets["whitebox"] = QACaseSetState{Status: caseSetReviewed, Suggestions: []QASuggestion{{ID: "suggestion-a", Status: "PENDING"}, {ID: "suggestion-b", Status: "PENDING"}}}
		return s
	}
	for _, decision := range []string{"ACCEPT", "DEFER", "DECLINE"} {
		t.Run(decision, func(t *testing.T) {
			root := t.TempDir()
			pkg, s := independentStore(t, root, makeState())
			s, err := RecordQASuggestion(root, pkg, s.RunID, "whitebox", "suggestion-a", decision, "optional wording is accounted for", nil)
			independentOK(t, err)
			if s.QACaseSets["whitebox"].Status == caseSetFrozen {
				t.Fatal("undisposed suggestion ignored")
			}
			s, err = RecordQASuggestion(root, pkg, s.RunID, "whitebox", "suggestion-b", "DEFER", "outside current acceptance", nil)
			independentOK(t, err)
			if s.QACaseSets["whitebox"].Status != caseSetFrozen || s.QACaseSets["whitebox"].BoundSnapshot != s.CurrentSnapshot {
				t.Fatal("disposed optional suggestions did not freeze")
			}
		})
	}
	for name, fields := range map[string][]string{"unknown": {"missing", "DEFER", "reason"}, "missing reason": {"suggestion-a", "DEFER", ""}, "invalid decision": {"suggestion-a", "IGNORE", "reason"}} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			pkg, s := independentStore(t, root, makeState())
			_, err := RecordQASuggestion(root, pkg, s.RunID, "whitebox", fields[0], fields[1], fields[2], nil)
			if err == nil {
				t.Fatal("invalid disposition accepted")
			}
		})
	}
	root := t.TempDir()
	pkg, s := independentStore(t, root, makeState())
	s, err := RecordQASuggestion(root, pkg, s.RunID, "whitebox", "suggestion-a", "ACCEPT", "changes A prerequisite", []string{"A"})
	independentOK(t, err)
	if s.QACasesByMode["whitebox"][0].ReviewStatus != "PENDING" || s.QACasesByMode["whitebox"][1].ReviewStatus != "PASS" || s.QACaseSets["whitebox"].Status != caseSetDraft || s.QAReviewByMode["whitebox"].Status != "PENDING" {
		t.Fatal("accepted semantic change did not reopen exact impact set")
	}
	set := s.QACaseSets["whitebox"]
	set.Suggestions = []QASuggestion{{Status: "DECLINE", Reason: "optional"}}
	if freezeQACaseSet(&s, "whitebox", &set, false) == nil {
		t.Fatal("suggestion disposition bypassed unapproved case")
	}
}

func TestIndependentWhiteboxSnapshotReuse(t *testing.T) {
	root := t.TempDir()
	independentGit(t, root, "init", "-q")
	independentWrite(t, filepath.Join(root, "existing_test.go"), "package existing\n")
	independentGit(t, root, "add", ".")
	independentGit(t, root, "commit", "-qm", "existing tests")
	s := independentState()
	s.CurrentSnapshot = independentGit(t, root, "rev-parse", "HEAD")
	independentOK(t, requireWhiteboxReviewSnapshot(root, s, "whitebox"))
	if independentGit(t, root, "rev-parse", "HEAD") != s.CurrentSnapshot {
		t.Fatal("reuse advanced snapshot")
	}
	independentWrite(t, filepath.Join(root, "existing_test.go"), "package changed\n")
	if requireWhiteboxReviewSnapshot(root, s, "whitebox") == nil {
		t.Fatal("uncommitted changed tests accepted")
	}
	independentGit(t, root, "add", ".")
	independentGit(t, root, "commit", "-qm", "correct assertion")
	if requireWhiteboxReviewSnapshot(root, s, "whitebox") == nil {
		t.Fatal("unregistered test snapshot accepted")
	}
	s.CurrentSnapshot = independentGit(t, root, "rev-parse", "HEAD")
	independentOK(t, requireWhiteboxReviewSnapshot(root, s, "whitebox"))
}

func TestIndependentPrepareCandidateIdentity(t *testing.T) {
	root := t.TempDir()
	independentGit(t, root, "init", "-q")
	independentWrite(t, filepath.Join(root, ".gitignore"), ".gates/\nrequirement.md\n")
	independentGit(t, root, "add", ".")
	independentGit(t, root, "commit", "-qm", "base")
	s := independentState()
	s.BaseSnapshot = independentGit(t, root, "rev-parse", "HEAD")
	for _, file := range []string{"product.go", "product_test.go"} {
		independentWrite(t, filepath.Join(root, file), "package product\n")
		independentGit(t, root, "add", ".")
		independentGit(t, root, "commit", "-qm", file)
	}
	s.CurrentSnapshot = independentGit(t, root, "rev-parse", "HEAD")
	old := s.CurrentSnapshot
	tree := independentGit(t, root, "rev-parse", "HEAD^{tree}")
	s.Actions["development-worker"] = ActionResult{Status: developmentComplete}
	s.SelectedGates = []string{"whitebox"}
	s.QACasesByMode["whitebox"] = []QACase{{ID: "A", Mode: "whitebox", ReviewStatus: "PASS"}}
	s.QACaseSets["whitebox"] = QACaseSetState{Status: caseSetFrozen, Version: "case-v1", BoundSnapshot: old}
	s.QAExecutionByMode["whitebox"] = QAExecutionResult{Status: "PASS", Snapshot: old}
	s.Gates["quality"] = GateResult{Status: "PASS", Snapshot: old}
	pkg, s := independentStore(t, root, s)
	if _, err := PrepareCandidate(root, pkg, s.RunID, ""); err == nil {
		t.Fatal("multiple commits without message accepted")
	}
	if independentGit(t, root, "rev-parse", "HEAD") != old {
		t.Fatal("rejected prepare changed candidate")
	}
	s, err := PrepareCandidate(root, pkg, s.RunID, "final QA candidate")
	independentOK(t, err)
	if s.CurrentSnapshot == old || independentGit(t, root, "rev-list", "--count", s.BaseSnapshot+"..HEAD") != "1" || independentGit(t, root, "rev-parse", "HEAD^{tree}") != tree {
		t.Fatal("candidate organization lost tree or did not squash")
	}
	if s.QAExecutionByMode["whitebox"].Status != "PENDING" || s.Gates["quality"].Status != "PENDING" || s.QACaseSets["whitebox"].BoundSnapshot != s.CurrentSnapshot || s.QACaseSets["whitebox"].Version != "case-v1" {
		t.Fatal("old verification reused or case identity unnecessarily changed")
	}
	id := s.CurrentSnapshot
	s, err = PrepareCandidate(root, pkg, s.RunID, "")
	independentOK(t, err)
	if s.CurrentSnapshot != id {
		t.Fatal("already organized candidate advanced")
	}
}

func TestIndependentCodexSourceProjection(t *testing.T) {
	root := t.TempDir()
	s := independentState()
	_, s = independentStore(t, root, s)
	independentOK(t, lifecycle.BeginRun(root, s.RunID))
	independentOK(t, lifecycle.BindDispatchWithProvider(root, s.RunID, "dispatch-meter", "native-thread", "codex"))
	path := filepath.Join(root, "rollout.jsonl")
	independentWrite(t, path, "{\"timestamp\":\"2026-09-01T00:00:00Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_started\",\"turn_id\":\"t\"}}\n{\"type\":\"token_usage_record\",\"payload\":{\"response_id\":\"response-1\",\"usage\":{\"input_tokens\":90,\"cached_input_tokens\":30,\"output_tokens\":8}}}\n{\"timestamp\":\"2026-09-01T00:00:04Z\",\"type\":\"event_msg\",\"payload\":{\"type\":\"task_complete\",\"turn_id\":\"t\"}}\n")
	for _, event := range []string{"SubagentStart", "SubagentStop"} {
		payload, err := json.Marshal(map[string]string{"thread_id": "native-thread", "cwd": root, "rollout_path": path})
		independentOK(t, err)
		_, err = lifecycle.Capture(root, "codex", event, payload)
		independentOK(t, err)
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
func TestIndependentHistoricalCheckpointOracle(t *testing.T) {
	data, err := os.ReadFile("review_contract_test.go")
	independentOK(t, err)
	fset := token.NewFileSet()
	parsed, err := parser.ParseFile(fset, "checkpoint.go", data, 0)
	independentOK(t, err)
	var function string
	for _, decl := range parsed.Decls {
		if f, ok := decl.(*ast.FuncDecl); ok && f.Name.Name == "TestCheckpointDoesNotImplementStage4OrRewriteHistory" {
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
			independentGit(t, root, "init", "-q")
			independentWrite(t, filepath.Join(root, "refactor-plan/stage-records.md"), "historical base\n")
			independentGit(t, root, "add", ".")
			independentGit(t, root, "commit", "-qm", "base")
			base := independentGit(t, root, "rev-parse", "HEAD")
			independentWrite(t, filepath.Join(root, scenario.path), "checkpoint change\n")
			independentWrite(t, filepath.Join(root, "refactor-plan/stage-records.md"), scenario.document)
			independentGit(t, root, "add", ".")
			independentGit(t, root, "commit", "-qm", "delivered")
			delivered := independentGit(t, root, "rev-parse", "HEAD")
			independentWrite(t, filepath.Join(root, "refactor-plan/stage-records.md"), "later current document rewrite\n")
			body := strings.ReplaceAll(strings.ReplaceAll(function, "860ac5eefe1c501aa9caae42de788af05a7f6809", base), "b7eab3b16036a0a5fdeb32efc33c9d93d740392c", delivered)
			harness := fmt.Sprintf("package checkpoint\nimport (\"testing\";\"strings\";\"os/exec\")\nfunc qaPackage(t *testing.T) string{return %q}\nfunc qaGit(t *testing.T,root string,args ...string)string{t.Helper();c:=exec.Command(\"git\",args...);c.Dir=root;b,e:=c.CombinedOutput();if e!=nil{t.Fatalf(\"git: %%v %%s\",e,b)};return strings.TrimSpace(string(b))}\n", root) + body + "\n"
			module := t.TempDir()
			independentWrite(t, filepath.Join(module, "go.mod"), "module checkpoint-fixture\ngo 1.22\n")
			independentWrite(t, filepath.Join(module, "checkpoint_test.go"), harness)
			c := exec.Command("go", "test", "-count=1", "-run", "^TestCheckpointDoesNotImplementStage4OrRewriteHistory$", "-v", ".")
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

func TestIndependentSealPreservesVerifiedIdentity(t *testing.T) {
	for _, commits := range []int{1, 2} {
		t.Run(fmt.Sprintf("%d commits", commits), func(t *testing.T) {
			root := t.TempDir()
			independentGit(t, root, "init", "-q")
			independentWrite(t, filepath.Join(root, ".gitignore"), ".gates/\nrequirement.md\n")
			independentGit(t, root, "add", ".")
			independentGit(t, root, "commit", "-qm", "base")
			s := independentState()
			s.BaseSnapshot = independentGit(t, root, "rev-parse", "HEAD")
			for i := 0; i < commits; i++ {
				independentWrite(t, filepath.Join(root, fmt.Sprintf("file%d.txt", i)), "committed change\n")
				independentGit(t, root, "add", ".")
				independentGit(t, root, "commit", "-qm", fmt.Sprintf("change %d", i))
			}
			s.CurrentSnapshot = independentGit(t, root, "rev-parse", "HEAD")
			s.Actions["development-worker"] = ActionResult{Status: developmentComplete}
			s.SelectedGates = []string{"blackbox"}
			s.QACasesByMode["blackbox"] = []QACase{{ID: "A", Mode: "blackbox", Description: "materialized independent case", Procedure: "real operation", Oracle: "observed result", ReviewStatus: "PASS"}}
			s.QADesignByMode["blackbox"] = ActionResult{Status: "PASS"}
			s.QAReviewByMode["blackbox"] = ActionResult{Status: "PASS"}
			s.QAExecutionByMode["blackbox"] = QAExecutionResult{Status: "PASS", Snapshot: s.CurrentSnapshot, Cases: []QAResultRecord{{CaseID: "A", Mode: "blackbox", Outcome: "PASS"}}}
			s.QACaseSets["blackbox"] = QACaseSetState{Status: caseSetFrozen, Version: "case-v1", BoundSnapshot: s.CurrentSnapshot}
			pkg, s := independentStore(t, root, s)
			// A documented multi-artifact run retains the pre-envelope route;
			// this fixture isolates Seal from the separately tested REQ/AC owner.
			independentWrite(t, filepath.Join(root, "solution.md"), "Independent fixture solution.\n")
			artifacts, err := requirementArtifactSet(root, s.RequirementSource, []string{"solution.md"})
			independentOK(t, err)
			s.RequirementArtifacts = artifacts
			independentOK(t, SaveRunState(root, s))
			verified := s.CurrentSnapshot
			_, err = Seal(root, pkg, s.RunID, nil, false, "must not be used for squash")
			if commits == 2 {
				if err == nil || !strings.Contains(err.Error(), "prepare-candidate") {
					t.Fatalf("unorganized candidate was not rejected: %v", err)
				}
			} else {
				independentOK(t, err)
				artifact, err := os.ReadFile(filepath.Join(root, ".gates/results", s.RunID+".blackbox-cases.md"))
				independentOK(t, err)
				if !strings.Contains(string(artifact), "materialized independent case") {
					t.Fatal("Seal lost existing case artifact")
				}
			}
			if independentGit(t, root, "rev-parse", "HEAD") != verified {
				t.Fatal("Seal changed candidate identity")
			}
		})
	}
}
