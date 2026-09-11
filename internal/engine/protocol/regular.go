package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"formal-gates/internal/cost"
	"formal-gates/internal/engine/authoring"
	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/guard"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/runtime"
	"formal-gates/internal/requirements"
	"formal-gates/internal/settings"
)

type RequirementDocument struct {
	Path       string                                    `json:"path"`
	Revision   string                                    `json:"revision"`
	Text       string                                    `json:"text"`
	Projection *requirements.FrozenRequirementProjection `json:"projection,omitempty"`
	Confirmed  bool                                      `json:"confirmed"`
}
type RequirementGuaranteeActivation struct {
	Status              string `json:"status"`
	Source              string `json:"source"`
	RequirementRevision string `json:"requirementRevision"`
	ProjectionDigest    string `json:"projectionDigest"`
	WaiverReason        string `json:"waiverReason,omitempty"`
}
type RegularRoute struct {
	Kind  string   `json:"kind"`
	QA    []string `json:"qa"`
	Gates []string `json:"gates"`
}
type DevelopmentSubtask struct {
	ID   string `json:"id"`
	Task string `json:"task"`
}
type DevelopmentBatch struct {
	ID               string               `json:"id"`
	Task             string               `json:"task"`
	Subtasks         []DevelopmentSubtask `json:"subtasks"`
	Dependencies     []string             `json:"dependencies"`
	Interfaces       string               `json:"interfaces"`
	StateBoundary    string               `json:"stateBoundary"`
	Risks            string               `json:"risks"`
	DefinitionOfDone string               `json:"definitionOfDone"`
	Validation       string               `json:"validation"`
	Recovery         string               `json:"recovery"`
	Handoff          string               `json:"handoff"`
	ContextCost      string               `json:"contextCost"`
}
type DevelopmentPlan struct {
	Batches             []DevelopmentBatch `json:"batches"`
	Rationale           string             `json:"rationale"`
	RequirementRevision string             `json:"requirementRevision"`
	Digest              string             `json:"digest"`
	ConfirmedRevision   string             `json:"confirmedRevision,omitempty"`
}
type RegularAsk struct {
	QA       *QARecoveryBinding `json:"qa,omitempty"`
	Preview  *RegularPreview    `json:"preview,omitempty"`
	ID       string             `json:"id"`
	Kind     string             `json:"kind"`
	Token    string             `json:"token"`
	Revision uint64             `json:"revision"`
	Digest   string             `json:"digest"`
	Options  []string           `json:"options"`
	Request  RegularRequest     `json:"request"`
}

// RegularPreview is displayed inside the Ask and participates in its digest.
// It is an immutable projection, not another authorization or cost ledger.
type RegularPreview struct {
	StateRevision       uint64                                 `json:"stateRevision"`
	RequirementRevision string                                 `json:"requirementRevision"`
	BaseSnapshot        string                                 `json:"baseSnapshot"`
	Candidate           string                                 `json:"candidate"`
	Consequences        string                                 `json:"consequences"`
	Reviews             map[string]*PreDevelopmentReviewSeries `json:"reviews"`
	Verification        *Verification                          `json:"verification,omitempty"`
	Guard               GuardProjection                        `json:"guard"`
	Cost                *cost.RunCost                          `json:"cost"`
}

type ExternalObservation struct {
	Identity   string `json:"identity"`
	Lineage    string `json:"lineage"`
	DiffDigest string `json:"diffDigest"`
}
type RegularRequest struct {
	External *ExternalObservation `json:"external,omitempty"`
	Target   string               `json:"target,omitempty"`

	RequiredSources []coverage.RequiredSource `json:"requiredSources,omitempty"`
	Action          string                    `json:"action"`
	Reason          string                    `json:"reason,omitempty"`
	ReviewAction    string                    `json:"reviewAction,omitempty"`
	FindingID       string                    `json:"findingId,omitempty"`
	ResultDigest    string                    `json:"resultDigest,omitempty"`
	Mode            string                    `json:"mode,omitempty"`
	Route           *RegularRoute             `json:"route,omitempty"`
	PlanDigest      string                    `json:"planDigest,omitempty"`
	Document        *RequirementDocument      `json:"document,omitempty"`
	Meaning         string                    `json:"meaning,omitempty"`
	Origin          string                    `json:"origin,omitempty"`
	Impact          map[string]string         `json:"impact,omitempty"`
	Keep            []string                  `json:"keep,omitempty"`
	Delta           []ReviewDeltaHunk         `json:"delta,omitempty"`
}
type ReviewDeltaHunk struct {
	Old    string `json:"old"`
	New    string `json:"new"`
	Source string `json:"source"`
	Reason string `json:"reason"`
}
type RegularDecision struct {
	RequestID string `json:"requestId"`
	Token     string `json:"token"`
	Digest    string `json:"digest"`
	Choice    string `json:"choice"`
	Reason    string `json:"reason,omitempty"`
}
type RequirementChangeReceipt struct {
	FromRevision  string            `json:"fromRevision"`
	ToRevision    string            `json:"toRevision"`
	Candidate     string            `json:"candidate"`
	StateRevision uint64            `json:"stateRevision"`
	Meaning       string            `json:"meaning"`
	Origin        string            `json:"origin"`
	Keep          []KeptReview      `json:"keep"`
	Impact        map[string]string `json:"impact"`
	Reason        string            `json:"reason"`
	DeltaDigest   string            `json:"deltaDigest"`
}
type KeptReview struct {
	Action          string `json:"action"`
	ResultDigest    string `json:"resultDigest"`
	FromRequirement string `json:"fromRequirement"`
	FromCandidate   string `json:"fromCandidate"`
	ToRequirement   string `json:"toRequirement"`
	ToCandidate     string `json:"toCandidate"`
}
type GuardProjection struct {
	Basis          guard.Result      `json:"basis"`
	UsedAttempts   int               `json:"usedAttempts"`
	UsedTokens     int64             `json:"usedTokens"`
	Status         string            `json:"status"`
	TokenCapStatus string            `json:"tokenCapStatus"`
	ContinueTasks  []runtime.TaskKey `json:"continueTasks,omitempty"`
}
type RegularState struct {
	StartedAt               time.Time                              `json:"startedAt,omitempty"`
	OwnerStartedAt          time.Time                              `json:"ownerStartedAt,omitempty"`
	OwnerTranscript         string                                 `json:"ownerTranscript,omitempty"`
	OwnerSession            string                                 `json:"ownerSession,omitempty"`
	OwnerProvider           string                                 `json:"ownerProvider,omitempty"`
	OwnerCostBaseline       *cost.TranscriptSnapshot               `json:"ownerCostBaseline,omitempty"`
	CatalogGateInstructions map[string]string                      `json:"catalogGateInstructions,omitempty"`
	ActionInstructions      map[string]string                      `json:"actionInstructions,omitempty"`
	ReviewerInstructions    string                                 `json:"reviewerInstructions,omitempty"`
	Verification            *Verification                          `json:"verification,omitempty"`
	VerificationErrors      []VerificationError                    `json:"verificationErrors,omitempty"`
	RepairErrors            []RepairError                          `json:"repairErrors,omitempty"`
	Preparation             *Preparation                           `json:"preparation,omitempty"`
	PreparationHistory      []Preparation                          `json:"preparationHistory,omitempty"`
	PreparationErrors       []PreparationError                     `json:"preparationErrors,omitempty"`
	RequiredSources         coverage.RequiredSources               `json:"requiredSources"`
	CatalogGates            []string                               `json:"catalogGates"`
	Requirement             RequirementDocument                    `json:"requirement"`
	Mode                    string                                 `json:"mode"`
	BaseSnapshot            string                                 `json:"baseSnapshot"`
	Candidate               string                                 `json:"candidate"`
	Settings                settings.Effective                     `json:"settings"`
	Reviews                 map[string]*PreDevelopmentReviewSeries `json:"preDevelopmentReviewSeries"`
	Plan                    *DevelopmentPlan                       `json:"developmentPlan,omitempty"`
	Topology                string                                 `json:"topology"`
	TopologyReason          string                                 `json:"topologyReason"`
	Route                   *RegularRoute                          `json:"route,omitempty"`
	Guarantee               RequirementGuaranteeActivation         `json:"requirementGuaranteeActivation"`
	Ask                     *RegularAsk                            `json:"ask,omitempty"`
	Requests                []RegularAsk                           `json:"requests"`
	Changes                 []RequirementChangeReceipt             `json:"requirementChanges"`
	ChangePending           bool                                   `json:"changePending"`
	Guard                   GuardProjection                        `json:"guard"`
	Terminal                string                                 `json:"terminal,omitempty"`
	RejectedResults         []RejectedReviewResult                 `json:"rejectedResults"`
}

