package protocol

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/guard"
	"formal-gates/internal/engine/runtime"
)

// Preparation is the current plan's projection, not another task lifecycle.
// Attempts and native sessions continue to live in the existing protocol maps.
type Preparation struct {
	BatchTasks          map[string]runtime.TaskKey   `json:"batchTasks,omitempty"`
	DevelopmentTask     *runtime.TaskKey             `json:"developmentTask,omitempty"`
	ReworkReason        string                       `json:"reworkReason,omitempty"`
	PlanDigest          string                       `json:"planDigest"`
	BindingDigest       string                       `json:"bindingDigest"`
	Resources           map[string]string            `json:"resources"`
	Batches             map[string]PreparationResult `json:"batches"`
	Commits             map[string]gitprep.Receipt   `json:"commits"`
	DevelopmentSnapshot string                       `json:"developmentSnapshot"`
	QA                  map[string]*PreparedQA       `json:"qa"`
	Operation           *GitPreparation              `json:"operation,omitempty"`
	Receipts            []GitPreparationReceipt      `json:"receipts"`
	Candidate           *PreparedCandidate           `json:"candidate,omitempty"`
	Blocked             string                       `json:"blocked,omitempty"`
	Rework              int                          `json:"rework"`
	ExtraAttempts       map[string]int               `json:"extraAttempts,omitempty"`
}

// QARecoveryBinding freezes the pre-wave action and exact reviewed inputs. Runtime
// attempts and semantic failures remain independent; this is an Ask payload only.
type QARecoveryBinding struct {
	Mode                string          `json:"mode"`
	Task                runtime.TaskKey `json:"task"`
	RequirementRevision string          `json:"requirementRevision"`
	Candidate           string          `json:"candidate"`
	CaseSetDigest       string          `json:"caseSetDigest"`
	ResultDigest        string          `json:"resultDigest"`
	ConsecutiveFailures int             `json:"consecutiveFailures"`
	RetryExhausted      bool            `json:"retryExhausted"`
}
type QAReviewWaiver struct {
	Binding  QARecoveryBinding `json:"binding"`
	Choice   string            `json:"choice"`
	Reason   string            `json:"reason"`
	Revision uint64            `json:"revision"`
}
type PreparedQA struct {
	DesignActionID         string          `json:"designActionId,omitempty"`
	PreviousDesignActionID string          `json:"previousDesignActionId,omitempty"`
	TestArtifactsFrom      string          `json:"testArtifactsFrom,omitempty"`
	TestArtifactsBase      string          `json:"testArtifactsBase,omitempty"`
	DesignGap              string          `json:"designGap,omitempty"`
	ConsecutiveFailures    int             `json:"consecutiveFailures"`
	RecoveryRequired       bool            `json:"recoveryRequired"`
	ReviewWaiver           *QAReviewWaiver `json:"reviewWaiver,omitempty"`

	ReviewScope    coverage.ScopeMode          `json:"reviewScope"`
	AuthoringBase  string                      `json:"authoringBase,omitempty"`
	PreviousDesign *QADesign                   `json:"previousDesign,omitempty"`
	RetainedReview *coverage.ReviewResult      `json:"retainedReview,omitempty"`
	NeedsRevision  bool                        `json:"needsRevision"`
	Revision       int                         `json:"revision"`
	Design         *QADesign                   `json:"design,omitempty"`
	Review         *coverage.ReviewResult      `json:"review,omitempty"`
	Whitelist      *coverage.ApprovedWhitelist `json:"whitelist,omitempty"`
	Suggestions    []QASuggestion              `json:"suggestions"`
	Frozen         bool                        `json:"frozen"`
	Snapshot       string                      `json:"snapshot"`
	Manifest       []string                    `json:"manifest"`
	Reuse          []TestReuse                 `json:"reuse"`
}
type QADesign struct {
	Replace           bool                               `json:"replace,omitempty"`
	RemoveCases       []string                           `json:"removeCases,omitempty"`
	RemoveSharedSteps []string                           `json:"removeSharedSteps,omitempty"`
	Manifest          coverage.AcceptanceManifest        `json:"manifest"`
	TestFiles         []string                           `json:"testFiles"`
	Alternatives      []coverage.AlternativeVerification `json:"alternatives,omitempty"`
	CoverageJudgment  string                             `json:"coverageJudgment,omitempty"`
}
type QASuggestion struct {
	ID                string   `json:"id"`
	Severity          string   `json:"severity"`
	Problem           string   `json:"problem"`
	Cases             []string `json:"cases"`
	Decision          string   `json:"decision,omitempty"`
	Reason            string   `json:"reason,omitempty"`
	ChangesAcceptance bool     `json:"changesAcceptance"`
}
type TestReuse struct {
	CaseID           string `json:"caseId"`
	TestRef          string `json:"testRef"`
	CoverageJudgment string `json:"coverageJudgment"`
	Procedure        string `json:"procedure"`
	Observation      string `json:"observation"`
	Comparison       string `json:"comparison"`
	Result           string `json:"result"`
}
type BatchValidation struct {
	SubtaskID   string `json:"subtaskId"`
	Procedure   string `json:"procedure"`
	Observation string `json:"observation"`
	Result      string `json:"result"`
}
type PreparationResult struct {
	ActionID            string                 `json:"actionId"`
	AttemptID           string                 `json:"attemptId"`
	Provider            string                 `json:"provider"`
	Identity            string                 `json:"identity"`
	RequirementRevision string                 `json:"requirementRevision"`
	Candidate           string                 `json:"candidate"`
	Status              string                 `json:"status"`
	Message             string                 `json:"message"`
	Manifest            []string               `json:"manifest"`
	Validation          []BatchValidation      `json:"validation"`
	Handoff             string                 `json:"handoff"`
	Design              *QADesign              `json:"design,omitempty"`
	Review              *coverage.ReviewResult `json:"review,omitempty"`
	Suggestions         []QASuggestion         `json:"suggestions,omitempty"`
	Reuse               []TestReuse            `json:"reuse,omitempty"`
}
type GitPreparation struct {
	Revision      uint64   `json:"revision"`
	ID            string   `json:"id"`
	Kind          string   `json:"kind"`
	BindingDigest string   `json:"bindingDigest"`
	Before        string   `json:"before"`
	Target        string   `json:"target"`
	Paths         []string `json:"paths"`
	Claimed       bool     `json:"claimed"`
}
type GitPreparationReceipt struct {
	Promotion *gitprep.PromotionReceipt `json:"promotion,omitempty"`
	Operation GitPreparation            `json:"operation"`
	Resources map[string]string         `json:"resources,omitempty"`
	Git       gitprep.Receipt           `json:"git"`
	Ownership *gitprep.Ownership        `json:"ownership,omitempty"`
	TestOwned []string                  `json:"testOwned,omitempty"`
	Artifacts map[string]string         `json:"artifacts,omitempty"`
}
type PreparedCandidate struct {
	ExternalArtifacts   map[string]string            `json:"externalArtifacts"`
	Candidate           coverage.ValidationCandidate `json:"candidate"`
	BaseSnapshot        string                       `json:"baseSnapshot"`
	DevelopmentSnapshot string                       `json:"developmentSnapshot"`
	PlanDigest          string                       `json:"planDigest"`
	BindingDigest       string                       `json:"bindingDigest"`
	View                gitprep.View                 `json:"view"`
	Coverage            coverage.CoverageContract    `json:"coverage"`
	Whitelist           coverage.ApprovedWhitelist   `json:"whitelist"`
	Artifacts           map[string]string            `json:"artifacts"`
	TestOwned           []string                     `json:"testOwned"`
	ReviewReady         bool                         `json:"reviewReady"`
}

var preparationSteps = []string{"development-worker", "blackbox-design", "blackbox-review", "whitebox-design", "whitebox-review"}

