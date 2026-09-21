package harness

// K5 — the harness-v1 composition layer (plan §1, §8, §13.2 K5 row). This
// file assembles the accepted components over the runner kernel and adds
// nothing below them: the kernel (internal/runner) owns scheduling,
// transactions, confirmations and recovery; the supervisor (Supervise) owns
// process execution; the adapters behind RunNative own the host protocols;
// domain.go owns the business judgements; retention.go owns the terminal
// archiving judgements. The kernel never imports this package — the
// dependency runs one way only.
//
// The phase graph is the plan §1 harness-v1 list, spelled as runner tasks:
//
//	confirm (gate) → product-review ∥ technical-review
//	  → development ∥ (qa-design → qa-review)
//	  → candidate (candidateAdmission) → approve (gate) → gate-review → terminal
//
// The four mechanisms the K5 line names all travel kernel paths: the
// confirmations are decisions rows settled by Store.Answer, the parallel join
// is the dependency graph under bounded Parallelism, the candidate gate and
// the repair loop are Store.RequestRevision/ReviseTask generations, and
// takeover is the Runner.Takeover consult of compose_takeover.go.
//
// Layout of a composition run directory (all composition-owned except the
// kernel's own files):
//
//	run.db, run.lock            the kernel's store (runner.Open)
//	compose-specs.json          the current task inputs (rewritten before revisions)
//	outcomes/<task>-<gen>.json  one business outcome per executed generation
//	jobs/<task>/<inv>/turn-<n>/ one supervised native job per turn

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"formal-gates/internal/runner"
)

// composeStageIDs are the fixed task ids of the harness-v1 phase list. The
// order is the graph's causal order; the ids are stable because outcomes and
// job directories are named after them.
const (
	stageConfirm       = "confirm"
	stageProductReview = "product-review"
	stageTechnicalRev  = "technical-review"
	stageDevelopment   = "development"
	stageQADesign      = "qa-design"
	stageQAReview      = "qa-review"
	stageCandidate     = "candidate"
	stageApprove       = "approve"
	stageGateReview    = "gate-review"
)

// composeStage is one stage of the fixed graph. Role empty means a local
// stage: its executor is a plain function (a gate's passage, or the
// candidateAdmission judgement over upstream outcomes), not a native call.
type composeStage struct {
	TaskID   string
	Name     string
	Role     string
	ReadOnly bool
	Depends  []string
	Gate     *runner.Confirmation
}

// composeGraph returns the stage list with its dependency edges filled in.
// Edges are fixed data derived once, so the graph can never drift between
// CreateRun and a later Run's spec set.
func composeGraph() []composeStage {
	stages := []composeStage{
		{TaskID: stageConfirm, Name: "requirement and plan confirmation",
			Gate: &runner.Confirmation{
				Options: []string{"approve", "abort"},
				Allow:   "approve",
				Body:    []byte("Approve the requirement and the plan before any review runs."),
			}},
		{TaskID: stageProductReview, Name: "product review", Role: "product-review", ReadOnly: true},
		{TaskID: stageTechnicalRev, Name: "technical review", Role: "technical-review", ReadOnly: true},
		{TaskID: stageDevelopment, Name: "development", Role: "development", ReadOnly: false},
		{TaskID: stageQADesign, Name: "QA design", Role: "qa-design", ReadOnly: true},
		{TaskID: stageQAReview, Name: "QA review", Role: "qa-review", ReadOnly: true},
		{TaskID: stageCandidate, Name: "candidate admission"},
		{TaskID: stageApprove, Name: "approval command and gate review",
			Gate: &runner.Confirmation{
				Options: []string{"approve", "abort"},
				Allow:   "approve",
				Body:    []byte("Approve the frozen candidate and the gate review before the final check."),
			}},
		{TaskID: stageGateReview, Name: "gate review", Role: "complexity-gate", ReadOnly: true},
	}
	deps := map[string][]string{
		stageProductReview: {stageConfirm},
		stageTechnicalRev:  {stageConfirm},
		stageDevelopment:   {stageProductReview, stageTechnicalRev},
		stageQADesign:      {stageProductReview, stageTechnicalRev},
		stageQAReview:      {stageQADesign},
		stageCandidate:     {stageDevelopment, stageQAReview},
		stageApprove:       {stageCandidate},
		stageGateReview:    {stageApprove},
	}
	for i := range stages {
		stages[i].Depends = deps[stages[i].TaskID]
	}
	return stages
}

