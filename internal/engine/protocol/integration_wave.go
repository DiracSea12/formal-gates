package protocol

import (
	"formal-gates/internal/engine/compiler"
	"slices"
)

// ValidationReuse maps an original child PASS to this wave's final candidate.
// The original result and child checkpoint remain unchanged.
type ValidationReuse struct {
	ChildReceiptDigest string `json:"childReceiptDigest"`
	SourceCandidate    string `json:"sourceCandidate"`
	SourceAction       string `json:"sourceAction"`
	ViewDigest         string `json:"viewDigest"`
}

func (s *State) addChildValidationTargets(w *ValidationWave, cd *compiler.CompiledDefinition) {
	if !s.mergeMaster() {
		return
	}
	views := s.candidateChildViews()
	for _, sliceID := range s.Topology.Slices {
		receipt := s.SliceReceipts[s.SliceChildren[sliceID]]
		child := receipt.Receipt
		view := views[child.ChildRunID]
		for _, gate := range child.SelectedGates {
			if gate == "blackbox" || gate == "whitebox" {
				continue
			}
			target := &ValidationTarget{SliceID: sliceID, Kind: "GATE", Gate: gate, Status: "PENDING"}
			target.Task = pipelineTask(cd, "engine.gate", w.ID+":slice:"+sliceID+":"+gate)
			gateView := view.Gates[gate]
			if view.ReceiptDigest == receipt.Digest && view.BaseSnapshot == s.BaseSnapshot && gateView.SourceDigest != "" && gateView.SourceDigest == gateView.CandidateDigest && gateView.Equal {
				target.Status = "PASS"
				target.Reuse = &ValidationReuse{ChildReceiptDigest: receipt.Digest, SourceCandidate: gateView.SourceCandidate, SourceAction: gateView.SourceAction, ViewDigest: digestOfCanonical(gateView)}
			}
			if prior := s.currentValidationWave(); prior != nil && target.Status == "PENDING" {
				s.carryGateResult(target, prior, w)
			}

			w.Targets[target.Task.String()] = target
		}
	}
}

func sameTarget(a, b *ValidationTarget) bool {
	return a.Kind == b.Kind && a.Mode == b.Mode && a.Gate == b.Gate && a.SliceID == b.SliceID
}

func (s *State) AffectedChildSet() []string {
	w := s.currentValidationWave()
	if w == nil || !s.mergeMaster() {
		return nil
	}
	var affected []string
	views := s.candidateChildViews()
	for _, sliceID := range s.Topology.Slices {
		receipt := s.SliceReceipts[s.SliceChildren[sliceID]]
		view := views[receipt.Receipt.ChildRunID]
		changed := view.ReceiptDigest != receipt.Digest || !view.ContentEqual
		for _, target := range w.Targets {
			if target.SliceID == sliceID && target.Reuse == nil {
				changed = true
			}
		}
		if changed {
			affected = append(affected, sliceID)
		}
	}
	slices.Sort(affected)
	return affected
}
