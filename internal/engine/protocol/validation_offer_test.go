package protocol

import (
	"encoding/json"
	"reflect"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
)

// An unchanged obligation retains one offer while its current revision advances.
// Changed decision content supersedes the offer without consuming authorization.
func TestValidationOffersRemainAvailableUntilUserDecision(t *testing.T) {
	for _, kind := range []string{"RETRY", "QA_REVIEW", "REPAIR", "QA_RETRY", "VALIDATION_DETAIL_DISPOSITION", "THAW", "QA_ARTIFACT_REPAIR", "DISPOSE", KindDispatchBudgetDisposition} {
		t.Run(kind, func(t *testing.T) {
			s := &State{PendingAsks: map[string]PendingAsk{}, Decisions: map[string]RecordedDecision{}, Validation: &Validation{Sets: map[coverage.ReviewKind]*CaseSet{}}}
			request := ValidationRequest{Kind: kind, Target: "obligation", Mode: coverage.ReviewBlackbox}
			if kind == "REPAIR" || kind == "QA_RETRY" {
				_, s = guardRepairFixture(t)
				request.Target = s.Validation.Waves[0].ID
				request.Binding.WaveID = request.Target
			}
			decisions := digestOfCanonical(s.Decisions)
			if !s.ensureValidationAsk(request, 1) {
				t.Fatal("initial offer not created")
			}
			var original string
			for id := range s.PendingAsks {
				original = id
			}
			for _, revision := range []uint64{3, 5} {
				if s.ensureValidationAsk(request, revision) {
					t.Fatal("revision alone replaced the obligation's offer")
				}
				rebindValidationAsks(s, revision)
				ask := s.PendingAsks[original]
				if len(s.PendingAsks) != 1 || ask.Resolved || ask.Validation.StateRevision != revision {
					t.Fatalf("current offer lost identity or revision: %+v", ask)
				}
				if digestOfCanonical(s.Decisions) != decisions {
					t.Fatal("revision advance changed user authorization")
				}
				if digestOfCanonical(s.validationRequestBasis(*ask.Validation)) != ask.Validation.Digest {
					t.Fatal("current revision retained a different decision basis")
				}
			}
			request.Category = "CASE_EXECUTABILITY"
			if !s.ensureValidationAsk(request, 6) {
				t.Fatal("changed decision content did not create a fresh offer")
			}
			if !s.PendingAsks[original].Resolved {
				t.Fatal("changed content left the replaced offer pending")
			}
			pending := 0
			for id, ask := range s.PendingAsks {
				if ask.Resolved {
					continue
				}
				pending++
				wantRequestDigest := digestOfCanonical(ask)
				ask.Resolved = true
				s.PendingAsks[id] = ask
				s.Decisions[id] = RecordedDecision{RequestID: id, Control: ControlValidation, Choice: "decline", Revision: 7}
				if s.requestDigest(id) != wantRequestDigest {
					t.Fatal("settlement changed the generated offer evidence")
				}
				beforeAsk, beforeDecision := digestOfCanonical(s.PendingAsks[id]), digestOfCanonical(s.Decisions[id])
				rebindValidationAsks(s, 8)
				if beforeAsk != digestOfCanonical(s.PendingAsks[id]) || beforeDecision != digestOfCanonical(s.Decisions[id]) || s.requestDigest(id) != wantRequestDigest {
					t.Fatal("commit binding rewrote resolved evidence")
				}
			}
			if pending != 1 {
				t.Fatalf("pending offers=%d, want one", pending)
			}
			if kind != "VALIDATION_DETAIL_DISPOSITION" && s.ensureValidationAsk(request, 9) {
				t.Fatal("real user decision was treated as display supersession")
			}
		})
	}
}