// NativeCall describes one supervised native call the composition is about to
// make, so the deployment can hand back the captured binding for exactly that
// call. Attempt is the 1-based turn number of this invocation: continuations
// (worker status CONTINUE) re-prompt the same session under the next attempt
// without touching the task, its input or its call records (§8.1).
type NativeCall struct {
	TaskID     string
	Generation int64
	Attempt    int
	JobDir     string
}

// ComposeConfig is everything the composition needs from its caller. Binding
// is the only host-side input: production returns the one captured binding
// (for example dshCaptureBinding); tests return scripted peers.
type ComposeConfig struct {
	// Dir is the run directory: the kernel's store plus the composition's
	// outcomes and job directories.
	Dir string
	// RunID names the run inside Dir; a reopened composition re-sends it.
	RunID string
	// Requirement is the requirement text every stage brief derives from.
	Requirement string
	// Workspace is the native workspace root the hosts run in (created if
	// missing). The harness-v1 scope is one local workspace, provided by the
	// caller exactly as the spec's project field is.
	Workspace string
	// Binary is the formal-gates binary that provides `harness supervise`
	// and `harness native`. Empty means FORMAL_GATES_HARNESS_BINARY, then
	// os.Executable.
	Binary string
	// Binding supplies the captured host binding for one native call.
	Binding func(NativeCall) (map[string]any, error)
	// RepairLimit is the spec's repair limit: the default allowance bound on
	// RequestRevision's first use. Zero means zero — the first repair round
	// already asks the user.
	RepairLimit int
	// Parallelism bounds concurrent ready tasks; zero runs one at a time.
	// Two is the harness-v1 shape (review pair, development/QA pair).
	Parallelism int
}

// Compose drives one harness-v1 run over the kernel. Drive advances the run
// until a user input is needed (a process confirmation or a held native
// interaction) or a terminal judgment is reached; Answer and the accepted
// native-respond path deliver inputs; Cancel settles in-flight calls.
type Compose struct {
	cfg    ComposeConfig
	binary string
	store  *runner.Store
	runner *runner.Runner

	baseCtx    context.Context
	baseCancel context.CancelFunc

	// specsMu guards specs — Run's spec set on this process — the drive
	// loop's progress signature, the surfaced-question bookkeeping, and the
	// durable repair-routing markers.
	specsMu       sync.Mutex
	specs         []runner.TaskSpec
	lastSignature string
	// surfaced remembers every process confirmation this process presented
	// (its allowing choice) and how it was answered. The kernel's settle
	// order reports any still-pending gate before a refusal, so a premature
	// gate wait would otherwise mask the user's abort; the composition reads
	// its own presentation record for that judgment.
	surfaced map[string]surfacedAsk
	// routedMarkers is the durable idempotence of the repair routing: the
	// target generation each task's last routed refusal was taken at
	// (compose-routed.json), so a refusal drives one revision exactly.
	routedMarkers map[string]int64
	// specsRebuilt marks that the saved spec cache was refused by the kernel
	// and the outcome-trail derivation took over (at most once).
	specsRebuilt bool

	// terminalStatus is the last terminal judgment (HARNESS_COMPLETE,
	// ABORTED, or "" while the run is not terminal); Archive reads it.
	terminalStatus string

	// mu guards the live supervised calls, the surfaced-hold dedup, and the
	// takeover consult's live observation facts.
	mu        sync.Mutex
	live      map[string]*liveJob
	announced map[string]bool
	heldCh    chan heldNotice
	// observed maps tasks whose pending invocations the consult kept under
	// observation to those invocations; classify reads it because the
	// kernel's settle order reports a premature gate wait before the
	// observation, and the drive must not present the approval gate while a
	// previous owner's call is still in flight.
	observed map[string]string

	// runMu guards the single in-flight Run goroutine.
	runMu   sync.Mutex
	runDone chan error // non-nil while a Run is advancing
}

// liveJob is one supervised native call this process started and has not yet
// reaped: the supervisor command (for cancellation) and its process group.
type liveJob struct {
	taskID string
	dir    string
	cmd    *exec.Cmd
	pgid   int
}

// heldNotice is one held native interaction the watcher surfaced; Drive
// re-checks it is still pending before presenting it.
type heldNotice struct {
	taskID    string
	jobDir    string
	requestID string
}

// ComposeStatus is Drive's stop classification.
type ComposeStatus string

