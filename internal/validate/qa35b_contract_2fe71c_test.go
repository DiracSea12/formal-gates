package validate

import (
	"path/filepath"

	"strings"
	"testing"
)

func qa35bContains(t *testing.T, path string, fragments ...string) {
	t.Helper()
	text := qa35bRead(t, path)
	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			t.Errorf("%s lacks contract %q", path, fragment)
		}
	}
}

func TestQA35BHumanWorkflowContractOwners(t *testing.T) {
	pkg := qa35bPackage(t)
	qa35bContains(t, filepath.Join(pkg, "SKILL.md"), "逐条定级理由", "同一条消息", "review-delta", "meaning preserved", "meaning changed", "AFFECTED", "FROZEN", "门审 P3 只作 Seal advisory")
	qa35bContains(t, filepath.Join(pkg, "references/formal-flow.md"), "同一", "缺少任一部分", "用户回复不能作为授权", "review-ready", "review-wave", "suggestion", "AFFECTED", "FULL", "有限 attempt", "review-delta", "requirement-impact", "state revision")
	qa35bContains(t, filepath.Join(pkg, "prompts/reviewer-base.md"), "每条 P0/P1", "逐", "P2", "正常", "acceptance-surface")
	qa35bContains(t, filepath.Join(pkg, "prompts/actions/start-readiness.md"), "实现偏好最高为 P2", "category")
	qa35bContains(t, filepath.Join(pkg, "prompts/actions/qa-design.md"), "先独立确定应验证的行为、分支和 oracle", "复用真正有效的覆盖")
	qa35bContains(t, filepath.Join(pkg, "prompts/actions/qa-execution.md"), "AFFECTED", "FULL", "RUNTIME_ERROR")
	// Gate assembly supplies this shared contract; do not require duplicated
	// clauses in each gate-specific document.
	qa35bContains(t, filepath.Join(pkg, "README.md"), "guard-preview", "冻结", "固定只跑受影响", "P2", "P3", "review-delta")
	qa35bContains(t, filepath.Join(pkg, "README_EN.md"), "guard-preview", "frozen", "fixed affected QA subset", "P2", "P3", "meaning-changing")
	qa35bContains(t, filepath.Join(pkg, "AGENTS.md"), "项目代理规则", "prompts/reviewer-base.md", "SKILL.md", "references/formal-flow.md")
}

func TestQA35BFutureEnginePlansInheritConvergenceContract(t *testing.T) {
	pkg := qa35bPackage(t)
	for _, name := range []string{"refactor-plan/final-implementation-draft.md", "refactor-plan/stage-3-5b-runtime-guardrails.md", "refactor-plan/incremental-seal-plan.md", "refactor-plan/route-matrix.md"} {
		qa35bContains(t, filepath.Join(pkg, name), "guard-preview", "Operator", "RETRY_EXHAUSTED", "FROZEN", "meaning-changed", "P3", "NOT_INSTALLED")
	}
	qa35bContract(t, filepath.Join(pkg, "refactor-plan/stage-3-5b-runtime-guardrails.md"),
		"不判断语义真假、根因、严重度或重复", "已核实 finding 的处置仍不唯一时才 Ask", "产品/技术审 confirm/dismiss 规则保持",
		"白盒最终 FROZEN case-set 绑定最终含测试快照后才 review-ready", "COLLECTING 未收齐或核实未完成时不建立返修",
		"门审 P2 carry 不复审，门审 P3 只作 Seal advisory", "QA Review 可选 P2/P3 以 suggestion ID 登记采纳/延期/不采纳及理由后冻结",
		"采纳改变共同路径、可执行性或验收含义时重审受影响部分",
		"保证最终候选 Seal 前 FULL", "THAW 仅限需求/对外行为/判据/稳定可执行性变化",
		"非 wave 与 wave 的 RUNTIME_ERROR/REJECTED_INVALID_RESULT", "复用 TaskKey，默认 MaxAttempts=2", "一次性追加一个 attempt",
		"raw finding、普通 result/receipt/resume 不增 TaskKey", "owner cost 和 token cap 的 UNAVAILABLE 继续由各自来源、区间或配置条件独立决定",
		"阶段 4 需求 revision 转换由 engine 单一 writer", "只有用户确认事件授权原子回退", "legacy 写入口不能绕过该边界")
	qa35bContract(t, filepath.Join(pkg, "refactor-plan/incremental-seal-plan.md"), "requirement impact 迁移到 engine 唯一 writer", "仅用户确认事件可原子登记新 revision、用户保留清单及其余结果的回退", "不重置额度或自动授权新产品审；旧 legacy 入口不得绕过")
}

