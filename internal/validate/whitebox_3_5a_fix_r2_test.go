package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
)

const whitebox35AFixR2TestFile = "internal/validate/whitebox_3_5a_fix_r2_test.go"

func whitebox35AFixR2RequirementDocument(requirementCount int) string {
	var out strings.Builder
	out.WriteString("# Formal requirement\n\nNatural-language background remains unrestricted.\n\n## 需求点\n")
	for index := 0; index < requirementCount; index++ {
		reqNumber := 1 + index*7
		acNumber := 1 + index*11
		fmt.Fprintf(&out, "\n### REQ-%03d：Requirement %d\n\n#### 要求\n\nMandatory behavior %d.\n\n#### 验收条件\n\n- AC-%03d：Observable result %d.\n\n#### 来源\n\nConfirmed input %d.\n", reqNumber, index+1, index+1, acNumber, index+1, index+1)
	}
	out.WriteString("\n## 方案\n\nNatural-language solution notes.\n")
	return out.String()
}

func whitebox35AFixR2Projection() *FrozenRequirementProjection {
	return &FrozenRequirementProjection{
		Path:          "requirements.md",
		ContentDigest: "content-digest",
		Requirements: []FrozenRequirement{
			{
				ID:          "REQ-001",
				Title:       "First requirement",
				Requirement: "First mandatory behavior",
				Source:      "confirmed input",
				AcceptanceConditions: []FrozenAcceptanceCondition{
					{ID: "AC-001", Text: "first observable condition", Line: 10},
					{ID: "AC-002", Text: "second observable condition", Line: 11},
				},
			},
			{
				ID:          "REQ-008",
				Title:       "Second requirement",
				Requirement: "Second mandatory behavior",
				Source:      "confirmed input",
				AcceptanceConditions: []FrozenAcceptanceCondition{
					{ID: "AC-011", Text: "third observable condition", Line: 20},
				},
			},
		},
	}
}

func whitebox35AFixR2GuaranteedState(t *testing.T) RunState {
	t.Helper()
	projection := whitebox35AFixR2Projection()
	digest, err := projectionDigest(projection)
	if err != nil {
		t.Fatalf("projection digest: %v", err)
	}
	state := NewRunState(
		"whitebox-run",
		formalFlow,
		projection.Path,
		"requirement-revision",
		"git",
		"base-snapshot",
		"current-snapshot",
		"base-prompt-revision",
		"catalog-revision",
		true,
		[]string{"quality-gate"},
		[]RequirementArtifact{{Path: projection.Path, Revision: "requirement-revision"}},
	)
	state.RouteMode = "full"
	state.SelectedGates = []string{blackboxQAID, whiteboxQAID, "quality-gate"}
	state.RequirementGuarantee = &RequirementGuarantee{
		Activation:          guaranteeActive,
		ActivationSource:    "EXPLICIT_REQUIREMENT_CONFIRMATION",
		RequirementRevision: state.RequirementRevision,
		SolutionRevision:    state.RequirementRevision,
		ManifestDigest:      digest,
		Projection:          projection,
		ReviewsByMode:       map[string]GuaranteeReviewRecord{},
	}
	return state
}

func whitebox35AFixR2DecisionOptions(t *testing.T, state RunState, mode string, cases []QACase) QAReviewRecordOptions {
	t.Helper()
	options := QAReviewRecordOptions{}
	for _, testCase := range cases {
		options.CaseDecisions = append(options.CaseDecisions, testCase.ID+"=PASS")
	}
	merge := state.RetainedOverall && state.Slicing != nil && state.Slicing.Decision == "split"
	sources, points := guaranteeReviewDecisionIDs(state, coverageKind(mode, merge), cases)
	for _, source := range sources {
		options.SourceDecisions = append(options.SourceDecisions, source+"=PASS")
	}
	for _, pointID := range points {
		parts := strings.SplitN(pointID, "::", 2)
		if len(parts) != 2 {
			t.Fatalf("unexpected point id %q", pointID)
		}
		options.PointDecisions = append(options.PointDecisions, parts[1]+"=PASS")
	}
	return options
}

func whitebox35AFixR2ApproveMode(t *testing.T, state *RunState, mode string) {
	t.Helper()
	cases := state.QACasesByMode[mode]
	options := whitebox35AFixR2DecisionOptions(t, *state, mode, cases)
	if err := recordGuaranteeReview(state, mode, cases, options); err != nil {
		t.Fatalf("record complete guarantee review for %s: %v", mode, err)
	}
}

func whitebox35AFixR2CompleteGuaranteeState(t *testing.T) RunState {
	t.Helper()
	state := whitebox35AFixR2GuaranteedState(t)
	state.QACasesByMode = map[string][]QACase{
		blackboxQAID: {
			{ID: "CASE-001", Mode: "blackbox", Description: "exercise public behavior", Procedure: "invoke the public entry", Oracle: "the first and third conditions are observed", AcceptanceCriteria: []string{"AC-001", "AC-011"}, ReviewStatus: "PASS"},
		},
		whiteboxQAID: {
			{ID: "CASE-002", Mode: "whitebox", Description: "exercise the direct structure", Procedure: "run the bound structure test", Oracle: "the second condition is enforced", Test: whitebox35AFixR2TestFile + "::TestWhitebox35AFixR2GuaranteePassRequiresUnionCoverageAndCurrentFullExecutedPass", AcceptanceCriteria: []string{"AC-002"}, ReviewStatus: "PASS"},
		},
	}
	whitebox35AFixR2ApproveMode(t, &state, blackboxQAID)
	whitebox35AFixR2ApproveMode(t, &state, whiteboxQAID)
	state.QAExecutionByMode = map[string]QAExecutionResult{
		blackboxQAID: {
			Status:   "PASS",
			Snapshot: state.CurrentSnapshot,
			Cases: []QAResultRecord{
				{CaseID: "CASE-001", Mode: "blackbox", Outcome: "PASS", Procedure: "executed public entry", Observation: "conditions observed", OracleResult: "matched", Origin: "executed"},
			},
		},
		whiteboxQAID: {
			Status:   "PASS",
			Snapshot: state.CurrentSnapshot,
			Cases: []QAResultRecord{
				{CaseID: "CASE-002", Mode: "whitebox", Outcome: "PASS", Procedure: "executed bound test", Observation: "condition enforced", OracleResult: "matched", Origin: "executed"},
			},
		},
	}
	return state
}

func whitebox35AFixR2RecordCandidate(t *testing.T, state *RunState, action, dispatchID, status string, findings []Finding, authorizationID string) SemanticReviewCandidate {
	t.Helper()
	dispatch := PreparedDispatch{ID: dispatchID, Target: action, RequirementRevision: state.RequirementRevision, ReviewAuthorizationID: authorizationID}
	projected, err := recordSemanticReviewCandidate(state, action, dispatch, ActionResult{Status: status, Message: dispatchID + " result", Findings: findings})
	if err != nil {
		t.Fatalf("record semantic review candidate %s: %v", dispatchID, err)
	}
	state.Actions[action] = projected
	series := state.PreDevelopmentReviewSeries[action]
	return series.RawCandidates[len(series.RawCandidates)-1]
}

func TestWhitebox35AFixR2RequirementProjectionAcceptsNaturalProseFencesAndExactFiniteSet(t *testing.T) {
	document := strings.Join([]string{
		"# Requirement proposal",
		"",
		"Natural background with ordinary Markdown.",
		"",
		"```markdown",
		"## 需求点",
		"### REQ-999：fenced example",
		"#### 验收条件",
		"- AC-999：fenced example",
		"```",
		"",
		"## 需求点",
		"",
		"### REQ-001：First behavior",
		"",
		"#### 要求",
		"",
		"First paragraph.",
		"Second paragraph.",
		"",
		"#### 验收条件",
		"",
		"- AC-001：First observable result.",
		"- AC-010：Second observable result.",
		"",
		"#### 来源",
		"",
		"Confirmed user input.",
		"",
		"### REQ-120：Second behavior",
		"",
		"#### 要求",
		"",
		"Another behavior.",
		"",
		"#### 验收条件",
		"",
		"- AC-200：Another observable result.",
		"",
		"#### 来源",
		"",
		"Another confirmed source.",
		"",
		"## 方案与风险",
		"",
		"Free-form solution prose.",
		"",
	}, "\r\n")

	projection, err := ParseRequirementProjection(filepath.Join("plans", "requirement.md"), []byte(document))
	if err != nil {
		t.Fatalf("valid requirement rejected: %v", err)
	}
	if projection.Path != "plans/requirement.md" {
		t.Fatalf("projection path = %q", projection.Path)
	}
	wantRequirements := []FrozenRequirement{
		{
			ID:          "REQ-001",
			Title:       "First behavior",
			Requirement: "First paragraph.\nSecond paragraph.",
			Source:      "Confirmed user input.",
			AcceptanceConditions: []FrozenAcceptanceCondition{
				{ID: "AC-001", Text: "First observable result.", Line: 23},
				{ID: "AC-010", Text: "Second observable result.", Line: 24},
			},
		},
		{
			ID:          "REQ-120",
			Title:       "Second behavior",
			Requirement: "Another behavior.",
			Source:      "Another confirmed source.",
			AcceptanceConditions: []FrozenAcceptanceCondition{
				{ID: "AC-200", Text: "Another observable result.", Line: 38},
			},
		},
	}
	if !reflect.DeepEqual(projection.Requirements, wantRequirements) {
		t.Fatalf("requirement projection mismatch:\ngot:  %#v\nwant: %#v", projection.Requirements, wantRequirements)
	}
	if strings.Contains(fmt.Sprintf("%#v", projection), "REQ-999") || strings.Contains(fmt.Sprintf("%#v", projection), "AC-999") {
		t.Fatalf("fenced examples entered the projection: %#v", projection)
	}
	normalized := strings.ReplaceAll(document, "\r\n", "\n")
	sum := sha256.Sum256([]byte(normalized))
	if projection.ContentDigest != hex.EncodeToString(sum[:]) {
		t.Fatalf("content digest = %s, want normalized document digest", projection.ContentDigest)
	}
}

