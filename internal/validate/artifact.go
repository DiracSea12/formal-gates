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
	Flow           string
	RunDir         string
}

var knownGates = map[string]bool{
	"requirements-clarification-gate": true,
	"qa-test-gate":                    true,
	"complexity-gate":                 true,
	"architecture-health-gate":        true,
	"code-quality-gate":               true,
}

func Artifact(options ArtifactOptions) Result {
	root := cleanRoot(options.Root)
	var result Result
	if strings.TrimSpace(options.File) == "" {
		result.add("artifact", "--file is required")
		return result
	}
	if !knownGates[options.Gate] {
		result.add("artifact", "unknown built-in gate: "+options.Gate)
		return result
	}
	path := resolvePath(root, options.File)
	runDir := artifactRunDir(options, options.WorkflowID)
	if options.RunDir == "" && samePath(runDir, root) {
		result.add("artifact", "artifact must be under .gates/runs")
		return result
	}
	if activeWorkflowRun(root, runDir) {
		logical, err := logicalPathInRun(runDir, path)
		if err != nil || !restrictedEvidencePath(root, runDir, logical) {
			result.add("artifact", "artifact must be under the active run restricted directory: "+slash(path))
			return result
		}
	}
	options.RunDir = runDir
	data, err := os.ReadFile(path)
	if err != nil {
		result.add(options.File, fmt.Sprintf("cannot read artifact: %v", err))
		return result
	}
	decoded := decodeArtifact(options, data, &result)
	if result.OK() && decoded.Policy.ReceiptRequired && finalizedReceiptExists(options, decoded) {
		if _, err := matchingReceiptRef(options, decoded); err != nil {
			result.add("receipt", err.Error())
		}
	}
	return result
}

func validateReceipt(options ArtifactOptions, runDir string, ref EvidenceRef, result *Result) {
	options.RunDir = runDir
	if activeWorkflowRun(options.Root, runDir) && !restrictedEvidencePath(options.Root, runDir, ref.Path) {
		result.add(options.File, "reviewer receipt must be under the active run restricted directory")
		return
	}
	receiptPath, err := safeEvidencePath(runDir, ref.Path)
	if err != nil {
		result.add(options.File, err.Error())
		return
	}
	if sha256File(receiptPath) != ref.SHA256 {
		result.add(options.File, "receipt hash mismatch")
		return
	}
	data, err := os.ReadFile(receiptPath)
	if err != nil {
		result.add(options.File, err.Error())
		return
	}
	var receipt reviewerProofReceipt
	if err := strictContractJSON(data, &receipt); err != nil {
		result.add(options.File, "reviewer receipt is invalid JSON")
		return
	}
	if receipt.ProofVersion != 1 || !knownReceiptProvider(receipt.Provider) || receipt.WorkflowID != options.WorkflowID || receipt.ChangeSnapshot != options.ChangeSnapshot || receipt.Gate != options.Gate || normalizeStage(receipt.Stage) != normalizeStage(options.Stage) {
		result.add(options.File, "reviewer receipt binding does not match artifact request")
	}
	if providerRequiresLifecycle(receipt.Provider) && (len(receipt.NormalizedEvents) != 2 || receipt.NormalizedEvents[0] != "subagent_start" || receipt.NormalizedEvents[1] != "subagent_stop") {
		result.add(options.File, "reviewer receipt must include start and stop events")
	}
	reviewPath := resolvePath(options.Root, options.File)
	receiptReviewPath := resolvePath(options.Root, receipt.ReviewArtifact)
	if logicalPath, pathErr := safeEvidencePath(runDir, receipt.ReviewArtifact); pathErr == nil {
		receiptReviewPath = logicalPath
	}
	if !samePath(receiptReviewPath, reviewPath) || receipt.ReviewArtifactSha256 != sha256File(reviewPath) {
		result.add(options.File, "reviewer receipt does not bind the exact review artifact bytes")
	}
	if reviewJudgmentLifecycle(receipt.Gate, receipt.Stage) {
		validateFinalSendPrompt(options.Root, runDir, receipt.PromptArtifact, receipt.PromptSha256, receipt.Gate, receipt.Stage, result, options.File)
	}
	validateReceiptDispatch(options, receipt, receiptPath, result)
	if providerRequiresLifecycle(receipt.Provider) {
		start := validateReceiptEvent(options, receipt, receipt.StartEventArtifact, receipt.StartEventSha256, "subagent_start", result)
		stop := validateReceiptEvent(options, receipt, receipt.StopEventArtifact, receipt.StopEventSha256, "subagent_stop", result)
		if start.SubagentID == "" || receipt.SubagentID != start.SubagentID || start.SubagentID != stop.SubagentID {
			result.add(options.File, "reviewer receipt start and stop subagent IDs do not match")
		}
	}
}

