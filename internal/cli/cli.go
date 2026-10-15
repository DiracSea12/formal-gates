package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"formal-gates/internal/prompts"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"formal-gates/internal/engine/definition"
	"formal-gates/internal/engine/facade"
	"formal-gates/internal/engine/protocol"
	"formal-gates/internal/engine/vcs"
	"formal-gates/internal/harness"
	"formal-gates/internal/host"
	"formal-gates/internal/lifecycle"
	"formal-gates/internal/validate"
)

type IO struct {
	Stdin          io.Reader
	Stdout, Stderr io.Writer
	Context        context.Context
	confirmation   func(any) error
	owner          *facade.OwnerBinding
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
	case "requirement":
		return runRequirement(args[1:], streams)
	case "native":
		return runNativeInteraction(args[1:], streams)
	case "settings":
		return runSettings(args[1:], streams)
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
		candidates, err := prompts.PackageRouteCandidates(*root)
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
	bootstrap := fs.Bool("bootstrap", false, "register the selected target in the installation registry without installing runtime files")
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
	if len(args) == 0 || strings.HasPrefix(args[0], "-") && !isHelpArg(args[0]) {
		return 1, fmt.Errorf("workflow subcommand is required (start|drive|submit|show|status|next|diagnose|guard-preview|progress|message|handoff|replan|serve)")
	}
	if isHelpArg(args[0]) {
		printWorkflowUsage(streams.Stdout, program)
		return 0, nil
	}
	if handler, ok := workflowSubcommands[args[0]]; ok {
		return handler(args[1:], streams)
	}
	return 1, fmt.Errorf("unknown workflow subcommand: %s", args[0])
}

var workflowSubcommands = map[string]func([]string, IO) (int, error){
	"execution-options": runWorkflowExecutionOptions,
	"replan":            runWorkflowReplan, "handoff": runWorkflowHandoff, "message": runWorkflowMessage, "progress": runWorkflowProgress, "start": runWorkflowStart, "drive": runWorkflowDrive, "submit": runWorkflowSubmit, "show": runWorkflowShow, "status": runWorkflowStatus, "next": runWorkflowNext, "diagnose": runWorkflowDiagnose, "guard-preview": runWorkflowGuardPreview, "serve": runWorkflowServe,
}

