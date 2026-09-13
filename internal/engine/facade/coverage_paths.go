package facade

import (
	"bytes"
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
	"os"
	"path/filepath"
)

type nativeCoverageInput struct {
	prompt    string
	schema    map[string]any
	files     map[string][]byte
	workspace string
}

func prepareNativeCoverage(run Run, claim protocol.ExecutionClaim, workspace string, sources []SubagentSource, job string) (*nativeCoverageInput, error) {
	if claim.Task.Step != "whitebox-design" || protocol.WhiteboxCoverageReady(run.Executions[claim.ActionID]) {
		return nil, nil
	}
	paths, err := coverageSourcePaths(run, claim, workspace)
	if err != nil {
		return nil, err
	}
	result := &nativeCoverageInput{workspace: workspace, files: map[string][]byte{filepath.Join(job, "requirement.md"): []byte(run.Regular.Requirement.Text)}}
	contracts := []map[string]string{}
	for i, source := range sources {
		p := filepath.Join(job, fmt.Sprintf("public-source-%d.txt", i))
		result.files[p] = []byte(source.Content)
		contracts = append(contracts, map[string]string{"kind": source.Kind, "path": p})
	}
	input := map[string]any{"binding": map[string]string{"actionId": claim.ActionID, "attemptId": claim.AttemptID, "candidate": claim.Candidate, "requirementRevision": claim.RequirementRevision}, "sourcePaths": paths, "requirementPath": filepath.Join(job, "requirement.md"), "publicContracts": contracts}
	instructions, err := actionInstructions(run, "actions/qa-design.md#白盒覆盖判断")
	if err != nil {
		return nil, err
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	result.prompt = instructions + "\n" + string(encoded)
	str := map[string]any{"type": "string"}
	result.schema = map[string]any{"type": "object", "additionalProperties": false, "required": []string{"coverageJudgment", "behaviorOracles"}, "properties": map[string]any{"coverageJudgment": str, "behaviorOracles": map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "object", "additionalProperties": false, "required": []string{"behavior", "oracle"}, "properties": map[string]any{"behavior": str, "oracle": str}}}}}
	return result, nil
}

func (p *nativeCoverageInput) writeFiles(job string, claim protocol.ExecutionClaim) error {
	write := func(path string, data []byte) error {
		if old, err := os.ReadFile(path); err == nil {
			if !bytes.Equal(old, data) {
				return fmt.Errorf("FILE_READ_BINDING: coverage file changed on resume")
			}
		} else if !os.IsNotExist(err) {
			return err
		} else {
			if err = os.MkdirAll(filepath.Dir(path), 0700); err != nil {
				return err
			}
			if err = os.WriteFile(path, data, 0600); err != nil {
				return err
			}
		}
		return nil
	}
	for path, data := range p.files {
		if err := write(path, data); err != nil {
			return err
		}
	}

	return nil
}

type coverageSourcePath struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// Bind the ordinary candidate workspace. Source discovery belongs to the
// independent reviewer; the engine neither guesses test paths nor builds a
// language-specific dependency closure or another source projection.
func coverageSourcePaths(run Run, claim protocol.ExecutionClaim, workspace string) ([]coverageSourcePath, error) {
	if run.Regular.BaseSnapshot == "" {
		return nil, fmt.Errorf("COVERAGE_SOURCE_BINDING: missing base snapshot")
	}
	head, err := gitprep.Head(workspace)
	if err != nil {
		return nil, err
	}
	if head != claim.Candidate {
		return nil, fmt.Errorf("COVERAGE_SOURCE_BINDING: workspace HEAD differs from candidate")
	}
	changed, err := gitprep.Changes(workspace, claim.Candidate)
	if err != nil {
		return nil, err
	}
	if len(changed) != 0 {
		// Incremental development may restore previously reviewed test artifacts
		// before coverage. Validate those existing bindings, without classifying
		// files by language or limiting the reviewer's ordinary read access.
		var retained *protocol.PreparedQA
		if run.Regular.Preparation != nil {
			retained = run.Regular.Preparation.QA["whitebox"]
		}
		if retained == nil || retained.TestArtifactsFrom == "" {
			return nil, fmt.Errorf("COVERAGE_SOURCE_BINDING: workspace differs from candidate")
		}
		declared := map[string]bool{}
		for _, name := range retained.Manifest {
			declared[name] = true
		}
		for _, name := range changed {
			if !declared[name] {
				return nil, fmt.Errorf("COVERAGE_SOURCE_BINDING: workspace differs from retained test artifacts")
			}
		}
		args := append([]string{"diff", "--quiet", retained.TestArtifactsFrom, "--"}, changed...)
		if _, err := gitprep.Git(workspace, args...); err != nil {
			return nil, fmt.Errorf("COVERAGE_SOURCE_BINDING: retained test artifacts changed: %w", err)
		}
	}
	full, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	return []coverageSourcePath{{Path: full, Reason: "bound candidate workspace; independently locate relevant implementation before inspecting existing tests"}}, nil
}
