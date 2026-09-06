package validate

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"formal-gates/internal/cost"
)

const qa35bCompactRequirement = "# QA fixture\n\n## 需求点\n\n### REQ-001：公开输入行为\n\n#### 要求\n接受正常输入并提示缺失输入。\n\n#### 验收条件\n- AC-001：正常输入返回成功。\n- AC-002：缺失输入返回 required input 错误。\n\n#### 来源\nQA Design 独立结构测试 fixture。\n"

func qa35bCompleteGuarantee(t *testing.T) (string, RunState) {
	t.Helper()
	root := t.TempDir()
	qa35bWrite(t, filepath.Join(root, "requirement.md"), qa35bCompactRequirement)
	s := qa35bState()
	s.RequirementSource = "requirement.md"
	var err error
	s.RequirementArtifacts, err = requirementArtifactSet(root, s.RequirementSource, nil)
	qa35bOK(t, err)
	s.RequirementRevision = s.RequirementArtifacts[0].Revision
	qa35bOK(t, activateFrozenGuarantee(root, &s))
	qa35bApproved(&s, "whitebox")
	updateGuaranteeForRoute(&s)
	s.QACasesByMode["whitebox"][0].AcceptanceCriteria = []string{"AC-001"}
	s.QACasesByMode["whitebox"][1].AcceptanceCriteria = []string{"AC-002"}
	qa35bOK(t, recordGuaranteeReview(&s, "whitebox", s.QACasesByMode["whitebox"], QAReviewRecordOptions{SourceDecisions: []string{"REQ-001=PASS"}, PointDecisions: []string{"AC-001=PASS", "AC-002=PASS"}, CaseDecisions: []string{"CASE-A=PASS", "CASE-B=PASS"}}))
	qa35bOK(t, recordQAReviewCaseSet(&s, "whitebox", ActionResult{Status: "PASS"}, PreparedDispatch{ID: "review"}))
	return root, s
}

func TestQA35BGuaranteeIntermediateAffectedAndFinalFull(t *testing.T) {
	root, s := qa35bCompleteGuarantee(t)
	s.Actions["development-worker"] = ActionResult{Status: "PASS"}
	s.PreRepairSnapshot = "base"
	prior := QAExecutionResult{Status: "FAIL", Snapshot: "base", Cases: []QAResultRecord{{CaseID: "CASE-A", Mode: "whitebox", Outcome: "FAIL"}, {CaseID: "CASE-B", Mode: "whitebox", Outcome: "PASS"}}}
	qa35bOK(t, recordExecutionScope(&s, "whitebox", "AFFECTED", []string{"CASE-A"}, "changed input branch", "PREPARE", "base", prior))
	qa35bOK(t, requireQAExecutionTransition(s, "whitebox"))
	s.QAExecutionByMode["whitebox"] = QAExecutionResult{Status: "PASS", Snapshot: s.CurrentSnapshot, Cases: []QAResultRecord{{CaseID: "CASE-A", Mode: "whitebox", Outcome: "PASS", Origin: "executed"}, {CaseID: "CASE-B", Mode: "whitebox", Outcome: "PASS", Origin: "inherited"}}}
	if report := deriveRequirementGuarantee(root, s); report.Status == "pass" {
		t.Fatal("intermediate inherited PASS closed final FULL guarantee")
	}
	qa35bOK(t, recordExecutionScope(&s, "whitebox", "FULL", nil, "final candidate", "PREPARE", "base", prior))
	s.QAExecutionByMode["whitebox"] = QAExecutionResult{Status: "PASS", Snapshot: s.CurrentSnapshot, Cases: []QAResultRecord{{CaseID: "CASE-A", Mode: "whitebox", Outcome: "PASS", Origin: "executed"}, {CaseID: "CASE-B", Mode: "whitebox", Outcome: "PASS", Origin: "executed"}}}
	if report := deriveRequirementGuarantee(root, s); report.Status != "pass" {
		t.Fatalf("FULL could not close guarantee: %+v", report)
	}
}

