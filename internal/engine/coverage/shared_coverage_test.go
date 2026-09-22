package coverage

import "testing"

func independentContract() (CoverageContract, ReviewResult) {
	binding := ManifestBinding{SourceBinding: SourceBinding{RunID: "independent-qa", RequirementRevision: "requirement-1", SolutionRevision: "solution-1"}, ReviewKind: ReviewWhitebox, RouteScope: "full", TopologyScope: "single"}
	m := AcceptanceManifest{Binding: binding, Sources: []string{"REQ"}, SharedSteps: []SharedStep{{ID: "prepare", Procedure: "create clean independent fixtures"}},
		Points: []AcceptancePoint{{PointID: "AC-A", ObservableBehavior: "first value", Oracle: "first equals 1"}, {PointID: "AC-B", ObservableBehavior: "second value", Oracle: "second equals 2"}},
		Cases:  []AcceptanceCase{{CaseID: "A", Mode: CaseWhitebox, TestRef: "file::first", Oracle: "first equals 1", SharedSteps: []string{"prepare"}}, {CaseID: "B", Mode: CaseWhitebox, TestRef: "file::second", Oracle: "second equals 2", SharedSteps: []string{"prepare"}}},
		Edges:  []CoverageEdge{{SourceID: "REQ", PointID: "AC-A", CaseID: "A"}, {SourceID: "REQ", PointID: "AC-B", CaseID: "B"}}}
	c := CoverageContract{RequiredSources: RequiredSources{Binding: binding.SourceBinding, Sources: []RequiredSource{{SourceID: "REQ", Category: ProductRequirement, Applicability: QAApplicability}}}, SelectedKinds: []ReviewKind{ReviewWhitebox}, Manifests: []AcceptanceManifest{m}, Candidate: &ValidationCandidate{Identity: "candidate-final"}}
	r := ReviewResult{Binding: binding, Scope: ScopeFull, SetStatus: StatusPass, SourceDecisions: []ReviewDecision{{ID: "REQ", Status: StatusPass}}, SharedStepDecisions: []ReviewDecision{{ID: "prepare", Status: StatusPass}}, PointDecisions: []ReviewDecision{{ID: "AC-A", Status: StatusPass}, {ID: "AC-B", Status: StatusPass}}, CaseDecisions: []ReviewDecision{{ID: "A", Status: StatusPass}, {ID: "B", Status: StatusPass}}}
	return c, r
}

func TestEngineSharedCoverage(t *testing.T) {
	c, r := independentContract()
	w, err := c.ValidateReviews([]ReviewResult{r})
	if err != nil || len(w.Entries) != 2 {
		t.Fatalf("one common decision plus both case decisions rejected: %+v %v", w, err)
	}
	for name, mutate := range map[string]func(*CoverageContract, *ReviewResult){
		"missing shared review":                 func(_ *CoverageContract, r *ReviewResult) { r.SharedStepDecisions = nil },
		"failed case with approved common step": func(_ *CoverageContract, r *ReviewResult) { r.CaseDecisions[1].Status = StatusFail },
		"missing per-case review":               func(_ *CoverageContract, r *ReviewResult) { r.CaseDecisions = r.CaseDecisions[:1] },
		"unknown shared reference":              func(c *CoverageContract, _ *ReviewResult) { c.Manifests[0].Cases[0].SharedSteps = []string{"missing"} },
		"missing coverage edge":                 func(c *CoverageContract, _ *ReviewResult) { c.Manifests[0].Edges = c.Manifests[0].Edges[:1] },
		"duplicate whitebox binding": func(c *CoverageContract, _ *ReviewResult) {
			c.Manifests[0].Cases[1].TestRef = c.Manifests[0].Cases[0].TestRef
		},
		"empty whitebox binding": func(c *CoverageContract, _ *ReviewResult) { c.Manifests[0].Cases[1].TestRef = "" },
	} {
		t.Run(name, func(t *testing.T) {
			cc, rr := independentContract()
			mutate(&cc, &rr)
			if _, err := cc.ValidateReviews([]ReviewResult{rr}); err == nil {
				t.Fatal("invalid structure or incomplete approval accepted")
			}
		})
	}
	before, err := c.ManifestDigest()
	if err != nil {
		t.Fatal(err)
	}
	c.Manifests[0].SharedSteps[0].Procedure = "changed prerequisite"
	after, err := c.ManifestDigest()
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatal("shared executable content absent from manifest identity")
	}
}

