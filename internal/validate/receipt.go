package validate

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"
)

type ReceiptRegisterOptions struct {
	Worktree                  string
	RunDir                    string
	Provider                  string
	WorkflowID                string
	ChangeSnapshot            string
	Gate                      string
	Stage                     string
	Artifact                  string
	ContextBundle             string
	Prompt                    string
	UserAuthorizedExtraReview bool
}

type ReceiptRegistration struct {
	DispatchID                     string `json:"dispatchId"`
	DispatchRegistrationArtifact   string `json:"dispatchRegistrationArtifact"`
	DispatchRegistrationStatusText string `json:"status"`
}

type ReceiptCaptureOptions struct {
	Worktree string
	RunDir   string
	Provider string
	Event    string
	Payload  []byte
}

type ReceiptCaptureEvent struct {
	EventArtifact   string `json:"eventArtifact"`
	EventSha256     string `json:"eventSha256"`
	NormalizedEvent string `json:"normalizedEvent"`
	Status          string `json:"status"`
}

type ReceiptFinalizeOptions struct {
	Worktree   string
	RunDir     string
	Provider   string
	WorkflowID string
	Gate       string
	Stage      string
	Artifact   string
}

type ReceiptFinalizeOutput struct {
	ReceiptArtifact string `json:"receiptArtifact"`
	ReceiptSha256   string `json:"receiptSha256"`
}

type ReceiptValidateOptions struct {
	Worktree       string
	Receipt        string
	Artifact       string
	Gate           string
	Stage          string
	WorkflowID     string
	ChangeSnapshot string
}

type ReceiptPreflightOptions struct {
	Host     string
	Worktree string
}

type ReceiptPreflightReport struct {
	Status                   string              `json:"status"`
	Host                     string              `json:"host"`
	Provider                 string              `json:"provider,omitempty"`
	Worktree                 string              `json:"worktree"`
	ConfigPath               string              `json:"configPath,omitempty"`
	CheckedConfigPaths       []string            `json:"checkedConfigPaths,omitempty"`
	RequiredLifecycleEvents  []string            `json:"requiredLifecycleEvents"`
	ConfiguredLifecycleHooks map[string][]string `json:"configuredLifecycleHooks,omitempty"`
	UsableCorrelationFields  []string            `json:"usableCorrelationFields"`
	RawPayloadArtifacts      []string            `json:"rawPayloadArtifacts"`
	Missing                  []string            `json:"missing"`
}

type dispatchRegistration struct {
	ProofVersion          int    `json:"proofVersion"`
	DispatchID            string `json:"dispatchId"`
	Provider              string `json:"provider"`
	WorkflowID            string `json:"workflowId"`
	ChangeSnapshot        string `json:"changeSnapshot"`
	Gate                  string `json:"gate"`
	Stage                 string `json:"stage"`
	ReviewArtifact        string `json:"reviewArtifact"`
	ContextBundle         string `json:"contextBundle,omitempty"`
	ContextSHA256         string `json:"contextSha256,omitempty"`
	ExtraReviewAuthorized bool   `json:"extraReviewAuthorized,omitempty"`
	PromptArtifact        string `json:"promptArtifact,omitempty"`
	PromptSha256          string `json:"promptSha256,omitempty"`
	ReceiptArtifact       string `json:"receiptArtifact,omitempty"`
	Status                string `json:"status"`
}

type reviewerProofReceipt struct {
	ProofVersion                 int      `json:"proofVersion"`
	Provider                     string   `json:"provider"`
	WorkflowID                   string   `json:"workflowId"`
	ChangeSnapshot               string   `json:"changeSnapshot"`
	Gate                         string   `json:"gate"`
	Stage                        string   `json:"stage"`
	DispatchID                   string   `json:"dispatchId"`
	DispatchRegistrationArtifact string   `json:"dispatchRegistrationArtifact"`
	DispatchRegistrationSha256   string   `json:"dispatchRegistrationSha256"`
	SubagentID                   string   `json:"subagentId"`
	NormalizedEvents             []string `json:"normalizedEvents"`
	StartEventArtifact           string   `json:"startEventArtifact"`
	StartEventSha256             string   `json:"startEventSha256"`
	StopEventArtifact            string   `json:"stopEventArtifact"`
	StopEventSha256              string   `json:"stopEventSha256"`
	ReviewArtifact               string   `json:"reviewArtifact"`
	ReviewArtifactSha256         string   `json:"reviewArtifactSha256"`
	PromptArtifact               string   `json:"promptArtifact,omitempty"`
	PromptSha256                 string   `json:"promptSha256,omitempty"`
}

type receiptEventRecord struct {
	Provider                     string `json:"provider"`
	WorkflowID                   string `json:"workflowId"`
	ChangeSnapshot               string `json:"changeSnapshot"`
	Gate                         string `json:"gate"`
	Stage                        string `json:"stage"`
	NormalizedEvent              string `json:"normalizedEvent"`
	RawEventName                 string `json:"rawEventName"`
	SubagentID                   string `json:"subagentId"`
	Status                       string `json:"status"`
	DispatchID                   string `json:"dispatchId,omitempty"`
	DispatchRegistrationArtifact string `json:"dispatchRegistrationArtifact,omitempty"`
	CapturedAtUTC                string `json:"capturedAtUtc"`
	RawPayload                   any    `json:"rawPayload,omitempty"`
	RawPayloadText               string `json:"rawPayloadText,omitempty"`
}

