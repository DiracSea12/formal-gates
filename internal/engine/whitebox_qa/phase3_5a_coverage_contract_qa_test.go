package whitebox_qa

import (
	"errors"
	"reflect"
	"sort"
	"strings"
	"testing"

	"formal-gates/internal/engine/coverage"
)

// The helpers in this file are intentionally local to the QA-design tests.
// They build the confirmed source/manifest graph directly instead of calling
// any of the implementation's test fixtures.
func stage35aBinding() coverage.SourceBinding {
	return coverage.SourceBinding{RunID: "stage35a-run", RequirementRevision: "req-r1", SolutionRevision: "sol-r1"}
}

func stage35aManifestBinding(kind coverage.ReviewKind) coverage.ManifestBinding {
	return coverage.ManifestBinding{SourceBinding: stage35aBinding(), ReviewKind: kind, RouteScope: "regular", TopologyScope: "master"}
}

func stage35aContract() coverage.CoverageContract {
	binding := stage35aBinding()
	manifest := coverage.AcceptanceManifest{
		Binding: stage35aManifestBinding(coverage.ReviewBlackbox),
		Sources: []string{"REQ-A"},
		Points: []coverage.AcceptancePoint{
			{PointID: "POINT-A", ObservableBehavior: "the contract is checked", Oracle: "validation returns no error"},
			{PointID: "POINT-B", ObservableBehavior: "the map is projected", Oracle: "all edges remain addressable"},
		},
		Cases: []coverage.AcceptanceCase{
			{CaseID: "CASE-A", Mode: coverage.CaseBlackbox, PublicEntry: "formal-gates coverage validate", Preconditions: "a JSON contract is available", Steps: "invoke the documented coverage entry", Oracle: "the result reports valid=true"},
			{CaseID: "CASE-B", Mode: coverage.CaseBlackbox, PublicEntry: "formal-gates coverage project-whitelist", Preconditions: "a valid manifest review is available", Steps: "invoke the documented whitelist entry", Oracle: "the result contains approved entries"},
		},
		Edges: []coverage.CoverageEdge{
			{SourceID: "REQ-A", PointID: "POINT-A", CaseID: "CASE-A"},
			{SourceID: "REQ-A", PointID: "POINT-A", CaseID: "CASE-B"},
			{SourceID: "REQ-A", PointID: "POINT-B", CaseID: "CASE-A"},
			{SourceID: "REQ-A", PointID: "POINT-B", CaseID: "CASE-B"},
		},
	}
	return coverage.CoverageContract{
		RequiredSources: coverage.RequiredSources{Binding: binding, Sources: []coverage.RequiredSource{{SourceID: "REQ-A", Category: coverage.ProductRequirement, Applicability: coverage.QAApplicability}}},
		SelectedKinds:   []coverage.ReviewKind{coverage.ReviewBlackbox},
		Manifests:       []coverage.AcceptanceManifest{manifest},
	}
}

func stage35aContractWithNonQA() coverage.CoverageContract {
	c := stage35aContract()
	c.RequiredSources.Sources = append(c.RequiredSources.Sources, coverage.RequiredSource{SourceID: "SOL-A", Category: coverage.SolutionConstraint, Applicability: coverage.NonQAApplicability})
	c.AlternativeVerifications = []coverage.AlternativeVerification{{SourceID: "SOL-A", Reason: "no QA point applies", Method: "documented static check", Status: coverage.StatusPass, Evidence: "sha256:alternative-proof"}}
	return c
}

func stage35aPassReview(c coverage.CoverageContract, index int) coverage.ReviewResult {
	m := c.Manifests[index]
	review := coverage.ReviewResult{Binding: m.Binding, Scope: coverage.ScopeFull, SetStatus: coverage.StatusPass}
	for _, id := range m.Sources {
		review.SourceDecisions = append(review.SourceDecisions, coverage.ReviewDecision{ID: id, Status: coverage.StatusPass})
	}
	for _, point := range m.Points {
		review.PointDecisions = append(review.PointDecisions, coverage.ReviewDecision{ID: point.PointID, Status: coverage.StatusPass})
	}
	for _, qaCase := range m.Cases {
		review.CaseDecisions = append(review.CaseDecisions, coverage.ReviewDecision{ID: qaCase.CaseID, Status: coverage.StatusPass})
	}
	return review
}

func stage35aWhitelistAndFullReport(t *testing.T) (coverage.CoverageContract, coverage.ApprovedWhitelist, coverage.ExecutionReport) {
	t.Helper()
	c := stage35aContract()
	w, err := c.ValidateReviews([]coverage.ReviewResult{stage35aPassReview(c, 0)})
	if err != nil {
		t.Fatalf("project whitelist: %v", err)
	}
	digests, err := c.Digests()
	if err != nil {
		t.Fatalf("contract digests: %v", err)
	}
	whitelistDigest, err := w.WhitelistDigest()
	if err != nil {
		t.Fatalf("whitelist digest: %v", err)
	}
	report := coverage.ExecutionReport{Binding: coverage.ExecutionBinding{
		RequiredSourcesDigest: digests.RequiredSourcesDigest,
		ManifestDigest:        digests.ManifestDigest,
		MapDigest:             digests.MapDigest,
		WhitelistDigest:       whitelistDigest,
		Candidate:             coverage.ValidationCandidate{Identity: "candidate-current", Digest: "sha256:candidate"},
		Scope:                 coverage.ScopeFull,
		ExpectedCaseIDs:       []string{"CASE-A", "CASE-B"},
		ActualCaseIDs:         []string{"CASE-A", "CASE-B"},
	}}
	for _, entry := range w.Entries {
		report.Records = append(report.Records, coverage.ExecutionRecord{ReviewKind: entry.ReviewKind, SourceID: entry.SourceID, PointID: entry.PointID, CaseID: entry.CaseID, Result: coverage.ExecutedPass, Provenance: coverage.ProvenanceExecuted})
	}
	return c, w, report
}