func runWorkflowStart(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow start", streams)
	machine := fs.Bool("machine", false, "print the complete program projection for machine consumers")
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "current engine run id")
	vcsName := fs.String("vcs", "", "external VCS name")
	base := fs.String("base-snapshot", "", "native base identity for a regular run")
	route := fs.String("route", "", "regular or lightweight")
	configuration := fs.String("configuration", "", "saved configuration name or position for this run; omitted uses the active default")
	intakePath := fs.String("intake-receipt", "", "program integration: exact original intake candidate file")
	intakeChoice := fs.Int("intake", 0, "original displayed intake candidate position")
	document := fs.Int("document", 0, "original displayed requirement document or lightweight material position")
	solutionDocument := fs.Int("solution-document", 0, "original displayed confirmation material position")
	var artifactDocuments positionListFlag
	fs.Var(&artifactDocuments, "artifact-document", "original displayed confirmation material position; repeatable")
	listMaterials := fs.Bool("materials", false, "list project confirmation materials without saving a candidate")
	precheck := fs.Bool("precheck", false, "read and project the complete requirement before confirmation; creates no run")
	listNodes := fs.Bool("nodes", false, "list user-selectable main-flow nodes without starting a run")
	var disabledNodes positionListFlag
	fs.Var(&disabledNodes, "disable-node", "user decision: disable an original --nodes position at start; repeatable")
	saveCandidate := fs.String("save-candidate", "", "save the complete program-generated intake candidate; creates no run")
	requirementPath := fs.String("requirement", "", "requirement document path for --precheck")
	solutionPath := fs.String("solution", "", "solution material for --precheck; defaults to the requirement document")
	var artifacts stringListFlag
	fs.Var(&artifacts, "artifact", "additional confirmation material for --precheck; repeat as needed")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	if *listNodes {
		if len(disabledNodes) != 0 {
			return 1, fmt.Errorf("--nodes lists choices; use --disable-node when starting the confirmed run")
		}
		if *machine {
			return printJSON(streams.Stdout, map[string]any{"flowNodes": definition.MainFlowNodes(), "choiceOwner": "user"})
		}
		return printJSON(streams.Stdout, map[string]any{"flowNodes": workflowFlowNodeChoices(), "choiceOwner": "user"})
	}
	flowSelection, err := selectFlowNodes(disabledNodes)
	if err != nil {
		return 1, err
	}
	routeName := strings.ToLower(strings.TrimSpace(*route))
	if routeName != "" && routeName != "regular" && routeName != "lightweight" {
		return 1, fmt.Errorf("workflow start supports --route regular or lightweight")
	}
	if *precheck {
		if flowSelection != nil {
			return 1, fmt.Errorf("--disable-node records the user's decision at start, after intake confirmation")
		}
		if *listMaterials {
			return printIntakeMaterials(*root, streams.Stdout)
		}
		structured := routeName != "lightweight"
		selected, err := selectIntakeDocuments(*root, structured, requirementPath, *document, solutionPath, *solutionDocument, &artifacts, artifactDocuments, streams.Stdout)
		if err != nil {
			return 1, err
		}
		if !selected {
			return 0, nil
		}
		preview, err := facade.PrepareIntake(*root, *requirementPath, *solutionPath, artifacts, structured)
		if err != nil {
			return 1, err
		}
		position := 0
		if *saveCandidate != "" {
			err = saveWorkflowCandidate(*saveCandidate, preview.IntakeCandidate)
		} else {
			position, err = facade.SaveOriginalCandidate(workflowPayloadDirectory(*root, "", "intake"), preview.IntakeCandidate)
		}
		if err != nil {
			return 1, err
		}
		return printPayloadValue(streams.Stdout, preview, "intake", position)
	}
	if routeName == "" {
		return 1, fmt.Errorf("workflow start requires --route regular or lightweight")
	}
	if *requirementPath != "" || *solutionPath != "" || len(artifacts) != 0 || *saveCandidate != "" || *document != 0 || *solutionDocument != 0 || len(artifactDocuments) != 0 || *listMaterials {
		return 1, fmt.Errorf("--requirement, --solution and --artifact are only available with --precheck; start requires the confirmed intake receipt")
	}
	if *intakePath != "" && *intakeChoice != 0 {
		return 1, fmt.Errorf("select an intake position or exact candidate file")
	}
	if *intakePath == "" {
		selected, done, err := selectOriginalPayload(workflowPayloadDirectory(*root, "", "intake"), *intakeChoice, streams.Stdout)
		if err != nil {
			return 1, err
		}
		if !done {
			return 0, nil
		}
		*intakePath = selected
	}
	receipt, err := readEngineIntakeReceipt(*intakePath)
	if err != nil {
		return 1, err
	}
	if strings.TrimSpace(*runID) == "" {
		return 1, fmt.Errorf("workflow start requires --run-id")
	}
	packageRoot := ""
	fs.Visit(func(f *flag.Flag) {
		if f.Name == "package-root" {
			packageRoot = *pkg
		}
	})
	admission, err := validate.ResolveEngineAdmission(*root, packageRoot)
	if err != nil {
		return 1, err
	}
	if streams.confirmation == nil {
		return 1, fmt.Errorf("NATIVE_CONFIRMATION_UNAVAILABLE: request confirm_start through the current host's formal-gates MCP service")
	}
	confirmation, err := confirmSavedFile(*intakePath, streams.confirmation)
	if err != nil {
		return 1, err
	}
	baseIdentity := ""
	if routeName == "regular" {
		baseIdentity = *base
		if baseIdentity == "" {
			baseIdentity, err = vcs.NewNative(*root, *vcsName, nil).Resolve()
			if err != nil {
				return 1, err
			}
		}
	}
	var owner facade.OwnerBinding
	if streams.owner != nil {
		owner = *streams.owner
	}
	f, run, err := facade.Start(facade.StartOptions{Root: *root, Configuration: *configuration, Confirm: confirmation, Owner: owner, Request: facade.StartRequest{FlowSelection: flowSelection, VCS: *vcsName, RunID: strings.TrimSpace(*runID), Route: routeName, BaseSnapshot: baseIdentity, Provider: admission.Provider, DefinitionSource: facade.DefaultDefinitionSource, DefinitionDigest: definition.WorkflowDefinitionDigest, IntakeConfirmationReceipt: receipt}, ArtifactRoot: *root, RuntimeFiles: admission.RuntimeFiles, Admission: &facade.Admission{RegistryPath: admission.RegistryPath, Target: admission.Target, Provider: admission.Provider, PackageDigest: admission.PackageDigest, InstalledTargetIdentity: admission.RecordID, Generation: admission.Generation, Lease: admission.Lease, Token: admission.Token}})
	if *machine {
		return printValue(streams.Stdout, run, err)
	}
	return printCurrentWorkflowRun(streams, f, err)
}

func runWorkflowShow(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow show", streams)
	machine := fs.Bool("machine", false, "print the complete program projection for machine consumers")
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "program integration: exact engine run id")
	runChoice := fs.Int("run", 0, "select a run by its displayed number")
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
		return 1, fmt.Errorf("UNSUPPORTED_RUN_VERSION: current engine run not found; use workflow diagnose")
	}
	if *machine {
		run, err := f.Show()
		return printValue(streams.Stdout, run, err)
	}
	return printCurrentWorkflowRun(streams, f, nil)
}

func runWorkflowStatus(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow status", streams)
	machine := fs.Bool("machine", false, "print the complete program projection for machine consumers")
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "program integration: exact engine run id")
	runChoice := fs.Int("run", 0, "select a run by its displayed number")
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
		return 1, fmt.Errorf("workflow status is UNSUPPORTED_RUN_VERSION: current engine run not found; use workflow diagnose")
	}
	if *machine {
		run, err := f.Status()
		return printValue(streams.Stdout, run, err)
	}
	return printCurrentWorkflowRun(streams, f, nil)
}

