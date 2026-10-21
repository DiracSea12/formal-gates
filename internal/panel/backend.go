// Package panel projects the flow-configuration panel's read surface and
// delegates every write to the existing settings and facade owners. It adds
// no state, cache, or write path of its own.
package panel

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"formal-gates/internal/engine/decision"
	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/prompts"
	"formal-gates/internal/settings"
	"formal-gates/internal/validate"
)

// Backend projects one project's configuration library for the panel. Root is
// the project directory and Home the user configuration home, matching the
// CLI's overlay inputs.
type Backend struct {
	Root string
	Home string
}

// RunSummary is the only run projection the panel receives: identity,
// progress, and the engine's node obligations. It never carries tokens or raw
// run state.
type RunSummary struct {
	RunID     string `json:"runId"`
	Status    string `json:"status"`
	StartedAt string `json:"startedAt,omitempty"`
	// Requirement is the run's requirement document display name: the
	// basename of the confirmed intake source path, e.g. "requirements.md".
	Requirement   string                        `json:"requirement,omitempty"`
	Configuration string                        `json:"configuration"`
	Next          decision.NextResult           `json:"next"`
	Nodes         []protocol.ExecutionNodeState `json:"nodes"`
	BusinessNodes []protocol.BusinessNode       `json:"businessNodes"`
	Topology      *protocol.TopologyBinding     `json:"topology,omitempty"`
	Flow          *protocol.FlowProgress        `json:"flow,omitempty"`
	Slices        []SliceProgress               `json:"slices,omitempty"`
}

// SliceProgress is the read-only identity/status of one slice child run plus
// the child's own node and flow projections; it never carries tokens or raw
// state.
type SliceProgress struct {
	ID              string                        `json:"id"`
	RunID           string                        `json:"runId"`
	Status          string                        `json:"status"`
	EnteredMainline bool                          `json:"enteredMainline,omitempty"`
	Nodes           []protocol.ExecutionNodeState `json:"nodes,omitempty"`
	Flow            *protocol.FlowProgress        `json:"flow,omitempty"`
}

// ConfigurationView adds the overlay scope the merged option was read from;
// settings.ConfigurationOption.Source is an absolute file path, which the API
// write surface must not echo back as the scope argument.
type ConfigurationView struct {
	settings.ConfigurationOption
	Scope string `json:"scope,omitempty"`
}

// State is the merged configuration library view the panel renders.
type State struct {
	Configurations []ConfigurationView `json:"configurations"`
	Active         string              `json:"active"`
	Effective      settings.Effective  `json:"effective"`
	Revisions      map[string]string   `json:"revisions"`
	Bindings       map[string][]string `json:"bindings"`
	Runs           []RunSummary        `json:"runs"`
	Error          string              `json:"error,omitempty"`
}

// IntakeSelection is the configuration choice recorded beside a checked
// candidate; it authorizes no start.
type IntakeSelection struct {
	Configuration string   `json:"configuration"`
	Disabled      []string `json:"disabled"`
}

// IntakeCheck reports a saved confirmation candidate's current validity and
// the selection the panel recorded with it.
type IntakeCheck struct {
	Superseded bool                  `json:"superseded"`
	Reason     string                `json:"reason,omitempty"`
	Preview    *facade.IntakePreview `json:"preview,omitempty"`
	Selection  *IntakeSelection      `json:"selection,omitempty"`
}