func TestEngineExecutionPartition(t *testing.T) {
	fixture := func() (CoverageContract, ApprovedWhitelist, ExecutionReport) {
		c, r := independentContract()
		w, err := c.ValidateReviews([]ReviewResult{r})
		if err != nil {
			t.Fatal(err)
		}
		report := ExecutionReport{Binding: ExecutionBinding{RequiredSourcesDigest: w.RequiredSourcesDigest, ManifestDigest: w.ManifestDigest, MapDigest: w.MapDigest, WhitelistDigest: w.Digest, Candidate: *c.Candidate, Scope: ScopeFull, ExpectedCaseIDs: []string{"A", "B"}, ActualCaseIDs: []string{"A", "B"}}, Records: []ExecutionRecord{{ReviewKind: ReviewWhitebox, SourceID: "REQ", PointID: "AC-A", CaseID: "A", Result: ExecutedPass, Provenance: ProvenanceExecuted}, {ReviewKind: ReviewWhitebox, SourceID: "REQ", PointID: "AC-B", CaseID: "B", Result: ExecutedPass, Provenance: ProvenanceExecuted}}}
		return c, w, report
	}
	c, w, r := fixture()
	if err := c.ValidateExecution(w, r); err != nil {
		t.Fatal(err)
	}
	for name, mutate := range map[string]func(*CoverageContract, *ExecutionReport){
		"new candidate": func(c *CoverageContract, _ *ExecutionReport) {
			c.Candidate = &ValidationCandidate{Identity: "candidate-next"}
		},
		"missing row":     func(_ *CoverageContract, r *ExecutionReport) { r.Records = r.Records[:1] },
		"duplicate row":   func(_ *CoverageContract, r *ExecutionReport) { r.Records = append(r.Records, r.Records[0]) },
		"unknown row":     func(_ *CoverageContract, r *ExecutionReport) { r.Records[0].CaseID = "unknown" },
		"fail row":        func(_ *CoverageContract, r *ExecutionReport) { r.Records[0].Result = ExecutedFail },
		"pending row":     func(_ *CoverageContract, r *ExecutionReport) { r.Records[0].Result = ExecutedPending },
		"authorized skip": func(_ *CoverageContract, r *ExecutionReport) { r.Records[0].Result = AuthorizedSkip },
		"inherited on FULL": func(_ *CoverageContract, r *ExecutionReport) {
			r.Records[0].Provenance = ProvenanceInherited
			r.Records[0].InheritedFromCandidate = "old"
			r.Records[0].ReuseEvidence = "reviewed unchanged behavior"
		},
	} {
		t.Run(name, func(t *testing.T) {
			cc, ww, rr := fixture()
			mutate(&cc, &rr)
			if err := cc.ValidateExecution(ww, rr); err == nil {
				t.Fatal("invalid execution counted as complete PASS")
			}
		})
	}
	r.Binding.Scope = ScopeAffected
	r.Records[0].Provenance = ProvenanceInherited
	r.Records[0].InheritedFromCandidate = "candidate-prior"
	r.Records[0].ReuseEvidence = "recorded impact assessment: A unchanged"
	if err := c.ValidateExecution(w, r); err != nil {
		t.Fatal(err)
	}
	r.Records[0].ReuseEvidence = ""
	if err := c.ValidateExecution(w, r); err == nil {
		t.Fatal("AFFECTED inheritance without evidence accepted")
	}
}
