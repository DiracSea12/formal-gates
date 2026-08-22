package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

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
	host := fs.String("host", "", "target host: claude, codex, cursor, dsh, or both")
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
	host := fs.String("host", "", "target host: claude, codex, cursor, dsh, or both")
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
		return 1, fmt.Errorf("workflow subcommand is required (e.g. workflow start|show|resume|abort ...)")
	}
	if isHelpArg(args[0]) {
		printWorkflowUsage(streams.Stdout, program)
		return 0, nil
	}
	if strings.HasPrefix(args[0], "-") {
		return 1, fmt.Errorf("workflow subcommand is required (e.g. workflow start|show|resume|abort ...)")
	}
	sub, args := args[0], args[1:]
	if handler, ok := workflowSubcommands[sub]; ok {
		return handler(args, streams)
	}
	return 1, fmt.Errorf("unknown workflow subcommand: %s", sub)
}

// workflowSubcommands dispatches each workflow subcommand to its own runner. The
// record/prepare/carry-family subcommands reuse shared runners; the state-changing
// subcommands each own their flag parsing and validate call.
var workflowSubcommands = map[string]func(args []string, streams IO) (int, error){
	"start":              runWorkflowStart,
	"show":               runWorkflowShow,
	"diagnose":           runWorkflowDiagnose,
	"resume":             runWorkflowResume,
	"abort":              runWorkflowAbort,
	"reset":              runWorkflowReset,
	"requirement":        runWorkflowRequirement,
	"route-candidates":   runWorkflowRouteCandidates,
	"slicing":            runWorkflowSlicing,
	"settle-findings":    runWorkflowSettleFindings,
	"route":              runWorkflowRoute,
	"route-add":          runWorkflowRouteAdd,
	"qa-worktree":        runWorkflowQAWorktree,
	"prepare-gate":       runWorkflowPrepareGate,
	"prepare-action":     runWorkflowPrepareAction,
	"claim-dispatch":     runWorkflowClaimDispatch,
	"record-action":      runRecordAction,
	"record-gate":        runRecordGate,
	"qa-design":          runQADesign,
	"qa-review":          runQAReview,
	"qa-execution":       runQAExecution,
	"qa-execution-scope": runWorkflowQAExecutionScope,
	"snapshot":           runWorkflowSnapshot,
	"cleanup":            runWorkflowCleanup,
	"future":             runWorkflowFuture,
	"carry":              runCarry,
	"authorize-repair":   runWorkflowAuthorizeRepair,
	"seal":               runWorkflowSeal,
}

func runWorkflowStart(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow start", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "optional run id")
	flow := fs.String("flow", "formal", "workflow flow")
	req := fs.String("requirement", "", "requirement source path")
	vcs := fs.String("vcs", "", "external VCS name")
	base := fs.String("base-snapshot", "", "optional native base identity to verify")
	currentSnapshot := fs.String("current-snapshot", "", "explicit native identity to adopt as the current snapshot (must be an ancestor or equal of the native head); defaults to the native head (RQ-010)")
	artifacts := stringListFlag{}
	fs.Var(&artifacts, "requirement-artifact", "additional requirement or solution document; repeat as needed")
	retainedOverall := fs.Bool("retained-overall", false, "retain this run for merged slice integration")
	split := fs.String("split", "", "required split intent: yes or no; yes requires --retained-overall (保留总任务实例) or --master (切片实例); skipped for --route lightweight")
	master := fs.String("master", "", "retained-overall master run id for a slice instance start (with --split yes)")
	route := fs.String("route", "", "lightweight route declaration: --route lightweight creates the run but performs no verification (start → 需求登记 → Seal 三步直达, 只留记录); empty starts the regular intake")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.Start(validate.StartOptions{Root: *root, PackageRoot: *pkg, RunID: *runID, Flow: *flow, RequirementSource: *req, RequirementArtifacts: artifacts, VCS: *vcs, BaseSnapshot: *base, CurrentSnapshot: *currentSnapshot, RetainedOverall: *retainedOverall, Split: *split, MasterRunID: *master, Route: *route})
	return printValue(streams.Stdout, state, err)
}

func runWorkflowShow(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow show", streams)
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "run id")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.LoadRunState(*root, *runID)
	return printValue(streams.Stdout, state, err)
}

func runWorkflowDiagnose(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow diagnose", streams)
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "run id whose raw state should be diagnosed")
	path := fs.String("path", "", "raw state or terminal summary path")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	statePath := strings.TrimSpace(*path)
	if statePath == "" {
		if strings.TrimSpace(*runID) == "" {
			return 1, fmt.Errorf("workflow diagnose requires --path or --run-id")
		}
		statePath = validate.RunStatePath(*root, *runID)
		if _, statErr := os.Stat(statePath); os.IsNotExist(statErr) {
			// Terminal runs intentionally remove .gates/tmp/<run-id>/state.json;
			// diagnose --run-id must fall back to the retained summary.
			statePath = validate.RunSummaryPath(*root, *runID)
		}
	}
	report, err := validate.DiagnoseState(statePath)
	return printValue(streams.Stdout, report, err)
}

