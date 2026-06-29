package validate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type GateRecordOptions struct {
	Worktree       string
	StatePath      string
	Gate           string
	Verdict        string
	Mode           string
	Stage          string
	Artifact       string
	Actor          string
	WorkflowID     string
	ChangeSnapshot string
	Reason         string
}

type GateAdmissionOptions struct {
	Worktree       string
	StatePath      string
	Gate           string
	WorkflowID     string
	ChangeSnapshot string
}

type GateShowOptions struct {
	Worktree  string
	StatePath string
}

type GateState struct {
	SchemaVersion int                       `json:"schemaVersion"`
	Gates         map[string]GateStateEntry `json:"gates"`
	History       []GateStateEntry          `json:"history"`
}

type GateStateEntry struct {
	Gate           string `json:"gate"`
	Verdict        string `json:"verdict"`
	Mode           string `json:"mode"`
	Stage          string `json:"stage"`
	Artifact       string `json:"artifact"`
	ArtifactHash   string `json:"artifactHash"`
	Actor          string `json:"actor"`
	Reason         string `json:"reason"`
	WorkflowID     string `json:"workflowId"`
	ChangeSnapshot string `json:"changeSnapshot"`
	Worktree       string `json:"worktree"`
	StatePath      string `json:"statePath"`
	UpdatedAtUTC   string `json:"updatedAtUtc"`
}

var gateVerdicts = map[string]bool{
	"PASS":             true,
	"CONDITIONAL_PASS": true,
	"REVIEW":           true,
	"FAIL":             true,
	"BLOCKED":          true,
}

type admissionRequirement struct {
	gate     string
	mode     string
	stage    string
	artifact bool
}

func GateRecord(options GateRecordOptions) Result {
	worktree := cleanRoot(options.Worktree)
	statePath := resolveStatePath(worktree, options.StatePath)
	var result Result
	if err := validateGateRecordOptions(worktree, options, &result); err != nil {
		result.add("gate-state", err.Error())
		return result
	}
	if !result.OK() {
		return result
	}

	state, err := loadGateState(statePath)
	if err != nil {
		result.add(slash(statePath), err.Error())
		return result
	}

	if options.Verdict == "PASS" {
		if err := validatePassArtifact(worktree, options); err != nil {
			result.add("gate-state", err.Error())
			return result
		}
		for _, requirement := range recordAdmissionRequirements(options) {
			if err := verifyRequirement(worktree, statePath, state, requirement, options.Gate, options.WorkflowID, options.ChangeSnapshot); err != nil {
				result.add("gate-state", err.Error())
				return result
			}
		}
	}

	entry := GateStateEntry{
		Gate:           options.Gate,
		Verdict:        options.Verdict,
		Mode:           options.Mode,
		Stage:          options.Stage,
		Artifact:       options.Artifact,
		ArtifactHash:   hashArtifactIfPresent(worktree, options.Artifact),
		Actor:          options.Actor,
		Reason:         options.Reason,
		WorkflowID:     options.WorkflowID,
		ChangeSnapshot: options.ChangeSnapshot,
		Worktree:       slash(absPath(worktree)),
		StatePath:      slash(absPath(statePath)),
		UpdatedAtUTC:   time.Now().UTC().Format(time.RFC3339Nano),
	}
	state.Gates[options.Gate] = entry
	state.History = append(state.History, entry)
	if err := writeGateState(statePath, state); err != nil {
		result.add(slash(statePath), err.Error())
	}
	return result
}

func GateVerifyAdmission(options GateAdmissionOptions) Result {
	worktree := cleanRoot(options.Worktree)
	statePath := resolveStatePath(worktree, options.StatePath)
	var result Result
	if !knownGates[options.Gate] || options.Gate == "requirements-clarification-gate" {
		result.add("gate", "unknown post-development gate: "+options.Gate)
		return result
	}
	requirements := admissionRequirements(options.Gate)
	if len(requirements) > 0 {
		if strings.TrimSpace(options.WorkflowID) == "" {
			result.add("workflow-id", "--workflow-id is required for admission checks")
		}
		if strings.TrimSpace(options.ChangeSnapshot) == "" {
			result.add("change-snapshot", "--change-snapshot is required for admission checks")
		}
	}
	if !result.OK() {
		return result
	}
	state, err := loadGateState(statePath)
	if err != nil {
		result.add(slash(statePath), err.Error())
		return result
	}
	for _, requirement := range requirements {
		if err := verifyRequirement(worktree, statePath, state, requirement, options.Gate, options.WorkflowID, options.ChangeSnapshot); err != nil {
			result.add("gate-state", err.Error())
			return result
		}
	}
	return result
}

func GateShow(options GateShowOptions) (GateState, Result) {
	worktree := cleanRoot(options.Worktree)
	statePath := resolveStatePath(worktree, options.StatePath)
	var result Result
	state, err := loadGateState(statePath)
	if err != nil {
		result.add(slash(statePath), err.Error())
		return GateState{}, result
	}
	if err := writeGateState(statePath, state); err != nil {
		result.add(slash(statePath), err.Error())
		return GateState{}, result
	}
	return state, result
}

