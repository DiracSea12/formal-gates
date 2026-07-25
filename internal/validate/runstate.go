package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type RunState struct {
	RunID                string                       `json:"runId"`
	Flow                 string                       `json:"flow"`
	Status               string                       `json:"status"`
	RequirementSource    string                       `json:"requirementSource"`
	RequirementRevision  string                       `json:"requirementRevision"`
	RequirementConfirmed bool                         `json:"requirementConfirmed"`
	BasePromptRevision   string                       `json:"basePromptRevision"`
	CatalogRevision      string                       `json:"catalogRevision"`
	VCS                  string                       `json:"vcs"`
	BaseSnapshot         string                       `json:"baseSnapshot"`
	CurrentSnapshot      string                       `json:"currentSnapshot"`
	PreRepairSnapshot    string                       `json:"preRepairSnapshot,omitempty"`
	RouteMode            string                       `json:"routeMode,omitempty"`
	SelectedGates        []string                     `json:"selectedGates"`
	SkipAuthorizations   map[string]SkipAuthorization `json:"skipAuthorizations"`
	CompletedReviewWaves int                          `json:"completedReviewWaves"`
	ExtraReviewWaves     int                          `json:"extraReviewWaves"`
	Actions              map[string]ActionResult      `json:"actions"`
	QACases              []QACase                     `json:"qaCases"`
	QAExecution          QAExecutionResult            `json:"qaExecution"`
	Gates                map[string]GateResult        `json:"gates"`
	Carry                map[string]CarryResult       `json:"carry"`
}

type ActionResult struct {
	Status   string    `json:"status"`
	Message  string    `json:"message,omitempty"`
	Findings []Finding `json:"findings,omitempty"`
}

type QAExecutionResult struct {
	Status   string           `json:"status,omitempty"`
	Message  string           `json:"message,omitempty"`
	Snapshot string           `json:"snapshot,omitempty"`
	Cases    []QAResultRecord `json:"cases,omitempty"`
	Findings []Finding        `json:"findings,omitempty"`
}

type QAResultRecord struct {
	CaseID       string `json:"caseId"`
	Outcome      string `json:"outcome"`
	Procedure    string `json:"procedure"`
	Observation  string `json:"observation"`
	OracleResult string `json:"oracleResult"`
}

type Finding struct {
	Severity  string   `json:"severity,omitempty"`
	Message   string   `json:"message"`
	Locations []string `json:"locations,omitempty"`
}

type SkipAuthorization struct {
	Origin   string `json:"origin"`
	Status   string `json:"status"`
	Snapshot string `json:"snapshot,omitempty"`
}

type GateResult struct {
	Status         string    `json:"status"`
	Snapshot       string    `json:"snapshot,omitempty"`
	SourceSnapshot string    `json:"sourceSnapshot,omitempty"`
	Findings       []Finding `json:"findings,omitempty"`
	Message        string    `json:"message,omitempty"`
}

type CarryResult struct {
	Decision       string `json:"decision"`
	SourceSnapshot string `json:"sourceSnapshot"`
	TargetSnapshot string `json:"targetSnapshot"`
	Message        string `json:"message,omitempty"`
}

type QACase struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Procedure   string `json:"procedure"`
	Oracle      string `json:"oracle"`
}

type RunSummary struct {
	RunID                string                       `json:"runId"`
	Flow                 string                       `json:"flow"`
	Status               string                       `json:"status"`
	RequirementRevision  string                       `json:"requirementRevision"`
	BasePromptRevision   string                       `json:"basePromptRevision"`
	CatalogRevision      string                       `json:"catalogRevision"`
	VCS                  string                       `json:"vcs"`
	BaseSnapshot         string                       `json:"baseSnapshot"`
	CurrentSnapshot      string                       `json:"currentSnapshot"`
	RouteMode            string                       `json:"routeMode"`
	SelectedGates        []string                     `json:"selectedGates"`
	SkipAuthorizations   map[string]SkipAuthorization `json:"skipAuthorizations"`
	CompletedReviewWaves int                          `json:"completedReviewWaves"`
	ExtraReviewWaves     int                          `json:"extraReviewWaves"`
	Gates                map[string]GateResult        `json:"gates"`
	QA                   QAExecutionResult            `json:"qaExecution"`
}