func stage35aCode(t *testing.T, err error, want string) *coverage.ValidationError {
	t.Helper()
	if err == nil {
		t.Fatalf("expected validation error %s", want)
	}
	var validationErr *coverage.ValidationError
	if !errors.As(err, &validationErr) {
		t.Fatalf("error %v is not a ValidationError", err)
	}
	if validationErr.Code != want {
		t.Fatalf("error code = %s, want %s (%v)", validationErr.Code, want, err)
	}
	if validationErr.Path == "" || validationErr.Message == "" {
		t.Fatalf("validation error lacks stable location/message: %+v", validationErr)
	}
	return validationErr
}

// A confirmed source list is the sole inventory. Bindings, source IDs,
// categories, and applicability are all required and reject common malformed
// records before manifest traversal starts.
func TestStage35AWhiteboxRequiredSourcesRejectMalformedInventory(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*coverage.CoverageContract)
		code   string
	}{
		{name: "missing run binding", mutate: func(c *coverage.CoverageContract) { c.RequiredSources.Binding.RunID = "" }, code: coverage.CodeInvalidBinding},
		{name: "missing requirement revision", mutate: func(c *coverage.CoverageContract) { c.RequiredSources.Binding.RequirementRevision = "" }, code: coverage.CodeInvalidBinding},
		{name: "missing solution revision", mutate: func(c *coverage.CoverageContract) { c.RequiredSources.Binding.SolutionRevision = "" }, code: coverage.CodeInvalidBinding},
		{name: "empty source inventory", mutate: func(c *coverage.CoverageContract) { c.RequiredSources.Sources = nil }, code: coverage.CodeInvalidSource},
		{name: "empty source id", mutate: func(c *coverage.CoverageContract) { c.RequiredSources.Sources[0].SourceID = "" }, code: coverage.CodeInvalidSource},
		{name: "whitespace source id", mutate: func(c *coverage.CoverageContract) { c.RequiredSources.Sources[0].SourceID = "  " }, code: coverage.CodeInvalidSource},
		{name: "duplicate source id", mutate: func(c *coverage.CoverageContract) {
			c.RequiredSources.Sources = append(c.RequiredSources.Sources, c.RequiredSources.Sources[0])
		}, code: coverage.CodeDuplicateID},
		{name: "unknown category", mutate: func(c *coverage.CoverageContract) {
			c.RequiredSources.Sources[0].Category = coverage.SourceCategory("OTHER")
		}, code: coverage.CodeInvalidSource},
		{name: "unknown applicability", mutate: func(c *coverage.CoverageContract) {
			c.RequiredSources.Sources[0].Applicability = coverage.Applicability("OTHER")
		}, code: coverage.CodeInvalidSource},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := stage35aContract()
			tc.mutate(&c)
			stage35aCode(t, c.Validate(), tc.code)
		})
	}
	childBound := stage35aContract()
	childBound.RequiredSources.Binding.ChildID = "child-1"
	childBound.Manifests[0].Binding.ChildID = "child-1"
	if err := childBound.Validate(); err != nil {
		t.Fatalf("matching child binding rejected: %v", err)
	}
}

// Selected review kinds and manifest bindings form a one-to-one scoped
// structure: every selected kind needs exactly one matching manifest, and the
// source/run/revision/route/topology binding must remain intact.
func TestStage35AWhiteboxManifestSelectionAndScopeBinding(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*coverage.CoverageContract)
		code   string
	}{
		{name: "duplicate selected kind", mutate: func(c *coverage.CoverageContract) {
			c.SelectedKinds = []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewBlackbox}
		}, code: coverage.CodeDuplicateID},
		{name: "unknown selected kind", mutate: func(c *coverage.CoverageContract) {
			c.SelectedKinds = []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewKind("OTHER")}
		}, code: coverage.CodeInvalidBinding},
		{name: "selected kind has no manifest", mutate: func(c *coverage.CoverageContract) {
			c.SelectedKinds = []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox}
		}, code: coverage.CodeMissingCoverage},
		{name: "manifest uses unselected kind", mutate: func(c *coverage.CoverageContract) { c.SelectedKinds = []coverage.ReviewKind{coverage.ReviewWhitebox} }, code: coverage.CodeInvalidBinding},
		{name: "run binding drift", mutate: func(c *coverage.CoverageContract) { c.Manifests[0].Binding.RunID = "other-run" }, code: coverage.CodeInvalidBinding},
		{name: "child binding drift", mutate: func(c *coverage.CoverageContract) { c.Manifests[0].Binding.ChildID = "child-1" }, code: coverage.CodeInvalidBinding},
		{name: "source revision drift", mutate: func(c *coverage.CoverageContract) { c.Manifests[0].Binding.RequirementRevision = "req-r2" }, code: coverage.CodeInvalidBinding},
		{name: "solution revision drift", mutate: func(c *coverage.CoverageContract) { c.Manifests[0].Binding.SolutionRevision = "sol-r2" }, code: coverage.CodeInvalidBinding},
		{name: "empty route scope", mutate: func(c *coverage.CoverageContract) { c.Manifests[0].Binding.RouteScope = "" }, code: coverage.CodeInvalidBinding},
		{name: "empty topology scope", mutate: func(c *coverage.CoverageContract) { c.Manifests[0].Binding.TopologyScope = "" }, code: coverage.CodeInvalidBinding},
		{name: "edge review kind drift", mutate: func(c *coverage.CoverageContract) { c.Manifests[0].Edges[0].ReviewKind = coverage.ReviewWhitebox }, code: coverage.CodeInvalidBinding},
		{name: "duplicate manifest kind", mutate: func(c *coverage.CoverageContract) { c.Manifests = append(c.Manifests, c.Manifests[0]) }, code: coverage.CodeDuplicateID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := stage35aContract()
			tc.mutate(&c)
			stage35aCode(t, c.Validate(), tc.code)
		})
	}
}