func TestQA35BSuggestionDeletionCannotRemoveLastACCoverage(t *testing.T) {
	_, s := qa35bCompleteGuarantee(t)
	qa35bOK(t, recordQAReviewCaseSet(&s, "whitebox", ActionResult{Status: "PASS", Findings: []Finding{{ID: "suggestion", Severity: "P3", Message: "simplify duplicate coverage while preserving both ACs"}}}, PreparedDispatch{ID: "review-two"}))
	caseSet := s.QACaseSets["whitebox"]
	caseSet.Suggestions[0].Status, caseSet.Suggestions[0].Reason = "ACCEPT", "simplify duplicate coverage"
	// Removing the sole AC-002 case is an ordinary mistaken interpretation of a
	// simplification suggestion. Freeze must detect the missing approved edge.
	s.QACasesByMode["whitebox"] = s.QACasesByMode["whitebox"][:1]
	err := freezeQACaseSet(&s, "whitebox", &caseSet, false)
	if err == nil || s.QACaseSets["whitebox"].Status == "FROZEN" {
		t.Fatalf("last AC coverage removed without rejection: %v", err)
	}
}

func TestQA35BGatePromotionRequiresBlockingEvidence(t *testing.T) {
	root, full := qa35bFinding(t)
	advisory := FindingInput{Severity: "P2", Message: full.Message}
	_, err := strictFinding(advisory, "", true, root, qa35bState())
	qa35bOK(t, err)
	advisory.Severity = "P1"
	_, err = strictFinding(advisory, "", true, root, qa35bState())
	if err == nil {
		t.Fatal("P2 promoted without blocking evidence")
	}
	_, err = strictFinding(full, "", true, root, qa35bState())
	qa35bOK(t, err)
}

func TestQA35BPlannedRepairRefreshesAfterAdjudicationCorrection(t *testing.T) {
	s := qa35bFindingWave()
	root, pkg, s := qa35bStore(t, s)
	ids := qa35bFindingIDs(s)
	var items []FindingAdjudicationInput
	selected := ""
	for _, id := range ids {
		items = append(items, qa35bAdjudication(id, "NEW", ""))
		if s.PostReviewFindings[id].Severity == "P2" {
			selected = id
		}
	}
	s, err := AdjudicateReviewWave(root, pkg, s.RunID, ReviewWaveAdjudicationInput{WaveID: "wave", StateRevision: s.StateIntegrity, Findings: items})
	qa35bOK(t, err)
	s, err = mutateRun(root, s.RunID, func(s *RunState) error { return ensureUnifiedRepairPlan(s) })
	qa35bOK(t, err)
	oldPlan := s.ReviewWaves["wave"].RepairPlan
	dismissedProblem := s.PostReviewFindings[selected].Adjudications[0].ProblemID
	s, err = AdjudicateReviewWave(root, pkg, s.RunID, ReviewWaveAdjudicationInput{WaveID: "wave", StateRevision: s.StateIntegrity, Findings: []FindingAdjudicationInput{qa35bAdjudication(selected, "DISMISSED", "")}})
	qa35bOK(t, err)
	qa35bOK(t, ensureUnifiedRepairPlan(&s))
	plan := s.ReviewWaves["wave"].RepairPlan
	if plan.ID != oldPlan.ID || containsExactString(plan.ProblemIDs, dismissedProblem) || len(plan.ProblemIDs) != 1 {
		t.Fatalf("undispatched plan did not retain identity and refresh effective problem set: %+v; dismissed=%s", plan, dismissedProblem)
	}
}

func TestQA35BNonReviewMeaningChangeRequiresUserAuthorization(t *testing.T) {
	root, pkg, s := qa35bNativeFixture(t)
	// A newly proposed requirement, independent of any reviewer finding, is a
	// documented normal revision path and still needs rollback authorization.
	qa35bSemanticReview(t, &s, "product-review", nil)
	qa35bSemanticReview(t, &s, "start-readiness", nil)
	qa35bOK(t, SaveRunState(root, s))
	before := qa35bRead(t, RunStatePath(root, s.RunID))
	qa35bWrite(t, filepath.Join(root, "requirement.md"), "QA fixture: proposed additional public operation.\n")
	_, err := UpdateRequirement(root, pkg, s.RunID, "", false, "changed", nil)
	if err == nil || qa35bRead(t, RunStatePath(root, s.RunID)) != before {
		t.Fatalf("meaning-changed without user impact confirmation changed authoritative state: %v", err)
	}
}