func ReceiptRegisterDispatch(options ReceiptRegisterOptions) (ReceiptRegistration, Result) {
	var result Result
	repo := cleanWorktree(options.Worktree)
	if !knownReceiptProvider(options.Provider) {
		result.add("receipt", "unsupported provider: "+options.Provider)
		return ReceiptRegistration{}, result
	}
	for flag, value := range map[string]string{"workflow-id": options.WorkflowID, "gate": options.Gate, "artifact": options.Artifact, "context-bundle": options.ContextBundle} {
		if strings.TrimSpace(value) == "" {
			result.add("receipt", "--"+flag+" is required")
		}
	}
	if !meaningful(options.ChangeSnapshot) {
		result.add("receipt", "--change-snapshot must be meaningful")
	}
	if !result.OK() {
		return ReceiptRegistration{}, result
	}
	runDir, err := resolveWorkflowRunDir(repo, options.WorkflowID, options.RunDir)
	if err != nil {
		result.add("run-dir", err.Error())
		return ReceiptRegistration{}, result
	}
	promptArtifact, promptSHA256 := "", ""
	var promptPath string
	var registeredPromptBytes []byte
	if reviewJudgmentLifecycle(options.Gate, options.Stage) {
		if strings.TrimSpace(options.Prompt) == "" {
			result.add("receipt", "--prompt is required for a review judgment")
			return ReceiptRegistration{}, result
		}
		promptPath, err = safeEvidencePath(runDir, options.Prompt)
		if err != nil || !restrictedEvidencePath(repo, runDir, options.Prompt) {
			if err != nil {
				result.add(options.Prompt, err.Error())
			} else {
				result.add(options.Prompt, "final-send prompt must be under the active run restricted directory")
			}
			return ReceiptRegistration{}, result
		}
		promptArtifact = relativePath(repo, promptPath)
		promptSHA256 = sha256File(promptPath)
		if !validateFinalSendPrompt(repo, runDir, promptArtifact, promptSHA256, options.Gate, options.Stage, &result, options.Prompt) {
			return ReceiptRegistration{}, result
		}
	}
	bundlePath, err := safeEvidencePath(runDir, options.ContextBundle)
	if err != nil {
		result.add(options.ContextBundle, err.Error())
		return ReceiptRegistration{}, result
	}
	bundleData, ok := readReviewerEvidenceRef(ArtifactOptions{Root: repo}, runDir, EvidenceRef{Path: options.ContextBundle, SHA256: sha256File(bundlePath)}, &result)
	if !ok {
		return ReceiptRegistration{}, result
	}
	contextRefs := validateContextBundle(ArtifactOptions{Root: repo, File: options.ContextBundle}, runDir, options.ContextBundle, bundleData, options.WorkflowID, strings.TrimSpace(options.ChangeSnapshot), &result)
	if reviewJudgmentLifecycle(options.Gate, options.Stage) {
		policyID := validateDispatchRegistrationContract(repo, runDir, promptArtifact, promptSHA256, options, bundlePath, &result)
		validateDispatchContextForPolicy(repo, runDir, policyID, contextRefs, &result)
	}
	if !result.OK() {
		return ReceiptRegistration{}, result
	}
	if reviewJudgmentLifecycle(options.Gate, options.Stage) {
		promptData, readErr := os.ReadFile(promptPath)
		if readErr != nil {
			result.add(options.Prompt, readErr.Error())
			return ReceiptRegistration{}, result
		}
		registeredPrompt, markerErr := addDispatchStaticValidation(string(promptData))
		if markerErr != nil {
			result.add(options.Prompt, markerErr.Error())
			return ReceiptRegistration{}, result
		}
		registeredPromptBytes = []byte(registeredPrompt)
		promptSHA256 = sha256Bytes(registeredPromptBytes)
		var markerResult Result
		if !validateDispatchStaticMarker(registeredPrompt, &markerResult, options.Prompt) {
			return ReceiptRegistration{}, markerResult
		}
	}
	artifactPath, err := reserveAbsentReviewArtifact(repo, runDir, options.Artifact)
	if err != nil {
		result.add(options.Artifact, err.Error())
		return ReceiptRegistration{}, result
	}
	release, err := acquireReceiptRegistrationLock(repo, runDir)
	if err != nil {
		result.add("receipt", err.Error())
		return ReceiptRegistration{}, result
	}
	defer release()
	id := newReceiptID()
	path := filepath.Join(receiptProofDir(repo, runDir, "dispatch"), sha256Bytes([]byte(artifactPath))+".json")
	record := dispatchRegistration{
		ProofVersion:          1,
		DispatchID:            id,
		Provider:              options.Provider,
		WorkflowID:            options.WorkflowID,
		ChangeSnapshot:        strings.TrimSpace(options.ChangeSnapshot),
		Gate:                  options.Gate,
		Stage:                 normalizeStage(options.Stage),
		ReviewArtifact:        relativePath(repo, artifactPath),
		ContextBundle:         slash(relativePath(runDir, bundlePath)),
		ContextSHA256:         sha256File(bundlePath),
		ExtraReviewAuthorized: options.UserAuthorizedExtraReview,
		PromptArtifact:        promptArtifact,
		PromptSha256:          promptSHA256,
		Status:                "open",
	}
	if existing, ok := decodeDispatch(path); ok {
		if existing.Status != "open" || strings.TrimSpace(existing.ReceiptArtifact) != "" {
			result.add("receipt", "review artifact path is already bound to a completed dispatch; use a distinct output path")
			return ReceiptRegistration{}, result
		}
		if hasLifecycleEventForDispatch(repo, runDir, existing.DispatchID, relativePath(repo, path)) {
			result.add("receipt", "review artifact path is already bound to a dispatch that has started; it cannot be rebound")
			return ReceiptRegistration{}, result
		}
		if existing.Provider == record.Provider && existing.WorkflowID == record.WorkflowID && existing.ChangeSnapshot == record.ChangeSnapshot && existing.Gate == record.Gate && normalizeStage(existing.Stage) == normalizeStage(record.Stage) && existing.ReviewArtifact == record.ReviewArtifact && existing.ContextBundle == record.ContextBundle && existing.ContextSHA256 == record.ContextSHA256 && existing.PromptArtifact == record.PromptArtifact && existing.PromptSha256 == record.PromptSha256 {
			if len(registeredPromptBytes) > 0 {
				if err := writeFileAtomic(promptPath, registeredPromptBytes, 0o600); err != nil {
					result.add(options.Prompt, err.Error())
					return ReceiptRegistration{}, result
				}
			}
			result.add("receipt", "review artifact path is already reserved by this dispatch")
			return ReceiptRegistration{}, result
		}
		if existing.WorkflowID != record.WorkflowID || existing.Gate != record.Gate || normalizeStage(existing.Stage) != normalizeStage(record.Stage) {
			if err := enforceReviewCapacity(repo, runDir, record.WorkflowID, record.Gate, record.Stage, options.UserAuthorizedExtraReview); err != nil {
				result.add("receipt", err.Error())
				return ReceiptRegistration{}, result
			}
		}
		if len(registeredPromptBytes) > 0 {
			if err := writeFileAtomic(promptPath, registeredPromptBytes, 0o600); err != nil {
				result.add(options.Prompt, err.Error())
				return ReceiptRegistration{}, result
			}
		}
		if err := writeJSON(path, record); err != nil {
			result.add("receipt", err.Error())
			return ReceiptRegistration{}, result
		}
		return ReceiptRegistration{
			DispatchID:                     id,
			DispatchRegistrationArtifact:   relativePath(repo, path),
			DispatchRegistrationStatusText: "rebound",
		}, result
	}
	if err := enforceReviewCapacity(repo, runDir, record.WorkflowID, record.Gate, record.Stage, options.UserAuthorizedExtraReview); err != nil {
		result.add("receipt", err.Error())
		return ReceiptRegistration{}, result
	}
	if len(registeredPromptBytes) > 0 {
		if err := writeFileAtomic(promptPath, registeredPromptBytes, 0o600); err != nil {
			result.add(options.Prompt, err.Error())
			return ReceiptRegistration{}, result
		}
	}
	if err := writeJSONExclusive(path, record); err != nil {
		if os.IsExist(err) {
			result.add("receipt", "review artifact path is already reserved by a dispatch; use a distinct output path for each review attempt")
			return ReceiptRegistration{}, result
		}
		result.add("receipt", err.Error())
		return ReceiptRegistration{}, result
	}
	return ReceiptRegistration{
		DispatchID:                     id,
		DispatchRegistrationArtifact:   relativePath(repo, path),
		DispatchRegistrationStatusText: "open",
	}, result
}

func hasLifecycleEventForDispatch(repo, runDir, dispatchID, dispatchArtifact string) bool {
	return hasReceiptEventForDispatch(repo, runDir, dispatchID, dispatchArtifact, "")
}

func hasReceiptEventForDispatch(repo, runDir, dispatchID, dispatchArtifact, normalizedEvent string) bool {
	dir := receiptProofDir(repo, runDir, "events")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		event, ok := decodeReceiptEvent(filepath.Join(dir, entry.Name()))
		if ok && event.DispatchID == dispatchID && event.DispatchRegistrationArtifact == dispatchArtifact && (normalizedEvent == "" || event.NormalizedEvent == normalizedEvent) {
			return true
		}
	}
	return false
}

func enforceReviewCapacity(repo, runDir, workflowID, gate, stage string, authorizedExtra bool) error {
	completed, open, err := reviewReservationCounts(repo, runDir, workflowID, gate, stage)
	if err != nil {
		return err
	}
	if authorizedExtra || completed+open < 3 {
		return nil
	}
	scope := gate
	if normalized := normalizeStage(stage); normalized != "" {
		scope += " / " + normalized
	}
	if completed >= 3 {
		return fmt.Errorf("review limit reached: %s already has %d finalized reviews in this workflow; another review requires explicit user authorization", scope, completed)
	}
	return fmt.Errorf("review capacity reserved: %s already has %d finalized reviews and %d open reservation(s) in this workflow; complete or reuse an open reservation before starting another review", scope, completed, open)
}