func runWorkflowResume(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow resume", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	adoptExternal := fs.Bool("adopt-external", false, "explicitly rebind the current snapshot to the drifted native head and record the reason")
	reason := fs.String("reason", "", "main-agent justification when adopting an external change")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	if *adoptExternal {
		state, err := validate.AdoptExternalChange(*root, *pkg, *runID, *reason)
		return printValue(streams.Stdout, state, err)
	}
	report, err := validate.ResumeReport(*root, *pkg, *runID)
	if err != nil {
		return 1, err
	}
	state, err := validate.LoadRunState(*root, *runID)
	if err != nil {
		return 1, err
	}
	return printJSON(streams.Stdout, map[string]any{"classificationRequired": report.ClassificationRequired, "catalogDelta": report.CatalogDelta, "nativeDrifted": report.NativeDrifted, "isolationDrifted": report.IsolationDrifted, "state": state})
}

func runWorkflowAbort(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow abort", streams)
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run-id", "", "run id")
	userConfirm := fs.Bool("user-confirm", false, "user-level confirmation required to abort this run; the main agent cannot trigger abort alone")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	// 需求 6 第 2 条：abort 误触第一时间硬阻断。未经用户级确认不执行、不落任何状态
	// （确认前返回，validate.Abort 不会被调用）。
	if !*userConfirm {
		return 1, fmt.Errorf("workflow abort requires --user-confirm (用户级确认): aborting a run is a destructive flow-state action the main agent cannot trigger alone")
	}
	summary, err := validate.Abort(*root, *runID)
	return printValue(streams.Stdout, summary, err)
}

func runWorkflowReset(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow reset", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	userApprove := fs.Bool("user-approve", false, "user-level authorization required to reset this run's flow state; the main agent cannot trigger reset alone")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	// 需求 5：用户权限门控的流程重置。未授权拒绝执行、不落任何状态（确认前返回，
	// validate.ResetRun 不会被调用）。
	if !*userApprove {
		return 1, fmt.Errorf("workflow reset requires --user-approve (用户显式授权): resetting a run's flow state is destructive and the main agent cannot trigger it alone")
	}
	result, err := validate.ResetRun(*root, *pkg, *runID)
	return printValue(streams.Stdout, result, err)
}

func runWorkflowRequirement(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow requirement", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	source := fs.String("source", "", "requirement source path; defaults to the current source")
	confirmed := fs.Bool("confirmed", false, "mark this exact requirement revision confirmed")
	meaning := fs.String("meaning", "", "semantic effect for a changed revision: preserved or changed")
	var artifacts stringListFlag
	fs.Var(&artifacts, "requirement-artifact", "complete additional requirement artifact set; repeat as needed")
	clearArtifacts := fs.Bool("clear-requirement-artifacts", false, "replace the set with the primary requirement only")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	var artifactSet []string
	if *clearArtifacts && len(artifacts) != 0 {
		return 1, fmt.Errorf("--clear-requirement-artifacts cannot be combined with --requirement-artifact")
	}
	if *clearArtifacts {
		artifactSet = []string{}
	} else if artifacts != nil {
		artifactSet = artifacts
	}
	state, err := validate.UpdateRequirement(*root, *pkg, *runID, *source, *confirmed, *meaning, artifactSet)
	return printValue(streams.Stdout, state, err)
}

func runWorkflowRouteCandidates(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow route-candidates", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	candidates, err := validate.RouteCandidates(*root, *pkg, *runID)
	return printValue(streams.Stdout, candidates, err)
}

func runWorkflowSlicing(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow slicing", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	decision := fs.String("decision", "", "split or no-split")
	count := fs.Int("count", 0, "subtask count for a split decision (>= 2)")
	slices := stringListFlag{}
	fs.Var(&slices, "slice", "slice definition; repeat as needed")
	parallel := fs.String("parallel", "", "parallel suggestion (which subtasks may run concurrently)")
	note := fs.String("note", "", "reason trace; required for no-split (建议不拆原因)")
	master := fs.String("master", "", "retained-overall master run id for a slice instance split decision")
	// --user-confirm：拆分决定与启动拆分声明冲突时的用户确认声明修订——--split no 的 run
	// 记 split 自升保留总任务实例（不重启）、保留总任务实例记 no-split 降级解死端；修订理由
	// 必填（--note）。切片实例（--master）不可经修订脱钩；绑定点仍是本次记录（记录后不重切）。
	userConfirm := fs.Bool("user-confirm", false, "user-confirmed amendment of the start split declaration when the slicing decision contradicts it: --split no -> split promotes the run to retained-overall without a restart; retained-overall -> no-split demotes it to a single run; the amendment reason (--note) is required")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.RecordSlicing(*root, *pkg, *runID, *decision, *count, slices, *parallel, *note, *master, validate.SlicingAmendOptions{UserConfirm: *userConfirm})
	return printValue(streams.Stdout, state, err)
}

func runWorkflowSettleFindings(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow settle-findings", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	action := fs.String("action", "", "product-review or start-readiness")
	confirm := stringListFlag{}
	fs.Var(&confirm, "confirm", "finding the user confirms as a real problem (需修订); repeat as needed")
	dismiss := stringListFlag{}
	fs.Var(&dismiss, "dismiss", "finding the user dismisses as not a problem (作废); repeat as needed")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.RecordSettledFindings(*root, *pkg, *runID, *action, confirm, dismiss)
	return printValue(streams.Stdout, state, err)
}