func qa35bOwnerStart(t *testing.T, root, pkg, id, transcript string) RunState {
	t.Helper()
	captureStartOwner(map[string]any{"command": fmt.Sprintf("formal-gates workflow start --root %q", root), "transcript_path": transcript, "session_id": "qa-owner"}, "codex")
	s, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: id, Flow: "formal", RequirementSource: "requirement.md", VCS: "git", Route: "lightweight"})
	qa35bOK(t, err)
	return s
}

func TestQA35BStartAndAbortMeasureOwnerInterval(t *testing.T) {
	root, pkg, _ := qa35bNativeFixture(t)
	path := filepath.Join(t.TempDir(), "owner.jsonl")
	first := "{\"timestamp\":\"one\",\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"last_token_usage\":{\"input_tokens\":100,\"cached_input_tokens\":40,\"output_tokens\":20}}}}\n"
	second := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(first, "one", "two"), "100", "35"), "40", "15"), "20", "8")
	qa35bWrite(t, path, first)
	s := qa35bOwnerStart(t, root, pkg, "owner-interval", path)
	if s.OwnerProvider != "codex" || s.OwnerTranscript != path || s.OwnerCostBaseline == nil || s.OwnerCostCompleted || s.OwnerCostBaseline.Usage != (cost.Usage{InputCacheHitTokens: 40, InputCacheMissTokens: 60, OutputTokens: 20}) {
		t.Fatalf("start baseline %+v", s.OwnerCostBaseline)
	}
	qa35bWrite(t, path, first+second)
	summary, err := Abort(root, s.RunID)
	qa35bOK(t, err)
	if summary.Status != "ABORTED" || summary.Cost.Owner.Source != cost.SourceTranscript || summary.Cost.Owner.TotalInputTokens != 35 || summary.Cost.Owner.InputCacheHitTokens != 15 || summary.Cost.Owner.OutputTokens != 8 || len(summary.Cost.Dispatches) != 0 {
		t.Fatalf("owner abort %+v", summary.Cost)
	}
}

func TestQA35BTwoActiveOwnersBothBecomeUnavailable(t *testing.T) {
	root, pkg, _ := qa35bNativeFixture(t)
	path := filepath.Join(t.TempDir(), "owner.jsonl")
	qa35bWrite(t, path, "{\"timestamp\":\"one\",\"type\":\"event_msg\",\"payload\":{\"type\":\"token_count\",\"info\":{\"last_token_usage\":{\"input_tokens\":10,\"output_tokens\":4}}}}\n")
	first := qa35bOwnerStart(t, root, pkg, "owner-one", path)
	second := qa35bOwnerStart(t, root, pkg, "owner-two", path)
	first, err := LoadRunState(root, first.RunID)
	qa35bOK(t, err)
	if !first.OwnerCostOverlapped || !second.OwnerCostOverlapped {
		t.Fatal("overlap not persisted on both runs")
	}
	for _, id := range []string{first.RunID, second.RunID} {
		summary, err := Abort(root, id)
		qa35bOK(t, err)
		if summary.Cost.Owner.Source != cost.SourceUnavailable || summary.Cost.TotalInputTokens != 0 || summary.Cost.OutputTokens != 0 {
			t.Fatalf("overlap estimated %+v", summary.Cost)
		}
	}
}

func TestQA35BSealKeepsP3InFinalSummary(t *testing.T) {
	root, pkg, s := qa35bNativeFixture(t)
	s.Actions["development-worker"] = ActionResult{Status: "VERIFIED"}
	s.SelectedGates = []string{"complexity-gate"}
	s.Gates["complexity-gate"] = GateResult{Status: "PASS", Snapshot: s.CurrentSnapshot, Findings: []Finding{{ID: "raw", Severity: "P3", Message: "optional wording"}}}
	s.PostReviewFindings["advisory"] = PostReviewFinding{ID: "advisory", SourceKind: "gate", SourceTarget: "complexity-gate", Severity: "P3", Message: "optional wording"}
	qa35bOK(t, SaveRunState(root, s))
	summary, err := Seal(root, pkg, s.RunID, nil, false, "")
	qa35bOK(t, err)
	if summary.Status != "SEALED" || len(summary.GateP3Advisories) != 1 || summary.GateP3Advisories[0].ID != "advisory" || summary.Gates["complexity-gate"].Status != "PASS" {
		t.Fatalf("seal lost advisory or changed PASS: %+v", summary)
	}
}
