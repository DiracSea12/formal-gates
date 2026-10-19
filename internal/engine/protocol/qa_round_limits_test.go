package protocol

import (
	"encoding/json"
	"fmt"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/settings"
)

func TestQAReviewRoundsUseIndependentHistoricalIntervals(t *testing.T) {
	e, s, _ := guardPipelineFixture(t)
	s.SelectedGates = []string{"blackbox", "whitebox", mergeQAGateID}
	for _, mode := range []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox, coverage.ReviewMerge} {
		s.Validation.Sets[mode] = &CaseSet{Status: "REVIEW", Version: string(mode), ReviewHistory: []CaseReviewEvidence{failedQAReview(), failedQAReview()}}
	}
	configuration := settings.Configuration{RoundLimits: map[string]int{"qa-blackbox": 5, "qa-whitebox": 3}}
	s.applyRoundLimits(configuration, 1)
	for mode, set := range s.Validation.Sets {
		task := s.reviewTask(mode, s.Validation.Generation, set.Version, e.cfg.Definition)
		s.DispatchBindings[string(mode)] = WorkBinding{Task: task, RoundExecution: s.roundExecution("engine.qa.review.rationale.v2", mode, 2)}
	}
	configuration.RoundLimits = map[string]int{"qa-blackbox": 1, "qa-whitebox": 1}
	s.applyRoundLimits(configuration, 3)
	if err := s.verifyValidationRoundLimits(e.cfg.Definition); err != nil {
		t.Fatal("a reduced limit rejudged admitted QA reviews", err)
	}
	for mode, set := range s.Validation.Sets {
		want := 3
		if mode == coverage.ReviewBlackbox {
			want = 5
		}
		if got, known := review.LimitAt(set.LimitHistory, 2); !known || got != want {
			t.Fatal("QA review used another mode's interval", mode, got, known)
		}
		original := s.DispatchBindings[string(mode)]
		later := original
		later.RoundExecution.Revision = 3
		s.DispatchBindings[string(mode)] = later
		if err := s.verifyValidationRoundLimits(e.cfg.Definition); err == nil {
			t.Fatal("QA review integrity did not consume its admission interval", mode)
		}
		s.DispatchBindings[string(mode)] = original
	}
	// Configuration may remove a mode from the current route; its dispatch
	// remains historical evidence and is still checked against its own interval.
	s.SelectedGates = nil
	if err := s.verifyValidationRoundLimits(e.cfg.Definition); err != nil {
		t.Fatal("historical QA mode depended on the current route", err)
	}
}

func decideQARound(t *testing.T, e *Engine, s *State, kind, choice string, revision uint64) string {
	t.Helper()
	mode := coverage.ReviewBlackbox
	request := ValidationRequest{Kind: kind, Mode: mode, Target: s.qaReviewTarget(mode)}
	if kind == "RETRY" {
		task := s.Validation.Sets[mode].DesignTask
		binding := s.validationBinding(task, e.cfg.Definition)
		s.EndedAttempts[task] = Attempt{IssuedAction: decision.IssuedAction{ActionID: "ended-design", Task: task}, Business: true, Work: &binding, RetryExhausted: true}
		request.Target = task.String()
	}
	if !s.ensureValidationAsk(request, revision) {
		t.Fatal("QA authorization offer was not created")
	}
	for id, ask := range s.PendingAsks {
		if ask.Validation == nil || ask.Validation.Kind != kind || ask.Resolved {
			continue
		}
		detail := &ValidationDecision{Reason: "Review the corrected documented QA cases", Digest: ask.Validation.Digest, StateRevision: revision}
		if err := e.acceptValidationDecision(s, ask, &DecidePayload{Choice: AskOptionID(choice), Validation: detail}, revision, revision+1); err != nil {
			t.Fatal("the actual QA decision was rejected", err)
		}
		ask.Resolved = true
		s.PendingAsks[id] = ask
		s.Decisions[id] = RecordedDecision{RequestID: id, Control: ControlValidation, Choice: AskOptionID(choice), Revision: revision + 1, Validation: detail}
		return id
	}
	t.Fatal("missing QA authorization offer")
	return ""
}

