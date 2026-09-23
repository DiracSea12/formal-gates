package validate

import (
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
)

func qaSemanticReview(t *testing.T, s *RunState, action string, findings []Finding) SemanticReviewCandidate {
	t.Helper()
	d := PreparedDispatch{ID: fmt.Sprintf("%s-%d", action, len(s.PreDevelopmentReviewSeries[action].RawCandidates)+1), RequirementRevision: s.RequirementRevision}
	checks := []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"}
	result, err := recordSemanticReviewCandidate(s, action, d, ActionResult{Status: "PASS", Findings: findings}, OperatorVerification{Checks: checks, Evidence: "QA fixture: independently verified documented normal paths and each finding's severity"})
	qaOK(t, err)
	s.Actions[action] = result
	series := s.PreDevelopmentReviewSeries[action]
	return series.RawCandidates[len(series.RawCandidates)-1]
}

func TestUnsettledProjectionKeepsSeverityAndRationale(t *testing.T) {
	s := qaState()
	for _, action := range []string{"product-review", "start-readiness"} {
		candidate := qaSemanticReview(t, &s, action, []Finding{{Severity: "P2", SeverityRationale: "limited documentation ambiguity; normal entry remains usable", Category: FindingCategoryImplementation, Message: "clarify the documented example"}})
		got := s.Actions[action].Findings[0]
		if got.ID != candidate.Findings[0].ID || got.Severity != "P2" || got.SeverityRationale != candidate.Findings[0].SeverityRationale || got.Message != candidate.Findings[0].Message {
			t.Fatalf("projection lost fields %+v", got)
		}
	}
}

func TestReseverityProjectsCurrentReasonAndPreservesRaw(t *testing.T) {
	s := qaState()
	candidate := qaSemanticReview(t, &s, "product-review", []Finding{{Severity: "P2", SeverityRationale: "limited ambiguity", Message: "example wording"}})
	raw := qaJSON(t, candidate)
	series := s.PreDevelopmentReviewSeries["product-review"]
	input := ReviewCorrectionInput{Action: "product-review", DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest, RequirementRevision: s.RequirementRevision, FindingID: candidate.Findings[0].ID, Decision: "RESEVERITIZE", Severity: "P3", SeverityRationale: "wording only; all normal acceptance paths work", Reason: "verified actual impact", Source: "user decision", UserAuthorized: true, ExpectedEffectiveRevision: series.EffectiveReviewStatus.Revision}
	qaOK(t, applyReviewCorrection(&s, input))
	got := s.Actions["product-review"].Findings[0]
	if got.Severity != "P3" || got.SeverityRationale != input.SeverityRationale || got.ID != candidate.Findings[0].ID || qaJSON(t, s.PreDevelopmentReviewSeries["product-review"].RawCandidates[0]) != raw {
		t.Fatal("effective projection overwrote raw evidence")
	}
	qaErr(t, applyReviewCorrection(&s, input), "conflict")
}

func TestProductAdvisoryDoesNotEscalateItemFailure(t *testing.T) {
	s := qaState()
	s.ReviewItemsByAction = map[string]map[string]ReviewItem{"product-review": {"REQ-001 obligation-to-AC completeness": {Status: "FAIL", Message: "minor wording gap"}}}
	status, findings, err := validateStrictSemanticResult("product-review", "PASS", "", []FindingInput{{Severity: "P2", Message: "minor wording gap"}}, false, t.TempDir(), s)
	qaOK(t, err)
	qaSemanticReview(t, &s, "product-review", findings)
	if status != "PASS" || s.Actions["product-review"].Status != "PASS" || s.PreDevelopmentReviewSeries["product-review"].EffectiveReviewStatus.Status != "PASS" || s.ReviewItemsByAction["product-review"]["REQ-001 obligation-to-AC completeness"].Status != "FAIL" {
		t.Fatal("item status decided finding/action severity")
	}
}

