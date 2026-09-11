package facade

import (
	"bytes"
	"encoding/json"
	"fmt"
	"formal-gates/internal/coordination"
	"formal-gates/internal/cost"
	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/persistence"
	"formal-gates/internal/engine/protocol"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// executeCodex is the Codex adapter's native model transport. It consumes an
// issued action before starting a process. The command is constructed here, not
// provided by a request or a model. B1 reviews need no tools: disabling shell,
// nested agents, MCP user configuration and web search leaves only the document
// and the result contract available to this independent, fresh session.
func (f *Facade) executeCodex(actionID string, resume bool) (Run, error) {
	run, err := f.Show()
	if err != nil {
		return Run{}, err
	}
	if run.Regular == nil {
		return Run{}, fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: Codex review transport requires regular")
	}
	execution, ok := run.Executions[actionID]
	if !ok {
		return Run{}, fmt.Errorf("UNKNOWN_ACTION: %s", actionID)
	}
	// Resolve and validate the binding before CLAIM, recovery, or any native call.
	if execution.Claim.Context == nil {
		if execution.Claimed || execution.Identity != "" {
			return Run{}, fmt.Errorf("RUNTIME_CONTEXT_MISSING: original task context is unavailable")
		}
		execution.Claim.Context, err = captureRuntimeContext(execution.Claim.Provider, run.Regular.Settings.Values)
		if err != nil {
			return Run{}, err
		}
	}
	probe := &exec.Cmd{Args: []string{execution.Claim.Context.Executable}}
	if err = runtimeCommandContext(probe, execution.Claim.Context); err != nil {
		return Run{}, err
	}
	// The owning driver and abort collector share one action lock. Abort may
	// stop its process while this call runs, then waits for the owner to settle
	// its result before deciding whether any collection remains necessary.
	unlock, err := coordination.AcquirePath(f.codexJob(actionID)+".lock", "native action")
	if err != nil {
		return Run{}, err
	}
	defer unlock()
	if !resume && execution.Claim.Task.Step == "development-worker" && execution.Claim.Operation == "resume" {
		return Run{}, fmt.Errorf("DEVELOPMENT_CONTINUITY: unfinished batch requires --resume with its bound identity")
	}
	continuation := resume && !execution.Claimed && execution.Claim.Task.Step == "development-worker" && execution.Claim.Operation == "resume"
	replacementRecovery := resume && execution.Recovery != nil && execution.Recovery.Outcome() == "UNRECOVERABLE" && !execution.ReplacementClaimed
	interrupted := resume && !continuation && !replacementRecovery
	if interrupted {
		recovered, complete, recoveryErr := f.reconcileCodex(actionID, execution)
		if recoveryErr != nil {
			return Run{}, recoveryErr
		}
		if complete {
			return recovered, nil
		}
		run, err = f.Show()
		if err != nil {
			return Run{}, err
		}
		execution = run.Executions[actionID]
	}
	if (!resume && execution.Claimed) || (resume && execution.ResumeClaimed && !replacementRecovery) || execution.Finished {
		return Run{}, fmt.Errorf("DUPLICATE_ACTION: execution was already claimed or consumed")
	}
	claim := execution.Claim
	if resume {
		claim.Operation = "resume"
		claim.Identity = execution.Identity
	}
	if claim.Task.Step != "product-review" && claim.Task.Step != "start-readiness" && !preparationAction(string(claim.Task.Step)) && !verificationAction(string(claim.Task.Step)) {
		return Run{}, fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: no B1 transport for %s", claim.Task.Step)
	}
	ev, _ := protocol.NewRegularEvent(protocol.EventID("claim:"+actionID+":"+claim.Operation), protocol.RegularEvent{Operation: "CLAIM", ExpectedRevision: run.Revision, Claim: &claim})
	// A rejected or duplicate claim returns here; neither command construction nor
	// subprocess creation occurs on these paths.
	if !replacementRecovery {
		if _, _, err = f.submitAdapterRegular(string(ev.ID), *ev.Regular); err != nil {
			return Run{}, err
		}
	}
	if resume && claim.Task.Step == "development-worker" {
		var recovery protocol.DeveloperRecovery
		var recoveryErr error
		if replacementRecovery {
			recovery = *execution.Recovery
		} else {
			recovery, recoveryErr = f.observeDeveloperRecovery(run, execution, interrupted)
		}
		if recoveryErr != nil {
			return Run{}, recoveryErr
		}
		switch recovery.Outcome() {
		case "RESUMABLE":
		case "UNRECOVERABLE":
			replacement := claim
			replacement.Operation = "replace"
			if _, _, err = f.submitAdapterRegular("replace:"+actionID, protocol.RegularEvent{Operation: "CLAIM", Claim: &replacement}); err != nil {
				return Run{}, err
			}
			resume = false
			claim.Identity = ""
			claim.Operation = "replace"
		default:
			return f.failedCodexStart(actionID, claim, fmt.Errorf("native recovery is UNKNOWN: %s %s", recovery.NativeResponse, recovery.TransportError))
		}
	}
	job := f.codexJob(actionID)
	if interrupted {
		job = codexResumeJob(job, execution.ResumeCount+1)
	}
	if err = os.MkdirAll(job, 0700); err != nil {
		return Run{}, err
	}
	schemaPath := filepath.Join(job, "result-schema.json")
	schema := reviewSchema(claim)
	if preparationAction(string(claim.Task.Step)) {
		schema = preparationSchema(string(claim.Task.Step), run, claim)
	}
	if verificationAction(string(claim.Task.Step)) {
		schema = verificationSchema(claim)
	}
	if err = writeJSONAtomic(schemaPath, schema); err != nil {
		return Run{}, err
	}
	var prompt string
	workspace := job
	if preparationAction(string(claim.Task.Step)) {
		prompt, workspace, err = preparationPrompt(run, claim)
		if err != nil {
			return Run{}, err
		}
	}
	if verificationAction(string(claim.Task.Step)) {
		prompt, workspace, err = verificationPrompt(run, claim)
		if err != nil {
			return Run{}, err
		}
	}
	if !preparationAction(string(claim.Task.Step)) && !verificationAction(string(claim.Task.Step)) {
		prompt, err = reviewPrompt(run, claim)
		if err != nil {
			return Run{}, err
		}
	}
	if err = os.WriteFile(filepath.Join(job, "prompt.txt"), []byte(prompt), 0600); err != nil {
		return Run{}, err
	}
	identity, nativeErr := f.runNativeBridge(run, claim, resume, job, workspace, prompt, schema)
	if identity == "" {
		return f.failedCodexStart(actionID, claim, nativeErr)
	}
	if nativeErr == nil && f.fault != nil {
		if err = f.fault(persistence.FaultExecuteAfter); err != nil {
			return Run{}, err
		}
	}
	return f.finishCodex(actionID, claim, identity, job, nativeErr)
}

func (f *Facade) finishCodex(actionID string, claim protocol.ExecutionClaim, identity, job string, waitErr error) (Run, error) {
	answer := filepath.Join(job, "answer.json")
	transcript, sourceErr := findBoundTranscript(identity, claim.Context)
	source := protocol.ExecutionSource{ActionID: actionID, AttemptID: claim.AttemptID, Provider: "codex", Identity: identity, Status: "STOPPED", TranscriptPath: transcript}
	if sourceErr == nil && waitErr != nil {
		if _, parseErr := cost.ParseTranscript("codex", transcript); parseErr != nil {
			sourceErr = fmt.Errorf("native process failed (%v) and transcript usage is unavailable: %w", waitErr, parseErr)
		}
	}
	if sourceErr != nil {
		source.Status = "UNAVAILABLE"
		source.Reason = sourceErr.Error()
	}
	// Return the raw result first. Source settlement follows through submit and
	// blocks any new issue in between, including after a process interruption.
	data, readErr := os.ReadFile(answer)
	if verificationAction(string(claim.Task.Step)) {
		return f.finishVerification(claim, identity, data, readErr, waitErr, source)
	}
	if preparationAction(string(claim.Task.Step)) {
		var result protocol.PreparationResult
		decodeErr := json.Unmarshal(bytes.TrimSpace(data), &result)
		if waitErr != nil || readErr != nil || decodeErr != nil {
			result = protocol.PreparationResult{ActionID: actionID, AttemptID: claim.AttemptID, Provider: claim.Provider, RequirementRevision: claim.RequirementRevision, Candidate: claim.Candidate, Status: "RUNTIME_ERROR", Message: fmt.Sprintf("native execution=%v answer=%v decode=%v", waitErr, readErr, decodeErr)}
		}
		result.Identity = identity
		current, err := f.Show()
		if err != nil {
			return Run{}, err
		}
		if claim.Task.Step == "development-worker" && result.Status == "PASS" {
			p := current.Regular.Preparation
			if err := gitprep.CheckManifest(p.Resources["development"], p.DevelopmentSnapshot, result.Manifest); err != nil {
				result.Status = "FAIL"
				result.Message = "Development delivery validation failed; correct the manifest in this same batch identity: " + err.Error()
			}
		}
		ev, _ := protocol.NewRegularEvent(protocol.EventID("result:"+actionID), protocol.RegularEvent{Operation: "PREPARATION_RESULT", ExpectedRevision: current.Revision, PreparationResult: &result})
		if !current.Executions[actionID].Finished {
			if _, _, err = f.submitAdapterRegular(string(ev.ID), *ev.Regular); err != nil {
				return Run{}, err
			}
		}
		if err = f.submitSource(source); err != nil {
			return Run{}, err
		}
		return f.Show()
	}
	var result protocol.RawReviewCandidate
	decodeErr := json.Unmarshal(bytes.TrimSpace(data), &result)
	if waitErr != nil || readErr != nil || decodeErr != nil {
		result = protocol.RawReviewCandidate{ActionID: actionID, AttemptID: claim.AttemptID, Provider: "codex", Action: string(claim.Task.Step), RequirementRevision: claim.RequirementRevision, Candidate: claim.Candidate, Status: "RUNTIME_ERROR", Message: fmt.Sprintf("native execution=%v answer=%v decode=%v", waitErr, readErr, decodeErr)}
	}
	result.Identity = identity
	current, err := f.Show()
	if err != nil {
		return Run{}, err
	}
	resultEvent, _ := protocol.NewRegularEvent(protocol.EventID("result:"+actionID), protocol.RegularEvent{Operation: "REVIEW_RESULT", ExpectedRevision: current.Revision, Review: &result})
	if !current.Executions[actionID].Finished {
		if _, _, err = f.submitAdapterRegular(string(resultEvent.ID), *resultEvent.Regular); err != nil {
			return Run{}, err
		}
	}
	if err = f.submitSource(source); err != nil {
		return Run{}, err
	}
	return f.Show()
}

func (f *Facade) submitSource(source protocol.ExecutionSource) error {
	run, err := f.Show()
	if err != nil {
		return err
	}
	if old := run.Executions[source.ActionID].Source; old != nil && *old == source && string(run.Next.Kind) != "HOST_ACTION" {
		return nil
	}
	ev, _ := protocol.NewRegularEvent(protocol.EventID(fmt.Sprintf("source:%s:%s:%d", source.ActionID, source.Status, run.Revision)), protocol.RegularEvent{Operation: "SOURCE", ExpectedRevision: run.Revision, Source: &source})
	_, _, err = f.submitAdapterRegular(string(ev.ID), *ev.Regular)
	return err
}
func reviewPrompt(run Run, claim protocol.ExecutionClaim) (string, error) {
	instructions, err := actionInstructions(run, "actions/development-worker.md#通用执行约定", "actions/"+string(claim.Task.Step)+".md")
	if err != nil {
		return "", err
	}
	return assemblePrompt(instructions, map[string]any{"binding": claim, "requirement": run.Regular.Requirement})
}

func reviewSchema(claim protocol.ExecutionClaim) map[string]any {
	str := map[string]any{"type": "string"}
	exact := func(value string) any { return map[string]any{"type": "string", "enum": []string{value}} }
	array := func(item any) map[string]any { return map[string]any{"type": "array", "items": item} }
	object := func(properties map[string]any) map[string]any {
		required := []string{}
		for name := range properties {
			required = append(required, name)
		}
		sort.Strings(required)
		return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
	}
	finding := object(map[string]any{"id": str, "severity": map[string]any{"type": "string", "enum": []string{"P0", "P1", "P2", "P3"}}, "category": str, "requirement": str, "problem": str, "normalPath": str, "consequence": str, "severityRationale": str, "remedy": str})
	fields := map[string]any{"actionId": exact(claim.ActionID), "attemptId": exact(claim.AttemptID), "provider": exact(claim.Provider), "identity": exact("NATIVE_IDENTITY"), "action": exact(string(claim.Task.Step)), "requirementRevision": exact(claim.RequirementRevision), "candidate": exact(claim.Candidate), "status": map[string]any{"type": "string", "enum": []string{"PASS", "FAIL"}}, "message": str, "complete": map[string]any{"type": "boolean"}, "findings": array(finding)}
	if claim.Task.Step == "start-readiness" {
		subtask := object(map[string]any{"id": str, "task": str})
		batch := object(map[string]any{"id": str, "task": str, "subtasks": array(subtask), "dependencies": array(str), "interfaces": str, "stateBoundary": str, "risks": str, "definitionOfDone": str, "validation": str, "recovery": str, "handoff": str, "contextCost": str})
		batch["properties"].(map[string]any)["dependencies"].(map[string]any)["description"] = "Only other Batch IDs declared in this plan; use [] when there are no cross-batch dependencies. Same-Batch Subtask order belongs in ordered subtasks, not dependencies. Exclude Subtask IDs, environment prerequisites and explanatory sentences."
		fields["granularityReview"] = object(map[string]any{"batches": array(batch), "rationale": str, "requirementRevision": exact(claim.RequirementRevision), "digest": str})
	}
	return object(fields)
}

// The invocation receipt is an adapter-owned recovery artifact for the native
// process, not a workflow writer. UNKNOWN without it never causes a second call.
type codexProcess struct {
	PID      int    `json:"pid"`
	ActionID string `json:"actionId"`
	Stopped  bool   `json:"stopped"`
}

func (f *Facade) codexJob(actionID string) string {
	return filepath.Join(f.stateDir, "executions", strings.ReplaceAll(strings.TrimPrefix(actionID, "act:"), "/", "_"))
}
func (f *Facade) reconcileCodex(actionID string, x protocol.RegularExecution) (Run, bool, error) {
	if !x.Claimed {
		return Run{}, false, fmt.Errorf("INTENT_MISMATCH: unclaimed action cannot be resumed")
	}
	if x.Finished && x.Source != nil && x.Source.Status != "RUNNING" {
		return Run{}, false, fmt.Errorf("DUPLICATE_ACTION: completed execution cannot be resumed")
	}
	job := f.codexJob(actionID)
	if x.ResumeClaimed || x.ResumeCount > 0 {
		job = codexResumeJob(job, x.ResumeCount)
	}
	data, err := os.ReadFile(filepath.Join(job, "process.json"))
	if err != nil {
		return Run{}, false, fmt.Errorf("UNKNOWN: no native process receipt for the claim; reconcile before retry: %w", err)
	}
	var process codexProcess
	if err = json.Unmarshal(data, &process); err != nil || process.ActionID != actionID {
		return Run{}, false, fmt.Errorf("INTENT_MISMATCH: native process receipt is invalid")
	}
	if !process.Stopped && codexProcessAlive(process.PID) {
		return Run{}, false, fmt.Errorf("HOST_ACTION_RUNNING: bound native process %d is still running", process.PID)
	}
	if run, err := f.Show(); err == nil && process.Stopped && checkpointCanResume(run, x) {
		return run, false, nil
	}
	identity := x.Identity
	if identity == "" {
		data, err = os.ReadFile(filepath.Join(job, "stdout.jsonl"))
		if err != nil {
			return Run{}, false, err
		}
		for _, line := range strings.Split(string(data), "\n") {
			var event struct {
				Type     string `json:"type"`
				ThreadID string `json:"thread_id"`
			}
			if json.Unmarshal([]byte(line), &event) == nil && event.Type == "thread.started" {
				identity = event.ThreadID
			}
			var native struct {
				Method string `json:"method"`
				Params struct {
					Thread struct {
						ID string `json:"id"`
					} `json:"thread"`
				} `json:"params"`
				Result struct {
					Thread struct {
						ID string `json:"id"`
					} `json:"thread"`
				} `json:"result"`
			}
			if json.Unmarshal([]byte(line), &native) == nil {
				if native.Method == "thread/started" && native.Params.Thread.ID != "" {
					identity = native.Params.Thread.ID
				}
				if native.Result.Thread.ID != "" {
					identity = native.Result.Thread.ID
				}
			}
		}
	}
	if identity == "" {
		return Run{}, false, fmt.Errorf("UNKNOWN: native process has no observed thread identity; no automatic retry")
	}
	if x.Identity == "" {
		if err = f.submitSource(protocol.ExecutionSource{ActionID: actionID, AttemptID: x.Claim.AttemptID, Provider: "codex", Identity: identity, Status: "RUNNING"}); err != nil {
			return Run{}, false, err
		}
	}
	if _, err = os.Stat(filepath.Join(job, "answer.json")); err == nil {
		r, err := f.finishCodex(actionID, x.Claim, identity, job, nil)
		return r, true, err
	}
	if x.ResumeClaimed {
		r, err := f.finishCodex(actionID, x.Claim, identity, job, fmt.Errorf("interrupted native resume has no result"))
		return r, true, err
	}
	return Run{}, false, nil
}

func (f *Facade) failedCodexStart(actionID string, claim protocol.ExecutionClaim, cause error) (Run, error) {
	src := protocol.ExecutionSource{ActionID: actionID, AttemptID: claim.AttemptID, Provider: "codex", Identity: claim.Identity, Status: "UNAVAILABLE", Reason: "Native execution ended without an observed thread; source unavailable: " + cause.Error()}
	if err := f.submitSource(src); err != nil {
		return Run{}, err
	}
	run, err := f.Show()
	if err != nil {
		return Run{}, err
	}
	if verificationAction(string(claim.Task.Step)) {
		return f.finishVerification(claim, claim.Identity, nil, cause, cause, src)
	}
	if preparationAction(string(claim.Task.Step)) {
		raw := protocol.PreparationResult{ActionID: actionID, AttemptID: claim.AttemptID, Provider: claim.Provider, Identity: claim.Identity, RequirementRevision: claim.RequirementRevision, Candidate: claim.Candidate, Status: "RUNTIME_ERROR", Message: src.Reason}
		ev, _ := protocol.NewRegularEvent(protocol.EventID("result:"+actionID), protocol.RegularEvent{Operation: "PREPARATION_RESULT", ExpectedRevision: run.Revision, PreparationResult: &raw})
		_, run, err = f.submitAdapterRegular(string(ev.ID), *ev.Regular)
		return run, err
	}
	raw := protocol.RawReviewCandidate{ActionID: actionID, AttemptID: claim.AttemptID, Identity: claim.Identity, Action: string(claim.Task.Step), Provider: "codex", RequirementRevision: claim.RequirementRevision, Candidate: claim.Candidate, Status: "RUNTIME_ERROR", Message: src.Reason}
	ev, _ := protocol.NewRegularEvent(protocol.EventID("result:"+actionID), protocol.RegularEvent{Operation: "REVIEW_RESULT", ExpectedRevision: run.Revision, Review: &raw})
	_, run, err = f.submitAdapterRegular(string(ev.ID), *ev.Regular)
	return run, err
}

// finishRegularAbort executes only the native processes created for this run's
// already authorized actions. Unknown process/identity receipts stay visible and
// cannot be turned into a successful cleanup by guessing that no call occurred.
func (f *Facade) finishRegularAbort() (Run, error) {
	run, err := f.Show()
	if err != nil {
		return Run{}, err
	}
	if err = f.abortTaskOperations(run); err != nil {
		return Run{}, err
	}
	for id, x := range run.Executions {
		if !x.Claimed {
			continue
		}
		if x.Finished && x.Source != nil && x.Source.Status != "RUNNING" {
			continue
		}
		job := f.codexJob(id)
		if x.ResumeClaimed || x.ResumeCount > 0 {
			job = codexResumeJob(job, x.ResumeCount)
		}
		data, err := os.ReadFile(filepath.Join(job, "process.json"))
		if err != nil {
			return Run{}, fmt.Errorf("UNKNOWN: abort needs the native process receipt for %s: %w", id, err)
		}
		var receipt codexProcess
		if err = json.Unmarshal(data, &receipt); err != nil || receipt.ActionID != id {
			return Run{}, fmt.Errorf("INTENT_MISMATCH: abort process receipt")
		}
		if !receipt.Stopped && codexProcessAlive(receipt.PID) {
			if err = stopCodexProcess(receipt.PID); err != nil {
				return Run{}, err
			}
			deadline := time.Now().Add(3 * time.Second)
			for codexProcessAlive(receipt.PID) && time.Now().Before(deadline) {
				time.Sleep(20 * time.Millisecond)
			}
			if codexProcessAlive(receipt.PID) {
				return Run{}, fmt.Errorf("HOST_ACTION_RUNNING: native process group %d is still stopping", receipt.PID)
			}
		}
		// A still-running original driver owns result collection. Killing its
		// native process does not make it safe to submit a competing result.
		var release func()
		deadline := time.Now().Add(3 * time.Second)
		for {
			release, err = coordination.AcquirePath(f.codexJob(id)+".lock", "native abort collection")
			if err == nil || time.Now().After(deadline) {
				break
			}
			time.Sleep(10 * time.Millisecond)
		}
		if err != nil {
			return Run{}, err
		}
		collectErr := func() error {
			defer release()
			current, err := f.Show()
			if err != nil {
				return err
			}
			x = current.Executions[id]
			if x.Finished && x.Source != nil && x.Source.Status != "RUNNING" {
				return nil
			}
			receipt.Stopped = true
			if err = writeJSONAtomic(filepath.Join(job, "process.json"), receipt); err != nil {
				return err
			}
			if x.Identity == "" {
				_, err = f.failedCodexStart(id, x.Claim, fmt.Errorf("user authorized abort; owned native process group stopped before any thread identity was observed"))
			} else {
				_, err = f.finishCodex(id, x.Claim, x.Identity, job, fmt.Errorf("user authorized abort"))
			}
			return err
		}()
		if collectErr != nil {
			return Run{}, collectErr
		}
	}
	run, err = f.Show()
	if err != nil {
		return Run{}, err
	}
	if run.Status == "COMPLETE" {
		if err = f.finalizeTerminal(run); err != nil {
			return Run{}, err
		}
	}
	return run, nil
}

func codexResumeJob(job string, count int) string {
	if count <= 1 {
		return filepath.Join(job, "resume")
	}
	return filepath.Join(job, fmt.Sprintf("resume-%d", count))
}

// Load fixed instructions from the admitted package once. Like gate instructions,
// they are retained in the run so a resume never picks up edits to package files.
func loadActionInstructions(root string) (map[string]string, error) {
	paths := []string{
		"actions/product-review.md", "actions/start-readiness.md",
		"actions/development-worker.md", "actions/qa-design.md",
		"actions/qa-review.md", "actions/qa-execution.md",
	}
	result := make(map[string]string, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(filepath.Join(root, "prompts", filepath.FromSlash(path)))
		if err != nil {
			return nil, fmt.Errorf("action prompt %s: %w", path, err)
		}
		if !utf8.Valid(data) || strings.TrimSpace(string(data)) == "" {
			return nil, fmt.Errorf("action prompt %s: expected nonempty UTF-8 Markdown", path)
		}
		result[path] = string(data)
	}
	return result, nil
}

func actionInstructions(run Run, paths ...string) (string, error) {
	parts := make([]string, 0, len(paths))
	for _, selector := range paths {
		path, section, selected := strings.Cut(selector, "#")
		if run.Regular == nil || strings.TrimSpace(run.Regular.ActionInstructions[path]) == "" {
			return "", fmt.Errorf("PROMPT_CONFIG_MISSING: run has no bound prompts/%s", path)
		}
		part := run.Regular.ActionInstructions[path]
		if selected {
			_, body, found := strings.Cut("\n"+part, "\n# "+section+"\n")
			body, _, _ = strings.Cut(body, "\n# ")
			if !found || strings.TrimSpace(body) == "" {
				return "", fmt.Errorf("PROMPT_CONFIG_MISSING: run has no bound section prompts/%s", selector)
			}
			part = "# " + section + "\n" + strings.TrimRight(body, "\n") + "\n"
		}
		parts = append(parts, part)
	}
	return strings.Join(parts, "\n\n"), nil
}

func assemblePrompt(instructions string, input any) (string, error) {
	data, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return "", err
	}
	return instructions + "\n\n" + string(data), nil
}
