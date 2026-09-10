package requirements

import (
	"strings"
	"testing"
)

func phase4LineOf(doc, marker string) int {
	lines := strings.Split(doc, "\n")
	for i, line := range lines {
		if strings.Contains(line, marker) {
			return i + 1
		}
	}
	return 0
}

func TestPhase4ParserMissingStructuralInputs(t *testing.T) {
	cases := []struct {
		name        string
		doc         string
		wantRule    string
		wantReqLine string
		wantReqID   string
	}{
		{
			name: "empty source",
			doc: `## 需求点

### REQ-001：输出

#### 要求
执行公开命令。

#### 验收条件
- AC-001：退出码为 0。

#### 来源

`,
			wantRule:  "来源 field must contain non-empty prose",
			wantReqID: "REQ-001",
		},
		{
			name: "no acceptance conditions",
			doc: `## 需求点

### REQ-001：输出

#### 要求
执行公开命令。

#### 验收条件

#### 来源
用户需求。
`,
			wantRule:  "验收条件 must contain at least one direct - AC-nnn：... list item",
			wantReqID: "REQ-001",
		},
		{
			name: "duplicate direct field",
			doc: `## 需求点

### REQ-001：输出

#### 要求
第一次要求。

#### 要求
重复要求。

#### 验收条件
- AC-001：退出码为 0。

#### 来源
用户需求。
`,
			wantRule:  "duplicate direct #### 要求 field",
			wantReqID: "REQ-001",
		},
		{
			name: "same level boundary does not bleed fields",
			doc: `## 需求点

### REQ-001：输出一

#### 要求
第一项要求。

### REQ-002：输出二

#### 要求
第二项要求。

#### 验收条件
- AC-002：退出码为 0。

#### 来源
用户需求。
`,
			wantRule:    "missing direct #### 验收条件 field",
			wantReqLine: "REQ-002",
			wantReqID:   "REQ-001",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseRequirementProjection("phase4-missing.md", []byte(tc.doc))
			if err == nil {
				t.Fatal("missing structural input was accepted")
			}
			message := err.Error()
			if !strings.Contains(message, "phase4-missing.md:") {
				t.Fatalf("diagnostic has no path/line: %v", err)
			}
			if !strings.Contains(message, "["+tc.wantReqID+"]") {
				t.Fatalf("diagnostic has no recognizable REQ ID: %v", err)
			}
			if !strings.Contains(message, tc.wantRule) {
				t.Fatalf("diagnostic=%v, want rule %q", err, tc.wantRule)
			}
			if tc.wantReqLine != "" {
				boundary := phase4LineOf(tc.doc, "### "+tc.wantReqLine+"：")
				if !strings.Contains(message, "phase4-missing.md:"+itoaPhase4(boundary)+" ["+tc.wantReqID+"]") {
					t.Fatalf("diagnostic=%v, want same-level boundary line %d for %s", err, boundary, tc.wantReqID)
				}
				if strings.Contains(message, "["+tc.wantReqLine+"]") && strings.Contains(message, "missing direct") {
					t.Fatalf("boundary fields leaked into the next REQ: %v", err)
				}
			}
		})
	}
}

func itoaPhase4(n int) string {
	if n == 0 {
		return "0"
	}
	digits := []byte{}
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}
	return string(digits)
}
