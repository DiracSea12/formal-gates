package worker

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"slices"
	"strings"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

// Result contains only worker judgments and authored material. Issued identity
// comes from Material.Input; workflow admission remains in protocol.Submit.
type Result struct {
	Outcome            string                 `json:"outcome"`
	FailureClass       authoring.FailureClass `json:"failureClass,omitempty"`
	InterruptionReason string                 `json:"interruptionReason,omitempty"`
	Review             *Review                `json:"review,omitempty"`
	Validation         *Validation            `json:"validation,omitempty"`
}
type Granularity struct {
	Reason           string     `json:"reason"`
	Batches          []Batch    `json:"batches"`
	VerificationTier string     `json:"verificationTier"`
	CheckpointGroups [][]string `json:"checkpointGroups,omitempty"`
	CheckpointReason string     `json:"checkpointReason,omitempty"`
}
type Batch struct {
	protocol.DevelopmentBatch
	Route *RouteProposal `json:"route,omitempty"`
}
type RouteProposal struct {
	Mode     string   `json:"mode"`
	Gates    []string `json:"gates"`
	Selected []string `json:"selected,omitempty"`
}
type Review struct {
	GranularityReview *Granularity      `json:"granularityReview,omitempty"`
	Status            string            `json:"status"`
	Message           string            `json:"message,omitempty"`
	Findings          []Finding         `json:"findings"`
	Verification      map[string]string `json:"verification"`
}
type Finding struct {
	Severity          string                     `json:"severity"`
	SeverityRationale string                     `json:"severityRationale,omitempty"`
	Category          string                     `json:"category,omitempty"`
	Requirement       string                     `json:"requirement,omitempty"`
	NormalPath        string                     `json:"normalPath,omitempty"`
	ActualError       string                     `json:"actualError,omitempty"`
	DeferralImpact    string                     `json:"deferralImpact,omitempty"`
	Verification      review.FindingVerification `json:"verification,omitempty"`
	Message           string                     `json:"message"`
	Locations         []string                   `json:"locations,omitempty"`
}
type Development struct {
	Completion    *protocol.DevelopmentCompletion `json:"completion,omitempty"`
	Paths         []string                        `json:"paths"`
	Summary       string                          `json:"summary"`
	AffectedCases []string                        `json:"affectedCases,omitempty"`
}

func development(value *Development) *protocol.DevelopmentEvidence {
	if value == nil {
		return nil
	}
	return &protocol.DevelopmentEvidence{Completion: value.Completion, Paths: value.Paths, Summary: value.Summary, AffectedCases: value.AffectedCases}
}

type Validation struct {
	RepairVerification *Execution                   `json:"repairVerification,omitempty"`
	Status             string                       `json:"status,omitempty"`
	GranularityReview  *Granularity                 `json:"granularityReview,omitempty"`
	Resolution         *protocol.ConflictResolution `json:"resolution,omitempty"`
	Development        *Development                 `json:"development,omitempty"`
	Manifest           *Manifest                    `json:"manifest,omitempty"`
	Review             *CaseReview                  `json:"review,omitempty"`
	Suggestions        []Suggestion                 `json:"suggestions,omitempty"`
	Execution          *Execution                   `json:"execution,omitempty"`
	Checkpoint         bool                         `json:"checkpoint,omitempty"`
	Findings           []Finding                    `json:"findings,omitempty"`
}
type Suggestion struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Reason   string `json:"reason,omitempty"`
}
type Manifest struct {
	SharedSteps []SharedStep        `json:"sharedSteps,omitempty"`
	Cases       []Case              `json:"cases"`
	Coverage    map[string][]string `json:"coverage"`
}
type SharedStep struct {
	ID         string               `json:"id"`
	Procedure  string               `json:"procedure"`
	Operations []coverage.Operation `json:"operations,omitempty"`
	Files      *[]string            `json:"files,omitempty"`
}
type Case struct {
	Files         *[]string            `json:"files,omitempty"`
	Operations    []coverage.Operation `json:"operations,omitempty"`
	Scenario      string               `json:"scenario"`
	SharedSteps   []string             `json:"sharedSteps,omitempty"`
	CaseID        string               `json:"caseId"`
	PublicEntry   string               `json:"publicEntry,omitempty"`
	Preconditions string               `json:"preconditions,omitempty"`
	Steps         string               `json:"steps,omitempty"`
	Oracle        string               `json:"oracle"`
	TestRef       string               `json:"testRef,omitempty"`
}
type Decision struct {
	Status coverage.DecisionStatus `json:"status"`
	Reason string                  `json:"reason"`
}
type CaseReview struct {
	SourceDecisions     map[string]Decision `json:"sourceDecisions"`
	PointDecisions      map[string]Decision `json:"pointDecisions"`
	CaseDecisions       map[string]Decision `json:"caseDecisions"`
	SharedStepDecisions map[string]Decision `json:"sharedStepDecisions,omitempty"`
}
type Execution struct {
	Records map[string]Observation `json:"records"`
}
type Observation struct {
	Comparison string                   `json:"comparison"`
	Result     coverage.ExecutionStatus `json:"result"`
}

