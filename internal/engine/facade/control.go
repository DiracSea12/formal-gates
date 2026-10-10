package facade

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/encoder"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/engine/vcs"
)

// ControlInput contains choices and new semantic content. All positions are
// one-based in the current Show projection; zero owner denotes master-merge.
// It carries no event identity, freshness token, original digest or work binding.
type ControlInput struct {
	Request           protocol.ControlKind    `json:"request,omitempty"`
	Ask               int                     `json:"ask,omitempty"`
	Choice            string                  `json:"choice,omitempty"`
	Reason            string                  `json:"reason,omitempty"`
	Gates             []int                   `json:"gates,omitempty"`
	Parallel          string                  `json:"parallel,omitempty"`
	Owners            []int                   `json:"owners,omitempty"`
	Review            int                     `json:"review,omitempty"`
	Candidate         int                     `json:"candidate,omitempty"`
	Finding           int                     `json:"finding,omitempty"`
	Severity          string                  `json:"severity,omitempty"`
	SeverityRationale string                  `json:"severityRationale,omitempty"`
	Invalidity        string                  `json:"invalidity,omitempty"`
	Requirement       *RequirementChangeInput `json:"requirement,omitempty"`
	Validation        *ValidationControlInput `json:"validation,omitempty"`
}

type WorkChoice struct {
	Disposition string `json:"disposition"`
	Reason      string `json:"reason"`
}
type ChildChoice struct {
	Work      []WorkChoice `json:"work"`
	Receipt   string       `json:"receipt,omitempty"`
	Candidate string       `json:"candidate,omitempty"`
	Route     string       `json:"route"`
	Reason    string       `json:"reason"`
}
type RequirementChangeInput struct {
	Classification string                     `json:"classification"`
	Redo           []string                   `json:"redo,omitempty"`
	Impact         protocol.RequirementImpact `json:"impact"`
	Work           []WorkChoice               `json:"work"`
	Children       []ChildChoice              `json:"children,omitempty"`
	Review         int                        `json:"review,omitempty"`
	Candidate      int                        `json:"candidate,omitempty"`
	Finding        int                        `json:"finding,omitempty"`
}
type ValidationControlInput struct {
	Kind        string              `json:"kind,omitempty"`
	Mode        coverage.ReviewKind `json:"mode,omitempty"`
	Category    string              `json:"category,omitempty"`
	Target      int                 `json:"target,omitempty"`
	Cases       []int               `json:"cases,omitempty"`
	Obligations []int               `json:"obligations,omitempty"`
	Remedies    []string            `json:"remedies,omitempty"`
	Remedy      string              `json:"remedy,omitempty"`
}

// ControlCandidate is a read-only, complete confirmation object. Saving it
// neither records a decision nor authorizes a request. Submit retains the core's
// exact replay, freshness and typed authorization checks.
type ControlCandidate struct {
	Root            string                           `json:"root"`
	Input           ControlInput                     `json:"input"`
	Event           protocol.Event                   `json:"event"`
	Run             Run                              `json:"run"`
	DevelopmentPlan *protocol.DevelopmentPlanPreview `json:"developmentPlan,omitempty"`
	Intake          *IntakePreview                   `json:"intake,omitempty"`
	Reset           *protocol.ResetPreview           `json:"reset,omitempty"`
}

func controlDigest(value any) string { data, _ := json.Marshal(value); return encoder.Digest(data) }
func position[T any](values []T, n int, label string) (T, error) {
	if n < 1 || n > len(values) {
		var zero T
		return zero, fmt.Errorf("%s position %d is outside the current %d choices", label, n, len(values))
	}
	return values[n-1], nil
}
func selectStrings(values []string, positions []int, label string) ([]string, error) {
	out := make([]string, len(positions))
	for i, n := range positions {
		value, err := position(values, n, label)
		if err != nil {
			return nil, err
		}
		out[i] = value
	}
	return out, nil
}

