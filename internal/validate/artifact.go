package validate

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

type ArtifactOptions struct {
	Root           string
	File           string
	Gate           string
	WorkflowID     string
	ChangeSnapshot string
	Stage          string
}

var knownGates = map[string]bool{
	"requirements-clarification-gate": true,
	"qa-test-gate":                    true,
	"complexity-gate":                 true,
	"architecture-health-gate":        true,
	"code-quality-gate":               true,
}

var postDevelopmentCommon = []string{
	"Review mode: ZERO_CONTEXT_FORMAL",
	"Prompt contamination check: PASS",
	"Semantic anti-anchor check: PASS",
	"Zero-context reviewer: YES",
	"Independent agent: YES",
	"Context bundle:",
	"Dispatch prompt artifact:",
	"No-anchor prompt: YES",
	"gate_route:",
}

func Artifact(options ArtifactOptions) Result {
	root := cleanRoot(options.Root)
	var result Result
	if strings.TrimSpace(options.File) == "" {
		result.add("artifact", "--file is required")
		return result
	}
	if strings.TrimSpace(options.Gate) == "" {
		result.add("artifact", "--gate is required")
		return result
	}
	if !knownGates[options.Gate] {
		result.add("artifact", "unknown built-in gate: "+options.Gate)
		return result
	}

	path := options.File
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, filepath.FromSlash(path))
	}
	text, err := readText(path)
	if err != nil {
		result.add(options.File, fmt.Sprintf("cannot read artifact: %v", err))
		return result
	}

	required := requiredArtifactFields(options.Gate)
	for _, field := range required {
		if !strings.Contains(text, field) {
			result.add(options.File, "missing required field text: "+field)
		}
	}
	for _, field := range fieldsThatNeedValues(options.Gate) {
		value := fieldValue(text, field)
		if !meaningful(value) {
			result.add(options.File, "field has no meaningful value: "+field)
		}
	}
	validateRoute(options, text, &result)
	return result
}

func requiredArtifactFields(gate string) []string {
	if gate == "requirements-clarification-gate" {
		return []string{
			"Requirement source:",
			"Alignment table artifact:",
			"Total alignment items:",
			"Open question IDs:",
			"User confirmation:",
			"Dimension coverage:",
			"Decision record:",
			"Covered formal targets:",
			"Downstream permission:",
			"gate_route:",
		}
	}

	fields := append([]string{}, postDevelopmentCommon...)
	fields = append(fields, "Prompt source: "+expectedPromptSource(gate))
	if gate == "complexity-gate" {
		fields = append(fields,
			"Script result",
			"Diff shape judgment",
			"Impact surface health",
			"Public/config surface",
			"New concepts",
			"Shrink opportunities",
			"Decision evidence",
		)
	}
	if gate == "qa-test-gate" {
		fields = append(fields, "Approved case set:", "QA-owned evidence:", "Case-to-artifact binding:")
	}
	return fields
}

func fieldsThatNeedValues(gate string) []string {
	if gate == "requirements-clarification-gate" {
		return []string{"Requirement source", "Alignment table artifact", "Total alignment items", "Decision record", "Covered formal targets"}
	}
	return []string{"Context bundle", "Dispatch prompt artifact"}
}

func expectedPromptSource(gate string) string {
	switch gate {
	case "qa-test-gate":
		return "agents/qa-test-gate.md"
	case "complexity-gate":
		return "agents/complexity-gate.md"
	case "architecture-health-gate":
		return "agents/architecture-health-gate.md"
	case "code-quality-gate":
		return "agents/code-quality-gate.md"
	default:
		return "agents/" + gate + ".md"
	}
}

