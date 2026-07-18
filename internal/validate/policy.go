package validate

import (
	"encoding/json"
	"sort"
)

type BuiltInPolicy struct {
	SchemaVersion            int              `json:"schemaVersion"`
	PostDevelopmentGateOrder []string         `json:"postDevelopmentGateOrder"`
	ArtifactPolicies         []ArtifactPolicy `json:"artifactPolicies"`
}

type ArtifactPolicy struct {
	ID                           string         `json:"id"`
	ArtifactRole                 string         `json:"artifactRole"`
	Gate                         string         `json:"gate"`
	Stage                        string         `json:"stage"`
	Flow                         string         `json:"flow"`
	Prerequisites                []PolicyPrereq `json:"prerequisites"`
	RequiredCheckIDs             []string       `json:"requiredCheckIds"`
	AllowedNotApplicableCheckIDs []string       `json:"allowedNotApplicableCheckIds"`
	ReceiptRequired              bool           `json:"receiptRequired"`
	ChangedFilesRequired         bool           `json:"changedFilesRequired"`
	VerificationRequired         bool           `json:"verificationRequired"`
	Mechanical                   bool           `json:"mechanical"`
}

type PolicyPrereq struct {
	Gate  string `json:"gate"`
	Stage string `json:"stage"`
	Flow  string `json:"flow"`
}

var commonReviewChecks = []string{"review.prompt-fields", "review.prompt-semantics"}

var qaDesignReviewChecks = []string{
	"qa.design.requirement-coverage", "qa.design.executability", "qa.design.oracles",
	"qa.design.evidence-binding", "qa.design.independence", "qa.design.case-set-binding",
}

var gateCheckIDs = map[string][]string{
	"complexity-gate": {
		"complexity.statistics", "complexity.diff-shape", "complexity.impact-surface",
		"complexity.public-config-surface", "complexity.new-concepts",
		"complexity.minimum-sufficient", "complexity.shrink-opportunities",
	},
	"architecture-health-gate": {
		"architecture.boundaries", "architecture.ownership", "architecture.public-surface",
		"architecture.state-lifecycle", "architecture.dependencies",
		"architecture.failure-semantics", "architecture.performance", "architecture.decoupling",
	},
	"code-quality-gate": {
		"code-quality.correctness", "code-quality.maintainability", "code-quality.performance",
		"code-quality.test-quality", "code-quality.dead-code", "code-quality.overfitting",
		"code-quality.validation-encoding", "code-quality.verification", "code-quality.residual-risk",
	},
}

func Policy() BuiltInPolicy {
	checks := func(gate string) []string {
		return append(append([]string{}, commonReviewChecks...), gateCheckIDs[gate]...)
	}
	policies := []ArtifactPolicy{
		{
			ID: "requirements.pass.v2", ArtifactRole: "REQUIREMENTS_PASS",
			Gate: "requirements-clarification-gate", Stage: "", Flow: "requirements",
			Prerequisites: []PolicyPrereq{}, RequiredCheckIDs: []string{},
			AllowedNotApplicableCheckIDs: []string{},
		},
		{
			ID: "qa.design-review.v2", ArtifactRole: "QA_REVIEW", Gate: "qa-test-gate",
			Stage: "Design Review", Flow: "pre-development",
			Prerequisites:                []PolicyPrereq{{Gate: "requirements-clarification-gate", Stage: "", Flow: "requirements"}},
			RequiredCheckIDs:             append(append([]string{}, commonReviewChecks...), qaDesignReviewChecks...),
			AllowedNotApplicableCheckIDs: []string{}, ReceiptRequired: true,
		},
		{
			ID: "carry.arbiter.v2", ArtifactRole: "CARRY_ARBITER", Gate: "qa-test-gate",
			Stage: "Carry", Flow: "carry", Prerequisites: []PolicyPrereq{},
			RequiredCheckIDs: []string{}, AllowedNotApplicableCheckIDs: []string{}, ReceiptRequired: true,
		},
		{
			ID: "qa.execution.v2", ArtifactRole: "QA_EXECUTION", Gate: "qa-test-gate",
			Stage: "Execution", Flow: "post-development", Prerequisites: []PolicyPrereq{},
			RequiredCheckIDs: []string{}, AllowedNotApplicableCheckIDs: []string{},
			ChangedFilesRequired: true, VerificationRequired: true, Mechanical: true,
		},
		{
			ID: "complexity.start-readiness.v2", ArtifactRole: "COMPLEXITY_REVIEW",
			Gate: "complexity-gate", Stage: "", Flow: "start-readiness",
			Prerequisites:                []PolicyPrereq{{Gate: "requirements-clarification-gate", Stage: "", Flow: "requirements"}},
			RequiredCheckIDs:             checks("complexity-gate"),
			AllowedNotApplicableCheckIDs: []string{"complexity.statistics"}, ReceiptRequired: true,
		},
		{
			ID: "complexity.post-development.v2", ArtifactRole: "COMPLEXITY_REVIEW",
			Gate: "complexity-gate", Stage: "", Flow: "post-development",
			Prerequisites:    []PolicyPrereq{{Gate: "qa-test-gate", Stage: "Execution", Flow: "post-development"}},
			RequiredCheckIDs: checks("complexity-gate"), AllowedNotApplicableCheckIDs: []string{},
			ReceiptRequired: true, ChangedFilesRequired: true, VerificationRequired: true,
		},
		{
			ID: "architecture.start-readiness.v2", ArtifactRole: "ARCHITECTURE_REVIEW",
			Gate: "architecture-health-gate", Stage: "", Flow: "start-readiness",
			Prerequisites: []PolicyPrereq{
				{Gate: "requirements-clarification-gate", Stage: "", Flow: "requirements"},
				{Gate: "complexity-gate", Stage: "", Flow: "start-readiness"},
			},
			RequiredCheckIDs:             checks("architecture-health-gate"),
			AllowedNotApplicableCheckIDs: []string{}, ReceiptRequired: true,
		},
		{
			ID: "architecture.post-development.v2", ArtifactRole: "ARCHITECTURE_REVIEW",
			Gate: "architecture-health-gate", Stage: "", Flow: "post-development",
			Prerequisites: []PolicyPrereq{
				{Gate: "qa-test-gate", Stage: "Execution", Flow: "post-development"},
				{Gate: "complexity-gate", Stage: "", Flow: "post-development"},
			},
			RequiredCheckIDs:             checks("architecture-health-gate"),
			AllowedNotApplicableCheckIDs: []string{}, ReceiptRequired: true,
			ChangedFilesRequired: true, VerificationRequired: true,
		},
		{
			ID: "code-quality.post-development.v2", ArtifactRole: "CODE_QUALITY_REVIEW",
			Gate: "code-quality-gate", Stage: "", Flow: "post-development",
			Prerequisites: []PolicyPrereq{
				{Gate: "qa-test-gate", Stage: "Execution", Flow: "post-development"},
				{Gate: "complexity-gate", Stage: "", Flow: "post-development"},
				{Gate: "architecture-health-gate", Stage: "", Flow: "post-development"},
			},
			RequiredCheckIDs:             checks("code-quality-gate"),
			AllowedNotApplicableCheckIDs: []string{}, ReceiptRequired: true,
			ChangedFilesRequired: true, VerificationRequired: true,
		},
		{
			ID: "final-execution.v2", ArtifactRole: "FINAL_EXECUTION", Gate: "qa-test-gate",
			Stage: "FinalExecution", Flow: "finalization",
			Prerequisites: []PolicyPrereq{
				{Gate: "qa-test-gate", Stage: "Execution", Flow: "post-development"},
				{Gate: "complexity-gate", Stage: "", Flow: "post-development"},
				{Gate: "architecture-health-gate", Stage: "", Flow: "post-development"},
				{Gate: "code-quality-gate", Stage: "", Flow: "post-development"},
			},
			RequiredCheckIDs: []string{}, AllowedNotApplicableCheckIDs: []string{}, Mechanical: true,
		},
	}
	sort.Slice(policies, func(i, j int) bool { return policies[i].ID < policies[j].ID })
	return BuiltInPolicy{
		SchemaVersion:            2,
		PostDevelopmentGateOrder: append([]string{}, postDevelopmentGateOrder...),
		ArtifactPolicies:         policies,
	}
}

