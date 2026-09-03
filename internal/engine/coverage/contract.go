// Package coverage contains the pure, in-memory QA coverage contract used by
// the engine.  It deliberately has no dependency on the workflow state
// machine, persistence, CLI, or legacy QA case types.  Callers provide the
// confirmed source list, per-kind manifests, review decisions, and candidate
// execution report; this package only validates and projects those values.
package coverage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// SourceCategory identifies the kind of confirmed obligation represented by a
// required source.
type SourceCategory string

const (
	ProductRequirement SourceCategory = "PRODUCT_REQUIREMENT"
	SolutionConstraint SourceCategory = "SOLUTION_CONSTRAINT"
)

// Applicability says whether a source is verified by a QA manifest or by an
// explicitly documented non-QA verification.
type Applicability string

const (
	QAApplicability    Applicability = "QA"
	NonQAApplicability Applicability = "NON_QA"
)

// ReviewKind is the selected QA kind to which a manifest belongs.
type ReviewKind string

const (
	ReviewBlackbox ReviewKind = "BLACKBOX"
	ReviewWhitebox ReviewKind = "WHITEBOX"
	ReviewMerge    ReviewKind = "MERGE"
)

// ScopeMode describes the review/execution scope.  The first baseline is
// normally FULL; AFFECTED may be used only when the caller supplies explicit
// execution accounting for the remainder.
type ScopeMode string

const (
	ScopeFull     ScopeMode = "FULL"
	ScopeAffected ScopeMode = "AFFECTED"
)

// DecisionStatus is shared by review and alternative verification decisions.
// Only PASS can be projected into an approved whitelist.
type DecisionStatus string

const (
	StatusPass           DecisionStatus = "PASS"
	StatusFail           DecisionStatus = "FAIL"
	StatusPending        DecisionStatus = "PENDING"
	StatusUnknown        DecisionStatus = "UNKNOWN"
	StatusAuthorizedSkip DecisionStatus = "AUTHORIZED_SKIP"
)

// ExecutionStatus preserves the result semantics observed by the executor.
// In particular, AUTHORIZED_SKIP is never treated as EXECUTED_PASS.
type ExecutionStatus string

const (
	ExecutedPass    ExecutionStatus = "EXECUTED_PASS"
	ExecutedFail    ExecutionStatus = "EXECUTED_FAIL"
	ExecutedPending ExecutionStatus = "PENDING"
	ExecutedUnknown ExecutionStatus = "UNKNOWN"
	AuthorizedSkip  ExecutionStatus = "AUTHORIZED_SKIP"
)

// Provenance identifies how an expected edge was accounted for in an
// execution report.
type Provenance string

const (
	ProvenanceExecuted    Provenance = "EXECUTED"
	ProvenanceInherited   Provenance = "INHERITED"
	ProvenanceNotExecuted Provenance = "NOT_EXECUTED"
)

// SourceBinding is the identity shared by the confirmed required source list.
// ChildID is empty for a master/non-sliced run and non-empty for a child.
type SourceBinding struct {
	RunID               string `json:"runId"`
	ChildID             string `json:"childId,omitempty"`
	RequirementRevision string `json:"requirementRevision"`
	SolutionRevision    string `json:"solutionRevision"`
}

func (b SourceBinding) equal(other SourceBinding) bool {
	return b == other
}

// RequiredSource is one obligation from the user-confirmed requiredSources
// list.  The list is the only source inventory consumed by this package.
type RequiredSource struct {
	SourceID      string         `json:"sourceId"`
	Category      SourceCategory `json:"category"`
	Applicability Applicability  `json:"applicability"`
}

// RequiredSources is the authoritative finite source list and its binding.
type RequiredSources struct {
	Binding SourceBinding    `json:"binding"`
	Sources []RequiredSource `json:"sources"`
}

// ManifestBinding binds one selected QA kind to the run and route/topology
// scope for which it was designed.
type ManifestBinding struct {
	SourceBinding
	ReviewKind    ReviewKind `json:"reviewKind"`
	RouteScope    string     `json:"routeScope"`
	TopologyScope string     `json:"topologyScope"`
}

func (b ManifestBinding) equal(other ManifestBinding) bool {
	return b.SourceBinding.equal(other.SourceBinding) && b.ReviewKind == other.ReviewKind && b.RouteScope == other.RouteScope && b.TopologyScope == other.TopologyScope
}

// AcceptancePoint is a finite, observable obligation produced by a manifest.
type AcceptancePoint struct {
	PointID            string `json:"pointId"`
	ObservableBehavior string `json:"observableBehavior"`
	Oracle             string `json:"oracle"`
}

// CaseMode describes the closed execution shape of an acceptance case.
type CaseMode string

const (
	CaseBlackbox CaseMode = "BLACKBOX"
	CaseWhitebox CaseMode = "WHITEBOX"
	CaseMerge    CaseMode = "MERGE"
)

// AcceptanceCase is a case in a manifest.  Black-box and merge cases use the
// public-entry fields; white-box cases use the unique test reference.
type AcceptanceCase struct {
	CaseID        string   `json:"caseId"`
	Mode          CaseMode `json:"mode"`
	PublicEntry   string   `json:"publicEntry,omitempty"`
	Preconditions string   `json:"preconditions,omitempty"`
	Steps         string   `json:"steps,omitempty"`
	Oracle        string   `json:"oracle"`
	TestRef       string   `json:"testRef,omitempty"`
}

// CoverageEdge is the single source of truth for source↔point↔case
// relationships.  Reverse lookups are derived from these triples.
type CoverageEdge struct {
	ReviewKind ReviewKind `json:"reviewKind,omitempty"`
	SourceID   string     `json:"sourceId"`
	PointID    string     `json:"pointId"`
	CaseID     string     `json:"caseId"`
}

func (e CoverageEdge) key() string { return e.SourceID + "\x00" + e.PointID + "\x00" + e.CaseID }

func executionEdgeKey(kind ReviewKind, sourceID, pointID, caseID string) string {
	return string(kind) + "\x00" + sourceID + "\x00" + pointID + "\x00" + caseID
}

// AcceptanceManifest is one selected kind's source, point, case, and edge
// declaration.  A source may occur in multiple manifests; every occurrence is
// independently reviewed and executed.
type AcceptanceManifest struct {
	Binding ManifestBinding   `json:"binding"`
	Sources []string          `json:"sources"`
	Points  []AcceptancePoint `json:"points"`
	Cases   []AcceptanceCase  `json:"cases"`
	Edges   []CoverageEdge    `json:"edges"`
}

// CoverageMap is a named projection of a manifest's edge relation.  It does
// not create a second source of truth: edges remain owned by the manifest and
// this value is produced on demand for map validation/digest binding.
type CoverageMap struct {
	Binding ManifestBinding `json:"binding"`
	Edges   []CoverageEdge  `json:"edges"`
}

