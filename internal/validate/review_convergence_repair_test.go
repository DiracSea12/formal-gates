package validate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRepairEarlyPreparationListsEveryUnresolvedTarget(t *testing.T) {
	statuses := []string{reviewTargetUnprepared, reviewTargetPrepared, reviewTargetInFlight, reviewTargetRuntimeError, reviewTargetInvalidResult, reviewTargetRetryExhaust}
	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			state := RunState{CurrentSnapshot: "snapshot", ActiveReviewWaveID: "wave", ReviewWaves: map[string]ReviewWave{"wave": {ID: "wave", Snapshot: "snapshot", Status: reviewWaveCollecting, Expected: map[string]ReviewWaveTarget{
				"gate:quality": {Status: reviewTargetPass}, "gate:architecture": {Status: status}, "action:qa-execution:blackbox": {Status: reviewTargetPrepared},
			}}}}
			err := ensureUnifiedRepairPlan(&state)
			if err == nil || !strings.Contains(err.Error(), "gate:architecture="+status) || !strings.Contains(err.Error(), "action:qa-execution:blackbox=PREPARED") || strings.Contains(err.Error(), "gate:quality") || state.ReviewWaves["wave"].RepairPlan != nil {
				t.Fatalf("unresolved targets missing or repair created: %v", err)
			}
		})
	}
}

func TestRepairCaseFailDirectsDesignAndRereview(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := beginQA(t, root, pkg, "case-fail-guidance")
	review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "case-fail-reviewer")
	decisions := passingReviewDecisions(state)
	decisions[0].Outcome = "FAIL"
	decisions[0].Reason = "the referenced executable is absent from the case setup"
	var err error
	state, err = RecordQAReview(root, pkg, state.RunID, review, decisions, "", nil)
	if err != nil {
		t.Fatal(err)
	}
	if state.qaReview("").Status != "FAIL" || state.qaDesign("").Status != "PENDING" {
		t.Fatal("case failure did not invalidate the design")
	}
	before := stateBytes(t, root, state.RunID)
	_, err = PrepareAction(root, pkg, state.RunID, "qa-review", "", false, "")
	if err == nil || !strings.Contains(err.Error(), "QA Design") || !strings.Contains(err.Error(), "bounded QA Review") || stateBytes(t, root, state.RunID) != before {
		t.Fatalf("case FAIL omitted its design/re-review work or changed state: %v", err)
	}
}

func TestRepairCurrentFindingEncodingAndDigests(t *testing.T) {
	for _, raw := range []string{
		`{"severity":"P2","verification":{},"message":"current suggestion","locations":["requirements.md:1"]}`,
		`{"severity":"P2","verification":{},"message":"current suggestion"}`,
		`{"severity":"P1","verification":{"scope":"normal use","severity":"core failure","causality":"wrong branch","reproduction":"run command"},"message":"current blocker"}`,
	} {
		var finding Finding
		if err := json.Unmarshal([]byte(raw), &finding); err != nil {
			t.Fatal(err)
		}
		data, err := json.Marshal(finding)
		if err != nil || string(data) != raw {
			t.Fatalf("finding changed its persisted representation: %s, %v", data, err)
		}
		legacyDigest := digestValue(struct {
			Severity  string
			Message   string
			Locations []string
		}{finding.Severity, finding.Message, finding.Locations})
		rawSemantic := `{"id":"finding-` + shortDigest(legacyDigest) + `","digest":"` + legacyDigest + `",` + raw[1:]
		var semantic SemanticReviewFinding
		if err := json.Unmarshal([]byte(rawSemantic), &semantic); err != nil {
			t.Fatal(err)
		}
		data, err = json.Marshal(semantic)
		if err != nil || string(data) != rawSemantic {
			t.Fatalf("semantic finding changed its persisted representation: %s, %v", data, err)
		}
		if semanticFindingDigest(semantic) == legacyDigest {
			t.Fatal("current finding dropped the extended digest contract")
		}
		semantic.Digest = semanticFindingDigest(semantic)
		semantic.Verification.Scope = "different scope evidence"
		if semanticFindingDigest(semantic) == semantic.Digest {
			t.Fatal("finding digest ignored changed evidence")
		}
	}
}

func TestRepairPreviewUsesCurrentFindingIdentityAndEffectiveAdjudication(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := mustStart(t, root, pkg, "preview-current-findings")
	state.SelectedGates = []string{"quality"}
	state.ActiveReviewWaveID = "current-wave"
	state.ReviewWaves["old-wave"] = ReviewWave{ID: "old-wave", Status: reviewWaveComplete}
	state.ReviewWaves["current-wave"] = ReviewWave{ID: "current-wave", Status: reviewWaveReadyForRepair}
	state.Problems["open-problem"] = CanonicalProblem{ID: "open-problem", Status: "OPEN"}
	state.Problems["resolved-problem"] = CanonicalProblem{ID: "resolved-problem", Status: "RESOLVED"}
	state.Gates["quality"] = GateResult{Status: "FAIL", Findings: []Finding{{ID: "raw-p2", Severity: "P2", Message: "same message"}, {ID: "raw-dismissed", Severity: "P1", Message: "dismissed claim"}}}
	state.PostReviewFindings = map[string]PostReviewFinding{
		"old-p2":       {ID: "old-p2", WaveID: "old-wave", SourceKind: "gate", SourceTarget: "quality", Severity: "P2", Message: "same message", Adjudications: []FindingAdjudication{{Decision: "DISMISSED"}}},
		"new-p2":       {ID: "new-p2", WaveID: "current-wave", SourceKind: "gate", SourceTarget: "quality", Severity: "P2", Message: "same message", Adjudications: []FindingAdjudication{{Decision: "NEW", ProblemID: "open-problem"}}},
		"distinct-p2":  {ID: "distinct-p2", WaveID: "current-wave", SourceKind: "gate", SourceTarget: "quality", Severity: "P2", Message: "same message", Adjudications: []FindingAdjudication{{Decision: "DISMISSED"}}},
		"dismissed-p1": {ID: "dismissed-p1", WaveID: "current-wave", SourceKind: "gate", SourceTarget: "quality", Severity: "P1", Message: "dismissed claim", Adjudications: []FindingAdjudication{{Decision: "NEW", ProblemID: "open-problem"}, {Decision: "DISMISSED"}}},
		"resolved-p1":  {ID: "resolved-p1", WaveID: "current-wave", SourceKind: "gate", SourceTarget: "quality", Severity: "P1", Message: "fixed claim", Adjudications: []FindingAdjudication{{Decision: "SAME_AS", ProblemID: "resolved-problem"}}},
		"open-p1":      {ID: "open-p1", WaveID: "current-wave", SourceKind: "gate", SourceTarget: "quality", Severity: "P1", Message: "open claim", Adjudications: []FindingAdjudication{{Decision: "NEW", ProblemID: "open-problem"}}},
		"pending-p3":   {ID: "pending-p3", WaveID: "current-wave", SourceKind: "gate", SourceTarget: "quality", Severity: "P3", Message: "pending advice"},
	}
	if err := SaveRunState(root, state); err != nil {
		t.Fatal(err)
	}
	before := stateBytes(t, root, state.RunID)
	var digest string
	for range 40 {
		preview, err := PreviewRepairAuthorization(root, pkg, state.RunID, RepairRerunEstimate{})
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(preview.BlockingFindings, []string{"open-p1: open claim"}) || !reflect.DeepEqual(preview.GateSuggestions, map[string]int{"P2:NEW:OPEN": 1, "P2:DISMISSED": 1, "P3:UNADJUDICATED": 1}) {
			t.Fatalf("preview did not project current identities and effective decisions: %+v", preview)
		}
		if digest != "" && digest != preview.Digest {
			t.Fatalf("unchanged state produced different preview digests: %s / %s", digest, preview.Digest)
		}
		digest = preview.Digest
	}
	if stateBytes(t, root, state.RunID) != before {
		t.Fatal("read-only preview changed persisted state")
	}
}

