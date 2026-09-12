package cli

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
	"formal-gates/internal/validate"
)

type IO struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
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
		var lockErr *validate.LockHeldError
		if errors.As(err, &lockErr) {
			_, _ = printJSON(streams.Stdout, map[string]any{
				"code": "LOCK_HELD", "operation": lockErr.Operation,
				"path": lockErr.Path, "message": lockErr.Error(),
			})
			return code
		}
		fmt.Fprintln(streams.Stderr, err)
	}
	return code
}

func operationError(streams IO, err error) (int, error) {
	var lockErr *validate.LockHeldError
	if !errors.As(err, &lockErr) {
		return 1, err
	}
	if _, printErr := printJSON(streams.Stdout, map[string]any{
		"code":      "LOCK_HELD",
		"operation": lockErr.Operation,
		"path":      lockErr.Path,
		"message":   lockErr.Error(),
	}); printErr != nil {
		return 1, printErr
	}
	return 1, nil
}

func run(program string, args []string, streams IO) (int, error) {
	if len(args) == 0 {
		return runPackage(nil, streams)
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printUsage(streams.Stdout, program)
		return 0, nil
	}
	if args[0] == "--version" || args[0] == "-version" {
		printVersion(streams.Stdout, program)
		return 0, nil
	}
	switch args[0] {
	case "package":
		return runPackage(args[1:], streams)
	case "registry":
		return runRegistry(args[1:], streams)
	case "install":
		return runInstall(args[1:], streams)
	case "uninstall":
		return runUninstall(args[1:], streams)
	case "workflow":
		return runWorkflow(program, args[1:], streams)
	case "hook":
		return runHook(program, args[1:], streams)
	case "lifecycle":
		return runLifecycle(program, args[1:], streams)
	case "gate":
		return runGate(program, args[1:], streams)
	case "canary":
		return runCanary(program, args[1:], streams)
	case "coverage":
		return runCoverage(program, args[1:], streams)
	default:
		printUsage(streams.Stdout, program)
		return 1, fmt.Errorf("unknown command: %s", args[0])
	}
}

func runPackage(args []string, streams IO) (int, error) {
	subcommand := "validate"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		subcommand, args = args[0], args[1:]
	}
	switch subcommand {
	case "validate":
		fs := newFlagSet("package validate", streams)
		root := fs.String("root", ".", "formal-gates package root")
		installed := fs.String("installed-target", "", "optional installed target root to bind in the validation receipt")
		vcs := fs.String("vcs-identity", "", "optional immutable VCS identity to bind in the validation receipt")
		pathFlags := stringListFlag{}
		fs.Var(&pathFlags, "path", "canonical path as name=PATH; repeat as needed")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		result := validate.Package(*root)
		code, err := printValidationResult(streams.Stdout, "package", result)
		if code != 0 || err != nil {
			return code, err
		}
		receipt, receiptErr := validate.PackageReceipt(*root)
		if receiptErr != nil {
			return 1, receiptErr
		}
		if strings.TrimSpace(*installed) != "" || strings.TrimSpace(*vcs) != "" || len(pathFlags) != 0 {
			if strings.TrimSpace(*installed) == "" || strings.TrimSpace(*vcs) == "" {
				return 1, fmt.Errorf("package validate identity output requires --installed-target and --vcs-identity together")
			}
			canonical := map[string]string{}
			for _, item := range pathFlags {
				parts := strings.SplitN(item, "=", 2)
				if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
					return 1, fmt.Errorf("--path must use name=PATH")
				}
				canonical[parts[0]] = parts[1]
			}
			baseline, baselineErr := validate.BuildBaselineReceipt(*vcs, *root, *installed, canonical)
			if baselineErr != nil {
				return 1, baselineErr
			}
			receipt.InstalledTarget = baseline.InstalledTarget
			receipt.InstalledTargetDigest = baseline.InstalledTargetDigest
			receipt.VCSIdentity = baseline.VCSIdentity
			receipt.Disjoint = baseline.Disjoint
			receipt.CanonicalPaths = baseline.CanonicalPaths
			receipt.DisjointProof = baseline.DisjointProof
			receipt.PathIdentities = baseline.PathIdentities
			receipt.HookConfigDigest = baseline.HookConfigDigest
			receipt.ManagedRuleDigest = baseline.ManagedRuleDigest
		}
		return printJSON(streams.Stdout, receipt)
	case "route-candidates":
		fs := newFlagSet("package route-candidates", streams)
		root := fs.String("root", ".", "formal-gates package root")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		candidates, err := validate.PackageRouteCandidates(*root)
		return printValue(streams.Stdout, candidates, err)
	case "baseline":
		fs := newFlagSet("package baseline", streams)
		root := fs.String("root", ".", "package root")
		installed := fs.String("installed-target", "", "installed target root to digest")
		vcs := fs.String("vcs-identity", "", "immutable VCS identity")
		output := fs.String("output", "", "baseline receipt output path")
		pathFlags := stringListFlag{}
		fs.Var(&pathFlags, "path", "canonical path as name=PATH; repeat as needed")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if strings.TrimSpace(*vcs) == "" || strings.TrimSpace(*output) == "" {
			return 1, fmt.Errorf("package baseline requires --vcs-identity and --output")
		}
		canonical := map[string]string{}
		for _, item := range pathFlags {
			parts := strings.SplitN(item, "=", 2)
			if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
				return 1, fmt.Errorf("--path must use name=PATH")
			}
			canonical[parts[0]] = parts[1]
		}
		receipt, err := validate.BuildBaselineReceipt(*vcs, *root, *installed, canonical)
		if err == nil {
			err = validate.WriteBaselineReceipt(*output, receipt)
		}
		return printValue(streams.Stdout, receipt, err)
	default:
		return 1, fmt.Errorf("unknown package subcommand: %s", subcommand)
	}
}

