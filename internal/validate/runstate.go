package validate

import (
	"os"
	"path/filepath"

	"formal-gates/internal/lifecycle"
)

type Finding struct {
	ID                string              `json:"id,omitempty"`
	Severity          string              `json:"severity,omitempty"`
	SeverityRationale string              `json:"severityRationale,omitempty"`
	Category          string              `json:"category,omitempty"`
	Requirement       string              `json:"requirement,omitempty"`
	NormalPath        string              `json:"normalPath,omitempty"`
	ActualError       string              `json:"actualError,omitempty"`
	DeferralImpact    string              `json:"deferralImpact,omitempty"`
	Verification      FindingVerification `json:"verification,omitempty"`
	Message           string              `json:"message"`
	Locations         []string            `json:"locations,omitempty"`
}

// FindingVerification is intentionally per finding. One batch-level operator
// paragraph cannot stand in for independent scope, severity, causality,
// reproduction, and (for Start Readiness) classification checks.
type FindingVerification struct {
	Scope          string `json:"scope,omitempty"`
	Severity       string `json:"severity,omitempty"`
	Causality      string `json:"causality,omitempty"`
	Reproduction   string `json:"reproduction,omitempty"`
	Classification string `json:"classification,omitempty"`
}

type QACase struct {
	SharedSteps []string `json:"sharedSteps,omitempty"`
	ID          string   `json:"id"`
	Mode        string   `json:"mode"`
	Description string   `json:"description"`
	Procedure   string   `json:"procedure"`
	Oracle      string   `json:"oracle"`
	// Test 是白盒用例对应的测试引用 = "<文件路径>::<函数名>"：文件路径定位到
	// 交付测试代码所在文件、函数名定位到该文件里的测试，两者都是不透明字符串、CLI 不解析
	// 代码内容。用例文档自包含——读文档即知该测试在哪个文件、叫什么。CLI 记录时只校验引用
	// 非空、且同一引用不被两条白盒用例共用（一个测试实现一个用例）；存在性与对应性由
	// qa-review（读代码核对）与 qa-execution（实际运行）验证，使"测 A 的测试给 B 用例标
	// PASS"可被发现。黑盒用例不需要（黑盒执行实际使用产品、无结构测试绑定）。
	Test string `json:"test,omitempty"`
	// AcceptanceCriteria are the case's primary AC bindings. Additional bindings
	// are duplicate evidence only and never change split responsibility.
	AcceptanceCriteria           []string `json:"acceptanceCriteria,omitempty"`
	AdditionalAcceptanceCriteria []string `json:"additionalAcceptanceCriteria,omitempty"`
	// ReviewStatus 由 qa-review 轮置位（PASS/FAIL/PENDING）。PENDING 之外还有一条
	// 置为 PASS 的路径：ApprovedSource（见下）。
	ReviewStatus string `json:"reviewStatus"`
	// ApprovedSource retains prior-run approval provenance; changed cases now
	// always receive an independent review before approval.

	ApprovedSource string `json:"approvedSource,omitempty"`
	// SuggestionIDs binds suggestion-approved additions or revisions to the
	// exact P2/P3 records absorbed by that QA Design round.
	SuggestionIDs []string `json:"suggestionIds,omitempty"`
}

func RunDir(root, runID string) string {
	return filepath.Join(lifecycle.CleanRoot(root), ".gates", "tmp", runID)
}

func RunStatePath(root, runID string) string {
	return filepath.Join(RunDir(root, runID), "state.json")
}

func RunSummaryPath(root, runID string) string {
	return filepath.Join(lifecycle.CleanRoot(root), ".gates", "results", runID+".json")
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
