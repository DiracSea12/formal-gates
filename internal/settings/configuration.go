package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strconv"
	"strings"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/host"
)

// FlowSelection owns the fixed main-flow choices of a run.
type FlowSelection struct {
	Disabled []string `json:"disabled"`
}

// ExecutionChoice preserves native values. On the run platform, omitted values
// inherit run defaults. Changing platform leaves omitted native options unset.
type ExecutionChoice struct {
	Provider        string `json:"provider,omitempty"`
	Model           string `json:"model,omitempty"`
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
}

// Configuration groups workflow choices. A nil Gates selects all discovered
// ordinary gates; an explicit empty list selects none. QA is selected separately.
type Configuration struct {
	FlowSelection *FlowSelection             `json:"flowSelection,omitempty"`
	QAModes       []string                   `json:"qaModes"`
	Gates         *[]string                  `json:"gates,omitempty"`
	Execution     map[string]ExecutionChoice `json:"execution,omitempty"`
}

const DefaultConfigurationName = "默认"

func DefaultConfiguration() Configuration {
	return Configuration{QAModes: []string{}}
}

func (configuration Configuration) EffectiveExecutionChoice(node string, defaults ExecutionChoice) ExecutionChoice {
	choice := configuration.Execution[node]
	if choice.Provider != "" && choice.Provider != defaults.Provider {
		defaults = ExecutionChoice{Provider: choice.Provider}
	}
	if choice.Model != "" {
		defaults.Model = choice.Model
	}
	if choice.ReasoningEffort != "" {
		defaults.ReasoningEffort = choice.ReasoningEffort
	}
	return defaults
}

func ValidateFlowSelection(selection *FlowSelection) error {
	if selection == nil {
		return nil
	}
	var ids []string
	for _, node := range definition.MainFlowNodes() {
		ids = append(ids, node.ID)
	}
	for i, id := range selection.Disabled {
		if !slices.Contains(ids, id) {
			return fmt.Errorf("flow selection: unknown main-flow node %q (available: %v)", id, ids)
		}
		if slices.Contains(selection.Disabled[:i], id) {
			return fmt.Errorf("flow selection: node %q is disabled more than once", id)
		}
	}
	return nil
}

func ValidateConfiguration(configuration Configuration) error {
	if err := ValidateFlowSelection(configuration.FlowSelection); err != nil {
		return err
	}
	for i, mode := range configuration.QAModes {
		if mode != "blackbox" && mode != "whitebox" {
			return fmt.Errorf("unknown QA mode %q (available: blackbox, whitebox)", mode)
		}
		if slices.Contains(configuration.QAModes[:i], mode) {
			return fmt.Errorf("QA mode %q is selected more than once", mode)
		}
	}
	if configuration.Gates != nil {
		for i, gate := range *configuration.Gates {
			if strings.TrimSpace(gate) == "" || slices.Contains([]string{"blackbox", "whitebox", "merge", "merge-qa", "merge-gate"}, gate) {
				return fmt.Errorf("%q is not an ordinary gate", gate)
			}
			if slices.Contains((*configuration.Gates)[:i], gate) {
				return fmt.Errorf("gate %q is selected more than once", gate)
			}
		}
	}
	var ids []string
	for _, node := range definition.ExecutionNodes() {
		ids = append(ids, node.ID)
	}
	for node, choice := range configuration.Execution {
		if !slices.Contains(ids, node) {
			return fmt.Errorf("unknown execution node %q (available: %v)", node, ids)
		}
		if choice.Provider != "" {
			platform, err := host.Lookup(choice.Provider)
			if err != nil || platform.ID != choice.Provider {
				return fmt.Errorf("execution node %s: unsupported canonical platform %q", node, choice.Provider)
			}
		}
		if choice.Model != "" && strings.TrimSpace(choice.Model) == "" || choice.ReasoningEffort != "" && strings.TrimSpace(choice.ReasoningEffort) == "" {
			return fmt.Errorf("execution node %s: model and effort must be nonempty values or omitted", node)
		}
	}
	return nil
}

