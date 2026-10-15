//go:build qastructure

package validate

// Trigger contracts have one source in SKILL.md; installation retains its bytes
// and publishes that source's managed host rule.
import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readWorktreeFile(t *testing.T, relative string) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join(repoRootValidateTest(t), filepath.FromSlash(relative)))
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}
func assertHasText(t *testing.T, text, want string) {
	t.Helper()
	if !strings.Contains(strings.Join(strings.Fields(text), ""), strings.Join(strings.Fields(want), "")) {
		t.Fatalf("required policy %q is absent", want)
	}
}
func skillDescription(t *testing.T, text string) string {
	t.Helper()
	frontmatter, body, ok := strings.Cut(strings.TrimPrefix(text, "---\n"), "\n---\n")
	if !strings.HasPrefix(text, "---\n") || !ok || body == "" {
		t.Fatal("skill frontmatter is incomplete")
	}
	for _, line := range strings.Split(frontmatter, "\n") {
		if strings.HasPrefix(line, "description:") {
			return line
		}
	}
	t.Fatal("skill description is absent")
	return ""
}

func TestSkillDescriptionDeclaresTriggerModel(t *testing.T) {
	description := skillDescription(t, readWorktreeFile(t, "SKILL.md"))
	for _, rule := range []string{"用户明确要求走正式流程时才进入完整受理流程", "默认提醒一次", "若需走 formal-gates 流程，可直接提出", "大/复杂需求在需求与方案确认、准备开做时再额外强调一次", "轻量路线"} {
		assertHasText(t, description, rule)
	}
}
func TestSkillIntakeBodyDeclaresDefaultRemindOnce(t *testing.T) {
	skill := readWorktreeFile(t, "SKILL.md")
	for _, rule := range []string{"默认提醒一次", "检测到复杂需求，建议走 formal-gates 流程", "非修改性的提问、解释、诊断和 review 不触发正式流程", "主代理不得自行触发", "先做确定性结构预检，通过后展示该文件全文与精确 revision", "自然语言只提出申请", "调用主宿主 confirm_start 显示绑定原材料的原生确认，用户亲自确认后由程序直接启动 run", "模型不能填写批准值、制作确认收据或用 CLI 自报确认"} {
		assertHasText(t, skill, rule)
	}
}
func TestSkillDelegatesAdvancementToCurrentCLI(t *testing.T) {
	skill := readWorktreeFile(t, "SKILL.md")
	assertHasText(t, skill, "主代理只负责需求语义、事实核实、完整材料展示和真实用户决定")
	assertHasText(t, skill, "程序独占下一步、派发、顺序、轮次、恢复、机械绑定与结果接纳")
	assertHasText(t, skill, "workflow next")
	assertHasText(t, skill, "workflow drive")
	for _, rule := range []string{
		"预检自动保存完整 intake 载荷并公布 `intake` 序号",
		"传入 `intake` 原序号与 `route`，由原生弹窗确认完整启动操作",
		"真人在原生表单确认后由程序直接提交原对象",
		"没有真人交互能力时明确不可执行，不以模型回包、自动批准 hook 或 NativeRespond 代替",
		"不抄取或重拼已知路径、身份、摘要、全文和收据",
		"lightweight 使用同一预检加 `--route lightweight`",
		"`--document` 复用 `--precheck --materials` 发布的普通材料序号",
		"记录 unverified",
		"不把它当验证通过",
	} {
		assertHasText(t, skill, rule)
	}
}
func TestSkillManagedRuleBlockDeclaresIntake(t *testing.T) {
	rule, err := LoadManagedRule(repoRootValidateTest(t))
	if err != nil {
		t.Fatal(err)
	}
	skill := readWorktreeFile(t, "SKILL.md")
	_, body, found := strings.Cut(skill, hostInstructionsStartMarker)
	if !found {
		t.Fatal("managed rule source is absent")
	}
	expected, _, found := strings.Cut(body, hostInstructionsEndMarker)
	if !found {
		t.Fatal("managed rule end is absent")
	}
	if strings.TrimSpace(rule) != strings.TrimSpace(expected) {
		t.Fatal("managed rule differs from its unique skill source")
	}
}
func TestAgentEntrypointUsesSkillAuthority(t *testing.T) {
	configuration := readWorktreeFile(t, "agents/openai.yaml")
	assertHasText(t, configuration, "读取并遵循 SKILL.md")
}
func TestManifestDescriptionStatesModel(t *testing.T) {
	var manifest struct {
		Description string `json:"description"`
	}
	if err := json.Unmarshal([]byte(readWorktreeFile(t, "formal-gates.manifest.json")), &manifest); err != nil {
		t.Fatal(err)
	}
	assertHasText(t, manifest.Description, "on-request formal flow")
	assertHasText(t, manifest.Description, "record-only lightweight route")
}
func TestInstallPreservesSkillAndManagedRule(t *testing.T) {
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
		t.Fatalf("installed targets=%d", len(report.Targets))
	}
	target := report.Targets[0]
	for _, relative := range []string{"SKILL.md", "agents/openai.yaml", "formal-gates.manifest.json"} {
		original, err := os.ReadFile(filepath.Join(source, relative))
		if err != nil {
			t.Fatal(err)
		}
		installed, err := os.ReadFile(filepath.Join(target.TargetPath, relative))
		if err != nil {
			t.Fatal(err)
		}
		if string(original) != string(installed) {
			t.Fatalf("installed %s differs from its source", relative)
		}
	}
	expected, err := LoadManagedRule(source)
	if err != nil {
		t.Fatal(err)
	}
	installed, err := os.ReadFile(target.ManagedRulePath)
	if err != nil {
		t.Fatal(err)
	}
	assertHasText(t, string(installed), hostInstructionsStartMarker)
	assertHasText(t, string(installed), hostInstructionsEndMarker)
	assertHasText(t, string(installed), expected)
}
