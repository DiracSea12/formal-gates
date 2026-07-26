package cli

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"strings"

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
		fmt.Fprintln(streams.Stderr, err)
	}
	return code
}

func run(program string, args []string, streams IO) (int, error) {
	if len(args) == 0 {
		return runPackage(nil, streams)
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		printUsage(streams.Stdout, program)
		return 0, nil
	}
	switch args[0] {
	case "package":
		return runPackage(args[1:], streams)
	case "install":
		return runInstall(args[1:], streams)
	case "workflow":
		return runWorkflow(args[1:], streams)
	case "hook":
		return runHook(args[1:], streams)
	case "canary":
		return runCanary(args[1:], streams)
	case "behavior":
		return runBehavior(args[1:], streams)
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
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		return printValidationResult(streams.Stdout, "package", validate.Package(*root))
	case "route-candidates":
		fs := newFlagSet("package route-candidates", streams)
		root := fs.String("root", ".", "formal-gates package root")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		candidates, err := validate.PackageRouteCandidates(*root)
		return printValue(streams.Stdout, candidates, err)
	default:
		return 1, fmt.Errorf("unknown package subcommand: %s", subcommand)
	}
}

func runInstall(args []string, streams IO) (int, error) {
	fs := flag.NewFlagSet("install", flag.ContinueOnError)
	fs.SetOutput(streams.Stderr)
	source := fs.String("source", "", "formal-gates source directory")
	host := fs.String("host", "", "target host: claude, codex, cursor, or both")
	scope := fs.String("scope", "", "install scope: global or project")
	project := fs.String("project", "", "project path for project installs")
	force := fs.Bool("force", false, "replace an existing target")
	skipHooks := fs.Bool("skip-hooks", false, "install without changing native host hooks")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	if fs.NArg() != 0 {
		return 1, fmt.Errorf("install does not accept positional arguments")
	}
	report, err := validate.Install(validate.InstallOptions{Source: *source, Host: *host, Scope: *scope, Project: *project, Force: *force, SkipHooks: *skipHooks})
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
}

