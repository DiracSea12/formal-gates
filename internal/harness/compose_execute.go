package harness

// The composition's Executor (plan §8.1): one runner invocation executes as a
// supervised native call — a fresh job directory per turn, a written intent
// (native-request.json), the accepted supervisor owning the process, its exit
// receipt verified, and only then a business result handed back for kernel
// admission. The supervised child is `harness native <jobdir>`, so the host
// side is carried by whichever adapter the binding names (native_dsh or
// native_zcode); this file never speaks a host protocol itself.
//
// Domain result semantics (the K4-review point) are wired exactly here: the
// framed answer is validated and judged by domain.workerOutcome, and each
// action maps to a kernel-visible consequence —
//
//   - accept      → the result is admitted; the task finishes.
//   - blocked     → the result is admitted as a BLOCKED fact; the terminal
//     judgment reports the run blocked (a wait, never a PASS).
//   - unresolved-findings (development/repair FAIL) → the result is admitted;
//     the candidate gate refuses on it and drives the repair loop.
//   - continue    → the same invocation continues with a further supervised
//     turn on the resumed session (§8.1: no task, input or call record
//     changes; no repair round is consumed — CONTINUE is a query).
//
// Anything that produced no admissible business result (a cancelled call, a
// failed native job, a session the host refused) returns an error: the
// invocation keeps its pending row and the next Run's takeover consult
// classifies it — Adopt / Observe / restart ask (compose_takeover.go).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"formal-gates/internal/runner"
)

// composeEvidence is the composition's admitted-result payload: the framed
// worker result plus where its evidence lives. The kernel stores it as the
// invocation's result JSON; nothing here duplicates scheduling state.
type composeEvidence struct {
	TaskID     string        `json:"task"`
	Generation int64         `json:"generation"`
	Attempt    int           `json:"attempt"`
	Role       string        `json:"role,omitempty"`
	Action     string        `json:"action"`
	Provider   string        `json:"provider,omitempty"`
	Worker     *WorkerResult `json:"worker,omitempty"`
	Job        string        `json:"job,omitempty"` // job dir, run-relative
}

// outcomeRecord is the durable business outcome of one executed generation:
// what the worker answered, the domain action, and — for the candidate stage —
// the admission verdict. route() and the terminal judgment read these; the
// kernel's own rows remain the only authority over what runs next.
type outcomeRecord struct {
	TaskID       string         `json:"task"`
	Generation   int64          `json:"generation"`
	InvocationID string         `json:"invocation"`
	Attempt      int            `json:"attempt"`
	Action       string         `json:"action"`
	Worker       *WorkerResult  `json:"worker,omitempty"`
	Provider     string         `json:"provider,omitempty"`
	Job          string         `json:"job,omitempty"`
	Admission    *admissionFact `json:"admission,omitempty"`
}

// admissionFact records the candidate gate's judgement and the upstream
// generations it was taken at, so a later routing (or a reopened owner) can
// tell an already-repaired refusal from a fresh one.
type admissionFact struct {
	OK                bool   `json:"ok"`
	Kind              string `json:"kind,omitempty"`
	Refusal           string `json:"refusal,omitempty"`
	DevelopmentGen    int64  `json:"development_generation"`
	QAReviewGen       int64  `json:"qa_review_generation"`
	DevelopmentRepair bool   `json:"development_needs_repair"`
	QAReviewRepair    bool   `json:"qa_review_needs_repair"`
}

// outcomePath is the outcome record's location for one (task, generation).
func outcomePath(dir, taskID string, generation int64) string {
	return filepath.Join(dir, "outcomes", fmt.Sprintf("%s-%d.json", taskID, generation))
}

