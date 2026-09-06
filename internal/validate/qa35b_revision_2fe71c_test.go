package validate

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
)

func qa35bSemanticReview(t *testing.T, s *RunState, action string, findings []Finding) SemanticReviewCandidate {
	t.Helper()
	d := PreparedDispatch{ID: fmt.Sprintf("%s-%d", action, len(s.PreDevelopmentReviewSeries[action].RawCandidates)+1), RequirementRevision: s.RequirementRevision}
	checks := []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"}
	result, err := recordSemanticReviewCandidate(s, action, d, ActionResult{Status: "PASS", Findings: findings}, OperatorVerification{Checks: checks, Evidence: "QA fixture: independently verified documented normal paths and each finding's severity"})
	qa35bOK(t, err)
	s.Actions[action] = result
	series := s.PreDevelopmentReviewSeries[action]
	return series.RawCandidates[len(series.RawCandidates)-1]
}

func TestQA35BUnsettledProjectionKeepsSeverityAndRationale(t *testing.T) {
	s := qa35bState()
	for _, action := range []string{"product-review", "start-readiness"} {
		candidate := qa35bSemanticReview(t, &s, action, []Finding{{Severity: "P2", SeverityRationale: "limited documentation ambiguity; normal entry remains usable", Category: FindingCategoryImplementation, Message: "clarify the documented example"}})
		got := s.Actions[action].Findings[0]
		if got.ID != candidate.Findings[0].ID || got.Severity != "P2" || got.SeverityRationale != candidate.Findings[0].SeverityRationale || got.Message != candidate.Findings[0].Message {
			t.Fatalf("projection lost fields %+v", got)
		}
	}
}

func TestQA35BReseverityProjectsCurrentReasonAndPreservesRaw(t *testing.T) {
	s := qa35bState()
	candidate := qa35bSemanticReview(t, &s, "product-review", []Finding{{Severity: "P2", SeverityRationale: "limited ambiguity", Message: "example wording"}})
	raw := qa35bJSON(t, candidate)
	series := s.PreDevelopmentReviewSeries["product-review"]
	input := ReviewCorrectionInput{Action: "product-review", DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: s.RequirementRevision, FindingID: candidate.Findings[0].ID, Decision: "RESEVERITIZE", Severity: "P3", SeverityRationale: "wording only; all normal acceptance paths work", Reason: "verified actual impact", Source: "user decision", UserAuthorized: true, ExpectedEffectiveRevision: series.EffectiveReviewStatus.Revision}
	qa35bOK(t, applyReviewCorrection(&s, input))
	got := s.Actions["product-review"].Findings[0]
	if got.Severity != "P3" || got.SeverityRationale != input.SeverityRationale || got.ID != candidate.Findings[0].ID || qa35bJSON(t, s.PreDevelopmentReviewSeries["product-review"].RawCandidates[0]) != raw {
		t.Fatal("effective projection overwrote raw evidence")
	}
	qa35bErr(t, applyReviewCorrection(&s, input), "conflict")
}

func TestQA35BProductAdvisoryDoesNotEscalateItemFailure(t *testing.T) {
	s := qa35bState()
	s.ReviewItemsByAction = map[string]map[string]ReviewItem{"product-review": {"REQ-001 obligation-to-AC completeness": {Status: "FAIL", Message: "minor wording gap"}}}
	status, findings, err := validateStrictSemanticResult("product-review", "PASS", "", []FindingInput{{Severity: "P2", Message: "minor wording gap"}}, false, t.TempDir(), s)
	qa35bOK(t, err)
	qa35bSemanticReview(t, &s, "product-review", findings)
	if status != "PASS" || s.Actions["product-review"].Status != "PASS" || s.PreDevelopmentReviewSeries["product-review"].EffectiveReviewStatus.Status != "PASS" || s.ReviewItemsByAction["product-review"]["REQ-001 obligation-to-AC completeness"].Status != "FAIL" {
		t.Fatal("item status decided finding/action severity")
	}
}