// State reads the merged library, overlay revisions, bindings, and every
// readable top-level run. A dangling active pointer is reported instead of
// silently falling back.
func (b Backend) State() (State, error) {
	effective, options, err := settings.LoadConfigurations(b.Root, b.Home)
	if err != nil && !errors.Is(err, settings.ErrActiveConfigurationMissing) {
		return State{}, err
	}
	state := State{
		Configurations: configurationViews(options, effective),
		Active:         effective.ConfigurationName,
		Effective:      effective,
		Revisions:      effective.Revisions,
		Bindings:       map[string][]string{},
		Runs:           []RunSummary{},
	}
	if err != nil {
		state.Error = err.Error()
	}
	bindings, bindErr := facade.BoundConfigurations(b.Root)
	if bindErr == nil {
		state.Bindings = bindings
	}
	entries, readErr := os.ReadDir(filepath.Join(b.Root, facade.EngineNamespace))
	if readErr != nil {
		return state, nil
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		run, runErr := openRun(b.Root, entry.Name())
		if runErr != nil || run.MasterRunID != "" {
			continue
		}
		summary := runSummary(run)
		for id, childID := range run.SliceChildren {
			child, err := openRun(b.Root, childID)
			if err != nil {
				continue
			}
			summary.Slices = append(summary.Slices, SliceProgress{
				ID:              id,
				RunID:           childID,
				Status:          child.Status,
				EnteredMainline: run.FlowProgress.EnteredMainlineSlice != nil && slices.Contains(run.FlowProgress.EnteredMainlineSlice, id),
				Nodes:           child.NodeStates,
				Flow:            &child.FlowProgress,
			})
		}
		slices.SortFunc(summary.Slices, func(a, b SliceProgress) int { return strings.Compare(a.ID, b.ID) })
		state.Runs = append(state.Runs, summary)
	}
	return state, nil
}

func openRun(root, runID string) (facade.Run, error) {
	f, err := facade.Open(root, runID)
	if err != nil {
		return facade.Run{}, err
	}
	return f.Show()
}

func runSummary(run facade.Run) RunSummary {
	requirement := ""
	if receipt := run.IntakeConfirmationReceipt; receipt != nil && receipt.RequirementSource != "" {
		requirement = path.Base(receipt.RequirementSource)
	}
	return RunSummary{
		RunID:         run.RunID,
		Status:        run.Status,
		StartedAt:     run.StartedAt,
		Requirement:   requirement,
		Configuration: run.Configuration.Name,
		Next:          run.Next,
		Nodes:         run.NodeStates,
		BusinessNodes: run.BusinessNodes,
		Topology:      run.Review.Topology,
		Flow:          &run.FlowProgress,
	}
}

// configurationViews derives each merged option's overlay scope by comparing
// its absolute source path to the effective overlay files; the built-in
// configuration has no writable scope.
func configurationViews(options []settings.ConfigurationOption, effective settings.Effective) []ConfigurationView {
	views := make([]ConfigurationView, 0, len(options))
	for _, option := range options {
		scope := ""
		if len(effective.Files) >= 2 {
			switch option.Source {
			case effective.Files[0]:
				scope = "user"
			case effective.Files[1]:
				scope = "project"
			}
		}
		views = append(views, ConfigurationView{ConfigurationOption: option, Scope: scope})
	}
	return views
}

func unfinished(run facade.Run) bool {
	return run.Status != "COMPLETE" && run.Status != "ABORTED"
}

// Save stores a complete draft in one locked transaction through
// settings.WriteConfiguration. When runID selects an unfinished run bound to
// name, the candidate must preserve the responsibilities of completed,
// claimed, and bypassed nodes and keep the run dispatchable.
func (b Backend) Save(scope, name string, candidate settings.Configuration, overwrite bool, expected map[string]string, runID string) (settings.Effective, error) {
	if runID != "" {
		f, err := facade.Open(b.Root, runID)
		if err != nil {
			return settings.Effective{}, err
		}
		run, err := f.Show()
		if err != nil {
			return settings.Effective{}, err
		}
		if unfinished(run) && run.Configuration.Name == name {
			current, err := b.configurationValue(name)
			if err != nil {
				return settings.Effective{}, err
			}
			if err := guardRunSave(f, run, name, current, candidate); err != nil {
				return settings.Effective{}, err
			}
		}
	}
	return settings.WriteConfiguration(b.Root, b.Home, scope, name, candidate, overwrite, expected)
}

