package harness

// K5 — the harness-v1 composition over the Go kernel. This file owns the
// shape: the phase list as kernel tasks, the caller's configuration, and the
// run-directory facts the flow derives everything else from.
//
// The business flow, as kernel tasks plus explicit transitions instead of
// a generic engine:
//
//	requirement → product-review → technical-review → plan
//	  → development ∥ (qa-design → qa-review) → candidate
//	  → checks (one per authorized check) ∥ gate reviews (one per gate)
//	  → evaluate → (repair round back to candidate) | seal → terminal
//
// Division of state, one owner per fact:
//
//   - the kernel (internal/runner) owns tasks, dependency edges, call
//     generations, questions and their answers, repair-allowance requests,
//     restart requests and takeover facts;
//   - the run directory owns the materials: requirement.md, the worker
//     outcome records (outcomes/), content-addressed artifacts, supervised
//     job directories, and three small append-only ledgers described below;
//   - this composition owns only the transitions between kernel runs.
//
// The ledgers (all append-only, all written AFTER the kernel fact they
// describe, so the kernel stays the authority and a crash between the two
// costs at most an idempotent replay):
//
//	revisions.jsonl  one {task, brief} per accepted task revision — the
//	                 byte carrier the kernel does not persist (§7); the last
//	                 entry per task is the brief its current generation is
//	                 bound to, re-verified by every Run's digest check;
//	asks.jsonl       one {task, generation, disposition} per question the
//	                 flow placed on a question task, so a transition never
//	                 asks twice about the same disposition;
//	answers.jsonl    the user's choices, written ahead of the kernel's
//     Answer commit so a crash between them replays the intent exactly
//     once, and so a refused choice (which the kernel only reports while
//     its rows stay unreadable) still derives the run's ABORTED terminal.

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"formal-gates/internal/runner"
)

// Task ids of the fixed phase list. Ask tasks (":ask" suffix) hold the
// mid-flow questions: a question task has no dependencies and a trivial
// executor, so its gated state means exactly "an open question blocks
// whatever depends on me", and every revision of it carries a fresh
// question — the one kernel rule (a task's latest gate is re-frozen pending
// by a later revision) therefore never resurrects an answered question here.
const (
	taskRequirement = "requirement"
	taskProduct     = "product-review"
	taskTechnical   = "technical-review"
	taskPlan        = "plan"
	taskDevelopment = "development"
	taskQADesign    = "qa-design"
	taskQAReview    = "qa-review"
	taskCandidate   = "candidate"
	taskEvaluate    = "evaluate"
	taskSeal        = "seal"
	askSuffix       = ":ask"
)

// checkTaskID / gateTaskID / their ask tasks.
func checkTaskID(id string) string { return "check:" + id }
func gateTaskID(g string) string   { return "gate:" + g }

// Role descriptions: the business instruction each worker stage carries.
var roleInstructions = map[string]string{
	"product-review":              "Check the requirement for coherent scope and concrete acceptance. Do not implement.",
	"technical-review":            "Review feasibility and the smallest implementation plan. State the plan in summary. Do not implement.",
	"development":                 "Implement only the approved requirement in this workspace. Do not git commit. Use CONTINUE only for genuinely unfinished work.",
	"repair":                      "Repair only the supplied mandatory findings and failed checks. Preserve unrelated working code. Do not git commit.",
	"qa-design":                   "Independently assess the approved checks against requirements using ONLY the baseline workspace. Explain the coverage and oracle of each check in summary, and return every check id. Missing implementation on the baseline is expected, not a blocker. A real coverage gap is FAIL.",
	"qa-review":                   "Independently review the supplied QA design for coverage and feasibility. Account for every authorized check id. Missing implementation on the baseline is expected. Do not implement or change approved commands.",
	"complexity-gate":             "Review the full base-to-candidate change for unnecessary complexity and changes unrelated to the approved requirement. Do not implement.",
	"implementation-quality-gate": "Review the full base-to-candidate change for correctness and maintainability. Inspect actual code and assertions. Do not implement.",
}

// workerStages lists the worker (native-call) tasks and their roles. The
// repair role is a later generation of development, not a separate task: a
// repair round re-runs development with the failed-verification brief and
// the same remembered session.
var workerStages = []struct{ TaskID, Role string }{
	{taskProduct, "product-review"},
	{taskTechnical, "technical-review"},
	{taskDevelopment, "development"},
	{taskQADesign, "qa-design"},
	{taskQAReview, "qa-review"},
}

