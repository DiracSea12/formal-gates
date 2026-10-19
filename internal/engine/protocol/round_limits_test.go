package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/settings"
)

func TestWorkBindingsCarryRoundIdentityOnlyForRoundBasedResponsibilities(t *testing.T) {
	binding := WorkBinding{Task: runtime.TaskKey{Node: "development", Step: "implement"}, RequirementRevision: "req", Generation: 1}
	unsigned, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(unsigned, []byte("round")) {
		t.Fatal("a non-round responsibility carried round fields", string(unsigned))
	}
	binding.RoundExecution = review.RoundExecution{Revision: 2, Completed: 1}
	signed, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	var restored WorkBinding
	if err := json.Unmarshal(signed, &restored); err != nil || restored != binding || !bytes.Contains(signed, []byte(`"roundRevision":2`)) || !bytes.Contains(signed, []byte(`"roundsCompleted":1`)) {
		t.Fatal("signed binding lost its value-level round identity", string(signed), err)
	}
}

func TestLibraryRoundLimitsExtendAutomaticReviewThenRequireAuthorization(t *testing.T) {
	e, dir := canonicalReviewEngine(t)
	library := bindLibrary(t, e, settings.DefaultConfiguration())
	issueReady(t, e)
	action := pendingActionByStep(t, e, "review.worker")
	for round := 1; round <= 3; round++ {
		snapshot := e.LoadMustSucceed(t)
		submitVerifiedReviewResult(t, e, fmt.Sprintf("limit-result-%d", round), action,
			reviewEvidenceAt(review.ProductReview, action, snapshot.State.RequirementRevision(), "FAIL", "requirement needs correction", []review.Finding{{Severity: "P1", Message: "normal acceptance requirement is incomplete"}}))
		if round == 3 {
			redoProductReview(t, e, "limits-fourth")
			exhausted := e.LoadMustSucceed(t)
			var offer string
			for id, ask := range exhausted.State.PendingAsks {
				if ask.RoundGrant != nil && !ask.Resolved {
					offer = id
				}
			}
			if offer == "" {
				t.Fatal("default capacity exhaustion did not offer an additional round")
			}
			token, err := e.Freshness(RequestID(offer))
			if err != nil {
				t.Fatal(err)
			}
			library.value.RoundLimits = map[string]int{"product-review": 5, "tech-review": 1}
			decision, err := NewReviewDecideEvent("obsolete-round-grant", RequestID(offer), token, "grant", ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{Action: review.ProductReview, Reason: "review corrected requirement"}})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := e.Submit(decision, Observation{}); err == nil {
				t.Fatal("approving an obsolete offer did not recompute the saved limit")
			}
			if _, _, err := e.Drive(Observation{}); err != nil {
				t.Fatal(err)
			}
			extended := e.LoadMustSucceed(t)
			if !extended.State.PendingAsks[offer].Resolved {
				t.Fatal("obsolete grant offer blocked the new automatic capacity")
			}
			for fourth := 4; fourth <= 5; fourth++ {
				action = pendingActionByStep(t, e, "review.worker")
				snapshot = e.LoadMustSucceed(t)
				series := snapshot.State.ReviewSeries.ProductReview
				if review.AuthorizationOfDispatch(series, action) != "" || series.DispatchRounds[action].Completed != fourth-1 {
					t.Fatal("extended automatic round borrowed a user authorization")
				}
				submitVerifiedReviewResult(t, e, fmt.Sprintf("limit-result-%d", fourth), action,
					reviewEvidenceAt(review.ProductReview, action, snapshot.State.RequirementRevision(), "FAIL", "requirement needs correction", []review.Finding{{Severity: "P1", Message: "normal acceptance requirement is incomplete"}}))
				if fourth == 4 {
					redoProductReview(t, e, "limits-fifth")
				}
			}
			break
		}
		redoProductReview(t, e, fmt.Sprintf("limits-%d", round+1))
		action = pendingActionByStep(t, e, "review.worker")
	}
	library.value.RoundLimits["product-review"] = 1
	if _, _, err := e.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	redoProductReview(t, e, "limits-sixth")
	snapshot := e.LoadMustSucceed(t)
	series := snapshot.State.ReviewSeries.ProductReview
	if series.Completed != 5 || series.AutomaticLimit != 1 || len(series.LimitHistory) != 3 || len(series.Authorizations) != 0 || len(snapshot.State.PendingActions) != 0 {
		t.Fatalf("lowered limit changed history or allowed a sixth automatic round: %+v", series)
	}
	if snapshot.State.ReviewSeries.StartReadiness.AutomaticLimit != 1 {
		t.Fatal("technical review ignored its independent configured value")
	}
	reopened, err := New(mustStore(t, dir), e.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := review.Validate(snapshot.State.RunID, reopened.LoadMustSucceed(t).State.ReviewSeries); err != nil {
		t.Fatal("reopening rejudged historical rounds under the reduced limit", err)
	}
}