func TestQA35BReviewOverrideDoesNotRestoreAutomaticCapacity(t *testing.T) {
	s := qa35bState()
	for i := 0; i < 3; i++ {
		qa35bSemanticReview(t, &s, "product-review", nil)
	}
	before := qa35bJSON(t, s)
	qa35bErr(t, requirePreDevelopmentReviewPreparation(s, "product-review", false, ""), "exhausted")
	qa35bErr(t, requirePreDevelopmentReviewPreparation(s, "product-review", true, ""), "non-empty")
	qa35bOK(t, requirePreDevelopmentReviewPreparation(s, "product-review", true, "user requests one more review"))
	if qa35bJSON(t, s) != before {
		t.Fatal("preparation check manufactured capacity")
	}
	qa35bOK(t, authorizePreDevelopmentReviewDispatch(&s, "product-review", "extra", true, "user requests one more review"))
	series := s.PreDevelopmentReviewSeries["product-review"]
	if series.Completed != 3 || series.RemainingAutomatic != 0 || len(series.Authorizations) != 1 || len(series.RawCandidates) != 3 || series.Authorizations[0].Reason != "user requests one more review" {
		t.Fatalf("lost budget/audit %+v", series)
	}
}

func qa35bDelta(t *testing.T, s *RunState, candidateRevision string) ReviewDeltaPackage {
	t.Helper()
	first := qa35bSemanticReview(t, s, "product-review", []Finding{{Severity: "P2", Message: "clarify first example"}})
	second := qa35bSemanticReview(t, s, "start-readiness", []Finding{{Severity: "P2", Category: FindingCategoryImplementation, Message: "clarify second example"}})
	qa35bOK(t, settleSemanticReviewFindings(s, "product-review", []string{"clarify first example"}, nil))
	qa35bOK(t, settleSemanticReviewFindings(s, "start-readiness", []string{"clarify second example"}, nil))
	f1, f2 := first.Findings[0].ID, second.Findings[0].ID
	d := ReviewDeltaPackage{BaseRevision: s.RequirementRevision, CandidateRevision: candidateRevision, ConfirmationScope: "REVIEW_DELTA", UserConfirmation: "user confirms the displayed complete change package", AffectedActions: []string{"start-readiness"},
		Sources: []ReviewDeltaSource{{Action: "product-review", FindingID: f1, Decision: "CONFIRM"}, {Action: "start-readiness", FindingID: f2, Decision: "CONFIRM"}},
		Hunks:   []ReviewDeltaHunk{{ID: "h1", Affected: []string{"REQ-001", "AC-001"}, SourceFindingIDs: []string{f1}, Disposition: "CONFIRM", Impact: "clarify first example", Verification: "captured exact base before editing and compared complete added/deleted text"}, {ID: "h2", Affected: []string{"方案"}, SourceFindingIDs: []string{f2}, Disposition: "CONFIRM", Impact: "move second example", Verification: "verified deletion and insertion against captured base; no unrelated hunk"}}}
	d.Digest = reviewDeltaDigest(d)
	return d
}

func TestQA35BReviewDeltaReceiptBindsEveryHunkAndSource(t *testing.T) {
	s := qa35bState()
	d := qa35bDelta(t, &s, "req-r2")
	before := qa35bJSON(t, s)
	r, err := validateReviewDelta(s, "req-r2", &d)
	qa35bOK(t, err)
	if r.ID == "" || r.BaseRevision != s.RequirementRevision || r.CandidateRevision != "req-r2" || r.DeltaDigest != d.Digest || len(r.Sources) != 2 || len(r.HunkVerifications) != 2 || r.ConfirmationSource != d.UserConfirmation || r.HunkVerifications[0].VerificationDigest == "" {
		t.Fatalf("incomplete receipt %+v", r)
	}
	if qa35bJSON(t, s) != before || strings.Contains(qa35bJSON(t, r), d.Hunks[0].Verification) {
		t.Fatal("validator changed state or persisted full verification text")
	}
	d.UserConfirmation = "later user answer"
	if reviewDeltaDigest(d) != r.DeltaDigest {
		t.Fatal("answer changed displayed package digest")
	}
}

