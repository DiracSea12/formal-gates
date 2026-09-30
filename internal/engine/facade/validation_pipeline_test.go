package facade

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"reflect"
	"slices"
	"testing"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/review"
)

const validationDocument = "# Delivery\n\n## 需求点\n\n### REQ-001：命令\n\n#### 要求\n命令验证输入。\n\n#### 验收条件\n- AC-001：正常输入成功，错误输入拒绝。\n\n#### 来源\n用户要求。\n"

type validationDriver struct {
	t        *testing.T
	f        *Facade
	run      Run
	seq      int
	revision string
}

func (d *validationDriver) eventID() protocol.EventID {
	d.seq++
	return protocol.EventID(fmt.Sprintf("event-%d", d.seq))
}
func (d *validationDriver) submit(e protocol.Event) protocol.Acceptance {
	d.t.Helper()
	if e.ID == "" {
		e.ID = d.eventID()
	}
	a, r, err := d.f.Submit(e)
	if err != nil {
		d.t.Fatalf("submit %s: %v", e.Kind, err)
	}
	d.run = r
	return a
}
func (d *validationDriver) drive() {
	d.t.Helper()
	r, err := d.f.Drive()
	if err != nil {
		d.t.Fatal(err)
	}
	d.run = r
}
func (d *validationDriver) receipt(id string) {
	d.t.Helper()
	e, err := protocol.NewSpawnReceiptEvent(d.eventID(), id, DefaultProvider, id, protocol.SpawnStatusSpawned)
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(e)
}
func (d *validationDriver) business(id string, p protocol.ValidationResult) {
	d.t.Helper()
	d.receipt(id)
	d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: string(d.eventID()), Validation: &p}})
}
func (d *validationDriver) control(kind protocol.ControlKind, choice string, p protocol.ReviewDecisionPayload) {
	d.t.Helper()
	id := d.eventID()
	var e protocol.Event
	var err error
	if kind == protocol.ControlSelectRoute && p.Route != nil {
		e, err = protocol.NewRouteRequestEvent(id, p.Route.Gates, protocol.AskOption{ID: protocol.AskOptionID(choice), Label: choice})
	} else {
		e, err = protocol.NewRequestEvent(id, kind, protocol.AskOption{ID: protocol.AskOptionID(choice), Label: choice})
	}
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(e)
	var token string
	for _, a := range d.run.PendingAsks {
		if a.RequestID == string(id) {
			token = a.Token
		}
	}
	e, err = protocol.NewReviewDecideEvent(d.eventID(), protocol.RequestID(id), token, protocol.AskOptionID(choice), p)
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(e)
}
func startValidationDriver(t *testing.T, inventory *protocol.CoverageInventory) *validationDriver {
	t.Helper()
	sum := sha256.Sum256([]byte(validationDocument))
	revision := fmt.Sprintf("sha256:%x", sum)
	receipt := testReceipt()
	receipt.Coverage = inventory
	receipt.RequirementRevision = revision
	receipt.Artifacts[0].Revision = revision
	f, r, err := Start(StartOptions{Capacity: 16, Root: t.TempDir(), Request: StartRequest{RunID: "validation", Route: "regular", BaseSnapshot: "native-run-base", DefinitionSource: DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: receipt}, Admission: &Admission{PackageDigest: "sha256:package", InstalledTargetIdentity: "candidate"}})
	if err != nil {
		t.Fatal(err)
	}
	d := &validationDriver{t: t, f: f, run: r, revision: revision}
	d.drive()
	return d
}
func newValidationDriver(t *testing.T, gates ...string) *validationDriver {
	return configuredValidationDriver(t, nil, gates...)
}
func configuredValidationDriver(t *testing.T, inventory *protocol.CoverageInventory, gates ...string) *validationDriver {
	return plannedValidationDriver(t, inventory, commandPlan(), gates...)
}

