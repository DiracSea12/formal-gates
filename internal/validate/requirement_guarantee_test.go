package validate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func guaranteeRequirementDocument(secondText string) string {
	if secondText == "" {
		secondText = "第二个结果可观察并通过。"
	}
	return `# 正式需求案

这里保留自然语言背景、方案与风险。

` + "```markdown\n## 需求点\n### REQ-999：围栏示例\n- AC-999：不参与解析\n```" + `

## 需求点

### REQ-001：公开行为

#### 要求

必须完成第一个行为，并且必须完成第二个行为。

#### 验收条件

- AC-001：第一个结果可观察并通过。
- AC-009：` + secondText + `

#### 来源

用户确认的对齐结论。
`
}

func assertItemizedGuaranteeReport(t *testing.T, report RequirementGuaranteeReport, status, requirementStatus string) {
	t.Helper()
	if report.Status != status || report.RequirementCount != 1 || report.AcceptanceCount != 2 || len(report.Requirements) != 1 || len(report.Items) != 2 {
		t.Fatalf("guarantee report omitted REQ/AC detail for %s: %#v", status, report)
	}
	if report.Requirements[0].RequirementID != "REQ-001" || report.Requirements[0].Status != requirementStatus || len(report.Requirements[0].AcceptanceIDs) != 2 || len(report.Requirements[0].Owners) == 0 {
		t.Fatalf("guarantee report omitted aggregate REQ completion for %s: %#v", status, report.Requirements)
	}
}

func TestRequirementProjectionParserContract(t *testing.T) {
	t.Run("valid natural prose fences and nonconsecutive ids", func(t *testing.T) {
		projection, err := ParseRequirementProjection("requirements.md", []byte(guaranteeRequirementDocument("")))
		if err != nil {
			t.Fatal(err)
		}
		if len(projection.Requirements) != 1 || projection.Requirements[0].ID != "REQ-001" || len(projection.Requirements[0].AcceptanceConditions) != 2 {
			t.Fatalf("unexpected projection: %#v", projection)
		}
		if projection.Requirements[0].AcceptanceConditions[1].ID != "AC-009" || projection.ContentDigest == "" {
			t.Fatalf("nonconsecutive ids or digest were lost: %#v", projection)
		}
	})

	t.Run("no finite item limit", func(t *testing.T) {
		var blocks []string
		for i := 1; i <= 160; i++ {
			blocks = append(blocks, fmt.Sprintf("### REQ-%03d：需求 %d\n\n#### 要求\n\n要求 %d。\n\n#### 验收条件\n\n- AC-%03d：结果 %d。\n\n#### 来源\n\n来源 %d。", i, i, i, i, i, i))
		}
		doc := "# 大需求案\n\n## 需求点\n\n" + strings.Join(blocks, "\n\n") + "\n"
		projection, err := ParseRequirementProjection("large.md", []byte(doc))
		if err != nil {
			t.Fatal(err)
		}
		if len(projection.Requirements) != 160 {
			t.Fatalf("parsed %d requirements", len(projection.Requirements))
		}
	})

	cases := []struct {
		name string
		doc  []byte
		want string
	}{
		{"invalid utf8", []byte{0xff, 0xfe}, "valid UTF-8"},
		{"missing section", []byte("# x\n"), "exactly one ## 需求点"},
		{"multiple sections", []byte("## 需求点\n\n### REQ-001：x\n\n#### 要求\n\nx\n\n#### 验收条件\n\n- AC-001：x\n\n#### 来源\n\nx\n\n## other\n\n## 需求点\n"), "exactly one ## 需求点"},
		{"invalid req heading", []byte("## 需求点\n\n### not-a-req\n"), "must match REQ"},
		{"duplicate req", []byte("## 需求点\n\n### REQ-001：a\n\n#### 要求\n\na\n\n#### 验收条件\n\n- AC-001：a\n\n#### 来源\n\na\n\n### REQ-001：b\n\n#### 要求\n\nb\n\n#### 验收条件\n\n- AC-002：b\n\n#### 来源\n\nb\n"), "duplicate REQ ID"},
		{"duplicate ac", []byte("## 需求点\n\n### REQ-001：a\n\n#### 要求\n\na\n\n#### 验收条件\n\n- AC-001：a\n- AC-001：b\n\n#### 来源\n\na\n"), "duplicate AC ID"},
		{"field order", []byte("## 需求点\n\n### REQ-001：a\n\n#### 验收条件\n\n- AC-001：a\n\n#### 要求\n\na\n\n#### 来源\n\na\n"), "要求 → 验收条件 → 来源"},
		{"empty requirement", []byte("## 需求点\n\n### REQ-001：a\n\n#### 要求\n\n#### 验收条件\n\n- AC-001：a\n\n#### 来源\n\na\n"), "要求 field must contain"},
		{"malformed ac", []byte("## 需求点\n\n### REQ-001：a\n\n#### 要求\n\na\n\n#### 验收条件\n\n- not-an-ac\n\n#### 来源\n\na\n"), "must match - AC"},
		{"extra field", []byte("## 需求点\n\n### REQ-001：a\n\n#### 要求\n\na\n\n#### 额外\n\nx\n\n#### 验收条件\n\n- AC-001：a\n\n#### 来源\n\na\n"), "may contain only"},
		{"req outside section", []byte("### REQ-001：outside\n\n## 需求点\n\n### REQ-002：a\n\n#### 要求\n\na\n\n#### 验收条件\n\n- AC-002：a\n\n#### 来源\n\na\n"), "outside ## 需求点"},
		{"ac outside field", []byte("- AC-777：outside\n\n## 需求点\n\n### REQ-001：a\n\n#### 要求\n\na\n\n#### 验收条件\n\n- AC-001：a\n\n#### 来源\n\na\n"), "outside its REQ"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseRequirementProjection("bad.md", tc.doc)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "bad.md:") {
				t.Fatalf("error=%v, want diagnostic containing %q and path/line", err, tc.want)
			}
		})
	}
}

