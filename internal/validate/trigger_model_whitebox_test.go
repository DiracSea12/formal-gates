//go:build phase0whitebox

package validate

// White-box structure tests for the trigger-model V2 change (TRIGGER-MODEL-V2-
// REQUIREMENT.md). Each test function below implements exactly one whitebox
// case and is bound to it by the QA CLI via a "--test <file>::<function>"
// reference; the function name is the opaque locator for that case's test.
//
// The cases verify the structural claims of the requirement against the current
// effective rules: the trigger model is "default remind once (conditional copy),
// large/complex requests get one extra emphasis (advisory copy), the two
// structured mentions are the design upper bound, only an explicit user request
// enters the full intake, and the copy never writes「不要求回应」explicitly";
// the lightweight route is back with the new semantics (start → register
// requirement → Seal, no verification, record only, seal marked「本 run 未经任何
// 验证」); and the renamed artifacts (canary quick-e2e-workflow, clarification
// fallback「快速澄清兜底」) no longer carry the old「轻量」wording.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readWorktreeFile reads a file from the current worktree (repo root located by
// go.mod), so every assertion targets the real shipped content.
func readWorktreeFile(t *testing.T, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRootValidateTest(t), filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(data)
}

// compactSpaces strips all whitespace so hard-wrapped Chinese prose and folded
// English sentences still match their intended phrase. Both sides of a compare
// are compacted identically, so only the non-whitespace character sequence is
// compared.
func compactSpaces(s string) string {
	return strings.Join(strings.Fields(s), "")
}

func assertHasText(t *testing.T, text, want string) {
	t.Helper()
	if !strings.Contains(compactSpaces(text), compactSpaces(want)) {
		t.Fatalf("expected text %q (whitespace-insensitive) to be present", want)
	}
}

func assertLacksText(t *testing.T, text, forbidden string) {
	t.Helper()
	if strings.Contains(compactSpaces(text), compactSpaces(forbidden)) {
		t.Fatalf("forbidden text %q (whitespace-insensitive) is present", forbidden)
	}
}

// skillDescription extracts the frontmatter description line of SKILL.md.
func skillDescription(t *testing.T, skill string) string {
	t.Helper()
	if !strings.HasPrefix(skill, "---\n") {
		t.Fatal("SKILL.md must start with a frontmatter block")
	}
	rest := skill[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		t.Fatal("SKILL.md frontmatter block is not closed")
	}
	for _, line := range strings.Split(rest[:end], "\n") {
		if strings.HasPrefix(line, "description:") {
			return line
		}
	}
	t.Fatal("SKILL.md frontmatter has no description field")
	return ""
}

// Case: the SKILL.md entrypoint description must declare the V2 trigger model:
// every content-modification request gets one default reminder (conditional
// copy), large/complex requests get one extra emphasis, and only an explicit
// user request enters the full intake; the lightweight route is back as a
// record-only route.
func TestSkillDescriptionDeclaresV2TriggerModel(t *testing.T) {
	desc := skillDescription(t, readWorktreeFile(t, "SKILL.md"))
	assertHasText(t, desc, "默认提醒一次")
	assertHasText(t, desc, "若需走 formal-gates 流程，可直接提出")
	assertHasText(t, desc, "大/复杂需求在需求与方案确认、准备开做时再额外强调一次")
	assertHasText(t, desc, "用户明确要求走正式流程时才进入完整受理流程")
	assertHasText(t, desc, "轻量路线")
	assertLacksText(t, desc, "不要求回应")
}

// Case: the SKILL.md intake body must encode the V2 default-remind rule: the
// default reminder uses the conditional copy「若需走 formal-gates 流程，可直接
// 提出」, large/complex requests get the advisory copy「检测到复杂需求，建议走
// formal-gates 流程」at the confirmation-to-start point, the body no longer
// carries the V1「两次结构化提及是设计上限/反复催促或追问」wording,
// non-modification requests do not trigger, the copy never writes「不要求回应」
// explicitly, and the full intake runs only on the user's explicit request.
func TestSkillIntakeBodyDeclaresDefaultRemindOnce(t *testing.T) {
	skill := readWorktreeFile(t, "SKILL.md")
	assertHasText(t, skill, "默认提醒一次：若需走 formal-gates 流程，可直接提出")
	assertHasText(t, skill, "检测到复杂需求，建议走 formal-gates 流程")
	assertHasText(t, skill, "在需求澄清与方案确认完毕、准备开做之际再额外强调一次")
	assertLacksText(t, skill, "反复催促或追问")
	assertHasText(t, skill, "不得自行触发")
	assertHasText(t, skill, "非修改性的提问、解释、诊断和 review 不触发")
	assertHasText(t, skill, "用户明确要求走正式流程（或明确要求触发 formal-gates）时，直接进入完整受理")
	assertLacksText(t, skill, "不要求回应")
	assertLacksText(t, skill, "默认按常规方式直接处理")
}