func TestQAReviewAdmissionSurvivesReductionRecoveryAndAcceptance(t *testing.T) {
	for _, authorization := range []string{"automatic", "approve", "redesign", "retry-redesign"} {
		for _, recover := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/recover=%t", authorization, recover), func(t *testing.T) {
				e, s, _ := guardPipelineFixture(t)
				// Reuse a complete, validated QA manifest and review, with the
				// actual run's source, route and topology bindings.
				source, _, _ := qaOperationState(t)
				set := source.Validation.Sets[coverage.ReviewBlackbox]
				s.RequirementRevisions[0].Coverage = &CoverageInventory{Sources: []coverage.RequiredSource{{SourceID: "source", Category: coverage.SolutionConstraint, Applicability: coverage.QAApplicability}}}
				set.Manifest.Binding = coverage.ManifestBinding{SourceBinding: s.coverageContract(nil).RequiredSources.Binding, ReviewKind: coverage.ReviewBlackbox, RouteScope: s.validationRouteDigest(), TopologyScope: digestOfCanonical(s.Topology)}
				set.Status, set.Version = "REVIEW", digestOfCanonical(set.Manifest)
				set.DesignTask = pipelineTask(e.cfg.Definition, "engine.qa.design.v6", "0:BLACKBOX:1:design")
				set.ReviewHistory = []CaseReviewEvidence{failedQAReview(), failedQAReview(), failedQAReview()}
				s.Validation.Sets[coverage.ReviewBlackbox] = set
				configuration := settings.Configuration{RoundLimits: map[string]int{"qa-blackbox": 5}}
				if authorization != "automatic" {
					configuration.RoundLimits["qa-blackbox"] = 3
				}
				s.applyRoundLimits(configuration, 1)
				var grant string
				if authorization != "automatic" {
					if !s.prewaveBlocked(coverage.ReviewBlackbox, "engine.qa.review.rationale.v2", e.cfg.Definition) {
						t.Fatal("an exhausted review did not require a real decision")
					}
					kind, choice := "QA_REVIEW", authorization
					if authorization == "retry-redesign" {
						kind, choice = "RETRY", "redesign"
						set.Status = "DRAFT"
					}
					grant = decideQARound(t, e, s, kind, choice, 2)
					// The authorized redesign leaves the review history intact.
					set.Status = "REVIEW"
				}
				if s.prewaveBlocked(coverage.ReviewBlackbox, "engine.qa.review.rationale.v2", e.cfg.Definition) {
					t.Fatal("the admitted QA round was blocked")
				}
				task := s.reviewTask(coverage.ReviewBlackbox, s.Validation.Generation, set.Version, e.cfg.Definition)
				action := "review"
				s.DispatchIssueRevisions[action] = 4
				s.PendingActions[action] = PendingAction{ActionID: action, Task: task}
				s.Attempts[task] = Attempt{IssuedAction: decision.IssuedAction{ActionID: action, Task: task}, ID: "review-attempt"}
				e.bindBusinessAttempt(s, task)
				binding := s.DispatchBindings[action]
				configuration.RoundLimits["qa-blackbox"] = 1
				s.applyRoundLimits(configuration, 5)
				if s.prewaveBlocked(coverage.ReviewBlackbox, "engine.qa.review.rationale.v2", e.cfg.Definition) || s.DispatchBindings[action] != binding {
					t.Fatal("a lowered limit revoked or rewrote an issued review")
				}
				if recover {
					s.EndedAttempts[task] = s.Attempts[task]
					delete(s.PendingActions, action)
					delete(s.Attempts, task)
					if s.prewaveBlocked(coverage.ReviewBlackbox, "engine.qa.review.rationale.v2", e.cfg.Definition) {
						t.Fatal("a retryable QA review lost its admission")
					}
					action = "replacement"
					issued := decision.IssuedAction{ActionID: action, Task: task}
					s.DispatchIssueRevisions[action] = 6
					s.PendingActions[action] = PendingAction{ActionID: action, Task: task}
					s.Attempts[task] = Attempt{IssuedAction: issued, ID: "replacement-attempt"}
					s.restoreRetryBinding(issued, 6)
					e.bindBusinessAttempt(s, task)
					if s.DispatchBindings[action] != binding || s.ObsoleteActions["review"].ReplacedBy != action {
						t.Fatal("retry assigned a new logical QA round or interval")
					}
				}
				raw, err := json.Marshal(s)
				if err != nil {
					t.Fatal(err)
				}
				var restored State
				if err := json.Unmarshal(raw, &restored); err != nil {
					t.Fatal(err)
				}
				s = &restored
				if err := s.verifyValidationRoundLimits(e.cfg.Definition); err != nil {
					t.Fatal("restored admitted QA review failed integrity", err)
				}
				pass := source.Validation.Waves[0].Reviews[0]
				pass.Binding = set.Manifest.Binding
				for _, list := range [][]coverage.ReviewDecision{pass.CaseDecisions, pass.SharedStepDecisions} {
					for i := range list {
						list[i].Reason = "Independently checked the documented procedure and oracle"
					}
				}
				record := WorkerResult{ActionID: action, Outcome: OutcomePass, Validation: &ValidationResult{Binding: binding, Status: "PASS", Review: &pass}}
				if err := e.acceptValidationResult(s, record, 7); err != nil {
					t.Fatal("normal QA result acceptance rejected an admitted round", err)
				}
				set = s.Validation.Sets[coverage.ReviewBlackbox]
				if len(set.ReviewHistory) != 4 || qaFailureStreak(set) != 0 || set.Review == nil || set.Review.Verdict() != coverage.StatusPass {
					t.Fatal("accepted PASS did not advance the actual QA history")
				}
				if err := s.verifyValidationRoundLimits(e.cfg.Definition); err != nil {
					t.Fatal("a later PASS reinterpreted a historical QA grant or streak", err)
				}
				set.reopen(nil)
				if err := s.verifyValidationRoundLimits(e.cfg.Definition); err != nil {
					t.Fatal("reopening the current case-set reinterpreted a historical QA round", err)
				}
				// A PASS ends the streak. A subsequent redesign/review is
				// automatic under limit 1, independently of the earlier grant.
				set.Status = "REVIEW"
				nextTask := s.reviewTask(coverage.ReviewBlackbox, s.Validation.Generation, set.Version, e.cfg.Definition)
				s.DispatchIssueRevisions["subsequent-review"] = 8
				s.Attempts[nextTask] = Attempt{IssuedAction: decision.IssuedAction{ActionID: "subsequent-review", Task: nextTask}}
				e.bindBusinessAttempt(s, nextTask)
				nextBinding := s.DispatchBindings["subsequent-review"]
				if nextBinding.RoundExecution.Completed != 0 {
					t.Fatal("the new automatic review inherited the historical FAIL streak")
				}
				failed := pass
				failed.SourceDecisions = append([]coverage.ReviewDecision(nil), pass.SourceDecisions...)
				failed.PointDecisions = append([]coverage.ReviewDecision(nil), pass.PointDecisions...)
				failed.CaseDecisions = append([]coverage.ReviewDecision(nil), pass.CaseDecisions...)
				for _, list := range [][]coverage.ReviewDecision{failed.SourceDecisions, failed.PointDecisions, failed.CaseDecisions} {
					for i := range list {
						list[i].Status = coverage.StatusFail
						list[i].Reason = "The redesigned cases omit their documented failure oracles"
					}
				}
				failed.SetStatus = coverage.StatusFail
				record = WorkerResult{ActionID: "subsequent-review", Outcome: OutcomePass, Validation: &ValidationResult{Binding: nextBinding, Status: "PASS", Review: &failed}}
				if err := e.acceptValidationResult(s, record, 9); err != nil {
					t.Fatal("the new automatic review could not record its real FAIL", err)
				}
				if qaFailureStreak(set) != 1 || !s.prewaveBlocked(coverage.ReviewBlackbox, "engine.qa.design.v6", e.cfg.Definition) {
					t.Fatal("an old grant leaked across PASS into a later FAIL boundary")
				}
				if err := s.verifyValidationRoundLimits(e.cfg.Definition); err != nil {
					t.Fatal("the current FAIL streak was applied to an earlier round", err)
				}
				if grant != "" {
					original := s.Decisions[grant]
					delete(s.Decisions, grant)
					if err := s.verifyValidationRoundLimits(e.cfg.Definition); err == nil {
						t.Fatal("historical QA integrity borrowed the later PASS instead of its original authorization")
					}
					future := original
					future.Revision = 8
					s.Decisions[grant] = future
					if err := s.verifyValidationRoundLimits(e.cfg.Definition); err == nil {
						t.Fatal("a later decision authorized an earlier QA round")
					}
					s.Decisions[grant] = original
				}
				s.Decisions["reset"] = RecordedDecision{Control: ControlReset, Choice: "proceed", Revision: 11, Reset: &ResetDecision{Applied: true}}
				s.Validation.Generation = 11
				s.Validation.Sets = map[coverage.ReviewKind]*CaseSet{coverage.ReviewBlackbox: {LimitHistory: []review.LimitInterval{{Value: 1, Revision: 11}}}}
				if err := s.verifyValidationRoundLimits(e.cfg.Definition); err != nil {
					t.Fatal("reset-archived QA signatures became obligations of the new case-set", err)
				}
				current := s.Validation.Sets[coverage.ReviewBlackbox]
				current.Status = "DRAFT"
				current.ReviewHistory = []CaseReviewEvidence{failedQAReview(), failedQAReview(), failedQAReview()}
				if !s.prewaveBlocked(coverage.ReviewBlackbox, "engine.qa.review.rationale.v2", e.cfg.Definition) {
					t.Fatal("a reset reused an earlier authorization at an equal history count")
				}
			})
		}
	}
}