func TestReviewOverrideDoesNotRestoreAutomaticCapacity(t *testing.T) {
	s := qaState()
	for i := 0; i < 3; i++ {
		qaSemanticReview(t, &s, "product-review", nil)
	}
	before := qaJSON(t, s)
	qaErr(t, requirePreDevelopmentReviewPreparation(s, "product-review", false, ""), "exhausted")
	qaErr(t, requirePreDevelopmentReviewPreparation(s, "product-review", true, ""), "non-empty")
	qaOK(t, requirePreDevelopmentReviewPreparation(s, "product-review", true, "user requests one more review"))
	if qaJSON(t, s) != before {
		t.Fatal("preparation check manufactured capacity")
	}
	qaOK(t, authorizePreDevelopmentReviewDispatch(&s, "product-review", "extra", true, "user requests one more review"))
	series := s.PreDevelopmentReviewSeries["product-review"]
	if series.Completed != 3 || series.RemainingAutomatic != 0 || len(series.Authorizations) != 1 || len(series.RawCandidates) != 3 || series.Authorizations[0].Reason != "user requests one more review" {
		t.Fatalf("lost budget/audit %+v", series)
	}
}

func qaDelta(t *testing.T, s *RunState, candidateRevision string) ReviewDeltaPackage {
	t.Helper()
	first := qaSemanticReview(t, s, "product-review", []Finding{{Severity: "P2", Message: "clarify first example"}})
	second := qaSemanticReview(t, s, "start-readiness", []Finding{{Severity: "P2", Category: FindingCategoryImplementation, Message: "clarify second example"}})
	qaOK(t, settleSemanticReviewFindings(s, "product-review", []string{"clarify first example"}, nil))
	qaOK(t, settleSemanticReviewFindings(s, "start-readiness", []string{"clarify second example"}, nil))
	f1, f2 := first.Findings[0].ID, second.Findings[0].ID
	d := ReviewDeltaPackage{BaseRevision: s.RequirementRevision, CandidateRevision: candidateRevision, ConfirmationScope: "REVIEW_DELTA", UserConfirmation: "user confirms the displayed complete change package", AffectedActions: []string{"start-readiness"},
		Sources: []ReviewDeltaSource{{Action: "product-review", FindingID: f1, Decision: "CONFIRM"}, {Action: "start-readiness", FindingID: f2, Decision: "CONFIRM"}},
		Hunks:   []ReviewDeltaHunk{{ID: "h1", Affected: []string{"REQ-001", "AC-001"}, SourceFindingIDs: []string{f1}, Disposition: "CONFIRM", Impact: "clarify first example", Verification: "captured exact base before editing and compared complete added/deleted text"}, {ID: "h2", Affected: []string{"方案"}, SourceFindingIDs: []string{f2}, Disposition: "CONFIRM", Impact: "move second example", Verification: "verified deletion and insertion against captured base; no unrelated hunk"}}}
	d.Digest = reviewDeltaDigest(d)
	return d
}

func TestReviewDeltaReceiptBindsEveryHunkAndSource(t *testing.T) {
	s := qaState()
	d := qaDelta(t, &s, "req-r2")
	before := qaJSON(t, s)
	r, err := validateReviewDelta(s, "req-r2", &d)
	qaOK(t, err)
	if r.ID == "" || r.BaseRevision != s.RequirementRevision || r.CandidateRevision != "req-r2" || r.DeltaDigest != d.Digest || len(r.Sources) != 2 || len(r.HunkVerifications) != 2 || r.ConfirmationSource != d.UserConfirmation || r.HunkVerifications[0].VerificationDigest == "" {
		t.Fatalf("incomplete receipt %+v", r)
	}
	if qaJSON(t, s) != before || strings.Contains(qaJSON(t, r), d.Hunks[0].Verification) {
		t.Fatal("validator changed state or persisted full verification text")
	}
	d.UserConfirmation = "later user answer"
	if reviewDeltaDigest(d) != r.DeltaDigest {
		t.Fatal("answer changed displayed package digest")
	}
}

func TestReviewDeltaRejectsStaleAndUnrelatedChanges(t *testing.T) {
	for _, name := range []string{"base", "candidate", "digest", "missing-verification", "unrelated-hunk", "source-without-hunk", "not-confirmed", "missing-confirmation"} {
		t.Run(name, func(t *testing.T) {
			s := qaState()
			d := qaDelta(t, &s, "req-r2")
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
			before := qaJSON(t, s)
			r, err := validateReviewDelta(s, "req-r2", &d)
			if err == nil || r.ID != "" || qaJSON(t, s) != before {
				t.Fatalf("accepted %s: %+v %v", name, r, err)
			}
		})
	}
}