func GateStateText(state GateState) string {
	keys := make([]string, 0, len(state.Gates))
	for gate := range state.Gates {
		keys = append(keys, gate)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "schemaVersion=%d history=%d\n", state.SchemaVersion, len(state.History))
	for _, gate := range keys {
		entry := state.Gates[gate]
		fmt.Fprintf(&b, "gate=%s verdict=%s workflowId=%s changeSnapshot=%s mode=%s stage=%s artifact=%s\n",
			entry.Gate, entry.Verdict, entry.WorkflowID, entry.ChangeSnapshot, entry.Mode, entry.Stage, entry.Artifact)
	}
	return strings.TrimRight(b.String(), "\n")
}

func GateStateJSON(state GateState) ([]byte, error) {
	return json.MarshalIndent(state, "", "  ")
}

func validateGateRecordOptions(worktree string, options GateRecordOptions, result *Result) error {
	if !knownGates[options.Gate] {
		return fmt.Errorf("unknown gate: %s", options.Gate)
	}
	if !gateVerdicts[options.Verdict] {
		return fmt.Errorf("unknown verdict: %s", options.Verdict)
	}
	if options.Verdict != "PASS" {
		return nil
	}
	if strings.TrimSpace(options.WorkflowID) == "" {
		result.add("workflow-id", "--workflow-id is required when recording PASS")
	}
	if strings.TrimSpace(options.ChangeSnapshot) == "" {
		result.add("change-snapshot", "--change-snapshot is required when recording PASS")
	}
	if strings.TrimSpace(options.Artifact) == "" {
		result.add("artifact", "--artifact is required when recording PASS")
	}
	if options.Gate == "qa-test-gate" && (options.Mode != "formal" || (options.Stage != "Execution" && options.Stage != "FinalExecution")) {
		result.add("qa-test-gate", "PASS requires --mode formal and --stage Execution or FinalExecution")
	}
	if strings.TrimSpace(options.Artifact) != "" && !isFile(resolvePath(worktree, options.Artifact)) {
		result.add("artifact", "artifact does not exist: "+options.Artifact)
	}
	return nil
}

func validatePassArtifact(worktree string, options GateRecordOptions) error {
	artifactResult := Artifact(ArtifactOptions{
		Root:           worktree,
		File:           options.Artifact,
		Gate:           options.Gate,
		WorkflowID:     options.WorkflowID,
		ChangeSnapshot: options.ChangeSnapshot,
		Stage:          options.Stage,
	})
	if artifactResult.OK() {
		return nil
	}
	messages := make([]string, 0, len(artifactResult.Failures))
	for _, failure := range artifactResult.Failures {
		messages = append(messages, failure.Path+": "+failure.Message)
	}
	return fmt.Errorf("PASS artifact validation failed: %s", strings.Join(messages, "; "))
}

func admissionRequirements(gate string) []admissionRequirement {
	switch gate {
	case "qa-test-gate":
		return nil
	case "complexity-gate":
		return []admissionRequirement{{gate: "qa-test-gate", mode: "formal", stage: "Execution", artifact: true}}
	case "architecture-health-gate":
		return []admissionRequirement{
			{gate: "qa-test-gate", mode: "formal", stage: "Execution", artifact: true},
			{gate: "complexity-gate", artifact: true},
		}
	case "code-quality-gate":
		return []admissionRequirement{
			{gate: "qa-test-gate", mode: "formal", stage: "Execution", artifact: true},
			{gate: "complexity-gate", artifact: true},
			{gate: "architecture-health-gate", artifact: true},
		}
	default:
		return nil
	}
}

func recordAdmissionRequirements(options GateRecordOptions) []admissionRequirement {
	if options.Gate == "qa-test-gate" && options.Stage == "FinalExecution" {
		return []admissionRequirement{
			{gate: "qa-test-gate", mode: "formal", stage: "Execution", artifact: true},
			{gate: "complexity-gate", artifact: true},
			{gate: "architecture-health-gate", artifact: true},
			{gate: "code-quality-gate", artifact: true},
		}
	}
	return admissionRequirements(options.Gate)
}