// writeOutcome lands one outcome record atomically. Plain JSON encoding:
// the record is a typed struct read only by the composition's routing, never
// content-addressed.
func writeOutcome(dir string, record outcomeRecord) error {
	if err := os.MkdirAll(filepath.Join(dir, "outcomes"), 0o755); err != nil {
		return err
	}
	body, err := json.Marshal(record)
	if err != nil {
		return err
	}
	tmp := outcomePath(dir, record.TaskID, record.Generation) + ".tmp"
	if err := os.WriteFile(tmp, body, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, outcomePath(dir, record.TaskID, record.Generation))
}

// readOutcome loads one generation's outcome, or nil when that generation
// never executed.
func (c *Compose) readOutcome(taskID string, generation int64) *outcomeRecord {
	body, err := os.ReadFile(outcomePath(c.cfg.Dir, taskID, generation))
	if err != nil {
		return nil
	}
	var record outcomeRecord
	if json.Unmarshal(body, &record) != nil {
		return nil
	}
	return &record
}

// maxOutcomeGeneration returns the highest generation with an outcome record
// (0 when the task never executed) — the executed-round count route() and the
// spec re-derivation both key off.
func (c *Compose) maxOutcomeGeneration(taskID string) int64 {
	entries, err := os.ReadDir(filepath.Join(c.cfg.Dir, "outcomes"))
	if err != nil {
		return 0
	}
	prefix := taskID + "-"
	var best int64
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".json") {
			continue
		}
		if gen, err := strconv.ParseInt(strings.TrimSuffix(strings.TrimPrefix(name, prefix), ".json"), 10, 64); err == nil && gen > best {
			best = gen
		}
	}
	return best
}

// latestOutcome is the newest generation's outcome, or nil.
func (c *Compose) latestOutcome(taskID string) *outcomeRecord {
	gen := c.maxOutcomeGeneration(taskID)
	if gen == 0 {
		return nil
	}
	return c.readOutcome(taskID, gen)
}

// findingsOf projects a worker outcome's findings for a repair brief.
func findingsOf(record *outcomeRecord) []Finding {
	if record == nil || record.Worker == nil {
		return nil
	}
	return record.Worker.Findings
}

// execute is the runner Executor: one invocation of one stage.
func (c *Compose) execute(ctx context.Context, inv runner.Invocation) (runner.Result, error) {
	var stage composeStage
	for _, st := range composeGraph() {
		if st.TaskID == inv.TaskID {
			stage = st
			break
		}
	}
	if stage.TaskID == "" {
		return runner.Result{}, fmt.Errorf("compose: invocation names unknown task %q", inv.TaskID)
	}
	if stage.Role == "" {
		return c.executeLocal(ctx, stage, inv)
	}
	return c.executeNative(ctx, stage, inv)
}