func TestImpactPreviewListsInvalidationAndBudget(t *testing.T) {
	s := qaState()
	qaSemanticReview(t, &s, "product-review", nil)
	qaSemanticReview(t, &s, "start-readiness", nil)
	s.Actions["development-worker"] = ActionResult{Status: "VERIFIED"}
	s.Gates["quality"] = GateResult{Status: "PASS"}
	s.QAReviewByMode["whitebox"] = ActionResult{Status: "PASS"}
	before := qaJSON(t, s)
	p, err := requirementImpactPreview(s, "req-r2", "delta", RequirementImpactInput{AcceptanceCriteria: "return an additional documented output"})
	qaOK(t, err)
	want := []string{"development-worker:VERIFIED", "gate:quality:PASS", "product-review:PASS", "qa-review:whitebox:PASS", "start-readiness:PASS"}
	if !reflect.DeepEqual(p.InvalidatedPasses, want) || p.BaseRevision != s.RequirementRevision || p.CandidateRevision != "req-r2" || p.StateRevision != s.StateIntegrity || p.ProductReviewCompleted != 1 || p.ProductReviewRemaining != 2 || p.ReviewDeltaDigest != "delta" || len(p.RollbackSteps) != 3 || p.Digest == "" || qaJSON(t, s) != before {
		t.Fatalf("bad impact preview %+v", p)
	}
	_, err = requirementImpactPreview(s, "req-r2", "delta", RequirementImpactInput{})
	qaErr(t, err, "at least one concrete")
}

func qaRequirementFixture(t *testing.T) (string, string, RunState, ReviewDeltaPackage) {
	t.Helper()
	root, pkg := t.TempDir(), qaPackage(t)
	text := qaRead(t, filepath.Join(pkg, "refactor-plan/review-convergence-and-stage-3-5b-requirement.md"))
	qaWrite(t, filepath.Join(root, "requirement.md"), text)
	qaGit(t, root, "init", "-q")
	qaGit(t, root, "add", "requirement.md")
	qaGit(t, root, "commit", "-q", "-m", "QA requirement fixture")
	s := qaState()
	s.RequirementSource = "requirement.md"
	var err error
	s.RequirementArtifacts, err = requirementArtifactSet(root, "requirement.md", nil)
	qaOK(t, err)
	s.RequirementRevision = s.RequirementArtifacts[0].Revision
	s.BaseSnapshot = qaGit(t, root, "rev-parse", "HEAD")
	s.CurrentSnapshot = s.BaseSnapshot
	updated := strings.Replace(text, "实时成本面板暂不实施", "实时成本面板本次暂不实施", 1)
	if updated == text {
		t.Fatal("fixture edit anchor missing")
	}
	qaWrite(t, filepath.Join(root, "requirement.md"), updated)
	revision, err := RequirementRevision(filepath.Join(root, "requirement.md"))
	qaOK(t, err)
	d := qaDelta(t, &s, revision)
	catalog, err := LoadPromptCatalog(pkg)
	qaOK(t, err)
	s.CatalogRevision, s.BasePromptRevision, s.PromptHashes = catalog.CatalogRevision, catalog.BaseRevision, catalogPromptHashes(catalog)
	qaOK(t, SaveRunState(root, s))
	s, err = LoadRunState(root, s.RunID)
	qaOK(t, err)
	return root, pkg, s, d
}

func TestMeaningPreservedKeepsProductPass(t *testing.T) {
	root, pkg, s, d := qaRequirementFixture(t)
	oldSeries := s.PreDevelopmentReviewSeries["product-review"]
	got, err := UpdateRequirement(root, pkg, s.RunID, "", true, "preserved", nil, RequirementUpdateOptions{ReviewDelta: &d})
	qaOK(t, err)
	if got.RequirementRevision != d.CandidateRevision || got.Actions["product-review"].Status != "PASS" || got.PreDevelopmentReviewSeries["product-review"].EffectiveReviewStatus.Status != "PASS" || got.PreDevelopmentReviewSeries["product-review"].Completed != oldSeries.Completed || got.Actions["start-readiness"].Status != "PENDING" || len(got.ReviewDeltaReceipts) != 1 || got.ReviewDeltaReceipts[0].ImpactPreviewDigest != "" {
		t.Fatal("preserved required rollback approval or lost product PASS")
	}
}

