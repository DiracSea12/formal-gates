package cli

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
)

// Workflow views present current semantic duties; engine projections remain
// complete for program consumers and saved confirmation candidates.
func workflowRunView(run facade.Run) map[string]any {
	next := map[string]any{"kind": run.Next.Kind}
	if run.Next.Kind == decision.KindReady || run.Next.Kind == decision.KindHostAction || run.Next.Kind == decision.KindOperator {
		next["action"] = "drive"
	}
	if run.Next.Wait != nil {
		next["reason"] = run.Next.Wait.Reason
	}
	view := map[string]any{"runId": run.RunID, "status": run.Status, "route": run.Route, "revision": run.Revision, "next": next, "unverified": run.Unverified}
	if run.SummaryPath != "" {
		view["summaryPath"] = run.SummaryPath
	}
	view["availableActions"] = run.AvailableActions
	questions := []map[string]any{}
	for index, ask := range run.PendingAsks {
		options := []map[string]any{}
		for _, option := range ask.Options {
			choice := string(option.ID)
			if ask.Control == protocol.ControlReviewFindingDecision {
				choice, _, _ = strings.Cut(choice, ":")
			}
			options = append(options, map[string]any{"choice": choice, "label": option.Label})
		}
		question := map[string]any{"ask": index + 1, "control": ask.Control, "options": options}
		if len(ask.RouteGates) != 0 {
			question["gates"] = numberedChoices(ask.RouteGates)
		}
		if ask.Validation != nil {
			v := ask.Validation
			question["validation"] = map[string]any{"kind": v.Kind, "mode": v.Mode, "category": v.Category, "affectedCases": v.AffectedCases, "obligations": numberedChoices(v.Obligations), "remedies": v.Remedies, "summary": v.Summary}
		}
		if ask.RoundGrant != nil {
			question["review"] = map[string]any{"action": ask.RoundGrant.Action, "completed": ask.RoundGrant.Completed, "automaticLimit": ask.RoundGrant.AutomaticLimit}
		}
		if ask.Control == protocol.ControlReviewFindingDecision {
			question["reviews"] = workflowReviewChoices(run.Review)
		}
		if ask.Control == protocol.ControlConfirmTopology {
			question["proposal"] = run.Review.ReadinessProposal
		}
		questions = append(questions, question)
	}
	view["questions"] = questions
	if run.PendingRequirementAsk != nil {
		work := []map[string]any{}
		for index, item := range run.RequirementWork {
			work = append(work, map[string]any{"work": index + 1, "task": item.Task, "requirementRevision": item.RequirementRevision})
		}
		view["requirementWork"] = work
	}
	return view
}

func workflowControlRunViews(runs []facade.Run) map[string]map[string]any {
	views := make(map[string]map[string]any, len(runs))
	number := 0
	for _, run := range runs {
		view := workflowRunView(run)
		for _, question := range view["questions"].([]map[string]any) {
			number++
			question["ask"] = number
		}
		views[run.RunID] = view
	}
	return views
}

func printCurrentWorkflowRun(streams IO, f *facade.Facade, executionErr error) (int, error) {
	if executionErr != nil {
		return 1, executionErr
	}
	runs, err := f.ControlRuns()
	if err != nil {
		return 1, err
	}
	views := workflowControlRunViews(runs)
	view := views[runs[0].RunID]
	children := []map[string]any{}
	for index, run := range runs[1:] {
		children = append(children, map[string]any{"child": index + 1, "run": views[run.RunID]})
	}
	view["children"] = children
	return printJSON(streams.Stdout, view)
}

func numberedChoices(values []string) []map[string]any {
	choices := []map[string]any{}
	for index, value := range values {
		choices = append(choices, map[string]any{"number": index + 1, "label": value})
	}
	return choices
}