// ComposeSpec is the harness-v1 business spec: what is being built, from
// which frozen base, under which
// explicitly authorized checks and gates.
type ComposeSpec struct {
	Project     string   // local Git repository root (not modified by the run)
	Base        string   // the frozen commit every workspace starts from
	Requirement string   // the requirement text (also written to requirement.md)
	Checks      []Check  // authorized checks, executed verbatim
	Gates       []string // gate reviews from the accepted gate set
	RepairLimit int      // repair rounds before the user must authorize more
}

// ComposeConfig is everything the composition needs from its caller. Binding
// is the only host-side input: production returns the captured host binding
// for one native call; tests return scripted peers.
type ComposeConfig struct {
	// Dir is the run directory: the kernel's store plus the composition's
	// materials. One directory holds one run for its whole life.
	Dir string
	// RunID names the run; a reopened composition must re-send the same id.
	RunID string
	// Spec is the business spec, bound at creation (see binding check).
	Spec ComposeSpec
	// Binary is the formal-gates binary providing `harness supervise` and
	// `harness native`. Empty means FORMAL_GATES_HARNESS_BINARY, then
	// os.Executable.
	Binary string
	// Binding supplies the captured host binding for one native call.
	Binding func(NativeCall) (map[string]any, error)
	// Parallelism bounds concurrent ready tasks; zero runs one at a time.
	Parallelism int
}

// NativeCall describes one supervised native call the composition is about
// to make, so the deployment can hand back the captured binding for exactly
// that call. Attempt is the 1-based turn number of this invocation:
// continuations (worker status CONTINUE) re-prompt the same session under
// the next attempt without touching the task, its input or its call records.
type NativeCall struct {
	TaskID     string
	Role       string
	Generation int64
	Attempt    int
	JobDir     string
}

// derivedSpec is the full harness Spec the judgements consume, plus its
// content revision, both bound at creation.
type derivedSpec struct {
	spec     Spec
	revision string
}

// binding is the creation-time fact a reopen must match (the R7 rule): the
// run is one immutable request, and a different project, base, check set,
// gate set, requirement or driver binary is a different run.
type runBinding struct {
	RunID             string   `json:"run_id"`
	RequirementSHA256 string   `json:"requirement_sha256"`
	Project           string   `json:"project"`
	Base              string   `json:"base"`
	Checks            []Check  `json:"checks"`
	Gates             []string `json:"gates"`
	RepairLimit       int      `json:"repair_limit"`
	SpecRevision      string   `json:"spec_revision"`
	Binary            string   `json:"binary"`
}

// record is the small mutable business state the flow cannot re-derive:
// the role session identities (a restart clears one; nothing else moves
// them) and the last terminal the flow itself reached. Everything else —
// rounds, candidates, routes, evidence references — is derived from the
// outcome trail on demand.
type record struct {
	Sessions map[string]string `json:"sessions"`
	Terminal string            `json:"terminal,omitempty"`
	Stopped  string            `json:"stop_reason,omitempty"`
}

// revisionEntry is one accepted task revision: the brief bytes the kernel
// bound (it persists only their digest, §7), so a reopened owner can re-send
// exactly what the run declared.
type revisionEntry struct {
	Task  string          `json:"task"`
	Brief json.RawMessage `json:"brief"`
	Why   string          `json:"why"`
}

// askEntry marks one placed question: the question task, the generation its
// gate landed at, and the disposition id it covers — the idempotence of
// "ask once per disposition".
type askEntry struct {
	Task        string `json:"task"`
	Generation  int64  `json:"generation"`
	Disposition string `json:"disposition"`
}

// answerEntry is one user choice, written ahead of the kernel's Answer
// commit (a write-ahead of intent: a crash between the two replays it) and
// read back to derive refusals the kernel reports only through run errors.
type answerEntry struct {
	DecisionID string `json:"decision_id"`
	Task       string `json:"task"`
	Choice     string `json:"choice"`
	Allow      string `json:"allow,omitempty"`
	Alternate  string `json:"alternate,omitempty"`
	// Options records the question's own choices, so the refusing test can
	// tell a restart request from a stage gate by shape, not by task name.
	Options []string `json:"options,omitempty"`
}

