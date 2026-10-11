package protocol

import (
	"fmt"
	"slices"
	"strings"

	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/runtime"
)

const (
	RequirementWorkRedo  = "REDO"
	RequirementWorkCarry = "CARRY"
)

// RequirementWork identifies an issued or queued worker obligation and any
// result available when its requirement-change decision is requested.
// IssuedRevision is zero only for authorized work awaiting initial issuance.
// RequirementRevision is the original issuance revision; carry never rewrites it.
type RequirementWork struct {
	ActionID            string          `json:"actionId"`
	Task                runtime.TaskKey `json:"task"`
	IssuedRevision      uint64          `json:"issuedRevision"`
	RequirementRevision string          `json:"requirementRevision"`
	ResultDigest        string          `json:"resultDigest,omitempty"`
}

// RequirementWorkDecision is the user's disposition of exactly one dispatch.
// An empty result digest authorizes its in-flight result by dispatch identity.
// The containing ConfirmedRevision binds the decision to its new revision and Ask.
type RequirementWorkDecision struct {
	Work        RequirementWork `json:"work"`
	Disposition string          `json:"disposition"`
	Reason      string          `json:"reason"`
}

// RequirementWork returns the worker obligations requiring an explicit redo or
// carry decision. The two development-preparation review series have their own
// decisions; other independent reviews follow the same work contract as workers.
func (e *Engine) RequirementWork() ([]RequirementWork, error) {
	_, state, err := e.load()
	if err != nil {
		return nil, err
	}
	if state == nil {
		return nil, &RejectedError{Code: CodeNotInitialized, Detail: "engine state does not exist"}
	}
	return e.requirementWork(state), nil
}

func (e *Engine) requirementWork(state *State) []RequirementWork {
	if d, ok := state.resetDecision(); ok && d.Reset.Applied && !d.Reset.Confirmed {
		// Reset has already retired its old QA, gate and repair duties. The
		// accepted preview retains each original development dispatch, including
		// queued or stopped work whose PASS has not been accepted.
		var work []RequirementWork
		for _, item := range state.PendingAsks[d.RequestID].Reset.Work {
			if state.validationRole(item.Task, e.cfg.Definition) == "engine.development.batch.v4" {
				work = append(work, item)
			}
		}
		return work
	}
	tasks := map[string]runtime.TaskKey{}
	for id, pending := range state.PendingActions {
		tasks[id] = pending.Task
	}
	for id, receipt := range state.SpawnReceipts {
		tasks[id] = receipt.Task
	}
	for id, history := range state.SpawnFailures {
		if len(history) > 0 {
			tasks[id] = history[len(history)-1].Task
		}
	}
	ended := map[string]Attempt{}
	for _, attempt := range state.EndedAttempts {
		if state.endedWorkCurrent(attempt, e.cfg.Definition) {
			tasks[attempt.ActionID] = attempt.Task
			ended[attempt.ActionID] = attempt
		}
	}
	ids := make([]string, 0, len(tasks))
	for id := range tasks {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	work := make([]RequirementWork, 0, len(ids))
	for _, id := range ids {
		task := tasks[id]
		if _, obsolete := state.ObsoleteActions[id]; obsolete {
			if _, owed := ended[id]; !owed {
				continue
			}
		}
		series := e.reviewSeriesOf(task.Step)
		if series == review.ProductReview || series == review.StartReadiness {
			continue
		}
		issued := state.issuedRevisionOf(id)
		revision := state.requirementRevisionAt(issued)
		if binding, known := state.DispatchBindings[id]; known {
			revision = binding.RequirementRevision
		}
		item := RequirementWork{ActionID: id, Task: task, IssuedRevision: issued, RequirementRevision: revision}
		if result, ok := state.Results[id]; ok {
			item.ResultDigest = result.Digest
		} else if result, ok := state.RejectedResults[id]; ok {
			item.ResultDigest = result.Payload.Digest
		} else if result, ok := state.StagedResults[id]; ok {
			item.ResultDigest = result.Digest
		} else {
			eventIDs := make([]string, 0)
			for eventID, result := range state.QuarantinedResults {
				if result.ActionID == id {
					eventIDs = append(eventIDs, eventID)
				}
			}
			slices.Sort(eventIDs)
			if len(eventIDs) > 0 {
				item.ResultDigest = state.QuarantinedResults[eventIDs[0]].Digest
			}
		}
		work = append(work, item)
	}
	queued := state.requiredWorkTasks()
	for _, member := range state.repairWork() {
		if member.result == nil && !slices.Contains(queued, member.task) {
			queued = append(queued, member.task)
		}
	}
	if state.Validation != nil {
		for _, wave := range state.Validation.Waves {
			if wave.Repair != nil {
				for _, target := range wave.Repair.Targets {
					if target.Status == "PENDING" && !slices.Contains(queued, target.Task) {
						queued = append(queued, target.Task)
					}
				}
			}
		}
	}
	for _, key := range state.planTasks(e.cfg.Definition) {
		if !slices.Contains(queued, key) && state.developmentResult(key) == nil {
			queued = append(queued, key)
		}
	}
	for _, key := range queued {
		if state.TaskStatusOf(key) != runtime.TaskQueued && state.TaskStatusOf(key) != "" {
			continue
		}
		actionID := "act:" + key.String()
		if _, known := tasks[actionID]; known || slices.ContainsFunc(work, func(w RequirementWork) bool { return w.Task == key }) {
			continue
		}
		revision := state.requirementRevisionForTask(key)
		if revision == "" {
			_, _, revision = state.confirmedDevelopmentPlan()
			if member := state.repairMember(key); member != nil {
				revision = member.wave.RequirementRevision
			}
		}
		work = append(work, RequirementWork{ActionID: actionID, Task: key, RequirementRevision: revision})
	}
	slices.SortFunc(work, func(a, b RequirementWork) int { return strings.Compare(a.ActionID, b.ActionID) })
	return work
}

func validateWorkDecisionShape(decisions []RequirementWorkDecision) error {
	seen := map[string]bool{}
	for _, choice := range decisions {
		work := choice.Work
		if work.ActionID == "" || !work.Task.Valid() || work.RequirementRevision == "" || strings.TrimSpace(choice.Reason) == "" {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "each work decision requires its dispatch, task, requirement revision and reason"}
		}
		if choice.Disposition != RequirementWorkRedo && choice.Disposition != RequirementWorkCarry {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "work disposition must be REDO or CARRY"}
		}
		if seen[work.ActionID] {
			return &RejectedError{Code: CodeEventSchemaInvalid, Detail: "a work dispatch may be decided only once"}
		}
		seen[work.ActionID] = true
	}
	return nil
}

