package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"formal-gates/internal/engine/facade"
	"formal-gates/internal/harness"
	"formal-gates/internal/host"
	"formal-gates/internal/validate"
)

func confirmSavedFile(path string, confirm func(any) error) (func(any) error, error) {
	if confirm == nil || path == "" || path == "-" {
		return confirm, nil
	}
	original, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return func(value any) error {
		if err := confirm(value); err != nil {
			return err
		}
		current, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if !bytes.Equal(original, current) {
			return fmt.Errorf("confirmation candidate changed while awaiting the host")
		}
		return nil
	}, nil
}

func runWorkflowServe(args []string, streams IO) (int, error) {
	flags := newFlagSet("workflow serve", streams)
	packageRoot := flags.String("package-root", "", "installed formal-gates package root")
	if code, err, done := parseFlagSet(flags, args, streams.Stdout); done {
		return code, err
	}
	controller, err := newPanelController()
	if err != nil {
		return 1, err
	}
	defer controller.Close()
	ctx := streams.Context
	if ctx == nil {
		ctx = context.Background()
	}
	return harness.ServeToolsWithContext(ctx, streams.Stdin, streams.Stdout, streams.Stderr, confirmationTools(), func(callCtx context.Context, name string, raw json.RawMessage, confirm func(any) error, metadata json.RawMessage) (any, error) {
		var context struct {
			Owner *facade.OwnerBinding `json:"formal-gates/owner"`
		}
		if len(metadata) != 0 {
			if err := json.Unmarshal(metadata, &context); err != nil {
				return nil, err
			}
		}
		if context.Owner == nil {
			session, err := host.ConversationSession(metadata)
			if err != nil {
				return nil, err
			}
			if session != "" {
				context.Owner = &facade.OwnerBinding{Session: session}
			}
		}
		current := streams
		current.owner = context.Owner
		current.Context = callCtx
		current.panelCtl = controller
		return callConfirmationTool(name, raw, current, confirm, *packageRoot)
	}, nil), nil
}

func confirmationTools() []any {
	root := map[string]any{"type": "string", "description": "Project directory; defaults to the service's current directory. The confirmation shows its absolute path."}
	position := func(description string) any {
		return map[string]any{"type": "integer", "minimum": 1, "description": description}
	}
	tool := func(name, description string, properties map[string]any, required ...string) any {
		return map[string]any{"name": name, "description": description, "inputSchema": map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}}
	}
	return []any{
		tool("confirm_start", "Request native confirmation of the complete saved intake and start operation. The host's response directly starts the run.", map[string]any{
			"root": root, "intake": position("Original intake position published by workflow start --precheck"),
			"summary":       map[string]any{"type": "string", "description": "One-sentence human-readable description of the operation being confirmed; hosts may show it in their permission prompt."},
			"route":         map[string]any{"type": "string", "enum": []string{"regular", "lightweight"}},
			"baseSnapshot":  map[string]any{"type": "string", "description": "User-specified native base revision or immutable identity for a regular run; the program validates and displays it. Omitted uses the current native snapshot; lightweight ignores it."},
			"vcs":           map[string]any{"type": "string", "description": "Native VCS name, using the existing workflow start selection when omitted."},
			"configuration": map[string]any{"type": "string", "description": "Saved configuration name or displayed position"},
		}, "intake"),
		tool("confirm_control", "Request native confirmation of an original saved control candidate. The host directly submits that exact candidate; no approval is returned for the agent to submit.", map[string]any{
			"root": root, "run": position("Original top-level run position; may be omitted when only one run exists"),
			"summary":   map[string]any{"type": "string", "description": "One-sentence human-readable description of the operation being confirmed; hosts may show it in their permission prompt."},
			"candidate": position("Original confirmationCandidate position published by workflow next or replan"),
		}, "candidate"),
		tool("confirm_native", "Request native confirmation and delivery of an original saved answer to the displayed pending host interaction.", map[string]any{
			"root": root, "run": position("Original top-level run position; may be omitted when only one run exists"),
			"summary":  map[string]any{"type": "string", "description": "One-sentence human-readable description of the operation being confirmed; hosts may show it in their permission prompt."},
			"child":    position("Original child position published by workflow show; omitted selects the master"),
			"question": position("Original pending question position published by native show"),
			"decision": position("Original saved decision position published by native respond"),
		}, "question", "decision"),
		tool("configure", "Open the flow configuration panel and wait for its result. Before intake selection, tell the user that this graphical panel is available now and later: ask the agent to call configure, or run formal-gates config in a terminal; disabling panelSelection switches selection to conversation. With intake, selected/timeout returns a saved configuration and derived route for confirm_start; the panel does not approve startup. Never call confirm_start after cancelled or superseded. If disabled/unavailable, explain the reason and offer equivalent settings/conversation choices. During a run, edit its bound configuration; no rebinding. Mention the panel alongside existing approval/blockage/progress/completion messages, not in a separate unsolicited reminder.", map[string]any{
			"root":   map[string]any{"type": "string", "description": "Project directory; defaults to the service's current directory."},
			"intake": position("Original intake position published by workflow start --precheck"),
			"force":  map[string]any{"type": "boolean", "description": "Open the panel even when panelSelection is disabled"},
		}),
	}
}