type ConfigurationOption struct {
	Position      int           `json:"position"`
	Name          string        `json:"name"`
	Source        string        `json:"source"`
	Active        bool          `json:"active"`
	Configuration Configuration `json:"configuration"`
}

// LoadConfigurations uses the same loader as run creation and field editing.
// Only the selected configuration is included in Effective and frozen in a run.
func LoadConfigurations(root, home string) (Effective, []ConfigurationOption, error) {
	return load(root, home, "", nil)
}

type configurationSet struct {
	values  map[string]Configuration
	sources map[string]string
	active  string
}

func newConfigurationSet() *configurationSet {
	return &configurationSet{values: map[string]Configuration{DefaultConfigurationName: DefaultConfiguration()}, sources: map[string]string{DefaultConfigurationName: "default"}, active: DefaultConfigurationName}
}

func (set *configurationSet) apply(path string, raw map[string]json.RawMessage) error {
	if data, present := raw["configurations"]; present {
		var named map[string]json.RawMessage
		if err := json.Unmarshal(data, &named); err != nil {
			return fmt.Errorf("%s: configurations: %w", path, err)
		}
		for name, data := range named {
			if strings.TrimSpace(name) == "" {
				return fmt.Errorf("%s: configuration name must be nonempty", path)
			}
			var fields map[string]any
			if err := json.Unmarshal(data, &fields); err != nil || fields == nil {
				return fmt.Errorf("%s: configuration %q must be an object", path, name)
			}
			if configurationHasNull(fields) {
				return fmt.Errorf("%s: configuration %q: null is invalid; remove the field to inherit", path, name)
			}
			configuration := DefaultConfiguration()
			decoder := json.NewDecoder(bytes.NewReader(data))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&configuration); err != nil {
				return fmt.Errorf("%s: configuration %q: %w", path, name, err)
			}
			if err := ValidateConfiguration(configuration); err != nil {
				return fmt.Errorf("%s: configuration %q: %w", path, name, err)
			}
			set.values[name], set.sources[name] = configuration, path
		}
	}
	if data, present := raw["configuration"]; present {
		if err := json.Unmarshal(data, &set.active); err != nil || strings.TrimSpace(set.active) == "" {
			return fmt.Errorf("%s: configuration must name an existing configuration", path)
		}
	}
	return nil
}

func configurationHasNull(value any) bool {
	switch value := value.(type) {
	case nil:
		return true
	case map[string]any:
		for _, child := range value {
			if configurationHasNull(child) {
				return true
			}
		}
	case []any:
		for _, child := range value {
			if configurationHasNull(child) {
				return true
			}
		}
	}
	return false
}

