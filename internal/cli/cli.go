package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"formal-gates/internal/validate"
)

type IO struct {
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
}

func Run(program string, args []string, streams IO) int {
	if streams.Stdin == nil {
		streams.Stdin = strings.NewReader("")
	}
	if streams.Stdout == nil {
		streams.Stdout = io.Discard
	}
	if streams.Stderr == nil {
		streams.Stderr = io.Discard
	}

	code, err := run(program, args, streams)
	if err != nil {
		fmt.Fprintln(streams.Stderr, err)
	}
	return code
}

func run(program string, args []string, streams IO) (int, error) {
	command := "package"
	if len(args) > 0 && args[0] != "-h" && args[0] != "--help" && args[0][0] != '-' {
		command = args[0]
		args = args[1:]
	}

	switch command {
	case "package":
		args = dropOptionalVerb(args, "validate")
		fs := flag.NewFlagSet("package", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		root := fs.String("root", ".", "formal-gates package root")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		return printValidationResult(streams.Stdout, "package", validate.Package(*root))
	case "artifact":
		args = dropOptionalVerb(args, "validate")
		fs := flag.NewFlagSet("artifact", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		root := fs.String("root", ".", "repository root for relative artifact references")
		file := fs.String("file", "", "artifact file to validate")
		gate := fs.String("gate", "", "gate id")
		workflowID := fs.String("workflow-id", "", "expected workflow id")
		changeSnapshot := fs.String("change-snapshot", "", "expected change snapshot")
		stage := fs.String("stage", "", "expected QA stage, when relevant")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		return printValidationResult(streams.Stdout, "artifact", validate.Artifact(validate.ArtifactOptions{
			Root:           *root,
			File:           *file,
			Gate:           *gate,
			WorkflowID:     *workflowID,
			ChangeSnapshot: *changeSnapshot,
			Stage:          *stage,
		}))
	case "hook":
		if hasHelpArg(args) {
			printHookUsage(streams.Stdout)
			return 0, nil
		}
		args = dropOptionalVerb(args, "decide")
		if hasHelpArg(args) {
			printHookUsage(streams.Stdout)
			return 0, nil
		}
		if len(args) != 0 {
			return 1, fmt.Errorf("hook decide does not accept positional arguments")
		}
		decision, err := readHookDecision(streams.Stdin)
		if err != nil {
			return 1, err
		}
		encoded, err := json.Marshal(decision)
		if err != nil {
			return 1, err
		}
		fmt.Fprintln(streams.Stdout, string(encoded))
		if decision.PermissionDecision == "deny" {
			return 2, nil
		}
		return 0, nil
	case "prompt":
		args = dropOptionalVerb(args, "validate")
		fs := flag.NewFlagSet("prompt", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		root := fs.String("root", ".", "repository or package root")
		text := fs.String("text", "", "dispatch prompt text")
		file := fs.String("file", "", "file containing dispatch prompt text")
		stdin := fs.Bool("stdin", false, "read dispatch prompt text from stdin")
		patterns := fs.String("patterns", "", "pollution patterns JSON path; defaults to hooks/pollution-patterns.json under --root")
		format := fs.String("format", "text", "output format: text or json")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if *format != "text" && *format != "json" {
			return 1, fmt.Errorf("unsupported --format %q (want text or json)", *format)
		}
		promptText, err := readPromptInput(*text, *file, *stdin, streams.Stdin)
		if err != nil {
			return 1, err
		}
		result, violations := validate.DispatchPromptWithViolations(validate.DispatchPromptOptions{
			Root:       *root,
			PromptText: promptText,
			ConfigPath: *patterns,
		})
		if *format == "json" {
			if !result.OK() && len(violations) == 0 {
				return printValidationResult(streams.Stdout, "prompt", result)
			}
			encoded, err := json.Marshal(violations)
			if err != nil {
				return 1, err
			}
			fmt.Fprintln(streams.Stdout, string(encoded))
			if !result.OK() {
				return 1, nil
			}
			return 0, nil
		}
		return printValidationResult(streams.Stdout, "prompt", result)
	case "install":
		fs := flag.NewFlagSet("install", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		source := fs.String("source", "", "formal-gates source directory")
		host := fs.String("host", "", "target host: claude, codex, cursor, or both")
		scope := fs.String("scope", "", "install scope: global or project")
		project := fs.String("project", "", "project path for project installs, or receipt worktree for global hook config")
		force := fs.Bool("force", false, "replace an existing formal-gates target")
		configureHooks := fs.Bool("configure-hooks", false, "write native host hook configuration")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if fs.NArg() != 0 {
			return 1, fmt.Errorf("install does not accept positional arguments")
		}
		report, err := validate.Install(validate.InstallOptions{
			Source:         *source,
			Host:           *host,
			Scope:          *scope,
			Project:        *project,
			Force:          *force,
			ConfigureHooks: *configureHooks,
		})
		if err != nil {
			return 1, err
		}
		for _, target := range report.Targets {
			fmt.Fprintf(streams.Stdout, "formal-gates installed for %s: %s\n", target.Host, target.TargetPath)
			if target.HookConfig != "" {
				fmt.Fprintf(streams.Stdout, "formal-gates hooks configured for %s: %s\n", target.Host, target.HookConfig)
			}
		}
		return 0, nil
	case "gate":
		return runGate(args, streams)
	case "workflow":
		return runWorkflow(args, streams)
	case "receipt":
		return runReceipt(args, streams)
	case "canary":
		return runCanary(args, streams)
	case "complexity":
		return runComplexity(args, streams)
	case "help", "-h", "--help":
		printUsage(streams.Stdout, program)
		return 0, nil
	default:
		printUsage(streams.Stdout, program)
		return 1, fmt.Errorf("unknown command: %s", command)
	}
}

func runComplexity(args []string, streams IO) (int, error) {
	if len(args) == 0 {
		printUsage(streams.Stdout, "formal-gates")
		return 1, fmt.Errorf("complexity subcommand is required")
	}
	subcommand := args[0]
	args = args[1:]
	switch subcommand {
	case "check":
		fs := flag.NewFlagSet("complexity check", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		vcs := fs.String("vcs", "auto", "diff source: auto, git, svn, or none")
		taskType := fs.String("task-type", "", "task type: delete-or-consolidate, bugfix, small-feature, refactor, or new-system")
		maxNet := fs.String("max-net", "", "maximum net diff budget")
		maxNewProdFiles := fs.String("max-new-prod-files", "", "maximum new production files")
		maxProdInsertions := fs.String("max-prod-insertions", "", "maximum production insertions")
		staged := fs.Bool("staged", false, "review staged git diff only")
		jsonOutput := fs.Bool("json", false, "emit JSON")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if strings.TrimSpace(*taskType) == "" {
			return 1, fmt.Errorf("--task-type is required")
		}
		maxNetValue, err := optionalInt(maxNet, "--max-net")
		if err != nil {
			return 1, err
		}
		maxNewProdFilesValue, err := optionalInt(maxNewProdFiles, "--max-new-prod-files")
		if err != nil {
			return 1, err
		}
		maxProdInsertionsValue, err := optionalInt(maxProdInsertions, "--max-prod-insertions")
		if err != nil {
			return 1, err
		}
		report, result := validate.Complexity(validate.ComplexityOptions{
			Worktree:          *worktree,
			VCS:               *vcs,
			TaskType:          *taskType,
			MaxNet:            maxNetValue,
			MaxNewProdFiles:   maxNewProdFilesValue,
			MaxProdInsertions: maxProdInsertionsValue,
			Staged:            *staged,
		})
		if !result.OK() {
			return printValidationResult(streams.Stdout, "complexity", result)
		}
		if *jsonOutput {
			data, err := validate.ComplexityJSON(report)
			if err != nil {
				return 1, err
			}
			fmt.Fprintln(streams.Stdout, string(data))
		} else {
			fmt.Fprintln(streams.Stdout, validate.ComplexityText(report))
		}
		return validate.ComplexityExitCode(report.Status), nil
	default:
		printUsage(streams.Stdout, "formal-gates")
		return 1, fmt.Errorf("unknown complexity subcommand: %s", subcommand)
	}
}

func runCanary(args []string, streams IO) (int, error) {
	if len(args) == 0 {
		printUsage(streams.Stdout, "formal-gates")
		return 1, fmt.Errorf("canary subcommand is required")
	}
	subcommand := args[0]
	args = args[1:]
	switch subcommand {
	case "portable":
		fs := flag.NewFlagSet("canary portable", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		root := fs.String("root", ".", "formal-gates package root")
		format := fs.String("format", "text", "output format: text or json")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if *format != "text" && *format != "json" {
			return 1, fmt.Errorf("unsupported --format %q (want text or json)", *format)
		}
		report, result := validate.PortableCanary(validate.PortableCanaryOptions{Root: *root})
		if *format == "json" {
			data, err := validate.PortableCanaryJSON(report)
			if err != nil {
				return 1, err
			}
			fmt.Fprintln(streams.Stdout, string(data))
		} else {
			for _, check := range report.Checks {
				if check.Detail == "" {
					fmt.Fprintf(streams.Stdout, "%s %s\n", check.Status, check.Name)
				} else {
					fmt.Fprintf(streams.Stdout, "%s %s: %s\n", check.Status, check.Name, check.Detail)
				}
			}
		}
		if !result.OK() {
			return 1, fmt.Errorf("formal-gates portable canary failed with %d issue(s)", len(result.Failures))
		}
		return 0, nil
	case "codex-hook":
		fs := flag.NewFlagSet("canary codex-hook", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		outputDir := fs.String("output-dir", "", "directory for canary artifacts")
		codexCommand := fs.String("codex-command", "codex", "Codex executable path or command name")
		timeoutSeconds := fs.Int("timeout-seconds", 180, "maximum seconds to wait for codex exec")
		keepTemp := fs.Bool("keep-temp", false, "keep successful canary artifacts")
		binary := fs.String("binary", "", "formal-gates binary to install as the temporary hook; defaults to the current executable")
		format := fs.String("format", "json", "output format: text or json")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if *format != "text" && *format != "json" {
			return 1, fmt.Errorf("unsupported --format %q (want text or json)", *format)
		}
		summary, result := validate.CodexHookCanary(validate.CodexHookCanaryOptions{
			Worktree:       *worktree,
			OutputDir:      *outputDir,
			CodexCommand:   *codexCommand,
			TimeoutSeconds: *timeoutSeconds,
			KeepTemp:       *keepTemp,
			Binary:         *binary,
		})
		if *format == "json" {
			data, err := validate.CodexHookCanaryJSON(summary)
			if err != nil {
				return 1, err
			}
			fmt.Fprintln(streams.Stdout, string(data))
		} else {
			fmt.Fprintf(streams.Stdout, "%s codex-hook-client-canary\n", summary.Status)
			fmt.Fprintf(streams.Stdout, "artifactDir: %s\n", summary.ArtifactDir)
			fmt.Fprintf(streams.Stdout, "preToolUsePayloadCount: %d\n", summary.PreToolUsePayloadCount)
			fmt.Fprintf(streams.Stdout, "markerExists: %t\n", summary.MarkerExists)
		}
		if !result.OK() {
			return 1, fmt.Errorf("formal-gates codex hook canary failed: %s", summary.Status)
		}
		return 0, nil
	case "codex-hook-probe":
		fs := flag.NewFlagSet("canary codex-hook-probe", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		payloadDir := fs.String("payload-dir", "", "directory where hook payloads are written")
		formalHookOutput := fs.String("formal-hook-output", "", "optional file to append formal hook decision output")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if fs.NArg() != 0 {
			return 1, fmt.Errorf("canary codex-hook-probe does not accept positional arguments")
		}
		payload, err := io.ReadAll(streams.Stdin)
		if err != nil {
			return 1, err
		}
		probe, result := validate.CodexHookProbe(validate.CodexHookProbeOptions{
			PayloadDir:       *payloadDir,
			FormalHookOutput: *formalHookOutput,
			Payload:          payload,
		})
		if !result.OK() {
			return printValidationResult(streams.Stdout, "canary codex-hook-probe", result)
		}
		if probe.Decision != nil {
			data, err := json.Marshal(probe.Decision)
			if err != nil {
				return 1, err
			}
			fmt.Fprintln(streams.Stdout, string(data))
		}
		return probe.ExitCode, nil
	default:
		printUsage(streams.Stdout, "formal-gates")
		return 1, fmt.Errorf("unknown canary subcommand: %s", subcommand)
	}
}

func runReceipt(args []string, streams IO) (int, error) {
	if len(args) == 0 {
		printUsage(streams.Stdout, "formal-gates")
		return 1, fmt.Errorf("receipt subcommand is required")
	}
	subcommand := args[0]
	args = args[1:]
	switch subcommand {
	case "register":
		fs := flag.NewFlagSet("receipt register", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		runDir := fs.String("run-dir", "", "workflow run directory under .claude/gates/runs")
		provider := fs.String("provider", "", "receipt provider: claude-code, codex, or cursor")
		artifact := fs.String("artifact", "", "review artifact path")
		gate := fs.String("gate", "", "gate id")
		stage := fs.String("stage", "", "gate stage")
		workflowID := fs.String("workflow-id", "", "workflow id")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		registration, result := validate.ReceiptRegisterDispatch(validate.ReceiptRegisterOptions{
			Worktree:   *worktree,
			RunDir:     *runDir,
			Provider:   *provider,
			Artifact:   *artifact,
			Gate:       *gate,
			Stage:      *stage,
			WorkflowID: *workflowID,
		})
		if !result.OK() {
			return printValidationResult(streams.Stdout, "receipt register", result)
		}
		return printJSON(streams.Stdout, registration)
	case "capture":
		fs := flag.NewFlagSet("receipt capture", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		runDir := fs.String("run-dir", "", "workflow run directory under .claude/gates/runs")
		provider := fs.String("provider", "", "receipt provider: claude-code, codex, or cursor")
		event := fs.String("event", "", "host lifecycle event name")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if fs.NArg() != 0 {
			return 1, fmt.Errorf("receipt capture does not accept positional arguments")
		}
		payload, err := io.ReadAll(streams.Stdin)
		if err != nil {
			return 1, err
		}
		eventRecord, result := validate.ReceiptCapture(validate.ReceiptCaptureOptions{
			Worktree: *worktree,
			RunDir:   *runDir,
			Provider: *provider,
			Event:    *event,
			Payload:  payload,
		})
		if !result.OK() {
			return printValidationResult(streams.Stdout, "receipt capture", result)
		}
		return printJSON(streams.Stdout, eventRecord)
	case "finalize":
		fs := flag.NewFlagSet("receipt finalize", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		runDir := fs.String("run-dir", "", "workflow run directory under .claude/gates/runs")
		provider := fs.String("provider", "", "receipt provider: claude-code, codex, or cursor")
		artifact := fs.String("artifact", "", "review artifact path")
		gate := fs.String("gate", "", "gate id")
		stage := fs.String("stage", "", "gate stage")
		workflowID := fs.String("workflow-id", "", "workflow id")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		receipt, result := validate.ReceiptFinalize(validate.ReceiptFinalizeOptions{
			Worktree:   *worktree,
			RunDir:     *runDir,
			Provider:   *provider,
			Artifact:   *artifact,
			Gate:       *gate,
			Stage:      *stage,
			WorkflowID: *workflowID,
		})
		if !result.OK() {
			return printValidationResult(streams.Stdout, "receipt finalize", result)
		}
		return printJSON(streams.Stdout, receipt)
	case "validate":
		fs := flag.NewFlagSet("receipt validate", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		receipt := fs.String("receipt", "", "receipt JSON path")
		artifact := fs.String("artifact", "", "review artifact path")
		gate := fs.String("gate", "", "gate id")
		stage := fs.String("stage", "", "gate stage")
		workflowID := fs.String("workflow-id", "", "workflow id")
		changeSnapshot := fs.String("change-snapshot", "", "change snapshot")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		result := validate.ReceiptValidate(validate.ReceiptValidateOptions{
			Worktree:       *worktree,
			Receipt:        *receipt,
			Artifact:       *artifact,
			Gate:           *gate,
			Stage:          *stage,
			WorkflowID:     *workflowID,
			ChangeSnapshot: *changeSnapshot,
		})
		return printValidationResult(streams.Stdout, "receipt", result)
	case "preflight":
		fs := flag.NewFlagSet("receipt preflight", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		host := fs.String("host", "", "host name: claude-code, codex, or cursor")
		worktree := fs.String("worktree", ".", "repository root")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		report, result := validate.ReceiptPreflight(validate.ReceiptPreflightOptions{
			Host:     *host,
			Worktree: *worktree,
		})
		if !result.OK() {
			return printValidationResult(streams.Stdout, "receipt preflight", result)
		}
		return printJSON(streams.Stdout, report)
	default:
		printUsage(streams.Stdout, "formal-gates")
		return 1, fmt.Errorf("unknown receipt subcommand: %s", subcommand)
	}
}

func runWorkflow(args []string, streams IO) (int, error) {
	if len(args) == 0 {
		printUsage(streams.Stdout, "formal-gates")
		return 1, fmt.Errorf("workflow subcommand is required")
	}
	subcommand := args[0]
	args = args[1:]
	switch subcommand {
	case "snapshot":
		fs := flag.NewFlagSet("workflow snapshot", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		vcs := fs.String("vcs", "auto", "snapshot source: auto, file-hash, git, or svn")
		baseRef := fs.String("base-ref", "", "base ref for git snapshots")
		headRef := fs.String("head-ref", "HEAD", "head ref for git snapshots")
		includeWorkingTree := fs.Bool("include-working-tree", false, "include dirty git working tree content")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		snapshot, result := validate.WorkflowSnapshot(validate.WorkflowSnapshotOptions{
			Worktree:           *worktree,
			VCS:                *vcs,
			BaseRef:            *baseRef,
			HeadRef:            *headRef,
			IncludeWorkingTree: *includeWorkingTree,
		})
		if !result.OK() {
			return printValidationResult(streams.Stdout, "workflow snapshot", result)
		}
		encoded, err := validate.WorkflowSnapshotJSON(snapshot)
		if err != nil {
			return 1, err
		}
		fmt.Fprintln(streams.Stdout, string(encoded))
		return 0, nil
	case "record-stage":
		fs := flag.NewFlagSet("workflow record-stage", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		state := fs.String("state", "", "gate state JSON path; defaults to .claude/gates/gate-state.json under --worktree")
		runDir := fs.String("run-dir", "", "workflow run directory under .claude/gates/runs")
		gate := fs.String("gate", "", "gate id")
		verdict := fs.String("verdict", "", "gate verdict")
		mode := fs.String("mode", "", "gate mode")
		stage := fs.String("stage", "", "gate stage")
		artifact := fs.String("artifact", "", "gate artifact path")
		actor := fs.String("actor", "gate-workflow", "recording actor")
		workflowID := fs.String("workflow-id", "", "workflow id")
		changeSnapshot := fs.String("change-snapshot", "", "change snapshot")
		reason := fs.String("reason", "", "recording reason")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if *verdict == "" {
			return 1, fmt.Errorf("--verdict is required")
		}
		result := validate.WorkflowRecordStage(validate.WorkflowRecordStageOptions{
			Worktree:       *worktree,
			StatePath:      *state,
			Gate:           *gate,
			Verdict:        *verdict,
			Mode:           *mode,
			Stage:          *stage,
			Artifact:       *artifact,
			Actor:          *actor,
			WorkflowID:     *workflowID,
			ChangeSnapshot: *changeSnapshot,
			Reason:         *reason,
			RunDir:         *runDir,
		})
		if !result.OK() {
			return printValidationResult(streams.Stdout, "workflow record-stage", result)
		}
		fmt.Fprintf(streams.Stdout, "GATE_WORKFLOW_RECORDED gate=%s verdict=%s workflowId=%s changeSnapshot=%s\n", *gate, *verdict, *workflowID, *changeSnapshot)
		return 0, nil
	case "verify-admission":
		fs := flag.NewFlagSet("workflow verify-admission", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		state := fs.String("state", "", "gate state JSON path; defaults to .claude/gates/gate-state.json under --worktree")
		runDir := fs.String("run-dir", "", "workflow run directory under .claude/gates/runs")
		gate := fs.String("gate", "", "gate id")
		workflowID := fs.String("workflow-id", "", "workflow id")
		changeSnapshot := fs.String("change-snapshot", "", "change snapshot")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		result := validate.WorkflowVerifyAdmission(validate.WorkflowVerifyAdmissionOptions{
			Worktree:       *worktree,
			StatePath:      *state,
			Gate:           *gate,
			WorkflowID:     *workflowID,
			ChangeSnapshot: *changeSnapshot,
			RunDir:         *runDir,
		})
		if !result.OK() {
			return printValidationResult(streams.Stdout, "workflow admission", result)
		}
		fmt.Fprintf(streams.Stdout, "GATE_WORKFLOW_ADMISSION_PASS gate=%s workflowId=%s changeSnapshot=%s\n", *gate, *workflowID, *changeSnapshot)
		return 0, nil
	case "final-verification":
		fs := flag.NewFlagSet("workflow final-verification", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		runDir := fs.String("run-dir", "", "workflow run directory under .claude/gates/runs")
		attemptsFile := fs.String("attempts-file", "", "JSON file containing final verification attempts")
		attemptsJSON := fs.String("attempts-json", "", "JSON string containing final verification attempts")
		output := fs.String("output", "", "output aggregate artifact path")
		finalQAArtifact := fs.String("final-qa-artifact", "", "existing FinalExecution artifact to record with --record-final-qa")
		recordFinalQA := fs.Bool("record-final-qa", false, "record the supplied FinalExecution artifact after writing final verification")
		state := fs.String("state", "", "gate state JSON path; defaults to .claude/gates/gate-state.json under --worktree")
		actor := fs.String("actor", "gate-workflow", "recording actor when --record-final-qa is used")
		workflowID := fs.String("workflow-id", "", "workflow id")
		changeSnapshot := fs.String("change-snapshot", "", "change snapshot")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		artifact, result := validate.WorkflowFinalVerification(validate.WorkflowFinalVerificationOptions{
			Worktree:        *worktree,
			StatePath:       *state,
			RunDir:          *runDir,
			AttemptsFile:    *attemptsFile,
			AttemptsJSON:    *attemptsJSON,
			OutputArtifact:  *output,
			FinalQAArtifact: *finalQAArtifact,
			RecordFinalQA:   *recordFinalQA,
			Actor:           *actor,
			WorkflowID:      *workflowID,
			ChangeSnapshot:  *changeSnapshot,
		})
		if !result.OK() {
			for _, failure := range result.Failures {
				fmt.Fprintf(streams.Stdout, "GATE_WORKFLOW_BLOCKED %s: %s\n", failure.Path, failure.Message)
			}
			return 1, fmt.Errorf("formal-gates workflow final-verification failed with %d issue(s)", len(result.Failures))
		}
		fmt.Fprintf(streams.Stdout, "GATE_WORKFLOW_FINAL_VERIFICATION status=%s accepted=%d attempts=%d\n", artifact.Status, len(artifact.AcceptedAttempts), len(artifact.Attempts))
		return 0, nil
	case "compact":
		fs := flag.NewFlagSet("workflow compact", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		runDir := fs.String("run-dir", "", "workflow run directory under .claude/gates/runs")
		workflowID := fs.String("workflow-id", "", "workflow id")
		changeSnapshot := fs.String("change-snapshot", "", "change snapshot")
		dryRun := fs.Bool("dry-run", false, "write archive and list cleanup without deleting source files")
		execute := fs.Bool("execute", false, "write verified archive and delete source files")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if *dryRun && *execute {
			return 1, fmt.Errorf("use only one of --dry-run or --execute")
		}
		archive, result := validate.WorkflowCompact(validate.WorkflowCompactOptions{
			Worktree:       *worktree,
			RunDir:         *runDir,
			WorkflowID:     *workflowID,
			ChangeSnapshot: *changeSnapshot,
			Execute:        *execute,
		})
		if !result.OK() {
			for _, failure := range result.Failures {
				fmt.Fprintf(streams.Stdout, "GATE_WORKFLOW_COMPACT_BLOCKED %s: %s\n", failure.Path, failure.Message)
			}
			return 1, fmt.Errorf("formal-gates workflow compact failed with %d issue(s)", len(result.Failures))
		}
		encoded, err := json.MarshalIndent(archive, "", "  ")
		if err != nil {
			return 1, err
		}
		fmt.Fprintln(streams.Stdout, string(encoded))
		return 0, nil
	case "cleanup":
		fs := flag.NewFlagSet("workflow cleanup", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		dryRun := fs.Bool("dry-run", false, "list allowed cleanup paths without deleting")
		execute := fs.Bool("execute", false, "delete allowed cleanup paths")
		var paths stringListFlag
		fs.Var(&paths, "path", "cleanup path; may be repeated")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if *dryRun && *execute {
			return 1, fmt.Errorf("use only one of --dry-run or --execute")
		}
		report, result := validate.WorkflowCleanup(validate.WorkflowCleanupOptions{
			Worktree: *worktree,
			Paths:    paths,
			Execute:  *execute,
		})
		if !result.OK() {
			for _, failure := range result.Failures {
				fmt.Fprintf(streams.Stdout, "GATE_WORKFLOW_CLEANUP_BLOCKED %s: %s\n", failure.Path, failure.Message)
			}
			return 1, fmt.Errorf("formal-gates workflow cleanup failed with %d issue(s)", len(result.Failures))
		}
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return 1, err
		}
		fmt.Fprintln(streams.Stdout, string(encoded))
		return 0, nil
	case "show":
		return runGate(append([]string{"show"}, args...), streams)
	default:
		printUsage(streams.Stdout, "formal-gates")
		return 1, fmt.Errorf("unknown workflow subcommand: %s", subcommand)
	}
}

func runGate(args []string, streams IO) (int, error) {
	if len(args) == 0 {
		printUsage(streams.Stdout, "formal-gates")
		return 1, fmt.Errorf("gate subcommand is required")
	}
	subcommand := args[0]
	args = args[1:]
	switch subcommand {
	case "record":
		fs := flag.NewFlagSet("gate record", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		state := fs.String("state", "", "gate state JSON path; defaults to .claude/gates/gate-state.json under --worktree")
		gate := fs.String("gate", "", "gate id")
		verdict := fs.String("verdict", "", "gate verdict")
		mode := fs.String("mode", "", "gate mode")
		stage := fs.String("stage", "", "gate stage")
		artifact := fs.String("artifact", "", "gate artifact path")
		actor := fs.String("actor", "", "recording actor")
		workflowID := fs.String("workflow-id", "", "workflow id")
		changeSnapshot := fs.String("change-snapshot", "", "change snapshot")
		reason := fs.String("reason", "", "recording reason")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if *verdict == "" {
			return 1, fmt.Errorf("--verdict is required")
		}
		result := validate.GateRecord(validate.GateRecordOptions{
			Worktree:       *worktree,
			StatePath:      *state,
			Gate:           *gate,
			Verdict:        *verdict,
			Mode:           *mode,
			Stage:          *stage,
			Artifact:       *artifact,
			Actor:          *actor,
			WorkflowID:     *workflowID,
			ChangeSnapshot: *changeSnapshot,
			Reason:         *reason,
		})
		if !result.OK() {
			return printValidationResult(streams.Stdout, "gate record", result)
		}
		fmt.Fprintf(streams.Stdout, "GATE_STATE_RECORDED gate=%s verdict=%s workflowId=%s\n", *gate, *verdict, *workflowID)
		return 0, nil
	case "verify-admission":
		fs := flag.NewFlagSet("gate verify-admission", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		state := fs.String("state", "", "gate state JSON path; defaults to .claude/gates/gate-state.json under --worktree")
		gate := fs.String("gate", "", "gate id")
		workflowID := fs.String("workflow-id", "", "workflow id")
		changeSnapshot := fs.String("change-snapshot", "", "change snapshot")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		result := validate.GateVerifyAdmission(validate.GateAdmissionOptions{
			Worktree:       *worktree,
			StatePath:      *state,
			Gate:           *gate,
			WorkflowID:     *workflowID,
			ChangeSnapshot: *changeSnapshot,
		})
		if !result.OK() {
			return printValidationResult(streams.Stdout, "gate admission", result)
		}
		fmt.Fprintf(streams.Stdout, "GATE_STATE_ADMISSION_PASS gate=%s workflowId=%s changeSnapshot=%s\n", *gate, *workflowID, *changeSnapshot)
		return 0, nil
	case "show":
		fs := flag.NewFlagSet("gate show", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		statePath := fs.String("state", "", "gate state JSON path; defaults to .claude/gates/gate-state.json under --worktree")
		format := fs.String("format", "json", "output format: json or text")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if *format != "json" && *format != "text" {
			return 1, fmt.Errorf("unsupported --format %q (want json or text)", *format)
		}
		state, result := validate.GateShow(validate.GateShowOptions{Worktree: *worktree, StatePath: *statePath})
		if !result.OK() {
			return printValidationResult(streams.Stdout, "gate show", result)
		}
		if *format == "text" {
			fmt.Fprintln(streams.Stdout, validate.GateStateText(state))
			return 0, nil
		}
		encoded, err := validate.GateStateJSON(state)
		if err != nil {
			return 1, err
		}
		fmt.Fprintln(streams.Stdout, string(encoded))
		return 0, nil
	default:
		printUsage(streams.Stdout, "formal-gates")
		return 1, fmt.Errorf("unknown gate subcommand: %s", subcommand)
	}
}

func dropOptionalVerb(args []string, verb string) []string {
	if len(args) > 0 && args[0] == verb {
		return args[1:]
	}
	return args
}

type stringListFlag []string

func (s *stringListFlag) String() string {
	return strings.Join(*s, ",")
}

func (s *stringListFlag) Set(value string) error {
	*s = append(*s, value)
	return nil
}

func optionalInt(value *string, name string) (*int, error) {
	if value == nil || strings.TrimSpace(*value) == "" {
		return nil, nil
	}
	parsed, err := strconv.Atoi(strings.TrimSpace(*value))
	if err != nil {
		return nil, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return &parsed, nil
}

func readPromptInput(text, file string, stdin bool, input io.Reader) (string, error) {
	sources := 0
	if text != "" {
		sources++
	}
	if strings.TrimSpace(file) != "" {
		sources++
	}
	if stdin {
		sources++
	}
	if sources > 1 {
		return "", fmt.Errorf("use only one of --text, --file, or --stdin")
	}
	if strings.TrimSpace(file) != "" {
		data, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	if stdin {
		data, err := io.ReadAll(input)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return text, nil
}

func printValidationResult(stdout io.Writer, name string, result validate.Result) (int, error) {
	if result.OK() {
		fmt.Fprintf(stdout, "PASS formal-gates %s validation\n", name)
		return 0, nil
	}
	for _, failure := range result.Failures {
		fmt.Fprintf(stdout, "FAIL %s: %s\n", failure.Path, failure.Message)
	}
	return 1, fmt.Errorf("formal-gates %s validation failed with %d issue(s)", name, len(result.Failures))
}

func printJSON(stdout io.Writer, value any) (int, error) {
	encoded, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return 1, err
	}
	fmt.Fprintln(stdout, string(encoded))
	return 0, nil
}

func readHookDecision(input io.Reader) (validate.HookDecision, error) {
	payload, err := io.ReadAll(input)
	if err != nil {
		return validate.HookDecision{}, err
	}
	if len(payload) == 0 {
		return validate.HookDecision{}, fmt.Errorf("hook payload is required on stdin")
	}
	return validate.Hook(payload)
}

func hasHelpArg(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

func parseFlagSet(fs *flag.FlagSet, args []string, helpOutput io.Writer) (int, error, bool) {
	if hasHelpArg(args) {
		fs.SetOutput(helpOutput)
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil, true
		}
		return 1, err, true
	}
	return 0, nil, false
}

func printHookUsage(stdout io.Writer) {
	fmt.Fprint(stdout, `formal-gates hook decide

Usage:
  formal-gates hook decide < payload.json

Reads one host hook JSON payload from stdin and prints a compact allow/deny JSON decision.
Exit codes:
  0  allow
  1  invalid payload or CLI usage error
  2  deny
`)
}

func printUsage(stdout io.Writer, program string) {
	fmt.Fprintf(stdout, `%s

Usage:
  %s package validate  --root <formal-gates>
  %s artifact validate --root <repo> --file <artifact> --gate <gate-id> --workflow-id <id> --change-snapshot <snapshot>
  %s prompt validate   --root <formal-gates> (--text <text> | --file <file> | --stdin) [--patterns <json>] [--format text|json]
  %s install           --source <formal-gates-dir> --host claude|codex|cursor|both --scope global|project [--project <path>] [--force] [--configure-hooks]
  %s gate record       --worktree <repo> --gate <gate-id> --verdict <verdict> [--artifact <artifact>] --workflow-id <id> --change-snapshot <snapshot>
  %s gate verify-admission --worktree <repo> --gate <gate-id> --workflow-id <id> --change-snapshot <snapshot>
  %s gate show         --worktree <repo> [--format json|text]
  %s workflow snapshot --worktree <repo> --vcs file-hash|git|auto [--base-ref <ref>] [--head-ref <ref>] [--include-working-tree]
  %s workflow record-stage --worktree <repo> --gate <gate-id> --verdict <verdict> [--artifact <artifact>] --workflow-id <id> --change-snapshot <snapshot>
  %s workflow verify-admission --worktree <repo> --gate <gate-id> --workflow-id <id> --change-snapshot <snapshot>
  %s workflow final-verification --worktree <repo> (--attempts-file <json> | --attempts-json <json>) --output <artifact> --workflow-id <id> --change-snapshot <snapshot> [--record-final-qa --final-qa-artifact <artifact>]
  %s workflow compact --worktree <repo> --run-dir .claude/gates/runs/<id> --workflow-id <id> [--change-snapshot <snapshot>] [--dry-run | --execute]
  %s workflow cleanup --worktree <repo> [--path <scratch-path>] [--dry-run | --execute]
  %s receipt register --provider <provider> --worktree <repo> [--run-dir <dir>] --artifact <review.md> --gate <gate-id> --workflow-id <id> [--stage <stage>]
  %s receipt capture --provider <provider> --event <event> --worktree <repo> [--run-dir <dir>] < payload.json
  %s receipt finalize --provider <provider> --worktree <repo> [--run-dir <dir>] --artifact <review.md> --gate <gate-id> --workflow-id <id> [--stage <stage>]
  %s receipt validate --worktree <repo> --receipt <receipt.json> --artifact <review.md> --gate <gate-id> --workflow-id <id> --change-snapshot <snapshot> [--stage <stage>]
  %s receipt preflight --host <host> --worktree <repo>
  %s hook decide       < payload.json
  %s canary portable   --root <formal-gates> [--format text|json]
  %s canary codex-hook --worktree <repo> [--codex-command <codex>] [--keep-temp]
  %s complexity check  --task-type <type> --worktree <repo> [--max-net <n>] [--max-new-prod-files <n>] [--max-prod-insertions <n>] [--staged] [--json]

The native CLI performs deterministic package, artifact, dispatch prompt, install, hook decision, basic gate-state checks, native workflow checks, receipt checks, complexity diff checks, the portable native canary, and the Codex hook live canary.
`, program, program, program, program, program, program, program, program, program, program, program, program, program, program, program, program, program, program, program, program, program, program, program)
}