func validateReceiptDispatch(options ArtifactOptions, receipt reviewerProofReceipt, receiptPath string, result *Result) {
	receiptReviewPath := resolvePath(options.Root, receipt.ReviewArtifact)
	if logicalPath, pathErr := safeEvidencePath(options.RunDir, receipt.ReviewArtifact); pathErr == nil {
		receiptReviewPath = logicalPath
	}
	if activeWorkflowRun(options.Root, options.RunDir) && !restrictedRepoPath(options.Root, options.RunDir, receipt.DispatchRegistrationArtifact) {
		result.add(options.File, "dispatch registration must be under the active run restricted directory")
		return
	}
	path := resolvePath(options.Root, receipt.DispatchRegistrationArtifact)
	if logicalPath, pathErr := safeEvidencePath(options.RunDir, receipt.DispatchRegistrationArtifact); pathErr == nil {
		path = logicalPath
	}
	if !isSHA256(receipt.DispatchRegistrationSha256) || sha256File(path) != receipt.DispatchRegistrationSha256 {
		result.add(options.File, "dispatch registration path or hash is invalid")
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		result.add(options.File, err.Error())
		return
	}
	var dispatch dispatchRegistration
	if err := strictContractJSON(data, &dispatch); err != nil {
		result.add(options.File, "dispatch registration is invalid JSON")
		return
	}
	if activeWorkflowRun(options.Root, options.RunDir) && (!restrictedRepoPath(options.Root, options.RunDir, dispatch.ReviewArtifact) || !restrictedRepoPath(options.Root, options.RunDir, dispatch.ReceiptArtifact)) {
		result.add(options.File, "dispatch dependencies must be under the active run restricted directory")
		return
	}
	dispatchReviewPath := resolvePath(options.Root, dispatch.ReviewArtifact)
	if logicalPath, pathErr := safeEvidencePath(options.RunDir, dispatch.ReviewArtifact); pathErr == nil {
		dispatchReviewPath = logicalPath
	}
	dispatchReceiptPath := resolvePath(options.Root, dispatch.ReceiptArtifact)
	if logicalPath, pathErr := safeEvidencePath(options.RunDir, dispatch.ReceiptArtifact); pathErr == nil {
		dispatchReceiptPath = logicalPath
	}
	if dispatch.ProofVersion != 1 || dispatch.Status != "finalized" || dispatch.DispatchID != receipt.DispatchID || dispatch.Provider != receipt.Provider || dispatch.WorkflowID != receipt.WorkflowID || dispatch.ChangeSnapshot != receipt.ChangeSnapshot || dispatch.Gate != receipt.Gate || normalizeStage(dispatch.Stage) != normalizeStage(receipt.Stage) || !samePath(dispatchReviewPath, receiptReviewPath) || !samePath(dispatchReceiptPath, receiptPath) {
		result.add(options.File, "finalized dispatch registration does not match receipt")
	}
	if reviewJudgmentLifecycle(receipt.Gate, receipt.Stage) && (dispatch.PromptArtifact != receipt.PromptArtifact || dispatch.PromptSha256 != receipt.PromptSha256) {
		result.add(options.File, "finalized dispatch registration prompt binding does not match receipt")
	}
	if reviewJudgmentLifecycle(receipt.Gate, receipt.Stage) {
		reviewBytes, err := os.ReadFile(receiptReviewPath)
		var envelope FormalGateEvidence
		if err != nil || strictContractJSON(reviewBytes, &envelope) != nil {
			result.add(options.File, "final review artifact cannot be related to its semantic submission")
		} else {
			envelope.Verdict = "PENDING"
			submittedBytes, marshalErr := json.MarshalIndent(envelope, "", "  ")
			if marshalErr != nil {
				result.add(options.File, marshalErr.Error())
			} else {
				submittedBytes = append(submittedBytes, '\n')
				if !isSHA256(dispatch.SemanticSubmissionSHA) || dispatch.SemanticSubmissionSHA != sha256Bytes(submittedBytes) {
					result.add(options.File, "finalized dispatch does not prove the CLI semantic submission")
				}
			}
		}
	} else if qaDesignLifecycle(receipt.Gate, receipt.Stage) {
		designBytes, err := os.ReadFile(receiptReviewPath)
		if err != nil || !isSHA256(dispatch.SemanticSubmissionSHA) || len(designBytes) == 0 {
			result.add(options.File, "finalized dispatch does not prove the CLI QA Design submission")
		}
	}
	validateQADesignReviewPromptBinding(options.Root, options.RunDir, dispatch.ReviewPolicyID, dispatch.PromptArtifact, dispatch.CheckEvidence, dispatch.WorkflowID, dispatch.ChangeSnapshot, result)
}

