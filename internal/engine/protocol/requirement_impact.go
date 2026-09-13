package protocol

import (
	"fmt"
	"sort"

	"formal-gates/internal/engine/runtime"
)

// ImpactItem is an engine-produced scope, not an operator's inferred authority.
// Keeping unfinished development preserves work; it does not manufacture PASS.
type ImpactItem struct {
	ID       string `json:"id"`
	CanKeep  bool   `json:"canKeep"`
	Evidence string `json:"evidence"`
}

type RequirementImpactDecision struct {
	RequestID           string            `json:"requestId"`
	DecisionEvent       string            `json:"decisionEvent"`
	RequirementRevision string            `json:"requirementRevision"`
	Candidate           string            `json:"candidate"`
	Revision            uint64            `json:"revision"`
	Choices             map[string]string `json:"choices"`
}

func requirementImpactItems(r *RegularState) []ImpactItem {
	items := map[string]ImpactItem{}
	for _, name := range reviewActions {
		series := r.Reviews[name]
		evidence := "not completed"
		if series != nil {
			evidence = series.CurrentDigest
		}
		items[name] = ImpactItem{ID: name, CanKeep: series != nil && effectiveReview(r, series) == "PASS", Evidence: evidence}
	}
	if p := r.Preparation; p != nil {
		items["development"] = ImpactItem{ID: "development", CanKeep: true, Evidence: "unfinished development/workspace at " + p.DevelopmentSnapshot}
		for id, c := range p.Commits {
			key := "batch:" + id
			items[key] = ImpactItem{ID: key, CanKeep: true, Evidence: c.After}
		}
		for mode, q := range p.QA {
			key := mode + "-review"
			items[key] = ImpactItem{ID: key, CanKeep: verificationKeep(r, key), Evidence: digestOfCanonical(q.Design)}
		}
	}
	if r.Verification != nil {
		for name, e := range r.Verification.Effective {
			items[name] = ImpactItem{ID: name, CanKeep: true, Evidence: digestOfCanonical(e)}
		}
	}
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]ImpactItem, 0, len(keys))
	for _, key := range keys {
		result = append(result, items[key])
	}
	return result
}

func recordRequirementImpact(r *RegularState, a *RegularAsk, d RegularDecision, eventID string, revision uint64) error {
	if len(a.ImpactItems) == 0 || len(d.Dispositions) != len(a.ImpactItems) {
		return fmt.Errorf("USER_IMPACT_REQUIRED: decide keep/rerun/delegate for every engine-listed item")
	}
	choices := map[string]string{}
	for _, item := range a.ImpactItems {
		choice := d.Dispositions[item.ID]
		switch choice {
		case "keep":
			if !item.CanKeep {
				return fmt.Errorf("USER_IMPACT_REQUIRED: %s has no eligible evidence to keep", item.ID)
			}
		case "rerun", "delegate":
		default:
			return fmt.Errorf("USER_IMPACT_REQUIRED: %s needs keep/rerun/delegate", item.ID)
		}
		choices[item.ID] = choice
	}
	r.RequirementImpact = &RequirementImpactDecision{RequestID: a.ID, DecisionEvent: eventID, RequirementRevision: r.Requirement.Revision, Candidate: r.Candidate, Revision: revision, Choices: choices}
	return nil
}

