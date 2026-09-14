package facade

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"formal-gates/internal/engine/gitprep"
	"formal-gates/internal/engine/protocol"
)

// This selects captured supplements only; ordinary reading of requirement
// references and the bound workspace does not require a capture.
// planActionSources selects only captured inputs of the accepted design, never
// live provenance paths or another role's workspace. Both native transports call
// this before claiming a new action; issued actions keep their fixed inputs.
func (f *Facade) planActionSources(run Run, claim protocol.ExecutionClaim, supplied []SubagentSource) ([]SubagentSource, error) {
	step := string(claim.Task.Step)
	if err := validateCapturedSources(supplied, step); err != nil {
		return nil, err
	}
	if run.Regular.Preparation == nil {
		return supplied, nil
	}
	mode := strings.Split(step, "-")[0]
	q := run.Regular.Preparation.QA[mode]
	if q == nil {
		return supplied, nil
	}
	var design *protocol.QADesign
	var action string
	switch step {
	case "blackbox-review", "whitebox-review":
		design, action = q.Design, q.DesignActionID
	case "blackbox-design", "whitebox-design":
		design, action = q.PreviousDesign, q.PreviousDesignActionID
	default:
		return supplied, nil
	}
	if design == nil {
		return supplied, nil
	}
	if err := validateAcceptedDesignAction(run, mode, design, action); err != nil {
		return nil, err
	}
	inherited, err := f.capturedActionSources(action)
	if err != nil {
		return nil, err
	}
	if err = validateCapturedSources(inherited, step); err != nil {
		return nil, err
	}
	// Explicit caller inputs cannot replace or extend the accepted contract.
	if len(supplied) != 0 && !reflect.DeepEqual(supplied, inherited) {
		return nil, fmt.Errorf("ACTION_SOURCE_BINDING: supplied sources differ from accepted design")
	}
	return inherited, nil
}

func validateCapturedSources(sources []SubagentSource, step string) error {
	development := step == "development-worker" || step == "repair"
	qa := step == "blackbox-design" || step == "blackbox-review" || step == "whitebox-design" || step == "whitebox-review"
	for _, source := range sources {
		if !(source.Kind == "public-contract" && (development || qa) || source.Kind == "environment-observation" && development) {
			return fmt.Errorf("SUBAGENT_SOURCE_SCOPE: %s cannot receive %s", step, source.Kind)
		}
		if source.Digest != fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(source.Content))) {
			return fmt.Errorf("ACTION_SOURCE_BINDING: captured content digest differs: %s", source.Path)
		}
	}
	return nil
}

// validateAcceptedDesignAction checks an explicit binding, never infers one from history.
func validateAcceptedDesignAction(run Run, mode string, design *protocol.QADesign, action string) error {
	x, ok := run.Executions[action]
	rejected := false
	for _, failure := range run.Regular.PreparationErrors {
		if failure.Result.ActionID == action {
			rejected = true
		}
	}
	result := x.PreparationResult
	if action == "" || !ok || !x.Finished || rejected || string(x.Claim.Task.Step) != mode+"-design" || result == nil || (result.Status != "DRAFT" && result.Status != "PASS" && result.Status != "FAIL") || !acceptedDesignProjectionMatches(run, result.Design, design) {
		return fmt.Errorf("ACTION_SOURCE_BINDING: accepted %s design requires its exact bound completed action", mode)
	}
	return nil
}

// prepareProjection may rebind retained QA to the current required sources and
// route without changing its originating action. Compare that exact projection,
// keeping every authored field and the remaining binding fields significant.
func acceptedDesignProjectionMatches(run Run, original, design *protocol.QADesign) bool {
	if original == nil || design == nil {
		return false
	}
	if reflect.DeepEqual(original, design) {
		return true
	}
	projected := *original
	projected.Manifest.Binding.SourceBinding = run.Regular.RequiredSources.Binding
	projected.Manifest.Binding.RouteScope = gitprep.Digest(run.Regular.Route)
	return reflect.DeepEqual(&projected, design)
}

func (f *Facade) capturedActionSources(action string) ([]SubagentSource, error) {
	data, err := os.ReadFile(filepath.Join(f.codexJob(action), "sources.json"))
	if err != nil {
		return nil, fmt.Errorf("ACTION_SOURCE_BINDING: %s canonical captures: %w", action, err)
	}
	var sources []SubagentSource
	if err = json.Unmarshal(data, &sources); err != nil {
		return nil, fmt.Errorf("ACTION_SOURCE_BINDING: %s canonical captures: %w", action, err)
	}
	return sources, nil
}

type SubagentSource struct {
	Kind    string `json:"kind"`
	Path    string `json:"path"`
	Digest  string `json:"digest"`
	Content string `json:"content"`
}
