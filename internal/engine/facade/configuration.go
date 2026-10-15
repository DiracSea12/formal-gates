package facade

import (
	"fmt"
	"os"
	"slices"

	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/settings"
)

// ConfigurationInput edits a run's selected configuration. A named selection
// reads the saved value; omitted names edit the run's current value.
type ConfigurationInput struct {
	Name      string                   `json:"name,omitempty"`
	FlowNode  string                   `json:"flowNode,omitempty"`
	QAMode    string                   `json:"qaMode,omitempty"`
	Enabled   *bool                    `json:"enabled,omitempty"`
	AllGates  *bool                    `json:"allGates,omitempty"`
	Execution *ExecutionSelectionInput `json:"execution,omitempty"`
}

func (f *Facade) prepareConfiguration(input ConfigurationInput, run Run) (*protocol.ConfigurationSelection, error) {
	selected := run.Configuration
	snapshot, err := f.engine.Load()
	if err != nil {
		return nil, err
	}
	if input.Name != "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		_, options, err := settings.LoadConfigurations(f.root, home)
		if err != nil {
			return nil, err
		}
		option, err := settings.SelectConfiguration(options, input.Name)
		if err != nil {
			return nil, err
		}
		selected = protocol.ConfigurationSelection{Name: option.Name, Source: option.Source, Configuration: option.Configuration}
		base := protocol.ExecutionChoice{Provider: snapshot.State.RunProvider, Model: snapshot.State.Settings.Values.Model, ReasoningEffort: snapshot.State.Settings.Values.ReasoningEffort}
		execution := make(map[string]settings.ExecutionChoice, len(protocol.ExecutionNodeIDs()))
		for _, node := range protocol.ExecutionNodeIDs() {
			execution[node] = option.Configuration.EffectiveExecutionChoice(node, base)
		}
		selected.Configuration.Execution = execution
	}
	// The input is a disposable copy; the persisted run and saved defaults are
	// only changed by their own accepted control and settings owners.
	selected.Configuration = settings.CloneConfiguration(selected.Configuration)
	configuration := &selected.Configuration
	if input.FlowNode != "" || input.QAMode != "" {
		if input.Enabled == nil {
			return nil, fmt.Errorf("node selection requires enabled true or false")
		}
		if input.FlowNode != "" {
			if !slices.Contains(protocol.FlowNodeIDs(), input.FlowNode) {
				return nil, fmt.Errorf("unknown main-flow node %q", input.FlowNode)
			}
			if configuration.FlowSelection == nil {
				configuration.FlowSelection = &settings.FlowSelection{}
			}
			configuration.FlowSelection.Disabled = settings.SetSelection(configuration.FlowSelection.Disabled, input.FlowNode, !*input.Enabled)
		}
		if input.QAMode != "" {
			if input.QAMode != "blackbox" && input.QAMode != "whitebox" {
				return nil, fmt.Errorf("unknown QA group %q", input.QAMode)
			}
			configuration.QAModes = settings.SetSelection(configuration.QAModes, input.QAMode, *input.Enabled)
		}
	}
	if input.AllGates != nil {
		configuration.Gates = nil
		if !*input.AllGates {
			empty := []string{}
			configuration.Gates = &empty
		}
	}
	if input.Execution != nil {
		node, err := executionNode(input.Execution.Node)
		if err != nil {
			return nil, err
		}
		current := configuration.Execution[node.ID]
		selection, err := PrepareExecutionSelection(f.root, f.admission.Target, snapshot.State.Settings.Values.Native, current, *input.Execution)
		if err != nil {
			return nil, err
		}
		if configuration.Execution == nil {
			configuration.Execution = map[string]settings.ExecutionChoice{}
		}
		configuration.Execution[selection.Node] = selection.Choice
	}
	if err := settings.ValidateConfiguration(*configuration); err != nil {
		return nil, err
	}
	return &selected, nil
}