func runWorkflowRoute(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow route", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	mode := fs.String("mode", "", "lightweight, full, or custom")
	gates := stringListFlag{}
	fs.Var(&gates, "gate", "selected gate id; repeat for custom route")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.SetRoute(*root, *pkg, *runID, *mode, gates)
	return printValue(streams.Stdout, state, err)
}

func runWorkflowRouteAdd(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow route-add", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	gates := stringListFlag{}
	fs.Var(&gates, "gate", "gate id to add; repeat as needed")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.AddRouteGates(*root, *pkg, *runID, gates)
	return printValue(streams.Stdout, state, err)
}

func runWorkflowQAWorktree(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow qa-worktree", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	worktree := fs.String("worktree", "", "QA isolation worktree path (created from the base snapshot by the host)")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.RegisterQAWorktree(*root, *pkg, *runID, *worktree)
	return printValue(streams.Stdout, state, err)
}

func runWorkflowPrepareGate(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow prepare-gate", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	gate := fs.String("gate", "", "discovered gate id")
	userRequested := fs.Bool("user-requested", false, "user explicitly authorizes a fresh dispatch instead of resuming the interrupted claimed subagent (records the authorization source)")
	userReason := fs.String("user-reason", "", "user reason for the override")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	prompt, err := validate.PrepareGate(*root, *pkg, *runID, *gate, *userRequested, *userReason)
	if err != nil {
		return 1, err
	}
	fmt.Fprint(streams.Stdout, prompt)
	emitParallelReminder(streams, *root, *runID)
	return 0, nil
}

func runWorkflowPrepareAction(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow prepare-action", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	action := fs.String("action", "", "installed action prompt id")
	mode := fs.String("mode", "", "blackbox or whitebox for qa-design/qa-review")
	userRequested := fs.Bool("user-requested", false, "user explicitly requests an override (e.g. re-review an already-passed round)")
	userReason := fs.String("user-reason", "", "user reason for the override")
	var scope stringListFlag
	fs.Var(&scope, "scope", "requirement item to include in an incremental product-review / start-readiness; repeat as needed (RQ-012)")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	prompt, err := validate.PrepareAction(*root, *pkg, *runID, *action, *mode, *userRequested, *userReason, scope...)
	if err != nil {
		return 1, err
	}
	fmt.Fprint(streams.Stdout, prompt)
	emitParallelReminder(streams, *root, *runID)
	return 0, nil
}

func runWorkflowClaimDispatch(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow claim-dispatch", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	dispatch := fs.String("dispatch", "", "prepared dispatch id")
	reviewer := fs.String("reviewer", "", "host reviewer or session identity")
	provider := fs.String("provider", "", "explicit lifecycle host provider (required when a shared launcher has multiple admitted hosts)")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.ClaimDispatchWithProvider(*root, *pkg, *runID, *dispatch, *reviewer, *provider)
	return workflowResult(streams, *root, *runID, state, err)
}

func runWorkflowFuture(args []string, streams IO) (int, error) {
	if len(args) == 0 || isHelpArg(args[0]) {
		return 0, printFutureUsage(streams.Stdout, "formal-gates")
	}
	action, args := args[0], args[1:]
	switch action {
	case "generate":
		fs := newFlagSet("workflow future generate", streams)
		root := fs.String("root", ".", "package root")
		packageDigest := fs.String("package-digest", "", "immutable package digest to include")
		output := fs.String("output", "", "optional envelope output path")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		envelope, err := validate.GenerateFutureEnvelope(*root, *packageDigest)
		if err != nil {
			return 1, err
		}
		if strings.TrimSpace(*output) != "" {
			if err := validate.WriteFutureEnvelope(*root, *output, envelope); err != nil {
				return 1, err
			}
		}
		return printValue(streams.Stdout, envelope, nil)
	case "write":
		name := "workflow future write"
		fs := newFlagSet(name, streams)
		root := fs.String("root", ".", "package root")
		path := fs.String("path", "", "versioned future state output path")
		output := fs.String("output", "", "alias for --path")
		envelopePath := fs.String("envelope", "", "validated envelope JSON path")
		input := fs.String("input", "", "alias for --envelope")
		packageDigest := fs.String("package-digest", "", "immutable package digest to include when generating an envelope")
		payload := fs.String("payload", "", "JSON payload to place in the future state document")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		target := strings.TrimSpace(*path)
		if target == "" {
			target = strings.TrimSpace(*output)
		}
		if target == "" {
			return 1, fmt.Errorf("%s requires --path or --output", name)
		}
		envelopeFile := strings.TrimSpace(*envelopePath)
		if envelopeFile == "" {
			envelopeFile = strings.TrimSpace(*input)
		}
		var envelope validate.VersionEnvelope
		var err error
		if envelopeFile != "" {
			envelope, err = validate.LoadFutureEnvelope(*root, envelopeFile)
		} else {
			envelope, err = validate.GenerateFutureEnvelope(*root, *packageDigest)
		}
		if err != nil {
			return 1, err
		}
		var value any
		if strings.TrimSpace(*payload) != "" {
			if err := json.Unmarshal([]byte(*payload), &value); err != nil {
				return 1, fmt.Errorf("future payload JSON is invalid: %w", err)
			}
		}
		err = validate.WriteFutureState(*root, target, envelope, value)
		return printValue(streams.Stdout, envelope, err)
	case "view":
		fs := newFlagSet("workflow future view", streams)
		root := fs.String("root", ".", "package root")
		path := fs.String("path", "", "future state or envelope path")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if strings.TrimSpace(*path) == "" {
			return 1, fmt.Errorf("workflow future view requires --path")
		}
		report, err := validate.DiagnoseFutureState(*root, *path)
		return printValue(streams.Stdout, report, err)
	default:
		return 1, fmt.Errorf("unknown workflow future action: %s", action)
	}
}

