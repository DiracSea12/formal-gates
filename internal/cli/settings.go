package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/prompts"
	"formal-gates/internal/settings"
	"formal-gates/internal/validate"
)

func runSettings(args []string, streams IO) (int, error) {
	command := "show"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		command, args = args[0], args[1:]
	}
	scalarName, scalarValue := "", ""
	if (command == "set" || command == "remove") && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		scalarName, args = args[0], args[1:]
		if command == "set" && len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			scalarValue, args = args[0], args[1:]
		}
	}
	fs := flag.NewFlagSet("settings "+command, flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	root := fs.String("root", ".", "project root")
	home, homeErr := os.UserHomeDir()
	userHome := fs.String("home", home, "user configuration home")
	scope := fs.String("scope", "project", "project or user overlay")
	field := fs.Int("field", 0, "published scalar field position")
	value := fs.String("value", "", "new scalar value; stdin when omitted")
	configuration := fs.String("configuration", "", "configuration name or displayed position; omitted selects the active configuration")
	name := fs.String("name", "", "new name for save or rename; omitted on save generates a unique copy name")
	roundLimit := fs.String("round-limit", "", "round limit name or displayed position; set takes --value 1..64, remove restores the default")
	var revisionFlags stringListFlag
	fs.Var(&revisionFlags, "revision", "scope=sha256 overlay revision from settings show; a changed file rejects the write; repeatable")
	flowNode := fs.String("flow-node", "", "flow-node name or displayed position")
	qaMode := fs.String("qa-mode", "", "QA group name or displayed position")
	enabled := fs.String("enabled", "", "true or false for the selected flow node or QA group")
	listGates := fs.Bool("gates", false, "show ordinary gate choices from the registered runtime")
	allGates := fs.Bool("all-gates", false, "select all discovered ordinary gates")
	noGates := fs.Bool("no-gates", false, "select no ordinary gates")
	var gates stringListFlag
	fs.Var(&gates, "gate", "ordinary gate name or displayed position; repeatable")
	execution := facade.ExecutionSelectionInput{}
	fs.StringVar(&execution.Node, "execution-node", "", "execution group name or displayed position")
	fs.StringVar(&execution.Platform, "platform", "", "native platform name or displayed position")
	fs.StringVar(&execution.Model, "model", "", "native model name, label or displayed position")
	fs.StringVar(&execution.Effort, "effort", "", "native effort name or displayed position")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	if homeErr != nil && *userHome == "" {
		return 1, homeErr
	}
	supplied := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { supplied[f.Name] = true })
	if scalarName != "" {
		if *field != 0 {
			return 1, fmt.Errorf("select a field by name or by --field")
		}
		effective, err := settings.Load(*root, *userHome)
		if err != nil {
			return 1, err
		}
		fields, err := settings.EditableFields(effective)
		if err != nil {
			return 1, err
		}
		for _, option := range fields {
			if option.Name == scalarName {
				*field = option.Position
			}
		}
		if *field == 0 {
			return 1, fmt.Errorf("unknown settings field %q", scalarName)
		}
		if scalarValue != "" {
			*value, supplied["value"] = scalarValue, true
		}
	}
	if execution.Node == "" && (supplied["platform"] || supplied["model"] || supplied["effort"]) {
		return 1, fmt.Errorf("--platform, --model and --effort require --execution-node")
	}
	if supplied["enabled"] && *flowNode == "" && *qaMode == "" {
		return 1, fmt.Errorf("--enabled requires --flow-node or --qa-mode")
	}
	var expected map[string]string
	for _, entry := range revisionFlags {
		revisionScope, revision, ok := strings.Cut(entry, "=")
		if !ok {
			return 1, fmt.Errorf("--revision must be scope=sha256")
		}
		if expected == nil {
			expected = map[string]string{}
		}
		expected[revisionScope] = revision
	}
	profileEdit := *flowNode != "" || *qaMode != "" || execution.Node != "" || *roundLimit != "" || *allGates || *noGates || len(gates) != 0
	if profileEdit && command != "set" && command != "remove" && command != "execution-options" {
		return 1, fmt.Errorf("workflow choices are changed with settings set or remove")
	}
	if *field != 0 && profileEdit {
		return 1, fmt.Errorf("select a scalar field or a workflow configuration setting")
	}
	if (command == "set" || command == "remove") && !profileEdit {
		if *field == 0 {
			return 1, fmt.Errorf("select a published --field or workflow configuration setting")
		}
		if command == "set" && !supplied["value"] {
			data, err := io.ReadAll(streams.Stdin)
			if err != nil {
				return 1, err
			}
			*value = string(data)
		}
		if _, err := settings.EditField(*root, *userHome, *scope, *field, *value, command == "remove", expected); err != nil {
			return 1, err
		}
	} else {
		switch command {
		case "show":
		case "save":
			if supplied["name"] && strings.TrimSpace(*name) == "" {
				return 1, fmt.Errorf("configuration name must be nonempty")
			}
			created, err := settings.SaveConfiguration(*root, *userHome, *scope, *name, *configuration, expected)
			if err != nil {
				return 1, err
			}
			*configuration = created
		case "use":
			if _, err := settings.UseConfiguration(*root, *userHome, *scope, *configuration, expected); err != nil {
				return 1, err
			}
		case "delete", "rename":
			resolveBindings := func() (map[string][]string, error) { return facade.BoundConfigurations(*root) }
			var err error
			if command == "delete" {
				_, err = settings.DeleteConfiguration(*root, *userHome, *scope, *configuration, resolveBindings, expected)
				*configuration = ""
			} else {
				_, err = settings.RenameConfiguration(*root, *userHome, *scope, *configuration, *name, resolveBindings, expected)
				*configuration = *name
			}
			if err != nil {
				return 1, err
			}
		case "set", "remove":
			categories := 0
			for _, present := range []bool{*flowNode != "", *qaMode != "", execution.Node != "", *roundLimit != "", *allGates || *noGates || len(gates) != 0} {
				if present {
					categories++
				}
			}
			if categories != 1 {
				return 1, fmt.Errorf("select one flow node, QA group, execution group, round limit or ordinary gate set")
			}
			if command == "remove" && execution.Node == "" && *roundLimit == "" {
				return 1, fmt.Errorf("settings remove accepts a scalar field, an execution group to inherit or a round limit to restore its default")
			}
			_, err := settings.EditConfiguration(*root, *userHome, *scope, *configuration, expected, func(selected *settings.Configuration) error {
				if *flowNode != "" || *qaMode != "" {
					on, err := strconv.ParseBool(*enabled)
					if err != nil {
						return fmt.Errorf("--enabled must be true or false")
					}
					if *flowNode != "" {
						node, err := settingsNode(definition.MainFlowNodes(), *flowNode)
						if err != nil {
							return err
						}
						selected.SetFlowNodeEnabled(node.ID, on)
					} else {
						mode, err := settingsChoice([]string{prompts.BlackboxQAID, prompts.WhiteboxQAID}, *qaMode, "QA group")
						if err != nil {
							return err
						}
						selected.SetQAMode(mode, on)
					}
					return nil
				}
				if execution.Node != "" {
					node, err := settingsNode(definition.ExecutionNodes(), execution.Node)
					if err != nil {
						return err
					}
					if command == "remove" {
						delete(selected.Execution, node.ID)
						return nil
					}
					for flag, value := range map[string]string{"platform": execution.Platform, "model": execution.Model, "effort": execution.Effort} {
						if supplied[flag] && value == "" {
							return fmt.Errorf("--%s requires a value; use settings remove --execution-node to inherit", flag)
						}
					}
					options, native, current, err := settingsExecutionContext(*root, *userHome, *selected, node.ID)
					if err != nil {
						return err
					}
					choice, err := facade.PrepareExecutionSelection(*root, options.Target, native, current, execution)
					if err != nil {
						return err
					}
					if selected.Execution == nil {
						selected.Execution = map[string]settings.ExecutionChoice{}
					}
					selected.Execution[node.ID] = choice.Choice
					return nil
				}
				if *roundLimit != "" {
					node, err := settingsChoice(settings.RoundLimitNodes, *roundLimit, "round limit")
					if err != nil {
						return err
					}
					if command == "remove" {
						delete(selected.RoundLimits, node)
						return nil
					}
					limit, err := strconv.Atoi(strings.TrimSpace(*value))
					if err != nil {
						return fmt.Errorf("--value must be an integer from 1 to 64")
					}
					if selected.RoundLimits == nil {
						selected.RoundLimits = map[string]int{}
					}
					selected.RoundLimits[node] = limit
					return nil
				}
				if *allGates && *noGates || (*allGates || *noGates) && len(gates) != 0 {
					return fmt.Errorf("select --all-gates, --no-gates or explicit --gate choices")
				}
				if *allGates {
					selected.Gates = nil
					return nil
				}
				chosen := []string{}
				if len(gates) != 0 {
					available, err := settingsGates(*root)
					if err != nil {
						return err
					}
					for _, input := range gates {
						gate, err := settingsChoice(available, input, "ordinary gate")
						if err != nil {
							return err
						}
						chosen = append(chosen, gate)
					}
				}
				selected.Gates = &chosen
				return nil
			})
			if err != nil {
				return 1, err
			}
		case "execution-options":
			effective, options, err := settings.LoadConfigurations(*root, *userHome)
			if err != nil {
				return 1, err
			}
			selected, err := settings.SelectConfiguration(options, *configuration)
			if err != nil {
				return 1, err
			}
			node, err := settingsNode(definition.ExecutionNodes(), execution.Node)
			if err != nil {
				return 1, err
			}
			admission, err := validate.ResolveEngineAdmission(*root, "")
			if err != nil {
				return 1, err
			}
			current := selected.Configuration.EffectiveExecutionChoice(node.ID, settings.ExecutionChoice{Provider: admission.Provider, Model: effective.Values.Model, ReasoningEffort: effective.Values.ReasoningEffort})
			queried, err := facade.QueryExecutionOptions(*root, admission.Target, effective.Values.Native, current, execution)
			if err != nil {
				return 1, err
			}
			return printJSON(streams.Stdout, map[string]any{"configuration": selected.Name, "options": queried, "platforms": numberedChoices(queried.Platforms), "models": numberedModels(queried)})
		default:
			return 1, fmt.Errorf("settings command must be show, save, set, remove, use, rename, delete or execution-options")
		}
	}
	return printSettings(*root, *userHome, *configuration, *listGates, streams)
}

