package protocol

import "formal-gates/internal/engine/runtime"

// controlTarget derives the pending decision's invalidation root from the same
// request semantics used by preparationDecision. An empty root is a run-wide
// barrier: requirement, workspace, route, stop and unknown controls stay paused.
// This is a projection, not persisted scheduling or new authorization.
func controlTarget(r *RegularState) string {
	if r == nil || r.ChangePending || r.Terminal != "" {
		return ""
	}
	if r.Ask == nil {
		return "none"
	}
	q := r.Ask.Request
	switch q.Action {
	case "qa-revise", "qa-suggestion", "qa-recovery":
		if r.Preparation != nil && r.Preparation.QA[q.Target] != nil {
			return q.Target
		}
	case "preparation-retry":
		return q.Target
	}
	return ""
}

// ControlPausesRun distinguishes controls with no independent work from a
// decision on one dependency branch. The Ask itself remains visible and bound.
func ControlPausesRun(r *RegularState) bool { return controlTarget(r) == "" }

func ControlBlocksTask(r *RegularState, task runtime.TaskKey) bool {
	root := controlTarget(r)
	if root == "" {
		return true
	}
	if root == "none" {
		return false
	}
	// Candidate verification consumes every approved preparation branch.
	if verificationStep(task.Step) {
		return true
	}
	if task.String() == root {
		return true
	}
	if r.Preparation != nil && r.Preparation.QA[root] != nil {
		return task.Step == qaTask(r.Preparation, root, "design").Step || task.Step == qaTask(r.Preparation, root, "review").Step
	}
	// Other ready tasks already satisfy their ordinary prerequisites. A retry
	// only authorizes another attempt of its exact target.
	return false
}

func ControlBlocksGit(r *RegularState, op GitPreparation) bool {
	root := controlTarget(r)
	if root == "" {
		return true
	}
	if root == "none" {
		return false
	}
	// Workspaces and batch commits consume the plan and development result;
	// candidate construction/promotion also consumes QA approval.
	switch op.Kind {
	case "workspaces", "development-workspace", "batch-commit":
		return ControlBlocksTask(r, batchTask(r.Preparation, op.Target))
	case "whitebox-workspace", "whitebox-commit":
		return ControlBlocksTask(r, qaTask(r.Preparation, "whitebox", "design"))
	default:
		return true
	}
}
