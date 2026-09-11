//go:build phase0whitebox

package validate

import

// 白盒结构测试（白盒设计者交付）：本文件由白盒 QA 设计者在开发后独立设计并编写，
// 覆盖 P1 QA 彻底解耦与 Carry 继承修复的关键确定性规则。每条白盒用例的
// Test 绑定 = 文件定位的测试引用 "<文件路径>::<函数名>"（如
// "whitebox_delivered_test.go::TestWhiteboxDirectRules"），CLI 记录时只校验引用非空、
// 且同一引用不被两条白盒用例共用；存在性与对应性由 qa-review 与 qa-execution 验证。
(
	"strings"
	"testing"
)

// TestBacklogAndKnowledgeDocsIgnored 覆盖验收：P2-BACKLOG.md 与
// PROMPT-ENGINEERING-KNOWLEDGE.md 被 .gitignore 忽略（git check-ignore 通过），记录范围外、
// 不跟踪。文件存在是本地交付事实、CI 干净检出时并不存在，因此这里只验证忽略规则。
func TestBacklogAndKnowledgeDocsIgnored(t *testing.T) {
	root := repoRootForCanaryTest(t)
	for _, name := range []string{"P2-BACKLOG.md", "PROMPT-ENGINEERING-KNOWLEDGE.md"} {
		out, err := (execNativeCommandRunner{}).Run(root, "git", "check-ignore", name)
		if err != nil || !strings.Contains(out, name) {
			t.Fatalf("git check-ignore %s failed: out=%q err=%v", name, out, err)
		}
	}
}
