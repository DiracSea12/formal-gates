package coverage

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func fixtureContract() (CoverageContract, []ReviewResult) {
	binding := SourceBinding{RunID: "run-1", ChildID: "", RequirementRevision: "req-1", SolutionRevision: "sol-1"}
	required := RequiredSources{Binding: binding, Sources: []RequiredSource{
		{SourceID: "REQ-1", Category: ProductRequirement, Applicability: QAApplicability},
		{SourceID: "SOL-1", Category: SolutionConstraint, Applicability: NonQAApplicability},
	}}
	blackboxBinding := ManifestBinding{SourceBinding: binding, ReviewKind: ReviewBlackbox, RouteScope: "regular", TopologyScope: "master"}
	whiteboxBinding := ManifestBinding{SourceBinding: binding, ReviewKind: ReviewWhitebox, RouteScope: "regular", TopologyScope: "master"}
	blackbox := AcceptanceManifest{
		Binding: blackboxBinding,
		Sources: []string{"REQ-1"},
		Points:  []AcceptancePoint{{PointID: "P-1", ObservableBehavior: "first behavior", Oracle: "first oracle"}, {PointID: "P-2", ObservableBehavior: "second behavior", Oracle: "second oracle"}},
		Cases:   []AcceptanceCase{{CaseID: "C-1", Mode: CaseBlackbox, PublicEntry: "workflow show", Preconditions: "run exists", Steps: "invoke show", Oracle: "status is printed"}, {CaseID: "C-2", Mode: CaseBlackbox, PublicEntry: "workflow status", Preconditions: "run exists", Steps: "invoke status", Oracle: "status is deterministic"}},
		Edges:   []CoverageEdge{{SourceID: "REQ-1", PointID: "P-1", CaseID: "C-1"}, {SourceID: "REQ-1", PointID: "P-1", CaseID: "C-2"}, {SourceID: "REQ-1", PointID: "P-2", CaseID: "C-1"}, {SourceID: "REQ-1", PointID: "P-2", CaseID: "C-2"}},
	}
	whitebox := AcceptanceManifest{
		Binding: whiteboxBinding,
		Sources: []string{"REQ-1"},
		Points:  []AcceptancePoint{{PointID: "P-3", ObservableBehavior: "structural behavior", Oracle: "digest is stable"}},
		Cases:   []AcceptanceCase{{CaseID: "C-3", Mode: CaseWhitebox, TestRef: "internal/engine/coverage/contract_test.go::TestContractDigestOrder", Oracle: "equal digest"}},
		Edges:   []CoverageEdge{{SourceID: "REQ-1", PointID: "P-3", CaseID: "C-3"}},
	}
	c := CoverageContract{RequiredSources: required, SelectedKinds: []ReviewKind{ReviewBlackbox, ReviewWhitebox}, Manifests: []AcceptanceManifest{blackbox, whitebox}, AlternativeVerifications: []AlternativeVerification{{SourceID: "SOL-1", Reason: "no applicable QA point", Method: "go test", Status: StatusPass, Evidence: "evidence digest"}}}
	reviews := []ReviewResult{
		{Binding: blackboxBinding, Scope: ScopeFull, SourceDecisions: []ReviewDecision{{ID: "REQ-1", Status: StatusPass}}, PointDecisions: []ReviewDecision{{ID: "P-1", Status: StatusPass}, {ID: "P-2", Status: StatusPass}}, CaseDecisions: []ReviewDecision{{ID: "C-1", Status: StatusPass}, {ID: "C-2", Status: StatusPass}}, SetStatus: StatusPass},
		{Binding: whiteboxBinding, Scope: ScopeFull, SourceDecisions: []ReviewDecision{{ID: "REQ-1", Status: StatusPass}}, PointDecisions: []ReviewDecision{{ID: "P-3", Status: StatusPass}}, CaseDecisions: []ReviewDecision{{ID: "C-3", Status: StatusPass}}, SetStatus: StatusPass},
	}
	return c, reviews
}