// executeLocal runs a stage whose executor is a plain function. The gate
// stages record the gate's passage; the candidate stage applies
// domain.candidateAdmission over the upstream outcomes and — when admission
// is refused — returns no receipt: the pending invocation is superseded by
// the repair revision that follows, and the approval gate behind it never
// opens on a candidate nobody may approve.
func (c *Compose) executeLocal(ctx context.Context, stage composeStage, inv runner.Invocation) (runner.Result, error) {
	switch stage.TaskID {
	case stageConfirm, stageApprove:
		return evidenceResult(composeEvidence{
			TaskID: stage.TaskID, Generation: inv.Generation, Action: "gate-open"})
	case stageCandidate:
		dev := c.latestOutcome(stageDevelopment)
		qa := c.latestOutcome(stageQAReview)
		if dev == nil || qa == nil {
			// The join guarantees both dependencies finished; a missing
			// outcome record means the evidence trail is broken — no
			// admission on guesswork.
			return runner.Result{}, fmt.Errorf("compose: candidate admission for task %q is missing an upstream outcome (development=%v, qa-review=%v)", inv.TaskID, dev != nil, qa != nil)
		}
		// The domain function judges needs_repair of the two results — the
		// workerOutcome action would under-count: a review FAIL is an
		// accepted review result whose findings still block the candidate.
		devRepair := dev.Worker != nil && NeedsRepair(*dev.Worker)
		qaRepair := qa.Worker != nil && NeedsRepair(*qa.Worker)
		verdict := candidateAdmission(candidateAdmissionInput{
			DevelopmentNeedsRepair: devRepair,
			QAReviewNeedsRepair:    qaRepair,
		})
		fact := &admissionFact{
			OK:                verdict["ok"] == true,
			Kind:              textOf(verdict["kind"]),
			Refusal:           textOf(verdict["refusal"]),
			DevelopmentGen:    dev.Generation,
			QAReviewGen:       qa.Generation,
			DevelopmentRepair: devRepair,
			QAReviewRepair:    qaRepair,
		}
		if err := writeOutcome(c.cfg.Dir, outcomeRecord{
			TaskID:       stage.TaskID,
			Generation:   inv.Generation,
			InvocationID: inv.InvocationID,
			Action:       "candidate",
			Admission:    fact,
		}); err != nil {
			return runner.Result{}, err
		}
		if !fact.OK {
			return runner.Result{}, fmt.Errorf("compose: candidate admission refused: %s", fact.Refusal)
		}
		return evidenceResult(composeEvidence{
			TaskID: stage.TaskID, Generation: inv.Generation, Action: "admitted"})
	default:
		return runner.Result{}, fmt.Errorf("compose: local stage %q has no executor", stage.TaskID)
	}
}

// executeNative runs one worker stage as supervised native call(s). The
// continuation loop mirrors the reference invoke_worker: a CONTINUE answer
// starts the next turn on the resumed session under the same invocation.
func (c *Compose) executeNative(ctx context.Context, stage composeStage, inv runner.Invocation) (runner.Result, error) {
	identity := ""
	attempt := 0
	for {
		attempt++
		jobDir := filepath.Join(c.cfg.Dir, "jobs", inv.TaskID, inv.InvocationID, fmt.Sprintf("turn-%02d", attempt))
		if err := os.MkdirAll(jobDir, 0o755); err != nil {
			return runner.Result{}, err
		}
		binding, err := c.cfg.Binding(NativeCall{
			TaskID:     inv.TaskID,
			Generation: inv.Generation,
			Attempt:    attempt,
			JobDir:     jobDir,
		})
		if err != nil {
			return runner.Result{}, fmt.Errorf("compose: binding for %s: %w", inv.TaskID, err)
		}
		if err := c.writeNativeRequest(jobDir, stage, inv, binding, attempt, identity); err != nil {
			return runner.Result{}, err
		}
		receipt, err := c.runSupervised(ctx, stage.TaskID, jobDir)
		if err != nil {
			return runner.Result{}, err
		}
		answer, err := settleNativeReceipt(jobDir, receipt)
		if err != nil {
			return runner.Result{}, err
		}
		provider := textOf(getOf(binding, "provider"))
		if provider == "" {
			if source, err := readJSONFile(filepath.Join(jobDir, "source.json")); err == nil {
				provider = textOf(getOf(source, "provider"))
			}
		}
		worker, err := validateWorkerAnswer(stage.Role, answer)
		if err != nil {
			return runner.Result{}, err
		}
		needsRepair := NeedsRepair(*worker)
		action := textOf(workerOutcome(workerOutcomeInput{
			Role: stage.Role, Status: worker.Status, NeedsRepair: needsRepair})["action"])
		record := outcomeRecord{
			TaskID:       inv.TaskID,
			Generation:   inv.Generation,
			InvocationID: inv.InvocationID,
			Attempt:      attempt,
			Action:       action,
			Worker:       worker,
			Provider:     provider,
			Job:          relJobPath(inv.TaskID, inv.InvocationID, attempt),
		}
		if err := writeOutcome(c.cfg.Dir, record); err != nil {
			return runner.Result{}, err
		}
		switch action {
		case "continue":
			// §8.1: the same logical call continues — resume the session and
			// run a further supervised turn. No generation bump, no repair
			// round, no new invocation.
			identity = nativeJobIdentity(jobDir)
			continue
		default:
			// accept, blocked and unresolved-findings all admit: the worker
			// returned a valid business result; the routing and the terminal
			// judgment decide what it means.
			return evidenceResult(composeEvidence{
				TaskID:     inv.TaskID,
				Generation: inv.Generation,
				Attempt:    attempt,
				Role:       stage.Role,
				Action:     action,
				Provider:   provider,
				Worker:     worker,
				Job:        record.Job,
			})
		}
	}
}