func (f *Facade) PrepareControl(input ControlInput) (ControlCandidate, error) {
	run, err := f.Show()
	if err != nil {
		return ControlCandidate{}, err
	}
	candidate := ControlCandidate{Root: f.root, Input: input, Run: run}
	event := protocol.Event{Kind: protocol.KindDecide}
	if input.Request != "" {
		event.Kind = protocol.KindRequestControl
		event.Request = &protocol.RequestPayload{Control: input.Request}
		switch input.Request {
		case protocol.ControlAbort:
			event.Request.Options = []protocol.AskOption{{ID: "abort", Label: "Abort the run"}, {ID: "decline", Label: "Keep the run"}}
		case protocol.ControlReset:
			event.Request.Options = []protocol.AskOption{{ID: "proceed", Label: "Reset"}, {ID: "decline", Label: "Keep the run"}}
		case protocol.ControlRequestRequirementChange:
			event.Request.Options = []protocol.AskOption{{ID: "proceed", Label: "Confirm the complete changed requirement"}, {ID: "abort", Label: "Keep the confirmed requirement"}}
			if input.Requirement != nil {
				pkg, intake, e := f.prepareRequirementChange(run, *input.Requirement)
				if e != nil {
					return candidate, e
				}
				candidate.Intake = &intake
				event.Request.Package = &pkg
				if input.Ask != 0 {
					ask, e := position(run.PendingAsks, input.Ask, "Ask")
					if e != nil {
						return candidate, e
					}
					event.Request.Request = protocol.RequestID(ask.RequestID)
					event.Request.Token = ask.Token
					event.Request.Options = nil
				}
			}
		case protocol.ControlReviewFindingDecision:
			correction, e := correctionFromRun(run, input)
			if e != nil {
				return candidate, e
			}
			option := strings.ToLower(correction.Decision)
			if correction.FindingID != "" {
				option += ":" + correction.FindingID
			}
			event.Request.Options = []protocol.AskOption{{ID: protocol.AskOptionID(option), Label: input.Choice}}
		case protocol.ControlValidation:
			request, e := validationRequestFromRun(run, input.Validation)
			if e != nil {
				return candidate, e
			}
			event.Request.Validation = request
			event.Request.Options = []protocol.AskOption{{ID: "approve", Label: "Approve the displayed action"}, {ID: "decline", Label: "Keep the current obligations"}}
		default:
			return candidate, fmt.Errorf("this request is issued by the engine, not the operator: %s", input.Request)
		}
	} else {
		ask, e := position(run.PendingAsks, input.Ask, "Ask")
		if e != nil {
			return candidate, e
		}
		var choice protocol.AskOptionID
		for _, option := range ask.Options {
			if string(option.ID) == input.Choice {
				choice = option.ID
				break
			}
		}
		// Finding options include program-owned finding identities. The caller names
		// the semantic action; the program binds its selected current finding.
		if ask.Control == protocol.ControlReviewFindingDecision {
			correction, e := correctionFromRun(run, input)
			if e != nil {
				return candidate, e
			}
			option := strings.ToLower(correction.Decision)
			if correction.FindingID != "" {
				option += ":" + correction.FindingID
			}
			for _, o := range ask.Options {
				if strings.EqualFold(string(o.ID), option) {
					choice = o.ID
					break
				}
			}
			event.Decide = &protocol.DecidePayload{Review: &protocol.ReviewDecisionPayload{Correction: &correction}}
		} else {
			event.Decide = &protocol.DecidePayload{}
		}
		if choice == "" {
			return candidate, fmt.Errorf("choice %q is not offered by the current Ask", input.Choice)
		}
		event.Decide.Request, event.Decide.Token, event.Decide.Choice = protocol.RequestID(ask.RequestID), ask.Token, choice
		switch ask.Control {
		case protocol.ControlConfirmTopology:
			topology := protocol.TopologyDecision{Decision: string(choice), Note: input.Reason}
			if choice == "split" {
				owners := map[string]bool{}
				if run.Review.ReadinessProposal != nil {
					for _, batch := range run.Review.ReadinessProposal.Batches {
						if batch.Owner != "" && batch.Owner != "master-merge" && !owners[batch.Owner] {
							owners[batch.Owner] = true
							topology.Slices = append(topology.Slices, batch.Owner)
						}
					}
				}
				count := len(topology.Slices)
				topology.SplitCount, topology.Parallel = count, input.Parallel
				if run.Validation == nil {
					return candidate, fmt.Errorf("split needs the confirmed acceptance conditions")
				}
				var conditions []string
				for _, req := range run.Validation.Document.Requirements {
					for _, ac := range req.AcceptanceConditions {
						conditions = append(conditions, ac.ID)
					}
				}
				if len(input.Owners) != len(conditions) {
					return candidate, fmt.Errorf("split needs one owner selection for each of the %d acceptance conditions", len(conditions))
				}
				for i, n := range input.Owners {
					owner := "master-merge"
					if n != 0 {
						owner, e = position(topology.Slices, n, "slice owner")
						if e != nil {
							return candidate, e
						}
					}
					topology.ACOwners = append(topology.ACOwners, protocol.ACOwner{AC: conditions[i], Owner: owner})
				}
				plan, e := f.DevelopmentPlan(protocol.RouteDecision{}, topology)
				if e != nil {
					return candidate, e
				}
				candidate.DevelopmentPlan = plan
				topology.DevelopmentPlan = &plan.Confirmation
			}
			event.Decide.Review = &protocol.ReviewDecisionPayload{Topology: &topology}
		case protocol.ControlSelectRoute:
			route := protocol.RouteDecision{Mode: string(choice), Gates: append([]string{}, ask.RouteGates...)}
			route.Selected, e = selectStrings(route.Gates, input.Gates, "gate")
			if e != nil {
				return candidate, e
			}
			plan, e := f.DevelopmentPlan(route)
			if e != nil {
				return candidate, e
			}
			candidate.DevelopmentPlan = plan
			route.DevelopmentPlan = &plan.Confirmation
			event.Decide.Review = &protocol.ReviewDecisionPayload{Route: &route}
		case protocol.ControlReviewRoundGrant:
			if choice == "grant" {
				if ask.RoundGrant == nil {
					return candidate, fmt.Errorf("review grant has no current offer")
				}
				event.Decide.Review = &protocol.ReviewDecisionPayload{RoundGrant: &protocol.RoundGrantDecision{Action: ask.RoundGrant.Action, Reason: input.Reason}}
			}
		case protocol.ControlRequestRequirementChange:
			if choice == "proceed" {
				face := run.PendingRequirementAsk
				if face == nil || face.RequestID != ask.RequestID || face.Package == nil {
					return candidate, fmt.Errorf("requirement change needs its complete prepared package")
				}
				p := face.Package
				req, e := f.request()
				if e != nil {
					return candidate, e
				}
				intake, e := PrepareIntake(f.root, req.IntakeConfirmationReceipt.RequirementSource, req.IntakeConfirmationReceipt.RequirementSource, nil, run.Route != "lightweight")
				if e != nil {
					return candidate, e
				}
				revision := intake.Revision
				if strings.HasPrefix(p.CandidateRevision, "sha256:") {
					revision = "sha256:" + revision
				}
				if revision != p.CandidateRevision {
					return candidate, fmt.Errorf("prepared requirement bytes differ from the pending confirmation package")
				}
				candidate.Intake = &intake
				event.Decide.Requirement = &protocol.RequirementDecisionPayload{Coverage: p.Coverage, BaseRevision: face.BaseRevision, CandidateRevision: p.CandidateRevision, DeltaDigest: p.DeltaDigest, ImpactDigest: p.ImpactDigest, Source: p.Source, Classification: p.Classification, RedoProductReview: p.RedoProductReview, RedoStartReadiness: p.RedoStartReadiness, RedoTopology: p.RedoTopology, RedoRoute: p.RedoRoute, WorkDecisions: p.WorkDecisions, Children: p.Children, Impact: p.Impact, ResetRequestID: p.ResetRequestID, FindingDigest: p.FindingDigest, Reason: input.Reason}
			}
		case protocol.ControlReset:
			if choice == "proceed" {
				snap, e := f.engine.Load()
				if e != nil {
					return candidate, e
				}
				preview := snap.State.PendingAsks[ask.RequestID].Reset
				if preview == nil {
					return candidate, fmt.Errorf("reset Ask has no preservation preview")
				}
				candidate.Reset = preview
				event.Decide.Reset = &protocol.ResetDecision{PreviewDigest: preview.Digest, Reason: input.Reason}
			}
		case protocol.ControlValidation:
			if choice != "decline" {
				if ask.Validation == nil {
					return candidate, fmt.Errorf("validation Ask has no current facts")
				}
				v := input.Validation
				if v == nil {
					v = &ValidationControlInput{}
				}
				obligations, e := selectStrings(ask.Validation.Obligations, v.Obligations, "obligation")
				if e != nil {
					return candidate, e
				}
				event.Decide.Validation = &protocol.ValidationDecision{Digest: ask.Validation.Digest, StateRevision: run.Revision, Reason: input.Reason, Obligations: obligations, Remedy: v.Remedy}
			}
		}
	}
	event.ID = protocol.EventID("control:" + controlDigest(struct {
		Run      string
		Revision uint64
		Event    protocol.Event
	}{run.RunID, run.Revision, event}))
	if err := event.Validate(); err != nil {
		return candidate, err
	}
	candidate.Event = event
	return candidate, nil
}