// MapDigest computes a canonical digest for this map projection.
func (m CoverageMap) MapDigest() (string, error) {
	if !validManifestBinding(m.Binding) {
		return "", invalid(CodeInvalidBinding, "map.binding", "map binding is incomplete")
	}
	edges := append([]CoverageEdge(nil), m.Edges...)
	sort.Slice(edges, func(i, j int) bool { return edges[i].key() < edges[j].key() })
	seen := make(map[string]struct{}, len(edges))
	for i, edge := range edges {
		if !nonempty(edge.SourceID, "source") || !nonempty(edge.PointID, "point") || !nonempty(edge.CaseID, "case") {
			return "", invalid(CodeOrphanEdge, fmt.Sprintf("map.edges[%d]", i), "edge IDs are required")
		}
		if _, duplicate := seen[edge.key()]; duplicate {
			return "", invalid(CodeDuplicateID, fmt.Sprintf("map.edges[%d]", i), "duplicate edge")
		}
		seen[edge.key()] = struct{}{}
	}
	return canonicalDigest(CoverageMap{Binding: m.Binding, Edges: edges})
}

// Map returns the manifest's edge relation with its binding attached.
func (m AcceptanceManifest) Map() CoverageMap {
	return CoverageMap{Binding: m.Binding, Edges: append([]CoverageEdge(nil), m.Edges...)}
}

// ValidateCoverageMap verifies a map projection against its manifest.
func ValidateCoverageMap(manifest AcceptanceManifest, coverageMap CoverageMap) error {
	if !coverageMap.Binding.equal(manifest.Binding) {
		return invalid(CodeInvalidBinding, "map.binding", "map binding does not match manifest")
	}
	if len(coverageMap.Edges) != len(manifest.Edges) {
		return invalid(CodeMissingCoverage, "map.edges", "map edge set does not match manifest")
	}
	want := make(map[string]struct{}, len(manifest.Edges))
	for i, edge := range manifest.Edges {
		if edge.ReviewKind != "" && edge.ReviewKind != manifest.Binding.ReviewKind {
			return invalid(CodeInvalidBinding, fmt.Sprintf("manifest.edges[%d].reviewKind", i), "manifest edge review kind does not match manifest binding")
		}
		want[edge.key()] = struct{}{}
	}
	seen := make(map[string]struct{}, len(coverageMap.Edges))
	for i, edge := range coverageMap.Edges {
		if edge.ReviewKind != "" && edge.ReviewKind != manifest.Binding.ReviewKind {
			return invalid(CodeInvalidBinding, fmt.Sprintf("map.edges[%d].reviewKind", i), "map edge review kind does not match manifest binding")
		}
		if _, ok := want[edge.key()]; !ok {
			return invalid(CodeUnknownID, "map.edges", "map contains an edge not declared by manifest")
		}
		if _, duplicate := seen[edge.key()]; duplicate {
			return invalid(CodeDuplicateID, "map.edges", "map contains a duplicate edge")
		}
		seen[edge.key()] = struct{}{}
	}
	if len(seen) != len(want) {
		return invalid(CodeMissingCoverage, "map.edges", "map omits a manifest edge")
	}
	return nil
}

// MapDigest returns the canonical digest for this manifest's map projection.
func (m AcceptanceManifest) MapDigest() (string, error) {
	if err := ValidateCoverageMap(m, m.Map()); err != nil {
		return "", err
	}
	return m.Map().MapDigest()
}

// PointsForSource derives the forward source→point view from Edges.
func (m AcceptanceManifest) PointsForSource(sourceID string) []string {
	seen := make(map[string]struct{})
	for _, edge := range m.Edges {
		if edge.SourceID == sourceID {
			seen[edge.PointID] = struct{}{}
		}
	}
	return sortedKeys(seen)
}

// CasesForPoint derives the point→case view from Edges.
func (m AcceptanceManifest) CasesForPoint(pointID string) []string {
	seen := make(map[string]struct{})
	for _, edge := range m.Edges {
		if edge.PointID == pointID {
			seen[edge.CaseID] = struct{}{}
		}
	}
	return sortedKeys(seen)
}

// SourcesForCase derives the reverse case→source view from Edges.
func (m AcceptanceManifest) SourcesForCase(caseID string) []string {
	seen := make(map[string]struct{})
	for _, edge := range m.Edges {
		if edge.CaseID == caseID {
			seen[edge.SourceID] = struct{}{}
		}
	}
	return sortedKeys(seen)
}

// AlternativeVerification closes a non-QA source.  A PASS with evidence is
// required; a reason or explanation by itself is not verification.
type AlternativeVerification struct {
	SourceID string         `json:"sourceId"`
	Reason   string         `json:"reason"`
	Method   string         `json:"method"`
	Status   DecisionStatus `json:"status"`
	Evidence string         `json:"evidence"`
}

// CoverageContract is the complete pure-logic input for coverage validation.
// SelectedKinds is optional for convenience; when omitted it is derived from
// the supplied manifests.  When supplied, every selected kind must have one
// manifest and no unselected kind may appear.
type CoverageContract struct {
	RequiredSources          RequiredSources           `json:"requiredSources"`
	SelectedKinds            []ReviewKind              `json:"selectedKinds,omitempty"`
	Manifests                []AcceptanceManifest      `json:"manifests"`
	AlternativeVerifications []AlternativeVerification `json:"alternativeVerifications,omitempty"`
	Candidate                *ValidationCandidate      `json:"candidate,omitempty"`
}

// ReviewDecision is a decision over a source, point, or case ID.
type ReviewDecision struct {
	ID     string         `json:"id"`
	Status DecisionStatus `json:"status"`
}

// ReviewResult contains the required per-manifest source, point, and case
// decisions.  SetStatus is retained for diagnostics but cannot replace the
// item-level decisions.
type ReviewResult struct {
	Binding         ManifestBinding  `json:"binding"`
	Scope           ScopeMode        `json:"scope"`
	SourceDecisions []ReviewDecision `json:"sourceDecisions"`
	PointDecisions  []ReviewDecision `json:"pointDecisions"`
	CaseDecisions   []ReviewDecision `json:"caseDecisions"`
	UnboundSources  []string         `json:"unboundSources,omitempty"`
	UnboundPoints   []string         `json:"unboundPoints,omitempty"`
	UnboundCases    []string         `json:"unboundCases,omitempty"`
	SetStatus       DecisionStatus   `json:"setStatus,omitempty"`
}

// WhitelistEntry is an approved source-point-case edge.
type WhitelistEntry struct {
	ReviewKind ReviewKind `json:"reviewKind"`
	SourceID   string     `json:"sourceId"`
	PointID    string     `json:"pointId"`
	CaseID     string     `json:"caseId"`
}