// RegularEvent extends the existing event union with typed business facts. The
// operation is closed; arbitrary names never become handlers or model commands.
type RegularEvent struct {
	TaskOperation        *TaskOperationRequest    `json:"taskOperation,omitempty"`
	TaskOperationReceipt *TaskOperationReceipt    `json:"taskOperationReceipt,omitempty"`
	VerificationResult   *VerificationResult      `json:"verificationResult,omitempty"`
	WaveAdjudication     *WaveAdjudication        `json:"waveAdjudication,omitempty"`
	RepairResult         *RepairResult            `json:"repairResult,omitempty"`
	DeveloperRecovery    *DeveloperRecovery       `json:"developerRecovery,omitempty"`
	PreparationResult    *PreparationResult       `json:"preparationResult,omitempty"`
	GitClaim             *GitPreparation          `json:"gitClaim,omitempty"`
	GitReceipt           *GitPreparationReceipt   `json:"gitReceipt,omitempty"`
	Operation            string                   `json:"operation"`
	ExpectedRevision     uint64                   `json:"expectedRevision"`
	Request              *RegularRequest          `json:"request,omitempty"`
	Decision             *RegularDecision         `json:"decision,omitempty"`
	Review               *RawReviewCandidate      `json:"review,omitempty"`
	Adjudication         *ReviewAdjudication      `json:"adjudication,omitempty"`
	Correction           *ReviewCorrectionReceipt `json:"correction,omitempty"`
	Claim                *ExecutionClaim          `json:"claim,omitempty"`
	Source               *ExecutionSource         `json:"source,omitempty"`
}

// HostRuntimeContext is captured by the supported adapter before first claim.
// It is execution input, never a second lifecycle or a caller-selectable retry.
type HostRuntimeContext struct {
	Provider    string            `json:"provider"`
	Executable  string            `json:"executable"`
	Environment map[string]string `json:"environment"`
}

type ExecutionClaim struct {
	Context             *HostRuntimeContext `json:"context,omitempty"`
	PrerequisiteDigest  string              `json:"prerequisiteDigest,omitempty"`
	ActionID            string              `json:"actionId"`
	AttemptID           string              `json:"attemptId"`
	Task                runtime.TaskKey     `json:"task"`
	Provider            string              `json:"provider"`
	RequirementRevision string              `json:"requirementRevision"`
	Candidate           string              `json:"candidate"`
	PayloadDigest       string              `json:"payloadDigest"`
	Operation           string              `json:"operation"`
	Identity            string              `json:"identity,omitempty"`
}
type ExecutionSource struct {
	ActionID       string `json:"actionId"`
	AttemptID      string `json:"attemptId"`
	Provider       string `json:"provider"`
	Identity       string `json:"identity"`
	TranscriptPath string `json:"transcriptPath,omitempty"`
	Status         string `json:"status"`
	Reason         string `json:"reason,omitempty"`
}

func (v RegularEvent) validate() error {
	n := 0
	for _, set := range []bool{v.TaskOperation != nil, v.TaskOperationReceipt != nil, v.Request != nil, v.Decision != nil, v.Review != nil, v.Adjudication != nil, v.Correction != nil, v.Claim != nil, v.Source != nil, v.PreparationResult != nil, v.GitClaim != nil, v.GitReceipt != nil, v.DeveloperRecovery != nil, v.VerificationResult != nil, v.WaveAdjudication != nil, v.RepairResult != nil} {
		if set {
			n++
		}
	}
	ok := false
	switch v.Operation {
	case "TASK_OPERATION_CLAIM":
		ok = v.TaskOperation != nil
	case "TASK_OPERATION_RECEIPT":
		ok = v.TaskOperationReceipt != nil
	case "VERIFICATION_RESULT":
		ok = v.VerificationResult != nil
	case "WAVE_ADJUDICATE":
		ok = v.WaveAdjudication != nil
	case "REPAIR_RESULT":
		ok = v.RepairResult != nil
	case "DEVELOPER_RECOVERY":
		ok = v.DeveloperRecovery != nil
	case "PREPARATION_RESULT":
		ok = v.PreparationResult != nil
	case "GIT_CLAIM":
		ok = v.GitClaim != nil
	case "GIT_RECEIPT":
		ok = v.GitReceipt != nil
	case "REQUEST":
		ok = v.Request != nil
	case "DECIDE":
		ok = v.Decision != nil
	case "REVIEW_RESULT":
		ok = v.Review != nil
	case "ADJUDICATE":
		ok = v.Adjudication != nil
	case "CORRECT":
		ok = v.Correction != nil
	case "CLAIM":
		ok = v.Claim != nil
	case "SOURCE":
		ok = v.Source != nil
	}
	if n != 1 || !ok || v.ExpectedRevision == 0 {
		return fmt.Errorf("regular event: a known operation, its one typed payload and expectedRevision are required")
	}
	return nil
}
func NewRegularEvent(id EventID, v RegularEvent) (Event, error) {
	ev := Event{ID: id, Kind: KindRegular, Regular: &v}
	return ev, ev.Validate()
}
func Document(path string, data []byte) RequirementDocument {
	sum := sha256.Sum256(data)
	return RequirementDocument{Path: filepath.ToSlash(path), Revision: hex.EncodeToString(sum[:]), Text: string(data)}
}

