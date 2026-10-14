package requirement

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"reflect"
	"strings"
	"testing"
)

const semanticDocument = "# Requirement and solution\n\n## 需求点\n\n### Archive completed work\n\n#### 要求\n\nMove completed work and preserve pending work.\n\n#### 验收条件\n\n- Completed work appears in the archive.\n- Pending work remains active.\n\n#### 来源\n\nUser request.\n\n### Read archived work\n\n#### 要求\n\nRead the original archived content.\n\n#### 验收条件\n\n- The archived content is exact.\n\n#### 来源\n\nUser request.\n"

func multilineConditionDocument(indent, identity string) (string, string) {
	condition := indent + "- " + identity + "Completed work appears in the archive.  \n" + indent + "  Repeating the operation retains the same entry.\n\n" + indent + "  - AC-001：Nested detail.\n" + indent + "    Keep the nested continuation.\n```markdown\n- AC-001：Example only\n#### 来源\n```"
	source := strings.Replace(semanticDocument, "- Completed work appears in the archive.", condition, 1)
	source = strings.Replace(source, "- Pending work remains active.", indent+"- Pending work remains active.\n"+indent+"  Preserve pending content.", 1)
	source = strings.Replace(source, "## 需求点", "## 背景与目标\n\nKeep the complete request.\n\n## 需求点", 1) + "\n## 方案\n\nReuse the archive owner.\n"
	return source, condition
}

func TestRequirementProjectionPreservesCompleteDirectConditions(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		for spaces := 0; spaces <= 3; spaces++ {
			for _, identity := range []string{"", "AC-010："} {
				t.Run(fmt.Sprintf("ending=%q/indent=%d/identity=%q", ending, spaces, identity), func(t *testing.T) {
					indent := strings.Repeat(" ", spaces)
					source, condition := multilineConditionDocument(indent, identity)
					document, err := ParseRequirementProjection("requirements.md", []byte(strings.ReplaceAll(source, "\n", ending)))
					if err != nil {
						t.Fatal(err)
					}
					conditions := document.Requirements[0].AcceptanceConditions
					if len(conditions) != 2 || len(document.Requirements[1].AcceptanceConditions) != 1 {
						t.Fatalf("nested items changed the direct condition count: %+v", document.Requirements)
					}
					wantText := "Completed work appears in the archive.\n" + strings.SplitN(condition, "\n", 2)[1]
					wantID, secondID, otherID := "AC-001", "AC-002", "AC-003"
					if identity != "" {
						wantID, secondID, otherID = "AC-010", "AC-001", "AC-002"
					}
					if conditions[0].Text != wantText || conditions[0].ID != wantID || conditions[1].ID != secondID || document.Requirements[1].AcceptanceConditions[0].ID != otherID || conditions[1].Text != "Pending work remains active.\n"+indent+"  Preserve pending content." {
						t.Fatalf("condition prose or identities changed: %+v", document.Requirements)
					}
					if document.Content != source {
						t.Fatal("confirmation material changed")
					}
					repeated, err := ParseRequirementProjection(document.Path, []byte(document.Content))
					if err != nil || !reflect.DeepEqual(document, repeated) {
						t.Fatal("reparsing changed complete conditions", err)
					}
				})
			}
		}
	}
}