func runRegistry(args []string, streams IO) (int, error) {
	if len(args) == 0 || isHelpArg(args[0]) {
		printRegistryUsage(streams.Stdout, "formal-gates")
		return 0, nil
	}
	subcommand, args := args[0], args[1:]
	switch subcommand {
	case "admit":
		fs := newFlagSet("registry admit", streams)
		path := fs.String("path", "", "registry JSON path")
		recordID := fs.String("record-id", "", "registry record id")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if strings.TrimSpace(*path) == "" || strings.TrimSpace(*recordID) == "" {
			return 1, fmt.Errorf("registry admit requires --path and --record-id")
		}
		receipt, err := validate.AdmitRegistry(*path, *recordID)
		if printCode, printErr := printValue(streams.Stdout, receipt, err); printErr != nil {
			return printCode, printErr
		}
		if !receipt.Accepted {
			return 1, fmt.Errorf("%s: %s", receipt.Code, receipt.Reason)
		}
		return 0, nil
	case "show":
		fs := newFlagSet("registry show", streams)
		path := fs.String("path", "", "registry JSON path")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		doc, err := validate.LoadRegistry(*path)
		return printValue(streams.Stdout, doc, err)
	default:
		return 1, fmt.Errorf("unknown registry subcommand: %s", subcommand)
	}
}

func runInstall(args []string, streams IO) (int, error) {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	source := fs.String("source", "", "formal-gates source directory")
	host := fs.String("host", "", "target host: "+installHostHelp())
	scope := fs.String("scope", "", "install scope: global or project")
	project := fs.String("project", "", "project path for project installs")
	releaseRoot := fs.String("release-root", "", "native transaction release root (bootstrap use)")
	binaryTarget := fs.String("binary-target", "", "native transaction executable target (bootstrap use)")
	bootstrap := fs.Bool("bootstrap", false, "register the selected target in the stage-0 admission bridge without installing runtime files")
	force := fs.Bool("force", false, "replace an existing target")
	skipHooks := fs.Bool("skip-hooks", false, "install without changing native host hooks")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	if fs.NArg() != 0 {
		return 1, fmt.Errorf("install does not accept positional arguments")
	}
	options := validate.InstallOptions{Source: *source, Host: *host, Scope: *scope, Project: *project, ReleaseRoot: *releaseRoot, BinaryTarget: *binaryTarget, Bootstrap: *bootstrap, Force: *force, SkipHooks: *skipHooks}
	if err := validate.RequireInstallLauncher(options); err != nil {
		return operationError(streams, err)
	}
	report, err := validate.Install(options)
	if err != nil {
		return operationError(streams, err)
	}
	for _, target := range report.Targets {
		fmt.Fprintf(streams.Stdout, "formal-gates installed for %s: %s\n", target.Host, target.TargetPath)
		if target.HookConfig != "" {
			fmt.Fprintf(streams.Stdout, "formal-gates hooks configured for %s: %s\n", target.Host, target.HookConfig)
		}
		if target.ManagedRulePath != "" && target.ManagedRuleAction == "APPLIED" {
			fmt.Fprintf(streams.Stdout, "formal-gates host instruction block written for %s: %s\n", target.Host, target.ManagedRulePath)
		}
	}
	// Keep the friendly lines above for interactive bootstrap users, and also
	// emit the complete immutable receipt so callers can bind source/installed
	// digests, manifests and hook/rule paths without scraping prose.
	if _, printErr := printJSON(streams.Stdout, report); printErr != nil {
		return 1, printErr
	}
	return 0, nil
}

func runUninstall(args []string, streams IO) (int, error) {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	host := fs.String("host", "", "target host: "+installHostHelp())
	scope := fs.String("scope", "", "uninstall scope: global or project")
	project := fs.String("project", "", "project path for project uninstalls")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	if fs.NArg() != 0 {
		return 1, fmt.Errorf("uninstall does not accept positional arguments")
	}
	options := validate.UninstallOptions{Host: *host, Scope: *scope, Project: *project}
	if err := validate.RequireUninstallLauncher(options); err != nil {
		return 1, err
	}
	report, err := validate.Uninstall(options)
	if err != nil {
		return operationError(streams, err)
	}
	for _, target := range report.Targets {
		fmt.Fprintf(streams.Stdout, "formal-gates uninstalled for %s: %s\n", target.Host, target.TargetPath)
		if target.ManagedRulePath != "" {
			fmt.Fprintf(streams.Stdout, "formal-gates host instruction block cleaned for %s: %s\n", target.Host, target.ManagedRulePath)
		}
		if target.HookConfig != "" {
			fmt.Fprintf(streams.Stdout, "formal-gates hooks cleaned for %s: %s\n", target.Host, target.HookConfig)
		}
	}
	return 0, nil
}