func (e *Engine) InitRegular(view *decision.State, provider, fp, runID string, initial RegularState) error {
	// The facade admits providers through the native runtime adapter registry.
	// Protocol state preserves that binding without owning a second host list.
	if strings.TrimSpace(provider) == "" {
		return fmt.Errorf("regular: provider is required")
	}
	if _, s, err := e.load(); err != nil {
		return err
	} else if s != nil {
		return &RejectedError{Code: CodeAlreadyInitialized, Detail: "engine state already exists"}
	}
	if initial.Requirement.Path == "" || initial.Requirement.Text == "" || initial.BaseSnapshot == "" {
		return fmt.Errorf("regular: one requirement document and Git base are required")
	}
	if initial.CatalogGates == nil {
		initial.CatalogGates = []string{"complexity-gate", "implementation-quality-gate"}
	}
	initial.Reviews = map[string]*PreDevelopmentReviewSeries{}
	for _, action := range reviewActions {
		initial.Reviews[action] = &PreDevelopmentReviewSeries{Action: action, Limit: 3}
	}
	initial.Guarantee = RequirementGuaranteeActivation{Status: "INACTIVE", Source: "UNCONFIRMED"}
	s := NewState(*view)
	s.RunID = runID
	s.RunProvider = provider
	s.Regular = &initial
	s.Cost = &cost.RunCost{}
	e.refreshRegular(s)
	_, err := e.commit(s, 0, fp)
	return err
}

func (e *Engine) driveRegular(s *State, revision uint64, fp string) (*decision.Plan, uint64, error) {
	r := s.Regular
	if r.Requirement.Projection == nil && r.Terminal == "" && r.Ask == nil {
		p, err := requirements.ParseRequirementProjection(r.Requirement.Path, []byte(r.Requirement.Text))
		if err != nil {
			return nil, revision, err
		}
		r.Requirement.Projection = &p
		e.refreshRegular(s)
		n, err := e.commit(s, revision, fp)
		if err != nil {
			return nil, revision, err
		}
		revision = n
	}
	prepareProjection(r)
	if refreshPreparationRecovery(s, revision+1) {
		e.refreshRegular(s)
		n, err := e.commit(s, revision, fp)
		if err != nil {
			return nil, revision, err
		}
		revision = n
	}
	refreshVerification(s)
	if issuePreparationGit(s, revision) {
		e.refreshRegular(s)
		n, err := e.commit(s, revision, fp)
		if err != nil {
			return nil, revision, err
		}
		revision = n
	}
	e.refreshRegular(s)
	p, err := decision.Decide(&s.State, decision.Observation{}, e.cfg.Definition)
	if err != nil {
		return nil, revision, err
	}
	if p.Next.Kind == decision.KindReady {
		issued, err := e.issueRegular(s, p, revision+1, fp)
		if err != nil {
			return nil, revision, err
		}
		if len(issued) > 0 {
			e.refreshRegular(s)
			revision, err = e.commit(s, revision, fp)
			if err != nil {
				return nil, revision, err
			}
			p, err = decision.Decide(&s.State, decision.Observation{}, e.cfg.Definition)
		}
	}
	return p, revision, err
}

func (e *Engine) refreshRegular(s *State) {
	r := s.Regular
	if r == nil {
		return
	}
	v := &decision.RegularView{Prechecked: r.Requirement.Projection != nil, Confirmed: r.Requirement.Confirmed, Mode: r.Mode, TopologyConfirmed: r.Topology == "no-split", RouteConfirmed: r.Route != nil, Terminal: r.Terminal != "" && r.Terminal != "ABORTING"}
	if r.Ask != nil {
		v.Control = r.Ask.Kind
	}
	if r.ChangePending && r.Ask == nil {
		v.Control = "requirement.change"
	}
	for _, action := range reviewActions {
		series := r.Reviews[action]
		key := reviewTask(r, action)
		status := effectiveReview(r, series)
		if status == "OPEN" {
			if at, exists := s.Attempts[key]; exists {
				switch {
				case at.RetryExhausted:
					status = "RETRY_EXHAUSTED"
				case s.Tasks[key] != runtime.TaskTerminal:
					status = "RUNNING"
				}
			}
			if series.Completed >= series.Limit && status == "OPEN" {
				status = "ROUND_EXHAUSTED"
			}
		}
		if action == "product-review" {
			v.ProductStatus = status
			v.ProductTask = key
		} else {
			v.TechnicalStatus = status
			v.TechnicalTask = key
		}
	}
	for _, intent := range s.PendingHostActions {
		v.HostSteps = append(v.HostSteps, intent.Step)
	}
	sort.Slice(v.HostSteps, func(i, j int) bool { return v.HostSteps[i] < v.HostSteps[j] })
	preparationView(s, v)
	verificationView(s, v)
	s.State.Regular = v
	if r.Mode == "regular" && r.Requirement.Confirmed {
		result, err := e.regularBasis(s)
		if err != nil {
			r.Guard.Status = "BLOCKED_BUG"
			v.GuardReason = "BLOCKED_BUG"
			return
		}
		r.Guard.Basis = result
		r.Guard.UsedAttempts = len(s.RegularExecutions)
		r.Guard.UsedTokens = 0
		r.Guard.TokenCapStatus = "UNCONFIGURED"
		pending := false
		unavailable := false
		for id, x := range s.RegularExecutions {
			if !x.Claimed {
				continue
			}
			if _, ok := s.Results[id]; ok || x.Finished {
				if entry, ok := s.Cost.Dispatches[id]; ok {
					if entry.Source == cost.SourceUnavailable {
						unavailable = true
					}
				} else {
					pending = true
				}
			}
		}
		for _, entry := range s.Cost.Dispatches {
			r.Guard.UsedTokens += entry.TotalInputTokens + entry.OutputTokens
			if entry.Source == cost.SourceUnavailable {
				unavailable = true
			}
		}
		r.Guard.Status = "READY"
		if r.Guard.UsedAttempts > result.TotalAllowance {
			r.Guard.Status = "BLOCKED_BUG"
		}
		if pending {
			r.Guard.Status = "COST_PENDING"
		}
		if r.Settings.Values.TokenCap != nil {
			r.Guard.TokenCapStatus = "AVAILABLE"
			if unavailable {
				r.Guard.TokenCapStatus = "UNAVAILABLE"
			}
			if !pending && r.Guard.UsedTokens >= *r.Settings.Values.TokenCap && len(r.Guard.ContinueTasks) == 0 {
				r.Guard.Status = "TOKEN_CAP"
			}
		}
		if r.Guard.Status != "READY" {
			v.GuardReason = r.Guard.Status
		}
	}
}