func TestRepairChoicesRefreshAtBothCommitSeams(t *testing.T) {
	for _, seam := range []string{"advance", "rebind"} {
		t.Run(seam, func(t *testing.T) {
			_, state := guardRepairFixture(t)
			source := state.Validation.Waves[0]
			state.ensureValidationAsk(ValidationRequest{Kind: "REPAIR", Target: source.ID}, 7)
			var prior PendingAsk
			for _, ask := range state.PendingAsks {
				if ask.Validation != nil && !ask.Resolved {
					prior = ask
				}
			}
			if !prior.hasOption("retry_qa") || !prior.hasOption("waive") {
				t.Fatal("real repair fixture lacks legal original choices")
			}
			projection := source
			projection.ID = "wave-full-validation"
			projection.Scope = coverage.ScopeFull
			if source.Scope == projection.Scope {
				projection.Scope = coverage.ScopeAffected
			}
			projection.Status, projection.Repair = "COLLECTING", nil
			state.Validation.Waves = append(state.Validation.Waves, projection)
			decisions := digestOfCanonical(state.Decisions)
			if seam == "advance" {
				if !state.reconcileValidationRemedyAsks(8) {
					t.Fatal("eligibility change did not count as business advancement")
				}
			} else {
				rebindValidationAsks(state, 8)
			}
			retired := state.PendingAsks[prior.RequestID]
			if !retired.Resolved {
				t.Fatal("ineligible source QA choice received another fresh binding")
			}
			retired.Resolved = false
			if !reflect.DeepEqual(retired, prior) || digestOfCanonical(state.Decisions) != decisions {
				t.Fatal("choice replacement rewrote request or decision evidence")
			}
			pending := 0
			for _, ask := range state.PendingAsks {
				if ask.Resolved {
					continue
				}
				pending++
				if ask.hasOption("retry_qa") || !ask.hasOption("approve") || !ask.hasOption("waive") {
					t.Fatal("replacement choices disagree with actual source eligibility")
				}
				if digestOfCanonical(state.validationRequestBasis(*ask.Validation)) != ask.Validation.Digest {
					t.Fatal("replacement has a different complete decision basis")
				}
			}
			if pending != 1 {
				t.Fatal("choice replacement did not keep exactly one owner")
			}
		})
	}
}

func TestValidationDecisionCreatesRequirementRequestEvidence(t *testing.T) {
	engine, state, _ := guardPipelineFixture(t)
	state.ensureValidationAsk(ValidationRequest{Kind: "QA_REVIEW", Mode: coverage.ReviewBlackbox, Target: state.qaReviewTarget(coverage.ReviewBlackbox)}, 7)
	var ask PendingAsk
	for _, current := range state.PendingAsks {
		if current.Validation != nil && !current.Resolved {
			ask = current
		}
	}
	if !ask.hasOption("requirement") {
		t.Fatal("validation offer does not permit requirement revision")
	}
	decision := &DecidePayload{Request: RequestID(ask.RequestID), Choice: "requirement", Validation: &ValidationDecision{Digest: ask.Validation.Digest, StateRevision: 7, Reason: "Clarify the requirement before another review"}}
	if err := engine.acceptValidationDecision(state, ask, decision, 7, 8); err != nil {
		t.Fatal(err)
	}
	requestID := "requirement:" + ask.Validation.Target + ":8"
	offer := state.PendingAsks[requestID]
	if offer.RequestID != requestID || offer.Control != ControlRequestRequirementChange || offer.BaseRevision != state.RequirementRevision() || offer.BaseGeneration != state.requirementGeneration() || !offer.hasOption("proceed") || !offer.hasOption("abort") {
		t.Fatalf("generated requirement offer = %+v", offer)
	}
	if _, external := state.Events[requestID]; external || state.requestDigest(requestID) != digestOfCanonical(offer) {
		t.Fatal("generated requirement request lost its complete offer evidence")
	}
}