// SubmitControl binds the saved original event to its project and run, then
// delegates replay, freshness and authorization to the original Submit owner.
func (f *Facade) SubmitControl(candidate ControlCandidate) (protocol.Acceptance, Run, error) {
	if candidate.Run.RunID != f.runID || !vcs.SamePath(candidate.Root, f.root) {
		return protocol.Acceptance{}, Run{}, fmt.Errorf("confirmation belongs to a different project or run")
	}
	if candidate.Event.Request == nil && candidate.Event.Decide == nil {
		return protocol.Acceptance{}, Run{}, fmt.Errorf("confirmation contains no program-generated control")
	}
	return f.Submit(candidate.Event)
}

func correctionFromRun(run Run, input ControlInput) (review.CorrectionInput, error) {
	action, e := position(run.Review.Actions, input.Review, "review")
	if e != nil {
		return review.CorrectionInput{}, e
	}
	c, e := position(action.Candidates, input.Candidate, "review candidate")
	if e != nil {
		return review.CorrectionInput{}, e
	}
	out := review.CorrectionInput{Action: review.Action(action.Action), DispatchID: c.DispatchID, ResultDigest: c.ResultDigest, RequirementRevision: c.RequirementRevision, Decision: strings.ToUpper(input.Choice), Reason: input.Reason, Severity: input.Severity, SeverityRationale: input.SeverityRationale, Invalidity: input.Invalidity, ExpectedEffectiveRevision: action.EffectiveRevision}
	if out.Decision != "INVALIDATE_RESULT" {
		finding, e := position(c.Findings, input.Finding, "finding")
		if e != nil {
			return out, e
		}
		out.FindingID = finding.ID
		out.Supersedes = finding.EffectiveDecisionID
	}
	return out, nil
}

