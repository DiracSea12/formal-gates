package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"formal-gates/internal/engine/coverage"
)

// coverageEnvelope is the small, stable JSON envelope shared by all three
// coverage commands.  The command-specific result stays owned by the pure
// coverage package; this adapter only labels a successful response.
type coverageEnvelope struct {
	OK     bool `json:"ok"`
	Result any  `json:"result"`
}

type coverageErrorEnvelope struct {
	Code    string `json:"code"`
	Path    string `json:"path"`
	Message string `json:"message"`
}

type coverageDigestsResult struct {
	Valid   bool `json:"valid"`
	Digests struct {
		RequiredSourcesDigest string `json:"requiredSourcesDigest"`
		ManifestDigest        string `json:"manifestDigest"`
		MapDigest             string `json:"mapDigest"`
	} `json:"digests"`
}

type coverageReconciliationResult struct {
	Valid   bool                      `json:"valid"`
	Binding coverage.ExecutionBinding `json:"binding"`
}

type projectWhitelistInput struct {
	Contract coverage.CoverageContract `json:"contract"`
	Reviews  []coverage.ReviewResult   `json:"reviews"`
}

type reconcileExecutionInput struct {
	Contract  coverage.CoverageContract  `json:"contract"`
	Whitelist coverage.ApprovedWhitelist `json:"whitelist"`
	Execution coverage.ExecutionReport   `json:"execution"`
}

// runCoverage exposes the 3.5a coverage contract without coupling it to
// workflow state, VCS, or legacy QA.  Inputs are read from stdin as JSON and
// each operation delegates directly to the corresponding pure function.
func runCoverage(program string, args []string, streams IO) (int, error) {
	if len(args) == 0 {
		printCoverageUsage(streams.Stdout, program)
		return 1, fmt.Errorf("coverage subcommand is required (e.g. coverage validate|project-whitelist|reconcile-execution)")
	}
	if isHelpArg(args[0]) {
		printCoverageUsage(streams.Stdout, program)
		return 0, nil
	}
	subcommand := args[0]
	if len(args) > 1 {
		if hasHelpArg(args[1:]) {
			printCoverageUsage(streams.Stdout, program)
			return 0, nil
		}
		return 1, fmt.Errorf("coverage %s does not accept positional arguments or flags", subcommand)
	}
	switch subcommand {
	case "validate":
		return runCoverageValidate(streams)
	case "project-whitelist":
		return runCoverageProjectWhitelist(streams)
	case "reconcile-execution":
		return runCoverageReconcileExecution(streams)
	default:
		return 1, fmt.Errorf("unknown coverage subcommand: %s", subcommand)
	}
}

func runCoverageValidate(streams IO) (int, error) {
	var contract coverage.CoverageContract
	if err := decodeCoverageJSON(streams.Stdin, &contract); err != nil {
		return emitCoverageError(streams.Stdout, err)
	}
	if err := coverage.Validate(contract); err != nil {
		return emitCoverageError(streams.Stdout, err)
	}
	digests, err := contract.Digests()
	if err != nil {
		return emitCoverageError(streams.Stdout, err)
	}
	result := coverageDigestsResult{Valid: true}
	result.Digests.RequiredSourcesDigest = digests.RequiredSourcesDigest
	result.Digests.ManifestDigest = digests.ManifestDigest
	result.Digests.MapDigest = digests.MapDigest
	return emitCoverageSuccess(streams.Stdout, result)
}

func runCoverageProjectWhitelist(streams IO) (int, error) {
	var input projectWhitelistInput
	if err := decodeCoverageJSON(streams.Stdin, &input); err != nil {
		return emitCoverageError(streams.Stdout, err)
	}
	whitelist, err := coverage.ProjectWhitelist(input.Contract, input.Reviews)
	if err != nil {
		return emitCoverageError(streams.Stdout, err)
	}
	return emitCoverageSuccess(streams.Stdout, whitelist)
}

func runCoverageReconcileExecution(streams IO) (int, error) {
	var input reconcileExecutionInput
	if err := decodeCoverageJSON(streams.Stdin, &input); err != nil {
		return emitCoverageError(streams.Stdout, err)
	}
	if err := coverage.ValidateExecution(input.Contract, input.Whitelist, input.Execution); err != nil {
		return emitCoverageError(streams.Stdout, err)
	}
	return emitCoverageSuccess(streams.Stdout, coverageReconciliationResult{Valid: true, Binding: input.Execution.Binding})
}

func emitCoverageSuccess(stdout io.Writer, result any) (int, error) {
	if _, err := printJSON(stdout, coverageEnvelope{OK: true, Result: result}); err != nil {
		return 1, err
	}
	return 0, nil
}

// emitCoverageError keeps expected contract failures machine-readable on
// stdout and avoids the generic Run wrapper printing a second text error.
func emitCoverageError(stdout io.Writer, err error) (int, error) {
	response := coverageErrorEnvelope{Code: "INVALID_JSON", Message: err.Error()}
	var validationErr *coverage.ValidationError
	if errors.As(err, &validationErr) {
		response.Code = validationErr.Code
		response.Path = validationErr.Path
		response.Message = validationErr.Message
	}
	if _, printErr := printJSON(stdout, response); printErr != nil {
		return 1, printErr
	}
	return 1, nil
}

func decodeCoverageJSON(input io.Reader, target any) error {
	payload, err := io.ReadAll(input)
	if err != nil {
		return fmt.Errorf("read JSON input: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("multiple JSON values are not allowed")
		}
		return err
	}
	return nil
}
