package worker

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

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
	Artifacts   *[]StringChange    `json:"artifacts,omitempty"`
	Independent *bool              `json:"independent,omitempty"`
	Procedure   *string            `json:"procedure,omitempty"`
	Operations  *[]OperationChange `json:"operations,omitempty"`
	Files       *[]Location        `json:"files,omitempty"`
}

type OperationChange struct {
	Reuse   *int                    `json:"reuse,omitempty"`
	Kind    *coverage.OperationKind `json:"kind,omitempty"`
	Argv    *[]StringChange         `json:"argv,omitempty"`
	Command *StringChange           `json:"command,omitempty"`
	Stdin   *StringChange           `json:"stdin,omitempty"`
}

// StringChange selects original field positions or read bytes, or authors new text.
type StringChange struct {
	Reuse      *int            `json:"reuse,omitempty"`
	Read       *int            `json:"read,omitempty"`
	From       *int            `json:"from,omitempty"`
	To         *int            `json:"to,omitempty"`
	FromColumn *int            `json:"fromColumn,omitempty"`
	ToColumn   *int            `json:"toColumn,omitempty"`
	Parts      *[]StringChange `json:"parts,omitempty"`
	value      string
}

func (change *StringChange) UnmarshalJSON(raw []byte) error {
	*change = StringChange{}
	if len(raw) > 0 && raw[0] == '"' {
		return json.Unmarshal(raw, &change.value)
	}
	type body StringChange
	if err := decodeObject(raw, (*body)(change)); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		return err
	}
	if fields["fromColumn"] != nil || fields["toColumn"] != nil {
		if change.FromColumn == nil || change.ToColumn == nil {
			return fmt.Errorf("string source columns require both fromColumn and toColumn")
		}
	}
	if change.Parts != nil {
		if change.Reuse != nil || change.Read != nil || change.From != nil || change.To != nil || change.FromColumn != nil || change.ToColumn != nil {
			return fmt.Errorf("string parts select only an ordered sequence")
		}
	} else if change.Reuse != nil {
		if *change.Reuse < 1 || change.Read != nil || change.From != nil || change.To != nil || change.FromColumn != nil || change.ToColumn != nil {
			return fmt.Errorf("string reuse selects only an original field position")
		}
	} else if change.Read == nil || change.From == nil || change.To == nil || *change.Read < 1 || *change.From < 1 || *change.To < *change.From {
		return fmt.Errorf("string source requires read and an inclusive from/to line range")
	}
	return nil
}
func (change StringChange) MarshalJSON() ([]byte, error) {
	if change.Reuse != nil || change.Read != nil || change.Parts != nil {
		type body StringChange
		return json.Marshal(body(change))
	}
	return json.Marshal(change.value)
}

func (change StringChange) text(original []string, facts []LocationFact) (string, error) {
	if change.Parts != nil {
		var text strings.Builder
		for _, part := range *change.Parts {
			if part.Parts != nil {
				return "", fmt.Errorf("string parts select fields, read ranges or new text")
			}
			value, err := part.text(original, facts)
			if err != nil {
				return "", err
			}
			text.WriteString(value)
		}
		return text.String(), nil
	}
	if change.Reuse != nil {
		if *change.Reuse < 1 || *change.Reuse > len(original) {
			return "", fmt.Errorf("string selection is outside original field")
		}
		return original[*change.Reuse-1], nil
	}
	if change.Read != nil {
		return sourceReadText(facts, *change.Read, *change.From, *change.To, change.FromColumn, change.ToColumn)
	}
	return change.value, nil
}

func reviseStrings(original []string, changes []StringChange, facts []LocationFact) ([]string, error) {
	result := make([]string, 0, len(changes))
	for _, change := range changes {
		value, err := change.text(original, facts)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, nil
}

// An explicit sequence supplies its final order; omitted original positions
// are removed. Reused operations retain omitted fields applicable to their kind.
func reviseOperations(original []coverage.Operation, changes []OperationChange, facts []LocationFact) ([]coverage.Operation, error) {
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
			var err error
			item.Argv, err = reviseStrings(item.Argv, *change.Argv, facts)
			if err != nil {
				return nil, err
			}
		}
		item.Argv = slices.Clone(item.Argv)
		if change.Command != nil {
			var err error
			item.Command, err = change.Command.text(nil, facts)
			if err != nil {
				return nil, err
			}
		}
		if change.Stdin != nil {
			var err error
			item.Stdin, err = change.Stdin.text(nil, facts)
			if err != nil {
				return nil, err
			}
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

func reviseCases(input *protocol.ValidationInput, changes *CaseRevisions, facts []LocationFact) (*Manifest, error) {
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
			if err := applyCaseChange(&item, change, ids, facts); err != nil {
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
			if err := applySharedStepChange(&item, changes.SharedSteps[index], facts); err != nil {
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
	return nil
}
func (item *DesignedSharedStep) UnmarshalJSON(raw []byte) error {
	type body DesignedSharedStep
	value := struct {
		*body
		Operations *[]OperationChange `json:"operations,omitempty"`
		Artifacts  *[]StringChange    `json:"artifacts,omitempty"`
	}{body: (*body)(item)}
	if err := decodeObject(raw, &value); err != nil {
		return err
	}
	var change SharedStepChange
	if err := json.Unmarshal(raw, &change); err != nil {
		return err
	}
	item.changes = &change
	return nil
}
func applyCaseChange(item *Case, change CaseChange, stepIDs []string, facts []LocationFact) error {
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
		item.Operations, err = reviseOperations(item.Operations, *operations, facts)
	}
	return err
}
func applySharedStepChange(item *SharedStep, change SharedStepChange, facts []LocationFact) error {
	operations := change.Operations
	change.Operations = nil
	artifacts := change.Artifacts
	change.Artifacts = nil
	raw, err := json.Marshal(change)
	if err != nil {
		return err
	}
	if err := json.Unmarshal(raw, item); err != nil {
		return err
	}
	if operations != nil {
		item.Operations, err = reviseOperations(item.Operations, *operations, facts)
		if err != nil {
			return err
		}
	}
	if artifacts != nil {
		item.Artifacts, err = reviseStrings(item.Artifacts, *artifacts, facts)
	}
	return err
}