func runWorkflow(args []string, streams IO) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("workflow subcommand is required")
	}
	sub, args := args[0], args[1:]
	switch sub {
	case "start":
		fs := newFlagSet("workflow start", streams)
		root, pkg := rootFlags(fs)
		runID := fs.String("run-id", "", "optional run id")
		flow := fs.String("flow", "formal", "workflow flow")
		req := fs.String("requirement", "", "requirement source path")
		vcs := fs.String("vcs", "", "external VCS name")
		base := fs.String("base-snapshot", "", "optional native base identity to verify")
		artifacts := stringListFlag{}
		fs.Var(&artifacts, "requirement-artifact", "additional requirement or solution document; repeat as needed")
		retainedOverall := fs.Bool("retained-overall", false, "retain this run for merged slice integration")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		state, err := validate.Start(validate.StartOptions{Root: *root, PackageRoot: *pkg, RunID: *runID, Flow: *flow, RequirementSource: *req, RequirementArtifacts: artifacts, VCS: *vcs, BaseSnapshot: *base, RetainedOverall: *retainedOverall})
		return printValue(streams.Stdout, state, err)
	case "show":
		fs := newFlagSet("workflow show", streams)
		root := fs.String("root", ".", "repository root")
		runID := fs.String("run-id", "", "run id")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		state, err := validate.LoadRunState(*root, *runID)
		return printValue(streams.Stdout, state, err)
	case "resume":
		fs := newFlagSet("workflow resume", streams)
		root, pkg := rootFlags(fs)
		runID := fs.String("run-id", "", "run id")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		state, classificationRequired, err := validate.Resume(*root, *pkg, *runID)
		if err != nil {
			return 1, err
		}
		return printJSON(streams.Stdout, map[string]any{"classificationRequired": classificationRequired, "state": state})
	case "abort":
		fs := newFlagSet("workflow abort", streams)
		root := fs.String("root", ".", "repository root")
		runID := fs.String("run-id", "", "run id")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		summary, err := validate.Abort(*root, *runID)
		return printValue(streams.Stdout, summary, err)
	case "requirement":
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
	case "route-candidates":
		fs := newFlagSet("workflow route-candidates", streams)
		root, pkg := rootFlags(fs)
		runID := fs.String("run-id", "", "run id")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		candidates, err := validate.RouteCandidates(*root, *pkg, *runID)
		return printValue(streams.Stdout, candidates, err)
	case "route":
		fs := newFlagSet("workflow route", streams)
		root, pkg := rootFlags(fs)
		runID := fs.String("run-id", "", "run id")
		mode := fs.String("mode", "", "full or custom")
		gates := stringListFlag{}
		fs.Var(&gates, "gate", "selected gate id; repeat for custom route")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		state, err := validate.SetRoute(*root, *pkg, *runID, *mode, gates)
		return printValue(streams.Stdout, state, err)
	case "route-add":
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
	case "prepare-gate":
		fs := newFlagSet("workflow prepare-gate", streams)
		root, pkg := rootFlags(fs)
		runID := fs.String("run-id", "", "run id")
		gate := fs.String("gate", "", "discovered gate id")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		prompt, err := validate.PrepareGate(*root, *pkg, *runID, *gate)
		if err != nil {
			return 1, err
		}
		fmt.Fprint(streams.Stdout, prompt)
		return 0, nil
	case "prepare-action":
		fs := newFlagSet("workflow prepare-action", streams)
		root, pkg := rootFlags(fs)
		runID := fs.String("run-id", "", "run id")
		action := fs.String("action", "", "installed action prompt id")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		prompt, err := validate.PrepareAction(*root, *pkg, *runID, *action)
		if err != nil {
			return 1, err
		}
		fmt.Fprint(streams.Stdout, prompt)
		return 0, nil
	case "claim-dispatch":
		fs := newFlagSet("workflow claim-dispatch", streams)
		root, pkg := rootFlags(fs)
		runID := fs.String("run-id", "", "run id")
		dispatch := fs.String("dispatch", "", "prepared dispatch id")
		reviewer := fs.String("reviewer", "", "host reviewer or session identity")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		state, err := validate.ClaimDispatch(*root, *pkg, *runID, *dispatch, *reviewer)
		return printValue(streams.Stdout, state, err)
	case "record-action":
		return runRecordAction(args, streams)
	case "record-gate":
		return runRecordGate(args, streams)
	case "qa-design":
		return runQADesign(args, streams)
	case "qa-review":
		return runQAReview(args, streams)
	case "qa-execution":
		return runQAExecution(args, streams)
	case "snapshot":
		fs := newFlagSet("workflow snapshot", streams)
		root, pkg := rootFlags(fs)
		runID := fs.String("run-id", "", "run id")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		state, err := validate.AdvanceSnapshot(*root, *pkg, *runID)
		return printValue(streams.Stdout, state, err)
	case "carry":
		return runCarry(args, streams)
	case "authorize-repair":
		fs := newFlagSet("workflow authorize-repair", streams)
		root, pkg := rootFlags(fs)
		runID := fs.String("run-id", "", "run id")
		cycles := fs.Int("cycles", 1, "additional user-authorized review waves")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		state, err := validate.AuthorizeExtraRepair(*root, *pkg, *runID, *cycles)
		return printValue(streams.Stdout, state, err)
	case "seal":
		fs := newFlagSet("workflow seal", streams)
		root, pkg := rootFlags(fs)
		runID := fs.String("run-id", "", "run id")
		skips := stringListFlag{}
		fs.Var(&skips, "skip", "selected non-passing gate explicitly authorized to skip")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		summary, err := validate.Seal(*root, *pkg, *runID, skips)
		return printValue(streams.Stdout, summary, err)
	default:
		return 1, fmt.Errorf("unknown workflow subcommand: %s", sub)
	}
}

