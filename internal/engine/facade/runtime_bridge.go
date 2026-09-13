package facade

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"reflect"
)

func (f *Facade) runNativeBridgeTurns(run Run, claim protocol.ExecutionClaim, resume bool, job, prompt string, plan nativeActionInput, adapter nativeExecutionTransport) (string, error) {
	if plan.coverage != nil {
		coverageJob := filepath.Join(job, "coverage")
		if err := plan.coverage.writeFiles(coverageJob, claim); err != nil {
			return "", err
		}
		identity, err := f.callNativeTurn(run, claim, resume, coverageJob, plan.coverage.workspace, plan.coverage.prompt, plan.coverage.schema, plan.toolDescription, true, adapter)
		if err != nil {
			return identity, err
		}
		if err = f.recordNativeCoverage(claim, identity, coverageJob, adapter); err != nil {
			return identity, err
		}
		run, err = f.Show()
		if err != nil {
			return identity, err
		}
		claim.Identity = identity
		resume = true
	}
	return f.callNativeTurn(run, claim, resume, job, plan.material.workspace, prompt, plan.schema, plan.toolDescription, false, adapter)
}

// Coverage is a separate invocation directory. Platform-private files never
// need to be enumerated, renamed, or understood by the workflow owner.
func (f *Facade) recordNativeCoverage(claim protocol.ExecutionClaim, identity, job string, adapter nativeExecutionTransport) error {
	current, err := f.Show()
	if err != nil {
		return err
	}
	if protocol.WhiteboxCoverageReady(current.Executions[claim.ActionID]) {
		return nil
	}
	if _, err := adapter.source(identity, job, claim.Context); err != nil {
		return err
	}
	answer, err := os.ReadFile(filepath.Join(job, "answer.json"))
	if err != nil {
		return err
	}
	bound := current.Executions[claim.ActionID].Claim
	bound.Identity = identity
	return f.commitNativeCoverage(current, bound, answer)
}

func (f *Facade) commitNativeCoverage(current Run, bound protocol.ExecutionClaim, answer []byte) error {
	q := protocol.TaskOperationRequest{RunID: current.RunID, ExpectedRevision: current.Revision, Binding: bound, RequestID: "coverage-before-tests", Kind: "whitebox.coverage", Prompt: string(answer)}
	original, exists := current.Executions[bound.ActionID].Operations[protocol.TaskOperationID(q)]
	if exists {
		q.ExpectedRevision = original.Request.ExpectedRevision
	}
	_, err := f.ExecuteTaskOperation(q, exists)
	return err
}

// Every platform enters through the same live-claim check and bounded callbacks.
func (f *Facade) callNativeTurn(run Run, claim protocol.ExecutionClaim, resume bool, job, workspace, prompt string, schema map[string]any, description string, noTools bool, adapter nativeExecutionTransport) (string, error) {
	current, err := f.Show()
	if err != nil {
		return "", err
	}
	x, ok := current.Executions[claim.ActionID]
	if !ok || !x.Claimed || x.Finished || x.Claim.AttemptID != claim.AttemptID || x.Claim.Provider != claim.Provider || !reflect.DeepEqual(x.Claim.Context, claim.Context) || current.RunID != run.RunID || run.Regular == nil {
		return "", fmt.Errorf("INTENT_MISMATCH: native turn requires the claimed live attempt")
	}
	if resume && (claim.Identity == "" || x.Identity != claim.Identity) || !resume && x.Identity != "" {
		return "", fmt.Errorf("DEVELOPMENT_CONTINUITY: native turn must preserve the observed identity")
	}
	if err := validateNativeInput(adapter, prompt); err != nil {
		return "", err
	}
	if err := os.MkdirAll(job, 0700); err != nil {
		return "", err
	}
	if err := writeJSONAtomic(filepath.Join(job, "result-schema.json"), schema); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(job, "prompt.txt"), []byte(prompt), 0600); err != nil {
		return "", err
	}
	return adapter.turn(nativeTurn{Claim: claim, Settings: run.Regular.Settings.Values,
		Root: f.root, RunID: run.RunID, Job: job, Workspace: workspace, Prompt: prompt,
		ToolDescription: description, Schema: schema, Resume: resume, NoTools: noTools,
		Attached: func(id string) error { return f.attachStreamIdentity(claim, id) },
		Operation: func(identity, request string, args json.RawMessage) (any, error) {
			return f.dispatchNativeOperation(claim.ActionID, identity, request, args)
		}})
}