// ApprovedWhitelist is projection-only output.  Its digest fields bind it to
// the exact source list, manifests, edge map, and review decisions used to
// produce it.
type ApprovedWhitelist struct {
	RequiredSourcesDigest    string                    `json:"requiredSourcesDigest"`
	ManifestDigest           string                    `json:"manifestDigest"`
	MapDigest                string                    `json:"mapDigest"`
	AlternativeVerifications []AlternativeVerification `json:"alternativeVerifications,omitempty"`
	Entries                  []WhitelistEntry          `json:"entries"`
	Digest                   string                    `json:"digest"`
}

// ValidationCandidate identifies the candidate against which execution ran.
type ValidationCandidate struct {
	Identity string `json:"identity"`
	Digest   string `json:"digest,omitempty"`
}

// ExecutionBinding freezes all digests and the candidate identity used by an
// execution report.  ExpectedCaseIDs is the frozen set of approved case IDs.
type ExecutionBinding struct {
	RequiredSourcesDigest string              `json:"requiredSourcesDigest"`
	ManifestDigest        string              `json:"manifestDigest"`
	MapDigest             string              `json:"mapDigest"`
	WhitelistDigest       string              `json:"whitelistDigest"`
	Candidate             ValidationCandidate `json:"candidate"`
	Scope                 ScopeMode           `json:"scope"`
	ExpectedCaseIDs       []string            `json:"expectedCaseIds"`
	ActualCaseIDs         []string            `json:"actualCaseIds"`
}

// ExecutionRecord accounts for one approved source-point-case edge.
type ExecutionRecord struct {
	ReviewKind             ReviewKind      `json:"reviewKind"`
	SourceID               string          `json:"sourceId"`
	PointID                string          `json:"pointId"`
	CaseID                 string          `json:"caseId"`
	Result                 ExecutionStatus `json:"result"`
	Provenance             Provenance      `json:"provenance"`
	InheritedFromCandidate string          `json:"inheritedFromCandidate,omitempty"`
	ReuseEvidence          string          `json:"reuseEvidence,omitempty"`
}

// ExecutionReport is the actual execution/identical inheritance accounting.
// NotExecuted is an explicit projection for callers that want a separate
// collection; records with ProvenanceNotExecuted must still be present in
// Records so the complete partition can be checked.
type ExecutionReport struct {
	Binding     ExecutionBinding  `json:"binding"`
	Records     []ExecutionRecord `json:"records"`
	NotExecuted []CoverageEdge    `json:"notExecuted,omitempty"`
}

// ValidationError is a stable, location-bearing contract error.
type ValidationError struct {
	Code    string
	Path    string
	Message string
}

func (e *ValidationError) Error() string {
	if e.Path == "" {
		return e.Code + ": " + e.Message
	}
	return e.Code + " at " + e.Path + ": " + e.Message
}

func invalid(code, path, message string) error {
	return &ValidationError{Code: code, Path: path, Message: message}
}

const (
	CodeInvalidBinding       = "INVALID_BINDING"
	CodeInvalidSource        = "INVALID_SOURCE"
	CodeDuplicateID          = "DUPLICATE_ID"
	CodeUnknownID            = "UNKNOWN_ID"
	CodeMissingCoverage      = "MISSING_COVERAGE"
	CodeOrphanEdge           = "ORPHAN_EDGE"
	CodeInvalidCaseBinding   = "INVALID_CASE_BINDING"
	CodeInvalidAlternative   = "INVALID_ALTERNATIVE_VERIFICATION"
	CodeIncompleteReview     = "INCOMPLETE_REVIEW"
	CodeReviewNotPass        = "REVIEW_NOT_PASS"
	CodeDigestMismatch       = "DIGEST_MISMATCH"
	CodeCandidateMismatch    = "CANDIDATE_MISMATCH"
	CodeExecutionSetMismatch = "EXECUTION_SET_MISMATCH"
	CodeExecutionNotPass     = "EXECUTION_NOT_PASS"
)

func nonempty(value, label string) bool { return strings.TrimSpace(value) != "" }

func validBinding(b SourceBinding) bool {
	return nonempty(b.RunID, "run") && nonempty(b.RequirementRevision, "requirement revision") && nonempty(b.SolutionRevision, "solution revision")
}

func validManifestBinding(b ManifestBinding) bool {
	if !validBinding(b.SourceBinding) || !nonempty(b.RouteScope, "route") || !nonempty(b.TopologyScope, "topology") {
		return false
	}
	return b.ReviewKind == ReviewBlackbox || b.ReviewKind == ReviewWhitebox || b.ReviewKind == ReviewMerge
}

func uniqueStrings(values []string) (map[string]struct{}, string) {
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		if !nonempty(value, "id") {
			return nil, value
		}
		if _, ok := seen[value]; ok {
			return nil, value
		}
		seen[value] = struct{}{}
	}
	return seen, ""
}

func sortedKeys(values map[string]struct{}) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedPointIDs(values map[string]AcceptancePoint) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedCaseIDs(values map[string]AcceptanceCase) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// ValidateRequiredSources validates the authoritative source list and its
// binding without interpreting requirement prose.
func ValidateRequiredSources(required RequiredSources) error {
	if !validBinding(required.Binding) {
		return invalid(CodeInvalidBinding, "requiredSources.binding", "run, requirement revision, and solution revision are required")
	}
	seen := make(map[string]struct{}, len(required.Sources))
	if len(required.Sources) == 0 {
		return invalid(CodeInvalidSource, "requiredSources.sources", "at least one confirmed source is required")
	}
	for i, source := range required.Sources {
		path := fmt.Sprintf("requiredSources.sources[%d]", i)
		if !nonempty(source.SourceID, "source") {
			return invalid(CodeInvalidSource, path+".sourceId", "source ID is required")
		}
		if _, exists := seen[source.SourceID]; exists {
			return invalid(CodeDuplicateID, path+".sourceId", "duplicate source ID")
		}
		seen[source.SourceID] = struct{}{}
		if source.Category != ProductRequirement && source.Category != SolutionConstraint {
			return invalid(CodeInvalidSource, path+".category", "unknown source category")
		}
		if source.Applicability != QAApplicability && source.Applicability != NonQAApplicability {
			return invalid(CodeInvalidSource, path+".applicability", "applicability must be QA or NON_QA")
		}
	}
	return nil
}