const (
	// StatusWaitingDecision: the run stopped on an unanswered process
	// confirmation (a gate, the repair-allowance ask or a restart ask).
	StatusWaitingDecision ComposeStatus = "waiting-decision"
	// StatusWaitingInteraction: a host held a client-directed request for the
	// user; answer through NativeRespond while the original call waits.
	StatusWaitingInteraction ComposeStatus = "waiting-interaction"
	// StatusObserved: a previous owner's call is in flight under observation;
	// Drive again once it settles.
	StatusObserved ComposeStatus = "observed"
	// StatusComplete: every task finished and the terminal judgment accepted
	// the chain (Terminal HARNESS_COMPLETE).
	StatusComplete ComposeStatus = "complete"
	// StatusAborted: the user refused a gate, a repair round or a restart;
	// Terminal ABORTED.
	StatusAborted ComposeStatus = "aborted"
	// StatusBlocked: a worker reported BLOCKED — a wait-for-disposition fact,
	// not a terminal and never a PASS.
	StatusBlocked ComposeStatus = "blocked"
	// StatusNeedsRepair: unresolved business findings the routing cannot
	// repair on its own; the run is not terminal.
	StatusNeedsRepair ComposeStatus = "needs-repair"
	// StatusFailed: the drive loop stopped making progress; Err carries why.
	StatusFailed ComposeStatus = "failed"
)

// ComposeState is one Drive stop: what the run waits for, or its terminal.
type ComposeState struct {
	Status   ComposeStatus
	Terminal string // HARNESS_COMPLETE, ABORTED, or "" while not terminal
	Pending  []runner.DecisionView
	Held     []HeldInteraction
	Err      error
}

// HeldInteraction is one host-held request routed to the user (§6.2): the
// original call stays alive while the decision is delivered through
// NativeRespond against the job directory.
type HeldInteraction struct {
	TaskID    string
	JobDir    string
	RequestID string
	Method    string
	Summary   string
	Options   any
}

// JobSettlement is one cancelled supervised call's receipt fact.
type JobSettlement struct {
	TaskID   string
	JobDir   string
	ExitCode int
	Error    string
}

// Terminal status names, shared with the retention judgements.
const (
	statusHarnessComplete = "HARNESS_COMPLETE"
	statusAborted         = "ABORTED"
)

// ComposeHarnessV1 opens (or creates) the composition run in cfg.Dir. A fresh
// directory gets the run, its tasks, edges and gates in one CreateRun; an
// existing one is adopted with the saved spec cache (falling back to the
// deterministic outcome trail if the kernel refuses its digests), because the
// kernel binds every task input at creation and a reopened owner must re-send
// exactly what the run now declares.
func ComposeHarnessV1(cfg ComposeConfig) (*Compose, error) {
	if cfg.Dir == "" || cfg.RunID == "" || cfg.Requirement == "" || cfg.Binding == nil {
		return nil, fmt.Errorf("compose: Dir, RunID, Requirement and Binding are required")
	}
	if cfg.Workspace == "" {
		cfg.Workspace = filepath.Join(cfg.Dir, "workspace")
	}
	if err := os.MkdirAll(cfg.Workspace, 0o755); err != nil {
		return nil, err
	}
	binary := cfg.Binary
	if binary == "" {
		binary = os.Getenv("FORMAL_GATES_HARNESS_BINARY")
	}
	if binary == "" {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		binary = exe
	}
	store, err := runner.Open(cfg.Dir)
	if err != nil {
		return nil, err
	}
	specs, err := initialSpecs(cfg.Requirement)
	if err != nil {
		store.Close()
		return nil, err
	}
	c := &Compose{
		cfg:           cfg,
		binary:        binary,
		store:         store,
		specs:         specs,
		surfaced:      map[string]surfacedAsk{},
		routedMarkers: loadRoutedMarkers(filepath.Join(cfg.Dir, "compose-routed.json")),
		live:          map[string]*liveJob{},
		announced:     map[string]bool{},
		observed:      map[string]string{},
		heldCh:        make(chan heldNotice, 16),
	}
	c.baseCtx, c.baseCancel = context.WithCancel(context.Background())
	if err := store.CreateRun(context.Background(), cfg.RunID, specs); err != nil {
		// A directory holds exactly one run; only that refusal means
		// "adopt the existing run" — anything else is a real failure.
		if !strings.Contains(err.Error(), "already holds run") {
			store.Close()
			return nil, err
		}
		if loaded, loadErr := loadComposeSpecs(filepath.Join(cfg.Dir, "compose-specs.json")); loadErr == nil {
			c.specs = loaded
		} else if !os.IsNotExist(loadErr) {
			store.Close()
			return nil, loadErr
		}
	} else if err := saveComposeSpecs(filepath.Join(cfg.Dir, "compose-specs.json"), c.specs); err != nil {
		store.Close()
		return nil, err
	}
	c.runner = runner.NewRunner(store, c.execute)
	c.runner.Parallelism = max(cfg.Parallelism, 1)
	c.runner.Takeover = c.consult
	c.runner.Restart = &runner.Confirmation{
		Options: []string{"restart-session", "abort"},
		Allow:   "restart-session",
		Body:    []byte("The session a pending call relied on is unavailable. Restart it under a fresh execution identity, or abort the call."),
	}
	go c.watchHeld()
	return c, nil
}

