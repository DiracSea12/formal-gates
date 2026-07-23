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
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		printUsage(streams.Stdout, program)
		return 0, nil
	}

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
		verb := "validate"
		if len(args) > 0 && args[0] != "-h" && args[0] != "--help" && !strings.HasPrefix(args[0], "-") {
			verb = args[0]
			args = args[1:]
		}
		switch verb {
		case "validate":
			fs := flag.NewFlagSet("artifact validate", flag.ContinueOnError)
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
			if fs.NArg() != 0 {
				return 1, fmt.Errorf("artifact validate does not accept positional arguments")
			}
			return printValidationResult(streams.Stdout, "artifact", validate.Artifact(validate.ArtifactOptions{
				Root:           *root,
				File:           *file,
				Gate:           *gate,
				WorkflowID:     *workflowID,
				ChangeSnapshot: *changeSnapshot,
				Stage:          *stage,
			}))
		case "compose-requirements":
			fs := flag.NewFlagSet("artifact compose-requirements", flag.ContinueOnError)
			fs.SetOutput(streams.Stderr)
			root := fs.String("root", ".", "repository root")
			runDir := fs.String("run-dir", "", "active workflow run directory")
			workflowID := fs.String("workflow-id", "", "workflow id")
			changeSnapshot := fs.String("change-snapshot", "", "change snapshot")
			outputDir := fs.String("output-dir", "", "run-local restricted output directory")
			previousAlignment := fs.String("previous-alignment", "", "optional run-local previous alignment evidence")
			requirementSource := fs.String("requirement-source", "", "requirement source bound into generated artifacts")
			userOriginal := fs.String("user-original", "", "confirmed original user requirement text")
			coverageScan := fs.String("coverage-scan", "", "semantic coverage result; must be PASS")
			scopeStatus := fs.String("scope-status", "", "scope preservation status")
			scopeMessage := fs.String("scope-message", "", "scope preservation judgment")
			taskStatus := fs.String("task-status", "", "task proof status")
			taskMessage := fs.String("task-message", "", "task proof judgment")
			var alignmentIDs, alignmentValues, coveredTargets, openBlockers, approvedDroppedIDs stringListFlag
			var alignmentPositions, dimensionPositions, dimensionRefs, dimensionRefItems intListFlag
			var dimensionStatuses, dimensionMessages stringListFlag
			fs.Var(&alignmentIDs, "alignment-id", "RQ-### id from the confirmed requirement catalog; may be repeated")
			fs.Var(&alignmentPositions, "alignment", "1-based alignment item position; repeat once per item")
			fs.Var(&alignmentValues, "alignment-value", "semantic alignment value in generated field order; repeat eight times per item")
			fs.Var(&coveredTargets, "covered-target", "covered document target bound into generated artifacts; may be repeated")
			fs.Var(&openBlockers, "open-blocker", "semantic open blocker; Requirements PASS requires none")
			fs.Var(&approvedDroppedIDs, "approved-dropped-id", "explicitly approved prior alignment ID removed from --previous-alignment; may be repeated")
			fs.Var(&dimensionPositions, "dimension", "1-based generated dimension position; repeat for positions 1 through 13")
			fs.Var(&dimensionStatuses, "dimension-status", "semantic status paired with the corresponding --dimension")
			fs.Var(&dimensionMessages, "dimension-message", "semantic message paired with the corresponding --dimension")
			fs.Var(&dimensionRefs, "dimension-ref", "1-based dimension position for an alignment reference")
			fs.Var(&dimensionRefItems, "dimension-ref-item", "1-based alignment item position paired with --dimension-ref")
			if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
				return code, err
			}
			if fs.NArg() != 0 {
				return 1, fmt.Errorf("artifact compose-requirements does not accept positional arguments")
			}
			if len(alignmentValues) != len(alignmentPositions)*validate.RequirementsSemanticValuesPerAlignment {
				return 1, fmt.Errorf("--alignment-value must be repeated exactly eight times per --alignment")
			}
			if len(dimensionPositions) != len(dimensionStatuses) || len(dimensionPositions) != len(dimensionMessages) {
				return 1, fmt.Errorf("--dimension, --dimension-status, and --dimension-message must be repeated the same number of times")
			}
			if len(dimensionRefs) != len(dimensionRefItems) {
				return 1, fmt.Errorf("--dimension-ref and --dimension-ref-item must be repeated the same number of times")
			}
			alignments := make([]validate.RequirementsAlignmentSubmission, 0, len(alignmentPositions))
			for index, position := range alignmentPositions {
				start := index * validate.RequirementsSemanticValuesPerAlignment
				values := alignmentValues[start : start+validate.RequirementsSemanticValuesPerAlignment]
				alignments = append(alignments, validate.RequirementsAlignmentSubmission{
					Position: position, RequirementOrQuestion: values[0], Source: values[1], WhyItMatters: values[2], Status: values[3],
					UserAnswer: values[4], DownstreamEffect: values[5], DocumentImpact: values[6], EvidenceNeeded: values[7],
				})
			}
			dimensionItems := map[int][]int{}
			submittedDimensions := map[int]bool{}
			for _, position := range dimensionPositions {
				submittedDimensions[position] = true
			}
			for index, position := range dimensionRefs {
				if !submittedDimensions[position] {
					return 1, fmt.Errorf("--dimension-ref %d has no corresponding --dimension", position)
				}
				dimensionItems[position] = append(dimensionItems[position], dimensionRefItems[index])
			}
			dimensions := make([]validate.RequirementsDimensionSubmission, 0, len(dimensionPositions))
			for index, position := range dimensionPositions {
				dimensions = append(dimensions, validate.RequirementsDimensionSubmission{Position: position, Status: dimensionStatuses[index], AlignmentItemPositions: append([]int{}, dimensionItems[position]...), Message: dimensionMessages[index]})
			}
			output, result := validate.ComposeRequirements(validate.ComposeRequirementsOptions{
				Root:               *root,
				RunDir:             *runDir,
				WorkflowID:         *workflowID,
				ChangeSnapshot:     *changeSnapshot,
				OutputDir:          *outputDir,
				PreviousAlignment:  *previousAlignment,
				ApprovedDroppedIDs: approvedDroppedIDs,
				RequirementSource:  *requirementSource,
				AlignmentIDs:       alignmentIDs,
				CoveredTargets:     coveredTargets,
				Alignments:         alignments,
				UserOriginal:       *userOriginal,
				OpenBlockers:       openBlockers,
				CoverageScan:       *coverageScan,
				ScopePreservation:  validate.PassOrNA{Status: *scopeStatus, Message: *scopeMessage},
				TaskProof:          validate.PassOrNA{Status: *taskStatus, Message: *taskMessage},
				Dimensions:         dimensions,
			})
			if !result.OK() {
				return printValidationResult(streams.Stdout, "artifact compose-requirements", result)
			}
			return printJSON(streams.Stdout, output)
		case "compose-qa-execution":
			fs := flag.NewFlagSet("artifact compose-qa-execution", flag.ContinueOnError)
			fs.SetOutput(streams.Stderr)
			root := fs.String("root", ".", "repository root")
			runDir := fs.String("run-dir", "", "active workflow run directory")
			workflowID := fs.String("workflow-id", "", "workflow id")
			changeSnapshot := fs.String("change-snapshot", "", "change snapshot")
			output := fs.String("output", "", "run-local restricted QA Execution output")
			approvedCaseSet := fs.String("approved-case-set", "", "run-local approved case-set evidence")
			designReview := fs.String("design-review", "", "run-local accepted Design Review closure")
			qaOwnedResults := fs.String("qa-owned-results", "", "run-local QA-owned results")
			caseResultBinding := fs.String("case-result-binding", "", "run-local case/result binding")
			changedFiles := fs.String("changed-files", "", "run-local changed-files evidence")
			verification := fs.String("verification", "", "run-local verification evidence")
			if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
				return code, err
			}
			if fs.NArg() != 0 {
				return 1, fmt.Errorf("artifact compose-qa-execution does not accept positional arguments")
			}
			ref, result := validate.ComposeQAExecution(validate.ComposeQAExecutionOptions{
				Root:              *root,
				RunDir:            *runDir,
				WorkflowID:        *workflowID,
				ChangeSnapshot:    *changeSnapshot,
				Output:            *output,
				ApprovedCaseSet:   *approvedCaseSet,
				DesignReview:      *designReview,
				QAOwnedResults:    *qaOwnedResults,
				CaseResultBinding: *caseResultBinding,
				ChangedFiles:      *changedFiles,
				Verification:      *verification,
			})
			if !result.OK() {
				return printValidationResult(streams.Stdout, "artifact compose-qa-execution", result)
			}
			return printJSON(streams.Stdout, ref)
		case "compose-context-bundle":
			fs := flag.NewFlagSet("artifact compose-context-bundle", flag.ContinueOnError)
			fs.SetOutput(streams.Stderr)
			root := fs.String("root", ".", "repository root")
			runDir := fs.String("run-dir", "", "active workflow run directory")
			workflowID := fs.String("workflow-id", "", "workflow id")
			changeSnapshot := fs.String("change-snapshot", "", "change snapshot")
			output := fs.String("output", "", "run-local restricted context-bundle output")
			var inputs stringListFlag
			fs.Var(&inputs, "input", "run-local restricted input path; may be repeated")
			if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
				return code, err
			}
			if fs.NArg() != 0 {
				return 1, fmt.Errorf("artifact compose-context-bundle does not accept positional arguments")
			}
			ref, result := validate.ComposeContextBundle(validate.ComposeContextBundleOptions{
				Root: *root, RunDir: *runDir, WorkflowID: *workflowID, ChangeSnapshot: *changeSnapshot,
				Output: *output, Inputs: inputs,
			})
			if !result.OK() {
				return printValidationResult(streams.Stdout, "artifact compose-context-bundle", result)
			}
			return printJSON(streams.Stdout, ref)
		case "compose-transition-chain":
			fs := flag.NewFlagSet("artifact compose-transition-chain", flag.ContinueOnError)
			fs.SetOutput(streams.Stderr)
			root := fs.String("root", ".", "repository root")
			runDir := fs.String("run-dir", "", "active workflow run directory")
			workflowID := fs.String("workflow-id", "", "workflow id")
			targetSnapshot := fs.String("target-snapshot", "", "target change snapshot")
			output := fs.String("output", "", "run-local restricted transition-chain output")
			var fromSnapshots, toSnapshots, changedFiles, verifications stringListFlag
			fs.Var(&fromSnapshots, "hop-from", "source snapshot for the corresponding transition hop")
			fs.Var(&toSnapshots, "hop-to", "target snapshot for the corresponding transition hop")
			fs.Var(&changedFiles, "hop-changed-files", "changed-files path for the corresponding transition hop")
			fs.Var(&verifications, "hop-verification", "verification path for the corresponding transition hop")
			if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
				return code, err
			}
			if fs.NArg() != 0 {
				return 1, fmt.Errorf("artifact compose-transition-chain does not accept positional arguments")
			}
			hopCount := len(fromSnapshots)
			if hopCount == 0 || len(toSnapshots) != hopCount || len(changedFiles) != hopCount || len(verifications) != hopCount {
				return 1, fmt.Errorf("transition hops require equal non-zero counts of --hop-from, --hop-to, --hop-changed-files, and --hop-verification")
			}
			hops := make([]validate.TransitionHopSource, 0, hopCount)
			for index := 0; index < hopCount; index++ {
				hops = append(hops, validate.TransitionHopSource{
					FromSnapshot: fromSnapshots[index], ToSnapshot: toSnapshots[index], ChangedFiles: changedFiles[index],
					Verification: verifications[index],
				})
			}
			ref, result := validate.ComposeTransitionChain(validate.ComposeTransitionChainOptions{
				Root: *root, RunDir: *runDir, WorkflowID: *workflowID, TargetSnapshot: *targetSnapshot,
				Output: *output, Hops: hops,
			})
			if !result.OK() {
				return printValidationResult(streams.Stdout, "artifact compose-transition-chain", result)
			}
			return printJSON(streams.Stdout, ref)
		case "compose-qa-owned-evidence":
			fs := flag.NewFlagSet("artifact compose-qa-owned-evidence", flag.ContinueOnError)
			fs.SetOutput(streams.Stderr)
			root := fs.String("root", ".", "repository root")
			runDir := fs.String("run-dir", "", "active workflow run directory")
			workflowID := fs.String("workflow-id", "", "workflow id")
			changeSnapshot := fs.String("change-snapshot", "", "change snapshot")
			approvedCaseSet := fs.String("approved-case-set", "", "run-local approved case-set evidence")
			outputDir := fs.String("output-dir", "", "run-local restricted output directory")
			var casePositions intListFlag
			var outcomes, procedures, observations, oracleResults stringListFlag
			fs.Var(&casePositions, "case", "1-based approved case position; repeat once per case")
			fs.Var(&outcomes, "outcome", "PASS or FAIL outcome paired with the corresponding --case")
			fs.Var(&procedures, "procedure", "semantic procedure paired with the corresponding --case")
			fs.Var(&observations, "observation", "semantic observation paired with the corresponding --case")
			fs.Var(&oracleResults, "oracle-result", "semantic oracle result paired with the corresponding --case")
			if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
				return code, err
			}
			if fs.NArg() != 0 {
				return 1, fmt.Errorf("artifact compose-qa-owned-evidence does not accept positional arguments")
			}
			if len(casePositions) != len(outcomes) || len(casePositions) != len(procedures) || len(casePositions) != len(observations) || len(casePositions) != len(oracleResults) {
				return 1, fmt.Errorf("--case, --outcome, --procedure, --observation, and --oracle-result must be repeated the same number of times")
			}
			cases := make([]validate.QAExecutionCaseSubmission, 0, len(casePositions))
			for index, position := range casePositions {
				cases = append(cases, validate.QAExecutionCaseSubmission{Position: position, Outcome: outcomes[index], Procedure: procedures[index], Observation: observations[index], OracleResult: oracleResults[index]})
			}
			output, result := validate.ComposeQAOwnedEvidence(validate.ComposeQAOwnedEvidenceOptions{
				Root: *root, RunDir: *runDir, WorkflowID: *workflowID, ChangeSnapshot: *changeSnapshot,
				ApprovedCaseSet: *approvedCaseSet, OutputDir: *outputDir, Cases: cases,
			})
			if !result.OK() {
				return printValidationResult(streams.Stdout, "artifact compose-qa-owned-evidence", result)
			}
			return printJSON(streams.Stdout, output)
		case "compose-changed-files":
			fs := flag.NewFlagSet("artifact compose-changed-files", flag.ContinueOnError)
			fs.SetOutput(streams.Stderr)
			root := fs.String("root", ".", "repository root")
			runDir := fs.String("run-dir", "", "active workflow run directory")
			workflowID := fs.String("workflow-id", "", "workflow id")
			changeSnapshot := fs.String("change-snapshot", "", "change snapshot")
			output := fs.String("output", "", "run-local restricted changed-files output")
			var paths stringListFlag
			fs.Var(&paths, "path", "delivery path relative to the repository; may be repeated")
			if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
				return code, err
			}
			if fs.NArg() != 0 {
				return 1, fmt.Errorf("artifact compose-changed-files does not accept positional arguments")
			}
			ref, result := validate.ComposeChangedFiles(validate.ComposeChangedFilesOptions{
				Root: *root, RunDir: *runDir, WorkflowID: *workflowID, ChangeSnapshot: *changeSnapshot,
				Output: *output, Paths: paths,
			})
			if !result.OK() {
				return printValidationResult(streams.Stdout, "artifact compose-changed-files", result)
			}
			return printJSON(streams.Stdout, ref)
		default:
			return 1, fmt.Errorf("unknown artifact subcommand: %s", verb)
		}
	case "handoff":
		verb := "validate"
		if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			verb, args = args[0], args[1:]
		}
		switch verb {
		case "validate":
			fs := flag.NewFlagSet("handoff validate", flag.ContinueOnError)
			fs.SetOutput(streams.Stderr)
			root := fs.String("root", ".", "repository root for relative handoff references")
			file := fs.String("file", "", "handoff artifact file to validate")
			workflowID := fs.String("workflow-id", "", "expected workflow id")
			changeSnapshot := fs.String("change-snapshot", "", "expected change snapshot")
			if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
				return code, err
			}
			return printValidationResult(streams.Stdout, "handoff", validate.Handoff(validate.HandoffOptions{Root: *root, File: *file, WorkflowID: *workflowID, ChangeSnapshot: *changeSnapshot}))
		case "compose":
			fs := flag.NewFlagSet("handoff compose", flag.ContinueOnError)
			fs.SetOutput(streams.Stderr)
			root := fs.String("root", ".", "repository root")
			runDir := fs.String("run-dir", "", "active workflow run directory")
			workflowID := fs.String("workflow-id", "", "workflow id")
			changeSnapshot := fs.String("change-snapshot", "", "change snapshot")
			vcs := fs.String("vcs", "", "external VCS used for development and review comparisons")
			output := fs.String("output", "", "run-local restricted handoff output")
			requirementTarget := fs.String("requirement-target", "", "requirement document or OpenSpec target")
			verificationRequirements := fs.String("verification-requirements", "", "semantic verification requirements")
			forbiddenContext := fs.String("forbidden-context", "", "forbidden context description")
			formalFlowMode := fs.String("formal-flow-mode", "", "none, four-gate, release, or seal")
			triggerSource := fs.String("trigger-source", "", "formal flow trigger source")
			qaCaseSet := fs.String("qa-case-set", "", "approved QA case-set path")
			designReview := fs.String("design-review", "", "accepted Design Review closure path")
			if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
				return code, err
			}
			ref, result := validate.ComposeHandoff(validate.HandoffComposeOptions{
				Root: *root, RunDir: *runDir, WorkflowID: *workflowID, ChangeSnapshot: *changeSnapshot, Output: *output, VCS: *vcs,
				RequirementTarget: *requirementTarget, VerificationRequirements: *verificationRequirements,
				ForbiddenContext: *forbiddenContext, FormalFlowMode: *formalFlowMode, TriggerSource: *triggerSource,
				QACaseSet: *qaCaseSet, DesignReview: *designReview,
			})
			if !result.OK() {
				return printValidationResult(streams.Stdout, "handoff compose", result)
			}
			return printJSON(streams.Stdout, ref)
		default:
			return 1, fmt.Errorf("unknown handoff subcommand: %s", verb)
		}
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
		verb := "validate"
		if len(args) > 0 && args[0] != "-h" && args[0] != "--help" && !strings.HasPrefix(args[0], "-") {
			verb = args[0]
			args = args[1:]
		}
		if verb == "prepare" {
			fs := flag.NewFlagSet("prompt prepare", flag.ContinueOnError)
			fs.SetOutput(streams.Stderr)
			root := fs.String("root", ".", "repository or package root")
			output := fs.String("output", "", "file that will contain the exact reviewer message")
			patterns := fs.String("patterns", "", "pollution patterns JSON path; defaults to hooks/pollution-patterns.json under --root")
			gate := fs.String("gate", "", "review gate id")
			stage := fs.String("stage", "", "review stage")
			currentRequirement := fs.String("current-requirement", "", "confirmed current requirement target")
			currentDiff := fs.String("current-diff", "", "current diff or proposed change target")
			worktree := fs.String("worktree", "", "repository worktree")
			changeSnapshot := fs.String("change-snapshot", "", "change snapshot")
			reviewArtifact := fs.String("review-artifact", "", "assigned reviewer output path")
			policyID := fs.String("policy-id", "", "review policy id")
			contextBundle := fs.String("context-bundle", "", "CLI-generated context bundle")
			if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
				return code, err
			}
			prepared, result := validate.PrepareDispatchPrompt(validate.PrepareDispatchPromptOptions{
				Root: *root, OutputFile: *output, ConfigPath: *patterns, Gate: *gate, Stage: *stage,
				CurrentRequirement: *currentRequirement, CurrentDiff: *currentDiff, Worktree: *worktree,
				ChangeSnapshot: *changeSnapshot, ReviewArtifact: *reviewArtifact, PolicyID: *policyID,
				ContextBundle: *contextBundle,
			})
			if !result.OK() {
				return printValidationResult(streams.Stdout, "prompt prepare", result)
			}
			encoded, err := json.Marshal(prepared)
			if err != nil {
				return 1, err
			}
			fmt.Fprintln(streams.Stdout, string(encoded))
			return 0, nil
		}
		if verb != "validate" {
			return 1, fmt.Errorf("unsupported prompt command %q (want validate or prepare)", verb)
		}
		fs := flag.NewFlagSet("prompt validate", flag.ContinueOnError)
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
			FinalSend:  true,
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
		skipHooks := fs.Bool("skip-hooks", false, "install runtime without changing native host hook configuration")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if fs.NArg() != 0 {
			return 1, fmt.Errorf("install does not accept positional arguments")
		}
		report, err := validate.Install(validate.InstallOptions{
			Source:    *source,
			Host:      *host,
			Scope:     *scope,
			Project:   *project,
			Force:     *force,
			SkipHooks: *skipHooks,
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
	case "behavior":
		return runBehavior(args, streams)
	case "policy":
		return runPolicy(args, streams)
	case "help", "-h", "--help":
		printUsage(streams.Stdout, program)
		return 0, nil
	default:
		printUsage(streams.Stdout, program)
		return 1, fmt.Errorf("unknown command: %s", command)
	}
}

