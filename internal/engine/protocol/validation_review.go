package protocol

import (
	"formal-gates/internal/engine/coverage"
	"maps"
	"slices"
	"strings"
)

// caseReviewIdentity includes every referenced shared instruction and mapping.
// An unchanged case body alone cannot carry approval across a shared change.
func caseReviewIdentity(m coverage.AcceptanceManifest, id string) string {
	var item *coverage.AcceptanceCase
	for i := range m.Cases {
		if m.Cases[i].CaseID == id {
			item = &m.Cases[i]
		}
	}
	if item == nil {
		return ""
	}
	steps := []coverage.SharedStep{}
	for _, step := range m.SharedSteps {
		if slices.Contains(item.SharedSteps, step.ID) {
			steps = append(steps, step)
		}
	}
	edges := []coverage.CoverageEdge{}
	for _, edge := range m.Edges {
		if edge.CaseID == id {
			edges = append(edges, edge)
		}
	}
	return digestOfCanonical(struct {
		Case  coverage.AcceptanceCase
		Steps []coverage.SharedStep
		Edges []coverage.CoverageEdge
	}{*item, steps, edges})
}
func (s *State) inheritableReviews(set *CaseSet) []coverage.ReviewDecision {
	if len(set.ReviewHistory) == 0 {
		return nil
	}
	prior := set.ReviewHistory[len(set.ReviewHistory)-1]

	decisions := []coverage.ReviewDecision{}
	for _, d := range prior.Review.CaseDecisions {
		if set.Manifest.Binding.ReviewKind == coverage.ReviewWhitebox {
			testRef := ""
			for _, item := range set.Manifest.Cases {
				if item.CaseID == d.ID {
					testRef = item.TestRef
					break
				}
			}
			if !s.sameCaseReviewContent(prior.Candidate, set.Candidate, d.ID, testRef) {
				continue
			}
		}
		if !slices.Contains(set.AffectedCases, d.ID) && d.Status == coverage.StatusPass && caseReviewIdentity(prior.Manifest, d.ID) == caseReviewIdentity(set.Manifest, d.ID) && caseReviewIdentity(set.Manifest, d.ID) != "" {
			decisions = append(decisions, d)
		}
	}
	return decisions
}
func inheritableSharedReviews(set *CaseSet) []coverage.ReviewDecision {
	if len(set.ReviewHistory) == 0 {
		return nil
	}
	prior := set.ReviewHistory[len(set.ReviewHistory)-1]
	var decisions []coverage.ReviewDecision
	for _, d := range prior.Review.SharedStepDecisions {
		if d.Status != coverage.StatusPass {
			continue
		}
		for _, current := range set.Manifest.SharedSteps {
			if current.ID == d.ID && slices.Contains(prior.Manifest.SharedSteps, current) {
				decisions = append(decisions, d)
			}
		}
	}
	return decisions
}

func (s *State) validateReviewPolicy(set *CaseSet, result coverage.ReviewResult) error {
	if result.Scope == coverage.ScopeAffected {
		baseline := false
		for _, prior := range set.ReviewHistory {
			if prior.Review.Scope == coverage.ScopeFull && prior.Manifest.Binding.ReviewKind == set.Manifest.Binding.ReviewKind {
				baseline = true
				break
			}
		}
		if !baseline {
			return validationError("first case-set review must establish a FULL baseline")
		}
	}
	for _, list := range [][]coverage.ReviewDecision{result.CaseDecisions, result.SharedStepDecisions} {
		for _, d := range list {
			if strings.TrimSpace(d.Reason) == "" {
				return validationError("case and shared-step reviews require their actual rationale: " + d.ID)
			}
		}
	}
	allowed := s.inheritableReviews(set)
	for _, d := range result.CaseDecisions {
		if d.Inherited && !slices.ContainsFunc(allowed, func(prior coverage.ReviewDecision) bool {
			return prior.ID == d.ID && d.Status == coverage.StatusPass && d.Reason == prior.Reason
		}) {
			return validationError("inherited approval requires unchanged responsibility and its original rationale: " + d.ID)
		}
	}
	shared := inheritableSharedReviews(set)
	for _, d := range result.SharedStepDecisions {
		if d.Inherited && !slices.ContainsFunc(shared, func(prior coverage.ReviewDecision) bool {
			return prior.ID == d.ID && d.Status == coverage.StatusPass && d.Reason == prior.Reason
		}) {
			return validationError("inherited shared approval requires unchanged instructions and original rationale: " + d.ID)
		}
	}
	for _, list := range [][]coverage.ReviewDecision{result.SourceDecisions, result.PointDecisions} {
		for _, d := range list {
			if d.Inherited {
				return validationError("aggregate decisions require current review")
			}
		}
	}
	return nil
}

// Whitebox approval covers the immutable test implementation as well as its
// manifest. Equal content may have different provider-native identities.
func (s *State) sameCandidateContent(previous, current string) bool {
	if previous == "" || current == "" {
		return false
	}
	if previous == current {
		return true
	}
	var previousDigest, currentDigest string
	for _, receipt := range s.HostActionReceipts {
		if receipt.AdapterOperation != "op.candidate" || (receipt.Status != HostActionStatusExecuted && receipt.Status != HostActionStatusReconciled) {
			continue
		}
		evidence, err := decodeCandidateEvidence(receipt)
		if err != nil {
			continue
		}
		if evidence.Identity == previous {
			previousDigest = evidence.Digest
		}
		if evidence.Identity == current {
			currentDigest = evidence.Digest
		}
	}
	return previousDigest != "" && previousDigest == currentDigest
}

func (s *State) sameCaseReviewContent(previous, current, caseID, testRef string) bool {
	if s.sameCandidateContent(previous, current) {
		return true
	}
	var before, after CaseReviewView
	for _, receipt := range s.HostActionReceipts {
		if receipt.AdapterOperation != "op.candidate" || (receipt.Status != HostActionStatusExecuted && receipt.Status != HostActionStatusReconciled) {
			continue
		}
		evidence, err := decodeCandidateEvidence(receipt)
		if err != nil {
			continue
		}
		if view, ok := evidence.ReviewViews[caseID]; ok && view.TestRef == testRef && evidence.Identity == previous {
			before = view
		}
		if view, ok := evidence.ReviewViews[caseID]; ok && view.TestRef == testRef && evidence.Identity == current {
			after = view
		}
	}
	return before.TestRef != "" && before.TestRef == after.TestRef && len(before.Inputs) > 0 && maps.Equal(before.Inputs, after.Inputs)
}