func TestRequirementProjectionReportsRecognizableIDForEmptyREQTitle(t *testing.T) {
	document := "## 需求点\n\n### REQ-001：\n"
	_, err := ParseRequirementProjection("requirements.md", []byte(document))
	if err == nil || !strings.Contains(err.Error(), "requirements.md:3 [REQ-001]") || !strings.Contains(err.Error(), "non-empty title") {
		t.Fatalf("empty REQ title diagnostic = %v", err)
	}
}

func TestRequirementProjectionDistinguishesShortACNumberFromEmptyACText(t *testing.T) {
	document := func(item string) string {
		return "## 需求点\n\n### REQ-001：Title\n\n#### 要求\n\nrequirement\n\n#### 验收条件\n\n- " + item + "\n\n#### 来源\n\nuser\n"
	}
	_, shortErr := ParseRequirementProjection("short.md", []byte(document("AC-1：condition")))
	if shortErr == nil || !strings.Contains(shortErr.Error(), "short.md:11 [REQ-001]") || !strings.Contains(shortErr.Error(), "AC number must contain at least three digits") || strings.Contains(shortErr.Error(), "AC condition text must be non-empty") {
		t.Fatalf("short AC diagnostic = %v", shortErr)
	}
	_, emptyErr := ParseRequirementProjection("empty.md", []byte(document("AC-001：")))
	if emptyErr == nil || !strings.Contains(emptyErr.Error(), "empty.md:11 [REQ-001]") || !strings.Contains(emptyErr.Error(), "AC condition text must be non-empty") || strings.Contains(emptyErr.Error(), "AC number must contain at least three digits") {
		t.Fatalf("empty AC diagnostic = %v", emptyErr)
	}
}

func startGuaranteeRun(t *testing.T, id string, retained bool) (string, string, RunState) {
	t.Helper()
	root, pkg := workflowFixture(t)
	writeTestFile(t, filepath.Join(root, "requirements.md"), guaranteeRequirementDocument(""))
	commitAll(t, root, "structured requirement")
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: id, Flow: formalFlow, RequirementSource: "requirements.md", VCS: "git", RetainedOverall: retained, Split: map[bool]string{true: "yes", false: "no"}[retained]})
	if err != nil {
		t.Fatal(err)
	}
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "requirements-clarification")
	state, err = RecordAction(root, pkg, state.RunID, "requirements-clarification", dispatch, "PASS", "", nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	state, err = UpdateRequirement(root, pkg, state.RunID, "", true, "", nil, RequirementUpdateOptions{ActivateGuarantee: true})
	if err != nil {
		t.Fatal(err)
	}
	if state.RequirementGuarantee == nil || state.RequirementGuarantee.Activation != guaranteeFrozen || state.RequirementGuarantee.Projection == nil {
		t.Fatalf("confirmation did not atomically freeze the guarantee: %#v", state.RequirementGuarantee)
	}
	assertItemizedGuaranteeReport(t, state.RequirementGuarantee.Report, guaranteeFrozen, "INCOMPLETE")
	return root, pkg, state
}