func TestContractValidatesManyToManyAndProjectsWhitelist(t *testing.T) {
	c, reviews := fixtureContract()
	if err := c.Validate(); err != nil {
		t.Fatalf("fixture should validate: %v", err)
	}
	w, err := c.ValidateReviews(reviews)
	if err != nil {
		t.Fatalf("reviews should validate: %v", err)
	}
	if len(w.Entries) != 5 || w.Digest == "" {
		t.Fatalf("unexpected whitelist: %#v", w)
	}
}

func TestContractRejectsMissingQAAndNonQASources(t *testing.T) {
	c, _ := fixtureContract()
	c.Manifests[0].Sources = nil
	var validationErr *ValidationError
	if !errors.As(c.Validate(), &validationErr) || validationErr.Code != CodeMissingCoverage {
		t.Fatalf("expected missing coverage, got %v", c.Validate())
	}
	c, _ = fixtureContract()
	c.Manifests[0].Sources[0] = "SOL-1"
	if !errors.As(c.Validate(), &validationErr) || validationErr.Code != CodeInvalidSource {
		t.Fatalf("expected non-QA manifest rejection, got %v", c.Validate())
	}
}

func TestContractRejectsCaseVariantAndOrphanEdges(t *testing.T) {
	c, _ := fixtureContract()
	c.Manifests[0].Cases[0].Mode = CaseWhitebox
	var validationErr *ValidationError
	if !errors.As(c.Validate(), &validationErr) || validationErr.Code != CodeInvalidCaseBinding {
		t.Fatalf("expected case variant rejection, got %v", c.Validate())
	}
	c, _ = fixtureContract()
	c.Manifests[0].Edges[0].PointID = "unknown"
	if !errors.As(c.Validate(), &validationErr) || validationErr.Code != CodeOrphanEdge {
		t.Fatalf("expected orphan edge rejection, got %v", c.Validate())
	}
}

func TestContractDigestOrder(t *testing.T) {
	c, _ := fixtureContract()
	first, err := c.MapDigest()
	if err != nil {
		t.Fatal(err)
	}
	c.Manifests[0].Edges[0], c.Manifests[0].Edges[3] = c.Manifests[0].Edges[3], c.Manifests[0].Edges[0]
	second, err := c.MapDigest()
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("map digest changed with input order: %s != %s", first, second)
	}
	c.Manifests[0].Binding.RouteScope = "changed-route"
	third, err := c.MapDigest()
	if err != nil {
		t.Fatal(err)
	}
	if first == third {
		t.Fatal("map digest did not change when route scope changed")
	}
}