// routeDecisionForGates builds the route fixture from the obligation gates a
// test asks for: the candidate list always carries both built-in QA modes, and
// a list that selects both keeps the spec-complete full route while any other
// combination pins those exact obligations as a custom selection.
func routeDecisionForGates(gates []string) protocol.RouteDecision {
	candidates := []string{"blackbox", "whitebox"}
	for _, gate := range gates {
		if !slices.Contains(candidates, gate) {
			candidates = append(candidates, gate)
		}
	}
	route := protocol.RouteDecision{Mode: "custom", Gates: candidates, Selected: gates}
	if slices.Contains(gates, "blackbox") && slices.Contains(gates, "whitebox") && slices.Equal(gates, candidates) {
		route = protocol.RouteDecision{Mode: "full", Gates: candidates}
	}
	return route
}
func plannedValidationDriver(t *testing.T, inventory *protocol.CoverageInventory, plan *protocol.GranularityReview, gates ...string) *validationDriver {
	t.Helper()
	d := startValidationDriver(t, inventory)
	revision := d.revision
	for _, action := range []review.Action{review.ProductReview, review.StartReadiness} {
		id := ""
		for key, pending := range d.run.PendingActions {
			if pending.ReviewSeries == action {
				id = key
			}
		}
		if id == "" {
			t.Fatalf("missing %s dispatch: %+v", action, d.run)
		}
		d.receipt(id)
		e, err := protocol.NewReviewWorkerResultEvent(d.eventID(), id, DefaultProvider, string(d.eventID()), protocol.ReviewResultEvidence{GranularityReview: func() *protocol.GranularityReview {
			if action == review.StartReadiness {
				return plan
			}
			return nil
		}(), Action: action, DispatchID: id, RequirementRevision: revision, Status: "PASS", Findings: []review.Finding{}, Verification: review.OperatorVerification{Checks: []string{"binding", "completeness", "evidence", "locations", "normal-entry", "requirement-match", "scope", "severity"}, Evidence: "checked the exact submitted requirement and all listed acceptance conditions"}})
		if err != nil {
			t.Fatal(err)
		}
		d.submit(e)
		d.drive()
	}
	d.control(protocol.ControlConfirmTopology, "no-split", protocol.ReviewDecisionPayload{Topology: &protocol.TopologyDecision{Decision: "no-split", Note: "one cohesive command"}})
	d.confirmRoute(routeDecisionForGates(gates))
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: validationDocument}}})
	d.drive()
	return d
}
func (d *validationDriver) action(handler string) (string, protocol.ValidationInput) {
	d.t.Helper()
	for id, input := range d.run.ValidationInputs {
		if input.Handler == handler {
			return id, input
		}
	}
	d.t.Fatalf("missing %s; inputs=%+v, next=%+v, validation=%+v", handler, d.run.ValidationInputs, d.run.Next, d.run.Validation)
	return "", protocol.ValidationInput{}
}
func (d *validationDriver) materialize() {
	d.t.Helper()
	for id, intent := range d.run.AdapterIntents {
		if intent.Adapter == nil || intent.Adapter.Operation != "op.candidate" {
			continue
		}
		assertCandidateStep(d.t, d, intent)
		data, _ := json.Marshal(intent.Adapter.Params)
		var p protocol.CandidateParameters
		if err := json.Unmarshal(data, &p); err != nil {
			d.t.Fatal(err)
		}
		identity := fmt.Sprintf("native-%d", d.seq)
		evidence := protocol.CandidateEvidence{ParametersDigest: intent.PayloadDigest, Identity: identity, Digest: "sha256:" + identity, Paths: p.Paths, TestOnly: true, ProductionViewEqual: true}
		evidence.GateView = gateReviewView(p)
		data, _ = json.Marshal(evidence)
		fields := map[string]any{}
		_ = json.Unmarshal(data, &fields)
		e, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), id, "op.candidate", DefaultProvider, intent.Correlation, intent.PayloadDigest, "EXECUTED", "", fields)
		if err != nil {
			d.t.Fatal(err)
		}
		d.submit(e)
		d.drive()
		snapshot, err := d.f.engine.Load()
		if err != nil {
			d.t.Fatal(err)
		}
		if snapshot.State.HostActionReceipts[id].Step != intent.Step {
			d.t.Fatal("candidate completion lost its original compiled step")
		}
		return
	}
	d.t.Fatalf("missing candidate intent: %+v", d.run)
}
func simpleManifest(input protocol.ValidationInput) coverage.AcceptanceManifest {
	m := input.Manifest
	m.Cases = []coverage.AcceptanceCase{{CaseID: "normal", Mode: coverage.CaseBlackbox, Scenario: "SUCCESS", PublicEntry: "app run", Preconditions: "clean project", Steps: "run with valid input", Oracle: "exits zero"}, {CaseID: "invalid", Mode: coverage.CaseBlackbox, Scenario: "FAILURE", PublicEntry: "app run", Preconditions: "clean project", Steps: "run with invalid input", Oracle: "rejects invalid input"}}
	for _, c := range m.Cases {
		m.Edges = append(m.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: c.CaseID})
	}
	return m
}
func approvedReview(m coverage.AcceptanceManifest) coverage.ReviewResult {
	r := coverage.ReviewResult{Binding: m.Binding, Scope: coverage.ScopeFull, SetStatus: coverage.StatusPass}
	for _, s := range m.Sources {
		r.SourceDecisions = append(r.SourceDecisions, coverage.ReviewDecision{ID: s, Status: coverage.StatusPass})
	}
	for _, p := range m.Points {
		r.PointDecisions = append(r.PointDecisions, coverage.ReviewDecision{ID: p.PointID, Status: coverage.StatusPass})
	}
	for _, c := range m.Cases {
		r.CaseDecisions = append(r.CaseDecisions, coverage.ReviewDecision{ID: c.CaseID, Status: coverage.StatusPass, Reason: fmt.Sprintf("Public entry and parameters, setup order and environment support %s; procedure %v records its evidence; oracle %v directly verifies the declared case scope.", c.CaseID, c.Steps, c.Oracle)})
	}
	return r
}
func (d *validationDriver) reviewReady() {
	d.t.Helper()
	_, input := d.action("engine.qa.design")
	d.reviewReadyWithManifest(simpleManifest(input))
}
func (d *validationDriver) reviewReadyWithManifest(m coverage.AcceptanceManifest) {
	d.t.Helper()
	dev, di := d.action("engine.development.batch")
	design, qi := d.action("engine.qa.design")
	if len(d.run.PendingActions) != 2 {
		d.t.Fatalf("development/design must be concurrent: %+v", d.run.PendingActions)
	}
	d.business(design, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Manifest: &m})
	d.drive()
	reviewID, ri := d.action("engine.qa.review.rationale")
	r := approvedReview(m)
	d.business(reviewID, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Review: &r})
	d.drive()
	if _, ok := d.run.ValidationInputs[dev]; !ok {
		d.t.Fatal("QA review disturbed development")
	}
	d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implemented input checks", Build: []string{"go build succeeded"}, Tests: []string{"input rejection test succeeded"}}})
	d.drive()
	d.materialize()
	d.materialize()
}
func executionFor(input protocol.ValidationInput) coverage.ExecutionReport {
	r := coverage.ExecutionReport{Binding: *input.Execution}
	for _, edge := range input.Whitelist.Entries {
		inherited := slices.IndexFunc(input.Inherited, func(row coverage.ExecutionRecord) bool {
			return row.ReviewKind == edge.ReviewKind && row.SourceID == edge.SourceID && row.PointID == edge.PointID && row.CaseID == edge.CaseID
		})
		if inherited >= 0 {
			r.Records = append(r.Records, input.Inherited[inherited])
		} else {
			r.Records = append(r.Records, coverage.ExecutionRecord{ReviewKind: edge.ReviewKind, SourceID: edge.SourceID, PointID: edge.PointID, CaseID: edge.CaseID, Result: coverage.ExecutedPass, Provenance: coverage.ProvenanceExecuted, Procedure: "executed app run with the case input", Observation: "recorded exit status and command output", Comparison: "exit status and output match the specified oracle"})
		}
		if !slices.Contains(r.Binding.ActualCaseIDs, edge.CaseID) {
			r.Binding.ActualCaseIDs = append(r.Binding.ActualCaseIDs, edge.CaseID)
		}
	}
	return r
}
func TestValidationPipelineUsesCanonicalPublicSurface(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	d.reviewReady()
	qa, qi := d.action("engine.qa.execution")
	gate, gi := d.action("engine.gate")
	if len(d.run.PendingActions) != 2 {
		t.Fatal("QA and gates were not issued together")
	}
	report := executionFor(qi)
	d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
	d.drive()
	if d.run.Validation.Waves[0].Status != "COLLECTING" {
		t.Fatal("wave closed before gate arrived")
	}
	d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "PASS"})
	d.drive()
	w := d.run.Validation.Waves[0]
	if w.Status != "COMPLETE" || d.run.Status == "COMPLETE" || len(d.run.AdapterIntents) != 0 {
		t.Fatalf("expected validated candidate waiting for promotion: %+v", d.run)
	}
	if w.Candidate != *d.run.Validation.Candidate || w.ExpectedDigest == "" {
		t.Fatal("final candidate binding missing")
	}
}