func callConfirmationTool(name string, raw json.RawMessage, streams IO, confirm func(any) error, packageRoot string) (any, error) {
	var output, errors bytes.Buffer
	invocation := IO{Stdin: bytes.NewReader(nil), Stdout: &output, Stderr: &errors, Context: streams.Context, confirmation: confirm, owner: streams.owner}
	// The serve's own installed package selects the host and its confirmation
	// channel. A permission-gate host decides before the call arrives, so the
	// arrived call is confirmed without an interactive form.
	if admission, err := validate.ResolveEngineAdmission("", packageRoot); err == nil {
		if descriptor, derr := host.Lookup(admission.Provider); derr == nil {
			invocation.confirmationChannel = descriptor.Confirmation
		}
	}
	if invocation.confirmationChannel == host.ConfirmationHostPermission {
		invocation.confirmation = func(any) error { return nil }
	}
	rootPath := func(root string) (string, error) {
		if root == "" {
			root = "."
		}
		return filepath.Abs(root)
	}
	var args []string
	var execute func([]string, IO) (int, error)
	switch name {
	case "configure":
		var input struct {
			Root   string `json:"root"`
			Intake *int   `json:"intake"`
			Force  bool   `json:"force"`
		}
		if err := harness.StrictDecode(raw, &input); err != nil {
			return nil, err
		}
		intake := 0
		if input.Intake != nil {
			// Positions are 1-based; an explicit zero is an invalid selection,
			// not an omitted library open.
			if *input.Intake < 1 {
				return nil, fmt.Errorf("select an original intake position")
			}
			intake = *input.Intake
		}
		root, err := rootPath(input.Root)
		if err != nil {
			return nil, err
		}
		if streams.panelCtl == nil {
			return nil, fmt.Errorf("panel controller unavailable")
		}
		callCtx := invocation.Context
		if callCtx == nil {
			callCtx = context.Background()
		}
		result, err := streams.panelCtl.configure(callCtx, root, intake, input.Force)
		if err != nil {
			return nil, err
		}
		return result, nil
	case "confirm_start":
		var input struct {
			Root          string `json:"root"`
			Intake        int    `json:"intake"`
			Summary       string `json:"summary"`
			Route         string `json:"route"`
			BaseSnapshot  string `json:"baseSnapshot"`
			VCS           string `json:"vcs"`
			Configuration string `json:"configuration"`
		}
		if err := harness.StrictDecode(raw, &input); err != nil {
			return nil, err
		}
		if input.Intake < 1 {
			return nil, fmt.Errorf("select an original intake position")
		}
		root, err := rootPath(input.Root)
		if err != nil {
			return nil, err
		}
		var identity [16]byte
		if _, err := rand.Read(identity[:]); err != nil {
			return nil, err
		}
		args = []string{"--root", root, "--run-id", fmt.Sprintf("run-%x", identity), "--intake", strconv.Itoa(input.Intake)}
		if input.Route != "" {
			args = append(args, "--route", input.Route)
		}
		if input.BaseSnapshot != "" {
			args = append(args, "--base-snapshot", input.BaseSnapshot)
		}
		if input.VCS != "" {
			args = append(args, "--vcs", input.VCS)
		}
		if input.Configuration != "" {
			args = append(args, "--configuration", input.Configuration)
		}
		if packageRoot != "" {
			args = append(args, "--package-root", packageRoot)
		}
		execute = runWorkflowStart
	case "confirm_control":
		var input struct {
			Root      string `json:"root"`
			Run       int    `json:"run"`
			Summary   string `json:"summary"`
			Candidate int    `json:"candidate"`
		}
		if err := harness.StrictDecode(raw, &input); err != nil {
			return nil, err
		}
		if input.Candidate < 1 {
			return nil, fmt.Errorf("select an original control candidate position")
		}
		root, err := rootPath(input.Root)
		if err != nil {
			return nil, err
		}
		args = []string{"--root", root, "--run", strconv.Itoa(input.Run), "--confirmation-candidate", strconv.Itoa(input.Candidate), "--single-step"}
		execute = runWorkflowSubmit
	case "confirm_native":
		var input struct {
			Root     string `json:"root"`
			Run      int    `json:"run"`
			Summary  string `json:"summary"`
			Child    int    `json:"child"`
			Question int    `json:"question"`
			Decision int    `json:"decision"`
		}
		if err := harness.StrictDecode(raw, &input); err != nil {
			return nil, err
		}
		if input.Question < 1 || input.Decision < 1 || input.Child < 0 {
			return nil, fmt.Errorf("select original native question, decision and child positions")
		}
		root, err := rootPath(input.Root)
		if err != nil {
			return nil, err
		}
		args = []string{"respond", "--root", root, "--question", strconv.Itoa(input.Question), "--decision", strconv.Itoa(input.Decision)}
		if input.Child == 0 {
			args = append(args, "--run", strconv.Itoa(input.Run))
		} else {
			id := ""
			owner, selected, err := selectWorkflowRun(root, &id, input.Run, &output)
			if err != nil {
				return nil, err
			}
			if !selected || owner == nil {
				return nil, fmt.Errorf("select a current top-level run")
			}
			runs, err := owner.ControlRuns()
			if err != nil {
				return nil, err
			}
			if input.Child >= len(runs) {
				return nil, fmt.Errorf("select a current child position")
			}
			args = append(args, "--run-id", runs[input.Child].RunID)
		}
		execute = runNativeInteraction
	default:
		return nil, fmt.Errorf("unknown confirmation tool %q", name)
	}
	code, err := execute(args, invocation)
	if err != nil {
		return nil, err
	}
	if code != 0 {
		return nil, fmt.Errorf("confirmation was not executed: %s", errors.String())
	}
	if json.Valid(output.Bytes()) {
		return json.RawMessage(output.Bytes()), nil
	}
	return map[string]any{"output": output.String()}, nil
}
