package validate

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func qaNativeFixture(t *testing.T) (string, string, RunState) {
	t.Helper()
	root, pkg, s := qaStore(t, qaState())
	// A documented legacy multi-artifact run exercises wave mechanics separately
	// from the guarantee projection, which has its own structural cases.
	qaWrite(t, filepath.Join(root, "acceptance.md"), "Documented acceptance attachment.\n")
	var err error
	s.RequirementArtifacts, err = requirementArtifactSet(root, "requirement.md", []string{"acceptance.md"})
	qaOK(t, err)
	qaWrite(t, filepath.Join(root, ".gitignore"), ".gates/\n")
	qaGit(t, root, "init", "-q")
	qaGit(t, root, "add", ".gitignore", "requirement.md", "acceptance.md")
	qaGit(t, root, "commit", "-q", "-m", "base fixture")
	s.BaseSnapshot = qaGit(t, root, "rev-parse", "HEAD")
	qaWrite(t, filepath.Join(root, "entry.go"), "package entry\nfunc Entry() int { return 1 }\n")
	qaGit(t, root, "add", "entry.go")
	qaGit(t, root, "commit", "-q", "-m", "development fixture")
	s.CurrentSnapshot = qaGit(t, root, "rev-parse", "HEAD")
	s.RequirementRevision = artifactRevision(s.RequirementArtifacts, s.RequirementSource)
	s.Actions["development-worker"] = ActionResult{Status: "PASS"}
	return root, pkg, s
}

func TestWhiteboxReviewReadyBarrierAndParallelPreparation(t *testing.T) {
	root, pkg, s := qaNativeFixture(t)
	qaApproved(&s, "whitebox")
	s.QAReviewByMode["whitebox"] = ActionResult{Status: "PENDING"}
	s.SelectedGates = []string{"whitebox", "complexity-gate", "implementation-quality-gate"}
	for _, id := range s.SelectedGates[1:] {
		s.Gates[id] = GateResult{Status: "PENDING"}
	}
	qaOK(t, SaveRunState(root, s))
	before := qaRead(t, RunStatePath(root, s.RunID))
	_, err := initializeReviewWave(root, pkg, s.RunID)
	qaErr(t, err, "FROZEN case-set")
	if qaRead(t, RunStatePath(root, s.RunID)) != before {
		t.Fatal("S1 initialized wave")
	}
	qaWrite(t, filepath.Join(root, "structure_test.go"), "package entry\nimport \"testing\"\nfunc TestEntry(t *testing.T) { if Entry()!=1 { t.Fatal(\"entry\") } }\n")
	qaGit(t, root, "add", "structure_test.go")
	qaGit(t, root, "commit", "-q", "-m", "whitebox fixture")
	s.CurrentSnapshot = qaGit(t, root, "rev-parse", "HEAD")
	s.QAReviewByMode["whitebox"] = ActionResult{Status: "PASS"}
	qaOK(t, recordQAReviewCaseSet(&s, "whitebox", ActionResult{Status: "PASS"}, PreparedDispatch{ID: "review"}))
	qaOK(t, SaveRunState(root, s))
	wave, err := PrepareReviewWave(root, pkg, s.RunID)
	qaOK(t, err)
	if wave.Snapshot != s.CurrentSnapshot || wave.BaseSnapshot != s.BaseSnapshot || wave.Status != "COLLECTING" || len(wave.Expected) != 3 {
		t.Fatalf("wave %+v", wave)
	}
	for key, target := range wave.Expected {
		if target.Status != "PREPARED" || target.DispatchID == "" || target.TaskKey == "" {
			t.Fatalf("not prepared together %s %+v", key, target)
		}
	}
	got, err := LoadRunState(root, s.RunID)
	qaOK(t, err)
	for _, d := range got.Dispatches {
		if d.SourceSnapshot != s.CurrentSnapshot {
			t.Fatal("dispatch bound before whitebox tests")
		}
		if d.TargetKind == "gate" {
			prompt := qaRead(t, d.PromptFile)
			if !strings.Contains(prompt, s.BaseSnapshot) || !strings.Contains(prompt, s.CurrentSnapshot) || strings.Contains(prompt, "accepts valid input") || strings.Contains(prompt, "independent.go::A") {
				t.Fatal("gate comparison or QA isolation violated")
			}
		}
	}
	again, err := PrepareReviewWave(root, pkg, s.RunID)
	qaOK(t, err)
	if again.ID != wave.ID || !reflect.DeepEqual(again.Expected, wave.Expected) {
		t.Fatal("duplicate wave or dispatches")
	}
}