// Validate validates every source, selected manifest, edge, and alternative
// verification in deterministic layer order.
func (c CoverageContract) Validate() error {
	if err := ValidateRequiredSources(c.RequiredSources); err != nil {
		return err
	}
	selected := c.SelectedKinds
	if len(selected) == 0 {
		selected = make([]ReviewKind, 0, len(c.Manifests))
		for _, manifest := range c.Manifests {
			selected = append(selected, manifest.Binding.ReviewKind)
		}
	}
	selectedSet := make(map[ReviewKind]struct{}, len(selected))
	for i, kind := range selected {
		if kind != ReviewBlackbox && kind != ReviewWhitebox && kind != ReviewMerge {
			return invalid(CodeInvalidBinding, fmt.Sprintf("selectedKinds[%d]", i), "unknown review kind")
		}
		if _, exists := selectedSet[kind]; exists {
			return invalid(CodeDuplicateID, fmt.Sprintf("selectedKinds[%d]", i), "duplicate review kind")
		}
		selectedSet[kind] = struct{}{}
	}
	if len(c.Manifests) == 0 {
		for _, source := range c.RequiredSources.Sources {
			if source.Applicability == QAApplicability {
				return invalid(CodeMissingCoverage, "manifests", "QA source is absent from every selected manifest: "+source.SourceID)
			}
		}
		if len(selectedSet) != 0 {
			return invalid(CodeMissingCoverage, "manifests", "selected kind has no manifest")
		}
	}

	sourceByID := make(map[string]RequiredSource, len(c.RequiredSources.Sources))
	for _, source := range c.RequiredSources.Sources {
		sourceByID[source.SourceID] = source
	}
	coveredQA := make(map[string]struct{})
	seenKinds := make(map[ReviewKind]struct{}, len(c.Manifests))
	seenPointIDs := make(map[string]struct{})
	seenCaseIDs := make(map[string]struct{})
	seenWhiteboxRefs := make(map[string]struct{})
	for mi, manifest := range c.Manifests {
		path := fmt.Sprintf("manifests[%d]", mi)
		if !validManifestBinding(manifest.Binding) {
			return invalid(CodeInvalidBinding, path+".binding", "manifest binding is incomplete or has an unknown kind")
		}
		if !manifest.Binding.SourceBinding.equal(c.RequiredSources.Binding) {
			return invalid(CodeInvalidBinding, path+".binding", "manifest source binding does not match requiredSources")
		}
		if _, ok := selectedSet[manifest.Binding.ReviewKind]; !ok {
			return invalid(CodeInvalidBinding, path+".binding.reviewKind", "manifest uses an unselected kind")
		}
		if _, duplicate := seenKinds[manifest.Binding.ReviewKind]; duplicate {
			return invalid(CodeDuplicateID, path+".binding.reviewKind", "duplicate manifest for review kind")
		}
		seenKinds[manifest.Binding.ReviewKind] = struct{}{}

		sources, duplicate := uniqueStrings(manifest.Sources)
		if duplicate != "" {
			return invalid(CodeDuplicateID, path+".sources", "duplicate or empty manifest source ID")
		}
		if len(sources) == 0 {
			return invalid(CodeMissingCoverage, path+".sources", "manifest must record at least one source")
		}
		for _, sourceID := range sortedKeys(sources) {
			source, exists := sourceByID[sourceID]
			if !exists {
				return invalid(CodeUnknownID, path+".sources", "manifest references unknown source "+sourceID)
			}
			if source.Applicability != QAApplicability {
				return invalid(CodeInvalidSource, path+".sources", "non-QA source appears in a QA manifest")
			}
			coveredQA[sourceID] = struct{}{}
		}

		pointByID := make(map[string]AcceptancePoint, len(manifest.Points))
		for pi, point := range manifest.Points {
			ppath := fmt.Sprintf("%s.points[%d]", path, pi)
			if !nonempty(point.PointID, "point") || !nonempty(point.ObservableBehavior, "behavior") || !nonempty(point.Oracle, "oracle") {
				return invalid(CodeInvalidSource, ppath, "point ID, observable behavior, and oracle are required")
			}
			if _, exists := pointByID[point.PointID]; exists {
				return invalid(CodeDuplicateID, ppath+".pointId", "duplicate point ID")
			}
			if _, exists := seenPointIDs[point.PointID]; exists {
				return invalid(CodeDuplicateID, ppath+".pointId", "point ID is reused across manifests")
			}
			seenPointIDs[point.PointID] = struct{}{}
			pointByID[point.PointID] = point
		}
		caseByID := make(map[string]AcceptanceCase, len(manifest.Cases))
		for ci, qaCase := range manifest.Cases {
			cpath := fmt.Sprintf("%s.cases[%d]", path, ci)
			if !nonempty(qaCase.CaseID, "case") || !nonempty(qaCase.Oracle, "oracle") {
				return invalid(CodeInvalidCaseBinding, cpath, "case ID and oracle are required")
			}
			if _, exists := caseByID[qaCase.CaseID]; exists {
				return invalid(CodeDuplicateID, cpath+".caseId", "duplicate case ID")
			}
			if _, exists := seenCaseIDs[qaCase.CaseID]; exists {
				return invalid(CodeDuplicateID, cpath+".caseId", "case ID is reused across manifests")
			}
			seenCaseIDs[qaCase.CaseID] = struct{}{}
			caseByID[qaCase.CaseID] = qaCase
			expectedMode := CaseBlackbox
			switch manifest.Binding.ReviewKind {
			case ReviewWhitebox:
				expectedMode = CaseWhitebox
			case ReviewMerge:
				expectedMode = CaseMerge
			}
			if qaCase.Mode != expectedMode {
				return invalid(CodeInvalidCaseBinding, cpath+".mode", "case mode does not match review kind")
			}
			if qaCase.Mode == CaseWhitebox {
				if !nonempty(qaCase.TestRef, "test") || nonempty(qaCase.PublicEntry, "entry") || nonempty(qaCase.Preconditions, "preconditions") || nonempty(qaCase.Steps, "steps") {
					return invalid(CodeInvalidCaseBinding, cpath, "whitebox cases require only a unique test reference and oracle")
				}
				if _, duplicate := seenWhiteboxRefs[qaCase.TestRef]; duplicate {
					return invalid(CodeDuplicateID, cpath+".testRef", "duplicate whitebox test reference")
				}
				seenWhiteboxRefs[qaCase.TestRef] = struct{}{}
			} else if !nonempty(qaCase.PublicEntry, "entry") || !nonempty(qaCase.Preconditions, "preconditions") || !nonempty(qaCase.Steps, "steps") || nonempty(qaCase.TestRef, "test") {
				return invalid(CodeInvalidCaseBinding, cpath, "blackbox/merge cases require public entry, preconditions, steps, and oracle")
			}
		}

		edges := make(map[string]struct{}, len(manifest.Edges))
		hasSource := make(map[string]struct{})
		hasPoint := make(map[string]struct{})
		hasCase := make(map[string]struct{})
		for ei, edge := range manifest.Edges {
			epath := fmt.Sprintf("%s.edges[%d]", path, ei)
			if _, ok := sources[edge.SourceID]; !ok {
				return invalid(CodeOrphanEdge, epath+".sourceId", "edge source is not declared by this manifest")
			}
			if _, ok := pointByID[edge.PointID]; !ok {
				return invalid(CodeOrphanEdge, epath+".pointId", "edge point is not declared by this manifest")
			}
			if _, ok := caseByID[edge.CaseID]; !ok {
				return invalid(CodeOrphanEdge, epath+".caseId", "edge case is not declared by this manifest")
			}
			if edge.ReviewKind != "" && edge.ReviewKind != manifest.Binding.ReviewKind {
				return invalid(CodeInvalidBinding, epath+".reviewKind", "edge review kind does not match manifest")
			}
			if _, duplicate := edges[edge.key()]; duplicate {
				return invalid(CodeDuplicateID, epath, "duplicate source-point-case edge")
			}
			edges[edge.key()] = struct{}{}
			hasSource[edge.SourceID] = struct{}{}
			hasPoint[edge.PointID] = struct{}{}
			hasCase[edge.CaseID] = struct{}{}
		}
		for _, sourceID := range sortedKeys(sources) {
			if _, ok := hasSource[sourceID]; !ok {
				return invalid(CodeMissingCoverage, path+".sources", "manifest source has no point/case edge: "+sourceID)
			}
		}
		for _, pointID := range sortedPointIDs(pointByID) {
			if _, ok := hasPoint[pointID]; !ok {
				return invalid(CodeMissingCoverage, path+".points", "point has no source/case edge: "+pointID)
			}
		}
		for _, caseID := range sortedCaseIDs(caseByID) {
			if _, ok := hasCase[caseID]; !ok {
				return invalid(CodeMissingCoverage, path+".cases", "case has no source/point edge: "+caseID)
			}
		}
	}
	selectedKinds := make([]string, 0, len(selectedSet))
	for kind := range selectedSet {
		selectedKinds = append(selectedKinds, string(kind))
	}
	sort.Strings(selectedKinds)
	for _, kindID := range selectedKinds {
		kind := ReviewKind(kindID)
		if _, ok := seenKinds[kind]; !ok {
			return invalid(CodeMissingCoverage, "manifests", "selected kind has no manifest")
		}
	}
	for _, source := range c.RequiredSources.Sources {
		if source.Applicability == QAApplicability {
			if _, ok := coveredQA[source.SourceID]; !ok {
				return invalid(CodeMissingCoverage, "manifests", "QA source is absent from every selected manifest: "+source.SourceID)
			}
		}
	}
	verificationBySource := make(map[string]struct{}, len(c.AlternativeVerifications))
	for i, verification := range c.AlternativeVerifications {
		path := fmt.Sprintf("alternativeVerifications[%d]", i)
		source, exists := sourceByID[verification.SourceID]
		if !exists || source.Applicability != NonQAApplicability {
			return invalid(CodeInvalidAlternative, path+".sourceId", "alternative verification must target a known NON_QA source")
		}
		if _, duplicate := verificationBySource[verification.SourceID]; duplicate {
			return invalid(CodeDuplicateID, path+".sourceId", "duplicate alternative verification")
		}
		verificationBySource[verification.SourceID] = struct{}{}
		if !nonempty(verification.Reason, "reason") || !nonempty(verification.Method, "method") || verification.Status != StatusPass || !nonempty(verification.Evidence, "evidence") {
			return invalid(CodeInvalidAlternative, path, "non-QA verification requires reason, method, PASS, and evidence")
		}
	}
	for _, source := range c.RequiredSources.Sources {
		if source.Applicability == NonQAApplicability {
			if _, ok := verificationBySource[source.SourceID]; !ok {
				return invalid(CodeInvalidAlternative, "alternativeVerifications", "NON_QA source has no PASS verification: "+source.SourceID)
			}
		}
	}
	return nil
}