func runRecordAction(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow record-action", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	action := fs.String("action", "", "installed action id")
	status := fs.String("status", "", "PASS, FAIL, or RUNTIME_ERROR")
	message := fs.String("message", "", "runtime or result message")
	dispatch := fs.String("dispatch", "", "prepared dispatch id returned in the task")
	findings := newFindingFlags(fs)
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.RecordAction(*root, *pkg, *runID, *action, *dispatch, *status, *message, *findings)
	return printValue(streams.Stdout, state, err)
}

func runRecordGate(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow record-gate", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	gate := fs.String("gate", "", "discovered gate id")
	status := fs.String("status", "", "PASS, FAIL, or RUNTIME_ERROR")
	message := fs.String("message", "", "runtime or result message")
	dispatch := fs.String("dispatch", "", "prepared dispatch id returned in the task")
	findings := newFindingFlags(fs)
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.RecordGate(*root, *pkg, *runID, *gate, *dispatch, *status, *message, *findings)
	return printValue(streams.Stdout, state, err)
}

func runQADesign(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow qa-design", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	runtimeError := fs.String("runtime-error", "", "QA Design runtime error")
	dispatch := fs.String("dispatch", "", "prepared dispatch id returned in the task")
	cases := []validate.QACaseInput{}
	fs.Var(&caseStart{cases: &cases}, "case", "start a QA case with its behavior description")
	fs.Var(&caseField{cases: &cases, field: "kind"}, "kind", "STATIC or LIVE for the current QA case")
	fs.Var(&caseField{cases: &cases, field: "procedure"}, "procedure", "procedure for the current QA case")
	fs.Var(&caseField{cases: &cases, field: "oracle"}, "oracle", "oracle for the current QA case")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.RecordQADesign(*root, *pkg, *runID, *dispatch, cases, *runtimeError)
	return printValue(streams.Stdout, state, err)
}

func runQAReview(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow qa-review", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	dispatch := fs.String("dispatch", "", "prepared dispatch id returned in the task")
	runtimeError := fs.String("runtime-error", "", "QA Review runtime error")
	decisions := []validate.QAReviewInput{}
	fs.Var(&qaReviewStart{items: &decisions}, "case", "start a decision for a pending CASE id")
	fs.Var(&qaReviewField{items: &decisions, field: "outcome"}, "outcome", "PASS or FAIL")
	fs.Var(&qaReviewField{items: &decisions, field: "reason"}, "reason", "required reason for FAIL")
	findings := newFindingFlags(fs)
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.RecordQAReview(*root, *pkg, *runID, *dispatch, decisions, *runtimeError, *findings)
	return printValue(streams.Stdout, state, err)
}

func runQAExecution(args []string, streams IO) (int, error) {
	fs := newFlagSet("workflow qa-execution", streams)
	root, pkg := rootFlags(fs)
	runID := fs.String("run-id", "", "run id")
	runtimeError := fs.String("runtime-error", "", "QA execution runtime error")
	dispatch := fs.String("dispatch", "", "prepared dispatch id returned in the task")
	results := []validate.QAResultInput{}
	fs.Var(&qaResultStart{results: &results}, "case-result", "start a result using its generated CASE id")
	for _, item := range []struct{ name, field, usage string }{{"outcome", "outcome", "PASS or FAIL"}, {"procedure", "procedure", "executed procedure"}, {"observation", "observation", "observed result"}, {"oracle-result", "oracle-result", "oracle comparison"}} {
		fs.Var(&qaResultField{results: &results, field: item.field}, item.name, item.usage)
	}
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.RecordQAExecution(*root, *pkg, *runID, *dispatch, results, *runtimeError)
	return printValue(streams.Stdout, state, err)
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
	fs.Var(&carryStart{items: &decisions}, "gate", "start a Carry decision for a gate")
	fs.Var(&carryField{items: &decisions, field: "decision"}, "decision", "INHERIT or RERUN")
	fs.Var(&carryField{items: &decisions, field: "reason"}, "reason", "semantic decision reason")
	if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
		return code, err
	}
	state, err := validate.RecordCarry(*root, *pkg, *runID, *dispatch, decisions, *runtimeError, *mainAgent, *mainReason)
	return printValue(streams.Stdout, state, err)
}