func runWorkflowQAExecutionScope(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow qa-execution-scope", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	mode := fs.String("mode", "", "QA dispatch mode: blackbox, whitebox, or empty for the merged set")
	decision := fs.String("decision", "", "FULL or AFFECTED")
	cases := fs.String("cases", "", "AFFECTED subset of approved case ids, comma separated")
	reason := fs.String("reason", "", "scope decision reason")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	var caseIDs []string
	for _, id := range strings.Split(*cases, ",") {
		if strings.TrimSpace(id) != "" {
			caseIDs = append(caseIDs, strings.TrimSpace(id))
		}
	}
	state, err := validate.RecordExecutionScope(*root, *pkg, *runID, *mode, *decision, caseIDs, *reason)
	return workflowResult(streams, *root, *runID, state, err)
}

func runWorkflowSnapshot(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow snapshot", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	dispatch := fs.String("dispatch", "", "prepared Development or Repair dispatch id")
	userRequested := fs.Bool("user-requested", false, "user explicitly releases the blackbox QA gate when it has not passed (records the authorization source)")
	reason := fs.String("reason", "", "user reason for the manual blackbox gate release")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.AdvanceSnapshot(*root, *pkg, *runID, *dispatch, *userRequested, *reason)
	return workflowResult(streams, *root, *runID, state, err)
}

func runWorkflowCleanup(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow cleanup", streams)
	root := fs.String("root", ".", "repository root")
	runID := fs.String("run", "", "explicitly delete this run's temp directory (terminated or not)")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	if *runID != "" {
		deleted, err := validate.CleanupTempRun(*root, *runID)
		return printValue(streams.Stdout, map[string]any{"deleted": deleted}, err)
	}
	result, err := validate.CleanupTempRuns(*root)
	return printValue(streams.Stdout, result, err)
}

func runWorkflowAuthorizeRepair(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow authorize-repair", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	cycles := fs.Int("cycles", 1, "must be 1; each invocation authorizes one additional review wave")
	// 轮次上限处与"是否授权再来一轮"同一交互打包 QA 重跑 scope 决策；三个参数
	// 均可重复、按 mode 分组（如 --qa-scope blackbox=AFFECTED --qa-cases blackbox=CASE-002）。
	scopeInputs := map[string]*validate.QAScopeInput{}
	fs.Var(&qaScopeValue{byMode: scopeInputs, field: "scope"}, "qa-scope", "QA rerun scope decision <mode>=<FULL|AFFECTED>; empty <mode> selects the merged QA set; repeat per mode")
	fs.Var(&qaScopeValue{byMode: scopeInputs, field: "cases"}, "qa-cases", "AFFECTED subset <mode>=<id,...>; empty <mode> selects the merged QA set; repeat per mode")
	fs.Var(&qaScopeValue{byMode: scopeInputs, field: "reason"}, "qa-reason", "scope decision reason <mode>=<reason>; empty <mode> selects the merged QA set; repeat per mode")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	scopes := make([]validate.QAScopeInput, 0, len(scopeInputs))
	for _, item := range scopeInputs {
		scopes = append(scopes, *item)
	}
	state, err := validate.AuthorizeExtraRepair(*root, *pkg, *runID, *cycles, scopes)
	return workflowResult(streams, *root, *runID, state, err)
}

func runWorkflowSeal(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow seal", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	skips := stringListFlag{}
	fs.Var(&skips, "skip", "selected non-passing gate explicitly authorized to skip")
	userRequested := fs.Bool("user-requested", false, "user explicitly requests the FAIL skips before the review-wave limit is exhausted")
	squashMessage := fs.String("squash-message", "", "combined commit message when seal squashes the git base-to-current range")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	summary, err := validate.Seal(*root, *pkg, *runID, skips, *userRequested, *squashMessage)
	return workflowResult(streams, *root, *runID, summary, err)
}