// Compose drives one harness-v1 run over the kernel. Drive advances it
// until a user input is needed or a terminal judgment is reached; Answer
// and the native-respond path deliver inputs; Cancel settles in-flight
// calls; Archive physicalizes a terminal run.
type Compose struct {
	cfg     ComposeConfig
	binary  string
	dir     string
	spec    derivedSpec
	binding runBinding
	store   *runner.Store
	runner  *runner.Runner

	baseCtx    context.Context
	baseCancel context.CancelFunc

	// mu guards the in-memory caches: the live supervised calls, the
	// held-interaction bookkeeping, the takeover observations and the
	// progress signature.
	mu            sync.Mutex
	live          map[string]*liveJob
	announced     map[string]bool
	heldCh        chan heldNotice
	observed      map[string]bool
	lastSignature string

	// runMu guards the single in-flight Run goroutine.
	runMu   sync.Mutex
	runDone chan error
}

// ComposeStatus is Drive's stop classification.
type ComposeStatus string

const (
	// StatusWaitingDecision: the run stopped on an unanswered question
	// (a gate, a retry ask, the repair-allowance ask or a restart ask).
	StatusWaitingDecision ComposeStatus = "waiting-decision"
	// StatusWaitingInteraction: a host held a client-directed request for
	// the user; answer through native-respond while the original call waits.
	StatusWaitingInteraction ComposeStatus = "waiting-interaction"
	// StatusObserved: a previous owner's call is in flight under
	// observation; Drive again once it settles.
	StatusObserved ComposeStatus = "observed"
	// StatusComplete: the run sealed; the result record exists on disk.
	StatusComplete ComposeStatus = "complete"
	// StatusAborted: the user refused a question; Terminal ABORTED.
	StatusAborted ComposeStatus = "aborted"
	// StatusBlocked: a worker reported BLOCKED and its retry ask is open.
	StatusBlocked ComposeStatus = "blocked"
	// StatusFailed: the drive stopped making progress; Err carries why.
	StatusFailed ComposeStatus = "failed"
)

// ComposeState is one Drive stop.
type ComposeState struct {
	Status   ComposeStatus
	Terminal string // HARNESS_COMPLETE, DEMO_COMPLETE or ABORTED at a terminal
	Pending  []runner.DecisionView
	Held     []HeldInteraction
	Err      error
}

// HeldInteraction is one host-held request routed to the user: the original
// call stays alive while the decision is delivered through native-respond
// against the job directory.
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

// Terminal status names (the certify judgement's vocabulary).
const (
	statusHarnessComplete = "HARNESS_COMPLETE"
	statusDemoComplete    = "DEMO_COMPLETE"
	statusAborted         = "ABORTED"
)

// ComposeHarnessV1 opens (or creates) the composition run in cfg.Dir.
// Creation binds the spec, the requirement bytes and the driver binary and
// writes them into the record; a reopen with any of them changed is a named
// refusal (the binding check), never a silent adoption of new inputs.
func ComposeHarnessV1(cfg ComposeConfig) (c *Compose, err error) {
	if cfg.Dir == "" || cfg.RunID == "" || cfg.Binding == nil {
		return nil, errors.New("compose: Dir, RunID and Binding are required")
	}
	if err := validateComposeSpec(cfg.Spec); err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(cfg.Dir)
	if err != nil {
		return nil, err
	}
	binary := cfg.Binary
	if binary == "" {
		binary = os.Getenv("FORMAL_GATES_HARNESS_BINARY")
	}
	if binary == "" {
		if exe, err := os.Executable(); err == nil {
			binary = exe
		}
	}
	if binary == "" {
		return nil, errors.New("compose: no driver binary; set ComposeConfig.Binary")
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return nil, err
	}
	spec, err := deriveFullSpec(cfg.Spec, binary)
	if err != nil {
		return nil, err
	}
	store, err := runner.Open(dir)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			store.Close()
		}
	}()
	c = &Compose{
		cfg:       cfg,
		store:     store,
		binary:    binary,
		dir:       dir,
		spec:      spec,
		binding:   bindingOf(cfg, spec, binary),
		live:      map[string]*liveJob{},
		announced: map[string]bool{},
		observed:  map[string]bool{},
		heldCh:    make(chan heldNotice, 16),
	}
	if err := c.adoptOrCreate(); err != nil {
		return nil, err
	}
	c.baseCtx, c.baseCancel = context.WithCancel(context.Background())
	c.runner = runner.NewRunner(store, c.execute)
	c.runner.Parallelism = max(cfg.Parallelism, 1)
	c.runner.Takeover = c.consult
	c.runner.Restart = &runner.Confirmation{
		Options: []string{"restart-session", "abort"},
		Allow:   "restart-session",
		Body:    []byte("The session a pending call relied on is unavailable. Restart it under a fresh execution identity, or abort the run."),
	}
	go c.watchHeld()
	return c, nil
}