func TestRequirementProjectionPreservesFencedParentFields(t *testing.T) {
	requirementText := "Emit this exact value:\n```json\n{\n  \"mode\": \"safe\",\n\n  \"count\": 2\n}\n```\nNo additional output."
	sourceText := "Confirmed template:\n~~~markdown\n## 需求点\n### REQ-001：Fenced requirement\n#### 要求\nKeep this as an example.\n#### 验收条件\n- AC-001：Fenced condition\n#### 来源\nFenced source.\n~~~\nOwner confirmation."
	source := strings.Replace(semanticDocument, "Move completed work and preserve pending work.", requirementText, 1)
	source = strings.Replace(source, "User request.", sourceText, 1)
	source = strings.TrimSuffix(source, "\n")
	document, err := ParseRequirementProjection("requirements.md", []byte(strings.ReplaceAll(source, "\n", "\r\n")))
	if err != nil {
		t.Fatal(err)
	}
	if document.Content != source || len(document.Requirements) != 2 {
		t.Fatalf("fenced examples changed confirmation material or requirement count: %+v", document)
	}
	first := document.Requirements[0]
	if first.ID != "REQ-001" || first.Requirement != requirementText || first.Source != sourceText {
		t.Fatalf("fenced parent fields lost content or changed identity: %+v", first)
	}
	conditions := first.AcceptanceConditions
	if len(conditions) != 2 || conditions[0].ID != "AC-001" || conditions[0].Text != "Completed work appears in the archive." || conditions[1].ID != "AC-002" || conditions[1].Text != "Pending work remains active." {
		t.Fatalf("fenced examples changed direct conditions: %+v", conditions)
	}
	points, links := ProjectPoints(document)
	wantPoints := []Point{
		{ID: "AC-001", RequirementID: "REQ-001", Requirement: requirementText, Source: sourceText, Text: "Completed work appears in the archive.", Oracle: "Completed work appears in the archive."},
		{ID: "AC-002", RequirementID: "REQ-001", Requirement: requirementText, Source: sourceText, Text: "Pending work remains active.", Oracle: "Pending work remains active."},
		{ID: "AC-003", RequirementID: "REQ-002", Requirement: "Read the original archived content.", Source: "User request.", Text: "The archived content is exact.", Oracle: "The archived content is exact."},
	}
	wantLinks := []SourcePoint{{SourceID: "REQ-001", PointID: "AC-001"}, {SourceID: "REQ-001", PointID: "AC-002"}, {SourceID: "REQ-002", PointID: "AC-003"}}
	if !reflect.DeepEqual(points, wantPoints) || !reflect.DeepEqual(links, wantLinks) {
		t.Fatalf("shared points lost confirmed parent fields or their binding: %+v %+v", points, links)
	}
}

func TestRequirementProjectionDistinguishesListPaddingFromNestedItems(t *testing.T) {
	for _, conditions := range []string{
		"-   First condition.\n  - Second condition.\n    - Nested second detail.\n - Third condition.\n   - Nested third detail.",
		"-\tFirst condition.\n  - Second condition.\n    - Nested second detail.\n - Third condition.\n   - Nested third detail.",
	} {
		source := strings.Replace(semanticDocument, "- Completed work appears in the archive.\n- Pending work remains active.", conditions, 1)
		document, err := ParseRequirementProjection("requirements.md", []byte(source))
		if err != nil {
			t.Fatal(err)
		}
		got := document.Requirements[0].AcceptanceConditions
		if len(got) != 3 || got[0].Text != "First condition." || got[1].Text != "Second condition.\n    - Nested second detail." || got[2].Text != "Third condition.\n   - Nested third detail." {
			t.Fatalf("list padding changed directness: %+v", got)
		}
	}
}

func TestRequirementProjectionAssignsMissingIdentitiesInDocumentOrder(t *testing.T) {
	data := strings.ReplaceAll(semanticDocument, "\n", "\r\n")
	document, err := ParseRequirementProjection("requirements.md", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Requirements) != 2 || document.Requirements[0].ID != "REQ-001" || document.Requirements[1].ID != "REQ-002" || document.Requirements[0].Title != "Archive completed work" {
		t.Fatalf("requirement identity/order: %+v", document.Requirements)
	}
	first, second := document.Requirements[0].AcceptanceConditions, document.Requirements[1].AcceptanceConditions
	if len(first) != 2 || len(second) != 1 || first[0].ID != "AC-001" || first[1].ID != "AC-002" || second[0].ID != "AC-003" || first[0].Text != "Completed work appears in the archive." || first[0].Line >= second[0].Line {
		t.Fatalf("acceptance identity, source text or hierarchy: %+v %+v", first, second)
	}
	sum := sha256.Sum256([]byte(semanticDocument))
	if document.Content != semanticDocument || document.ContentDigest != hex.EncodeToString(sum[:]) {
		t.Fatal("identity assignment changed confirmation material or digest")
	}
	repeated, err := ParseRequirementProjection(document.Path, []byte(document.Content))
	if err != nil || !reflect.DeepEqual(document, repeated) {
		t.Fatal("reparsing changed the projection", err)
	}
}