func validateRoute(options ArtifactOptions, text string, result *Result) {
	if options.WorkflowID != "" && routeValue(text, "workflow_id") != options.WorkflowID {
		result.add(options.File, "gate_route workflow_id does not match --workflow-id")
	}
	if options.ChangeSnapshot != "" && routeValue(text, "change_snapshot") != options.ChangeSnapshot {
		result.add(options.File, "gate_route change_snapshot does not match --change-snapshot")
	}
	next := routeValue(text, "next_action")
	if next == "" {
		result.add(options.File, "gate_route next_action is missing")
	}
	if options.Gate == "requirements-clarification-gate" {
		if strings.ToLower(strings.TrimSpace(fieldValue(text, "Open question IDs"))) != "none" {
			result.add(options.File, "Open question IDs must be none for PASS")
		}
		if !strings.EqualFold(strings.TrimSpace(fieldValue(text, "User confirmation")), "YES") {
			result.add(options.File, "User confirmation must be YES for PASS")
		}
	}
	if options.Gate != "requirements-clarification-gate" {
		validateLegacyReviewerProof(text, options.File, result)
		if meaningful(fieldValue(text, "Reviewer proof receipt")) {
			validateReceipt(options, text, result)
		}
	}
}

func validateLegacyReviewerProof(text, file string, result *Result) {
	reviewerAgentID := fieldValue(text, "Reviewer agent id")
	if strings.TrimSpace(reviewerAgentID) != "" && regexp.MustCompile(`<[^>\r\n]+>`).MatchString(reviewerAgentID) {
		result.add(file, "Reviewer agent id placeholder is not proof; use Reviewer proof receipt only when receipt-backed proof is claimed")
	}
	for _, field := range []string{"Reviewer proof", "Self-reported reviewer proof", "Reviewer proof artifact"} {
		if meaningful(fieldValue(text, field)) {
			result.add(file, field+" is a legacy self-reported proof field; use Reviewer proof receipt only when receipt-backed proof is claimed")
		}
	}
}