func NewRunState(runID, flow, requirementSource, requirementRevision, vcs, baseSnapshot, currentSnapshot, basePromptRevision, catalogRevision string, confirmed bool, gateIDs []string) RunState {
	gates := map[string]GateResult{}
	for _, id := range gateIDs {
		gates[id] = GateResult{Status: "PENDING"}
	}
	return RunState{RunID: runID, Flow: flow, Status: "ACTIVE", RequirementSource: requirementSource, RequirementRevision: requirementRevision, RequirementConfirmed: confirmed, BasePromptRevision: basePromptRevision, CatalogRevision: catalogRevision, VCS: vcs, BaseSnapshot: baseSnapshot, CurrentSnapshot: currentSnapshot, SelectedGates: []string{}, SkipAuthorizations: map[string]SkipAuthorization{}, Actions: pendingRequirementActions(), QACases: []QACase{}, QAExecution: QAExecutionResult{Status: "PENDING"}, Gates: gates, Carry: map[string]CarryResult{}}
}

func pendingRequirementActions() map[string]ActionResult {
	return map[string]ActionResult{"requirements-clarification": {Status: "PENDING"}, "start-readiness": {Status: "PENDING"}, "qa-design": {Status: "PENDING"}, "development-worker": {Status: "PENDING"}}
}

func RunDir(root, runID string) string {
	return filepath.Join(cleanRoot(root), ".gates", "tmp", runID)
}

func RunStatePath(root, runID string) string {
	return filepath.Join(RunDir(root, runID), "state.json")
}

func SaveRunState(root string, state RunState) error {
	if strings.TrimSpace(state.RunID) == "" {
		return fmt.Errorf("run id is required")
	}
	if state.Status != "ACTIVE" && state.Status != "SEALED" && state.Status != "ABORTED" {
		return fmt.Errorf("invalid run status %q", state.Status)
	}
	if state.Actions == nil {
		state.Actions = map[string]ActionResult{}
	}
	if state.Gates == nil {
		state.Gates = map[string]GateResult{}
	}
	if state.Carry == nil {
		state.Carry = map[string]CarryResult{}
	}
	if state.SkipAuthorizations == nil {
		state.SkipAuthorizations = map[string]SkipAuthorization{}
	}
	if state.SelectedGates == nil {
		state.SelectedGates = []string{}
	}
	if state.QACases == nil {
		state.QACases = []QACase{}
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	path := RunStatePath(root, state.RunID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return writeAtomic(path, append(data, '\n'), 0o600)
}

func LoadRunState(root, runID string) (RunState, error) {
	data, err := os.ReadFile(RunStatePath(root, runID))
	if err != nil {
		return RunState{}, err
	}
	var state RunState
	if err := json.Unmarshal(data, &state); err != nil {
		return RunState{}, fmt.Errorf("state JSON is invalid: %w", err)
	}
	if state.RunID != runID {
		return RunState{}, fmt.Errorf("state run id does not match %q", runID)
	}
	return state, nil
}

func DeleteRun(root, runID string) error {
	return os.RemoveAll(RunDir(root, runID))
}

func SaveRunSummary(root string, state RunState) error {
	summary := runSummary(state)
	data, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		return err
	}
	dir := filepath.Join(cleanRoot(root), ".gates", "results")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return writeAtomic(RunSummaryPath(root, state.RunID), append(data, '\n'), 0o600)
}

func RunSummaryPath(root, runID string) string {
	return filepath.Join(cleanRoot(root), ".gates", "results", runID+".json")
}

func runSummary(state RunState) RunSummary {
	return RunSummary{RunID: state.RunID, Flow: state.Flow, Status: state.Status, RequirementRevision: state.RequirementRevision, BasePromptRevision: state.BasePromptRevision, CatalogRevision: state.CatalogRevision, VCS: state.VCS, BaseSnapshot: state.BaseSnapshot, CurrentSnapshot: state.CurrentSnapshot, RouteMode: state.RouteMode, SelectedGates: state.SelectedGates, SkipAuthorizations: state.SkipAuthorizations, CompletedReviewWaves: state.CompletedReviewWaves, ExtraReviewWaves: state.ExtraReviewWaves, Gates: state.Gates, QA: state.QAExecution}
}

func RequirementRevision(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func writeAtomic(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".state-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(mode); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceCompletedFile(tmpName, path)
}