func (e *Engine) validateRequirementWork(state *State, decisions []RequirementWorkDecision, requesting bool) error {
	current := e.requirementWork(state)
	reject := func(detail string) error {
		return &RejectedError{Code: CodeRequirementDecisionRejected, Detail: detail}
	}
	if len(current) != len(decisions) {
		return reject("the requirement change must explicitly decide every current downstream dispatch")
	}
	byID := make(map[string]RequirementWork, len(current))
	for _, item := range current {
		byID[item.ActionID] = item
	}
	for _, choice := range decisions {
		want, ok := byID[choice.Work.ActionID]
		if !ok {
			return reject(fmt.Sprintf("work dispatch %q is not current", choice.Work.ActionID))
		}
		got := choice.Work
		if !requesting && got.ResultDigest == "" {
			want.ResultDigest = ""
		}
		if got != want {
			return reject(fmt.Sprintf("work dispatch %q does not match its issuance or result evidence", got.ActionID))
		}
		delete(byID, got.ActionID)
	}
	return nil
}

func sameRequirementPackage(a, b RequirementDecisionPackage) bool {
	return digestOfCanonical(a.Coverage) == digestOfCanonical(b.Coverage) && a.CandidateRevision == b.CandidateRevision && a.DeltaDigest == b.DeltaDigest && a.ImpactDigest == b.ImpactDigest &&
		a.Source == b.Source && a.Classification == b.Classification && a.RedoProductReview == b.RedoProductReview &&
		a.RedoStartReadiness == b.RedoStartReadiness && a.RedoTopology == b.RedoTopology && a.RedoRoute == b.RedoRoute &&
		a.FindingDigest == b.FindingDigest && a.ResetRequestID == b.ResetRequestID && digestOfCanonical(a.Children) == digestOfCanonical(b.Children) && digestOfCanonical(a.Impact) == digestOfCanonical(b.Impact) && slices.Equal(a.WorkDecisions, b.WorkDecisions)
}