func verifyRequirement(worktree, statePath string, state GateState, requirement admissionRequirement, requiredFor, workflowID, changeSnapshot string) error {
	entries := entriesForGateNewestFirst(state, requirement.gate)
	if len(entries) == 0 {
		return fmt.Errorf("missing prerequisite gate=%s requiredFor=%s state=%s", requirement.gate, requiredFor, slash(statePath))
	}
	var latestRoute *GateStateEntry
	for i := range entries {
		entry := entries[i]
		if entry.WorkflowID == workflowID && entry.ChangeSnapshot == changeSnapshot {
			if latestRoute == nil {
				copy := entry
				latestRoute = &copy
			}
			if entry.Verdict != "PASS" {
				return fmt.Errorf("gate=%s verdict=%s required=PASS requiredFor=%s state=%s", requirement.gate, entry.Verdict, requiredFor, slash(statePath))
			}
			if requirement.mode != "" && entry.Mode != requirement.mode {
				continue
			}
			if requirement.stage != "" && entry.Stage != requirement.stage {
				continue
			}
			if requirement.artifact {
				if err := verifyEntryArtifact(worktree, statePath, entry, requiredFor); err != nil {
					return err
				}
			}
			return nil
		}
	}
	if latestRoute == nil {
		return fmt.Errorf("missing route gate=%s requiredFor=%s workflowId=%s changeSnapshot=%s state=%s", requirement.gate, requiredFor, workflowID, changeSnapshot, slash(statePath))
	}
	if requirement.mode != "" && latestRoute.Mode != requirement.mode {
		return fmt.Errorf("gate=%s mode=%s requiredMode=%s requiredFor=%s state=%s", requirement.gate, latestRoute.Mode, requirement.mode, requiredFor, slash(statePath))
	}
	if requirement.stage != "" && latestRoute.Stage != requirement.stage {
		return fmt.Errorf("gate=%s stage=%s requiredStage=%s requiredFor=%s state=%s", requirement.gate, latestRoute.Stage, requirement.stage, requiredFor, slash(statePath))
	}
	return fmt.Errorf("gate=%s prerequisite did not satisfy admission for %s", requirement.gate, requiredFor)
}

func verifyEntryArtifact(worktree, statePath string, entry GateStateEntry, requiredFor string) error {
	if strings.TrimSpace(entry.Artifact) == "" {
		return fmt.Errorf("gate=%s artifactMissing requiredFor=%s state=%s", entry.Gate, requiredFor, slash(statePath))
	}
	artifactPath := resolvePath(worktree, entry.Artifact)
	if !isFile(artifactPath) {
		return fmt.Errorf("gate=%s artifactMissing=%s requiredFor=%s state=%s", entry.Gate, entry.Artifact, requiredFor, slash(statePath))
	}
	if strings.TrimSpace(entry.ArtifactHash) == "" {
		return fmt.Errorf("gate=%s artifactHashMissing=%s requiredFor=%s state=%s", entry.Gate, entry.Artifact, requiredFor, slash(statePath))
	}
	if actual := sha256File(artifactPath); actual != strings.ToLower(entry.ArtifactHash) {
		return fmt.Errorf("gate=%s artifactHashMismatch=%s requiredFor=%s state=%s", entry.Gate, entry.Artifact, requiredFor, slash(statePath))
	}
	return nil
}

func entriesForGateNewestFirst(state GateState, gate string) []GateStateEntry {
	entries := make([]GateStateEntry, 0, len(state.History)+1)
	for i := len(state.History) - 1; i >= 0; i-- {
		if state.History[i].Gate == gate {
			entries = append(entries, state.History[i])
		}
	}
	if len(entries) == 0 {
		if entry, ok := state.Gates[gate]; ok {
			entries = append(entries, entry)
		}
	}
	return entries
}

func loadGateState(path string) (GateState, error) {
	state := newGateState()
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return state, nil
	}
	if err != nil {
		return state, err
	}
	if info.IsDir() {
		return state, fmt.Errorf("state path is a directory")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return state, err
	}
	if strings.TrimSpace(string(data)) == "" {
		return state, nil
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, fmt.Errorf("state JSON is invalid: %w", err)
	}
	if state.SchemaVersion == 0 {
		state.SchemaVersion = 1
	}
	if state.Gates == nil {
		state.Gates = map[string]GateStateEntry{}
	}
	if state.History == nil {
		state.History = []GateStateEntry{}
	}
	return state, nil
}

func writeGateState(path string, state GateState) error {
	if state.SchemaVersion == 0 {
		state.SchemaVersion = 1
	}
	if state.Gates == nil {
		state.Gates = map[string]GateStateEntry{}
	}
	if state.History == nil {
		state.History = []GateStateEntry{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o600)
}

func newGateState() GateState {
	return GateState{
		SchemaVersion: 1,
		Gates:         map[string]GateStateEntry{},
		History:       []GateStateEntry{},
	}
}

func resolveStatePath(worktree, statePath string) string {
	if strings.TrimSpace(statePath) != "" {
		if filepath.IsAbs(statePath) {
			return filepath.Clean(statePath)
		}
		return filepath.Clean(filepath.Join(worktree, filepath.FromSlash(statePath)))
	}
	return filepath.Join(worktree, ".claude", "gates", "gate-state.json")
}

func hashArtifactIfPresent(worktree, artifact string) string {
	if strings.TrimSpace(artifact) == "" {
		return ""
	}
	path := resolvePath(worktree, artifact)
	if !isFile(path) {
		return ""
	}
	return sha256File(path)
}

func absPath(path string) string {
	full, err := filepath.Abs(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(full)
}