func TestConfiguredTechnicalReviewConsumesItsOwnAutomaticLimit(t *testing.T) {
	e, _ := canonicalReviewEngine(t)
	bindLibrary(t, e, settings.Configuration{RoundLimits: map[string]int{"product-review": 64, "tech-review": 1}})
	action := issueReady(t, e)[0]
	submitVerifiedReviewResult(t, e, "technical-product-pass", action, plannedReviewEvidence(review.ProductReview, action, "", "PASS", "clean", nil))
	action = pendingActionByStep(t, e, "review.readiness")
	submitVerifiedReviewResult(t, e, "technical-first", action, plannedReviewEvidence(review.StartReadiness, action, "", "FAIL", "technical acceptance needs correction", []review.Finding{{Severity: "P1", Category: "acceptance-entry", Message: "normal technical acceptance entry is missing"}}))
	snapshot := e.LoadMustSucceed(t)
	askAndDecideRequirement(t, e, "technical-redo", "proceed", &RequirementDecisionPayload{
		BaseRevision: snapshot.State.RequirementRevision(), CandidateRevision: "technical-revised", DeltaDigest: "sha256:technical-delta", ImpactDigest: "sha256:technical-impact",
		Source: RequirementSourceUserInitiated, Classification: RequirementClassificationChanged, RedoStartReadiness: true, Reason: "review corrected technical acceptance",
	})
	snapshot = e.LoadMustSucceed(t)
	if len(snapshot.State.PendingActions) != 0 || snapshot.State.ReviewSeries.StartReadiness.RemainingAutomatic != 0 || snapshot.State.ReviewSeries.ProductReview.RemainingAutomatic != 63 {
		t.Fatal("technical review borrowed product review capacity")
	}
	found := false
	for _, ask := range snapshot.State.PendingAsks {
		found = found || !ask.Resolved && ask.RoundGrant != nil && ask.RoundGrant.Action == review.StartReadiness && ask.RoundGrant.AutomaticLimit == 1
	}
	if !found {
		t.Fatal("technical limit exhaustion did not produce its own grant offer")
	}
}

func TestResetRebuildsReviewSeriesWithAppliedLimits(t *testing.T) {
	e, _ := canonicalReviewEngine(t)
	bindLibrary(t, e, settings.Configuration{RoundLimits: map[string]int{"product-review": 1, "tech-review": 64}})
	request, err := NewRequestEvent("round-limit-reset", ControlReset, AskOption{ID: "proceed", Label: "Reset run"})
	if err != nil {
		t.Fatal(err)
	}
	request = request.WithResetPreview(e.LoadMustSucceed(t).State.ResetPreview())
	if _, err := e.Submit(request, Observation{}); err != nil {
		t.Fatal(err)
	}
	token, err := e.Freshness(RequestID(request.ID))
	if err != nil {
		t.Fatal(err)
	}
	decision, err := NewDecideEvent("round-limit-reset-decision", RequestID(request.ID), token, "proceed")
	if err != nil {
		t.Fatal(err)
	}
	state := e.LoadMustSucceed(t).State
	decision.Decide.Reset = &ResetDecision{PreviewDigest: state.PendingAsks[string(request.ID)].Reset.Digest, Reason: "rebuild review obligations"}
	accepted, err := e.Submit(decision, Observation{})
	if err != nil {
		t.Fatal(err)
	}
	state = e.LoadMustSucceed(t).State
	if state.ReviewSeries.ProductReview.AutomaticLimit != 1 || state.ReviewSeries.StartReadiness.AutomaticLimit != 64 || !reflect.DeepEqual(state.ReviewSeries.ProductReview.LimitHistory, []review.LimitInterval{{Value: 1, Revision: accepted.Revision}}) || state.ReviewSeries.ProductReview.Completed != 0 {
		t.Fatal("reset restored default limits instead of the applied configuration")
	}
}