func TestContractExecutionFullAndAffected(t *testing.T) {
	c, reviews := fixtureContract()
	w, err := c.ValidateReviews(reviews)
	if err != nil {
		t.Fatal(err)
	}
	requiredDigest, _ := c.RequiredSourcesDigest()
	manifestDigest, _ := c.ManifestDigest()
	mapDigest, _ := c.MapDigest()
	whitelistDigest, _ := w.WhitelistDigest()
	full := ExecutionReport{Binding: ExecutionBinding{RequiredSourcesDigest: requiredDigest, ManifestDigest: manifestDigest, MapDigest: mapDigest, WhitelistDigest: whitelistDigest, Candidate: ValidationCandidate{Identity: "candidate-1"}, Scope: ScopeFull, ExpectedCaseIDs: []string{"C-1", "C-2", "C-3"}, ActualCaseIDs: []string{"C-1", "C-2", "C-3"}}}
	for _, entry := range w.Entries {
		full.Records = append(full.Records, ExecutionRecord{ReviewKind: entry.ReviewKind, SourceID: entry.SourceID, PointID: entry.PointID, CaseID: entry.CaseID, Result: ExecutedPass, Provenance: ProvenanceExecuted})
	}
	if err := c.ValidateExecution(w, full); err != nil {
		t.Fatalf("full execution should validate: %v", err)
	}
	candidate := ValidationCandidate{Identity: "candidate-1"}
	c.Candidate = &candidate
	if err := c.ValidateExecution(w, full); err != nil {
		t.Fatalf("bound full execution should validate: %v", err)
	}
	full.Binding.Candidate.Identity = "candidate-old"
	if err := c.ValidateExecution(w, full); err == nil {
		t.Fatal("execution for a different candidate must be rejected")
	}
	full.Binding.Candidate.Identity = "candidate-1"
	affected := full
	affected.Binding.Scope = ScopeAffected
	affected.Records = nil
	for i, entry := range w.Entries {
		record := ExecutionRecord{ReviewKind: entry.ReviewKind, SourceID: entry.SourceID, PointID: entry.PointID, CaseID: entry.CaseID}
		if i == 0 {
			record.Result, record.Provenance = ExecutedPass, ProvenanceExecuted
		} else {
			record.Result, record.Provenance, record.InheritedFromCandidate, record.ReuseEvidence = ExecutedPass, ProvenanceInherited, "candidate-0", "view-receipt"
		}
		affected.Records = append(affected.Records, record)
	}
	if err := c.ValidateExecution(w, affected); err != nil {
		t.Fatalf("affected execution should validate: %v", err)
	}
	full.Binding.Candidate.Identity = "candidate-2"
	full.Records[0].Result = AuthorizedSkip
	if err := c.ValidateExecution(w, full); err == nil {
		t.Fatal("authorized skip must not pass execution")
	}
	full.Records[0].Result = ExecutedPass
	full.Binding.ActualCaseIDs = []string{"C-1"}
	if err := c.ValidateExecution(w, full); err == nil {
		t.Fatal("incomplete actual case set must be rejected")
	}
}

func TestContractExecutionRequiresPassForEveryMappedCase(t *testing.T) {
	c, reviews := fixtureContract()
	w, err := c.ValidateReviews(reviews)
	if err != nil {
		t.Fatal(err)
	}
	requiredDigest, _ := c.RequiredSourcesDigest()
	manifestDigest, _ := c.ManifestDigest()
	mapDigest, _ := c.MapDigest()
	whitelistDigest, _ := w.WhitelistDigest()
	report := ExecutionReport{
		Binding: ExecutionBinding{
			RequiredSourcesDigest: requiredDigest,
			ManifestDigest:        manifestDigest,
			MapDigest:             mapDigest,
			WhitelistDigest:       whitelistDigest,
			Candidate:             ValidationCandidate{Identity: "candidate-current"},
			Scope:                 ScopeAffected,
			ExpectedCaseIDs:       []string{"C-1", "C-2", "C-3"},
			ActualCaseIDs:         []string{"C-1", "C-2", "C-3"},
		},
	}
	for i, entry := range w.Entries {
		record := ExecutionRecord{
			ReviewKind: entry.ReviewKind,
			SourceID:   entry.SourceID,
			PointID:    entry.PointID,
			CaseID:     entry.CaseID,
			Result:     ExecutedPass,
			Provenance: ProvenanceExecuted,
		}
		if i == 1 {
			record.Result = ExecutedUnknown
			record.Provenance = ProvenanceNotExecuted
			report.NotExecuted = append(report.NotExecuted, CoverageEdge{ReviewKind: entry.ReviewKind, SourceID: entry.SourceID, PointID: entry.PointID, CaseID: entry.CaseID})
		}
		report.Records = append(report.Records, record)
	}
	var validationErr *ValidationError
	if err := c.ValidateExecution(w, report); !errors.As(err, &validationErr) {
		t.Fatalf("expected incomplete point execution rejection, got %v", err)
	} else if validationErr.Code != CodeExecutionNotPass {
		t.Fatalf("expected %s, got %s", CodeExecutionNotPass, validationErr.Code)
	}
}

