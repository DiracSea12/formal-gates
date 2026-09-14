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
	if protocol.ControlBlocksGit(run.Regular, op) {
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
		// A new development snapshot can require a new whitebox workspace
		// without changing the plan binding or rework count. The operation ID
		// already binds that input and QA revision; replay reuses its workspace,
		// while a later operation cannot collide with committed test artifacts.
		path := filepath.Join(resourceRoot, "whitebox-"+strings.TrimPrefix(op.ID, "git:sha256:"))
		if err := gitprep.Workspace(f.root, path, op.Before); err != nil {
			return receipt, err
		}
		if q := p.QA["whitebox"]; q != nil && q.TestArtifactsFrom != "" {
			if err := gitprep.RestoreTestArtifacts(path, q.TestArtifactsBase, q.TestArtifactsFrom, q.Manifest); err != nil {
				return receipt, err
			}
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
		head, err := gitprep.Head(wb)
		if err != nil {
			return receipt, err
		}
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
		// This is a review snapshot in the isolated whitebox workspace, not
		// promotion. Only the independent review can classify nonempty changes.
		if q := p.QA["whitebox"]; q != nil && q.Design != nil {
			// Resolve exact files from the existing Git views, including deleted
			// tests. Directories cannot exclude a whole production subtree.
			files := map[string]bool{}
			for _, view := range []gitprep.View{before, after} {
				for _, entry := range view.Entries {
					if entry.Type == "blob" {
						files[entry.Path] = true
					}
				}
			}
			for _, name := range q.Design.TestFiles {
				if !files[name] {
					return receipt, fmt.Errorf("whitebox testFiles path is not a project file: %s", name)
				}
			}
		}
		ownership, err := gitprep.TestOnly(before, after, nil, nil)
		if err != nil {
			return receipt, err
		}
		if ownership.Status != "TEST_ONLY" {
			ownership = gitprep.Ownership{Status: "REVIEW_PENDING", Reason: "independent whitebox review must inspect the complete development-to-whitebox diff"}
		}
		receipt.Ownership = &ownership
		commit, err := gitprep.Commit(wb, op.Before, op.ID, op.Paths)
		if err != nil {
			return receipt, err
		}
		diff, err := gitprep.Git(wb, "diff", "--name-only", "--no-renames", "-z", p.DevelopmentSnapshot, commit.After, "--")
		if err != nil {
			return receipt, err
		}
		receipt.Ownership.Paths = []string{}
		for _, name := range strings.Split(diff, "\x00") {
			if name != "" {
				receipt.Ownership.Paths = append(receipt.Ownership.Paths, name)
			}
		}
		receipt.Git = commit
	case "finalize":
		if !protocol.WhiteboxPromotionReady(p) {
			return receipt, fmt.Errorf("WHITEBOX_REVIEW_REQUIRED: whitebox testFiles and changes need independent test-only review before candidate promotion")
		}
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
		// Independent review approves test-only edits separately from whole-file
		// ownership. A mixed production/test file may have valid whitebox edits
		// but must remain in the implementation diff. Only declared, reviewed
		// pure test files (including reused developer tests) can be excluded.
		if q := p.QA["whitebox"]; q != nil {
			receipt.TestOwned = append([]string{}, q.Design.TestFiles...)
			sort.Strings(receipt.TestOwned)
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

func preparePreparationPrompt(run Run, claim protocol.ExecutionClaim) (nativePromptMaterial, error) {
	payload, workspace, err := preparationInput(run, claim)
	if err != nil {
		return nativePromptMaterial{}, err
	}
	step := string(claim.Task.Step)
	action := step
	if strings.HasSuffix(step, "-design") {
		action = "qa-design"
	} else if strings.HasSuffix(step, "-review") {
		action = "qa-review"
	}
	path := "actions/" + action + ".md"
	if action == "development-worker" {
		path += "#开发工作者"
	} else if action == "qa-design" {
		path += "#QA 设计"
	} else if action == "qa-review" {
		path += "#QA 审查"
	}
	paths := []string{"actions/development-worker.md#通用执行约定", path}
	if action == "development-worker" {
		paths = append(paths, "actions/development-worker.md#Batch 交付")
	}
	if action == "qa-design" {
		section := "黑盒设计"
		if step == "whitebox-design" {
			section = "白盒设计"
		}
		paths = append(paths, "actions/qa-design.md#"+section)
	}

	if step == "whitebox-review" {
		paths = append(paths, "actions/qa-review.md#白盒改动与最终候选")
	}
	if step == "blackbox-review" {
		paths = append(paths, "actions/qa-review.md#黑盒审查")
	}
	instructions, err := actionInstructions(run, paths...)
	if err != nil {
		return nativePromptMaterial{}, err
	}
	return nativePromptMaterial{instructions: instructions, input: payload, workspace: workspace}, nil
}

// The structured task is shared by native model prompts and explicitly assigned
// external developers. Native prompt configuration is not an external capability.
func preparationInput(run Run, claim protocol.ExecutionClaim) (map[string]any, string, error) {
	p := run.Regular.Preparation
	if p == nil {
		return nil, "", fmt.Errorf("no preparation binding")
	}
	step := string(claim.Task.Step)
	mode := strings.Split(step, "-")[0]
	workspace := p.Resources[mode]
	document := run.Regular.Requirement
	payload := map[string]any{"binding": claim, "requirement": map[string]any{"path": document.Path, "revision": document.Revision, "text": document.Text, "confirmed": document.Confirmed}, "requiredSources": run.Regular.RequiredSources, "routeScope": gitprep.Digest(run.Regular.Route), "topologyScope": run.Regular.Topology}
	if step == "development-worker" {
		workspace = p.Resources["development"]
		increment, incrementBatch, err := developmentIncrementInput(run, claim)
		if err != nil {
			return nil, "", err
		}
		if increment != nil {
			payload["developmentIncrement"] = increment
		}
		if previous := run.Executions[claim.ActionID].PreviousAction; previous != "" {
			payload["previousDevelopmentResult"] = (*protocol.PreparationResult)(nil)
			// Handoff can retire an unclaimed continuation without a result of its
			// own. Follow its existing lineage to the last actual checkpoint; keep
			// that result's original provider/identity rather than rebinding it.
			for previous != "" {
				prior, ok := run.Executions[previous]
				if !ok || prior.Claim.Task != claim.Task {
					break
				}
				if prior.PreparationResult != nil {
					payload["previousDevelopmentResult"] = prior.PreparationResult
					break
				}
				previous = prior.PreviousAction
			}
		}
		for _, b := range run.Regular.Plan.Batches {
			if incrementBatch != "" && b.ID != incrementBatch {
				continue
			}
			if _, done := p.Commits[b.ID]; !done || incrementBatch != "" {
				payload["batch"] = b
				payload["reworkReason"] = p.ReworkReason
				break
			}
		}
		payload["committedHandoffs"] = map[string]string{}
		handoffs := payload["committedHandoffs"].(map[string]string)
		if batch, ok := payload["batch"].(protocol.DevelopmentBatch); ok {
			for _, id := range batch.Dependencies {
				if _, done := p.Commits[id]; done {
					handoffs[id] = p.Batches[id].Handoff
				}
			}
		}
	} else if strings.HasSuffix(step, "-design") {
		payload["reviewKind"] = strings.ToUpper(mode)
		payload["previousDesign"] = p.QA[mode].PreviousDesign
		payload["reviewGaps"] = p.QA[mode].RetainedReview
		payload["existingTestEvidence"] = p.QA[mode].Reuse
	} else {
		payload["reviewScope"] = p.QA[mode].ReviewScope
		payload["design"] = p.QA[mode].Design
		payload["retainedReview"] = p.QA[mode].RetainedReview
		payload["existingTestEvidence"] = p.QA[mode].Reuse
		if mode == "whitebox" {
			payload["testChanges"] = map[string]any{
				"base": p.DevelopmentSnapshot, "candidate": claim.Candidate,
				"diffArgs": []string{"diff", "--binary", "--no-renames", p.DevelopmentSnapshot, claim.Candidate, "--"},
			}
		}
	}
	if qa := p.QA[mode]; qa != nil && qa.DesignGap != "" {
		payload["designGap"] = qa.DesignGap
	}
	if workspace == "" {
		return nil, "", fmt.Errorf("missing authorized workspace")
	}
	preparationRetryFeedback(run, claim, payload)
	return payload, workspace, nil
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

	if err := f.cleanupQAResources(run); err != nil {
		return err
	}
	isolated := map[string]bool{}
	for _, execution := range run.Executions {
		for _, op := range execution.Operations {
			if op.WorkspaceMode == "isolated" && op.Receipt != nil && op.Receipt.Workspace != "" {
				isolated[op.Receipt.Workspace] = true
			}
		}
	}
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
		if p.Operation != nil && p.Operation.Kind == "whitebox-workspace" {
			// The worktree may exist before its resource receipt is persisted.
			paths[filepath.Join(base, "whitebox-"+strings.TrimPrefix(p.Operation.ID, "git:sha256:"))] = true
		}
		for _, name := range []string{"development", "blackbox", "candidate"} {
			paths[filepath.Join(base, name)] = true
		}
		for path := range paths {
			if isolated[path] {
				if err = os.RemoveAll(path); err != nil {
					return err
				}
				continue
			}
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
		fields["status"] = map[string]any{"type": "string", "enum": []string{"PASS", "CONTINUE"}}
		fields["handoff"] = map[string]any{"type": "string", "minLength": 1, "description": "Only unresolved decisions/debt unavailable from committed files. If none, write No unresolved decisions."}
		fields["validation"] = array(object(map[string]any{"subtaskId": str, "procedure": str, "observation": str, "result": map[string]any{"type": "string", "enum": []string{"PASS", "FAIL"}}}))
		return object(fields)
	}
	mode := strings.Split(step, "-")[0]
	sourceBinding := run.Regular.RequiredSources.Binding
	binding := object(map[string]any{"runId": exact(sourceBinding.RunID), "childId": exact(sourceBinding.ChildID), "requirementRevision": exact(sourceBinding.RequirementRevision), "solutionRevision": exact(sourceBinding.SolutionRevision), "reviewKind": exact(strings.ToUpper(mode)), "routeScope": exact(gitprep.Digest(run.Regular.Route)), "topologyScope": exact(run.Regular.Topology)})
	if strings.HasSuffix(step, "-design") {
		fields["status"] = exact("DRAFT")
		if mode == "whitebox" {
			fields["status"] = map[string]any{"type": "string", "enum": []string{"DRAFT", "FAIL"}}
		}
		caseFields := map[string]any{"workspaceMode": map[string]any{"type": "string", "enum": []string{"candidate", "isolated"}}, "command": object(map[string]any{"executable": map[string]any{"type": "string", "description": "One program name or path, without shell quotes or arguments; e.g. go, with test and ./... in args. Never a shell command string such as go test."}, "args": array(str), "expectedExitCode": map[string]any{"type": "integer"}}), "dependencies": array(str), "caseId": str, "mode": exact(strings.ToUpper(mode)), "oracle": str, "sharedSteps": array(str)}
		caseFields["command"] = map[string]any{"anyOf": []any{caseFields["command"], map[string]any{"type": "null"}}, "description": "Optional existing executable procedure. Use null when the executor will implement the approved steps; do not write a driver script during design."}
		caseFields["dependencies"].(map[string]any)["description"] = "Only case IDs from the retained complete set or earlier in this increment; use [] for an independent case. Shared-step IDs and environment prerequisites do not belong here."
		if mode == "blackbox" {
			caseFields["publicEntry"] = str
			caseFields["preconditions"] = str
			caseFields["steps"] = str
		} else {
			caseFields["testRef"] = str
		}
		manifest := object(map[string]any{"binding": binding, "sources": array(str), "points": array(object(map[string]any{"pointId": str, "observableBehavior": str, "oracle": str})), "cases": array(object(caseFields)), "edges": array(object(map[string]any{"sourceId": str, "pointId": str, "caseId": str})), "sharedSteps": array(object(map[string]any{"id": str, "procedure": str, "artifacts": array(str), "command": caseFields["command"], "independent": map[string]any{"type": "boolean"}}))})
		alternatives := array(object(map[string]any{"sourceId": str, "reason": str, "method": str, "status": exact("PASS"), "evidence": str}))
		alternatives["description"] = "Only explicit NON_QA sources may have an alternative verification. Never use alternatives to defer or weaken an AC; return an empty array when every source is QA."
		designFields := map[string]any{"replace": map[string]any{"type": "boolean"}, "removeCases": array(str), "removeSharedSteps": array(str), "manifest": manifest, "alternatives": alternatives}
		if mode == "whitebox" {
			designFields["testFiles"] = map[string]any{"type": "array", "items": str, "description": "Exact project-relative test-only file paths, including unchanged reused developer tests and test-only fixtures. Never directories, globs, production/shared files or paths inferred from testRef. Include deleted test paths; return [] explicitly if no project files are test-only. Independent whitebox review must approve these files before gate exclusion. The top-level manifest lists only actual file changes."}
		}
		fields["design"] = object(designFields)
		if mode == "whitebox" {
			fields["reuse"] = array(object(map[string]any{"caseId": str, "testRef": str, "coverageJudgment": str, "procedure": str, "observation": str, "comparison": str, "result": map[string]any{"type": "string", "enum": []string{"EXECUTED_PASS", "EXECUTED_FAIL"}}}))
		}
	} else {
		decisions := array(object(map[string]any{"id": str, "status": map[string]any{"type": "string", "enum": []string{"PASS", "FAIL"}}}))
		fields["review"] = object(map[string]any{"setStatus": map[string]any{"type": "string", "enum": []string{"PASS", "FAIL"}}, "binding": binding, "scope": exact(string(run.Regular.Preparation.QA[mode].ReviewScope)), "sourceDecisions": decisions, "pointDecisions": decisions, "caseDecisions": decisions, "sharedStepDecisions": decisions})
		fields["suggestions"] = array(object(map[string]any{"id": str, "severity": map[string]any{"type": "string", "enum": []string{"P2", "P3"}}, "problem": str, "cases": array(str), "changesAcceptance": map[string]any{"type": "boolean"}}))
	}
	return object(fields)
}