func TestReviewRetryRetainsLimitAdmissionAfterDecrease(t *testing.T) {
	cd, reg := singleCarrierFixture(t)
	e, _ := reviewEngine(t, cd, reg)
	library := bindLibrary(t, e, settings.DefaultConfiguration())
	exhaustProductReviewCapacity(t, e)
	redoProductReview(t, e, "retry-fourth")
	library.value.RoundLimits = map[string]int{"product-review": 5}
	if _, _, err := e.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	action := pendingActionID(t, e)
	snapshot := e.LoadMustSucceed(t)
	admission := snapshot.State.ReviewSeries.ProductReview.DispatchRounds[action]
	pending := snapshot.State.PendingActions[action]
	library.value.RoundLimits["product-review"] = 1
	if _, _, err := e.Drive(Observation{}); err != nil {
		t.Fatal(err)
	}
	submitSpawnReceipt(t, e, "retry-source", action)
	event, err := NewWorkerResultEvent("retry-transient", action, testProvider, OutcomeRuntimeError, "sha256:transient", authoring.FailureTransientEngine)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.Submit(event, Observation{}); err != nil {
		t.Fatal(err)
	}
	retry := pendingActionID(t, e)
	snapshot = e.LoadMustSucceed(t)
	if retry == action || snapshot.State.PendingActions[retry].Task != pending.Task || snapshot.State.ReviewSeries.ProductReview.DispatchRounds[retry] != admission || snapshot.State.ReviewSeries.ProductReview.Issued != 4 {
		t.Fatal("transient retry acquired a new semantic round or a new limit")
	}
	submitVerifiedReviewResult(t, e, "retry-final-result", retry, reviewEvidenceAt(review.ProductReview, retry, snapshot.State.RequirementRevision(), "PASS", "corrected", nil))
	if got := e.LoadMustSucceed(t).State.ReviewSeries.ProductReview; got.Completed != 4 || len(got.Authorizations) != 0 {
		t.Fatal("retry failed to consume its originally admitted automatic round")
	}
}