// validateComposeSpec applies the accepted spec contract (contract.go) to
// the caller's spec, with the driver-side facts filled in.
func validateComposeSpec(s ComposeSpec) error {
	full := Spec{
		Version:           Version,
		Project:           s.Project,
		Base:              s.Base,
		RequirementDigest: fmt.Sprintf("%x", sha256.Sum256([]byte(s.Requirement))),
		Checks:            s.Checks,
		Gates:             s.Gates,
		Provider:          "native",
		RepairLimit:       s.RepairLimit,
	}
	return ValidateSpec(full)
}

// deriveFullSpec builds the judgement-facing Spec and its content revision.
func deriveFullSpec(s ComposeSpec, binary string) (derivedSpec, error) {
	sum := sha256.Sum256([]byte(s.Requirement))
	project, err := filepath.Abs(s.Project)
	if err != nil {
		return derivedSpec{}, err
	}
	full := Spec{
		Version:           Version,
		Project:           project,
		Base:              s.Base,
		RequirementDigest: hex.EncodeToString(sum[:]),
		Checks:            s.Checks,
		Gates:             s.Gates,
		Provider:          "native",
		RepairLimit:       s.RepairLimit,
	}
	return derivedSpec{spec: full, revision: Digest(full)}, nil
}

// bindingOf freezes the creation binding.
func bindingOf(cfg ComposeConfig, spec derivedSpec, binary string) runBinding {
	return runBinding{
		RunID:             cfg.RunID,
		RequirementSHA256: spec.spec.RequirementDigest,
		Project:           spec.spec.Project,
		Base:              spec.spec.Base,
		Checks:            spec.spec.Checks,
		Gates:             spec.spec.Gates,
		RepairLimit:       spec.spec.RepairLimit,
		SpecRevision:      spec.revision,
		Binary:            binary,
	}
}

// adoptOrCreate: a fresh directory gets the run, its tasks, edges and the
// requirement gate in one CreateRun; an existing one is adopted after the
// binding check — the persisted binding, not the current arguments, defines
// what this run is.
func (c *Compose) adoptOrCreate() error {
	if _, err := os.Stat(filepath.Join(c.dir, "binding.json")); err == nil {
		bound, err := c.readBinding()
		if err != nil {
			return err
		}
		return compareBindings(bound, c.binding)
	} else if !os.IsNotExist(err) {
		return err
	}
	specs, err := c.creationSpecs()
	if err != nil {
		return err
	}
	if err := c.store.CreateRun(context.Background(), c.cfg.RunID, specs); err != nil {
		// The only tolerable failure here is a crash between the kernel's
		// CreateRun and this file landing: the directory then holds exactly
		// this run but no binding yet. Anything else is a real refusal.
		held, ok := heldRunID(err)
		if !ok || held != c.cfg.RunID {
			return err
		}
	}
	if err := writeAtomicBytes(filepath.Join(c.dir, "requirement.md"), []byte(c.cfg.Spec.Requirement)); err != nil {
		return err
	}
	if err := writeAtomicJSON(filepath.Join(c.dir, "record.json"), record{Sessions: map[string]string{}}); err != nil {
		return err
	}
	return writeAtomicJSON(filepath.Join(c.dir, "binding.json"), c.binding)
}

// heldRunID recognizes the kernel's "directory already holds run X"
// refusal and returns X — the one place the composition reads a kernel
// error's text, because no read API exposes the directory's run id.
func heldRunID(err error) (string, bool) {
	const marker = "already holds run "
	text := err.Error()
	at := strings.Index(text, marker)
	if at < 0 {
		return "", false
	}
	rest := text[at+len(marker):]
	if len(rest) < 2 || rest[0] != '"' {
		return "", false
	}
	end := strings.IndexByte(rest[1:], '"')
	if end < 0 {
		return "", false
	}
	return rest[1 : 1+end], true
}

