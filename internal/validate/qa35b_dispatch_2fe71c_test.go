package validate

// Independent structural QA for dispatch-2fe71c878cf4bb6c672661a8.
// Fixtures describe documented intermediate states at their owning layer.
// Persisted fixtures use the production writer only in t.TempDir; no real run,
// manually edited state file, prior test helper or prior test suite is consumed.
import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"formal-gates/internal/cost"
)

func qa35bState() RunState {
	s := RunState{RunID: "qa35b", Status: "ACTIVE", Flow: "formal", VCS: "git", BaseSnapshot: "base", CurrentSnapshot: "candidate", RequirementRevision: "req-r1", RequirementConfirmed: true,
		StateIntegrity: "state-r1", RouteMode: "custom", Actions: map[string]ActionResult{}, Gates: map[string]GateResult{}, Carry: map[string]CarryResult{},
		Dispatches: map[string]PreparedDispatch{}, DispatchObligations: map[string]DispatchObligation{}, QACasesByMode: map[string][]QACase{},
		QADesignByMode: map[string]ActionResult{}, QAReviewByMode: map[string]ActionResult{}, QAExecutionByMode: map[string]QAExecutionResult{},
		PriorQAExecutionByMode: map[string]*QAExecutionResult{}, ExecutionScopes: map[string]QAExecutionScope{},
		PreDevelopmentReviewSeries: newPreDevelopmentReviewSeries(), NeedsReReview: map[string]string{}, ReReviewDispatch: map[string]string{}, ReviewOverrides: map[string]string{}}
	ensureConvergenceState(&s)
	return s
}

