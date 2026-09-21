package harness

// The verification stages: candidate admission and snapshot, the authorized
// checks (one task per check), the gate reviews on frozen candidate clones,
// the route judgement, and the seal. Each is a kernel task whose executor
// applies the accepted bridge judgements (domain.go, bridge.go) — no
// judgement is re-derived here.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"formal-gates/internal/runner"
)

// executeCandidate admits over the two joined results (development and QA
// review), then applies the snapshot judgement over the development
// workspace keyed by {revision, round, development ref}. An admission
// refusal is a fatal stop, not an ask: every path that could refuse was
// already asked about inside the worker loops.
func (c *Compose) executeCandidate(inv runner.Invocation) (runner.Result, error) {
	dev := c.latestOutcome(taskDevelopment)
	qa := c.latestOutcome(taskQAReview)
	if dev == nil || qa == nil {
		return runner.Result{}, fmt.Errorf(
			"compose: candidate admission is missing an upstream outcome (development=%v, qa-review=%v)", dev != nil, qa != nil)
	}
	verdict := candidateAdmission(candidateAdmissionInput{
		DevelopmentNeedsRepair: dev.Worker != nil && NeedsRepair(*dev.Worker),
		QAReviewNeedsRepair:    qa.Worker != nil && NeedsRepair(*qa.Worker),
	})
	if verdict["ok"] != true {
		return runner.Result{}, fmt.Errorf("compose: candidate admission refused: %s", textOf(verdict["refusal"]))
	}
	round := c.repairRounds()
	root, err := c.developmentWorkspace()
	if err != nil {
		return runner.Result{}, err
	}
	op := Digest(struct {
		Revision    string `json:"revision"`
		Round       int    `json:"round"`
		Development string `json:"development"`
	}{c.spec.revision, round, outcomeRef(dev)})
	result, err := snapshot(workspaceInput{Root: root, OperationID: op})
	if err != nil {
		return runner.Result{}, err
	}
	candidate, _ := result.(map[string]any)
	hash := textOf(candidate["candidate"])
	if hash == "" {
		return runner.Result{}, fmt.Errorf("compose: snapshot judgement returned no candidate")
	}
	// The repair-no-change evidence: a repair round that produced the same
	// candidate as the previous round.
	if round > 0 {
		if previous := c.previousCandidate(); previous != "" && previous == hash {
			c.event("repair-no-change", map[string]any{"candidate": hash})
		}
	}
	record := outcomeRecord{
		Task: taskCandidate, Generation: inv.Generation, Action: "admitted",
		Candidate: hash,
	}
	if err := c.writeOutcome(record); err != nil {
		return runner.Result{}, err
	}
	return resultOf(&workerOutput{Generation: inv.Generation, Revision: c.spec.revision})
}

// repairRounds counts the completed repair rounds: one per development
// outcome whose brief carried the repair role. Derived, never stored.
func (c *Compose) repairRounds() int {
	rounds := 0
	for _, gen := range c.outcomeGenerations(taskDevelopment) {
		if outcome := c.readOutcome(taskDevelopment, gen); outcome != nil && outcome.Output != nil && outcome.Output.Role == "repair" {
			rounds++
		}
	}
	return rounds
}

// previousCandidate is the candidate of the round before the latest one.
func (c *Compose) previousCandidate() string {
	gens := c.outcomeGenerations(taskCandidate)
	if len(gens) < 2 {
		return ""
	}
	if outcome := c.readOutcome(taskCandidate, gens[len(gens)-2]); outcome != nil {
		return outcome.Candidate
	}
	return ""
}