// compareBindings is the reopen rule: every creation fact must match, and a
// mismatch names the field.
func compareBindings(bound, now runBinding) error {
	kind := func() string {
		switch {
		case bound.RunID != now.RunID:
			return "run id"
		case bound.RequirementSHA256 != now.RequirementSHA256:
			return "requirement"
		case bound.Project != now.Project:
			return "project"
		case bound.Base != now.Base:
			return "base"
		case bound.RepairLimit != now.RepairLimit:
			return "repair limit"
		case bound.Binary != now.Binary:
			return "driver binary"
		case bound.SpecRevision != now.SpecRevision:
			return "spec"
		default:
			return ""
		}
	}()
	if kind == "" {
		return nil
	}
	return fmt.Errorf("compose: RUN_BINDING: %s changed since this run was created; a run is one immutable request — open a new run instead", kind)
}

// readBinding loads the persisted creation binding.
func (c *Compose) readBinding() (runBinding, error) {
	var bound runBinding
	body, err := os.ReadFile(filepath.Join(c.dir, "binding.json"))
	if err != nil {
		return bound, fmt.Errorf("compose: RUN_BINDING: the run's creation binding is unreadable: %w", err)
	}
	if err := json.Unmarshal(body, &bound); err != nil {
		return bound, fmt.Errorf("compose: RUN_BINDING: the run's creation binding is malformed: %w", err)
	}
	return bound, nil
}

// creationSpecs builds the whole task set with its dependency edges. Ask
// tasks have no dependencies (their question, not their execution, is the
// point); business tasks name the ask tasks of the phase before them so an
// open question holds the chain at its own stage boundary.
func (c *Compose) creationSpecs() ([]runner.TaskSpec, error) {
	var specs []runner.TaskSpec
	add := func(taskID, name string, deps []string, confirm *runner.Confirmation) error {
		brief, err := c.initialBrief(taskID)
		if err != nil {
			return err
		}
		specs = append(specs, runner.TaskSpec{
			TaskID: taskID, Name: name, DependsOn: deps, Input: brief, Confirm: confirm,
		})
		return nil
	}
	// The requirement confirmation is a creation-time fact: requirement,
	// checks, gates, project and base all exist when the run is created.
	requirementBody, err := canonicalBody(map[string]any{
		"requirement": c.dir + "/requirement.md",
		"checks":      c.spec.spec.Checks,
		"gates":       c.spec.spec.Gates,
		"project":     c.spec.spec.Project,
		"base":        c.spec.spec.Base,
	})
	if err != nil {
		return nil, err
	}
	if err := add(taskRequirement, "requirement confirmation", nil, &runner.Confirmation{
		Options: []string{"approve", "abort"}, Allow: "approve",
		Revision: c.spec.revision, Body: requirementBody,
	}); err != nil {
		return nil, err
	}
	if err := add(taskProduct, "product review", []string{taskRequirement, askTask(taskProduct)}, nil); err != nil {
		return nil, err
	}
	if err := add(taskTechnical, "technical review", []string{taskProduct, askTask(taskProduct), askTask(taskTechnical)}, nil); err != nil {
		return nil, err
	}
	if err := add(taskPlan, "plan approval", []string{taskTechnical, askTask(taskTechnical)}, nil); err != nil {
		return nil, err
	}
	if err := add(taskDevelopment, "development", []string{taskPlan}, nil); err != nil {
		return nil, err
	}
	if err := add(taskQADesign, "QA design", []string{taskPlan}, nil); err != nil {
		return nil, err
	}
	if err := add(taskQAReview, "QA review", []string{taskQADesign, askTask(taskQADesign)}, nil); err != nil {
		return nil, err
	}
	verify := []string{taskDevelopment, askTask(taskDevelopment), taskQAReview, askTask(taskQADesign)}
	if err := add(taskCandidate, "candidate admission and snapshot", verify, nil); err != nil {
		return nil, err
	}
	var verifyAll []string
	for _, check := range c.spec.spec.Checks {
		id := checkTaskID(check.ID)
		if err := add(id, "check "+check.ID, []string{taskCandidate}, nil); err != nil {
			return nil, err
		}
		verifyAll = append(verifyAll, id, askTask(id))
	}
	for _, gate := range c.spec.spec.Gates {
		id := gateTaskID(gate)
		if err := add(id, gate, []string{taskCandidate}, nil); err != nil {
			return nil, err
		}
		verifyAll = append(verifyAll, id, askTask(id))
	}
	sort.Strings(verifyAll)
	if err := add(taskEvaluate, "evaluate route", append([]string{taskCandidate}, verifyAll...), nil); err != nil {
		return nil, err
	}
	if err := add(taskSeal, "seal", []string{taskEvaluate}, nil); err != nil {
		return nil, err
	}
	// Question tasks last: they depend on nothing.
	for _, task := range c.questionTasks() {
		if err := add(task, "question "+task, nil, nil); err != nil {
			return nil, err
		}
	}
	return specs, nil
}

