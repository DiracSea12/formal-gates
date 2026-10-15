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
	Catalogue host.SelectionCatalogue  `json:"catalogue"`
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

func (f *Facade) selectionCatalogue(state *protocol.State, provider, model string) (host.SelectionCatalogue, error) {
	adapter, err := host.LookupAdapter(provider)
	if err != nil {
		return host.SelectionCatalogue{}, err
	}
	options, ok := adapter.(host.SelectionAdapter)
	if !ok {
		return host.SelectionCatalogue{}, fmt.Errorf("platform %q has no execution selection adapter (available: %v)", provider, executionPlatforms())
	}
	var native map[string]string
	if state.Settings != nil {
		native = state.Settings.Values.Native
	}
	return options.SelectionOptions(f.root, f.admission.Target, native, model)
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

func (f *Facade) executionOptions(input ExecutionSelectionInput) (ExecutionOptions, error) {
	node, err := executionNode(strings.TrimSpace(input.Node))
	if err != nil {
		return ExecutionOptions{}, err
	}
	snap, err := f.engine.Load()
	if err != nil {
		return ExecutionOptions{}, err
	}
	effective := snap.State.EffectiveExecutionChoice(node.ID)
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
	catalogue, err := f.selectionCatalogue(snap.State, platform, model)
	if err != nil {
		return ExecutionOptions{}, err
	}
	if input.Model != "" {
		option, err := selectionModel(catalogue, input.Model)
		if err != nil {
			return ExecutionOptions{}, err
		}
		if option.Value != model {
			catalogue, err = f.selectionCatalogue(snap.State, platform, option.Value)
			if err != nil {
				return ExecutionOptions{}, err
			}
		}
	}
	return ExecutionOptions{Node: node, Effective: effective, Platforms: executionPlatforms(), Platform: platform, Catalogue: catalogue}, nil
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

func (f *Facade) prepareExecutionSelection(input ExecutionSelectionInput) (*protocol.ExecutionSelection, error) {
	if input.Platform == "" && input.Model == "" && input.Effort == "" {
		return nil, fmt.Errorf("execution selection requires a platform, model or effort")
	}
	options, err := f.executionOptions(input)
	if err != nil {
		return nil, err
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
	return &protocol.ExecutionSelection{Node: options.Node.ID, Choice: choice}, nil
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

func (f *Facade) validateExecutionSelection(selection *protocol.ExecutionSelection) error {
	snap, err := f.engine.Load()
	if err != nil {
		return err
	}
	catalogue, err := f.selectionCatalogue(snap.State, selection.Choice.Provider, selection.Choice.Model)
	if err != nil {
		return err
	}
	return validateExecutionChoice(selection.Choice, catalogue)
}

func (f *Facade) recordedSubmission(event protocol.Event, selectExecution bool) (protocol.Acceptance, bool, error) {
	accepted, recorded, err := f.recordedEvent(event)
	if err != nil || !recorded || !selectExecution {
		return accepted, recorded, err
	}
	snap, err := f.engine.Load()
	if isMissingEngineState(err) {
		summary, summaryErr := f.readTerminalIntent()
		if summaryErr != nil {
			summary, summaryErr = f.readSummary()
		}
		snap.State, err = summary.Termination, summaryErr
	}
	if err != nil {
		return protocol.Acceptance{}, false, err
	}
	decision, recorded := snap.State.Decisions[accepted.RequestID]
	if !recorded {
		return accepted, false, nil
	}
	return snap.State.Events[decision.EventID].Acceptance, true, nil
}

// The user's execution instruction approves the complete prepared tuple. Both
// typed protocol steps share Submit's native window with job ownership.
func (f *Facade) confirmExecutionSelection(event protocol.Event, accepted protocol.Acceptance) (protocol.Acceptance, error) {
	snap, err := f.engine.Load()
	if err != nil {
		return protocol.Acceptance{}, err
	}
	ask, ok := snap.State.PendingAsks[accepted.RequestID]
	if !ok || ask.Execution == nil {
		return protocol.Acceptance{}, fmt.Errorf("execution selection has no current typed Ask")
	}
	token, err := snap.Freshness(protocol.RequestID(accepted.RequestID))
	if err != nil {
		return protocol.Acceptance{}, err
	}
	decision := protocol.Event{ID: protocol.EventID(string(event.ID) + ":approve"), Kind: protocol.KindDecide, Decide: &protocol.DecidePayload{Request: protocol.RequestID(accepted.RequestID), Token: token, Choice: "approve", Execution: &protocol.ExecutionSelectionDecision{Digest: ask.Execution.CanonicalDigest()}}}
	return f.engine.Submit(decision, protocol.Observation{})
}
