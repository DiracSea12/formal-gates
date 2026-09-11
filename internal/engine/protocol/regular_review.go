package protocol

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/requirements"
	"strings"
)

var reviewActions = []string{"product-review", "start-readiness"}

type PreDevelopmentReviewSeries struct {
	Action             string                    `json:"action"`
	Completed          int                       `json:"completed"`
	Sequence           int                       `json:"sequence"`
	Limit              int                       `json:"limit"`
	ExtraAttempts      map[string]int            `json:"extraAttempts"`
	RawCandidates      []RawReviewCandidate      `json:"rawCandidates"`
	Adjudications      []ReviewAdjudication      `json:"adjudications"`
	Corrections        []ReviewCorrectionReceipt `json:"corrections"`
	CurrentDigest      string                    `json:"currentDigest"`
	CurrentRequirement string                    `json:"currentRequirement"`
	Fresh              bool                      `json:"fresh"`
	Kept               bool                      `json:"kept"`
}
type RawReviewCandidate struct {
	ActionID            string           `json:"actionId"`
	AttemptID           string           `json:"attemptId"`
	Provider            string           `json:"provider"`
	Identity            string           `json:"identity"`
	Action              string           `json:"action"`
	RequirementRevision string           `json:"requirementRevision"`
	Candidate           string           `json:"candidate"`
	Status              string           `json:"status"`
	Message             string           `json:"message"`
	Complete            bool             `json:"complete"`
	Findings            []ReviewFinding  `json:"findings"`
	GranularityReview   *DevelopmentPlan `json:"granularityReview,omitempty"`
	Digest              string           `json:"digest"`
	Quarantined         bool             `json:"quarantined"`
}
type ReviewFinding struct {
	ID                string `json:"id"`
	Severity          string `json:"severity"`
	Category          string `json:"category"`
	Requirement       string `json:"requirement"`
	Problem           string `json:"problem"`
	NormalPath        string `json:"normalPath"`
	Consequence       string `json:"consequence"`
	SeverityRationale string `json:"severityRationale"`
	Remedy            string `json:"remedy"`
}
type FindingAdjudication struct {
	FindingID string `json:"findingId"`
	Decision  string `json:"decision"`
	Severity  string `json:"severity"`
	Category  string `json:"category"`
	Reason    string `json:"reason"`
	Evidence  string `json:"evidence"`
}
type ReviewAdjudication struct {
	Action               string                `json:"action"`
	ResultDigest         string                `json:"resultDigest"`
	Findings             []FindingAdjudication `json:"findings"`
	CompletenessEvidence string                `json:"completenessEvidence"`
	Revision             uint64                `json:"revision"`
}
type ReviewCorrectionReceipt struct {
	Action       string `json:"action"`
	ResultDigest string `json:"resultDigest"`
	FindingID    string `json:"findingId"`
	OldDecision  string `json:"oldDecision"`
	NewDecision  string `json:"newDecision"`
	OldSeverity  string `json:"oldSeverity"`
	NewSeverity  string `json:"newSeverity"`
	Reason       string `json:"reason"`
	Revision     uint64 `json:"revision"`
}
type RejectedReviewResult struct {
	Raw       RawReviewCandidate `json:"raw"`
	ActionID  string             `json:"actionId"`
	AttemptID string             `json:"attemptId"`
	Digest    string             `json:"digest"`
	Reason    string             `json:"reason"`
	Revision  uint64             `json:"revision"`
}

