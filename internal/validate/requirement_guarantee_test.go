package validate

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