func reviewReservationCounts(repo, runDir, workflowID, gate, stage string) (finalized, open int, err error) {
	dir := receiptProofDir(repo, runDir, "dispatch")
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("cannot read dispatch registrations: %w", err)
	}
	wantStage := normalizeStage(stage)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		dispatch, ok := decodeDispatch(filepath.Join(dir, entry.Name()))
		if !ok || dispatch.WorkflowID != workflowID || dispatch.Gate != gate || normalizeStage(dispatch.Stage) != wantStage {
			continue
		}
		if dispatch.Status == "finalized" && strings.TrimSpace(dispatch.ReceiptArtifact) != "" {
			finalized++
		} else if dispatch.Status == "open" && strings.TrimSpace(dispatch.ReceiptArtifact) == "" && !hasReceiptEventForDispatch(repo, runDir, dispatch.DispatchID, relativePath(repo, filepath.Join(dir, entry.Name())), "subagent_stop") {
			open++
		}
	}
	return finalized, open, nil
}

func ReceiptCapture(options ReceiptCaptureOptions) (ReceiptCaptureEvent, Result) {
	var result Result
	repo := cleanWorktree(options.Worktree)
	payload, payloadText := decodePayload(options.Payload)
	provider := firstNonEmpty(options.Provider, payloadScalar(payload, []string{"provider", "receiptProvider", "hostProvider"}, 0))
	if !knownReceiptProvider(provider) {
		result.add("receipt", "unsupported provider: "+provider)
		return ReceiptCaptureEvent{}, result
	}
	eventName := firstNonEmpty(options.Event, payloadScalar(payload, []string{"eventName", "event", "hookEvent", "type", "lifecycleEvent", "hook_event_name"}, 0))
	normalized, err := normalizeReceiptEvent(provider, eventName)
	if err != nil {
		result.add("receipt", err.Error())
		return ReceiptCaptureEvent{}, result
	}

	dispatchID := payloadScalar(payload, []string{"dispatchId", "dispatch_id"}, 0)
	dispatchArtifact := payloadScalar(payload, []string{"dispatchRegistrationArtifact", "dispatch_registration_artifact", "dispatchPath", "dispatchRegistrationPath"}, 0)
	workflowID := payloadScalar(payload, []string{"workflowId", "formalWorkflowId", "workflow_id"}, 0)
	runDir, err := receiptCaptureRunDir(repo, options.RunDir, workflowID, provider, dispatchID, dispatchArtifact)
	if err != nil {
		result.add("run-dir", err.Error())
		return ReceiptCaptureEvent{}, result
	}
	dispatch, dispatchRel, err := readDispatchRegistration(repo, runDir, provider, dispatchID, dispatchArtifact)
	if err != nil {
		result.add("receipt", err.Error())
		return ReceiptCaptureEvent{}, result
	}
	changeSnapshot := payloadScalar(payload, []string{"changeSnapshot", "change_snapshot"}, 0)
	gate := payloadScalar(payload, []string{"gate", "gateId", "gate_id"}, 0)
	stage := payloadScalar(payload, []string{"stage", "gateStage", "stageName"}, 0)
	if dispatch != nil {
		if workflowID != "" && workflowID != dispatch.WorkflowID {
			result.add("receipt", "lifecycle workflowId does not match dispatch registration")
			return ReceiptCaptureEvent{}, result
		}
		if changeSnapshot != "" && changeSnapshot != dispatch.ChangeSnapshot {
			result.add("receipt", "lifecycle changeSnapshot does not match dispatch registration")
			return ReceiptCaptureEvent{}, result
		}
		if gate != "" && gate != dispatch.Gate {
			result.add("receipt", "lifecycle gate does not match dispatch registration")
			return ReceiptCaptureEvent{}, result
		}
		if stage != "" && normalizeStage(stage) != normalizeStage(dispatch.Stage) {
			result.add("receipt", "lifecycle stage does not match dispatch registration")
			return ReceiptCaptureEvent{}, result
		}
		workflowID = dispatch.WorkflowID
		changeSnapshot = dispatch.ChangeSnapshot
		gate = dispatch.Gate
		stage = dispatch.Stage
		dispatchID = dispatch.DispatchID
		dispatchArtifact = dispatchRel
	}
	subagentID := payloadScalar(payload, []string{"subagentId", "subagent_id", "agentId", "agent_id", "taskId", "task_id"}, 0)
	status := payloadScalar(payload, []string{"status", "result", "outcome", "stopStatus", "stop_status", "reason"}, 0)
	missing := missingReceiptFields(map[string]string{
		"workflowId": workflowID,
		"gate":       gate,
		"subagentId": subagentID,
		"dispatchId or dispatchRegistrationArtifact": firstNonEmpty(dispatchID, dispatchArtifact),
	})
	if len(missing) > 0 {
		result.add("receipt", "UNPROVEN lifecycle event missing correlation field(s): "+strings.Join(missing, ", "))
		return ReceiptCaptureEvent{}, result
	}

	id := newReceiptID()
	eventPath := filepath.Join(receiptProofDir(repo, runDir, "events"), id+".json")
	record := receiptEventRecord{
		Provider:                     provider,
		WorkflowID:                   workflowID,
		ChangeSnapshot:               changeSnapshot,
		Gate:                         gate,
		Stage:                        normalizeStage(stage),
		NormalizedEvent:              normalized,
		RawEventName:                 eventName,
		SubagentID:                   subagentID,
		Status:                       status,
		DispatchID:                   dispatchID,
		DispatchRegistrationArtifact: dispatchArtifact,
		CapturedAtUTC:                time.Now().UTC().Format(time.RFC3339Nano),
	}
	if payload != nil {
		record.RawPayload = payload
	} else if strings.TrimSpace(payloadText) != "" {
		record.RawPayloadText = payloadText
	}
	if err := writeJSON(eventPath, record); err != nil {
		result.add("receipt", err.Error())
		return ReceiptCaptureEvent{}, result
	}
	return ReceiptCaptureEvent{
		EventArtifact:   relativePath(repo, eventPath),
		EventSha256:     sha256File(eventPath),
		NormalizedEvent: normalized,
		Status:          "captured",
	}, result
}