func TestValidateCoverageMapRejectsMismatchedEdgeReviewKind(t *testing.T) {
	c, _ := fixtureContract()
	manifest := c.Manifests[0]
	coverageMap := manifest.Map()
	coverageMap.Edges[0].ReviewKind = ReviewWhitebox
	var validationErr *ValidationError
	if err := ValidateCoverageMap(manifest, coverageMap); !errors.As(err, &validationErr) {
		t.Fatalf("expected mismatched edge review kind rejection, got %v", err)
	} else if validationErr.Code != CodeInvalidBinding {
		t.Fatalf("expected %s, got %s", CodeInvalidBinding, validationErr.Code)
	}
}

func TestContractAffectedAuthorizedSkipDiagnosticNamesStatus(t *testing.T) {
	c, reviews := fixtureContract()
	w, err := c.ValidateReviews(reviews)
	if err != nil {
		t.Fatal(err)
	}
	requiredDigest, _ := c.RequiredSourcesDigest()
	manifestDigest, _ := c.ManifestDigest()
	mapDigest, _ := c.MapDigest()
	whitelistDigest, _ := w.WhitelistDigest()
	report := ExecutionReport{
		Binding: ExecutionBinding{
			RequiredSourcesDigest: requiredDigest,
			ManifestDigest:        manifestDigest,
			MapDigest:             mapDigest,
			WhitelistDigest:       whitelistDigest,
			Candidate:             ValidationCandidate{Identity: "candidate-current"},
			Scope:                 ScopeAffected,
			ExpectedCaseIDs:       []string{"C-1", "C-2", "C-3"},
			ActualCaseIDs:         []string{"C-1", "C-2", "C-3"},
		},
	}
	for i, entry := range w.Entries {
		record := ExecutionRecord{
			ReviewKind: entry.ReviewKind,
			SourceID:   entry.SourceID,
			PointID:    entry.PointID,
			CaseID:     entry.CaseID,
			Result:     ExecutedPass,
			Provenance: ProvenanceInherited,
		}
		if i == 0 {
			record.Provenance = ProvenanceExecuted
		} else {
			record.InheritedFromCandidate = "candidate-previous"
			record.ReuseEvidence = "view-receipt"
		}
		report.Records = append(report.Records, record)
	}
	report.Records[1].Result = AuthorizedSkip
	var validationErr *ValidationError
	if err := c.ValidateExecution(w, report); !errors.As(err, &validationErr) {
		t.Fatalf("expected AFFECTED AUTHORIZED_SKIP rejection, got %v", err)
	} else if validationErr.Code != CodeExecutionNotPass {
		t.Fatalf("expected %s, got %s", CodeExecutionNotPass, validationErr.Code)
	} else if !strings.Contains(validationErr.Message, string(AuthorizedSkip)) {
		t.Fatalf("diagnostic omitted %s: %q", AuthorizedSkip, validationErr.Message)
	}
}

func TestContractNoQASourcesUseAlternativeVerification(t *testing.T) {
	binding := SourceBinding{RunID: "run", RequirementRevision: "req", SolutionRevision: "sol"}
	c := CoverageContract{RequiredSources: RequiredSources{Binding: binding, Sources: []RequiredSource{{SourceID: "S", Category: SolutionConstraint, Applicability: NonQAApplicability}}}, AlternativeVerifications: []AlternativeVerification{{SourceID: "S", Reason: "none", Method: "manual", Status: StatusPass, Evidence: "proof"}}}
	if err := c.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestContractRejectsStaleWhitelistProjection(t *testing.T) {
	c, reviews := fixtureContract()
	w, err := c.ValidateReviews(reviews)
	if err != nil {
		t.Fatal(err)
	}
	w.Entries[0].CaseID = "C-2"
	if _, err := w.WhitelistDigest(); err == nil {
		t.Fatal("mutating a projected whitelist must invalidate its digest")
	}
}

func TestContractDigestProjectionDoesNotMutateInput(t *testing.T) {
	c, _ := fixtureContract()
	before := append([]string(nil), c.Manifests[0].Sources...)
	if _, err := c.ManifestDigest(); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, c.Manifests[0].Sources) {
		t.Fatal("digest changed caller-owned manifest ordering")
	}
}