// A source may be covered by more than one selected QA kind. Each manifest is
// independently bound and contributes its own points/cases to the aggregate.
func TestStage35AWhiteboxMultiKindManifestsCoverSharedSource(t *testing.T) {
	c := stage35aContract()
	c.SelectedKinds = []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox}
	whitebox := coverage.AcceptanceManifest{
		Binding: stage35aManifestBinding(coverage.ReviewWhitebox),
		Sources: []string{"REQ-A"},
		Points:  []coverage.AcceptancePoint{{PointID: "POINT-W", ObservableBehavior: "whitebox structure is checked", Oracle: "test reference is bound"}},
		Cases:   []coverage.AcceptanceCase{{CaseID: "CASE-W", Mode: coverage.CaseWhitebox, TestRef: "internal/engine/whitebox_qa/phase3_5a_coverage_contract_qa_test.go::TestStage35AWhiteboxMultiKindManifestsCoverSharedSource", Oracle: "PASS"}},
		Edges:   []coverage.CoverageEdge{{ReviewKind: coverage.ReviewWhitebox, SourceID: "REQ-A", PointID: "POINT-W", CaseID: "CASE-W"}},
	}
	c.Manifests = append(c.Manifests, whitebox)
	if err := c.Validate(); err != nil {
		t.Fatalf("valid multi-kind manifest set rejected: %v", err)
	}
	if got := c.Manifests[1].PointsForSource("REQ-A"); !reflect.DeepEqual(got, []string{"POINT-W"}) {
		t.Fatalf("whitebox source projection = %v", got)
	}

	reviews := []coverage.ReviewResult{stage35aPassReview(c, 0), stage35aPassReview(c, 1)}
	w, err := c.ValidateReviews(reviews)
	if err != nil {
		t.Fatalf("multi-kind reviews rejected: %v", err)
	}
	if len(w.Entries) != len(c.Manifests[0].Edges)+len(c.Manifests[1].Edges) {
		t.Fatalf("multi-kind whitelist entries = %d, want both manifest edge sets", len(w.Entries))
	}
}

// Every manifest source, point, and case must participate in at least one
// source↔point↔case edge; an otherwise well-formed orphan is still incomplete.
func TestStage35AWhiteboxManifestGraphRejectsOrphanObjects(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*coverage.CoverageContract)
		code   string
	}{
		{name: "source without edge", mutate: func(c *coverage.CoverageContract) {
			c.RequiredSources.Sources = append(c.RequiredSources.Sources, coverage.RequiredSource{SourceID: "REQ-B", Category: coverage.ProductRequirement, Applicability: coverage.QAApplicability})
			c.Manifests[0].Sources = append(c.Manifests[0].Sources, "REQ-B")
		}, code: coverage.CodeMissingCoverage},
		{name: "QA source absent from manifest", mutate: func(c *coverage.CoverageContract) {
			c.Manifests[0].Sources = nil
		}, code: coverage.CodeMissingCoverage},
		{name: "point without edge", mutate: func(c *coverage.CoverageContract) {
			c.Manifests[0].Points = append(c.Manifests[0].Points, coverage.AcceptancePoint{PointID: "POINT-ORPHAN", ObservableBehavior: "unused", Oracle: "must be rejected"})
		}, code: coverage.CodeMissingCoverage},
		{name: "case without edge", mutate: func(c *coverage.CoverageContract) {
			c.Manifests[0].Cases = append(c.Manifests[0].Cases, coverage.AcceptanceCase{CaseID: "CASE-ORPHAN", Mode: coverage.CaseBlackbox, PublicEntry: "formal-gates coverage validate", Preconditions: "input", Steps: "invoke", Oracle: "valid"})
		}, code: coverage.CodeMissingCoverage},
		{name: "unknown source", mutate: func(c *coverage.CoverageContract) { c.Manifests[0].Sources[0] = "REQ-UNKNOWN" }, code: coverage.CodeUnknownID},
		{name: "non-QA source in manifest", mutate: func(c *coverage.CoverageContract) {
			*c = stage35aContractWithNonQA()
			c.Manifests[0].Sources = append(c.Manifests[0].Sources, "SOL-A")
		}, code: coverage.CodeInvalidSource},
		{name: "duplicate source", mutate: func(c *coverage.CoverageContract) {
			c.Manifests[0].Sources = append(c.Manifests[0].Sources, c.Manifests[0].Sources[0])
		}, code: coverage.CodeDuplicateID},
		{name: "duplicate point", mutate: func(c *coverage.CoverageContract) {
			c.Manifests[0].Points = append(c.Manifests[0].Points, c.Manifests[0].Points[0])
		}, code: coverage.CodeDuplicateID},
		{name: "duplicate case", mutate: func(c *coverage.CoverageContract) {
			c.Manifests[0].Cases = append(c.Manifests[0].Cases, c.Manifests[0].Cases[0])
		}, code: coverage.CodeDuplicateID},
		{name: "duplicate edge", mutate: func(c *coverage.CoverageContract) {
			c.Manifests[0].Edges = append(c.Manifests[0].Edges, c.Manifests[0].Edges[0])
		}, code: coverage.CodeDuplicateID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := stage35aContract()
			tc.mutate(&c)
			stage35aCode(t, c.Validate(), tc.code)
		})
	}
}

// Reverse graph queries derive sorted, de-duplicated views from the single edge
// relation, and Map returns an isolated edge slice rather than caller-owned
// storage.
func TestStage35AWhiteboxCoverageQueriesDeriveStableReverseViews(t *testing.T) {
	manifest := coverage.AcceptanceManifest{Edges: []coverage.CoverageEdge{
		{SourceID: "SRC-2", PointID: "POINT-2", CaseID: "CASE-2"},
		{SourceID: "SRC-1", PointID: "POINT-1", CaseID: "CASE-2"},
		{SourceID: "SRC-1", PointID: "POINT-2", CaseID: "CASE-1"},
		{SourceID: "SRC-1", PointID: "POINT-2", CaseID: "CASE-2"},
		{SourceID: "SRC-1", PointID: "POINT-2", CaseID: "CASE-2"},
	}}
	if got, want := manifest.PointsForSource("SRC-1"), []string{"POINT-1", "POINT-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("PointsForSource = %v, want %v", got, want)
	}
	if got, want := manifest.CasesForPoint("POINT-2"), []string{"CASE-1", "CASE-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CasesForPoint = %v, want %v", got, want)
	}
	if got, want := manifest.SourcesForCase("CASE-2"), []string{"SRC-1", "SRC-2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("SourcesForCase = %v, want %v", got, want)
	}
	projected := manifest.Map()
	projected.Edges[0].SourceID = "MUTATED"
	if manifest.Edges[0].SourceID == "MUTATED" {
		t.Fatal("Map must return an independent edge slice")
	}
}