func TestWhitebox35AFixR2RequirementProjectionAcceptsSingleAndManyNonContiguousIDs(t *testing.T) {
	for _, count := range []int{1, 128} {
		t.Run(fmt.Sprintf("count-%d", count), func(t *testing.T) {
			projection, err := ParseRequirementProjection("requirement.md", []byte(whitebox35AFixR2RequirementDocument(count)))
			if err != nil {
				t.Fatalf("valid finite requirement set rejected: %v", err)
			}
			if len(projection.Requirements) != count {
				t.Fatalf("requirement count = %d, want %d", len(projection.Requirements), count)
			}
			for index, requirement := range projection.Requirements {
				wantREQ := fmt.Sprintf("REQ-%03d", 1+index*7)
				wantAC := fmt.Sprintf("AC-%03d", 1+index*11)
				if requirement.ID != wantREQ || len(requirement.AcceptanceConditions) != 1 || requirement.AcceptanceConditions[0].ID != wantAC {
					t.Fatalf("projection[%d] = %#v, want %s/%s", index, requirement, wantREQ, wantAC)
				}
			}
		})
	}
}

func TestWhitebox35AFixR2RequirementProjectionRejectsSectionAndBoundaryViolations(t *testing.T) {
	cases := []struct {
		name     string
		document string
		want     string
	}{
		{name: "missing section", document: "# only prose\n", want: "exactly one ## 需求点"},
		{name: "empty section", document: "## 需求点\n\n## 方案\n", want: "must contain at least one valid direct REQ heading"},
		{name: "multiple sections", document: whitebox35AFixR2RequirementDocument(1) + "\n## 需求点\n", want: "exactly one ## 需求点"},
		{name: "invalid direct heading", document: "## 需求点\n### Feature\n", want: "every direct ### heading"},
		{name: "REQ outside section", document: "### REQ-001：Outside\n\n## 需求点\n### REQ-002：Inside\n#### 要求\ntext\n#### 验收条件\n- AC-002：ok\n#### 来源\nsource\n", want: "REQ heading appears outside"},
		{name: "AC outside field", document: "## 需求点\n### REQ-001：Title\n#### 要求\ntext\n- AC-001：wrong field\n#### 验收条件\n- AC-002：ok\n#### 来源\nsource\n", want: "AC list item appears outside"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, err := ParseRequirementProjection("requirements.md", []byte(test.document))
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want diagnostic containing %q", err, test.want)
			}
			var precheck *RequirementPrecheckError
			if !errors.As(err, &precheck) || len(precheck.Issues) == 0 {
				t.Fatalf("error is not a structured precheck error: %T %v", err, err)
			}
			for _, issue := range precheck.Issues {
				if issue.Path != "requirements.md" || issue.Line < 1 || strings.TrimSpace(issue.Rule) == "" {
					t.Fatalf("incomplete issue: %#v", issue)
				}
			}
		})
	}
	_, err := ParseRequirementProjection("invalid-utf8.md", []byte{0xff, 0xfe})
	if err == nil {
		t.Fatal("invalid UTF-8 document unexpectedly passed")
	}
	var precheck *RequirementPrecheckError
	if !errors.As(err, &precheck) {
		t.Fatalf("invalid UTF-8 error is not structured: %T %v", err, err)
	}
	wantIssues := []RequirementPrecheckIssue{{Path: "invalid-utf8.md", Line: 1, Rule: "document is not valid UTF-8 Markdown"}}
	if !reflect.DeepEqual(precheck.Issues, wantIssues) {
		t.Fatalf("invalid UTF-8 issues = %#v, want %#v", precheck.Issues, wantIssues)
	}
}

func TestWhitebox35AFixR2RequirementProjectionRejectsDuplicateIDsAndMalformedFieldsPrecisely(t *testing.T) {
	document := strings.Join([]string{
		"## 需求点",
		"### REQ-001：First",
		"#### 验收条件",
		"- AC-001：first",
		"- malformed acceptance item",
		"#### 要求",
		"",
		"#### 要求",
		"duplicate field",
		"#### 来源",
		"source",
		"#### Extra",
		"### REQ-001：Duplicate",
		"#### 要求",
		"requirement",
		"#### 验收条件",
		"- AC-001：duplicate",
		"#### 来源",
		"",
	}, "\n")
	_, err := ParseRequirementProjection("requirements.md", []byte(document))
	if err == nil {
		t.Fatal("malformed requirement unexpectedly passed")
	}
	wantRules := []string{
		"direct #### fields must appear exactly",
		"every direct list item in 验收条件 must match",
		"duplicate direct #### 要求 field",
		"REQ blocks may contain only direct ####",
		"duplicate REQ ID",
		"duplicate AC ID AC-001",
		"来源 field must contain non-empty prose",
	}
	var precheck *RequirementPrecheckError
	if !errors.As(err, &precheck) {
		t.Fatalf("error is not RequirementPrecheckError: %T", err)
	}
	if len(precheck.Issues) != len(wantRules) {
		t.Fatalf("issue count = %d, want %d: %#v", len(precheck.Issues), len(wantRules), precheck.Issues)
	}
	for _, rule := range wantRules {
		matched := false
		for _, issue := range precheck.Issues {
			if strings.Contains(issue.Rule, rule) {
				matched = true
				if issue.Requirement != "REQ-001" {
					t.Fatalf("issue %q bound to %q, want REQ-001: %#v", rule, issue.Requirement, issue)
				}
				if issue.Path != "requirements.md" || issue.Line < 1 {
					t.Fatalf("issue %q lacks exact path/positive line: %#v", rule, issue)
				}
			}
		}
		if !matched {
			t.Errorf("structured issues missing rule containing %q: %#v", rule, precheck.Issues)
		}
	}
}