func passGuaranteeProductReview(t *testing.T, root, pkg string, state RunState) RunState {
	t.Helper()
	prompt, err := PrepareAction(root, pkg, state.RunID, "product-review", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "independently omissible mandatory obligation") || !strings.Contains(prompt, "REQ-001 要求") {
		t.Fatalf("product review prompt omitted obligation-to-AC completeness: %s", prompt)
	}
	state, err = LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	dispatch := openDispatchID(state, "action", "product-review")
	if _, err := ClaimDispatch(root, pkg, state.RunID, dispatch, state.RunID+"-product-review"); err != nil {
		t.Fatal(err)
	}
	items := make([]ReviewItemInput, 0, len(state.RequirementGuarantee.Projection.Requirements))
	for _, req := range state.RequirementGuarantee.Projection.Requirements {
		items = append(items, ReviewItemInput{Key: guaranteeProductReviewKey(req.ID), Status: "PASS"})
	}
	state, err = RecordAction(root, pkg, state.RunID, "product-review", dispatch, "PASS", "", nil, false, "", items...)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func readyGuaranteeRoute(t *testing.T, id string, retained bool) (string, string, RunState) {
	t.Helper()
	root, pkg, state := startGuaranteeRun(t, id, retained)
	state = passGuaranteeProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	return root, pkg, state
}

func guaranteeReviewInputs(state RunState, mode string) ([]QAReviewInput, QAReviewRecordOptions) {
	cases := state.qaModeCases(mode)
	var decisions []QAReviewInput
	var caseDecisions []string
	for _, testCase := range cases {
		if testCase.ReviewStatus != "PASS" {
			decisions = append(decisions, QAReviewInput{CaseID: testCase.ID, Outcome: "PASS"})
		}
		caseDecisions = append(caseDecisions, testCase.ID+"=PASS")
	}
	merge := state.RetainedOverall && state.Slicing != nil && state.Slicing.Decision == "split"
	sources, points := guaranteeReviewDecisionIDs(state, coverageKind(mode, merge), cases)
	sourceDecisions := make([]string, 0, len(sources))
	for _, id := range sources {
		sourceDecisions = append(sourceDecisions, id+"=PASS")
	}
	pointDecisions := make([]string, 0, len(points))
	for _, pointID := range points {
		parts := strings.SplitN(pointID, "::", 2)
		pointDecisions = append(pointDecisions, parts[len(parts)-1]+"=PASS")
	}
	sort.Strings(caseDecisions)
	return decisions, QAReviewRecordOptions{SourceDecisions: sourceDecisions, PointDecisions: pointDecisions, CaseDecisions: caseDecisions}
}

func recordGuaranteeDesignAndReview(t *testing.T, root, pkg string, state RunState, mode string, cases []QACaseInput) RunState {
	t.Helper()
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design", mode)
	state, err := RecordQADesign(root, pkg, state.RunID, design, cases, "")
	if err != nil {
		t.Fatal(err)
	}
	reviewPrompt, err := PrepareAction(root, pkg, state.RunID, "qa-review", mode, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(reviewPrompt, "--source-decision") || !strings.Contains(reviewPrompt, "--point-decision") || !strings.Contains(reviewPrompt, "--case-decision") {
		t.Fatalf("QA review prompt omitted explicit three-level decisions: %s", reviewPrompt)
	}
	state, err = LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	review := openDispatchID(state, "action", "qa-review")
	if _, err := ClaimDispatch(root, pkg, state.RunID, review, state.RunID+"-qa-review-"+mode); err != nil {
		t.Fatal(err)
	}
	decisions, options := guaranteeReviewInputs(state, mode)
	state, err = RecordQAReview(root, pkg, state.RunID, review, decisions, "", nil, options)
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func advanceGuaranteeSnapshot(t *testing.T, root, pkg string, state RunState, name string) RunState {
	t.Helper()
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	writeTestFile(t, filepath.Join(root, name+".txt"), "delivery\n")
	commitAll(t, root, "delivery "+name)
	state, err := AdvanceSnapshot(root, pkg, state.RunID, dispatch, false, "")
	if err != nil {
		t.Fatal(err)
	}
	return state
}

func TestRequirementConfirmationPrecheckAndProductCompletenessGate(t *testing.T) {
	root, pkg := workflowFixture(t)
	writeTestFile(t, filepath.Join(root, "requirements.md"), "not a structured requirement\n")
	commitAll(t, root, "invalid requirement")
	state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "precheck-reject", Flow: formalFlow, RequirementSource: "requirements.md", VCS: "git", Split: "no"})
	if err != nil {
		t.Fatal(err)
	}
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "requirements-clarification")
	if _, err := RecordAction(root, pkg, state.RunID, "requirements-clarification", dispatch, "PASS", "", nil, false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := UpdateRequirement(root, pkg, state.RunID, "", false, "", nil); err == nil || !strings.Contains(err.Error(), "precheck failed") {
		t.Fatalf("pre-confirmation registration accepted an invalid single requirement artifact: %v", err)
	}
	if _, err := UpdateRequirement(root, pkg, state.RunID, "", true, "", nil); err == nil || !strings.Contains(err.Error(), "precheck failed") {
		t.Fatalf("plain confirmation accepted an invalid single requirement artifact: %v", err)
	}
	state, _ = LoadRunState(root, state.RunID)
	if state.RequirementConfirmed || state.RequirementGuarantee != nil {
		t.Fatalf("failed precheck partially committed confirmation: %#v", state)
	}
	if _, err := UpdateRequirement(root, pkg, state.RunID, "", true, "", nil, RequirementUpdateOptions{ActivateGuarantee: true}); err == nil || !strings.Contains(err.Error(), "precheck failed") {
		t.Fatalf("guarantee activation accepted an invalid requirement: %v", err)
	}

	root, pkg, state = startGuaranteeRun(t, "product-completeness", false)
	_, err = PrepareAction(root, pkg, state.RunID, "product-review", "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	state, _ = LoadRunState(root, state.RunID)
	dispatch = openDispatchID(state, "action", "product-review")
	if _, err := ClaimDispatch(root, pkg, state.RunID, dispatch, "product-completeness-reviewer"); err != nil {
		t.Fatal(err)
	}
	bad := []ReviewItemInput{{Key: guaranteeProductReviewKey("REQ-001"), Status: "FAIL", Reason: "second mandatory obligation has no AC"}}
	if _, err := RecordAction(root, pkg, state.RunID, "product-review", dispatch, "PASS", "", nil, false, "", bad...); err == nil || !strings.Contains(err.Error(), "cannot PASS") {
		t.Fatalf("product review passed an uncovered obligation: %v", err)
	}
}

func TestRequirementGuaranteeNonSplitPassAndWaiver(t *testing.T) {
	t.Run("complete closure", func(t *testing.T) {
		root, pkg, state := readyGuaranteeRoute(t, "guarantee-pass", false)
		state = recordSlicing(t, root, pkg, state, "no-split")
		state = setRoute(t, root, pkg, state, "custom", []string{blackboxQAID})
		if state.RequirementGuarantee.Activation != guaranteeActive {
			t.Fatalf("QA route did not activate guarantee: %#v", state.RequirementGuarantee)
		}
		state = recordGuaranteeDesignAndReview(t, root, pkg, state, "", []QACaseInput{{Mode: "blackbox", Description: "all confirmed behavior", Procedure: "use the public command", Oracle: "both outcomes pass", AcceptanceCriteria: []string{"AC-001", "AC-009"}}})
		state = advanceGuaranteeSnapshot(t, root, pkg, state, "guarantee-pass")
		execution := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
		state, err := RecordQAExecution(root, pkg, state.RunID, execution, passingExecution(state.allQACases()), "")
		if err != nil {
			t.Fatal(err)
		}
		if state.qaExecution("").Status != "PASS" || state.RequirementGuarantee.Report.Status != "pass" {
			t.Fatalf("complete closure did not pass: QA=%#v guarantee=%#v", state.qaExecution(""), state.RequirementGuarantee.Report)
		}
		shown, err := LoadRunStateForShow(root, state.RunID)
		if err != nil {
			t.Fatal(err)
		}
		if len(shown.RequirementGuarantee.Report.Items) != 2 || shown.RequirementGuarantee.Report.RequirementCount != 1 || shown.RequirementGuarantee.Report.AcceptanceCount != 2 {
			t.Fatalf("show omitted itemized guarantee state: %#v", shown.RequirementGuarantee.Report)
		}
		assertItemizedGuaranteeReport(t, shown.RequirementGuarantee.Report, "pass", "PASS")
		summary, err := Seal(root, pkg, state.RunID, nil, false, "")
		if err != nil {
			t.Fatal(err)
		}
		if summary.RequirementGuarantee == nil || summary.RequirementGuarantee.Report.Status != "pass" {
			t.Fatalf("seal omitted passing guarantee: %#v", summary.RequirementGuarantee)
		}
	})

	t.Run("incomplete closure requires explicit audited waiver", func(t *testing.T) {
		root, pkg, state := readyGuaranteeRoute(t, "guarantee-waiver", false)
		state = recordSlicing(t, root, pkg, state, "no-split")
		state = setRoute(t, root, pkg, state, "custom", []string{blackboxQAID})
		state = recordGuaranteeDesignAndReview(t, root, pkg, state, "", []QACaseInput{{Mode: "blackbox", Description: "partial behavior", Procedure: "use the public command", Oracle: "first outcome passes", AcceptanceCriteria: []string{"AC-001"}}})
		state = advanceGuaranteeSnapshot(t, root, pkg, state, "guarantee-waiver")
		execution := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
		state, err := RecordQAExecution(root, pkg, state.RunID, execution, passingExecution(state.allQACases()), "")
		if err != nil {
			t.Fatal(err)
		}
		if state.qaExecution("").Status != "FAIL" || state.RequirementGuarantee.Report.Status != "incomplete" {
			t.Fatalf("partial closure incorrectly passed: QA=%#v guarantee=%#v", state.qaExecution(""), state.RequirementGuarantee.Report)
		}
		assertItemizedGuaranteeReport(t, state.RequirementGuarantee.Report, "incomplete", "INCOMPLETE")
		if state.RequirementGuarantee.Report.Items[0].ReviewStatus != "PASS" || state.RequirementGuarantee.Report.Items[0].Execution != "PASS" || state.RequirementGuarantee.Report.Items[1].ReviewStatus != "PENDING" || state.RequirementGuarantee.Report.Items[1].Execution != "PENDING" {
			t.Fatalf("partial closure did not retain per-AC review/execution evidence: %#v", state.RequirementGuarantee.Report.Items)
		}
		if _, err := Seal(root, pkg, state.RunID, nil, false, ""); err == nil || !strings.Contains(err.Error(), "requirement guarantee is not complete") {
			t.Fatalf("normal seal accepted incomplete guarantee: %v", err)
		}
		summary, err := Seal(root, pkg, state.RunID, []string{guaranteeSealSkipID}, true, "", SealOptions{GuaranteeWaiverReason: "AC-009=user accepts the listed missing coverage and execution gap"})
		if err != nil {
			t.Fatal(err)
		}
		if summary.RequirementGuarantee.Activation != guaranteeWaived || summary.RequirementGuarantee.Waiver == nil || len(summary.RequirementGuarantee.Waiver.Unresolved) == 0 {
			t.Fatalf("waived seal lost audit detail: %#v", summary.RequirementGuarantee)
		}
		assertItemizedGuaranteeReport(t, summary.RequirementGuarantee.Report, guaranteeWaived, "INCOMPLETE")
	})
}

func TestActiveGuaranteeRepairRequiresCurrentCandidateFullExecution(t *testing.T) {
	root, pkg, state := readyGuaranteeRoute(t, "guarantee-repair-full", false)
	state = recordSlicing(t, root, pkg, state, "no-split")
	state = setRoute(t, root, pkg, state, "custom", []string{blackboxQAID, "quality"})
	state = recordGuaranteeDesignAndReview(t, root, pkg, state, "", []QACaseInput{{Mode: "blackbox", Description: "all confirmed behavior", Procedure: "use the public command", Oracle: "both outcomes pass", AcceptanceCriteria: []string{"AC-001", "AC-009"}}})
	state = advanceGuaranteeSnapshot(t, root, pkg, state, "guarantee-repair-initial")
	oldSnapshot := state.CurrentSnapshot
	execution := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err := RecordQAExecution(root, pkg, state.RunID, execution, passingExecution(state.allQACases()), "")
	if err != nil {
		t.Fatal(err)
	}
	state = recordGateResult(t, root, pkg, state, "quality", "guarantee-repair-quality", "FAIL", "", []FindingInput{{Severity: "P1", Message: "repair the implementation"}})
	if state.CompletedReviewWaves != 1 || state.Actions["development-worker"].Status != developmentVerified {
		t.Fatalf("blocking review wave did not open a repair round: %#v", state.Actions["development-worker"])
	}

	state = advanceGuaranteeSnapshot(t, root, pkg, state, "guarantee-repair-current")
	if state.PreRepairSnapshot != oldSnapshot {
		t.Fatalf("pre-repair snapshot = %q, want %q", state.PreRepairSnapshot, oldSnapshot)
	}
	if current := state.qaExecution(""); current.Status != "PENDING" {
		t.Fatalf("old guarantee QA result stayed current after repair: %#v", current)
	}
	prior := state.priorQAExecution("")
	if prior == nil || prior.Status != "PASS" || prior.Snapshot != oldSnapshot {
		t.Fatalf("old guarantee QA result was not retained only as prior evidence: %#v", prior)
	}
	if eligible := eligibleMainCarryResults(state, false); len(eligible) != 0 {
		t.Fatalf("active guarantee exposed prior QA PASS to Carry: %v", eligible)
	}
	if _, err := RecordCarry(root, pkg, state.RunID, "", nil, "", true, "implementation repair does not alter behavior"); err == nil || !strings.Contains(err.Error(), "no prior passing selected results") {
		t.Fatalf("active guarantee accepted QA Carry before scope: %v", err)
	}
	if _, err := RecordExecutionScope(root, pkg, state.RunID, "", "AFFECTED", []string{"CASE-001"}, "rerun one case"); err == nil || !strings.Contains(err.Error(), "requires FULL QA execution") {
		t.Fatalf("active guarantee accepted AFFECTED repair execution: %v", err)
	}
	state, err = RecordExecutionScope(root, pkg, state.RunID, "", "FULL", nil, "rerun every approved case on the repaired candidate")
	if err != nil {
		t.Fatal(err)
	}
	if state.ExecutionScopes[""].Decision != "FULL" {
		t.Fatalf("FULL repair execution scope was not recorded: %#v", state.ExecutionScopes)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-execution", "", false, ""); err != nil {
		t.Fatalf("current-candidate FULL QA execution was not dispatchable: %v", err)
	}
}

func TestRequirementGuaranteeRouteArtifactAndRevisionBehavior(t *testing.T) {
	t.Run("custom without QA is explicit not guaranteed and full is active", func(t *testing.T) {
		root, pkg, state := readyGuaranteeRoute(t, "custom-no-qa", false)
		state = recordSlicing(t, root, pkg, state, "no-split")
		state = setRoute(t, root, pkg, state, "custom", []string{"quality"})
		if state.RequirementGuarantee.Activation != guaranteeNotGuaranteed || state.RequirementGuarantee.Report.Status != guaranteeNotGuaranteed {
			t.Fatalf("custom no-QA route was not marked not-guaranteed: %#v", state.RequirementGuarantee)
		}
		assertItemizedGuaranteeReport(t, state.RequirementGuarantee.Report, guaranteeNotGuaranteed, "INCOMPLETE")

		root, pkg, state = readyGuaranteeRoute(t, "full-active", false)
		state = recordSlicing(t, root, pkg, state, "no-split")
		state = setRoute(t, root, pkg, state, "full", nil)
		if state.RequirementGuarantee.Activation != guaranteeActive || !isSelectedQA(state) {
			t.Fatalf("full route did not enter active guarantee: %#v", state)
		}
	})

	t.Run("formal confirmation does not infer activation without the explicit flag", func(t *testing.T) {
		root, pkg := workflowFixture(t)
		writeTestFile(t, filepath.Join(root, "requirements.md"), guaranteeRequirementDocument(""))
		commitAll(t, root, "structured requirement")
		state, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "no-inference", Flow: formalFlow, RequirementSource: "requirements.md", VCS: "git", Split: "no"})
		if err != nil {
			t.Fatal(err)
		}
		dispatch := prepareDispatch(t, root, pkg, state.RunID, "requirements-clarification")
		if _, err := RecordAction(root, pkg, state.RunID, "requirements-clarification", dispatch, "PASS", "", nil, false, ""); err != nil {
			t.Fatal(err)
		}
		state, err = UpdateRequirement(root, pkg, state.RunID, "", true, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		if state.RequirementGuarantee != nil {
			t.Fatalf("formal confirmation inferred a guarantee activation: %#v", state.RequirementGuarantee)
		}
	})

	t.Run("additional requirement artifact blocks QA design", func(t *testing.T) {
		root, pkg, state := startGuaranteeRun(t, "artifact-block", false)
		state, err := UpdateRequirement(root, pkg, state.RunID, "", true, "preserved", []string{"design.md"})
		if err != nil {
			t.Fatal(err)
		}
		state = passGuaranteeProductReview(t, root, pkg, state)
		state = recordReadiness(t, root, pkg, state)
		state = recordSlicing(t, root, pkg, state, "no-split")
		state = setRoute(t, root, pkg, state, "full", nil)
		if state.RequirementGuarantee.Activation != guaranteeBlocked {
			t.Fatalf("extra artifact did not block guarantee: %#v", state.RequirementGuarantee)
		}
		assertItemizedGuaranteeReport(t, state.RequirementGuarantee.Report, guaranteeBlocked, "INCOMPLETE")
		if _, err := PrepareAction(root, pkg, state.RunID, "qa-design", "", false, ""); err == nil || !strings.Contains(err.Error(), "exactly one") {
			t.Fatalf("QA design accepted multiple requirement artifacts: %v", err)
		}
	})

	t.Run("revision drift invalidates all guarantee evidence", func(t *testing.T) {
		root, pkg, state := readyGuaranteeRoute(t, "guarantee-drift", false)
		state = recordSlicing(t, root, pkg, state, "no-split")
		state = setRoute(t, root, pkg, state, "custom", []string{blackboxQAID})
		state = recordGuaranteeDesignAndReview(t, root, pkg, state, "", []QACaseInput{{Mode: "blackbox", Description: "behavior", Procedure: "run", Oracle: "pass", AcceptanceCriteria: []string{"AC-001", "AC-009"}}})
		oldDigest := state.RequirementGuarantee.ManifestDigest
		writeTestFile(t, filepath.Join(root, "requirements.md"), guaranteeRequirementDocument("修订后的第二个结果可观察并通过。"))
		commitAll(t, root, "requirement revision")
		state, err := UpdateRequirement(root, pkg, state.RunID, "", true, "preserved", nil)
		if err != nil {
			t.Fatal(err)
		}
		if !state.RequirementConfirmed || state.RequirementGuarantee.Activation != guaranteeActive || state.RequirementGuarantee.ManifestDigest == oldDigest || len(state.RequirementGuarantee.ReviewsByMode) != 0 || len(state.QAExecutionByMode) != 0 {
			t.Fatalf("revision drift retained stale guarantee evidence: %#v", state.RequirementGuarantee)
		}
		for _, testCase := range state.allQACases() {
			if testCase.ReviewStatus != "PENDING" {
				t.Fatalf("stale case approval survived revision drift: %#v", testCase)
			}
		}
	})
}

func TestRequirementGuaranteeFullRouteUsesUnionAcrossQAModes(t *testing.T) {
	root, pkg, state := readyGuaranteeRoute(t, "full-union", false)
	state = recordSlicing(t, root, pkg, state, "no-split")
	state = setRoute(t, root, pkg, state, "full", nil)
	state = recordGuaranteeDesignAndReview(t, root, pkg, state, "", []QACaseInput{{Mode: "blackbox", Description: "first behavior", Procedure: "run public command", Oracle: "first passes", AcceptanceCriteria: []string{"AC-001"}}})
	state = advanceGuaranteeSnapshot(t, root, pkg, state, "full-union")

	const whiteboxTestFile = "whitebox_delivered_test.go"
	writeTestFile(t, filepath.Join(root, whiteboxTestFile), whiteboxDeliveredTestCode)
	state = recordGuaranteeDesignAndReview(t, root, pkg, state, whiteboxQAID, []QACaseInput{{Mode: "whitebox", Description: "second structure", Procedure: "run structure test", Oracle: "second passes", Test: whiteboxTestFile + "::TestWhiteboxStructure", AcceptanceCriteria: []string{"AC-009"}}})
	commitAll(t, root, "whitebox guarantee test")
	var err error
	state, err = AdvanceSnapshot(root, pkg, state.RunID, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	execution := prepareDispatch(t, root, pkg, state.RunID, "qa-execution")
	state, err = RecordQAExecution(root, pkg, state.RunID, execution, passingExecution(state.allQACases()), "")
	if err != nil {
		t.Fatal(err)
	}
	if state.RequirementGuarantee.Report.Status != "pass" {
		t.Fatalf("union coverage across selected QA modes did not pass: %#v", state.RequirementGuarantee.Report)
	}
}

func TestRequirementGuaranteeReviewRequiresOnlyTheModeRepresentedProjection(t *testing.T) {
	state := whitebox35AFixR2GuaranteedState(t)
	cases := []QACase{{
		ID:                 "CASE-ONLY-ONE-AC",
		Mode:               whiteboxQAID,
		Oracle:             "the covered condition passes",
		Test:               "requirement_guarantee_test.go::TestRequirementGuaranteeReviewRequiresOnlyTheModeRepresentedProjection",
		AcceptanceCriteria: []string{"AC-001"},
		ReviewStatus:       "PASS",
	}}
	partial := QAReviewRecordOptions{
		SourceDecisions: []string{"REQ-001=PASS"},
		PointDecisions:  []string{"AC-001=PASS"},
		CaseDecisions:   []string{"CASE-ONLY-ONE-AC=PASS"},
	}
	if err := recordGuaranteeReview(&state, whiteboxQAID, cases, partial); err != nil {
		t.Fatalf("mode-local REQ/AC review was rejected: %v", err)
	}
	state.RequirementGuarantee.ReviewsByMode = map[string]GuaranteeReviewRecord{}

	complete := QAReviewRecordOptions{
		SourceDecisions: []string{"REQ-001=PASS", "REQ-008=PASS"},
		PointDecisions:  []string{"AC-001=PASS", "AC-002=PASS", "AC-011=PASS"},
		CaseDecisions:   []string{"CASE-ONLY-ONE-AC=PASS"},
	}
	if err := recordGuaranteeReview(&state, whiteboxQAID, cases, complete); err == nil || !strings.Contains(err.Error(), "unknown source decision") {
		t.Fatalf("cross-mode REQ/AC decisions were accepted by one mode: %v", err)
	}
	if err := recordGuaranteeReview(&state, whiteboxQAID, cases, partial); err != nil {
		t.Fatal(err)
	}
	record := state.RequirementGuarantee.ReviewsByMode[whiteboxQAID]
	if len(record.Review.SourceDecisions) != 1 || len(record.Review.PointDecisions) != 1 || record.Whitelist == nil {
		t.Fatalf("mode-local explicit review was not retained before union projection: %#v", record)
	}
	if report := deriveRequirementGuarantee(t.TempDir(), state); report.Status == "pass" {
		t.Fatalf("explicit decisions hid missing case coverage: %#v", report)
	}
}

func TestRequirementRevisionChangeRequiresSplitResponsibilitiesToBeRecordedAgain(t *testing.T) {
	root, pkg, state := readyGuaranteeRoute(t, "split-revision-responsibilities", true)
	slices := []string{"revision-slice-a", "revision-slice-b"}
	parallel := "the two responsibility slices may run in parallel"
	state, err := RecordSlicing(root, pkg, state.RunID, "split", len(slices), slices, parallel, "", "", SlicingAmendOptions{ACResponsibilities: []string{"AC-001=revision-slice-a", "AC-009=master-merge"}})
	if err != nil {
		t.Fatal(err)
	}

	writeTestFile(t, filepath.Join(root, "requirements.md"), guaranteeRequirementDocument("修订后仍可观察，但责任必须重新确认。"))
	commitAll(t, root, "revise split requirement")
	state, err = UpdateRequirement(root, pkg, state.RunID, "", true, "preserved", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.Slicing != nil {
		t.Fatalf("requirement revision retained stale split responsibility binding: %#v", state.Slicing)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "qa-design", "", false, ""); err == nil || !strings.Contains(err.Error(), "responsibility binding is missing") {
		t.Fatalf("revision-invalidated split run could claim QA ownership before rebinding: %v", err)
	}

	state, err = RecordSlicing(root, pkg, state.RunID, "split", len(slices), slices, parallel, "", "", SlicingAmendOptions{ACResponsibilities: []string{"AC-001=revision-slice-b", "AC-009=master-merge"}})
	if err != nil {
		t.Fatalf("record split responsibilities for the new revision: %v", err)
	}
	if got := state.Slicing.ACResponsibilities["AC-001"]; got != "revision-slice-b" {
		t.Fatalf("new revision responsibility = %q, want revision-slice-b", got)
	}
}

func TestRequirementGuaranteeSplitMasterFinalFullExecution(t *testing.T) {
	root, pkg, master := readyGuaranteeRoute(t, "guarantee-master", true)
	var err error
	master, err = RecordSlicing(root, pkg, master.RunID, "split", 2, []string{"guarantee-slice", "unowned-slice"}, "slices may run in parallel", "", "", SlicingAmendOptions{ACResponsibilities: []string{"AC-001=guarantee-slice", "AC-009=" + masterMergeOwner}})
	if err != nil {
		t.Fatal(err)
	}
	if master.RequirementGuarantee.Activation != guaranteeActive {
		t.Fatalf("retained master guarantee not active: %#v", master.RequirementGuarantee)
	}
	master = recordGuaranteeDesignAndReview(t, root, pkg, master, "", []QACaseInput{{Mode: "blackbox", Description: "merge interaction", Procedure: "exercise merged candidate", Oracle: "integration passes", AcceptanceCriteria: []string{"AC-009"}}})

	slice, err := Start(StartOptions{Root: root, PackageRoot: pkg, RunID: "guarantee-slice", Flow: formalFlow, RequirementSource: "requirements.md", VCS: "git", Split: "yes", MasterRunID: master.RunID})
	if err != nil {
		t.Fatal(err)
	}
	dispatch := prepareDispatch(t, root, pkg, slice.RunID, "requirements-clarification")
	slice, err = RecordAction(root, pkg, slice.RunID, "requirements-clarification", dispatch, "PASS", "", nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	slice, err = UpdateRequirement(root, pkg, slice.RunID, "", true, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	slice, err = RecordSlicing(root, pkg, slice.RunID, "split", 2, master.Slicing.Slices, master.Slicing.Parallel, "", master.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if slice.RequirementGuarantee == nil || !strings.Contains(slice.RequirementGuarantee.ActivationSource, master.RunID) {
		t.Fatalf("slice did not inherit the master's frozen projection: %#v", slice.RequirementGuarantee)
	}
	slice = setRoute(t, root, pkg, slice, "custom", []string{blackboxQAID})
	slice = recordGuaranteeDesignAndReview(t, root, pkg, slice, "", []QACaseInput{{Mode: "blackbox", Description: "slice behavior", Procedure: "run slice command", Oracle: "slice passes", AcceptanceCriteria: []string{"AC-001"}, AdditionalAcceptanceCriteria: []string{"AC-009"}}})
	slice = advanceGuaranteeSnapshot(t, root, pkg, slice, "guarantee-slice")
	sliceExec := prepareDispatch(t, root, pkg, slice.RunID, "qa-execution")
	slice, err = RecordQAExecution(root, pkg, slice.RunID, sliceExec, passingExecution(slice.allQACases()), "")
	if err != nil {
		t.Fatal(err)
	}
	if slice.RequirementGuarantee.Report.Status != "pass" || len(slice.RequirementGuarantee.Report.Items) != 1 || slice.RequirementGuarantee.Report.Items[0].AcceptanceID != "AC-001" {
		t.Fatalf("slice reported beyond its responsibility: %#v", slice.RequirementGuarantee.Report)
	}

	master, err = AdvanceSnapshot(root, pkg, master.RunID, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareAction(root, pkg, master.RunID, "qa-execution", "", false, ""); err == nil || !strings.Contains(err.Error(), "sealed guarantee evidence") {
		t.Fatalf("master execution did not require immutable slice evidence: %v", err)
	}
	if _, err := Seal(root, pkg, slice.RunID, nil, false, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(sliceGuaranteePath(root, master.RunID, slice.RunID)); err != nil {
		t.Fatalf("slice seal did not leave immutable guarantee evidence: %v", err)
	}
	finalCases, err := masterFinalGuaranteeCases(root, master)
	if err != nil {
		t.Fatal(err)
	}
	if len(finalCases) != 2 || !strings.Contains(finalCases[0].ID+finalCases[1].ID, "guarantee-slice::") {
		t.Fatalf("master final whitelist did not aggregate and qualify cases: %#v", finalCases)
	}
	masterExec := prepareDispatch(t, root, pkg, master.RunID, "qa-execution")
	master, err = RecordQAExecution(root, pkg, master.RunID, masterExec, passingExecution(finalCases), "")
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range master.qaExecution("").Cases {
		if record.Origin != "executed" {
			t.Fatalf("master final FULL execution inherited a slice result: %#v", record)
		}
	}
	gate := prepareAndClaim(t, root, pkg, master.RunID, mergeGateID, "guarantee-merge-gate")
	master, err = RecordGate(root, pkg, master.RunID, mergeGateID, gate, "PASS", "", comparedRange(master), nil)
	if err != nil {
		t.Fatal(err)
	}
	if master.RequirementGuarantee.Report.Status != "pass" {
		t.Fatalf("retained master aggregate did not pass: %#v", master.RequirementGuarantee.Report)
	}
	summary, err := Seal(root, pkg, master.RunID, nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if summary.RequirementGuarantee.Report.Status != "pass" || len(summary.RequirementGuarantee.Report.Items) != 2 {
		t.Fatalf("master seal omitted complete aggregate: %#v", summary.RequirementGuarantee)
	}
	materializedPath := filepath.Join(root, ".gates", "results", master.RunID+".blackbox-cases.md")
	materialized, err := os.ReadFile(materializedPath)
	if err != nil {
		t.Fatalf("master seal did not materialize the final blackbox definitions: %v", err)
	}
	for _, want := range []string{"merge interaction", "guarantee-slice::BLACKBOX::CASE-001", "slice behavior"} {
		if !strings.Contains(string(materialized), want) {
			t.Fatalf("master blackbox materialization omitted %q:\n%s", want, materialized)
		}
	}
}

func TestRequirementGuaranteeSplitMasterAllowsEmptyMergeCaseSet(t *testing.T) {
	root, pkg, master := readyGuaranteeRoute(t, "empty-merge-master", true)
	master, err := RecordSlicing(root, pkg, master.RunID, "split", 2, []string{"slice-one", "slice-two"}, "parallel", "", "", SlicingAmendOptions{ACResponsibilities: []string{"AC-001=slice-one", "AC-009=slice-two"}})
	if err != nil {
		t.Fatal(err)
	}
	master = recordGuaranteeDesignAndReview(t, root, pkg, master, "", nil)
	if master.qaDesign("").Status != "PASS" || master.qaReview("").Status != "PASS" {
		t.Fatalf("empty merge interaction set did not preserve the existing traced PASS behavior: design=%#v review=%#v", master.qaDesign(""), master.qaReview(""))
	}
}

func TestRequirementGuaranteeRetainedMasterAcceptsCompleteTwoSliceFinalExecution(t *testing.T) {
	root, pkg, master := readyGuaranteeRoute(t, "two-slice-master", true)
	master, err := RecordSlicing(root, pkg, master.RunID, "split", 2, []string{"slice-a", "slice-b"}, "parallel", "", "", SlicingAmendOptions{ACResponsibilities: []string{"AC-001=slice-a", "AC-009=slice-b"}})
	if err != nil {
		t.Fatal(err)
	}
	master = recordGuaranteeDesignAndReview(t, root, pkg, master, "", nil)

	for index, owner := range master.Slicing.Slices {
		acID := []string{"AC-001", "AC-009"}[index]
		guarantee := *master.RequirementGuarantee
		guarantee.ReviewsByMode = map[string]GuaranteeReviewRecord{}
		sliceBinding := *master.Slicing
		sliceBinding.MasterRunID = master.RunID
		testCase := QACase{ID: "CASE-001", Mode: blackboxQAID, Description: owner + " behavior", Procedure: "run slice", Oracle: "slice passes", AcceptanceCriteria: []string{acID}, ReviewStatus: "PASS"}
		slice := RunState{
			RunID:                owner,
			SplitMasterRunID:     master.RunID,
			RequirementRevision:  master.RequirementRevision,
			CurrentSnapshot:      master.CurrentSnapshot,
			RouteMode:            "custom",
			Slicing:              &sliceBinding,
			QACasesByMode:        map[string][]QACase{blackboxQAID: {testCase}},
			RequirementGuarantee: &guarantee,
		}
		decisions, options := guaranteeReviewInputs(slice, blackboxQAID)
		if len(decisions) != 0 {
			t.Fatalf("pre-approved slice fixture unexpectedly required case decisions: %#v", decisions)
		}
		if err := recordGuaranteeReview(&slice, blackboxQAID, []QACase{testCase}, options); err != nil {
			t.Fatalf("record %s review: %v", owner, err)
		}
		if err := saveSliceGuaranteeRecord(root, slice); err != nil {
			t.Fatal(err)
		}
	}

	writeTestFile(t, filepath.Join(root, "two-slice-merged-candidate.txt"), "merged candidate\n")
	commitAll(t, root, "two-slice merged candidate")
	master, err = AdvanceSnapshot(root, pkg, master.RunID, "", false, "")
	if err != nil {
		t.Fatal(err)
	}
	finalCases, err := masterFinalGuaranteeCases(root, master)
	if err != nil {
		t.Fatal(err)
	}
	if len(finalCases) != 2 {
		t.Fatalf("qualified final case count = %d, want 2: %#v", len(finalCases), finalCases)
	}
	// Reproduce the public candidate that carried a concrete mode on the final
	// retained-master execution dispatch. Its local merge design/review remains
	// authoritative under the merged key; only the transition aliases are set.
	master.QADesignByMode[blackboxQAID] = ActionResult{Status: "PASS"}
	master.QAReviewByMode[blackboxQAID] = ActionResult{Status: "PASS"}
	if err := SaveRunState(root, master); err != nil {
		t.Fatal(err)
	}
	execution := prepareDispatch(t, root, pkg, master.RunID, "qa-execution", blackboxQAID)
	master, err = RecordQAExecution(root, pkg, master.RunID, execution, passingExecution(finalCases), "")
	if err != nil {
		t.Fatalf("complete retained-master FULL execution was rejected: %v", err)
	}
	if master.RequirementGuarantee.Report.Status != "pass" || master.qaExecution("").Status != "PASS" {
		t.Fatalf("complete retained-master execution did not close the guarantee: QA=%#v guarantee=%#v", master.qaExecution(""), master.RequirementGuarantee.Report)
	}
}

func TestRequirementGuaranteeSplitMasterReportsNoQASliceItems(t *testing.T) {
	root, pkg, master := readyGuaranteeRoute(t, "not-guaranteed-master", true)
	master, err := RecordSlicing(root, pkg, master.RunID, "split", 2, []string{"no-qa-slice-a", "no-qa-slice-b"}, "parallel", "", "", SlicingAmendOptions{ACResponsibilities: []string{"AC-001=no-qa-slice-a", "AC-009=no-qa-slice-b"}})
	if err != nil {
		t.Fatal(err)
	}
	for _, runID := range master.Slicing.Slices {
		slice := RunState{
			RunID:               runID,
			SplitMasterRunID:    master.RunID,
			RequirementRevision: master.RequirementRevision,
			CurrentSnapshot:     master.CurrentSnapshot,
			RouteMode:           "custom",
			QACasesByMode:       map[string][]QACase{},
			RequirementGuarantee: &RequirementGuarantee{
				Activation:          guaranteeNotGuaranteed,
				RequirementRevision: master.RequirementRevision,
				ManifestDigest:      master.RequirementGuarantee.ManifestDigest,
				ReviewsByMode:       map[string]GuaranteeReviewRecord{},
			},
		}
		if err := saveSliceGuaranteeRecord(root, slice); err != nil {
			t.Fatal(err)
		}
	}
	refreshRequirementGuarantee(root, &master)
	assertItemizedGuaranteeReport(t, master.RequirementGuarantee.Report, guaranteeNotGuaranteed, "INCOMPLETE")
	if master.RequirementGuarantee.Report.Items[0].Owner != "no-qa-slice-a" || master.RequirementGuarantee.Report.Items[1].Owner != "no-qa-slice-b" {
		t.Fatalf("master report lost AC responsibility owners: %#v", master.RequirementGuarantee.Report.Items)
	}
}