func qa35bContract(t *testing.T, path string, clauses ...string) {
	t.Helper()
	compact := func(s string) string { return strings.Join(strings.Fields(s), "") }
	body := compact(qa35bRead(t, path))
	for _, clause := range clauses {
		if !strings.Contains(body, compact(clause)) {
			t.Errorf("%s lacks normative clause %q", path, clause)
		}
	}
}

func TestQA35BInitialAndDeltaConfirmationHaveSeparateContracts(t *testing.T) {
	pkg := qa35bPackage(t)
	qa35bContract(t, filepath.Join(pkg, "SKILL.md"), "把完整整合后的需求与技术方案直接写入唯一正式需求文件", "该文件全文与精确 revision，单独发起确认提问并等待用户明确确认", "先从编辑前完整文本制作穷尽 review-delta", "CLI 不猜文本含义；保留既有 PASS需要明确清单")
	qa35bContract(t, filepath.Join(pkg, "prompts/actions/requirements-clarification.md"), "唯一正式需求文件", "再向用户呈现该文件全文与精确 revision 并单独请求最终确认", "取得用户最终确认后才返回 PASS", "预检成功后，以 `workflow submit` 的 `REQUEST` 提出 `confirm-requirement`", "不得在用户确认前提交确认决定")
	qa35bContract(t, filepath.Join(pkg, "references/formal-flow.md"), "编辑前完整 base 文本生成穷尽的 review-delta JSON", "base/candidate revision、finding 来源、每个 hunk 的影响与核验", "当前 state revision、Ask token/digest 共同防止过期确认")
}

func TestQA35BAcceptanceSurfaceHasLocalResponsibilityAndGateRule(t *testing.T) {
	pkg := qa35bPackage(t)
	qa35bContract(t, filepath.Join(pkg, "prompts/reviewer-base.md"),
		"`acceptance-surface` 需求/AC 引用", "核对已确认需求中的对外验收职责",
		"不能替代需求、实现或测试", "不得据此要求普通内部接口批量增加模板注释")
}

func TestQA35BCheckpointDoesNotImplementStage4OrRewriteHistory(t *testing.T) {
	pkg := qa35bPackage(t)
	const base = "860ac5eefe1c501aa9caae42de788af05a7f6809"
	const delivered = "b7eab3b16036a0a5fdeb32efc33c9d93d740392c"
	paths := strings.Split(qa35bGit(t, pkg, "diff", "--name-only", base, delivered), "\n")
	for _, path := range paths {
		if strings.HasPrefix(path, "internal/engine/") && !strings.HasPrefix(path, "internal/engine/guard/") {
			t.Errorf("stage 4 runtime touched: %s", path)
		}
		if strings.Contains(strings.ToUpper(path), "ADR-001") || strings.Contains(strings.ToUpper(path), "ADR-002") {
			t.Errorf("ADR changed: %s", path)
		}
	}
	old := qa35bGit(t, pkg, "show", base+":refactor-plan/stage-records.md")
	// Compare the document from the same immutable pair as the path checks.
	current := qa35bGit(t, pkg, "show", delivered+":refactor-plan/stage-records.md")
	if !strings.HasPrefix(current, old) || len(current) <= len(old) {
		t.Fatal("stage record history changed instead of checkpoint append")
	}
}