func TestWhitebox35AFixR2GuaranteeActivationFreezesExactRevisionAndRejectsDriftOrInvalidDocument(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "requirement.md")
	valid := []byte(whitebox35AFixR2RequirementDocument(2))
	if err := os.WriteFile(path, valid, 0o600); err != nil {
		t.Fatal(err)
	}
	revision, err := RequirementRevision(path)
	if err != nil {
		t.Fatal(err)
	}
	newState := func() RunState {
		return NewRunState("activation-run", formalFlow, "requirement.md", revision, "git", "base", "current", "prompt", "catalog", true, nil, []RequirementArtifact{{Path: "requirement.md", Revision: revision}})
	}

	state := newState()
	if err := activateFrozenGuarantee(root, &state); err != nil {
		t.Fatalf("activate guarantee: %v", err)
	}
	if state.RequirementGuarantee == nil || state.RequirementGuarantee.Activation != guaranteeFrozen || state.RequirementGuarantee.ActivationSource != "EXPLICIT_REQUIREMENT_CONFIRMATION" {
		t.Fatalf("unexpected activation envelope: %#v", state.RequirementGuarantee)
	}
	if state.RequirementGuarantee.RequirementRevision != revision || state.RequirementGuarantee.SolutionRevision != revision || len(state.RequirementGuarantee.Projection.Requirements) != 2 {
		t.Fatalf("activation did not freeze the exact revision/projection: %#v", state.RequirementGuarantee)
	}
	if err := validateGuaranteeEnvelope(state); err != nil {
		t.Fatalf("fresh activation envelope invalid: %v", err)
	}

	if err := os.WriteFile(path, append(valid, []byte("\nchanged\n")...), 0o600); err != nil {
		t.Fatal(err)
	}
	drifted := newState()
	if err := activateFrozenGuarantee(root, &drifted); err == nil || !strings.Contains(err.Error(), "does not match the current file digest") {
		t.Fatalf("drift activation error = %v", err)
	}
	if drifted.RequirementGuarantee != nil {
		t.Fatal("failed drift activation partially wrote the envelope")
	}

	if err := os.WriteFile(path, []byte("# no structured section\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	invalid := newState()
	invalid.RequirementRevision, err = RequirementRevision(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := activateFrozenGuarantee(root, &invalid); err == nil {
		t.Fatal("invalid structure activated a guarantee")
	} else {
		var precheck *RequirementPrecheckError
		if !errors.As(err, &precheck) {
			t.Fatalf("invalid structure returned %T, want RequirementPrecheckError", err)
		}
	}
	if invalid.RequirementGuarantee != nil {
		t.Fatalf("invalid structure partially activated guarantee: %#v", invalid.RequirementGuarantee)
	}
	lightweight := newState()
	lightweight.RouteMode = "lightweight"
	if err := activateFrozenGuarantee(root, &lightweight); err == nil || !strings.Contains(err.Error(), "lightweight route") {
		t.Fatalf("lightweight activation error = %v", err)
	}
	if lightweight.RequirementGuarantee != nil {
		t.Fatalf("lightweight rejection partially activated guarantee: %#v", lightweight.RequirementGuarantee)
	}
}

func TestWhitebox35AFixR2GuaranteeRouteStateIsExplicitAndArtifactCardinalityBound(t *testing.T) {
	state := whitebox35AFixR2GuaranteedState(t)
	state.RequirementGuarantee.Activation = guaranteeFrozen
	updateGuaranteeForRoute(&state)
	if state.RequirementGuarantee.Activation != guaranteeActive || state.RequirementGuarantee.Reason != "" {
		t.Fatalf("QA route activation = %#v", state.RequirementGuarantee)
	}

	withoutQA := whitebox35AFixR2GuaranteedState(t)
	withoutQA.RequirementGuarantee.Activation = guaranteeFrozen
	withoutQA.RouteMode = "custom"
	withoutQA.SelectedGates = []string{"quality-gate"}
	updateGuaranteeForRoute(&withoutQA)
	if withoutQA.RequirementGuarantee.Activation != guaranteeNotGuaranteed || !strings.Contains(withoutQA.RequirementGuarantee.Reason, "selected no QA") {
		t.Fatalf("custom-without-QA state = %#v", withoutQA.RequirementGuarantee)
	}

	extraArtifact := whitebox35AFixR2GuaranteedState(t)
	extraArtifact.RequirementGuarantee.Activation = guaranteeFrozen
	extraArtifact.RequirementArtifacts = append(extraArtifact.RequirementArtifacts, RequirementArtifact{Path: "second.md", Revision: "second"})
	updateGuaranteeForRoute(&extraArtifact)
	if extraArtifact.RequirementGuarantee.Activation != guaranteeBlocked || !strings.Contains(extraArtifact.RequirementGuarantee.Reason, "exactly one") {
		t.Fatalf("multi-artifact state = %#v", extraArtifact.RequirementGuarantee)
	}

	legacy := NewRunState("legacy", formalFlow, "free-form.md", "rev", "git", "base", "current", "prompt", "catalog", true, nil, []RequirementArtifact{{Path: "free-form.md", Revision: "rev"}})
	legacy.RouteMode = "full"
	legacy.SelectedGates = []string{blackboxQAID, whiteboxQAID}
	updateGuaranteeForRoute(&legacy)
	if legacy.RequirementGuarantee != nil {
		t.Fatal("route/artifact/document shape guessed an activation envelope")
	}
}

func TestWhitebox35AFixR2RequirementRevisionChangeInvalidatesAllGuaranteeBindingsButPreservesReviewLedger(t *testing.T) {
	state := whitebox35AFixR2CompleteGuaranteeState(t)
	whitebox35AFixR2RecordCandidate(t, &state, "product-review", "review-before-revision-change", "PASS", nil, "")
	state.QACasesByMode[whiteboxQAID][0].ApprovedSource = "prior approval"
	state.QAReviewByMode = map[string]ActionResult{blackboxQAID: {Status: "PASS"}, whiteboxQAID: {Status: "PASS"}}
	state.PriorQAExecutionByMode = map[string]*QAExecutionResult{whiteboxQAID: {Status: "PASS", Snapshot: "older-snapshot"}}
	state.RequirementGuarantee.Waiver = &RequirementGuaranteeWaiver{Origin: "SEAL-USER", Reason: "old waiver", Snapshot: state.CurrentSnapshot, Unresolved: []string{"AC-002"}}
	seriesBefore := state.PreDevelopmentReviewSeries["product-review"]

	state.RequirementRevision = "new-requirement-revision"
	invalidateGuaranteeRevision(&state)
	guarantee := state.RequirementGuarantee
	if state.RequirementConfirmed || guarantee.Activation != guaranteeFrozen || guarantee.RequirementRevision != state.RequirementRevision || guarantee.SolutionRevision != state.RequirementRevision || guarantee.ManifestDigest != "" || guarantee.Projection != nil || guarantee.Waiver != nil || len(guarantee.ReviewsByMode) != 0 {
		t.Fatalf("revision invalidation envelope = %#v confirmed=%v", guarantee, state.RequirementConfirmed)
	}
	for mode, cases := range state.QACasesByMode {
		for _, testCase := range cases {
			if testCase.ReviewStatus != "PENDING" || testCase.ApprovedSource != "" {
				t.Errorf("%s case retained stale approval: %#v", mode, testCase)
			}
		}
	}
	if len(state.QAReviewByMode) != 0 || len(state.QAExecutionByMode) != 0 || len(state.PriorQAExecutionByMode) != 0 {
		t.Fatalf("revision invalidation retained QA authority: review=%#v execution=%#v prior=%#v", state.QAReviewByMode, state.QAExecutionByMode, state.PriorQAExecutionByMode)
	}
	if !reflect.DeepEqual(state.PreDevelopmentReviewSeries["product-review"], seriesBefore) {
		t.Fatal("requirement guarantee invalidation reset the independent semantic review ledger")
	}
}

func TestWhitebox35AFixR2SplitRequirementRevisionInvalidationDiscardsACResponsibilities(t *testing.T) {
	state := whitebox35AFixR2CompleteGuaranteeState(t)
	state.RetainedOverall = true
	state.Slicing = &Slicing{
		Decision:   "split",
		SplitCount: 2,
		Slices:     []string{"slice-a", "slice-b"},
		Parallel:   "parallel",
		ACResponsibilities: map[string]string{
			"AC-001": "slice-a",
			"AC-002": "slice-b",
			"AC-011": masterMergeOwner,
		},
	}
	oldResponsibilities := copyStringMap(state.Slicing.ACResponsibilities)

	state.RequirementRevision = "new-split-requirement-revision"
	invalidateGuaranteeRevision(&state)

	if state.Slicing != nil {
		t.Fatalf("revision invalidation retained the old split binding point: %#v", state.Slicing)
	}
	if len(oldResponsibilities) != 3 || oldResponsibilities["AC-001"] != "slice-a" || oldResponsibilities["AC-002"] != "slice-b" || oldResponsibilities["AC-011"] != masterMergeOwner {
		t.Fatalf("test fixture did not establish the old finite AC responsibility map: %#v", oldResponsibilities)
	}
	if state.RequirementGuarantee.Projection != nil || state.RequirementGuarantee.ManifestDigest != "" || state.RequirementGuarantee.RequirementRevision != state.RequirementRevision {
		t.Fatalf("split invalidation did not bind the frozen envelope to reconfirmation of the new revision: %#v", state.RequirementGuarantee)
	}
}

func TestWhitebox35AFixR2ProductReviewRequiresOneObligationDecisionPerREQ(t *testing.T) {
	state := whitebox35AFixR2GuaranteedState(t)
	state.ReviewItemsByAction["product-review"] = map[string]ReviewItem{"existing context": {Status: "PASS"}}
	ensureGuaranteeProductReviewItems(&state)
	table := state.ReviewItemsByAction["product-review"]
	if len(table) != 3 || table["existing context"].Status != "PASS" {
		t.Fatalf("product-review item table = %#v", table)
	}
	for _, reqID := range []string{"REQ-001", "REQ-008"} {
		key := guaranteeProductReviewKey(reqID)
		if table[key].Status != "PENDING" {
			t.Fatalf("%s item = %#v, want PENDING", reqID, table[key])
		}
	}
	if err := requireGuaranteeProductReviewResult(state, "PASS"); err == nil || !strings.Contains(err.Error(), "must decide") {
		t.Fatalf("pending completeness result error = %v", err)
	}
	for key, item := range table {
		item.Status = "PASS"
		table[key] = item
	}
	if err := requireGuaranteeProductReviewResult(state, "PASS"); err != nil {
		t.Fatalf("all completeness items PASS: %v", err)
	}
	failedKey := guaranteeProductReviewKey("REQ-008")
	table[failedKey] = ReviewItem{Status: "FAIL", Message: "one obligation lacks an AC"}
	if err := requireGuaranteeProductReviewResult(state, "PASS"); err == nil || !strings.Contains(err.Error(), "cannot PASS") {
		t.Fatalf("PASS with failed obligation error = %v", err)
	}
	if err := requireGuaranteeProductReviewResult(state, "FAIL"); err != nil {
		t.Fatalf("FAIL correctly carrying the uncovered obligation was rejected: %v", err)
	}
}

func TestWhitebox35AFixR2CaseBindingsRequireKnownUniquePrimaryACAndRespectSplitOwnership(t *testing.T) {
	base := whitebox35AFixR2GuaranteedState(t)
	valid := QACase{ID: "CASE-001", Mode: "whitebox", AcceptanceCriteria: []string{"AC-001"}, AdditionalAcceptanceCriteria: []string{"AC-011"}}
	if err := validateGuaranteeCaseBindings(base, valid); err != nil {
		t.Fatalf("valid binding rejected: %v", err)
	}
	invalid := []struct {
		name string
		item QACase
		want string
	}{
		{name: "missing primary", item: QACase{ID: "CASE-002", AdditionalAcceptanceCriteria: []string{"AC-001"}}, want: "at least one explicit --ac"},
		{name: "unknown", item: QACase{ID: "CASE-003", AcceptanceCriteria: []string{"AC-999"}}, want: "unknown acceptance condition AC-999"},
		{name: "duplicate across primary and additional", item: QACase{ID: "CASE-004", AcceptanceCriteria: []string{"AC-001"}, AdditionalAcceptanceCriteria: []string{"AC-001"}}, want: "repeats acceptance condition AC-001"},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if err := validateGuaranteeCaseBindings(base, test.item); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}

	slice := whitebox35AFixR2GuaranteedState(t)
	slice.RunID = "slice-a"
	slice.SplitMasterRunID = "master"
	slice.Slicing = &Slicing{Decision: "split", ACResponsibilities: map[string]string{"AC-001": "slice-a", "AC-002": "slice-b", "AC-011": masterMergeOwner}}
	if err := validateGuaranteeCaseBindings(slice, QACase{ID: "CASE-005", AcceptanceCriteria: []string{"AC-001"}, AdditionalAcceptanceCriteria: []string{"AC-002"}}); err != nil {
		t.Fatalf("slice primary plus cross-scope additional coverage rejected: %v", err)
	}
	if err := validateGuaranteeCaseBindings(slice, QACase{ID: "CASE-006", AcceptanceCriteria: []string{"AC-002"}}); err == nil || !strings.Contains(err.Error(), "cannot claim primary responsibility") {
		t.Fatalf("foreign slice primary error = %v", err)
	}

	master := whitebox35AFixR2GuaranteedState(t)
	master.RetainedOverall = true
	master.Slicing = &Slicing{Decision: "split", ACResponsibilities: map[string]string{"AC-001": "slice-a", "AC-002": "slice-b", "AC-011": masterMergeOwner}}
	if err := validateGuaranteeCaseBindings(master, QACase{ID: "CASE-007", AcceptanceCriteria: []string{"AC-011"}, AdditionalAcceptanceCriteria: []string{"AC-001"}}); err != nil {
		t.Fatalf("master merge primary plus additional coverage rejected: %v", err)
	}
	if err := validateGuaranteeCaseBindings(master, QACase{ID: "CASE-008", AcceptanceCriteria: []string{"AC-001"}}); err == nil || !strings.Contains(err.Error(), "master merge QA cannot claim") {
		t.Fatalf("slice-owned master primary error = %v", err)
	}
}

func TestWhitebox35AFixR2ManifestProjectsREQACCaseEdgesAndWhiteboxTestReference(t *testing.T) {
	state := whitebox35AFixR2GuaranteedState(t)
	testRef := whitebox35AFixR2TestFile + "::TestWhitebox35AFixR2ManifestProjectsREQACCaseEdgesAndWhiteboxTestReference"
	cases := []QACase{{
		ID:                           "CASE-010",
		Mode:                         "whitebox",
		Description:                  "one structural responsibility",
		Procedure:                    "run the uniquely bound test",
		Oracle:                       "all three linked conditions are enforced",
		Test:                         testRef,
		AcceptanceCriteria:           []string{"AC-001", "AC-002"},
		AdditionalAcceptanceCriteria: []string{"AC-011"},
		ReviewStatus:                 "PASS",
	}}
	manifest, err := localGuaranteeManifest(state, whiteboxQAID, cases)
	if err != nil {
		t.Fatalf("manifest: %v", err)
	}
	want := coverage.AcceptanceManifest{
		Binding: coverage.ManifestBinding{
			SourceBinding: coverage.SourceBinding{
				RunID:               state.RunID,
				RequirementRevision: state.RequirementRevision,
				SolutionRevision:    state.RequirementRevision,
			},
			ReviewKind:    coverage.ReviewWhitebox,
			RouteScope:    "full",
			TopologyScope: "no-split",
		},
		Sources: []string{"REQ-001", "REQ-008"},
		Points: []coverage.AcceptancePoint{
			{PointID: "WHITEBOX::AC-001", ObservableBehavior: "first observable condition", Oracle: "all three linked conditions are enforced"},
			{PointID: "WHITEBOX::AC-002", ObservableBehavior: "second observable condition", Oracle: "all three linked conditions are enforced"},
			{PointID: "WHITEBOX::AC-011", ObservableBehavior: "third observable condition", Oracle: "all three linked conditions are enforced"},
		},
		Cases: []coverage.AcceptanceCase{{
			CaseID: "CASE-010", Mode: coverage.CaseWhitebox, Oracle: "all three linked conditions are enforced", TestRef: testRef,
		}},
		Edges: []coverage.CoverageEdge{
			{ReviewKind: coverage.ReviewWhitebox, SourceID: "REQ-001", PointID: "WHITEBOX::AC-001", CaseID: "CASE-010"},
			{ReviewKind: coverage.ReviewWhitebox, SourceID: "REQ-001", PointID: "WHITEBOX::AC-002", CaseID: "CASE-010"},
			{ReviewKind: coverage.ReviewWhitebox, SourceID: "REQ-008", PointID: "WHITEBOX::AC-011", CaseID: "CASE-010"},
		},
	}
	if !reflect.DeepEqual(manifest, want) {
		t.Fatalf("manifest projection mismatch:\ngot:  %#v\nwant: %#v", manifest, want)
	}
}

func TestWhitebox35AFixR2ReviewRequiresExactExplicitSourcePointAndCaseDecisions(t *testing.T) {
	newFixture := func() (RunState, []QACase, QAReviewRecordOptions) {
		state := whitebox35AFixR2GuaranteedState(t)
		cases := []QACase{{ID: "CASE-020", Mode: "whitebox", Description: "description", Procedure: "procedure", Oracle: "oracle", Test: whitebox35AFixR2TestFile + "::TestWhitebox35AFixR2ReviewRequiresExactExplicitSourcePointAndCaseDecisions", AcceptanceCriteria: []string{"AC-001"}, ReviewStatus: "PASS"}}
		return state, cases, whitebox35AFixR2DecisionOptions(t, state, whiteboxQAID, cases)
	}

	state, cases, options := newFixture()
	options.SourceDecisions = nil
	if err := recordGuaranteeReview(&state, whiteboxQAID, cases, options); err == nil || !strings.Contains(err.Error(), "missing explicit source decision") {
		t.Fatalf("missing source error = %v", err)
	}

	state, cases, options = newFixture()
	options.PointDecisions = append(options.PointDecisions, "AC-999=PASS")
	if err := recordGuaranteeReview(&state, whiteboxQAID, cases, options); err == nil || !strings.Contains(err.Error(), "unknown point decision") {
		t.Fatalf("unknown point error = %v", err)
	}

	state, cases, options = newFixture()
	options.CaseDecisions = []string{"CASE-020=FAIL"}
	if err := recordGuaranteeReview(&state, whiteboxQAID, cases, options); err == nil || !strings.Contains(err.Error(), "does not match its QA Review outcome") {
		t.Fatalf("case-decision mismatch error = %v", err)
	}

	state, cases, options = newFixture()
	options.UnboundPoints = []string{"AC-001"}
	if err := recordGuaranteeReview(&state, whiteboxQAID, cases, options); err != nil {
		t.Fatalf("record explicit unbound review: %v", err)
	}
	record := state.RequirementGuarantee.ReviewsByMode[whiteboxQAID]
	if record.Review.SetStatus == coverage.StatusPass || record.Whitelist != nil {
		t.Fatalf("unbound review produced approval: %#v", record)
	}

	state, cases, options = newFixture()
	if err := recordGuaranteeReview(&state, whiteboxQAID, cases, options); err != nil {
		t.Fatalf("complete explicit PASS review rejected: %v", err)
	}
	record = state.RequirementGuarantee.ReviewsByMode[whiteboxQAID]
	if record.Review.SetStatus != coverage.StatusPass || record.Review.Scope != coverage.ScopeFull || record.Whitelist == nil {
		t.Fatalf("complete review did not project a FULL whitelist: %#v", record)
	}
}

func TestWhitebox35AFixR2IncrementalReviewPromptKeepsAcceptedCaseBindings(t *testing.T) {
	state := whitebox35AFixR2GuaranteedState(t)
	acceptedTest := whitebox35AFixR2TestFile + "::TestWhitebox35AFixR2IncrementalReviewPromptKeepsAcceptedCaseBindings"
	state.QACasesByMode = map[string][]QACase{
		whiteboxQAID: {
			{ID: "CASE-020", Mode: "whitebox", Description: "accepted structure", Procedure: "run the accepted test", Oracle: "the first and third conditions pass", Test: acceptedTest, AcceptanceCriteria: []string{"AC-001"}, AdditionalAcceptanceCriteria: []string{"AC-011"}, ReviewStatus: "PASS"},
			{ID: "CASE-021", Mode: "whitebox", Description: "pending structure", Procedure: "run the pending test", Oracle: "the second condition passes", Test: whitebox35AFixR2TestFile + "::TestPendingStructure", AcceptanceCriteria: []string{"AC-002"}, ReviewStatus: "PENDING"},
		},
	}
	state.QADesignChangesByMode = map[string]QADesignChange{whiteboxQAID: {Added: []string{"CASE-021"}}}

	detail, err := actionPromptDetail("", state, PromptCatalog{}, "qa-review", whiteboxQAID)
	if err != nil {
		t.Fatal(err)
	}
	pendingAt := strings.Index(detail, "Return one decision for every pending case below:")
	if pendingAt < 0 {
		t.Fatalf("incremental review prompt omitted the pending-case section:\n%s", detail)
	}
	accepted := detail[:pendingAt]
	for _, want := range []string{
		"do not return new --case/--outcome decisions",
		"CASE-020",
		"primary AC bindings: AC-001",
		"additional AC evidence: AC-011",
		"test: " + acceptedTest,
		"review status: PASS",
	} {
		if !strings.Contains(accepted, want) {
			t.Fatalf("accepted-case context omitted %q:\n%s", want, accepted)
		}
	}
}

func TestWhitebox35AFixR2GuaranteePassRequiresUnionCoverageAndCurrentFullExecutedPass(t *testing.T) {
	state := whitebox35AFixR2CompleteGuaranteeState(t)
	report := deriveRequirementGuarantee(t.TempDir(), state)
	if report.Status != "pass" {
		t.Fatalf("guarantee status = %q: %s", report.Status, report.Reason)
	}
	if report.RequirementCount != 2 || report.AcceptanceCount != 3 || len(report.Items) != 3 {
		t.Fatalf("report counts/items = %#v", report)
	}
	wantItems := []RequirementGuaranteeItemReport{
		{RequirementID: "REQ-001", AcceptanceID: "AC-001", Owner: state.RunID, Cases: []string{"CASE-001"}, ReviewStatus: "PASS", Execution: "PASS"},
		{RequirementID: "REQ-001", AcceptanceID: "AC-002", Owner: state.RunID, Cases: []string{"CASE-002"}, ReviewStatus: "PASS", Execution: "PASS"},
		{RequirementID: "REQ-008", AcceptanceID: "AC-011", Owner: state.RunID, Cases: []string{"CASE-001"}, ReviewStatus: "PASS", Execution: "PASS"},
	}
	if !reflect.DeepEqual(report.Items, wantItems) {
		t.Fatalf("AC-to-case report mismatch:\ngot:  %#v\nwant: %#v", report.Items, wantItems)
	}
	wantRequirements := []RequirementGuaranteeRequirementReport{
		{RequirementID: "REQ-001", Owners: []string{state.RunID}, AcceptanceIDs: []string{"AC-001", "AC-002"}, Cases: []string{"CASE-001", "CASE-002"}, ReviewStatus: "PASS", Execution: "PASS", Status: "PASS"},
		{RequirementID: "REQ-008", Owners: []string{state.RunID}, AcceptanceIDs: []string{"AC-011"}, Cases: []string{"CASE-001"}, ReviewStatus: "PASS", Execution: "PASS", Status: "PASS"},
	}
	if !reflect.DeepEqual(report.Requirements, wantRequirements) {
		t.Fatalf("REQ completion projection mismatch:\ngot:  %#v\nwant: %#v", report.Requirements, wantRequirements)
	}
	if report.RequirementRevision != state.RequirementRevision || report.SolutionRevision != state.RequirementRevision || report.ManifestDigest != state.RequirementGuarantee.ManifestDigest {
		t.Fatalf("report binding mismatch: %#v", report)
	}
}

func TestWhitebox35AFixR2GuaranteeRejectsMissingCoverageReviewAndNonCurrentOrNonExecutedResults(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*RunState)
	}{
		{name: "missing AC coverage", mutate: func(state *RunState) { delete(state.QACasesByMode, whiteboxQAID) }},
		{name: "missing explicit review", mutate: func(state *RunState) { delete(state.RequirementGuarantee.ReviewsByMode, whiteboxQAID) }},
		{name: "old candidate", mutate: func(state *RunState) {
			result := state.QAExecutionByMode[whiteboxQAID]
			result.Snapshot = "old-snapshot"
			state.QAExecutionByMode[whiteboxQAID] = result
		}},
		{name: "inherited result", mutate: func(state *RunState) {
			result := state.QAExecutionByMode[whiteboxQAID]
			result.Cases[0].Origin = "inherited"
			state.QAExecutionByMode[whiteboxQAID] = result
		}},
		{name: "failed result", mutate: func(state *RunState) {
			result := state.QAExecutionByMode[whiteboxQAID]
			result.Cases[0].Outcome = "FAIL"
			state.QAExecutionByMode[whiteboxQAID] = result
		}},
		{name: "missing result", mutate: func(state *RunState) { delete(state.QAExecutionByMode, whiteboxQAID) }},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			state := whitebox35AFixR2CompleteGuaranteeState(t)
			test.mutate(&state)
			report := deriveRequirementGuarantee(t.TempDir(), state)
			if report.Status == "pass" || strings.TrimSpace(report.Reason) == "" {
				t.Fatalf("invalid closure produced %#v", report)
			}
		})
	}
}