func ReceiptFinalize(options ReceiptFinalizeOptions) (ReceiptFinalizeOutput, Result) {
	var result Result
	repo := cleanWorktree(options.Worktree)
	if !knownReceiptProvider(options.Provider) {
		result.add("receipt", "unsupported provider: "+options.Provider)
		return ReceiptFinalizeOutput{}, result
	}
	runDir, err := resolveWorkflowRunDir(repo, options.WorkflowID, options.RunDir)
	if err != nil {
		result.add("run-dir", err.Error())
		return ReceiptFinalizeOutput{}, result
	}
	artifactPath, err := runLocalReviewArtifactPath(repo, runDir, options.Artifact)
	if err != nil {
		result.add(options.Artifact, err.Error())
		return ReceiptFinalizeOutput{}, result
	}
	if !isFile(artifactPath) {
		result.add(options.Artifact, "review artifact does not exist")
		return ReceiptFinalizeOutput{}, result
	}
	stage := normalizeStage(options.Stage)
	dispatchPath, dispatch, ok := findOpenDispatch(repo, runDir, options.Provider, options.WorkflowID, options.Gate, stage, artifactPath)
	if !ok {
		result.add("receipt", "UNPROVEN receipt finalization requires exactly one matching open dispatch registration")
		return ReceiptFinalizeOutput{}, result
	}
	startPath, startEvent, stopPath, stopEvent, ok := findLifecyclePair(repo, runDir, dispatch.DispatchID, relativePath(repo, dispatchPath), options.Provider, options.WorkflowID, options.Gate, stage)
	if !ok {
		result.add("receipt", "UNPROVEN receipt finalization requires exactly one matching subagent_start and one matching subagent_stop lifecycle event")
		return ReceiptFinalizeOutput{}, result
	}
	if startEvent.SubagentID != "" && stopEvent.SubagentID != "" && startEvent.SubagentID != stopEvent.SubagentID {
		result.add("receipt", "UNPROVEN receipt finalization blocked: start/stop subagent ids mismatch")
		return ReceiptFinalizeOutput{}, result
	}
	startAt, startErr := time.Parse(time.RFC3339Nano, startEvent.CapturedAtUTC)
	stopAt, stopErr := time.Parse(time.RFC3339Nano, stopEvent.CapturedAtUTC)
	if startErr != nil || stopErr != nil || !stopAt.After(startAt) {
		result.add("receipt", "UNPROVEN receipt finalization blocked: subagent_stop must be captured strictly after subagent_start")
		return ReceiptFinalizeOutput{}, result
	}
	if reviewJudgmentLifecycle(dispatch.Gate, dispatch.Stage) {
		if !validateFinalSendPrompt(repo, runDir, dispatch.PromptArtifact, dispatch.PromptSha256, dispatch.Gate, dispatch.Stage, &result, "receipt") {
			return ReceiptFinalizeOutput{}, result
		}
	}
	release, err := acquireReceiptRegistrationLock(repo, runDir)
	if err != nil {
		result.add("receipt", err.Error())
		return ReceiptFinalizeOutput{}, result
	}
	defer release()
	completed, _, err := reviewReservationCounts(repo, runDir, dispatch.WorkflowID, dispatch.Gate, dispatch.Stage)
	if err != nil {
		result.add("receipt", err.Error())
		return ReceiptFinalizeOutput{}, result
	}
	if completed >= 3 && !dispatch.ExtraReviewAuthorized {
		result.add("receipt", "review limit reached before finalization; another completed review requires explicit user authorization")
		return ReceiptFinalizeOutput{}, result
	}
	artifactBytes, err := os.ReadFile(artifactPath)
	if err != nil {
		result.add(options.Artifact, err.Error())
		return ReceiptFinalizeOutput{}, result
	}
	if !qaDesignLifecycle(dispatch.Gate, dispatch.Stage) {
		var envelope FormalGateEvidence
		if err := strictJSON(artifactBytes, &envelope); err != nil {
			result.add(options.Artifact, "completed review artifact does not match dispatch binding")
			return ReceiptFinalizeOutput{}, result
		}
		envelope.WorkflowID = dispatch.WorkflowID
		envelope.ChangeSnapshot = dispatch.ChangeSnapshot
		envelope.Gate = dispatch.Gate
		envelope.Stage = normalizeStage(dispatch.Stage)
		envelope.SchemaVersion = 2
		if role, ok := reviewArtifactRole(dispatch.Gate, dispatch.Stage); ok {
			envelope.ArtifactRole = role
		}
		if dispatch.ContextBundle != "" && dispatch.ContextSHA256 != "" {
			contextRef := EvidenceRef{Path: dispatch.ContextBundle, SHA256: dispatch.ContextSHA256}
			switch envelope.ArtifactRole {
			case "CARRY_ARBITER":
				var payload CarryPayload
				if err := strictContractJSON(envelope.Payload, &payload); err != nil {
					result.add(options.Artifact, "completed review artifact does not match dispatch binding")
					return ReceiptFinalizeOutput{}, result
				}
				payload.ContextBundle = contextRef
				envelope.Payload, err = json.Marshal(payload)
			default:
				var payload ReviewerPayload
				if err := strictContractJSON(envelope.Payload, &payload); err != nil {
					result.add(options.Artifact, "completed review artifact does not match dispatch binding")
					return ReceiptFinalizeOutput{}, result
				}
				payload.ContextBundle = contextRef
				envelope.Payload, err = json.Marshal(payload)
			}
			if err != nil {
				result.add(options.Artifact, err.Error())
				return ReceiptFinalizeOutput{}, result
			}
		}
		artifactBytes, err = json.MarshalIndent(envelope, "", "  ")
		if err != nil {
			result.add(options.Artifact, err.Error())
			return ReceiptFinalizeOutput{}, result
		}
		artifactBytes = append(artifactBytes, '\n')
	}
	if reviewJudgmentLifecycle(dispatch.Gate, dispatch.Stage) {
		artifactOptions := ArtifactOptions{Root: repo, RunDir: runDir, File: relativePath(repo, artifactPath), Gate: dispatch.Gate, Stage: dispatch.Stage, WorkflowID: dispatch.WorkflowID, ChangeSnapshot: dispatch.ChangeSnapshot}
		decodeArtifact(artifactOptions, artifactBytes, &result)
		if !result.OK() {
			return ReceiptFinalizeOutput{}, result
		}
	}
	if !qaDesignLifecycle(dispatch.Gate, dispatch.Stage) {
		if err := writeFileAtomic(artifactPath, artifactBytes, 0o600); err != nil {
			result.add(options.Artifact, err.Error())
			return ReceiptFinalizeOutput{}, result
		}
	}
	receiptPath := filepath.Join(receiptProofDir(repo, runDir, ""), newReceiptID()+".json")
	dispatchRel := relativePath(repo, dispatchPath)
	receiptRel := relativePath(repo, receiptPath)
	finalizedDispatch := dispatch
	finalizedDispatch.ReceiptArtifact = receiptRel
	finalizedDispatch.Status = "finalized"
	dispatchBytes, err := json.MarshalIndent(finalizedDispatch, "", "  ")
	if err != nil {
		result.add("receipt", err.Error())
		return ReceiptFinalizeOutput{}, result
	}
	dispatchBytes = append(dispatchBytes, '\n')
	receipt := reviewerProofReceipt{
		ProofVersion:                 1,
		Provider:                     options.Provider,
		WorkflowID:                   options.WorkflowID,
		ChangeSnapshot:               dispatch.ChangeSnapshot,
		Gate:                         options.Gate,
		Stage:                        stage,
		DispatchID:                   dispatch.DispatchID,
		DispatchRegistrationArtifact: dispatchRel,
		DispatchRegistrationSha256:   sha256Bytes(dispatchBytes),
		SubagentID:                   startEvent.SubagentID,
		NormalizedEvents:             []string{"subagent_start", "subagent_stop"},
		StartEventArtifact:           relativePath(repo, startPath),
		StartEventSha256:             sha256File(startPath),
		StopEventArtifact:            relativePath(repo, stopPath),
		StopEventSha256:              sha256File(stopPath),
		ReviewArtifact:               relativePath(repo, artifactPath),
		ReviewArtifactSha256:         sha256Bytes(artifactBytes),
		PromptArtifact:               dispatch.PromptArtifact,
		PromptSha256:                 dispatch.PromptSha256,
	}
	if err := writeJSON(receiptPath, receipt); err != nil {
		result.add("receipt", err.Error())
		return ReceiptFinalizeOutput{}, result
	}
	if err := writeFileAtomic(dispatchPath, dispatchBytes, 0o600); err != nil {
		result.add("receipt", err.Error())
		return ReceiptFinalizeOutput{}, result
	}
	return ReceiptFinalizeOutput{
		ReceiptArtifact: receiptRel,
		ReceiptSha256:   sha256File(receiptPath),
	}, result
}

func reviewArtifactRole(gate, stage string) (string, bool) {
	role := ""
	for _, policy := range Policy().ArtifactPolicies {
		if policy.Gate != gate || normalizeStage(policy.Stage) != normalizeStage(stage) || !policy.ReceiptRequired {
			continue
		}
		if role != "" && role != policy.ArtifactRole {
			return "", false
		}
		role = policy.ArtifactRole
	}
	return role, role != ""
}

