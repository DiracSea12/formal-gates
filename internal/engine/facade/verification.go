package facade

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
)

func verificationAction(step string) bool {
	return step == "blackbox-execution" || step == "whitebox-execution" || step == "complexity-gate" || step == "implementation-quality-gate" || step == "repair-worker"
}
func verificationWave(run Run) *protocol.VerificationWave {
	if run.Regular.Verification == nil {
		return nil
	}
	for i := range run.Regular.Verification.Waves {
		w := &run.Regular.Verification.Waves[i]
		if w.ID == run.Regular.Verification.Current {
			return w
		}
	}
	return nil
}
func verificationRepair(run Run) *protocol.RepairPlan {
	w := verificationWave(run)
	if w == nil {
		return nil
	}
	for i := range run.Regular.Verification.Repairs {
		p := &run.Regular.Verification.Repairs[i]
		if p.WaveID == w.ID {
			return p
		}
	}
	return nil
}
func verificationPrompt(run Run, claim protocol.ExecutionClaim) (string, string, error) {
	w := verificationWave(run)
	if w == nil {
		return "", "", fmt.Errorf("no bound review wave")
	}
	workspace := run.Regular.Preparation.Resources["candidate"]
	for _, receipt := range run.Regular.Preparation.Receipts {
		if receipt.Git.After == claim.Candidate {
			for name, path := range receipt.Resources {
				if name == "candidate" || strings.HasPrefix(name, "repair:") {
					workspace = path
				}
			}
		}
	}
	payload := map[string]any{"binding": claim, "requirement": run.Regular.Requirement, "baseSnapshot": run.Regular.BaseSnapshot, "candidate": claim.Candidate}
	instructions := "Return direct JSON matching the supplied result schema. Set identity to NATIVE_IDENTITY. Never fabricate checks, delegate, mutate Git history or inspect another worker's evidence."
	if claim.Task.Step == "repair-worker" {
		repair := verificationRepair(run)
		if repair == nil {
			return "", "", fmt.Errorf("no repair plan")
		}
		workspace = run.Regular.Preparation.Resources[repair.ID]
		payload["repairPlan"] = repair
		payload["problems"] = run.Regular.Verification.Problems
		payload["sourceResults"] = w.Results
		payload["frozenQA"] = run.Regular.Preparation.QA
		instructions += " Implement only every canonical problem in the unique repairPlan. Leave frozen case artifacts unchanged; a change of QA acceptance needs explicit QA revision. Return all changed/new/deleted paths in manifest, all resolved problem IDs, actual successful build and full project tests (procedure/observation/result PASS), and independently execute every affected case with its actual output. Finish implementation edits before case execution. For each affected case, use formal_gates_operation in order qa.prepare, qa.target, qa.collect; prepare copies this registered repair workspace including uncommitted edits into an independent self-test copy. Report the collected output and workspace. These self-tests do not replace the new candidate QA wave. A failure is status FAIL and a concrete message, never a partial PASS."
	} else if strings.HasSuffix(string(claim.Task.Step), "-execution") {
		mode := strings.TrimSuffix(string(claim.Task.Step), "-execution")
		payload["design"] = run.Regular.Preparation.QA[mode].Design
		for _, t := range w.Targets {
			if t.Target == mode {
				payload["target"] = t
			}
		}
		payload["checkpoint"] = w.Checkpoints[mode]
		instructions += " Use formal_gates_operation for each target case in order qa.prepare, qa.target, qa.collect. The bridge executes only its approved command. Use the returned workspace and collected output in case evidence. Execute every supplied target.expected case using this exact candidate. Reuse non-acceptance build/preparation once and identify that reuse in each case; install/native calls that ARE the acceptance target must run for each case. Give independent cases clean directories and continue independent cases after a FAIL; only named dependent cases may be BLOCKED. Preserve complete checkpoint entries byte-for-byte when bindings match, run only remaining cases. If an actual tool/environment change invalidates a completed checkpoint case, first return CHECKPOINT with an invalidated item containing caseId/previousTool/previousEnvironment/tool/environment/reason and move the invalidated cases to remaining. Retain unaffected entries. The accepted checkpoint authorizes one new prepare/target/collect sequence for each changed case on resume; unchanged completed stages cannot run again. Never erase a FAIL without this exact changed-input receipt. Each case includes caseId/status PASS|FAIL|BLOCKED/procedure/output/comparison/environment/tool/workspace/dependencies/reusedPreparation. Each FAIL needs a raw finding with requirement equal to its caseId and concrete remedy. The final cases must cover exactly expected, remaining must be empty. If interrupted, return CHECKPOINT with all completed PASS/FAIL, remaining IDs and the actual interruption reason; no incomplete item is PASS."
	} else {
		// Gates see the original baseline-to-candidate implementation diff, excluding
		// independently owned tests and case artifacts. No QA body/result is handed
		// to the gate, even though all evidence belongs to the same immutable wave.
		args := []string{"diff", "--binary", run.Regular.BaseSnapshot, claim.Candidate, "--", "."}
		for _, path := range w.Candidate.TestOwned {
			args = append(args, ":(exclude)"+path)
		}
		for path := range w.Candidate.Artifacts {
			if path != run.Regular.Requirement.Path {
				args = append(args, ":(exclude)"+path)
			}
		}
		diff, err := exec.Command("git", append([]string{"-C", workspace}, args...)...).Output()
		if err != nil {
			return "", "", err
		}
		payload["fullImplementationDiff"] = string(diff)
		payload["reviewerInstructions"] = run.Regular.ReviewerInstructions
		payload["gateInstructions"] = run.Regular.CatalogGateInstructions[string(claim.Task.Step)]
		payload["reviewScope"] = "FULL"
		instructions += " Independently review the complete original-baseline-to-candidate implementation diff and complete requirement. Every finding needs its own ID, severity rationale, normal-use reproduction, consequence and remedy. P0/P1 makes FAIL; P2 requires repair with PASS; P3 is advisory. Do not read .gates/results or any QA workspace/results. Return empty cases and remaining arrays."
	}
	if workspace == "" {
		return "", "", fmt.Errorf("missing registered verification workspace")
	}
	head, err := gitprep.Head(workspace)
	if err != nil {
		return "", "", err
	}
	if head != claim.Candidate {
		return "", "", fmt.Errorf("EXTERNAL_CHANGED: authorized verification workspace identity differs")
	}
	bytes, err := json.MarshalIndent(payload, "", "  ")
	return instructions + "\n\n" + string(bytes), workspace, err
}
func verificationSchema(claim protocol.ExecutionClaim) map[string]any {
	str := map[string]any{"type": "string"}
	arr := func(item any) map[string]any { return map[string]any{"type": "array", "items": item} }
	obj := func(fields map[string]any) map[string]any {
		keys := []string{}
		for key := range fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return map[string]any{"type": "object", "properties": fields, "required": keys, "additionalProperties": false}
	}
	exact := func(value string) map[string]any { return map[string]any{"type": "string", "enum": []string{value}} }
	fields := map[string]any{"actionId": exact(claim.ActionID), "attemptId": exact(claim.AttemptID), "provider": exact(claim.Provider), "identity": exact("NATIVE_IDENTITY"), "requirementRevision": exact(claim.RequirementRevision), "candidate": exact(claim.Candidate), "status": map[string]any{"type": "string", "enum": []string{"PASS", "FAIL", "CHECKPOINT"}}, "message": str}
	oneCase := obj(map[string]any{"caseId": str, "status": map[string]any{"type": "string", "enum": []string{"PASS", "FAIL", "BLOCKED"}}, "procedure": str, "output": str, "comparison": str, "environment": str, "tool": str, "workspace": str, "dependencies": arr(str), "reusedPreparation": str})
	fields["cases"] = arr(oneCase)
	if claim.Task.Step == "repair-worker" {
		fields["status"] = map[string]any{"type": "string", "enum": []string{"PASS", "FAIL"}}
		fields["planId"] = exact(claim.PrerequisiteDigest)
		fields["manifest"] = arr(str)
		fields["resolved"] = arr(str)
		check := obj(map[string]any{"subtaskId": str, "procedure": str, "observation": str, "result": map[string]any{"type": "string", "enum": []string{"PASS", "FAIL"}}})
		fields["build"] = check
		fields["projectTests"] = check
	} else {
		fields["bindingDigest"] = exact(claim.PrerequisiteDigest)
		fields["remaining"] = arr(str)
		fields["invalidated"] = arr(obj(map[string]any{"caseId": str, "previousTool": str, "previousEnvironment": str, "tool": str, "environment": str, "reason": str}))
		fields["findings"] = arr(obj(map[string]any{"id": str, "severity": map[string]any{"type": "string", "enum": []string{"P0", "P1", "P2", "P3"}}, "category": str, "requirement": str, "problem": str, "normalPath": str, "consequence": str, "severityRationale": str, "remedy": str}))
	}
	return obj(fields)
}
func (f *Facade) finishVerification(claim protocol.ExecutionClaim, identity string, data []byte, readErr, waitErr error, source protocol.ExecutionSource) (Run, error) {
	v := protocol.RegularEvent{}
	if claim.Task.Step == "repair-worker" {
		var result protocol.RepairResult
		err := json.Unmarshal(data, &result)
		if readErr != nil || waitErr != nil || err != nil {
			result = protocol.RepairResult{ActionID: claim.ActionID, AttemptID: claim.AttemptID, Provider: claim.Provider, RequirementRevision: claim.RequirementRevision, Candidate: claim.Candidate, PlanID: claim.PrerequisiteDigest, Status: "RUNTIME_ERROR", Message: fmt.Sprintf("native=%v read=%v decode=%v", waitErr, readErr, err)}
		}
		result.Identity = identity
		v.Operation = "REPAIR_RESULT"
		v.RepairResult = &result
	} else {
		var result protocol.VerificationResult
		err := json.Unmarshal(data, &result)
		if readErr != nil || waitErr != nil || err != nil {
			result = protocol.VerificationResult{ActionID: claim.ActionID, AttemptID: claim.AttemptID, Provider: claim.Provider, RequirementRevision: claim.RequirementRevision, Candidate: claim.Candidate, BindingDigest: claim.PrerequisiteDigest, Status: "RUNTIME_ERROR", Message: fmt.Sprintf("native=%v read=%v decode=%v", waitErr, readErr, err)}
		}
		result.Identity = identity
		v.Operation = "VERIFICATION_RESULT"
		v.VerificationResult = &result
	}
	current, err := f.Show()
	if err != nil {
		return Run{}, err
	}
	if !current.Executions[claim.ActionID].Finished {
		if _, _, err = f.submitAdapterRegular("result:"+claim.ActionID+":"+gitprep.Digest(data), v); err != nil {
			return Run{}, err
		}
	}
	if err = f.submitSource(source); err != nil {
		return Run{}, err
	}
	return f.Show()
}
func (f *Facade) performVerificationGit(run Run, op protocol.GitPreparation) (protocol.GitPreparationReceipt, error) {
	p := run.Regular.Preparation
	receipt := protocol.GitPreparationReceipt{Operation: op, Resources: map[string]string{}}
	switch op.Kind {
	case "repair-workspace":
		path := filepath.Join(f.stateDir, "resources", strings.TrimPrefix(p.BindingDigest, "sha256:"), strings.ReplaceAll(op.Target, ":", "-"))
		if err := gitprep.Workspace(f.root, path, op.Before); err != nil {
			return receipt, err
		}
		receipt.Resources[op.Target] = path
	case "repair-commit":
		path := p.Resources[op.Target]
		commit, err := gitprep.Commit(path, op.Before, op.ID, op.Paths)
		if err != nil {
			return receipt, err
		}
		receipt.Git = commit
		receipt.Resources["candidate"] = path
	case "promote":
		source := p.Candidate.Candidate.Identity
		promotion, err := gitprep.Promote(f.root, source, op.Target, op.ID)
		if err != nil {
			return receipt, err
		}
		receipt.Promotion = &promotion
		receipt.Git = gitprep.Receipt{OperationID: op.ID, Before: op.Before, After: promotion.Target.Identity, View: promotion.Target}
	default:
		return receipt, fmt.Errorf("unknown verification Git operation")
	}
	return receipt, nil
}
func checkpointCanResume(run Run, x protocol.RegularExecution) bool {
	if !strings.HasSuffix(string(x.Claim.Task.Step), "-execution") {
		return false
	}
	w := verificationWave(run)
	if w == nil {
		return false
	}
	c, ok := w.Checkpoints[strings.TrimSuffix(string(x.Claim.Task.Step), "-execution")]
	return ok && c.ActionID == x.Claim.ActionID && c.BindingDigest == x.Claim.PrerequisiteDigest && !x.ResumeClaimed
}