func TestWhitebox35AFixR2ActiveGuaranteeRejectsAffectedScopeAndWaiverAuditsUnresolvedItems(t *testing.T) {
	state := whitebox35AFixR2GuaranteedState(t)
	state.QACasesByMode = map[string][]QACase{whiteboxQAID: {{ID: "CASE-030", Mode: "whitebox", AcceptanceCriteria: []string{"AC-001"}, ReviewStatus: "PASS"}}}
	prior := QAExecutionResult{Status: "FAIL", Snapshot: "prior", Cases: []QAResultRecord{{CaseID: "CASE-030", Mode: "whitebox", Outcome: "FAIL"}}}
	if err := recordExecutionScope(&state, whiteboxQAID, "AFFECTED", []string{"CASE-030"}, "targeted rerun", scopeSourcePrepare, "prior", prior); err == nil || !strings.Contains(err.Error(), "requires FULL QA execution") {
		t.Fatalf("AFFECTED scope error = %v", err)
	}
	if err := recordExecutionScope(&state, whiteboxQAID, "FULL", nil, "full rerun", scopeSourcePrepare, "prior", prior); err != nil {
		t.Fatalf("FULL scope rejected: %v", err)
	}
	if state.ExecutionScopes[whiteboxQAID].Decision != "FULL" || len(state.ExecutionScopes[whiteboxQAID].CaseIDs) != 0 {
		t.Fatalf("FULL scope record = %#v", state.ExecutionScopes[whiteboxQAID])
	}

	if err := authorizeRequirementGuaranteeWaiver(t.TempDir(), &state, ""); err == nil || !strings.Contains(err.Error(), "requires --guarantee-waiver-reason") {
		t.Fatalf("empty waiver reason error = %v", err)
	}
	if err := authorizeRequirementGuaranteeWaiver(t.TempDir(), &state, "user accepts incomplete QA closure"); err != nil {
		t.Fatalf("authorize waiver: %v", err)
	}
	wantWaiver := &RequirementGuaranteeWaiver{
		Origin:   "SEAL-USER",
		Reason:   "user accepts incomplete QA closure",
		Snapshot: state.CurrentSnapshot,
		Unresolved: []string{
			"REQ-001/AC-001 review=PENDING execution=PENDING",
			"REQ-001/AC-002 review=PENDING execution=PENDING",
			"REQ-008/AC-011 review=PENDING execution=PENDING",
		},
	}
	if state.RequirementGuarantee.Activation != guaranteeWaived || !reflect.DeepEqual(state.RequirementGuarantee.Waiver, wantWaiver) {
		t.Fatalf("waiver audit record mismatch:\ngot:  %#v\nwant: %#v", state.RequirementGuarantee.Waiver, wantWaiver)
	}
	if state.RequirementGuarantee.Report.Status != guaranteeWaived || state.RequirementGuarantee.Report.Reason != wantWaiver.Reason || state.RequirementGuarantee.Waiver.Snapshot == prior.Snapshot {
		t.Fatalf("waived report status = %#v", state.RequirementGuarantee.Report)
	}
}