func TestWhiteboxAdvancementRequiresDesignAtCurrentSnapshot(t *testing.T) {
	s := qaState()
	s.Actions["development-worker"] = ActionResult{Status: "PASS"}
	qaApproved(&s, "whitebox")
	if whiteboxTestCodeAdvancement(s) {
		t.Fatal("advance before bound design")
	}
	s.QADesignByMode["whitebox"] = ActionResult{Status: "PASS", DispatchID: "design"}
	s.Dispatches["design"] = PreparedDispatch{ID: "design", Target: "qa-design", Mode: "whitebox", SourceSnapshot: "candidate"}
	if !whiteboxTestCodeAdvancement(s) {
		t.Fatal("design cannot advance S1")
	}
	s.CurrentSnapshot = "S2"
	if whiteboxTestCodeAdvancement(s) {
		t.Fatal("same design advances twice")
	}
}

func TestFrozenQARejectsDesignAndReview(t *testing.T) {
	s := qaState()
	qaApproved(&s, "whitebox")
	qaOK(t, recordQAReviewCaseSet(&s, "whitebox", ActionResult{Status: "PASS"}, PreparedDispatch{ID: "review"}))
	qaErr(t, requireQADesignTransition(s, "whitebox"), "FROZEN")
	qaErr(t, requireQAReviewTransition(s, "whitebox"), "authoritative PASS")
}

func TestOnlyP3DoesNotBlockSealTransition(t *testing.T) {
	s := qaState()
	s.Actions["development-worker"] = ActionResult{Status: "VERIFIED"}
	s.SelectedGates = []string{"quality"}
	s.Gates["quality"] = GateResult{Status: "PASS", Snapshot: "candidate", Findings: []Finding{{Severity: "P3", Message: "optional phrasing"}}}
	qaOK(t, requireSealTransition(s))
	if hasSuggestionRecommendation(s) || hasRepairableBlocker(s) {
		t.Fatal("P3 became repair work")
	}
}

func qaContains(t *testing.T, path string, fragments ...string) {
	t.Helper()
	text := qaRead(t, path)
	for _, fragment := range fragments {
		if !strings.Contains(text, fragment) {
			t.Errorf("%s lacks contract %q", path, fragment)
		}
	}
}

func TestHumanWorkflowContractOwners(t *testing.T) {
	pkg := qaPackage(t)
	qaContains(t, filepath.Join(pkg, "SKILL.md"), "逐条定级理由", "同一条消息", "review-delta", "meaning preserved", "meaning changed", "AFFECTED", "FROZEN", "门审 P3 只作 Seal advisory")
	qaContains(t, filepath.Join(pkg, "references/formal-flow.md"), "同一", "缺少任一部分", "用户回复不能作为授权", "review-ready", "prepare-review-wave", "suggestion", "AFFECTED", "FULL", "有限 attempt", "review-delta", "requirement-impact", "state-revision")
	qaContains(t, filepath.Join(pkg, "prompts/reviewer-base.md"), "每条 P0/P1", "逐", "P2", "正常", "acceptance-surface")
	qaContains(t, filepath.Join(pkg, "prompts/actions/start-readiness.md"), "实现偏好最高为 P2", "category")
	qaContains(t, filepath.Join(pkg, "prompts/actions/qa-design.md"), "先独立确定应验证的行为、分支和 oracle", "复用真正有效的覆盖")
	qaContains(t, filepath.Join(pkg, "prompts/actions/qa-execution.md"), "AFFECTED", "FULL", "RUNTIME_ERROR")
	for _, name := range []string{"complexity-gate.md", "implementation-quality-gate.md"} {
		qaContains(t, filepath.Join(pkg, "gates", name), "acceptance-surface")
	}
	qaContains(t, filepath.Join(pkg, "README.md"), "guard-preview", "冻结", "固定只跑受影响", "P2", "P3", "review-delta")
	qaContains(t, filepath.Join(pkg, "README_EN.md"), "guard-preview", "frozen", "fixed affected QA subset", "P2", "P3", "meaning-changing")
	qaContains(t, filepath.Join(pkg, "AGENTS.md"), "项目代理规则", "prompts/reviewer-base.md", "SKILL.md", "references/formal-flow.md")
}