func TestMissingReviewClosesFiniteAttemptsAndAllowsOneBoundExtra(t *testing.T) {
	d := startValidationDriver(t, nil)
	var task protocol.PendingAction
	for i := 0; i < 3; i++ {
		if len(d.run.PendingActions) != 1 {
			t.Fatalf("expected one execution: %+v", d.run.PendingActions)
		}
		var id string
		for key, p := range d.run.PendingActions {
			id = key
			if i == 0 {
				task = p
			} else if p.Task != task.Task {
				t.Fatal("invalid result changed the logical review duty")
			}
		}
		d.receipt(id)
		a := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: id, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: fmt.Sprintf("missing-%d", i)}})
		if a.Status != "REJECTED_INVALID_RESULT" || d.run.RejectedResults[id].Reason == "" {
			t.Fatalf("missing evidence accepted: %+v", a)
		}
		d.drive()
		if i == 1 {
			var ask PendingAskFace
			for _, p := range d.run.PendingAsks {
				if p.Validation != nil && p.Validation.Kind == "RETRY" {
					ask = p
				}
			}
			if ask.Validation == nil || len(d.run.PendingActions) != 0 {
				t.Fatalf("retry did not stop: %+v", d.run)
			}
			payload := &protocol.ValidationDecision{Reason: "one more execution with corrected payload", Digest: ask.Validation.Digest, StateRevision: d.run.Revision}
			d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "approve", Validation: payload}})
			d.drive()
		}
	}
	if len(d.run.PendingActions) != 0 {
		t.Fatal("extra attempt expanded after exhaustion")
	}
	ask := d.validationAsk("RETRY")
	for _, option := range ask.Options {
		if option.ID == "approve" {
			t.Fatal("extra attempt authorization expanded after exhaustion")
		}
	}
	for _, a := range d.run.EndedAttempts {
		if a.Attempts != 3 || a.MaxAttempts != 3 || !a.RetryExhausted {
			t.Fatalf("wrong retry state %+v", a)
		}
	}
}