func runWorkflow(program string, args []string, streams IO) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("workflow subcommand is required (e.g. workflow start|show|drive|submit ...)")
	}
	if isHelpArg(args[0]) {
		printWorkflowUsage(streams.Stdout, program)
		return 0, nil
	}
	if strings.HasPrefix(args[0], "-") {
		return 1, fmt.Errorf("workflow subcommand is required (e.g. workflow start|show|drive|submit ...)")
	}
	sub, args := args[0], args[1:]
	if handler, ok := workflowSubcommands[sub]; ok {
		return handler(args, streams)
	}
	return 1, fmt.Errorf("unknown workflow subcommand: %s", sub)
}

// Workflow commands all use the current engine. Historical runs have only raw
// diagnosis; no command forwards to a former writer.
var workflowSubcommands = map[string]func([]string, IO) (int, error){
	"start":         runWorkflowStart,
	"show":          runWorkflowShow,
	"status":        runWorkflowStatus,
	"next":          runWorkflowNext,
	"drive":         runWorkflowDrive,
	"submit":        runWorkflowSubmit,
	"diagnose":      runWorkflowDiagnose,
	"guard-preview": runWorkflowGuardPreview,
}

func workflowFlag(args []string, name, fallback string) string {
	prefix := "--" + name + "="
	for index, arg := range args {
		if strings.HasPrefix(arg, prefix) {
			return strings.TrimPrefix(arg, prefix)
		}
		if arg == "--"+name && index+1 < len(args) {
			return args[index+1]
		}
	}
	return fallback
}

func runWorkflowStart(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow start", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	req := fs.String("requirement", "", "one project-relative requirement file")
	vcs := fs.String("vcs", "git", "Git is the supported VCS")
	base := fs.String("base-snapshot", "", "optional immutable Git base identity to verify")
	route := fs.String("route", "", "regular intake, or lightweight with --intake-receipt")
	intake := fs.String("intake-receipt", "", "typed lightweight intake confirmation receipt JSON path")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	if *vcs != "git" {
		return 1, fmt.Errorf("UNSUPPORTED_ENGINE_ENTRY: workflow supports Git only")
	}
	candidatePackageRoot := *pkg
	if workflowFlag(args, "package-root", "") == "" {
		candidatePackageRoot = ""
	}
	admission, err := validate.ResolveEngineAdmission(*root, candidatePackageRoot)
	if err != nil {
		return 1, err
	}
	// Facade intake normalizes the installed host once before issuing identities.
	request := facade.StartRequest{Provider: admission.Host, RunID: *runID, RequirementPath: *req, BaseSnapshot: *base, Route: *route}
	if *intake != "" {
		if *req != "" || (*route != "" && *route != "lightweight") {
			return 1, fmt.Errorf("--intake-receipt accepts only lightweight and no --requirement")
		}
		receipt, err := readEngineIntakeReceipt(*intake)
		if err != nil {
			return 1, err
		}
		request = facade.StartRequest{RunID: *runID, Route: "lightweight", Provider: "engine", DefinitionSource: facade.DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: receipt}
	} else if *req == "" {
		return 1, fmt.Errorf("workflow start requires --requirement or --intake-receipt")
	}
	_, run, err := facade.Start(facade.StartOptions{Root: *root, Request: request, PackageRoot: admission.Target, ArtifactRoot: *root, Admission: &facade.Admission{RegistryPath: admission.RegistryPath, PackageDigest: admission.PackageDigest, InstalledTargetIdentity: admission.RecordID, Generation: admission.Generation, Lease: admission.Lease, Token: admission.Token}})
	return printValue(streams.Stdout, run, err)
}

func runWorkflowShow(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow show", streams)
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "run id")
	summary := fs.Bool("summary", false, "show incremental progress and evidence/message paths")
	after := fs.Uint64("after-revision", 0, "return operation results accepted after this revision (with --summary)")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	if strings.TrimSpace(*runID) == "" {
		return 1, fmt.Errorf("run id is required")
	}
	if facadeRun, ok, err := openEngineIfPresent(*root, *runID); ok {
		if err != nil {
			return 1, err
		}
		if *summary {
			progress, err := facadeRun.ProgressSince(*after)
			return printValue(streams.Stdout, progress, err)
		}
		if *after != 0 {
			return 1, fmt.Errorf("--after-revision requires --summary")
		}
		run, err := facadeRun.Show()
		return printValue(streams.Stdout, run, err)
	}
	return 1, fmt.Errorf("UNSUPPORTED_RUN_VERSION: no current engine run; use workflow diagnose for raw historical state")
}

func runWorkflowStatus(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow status", streams)
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "engine run id")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	f, ok, err := openEngineIfPresent(*root, *runID)
	if !ok {
		return 1, fmt.Errorf("workflow status is supported for candidate engine runs only")
	}
	if err != nil {
		return 1, err
	}
	run, err := f.Status()
	return printValue(streams.Stdout, run, err)
}

func runWorkflowNext(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow next", streams)
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "engine run id")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	f, ok, err := openEngineIfPresent(*root, *runID)
	if !ok {
		return 1, fmt.Errorf("workflow next is supported for candidate engine runs only")
	}
	if err != nil {
		return 1, err
	}
	if run, showErr := f.Show(); showErr == nil && run.Regular != nil {
		return printValue(streams.Stdout, run, nil)
	}
	next, err := f.Next()
	return printValue(streams.Stdout, next, err)
}