// configurationValue loads the bound name's current merged library value; the
// guard compares the candidate to what a save would replace, not to the run's
// last applied history.
func (b Backend) configurationValue(name string) (settings.Configuration, error) {
	_, options, err := settings.LoadConfigurations(b.Root, b.Home)
	if err != nil && !errors.Is(err, settings.ErrActiveConfigurationMissing) {
		return settings.Configuration{}, err
	}
	for _, option := range options {
		if option.Name == name {
			return option.Configuration, nil
		}
	}
	return settings.Configuration{}, nil
}

// guardRunSave compares the candidate to the bound name's current library
// value and rejects changes to responsibilities the engine reports as no
// longer editable, then checks the candidate keeps the run continuable.
func guardRunSave(f *facade.Facade, run facade.Run, name string, current, candidate settings.Configuration) error {
	for _, state := range run.NodeStates {
		if state.Editable {
			continue
		}
		if nodeResponsibilitiesDiffer(current, candidate, state.Node) {
			return fmt.Errorf("configuration %q changes %s, which is already completed, claimed, or bypassed in the selected run", name, state.Node)
		}
	}
	states, err := f.ConfigurationNodeStates(candidate)
	if err != nil {
		return err
	}
	return guardCandidateStates(states)
}

// guardCandidateStates applies §3.2.2's save guards to the engine's own
// projection: at least one enabled node must still owe dispatchable work, and
// outstanding QA, gate, or repair work cannot be saved with development
// closed.
func guardCandidateStates(states []protocol.ExecutionNodeState) error {
	dispatchable := false
	owedRepairTail := false
	developmentEnabled := true
	for _, state := range states {
		switch state.Node {
		case "development":
			developmentEnabled = state.Enabled
		case "qa-blackbox", "qa-whitebox", "gate", "repair":
			owedRepairTail = owedRepairTail || state.Owed
		}
		if state.Owed && state.Enabled && state.Dispatchable {
			dispatchable = true
		}
	}
	if !dispatchable {
		return fmt.Errorf("the candidate leaves no enabled node with outstanding dispatchable work; the selected run could not continue under it")
	}
	if owedRepairTail && !developmentEnabled {
		return fmt.Errorf("outstanding QA, gate, or repair work cannot be saved with the development node disabled")
	}
	return nil
}

// nodeResponsibilitiesDiffer reports whether the candidate changes any field
// owned by one node: its enablement, QA mode, gate list, execution choice, or
// round limit. start-readiness's limit lives under the tech-review key.
func nodeResponsibilitiesDiffer(current, candidate settings.Configuration, node string) bool {
	disabled := node
	switch node {
	case "qa-blackbox", "qa-whitebox":
		disabled = "qa"
		mode := node[3:]
		if slices.Contains(current.QAModes, mode) != slices.Contains(candidate.QAModes, mode) {
			return true
		}
	case "gate":
		currentGates, candidateGates := gateList(current), gateList(candidate)
		if (current.Gates == nil) != (candidate.Gates == nil) || !slices.Equal(currentGates, candidateGates) {
			return true
		}
	}
	if flowDisabled(current, disabled) != flowDisabled(candidate, disabled) {
		return true
	}
	if current.EffectiveExecutionChoice(node, settings.ExecutionChoice{}) != candidate.EffectiveExecutionChoice(node, settings.ExecutionChoice{}) {
		return true
	}
	limitKey := node
	if node == "start-readiness" {
		limitKey = "tech-review"
	}
	return current.RoundLimit(limitKey) != candidate.RoundLimit(limitKey)
}

func gateList(configuration settings.Configuration) []string {
	if configuration.Gates == nil {
		return nil
	}
	return *configuration.Gates
}

func flowDisabled(configuration settings.Configuration, node string) bool {
	return configuration.FlowSelection != nil && slices.Contains(configuration.FlowSelection.Disabled, node)
}