func TestWhitebox35AFixR2SplitResponsibilitiesMustBeCompleteUniqueKnownAndInheritedBySlice(t *testing.T) {
	master := whitebox35AFixR2GuaranteedState(t)
	owners, err := validateACResponsibilities(master, []string{"slice-a", "slice-b"}, []string{"AC-001=slice-a", "AC-002=slice-b", "AC-011=master-merge"})
	if err != nil {
		t.Fatalf("valid responsibility map: %v", err)
	}
	want := map[string]string{"AC-001": "slice-a", "AC-002": "slice-b", "AC-011": masterMergeOwner}
	if !reflect.DeepEqual(owners, want) {
		t.Fatalf("owners = %#v, want %#v", owners, want)
	}
	invalid := []struct {
		name   string
		inputs []string
		want   string
	}{
		{name: "missing", inputs: []string{"AC-001=slice-a", "AC-002=slice-b"}, want: "AC-011 is missing"},
		{name: "duplicate", inputs: []string{"AC-001=slice-a", "AC-001=slice-b", "AC-002=slice-b", "AC-011=master-merge"}, want: "duplicate primary responsibility"},
		{name: "unknown AC", inputs: []string{"AC-001=slice-a", "AC-002=slice-b", "AC-011=master-merge", "AC-999=slice-a"}, want: "unknown acceptance condition AC-999"},
		{name: "unknown scope", inputs: []string{"AC-001=slice-a", "AC-002=slice-c", "AC-011=master-merge"}, want: "unknown slice scope slice-c"},
	}
	for _, test := range invalid {
		t.Run(test.name, func(t *testing.T) {
			if _, err := validateACResponsibilities(master, []string{"slice-a", "slice-b"}, test.inputs); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
		})
	}

	master.RunID = "master"
	master.RetainedOverall = true
	master.Slicing = &Slicing{Decision: "split", SplitCount: 2, Slices: []string{"slice-a", "slice-b"}, ACResponsibilities: owners}
	slice := NewRunState("slice-a", formalFlow, master.RequirementSource, master.RequirementRevision, "git", master.BaseSnapshot, master.CurrentSnapshot, "prompt", "catalog", true, nil, master.RequirementArtifacts)
	slice.SplitMasterRunID = master.RunID
	slice.Slicing = &Slicing{Decision: "split", MasterRunID: master.RunID, ACResponsibilities: copyStringMap(owners)}
	if err := inheritRequirementGuarantee(master, &slice); err != nil {
		t.Fatalf("inherit guarantee: %v", err)
	}
	if slice.RequirementGuarantee.Activation != guaranteeFrozen || slice.RequirementGuarantee.ActivationSource != "INHERITED_FROM_MASTER:master" || len(slice.RequirementGuarantee.ReviewsByMode) != 0 {
		t.Fatalf("inherited guarantee = %#v", slice.RequirementGuarantee)
	}
	if slice.RequirementGuarantee.RequirementRevision != master.RequirementGuarantee.RequirementRevision ||
		slice.RequirementGuarantee.SolutionRevision != master.RequirementGuarantee.SolutionRevision ||
		slice.RequirementGuarantee.ManifestDigest != master.RequirementGuarantee.ManifestDigest {
		t.Fatalf("slice inherited wrong revision/digest binding:\nslice=%#v\nmaster=%#v", slice.RequirementGuarantee, master.RequirementGuarantee)
	}
	if !reflect.DeepEqual(slice.RequirementGuarantee.Projection, master.RequirementGuarantee.Projection) {
		t.Fatalf("slice projection differs from the exact frozen master projection:\nslice=%#v\nmaster=%#v", slice.RequirementGuarantee.Projection, master.RequirementGuarantee.Projection)
	}
	targets := guaranteeTargetACs(slice)
	if !reflect.DeepEqual(targets, map[string]bool{"AC-001": true}) {
		t.Fatalf("slice targets = %#v", targets)
	}
	slice.RequirementGuarantee.Projection.Requirements[0].Title = "slice-local mutation"
	if master.RequirementGuarantee.Projection.Requirements[0].Title == "slice-local mutation" {
		t.Fatal("slice inherited the master's projection by alias instead of by value")
	}
}

