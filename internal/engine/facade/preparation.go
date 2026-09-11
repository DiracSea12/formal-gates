package facade

import (
	"encoding/json"
	"errors"
	"fmt"
	"formal-gates/internal/coordination"
	"formal-gates/internal/engine/persistence"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
)

// drivePreparation executes only the operation already persisted by Drive. The
// engine owns ordering and binding; this adapter owns the native side effects.
// A claimed operation can reconcile its registered resource or commit trailer;
// it cannot silently substitute a new operation after a process interruption.
func (f *Facade) drivePreparation(run Run) (Run, error) {
	p := run.Regular.Preparation
	if p == nil || p.Operation == nil {
		return run, nil
	}
	op := *p.Operation
	unlock, err := coordination.AcquirePath(filepath.Join(f.stateDir, "git-operations", "adapter.lock"), "Git preparation")
	if err != nil {
		return Run{}, err
	}
	defer unlock()
	current, err := f.Show()
	if err != nil {
		return Run{}, err
	}
	if current.Regular.Preparation == nil || current.Regular.Preparation.Operation == nil || current.Regular.Preparation.Operation.ID != op.ID {
		return current, nil
	}
	run = current
	p = run.Regular.Preparation
	op = *p.Operation
	if run.Regular.Terminal != "" || run.Regular.ChangePending || run.Regular.Ask != nil {
		return Run{}, fmt.Errorf("EVENT_NOT_CURRENT: Git preparation paused by control")
	}
	if !op.Claimed {
		ev, _ := protocol.NewRegularEvent(protocol.EventID("claim:"+op.ID), protocol.RegularEvent{Operation: "GIT_CLAIM", ExpectedRevision: run.Revision, GitClaim: &op})
		_, next, err := f.Submit(ev)
		if err != nil {
			return Run{}, err
		}
		run = next
		p = run.Regular.Preparation
		op = *p.Operation
	}
	receiptPath := filepath.Join(f.stateDir, "git-operations", strings.TrimPrefix(op.ID, "git:sha256:")+".json")
	var receipt protocol.GitPreparationReceipt
	if data, err := os.ReadFile(receiptPath); err == nil {
		if err = json.Unmarshal(data, &receipt); err != nil {
			return Run{}, err
		}
		if gitprep.Digest(receipt.Operation) != gitprep.Digest(op) {
			return Run{}, fmt.Errorf("UNKNOWN: Git operation receipt differs")
		}
	} else if !os.IsNotExist(err) {
		return Run{}, err
	} else {
		var err error
		receipt, err = f.performPreparationGit(run, op)
		if err != nil {
			return Run{}, err
		}
		if f.fault != nil {
			if err = f.fault(persistence.FaultExecuteAfter); err != nil {
				return Run{}, err
			}
		}
		if err = os.MkdirAll(filepath.Dir(receiptPath), 0700); err != nil {
			return Run{}, err
		}
		if err = writeJSONAtomic(receiptPath, receipt); err != nil {
			return Run{}, err
		}
	}
	current, err = f.Show()
	if err != nil {
		return Run{}, err
	}
	ev, _ := protocol.NewRegularEvent(protocol.EventID("receipt:"+op.ID), protocol.RegularEvent{Operation: "GIT_RECEIPT", ExpectedRevision: current.Revision, GitReceipt: &receipt})
	_, run, err = f.Submit(ev)
	return run, err
}
func (f *Facade) performPreparationGit(run Run, op protocol.GitPreparation) (protocol.GitPreparationReceipt, error) {
	p := run.Regular.Preparation
	r := run.Regular
	receipt := protocol.GitPreparationReceipt{Operation: op, Resources: map[string]string{}}
	resourceRoot := filepath.Join(f.stateDir, "resources", strings.TrimPrefix(p.BindingDigest, "sha256:"))
	switch op.Kind {
	case "repair-workspace", "repair-commit", "promote":
		return f.performVerificationGit(run, op)
	case "workspaces":
		for _, mode := range []string{"development", "blackbox"} {
			if mode == "blackbox" && p.QA[mode] == nil {
				continue
			}
			base := op.Before
			if mode == "blackbox" {
				base = r.BaseSnapshot
			}
			path := filepath.Join(resourceRoot, mode)
			if mode == "development" && p.Resources[mode] != "" {
				path = p.Resources[mode]
			}
			if err := gitprep.Workspace(f.root, path, base); err != nil {
				return receipt, err
			}
			receipt.Resources[mode] = path
		}
	case "batch-commit":
		commit, err := gitprep.Commit(p.Resources["development"], op.Before, op.ID, op.Paths)
		if err != nil {
			return receipt, err
		}
		receipt.Git = commit
	case "whitebox-workspace":
		path := filepath.Join(resourceRoot, fmt.Sprintf("whitebox-%d", p.Rework))
		if err := gitprep.Workspace(f.root, path, op.Before); err != nil {
			return receipt, err
		}
		receipt.Resources["whitebox"] = path
	case "whitebox-commit":
		wb := p.Resources["whitebox"]
		before, err := gitprep.Snapshot(f.root, p.DevelopmentSnapshot)
		for _, commit := range p.Commits {
			if commit.After == p.DevelopmentSnapshot {
				before = commit.View
			}
		}
		if err != nil {
			return receipt, err
		}
		oldOwners, oldErr := gitprep.TestOwnershipResolver(p.Resources["development"])
		newOwners, newErr := gitprep.TestOwnershipResolver(wb)
		ownership := gitprep.Ownership{Status: "UNKNOWN"}
		head, err := gitprep.Head(wb)
		if err != nil {
			return receipt, err
		}
		if oldErr == nil && newErr == nil {
			var after gitprep.View
			if head == op.Before {
				if err = gitprep.CheckManifest(wb, op.Before, op.Paths); err != nil {
					return receipt, err
				}
				after, err = gitprep.WorkingView(wb, op.Before)
			} else {
				after, err = gitprep.Snapshot(wb, head)
			}
			if err != nil {
				return receipt, err
			}
			ownership, err = gitprep.TestOnly(before, after, oldOwners, newOwners)
			if err != nil {
				return receipt, err
			}
		} else {
			ownership.Reason = fmt.Sprintf("old discovery=%v; new discovery=%v", oldErr, newErr)
		}
		receipt.Ownership = &ownership
		if ownership.Status != "TEST_ONLY" {
			receipt.Git = gitprep.Receipt{OperationID: op.ID, Before: op.Before, After: op.Before, View: before}
			return receipt, nil
		}
		commit, err := gitprep.Commit(wb, op.Before, op.ID, op.Paths)
		if err != nil {
			return receipt, err
		}
		receipt.Git = commit
	case "finalize":
		// Each finalize operation owns its workspace, so a QA revision cannot
		// reuse the previous candidate squashed against the run baseline.
		path := filepath.Join(resourceRoot, "candidate-"+strings.TrimPrefix(op.ID, "git:sha256:"))
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err = gitprep.Workspace(f.root, path, op.Before); err != nil {
				return receipt, err
			}
		}
		receipt.Resources["candidate"] = path
		paths := []string{r.Requirement.Path}
		receipt.Artifacts = map[string]string{r.Requirement.Path: r.Requirement.Revision}
		requirementTarget := filepath.Join(path, r.Requirement.Path)
		if err := os.MkdirAll(filepath.Dir(requirementTarget), 0755); err != nil {
			return receipt, err
		}
		if old, err := os.ReadFile(requirementTarget); err != nil || string(old) != r.Requirement.Text {
			if err = os.WriteFile(requirementTarget, []byte(r.Requirement.Text), 0644); err != nil {
				return receipt, err
			}
		}
		for _, mode := range r.Route.QA {
			artifact := protocol.PreparationArtifact(run.RunID, mode)
			paths = append(paths, artifact)
			data, err := json.MarshalIndent(p.QA[mode].Design, "", "  ")
			if err != nil {
				return receipt, err
			}
			data = append(data, '\n')
			receipt.Artifacts[artifact] = gitprep.Digest(p.QA[mode].Design)
			target := filepath.Join(path, artifact)
			if err = os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return receipt, err
			}
			if old, err := os.ReadFile(target); err != nil || string(old) != string(data) {
				if err = os.WriteFile(target, data, 0644); err != nil {
					return receipt, err
				}
			}
		}
		materialized := filepath.Join(f.stateDir, "git-operations", strings.TrimPrefix(op.ID, "git:sha256:")+"-materialized.json")
		if _, err := os.Stat(materialized); os.IsNotExist(err) {
			// Projects commonly ignore .gates runtime data. These exact frozen
			// case artifacts are delivery content, so explicitly track their
			// intent before scanning changes; unrelated ignored files stay out.
			if _, err := gitprep.Git(path, append([]string{"add", "-f", "-N", "--"}, paths...)...); err != nil {
				return receipt, err
			}
			changed, err := gitprep.Changes(path, op.Before)
			if err != nil {
				return receipt, err
			}
			want := map[string]bool{}
			for _, p := range paths {
				want[p] = true
			}
			for _, p := range changed {
				if !want[p] {
					return receipt, fmt.Errorf("unexpected candidate delivery path %s", p)
				}
			}
			commit, err := gitprep.Commit(path, op.Before, op.ID+":materialize", changed)
			if err != nil {
				return receipt, err
			}
			if err = os.MkdirAll(filepath.Dir(materialized), 0700); err != nil {
				return receipt, err
			}
			if err = writeJSONAtomic(materialized, commit); err != nil {
				return receipt, err
			}
		}
		commit, err := gitprep.Squash(path, r.Candidate, op.ID)
		if err != nil {
			return receipt, err
		}
		commit.Before = op.Before
		receipt.Git = commit
		// Test ownership is a gate scope, independent of who authored those files.
		if owners, err := gitprep.TestOwnershipResolver(path); err == nil {
			for _, entry := range commit.View.Entries {
				if owners[entry.Path] {
					receipt.TestOwned = append(receipt.TestOwned, entry.Path)
				}
			}
			sort.Strings(receipt.TestOwned)
		} else if p.QA["whitebox"] != nil {
			return receipt, err
		}
		for _, artifact := range paths {
			if _, err = gitprep.Git(path, "cat-file", "-e", commit.After+":"+artifact); err != nil {
				return receipt, fmt.Errorf("missing candidate artifact %s: %w", artifact, err)
			}
		}
	default:
		return receipt, fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: Git operation %q", op.Kind)
	}
	return receipt, nil
}

