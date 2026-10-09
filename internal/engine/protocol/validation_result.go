package protocol

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"formal-gates/internal/engine/compiler"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/review"
	"formal-gates/internal/requirement"
)

const CodeValidationEvidenceRejected = "VALIDATION_EVIDENCE_REJECTED"

func validationReject(err error) error {
	return &RejectedError{Code: CodeValidationEvidenceRejected, Detail: err.Error()}
}
func validationError(message string) error { return validationReject(fmt.Errorf("%s", message)) }

func (e *Engine) acceptValidationObservation(s *State, p *OperatorObservationPayload, revision, nextRevision uint64) error {
	if !pipelineEnabled(s, e.cfg.Definition) {
		return validationError("validation is not part of this definition/route")
	}
	o := p.Validation
	if o.Kind == "REQUIREMENT" {
		if s.resetPending() {
			return validationError("requirement inventory awaits the complete reset confirmation")
		}
		if o.RequirementRevision != s.RequirementRevision() || s.IntakeConfirmationReceipt == nil {
			return validationError("requirement observation must bind the confirmed revision")
		}
		doc, err := requirement.ParseRequirementProjection(s.IntakeConfirmationReceipt.RequirementSource, []byte(o.Document))
		if err != nil {
			return validationReject(err)
		}
		if strings.TrimPrefix(o.RequirementRevision, "sha256:") != doc.ContentDigest {
			return validationError("document bytes differ from the confirmed requirement revision")
		}
		if s.Validation != nil && s.Validation.RequirementRevision == o.RequirementRevision && s.Validation.Generation == s.requirementGeneration() {
			return validationError("requirement inventory is already frozen")
		}
		if s.Validation == nil {
			s.Validation = &Validation{Sets: map[coverage.ReviewKind]*CaseSet{}, Findings: map[string]*ValidationFinding{}, Problems: map[string]*ValidationProblem{}}
		}
		s.Validation.Document = doc
		if s.RequirementGuarantee != nil {
			s.RequirementGuarantee.ManifestDigest = digestOfCanonical(doc.Requirements)
		}
		s.Validation.RequirementRevision = o.RequirementRevision
		s.Validation.Generation = s.requirementGeneration()
		if err := s.validateConfirmedCoverage(); err != nil {
			return err
		}
		return nil
	}
	if s.Validation == nil {
		return validationError("confirmed requirement inventory is missing")
	}
	switch o.Kind {
	case "GATE_IMPACT":
		return s.acceptGateImpact(p.Subject, o)
	case "EXECUTION_SCOPE":
		return s.acceptExecutionScope(p.Subject, o)
	case "SUGGESTION":
		set := s.Validation.Sets[o.Mode]
		if set == nil {
			return validationError("unknown case-set")
		}
		if o.Reason == "" || !slices.Contains([]string{"ACCEPT", "DEFER", "DECLINE"}, o.Decision) {
			return validationError("suggestion needs ACCEPT/DEFER/DECLINE and a reason")
		}
		found := false
		for i := range set.Suggestions {
			item := &set.Suggestions[i]
			if item.ID == o.SuggestionID {
				if item.Decision != "" {
					return validationError("suggestion already disposed")
				}
				item.Decision = o.Decision
				item.Reason = o.Reason
				found = true
			}
		}
		if !found {
			return validationError("unknown suggestion ID")
		}
		if len(o.AffectedCases) > 0 {
			if o.Decision != "ACCEPT" {
				return validationError("only accepted suggestions can revise cases")
			}
			if err := recordAffectedCases(set, o.AffectedCases); err != nil {
				return err
			}
		}
	case "ADJUDICATE":
		return e.adjudicateValidation(s, p.Subject, o.Findings, nextRevision)
	default:
		return validationError("unknown validation observation kind")
	}
	return nil
}
func recordAffectedCases(set *CaseSet, ids []string) error {
	for _, id := range ids {
		found := false
		for _, c := range set.Manifest.Cases {
			if c.CaseID == id {
				found = true
			}
		}
		if !found {
			return validationError("affected case is not in the manifest: " + id)
		}
	}
	for _, id := range ids {
		if !slices.Contains(set.AffectedCases, id) {
			set.AffectedCases = append(set.AffectedCases, id)
		}
	}
	return nil
}

