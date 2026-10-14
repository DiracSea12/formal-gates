package protocol

import (
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
	"slices"
	"strings"
)

func (s *State) gateImpactPending() string {
	if w := s.currentValidationWave(); w != nil {
		for _, key := range sortedTargetKeys(w) {
			target := w.Targets[key]
			if target.GateImpact != nil && target.GateImpact.Decision == "" {
				return "submit GATE_IMPACT with INHERIT or RERUN and the independent impact reason for " + target.Gate
			}
		}
	}
	return ""
}

func (s *State) acceptGateImpact(subject string, o *ValidationObservation) error {
	w := s.currentValidationWave()
	if w == nil || w.ID != subject || w.Status != "COLLECTING" {
		return validationError("gate impact requires the current collecting wave")
	}
	for _, target := range w.Targets {
		if target.Gate != o.Gate || target.GateImpact == nil {
			continue
		}
		if target.GateImpact.Decision != "" {
			return validationError("gate impact is already fixed for this candidate")
		}
		if (o.Decision != "INHERIT" && o.Decision != "RERUN") || strings.TrimSpace(o.Reason) == "" {
			return validationError("gate impact needs INHERIT/RERUN and its independent reason")
		}
		source := s.validationWave(target.GateImpact.SourceWave)
		if source == nil {
			return validationError("gate impact source is unavailable")
		}
		for _, prior := range source.Targets {
			if prior.Gate != target.Gate || prior.Status != "PASS" {
				continue
			}
			impact := target.GateImpact
			if o.Decision == "INHERIT" && !s.carryValidationTarget(target, prior, source, w) {
				return validationError("gate inheritance requires the same completed responsibility")
			}
			target.GateImpact = impact
			target.GateImpact.Decision, target.GateImpact.Reason = o.Decision, o.Reason
			s.routingReleasedCapacity = true
			return nil
		}
	}
	return validationError("gate has no pending impact judgment")
}

// GateInput identifies the native range for one gate without QA case content.
type GateInput struct {
	Batches        []DevelopmentBatch           `json:"batches,omitempty"`
	Gate           string                       `json:"gate"`
	BaseSnapshot   string                       `json:"baseSnapshot"`
	Candidate      coverage.ValidationCandidate `json:"candidate"`
	TestOwnedPaths []string                     `json:"testOwnedPaths"`
}

func (s *State) gateInput(binding WorkBinding, task runtime.TaskKey) *GateInput {
	if input := s.repairGateInput(binding); input != nil {
		return input
	}
	wave := s.validationWave(binding.WaveID)
	if wave == nil {
		return nil
	}
	target := wave.Targets[task.String()]
	if target == nil || target.Kind != "GATE" {
		return nil
	}
	var selected HostActionIntent
	var proof CandidateEvidence
	for action, receipt := range s.HostActionReceipts {
		if receipt.AdapterOperation != "op.candidate" || (receipt.Status != HostActionStatusExecuted && receipt.Status != HostActionStatusReconciled) {
			continue
		}
		intent, exists := s.hostIntent(action)
		params, err := candidateParameters(intent)
		if !exists || err != nil || params.Stage != "REVIEW_READY" || !s.candidateContextCurrent(params) || params.ReviewBase != s.BaseSnapshot || !slices.Equal(params.SourceTasks, wave.SourceTasks) {
			continue
		}
		evidence, err := decodeCandidateEvidence(receipt)
		if err == nil && evidence.ParametersDigest == intent.PayloadDigest && evidence.GateView != nil && evidence.Identity == wave.Candidate.Identity && evidence.Digest == wave.Candidate.Digest && intent.Revision > selected.Revision {
			selected, proof = intent, evidence
		}
	}
	if selected.ActionID == "" {
		return nil
	}
	_, batches := s.gateRequirementContext(binding)
	return &GateInput{Batches: batches, Gate: target.Gate, BaseSnapshot: s.BaseSnapshot, Candidate: wave.Candidate, TestOwnedPaths: slices.Clone(proof.GateView.TestOwnedPaths)}
}
