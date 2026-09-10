package requirements

import (
	"fmt"
	"strings"
	"testing"
)

func guaranteeRequirementDocument(secondText string) string {
	if secondText == "" {
		secondText = "第二个结果可观察并通过。"
	}
	return `# 正式需求案

这里保留自然语言背景、方案与风险。

` + "```markdown\n## 需求点\n### REQ-999：围栏示例\n- AC-999：不参与解析\n```" + `

## 需求点

### REQ-001：公开行为

#### 要求

必须完成第一个行为，并且必须完成第二个行为。

#### 验收条件

- AC-001：第一个结果可观察并通过。
- AC-009：` + secondText + `

#### 来源

用户确认的对齐结论。
`
}

func TestRequirementProjectionParserContract(t *testing.T) {
	t.Run("valid natural prose fences and nonconsecutive ids", func(t *testing.T) {
		projection, err := ParseRequirementProjection("requirements.md", []byte(guaranteeRequirementDocument("")))
		if err != nil {
			t.Fatal(err)
		}
		if len(projection.Requirements) != 1 || projection.Requirements[0].ID != "REQ-001" || len(projection.Requirements[0].AcceptanceConditions) != 2 {
			t.Fatalf("unexpected projection: %#v", projection)
		}
		if projection.Requirements[0].AcceptanceConditions[1].ID != "AC-009" || projection.ContentDigest == "" {
			t.Fatalf("nonconsecutive ids or digest were lost: %#v", projection)
		}
	})

	t.Run("no finite item limit", func(t *testing.T) {
		var blocks []string
		for i := 1; i <= 160; i++ {
			blocks = append(blocks, fmt.Sprintf("### REQ-%03d：需求 %d\n\n#### 要求\n\n要求 %d。\n\n#### 验收条件\n\n- AC-%03d：结果 %d。\n\n#### 来源\n\n来源 %d。", i, i, i, i, i, i))
		}
		doc := "# 大需求案\n\n## 需求点\n\n" + strings.Join(blocks, "\n\n") + "\n"
		projection, err := ParseRequirementProjection("large.md", []byte(doc))
		if err != nil {
			t.Fatal(err)
		}
		if len(projection.Requirements) != 160 {
			t.Fatalf("parsed %d requirements", len(projection.Requirements))
		}
	})

	cases := []struct {
		name string
		doc  []byte
		want string
	}{
		{"invalid utf8", []byte{0xff, 0xfe}, "valid UTF-8"},
		{"missing section", []byte("# x\n"), "exactly one ## 需求点"},
		{"multiple sections", []byte("## 需求点\n\n### REQ-001：x\n\n#### 要求\n\nx\n\n#### 验收条件\n\n- AC-001：x\n\n#### 来源\n\nx\n\n## other\n\n## 需求点\n"), "exactly one ## 需求点"},
		{"invalid req heading", []byte("## 需求点\n\n### not-a-req\n"), "must match REQ"},
		{"duplicate req", []byte("## 需求点\n\n### REQ-001：a\n\n#### 要求\n\na\n\n#### 验收条件\n\n- AC-001：a\n\n#### 来源\n\na\n\n### REQ-001：b\n\n#### 要求\n\nb\n\n#### 验收条件\n\n- AC-002：b\n\n#### 来源\n\nb\n"), "duplicate REQ ID"},
		{"duplicate ac", []byte("## 需求点\n\n### REQ-001：a\n\n#### 要求\n\na\n\n#### 验收条件\n\n- AC-001：a\n- AC-001：b\n\n#### 来源\n\na\n"), "duplicate AC ID"},
		{"field order", []byte("## 需求点\n\n### REQ-001：a\n\n#### 验收条件\n\n- AC-001：a\n\n#### 要求\n\na\n\n#### 来源\n\na\n"), "要求 → 验收条件 → 来源"},
		{"empty requirement", []byte("## 需求点\n\n### REQ-001：a\n\n#### 要求\n\n#### 验收条件\n\n- AC-001：a\n\n#### 来源\n\na\n"), "要求 field must contain"},
		{"malformed ac", []byte("## 需求点\n\n### REQ-001：a\n\n#### 要求\n\na\n\n#### 验收条件\n\n- not-an-ac\n\n#### 来源\n\na\n"), "must match - AC"},
		{"extra field", []byte("## 需求点\n\n### REQ-001：a\n\n#### 要求\n\na\n\n#### 额外\n\nx\n\n#### 验收条件\n\n- AC-001：a\n\n#### 来源\n\na\n"), "may contain only"},
		{"req outside section", []byte("### REQ-001：outside\n\n## 需求点\n\n### REQ-002：a\n\n#### 要求\n\na\n\n#### 验收条件\n\n- AC-002：a\n\n#### 来源\n\na\n"), "outside ## 需求点"},
		{"ac outside field", []byte("- AC-777：outside\n\n## 需求点\n\n### REQ-001：a\n\n#### 要求\n\na\n\n#### 验收条件\n\n- AC-001：a\n\n#### 来源\n\na\n"), "outside its REQ"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseRequirementProjection("bad.md", tc.doc)
			if err == nil || !strings.Contains(err.Error(), tc.want) || !strings.Contains(err.Error(), "bad.md:") {
				t.Fatalf("error=%v, want diagnostic containing %q and path/line", err, tc.want)
			}
		})
	}
}

func TestRequirementProjectionReportsRecognizableIDForEmptyREQTitle(t *testing.T) {
	document := "## 需求点\n\n### REQ-001：\n"
	_, err := ParseRequirementProjection("requirements.md", []byte(document))
	if err == nil || !strings.Contains(err.Error(), "requirements.md:3 [REQ-001]") || !strings.Contains(err.Error(), "non-empty title") {
		t.Fatalf("empty REQ title diagnostic = %v", err)
	}
}

func TestRequirementProjectionDistinguishesShortACNumberFromEmptyACText(t *testing.T) {
	document := func(item string) string {
		return "## 需求点\n\n### REQ-001：Title\n\n#### 要求\n\nrequirement\n\n#### 验收条件\n\n- " + item + "\n\n#### 来源\n\nuser\n"
	}
	_, shortErr := ParseRequirementProjection("short.md", []byte(document("AC-1：condition")))
	if shortErr == nil || !strings.Contains(shortErr.Error(), "short.md:11 [REQ-001]") || !strings.Contains(shortErr.Error(), "AC number must contain at least three digits") || strings.Contains(shortErr.Error(), "AC condition text must be non-empty") {
		t.Fatalf("short AC diagnostic = %v", shortErr)
	}
	_, emptyErr := ParseRequirementProjection("empty.md", []byte(document("AC-001：")))
	if emptyErr == nil || !strings.Contains(emptyErr.Error(), "empty.md:11 [REQ-001]") || !strings.Contains(emptyErr.Error(), "AC condition text must be non-empty") || strings.Contains(emptyErr.Error(), "AC number must contain at least three digits") {
		t.Fatalf("empty AC diagnostic = %v", emptyErr)
	}
}