func fieldValue(text, field string) string {
	pattern := regexp.MustCompile(`(?im)^[ \t]*` + regexp.QuoteMeta(field) + `[ \t]*:[ \t]*(.*?)[ \t]*$`)
	match := pattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

func routeValue(text, field string) string {
	pattern := regexp.MustCompile(`(?im)^[ \t]*` + regexp.QuoteMeta(field) + `[ \t]*:[ \t]*"?([^"\r\n]+)"?[ \t]*$`)
	match := pattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
}

type reviewerProofReceipt struct {
	ProofVersion                  int      `json:"proofVersion"`
	Provider                      string   `json:"provider"`
	WorkflowID                    string   `json:"workflowId"`
	Gate                          string   `json:"gate"`
	Stage                         string   `json:"stage"`
	DispatchID                    string   `json:"dispatchId"`
	DispatchRegistrationArtifact  string   `json:"dispatchRegistrationArtifact"`
	DispatchRegistrationSha256    string   `json:"dispatchRegistrationSha256"`
	NormalizedEvents              []string `json:"normalizedEvents"`
	StartEventArtifact            string   `json:"startEventArtifact"`
	StartEventSha256              string   `json:"startEventSha256"`
	StopEventArtifact             string   `json:"stopEventArtifact"`
	StopEventSha256               string   `json:"stopEventSha256"`
	ReviewArtifact                string   `json:"reviewArtifact"`
	ReviewArtifactCanonicalSha256 string   `json:"reviewArtifactCanonicalSha256"`
}

type lifecycleEvent struct {
	WorkflowID       string `json:"workflowId"`
	Gate             string `json:"gate"`
	Stage            string `json:"stage"`
	NormalizedEvent  string `json:"normalizedEvent"`
	SubagentID       string `json:"subagentId"`
	Kind             string `json:"kind"`
	Event            string `json:"event"`
	FormalWorkflowID string `json:"formalWorkflowId"`
	GateID           string `json:"gateId"`
	DispatchID       string `json:"dispatchId"`
	DispatchArtifact string `json:"dispatchRegistrationArtifact"`
}

type dispatchRegistration struct {
	ProofVersion    int    `json:"proofVersion"`
	DispatchID      string `json:"dispatchId"`
	Provider        string `json:"provider"`
	WorkflowID      string `json:"workflowId"`
	Gate            string `json:"gate"`
	Stage           string `json:"stage"`
	ReviewArtifact  string `json:"reviewArtifact"`
	ReceiptArtifact string `json:"receiptArtifact"`
}

func validateReceipt(options ArtifactOptions, text string, result *Result) {
	value := fieldValue(text, "Reviewer proof receipt")
	receiptPathText, expectedHash, ok := parseHashedReference(value)
	if !ok {
		result.add(options.File, "Reviewer proof receipt: <path> sha256=<sha256>")
		return
	}
	receiptPath := resolvePath(options.Root, receiptPathText)
	if !isFile(receiptPath) {
		result.add(options.File, "Reviewer proof receipt path does not exist: "+receiptPathText)
		return
	}
	if actual := sha256File(receiptPath); actual != expectedHash {
		result.add(options.File, "Reviewer proof receipt sha256 mismatch: "+receiptPathText)
		return
	}
	data, err := os.ReadFile(receiptPath)
	if err != nil {
		result.add(options.File, "cannot read Reviewer proof receipt: "+err.Error())
		return
	}
	var receipt reviewerProofReceipt
	if err := json.Unmarshal(data, &receipt); err != nil {
		result.add(options.File, "Reviewer proof receipt is not valid JSON")
		return
	}
	if receipt.ProofVersion != 1 {
		result.add(options.File, "Reviewer proof receipt proofVersion must be 1")
	}
	if !knownReceiptProvider(receipt.Provider) {
		result.add(options.File, "Reviewer proof receipt provider is unsupported")
	}
	if options.WorkflowID != "" && receipt.WorkflowID != options.WorkflowID {
		result.add(options.File, "Reviewer proof receipt workflowId must match --workflow-id")
	}
	if receipt.Gate != options.Gate {
		result.add(options.File, "Reviewer proof receipt gate must match --gate")
	}
	if normalizeStage(receipt.Stage) != normalizeStage(options.Stage) {
		result.add(options.File, "Reviewer proof receipt stage must match --stage")
	}
	if !contains(receipt.NormalizedEvents, "subagent_start") || !contains(receipt.NormalizedEvents, "subagent_stop") {
		result.add(options.File, "Reviewer proof receipt must include subagent_start and subagent_stop")
	}
	reviewPath := resolvePath(options.Root, options.File)
	receiptReviewPath := resolvePath(options.Root, receipt.ReviewArtifact)
	if filepath.Clean(receiptReviewPath) != filepath.Clean(reviewPath) {
		result.add(options.File, "Reviewer proof receipt reviewArtifact must match artifact path")
	}
	if receipt.ReviewArtifactCanonicalSha256 != canonicalReviewArtifactHash(text) {
		result.add(options.File, "Reviewer proof receipt reviewArtifactCanonicalSha256 mismatch")
	}
	validateReceiptDispatch(options, receipt, receiptPath, result)
	start := validateReceiptEvent(options, receipt, receipt.StartEventArtifact, receipt.StartEventSha256, "subagent_start", result)
	stop := validateReceiptEvent(options, receipt, receipt.StopEventArtifact, receipt.StopEventSha256, "subagent_stop", result)
	if start.SubagentID != "" && stop.SubagentID != "" && start.SubagentID != stop.SubagentID {
		result.add(options.File, "Reviewer proof receipt start/stop subagentId mismatch")
	}
}

func validateReceiptDispatch(options ArtifactOptions, receipt reviewerProofReceipt, receiptPath string, result *Result) {
	if strings.TrimSpace(receipt.DispatchRegistrationArtifact) == "" || !isSHA256(receipt.DispatchRegistrationSha256) {
		result.add(options.File, "Reviewer proof receipt dispatch registration path/hash is missing")
		return
	}
	path := resolvePath(options.Root, receipt.DispatchRegistrationArtifact)
	if !isFile(path) {
		result.add(options.File, "Reviewer proof receipt dispatch registration path does not exist: "+receipt.DispatchRegistrationArtifact)
		return
	}
	if actual := sha256File(path); actual != strings.ToLower(receipt.DispatchRegistrationSha256) {
		result.add(options.File, "Reviewer proof receipt dispatch registration sha256 mismatch: "+receipt.DispatchRegistrationArtifact)
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		result.add(options.File, "cannot read Reviewer proof receipt dispatch registration: "+err.Error())
		return
	}
	var dispatch dispatchRegistration
	if err := json.Unmarshal(data, &dispatch); err != nil {
		result.add(options.File, "Reviewer proof receipt dispatch registration is not valid JSON")
		return
	}
	if dispatch.ProofVersion != 1 {
		result.add(options.File, "Reviewer proof receipt dispatch registration proofVersion must be 1")
	}
	if dispatch.DispatchID != receipt.DispatchID {
		result.add(options.File, "Reviewer proof receipt dispatch registration dispatchId must match receipt")
	}
	if !knownReceiptProvider(dispatch.Provider) {
		result.add(options.File, "Reviewer proof receipt dispatch registration provider is unsupported")
	}
	if dispatch.Provider != receipt.Provider {
		result.add(options.File, "Reviewer proof receipt dispatch registration provider must match receipt")
	}
	if dispatch.WorkflowID != receipt.WorkflowID {
		result.add(options.File, "Reviewer proof receipt dispatch registration workflowId must match receipt")
	}
	if dispatch.Gate != receipt.Gate {
		result.add(options.File, "Reviewer proof receipt dispatch registration gate must match receipt")
	}
	if normalizeStage(dispatch.Stage) != normalizeStage(receipt.Stage) {
		result.add(options.File, "Reviewer proof receipt dispatch registration stage must match receipt")
	}
	if filepath.Clean(resolvePath(options.Root, dispatch.ReviewArtifact)) != filepath.Clean(resolvePath(options.Root, receipt.ReviewArtifact)) {
		result.add(options.File, "Reviewer proof receipt dispatch registration reviewArtifact must match receipt")
	}
	if strings.TrimSpace(dispatch.ReceiptArtifact) == "" {
		result.add(options.File, "Reviewer proof receipt dispatch registration receiptArtifact must be finalized")
	} else if filepath.Clean(resolvePath(options.Root, dispatch.ReceiptArtifact)) != filepath.Clean(receiptPath) {
		result.add(options.File, "Reviewer proof receipt dispatch registration receiptArtifact must match receipt")
	}
}

func validateReceiptEvent(options ArtifactOptions, receipt reviewerProofReceipt, pathText, expectedHash, expectedEvent string, result *Result) lifecycleEvent {
	var event lifecycleEvent
	if strings.TrimSpace(pathText) == "" || !isSHA256(expectedHash) {
		result.add(options.File, "Reviewer proof receipt "+expectedEvent+" event path/hash is missing")
		return event
	}
	path := resolvePath(options.Root, pathText)
	if !isFile(path) {
		result.add(options.File, "Reviewer proof receipt "+expectedEvent+" event path does not exist: "+pathText)
		return event
	}
	if actual := sha256File(path); actual != strings.ToLower(expectedHash) {
		result.add(options.File, "Reviewer proof receipt "+expectedEvent+" event sha256 mismatch: "+pathText)
		return event
	}
	data, err := os.ReadFile(path)
	if err != nil {
		result.add(options.File, "cannot read Reviewer proof receipt "+expectedEvent+" event: "+err.Error())
		return event
	}
	if err := json.Unmarshal(data, &event); err != nil {
		result.add(options.File, "Reviewer proof receipt "+expectedEvent+" event is not valid JSON")
		return event
	}
	if event.WorkflowID == "" {
		event.WorkflowID = event.FormalWorkflowID
	}
	if event.Gate == "" {
		event.Gate = event.GateID
	}
	if event.NormalizedEvent == "" {
		event.NormalizedEvent = event.Kind
	}
	if event.NormalizedEvent == "" {
		event.NormalizedEvent = event.Event
	}
	if options.WorkflowID != "" && event.WorkflowID != options.WorkflowID {
		result.add(options.File, "Reviewer proof receipt "+expectedEvent+" event workflowId must match --workflow-id")
	}
	if event.Gate != options.Gate {
		result.add(options.File, "Reviewer proof receipt "+expectedEvent+" event gate must match --gate")
	}
	if normalizeStage(event.Stage) != normalizeStage(options.Stage) {
		result.add(options.File, "Reviewer proof receipt "+expectedEvent+" event stage must match --stage")
	}
	if event.NormalizedEvent != expectedEvent {
		result.add(options.File, "Reviewer proof receipt event kind must be "+expectedEvent)
	}
	if strings.TrimSpace(receipt.DispatchID) != "" && event.DispatchID != receipt.DispatchID {
		result.add(options.File, "Reviewer proof receipt "+expectedEvent+" event dispatchId must match dispatch registration")
	}
	if strings.TrimSpace(receipt.DispatchID) == "" && strings.TrimSpace(event.DispatchID) != "" {
		result.add(options.File, "Reviewer proof receipt "+expectedEvent+" event dispatchId must match dispatch registration")
	}
	if strings.TrimSpace(receipt.DispatchRegistrationArtifact) != "" {
		receiptDispatchPath := filepath.Clean(resolvePath(options.Root, receipt.DispatchRegistrationArtifact))
		if strings.TrimSpace(event.DispatchArtifact) == "" {
			result.add(options.File, "Reviewer proof receipt "+expectedEvent+" event dispatchRegistrationArtifact must match receipt")
		} else if filepath.Clean(resolvePath(options.Root, event.DispatchArtifact)) != receiptDispatchPath {
			result.add(options.File, "Reviewer proof receipt "+expectedEvent+" event dispatchRegistrationArtifact must match receipt")
		}
	} else if strings.TrimSpace(event.DispatchArtifact) != "" {
		result.add(options.File, "Reviewer proof receipt "+expectedEvent+" event dispatchRegistrationArtifact must match receipt")
	}
	return event
}

func parseHashedReference(value string) (string, string, bool) {
	value = strings.Trim(strings.TrimSpace(value), `"'`)
	match := regexp.MustCompile(`(?i)\bsha(?:256)?\s*[:=]\s*([a-f0-9]{64})\b`).FindStringSubmatch(value)
	if len(match) < 2 {
		return "", "", false
	}
	pathText := regexp.MustCompile(`(?i)\s+sha(?:256)?\s*[:=]\s*[a-f0-9]{64}\b`).ReplaceAllString(value, "")
	pathText = strings.TrimSpace(regexp.MustCompile(`\s+\(.*$`).ReplaceAllString(pathText, ""))
	if pathText == "" {
		return "", "", false
	}
	return pathText, strings.ToLower(match[1]), true
}

func resolvePath(root, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Clean(filepath.Join(cleanRoot(root), filepath.FromSlash(value)))
}

func sha256File(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func sha256FileForTest(t interface{ Fatal(args ...any) }, path string) string {
	value := sha256File(path)
	if value == "" {
		t.Fatal("failed to hash file: " + path)
	}
	return value
}

func canonicalReviewArtifactHash(text string) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	out := make([]string, 0, len(lines))
	receiptLine := regexp.MustCompile(`(?i)^[ \t]*Reviewer proof receipt[ \t]*:`)
	for _, line := range lines {
		if receiptLine.MatchString(line) {
			continue
		}
		out = append(out, strings.TrimRight(line, " \t"))
	}
	sum := sha256.Sum256([]byte(strings.Join(out, "\n")))
	return hex.EncodeToString(sum[:])
}

func knownReceiptProvider(provider string) bool {
	switch provider {
	case "codex", "claude-code", "cursor":
		return true
	default:
		return false
	}
}

func normalizeStage(stage string) string {
	return strings.TrimSpace(stage)
}

func isSHA256(value string) bool {
	return regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(strings.ToLower(strings.TrimSpace(value)))
}

func meaningful(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if regexp.MustCompile(`<[^>\r\n]+>`).MatchString(value) {
		return false
	}
	switch strings.ToLower(value) {
	case "unavailable", "unknown", "none", "null", "n/a", "na", "todo", "tbd", "placeholder", "sample", "example":
		return false
	}
	return true
}