// emitParallelReminder runs the parallel check for a run and prints any
// reminder to stderr (never stdout, keeping the machine JSON clean). It is the
// single shared emission point used both after state-changing workflow commands
// and by the lifecycle capture hook trigger. It is read-only for the workflow: it
// only reads the run state and a run-scoped cooldown marker, never touching
// dispatches, lifecycle events, or review results.
func emitParallelReminder(streams IO, root, runID string) {
	if strings.TrimSpace(root) == "" || strings.TrimSpace(runID) == "" {
		return
	}
	advice, remind := validate.ParallelCheck(root, runID, time.Now())
	if remind && strings.TrimSpace(advice.Message) != "" {
		fmt.Fprintln(streams.Stderr, advice.Message)
	}
}

// workflowResult wraps printValue for state-changing workflow commands so the
// parallel reminder is emitted after a successful dispatch-state change
// (show and other read-only commands do not use it).
func workflowResult(streams IO, root, runID string, value any, err error) (int, error) {
	if err == nil {
		emitParallelReminder(streams, root, runID)
	}
	return printValue(streams.Stdout, value, err)
}

func runRecordAction(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow record-action", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	action := fs.String("action", "", "installed action id")
	status := fs.String("status", "", "PASS, FAIL, or RUNTIME_ERROR")
	message := fs.String("message", "", "runtime or result message")
	dispatch := fs.String("dispatch", "", "prepared dispatch id returned in the task")
	userRequested := fs.Bool("user-requested", false, "user explicitly requests an override of the review rule (records the authorization source)")
	userReason := fs.String("user-reason", "", "user reason for the override")
	findings := newFindingFlags(fs)
	items := []validate.ReviewItemInput{}
	newReviewItemStart(fs, &items)
	newReviewItemField(fs, "item-status", "PASS or FAIL for the current review item", "item-status", &items)
	newReviewItemField(fs, "item-reason", "required finding reason for a FAIL item", "item-reason", &items)
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.RecordAction(*root, *pkg, *runID, *action, *dispatch, *status, *message, *findings, *userRequested, *userReason, items...)
	return workflowResult(streams, *root, *runID, state, err)
}

func runRecordGate(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow record-gate", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	gate := fs.String("gate", "", "discovered gate id")
	status := fs.String("status", "", "PASS, FAIL, or RUNTIME_ERROR")
	message := fs.String("message", "", "runtime or result message")
	dispatch := fs.String("dispatch", "", "prepared dispatch id returned in the task")
	compared := fs.String("compared", "", "exact snapshot pair the reviewer compared, as <base>..<current>")
	findings := newFindingFlags(fs)
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.RecordGate(*root, *pkg, *runID, *gate, *dispatch, *status, *message, *compared, *findings)
	return workflowResult(streams, *root, *runID, state, err)
}

func runQADesign(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow qa-design", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	runtimeError := fs.String("runtime-error", "", "QA Design runtime error")
	dispatch := fs.String("dispatch", "", "prepared dispatch id returned in the task")
	cases := []validate.QACaseInput{}
	newQACaseStart(fs, &cases)
	// --case-id 是增量契约的修改引用：给当前用例标一个既有 CASE id，表示本轮修改该用例
	// （id 必须已存在且属于本派发 mode）。不带 --case-id 的用例是新增（CLI 分配全局唯一 id）。
	newQACaseField(fs, "case-id", "existing CASE id this design round modifies (incremental contract; the id must already exist in this mode)", "case-id", &cases)
	newQACaseField(fs, "mode", "blackbox or whitebox for the current QA case", "mode", &cases)
	newQACaseField(fs, "procedure", "procedure for the current QA case", "procedure", &cases)
	newQACaseField(fs, "oracle", "oracle for the current QA case", "oracle", &cases)
	newQACaseField(fs, "test", "whitebox test reference <file>::<function> locating the delivered test implementing the current QA case (RQ-013 binding; required for whitebox cases, unique per case)", "test", &cases)
	// --remove-case / --replace-all 是增量记录之外的显式操作：前者按 id 删除（可重复），
	// 后者整体替换本 mode 用例集（替换空集即清空该 mode），二者互斥。
	removeCases := stringListFlag{}
	fs.Var(&removeCases, "remove-case", "remove an existing CASE id from this mode (repeatable; the id must exist)")
	replaceAll := fs.Bool("replace-all", false, "replace this mode's whole QA case set with the submitted cases (an empty submission clears the mode)")
	// --per-suggestion 吸收该 mode 最近一次 qa-review 记录在案的 P2 集合级建议：本轮
	// 新增/修改用例直接置为已批准（SUGGESTION_APPLIED 溯源），不派新 qa-review；要求该
	// mode 最近一次 qa-review 结果为 PASS 且含 P2 集合级发现项，不能与 --replace-all 同用。
	perSuggestion := fs.Bool("per-suggestion", false, "absorb the mode's recorded P2 set-level qa-review suggestions directly: cases submitted this round are recorded approved (SUGGESTION_APPLIED provenance) without a new qa-review round; requires the mode's latest qa-review PASS with P2 set findings; cannot combine with --replace-all")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.RecordQADesign(*root, *pkg, *runID, *dispatch, cases, *runtimeError, validate.QADesignRecordOptions{RemoveCases: removeCases, ReplaceAll: *replaceAll, PerSuggestion: *perSuggestion})
	return workflowResult(streams, *root, *runID, state, err)
}