func ReceiptValidate(options ReceiptValidateOptions) Result {
	root := cleanWorktree(options.Worktree)
	var result Result
	if strings.TrimSpace(options.Receipt) == "" {
		result.add("receipt", "--receipt is required")
		return result
	}
	if strings.TrimSpace(options.Artifact) == "" {
		result.add("receipt", "--artifact is required")
		return result
	}
	artifactPath := resolvePath(root, options.Artifact)
	artifactData, err := os.ReadFile(artifactPath)
	if err != nil {
		result.add(options.Artifact, "cannot read artifact: "+err.Error())
		return result
	}
	receiptPath := resolvePath(root, options.Receipt)
	if !isFile(receiptPath) {
		result.add(options.Receipt, "receipt path does not exist")
		return result
	}
	if !qaDesignLifecycle(options.Gate, options.Stage) {
		var envelope FormalGateEvidence
		if err := strictJSON(artifactData, &envelope); err != nil {
			result.add(options.Artifact, err.Error())
			return result
		}
		if options.WorkflowID != "" && envelope.WorkflowID != options.WorkflowID {
			result.add(options.Artifact, "workflowId does not match --workflow-id")
		}
		if options.ChangeSnapshot != "" && envelope.ChangeSnapshot != options.ChangeSnapshot {
			result.add(options.Artifact, "changeSnapshot does not match --change-snapshot")
		}
		if envelope.Gate != options.Gate || normalizeStage(envelope.Stage) != normalizeStage(options.Stage) {
			result.add(options.Artifact, "gate or stage does not match")
		}
	}
	activeRun := artifactRunDir(ArtifactOptions{Root: root, File: options.Artifact}, options.WorkflowID)
	logical, logicalErr := logicalPathInRun(activeRun, receiptPath)
	if logicalErr != nil {
		result.add(options.Artifact, logicalErr.Error())
		return result
	}
	ref := EvidenceRef{Path: logical, SHA256: sha256File(receiptPath)}
	validateReceipt(ArtifactOptions{Root: root, File: options.Artifact, Gate: options.Gate, WorkflowID: options.WorkflowID, ChangeSnapshot: options.ChangeSnapshot, Stage: options.Stage}, activeRun, ref, &result)
	return result
}

func qaDesignLifecycle(gate, stage string) bool {
	return gate == "qa-test-gate" && normalizeStage(stage) == "Design"
}

func reviewJudgmentLifecycle(gate, stage string) bool {
	switch gate {
	case "complexity-gate", "architecture-health-gate", "code-quality-gate":
		return true
	case "qa-test-gate":
		stage = normalizeStage(stage)
		return stage == "Design Review" || stage == "Carry"
	default:
		return false
	}
}

func expectedDispatchRole(gate, stage string) string {
	if gate == "qa-test-gate" && normalizeStage(stage) == "Carry" {
		return "carry-forward-arbiter"
	}
	return gate
}

func validateFinalSendPrompt(root, runDir, artifact, expectedHash, expectedGate, expectedStage string, result *Result, where string) bool {
	if strings.TrimSpace(artifact) == "" || !isSHA256(expectedHash) {
		result.add(where, "review judgment receipt must bind the exact final-send prompt path and hash")
		return false
	}
	if activeWorkflowRun(root, runDir) && !restrictedRepoPath(root, runDir, artifact) {
		result.add(where, "final-send prompt must be under the active run restricted directory")
		return false
	}
	path := resolvePath(root, artifact)
	data, err := os.ReadFile(path)
	if err != nil || sha256Bytes(data) != expectedHash {
		result.add(where, "final-send prompt path or hash is invalid")
		return false
	}
	promptResult, _ := DispatchPromptWithViolations(DispatchPromptOptions{Root: root, PromptText: string(data), FinalSend: true})
	if !promptResult.OK() {
		for _, failure := range promptResult.Failures {
			result.add(where, "final-send prompt validation failed: "+failure.Message)
		}
		return false
	}
	if strictDispatchPromptFields(string(data))["formal_gate_dispatch"] != expectedDispatchRole(expectedGate, expectedStage) {
		result.add(where, "final-send prompt formal_gate_dispatch does not match the registered gate and stage role")
		return false
	}
	return true
}

func validateDispatchRegistrationContract(root, runDir, promptArtifact, promptHash string, options ReceiptRegisterOptions, bundlePath string, result *Result) string {
	path := resolvePath(root, promptArtifact)
	data, err := os.ReadFile(path)
	if err != nil || sha256Bytes(data) != promptHash {
		result.add("receipt", "cannot validate the exact final-send prompt contract")
		return ""
	}
	prompt := string(data)
	fields := strictDispatchPromptFields(prompt)
	if !samePath(resolvePath(root, fields["worktree"]), cleanWorktree(options.Worktree)) {
		result.add("receipt", "final-send prompt Worktree does not match --worktree")
	}
	if fields["base commit or snapshot"] != strings.TrimSpace(options.ChangeSnapshot) {
		result.add("receipt", "final-send prompt Base commit or snapshot does not match --change-snapshot")
	}
	if !samePath(resolvePath(root, fields["output path"]), resolvePath(root, options.Artifact)) {
		result.add("receipt", "final-send prompt Output path does not match --artifact")
	}

	format := fields["output format"]
	role, policies := dispatchOutputContracts(options.Gate, options.Stage)
	if role == "" || !strings.Contains(format, "schema-version-2") || !strings.Contains(format, role) {
		result.add("receipt", "final-send prompt Output format does not match the registered gate and stage schema")
	}
	policyID := ""
	for _, candidate := range policies {
		if strings.Contains(format, candidate) {
			if policyID != "" {
				result.add("receipt", "final-send prompt Output format names multiple review policies")
				return ""
			}
			policyID = candidate
		}
	}
	if policyID == "" {
		result.add("receipt", "final-send prompt Output format does not name the registered review policy")
	}

	bindingPattern := regexp.MustCompile(`contextBundle=([^\s,;]+) sha256=([a-f0-9]{64})`)
	bindings := bindingPattern.FindAllStringSubmatch(format, -1)
	wantPath := slash(relativePath(runDir, bundlePath))
	wantHash := sha256File(bundlePath)
	if len(bindings) != 1 || bindings[0][1] != wantPath || bindings[0][2] != wantHash {
		result.add("receipt", "final-send prompt contextBundle binding does not match --context-bundle")
	}
	return policyID
}

func dispatchOutputContracts(gate, stage string) (string, []string) {
	if gate == "qa-test-gate" && normalizeStage(stage) == "Carry" {
		return "CARRY_ARBITER", []string{"carry.arbiter.v2"}
	}
	role := ""
	policies := []string{}
	for _, policy := range Policy().ArtifactPolicies {
		if policy.Gate != gate || normalizeStage(policy.Stage) != normalizeStage(stage) || !policy.ReceiptRequired {
			continue
		}
		if role == "" {
			role = policy.ArtifactRole
		}
		if role != policy.ArtifactRole {
			return "", nil
		}
		policies = append(policies, policy.ID)
	}
	return role, policies
}

func validateDispatchContextForPolicy(root, runDir, policyID string, refs []EvidenceRef, result *Result) {
	if policyID != "complexity.post-development.v2" {
		return
	}
	options := ArtifactOptions{Root: root}
	for _, ref := range refs {
		data, ok := readReviewerEvidenceRef(options, runDir, ref, result)
		if !ok {
			continue
		}
		var value any
		if json.Unmarshal(data, &value) == nil && containsDevelopmentBudgetMaterial(value) {
			result.add(ref.Path, "post-development complexity dispatch context must not include development-time budget or statistics schema fields")
		}
	}
}