func reviewTask(r *RegularState, action string) runtime.TaskKey {
	series := r.Reviews[action]
	// Semantic rounds change the scope; retries retain this exact key.
	key, _ := runtime.NewTaskKey("entry", authoring.StepID(action), "review-"+strconv.Itoa(series.Sequence+1))
	return key
}
func (e *Engine) regularBasis(s *State) (guard.Result, error) {
	r := s.Regular
	confirmed := func(id, rev string) guard.ConfirmedIdentity {
		return guard.ConfirmedIdentity{Identity: guard.Identity{ID: id, Revision: rev}, ConfirmedRevision: rev}
	}
	// Before route/topology exist they contribute no tasks. Their authoritative
	// identity explicitly records this intake state, rather than assuming full.
	route, topology, batch := "UNBOUND", "UNBOUND", "UNBOUND"
	if r.Route != nil {
		route = digestOfCanonical(r.Route)
	}
	if r.Topology != "" {
		topology = digestOfCanonical(r.TopologyReason)
	}
	if r.Plan != nil && r.Plan.ConfirmedRevision != "" {
		batch = r.Plan.Digest
	}
	b := guard.Basis{SchemaVersion: 1, Requirement: confirmed(r.Requirement.Path, r.Requirement.Revision), Batch: confirmed("batch", batch), Route: confirmed("route", route), Topology: confirmed("topology", topology), Definition: guard.Definition{Identity: guard.Identity{ID: "workflow", Revision: string(e.cfg.Definition.Version)}, ExternalSteps: []runtime.TaskKey{}}}
	for _, action := range reviewActions {
		step := runtime.TaskKey{Node: "entry", Step: authoring.StepID(action)}
		b.Definition.ExternalSteps = append(b.Definition.ExternalSteps, step)
		series := r.Reviews[action]
		for seq := 1; seq <= series.Limit; seq++ {
			key := step
			key.Scope = "review-" + strconv.Itoa(seq)
			max := 2 + series.ExtraAttempts[key.String()]
			b.ExternalTasks = append(b.ExternalTasks, guard.ExternalTask{TaskKey: key, Provenance: guard.Provenance{Kind: guard.DefinitionSource, Identity: b.Definition.Identity}, MaxAttempts: &max})
		}
	}
	preparationAllowance(s, &b)
	verificationAllowance(s, &b)
	return guard.Calculate(b)
}

// RegularExecution retains native claim/receipt facts by the existing ActionID.
// Scheduling and retry authority remain TaskKey/Attempt/PendingActions; ended
// actions leave PendingActions while this immutable history survives task reuse.
type RegularExecution struct {
	Operations         map[string]TaskOperation `json:"operations,omitempty"`
	ResumeCount        int                      `json:"resumeCount"`
	VerificationResult *VerificationResult      `json:"verificationResult,omitempty"`
	RepairResult       *RepairResult            `json:"repairResult,omitempty"`
	PreviousAction     string                   `json:"previousAction,omitempty"`
	Recovery           *DeveloperRecovery       `json:"developerRecovery,omitempty"`
	RecoveryHistory    []DeveloperRecovery      `json:"developerRecoveryHistory,omitempty"`
	ReplacementClaimed bool                     `json:"replacementClaimed"`
	PreparationResult  *PreparationResult       `json:"preparationResult,omitempty"`
	Claim              ExecutionClaim           `json:"claim"`
	Number             int                      `json:"number"`
	Claimed            bool                     `json:"claimed"`
	ResumeClaimed      bool                     `json:"resumeClaimed"`
	Identity           string                   `json:"identity,omitempty"`
	Finished           bool                     `json:"finished"`
	Source             *ExecutionSource         `json:"source,omitempty"`
}

func (e *Engine) issueRegular(s *State, p *decision.Plan, next uint64, fp string) (decision.IssuedSet, error) {
	r := s.Regular
	e.refreshRegular(s)
	if r.ChangePending || r.Ask != nil || r.Guard.Status != "READY" {
		return nil, nil
	}
	capacity := r.Settings.Values.Capacity
	for _, x := range s.RegularExecutions {
		if !x.Finished {
			capacity--
		}
	}
	if capacity < 0 {
		capacity = 0
	}
	adm := decision.Admission{Capacity: capacity}
	issued, err := decision.SelectIssued(p, adm, ackStore{})
	if err != nil {
		return nil, err
	}
	identity, err := identityOfPlan(p)
	if err != nil {
		return nil, err
	}
	for i := range issued {
		a := &issued[i]
		number := 1
		for _, x := range s.RegularExecutions {
			if x.Claim.Task == a.Task {
				number++
			}
		}
		max := preparationMax(r, a.Task)
		if number > max {
			return nil, fmt.Errorf("RETRY_EXHAUSTED: %s", a.Task.String())
		}
		a.ActionID += "/attempt-" + strconv.Itoa(number)
		if r.Guard.UsedAttempts+len(issued) > r.Guard.Basis.TotalAllowance {
			return nil, fmt.Errorf("BLOCKED_BUG: attempted issue exceeds recomputed basis")
		}
		// A new attempt replaces only the scheduling projection. The terminal old
		// execution/result and source remain immutable and counted in the ledger.
		delete(s.Attempts, a.Task)
		delete(s.Tasks, a.Task)
	}
	if err = s.issueInto(issued, next, r.Candidate, s.RunProvider, identity, e.retryMaxAttempts); err != nil {
		return nil, err
	}
	for _, a := range issued {
		at := s.Attempts[a.Task]
		number := 1
		for _, x := range s.RegularExecutions {
			if x.Claim.Task == a.Task {
				number++
			}
		}
		candidate, prerequisite := preparationClaim(r, a.Task)
		at.Bindings.Snapshot = candidate
		at.MaxAttempts = preparationMax(r, a.Task)
		s.Attempts[a.Task] = at
		c := ExecutionClaim{PrerequisiteDigest: prerequisite, ActionID: a.ActionID, AttemptID: at.ID, Task: a.Task, Provider: s.RunProvider, RequirementRevision: r.Requirement.Revision, Candidate: candidate, Operation: "spawn"}
		if c.Task.Step == "start-readiness" {
			c.PrerequisiteDigest = r.Reviews["product-review"].CurrentDigest
		}
		c.PayloadDigest = digestOfCanonical(struct {
			Action    string
			Document  RequirementDocument
			Candidate string
		}{string(a.Step), r.Requirement, c.Candidate})
		if preparationStep(a.Step) {
			c.PayloadDigest = digestOfCanonical(struct {
				Claim ExecutionClaim
				Plan  *DevelopmentPlan
				QA    map[string]*PreparedQA
			}{c, r.Plan, r.Preparation.QA})
		}
		if verificationStep(a.Step) {
			c.PayloadDigest = digestOfCanonical(struct {
				Claim  ExecutionClaim
				Wave   *VerificationWave
				Repair *RepairPlan
			}{c, currentWave(r), currentRepair(r)})
		}
		x := RegularExecution{Claim: c, Number: number}
		for _, prior := range s.RegularExecutions {
			if prior.Claim.Task == c.Task && prior.Claim.Context != nil {
				x.Claim.Context = prior.Claim.Context
				break
			}
		}
		if a.Step == "repair-worker" {
			currentRepair(r).Issued = true
		}
		if c.Task.Step == "development-worker" {
			r.Preparation.DevelopmentTask = &c.Task
			if previous, ok := latestDeveloper(s, c.Task); ok {
				x.PreviousAction = previous.Claim.ActionID
				x.Claim.Context = previous.Claim.Context
				if previous.Identity != "" {
					x.Claim.Operation = "resume"
					x.Claim.Identity = previous.Identity
					x.Identity = previous.Identity
				}
			}
			x.Claim.PayloadDigest = digestOfCanonical(struct {
				Claim          ExecutionClaim
				PreviousAction string
			}{x.Claim, x.PreviousAction})
		}
		s.RegularExecutions[a.ActionID] = x
	}
	return issued, nil
}

