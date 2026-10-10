package requirement

import (
	"crypto/sha256"
	"encoding/hex"
	"reflect"
	"strings"
	"testing"
)

const semanticDocument = "# Requirement and solution\n\n## 需求点\n\n### Archive completed work\n\n#### 要求\n\nMove completed work and preserve pending work.\n\n#### 验收条件\n\n- Completed work appears in the archive.\n- Pending work remains active.\n\n#### 来源\n\nUser request.\n\n### Read archived work\n\n#### 要求\n\nRead the original archived content.\n\n#### 验收条件\n\n- The archived content is exact.\n\n#### 来源\n\nUser request.\n"

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