// Case: after the complete requirement and solution are confirmed, intake
// proceeds into the formal flow; full/custom stays undecided until after the
// slicing decision and lightweight skips slicing and route selection.
func TestSkillStepFourEntersAfterConfirmation(t *testing.T) {
	skill := readWorktreeFile(t, "SKILL.md")
	assertHasText(t, skill, "用户明确确认完整需求与方案后进入正式流程")
	assertHasText(t, skill, "正式路线（`full`/`custom`）本阶段不定，拆分决定之后确认")
	assertHasText(t, skill, "不经拆分决定与路线选择")
	assertLacksText(t, skill, "询问用户是否进入正式流程")
}

// Case: the SKILL.md lightweight route section must declare the record-only
// semantics (start → 需求登记 → Seal, no verification, no snapshot, no slicing
// decision, no route selection) and the seal annotation「本 run 未经任何验证」.
func TestSkillDeclaresLightweightRouteRecordOnly(t *testing.T) {
	skill := readWorktreeFile(t, "SKILL.md")
	assertHasText(t, skill, "轻量路线")
	assertHasText(t, skill, "`workflow start --route lightweight`")
	assertHasText(t, skill, "不做拆分决定、不选 QA/门路线、不快照")
	assertHasText(t, skill, "本 run 未经任何验证")
	assertHasText(t, skill, "正式流程内的路线（非受理阶段选项）")
}

// Case: the session-level "不走流程" declaration, the 【流程提示】 session prompt,
// the main-agent high-confidence skip determination must all be gone; the
// removal is surgical, so the unrelated slicing phrase "高置信要拆" stays.
func TestSkillDropsSessionDeclarationAndHighConfidenceSkip(t *testing.T) {
	skill := readWorktreeFile(t, "SKILL.md")
	assertLacksText(t, skill, "流程提示")
	assertLacksText(t, skill, "会话级")
	assertLacksText(t, skill, "不走流程")
	assertLacksText(t, skill, "高置信度判断")
	assertLacksText(t, skill, "跳过整个受理流程")
	assertHasText(t, skill, "高置信要拆时需用户确认拆分方案")
}

// Case: README.md (CN) must carry the V2 trigger model (default remind once,
// large/complex extra emphasis, no self-trigger, explicit request enters the
// formal flow), reintroduce the lightweight route, and drop the retired FAQ
// "轻量和 formal 是什么关系".
func TestReadmeCnStatesV2TriggerModel(t *testing.T) {
	readme := readWorktreeFile(t, "README.md")
	assertLacksText(t, readme, "轻量和 formal 是什么关系")
	assertHasText(t, readme, "默认提醒一次")
	assertHasText(t, readme, "若需走 formal-gates 流程，可直接提出")
	assertHasText(t, readme, "检测到复杂需求，建议走 formal-gates 流程")
	assertHasText(t, readme, "不会自行触发")
	assertHasText(t, readme, "你明确要求时才进入正式流程")
	assertHasText(t, readme, "formal-gates 什么时候会被触发")
	assertHasText(t, readme, "轻量路线")
	assertLacksText(t, readme, "由你决定是否进入正式流程")
	assertLacksText(t, readme, "受理阶段只决定是否进入正式流程")
}

// Case: README_EN.md must carry the V2 trigger model in English and reintroduce
// the lightweight route, dropping the retired FAQ
// "What is the relationship between lightweight and formal?".
func TestReadmeEnStatesV2TriggerModel(t *testing.T) {
	readme := readWorktreeFile(t, "README_EN.md")
	assertLacksText(t, readme, "What is the relationship between lightweight and formal")
	assertHasText(t, readme, "One default reminder")
	assertHasText(t, readme, "you may directly request the formal-gates flow")
	assertHasText(t, readme, "detected complex requirements")
	assertHasText(t, readme, "never self-triggers")
	assertHasText(t, readme, "When is formal-gates triggered?")
	assertHasText(t, readme, "lightweight")
	assertLacksText(t, readme, "asks you whether to enter the formal flow")
	assertLacksText(t, readme, "intake decides only whether to enter")
}