func TestConfirmedSolutionCoverageAndNonQAEvidenceReachWhitelist(t *testing.T) {
	inventory := &protocol.CoverageInventory{
		Sources:      []coverage.RequiredSource{{SourceID: "SOL-001", Category: coverage.SolutionConstraint, Applicability: coverage.QAApplicability}, {SourceID: "SOL-002", Category: coverage.SolutionConstraint, Applicability: coverage.NonQAApplicability}},
		Points:       []coverage.AcceptancePoint{{PointID: "SP-001", ObservableBehavior: "operation records its result", Oracle: "record exists after success"}},
		Links:        []protocol.SourcePoint{{SourceID: "SOL-001", PointID: "AC-001"}, {SourceID: "SOL-001", PointID: "SP-001"}},
		Alternatives: []coverage.AlternativeVerification{{SourceID: "SOL-002", Reason: "release policy is document verification", Method: "inspect release policy", Status: coverage.StatusPass, Evidence: "policy contains required supported platform statement"}},
	}
	d := configuredValidationDriver(t, inventory, "blackbox", "quality")
	design, qi := d.action("engine.qa.design")
	dev, di := d.action("engine.development.batch")
	m := simpleManifest(qi)
	m.Edges = nil
	for _, link := range qi.SourcePoints {
		for _, c := range m.Cases {
			m.Edges = append(m.Edges, coverage.CoverageEdge{SourceID: link.SourceID, PointID: link.PointID, CaseID: c.CaseID})
		}
	}
	d.business(design, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Manifest: &m})
	d.drive()
	id, ri := d.action("engine.qa.review.rationale")
	r := approvedReview(m)
	d.business(id, protocol.ValidationResult{Binding: ri.Binding, Status: "PASS", Review: &r})
	d.business(dev, protocol.ValidationResult{Binding: di.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go"}, Summary: "implemented records", Build: []string{"build succeeded"}, Tests: []string{"record test passed"}}})
	d.materialize()
	d.materialize()
	_, input := d.action("engine.qa.execution")
	if len(input.Contract.RequiredSources.Sources) != 2 || len(input.Whitelist.Entries) != 6 {
		t.Fatalf("source/point edges lost: %+v", input)
	}
	if got := d.run.IntakeConfirmationReceipt.Coverage; got == nil || got.Alternatives[0].Evidence != inventory.Alternatives[0].Evidence {
		t.Fatal("confirmed non-QA evidence lost")
	}
}

func (d *validationDriver) carryFormattingChange() string {
	d.t.Helper()
	document := validationDocument + "\n"
	revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
	work := []protocol.RequirementWorkDecision{}
	for _, w := range d.run.RequirementWork {
		work = append(work, protocol.RequirementWorkDecision{Work: w, Disposition: protocol.RequirementWorkCarry, Reason: "same obligations and execution inputs"})
	}
	pkg := protocol.RequirementDecisionPackage{CandidateRevision: revision, DeltaDigest: "sha256:editorial", ImpactDigest: "sha256:unchanged-work", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, WorkDecisions: work}
	request, err := protocol.NewRequirementRequestEvent(d.eventID(), pkg, protocol.AskOption{ID: "proceed", Label: "Confirm"}, protocol.AskOption{ID: "abort", Label: "Keep requirement"})
	if err != nil {
		d.t.Fatal(err)
	}
	d.submit(request)
	ask := d.run.PendingRequirementAsk
	d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "proceed", Requirement: &protocol.RequirementDecisionPayload{BaseRevision: d.revision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: work, Reason: "confirmed editorial change with exact work carry"}}})
	d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: document}}})
	d.revision = revision
	d.drive()
	return revision
}

