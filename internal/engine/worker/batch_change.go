package worker

import (
	"encoding/json"
	"fmt"
	"formal-gates/internal/engine/protocol"
	"slices"
)

type BatchChange struct {
	Deliverable   *string        `json:"deliverable,omitempty"`
	PublicEntry   *string        `json:"publicEntry,omitempty"`
	Interfaces    *string        `json:"interfaces,omitempty"`
	StateBoundary *string        `json:"stateBoundary,omitempty"`
	Risk          *string        `json:"risk,omitempty"`
	Rollback      *string        `json:"rollback,omitempty"`
	Recovery      *string        `json:"recovery,omitempty"`
	Handoff       *string        `json:"handoff,omitempty"`
	ContextCost   *string        `json:"contextCost,omitempty"`
	Spike         *bool          `json:"spike,omitempty"`
	Owner         *int           `json:"owner,omitempty"`
	DependsOn     *[]int         `json:"dependsOn,omitempty"`
	Subtasks      *[]Subtask     `json:"subtasks,omitempty"`
	DoD           *[]Criterion   `json:"dod,omitempty"`
	Checks        *[]Check       `json:"checks,omitempty"`
	Artifacts     *[]Location    `json:"artifacts,omitempty"`
	NewArtifacts  *[]string      `json:"newArtifacts,omitempty"`
	Route         *RouteProposal `json:"route,omitempty"`
}

func (item *Batch) UnmarshalJSON(raw []byte) error {
	type body Batch
	if err := decodeObject(raw, (*body)(item)); err != nil {
		return err
	}
	var change BatchChange
	if err := json.Unmarshal(raw, &change); err != nil {
		return err
	}
	item.changes = &change
	return nil
}

func reviseSubtasks(original []protocol.DevelopmentSubtask, changes []Subtask, batch string) ([]protocol.DevelopmentSubtask, error) {
	if len(changes) == 0 {
		return []protocol.DevelopmentSubtask{}, nil
	}
	result := slices.Clone(original)
	removed := map[string]bool{}
	for _, change := range changes {
		if change.Reuse == 0 {
			if change.Remove {
				return nil, fmt.Errorf("removal selects an original subtask")
			}
			result = append(result, protocol.DevelopmentSubtask{ID: contentID("subtask", struct{ Batch, Description string }{batch, change.Description}, len(result)), Description: change.Description})
			continue
		}
		if change.Reuse < 1 || change.Reuse > len(original) {
			return nil, fmt.Errorf("subtask selection is outside original batch")
		}
		if change.Remove {
			if change.Description != "" {
				return nil, fmt.Errorf("removed subtask cannot also change content")
			}
			removed[original[change.Reuse-1].ID] = true
			continue
		}
		if change.Description != "" {
			result[change.Reuse-1].Description = change.Description
		}
	}
	return slices.DeleteFunc(result, func(item protocol.DevelopmentSubtask) bool { return removed[item.ID] }), nil
}
func reviseCriteria(original []protocol.DevelopmentCriterion, changes []Criterion) ([]protocol.DevelopmentCriterion, error) {
	if len(changes) == 0 {
		return []protocol.DevelopmentCriterion{}, nil
	}
	result := slices.Clone(original)
	removed := map[string]bool{}
	for _, change := range changes {
		if change.Reuse == 0 {
			if change.Remove {
				return nil, fmt.Errorf("removal selects an original criterion")
			}
			result = append(result, protocol.DevelopmentCriterion{ID: contentID("criterion", change.Description, len(result)), Description: change.Description})
			continue
		}
		if change.Reuse < 1 || change.Reuse > len(original) {
			return nil, fmt.Errorf("criterion selection is outside original batch")
		}
		if change.Remove {
			if change.Description != "" {
				return nil, fmt.Errorf("removed criterion cannot also change content")
			}
			removed[original[change.Reuse-1].ID] = true
			continue
		}
		if change.Description != "" {
			result[change.Reuse-1].Description = change.Description
		}
	}
	return slices.DeleteFunc(result, func(item protocol.DevelopmentCriterion) bool { return removed[item.ID] }), nil
}
