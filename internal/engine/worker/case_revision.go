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
	Remove        bool               `json:"remove,omitempty"`
	Files         *[]Location        `json:"files,omitempty"`
	Operations    *[]OperationChange `json:"operations,omitempty"`
	Scenario      *string            `json:"scenario,omitempty"`
	SharedSteps   *[]int             `json:"sharedSteps,omitempty"`
	PublicEntry   *string            `json:"publicEntry,omitempty"`
	Preconditions *string            `json:"preconditions,omitempty"`
	Steps         *string            `json:"steps,omitempty"`
	Oracle        *string            `json:"oracle,omitempty"`
	TestFile      *int               `json:"testFile,omitempty"`
	TestSymbol    *int               `json:"testSymbol,omitempty"`
}
type SharedStepChange struct {
	Remove      bool               `json:"remove,omitempty"`
	Artifacts   *[]string          `json:"artifacts,omitempty"`
	Independent *bool              `json:"independent,omitempty"`
	Procedure   *string            `json:"procedure,omitempty"`
	Operations  *[]OperationChange `json:"operations,omitempty"`
	Files       *[]Location        `json:"files,omitempty"`
}

type OperationChange struct {
	Reuse   *int                    `json:"reuse,omitempty"`
	Kind    *coverage.OperationKind `json:"kind,omitempty"`
	Argv    *[]string               `json:"argv,omitempty"`
	Command *string                 `json:"command,omitempty"`
	Stdin   *string                 `json:"stdin,omitempty"`
}

// An explicit sequence supplies its final order; omitted original positions
// are removed. Reused operations retain omitted fields applicable to their kind.
func reviseOperations(original []coverage.Operation, changes []OperationChange) ([]coverage.Operation, error) {
	result := make([]coverage.Operation, 0, len(changes))
	for _, change := range changes {
		item := coverage.Operation{}
		if change.Reuse != nil {
			if *change.Reuse < 1 || *change.Reuse > len(original) {
				return nil, fmt.Errorf("operation selection is outside original QA item")
			}
			item = original[*change.Reuse-1]
		}
		if change.Kind != nil {
			if item.Kind != *change.Kind {
				switch *change.Kind {
				case coverage.OperationArgv:
					item.Command = ""
				case coverage.OperationShell:
					item.Argv = nil
				}
			}
			item.Kind = *change.Kind
		}
		if change.Argv != nil {
			item.Argv = *change.Argv
		}
		item.Argv = slices.Clone(item.Argv)
		if change.Command != nil {
			item.Command = *change.Command
		}
		if change.Stdin != nil {
			item.Stdin = *change.Stdin
		}
		result = append(result, item)
	}
	return result, coverage.ValidateOperations(result)
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
			ids := []string{}
			for _, step := range input.Manifest.SharedSteps {
				ids = append(ids, step.ID)
			}
			if err := applyCaseChange(&item, change, ids); err != nil {
				return nil, err
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
			if err := applySharedStepChange(&item, changes.SharedSteps[index]); err != nil {
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

// Presence in the semantic input controls inheritance for both design entries
// and affected-case revisions; no retained text is supplied by the model.
func (item *DesignedCase) UnmarshalJSON(raw []byte) error {
	type body DesignedCase
	value := struct {
		*body
		Operations *[]OperationChange `json:"operations,omitempty"`
	}{body: (*body)(item)}
	if err := decodeObject(raw, &value); err != nil {
		return err
	}
	var change CaseChange
	if err := json.Unmarshal(raw, &change); err != nil {
		return err
	}
	item.changes = &change
	if item.Case == 0 && value.Operations != nil {
		var err error
		item.Operations, err = reviseOperations(nil, *value.Operations)
		return err
	}
	return nil
}
func (item *DesignedSharedStep) UnmarshalJSON(raw []byte) error {
	type body DesignedSharedStep
	value := struct {
		*body
		Operations *[]OperationChange `json:"operations,omitempty"`
	}{body: (*body)(item)}
	if err := decodeObject(raw, &value); err != nil {
		return err
	}
	var change SharedStepChange
	if err := json.Unmarshal(raw, &change); err != nil {
		return err
	}
	item.changes = &change
	if item.Step == 0 && value.Operations != nil {
		var err error
		item.Operations, err = reviseOperations(nil, *value.Operations)
		return err
	}
	return nil
}
func applyCaseChange(item *Case, change CaseChange, stepIDs []string) error {
	selected := change.SharedSteps
	change.SharedSteps = nil
	operations := change.Operations
	change.Operations = nil
	raw, err := json.Marshal(change)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, item); err != nil {
		return err
	}
	if selected != nil {
		item.SharedSteps, err = selectIDs(*selected, stepIDs)
		if err != nil {
			return err
		}
	}
	if operations != nil {
		item.Operations, err = reviseOperations(item.Operations, *operations)
	}
	return err
}
func applySharedStepChange(item *SharedStep, change SharedStepChange) error {
	operations := change.Operations
	change.Operations = nil
	raw, err := json.Marshal(change)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, item); err != nil {
		return err
	}
	if operations != nil {
		item.Operations, err = reviseOperations(item.Operations, *operations)
	}
	return err
}