func currentReview(series *PreDevelopmentReviewSeries) *RawReviewCandidate {
	for i := range series.RawCandidates {
		if series.RawCandidates[i].Digest == series.CurrentDigest {
			return &series.RawCandidates[i]
		}
	}
	return nil
}
func findingDecision(series *PreDevelopmentReviewSeries, digest, id string) (string, string) {
	d, sev := "", ""
	for _, a := range series.Adjudications {
		if a.ResultDigest == digest {
			for _, f := range a.Findings {
				if f.FindingID == id {
					d, sev = f.Decision, f.Severity
				}
			}
		}
	}
	for _, c := range series.Corrections {
		if c.ResultDigest == digest && c.FindingID == id {
			d, sev = c.NewDecision, c.NewSeverity
		}
	}
	return d, sev
}
func effectiveReview(r *RegularState, series *PreDevelopmentReviewSeries) string {
	if series.Fresh || series.CurrentDigest == "" || series.CurrentRequirement != r.Requirement.Revision {
		return "OPEN"
	}
	c := currentReview(series)
	if c == nil {
		return "OPEN"
	}
	if series.Kept {
		return "PASS"
	}
	var adj *ReviewAdjudication
	for i := range series.Adjudications {
		if series.Adjudications[i].ResultDigest == c.Digest {
			adj = &series.Adjudications[i]
		}
	}
	if adj == nil {
		return "OPERATOR"
	}
	for _, f := range c.Findings {
		d, _ := findingDecision(series, c.Digest, f.ID)
		if d == "VALID" || d == "CONFIRMED" {
			return "FINDINGS"
		}
	}
	return "PASS"
}
func validSeverity(s string) bool { return s == "P0" || s == "P1" || s == "P2" || s == "P3" }
func validateReview(r *RegularState, v RawReviewCandidate) error {
	if v.Status != "PASS" && v.Status != "FAIL" {
		return fmt.Errorf("review status must be PASS or FAIL")
	}
	if v.Status == "FAIL" && len(v.Findings) == 0 {
		return fmt.Errorf("FAIL review requires findings")
	}
	if !v.Complete || strings.TrimSpace(v.Message) == "" {
		return fmt.Errorf("review must be complete and include a message")
	}
	seen := map[string]bool{}
	for _, f := range v.Findings {
		if f.ID == "" || seen[f.ID] || !validSeverity(f.Severity) {
			return fmt.Errorf("each finding needs a unique ID and P0-P3 severity")
		}
		seen[f.ID] = true
		if strings.TrimSpace(f.Problem) == "" || strings.TrimSpace(f.NormalPath) == "" || strings.TrimSpace(f.Consequence) == "" || strings.TrimSpace(f.SeverityRationale) == "" || strings.TrimSpace(f.Remedy) == "" {
			return fmt.Errorf("finding %s requires problem, normalPath, consequence, severityRationale and remedy", f.ID)
		}
		if v.Action == "start-readiness" {
			switch f.Category {
			case "requirement-conflict", "responsibility-dependency", "acceptance-entry", "implementation-detail":
			default:
				return fmt.Errorf("finding %s: technical category is required", f.ID)
			}
			if (f.Severity == "P0" || f.Severity == "P1") && f.Category == "implementation-detail" {
				return fmt.Errorf("finding %s: implementation-detail is at most P2", f.ID)
			}
		}
	}
	blocking := false
	for _, f := range v.Findings {
		blocking = blocking || f.Severity == "P0" || f.Severity == "P1"
	}
	if (v.Status == "FAIL") != blocking {
		return fmt.Errorf("review status must be FAIL exactly when findings contain P0/P1; P2/P3 alone require PASS")
	}
	if v.Action == "start-readiness" {
		if v.GranularityReview == nil {
			return fmt.Errorf("technical review requires granularityReview")
		}
		if err := validateDevelopmentPlan(*v.GranularityReview, v.RequirementRevision); err != nil {
			return err
		}
	}
	return nil
}
func validateDevelopmentPlan(p DevelopmentPlan, req string) error {
	if len(p.Batches) == 0 || p.Rationale == "" || p.RequirementRevision != req {
		return fmt.Errorf("granularityReview needs batches, rationale and the current requirementRevision")
	}
	seen := map[string]bool{}
	subtasks := map[string]bool{}
	for _, b := range p.Batches {
		if b.ID == "" || seen[b.ID] || len(b.Subtasks) == 0 {
			return fmt.Errorf("batch IDs must be unique and each batch needs ordered subtasks")
		}
		seen[b.ID] = true
		for _, field := range []string{b.Task, b.Interfaces, b.StateBoundary, b.Risks, b.DefinitionOfDone, b.Validation, b.Recovery, b.Handoff, b.ContextCost} {
			if strings.TrimSpace(field) == "" {
				return fmt.Errorf("batch %s: task, interfaces, stateBoundary, risks, definitionOfDone, validation, recovery, handoff and contextCost are required", b.ID)
			}
		}
		for _, st := range b.Subtasks {
			if st.ID == "" || subtasks[st.ID] || strings.TrimSpace(st.Task) == "" {
				return fmt.Errorf("subtask IDs must be unique with a task")
			}
			subtasks[st.ID] = true
		}
	}
	done := map[string]bool{}
	for len(done) < len(p.Batches) {
		progress := false
		for _, b := range p.Batches {
			if done[b.ID] {
				continue
			}
			ready := true
			for _, dep := range b.Dependencies {
				if !seen[dep] {
					return fmt.Errorf("batch %s: dependency %s is unknown", b.ID, dep)
				}
				if !done[dep] {
					ready = false
				}
			}
			if ready {
				done[b.ID] = true
				progress = true
			}
		}
		if !progress {
			return fmt.Errorf("development plan contains a dependency cycle")
		}
	}
	return nil
}
func (e *Engine) acceptRegularReview(s *State, v RawReviewCandidate, revision uint64) (string, error) {
	r := s.Regular
	x, ok := s.RegularExecutions[v.ActionID]
	if !ok || !x.Claimed {
		return "", fmt.Errorf("UNKNOWN_ACTION: review requires a claimed action")
	}
	if x.Finished {
		return "", fmt.Errorf("RECEIPT_CONFLICT: attempt already ended")
	}
	if v.AttemptID != x.Claim.AttemptID || (v.Identity == "" && v.Status != "RUNTIME_ERROR") || v.Identity != x.Identity {
		return "", fmt.Errorf("INTENT_MISMATCH: result does not identify this claimed native attempt")
	}
	// Once action/attempt/native identity establish the returning execution, an
	// invalid model-provided binding is a rejected result of that finite attempt.
	// It must not remain live forever or become an accepted semantic review.
	wrongBinding := v.Provider != x.Claim.Provider || v.Action != string(x.Claim.Task.Step) || v.RequirementRevision != x.Claim.RequirementRevision || v.Candidate != x.Claim.Candidate
	v.Digest = ""
	v.Digest = digestOfCanonical(v)
	x.Finished = true
	delete(s.PendingActions, v.ActionID)
	s.RegularExecutions[v.ActionID] = x
	at := s.Attempts[x.Claim.Task]
	s.Tasks[x.Claim.Task] = runtime.TaskTerminal
	reject := validateReview(r, v)
	if wrongBinding {
		reject = fmt.Errorf("INTENT_MISMATCH: raw result provider/action/requirement/candidate differs from claimed task")
	}
	if v.Status == "RUNTIME_ERROR" {
		reject = fmt.Errorf("RUNTIME_ERROR: %s", v.Message)
	}
	if reject != nil {
		r.RejectedResults = append(r.RejectedResults, RejectedReviewResult{Raw: v, ActionID: v.ActionID, AttemptID: v.AttemptID, Digest: v.Digest, Reason: reject.Error(), Revision: revision + 1})
		max := 3 + r.Reviews[string(x.Claim.Task.Step)].ExtraAttempts[x.Claim.Task.String()]
		at.Attempts = x.Number
		at.RetryExhausted = x.Number >= max
		s.Attempts[x.Claim.Task] = at
		return "REJECTED_RESULT", nil
	}
	s.Results[v.ActionID] = WorkerResult{ActionID: v.ActionID, Provider: v.Provider, Outcome: v.Status, PayloadDigest: v.Digest, Digest: v.Digest}
	series := r.Reviews[v.Action]
	if r.ChangePending || v.RequirementRevision != r.Requirement.Revision || v.Candidate != r.Candidate || (v.Action == "start-readiness" && (effectiveReview(r, r.Reviews["product-review"]) != "PASS" || x.Claim.PrerequisiteDigest != r.Reviews["product-review"].CurrentDigest)) {
		v.Quarantined = true
		series.RawCandidates = append(series.RawCandidates, v)
		return "QUARANTINED", nil
	}
	series.RawCandidates = append(series.RawCandidates, v)
	series.CurrentDigest = v.Digest
	series.CurrentRequirement = v.RequirementRevision
	series.Completed++
	series.Sequence++
	series.Fresh = false
	series.Kept = false
	if v.GranularityReview != nil {
		p := *v.GranularityReview
		p.Digest = ""
		p.ConfirmedRevision = ""
		p.Digest = digestOfCanonical(p)
		r.Plan = &p
	}
	return "ACCEPTED", nil
}
func adjudicateRegularReview(r *RegularState, a ReviewAdjudication, revision uint64) error {
	series := r.Reviews[a.Action]
	if series == nil {
		return fmt.Errorf("unknown review action")
	}
	c := currentReview(series)
	if c == nil || c.Digest != a.ResultDigest || c.RequirementRevision != r.Requirement.Revision || a.CompletenessEvidence == "" {
		return fmt.Errorf("adjudication requires current result digest and completeness evidence")
	}
	for _, old := range series.Adjudications {
		if old.ResultDigest == a.ResultDigest {
			return fmt.Errorf("result already adjudicated; use a bound correction")
		}
	}
	if len(a.Findings) != len(c.Findings) {
		return fmt.Errorf("adjudication must cover every finding exactly once")
	}
	seen := map[string]bool{}
	for _, f := range a.Findings {
		var raw *ReviewFinding
		for i := range c.Findings {
			if c.Findings[i].ID == f.FindingID {
				raw = &c.Findings[i]
			}
		}
		if raw == nil || seen[f.FindingID] || (f.Decision != "VALID" && f.Decision != "DISMISSED") || !validSeverity(f.Severity) || f.Evidence == "" || f.Reason == "" {
			return fmt.Errorf("finding %s needs a unique raw binding, VALID/DISMISSED, severity, reason and normal-path evidence", f.FindingID)
		}
		seen[f.FindingID] = true
		if a.Action == "start-readiness" && (f.Category != raw.Category || ((f.Severity == "P0" || f.Severity == "P1") && f.Category == "implementation-detail")) {
			return fmt.Errorf("technical classification is inconsistent")
		}
	}
	a.Revision = revision + 1
	series.Adjudications = append(series.Adjudications, a)
	return nil
}
func correctRegularReview(r *RegularState, c ReviewCorrectionReceipt, revision uint64) error {
	series := r.Reviews[c.Action]
	if series == nil || c.ResultDigest != series.CurrentDigest || strings.TrimSpace(c.Reason) == "" {
		return fmt.Errorf("correction requires current result digest and a reason")
	}
	old, sev := findingDecision(series, c.ResultDigest, c.FindingID)
	if old == "" || old != c.OldDecision || sev != c.OldSeverity {
		return fmt.Errorf("correction conflicts with effective old values")
	}
	if !validSeverity(c.NewSeverity) {
		return fmt.Errorf("correction severity must be P0-P3")
	}
	switch c.NewDecision {
	case "VALID", "DISMISSED", "RESOLVED":
	default:
		return fmt.Errorf("invalid corrected decision")
	}
	raw := currentReview(series)
	for _, f := range raw.Findings {
		if f.ID == c.FindingID && c.Action == "start-readiness" && f.Category == "implementation-detail" && (c.NewSeverity == "P0" || c.NewSeverity == "P1") {
			return fmt.Errorf("implementation-detail is at most P2")
		}
	}
	c.Revision = revision + 1
	series.Corrections = append(series.Corrections, c)
	if c.NewDecision == "VALID" {
		series.Kept = false
		invalidateAfterReview(r, c.Action)
	}
	return nil
}
func invalidateAfterReview(r *RegularState, action string) {
	if action == "product-review" {
		r.Reviews["start-readiness"].Fresh = true
	}
	r.Topology = ""
	if r.Plan != nil {
		r.Plan.ConfirmedRevision = ""
	}
	r.Guarantee.Status = "INACTIVE"
}