func TestReviewRoundAdmissionSurvivesLimitReductionAndRecovery(t *testing.T) {
	for _, authorized := range []bool{false, true} {
		t.Run(fmt.Sprintf("authorized=%t", authorized), func(t *testing.T) {
			cd, reg := singleCarrierFixture(t)
			e, dir := reviewEngine(t, cd, reg)
			library := bindLibrary(t, e, settings.DefaultConfiguration())
			exhaustProductReviewCapacity(t, e)
			redoProductReview(t, e, "inflight-fourth")
			if authorized {
				askAndDecide(t, e, ControlReviewRoundGrant, "inflight-grant", grantOptions(), "grant", &ReviewDecisionPayload{RoundGrant: &RoundGrantDecision{Action: review.ProductReview, Reason: "review corrected requirement"}})
			} else {
				library.value.RoundLimits = map[string]int{"product-review": 5}
				if _, _, err := e.Drive(Observation{}); err != nil {
					t.Fatal(err)
				}
			}
			action := pendingActionID(t, e)
			before := e.LoadMustSucceed(t)
			admission := before.State.ReviewSeries.ProductReview.DispatchRounds[action]
			authorization := review.AuthorizationOfDispatch(before.State.ReviewSeries.ProductReview, action)
			library.value.RoundLimits = map[string]int{"product-review": 1}
			if _, _, err := e.Drive(Observation{}); err != nil {
				t.Fatal(err)
			}
			observedStoppedWorker(t, e, action)
			plan, _, err := e.RecoverAttempt(action, Interruption{CauseKnown: true}, Observation{})
			if err != nil || plan.Action != RecoveryNewAttempt {
				t.Fatal("could not recover the originally admitted review", plan, err)
			}
			reopened, err := New(mustStore(t, dir), e.cfg)
			if err != nil {
				t.Fatal(err)
			}
			snapshot := reopened.LoadMustSucceed(t)
			replacement := snapshot.State.ObsoleteActions[action].ReplacedBy
			if snapshot.State.ReviewSeries.ProductReview.DispatchRounds[replacement] != admission || review.AuthorizationOfDispatch(snapshot.State.ReviewSeries.ProductReview, replacement) != authorization {
				t.Fatal("recovery or reopen changed the logical round's limit or authorization")
			}
			evidence := reviewEvidenceAt(review.ProductReview, replacement, snapshot.State.RequirementRevision(), "PASS", "corrected", nil)
			evidence.AuthorizationID = authorization
			submitVerifiedReviewResult(t, reopened, "recovered-limit-result", replacement, evidence)
			after := reopened.LoadMustSucceed(t).State.ReviewSeries.ProductReview
			if after.Completed != 4 || after.AutomaticLimit != 1 || after.ExtraConsumed != map[bool]int{false: 0, true: 1}[authorized] {
				t.Fatal("reduced limit rejected or reassigned the recovered result")
			}
		})
	}
}

func failedQAReview() CaseReviewEvidence {
	return CaseReviewEvidence{Review: coverage.ReviewResult{CaseDecisions: []coverage.ReviewDecision{{Status: coverage.StatusFail}}}}
}

func TestQAFailureLimitsApplyAtDefaultAndRangeBoundaries(t *testing.T) {
	cd := compiledCanonicalWorkflow(t)
	for _, limit := range []int{1, 3, 64} {
		for _, mode := range []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox, coverage.ReviewMerge} {
			t.Run(fmt.Sprintf("%s/%d", mode, limit), func(t *testing.T) {
				set := &CaseSet{Status: "DRAFT", LimitHistory: []review.LimitInterval{{Value: limit, Revision: 1}}}
				s := &State{Validation: &Validation{Sets: map[coverage.ReviewKind]*CaseSet{mode: set}}}
				for n := 0; n < limit-1; n++ {
					set.ReviewHistory = append(set.ReviewHistory, failedQAReview())
				}
				if s.prewaveBlocked(mode, "engine.qa.design.v6", cd) {
					t.Fatal("QA design stopped before its configured FAIL limit")
				}
				set.ReviewHistory = append(set.ReviewHistory, failedQAReview())
				if !s.prewaveBlocked(mode, "engine.qa.design.v6", cd) {
					t.Fatal("QA design crossed its configured FAIL limit")
				}
				set.ReviewHistory = append(set.ReviewHistory, CaseReviewEvidence{Review: coverage.ReviewResult{}})
				if s.prewaveBlocked(mode, "engine.qa.design.v6", cd) {
					t.Fatal("a real PASS did not end the continuous FAIL streak")
				}
			})
		}
	}
}