func settingsNode(nodes []definition.FlowNode, value string) (definition.FlowNode, error) {
	for _, node := range nodes {
		if value == node.ID || value == node.Description {
			return node, nil
		}
	}
	if n, err := strconv.Atoi(value); err == nil && n >= 1 && n <= len(nodes) {
		return nodes[n-1], nil
	}
	return definition.FlowNode{}, fmt.Errorf("unknown node %q; select its name or position from settings show", value)
}

func settingsChoice(choices []string, input, label string) (string, error) {
	if slices.Contains(choices, input) {
		return input, nil
	}
	if n, err := strconv.Atoi(input); err == nil && n >= 1 && n <= len(choices) {
		return choices[n-1], nil
	}
	return "", fmt.Errorf("unknown %s %q (available: %v)", label, input, choices)
}

func settingsExecutionContext(root, home string, configuration settings.Configuration, node string) (validate.EngineAdmission, map[string]string, settings.ExecutionChoice, error) {
	effective, err := settings.Load(root, home)
	if err != nil {
		return validate.EngineAdmission{}, nil, settings.ExecutionChoice{}, err
	}
	admission, err := validate.ResolveEngineAdmission(root, "")
	if err != nil {
		return admission, nil, settings.ExecutionChoice{}, err
	}
	return admission, effective.Values.Native, configuration.EffectiveExecutionChoice(node, settings.ExecutionChoice{Provider: admission.Provider, Model: effective.Values.Model, ReasoningEffort: effective.Values.ReasoningEffort}), nil
}