func runWorkflowDrive(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow drive", streams)
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "engine run id")
	actionID := fs.String("action-id", "", "execute this issued native action")
	external := fs.Bool("external-executor", false, "claim an explicitly user-assigned external developer action")
	continuous := fs.Bool("continuous", false, "consume engine-issued actions until a user, operator, external executor or blocked boundary")
	operationPath := fs.String("operation-request", "", "typed internal task operation request JSON path")
	nativeOperationPath := fs.String("operation-native", "", "bound native tool request JSON path")
	mcp := fs.Bool("operation-mcp", false, "serve bound task operations over MCP stdio")
	nativeIdentity := fs.String("native-identity", "", "native session identity, verified before task operation execution")
	resume := fs.Bool("resume", false, "resume this action through the bound native session")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	if *continuous && (*actionID != "" || *external || *resume || *operationPath != "" || *nativeOperationPath != "" || *mcp || *nativeIdentity != "") {
		return 1, fmt.Errorf("--continuous cannot combine with a single action or task operation")
	}
	if *mcp && (*actionID == "" || *nativeIdentity == "" || *operationPath != "" || *nativeOperationPath != "" || *resume) {
		return 1, fmt.Errorf("--operation-mcp requires --action-id and --native-identity; cannot combine with --operation-request or --resume")
	}
	if !*mcp && *nativeOperationPath == "" && *nativeIdentity != "" {
		return 1, fmt.Errorf("--native-identity requires --operation-mcp or --operation-native")
	}
	if *nativeOperationPath != "" && (*mcp || *actionID == "" || *nativeIdentity == "" || *operationPath != "" || *resume) {
		return 1, fmt.Errorf("--operation-native requires --action-id and --native-identity; cannot combine with --operation-mcp, --operation-request or --resume")
	}
	if *external && (*actionID == "" || *resume || *operationPath != "" || *mcp || *nativeOperationPath != "" || *nativeIdentity != "") {
		return 1, fmt.Errorf("--external-executor requires only --action-id and cannot combine with native operations")
	}
	f, ok, err := openEngineIfPresent(*root, *runID)
	if !ok {
		return 1, fmt.Errorf("workflow drive is supported for candidate engine runs only")
	}
	if err != nil {
		return 1, err
	}
	if *mcp {
		if err := f.ServeNativeOperationMCP(*actionID, *nativeIdentity, streams.Stdin, streams.Stdout); err != nil {
			return 1, err
		}
		return 0, nil
	}
	if *nativeOperationPath != "" {
		data, err := os.ReadFile(*nativeOperationPath)
		if err != nil {
			return 1, err
		}
		result, err := f.RunNativeOperation(*actionID, *nativeIdentity, data)
		return printValue(streams.Stdout, result, err)
	}
	if *operationPath != "" {
		if *actionID != "" {
			return 1, fmt.Errorf("--operation-request cannot be combined with --action-id")
		}
		data, err := os.ReadFile(*operationPath)
		if err != nil {
			return 1, err
		}
		var q protocol.TaskOperationRequest
		if err = json.Unmarshal(data, &q); err != nil {
			return 1, err
		}
		result, err := f.RunTaskOperation(q, *resume)
		return printValue(streams.Stdout, result, err)
	}
	if *external {
		input, err := f.PrepareExternalDeveloper(*actionID)
		return printValue(streams.Stdout, input, err)
	}
	if *continuous {
		result, err := f.DriveContinuous(context.Background())
		return printValue(streams.Stdout, result, err)
	}
	if *actionID != "" {
		run, err := f.ExecuteNative(*actionID, *resume)
		return printValue(streams.Stdout, run, err)
	}
	run, err := f.Drive()
	return printValue(streams.Stdout, run, err)
}

func runWorkflowSubmit(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow submit", streams)
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "engine run id")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	f, ok, err := openEngineIfPresent(*root, *runID)
	if !ok {
		return 1, fmt.Errorf("workflow submit is supported for candidate engine runs only")
	}
	if err != nil {
		return 1, err
	}
	var event protocol.Event
	if err := json.NewDecoder(streams.Stdin).Decode(&event); err != nil {
		return 1, fmt.Errorf("workflow submit requires a typed event JSON object on stdin: %w", err)
	}
	acceptance, run, err := f.Submit(event)
	if err != nil {
		return 1, err
	}
	return printJSON(streams.Stdout, map[string]any{"acceptance": acceptance, "next": run.Next, "run": run})
}