// These are developer regressions for the unified repair. They exercise the
// production transitions in disposable repositories, not the active formal run.
func TestRepairCorrectionAfterDispatchMovesToNextWave(t *testing.T) {
	root, pkg, state := repairGuaranteedDelivery(t, "deferred-correction")
	if _, err := PrepareReviewWave(root, pkg, state.RunID); err != nil {
		t.Fatal(err)
	}
	qa := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	state, err := RecordQAExecution(root, pkg, state.RunID, qa, passingExecution(state.qaModeCases("blackbox")), "")
	if err != nil {
		t.Fatal(err)
	}
	state = recordGateResult(t, root, pkg, state, "quality", "deferred-gate", "FAIL", "", []FindingInput{fixtureBlockingFinding("P1", "first issue", ".gitignore:1"), fixtureBlockingFinding("P1", "second issue", ".gitignore:1")})
	state = repairAdjudicateAll(t, root, pkg, state)
	oldWave := state.ActiveReviewWaveID
	var findings []string
	for id, f := range state.PostReviewFindings {
		if f.WaveID == oldWave {
			findings = append(findings, id)
		}
	}
	first, _ := effectiveFindingAdjudication(state.PostReviewFindings[findings[0]])
	second, _ := effectiveFindingAdjudication(state.PostReviewFindings[findings[1]])
	correct := func(decision, problem string) {
		input := ReviewWaveAdjudicationInput{WaveID: oldWave, StateRevision: state.StateIntegrity, Findings: []FindingAdjudicationInput{{FindingID: findings[1], Decision: decision, ProblemID: problem, Dimensions: second.Dimensions, Verification: second.Verification, References: second.References, Reason: "rechecked the five dimensions and corrected the association"}}}
		var err error
		state, err = AdjudicateReviewWave(root, pkg, state.RunID, input)
		if err != nil {
			t.Fatal(err)
		}
	}
	correct("SAME_AS", first.ProblemID)
	development := prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	state, _ = LoadRunState(root, state.RunID)
	plan := *state.ReviewWaves[oldWave].RepairPlan
	correct("NEW", "")
	if !reflect.DeepEqual(plan, *state.ReviewWaves[oldWave].RepairPlan) {
		t.Fatal("correction changed an already-dispatched plan")
	}
	writeTestFile(t, filepath.Join(root, "deferred-fix.txt"), "first issue repaired\n")
	commitAll(t, root, "repair only the dispatched problem")
	state, err = AdvanceSnapshot(root, pkg, state.RunID, development, false, "")
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordExecutionScope(root, pkg, state.RunID, "blackbox", "AFFECTED", []string{state.qaModeCases("blackbox")[0].ID}, "repair changes first behavior")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareReviewWave(root, pkg, state.RunID); err != nil {
		t.Fatal(err)
	}
	qa = prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	state, err = RecordQAExecution(root, pkg, state.RunID, qa, passingExecution(qaExecutionRequiredCases(state, "blackbox")), "")
	if err != nil {
		t.Fatal(err)
	}
	state = recordGateResult(t, root, pkg, state, "quality", "deferred-recheck", "PASS", "", nil)
	if state.ReviewWaves[state.ActiveReviewWaveID].Status != reviewWaveReadyForRepair || state.Problems[first.ProblemID].Status != "RESOLVED" || state.Problems[second.ProblemID].Status != "OPEN" {
		t.Fatal("verification dropped the deferred correction or failed to resolve the actual repair")
	}
	prepareDispatch(t, root, pkg, state.RunID, "development-worker")
	state, _ = LoadRunState(root, state.RunID)
	if got := state.ReviewWaves[state.ActiveReviewWaveID].RepairPlan.ProblemIDs; len(got) != 1 || got[0] != second.ProblemID {
		t.Fatalf("next repair = %v", got)
	}
}

func TestRepairSeverityRationaleContractAtRecordBoundaries(t *testing.T) {
	for _, target := range []string{"product-review", "start-readiness", "qa-review", "quality"} {
		t.Run(target, func(t *testing.T) {
			root, pkg := workflowFixture(t)
			state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "rationale-"+target))
			mode := ""
			if target == "start-readiness" {
				state = recordProductReview(t, root, pkg, state)
			}
			if target == "qa-review" {
				state = beginQA(t, root, pkg, "rationale-qa-design")
			}
			if target == "quality" {
				state = readyDeliveryForRoute(t, root, pkg, "rationale-gate", "custom", []string{"quality"})
			}
			for _, rationale := range []string{"违反需求。", "P1：正常使用路径产生明确错误或阻塞核心验收"} {
				var d string
				if target == "product-review" || target == "start-readiness" {
					d = prepareDispatch(t, root, pkg, state.RunID, target)
				} else {
					d = prepareAndClaim(t, root, pkg, state.RunID, target, "rationale-"+rationale, mode)
				}
				finding := completeGovernanceFinding("normal path fails", target == "quality")
				finding.SeverityRationale = rationale
				var err error
				switch target {
				case "quality":
					_, err = RecordGate(root, pkg, state.RunID, target, d, "FAIL", "", comparedRange(state), []FindingInput{finding})
				case "qa-review":
					_, err = RecordQAReview(root, pkg, state.RunID, d, passingReviewDecisions(state), "", []FindingInput{finding}, QAReviewRecordOptions{})
				default:
					_, err = RecordAction(root, pkg, state.RunID, target, d, "FAIL", "", []FindingInput{finding}, false, "", fixtureOperatorVerification())
				}
				if err == nil || !strings.Contains(err.Error(), "REJECTED_INVALID_RESULT") {
					t.Fatalf("placeholder %q was not rejected: %v", rationale, err)
				}
				state, _ = LoadRunState(root, state.RunID)
				if len(state.PostReviewFindings) != 0 || len(state.Problems) != 0 {
					t.Fatal("invalid rationale reached the problem ledger")
				}
			}
		})
	}
}