func TestWhitebox35AFixR2RetainedMasterAggregatesQualifiedSliceCasesAndRequiresFinalMergedFullExecution(t *testing.T) {
	root := t.TempDir()
	owners := map[string]string{"AC-001": "slice-a", "AC-002": "slice-b", "AC-011": masterMergeOwner}
	master := whitebox35AFixR2GuaranteedState(t)
	master.RunID = "master"
	master.RetainedOverall = true
	master.RouteMode = "merge"
	master.SelectedGates = []string{mergeQAID, mergeGateID}
	master.Slicing = &Slicing{Decision: "split", SplitCount: 2, Slices: []string{"slice-a", "slice-b"}, ACResponsibilities: copyStringMap(owners)}
	masterCase := QACase{ID: "CASE-M", Mode: "blackbox", Description: "cross-slice integration", Procedure: "exercise the merged integration", Oracle: "merge-owned AC passes", AcceptanceCriteria: []string{"AC-011"}, ReviewStatus: "PASS"}
	master.QACasesByMode = map[string][]QACase{"": {masterCase}}
	whitebox35AFixR2ApproveMode(t, &master, "")

	writeSlice := func(runID, mode, caseID, acID, testRef string) {
		t.Helper()
		slice := whitebox35AFixR2GuaranteedState(t)
		slice.RunID = runID
		slice.SplitMasterRunID = master.RunID
		slice.RouteMode = "full"
		slice.SelectedGates = []string{mode}
		slice.Slicing = &Slicing{Decision: "split", MasterRunID: master.RunID, ACResponsibilities: copyStringMap(owners)}
		slice.RequirementGuarantee.ActivationSource = "INHERITED_FROM_MASTER:" + master.RunID
		testCase := QACase{ID: caseID, Mode: mode, Description: runID + " behavior", Procedure: "execute " + runID, Oracle: acID + " passes", Test: testRef, AcceptanceCriteria: []string{acID}, ReviewStatus: "PASS"}
		slice.QACasesByMode = map[string][]QACase{mode: {testCase}}
		whitebox35AFixR2ApproveMode(t, &slice, mode)
		if err := saveSliceGuaranteeRecord(root, slice); err != nil {
			t.Fatalf("save %s evidence: %v", runID, err)
		}
	}
	writeSlice("slice-a", blackboxQAID, "CASE-A", "AC-001", "")
	writeSlice("slice-b", whiteboxQAID, "CASE-B", "AC-002", whitebox35AFixR2TestFile+"::TestWhitebox35AFixR2RetainedMasterAggregatesQualifiedSliceCasesAndRequiresFinalMergedFullExecution")

	beforeExecution := deriveRequirementGuarantee(root, master)
	if beforeExecution.Status == "pass" || !strings.Contains(beforeExecution.Reason, "current") {
		t.Fatalf("slice-only evidence substituted for merged execution: %#v", beforeExecution)
	}
	approved, err := masterFinalGuaranteeCases(root, master)
	if err != nil {
		t.Fatalf("aggregate final cases: %v", err)
	}
	gotIDs := make([]string, len(approved))
	for index, testCase := range approved {
		gotIDs[index] = testCase.ID
		if testCase.ReviewStatus != "PASS" {
			t.Errorf("aggregated case is not approved: %#v", testCase)
		}
	}
	wantIDs := []string{"master::MERGE::CASE-M", "slice-a::BLACKBOX::CASE-A", "slice-b::WHITEBOX::CASE-B"}
	if !reflect.DeepEqual(gotIDs, wantIDs) {
		t.Fatalf("qualified final cases = %#v, want %#v", gotIDs, wantIDs)
	}
	result := QAExecutionResult{Status: "PASS", Snapshot: master.CurrentSnapshot}
	for _, testCase := range approved {
		result.Cases = append(result.Cases, QAResultRecord{CaseID: testCase.ID, Mode: testCase.Mode, Outcome: "PASS", Procedure: "executed on merged candidate", Observation: "condition observed", OracleResult: "matched", Origin: "executed"})
	}
	master.QAExecutionByMode = map[string]QAExecutionResult{"": result}
	report := deriveRequirementGuarantee(root, master)
	if report.Status != "pass" || report.RequirementCount != 2 || report.AcceptanceCount != 3 {
		t.Fatalf("final merged FULL execution did not close guarantee: %#v", report)
	}
	ownersSeen := map[string]string{}
	for _, item := range report.Items {
		ownersSeen[item.AcceptanceID] = item.Owner
		if item.Execution != "PASS" || item.ReviewStatus != "PASS" {
			t.Errorf("incomplete merged item: %#v", item)
		}
	}
	if !reflect.DeepEqual(ownersSeen, owners) {
		t.Fatalf("reported owners = %#v, want %#v", ownersSeen, owners)
	}
}

func TestWhitebox35AFixR2SealedSliceEvidenceIsDigestBoundAndImmutable(t *testing.T) {
	root := t.TempDir()
	state := whitebox35AFixR2GuaranteedState(t)
	state.RunID = "slice-a"
	state.SplitMasterRunID = "master"
	state.RouteMode = "full"
	state.Slicing = &Slicing{Decision: "split", MasterRunID: "master", ACResponsibilities: map[string]string{"AC-001": "slice-a", "AC-002": "slice-b", "AC-011": masterMergeOwner}}
	state.QACasesByMode = map[string][]QACase{whiteboxQAID: {{ID: "CASE-040", Mode: "whitebox", Description: "slice case", Procedure: "run test", Oracle: "pass", Test: whitebox35AFixR2TestFile + "::TestWhitebox35AFixR2SealedSliceEvidenceIsDigestBoundAndImmutable", AcceptanceCriteria: []string{"AC-001"}, ReviewStatus: "PASS"}}}
	whitebox35AFixR2ApproveMode(t, &state, whiteboxQAID)
	if err := saveSliceGuaranteeRecord(root, state); err != nil {
		t.Fatalf("save slice record: %v", err)
	}
	record, err := loadSliceGuaranteeRecord(root, "master", "slice-a")
	if err != nil {
		t.Fatalf("load slice record: %v", err)
	}
	if record.RunID != state.RunID || record.MasterRunID != state.SplitMasterRunID || record.Activation != state.RequirementGuarantee.Activation ||
		record.RequirementRevision != state.RequirementRevision || record.ManifestDigest != state.RequirementGuarantee.ManifestDigest ||
		record.Snapshot != state.CurrentSnapshot || record.RouteMode != state.RouteMode {
		t.Fatalf("slice record binding mismatch: %#v", record)
	}
	if !reflect.DeepEqual(record.CasesByMode, state.QACasesByMode) {
		t.Fatalf("slice record lost case authority:\nrecord=%#v\nstate=%#v", record.CasesByMode, state.QACasesByMode)
	}
	manifest, err := localGuaranteeManifest(state, whiteboxQAID, state.QACasesByMode[whiteboxQAID])
	if err != nil {
		t.Fatalf("rebuild slice manifest: %v", err)
	}
	review := record.ReviewsByMode[whiteboxQAID]
	wantReview := state.RequirementGuarantee.ReviewsByMode[whiteboxQAID]
	if len(record.ReviewsByMode) != 1 || !reflect.DeepEqual(review.Review.Binding, manifest.Binding) ||
		review.Review.Scope != wantReview.Review.Scope || !reflect.DeepEqual(review.Review.SourceDecisions, wantReview.Review.SourceDecisions) ||
		!reflect.DeepEqual(review.Review.PointDecisions, wantReview.Review.PointDecisions) || !reflect.DeepEqual(review.Review.CaseDecisions, wantReview.Review.CaseDecisions) ||
		review.Review.SetStatus != coverage.StatusPass || review.Whitelist == nil || wantReview.Whitelist == nil ||
		review.Whitelist.Digest != wantReview.Whitelist.Digest || !reflect.DeepEqual(review.Whitelist.Entries, wantReview.Whitelist.Entries) {
		t.Fatalf("sealed review binding/decisions/whitelist mismatch:\nrecord=%#v\nwant=%#v\nmanifest=%#v", review, wantReview, manifest)
	}
	digest := record.Digest
	record.Digest = ""
	wantDigest, err := marshalWithoutDigest(record)
	if err != nil {
		t.Fatalf("recompute slice record digest: %v", err)
	}
	if digest == "" || digest != wantDigest {
		t.Fatalf("slice record digest = %q, want content digest %q", digest, wantDigest)
	}
	if err := saveSliceGuaranteeRecord(root, state); err != nil {
		t.Fatalf("idempotent identical save failed: %v", err)
	}
	state.QACasesByMode[whiteboxQAID][0].Oracle = "different"
	if err := saveSliceGuaranteeRecord(root, state); err == nil || !strings.Contains(err.Error(), "immutable") {
		t.Fatalf("changed immutable record error = %v", err)
	}
}