// A map projection must exactly match its manifest, including binding and
// edge identity; missing, unknown, duplicate, or wrong-kind edges are distinct
// structural failures.
func TestStage35AWhiteboxCoverageMapRejectsDriftAndDuplicates(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*coverage.CoverageMap)
		code   string
	}{
		{name: "binding drift", mutate: func(m *coverage.CoverageMap) { m.Binding.RouteScope = "other" }, code: coverage.CodeInvalidBinding},
		{name: "missing edge", mutate: func(m *coverage.CoverageMap) { m.Edges = m.Edges[:len(m.Edges)-1] }, code: coverage.CodeMissingCoverage},
		{name: "unknown edge", mutate: func(m *coverage.CoverageMap) {
			m.Edges[len(m.Edges)-1] = coverage.CoverageEdge{SourceID: "REQ-A", PointID: "POINT-A", CaseID: "CASE-UNKNOWN"}
		}, code: coverage.CodeUnknownID},
		{name: "duplicate edge", mutate: func(m *coverage.CoverageMap) { m.Edges[len(m.Edges)-1] = m.Edges[0] }, code: coverage.CodeDuplicateID},
		{name: "edge review kind drift", mutate: func(m *coverage.CoverageMap) {
			m.Edges[0].ReviewKind = coverage.ReviewWhitebox
		}, code: coverage.CodeInvalidBinding},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := stage35aContract()
			m := c.Manifests[0].Map()
			tc.mutate(&m)
			stage35aCode(t, coverage.ValidateCoverageMap(c.Manifests[0], m), tc.code)
		})
	}
}

// Case records are closed by review kind: blackbox/merge require the public
// entry shape, while whitebox requires one unique test reference and forbids
// public-entry fields.
func TestStage35AWhiteboxCaseBindingVariantsAreClosed(t *testing.T) {
	validKinds := []struct {
		kind coverage.ReviewKind
		mode coverage.CaseMode
	}{
		{coverage.ReviewBlackbox, coverage.CaseBlackbox},
		{coverage.ReviewWhitebox, coverage.CaseWhitebox},
		{coverage.ReviewMerge, coverage.CaseMerge},
	}
	for _, tc := range validKinds {
		c := stage35aContract()
		c.SelectedKinds = []coverage.ReviewKind{tc.kind}
		m := c.Manifests[0]
		m.Points = []coverage.AcceptancePoint{{PointID: "POINT-A", ObservableBehavior: "the contract is checked", Oracle: "validation returns no error"}}
		m.Binding = stage35aManifestBinding(tc.kind)
		m.Cases = []coverage.AcceptanceCase{{CaseID: "CASE-CLOSED", Mode: tc.mode, Oracle: "observable result"}}
		if tc.mode == coverage.CaseWhitebox {
			m.Cases[0].TestRef = "internal/engine/whitebox_qa/phase3_5a_coverage_contract_qa_test.go::TestStage35AWhiteboxCaseBindingVariantsAreClosed"
		} else {
			m.Cases[0].PublicEntry = "formal-gates coverage validate"
			m.Cases[0].Preconditions = "JSON fixture"
			m.Cases[0].Steps = "invoke entry"
		}
		c.Manifests = []coverage.AcceptanceManifest{m}
		c.Manifests[0].Edges = []coverage.CoverageEdge{{SourceID: "REQ-A", PointID: "POINT-A", CaseID: "CASE-CLOSED"}}
		if err := c.Validate(); err != nil {
			t.Fatalf("valid %s case rejected: %v", tc.kind, err)
		}
	}

	invalid := []struct {
		name   string
		mutate func(*coverage.AcceptanceCase)
	}{
		{name: "blackbox missing public entry", mutate: func(q *coverage.AcceptanceCase) { q.PublicEntry = "" }},
		{name: "blackbox whitespace public entry", mutate: func(q *coverage.AcceptanceCase) { q.PublicEntry = "  " }},
		{name: "blackbox missing preconditions", mutate: func(q *coverage.AcceptanceCase) { q.Preconditions = "" }},
		{name: "blackbox missing steps", mutate: func(q *coverage.AcceptanceCase) { q.Steps = "" }},
		{name: "blackbox carries test ref", mutate: func(q *coverage.AcceptanceCase) { q.TestRef = "unexpected" }},
		{name: "blackbox missing oracle", mutate: func(q *coverage.AcceptanceCase) { q.Oracle = "" }},
	}
	for _, tc := range invalid {
		t.Run(tc.name, func(t *testing.T) {
			c := stage35aContract()
			tc.mutate(&c.Manifests[0].Cases[0])
			stage35aCode(t, c.Validate(), coverage.CodeInvalidCaseBinding)
		})
	}
	for _, tc := range []struct {
		name   string
		mutate func(*coverage.AcceptanceCase)
	}{
		{name: "whitebox missing ref", mutate: func(q *coverage.AcceptanceCase) { q.TestRef = "" }},
		{name: "whitebox carries public fields", mutate: func(q *coverage.AcceptanceCase) { q.PublicEntry = "formal-gates coverage validate" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := stage35aContract()
			c.SelectedKinds = []coverage.ReviewKind{coverage.ReviewWhitebox}
			c.Manifests[0].Binding = stage35aManifestBinding(coverage.ReviewWhitebox)
			c.Manifests[0].Cases[0].Mode = coverage.CaseWhitebox
			c.Manifests[0].Cases[0].PublicEntry = ""
			c.Manifests[0].Cases[0].Preconditions = ""
			c.Manifests[0].Cases[0].Steps = ""
			c.Manifests[0].Cases[0].TestRef = "internal/engine/whitebox_qa/phase3_5a_coverage_contract_qa_test.go::TestStage35AWhiteboxCaseBindingVariantsAreClosed"
			tc.mutate(&c.Manifests[0].Cases[0])
			stage35aCode(t, c.Validate(), coverage.CodeInvalidCaseBinding)
		})
	}

	t.Run("duplicate whitebox test reference", func(t *testing.T) {
		c := stage35aContract()
		c.SelectedKinds = []coverage.ReviewKind{coverage.ReviewWhitebox}
		c.Manifests[0].Binding = stage35aManifestBinding(coverage.ReviewWhitebox)
		c.Manifests[0].Cases = []coverage.AcceptanceCase{
			{CaseID: "CASE-W1", Mode: coverage.CaseWhitebox, TestRef: "internal/engine/whitebox_qa/phase3_5a_coverage_contract_qa_test.go::TestStage35AWhiteboxCaseBindingVariantsAreClosed", Oracle: "PASS"},
			{CaseID: "CASE-W2", Mode: coverage.CaseWhitebox, TestRef: "internal/engine/whitebox_qa/phase3_5a_coverage_contract_qa_test.go::TestStage35AWhiteboxCaseBindingVariantsAreClosed", Oracle: "PASS"},
		}
		c.Manifests[0].Points = []coverage.AcceptancePoint{{PointID: "POINT-W", ObservableBehavior: "whitebox structure is checked", Oracle: "test reference is bound"}}
		c.Manifests[0].Edges = []coverage.CoverageEdge{{SourceID: "REQ-A", PointID: "POINT-W", CaseID: "CASE-W1"}, {SourceID: "REQ-A", PointID: "POINT-W", CaseID: "CASE-W2"}}
		stage35aCode(t, c.Validate(), coverage.CodeDuplicateID)
	})
}