func TestRepairPendingSignatureReusesTicket(t *testing.T) {
	for _, target := range []string{"product-review", "qa-review", "qa-execution", "quality"} {
		t.Run(target, func(t *testing.T) {
			root, pkg := workflowFixture(t)
			state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "pending-"+target))
			if target == "qa-review" {
				state = beginQA(t, root, pkg, "pending-qa-design")
			}
			if target == "qa-execution" {
				state = readyDelivery(t, root, pkg, "pending-qa-execution-delivery")
			}
			if target == "quality" {
				state = readyDeliveryForRoute(t, root, pkg, "pending-gate", "custom", []string{"quality"})
			}
			prepare := func() (string, error) {
				if target == "quality" {
					return PrepareGate(root, pkg, state.RunID, target, false, "")
				}
				return PrepareAction(root, pkg, state.RunID, target, "", false, "")
			}
			prompt, err := prepare()
			if err != nil {
				t.Fatal(err)
			}
			// Persist the documented recovered PENDING state; the test targets
			// prepare's reuse boundary and does not fabricate a semantic result.
			state, err = mutateRun(root, state.RunID, func(s *RunState) error {
				for id, d := range s.Dispatches {
					if d.Target == target && d.Status == "OPEN" {
						d.Status = "PENDING"
						s.Dispatches[id] = d
					}
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			count, tasks := len(state.Dispatches), len(state.DispatchObligations)
			if repeated, err := prepare(); err != nil || repeated != prompt {
				t.Fatalf("pending ticket not reused: %v", err)
			}
			state, _ = LoadRunState(root, state.RunID)
			if len(state.Dispatches) != count || len(state.DispatchObligations) != tasks {
				t.Fatal("pending preparation consumed another attempt")
			}
		})
	}
}

func repairGuaranteedDelivery(t *testing.T, id string) (string, string, RunState) {
	t.Helper()
	root, pkg, state := readyGuaranteeRoute(t, id, false)
	state = recordSlicing(t, root, pkg, state, "no-split")
	state = setRoute(t, root, pkg, state, "custom", []string{blackboxQAID, "quality"})
	var err error
	state, err = RegisterQAWorktree(root, pkg, state.RunID, createQAWorktree(t, root, state))
	if err != nil {
		t.Fatal(err)
	}
	state = recordGuaranteeDesignAndReview(t, root, pkg, state, "blackbox", []QACaseInput{
		{Mode: "blackbox", Description: "first behavior", Procedure: "run the first public operation", Oracle: "first result matches", AcceptanceCriteria: []string{"AC-001"}},
		{Mode: "blackbox", Description: "second behavior", Procedure: "run the second public operation", Oracle: "second result matches", AcceptanceCriteria: []string{"AC-009"}},
	})
	state = advanceGuaranteeSnapshot(t, root, pkg, state, id+"-delivery")
	return root, pkg, state
}

func repairGateP2CorrectionFixture(t *testing.T, id string) (string, string, RunState, ReviewWaveAdjudicationInput) {
	t.Helper()
	root, pkg, state := repairGuaranteedDelivery(t, id)
	if _, err := PrepareReviewWave(root, pkg, state.RunID); err != nil {
		t.Fatal(err)
	}
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	state, err := RecordQAExecution(root, pkg, state.RunID, dispatch, passingExecution(state.qaModeCases("blackbox")), "")
	if err != nil {
		t.Fatal(err)
	}
	gate := prepareAndClaim(t, root, pkg, state.RunID, "quality", id+"-gate")
	location := id + "-delivery.txt:1"
	state, err = RecordGate(root, pkg, state.RunID, "quality", gate, "PASS", "", comparedRange(state), []FindingInput{{Severity: "P2", Message: "first result is incorrect", Locations: []string{location}}, {Severity: "P2", Message: "duplicate output"}, {Severity: "P3", Message: "optional wording"}})
	if err != nil {
		t.Fatal(err)
	}
	input := ReviewWaveAdjudicationInput{WaveID: state.ActiveReviewWaveID, StateRevision: state.StateIntegrity}
	for findingID, finding := range state.PostReviewFindings {
		if finding.Message == "first result is incorrect" {
			input.Findings = []FindingAdjudicationInput{{FindingID: findingID, Decision: "NEW", Dimensions: ProblemDimensions{Requirement: "REQ-001", NormalFailurePath: "run the first public operation", AffectedModule: "public command", RootCause: "wrong result branch", ActualConsequence: "the required first result is unavailable"}, Verification: FindingVerification{Scope: "main agent checked REQ-001", Severity: "main agent checked the unusable result, not just the item status", Causality: "main agent traced the wrong result branch", Reproduction: "main agent independently executed the first public operation and observed the incorrect result"}, References: []string{location}, Reason: "the observed failure violates the core result contract and cannot remain advisory", GateP1Evidence: &GateP1EvidenceInput{SeverityRationale: "The required first operation returns an unusable result with no documented workaround; P2 would understate the broken core acceptance path.", Requirement: "REQ-001", NormalPath: "run the first public operation", ActualError: "the first result differs from the required output", DeferralImpact: "shipping leaves the required first operation unusable", Locations: []string{location}}}}
		}
	}
	if len(input.Findings) != 1 {
		t.Fatal("fixture gate P2 was not collected")
	}
	return root, pkg, state, input
}

func TestRepairGateP2EscalationRejectsIncompleteOrMisboundEvidence(t *testing.T) {
	root, pkg, state, input := repairGateP2CorrectionFixture(t, "p2-correction-rejections")
	before := stateBytes(t, root, state.RunID)
	for name, change := range map[string]func(*ReviewWaveAdjudicationInput){
		"rationale":   func(i *ReviewWaveAdjudicationInput) { i.Findings[0].GateP1Evidence.SeverityRationale = "" },
		"requirement": func(i *ReviewWaveAdjudicationInput) { i.Findings[0].GateP1Evidence.Requirement = "" },
		"unknown requirement": func(i *ReviewWaveAdjudicationInput) {
			i.Findings[0].GateP1Evidence.Requirement = "REQ-999"
			i.Findings[0].Dimensions.Requirement = "REQ-999"
		},
		"normal path":      func(i *ReviewWaveAdjudicationInput) { i.Findings[0].GateP1Evidence.NormalPath = "" },
		"actual error":     func(i *ReviewWaveAdjudicationInput) { i.Findings[0].GateP1Evidence.ActualError = "" },
		"deferral":         func(i *ReviewWaveAdjudicationInput) { i.Findings[0].GateP1Evidence.DeferralImpact = "" },
		"location missing": func(i *ReviewWaveAdjudicationInput) { i.Findings[0].GateP1Evidence.Locations = nil },
		"location invalid": func(i *ReviewWaveAdjudicationInput) {
			i.Findings[0].GateP1Evidence.Locations = []string{"missing.go:1"}
		},
		"line invalid": func(i *ReviewWaveAdjudicationInput) {
			i.Findings[0].GateP1Evidence.Locations = []string{"p2-correction-rejections-delivery.txt:99999"}
		},
		"scope verification":       func(i *ReviewWaveAdjudicationInput) { i.Findings[0].Verification.Scope = "" },
		"severity verification":    func(i *ReviewWaveAdjudicationInput) { i.Findings[0].Verification.Severity = "" },
		"causality verification":   func(i *ReviewWaveAdjudicationInput) { i.Findings[0].Verification.Causality = "" },
		"independent reproduction": func(i *ReviewWaveAdjudicationInput) { i.Findings[0].Verification.Reproduction = "" },
		"stale revision":           func(i *ReviewWaveAdjudicationInput) { i.StateRevision = "old" },
		"wrong wave":               func(i *ReviewWaveAdjudicationInput) { i.WaveID = "another-wave" },
		"wrong finding":            func(i *ReviewWaveAdjudicationInput) { i.Findings[0].FindingID = "another-finding" },
		"dismissed correction":     func(i *ReviewWaveAdjudicationInput) { i.Findings[0].Decision = "DISMISSED" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate := input
			candidate.Findings = append([]FindingAdjudicationInput{}, input.Findings...)
			evidence := *input.Findings[0].GateP1Evidence
			candidate.Findings[0].GateP1Evidence = &evidence
			change(&candidate)
			if _, err := AdjudicateReviewWave(root, pkg, state.RunID, candidate); err == nil || stateBytes(t, root, state.RunID) != before {
				t.Fatalf("invalid correction was accepted or wrote state: %v", err)
			}
		})
	}
	// P3 stays an advisory; supplying the P1 evidence envelope cannot promote it.
	for id, finding := range state.PostReviewFindings {
		if finding.Severity == "P3" {
			candidate := input
			candidate.Findings = append([]FindingAdjudicationInput{}, input.Findings...)
			candidate.Findings[0].FindingID = id
			if _, err := AdjudicateReviewWave(root, pkg, state.RunID, candidate); err == nil || stateBytes(t, root, state.RunID) != before {
				t.Fatalf("P3 entered the P2-only correction path: %v", err)
			}
		}
	}
}

