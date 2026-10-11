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
	"formal-gates/internal/requirement"
)

// Interruption reports an inability to complete the issued semantic duty. Native
// transport failures and their classification belong to the host adapter.
type Interruption struct {
	InterruptionReason string `json:"interruptionReason"`
}
type DevelopmentContinuation struct {
	Summary       string `json:"summary"`
	RemainingWork string `json:"remainingWork"`
}
type Repair struct {
	Completion        *Completion  `json:"completion,omitempty"`
	Summary           string       `json:"summary,omitempty"`
	AffectedCases     []int        `json:"affectedCases,omitempty"`
	GranularityReview *Granularity `json:"granularityReview,omitempty"`
}
type Design struct {
	DesignManifest
	Checkpoint bool `json:"checkpoint,omitempty"`
}
type CommandBindings struct {
	Commands [][]OperationChange `json:"commands"`
}

type ExecutionJudgments struct {
	Execution
	Findings []Finding `json:"findings,omitempty"`
}
type ExecutionInterruption struct {
	InterruptionReason string         `json:"interruptionReason"`
	Records            []*Observation `json:"records,omitempty"`
	Findings           []Finding      `json:"findings,omitempty"`
}
type Gate struct {
	Findings []Finding `json:"findings"`
}
type ConflictResolution struct {
	Summary string `json:"summary"`
}

// decodedResult is the role-selected assembly input, never a model wire format.
type decodedResult struct {
	Review      *Review
	Validation  *decodedValidation
	Granularity *protocol.GranularityReview
}
type Granularity struct {
	changes          *GranularityChange
	Reason           string  `json:"reason"`
	Batches          []Batch `json:"batches"`
	VerificationTier string  `json:"verificationTier"`
	CheckpointGroups [][]int `json:"checkpointGroups,omitempty"`
	CheckpointReason string  `json:"checkpointReason,omitempty"`
}
type Batch struct {
	Reuse         int  `json:"reuse,omitempty"`
	Remove        bool `json:"remove,omitempty"`
	changes       *BatchChange
	Owner         *int           `json:"owner,omitempty"`
	DependsOn     []int          `json:"dependsOn,omitempty"`
	Route         *RouteProposal `json:"route,omitempty"`
	Subtasks      []Subtask      `json:"subtasks"`
	Deliverable   string         `json:"deliverable"`
	PublicEntry   string         `json:"publicEntry"`
	Artifacts     []Location     `json:"artifacts,omitempty"`
	NewArtifacts  []string       `json:"newArtifacts,omitempty"`
	Interfaces    string         `json:"interfaces"`
	StateBoundary string         `json:"stateBoundary"`
	Risk          string         `json:"risk"`
	DoD           []Criterion    `json:"dod"`
	Checks        []Check        `json:"checks,omitempty"`
	Rollback      string         `json:"rollback"`
	Recovery      string         `json:"recovery"`
	Handoff       string         `json:"handoff"`
	ContextCost   string         `json:"contextCost"`
	Spike         bool           `json:"spike,omitempty"`
}
type Subtask struct {
	Reuse       int    `json:"reuse,omitempty"`
	Remove      bool   `json:"remove,omitempty"`
	Description string `json:"description"`
}
type Criterion struct {
	Reuse       int    `json:"reuse,omitempty"`
	Remove      bool   `json:"remove,omitempty"`
	Description string `json:"description"`
}
type Check struct {
	Reuse   int    `json:"reuse,omitempty"`
	Read    int    `json:"read,omitempty"`
	From    int    `json:"from,omitempty"`
	To      int    `json:"to,omitempty"`
	Purpose string `json:"purpose,omitempty"`
	Command string `json:"command,omitempty"`
}

func (item *Check) UnmarshalJSON(raw []byte) error {
	type body Check
	if err := decodeObject(raw, (*body)(item)); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	allowed, required := []string{"command", "purpose"}, []string{"command"}
	if _, present := fields["reuse"]; present {
		allowed, required = []string{"reuse"}, []string{"reuse"}
	} else if _, present := fields["read"]; present {
		allowed, required = []string{"read", "from", "to", "purpose"}, []string{"read", "from", "to"}
	}
	for name := range fields {
		if !slices.Contains(allowed, name) {
			return fmt.Errorf("check selects one approved check, original read range, or new command")
		}
	}
	for _, name := range required {
		if value, present := fields[name]; !present || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return fmt.Errorf("check requires %s", name)
		}
	}
	return nil
}