// evidenceResult packs the admitted evidence as the kernel result payload.
// Plain json.Marshal, not the canonical encoder: this body is a typed record
// read back only through the outcome files, never content-addressed.
func evidenceResult(evidence composeEvidence) (runner.Result, error) {
	payload, err := json.Marshal(evidence)
	if err != nil {
		return runner.Result{}, err
	}
	return runner.Result{Payload: payload}, nil
}

// writeNativeRequest lands the adapter request: the provider and binding come
// from the caller's capture, the prompt from the invocation's bound input
// plus (for continuations) the prior turn's summary, the schema from the
// accepted worker contract. Identity empty means a new session; a
// continuation passes the session the prior turn established.
func (c *Compose) writeNativeRequest(jobDir string, stage composeStage, inv runner.Invocation, binding map[string]any, attempt int, identity string) error {
	prompt := string(inv.Input)
	if attempt > 1 {
		prior := c.readOutcome(stage.TaskID, inv.Generation)
		priorSummary := ""
		if prior != nil && prior.Worker != nil {
			priorSummary = prior.Worker.Summary
		}
		prompt += fmt.Sprintf("\n\nContinuation %d of the same work (prior status CONTINUE, prior summary: %s). Continue and finish it.",
			attempt, priorSummary)
	}
	request := map[string]any{
		"provider":         textOf(getOf(binding, "provider")),
		"binding":          binding,
		"workspace":        c.cfg.Workspace,
		"read_only":        stage.ReadOnly,
		"identity":         identity,
		"model":            "",
		"reasoning_effort": "",
		"prompt":           prompt,
		"schema":           plainWorkerSchema(),
	}
	return writeAtomicJSON(filepath.Join(jobDir, "native-request.json"), request)
}

// plainWorkerSchema is the accepted worker result schema as plain decoded
// values (numbers as json.Number): WorkerSchema's typed []string enums speak
// Go, while every canonical encoding here — the request digest the adapter
// embeds in its prompt included — speaks decoded JSON.
func plainWorkerSchema() map[string]any {
	body, err := json.Marshal(WorkerSchema())
	if err != nil {
		return map[string]any{}
	}
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return map[string]any{}
	}
	return objectOrEmpty(value)
}