func workflowReviewChoices(review facade.ReviewFace) []map[string]any {
	choices := []map[string]any{}
	for actionIndex, action := range review.Actions {
		for candidateIndex, candidate := range action.Candidates {
			if candidate.Excluded {
				continue
			}
			findings := []map[string]any{}
			for findingIndex, finding := range candidate.Findings {
				if finding.EffectiveDecision != "" {
					continue
				}
				f := finding.Evidence
				findings = append(findings, map[string]any{"finding": findingIndex + 1, "severity": finding.Severity, "message": finding.Message, "severityRationale": f.SeverityRationale, "category": f.Category, "requirement": f.Requirement, "normalPath": f.NormalPath, "actualError": f.ActualError, "deferralImpact": f.DeferralImpact, "locations": f.Locations, "verification": f.Verification})
			}
			if len(findings) != 0 {
				choices = append(choices, map[string]any{"review": actionIndex + 1, "action": action.Action, "candidate": candidateIndex + 1, "requirementRevision": candidate.RequirementRevision, "findings": findings})
			}
		}
	}
	return choices
}

func workflowConfirmationView(candidate facade.ControlCandidate) map[string]any {
	run := workflowRunView(candidate.Run)
	questions := []map[string]any{}
	request := ""
	if candidate.Event.Decide != nil {
		request = string(candidate.Event.Decide.Request)
	} else if candidate.Event.Request != nil {
		request = string(candidate.Event.Request.Request)
	}
	if request != "" {
		for index, ask := range candidate.Run.PendingAsks {
			if ask.RequestID == request {
				question := run["questions"].([]map[string]any)[index]
				question["ask"] = candidate.Input.Ask
				questions = append(questions, question)
				break
			}
		}
	}
	run["questions"] = questions
	view := map[string]any{"input": candidate.Input, "run": run}
	if candidate.DevelopmentPlan != nil {
		view["proposal"] = candidate.DevelopmentPlan.Proposal
		view["planDigest"] = candidate.DevelopmentPlan.Confirmation.Digest
	}
	if candidate.Intake != nil {
		view["materials"], view["revision"] = candidate.Intake.Materials, candidate.Intake.Revision
	}
	if candidate.Reset != nil {
		view["reset"] = candidate.Reset
	}
	return view
}

func workflowConsumptionView(value workflowConsumption, runs map[string]map[string]any) map[string]any {
	run := runs[value.Run.RunID]
	view := map[string]any{"run": run, "boundary": value.Boundary, "progress": value.Progress}
	if value.Acceptance != nil {
		view["acceptance"] = map[string]any{"status": value.Acceptance.Status, "revision": value.Acceptance.Revision}
	}
	keys := []string{}
	for key := range value.Blocked {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	blocked := []string{}
	for _, key := range keys {
		blocked = append(blocked, value.Blocked[key])
	}
	view["blocked"] = blocked
	children := []map[string]any{}
	keys = keys[:0]
	for key := range value.Children {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for index, key := range keys {
		child := workflowConsumptionView(value.Children[key], runs)
		child["child"] = index + 1
		children = append(children, child)
	}
	view["children"] = children
	return view
}

func printCurrentWorkflowConsumption(streams IO, f *facade.Facade, value workflowConsumption, machine bool, executionErr error) (int, error) {
	if machine {
		return printWorkflowConsumption(streams, value, executionErr)
	}
	runs, err := f.ControlRuns()
	if err != nil {
		return 1, errors.Join(executionErr, err)
	}
	children := make(map[string]workflowConsumption, len(runs)-1)
	for _, run := range runs[1:] {
		child := value.Children[run.RunID]
		child.Run = run
		children[run.RunID] = child
	}
	value.Run, value.Children = runs[0], children
	return printWorkflowView(streams, value, false, executionErr, runs)
}

func printWorkflowView(streams IO, value workflowConsumption, machine bool, executionErr error, runs []facade.Run) (int, error) {
	if machine {
		return printWorkflowConsumption(streams, value, executionErr)
	}
	views := workflowControlRunViews(runs)
	if code, err := printJSON(streams.Stdout, workflowConsumptionView(value, views)); err != nil {
		return code, err
	}
	if executionErr != nil {
		fmt.Fprintln(streams.Stderr, executionErr)
		if errors.Is(executionErr, context.Canceled) {
			return 130, nil
		}
		return 1, nil
	}
	return 0, nil
}