func DecodeResult(input protocol.WorkerInput, eventID protocol.EventID, raw []byte, originalDigest string) (protocol.Event, error) {
	var value Result
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return protocol.Event{}, err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return protocol.Event{}, fmt.Errorf("worker result must contain one JSON object")
	}
	if input.Handler == "engine.repair.verify" && value.Validation != nil && value.Validation.Status != "" {
		return protocol.Event{}, fmt.Errorf("repair comparison status is derived from case judgments")
	}
	if originalDigest == "" {
		return protocol.Event{}, fmt.Errorf("verified native original output digest is required")
	}
	payload := protocol.WorkerResultPayload{ActionID: input.Pending.ActionID, Provider: input.Provider, PayloadDigest: originalDigest, Outcome: value.Outcome, FailureClass: value.FailureClass, InterruptionReason: value.InterruptionReason}
	if input.Handler == "engine.development.batch" && input.Validation != nil && input.Validation.Development != nil && input.Validation.Development.Result != nil {
		payload.SourceResult = input.Validation.Development.Result.Digest
	}
	if value.Review != nil {
		if input.Review == nil || value.Validation != nil {
			return protocol.Event{}, fmt.Errorf("worker review result has wrong role")
		}
		r := value.Review
		body := protocol.ReviewResultEvidence{GranularityReview: granularity(r.GranularityReview), Status: r.Status, Message: r.Message, Findings: findings(r.Findings)}
		body.Action, body.DispatchID, body.RequirementRevision, body.AuthorizationID = input.Review.Action, input.Pending.ActionID, input.Review.RequirementRevision, input.Review.AuthorizationID
		checks := review.RequiredVerificationChecks()
		if len(value.Review.Verification) != len(checks) {
			return protocol.Event{}, fmt.Errorf("review requires evidence for every verification check")
		}
		evidence := []string{}
		for _, key := range checks {
			text := strings.TrimSpace(value.Review.Verification[key])
			if text == "" {
				return protocol.Event{}, fmt.Errorf("review verification evidence is missing: %s", key)
			}
			evidence = append(evidence, key+": "+text)
		}
		body.Verification = review.OperatorVerification{Checks: checks, Evidence: strings.Join(evidence, "\n")}
		payload.Review = &body
	}
	if value.Validation != nil {
		if input.Validation == nil || value.Review != nil {
			return protocol.Event{}, fmt.Errorf("worker validation result has wrong role")
		}
		issued := input.Validation
		v := value.Validation
		if input.Handler == "engine.qa.design" && v.Development != nil {
			return protocol.Event{}, fmt.Errorf("QA design delivery paths come from manifest file relations")
		}
		if v.Development != nil && v.Development.Completion != nil && completionBatch(input) == nil && (v.GranularityReview == nil || len(v.GranularityReview.Batches) != 1) {
			return protocol.Event{}, fmt.Errorf("completion requires the issued or proposed single development batch")
		}
		body := protocol.ValidationResult{Status: v.Status, GranularityReview: granularity(v.GranularityReview), Resolution: v.Resolution, Development: development(v.Development), Checkpoint: v.Checkpoint, Findings: findings(v.Findings)}
		body.Binding = issued.Binding
		if input.Handler == "engine.development.batch" && body.Development != nil && issued.Development != nil && issued.Development.Result != nil {
			prior := issued.Development.Result.Validation.Development
			body.Development.Paths = append(body.Development.Paths, prior.Paths...)
			slices.Sort(body.Development.Paths)
			body.Development.Paths = slices.Compact(body.Development.Paths)
		}
		for _, suggestion := range v.Suggestions {
			body.Suggestions = append(body.Suggestions, protocol.QASuggestion{Severity: suggestion.Severity, Message: suggestion.Message, Reason: suggestion.Reason})
		}
		if value.Validation.Manifest != nil {
			manifest := coverage.AcceptanceManifest{}
			for _, item := range v.Manifest.SharedSteps {
				step := coverage.SharedStep{ID: item.ID, Procedure: item.Procedure, Operations: slices.Clone(item.Operations)}
				if item.Files != nil {
					step.Files = slices.Clone(*item.Files)
				} else {
					for _, original := range issued.Manifest.SharedSteps {
						if original.ID == item.ID {
							step.Files = slices.Clone(original.Files)
						}
					}
				}
				manifest.SharedSteps = append(manifest.SharedSteps, step)
			}
			for _, item := range v.Manifest.Cases {
				c := coverage.AcceptanceCase{Operations: slices.Clone(item.Operations), Scenario: item.Scenario, SharedSteps: slices.Clone(item.SharedSteps), CaseID: item.CaseID, PublicEntry: item.PublicEntry, Preconditions: item.Preconditions, Steps: item.Steps, Oracle: item.Oracle, TestRef: item.TestRef}
				if item.Files != nil {
					c.Files = slices.Clone(*item.Files)
				} else {
					for _, original := range issued.Manifest.Cases {
						if original.CaseID == item.CaseID {
							c.Files = slices.Clone(original.Files)
						}
					}
				}
				manifest.Cases = append(manifest.Cases, c)
			}
			manifest.Binding, manifest.Sources, manifest.Points = issued.Manifest.Binding, slices.Clone(issued.Manifest.Sources), slices.Clone(issued.Manifest.Points)
			for i := range manifest.Cases {
				manifest.Cases[i].Mode = coverage.CaseMode(manifest.Binding.ReviewKind)
			}
			points := map[string]bool{}
			for _, point := range issued.Manifest.Points {
				points[point.PointID] = true
			}
			for _, point := range sortedKeys(value.Validation.Manifest.Coverage) {
				if !points[point] {
					return protocol.Event{}, fmt.Errorf("coverage selects unknown point: %s", point)
				}
				for _, caseID := range value.Validation.Manifest.Coverage[point] {
					for _, link := range issued.SourcePoints {
						if link.PointID == point {
							manifest.Edges = append(manifest.Edges, coverage.CoverageEdge{ReviewKind: manifest.Binding.ReviewKind, SourceID: link.SourceID, PointID: point, CaseID: caseID})
						}
					}
				}
			}
			body.Manifest = &manifest
			if input.Handler == "engine.qa.design" && manifest.Binding.ReviewKind == coverage.ReviewWhitebox {
				if paths := manifest.TestFiles(); len(paths) > 0 {
					body.Development = &protocol.DevelopmentEvidence{Paths: paths}
				}
			}
		}
		if value.Validation.Review != nil {
			if issued.ReviewScope == "" {
				return protocol.Event{}, fmt.Errorf("case review lacks issued scope")
			}
			result := coverage.ReviewResult{Binding: issued.Manifest.Binding, Scope: issued.ReviewScope}
			sources := slices.Clone(issued.Manifest.Sources)
			points, cases, shared := []string{}, []string{}, []string{}
			for _, point := range issued.Manifest.Points {
				points = append(points, point.PointID)
			}
			for _, item := range issued.Manifest.Cases {
				cases = append(cases, item.CaseID)
			}
			for _, item := range issued.Manifest.SharedSteps {
				shared = append(shared, item.ID)
			}
			var err error
			if result.SourceDecisions, err = decisions(value.Validation.Review.SourceDecisions, sources, nil); err != nil {
				return protocol.Event{}, err
			}
			if result.PointDecisions, err = decisions(value.Validation.Review.PointDecisions, points, nil); err != nil {
				return protocol.Event{}, err
			}
			if result.CaseDecisions, err = decisions(value.Validation.Review.CaseDecisions, cases, issued.InheritableReviews); err != nil {
				return protocol.Event{}, err
			}
			if result.SharedStepDecisions, err = decisions(value.Validation.Review.SharedStepDecisions, shared, issued.InheritableSharedReviews); err != nil {
				return protocol.Event{}, err
			}
			result.SetStatus = result.Verdict()
			for _, id := range sources {
				if !slices.ContainsFunc(issued.Manifest.Edges, func(edge coverage.CoverageEdge) bool { return edge.SourceID == id }) {
					result.UnboundSources = append(result.UnboundSources, id)
				}
			}
			for _, id := range points {
				if !slices.ContainsFunc(issued.Manifest.Edges, func(edge coverage.CoverageEdge) bool { return edge.PointID == id }) {
					result.UnboundPoints = append(result.UnboundPoints, id)
				}
			}
			for _, id := range cases {
				if !slices.ContainsFunc(issued.Manifest.Edges, func(edge coverage.CoverageEdge) bool { return edge.CaseID == id }) {
					result.UnboundCases = append(result.UnboundCases, id)
				}
			}
			body.Review = &result
		}
		if v.RepairVerification != nil {
			if input.Handler != "engine.repair.verify" || issued.Repair == nil || issued.Repair.FinalCandidate == nil {
				return protocol.Event{}, fmt.Errorf("repair comparison lacks its issued native responsibility")
			}
			if len(v.RepairVerification.Records) != len(issued.Repair.VerificationCases) {
				return protocol.Event{}, fmt.Errorf("repair comparison must cover exactly its issued affected cases")
			}
			d := protocol.DevelopmentEvidence{Paths: slices.Clone(issued.Repair.VerificationPaths), AffectedCases: slices.Clone(issued.Repair.VerificationCases), Summary: "Compared the approved affected cases against native execution evidence"}
			body.Status = protocol.OutcomePass
			for _, caseID := range issued.Repair.VerificationCases {
				index := slices.IndexFunc(issued.ExecutionFacts, func(fact protocol.ExecutionFact) bool { return fact.CaseID == caseID })
				if index < 0 {
					return protocol.Event{}, fmt.Errorf("repair case has no complete issued native facts: %s", caseID)
				}
				comparison, present := v.RepairVerification.Records[caseID]
				if !present || strings.TrimSpace(comparison.Comparison) == "" {
					return protocol.Event{}, fmt.Errorf("repair case lacks an oracle comparison: %s", caseID)
				}
				procedure, observation, err := issued.ExecutionFacts[index].Render()
				if err != nil {
					return protocol.Event{}, err
				}
				for _, operation := range issued.ExecutionFacts[index].Operations {
					if operation.Parameters.QA.GoTestRef != "" && operation.Evidence.GoResult != "pass" && comparison.Result == coverage.ExecutedPass {
						return protocol.Event{}, fmt.Errorf("repair Go test did not actually pass: %s", caseID)
					}
				}
				var mode coverage.ReviewKind
				for _, manifest := range issued.Contract.Manifests {
					if slices.ContainsFunc(manifest.Cases, func(item coverage.AcceptanceCase) bool { return item.CaseID == caseID }) {
						mode = manifest.Binding.ReviewKind
					}
				}
				d.CaseRuns = append(d.CaseRuns, protocol.RepairCaseEvidence{Mode: mode, CaseID: caseID, Procedure: procedure, Observation: observation, Comparison: comparison.Comparison, Result: comparison.Result})
				if comparison.Result != coverage.ExecutedPass {
					body.Status = protocol.OutcomeFail
				}
			}
			body.Development = &d
		}

		if value.Validation.Execution != nil {
			if issued.Execution == nil || issued.Whitelist == nil {
				return protocol.Event{}, fmt.Errorf("execution lacks issued whitelist and binding")
			}
			report := coverage.ExecutionReport{Binding: *issued.Execution}
			report.Binding.ActualCaseIDs = nil
			for _, caseID := range sortedKeys(value.Validation.Execution.Records) {
				if !slices.Contains(issued.PendingCases, caseID) || !slices.Contains(issued.RequiredCases, caseID) {
					return protocol.Event{}, fmt.Errorf("execution selects a case outside pending scope: %s", caseID)
				}
				index := slices.IndexFunc(issued.ExecutionFacts, func(fact protocol.ExecutionFact) bool { return fact.CaseID == caseID })
				if index < 0 {
					return protocol.Event{}, fmt.Errorf("execution case has no issued complete native facts: %s", caseID)
				}
				fact := issued.ExecutionFacts[index]
				procedure, actual, err := fact.Render()
				if err != nil {
					return protocol.Event{}, err
				}
				observation := value.Validation.Execution.Records[caseID]
				for _, op := range fact.Operations {
					if op.Parameters.QA.GoTestRef != "" && op.Evidence.GoResult != "pass" && observation.Result == coverage.ExecutedPass {
						return protocol.Event{}, fmt.Errorf("Go test did not actually pass: %s", caseID)
					}
				}
				for _, edge := range issued.Whitelist.Entries {
					complete := slices.ContainsFunc(issued.Completed, func(row coverage.ExecutionRecord) bool {
						return row.ReviewKind == edge.ReviewKind && row.SourceID == edge.SourceID && row.PointID == edge.PointID && row.CaseID == edge.CaseID
					})
					if edge.CaseID == caseID && !complete {
						report.Records = append(report.Records, coverage.ExecutionRecord{ReviewKind: edge.ReviewKind, SourceID: edge.SourceID, PointID: edge.PointID, CaseID: caseID, Procedure: procedure, Observation: actual, Comparison: observation.Comparison, Result: observation.Result, Provenance: coverage.ProvenanceExecuted})
					}
				}
			}
			for _, inherited := range issued.Inherited {
				if !slices.ContainsFunc(issued.Completed, func(row coverage.ExecutionRecord) bool { return sameEdge(row, inherited) }) {
					report.Records = append(report.Records, inherited)
				}
			}
			for _, row := range report.Records {
				if !slices.Contains(report.Binding.ActualCaseIDs, row.CaseID) {
					report.Binding.ActualCaseIDs = append(report.Binding.ActualCaseIDs, row.CaseID)
				}
			}
			slices.Sort(report.Binding.ActualCaseIDs)
			for _, edge := range issued.Whitelist.Entries {
				present := func(rows []coverage.ExecutionRecord) bool {
					return slices.ContainsFunc(rows, func(row coverage.ExecutionRecord) bool {
						return row.ReviewKind == edge.ReviewKind && row.SourceID == edge.SourceID && row.PointID == edge.PointID && row.CaseID == edge.CaseID
					})
				}
				if !present(report.Records) && !present(issued.Completed) {
					report.NotExecuted = append(report.NotExecuted, coverage.CoverageEdge{ReviewKind: edge.ReviewKind, SourceID: edge.SourceID, PointID: edge.PointID, CaseID: edge.CaseID})
				}
			}
			body.Tool, body.Environment = protocol.ExecutionIdentities(issued.ExecutionFacts)
			body.Execution = &report
		}
		payload.Validation = &body
	}
	if payload.Outcome == protocol.OutcomePass && (input.Review != nil && payload.Review == nil || input.Validation != nil && payload.Validation == nil) {
		return protocol.Event{}, fmt.Errorf("worker result lacks issued role evidence")
	}
	event := protocol.Event{ID: eventID, Kind: protocol.KindWorkerResult, Result: &payload}
	return event, event.Validate()
}