// executeCheck runs one authorized check, one task per check:
// binding judgement, two-phase retention over the previous record (the
// branch condition lives only in the accepted check-retention judgement),
// a disposable clone at the candidate, the exact approved argv under the
// supervisor with the cache environment, and the Git tamper check.
func (c *Compose) executeCheck(ctx context.Context, inv runner.Invocation, check Check, round, attempt int) (runner.Result, error) {
	root, err := c.developmentWorkspace()
	if err != nil {
		return runner.Result{}, err
	}
	candidate := c.currentCandidate()
	if candidate == "" {
		return runner.Result{}, fmt.Errorf("compose: check %s has no frozen candidate", check.ID)
	}
	binding, err := scopeBinding(bindingInput{Root: root, Candidate: candidate, Revision: c.spec.revision, Check: check})
	if err != nil {
		return runner.Result{}, err
	}
	old := c.latestCheckRecord(check.ID)
	var oldSnapshot *retentionOld
	if old != nil && old.Status != "" {
		oldSnapshot = &retentionOld{Binding: old.Binding, Status: old.Status, Candidate: old.Candidate}
	}
	// Phase 1: is the previous record even eligible for re-verification?
	retention := checkRetention(checkRetentionInput{Binding: binding, Old: oldSnapshot})
	if textOf(retention["action"]) == "verify" {
		valid := c.verifyCheckEvidence(old) == nil
		// Phase 2: the verdict over the evidence fact. The branch
		// condition — fail on the first look, re-run after an explicit
		// retry — lives in the judgement, fed by the attempt counter.
		retention = checkRetention(checkRetentionInput{
			Binding: binding, Old: oldSnapshot, Candidate: candidate,
			Attempt: attempt, EvidenceValid: &valid,
		})
		switch action := textOf(retention["action"]); action {
		case "fail":
			return runner.Result{}, c.park(checkTaskID(check.ID), inv.Generation, attempt, disposition{
				Task: checkTaskID(check.ID), Kind: "check-runtime",
				Reason: fmt.Sprintf("check %s: retained evidence no longer verifies", check.ID),
			})
		case "run":
			c.event("check-evidence-invalid", map[string]any{
				"check": check.ID, "previous": old.Evidence, "retry_attempt": attempt,
			})
		}
	}
	if textOf(retention["action"]) == "retain" {
		retained := *old
		retained.Candidate = candidate
		retained.InheritedFrom = textOf(retention["inherited_from"])
		record := outcomeRecord{
			Task: checkTaskID(check.ID), Generation: inv.Generation, Action: "retained",
			Check: &retained, Retained: true, Candidate: candidate,
		}
		if err := c.writeOutcome(record); err != nil {
			return runner.Result{}, err
		}
		c.event("check-retained", map[string]any{"check": check.ID, "status": retained.Status, "binding": binding})
		return resultOf(&workerOutput{Generation: inv.Generation, Revision: c.spec.revision})
	}
	// Run: a disposable clone of the candidate, the exact approved argv,
	// the cache environment, and the Git tamper check after execution.
	operation := digestOfMap(map[string]any{
		"kind": "check", "candidate": candidate,
		"check": check, "binding": binding, "attempt": attempt,
	})
	workspace, err := c.workspaceClone(root, "check-"+operation[:24], candidate)
	if err != nil {
		return runner.Result{}, err
	}
	cache := filepath.Join(workspace, ".formal-gates-cache")
	if err := os.MkdirAll(filepath.Join(cache, "tmp"), 0o755); err != nil {
		return runner.Result{}, err
	}
	environment := map[string]string{
		"PYTHONDONTWRITEBYTECODE": "1",
		"GOCACHE":                 filepath.Join(cache, "go-build"),
		"GOTMPDIR":                filepath.Join(cache, "tmp"),
		"TMPDIR":                  filepath.Join(cache, "tmp"),
	}
	// The exact approved argv, verbatim: the supervisor runs argv[0]
	// itself — the composition never wraps or rewrites the user's command.
	argv := check.Argv
	receipt, err := c.runSupervised(ctx, checkTaskID(check.ID),
		filepath.Join(c.dir, "jobs", pathSafe(checkTaskID(check.ID)), inv.InvocationID, fmt.Sprintf("turn-%02d", attempt+1)),
		"check", argv, workspace, environment)
	if err != nil {
		return runner.Result{}, err
	}
	if err := checkReceiptRan(receipt); err != nil {
		return runner.Result{}, c.park(checkTaskID(check.ID), inv.Generation, attempt, disposition{
			Task: checkTaskID(check.ID), Kind: "check-runtime", Reasons: []string{err.Error()},
			Reason: err.Error(),
		})
	}
	if err := verifyCandidateIntact(workspace, candidate); err != nil {
		return runner.Result{}, c.park(checkTaskID(check.ID), inv.Generation, attempt, disposition{
			Task: checkTaskID(check.ID), Kind: "check-runtime", Reasons: []string{err.Error()},
			Reason: err.Error(),
		})
	}
	exitCode := intOf(getOf(receipt, "exit_code"))
	result := CheckResult{
		ID: check.ID, Candidate: candidate, Binding: binding,
		Status: "FAIL", ExitCode: exitCode, Evidence: requestDigestOf(receipt),
	}
	if exitCode == 0 {
		result.Status = "PASS"
	}
	record := outcomeRecord{
		Task: checkTaskID(check.ID), Generation: inv.Generation, Action: "executed",
		Check: &result, Candidate: candidate,
	}
	if err := c.writeOutcome(record); err != nil {
		return runner.Result{}, err
	}
	c.event("check-complete", map[string]any{"check": check.ID, "exit_code": exitCode})
	return resultOf(&workerOutput{Generation: inv.Generation, Revision: c.spec.revision})
}