// Close stops the watcher, cancels any in-flight run this process owns,
// waits for its goroutine and releases the run directory. A run still
// advancing refuses with runner.ErrAdvancing rather than tearing state out
// from under it.
func (c *Compose) Close() error {
	c.baseCancel()
	c.waitRunDone(10 * time.Second)
	return c.store.Close()
}

// initialSpecs builds the creation-time spec set: every stage's input is its
// deterministic brief — the stage, its role and the requirement text.
func initialSpecs(requirement string) ([]runner.TaskSpec, error) {
	stages := composeGraph()
	specs := make([]runner.TaskSpec, 0, len(stages))
	for _, st := range stages {
		brief, err := stageBrief(st, requirement)
		if err != nil {
			return nil, err
		}
		specs = append(specs, runner.TaskSpec{
			TaskID:    st.TaskID,
			Name:      st.Name,
			DependsOn: st.Depends,
			Input:     brief,
			Confirm:   st.Gate,
		})
	}
	return specs, nil
}

// stageBrief is the input bytes of one stage at generation 1: canonical JSON
// of the stage identity plus the requirement, so the digest the kernel binds
// is a function of declared data only.
func stageBrief(st composeStage, requirement string) ([]byte, error) {
	brief := map[string]any{"stage": st.TaskID, "requirement": requirement}
	if st.Role != "" {
		brief["role"] = st.Role
	}
	return CanonicalJSON(brief)
}

// repairBrief is the input of one repair round: the stage brief plus the
// round number and the findings that drove it. It is a pure function of the
// recorded outcome, which is what lets a reopened owner re-derive the exact
// input a past revision bound. Findings are rendered as plain decoded
// values: the canonical encoder content-addresses this brief and speaks no
// typed structs.
func repairBrief(st composeStage, requirement string, round int, findings []Finding) ([]byte, error) {
	plain := make([]any, 0, len(findings))
	for _, finding := range findings {
		plain = append(plain, map[string]any{
			"id": finding.ID, "severity": finding.Severity,
			"message": finding.Message, "paths": pathsToAny(finding.Paths),
		})
	}
	brief := map[string]any{
		"stage":       st.TaskID,
		"requirement": requirement,
		"repair":      map[string]any{"round": round, "findings": plain},
	}
	if st.Role != "" {
		brief["role"] = st.Role
	}
	return CanonicalJSON(brief)
}

// pathsToAny widens a typed string slice to plain values for the canonical
// encoder.
func pathsToAny(paths []string) []any {
	out := make([]any, 0, len(paths))
	for _, path := range paths {
		out = append(out, path)
	}
	return out
}

// loadComposeSpecs reads the saved spec cache.
func loadComposeSpecs(path string) ([]runner.TaskSpec, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var saved []runner.TaskSpec
	if err := json.Unmarshal(body, &saved); err != nil {
		return nil, fmt.Errorf("compose: %s is unreadable: %w", path, err)
	}
	return saved, nil
}