func preparationStep(step authoring.StepID) bool {
	for _, s := range preparationSteps {
		if string(step) == s {
			return true
		}
	}
	return false
}
func preparationBinding(r *RegularState) string {
	return digestOfCanonical(struct {
		Req       string
		Candidate string
		Plan      *DevelopmentPlan
		Route     *RegularRoute
		Topology  string
	}{r.Requirement.Revision, r.Candidate, r.Plan, r.Route, r.Topology})
}
func prepareProjection(r *RegularState) {
	if r.Route == nil || r.Plan == nil || r.Plan.ConfirmedRevision != r.Requirement.Revision || r.Topology != "no-split" {
		return
	}
	binding := preparationBinding(r)
	if r.Preparation != nil && r.Preparation.BindingDigest == binding {
		if r.Preparation.Resources["development"] == "" {
			retainExplicitDevelopment(r, r.Preparation)
		}
		return
	}
	if r.Preparation != nil {
		r.PreparationHistory = append(r.PreparationHistory, *r.Preparation)
	}
	p := &Preparation{PlanDigest: r.Plan.Digest, BindingDigest: binding, Resources: map[string]string{}, Batches: map[string]PreparationResult{}, Commits: map[string]gitprep.Receipt{}, QA: map[string]*PreparedQA{}, DevelopmentSnapshot: r.Candidate}
	if old := r.Preparation; old != nil {
		p.BatchTasks = map[string]runtime.TaskKey{}
		for id, key := range old.BatchTasks {
			p.BatchTasks[id] = key
		}
	}
	if old := r.Preparation; old != nil && old.DevelopmentTask != nil {
		// A revised plan may rename its first unfinished batch. The open work still
		// owns the same TaskKey, native session, attempt budget and working files.
		p.DevelopmentTask = old.DevelopmentTask
		p.DevelopmentSnapshot = old.DevelopmentSnapshot
		p.Resources["development"] = old.Resources["development"]
		p.ExtraAttempts = old.ExtraAttempts
	}
	for _, m := range r.Route.QA {
		p.QA[m] = &PreparedQA{ReviewScope: coverage.ScopeFull}
	}
	if old := r.Preparation; old != nil && len(r.Changes) > 0 && r.Changes[len(r.Changes)-1].ToRevision == r.Requirement.Revision {
		change := r.Changes[len(r.Changes)-1]
		if change.UserDisposition != nil {
			p.DevelopmentSnapshot = old.DevelopmentSnapshot
			p.Resources["development"] = old.Resources["development"]
			p.ExtraAttempts = old.ExtraAttempts
			for id, result := range old.Batches {
				impact := change.Impact["batch:"+id]
				if impact == "unaffected" {
					p.Batches[id] = result
					if receipt, ok := old.Commits[id]; ok && receipt.After != "" {
						p.Commits[id] = receipt
					}
				} else {
					key := batchTask(old, id)
					delete(p.BatchTasks, id)
					if p.DevelopmentTask != nil && *p.DevelopmentTask == key {
						p.DevelopmentTask = nil
					}
				}
			}
		} else if change.Impact["development"] == "unaffected" {
			p.Batches = old.Batches
			p.Commits = old.Commits
			p.DevelopmentSnapshot = old.DevelopmentSnapshot
			p.Resources["development"] = old.Resources["development"]
			if change.Origin == "EXTERNAL_ADOPTION" {
				p.DevelopmentSnapshot = r.Candidate
				delete(p.Resources, "development")
			}
		}
		for mode, q := range old.QA {
			keep := change.Impact[mode+"-review"] == "unaffected"
			for _, kept := range change.Keep {
				if kept.Action == mode+"-review" {
					keep = true
				}
			}
			if p.QA[mode] == nil || !keep || q.ReviewWaiver != nil {
				continue
			}
			if !q.Frozen && (q.Design != nil || q.PreviousDesign != nil) {
				// An explicit unaffected classification also preserves unfinished
				// design and its per-item approvals. Keep history independent.
				data, err := json.Marshal(q)
				if err != nil {
					continue
				}
				var retained PreparedQA
				if err := json.Unmarshal(data, &retained); err != nil {
					continue
				}
				valid := true
				for _, design := range []*QADesign{retained.Design, retained.PreviousDesign} {
					if design == nil {
						continue
					}
					design.Manifest.Binding.SourceBinding = r.RequiredSources.Binding
					design.Manifest.Binding.RouteScope = digestOfCanonical(r.Route)
					design.Manifest.Binding.TopologyScope = r.Topology
					if validateQADraft(r, mode, *design) != nil {
						valid = false
						break
					}
				}
				if !valid {
					continue
				}
				for _, review := range []*coverage.ReviewResult{retained.Review, retained.RetainedReview} {
					if review == nil {
						continue
					}
					review.Binding.SourceBinding = r.RequiredSources.Binding
					review.Binding.RouteScope = digestOfCanonical(r.Route)
					review.Binding.TopologyScope = r.Topology
				}
				p.QA[mode] = &retained
				if change.Origin == "EXTERNAL_ADOPTION" && mode == "whitebox" && retained.Snapshot != p.DevelopmentSnapshot {
					// Author against the adopted candidate, never the old source tree.
					retained.Snapshot = p.DevelopmentSnapshot
				} else {
					p.Resources[mode] = old.Resources[mode]
				}
				continue
			}
			if !q.Frozen || q.Design == nil || q.Review == nil {
				continue
			}
			copy := *q
			design := *q.Design
			review := *q.Review
			design.Manifest.Binding.SourceBinding = r.RequiredSources.Binding
			design.Manifest.Binding.RouteScope = digestOfCanonical(r.Route)
			review.Binding = design.Manifest.Binding
			contract := coverage.CoverageContract{RequiredSources: r.RequiredSources, Manifests: []coverage.AcceptanceManifest{design.Manifest}, AlternativeVerifications: design.Alternatives}
			if err := validateQADesign(r, mode, design); err != nil {
				continue
			}
			whitelist, err := contract.ValidateReviews([]coverage.ReviewResult{review})
			if err != nil {
				continue
			}
			if change.Origin == "EXTERNAL_ADOPTION" && mode == "whitebox" {
				copy.Snapshot = p.DevelopmentSnapshot
			}
			copy.Design = &design
			copy.Review = &review
			copy.Whitelist = &whitelist
			p.QA[mode] = &copy
			p.Resources[mode] = old.Resources[mode]
		}
	}
	retainExplicitDevelopment(r, p)
	r.Preparation = p
}
func preparationTask(p *Preparation, step, scope string) runtime.TaskKey {
	return runtime.TaskKey{Node: "entry", Step: authoring.StepID(step), Scope: strings.TrimPrefix(p.BindingDigest, "sha256:") + ":" + scope}
}
func batchTask(p *Preparation, id string) runtime.TaskKey {
	if key, ok := p.BatchTasks[id]; ok {
		return key
	}
	if p.DevelopmentTask != nil {
		return *p.DevelopmentTask
	}
	return preparationTask(p, "development-worker", fmt.Sprintf("%s-%d", id, p.Rework))
}
func qaTask(p *Preparation, mode, kind string) runtime.TaskKey {
	return preparationTask(p, mode+"-"+kind, fmt.Sprintf("qa-%d-%d", p.QA[mode].Revision, p.Rework))
}
func allDevelopment(r *RegularState) bool {
	p := r.Preparation
	if p == nil || r.Plan == nil {
		return false
	}
	for _, b := range r.Plan.Batches {
		if _, ok := p.Commits[b.ID]; !ok {
			return false
		}
	}
	return true
}
func nextBatch(r *RegularState) *DevelopmentBatch {
	p := r.Preparation
	for i := range r.Plan.Batches {
		b := &r.Plan.Batches[i]
		if _, ok := p.Commits[b.ID]; ok {
			continue
		}
		ready := true
		for _, dep := range b.Dependencies {
			if _, ok := p.Commits[dep]; !ok {
				ready = false
			}
		}
		if ready {
			return b
		}
	}
	return nil
}

// WhiteboxPromotionReady separates an isolated review snapshot from the final
// candidate. A review is accepted only against the current snapshot; revisions
// clear it. A case-set waiver approves neither test-file declarations nor edits.
func WhiteboxPromotionReady(p *Preparation) bool {
	q := p.QA["whitebox"]
	if q == nil {
		return true
	}
	if q.Design == nil || q.Design.TestFiles == nil {
		return false
	}
	return (q.Snapshot == p.DevelopmentSnapshot && len(q.Design.TestFiles) == 0) ||
		(q.Frozen && q.Review != nil && q.Whitelist != nil && q.ReviewWaiver == nil && !q.NeedsRevision)
}