// checkReceiptRan applies the execution-error rule: an error, a
// signal death or a shell-failure exit (126/127/130) means the check could
// not execute.
func checkReceiptRan(receipt map[string]any) error {
	if textOf(getOf(receipt, "error")) != "" {
		return fmt.Errorf("check could not execute: %s; inspect evidence", textOf(getOf(receipt, "error")))
	}
	exit := intOf(getOf(receipt, "exit_code"))
	if exit < 0 || exit == 126 || exit == 127 || exit == 130 {
		return fmt.Errorf("check could not execute: exit_code=%d; inspect evidence", exit)
	}
	return nil
}

// verifyCandidateIntact is the Git tamper check: HEAD must still be the
// candidate and the tracked tree must be clean.
func verifyCandidateIntact(workspace, candidate string) error {
	head, err := git(workspace, "rev-parse", "HEAD")
	if err != nil {
		return err
	}
	if head != candidate {
		return fmt.Errorf("verification changed the Git HEAD of the candidate")
	}
	status, err := git(workspace, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return err
	}
	if status != "" {
		return fmt.Errorf("verification changed tracked candidate files")
	}
	return nil
}

// latestCheckRecord finds the newest settled record of one check id (its
// own task's latest outcome), the retention judgement's "old".
func (c *Compose) latestCheckRecord(checkID string) *CheckResult {
	outcome := c.latestOutcome(checkTaskID(checkID))
	if outcome == nil {
		return nil
	}
	return outcome.Check
}

// verifyCheckEvidence re-verifies a settled record's process receipt: the
// job directory whose request the evidence reference digests must still
// hold a receipt that describes it.
func (c *Compose) verifyCheckEvidence(record *CheckResult) error {
	if record == nil || record.Evidence == "" {
		return fmt.Errorf("no recorded evidence")
	}
	jobDir := c.jobByRequestDigest(filepath.Join(c.dir, "jobs"), record.Evidence)
	if jobDir == "" {
		return fmt.Errorf("recorded evidence %s has no job directory", record.Evidence)
	}
	receiptValue, err := readJSONFile(filepath.Join(jobDir, "exit.json"))
	if err != nil {
		return err
	}
	receipt, ok := receiptValue.(map[string]any)
	if !ok {
		return fmt.Errorf("exit.json of %s is not an object", jobDir)
	}
	requestValue, err := readJSONFile(filepath.Join(jobDir, "request.json"))
	if err != nil {
		return err
	}
	request, ok := requestValue.(map[string]any)
	if !ok {
		return fmt.Errorf("request.json of %s is not an object", jobDir)
	}
	if _, refusal := VerifyProcess(jobDir, receipt, receipt, request, receiptProofs(receipt)); refusal != "" {
		return fmt.Errorf("%s", refusal)
	}
	return nil
}

// requestDigestOf is a receipt's request digest — the evidence reference a
// check record carries.
func requestDigestOf(receipt map[string]any) string {
	return textOf(getOf(receipt, "request_digest"))
}

// jobByRequestDigest finds a job directory whose request.json digests to ref.
func (c *Compose) jobByRequestDigest(root, ref string) string {
	var found string
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || found != "" || !entry.IsDir() {
			return nil
		}
		requestValue, err := readJSONFile(filepath.Join(path, "request.json"))
		if err != nil {
			return nil
		}
		request, ok := requestValue.(map[string]any)
		if !ok {
			return nil
		}
		digest, err := CanonicalDigest(request)
		if err == nil && digest == ref {
			found = path
		}
		return nil
	})
	return found
}

// receiptProofs extracts the stream proofs a receipt names.
func receiptProofs(receipt map[string]any) []Proof {
	streams := objectOrEmpty(getOf(receipt, "streams"))
	proofs := make([]Proof, 0, len(streams))
	for name, bound := range streams {
		proofs = append(proofs, Proof{Name: name, Expected: objectOrEmpty(bound)})
	}
	return proofs
}

