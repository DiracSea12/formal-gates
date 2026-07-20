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
	RunDir         string
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
	RunDir         string
	Gate           string
	Mode           string
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
	Transitions   []CarryTransitionRecord   `json:"transitions"`
}

type CarryTransitionRecord struct {
	WorkflowID         string `json:"workflowId"`
	ChangeSnapshot     string `json:"changeSnapshot"`
	ArbiterClosure     string `json:"arbiterClosure"`
	ArbiterClosureHash string `json:"arbiterClosureHash"`
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

var postDevelopmentGateOrder = []string{
	"qa-test-gate",
	"complexity-gate",
	"architecture-health-gate",
	"code-quality-gate",
}

func GateRecord(options GateRecordOptions) Result {
	if policy, ok := recordingPolicy(options.Gate, options.Stage, options.Mode); ok && policy.ArtifactRole == "FINAL_EXECUTION" {
		var result Result
		result.add("qa-test-gate", "FinalExecution can only be recorded by workflow final-verification --record-final-qa")
		return result
	}
	return gateRecord(options)
}

func gateRecord(options GateRecordOptions) Result {
	worktree := cleanRoot(options.Worktree)
	if strings.TrimSpace(options.RunDir) == "" {
		inferred := artifactRunDir(ArtifactOptions{Root: worktree, File: options.Artifact}, options.WorkflowID)
		if activeWorkflowRun(worktree, inferred) {
			options.RunDir = inferred
		}
	}
	statePath := resolveStatePath(worktree, options.StatePath)
	if strings.TrimSpace(options.StatePath) == "" && strings.TrimSpace(options.RunDir) != "" {
		statePath = filepath.Join(options.RunDir, "restricted", "gate-state.json")
	}
	var result Result
	if err := validateGateRecordOptions(worktree, options, &result); err != nil {
		result.add("gate-state", err.Error())
		return result
	}
	if !result.OK() {
		return result
	}

	policy, _ := recordingPolicy(options.Gate, options.Stage, options.Mode)
	flow := policy.Flow
	artifactPath := resolvePath(worktree, options.Artifact)
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		result.add("artifact", err.Error())
		return result
	}
	artifactOptions := ArtifactOptions{Root: worktree, File: options.Artifact, Gate: options.Gate, WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot, Stage: options.Stage, Flow: flow, RunDir: options.RunDir}
	decoded := decodeArtifact(artifactOptions, data, &result)
	validateCompositionProof(artifactOptions, &decoded, &result)
	if !result.OK() || decoded.Envelope.Verdict != options.Verdict {
		if result.OK() {
			result.add("artifact", "artifact verdict must match --verdict")
		}
		return result
	}
	if options.Verdict != "PASS" {
		return result
	}
	releaseState, err := acquireGateStateLock(statePath)
	if err != nil {
		result.add(slash(statePath), "cannot lock gate state: "+err.Error())
		return result
	}
	defer releaseState()
	state, err := loadGateState(statePath)
	if err != nil {
		result.add(slash(statePath), err.Error())
		return result
	}
	if decoded.Requirements != nil {
		if err := verifyRequirementsContinuity(worktree, statePath, options.RunDir, state, decoded); err != nil {
			result.add("requirements-clarification-gate", err.Error())
			return result
		}
	}
	if options.Verdict == "PASS" {
		for _, requirement := range decoded.Policy.Prerequisites {
			if err := verifyRequirement(worktree, statePath, options.RunDir, state, requirement, options.Gate, options.WorkflowID, options.ChangeSnapshot); err != nil {
				result.add("gate-state", err.Error())
				return result
			}
		}
	}

	storedArtifact := options.Artifact
	storedHash := hashArtifactIfPresent(worktree, options.Artifact)
	if decoded.Envelope.ArtifactRole != "FINAL_EXECUTION" {
		var receipt *EvidenceRef
		if decoded.Policy.ReceiptRequired {
			ref, err := matchingReceiptRef(artifactOptions, decoded)
			if err != nil {
				result.add("receipt", err.Error())
				return result
			}
			if err := validateDesignReviewIndependence(artifactOptions, decoded, ref); err != nil {
				result.add("receipt", err.Error())
				return result
			}
			receipt = &ref
		}
		closure, err := buildClosure(artifactOptions, decoded, receipt)
		if err != nil {
			result.add("closure", err.Error())
			return result
		}
		closurePath := filepath.Join(decoded.RunDir, filepath.FromSlash(closure.Path))
		storedArtifact, storedHash = relativePath(worktree, closurePath), closure.SHA256
	}
	entry := GateStateEntry{
		Gate:           options.Gate,
		Verdict:        options.Verdict,
		Mode:           flow,
		Stage:          options.Stage,
		Artifact:       storedArtifact,
		ArtifactHash:   storedHash,
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

func GateRecordTransition(options WorkflowRecordTransitionOptions) Result {
	worktree := cleanRoot(options.Worktree)
	statePath := resolveStatePath(worktree, options.StatePath)
	if strings.TrimSpace(options.StatePath) == "" && strings.TrimSpace(options.RunDir) != "" {
		statePath = filepath.Join(options.RunDir, "restricted", "gate-state.json")
	}
	var result Result
	for name, value := range map[string]string{"workflow-id": options.WorkflowID, "change-snapshot": options.ChangeSnapshot, "artifact": options.Artifact} {
		if strings.TrimSpace(value) == "" {
			result.add(name, "--"+name+" is required")
		}
	}
	if !result.OK() {
		return result
	}
	artifactPath := resolvePath(worktree, options.Artifact)
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		result.add("artifact", err.Error())
		return result
	}
	artifactOptions := ArtifactOptions{
		Root: worktree, RunDir: options.RunDir, File: options.Artifact,
		Gate: "qa-test-gate", Stage: "Carry", Flow: "carry",
		WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot,
	}
	decoded := decodeArtifact(artifactOptions, data, &result)
	if !result.OK() {
		return result
	}
	if decoded.Envelope.Verdict != "PASS" {
		result.add("artifact", "workflow record-transition accepts only a terminal PASS Carry Arbiter artifact")
		return result
	}
	receipt, err := matchingReceiptRef(artifactOptions, decoded)
	if err != nil {
		result.add("receipt", err.Error())
		return result
	}
	closure, err := buildClosure(artifactOptions, decoded, &receipt)
	if err != nil {
		result.add("closure", err.Error())
		return result
	}
	closurePath := filepath.Join(decoded.RunDir, filepath.FromSlash(closure.Path))
	storedPath := relativePath(worktree, closurePath)
	releaseState, err := acquireGateStateLock(statePath)
	if err != nil {
		result.add(slash(statePath), "cannot lock gate state: "+err.Error())
		return result
	}
	defer releaseState()
	state, err := loadGateState(statePath)
	if err != nil {
		result.add(slash(statePath), err.Error())
		return result
	}
	if err := validateCarryDecisionCoverage(state, decoded.CarryChain, decoded.Carry.Decisions, options.WorkflowID, options.ChangeSnapshot); err != nil {
		result.add("transition", err.Error())
		return result
	}
	for _, existing := range state.Transitions {
		if existing.WorkflowID != options.WorkflowID || existing.ChangeSnapshot != options.ChangeSnapshot {
			continue
		}
		if existing.ArbiterClosure != storedPath || existing.ArbiterClosureHash != closure.SHA256 {
			result.add("transition", "conflicting Carry transition already exists for workflow and target snapshot")
		}
		return result
	}
	state.Transitions = append(state.Transitions, CarryTransitionRecord{
		WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot,
		ArbiterClosure: storedPath, ArbiterClosureHash: closure.SHA256,
	})
	if err := writeGateState(statePath, state); err != nil {
		result.add(slash(statePath), err.Error())
	}
	return result
}

// validateCarryDecisionCoverage keeps the typed transition complete for the
// normal workflow: every prior PASS gate represented by the repair chain must
// receive exactly one independent decision.
func validateCarryDecisionCoverage(state GateState, chain *TransitionChain, decisions []CarryDecision, workflowID, targetSnapshot string) error {
	if chain == nil {
		return fmt.Errorf("transition chain is required before checking eligible Carry decisions")
	}
	sourceSnapshots := map[string]bool{}
	for _, hop := range chain.Hops {
		sourceSnapshots[hop.FromSnapshot] = true
	}
	eligible := map[string]bool{}
	for _, entry := range state.History {
		if entry.WorkflowID != workflowID || entry.Verdict != "PASS" || !sourceSnapshots[entry.ChangeSnapshot] {
			continue
		}
		if entry.Mode != "formal" && entry.Mode != "post-development" && entry.Mode != "" {
			continue
		}
		stage, role := sourceGateContract(entry.Gate)
		if role != "" && normalizeStage(entry.Stage) == normalizeStage(stage) {
			eligible[entry.Gate] = true
		}
	}
	if len(eligible) == 0 {
		return nil
	}
	decided := map[string]bool{}
	for _, decision := range decisions {
		if !eligible[decision.Gate] {
			return fmt.Errorf("Carry decision gate=%s has no eligible prior PASS for target=%s", decision.Gate, targetSnapshot)
		}
		decided[decision.Gate] = true
	}
	for gate := range eligible {
		if !decided[gate] {
			return fmt.Errorf("Carry decisions are incomplete: eligible prior PASS gate=%s has no decision", gate)
		}
	}
	return nil
}

func GateVerifyAdmission(options GateAdmissionOptions) Result {
	worktree := cleanRoot(options.Worktree)
	statePath := resolveStatePath(worktree, options.StatePath)
	if strings.TrimSpace(options.StatePath) == "" && strings.TrimSpace(options.WorkflowID) != "" {
		if runDir, err := resolveWorkflowRunDir(worktree, options.WorkflowID, options.RunDir); err == nil {
			candidate := filepath.Join(runDir, "restricted", "gate-state.json")
			if isFile(candidate) {
				options.RunDir = runDir
				statePath = candidate
			}
		}
	}
	var result Result
	if !knownGates[options.Gate] || options.Gate == "requirements-clarification-gate" {
		result.add("gate", "unknown post-development gate: "+options.Gate)
		return result
	}
	flow, flowOK := admissionFlow(strings.TrimSpace(options.Mode))
	policy, ok := admissionPolicy(options.Gate, flow)
	if !flowOK || !ok {
		result.add("gate", fmt.Sprintf("unsupported admission policy gate=%s flow=%s", options.Gate, flow))
		return result
	}
	requirements := policy.Prerequisites
	if flow == "post-development" || flow == "start-readiness" {
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
		if err := verifyRequirement(worktree, statePath, options.RunDir, state, requirement, options.Gate, options.WorkflowID, options.ChangeSnapshot); err != nil {
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

func verifyRequirement(worktree, statePath, runDir string, state GateState, requirement PolicyPrereq, requiredFor, workflowID, changeSnapshot string) error {
	entries := entriesForGateNewestFirst(state, requirement.Gate)
	for i := range entries {
		entry := entries[i]
		if entry.WorkflowID == workflowID && entry.ChangeSnapshot == changeSnapshot {
			if entry.Verdict != "PASS" {
				return fmt.Errorf("current-pass-not-real: gate=%s verdict=%s required=PASS requiredFor=%s state=%s", requirement.Gate, entry.Verdict, requiredFor, slash(statePath))
			}
			if requirement.Flow != "" && entry.Mode != requirement.Flow {
				return fmt.Errorf("current-pass-missing: gate=%s mode=%s requiredMode=%s requiredFor=%s state=%s", requirement.Gate, entry.Mode, requirement.Flow, requiredFor, slash(statePath))
			}
			if normalizeStage(entry.Stage) != normalizeStage(requirement.Stage) {
				return fmt.Errorf("current-pass-missing: gate=%s stage=%s requiredStage=%s requiredFor=%s state=%s", requirement.Gate, entry.Stage, requirement.Stage, requiredFor, slash(statePath))
			}
			if err := verifyEntryArtifact(worktree, statePath, runDir, entry, requiredFor); err != nil {
				return fmt.Errorf("current-pass-artifact-invalid: %s", err.Error())
			}
			return nil
		}
	}
	if requirement.Flow == "post-development" {
		if _, _, err := acceptedCarryForGate(worktree, runDir, state, workflowID, changeSnapshot, requirement.Gate); err == nil {
			return nil
		}
	}
	if len(entries) == 0 {
		return fmt.Errorf("current-pass-missing: missing prerequisite gate=%s requiredFor=%s state=%s", requirement.Gate, requiredFor, slash(statePath))
	}
	return fmt.Errorf("current-pass-missing: missing route gate=%s requiredFor=%s workflowId=%s changeSnapshot=%s state=%s", requirement.Gate, requiredFor, workflowID, changeSnapshot, slash(statePath))
}

func acceptedCarryForGate(worktree, runDir string, state GateState, workflowID, targetSnapshot, gate string) (CarryDecision, EvidenceRef, error) {
	for i := len(state.Transitions) - 1; i >= 0; i-- {
		transition := state.Transitions[i]
		if transition.WorkflowID != workflowID || transition.ChangeSnapshot != targetSnapshot {
			continue
		}
		closurePath := resolvePath(worktree, transition.ArbiterClosure)
		logical, err := logicalPathInRun(runDir, closurePath)
		if err != nil {
			return CarryDecision{}, EvidenceRef{}, err
		}
		ref := EvidenceRef{Path: logical, SHA256: transition.ArbiterClosureHash}
		options := ArtifactOptions{Root: worktree, RunDir: runDir, File: transition.ArbiterClosure, Gate: "qa-test-gate", Stage: "Carry", Flow: "carry", WorkflowID: workflowID, ChangeSnapshot: targetSnapshot}
		decision, err := validateAcceptedCarryDecision(options, ref, gate)
		return decision, ref, err
	}
	return CarryDecision{}, EvidenceRef{}, fmt.Errorf("accepted Carry transition is missing for workflow=%s targetSnapshot=%s", workflowID, targetSnapshot)
}

func verifyRequirementsContinuity(worktree, statePath, runDir string, state GateState, current decodedArtifact) error {
	var prior *GateStateEntry
	for _, entry := range entriesForGateNewestFirst(state, "requirements-clarification-gate") {
		if entry.WorkflowID == current.Envelope.WorkflowID && entry.Verdict == "PASS" && entry.Mode == "requirements" {
			copy := entry
			prior = &copy
			break
		}
	}
	if prior == nil {
		if current.Requirements.PreviousAlignment != nil {
			return fmt.Errorf("previousAlignment must be omitted before the first recorded requirements PASS")
		}
		return nil
	}
	if current.Requirements.PreviousAlignment == nil {
		return fmt.Errorf("previousAlignment must reference the actual prior requirements PASS alignment")
	}
	if err := verifyEntryArtifact(worktree, statePath, runDir, *prior, "requirements continuity"); err != nil {
		return fmt.Errorf("prior requirements PASS is invalid: %w", err)
	}
	closurePath := resolvePath(worktree, prior.Artifact)
	closureData, err := os.ReadFile(closurePath)
	if err != nil {
		return err
	}
	var closure EvidenceClosure
	if err := strictJSON(closureData, &closure); err != nil {
		return fmt.Errorf("prior requirements closure is invalid: %w", err)
	}
	closureRunDir := runDir
	if closureRunDir == "" {
		closureRunDir = filepath.Dir(filepath.Dir(closurePath))
	}
	rootPath, err := safeEvidencePath(closureRunDir, closure.RootArtifact)
	if err != nil {
		return fmt.Errorf("prior requirements root is invalid: %w", err)
	}
	rootData, err := os.ReadFile(rootPath)
	if err != nil {
		return err
	}
	var envelope FormalGateEvidence
	if err := strictContractJSON(rootData, &envelope); err != nil || envelope.ArtifactRole != "REQUIREMENTS_PASS" || envelope.Gate != "requirements-clarification-gate" || envelope.WorkflowID != prior.WorkflowID || envelope.ChangeSnapshot != prior.ChangeSnapshot || envelope.Verdict != "PASS" {
		return fmt.Errorf("prior requirements root does not match its recorded PASS")
	}
	var payload RequirementsPayload
	if err := strictContractJSON(envelope.Payload, &payload); err != nil {
		return fmt.Errorf("prior requirements payload is invalid: %w", err)
	}
	if *current.Requirements.PreviousAlignment != payload.Alignment {
		return fmt.Errorf("previousAlignment must equal the actual prior requirements PASS alignment reference")
	}
	return nil
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
	if strings.TrimSpace(options.WorkflowID) == "" {
		result.add("workflow-id", "--workflow-id is required")
	}
	if strings.TrimSpace(options.ChangeSnapshot) == "" {
		result.add("change-snapshot", "--change-snapshot is required")
	}
	if strings.TrimSpace(options.Artifact) == "" {
		result.add("artifact", "--artifact is required")
	}
	if _, ok := recordingPolicy(options.Gate, options.Stage, options.Mode); !ok {
		result.add(options.Gate, recordingPolicyMismatchMessage(options.Gate, options.Stage))
	}
	if strings.TrimSpace(options.Artifact) != "" && !isFile(resolvePath(worktree, options.Artifact)) {
		result.add("artifact", "artifact does not exist: "+options.Artifact)
	}
	return nil
}

func verifyEntryArtifact(worktree, statePath, runDir string, entry GateStateEntry, requiredFor string) error {
	if strings.TrimSpace(entry.Artifact) == "" {
		return fmt.Errorf("gate=%s artifactMissing requiredFor=%s state=%s", entry.Gate, requiredFor, slash(statePath))
	}
	artifactPath := resolvePath(worktree, entry.Artifact)
	if strings.TrimSpace(runDir) != "" {
		if err := requireAbsPathUnderRunDir(runDir, "artifact", artifactPath); err != nil {
			return fmt.Errorf("gate=%s artifactOutOfBounds=%s requiredFor=%s state=%s", entry.Gate, entry.Artifact, requiredFor, slash(statePath))
		}
	}
	if !isFile(artifactPath) {
		return fmt.Errorf("gate=%s artifactMissing=%s requiredFor=%s state=%s", entry.Gate, entry.Artifact, requiredFor, slash(statePath))
	}
	if strings.TrimSpace(entry.ArtifactHash) == "" {
		return fmt.Errorf("gate=%s artifactHashMissing=%s requiredFor=%s state=%s", entry.Gate, entry.Artifact, requiredFor, slash(statePath))
	}
	if actual := sha256File(artifactPath); actual != strings.ToLower(entry.ArtifactHash) {
		return fmt.Errorf("gate=%s artifactHashMismatch=%s requiredFor=%s state=%s", entry.Gate, entry.Artifact, requiredFor, slash(statePath))
	}
	data, err := os.ReadFile(artifactPath)
	if err != nil {
		return err
	}
	var closure EvidenceClosure
	if err := strictJSON(data, &closure); err == nil && closure.SchemaVersion == 2 {
		closureRunDir := runDir
		if closureRunDir == "" {
			closureRunDir = filepath.Dir(filepath.Dir(artifactPath))
		}
		if err := verifyClosure(ArtifactOptions{Root: worktree}, closureRunDir, closure); err != nil {
			return err
		}
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
	if state.SchemaVersion != 2 {
		return state, fmt.Errorf("old gate state schema is not compatible; start a new workflow")
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
	state.SchemaVersion = 2
	if state.Gates == nil {
		state.Gates = map[string]GateStateEntry{}
	}
	if state.History == nil {
		state.History = []GateStateEntry{}
	}
	if state.Transitions == nil {
		state.Transitions = []CarryTransitionRecord{}
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return writeFileAtomic(path, append(data, '\n'), 0o600)
}

func acquireGateStateLock(statePath string) (func(), error) {
	lockPath := statePath + ".lock"
	if err := os.MkdirAll(filepath.Dir(lockPath), 0o700); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		file, err := os.OpenFile(lockPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			if closeErr := file.Close(); closeErr != nil {
				_ = os.Remove(lockPath)
				return nil, closeErr
			}
			return func() { _ = os.Remove(lockPath) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if info, statErr := os.Stat(lockPath); statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			_ = os.Remove(lockPath)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for another gate-state update to finish")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func newGateState() GateState {
	return GateState{
		SchemaVersion: 2,
		Gates:         map[string]GateStateEntry{},
		History:       []GateStateEntry{},
		Transitions:   []CarryTransitionRecord{},
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