func qa35bJSON(t *testing.T, value any) string {
	t.Helper()
	b, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
func qa35bErr(t *testing.T, err error, part string) {
	t.Helper()
	if err == nil || !strings.Contains(err.Error(), part) {
		t.Fatalf("want error containing %q, got %v", part, err)
	}
}
func qa35bOK(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func qa35bPackage(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("../..")
	qa35bOK(t, err)
	return p
}
func qa35bRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	qa35bOK(t, err)
	return string(b)
}
func qa35bWrite(t *testing.T, path, data string) {
	t.Helper()
	qa35bOK(t, os.WriteFile(path, []byte(data), 0600))
}

func qa35bStore(t *testing.T, s RunState) (string, string, RunState) {
	t.Helper()
	root, pkg := t.TempDir(), qa35bPackage(t)
	qa35bWrite(t, filepath.Join(root, "requirement.md"), "QA structural fixture: documented normal workflow.\n")
	artifacts, err := requirementArtifactSet(root, "requirement.md", nil)
	qa35bOK(t, err)
	s.RequirementArtifacts = artifacts
	s.RequirementSource = "requirement.md"
	catalog, err := LoadPromptCatalog(pkg)
	qa35bOK(t, err)
	s.CatalogRevision, s.BasePromptRevision, s.PromptHashes = catalog.CatalogRevision, catalog.BaseRevision, catalogPromptHashes(catalog)
	qa35bOK(t, SaveRunState(root, s))
	s, err = LoadRunState(root, s.RunID)
	qa35bOK(t, err)
	ensureConvergenceState(&s)
	return root, pkg, s
}

func qa35bFinding(t *testing.T) (string, FindingInput) {
	t.Helper()
	root := t.TempDir()
	qa35bWrite(t, filepath.Join(root, "entry.go"), "package example\n// public entry\n")
	return root, FindingInput{Severity: "P1", SeverityRationale: "REQ-001: the documented record command cannot accept valid evidence, so every normal gate result is blocked; no low-cost P2 workaround exists", Requirement: "REQ-001", NormalPath: "workflow record-gate with complete normal result", ActualError: "complete result is rejected", DeferralImpact: "delivery cannot record its required gate result", Message: "complete gate record is rejected", Locations: []string{"entry.go:2"},
		Verification: FindingVerification{Scope: "documented record command", Severity: "core acceptance blocked without workaround", Causality: "record validator rejects the valid branch", Reproduction: "invoke the documented record command with a complete result", Classification: "core acceptance entry is missing"}}
}

func TestQA35BGateRequiresEveryBlockingField(t *testing.T) {
	root, complete := qa35bFinding(t)
	for name, omit := range map[string]func(*FindingInput){
		"severityRationale": func(f *FindingInput) { f.SeverityRationale = "" }, "requirement": func(f *FindingInput) { f.Requirement = "" },
		"normalPath": func(f *FindingInput) { f.NormalPath = "" }, "actualError": func(f *FindingInput) { f.ActualError = "" },
		"deferralImpact": func(f *FindingInput) { f.DeferralImpact = "" }, "locations": func(f *FindingInput) { f.Locations = nil },
		"verification.scope": func(f *FindingInput) { f.Verification.Scope = "" }, "verification.severity": func(f *FindingInput) { f.Verification.Severity = "" },
		"verification.causality": func(f *FindingInput) { f.Verification.Causality = "" }, "verification.reproduction": func(f *FindingInput) { f.Verification.Reproduction = "" },
	} {
		t.Run(name, func(t *testing.T) {
			f := complete
			omit(&f)
			got, err := strictFinding(f, "", true, root, qa35bState())
			var ce *ResultContractError
			if !errors.As(err, &ce) || !containsExactString(ce.Fields, name) || got.ID != "" {
				t.Fatalf("got %+v, err %+v", got, err)
			}
		})
	}
}

func TestQA35BFindingLocationsResolve(t *testing.T) {
	root, f := qa35bFinding(t)
	for _, location := range []string{"missing.go:1", "entry.go:99"} {
		f.Locations = []string{location}
		_, err := strictFinding(f, "", true, root, qa35bState())
		var ce *ResultContractError
		if !errors.As(err, &ce) || !reflect.DeepEqual(ce.InvalidLocations, []string{location}) {
			t.Fatalf("location=%s err=%+v", location, err)
		}
	}
}

func TestQA35BCompleteGateEvidenceIsRetained(t *testing.T) {
	root, f := qa35bFinding(t)
	status, findings, err := validateStrictSemanticResult("", "FAIL", "", []FindingInput{f}, true, root, qa35bState())
	qa35bOK(t, err)
	if status != "FAIL" || len(findings) != 1 {
		t.Fatalf("%s %+v", status, findings)
	}
	got := findings[0]
	if got.ID == "" || got.Requirement != f.Requirement || got.SeverityRationale != f.SeverityRationale || got.NormalPath != f.NormalPath || got.ActualError != f.ActualError || got.DeferralImpact != f.DeferralImpact || !reflect.DeepEqual(got.Verification, f.Verification) || !reflect.DeepEqual(got.Locations, f.Locations) {
		t.Fatalf("evidence lost: %+v", got)
	}
}

func TestQA35BAdvisoriesNeedNoBlockingEvidence(t *testing.T) {
	for _, severity := range []string{"P2", "P3"} {
		t.Run(severity, func(t *testing.T) {
			status, findings, err := validateStrictSemanticResult("", "PASS", "", []FindingInput{{Severity: severity, Message: "optional clarification"}}, true, t.TempDir(), qa35bState())
			qa35bOK(t, err)
			if status != "PASS" || len(findings) != 1 || findings[0].Severity != severity {
				t.Fatalf("advisory upgraded: %s %+v", status, findings)
			}
		})
	}
}

func TestQA35BReadinessClassificationMatrix(t *testing.T) {
	root, complete := qa35bFinding(t)
	for _, severity := range []string{"P0", "P1", "P2", "P3"} {
		t.Run(severity, func(t *testing.T) {
			f := complete
			f.Severity = severity
			f.ActualError = ""
			_, err := strictFinding(f, "start-readiness", false, root, qa35bState())
			if err == nil {
				t.Fatal("missing category accepted")
			}
			for _, category := range []string{FindingCategoryRequirementConflict, FindingCategoryResponsibility, FindingCategoryMissingEntry, FindingCategoryImplementation, FindingCategoryOther} {
				f.Category = category
				_, err = strictFinding(f, "start-readiness", false, root, qa35bState())
				if (severity == "P0" || severity == "P1") && (category == FindingCategoryImplementation || category == FindingCategoryOther) {
					qa35bErr(t, err, "at most P2")
				} else {
					qa35bOK(t, err)
				}
			}
		})
	}
}

func TestQA35BReadinessNeedsIndependentClassification(t *testing.T) {
	root, f := qa35bFinding(t)
	f.Category = FindingCategoryMissingEntry
	f.ActualError = ""
	_, err := strictFinding(f, "start-readiness", false, root, qa35bState())
	qa35bOK(t, err)
	f.Verification.Classification = ""
	_, err = strictFinding(f, "start-readiness", false, root, qa35bState())
	var ce *ResultContractError
	if !errors.As(err, &ce) || !containsExactString(ce.Fields, "verification.classification") {
		t.Fatalf("%v", err)
	}
}

func TestQA35BSeverityRationaleIsPerFinding(t *testing.T) {
	root, f := qa35bFinding(t)
	for _, action := range []string{"product-review", "start-readiness", "qa-review", "gate"} {
		t.Run(action, func(t *testing.T) {
			first, second := f, f
			first.Category, second.Category = FindingCategoryRequirementConflict, FindingCategoryRequirementConflict
			second.Message = "second independent result"
			for _, empty := range []string{"", "P0", "P1", "item fail", "requirement violated"} {
				second.SeverityRationale = empty
				_, _, err := validateStrictSemanticResult(action, "FAIL", "batch explanation", []FindingInput{first, second}, action == "gate", root, qa35bState())
				if err == nil {
					t.Fatalf("accepted second rationale %q", empty)
				}
			}
		})
	}
}

func qa35bAttempt(t *testing.T, s *RunState, id, key, kind, target, mode string) PreparedDispatch {
	t.Helper()
	attempt, err := registerDispatchAttempt(s, id, key, "sig-"+key, kind, target, mode)
	qa35bOK(t, err)
	d := PreparedDispatch{ID: id, TargetKind: kind, Target: target, Mode: mode, TaskKey: key, Signature: "sig-" + key, Attempt: attempt, Status: "CLAIMED", SourceSnapshot: s.CurrentSnapshot, RequirementRevision: s.RequirementRevision}
	s.Dispatches[id] = d
	return d
}

func TestQA35BRejectedReceiptIsOnlyCommittedResult(t *testing.T) {
	for _, target := range []string{"product-review", "start-readiness", "qa-review", "gate"} {
		t.Run(target, func(t *testing.T) {
			s := qa35bState()
			kind := "action"
			if target == "gate" {
				kind = "gate"
			}
			d := qa35bAttempt(t, &s, "dispatch-one", "task", kind, target, "")
			root, _, s := qa35bStore(t, s)
			beforeActions := qa35bJSON(t, s.Actions)
			beforeGates := qa35bJSON(t, s.Gates)
			got, err := mutateRun(root, s.RunID, func(s *RunState) error {
				return rejectInvalidResult(s, d, &ResultContractError{Reason: "missing per-finding evidence", Fields: []string{"severityRationale"}, InvalidLocations: []string{"absent.go:2"}})
			})
			qa35bErr(t, err, "REJECTED_INVALID_RESULT")
			if len(got.RejectedResults) != 1 || got.Dispatches[d.ID].Status != "REJECTED_INVALID_RESULT" || got.DispatchObligations[d.TaskKey].Status != "RETRYABLE" || qa35bJSON(t, got.Actions) != beforeActions || qa35bJSON(t, got.Gates) != beforeGates || len(got.Problems) != 0 || len(got.PostReviewFindings) != 0 {
				t.Fatalf("invalid result leaked: %+v", got)
			}
			r := got.RejectedResults[0]
			if r.DispatchID != d.ID || r.Attempt != 1 || r.TaskKey != d.TaskKey || r.Signature != d.Signature || r.Target != target || !reflect.DeepEqual(r.RejectedFields, []string{"severityRationale"}) || !reflect.DeepEqual(r.InvalidLocations, []string{"absent.go:2"}) {
				t.Fatalf("receipt misbound %+v", r)
			}
		})
	}
}

func TestQA35BBoundedRetriesWorkOutsideWave(t *testing.T) {
	for _, target := range []string{"product-review", "start-readiness", "qa-review", "qa-execution", "gate"} {
		t.Run(target, func(t *testing.T) {
			s := qa35bState()
			first := qa35bAttempt(t, &s, "first", "same-key", "action", target, "")
			finishDispatchAttempt(&s, first.ID, "RUNTIME_ERROR", "worker stopped")
			second := qa35bAttempt(t, &s, "second", "same-key", "action", target, "")
			finishDispatchAttempt(&s, second.ID, "REJECTED_INVALID_RESULT", "missing required field")
			if first.TaskKey != second.TaskKey || second.Attempt != 2 || len(s.DispatchObligations) != 1 || s.DispatchObligations[first.TaskKey].Status != "RETRY_EXHAUSTED" {
				t.Fatalf("retry projection %+v", s.DispatchObligations)
			}
			_, err := registerDispatchAttempt(&s, "third", "same-key", "sig-same-key", "action", target, "")
			qa35bErr(t, err, "RETRY_EXHAUSTED")
			if len(s.Dispatches) != 2 || len(s.Problems) != 0 || s.CompletedReviewWaves != 0 {
				t.Fatal("retry manufactured results or capacity")
			}
		})
	}
}

func TestQA35BExactlyOneAdditionalAttemptAuthorization(t *testing.T) {
	s := qa35bState()
	for _, id := range []string{"first", "second"} {
		d := qa35bAttempt(t, &s, id, "key", "action", "qa-review", "whitebox")
		finishDispatchAttempt(&s, d.ID, "RUNTIME_ERROR", "service unavailable")
	}
	root, pkg, s := qa35bStore(t, s)
	path := RunStatePath(root, s.RunID)
	before := qa35bRead(t, path)
	_, err := AuthorizeDispatchRetry(root, pkg, s.RunID, "key", "old", "allow final retry")
	qa35bErr(t, err, "stale")
	_, err = AuthorizeDispatchRetry(root, pkg, s.RunID, "key", s.StateIntegrity, "")
	qa35bErr(t, err, "non-empty reason")
	if qa35bRead(t, path) != before {
		t.Fatal("invalid authorization changed state")
	}
	s, err = AuthorizeDispatchRetry(root, pkg, s.RunID, "key", s.StateIntegrity, "user authorizes final retry")
	qa35bOK(t, err)
	o := s.DispatchObligations["key"]
	if o.MaxAttempts != 3 || o.AttemptsUsed != 2 || !o.ExtraAttemptAuthorized || o.Status != "RETRYABLE" {
		t.Fatalf("%+v", o)
	}
	oldDigest := o.BasisDigest
	s, err = mutateRun(root, s.RunID, func(s *RunState) error {
		d := qa35bAttempt(t, s, "third", "key", "action", "qa-review", "whitebox")
		finishDispatchAttempt(s, d.ID, "RUNTIME_ERROR", "still unavailable")
		return nil
	})
	qa35bOK(t, err)
	if s.DispatchObligations["key"].BasisDigest == oldDigest || s.DispatchObligations["key"].Status != "RETRY_EXHAUSTED" || !strings.Contains(s.DispatchObligations["key"].NextStep, "abort/cleanup") {
		t.Fatal("exhaustion not terminal")
	}
	_, err = AuthorizeDispatchRetry(root, pkg, s.RunID, "key", s.StateIntegrity, "again")
	qa35bErr(t, err, "one additional")
}

func TestQA35BDispatchSignatureBindsAuthoritativeInputs(t *testing.T) {
	s := qa35bState()
	s.QACasesByMode["whitebox"] = []QACase{{ID: "CASE-A", Description: "first"}}
	c := PromptCatalog{CatalogRevision: "catalog", BaseRevision: "prompt"}
	key, signature := dispatchIdentity(s, c, "action", "qa-design", "whitebox", "input")
	for _, name := range []string{"kind", "target", "mode", "base", "current", "requirement", "case", "catalog", "prompt", "input"} {
		t.Run(name, func(t *testing.T) {
			copy := qa35bState()
			copy.QACasesByMode["whitebox"] = []QACase{{ID: "CASE-A", Description: "first"}}
			catalog := c
			kind, target, mode, input := "action", "qa-design", "whitebox", "input"
			switch name {
			case "kind":
				kind = "gate"
			case "target":
				target = "qa-review"
			case "mode":
				mode = "blackbox"
			case "base":
				copy.BaseSnapshot = "other"
			case "current":
				copy.CurrentSnapshot = "other"
			case "requirement":
				copy.RequirementRevision = "other"
			case "case":
				copy.QACasesByMode[mode][0].Description = "changed"
			case "catalog":
				catalog.CatalogRevision = "other"
			case "prompt":
				copy.BasePromptRevision = "other"
			case "input":
				input = "other"
			}
			k, sig := dispatchIdentity(copy, catalog, kind, target, mode, input)
			if k == key || sig == signature {
				t.Fatal("identity omitted")
			}
		})
	}
}

func TestQA35BInFlightDispatchReusesCanonicalPrompt(t *testing.T) {
	s := qa35bState()
	path := filepath.Join(t.TempDir(), "prompt.md")
	qa35bWrite(t, path, "canonical prompt\n")
	for _, status := range []string{"OPEN", "CLAIMED"} {
		s.Dispatches = map[string]PreparedDispatch{"one": {ID: "one", Status: status, Signature: "same", PromptFile: path}}
		before := qa35bJSON(t, s)
		got, reused, err := duplicateDispatchPrompt(s, "same")
		qa35bOK(t, err)
		if !reused || got != "canonical prompt\n" || qa35bJSON(t, s) != before {
			t.Fatal("in-flight duplicate changed state")
		}
	}
}

func TestQA35BFinalDispatchDedupRoutesByResponsibility(t *testing.T) {
	for _, target := range []string{"product-review", "start-readiness", "qa-review", "qa-execution", "gate"} {
		t.Run(target, func(t *testing.T) {
			s := qa35bState()
			kind := "action"
			if target == "gate" {
				kind = "gate"
			}
			d := PreparedDispatch{ID: "one", Status: "COMPLETED", Signature: "same", ResultStatus: "FAIL", TargetKind: kind, Target: target}
			s.Dispatches[d.ID] = d
			before := qa35bJSON(t, s)
			_, reused, err := duplicateDispatchPrompt(s, "same")
			qa35bErr(t, err, "authoritative FAIL")
			want := "unified repair"
			if target == "product-review" || target == "start-readiness" {
				want = "settle the existing findings"
			}
			if target == "qa-review" {
				want = "QA Design"
			}
			qa35bErr(t, err, want)
			if reused || qa35bJSON(t, s) != before {
				t.Fatal("FAIL dedup changed counters or created work")
			}
			d.ResultStatus = "PASS"
			s.Dispatches[d.ID] = d
			_, _, err = duplicateDispatchPrompt(s, "same")
			qa35bErr(t, err, "authoritative PASS")
		})
	}
}

func qa35bApproved(s *RunState, mode string) {
	if s.QACasesByMode == nil {
		s.QACasesByMode = map[string][]QACase{}
	}
	if s.QADesignByMode == nil {
		s.QADesignByMode = map[string]ActionResult{}
	}
	if s.QAReviewByMode == nil {
		s.QAReviewByMode = map[string]ActionResult{}
	}
	s.SelectedGates = appendUniqueStrings(s.SelectedGates, mode)
	s.QACasesByMode[mode] = []QACase{{ID: "CASE-A", Mode: mode, Description: "accepts valid input", Procedure: "call owned function", Oracle: "returns expected value", Test: "independent.go::A", ReviewStatus: "PASS"}, {ID: "CASE-B", Mode: mode, Description: "rejects omitted argument", Procedure: "call owned function without required input", Oracle: "returns required-input error", Test: "independent.go::B", ReviewStatus: "PASS"}}
	s.QADesignByMode[mode], s.QAReviewByMode[mode] = ActionResult{Status: "PASS"}, ActionResult{Status: "PASS"}
}

func TestQA35BQAReviewNoSuggestionsFreezesImmediately(t *testing.T) {
	s := qa35bState()
	qa35bApproved(&s, "whitebox")
	qa35bOK(t, recordQAReviewCaseSet(&s, "whitebox", ActionResult{Status: "PASS"}, PreparedDispatch{ID: "review"}))
	c := s.QACaseSets["whitebox"]
	if c.Status != "FROZEN" || c.Version == "" || c.BoundSnapshot != "candidate" || c.PendingSnapshotAdvance || len(c.Suggestions) != 0 {
		t.Fatalf("%+v", c)
	}
}

func TestQA35BQAReviewFailureDoesNotFreeze(t *testing.T) {
	s := qa35bState()
	qa35bApproved(&s, "whitebox")
	qa35bOK(t, recordQAReviewCaseSet(&s, "whitebox", ActionResult{Status: "FAIL", Findings: []Finding{{Severity: "P1", Message: "missing core coverage"}}}, PreparedDispatch{ID: "review"}))
	c := s.QACaseSets["whitebox"]
	if c.Status != "DRAFT" || c.Version != "" || c.BoundSnapshot != "" {
		t.Fatalf("FAIL froze %+v", c)
	}
	s.QACasesByMode["whitebox"][0].ReviewStatus = "FAIL"
	err := freezeQACaseSet(&s, "whitebox", &c, false)
	qa35bErr(t, err, "CASE-A is FAIL")
}

func qa35bSuggestions(t *testing.T) RunState {
	t.Helper()
	s := qa35bState()
	qa35bApproved(&s, "whitebox")
	qa35bOK(t, recordQAReviewCaseSet(&s, "whitebox", ActionResult{Status: "PASS", Findings: []Finding{{ID: "first", Severity: "P2", Message: "include empty input"}, {ID: "second", Severity: "P3", Message: "clarify evidence"}}}, PreparedDispatch{ID: "review"}))
	return s
}

// Stage 3.5c keeps the stable suggestion/freeze boundary, but accepts reasoned
// optional dispositions without manufacturing changed tests or a new snapshot.
func TestQA35BSuggestionsDelayFreezeUntilDisposed(t *testing.T) {
	s := qa35bSuggestions(t)
	c := s.QACaseSets["whitebox"]
	if c.Status != caseSetReviewed || len(c.Suggestions) != 2 || c.Suggestions[0].ID == c.Suggestions[1].ID {
		t.Fatal("stable suggestions missing")
	}
	if err := freezeQACaseSet(&s, "whitebox", &c, false); err == nil {
		t.Fatal("pending suggestions froze")
	}
	c.Suggestions[0].Status, c.Suggestions[0].Reason = "DEFER", "existing coverage sufficient"
	if err := freezeQACaseSet(&s, "whitebox", &c, false); err == nil {
		t.Fatal("omitted pending suggestion froze")
	}
	c.Suggestions[1].Status = "DECLINE"
	if err := freezeQACaseSet(&s, "whitebox", &c, false); err == nil {
		t.Fatal("missing disposition reason froze")
	}
	c.Suggestions[1].Reason = "existing evidence is explicit"
	qa35bOK(t, freezeQACaseSet(&s, "whitebox", &c, false))
	if c.Status != caseSetFrozen || c.PendingSnapshotAdvance || c.BoundSnapshot != s.CurrentSnapshot {
		t.Fatal("optional disposition required a needless snapshot")
	}
	s.QACaseSets["whitebox"] = c
	s.CurrentSnapshot = "with-tests"
	bindFrozenCaseSetsToSnapshot(&s)
	if s.QACaseSets["whitebox"].Version != c.Version || s.QACaseSets["whitebox"].BoundSnapshot != "with-tests" {
		t.Fatal("snapshot binding failed")
	}
}

func TestQA35BSnapshotRebindPreservesFrozenCaseIdentity(t *testing.T) {
	s := qa35bState()
	qa35bApproved(&s, "whitebox")
	qa35bOK(t, recordQAReviewCaseSet(&s, "whitebox", ActionResult{Status: "PASS"}, PreparedDispatch{ID: "review"}))
	cases, version := qa35bJSON(t, s.QACasesByMode), s.QACaseSets["whitebox"].Version
	s.CurrentSnapshot = "ordinary-fix"
	bindFrozenCaseSetsToSnapshot(&s)
	if s.QACaseSets["whitebox"].Version != version || qa35bJSON(t, s.QACasesByMode) != cases || s.QACaseSets["whitebox"].Status != "FROZEN" {
		t.Fatal("ordinary code change reopened QA")
	}
}

func TestQA35BLegalThawInvalidatesOnlyAffectedApproval(t *testing.T) {
	for _, category := range []string{"REQUIREMENT_MEANING", "EXTERNAL_BEHAVIOR", "SUCCESS_FAILURE_CRITERIA", "CASE_EXECUTABILITY"} {
		t.Run(category, func(t *testing.T) {
			s := qa35bState()
			qa35bApproved(&s, "whitebox")
			qa35bOK(t, recordQAReviewCaseSet(&s, "whitebox", ActionResult{Status: "PASS"}, PreparedDispatch{ID: "review"}))
			s.QAExecutionByMode["whitebox"] = QAExecutionResult{Status: "PASS", Snapshot: s.CurrentSnapshot}
			s.Carry["whitebox"] = CarryResult{Decision: "INHERIT"}
			root, pkg, s := qa35bStore(t, s)
			got, err := ThawQACaseSet(root, pkg, s.RunID, "whitebox", category, "stable documented acceptance changed", "req-r2", []string{"CASE-A"})
			qa35bOK(t, err)
			c := got.QACaseSets["whitebox"]
			if c.Status != "THAWED" || c.Version != "" || c.BoundSnapshot != "" || len(c.Thaws) != 1 || c.Thaws[0].Category != category || got.QACasesByMode["whitebox"][0].ReviewStatus != "PENDING" || got.QACasesByMode["whitebox"][1].ReviewStatus != "PASS" || got.QADesignByMode["whitebox"].Status != "PENDING" || got.QAReviewByMode["whitebox"].Status != "PENDING" {
				t.Fatalf("bad thaw %+v", got.QACaseSets)
			}
			if _, exists := got.QAExecutionByMode["whitebox"]; exists {
				t.Fatal("old execution survived")
			}
			if _, exists := got.Carry["whitebox"]; exists {
				t.Fatal("old carry survived")
			}
		})
	}
}

func TestQA35BTransientEnvironmentCannotThaw(t *testing.T) {
	s := qa35bState()
	qa35bApproved(&s, "whitebox")
	qa35bOK(t, recordQAReviewCaseSet(&s, "whitebox", ActionResult{Status: "PASS"}, PreparedDispatch{ID: "review"}))
	root, pkg, s := qa35bStore(t, s)
	path := RunStatePath(root, s.RunID)
	before := qa35bRead(t, path)
	for _, category := range []string{"RUNTIME_ERROR", "IMPLEMENTATION_CHANGE", ""} {
		_, err := ThawQACaseSet(root, pkg, s.RunID, "whitebox", category, "worker temporarily unavailable", "candidate", []string{"CASE-A"})
		qa35bErr(t, err, "thaw category")
	}
	if qa35bRead(t, path) != before {
		t.Fatal("invalid thaw changed state")
	}
}

func TestQA35BAffectedScopeIncludesPriorFailures(t *testing.T) {
	s := qa35bState()
	qa35bApproved(&s, "whitebox")
	s.QACaseSets["whitebox"] = QACaseSetState{Status: caseSetFrozen}
	s.PreRepairSnapshot = "base"
	prior := QAExecutionResult{Status: "FAIL", Snapshot: "base", Cases: []QAResultRecord{{CaseID: "CASE-A", Mode: "whitebox", Outcome: "FAIL"}, {CaseID: "CASE-B", Mode: "whitebox", Outcome: "PASS"}}}
	qa35bErr(t, recordExecutionScope(&s, "whitebox", "AFFECTED", []string{"CASE-B"}, "fix", "PREPARE", "base", prior), "prior FAIL case CASE-A")
	qa35bErr(t, recordExecutionScope(&s, "whitebox", "FULL", nil, "fix", "PREPARE", "base", prior), "ordinary intermediate repair")
	qa35bOK(t, recordExecutionScope(&s, "whitebox", "AFFECTED", []string{"CASE-A", "CASE-B"}, "both paths affected", "PREPARE", "base", prior))
	if sc := s.ExecutionScopes["whitebox"]; sc.Origin != "MAIN_AGENT" || sc.Decision != "AFFECTED" || !reflect.DeepEqual(sc.CaseIDs, []string{"CASE-A", "CASE-B"}) {
		t.Fatalf("%+v", sc)
	}
}

func TestQA35BAffectedScopeRejectsBadSubsets(t *testing.T) {
	for _, subset := range [][]string{nil, {"CASE-A", "CASE-A"}, {"unknown"}} {
		s := qa35bState()
		qa35bApproved(&s, "whitebox")
		s.QACaseSets["whitebox"] = QACaseSetState{Status: caseSetFrozen}
		s.PreRepairSnapshot = "base"
		before := qa35bJSON(t, s)
		err := recordExecutionScope(&s, "whitebox", "AFFECTED", subset, "fix", "PREPARE", "base", QAExecutionResult{})
		if err == nil || qa35bJSON(t, s) != before {
			t.Fatalf("invalid subset %v err %v", subset, err)
		}
	}
}

func TestQA35BFinalFullReopensOnlySelectedMode(t *testing.T) {
	s := qa35bState()
	qa35bApproved(&s, "whitebox")
	s.QACaseSets["whitebox"] = QACaseSetState{Status: caseSetFrozen}
	s.QAExecutionByMode["whitebox"] = QAExecutionResult{Status: "PASS", Snapshot: "candidate"}
	s.QAExecutionByMode["blackbox"] = QAExecutionResult{Status: "PASS", Snapshot: "candidate"}
	s.ExecutionScopes["whitebox"] = QAExecutionScope{Decision: "AFFECTED", BaseSnapshot: "base"}
	qa35bOK(t, recordExecutionScope(&s, "whitebox", "FULL", nil, "final candidate", "PREPARE", "base", QAExecutionResult{}))
	if s.QAExecutionByMode["whitebox"].Status != "PENDING" || s.QAExecutionByMode["blackbox"].Status != "PASS" || s.PriorQAExecutionByMode["whitebox"].Status != "PASS" || len(qaExecutionRequiredCases(s, "whitebox")) != 2 {
		t.Fatal("final FULL did not reopen complete mode")
	}
}

func qa35bWaveState() RunState {
	s := qa35bState()
	s.SelectedGates = []string{"whitebox", "quality"}
	qa35bApproved(&s, "whitebox")
	w := ReviewWave{ID: "wave", Number: 1, Snapshot: s.CurrentSnapshot, BaseSnapshot: s.BaseSnapshot, RequirementRevision: s.RequirementRevision, Expected: expectedReviewWaveTargets(s), Status: reviewWaveCollecting}
	s.ReviewWaves[w.ID], s.ActiveReviewWaveID = w, w.ID
	return s
}

func TestQA35BWaveCollectionWaitsForEveryTarget(t *testing.T) {
	for _, pending := range []string{reviewTargetUnprepared, reviewTargetPrepared, reviewTargetInFlight, reviewTargetRuntimeError, reviewTargetInvalidResult, reviewTargetRetryExhaust} {
		t.Run(pending, func(t *testing.T) {
			s := qa35bWaveState()
			w := s.ReviewWaves["wave"]
			for key, v := range w.Expected {
				if v.Kind == "gate" {
					v.Status = pending
				} else {
					v.Status = "PASS"
				}
				w.Expected[key] = v
			}
			s.ReviewWaves[w.ID] = w
			maybeCollectReviewWave(&s)
			if s.ReviewWaves[w.ID].Status != "COLLECTING" || s.CompletedReviewWaves != 0 {
				t.Fatal("wave closed early")
			}
			qa35bErr(t, ensureUnifiedRepairPlan(&s), "not collected and adjudicated")
		})
	}
}

func TestQA35BWaveTargetsAndBindingsAreFrozen(t *testing.T) {
	s := qa35bWaveState()
	expected := expectedReviewWaveTargets(s)
	if len(expected) != 2 || expected["action:qa-execution:whitebox"].Status != "UNPREPARED" || expected["gate:quality"].Status != "UNPREPARED" {
		t.Fatalf("%+v", expected)
	}
	versions := map[string]string{"whitebox": "case-v1"}
	base := reviewWaveExpectedDigest(s, versions, expected)
	s.SelectedGates = []string{"quality", "whitebox"}
	if reviewWaveExpectedDigest(s, versions, expectedReviewWaveTargets(s)) != base {
		t.Fatal("route order changes digest")
	}
	s.SelectedGates = append(s.SelectedGates, "complexity")
	if reviewWaveExpectedDigest(s, versions, expectedReviewWaveTargets(s)) == base {
		t.Fatal("target change ignored")
	}
	s.CurrentSnapshot = "new"
	if reviewWaveExpectedDigest(s, versions, expected) == base {
		t.Fatal("snapshot ignored")
	}
	qa35bErr(t, requireReviewWaveDispatch(s, "gate", "quality", ""), "not COLLECTING for the current snapshot")
}

func TestQA35BWaveCollectionCountsOnce(t *testing.T) {
	s := qa35bWaveState()
	w := s.ReviewWaves["wave"]
	for key, v := range w.Expected {
		v.Status = "PASS"
		w.Expected[key] = v
	}
	s.ReviewWaves[w.ID] = w
	maybeCollectReviewWave(&s)
	maybeCollectReviewWave(&s)
	if s.CompletedReviewWaves != 1 || s.ReviewWaves[w.ID].Status != "COMPLETE" || !s.ReviewWaves[w.ID].Counted {
		t.Fatalf("%+v", s.ReviewWaves[w.ID])
	}
}

func TestQA35BErrorDispositionAndLateResult(t *testing.T) {
	s := qa35bWaveState()
	w := s.ReviewWaves["wave"]
	for key, v := range w.Expected {
		if v.Kind == "gate" {
			v.Status = "RUNTIME_ERROR"
			v.ResultStatus = "RUNTIME_ERROR"
			v.DispatchID = "late"
		} else {
			v.Status = "PASS"
		}
		w.Expected[key] = v
	}
	s.ReviewWaves[w.ID] = w
	root, pkg, s := qa35bStore(t, s)
	got, err := DisposeReviewWaveTarget(root, pkg, s.RunID, "gate:quality", s.StateIntegrity, "user accepts unavailable check for this collection")
	qa35bOK(t, err)
	target := got.ReviewWaves["wave"].Expected["gate:quality"]
	if target.Status != "DISPOSED" || target.ResultStatus != "RUNTIME_ERROR" || target.Disposition != "USER_DISPOSED_ERROR" || target.DispositionWhy == "" {
		t.Fatalf("%+v", target)
	}
	recordReviewWaveAttempt(&got, PreparedDispatch{ID: "late", TargetKind: "gate", Target: "quality", SourceSnapshot: "candidate"}, "PASS", "late completion")
	if got.ReviewWaves["wave"].Status != "COMPLETE" || len(got.ReviewWaves["wave"].LateResults) != 1 || got.ReviewWaves["wave"].Expected["gate:quality"].Status != "DISPOSED" {
		t.Fatal("late result reopened/replaced disposition")
	}
}

func TestQA35BPendingWaveTargetCannotBeDisposed(t *testing.T) {
	for _, status := range []string{"UNPREPARED", "PREPARED", "IN_FLIGHT"} {
		s := qa35bWaveState()
		w := s.ReviewWaves["wave"]
		target := w.Expected["gate:quality"]
		target.Status = status
		w.Expected[target.Key] = target
		s.ReviewWaves[w.ID] = w
		root, pkg, s := qa35bStore(t, s)
		before := qa35bRead(t, RunStatePath(root, s.RunID))
		_, err := DisposeReviewWaveTarget(root, pkg, s.RunID, target.Key, s.StateIntegrity, "user disposition")
		qa35bErr(t, err, "cannot be disposed")
		if qa35bRead(t, RunStatePath(root, s.RunID)) != before {
			t.Fatal("pending disposition wrote state")
		}
	}
}

func qa35bFindingWave() RunState {
	s := qa35bWaveState()
	w := s.ReviewWaves["wave"]
	for key, v := range w.Expected {
		v.Status = "PASS"
		w.Expected[key] = v
	}
	s.ReviewWaves[w.ID] = w
	s.Gates["quality"] = GateResult{Status: "PASS", Snapshot: "candidate", Findings: []Finding{{ID: "raw-a", Severity: "P2", Message: "normal path A requires correction"}, {ID: "raw-b", Severity: "P2", Message: "normal path B has same cause"}, {ID: "raw-c", Severity: "P3", Message: "advisory"}}}
	maybeCollectReviewWave(&s)
	return s
}

func qa35bAdjudication(id, decision, problem string) FindingAdjudicationInput {
	return FindingAdjudicationInput{FindingID: id, Decision: decision, ProblemID: problem, Dimensions: ProblemDimensions{Requirement: "REQ-004", NormalFailurePath: "documented workflow record command", AffectedModule: "validator", RootCause: "normal input mapped incorrectly", ActualConsequence: "documented record rejected"}, Verification: FindingVerification{Scope: "normal path", Severity: "limited impact with workaround", Causality: "validator branch", Reproduction: "documented command with fixture"}, References: []string{"entry.go:2"}, Reason: "independently reproduced and compared the five dimensions"}
}

func qa35bFindingIDs(s RunState) []string {
	var ids []string
	for id := range s.PostReviewFindings {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func TestQA35BAdjudicationCreatesMergesAndDismisses(t *testing.T) {
	s := qa35bFindingWave()
	root, pkg, s := qa35bStore(t, s)
	ids := qa35bFindingIDs(s)
	got, err := AdjudicateReviewWave(root, pkg, s.RunID, ReviewWaveAdjudicationInput{WaveID: "wave", StateRevision: s.StateIntegrity, Findings: []FindingAdjudicationInput{qa35bAdjudication(ids[0], "NEW", "")}})
	qa35bOK(t, err)
	if got.ReviewWaves["wave"].Status != "AWAITING_ADJUDICATION" {
		t.Fatal("unadjudicated finding skipped")
	}
	first := got.PostReviewFindings[ids[0]].Adjudications[0]
	p := first.ProblemID
	got, err = AdjudicateReviewWave(root, pkg, s.RunID, ReviewWaveAdjudicationInput{WaveID: "wave", StateRevision: got.StateIntegrity, Findings: []FindingAdjudicationInput{qa35bAdjudication(ids[1], "SAME_AS", p), qa35bAdjudication(ids[2], "DISMISSED", "")}})
	qa35bOK(t, err)
	if len(got.Problems) != 1 || len(got.Problems[p].FindingIDs) != 2 || got.Problems[p].Status != "OPEN" || got.Problems[p].Dimensions.RootCause == "" || len(got.Problems[p].Evidence) != 1 {
		t.Fatalf("%+v", got.Problems)
	}
	for _, id := range ids {
		a := got.PostReviewFindings[id].Adjudications[0]
		if a.Revision != 1 || a.Reason == "" || a.StateRevision == "" || a.Dimensions.ActualConsequence == "" || a.Verification.Reproduction == "" || len(a.References) == 0 {
			t.Fatalf("lost adjudication %+v", a)
		}
	}
}

func TestQA35BAdjudicationRequiresCASAndFiveDimensions(t *testing.T) {
	s := qa35bFindingWave()
	root, pkg, s := qa35bStore(t, s)
	id := qa35bFindingIDs(s)[0]
	before := qa35bRead(t, RunStatePath(root, s.RunID))
	for _, field := range []string{"state-revision", "requirement", "path", "module", "cause", "consequence", "verification", "reason", "references"} {
		item := qa35bAdjudication(id, "NEW", "")
		revision := s.StateIntegrity
		switch field {
		case "state-revision":
			revision = "old"
		case "requirement":
			item.Dimensions.Requirement = ""
		case "path":
			item.Dimensions.NormalFailurePath = ""
		case "module":
			item.Dimensions.AffectedModule = ""
		case "cause":
			item.Dimensions.RootCause = ""
		case "consequence":
			item.Dimensions.ActualConsequence = ""
		case "verification":
			item.Verification.Scope = ""
		case "reason":
			item.Reason = ""
		case "references":
			item.References = nil
		}
		_, err := AdjudicateReviewWave(root, pkg, s.RunID, ReviewWaveAdjudicationInput{WaveID: "wave", StateRevision: revision, Findings: []FindingAdjudicationInput{item}})
		if err == nil {
			t.Fatalf("missing %s accepted", field)
		}
		if qa35bRead(t, RunStatePath(root, s.RunID)) != before {
			t.Fatalf("missing %s changed state", field)
		}
	}
}

func TestQA35BRepairUnionExcludesAdvisoriesAndResolved(t *testing.T) {
	s := qa35bWaveState()
	w := s.ReviewWaves["wave"]
	w.Status = reviewWaveReadyForRepair
	s.ReviewWaves[w.ID] = w
	for i, severity := range []string{"P0", "P1", "P2", "P3", "QA_FAIL", "P2"} {
		id, problem := fmt.Sprintf("f%d", i), fmt.Sprintf("p%d", i)
		target := "quality"
		if severity == "QA_FAIL" {
			target = "qa-execution"
		}
		s.PostReviewFindings[id] = PostReviewFinding{ID: id, WaveID: "wave", SourceTarget: target, Severity: severity, Adjudications: []FindingAdjudication{{Decision: "NEW", ProblemID: problem}}}
		status := "OPEN"
		if i == 5 {
			status = "RESOLVED"
		}
		s.Problems[problem] = CanonicalProblem{ID: problem, Status: status}
	}
	qa35bOK(t, ensureUnifiedRepairPlan(&s))
	plan := s.ReviewWaves["wave"].RepairPlan
	if !reflect.DeepEqual(plan.ProblemIDs, []string{"p0", "p1", "p2", "p4"}) {
		t.Fatalf("%+v", plan)
	}
	before := qa35bJSON(t, plan)
	qa35bOK(t, ensureUnifiedRepairPlan(&s))
	if qa35bJSON(t, s.ReviewWaves["wave"].RepairPlan) != before {
		t.Fatal("second logical plan created")
	}
}

func TestQA35BResolvedAliasAndDismissalDoNotRepair(t *testing.T) {
	s := qa35bFindingWave()
	root, pkg, s := qa35bStore(t, s)
	ids := qa35bFindingIDs(s)
	// The resolved problem is a lawful prior-wave input to the adjudicator.
	s.Problems["prior"] = CanonicalProblem{ID: "prior", Status: "RESOLVED", ResolvedAt: "candidate", FindingIDs: []string{"previous-finding"}}
	qa35bOK(t, SaveRunState(root, s))
	s, err := LoadRunState(root, s.RunID)
	qa35bOK(t, err)
	items := []FindingAdjudicationInput{qa35bAdjudication(ids[0], "SAME_AS", "prior"), qa35bAdjudication(ids[1], "DISMISSED", ""), qa35bAdjudication(ids[2], "DISMISSED", "")}
	got, err := AdjudicateReviewWave(root, pkg, s.RunID, ReviewWaveAdjudicationInput{WaveID: "wave", StateRevision: s.StateIntegrity, Findings: items})
	qa35bOK(t, err)
	if got.ReviewWaves["wave"].Status != "COMPLETE" || len(got.Problems) != 1 || got.Problems["prior"].Status != "RESOLVED" || got.ReviewWaves["wave"].RepairPlan != nil {
		t.Fatal("resolved/dismissed finding reopened repair")
	}
}

func TestQA35BAdjudicationCorrectionKeepsHistory(t *testing.T) {
	s := qa35bFindingWave()
	root, pkg, s := qa35bStore(t, s)
	id := qa35bFindingIDs(s)[0]
	got, err := AdjudicateReviewWave(root, pkg, s.RunID, ReviewWaveAdjudicationInput{WaveID: "wave", StateRevision: s.StateIntegrity, Findings: []FindingAdjudicationInput{qa35bAdjudication(id, "NEW", "")}})
	qa35bOK(t, err)
	first := got.PostReviewFindings[id].Adjudications[0]
	got, err = AdjudicateReviewWave(root, pkg, s.RunID, ReviewWaveAdjudicationInput{WaveID: "wave", StateRevision: got.StateIntegrity, Findings: []FindingAdjudicationInput{qa35bAdjudication(id, "DISMISSED", "")}})
	qa35bOK(t, err)
	history := got.PostReviewFindings[id].Adjudications
	if len(history) != 2 || !reflect.DeepEqual(history[0], first) || history[1].Revision != 2 || history[1].Supersedes != 1 || got.Problems[first.ProblemID].ID == "" {
		t.Fatalf("history lost %+v", history)
	}
}

func TestQA35BLaterWaveNewBlockingFindingIsMarked(t *testing.T) {
	s := qa35bWaveState()
	w := s.ReviewWaves["wave"]
	w.Number = 2
	s.Gates["quality"] = GateResult{Findings: []Finding{{ID: "raw", Severity: "P1", Message: "new normal-path defect"}}}
	ids := syncReviewWaveFindings(&s, w)
	again := syncReviewWaveFindings(&s, w)
	if len(ids) != 1 || !reflect.DeepEqual(ids, again) || !s.PostReviewFindings[ids[0]].MissedEarlier || s.PostReviewFindings[ids[0]].FirstSeenWave != 2 || len(s.Problems) != 0 {
		t.Fatal("new finding marking or independent adjudication failed")
	}
}

func TestQA35BPureP2CompletionCarriesWithoutNewWave(t *testing.T) {
	s := qa35bState()
	s.CurrentSnapshot = "fixed"
	s.CompletedReviewWaves = 1
	s.ExtraReviewWaves = 2
	s.Gates["quality"] = GateResult{Status: "PASS", Snapshot: "candidate"}
	s.Gates["other"] = GateResult{Status: "PASS", Snapshot: "candidate"}
	s.Problems["p"] = CanonicalProblem{ID: "p", Status: "OPEN"}
	s.PostReviewFindings["f"] = PostReviewFinding{ID: "f", WaveID: "wave", SourceKind: "gate", SourceTarget: "quality", Severity: "P2", Adjudications: []FindingAdjudication{{Decision: "NEW", ProblemID: "p"}}}
	s.ReviewWaves["wave"] = ReviewWave{ID: "wave", Snapshot: "candidate", Status: "READY_FOR_REPAIR", RepairPlan: &UnifiedRepairPlan{ID: "repair", WaveID: "wave", Snapshot: "candidate", ProblemIDs: []string{"p"}, RepairSnapshot: "fixed"}}
	root, pkg, s := qa35bStore(t, s)
	got, err := CompleteGateP2Repair(root, pkg, s.RunID, "wave", []string{"go build ./...: exit 0"}, []string{"full project tests: exit 0"}, nil)
	qa35bOK(t, err)
	if got.CompletedReviewWaves != 1 || got.ExtraReviewWaves != 2 || got.Gates["quality"].Snapshot != "fixed" || got.Gates["quality"].Status != "PASS" || got.Gates["other"].Snapshot != "candidate" || got.Carry["quality"].Origin != "GATE_P2_COMPLETION" || got.Problems["p"].Status != "RESOLVED" || len(got.P2CompletionCredentials) != 1 {
		t.Fatalf("bad P2 completion %+v", got.P2CompletionCredentials)
	}
	c := got.P2CompletionCredentials[0]
	if c.GateID != "quality" || c.SourceSnapshot != "candidate" || c.RepairSnapshot != "fixed" || !reflect.DeepEqual(c.ProblemIDs, []string{"p"}) {
		t.Fatalf("credential misbound %+v", c)
	}
	_, err = CompleteGateP2Repair(root, pkg, s.RunID, "wave", []string{"build"}, []string{"tests"}, nil)
	qa35bErr(t, err, "already completed")
}

func TestQA35BGateP3IsSortedSealAdvisoryOnly(t *testing.T) {
	s := qa35bState()
	for _, id := range []string{"z", "a", "dismissed", "resolved"} {
		f := PostReviewFinding{ID: id, SourceKind: "gate", Severity: "P3"}
		if id == "dismissed" {
			f.Adjudications = []FindingAdjudication{{Decision: "DISMISSED"}}
		}
		if id == "resolved" {
			f.Adjudications = []FindingAdjudication{{Decision: "SAME_AS", ProblemID: "p"}}
		}
		s.PostReviewFindings[id] = f
	}
	s.Problems["p"] = CanonicalProblem{ID: "p", Status: "RESOLVED"}
	before := qa35bJSON(t, s)
	got := unresolvedGateP3(s)
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "z" || postReviewFindingRequiresRepair(got[0]) || qa35bJSON(t, s) != before {
		t.Fatalf("advisory projection %+v", got)
	}
}

func TestQA35BRepairAuthorizationPreviewIsBoundAndReadOnly(t *testing.T) {
	s := qa35bState()
	qa35bApproved(&s, "whitebox")
	s.CompletedReviewWaves = 3
	s.SelectedGates = []string{"quality", "whitebox"}
	s.DispatchObligations["a"] = DispatchObligation{AttemptsUsed: 2}
	s.DispatchObligations["b"] = DispatchObligation{AttemptsUsed: 1}
	s.Gates["quality"] = GateResult{Findings: []Finding{{ID: "f", Severity: "P1", Message: "blocking"}, {ID: "p2", Severity: "P2", Message: "fix"}, {ID: "p3", Severity: "P3", Message: "advice"}}}
	s.QAExecutionByMode["whitebox"] = QAExecutionResult{Status: "FAIL", Snapshot: s.CurrentSnapshot, Cases: []QAResultRecord{{CaseID: "CASE-A", Mode: "whitebox", Outcome: "FAIL"}}, Findings: []Finding{{Message: "CASE-A failed"}}}
	s.QACaseSets["whitebox"] = QACaseSetState{Suggestions: []QASuggestion{{Severity: "P3", Status: "PENDING"}}}
	estimate := RepairRerunEstimate{BuildTargets: []string{"./cmd/formal-gates"}, TestTargets: []string{"./..."}, QACases: map[string][]string{"whitebox": {"CASE-A"}}}
	root, pkg, s := qa35bStore(t, s)
	path := RunStatePath(root, s.RunID)
	before := qa35bRead(t, path)
	p, err := PreviewRepairAuthorization(root, pkg, s.RunID, estimate)
	qa35bOK(t, err)
	if qa35bRead(t, path) != before || p.ReviewWave != 3 || p.RemainingAutomaticWaves != 0 || p.DispatchAttempts != 3 || p.Runtime != "UNAVAILABLE" || p.EngineAllowance != "UNAVAILABLE" || !reflect.DeepEqual(p.QAFailures, []string{"CASE-A failed"}) || !reflect.DeepEqual(p.BlockingFindings, []string{"f: blocking"}) || p.GateSuggestions["P2:UNADJUDICATED"] != 1 || p.GateSuggestions["P3:UNADJUDICATED"] != 1 || p.QAReviewSuggestions["P3:PENDING"] != 1 || !reflect.DeepEqual(p.Estimate, estimate) {
		t.Fatalf("incomplete or mutating preview %+v", p)
	}
	qa35bOK(t, validateRepairAuthorizationPreview(s, p, RepairAuthorizationInput{StateRevision: s.StateIntegrity, PreviewDigest: p.Digest}))
	qa35bErr(t, validateRepairAuthorizationPreview(s, p, RepairAuthorizationInput{StateRevision: "old", PreviewDigest: p.Digest}), "stale")
	estimate.TestTargets = []string{"different-target"}
	changed := repairAuthorizationPreviewFromState(s, estimate)
	qa35bErr(t, validateRepairAuthorizationPreview(s, changed, RepairAuthorizationInput{StateRevision: s.StateIntegrity, PreviewDigest: p.Digest}), "preview digest")
}

func TestQA35BOwnerTerminalProjectionDoesNotTouchResults(t *testing.T) {
	s := qa35bState()
	s.Actions["product-review"] = ActionResult{Status: "PASS"}
	s.Gates["quality"] = GateResult{Status: "PASS"}
	s.QAExecutionByMode["whitebox"] = QAExecutionResult{Status: "FAIL"}
	s.Cost = &cost.RunCost{}
	cost.Record(s.Cost, "dispatch", cost.DispatchCost{TotalInputTokens: 10, OutputTokens: 4, Source: cost.SourceTranscript})
	results := qa35bJSON(t, []any{s.Actions, s.Gates, s.QAExecutionByMode, s.Dispatches})
	finishOwnerCost(&s)
	before := qa35bJSON(t, s)
	finishOwnerCost(&s)
	if qa35bJSON(t, s) != before || qa35bJSON(t, []any{s.Actions, s.Gates, s.QAExecutionByMode, s.Dispatches}) != results || !s.OwnerCostCompleted || s.Cost.Owner.Source != cost.SourceUnavailable || s.Cost.Owner.TotalInputTokens != 0 || s.Cost.TotalInputTokens != 10 {
		t.Fatal("owner terminal affected results or doubled cost")
	}
}

func TestQA35BOwnerOverlapProducesUnavailable(t *testing.T) {
	s := qa35bState()
	s.OwnerCostOverlapped = true
	finishOwnerCost(&s)
	if !s.OwnerCostCompleted || s.Cost.Owner.Source != cost.SourceUnavailable || s.Cost.TotalInputTokens != 0 || s.Cost.OutputTokens != 0 {
		t.Fatalf("overlap estimated: %+v", s.Cost)
	}
}

// qa35bGit creates a private native snapshot for snapshot/requirement API tests.
func qa35bGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	command.Env = append(os.Environ(), "GIT_AUTHOR_NAME=QA Design", "GIT_AUTHOR_EMAIL=qa@example.invalid", "GIT_COMMITTER_NAME=QA Design", "GIT_COMMITTER_EMAIL=qa@example.invalid")
	out, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}