func TestQA35BReviewDeltaRejectsStaleAndUnrelatedChanges(t *testing.T) {
	for _, name := range []string{"base", "candidate", "digest", "missing-verification", "unrelated-hunk", "source-without-hunk", "not-confirmed", "missing-confirmation"} {
		t.Run(name, func(t *testing.T) {
			s := qa35bState()
			d := qa35bDelta(t, &s, "req-r2")
			switch name {
			case "base":
				d.BaseRevision = "old"
			case "candidate":
				d.CandidateRevision = "old"
			case "digest":
				d.Hunks[0].Impact = "changed after preview"
			case "missing-verification":
				d.Hunks[0].Verification = ""
			case "unrelated-hunk":
				d.Hunks[0].SourceFindingIDs = []string{"unrelated"}
			case "source-without-hunk":
				d.Hunks = d.Hunks[:1]
			case "not-confirmed":
				d.Sources[0].Decision = "DISMISS"
			case "missing-confirmation":
				d.UserConfirmation = ""
			}
			if name != "digest" {
				d.Digest = reviewDeltaDigest(d)
			}
			before := qa35bJSON(t, s)
			r, err := validateReviewDelta(s, "req-r2", &d)
			if err == nil || r.ID != "" || qa35bJSON(t, s) != before {
				t.Fatalf("accepted %s: %+v %v", name, r, err)
			}
		})
	}
}

func TestQA35BImpactPreviewListsInvalidationAndBudget(t *testing.T) {
	s := qa35bState()
	qa35bSemanticReview(t, &s, "product-review", nil)
	qa35bSemanticReview(t, &s, "start-readiness", nil)
	s.Actions["development-worker"] = ActionResult{Status: "VERIFIED"}
	s.Gates["quality"] = GateResult{Status: "PASS"}
	s.QAReviewByMode["whitebox"] = ActionResult{Status: "PASS"}
	before := qa35bJSON(t, s)
	p, err := requirementImpactPreview(s, "req-r2", "delta", RequirementImpactInput{AcceptanceCriteria: "return an additional documented output"})
	qa35bOK(t, err)
	want := []string{"development-worker:VERIFIED", "gate:quality:PASS", "product-review:PASS", "qa-review:whitebox:PASS", "start-readiness:PASS"}
	if !reflect.DeepEqual(p.InvalidatedPasses, want) || p.BaseRevision != s.RequirementRevision || p.CandidateRevision != "req-r2" || p.StateRevision != s.StateIntegrity || p.ProductReviewCompleted != 1 || p.ProductReviewRemaining != 2 || p.ReviewDeltaDigest != "delta" || len(p.RollbackSteps) != 3 || p.Digest == "" || qa35bJSON(t, s) != before {
		t.Fatalf("bad impact preview %+v", p)
	}
	_, err = requirementImpactPreview(s, "req-r2", "delta", RequirementImpactInput{})
	qa35bErr(t, err, "at least one concrete")
}