func (set *configurationSet) options() []ConfigurationOption {
	names := make([]string, 0, len(set.values))
	for name := range set.values {
		if name != DefaultConfigurationName {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	names = append([]string{DefaultConfigurationName}, names...)
	options := make([]ConfigurationOption, 0, len(names))
	for i, name := range names {
		options = append(options, ConfigurationOption{Position: i + 1, Name: name, Source: set.sources[name], Active: name == set.active, Configuration: set.values[name]})
	}
	return options
}

func SelectConfiguration(options []ConfigurationOption, selection string) (ConfigurationOption, error) {
	for _, option := range options {
		if option.Name == selection || selection == "" && option.Active {
			return option, nil
		}
	}
	if n, err := strconv.Atoi(selection); err == nil && n >= 1 && n <= len(options) {
		return options[n-1], nil
	}
	return ConfigurationOption{}, fmt.Errorf("unknown configuration %q; use settings show to select its name or position", selection)
}

func CloneConfiguration(configuration Configuration) Configuration {
	if configuration.FlowSelection != nil {
		configuration.FlowSelection = &FlowSelection{Disabled: slices.Clone(configuration.FlowSelection.Disabled)}
	}
	configuration.QAModes = slices.Clone(configuration.QAModes)
	configuration.Execution = maps.Clone(configuration.Execution)
	if configuration.Gates != nil {
		gates := slices.Clone(*configuration.Gates)
		configuration.Gates = &gates
	}
	return configuration
}

func SetSelection(values []string, value string, selected bool) []string {
	values = slices.DeleteFunc(values, func(existing string) bool { return existing == value })
	if selected {
		values = append(values, value)
	}
	return values
}

func configurationMap(raw map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	named := map[string]json.RawMessage{}
	if data, present := raw["configurations"]; present {
		if err := json.Unmarshal(data, &named); err != nil {
			return nil, err
		}
	}
	return named, nil
}

func SaveConfiguration(root, home, scope, name, from string) (Effective, error) {
	if strings.TrimSpace(name) == "" {
		return Effective{}, fmt.Errorf("configuration name must be supplied by the user")
	}
	return editOverlay(root, home, scope, func(raw map[string]json.RawMessage, effective Effective, options []ConfigurationOption) error {
		for _, option := range options {
			if option.Name == name && option.Source != "default" {
				return fmt.Errorf("configuration %q already exists; use settings set to modify it", name)
			}
		}
		selected, err := SelectConfiguration(options, from)
		if err != nil {
			return err
		}
		return storeConfiguration(raw, name, selected.Configuration)
	})
}

func storeConfiguration(raw map[string]json.RawMessage, name string, configuration Configuration) error {
	named, err := configurationMap(raw)
	if err != nil {
		return err
	}
	if configuration.QAModes == nil {
		configuration.QAModes = []string{}
	}
	if configuration.FlowSelection != nil && configuration.FlowSelection.Disabled == nil {
		configuration.FlowSelection.Disabled = []string{}
	}
	if configuration.Gates != nil && *configuration.Gates == nil {
		empty := []string{}
		configuration.Gates = &empty
	}
	named[name], err = json.Marshal(configuration)
	if err != nil {
		return err
	}
	raw["configurations"], err = json.Marshal(named)
	return err
}

// EditConfiguration reads and changes its selected value while holding the
// same overlay lock as field edits, preserving concurrent edits to other values.
func EditConfiguration(root, home, scope, selection string, change func(*Configuration) error) (Effective, error) {
	return editOverlay(root, home, scope, func(raw map[string]json.RawMessage, effective Effective, options []ConfigurationOption) error {
		selected, err := SelectConfiguration(options, selection)
		if err != nil {
			return err
		}
		configuration := CloneConfiguration(selected.Configuration)
		if err := change(&configuration); err != nil {
			return err
		}
		return storeConfiguration(raw, selected.Name, configuration)
	})
}

func UseConfiguration(root, home, scope, selection string) (Effective, error) {
	return editOverlay(root, home, scope, func(raw map[string]json.RawMessage, effective Effective, options []ConfigurationOption) error {
		selected, err := SelectConfiguration(options, selection)
		if err != nil {
			return err
		}
		path := effective.Files[1]
		if scope == "user" {
			path = effective.Files[0]
		}
		if selected.Source != "default" && selected.Source != path {
			if err := storeConfiguration(raw, selected.Name, selected.Configuration); err != nil {
				return err
			}
		}
		raw["configuration"], err = json.Marshal(selected.Name)
		return err
	})
}

func DeleteConfiguration(root, home, scope, selection string) (Effective, error) {
	return editOverlay(root, home, scope, func(raw map[string]json.RawMessage, effective Effective, options []ConfigurationOption) error {
		selected, err := SelectConfiguration(options, selection)
		if err != nil {
			return err
		}
		named, err := configurationMap(raw)
		if err != nil {
			return err
		}
		if _, present := named[selected.Name]; !present {
			return fmt.Errorf("configuration %q is not saved in the %s scope", selected.Name, scope)
		}
		delete(named, selected.Name)
		raw["configurations"], err = json.Marshal(named)
		if err != nil {
			return err
		}
		var active string
		if data, present := raw["configuration"]; present {
			if err := json.Unmarshal(data, &active); err != nil {
				return err
			}
			if active == selected.Name {
				raw["configuration"], err = json.Marshal(DefaultConfigurationName)
			}
		}
		return err
	})
}
