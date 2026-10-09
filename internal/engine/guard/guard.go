// Package guard computes finite dispatch allowances from declared basis identities.
// It performs no I/O and does not authenticate confirmations or issue tasks.
// A preview only proves that supplied declarations are structurally consistent
// (REQ-013/014).
package guard

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"formal-gates/internal/engine/runtime"
)

type Identity struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

// ConfirmedIdentity requires an explicit declaration for the exact revision,
// rather than treating a missing confirmation as permission to expand the basis.
type ConfirmedIdentity struct {
	Identity
	ConfirmedRevision string `json:"confirmedRevision"`
}

// Definition declares the closed set of external node/step pairs. Dynamic scopes
// reuse those steps; local computation and arbitrary undeclared steps add no slots.
type Definition struct {
	Identity
	ExternalSteps []runtime.TaskKey `json:"externalSteps"`
}

type SourceKind string

const (
	DefinitionSource SourceKind = "DEFINITION"
	BatchSource      SourceKind = "BATCH"
	RouteSource      SourceKind = "ROUTE"
	TopologySource   SourceKind = "TOPOLOGY"
	QASource         SourceKind = "APPROVED_QA"
	GateSource       SourceKind = "SELECTED_GATE"
	ObligationSource SourceKind = "ESTABLISHED_OBLIGATION"
	RepairSource     SourceKind = "ESTABLISHED_REPAIR"
)

type Provenance struct {
	Kind SourceKind `json:"kind"`
	Identity
}

// ExternalTask covers both agent and host dispatches. A nil MaxAttempts uses 2;
// an explicit zero is invalid. Retries retain TaskKey and change only this policy.
type ExternalTask struct {
	TaskKey     runtime.TaskKey `json:"taskKey"`
	Provenance  Provenance      `json:"provenance"`
	MaxAttempts *int            `json:"maxAttempts,omitempty"`
}

type Basis struct {
	SchemaVersion          int               `json:"schemaVersion"`
	Requirement            ConfirmedIdentity `json:"requirement"`
	Batch                  ConfirmedIdentity `json:"batch"`
	Route                  ConfirmedIdentity `json:"route"`
	Topology               ConfirmedIdentity `json:"topology"`
	Definition             Definition        `json:"definition"`
	ApprovedQA             []Identity        `json:"approvedQA"`
	SelectedGates          []Identity        `json:"selectedGates"`
	EstablishedObligations []Identity        `json:"establishedObligations"`
	EstablishedRepairs     []Identity        `json:"establishedRepairs"`
	ExternalTasks          []ExternalTask    `json:"externalTasks"`
}

type Result struct {
	Basis                    Basis  `json:"basis"`
	DistinctExternalTaskKeys int    `json:"distinctExternalTaskKeys"`
	RequestAllowance         int    `json:"requestAllowance"`
	RetryAllowance           int    `json:"retryAllowance"`
	TotalAllowance           int    `json:"totalAllowance"`
	BudgetBasisDigest        string `json:"budgetBasisDigest"`
}

