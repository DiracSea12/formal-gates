package facade

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/prompts"
	"formal-gates/internal/settings"
)

// ValidateStartConfiguration rejects a configuration that cannot start a run:
// QA, gates or repair without an enabled development node, or no enabled
// flow node at all.
func ValidateStartConfiguration(configuration settings.Configuration) error {
	enabled := func(node string) bool {
		return configuration.FlowSelection == nil || !slices.Contains(configuration.FlowSelection.Disabled, node)
	}
	qa := enabled("qa") && len(configuration.QAModes) != 0
	if !enabled("development") && (qa || enabled("gate") || enabled("repair")) {
		return fmt.Errorf("QA, gates and repair require an enabled development node")
	}
	for _, node := range definition.MainFlowNodes() {
		if enabled(node.ID) && (node.ID != "qa" || qa) {
			return nil
		}
	}
	return fmt.Errorf("starting a run requires at least one enabled flow node")
}

func StartConfiguration(root, selection string) (settings.Effective, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return settings.Effective{}, err
	}
	effective, options, err := settings.LoadConfigurations(root, home)
	if err != nil {
		return effective, err
	}
	selected, err := settings.SelectConfiguration(options, selection)
	if err != nil {
		return effective, err
	}
	effective.ConfigurationName, effective.Configuration, effective.ConfigurationSource = selected.Name, selected.Configuration, selected.Source
	return effective, nil
}

// configurationLibrary resolves the exact bound name in the merged library.
func configurationLibrary(root string) func(string) (settings.Configuration, error) {
	return func(name string) (settings.Configuration, error) {
		home, err := os.UserHomeDir()
		if err != nil {
			return settings.Configuration{}, err
		}
		_, options, err := settings.LoadConfigurations(root, home)
		var read *fs.PathError
		if errors.As(err, &read) {
			return settings.Configuration{}, err
		}
		if err != nil {
			return settings.Configuration{}, &protocol.ConfigurationUnavailable{Reason: err.Error()}
		}
		for _, option := range options {
			if option.Name == name {
				return option.Configuration, nil
			}
		}
		return settings.Configuration{}, &protocol.ConfigurationUnavailable{Reason: fmt.Sprintf("bound configuration %q does not exist", name)}
	}
}

func configurationGates(target string) func() ([]string, error) {
	return func() ([]string, error) {
		catalogue, err := prompts.LoadPromptCatalog(target)
		if err != nil {
			return nil, err
		}
		return catalogue.RouteCandidates(), nil
	}
}

func configurationParent(root string) func(string) (protocol.Snapshot, error) {
	return func(id string) (protocol.Snapshot, error) {
		parent, err := Open(root, id)
		if err != nil {
			return protocol.Snapshot{}, err
		}
		return parent.engine.Load()
	}
}