func preparationAction(step string) bool {
	return step == "development-worker" || step == "blackbox-design" || step == "blackbox-review" || step == "whitebox-design" || step == "whitebox-review"
}

// preparationPrompt is the engine-generated thin handoff. Development receives
// one complete Batch with ordered Subtasks, never another batch's live session.
// Blackbox receives only confirmed inputs and the baseline authoring resource.
func preparationPrompt(run Run, claim protocol.ExecutionClaim) (string, string, error) {
	p := run.Regular.Preparation
	if p == nil {
		return "", "", fmt.Errorf("no preparation binding")
	}
	step := string(claim.Task.Step)
	mode := strings.Split(step, "-")[0]
	workspace := p.Resources[mode]
	payload := map[string]any{"resultContract": map[string]any{"actionId": claim.ActionID, "attemptId": claim.AttemptID, "provider": claim.Provider, "identity": "NATIVE_IDENTITY", "requirementRevision": claim.RequirementRevision, "candidate": claim.Candidate, "status": "PASS or FAIL", "message": "concrete completion or gap", "manifest": []string{}, "validation": []protocol.BatchValidation{}, "handoff": "decision-only handoff", "design": "QADesign for authoring: {coverageJudgment, manifest, alternatives}", "review": "ReviewResult for review: {binding,scope:reviewScope,sourceDecisions,pointDecisions,caseDecisions,sharedStepDecisions}; each decision {id,status:PASS or FAIL}", "suggestions": []protocol.QASuggestion{}, "reuse": []protocol.TestReuse{}}, "binding": claim, "requirement": run.Regular.Requirement, "requiredSources": run.Regular.RequiredSources, "routeScope": gitprep.Digest(run.Regular.Route), "topologyScope": run.Regular.Topology}
	instructions := "Return a PreparationResult JSON object. Do not delegate or invoke Git write commands. The adapter owns all VCS writes. Use only this workspace and confirmed scope. Report actual checks, never fabricated evidence."
	if step == "development-worker" {
		workspace = p.Resources["development"]
		if previous := run.Executions[claim.ActionID].PreviousAction; previous != "" {
			payload["previousDevelopmentResult"] = run.Executions[previous].PreparationResult
		}
		instructions += " Keep this native identity until the entire Batch DoD passes. Return status CONTINUE for an unfinished normal turn, or FAIL for a failed self-test, with actual remaining work in message. Neither result ends the Batch or authorizes another developer. PASS requires every subtask complete and validated."
		for _, b := range run.Regular.Plan.Batches {
			if _, done := p.Commits[b.ID]; !done {
				payload["batch"] = b
				payload["reworkReason"] = p.ReworkReason
				break
			}
		}
		payload["committedHandoffs"] = map[string]string{}
		handoffs := payload["committedHandoffs"].(map[string]string)
		for id, result := range p.Batches {
			if _, done := p.Commits[id]; done {
				handoffs[id] = result.Handoff
			}
		}
		instructions += " Implement only the bound Batch, keep its Subtasks in this same session. Return exact changed/deleted/new file paths in manifest, one validation per ordered subtask with subtaskId/procedure/observation/result PASS, and a handoff containing only decisions or debt unavailable from committed files."
	} else if strings.HasSuffix(step, "-design") {
		payload["reviewKind"] = strings.ToUpper(mode)
		payload["previousDesign"] = p.QA[mode].PreviousDesign
		payload["reviewGaps"] = p.QA[mode].RetainedReview
		instructions += " Independently determine coverage first. Return design with coverageJudgment, manifest (binding, sources, points, cases, edges, sharedSteps) and alternatives. Cover the ACs assigned to this mode; the selected modes together must cover every AC. Each covered AC must be a point whose pointId is REVIEWKIND:AC-ID (for example BLACKBOX:AC-001), with a case edge from the requirement file source. Prefix case IDs with REVIEWKIND: too so modes never collide. Each case has its own oracle; shared preparation is referenced by ID."
		instructions += " Each case must declare command {executable,args,expectedExitCode} as exact argv for the public target or test. executable is only a program name or file path, without shell quoting or arguments: use executable \"go\" and args [\"test\",\"./...\"], never executable \"go test\". dependencies contains only case IDs declared earlier in this manifest, or [] when there are none; never put shared-step IDs or environment prerequisites in dependencies. Use sharedSteps for shared-step IDs; environment availability is not a case dependency. The runtime prepares the candidate copy and captures output; no shell interpolation occurs."
		if mode == "blackbox" {
			instructions += " Do not inspect any development workspace, implementation changes, whitebox work, or developer explanations. This workspace is the original baseline. Return the manifest in JSON without editing project files."
		} else {
			instructions += " Inspect and independently run existing tests before adding only missing coverage. Use the available native shell tool in this assigned whitebox authoring workspace for test discovery and trial execution, including running newly written tests before reporting reuse. The qa.prepare/qa.target/qa.collect operations are for later frozen-case execution or repair and are unavailable during authoring. These trial results are preparation evidence, not formal QA results. Edit only test-owned test/fixture files. Return all file paths in manifest, and reuse records for every case with caseId/testRef/coverageJudgment/procedure/observation/comparison/result EXECUTED_PASS or EXECUTED_FAIL. If a valid test reveals a production failure, return status FAIL with the actual failure evidence and a concrete message; do not change the test to force a pass. Production changes and unknown ownership return to development."
		}
	} else {
		payload["reviewScope"] = p.QA[mode].ReviewScope
		payload["design"] = p.QA[mode].Design
		payload["retainedReview"] = p.QA[mode].RetainedReview
		payload["existingTestEvidence"] = p.QA[mode].Reuse
		instructions += " Independently review every source, point, case and shared step without an unchanged retained approval. Review shared preparation once, and assess each changed case oracle separately. Return review with the exact manifest binding, the supplied reviewScope and individual PASS/FAIL decisions. FULL establishes initial approvals; AFFECTED freshly reviews changed items while the engine retains unchanged approvals. Each oracle remains independently checked. Check that command.executable is a single actual program name or path and command.args contains each argument separately; a shell command string in executable is a blocking case defect. Optional suggestions need stable id/severity P2 or P3/problem/cases/changesAcceptance; do not self-dispose suggestions. Real acceptance gaps are FAIL, not optional suggestions. Whitebox Review owns quality of ALL tests, including developer tests, and reads the complete candidate implementation and every case/test reference."
	}
	if workspace == "" {
		return "", "", fmt.Errorf("missing authorized workspace")
	}
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return "", "", err
	}
	return instructions + "\n\n" + string(data), workspace, nil
}