func settingsGates(root string) ([]string, error) {
	admission, err := validate.ResolveEngineAdmission(root, "")
	if err != nil {
		return nil, err
	}
	candidates, err := prompts.PackageRouteCandidates(admission.Target)
	if err != nil {
		return nil, err
	}
	return slices.DeleteFunc(candidates, func(gate string) bool { return gate == prompts.BlackboxQAID || gate == prompts.WhiteboxQAID }), nil
}

func numberedModels(options facade.ExecutionOptions) []map[string]any {
	models := []map[string]any{}
	for i, model := range options.Catalogue.Models {
		models = append(models, map[string]any{"model": i + 1, "name": model.Value, "label": model.Label, "efforts": numberedChoices(model.Efforts), "effortsReported": model.Efforts != nil, "defaultEffort": model.DefaultEffort})
	}
	return models
}

func printSettings(root, home, selection string, listGates bool, streams IO) (int, error) {
	effective, options, err := settings.LoadConfigurations(root, home)
	if err != nil {
		return 1, err
	}
	selected, err := settings.SelectConfiguration(options, selection)
	if err != nil {
		return 1, err
	}
	fields, err := settings.EditableFields(effective)
	if err != nil {
		return 1, err
	}
	flow := []map[string]any{}
	for i, node := range definition.MainFlowNodes() {
		enabled := selected.Configuration.FlowSelection == nil || !slices.Contains(selected.Configuration.FlowSelection.Disabled, node.ID)
		flow = append(flow, map[string]any{"node": i + 1, "name": node.ID, "description": node.Description, "enabled": enabled})
	}
	qa := []map[string]any{}
	for i, mode := range []string{prompts.BlackboxQAID, prompts.WhiteboxQAID} {
		qa = append(qa, map[string]any{"mode": i + 1, "name": mode, "enabled": selected.Configuration.QAModeEnabled(mode)})
	}
	execution := []map[string]any{}
	for i, node := range definition.ExecutionNodes() {
		execution = append(execution, map[string]any{"node": i + 1, "name": node.ID, "description": node.Description, "execution": selected.Configuration.Execution[node.ID]})
	}
	roundLimits := []map[string]any{}
	for i, node := range settings.RoundLimitNodes {
		_, saved := selected.Configuration.RoundLimits[node]
		roundLimits = append(roundLimits, map[string]any{"limit": i + 1, "name": node, "value": selected.Configuration.RoundLimit(node), "default": !saved})
	}
	view := map[string]any{"effective": effective, "fields": fields, "configurations": options, "configuration": selected.Name, "flowNodes": flow, "qaModes": qa, "executionNodes": execution, "roundLimits": roundLimits, "revisions": effective.Revisions}
	if listGates {
		available, err := settingsGates(root)
		if err != nil {
			return 1, err
		}
		view["gates"] = numberedChoices(available)
	}
	return printJSON(streams.Stdout, view)
}