func TestConfirmedCarryKeepsIssuedDevelopmentAndDesign(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "quality")
	old := map[string]protocol.PendingAction{}
	for id, a := range d.run.PendingActions {
		old[id] = a
	}
	revision := d.carryFormattingChange()
	if len(d.run.PendingActions) != 2 {
		t.Fatalf("carry duplicated obligations: %+v", d.run.PendingActions)
	}
	for id, a := range old {
		if d.run.PendingActions[id] != a {
			t.Fatal("carry replaced issued identity")
		}
	}
	// Carried source bindings are projected after verifying the original result's dispatch.
	id, input := d.action("engine.qa.design")
	m := simpleManifest(input)
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &m})
	if d.run.Validation.Sets[coverage.ReviewBlackbox].Manifest.Binding.RequirementRevision != revision {
		t.Fatal("carried design did not bind the confirmed revision")
	}
}

func TestDualQACandidatePreservesReviewedTestBytesAndDeletion(t *testing.T) {
	d := newValidationDriver(t, "blackbox", "whitebox", "quality")
	id, input := d.action("engine.qa.design")
	m := simpleManifest(input)
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &m})
	d.drive()
	id, input = d.action("engine.qa.review.rationale")
	r := approvedReview(m)
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &r})
	id, input = d.action("engine.development.batch")
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Development: &protocol.DevelopmentEvidence{Completion: commandCompletion(), Paths: []string{"app.go", "old_test.go"}, Summary: "production and existing tests", Build: []string{"build passed"}, Tests: []string{"baseline passed"}}})
	trees := map[string]map[string]string{}
	materialize := func() (string, protocol.CandidateParameters) {
		t.Helper()
		if len(d.run.AdapterIntents) != 1 {
			t.Fatalf("expected one typed candidate intent: %+v", d.run.AdapterIntents)
		}
		var intent protocol.HostActionIntent
		for _, i := range d.run.AdapterIntents {
			intent = i
		}
		data, _ := json.Marshal(intent.Adapter.Params)
		var p protocol.CandidateParameters
		if err := json.Unmarshal(data, &p); err != nil {
			t.Fatal(err)
		}
		tree := map[string]string{}
		for path, content := range trees[p.Base] {
			tree[path] = content
		}
		switch p.Stage {
		case "DEVELOPMENT":
			tree["app.go"] = "validated production bytes"
			tree["old_test.go"] = "obsolete test"
		case "QA_ARTIFACT":
			tree["app_test.go"] = "reviewed assertion bytes"
			delete(tree, "old_test.go")
		}
		for mode, a := range p.QAArtifacts {
			if a.Identity != d.run.Validation.Sets[mode].Candidate {
				t.Fatal("assembly used an unreviewed artifact")
			}
			for _, path := range a.Paths {
				if content, ok := trees[a.Identity][path]; ok {
					tree[path] = content
				} else {
					delete(tree, path)
				}
			}
		}
		identity := fmt.Sprintf("tree-%d", len(trees))
		trees[identity] = tree
		treeBytes, _ := json.Marshal(tree)
		e := protocol.CandidateEvidence{ParametersDigest: intent.PayloadDigest, Identity: identity, Digest: fmt.Sprintf("sha256:%x", sha256.Sum256(treeBytes)), Paths: p.Paths, TestOnly: true, ProductionViewEqual: true}
		e.GateView = gateReviewView(p)
		data, _ = json.Marshal(e)
		values := map[string]any{}
		_ = json.Unmarshal(data, &values)
		event, err := protocol.NewAdapterHostActionReceiptEvent(d.eventID(), intent.ActionID, "op.candidate", DefaultProvider, intent.Correlation, intent.PayloadDigest, protocol.HostActionStatusExecuted, "", values)
		if err != nil {
			t.Fatal(err)
		}
		d.submit(event)
		d.drive()
		return identity, p
	}
	materialize()
	materialize()
	id, input = d.action("engine.qa.design")
	m = input.Manifest
	for _, row := range []struct{ id, scenario string }{{"white-success", "SUCCESS"}, {"white-misuse", "MISUSE"}} {
		m.Cases = append(m.Cases, coverage.AcceptanceCase{CaseID: row.id, Mode: coverage.CaseWhitebox, Scenario: row.scenario, TestRef: "app_test.go#" + row.id, Oracle: "specified input outcome"})
		m.Edges = append(m.Edges, coverage.CoverageEdge{SourceID: "REQ-001", PointID: "AC-001", CaseID: row.id})
	}
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Manifest: &m, Development: &protocol.DevelopmentEvidence{Paths: []string{"app_test.go", "old_test.go"}}})
	materialize()
	id, input = d.action("engine.qa.review.rationale")
	r = approvedReview(m)
	d.business(id, protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &r})
	final, p := materialize()
	if p.Stage != "REVIEW_READY" || trees[final]["app.go"] != "validated production bytes" || trees[final]["app_test.go"] != "reviewed assertion bytes" {
		t.Fatalf("final artifact contents: %+v", trees[final])
	}
	if _, exists := trees[final]["old_test.go"]; exists {
		t.Fatal("reviewed deletion disappeared during assembly")
	}
	_, gate := d.gateAction("quality")
	if p.ReviewBase != "native-run-base" || p.Base == p.ReviewBase || gate.Gate == nil || gate.Gate.Gate != "quality" || gate.Gate.BaseSnapshot != p.ReviewBase || gate.Gate.Candidate != d.run.Validation.Waves[0].Candidate || !reflect.DeepEqual(gate.Gate.TestOwnedPaths, []string{"app_test.go", "old_test.go"}) {
		t.Fatalf("gate range lost native base, candidate, or explicit artifact ownership: %+v", gate.Gate)
	}
	if len(gate.Manifest.Cases) != 0 || len(gate.Completed) != 0 || gate.Whitelist != nil {
		t.Fatal("gate received QA case content")
	}
	if len(d.run.Validation.Waves) != 1 || len(d.run.Validation.Waves[0].Targets) != 3 {
		t.Fatalf("shared AC did not reach dual-QA validation: %+v", d.run.Validation.Waves)
	}
}