// Adapter facts may race with another authorized task's result. Only CAS/lock
// conflicts retry, using a fresh revision; task bindings and native calls remain
// unchanged. User decisions deliberately keep their original freshness contract.
func (f *Facade) submitAdapterRegular(id string, v protocol.RegularEvent) (protocol.Acceptance, Run, error) {
	var err error
	for attempt := 0; attempt < 32; attempt++ {
		run, e := f.Show()
		if e != nil {
			return protocol.Acceptance{}, Run{}, e
		}
		v.ExpectedRevision = run.Revision
		ev, e := protocol.NewRegularEvent(protocol.EventID(fmt.Sprintf("%s:revision-%d", id, run.Revision)), v)
		if e != nil {
			return protocol.Acceptance{}, Run{}, e
		}
		a, r, e := f.Submit(ev)
		if e == nil {
			return a, r, nil
		}
		err = e
		var conflict *persistence.RevisionConflictError
		var lock *persistence.LockHeldError
		var rejected *protocol.RejectedError
		if !errors.As(e, &conflict) && !errors.As(e, &lock) && !(errors.As(e, &rejected) && rejected.Code == protocol.CodeStaleFreshness) {
			return a, r, e
		}
		time.Sleep(5 * time.Millisecond)
	}
	return protocol.Acceptance{}, Run{}, err
}
func (f *Facade) cleanupPreparationResources(run Run) error {
	if run.Regular == nil || (run.Regular.Preparation == nil && len(run.Regular.PreparationHistory) == 0) {
		return nil
	}
	unlock, err := coordination.AcquirePath(filepath.Join(f.stateDir, "git-operations", "adapter.lock"), "Git cleanup")
	if err != nil {
		return err
	}
	defer unlock()
	all := append([]protocol.Preparation(nil), run.Regular.PreparationHistory...)
	if run.Regular.Preparation != nil {
		all = append(all, *run.Regular.Preparation)
	}
	for _, p := range all {
		paths := map[string]bool{}
		for _, path := range p.Resources {
			paths[path] = true
		}
		for _, receipt := range p.Receipts {
			for _, path := range receipt.Resources {
				paths[path] = true
			}
		}
		base := filepath.Join(f.stateDir, "resources", strings.TrimPrefix(p.BindingDigest, "sha256:"))
		if p.Operation != nil && p.Operation.Kind == "finalize" {
			paths[filepath.Join(base, "candidate-"+strings.TrimPrefix(p.Operation.ID, "git:sha256:"))] = true
		}
		for _, name := range []string{"development", "blackbox", "candidate", fmt.Sprintf("whitebox-%d", p.Rework)} {
			paths[filepath.Join(base, name)] = true
		}
		for path := range paths {
			if err = gitprep.Cleanup(f.root, path); err != nil {
				return err
			}
		}
	}
	return nil
}