// saveComposeSpecs lands the spec cache atomically with plain JSON encoding:
// the cache is a typed record read only by this composition, never
// content-addressed, so the canonical encoder has no stake in it.
func saveComposeSpecs(path string, specs []runner.TaskSpec) error {
	body, err := json.Marshal(specs)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// specsSnapshot copies the current spec set for Run and revision calls.
func (c *Compose) specsSnapshot() []runner.TaskSpec {
	c.specsMu.Lock()
	defer c.specsMu.Unlock()
	return append([]runner.TaskSpec(nil), c.specs...)
}

// replaceSpecInput swaps one task's input and persists the cache BEFORE the
// revision that binds it: a crash can then leave the file ahead of the
// database (recoverable by re-derivation in Drive), never behind it.
func (c *Compose) replaceSpecInput(taskID string, input []byte) error {
	c.specsMu.Lock()
	specs := append([]runner.TaskSpec(nil), c.specs...)
	for i := range specs {
		if specs[i].TaskID == taskID {
			specs[i].Input = input
		}
	}
	if err := saveComposeSpecs(filepath.Join(c.cfg.Dir, "compose-specs.json"), specs); err != nil {
		c.specsMu.Unlock()
		return err
	}
	c.specs = specs
	c.specsMu.Unlock()
	return nil
}

// deriveSpecsFromOutcomes rebuilds the spec set from the outcome trail: a
// task at generation 1 keeps its brief; a task whose generation g>1 has an
// outcome record ran repair rounds 2..g, each bound to the repair brief
// derived from the previous round's findings. This is the same derivation
// route() uses, so both spellings produce identical bytes.
func (c *Compose) deriveSpecsFromOutcomes() {
	specs, err := initialSpecs(c.cfg.Requirement)
	if err != nil {
		return
	}
	byID := map[string]composeStage{}
	for _, st := range composeGraph() {
		byID[st.TaskID] = st
	}
	for i := range specs {
		st := byID[specs[i].TaskID]
		if st.Role == "" {
			continue // local stages are never the target of a repair
		}
		gen := c.maxOutcomeGeneration(st.TaskID)
		for g := int64(2); g <= gen; g++ {
			prev := c.readOutcome(st.TaskID, g-1)
			if prev == nil {
				break
			}
			brief, err := repairBrief(st, c.cfg.Requirement, int(g), findingsOf(prev))
			if err != nil {
				break
			}
			specs[i].Input = brief
		}
	}
	c.specsMu.Lock()
	c.specs = specs
	c.specsRebuilt = true
	c.specsMu.Unlock()
}

// surfacedAsk is one presented question's allowing choice and answer.
type surfacedAsk struct {
	TaskID string
	Allow  string
	Choice string
}

// loadRoutedMarkers reads the durable repair-routing markers; a missing file
// is an empty map.
func loadRoutedMarkers(path string) map[string]int64 {
	markers := map[string]int64{}
	body, err := os.ReadFile(path)
	if err != nil {
		return markers
	}
	json.Unmarshal(body, &markers)
	return markers
}

// saveRoutedMarkers lands the routing markers atomically.
func saveRoutedMarkers(path string, markers map[string]int64) error {
	body, err := json.Marshal(markers)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// rememberSurfaced records the allowing choice of every question the
// composition presents. The allow choice is recognized from the question's
// own options — the composition is the declaring run for every question it
// drives (gates, the repair ask, the restart ask), so the vocabulary is
// local, not a guess about foreign questions.
func (c *Compose) rememberSurfaced(pending []runner.DecisionView) {
	c.specsMu.Lock()
	defer c.specsMu.Unlock()
	for _, question := range pending {
		allow := ""
		for _, option := range question.Options {
			switch option {
			case "approve", "continue-repair", "restart-session":
				allow = option
			}
		}
		if allow == "" {
			continue // not one of the composition's own questions
		}
		if _, ok := c.surfaced[question.DecisionID]; ok {
			continue // keep the recorded answer
		}
		c.surfaced[question.DecisionID] = surfacedAsk{TaskID: question.TaskID, Allow: allow}
	}
}

// refusedAnswer reports whether any presented question was answered with a
// choice other than its allowing one — the user's abort, which no premature
// gate wait may mask.
func (c *Compose) refusedAnswer() bool {
	c.specsMu.Lock()
	defer c.specsMu.Unlock()
	for _, ask := range c.surfaced {
		if ask.Choice != "" && ask.Choice != ask.Allow {
			return true
		}
	}
	return false
}

// Answer settles one process confirmation with the user's choice — a thin,
// honest passthrough: the composition owns which questions exist, the kernel
// owns the settle rules. The choice is remembered for the questions this
// process presented, whose refusal is a domain-level abort.
func (c *Compose) Answer(ctx context.Context, id, choice string) (runner.AnswerReceipt, error) {
	receipt, err := c.store.Answer(ctx, id, choice)
	if err != nil {
		return receipt, err
	}
	c.specsMu.Lock()
	if ask, ok := c.surfaced[id]; ok {
		ask.Choice = choice
		c.surfaced[id] = ask
	}
	c.specsMu.Unlock()
	return receipt, nil
}

// PendingDecisions lists the run's unanswered questions in the kernel's
// stable order.
func (c *Compose) PendingDecisions(ctx context.Context) ([]runner.DecisionView, error) {
	return c.store.PendingDecisions(ctx)
}