// executeGate runs one gate review on a frozen candidate clone: the worker
// loop (read-only role, no feedback), then the Git tamper check. A gate
// that changed the candidate is blocked as a fatal runtime stop reported
// to the user, never a PASS.
func (c *Compose) executeGate(ctx context.Context, inv runner.Invocation, gate string) (runner.Result, error) {
	brief := workerBrief{Stage: gateTaskID(gate), Role: gate, Revision: c.spec.revision}
	// Gates read a clone of the frozen candidate: route the loop there by
	// cloning ahead of the call and pointing the workspace at it.
	candidate := c.currentCandidate()
	if candidate == "" {
		return runner.Result{}, fmt.Errorf("compose: gate %s has no frozen candidate", gate)
	}
	root, err := c.developmentWorkspace()
	if err != nil {
		return runner.Result{}, err
	}
	workspace, err := c.workspaceClone(root, gate+"-"+candidate[:16], candidate)
	if err != nil {
		return runner.Result{}, err
	}
	identity := ""
	attempt := 0
	series := c.seriesKey(gateTaskID(gate), inv.Generation, brief)
	for {
		attempt++
		if output, ok := c.replayWorkerTurn(gateTaskID(gate), inv.Generation, series, attempt); ok {
			return c.finishGate(gateTaskID(gate), inv.Generation, candidate, output)
		}
		jobDir := filepath.Join(c.dir, "jobs", pathSafe(gateTaskID(gate)), inv.InvocationID, fmt.Sprintf("turn-%02d", attempt))
		if err := os.MkdirAll(jobDir, 0o755); err != nil {
			return runner.Result{}, err
		}
		key := c.turnKey(series, attempt)
		materials, err := c.writeMaterials(jobDir, key, brief, workspace, false, nil)
		if err != nil {
			return runner.Result{}, err
		}
		binding, err := c.cfg.Binding(NativeCall{
			TaskID: gateTaskID(gate), Role: gate, Generation: inv.Generation, Attempt: attempt, JobDir: jobDir,
		})
		if err != nil {
			return runner.Result{}, err
		}
		if err := c.writeNativeRequest(jobDir, brief, workspace, binding, identity, materials); err != nil {
			return runner.Result{}, err
		}
		receipt, err := c.runSupervised(ctx, gateTaskID(gate), jobDir, "native", c.nativeJobArgv(jobDir), workspace, map[string]string{
			"LANGSMITH_TRACING": "false", "LANGCHAIN_TRACING_V2": "false",
		})
		if err != nil {
			return runner.Result{}, err
		}
		answer, err := settleNativeReceipt(jobDir, receipt)
		if err != nil {
			return runner.Result{}, c.parkRuntime(gateTaskID(gate), inv.Generation, attempt, err)
		}
		worker, err := validateWorkerAnswer(gate, answer, c.spec.spec.Checks)
		if err != nil {
			return runner.Result{}, c.parkRuntime(gateTaskID(gate), inv.Generation, attempt, err)
		}
		output := &workerOutput{
			Result: *worker, NeedsRepair: NeedsRepair(*worker), Role: gate,
			Revision: c.spec.revision, Generation: inv.Generation, Series: series,
			Turn: attempt, Job: relJobPath(gateTaskID(gate), inv.InvocationID, attempt),
			Identity: turnIdentity(jobDir),
		}
		if err := c.recordWorkerTurn(output); err != nil {
			return runner.Result{}, err
		}
		c.event("worker-complete", map[string]any{"role": gate, "task": gateTaskID(gate), "status": worker.Status})
		action := textOf(workerOutcome(workerOutcomeInput{
			Role: gate, Status: worker.Status, NeedsRepair: output.NeedsRepair})["action"])
		switch action {
		case "continue":
			identity = output.Identity
			continue
		case "blocked":
			return runner.Result{}, c.park(gateTaskID(gate), inv.Generation, attempt, disposition{
				Task: gateTaskID(gate), Kind: "worker-blocked", Reason: worker.Summary,
				Evidence: turnEvidenceRef(gateTaskID(gate), inv.Generation, series, attempt),
			})
		default:
			// A gate verdict — PASS or FAIL with findings — is a legitimate
			// admitted result: the route judgement consumes it.
			if err := verifyCandidateIntact(workspace, candidate); err != nil {
				return runner.Result{}, fmt.Errorf("compose: read-only gate changed the candidate: %w", err)
			}
			return c.finishGate(gateTaskID(gate), inv.Generation, candidate, output)
		}
	}
}