// questionTasks lists every ask task: the plan approval's gate plus one per
// worker and check stage whose flow can raise a mid-flight question.
func (c *Compose) questionTasks() []string {
	tasks := []string{askTask(taskPlan)}
	for _, stage := range workerStages {
		tasks = append(tasks, askTask(stage.TaskID))
	}
	for _, check := range c.spec.spec.Checks {
		tasks = append(tasks, askTask(checkTaskID(check.ID)))
	}
	for _, gate := range c.spec.spec.Gates {
		tasks = append(tasks, askTask(gateTaskID(gate)))
	}
	return tasks
}

// askTask names the question task of a stage.
func askTask(taskID string) string { return taskID + askSuffix }

// pathSafe renders a task id as one path segment ("check:x" carries a
// colon, which a Windows path cannot).
func pathSafe(taskID string) string { return strings.ReplaceAll(taskID, ":", "-") }

// stageTaskOf names the stage a question task belongs to.
func stageTaskOf(questionTask string) string { return strings.TrimSuffix(questionTask, askSuffix) }

// isQuestionTask reports whether taskID is one of the ask tasks.
func (c *Compose) isQuestionTask(taskID string) bool {
	if !strings.HasSuffix(taskID, askSuffix) {
		return false
	}
	for _, question := range c.questionTasks() {
		if question == taskID {
			return true
		}
	}
	return false
}

// initialBrief is a task's generation-1 input: the minimal identity the
// stage derives its full materials from at execution time. Worker briefs
// grow a feedback wrapper through revisions; check briefs carry their
// round and runtime-retry attempt; the rest never change.
func (c *Compose) initialBrief(taskID string) ([]byte, error) {
	switch {
	case taskID == taskRequirement:
		return CanonicalJSON(map[string]any{"stage": taskID, "revision": c.spec.revision})
	case taskID == taskPlan:
		return CanonicalJSON(map[string]any{"stage": taskID, "revision": c.spec.revision})
	case taskID == taskCandidate:
		return CanonicalJSON(map[string]any{"stage": taskID, "revision": c.spec.revision})
	case taskID == taskEvaluate || taskID == taskSeal:
		return CanonicalJSON(map[string]any{"stage": taskID, "revision": c.spec.revision})
	case c.isQuestionTask(taskID):
		return CanonicalJSON(map[string]any{"stage": taskID, "revision": c.spec.revision})
	case strings.HasPrefix(taskID, "check:"):
		return CanonicalJSON(map[string]any{"stage": taskID, "revision": c.spec.revision, "round": 0, "attempt": 0})
	default:
		role := c.roleOf(taskID)
		if role == "" {
			return nil, fmt.Errorf("compose: unknown task %q", taskID)
		}
		return CanonicalJSON(map[string]any{"stage": taskID, "role": role, "revision": c.spec.revision, "feedback": nil})
	}
}

// roleOf maps a task id to its worker role; repair is a development
// generation, and gate tasks carry their gate name as role.
func (c *Compose) roleOf(taskID string) string {
	for _, stage := range workerStages {
		if stage.TaskID == taskID {
			return stage.Role
		}
	}
	if strings.HasPrefix(taskID, "gate:") {
		return strings.TrimPrefix(taskID, "gate:")
	}
	return ""
}

// specSet produces the spec list for one Run: every task's creation brief,
// with the last accepted revision entry replacing it. Entries are written
// only after the kernel accepted the revision, so the list always names the
// bytes the run's digests are bound to.
func (c *Compose) specSet() ([]runner.TaskSpec, error) {
	specs, err := c.creationSpecs()
	if err != nil {
		return nil, err
	}
	last := map[string]json.RawMessage{}
	for _, entry := range c.readRevisions() {
		last[entry.Task] = entry.Brief
	}
	for i := range specs {
		if brief, ok := last[specs[i].TaskID]; ok {
			specs[i].Input = []byte(brief)
		}
	}
	return specs, nil
}

