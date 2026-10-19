package settings

import (
	"bytes"
	"encoding/json"
	"errors"
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
// An omitted round limit uses DefaultRoundLimit.
type Configuration struct {
	FlowSelection *FlowSelection             `json:"flowSelection,omitempty"`
	QAModes       []string                   `json:"qaModes"`
	Gates         *[]string                  `json:"gates,omitempty"`
	Execution     map[string]ExecutionChoice `json:"execution,omitempty"`
	RoundLimits   map[string]int             `json:"roundLimits,omitempty"`
}

// DefaultConfigurationName is the built-in configuration. It cannot be saved
// over, edited, renamed or deleted; file entries with this name are ignored.
const DefaultConfigurationName = "默认"

// RoundLimitNodes are the responsibilities with a configurable automatic
// round limit.
var RoundLimitNodes = []string{"product-review", "tech-review", "qa-blackbox", "qa-whitebox", "repair"}

const DefaultRoundLimit = 3

// ErrActiveConfigurationMissing reports an active pointer that names no
// configuration in any layer.
var ErrActiveConfigurationMissing = errors.New("does not exist")

var errBuiltinConfiguration = fmt.Errorf("built-in configuration %q cannot be saved over, edited, renamed or deleted; save a named copy with settings save --name", DefaultConfigurationName)

func DefaultConfiguration() Configuration {
	return Configuration{QAModes: []string{}}
}

func (configuration Configuration) QAModeEnabled(mode string) bool {
	return (configuration.FlowSelection == nil || !slices.Contains(configuration.FlowSelection.Disabled, "qa")) && slices.Contains(configuration.QAModes, mode)
}

// SetQAMode changes the effective QA selection and keeps its two persisted
// fields consistent. Disabled QA discards latent modes before editing, so
// enabling one mode cannot silently enable the other. No selected modes
// disables the QA node; selecting any mode enables it.
func (configuration *Configuration) SetQAMode(mode string, enabled bool) {
	if configuration.FlowSelection == nil {
		configuration.FlowSelection = &FlowSelection{}
	}
	if slices.Contains(configuration.FlowSelection.Disabled, "qa") {
		configuration.QAModes = nil
	}
	configuration.QAModes = SetSelection(configuration.QAModes, mode, enabled)
	configuration.FlowSelection.Disabled = SetSelection(configuration.FlowSelection.Disabled, "qa", len(configuration.QAModes) == 0)
}

// SetFlowNodeEnabled treats the QA node as an aggregate switch for both groups.
func (configuration *Configuration) SetFlowNodeEnabled(node string, enabled bool) {
	if node == "qa" {
		for _, mode := range []string{"blackbox", "whitebox"} {
			configuration.SetQAMode(mode, enabled)
		}
		return
	}
	if configuration.FlowSelection == nil {
		configuration.FlowSelection = &FlowSelection{}
	}
	configuration.FlowSelection.Disabled = SetSelection(configuration.FlowSelection.Disabled, node, !enabled)
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

func (configuration Configuration) RoundLimit(node string) int {
	if limit, present := configuration.RoundLimits[node]; present {
		return limit
	}
	return DefaultRoundLimit
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
	for node, limit := range configuration.RoundLimits {
		if !slices.Contains(RoundLimitNodes, node) {
			return fmt.Errorf("unknown round limit %q (available: %v)", node, RoundLimitNodes)
		}
		if limit < 1 || limit > 64 {
			return fmt.Errorf("round limit %s must be 1..64", node)
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
	// Shadowed lists the files whose ignored entries reuse the built-in name.
	Shadowed []string `json:"shadowed,omitempty"`
}

// LoadConfigurations uses the same loader as run creation and field editing.
// Only the selected configuration is included in Effective.
// With ErrActiveConfigurationMissing it still returns the merged options so
// the active pointer can be rewritten.
func LoadConfigurations(root, home string) (Effective, []ConfigurationOption, error) {
	return load(root, home, "", nil)
}

type configurationSet struct {
	values   map[string]Configuration
	sources  map[string]string
	active   string
	shadowed []string
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
			if name == DefaultConfigurationName {
				set.shadowed = append(set.shadowed, path)
				continue
			}
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
	options[0].Shadowed = set.shadowed
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
	configuration.RoundLimits = maps.Clone(configuration.RoundLimits)
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

// SaveConfiguration returns the created name without changing the active pointer.
// An omitted name ("") requests a unique copy name from the locked merged view.
func SaveConfiguration(root, home, scope, name, from string, expected map[string]string) (string, error) {
	if name != "" && strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("configuration name must be nonempty")
	}
	if name == DefaultConfigurationName {
		return "", errBuiltinConfiguration
	}
	_, err := editOverlay(root, home, scope, expected, func(raw map[string]json.RawMessage, effective Effective, options []ConfigurationOption) error {
		selected, err := SelectConfiguration(options, from)
		if err != nil {
			return err
		}
		if name == "" {
			base := selected.Name + " 副本"
			name = base
			for number := 2; slices.ContainsFunc(options, func(option ConfigurationOption) bool { return option.Name == name }); number++ {
				name = fmt.Sprintf("%s %d", base, number)
			}
		} else if slices.ContainsFunc(options, func(option ConfigurationOption) bool { return option.Name == name }) {
			return fmt.Errorf("configuration %q already exists; use settings set to modify it", name)
		}
		return storeConfiguration(raw, name, CloneConfiguration(selected.Configuration))
	})
	if err != nil {
		return "", err
	}
	return name, nil
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
func EditConfiguration(root, home, scope, selection string, expected map[string]string, change func(*Configuration) error) (Effective, error) {
	return editOverlay(root, home, scope, expected, func(raw map[string]json.RawMessage, effective Effective, options []ConfigurationOption) error {
		selected, err := SelectConfiguration(options, selection)
		if err != nil {
			return err
		}
		if selected.Name == DefaultConfigurationName {
			return errBuiltinConfiguration
		}
		configuration := CloneConfiguration(selected.Configuration)
		if err := change(&configuration); err != nil {
			return err
		}
		return storeConfiguration(raw, selected.Name, configuration)
	})
}

// UseConfiguration also repairs a dangling active pointer: its target may be
// any configuration saved in a layer, or the built-in one.
func UseConfiguration(root, home, scope, selection string, expected map[string]string) (Effective, error) {
	return editOverlay(root, home, scope, expected, func(raw map[string]json.RawMessage, effective Effective, options []ConfigurationOption) error {
		selected, err := SelectConfiguration(options, selection)
		if err != nil {
			return err
		}
		raw["configuration"], err = json.Marshal(selected.Name)
		return err
	})
}

// DeleteConfiguration and RenameConfiguration require the selected source scope
// and change only that file. resolveBindings reads unfinished run bindings inside
// the locked edit window; it must not acquire business or writer locks.
func DeleteConfiguration(root, home, scope, selection string, resolveBindings func() (map[string][]string, error), expected map[string]string) (Effective, error) {
	return editOverlay(root, home, scope, expected, func(raw map[string]json.RawMessage, effective Effective, options []ConfigurationOption) error {
		name, named, err := unboundSavedConfiguration(raw, effective, options, scope, selection, resolveBindings)
		if err != nil {
			return err
		}
		delete(named, name)
		if raw["configurations"], err = json.Marshal(named); err != nil {
			return err
		}
		return retargetActive(raw, name, DefaultConfigurationName)
	})
}

func RenameConfiguration(root, home, scope, selection, newName string, resolveBindings func() (map[string][]string, error), expected map[string]string) (Effective, error) {
	if strings.TrimSpace(newName) == "" {
		return Effective{}, fmt.Errorf("new configuration name must be supplied by the user")
	}
	if newName == DefaultConfigurationName {
		return Effective{}, errBuiltinConfiguration
	}
	return editOverlay(root, home, scope, expected, func(raw map[string]json.RawMessage, effective Effective, options []ConfigurationOption) error {
		name, named, err := unboundSavedConfiguration(raw, effective, options, scope, selection, resolveBindings)
		if err != nil {
			return err
		}
		for _, option := range options {
			if option.Name == newName {
				return fmt.Errorf("configuration %q already exists", newName)
			}
		}
		named[newName] = named[name]
		delete(named, name)
		if raw["configurations"], err = json.Marshal(named); err != nil {
			return err
		}
		return retargetActive(raw, name, newName)
	})
}

func unboundSavedConfiguration(raw map[string]json.RawMessage, effective Effective, options []ConfigurationOption, scope, selection string, resolveBindings func() (map[string][]string, error)) (string, map[string]json.RawMessage, error) {
	selected, err := SelectConfiguration(options, selection)
	if err != nil {
		return "", nil, err
	}
	if selected.Name == DefaultConfigurationName {
		return "", nil, errBuiltinConfiguration
	}
	path := effective.Files[1]
	if scope == "user" {
		path = effective.Files[0]
	}
	if selected.Source != path {
		return "", nil, fmt.Errorf("configuration %q source is %s, not the %s scope; select its source scope", selected.Name, selected.Source, scope)
	}
	bound, err := resolveBindings()
	if err != nil {
		return "", nil, err
	}
	if runs := bound[selected.Name]; len(runs) != 0 {
		return "", nil, fmt.Errorf("configuration %q is bound to unfinished runs %s; edit it instead", selected.Name, strings.Join(runs, ", "))
	}
	named, err := configurationMap(raw)
	if err != nil {
		return "", nil, err
	}
	return selected.Name, named, nil
}

// retargetActive keeps this file's active pointer naming an existing value.
func retargetActive(raw map[string]json.RawMessage, from, to string) error {
	data, present := raw["configuration"]
	if !present {
		return nil
	}
	var active string
	if err := json.Unmarshal(data, &active); err != nil || active != from {
		return err
	}
	var err error
	raw["configuration"], err = json.Marshal(to)
	return err
}