func runWorkflowNext(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow next", streams)
	machine := fs.Bool("machine", false, "print the complete program projection for machine consumers")
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "program integration: exact engine run id")
	runChoice := fs.Int("run", 0, "select a run by its displayed number")
	draftPath := fs.String("decision", "", "program integration: typed control input file; - reads stdin")
	saveCandidate := fs.String("save-candidate", "", "save the complete program-generated control confirmation")
	arguments := registerControlArguments(fs)
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
		return 1, fmt.Errorf("workflow next is UNSUPPORTED_RUN_VERSION: current engine run not found; use workflow diagnose")
	}
	input, provided, err := arguments.input()
	if err != nil {
		return 1, err
	}
	if provided && *draftPath != "" {
		return 1, fmt.Errorf("select semantic flags or machine decision input")
	}
	if *draftPath == "" && !provided {
		if *saveCandidate != "" {
			return 1, fmt.Errorf("--save-candidate requires semantic choice flags or program decision input")
		}
		if *machine {
			next, err := f.Next()
			return printValue(streams.Stdout, next, err)
		}
		return printCurrentWorkflowRun(streams, f, nil)
	}
	if !provided {
		if err := readWorkflowInput(*draftPath, streams.Stdin, &input); err != nil {
			return 1, err
		}
	}
	prepare := f.PrepareCurrentControl
	if *machine {
		prepare = f.PrepareControl
	}
	candidate, err := prepare(input)
	if err != nil {
		return 1, err
	}
	position := 0
	if *saveCandidate != "" {
		err = saveWorkflowCandidate(*saveCandidate, candidate)
	} else {
		position, err = facade.SaveOriginalCandidate(workflowPayloadDirectory(*root, *runID, "control"), candidate)
	}
	if err != nil {
		return 1, err
	}
	if *machine {
		return printPayloadValue(streams.Stdout, candidate, "confirmationCandidate", position)
	}
	return printPayloadValue(streams.Stdout, workflowConfirmationView(candidate), "confirmationCandidate", position)
}

func runWorkflowDrive(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow drive", streams)
	machine := fs.Bool("machine", false, "print the complete program projection for machine consumers")
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "program integration: exact engine run id")
	runChoice := fs.Int("run", 0, "select a run by its displayed number")
	singleStep := fs.Bool("single-step", false, "diagnostic: advance engine facts without consuming native workers")
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
		return 1, fmt.Errorf("workflow drive is UNSUPPORTED_RUN_VERSION: current engine run not found; use workflow diagnose")
	}
	if *singleStep {
		run, err := f.Drive()
		if *machine {
			return printValue(streams.Stdout, run, err)
		}
		return printCurrentWorkflowRun(streams, f, err)
	}
	value, executionErr := consumeWorkflow(streams, f, *root, *runID)
	return printCurrentWorkflowConsumption(streams, f, value, *machine, executionErr)
}