// ValidateManifest validates a manifest against the authoritative source list
// as a convenience for design-time callers.  Cross-manifest coverage is
// checked by CoverageContract.Validate.
func ValidateManifest(required RequiredSources, manifest AcceptanceManifest) error {
	return (CoverageContract{RequiredSources: required, Manifests: []AcceptanceManifest{manifest}, SelectedKinds: []ReviewKind{manifest.Binding.ReviewKind}}).Validate()
}

// Validate is the package-level spelling used by adapters that treat the
// contract as a standalone value.
func Validate(contract CoverageContract) error { return contract.Validate() }

// Validate checks this manifest against a required source list.
func (m AcceptanceManifest) Validate(required RequiredSources) error {
	return ValidateManifest(required, m)
}

// RequiredSourcesDigest returns a canonical digest of the authoritative list.
func (c CoverageContract) RequiredSourcesDigest() (string, error) {
	if err := ValidateRequiredSources(c.RequiredSources); err != nil {
		return "", err
	}
	copyValue := c.RequiredSources
	copyValue.Sources = append([]RequiredSource(nil), copyValue.Sources...)
	sort.Slice(copyValue.Sources, func(i, j int) bool { return copyValue.Sources[i].SourceID < copyValue.Sources[j].SourceID })
	return canonicalDigest(copyValue)
}

// RequiredSourcesDigest is also available directly on the source projection.
func (r RequiredSources) Digest() (string, error) {
	return (CoverageContract{RequiredSources: r}).RequiredSourcesDigest()
}

// ContractDigests groups the independently bound digests consumed by review
// and execution.  WhitelistDigest is populated only after review projection.
type ContractDigests struct {
	RequiredSourcesDigest string
	ManifestDigest        string
	MapDigest             string
	WhitelistDigest       string
}

// Digests computes all pre-review contract digests in one deterministic call.
func (c CoverageContract) Digests() (ContractDigests, error) {
	required, err := c.RequiredSourcesDigest()
	if err != nil {
		return ContractDigests{}, err
	}
	manifest, err := c.ManifestDigest()
	if err != nil {
		return ContractDigests{}, err
	}
	mapDigest, err := c.MapDigest()
	if err != nil {
		return ContractDigests{}, err
	}
	return ContractDigests{RequiredSourcesDigest: required, ManifestDigest: manifest, MapDigest: mapDigest}, nil
}

// ManifestDigest returns a canonical digest of all manifests, sorted by kind
// and IDs.  Edges intentionally live in MapDigest.
func (c CoverageContract) ManifestDigest() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	manifests := append([]AcceptanceManifest(nil), c.Manifests...)
	for i := range manifests {
		manifests[i].Sources = append([]string(nil), manifests[i].Sources...)
		manifests[i].Points = append([]AcceptancePoint(nil), manifests[i].Points...)
		manifests[i].Cases = append([]AcceptanceCase(nil), manifests[i].Cases...)
		sort.Strings(manifests[i].Sources)
		sort.Slice(manifests[i].Points, func(a, b int) bool { return manifests[i].Points[a].PointID < manifests[i].Points[b].PointID })
		sort.Slice(manifests[i].Cases, func(a, b int) bool { return manifests[i].Cases[a].CaseID < manifests[i].Cases[b].CaseID })
		manifests[i].Edges = nil
	}
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].Binding.ReviewKind < manifests[j].Binding.ReviewKind })
	return canonicalDigest(manifests)
}