func TestRepairAskHistoryRetainsOriginalEvidence(t *testing.T) {
	s, _, task := developmentCheckState(t)
	result := s.Results["edit-action"]
	result.Validation.Binding.WaveID = "historical-wave"
	s.Results[result.ActionID] = result
	s.DispatchBindings[result.ActionID] = result.Validation.Binding
	addDevelopmentSource(t, s)
	parameters, ready := s.nextCheckParameters(result.ActionID)
	if !ready {
		t.Fatal("original check is not ready")
	}
	addCheckReceipt(t, s, parameters, 7)
	qa := runtime.TaskKey{Node: "validation", Step: "qa", Scope: "carried"}
	gate := runtime.TaskKey{Node: "validation", Step: "gate", Scope: "repair"}
	carried := WorkerResult{ActionID: "qa-original", Outcome: OutcomePass, Digest: "original-qa-digest", Validation: &ValidationResult{Binding: WorkBinding{Task: qa, WaveID: "physical-source", Generation: 0}, Status: "PASS"}}
	gateResult := WorkerResult{ActionID: "gate-original", Outcome: OutcomeRuntimeError, Validation: &ValidationResult{Binding: WorkBinding{Task: gate, WaveID: "historical-wave", Generation: 1}, Status: "RUNTIME_ERROR"}}
	s.Results[carried.ActionID], s.Results[gateResult.ActionID] = carried, gateResult
	wave := ValidationWave{
		ID: "historical-wave", RequirementRevision: "req", Generation: 1, Status: "INVALIDATED", Scope: coverage.ScopeAffected,
		Candidate: coverage.ValidationCandidate{Identity: "original-candidate", Digest: "original-content"},
		Targets:   map[string]*ValidationTarget{qa.String(): {Task: qa, Kind: "QA", Mode: coverage.ReviewBlackbox, Status: "PASS", ResultAction: carried.ActionID, RequiredCases: []string{"normal"}}},
		Repair:    &ValidationRepair{Task: task, Problems: []string{"original-problem"}, Targets: map[string]*ValidationTarget{gate.String(): {Task: gate, Kind: "GATE", Gate: "quality", Status: "AUTHORIZED_SKIP", ResultAction: gateResult.ActionID}}},
	}
	s.Validation.Generation = 9
	s.Validation.RequirementRevision = "current-requirement"
	s.Validation.Waves = []ValidationWave{wave}
	before, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	history := s.repairHistory(&s.Validation.Waves[0])
	if history.Wave != wave.ID || history.Candidate != wave.Candidate || history.RequirementRevision != "req" || history.Generation != 1 || history.Status != "INVALIDATED" || history.Counted || history.WaveScope != "AFFECTED" {
		t.Fatalf("historical metadata changed: %+v", history)
	}
	if len(history.Targets) != 2 || history.Targets[0].Task != gate.String() || history.Targets[0].Status != "AUTHORIZED_SKIP" || !reflect.DeepEqual(history.Targets[1].RequiredCases, []string{"normal"}) {
		t.Fatalf("original targets or repair gate lost: %+v", history.Targets)
	}
	if !reflect.DeepEqual(history.Results, []WorkerResult{result, gateResult, carried}) {
		t.Fatalf("original result identities, bindings, or outcomes changed: %+v", history.Results)
	}
	evidence, exists := s.checkReceipt(result.ActionID, parameters.Check.ID)
	if !exists || len(history.Checks) != 1 || !reflect.DeepEqual(history.Checks[0], *evidence) || history.Checks[0].ExitCode != 7 || history.Checks[0].SourceIdentity != parameters.SourceIdentity || history.Checks[0].Stdout.SHA256 != "out" || history.Checks[0].Stderr.SHA256 != "err" {
		t.Fatalf("actual failed check source/exit/streams changed: %+v", history.Checks)
	}
	if !reflect.DeepEqual(history, s.repairHistory(&s.Validation.Waves[0])) {
		t.Fatal("historical map iteration changed presentation order")
	}
	after, err := json.Marshal(s)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("history projection changed its original state")
	}
}

func TestRepairAskHistoryBindsAuthorizationDigest(t *testing.T) {
	engine, s := guardRepairFixture(t)
	current := s.Validation.Waves[0]
	prior := ValidationWave{ID: "older-wave", RequirementRevision: "older-requirement", Generation: 0, Status: "INVALIDATED", Candidate: coverage.ValidationCandidate{Identity: "older-candidate", Digest: "older-content"}}
	later := prior
	later.ID = "second-wave"
	later.Candidate.Identity = "second-candidate"
	s.Validation.Waves = []ValidationWave{prior, later, current}
	if !s.ensureValidationAsk(ValidationRequest{Kind: "REPAIR", Target: current.ID}, 7) {
		t.Fatal("current repair offer not created")
	}
	var ask PendingAsk
	for _, offer := range s.PendingAsks {
		if offer.Validation != nil && offer.Validation.Kind == "REPAIR" && !offer.Resolved {
			ask = offer
		}
	}
	var summary struct{ Detail repairAskFace }
	if err := json.Unmarshal(ask.Validation.Summary, &summary); err != nil {
		t.Fatal(err)
	}
	if len(summary.Detail.History) != 2 || summary.Detail.History[0].Wave != prior.ID || summary.Detail.History[1].Wave != later.ID || summary.Detail.Wave != current.ID {
		t.Fatal("history does not retain creation order before the source wave")
	}
	originalDigest := ask.Validation.Digest
	s.Validation.Waves[0].Candidate.Digest = "changed-original-evidence"
	decision := &DecidePayload{Choice: "approve", Validation: &ValidationDecision{Digest: originalDigest, StateRevision: 7, Reason: "authorize the displayed evidence"}}
	before := digestOfCanonical(s)
	if err := engine.acceptValidationDecision(s, ask, decision, 7, 8); err == nil || before != digestOfCanonical(s) {
		t.Fatal("old authorization accepted changed historical evidence or changed state")
	}
	rebindValidationAsks(s, 8)
	rebound := s.PendingAsks[ask.RequestID]
	if rebound.Validation.Digest == originalDigest || rebound.Validation.StateRevision != 8 || rebound.Resolved {
		t.Fatal("history did not follow the existing digest/freshness binding")
	}
}