func (f *Facade) verifyPreparationReceipt(event protocol.Event) error {
	if event.Regular != nil && event.Regular.PreparationResult != nil {
		result := event.Regular.PreparationResult
		run, err := f.Show()
		if err != nil {
			return err
		}
		x := run.Executions[result.ActionID]
		if x.Claim.Task.Step == "development-worker" && result.Status == "PASS" && !x.Finished {
			p := run.Regular.Preparation
			if err := gitprep.CheckManifest(p.Resources["development"], p.DevelopmentSnapshot, result.Manifest); err != nil {
				return err
			}
		}
	}
	if event.Regular == nil || event.Regular.GitReceipt == nil {
		return nil
	}
	receipt := event.Regular.GitReceipt
	path := filepath.Join(f.stateDir, "git-operations", strings.TrimPrefix(receipt.Operation.ID, "git:sha256:")+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("Git receipt requires the adapter's completed native operation: %w", err)
	}
	var actual protocol.GitPreparationReceipt
	if err = json.Unmarshal(data, &actual); err != nil {
		return err
	}
	if gitprep.Digest(actual) != gitprep.Digest(receipt) {
		return fmt.Errorf("INTENT_MISMATCH: submitted Git evidence differs from native receipt")
	}
	if receipt.Git.After != "" {
		view, err := gitprep.Snapshot(f.root, receipt.Git.After)
		if err != nil {
			return err
		}
		if view.Digest != receipt.Git.View.Digest {
			return fmt.Errorf("EXTERNAL_CHANGED: candidate validation view differs from native receipt")
		}
	}
	return nil
}