func TestRepairGateP2EscalationPreservesProblemAndEnforcesP1(t *testing.T) {
	root, pkg, state, input := repairGateP2CorrectionFixture(t, "p2-correction-effective")
	state = repairAdjudicateAll(t, root, pkg, state)
	state, err := mutateRun(root, state.RunID, ensureUnifiedRepairPlan)
	if err != nil {
		t.Fatal(err)
	}
	before := state
	findingID := input.Findings[0].FindingID
	priorAdjudication, _ := effectiveFindingAdjudication(state.PostReviewFindings[findingID])
	input.StateRevision = state.StateIntegrity
	input.Findings[0].Decision, input.Findings[0].ProblemID = "SAME_AS", priorAdjudication.ProblemID
	state, err = AdjudicateReviewWave(root, pkg, state.RunID, input)
	if err != nil {
		t.Fatal(err)
	}
	corrected := state.PostReviewFindings[findingID]
	audit, _ := effectiveFindingAdjudication(corrected)
	if corrected.ID != findingID || corrected.Severity != "P1" || audit.ProblemID != priorAdjudication.ProblemID || audit.OriginalFinding == nil || audit.OriginalFinding.Severity != "P2" || audit.GateP1Evidence == nil || audit.GateP1Evidence.Severity != "P1" || audit.GateP1Evidence.ID != audit.OriginalFinding.ID || audit.Supersedes != priorAdjudication.Revision {
		t.Fatalf("correction lost its source evidence or problem identity: %+v", corrected)
	}
	gate := state.Gates["quality"]
	wave := state.ReviewWaves[input.WaveID]
	if gate.Status != "FAIL" || gate.Findings[0].Severity != "P1" || gate.Findings[1].Severity != "P2" || gate.Findings[2].Severity != "P3" || wave.Expected["gate:quality"].Status != "FAIL" || wave.Status != reviewWaveReadyForRepair {
		t.Fatalf("P1 correction did not reach effective gate/wave: %+v %+v", gate, wave)
	}
	if !reflect.DeepEqual(before.Dispatches, state.Dispatches) || !reflect.DeepEqual(before.DispatchObligations, state.DispatchObligations) || !reflect.DeepEqual(before.ReviewWaves[input.WaveID].RepairPlan, wave.RepairPlan) || len(before.Problems) != len(state.Problems) || before.CompletedReviewWaves != state.CompletedReviewWaves || before.ExtraReviewWaves != state.ExtraReviewWaves {
		t.Fatal("correction changed the repair identity, associations, dispatches or review capacity")
	}
	for id, prior := range before.Problems {
		current := state.Problems[id]
		if current.ID != prior.ID || current.Status != prior.Status || !reflect.DeepEqual(current.FindingIDs, prior.FindingIDs) {
			t.Fatalf("correction changed a problem association: %+v %+v", prior, current)
		}
	}
	preview := repairAuthorizationPreviewFromState(state, RepairRerunEstimate{})
	if len(preview.BlockingFindings) != 1 {
		t.Fatalf("corrected P1 is absent from the public blocker projection: %+v", preview)
	}
	if _, err := Seal(root, pkg, state.RunID, nil, false, ""); err == nil || !strings.Contains(err.Error(), "before Seal") {
		t.Fatalf("corrected P1 did not block Seal: %v", err)
	}
	state = advanceGuaranteeSnapshot(t, root, pkg, state, "p1-correction-repair")
	if _, err := CompleteGateP2Repair(root, pkg, state.RunID, input.WaveID, []string{"full build passed"}, []string{"full project tests passed"}, nil); err == nil || !strings.Contains(err.Error(), "not a pure gate-P2 repair") {
		t.Fatalf("corrected P1 was carried through the P2-only completion path: %v", err)
	}
	state, err = RecordExecutionScope(root, pkg, state.RunID, "blackbox", "AFFECTED", []string{state.qaModeCases("blackbox")[0].ID}, "the first public operation is affected by this intermediate P1 repair")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := PrepareReviewWave(root, pkg, state.RunID); err != nil {
		t.Fatal(err)
	}
	state, _ = LoadRunState(root, state.RunID)
	if state.ReviewWaves[state.ActiveReviewWaveID].Expected["gate:quality"].Status != reviewTargetPrepared {
		t.Fatal("repaired P1 gate did not require its ordinary review on the new snapshot")
	}
}