// Copy delegates to the locked automatic naming in settings.SaveConfiguration.
func (b Backend) Copy(scope, source string, expected map[string]string) (string, error) {
	return settings.SaveConfiguration(b.Root, b.Home, scope, "", source, expected)
}

// Rename delegates to the settings owner with live run bindings.
func (b Backend) Rename(scope, name, newName string, expected map[string]string) (settings.Effective, error) {
	return settings.RenameConfiguration(b.Root, b.Home, scope, name, newName, func() (map[string][]string, error) {
		return facade.BoundConfigurations(b.Root)
	}, expected)
}

// Delete delegates to the settings owner with live run bindings.
func (b Backend) Delete(scope, name string, expected map[string]string) (settings.Effective, error) {
	return settings.DeleteConfiguration(b.Root, b.Home, scope, name, func() (map[string][]string, error) {
		return facade.BoundConfigurations(b.Root)
	}, expected)
}

// Use delegates to the settings owner; the active pointer writes through the
// project overlay.
func (b Backend) Use(name string, expected map[string]string) (settings.Effective, error) {
	return settings.UseConfiguration(b.Root, b.Home, "project", name, expected)
}

// panelEditableSettings are the only global settings the panel edits.
var panelEditableSettings = map[string]bool{
	"panelIdleTimeout": true,
	"panelSelection":   true,
}

// EditPanelSetting changes one panel-owned global setting in its own overlay
// layer through the existing field-edit owner.
func (b Backend) EditPanelSetting(name, value string, expected map[string]string) (settings.Effective, error) {
	if !panelEditableSettings[name] {
		return settings.Effective{}, fmt.Errorf("setting %q is not editable from the panel", name)
	}
	effective, err := settings.Load(b.Root, b.Home)
	if err != nil {
		return settings.Effective{}, err
	}
	fields, err := settings.EditableFields(effective)
	if err != nil {
		return settings.Effective{}, err
	}
	for _, field := range fields {
		if field.Name != name {
			continue
		}
		scope := "project"
		if field.Source == effective.Files[0] {
			scope = "user"
		}
		return settings.EditField(b.Root, b.Home, scope, field.Position, value, false, expected)
	}
	return settings.Effective{}, fmt.Errorf("unknown setting %q", name)
}

// Gates lists the gate candidates for the selected configuration the same way
// the CLI does: package candidates minus the QA gates.
func (b Backend) Gates() ([]string, error) {
	admission, err := validate.ResolveEngineAdmission(b.Root, "")
	if err != nil {
		return nil, err
	}
	candidates, err := prompts.PackageRouteCandidates(admission.Target)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(candidates, func(gate string) bool {
		return gate == prompts.BlackboxQAID || gate == prompts.WhiteboxQAID
	}), nil
}

// ExecutionOptions reads the native choices for one node of the selected
// configuration exactly as the settings CLI does. A missing registered native
// runtime is an ordinary error for this endpoint, not a library failure.
func (b Backend) ExecutionOptions(configurationName string, input facade.ExecutionSelectionInput) (facade.ExecutionOptions, error) {
	effective, options, err := settings.LoadConfigurations(b.Root, b.Home)
	if err != nil {
		return facade.ExecutionOptions{}, err
	}
	var selected *settings.ConfigurationOption
	for i := range options {
		if options[i].Name == configurationName {
			selected = &options[i]
			break
		}
	}
	if selected == nil {
		return facade.ExecutionOptions{}, fmt.Errorf("unknown configuration %q", configurationName)
	}
	admission, err := validate.ResolveEngineAdmission(b.Root, "")
	if err != nil {
		return facade.ExecutionOptions{}, err
	}
	node := executionNodeID(input.Node)
	current := selected.Configuration.EffectiveExecutionChoice(node, settings.ExecutionChoice{
		Provider:        admission.Provider,
		Model:           effective.Values.Model,
		ReasoningEffort: effective.Values.ReasoningEffort,
	})
	result, err := facade.QueryExecutionOptions(b.Root, admission.Target, effective.Values.Native, current, input)
	if err != nil {
		return result, err
	}
	result.Installed = installedExecutables(effective.Values.Native, result.Platforms)
	return result, nil
}