// Each actor gets a closed native output schema. A JSON-in-string wrapper would
// leave nested field types unconstrained and turn otherwise valid work into an
// avoidable runtime error, so these schemas use the same wire shapes as protocol.
func preparationSchema(step string, run Run, claim protocol.ExecutionClaim) map[string]any {
	str := map[string]any{"type": "string"}
	array := func(item any) map[string]any { return map[string]any{"type": "array", "items": item} }
	object := func(fields map[string]any) map[string]any {
		keys := []string{}
		for key := range fields {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		return map[string]any{"type": "object", "properties": fields, "required": keys, "additionalProperties": false}
	}
	fields := map[string]any{"actionId": str, "attemptId": str, "provider": str, "identity": str, "requirementRevision": str, "candidate": str, "status": map[string]any{"type": "string", "enum": []string{"PASS", "FAIL"}}, "message": str, "manifest": array(str)}

	exact := func(value string) map[string]any { return map[string]any{"type": "string", "enum": []string{value}} }
	fields["actionId"] = exact(claim.ActionID)
	fields["attemptId"] = exact(claim.AttemptID)
	fields["provider"] = exact(claim.Provider)
	fields["identity"] = exact("NATIVE_IDENTITY")
	fields["requirementRevision"] = exact(claim.RequirementRevision)
	fields["candidate"] = exact(claim.Candidate)
	if step != "development-worker" && step != "whitebox-design" {
		fields["manifest"] = map[string]any{"type": "array", "items": str, "maxItems": 0, "description": "No project files may be edited by this task. Always return an empty array. The QA coverage manifest belongs inside design.manifest."}
	} else {
		fields["manifest"] = map[string]any{"type": "array", "items": str, "description": "Only actual changed, new or deleted project-relative file path strings; never coverage source/point/case IDs."}
	}
	if step == "development-worker" {
		fields["status"] = map[string]any{"type": "string", "enum": []string{"PASS", "FAIL", "CONTINUE"}}
		fields["handoff"] = map[string]any{"type": "string", "minLength": 1, "description": "Only unresolved decisions/debt unavailable from committed files. If none, write No unresolved decisions."}
		fields["validation"] = array(object(map[string]any{"subtaskId": str, "procedure": str, "observation": str, "result": map[string]any{"type": "string", "enum": []string{"PASS", "FAIL"}}}))
		return object(fields)
	}
	mode := strings.Split(step, "-")[0]
	sourceBinding := run.Regular.RequiredSources.Binding
	binding := object(map[string]any{"runId": exact(sourceBinding.RunID), "childId": exact(sourceBinding.ChildID), "requirementRevision": exact(sourceBinding.RequirementRevision), "solutionRevision": exact(sourceBinding.SolutionRevision), "reviewKind": exact(strings.ToUpper(mode)), "routeScope": exact(gitprep.Digest(run.Regular.Route)), "topologyScope": exact(run.Regular.Topology)})
	if strings.HasSuffix(step, "-design") {
		caseFields := map[string]any{"command": object(map[string]any{"executable": map[string]any{"type": "string", "description": "One program name or path, without shell quotes or arguments; e.g. go, with test and ./... in args. Never a shell command string such as go test."}, "args": array(str), "expectedExitCode": map[string]any{"type": "integer"}}), "dependencies": array(str), "caseId": str, "mode": exact(strings.ToUpper(mode)), "oracle": str, "sharedSteps": array(str)}
		caseFields["dependencies"].(map[string]any)["description"] = "Only case IDs declared earlier in this manifest; use [] for an independent case. Shared-step IDs and environment prerequisites do not belong here."
		if mode == "blackbox" {
			caseFields["publicEntry"] = str
			caseFields["preconditions"] = str
			caseFields["steps"] = str
		} else {
			caseFields["testRef"] = str
		}
		manifest := object(map[string]any{"binding": binding, "sources": array(str), "points": array(object(map[string]any{"pointId": str, "observableBehavior": str, "oracle": str})), "cases": array(object(caseFields)), "edges": array(object(map[string]any{"sourceId": str, "pointId": str, "caseId": str})), "sharedSteps": array(object(map[string]any{"id": str, "procedure": str}))})
		alternatives := array(object(map[string]any{"sourceId": str, "reason": str, "method": str, "status": exact("PASS"), "evidence": str}))
		alternatives["description"] = "Only explicit NON_QA sources may have an alternative verification. Never use alternatives to defer or weaken an AC; return an empty array when every source is QA."
		fields["design"] = object(map[string]any{"coverageJudgment": str, "manifest": manifest, "alternatives": alternatives})
		if mode == "whitebox" {
			fields["reuse"] = array(object(map[string]any{"caseId": str, "testRef": str, "coverageJudgment": str, "procedure": str, "observation": str, "comparison": str, "result": map[string]any{"type": "string", "enum": []string{"EXECUTED_PASS", "EXECUTED_FAIL"}}}))
		}
	} else {
		decisions := array(object(map[string]any{"id": str, "status": map[string]any{"type": "string", "enum": []string{"PASS", "FAIL"}}}))
		fields["review"] = object(map[string]any{"binding": binding, "scope": exact(string(run.Regular.Preparation.QA[mode].ReviewScope)), "sourceDecisions": decisions, "pointDecisions": decisions, "caseDecisions": decisions, "sharedStepDecisions": decisions})
		fields["suggestions"] = array(object(map[string]any{"id": str, "severity": map[string]any{"type": "string", "enum": []string{"P2", "P3"}}, "problem": str, "cases": array(str), "changesAcceptance": map[string]any{"type": "boolean"}}))
	}
	return object(fields)
}
