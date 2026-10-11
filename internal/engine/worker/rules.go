package worker

import (
	"strings"

	"formal-gates/internal/engine/coverage"
	"formal-gates/internal/engine/protocol"
)

// roleRules selects the current role sections from the installed catalog.
func roleRules(content string, input protocol.WorkerInput) string {
	qa := strings.HasPrefix(input.Handler, "engine.qa.")
	whitebox := qa && input.Validation != nil && input.Validation.Manifest.Binding.ReviewKind == coverage.ReviewWhitebox
	lines := strings.Split(content, "\n")
	selected := make([]string, 0, len(lines))
	for _, line := range lines {
		if strings.HasPrefix(line, "QA 正常可达前置") && !qa {
			continue
		}
		if qa {
			if strings.HasPrefix(line, "黑盒依据真实公开入口观察") && whitebox {
				continue
			}
			if strings.HasPrefix(line, "白盒依据本轮指定结构测试") && !whitebox {
				continue
			}
			if strings.HasPrefix(line, "白盒逐项读 reviewBase") && !whitebox {
				continue
			}
			if strings.HasPrefix(line, "黑盒在从基线") && whitebox {
				continue
			}
			if (strings.HasPrefix(line, "白盒在开发后") || strings.HasPrefix(line, "白盒测试文件")) && !whitebox {
				continue
			}
			if strings.HasPrefix(line, "黑盒在基线隔离区审") {
				black, white, _ := strings.Cut(line, "；")
				if whitebox {
					line = white
				} else {
					_, preparation, _ := strings.Cut(white, "测试准备")
					line = black + "。测试准备" + preparation
				}
			}

			if strings.HasPrefix(line, "白盒先独立列行为") {
				if whitebox {
					line = strings.Split(line, "黑盒不能")[0]
				} else {
					_, line, _ = strings.Cut(line, "黑盒不能")
					line = "黑盒不能" + line
				}
			}
			if strings.HasPrefix(line, "白盒 case/shared-step") && !whitebox {
				continue
			}
		}
		selected = append(selected, line)
	}
	return strings.TrimSpace(strings.Join(selected, "\n"))
}