func ReceiptPreflight(options ReceiptPreflightOptions) (ReceiptPreflightReport, Result) {
	var result Result
	host := strings.TrimSpace(options.Host)
	def, ok := receiptHostPreflightDefinition(host)
	if !ok {
		result.add("receipt", "unsupported host: "+host)
		return ReceiptPreflightReport{}, result
	}
	repo := cleanWorktree(options.Worktree)
	checkedConfigPaths := receiptCheckedConfigPaths(repo, def)
	configPath := ""
	for _, candidate := range checkedConfigPaths {
		if isFile(candidate) {
			configPath = candidate
			break
		}
	}
	configured := map[string][]string{}
	missing := []string{}
	if configPath == "" {
		missing = append(missing, def.MissingConfigMessage)
		for _, event := range def.Events {
			configured[event.OutputName] = []string{}
		}
	} else {
		config, err := readHookConfig(configPath)
		if err != nil {
			missing = append(missing, def.ConfigReadErrorPrefix+": "+err.Error())
		} else {
			for _, event := range def.Events {
				commands := hostCanaryHookCommands(config, event.ConfigEventName, def.HookShape)
				configured[event.OutputName] = commands
				if !hasReceiptCaptureCommand(commands, def.Provider, event.ReceiptEventName) {
					missing = append(missing, event.HookMissing)
				}
			}
		}
	}
	for _, event := range def.Events {
		missing = append(missing, event.PayloadMissing)
	}
	missing = append(missing,
		"host lifecycle canary evidence",
		"usable host correlation fields tying both payloads to one dispatch registration",
	)

	checked := make([]string, 0, len(checkedConfigPaths))
	for _, path := range checkedConfigPaths {
		checked = append(checked, slash(path))
	}
	return ReceiptPreflightReport{
		Status:                   "HOST_AUTO_CAPTURE_UNPROVEN",
		Host:                     def.DisplayName,
		Provider:                 def.Provider,
		Worktree:                 slash(repo),
		ConfigPath:               slash(configPath),
		CheckedConfigPaths:       checked,
		RequiredLifecycleEvents:  def.RequiredEvents(),
		ConfiguredLifecycleHooks: configured,
		UsableCorrelationFields:  []string{},
		RawPayloadArtifacts:      []string{},
		Missing:                  missing,
	}, result
}

type receiptHostPreflight struct {
	DisplayName           string
	Provider              string
	ProjectConfigRelative string
	GlobalConfigRelative  string
	MissingConfigMessage  string
	ConfigReadErrorPrefix string
	HookShape             string
	Events                []receiptHostEvent
}

type receiptHostEvent struct {
	ConfigEventName  string
	ReceiptEventName string
	OutputName       string
	HookMissing      string
	PayloadMissing   string
}

func (def receiptHostPreflight) RequiredEvents() []string {
	events := make([]string, 0, len(def.Events))
	for _, event := range def.Events {
		events = append(events, event.OutputName)
	}
	return events
}

func receiptHostPreflightDefinition(host string) (receiptHostPreflight, bool) {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "claude", "claude-code", "claude code":
		return receiptHostPreflight{
			DisplayName:           "Claude Code",
			Provider:              "claude-code",
			ProjectConfigRelative: filepath.FromSlash(".claude/settings.json"),
			GlobalConfigRelative:  filepath.FromSlash(".claude/settings.json"),
			MissingConfigMessage:  "Claude Code settings.json with SubagentStart/SubagentStop receipt hooks",
			ConfigReadErrorPrefix: "readable Claude Code hook config JSON",
			HookShape:             "nested",
			Events: []receiptHostEvent{
				{
					ConfigEventName:  "SubagentStart",
					ReceiptEventName: "SubagentStart",
					OutputName:       "SubagentStart",
					HookMissing:      "Claude Code SubagentStart receipt capture hook",
					PayloadMissing:   "real Claude Code host-emitted SubagentStart payload artifact",
				},
				{
					ConfigEventName:  "SubagentStop",
					ReceiptEventName: "SubagentStop",
					OutputName:       "SubagentStop",
					HookMissing:      "Claude Code SubagentStop receipt capture hook",
					PayloadMissing:   "real Claude Code host-emitted SubagentStop payload artifact",
				},
			},
		}, true
	case "codex":
		return receiptHostPreflight{
			DisplayName:           "Codex",
			Provider:              "codex",
			ProjectConfigRelative: filepath.FromSlash(".codex/hooks.json"),
			GlobalConfigRelative:  filepath.FromSlash(".codex/hooks.json"),
			MissingConfigMessage:  "Codex hooks.json with SubagentStart/SubagentStop receipt hooks",
			ConfigReadErrorPrefix: "readable Codex hook config JSON",
			HookShape:             "nested",
			Events: []receiptHostEvent{
				{
					ConfigEventName:  "SubagentStart",
					ReceiptEventName: "SubagentStart",
					OutputName:       "SubagentStart",
					HookMissing:      "Codex SubagentStart receipt capture hook",
					PayloadMissing:   "real Codex host-emitted SubagentStart payload artifact",
				},
				{
					ConfigEventName:  "SubagentStop",
					ReceiptEventName: "SubagentStop",
					OutputName:       "SubagentStop",
					HookMissing:      "Codex SubagentStop receipt capture hook",
					PayloadMissing:   "real Codex host-emitted SubagentStop payload artifact",
				},
			},
		}, true
	case "cursor":
		return receiptHostPreflight{
			DisplayName:           "Cursor",
			Provider:              "cursor",
			ProjectConfigRelative: filepath.FromSlash(".cursor/hooks.json"),
			GlobalConfigRelative:  filepath.FromSlash(".cursor/hooks.json"),
			MissingConfigMessage:  "Cursor hooks.json with subagentStart/subagentStop receipt hooks",
			ConfigReadErrorPrefix: "readable Cursor hook config JSON",
			HookShape:             "flat",
			Events: []receiptHostEvent{
				{
					ConfigEventName:  "subagentStart",
					ReceiptEventName: "SubagentStart",
					OutputName:       "subagentStart",
					HookMissing:      "Cursor subagentStart receipt capture hook",
					PayloadMissing:   "real Cursor host-emitted subagentStart payload artifact",
				},
				{
					ConfigEventName:  "subagentStop",
					ReceiptEventName: "SubagentStop",
					OutputName:       "subagentStop",
					HookMissing:      "Cursor subagentStop receipt capture hook",
					PayloadMissing:   "real Cursor host-emitted subagentStop payload artifact",
				},
			},
		}, true
	default:
		return receiptHostPreflight{}, false
	}
}

func receiptCheckedConfigPaths(repo string, def receiptHostPreflight) []string {
	paths := []string{filepath.Join(repo, def.ProjectConfigRelative)}
	if home, err := installHomeDir(); err == nil {
		paths = append(paths, filepath.Join(home, def.GlobalConfigRelative))
	}
	return paths
}

func hostCanaryHookCommands(config map[string]any, eventName, shape string) []string {
	var commands []string
	hooksRoot, _ := config["hooks"].(map[string]any)
	entries, _ := hooksRoot[eventName].([]any)
	for _, entry := range entries {
		entryMap, _ := entry.(map[string]any)
		if shape == "nested" {
			nested, _ := entryMap["hooks"].([]any)
			for _, hook := range nested {
				hookMap, _ := hook.(map[string]any)
				if command, _ := hookMap["command"].(string); strings.TrimSpace(command) != "" {
					commands = append(commands, command)
				}
			}
			continue
		}
		if command, _ := entryMap["command"].(string); strings.TrimSpace(command) != "" {
			commands = append(commands, command)
		}
	}
	return commands
}

func hasReceiptCaptureCommand(commands []string, provider, event string) bool {
	for _, command := range commands {
		lower := strings.ToLower(command)
		if containsScriptRuntimeMarker(lower) {
			continue
		}
		if strings.Contains(lower, "formal-gates") &&
			strings.Contains(lower, "receipt") &&
			strings.Contains(lower, "capture") &&
			strings.Contains(lower, strings.ToLower(provider)) &&
			strings.Contains(lower, strings.ToLower(event)) {
			return true
		}
	}
	return false
}