func TestRepairAskHistoryRetainsChecksAfterPlanChange(t *testing.T) {
	_, s := guardRepairFixture(t)
	wave := &s.Validation.Waves[0]
	plan := s.Results["act:readiness-plan"].Review.GranularityReview
	plan.Batches[0].Checks = []DevelopmentCheck{
		{ID: "build", Purpose: CheckFullBuild, Command: "go build ./..."},
		{ID: "tests", Purpose: CheckFullTests, Command: "go test -timeout=0 ./..."},
	}
	batch := DevelopmentBatch{ID: "command", Checks: []DevelopmentCheck{{ID: "local", Command: "check command"}}}
	result := WorkerResult{ActionID: "repair-completion", Outcome: OutcomePass, Validation: &ValidationResult{
		Binding: WorkBinding{Task: wave.Repair.Task, WaveID: wave.ID, RequirementRevision: wave.RequirementRevision, Generation: wave.Generation, WorkspaceID: "repair-workspace"},
		Status:  "PASS", GranularityReview: &GranularityReview{Batches: []DevelopmentBatch{batch}},
		Development: &DevelopmentEvidence{Paths: []string{"app.go"}, Summary: "repaired normal command"},
	}}
	s.Results[result.ActionID] = result
	s.DispatchBindings[result.ActionID] = result.Validation.Binding
	checks := s.editChecks(result)
	if len(checks) != 3 {
		t.Fatal("original repair does not bind local, full build and full test checks")
	}
	recordDevelopmentChecksFixture(t, s, result.ActionID, DevelopmentBatch{Checks: checks})
	original := s.repairHistory(wave)
	if len(original.Checks) != len(checks) {
		t.Fatal("original executed checks were not projected")
	}
	revised := *plan
	revised.Batches = []DevelopmentBatch{{ID: "changed", Checks: []DevelopmentCheck{{ID: "new-build", Purpose: CheckFullBuild, Command: "build changed project"}}}}
	readiness := s.Results["act:readiness-plan"]
	readiness.ActionID = "changed-readiness"
	review := *readiness.Review
	review.GranularityReview = &revised
	readiness.Review = &review
	s.Results[readiness.ActionID] = readiness
	decision := s.Decisions["route-1"]
	decision.RequestID, decision.Revision = "changed-route", decision.Revision+1
	routeReview := *decision.Review
	route := *routeReview.Route
	route.DevelopmentPlan = &DevelopmentPlanConfirmation{ReviewActionID: readiness.ActionID}
	routeReview.Route = &route
	decision.Review = &routeReview
	s.Decisions[decision.RequestID] = decision
	if len(s.fullProjectChecks()) != 1 || s.fullProjectChecks()[0].Command != "build changed project" {
		t.Fatal("current confirmed plan did not change its checks")
	}
	if _, found := s.checkReceipt(result.ActionID, checks[1].ID); found {
		t.Fatal("fixture did not reach the current-plan check eligibility seam")
	}
	before := digestOfCanonical(s)
	if changed := s.repairHistory(wave); !reflect.DeepEqual(original, changed) {
		t.Fatalf("new plan changed original historical evidence: %+v", changed)
	}
	if before != digestOfCanonical(s) {
		t.Fatal("historical check projection rewrote the original receipts")
	}
}
