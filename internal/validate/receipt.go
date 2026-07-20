package validate

import (
	"bytes"
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
	"unicode/utf8"
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
	ChangedFiles              string
	Verification              string
	QADesignCaseSet           string
	QADesignReceipt           string
	ComplexityStatistics      string
	TransitionChain           string
	CarrySourceClosures       []string
	QACaseCount               int
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

type ReceiptSubmitOptions struct {
	Worktree       string
	Artifact       string
	Checks         []ReceiptSemanticCheck
	Findings       []ReceiptSemanticFinding
	Locations      []ReceiptSemanticLocation
	CarryDecisions []ReceiptSemanticCarryDecision
	DesignCases    []ReceiptSemanticDesignCase
}

type ReceiptSemanticCheck struct {
	Position int
	Status   string
	Message  string
}

type ReceiptSemanticFinding struct {
	CheckPosition int
	Message       string
}

type ReceiptSemanticLocation struct {
	FindingPosition int
	Path            string
	StartLine       int
	EndLine         int
}

type ReceiptSemanticCarryDecision struct {
	GatePosition int
	Decision     string
	Reason       string
}

type ReceiptSemanticDesignCase struct {
	Position int
	Values   []string
}

type ReceiptSubmission struct {
	Artifact       string `json:"artifact"`
	ArtifactRole   string `json:"artifactRole,omitempty"`
	ArtifactSha256 string `json:"artifactSha256"`
	Status         string `json:"status"`
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
	ProofVersion          int                       `json:"proofVersion"`
	DispatchID            string                    `json:"dispatchId"`
	Provider              string                    `json:"provider"`
	WorkflowID            string                    `json:"workflowId"`
	ChangeSnapshot        string                    `json:"changeSnapshot"`
	Gate                  string                    `json:"gate"`
	Stage                 string                    `json:"stage"`
	ReviewArtifact        string                    `json:"reviewArtifact"`
	ContextBundle         string                    `json:"contextBundle,omitempty"`
	ContextSHA256         string                    `json:"contextSha256,omitempty"`
	ReviewPolicyID        string                    `json:"reviewPolicyId,omitempty"`
	ReviewTemplate        string                    `json:"reviewTemplate,omitempty"`
	ReviewTemplateSHA256  string                    `json:"reviewTemplateSha256,omitempty"`
	SemanticSubmissionSHA string                    `json:"semanticSubmissionSha256,omitempty"`
	ChangedFiles          *EvidenceRef              `json:"changedFiles,omitempty"`
	Verification          *EvidenceRef              `json:"verification,omitempty"`
	CheckEvidence         []registeredCheckEvidence `json:"checkEvidence,omitempty"`
	TransitionChain       *EvidenceRef              `json:"transitionChain,omitempty"`
	CarrySources          []registeredCarrySource   `json:"carrySources,omitempty"`
	QACaseCount           int                       `json:"qaCaseCount,omitempty"`
	ExtraReviewAuthorized bool                      `json:"extraReviewAuthorized,omitempty"`
	PromptArtifact        string                    `json:"promptArtifact,omitempty"`
	PromptSha256          string                    `json:"promptSha256,omitempty"`
	ReceiptArtifact       string                    `json:"receiptArtifact,omitempty"`
	Status                string                    `json:"status"`
}

type registeredCheckEvidence struct {
	ID           string        `json:"id"`
	EvidenceRefs []EvidenceRef `json:"evidenceRefs"`
}

type registeredCarrySource struct {
	Gate               string      `json:"gate"`
	SourceSnapshot     string      `json:"sourceSnapshot"`
	SourceGateEvidence EvidenceRef `json:"sourceGateEvidence"`
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
	if !reviewJudgmentLifecycle(options.Gate, options.Stage) && !qaDesignLifecycle(options.Gate, options.Stage) {
		result.add("receipt", "receipt registration supports only QA Design or an independent review judgment; QA Execution uses CLI composition")
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
	bundleRef := EvidenceRef{Path: options.ContextBundle, SHA256: sha256File(bundlePath)}
	if result.OK() {
		if proofErr := validateStandaloneCompositionProof(repo, runDir, "context-bundle.v1", options.WorkflowID, strings.TrimSpace(options.ChangeSnapshot), bundleRef); proofErr != nil {
			result.add(options.ContextBundle, proofErr.Error())
		}
	}
	policyID := ""
	if reviewJudgmentLifecycle(options.Gate, options.Stage) {
		policyID = validateDispatchRegistrationContract(repo, runDir, promptArtifact, promptSHA256, options, bundlePath, &result)
		validateDispatchContextForPolicy(repo, runDir, policyID, contextRefs, &result)
	}
	changedFiles, verification, checkEvidence := registeredReviewEvidence(repo, runDir, policyID, options, &result)
	validateRegisteredComplexityStatistics(repo, runDir, policyID, checkEvidence, options.WorkflowID, options.ChangeSnapshot, &result, "receipt")
	if reviewJudgmentLifecycle(options.Gate, options.Stage) {
		validateQADesignReviewPromptBinding(repo, runDir, policyID, promptArtifact, checkEvidence, options.WorkflowID, options.ChangeSnapshot, &result)
	}
	transitionChain, carrySources := registeredCarryEvidence(repo, runDir, policyID, options, &result)
	if qaDesignLifecycle(options.Gate, options.Stage) {
		if options.QACaseCount < 1 || options.QACaseCount > 200 {
			result.add("receipt", "--qa-case-count between 1 and 200 is required for QA Design")
		}
	} else if options.QACaseCount != 0 {
		result.add("receipt", "--qa-case-count is accepted only for QA Design")
	}
	if !result.OK() {
		return ReceiptRegistration{}, result
	}
	artifactPath, err := runLocalReviewArtifactPath(repo, runDir, options.Artifact)
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
		ReviewPolicyID:        policyID,
		ChangedFiles:          changedFiles,
		Verification:          verification,
		CheckEvidence:         checkEvidence,
		TransitionChain:       transitionChain,
		CarrySources:          carrySources,
		QACaseCount:           options.QACaseCount,
		ExtraReviewAuthorized: options.UserAuthorizedExtraReview,
		PromptArtifact:        promptArtifact,
		PromptSha256:          promptSHA256,
		Status:                "open",
	}
	var reviewTemplate FormalGateEvidence
	var templateBytes []byte
	var marshalErr error
	if reviewJudgmentLifecycle(record.Gate, record.Stage) {
		reviewTemplate, err = generatedDispatchTemplate(record)
		if err != nil {
			result.add("receipt", err.Error())
			return ReceiptRegistration{}, result
		}
		templateBytes, marshalErr = json.MarshalIndent(reviewTemplate, "", "  ")
		if marshalErr != nil {
			result.add("receipt", marshalErr.Error())
			return ReceiptRegistration{}, result
		}
		templateBytes = append(templateBytes, '\n')
	} else if qaDesignLifecycle(record.Gate, record.Stage) {
		templateBytes = generatedQADesignTemplate(record.QACaseCount)
	}
	if len(templateBytes) > 0 {
		templateHash := sha256Bytes(templateBytes)
		suffix := ".json"
		if qaDesignLifecycle(record.Gate, record.Stage) {
			suffix = ".md"
		}
		templatePath := filepath.Join(receiptProofDir(repo, runDir, "templates"), templateHash+suffix)
		templateRel, relErr := filepath.Rel(absPath(repo), absPath(templatePath))
		if relErr != nil || strings.HasPrefix(templateRel, "..") {
			result.add("receipt", "cannot derive run-local review template path")
			return ReceiptRegistration{}, result
		}
		record.ReviewTemplate = filepath.ToSlash(templateRel)
		record.ReviewTemplateSHA256 = templateHash
	}
	if existing, ok := decodeDispatch(path); ok {
		if existing.Status != "open" || strings.TrimSpace(existing.ReceiptArtifact) != "" {
			result.add("receipt", "review artifact path is already bound to a completed dispatch; use a distinct output path")
			return ReceiptRegistration{}, result
		}
		if hasLifecycleEventForDispatch(repo, runDir, existing.DispatchID, relativePath(repo, path)) {
			result.add("receipt", "review artifact path already exists; dispatch has started and cannot be rebound")
			return ReceiptRegistration{}, result
		}
		if !samePath(resolvePath(repo, existing.ReviewArtifact), artifactPath) || existing.ReviewTemplate == "" || !isSHA256(existing.ReviewTemplateSHA256) {
			result.add(options.Artifact, "review artifact path is already reserved by a non-generated dispatch; use a distinct output path")
			return ReceiptRegistration{}, result
		}
		existingTemplate, templateErr := os.ReadFile(resolvePath(repo, existing.ReviewTemplate))
		existingArtifact, artifactErr := os.ReadFile(artifactPath)
		if templateErr != nil || artifactErr != nil || sha256Bytes(existingTemplate) != existing.ReviewTemplateSHA256 || !bytes.Equal(existingTemplate, artifactBytesForDispatch(existing)) || !bytes.Equal(existingArtifact, existingTemplate) {
			result.add(options.Artifact, "review artifact path is already reserved by a non-CLI-generated template; use a distinct output path")
			return ReceiptRegistration{}, result
		}
		oldArtifact := append([]byte{}, existingArtifact...)
		newTemplatePath := resolvePath(repo, record.ReviewTemplate)
		newTemplateCreated := false
		if record.ReviewTemplate != "" {
			if _, statErr := os.Lstat(newTemplatePath); os.IsNotExist(statErr) {
				if err := writeBytesExclusive(newTemplatePath, templateBytes); err != nil {
					result.add("receipt", "cannot write generated review template: "+err.Error())
					return ReceiptRegistration{}, result
				}
				newTemplateCreated = true
			} else if statErr != nil || sha256File(newTemplatePath) != record.ReviewTemplateSHA256 {
				result.add("receipt", "cannot verify generated review template: "+record.ReviewTemplate)
				return ReceiptRegistration{}, result
			}
		}
		if existing.Provider == record.Provider && existing.WorkflowID == record.WorkflowID && existing.ChangeSnapshot == record.ChangeSnapshot && existing.Gate == record.Gate && normalizeStage(existing.Stage) == normalizeStage(record.Stage) && existing.ReviewArtifact == record.ReviewArtifact && existing.ContextBundle == record.ContextBundle && existing.ContextSHA256 == record.ContextSHA256 && existing.PromptArtifact == record.PromptArtifact && existing.PromptSha256 == record.PromptSha256 {
			result.add("receipt", "review artifact path already exists; already reserved by this dispatch")
			if newTemplateCreated {
				_ = os.Remove(newTemplatePath)
			}
			return ReceiptRegistration{}, result
		}
		if existing.WorkflowID != record.WorkflowID || existing.Gate != record.Gate || normalizeStage(existing.Stage) != normalizeStage(record.Stage) {
			if err := enforceReviewCapacity(repo, runDir, record.WorkflowID, record.Gate, record.Stage, options.UserAuthorizedExtraReview); err != nil {
				result.add("receipt", err.Error())
				if newTemplateCreated {
					_ = os.Remove(newTemplatePath)
				}
				return ReceiptRegistration{}, result
			}
		}
		if err := writeFileAtomic(artifactPath, templateBytes, 0o600); err != nil {
			result.add(options.Artifact, "cannot rebind generated review template: "+err.Error())
			if newTemplateCreated {
				_ = os.Remove(newTemplatePath)
			}
			return ReceiptRegistration{}, result
		}
		if err := writeJSON(path, record); err != nil {
			_ = writeFileAtomic(artifactPath, oldArtifact, 0o600)
			if newTemplateCreated {
				_ = os.Remove(newTemplatePath)
			}
			result.add("receipt", err.Error())
			return ReceiptRegistration{}, result
		}
		return ReceiptRegistration{
			DispatchID:                     id,
			DispatchRegistrationArtifact:   relativePath(repo, path),
			DispatchRegistrationStatusText: "rebound",
		}, result
	}
	if _, statErr := os.Lstat(artifactPath); statErr == nil {
		result.add(options.Artifact, "review artifact path already exists; stale output must be removed before registration")
		return ReceiptRegistration{}, result
	} else if !os.IsNotExist(statErr) {
		result.add(options.Artifact, statErr.Error())
		return ReceiptRegistration{}, result
	}
	if err := enforceReviewCapacity(repo, runDir, record.WorkflowID, record.Gate, record.Stage, options.UserAuthorizedExtraReview); err != nil {
		result.add("receipt", err.Error())
		return ReceiptRegistration{}, result
	}
	if record.ReviewTemplate != "" {
		templatePath := resolvePath(repo, record.ReviewTemplate)
		if err := writeBytesExclusive(templatePath, templateBytes); err != nil {
			if !os.IsExist(err) || sha256File(templatePath) != record.ReviewTemplateSHA256 {
				result.add("receipt", "cannot write generated review template: "+err.Error())
				return ReceiptRegistration{}, result
			}
		}
		if err := writeBytesExclusive(artifactPath, templateBytes); err != nil {
			result.add(options.Artifact, "cannot write generated review template: "+err.Error())
			return ReceiptRegistration{}, result
		}
	}
	if err := writeJSONExclusive(path, record); err != nil {
		if record.ReviewTemplate != "" {
			_ = os.Remove(artifactPath)
		}
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

func artifactBytesForDispatch(record dispatchRegistration) []byte {
	if qaDesignLifecycle(record.Gate, record.Stage) {
		return generatedQADesignTemplate(record.QACaseCount)
	}
	expected, err := generatedDispatchTemplate(record)
	if err != nil {
		return nil
	}
	data, err := json.MarshalIndent(expected, "", "  ")
	if err != nil {
		return nil
	}
	return append(data, '\n')
}

func registeredReviewEvidence(repo, runDir, policyID string, options ReceiptRegisterOptions, result *Result) (*EvidenceRef, *EvidenceRef, []registeredCheckEvidence) {
	if policyID == "" || policyID == "carry.arbiter.v2" {
		if options.ChangedFiles != "" || options.Verification != "" || options.QADesignCaseSet != "" || options.QADesignReceipt != "" || options.ComplexityStatistics != "" {
			result.add("receipt", "static reviewer evidence is not accepted for this dispatch role")
		}
		return nil, nil, nil
	}
	policy, ok := policyByID(policyID)
	if !ok {
		result.add("receipt", "review policy is unknown")
		return nil, nil, nil
	}
	resolve := func(label, logical string, required bool) *EvidenceRef {
		if strings.TrimSpace(logical) == "" {
			if required {
				result.add("receipt", "--"+label+" is required for "+policyID)
			}
			return nil
		}
		if !required {
			result.add("receipt", "--"+label+" is not allowed for "+policyID)
			return nil
		}
		ref, err := registeredEvidenceRef(repo, runDir, logical)
		if err != nil {
			result.add(logical, err.Error())
			return nil
		}
		composer := map[string]string{"changed-files": "changed-files.v1", "verification": "verification.v1"}[label]
		if composer != "" {
			if err := validateStandaloneCompositionProof(repo, runDir, composer, options.WorkflowID, options.ChangeSnapshot, ref); err != nil {
				result.add(logical, err.Error())
				return nil
			}
		}
		return &ref
	}
	changedFiles := resolve("changed-files", options.ChangedFiles, policy.ChangedFilesRequired)
	verification := resolve("verification", options.Verification, policy.VerificationRequired)
	resolveBinding := func(label, logical string) *EvidenceRef {
		if strings.TrimSpace(logical) == "" {
			result.add("receipt", "--"+label+" is required for "+policyID)
			return nil
		}
		ref, err := registeredEvidenceRef(repo, runDir, logical)
		if err != nil {
			result.add(logical, err.Error())
			return nil
		}
		return &ref
	}
	var bindings []registeredCheckEvidence
	switch policyID {
	case "qa.design-review.v2":
		if options.ComplexityStatistics != "" {
			result.add("receipt", "--complexity-statistics is not allowed for "+policyID)
		}
		caseSet := resolveBinding("qa-design-case-set", options.QADesignCaseSet)
		designReceipt := resolveBinding("qa-design-receipt", options.QADesignReceipt)
		if caseSet != nil && designReceipt != nil {
			bindings = []registeredCheckEvidence{{ID: "qa.design.case-set-binding", EvidenceRefs: []EvidenceRef{*caseSet, *designReceipt}}}
		}
	case "complexity.post-development.v2":
		if options.QADesignCaseSet != "" || options.QADesignReceipt != "" {
			result.add("receipt", "QA Design binding flags are not allowed for "+policyID)
		}
		statistics := resolveBinding("complexity-statistics", options.ComplexityStatistics)
		if statistics != nil {
			bindings = []registeredCheckEvidence{{ID: "complexity.statistics", EvidenceRefs: []EvidenceRef{*statistics}}}
		}
	default:
		if options.QADesignCaseSet != "" || options.QADesignReceipt != "" || options.ComplexityStatistics != "" {
			result.add("receipt", "role-specific check evidence is not accepted for "+policyID)
		}
	}
	return changedFiles, verification, bindings
}

func registeredCarryEvidence(repo, runDir, policyID string, options ReceiptRegisterOptions, result *Result) (*EvidenceRef, []registeredCarrySource) {
	if policyID != "carry.arbiter.v2" {
		if options.TransitionChain != "" || len(options.CarrySourceClosures) > 0 {
			result.add("receipt", "Carry static evidence is not accepted for this dispatch role")
		}
		return nil, nil
	}
	if strings.TrimSpace(options.TransitionChain) == "" {
		result.add("receipt", "--transition-chain is required for carry.arbiter.v2")
		return nil, nil
	}
	transition, err := registeredEvidenceRef(repo, runDir, options.TransitionChain)
	if err != nil {
		result.add(options.TransitionChain, err.Error())
		return nil, nil
	}
	if err := validateStandaloneCompositionProof(repo, runDir, "transition-chain.v1", options.WorkflowID, options.ChangeSnapshot, transition); err != nil {
		result.add(options.TransitionChain, err.Error())
		return nil, nil
	}
	if len(options.CarrySourceClosures) == 0 {
		result.add("receipt", "at least one --carry-source-closure is required for carry.arbiter.v2")
	}
	seen := map[string]bool{}
	byGate := map[string]registeredCarrySource{}
	for _, logical := range options.CarrySourceClosures {
		logical = strings.TrimSpace(logical)
		if logical == "" {
			result.add("receipt", "--carry-source-closure must name a run-relative closure path")
			continue
		}
		ref, refErr := registeredEvidenceRef(repo, runDir, logical)
		if refErr != nil {
			result.add(logical, refErr.Error())
			continue
		}
		data, readErr := os.ReadFile(resolvePath(runDir, ref.Path))
		if readErr != nil {
			result.add(logical, readErr.Error())
			continue
		}
		var closure EvidenceClosure
		if strictContractJSON(data, &closure) != nil || closure.WorkflowID != options.WorkflowID || !stringSet(postDevelopmentGateOrder)[closure.Gate] || closure.Verdict != "PASS" || strings.TrimSpace(closure.ChangeSnapshot) == "" {
			result.add(logical, "Carry source must be a same-workflow PASS closure for a fixed post-development gate")
			continue
		}
		gate := closure.Gate
		stage, role := sourceGateContract(gate)
		if normalizeStage(closure.Stage) != stage || closure.RootRole != role {
			result.add(logical, "Carry source closure does not match the fixed gate contract")
			continue
		}
		if err := verifyClosure(ArtifactOptions{Root: repo, RunDir: runDir, WorkflowID: options.WorkflowID}, runDir, closure); err != nil {
			result.add(logical, err.Error())
			continue
		}
		if seen[gate] {
			result.add("receipt", "duplicate Carry source closure for gate: "+gate)
			continue
		}
		seen[gate] = true
		byGate[gate] = registeredCarrySource{Gate: gate, SourceSnapshot: closure.ChangeSnapshot, SourceGateEvidence: ref}
	}
	sources := make([]registeredCarrySource, 0, len(byGate))
	for _, gate := range postDevelopmentGateOrder {
		if source, ok := byGate[gate]; ok {
			sources = append(sources, source)
		}
	}
	return &transition, sources
}

func validateRegisteredComplexityStatistics(repo, runDir, policyID string, checkEvidence []registeredCheckEvidence, workflowID, snapshot string, result *Result, where string) {
	if policyID != "complexity.post-development.v2" {
		return
	}
	var refs []EvidenceRef
	for _, binding := range checkEvidence {
		if binding.ID == "complexity.statistics" {
			refs = append(refs, binding.EvidenceRefs...)
		}
	}
	if len(refs) != 1 {
		result.add(where, "complexity.statistics requires exactly one CLI-generated statistics report")
		return
	}
	if err := validateComplexityStatisticsEvidence(repo, runDir, workflowID, snapshot, refs[0]); err != nil {
		result.add(refs[0].Path, err.Error())
	}
}

func registeredEvidenceRef(repo, runDir, logical string) (EvidenceRef, error) {
	path, err := safeEvidencePath(runDir, logical)
	if err != nil {
		return EvidenceRef{}, err
	}
	logicalPath, err := logicalPathInRun(runDir, path)
	if err != nil || !restrictedEvidencePath(repo, runDir, logicalPath) {
		return EvidenceRef{}, fmt.Errorf("static evidence must be under the active run restricted directory")
	}
	if !isFile(path) {
		return EvidenceRef{}, fmt.Errorf("static evidence file does not exist")
	}
	return EvidenceRef{Path: slash(logicalPath), SHA256: sha256File(path)}, nil
}

func generatedDispatchTemplate(record dispatchRegistration) (FormalGateEvidence, error) {
	if record.ReviewPolicyID == "carry.arbiter.v2" {
		return generatedCarryTemplate(record)
	}
	return generatedReviewTemplate(record)
}

func generatedCarryTemplate(record dispatchRegistration) (FormalGateEvidence, error) {
	if record.TransitionChain == nil || len(record.CarrySources) == 0 {
		return FormalGateEvidence{}, fmt.Errorf("Carry template requires script-owned transition and source bindings")
	}
	decisions := make([]CarryDecision, 0, len(record.CarrySources))
	for _, source := range record.CarrySources {
		decisions = append(decisions, CarryDecision{
			Gate: source.Gate, SourceSnapshot: source.SourceSnapshot,
			SourceGateEvidence: source.SourceGateEvidence,
			Decision:           "PENDING", Reason: "PENDING",
		})
	}
	payloadBytes, err := json.Marshal(CarryPayload{
		ContextBundle:  EvidenceRef{Path: record.ContextBundle, SHA256: record.ContextSHA256},
		ReviewPolicyID: "carry.arbiter.v2", TransitionChain: *record.TransitionChain,
		Decisions: decisions,
	})
	if err != nil {
		return FormalGateEvidence{}, err
	}
	return FormalGateEvidence{
		SchemaVersion: 2, ArtifactRole: "CARRY_ARBITER",
		WorkflowID: record.WorkflowID, ChangeSnapshot: record.ChangeSnapshot,
		Gate: "qa-test-gate", Stage: "Carry", Verdict: "PENDING", Payload: payloadBytes,
	}, nil
}

func generatedReviewTemplate(record dispatchRegistration) (FormalGateEvidence, error) {
	policy, ok := policyByID(record.ReviewPolicyID)
	if !ok || policy.ArtifactRole == "" || policy.ArtifactRole == "CARRY_ARBITER" {
		return FormalGateEvidence{}, fmt.Errorf("review policy cannot generate a reviewer template")
	}
	evidence := map[string][]EvidenceRef{}
	for _, binding := range record.CheckEvidence {
		if _, duplicate := evidence[binding.ID]; duplicate {
			return FormalGateEvidence{}, fmt.Errorf("duplicate generated check evidence id: %s", binding.ID)
		}
		evidence[binding.ID] = append([]EvidenceRef{}, binding.EvidenceRefs...)
	}
	checks := make([]ReviewCheck, 0, len(policy.RequiredCheckIDs))
	for _, id := range policy.RequiredCheckIDs {
		checks = append(checks, ReviewCheck{
			ID: id, Status: "PENDING", Message: "PENDING",
			EvidenceRefs: append([]EvidenceRef{}, evidence[id]...), Findings: []Finding{},
		})
	}
	payload := ReviewerPayload{
		ContextBundle:  EvidenceRef{Path: record.ContextBundle, SHA256: record.ContextSHA256},
		ReviewPolicyID: record.ReviewPolicyID,
		Checks:         checks,
		ChangedFiles:   record.ChangedFiles,
		Verification:   record.Verification,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return FormalGateEvidence{}, err
	}
	return FormalGateEvidence{
		SchemaVersion: 2, ArtifactRole: policy.ArtifactRole,
		WorkflowID: record.WorkflowID, ChangeSnapshot: record.ChangeSnapshot,
		Gate: record.Gate, Stage: normalizeStage(record.Stage), Verdict: "PENDING",
		Payload: payloadBytes,
	}, nil
}

const QADesignSemanticValuesPerCase = 7

var qaDesignFieldLabels = []string{"Claim", "Source", "Action", "Oracle", "Failure signal", "Evidence", "Gap"}

func generatedQADesignTemplate(count int) []byte {
	var builder strings.Builder
	builder.WriteString("# QA Design\n\n")
	for index := 1; index <= count; index++ {
		fmt.Fprintf(&builder, "Case ID: CASE-%03d\n", index)
		for _, label := range qaDesignFieldLabels {
			builder.WriteString(label)
			builder.WriteString(": PENDING\n")
		}
		builder.WriteByte('\n')
	}
	return []byte(builder.String())
}

func ReceiptSubmit(options ReceiptSubmitOptions) (ReceiptSubmission, Result) {
	var result Result
	repo := cleanWorktree(options.Worktree)
	runDir, _, err := promptRunRelativePath(repo, options.Artifact)
	if err != nil {
		result.add(options.Artifact, err.Error())
		return ReceiptSubmission{}, result
	}
	artifactPath, err := runLocalReviewArtifactPath(repo, runDir, options.Artifact)
	if err != nil {
		result.add(options.Artifact, err.Error())
		return ReceiptSubmission{}, result
	}
	release, err := acquireReceiptRegistrationLock(repo, runDir)
	if err != nil {
		result.add("receipt", err.Error())
		return ReceiptSubmission{}, result
	}
	defer release()

	dispatchPath := filepath.Join(receiptProofDir(repo, runDir, "dispatch"), sha256Bytes([]byte(artifactPath))+".json")
	dispatch, ok := decodeDispatch(dispatchPath)
	if !ok || dispatch.Status != "open" || strings.TrimSpace(dispatch.ReceiptArtifact) != "" ||
		!samePath(resolvePath(repo, dispatch.ReviewArtifact), artifactPath) ||
		(!reviewJudgmentLifecycle(dispatch.Gate, dispatch.Stage) && !qaDesignLifecycle(dispatch.Gate, dispatch.Stage)) {
		result.add("receipt", "semantic submission requires the assigned artifact from one open review or QA Design dispatch")
		return ReceiptSubmission{}, result
	}
	var expected FormalGateEvidence
	var templateBytes []byte
	if qaDesignLifecycle(dispatch.Gate, dispatch.Stage) {
		templateBytes = generatedQADesignTemplate(dispatch.QACaseCount)
	} else {
		expected, err = generatedDispatchTemplate(dispatch)
		if err != nil {
			result.add("receipt", err.Error())
			return ReceiptSubmission{}, result
		}
		templateBytes, err = json.MarshalIndent(expected, "", "  ")
		if err != nil {
			result.add("receipt", err.Error())
			return ReceiptSubmission{}, result
		}
		templateBytes = append(templateBytes, '\n')
	}
	proofBytes, proofErr := os.ReadFile(resolvePath(repo, dispatch.ReviewTemplate))
	artifactBytes, artifactErr := os.ReadFile(artifactPath)
	if proofErr != nil || artifactErr != nil || sha256Bytes(proofBytes) != dispatch.ReviewTemplateSHA256 ||
		!bytes.Equal(proofBytes, templateBytes) || !bytes.Equal(artifactBytes, templateBytes) {
		result.add(options.Artifact, "assigned artifact is not the untouched CLI-generated semantic template")
		return ReceiptSubmission{}, result
	}

	var submittedBytes, finalizedBytes []byte
	if qaDesignLifecycle(dispatch.Gate, dispatch.Stage) {
		submittedBytes, err = composeQADesignSemanticSubmission(dispatch, options)
		if err == nil {
			finalizedBytes, err = finalizeGeneratedQADesign(repo, dispatch, submittedBytes)
		}
	} else if dispatch.ReviewPolicyID == "carry.arbiter.v2" {
		submittedBytes, err = composeCarrySemanticSubmission(expected, options)
		if err == nil {
			finalizedBytes, err = finalizeGeneratedCarry(repo, dispatch, submittedBytes)
		}
	} else {
		submittedBytes, err = composeReviewSemanticSubmission(expected, options)
		if err == nil {
			finalizedBytes, err = finalizeGeneratedReview(repo, dispatch, submittedBytes)
		}
	}
	if err != nil {
		result.add(options.Artifact, err.Error())
		return ReceiptSubmission{}, result
	}
	if reviewJudgmentLifecycle(dispatch.Gate, dispatch.Stage) {
		artifactOptions := ArtifactOptions{
			Root: repo, RunDir: runDir, File: relativePath(repo, artifactPath), Gate: dispatch.Gate,
			Stage: dispatch.Stage, WorkflowID: dispatch.WorkflowID, ChangeSnapshot: dispatch.ChangeSnapshot,
		}
		decodeArtifact(artifactOptions, finalizedBytes, &result)
		if !result.OK() {
			return ReceiptSubmission{}, result
		}
	}
	submittedDispatch := dispatch
	submittedDispatch.SemanticSubmissionSHA = sha256Bytes(submittedBytes)
	dispatchBytes, err := json.MarshalIndent(submittedDispatch, "", "  ")
	if err != nil {
		result.add(options.Artifact, err.Error())
		return ReceiptSubmission{}, result
	}
	dispatchBytes = append(dispatchBytes, '\n')
	if err := writeFileAtomic(artifactPath, submittedBytes, 0o600); err != nil {
		result.add(options.Artifact, err.Error())
		return ReceiptSubmission{}, result
	}
	if err := writeFileAtomic(dispatchPath, dispatchBytes, 0o600); err != nil {
		_ = writeFileAtomic(artifactPath, templateBytes, 0o600)
		result.add(options.Artifact, "cannot commit semantic submission proof: "+err.Error())
		return ReceiptSubmission{}, result
	}
	return ReceiptSubmission{
		Artifact: relativePath(repo, artifactPath), ArtifactRole: expected.ArtifactRole,
		ArtifactSha256: sha256Bytes(submittedBytes), Status: "submitted",
	}, result
}

func composeQADesignSemanticSubmission(record dispatchRegistration, options ReceiptSubmitOptions) ([]byte, error) {
	if len(options.Checks) != 0 || len(options.Findings) != 0 || len(options.Locations) != 0 || len(options.CarryDecisions) != 0 {
		return nil, fmt.Errorf("QA Design submission accepts only ordered case values")
	}
	if len(options.DesignCases) != record.QACaseCount {
		return nil, fmt.Errorf("QA Design semantics must cover every generated case exactly once")
	}
	values := make([][]string, record.QACaseCount)
	for _, semantic := range options.DesignCases {
		index := semantic.Position - 1
		if index < 0 || index >= record.QACaseCount {
			return nil, fmt.Errorf("QA Design case position is unknown: %d", semantic.Position)
		}
		if values[index] != nil {
			return nil, fmt.Errorf("QA Design case position is duplicated: %d", semantic.Position)
		}
		if len(semantic.Values) != len(qaDesignFieldLabels) {
			return nil, fmt.Errorf("QA Design case position %d requires exactly %d semantic values", semantic.Position, len(qaDesignFieldLabels))
		}
		values[index] = append([]string{}, semantic.Values...)
		for valueIndex, value := range values[index] {
			if strings.TrimSpace(value) == "" || strings.TrimSpace(value) == "PENDING" || strings.ContainsAny(value, "\r\n") {
				return nil, fmt.Errorf("QA Design semantic value %d is invalid at case position %d", valueIndex+1, semantic.Position)
			}
		}
	}
	var builder strings.Builder
	builder.WriteString("# QA Design\n\n")
	for index, semanticValues := range values {
		if semanticValues == nil {
			return nil, fmt.Errorf("QA Design case position is missing: %d", index+1)
		}
		fmt.Fprintf(&builder, "Case ID: CASE-%03d\n", index+1)
		for fieldIndex, label := range qaDesignFieldLabels {
			fmt.Fprintf(&builder, "%s: %s\n", label, semanticValues[fieldIndex])
		}
		builder.WriteByte('\n')
	}
	return []byte(builder.String()), nil
}

func composeReviewSemanticSubmission(expected FormalGateEvidence, options ReceiptSubmitOptions) ([]byte, error) {
	if len(options.CarryDecisions) != 0 || len(options.DesignCases) != 0 {
		return nil, fmt.Errorf("reviewer submission accepts only checks, findings, and locations")
	}
	var payload ReviewerPayload
	if err := strictContractJSON(expected.Payload, &payload); err != nil {
		return nil, err
	}
	policy, ok := policyByID(payload.ReviewPolicyID)
	if !ok {
		return nil, fmt.Errorf("review policy is unknown")
	}
	if len(options.Checks) != len(payload.Checks) {
		return nil, fmt.Errorf("semantic checks must cover every generated check exactly once")
	}
	allowedNA := stringSet(policy.AllowedNotApplicableCheckIDs)
	seenChecks := make([]bool, len(payload.Checks))
	for _, semantic := range options.Checks {
		index := semantic.Position - 1
		if index < 0 || index >= len(payload.Checks) {
			return nil, fmt.Errorf("semantic check position is unknown: %d", semantic.Position)
		}
		if seenChecks[index] {
			return nil, fmt.Errorf("semantic check position is duplicated: %d", semantic.Position)
		}
		seenChecks[index] = true
		if !map[string]bool{"PASS": true, "REVIEW": true, "FAIL": true, "BLOCKED": true, "NOT_APPLICABLE": true}[semantic.Status] {
			return nil, fmt.Errorf("semantic check status is invalid at position %d", semantic.Position)
		}
		if semantic.Status == "NOT_APPLICABLE" && !allowedNA[payload.Checks[index].ID] {
			return nil, fmt.Errorf("NOT_APPLICABLE is not allowed at semantic check position %d", semantic.Position)
		}
		if strings.TrimSpace(semantic.Message) == "" || strings.TrimSpace(semantic.Message) == "PENDING" {
			return nil, fmt.Errorf("semantic check message is missing at position %d", semantic.Position)
		}
		payload.Checks[index].Status = semantic.Status
		payload.Checks[index].Message = semantic.Message
		payload.Checks[index].Findings = []Finding{}
	}
	for index, semantic := range options.Findings {
		checkIndex := semantic.CheckPosition - 1
		if checkIndex < 0 || checkIndex >= len(payload.Checks) {
			return nil, fmt.Errorf("finding %d names an unknown semantic check position", index+1)
		}
		if strings.TrimSpace(semantic.Message) == "" {
			return nil, fmt.Errorf("finding message is missing at position %d", index+1)
		}
		payload.Checks[checkIndex].Findings = append(payload.Checks[checkIndex].Findings, Finding{Message: semantic.Message, Locations: []Location{}})
	}
	for index, semantic := range options.Locations {
		findingIndex := semantic.FindingPosition - 1
		if findingIndex < 0 || findingIndex >= len(options.Findings) {
			return nil, fmt.Errorf("location %d names an unknown finding position", index+1)
		}
		finding := options.Findings[findingIndex]
		checkIndex := finding.CheckPosition - 1
		location := Location{Path: semantic.Path, StartLine: semantic.StartLine, EndLine: semantic.EndLine}
		var locationResult Result
		validateFindings([]Finding{{Message: finding.Message, Locations: []Location{location}}}, &locationResult, "semantic location")
		if !locationResult.OK() {
			return nil, fmt.Errorf("location %d is invalid: %s", index+1, locationResult.Failures[0].Message)
		}
		findingOffset := 0
		for prior := 0; prior < findingIndex; prior++ {
			if options.Findings[prior].CheckPosition == finding.CheckPosition {
				findingOffset++
			}
		}
		payload.Checks[checkIndex].Findings[findingOffset].Locations = append(payload.Checks[checkIndex].Findings[findingOffset].Locations, location)
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	expected.Payload = payloadBytes
	encoded, err := json.MarshalIndent(expected, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func composeCarrySemanticSubmission(expected FormalGateEvidence, options ReceiptSubmitOptions) ([]byte, error) {
	if len(options.Checks) != 0 || len(options.Findings) != 0 || len(options.Locations) != 0 || len(options.DesignCases) != 0 {
		return nil, fmt.Errorf("Carry submission accepts only per-gate decisions and reasons")
	}
	var payload CarryPayload
	if err := strictContractJSON(expected.Payload, &payload); err != nil {
		return nil, err
	}
	if len(options.CarryDecisions) != len(payload.Decisions) {
		return nil, fmt.Errorf("Carry semantics must cover every generated gate exactly once")
	}
	seenGates := make([]bool, len(payload.Decisions))
	for _, semantic := range options.CarryDecisions {
		index := semantic.GatePosition - 1
		if index < 0 || index >= len(payload.Decisions) {
			return nil, fmt.Errorf("Carry gate position is unknown: %d", semantic.GatePosition)
		}
		if seenGates[index] {
			return nil, fmt.Errorf("Carry gate position is duplicated: %d", semantic.GatePosition)
		}
		seenGates[index] = true
		if !map[string]bool{"ACCEPT_CARRY": true, "RERUN_REQUIRED": true, "BLOCKED": true}[semantic.Decision] {
			return nil, fmt.Errorf("Carry decision is invalid at gate position %d", semantic.GatePosition)
		}
		if strings.TrimSpace(semantic.Reason) == "" || strings.TrimSpace(semantic.Reason) == "PENDING" {
			return nil, fmt.Errorf("Carry reason is missing at gate position %d", semantic.GatePosition)
		}
		payload.Decisions[index].Decision = semantic.Decision
		payload.Decisions[index].Reason = semantic.Reason
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	expected.Payload = payloadBytes
	encoded, err := json.MarshalIndent(expected, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(encoded, '\n'), nil
}

func finalizeGeneratedQADesign(repo string, record dispatchRegistration, designBytes []byte) ([]byte, error) {
	expected := generatedQADesignTemplate(record.QACaseCount)
	proofBytes, err := os.ReadFile(resolvePath(repo, record.ReviewTemplate))
	if err != nil || sha256Bytes(proofBytes) != record.ReviewTemplateSHA256 || !bytes.Equal(proofBytes, expected) {
		return nil, fmt.Errorf("script-generated QA Design template proof is invalid")
	}
	if !utf8.Valid(designBytes) || bytes.Contains(designBytes, []byte{'\r'}) || len(designBytes) == 0 || designBytes[len(designBytes)-1] != '\n' {
		return nil, fmt.Errorf("QA Design must preserve the generated UTF-8 line structure")
	}
	lines := strings.Split(string(designBytes), "\n")
	wantLines := 2 + record.QACaseCount*9
	if len(lines) != wantLines+1 || lines[0] != "# QA Design" || lines[1] != "" || lines[len(lines)-1] != "" {
		return nil, fmt.Errorf("AI modified the script-owned QA Design structure")
	}
	position := 2
	for index := 1; index <= record.QACaseCount; index++ {
		if lines[position] != fmt.Sprintf("Case ID: CASE-%03d", index) {
			return nil, fmt.Errorf("AI modified the script-owned QA case catalog")
		}
		position++
		for _, label := range qaDesignFieldLabels {
			prefix := label + ": "
			if !strings.HasPrefix(lines[position], prefix) {
				return nil, fmt.Errorf("AI modified the script-owned QA Design field catalog")
			}
			value := strings.TrimSpace(strings.TrimPrefix(lines[position], prefix))
			if value == "" || value == "PENDING" {
				return nil, fmt.Errorf("QA designer must fill every semantic case slot")
			}
			position++
		}
		if lines[position] != "" {
			return nil, fmt.Errorf("AI modified the script-owned QA Design separators")
		}
		position++
	}
	return designBytes, nil
}

func finalizeGeneratedReview(repo string, record dispatchRegistration, reviewerBytes []byte) ([]byte, error) {
	expected, err := generatedReviewTemplate(record)
	if err != nil {
		return nil, err
	}
	expectedBytes, err := json.MarshalIndent(expected, "", "  ")
	if err != nil {
		return nil, err
	}
	expectedBytes = append(expectedBytes, '\n')
	templatePath := resolvePath(repo, record.ReviewTemplate)
	templateBytes, err := os.ReadFile(templatePath)
	if err != nil || sha256Bytes(templateBytes) != record.ReviewTemplateSHA256 || !bytes.Equal(templateBytes, expectedBytes) {
		return nil, fmt.Errorf("script-generated review template proof is invalid")
	}
	var actual FormalGateEvidence
	if err := strictContractJSON(reviewerBytes, &actual); err != nil {
		return nil, fmt.Errorf("review judgment JSON is invalid: %w", err)
	}
	if actual.SchemaVersion != expected.SchemaVersion || actual.ArtifactRole != expected.ArtifactRole || actual.WorkflowID != expected.WorkflowID || actual.ChangeSnapshot != expected.ChangeSnapshot || actual.Gate != expected.Gate || normalizeStage(actual.Stage) != normalizeStage(expected.Stage) || actual.Verdict != "PENDING" {
		return nil, fmt.Errorf("AI modified script-owned reviewer envelope fields")
	}
	var actualPayload, expectedPayload ReviewerPayload
	if err := strictContractJSON(actual.Payload, &actualPayload); err != nil {
		return nil, fmt.Errorf("review judgment payload is invalid: %w", err)
	}
	if err := strictContractJSON(expected.Payload, &expectedPayload); err != nil {
		return nil, err
	}
	if actualPayload.ContextBundle != expectedPayload.ContextBundle || actualPayload.ReviewPolicyID != expectedPayload.ReviewPolicyID || !reflect.DeepEqual(actualPayload.ChangedFiles, expectedPayload.ChangedFiles) || !reflect.DeepEqual(actualPayload.Verification, expectedPayload.Verification) || len(actualPayload.Checks) != len(expectedPayload.Checks) {
		return nil, fmt.Errorf("AI modified script-owned reviewer payload fields")
	}
	priority := map[string]int{"PASS": 0, "NOT_APPLICABLE": 0, "REVIEW": 1, "FAIL": 2, "BLOCKED": 3}
	aggregate := "PASS"
	checks := make([]ReviewCheck, 0, len(actualPayload.Checks))
	for i := range actualPayload.Checks {
		actualCheck, expectedCheck := actualPayload.Checks[i], expectedPayload.Checks[i]
		if actualCheck.ID != expectedCheck.ID || !reflect.DeepEqual(actualCheck.EvidenceRefs, expectedCheck.EvidenceRefs) {
			return nil, fmt.Errorf("AI modified script-owned reviewer check catalog or evidence binding")
		}
		if _, ok := priority[actualCheck.Status]; !ok || strings.TrimSpace(actualCheck.Message) == "" || actualCheck.Message == "PENDING" {
			return nil, fmt.Errorf("reviewer must fill every semantic status and message slot")
		}
		if actualCheck.Findings == nil {
			return nil, fmt.Errorf("reviewer must provide each semantic findings array")
		}
		if priority[actualCheck.Status] > priority[aggregate] {
			aggregate = actualCheck.Status
		}
		checks = append(checks, ReviewCheck{
			ID: expectedCheck.ID, Status: actualCheck.Status, Message: actualCheck.Message,
			EvidenceRefs: append([]EvidenceRef{}, expectedCheck.EvidenceRefs...), Findings: actualCheck.Findings,
		})
	}
	expectedPayload.Checks = checks
	payloadBytes, err := json.Marshal(expectedPayload)
	if err != nil {
		return nil, err
	}
	expected.Verdict = aggregate
	expected.Payload = payloadBytes
	finalBytes, err := json.MarshalIndent(expected, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(finalBytes, '\n'), nil
}

func finalizeGeneratedCarry(repo string, record dispatchRegistration, reviewerBytes []byte) ([]byte, error) {
	expected, err := generatedCarryTemplate(record)
	if err != nil {
		return nil, err
	}
	expectedBytes, err := json.MarshalIndent(expected, "", "  ")
	if err != nil {
		return nil, err
	}
	expectedBytes = append(expectedBytes, '\n')
	templateBytes, err := os.ReadFile(resolvePath(repo, record.ReviewTemplate))
	if err != nil || sha256Bytes(templateBytes) != record.ReviewTemplateSHA256 || !bytes.Equal(templateBytes, expectedBytes) {
		return nil, fmt.Errorf("script-generated Carry template proof is invalid")
	}
	var actual FormalGateEvidence
	if err := strictContractJSON(reviewerBytes, &actual); err != nil {
		return nil, fmt.Errorf("Carry judgment JSON is invalid: %w", err)
	}
	if actual.SchemaVersion != expected.SchemaVersion || actual.ArtifactRole != expected.ArtifactRole || actual.WorkflowID != expected.WorkflowID || actual.ChangeSnapshot != expected.ChangeSnapshot || actual.Gate != expected.Gate || normalizeStage(actual.Stage) != normalizeStage(expected.Stage) || actual.Verdict != "PENDING" {
		return nil, fmt.Errorf("AI modified script-owned Carry envelope fields")
	}
	var actualPayload, expectedPayload CarryPayload
	if err := strictContractJSON(actual.Payload, &actualPayload); err != nil {
		return nil, fmt.Errorf("Carry judgment payload is invalid: %w", err)
	}
	if err := strictContractJSON(expected.Payload, &expectedPayload); err != nil {
		return nil, err
	}
	if actualPayload.ContextBundle != expectedPayload.ContextBundle || actualPayload.ReviewPolicyID != expectedPayload.ReviewPolicyID || actualPayload.TransitionChain != expectedPayload.TransitionChain || len(actualPayload.Decisions) != len(expectedPayload.Decisions) {
		return nil, fmt.Errorf("AI modified script-owned Carry payload fields")
	}
	decisions := make([]CarryDecision, 0, len(actualPayload.Decisions))
	for i := range actualPayload.Decisions {
		actualDecision, expectedDecision := actualPayload.Decisions[i], expectedPayload.Decisions[i]
		if actualDecision.Gate != expectedDecision.Gate || actualDecision.SourceSnapshot != expectedDecision.SourceSnapshot || actualDecision.SourceGateEvidence != expectedDecision.SourceGateEvidence {
			return nil, fmt.Errorf("AI modified script-owned Carry decision bindings")
		}
		if !map[string]bool{"ACCEPT_CARRY": true, "RERUN_REQUIRED": true, "BLOCKED": true}[actualDecision.Decision] || strings.TrimSpace(actualDecision.Reason) == "" || actualDecision.Reason == "PENDING" {
			return nil, fmt.Errorf("Carry reviewer must fill every semantic decision and reason slot")
		}
		expectedDecision.Decision = actualDecision.Decision
		expectedDecision.Reason = actualDecision.Reason
		decisions = append(decisions, expectedDecision)
	}
	expectedPayload.Decisions = decisions
	payloadBytes, err := json.Marshal(expectedPayload)
	if err != nil {
		return nil, err
	}
	expected.Verdict = carryAggregateVerdict(decisions)
	expected.Payload = payloadBytes
	finalBytes, err := json.MarshalIndent(expected, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(finalBytes, '\n'), nil
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
		validateQADesignReviewPromptBinding(repo, runDir, dispatch.ReviewPolicyID, dispatch.PromptArtifact, dispatch.CheckEvidence, dispatch.WorkflowID, dispatch.ChangeSnapshot, &result)
		if !result.OK() {
			return ReceiptFinalizeOutput{}, result
		}
	}
	validateRegisteredComplexityStatistics(repo, runDir, dispatch.ReviewPolicyID, dispatch.CheckEvidence, dispatch.WorkflowID, dispatch.ChangeSnapshot, &result, "receipt")
	if !result.OK() {
		return ReceiptFinalizeOutput{}, result
	}
	release, err := acquireReceiptRegistrationLock(repo, runDir)
	if err != nil {
		result.add("receipt", err.Error())
		return ReceiptFinalizeOutput{}, result
	}
	defer release()
	lockedDispatch, ok := decodeDispatch(dispatchPath)
	if !ok || lockedDispatch.DispatchID != dispatch.DispatchID || lockedDispatch.Status != "open" || strings.TrimSpace(lockedDispatch.ReceiptArtifact) != "" {
		result.add("receipt", "UNPROVEN receipt finalization lost its open dispatch registration")
		return ReceiptFinalizeOutput{}, result
	}
	dispatch = lockedDispatch
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
	if reviewJudgmentLifecycle(dispatch.Gate, dispatch.Stage) || qaDesignLifecycle(dispatch.Gate, dispatch.Stage) {
		if !isSHA256(dispatch.SemanticSubmissionSHA) || dispatch.SemanticSubmissionSHA != sha256Bytes(artifactBytes) {
			result.add(options.Artifact, "review or QA Design output requires CLI semantic submission before finalization")
			return ReceiptFinalizeOutput{}, result
		}
	}
	if dispatch.ReviewTemplate != "" {
		if qaDesignLifecycle(dispatch.Gate, dispatch.Stage) {
			artifactBytes, err = finalizeGeneratedQADesign(repo, dispatch, artifactBytes)
		} else if dispatch.ReviewPolicyID == "carry.arbiter.v2" {
			artifactBytes, err = finalizeGeneratedCarry(repo, dispatch, artifactBytes)
		} else {
			artifactBytes, err = finalizeGeneratedReview(repo, dispatch, artifactBytes)
		}
		if err != nil {
			result.add(options.Artifact, err.Error())
			return ReceiptFinalizeOutput{}, result
		}
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

func validateQADesignReviewPromptBinding(root, runDir, policyID, promptArtifact string, checkEvidence []registeredCheckEvidence, workflowID, changeSnapshot string, result *Result) {
	if policyID != "qa.design-review.v2" {
		return
	}
	data, err := os.ReadFile(resolvePath(root, promptArtifact))
	if err != nil {
		result.add(promptArtifact, "cannot read QA Design Review prompt")
		return
	}
	target := strictDispatchPromptFields(string(data))["current diff or proposed change"]
	targetPath := resolvePath(root, target)
	if requireAbsPathUnderRunDir(runDir, "QA Design Review prompt target", targetPath) != nil || !isFile(targetPath) {
		result.add(promptArtifact, "QA Design Review prompt must target an existing case set under the active run restricted directory")
		return
	}
	var refs []EvidenceRef
	for _, binding := range checkEvidence {
		if binding.ID == "qa.design.case-set-binding" {
			refs = append(refs, binding.EvidenceRefs...)
		}
	}
	caseSet, _ := validateQADesignCaseSetBindingRefs(ArtifactOptions{Root: root, File: promptArtifact, RunDir: runDir}, runDir, workflowID, changeSnapshot, refs, result)
	if caseSet == nil {
		return
	}
	if !samePath(targetPath, resolvePath(runDir, caseSet.Path)) || sha256File(targetPath) != caseSet.SHA256 {
		result.add(promptArtifact, "QA Design Review prompt target must exactly match the bound case set")
	}
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