func repairAdjudicateAll(t *testing.T, root, pkg string, state RunState) RunState {
	t.Helper()
	input := ReviewWaveAdjudicationInput{WaveID: state.ActiveReviewWaveID, StateRevision: state.StateIntegrity}
	for id, finding := range state.PostReviewFindings {
		if finding.WaveID != input.WaveID {
			continue
		}
		input.Findings = append(input.Findings, FindingAdjudicationInput{
			FindingID: id, Decision: "NEW",
			Dimensions:   ProblemDimensions{Requirement: "REQ-001", NormalFailurePath: "the documented operation", AffectedModule: "public command", RootCause: finding.Message, ActualConsequence: "the reported result requires a correction"},
			Verification: FindingVerification{Scope: "confirmed requirement match", Severity: "checked the observed consequence", Causality: "traced the reported command path", Reproduction: "repeated the bound operation"},
			References:   []string{"requirements.md:1"}, Reason: "the five dimensions identify an unresolved problem",
		})
	}
	next, err := AdjudicateReviewWave(root, pkg, state.RunID, input)
	if err != nil {
		t.Fatal(err)
	}
	return next
}

func TestRepairGuaranteeAffectedWaveCanEnterFinalFull(t *testing.T) {
	for _, finalOutcome := range []string{"PASS", "FAIL"} {
		t.Run(finalOutcome, func(t *testing.T) {
			root, pkg, state := repairGuaranteedDelivery(t, "affected-final-"+strings.ToLower(finalOutcome))
			if _, err := PrepareReviewWave(root, pkg, state.RunID); err != nil {
				t.Fatal(err)
			}
			results := passingExecution(state.qaModeCases("blackbox"))
			results[0].Outcome, results[0].Observation = "FAIL", "first operation returned the wrong result"
			dispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
			state, err := RecordQAExecution(root, pkg, state.RunID, dispatch, results, "")
			if err != nil {
				t.Fatal(err)
			}
			state = recordGateResult(t, root, pkg, state, "quality", state.RunID+"-quality", "PASS", "", nil)
			state = repairAdjudicateAll(t, root, pkg, state)
			state = advanceGuaranteeSnapshot(t, root, pkg, state, state.RunID+"-repair")
			state, err = RecordCarry(root, pkg, state.RunID, "", nil, "", true, "the repair changes the failing operation only")
			if err != nil {
				t.Fatal(err)
			}
			state, err = RecordExecutionScope(root, pkg, state.RunID, "blackbox", "AFFECTED", []string{results[0].CaseID}, "first operation was repaired")
			if err != nil {
				t.Fatal(err)
			}
			if _, err := PrepareReviewWave(root, pkg, state.RunID); err != nil {
				t.Fatal(err)
			}
			dispatch = prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
			state, err = RecordQAExecution(root, pkg, state.RunID, dispatch, passingExecution(qaExecutionRequiredCases(state, "blackbox")), "")
			if err != nil {
				t.Fatal(err)
			}
			wave := state.ReviewWaves[state.ActiveReviewWaveID]
			if state.qaExecution("blackbox").Status != "PASS" || wave.Status != reviewWaveComplete || state.PreRepairSnapshot != "" {
				t.Fatalf("intermediate result was not a completed PASS: %+v %+v", state.qaExecution("blackbox"), wave)
			}
			if got := state.qaExecution("blackbox").Cases[1]; got.Origin != "inherited" || got.Outcome != "PASS" {
				t.Fatalf("unaffected PASS was not retained: %+v", got)
			}
			if deriveRequirementGuarantee(root, state).Status == "pass" {
				t.Fatal("intermediate inherited results satisfied final FULL")
			}
			count := state.CompletedReviewWaves
			state, err = RecordExecutionScope(root, pkg, state.RunID, "blackbox", "FULL", nil, "final candidate regression")
			if err != nil {
				t.Fatal(err)
			}
			finalWave, err := PrepareReviewWave(root, pkg, state.RunID)
			if err != nil || finalWave.ID != wave.ID || finalWave.ExpectedDigest != wave.ExpectedDigest {
				t.Fatalf("final regression changed the frozen wave: %+v %v", finalWave, err)
			}
			dispatch = prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
			results = passingExecution(state.qaModeCases("blackbox"))
			results[0].Outcome = finalOutcome
			state, err = RecordQAExecution(root, pkg, state.RunID, dispatch, results, "")
			if err != nil || state.qaExecution("blackbox").Status != finalOutcome || state.CompletedReviewWaves != count {
				t.Fatalf("final FULL result/count: %+v %v", state.qaExecution("blackbox"), err)
			}
			for _, result := range state.qaExecution("blackbox").Cases {
				if result.Origin != "executed" {
					t.Fatalf("final FULL inherited a result: %+v", result)
				}
			}
			if finalOutcome == "PASS" && deriveRequirementGuarantee(root, state).Status != "pass" {
				t.Fatal("final FULL PASS did not close the guarantee")
			}
			if finalOutcome == "FAIL" {
				state = repairAdjudicateAll(t, root, pkg, state)
				if _, err := PrepareAction(root, pkg, state.RunID, "development-worker", "", false, ""); err != nil {
					t.Fatalf("a real final FULL failure has no repair path: %v", err)
				}
			}
		})
	}
}