func runWorkflowSubmit(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow submit", streams)
	machine := fs.Bool("machine", false, "print the complete program projection for machine consumers")
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "program integration: exact engine run id")
	runChoice := fs.Int("run", 0, "select a run by its displayed number")
	materialPath := fs.String("worker-material", "", "issued material reference JSON file")
	nativeDirectory := fs.String("native-directory", "", "original native evidence directory")
	sourcePath := fs.String("native-source", "", "original native source receipt JSON")
	confirmationPath := fs.String("confirmation", "", "program integration: exact original control candidate file")
	candidateChoice := fs.Int("confirmation-candidate", 0, "original displayed control candidate position")
	confirm := fs.Bool("confirm", false, "confirm the original selected control candidate after explicit user confirmation")
	singleStep := fs.Bool("single-step", false, "diagnostic: accept this event without consuming subsequent native workers")
	arguments := registerControlArguments(fs)
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	input, provided, err := arguments.input()
	if err != nil {
		return 1, err
	}
	if provided && (*confirmationPath != "" || *materialPath != "" || hasFlagArgument(args, "confirmation-candidate") || *confirm) {
		return 1, fmt.Errorf("semantic flags cannot modify a saved confirmation or worker result")
	}
	if (hasFlagArgument(args, "confirmation-candidate") || *confirm) && *confirmationPath != "" {
		return 1, fmt.Errorf("select a candidate position or exact confirmation file")
	}
	var selectedOwner *facade.Facade
	if *candidateChoice != 0 || *confirm || hasFlagArgument(args, "confirmation-candidate") {
		owner, selected, err := selectWorkflowRun(*root, runID, *runChoice, streams.Stdout)
		if err != nil {
			return 1, err
		}
		if !selected {
			return 0, nil
		}
		if owner == nil {
			return 1, fmt.Errorf("current engine run not found")
		}
		path, done, err := selectOriginalPayload(workflowPayloadDirectory(*root, *runID, "control"), *candidateChoice, streams.Stdout)
		if err != nil {
			return 1, err
		}
		if !done {
			return 0, nil
		}
		*confirmationPath = path
		selectedOwner = owner
		*runChoice = 0
	}
	var saved *facade.ControlCandidate
	if *confirmationPath != "" && *materialPath == "" {
		var candidate facade.ControlCandidate
		if err := readWorkflowInput(*confirmationPath, streams.Stdin, &candidate); err != nil {
			return 1, err
		}
		if selectedOwner != nil {
			runs, err := selectedOwner.ControlRuns()
			if err != nil {
				return 1, err
			}
			bound := false
			for _, run := range runs {
				if run.RunID == candidate.Run.RunID {
					bound = true
					break
				}
			}
			if !bound {
				return 1, fmt.Errorf("confirmation belongs to a different project or run")
			}
		}
		mismatch := false
		fs.Visit(func(f *flag.Flag) {
			switch f.Name {
			case "root":
				mismatch = mismatch || !vcs.SamePath(*root, candidate.Root)
			case "run":
				mismatch = mismatch || selectedOwner == nil
			case "run-id":
				mismatch = mismatch || selectedOwner == nil && *runID != candidate.Run.RunID
			}
		})
		if mismatch {
			return 1, fmt.Errorf("confirmation belongs to a different project or run")
		}
		*root, *runID = candidate.Root, candidate.Run.RunID
		saved = &candidate
	}
	f, selected, err := selectWorkflowRun(*root, runID, *runChoice, streams.Stdout)
	if err != nil {
		return 1, err
	}
	if !selected {
		return 0, nil
	}
	if f == nil {
		return 1, fmt.Errorf("workflow submit is UNSUPPORTED_RUN_VERSION: current engine run not found; use workflow diagnose")
	}
	if *materialPath != "" {
		var material facade.WorkerMaterial
		if err := readJSONFileInto(*materialPath, &material); err != nil {
			return 1, err
		}
		var source map[string]any
		if err := readJSONFileInto(*sourcePath, &source); err != nil {
			return 1, err
		}
		sourceDigest, err := harness.CanonicalDigest(source)
		if err != nil {
			return 1, err
		}
		eventID := protocol.EventID("worker-result:" + material.Digest + ":" + sourceDigest)
		acceptance, run, err := f.SubmitWorkerResult(eventID, material, *nativeDirectory, source, harness.NativeWorkerResultEvidence)
		if err != nil {
			return 1, err
		}
		if *machine {
			return printJSON(streams.Stdout, map[string]any{"acceptance": acceptance, "next": run.Next, "run": run})
		}
		return printCurrentWorkflowConsumption(streams, f, workflowConsumption{Run: run, Acceptance: &acceptance}, false, nil)
	}
	var event protocol.Event
	if saved != nil {
		event = saved.Event
	} else {
		if !provided {
			if err := readWorkflowInput("-", streams.Stdin, &input); err != nil {
				return 1, fmt.Errorf("workflow submit requires semantic request flags or program input: %w", err)
			}
		}
		if input.Request == "" {
			return 1, fmt.Errorf("user decisions require the complete candidate prepared by workflow next with semantic choice flags, then submit --confirmation-candidate after user confirmation")
		}
		prepare := f.PrepareCurrentControl
		if *machine {
			prepare = f.PrepareControl
		}
		candidate, err := prepare(input)
		if err != nil {
			return 1, err
		}
		if candidate.Run.RunID != *runID {
			f, _, err = openEngineIfPresent(candidate.Root, candidate.Run.RunID)
			if err != nil {
				return 1, err
			}
		}
		*root, *runID = candidate.Root, candidate.Run.RunID
		saved = &candidate
		event = candidate.Event
	}
	var acceptance protocol.Acceptance
	var run facade.Run
	confirmation := streams.confirmation
	if confirmation != nil {
		confirmation = func(value any) error {
			candidate := value.(facade.ControlCandidate)
			view := workflowConfirmationView(candidate)
			return streams.confirmation(view)
		}
	}
	confirmation, err = confirmSavedFile(*confirmationPath, confirmation)
	if err != nil {
		return 1, err
	}
	acceptance, run, err = f.SubmitControl(*saved, confirmation)
	if err != nil {
		return 1, err
	}
	if !*singleStep && workflowControlEvent(event) && (event.Request == nil || event.Request.Control != protocol.ControlSelectExecution) {
		value, executionErr := consumeWorkflow(streams, f, *root, *runID)
		value.Acceptance = &acceptance
		return printCurrentWorkflowConsumption(streams, f, value, *machine, executionErr)
	}
	if *machine {
		return printJSON(streams.Stdout, map[string]any{"acceptance": acceptance, "next": run.Next, "run": run})
	}
	return printCurrentWorkflowConsumption(streams, f, workflowConsumption{Run: run, Acceptance: &acceptance}, false, nil)
}