func (e *Engine) admitRegularEvent(ev Event, s *State, revision uint64) (Acceptance, error) {
	v := ev.Regular
	if v == nil {
		return Acceptance{}, fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: regular accepts typed regular events only")
	}
	if v.ExpectedRevision != revision {
		return Acceptance{}, &RejectedError{Code: CodeStaleFreshness, Detail: "regular expectedRevision is not current"}
	}
	r := s.Regular
	if r.Terminal != "" && (r.Terminal != "ABORTING" || (v.Operation != "SOURCE" && v.Operation != "REVIEW_RESULT" && v.Operation != "PREPARATION_RESULT" && v.Operation != "VERIFICATION_RESULT" && v.Operation != "REPAIR_RESULT" && v.Operation != "TASK_OPERATION_RECEIPT")) {
		return Acceptance{}, fmt.Errorf("regular run is terminal")
	}
	var err error
	status := "ACCEPTED"
	switch v.Operation {
	case "TASK_OPERATION_CLAIM":
		err = e.claimTaskOperation(s, *v.TaskOperation, revision)
	case "TASK_OPERATION_RECEIPT":
		err = e.acceptTaskOperation(s, *v.TaskOperationReceipt, revision)
	case "VERIFICATION_RESULT":
		status, err = e.acceptVerification(s, *v.VerificationResult, revision)
	case "WAVE_ADJUDICATE":
		err = adjudicateWave(r, *v.WaveAdjudication, revision)
		if err == nil {
			reconcileDismissedQA(s, *v.WaveAdjudication)
		}
	case "REPAIR_RESULT":
		status, err = e.acceptRepair(s, *v.RepairResult, revision)
	case "DEVELOPER_RECOVERY":
		err = acceptDeveloperRecovery(s, *v.DeveloperRecovery)
	case "PREPARATION_RESULT":
		status, err = e.acceptPreparation(s, *v.PreparationResult, revision)
	case "GIT_CLAIM":
		err = e.claimGit(s, *v.GitClaim)
	case "GIT_RECEIPT":
		err = e.acceptGit(s, *v.GitReceipt)
	case "REQUEST":
		err = e.regularRequest(s, string(ev.ID), *v.Request, revision)
	case "DECIDE":
		err = e.regularDecision(s, *v.Decision, revision, string(ev.ID))
	case "REVIEW_RESULT":
		status, err = e.acceptRegularReview(s, *v.Review, revision)
	case "ADJUDICATE":
		err = adjudicateRegularReview(r, *v.Adjudication, revision)
	case "CORRECT":
		err = correctRegularReview(r, *v.Correction, revision)
	case "CLAIM":
		err = e.claimRegular(s, *v.Claim)
	case "SOURCE":
		err = e.sourceRegular(s, *v.Source)
	}
	if err != nil {
		return Acceptance{}, err
	}
	// Stale unclaimed work can retire immediately. A claimed developer first
	// stops through the existing host action and source settlement; only then
	// release its scheduling slot, retaining its identity and spent attempts.
	for id, x := range s.RegularExecutions {
		stale := x.Claim.RequirementRevision != r.Requirement.Revision || !currentPreparationClaim(r, x.Claim) || (x.Claim.Task.Step == "start-readiness" && x.Claim.PrerequisiteDigest != r.Reviews["product-review"].CurrentDigest)
		if stale && x.Claim.Task.Step == "development-worker" && x.Claimed && !x.Finished && (x.Source == nil || x.Source.Status == "RUNNING") {
			target := &AgentHostAction{WorkerActionID: id, AttemptID: x.Claim.AttemptID, Identity: x.Identity, Reason: "execution binding superseded"}
			intentID := "hact:" + id + ":terminate"
			if _, exists := s.PendingHostActions[intentID]; !exists {
				s.PendingHostActions[intentID] = HostActionIntent{ActionID: intentID, Operation: HostActionTerminateAgent, Step: x.Claim.Task.Step, Terminate: target, PayloadDigest: digestOfCanonical(target), Correlation: x.Identity, Revision: revision + 1}
			}
		}
		if stale && !x.Finished && (!x.Claimed || (x.Claim.Task.Step == "development-worker" && x.Source != nil && x.Source.Status != "RUNNING")) {
			x.Finished = true
			delete(s.PendingActions, id)
			s.RegularExecutions[id] = x
			s.Tasks[x.Claim.Task] = runtime.TaskTerminal
			at := s.Attempts[x.Claim.Task]
			at.RetryExhausted = x.Number >= preparationMax(r, x.Claim.Task)
			s.Attempts[x.Claim.Task] = at
		}
	}
	for id, x := range s.RegularExecutions {
		if !x.Finished || x.Source == nil || x.Source.Status == "RUNNING" {
			continue
		}
		if _, settled := s.Cost.Dispatches[id]; settled {
			continue
		}
		if err := settleRegularSource(s, *x.Source, x); err != nil {
			return Acceptance{}, err
		}
	}
	if r.Terminal == "ABORTING" {
		done := true
		for id, x := range s.RegularExecutions {
			if !x.Finished {
				done = false
			}
			if x.Claimed {
				if _, settled := s.Cost.Dispatches[id]; !settled {
					done = false
				}
			}
		}
		if done && len(s.PendingHostActions) == 0 {
			r.Terminal = "ABORTED"
		}
	}
	prepareProjection(r)
	refreshPreparationRecovery(s, revision+1)
	refreshVerification(s)
	// This ordering is one persistence transaction. A returned result cannot
	// release capacity until its source has settled and guard has been recomputed.
	e.refreshRegular(s)
	var refill []string
	plan, planErr := decision.Decide(&s.State, decision.Observation{}, e.cfg.Definition)
	if planErr != nil {
		return Acceptance{}, planErr
	}
	if plan.Next.Kind == decision.KindReady {
		issued, issueErr := e.issueRegular(s, plan, revision+1, r.Candidate)
		if issueErr != nil {
			return Acceptance{}, issueErr
		}
		for _, a := range issued {
			refill = append(refill, a.ActionID)
		}
		e.refreshRegular(s)
	}
	return Acceptance{Refill: refill, EventID: string(ev.ID), Kind: string(ev.Kind), Status: status, Revision: revision + 1}, nil
}