func bindWork(work []protocol.RequirementWork, choices []WorkChoice) ([]protocol.RequirementWorkDecision, error) {
	if len(work) != len(choices) {
		return nil, fmt.Errorf("decide each of the %d current work obligations", len(work))
	}
	out := make([]protocol.RequirementWorkDecision, len(work))
	for i, w := range work {
		out[i] = protocol.RequirementWorkDecision{Work: w, Disposition: choices[i].Disposition, Reason: choices[i].Reason}
	}
	return out, nil
}

func (f *Facade) prepareRequirementChange(run Run, input RequirementChangeInput) (protocol.RequirementDecisionPackage, IntakePreview, error) {
	req, e := f.request()
	if e != nil {
		return protocol.RequirementDecisionPackage{}, IntakePreview{}, e
	}
	path := req.IntakeConfirmationReceipt.RequirementSource
	intake, e := PrepareIntake(f.root, path, path, nil, run.Route != "lightweight")
	if e != nil {
		return protocol.RequirementDecisionPackage{}, intake, e
	}
	revision := intake.Revision
	if strings.HasPrefix(run.Review.RequirementRevision, "sha256:") {
		revision = "sha256:" + revision
	}
	p := protocol.RequirementDecisionPackage{CandidateRevision: revision, Classification: input.Classification, Source: protocol.RequirementSourceUserInitiated, Impact: &input.Impact}
	p.DeltaDigest = controlDigest(struct {
		Base, Candidate string
		Changed         []string
	}{run.Review.RequirementRevision, revision, input.Impact.ChangedItems})
	p.ImpactDigest = controlDigest(input.Impact)
	for _, name := range input.Redo {
		switch name {
		case "product-review":
			p.RedoProductReview = true
		case "start-readiness":
			p.RedoStartReadiness = true
		case "topology":
			p.RedoTopology = true
		case "route":
			p.RedoRoute = true
		default:
			return p, intake, fmt.Errorf("unknown requirement redo choice %q", name)
		}
	}
	p.WorkDecisions, e = bindWork(run.RequirementWork, input.Work)
	if e != nil {
		return p, intake, e
	}
	if len(run.RequirementChildren) != len(input.Children) {
		return p, intake, fmt.Errorf("decide every current child")
	}
	for i, original := range run.RequirementChildren {
		choice := input.Children[i]
		work := make([]protocol.RequirementWork, len(original.WorkDecisions))
		for j, w := range original.WorkDecisions {
			work[j] = w.Work
		}
		original.WorkDecisions, e = bindWork(work, choice.Work)
		if e != nil {
			return p, intake, e
		}
		original.ReceiptDisposition, original.CandidateDisposition, original.RouteDisposition, original.Reason = choice.Receipt, choice.Candidate, choice.Route, choice.Reason
		p.Children = append(p.Children, original)
	}
	if input.Finding != 0 {
		action, e := position(run.Review.Actions, input.Review, "review")
		if e != nil {
			return p, intake, e
		}
		cand, e := position(action.Candidates, input.Candidate, "candidate")
		if e != nil {
			return p, intake, e
		}
		finding, e := position(cand.Findings, input.Finding, "finding")
		if e != nil {
			return p, intake, e
		}
		p.FindingDigest = finding.Digest
		p.Source = protocol.RequirementSourceReviewFindingP2
		if finding.Severity == "P0" || finding.Severity == "P1" {
			p.Source = protocol.RequirementSourceReviewFindingP1
		}
	}
	snap, e := f.engine.Load()
	if e != nil {
		return p, intake, e
	}
	for id, d := range snap.State.Decisions {
		if d.Reset != nil && d.Reset.Applied && !d.Reset.Confirmed {
			p.Source = protocol.RequirementSourceReset
			p.ResetRequestID = id
			break
		}
	}
	return p, intake, nil
}

