package facade

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
)

// gateNativeView projects the complete issued range and context. Only proven
// test transitions in that range can be excluded by the GateReviewView rule.
func gateNativeView(entries map[string]string, changes []vcs.Change, base, after map[string]bool) (map[string]string, []vcs.Change) {
	context := maps.Clone(entries)
	rangeView := []vcs.Change{}
	for _, change := range changes {
		if testOwnedTransition(change.Path, change.BaseMode != "000000", change.Mode != "000000", base, after) {
			delete(context, change.Path)
		} else {
			rangeView = append(rangeView, change)
		}
	}
	return context, rangeView
}

func (f *Facade) childValidationViews(s *protocol.State, head string, currentOwnership map[string]bool) (map[string]protocol.ChildValidationView, error) {
	if s.ChildCreation == nil || len(s.SliceReceipts) == 0 {
		return nil, nil
	}
	finalEntries, err := f.vcs.TreeEntries(head)
	if err != nil {
		return nil, err
	}
	finalChanges, err := f.vcs.Changes(s.BaseSnapshot, head)
	if err != nil {
		return nil, err
	}
	baseOwnership, err := f.ownershipAt(s, s.BaseSnapshot)
	if err != nil {
		return nil, err
	}
	candidateContext, candidateRange := gateNativeView(finalEntries, finalChanges, baseOwnership, currentOwnership)
	views := map[string]protocol.ChildValidationView{}
	for _, sliceID := range s.Topology.Slices {
		receipt := s.SliceReceipts[s.SliceChildren[sliceID]]
		child := receipt.Receipt
		owned, err := f.vcs.Changes(s.ChildCreation.Source, child.Snapshot)
		if err != nil {
			return nil, err
		}
		view := protocol.ChildValidationView{BaseSnapshot: s.BaseSnapshot, ReceiptDigest: receipt.Digest, OwnedChanges: owned, ContentEqual: true, Gates: map[string]protocol.ChildGateView{}}
		for _, change := range owned {
			entry, err := f.vcs.Entry(child.Snapshot, change.Path)
			if err != nil {
				return nil, err
			}
			actual, err := f.vcs.Entry(head, change.Path)
			if err != nil {
				return nil, err
			}
			view.ContentEqual = view.ContentEqual && actual == entry
		}
		for _, result := range child.Validation.Results {
			if result.Target.Kind != "GATE" || result.Result.Validation == nil {
				continue
			}
			verdict, action := result.Result.Validation, result.Target.ResultAction
			for i := len(s.Validation.Waves) - 1; i >= 0; i-- {
				wave := s.Validation.Waves[i]
				var previous *protocol.ValidationResult
				for _, target := range wave.Targets {
					accepted := s.Results[target.ResultAction].Validation
					if target.SliceID == sliceID && target.Kind == "GATE" && target.Gate == result.Target.Gate && target.Status == "PASS" && accepted != nil && accepted.Status == "PASS" && accepted.Development == nil {
						previous, action = accepted, target.ResultAction
					}
				}
				if previous != nil {
					verdict = previous
					break
				}
			}
			binding := verdict.Binding
			gateView := protocol.ChildGateView{SourceCandidate: binding.Candidate, SourceAction: action}
			// A carry or repair result without this gate's original signed FULL
			// input cannot prove equality. Its preserved receipt remains evidence.
			if result.Target.Status != "PASS" || verdict.Status != "PASS" || verdict.Development != nil || binding.Task.Step != result.Target.Task.Step || binding.RequirementRevision != s.Validation.RequirementRevision || binding.Candidate == "" {
				view.Gates[result.Target.Gate] = gateView
				continue
			}
			originalChanges, err := f.vcs.Changes(s.BaseSnapshot, binding.Candidate)
			if err != nil {
				return nil, err
			}
			originalEntries, err := f.vcs.TreeEntries(binding.Candidate)
			if err != nil {
				return nil, err
			}
			originalOwnership, err := f.ownershipAt(s, binding.Candidate)
			if err != nil {
				return nil, err
			}
			if originalOwnership == nil && binding.Candidate == child.Validation.Candidate.Identity && child.Validation.Candidate.Digest == child.Tree {
				originalOwnership, err = f.ownershipAt(s, child.Snapshot)
				if err != nil {
					return nil, err
				}
			}
			sourceContext, sourceRange := gateNativeView(originalEntries, originalChanges, baseOwnership, originalOwnership)
			digest := func(context map[string]string, changes []vcs.Change) string {
				raw, _ := json.Marshal(struct {
					Requirement string
					Gate        string
					Scope       coverage.ScopeMode
					Route       string
					Definition  string
					Package     string
					Base        string
					Context     map[string]string
					Changes     []vcs.Change
				}{binding.RequirementRevision, result.Target.Gate, child.Validation.Scope, binding.RouteScope, child.Envelope.DefinitionDigest, child.Envelope.PackageDigest, s.BaseSnapshot, context, changes})
				return fmt.Sprintf("sha256:%x", sha256.Sum256(raw))
			}
			gateView.SourceDigest, gateView.CandidateDigest = digest(sourceContext, sourceRange), digest(candidateContext, candidateRange)
			gateView.Equal = maps.Equal(sourceContext, candidateContext) && slices.Equal(sourceRange, candidateRange)
			view.Gates[result.Target.Gate] = gateView
		}
		views[child.ChildRunID] = view
	}
	return views, nil
}