func reopenCases(set *CaseSet, ids []string) error {
	if err := recordAffectedCases(set, ids); err != nil {
		return err
	}
	set.reopen(nil)
	return nil
}

// Reopening invalidates current approval; prior reviews remain inheritance evidence.
func (set *CaseSet) reopen(ids []string) {
	for _, id := range ids {
		if !slices.Contains(set.AffectedCases, id) {
			set.AffectedCases = append(set.AffectedCases, id)
		}
	}
	set.Status = "DRAFT"
	set.Review = nil
	set.Candidate = ""
}

func advanceCaseReview(set *CaseSet) bool {
	if (set.Status != "REVIEW" && set.Status != "SUGGESTIONS") || set.Review == nil || set.Review.Verdict() != coverage.StatusPass {
		return false
	}
	if len(set.AffectedCases) > 0 {
		set.reopen(nil)
		return true
	}
	status := "FROZEN"
	for _, sg := range set.Suggestions {
		if sg.Decision == "" || sg.Reason == "" {
			status = "SUGGESTIONS"
			break
		}
	}
	changed := set.Status != status
	set.Status = status
	return changed
}

func (s *State) validateManifest(mode coverage.ReviewKind, m coverage.AcceptanceManifest) error {
	return s.validateDesignManifest(mode, m, true)
}

func (s *State) validateDesignManifest(mode coverage.ReviewKind, m coverage.AcceptanceManifest, complete bool) error {
	contract := s.coverageContract(nil)
	want := coverage.ManifestBinding{SourceBinding: contract.RequiredSources.Binding, ReviewKind: mode, RouteScope: s.validationRouteDigest(), TopologyScope: digestOfCanonical(s.Topology)}
	if m.Binding != want {
		return validationError("manifest has wrong run/revision/mode/route/topology binding")
	}
	points, links := s.coveragePoints()
	pointIndex := map[string]coverage.AcceptancePoint{}
	for _, point := range points {
		pointIndex[point.PointID] = point
	}
	for _, point := range m.Points {
		if want, ok := pointIndex[point.PointID]; !ok || want != point {
			return validationError("manifest point differs from confirmed acceptance point")
		}
	}
	for _, edge := range m.Edges {
		found := false
		for _, link := range links {
			if link.SourceID == edge.SourceID && link.PointID == edge.PointID {
				found = true
			}
		}
		if !found {
			return validationError("edge is not a confirmed source-point relation")
		}
	}
	contract.Manifests = []coverage.AcceptanceManifest{m}
	contract.SelectedKinds = []coverage.ReviewKind{mode}
	contract.RequiredSources.Sources = nil
	allSources := s.coverageContract(nil).RequiredSources.Sources
	for _, id := range m.Sources {
		found := false
		for _, src := range allSources {
			if src.SourceID == id {
				contract.RequiredSources.Sources = append(contract.RequiredSources.Sources, src)
				found = true
			}
		}
		if !found {
			return validationError("unknown source")
		}
	}
	contract.AlternativeVerifications = nil
	if err := contract.Validate(); err != nil {
		return validationReject(err)
	}
	if err := coverage.ValidateRecordedScenarios(m); err != nil {
		return validationReject(err)
	}
	if complete {
		if err := coverage.ValidateScenarios(m); err != nil {
			return validationReject(err)
		}
	}
	return nil
}
func (s *State) validateCaseReview(mode coverage.ReviewKind, r coverage.ReviewResult) error {
	c := modeCoverageContract(s.coverageContract(nil), mode)
	if err := c.ValidateReviewEvidence([]coverage.ReviewResult{r}); err != nil {
		return validationReject(err)
	}
	return nil
}

// prepareValidationResult builds only the next QA domain value. Transport,
// capacity, barrier and attempt facts stay in their existing transaction.
func (e *Engine) prepareValidationResult(s *State, record WorkerResult) (*Validation, error) {
	copy := *s
	data, err := json.Marshal(s.Validation)
	if err != nil {
		return nil, err
	}
	var next Validation
	if err := json.Unmarshal(data, &next); err != nil {
		return nil, err
	}
	copy.Validation = &next
	if err := e.acceptValidationResult(&copy, record); err != nil {
		return nil, err
	}
	return &next, nil
}