// archiveDispatchResults keeps every available result on the same obsolete
// evidence path before the dispatch is removed from the acceptance indexes.
func (s *State) archiveDispatchResults(actionID string) {
	if result, ok := s.Results[actionID]; ok {
		s.archiveObsoleteResult(result)
		delete(s.Results, actionID)
	}
	if result, ok := s.StagedResults[actionID]; ok {
		s.archiveObsoleteResult(result)
		delete(s.StagedResults, actionID)
	}
	for eventID, result := range s.QuarantinedResults {
		if result.ActionID == actionID {
			s.archiveObsoleteResult(result)
			delete(s.QuarantinedResults, eventID)
		}
	}
}

// replacementWorkTask derives an execution identity from its authorization.
// Package order is part of Ask identity and remains unchanged in the ledger.
func replacementWorkTask(entry ConfirmedRevision, index int) runtime.TaskKey {
	old := entry.WorkDecisions[index].Work.Task
	return runtime.TaskKey{Node: old.Node, Step: old.Step, Scope: fmt.Sprintf("requirement:%d:%d", entry.StateRevision, index)}
}

func (s *State) requirementRevisionForTask(key runtime.TaskKey) string {
	for _, entry := range s.RequirementRevisions {
		for i, choice := range entry.WorkDecisions {
			if choice.Disposition == RequirementWorkRedo && replacementWorkTask(entry, i) == key {
				return entry.Revision
			}
		}
	}
	return ""
}

// requiredWorkTasks derives obligations from the confirmation ledger. Runtime
// TERMINAL alone never discharges work: the accepted result must be PASS.
func (s *State) requiredWorkTasks() []runtime.TaskKey {
	required := map[runtime.TaskKey]bool{}
	for _, entry := range s.RequirementRevisions {
		if !s.requirementWorkRevisionCurrent(entry.StateRevision) {
			continue
		}
		for i, choice := range entry.WorkDecisions {
			if choice.Disposition != RequirementWorkRedo {
				continue
			}
			delete(required, choice.Work.Task)
			required[replacementWorkTask(entry, i)] = true
		}
	}
	for id, result := range s.Results {
		if result.Outcome != OutcomePass || result.Validation != nil && result.Validation.Status == "CONTINUE" {
			continue
		}
		if _, obsolete := s.ObsoleteActions[id]; obsolete {
			continue
		}
		if receipt, ok := s.SpawnReceipts[id]; ok {
			delete(required, receipt.Task)
		}
	}
	keys := make([]runtime.TaskKey, 0, len(required))
	for key := range required {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b runtime.TaskKey) int { return strings.Compare(a.String(), b.String()) })
	return keys
}

func (e *Engine) applyRequirementWork(state *State, choices []RequirementWorkDecision, requestID string) error {
	entry := state.RequirementRevisions[len(state.RequirementRevisions)-1]
	e.invalidateValidationWork(state, choices)
	for i, choice := range choices {
		if choice.Disposition == RequirementWorkCarry {
			continue
		}
		work := choice.Work
		delete(state.EndedAttempts, work.Task)
		state.archiveDispatchResults(work.ActionID)
		if _, live := state.PendingActions[work.ActionID]; live {
			state.ObsoleteActions[work.ActionID] = ObsoleteAction{ActionID: work.ActionID, Task: work.Task, Reason: "superseded by requirement decision " + requestID}
		} else if _, pending := state.retireAction(work.ActionID, "", "superseded by requirement decision "+requestID); !pending {
			if _, recorded := state.ObsoleteActions[work.ActionID]; !recorded {
				state.ObsoleteActions[work.ActionID] = ObsoleteAction{ActionID: work.ActionID, Task: work.Task, Reason: "superseded by requirement decision " + requestID}
			}
		}
		if err := state.terminalizeTask(work.Task); err != nil {
			return err
		}
		key := replacementWorkTask(entry, i)
		state.Tasks[key] = runtime.TaskQueued
		obsolete := state.ObsoleteActions[work.ActionID]
		obsolete.ReplacedBy = "act:" + key.String()
		state.ObsoleteActions[work.ActionID] = obsolete
	}
	e.reconcileWorkerRetryAsks(state)
	return nil
}

func (s *State) requirementWorkReplacement(task runtime.TaskKey) (runtime.TaskKey, bool) {
	authorized := false
	for _, entry := range s.RequirementRevisions {
		for i, choice := range entry.WorkDecisions {
			if choice.Work.Task != task {
				continue
			}
			authorized = true
			if choice.Disposition == RequirementWorkRedo {
				task = replacementWorkTask(entry, i)
			}
		}
	}
	return task, authorized
}