func runWorkflowDiagnose(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow diagnose", streams)
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "run id")
	path := fs.String("path", "", "raw state or terminal summary path")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	statePath := strings.TrimSpace(*path)
	if statePath == "" && strings.TrimSpace(*runID) == "" {
		reports := []facade.DiagnosticReport{}
		for _, namespace := range []string{facade.EngineNamespace, filepath.Join(".gates", "tmp")} {
			directory := filepath.Join(*root, namespace)
			entries, err := os.ReadDir(directory)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return 1, err
			}
			for _, entry := range entries {
				if !entry.IsDir() {
					continue
				}
				path := filepath.Join(directory, entry.Name(), "state.json")
				report, err := facade.Diagnose(path, "", "")
				if os.IsNotExist(err) {
					continue
				}
				reports = append(reports, report)
				if err != nil {
					if _, printErr := printJSON(streams.Stdout, reports); printErr != nil {
						return 1, printErr
					}
					return 1, err
				}
			}
		}
		return printJSON(streams.Stdout, reports)
	}
	if statePath == "" {
		statePath = filepath.Join(*root, facade.EngineNamespace, *runID, "state.json")
		if _, err := os.Stat(statePath); os.IsNotExist(err) {
			summary := filepath.Join(*root, facade.EngineNamespace, *runID, "terminal-summary.json")
			if _, err := os.Stat(summary); err == nil {
				statePath = summary
			} else {
				statePath = validate.RunStatePath(*root, *runID)
				if _, err := os.Stat(statePath); os.IsNotExist(err) {
					statePath = validate.RunSummaryPath(*root, *runID)
				}
			}
		}
	}
	report, err := facade.Diagnose(statePath, "", "")
	if err != nil {
		if _, printErr := printJSON(streams.Stdout, report); printErr != nil {
			return 1, printErr
		}
		return 1, err
	}
	return printValue(streams.Stdout, report, err)
}

// standalonePromptSeparator visually splits multiple standalone gate prompts
// printed by gate run so the host can hand each to its own zero-context reviewer.
const standalonePromptSeparator = "------------------------------------------------"

// runGate handles the standalone gate commands: gate run assembles and
// prints one detached review prompt per gate id, and gate report validates and
// displays a reviewer result from the original captured context. Neither touches
// run state or consumes review-round limits.
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
		vcsName := fs.String("vcs", "git", "external VCS name")
		scope := fs.String("scope", "", "optional logical review scope")
		contextPath := fs.String("save-context", "", "save original review comparison and numeric location evidence; defaults to a program-selected review payload")
		gateChoices := stringListFlag{}
		fs.Var(&gateChoices, "gate", "select a discovered gate by its displayed number (repeatable)")
		ids, code, err, done := parseFlagSetAllowPositional(fs, args, streams.Stdout)
		if done {
			return code, err
		}
		if len(ids) > 0 && len(gateChoices) > 0 {
			return 1, fmt.Errorf("select gate numbers or exact integration ids, not both")
		}
		for _, id := range ids {
			if strings.HasPrefix(id, "-") {
				return 1, fmt.Errorf("gate run flags must precede the positional gate ids; put --flags before the ids (e.g. gate run --scope s <id...>)")
			}
		}
		catalog, err := prompts.LoadPromptCatalog(*pkg)
		if err != nil {
			return 1, err
		}
		if len(ids) == 0 && len(gateChoices) == 0 {
			choices := []map[string]any{}
			for i, gate := range catalog.Gates {
				choices = append(choices, map[string]any{"gate": i + 1, "name": gate.ID})
			}
			return printJSON(streams.Stdout, choices)
		}
		for _, choice := range gateChoices {
			position, err := strconv.Atoi(choice)
			if err != nil || position < 1 || position > len(catalog.Gates) {
				return 1, fmt.Errorf("select an original displayed gate number")
			}
			ids = append(ids, catalog.Gates[position-1].ID)
		}
		for _, id := range ids {
			if _, ok := catalog.Gate(id); !ok {
				return 1, fmt.Errorf("unknown discovered gate %q", id)
			}
		}
		reviewContext, err := validate.CaptureStandaloneGateContext(*root, *vcsName, *scope)
		if err != nil {
			return 1, err
		}
		if *contextPath == "" {
			directory := filepath.Join(*root, ".gates", "tmp", "gate-review")
			if err := os.MkdirAll(directory, 0700); err != nil {
				return 1, err
			}
			paths, err := filepath.Glob(filepath.Join(directory, "review-*.json"))
			if err != nil {
				return 1, err
			}
			for ordinal := len(paths) + 1; ; ordinal++ {
				*contextPath = filepath.Join(directory, fmt.Sprintf("review-%09d.json", ordinal))
				file, err := os.OpenFile(*contextPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
				if os.IsExist(err) {
					continue
				}
				if err != nil {
					return 1, err
				}
				if err := file.Close(); err != nil {
					return 1, err
				}
				break
			}
		}
		if err := saveWorkflowCandidate(*contextPath, reviewContext); err != nil {
			return 1, err
		}
		reviewPaths, err := filepath.Glob(filepath.Join(*root, ".gates", "tmp", "gate-review", "review-*.json"))
		if err != nil {
			return 1, err
		}
		reviewPosition := 0
		for i, path := range reviewPaths {
			if vcs.SamePath(path, *contextPath) {
				reviewPosition = i + 1
				break
			}
		}
		prompts := make([]string, 0, len(ids))
		for _, id := range ids {
			prompt, err := validate.ComposeStandaloneGatePrompt(catalog, id, reviewContext)
			if err != nil {
				return 1, err
			}
			if reviewPosition != 0 {
				prompt = strings.Replace(prompt, "[Current change]\n", fmt.Sprintf("[Current change]\nOriginal review position: %d\n", reviewPosition), 1)
			}
			prompts = append(prompts, prompt)
		}
		fmt.Fprint(streams.Stdout, strings.Join(prompts, "\n\n"+standalonePromptSeparator+"\n\n"))
		return 0, nil
	case "report":
		fs := newFlagSet("gate report", streams)
		contextPath := fs.String("context", "", "program integration: original review context file")
		root := fs.String("root", ".", "project root")
		reviewChoice := fs.Int("review", 0, "select original standalone review by displayed number")
		arguments := registerStandaloneResultArguments(fs)
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if *contextPath == "" {
			selected, done, err := selectStandaloneContext(*root, *reviewChoice, streams.Stdout)
			if err != nil {
				return 1, err
			}
			if !done {
				return 0, nil
			}
			*contextPath = selected
		} else if *reviewChoice != 0 {
			return 1, fmt.Errorf("select original review number or program context file")
		}
		var reviewContext validate.StandaloneGateContext
		if err := readJSONFileInto(*contextPath, &reviewContext); err != nil {
			return 1, err
		}
		payload, provided, err := arguments.input()
		if err != nil {
			return 1, err
		}
		if !provided {
			payload, err = io.ReadAll(streams.Stdin)
			if err != nil {
				return 1, err
			}
		}
		result, err := validate.ValidateStandaloneGateResult(payload, reviewContext)
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
	recordDir := fs.String("record-dir", "", "record the original payload and this hook decision in a directory")
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
	var response []byte
	if resp := validate.HookResponse(*provider, decision); resp != nil {
		response, err = json.Marshal(resp)
		if err != nil {
			return 1, err
		}
		response = append(response, '\n')
	}
	exitCode := validate.HookExitCode(*provider, decision)
	if *recordDir != "" {
		if err := validate.RecordHookDecision(*recordDir, payload, *provider, decision, response, exitCode); err != nil {
			return 1, err
		}
	}
	if len(response) != 0 {
		if _, err := streams.Stdout.Write(response); err != nil {
			return 1, err
		}
	}
	return exitCode, nil
}