func sortedKeys[T any](values map[string]T) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}
func sameEdge(left, right coverage.ExecutionRecord) bool {
	return left.ReviewKind == right.ReviewKind && left.SourceID == right.SourceID && left.PointID == right.PointID && left.CaseID == right.CaseID
}
func decisions(values map[string]Decision, ids []string, inherited []coverage.ReviewDecision) ([]coverage.ReviewDecision, error) {
	for key := range values {
		if !slices.Contains(ids, key) {
			return nil, fmt.Errorf("review selects unknown item: %s", key)
		}
	}
	result := []coverage.ReviewDecision{}
	for _, id := range ids {
		if value, exists := values[id]; exists {
			result = append(result, coverage.ReviewDecision{ID: id, Status: value.Status, Reason: value.Reason})
			continue
		}
		prior := slices.IndexFunc(inherited, func(item coverage.ReviewDecision) bool { return item.ID == id })
		if prior < 0 {
			return nil, fmt.Errorf("review decision is missing: %s", id)
		}
		value := inherited[prior]
		value.Inherited = true
		result = append(result, value)
	}
	return result, nil
}

func findings(values []Finding) []review.Finding {
	if values == nil {
		return nil
	}
	result := make([]review.Finding, 0, len(values))
	for _, item := range values {
		result = append(result, review.Finding{Severity: item.Severity, SeverityRationale: item.SeverityRationale, Category: item.Category, Requirement: item.Requirement, NormalPath: item.NormalPath, ActualError: item.ActualError, DeferralImpact: item.DeferralImpact, Verification: item.Verification, Message: item.Message, Locations: slices.Clone(item.Locations)})
	}
	return result
}

func granularity(value *Granularity) *protocol.GranularityReview {
	if value == nil {
		return nil
	}
	result := &protocol.GranularityReview{Reason: value.Reason, VerificationTier: value.VerificationTier, CheckpointGroups: value.CheckpointGroups, CheckpointReason: value.CheckpointReason}
	for _, item := range value.Batches {
		batch := item.DevelopmentBatch
		batch.Route = nil
		if item.Route != nil {
			batch.Route = &protocol.RouteDecision{Mode: item.Route.Mode, Gates: slices.Clone(item.Route.Gates), Selected: slices.Clone(item.Route.Selected)}
		}
		result.Batches = append(result.Batches, batch)
	}
	return result
}