// finishGate records and admits one gate verdict.
func (c *Compose) finishGate(taskID string, generation int64, candidate string, output *workerOutput) (runner.Result, error) {
	record := outcomeRecord{
		Task: taskID, Generation: generation, Action: "gate",
		Worker: &output.Result, Output: output, Candidate: candidate,
	}
	if err := c.writeOutcome(record); err != nil {
		return runner.Result{}, err
	}
	return resultOf(output)
}

// executeEvaluate applies the route judgement over the full check and gate
// records. A repair route also publishes the
// failed-verification bundle the repair round works from — the durable
// feedback every later transition derives the repair brief from.
func (c *Compose) executeEvaluate(inv runner.Invocation) (runner.Result, error) {
	var checks []routeCheck
	var checkRecords []CheckResult
	for _, check := range c.spec.spec.Checks {
		outcome := c.latestOutcome(checkTaskID(check.ID))
		if outcome == nil || outcome.Check == nil {
			return runner.Result{}, fmt.Errorf("compose: evaluate is missing the record of check %s", check.ID)
		}
		checks = append(checks, routeCheck{ID: check.ID, Status: outcome.Check.Status})
		checkRecords = append(checkRecords, *outcome.Check)
	}
	var gates []routeGate
	for _, gate := range c.spec.spec.Gates {
		outcome := c.latestOutcome(gateTaskID(gate))
		if outcome == nil || outcome.Worker == nil {
			return runner.Result{}, fmt.Errorf("compose: evaluate is missing the record of gate %s", gate)
		}
		gates = append(gates, routeGate{Gate: gate, Result: *outcome.Worker})
	}
	verdict := evaluateRoute(evaluateRouteInput{Checks: checks, Gates: gates})
	route := textOf(verdict["route"])
	record := outcomeRecord{Task: taskEvaluate, Generation: inv.Generation, Action: route, Route: route}
	if route == "repair" {
		// The route judgement builds its two id lists itself as []string
		// (domain.go) — they are not decoded JSON, so listOf, whose domain is
		// the decoded []any shape, would coerce them to empty and strip the
		// repair bundle of its failed material. Assert the actual type.
		failedIDs, _ := verdict["failed_checks"].([]string)
		failed := map[string]bool{}
		for _, id := range failedIDs {
			failed[id] = true
		}
		blockingIDs, _ := verdict["blocking_gates"].([]string)
		blocking := map[string]bool{}
		for _, gate := range blockingIDs {
			blocking[gate] = true
		}
		var failedChecks []CheckResult
		for _, result := range checkRecords {
			if failed[result.ID] {
				failedChecks = append(failedChecks, result)
			}
		}
		var blockingGates []map[string]any
		for _, gate := range c.spec.spec.Gates {
			if blocking[gate] {
				if outcome := c.latestOutcome(gateTaskID(gate)); outcome != nil && outcome.Worker != nil {
					blockingGates = append(blockingGates, map[string]any{"gate": gate, "result": *outcome.Worker})
				}
			}
		}
		bundle, err := canonicalBody(map[string]any{
			"failed_checks": failedChecks, "gate_findings": blockingGates,
			"candidate": c.currentCandidate(), "revision": c.spec.revision,
		})
		if err != nil {
			return runner.Result{}, err
		}
		ref := digestBytes(bundle)
		if err := writeAtomicBytes(filepath.Join(c.dir, "artifacts", ref+".json"), bundle); err != nil {
			return runner.Result{}, err
		}
		record.Feedback = ref
	}
	if err := c.writeOutcome(record); err != nil {
		return runner.Result{}, err
	}
	return resultOf(&workerOutput{Generation: inv.Generation, Revision: c.spec.revision})
}

// evaluateFeedbackRef is the latest evaluate outcome's feedback bundle ref.
func (c *Compose) evaluateFeedbackRef() string {
	if outcome := c.latestOutcome(taskEvaluate); outcome != nil {
		return outcome.Feedback
	}
	return ""
}

