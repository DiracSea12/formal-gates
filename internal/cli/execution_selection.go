package cli

import (
	"fmt"

	"formal-gates/internal/engine/facade"
)

func runWorkflowExecutionOptions(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow execution-options", streams)
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "program integration: exact engine run id")
	runChoice := fs.Int("run", 0, "select a run by its displayed number")
	machine := fs.Bool("machine", false, "print the complete native option catalogue")
	input := facade.ExecutionSelectionInput{}
	fs.StringVar(&input.Node, "execution-node", "", "execution group name or displayed execution-node number")
	fs.StringVar(&input.Platform, "platform", "", "native platform name or displayed platform number")
	fs.StringVar(&input.Model, "model", "", "native model name, label or displayed model number")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	f, selected, err := selectWorkflowRun(*root, runID, *runChoice, streams.Stdout)
	if err != nil {
		return 1, err
	}
	if !selected {
		return 0, nil
	}
	if f == nil {
		return 1, fmt.Errorf("current engine run not found; use workflow diagnose")
	}
	options, err := f.QueryExecutionOptions(input)
	if err != nil || *machine {
		return printValue(streams.Stdout, options, err)
	}
	models := []map[string]any{}
	for i, model := range options.Catalogue.Models {
		label := model.Label
		if label == "" {
			label = model.Value
		}
		models = append(models, map[string]any{"model": i + 1, "label": label, "efforts": numberedChoices(model.Efforts), "effortsReported": model.Efforts != nil, "defaultEffort": model.DefaultEffort})
	}
	return printJSON(streams.Stdout, map[string]any{"node": options.Node.Description, "effective": options.Effective, "platform": options.Platform, "platforms": numberedChoices(options.Platforms), "models": models, "nativeDefault": options.Catalogue.Default, "source": options.Catalogue.Source})
}