func qa35bRequirementFixture(t *testing.T) (string, string, RunState, ReviewDeltaPackage) {
	t.Helper()
	root, pkg := t.TempDir(), qa35bPackage(t)
	text := qa35bRead(t, filepath.Join(pkg, "refactor-plan/review-convergence-and-stage-3-5b-requirement.md"))
	qa35bWrite(t, filepath.Join(root, "requirement.md"), text)
	qa35bGit(t, root, "init", "-q")
	qa35bGit(t, root, "add", "requirement.md")
	qa35bGit(t, root, "commit", "-q", "-m", "QA requirement fixture")
	s := qa35bState()
	s.RequirementSource = "requirement.md"
	var err error
	s.RequirementArtifacts, err = requirementArtifactSet(root, "requirement.md", nil)
	qa35bOK(t, err)
	s.RequirementRevision = s.RequirementArtifacts[0].Revision
	s.BaseSnapshot = qa35bGit(t, root, "rev-parse", "HEAD")
	s.CurrentSnapshot = s.BaseSnapshot
	updated := strings.Replace(text, "实时成本面板暂不实施", "实时成本面板本次暂不实施", 1)
	if updated == text {
		t.Fatal("fixture edit anchor missing")
	}
	qa35bWrite(t, filepath.Join(root, "requirement.md"), updated)
	revision, err := RequirementRevision(filepath.Join(root, "requirement.md"))
	qa35bOK(t, err)
	d := qa35bDelta(t, &s, revision)
	catalog, err := LoadPromptCatalog(pkg)
	qa35bOK(t, err)
	s.CatalogRevision, s.BasePromptRevision, s.PromptHashes = catalog.CatalogRevision, catalog.BaseRevision, catalogPromptHashes(catalog)
	qa35bOK(t, SaveRunState(root, s))
	s, err = LoadRunState(root, s.RunID)
	qa35bOK(t, err)
	return root, pkg, s, d
}

func TestQA35BMeaningPreservedKeepsProductPass(t *testing.T) {
	root, pkg, s, d := qa35bRequirementFixture(t)
	oldSeries := s.PreDevelopmentReviewSeries["product-review"]
	got, err := UpdateRequirement(root, pkg, s.RunID, "", true, "preserved", nil, RequirementUpdateOptions{ReviewDelta: &d})
	qa35bOK(t, err)
	if got.RequirementRevision != d.CandidateRevision || got.Actions["product-review"].Status != "PASS" || got.PreDevelopmentReviewSeries["product-review"].EffectiveReviewStatus.Status != "PASS" || got.PreDevelopmentReviewSeries["product-review"].Completed != oldSeries.Completed || got.Actions["start-readiness"].Status != "PENDING" || len(got.ReviewDeltaReceipts) != 1 || got.ReviewDeltaReceipts[0].ImpactPreviewDigest != "" {
		t.Fatal("preserved required rollback approval or lost product PASS")
	}
}

func TestQA35BMeaningChangedRejectsMissingAndStaleAuthorization(t *testing.T) {
	for _, name := range []string{"missing", "state", "digest", "confirmation"} {
		t.Run(name, func(t *testing.T) {
			root, pkg, s, d := qa35bRequirementFixture(t)
			before := qa35bRead(t, RunStatePath(root, s.RunID))
			impact := RequirementImpactInput{Scope: "user proposed a new documented operation"}
			p, err := PreviewRequirementImpact(root, pkg, s.RunID, "", nil, d.Digest, impact)
			qa35bOK(t, err)
			if qa35bRead(t, RunStatePath(root, s.RunID)) != before {
				t.Fatal("impact preview wrote state")
			}
			a := &RequirementImpactAuthorization{PreviewDigest: p.Digest, StateRevision: s.StateIntegrity, UserConfirmation: "user confirms displayed rollback", Impact: impact}
			switch name {
			case "missing":
				a = nil
			case "state":
				a.StateRevision = "old"
			case "digest":
				a.PreviewDigest = "old"
			case "confirmation":
				a.UserConfirmation = ""
			}
			_, err = UpdateRequirement(root, pkg, s.RunID, "", true, "changed", nil, RequirementUpdateOptions{ReviewDelta: &d, ImpactAuthorization: a})
			if err == nil || !strings.Contains(err.Error(), "meaning-changed") || qa35bRead(t, RunStatePath(root, s.RunID)) != before {
				t.Fatalf("invalid authorization wrote state or wrong failure: %v", err)
			}
		})
	}
}