func runQAReview(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow qa-review", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	dispatch := fs.String("dispatch", "", "prepared dispatch id returned in the task")
	runtimeError := fs.String("runtime-error", "", "QA Review runtime error")
	decisions := []validate.QAReviewInput{}
	newQAReviewStart(fs, &decisions)
	newQAReviewField(fs, "outcome", "PASS or FAIL", "outcome", &decisions)
	newQAReviewField(fs, "reason", "required reason for FAIL", "reason", &decisions)
	findings := newFindingFlags(fs)
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.RecordQAReview(*root, *pkg, *runID, *dispatch, decisions, *runtimeError, *findings)
	return workflowResult(streams, *root, *runID, state, err)
}

func runQAExecution(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow qa-execution", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	runtimeError := fs.String("runtime-error", "", "QA execution runtime error")
	dispatch := fs.String("dispatch", "", "prepared dispatch id returned in the task")
	results := []validate.QAResultInput{}
	newQAResultStart(fs, &results)
	for _, item := range []struct{ name, field, usage string }{{"outcome", "outcome", "PASS or FAIL"}, {"procedure", "procedure", "executed procedure"}, {"observation", "observation", "observed result"}, {"oracle-result", "oracle-result", "oracle comparison"}} {
		newQAResultField(fs, item.name, item.usage, item.field, &results)
	}
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.RecordQAExecution(*root, *pkg, *runID, *dispatch, results, *runtimeError)
	return workflowResult(streams, *root, *runID, state, err)
}

func runCarry(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow carry", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	runtimeError := fs.String("runtime-error", "", "Carry runtime error")
	mainAgent := fs.Bool("main-agent", false, "inherit every prior PASS without independent Carry")
	mainReason := fs.String("main-reason", "", "main-agent reason from the immediate repair comparison")
	dispatch := fs.String("dispatch", "", "prepared Carry dispatch id")
	decisions := []validate.CarryInput{}
	newCarryStart(fs, &decisions)
	newCarryField(fs, "decision", "INHERIT or RERUN", "decision", &decisions)
	newCarryField(fs, "reason", "semantic decision reason", "reason", &decisions)
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.RecordCarry(*root, *pkg, *runID, *dispatch, decisions, *runtimeError, *mainAgent, *mainReason)
	return workflowResult(streams, *root, *runID, state, err)
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
	provider := fs.String("provider", "", "hook host provider: codex, claude-code, cursor, deepseek-harness, or empty for the generic protocol")
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
	decision, err := validate.Hook(payload)
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
		provider := fs.String("provider", "", "host provider: claude-code, codex, cursor, or deepseek-harness")
		event := fs.String("event", "", "provider lifecycle event name")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		payload, err := io.ReadAll(streams.Stdin)
		if err != nil {
			return 1, err
		}
		result, err := lifecycle.Capture(*root, *provider, *event, payload)
		// 生命周期 hook（子代理启动/停止）时也触发并行检查——事件落盘后对每个仓库根
		// 的活动 run 复用公共提醒发射点，提醒写 stderr、不干扰生命周期与派发状态。
		if err == nil {
			for _, capturedRoot := range result.Roots {
				runIDs, runErr := lifecycle.ActiveRunIDs(capturedRoot)
				if runErr != nil {
					continue
				}
				for _, runID := range runIDs {
					emitParallelReminder(streams, capturedRoot, runID)
				}
			}
		}
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

type findingStart struct{ items *[]validate.FindingInput }

func (f *findingStart) String() string { return "" }
func (f *findingStart) Set(v string) error {
	*f.items = append(*f.items, validate.FindingInput{Message: v})
	return nil
}

type findingLocation struct{ items *[]validate.FindingInput }

func (f *findingLocation) String() string { return "" }
func (f *findingLocation) Set(v string) error {
	if len(*f.items) == 0 {
		return fmt.Errorf("--location must follow --finding")
	}
	last := len(*f.items) - 1
	(*f.items)[last].Locations = append((*f.items)[last].Locations, v)
	return nil
}

type findingSeverity struct{ items *[]validate.FindingInput }

func (f *findingSeverity) String() string { return "" }
func (f *findingSeverity) Set(v string) error {
	if len(*f.items) == 0 {
		return fmt.Errorf("--severity must follow --finding")
	}
	last := len(*f.items) - 1
	if (*f.items)[last].Severity != "" {
		return fmt.Errorf("duplicate --severity for current finding")
	}
	(*f.items)[last].Severity = v
	return nil
}
func newFindingFlags(fs *flag.FlagSet) *[]validate.FindingInput {
	items := []validate.FindingInput{}
	fs.Var(&findingStart{&items}, "finding", "start a finding with its message")
	fs.Var(&findingSeverity{&items}, "severity", "P0, P1, P2, or P3 for the current gate finding")
	fs.Var(&findingLocation{&items}, "location", "repository location for the current finding")
	return &items
}

// qaScopeValue parses the repeatable authorize-repair QA scope flags
// "--qa-scope <mode>=<value>" / "--qa-cases..." / "--qa-reason...". All
// three flags share one map keyed by mode, so a mode's scope decision, cases, and
// reason can be supplied in any order and grouped per mode. An empty <mode> (a
// leading '=') selects the merged / single-dispatch QA set.
type qaScopeValue struct {
	byMode map[string]*validate.QAScopeInput
	field  string
}

func (v *qaScopeValue) String() string { return "" }

func (v *qaScopeValue) Set(raw string) error {
	mode, value, ok := strings.Cut(raw, "=")
	if !ok {
		return fmt.Errorf("--qa-%s must be <mode>=<value> (an empty <mode> selects the merged QA set)", v.field)
	}
	mode = strings.TrimSpace(mode)
	if v.field == "scope" && strings.TrimSpace(value) == "" {
		return fmt.Errorf("--qa-scope requires a decision value after '='")
	}
	item, exists := v.byMode[mode]
	if !exists {
		item = &validate.QAScopeInput{Mode: mode}
		v.byMode[mode] = item
	}
	switch v.field {
	case "scope":
		item.Decision = value
	case "cases":
		for _, id := range strings.Split(value, ",") {
			if strings.TrimSpace(id) != "" {
				item.CaseIDs = append(item.CaseIDs, strings.TrimSpace(id))
			}
		}
	case "reason":
		item.Reason = value
	}
	return nil
}

type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }
func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