func (e *Engine) acceptValidationResult(s *State, record WorkerResult) error {
	p := record.Validation
	b, known := s.DispatchBindings[record.ActionID]
	if !known {
		return validationError("business result is not bound to an issued validation task")
	}
	if p == nil || p.Binding != b {
		return validationError("business result binding differs from its dispatch")
	}
	if s.Validation == nil {
		return validationError("requirement inventory is missing")
	}
	requirementCarry := b.Generation != s.Validation.Generation
	routeCarry := b.RouteScope != "" && b.RouteScope != s.validationRouteDigest()
	if requirementCarry || routeCarry {
		if requirementCarry && !s.validationCarryAuthorized(record.ActionID) {
			return validationError("result has no current requirement carry authorization")
		}
		projected := *p
		mode := s.validationMode(b.Task)
		binding := coverage.ManifestBinding{SourceBinding: s.coverageContract(nil).RequiredSources.Binding, ReviewKind: mode, RouteScope: s.validationRouteDigest(), TopologyScope: digestOfCanonical(s.Topology)}
		if p.Manifest != nil {
			m := *p.Manifest
			if routeCarry && m.Binding.RouteScope != b.RouteScope {
				return validationError("design differs from its issued route scope")
			}
			m.Binding = binding
			projected.Manifest = &m
		}
		if p.Review != nil {
			r := *p.Review
			r.Binding = binding
			projected.Review = &r
		}
		if set := s.Validation.Sets[mode]; set != nil && p.Review != nil {
			original := set.Manifest
			original.Binding = record.Validation.Review.Binding
			if digestOfCanonical(original) != b.CaseVersion {
				return validationError("carried review differs from its signed case-set version")
			}
			b.CaseVersion = set.Version
		}
		p = &projected
	}
	role := s.validationRole(b.Task, e.cfg.Definition)
	if p.GranularityReview != nil && role != "engine.repair" {
		return validationError("only a repair worker can submit a repair execution plan")
	}
	if p.Checkpoint && role != "engine.qa.design" && role != "engine.qa.execution" {
		return validationError("only QA design and execution can record a checkpoint")
	}
	mode := s.validationMode(b.Task)
	if record.Outcome != OutcomePass && !(record.Outcome == OutcomeRuntimeError && role == "engine.qa.execution" && p.Checkpoint && p.Execution != nil) {
		return validationError("semantic result requires a completed worker transport")
	}
	count := 0
	for _, present := range []bool{p.Development != nil, p.Manifest != nil, p.Review != nil, p.Execution != nil, p.Resolution != nil} {
		if present {
			count++
		}
	}
	switch role {
	case "engine.merge.resolve":
		input := s.conflictInput(b.Task.Scope)
		if count != 1 || p.Status != "PASS" || p.Resolution == nil || input == nil || p.Resolution.Summary == "" {
			return validationError("conflict resolution requires the original conflict input and complete manifest")
		}
		expected := []string{}
		for _, conflict := range input.Evidence.Conflicts {
			expected = append(expected, conflict.Path)
		}
		actual := slices.Clone(p.Resolution.Paths)
		slices.Sort(expected)
		slices.Sort(actual)
		if len(expected) == 0 || !slices.Equal(actual, expected) {
			return validationError("resolution manifest must cover exactly the original conflict paths")
		}
	case "engine.repair.verify":
		return s.acceptRepairComparison(record, e.cfg.Definition)
	case "engine.repair":
		return s.acceptRepairResult(record)
	case "engine.development.batch":
		if count != 1 || p.Development == nil || p.Status != "PASS" || len(p.Development.Paths) == 0 || p.Development.Summary == "" {
			return validationError("development requires declared paths and summary")
		}
		if role == "engine.development.batch" {
			_, _, batch, ok := s.developmentBatch(p.Binding.Task, e.cfg.Definition)
			if !ok {
				return validationError("development task is not a confirmed batch")
			}
			if err := validateDevelopmentCompletion(batch, p.Development); err != nil {
				return err
			}
			if s.Validation.DevelopmentSnapshot != "" {
				s.Validation.DevelopmentSnapshot, s.Validation.AuthoringWorkspace = "", ""
				s.Validation.Candidate = nil
				if set := s.Validation.Sets[coverage.ReviewWhitebox]; set != nil {
					set.reopen(nil)
				}
			}

		}
	case "engine.qa.design":
		if (count != 1 && !(count == 2 && mode == coverage.ReviewWhitebox && p.Development != nil)) || p.Manifest == nil || p.Status != "PASS" {
			return validationError("QA design requires one manifest")
		}
		if err := s.validateDesignManifest(mode, *p.Manifest, !p.Checkpoint); err != nil {
			return err
		}
		old := s.Validation.Sets[mode]
		set := &CaseSet{DesignTask: b.Task, Manifest: *p.Manifest, Status: "REVIEW", Version: digestOfCanonical(p.Manifest)}
		if p.Checkpoint {
			set.Status = "DRAFT"
		}
		if p.Development != nil {
			set.Paths = slices.Clone(p.Development.Paths)
		}
		if old != nil {
			for _, prior := range old.ReviewHistory {
				for _, approved := range prior.Review.CaseDecisions {
					if p.Checkpoint && approved.Status == coverage.StatusPass && slices.ContainsFunc(old.Manifest.Cases, func(c coverage.AcceptanceCase) bool { return c.CaseID == approved.ID }) && !slices.ContainsFunc(set.Manifest.Cases, func(c coverage.AcceptanceCase) bool { return c.CaseID == approved.ID }) {
						return validationError("design must retain previously approved case responsibility: " + approved.ID)
					}
				}
			}
			if p.Checkpoint && old.Status == "THAWED" {
				set.Status = "THAWED"
			}
			set.AffectedCases = old.AffectedCases
			set.Suggestions = old.Suggestions
			set.ReviewHistory = old.ReviewHistory
			if old.Status != "DRAFT" && old.Status != "THAWED" {
				return validationError("case-set is not open for design")
			}
		}
		s.Validation.Sets[mode] = set
	case "engine.qa.review.rationale":
		set := s.Validation.Sets[mode]
		if set == nil || count != 1 || p.Review == nil || set.Status != "REVIEW" || b.CaseVersion != set.Version {
			return validationError("review does not bind an open case-set version")
		}
		if err := s.validateReviewPolicy(set, *p.Review); err != nil {
			return err
		}
		if err := s.validateCaseReview(mode, *p.Review); err != nil {
			return err
		}
		for i := range p.Suggestions {
			sg := p.Suggestions[i]
			if (sg.Severity != "P2" && sg.Severity != "P3") || sg.Message == "" || sg.Decision != "" {
				return validationError("QA suggestion must be an undisposed P2/P3")
			}
			sg.ID = digestOfCanonical(struct {
				Action  string
				Index   int
				Message string
			}{record.ActionID, i, sg.Message})
			set.Suggestions = append(set.Suggestions, sg)
		}
		set.AffectedCases = nil
		set.Review = p.Review
		rawManifest := set.Manifest
		rawManifest.Binding = record.Validation.Review.Binding
		set.ReviewHistory = append(set.ReviewHistory, CaseReviewEvidence{Candidate: record.Validation.Binding.Candidate, Manifest: rawManifest, Review: *record.Validation.Review})
		if p.Review.Verdict() == coverage.StatusFail {
			set.reopen(nil)
		}
	case "engine.qa.execution", "engine.gate":
		w := s.validationWave(b.WaveID)
		_, repairTarget := s.repairGateTarget(b.Task)
		if w == nil || w.Status != "COLLECTING" && repairTarget == nil && !(w.Status == "INVALIDATED" && s.validationCarryAuthorized(record.ActionID)) {
			return validationError("validation wave is not collecting this result")
		}
		target := w.target(b.Task.String())
		if target == nil || target.Status == "DISPOSED" || target.Status == "PASS" || target.Status == "FAIL" {
			return validationError("validation target is already settled or unknown")
		}
		if role == "engine.qa.execution" {
			if count != 1 || p.Execution == nil {
				return validationError("QA execution requires per-case records")
			}
			if err := s.acceptExecution(target, *p.Execution, p.Tool, p.Environment, p.Checkpoint, e.cfg.Definition); err != nil {
				return err
			}
			if p.Checkpoint {
				return nil
			}
			target.Status = string(coverage.ExecutionVerdict(target.Execution.Records))
		} else {
			if count != 0 || (p.Status != "PASS" && p.Status != "FAIL") {
				return validationError("gate requires PASS/FAIL and findings")
			}
			target.Status = p.Status
		}
		target.ResultAction = record.ActionID
		for i, raw := range p.Findings {
			if err := validateRawFinding(raw); err != nil {
				return err
			}
			id := digestOfCanonical(struct {
				Wave, Target string
				Index        int
				Raw          review.Finding
			}{w.ID, b.Task.String(), i, raw})
			s.Validation.Findings[id] = &ValidationFinding{ID: id, WaveID: w.ID, Target: b.Task.String(), Raw: &raw}
		}
		if role == "engine.qa.execution" {
			for _, row := range target.Execution.Records {
				if row.Result != coverage.ExecutedFail {
					continue
				}
				failure := ExecutionFailure{ResultAction: record.ActionID, CaseID: row.CaseID}
				id := digestOfCanonical(failure)
				s.Validation.Findings[id] = &ValidationFinding{ID: id, WaveID: w.ID, Target: b.Task.String(), ExecutionFailure: &failure}
			}
		} else if target.Status == "FAIL" && len(p.Findings) == 0 {
			return validationError("a failed validation target needs a finding for Operator adjudication")
		}
	default:
		return validationError("unknown business handler")
	}
	return nil
}
func validateRawFinding(f review.Finding) error {
	if f.Message == "" || !slices.Contains([]string{"P0", "P1", "P2", "P3"}, f.Severity) {
		return validationError("finding requires message and severity")
	}
	if f.Severity == "P0" || f.Severity == "P1" {
		if f.SeverityRationale == "" || f.Requirement == "" || f.NormalPath == "" || f.ActualError == "" || f.DeferralImpact == "" || len(f.Locations) == 0 {
			return validationError("P0/P1 finding requires independent rationale, requirement, normal path, actual error, deferral impact and locations")
		}
	}
	return nil
}