func runWorkflowDiagnose(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow diagnose", streams)
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "run id whose raw state should be diagnosed")
	path := fs.String("path", "", "raw state or terminal summary path")
	config := fs.Bool("config", false, "show validated effective configuration and source files")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	if *config {
		value, err := facade.Settings(*root)
		return printValue(streams.Stdout, value, err)
	}
	if strings.TrimSpace(*path) == "" && strings.TrimSpace(*runID) != "" {
		// Diagnose is the raw, read-only path. It must not require launcher
		// admission or open a normal façade writer, and terminal runs use the
		// retained summary after active state cleanup.
		engineDir := filepath.Join(*root, facade.EngineNamespace, *runID)
		if _, statErr := os.Stat(engineDir); statErr == nil {
			statePath := filepath.Join(engineDir, "state.json")
			if _, stateErr := os.Stat(statePath); os.IsNotExist(stateErr) {
				statePath = filepath.Join(engineDir, "terminal-summary.json")
			}
			report, diagErr := facade.Diagnose(statePath, "", "")
			return printValue(streams.Stdout, report, diagErr)
		}
	}
	statePath := strings.TrimSpace(*path)
	if statePath == "" {
		if strings.TrimSpace(*runID) == "" {
			return 1, fmt.Errorf("workflow diagnose requires --path or --run-id")
		}
		statePath = filepath.Join(*root, ".gates", "tmp", *runID, "state.json")
		if _, statErr := os.Stat(statePath); os.IsNotExist(statErr) {
			// Terminal runs intentionally remove .gates/tmp/<run-id>/state.json;
			// diagnose --run-id must fall back to the retained summary.
			statePath = filepath.Join(*root, ".gates", "results", *runID+".json")
		}
	}
	// Explicit candidate paths (state or retained terminal summary) use the
	// candidate raw parser, matching --run-id behavior.
	engineRoot, _ := filepath.Abs(filepath.Join(*root, facade.EngineNamespace))
	cleanPath, _ := filepath.Abs(statePath)
	if rel, relErr := filepath.Rel(engineRoot, cleanPath); relErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		report, diagErr := facade.Diagnose(statePath, "", "")
		return printValue(streams.Stdout, report, diagErr)
	}
	data, err := os.ReadFile(statePath)
	if err != nil {
		return 1, err
	}
	var raw json.RawMessage
	if err = json.Unmarshal(data, &raw); err != nil {
		return 1, err
	}
	return printJSON(streams.Stdout, map[string]any{"code": "UNSUPPORTED_RUN_VERSION", "path": statePath, "readOnly": true, "raw": raw})
}

// standalonePromptSeparator visually splits multiple standalone gate prompts
// printed by gate run so the host can hand each to its own zero-context reviewer.
const standalonePromptSeparator = "------------------------------------------------"

// runGate handles the standalone gate commands: gate run assembles and
// prints one detached review prompt per gate id, and gate report validates and
// displays a reviewer result. Neither touches run state, persists anything, or
// consumes review-round limits.
func runGate(program string, args []string, streams IO) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("gate subcommand is required (e.g. gate run <ids...>|report)")
	}
	if isHelpArg(args[0]) {
		printGateUsage(streams.Stdout, program)
		return 0, nil
	}
	if strings.HasPrefix(args[0], "-") {
		return 1, fmt.Errorf("gate subcommand is required (e.g. gate run <ids...>|report)")
	}
	sub, args := args[0], args[1:]
	switch sub {
	case "run":
		fs := newFlagSet("gate run", streams)
		root, pkg := rootFlags(fs)
		vcs := fs.String("vcs", "git", "external VCS name")
		scope := fs.String("scope", "", "optional logical review scope")
		ids, code, err, done := parseFlagSetAllowPositional(fs, args, streams.Stdout)
		if done {
			return code, err
		}
		if len(ids) == 0 {
			return 1, fmt.Errorf("gate run requires at least one gate id")
		}
		for _, id := range ids {
			if strings.HasPrefix(id, "-") {
				return 1, fmt.Errorf("gate run flags must precede the positional gate ids; put --flags before the ids (e.g. gate run --scope s <id...>)")
			}
		}
		catalog, err := validate.LoadPromptCatalog(*pkg)
		if err != nil {
			return 1, err
		}
		prompts := make([]string, 0, len(ids))
		for _, id := range ids {
			prompt, err := validate.ComposeStandaloneGatePrompt(catalog, id, *root, *vcs, *scope)
			if err != nil {
				return 1, err
			}
			prompts = append(prompts, prompt)
		}
		fmt.Fprint(streams.Stdout, strings.Join(prompts, "\n\n"+standalonePromptSeparator+"\n\n"))
		return 0, nil
	case "report":
		fs := newFlagSet("gate report", streams)
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		payload, err := io.ReadAll(streams.Stdin)
		if err != nil {
			return 1, err
		}
		result, err := validate.ValidateStandaloneGateResult(payload)
		if err != nil {
			return 1, err
		}
		fmt.Fprintln(streams.Stdout, "脱离 run 的快速检查、非正式结论、未持久化。")
		fmt.Fprintf(streams.Stdout, "status: %s\n", result.Status)
		if strings.TrimSpace(result.Message) != "" {
			fmt.Fprintf(streams.Stdout, "message: %s\n", strings.TrimSpace(result.Message))
		}
		for _, finding := range result.Findings {
			line := fmt.Sprintf("- [%s] %s", finding.Severity, strings.TrimSpace(finding.Message))
			if len(finding.Locations) != 0 {
				line += " (" + strings.Join(finding.Locations, ", ") + ")"
			}
			fmt.Fprintln(streams.Stdout, line)
		}
		return 0, nil
	default:
		return 1, fmt.Errorf("unknown gate subcommand: %s", sub)
	}
}

