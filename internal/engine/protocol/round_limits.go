package protocol

import (
	"fmt"
	"strconv"
	"strings"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/settings"
)

func reviewLimitNode(action review.Action) string {
	if action == review.StartReadiness {
		return "tech-review"
	}
	return "product-review"
}

func qaLimitNode(mode coverage.ReviewKind) string {
	if mode == coverage.ReviewBlackbox {
		return "qa-blackbox"
	}
	return "qa-whitebox"
}

func (s *State) roundLimit(node string) int {
	return s.AppliedConfiguration().RoundLimit(node)
}

func (s *State) roundLimitInterval(node string) review.LimitInterval {
	entry := review.LimitInterval{Value: settings.DefaultRoundLimit}
	for _, applied := range s.AppliedConfigurations {
		if applied.Bypassed {
			continue
		}
		value := applied.Configuration.RoundLimit(node)
		if entry.Revision == 0 || value != entry.Value {
			entry = review.LimitInterval{Value: value, Revision: applied.Revision}
		}
	}
	return entry
}

func (s *State) applyRoundLimits(configuration settings.Configuration, revision uint64) {
	for _, action := range []review.Action{review.ProductReview, review.StartReadiness} {
		s.ReviewSeries = review.SetLimit(s.ReviewSeries, action, configuration.RoundLimit(reviewLimitNode(action)), revision)
	}
	if s.Validation == nil {
		return
	}
	for mode, set := range s.Validation.Sets {
		if set != nil {
			set.LimitHistory = review.AppendLimit(set.LimitHistory, configuration.RoundLimit(qaLimitNode(mode)), revision)
		}
	}
	for i := range s.Validation.Waves {
		wave := &s.Validation.Waves[i]
		wave.LimitHistory = review.AppendLimit(wave.LimitHistory, configuration.RoundLimit("repair"), revision)
	}
}

func qaFailureStreak(set *CaseSet) int {
	failures := 0
	if set != nil {
		for i := len(set.ReviewHistory) - 1; i >= 0 && set.ReviewHistory[i].Review.Verdict() == coverage.StatusFail; i-- {
			failures++
		}
	}
	return failures
}

func (s *State) qaRoundLimit(mode coverage.ReviewKind) int {
	if s.Validation != nil {
		if set := s.Validation.Sets[mode]; set != nil && len(set.LimitHistory) != 0 {
			return set.LimitHistory[len(set.LimitHistory)-1].Value
		}
	}
	return s.roundLimit(qaLimitNode(mode))
}

func lastRoundLimit(history []review.LimitInterval) int {
	if len(history) == 0 {
		return settings.DefaultRoundLimit
	}
	return history[len(history)-1].Value
}

func (s *State) repairRoundLimit(work *repairWork) int {
	if len(work.wave.LimitHistory) != 0 {
		return lastRoundLimit(work.wave.LimitHistory)
	}
	return s.roundLimit("repair")
}

func (s *State) repairRoundBlocked(work *repairWork) bool {
	if s.repairExtraAuthorized(work) || work.wave.Repair.PureP2 || work.pureP2 {
		return false
	}
	// A live or retryable execution already passed admission. Reconfiguration
	// does not revoke that promise or create an extra authorization obligation.
	for action, binding := range s.DispatchBindings {
		if binding.Task == work.task && (s.liveDispatch(action) || s.EndedAttempts[work.task].ActionID == action) {
			return false
		}
	}
	return s.completedValidationWaves() >= s.repairRoundLimit(work)
}

func (s *State) roundExecution(keyRole string, mode coverage.ReviewKind, issued uint64) review.RoundExecution {
	switch keyRole {
	case "engine.qa.review.rationale.v2":
		return review.RoundExecution{Revision: issued, Completed: qaFailureStreak(s.Validation.Sets[mode])}
	case "engine.repair.v7":
		return review.RoundExecution{Revision: issued, Completed: s.completedValidationWaves()}
	}
	return review.RoundExecution{}
}