func TestConfirmedCarryAcceptsOriginalReviewAndWaveEvidence(t *testing.T) {
	for _, handler := range []string{"engine.qa.review.rationale", "engine.qa.execution", "engine.gate"} {
		t.Run(handler, func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			var action string
			var input protocol.ValidationInput
			var result protocol.ValidationResult
			if handler == "engine.qa.review.rationale" {
				design, designed := d.action("engine.qa.design")
				manifest := simpleManifest(designed)
				d.business(design, protocol.ValidationResult{Binding: designed.Binding, Status: "PASS", Manifest: &manifest})
				d.drive()
				action, input = d.action(handler)
				reviewed := approvedReview(manifest)
				result = protocol.ValidationResult{Binding: input.Binding, Status: "PASS", Review: &reviewed}
			} else {
				d.reviewReady()
				action, input = d.action(handler)
				result = protocol.ValidationResult{Binding: input.Binding, Status: "PASS"}
				if handler == "engine.qa.execution" {
					report := executionFor(input)
					result.Execution, result.Tool, result.Environment = &report, "app-v1", "isolated-project"
				} else {
					qa, qi := d.action("engine.qa.execution")
					report := executionFor(qi)
					d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
				}
			}
			oldAction := d.run.PendingActions[action]
			revision := d.carryFormattingChange()
			if d.run.PendingActions[action] != oldAction {
				t.Fatal("carry replaced the signed execution")
			}
			current := d.run.ValidationInputs[action]
			if current.Binding != input.Binding {
				t.Fatal("carry rewrote the issued work binding")
			}
			if handler == "engine.qa.execution" {
				before, _ := json.Marshal(input.Execution)
				after, _ := json.Marshal(current.Execution)
				if string(before) != string(after) {
					t.Fatal("carry changed the frozen execution contract")
				}
			}
			d.receipt(action)
			accepted := d.submit(protocol.Event{Kind: protocol.KindWorkerResult, Result: &protocol.WorkerResultPayload{ActionID: action, Provider: DefaultProvider, Outcome: protocol.OutcomePass, PayloadDigest: "carried-result", Validation: &result}})
			if accepted.Status != "ACCEPTED" || len(d.run.RejectedResults) != 0 {
				t.Fatalf("carried result rejected: %+v %+v", accepted, d.run.RejectedResults)
			}
			if handler == "engine.qa.review.rationale" {
				set := d.run.Validation.Sets[coverage.ReviewBlackbox]
				if set.Status != "FROZEN" || set.Review.Binding.RequirementRevision != revision {
					t.Fatal("carried review did not fulfill current obligation")
				}
				if len(set.ReviewHistory) != 1 || set.ReviewHistory[0].Review.Binding != result.Review.Binding {
					t.Fatal("raw review history lost the original binding")
				}
			} else if handler == "engine.gate" && d.run.Validation.Waves[0].Status != "COMPLETE" {
				t.Fatal("carried gate did not complete the frozen wave")
			}
			for _, pending := range d.run.PendingActions {
				if pending.Task == oldAction.Task {
					t.Fatal("accepted carry left a duplicate task or retry")
				}
			}
		})
	}
}