func (e *Engine) claimRegular(s *State, c ExecutionClaim) error {
	r := s.Regular
	x, ok := s.RegularExecutions[c.ActionID]
	if !ok {
		return fmt.Errorf("UNKNOWN_ACTION: action was not issued")
	}
	want := x.Claim
	want.Operation = c.Operation
	want.Identity = c.Identity
	if want.Context == nil && !x.Claimed {
		want.Context = c.Context
	}
	if !reflect.DeepEqual(c, want) {
		return fmt.Errorf("INTENT_MISMATCH: claim must match action, TaskKey, Attempt, provider, requirement, candidate and payload")
	}
	if r.ChangePending || r.Ask != nil || r.Terminal != "" || c.RequirementRevision != r.Requirement.Revision || !currentPreparationClaim(r, c) {
		return fmt.Errorf("EVENT_NOT_CURRENT: execution binding is cancelled or stale")
	}
	if c.Task.Step == "start-readiness" && (effectiveReview(r, r.Reviews["product-review"]) != "PASS" || c.PrerequisiteDigest != r.Reviews["product-review"].CurrentDigest) {
		return fmt.Errorf("EVENT_NOT_CURRENT: prerequisite product review changed")
	}
	e.refreshRegular(s)
	// Replacement consumes the already authorized native recovery claim. Its
	// evidence check below still runs; consuming that claim's token continue must
	// not require a second authorization for the same attempt.
	if r.Guard.Status != "READY" && !(c.Operation == "replace" && x.Claimed && r.Guard.Status == "TOKEN_CAP") {
		return fmt.Errorf("%s: execution is blocked", r.Guard.Status)
	}
	if c.Operation != "replace" && r.Settings.Values.TokenCap != nil && r.Guard.UsedTokens >= *r.Settings.Values.TokenCap {
		allowed := false
		for _, task := range r.Guard.ContinueTasks {
			if task == c.Task {
				allowed = true
			}
		}
		if !allowed {
			return fmt.Errorf("TOKEN_CAP: continue authorization is bound to another TaskKey")
		}
	}
	if c.Operation == "spawn" {
		if c.Task.Step == "development-worker" && x.Claim.Identity != "" {
			return fmt.Errorf("DEVELOPMENT_CONTINUITY: unfinished batch must resume its bound native identity")
		}
		if x.Claimed || x.Finished || c.Identity != "" {
			return fmt.Errorf("DUPLICATE_ACTION: spawn already claimed")
		}
		x.Claimed = true
	} else if c.Operation == "resume" {
		continuation := c.Task.Step == "development-worker" && !x.Claimed && x.Claim.Operation == "resume" && c.Identity == x.Claim.Identity
		if (!x.Claimed && !continuation) || x.Finished || x.Identity == "" || c.Identity != x.Identity || x.ResumeClaimed {
			return fmt.Errorf("INTENT_MISMATCH: resume needs the bound live identity")
		}
		if continuation {
			x.Claimed = true
		} else {
			x.ResumeClaimed = true
			x.ResumeCount++
			if x.Recovery != nil {
				x.RecoveryHistory = append(x.RecoveryHistory, *x.Recovery)
				x.Recovery = nil
			}
		}
		target := &AgentHostAction{WorkerActionID: c.ActionID, AttemptID: c.AttemptID, Identity: c.Identity, Reason: "Resume the bound interrupted native session"}
		intent := HostActionIntent{ActionID: regularResumeIntentID(x), Operation: HostActionResumeAgent, Step: c.Task.Step, Resume: target, PayloadDigest: digestOfCanonical(target), Correlation: c.Identity}
		s.PendingHostActions[intent.ActionID] = intent
	} else if c.Operation == "replace" && c.Task.Step == "development-worker" {
		if err := claimDeveloperReplacement(s, &x, c); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: unknown execution operation")
	}
	x.Claim.Context = c.Context
	s.RegularExecutions[c.ActionID] = x
	if c.Operation == "spawn" || c.Operation == "resume" || c.Operation == "replace" {
		kept := r.Guard.ContinueTasks[:0]
		for _, key := range r.Guard.ContinueTasks {
			if key != c.Task {
				kept = append(kept, key)
			}
		}
		r.Guard.ContinueTasks = kept
	}
	return nil
}
func (e *Engine) sourceRegular(s *State, src ExecutionSource) error {
	x, ok := s.RegularExecutions[src.ActionID]
	if !ok || !x.Claimed {
		return fmt.Errorf("UNKNOWN_ACTION: source requires a claimed execution")
	}
	if src.AttemptID != x.Claim.AttemptID || src.Provider != x.Claim.Provider || (src.Identity == "" && src.Status != "UNAVAILABLE") {
		return fmt.Errorf("INTENT_MISMATCH: source binding mismatch")
	}
	if x.Identity != "" && x.Identity != src.Identity {
		return fmt.Errorf("INTENT_MISMATCH: source identity changed")
	}
	if src.Status != "RUNNING" && src.Status != "STOPPED" && src.Status != "UNAVAILABLE" {
		return fmt.Errorf("source status must be RUNNING, STOPPED or UNAVAILABLE")
	}
	if src.Status == "UNAVAILABLE" && strings.TrimSpace(src.Reason) == "" {
		return fmt.Errorf("source unavailable requires an explicit reason")
	}
	if src.Status == "RUNNING" && x.Claim.Task.Step == "development-worker" {
		if x.Claim.Identity != "" && src.Identity != x.Claim.Identity && !x.ReplacementClaimed {
			return fmt.Errorf("DEVELOPMENT_CONTINUITY: result source is not the bound developer")
		}
		for id, prior := range s.RegularExecutions {
			if id != src.ActionID && prior.Claim.Task.Step == "development-worker" && prior.Claim.Task != x.Claim.Task && prior.Identity == src.Identity {
				return fmt.Errorf("INTENT_MISMATCH: development batches require fresh independent native sessions")
			}
		}
	}
	if src.Identity != "" {
		event := LifecycleStart
		if src.Status != "RUNNING" {
			event = LifecycleStop
		}
		record := LifecycleEventRecord{Provider: src.Provider, Correlation: src.Identity, Identity: src.Identity, Event: event}
		record.Digest = digestOfCanonical(record)
		exists := false
		for _, old := range s.LifecycleEvents {
			if old.Digest == record.Digest {
				exists = true
			}
		}
		if !exists {
			s.LifecycleEvents = append(s.LifecycleEvents, record)
		}
		if src.Status == "RUNNING" {
			receipt := SpawnReceipt{ActionID: src.ActionID, Provider: src.Provider, Correlation: src.Identity, Status: SpawnStatusSpawned, LifecycleIdentity: src.Identity}
			receipt.Digest = digestOfCanonical(receipt)
			s.SpawnReceipts[src.ActionID] = receipt
			intentID := regularResumeIntentID(x)
			if intent, ok := s.PendingHostActions[intentID]; ok {
				receipt := HostActionReceipt{ActionID: intentID, Operation: HostActionResumeAgent, Step: x.Claim.Task.Step, Provider: src.Provider, Correlation: src.Identity, PayloadDigest: intent.PayloadDigest, Status: HostActionStatusExecuted, LifecycleEvidence: &LifecycleEvidence{Identity: src.Identity, Event: LifecycleStart}}
				receipt.Digest = digestOfCanonical(receipt)
				s.HostActionReceipts[intentID] = receipt
				delete(s.PendingHostActions, intentID)
			}
			intentID = "hact:" + src.ActionID + ":replace"
			if intent, ok := s.PendingHostActions[intentID]; ok && x.ReplacementClaimed {
				receipt := HostActionReceipt{ActionID: intentID, Operation: intent.Operation, Step: x.Claim.Task.Step, Provider: src.Provider, Correlation: intent.Correlation, PayloadDigest: intent.PayloadDigest, Status: HostActionStatusExecuted, AdapterOperation: "developer.replace", LifecycleEvidence: &LifecycleEvidence{Identity: src.Identity, Event: LifecycleStart}}
				receipt.Digest = digestOfCanonical(receipt)
				s.HostActionReceipts[intentID] = receipt
				delete(s.PendingHostActions, intentID)
			}
		}
		if s.lifecyclePaired(src.Identity, src.Identity) {
			s.LifecycleVerified[src.Identity] = LifecycleVerification{Correlation: src.Identity, Identity: src.Identity, Provider: src.Provider}
		}
	}
	if src.Status != "RUNNING" {
		intentID := "hact:" + src.ActionID + ":terminate"
		if intent, ok := s.PendingHostActions[intentID]; ok {
			receipt := HostActionReceipt{ActionID: intentID, Operation: HostActionTerminateAgent, Step: x.Claim.Task.Step, Provider: src.Provider, Correlation: src.Identity, PayloadDigest: intent.PayloadDigest, Status: HostActionStatusExecuted, LifecycleEvidence: &LifecycleEvidence{Identity: src.Identity, Event: LifecycleStop}}
			receipt.Digest = digestOfCanonical(receipt)
			s.HostActionReceipts[intentID] = receipt
			delete(s.PendingHostActions, intentID)
		}
	}
	if src.Status != "RUNNING" {
		intentID := regularResumeIntentID(x)
		if intent, ok := s.PendingHostActions[intentID]; ok {
			receipt := HostActionReceipt{ActionID: intentID, Operation: HostActionResumeAgent, Step: x.Claim.Task.Step, Provider: src.Provider, Correlation: src.Identity, PayloadDigest: intent.PayloadDigest, Status: HostActionStatusFailed, FailureClass: authoring.FailureTransientEngine}
			receipt.Digest = digestOfCanonical(receipt)
			s.HostActionFailures[intentID] = append(s.HostActionFailures[intentID], receipt)
			delete(s.PendingHostActions, intentID)
		}
		intentID = "hact:" + src.ActionID + ":replace"
		if intent, ok := s.PendingHostActions[intentID]; ok && x.ReplacementClaimed {
			receipt := HostActionReceipt{ActionID: intentID, Operation: intent.Operation, Step: x.Claim.Task.Step, Provider: src.Provider, Correlation: intent.Correlation, PayloadDigest: intent.PayloadDigest, Status: HostActionStatusFailed, FailureClass: authoring.FailureTransientEngine, AdapterOperation: "developer.replace"}
			receipt.Digest = digestOfCanonical(receipt)
			s.HostActionFailures[intentID] = append(s.HostActionFailures[intentID], receipt)
			delete(s.PendingHostActions, intentID)
		}
	}
	x.Identity = src.Identity
	x.Source = &src
	s.RegularExecutions[src.ActionID] = x
	if src.Status == "RUNNING" {
		return nil
	}
	return nil
}