// runSupervised executes the accepted supervisor pattern for one job
// directory: the request names the native job process, the supervisor claims
// the owner lock, records its identity, spawns and reaps the child, and
// writes exit.json. This process only waits — and on cancellation signals the
// supervisor's process group, exactly the SIGTERM path the supervisor turns
// into the deterministic 130 receipt.
func (c *Compose) runSupervised(ctx context.Context, taskID, jobDir string) (map[string]any, error) {
	request := map[string]any{
		"kind": "native",
		// argv as []any: the canonical encoder that digests this request
		// speaks plain decoded values, not typed slices.
		"argv":  []any{c.binary, "harness", "native", jobDir},
		"cwd":   c.cfg.Workspace,
		"stdin": "",
	}
	if err := writeAtomicJSON(filepath.Join(jobDir, "request.json"), request); err != nil {
		return nil, err
	}
	cmd := exec.Command(c.binary, "harness", "supervise", jobDir)
	cmd.SysProcAttr = supervisorSysProcAttr()
	cmd.Env = append(os.Environ(), "FORMAL_GATES_HARNESS_BINARY="+c.binary)
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("compose: cannot start the supervisor for %s: %w", jobDir, err)
	}
	job := &liveJob{taskID: taskID, dir: jobDir, cmd: cmd, pgid: jobProcessGroup(cmd)}
	c.trackJob(job, true)
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	var err error
	select {
	case err = <-waitErr:
	case <-ctx.Done():
		// Cancellation settles through the supervisor: signal its group and
		// let it write the cancellation receipt before we reap it.
		terminateJob(job)
		err = <-waitErr
	}
	c.trackJob(job, false)
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("compose: supervisor for %s exited %d without a receipt", jobDir, ee.ExitCode())
		}
		return nil, fmt.Errorf("compose: supervisor for %s failed: %w", jobDir, err)
	}
	receipt, err := readJSONFile(filepath.Join(jobDir, "exit.json"))
	if err != nil {
		return nil, fmt.Errorf("compose: supervisor for %s wrote no exit.json: %w", jobDir, err)
	}
	record, ok := receipt.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("compose: exit.json of %s is not an object", jobDir)
	}
	return record, nil
}

// settleNativeReceipt is the receipt→admission gate: the supervisor's receipt
// must still describe the request it was launched with (VerifyProcess, the
// accepted evidence contract), the child must have exited 0, and the framed
// business answer must be present. Anything else is no receipt — the
// invocation stays pending for the takeover consult to classify.
func settleNativeReceipt(jobDir string, receipt map[string]any) (map[string]any, error) {
	requestValue, err := readJSONFile(filepath.Join(jobDir, "request.json"))
	if err != nil {
		return nil, err
	}
	request, ok := requestValue.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("compose: request.json of %s is not an object", jobDir)
	}
	streams := objectOrEmpty(getOf(receipt, "streams"))
	proofs := []Proof{}
	for _, name := range []string{"stdout.log", "stderr.log"} {
		if bound, present := streams[name]; present {
			proofs = append(proofs, Proof{Name: name, Expected: objectOrEmpty(bound)})
		}
	}
	// The Python store passes the exit receipt itself as the process record
	// (handover.py); the comparison is what pins the receipt to the record.
	if kind, refusal := VerifyProcess(jobDir, receipt, receipt, request, proofs); refusal != "" {
		return nil, fmt.Errorf("compose: %s: %s: %s", jobDir, kind, refusal)
	}
	if !valuesEqual(getOf(receipt, "exit_code"), 0) {
		detail := ""
		if body, err := readJSONFile(filepath.Join(jobDir, "native-error.json")); err == nil {
			detail = textOf(getOf(body, "error"))
		}
		if detail == "" {
			detail = "native job exited " + textOf(getOf(receipt, "exit_code"))
		}
		return nil, fmt.Errorf("compose: %s: %s", jobDir, detail)
	}
	answerValue, err := readJSONFile(filepath.Join(jobDir, "answer.json"))
	if err != nil {
		return nil, fmt.Errorf("compose: %s produced no framed answer: %w", jobDir, err)
	}
	answer, ok := answerValue.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("compose: answer.json of %s is not an object", jobDir)
	}
	return answer, nil
}

// validateWorkerAnswer frames→validates the answer into a WorkerResult: a
// result the worker contract refuses is a runtime error, not a product
// verdict — it produces no receipt and waits for user disposition.
func validateWorkerAnswer(role string, answer map[string]any) (*WorkerResult, error) {
	// findings and check_ids must be explicit arrays; anything else is an
	// invalid worker result and is refused by ValidateWorker below.
	body, err := CanonicalJSON(answer)
	if err != nil {
		return nil, err
	}
	var worker WorkerResult
	if err := json.Unmarshal(body, &worker); err != nil {
		return nil, fmt.Errorf("compose: worker answer is not a valid result: %w", err)
	}
	if err := ValidateWorker(role, worker, nil); err != nil {
		return nil, fmt.Errorf("compose: worker answer refused: %w", err)
	}
	return &worker, nil
}