// Digest computes the canonical manifest digest for a single manifest.
func (m AcceptanceManifest) Digest(required RequiredSources) (string, error) {
	return (CoverageContract{RequiredSources: required, SelectedKinds: []ReviewKind{m.Binding.ReviewKind}, Manifests: []AcceptanceManifest{m}}).ManifestDigest()
}

// MapDigest returns a canonical digest of all source-point-case edges.
func (c CoverageContract) MapDigest() (string, error) {
	if err := c.Validate(); err != nil {
		return "", err
	}
	type boundMap struct {
		Binding ManifestBinding `json:"binding"`
		Edges   []CoverageEdge  `json:"edges"`
	}
	maps := make([]boundMap, len(c.Manifests))
	for i, manifest := range c.Manifests {
		maps[i] = boundMap{Binding: manifest.Binding, Edges: append([]CoverageEdge(nil), manifest.Edges...)}
		sort.Slice(maps[i].Edges, func(a, b int) bool { return maps[i].Edges[a].key() < maps[i].Edges[b].key() })
	}
	sort.Slice(maps, func(i, j int) bool { return maps[i].Binding.ReviewKind < maps[j].Binding.ReviewKind })
	return canonicalDigest(maps)
}

func canonicalDigest(value any) (string, error) {
	data, err := CanonicalJSON(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

// CanonicalJSON encodes a map-free value using encoding/json's deterministic
// struct field order.  Callers should sort semantically unordered slices
// before invoking it; the component digest methods in this package do so.
func CanonicalJSON(value any) ([]byte, error) { return json.Marshal(value) }

// CanonicalDigest is exported for callers that need the same sha256: encoding
// for already-validated, map-free contract projections.
func CanonicalDigest(value any) (string, error) { return canonicalDigest(value) }

// ValidateReviews verifies complete item-level PASS decisions and projects
// the approved whitelist.  Reviews must contain exactly one result for every
// manifest in the contract.
func (c CoverageContract) ValidateReviews(reviews []ReviewResult) (ApprovedWhitelist, error) {
	if err := c.Validate(); err != nil {
		return ApprovedWhitelist{}, err
	}
	if len(reviews) != len(c.Manifests) {
		return ApprovedWhitelist{}, invalid(CodeIncompleteReview, "reviews", "one review result is required per manifest")
	}
	manifestByKind := make(map[ReviewKind]AcceptanceManifest, len(c.Manifests))
	for _, manifest := range c.Manifests {
		manifestByKind[manifest.Binding.ReviewKind] = manifest
	}
	seenKinds := make(map[ReviewKind]struct{}, len(reviews))
	entries := make([]WhitelistEntry, 0)
	for ri, review := range reviews {
		path := fmt.Sprintf("reviews[%d]", ri)
		manifest, exists := manifestByKind[review.Binding.ReviewKind]
		if !exists || !review.Binding.equal(manifest.Binding) {
			return ApprovedWhitelist{}, invalid(CodeInvalidBinding, path+".binding", "review binding does not match a manifest")
		}
		if review.Scope != ScopeFull && review.Scope != ScopeAffected {
			return ApprovedWhitelist{}, invalid(CodeIncompleteReview, path+".scope", "review scope must be FULL or AFFECTED")
		}
		if review.SetStatus != "" && review.SetStatus != StatusPass {
			return ApprovedWhitelist{}, invalid(CodeReviewNotPass, path+".setStatus", "set-level review status is not PASS")
		}
		if len(review.UnboundSources) != 0 || len(review.UnboundPoints) != 0 || len(review.UnboundCases) != 0 {
			return ApprovedWhitelist{}, invalid(CodeIncompleteReview, path, "review contains unbound source, point, or case entries")
		}
		if _, duplicate := seenKinds[review.Binding.ReviewKind]; duplicate {
			return ApprovedWhitelist{}, invalid(CodeDuplicateID, path+".binding.reviewKind", "duplicate review result")
		}
		seenKinds[review.Binding.ReviewKind] = struct{}{}
		if err := validateDecisions(review.SourceDecisions, manifest.Sources, path+".sourceDecisions", "source"); err != nil {
			return ApprovedWhitelist{}, err
		}
		pointIDs := make([]string, len(manifest.Points))
		for i := range manifest.Points {
			pointIDs[i] = manifest.Points[i].PointID
		}
		if err := validateDecisions(review.PointDecisions, pointIDs, path+".pointDecisions", "point"); err != nil {
			return ApprovedWhitelist{}, err
		}
		caseIDs := make([]string, len(manifest.Cases))
		for i := range manifest.Cases {
			caseIDs[i] = manifest.Cases[i].CaseID
		}
		if err := validateDecisions(review.CaseDecisions, caseIDs, path+".caseDecisions", "case"); err != nil {
			return ApprovedWhitelist{}, err
		}
		for _, edge := range manifest.Edges {
			entries = append(entries, WhitelistEntry{ReviewKind: manifest.Binding.ReviewKind, SourceID: edge.SourceID, PointID: edge.PointID, CaseID: edge.CaseID})
		}
	}
	if len(seenKinds) != len(manifestByKind) {
		return ApprovedWhitelist{}, invalid(CodeIncompleteReview, "reviews", "a manifest review is missing")
	}
	sort.Slice(entries, func(i, j int) bool {
		a, b := entries[i], entries[j]
		if a.ReviewKind != b.ReviewKind {
			return a.ReviewKind < b.ReviewKind
		}
		if a.SourceID != b.SourceID {
			return a.SourceID < b.SourceID
		}
		if a.PointID != b.PointID {
			return a.PointID < b.PointID
		}
		return a.CaseID < b.CaseID
	})
	requiredDigest, err := c.RequiredSourcesDigest()
	if err != nil {
		return ApprovedWhitelist{}, err
	}
	manifestDigest, err := c.ManifestDigest()
	if err != nil {
		return ApprovedWhitelist{}, err
	}
	mapDigest, err := c.MapDigest()
	if err != nil {
		return ApprovedWhitelist{}, err
	}
	w := ApprovedWhitelist{RequiredSourcesDigest: requiredDigest, ManifestDigest: manifestDigest, MapDigest: mapDigest, Entries: entries}
	w.AlternativeVerifications = append([]AlternativeVerification(nil), c.AlternativeVerifications...)
	sort.Slice(w.AlternativeVerifications, func(i, j int) bool {
		return w.AlternativeVerifications[i].SourceID < w.AlternativeVerifications[j].SourceID
	})
	w.Digest, err = canonicalDigest(w)
	if err != nil {
		return ApprovedWhitelist{}, err
	}
	return w, nil
}

func validateDecisions(decisions []ReviewDecision, expected []string, path, label string) error {
	if len(decisions) != len(expected) {
		return invalid(CodeIncompleteReview, path, "review must contain exactly one decision for every "+label)
	}
	want := make(map[string]struct{}, len(expected))
	for _, id := range expected {
		want[id] = struct{}{}
	}
	seen := make(map[string]struct{}, len(decisions))
	for i, decision := range decisions {
		if _, ok := want[decision.ID]; !ok {
			return invalid(CodeUnknownID, fmt.Sprintf("%s[%d].id", path, i), "unknown "+label+" ID")
		}
		if _, duplicate := seen[decision.ID]; duplicate {
			return invalid(CodeDuplicateID, fmt.Sprintf("%s[%d].id", path, i), "duplicate "+label+" decision")
		}
		seen[decision.ID] = struct{}{}
		if decision.Status != StatusPass {
			return invalid(CodeReviewNotPass, fmt.Sprintf("%s[%d].status", path, i), label+" decision is not PASS")
		}
	}
	for _, id := range expected {
		if _, ok := seen[id]; !ok {
			return invalid(CodeIncompleteReview, path, "missing "+label+" decision: "+id)
		}
	}
	return nil
}

// ProjectWhitelist is a short alias for ValidateReviews.
func ProjectWhitelist(c CoverageContract, reviews []ReviewResult) (ApprovedWhitelist, error) {
	return c.ValidateReviews(reviews)
}

// ValidateReviews is the package-level spelling of CoverageContract.ValidateReviews.
func ValidateReviews(c CoverageContract, reviews []ReviewResult) (ApprovedWhitelist, error) {
	return c.ValidateReviews(reviews)
}

// WhitelistDigest computes the digest of a projected whitelist after checking
// that all required digest bindings are present.
func (w ApprovedWhitelist) WhitelistDigest() (string, error) {
	if !nonempty(w.RequiredSourcesDigest, "required") || !nonempty(w.ManifestDigest, "manifest") || !nonempty(w.MapDigest, "map") {
		return "", invalid(CodeDigestMismatch, "whitelist", "whitelist digest bindings are incomplete")
	}
	copyValue := w
	copyValue.Digest = ""
	digest, err := canonicalDigest(copyValue)
	if err != nil {
		return "", err
	}
	if !nonempty(w.Digest, "whitelist") || w.Digest != digest {
		return "", invalid(CodeDigestMismatch, "whitelist.digest", "whitelist digest does not match its projection")
	}
	return digest, nil
}

func validateWhitelist(c CoverageContract, whitelist ApprovedWhitelist, requiredDigest, manifestDigest, mapDigest string) error {
	if whitelist.RequiredSourcesDigest != requiredDigest || whitelist.ManifestDigest != manifestDigest || whitelist.MapDigest != mapDigest {
		return invalid(CodeDigestMismatch, "whitelist", "whitelist bindings do not match current contract")
	}
	if _, err := whitelist.WhitelistDigest(); err != nil {
		return err
	}
	wantAlternatives := append([]AlternativeVerification(nil), c.AlternativeVerifications...)
	sort.Slice(wantAlternatives, func(i, j int) bool { return wantAlternatives[i].SourceID < wantAlternatives[j].SourceID })
	if !reflect.DeepEqual(wantAlternatives, whitelist.AlternativeVerifications) {
		return invalid(CodeDigestMismatch, "whitelist.alternativeVerifications", "whitelist non-QA verification projection does not match contract")
	}
	expected := make(map[string]WhitelistEntry)
	for _, manifest := range c.Manifests {
		for _, edge := range manifest.Edges {
			entry := WhitelistEntry{ReviewKind: manifest.Binding.ReviewKind, SourceID: edge.SourceID, PointID: edge.PointID, CaseID: edge.CaseID}
			key := string(entry.ReviewKind) + "\x00" + entry.SourceID + "\x00" + entry.PointID + "\x00" + entry.CaseID
			expected[key] = entry
		}
	}
	seen := make(map[string]struct{}, len(whitelist.Entries))
	for i, entry := range whitelist.Entries {
		key := string(entry.ReviewKind) + "\x00" + entry.SourceID + "\x00" + entry.PointID + "\x00" + entry.CaseID
		if _, duplicate := seen[key]; duplicate {
			return invalid(CodeDuplicateID, fmt.Sprintf("whitelist.entries[%d]", i), "duplicate whitelist edge")
		}
		seen[key] = struct{}{}
		if _, ok := expected[key]; !ok {
			return invalid(CodeUnknownID, fmt.Sprintf("whitelist.entries[%d]", i), "whitelist edge is not present in the current map")
		}
	}
	if len(seen) != len(expected) {
		return invalid(CodeMissingCoverage, "whitelist.entries", "whitelist omits an approved source-point-case edge")
	}
	return nil
}

// ValidateExecution checks candidate/digest bindings and the complete expected
// edge partition.  AFFECTED reports may use inherited PASS records only when a
// source candidate and reuse evidence are both supplied.
func (c CoverageContract) ValidateExecution(whitelist ApprovedWhitelist, report ExecutionReport) error {
	if err := c.Validate(); err != nil {
		return err
	}
	requiredDigest, err := c.RequiredSourcesDigest()
	if err != nil {
		return err
	}
	manifestDigest, err := c.ManifestDigest()
	if err != nil {
		return err
	}
	mapDigest, err := c.MapDigest()
	if err != nil {
		return err
	}
	if err := validateWhitelist(c, whitelist, requiredDigest, manifestDigest, mapDigest); err != nil {
		return err
	}
	whitelistDigest, err := whitelist.WhitelistDigest()
	if err != nil {
		return err
	}
	if report.Binding.RequiredSourcesDigest != requiredDigest || report.Binding.ManifestDigest != manifestDigest || report.Binding.MapDigest != mapDigest || report.Binding.WhitelistDigest != whitelistDigest {
		return invalid(CodeDigestMismatch, "execution.binding", "execution does not match current contract/whitelist digests")
	}
	if !nonempty(report.Binding.Candidate.Identity, "candidate") {
		return invalid(CodeCandidateMismatch, "execution.binding.candidate.identity", "candidate identity is required")
	}
	if c.Candidate == nil {
		return invalid(CodeCandidateMismatch, "contract.candidate", "current validation candidate is required for execution reconciliation")
	}
	if *c.Candidate != report.Binding.Candidate {
		return invalid(CodeCandidateMismatch, "execution.binding.candidate", "execution candidate does not match the current validation candidate")
	}
	if report.Binding.Scope != ScopeFull && report.Binding.Scope != ScopeAffected {
		return invalid(CodeExecutionSetMismatch, "execution.binding.scope", "scope must be FULL or AFFECTED")
	}
	caseSet := make(map[string]struct{})
	for _, entry := range whitelist.Entries {
		caseSet[entry.CaseID] = struct{}{}
	}
	wantCaseIDs := make([]string, 0, len(caseSet))
	for id := range caseSet {
		wantCaseIDs = append(wantCaseIDs, id)
	}
	sort.Strings(wantCaseIDs)
	actualExpected := append([]string(nil), report.Binding.ExpectedCaseIDs...)
	sort.Strings(actualExpected)
	if len(actualExpected) != len(wantCaseIDs) {
		return invalid(CodeExecutionSetMismatch, "execution.binding.expectedCaseIds", "expected case ID set does not match whitelist")
	}
	for i := range wantCaseIDs {
		if wantCaseIDs[i] != actualExpected[i] {
			return invalid(CodeExecutionSetMismatch, "execution.binding.expectedCaseIds", "expected case ID set does not match whitelist")
		}
	}
	edgeByKey := make(map[string]WhitelistEntry, len(whitelist.Entries))
	for _, edge := range whitelist.Entries {
		edgeByKey[string(edge.ReviewKind)+"\x00"+edge.SourceID+"\x00"+edge.PointID+"\x00"+edge.CaseID] = edge
	}
	seen := make(map[string]struct{}, len(report.Records))
	passByEdge := make(map[string]struct{}, len(report.Records))
	passByPoint := make(map[string]int)
	notExecutedByKey := make(map[string]struct{})
	actualCaseSet := make(map[string]struct{})
	for i, record := range report.Records {
		path := fmt.Sprintf("execution.records[%d]", i)
		key := executionEdgeKey(record.ReviewKind, record.SourceID, record.PointID, record.CaseID)
		if _, ok := edgeByKey[key]; !ok {
			return invalid(CodeUnknownID, path, "execution record is not an approved edge")
		}
		if _, duplicate := seen[key]; duplicate {
			return invalid(CodeDuplicateID, path, "duplicate execution edge")
		}
		seen[key] = struct{}{}
		actualCaseSet[record.CaseID] = struct{}{}
		if record.Provenance != ProvenanceExecuted && record.Provenance != ProvenanceInherited && record.Provenance != ProvenanceNotExecuted {
			return invalid(CodeExecutionSetMismatch, path+".provenance", "unknown execution provenance")
		}
		if report.Binding.Scope == ScopeFull && record.Provenance != ProvenanceExecuted {
			return invalid(CodeExecutionSetMismatch, path+".provenance", "FULL execution cannot use inherited or not-executed records")
		}
		if record.Provenance == ProvenanceExecuted {
			if record.Result != ExecutedPass {
				return invalid(CodeExecutionNotPass, path+".result", "executed result is not EXECUTED_PASS")
			}
			if record.InheritedFromCandidate != "" || record.ReuseEvidence != "" {
				return invalid(CodeExecutionSetMismatch, path, "executed record cannot carry inheritance evidence")
			}
			passByEdge[key] = struct{}{}
			passByPoint[string(record.ReviewKind)+"\x00"+record.PointID]++
		} else if record.Provenance == ProvenanceInherited {
			if record.Result == AuthorizedSkip {
				return invalid(CodeExecutionNotPass, path, fmt.Sprintf("inherited result %q is not EXECUTED_PASS", record.Result))
			}
			if record.Result != ExecutedPass || !nonempty(record.InheritedFromCandidate, "source candidate") || !nonempty(record.ReuseEvidence, "reuse evidence") || record.InheritedFromCandidate == report.Binding.Candidate.Identity {
				return invalid(CodeExecutionNotPass, path, "inherited PASS requires a distinct source candidate and reuse evidence")
			}
			passByEdge[key] = struct{}{}
			passByPoint[string(record.ReviewKind)+"\x00"+record.PointID]++
		} else {
			if record.Result == ExecutedPass {
				return invalid(CodeExecutionNotPass, path+".result", "not-executed record cannot be EXECUTED_PASS")
			}
			notExecutedByKey[key] = struct{}{}
		}
	}
	if report.Binding.Scope == ScopeFull {
		if len(report.NotExecuted) != 0 {
			return invalid(CodeExecutionSetMismatch, "execution.notExecuted", "FULL execution cannot list unexecuted edges")
		}
		if len(seen) != len(edgeByKey) {
			return invalid(CodeExecutionSetMismatch, "execution.records", "FULL execution must account for every approved edge")
		}
	} else {
		if len(seen) != len(edgeByKey) {
			return invalid(CodeExecutionSetMismatch, "execution.records", "AFFECTED execution must partition every expected edge")
		}
		for _, edge := range report.NotExecuted {
			var key string
			for approvedKey, approved := range edgeByKey {
				if (edge.ReviewKind == "" || approved.ReviewKind == edge.ReviewKind) && approved.SourceID == edge.SourceID && approved.PointID == edge.PointID && approved.CaseID == edge.CaseID {
					key = approvedKey
					break
				}
			}
			if key == "" {
				return invalid(CodeExecutionSetMismatch, "execution.notExecuted", "not-executed edge is not approved")
			}
			if _, ok := notExecutedByKey[key]; !ok {
				return invalid(CodeExecutionSetMismatch, "execution.notExecuted", "not-executed edge is not recorded with NOT_EXECUTED provenance")
			}
		}
		if len(report.NotExecuted) != len(notExecutedByKey) {
			return invalid(CodeExecutionSetMismatch, "execution.notExecuted", "AFFECTED not-executed collection does not match execution records")
		}
	}
	actualIDs := make([]string, 0, len(actualCaseSet))
	for id := range actualCaseSet {
		actualIDs = append(actualIDs, id)
	}
	sort.Strings(actualIDs)
	reportedActual := append([]string(nil), report.Binding.ActualCaseIDs...)
	sort.Strings(reportedActual)
	if len(reportedActual) != len(actualIDs) {
		return invalid(CodeExecutionSetMismatch, "execution.binding.actualCaseIds", "actual case ID set does not match execution records")
	}
	for i := range actualIDs {
		if actualIDs[i] != reportedActual[i] {
			return invalid(CodeExecutionSetMismatch, "execution.binding.actualCaseIds", "actual case ID set does not match execution records")
		}
	}
	for _, edge := range whitelist.Entries {
		pointKey := string(edge.ReviewKind) + "\x00" + edge.PointID
		if passByPoint[pointKey] == 0 {
			return invalid(CodeExecutionNotPass, "execution.records", "point has no PASS for edge "+edge.PointID)
		}
		if _, ok := passByEdge[executionEdgeKey(edge.ReviewKind, edge.SourceID, edge.PointID, edge.CaseID)]; !ok {
			return invalid(CodeExecutionNotPass, "execution.records", "edge has no PASS for case "+edge.CaseID)
		}
	}
	return nil
}

// ValidateExecution is also available as a package-level helper.
func ValidateExecution(c CoverageContract, whitelist ApprovedWhitelist, report ExecutionReport) error {
	return c.ValidateExecution(whitelist, report)
}

// ValidateExecutionForCandidate is the explicit form used when the caller
// keeps the current candidate outside the coverage contract value.
func ValidateExecutionForCandidate(c CoverageContract, whitelist ApprovedWhitelist, report ExecutionReport, candidate ValidationCandidate) error {
	c.Candidate = &candidate
	return c.ValidateExecution(whitelist, report)
}