type RouteProposal struct {
	Mode     string `json:"mode"`
	Selected []int  `json:"selected,omitempty"`
}
type Completion struct {
	DoD     []string `json:"dod"`
	Handoff string   `json:"handoff"`
}
type Review struct {
	GranularityReview *Granularity `json:"granularityReview,omitempty"`
	Message           string       `json:"message,omitempty"`
	Findings          []Finding    `json:"findings"`
	Verification      []string     `json:"verification"`
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
	Locations         []Location                 `json:"locations,omitempty"`
}
type Development struct {
	RemainingWork string      `json:"-"`
	Completion    *Completion `json:"completion,omitempty"`
	paths         []string
	Summary       string `json:"summary"`
}

func development(value *Development) *protocol.DevelopmentEvidence {
	if value == nil {
		return nil
	}
	return &protocol.DevelopmentEvidence{RemainingWork: value.RemainingWork, Completion: nil, Paths: value.paths, Summary: value.Summary, AffectedCases: nil}
}

type decodedValidation struct {
	Observations        *ObservationJudgments
	RepairVerification  *Execution
	Status              string
	GranularityReview   *Granularity
	Resolution          *protocol.ConflictResolution
	Development         *Development
	RepairAffectedCases []int
	QACommands          []protocol.QACaseOperations
	Manifest            *Manifest
	Review              *CaseReview
	Suggestions         []Suggestion
	Execution           *Execution
	Checkpoint          bool
	Findings            []Finding
}
type Suggestion struct {
	Severity string `json:"severity"`
	Message  string `json:"message"`
	Reason   string `json:"reason,omitempty"`
}
type DesignManifest struct {
	Replace           bool                 `json:"replace,omitempty"`
	RemoveCases       []int                `json:"removeCases,omitempty"`
	RemoveSharedSteps []int                `json:"removeSharedSteps,omitempty"`
	SharedSteps       []DesignedSharedStep `json:"sharedSteps,omitempty"`
	Cases             []DesignedCase       `json:"cases"`
	Coverage          [][]int              `json:"coverage"`
}
type DesignedSharedStep struct {
	changes     *SharedStepChange
	Artifacts   []string             `json:"artifacts,omitempty"`
	Independent bool                 `json:"independent,omitempty"`
	Step        int                  `json:"step,omitempty"`
	Procedure   string               `json:"procedure"`
	Operations  []coverage.Operation `json:"operations,omitempty"`
	Files       *[]Location          `json:"files,omitempty"`
}
type DesignedCase struct {
	changes       *CaseChange
	Case          int                  `json:"case,omitempty"`
	Files         *[]Location          `json:"files,omitempty"`
	Operations    []coverage.Operation `json:"operations,omitempty"`
	Scenario      string               `json:"scenario"`
	SharedSteps   []int                `json:"sharedSteps,omitempty"`
	PublicEntry   string               `json:"publicEntry,omitempty"`
	Preconditions string               `json:"preconditions,omitempty"`
	Steps         string               `json:"steps,omitempty"`
	Oracle        string               `json:"oracle"`
	TestFile      int                  `json:"testFile,omitempty"`
	TestSymbol    int                  `json:"testSymbol,omitempty"`
}
type Manifest struct {
	Replace           bool                `json:"replace,omitempty"`
	RemoveCases       []string            `json:"removeCases,omitempty"`
	RemoveSharedSteps []string            `json:"removeSharedSteps,omitempty"`
	SharedSteps       []SharedStep        `json:"sharedSteps,omitempty"`
	Cases             []Case              `json:"cases"`
	Coverage          map[string][]string `json:"coverage"`
}
type SharedStep struct {
	Artifacts   []string             `json:"artifacts,omitempty"`
	Independent bool                 `json:"independent,omitempty"`
	ID          string               `json:"id"`
	Procedure   string               `json:"procedure"`
	Operations  []coverage.Operation `json:"operations,omitempty"`
	Files       *[]Location          `json:"files,omitempty"`
}
type Case struct {
	Files         *[]Location          `json:"files,omitempty"`
	Operations    []coverage.Operation `json:"operations,omitempty"`
	Scenario      string               `json:"scenario"`
	SharedSteps   []string             `json:"sharedSteps,omitempty"`
	CaseID        string               `json:"caseId"`
	PublicEntry   string               `json:"publicEntry,omitempty"`
	Preconditions string               `json:"preconditions,omitempty"`
	Steps         string               `json:"steps,omitempty"`
	Oracle        string               `json:"oracle"`
	TestFile      int                  `json:"testFile,omitempty"`
	TestSymbol    int                  `json:"testSymbol,omitempty"`
}
type Decision struct {
	Status coverage.DecisionStatus `json:"status"`
	Reason string                  `json:"reason"`
}
type CaseReview struct {
	Suggestions         []Suggestion `json:"suggestions,omitempty"`
	SourceDecisions     []*Decision  `json:"sourceDecisions"`
	PointDecisions      []*Decision  `json:"pointDecisions"`
	CaseDecisions       []*Decision  `json:"caseDecisions"`
	SharedStepDecisions []*Decision  `json:"sharedStepDecisions,omitempty"`
}
type Execution struct {
	Records []*Observation `json:"records"`
}
type Observation struct {
	Comparison string                   `json:"comparison"`
	Result     coverage.ExecutionStatus `json:"result"`
}

