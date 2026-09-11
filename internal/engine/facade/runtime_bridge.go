package facade

import (
	"encoding/json"
	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
	"strings"
)

func (f *Facade) runNativeBridgeTurns(run Run, claim protocol.ExecutionClaim, resume bool, job, workspace, prompt string, schema map[string]any, turn func(Run, protocol.ExecutionClaim, bool, string, string, string, map[string]any, bool) (string, error)) (string, error) {
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
		identity := run.Executions[claim.ActionID].Identity
		if _, savedErr := os.Stat(filepath.Join(job, "coverage-answer.json")); savedErr != nil || identity == "" {
			identity, err = turn(run, claim, resume, job, job, coverageInstructions+"\n"+string(input), coverageSchema, true)
			if err != nil {
				return identity, err
			}
		}

		data, err := os.ReadFile(filepath.Join(job, "coverage-answer.json"))
		if err != nil {
			return identity, err
		}
		current, err := f.Show()
		if err != nil {
			return identity, err
		}
		bound := current.Executions[claim.ActionID].Claim
		bound.Identity = identity
		err = f.commitNativeCoverage(current, bound, data)
		if err != nil {
			return identity, err
		}
		if err = os.Rename(filepath.Join(job, "stdout.jsonl"), filepath.Join(job, "coverage-stdout.jsonl")); err != nil {
			return identity, err
		}
		run, err = f.Show()
		if err != nil {
			return identity, err
		}
		claim.Identity = identity
		resume = true
	}
	return turn(run, claim, resume, job, workspace, prompt, schema, false)
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