// Every NON_QA source needs one documented reason, method, PASS result, and
// evidence record; explanations, unknown sources, duplicate records, and
// non-PASS statuses cannot close the source.
func TestStage35AWhiteboxAlternativeVerificationRequiresEvidenceClosure(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*coverage.CoverageContract)
		code   string
	}{
		{name: "missing verification", mutate: func(c *coverage.CoverageContract) { c.AlternativeVerifications = nil }, code: coverage.CodeInvalidAlternative},
		{name: "non-pass status", mutate: func(c *coverage.CoverageContract) { c.AlternativeVerifications[0].Status = coverage.StatusPending }, code: coverage.CodeInvalidAlternative},
		{name: "missing evidence", mutate: func(c *coverage.CoverageContract) { c.AlternativeVerifications[0].Evidence = "" }, code: coverage.CodeInvalidAlternative},
		{name: "unknown source", mutate: func(c *coverage.CoverageContract) { c.AlternativeVerifications[0].SourceID = "UNKNOWN" }, code: coverage.CodeInvalidAlternative},
		{name: "duplicate verification", mutate: func(c *coverage.CoverageContract) {
			c.AlternativeVerifications = append(c.AlternativeVerifications, c.AlternativeVerifications[0])
		}, code: coverage.CodeDuplicateID},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := stage35aContractWithNonQA()
			tc.mutate(&c)
			stage35aCode(t, c.Validate(), tc.code)
		})
	}
	if err := stage35aContractWithNonQA().Validate(); err != nil {
		t.Fatalf("complete non-QA verification rejected: %v", err)
	}
}

// Review projection is item-complete, binding-scoped, and PASS-only. A set
// status or a partially filled list cannot stand in for source/point/case
// decisions.
func TestStage35AWhiteboxReviewProjectionRequiresCompletePassDecisions(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*coverage.ReviewResult)
		code   string
	}{
		{name: "missing source decision", mutate: func(r *coverage.ReviewResult) { r.SourceDecisions = nil }, code: coverage.CodeIncompleteReview},
		{name: "unknown point decision", mutate: func(r *coverage.ReviewResult) { r.PointDecisions[0].ID = "UNKNOWN" }, code: coverage.CodeUnknownID},
		{name: "duplicate case decision", mutate: func(r *coverage.ReviewResult) { r.CaseDecisions[1].ID = r.CaseDecisions[0].ID }, code: coverage.CodeDuplicateID},
		{name: "failed point decision", mutate: func(r *coverage.ReviewResult) { r.PointDecisions[0].Status = coverage.StatusFail }, code: coverage.CodeReviewNotPass},
		{name: "set status not pass", mutate: func(r *coverage.ReviewResult) { r.SetStatus = coverage.StatusFail }, code: coverage.CodeReviewNotPass},
		{name: "unbound declaration", mutate: func(r *coverage.ReviewResult) { r.UnboundCases = []string{"CASE-A"} }, code: coverage.CodeIncompleteReview},
		{name: "invalid scope", mutate: func(r *coverage.ReviewResult) { r.Scope = coverage.ScopeMode("PARTIAL") }, code: coverage.CodeIncompleteReview},
		{name: "binding drift", mutate: func(r *coverage.ReviewResult) { r.Binding.RouteScope = "other" }, code: coverage.CodeInvalidBinding},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := stage35aContract()
			review := stage35aPassReview(c, 0)
			tc.mutate(&review)
			_, err := c.ValidateReviews([]coverage.ReviewResult{review})
			stage35aCode(t, err, tc.code)
		})
	}
	if _, err := cValidateReviewsForStage35A(t); err != nil {
		t.Fatalf("complete review projection rejected: %v", err)
	}
}

func cValidateReviewsForStage35A(t *testing.T) (coverage.ApprovedWhitelist, error) {
	t.Helper()
	c := stage35aContract()
	return c.ValidateReviews([]coverage.ReviewResult{stage35aPassReview(c, 0)})
}