// canonicalBody canonicalizes a value that may contain typed records ([]Check
// and friends): the typed fields are marshaled with their JSON tags first,
// then re-decoded with number fidelity so the canonical encoder sees only the
// plain value shapes it owns. Digesting the marshaled bytes directly would use
// a different (non-canonical) encoding for the same content.
func canonicalBody(value any) ([]byte, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	plain, err := decodeValue(raw)
	if err != nil {
		return nil, err
	}
	return CanonicalJSON(plain)
}

// ---- ledger plumbing -------------------------------------------------------

// appendLedger appends one canonical JSON line to a ledger file.
func appendLedger(path string, value any) error {
	body, err := canonicalBody(value)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(body, '\n')); err != nil {
		return err
	}
	return f.Sync()
}

// readLedger decodes every line of a ledger; a missing file is empty.
func readLedger[T any](path string) ([]T, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []T
	for _, line := range strings.Split(string(body), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var entry T
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			return nil, fmt.Errorf("compose: %s is unreadable: %w", filepath.Base(path), err)
		}
		out = append(out, entry)
	}
	return out, nil
}

// readRevisions loads the revision ledger.
func (c *Compose) readRevisions() []revisionEntry {
	entries, err := readLedger[revisionEntry](filepath.Join(c.dir, "revisions.jsonl"))
	if err != nil {
		return nil
	}
	return entries
}

// lastBrief returns the brief a task's current generation is bound to (its
// creation brief, or its last revision entry's).
func (c *Compose) lastBrief(taskID string) []byte {
	entries := c.readRevisions()
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Task == taskID {
			return []byte(entries[i].Brief)
		}
	}
	brief, err := c.initialBrief(taskID)
	if err != nil {
		return nil
	}
	return brief
}

// revise records one accepted revision: kernel first (ReviseTask), ledger
// second. A crash between the two costs one idempotent replay — the
// transition re-derives the same brief and revises again, and the worker
// receipt replay makes the extra generation free.
func (c *Compose) revise(ctx context.Context, taskID string, brief []byte, why string) error {
	spec := runner.TaskSpec{TaskID: taskID, Input: brief}
	if err := c.store.ReviseTask(ctx, taskID, spec); err != nil {
		return err
	}
	return appendLedger(filepath.Join(c.dir, "revisions.jsonl"), revisionEntry{
		Task: taskID, Brief: json.RawMessage(brief), Why: why,
	})
}

// askRecorded reports the ask marker covering one disposition.
func (c *Compose) askRecorded(questionTask, disposition string) bool {
	for _, entry := range c.readAsks() {
		if entry.Task == questionTask && entry.Disposition == disposition {
			return true
		}
	}
	return false
}

// readAsks loads the placed-question ledger.
func (c *Compose) readAsks() []askEntry {
	entries, err := readLedger[askEntry](filepath.Join(c.dir, "asks.jsonl"))
	if err != nil {
		return nil
	}
	return entries
}

// readAnswers loads the user-choice ledger.
func (c *Compose) readAnswers() []answerEntry {
	entries, err := readLedger[answerEntry](filepath.Join(c.dir, "answers.jsonl"))
	if err != nil {
		return nil
	}
	return entries
}

// readRecord loads the mutable business record; a missing file is an empty
// one (the run never wrote a session or a terminal).
func (c *Compose) readRecord() record {
	var current record
	body, err := os.ReadFile(filepath.Join(c.dir, "record.json"))
	if err != nil {
		return record{Sessions: map[string]string{}}
	}
	if json.Unmarshal(body, &current) != nil {
		return record{Sessions: map[string]string{}}
	}
	if current.Sessions == nil {
		current.Sessions = map[string]string{}
	}
	return current
}

// writeRecord lands the record atomically.
func (c *Compose) writeRecord(current record) error {
	return writeAtomicJSON(filepath.Join(c.dir, "record.json"), current)
}

// session returns the remembered identity of one stage's session.
func (c *Compose) session(taskID string) string { return c.readRecord().Sessions[taskID] }

// clearSession drops one stage's session identity (a restart answer).
func (c *Compose) clearSession(taskID string) {
	current := c.readRecord()
	if current.Sessions[taskID] == "" {
		return
	}
	delete(current.Sessions, taskID)
	_ = c.writeRecord(current)
}

// bytesEqual is bytes.Equal for the two brief spellings.
func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// writeAtomicBytes lands bytes atomically (temp file, fsync, rename).
func writeAtomicBytes(path string, body []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(body); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(name, path)
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