func (s *State) verifyQAReviewRound(action string, binding WorkBinding) error {
	revision := binding.RoundExecution.Revision
	if revision == 0 {
		return fmt.Errorf("dispatch %s has no QA round admission", action)
	}
	if !s.requirementWorkRevisionCurrent(revision) {
		return nil
	}
	// The source task retains mode and review ordinal even after route changes
	// or work replacement. The ordinal identifies a history prefix, not a streak.
	parts := strings.Split(s.sourceValidationTask(binding.Task).Scope, ":")
	if len(parts) < 2 {
		return fmt.Errorf("dispatch %s has no QA review identity", action)
	}
	mode := coverage.ReviewKind(parts[1])
	set := s.Validation.Sets[mode]
	if set == nil {
		return fmt.Errorf("dispatch %s has no QA case-set", action)
	}
	limit, known := review.LimitAt(set.LimitHistory, revision)
	if !known {
		return fmt.Errorf("dispatch %s has no effective QA review limit", action)
	}
	if binding.RoundExecution.Completed < limit {
		return nil
	}
	ordinal, err := strconv.Atoi(parts[len(parts)-1])
	if err != nil || ordinal < 1 {
		return fmt.Errorf("dispatch %s has no QA review history boundary", action)
	}
	for id, d := range s.Decisions {
		r := s.PendingAsks[id].Validation
		if r == nil || d.Revision > revision || !s.requirementWorkRevisionCurrent(d.Revision) {
			continue
		}
		// A carried task may retain its source generation. The original Ask
		// identifies the generation of the decision, independently of that task.
		target := fmt.Sprintf("%d:%s:%d", r.Binding.Generation, mode, ordinal-1)
		if qaDecisionAuthorizesRound(r, d, mode, "engine.qa.review.rationale.v2", ordinal-1, target) {
			return nil
		}
	}
	return fmt.Errorf("dispatch %s exceeds its QA review limit without authorization", action)
}

func (s *State) verifyValidationRoundLimits(cd *compiler.CompiledDefinition) error {
	if s.Validation == nil {
		return nil
	}
	for mode, set := range s.Validation.Sets {
		if set != nil {
			if err := review.ValidateLimits(set.LimitHistory); err != nil {
				return fmt.Errorf("%s: %w", mode, err)
			}
		}
	}
	for _, wave := range s.Validation.Waves {
		if err := review.ValidateLimits(wave.LimitHistory); err != nil {
			return fmt.Errorf("wave %s: %w", wave.ID, err)
		}
	}
	for action, binding := range s.DispatchBindings {
		if binding.RoundExecution.Completed < 0 {
			return fmt.Errorf("dispatch %s has a negative round count", action)
		}
		role := s.validationRole(binding.Task, cd)
		if role == "engine.qa.review.rationale.v2" {
			if err := s.verifyQAReviewRound(action, binding); err != nil {
				return err
			}
			continue
		}
		if role != "engine.repair.v7" {
			continue
		}
		revision := binding.RoundExecution.Revision
		if revision == 0 {
			return fmt.Errorf("dispatch %s has no repair round admission", action)
		}
		wave := s.validationWave(binding.WaveID)
		if wave == nil {
			// Reset archives the wave projection while retaining immutable
			// dispatch evidence. Its boundary is owned by the reset decision.
			if s.beforeResetBoundary(action) {
				continue
			}
			return fmt.Errorf("dispatch %s has no repair wave", action)
		}
		limit, known := review.LimitAt(wave.LimitHistory, revision)
		if !known {
			return fmt.Errorf("dispatch %s has no effective repair limit", action)
		}
		authorized := wave.Repair != nil && wave.ExtraAuthorized && binding.Task == wave.Repair.Task
		if wave.ExtraAuthorized {
			for _, execution := range s.repairExecutionHistory(wave) {
				if execution.previous == nil && s.sourceValidationTask(execution.task) == s.sourceValidationTask(binding.Task) {
					authorized = true
				}
			}
		}
		for id, decision := range s.Decisions {
			request := s.PendingAsks[id].Validation
			authorized = authorized || request != nil && request.Kind == "REPAIR" && request.Binding.Task == binding.Task && decision.Choice == "approve"
		}
		if binding.RoundExecution.Completed >= limit && !authorized && !binding.PureP2 {
			return fmt.Errorf("dispatch %s exceeds its repair limit without authorization", action)
		}
	}
	return nil
}