func TestRepairNonReviewImpactAndSelectiveThaw(t *testing.T) {
	root, pkg, state := repairGuaranteedDelivery(t, "impact-thaw")
	oldVersion := state.QACaseSets["blackbox"].Version
	oldCases := append([]QACase{}, state.qaModeCases("blackbox")...)
	writeTestFile(t, filepath.Join(root, "requirements.md"), guaranteeRequirementDocument("第二个结果采用修订后的成功判据。"))
	before := stateBytes(t, root, state.RunID)
	if _, err := UpdateRequirement(root, pkg, state.RunID, "", false, "changed", nil); err == nil || stateBytes(t, root, state.RunID) != before {
		t.Fatalf("ordinary meaning change without authorization was not atomic: %v", err)
	}
	impact := RequirementImpactInput{AcceptanceCriteria: "AC-009 changes its success criterion; product review must restart"}
	preview, err := PreviewRequirementImpact(root, pkg, state.RunID, "", nil, "", impact)
	if err != nil || stateBytes(t, root, state.RunID) != before {
		t.Fatalf("impact preview: %v", err)
	}
	auth := &RequirementImpactAuthorization{PreviewDigest: preview.Digest, StateRevision: preview.StateRevision, Impact: impact}
	if _, err := UpdateRequirement(root, pkg, state.RunID, "", false, "changed", nil, RequirementUpdateOptions{ImpactAuthorization: auth}); err == nil || stateBytes(t, root, state.RunID) != before {
		t.Fatalf("missing confirmation was accepted: %v", err)
	}
	auth.UserConfirmation = "isolated regression fixture: the user confirms the displayed rollback"
	state, err = UpdateRequirement(root, pkg, state.RunID, "", false, "changed", nil, RequirementUpdateOptions{ImpactAuthorization: auth})
	if err != nil {
		t.Fatal(err)
	}
	if state.Actions["product-review"].Status != "PENDING" || state.RequirementConfirmed || len(state.QAReviewByMode) != 0 || state.QACaseSets["blackbox"].Version != oldVersion {
		t.Fatalf("rollback lost the selective thaw boundary: %+v %+v", state.Actions, state.QACaseSets)
	}
	if len(state.ReviewDeltaReceipts) != 1 || state.ReviewDeltaReceipts[0].ImpactPreviewDigest != preview.Digest {
		t.Fatal("ordinary impact authorization was not retained")
	}
	for i, testCase := range state.qaModeCases("blackbox") {
		if testCase.ReviewStatus != oldCases[i].ReviewStatus {
			t.Fatal("requirement registration erased an existing case approval")
		}
	}
	state, err = ThawQACaseSet(root, pkg, state.RunID, "blackbox", "REQUIREMENT_MEANING", "only AC-009 changed", state.RequirementRevision, []string{oldCases[1].ID})
	if err != nil {
		t.Fatal(err)
	}
	if cases := state.qaModeCases("blackbox"); cases[0].ReviewStatus != "PASS" || cases[1].ReviewStatus != "PENDING" || state.QACaseSets["blackbox"].Status != caseSetThawed {
		t.Fatalf("thaw did not retain the unaffected approval: %+v", cases)
	}
	state = confirmRequirement(t, root, pkg, state)
	state = recordProductReview(t, root, pkg, state)
	state = recordReadiness(t, root, pkg, state)
	worktree := createQAWorktree(t, root, state)
	updatedRequirement, err := os.ReadFile(filepath.Join(root, "requirements.md"))
	if err != nil {
		t.Fatal(err)
	}
	writeTestFile(t, filepath.Join(worktree, "requirements.md"), string(updatedRequirement))
	state, err = RegisterQAWorktree(root, pkg, state.RunID, worktree)
	if err != nil {
		t.Fatal(err)
	}
	design := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
	state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{{CaseID: oldCases[1].ID, Mode: "blackbox", Description: "second behavior", Procedure: "run the second public operation", Oracle: "the revised second result matches", AcceptanceCriteria: []string{"AC-009"}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", "selective-thaw-reviewer", "blackbox")
	decisions, options := guaranteeReviewInputs(state, "blackbox")
	if len(decisions) != 1 || decisions[0].CaseID != oldCases[1].ID {
		t.Fatalf("unaffected case was reopened for review: %+v", decisions)
	}
	state, err = RecordQAReview(root, pkg, state.RunID, review, decisions, "", nil, options)
	if err != nil {
		t.Fatal(err)
	}
	if state.QACaseSets["blackbox"].Status != caseSetFrozen || state.QACaseSets["blackbox"].Version == oldVersion || state.qaModeCases("blackbox")[0].ApprovedSource != oldCases[0].ApprovedSource {
		t.Fatalf("selective revision did not refreeze while preserving the unaffected approval: %+v", state.QACaseSets)
	}
}

func TestRepairRepeatWaveAfterOneTargetPass(t *testing.T) {
	root, pkg, state := blackboxFrozenDelivery(t, "repeat-wave-pass", []string{blackboxQAID, "quality"})
	wave, err := PrepareReviewWave(root, pkg, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	state = recordGateResult(t, root, pkg, state, "quality", "repeat-wave-quality", "PASS", "", nil)
	count := len(state.Dispatches)
	repeated, err := PrepareReviewWave(root, pkg, state.RunID)
	if err != nil || repeated.ID != wave.ID || repeated.ExpectedDigest != wave.ExpectedDigest || repeated.Expected["gate:quality"].Status != "PASS" {
		t.Fatalf("completed target changed wave identity: %+v %v", repeated, err)
	}
	state, _ = LoadRunState(root, state.RunID)
	if len(state.Dispatches) != count {
		t.Fatal("repeat wave created a duplicate dispatch")
	}
}

func TestRepairFirstWaveGateUsesReadinessBarrier(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "first-wave-barrier"), "custom", []string{whiteboxQAID, "quality"})
	state = advanceGuaranteeSnapshot(t, root, pkg, state, "first-wave-S1")
	before := stateBytes(t, root, state.RunID)
	if _, err := PrepareGate(root, pkg, state.RunID, "quality", false, ""); err == nil || !strings.Contains(err.Error(), "FROZEN") || stateBytes(t, root, state.RunID) != before {
		t.Fatalf("gate entered before first-wave readiness: %v", err)
	}
}

// Existing tests that exercise a successful rollback now provide the same
// bound impact authorization as a normal caller. Negative tests still call
// UpdateRequirement directly so this helper cannot hide a missing guard.
func repairImpactOptions(t *testing.T, root, pkg, runID string) RequirementUpdateOptions {
	t.Helper()
	impact := RequirementImpactInput{Scope: "the revised requirement changes the fixture's confirmed scope and rolls product review back"}
	preview, err := PreviewRequirementImpact(root, pkg, runID, "", nil, "", impact)
	if err != nil {
		t.Fatal(err)
	}
	return RequirementUpdateOptions{ImpactAuthorization: &RequirementImpactAuthorization{PreviewDigest: preview.Digest, StateRevision: preview.StateRevision, UserConfirmation: "isolated regression fixture: user confirms the displayed scope change and rollback", Impact: impact}}
}

func repairExplicitReview(t *testing.T, root, pkg, runID, target string) string {
	t.Helper()
	if _, err := PrepareAction(root, pkg, runID, target, "", true, "isolated regression fixture explicitly requests this additional review"); err != nil {
		t.Fatal(err)
	}
	state, err := LoadRunState(root, runID)
	if err != nil {
		t.Fatal(err)
	}
	id := openDispatchID(state, "action", target)
	if _, err := ClaimDispatch(root, pkg, runID, id, "regression-reviewer-"+id); err != nil {
		t.Fatal(err)
	}
	return id
}

func repairDocumentJSON(t *testing.T, marker string, bindings map[string]string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "references", "formal-flow.md"))
	if err != nil {
		t.Fatal(err)
	}
	_, tail, ok := strings.Cut(string(data), "<!-- "+marker+" -->")
	if !ok {
		t.Fatal("document example marker missing: " + marker)
	}
	_, tail, ok = strings.Cut(tail, "```json\n")
	if !ok {
		t.Fatal("JSON example missing")
	}
	body, _, ok := strings.Cut(tail, "\n```")
	if !ok {
		t.Fatal("JSON example is not closed")
	}
	for key, value := range bindings {
		body = strings.ReplaceAll(body, "${"+key+"}", value)
	}
	path := filepath.Join(t.TempDir(), "input.json")
	writeTestFile(t, path, body)
	return path
}