func containsScriptRuntimeMarker(lower string) bool {
	for _, marker := range []string{".ps1", "powershell", "pwsh", "python", "node", "bash", ".bat", ".cmd", ".js"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func normalizeReceiptEvent(provider, event string) (string, error) {
	if !knownReceiptProvider(provider) {
		return "", fmt.Errorf("unsupported provider: %s", provider)
	}
	switch event {
	case "SubagentStart", "subagentStart":
		return "subagent_start", nil
	case "SubagentStop", "subagentStop":
		return "subagent_stop", nil
	default:
		return "", fmt.Errorf("unsupported %s lifecycle event: %s", provider, event)
	}
}

func providerForHost(host string) string {
	switch strings.ToLower(strings.TrimSpace(host)) {
	case "claude", "claude-code", "claude code":
		return "claude-code"
	case "codex":
		return "codex"
	case "cursor":
		return "cursor"
	default:
		return ""
	}
}

func cleanWorktree(worktree string) string {
	root := cleanRoot(worktree)
	abs, err := filepath.Abs(root)
	if err != nil {
		return filepath.Clean(root)
	}
	return filepath.Clean(abs)
}

func decodePayload(data []byte) (any, string) {
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil, ""
	}
	var payload any
	if err := json.Unmarshal([]byte(text), &payload); err != nil {
		return nil, text
	}
	return payload, text
}

func payloadScalar(value any, names []string, depth int) string {
	if value == nil || depth > 3 {
		return ""
	}
	if m, ok := value.(map[string]any); ok {
		for _, name := range names {
			for key, raw := range m {
				if strings.EqualFold(key, name) {
					if scalar := scalarString(raw); scalar != "" {
						return scalar
					}
				}
			}
		}
		for _, container := range []string{"payload", "event", "data", "hook", "tool_input", "toolInput", "input"} {
			for key, raw := range m {
				if strings.EqualFold(key, container) {
					if scalar := payloadScalar(raw, names, depth+1); scalar != "" {
						return scalar
					}
				}
			}
		}
	}
	return ""
}

func scalarString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64, bool:
		return strings.TrimSpace(fmt.Sprint(v))
	default:
		rv := reflect.ValueOf(value)
		if rv.IsValid() && rv.Kind() >= reflect.Int && rv.Kind() <= reflect.Uint64 {
			return strings.TrimSpace(fmt.Sprint(value))
		}
	}
	return ""
}

func readDispatchRegistration(repo, runDir, provider, dispatchID, artifact string) (*dispatchRegistration, string, error) {
	dispatchID = strings.TrimSpace(dispatchID)
	artifact = strings.TrimSpace(artifact)
	if strings.TrimSpace(artifact) != "" {
		path := absPath(resolvePath(repo, artifact))
		dispatchDir := absPath(receiptProofDir(repo, runDir, "dispatch"))
		if samePath(path, dispatchDir) || !pathUnder(path, dispatchDir) {
			return nil, "", fmt.Errorf("UNPROVEN lifecycle dispatch registration path is outside the active dispatch proof directory")
		}
		dispatch, ok := decodeDispatch(path)
		if !ok || (provider != "" && dispatch.Provider != provider) {
			return nil, "", fmt.Errorf("UNPROVEN lifecycle dispatch registration cannot be resolved")
		}
		if dispatchID != "" && dispatch.DispatchID != dispatchID {
			return nil, "", fmt.Errorf("UNPROVEN lifecycle dispatchId conflicts with dispatchRegistrationArtifact")
		}
		return &dispatch, relativePath(repo, path), nil
	}
	if dispatchID == "" {
		return nil, "", nil
	}
	dir := receiptProofDir(repo, runDir, "dispatch")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, "", fmt.Errorf("UNPROVEN lifecycle dispatch registration cannot be resolved")
	}
	var found *dispatchRegistration
	var foundRel string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		dispatch, ok := decodeDispatch(path)
		if !ok || dispatch.DispatchID != dispatchID || (provider != "" && dispatch.Provider != provider) {
			continue
		}
		if found != nil {
			return nil, "", fmt.Errorf("UNPROVEN lifecycle dispatchId resolves to multiple registrations")
		}
		copy := dispatch
		found = &copy
		foundRel = relativePath(repo, path)
	}
	if found == nil {
		return nil, "", fmt.Errorf("UNPROVEN lifecycle dispatch registration cannot be resolved")
	}
	return found, foundRel, nil
}

func decodeDispatch(path string) (dispatchRegistration, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return dispatchRegistration{}, false
	}
	var dispatch dispatchRegistration
	if err := strictContractJSON(data, &dispatch); err != nil {
		return dispatchRegistration{}, false
	}
	return dispatch, true
}

func findOpenDispatch(repo, runDir, provider, workflowID, gate, stage, artifactPath string) (string, dispatchRegistration, bool) {
	dir := receiptProofDir(repo, runDir, "dispatch")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", dispatchRegistration{}, false
	}
	var path string
	var found dispatchRegistration
	count := 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		candidate := filepath.Join(dir, entry.Name())
		dispatch, ok := decodeDispatch(candidate)
		if !ok {
			continue
		}
		if dispatch.Provider == provider &&
			dispatch.WorkflowID == workflowID &&
			dispatch.Gate == gate &&
			normalizeStage(dispatch.Stage) == normalizeStage(stage) &&
			dispatch.Status == "open" &&
			strings.TrimSpace(dispatch.ReceiptArtifact) == "" &&
			samePath(resolvePath(repo, dispatch.ReviewArtifact), artifactPath) {
			count++
			path = candidate
			found = dispatch
		}
	}
	return path, found, count == 1
}

func findLifecyclePair(repo, runDir, dispatchID, dispatchRel, provider, workflowID, gate, stage string) (string, receiptEventRecord, string, receiptEventRecord, bool) {
	dir := receiptProofDir(repo, runDir, "events")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", receiptEventRecord{}, "", receiptEventRecord{}, false
	}
	var startPath, stopPath string
	var start, stop receiptEventRecord
	startCount, stopCount := 0, 0
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		event, ok := decodeReceiptEvent(path)
		if !ok || event.Provider != provider || event.WorkflowID != workflowID || event.Gate != gate || normalizeStage(event.Stage) != normalizeStage(stage) {
			continue
		}
		if event.DispatchID != dispatchID || event.DispatchRegistrationArtifact != dispatchRel {
			continue
		}
		switch event.NormalizedEvent {
		case "subagent_start":
			startCount++
			startPath = path
			start = event
		case "subagent_stop":
			stopCount++
			stopPath = path
			stop = event
		}
	}
	return startPath, start, stopPath, stop, startCount == 1 && stopCount == 1
}

func decodeReceiptEvent(path string) (receiptEventRecord, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return receiptEventRecord{}, false
	}
	var event receiptEventRecord
	if err := strictContractJSON(data, &event); err != nil {
		return receiptEventRecord{}, false
	}
	return event, true
}

func receiptCaptureRunDir(repo, value, workflowID, provider, dispatchID, dispatchArtifact string) (string, error) {
	explicit := ""
	if strings.TrimSpace(value) != "" {
		var err error
		explicit, err = resolveWorkflowRunDir(repo, workflowID, value)
		if err != nil {
			return "", err
		}
	}
	if strings.TrimSpace(dispatchArtifact) != "" {
		correlated, err := dispatchArtifactRunDir(repo, dispatchArtifact)
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(value) != "" && (correlated == "" || !samePath(explicit, correlated)) {
			return "", fmt.Errorf("explicit run directory conflicts with dispatch correlation")
		}
		return correlated, nil
	}
	if strings.TrimSpace(value) != "" || strings.TrimSpace(dispatchID) == "" {
		return explicit, nil
	}
	runsRoot := filepath.Join(repo, ".claude", "gates", "runs")
	entries, _ := os.ReadDir(runsRoot)
	match, count := "", 0
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(runsRoot, entry.Name())
		if _, _, err := readDispatchRegistration(repo, candidate, provider, dispatchID, ""); err == nil {
			match, count = candidate, count+1
		}
	}
	if count != 1 {
		return "", fmt.Errorf("UNPROVEN lifecycle dispatchId does not select exactly one active run")
	}
	return match, nil
}

func dispatchArtifactRunDir(repo, artifact string) (string, error) {
	path := absPath(resolvePath(repo, artifact))
	dispatchDir := filepath.Dir(path)
	proofsDir := filepath.Dir(dispatchDir)
	restrictedDir := filepath.Dir(proofsDir)
	runDir := filepath.Dir(restrictedDir)
	runsRoot := filepath.Join(repo, ".claude", "gates", "runs")
	if filepath.Base(dispatchDir) != "dispatch" || filepath.Base(proofsDir) != "proofs" || filepath.Base(restrictedDir) != "restricted" || samePath(runDir, runsRoot) || !pathUnder(runDir, runsRoot) {
		return "", fmt.Errorf("UNPROVEN lifecycle dispatch registration path is outside an active dispatch proof directory")
	}
	return runDir, nil
}

