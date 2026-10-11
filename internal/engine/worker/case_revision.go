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
	Remove        bool                  `json:"remove,omitempty"`
	Files         *[]Location           `json:"files,omitempty"`
	Operations    *[]coverage.Operation `json:"operations,omitempty"`
	Scenario      *string               `json:"scenario,omitempty"`
	SharedSteps   *[]int                `json:"sharedSteps,omitempty"`
	PublicEntry   *string               `json:"publicEntry,omitempty"`
	Preconditions *string               `json:"preconditions,omitempty"`
	Steps         *string               `json:"steps,omitempty"`
	Oracle        *string               `json:"oracle,omitempty"`
	TestFile      *int                  `json:"testFile,omitempty"`
	TestSymbol    *int                  `json:"testSymbol,omitempty"`
}
type SharedStepChange struct {
	Remove      bool                  `json:"remove,omitempty"`
	Artifacts   *[]string             `json:"artifacts,omitempty"`
	Independent *bool                 `json:"independent,omitempty"`
	Procedure   *string               `json:"procedure,omitempty"`
	Operations  *[]coverage.Operation `json:"operations,omitempty"`
	Files       *[]Location           `json:"files,omitempty"`
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
		item := Case{CaseID: original.CaseID, Scenario: original.Scenario, Operations: slices.Clone(original.Operations), SharedSteps: slices.Clone(original.SharedSteps), PublicEntry: original.PublicEntry, Preconditions: original.Preconditions, Steps: original.Steps, Oracle: original.Oracle}
		if index >= 0 {
			change := changes.Cases[index]
			if change.Remove {
				if err := validateRemoval(change); err != nil {
					return nil, err
				}
				manifest.RemoveCases = append(manifest.RemoveCases, original.CaseID)
				continue
			}
			sharedChoices := change.SharedSteps
			change.SharedSteps = nil
			raw, err := json.Marshal(change)
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
		item := SharedStep{Artifacts: slices.Clone(original.Artifacts), Independent: original.Independent, ID: original.ID, Procedure: original.Procedure, Operations: slices.Clone(original.Operations)}
		if index >= 0 && changes.SharedSteps != nil {
			if changes.SharedSteps[index].Remove {
				if err := validateRemoval(changes.SharedSteps[index]); err != nil {
					return nil, err
				}
				manifest.RemoveSharedSteps = append(manifest.RemoveSharedSteps, original.ID)
				continue
			}
			raw, err := json.Marshal(changes.SharedSteps[index])
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

func validateRemoval(change any) error {
	body, err := json.Marshal(change)
	if err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil {
		return err
	}
	if len(fields) != 1 {
		return fmt.Errorf("a removed QA item cannot also modify semantic content")
	}
	return nil
}