func runHook(program string, args []string, streams IO) (int, error) {
	if len(args) > 0 && isHelpArg(args[0]) {
		printHookUsage(streams.Stdout, program)
		return 0, nil
	}
	if len(args) == 0 || args[0] != "decide" {
		return 1, fmt.Errorf("hook decide is required")
	}
	fs := newFlagSet("hook decide", streams)
	provider := fs.String("provider", "", "hook host provider: "+providerHelp()+", or empty for the generic protocol")
	if code, err, done := parseFlagSet(fs, args[1:], streams.Stdout); done {
		return code, err
	}
	if err := validateHookProvider(*provider); err != nil {
		return 1, err
	}
	payload, err := io.ReadAll(streams.Stdin)
	if err != nil {
		return 1, err
	}
	if strings.TrimSpace(string(payload)) == "" {
		return 1, fmt.Errorf("hook decide requires a JSON decision payload on stdin (e.g. the host PreToolUse payload); got empty input")
	}
	decision, err := validate.Hook(payload, *provider)
	if err != nil {
		return 1, err
	}
	if resp := validate.HookResponse(*provider, decision); resp != nil {
		data, _ := json.Marshal(resp)
		fmt.Fprintln(streams.Stdout, string(data))
	}
	return validate.HookExitCode(*provider, decision), nil
}

func runLifecycle(program string, args []string, streams IO) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("lifecycle subcommand is required (e.g. lifecycle capture|verify)")
	}
	if isHelpArg(args[0]) {
		printLifecycleUsage(streams.Stdout, program)
		return 0, nil
	}
	if strings.HasPrefix(args[0], "-") {
		return 1, fmt.Errorf("lifecycle subcommand is required (e.g. lifecycle capture|verify)")
	}
	sub, args := args[0], args[1:]
	switch sub {
	case "capture":
		fs := newFlagSet("lifecycle capture", streams)
		root := fs.String("root", "", "repository root override (normally derived from the host payload)")
		provider := fs.String("provider", "", "host provider: "+providerHelp())
		event := fs.String("event", "", "provider lifecycle event name")
		runID := fs.String("run-id", "", "explicit run binding for a resumed native session")
		dispatchID := fs.String("dispatch", "", "already-claimed dispatch receiving this observed lifecycle event")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		payload, err := io.ReadAll(streams.Stdin)
		if err != nil {
			return 1, err
		}
		var binding []lifecycle.CaptureBinding
		if *runID != "" || *dispatchID != "" {
			binding = append(binding, lifecycle.CaptureBinding{RunID: *runID, DispatchID: *dispatchID})
		}
		result, err := lifecycle.Capture(*root, *provider, *event, payload, binding...)
		return printValue(streams.Stdout, result, err)
	case "verify":
		fs := newFlagSet("lifecycle verify", streams)
		root := fs.String("root", ".", "repository root")
		runID := fs.String("run-id", "", "run id")
		dispatch := fs.String("dispatch", "", "prepared dispatch id")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		result, err := lifecycle.VerifyDispatch(*root, *runID, *dispatch)
		return printValue(streams.Stdout, result, err)
	default:
		return 1, fmt.Errorf("unknown lifecycle subcommand: %s", sub)
	}
}

func runCanary(program string, args []string, streams IO) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("canary subcommand is required (e.g. canary portable|fault-matrix|codex-hook|codex-hook-probe)")
	}
	if isHelpArg(args[0]) {
		printCanaryUsage(streams.Stdout, program)
		return 0, nil
	}
	if strings.HasPrefix(args[0], "-") {
		return 1, fmt.Errorf("canary subcommand is required (e.g. canary portable|fault-matrix|codex-hook|codex-hook-probe)")
	}
	sub, args := args[0], args[1:]
	switch sub {
	case "fault-matrix":
		fs := newFlagSet("canary fault-matrix", streams)
		root := fs.String("root", ".", "package root")
		fixture := fs.String("fixture", "", "one fault fixture, such as copy-component:prompts or verify-stage:installed-target")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		report, result := validate.InstallFaultMatrix(validate.InstallFaultMatrixOptions{Root: *root, Fixture: *fixture})
		if code, err := printJSON(streams.Stdout, report); err != nil {
			return code, err
		}
		if !result.OK() {
			return 1, fmt.Errorf("install fault matrix failed")
		}
		return 0, nil
	case "portable":
		fs := newFlagSet("canary portable", streams)
		root := fs.String("root", ".", "package root")
		format := fs.String("format", "text", "text or json")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		formatValue := strings.ToLower(strings.TrimSpace(*format))
		if formatValue != "text" && formatValue != "json" {
			return 1, fmt.Errorf("canary portable --format must be text or json; got %q", *format)
		}
		report, result := validate.PortableCanary(validate.PortableCanaryOptions{Root: *root})
		if formatValue == "json" {
			printJSON(streams.Stdout, report)
		} else {
			for _, check := range report.Checks {
				fmt.Fprintf(streams.Stdout, "%s %s: %s\n", check.Status, check.Name, check.Detail)
			}
		}
		if !result.OK() {
			return 1, fmt.Errorf("portable canary failed")
		}
		return 0, nil
	case "codex-hook":
		fs := newFlagSet("canary codex-hook", streams)
		worktree := fs.String("worktree", ".", "repository root")
		command := fs.String("codex-command", "codex", "Codex command")
		timeout := fs.Int("timeout-seconds", 180, "timeout seconds")
		keep := fs.Bool("keep-temp", false, "keep temporary files")
		binary := fs.String("binary", "", "formal-gates binary")
		format := fs.String("format", "json", "text or json")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		summary, result := validate.CodexHookCanary(validate.CodexHookCanaryOptions{Worktree: *worktree, CodexCommand: *command, TimeoutSeconds: *timeout, KeepTemp: *keep, Binary: *binary})
		if *format == "json" {
			printJSON(streams.Stdout, summary)
		} else {
			fmt.Fprintf(streams.Stdout, "%s codex-hook-client-canary\n", summary.Status)
		}
		if !result.OK() {
			return 1, fmt.Errorf("codex hook canary failed: %s", summary.Status)
		}
		return 0, nil
	case "codex-hook-probe":
		fs := newFlagSet("canary codex-hook-probe", streams)
		dir := fs.String("payload-dir", "", "payload directory")
		quiet := fs.Bool("quiet", false, "do not write status text to stdout when used as a host hook")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		payload, err := io.ReadAll(streams.Stdin)
		if err != nil {
			return 1, err
		}
		probe, result := validate.CodexHookProbe(validate.CodexHookProbeOptions{PayloadDir: *dir, Payload: payload})
		if !result.OK() {
			return printValidationResult(streams.Stdout, "canary codex-hook-probe", result)
		}
		if probe.PayloadPath != "" && !*quiet {
			fmt.Fprintf(streams.Stdout, "codex-hook-probe: wrote %d-byte payload to %s\n", probe.PayloadBytes, probe.PayloadPath)
		}
		return probe.ExitCode, nil
	default:
		return 1, fmt.Errorf("unknown canary subcommand: %s", sub)
	}
}