func runLifecycle(program string, args []string, streams IO) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("lifecycle subcommand is required (e.g. lifecycle verify)")
	}
	if isHelpArg(args[0]) {
		printLifecycleUsage(streams.Stdout, program)
		return 0, nil
	}
	if strings.HasPrefix(args[0], "-") {
		return 1, fmt.Errorf("lifecycle subcommand is required (e.g. lifecycle verify)")
	}
	sub, args := args[0], args[1:]
	switch sub {
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
		keep := fs.Bool("keep-temp", false, "keep the isolated workspace")
		binary := fs.String("binary", "", "formal-gates binary")
		format := fs.String("format", "json", "text or json")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		summary, result := validate.CodexHookCanary(validate.CodexHookCanaryOptions{Worktree: *worktree, CodexCommand: *command, KeepTemp: *keep, Binary: *binary})
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

type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }
func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

func rootFlags(fs *flag.FlagSet) (*string, *string) {
	return fs.String("root", ".", "repository root"), fs.String("package-root", ".", "installed formal-gates package root")
}

func readWorkflowInput(path string, stdin io.Reader, value any) error {
	reader := stdin
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()
		reader = file
	}
	decoder := json.NewDecoder(reader)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("expected one complete JSON object")
		}
		return err
	}
	return nil
}
func saveWorkflowCandidate(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	return facade.WriteOriginalPayload(path, append(data, '\n'), false)
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
	admission, err := validate.ResolveEngineAdmissionByRecord(request.InstalledTargetIdentity, request.AdmissionRegistryPath)
	if err != nil {
		return nil, true, err
	}
	if request.PackageDigest != admission.PackageDigest || request.InstalledTargetIdentity != admission.RecordID ||
		request.AdmissionGeneration != admission.Generation || request.AdmissionLease != admission.Lease || request.AdmissionToken != admission.Token {
		return nil, true, fmt.Errorf("UNREGISTERED_INSTALL: engine run identity does not match current admitted launcher")
	}
	f, err := facade.Open(root, runID, admission.RuntimeFiles)
	if err != nil {
		return nil, true, err
	}
	// Once the envelope has passed its version/identity barrier, enforce the
	// current project's target binding. This catches a run copied into another
	// project while preserving the more specific version diagnostic above.
	bound, bindingErr := validate.ResolveEngineAdmission(root, admission.Target, request.AdmissionRegistryPath)
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
	fmt.Fprintf(w, "Usage: %s <command>\n\nCommands:\n  package validate|route-candidates|baseline\n  registry admit|show\n  install\n  uninstall\n  workflow start|drive|submit|show|status|next|progress|message|replan|handoff|diagnose|guard-preview|serve\n  requirement show|append|edit|remove\n  settings show|save|set|remove|use|delete|execution-options\n  native show|respond\n  coverage validate|project-whitelist|reconcile-execution\n  gate run <ids...>|report\n  hook decide\n  lifecycle verify\n  canary portable|fault-matrix|codex-hook|codex-hook-probe\n", program)
}

func printCoverageUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s coverage <subcommand>\n\nSubcommands:\n  validate             validate a coverage contract JSON document from stdin\n  project-whitelist    project an approved whitelist from contract/reviews JSON on stdin\n  reconcile-execution  reconcile execution JSON against a contract and whitelist on stdin\n\nAll commands emit JSON on stdout; contract failures return a non-zero exit code and {code,path,message}.\n", program)
}

func printRegistryUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s registry <subcommand>\n\nSubcommands:\n  admit --path PATH --record-id ID\n  show --path PATH\n\nRegistry mutation is owned by install --bootstrap/install/uninstall.\n", program)
}

func printWorkflowUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s workflow <subcommand>\n\nSubcommands:\n  start|drive|submit|show|status|next|diagnose|guard-preview|progress|message|handoff|replan|serve\n\nRun `%s workflow <subcommand> --help` for a subcommand's flags.\n", program, program)
}

func printGateUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s gate <subcommand>\n\nSubcommands:\n  run --gate N  select discovered gate numbers and assemble detached review prompts\n  report        construct and display a standalone gate result from semantic flags\n\nRun `%s gate <subcommand> --help` for a subcommand's flags.\n", program, program)
}

func printHookUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s hook decide\n\nHook decision CLI: reads the host hook JSON payload from stdin and prints the JSON decision.\nFlags:\n  --provider <%s|''>  hook host provider (Codex and ZCode use host-specific JSON protocols)\n  --record-dir DIR  save original payload and the emitted local decision\n", program, providerHelp())
}

func printLifecycleUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s lifecycle <subcommand>\n\nSubcommands:\n  verify   verify a dispatch's lifecycle binding\n\nRun `%s lifecycle <subcommand> --help` for a subcommand's flags.\n", program, program)
}

func printCanaryUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s canary <subcommand>\n\nSubcommands:\n  portable        run the host-agnostic package/install canary\n  fault-matrix    exercise public install copy/switch/verify/recovery fixtures\n                  --fixture copy-component:prompts|verify-stage:installed-target selects one fixture\n  codex-hook      report Codex native hook interception qualification\n  codex-hook-probe  capture a hook payload to --payload-dir (for hook debugging)\n\nRun `%s canary <subcommand> --help` for a subcommand's flags.\n", program, program)
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

func readJSONFileInto(path string, value any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(value)
}

func runWorkflowProgress(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow progress", streams)
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "program integration: exact engine run id")
	runChoice := fs.Int("run", 0, "select a run by its displayed number")
	machine := fs.Bool("machine", false, "print complete evidence projection")
	after := fs.Uint64("after-revision", 0, "program integration: last observed progress revision")
	afterFile := fs.String("after-file", "", "program integration: exact original progress output file")
	afterChoice := fs.Int("after", 0, "original displayed saved progress position; 0 selects the unique payload")
	listProgress := fs.Bool("cursors", false, "list this run's original saved progress payloads")
	saveProgress := fs.Bool("save-progress", false, "save the complete original progress payload for later cursor selection")
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
		return 1, fmt.Errorf("current engine run not found")
	}
	if *afterChoice != 0 || hasFlagArgument(args, "after") || *listProgress {
		if *afterFile != "" || *after != 0 {
			return 1, fmt.Errorf("select original progress position, file or exact revision")
		}
		if *listProgress {
			return listOriginalPayloads(workflowPayloadDirectory(*root, *runID, "progress"), streams.Stdout)
		}
		path, done, err := selectOriginalPayload(workflowPayloadDirectory(*root, *runID, "progress"), *afterChoice, streams.Stdout)
		if err != nil {
			return 1, err
		}
		if !done {
			return 0, nil
		}
		*afterFile = path
	}
	if *afterFile != "" {
		if *after != 0 {
			return 1, fmt.Errorf("select original progress file or exact revision")
		}
		var cursor struct {
			Revision uint64 `json:"revision"`
			RunID    string `json:"runId"`
			Root     string `json:"root"`
		}
		var fields map[string]json.RawMessage
		if err := readJSONFileInto(*afterFile, &fields); err != nil {
			return 1, err
		}
		for _, field := range []struct {
			name  string
			value any
		}{{"revision", &cursor.Revision}, {"root", &cursor.Root}, {"runId", &cursor.RunID}} {
			if err := json.Unmarshal(fields[field.name], field.value); err != nil {
				return 1, fmt.Errorf("original progress %s: %w", field.name, err)
			}
		}
		if cursor.RunID != *runID {
			return 1, fmt.Errorf("progress cursor belongs to another run")
		}
		if cursor.Root == "" || !vcs.SamePath(cursor.Root, *root) {
			return 1, fmt.Errorf("progress cursor belongs to another project")
		}
		*after = cursor.Revision
	}
	progress, err := f.ProgressSince(*after, harness.NativeJobAlive)
	if err != nil {
		return 1, err
	}
	position := 0
	if *saveProgress {
		position, err = facade.SaveOriginalCandidate(workflowPayloadDirectory(*root, *runID, "progress"), progress)
		if err != nil {
			return 1, err
		}
	}
	if *machine {
		return printPayloadValue(streams.Stdout, progress, "progress", position)
	}
	run, err := f.Show()
	if err != nil {
		return 1, err
	}
	return printPayloadValue(streams.Stdout, workflowProgressView(progress, run), "progress", position)
}