func settleRegularSource(s *State, src ExecutionSource, x RegularExecution) error {
	entry := cost.DispatchCost{Target: string(x.Claim.Task.Step), Kind: "action", Source: cost.SourceUnavailable, TranscriptPath: src.TranscriptPath, SourceReason: src.Reason}
	if src.Status == "STOPPED" {
		if err := checkNativeTranscriptIdentity(src.Provider, src.TranscriptPath, src.Identity); err != nil {
			return err
		}
		usage, err := cost.ParseTranscript(src.Provider, src.TranscriptPath)
		if err != nil {
			return fmt.Errorf("COST_PENDING: %w; explicitly record UNAVAILABLE only after source diagnosis", err)
		}
		entry.Source = cost.SourceTranscript
		entry.SourceReason = ""
		entry.TotalInputTokens = usage.InputCacheHitTokens + usage.InputCacheMissTokens
		entry.InputCacheHitTokens = usage.InputCacheHitTokens
		entry.InputCacheMissTokens = usage.InputCacheMissTokens
		entry.OutputTokens = usage.OutputTokens
		var after time.Time
		// A resumed native transcript contains all preceding turns. Charge only
		// usage not already present in the existing per-action cost projection.
		for id, prior := range s.RegularExecutions {
			if id == src.ActionID || prior.Identity != src.Identity || prior.Claim.Provider != x.Claim.Provider {
				continue
			}
			settled, ok := s.Cost.Dispatches[id]
			if !ok || settled.Source != cost.SourceTranscript {
				continue
			}
			entry.InputCacheHitTokens -= settled.InputCacheHitTokens
			entry.InputCacheMissTokens -= settled.InputCacheMissTokens
			entry.OutputTokens -= settled.OutputTokens
			entry.TotalInputTokens -= settled.TotalInputTokens
			if settled.Timing != nil {
				ended, _ := time.Parse(time.RFC3339Nano, settled.Timing.CompletedAt)
				if ended.After(after) {
					after = ended
				}
			}
		}
		if entry.InputCacheHitTokens < 0 || entry.InputCacheMissTokens < 0 || entry.OutputTokens < 0 {
			return fmt.Errorf("COST_PENDING: native transcript is smaller than its settled session usage")
		}
		if timing, err := nativeTranscriptTiming(src.Provider, src.TranscriptPath, after); err == nil {
			entry.Timing = &timing
		}
	}
	cost.Record(s.Cost, src.ActionID, entry)
	return nil
}