func TestValidationRequirementRemedyUsesOneConfirmableAsk(t *testing.T) {
	for _, choice := range []protocol.AskOptionID{"proceed", "abort"} {
		t.Run(string(choice), func(t *testing.T) {
			d := newValidationDriver(t, "blackbox", "quality")
			d.reviewReady()
			qa, qi := d.action("engine.qa.execution")
			gate, gi := d.action("engine.gate")
			report := executionFor(qi)
			d.business(qa, protocol.ValidationResult{Binding: qi.Binding, Status: "PASS", Execution: &report, Tool: "app-v1", Environment: "isolated-project"})
			d.business(gate, protocol.ValidationResult{Binding: gi.Binding, Status: "FAIL", Findings: []review.Finding{{Severity: "P2", Message: "Output ordering needs correction"}}})
			wave := d.run.Validation.Waves[0]
			var findingID string
			for id := range d.run.Validation.Findings {
				findingID = id
			}
			resolution := protocol.FindingResolution{FindingID: findingID, Decision: "NEW", Requirement: "AC-001", NormalPath: "app run", Module: "output", RootCause: "ordering", Consequence: "wrong ordering", Severity: "P2", SeverityRationale: "limited output issue", Verification: review.FindingVerification{Scope: "in scope", Severity: "limited", Causality: "verified", Reproduction: "app run", Classification: "implementation"}, Reason: "verified"}
			d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: wave.ID, Validation: &protocol.ValidationObservation{Kind: "ADJUDICATE", Findings: []protocol.FindingResolution{resolution}}}})
			d.submit(protocol.Event{Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{Control: protocol.ControlValidation, Options: []protocol.AskOption{{ID: "approve", Label: "Choose remedy"}}, Validation: &protocol.ValidationRequest{Kind: "VALIDATION_DETAIL_DISPOSITION", Target: wave.ID, Remedies: []string{"REPAIR", "REQUEST_REQUIREMENT_CHANGE"}}}})
			var selected bool
			for _, ask := range d.run.PendingAsks {
				if ask.Validation != nil && ask.Validation.Kind == "VALIDATION_DETAIL_DISPOSITION" {
					d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: ask.Token, Choice: "approve", Validation: &protocol.ValidationDecision{Reason: "Clarify the required ordering before further repair", Digest: ask.Validation.Digest, StateRevision: d.run.Revision, Remedy: "REQUEST_REQUIREMENT_CHANGE"}}})
					selected = true
					break
				}
			}
			if !selected || d.run.PendingRequirementAsk == nil {
				t.Fatal("remedy did not create the requirement barrier")
			}
			ask := *d.run.PendingRequirementAsk
			t.Logf("created requirement ask=%s options=%+v package=%+v", ask.RequestID, ask.Options, ask.Package)
			d.drive()
			if len(d.run.PendingActions) != 0 || d.run.PendingRequirementAsk.RequestID != ask.RequestID {
				t.Fatal("requirement barrier dispatched repair or changed Ask identity")
			}
			document := validationDocument + "\n"
			revision := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(document)))
			work := []protocol.RequirementWorkDecision{}
			for _, w := range d.run.RequirementWork {
				work = append(work, protocol.RequirementWorkDecision{Work: w, Disposition: protocol.RequirementWorkCarry, Reason: "same obligations and execution inputs"})
			}
			pkg := protocol.RequirementDecisionPackage{CandidateRevision: revision, DeltaDigest: "sha256:editorial", ImpactDigest: "sha256:unchanged-work", Source: protocol.RequirementSourceUserInitiated, Classification: protocol.RequirementClassificationPreserved, WorkDecisions: work}
			d.submit(protocol.Event{Kind: protocol.KindRequestControl, Request: &protocol.RequestPayload{Control: protocol.ControlRequestRequirementChange, Request: protocol.RequestID(ask.RequestID), Token: d.run.PendingRequirementAsk.Token, Package: &pkg}})
			if d.run.PendingRequirementAsk.RequestID != ask.RequestID || d.run.PendingRequirementAsk.Package == nil {
				t.Fatal("package update replaced or failed to bind the existing Ask")
			}
			t.Logf("package bound to same ask=%s", ask.RequestID)
			decision := &protocol.DecidePayload{Request: protocol.RequestID(ask.RequestID), Token: d.run.PendingRequirementAsk.Token, Choice: choice}
			if choice == "proceed" {
				decision.Requirement = &protocol.RequirementDecisionPayload{BaseRevision: d.revision, CandidateRevision: revision, DeltaDigest: pkg.DeltaDigest, ImpactDigest: pkg.ImpactDigest, Source: pkg.Source, Classification: pkg.Classification, WorkDecisions: work, Reason: "Confirmed unchanged ordering requirement"}
			}
			d.submit(protocol.Event{Kind: protocol.KindDecide, Decide: decision})
			if d.run.PendingRequirementAsk != nil {
				t.Fatal("resolved Ask retained the requirement barrier")
			}
			expected := d.revision
			if choice == "proceed" {
				expected = revision
			}
			if d.run.Review.RequirementRevision != expected {
				t.Fatalf("requirement=%s want %s", d.run.Review.RequirementRevision, expected)
			}
			if choice == "proceed" {
				d.submit(protocol.Event{Kind: protocol.KindOperatorObservation, Observation: &protocol.OperatorObservationPayload{Subject: revision, Validation: &protocol.ValidationObservation{Kind: "REQUIREMENT", RequirementRevision: revision, Document: document}}})
			}
			d.drive()
			_, repair := d.action("engine.repair")
			if repair.Binding.RequirementRevision != expected {
				t.Fatal("repair resumed against the wrong confirmed requirement")
			}
			t.Logf("resolved %s; current requirement=%s; repair resumed", choice, expected)
		})
	}
}

// The adapter fixture classifies the explicitly declared QA artifacts. It does
// not infer ownership from path spelling or provide evidence in Submit/Drive.
func gateReviewView(params protocol.CandidateParameters) *protocol.GateReviewView {
	if params.Stage != "REVIEW_READY" {
		return nil
	}
	view := &protocol.GateReviewView{TestOwnedPaths: []string{}}
	for _, artifact := range params.QAArtifacts {
		for _, path := range artifact.Paths {
			if !slices.Contains(view.TestOwnedPaths, path) {
				view.TestOwnedPaths = append(view.TestOwnedPaths, path)
			}
		}
	}
	slices.Sort(view.TestOwnedPaths)
	return view
}
