package requirement

import (
	"strings"
	"testing"
)

func prose(s string) *string { return &s }

func TestSemanticRenderingCreatesPrecheckedDocumentAndRetainsExistingMaterial(t *testing.T) {
	created, err := RenderSemanticChange("requirement.md", nil, SemanticChange{Title: prose("归档"), Requirement: prose("保存中文、引号 \" 与换行。\n\n```go\nvalue := 1\n```"), Acceptance: prose("归档可读取。"), Source: prose("用户原请求。"), Context: prose("保留工作；正文中提及 ## 方案。"), Solution: prose("使用现有归属。")})
	if err != nil {
		t.Fatal(err)
	}
	original, err := ParseRequirementProjection("requirement.md", created)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(created), "## 方案\n\n使用现有归属。") || original.Requirements[0].ID != "REQ-001" || original.Requirements[0].AcceptanceConditions[0].ID != "AC-001" {
		t.Fatalf("initial document: %s", created)
	}
	edited, err := RenderSemanticChange("requirement.md", created, SemanticChange{Item: 1, Condition: 1, Acceptance: prose("可以重复读取。")})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(string(created), "AC-001：归档可读取。", "AC-001：可以重复读取。", 1)
	if string(edited) != want {
		t.Fatalf("edit changed unselected material\nwant: %q\ngot: %q", want, edited)
	}
	appended, err := RenderSemanticChange("requirement.md", edited, SemanticChange{Title: prose("查询"), Requirement: prose("查询原归档。"), Acceptance: prose("找到内容。"), Source: prose("用户请求。")})
	if err != nil {
		t.Fatal(err)
	}
	document, err := ParseRequirementProjection("requirement.md", appended)
	if err != nil || document.Requirements[0].ID != "REQ-001" || document.Requirements[1].ID != "REQ-002" || document.Requirements[1].AcceptanceConditions[0].ID != "AC-002" {
		t.Fatalf("stable identities: %+v %v", document, err)
	}
}

func TestSemanticEditsPreserveAssignedIdentitiesAndRejectInvalidChanges(t *testing.T) {
	edited, err := RenderSemanticChange("requirement.md", []byte(semanticDocument), SemanticChange{Item: 1, Requirement: prose("保留身份的新语义。")})
	if err != nil {
		t.Fatal(err)
	}
	doc, err := ParseRequirementProjection("requirement.md", edited)
	if err != nil || doc.Requirements[1].Requirement != "Read the original archived content." || doc.Requirements[1].AcceptanceConditions[0].ID != "AC-003" {
		t.Fatalf("inherited material: %+v %v", doc, err)
	}
	removed, err := RenderSemanticChange("requirement.md", edited, SemanticChange{Item: 1, Remove: true})
	if err != nil {
		t.Fatal(err)
	}
	doc, err = ParseRequirementProjection("requirement.md", removed)
	if err != nil || len(doc.Requirements) != 1 || doc.Requirements[0].ID != "REQ-002" || doc.Requirements[0].AcceptanceConditions[0].ID != "AC-003" {
		t.Fatalf("removal changed surviving identities: %+v %v", doc, err)
	}
	for _, change := range []SemanticChange{{Item: 1, Condition: 2, RemoveCondition: true}, {Item: 1, Requirement: prose("")}, {Item: 1, Condition: 1, RemoveCondition: true}, {Item: 1, Title: prose("line\nheading")}} {
		if _, err := RenderSemanticChange("requirement.md", removed, change); err == nil {
			t.Fatalf("invalid change accepted: %+v", change)
		}
	}
}