// nativeJobIdentity reads the session identity a completed native job
// established, for the continuation turn's resume.
func nativeJobIdentity(jobDir string) string {
	if source, err := readJSONFile(filepath.Join(jobDir, "source.json")); err == nil {
		if id := textOf(getOf(source, "identity")); id != "" {
			return id
		}
	}
	return ""
}

// relJobPath is a job dir relative to the run directory.
func relJobPath(taskID, invocationID string, attempt int) string {
	return filepath.ToSlash(filepath.Join("jobs", taskID, invocationID, fmt.Sprintf("turn-%02d", attempt)))
}

// trackJob registers/unregisters a live supervised call for cancellation and
// held-interaction watching.
func (c *Compose) trackJob(job *liveJob, on bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if on {
		c.live[job.dir] = job
	} else {
		delete(c.live, job.dir)
	}
}

// snapshotLive returns the currently in-flight supervised calls, ordered by
// job directory so settlement reports are stable.
func (c *Compose) snapshotLive() []*liveJob {
	c.mu.Lock()
	defer c.mu.Unlock()
	jobs := make([]*liveJob, 0, len(c.live))
	for _, job := range c.live {
		jobs = append(jobs, job)
	}
	sort.Slice(jobs, func(i, j int) bool { return jobs[i].dir < jobs[j].dir })
	return jobs
}

// watchHeld routes held native interactions (§6.2): while a supervised call
// runs, any client-directed request its host holds for the user is surfaced
// once, deduplicated, until it is answered through NativeRespond and the
// original turn continues.
func (c *Compose) watchHeld() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-c.baseCtx.Done():
			return
		case <-ticker.C:
		}
		for _, job := range c.snapshotLive() {
			records, err := readNativePendings(job.dir)
			if err != nil {
				continue
			}
			for _, raw := range records {
				record := objectOrEmpty(raw)
				if textOf(getOf(record, "status")) != "pending" {
					continue
				}
				requestID := textOf(getOf(record, "request_id"))
				key := job.dir + "\x00" + requestID
				c.mu.Lock()
				seen := c.announced[key]
				c.announced[key] = true
				c.mu.Unlock()
				if seen {
					continue
				}
				select {
				case c.heldCh <- heldNotice{taskID: job.taskID, jobDir: job.dir, requestID: requestID}:
				case <-c.baseCtx.Done():
					return
				}
			}
		}
	}
}

// heldInteraction builds the user-facing record for one surfaced notice,
// re-checking the request is still pending (a notice may have sat in the
// channel across Drive calls while the user already answered it).
func (c *Compose) heldInteraction(notice heldNotice) (HeldInteraction, bool) {
	records, err := readNativePendings(notice.jobDir)
	if err != nil {
		return HeldInteraction{}, false
	}
	for _, raw := range records {
		record := objectOrEmpty(raw)
		if textOf(getOf(record, "status")) != "pending" ||
			!valuesEqual(getOf(record, "request_id"), notice.requestID) {
			continue
		}
		return HeldInteraction{
			TaskID:    notice.taskID,
			JobDir:    notice.jobDir,
			RequestID: notice.requestID,
			Method:    textOf(getOf(record, "method")),
			Summary:   textOf(getOf(record, "summary")),
			Options:   getOf(record, "options"),
		}, true
	}
	return HeldInteraction{}, false
}

// drainStaleHeld discards buffered notices that are no longer pending, so a
// returned interaction is always one the caller can still answer.
func (c *Compose) drainStaleHeld() []HeldInteraction {
	var live []HeldInteraction
	for {
		select {
		case notice := <-c.heldCh:
			if held, ok := c.heldInteraction(notice); ok {
				live = append(live, held)
			}
		default:
			return live
		}
	}
}