// Apply input may classify delegated items only. Fixed user choices are copied
// into the normalized request; contradictory operator input is rejected, never
// silently preferred over the user's earlier typed decision.
func resolveRequirementImpact(r *RegularState, q *RegularRequest) error {
	decision := r.RequirementImpact
	if decision == nil || decision.RequirementRevision != r.Requirement.Revision || decision.Candidate != r.Candidate {
		return fmt.Errorf("USER_IMPACT_REQUIRED: confirm the requirement-change disposition Ask before applying")
	}
	impact := map[string]string{}
	for name, value := range q.Impact {
		if _, ok := decision.Choices[name]; !ok {
			return fmt.Errorf("USER_IMPACT_REQUIRED: %s was not delegated or selected", name)
		}
		impact[name] = value
	}
	keep := map[string]bool{}
	for _, name := range q.Keep {
		keep[name] = true
	}
	for name, choice := range decision.Choices {
		switch choice {
		case "keep":
			if proposed := impact[name]; proposed != "" && proposed != "unaffected" {
				return fmt.Errorf("USER_IMPACT_CONFLICT: %s was fixed to keep", name)
			}
			impact[name] = "unaffected"
			if r.Reviews[name] != nil || verificationKeep(r, name) {
				keep[name] = true
			}
		case "rerun":
			if proposed := impact[name]; (proposed != "" && proposed != "affected") || keep[name] {
				return fmt.Errorf("USER_IMPACT_CONFLICT: %s was fixed to rerun", name)
			}
			impact[name] = "affected"
		case "delegate":
			if impact[name] != "unaffected" && impact[name] != "affected" && impact[name] != "unknown" {
				return fmt.Errorf("USER_IMPACT_REQUIRED: explicitly classify delegated item %s", name)
			}
		}
	}
	for name := range keep {
		if _, ok := decision.Choices[name]; !ok {
			return fmt.Errorf("USER_IMPACT_REQUIRED: keep %s was not authorized", name)
		}
	}
	q.Impact = impact
	q.Keep = nil
	for name := range keep {
		q.Keep = append(q.Keep, name)
	}
	sort.Strings(q.Keep)
	return nil
}

// Preserve the original TaskKeys before a new requirement creates a projection.
// Explicit reruns add one execution authorization; no attempt or PASS is erased.
func bindRequirementBatchLineage(s *State, q RegularRequest) {
	p := s.Regular.Preparation
	if p == nil {
		return
	}
	tasks := map[string]runtime.TaskKey{}
	for id, key := range p.BatchTasks {
		tasks[id] = key
	}
	for id, result := range p.Batches {
		x, ok := s.RegularExecutions[result.ActionID]
		if !ok {
			continue
		}
		tasks[id] = x.Claim.Task
		impact := q.Impact["batch:"+id]
		if impact == "" {
			impact = q.Impact["development"]
		}
		if impact != "unaffected" {
			if p.ExtraAttempts == nil {
				p.ExtraAttempts = map[string]int{}
			}
			p.ExtraAttempts[x.Claim.Task.String()]++
		}
	}
	if b := nextBatch(s.Regular); b != nil {
		tasks[b.ID] = batchTask(p, b.ID)
	}
	p.BatchTasks = tasks
}

// A paused, never-started action has no native side effects to reconcile. An
// explicit change retires its obsolete binding without inventing a FAIL. The
// original task and attempt remain in history; the decision grants replacement
// capacity instead of resetting an attempt counter.
func retireUnclaimedOnChange(s *State, decisionID string, steps ...string) {
	for id, x := range s.RegularExecutions {
		if len(steps) > 0 {
			selected := false
			for _, step := range steps {
				if string(x.Claim.Task.Step) == step {
					selected = true
					break
				}
			}
			if !selected {
				continue
			}
		}
		if x.Claimed || x.Finished {
			continue
		}
		x.Finished = true
		x.SupersededBy = decisionID
		s.RegularExecutions[id] = x
		delete(s.PendingActions, id)
		s.Tasks[x.Claim.Task] = runtime.TaskTerminal
		key := x.Claim.Task.String()
		if review := s.Regular.Reviews[string(x.Claim.Task.Step)]; review != nil {
			if review.ExtraAttempts == nil {
				review.ExtraAttempts = map[string]int{}
			}
			review.ExtraAttempts[key]++
		} else if p := s.Regular.Preparation; p != nil {
			if p.ExtraAttempts == nil {
				p.ExtraAttempts = map[string]int{}
			}
			p.ExtraAttempts[key]++
		}
	}
}
