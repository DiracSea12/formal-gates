package worker

import (
	"fmt"
	"slices"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

type ObservationJudgments struct {
	Findings    []FindingJudgment `json:"findings"`
	Scopes      []ScopeJudgment   `json:"scopes"`
	GateImpacts []ImpactJudgment  `json:"gateImpacts"`
}
type FindingJudgment struct {
	Decision          string                     `json:"decision"`
	SameAs            string                     `json:"sameAs,omitempty"`
	Requirement       string                     `json:"requirement"`
	NormalPath        string                     `json:"normalPath"`
	Module            string                     `json:"module"`
	RootCause         string                     `json:"rootCause"`
	Consequence       string                     `json:"consequence"`
	Severity          string                     `json:"severity,omitempty"`
	SeverityRationale string                     `json:"severityRationale,omitempty"`
	Verification      review.FindingVerification `json:"verification"`
	Reason            string                     `json:"reason"`
}
type ScopeJudgment struct {
	Cases  []string `json:"cases"`
	Reason string   `json:"reason"`
}
type ImpactJudgment struct {
	Decision string `json:"decision"`
	Reason   string `json:"reason"`
}

func assembleObservations(input *protocol.ObservationInput, judgments *ObservationJudgments) ([]protocol.OperatorObservationPayload, error) {
	if input == nil || judgments.Findings == nil || judgments.Scopes == nil || judgments.GateImpacts == nil || len(judgments.Findings) != len(input.Findings) || len(judgments.Scopes) != len(input.Scopes) || len(judgments.GateImpacts) != len(input.GateImpacts) {
		return nil, fmt.Errorf("semantic judgment must cover exactly every issued duty")
	}
	observations := []protocol.OperatorObservationPayload{}
	appendObservation := func(value protocol.ValidationObservation) {
		observations = append(observations, protocol.OperatorObservationPayload{Subject: input.WaveID, Validation: &value})
	}
	if len(input.Findings) > 0 {
		rows := []protocol.FindingResolution{}
		for index, source := range input.Findings {
			f := source.Finding
			value := judgments.Findings[index]
			row := protocol.FindingResolution{FindingID: f.ID, Supersedes: len(f.History), Decision: value.Decision, Requirement: value.Requirement, NormalPath: value.NormalPath, Module: value.Module, RootCause: value.RootCause, Consequence: value.Consequence, Severity: value.Severity, SeverityRationale: value.SeverityRationale, Verification: value.Verification, Reason: value.Reason}
			if value.Decision == "SAME_AS" {
				row.ProblemID = value.SameAs
				for index, related := range input.Findings {
					if related.Finding.ID == value.SameAs && judgments.Findings[index].Decision == "NEW" {
						row.ProblemID = protocol.FindingProblemID(related.Finding)
					}
				}
			} else if value.SameAs != "" {
				return nil, fmt.Errorf("only SAME_AS can select a related problem")
			}
			rows = append(rows, row)
		}
		slices.SortStableFunc(rows, func(a, b protocol.FindingResolution) int {
			if a.Decision == "NEW" && b.Decision != "NEW" {
				return -1
			}
			if b.Decision == "NEW" && a.Decision != "NEW" {
				return 1
			}
			return 0
		})
		appendObservation(protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: rows})
	}
	for index, source := range input.Scopes {
		value := judgments.Scopes[index]
		appendObservation(protocol.ValidationObservation{Kind: "EXECUTION_SCOPE", Mode: source.Mode, AffectedCases: value.Cases, Reason: value.Reason})
	}
	for index, source := range input.GateImpacts {
		value := judgments.GateImpacts[index]
		appendObservation(protocol.ValidationObservation{Kind: "GATE_IMPACT", Gate: source.Gate, Decision: value.Decision, Reason: value.Reason})
	}
	return observations, nil
}