func TestRepairDocumentExamplesUseProductionParsersAndBindings(t *testing.T) {
	t.Run("adjudication", func(t *testing.T) {
		root, pkg, state := blackboxFrozenDelivery(t, "document-adjudication", []string{blackboxQAID, "quality"})
		wave, err := PrepareReviewWave(root, pkg, state.RunID)
		if err != nil {
			t.Fatal(err)
		}
		dispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
		state, err = RecordQAExecution(root, pkg, state.RunID, dispatch, passingExecution(state.qaModeCases("blackbox")), "")
		if err != nil {
			t.Fatal(err)
		}
		state = recordGateResult(t, root, pkg, state, "quality", "document-quality", "PASS", "", []FindingInput{{Severity: "P2", Message: "duplicate output"}})
		var findingID string
		for id := range state.PostReviewFindings {
			findingID = id
		}
		path := repairDocumentJSON(t, "adjudicate-wave-input-example", map[string]string{"wave_id": wave.ID, "state_revision": state.StateIntegrity, "finding_id": findingID})
		input, err := LoadReviewWaveAdjudication(path)
		if err != nil {
			t.Fatal(err)
		}
		state, err = AdjudicateReviewWave(root, pkg, state.RunID, input)
		if err != nil || state.ReviewWaves[wave.ID].Status != reviewWaveReadyForRepair {
			t.Fatalf("documented JSON was not accepted: %v", err)
		}
	})
	t.Run("review delta", func(t *testing.T) {
		root, pkg, state, findingID := confirmedStartFindingRun(t, "document-review-delta")
		writeTestFile(t, resolveFromRoot(root, state.RequirementSource), testRequirementDocument("Revise the confirmed responsibility assignment."))
		path := repairDocumentJSON(t, "review-delta-input-example", map[string]string{"finding_id": findingID})
		input, err := LoadReviewDeltaPackage(path)
		if err != nil {
			t.Fatal(err)
		}
		before := stateBytes(t, root, state.RunID)
		preview, err := PreviewReviewDelta(root, pkg, state.RunID, "", nil, input)
		if err != nil || preview.UserConfirmation != "" || preview.Digest == "" || stateBytes(t, root, state.RunID) != before {
			t.Fatalf("documented preview was not read-only and unconfirmed: %+v %v", preview, err)
		}
		if _, err := UpdateRequirement(root, pkg, state.RunID, "", true, "preserved", nil, RequirementUpdateOptions{ReviewDelta: &preview}); err == nil {
			t.Fatal("a preview was accepted as confirmation")
		}
		preview.UserConfirmation = "isolated regression fixture: user confirms the displayed delta"
		data, err := json.Marshal(preview)
		if err != nil {
			t.Fatal(err)
		}
		writeTestFile(t, path, string(data))
		confirmed, err := LoadReviewDeltaPackage(path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := UpdateRequirement(root, pkg, state.RunID, "", true, "preserved", nil, RequirementUpdateOptions{ReviewDelta: &confirmed}); err != nil {
			t.Fatal(err)
		}
	})
}

func TestRepairSealRejectsUnadjudicatedAndUnfinishedP2(t *testing.T) {
	root, pkg, state := blackboxFrozenDelivery(t, "seal-p2-boundary", []string{blackboxQAID, "quality"})
	if _, err := PrepareReviewWave(root, pkg, state.RunID); err != nil {
		t.Fatal(err)
	}
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	var err error
	state, err = RecordQAExecution(root, pkg, state.RunID, dispatch, passingExecution(state.qaModeCases("blackbox")), "")
	if err != nil {
		t.Fatal(err)
	}
	state = recordGateResult(t, root, pkg, state, "quality", "seal-p2-quality", "PASS", "", []FindingInput{{Severity: "P2", Message: "remove duplicate branch"}})
	for _, phase := range []string{"unadjudicated", "awaiting repair"} {
		before := stateBytes(t, root, state.RunID)
		if _, err := Seal(root, pkg, state.RunID, nil, false, ""); err == nil || !strings.Contains(err.Error(), "before Seal") || stateBytes(t, root, state.RunID) != before {
			t.Fatalf("%s P2 reached Seal or partially wrote: %v", phase, err)
		}
		if phase == "unadjudicated" {
			state = repairAdjudicateAll(t, root, pkg, state)
		}
	}
}

func TestRepairPreviewSurvivesReadingTimeButNotStateChange(t *testing.T) {
	root, pkg, state := blackboxFrozenDelivery(t, "preview-reading-time", []string{blackboxQAID, "quality"})
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "qa-execution", "blackbox")
	results := passingExecution(state.qaModeCases("blackbox"))
	results[0].Outcome = "FAIL"
	var err error
	state, err = RecordQAExecution(root, pkg, state.RunID, dispatch, results, "")
	if err != nil {
		t.Fatal(err)
	}
	state = recordGateResult(t, root, pkg, state, "quality", "preview-reading-quality", "PASS", "", nil)
	state, err = mutateRun(root, state.RunID, func(s *RunState) error {
		s.CompletedReviewWaves = automaticReviewWaveLimit
		s.StartedAt = time.Now().Add(-time.Minute).Format(time.RFC3339Nano)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	estimate := RepairRerunEstimate{BuildTargets: []string{"go build ./..."}, TestTargets: []string{"go test ./..."}, QACases: map[string][]string{"blackbox": {results[0].CaseID}}}
	preview, err := PreviewRepairAuthorization(root, pkg, state.RunID, estimate)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	after, err := PreviewRepairAuthorization(root, pkg, state.RunID, estimate)
	if err != nil || after.Runtime == preview.Runtime || after.Digest != preview.Digest || after.StateRevision != preview.StateRevision {
		t.Fatalf("reading time changed authorization binding: %+v %+v %v", preview, after, err)
	}
	state, err = AuthorizeExtraRepair(root, pkg, state.RunID, 1, []QAScopeInput{{Mode: "blackbox", Decision: "AFFECTED", CaseIDs: []string{results[0].CaseID}, Reason: "the first operation is affected"}}, RepairAuthorizationInput{PreviewDigest: preview.Digest, StateRevision: preview.StateRevision, Estimate: estimate})
	if err != nil {
		t.Fatal(err)
	}
	newPreview := repairAuthorizationPreviewFromState(state, estimate)
	if err := validateRepairAuthorizationPreview(state, newPreview, RepairAuthorizationInput{PreviewDigest: preview.Digest, StateRevision: preview.StateRevision}); err == nil {
		t.Fatal("a real authorization state change left the old preview usable")
	}
}

func TestRepairReadinessResultDoesNotMintAnotherTask(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "readiness-result-identity"))
	state = recordProductReview(t, root, pkg, state)
	dispatch := prepareDispatch(t, root, pkg, state.RunID, "start-readiness")
	state, err := RecordAction(root, pkg, state.RunID, "start-readiness", dispatch, "FAIL", "", []FindingInput{completeGovernanceFinding("the declared dependencies conflict", false)}, false, "", fixtureOperatorVerification())
	if err != nil {
		t.Fatal(err)
	}
	before := stateBytes(t, root, state.RunID)
	if _, err := PrepareAction(root, pkg, state.RunID, "start-readiness", "", false, ""); err == nil || !strings.Contains(err.Error(), "authoritative FAIL") || stateBytes(t, root, state.RunID) != before {
		t.Fatalf("unchanged readiness FAIL created new work: %v", err)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "product-review", "", true, "  "); err == nil || !strings.Contains(err.Error(), "user-reason") || stateBytes(t, root, state.RunID) != before {
		t.Fatalf("empty override reason was manufactured: %v", err)
	}
}