// itemStartFlag is the shared start-flag template for the repeatable record
// flag families (QA cases / QA review decisions / QA results / review items /
// carry decisions): every Set appends a new item built from the flag value.
type itemStartFlag[T any] struct {
	items    *[]T
	makeItem func(string) T
}

func (f *itemStartFlag[T]) String() string { return "" }
func (f *itemStartFlag[T]) Set(v string) error {
	*f.items = append(*f.items, f.makeItem(v))
	return nil
}

// itemFieldFlag is the shared field-flag template for the repeatable record flag
// families: it sets one field of the most recently started item, rejects a field
// with no item yet, and rejects a duplicate assignment for the same item.
type itemFieldFlag[T any] struct {
	items         *[]T
	field         string
	followFlag    string // the start flag the field must follow (for the message)
	followLabel   string // human label of the item group for the follow message
	itemLabel     string // human label of the item for the duplicate message
	assignedGroup int
	setField      func(*T, string)
}

func (f *itemFieldFlag[T]) String() string { return "" }
func (f *itemFieldFlag[T]) Set(v string) error {
	if len(*f.items) == 0 {
		return fmt.Errorf("%s must follow --%s", f.followLabel, f.followFlag)
	}
	if f.assignedGroup == len(*f.items) {
		return fmt.Errorf("duplicate --%s for current %s", f.field, f.itemLabel)
	}
	f.setField(&(*f.items)[len(*f.items)-1], v)
	f.assignedGroup = len(*f.items)
	return nil
}

// newItemStartFlag registers a start flag that appends an item per occurrence.
func newItemStartFlag[T any](fs *flag.FlagSet, name, usage string, items *[]T, makeItem func(string) T) {
	fs.Var(&itemStartFlag[T]{items: items, makeItem: makeItem}, name, usage)
}

// newItemFieldFlag registers a field flag that sets one field of the most recent
// item. followFlag is the start flag name, followLabel and itemLabel are the
// human labels used in the error messages, and setField writes the parsed value.
func newItemFieldFlag[T any](fs *flag.FlagSet, name, usage string, items *[]T, field, followFlag, followLabel, itemLabel string, setField func(*T, string)) {
	fs.Var(&itemFieldFlag[T]{items: items, field: field, followFlag: followFlag, followLabel: followLabel, itemLabel: itemLabel, setField: setField}, name, usage)
}

func newQACaseStart(fs *flag.FlagSet, cases *[]validate.QACaseInput) {
	newItemStartFlag(fs, "case", "start a QA case with its behavior description", cases, func(v string) validate.QACaseInput {
		return validate.QACaseInput{Description: v}
	})
}

func newQACaseField(fs *flag.FlagSet, name, usage, field string, cases *[]validate.QACaseInput) {
	newItemFieldFlag(fs, name, usage, cases, field, "case", "case field", "QA case", func(p *validate.QACaseInput, v string) {
		switch field {
		case "case-id":
			p.CaseID = v
		case "mode":
			p.Mode = v
		case "procedure":
			p.Procedure = v
		case "test":
			p.Test = v
		default:
			p.Oracle = v
		}
	})
}

func newQAReviewStart(fs *flag.FlagSet, items *[]validate.QAReviewInput) {
	newItemStartFlag(fs, "case", "start a decision for a pending CASE id", items, func(v string) validate.QAReviewInput {
		return validate.QAReviewInput{CaseID: v}
	})
}

func newQAReviewField(fs *flag.FlagSet, name, usage, field string, items *[]validate.QAReviewInput) {
	newItemFieldFlag(fs, name, usage, items, field, "case", "QA Review field", "QA Review decision", func(p *validate.QAReviewInput, v string) {
		if field == "outcome" {
			p.Outcome = v
		} else {
			p.Reason = v
		}
	})
}

func newQAResultStart(fs *flag.FlagSet, results *[]validate.QAResultInput) {
	newItemStartFlag(fs, "case-result", "start a result using its generated CASE id", results, func(v string) validate.QAResultInput {
		return validate.QAResultInput{CaseID: v}
	})
}