func desiredGit(r *RegularState) *GitPreparation {
	p := r.Preparation
	if p != nil && p.Candidate != nil {
		return desiredVerificationGit(r)
	}
	if p == nil || p.Blocked != "" || p.Candidate != nil {
		return nil
	}
	op := GitPreparation{BindingDigest: p.BindingDigest, Before: p.DevelopmentSnapshot}
	if p.Resources["development"] == "" || (p.QA["blackbox"] != nil && p.Resources["blackbox"] == "") {
		op.Kind = "workspaces"
	} else if b := nextBatch(r); b != nil {
		if result, ok := p.Batches[b.ID]; ok {
			op.Kind = "batch-commit"
			op.Target = b.ID
			op.Paths = result.Manifest
		}
	} else if allDevelopment(r) {
		if q := p.QA["blackbox"]; q != nil && !q.Frozen {
			return nil
		}
		if q := p.QA["whitebox"]; q != nil {
			if p.Resources["whitebox"] == "" {
				op.Kind = "whitebox-workspace"
			} else if q.Design != nil && q.Snapshot == "" {
				op.Kind = "whitebox-commit"
				if q.AuthoringBase != "" {
					op.Before = q.AuthoringBase
				}
				op.Paths = q.Manifest
			} else if !q.Frozen {
				return nil
			}
		}
		if op.Kind == "" {
			if !WhiteboxPromotionReady(p) {
				return nil
			}
			op.Kind = "finalize"
			if q := p.QA["whitebox"]; q != nil {
				op.Before = q.Snapshot
			}
		}
	}
	if op.Kind == "" {
		return nil
	}
	scope := fmt.Sprint(p.Rework)
	for _, mode := range []string{"blackbox", "whitebox"} {
		if q := p.QA[mode]; q != nil {
			scope += fmt.Sprintf(":%s:%d", mode, q.Revision)
		}
	}
	op.ID = "git:" + digestOfCanonical(struct {
		Operation GitPreparation
		Scope     string
	}{op, scope})
	return &op
}

// preparationView publishes independent preparation tasks. A failed review is
// either returned to design immediately or exposed as a typed recovery Ask.
func preparationView(s *State, v *decision.RegularView) {
	r := s.Regular
	p := r.Preparation
	if p == nil {
		return
	}
	if p.BindingDigest != preparationBinding(r) {
		v.PreparationWait = "PLAN_CHANGED"
		return
	}
	if p.Blocked != "" {
		v.PreparationWait = p.Blocked
		return
	}
	if p.Operation != nil {
		v.PreparationWait = "GIT_PREPARATION"
		return
	}
	if desiredGit(r) != nil {
		v.PreparationWait = "GIT_PREPARATION"
		return
	}
	if p.Candidate != nil {
		v.ReviewReady = p.Candidate.ReviewReady
		return
	}
	add := func(key runtime.TaskKey) {
		if at, ok := s.Attempts[key]; ok {
			if at.RetryExhausted {
				v.PreparationAsk = "RETRY_EXHAUSTED"
				return
			}
			if s.Tasks[key] != runtime.TaskTerminal {
				return
			}
		}
		v.PreparationTasks = append(v.PreparationTasks, decision.ReadyTask{Task: key, Step: key.Step})
	}
	if p.Resources["development"] == "" {
		return
	}
	if b := nextBatch(r); b != nil {
		if _, ok := p.Batches[b.ID]; !ok {
			add(batchTask(p, b.ID))
		}
	}
	for _, mode := range []string{"blackbox", "whitebox"} {
		q := p.QA[mode]
		if q == nil || q.Frozen {
			continue
		}
		if mode == "whitebox" && (!allDevelopment(r) || p.Resources["whitebox"] == "") {
			continue
		}
		if q.NeedsRevision {
			v.PreparationWait = "QA_REVISION_REQUIRED"
			continue
		}
		if q.Design == nil {
			add(qaTask(p, mode, "design"))
		} else if mode != "whitebox" || q.Snapshot != "" {
			if q.Review == nil {
				add(qaTask(p, mode, "review"))
			} else {
				v.PreparationWait = "QA_SUGGESTIONS"
			}
		}
	}
	if len(v.PreparationTasks) == 0 && v.PreparationWait == "" {
		v.PreparationWait = "TASKS_IN_FLIGHT"
	}
}
func preparationClaim(r *RegularState, key runtime.TaskKey) (string, string) {
	if verificationStep(key.Step) {
		return verificationClaim(r, key)
	}
	p := r.Preparation
	if !preparationStep(key.Step) || p == nil {
		return r.Candidate, ""
	}
	candidate := p.DevelopmentSnapshot
	if strings.HasPrefix(string(key.Step), "blackbox") {
		candidate = r.BaseSnapshot
	} else if key.Step == "whitebox-design" && p.QA["whitebox"].AuthoringBase != "" {
		candidate = p.QA["whitebox"].AuthoringBase
	} else if key.Step == "whitebox-review" {
		candidate = p.QA["whitebox"].Snapshot
	}
	return candidate, p.BindingDigest
}
func currentPreparationClaim(r *RegularState, c ExecutionClaim) bool {
	if verificationStep(c.Task.Step) {
		return currentVerificationClaim(r, c)
	}
	if !preparationStep(c.Task.Step) {
		return c.Candidate == r.Candidate
	}
	p := r.Preparation
	if p == nil || c.PrerequisiteDigest != p.BindingDigest || p.BindingDigest != preparationBinding(r) {
		return false
	}
	candidate, _ := preparationClaim(r, c.Task)
	if c.Task.Step == "development-worker" {
		b := nextBatch(r)
		return b != nil && c.Task == batchTask(p, b.ID) && c.Candidate == candidate
	}
	parts := strings.Split(string(c.Task.Step), "-")
	if len(parts) != 2 || p.QA[parts[0]] == nil {
		return false
	}
	return c.Task == qaTask(p, parts[0], parts[1]) && c.Candidate == candidate
}
func preparationAllowance(s *State, b *guard.Basis) {
	r := s.Regular
	p := r.Preparation
	for _, step := range preparationSteps {
		b.Definition.ExternalSteps = append(b.Definition.ExternalSteps, runtime.TaskKey{Node: "entry", Step: authoring.StepID(step)})
	}
	keys := map[runtime.TaskKey]bool{}
	// Old issued attempts remain part of the bound definition's finite history;
	// changing a plan cannot erase already spent attempts from the authoritative cap.
	for _, x := range s.RegularExecutions {
		if preparationStep(x.Claim.Task.Step) {
			keys[x.Claim.Task] = true
		}
	}
	if p != nil && r.Plan != nil {
		current := nextBatch(r)
		for _, batch := range r.Plan.Batches {
			key := preparationTask(p, "development-worker", fmt.Sprintf("%s-%d", batch.ID, p.Rework))
			if bound, ok := p.BatchTasks[batch.ID]; ok {
				key = bound
			} else if current != nil && batch.ID == current.ID {
				key = batchTask(p, batch.ID)
			}
			keys[key] = true
		}
		for mode := range p.QA {
			keys[qaTask(p, mode, "design")] = true
			keys[qaTask(p, mode, "review")] = true
		}
	}
	for key := range keys {
		max := 3
		for _, old := range r.PreparationHistory {
			if n := 3 + old.ExtraAttempts[key.String()]; n > max {
				max = n
			}
		}
		if p != nil {
			if n := 3 + p.ExtraAttempts[key.String()]; n > max {
				max = n
			}
		}
		max += continuationCredit(s, key)
		b.ExternalTasks = append(b.ExternalTasks, guard.ExternalTask{TaskKey: key, Provenance: guard.Provenance{Kind: guard.DefinitionSource, Identity: b.Definition.Identity}, MaxAttempts: &max})
	}
}

// continuationCredit excludes only accepted normal development returns from the
// failure allowance. Dispatch identities, source evidence and cost remain intact.
func continuationCredit(s *State, key runtime.TaskKey) int {
	if key.Step != "development-worker" {
		return 0
	}
	n := 0
	for id, x := range s.RegularExecutions {
		if x.Claim.Task == key && x.Finished && s.Results[id].Outcome == "CONTINUE" {
			n++
		}
	}
	return n
}

func preparationMax(s *State, key runtime.TaskKey) int {
	r := s.Regular
	if verificationStep(key.Step) {
		if r.Verification == nil {
			return 3
		}
		return 3 + r.Verification.ExtraAttempts[key.String()]
	}
	if preparationStep(key.Step) {
		if r.Preparation == nil {
			return 3
		}
		return 3 + r.Preparation.ExtraAttempts[key.String()] + continuationCredit(s, key)
	}
	return 3 + r.Reviews[string(key.Step)].ExtraAttempts[key.String()]
}