func PolicyJSON(policy BuiltInPolicy) ([]byte, error) {
	return json.MarshalIndent(policy, "", "  ")
}

func policyByID(id string) (ArtifactPolicy, bool) {
	for _, policy := range Policy().ArtifactPolicies {
		if policy.ID == id {
			return policy, true
		}
	}
	return ArtifactPolicy{}, false
}

func admissionPolicy(gate, flow string) (ArtifactPolicy, bool) {
	for _, policy := range Policy().ArtifactPolicies {
		if policy.Gate == gate && policy.Flow == flow {
			return policy, true
		}
	}
	return ArtifactPolicy{}, false
}

func admissionFlow(mode string) (string, bool) {
	switch mode {
	case "", "formal", "post-development":
		return "post-development", true
	case "start-readiness":
		return "start-readiness", true
	case "pre-development":
		return "pre-development", true
	default:
		return "", false
	}
}

func recordingPolicy(gate, stage, mode string) (ArtifactPolicy, bool) {
	var selected ArtifactPolicy
	for _, policy := range Policy().ArtifactPolicies {
		if policy.Gate != gate || normalizeStage(policy.Stage) != normalizeStage(stage) || !recordModeMatchesPolicy(policy, mode) {
			continue
		}
		if selected.ID != "" {
			return ArtifactPolicy{}, false
		}
		selected = policy
	}
	return selected, selected.ID != ""
}

func recordingPolicyMismatchMessage(gate, stage string) string {
	var onlyFlow string
	for _, policy := range Policy().ArtifactPolicies {
		if policy.Gate != gate || normalizeStage(policy.Stage) != normalizeStage(stage) {
			continue
		}
		if onlyFlow != "" && onlyFlow != policy.Flow {
			return "recording mode and stage do not match an artifact policy"
		}
		onlyFlow = policy.Flow
	}
	if onlyFlow == "requirements" {
		return "recording accepts only --mode requirements"
	}
	return "recording mode and stage do not match an artifact policy"
}

func recordModeMatchesPolicy(policy ArtifactPolicy, mode string) bool {
	switch policy.Flow {
	case "requirements":
		return mode == "" || mode == "requirements"
	case "start-readiness":
		return mode == "start-readiness"
	case "post-development":
		return mode == "formal" || (mode == "" && policy.Stage == "")
	case "pre-development":
		return mode == "pre-development"
	case "finalization":
		return mode == "formal" && policy.Mechanical
	default:
		return false
	}
}

func fixedPolicy(role, gate, stage string) (ArtifactPolicy, bool) {
	for _, policy := range Policy().ArtifactPolicies {
		if policy.ArtifactRole == role && policy.Gate == gate && policy.Stage == stage {
			if role == "REQUIREMENTS_PASS" || policy.Mechanical {
				return policy, true
			}
		}
	}
	return ArtifactPolicy{}, false
}