// InvalidResult binds program rejection to the original issued input and output.
func InvalidResult(input protocol.WorkerInput, eventID protocol.EventID, digest, reason string) (protocol.Event, error) {
	event, err := protocol.NewInvalidWorkerResultEvent(eventID, input.Pending.ActionID, input.Provider, digest, reason)
	if err != nil {
		return protocol.Event{}, err
	}
	if prior := developmentSource(input); prior != nil {
		event.Result.SourceResult = prior.Digest
	}
	return event, event.Validate()
}

func DecodeResult(input protocol.WorkerInput, eventID protocol.EventID, raw []byte, originalDigest string) (protocol.Event, error) {
	return DecodeResultWithPaths(input, eventID, raw, originalDigest, nil)
}

// DecodeResultWithPaths assembles the native terminal's captured VCS changes.
// The caller supplies the original source-bound facts, including on recovery.
func DecodeResultWithPaths(input protocol.WorkerInput, eventID protocol.EventID, raw []byte, originalDigest string, paths []string) (protocol.Event, error) {
	return DecodeResultWithFacts(input, eventID, raw, originalDigest, paths, nil)
}

func DecodeResultWithFacts(input protocol.WorkerInput, eventID protocol.EventID, raw []byte, originalDigest string, paths []string, locationFacts []LocationFact) (protocol.Event, error) {
	value, interruption, err := decodeRoleBody(input, raw, locationFacts)
	if err != nil {
		return protocol.Event{}, err
	}
	if originalDigest == "" {
		return protocol.Event{}, fmt.Errorf("verified native original output digest is required")
	}
	var proposed *Granularity
	if value.Review != nil {
		proposed = value.Review.GranularityReview
	} else if value.Validation != nil {
		proposed = value.Validation.GranularityReview
	}
	completed := input.Handler == "engine.repair.v5" && value.Validation != nil && value.Validation.Development != nil && value.Validation.Development.Completion != nil
	value.Granularity, err = DecodeGranularity(input, proposed, paths, locationFacts, completed)
	if err != nil {
		return protocol.Event{}, err
	}
	payload := protocol.WorkerResultPayload{ActionID: input.Pending.ActionID, Provider: input.Provider, PayloadDigest: originalDigest, Outcome: protocol.OutcomePass}
	if interruption != "" {
		payload.Outcome = protocol.OutcomeRuntimeError
		payload.FailureClass = authoring.FailureUserActionRequired
		payload.InterruptionReason = interruption
	}
	if prior := developmentSource(input); prior != nil {
		payload.SourceResult = prior.Digest
	}
	if value.Review != nil {
		if input.Review == nil || value.Validation != nil {
			return protocol.Event{}, fmt.Errorf("worker review result has wrong role")
		}
		r := value.Review
		items, err := findings(r.Findings, locationFacts, input.RequirementDocument)
		if err != nil {
			return protocol.Event{}, err
		}
		body := protocol.ReviewResultEvidence{GranularityReview: value.Granularity, Status: findingStatus(r.Findings), Message: r.Message, Findings: items}
		body.Action, body.DispatchID, body.RequirementRevision, body.AuthorizationID = input.Review.Action, input.Pending.ActionID, input.Review.RequirementRevision, input.Review.AuthorizationID
		checks := review.RequiredVerificationChecks()
		if len(value.Review.Verification) != len(checks) {
			return protocol.Event{}, fmt.Errorf("review requires evidence for every verification check")
		}
		evidence := []string{}
		for _, key := range checks {
			text := strings.TrimSpace(value.Review.Verification[len(evidence)])
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
		if v.Development != nil {
			if paths == nil {
				return protocol.Event{}, fmt.Errorf("development requires original native terminal path facts")
			}
			v.Development.paths = slices.Clone(paths)
		}
		if v.Development != nil && v.Development.Completion != nil && completionBatch(input) == nil && (v.GranularityReview == nil || len(v.GranularityReview.Batches) != 1) {
			return protocol.Event{}, fmt.Errorf("completion requires the issued or proposed single development batch")
		}
		items, err := findings(v.Findings, locationFacts, input.RequirementDocument)
		if err != nil {
			return protocol.Event{}, err
		}
		body := protocol.ValidationResult{Status: v.Status, GranularityReview: value.Granularity, Resolution: v.Resolution, Development: development(v.Development), Checkpoint: v.Checkpoint, Findings: items}
		body.Binding = issued.Binding
		body.QACommands = v.QACommands
		if body.Development != nil {
			batch := completionBatch(input)
			if batch == nil && body.GranularityReview != nil && len(body.GranularityReview.Batches) == 1 {
				batch = &body.GranularityReview.Batches[0]
			}
			var err error
			body.Development.Completion, err = assembleCompletion(v.Development.Completion, batch)
			if err != nil {
				return protocol.Event{}, err
			}
			body.Development.AffectedCases, err = selectIDs(v.RepairAffectedCases, repairCaseIDs(issued))
			if err != nil {
				return protocol.Event{}, err
			}
		}
		if v.Observations != nil {
			observations, err := assembleObservations(issued.Observation, v.Observations)
			if err != nil {
				return protocol.Event{}, err
			}
			body.Observations = observations
		}
		if source := developmentSource(input); source != nil && body.Development != nil {
			prior := source.Validation.Development
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
				step := coverage.SharedStep{Artifacts: item.Artifacts, Independent: item.Independent, ID: item.ID, Procedure: item.Procedure, Operations: slices.Clone(item.Operations)}
				if item.Files != nil {
					step.Files, err = selectedTestFiles(*item.Files, locationFacts)
					if err != nil {
						return protocol.Event{}, err
					}
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
				c := coverage.AcceptanceCase{Operations: slices.Clone(item.Operations), Scenario: item.Scenario, SharedSteps: slices.Clone(item.SharedSteps), CaseID: item.CaseID, PublicEntry: item.PublicEntry, Preconditions: item.Preconditions, Steps: item.Steps, Oracle: item.Oracle}
				if item.Files != nil {
					c.Files, err = selectedTestFiles(*item.Files, locationFacts)
					if err != nil {
						return protocol.Event{}, err
					}
				} else {
					for _, original := range issued.Manifest.Cases {
						if original.CaseID == item.CaseID {
							c.Files = slices.Clone(original.Files)
						}
					}
				}
				originalRef := ""
				for _, original := range issued.Manifest.Cases {
					if original.CaseID == item.CaseID {
						originalRef = original.TestRef
					}
				}
				c.TestRef, err = selectedTestRef(item, c.Files, originalRef, locationFacts)
				if err != nil {
					return protocol.Event{}, err
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
			// Coverage selects relations; existing relations retain their original
			// representation and order so unchanged approval evidence stays exact.
			for _, edge := range issued.Manifest.Edges {
				selected, replaced := v.Manifest.Coverage[edge.PointID]
				if (!replaced || slices.Contains(selected, edge.CaseID)) && ((!v.Manifest.Replace && !slices.Contains(v.Manifest.RemoveCases, edge.CaseID)) || slices.ContainsFunc(manifest.Cases, func(item coverage.AcceptanceCase) bool { return item.CaseID == edge.CaseID })) {
					manifest.Edges = append(manifest.Edges, edge)
				}
			}
			for _, point := range sortedKeys(v.Manifest.Coverage) {
				if !points[point] {
					return protocol.Event{}, fmt.Errorf("coverage selects unknown point: %s", point)
				}
				for _, caseID := range v.Manifest.Coverage[point] {
					for _, link := range issued.SourcePoints {
						if link.PointID != point || slices.ContainsFunc(manifest.Edges, func(edge coverage.CoverageEdge) bool {
							return edge.SourceID == link.SourceID && edge.PointID == point && edge.CaseID == caseID
						}) {
							continue
						}
						manifest.Edges = append(manifest.Edges, coverage.CoverageEdge{ReviewKind: manifest.Binding.ReviewKind, SourceID: link.SourceID, PointID: point, CaseID: caseID})
					}
				}
			}
			change := protocol.QADesignChange{Replace: v.Manifest.Replace, RemoveCases: v.Manifest.RemoveCases, RemoveSharedSteps: v.Manifest.RemoveSharedSteps}
			for point := range v.Manifest.Coverage {
				change.CoveragePoints = append(change.CoveragePoints, point)
			}
			slices.Sort(change.CoveragePoints)
			manifest, err = protocol.MergeQADesign(issued.Manifest, manifest, change)
			if err != nil {
				return protocol.Event{}, err
			}
			body.Manifest, body.DesignChange = &manifest, &change
			if input.Handler == "engine.qa.design.v4" && manifest.Binding.ReviewKind == coverage.ReviewWhitebox {
				if paths == nil {
					return protocol.Event{}, fmt.Errorf("whitebox design requires original native terminal path facts")
				}
				body.Development = &protocol.DevelopmentEvidence{Paths: slices.Clone(paths)}
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
			body.Status = string(result.SetStatus)
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
			if input.Handler != "engine.repair.verify.v2" || issued.Repair == nil || issued.Repair.FinalCandidate == nil {
				return protocol.Event{}, fmt.Errorf("repair comparison lacks its issued native responsibility")
			}
			repairRecords, err := observations(v.RepairVerification.Records, issued.Repair.VerificationCases)
			if err != nil {
				return protocol.Event{}, err
			}
			if len(repairRecords) != len(issued.Repair.VerificationCases) {
				return protocol.Event{}, fmt.Errorf("repair comparison must cover exactly its issued affected cases")
			}
			d := protocol.DevelopmentEvidence{Paths: slices.Clone(issued.Repair.VerificationPaths), AffectedCases: slices.Clone(issued.Repair.VerificationCases), Summary: "Compared the approved affected cases against native execution evidence"}
			body.Status = protocol.OutcomePass
			for _, caseID := range issued.Repair.VerificationCases {
				index := slices.IndexFunc(issued.ExecutionFacts, func(fact protocol.ExecutionFact) bool { return fact.CaseID == caseID })
				if index < 0 {
					return protocol.Event{}, fmt.Errorf("repair case has no complete issued native facts: %s", caseID)
				}
				comparison, present := repairRecords[caseID]
				if !present || strings.TrimSpace(comparison.Comparison) == "" || comparison.Result != coverage.ExecutedPass && comparison.Result != coverage.ExecutedFail {
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
			records, err := observations(value.Validation.Execution.Records, executionCaseIDs(issued))
			if err != nil {
				return protocol.Event{}, err
			}
			report := coverage.ExecutionReport{Binding: *issued.Execution}
			report.Binding.ActualCaseIDs = nil
			for _, caseID := range sortedKeys(records) {
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
				observation := records[caseID]
				if strings.TrimSpace(observation.Comparison) == "" || observation.Result != coverage.ExecutedPass && observation.Result != coverage.ExecutedFail {
					return protocol.Event{}, fmt.Errorf("case judgment requires a complete oracle comparison: %s", caseID)
				}
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
			body.Checkpoint = len(report.NotExecuted) > 0
			body.Status = string(coverage.ExecutionVerdict(append(slices.Clone(issued.Completed), report.Records...)))
			if body.Checkpoint && body.Status == protocol.OutcomePass {
				body.Status = ""
			}
		}
		payload.Validation = &body
	}
	if payload.Outcome == protocol.OutcomePass && (input.Review != nil && payload.Review == nil || input.Validation != nil && payload.Validation == nil) {
		return protocol.Event{}, fmt.Errorf("worker result lacks issued role evidence")
	}
	event := protocol.Event{ID: eventID, Kind: protocol.KindWorkerResult, Result: &payload}
	return event, event.Validate()
}

// semanticRole selects the original typed semantic body for schema and decoding.
func semanticRole(input protocol.WorkerInput) (any, error) {
	if input.Review != nil {
		return &Review{}, nil
	}
	if input.Validation == nil {
		return nil, fmt.Errorf("worker result schema lacks typed role")
	}
	switch input.Handler {
	case "engine.development.batch.v4":
		return &Development{}, nil
	case "engine.repair.v5":
		return &Repair{}, nil
	case "engine.merge.resolve.v3":
		return &ConflictResolution{}, nil
	case "engine.qa.design.v4":
		if len(input.Validation.AffectedCases) > 0 {
			return &CaseRevisions{}, nil
		}
		return &Design{}, nil
	case "engine.qa.review.rationale.v2":
		return &CaseReview{}, nil
	case "engine.qa.execution.v3":
		if len(input.Validation.CommandCases) > 0 {
			return &CommandBindings{}, nil
		}
		return &ExecutionJudgments{}, nil
	case "engine.validation.observation.v2":
		return &ObservationJudgments{}, nil
	case "engine.repair.verify.v2":
		return &Execution{}, nil
	case "engine.gate.v2":
		return &Gate{}, nil
	default:
		return nil, fmt.Errorf("worker result schema has no role %s", input.Handler)
	}
}

func decodeRoleBody(input protocol.WorkerInput, raw []byte, locationFacts ...[]LocationFact) (decodedResult, string, error) {
	var facts []LocationFact
	if len(locationFacts) > 0 {
		facts = locationFacts[0]
	}
	schema, err := completedSchema(input)
	if err != nil {
		return decodedResult{}, "", err
	}
	var fields map[string]json.RawMessage
	if err := decodeObject(raw, &fields); err != nil {
		return decodedResult{}, "", err
	}
	if _, present := fields["remainingWork"]; present {
		if input.Validation == nil || input.Handler != "engine.development.batch.v4" && input.Handler != "engine.repair.v5" {
			return decodedResult{}, "", fmt.Errorf("only development and repair can continue unfinished work")
		}
		var partial DevelopmentContinuation
		if err := decodeObject(raw, &partial); err != nil {
			return decodedResult{}, "", err
		}
		if strings.TrimSpace(partial.Summary) == "" || strings.TrimSpace(partial.RemainingWork) == "" {
			return decodedResult{}, "", fmt.Errorf("development continuation requires summary and actual remaining work")
		}
		return decodedResult{Validation: &decodedValidation{Status: "CONTINUE", Development: &Development{Summary: partial.Summary, RemainingWork: partial.RemainingWork}}}, "", nil
	}
	if reason, present := fields["interruptionReason"]; present {
		var text string
		if err := json.Unmarshal(reason, &text); err != nil {
			return decodedResult{}, "", err
		}
		if strings.TrimSpace(text) == "" {
			return decodedResult{}, "", fmt.Errorf("interruption requires an actual reason")
		}
		if input.Handler == "engine.qa.execution.v3" && input.Validation != nil {
			var partial ExecutionInterruption
			if err := decodeObject(raw, &partial); err != nil {
				return decodedResult{}, "", err
			}
			if partial.Records != nil || partial.Findings != nil {
				return decodedResult{Validation: &decodedValidation{Execution: &Execution{Records: partial.Records}, Findings: partial.Findings}}, text, nil
			}
		} else if len(fields) != 1 {
			return decodedResult{}, "", fmt.Errorf("interruption cannot contain completed role evidence")
		}
		return decodedResult{}, text, nil
	}
	target, err := semanticRole(input)
	if err != nil {
		return decodedResult{}, "", err
	}
	var result decodedResult
	if input.Review != nil {
		result.Review = target.(*Review)
		if err := decodeObject(raw, result.Review); err != nil {
			return result, "", err
		}
		if input.Review.Action != review.StartReadiness && result.Review.GranularityReview != nil {
			return result, "", fmt.Errorf("product review cannot propose development granularity")
		}
		for _, finding := range result.Review.Findings {
			if err := review.ValidateFindingClassification(input.Review.Action, finding.Category, finding.Severity); err != nil {
				return result, "", err
			}
		}
	} else if input.Validation != nil {
		v := &decodedValidation{Status: protocol.OutcomePass}
		result.Validation = v
		if err := decodeObject(raw, target); err != nil {
			return result, "", err
		}
		switch body := target.(type) {
		case *Development:
			v.Development = body
		case *Repair:
			v.GranularityReview = body.GranularityReview
			v.RepairAffectedCases = body.AffectedCases
			if fields["summary"] != nil || fields["completion"] != nil || fields["affectedCases"] != nil {
				v.Development = &Development{Summary: body.Summary, Completion: body.Completion}
			}
		case *ConflictResolution:
			if input.Validation.Conflict == nil || len(input.Validation.Conflict.Evidence.Conflicts) == 0 {
				return result, "", fmt.Errorf("resolution requires issued native conflicts")
			}
			v.Resolution = &protocol.ConflictResolution{Summary: body.Summary}
			for _, conflict := range input.Validation.Conflict.Evidence.Conflicts {
				v.Resolution.Paths = append(v.Resolution.Paths, conflict.Path)
			}
		case *Design:
			manifest, err := assembleDesign(input, body, facts)
			if err != nil {
				return result, "", err
			}
			v.Manifest, v.Checkpoint = manifest, body.Checkpoint
		case *CaseRevisions:
			manifest, err := reviseCases(input.Validation, body, facts)
			if err != nil {
				return result, "", err
			}
			v.Manifest, v.Checkpoint = manifest, body.Checkpoint
		case *CaseReview:
			v.Review, v.Suggestions = body, body.Suggestions
		case *ObservationJudgments:
			v.Observations = body
		case *CommandBindings:
			if len(body.Commands) != len(input.Validation.CommandCases) {
				return result, "", fmt.Errorf("commands must follow exactly the issued missing-command cases")
			}
			for index, changes := range body.Commands {
				operations, err := reviseOperations(nil, changes, facts)
				if err != nil {
					return result, "", err
				}
				if len(operations) == 0 {
					return result, "", fmt.Errorf("a missing invocation requires actual operations")
				}
				v.QACommands = append(v.QACommands, protocol.QACaseOperations{CaseID: input.Validation.CommandCases[index], Operations: operations})
			}
			v.Checkpoint = true
		case *ExecutionJudgments:
			v.Execution, v.Findings = &body.Execution, body.Findings
		case *Execution:
			v.RepairVerification = body
		case *Gate:
			v.Status, v.Findings = findingStatus(body.Findings), body.Findings
		}
	} else {
		return result, "", fmt.Errorf("worker result lacks issued typed role")
	}
	if input.Handler == "engine.repair.v5" && fields["completion"] != nil && fields["granularityReview"] != nil {
		var plan struct {
			Batches []map[string]json.RawMessage `json:"batches"`
		}
		if err := json.Unmarshal(fields["granularityReview"], &plan); err != nil {
			return result, "", err
		}
		for _, batch := range plan.Batches {
			for _, name := range []string{"artifacts", "newArtifacts"} {
				if _, present := batch[name]; present {
					return result, "", fmt.Errorf("completed repair artifacts come from original native paths; omit %s", name)
				}
			}
		}
	}
	for name := range fields {
		if schema["properties"].(map[string]any)[name] == nil {
			return result, "", fmt.Errorf("json: unknown field %q", name)
		}
	}
	for _, name := range schema["required"].([]any) {
		body, present := fields[name.(string)]
		if !present || bytes.Equal(bytes.TrimSpace(body), []byte("null")) {
			return result, "", fmt.Errorf("semantic role field is required: %s", name)
		}
	}
	return result, "", nil
}
func decodeObject(raw []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("worker result must contain one JSON object")
	}
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return fmt.Errorf("worker result must be an object")
	}
	return nil
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
func decisions(values []*Decision, ids []string, inherited []coverage.ReviewDecision) ([]coverage.ReviewDecision, error) {
	required := 0
	for _, id := range ids {
		if !slices.ContainsFunc(inherited, func(item coverage.ReviewDecision) bool { return item.ID == id }) {
			required++
		}
	}
	if len(values) != required {
		return nil, fmt.Errorf("review must follow the issued non-inherited item order")
	}
	result := []coverage.ReviewDecision{}
	selected := 0
	for _, id := range ids {
		prior := slices.IndexFunc(inherited, func(item coverage.ReviewDecision) bool { return item.ID == id })
		if prior >= 0 {
			value := inherited[prior]
			value.Inherited = true
			result = append(result, value)
			continue
		}
		value := values[selected]
		selected++
		if value == nil {
			return nil, fmt.Errorf("review decision is missing: %s", id)
		}
		result = append(result, coverage.ReviewDecision{ID: id, Status: value.Status, Reason: value.Reason})
	}
	return result, nil
}

func findings(values []Finding, facts []LocationFact, document requirement.FrozenRequirementProjection) ([]review.Finding, error) {
	if values == nil {
		return nil, nil
	}
	result := make([]review.Finding, 0, len(values))
	for _, item := range values {
		locations, err := LocationPaths(item.Locations, facts, document)
		if err != nil {
			return nil, err
		}
		result = append(result, review.Finding{Severity: item.Severity, SeverityRationale: item.SeverityRationale, Category: item.Category, Requirement: item.Requirement, NormalPath: item.NormalPath, ActualError: item.ActualError, DeferralImpact: item.DeferralImpact, Verification: item.Verification, Message: item.Message, Locations: locations})
	}
	return result, nil
}

func developmentSource(input protocol.WorkerInput) *protocol.WorkerResult {
	if input.Validation == nil {
		return nil
	}
	if input.Handler == "engine.development.batch.v4" && input.Validation.Development != nil {
		return input.Validation.Development.Result
	}
	if input.Handler == "engine.repair.v5" && input.Validation.Repair != nil {
		return input.Validation.Repair.Result
	}
	return nil
}

func selectedTestFiles(locations []Location, facts []LocationFact) ([]string, error) {
	for _, location := range locations {
		if location.Line != 0 || location.RequirementLine != 0 {
			return nil, fmt.Errorf("test material selects an original file, not a source line")
		}
	}
	return LocationPaths(locations, facts, requirement.FrozenRequirementProjection{})
}

func selectedTestRef(item Case, files []string, original string, facts []LocationFact) (string, error) {
	if item.TestFile == 0 && item.TestSymbol == 0 {
		if len(item.Operations) > 0 {
			if _, err := coverage.GoTestOperation(original); err == nil {
				return "", nil
			}
		}
		return original, nil
	}
	if item.TestFile < 1 || item.TestFile > len(files) || item.TestSymbol < 1 || item.Files == nil || item.TestFile > len(*item.Files) {
		return "", fmt.Errorf("testFile and testSymbol select an original read in this case's files")
	}
	location := (*item.Files)[item.TestFile-1]
	if location.Read < 1 || location.Read > len(facts) {
		return "", fmt.Errorf("test symbol selection requires its published original read")
	}
	fact := facts[location.Read-1]
	if fact.Kind != "read" || len(fact.Paths) != 1 || fact.Paths[0] != files[item.TestFile-1] {
		return "", fmt.Errorf("test symbol selection differs from its original read file")
	}
	symbols, err := coverage.GoTestSymbols(fact.Paths[0], []byte(fact.Content))
	if err != nil {
		return "", err
	}
	if item.TestSymbol > len(symbols) {
		return "", fmt.Errorf("testSymbol is not in the selected read's published test symbols")
	}
	return files[item.TestFile-1] + "::" + symbols[item.TestSymbol-1], nil
}

// ValidateSemanticBody checks the issued role body before publishing tool evidence.
func ValidateSemanticBody(input protocol.WorkerInput, raw []byte, facts ...[]LocationFact) error {
	_, _, err := decodeRoleBody(input, raw, facts...)
	return err
}