// executionNodeID resolves a panel node selection (id, description, or
// position) to the canonical execution node id used as the execution key.
func executionNodeID(value string) string {
	nodes := definition.ExecutionNodes()
	if n, err := strconv.Atoi(value); err == nil && n >= 1 && n <= len(nodes) {
		return nodes[n-1].ID
	}
	for _, node := range nodes {
		if value == node.ID || value == node.Description {
			return node.ID
		}
	}
	return value
}

// CheckIntake revalidates an earlier confirmation candidate through the same
// owner the engine uses, without confirming or starting anything. A missing
// or changed artifact makes the candidate superseded; prior use by another
// run does not.
func (b Backend) CheckIntake(candidatePath, configurationName string) (IntakeCheck, error) {
	receipt, check, err := b.intakeReceipt(candidatePath)
	if err != nil || check.Superseded {
		return check, err
	}
	_, options, err := settings.LoadConfigurations(b.Root, b.Home)
	if err != nil {
		return IntakeCheck{}, err
	}
	var selected *settings.ConfigurationOption
	for i := range options {
		if options[i].Name == configurationName {
			selected = &options[i]
			break
		}
	}
	if selected == nil {
		return IntakeCheck{}, fmt.Errorf("unknown configuration %q", configurationName)
	}
	if err := facade.ValidateStartConfiguration(selected.Configuration); err != nil {
		return IntakeCheck{}, err
	}
	preview, err := facade.CheckIntakeCandidate(b.Root, receipt, true)
	if err != nil {
		if strings.HasPrefix(err.Error(), facade.InvalidIntakeConfirmation) {
			return IntakeCheck{Superseded: true, Reason: err.Error()}, nil
		}
		return IntakeCheck{}, err
	}
	disabled := []string{}
	if selected.Configuration.FlowSelection != nil {
		disabled = slices.Clone(selected.Configuration.FlowSelection.Disabled)
	}
	return IntakeCheck{
		Preview:   &preview,
		Selection: &IntakeSelection{Configuration: configurationName, Disabled: disabled},
	}, nil
}

// intakeReceipt loads the candidate's original confirmation receipt exactly as
// the issuing CLI wrote it; no path generation or renumbering.
func (b Backend) intakeReceipt(candidatePath string) (facade.IntakeConfirmationReceipt, IntakeCheck, error) {
	path := candidatePath
	if !filepath.IsAbs(path) {
		path = filepath.Join(b.Root, path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return facade.IntakeConfirmationReceipt{}, IntakeCheck{Superseded: true, Reason: "the intake candidate no longer exists"}, nil
		}
		return facade.IntakeConfirmationReceipt{}, IntakeCheck{}, err
	}
	var receipt facade.IntakeConfirmationReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return facade.IntakeConfirmationReceipt{}, IntakeCheck{Superseded: true, Reason: "the intake candidate is unreadable"}, nil
	}
	return receipt, IntakeCheck{}, nil
}

// CheckIntakeFreshness validates the candidate's artifact bindings ahead of any
// configuration selection, so a stale candidate surfaces as superseded instead
// of being hidden behind an invalid draft.
func (b Backend) CheckIntakeFreshness(candidatePath string) (IntakeCheck, error) {
	receipt, check, err := b.intakeReceipt(candidatePath)
	if err != nil || check.Superseded {
		return check, err
	}
	if _, err := facade.CheckIntakeCandidate(b.Root, receipt, false); err != nil {
		if strings.HasPrefix(err.Error(), facade.InvalidIntakeConfirmation) {
			return IntakeCheck{Superseded: true, Reason: err.Error()}, nil
		}
		return IntakeCheck{}, err
	}
	return IntakeCheck{}, nil
}
