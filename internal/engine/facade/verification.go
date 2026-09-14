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
	return step == "blackbox-execution" || step == "whitebox-execution" || step == "complexity-gate" || step == "implementation-quality-gate" || step == "repair"
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

func prepareVerificationPrompt(run Run, claim protocol.ExecutionClaim) (nativePromptMaterial, error) {
	w := verificationWave(run)
	if w == nil {
		return nativePromptMaterial{}, fmt.Errorf("no bound review wave")
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
	files := map[string][]byte{}
	payload := map[string]any{"binding": claim, "requirement": run.Regular.Requirement, "baseSnapshot": run.Regular.BaseSnapshot, "candidate": claim.Candidate}

	paths := []string{"actions/development-worker.md#通用执行约定"}
	if claim.Task.Step == "repair" {
		paths = append(paths, "actions/development-worker.md#开发工作者", "actions/development-worker.md#返修交付")
	} else if strings.HasSuffix(string(claim.Task.Step), "-execution") {
		path := "actions/qa-execution.md"
		section := "黑盒执行"
		if claim.Task.Step == "whitebox-execution" {
			section = "白盒执行"
		}
		paths = append(paths, path+"#QA 执行", path+"#"+section)
	}
	instructions, err := actionInstructions(run, paths...)
	if err != nil {
		return nativePromptMaterial{}, err
	}
	if claim.Task.Step == "repair" {
		repair := verificationRepair(run)
		if repair == nil {
			return nativePromptMaterial{}, fmt.Errorf("no repair plan")
		}
		workspace = run.Regular.Preparation.Resources[repair.ID]
		payload["repairPlan"] = repair
		if previous := run.Executions[claim.ActionID].RepairResult; previous != nil && previous.Status == "CONTINUE" {
			payload["continuation"] = map[string]any{"message": previous.Message, "manifest": previous.Manifest, "resolved": previous.Resolved}
		}
		if err = verificationRepairInputs(run, repair, payload, files); err != nil {
			return nativePromptMaterial{}, err
		}

	} else if strings.HasSuffix(string(claim.Task.Step), "-execution") {
		mode := strings.TrimSuffix(string(claim.Task.Step), "-execution")
		if err = verificationExecutionInputs(run, w, mode, payload, files); err != nil {
			return nativePromptMaterial{}, err
		}

	} else {
		// Gates see the original baseline-to-candidate implementation diff, excluding
		// independently owned tests and case artifacts. No QA body/result is handed
		// to the gate, even though all evidence belongs to the same immutable wave.
		args := []string{"diff", "--binary", run.Regular.BaseSnapshot, claim.Candidate, "--", "."}
		for _, path := range w.Candidate.TestOwned {
			args = append(args, ":(top,literal,exclude)"+path)
		}
		for path := range w.Candidate.Artifacts {
			if path != run.Regular.Requirement.Path {
				args = append(args, ":(exclude)"+path)
			}
		}
		sort.Strings(args[6:])
		payload["implementation"] = map[string]any{"workspace": workspace, "base": run.Regular.BaseSnapshot, "candidate": claim.Candidate, "diffArgs": args, "read": "Use native read-only Git/file tools in this bound workspace. Inspect the full base-to-candidate implementation diff on demand with diffArgs, preserving all exclusions; do not inspect independently owned tests or QA artifacts."}

		if strings.TrimSpace(run.Regular.ReviewerInstructions) == "" || strings.TrimSpace(run.Regular.CatalogGateInstructions[string(claim.Task.Step)]) == "" {
			return nativePromptMaterial{}, fmt.Errorf("PROMPT_CONFIG_MISSING: run has no bound reviewer or %s gate instructions", claim.Task.Step)
		}
		payload["reviewerInstructions"] = run.Regular.ReviewerInstructions
		payload["gateInstructions"] = run.Regular.CatalogGateInstructions[string(claim.Task.Step)]
		payload["reviewScope"] = "FULL"
	}
	if workspace == "" {
		return nativePromptMaterial{}, fmt.Errorf("missing registered verification workspace")
	}
	head, err := gitprep.Head(workspace)
	if err != nil {
		return nativePromptMaterial{}, err
	}
	if claim.Task.Step == "complexity-gate" || claim.Task.Step == "implementation-quality-gate" {
		if err := exec.Command("git", "-C", workspace, "cat-file", "-e", run.Regular.BaseSnapshot+"^{commit}").Run(); err != nil {
			return nativePromptMaterial{}, fmt.Errorf("verification baseline unavailable: %w", err)
		}
	}
	if head != claim.Candidate {
		return nativePromptMaterial{}, fmt.Errorf("EXTERNAL_CHANGED: authorized verification workspace identity differs")
	}
	return nativePromptMaterial{instructions: instructions, input: payload, workspace: workspace, files: files}, nil
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
	if claim.Task.Step == "repair" {
		fields["status"] = map[string]any{"type": "string", "enum": []string{"PASS", "CONTINUE"}}
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
	if claim.Task.Step == "repair" {
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
	if x.Claim.Task.Step != "repair" && !strings.HasSuffix(string(x.Claim.Task.Step), "-execution") {
		return false
	}
	w := verificationWave(run)
	if w == nil {
		return false
	}
	c, ok := w.Checkpoints[strings.TrimSuffix(string(x.Claim.Task.Step), "-execution")]
	return ok && c.ActionID == x.Claim.ActionID && c.BindingDigest == x.Claim.PrerequisiteDigest && !x.ResumeClaimed
}
