package facade

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/host"
)

// ExecutionSelectionInput names current choices by their displayed position or
// semantic value. Omitted fields inherit the effective choice on the same platform.
type ExecutionSelectionInput struct {
	Node     string `json:"node"`
	Platform string `json:"platform,omitempty"`
	Model    string `json:"model,omitempty"`
	Effort   string `json:"effort,omitempty"`
}

type ExecutionOptions struct {
	Node      definition.FlowNode      `json:"node"`
	Effective protocol.ExecutionChoice `json:"effective"`
	Platforms []string                 `json:"platforms"`
	Platform  string                   `json:"platform"`
	// Unavailable marks a platform with no execution-selection adapter; it is
	// a state of the saved/effective value, not a query failure, so the
	// catalogue stays empty. Host marks the unavailable platform whose name
	// still resolves to a registered descriptor: the lookup succeeded but
	// its adapter is not a SelectionAdapter.
	Unavailable bool                    `json:"unavailable,omitempty"`
	Host        bool                    `json:"host,omitempty"`
	Catalogue   host.SelectionCatalogue `json:"catalogue"`
	// Installed reports, per execution platform name, whether its own
	// executable resolution succeeds on PATH. The API surface fills it; the
	// catalogue query itself never probes.
	Installed map[string]bool `json:"installed,omitempty"`
}

func executionNode(value string) (definition.FlowNode, error) {
	nodes := definition.ExecutionNodes()
	if n, err := strconv.Atoi(value); err == nil {
		return position(nodes, n, "执行节点")
	}
	for _, node := range nodes {
		if value == node.ID || value == node.Description {
			return node, nil
		}
	}
	return definition.FlowNode{}, fmt.Errorf("unknown execution node %q (available: %v)", value, protocol.ExecutionNodeIDs())
}

func selectionPlatform(value string) (host.Descriptor, error) {
	if n, err := strconv.Atoi(value); err == nil {
		name, err := position(executionPlatforms(), n, "platform")
		if err != nil {
			return host.Descriptor{}, fmt.Errorf("%w (available: %v)", err, executionPlatforms())
		}
		return host.Lookup(name)
	}
	descriptor, err := host.Lookup(value)
	if err != nil {
		return descriptor, fmt.Errorf("%w (available: %v)", err, executionPlatforms())
	}
	return descriptor, nil
}

func executionPlatforms() []string {
	var names []string
	for _, adapter := range host.RegisteredAdapters() {
		if _, ok := adapter.(host.SelectionAdapter); ok {
			descriptor := adapter.HostDescriptor()
			name := descriptor.InstallName
			if name == "" {
				name = descriptor.ID
			}
			names = append(names, name)
		}
	}
	return names
}

func selectionCatalogue(root, runtimeTarget string, native map[string]string, provider, model string) (host.SelectionCatalogue, error) {
	adapter, err := host.LookupAdapter(provider)
	if err != nil {
		return host.SelectionCatalogue{}, err
	}
	options, ok := adapter.(host.SelectionAdapter)
	if !ok {
		return host.SelectionCatalogue{}, fmt.Errorf("platform %q has no execution selection adapter (available: %v)", provider, executionPlatforms())
	}
	return options.SelectionOptions(root, runtimeTarget, native, model)
}

func selectionModel(catalogue host.SelectionCatalogue, value string) (host.ModelOption, error) {
	for _, option := range catalogue.Models {
		if option.Value == value {
			return option, nil
		}
	}
	if n, err := strconv.Atoi(value); err == nil {
		option, err := position(catalogue.Models, n, "model")
		if err != nil {
			return option, fmt.Errorf("%w (available: %v)", err, catalogue.Models)
		}
		return option, nil
	}
	var matches []host.ModelOption
	for _, option := range catalogue.Models {
		if strings.EqualFold(option.Value, value) || strings.EqualFold(option.Label, value) {
			matches = append(matches, option)
		}
	}
	if len(matches) == 1 {
		return matches[0], nil
	}
	return host.ModelOption{}, fmt.Errorf("model %q is not an unambiguous native option (available: %v)", value, catalogue.Models)
}

// QueryExecutionOptions reads the native catalogue for configuration editing.
// Current carries the caller's effective tuple.
func QueryExecutionOptions(root, runtimeTarget string, native map[string]string, current protocol.ExecutionChoice, input ExecutionSelectionInput) (ExecutionOptions, error) {
	node, err := executionNode(strings.TrimSpace(input.Node))
	if err != nil {
		return ExecutionOptions{}, err
	}
	effective := current
	platform := effective.Provider
	if input.Platform != "" {
		descriptor, err := selectionPlatform(input.Platform)
		if err != nil {
			return ExecutionOptions{}, err
		}
		platform = descriptor.ID
	}
	model := effective.Model
	if platform != effective.Provider {
		model = ""
	}
	if adapter, err := host.LookupAdapter(platform); err != nil {
		return ExecutionOptions{Node: node, Effective: effective, Platforms: executionPlatforms(), Platform: platform, Unavailable: true}, nil
	} else if _, ok := adapter.(host.SelectionAdapter); !ok {
		return ExecutionOptions{Node: node, Effective: effective, Platforms: executionPlatforms(), Platform: platform, Unavailable: true, Host: true}, nil
	}
	catalogue, err := selectionCatalogue(root, runtimeTarget, native, platform, model)
	if err != nil {
		return ExecutionOptions{}, err
	}
	if input.Model != "" {
		option, err := selectionModel(catalogue, input.Model)
		if err != nil {
			return ExecutionOptions{}, err
		}
		if option.Value != model {
			catalogue, err = selectionCatalogue(root, runtimeTarget, native, platform, option.Value)
			if err != nil {
				return ExecutionOptions{}, err
			}
		}
	}
	return ExecutionOptions{Node: node, Effective: effective, Platforms: executionPlatforms(), Platform: platform, Catalogue: catalogue}, nil
}