func reserveAbsentReviewArtifact(repo, runDir, logical string) (string, error) {
	path, err := runLocalReviewArtifactPath(repo, runDir, logical)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(path); err == nil {
		return "", fmt.Errorf("review artifact path already exists; stale output must be removed before registration")
	} else if !os.IsNotExist(err) {
		return "", err
	}
	return path, nil
}

func runLocalReviewArtifactPath(repo, runDir, logical string) (string, error) {
	logical = strings.TrimSpace(logical)
	if logical == "" || logical == "." || strings.Contains(logical, "\\") || filepath.IsAbs(logical) || regexp.MustCompile(`^[A-Za-z]:|^[A-Za-z][A-Za-z0-9+.-]*:`).MatchString(logical) {
		return "", fmt.Errorf("unsafe run-local review artifact path: %s", logical)
	}
	for _, part := range strings.Split(logical, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("unsafe run-local review artifact path: %s", logical)
		}
	}
	path := resolvePath(repo, logical)
	parent, err := filepath.EvalSymlinks(filepath.Dir(path))
	if err != nil {
		return "", fmt.Errorf("review artifact parent directory is not available: %w", err)
	}
	if _, err := logicalPathInRun(runDir, filepath.Join(parent, filepath.Base(path))); err != nil {
		return "", err
	}
	canonicalRun := absPath(runDir)
	if resolved, err := filepath.EvalSymlinks(canonicalRun); err == nil {
		canonicalRun = resolved
	}
	restricted := filepath.Join(canonicalRun, "restricted")
	if !samePath(parent, restricted) && !pathUnder(parent, restricted) {
		return "", fmt.Errorf("review artifact must be under the active run restricted directory")
	}
	return filepath.Join(parent, filepath.Base(path)), nil
}

func receiptProofDir(repo, runDir, leaf string) string {
	return filepath.Join(runDir, "restricted", "proofs", leaf)
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return writeFileAtomic(path, data, 0o600)
}

func acquireReceiptRegistrationLock(repo, runDir string) (func(), error) {
	dir := receiptProofDir(repo, runDir, "dispatch")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, ".register.lock")
	deadline := time.Now().Add(5 * time.Second)
	for {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			if closeErr := file.Close(); closeErr != nil {
				_ = os.Remove(path)
				return nil, closeErr
			}
			return func() { _ = os.Remove(path) }, nil
		}
		if !os.IsExist(err) {
			return nil, err
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > 30*time.Second {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for another receipt registration to finish")
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func writeJSONExclusive(path string, value any) (err error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	defer func() {
		_ = file.Close()
		if err != nil {
			_ = os.Remove(path)
		}
	}()
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err = encoder.Encode(value); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	return file.Close()
}

func sha256Bytes(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func matchingReceiptRef(options ArtifactOptions, artifact decodedArtifact) (EvidenceRef, error) {
	repo := cleanWorktree(options.Root)
	dir := receiptProofDir(repo, artifact.RunDir, "dispatch")
	reviewPath := filepath.Clean(resolvePath(options.Root, options.File))
	var receiptPath string
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		dispatch, ok := decodeDispatch(filepath.Join(dir, entry.Name()))
		if !ok || dispatch.ReceiptArtifact == "" || dispatch.WorkflowID != artifact.Envelope.WorkflowID || dispatch.ChangeSnapshot != artifact.Envelope.ChangeSnapshot || dispatch.Gate != artifact.Envelope.Gate || normalizeStage(dispatch.Stage) != normalizeStage(artifact.Envelope.Stage) || !samePath(resolvePath(options.Root, dispatch.ReviewArtifact), reviewPath) {
			continue
		}
		if receiptPath != "" {
			return EvidenceRef{}, fmt.Errorf("multiple finalized receipts match review artifact")
		}
		receiptPath = resolvePath(options.Root, dispatch.ReceiptArtifact)
	}
	if receiptPath == "" {
		return EvidenceRef{}, fmt.Errorf("matching finalized receipt is missing")
	}
	logical, err := logicalPathInRun(artifact.RunDir, receiptPath)
	if err != nil {
		return EvidenceRef{}, err
	}
	ref := EvidenceRef{Path: logical, SHA256: sha256File(receiptPath)}
	var result Result
	validateReceipt(options, artifact.RunDir, ref, &result)
	if !result.OK() {
		return EvidenceRef{}, fmt.Errorf("receipt validation failed: %s", result.Failures[0].Message)
	}
	return ref, nil
}

func finalizedReceiptExists(options ArtifactOptions, artifact decodedArtifact) bool {
	repo := cleanWorktree(options.Root)
	dir := receiptProofDir(repo, artifact.RunDir, "dispatch")
	reviewPath := filepath.Clean(resolvePath(options.Root, options.File))
	entries, _ := os.ReadDir(dir)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		dispatch, ok := decodeDispatch(filepath.Join(dir, entry.Name()))
		if ok && dispatch.Status == "finalized" && dispatch.ReceiptArtifact != "" && dispatch.WorkflowID == artifact.Envelope.WorkflowID && dispatch.ChangeSnapshot == artifact.Envelope.ChangeSnapshot && dispatch.Gate == artifact.Envelope.Gate && normalizeStage(dispatch.Stage) == normalizeStage(artifact.Envelope.Stage) && samePath(resolvePath(options.Root, dispatch.ReviewArtifact), reviewPath) {
			return true
		}
	}
	return false
}

func validateDesignReviewIndependence(options ArtifactOptions, artifact decodedArtifact, reviewReceiptRef EvidenceRef) error {
	if artifact.Policy.ID != "qa.design-review.v2" || artifact.Reviewer == nil {
		return nil
	}
	reviewReceipt, err := readProofReceiptRef(artifact.RunDir, reviewReceiptRef)
	if err != nil {
		return err
	}
	for _, check := range artifact.Reviewer.Checks {
		if check.ID != "qa.design.case-set-binding" {
			continue
		}
		for _, ref := range check.EvidenceRefs {
			designReceipt, err := readProofReceiptRef(artifact.RunDir, ref)
			if err != nil || designReceipt.Gate != "qa-test-gate" || normalizeStage(designReceipt.Stage) != "Design" {
				continue
			}
			if designReceipt.SubagentID == reviewReceipt.SubagentID {
				return fmt.Errorf("QA Design and Design Review must use different subagents")
			}
			return nil
		}
	}
	return fmt.Errorf("QA Design receipt is missing from Design Review evidence")
}

func readProofReceiptRef(runDir string, ref EvidenceRef) (reviewerProofReceipt, error) {
	path, err := safeEvidencePath(runDir, ref.Path)
	if err != nil || sha256File(path) != ref.SHA256 {
		return reviewerProofReceipt{}, fmt.Errorf("reviewer receipt path or hash is invalid")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return reviewerProofReceipt{}, err
	}
	var receipt reviewerProofReceipt
	if err := strictContractJSON(data, &receipt); err != nil {
		return reviewerProofReceipt{}, fmt.Errorf("reviewer receipt is invalid JSON")
	}
	return receipt, nil
}

func relativePath(root, path string) string {
	root = canonicalRelativeBase(root)
	path = canonicalRelativeBase(path)
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

func canonicalRelativeBase(value string) string {
	abs, err := filepath.Abs(value)
	if err == nil {
		value = abs
	}
	if canonical, err := filepath.EvalSymlinks(value); err == nil {
		return canonical
	}
	if parent, err := filepath.EvalSymlinks(filepath.Dir(value)); err == nil {
		return filepath.Join(parent, filepath.Base(value))
	}
	return filepath.Clean(value)
}

func newReceiptID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UTC().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func missingReceiptFields(values map[string]string) []string {
	var missing []string
	for key, value := range values {
		if strings.TrimSpace(value) == "" {
			missing = append(missing, key)
		}
	}
	return missing
}