func TestRepairExplicitReviewInvalidRetryKeepsObligation(t *testing.T) {
	root, pkg := workflowFixture(t)
	state := confirmRequirement(t, root, pkg, mustStart(t, root, pkg, "explicit-invalid-retry"))
	state = recordProductReview(t, root, pkg, state)
	dispatch := repairExplicitReview(t, root, pkg, state.RunID, "product-review")
	if _, err := RecordAction(root, pkg, state.RunID, "product-review", dispatch, "FAIL", "", []FindingInput{{Severity: "P1", Message: "missing finding evidence"}}, false, "", fixtureOperatorVerification()); err == nil {
		t.Fatal("incomplete result was accepted")
	}
	state, err := LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	prior := state.Dispatches[dispatch]
	if prior.ResultStatus != "REJECTED_INVALID_RESULT" {
		t.Fatalf("invalid-result retry was not available: %+v", prior)
	}
	if _, err := PrepareAction(root, pkg, state.RunID, "product-review", "", false, ""); err != nil {
		t.Fatalf("authorized review retry was blocked: %v", err)
	}
	state, _ = LoadRunState(root, state.RunID)
	retry := state.Dispatches[openDispatchID(state, "action", "product-review")]
	if retry.TaskKey != prior.TaskKey || retry.Attempt != prior.Attempt+1 || !retry.UserRequested || state.PreDevelopmentReviewSeries["product-review"].Completed != 1 {
		t.Fatalf("authorized invalid retry changed the obligation or consumed a round: %+v %+v", prior, retry)
	}
}

func TestRepairProductObligationFailCanCarryLimitedAdvisory(t *testing.T) {
	for _, severity := range []string{"P2", "P3"} {
		t.Run(severity, func(t *testing.T) {
			root, pkg, state := startGuaranteeRun(t, "obligation-advisory-"+strings.ToLower(severity), false)
			dispatch := prepareDispatch(t, root, pkg, state.RunID, "product-review")
			state, err := RecordAction(root, pkg, state.RunID, "product-review", dispatch, "PASS", "", []FindingInput{{Severity: severity, Message: "the documented stderr detail lacks a separate AC", Locations: []string{"requirements.md:1"}}}, false, "", fixtureOperatorVerification(), ReviewItemInput{Key: guaranteeProductReviewKey("REQ-001"), Status: "FAIL", Reason: "stderr detail has no separate AC"})
			if err != nil || state.Actions["product-review"].Status != "PASS" || state.Actions["product-review"].Findings[0].Severity != severity || state.ReviewItemsByAction["product-review"][guaranteeProductReviewKey("REQ-001")].Status != "FAIL" {
				t.Fatalf("item FAIL changed advisory severity or rejected PASS: %+v %v", state.Actions["product-review"], err)
			}
		})
	}
}

func TestRepairBlackboxFindingAndSuggestionInputsUseIsolation(t *testing.T) {
	for _, severity := range []string{"P1", "P2", "P3"} {
		t.Run(severity, func(t *testing.T) {
			root, pkg := workflowFixture(t)
			state := confirmAndRoute(t, root, pkg, mustStart(t, root, pkg, "qa-location-"+strings.ToLower(severity)), "custom", []string{blackboxQAID})
			var err error
			state, err = RegisterQAWorktree(root, pkg, state.RunID, createQAWorktree(t, root, state))
			if err != nil {
				t.Fatal(err)
			}
			design := prepareDispatch(t, root, pkg, state.RunID, "qa-design", "blackbox")
			state, err = RecordQADesign(root, pkg, state.RunID, design, []QACaseInput{{Mode: "blackbox", Description: "public result", Procedure: "run the documented command", Oracle: "the documented result"}}, "")
			if err != nil {
				t.Fatal(err)
			}
			review := prepareAndClaim(t, root, pkg, state.RunID, "qa-review", state.RunID+"-reviewer", "blackbox")
			finding := completeGovernanceFinding("check the command exit code in the oracle", false)
			finding.Severity = severity
			finding.Locations = []string{".gates/cases/blackbox.md:1"}
			state, err = RecordQAReview(root, pkg, state.RunID, review, passingReviewDecisions(state), "", []FindingInput{finding}, QAReviewRecordOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if severity == "P1" {
				if state.qaReview("blackbox").Status != "FAIL" {
					t.Fatal("set-level P1 did not fail review")
				}
				return
			}
			prompt, err := PrepareAction(root, pkg, state.RunID, "qa-design", "blackbox", false, "")
			suggestion := state.QACaseSets["blackbox"].Suggestions[0]
			if err != nil || !strings.Contains(prompt, suggestion.ID) || !strings.Contains(prompt, suggestion.Message) || !strings.Contains(prompt, finding.Locations[0]) || !strings.Contains(prompt, "--per-suggestion") {
				t.Fatalf("suggestion dispatch omitted its exact input: %v\n%s", err, prompt)
			}
		})
	}
}