func TestQA35BCombinedDeltaAndImpactConfirmation(t *testing.T) {
	root, pkg, s, d := qa35bRequirementFixture(t)
	d.ConfirmationScope = "REVIEW_DELTA_AND_REQUIREMENT_IMPACT"
	d.Digest = reviewDeltaDigest(d)
	impact := RequirementImpactInput{AcceptanceCriteria: "new documented acceptance behavior proposed by Operator"}
	p, err := PreviewRequirementImpact(root, pkg, s.RunID, "", nil, d.Digest, impact)
	qa35bOK(t, err)
	d.ImpactPreviewDigest = p.Digest
	old := s.PreDevelopmentReviewSeries["product-review"]
	got, err := UpdateRequirement(root, pkg, s.RunID, "", true, "changed", nil, RequirementUpdateOptions{ReviewDelta: &d, ImpactAuthorization: &RequirementImpactAuthorization{PreviewDigest: p.Digest, StateRevision: s.StateIntegrity, Impact: impact}})
	qa35bOK(t, err)
	if got.RequirementRevision != d.CandidateRevision || !got.RequirementConfirmed || got.Actions["product-review"].Status != "PENDING" || got.PreDevelopmentReviewSeries["product-review"].Completed != old.Completed || got.PreDevelopmentReviewSeries["product-review"].CurrentLimit != old.CurrentLimit || len(got.ReviewDeltaReceipts) != 1 || len(got.Dispatches) != 0 {
		t.Fatal("combined authorization failed atomic revision/rollback/budget contract")
	}
	r := got.ReviewDeltaReceipts[0]
	if r.ConfirmationSource != d.UserConfirmation || r.ImpactConfirmationSource != d.UserConfirmation || r.ImpactPreviewDigest != p.Digest || r.ImpactStateRevision != s.StateIntegrity {
		t.Fatalf("combined receipt %+v", r)
	}
}

func TestQA35BReviewDeltaPreviewChecksWholeCandidate(t *testing.T) {
	root, pkg, s, d := qa35bRequirementFixture(t)
	before := qa35bRead(t, RunStatePath(root, s.RunID))
	p, err := PreviewReviewDelta(root, pkg, s.RunID, "", nil, d)
	qa35bOK(t, err)
	if p.CandidateRevision != d.CandidateRevision || p.UserConfirmation != "" || p.Digest != d.Digest || qa35bRead(t, RunStatePath(root, s.RunID)) != before {
		t.Fatal("delta preview binding/read-only error")
	}
	qa35bWrite(t, filepath.Join(root, "requirement.md"), "# Incomplete candidate\n### REQ-001：missing acceptance\n")
	_, err = PreviewReviewDelta(root, pkg, s.RunID, "", nil, d)
	qa35bErr(t, err, "precheck")
	if qa35bRead(t, RunStatePath(root, s.RunID)) != before {
		t.Fatal("invalid candidate changed state")
	}
}

func TestQA35BFreezeRechecksGuaranteeCoverage(t *testing.T) {
	root, _, s, _ := qa35bRequirementFixture(t)
	// Construct a guarantee for the complete candidate with the production
	// activator; the source/point/case projection is never fabricated here.
	s.RequirementRevision, _ = RequirementRevision(filepath.Join(root, "requirement.md"))
	s.RequirementArtifacts, _ = requirementArtifactSet(root, "requirement.md", nil)
	qa35bOK(t, activateFrozenGuarantee(root, &s))
	qa35bApproved(&s, "whitebox")
	updateGuaranteeForRoute(&s)
	s.QACasesByMode["whitebox"][0].AcceptanceCriteria = []string{"AC-001"}
	s.QACasesByMode["whitebox"][1].AcceptanceCriteria = []string{"AC-002"}
	// A previous PASS marker cannot replace the source/point/case projection.
	s.RequirementGuarantee.ReviewsByMode["whitebox"] = GuaranteeReviewRecord{Review: coverage.ReviewResult{SetStatus: coverage.StatusPass}}
	c := QACaseSetState{}
	err := freezeQACaseSet(&s, "whitebox", &c, false)
	if err == nil || c.Status == "FROZEN" || c.Version != "" {
		t.Fatal("incomplete guarantee froze")
	}
}