// Required-source, manifest, and map digests are canonical projections: input
// ordering does not matter, the digest has sha256: encoding, and digesting
// never reorders caller-owned slices.
func TestStage35AWhiteboxCanonicalDigestsIgnoreInputOrder(t *testing.T) {
	c := stage35aContract()
	c.RequiredSources.Sources = append(c.RequiredSources.Sources, coverage.RequiredSource{SourceID: "REQ-B", Category: coverage.ProductRequirement, Applicability: coverage.QAApplicability})
	c.Manifests[0].Sources = append(c.Manifests[0].Sources, "REQ-B")
	c.Manifests[0].Edges = append(c.Manifests[0].Edges,
		coverage.CoverageEdge{SourceID: "REQ-B", PointID: "POINT-A", CaseID: "CASE-A"},
		coverage.CoverageEdge{SourceID: "REQ-B", PointID: "POINT-B", CaseID: "CASE-B"})
	c.SelectedKinds = []coverage.ReviewKind{coverage.ReviewBlackbox, coverage.ReviewWhitebox}
	c.Manifests = append(c.Manifests, coverage.AcceptanceManifest{
		Binding: stage35aManifestBinding(coverage.ReviewWhitebox),
		Sources: []string{"REQ-A"},
		Points:  []coverage.AcceptancePoint{{PointID: "POINT-W", ObservableBehavior: "whitebox structure is checked", Oracle: "test reference is bound"}},
		Cases:   []coverage.AcceptanceCase{{CaseID: "CASE-W", Mode: coverage.CaseWhitebox, TestRef: "internal/engine/whitebox_qa/phase3_5a_coverage_contract_qa_test.go::TestStage35AWhiteboxCanonicalDigestsIgnoreInputOrder", Oracle: "PASS"}},
		Edges:   []coverage.CoverageEdge{{ReviewKind: coverage.ReviewWhitebox, SourceID: "REQ-A", PointID: "POINT-W", CaseID: "CASE-W"}},
	})
	originalSources := append([]coverage.RequiredSource(nil), c.RequiredSources.Sources...)
	originalManifestSources := append([]string(nil), c.Manifests[0].Sources...)
	originalManifestPoints := append([]coverage.AcceptancePoint(nil), c.Manifests[0].Points...)
	originalManifestCases := append([]coverage.AcceptanceCase(nil), c.Manifests[0].Cases...)
	originalManifestEdges := append([]coverage.CoverageEdge(nil), c.Manifests[0].Edges...)
	first, err := c.Digests()
	if err != nil {
		t.Fatalf("initial digests: %v", err)
	}
	if !reflect.DeepEqual(c.RequiredSources.Sources, originalSources) || !reflect.DeepEqual(c.Manifests[0].Sources, originalManifestSources) || !reflect.DeepEqual(c.Manifests[0].Points, originalManifestPoints) || !reflect.DeepEqual(c.Manifests[0].Cases, originalManifestCases) || !reflect.DeepEqual(c.Manifests[0].Edges, originalManifestEdges) {
		t.Fatal("digest calculation reordered caller-owned slices")
	}
	sort.Slice(c.RequiredSources.Sources, func(i, j int) bool {
		return c.RequiredSources.Sources[i].SourceID > c.RequiredSources.Sources[j].SourceID
	})
	sort.Slice(c.Manifests[0].Sources, func(i, j int) bool { return c.Manifests[0].Sources[i] > c.Manifests[0].Sources[j] })
	sort.Slice(c.Manifests[0].Points, func(i, j int) bool { return c.Manifests[0].Points[i].PointID > c.Manifests[0].Points[j].PointID })
	sort.Slice(c.Manifests[0].Cases, func(i, j int) bool { return c.Manifests[0].Cases[i].CaseID > c.Manifests[0].Cases[j].CaseID })
	sort.Slice(c.Manifests[0].Edges, func(i, j int) bool {
		return stage35aEdgeKey(c.Manifests[0].Edges[i]) > stage35aEdgeKey(c.Manifests[0].Edges[j])
	})
	sort.Slice(c.Manifests, func(i, j int) bool { return c.Manifests[i].Binding.ReviewKind > c.Manifests[j].Binding.ReviewKind })
	second, err := c.Digests()
	if err != nil {
		t.Fatalf("reordered digests: %v", err)
	}
	if first != second {
		t.Fatalf("canonical digest changed after reordering: first=%+v second=%+v", first, second)
	}
	if !strings.HasPrefix(first.RequiredSourcesDigest, "sha256:") || !strings.HasPrefix(first.ManifestDigest, "sha256:") || !strings.HasPrefix(first.MapDigest, "sha256:") {
		t.Fatalf("unexpected digest encoding: %+v", first)
	}
}

// Test-only edge key helper avoids depending on unexported implementation
// details while preserving a deterministic order for the permutation above.
func stage35aEdgeKey(e coverage.CoverageEdge) string {
	return e.SourceID + "\x00" + e.PointID + "\x00" + e.CaseID
}

// FULL execution must account for every approved edge exactly once and cannot
// smuggle inherited/not-executed records or an auxiliary NotExecuted list into
// a full PASS.
func TestStage35AWhiteboxFullExecutionRequiresExactExecutedPartition(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*coverage.ExecutionReport)
		code   string
	}{
		{name: "missing edge record", mutate: func(r *coverage.ExecutionReport) { r.Records = r.Records[:len(r.Records)-1] }, code: coverage.CodeExecutionSetMismatch},
		{name: "unknown execution edge", mutate: func(r *coverage.ExecutionReport) { r.Records[0].ReviewKind = coverage.ReviewWhitebox }, code: coverage.CodeUnknownID},
		{name: "executed result is not pass", mutate: func(r *coverage.ExecutionReport) { r.Records[0].Result = coverage.ExecutedFail }, code: coverage.CodeExecutionNotPass},
		{name: "executed record carries inheritance evidence", mutate: func(r *coverage.ExecutionReport) { r.Records[0].InheritedFromCandidate = "candidate-old" }, code: coverage.CodeExecutionSetMismatch},
		{name: "inherited provenance", mutate: func(r *coverage.ExecutionReport) {
			r.Records[0].Provenance = coverage.ProvenanceInherited
			r.Records[0].InheritedFromCandidate = "candidate-old"
			r.Records[0].ReuseEvidence = "receipt"
		}, code: coverage.CodeExecutionSetMismatch},
		{name: "full not-executed list", mutate: func(r *coverage.ExecutionReport) {
			r.NotExecuted = []coverage.CoverageEdge{{SourceID: "REQ-A", PointID: "POINT-A", CaseID: "CASE-A"}}
		}, code: coverage.CodeExecutionSetMismatch},
		{name: "duplicate edge record", mutate: func(r *coverage.ExecutionReport) { r.Records = append(r.Records, r.Records[0]) }, code: coverage.CodeDuplicateID},
		{name: "duplicate expected case id", mutate: func(r *coverage.ExecutionReport) { r.Binding.ExpectedCaseIDs = []string{"CASE-A", "CASE-A"} }, code: coverage.CodeExecutionSetMismatch},
		{name: "expected case drift", mutate: func(r *coverage.ExecutionReport) { r.Binding.ExpectedCaseIDs = []string{"CASE-A"} }, code: coverage.CodeExecutionSetMismatch},
		{name: "actual case drift", mutate: func(r *coverage.ExecutionReport) { r.Binding.ActualCaseIDs = []string{"CASE-A"} }, code: coverage.CodeExecutionSetMismatch},
		{name: "unknown execution scope", mutate: func(r *coverage.ExecutionReport) { r.Binding.Scope = coverage.ScopeMode("PARTIAL") }, code: coverage.CodeExecutionSetMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, w, report := stage35aWhitelistAndFullReport(t)
			tc.mutate(&report)
			stage35aCode(t, c.ValidateExecution(w, report), tc.code)
		})
	}
	if c, w, report := stage35aWhitelistAndFullReport(t); c.ValidateExecution(w, report) != nil {
		t.Fatal("complete FULL execution should validate")
	}
}