func openEngineIfPresent(root, runID string) (*facade.Facade, bool, error) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(runID) == "" {
		return nil, false, nil
	}
	dir := filepath.Join(root, facade.EngineNamespace, runID)
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil
		}
		return nil, true, err
	}
	requestData, err := os.ReadFile(filepath.Join(dir, "request.json"))
	if err != nil {
		return nil, true, err
	}
	var request facade.StartRequest
	if err := json.Unmarshal(requestData, &request); err != nil {
		return nil, true, fmt.Errorf("UNREGISTERED_INSTALL: engine request is invalid: %w", err)
	}
	// The run request carries the exact registry record selected at start. Use
	// that identity first so one stable launcher can own several project/global
	// records; resolving by launcher alone would report every sibling as a
	// conflict. The façade is opened before the root binding check so malformed
	// copies still surface their UNSUPPORTED_RUN_VERSION barrier rather than a
	// misleading admission error.
	admission, err := validate.ResolveEngineAdmissionByRecord(request.InstalledTargetIdentity)
	if err != nil {
		return nil, true, err
	}
	if request.PackageDigest != admission.PackageDigest || request.InstalledTargetIdentity != admission.RecordID ||
		request.AdmissionGeneration != admission.Generation || request.AdmissionLease != admission.Lease || request.AdmissionToken != admission.Token {
		return nil, true, fmt.Errorf("UNREGISTERED_INSTALL: engine run identity does not match current admitted launcher")
	}
	f, err := facade.Open(root, runID)
	if err != nil {
		return nil, true, err
	}
	// Once the envelope has passed its version/identity barrier, enforce the
	// current project's target binding. This catches a run copied into another
	// project while preserving the more specific version diagnostic above.
	bound, bindingErr := validate.ResolveEngineAdmission(root, admission.Target)
	if bindingErr != nil {
		return nil, true, bindingErr
	}
	if bound.RecordID != admission.RecordID || bound.PackageDigest != admission.PackageDigest {
		return nil, true, fmt.Errorf("UNREGISTERED_INSTALL: engine run identity does not match current project binding")
	}
	return f, true, nil
}

func newFlagSet(name string, streams IO) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	return fs
}
func printValue(stdout io.Writer, value any, err error) (int, error) {
	if err != nil {
		return 1, err
	}
	return printJSON(stdout, value)
}
func printJSON(stdout io.Writer, value any) (int, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return 1, err
	}
	fmt.Fprintln(stdout, string(data))
	return 0, nil
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
func isHelpArg(arg string) bool {
	return arg == "-h" || arg == "--help" || arg == "help"
}
func hasHelpArg(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}

// parseFlagSetParsed parses args with the shared help-check and parse-error
// branches and returns the remaining positional args. parseFlagSet rejects them,
// while parseFlagSetAllowPositional returns them for commands that accept
// positional arguments (gate run <ids...>).
func parseFlagSetParsed(fs *flag.FlagSet, args []string, help io.Writer) ([]string, int, error, bool) {
	if hasHelpArg(args) {
		fs.SetOutput(help)
		fs.Usage()
		return nil, 0, nil, true
	}
	if err := fs.Parse(args); err != nil {
		return nil, 1, err, true
	}
	return fs.Args(), 0, nil, false
}

func parseFlagSet(fs *flag.FlagSet, args []string, help io.Writer) (int, error, bool) {
	positional, code, err, done := parseFlagSetParsed(fs, args, help)
	if done {
		return code, err, true
	}
	if len(positional) != 0 {
		return 1, fmt.Errorf("%s does not accept positional arguments", fs.Name()), true
	}
	return 0, nil, false
}

