package validate

import (
	"fmt"

	"strings"
)

const whitebox35AFixR2TestFile = "internal/validate/whitebox_3_5a_fix_r2_test.go"

func whitebox35AFixR2RequirementDocument(requirementCount int) string {
	var out strings.Builder
	out.WriteString("# Formal requirement\n\nNatural-language background remains unrestricted.\n\n## 需求点\n")
	for index := 0; index < requirementCount; index++ {
		reqNumber := 1 + index*7
		acNumber := 1 + index*11
		fmt.Fprintf(&out, "\n### REQ-%03d：Requirement %d\n\n#### 要求\n\nMandatory behavior %d.\n\n#### 验收条件\n\n- AC-%03d：Observable result %d.\n\n#### 来源\n\nConfirmed input %d.\n", reqNumber, index+1, index+1, acNumber, index+1, index+1)
	}
	out.WriteString("\n## 方案\n\nNatural-language solution notes.\n")
	return out.String()
}
