package worker

import (
	"encoding/json"
	"fmt"
	"slices"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

type CaseRevisions struct {
	Cases       []CaseChange       `json:"cases"`
	SharedSteps []SharedStepChange `json:"sharedSteps,omitempty"`
	Checkpoint  bool               `json:"checkpoint,omitempty"`
}
type CaseChange struct {
	Files         *[]string             `json:"files,omitempty"`
	Operations    *[]coverage.Operation `json:"operations,omitempty"`
	Scenario      *string               `json:"scenario,omitempty"`
	SharedSteps   *[]int                `json:"sharedSteps,omitempty"`
	PublicEntry   *string               `json:"publicEntry,omitempty"`
	Preconditions *string               `json:"preconditions,omitempty"`
	Steps         *string               `json:"steps,omitempty"`
	Oracle        *string               `json:"oracle,omitempty"`
	TestRef       *string               `json:"testRef,omitempty"`
}
type SharedStepChange struct {
	Procedure  *string               `json:"procedure,omitempty"`
	Operations *[]coverage.Operation `json:"operations,omitempty"`
	Files      *[]string             `json:"files,omitempty"`
}

func revisionSharedSteps(input *protocol.ValidationInput) []coverage.SharedStep {
	steps := []coverage.SharedStep{}
	for _, step := range input.Manifest.SharedSteps {
		if slices.ContainsFunc(input.Manifest.Cases, func(item coverage.AcceptanceCase) bool {
			return slices.Contains(input.AffectedCases, item.CaseID) && slices.Contains(item.SharedSteps, step.ID)
		}) {
			steps = append(steps, step)
		}
	}
	return steps
}

func reviseCases(input *protocol.ValidationInput, changes *CaseRevisions) (*Manifest, error) {
	shared := revisionSharedSteps(input)
	if len(changes.Cases) != len(input.AffectedCases) || changes.SharedSteps != nil && len(changes.SharedSteps) != len(shared) {
		return nil, fmt.Errorf("case revisions must cover exactly the issued affected cases and shared steps")
	}
	manifest := &Manifest{Cases: []Case{}, Coverage: map[string][]string{}}
	for _, original := range input.Manifest.Cases {
		index := slices.Index(input.AffectedCases, original.CaseID)
		var item Case
		raw, err := json.Marshal(original)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		if index >= 0 {
			change := changes.Cases[index]
			sharedChoices := change.SharedSteps
			change.SharedSteps = nil
			raw, err = json.Marshal(change)
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(raw, &item); err != nil {
				return nil, err
			}
			if sharedChoices != nil {
				ids := []string{}
				for _, step := range input.Manifest.SharedSteps {
					ids = append(ids, step.ID)
				}
				item.SharedSteps, err = selectIDs(*sharedChoices, ids)
				if err != nil {
					return nil, err
				}
			}
		}
		manifest.Cases = append(manifest.Cases, item)
	}
	for _, original := range input.Manifest.SharedSteps {
		index := slices.IndexFunc(shared, func(step coverage.SharedStep) bool { return step.ID == original.ID })
		var item SharedStep
		raw, err := json.Marshal(original)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(raw, &item); err != nil {
			return nil, err
		}
		if index >= 0 && changes.SharedSteps != nil {
			raw, err = json.Marshal(changes.SharedSteps[index])
			if err != nil {
				return nil, err
			}
			if err := json.Unmarshal(raw, &item); err != nil {
				return nil, err
			}
		}
		manifest.SharedSteps = append(manifest.SharedSteps, item)
	}
	return manifest, nil
}