func runHook(args []string, streams IO) (int, error) {
	if len(args) == 0 || args[0] != "decide" {
		return 1, fmt.Errorf("hook decide is required")
	}
	fs := newFlagSet("hook decide", streams)
	if code, err, done := parseFlagSet(fs, args[1:], streams.Stdout); done {
		return code, err
	}
	payload, err := io.ReadAll(streams.Stdin)
	if err != nil {
		return 1, err
	}
	decision, err := validate.Hook(payload)
	if err != nil {
		return 1, err
	}
	data, _ := json.Marshal(decision)
	fmt.Fprintln(streams.Stdout, string(data))
	if decision.PermissionDecision == "deny" {
		return 2, nil
	}
	return 0, nil
}

func runBehavior(args []string, streams IO) (int, error) {
	if len(args) == 0 || args[0] != "evaluate" {
		return 1, fmt.Errorf("behavior evaluate is required")
	}
	fs := newFlagSet("behavior evaluate", streams)
	root := fs.String("root", ".", "package root")
	cases := fs.String("cases", "examples/skill-behavior-prompts.json", "behavior case file")
	answers := fs.String("answers", "", "behavior answers file")
	if code, err, done := parseFlagSet(fs, args[1:], streams.Stdout); done {
		return code, err
	}
	report, result := validate.Behavior(validate.BehaviorOptions{Root: *root, CasesFile: *cases, AnswersFile: *answers})
	code, err := printJSON(streams.Stdout, report)
	if !result.OK() {
		return 1, fmt.Errorf("formal-gates behavior evaluate failed with %d issue(s)", len(result.Failures))
	}
	return code, err
}