func TestMeaningChangedRejectsMissingAndStaleAuthorization(t *testing.T) {
	for _, name := range []string{"missing", "state", "digest", "confirmation"} {
		t.Run(name, func(t *testing.T) {
			root, pkg, s, d := qaRequirementFixture(t)
			before := qaRead(t, RunStatePath(root, s.RunID))
			impact := RequirementImpactInput{Scope: "user proposed a new documented operation"}
			p, err := PreviewRequirementImpact(root, pkg, s.RunID, "", nil, d.Digest, impact)
			qaOK(t, err)
			if qaRead(t, RunStatePath(root, s.RunID)) != before {
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
			if err == nil || !strings.Contains(err.Error(), "meaning-changed") || qaRead(t, RunStatePath(root, s.RunID)) != before {
				t.Fatalf("invalid authorization wrote state or wrong failure: %v", err)
			}
		})
	}
}

func TestCombinedDeltaAndImpactConfirmation(t *testing.T) {
	root, pkg, s, d := qaRequirementFixture(t)
	d.ConfirmationScope = "REVIEW_DELTA_AND_REQUIREMENT_IMPACT"
	d.Digest = reviewDeltaDigest(d)
	impact := RequirementImpactInput{AcceptanceCriteria: "new documented acceptance behavior proposed by Operator"}
	p, err := PreviewRequirementImpact(root, pkg, s.RunID, "", nil, d.Digest, impact)
	qaOK(t, err)
	d.ImpactPreviewDigest = p.Digest
	old := s.PreDevelopmentReviewSeries["product-review"]
	got, err := UpdateRequirement(root, pkg, s.RunID, "", true, "changed", nil, RequirementUpdateOptions{ReviewDelta: &d, ImpactAuthorization: &RequirementImpactAuthorization{PreviewDigest: p.Digest, StateRevision: s.StateIntegrity, Impact: impact}})
	qaOK(t, err)
	if got.RequirementRevision != d.CandidateRevision || !got.RequirementConfirmed || got.Actions["product-review"].Status != "PENDING" || got.PreDevelopmentReviewSeries["product-review"].Completed != old.Completed || got.PreDevelopmentReviewSeries["product-review"].CurrentLimit != old.CurrentLimit || len(got.ReviewDeltaReceipts) != 1 || len(got.Dispatches) != 0 {
		t.Fatal("combined authorization failed atomic revision/rollback/budget contract")
	}
	r := got.ReviewDeltaReceipts[0]
	if r.ConfirmationSource != d.UserConfirmation || r.ImpactConfirmationSource != d.UserConfirmation || r.ImpactPreviewDigest != p.Digest || r.ImpactStateRevision != s.StateIntegrity {
		t.Fatalf("combined receipt %+v", r)
	}
}

func TestReviewDeltaPreviewChecksWholeCandidate(t *testing.T) {
	root, pkg, s, d := qaRequirementFixture(t)
	before := qaRead(t, RunStatePath(root, s.RunID))
	p, err := PreviewReviewDelta(root, pkg, s.RunID, "", nil, d)
	qaOK(t, err)
	if p.CandidateRevision != d.CandidateRevision || p.UserConfirmation != "" || p.Digest != d.Digest || qaRead(t, RunStatePath(root, s.RunID)) != before {
		t.Fatal("delta preview binding/read-only error")
	}
	qaWrite(t, filepath.Join(root, "requirement.md"), "# Incomplete candidate\n### REQ-001：missing acceptance\n")
	_, err = PreviewReviewDelta(root, pkg, s.RunID, "", nil, d)
	qaErr(t, err, "precheck")
	if qaRead(t, RunStatePath(root, s.RunID)) != before {
		t.Fatal("invalid candidate changed state")
	}
}

func TestFreezeRechecksGuaranteeCoverage(t *testing.T) {
	root, _, s, _ := qaRequirementFixture(t)
	// Construct a guarantee for the complete candidate with the production
	// activator; the source/point/case projection is never fabricated here.
	s.RequirementRevision, _ = RequirementRevision(filepath.Join(root, "requirement.md"))
	s.RequirementArtifacts, _ = requirementArtifactSet(root, "requirement.md", nil)
	qaOK(t, activateFrozenGuarantee(root, &s))
	qaApproved(&s, "whitebox")
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