func TestWhitebox35AFixR2NewRunStateInitializesPersistsAndValidatesIndependentReviewSeries(t *testing.T) {
	state := NewRunState("series-run", formalFlow, "requirement.md", "revision", "git", "base", "current", "prompt", "catalog", false, nil, []RequirementArtifact{{Path: "requirement.md", Revision: "revision"}})
	if err := validatePreDevelopmentReviewState(state); err != nil {
		t.Fatalf("new review state invalid: %v", err)
	}
	if len(state.PreDevelopmentReviewSeries) != 2 {
		t.Fatalf("series count = %d", len(state.PreDevelopmentReviewSeries))
	}
	for _, action := range preDevelopmentReviewActions {
		series := state.PreDevelopmentReviewSeries[action]
		if series.Action != action || series.ActivationSource != preDevelopmentReviewActivation || series.AutomaticLimit != 3 || series.CurrentLimit != 3 || series.RemainingAutomatic != 3 || series.NextRoundRequirement != "AUTOMATIC_CAPACITY" {
			t.Errorf("initial %s series = %#v", action, series)
		}
	}

	root := t.TempDir()
	if err := SaveRunState(root, state); err != nil {
		t.Fatalf("save initialized state: %v", err)
	}
	loaded, err := LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatalf("load initialized state: %v", err)
	}
	if !reflect.DeepEqual(loaded.PreDevelopmentReviewSeries, state.PreDevelopmentReviewSeries) {
		t.Fatalf("persisted series changed:\nloaded=%#v\nwant=%#v", loaded.PreDevelopmentReviewSeries, state.PreDevelopmentReviewSeries)
	}

	missing := state
	missing.PreDevelopmentReviewSeries = nil
	if err := validatePreDevelopmentReviewState(missing); err == nil || !strings.Contains(err.Error(), "missing") {
		t.Fatalf("missing series error = %v", err)
	}
	partial := state
	partial.PreDevelopmentReviewSeries = map[string]PreDevelopmentReviewSeries{"product-review": state.PreDevelopmentReviewSeries["product-review"]}
	if err := validatePreDevelopmentReviewState(partial); err == nil || !strings.Contains(err.Error(), "exactly product-review and start-readiness") {
		t.Fatalf("partial series error = %v", err)
	}
	tampered := state
	tampered.PreDevelopmentReviewSeries = newPreDevelopmentReviewSeries()
	series := tampered.PreDevelopmentReviewSeries["product-review"]
	series.Completed = 1
	tampered.PreDevelopmentReviewSeries["product-review"] = series
	if err := validatePreDevelopmentReviewState(tampered); err == nil || !strings.Contains(err.Error(), "does not match accepted candidate projection") {
		t.Fatalf("guessed completed count error = %v", err)
	}
}

func TestWhitebox35AFixR2ReviewRoundLimitIsActionScopedAndRuntimeRetryReusesOneAuthorization(t *testing.T) {
	state := NewRunState("round-run", formalFlow, "requirement.md", "revision", "git", "base", "current", "prompt", "catalog", true, nil, nil)
	for round := 1; round <= 3; round++ {
		whitebox35AFixR2RecordCandidate(t, &state, "product-review", fmt.Sprintf("product-%d", round), "PASS", nil, "")
	}
	product := state.PreDevelopmentReviewSeries["product-review"]
	start := state.PreDevelopmentReviewSeries["start-readiness"]
	if product.Completed != 3 || product.NextRoundRequirement != "USER_DECISION_REQUIRED" || start.Completed != 0 || start.RemainingAutomatic != 3 {
		t.Fatalf("independent series after three product rounds: product=%#v start=%#v", product, start)
	}
	if err := requirePreDevelopmentReviewPreparation(state, "product-review", false, ""); err == nil || !strings.Contains(err.Error(), "requires --user-requested") {
		t.Fatalf("unapproved fourth-round error = %v", err)
	}
	if err := authorizePreDevelopmentReviewDispatch(&state, "product-review", "product-4-runtime", true, "user authorizes one extra product review"); err != nil {
		t.Fatalf("authorize fourth round: %v", err)
	}
	authorizationID := reviewAuthorizationForDispatch(state, "product-review", "product-4-runtime")
	beforeRuntime := state.PreDevelopmentReviewSeries["product-review"].Completed
	result, err := recordSemanticReviewCandidate(&state, "product-review", PreparedDispatch{ID: "product-4-runtime", RequirementRevision: state.RequirementRevision, ReviewAuthorizationID: authorizationID}, ActionResult{Status: "RUNTIME_ERROR", Message: "temporary service error"})
	if err != nil || result.Status != "RUNTIME_ERROR" {
		t.Fatalf("runtime result = %#v, %v", result, err)
	}
	if state.PreDevelopmentReviewSeries["product-review"].Completed != beforeRuntime {
		t.Fatal("runtime error consumed a semantic round")
	}
	if err := authorizePreDevelopmentReviewDispatch(&state, "product-review", "product-4-retry", false, ""); err != nil {
		t.Fatalf("bind retry to existing authorization: %v", err)
	}
	retryAuthorization := reviewAuthorizationForDispatch(state, "product-review", "product-4-retry")
	if retryAuthorization == "" || retryAuthorization != authorizationID {
		t.Fatalf("retry authorization = %q, want %q", retryAuthorization, authorizationID)
	}
	whitebox35AFixR2RecordCandidate(t, &state, "product-review", "product-4-retry", "PASS", nil, retryAuthorization)
	product = state.PreDevelopmentReviewSeries["product-review"]
	start = state.PreDevelopmentReviewSeries["start-readiness"]
	if product.Completed != 4 || product.CurrentLimit != 4 || product.ExtraAuthorized != 1 || product.ExtraConsumed != 1 || product.RemainingCapacity != 0 || start.CurrentLimit != 3 || len(start.Authorizations) != 0 {
		t.Fatalf("fourth-round accounting: product=%#v start=%#v", product, start)
	}
}

func TestWhitebox35AFixR2RequirementStalenessPreservesSemanticRoundAndAuthorizationHistory(t *testing.T) {
	state := NewRunState("stale-run", formalFlow, "requirement.md", "revision", "git", "base", "current", "prompt", "catalog", true, nil, nil)
	for round := 1; round <= 3; round++ {
		whitebox35AFixR2RecordCandidate(t, &state, "product-review", fmt.Sprintf("round-%d", round), "PASS", nil, "")
	}
	if err := authorizePreDevelopmentReviewDispatch(&state, "product-review", "round-4", true, "one extra round"); err != nil {
		t.Fatal(err)
	}
	authorizationID := reviewAuthorizationForDispatch(state, "product-review", "round-4")
	whitebox35AFixR2RecordCandidate(t, &state, "product-review", "round-4", "PASS", nil, authorizationID)
	before := state.PreDevelopmentReviewSeries["product-review"]
	markPreDevelopmentReviewsStale(&state, "REQUIREMENT_REVISION_INVALIDATION")
	after := state.PreDevelopmentReviewSeries["product-review"]
	if after.Completed != before.Completed || after.CurrentLimit != before.CurrentLimit || !reflect.DeepEqual(after.RawCandidates, before.RawCandidates) || !reflect.DeepEqual(after.Authorizations, before.Authorizations) {
		t.Fatalf("staleness erased durable history:\nbefore=%#v\nafter=%#v", before, after)
	}
	if after.EffectiveReviewStatus.Status != "PENDING" || after.EffectiveReviewStatus.Source != "REQUIREMENT_REVISION_INVALIDATION" || len(after.EffectiveReviewStatus.BlockingFindingIDs) != 0 || len(after.EffectiveReviewStatus.AdvisoryFindingIDs) != 0 {
		t.Fatalf("stale effective projection = %#v", after.EffectiveReviewStatus)
	}
	if err := validatePreDevelopmentReviewState(state); err != nil {
		t.Fatalf("stale state invalid: %v", err)
	}
}