func (e *Engine) regularRequest(s *State, id string, q RegularRequest, revision uint64) error {
	r := s.Regular
	if r.Ask != nil {
		if q.Action != "requirement-change" && q.Action != "abort" {
			return fmt.Errorf("REQUEST_ALREADY_PENDING: resolve the current request first")
		}
		// A user change may arrive while any earlier confirmation is pending.
		// Preserve and supersede that Ask instead of leaving an obsolete token
		// between the user and the required Phase A isolation/abort path.
		old := r.Ask
		s.Decisions[old.ID] = RecordedDecision{RequestID: old.ID, Control: ControlKind(old.Request.Action), Choice: AskOptionID("superseded"), EventID: id, Revision: revision + 1, Reason: q.Reason}
		r.Requests = append(r.Requests, *old)
		r.Ask = nil
	}
	if q.Action != "confirm-requirement" && q.Action != "mode" && q.Action != "topology" && q.Action != "route" && strings.TrimSpace(q.Reason) == "" {
		return fmt.Errorf("request %s requires a reason", q.Action)
	}
	switch q.Action {
	case "verification-retry", "authorize-wave":
		if err := verificationRequest(s, q); err != nil {
			return err
		}
	case "plan-change", "qa-suggestion", "qa-revise", "development-rework", "preparation-retry":
		if err := preparationRequest(s, q); err != nil {
			return err
		}
	case "confirm-requirement":
		if len(q.RequiredSources) == 0 {
			q.RequiredSources = []coverage.RequiredSource{{SourceID: r.Requirement.Path, Category: coverage.ProductRequirement, Applicability: coverage.QAApplicability}}
		}
		sources := coverage.RequiredSources{Binding: coverage.SourceBinding{RunID: s.RunID, RequirementRevision: r.Requirement.Revision, SolutionRevision: r.Requirement.Revision}, Sources: q.RequiredSources}
		if err := coverage.ValidateRequiredSources(sources); err != nil {
			return err
		}
		if r.Requirement.Projection == nil || r.Requirement.Confirmed {
			return fmt.Errorf("requirement must be prechecked and not already confirmed")
		}
	case "mode":
		if q.Mode == "regular" && s.RunProvider != "codex" {
			return fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: regular requires the codex provider")
		}
		if !r.Requirement.Confirmed || r.Mode != "" || (q.Mode != "regular" && q.Mode != "lightweight") {
			return fmt.Errorf("mode requires confirmed intake and an initial regular/lightweight choice")
		}
	case "topology":
		if effectiveReview(r, r.Reviews["product-review"]) != "PASS" || effectiveReview(r, r.Reviews["start-readiness"]) != "PASS" || r.Plan == nil || r.Plan.Digest != q.PlanDigest || q.Reason == "" {
			return fmt.Errorf("topology requires both reviews PASS, current plan digest and no-split reason")
		}
	case "route", "route-add", "route-reselect":
		if err := validateRouteChangeAfterDevelopment(s, q); err != nil {
			return err
		}
		if r.Mode != "regular" || r.Topology != "no-split" || r.Plan == nil || r.Plan.ConfirmedRevision != r.Requirement.Revision || effectiveReview(r, r.Reviews["start-readiness"]) != "PASS" {
			return fmt.Errorf("route requires regular, technical PASS and confirmed no-split plan")
		}
		if err := validateRegularRoute(q.Route, r.CatalogGates); err != nil {
			return err
		}
		if q.Action == "route" && r.Route != nil {
			return fmt.Errorf("route already bound; request route-add or route-reselect")
		}
	case "finding":
		series := r.Reviews[q.ReviewAction]
		if series == nil || series.CurrentDigest != q.ResultDigest {
			return fmt.Errorf("finding must bind current review result")
		}
		d, _ := findingDecision(series, q.ResultDigest, q.FindingID)
		if d != "VALID" {
			return fmt.Errorf("finding must have a valid operator adjudication")
		}
	case "review-again", "authorize-round", "authorize-retry":
		series := r.Reviews[q.ReviewAction]
		if series == nil {
			return fmt.Errorf("request must name product-review or start-readiness")
		}
		if q.Action == "review-again" && series.Completed >= series.Limit {
			return fmt.Errorf("ROUND_EXHAUSTED: authorize exactly one action-specific round first")
		}
		if q.Action == "authorize-round" && series.Completed < series.Limit {
			return fmt.Errorf("extra round authorization is only available after the current limit")
		}
		if q.Action == "authorize-retry" {
			at, ok := s.Attempts[reviewTask(r, q.ReviewAction)]
			if !ok || !at.RetryExhausted {
				return fmt.Errorf("retry authorization requires exhausted current TaskKey")
			}
		}
	case "continue":
		if r.Guard.Status != "TOKEN_CAP" {
			return fmt.Errorf("continue is available only for the current token-cap stop")
		}
	case "requirement-change":
		if q.Origin != "USER_INITIATED_CHANGE" && q.Origin != "REVIEW_FINDING" {
			return fmt.Errorf("requirement-change origin must be USER_INITIATED_CHANGE or REVIEW_FINDING")
		}
		r.ChangePending = true
		invalidateVerification(s, q.Action)
		r.Guard.ContinueTasks = nil
	case "apply-requirement":
		if !r.ChangePending {
			return fmt.Errorf("request requirement-change before applying its candidate")
		}
		if err := validateRequirementChange(r, q); err != nil {
			return err
		}
	case "jump":
		if q.Target != "product-review" && q.Target != "start-readiness" {
			return fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: B1 jump target must be a pre-development review")
		}
		if r.Mode != "regular" || (q.Target == "start-readiness" && effectiveReview(r, r.Reviews["product-review"]) != "PASS") {
			return fmt.Errorf("jump cannot skip an unmet prerequisite")
		}
		for _, x := range s.RegularExecutions {
			if !x.Finished {
				return fmt.Errorf("jump requires current attempts to finish and settle")
			}
		}
		if r.Guard.Status == "COST_PENDING" {
			return fmt.Errorf("jump requires source reconciliation")
		}
	case "adopt-external":
		if q.External == nil || q.External.Identity == "" || q.External.Identity == r.Candidate || q.External.Lineage != r.BaseSnapshot || q.External.DiffDigest == "" {
			return fmt.Errorf("adopt-external requires exact identity, original lineage and diff")
		}
		for _, action := range reviewActions {
			if q.Impact[action] != "affected" && q.Impact[action] != "unaffected" && q.Impact[action] != "unknown" {
				return fmt.Errorf("adopt-external requires explicit impact for %s", action)
			}
		}
		for _, action := range q.Keep {
			if (r.Reviews[action] == nil || effectiveReview(r, r.Reviews[action]) != "PASS") && !verificationKeep(r, action) {
				return fmt.Errorf("adopt-external keep requires existing PASS")
			}
		}
		r.ChangePending = true
		invalidateVerification(s, q.Action)
		r.Guard.ContinueTasks = nil
	case "abort", "reset":
	default:
		return fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: request %q is not available in the regular boundary", q.Action)
	}
	options := []string{"confirm", "cancel"}
	if q.Action == "authorize-wave" {
		ids := openWaveProblems(r)
		if len(ids) == 0 {
			return fmt.Errorf("extra wave requires remaining concrete obligations")
		}
		if len(q.Keep) > 0 && !equalStrings(q.Keep, ids) {
			return fmt.Errorf("wave disposition must bind exact current obligation IDs")
		}
		q.Keep = ids
		q.Target = currentWave(r).ID
		options = []string{"additional-wave", "retry-qa", "waiver", "change-requirement", "abort", "cancel"}
	}
	if q.Action == "finding" {
		options = []string{"confirm", "dismiss", "clarify"}
	}
	ask := &RegularAsk{ID: id, Kind: q.Action, Revision: revision + 1, Request: q, Options: options}
	if q.Action == "authorize-wave" || q.Action == "apply-requirement" || q.Action == "adopt-external" || q.Action == "reset" {
		ask.Preview = &RegularPreview{StateRevision: revision + 1, RequirementRevision: r.Requirement.Revision, BaseSnapshot: r.BaseSnapshot, Candidate: r.Candidate, Reviews: r.Reviews, Verification: r.Verification, Guard: r.Guard, Cost: s.Cost, Consequences: "Preserve original evidence, cost, attempts, base and review counts. Apply only explicit keep/unaffected classifications; invalidate other dependent results and reconcile side effects. Reset additionally clears current plan, topology, route and guarantee. An additional-wave decision adds exactly one semantic wave to the current exhausted series."}
	}
	if ask.Preview != nil {
		data, err := json.Marshal(ask.Preview)
		if err != nil {
			return err
		}
		var frozen RegularPreview
		if err = json.Unmarshal(data, &frozen); err != nil {
			return err
		}
		ask.Preview = &frozen
	}
	ask.Digest = digestOfCanonical(struct {
		Run       string
		Req       string
		Base      string
		Candidate string
		Revision  uint64
		Request   RegularRequest
		Preview   *RegularPreview
	}{s.RunID, r.Requirement.Revision, r.BaseSnapshot, r.Candidate, revision + 1, q, ask.Preview})
	ask.Token = freshnessToken(revision+1, RequestID(id))
	r.Ask = ask
	return nil
}
func validateRegularRoute(route *RegularRoute, catalog []string) error {
	if route == nil || (route.Kind != "full" && route.Kind != "custom") {
		return fmt.Errorf("route must be full or custom")
	}
	for _, mode := range route.QA {
		if mode != "blackbox" && mode != "whitebox" {
			return fmt.Errorf("unsupported QA mode %q", mode)
		}
	}
	for _, gate := range route.Gates {
		found := false
		for _, id := range catalog {
			if id == gate {
				found = true
			}
		}
		if !found {
			return fmt.Errorf("unsupported gate %q; select an ID from this run's catalogGates", gate)
		}
	}
	if route.Kind == "custom" && len(route.QA)+len(route.Gates) == 0 {
		return fmt.Errorf("custom route must select QA or gates")
	}
	return nil
}
func (e *Engine) regularDecision(s *State, d RegularDecision, revision uint64, eventID string) error {
	r := s.Regular
	a := r.Ask
	if a == nil || a.ID != d.RequestID {
		return fmt.Errorf("UNKNOWN_REQUEST: no current matching request")
	}
	if d.Token != freshnessToken(revision, RequestID(a.ID)) || d.Digest != a.Digest {
		return fmt.Errorf("STALE_FRESHNESS: use current request token and digest")
	}
	valid := false
	for _, choice := range a.Options {
		if choice == d.Choice {
			valid = true
		}
	}
	if !valid {
		return fmt.Errorf("INVALID_CHOICE")
	}
	q := a.Request
	if q.Action == "qa-recovery" {
		if d.Choice == "abort" {
			if strings.TrimSpace(d.Reason) == "" {
				return fmt.Errorf("QA abort requires reason")
			}
			q = RegularRequest{Action: "abort", Reason: d.Reason}
		} else if err := decideQARecovery(s, a, d, revision+1); err != nil {
			return err
		}
	}
	if d.Choice != "cancel" && a.Preview != nil && a.Preview.StateRevision != revision {
		return fmt.Errorf("STALE_FRESHNESS: input/cost changed after preview; request a new bounded preview")
	}
	if d.Choice == "cancel" {
		if q.Action == "requirement-change" || q.Action == "adopt-external" {
			r.ChangePending = false
		}
		r.Requests = append(r.Requests, *a)
		s.Decisions[a.ID] = RecordedDecision{RequestID: a.ID, Control: ControlKind(q.Action), Choice: AskOptionID(d.Choice), EventID: eventID, Revision: revision + 1, Reason: q.Reason}
		r.Requests = append(r.Requests, *a)
		r.Ask = nil
		return nil
	}
	if q.Action == "authorize-wave" && d.Choice == "abort" {
		if strings.TrimSpace(d.Reason) == "" {
			return fmt.Errorf("wave abort requires reason")
		}
		q = RegularRequest{Action: "abort", Reason: d.Reason}
	}
	switch q.Action {
	case "authorize-wave":
		if err := decideWaveRecovery(s, a, d, revision+1); err != nil {
			return err
		}
	case "qa-recovery":
		// Applied above against the frozen exact QA binding.
	case "verification-retry":
		verificationDecision(s, q)
	case "plan-change", "qa-suggestion", "qa-revise", "development-rework", "preparation-retry":
		preparationDecision(s, q)
	case "confirm-requirement":
		r.Requirement.Confirmed = true
		r.RequiredSources = coverage.RequiredSources{Binding: coverage.SourceBinding{RunID: s.RunID, RequirementRevision: r.Requirement.Revision, SolutionRevision: r.Requirement.Revision}, Sources: q.RequiredSources}
	case "mode":
		r.Mode = q.Mode
		if q.Mode == "lightweight" {
			s.Route = "lightweight"
			r.Terminal = "UNVERIFIED"
		}
	case "topology":
		r.Topology = "no-split"
		r.TopologyReason = q.Reason
		r.Plan.ConfirmedRevision = r.Requirement.Revision
		r.RequiredSources.Binding.SolutionRevision = r.Plan.Digest
		if r.Route != nil {
			r.Guarantee.RequirementRevision = r.Requirement.Revision
			r.Guarantee.ProjectionDigest = digestOfCanonical(r.Requirement.Projection)
			if len(r.Route.QA) > 0 {
				r.Guarantee.Status = "ACTIVE"
			}
		}
	case "route", "route-add", "route-reselect":
		oldBinding := preparationBinding(r)
		route := *q.Route
		if route.Kind == "full" {
			route.QA = []string{"blackbox", "whitebox"}
			route.Gates = append([]string(nil), r.CatalogGates...)
		}
		if q.Action == "route-add" && r.Route != nil {
			route.QA = uniqueAppend(r.Route.QA, route.QA...)
			route.Gates = uniqueAppend(r.Route.Gates, route.Gates...)
		}
		r.Route = &route
		if q.Action == "route-add" {
			rebindAddedGates(s, oldBinding)
		}
		s.Route = route.Kind
		r.Guarantee = RequirementGuaranteeActivation{Status: "INACTIVE", Source: "ROUTE_CONFIRMATION", RequirementRevision: r.Requirement.Revision, ProjectionDigest: digestOfCanonical(r.Requirement.Projection)}
		if len(route.QA) > 0 {
			r.Guarantee.Status = "ACTIVE"
		}
	case "finding":
		series := r.Reviews[q.ReviewAction]
		old, sev := findingDecision(series, q.ResultDigest, q.FindingID)
		value := "CONFIRMED"
		if d.Choice == "dismiss" {
			value = "DISMISSED"
		}
		if d.Choice == "clarify" {
			return fmt.Errorf("CLARIFICATION_REQUIRED: provide a complete remedy before confirming")
		}
		series.Corrections = append(series.Corrections, ReviewCorrectionReceipt{Action: q.ReviewAction, ResultDigest: q.ResultDigest, FindingID: q.FindingID, OldDecision: old, NewDecision: value, OldSeverity: sev, NewSeverity: sev, Reason: q.Reason, Revision: revision + 1})
	case "review-again", "authorize-round":
		series := r.Reviews[q.ReviewAction]
		if q.Action == "authorize-round" {
			series.Limit++
		}
		series.Fresh = true
		invalidateAfterReview(r, q.ReviewAction)
	case "authorize-retry":
		series := r.Reviews[q.ReviewAction]
		key := reviewTask(r, q.ReviewAction)
		if series.ExtraAttempts == nil {
			series.ExtraAttempts = map[string]int{}
		}
		series.ExtraAttempts[key.String()]++
		at := s.Attempts[key]
		at.RetryExhausted = false
		s.Attempts[key] = at
	case "continue":
		key := reviewTask(r, "product-review")
		if effectiveReview(r, r.Reviews["product-review"]) == "PASS" {
			key = reviewTask(r, "start-readiness")
		}
		r.Guard.ContinueTasks = []runtime.TaskKey{key}
		if effectiveReview(r, r.Reviews["start-readiness"]) == "PASS" && r.Preparation != nil {
			r.Guard.ContinueTasks = sortedPreparationTasks(s)
			for _, x := range s.RegularExecutions {
				if !x.Finished {
					r.Guard.ContinueTasks = append(r.Guard.ContinueTasks, x.Claim.Task)
				}
			}
			if len(r.Guard.ContinueTasks) > r.Settings.Values.Capacity {
				r.Guard.ContinueTasks = r.Guard.ContinueTasks[:r.Settings.Values.Capacity]
			}
		}
	case "requirement-change":
		r.ChangePending = true
	case "apply-requirement":
		classifyVerificationChange(r, q, revision+1)
		applyRequirementChange(r, q, revision+1, s.RunID)
	case "jump":
		r.Reviews[q.Target].Fresh = true
		invalidateAfterReview(r, q.Target)
	case "adopt-external":
		classifyVerificationChange(r, q, revision+1)
		for _, action := range reviewActions {
			series := r.Reviews[action]
			keep := false
			for _, selected := range q.Keep {
				if action == selected {
					keep = true
				}
			}
			if keep {
				series.Kept = true
			} else if q.Impact[action] != "unaffected" {
				series.Fresh = true
			}
		}
		change := RequirementChangeReceipt{FromRevision: r.Requirement.Revision, ToRevision: r.Requirement.Revision, Candidate: q.External.Identity, StateRevision: revision + 1, Meaning: "external", Origin: "EXTERNAL_ADOPTION", Impact: q.Impact, Reason: q.Reason, DeltaDigest: q.External.DiffDigest}
		for _, mode := range []string{"blackbox", "whitebox"} {
			name := mode + "-review"
			keep := q.Impact[name] == "unaffected"
			for _, selected := range q.Keep {
				keep = keep || selected == name
			}
			if keep && verificationKeep(r, name) {
				qa := r.Preparation.QA[mode]
				change.Keep = append(change.Keep, KeptReview{Action: name, ResultDigest: digestOfCanonical(qa.Review), FromRequirement: r.Requirement.Revision, FromCandidate: qaReviewCandidate(r, mode), ToRequirement: r.Requirement.Revision, ToCandidate: q.External.Identity})
			}
		}
		r.Changes = append(r.Changes, change)
		r.Candidate = q.External.Identity
		r.ChangePending = false
		r.Topology = ""
		if r.Plan != nil {
			r.Plan.ConfirmedRevision = ""
		}
		r.Guarantee.Status = "INACTIVE"
	case "abort":
		if r.Preparation != nil && r.Preparation.Operation != nil {
			delete(s.PendingHostActions, r.Preparation.Operation.ID)
			r.Preparation.Operation = nil
		}
		r.Terminal = "ABORTING"
		r.ChangePending = true
		invalidateVerification(s, q.Action)
		r.Guard.ContinueTasks = nil
		for id, x := range s.RegularExecutions {
			if !x.Claimed {
				x.Finished = true
				delete(s.PendingActions, id)
				s.RegularExecutions[id] = x
				continue
			}
			if !x.Finished && x.Identity != "" {
				target := &AgentHostAction{WorkerActionID: id, AttemptID: x.Claim.AttemptID, Identity: x.Identity, Reason: q.Reason}
				intent := HostActionIntent{ActionID: "hact:" + id + ":terminate", Operation: HostActionTerminateAgent, Step: x.Claim.Task.Step, Terminate: target, PayloadDigest: digestOfCanonical(target), Correlation: x.Identity, Revision: revision + 1}
				s.PendingHostActions[intent.ActionID] = intent
			}
		}
	case "reset":
		if r.Preparation != nil && r.Preparation.Operation != nil {
			return fmt.Errorf("reset requires Git operation reconciliation")
		}
		for _, x := range s.RegularExecutions {
			if x.Claimed && !x.Finished {
				return fmt.Errorf("reset requires running execution reconciliation")
			}
		}
		for _, series := range r.Reviews {
			series.Fresh = true
		}
		r.Topology = ""
		r.Plan = nil
		if r.Preparation != nil {
			r.PreparationHistory = append(r.PreparationHistory, *r.Preparation)
			r.Preparation = nil
		}
		r.Route = nil
		r.Guarantee.Status = "INACTIVE"
	}
	s.Decisions[a.ID] = RecordedDecision{RequestID: a.ID, Control: ControlKind(q.Action), Choice: AskOptionID(d.Choice), EventID: eventID, Revision: revision + 1, Reason: q.Reason}
	r.Requests = append(r.Requests, *a)
	r.Ask = nil
	return nil
}
func uniqueAppend(out []string, items ...string) []string {
	out = append([]string(nil), out...)
	for _, item := range items {
		found := false
		for _, v := range out {
			if v == item {
				found = true
			}
		}
		if !found {
			out = append(out, item)
		}
	}
	return out
}
func validateRequirementChange(r *RegularState, q RegularRequest) error {
	if q.Document == nil || q.Document.Path != r.Requirement.Path || q.Document.Revision == r.Requirement.Revision {
		return fmt.Errorf("requirement candidate must revise the same bound file")
	}
	want := Document(q.Document.Path, []byte(q.Document.Text))
	if want.Revision != q.Document.Revision {
		return fmt.Errorf("requirement candidate digest mismatch")
	}
	if q.Meaning != "preserved" && q.Meaning != "changed" {
		return fmt.Errorf("meaning must be preserved or changed")
	}
	if q.Origin != "USER_INITIATED_CHANGE" && q.Origin != "REVIEW_FINDING" {
		return fmt.Errorf("requirement origin is required")
	}
	if _, err := requirements.ParseRequirementProjection(q.Document.Path, []byte(q.Document.Text)); err != nil {
		return err
	}
	for _, action := range reviewActions {
		switch q.Impact[action] {
		case "unaffected", "affected", "unknown":
		default:
			return fmt.Errorf("impact must explicitly classify %s", action)
		}
	}
	for _, action := range q.Keep {
		series := r.Reviews[action]
		if (series == nil || effectiveReview(r, series) != "PASS") && !verificationKeep(r, action) {
			return fmt.Errorf("keep %q requires an existing effective PASS", action)
		}
	}
	if q.Origin == "REVIEW_FINDING" {
		// Each replacement is ordered against the complete bound base; the composed
		// result must equal the complete candidate, proving no changed bytes omitted.
		text := r.Requirement.Text
		if len(q.Delta) == 0 {
			return fmt.Errorf("review finding revision requires exhaustive review-delta")
		}
		for _, h := range q.Delta {
			if h.Old == "" || strings.Count(text, h.Old) != 1 || h.Source == "" || h.Reason == "" {
				return fmt.Errorf("each delta hunk needs a unique base span, source and verification reason")
			}
			matched := false
			for action, series := range r.Reviews {
				if raw := currentReview(series); raw != nil {
					for _, f := range raw.Findings {
						d, _ := findingDecision(series, series.CurrentDigest, f.ID)
						if h.Source == action+":"+series.CurrentDigest+":"+f.ID && d == "CONFIRMED" {
							matched = true
						}
					}
				}
			}
			if !matched {
				return fmt.Errorf("delta source must bind action:resultDigest:findingId of a user-confirmed finding")
			}
			text = strings.Replace(text, h.Old, h.New, 1)
		}
		if text != q.Document.Text {
			return fmt.Errorf("review-delta does not reproduce the complete candidate from the bound base")
		}
	}
	return nil
}
func applyRequirementChange(r *RegularState, q RegularRequest, revision uint64, runID string) {
	old := r.Requirement
	doc := *q.Document
	p, _ := requirements.ParseRequirementProjection(doc.Path, []byte(doc.Text))
	doc.Projection = &p
	doc.Confirmed = true
	receipt := RequirementChangeReceipt{FromRevision: old.Revision, ToRevision: doc.Revision, Candidate: r.Candidate, StateRevision: revision, Meaning: q.Meaning, Origin: q.Origin, Impact: q.Impact, Reason: q.Reason, DeltaDigest: digestOfCanonical(q.Delta)}
	for _, action := range reviewActions {
		series := r.Reviews[action]
		keep := false
		for _, selected := range q.Keep {
			if selected == action {
				keep = true
			}
		}
		settledMinor := false
		if q.Origin == "REVIEW_FINDING" && currentReview(series) != nil {
			minorFound, blocked := false, false
			for _, f := range currentReview(series).Findings {
				d, sev := findingDecision(series, series.CurrentDigest, f.ID)
				if d == "VALID" || (d == "CONFIRMED" && (sev == "P0" || sev == "P1")) {
					blocked = true
				}
				if d == "CONFIRMED" && (sev == "P2" || sev == "P3") {
					minorFound = true
				}
			}
			settledMinor = minorFound && !blocked
			if settledMinor {
				for _, f := range currentReview(series).Findings {
					d, sev := findingDecision(series, series.CurrentDigest, f.ID)
					if d == "CONFIRMED" {
						series.Corrections = append(series.Corrections, ReviewCorrectionReceipt{Action: action, ResultDigest: series.CurrentDigest, FindingID: f.ID, OldDecision: d, NewDecision: "RESOLVED", OldSeverity: sev, NewSeverity: sev, Reason: "Confirmed remedy applied in exhaustive review-delta " + digestOfCanonical(q.Delta), Revision: revision})
					}
				}
			}
		}
		implicit := q.Impact[action] == "unaffected" && q.Origin == "USER_INITIATED_CHANGE"
		if action == "product-review" && q.Meaning == "preserved" {
			implicit = true
		}
		if (keep || implicit || settledMinor) && effectiveReview(r, series) == "PASS" {
			receipt.Keep = append(receipt.Keep, KeptReview{Action: action, ResultDigest: series.CurrentDigest, FromRequirement: series.CurrentRequirement, FromCandidate: r.Candidate, ToRequirement: doc.Revision, ToCandidate: r.Candidate})
			series.CurrentRequirement = doc.Revision
			series.Kept = keep
			series.Fresh = false
		} else {
			series.Fresh = true
		}
	}
	for _, name := range q.Keep {
		if strings.HasSuffix(name, "-review") && verificationKeep(r, name) {
			mode := strings.TrimSuffix(name, "-review")
			receipt.Keep = append(receipt.Keep, KeptReview{Action: name, ResultDigest: digestOfCanonical(r.Preparation.QA[mode].Review), FromRequirement: old.Revision, FromCandidate: qaReviewCandidate(r, mode), ToRequirement: doc.Revision, ToCandidate: r.Candidate})
		}
	}
	r.Requirement = doc
	if len(r.RequiredSources.Sources) == 0 {
		r.RequiredSources = coverage.RequiredSources{Binding: coverage.SourceBinding{RunID: runID}, Sources: []coverage.RequiredSource{{SourceID: doc.Path, Category: coverage.ProductRequirement, Applicability: coverage.QAApplicability}}}
	}
	r.RequiredSources.Binding.RequirementRevision = doc.Revision
	r.RequiredSources.Binding.SolutionRevision = doc.Revision
	r.Changes = append(r.Changes, receipt)
	r.ChangePending = false
	r.Topology = ""
	if r.Plan != nil {
		r.Plan.ConfirmedRevision = ""
	}
	if r.Plan != nil && effectiveReview(r, r.Reviews["start-readiness"]) == "PASS" {
		r.Plan.RequirementRevision = doc.Revision
		r.Plan.Digest = ""
		r.Plan.Digest = digestOfCanonical(*r.Plan)
	}
	r.Guarantee.Status = "INACTIVE"
}

func qaReviewCandidate(r *RegularState, mode string) string {
	if mode == "blackbox" {
		return r.BaseSnapshot
	}
	return r.Preparation.QA[mode].Snapshot
}