func (f *Facade) executionContext(input ExecutionSelectionInput) (protocol.ExecutionChoice, map[string]string, error) {
	node, err := executionNode(strings.TrimSpace(input.Node))
	if err != nil {
		return protocol.ExecutionChoice{}, nil, err
	}
	snap, err := f.engine.Load()
	if err != nil {
		return protocol.ExecutionChoice{}, nil, err
	}
	var native map[string]string
	if snap.State.Settings != nil {
		native = snap.State.Settings.Values.Native
	}
	return snap.State.EffectiveExecutionChoice(node.ID), native, nil
}

func (f *Facade) executionOptions(input ExecutionSelectionInput) (ExecutionOptions, error) {
	current, native, err := f.executionContext(input)
	if err != nil {
		return ExecutionOptions{}, err
	}
	return QueryExecutionOptions(f.root, f.admission.Target, native, current, input)
}

// QueryExecutionOptions reads native choices without recording a decision.
func (f *Facade) QueryExecutionOptions(input ExecutionSelectionInput) (ExecutionOptions, error) {
	unlock, err := f.acquireControlReadWindow()
	if err != nil {
		return ExecutionOptions{}, err
	}
	defer unlock()
	return f.executionOptions(input)
}

// ExecutionSelection is a native choice for one configuration node.
type ExecutionSelection struct {
	Node   string                   `json:"node"`
	Choice protocol.ExecutionChoice `json:"choice"`
}

// PrepareExecutionSelection resolves and validates a tuple against native choices.
func PrepareExecutionSelection(root, runtimeTarget string, native map[string]string, current protocol.ExecutionChoice, input ExecutionSelectionInput) (*ExecutionSelection, error) {
	if input.Platform == "" && input.Model == "" && input.Effort == "" {
		return nil, fmt.Errorf("execution selection requires a platform, model or effort")
	}
	options, err := QueryExecutionOptions(root, runtimeTarget, native, current, input)
	if err != nil {
		return nil, err
	}
	if options.Unavailable {
		return nil, fmt.Errorf("platform %q has no execution selection adapter (available: %v)", options.Platform, options.Platforms)
	}
	choice := options.Effective
	if options.Platform != choice.Provider {
		choice = protocol.ExecutionChoice{Provider: options.Platform}
	}
	if input.Model != "" {
		model, err := selectionModel(options.Catalogue, input.Model)
		if err != nil {
			return nil, err
		}
		choice.Model = model.Value
	}
	if input.Effort != "" {
		choice.ReasoningEffort = input.Effort
		if n, err := strconv.Atoi(input.Effort); err == nil {
			model := choice.Model
			if model == "" {
				model = options.Catalogue.Current.Model
			}
			if model == "" {
				model = options.Catalogue.Default.Model
			}
			option, err := selectionModel(options.Catalogue, model)
			if err != nil {
				return nil, err
			}
			if len(option.Efforts) == 0 {
				return nil, validateExecutionChoice(choice, options.Catalogue)
			}
			choice.ReasoningEffort, err = position(option.Efforts, n, "effort")
			if err != nil {
				return nil, fmt.Errorf("%w (native options: %v)", err, option.Efforts)
			}
		}
	}
	if err := validateExecutionChoice(choice, options.Catalogue); err != nil {
		return nil, err
	}
	return &ExecutionSelection{Node: options.Node.ID, Choice: choice}, nil
}

func validateExecutionChoice(choice protocol.ExecutionChoice, catalogue host.SelectionCatalogue) error {
	if choice.Model == "" && choice.ReasoningEffort == "" {
		return nil
	}
	model := choice.Model
	if model == "" {
		model = catalogue.Current.Model
	}
	if model == "" {
		model = catalogue.Default.Model
	}
	if model != "" {
		option, err := selectionModel(catalogue, model)
		if err != nil {
			return err
		}
		if choice.ReasoningEffort != "" {
			switch {
			case option.Efforts == nil:
				return fmt.Errorf("native platform %q did not report effort options for model %q; cannot verify effort %q", choice.Provider, option.Value, choice.ReasoningEffort)
			case len(option.Efforts) == 0:
				return fmt.Errorf("model %q has no selectable reasoning effort (native options: [])", option.Value)
			case !slices.Contains(option.Efforts, choice.ReasoningEffort):
				return fmt.Errorf("effort %q is unavailable for model %q (native options: %v)", choice.ReasoningEffort, option.Value, option.Efforts)
			}
		}
	} else if choice.ReasoningEffort != "" {
		return fmt.Errorf("native platform %q did not report a model or effort options", choice.Provider)
	}
	return nil
}