func newQAResultField(fs *flag.FlagSet, name, usage, field string, results *[]validate.QAResultInput) {
	newItemFieldFlag(fs, name, usage, results, field, "case-result", "QA result field", "QA result", func(p *validate.QAResultInput, v string) {
		switch field {
		case "outcome":
			p.Outcome = v
		case "procedure":
			p.Procedure = v
		case "observation":
			p.Observation = v
		case "oracle-result":
			p.OracleResult = v
		}
	})
}

func newReviewItemStart(fs *flag.FlagSet, items *[]validate.ReviewItemInput) {
	newItemStartFlag(fs, "item", "start a requirement item decision for the incremental review (RQ-012)", items, func(v string) validate.ReviewItemInput {
		return validate.ReviewItemInput{Key: v}
	})
}

func newReviewItemField(fs *flag.FlagSet, name, usage, field string, items *[]validate.ReviewItemInput) {
	newItemFieldFlag(fs, name, usage, items, field, "item", "review item field", "review item", func(p *validate.ReviewItemInput, v string) {
		if field == "item-status" {
			p.Status = v
		} else {
			p.Reason = v
		}
	})
}

func newCarryStart(fs *flag.FlagSet, items *[]validate.CarryInput) {
	newItemStartFlag(fs, "gate", "start a Carry decision for a gate", items, func(v string) validate.CarryInput {
		return validate.CarryInput{GateID: v}
	})
}

func newCarryField(fs *flag.FlagSet, name, usage, field string, items *[]validate.CarryInput) {
	newItemFieldFlag(fs, name, usage, items, field, "gate", "Carry field", "Carry decision", func(p *validate.CarryInput, v string) {
		if field == "decision" {
			p.Decision = v
		} else {
			p.Message = v
		}
	})
}
func rootFlags(fs *flag.FlagSet) (*string, *string) {
	return fs.String("root", ".", "repository root"), fs.String("package-root", ".", "installed formal-gates package root")
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
	fmt.Fprintf(w, "Usage: %s <command>\n\nCommands:\n  package validate|route-candidates|baseline\n  registry admit|show\n  install\n  uninstall\n  workflow start|show|diagnose|resume|abort|reset|requirement|route-candidates|route|route-add|slicing|settle-findings|qa-worktree|prepare-gate|prepare-action|claim-dispatch|record-action|record-gate|qa-design|qa-review|qa-execution|qa-execution-scope|snapshot|future|carry|authorize-repair|seal|cleanup\n  gate run <ids...>|report\n  hook decide\n  lifecycle capture|verify\n  canary portable|fault-matrix|codex-hook|codex-hook-probe\n", program)
}

func printRegistryUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s registry <subcommand>\n\nSubcommands:\n  admit --path PATH --record-id ID\n  show --path PATH\n\nRegistry mutation is owned by install --bootstrap/install/uninstall.\n", program)
}

// 二层 --help 粒度统一：workflow/gate/hook/lifecycle/canary 各自打印本组的子命令清单，
// 而不是回落到顶层 usage（P2 修复）。叶子命令（如 workflow show --help）仍打印该子命令
// 自己的 flag usage。
func printWorkflowUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s workflow <subcommand>\n\nSubcommands:\n  start|show|diagnose|resume|abort|reset|requirement|route-candidates|route|route-add|slicing|settle-findings|qa-worktree|prepare-gate|prepare-action|claim-dispatch|record-action|record-gate|qa-design|qa-review|qa-execution|qa-execution-scope|snapshot|future|carry|authorize-repair|seal|cleanup\n\nRun `%s workflow <subcommand> --help` for a subcommand's flags.\n", program, program)
}

func printFutureUsage(w io.Writer, program string) error {
	fmt.Fprintf(w, "Usage: %s workflow future <generate|write|view>\n\nGenerate, inspect, or write the versioned candidate envelope derived from definitions/workflow.json through its owning future writer.\n", program)
	return nil
}

func printGateUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s gate <subcommand>\n\nSubcommands:\n  run <ids...>  assemble one detached review prompt per gate id (outside any run)\n  report        validate and display a standalone gate review result from stdin\n\nRun `%s gate <subcommand> --help` for a subcommand's flags.\n", program, program)
}

func printHookUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s hook decide\n\nHook decision CLI: reads the host hook JSON payload from stdin and prints the JSON decision.\nFlags:\n  --provider <codex|claude-code|cursor|deepseek-harness|''>  hook host provider (codex uses the Codex JSON block protocol)\n", program)
}

func printLifecycleUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s lifecycle <subcommand>\n\nSubcommands:\n  capture  record a host lifecycle event (subagent start/stop) from the host payload on stdin\n  verify   verify a dispatch's lifecycle binding\n\nRun `%s lifecycle <subcommand> --help` for a subcommand's flags.\n", program, program)
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
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "", "codex", "claude", "claude-code", "claude code", "cursor", "dsh", "deepseek", "deepseek-harness":
		return nil
	default:
		return fmt.Errorf("unsupported hook provider %q (want codex, claude-code, cursor, deepseek-harness, or empty for the generic protocol)", provider)
	}
}