func TestWhitebox35AFixR2FindingDismissalAndSupersedingCorrectionReprojectWithoutRewritingRawEvidence(t *testing.T) {
	state := whitebox35AFixR2GuaranteedState(t)
	state.RunID = "correction-run"
	candidate := whitebox35AFixR2RecordCandidate(t, &state, "product-review", "review-1", "FAIL", []Finding{{Severity: "P1", Message: "blocking candidate"}, {Severity: "P2", Message: "advisory candidate"}}, "")
	rawBefore := candidate
	series := state.PreDevelopmentReviewSeries["product-review"]
	if series.EffectiveReviewStatus.Status != "FAIL" || len(series.EffectiveReviewStatus.BlockingFindingIDs) != 1 || len(series.EffectiveReviewStatus.AdvisoryFindingIDs) != 1 {
		t.Fatalf("initial effective projection = %#v", series.EffectiveReviewStatus)
	}
	blocking := candidate.Findings[0]
	if err := applyReviewCorrection(&state, ReviewCorrectionInput{
		Action: "product-review", DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest,
		RequirementRevision: candidate.RequirementRevision, FindingID: blocking.ID, Decision: "DISMISS",
		Reason: "user rejects the candidate finding", Source: "user decision", UserAuthorized: true,
		ExpectedEffectiveRevision: series.EffectiveReviewStatus.Revision,
	}); err != nil {
		t.Fatalf("dismiss finding: %v", err)
	}
	series = state.PreDevelopmentReviewSeries["product-review"]
	if series.EffectiveReviewStatus.Status != "PASS" || len(series.EffectiveReviewStatus.BlockingFindingIDs) != 0 || len(series.EffectiveReviewStatus.AdvisoryFindingIDs) != 1 || series.Completed != 1 || state.Actions["product-review"].Status != "PASS" {
		t.Fatalf("dismissed effective projection = %#v action=%#v", series, state.Actions["product-review"])
	}
	if !reflect.DeepEqual(series.RawCandidates[0], rawBefore) {
		t.Fatal("dismissal rewrote immutable raw candidate evidence")
	}

	state.Slicing = &Slicing{Decision: "no-split", Note: "before correction"}
	state.RouteMode = "full"
	state.SelectedGates = []string{whiteboxQAID, "quality-gate"}
	state.Actions["development-worker"] = ActionResult{Status: developmentComplete}
	state.QACasesByMode = map[string][]QACase{whiteboxQAID: {{ID: "CASE-050", Mode: "whitebox", ReviewStatus: "PASS", ApprovedSource: "stale-review-authority"}}}
	state.QADesignByMode = map[string]ActionResult{whiteboxQAID: {Status: "PASS"}}
	state.QADesignChangesByMode = map[string]QADesignChange{whiteboxQAID: {Modified: []string{"CASE-050"}}}
	state.QAReviewByMode = map[string]ActionResult{whiteboxQAID: {Status: "PASS"}}
	state.QAExecutionByMode = map[string]QAExecutionResult{whiteboxQAID: {Status: "PASS", Snapshot: state.CurrentSnapshot}}
	state.RequirementGuarantee.ReviewsByMode = map[string]GuaranteeReviewRecord{whiteboxQAID: {Review: coverage.ReviewResult{SetStatus: coverage.StatusPass}}}
	state.RequirementGuarantee.Report = RequirementGuaranteeReport{Status: "pass"}
	state.Gates["quality-gate"] = GateResult{Status: "PASS", Snapshot: state.CurrentSnapshot}
	latest := latestFindingDecisionID(series, candidate.ResultDigest, blocking.ID)
	if err := applyReviewCorrection(&state, ReviewCorrectionInput{
		Action: "product-review", DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest,
		RequirementRevision: candidate.RequirementRevision, FindingID: blocking.ID, Decision: "RESEVERITIZE", Severity: "P1",
		Supersedes: latest, Reason: "user restores blocking severity", Source: "user correction", UserAuthorized: true,
		ExpectedEffectiveRevision: series.EffectiveReviewStatus.Revision,
	}); err != nil {
		t.Fatalf("superseding correction: %v", err)
	}
	series = state.PreDevelopmentReviewSeries["product-review"]
	if series.EffectiveReviewStatus.Status != "FAIL" || len(series.Corrections) != 1 || series.Corrections[0].Supersedes != latest || series.Completed != 1 {
		t.Fatalf("superseding correction projection = %#v", series)
	}
	if state.Slicing != nil || state.RouteMode != "" || len(state.SelectedGates) != 0 || state.Actions["development-worker"].Status != developmentPending || state.Gates["quality-gate"].Status != "PENDING" {
		t.Fatalf("PASS-to-blocking correction did not invalidate downstream evidence: %#v", state)
	}
	if len(state.QADesignByMode) != 0 || len(state.QADesignChangesByMode) != 0 || len(state.QAReviewByMode) != 0 || len(state.QAExecutionByMode) != 0 || len(state.RequirementGuarantee.ReviewsByMode) != 0 || state.RequirementGuarantee.Report.Status != "" {
		t.Fatalf("PASS-to-blocking correction retained stale QA design/review/execution authority: design=%#v changes=%#v review=%#v execution=%#v guarantee=%#v", state.QADesignByMode, state.QADesignChangesByMode, state.QAReviewByMode, state.QAExecutionByMode, state.RequirementGuarantee)
	}
	if got := state.QACasesByMode[whiteboxQAID][0]; got.ReviewStatus != "PENDING" || got.ApprovedSource != "" {
		t.Fatalf("PASS-to-blocking correction retained stale case approval: %#v", got)
	}
	if state.NeedsReReview["product-review"] != blocking.Message || !reflect.DeepEqual(series.RawCandidates[0], rawBefore) {
		t.Fatalf("legacy projection/raw preservation mismatch: needs=%#v raw=%#v", state.NeedsReReview, series.RawCandidates[0])
	}
}

func TestWhitebox35AFixR2CorrectionRequiresAuthorizationExactBindingAndEffectiveRevisionCAS(t *testing.T) {
	newFixture := func() (RunState, SemanticReviewCandidate) {
		state := NewRunState("binding-run", formalFlow, "requirement.md", "revision", "git", "base", "current", "prompt", "catalog", true, nil, nil)
		candidate := whitebox35AFixR2RecordCandidate(t, &state, "start-readiness", "readiness-1", "FAIL", []Finding{{Severity: "P1", Message: "candidate"}}, "")
		return state, candidate
	}
	cases := []struct {
		name   string
		mutate func(*ReviewCorrectionInput)
		want   string
	}{
		{name: "no authorization", mutate: func(input *ReviewCorrectionInput) { input.UserAuthorized = false }, want: "explicit user authorization"},
		{name: "stale CAS", mutate: func(input *ReviewCorrectionInput) { input.ExpectedEffectiveRevision++ }, want: "expected effective revision"},
		{name: "wrong dispatch", mutate: func(input *ReviewCorrectionInput) { input.DispatchID = "other-dispatch" }, want: "binding does not match"},
		{name: "wrong result digest", mutate: func(input *ReviewCorrectionInput) { input.ResultDigest = "other-result-digest" }, want: "binding does not match"},
		{name: "wrong requirement revision", mutate: func(input *ReviewCorrectionInput) { input.RequirementRevision = "other-revision" }, want: "binding does not match"},
		{name: "unknown finding", mutate: func(input *ReviewCorrectionInput) { input.FindingID = "finding-not-in-result" }, want: "is not in the bound raw candidate"},
		{name: "missing reason", mutate: func(input *ReviewCorrectionInput) { input.Reason = "" }, want: "non-empty reason"},
		{name: "missing authorization source", mutate: func(input *ReviewCorrectionInput) { input.Source = "" }, want: "non-empty reason and authorization source"},
		{name: "invalid decision", mutate: func(input *ReviewCorrectionInput) { input.Decision = "APPROVE" }, want: "decision must be"},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			state, candidate := newFixture()
			seriesBefore := digestValue(state.PreDevelopmentReviewSeries["start-readiness"])
			input := ReviewCorrectionInput{
				Action: "start-readiness", DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest,
				RequirementRevision: candidate.RequirementRevision, FindingID: candidate.Findings[0].ID,
				Decision: "DISMISS", Reason: "user decision", Source: "user", UserAuthorized: true,
				ExpectedEffectiveRevision: state.PreDevelopmentReviewSeries["start-readiness"].EffectiveReviewStatus.Revision,
			}
			test.mutate(&input)
			if err := applyReviewCorrection(&state, input); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %q", err, test.want)
			}
			if after := digestValue(state.PreDevelopmentReviewSeries["start-readiness"]); after != seriesBefore {
				t.Fatalf("rejected correction mutated ledger: before=%s after=%s", seriesBefore, after)
			}
		})
	}
}

func TestWhitebox35AFixR2WholeResultInvalidationRemovesRoundAndReusesAuthorizationWithoutDeletingEvidence(t *testing.T) {
	state := NewRunState("invalidate-run", formalFlow, "requirement.md", "revision", "git", "base", "current", "prompt", "catalog", true, nil, nil)
	for round := 1; round <= 3; round++ {
		whitebox35AFixR2RecordCandidate(t, &state, "start-readiness", fmt.Sprintf("automatic-%d", round), "PASS", nil, "")
	}
	if err := authorizePreDevelopmentReviewDispatch(&state, "start-readiness", "extra-1", true, "authorize one extra semantic round"); err != nil {
		t.Fatal(err)
	}
	authorizationID := reviewAuthorizationForDispatch(state, "start-readiness", "extra-1")
	candidate := whitebox35AFixR2RecordCandidate(t, &state, "start-readiness", "extra-1", "PASS", nil, authorizationID)
	series := state.PreDevelopmentReviewSeries["start-readiness"]
	if series.Completed != 4 || series.ExtraConsumed != 1 {
		t.Fatalf("pre-invalidation series = %#v", series)
	}
	if err := applyReviewCorrection(&state, ReviewCorrectionInput{
		Action: "start-readiness", DispatchID: candidate.DispatchID, ResultDigest: candidate.ResultDigest,
		RequirementRevision: candidate.RequirementRevision, Decision: "INVALIDATE_RESULT", Invalidity: "MISBOUND",
		Reason: "user confirms the complete result was bound incorrectly", Source: "user correction", UserAuthorized: true,
		ExpectedEffectiveRevision: series.EffectiveReviewStatus.Revision,
	}); err != nil {
		t.Fatalf("invalidate result: %v", err)
	}
	series = state.PreDevelopmentReviewSeries["start-readiness"]
	if series.Completed != 3 || series.ExtraConsumed != 0 || len(series.RawCandidates) != 4 || len(series.Corrections) != 1 || !candidateExcluded(series, candidate.ResultDigest) || series.EffectiveReviewStatus.Status != "PENDING" || state.Actions["start-readiness"].Status != "PENDING" {
		t.Fatalf("post-invalidation series = %#v action=%#v", series, state.Actions["start-readiness"])
	}
	if err := authorizePreDevelopmentReviewDispatch(&state, "start-readiness", "extra-retry", false, ""); err != nil {
		t.Fatalf("reuse released authorization: %v", err)
	}
	if got := reviewAuthorizationForDispatch(state, "start-readiness", "extra-retry"); got != authorizationID {
		t.Fatalf("released authorization rebound as %q, want %q", got, authorizationID)
	}
	if err := validatePreDevelopmentReviewState(state); err != nil {
		t.Fatalf("invalidated-result ledger failed validation: %v", err)
	}
}