func TestFutureEnginePlansInheritConvergenceContract(t *testing.T) {
	pkg := qaPackage(t)
	for _, name := range []string{"refactor-plan/final-implementation-draft.md", "refactor-plan/stage-3-5b-runtime-guardrails.md", "refactor-plan/incremental-seal-plan.md", "refactor-plan/route-matrix.md"} {
		qaContains(t, filepath.Join(pkg, name), "guard-preview", "Operator", "RETRY_EXHAUSTED", "FROZEN", "meaning-changed", "P3", "NOT_INSTALLED")
	}
	qaContains(t, filepath.Join(pkg, "openspec/changes/orchestration-pipeline-engine/master-requirements.md"), "guard-preview", "Operator", "severity rationale", "meaning-changed", "RETRY_EXHAUSTED", "FROZEN")
	qaContract(t, filepath.Join(pkg, "refactor-plan/stage-3-5b-runtime-guardrails.md"),
		"不判断语义真假、根因、严重度或重复", "已核实 finding 的处置仍不唯一时才 Ask", "产品/技术审 confirm/dismiss 规则保持",
		"白盒最终 FROZEN case-set 绑定最终含测试快照后才 review-ready", "COLLECTING 未收齐或核实未完成时不建立返修",
		"门审 P2 carry 不复审，门审 P3 只作 Seal advisory", "QA Review P2/P3 以 suggestion ID 吸收后冻结且不复审",
		"保证最终候选 Seal 前 FULL", "THAW 仅限需求/对外行为/判据/稳定可执行性变化",
		"非 wave 与 wave 的 RUNTIME_ERROR/REJECTED_INVALID_RESULT", "复用 TaskKey，默认 MaxAttempts=2", "一次性追加一个 attempt",
		"raw finding、普通 result/receipt/resume 不增 TaskKey", "owner cost 和 token cap 的 UNAVAILABLE 继续由各自来源、区间或配置条件独立决定",
		"阶段 4 需求 revision 转换由 engine 单一 writer", "只有用户确认事件授权原子回退", "legacy 写入口不能绕过该边界")
	qaContract(t, filepath.Join(pkg, "refactor-plan/incremental-seal-plan.md"), "requirement impact 迁移到 engine 唯一 writer", "仅用户确认事件可授权原子回退", "重置额度或自动授权新产品审；旧 legacy 入口不得绕过")
	qaContract(t, filepath.Join(pkg, "openspec/changes/orchestration-pipeline-engine/master-requirements.md"), "阶段 5–7 继承同一 finding/problem ID、归并、唯一返修、P3 advisory 和 QA 冻结语义")
}

func qaContract(t *testing.T, path string, clauses ...string) {
	t.Helper()
	compact := func(s string) string { return strings.Join(strings.Fields(s), "") }
	body := compact(qaRead(t, path))
	for _, clause := range clauses {
		if !strings.Contains(body, compact(clause)) {
			t.Errorf("%s lacks normative clause %q", path, clause)
		}
	}
}

func TestInitialAndDeltaConfirmationHaveSeparateContracts(t *testing.T) {
	pkg := qaPackage(t)
	qaContract(t, filepath.Join(pkg, "SKILL.md"), "把完整整合后的需求与技术方案直接写入唯一正式需求文件", "该文件全文与精确 revision，单独发起确认提问并等待用户明确确认", "先从编辑前完整文本制作穷尽 review-delta", "CLI 不判断文本含义，也不保存 base 文本/diff，只保存核验摘要与确认绑定")
	qaContract(t, filepath.Join(pkg, "prompts/actions/requirements-clarification.md"), "唯一正式需求文件", "再向用户呈现该文件全文与精确 revision 并单独请求最终确认", "取得用户最终确认后才返回 PASS", "单一结构化正式需求会在该确认中原子冻结保证 envelope")
	qaContract(t, filepath.Join(pkg, "references/formal-flow.md"), "编辑前完整 base 文本生成穷尽的 review-delta JSON", "base/candidate revision、finding 来源、每个 hunk 的影响与核验", "CLI 只校验结构、revision/finding/hunk/确认绑定并保存摘要回执，不保存 base 文本或 diff")
}