func TestSemanticEditsPreserveUnchangedDocumentBytes(t *testing.T) {
	explicit := "# Request\n\n## 需求点\n\n### REQ-001： First  \n\n#### 要求\n\nOriginal body.\n\n#### 验收条件\n\n- AC-001： First condition.\n\n#### 来源\n\nOwner.\n\n### REQ-002： Second  \n\n#### 要求\n\nKeep body.\n\n#### 验收条件\n\n- AC-002： Keep condition.\n\n#### 来源\n\nOwner.\n"
	implicit := "# Request\r\n\r\n  ## 需求点\r\n\r\n  ###  First  \r\n\r\n #### 要求\r\n\r\nOriginal body.\r\n\r\n```markdown\r\n### Fenced heading\r\n#### 来源\r\n```\r\n\r\n #### 验收条件\r\n\r\n  -   First condition.  \r\n\r\n #### 来源\r\n\r\nOwner.\r\n\r\n### Second  \r\n\r\n#### 要求\r\n\r\nKeep body.\r\n\r\n#### 验收条件\r\n\r\n- Keep condition.\r\n\r\n#### 来源\r\n\r\nOwner."
	for _, source := range []string{explicit, implicit} {
		if _, err := ParseRequirementProjection("requirement.md", []byte(source)); err != nil {
			t.Fatal(err)
		}
		for _, test := range []struct {
			change   SemanticChange
			old, new string
		}{
			{SemanticChange{Item: 1}, "", ""},
			{SemanticChange{Item: 1, Requirement: prose("Changed body.")}, "Original body.", "Changed body."},
			{SemanticChange{Item: 1, Title: prose("Changed title")}, "First", "Changed title"},
			{SemanticChange{Item: 1, Condition: 1, Acceptance: prose("Changed condition.")}, "First condition.", "Changed condition."},
		} {
			got, err := RenderSemanticChange("requirement.md", []byte(source), test.change)
			if err != nil {
				t.Fatal(err)
			}
			want := source
			if test.old != "" {
				want = strings.Replace(source, test.old, test.new, 1)
			}
			if test.change.Requirement != nil && source == implicit {
				want = strings.Replace(want, "\r\n\r\n```markdown\r\n### Fenced heading\r\n#### 来源\r\n```", "", 1)
			}
			if string(got) != want {
				t.Fatalf("unselected source bytes changed\nwant=%q\ngot=%q", want, got)
			}
		}
	}
}

func TestSemanticSectionEditsPreserveOtherDocumentBytes(t *testing.T) {
	source := "# Request\n\n ## 背景与目标  \n\nOriginal context.\n\n```markdown\n## 方案\n```\n\n## 需求点\n\n### First  \n\n#### 要求\n\nOriginal body.\n\n#### 验收条件\n\n-   First condition.  \n\n#### 来源\n\nOwner.\n\n  ## 方案  \n\nOriginal solution.\n\n## Notes\n\nKeep notes."
	for _, original := range []string{source, strings.ReplaceAll(source, "\n", "\r\n")} {
		ending := "\n"
		if strings.Contains(original, "\r\n") {
			ending = "\r\n"
		}
		contextBody := "Original context." + ending + ending + "```markdown" + ending + "## 方案" + ending + "```"
		for _, test := range []struct {
			change SemanticChange
			want   string
		}{
			{SemanticChange{Solution: prose("Corrected solution.")}, strings.Replace(original, "Original solution.", "Corrected solution.", 1)},
			{SemanticChange{Context: prose("Corrected context.")}, strings.Replace(original, contextBody, "Corrected context.", 1)},
			{SemanticChange{Context: prose("Corrected context."), Solution: prose("Corrected solution.\nAnother paragraph.")}, strings.Replace(strings.Replace(original, contextBody, "Corrected context.", 1), "Original solution.", "Corrected solution."+ending+"Another paragraph.", 1)},
			{SemanticChange{Item: 1, Requirement: prose("Corrected body."), Solution: prose("Corrected solution.")}, strings.Replace(strings.Replace(original, "Original body.", "Corrected body.", 1), "Original solution.", "Corrected solution.", 1)},
		} {
			got, err := RenderSemanticChange("requirement.md", []byte(original), test.change)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != test.want {
				t.Fatalf("section edit changed other original bytes\nwant=%q\ngot=%q", test.want, got)
			}
		}
	}
}