func sortedStrings(input []string) []string {
	out := append([]string(nil), input...)
	sort.Strings(out)
	return out
}

func RegularFreshness(revision uint64, id string) string {
	return freshnessToken(revision, RequestID(id))
}

// regularCommitter binds the source bytes used by this submission to the same
// CAS transaction as the result, cost projection, guard and refill. No provider
// polling or network operation occurs while the store lock is held.
func (e *Engine) regularCommitter(s *State, ev Event, fingerprint string) (func(*State, uint64) (uint64, error), error) {
	paths := map[string]bool{}
	for id, x := range s.RegularExecutions {
		if _, settled := s.Cost.Dispatches[id]; !settled && x.Source != nil && x.Source.Status == "STOPPED" {
			paths[x.Source.TranscriptPath] = true
		}
	}
	if ev.Regular != nil && ev.Regular.Source != nil && ev.Regular.Source.Status == "STOPPED" {
		paths[ev.Regular.Source.TranscriptPath] = true
	}
	if ev.Regular != nil && ev.Regular.TaskOperationReceipt != nil && ev.Regular.TaskOperationReceipt.TranscriptPath != "" {
		paths[ev.Regular.TaskOperationReceipt.TranscriptPath] = true
	}
	if len(paths) == 0 {
		return func(next *State, revision uint64) (uint64, error) { return e.commit(next, revision, fingerprint) }, nil
	}
	collect := func(base string) (string, error) {
		observed := map[string]string{"workflow": base}
		for path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				return "", fmt.Errorf("COST_PENDING: %w", err)
			}
			sum := sha256.Sum256(data)
			observed[path] = hex.EncodeToString(sum[:])
		}
		return digestOfCanonical(observed), nil
	}
	expected, err := collect(fingerprint)
	if err != nil {
		return nil, err
	}
	return func(next *State, revision uint64) (uint64, error) {
		result, err := e.store.Save(persistence.Transaction{ExpectedRevision: revision, ExpectedFingerprint: expected, CollectFingerprint: func() (string, error) {
			base, err := e.collect()
			if err != nil {
				return "", err
			}
			return collect(base)
		}, Content: next})
		return result.Revision, err
	}, nil
}

// RegularAvailableActions is a read-only projection of currently legal request
// families. Each request still validates its concrete payload and current token.
func RegularAvailableActions(s *State) []string {
	r := s.Regular
	if r == nil || (r.Terminal != "" && r.Terminal != "ABORTING") {
		return nil
	}
	if r.Terminal == "ABORTING" {
		return []string{"drive"}
	}
	if r.Ask != nil {
		return []string{"submit:DECIDE", "submit:REQUEST:requirement-change", "submit:REQUEST:abort"}
	}
	actions := []string{"submit:REQUEST:abort", "submit:REQUEST:requirement-change"}
	if r.ChangePending {
		return append(actions, "submit:REQUEST:apply-requirement")
	}
	if !r.Requirement.Confirmed {
		if r.Requirement.Projection != nil {
			actions = append(actions, "submit:REQUEST:confirm-requirement")
		}
		return actions
	}
	if r.Mode == "" {
		return append(actions, "submit:REQUEST:mode")
	}
	if r.Mode != "regular" {
		return actions
	}
	actions = append(actions, "submit:REQUEST:adopt-external")
	if r.Verification != nil {
		actions = append(actions, "submit:WAVE_ADJUDICATE", "submit:REQUEST:verification-retry", "submit:REQUEST:authorize-wave")
	}
	if r.Preparation != nil {
		actions = append(actions, "submit:REQUEST:plan-change", "submit:REQUEST:qa-suggestion", "submit:REQUEST:qa-revise", "submit:REQUEST:development-rework", "submit:REQUEST:preparation-retry")
	}
	finished := true
	for _, x := range s.RegularExecutions {
		if !x.Finished {
			finished = false
		}
		if x.Claimed {
			actions = uniqueAppend(actions, "drive:resume")
		} else if !x.Finished {
			actions = uniqueAppend(actions, "drive:action-id")
		}
	}
	if finished && r.Guard.Status != "COST_PENDING" {
		actions = append(actions, "submit:REQUEST:reset", "submit:REQUEST:jump")
	}
	if r.Guard.Status == "TOKEN_CAP" {
		actions = append(actions, "submit:REQUEST:continue")
	}
	for _, action := range reviewActions {
		series := r.Reviews[action]
		status := effectiveReview(r, series)
		if status == "OPERATOR" {
			actions = uniqueAppend(actions, "submit:ADJUDICATE")
		}
		if status == "FINDINGS" {
			actions = uniqueAppend(actions, "submit:REQUEST:finding")
		}
		if series.CurrentDigest != "" {
			actions = uniqueAppend(actions, "submit:CORRECT")
		}
		if series.Completed >= series.Limit {
			actions = uniqueAppend(actions, "submit:REQUEST:authorize-round")
		} else if status == "PASS" || status == "FINDINGS" {
			actions = uniqueAppend(actions, "submit:REQUEST:review-again")
		}
		if at, ok := s.Attempts[reviewTask(r, action)]; ok && at.RetryExhausted {
			actions = uniqueAppend(actions, "submit:REQUEST:authorize-retry")
		}
	}
	if effectiveReview(r, r.Reviews["product-review"]) == "PASS" && effectiveReview(r, r.Reviews["start-readiness"]) == "PASS" && r.Plan != nil {
		actions = append(actions, "submit:REQUEST:topology")
		if r.Topology == "no-split" {
			if r.Route == nil {
				actions = append(actions, "submit:REQUEST:route")
			} else {
				actions = append(actions, "submit:REQUEST:route-add", "submit:REQUEST:route-reselect")
			}
		}
	}
	return actions
}

func regularResumeIntentID(x RegularExecution) string {
	id := "hact:" + x.Claim.ActionID + ":resume"
	if x.ResumeCount > 1 {
		id += fmt.Sprintf(":%d", x.ResumeCount)
	}
	return id
}
