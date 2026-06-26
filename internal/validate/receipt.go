package validate

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"time"
)

type ReceiptRegisterOptions struct {
	Worktree   string
	Provider   string
	WorkflowID string
	Gate       string
	Stage      string
	Artifact   string
}

type ReceiptRegistration struct {
	DispatchID                     string `json:"dispatchId"`
	DispatchRegistrationArtifact   string `json:"dispatchRegistrationArtifact"`
	DispatchRegistrationSha256     string `json:"dispatchRegistrationSha256"`
	DispatchRegistrationStatusText string `json:"status"`
}

type ReceiptCaptureOptions struct {
	Worktree string
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
	Provider   string
	WorkflowID string
	Gate       string
	Stage      string
	Artifact   string
}

type ReceiptFinalizeOutput struct {
	ReviewerProofReceipt string `json:"reviewerProofReceipt"`
	ReceiptArtifact      string `json:"receiptArtifact"`
	ReceiptSha256        string `json:"receiptSha256"`
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

type receiptEventRecord struct {
	Provider                     string `json:"provider"`
	WorkflowID                   string `json:"workflowId"`
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
	if strings.TrimSpace(options.WorkflowID) == "" {
		result.add("receipt", "--workflow-id is required")
	}
	if strings.TrimSpace(options.Gate) == "" {
		result.add("receipt", "--gate is required")
	}
	if strings.TrimSpace(options.Artifact) == "" {
		result.add("receipt", "--artifact is required")
	}
	if !result.OK() {
		return ReceiptRegistration{}, result
	}
	artifactPath := resolvePath(repo, options.Artifact)
	if !isFile(artifactPath) {
		result.add(options.Artifact, "review artifact does not exist")
		return ReceiptRegistration{}, result
	}
	id := newReceiptID()
	dispatchDir := filepath.Join(repo, ".claude", "gates", "proofs", "dispatch")
	path := filepath.Join(dispatchDir, id+".json")
	record := dispatchRegistration{
		ProofVersion:   1,
		DispatchID:     id,
		Provider:       options.Provider,
		WorkflowID:     options.WorkflowID,
		Gate:           options.Gate,
		Stage:          normalizeStage(options.Stage),
		ReviewArtifact: relativePath(repo, artifactPath),
	}
	data := map[string]any{
		"proofVersion":    1,
		"dispatchId":      record.DispatchID,
		"provider":        record.Provider,
		"workflowId":      record.WorkflowID,
		"gate":            record.Gate,
		"stage":           record.Stage,
		"worktree":        filepath.ToSlash(repo),
		"reviewArtifact":  record.ReviewArtifact,
		"status":          "open",
		"registeredAtUtc": time.Now().UTC().Format(time.RFC3339Nano),
	}
	if err := writeJSON(path, data); err != nil {
		result.add("receipt", err.Error())
		return ReceiptRegistration{}, result
	}
	return ReceiptRegistration{
		DispatchID:                     id,
		DispatchRegistrationArtifact:   relativePath(repo, path),
		DispatchRegistrationSha256:     sha256File(path),
		DispatchRegistrationStatusText: "open",
	}, result
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
	dispatch, dispatchRel := readDispatchRegistration(repo, provider, dispatchID, dispatchArtifact)
	workflowID := payloadScalar(payload, []string{"workflowId", "formalWorkflowId", "workflow_id"}, 0)
	gate := payloadScalar(payload, []string{"gate", "gateId", "gate_id"}, 0)
	stage := payloadScalar(payload, []string{"stage", "gateStage", "stageName"}, 0)
	if dispatch != nil {
		workflowID = firstNonEmpty(workflowID, dispatch.WorkflowID)
		gate = firstNonEmpty(gate, dispatch.Gate)
		stage = firstNonEmpty(stage, dispatch.Stage)
		dispatchID = firstNonEmpty(dispatchID, dispatch.DispatchID)
		dispatchArtifact = firstNonEmpty(dispatchArtifact, dispatchRel)
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
	eventPath := filepath.Join(repo, ".claude", "gates", "proofs", "events", id+".json")
	record := receiptEventRecord{
		Provider:                     provider,
		WorkflowID:                   workflowID,
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
	artifactPath := resolvePath(repo, options.Artifact)
	if !isFile(artifactPath) {
		result.add(options.Artifact, "review artifact does not exist")
		return ReceiptFinalizeOutput{}, result
	}
	stage := normalizeStage(options.Stage)
	dispatchPath, dispatch, ok := findOpenDispatch(repo, options.Provider, options.WorkflowID, options.Gate, stage, artifactPath)
	if !ok {
		result.add("receipt", "UNPROVEN receipt finalization requires exactly one matching open dispatch registration")
		return ReceiptFinalizeOutput{}, result
	}
	startPath, startEvent, stopPath, stopEvent, ok := findLifecyclePair(repo, dispatch.DispatchID, relativePath(repo, dispatchPath), options.Provider, options.WorkflowID, options.Gate, stage)
	if !ok {
		result.add("receipt", "UNPROVEN receipt finalization requires exactly one matching subagent_start and one matching subagent_stop lifecycle event")
		return ReceiptFinalizeOutput{}, result
	}
	if startEvent.SubagentID != "" && stopEvent.SubagentID != "" && startEvent.SubagentID != stopEvent.SubagentID {
		result.add("receipt", "UNPROVEN receipt finalization blocked: start/stop subagent ids mismatch")
		return ReceiptFinalizeOutput{}, result
	}
	artifactText, err := readText(artifactPath)
	if err != nil {
		result.add(options.Artifact, err.Error())
		return ReceiptFinalizeOutput{}, result
	}
	receiptPath := filepath.Join(repo, ".claude", "gates", "proofs", newReceiptID()+".json")
	dispatchRel := relativePath(repo, dispatchPath)
	receiptRel := relativePath(repo, receiptPath)
	dispatch.ReceiptArtifact = receiptRel
	dispatchMap := map[string]any{
		"proofVersion":    1,
		"dispatchId":      dispatch.DispatchID,
		"provider":        dispatch.Provider,
		"workflowId":      dispatch.WorkflowID,
		"gate":            dispatch.Gate,
		"stage":           dispatch.Stage,
		"reviewArtifact":  dispatch.ReviewArtifact,
		"receiptArtifact": receiptRel,
		"status":          "finalized",
	}
	if err := writeJSON(dispatchPath, dispatchMap); err != nil {
		result.add("receipt", err.Error())
		return ReceiptFinalizeOutput{}, result
	}
	receipt := map[string]any{
		"proofVersion":                  1,
		"provider":                      options.Provider,
		"workflowId":                    options.WorkflowID,
		"gate":                          options.Gate,
		"stage":                         stage,
		"worktree":                      filepath.ToSlash(repo),
		"dispatchId":                    dispatch.DispatchID,
		"dispatchRegistrationArtifact":  dispatchRel,
		"dispatchRegistrationSha256":    sha256File(dispatchPath),
		"subagentId":                    startEvent.SubagentID,
		"normalizedEvents":              []string{"subagent_start", "subagent_stop"},
		"rawEventNames":                 []string{startEvent.RawEventName, stopEvent.RawEventName},
		"startEventArtifact":            relativePath(repo, startPath),
		"startEventSha256":              sha256File(startPath),
		"stopEventArtifact":             relativePath(repo, stopPath),
		"stopEventSha256":               sha256File(stopPath),
		"reviewArtifact":                relativePath(repo, artifactPath),
		"reviewArtifactCanonicalSha256": canonicalReviewArtifactHash(artifactText),
		"status":                        stopEvent.Status,
	}
	if err := writeJSON(receiptPath, receipt); err != nil {
		result.add("receipt", err.Error())
		return ReceiptFinalizeOutput{}, result
	}
	hash := sha256File(receiptPath)
	return ReceiptFinalizeOutput{
		ReviewerProofReceipt: receiptRel + " sha256=" + hash,
		ReceiptArtifact:      receiptRel,
		ReceiptSha256:        hash,
	}, result
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
	artifactText, err := readText(artifactPath)
	if err != nil {
		result.add(options.Artifact, "cannot read artifact: "+err.Error())
		return result
	}
	receiptPath := resolvePath(root, options.Receipt)
	if !isFile(receiptPath) {
		result.add(options.Receipt, "receipt path does not exist")
		return result
	}
	if options.WorkflowID != "" && routeValue(artifactText, "workflow_id") != options.WorkflowID {
		result.add(options.Artifact, "gate_route workflow_id does not match --workflow-id")
	}
	if options.ChangeSnapshot != "" && routeValue(artifactText, "change_snapshot") != options.ChangeSnapshot {
		result.add(options.Artifact, "gate_route change_snapshot does not match --change-snapshot")
	}
	receiptRef := relativePath(root, receiptPath) + " sha256=" + sha256File(receiptPath)
	withSyntheticReceipt := ensureReceiptReference(artifactText, receiptRef)
	validateReceipt(ArtifactOptions{
		Root:       root,
		File:       options.Artifact,
		Gate:       options.Gate,
		WorkflowID: options.WorkflowID,
		Stage:      options.Stage,
	}, withSyntheticReceipt, &result)
	return result
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
		Status:                   "UNSUPPORTED_HOST_RECEIPT",
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

func readDispatchRegistration(repo, provider, dispatchID, artifact string) (*dispatchRegistration, string) {
	if strings.TrimSpace(artifact) != "" {
		path := resolvePath(repo, artifact)
		if dispatch, ok := decodeDispatch(path); ok && (provider == "" || dispatch.Provider == provider) {
			return &dispatch, relativePath(repo, path)
		}
	}
	if strings.TrimSpace(dispatchID) == "" {
		return nil, ""
	}
	dir := filepath.Join(repo, ".claude", "gates", "proofs", "dispatch")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, ""
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
			return nil, ""
		}
		copy := dispatch
		found = &copy
		foundRel = relativePath(repo, path)
	}
	return found, foundRel
}

func decodeDispatch(path string) (dispatchRegistration, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return dispatchRegistration{}, false
	}
	var dispatch dispatchRegistration
	if err := json.Unmarshal(data, &dispatch); err != nil {
		return dispatchRegistration{}, false
	}
	return dispatch, true
}

func findOpenDispatch(repo, provider, workflowID, gate, stage, artifactPath string) (string, dispatchRegistration, bool) {
	dir := filepath.Join(repo, ".claude", "gates", "proofs", "dispatch")
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
			strings.TrimSpace(dispatch.ReceiptArtifact) == "" &&
			filepath.Clean(resolvePath(repo, dispatch.ReviewArtifact)) == filepath.Clean(artifactPath) {
			count++
			path = candidate
			found = dispatch
		}
	}
	return path, found, count == 1
}

func findLifecyclePair(repo, dispatchID, dispatchRel, provider, workflowID, gate, stage string) (string, receiptEventRecord, string, receiptEventRecord, bool) {
	dir := filepath.Join(repo, ".claude", "gates", "proofs", "events")
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
		if event.DispatchID != dispatchID && filepath.Clean(resolvePath(repo, event.DispatchRegistrationArtifact)) != filepath.Clean(resolvePath(repo, dispatchRel)) {
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
	if err := json.Unmarshal(data, &event); err != nil {
		return receiptEventRecord{}, false
	}
	return event, true
}

func ensureReceiptReference(text, ref string) string {
	lines := strings.Split(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"), "\n")
	for i, line := range lines {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(line)), "reviewer proof receipt:") {
			lines[i] = "Reviewer proof receipt: " + ref
			return strings.Join(lines, "\n")
		}
	}
	insert := 6
	if len(lines) < insert {
		insert = len(lines)
	}
	out := append([]string{}, lines[:insert]...)
	out = append(out, "Reviewer proof receipt: "+ref)
	out = append(out, lines[insert:]...)
	return strings.Join(out, "\n")
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
	return os.WriteFile(path, data, 0o600)
}

func relativePath(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || strings.HasPrefix(rel, "..") {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
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
