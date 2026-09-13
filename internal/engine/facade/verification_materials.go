package facade

import (
	"encoding/json"
	"fmt"
	"sort"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

func verificationFile(files map[string][]byte, name string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	files[name] = append(data, '\n')
	return nil
}

// A checkpoint changes the execution slice, not the action's frozen materials.
// Publish each resumed slice under its invocation number and bind all of its
// references before prompt validation. Earlier files remain immutable.
func (m *nativePromptMaterial) bindVerificationResumeFiles(count int) {
	for key, name := range map[string]string{"caseFile": "qa-target.json", "checkpointFile": "checkpoint.json"} {
		data, ok := m.files[name]
		if !ok {
			continue
		}
		resumed := fmt.Sprintf("resume-%d-%s", count, name)
		delete(m.files, name)
		m.files[resumed] = data
		m.input[key] = map[string]string{"path": resumed}
		if key == "caseFile" {
			m.input["requiredInputs"] = []any{map[string]string{"path": resumed}}
		}
	}
}

func verificationExecutionInputs(run Run, w *protocol.VerificationWave, mode string, input map[string]any, files map[string][]byte) error {
	q := run.Regular.Preparation.QA[mode]
	if q == nil || q.Design == nil {
		return fmt.Errorf("missing bound %s execution design", mode)
	}
	var target *protocol.VerificationTarget
	for i := range w.Targets {
		if w.Targets[i].Target == mode {
			target = &w.Targets[i]
			break
		}
	}
	if target == nil {
		return fmt.Errorf("missing bound %s execution target", mode)
	}

	input["target"] = map[string]any{"target": target.Target, "task": target.Task, "reviewScope": target.ReviewScope, "scope": target.Scope, "expected": target.Expected, "bindingDigest": target.BindingDigest}
	checkpoint, hasCheckpoint := w.Checkpoints[mode]
	completed := map[string]bool{}
	for _, row := range checkpoint.Cases {
		completed[row.CaseID] = true
	}
	remaining := []string{}
	for _, id := range target.Expected {
		if !completed[id] {
			remaining = append(remaining, id)
		}
	}
	cases, shared, err := verificationCaseScope(q.Design.Manifest, remaining)
	if err != nil {
		return err
	}
	related := map[string]bool{}
	for _, id := range target.Expected {
		related[id] = true
	}
	for _, c := range cases {
		related[c.CaseID] = true
	}
	retained := map[string]string{}
	inherited := map[string]protocol.CaseExecution{}
	for id, row := range target.Inherited {
		if related[id] {
			retained[id] = row.Status
			inherited[id] = row
		}
	}
	if hasCheckpoint {
		rows := []protocol.CaseExecution{}
		for _, row := range checkpoint.Cases {
			if related[row.CaseID] {
				rows = append(rows, row)
				retained[row.CaseID] = row.Status
			}
		}
		checkpoint.Cases = rows
		ids := []string{}
		for _, id := range checkpoint.Remaining {
			if related[id] {
				ids = append(ids, id)
			}
		}
		checkpoint.Remaining = ids
		// Case observations retain the exact original evidence; old free-form review
		// findings are not execution instructions for a different remaining case.
		checkpoint.Findings = nil
		invalidated := []protocol.CheckpointInvalidation{}
		for _, item := range checkpoint.Invalidated {
			if related[item.CaseID] {
				invalidated = append(invalidated, item)
			}
		}
		checkpoint.Invalidated = invalidated
	}
	if hasCheckpoint || len(inherited) > 0 {
		if err := verificationFile(files, "checkpoint.json", map[string]any{"checkpoint": checkpoint, "inherited": inherited}); err != nil {
			return err
		}
		input["checkpointFile"] = map[string]string{"path": "checkpoint.json"}
	}
	if err := verificationFile(files, "qa-target.json", map[string]any{"binding": q.Design.Manifest.Binding, "execute": remaining, "cases": cases, "sharedSteps": shared}); err != nil {
		return err
	}
	input["caseFile"] = map[string]string{"path": "qa-target.json"}
	input["requiredInputs"] = []any{map[string]string{"path": "qa-target.json"}}
	input["remaining"] = remaining
	input["retainedCaseStatuses"] = retained
	input["materialUse"] = "Read requirement.path and caseFile.path in full. Execute only remaining IDs through bound QA operations. caseFile includes their necessary dependency definitions and shared steps; dependency presence does not authorize rerunning retained cases. The engine preserves prior rows. Read checkpointFile only for relevant resume evidence or the existing invalidation procedure."

	return nil
}
func verificationRepairInputs(run Run, plan *protocol.RepairPlan, input map[string]any, files map[string][]byte) error {
	problems := map[string]protocol.CanonicalProblem{}
	findingKeys := map[string]bool{}
	affected := map[string]bool{}
	for _, id := range plan.Problems {
		p, ok := run.Regular.Verification.Problems[id]
		if !ok {
			return fmt.Errorf("repair plan references absent problem %s", id)
		}
		problems[id] = p
		for _, key := range p.Findings {
			findingKeys[key] = true
		}
		for _, c := range p.AffectedCases {
			affected[c] = true
		}
	}
	for _, c := range plan.AffectedCases {
		affected[c] = true
	}
	if err := verificationFile(files, "repair-problems.json", problems); err != nil {
		return err
	}
	input["problemFile"] = map[string]string{"path": "repair-problems.json"}
	input["requiredInputs"] = []any{map[string]string{"path": "repair-problems.json"}}
	references := []any{}
	for _, wave := range run.Regular.Verification.Waves {
		targets := []string{}
		for key := range wave.Results {
			targets = append(targets, key)
		}
		sort.Strings(targets)
		for _, target := range targets {
			result := wave.Results[target]
			findings := []protocol.ReviewFinding{}
			for _, finding := range result.Findings {
				if findingKeys[wave.ID+":"+target+":"+finding.ID] {
					findings = append(findings, finding)
				}
			}
			if len(findings) == 0 {
				continue
			}
			cases := []protocol.CaseExecution{}
			for _, c := range result.Cases {
				if affected[c.CaseID] {
					cases = append(cases, c)
				}
			}
			name := fmt.Sprintf("repair-result-%03d.json", len(references)+1)
			material := map[string]any{"actionId": result.ActionID, "attemptId": result.AttemptID, "candidate": result.Candidate, "requirementRevision": result.RequirementRevision, "bindingDigest": result.BindingDigest, "originalResultDigest": result.Digest, "findings": findings, "cases": cases}
			if err := verificationFile(files, name, material); err != nil {
				return err
			}
			references = append(references, map[string]any{"waveId": wave.ID, "target": target, "file": name})
		}
	}
	qa := map[string]any{}
	for mode, q := range run.Regular.Preparation.QA {
		if q == nil || q.Design == nil {
			continue
		}
		ids := []string{}
		for _, c := range q.Design.Manifest.Cases {
			if affected[c.CaseID] {
				ids = append(ids, c.CaseID)
			}
		}
		if len(ids) == 0 {
			continue
		}
		cases, steps, err := verificationCaseScope(q.Design.Manifest, ids)
		if err != nil {
			return err
		}

		qa[mode] = map[string]any{"binding": q.Design.Manifest.Binding, "cases": cases, "sharedSteps": steps}
	}
	if len(qa) > 0 {
		if err := verificationFile(files, "repair-qa.json", qa); err != nil {
			return err
		}
		input["repairQA"] = map[string]string{"path": "repair-qa.json"}
		input["requiredInputs"] = append(input["requiredInputs"].([]any), map[string]string{"path": "repair-qa.json"})
	}
	input["resultFiles"] = references
	input["materialUse"] = "Read requirement.path, problemFile.path and repairQA.path (when present) in full. Repair only repairPlan problems. Read a related resultFiles entry when locating that finding or its observed failure; the files retain original result binding/digest and only related findings/case observations. Use the frozen QA operations for repairPlan.affectedCases; do not rewrite independently owned QA artifacts."
	return nil
}

// Retain original case/step order and exact bodies, selecting only the current
// cases and their declared transitive prerequisites.
func verificationCaseScope(manifest coverage.AcceptanceManifest, ids []string) ([]coverage.AcceptanceCase, []coverage.SharedStep, error) {
	byID := map[string]coverage.AcceptanceCase{}
	for _, c := range manifest.Cases {
		byID[c.CaseID] = c
	}
	selected, visiting := map[string]bool{}, map[string]bool{}
	var visit func(string) error
	visit = func(id string) error {
		if visiting[id] {
			return fmt.Errorf("cyclic execution case dependency %s", id)
		}
		if selected[id] {
			return nil
		}
		c, ok := byID[id]
		if !ok {
			return fmt.Errorf("execution target/dependency %s absent from frozen design", id)
		}
		visiting[id] = true
		for _, dep := range c.Dependencies {
			if err := visit(dep); err != nil {
				return err
			}
		}
		delete(visiting, id)
		selected[id] = true
		return nil
	}
	for _, id := range ids {
		if err := visit(id); err != nil {
			return nil, nil, err
		}
	}
	cases := []coverage.AcceptanceCase{}
	needed := map[string]bool{}
	for _, c := range manifest.Cases {
		if selected[c.CaseID] {
			cases = append(cases, c)
			for _, id := range c.SharedSteps {
				needed[id] = true
			}
		}
	}
	steps := []coverage.SharedStep{}
	for _, step := range manifest.SharedSteps {
		if needed[step.ID] {
			steps = append(steps, step)
			delete(needed, step.ID)
		}
	}
	if len(needed) > 0 {
		return nil, nil, fmt.Errorf("execution cases require unknown shared preparation")
	}
	return cases, steps, nil
}
