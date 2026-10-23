package prompts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptDescriptionUsesOnlyExplicitFrontmatter(t *testing.T) {
	for _, test := range []struct {
		name, content, description string
	}{
		{"Chinese", "---\n简介: 中文简介\n---\n# 正文", "中文简介"},
		{"English", "---\ndescription: English description\n---\n# Body", "English description"},
		{"multiline", "---\ndescription: |\n  First line\n  Second line\n---\n# Body", "First line\nSecond line"},
		{"CRLF", "---\r\n简介: |\r\n  第一行\r\n  第二行\r\n---\r\n# 正文", "第一行\n第二行"},
		{"Chinese precedence", "---\n简介: 中文\ndescription: English\n---\n正文", "中文"},
		{"empty Chinese precedence", "---\n简介:\ndescription: English\n---\n正文", ""},
		{"empty", "---\ndescription: '  '\n---\n正文", ""},
		{"absent", "# 门\n普通正文", ""},
		{"body", "# 门\n简介: 正文中写的不是字段\ndescription: body", ""},
		{"fenced example", "```yaml\n---\n简介: 示例\n---\n```", ""},
		{"body frontmatter", "# 门\n---\n简介: 正文\n---", ""},
		{"unknown metadata", "---\ntitle: Gate\ntags: [one, two]\n---\nBody", ""},
		{"opening horizontal rule", "---\n# Review\n正文", ""},
		{"unclosed field block", "---\ndescription: text\nBody", ""},
		{"scalar header", "---\nReview\n---\nBody", ""},
		{"list header", "---\n- Review\n- Checks\n---\nBody", ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			directory := t.TempDir()
			if err := os.WriteFile(filepath.Join(directory, "example.md"), []byte(test.content), 0o600); err != nil {
				t.Fatal(err)
			}
			definitions, err := discoverPromptDirectory(directory)
			if err != nil {
				t.Fatal(err)
			}
			if len(definitions) != 1 || definitions[0] != (PromptDefinition{ID: "example", Content: test.content, Description: test.description}) {
				t.Fatalf("prompt metadata or content: %+v", definitions)
			}
		})
	}
}

func TestPromptDescriptionReportsInvalidFrontmatter(t *testing.T) {
	for _, content := range []string{
		"---\ndescription: [invalid\n---\nBody",
		"---\ndescription: [one, two]\n---\nBody",
	} {
		directory := t.TempDir()
		if err := os.WriteFile(filepath.Join(directory, "example.md"), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, err := discoverPromptDirectory(directory); err == nil || !strings.Contains(err.Error(), "example.md: frontmatter") {
			t.Fatalf("invalid frontmatter was not attributed to its prompt: %v", err)
		}
	}
}