func (e *Engine) acceptPreparation(s *State, result PreparationResult, revision uint64) (string, error) {
	r := s.Regular
	x, ok := s.RegularExecutions[result.ActionID]
	if !ok || !x.Claimed || x.Finished || !preparationStep(x.Claim.Task.Step) {
		return "", fmt.Errorf("UNKNOWN_ACTION: preparation requires a live claimed task")
	}
	if result.AttemptID != x.Claim.AttemptID || result.Identity != x.Identity {
		return "", fmt.Errorf("INTENT_MISMATCH: preparation native attempt")
	}
	x.Finished = true
	x.PreparationResult = &result
	s.RegularExecutions[result.ActionID] = x
	delete(s.PendingActions, result.ActionID)
	s.Tasks[x.Claim.Task] = runtime.TaskTerminal
	reject := func(err error) (string, error) {
		at := s.Attempts[x.Claim.Task]
		at.Attempts = x.Number
		at.RetryExhausted = x.Number >= preparationMax(s, x.Claim.Task)
		s.Attempts[x.Claim.Task] = at
		r.PreparationErrors = append(r.PreparationErrors, PreparationError{Result: result, Reason: err.Error(), Revision: revision + 1})
		return "REJECTED_RESULT", nil
	}
	if (result.Status != "PASS" && result.Status != "FAIL" && result.Status != "CONTINUE" && result.Status != "DRAFT") || (result.Status == "CONTINUE" && x.Claim.Task.Step != "development-worker") || result.Provider != x.Claim.Provider || result.RequirementRevision != x.Claim.RequirementRevision || result.Candidate != x.Claim.Candidate {
		return reject(fmt.Errorf("invalid preparation result binding/status: %s %s", result.Status, result.Message))
	}
	if !currentPreparationClaim(r, x.Claim) || r.ChangePending {
		return reject(fmt.Errorf("QUARANTINED: obsolete preparation binding"))
	}
	if _, err := gitprep.Paths(result.Manifest); err != nil {
		return reject(err)
	}
	p := r.Preparation
	step := string(x.Claim.Task.Step)
	if step == "development-worker" && result.Status != "PASS" && result.Status != "CONTINUE" {
		return reject(fmt.Errorf("development result requires PASS or CONTINUE"))
	}
	if result.Status == "DRAFT" && !strings.HasSuffix(step, "-design") {
		return reject(fmt.Errorf("DRAFT is only a QA design delivery status"))
	}
	if step == "development-worker" && result.Status != "PASS" {
		if strings.TrimSpace(result.Message) == "" {
			return reject(fmt.Errorf("unfinished development requires its actual continuation reason"))
		}
		s.Results[result.ActionID] = WorkerResult{ActionID: result.ActionID, Provider: result.Provider, Outcome: result.Status, Digest: digestOfCanonical(result)}
		at := s.Attempts[x.Claim.Task]
		at.RetryExhausted = x.Number >= preparationMax(s, x.Claim.Task)
		s.Attempts[x.Claim.Task] = at
		return "ACCEPTED", nil
	}
	if result.Status == "FAIL" && step == "blackbox-design" {
		return reject(fmt.Errorf("blackbox design delivers a draft increment, not a coverage FAIL; coverage belongs to independent review"))
	}
	if result.Status == "FAIL" && step != "whitebox-design" {
		if !strings.HasSuffix(step, "-review") || result.Review == nil || result.Message == "" {
			return reject(fmt.Errorf("FAIL needs a complete QA review and concrete gap"))
		}
		mode := strings.Split(step, "-")[0]
		q := p.QA[mode]
		if q == nil || q.Design == nil || result.Review.Binding != q.Design.Manifest.Binding || result.Review.Scope != q.ReviewScope {
			return reject(fmt.Errorf("FAIL review binding differs"))
		}
		if err := validateFailedQAReview(r, mode, *q.Design, *result.Review); err != nil {
			return reject(err)
		}
		if q.RetainedReview != nil {
			mergeQAReview(result.Review, *q.RetainedReview)
		}
		q.Review = result.Review
		q.DesignGap = result.Message
		q.ConsecutiveFailures++
		q.RecoveryRequired = q.ConsecutiveFailures >= 3
		q.Frozen = false
		q.NeedsRevision = true
		s.Results[result.ActionID] = WorkerResult{ActionID: result.ActionID, Provider: result.Provider, Outcome: result.Status, Digest: digestOfCanonical(result)}
		return "ACCEPTED", nil
	}
	if step == "development-worker" {
		b := nextBatch(r)
		if b == nil || batchTask(p, b.ID) != x.Claim.Task {
			return reject(fmt.Errorf("development batch is not current"))
		}
		if strings.TrimSpace(result.Handoff) == "" || len(result.Validation) != len(b.Subtasks) {
			return reject(fmt.Errorf("batch requires handoff and one actual validation per subtask"))
		}
		for i, v := range result.Validation {
			if v.SubtaskID != b.Subtasks[i].ID || v.Result != "PASS" || strings.TrimSpace(v.Procedure) == "" || strings.TrimSpace(v.Observation) == "" {
				return reject(fmt.Errorf("batch subtask validation missing or out of order"))
			}
		}
		p.Batches[b.ID] = result
		if p.BatchTasks == nil {
			p.BatchTasks = map[string]runtime.TaskKey{}
		}
		p.BatchTasks[b.ID] = x.Claim.Task
	} else {
		mode := strings.Split(step, "-")[0]
		q := p.QA[mode]
		if q == nil {
			return reject(fmt.Errorf("QA mode is not selected"))
		}
		if strings.HasSuffix(step, "-design") {
			if result.Design == nil || (result.Status != "DRAFT" && !(mode == "whitebox" && result.Status == "FAIL")) {
				return reject(fmt.Errorf("QA design requires a DRAFT increment; only an executed whitebox production failure returns FAIL"))
			}
			if mode == "whitebox" {
				if result.Design.TestFiles == nil {
					return reject(fmt.Errorf("whitebox increment requires explicit testFiles"))
				}
				if _, err := gitprep.Paths(result.Design.TestFiles); err != nil {
					return reject(err)
				}
			}
			editedCases := []string{}
			for _, c := range result.Design.Manifest.Cases {
				editedCases = append(editedCases, c.CaseID)
			}
			merged, ids, err := mergeQADesign(q.PreviousDesign, *result.Design, mode, s)
			if err != nil {
				return reject(err)
			}
			for i, id := range editedCases {
				if bound, ok := ids[id]; ok {
					editedCases[i] = bound
				}
			}
			result.Design = &merged
			for i := range result.Reuse {
				if id, ok := ids[result.Reuse[i].CaseID]; ok {
					result.Reuse[i].CaseID = id
				}
			}
			if err := validateQADraft(r, mode, merged); err != nil {
				return reject(err)
			}
			if mode == "blackbox" && len(result.Manifest) > 0 {
				return reject(fmt.Errorf("blackbox authoring returns the contract; no project implementation edits"))
			}
			if mode == "whitebox" {
				if result.Status == "FAIL" && strings.TrimSpace(result.Message) == "" {
					return reject(fmt.Errorf("whitebox failure requires its concrete production failure reason"))
				}
				if !WhiteboxCoverageReady(x) {
					return reject(fmt.Errorf("whitebox requires coverage receipt before existing-test reuse"))
				}
				if !allDevelopment(r) {
					return reject(fmt.Errorf("development must complete before whitebox"))
				}
				if err := validateQAReuseIncrement(q.PreviousDesign, merged, result.Reuse, result.Manifest, editedCases...); err != nil {
					return reject(err)
				}
				if err := validateReuse(result.Design.Manifest, mergeQAReuse(q.Reuse, result.Reuse, merged.Manifest), result.Status == "FAIL"); err != nil {
					return reject(err)
				}
			}
			if q.PreviousDesign != nil && q.RetainedReview != nil {
				retained := ChangedQA(q.PreviousDesign.Manifest, result.Design.Manifest, *q.RetainedReview)
				q.RetainedReview = &retained
			}
			syncQAReviewScope(q)
			q.Design = result.Design
			q.DesignGap = ""
			q.DesignActionID = result.ActionID
			q.Manifest = result.Manifest
			q.Reuse = mergeQAReuse(q.Reuse, result.Reuse, merged.Manifest)
			if mode == "whitebox" && result.Status == "FAIL" {
				// A valid test can reveal a production defect during authoring.
				// Keep that evidence and use the existing explicit rework decision;
				// accepting the report must not freeze or commit a failed candidate.
				p.Blocked = "PRODUCTION_FAILURE"
				p.ReworkReason = result.Message
			}
		} else {
			if q.Design == nil || result.Review == nil {
				return reject(fmt.Errorf("QA review needs exact design and per-source/point/case decisions"))
			}
			if result.Review.Scope != q.ReviewScope {
				return reject(fmt.Errorf("QA review scope differs from the bound %s input scope", q.ReviewScope))
			}
			if q.RetainedReview != nil {
				mergeQAReview(result.Review, *q.RetainedReview)
			}
			if err := validateQADesign(r, mode, *q.Design); err != nil {
				return reject(err)
			}
			c := qaContract(r, mode, *q.Design)
			w, err := c.ValidateReviews([]coverage.ReviewResult{*result.Review})
			if err != nil {
				return reject(err)
			}
			ids := map[string]bool{}
			for _, sg := range result.Suggestions {
				if sg.ID == "" || ids[sg.ID] || (sg.Severity != "P2" && sg.Severity != "P3") || sg.Problem == "" || sg.Decision != "" || sg.Reason != "" {
					return reject(fmt.Errorf("QA optional suggestions require unique stable IDs and P2/P3; disposition is a separate user decision"))
				}
				ids[sg.ID] = true
			}
			q.Review = result.Review
			q.ConsecutiveFailures = 0
			q.RecoveryRequired = false
			q.ReviewWaiver = nil
			q.Whitelist = &w
			q.Suggestions = result.Suggestions
			q.Frozen = len(q.Suggestions) == 0
		}
	}
	s.Results[result.ActionID] = WorkerResult{ActionID: result.ActionID, Provider: result.Provider, Outcome: result.Status, PayloadDigest: digestOfCanonical(result), Digest: digestOfCanonical(result)}
	return "ACCEPTED", nil
}

