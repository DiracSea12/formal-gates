package facade

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

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
	material, err := prepareReviewPrompt(run, claim)
	if err != nil {
		return "", err
	}
	return material.render(claim)
}

func prepareReviewPrompt(run Run, claim protocol.ExecutionClaim) (nativePromptMaterial, error) {
	instructions, err := actionInstructions(run, "actions/development-worker.md#通用执行约定", "actions/"+string(claim.Task.Step)+".md")
	if err != nil {
		return nativePromptMaterial{}, err
	}
	input := map[string]any{"binding": claim, "requirement": run.Regular.Requirement}
	if claim.Task.Step == "start-readiness" {
		for i := len(run.Regular.Requests) - 1; i >= 0; i-- {
			request := run.Regular.Requests[i]
			decision, ok := run.reviewDecisions[request.ID]
			if !ok || decision.Choice != "confirm" {
				continue
			}
			if request.Request.Action == "apply-requirement" || request.Request.Action == "reset" {
				break
			}
			if request.Request.Action != "plan-change" {
				continue
			}
			input["confirmedPlanChange"] = request.Request
			if series := run.Regular.Reviews["start-readiness"]; series != nil {
				plans := []protocol.DevelopmentPlan{}
				for _, prior := range series.RawCandidates {
					if prior.RequirementRevision == claim.RequirementRevision && !prior.Quarantined && prior.GranularityReview != nil {
						plans = append(plans, *prior.GranularityReview)
					}
				}
				input["previousPlans"] = plans
			}
			break
		}
	}
	return nativePromptMaterial{instructions: instructions, input: input}, nil
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
	StartedAt   string `json:"startedAt,omitempty"`
	CompletedAt string `json:"completedAt,omitempty"`
	PID         int    `json:"pid"`
	ActionID    string `json:"actionId"`
	Stopped     bool   `json:"stopped"`
}

func (f *Facade) codexJob(actionID string) string {
	name := strings.ReplaceAll(strings.TrimPrefix(actionID, "act:"), "/", "_")
	if runtime.GOOS == "windows" {
		// Issued batch action IDs contain a colon before the batch ID.
		name = strings.ReplaceAll(name, ":", "_")
	}
	return filepath.Join(f.stateDir, "executions", name)
}

func (f *Facade) failedNativeStart(actionID string, claim protocol.ExecutionClaim, cause error) (Run, error) {
	src := protocol.ExecutionSource{ActionID: actionID, AttemptID: claim.AttemptID, Provider: claim.Provider, Identity: claim.Identity, Status: "UNAVAILABLE", Reason: "Native execution ended without an observed thread; source unavailable: " + cause.Error()}
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
	raw := protocol.RawReviewCandidate{ActionID: actionID, AttemptID: claim.AttemptID, Identity: claim.Identity, Action: string(claim.Task.Step), Provider: claim.Provider, RequirementRevision: claim.RequirementRevision, Candidate: claim.Candidate, Status: "RUNTIME_ERROR", Message: src.Reason}
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
		if err := f.finishNativeCancellation(id, "user authorized abort"); err != nil {
			return Run{}, err
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

// finishNativeCancellation settles an already authorized cancellation. It never
// claims, resumes or replaces a native task; abort and ordinary terminate intents
// share the same process evidence, action lock and result/source collection.
func (f *Facade) finishNativeCancellation(id, reason string) error {
	run, err := f.Show()
	if err != nil {
		return err
	}
	x, ok := run.Executions[id]
	if !ok || !x.Claimed {
		return fmt.Errorf("INTENT_MISMATCH: cancellation requires a claimed execution")
	}
	if x.Finished && x.Source != nil && x.Source.Status != "RUNNING" {
		return nil
	}
	job := f.codexJob(id)
	if x.ResumeClaimed || x.ResumeCount > 0 {
		job = codexResumeJob(job, x.ResumeCount)
	}
	job, err = nativeProcessDirectory(job, string(x.Claim.Task.Step))
	if err != nil {
		return fmt.Errorf("UNKNOWN: cancellation needs the native process receipt for %s: %w", id, err)
	}
	data, err := os.ReadFile(filepath.Join(job, "process.json"))
	if err != nil {
		return fmt.Errorf("UNKNOWN: cancellation needs the native process receipt for %s: %w", id, err)
	}
	var receipt codexProcess
	if err = json.Unmarshal(data, &receipt); err != nil || receipt.ActionID != id {
		return fmt.Errorf("INTENT_MISMATCH: cancellation process receipt")
	}
	if !receipt.Stopped && codexProcessAlive(receipt.PID) {
		if err = stopCodexProcess(receipt.PID); err != nil {
			return err
		}
		deadline := time.Now().Add(3 * time.Second)
		for codexProcessAlive(receipt.PID) && time.Now().Before(deadline) {
			time.Sleep(20 * time.Millisecond)
		}
		if codexProcessAlive(receipt.PID) {
			return fmt.Errorf("HOST_ACTION_RUNNING: native process group %d is still stopping", receipt.PID)
		}
	}
	// A still-running original driver owns result collection. Killing its
	// native process does not make it safe to submit a competing result.
	var release func()
	deadline := time.Now().Add(3 * time.Second)
	for {
		release, err = coordination.AcquirePath(f.codexJob(id)+".lock", "native cancellation collection")
		if err == nil || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		return err
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
		data, err := os.ReadFile(filepath.Join(job, "process.json"))
		if err != nil {
			return err
		}
		var preserved map[string]json.RawMessage
		if err = json.Unmarshal(data, &preserved); err != nil {
			return err
		}
		preserved["stopped"] = json.RawMessage("true")
		if err = writeJSONAtomic(filepath.Join(job, "process.json"), preserved); err != nil {
			return err
		}
		if x.Identity == "" {
			_, err = f.failedNativeStart(id, x.Claim, fmt.Errorf("%s; owned native process group stopped before any thread identity was observed", reason))
		} else {
			adapter, lookupErr := runtimeAdapter(x.Claim.Provider)
			if lookupErr != nil {
				return lookupErr
			}
			_, err = f.collectNativeResult(id, x.Claim, x.Identity, job, fmt.Errorf("%s", reason), adapter)
		}
		return err
	}()
	if collectErr != nil {
		return collectErr
	}
	return nil
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
	// The source document is authoritative; its derived projection repeats the
	// same requirements and acceptance text. Keep the complete document in the
	// actual message, including for roles that cannot read files.
	if payload, ok := input.(map[string]any); ok {
		copy := make(map[string]any, len(payload))
		for key, value := range payload {
			copy[key] = value
		}
		if binding, ok := copy["binding"].(protocol.ExecutionClaim); ok {
			// The adapter needs the captured environment to launch/resume;
			// the worker only needs the action and result binding.
			binding.Context = nil
			copy["binding"] = binding
		}
		if requirement, ok := copy["requirement"].(protocol.RequirementDocument); ok {
			copy["requirement"] = map[string]any{"path": requirement.Path, "revision": requirement.Revision, "text": requirement.Text, "confirmed": requirement.Confirmed}
		}
		input = copy
	}
	data, err := json.MarshalIndent(input, "", "  ")
	if err != nil {
		return "", err
	}
	return instructions + "\n\n" + string(data), nil
}