// Calculate owns both normalization and the allowance/digest algorithm. All
// collections are copied, sorted and deduplicated, so order, duplicate declarations
// and an omitted versus explicit default retry do not change the canonical basis.
// Every identity, provenance and retry value remains in the digest, even if its
// change does not affect the number of slots. No caller-owned data is modified.
func Calculate(input Basis) (Result, error) {
	b := input
	if b.SchemaVersion != 1 {
		return Result{}, fmt.Errorf("schemaVersion: expected 1")
	}
	for _, field := range []struct {
		name  string
		value ConfirmedIdentity
	}{
		{"requirement", b.Requirement}, {"batch", b.Batch},
		{"route", b.Route}, {"topology", b.Topology},
	} {
		if err := validateIdentity(field.name, field.value.Identity); err != nil {
			return Result{}, err
		}
		if field.value.ConfirmedRevision != field.value.Revision {
			return Result{}, fmt.Errorf("%s.confirmedRevision: must declare the exact revision", field.name)
		}
	}
	if err := validateIdentity("definition", b.Definition.Identity); err != nil {
		return Result{}, err
	}
	if b.Definition.ExternalSteps == nil {
		return Result{}, fmt.Errorf("definition.externalSteps: explicitly declare the closed external step set (empty array is allowed)")
	}
	steps := make(map[runtime.TaskKey]bool)
	b.Definition.ExternalSteps = make([]runtime.TaskKey, 0, len(input.Definition.ExternalSteps))
	for i, key := range input.Definition.ExternalSteps {
		if err := validateKey(key); err != nil || key.Scope != "" {
			return Result{}, fmt.Errorf("definition.externalSteps[%d]: expected a complete node/step without scope", i)
		}
		if !steps[key] {
			steps[key] = true
			b.Definition.ExternalSteps = append(b.Definition.ExternalSteps, key)
		}
	}
	sort.Slice(b.Definition.ExternalSteps, func(i, j int) bool {
		return b.Definition.ExternalSteps[i].String() < b.Definition.ExternalSteps[j].String()
	})
	sources := map[Provenance]bool{
		{DefinitionSource, b.Definition.Identity}: true,
		{BatchSource, b.Batch.Identity}:           true,
		{RouteSource, b.Route.Identity}:           true,
		{TopologySource, b.Topology.Identity}:     true,
	}
	for _, field := range []struct {
		name   string
		kind   SourceKind
		values *[]Identity
	}{
		{"approvedQA", QASource, &b.ApprovedQA},
		{"selectedGates", GateSource, &b.SelectedGates},
		{"establishedObligations", ObligationSource, &b.EstablishedObligations},
		{"establishedRepairs", RepairSource, &b.EstablishedRepairs},
	} {
		items, err := normalizeIdentities(field.name, *field.values)
		if err != nil {
			return Result{}, err
		}
		*field.values = items
		for _, item := range items {
			sources[Provenance{field.kind, item}] = true
		}
	}

	tasks := make(map[runtime.TaskKey]ExternalTask)
	b.ExternalTasks = make([]ExternalTask, 0, len(input.ExternalTasks))
	result := Result{}
	for i, task := range input.ExternalTasks {
		path := fmt.Sprintf("externalTasks[%d]", i)
		if err := validateKey(task.TaskKey); err != nil {
			return Result{}, fmt.Errorf("%s.taskKey: %w", path, err)
		}
		step := task.TaskKey
		step.Scope = ""
		if !steps[step] {
			return Result{}, fmt.Errorf("%s.taskKey: %q is not in definition.externalSteps", path, task.TaskKey.String())
		}
		if !sources[task.Provenance] {
			return Result{}, fmt.Errorf("%s.provenance: kind/id/revision must match a declared allowed source", path)
		}
		attempts := 2
		if task.MaxAttempts != nil {
			attempts = *task.MaxAttempts
		}
		if attempts < 1 {
			return Result{}, fmt.Errorf("%s.maxAttempts: must be at least 1", path)
		}
		task.MaxAttempts = &attempts
		if previous, exists := tasks[task.TaskKey]; exists {
			if previous.Provenance != task.Provenance || *previous.MaxAttempts != attempts {
				return Result{}, fmt.Errorf("%s: conflicting declarations for TaskKey %q", path, task.TaskKey.String())
			}
			continue
		}
		if attempts > int(^uint(0)>>1)-result.TotalAllowance {
			return Result{}, fmt.Errorf("%s.maxAttempts: total allowance exceeds integer range", path)
		}
		tasks[task.TaskKey] = task
		b.ExternalTasks = append(b.ExternalTasks, task)
		result.RequestAllowance++
		result.RetryAllowance += attempts - 1
		result.TotalAllowance += attempts
	}
	sort.Slice(b.ExternalTasks, func(i, j int) bool {
		return b.ExternalTasks[i].TaskKey.String() < b.ExternalTasks[j].TaskKey.String()
	})
	encoded, err := json.Marshal(b)
	if err != nil {
		return Result{}, fmt.Errorf("basis: %w", err)
	}
	result.Basis = b
	result.DistinctExternalTaskKeys = len(b.ExternalTasks)
	result.BudgetBasisDigest = fmt.Sprintf("sha256:%x", sha256.Sum256(encoded))
	return result, nil
}

func validateKey(key runtime.TaskKey) error {
	if strings.TrimSpace(string(key.Node)) == "" || strings.TrimSpace(string(key.Step)) == "" {
		return fmt.Errorf("node and step are required")
	}
	_, err := runtime.NewTaskKey(key.Node, key.Step, key.Scope)
	return err
}

func validateIdentity(path string, id Identity) error {
	if strings.TrimSpace(id.ID) == "" {
		return fmt.Errorf("%s.id: required", path)
	}
	if strings.TrimSpace(id.Revision) == "" {
		return fmt.Errorf("%s.revision: required", path)
	}
	return nil
}

func normalizeIdentities(path string, input []Identity) ([]Identity, error) {
	items := make([]Identity, 0, len(input))
	seen := make(map[string]string)
	for i, id := range input {
		if err := validateIdentity(fmt.Sprintf("%s[%d]", path, i), id); err != nil {
			return nil, err
		}
		if revision, exists := seen[id.ID]; exists {
			if revision != id.Revision {
				return nil, fmt.Errorf("%s[%d]: conflicting revisions for %q", path, i, id.ID)
			}
			continue
		}
		seen[id.ID] = id.Revision
		items = append(items, id)
	}
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
	return items, nil
}