func TestIssuedQAReviewRetainsItsLimitIdentityAfterReduction(t *testing.T) {
	e, s, _ := guardPipelineFixture(t)
	mode := coverage.ReviewBlackbox
	set := s.Validation.Sets[mode]
	set.Status = "REVIEW"
	set.Manifest.Binding = coverage.ManifestBinding{SourceBinding: s.coverageContract(nil).RequiredSources.Binding, ReviewKind: mode, RouteScope: s.validationRouteDigest()}
	set.Version = digestOfCanonical(set.Manifest)
	set.ReviewHistory = []CaseReviewEvidence{failedQAReview(), failedQAReview()}
	configuration := settings.Configuration{RoundLimits: map[string]int{"qa-blackbox": 5}}
	s.applyRoundLimits(configuration, 1)
	task := s.reviewTask(mode, s.Validation.Generation, set.Version, e.cfg.Definition)
	s.DispatchIssueRevisions["qa-review"] = 2
	s.PendingActions["qa-review"] = PendingAction{ActionID: "qa-review", Task: task}
	s.Attempts[task] = Attempt{IssuedAction: decision.IssuedAction{ActionID: "qa-review", Task: task, Step: task.Step}, ID: "review-attempt"}
	e.bindBusinessAttempt(s, task)
	binding := s.DispatchBindings["qa-review"]
	if binding.RoundExecution.Revision != 2 || binding.RoundExecution.Completed != 2 {
		t.Fatal("QA review dispatch did not carry its admission identity")
	}
	configuration.RoundLimits["qa-blackbox"] = 1
	s.applyRoundLimits(configuration, 3)
	if s.prewaveBlocked(mode, "engine.qa.review.rationale.v2", e.cfg.Definition) || s.DispatchBindings["qa-review"] != binding {
		t.Fatal("decreased QA limit withdrew or rebound an issued review")
	}
	if err := s.verifyValidationRoundLimits(e.cfg.Definition); err != nil {
		t.Fatal("QA integrity rejudged an issued automatic review after reduction", err)
	}
}

func TestQADesignLimitsUseIndependentFailureStreaksAndWhiteboxForMerge(t *testing.T) {
	cd := compiledCanonicalWorkflow(t)
	s := &State{Validation: &Validation{Generation: 1, Sets: map[coverage.ReviewKind]*CaseSet{}}, PendingAsks: map[string]PendingAsk{}, Decisions: map[string]RecordedDecision{}}
	s.ReviewSeries = review.NewSet()
	configuration := settings.Configuration{RoundLimits: map[string]int{"qa-blackbox": 1, "qa-whitebox": 64}}
	s.AppliedConfigurations = []AppliedConfiguration{{Revision: 1, Configuration: settings.CloneConfiguration(configuration)}}
	for _, mode := range []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox, coverage.ReviewMerge} {
		s.Validation.Sets[mode] = &CaseSet{Status: "DRAFT", ReviewHistory: []CaseReviewEvidence{failedQAReview()}}
	}
	s.applyRoundLimits(configuration, 1)
	for _, mode := range []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox, coverage.ReviewMerge} {
		if blocked := s.prewaveBlocked(mode, "engine.qa.design.v6", cd); blocked != (mode == coverage.ReviewBlackbox) {
			t.Fatal("QA fail limits shared values or merge did not follow whitebox", mode, blocked)
		}
	}
	s.ensureValidationAsk(ValidationRequest{Kind: "QA_REVIEW", Mode: coverage.ReviewBlackbox, Target: s.qaReviewTarget(coverage.ReviewBlackbox)}, 2)
	var askID string
	for id := range s.PendingAsks {
		askID = id
	}
	configuration.RoundLimits["qa-blackbox"] = 5
	s.applyRoundLimits(configuration, 3)
	s.reconcileValidationRemedyAsks(3)
	if !s.PendingAsks[askID].Resolved || s.prewaveBlocked(coverage.ReviewBlackbox, "engine.qa.design.v6", cd) {
		t.Fatal("raising QA capacity retained the obsolete authorization ask")
	}
	for _, mode := range []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox, coverage.ReviewMerge} {
		if qaFailureStreak(s.Validation.Sets[mode]) != 1 {
			t.Fatal("saving configuration reset a failure streak")
		}
	}
	configuration.RoundLimits["qa-whitebox"] = 1
	s.applyRoundLimits(configuration, 4)
	for _, mode := range []coverage.ReviewKind{coverage.ReviewWhitebox, coverage.ReviewMerge} {
		if !s.prewaveBlocked(mode, "engine.qa.design.v6", cd) || len(s.Validation.Sets[mode].LimitHistory) != 2 {
			t.Fatal("whitebox or merge failed to apply the decreased limit")
		}
	}
}