func TestRequirementProjectionPreservesSourceIdentitiesAndSkipsReservedNumbers(t *testing.T) {
	data := strings.Replace(semanticDocument, "### Read archived work", "### REQ-001：Read archived work", 1)
	data = strings.Replace(data, "- The archived content is exact.", "- AC-001：The archived content is exact.", 1)
	data += "\n## Example\n\n```markdown\n## 需求点\n### REQ-002：Example only\n#### 验收条件\n- AC-002：Example only\n```\n"
	document, err := ParseRequirementProjection("requirements.md", []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	if document.Requirements[0].ID != "REQ-002" || document.Requirements[1].ID != "REQ-001" || document.Requirements[0].AcceptanceConditions[0].ID != "AC-002" || document.Requirements[0].AcceptanceConditions[1].ID != "AC-003" || document.Requirements[1].AcceptanceConditions[0].ID != "AC-001" || document.Content != data {
		t.Fatalf("explicit source IDs or fence boundary changed: %+v", document)
	}
	addition := strings.Replace(semanticDocument, "### Archive completed work", "### REQ-002：Archive completed work", 1)
	addition = strings.Replace(addition, "### Read archived work", "### REQ-001：Read archived work", 1)
	addition = strings.Replace(addition, "- Completed work appears in the archive.", "- AC-002：Completed work appears in the archive.", 1)
	addition = strings.Replace(addition, "- Pending work remains active.", "- AC-003：Pending work remains active.", 1)
	addition = strings.Replace(addition, "- The archived content is exact.", "- AC-001：The archived content is exact.\n- Empty archive has no entries.", 1)
	revised, err := ParseRequirementProjection("requirements.md", []byte(addition))
	if err != nil || revised.Requirements[1].AcceptanceConditions[1].ID != "AC-004" {
		t.Fatal("new condition reused an explicit identity", err)
	}
}

func TestRequirementProjectionRejectsInvalidExplicitIdentitiesAndStructure(t *testing.T) {
	for _, data := range []string{
		strings.Replace(semanticDocument, "Move completed work and preserve pending work.", "```json\n{\"mode\":\"safe\"}\n```", 1),
		strings.Replace(semanticDocument, "User request.", "~~~markdown\nConfirmed template.\n~~~", 1),
		strings.Replace(semanticDocument, "### Archive completed work", "### REQ-01：Archive completed work", 1),
		strings.Replace(semanticDocument, "### Archive completed work", "### REQ-001：", 1),
		strings.Replace(strings.Replace(semanticDocument, "### Archive completed work", "### REQ-001：Archive completed work", 1), "### Read archived work", "### REQ-001：Read archived work", 1),
		strings.Replace(semanticDocument, "- Completed work appears in the archive.", "- AC-01：Completed work appears in the archive.", 1),
		strings.Replace(semanticDocument, "- Completed work appears in the archive.", "- AC-001：", 1),
		strings.Replace(strings.Replace(semanticDocument, "- Completed work appears in the archive.", "- AC-001：Completed work appears in the archive.", 1), "- Pending work remains active.", "- AC-001：Pending work remains active.", 1),
		strings.Replace(semanticDocument, "### Archive completed work", "###", 1),
		strings.Replace(semanticDocument, "- Completed work appears in the archive.\n- Pending work remains active.", "- ", 1),
		strings.Replace(semanticDocument, "#### 来源", "#### Undefined", 1),
	} {
		if _, err := ParseRequirementProjection("requirements.md", []byte(data)); err == nil {
			t.Fatalf("invalid explicit identity/structure accepted: %s", data)
		}
	}
}