// AFFECTED execution permits executed and inherited PASS records only when
// inheritance cites a distinct candidate and evidence; NOT_EXECUTED records
// must be explicitly partitioned and never count as PASS.
func TestStage35AWhiteboxAffectedExecutionRequiresInheritanceEvidence(t *testing.T) {
	c, w, report := stage35aWhitelistAndFullReport(t)
	report.Binding.Scope = coverage.ScopeAffected
	for i := range report.Records {
		if i == 0 {
			continue
		}
		report.Records[i].Provenance = coverage.ProvenanceInherited
		report.Records[i].InheritedFromCandidate = "candidate-previous"
		report.Records[i].ReuseEvidence = "view-receipt"
	}
	if err := c.ValidateExecution(w, report); err != nil {
		t.Fatalf("valid affected execution rejected: %v", err)
	}
	cases := []struct {
		name   string
		mutate func(*coverage.ExecutionReport)
		code   string
	}{
		{name: "same candidate inheritance", mutate: func(r *coverage.ExecutionReport) { r.Records[1].InheritedFromCandidate = r.Binding.Candidate.Identity }, code: coverage.CodeExecutionNotPass},
		{name: "missing source candidate", mutate: func(r *coverage.ExecutionReport) { r.Records[1].InheritedFromCandidate = "" }, code: coverage.CodeExecutionNotPass},
		{name: "missing reuse evidence", mutate: func(r *coverage.ExecutionReport) { r.Records[1].ReuseEvidence = "" }, code: coverage.CodeExecutionNotPass},
		{name: "authorized skip is not pass", mutate: func(r *coverage.ExecutionReport) { r.Records[1].Result = coverage.AuthorizedSkip }, code: coverage.CodeExecutionNotPass},
		{name: "not-executed record claims pass", mutate: func(r *coverage.ExecutionReport) {
			r.Records[1].Provenance = coverage.ProvenanceNotExecuted
			r.Records[1].Result = coverage.ExecutedPass
			r.NotExecuted = []coverage.CoverageEdge{{ReviewKind: r.Records[1].ReviewKind, SourceID: r.Records[1].SourceID, PointID: r.Records[1].PointID, CaseID: r.Records[1].CaseID}}
		}, code: coverage.CodeExecutionNotPass},
		{name: "unknown provenance", mutate: func(r *coverage.ExecutionReport) { r.Records[1].Provenance = coverage.Provenance("UNKNOWN") }, code: coverage.CodeExecutionSetMismatch},
		{name: "not-executed collection missing", mutate: func(r *coverage.ExecutionReport) {
			r.Records[1].Provenance = coverage.ProvenanceNotExecuted
			r.Records[1].Result = coverage.ExecutedUnknown
		}, code: coverage.CodeExecutionSetMismatch},
		{name: "not-executed collection has extra edge", mutate: func(r *coverage.ExecutionReport) {
			r.NotExecuted = append(r.NotExecuted, coverage.CoverageEdge{SourceID: "REQ-A", PointID: "POINT-B", CaseID: "CASE-B"})
		}, code: coverage.CodeExecutionSetMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, baseline := stage35aWhitelistAndFullReport(t)
			baseline.Binding.Scope = coverage.ScopeAffected
			for i := range baseline.Records {
				if i != 0 {
					baseline.Records[i].Provenance = coverage.ProvenanceInherited
					baseline.Records[i].InheritedFromCandidate = "candidate-previous"
					baseline.Records[i].ReuseEvidence = "view-receipt"
				}
			}
			c2, w2, _ := stage35aWhitelistAndFullReport(t)
			tc.mutate(&baseline)
			stage35aCode(t, c2.ValidateExecution(w2, baseline), tc.code)
		})
	}
}