type PreparationError struct {
	Result   PreparationResult `json:"result"`
	Reason   string            `json:"reason"`
	Revision uint64            `json:"revision"`
}

func qaContract(r *RegularState, mode string, d QADesign) coverage.CoverageContract {
	required := r.RequiredSources
	if len(r.Route.QA) > 1 {
		required.Sources = nil
		for _, source := range r.RequiredSources.Sources {
			include := source.Applicability != coverage.QAApplicability
			for _, edge := range d.Manifest.Edges {
				if edge.SourceID == source.SourceID {
					include = true
				}
			}
			if include {
				required.Sources = append(required.Sources, source)
			}
		}
	}
	return coverage.CoverageContract{RequiredSources: required, SelectedKinds: []coverage.ReviewKind{coverage.ReviewKind(strings.ToUpper(mode))}, Manifests: []coverage.AcceptanceManifest{d.Manifest}, AlternativeVerifications: d.Alternatives}
}
func validateQADraft(r *RegularState, mode string, d QADesign) error {
	return validateQADesignCoverage(r, mode, d, false)
}

func validateQADesign(r *RegularState, mode string, d QADesign) error {
	return validateQADesignCoverage(r, mode, d, true)
}

func validateQADesignCoverage(r *RegularState, mode string, d QADesign, complete bool) error {
	if mode == "whitebox" {
		if d.TestFiles == nil {
			return fmt.Errorf("whitebox design requires explicit testFiles, separate from the change manifest")
		}
		if _, err := gitprep.Paths(d.TestFiles); err != nil {
			return fmt.Errorf("whitebox testFiles: %w", err)
		}
	} else if len(d.TestFiles) != 0 {
		return fmt.Errorf("only whitebox design declares testFiles")
	}
	m := d.Manifest
	seen := map[string]bool{}
	for _, c := range m.Cases {
		if c.Command != nil && strings.TrimSpace(c.Command.Executable) == "" {
			return fmt.Errorf("QA case %s command executable cannot be empty", c.CaseID)
		}
		// A bare executable is one program name. Paths may contain spaces; arguments
		// are separate array entries and are never inferred from a command string.
		if c.Command != nil && !strings.ContainsAny(c.Command.Executable, "/\\") && strings.ContainsAny(c.Command.Executable, " \t\r\n") {
			return fmt.Errorf("QA case %s executable must be one program name or path; put arguments in args", c.CaseID)
		}
		for _, dep := range c.Dependencies {
			if !seen[dep] {
				return fmt.Errorf("QA case %s dependency %s must name an earlier declared case", c.CaseID, dep)
			}
		}
		seen[c.CaseID] = true
	}
	if m.Binding.SourceBinding != r.RequiredSources.Binding || m.Binding.ReviewKind != coverage.ReviewKind(strings.ToUpper(mode)) || m.Binding.RouteScope != digestOfCanonical(r.Route) || m.Binding.TopologyScope != r.Topology {
		return fmt.Errorf("QA manifest binding differs from inventory/route/topology")
	}
	c := qaContract(r, mode, d)
	if !complete {
		return c.ValidateDraft()
	}
	if err := c.Validate(); err != nil {
		return err
	}
	// The generic contract permits arbitrary point IDs. The regular adapter adds
	// the exact structural AC obligation, without synthesizing review decisions.
	if len(r.Route.QA) > 1 {
		return nil
	}
	for _, req := range r.Requirement.Projection.Requirements {
		for _, ac := range req.AcceptanceConditions {
			found := false
			for _, edge := range m.Edges {
				if (edge.SourceID == r.Requirement.Path || edge.SourceID == req.ID) && edge.PointID == strings.ToUpper(mode)+":"+ac.ID {
					found = true
				}
			}
			if !found {
				return fmt.Errorf("AC %s has no case edge from its requirement source", ac.ID)
			}
		}
	}
	return nil
}
func validateReuse(m coverage.AcceptanceManifest, reuse []TestReuse, failed bool) error {
	failures := 0
	by := map[string]TestReuse{}
	for _, r := range reuse {
		if _, ok := by[r.CaseID]; ok {
			return fmt.Errorf("duplicate whitebox test run")
		}
		by[r.CaseID] = r
	}
	for _, c := range m.Cases {
		r, ok := by[c.CaseID]
		if !ok || r.TestRef != c.TestRef || r.CoverageJudgment == "" || r.Procedure == "" || r.Observation == "" || r.Comparison == "" || (r.Result != "EXECUTED_PASS" && (!failed || r.Result != "EXECUTED_FAIL")) {
			return fmt.Errorf("whitebox case %s requires independent coverage judgment and actual test procedure/observation/comparison", c.CaseID)
		}
		if r.Result == "EXECUTED_FAIL" {
			failures++
		}
	}
	if failed && failures == 0 {
		return fmt.Errorf("whitebox FAIL requires an actually failed test")
	}
	return nil
}