func validationRequestFromRun(run Run, input *ValidationControlInput) (*protocol.ValidationRequest, error) {
	if input == nil || run.Validation == nil {
		return nil, fmt.Errorf("validation request needs its semantic choice and an active pipeline")
	}
	out := &protocol.ValidationRequest{Kind: input.Kind, Mode: input.Mode, Category: input.Category, Remedies: input.Remedies}
	if len(run.Validation.Waves) > 0 {
		wave := run.Validation.Waves[len(run.Validation.Waves)-1]
		out.Target = wave.ID
		out.Binding = protocol.WorkBinding{RequirementRevision: wave.RequirementRevision, Generation: wave.Generation, Candidate: wave.Candidate.Identity, WaveID: wave.ID}
		if input.Kind == "DISPOSE" {
			var targets []string
			for id := range wave.Targets {
				targets = append(targets, id)
			}
			sort.Strings(targets)
			id, e := position(targets, input.Target, "validation target")
			if e != nil {
				return nil, e
			}
			out.Target = id
			out.Binding.Task = wave.Targets[id].Task
		}
	}
	if len(input.Cases) > 0 {
		set := run.Validation.Sets[input.Mode]
		if set == nil {
			return nil, fmt.Errorf("unknown current QA case set")
		}
		var cases []string
		for _, c := range set.Manifest.Cases {
			cases = append(cases, c.CaseID)
		}
		selected, e := selectStrings(cases, input.Cases, "case")
		if e != nil {
			return nil, e
		}
		out.AffectedCases = selected
	}
	return out, nil
}