// Execution is bound to all contract/whitelist digests and, when supplied,
// the current candidate identity and digest. Any stale binding invalidates the
// report before edge reconciliation.
func TestStage35AWhiteboxExecutionRejectsStaleDigestOrCandidateBinding(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*coverage.CoverageContract, *coverage.ExecutionReport)
		code   string
	}{
		{name: "required digest drift", mutate: func(_ *coverage.CoverageContract, r *coverage.ExecutionReport) {
			r.Binding.RequiredSourcesDigest = "sha256:stale"
		}, code: coverage.CodeDigestMismatch},
		{name: "manifest digest drift", mutate: func(_ *coverage.CoverageContract, r *coverage.ExecutionReport) {
			r.Binding.ManifestDigest = "sha256:stale"
		}, code: coverage.CodeDigestMismatch},
		{name: "map digest drift", mutate: func(_ *coverage.CoverageContract, r *coverage.ExecutionReport) { r.Binding.MapDigest = "sha256:stale" }, code: coverage.CodeDigestMismatch},
		{name: "whitelist digest drift", mutate: func(_ *coverage.CoverageContract, r *coverage.ExecutionReport) {
			r.Binding.WhitelistDigest = "sha256:stale"
		}, code: coverage.CodeDigestMismatch},
		{name: "required source projection changes old whitelist", mutate: func(c *coverage.CoverageContract, _ *coverage.ExecutionReport) {
			c.RequiredSources.Sources[0].Category = coverage.SolutionConstraint
		}, code: coverage.CodeDigestMismatch},
		{name: "route scope changes old whitelist", mutate: func(c *coverage.CoverageContract, _ *coverage.ExecutionReport) {
			c.Manifests[0].Binding.RouteScope = "new-route"
		}, code: coverage.CodeDigestMismatch},
		{name: "topology scope changes old whitelist", mutate: func(c *coverage.CoverageContract, _ *coverage.ExecutionReport) {
			c.Manifests[0].Binding.TopologyScope = "new-topology"
		}, code: coverage.CodeDigestMismatch},
		{name: "candidate identity drift", mutate: func(_ *coverage.CoverageContract, r *coverage.ExecutionReport) {
			r.Binding.Candidate.Identity = "candidate-old"
		}, code: coverage.CodeCandidateMismatch},
		{name: "candidate digest drift", mutate: func(_ *coverage.CoverageContract, r *coverage.ExecutionReport) {
			r.Binding.Candidate.Digest = "sha256:other"
		}, code: coverage.CodeCandidateMismatch},
		{name: "candidate identity required", mutate: func(_ *coverage.CoverageContract, r *coverage.ExecutionReport) { r.Binding.Candidate.Identity = "" }, code: coverage.CodeCandidateMismatch},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, w, report := stage35aWhitelistAndFullReport(t)
			current := report.Binding.Candidate
			c.Candidate = &current
			tc.mutate(&c, &report)
			stage35aCode(t, c.ValidateExecution(w, report), tc.code)
		})
	}
}

// Projection digests cover all approved edges and alternative-verification
// records. Mutating either projection without recomputing its digest is
// detectable and cannot be accepted as a current whitelist.
func TestStage35AWhiteboxWhitelistProjectionDigestDetectsMutation(t *testing.T) {
	c := stage35aContractWithNonQA()
	w, err := c.ValidateReviews([]coverage.ReviewResult{stage35aPassReview(c, 0)})
	if err != nil {
		t.Fatalf("project whitelist: %v", err)
	}
	if got, err := w.WhitelistDigest(); err != nil || got != w.Digest {
		t.Fatalf("projected whitelist digest = %q, err=%v, stored=%q", got, err, w.Digest)
	}
	w.AlternativeVerifications[0].Evidence = "sha256:changed"
	if _, err := w.WhitelistDigest(); err == nil {
		t.Fatal("mutated alternative verification retained whitelist digest")
	}
	w, err = c.ValidateReviews([]coverage.ReviewResult{stage35aPassReview(c, 0)})
	if err != nil {
		t.Fatal(err)
	}
	w.Entries[0].CaseID = "CASE-UNKNOWN"
	if _, err := w.WhitelistDigest(); err == nil {
		t.Fatal("mutated whitelist edge retained whitelist digest")
	}
}

// Package-level helpers and value methods are thin adapters over the same
// validated pure logic and must agree on successful projections/execution.
func TestStage35AWhiteboxExportedCoverageAdaptersDelegateConsistently(t *testing.T) {
	c := stage35aContract()
	reviews := []coverage.ReviewResult{stage35aPassReview(c, 0)}
	if err := coverage.Validate(c); err != nil {
		t.Fatalf("coverage.Validate: %v", err)
	}
	if err := coverage.ValidateManifest(c.RequiredSources, c.Manifests[0]); err != nil {
		t.Fatalf("coverage.ValidateManifest: %v", err)
	}
	if err := c.Manifests[0].Validate(c.RequiredSources); err != nil {
		t.Fatalf("manifest.Validate: %v", err)
	}
	if _, err := c.RequiredSources.Digest(); err != nil {
		t.Fatalf("required source Digest: %v", err)
	}
	if _, err := c.Manifests[0].Digest(c.RequiredSources); err != nil {
		t.Fatalf("manifest Digest: %v", err)
	}
	if _, err := c.Manifests[0].MapDigest(); err != nil {
		t.Fatalf("manifest MapDigest: %v", err)
	}
	if _, err := c.Manifests[0].Map().MapDigest(); err != nil {
		t.Fatalf("map MapDigest: %v", err)
	}
	want, err := c.ValidateReviews(reviews)
	if err != nil {
		t.Fatalf("method ValidateReviews: %v", err)
	}
	gotProject, err := coverage.ProjectWhitelist(c, reviews)
	if err != nil {
		t.Fatalf("ProjectWhitelist: %v", err)
	}
	gotValidate, err := coverage.ValidateReviews(c, reviews)
	if err != nil {
		t.Fatalf("package ValidateReviews: %v", err)
	}
	if !reflect.DeepEqual(want, gotProject) || !reflect.DeepEqual(want, gotValidate) {
		t.Fatalf("whitelist adapters diverged: method=%+v project=%+v package=%+v", want, gotProject, gotValidate)
	}
	_, w, report := stage35aWhitelistAndFullReport(t)
	if err := coverage.ValidateExecution(c, w, report); err != nil {
		t.Fatalf("package ValidateExecution: %v", err)
	}
	if err := coverage.ValidateExecutionForCandidate(c, w, report, report.Binding.Candidate); err != nil {
		t.Fatalf("ValidateExecutionForCandidate: %v", err)
	}
	if _, err := coverage.CanonicalJSON(struct {
		Value string `json:"value"`
	}{"ok"}); err != nil {
		t.Fatalf("CanonicalJSON: %v", err)
	}
	if digest, err := coverage.CanonicalDigest(struct {
		Value string `json:"value"`
	}{"ok"}); err != nil || !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("CanonicalDigest = %q, err=%v", digest, err)
	}
}