func TestRepairLimitChangesRetireOnlyUnneededOffersAndRetainGrantedWork(t *testing.T) {
	e, s := guardRepairFixture(t)
	s.ReviewSeries = review.NewSet()
	wave := &s.Validation.Waves[0]
	wave.Counted = true
	configuration := settings.Configuration{RoundLimits: map[string]int{"repair": 1}}
	s.AppliedConfigurations = []AppliedConfiguration{{Revision: 1, Configuration: settings.CloneConfiguration(configuration)}}
	s.applyRoundLimits(configuration, 1)
	work := s.nextRepairWork()
	if work == nil || !s.repairRoundBlocked(work) {
		t.Fatal("one counted wave did not exhaust limit 1")
	}
	s.ensureValidationAsk(ValidationRequest{Kind: "REPAIR", Target: wave.ID}, 2)
	var askID string
	for id, ask := range s.PendingAsks {
		if ask.Validation != nil && ask.Validation.Kind == "REPAIR" {
			askID = id
		}
	}
	face := s.repairAskDetail(*s.PendingAsks[askID].Validation)
	if face.AutomaticLimit != 1 || face.WavesUsed != 1 || face.RemainingAutomatic != 0 {
		t.Fatal("repair authorization summary ignored the configured limit", face)
	}
	configuration.RoundLimits["repair"] = 5
	s.AppliedConfigurations = append(s.AppliedConfigurations, AppliedConfiguration{Revision: 3, Configuration: settings.CloneConfiguration(configuration)})
	s.applyRoundLimits(configuration, 3)
	s.reconcileValidationRemedyAsks(3)
	if !s.PendingAsks[askID].Resolved || s.repairRoundBlocked(work) || s.repairAskDetail(ValidationRequest{Target: wave.ID}).RemainingAutomatic != 4 {
		t.Fatal("raised repair limit retained a blocking authorization request")
	}
	configuration.RoundLimits["repair"] = 1
	s.AppliedConfigurations = append(s.AppliedConfigurations, AppliedConfiguration{Revision: 4, Configuration: settings.CloneConfiguration(configuration)})
	s.applyRoundLimits(configuration, 4)
	s.Decisions[askID] = RecordedDecision{Choice: "approve"}
	if s.repairRoundBlocked(work) {
		t.Fatal("decreased repair limit revoked an exact existing grant")
	}
	binding := WorkBinding{Task: work.task, WaveID: wave.ID, RoundExecution: review.RoundExecution{Revision: 3, Completed: 1}}
	s.DispatchBindings["issued-repair"] = binding
	if err := s.verifyValidationRoundLimits(e.cfg.Definition); err != nil {
		t.Fatal("historical repair admission was checked against the latest limit", err)
	}
	delete(s.Decisions, askID)
	s.PendingActions["issued-repair"] = PendingAction{ActionID: "issued-repair", Task: work.task}
	if s.repairRoundBlocked(work) || s.DispatchBindings["issued-repair"] != binding {
		t.Fatal("limit reduction withdrew or rebound an admitted automatic repair")
	}
	delete(s.PendingActions, "issued-repair")
	binding.RoundExecution = review.RoundExecution{Revision: 4, Completed: 1}
	s.DispatchBindings["issued-repair"] = binding
	if err := s.verifyValidationRoundLimits(e.cfg.Definition); err == nil {
		t.Fatal("repair interval integrity discarded the missing-authorization check")
	}
}