// executeSeal verifies every accepted evidence,
// certify, publish harness-result.json atomically, and record the terminal.
// Completion is this record plus the file — neither alone.
func (c *Compose) executeSeal(inv runner.Invocation) (runner.Result, error) {
	// The route must be seal at the current chain.
	if outcome := c.latestOutcome(taskEvaluate); outcome == nil || outcome.Route != "seal" {
		// The route is exclusive — repair or seal, never both — but the
		// static task graph dispatches seal the moment
		// evaluate finishes, so the guard parks the invocation instead.
		// The marker is inert to the transitions — the repair route's own
		// revision supersedes it — and exists so recovery never mistakes
		// the parked seal for a dead call.
		return runner.Result{}, c.park(taskSeal, inv.Generation, 0, disposition{
			Task: taskSeal, Kind: "route-guard", Generation: inv.Generation,
			Reason: "the verification route is repair, not seal",
		})
	}
	candidate := c.currentCandidate()
	if candidate == "" {
		return runner.Result{}, fmt.Errorf("compose: seal has no frozen candidate")
	}
	var checks []CheckResult
	for _, check := range c.spec.spec.Checks {
		outcome := c.latestOutcome(checkTaskID(check.ID))
		if outcome == nil || outcome.Check == nil {
			return runner.Result{}, fmt.Errorf("compose: seal is missing the record of check %s", check.ID)
		}
		if outcome.Check.Evidence != "" {
			if err := c.verifyCheckEvidence(outcome.Check); err != nil {
				return runner.Result{}, fmt.Errorf("compose: check %s evidence: %w", check.ID, err)
			}
		}
		checks = append(checks, *outcome.Check)
	}
	var gates []GateResult
	for _, gate := range c.spec.spec.Gates {
		outcome := c.latestOutcome(gateTaskID(gate))
		if outcome == nil || outcome.Worker == nil {
			return runner.Result{}, fmt.Errorf("compose: seal is missing the record of gate %s", gate)
		}
		gates = append(gates, GateResult{
			ID: gate, Candidate: outcome.Candidate, Revision: c.spec.revision,
			Evidence: outcomeRef(outcome), Result: *outcome.Worker,
		})
	}
	root, err := c.developmentWorkspace()
	if err != nil {
		return runner.Result{}, err
	}
	certificate, err := certify(sealInput{
		Spec: c.spec.spec, Revision: c.spec.revision, Root: root,
		Candidate: candidate, Checks: checks, Gates: gates,
	})
	if err != nil {
		return runner.Result{}, err
	}
	cert, _ := certificate.(map[string]any)
	status := textOf(cert["status"])
	body, err := canonicalBody(cert)
	if err != nil {
		return runner.Result{}, err
	}
	if err := writeAtomicBytes(filepath.Join(c.dir, "harness-result.json"), body); err != nil {
		return runner.Result{}, err
	}
	current := c.readRecord()
	current.Terminal = status
	if err := c.writeRecord(current); err != nil {
		return runner.Result{}, err
	}
	record := outcomeRecord{
		Task: taskSeal, Generation: inv.Generation, Action: "seal",
		Status: status, Terminal: digestBytes(body),
	}
	if err := c.writeOutcome(record); err != nil {
		return runner.Result{}, err
	}
	c.event("terminal", map[string]any{"status": status, "candidate": candidate})
	return resultOf(&workerOutput{Generation: inv.Generation, Revision: c.spec.revision})
}

// terminalJudgment is the completeness check after a clean drain: the seal
// stage must have certified AND the result record must exist on disk — the
// R1/R2 rule that a FAIL chain can never report completion.
func (c *Compose) terminalJudgment() (ComposeState, bool) {
	if _, err := os.Stat(filepath.Join(c.dir, "harness-result.json")); err != nil {
		return ComposeState{Status: StatusFailed,
			Err: fmt.Errorf("compose: the run drained without a sealed result")}, true
	}
	seal := c.latestOutcome(taskSeal)
	if seal == nil || seal.Status == "" {
		return ComposeState{Status: StatusFailed,
			Err: fmt.Errorf("compose: the run drained without a seal record")}, true
	}
	current := c.readRecord()
	current.Terminal = seal.Status
	_ = c.writeRecord(current)
	return ComposeState{Status: StatusComplete, Terminal: seal.Status}, true
}

// intOf renders a decoded JSON number as an int (-1 when it is not one).
func intOf(value any) int {
	if number, ok := value.(json.Number); ok {
		if n, err := number.Int64(); err == nil {
			return int(n)
		}
	}
	return -1
}