func runPolicy(args []string, streams IO) (int, error) {
	if len(args) == 0 {
		printUsage(streams.Stdout, "formal-gates")
		return 1, fmt.Errorf("policy subcommand is required")
	}
	subcommand := args[0]
	args = args[1:]
	switch subcommand {
	case "show":
		fs := flag.NewFlagSet("policy show", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		format := fs.String("format", "json", "output format: json")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if *format != "json" {
			return 1, fmt.Errorf("unsupported --format %q (want json)", *format)
		}
		data, err := validate.PolicyJSON(validate.Policy())
		if err != nil {
			return 1, err
		}
		fmt.Fprintln(streams.Stdout, string(data))
		return 0, nil
	default:
		printUsage(streams.Stdout, "formal-gates")
		return 1, fmt.Errorf("unknown policy subcommand: %s", subcommand)
	}
}

func runBehavior(args []string, streams IO) (int, error) {
	if len(args) == 0 {
		printUsage(streams.Stdout, "formal-gates")
		return 1, fmt.Errorf("behavior subcommand is required")
	}
	subcommand := args[0]
	args = args[1:]
	switch subcommand {
	case "evaluate":
		fs := flag.NewFlagSet("behavior evaluate", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		root := fs.String("root", ".", "repository or package root")
		cases := fs.String("cases", "examples/skill-behavior-prompts.json", "behavior case JSON file")
		answers := fs.String("answers", "", "behavior answer JSON file")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		report, result := validate.Behavior(validate.BehaviorOptions{
			Root:        *root,
			CasesFile:   *cases,
			AnswersFile: *answers,
		})
		encoded, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return 1, err
		}
		fmt.Fprintln(streams.Stdout, string(encoded))
		if !result.OK() {
			return 1, fmt.Errorf("formal-gates behavior evaluate failed with %d issue(s)", len(result.Failures))
		}
		return 0, nil
	default:
		return 1, fmt.Errorf("unknown behavior subcommand: %s", subcommand)
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
		runDir := fs.String("run-dir", "", "workflow run directory under .gates/runs")
		provider := fs.String("provider", "", "receipt provider: claude-code, codex, or cursor")
		artifact := fs.String("artifact", "", "review artifact path")
		contextBundle := fs.String("context-bundle", "", "validated context bundle path")
		prompt := fs.String("prompt", "", "exact final-send reviewer prompt; required for review judgments")
		changedFiles := fs.String("changed-files", "", "run-relative changed-files evidence; required by post-development reviewer policies")
		verification := fs.String("verification", "", "run-relative verification evidence; required by post-development reviewer policies")
		qaDesignCaseSet := fs.String("qa-design-case-set", "", "QA Design case-set path for Design Review")
		qaDesignReceipt := fs.String("qa-design-receipt", "", "QA Design receipt path for Design Review")
		transitionChain := fs.String("transition-chain", "", "script-owned Carry transition chain evidence")
		var carrySourceClosures stringListFlag
		fs.Var(&carrySourceClosures, "carry-source-closure", "verified source closure path for Carry; may be repeated")
		qaCaseCount := fs.Int("qa-case-count", 0, "number of CLI-generated QA Design cases")
		gate := fs.String("gate", "", "gate id")
		stage := fs.String("stage", "", "gate stage")
		workflowID := fs.String("workflow-id", "", "workflow id")
		changeSnapshot := fs.String("change-snapshot", "", "target change snapshot")
		userAuthorizedExtraReview := fs.Bool("user-authorized-extra-review", false, "allow a review beyond the standard three only after explicit user approval")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		registration, result := validate.ReceiptRegisterDispatch(validate.ReceiptRegisterOptions{
			Worktree:                  *worktree,
			RunDir:                    *runDir,
			Provider:                  *provider,
			Artifact:                  *artifact,
			ContextBundle:             *contextBundle,
			Prompt:                    *prompt,
			ChangedFiles:              *changedFiles,
			Verification:              *verification,
			QADesignCaseSet:           *qaDesignCaseSet,
			QADesignReceipt:           *qaDesignReceipt,
			TransitionChain:           *transitionChain,
			CarrySourceClosures:       carrySourceClosures,
			QACaseCount:               *qaCaseCount,
			Gate:                      *gate,
			Stage:                     *stage,
			WorkflowID:                *workflowID,
			ChangeSnapshot:            *changeSnapshot,
			UserAuthorizedExtraReview: *userAuthorizedExtraReview,
		})
		if !result.OK() {
			return printValidationResult(streams.Stdout, "receipt register", result)
		}
		return printJSON(streams.Stdout, registration)
	case "submit":
		fs := flag.NewFlagSet("receipt submit", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		artifact := fs.String("artifact", "", "assigned reviewer or Carry artifact path")
		var checkPositions, findingChecks, locationFindings, locationStarts, locationEnds, carryGates, designCases intListFlag
		var statuses, messages, findingMessages, locationPaths, decisions, reasons, caseValues stringListFlag
		fs.Var(&checkPositions, "check", "1-based generated policy check position; repeat once per check")
		fs.Var(&statuses, "status", "semantic status paired with the corresponding --check")
		fs.Var(&messages, "message", "semantic message paired with the corresponding --check")
		fs.Var(&findingChecks, "finding-check", "1-based generated check position for the corresponding finding")
		fs.Var(&findingMessages, "finding-message", "semantic finding message paired with --finding-check")
		fs.Var(&locationFindings, "location-finding", "1-based submitted finding position for the corresponding location")
		fs.Var(&locationPaths, "location-path", "repository-relative semantic source path paired with --location-finding")
		fs.Var(&locationStarts, "location-start", "positive start line paired with --location-finding")
		fs.Var(&locationEnds, "location-end", "end line paired with --location-finding")
		fs.Var(&carryGates, "carry-gate", "1-based generated Carry gate position; repeat once per gate")
		fs.Var(&decisions, "decision", "semantic Carry decision paired with --carry-gate")
		fs.Var(&reasons, "reason", "semantic Carry reason paired with --carry-gate")
		fs.Var(&designCases, "design-case", "1-based generated QA Design case position; repeat once per case")
		fs.Var(&caseValues, "case-value", "semantic QA Design value in generated field order; repeat seven times per case")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if fs.NArg() != 0 {
			return 1, fmt.Errorf("receipt submit does not accept positional arguments")
		}
		if len(checkPositions) != len(statuses) || len(checkPositions) != len(messages) {
			return 1, fmt.Errorf("--check, --status, and --message must be repeated the same number of times")
		}
		if len(findingChecks) != len(findingMessages) {
			return 1, fmt.Errorf("--finding-check and --finding-message must be repeated the same number of times")
		}
		if len(locationFindings) != len(locationPaths) || len(locationFindings) != len(locationStarts) || len(locationFindings) != len(locationEnds) {
			return 1, fmt.Errorf("all four --location-* flags must be repeated the same number of times")
		}
		if len(carryGates) != len(decisions) || len(carryGates) != len(reasons) {
			return 1, fmt.Errorf("--carry-gate, --decision, and --reason must be repeated the same number of times")
		}
		if len(caseValues) != len(designCases)*validate.QADesignSemanticValuesPerCase {
			return 1, fmt.Errorf("--case-value must be repeated exactly seven times per --design-case")
		}
		options := validate.ReceiptSubmitOptions{Worktree: *worktree, Artifact: *artifact}
		for i := range checkPositions {
			options.Checks = append(options.Checks, validate.ReceiptSemanticCheck{Position: checkPositions[i], Status: statuses[i], Message: messages[i]})
		}
		for i := range findingChecks {
			options.Findings = append(options.Findings, validate.ReceiptSemanticFinding{CheckPosition: findingChecks[i], Message: findingMessages[i]})
		}
		for i := range locationFindings {
			options.Locations = append(options.Locations, validate.ReceiptSemanticLocation{FindingPosition: locationFindings[i], Path: locationPaths[i], StartLine: locationStarts[i], EndLine: locationEnds[i]})
		}
		for i := range carryGates {
			options.CarryDecisions = append(options.CarryDecisions, validate.ReceiptSemanticCarryDecision{GatePosition: carryGates[i], Decision: decisions[i], Reason: reasons[i]})
		}
		for i := range designCases {
			start := i * validate.QADesignSemanticValuesPerCase
			options.DesignCases = append(options.DesignCases, validate.ReceiptSemanticDesignCase{Position: designCases[i], Values: append([]string{}, caseValues[start:start+validate.QADesignSemanticValuesPerCase]...)})
		}
		submission, result := validate.ReceiptSubmit(options)
		if !result.OK() {
			return printValidationResult(streams.Stdout, "receipt submit", result)
		}
		return printJSON(streams.Stdout, submission)
	case "capture":
		fs := flag.NewFlagSet("receipt capture", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		runDir := fs.String("run-dir", "", "workflow run directory under .gates/runs")
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
		runDir := fs.String("run-dir", "", "workflow run directory under .gates/runs")
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
	case "record-stage":
		fs := flag.NewFlagSet("workflow record-stage", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		state := fs.String("state", "", "gate state JSON path; defaults to gate-state.json in the active workflow run")
		runDir := fs.String("run-dir", "", "workflow run directory under .gates/runs")
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
		if *verdict == "PASS" {
			fmt.Fprintf(streams.Stdout, "GATE_WORKFLOW_RECORDED gate=%s verdict=%s workflowId=%s changeSnapshot=%s\n", *gate, *verdict, *workflowID, *changeSnapshot)
		} else {
			fmt.Fprintf(streams.Stdout, "GATE_WORKFLOW_NOT_RECORDED gate=%s verdict=%s workflowId=%s changeSnapshot=%s\n", *gate, *verdict, *workflowID, *changeSnapshot)
		}
		return 0, nil
	case "record-transition":
		fs := flag.NewFlagSet("workflow record-transition", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		state := fs.String("state", "", "gate state JSON path; defaults to gate-state.json in the active workflow run")
		runDir := fs.String("run-dir", "", "workflow run directory under .gates/runs")
		artifact := fs.String("artifact", "", "receipt-bound Carry Arbiter JSON artifact")
		workflowID := fs.String("workflow-id", "", "workflow id")
		changeSnapshot := fs.String("change-snapshot", "", "target change snapshot")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		result := validate.WorkflowRecordTransition(validate.WorkflowRecordTransitionOptions{
			Worktree: *worktree, StatePath: *state, RunDir: *runDir, Artifact: *artifact,
			WorkflowID: *workflowID, ChangeSnapshot: *changeSnapshot,
		})
		if !result.OK() {
			return printValidationResult(streams.Stdout, "workflow record-transition", result)
		}
		fmt.Fprintf(streams.Stdout, "GATE_WORKFLOW_TRANSITION_RECORDED workflowId=%s changeSnapshot=%s\n", *workflowID, *changeSnapshot)
		return 0, nil
	case "verify-admission":
		fs := flag.NewFlagSet("workflow verify-admission", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		state := fs.String("state", "", "gate state JSON path; defaults to gate-state.json in the active workflow run")
		runDir := fs.String("run-dir", "", "workflow run directory under .gates/runs")
		gate := fs.String("gate", "", "gate id")
		mode := fs.String("mode", "", "gate mode")
		workflowID := fs.String("workflow-id", "", "workflow id")
		changeSnapshot := fs.String("change-snapshot", "", "change snapshot")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		result := validate.WorkflowVerifyAdmission(validate.WorkflowVerifyAdmissionOptions{
			Worktree:       *worktree,
			StatePath:      *state,
			Gate:           *gate,
			Mode:           *mode,
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
		runDir := fs.String("run-dir", "", "workflow run directory under .gates/runs")
		var attemptArtifacts stringListFlag
		fs.Var(&attemptArtifacts, "attempt-artifact", "run-local PASS verification artifact; may be repeated")
		output := fs.String("output", "", "output aggregate artifact path")
		finalQAArtifact := fs.String("final-qa-artifact", "", "output path for the generated deterministic FinalExecution artifact")
		recordFinalQA := fs.Bool("record-final-qa", false, "generate and record FinalExecution after writing final verification")
		state := fs.String("state", "", "gate state JSON path; defaults to gate-state.json in the active workflow run")
		actor := fs.String("actor", "gate-workflow", "recording actor when --record-final-qa is used")
		workflowID := fs.String("workflow-id", "", "workflow id")
		changeSnapshot := fs.String("change-snapshot", "", "change snapshot")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		artifact, result := validate.WorkflowFinalVerification(validate.WorkflowFinalVerificationOptions{
			Worktree:         *worktree,
			StatePath:        *state,
			RunDir:           *runDir,
			AttemptArtifacts: attemptArtifacts,
			OutputArtifact:   *output,
			FinalQAArtifact:  *finalQAArtifact,
			RecordFinalQA:    *recordFinalQA,
			Actor:            *actor,
			WorkflowID:       *workflowID,
			ChangeSnapshot:   *changeSnapshot,
		})
		if !result.OK() {
			for _, failure := range result.Failures {
				fmt.Fprintf(streams.Stdout, "GATE_WORKFLOW_BLOCKED %s: %s\n", failure.Path, failure.Message)
			}
			return 1, fmt.Errorf("formal-gates workflow final-verification failed with %d issue(s)", len(result.Failures))
		}
		if *recordFinalQA {
			_, cleanupResult := validate.WorkflowCleanup(validate.WorkflowCleanupOptions{Worktree: *worktree, Execute: true})
			if !cleanupResult.OK() {
				for _, failure := range cleanupResult.Failures {
					fmt.Fprintf(streams.Stdout, "GATE_WORKFLOW_CLEANUP_BLOCKED %s: %s\n", failure.Path, failure.Message)
				}
				return 1, fmt.Errorf("formal-gates workflow cleanup failed with %d issue(s)", len(cleanupResult.Failures))
			}
		}
		fmt.Fprintf(streams.Stdout, "GATE_WORKFLOW_FINAL_VERIFICATION status=%s accepted=%d attempts=%d\n", artifact.Status, len(artifact.AcceptedAttempts), len(artifact.Attempts))
		return 0, nil
	case "cleanup":
		fs := flag.NewFlagSet("workflow cleanup", flag.ContinueOnError)
		fs.SetOutput(streams.Stderr)
		worktree := fs.String("worktree", ".", "repository root")
		dryRun := fs.Bool("dry-run", false, "list allowed cleanup paths without deleting")
		execute := fs.Bool("execute", false, "delete allowed cleanup paths")
		flowID := fs.String("flow-id", "", "optional temporary workflow flow id")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if *dryRun && *execute {
			return 1, fmt.Errorf("use only one of --dry-run or --execute")
		}
		report, result := validate.WorkflowCleanup(validate.WorkflowCleanupOptions{
			Worktree: *worktree,
			FlowID:   *flowID,
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
		state := fs.String("state", "", "gate state JSON path; defaults to gate-state.json in the active workflow run")
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
		state := fs.String("state", "", "gate state JSON path; defaults to gate-state.json in the active workflow run")
		gate := fs.String("gate", "", "gate id")
		mode := fs.String("mode", "", "gate mode")
		workflowID := fs.String("workflow-id", "", "workflow id")
		changeSnapshot := fs.String("change-snapshot", "", "change snapshot")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		result := validate.WorkflowVerifyAdmission(validate.WorkflowVerifyAdmissionOptions{
			Worktree:       *worktree,
			StatePath:      *state,
			Gate:           *gate,
			Mode:           *mode,
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
		statePath := fs.String("state", "", "read-only active workflow gate state JSON path (required)")
		format := fs.String("format", "json", "output format: json or text")
		if code, err, done := parseFlagSet(fs, args, streams.Stdout); done {
			return code, err
		}
		if *format != "json" && *format != "text" {
			return 1, fmt.Errorf("unsupported --format %q (want json or text)", *format)
		}
		if strings.TrimSpace(*statePath) == "" {
			return 1, fmt.Errorf("--state is required; use the workflow's restricted/gate-state.json")
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

type intListFlag []int

func (s *intListFlag) String() string {
	values := make([]string, 0, len(*s))
	for _, value := range *s {
		values = append(values, strconv.Itoa(value))
	}
	return strings.Join(values, ",")
}

func (s *intListFlag) Set(value string) error {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return fmt.Errorf("must be an integer: %q", value)
	}
	*s = append(*s, parsed)
	return nil
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
	usage := `%s

Usage:
  %s package validate  --root <formal-gates>
  %s artifact validate --root <repo> --file <artifact> --gate <gate-id> --workflow-id <id> --change-snapshot <snapshot> [--stage <stage>]
  %s artifact compose-requirements --root <repo> [--run-dir <dir>] --workflow-id <id> --change-snapshot <snapshot> --output-dir <restricted/dir> --requirement-source <source> --alignment-id <RQ-###> --alignment <position> --alignment-value <semantic-value>... --user-original <text> --coverage-scan PASS --scope-status <status> --scope-message <text> --task-status <status> --task-message <text> --dimension <position> --dimension-status <status> --dimension-message <text> --dimension-ref <position> --dimension-ref-item <alignment-position> --covered-target <path> [--previous-alignment <alignment.json> --approved-dropped-id <RQ-###> ...]
  %s artifact compose-qa-execution --root <repo> [--run-dir <dir>] --workflow-id <id> --change-snapshot <snapshot> --output <restricted/execution.json> --approved-case-set <ref> --design-review <ref> --qa-owned-results <ref> --case-result-binding <ref> --changed-files <ref> --verification <ref>
  %s artifact compose-context-bundle --root <repo> [--run-dir <dir>] --workflow-id <id> --change-snapshot <snapshot> --output <restricted/bundle.json> --input <restricted/path> [--input <restricted/path> ...]
  %s artifact compose-transition-chain --root <repo> [--run-dir <dir>] --workflow-id <id> --target-snapshot <snapshot> --output <restricted/chain.json> --hop-from <snapshot> --hop-to <snapshot> --hop-changed-files <path> --hop-verification <path> [...]
  %s artifact compose-qa-owned-evidence --root <repo> [--run-dir <dir>] --workflow-id <id> --change-snapshot <snapshot> --approved-case-set <ref> --case <position> --outcome PASS|FAIL --procedure <text> --observation <text> --oracle-result <text> --output-dir <restricted/dir>
  %s artifact compose-changed-files --root <repo> [--run-dir <dir>] --workflow-id <id> --change-snapshot <external-vcs-snapshot> --path <delivery/path> [--path <delivery/path> ...] --output <restricted/changed-files.txt>
  %s handoff validate  --root <repo> --file <handoff> --workflow-id <id> --change-snapshot <snapshot>
  %s handoff compose   --root <repo> [--run-dir <dir>] --workflow-id <id> --change-snapshot <snapshot> --vcs <git|svn|p4|other> --output <restricted/handoff.md> --requirement-target <target> --verification-requirements <text> --forbidden-context <text> --formal-flow-mode <mode> --trigger-source <text> [--qa-case-set <path> --design-review <closure>]
  %s prompt validate   --root <formal-gates> (--text <text> | --file <file> | --stdin) [--patterns <json>] [--format text|json]
  %s prompt prepare    --root <repo> --output <exact-send.txt> --gate <gate> [--stage <stage>] --current-requirement <target> --current-diff <target> --worktree <repo> --change-snapshot <snapshot> --review-artifact <review.json> --policy-id <policy> --context-bundle <bundle.json> [--patterns <json>]
  %s install           --source <formal-gates-dir> --host claude|codex|cursor|both --scope global|project [--project <path>] [--force] [--skip-hooks]
  %s gate record       --worktree <repo> --gate <gate-id> --verdict <verdict> --artifact <artifact> --workflow-id <id> --change-snapshot <snapshot> [--mode <mode>] [--stage <stage>] [--state <active-run-json>] [--actor <actor>] [--reason <text>]
  %s gate verify-admission --worktree <repo> --gate <gate-id> --workflow-id <id> --change-snapshot <snapshot> [--mode <mode>] [--state <active-run-json>]
  %s gate show         --worktree <repo> --state <active-run-json> [--format json|text]
  %s workflow record-stage --worktree <repo> [--run-dir <dir>] --gate <gate-id> --verdict <verdict> --artifact <artifact> --workflow-id <id> --change-snapshot <snapshot> [--mode <mode>] [--stage <stage>] [--state <active-run-json>] [--actor <actor>] [--reason <text>]
  %s workflow record-transition --worktree <repo> [--run-dir <dir>] --artifact <carry-arbiter.json> --workflow-id <id> --change-snapshot <target> [--state <active-run-json>]
  %s workflow verify-admission --worktree <repo> [--run-dir <dir>] --gate <gate-id> --workflow-id <id> --change-snapshot <snapshot> [--mode <mode>] [--state <active-run-json>]
  %s workflow final-verification --worktree <repo> [--run-dir <dir>] --attempt-artifact <restricted/path> [--attempt-artifact <restricted/path> ...] --output <artifact> --workflow-id <id> --change-snapshot <snapshot> [--state <active-run-json>] [--record-final-qa --final-qa-artifact <artifact> --actor <actor>]
  %s workflow cleanup --worktree <repo> [--flow-id <temporary-flow-id>] [--dry-run | --execute]
  %s receipt register --provider <provider> --worktree <repo> [--run-dir <dir>] --context-bundle <bundle.json> [--prompt <exact-send.txt>] [--qa-case-count <n>] [--changed-files <ref>] [--verification <ref>] [--qa-design-case-set <ref> --qa-design-receipt <ref>] [--transition-chain <ref> --carry-source-closure <closure> ...] --artifact <review.json> --gate <gate-id> --workflow-id <id> --change-snapshot <snapshot> [--stage <stage>] [--user-authorized-extra-review]
  %s receipt submit --worktree <repo> --artifact <assigned-output> [--check <position> --status <status> --message <text> ...] [--finding-check <position> --finding-message <text> ...] [--location-finding <position> --location-path <path> --location-start <line> --location-end <line> ...] [--carry-gate <position> --decision <decision> --reason <text> ...] [--design-case <position> --case-value <semantic-value> ...]
  %s receipt capture --provider <provider> --event <event> --worktree <repo> [--run-dir <dir>] < payload.json
  %s receipt finalize --provider <provider> --worktree <repo> [--run-dir <dir>] --artifact <review.json> --gate <gate-id> --workflow-id <id> [--stage <stage>]
  %s receipt validate --worktree <repo> --receipt <receipt.json> --artifact <review.json> --gate <gate-id> --workflow-id <id> --change-snapshot <snapshot> [--stage <stage>]
  %s receipt preflight --host <host> --worktree <repo>
  %s hook decide       < payload.json
  %s canary portable   --root <formal-gates> [--format text|json]
  %s canary codex-hook --worktree <repo> [--codex-command <codex>] [--keep-temp]
  %s behavior evaluate --root <formal-gates> [--cases <cases.json>] [--answers <answers.json>]
  %s policy show       --format json
`
	args := make([]any, strings.Count(usage, "%s"))
	for i := range args {
		args[i] = program
	}
	fmt.Fprintf(stdout, usage, args...)
}