func TestAcceptanceSurfaceHasLocalResponsibilityAndGateRule(t *testing.T) {
	pkg := qaPackage(t)
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(pkg, "internal/validate/review_convergence.go"), nil, parser.ParseComments)
	qaOK(t, err)
	found := false
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "PrepareReviewWave" {
			continue
		}
		found = fn.Doc != nil && strings.Contains(fn.Doc.Text(), "acceptance-surface") && strings.Contains(fn.Doc.Text(), "AC-009/AC-012")
	}
	if !found {
		t.Fatal("PrepareReviewWave lacks adjacent AC-009/AC-012 responsibility")
	}
	qaContract(t, filepath.Join(pkg, "gates/complexity-gate.md"), "建议删除或形成 finding 前先核对附近的 `acceptance-surface` REQ/AC 引用", "不自动证明接口必要，也不要求普通内部接口增加模板注释")
	qaContract(t, filepath.Join(pkg, "gates/implementation-quality-gate.md"), "建议删除或据此形成 finding 前先核对附近的 `acceptance-surface` REQ/AC 引用", "不替代需求、实现或测试，也不要求普通接口批量添加")
}

func TestCheckpointDoesNotImplementStage4OrRewriteHistory(t *testing.T) {
	pkg := qaPackage(t)
	const base = "860ac5eefe1c501aa9caae42de788af05a7f6809"
	const delivered = "b7eab3b16036a0a5fdeb32efc33c9d93d740392c"
	paths := strings.Split(qaGit(t, pkg, "diff", "--name-only", base, delivered), "\n")
	for _, path := range paths {
		if strings.HasPrefix(path, "internal/engine/") && !strings.HasPrefix(path, "internal/engine/guard/") {
			t.Errorf("stage 4 runtime touched: %s", path)
		}
		if strings.Contains(strings.ToUpper(path), "ADR-001") || strings.Contains(strings.ToUpper(path), "ADR-002") {
			t.Errorf("ADR changed: %s", path)
		}
	}
	old := qaGit(t, pkg, "show", base+":refactor-plan/stage-records.md")
	// Compare the document from the same immutable pair as the path checks.
	current := qaGit(t, pkg, "show", delivered+":refactor-plan/stage-records.md")
	if !strings.HasPrefix(current, old) || len(current) <= len(old) {
		t.Fatal("stage record history changed instead of checkpoint append")
	}
}

func TestGuardAndCostDependencyBoundaries(t *testing.T) {
	pkg := qaPackage(t)
	for _, dir := range []string{"internal/engine/guard", "internal/cost"} {
		entries, err := os.ReadDir(filepath.Join(pkg, dir))
		qaOK(t, err)
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(pkg, dir, entry.Name()), nil, parser.ImportsOnly)
			qaOK(t, err)
			for _, spec := range file.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				qaOK(t, err)
				if dir == "internal/cost" && (strings.Contains(path, "/validate") || strings.Contains(path, "/engine")) {
					t.Errorf("cost reverse dependency %s", path)
				}
				if dir == "internal/engine/guard" && !containsExactString([]string{"crypto/sha256", "encoding/json", "fmt", "sort", "strings", "formal-gates/internal/engine/runtime"}, path) {
					t.Errorf("pure guard acquired dependency %s", path)
				}
			}
		}
	}
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(pkg, "internal/cli/guard_preview.go"), nil, 0)
	qaOK(t, err)
	calls := 0
	ast.Inspect(file, func(node ast.Node) bool {
		call, ok := node.(*ast.CallExpr)
		if !ok {
			return true
		}
		selector, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		owner, ok := selector.X.(*ast.Ident)
		if ok && owner.Name == "guard" && selector.Sel.Name == "Calculate" {
			calls++
		}
		return true
	})
	if calls != 1 {
		t.Fatalf("preview must delegate exactly once to production Calculate, got %d calls", calls)
	}
}
