package facade

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"reflect"
	"strings"
)

func (f *Facade) runNativeBridgeTurns(run Run, claim protocol.ExecutionClaim, resume bool, job, workspace, prompt string, schema map[string]any, adapter nativeExecutionTransport) (string, error) {
	if claim.Task.Step == "whitebox-design" && !protocol.WhiteboxCoverageReady(run.Executions[claim.ActionID]) {
		owners, err := gitprep.TestOwnershipResolver(workspace)
		if err != nil {
			return "", err
		}
		view, err := gitprep.Snapshot(workspace, claim.Candidate)
		if err != nil {
			return "", err
		}
		production := map[string]string{}
		for _, entry := range view.Entries {
			if !owners[entry.Path] && strings.HasSuffix(entry.Path, ".go") {
				content, err := os.ReadFile(filepath.Join(workspace, entry.Path))
				if err != nil {
					return "", err
				}
				production[entry.Path] = string(content)
			}
		}
		input, _ := json.Marshal(map[string]any{"requirement": run.Regular.Requirement, "production": production})
		str := map[string]any{"type": "string"}
		coverageSchema := map[string]any{"type": "object", "additionalProperties": false, "required": []string{"coverageJudgment", "behaviorOracles"}, "properties": map[string]any{"coverageJudgment": str, "behaviorOracles": map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"behavior", "oracle"}, "properties": map[string]any{"behavior": str, "oracle": str}}}}}
		coverageInstructions, err := actionInstructions(run, "actions/qa-design.md#白盒覆盖判断")
		if err != nil {
			return "", err
		}
		coverageJob := filepath.Join(job, "coverage")
		identity := run.Executions[claim.ActionID].Identity
		identity, err = f.callNativeTurn(run, claim, resume, coverageJob, coverageJob, coverageInstructions+"\n"+string(input), coverageSchema, true, adapter)
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
	return f.callNativeTurn(run, claim, resume, job, workspace, prompt, schema, false, adapter)
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
func (f *Facade) callNativeTurn(run Run, claim protocol.ExecutionClaim, resume bool, job, workspace, prompt string, schema map[string]any, noTools bool, adapter nativeExecutionTransport) (string, error) {
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
	description, err := actionInstructions(run, "actions/development-worker.md#工具说明")
	if err != nil {
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