// parseFlagSetAllowPositional is parseFlagSet's variant for commands that accept
// positional arguments (gate run <ids...>); it returns the remaining positional
// args instead of rejecting them.
func parseFlagSetAllowPositional(fs *flag.FlagSet, args []string, help io.Writer) ([]string, int, error, bool) {
	return parseFlagSetParsed(fs, args, help)
}
func printUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s <command>\n\nCommands:\n  package validate|route-candidates|baseline\n  registry admit|show\n  install\n  uninstall\n  workflow start|drive|submit|show|status|next|diagnose|guard-preview\n  coverage validate|project-whitelist|reconcile-execution\n  gate run <ids...>|report\n  hook decide\n  lifecycle capture|verify\n  canary portable|fault-matrix|codex-hook|codex-hook-probe\n", program)
}

func printCoverageUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s coverage <subcommand>\n\nSubcommands:\n  validate             validate a coverage contract JSON document from stdin\n  project-whitelist    project an approved whitelist from contract/reviews JSON on stdin\n  reconcile-execution  reconcile execution JSON against a contract and whitelist on stdin\n\nAll commands emit JSON on stdout; contract failures return a non-zero exit code and {code,path,message}.\n", program)
}

func printRegistryUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s registry <subcommand>\n\nSubcommands:\n  admit --path PATH --record-id ID\n  show --path PATH\n\nRegistry mutation is owned by install --bootstrap/install/uninstall.\n", program)
}

// 二层 --help 粒度统一：workflow/gate/hook/lifecycle/canary 各自打印本组的子命令清单，
// 而不是回落到顶层 usage（P2 修复）。叶子命令（如 workflow show --help）仍打印该子命令
// 自己的 flag usage。
func printWorkflowUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s workflow <subcommand>\n\nSubcommands:\n  start|drive|submit|show|status|next|diagnose|guard-preview\n\nRun `%s workflow <subcommand> --help` for a subcommand's flags.\n", program, program)
}

func printGateUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s gate <subcommand>\n\nSubcommands:\n  run <ids...>  assemble one detached review prompt per gate id (outside any run)\n  report        validate and display a standalone gate review result from stdin\n\nRun `%s gate <subcommand> --help` for a subcommand's flags.\n", program, program)
}

func printHookUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s hook decide\n\nHook decision CLI: reads the host hook JSON payload from stdin and prints the JSON decision.\nFlags:\n  --provider <%s|''>  hook host provider (Codex and ZCode use host-specific JSON protocols)\n", program, providerHelp())
}

func printLifecycleUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s lifecycle <subcommand>\n\nSubcommands:\n  capture  record a host lifecycle event (%s) from the host payload on stdin\n  verify   verify a dispatch's lifecycle binding\n\nRun `%s lifecycle <subcommand> --help` for a subcommand's flags.\n", program, lifecycleEventHelp(), program)
}

func printCanaryUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s canary <subcommand>\n\nSubcommands:\n  portable        run the host-agnostic package/install canary\n  fault-matrix    exercise public install copy/switch/verify/recovery fixtures\n                  --fixture copy-component:prompts|verify-stage:installed-target selects one fixture\n  codex-hook      run the live Codex hook blocking canary\n  codex-hook-probe  capture a hook payload to --payload-dir (for hook debugging)\n\nRun `%s canary <subcommand> --help` for a subcommand's flags.\n", program, program)
}

// printVersion reports the binary's version situation. The project keeps no
// embedded version constant, so this gives a usable hint rather than a bare
// "unknown command" error.
func printVersion(w io.Writer, program string) {
	fmt.Fprintf(w, "%s: development build (no embedded version metadata); see CHANGELOG.md for release history.\n", program)
}

// validateHookProvider accepts the recognized hook host providers and rejects
// anything else instead of silently treating it as the generic default.
func validateHookProvider(provider string) error {
	if strings.TrimSpace(provider) == "" {
		return nil
	}
	if descriptor, err := host.Lookup(provider); err == nil && descriptor.Installable {
		return nil
	}
	return fmt.Errorf("unsupported hook provider %q (want %s, or empty for the generic protocol)", provider, providerHelp())
}

func installHostHelp() string {
	return strings.Join(host.InstallableNames(), ", ")
}

func providerHelp() string {
	return strings.Join(host.InstallableIDs(), "|")
}

func lifecycleEventHelp() string {
	seen := map[string]bool{}
	events := make([]string, 0)
	for _, descriptor := range host.All() {
		if !descriptor.Installable {
			continue
		}
		for _, event := range descriptor.LifecycleEvents {
			if !seen[event] {
				seen[event] = true
				events = append(events, event)
			}
		}
	}
	return strings.Join(events, "|")
}

type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }
func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func rootFlags(fs *flag.FlagSet) (*string, *string) {
	return fs.String("root", ".", "repository root"), fs.String("package-root", ".", "installed formal-gates package root")
}

func readEngineIntakeReceipt(input string) (facade.IntakeConfirmationReceipt, error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return facade.IntakeConfirmationReceipt{}, fmt.Errorf("%s: workflow start requires --intake-receipt", facade.InvalidIntakeConfirmation)
	}
	var data []byte
	var err error
	data, err = os.ReadFile(input)
	if err != nil {
		return facade.IntakeConfirmationReceipt{}, fmt.Errorf("%s: intake receipt is unavailable", facade.InvalidIntakeConfirmation)
	}
	var receipt facade.IntakeConfirmationReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		return facade.IntakeConfirmationReceipt{}, fmt.Errorf("%s: intakeConfirmationReceipt JSON is invalid", facade.InvalidIntakeConfirmation)
	}
	return receipt, nil
}