func runWorkflowMessage(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow message", streams)
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "program integration: exact engine run id")
	runChoice := fs.Int("run", 0, "select a run by its displayed number")
	worker := fs.Int("worker", 0, "current worker number from show")
	text := fs.String("text", "", "new supplementary text")
	textFile := fs.String("text-file", "", "program integration: exact original supplementary text file")
	textPayload := fs.Int("text-payload", 0, "original displayed text payload for the selected worker")
	prepareText := fs.Bool("prepare-text", false, "save original stdin text bytes for this worker without sending")
	listText := fs.Bool("texts", false, "list this worker's original text payloads")
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
		return 1, fmt.Errorf("current engine run not found")
	}
	if *prepareText || *listText || *textPayload != 0 || hasFlagArgument(args, "text-payload") {
		if *text != "" || *textFile != "" {
			return 1, fmt.Errorf("choose authored text or original text payload")
		}
		directory, err := workerTextDirectory(f, *root, *runID, *worker)
		if err != nil {
			return 1, err
		}
		if *prepareText {
			if *listText || *textPayload != 0 {
				return 1, fmt.Errorf("prepare or select original text")
			}
			body, err := io.ReadAll(streams.Stdin)
			if err != nil {
				return 1, err
			}
			_, position, err := facade.SaveOriginalPayload(directory, body)
			return printValue(streams.Stdout, map[string]any{"textPayload": position, "text": string(body)}, err)
		}
		if *listText {
			return listOriginalPayloads(directory, streams.Stdout)
		}
		path, done, err := selectOriginalPayload(directory, *textPayload, streams.Stdout)
		if err != nil {
			return 1, err
		}
		if !done {
			return 0, nil
		}
		*textFile = path
	}
	if *textFile != "" {
		if *text != "" {
			return 1, fmt.Errorf("choose authored text or original text file")
		}
		body, err := os.ReadFile(*textFile)
		if err != nil {
			return 1, err
		}
		*text = string(body)
	}
	message, err := f.QueueWorkerMessage(*worker, *text)
	return printValue(streams.Stdout, message, err)
}

func runWorkflowHandoff(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow handoff", streams)
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "program integration: exact engine run id")
	runChoice := fs.Int("run", 0, "select a run by its displayed number")
	worker := fs.Int("worker", 0, "current developer number from show")
	reason := fs.String("reason", "", "actual user reason")
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
		return 1, fmt.Errorf("current engine run not found")
	}
	_, err = f.RequestWorkerHandoff(*worker, *reason)
	return printCurrentWorkflowRun(streams, f, err)
}
func runWorkflowReplan(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow replan", streams)
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "program integration: exact engine run id")
	runChoice := fs.Int("run", 0, "select a run by its displayed number")
	inputPath := fs.String("decision", "", "program integration: typed requirement change input file")
	save := fs.String("save-candidate", "", "save original control candidate for confirmation")
	arguments := registerControlArguments(fs)
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
		return 1, fmt.Errorf("current engine run not found")
	}
	control, provided, err := arguments.input()
	if err != nil {
		return 1, err
	}
	if provided && *inputPath != "" {
		return 1, fmt.Errorf("select semantic flags or machine decision input")
	}
	var input facade.RequirementChangeInput
	if provided {
		if control.Requirement == nil {
			return 1, fmt.Errorf("replan requires requirement semantic fields")
		}
		input = *control.Requirement
	} else if err := readWorkflowInput(*inputPath, streams.Stdin, &input); err != nil {
		return 1, err
	}
	candidate, err := f.PrepareReplan(input)
	if err != nil {
		return 1, err
	}
	position := 0
	if *save != "" {
		err = saveWorkflowCandidate(*save, candidate)
	} else {
		position, err = facade.SaveOriginalCandidate(workflowPayloadDirectory(*root, *runID, "control"), candidate)
	}
	if err != nil {
		return 1, err
	}
	return printPayloadValue(streams.Stdout, workflowConfirmationView(candidate), "confirmationCandidate", position)
}