func TestRepairAutomaticLimitsUseCountedWavesAtRangeBoundaries(t *testing.T) {
	for _, limit := range []int{1, 3, 64} {
		t.Run(fmt.Sprint(limit), func(t *testing.T) {
			_, s := guardRepairFixture(t)
			s.Validation.Waves[0].Counted = false
			for n := 0; n < limit-1; n++ {
				s.Validation.Waves = append(s.Validation.Waves, ValidationWave{ID: fmt.Sprintf("prior-%d", n), Counted: true})
			}
			s.applyRoundLimits(settings.Configuration{RoundLimits: map[string]int{"repair": limit}}, 1)
			work := s.nextRepairWork()
			if work == nil || s.repairRoundBlocked(work) || s.completedValidationWaves() != limit-1 {
				t.Fatal("repair stopped before the configured automatic limit")
			}
			s.Validation.Waves[0].Counted = true
			if !s.repairRoundBlocked(work) || s.completedValidationWaves() != limit {
				t.Fatal("repair ignored the configured counted-wave limit")
			}
			work.pureP2 = true
			if s.repairRoundBlocked(work) {
				t.Fatal("round configuration changed the existing pure-P2 authority")
			}
		})
	}
}
func TestRepairWaveGrantCoversInitialMembersAcrossLimitAndNodeChanges(t *testing.T) {
	e, s := guardRepairFixture(t)
	wave := &s.Validation.Waves[0]
	wave.LimitHistory = []review.LimitInterval{{Value: 3}, {Value: 1, Revision: 5}}
	wave.ExtraAuthorized = true
	plan := plannedReviewEvidence(review.StartReadiness, "plan", "", "PASS", "independent members", nil).GranularityReview
	second := plan.Batches[0]
	second.ID = "other-member"
	plan.Batches = append(plan.Batches, second)
	s.Results["repair-plan"] = WorkerResult{ActionID: "repair-plan", Outcome: OutcomePass, Validation: &ValidationResult{Binding: WorkBinding{Task: wave.Repair.Task, WaveID: wave.ID}, GranularityReview: plan}}
	members := s.repairExecutions(wave)
	if len(members) != 2 {
		t.Fatal("fixture did not establish two independent repair members")
	}
	for n, member := range members {
		s.DispatchBindings[fmt.Sprintf("member-%d", n)] = WorkBinding{Task: member.task, WaveID: wave.ID, BatchID: member.batch.ID, PlanRequest: "repair-plan", RoundExecution: review.RoundExecution{Revision: 2, Completed: 3}}
	}
	s.FlowSelection = &FlowSelection{Disabled: []string{"repair"}}
	s.Validation.Sets[coverage.ReviewBlackbox].LimitHistory = []review.LimitInterval{{Value: 3}}
	if err := s.verifyValidationRoundLimits(e.cfg.Definition); err != nil {
		t.Fatal("a historical wave grant was revoked for its initial batch members", err)
	}
	wave.ExtraAuthorized = false
	if err := s.verifyValidationRoundLimits(e.cfg.Definition); err == nil {
		t.Fatal("initial member integrity no longer requires the original wave grant")
	}
}

func TestResetArchivedRepairSignaturesDoNotBecomeCurrentWaveObligations(t *testing.T) {
	e, s := guardRepairFixture(t)
	wave := s.Validation.Waves[0]
	s.DispatchBindings["archived-repair"] = WorkBinding{Task: wave.Repair.Task, WaveID: wave.ID, RoundExecution: review.RoundExecution{Revision: 1}}
	s.DispatchIssueRevisions["archived-repair"] = 1
	s.Decisions["reset"] = RecordedDecision{Control: ControlReset, Choice: "proceed", Revision: 2, Reset: &ResetDecision{Applied: true}}
	s.Validation.Waves = nil
	s.Validation.Sets[coverage.ReviewBlackbox].LimitHistory = []review.LimitInterval{{Value: 3}}
	if err := s.verifyValidationRoundLimits(e.cfg.Definition); err != nil {
		t.Fatal("retained reset evidence was mistaken for a live repair obligation", err)
	}
	delete(s.Decisions, "reset")
	if err := s.verifyValidationRoundLimits(e.cfg.Definition); err == nil {
		t.Fatal("a missing current wave was accepted without a reset archive boundary")
	}
}