func runCanary(args []string, streams IO) (int, error) {
	if len(args) == 0 {
		return 1, fmt.Errorf("canary subcommand is required")
	}
	sub, args := args[0], args[1:]
	switch sub {
	case "portable":
		fs := newFlagSet("canary portable", streams)
		root := fs.String("root", ".", "package root")
		format := fs.String("format", "text", "text or json")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		report, result := validate.PortableCanary(validate.PortableCanaryOptions{Root: *root})
		if *format == "json" {
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
		output := fs.String("formal-hook-output", "", "decision output file")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		payload, err := io.ReadAll(streams.Stdin)
		if err != nil {
			return 1, err
		}
		probe, result := validate.CodexHookProbe(validate.CodexHookProbeOptions{PayloadDir: *dir, FormalHookOutput: *output, Payload: payload})
		if !result.OK() {
			return printValidationResult(streams.Stdout, "canary codex-hook-probe", result)
		}
		if probe.Decision != nil {
			printJSON(streams.Stdout, probe.Decision)
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
	fs.Var(&findingSeverity{&items}, "severity", "P0, P1, or P2 for the current gate finding")
	fs.Var(&findingLocation{&items}, "location", "repository location for the current finding")
	return &items
}

type stringListFlag []string

func (f *stringListFlag) String() string { return strings.Join(*f, ",") }
func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

type caseStart struct{ cases *[]validate.QACaseInput }

func (f *caseStart) String() string { return "" }
func (f *caseStart) Set(v string) error {
	*f.cases = append(*f.cases, validate.QACaseInput{Description: v})
	return nil
}

type caseField struct {
	cases         *[]validate.QACaseInput
	field         string
	assignedGroup int
}

func (f *caseField) String() string { return "" }
func (f *caseField) Set(v string) error {
	if len(*f.cases) == 0 {
		return fmt.Errorf("case field must follow --case")
	}
	if f.assignedGroup == len(*f.cases) {
		return fmt.Errorf("duplicate --%s for current QA case", f.field)
	}
	p := &(*f.cases)[len(*f.cases)-1]
	if f.field == "kind" {
		p.Kind = v
	} else if f.field == "procedure" {
		p.Procedure = v
	} else {
		p.Oracle = v
	}
	f.assignedGroup = len(*f.cases)
	return nil
}

type qaReviewStart struct{ items *[]validate.QAReviewInput }

func (f *qaReviewStart) String() string { return "" }
func (f *qaReviewStart) Set(v string) error {
	*f.items = append(*f.items, validate.QAReviewInput{CaseID: v})
	return nil
}

type qaReviewField struct {
	items         *[]validate.QAReviewInput
	field         string
	assignedGroup int
}

func (f *qaReviewField) String() string { return "" }
func (f *qaReviewField) Set(v string) error {
	if len(*f.items) == 0 {
		return fmt.Errorf("QA Review field must follow --case")
	}
	if f.assignedGroup == len(*f.items) {
		return fmt.Errorf("duplicate --%s for current QA Review decision", f.field)
	}
	p := &(*f.items)[len(*f.items)-1]
	if f.field == "outcome" {
		p.Outcome = v
	} else {
		p.Reason = v
	}
	f.assignedGroup = len(*f.items)
	return nil
}

type qaResultStart struct{ results *[]validate.QAResultInput }

func (f *qaResultStart) String() string { return "" }
func (f *qaResultStart) Set(v string) error {
	*f.results = append(*f.results, validate.QAResultInput{CaseID: v})
	return nil
}

type qaResultField struct {
	results       *[]validate.QAResultInput
	field         string
	assignedGroup int
}

func (f *qaResultField) String() string { return "" }
func (f *qaResultField) Set(v string) error {
	if len(*f.results) == 0 {
		return fmt.Errorf("QA result field must follow --case-result")
	}
	if f.assignedGroup == len(*f.results) {
		return fmt.Errorf("duplicate --%s for current QA result", f.field)
	}
	p := &(*f.results)[len(*f.results)-1]
	switch f.field {
	case "outcome":
		p.Outcome = v
	case "procedure":
		p.Procedure = v
	case "observation":
		p.Observation = v
	case "oracle-result":
		p.OracleResult = v
	}
	f.assignedGroup = len(*f.results)
	return nil
}

type carryStart struct{ items *[]validate.CarryInput }

func (f *carryStart) String() string { return "" }
func (f *carryStart) Set(v string) error {
	*f.items = append(*f.items, validate.CarryInput{GateID: v})
	return nil
}

type carryField struct {
	items         *[]validate.CarryInput
	field         string
	assignedGroup int
}

func (f *carryField) String() string { return "" }
func (f *carryField) Set(v string) error {
	if len(*f.items) == 0 {
		return fmt.Errorf("Carry field must follow --gate")
	}
	if f.assignedGroup == len(*f.items) {
		return fmt.Errorf("duplicate --%s for current Carry decision", f.field)
	}
	p := &(*f.items)[len(*f.items)-1]
	if f.field == "decision" {
		p.Decision = v
	} else {
		p.Message = v
	}
	f.assignedGroup = len(*f.items)
	return nil
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
func hasHelpArg(args []string) bool {
	for _, arg := range args {
		if arg == "-h" || arg == "--help" {
			return true
		}
	}
	return false
}
func parseFlagSet(fs *flag.FlagSet, args []string, help io.Writer) (int, error, bool) {
	if hasHelpArg(args) {
		fs.SetOutput(help)
		fs.Usage()
		return 0, nil, true
	}
	if err := fs.Parse(args); err != nil {
		return 1, err, true
	}
	if fs.NArg() != 0 {
		return 1, fmt.Errorf("%s does not accept positional arguments", fs.Name()), true
	}
	return 0, nil, false
}
func printUsage(w io.Writer, program string) {
	fmt.Fprintf(w, "Usage: %s <command>\n\nCommands:\n  package validate|route-candidates\n  install\n  workflow start|show|resume|abort|requirement|route-candidates|route|route-add|prepare-gate|prepare-action|claim-dispatch|record-action|record-gate|qa-design|qa-review|qa-execution|snapshot|carry|authorize-repair|seal\n  hook decide\n  canary portable|codex-hook|codex-hook-probe\n  behavior evaluate\n", program)
}