// Case: references/example-run.md must carry the V2 trigger model (default
// remind branch with the conditional copy, large/complex advisory emphasis),
// reintroduce the lightweight route inside the formal flow (not an intake-phase
// option), without retaining the obsolete enter-formal-flow question.
func TestExampleRunStatesV2TriggerModel(t *testing.T) {
	example := readWorktreeFile(t, "references/example-run.md")
	assertLacksText(t, example, "不走流程")
	assertHasText(t, example, "默认提醒分支")
	assertHasText(t, example, "若需走 formal-gates 流程，可直接提出")
	assertHasText(t, example, "检测到复杂需求，建议走 formal-gates 流程")
	assertHasText(t, example, "在需求澄清与方案确认完毕、准备开做之际再额外强调一次")
	assertHasText(t, example, "3. 进入正式流程")
	assertLacksText(t, example, "询问用户是否进入正式流程")
	assertLacksText(t, example, "3. 是否进入正式流程")
	assertHasText(t, example, "普通请求不进入本示例的正式流程分支")
}

// Case: references/formal-flow.md must declare the lightweight start surface
// (--route lightweight), the start → 需求登记 → Seal record-only route, and the
// seal annotation「本 run 未经任何验证」, while keeping the SKILL.md ownership
// line and dropping the session-declaration dangling reference.
func TestFormalFlowDeclaresLightweightRoute(t *testing.T) {
	flow := readWorktreeFile(t, "references/formal-flow.md")
	assertHasText(t, flow, "流程顺序只由 `SKILL.md` 拥有")
	assertHasText(t, flow, "--route lightweight")
	assertHasText(t, flow, "start → 需求登记 → Seal")
	assertHasText(t, flow, "本 run 未经任何验证")
	assertLacksText(t, flow, "不走流程")
	assertLacksText(t, flow, "会话声明")
}

// Case: the marker block in SKILL.md must declare the V2 managed rule (default
// remind once, large/complex extra emphasis, full intake only on explicit
// request) and must not write「不要求回应」explicitly nor retain the forced-intake
// phrasing.
func TestSkillManagedRuleBlockDeclaresV2Intake(t *testing.T) {
	rule, err := LoadManagedRule(repoRootValidateTest(t))
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, rule, "默认提醒一次")
	assertHasText(t, rule, "若需走 formal-gates 流程，可直接提出")
	assertHasText(t, rule, "检测到复杂需求，建议走 formal-gates 流程")
	assertHasText(t, rule, "在需求澄清与方案确认完毕、准备开做之际再额外强调一次")
	assertHasText(t, rule, "用户明确要求走正式流程（或明确要求触发 formal-gates）时")
	assertLacksText(t, rule, "受理阶段只决定是否进入正式流程")
	assertLacksText(t, rule, "不要求回应")
	assertLacksText(t, rule, "必须先执行 formal-gates 受理流程")
}

// Case: agents/openai.yaml default_prompt must encode the V2 intake sequencing
// — mention once, do not self-trigger, enter after the confirmed intake,
// full/custom confirmed after the slicing decision, lightweight skips slicing
// and route selection and seals without verification.
func TestOpenAIDefaultPromptStatesV2Model(t *testing.T) {
	openai := readWorktreeFile(t, "agents/openai.yaml")
	assertHasText(t, openai, "mention once")
	assertHasText(t, openai, "not self-trigger")
	assertHasText(t, openai, "on explicit user request")
	assertHasText(t, openai, "then enter the formal flow")
	assertLacksText(t, openai, "ask whether to enter the formal flow")
	assertHasText(t, openai, "route full/custom is confirmed after the slicing decision")
	assertHasText(t, openai, "lightweight")
}

// Case: formal-gates.manifest.json description must describe the V2 model (a
// default one-time reminder, an on-request formal flow, a record-only
// lightweight route) and must not retain the "universal modification intake"
// phrasing.
func TestManifestDescriptionStatesV2Model(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRootValidateTest(t), "formal-gates.manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	assertHasText(t, doc.Description, "on-request formal flow")
	assertHasText(t, doc.Description, "record-only lightweight route")
	assertLacksText(t, doc.Description, "universal modification intake")
}

