package facade

import (
	"fmt"
	"os"
	"reflect"

	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
)

// verifyExecutorHandoff captures the adapter capability and exact working view
// before the user preview, then rechecks both before the decision is committed.
func (f *Facade) verifyExecutorHandoff(event *protocol.Event) error {
	if event.Regular == nil {
		return nil
	}
	v := event.Regular
	if v.Operation != "REQUEST" && v.Operation != "DECIDE" {
		return nil
	}
	if v.Operation == "REQUEST" && (v.Request == nil || v.Request.Action != "executor-handoff") {
		return nil
	}
	run, err := f.Show()
	if err != nil {
		return err
	}
	var q *protocol.RegularRequest
	capture := false
	if v.Operation == "REQUEST" && v.Request != nil && v.Request.Action == "executor-handoff" {
		q, capture = v.Request, true
	} else if v.Operation == "DECIDE" && v.Decision != nil && v.Decision.Choice != "cancel" && run.Regular != nil && run.Regular.Ask != nil && run.Regular.Ask.Kind == "executor-handoff" {
		q = &run.Regular.Ask.Request
	}
	if q == nil {
		return nil
	}
	if q.Handoff == nil || run.Regular == nil || run.Regular.Preparation == nil {
		return fmt.Errorf("EXECUTOR_HANDOFF: executor and active workspace are required")
	}
	x, ok := run.Executions[q.Target]
	if !ok || x.Finished || x.Claim.Task.Step != "development-worker" {
		return fmt.Errorf("EXECUTOR_HANDOFF: unknown or completed developer action")
	}
	p := run.Regular.Preparation
	head, err := gitprep.Head(protocol.DevelopmentWorkspace(p, x.Claim.Task))
	if err != nil {
		return err
	}
	if head != x.Claim.Candidate {
		return fmt.Errorf("EXECUTOR_HANDOFF: reconcile development HEAD first")
	}
	view, err := gitprep.WorkingView(protocol.DevelopmentWorkspace(p, x.Claim.Task), head)
	if err != nil {
		return err
	}
	if !capture || q.Handoff.WorkspaceView != "" {
		if view.Digest != q.Handoff.WorkspaceView {
			return fmt.Errorf("STALE_FRESHNESS: handoff workspace changed")
		}
	}
	q.Handoff.WorkspaceView = view.Digest
	if q.Handoff.To.Kind == "native" {
		adapter, err := runtimeAdapter(q.Handoff.To.Provider)
		if err != nil {
			return fmt.Errorf("EXECUTOR_HANDOFF: target capability unavailable: %w", err)
		}
		binding, err := captureRuntimeContext(q.Handoff.To.Provider, run.Regular.Settings.Values, f.stateDir, f.root)
		if err == nil {
			err = adapter.validate(binding)
		}
		if err != nil {
			return fmt.Errorf("EXECUTOR_HANDOFF: target capability unavailable: %w", err)
		}
		if !capture || q.Handoff.To.Context != nil {
			if !reflect.DeepEqual(binding, q.Handoff.To.Context) {
				return fmt.Errorf("STALE_FRESHNESS: handoff target context changed")
			}
		}
		q.Handoff.To.Context = binding
	}
	return nil
}

type ExternalDevelopment struct {
	Binding   protocol.ExecutionClaim `json:"binding"`
	Executor  protocol.ExecutorTarget `json:"executor"`
	Workspace string                  `json:"workspace"`
	Input     map[string]any          `json:"input"`
	Schema    map[string]any          `json:"schema"`
}

// PrepareExternalDeveloper uses the ordinary claim/result writer without ever
// spawning a platform. Only an explicit user handoff can select this mode.
func (f *Facade) PrepareExternalDeveloper(actionID string) (ExternalDevelopment, error) {
	run, err := f.Show()
	if err != nil {
		return ExternalDevelopment{}, err
	}
	if run.Regular == nil || run.Regular.Ask != nil || run.Regular.ChangePending || run.Regular.Terminal != "" {
		return ExternalDevelopment{}, fmt.Errorf("EVENT_NOT_CURRENT: external developer is paused by the engine")
	}
	x, ok := run.Executions[actionID]
	if !ok || x.Finished || x.Executor == nil || x.Executor.Kind != "external" || x.Claim.Task.Step != "development-worker" {
		return ExternalDevelopment{}, fmt.Errorf("EXECUTOR_HANDOFF: action is not assigned to an external developer")
	}
	if !x.Claimed {
		_, run, err = f.submitAdapterRegular("external-claim:"+actionID, protocol.RegularEvent{Operation: "CLAIM", Claim: &x.Claim})
		if err != nil {
			return ExternalDevelopment{}, err
		}
		x = run.Executions[actionID]
	}
	claim := x.Claim
	claim.Identity = x.Identity
	payload, workspace, err := preparationInput(run, claim)
	if err != nil {
		return ExternalDevelopment{}, err
	}
	material := nativePromptMaterial{input: payload, workspace: workspace}
	job := f.codexJob(actionID)
	if err = material.bindFiles(job, run.Regular.Requirement, f.root); err != nil {
		return ExternalDevelopment{}, err
	}
	if err = os.MkdirAll(job, 0700); err != nil {
		return ExternalDevelopment{}, err
	}
	if err = material.writeFiles(job); err != nil {
		return ExternalDevelopment{}, err
	}
	payload = material.input
	schema := preparationSchema("development-worker", run, claim)
	properties := schema["properties"].(map[string]any)
	properties["identity"] = map[string]any{"type": "string", "enum": []string{x.Identity}}
	return ExternalDevelopment{Binding: claim, Executor: *x.Executor, Workspace: workspace, Input: payload, Schema: schema}, nil
}

func (f *Facade) verifyExternalDevelopmentResult(event protocol.Event) error {
	if event.Regular == nil || event.Regular.PreparationResult == nil {
		return nil
	}
	// A lost acknowledgement may be retried after the engine has committed or
	// cleaned the workspace. The existing event writer checks the original
	// payload digest and returns its receipt; it must not revalidate clean files
	// as though this were a new delivery. Conflicting replays still reach and
	// are rejected by that same writer, without a second receipt mechanism.
	snapshot, err := f.engine.Load()
	if err != nil {
		return err
	}
	if _, accepted := snapshot.State.Events[string(event.ID)]; accepted {
		return nil
	}
	result := event.Regular.PreparationResult
	run, err := f.Show()
	if err != nil {
		return err
	}
	x, ok := run.Executions[result.ActionID]
	if !ok || x.Executor == nil || x.Executor.Kind != "external" || result.Status != "PASS" {
		return nil
	}
	p := run.Regular.Preparation
	return gitprep.CheckManifest(protocol.DevelopmentWorkspace(p, x.Claim.Task), x.Claim.Candidate, result.Manifest)
}