func validateReceiptEvent(options ArtifactOptions, receipt reviewerProofReceipt, pathText, expectedHash, expectedEvent string, result *Result) receiptEventRecord {
	var event receiptEventRecord
	if activeWorkflowRun(options.Root, options.RunDir) && (!restrictedRepoPath(options.Root, options.RunDir, pathText)) {
		result.add(options.File, expectedEvent+" event must be under the active run restricted directory")
		return event
	}
	path := resolvePath(options.Root, pathText)
	if logicalPath, pathErr := safeEvidencePath(options.RunDir, pathText); pathErr == nil {
		path = logicalPath
	}
	if !isSHA256(expectedHash) || sha256File(path) != expectedHash {
		result.add(options.File, expectedEvent+" event path or hash is invalid")
		return event
	}
	data, err := os.ReadFile(path)
	if err != nil || strictContractJSON(data, &event) != nil {
		result.add(options.File, expectedEvent+" event is invalid")
		return event
	}
	if activeWorkflowRun(options.Root, options.RunDir) && !restrictedRepoPath(options.Root, options.RunDir, event.DispatchRegistrationArtifact) {
		result.add(options.File, expectedEvent+" dispatch dependency must be under the active run restricted directory")
		return event
	}
	if event.Provider != receipt.Provider || event.WorkflowID != options.WorkflowID || event.ChangeSnapshot != options.ChangeSnapshot || event.Gate != options.Gate || normalizeStage(event.Stage) != normalizeStage(options.Stage) || event.NormalizedEvent != expectedEvent || event.DispatchID != receipt.DispatchID || !samePath(resolvePath(options.Root, event.DispatchRegistrationArtifact), resolvePath(options.Root, receipt.DispatchRegistrationArtifact)) {
		result.add(options.File, expectedEvent+" event binding does not match receipt")
	}
	return event
}

func fieldValue(text, field string) string {
	pattern := regexp.MustCompile(`(?im)^[ \t]*` + regexp.QuoteMeta(field) + `[ \t]*:[ \t]*(.*)$`)
	match := pattern.FindStringSubmatch(text)
	if len(match) < 2 {
		return ""
	}
	return strings.TrimSpace(match[1])
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
	hash := sha256File(path)
	if hash == "" {
		t.Fatal("failed to hash file: " + path)
	}
	return hash
}

func knownReceiptProvider(provider string) bool {
	return provider == "codex" || provider == "claude-code" || provider == "cursor"
}
func normalizeStage(stage string) string { return strings.TrimSpace(stage) }
func isSHA256(value string) bool {
	return regexp.MustCompile(`^[a-f0-9]{64}$`).MatchString(strings.TrimSpace(value))
}
func meaningful(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || regexp.MustCompile(`<[^>\r\n]+>`).MatchString(value) {
		return false
	}
	switch strings.ToLower(value) {
	case "unavailable", "unknown", "none", "null", "n/a", "na", "todo", "tbd", "placeholder", "sample", "example":
		return false
	}
	return true
}