func (e *Engine) claimGit(s *State, op GitPreparation) error {
	r := s.Regular
	p := r.Preparation
	if p == nil || p.Operation == nil {
		return fmt.Errorf("UNKNOWN_ACTION: Git preparation was not issued")
	}
	want := *p.Operation
	want.Claimed = op.Claimed
	if digestOfCanonical(want) != digestOfCanonical(op) || op.Claimed || p.Operation.Claimed || ControlBlocksGit(r, op) || op.BindingDigest != preparationBinding(r) {
		return fmt.Errorf("EVENT_NOT_CURRENT: stale, duplicate or cancelled Git operation")
	}
	p.Operation.Claimed = true
	return nil
}
func (e *Engine) acceptGit(s *State, receipt GitPreparationReceipt) error {
	r := s.Regular
	p := r.Preparation
	if p == nil || p.Operation == nil || !p.Operation.Claimed {
		return fmt.Errorf("UNKNOWN_ACTION: Git receipt needs claimed intent")
	}
	op := *p.Operation
	if digestOfCanonical(receipt.Operation) != digestOfCanonical(op) {
		return fmt.Errorf("INTENT_MISMATCH: Git receipt")
	}
	if r.ChangePending || op.BindingDigest != preparationBinding(r) {
		return fmt.Errorf("EVENT_NOT_CURRENT: reconcile cancelled Git resource before advancing")
	}
	if op.Kind != "workspaces" && op.Kind != "whitebox-workspace" && op.Kind != "repair-workspace" && (receipt.Git.OperationID != op.ID || receipt.Git.Before != op.Before || receipt.Git.After == "" || receipt.Git.View.Identity != receipt.Git.After || receipt.Git.View.Digest == "") {
		return fmt.Errorf("incomplete Git snapshot receipt")
	}
	for k, v := range receipt.Resources {
		if v == "" {
			return fmt.Errorf("resource path is empty")
		}
		p.Resources[k] = v
	}
	switch op.Kind {
	case "repair-workspace", "repair-commit", "promote":
		if err := acceptVerificationGit(s, receipt); err != nil {
			return err
		}
	case "workspaces":
		if p.Resources["development"] == "" || (p.QA["blackbox"] != nil && p.Resources["blackbox"] == "") {
			return fmt.Errorf("missing isolated workspaces")
		}
	case "batch-commit":
		p.Commits[op.Target] = receipt.Git
		p.DevelopmentSnapshot = receipt.Git.After
		p.DevelopmentTask = nil
	case "whitebox-workspace":
		if p.Resources["whitebox"] == "" {
			return fmt.Errorf("missing whitebox workspace")
		}
	case "whitebox-commit":
		if receipt.Ownership == nil || (receipt.Ownership.Status != "TEST_ONLY" && receipt.Ownership.Status != "REVIEW_PENDING") {
			p.Blocked = "PRODUCTION_CHANGED"
		} else {
			p.QA["whitebox"].Snapshot = receipt.Git.After
			// The reviewed scope is cumulative across whitebox revisions. Keep
			// that exact delta for later test-artifact retention.
			if receipt.Ownership.Paths != nil {
				p.QA["whitebox"].Manifest = receipt.Ownership.Paths
			}
		}
	case "finalize":
		if !WhiteboxPromotionReady(p) {
			return fmt.Errorf("whitebox testFiles and changes require independent test-only review")
		}
		if !allDevelopment(r) {
			return fmt.Errorf("candidate requires all development batches")
		}
		contract := coverage.CoverageContract{RequiredSources: r.RequiredSources, Candidate: &coverage.ValidationCandidate{Identity: receipt.Git.After, Digest: receipt.Git.View.Digest}}
		reviews := []coverage.ReviewResult{}
		for _, mode := range []string{"blackbox", "whitebox"} {
			q := p.QA[mode]
			if q == nil {
				continue
			}
			if !q.Frozen || q.Design == nil || (q.ReviewWaiver == nil && (q.Review == nil || q.Whitelist == nil)) {
				return fmt.Errorf("candidate requires all selected case-sets FROZEN")
			}
			contract.SelectedKinds = append(contract.SelectedKinds, coverage.ReviewKind(strings.ToUpper(mode)))
			contract.Manifests = append(contract.Manifests, q.Design.Manifest)
			if len(contract.AlternativeVerifications) == 0 {
				contract.AlternativeVerifications = q.Design.Alternatives
			}
			if q.ReviewWaiver == nil {
				reviews = append(reviews, *q.Review)
			}
		}
		for _, req := range r.Requirement.Projection.Requirements {
			if len(contract.Manifests) == 0 {
				break
			}
			for _, ac := range req.AcceptanceConditions {
				found := false
				for _, manifest := range contract.Manifests {
					for _, edge := range manifest.Edges {
						if (edge.SourceID == r.Requirement.Path || edge.SourceID == req.ID) && edge.PointID == string(manifest.Binding.ReviewKind)+":"+ac.ID {
							found = true
						}
					}
				}
				if !found {
					return fmt.Errorf("AC %s has no case edge in the selected QA union", ac.ID)
				}
			}
		}
		w := coverage.ApprovedWhitelist{}
		if len(reviews) == len(contract.Manifests) && len(reviews) > 0 {
			var err error
			w, err = contract.ValidateReviews(reviews)
			if err != nil {
				return err
			}
		} else if len(contract.Manifests) > 0 {
			// A pre-wave waiver is recorded separately from approvals. It permits
			// execution of these exact cases, but creates neither review PASS nor
			// execution PASS, and the full requirement guarantee remains WAIVED.
			if err := contract.Validate(); err != nil {
				return err
			}
			for _, mode := range []string{"blackbox", "whitebox"} {
				q := p.QA[mode]
				if q == nil || q.ReviewWaiver != nil {
					continue
				}
				approved, err := qaContract(r, mode, *q.Design).ValidateReviews([]coverage.ReviewResult{*q.Review})
				if err != nil {
					return err
				}
				w.Entries = append(w.Entries, approved.Entries...)
			}
			w.RequiredSourcesDigest, _ = contract.RequiredSourcesDigest()
			w.ManifestDigest, _ = contract.ManifestDigest()
			w.MapDigest, _ = contract.MapDigest()
			w.AlternativeVerifications = contract.AlternativeVerifications
			w.Digest = digestOfCanonical(w)
		}
		for _, mode := range r.Route.QA {
			path := PreparationArtifact(s.RunID, mode)
			data := gitprep.Digest(p.QA[mode].Design)
			if receipt.Artifacts[path] != data {
				return fmt.Errorf("candidate omitted approved QA artifact %s", path)
			}
		}
		external := map[string]string{"requiredSources": digestOfCanonical(r.RequiredSources), "approvedWhitelist": digestOfCanonical(w)}
		for mode, q := range p.QA {
			external[mode+"Review"] = digestOfCanonical(q.Review)
			if q.ReviewWaiver != nil {
				external[mode+"ReviewWaiver"] = digestOfCanonical(q.ReviewWaiver)
			}
			external[mode+"Suggestions"] = digestOfCanonical(q.Suggestions)
			external[mode+"Reuse"] = digestOfCanonical(q.Reuse)
		}
		p.Candidate = &PreparedCandidate{ExternalArtifacts: external, Candidate: *contract.Candidate, BaseSnapshot: r.BaseSnapshot, DevelopmentSnapshot: p.DevelopmentSnapshot, PlanDigest: p.PlanDigest, BindingDigest: p.BindingDigest, View: receipt.Git.View, Coverage: contract, Whitelist: w, Artifacts: receipt.Artifacts, TestOwned: receipt.TestOwned, ReviewReady: true}
	}
	p.Receipts = append(p.Receipts, receipt)
	delete(s.PendingHostActions, op.ID)
	p.Operation = nil
	return nil
}
func PreparationArtifact(run, mode string) string {
	return ".gates/results/" + run + "." + mode + "-cases.json"
}
func issuePreparationGit(s *State, revision uint64) bool {
	r := s.Regular
	p := r.Preparation
	if p == nil || p.Operation != nil || r.Guard.Status == "COST_PENDING" || ControlPausesRun(r) || r.Topology != "no-split" || !reviewPrerequisiteSatisfied(r, r.Reviews["start-readiness"]) {
		return false
	}
	op := desiredGit(r)
	if op == nil || ControlBlocksGit(r, *op) {
		return false
	}
	op.Revision = revision + 1
	p.Operation = op
	s.PendingHostActions[op.ID] = HostActionIntent{ActionID: op.ID, Operation: HostActionExecuteAdapterOperation, Step: "git.prepare", Adapter: &AdapterHostAction{Operation: "git.prepare", Schema: "schema.git.prepare", Params: map[string]any{"id": op.ID, "kind": op.Kind, "bindingDigest": op.BindingDigest}}, PayloadDigest: digestOfCanonical(op), Revision: revision + 1}
	return true
}
func sortedPreparationTasks(s *State) []runtime.TaskKey {
	v := decision.RegularView{}
	preparationView(s, &v)
	verificationView(s, &v)
	out := []runtime.TaskKey{}
	for _, r := range v.PreparationTasks {
		out = append(out, r.Task)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

// ChangedQA invalidates only changed cases and users of changed shared steps.
// Review decisions for unchanged IDs survive; aggregate PASS never fills a gap.
func ChangedQA(old, new coverage.AcceptanceManifest, review coverage.ReviewResult) coverage.ReviewResult {
	changedShared := map[string]bool{}
	oldShared := map[string]string{}
	for _, s := range old.SharedSteps {
		oldShared[s.ID] = digestOfCanonical(s)
	}
	for _, s := range new.SharedSteps {
		if oldShared[s.ID] != digestOfCanonical(s) {
			changedShared[s.ID] = true
		}
		delete(oldShared, s.ID)
	}
	for id := range oldShared {
		changedShared[id] = true
	}
	oldCases := map[string]string{}
	for _, c := range old.Cases {
		oldCases[c.CaseID] = digestOfCanonical(c)
	}
	valid := map[string]bool{}
	for _, c := range new.Cases {
		same := oldCases[c.CaseID] == digestOfCanonical(c)
		for _, id := range c.SharedSteps {
			if changedShared[id] {
				same = false
			}
		}
		if same {
			valid[c.CaseID] = true
		}
	}
	keep := []coverage.ReviewDecision{}
	for _, d := range review.CaseDecisions {
		if valid[d.ID] {
			keep = append(keep, d)
		}
	}
	review.CaseDecisions = keep
	shared := []coverage.ReviewDecision{}
	for _, d := range review.SharedStepDecisions {
		if !changedShared[d.ID] {
			shared = append(shared, d)
		}
	}
	review.SharedStepDecisions = shared
	edgeKey := func(m coverage.AcceptanceManifest, id string, point bool) string {
		edges := []string{}
		for _, e := range m.Edges {
			if (!point && e.SourceID == id) || (point && e.PointID == id) {
				edges = append(edges, digestOfCanonical(e))
			}
		}
		sort.Strings(edges)
		if point {
			for _, p := range m.Points {
				if p.PointID == id {
					return digestOfCanonical(p) + digestOfCanonical(edges)
				}
			}
		} else {
			for _, source := range m.Sources {
				if source == id {
					return digestOfCanonical(edges)
				}
			}
		}
		return ""
	}
	retain := func(in []coverage.ReviewDecision, point bool) []coverage.ReviewDecision {
		out := []coverage.ReviewDecision{}
		for _, d := range in {
			before, after := edgeKey(old, d.ID, point), edgeKey(new, d.ID, point)
			if before != "" && before == after {
				out = append(out, d)
			}
		}
		return out
	}
	review.SourceDecisions = retain(review.SourceDecisions, false)
	review.PointDecisions = retain(review.PointDecisions, true)
	review.SetStatus = coverage.StatusPending
	return review
}
func preparationRequest(s *State, q RegularRequest) error {
	r := s.Regular
	p := r.Preparation
	switch q.Action {
	case "plan-change":
		if p != nil && p.Operation != nil {
			return fmt.Errorf("plan change requires current Git operation reconciliation")
		}
		if r.Plan == nil {
			return fmt.Errorf("no development plan to revise")
		}
	case "qa-suggestion":
		if p == nil || p.QA[q.Target] == nil {
			return fmt.Errorf("unknown QA mode")
		}
		qa := p.QA[q.Target]
		for _, sg := range qa.Suggestions {
			if sg.ID == q.FindingID && sg.Decision == "" {
				if q.Meaning != "adopted" && q.Meaning != "deferred" && q.Meaning != "not-adopted" {
					return fmt.Errorf("suggestion disposition must be adopted/deferred/not-adopted")
				}
				return nil
			}
		}
		return fmt.Errorf("unknown or already disposed QA suggestion")
	case "qa-revise":
		if p != nil && p.Candidate != nil && q.Meaning != "acceptance" && q.Meaning != "public-behavior" && q.Meaning != "stable-execution" {
			return fmt.Errorf("THAW requires changed acceptance, public behavior or stable executability")
		}

		if p == nil || p.QA[q.Target] == nil {
			return fmt.Errorf("QA revision requires a current unfrozen validation candidate")
		}
	case "development-rework":
		if p == nil || (p.Blocked != "PRODUCTION_CHANGED" && p.Blocked != "PRODUCTION_FAILURE") {
			return fmt.Errorf("development rework requires whitebox production failure, production change or UNKNOWN")
		}
		if _, err := developmentReworkBatch(r, q.Target); err != nil {
			return err
		}
	case "preparation-retry":
		if p == nil {
			return fmt.Errorf("no preparation attempt")
		}
		for key, at := range s.Attempts {
			if key.String() == q.Target && preparationStep(key.Step) && at.RetryExhausted {
				return nil
			}
		}
		return fmt.Errorf("retry requires an exhausted preparation TaskKey")
	default:
		return fmt.Errorf("unknown preparation control")
	}
	return nil
}

// Rework consumes the plan's already pending work, including a confirmed
// development increment. It must not reopen an unrelated completed Batch.
func developmentReworkBatch(r *RegularState, target string) (*DevelopmentBatch, error) {
	if r.Plan == nil || len(r.Plan.Batches) == 0 {
		return nil, fmt.Errorf("development rework requires a development plan")
	}
	if pending := nextBatch(r); pending != nil {
		if target != "" && target != pending.ID {
			return nil, fmt.Errorf("development rework must use pending Batch %s", pending.ID)
		}
		return pending, nil
	}
	if target == "" {
		return &r.Plan.Batches[len(r.Plan.Batches)-1], nil
	}
	for i := range r.Plan.Batches {
		if r.Plan.Batches[i].ID == target {
			return &r.Plan.Batches[i], nil
		}
	}
	return nil, fmt.Errorf("development rework target must reference the confirmed plan")
}

func preparationDecision(s *State, q RegularRequest) {
	r := s.Regular
	p := r.Preparation
	switch q.Action {
	case "plan-change":
		invalidateAfterReview(r, "start-readiness")
		r.Reviews["start-readiness"].Fresh = true
	case "qa-suggestion":
		qa := p.QA[q.Target]
		for i := range qa.Suggestions {
			sg := &qa.Suggestions[i]
			if sg.ID == q.FindingID {
				sg.Decision = q.Meaning
				sg.Reason = q.Reason
				if q.Meaning == "adopted" {
					qa.NeedsRevision = true
					qa.Frozen = false
				}
			}
		}
		qa.Frozen = qa.Review != nil && !qa.NeedsRevision
		for _, sg := range qa.Suggestions {
			if sg.Decision == "" {
				qa.Frozen = false
			}
		}
	case "qa-revise":
		invalidateVerification(s, "QA artifact revision")
		p.Candidate = nil
		reviseQA(p.QA[q.Target])
		p.QA[q.Target].DesignGap = q.Reason
	case "development-rework":
		batch, _ := developmentReworkBatch(r, q.Target) // validated by preparationRequest
		p.Rework++
		if p.Blocked != "PRODUCTION_FAILURE" {
			p.ReworkReason = "Whitebox could not prove a test-only candidate. Recheck the required behavior and production inputs."
			for i := len(p.Receipts) - 1; i >= 0; i-- {
				if p.Receipts[i].Ownership != nil {
					p.ReworkReason += " " + p.Receipts[i].Ownership.Reason
					break
				}
			}
		}
		p.Blocked = ""
		if result, ok := p.Batches[batch.ID]; ok {
			key := s.RegularExecutions[result.ActionID].Claim.Task
			p.DevelopmentTask = &key
		}
		delete(p.Batches, batch.ID)
		delete(p.Commits, batch.ID)
		delete(p.Resources, "whitebox")
		p.QA["whitebox"] = &PreparedQA{ReviewScope: coverage.ScopeFull}
		p.Candidate = nil
	case "preparation-retry":
		if p.ExtraAttempts == nil {
			p.ExtraAttempts = map[string]int{}
		}
		p.ExtraAttempts[q.Target]++
		for key, at := range s.Attempts {
			if key.String() == q.Target {
				at.RetryExhausted = false
				s.Attempts[key] = at
			}
		}
	}
}

func mergeQAReview(review *coverage.ReviewResult, retained coverage.ReviewResult) {
	merge := func(current, old []coverage.ReviewDecision) []coverage.ReviewDecision {
		seen := map[string]bool{}
		for _, d := range current {
			seen[d.ID] = true
		}
		for _, d := range old {
			if !seen[d.ID] && d.Status == coverage.StatusPass {
				current = append(current, d)
			}
		}
		return current
	}
	review.SourceDecisions = merge(review.SourceDecisions, retained.SourceDecisions)
	review.PointDecisions = merge(review.PointDecisions, retained.PointDecisions)
	review.CaseDecisions = merge(review.CaseDecisions, retained.CaseDecisions)
	review.SharedStepDecisions = merge(review.SharedStepDecisions, retained.SharedStepDecisions)
}

// A valid semantic FAIL promptly returns to design while development remains in
// flight. The source settlement remains the capacity barrier; errors never create
// a fresh semantic task and therefore cannot evade the same-TaskKey retry limit.
func reviseQA(q *PreparedQA) {
	q.Frozen = false
	if q.Design == nil && q.Review == nil {
		syncQAReviewScope(q)
		if q.NeedsRevision {
			q.NeedsRevision = false
			q.ReviewWaiver = nil
			q.Revision++
		}
		return
	}
	q.AuthoringBase = q.Snapshot
	q.PreviousDesign = q.Design
	q.PreviousDesignActionID = q.DesignActionID
	if q.Review != nil {
		q.RetainedReview = q.Review
	}
	syncQAReviewScope(q)
	q.NeedsRevision = false
	q.ReviewWaiver = nil
	q.Design = nil
	q.DesignActionID = ""
	q.Review = nil
	q.Whitelist = nil
	q.Revision++
	q.Snapshot = ""
}
func refreshPreparationRecovery(s *State, revision uint64) bool {
	r := s.Regular
	p := r.Preparation
	if p == nil || p.BindingDigest != preparationBinding(r) || r.Ask != nil || r.ChangePending || r.Terminal != "" {
		return false
	}
	for _, mode := range []string{"blackbox", "whitebox"} {
		q := p.QA[mode]
		if q == nil || q.Frozen {
			continue
		}
		kind := "design"
		if q.Design != nil {
			kind = "review"
		}
		key := qaTask(p, mode, kind)
		at := s.Attempts[key]
		if q.NeedsRevision && !q.RecoveryRequired {
			reviseQA(q)
			return true
		}
		if !q.RecoveryRequired && !at.RetryExhausted {
			continue
		}
		candidate, _ := preparationClaim(r, key)
		binding := &QARecoveryBinding{Mode: mode, Task: key, RequirementRevision: r.Requirement.Revision, Candidate: candidate, CaseSetDigest: digestOfCanonical(q.Design), ResultDigest: digestOfCanonical(q.Review), ConsecutiveFailures: q.ConsecutiveFailures, RetryExhausted: at.RetryExhausted}
		// The complete current failure and native error receipts stay in the run.
		for _, x := range s.RegularExecutions {
			if x.Claim.Task == key && x.Number == at.Attempts && x.PreparationResult != nil {
				binding.ResultDigest = digestOfCanonical(x.PreparationResult)
			}
		}
		reason := "Three consecutive pre-wave review FAIL results require a bounded disposition."
		if at.RetryExhausted {
			reason = "The current QA TaskKey exhausted its configured runtime attempt budget; another attempt requires a recovery decision."
		}
		ask := &RegularAsk{ID: fmt.Sprintf("qa-recovery:%s:%d", mode, revision), Kind: "qa-recovery", Revision: revision, Request: RegularRequest{Action: "qa-recovery", Target: mode, Reason: reason}, Options: []string{"redesign", "fresh-review", "change-requirement", "waiver", "skip", "abort"}, QA: binding}
		if at.RetryExhausted {
			ask.Options = append([]string{"retry"}, ask.Options...)
		}
		ask.Digest = digestOfCanonical(ask)
		ask.Token = freshnessToken(revision, RequestID(ask.ID))
		r.Ask = ask
		return true
	}
	return false
}
func decideQARecovery(s *State, a *RegularAsk, d RegularDecision, revision uint64) error {
	r := s.Regular
	b := a.QA
	if b == nil || r.Preparation == nil || b.RequirementRevision != r.Requirement.Revision {
		return fmt.Errorf("STALE_FRESHNESS: QA recovery inputs changed")
	}
	q := r.Preparation.QA[b.Mode]
	if q == nil || b.CaseSetDigest != digestOfCanonical(q.Design) {
		return fmt.Errorf("STALE_FRESHNESS: QA case-set changed")
	}
	candidate, _ := preparationClaim(r, b.Task)
	if candidate != b.Candidate {
		return fmt.Errorf("STALE_FRESHNESS: QA candidate changed")
	}
	if strings.TrimSpace(d.Reason) == "" {
		return fmt.Errorf("QA disposition requires a nonempty reason bound to current revision")
	}
	switch d.Choice {
	case "retry":
		if !s.Attempts[b.Task].RetryExhausted {
			return fmt.Errorf("retry is not exhausted")
		}
		preparationDecision(s, RegularRequest{Action: "preparation-retry", Target: b.Task.String()})
	case "redesign":
		// Unavailability is an attempt failure, not a new semantic design round.
		// Even an explicit redesign cannot silently reset an exhausted TaskKey.
		if b.RetryExhausted {
			preparationDecision(s, RegularRequest{Action: "preparation-retry", Target: b.Task.String()})
		} else {
			reviseQA(q)
		}
	case "fresh-review":
		if q.Design == nil {
			return fmt.Errorf("fresh review requires an exact case-set")
		}
		if b.RetryExhausted {
			preparationDecision(s, RegularRequest{Action: "preparation-retry", Target: b.Task.String()})
		} else {
			q.Revision++
			q.Review = nil
			q.NeedsRevision = false
			q.RetainedReview = nil
			q.ReviewScope = coverage.ScopeFull
		}
	case "waiver", "skip":
		if b.Mode == "whitebox" && (q.Snapshot != r.Preparation.DevelopmentSnapshot || (q.Design != nil && len(q.Design.TestFiles) != 0)) {
			return fmt.Errorf("whitebox testFiles and project changes require independent test-only review; case-set waiver cannot authorize promotion")
		}
		if q.Design == nil {
			return fmt.Errorf("review waiver/skip requires an exact authored case-set; redesign first")
		}
		q.ReviewWaiver = &QAReviewWaiver{Binding: *b, Choice: d.Choice, Reason: d.Reason, Revision: revision}
		q.Frozen = true
		q.NeedsRevision = false
		r.Guarantee.Status = "WAIVED"
		r.Guarantee.WaiverReason = d.Reason
	case "change-requirement":
		r.ChangePending = true
		invalidateVerification(s, "QA recovery requirement change")
	case "abort":
		// Use the normal abort request/decision path so native termination and cost
		// reconciliation retain the same lifecycle as every other phase.
		return fmt.Errorf("INTERNAL: abort must use the common control path")
	default:
		return fmt.Errorf("INVALID_CHOICE")
	}
	switch d.Choice {
	case "retry", "redesign", "fresh-review":
		q.DesignGap = d.Reason
	}
	q.RecoveryRequired = false
	return nil
}

// A semantic FAIL must be just as complete as PASS. Checking a value copy with
// FAIL decisions normalized solely for structural validation avoids counting an
// incomplete native response as a semantic review. The raw FAIL is never edited.
func validateFailedQAReview(r *RegularState, mode string, design QADesign, review coverage.ReviewResult) error {
	failed := review.SetStatus == coverage.StatusFail
	normalize := func(in []coverage.ReviewDecision) ([]coverage.ReviewDecision, error) {
		out := append([]coverage.ReviewDecision{}, in...)
		for i := range out {
			if out[i].Status == coverage.StatusFail {
				failed = true
				out[i].Status = coverage.StatusPass
			} else if out[i].Status != coverage.StatusPass {
				return nil, fmt.Errorf("incomplete QA FAIL decision")
			}
		}
		return out, nil
	}
	var err error
	if review.SourceDecisions, err = normalize(review.SourceDecisions); err != nil {
		return err
	}
	if review.PointDecisions, err = normalize(review.PointDecisions); err != nil {
		return err
	}
	if review.CaseDecisions, err = normalize(review.CaseDecisions); err != nil {
		return err
	}
	if review.SharedStepDecisions, err = normalize(review.SharedStepDecisions); err != nil {
		return err
	}
	if !failed {
		return fmt.Errorf("QA FAIL requires at least one concrete failed item")
	}
	review.SetStatus = coverage.StatusPass
	if q := r.Preparation.QA[mode]; q.RetainedReview != nil {
		mergeQAReview(&review, *q.RetainedReview)
	}
	return qaContract(r, mode, design).ValidateDraftReview([]coverage.ReviewResult{review})
}

// A current explicit keep decision also applies when confirming an unchanged
// requirement after a recording pause. That path has no new apply-requirement
// receipt, so the older change classification cannot override the user's choice.
func retainExplicitDevelopment(r *RegularState, p *Preparation) {
	d := r.RequirementImpact
	if d == nil || d.RequirementRevision != r.Requirement.Revision || d.Candidate != r.Candidate || d.Choices["development"] != "keep" {
		return
	}
	for i := len(r.PreparationHistory) - 1; i >= 0; i-- {
		old := &r.PreparationHistory[i]
		if old.PlanDigest != p.PlanDigest || old.Resources["development"] == "" {
			continue
		}
		p.Resources["development"] = old.Resources["development"]
		p.DevelopmentSnapshot = old.DevelopmentSnapshot
		p.ExtraAttempts = old.ExtraAttempts
		for id, result := range old.Batches {
			if d.Choices["batch:"+id] != "keep" {
				continue
			}
			if _, exists := p.Batches[id]; !exists {
				p.Batches[id] = result
				if receipt, ok := old.Commits[id]; ok {
					p.Commits[id] = receipt
				}
			}
		}
		return
	}
}

// An affected review needs an actual retained approval, not merely an old scope.
func syncQAReviewScope(q *PreparedQA) {
	q.ReviewScope = coverage.ScopeFull
	if q.RetainedReview == nil {
		return
	}
	for _, rows := range [][]coverage.ReviewDecision{q.RetainedReview.SourceDecisions, q.RetainedReview.PointDecisions, q.RetainedReview.CaseDecisions, q.RetainedReview.SharedStepDecisions} {
		for _, row := range rows {
			if row.Status == coverage.StatusPass {
				q.ReviewScope = coverage.ScopeAffected
				return
			}
		}
	}
}