// modeCoverageContract projects the QA-owned portion without sharing other
// modes' cases or non-QA verification evidence with the recipient.
func modeCoverageContract(c coverage.CoverageContract, mode coverage.ReviewKind) coverage.CoverageContract {
	manifests := []coverage.AcceptanceManifest{}
	for _, m := range c.Manifests {
		if m.Binding.ReviewKind == mode {
			manifests = append(manifests, m)
		}
	}
	c.Manifests = manifests
	c.SelectedKinds = []coverage.ReviewKind{mode}
	sources := []coverage.RequiredSource{}
	for _, src := range c.RequiredSources.Sources {
		if len(manifests) == 0 && src.Applicability == coverage.QAApplicability || len(manifests) == 1 && slices.Contains(manifests[0].Sources, src.SourceID) {
			sources = append(sources, src)
		}
	}
	c.RequiredSources.Sources = sources
	c.AlternativeVerifications = nil
	return c
}

func (s *State) executionContract(mode coverage.ReviewKind, wave *ValidationWave) (coverage.CoverageContract, coverage.ApprovedWhitelist, error) {
	c := modeCoverageContract(wave.Coverage, mode)
	var approved coverage.ReviewResult
	for _, r := range wave.Reviews {
		if r.Binding.ReviewKind == mode {
			approved = r
		}
	}
	if len(c.Manifests) != 1 {
		return c, coverage.ApprovedWhitelist{}, validationError("frozen wave has no mode contract")
	}
	w, err := c.ValidateReviews([]coverage.ReviewResult{approved})
	return c, w, err
}
func (s *State) acceptExecution(target *ValidationTarget, report coverage.ExecutionReport, tool, environment string, checkpoint bool, cd *compiler.CompiledDefinition) error {
	if tool == "" || environment == "" {
		return validationError("execution requires tool and environment identities")
	}
	wave := s.validationWaveForTask(target.Task)
	if wave == nil {
		return validationError("execution has no frozen wave")
	}
	c, w, err := s.executionContract(target.Mode, wave)
	if err != nil {
		return validationReject(err)
	}
	if report.Binding.RequiredSourcesDigest != w.RequiredSourcesDigest || report.Binding.ManifestDigest != w.ManifestDigest || report.Binding.MapDigest != w.MapDigest || report.Binding.WhitelistDigest != w.Digest || report.Binding.Candidate != wave.Candidate {
		return validationError("execution is not bound to the current approved candidate")
	}
	if report.Binding.Scope != wave.Scope {
		return validationError("execution scope differs from the frozen wave")
	}
	expected := executionCaseIDs(w)
	required := requiredExecutionCases(wave, target, w)
	if len(required) == 0 {
		return validationError("execution has no fixed host scope")
	}
	_, err = s.executionInheritance(wave, target.Mode, w, required, target.ScopeReason)
	if err != nil {
		return err
	}
	ids := slices.Clone(report.Binding.ExpectedCaseIDs)
	slices.Sort(ids)
	if !slices.Equal(ids, expected) {
		return validationError("expected cases differ from approved whitelist")
	}
	actual := []string{}
	for _, row := range report.Records {
		if !slices.Contains(actual, row.CaseID) {
			actual = append(actual, row.CaseID)
		}
	}
	slices.Sort(actual)
	declared := slices.Clone(report.Binding.ActualCaseIDs)
	slices.Sort(declared)
	if !slices.Equal(actual, declared) {
		return validationError("submitted actual case IDs do not match submitted execution records")
	}

	nativeFacts := s.qaExecutionFacts(target, cd)
	expectedTool, expectedEnvironment := ExecutionIdentities(nativeFacts)
	if tool != expectedTool || environment != expectedEnvironment {
		return validationError("execution tool/environment differ from native receipts")
	}
	rows := map[string]coverage.ExecutionRecord{}
	key := func(r coverage.ExecutionRecord) string {
		return string(r.ReviewKind) + "/" + r.SourceID + "/" + r.PointID + "/" + r.CaseID
	}
	if target.Execution != nil && target.Tool == tool && target.Environment == environment && target.Execution.Binding.Candidate == report.Binding.Candidate {
		for _, r := range target.Execution.Records {
			rows[key(r)] = r
		}
	}
	for _, r := range report.Records {
		k := key(r)
		if _, exists := rows[k]; exists {
			return validationError("duplicate execution evidence: " + r.CaseID)
		}
		found := false
		for _, edge := range w.Entries {
			if edge.ReviewKind == r.ReviewKind && edge.SourceID == r.SourceID && edge.PointID == r.PointID && edge.CaseID == r.CaseID {
				found = true
			}
		}
		if !found {
			return validationError("execution has an unapproved edge")
		}
		if r.Result != coverage.ExecutedPass && r.Result != coverage.ExecutedFail {
			return validationError("unfinished/skipped cases cannot supply completed evidence")
		}
		if r.Provenance == coverage.ProvenanceExecuted {
			if !slices.Contains(required, r.CaseID) {
				return validationError("executed case is outside the fixed host scope: " + r.CaseID)
			}
			index := slices.IndexFunc(nativeFacts, func(fact ExecutionFact) bool { return fact.CaseID == r.CaseID })
			if index < 0 {
				return validationError("execution case has no complete native receipts: " + r.CaseID)
			}
			fact := nativeFacts[index]
			procedure, actual, err := fact.Render()
			if err != nil {
				return err
			}
			if r.Procedure != procedure || r.Observation != actual {
				return validationError("execution facts differ from original receipts: " + r.CaseID)
			}
			for _, operation := range fact.Operations {
				if operation.Parameters.QA.GoTestRef != "" && operation.Evidence.GoResult != "pass" && r.Result == coverage.ExecutedPass {
					return validationError("Go test did not actually pass: " + r.CaseID)
				}
			}
			if err := coverage.ValidateEvidence(r.CaseID, r.Procedure, r.Observation, r.Comparison); err != nil {
				return validationReject(err)
			}
		} else {
			if r.Provenance != coverage.ProvenanceInherited || r.Result != coverage.ExecutedPass || r.ReuseEvidence == "" || r.InheritedFromCandidate == "" || slices.Contains(required, r.CaseID) {
				return validationError("inheritance is outside the fixed partition")
			}
			edge := coverage.WhitelistEntry{ReviewKind: r.ReviewKind, SourceID: r.SourceID, PointID: r.PointID, CaseID: r.CaseID}
			if _, err := s.inheritedExecutionRecord(wave, target.Mode, edge, r.InheritedFromCandidate, r.ReuseEvidence); err != nil {
				return err
			}
		}
		rows[k] = r
	}
	merged := report
	merged.Records = nil
	merged.Binding.ActualCaseIDs = nil
	keys := make([]string, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	for _, k := range keys {
		r := rows[k]
		merged.Records = append(merged.Records, r)
		if !slices.Contains(merged.Binding.ActualCaseIDs, r.CaseID) {
			merged.Binding.ActualCaseIDs = append(merged.Binding.ActualCaseIDs, r.CaseID)
		}
	}
	slices.Sort(merged.Binding.ActualCaseIDs)
	if !checkpoint {
		if err := c.ValidateResults(w, merged); err != nil {
			return validationReject(err)
		}
	}
	target.Execution = &merged
	target.Tool = tool
	target.Environment = environment
	return nil
}
func sameWaveCases(a, b *ValidationWave, mode coverage.ReviewKind) bool {
	if a.Versions[mode] == b.Versions[mode] {
		return true
	}
	for _, first := range a.Coverage.Manifests {
		if first.Binding.ReviewKind != mode {
			continue
		}
		for _, second := range b.Coverage.Manifests {
			if second.Binding.ReviewKind == mode {
				first.Binding = second.Binding
				return digestOfCanonical(first) == digestOfCanonical(second)
			}
		}
	}
	return false
}

func (s *State) validateRepairEvidence(d DevelopmentEvidence, requireRuns bool) error {
	var manifests []coverage.AcceptanceManifest
	for _, mode := range s.validationKinds() {
		if set := s.Validation.Sets[mode]; set != nil {
			manifest := set.Manifest
			manifest.Binding.ReviewKind = mode
			manifests = append(manifests, manifest)
		}
	}
	return ValidateRepairEvidence(manifests, d, requireRuns)
}

// ValidateRepairEvidence checks declared affected cases against the issued
// approved manifests. Supplied runs always belong to that exact declared set;
// requireRuns additionally requires complete self-run evidence for each case.
func ValidateRepairEvidence(manifests []coverage.AcceptanceManifest, d DevelopmentEvidence, requireRuns bool) error {
	expected := map[string]coverage.ReviewKind{}
	for _, manifest := range manifests {
		for _, c := range manifest.Cases {
			if _, exists := expected[c.CaseID]; exists {
				return validationError("repair needs unambiguous case IDs across selected modes")
			}
			expected[c.CaseID] = manifest.Binding.ReviewKind
		}
	}
	affected := map[string]bool{}
	for _, id := range d.AffectedCases {
		if affected[id] || expected[id] == "" {
			return validationError("repair affected case is duplicate or outside the approved set: " + id)
		}
		affected[id] = true
	}
	seen := map[string]bool{}
	for _, row := range d.CaseRuns {
		if !affected[row.CaseID] || expected[row.CaseID] != row.Mode || seen[row.CaseID] || row.Result != coverage.ExecutedPass {
			return validationError("repair execution must uniquely match each affected mode/case and pass")
		}
		if err := coverage.ValidateEvidence(row.CaseID, row.Procedure, row.Observation, row.Comparison); err != nil {
			return validationReject(err)
		}
		seen[row.CaseID] = true
	}
	if requireRuns && len(expected) > 0 && (len(affected) == 0 || len(seen) != len(affected)) {
		return validationError("pure P2 repair requires self-run evidence for every declared affected case")
	}
	return nil
}