// Case: the documented install path (Install, project scope — the same code path
// that populates the worktree + .claude + .codex + .cursor locations) must
// reproduce the V2 trigger model at the install target: the default reminder
// copy and the lightweight route in SKILL.md/README.md, the no-self-trigger
// metadata, the on-request manifest description, and the managed rule file
// written to the host rule file must carry the V2 wording without「不要求回应」.
func TestInstallReproducesV2ModelAtTarget(t *testing.T) {
	// 2026-08-21 事故根因修复：install 的共享 registry 与稳定 launcher 默认都
	// 按 HOME 解析（installRegistryPath/defaultStableLauncherPath ->
	// installHomeDir）。本用例此前未隔离 HOME，把真实 ~/.formal-gates/
	// registry.json 与 ~/.local/bin/formal-gates 写坏（launcher 被源包 bin/
	// 下的 25 字节桩覆盖）。先把 HOME 指向临时目录，再以完全相同的安装语义
	// 复现 V2 触发模型断言。
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	source := copyPackageFixture(t)
	project := t.TempDir()
	report, err := Install(InstallOptions{Source: source, Host: "claude", Scope: "project", Project: project, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Targets) != 1 {
		t.Fatalf("expected exactly one install target, got %d", len(report.Targets))
	}
	installed := filepath.Join(project, ".claude", "skills", "formal-gates")

	skill, err := os.ReadFile(filepath.Join(installed, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, string(skill), "若需走 formal-gates 流程，可直接提出")
	assertHasText(t, string(skill), "检测到复杂需求，建议走 formal-gates 流程")
	assertHasText(t, string(skill), "本 run 未经任何验证")

	readme, err := os.ReadFile(filepath.Join(installed, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, string(readme), "默认提醒一次")
	assertHasText(t, string(readme), "轻量路线")

	openai, err := os.ReadFile(filepath.Join(installed, "agents", "openai.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, string(openai), "not self-trigger")
	assertHasText(t, string(openai), "lightweight")

	manifest, err := os.ReadFile(filepath.Join(installed, "formal-gates.manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, string(manifest), "on-request formal flow")

	managed, err := os.ReadFile(filepath.Join(project, "CLAUDE.md"))
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, string(managed), "默认提醒一次")
	assertHasText(t, string(managed), "若需走 formal-gates 流程，可直接提出")
	assertHasText(t, string(managed), hostInstructionsStartMarker)
	assertHasText(t, string(managed), hostInstructionsEndMarker)
	assertLacksText(t, string(managed), "不要求回应")
	assertLacksText(t, string(managed), "必须先执行 formal-gates 受理流程")
}

// Case: the renames required by V2 must be in place — the "lightweight-workflow"
// canary name is now "quick-e2e-workflow" and the "轻量澄清兜底" clarification
// fallback is now "快速澄清兜底", so the old「轻量」wording is gone from both.
func TestRenamedArtifactsDropLightweightWording(t *testing.T) {
	canary := readWorktreeFile(t, "internal/validate/canary.go")
	assertHasText(t, canary, `add("quick-e2e-workflow"`)
	assertLacksText(t, canary, `add("lightweight-workflow"`)
	assertHasText(t, canary, "runQuickE2ECanary")
	assertLacksText(t, canary, "runLightweightCanary")
	clarification := readWorktreeFile(t, "prompts/actions/requirements-clarification.md")
	assertHasText(t, clarification, "快速澄清兜底")
	assertLacksText(t, clarification, "轻量澄清兜底")
}

// Case: a lightweight run can go start (--route lightweight) → 需求登记 → Seal
// in three steps, skipping the slicing decision, route selection, development
// snapshot and every verification gate, and its seal record is explicitly marked
// 「本 run 未经任何验证」.
func TestLightweightRouteSealsWithoutVerification(t *testing.T) {
	packageRoot := repoRootValidateTest(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "requirement.md"), []byte("record-only behavior\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := initializeCanaryGit(root); err != nil {
		t.Fatal(err)
	}

	state, err := Start(StartOptions{Root: root, PackageRoot: packageRoot, RunID: "lightweight-run", Flow: "formal", RequirementSource: "requirement.md", VCS: "git", Route: "lightweight"})
	if err != nil {
		t.Fatal(err)
	}
	if state.RouteMode != "lightweight" {
		t.Fatalf("expected lightweight route mode, got %q", state.RouteMode)
	}
	if state.SplitDeclaration != "" {
		t.Fatalf("lightweight start must not record a split declaration, got %q", state.SplitDeclaration)
	}

	// 需求登记：requirements-clarification PASS + requirement --confirmed。
	if _, err := PrepareAction(root, packageRoot, state.RunID, "requirements-clarification", "", false, ""); err != nil {
		t.Fatal(err)
	}
	state, err = LoadRunState(root, state.RunID)
	if err != nil {
		t.Fatal(err)
	}
	state, err = RecordAction(root, packageRoot, state.RunID, "requirements-clarification", openDispatchID(state, "action", "requirements-clarification"), "PASS", "", nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	state, err = UpdateRequirement(root, packageRoot, state.RunID, "", true, "", nil)
	if err != nil {
		t.Fatal(err)
	}

	// 轻量直达 Seal：无拆分决定、无路线确认、无开发快照、无任何验证。
	summary, err := Seal(root, packageRoot, state.RunID, nil, false, "")
	if err != nil {
		t.Fatal(err)
	}
	if summary.RouteMode != "lightweight" {
		t.Fatalf("expected sealed lightweight route mode, got %q", summary.RouteMode)
	}
	if summary.Unverified != "本 run 未经任何验证" {
		t.Fatalf("lightweight seal record must be marked unverified, got %q", summary.Unverified)
	}
}
