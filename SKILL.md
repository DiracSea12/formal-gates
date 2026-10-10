---
name: formal-gates
description: 用户明确要求走正式流程时才进入完整受理流程；用于 formal-gates 对齐、审查、发布、Seal、安装或正式诊断，支持不验证只留记录的轻量路线。非修改性的提问、解释、诊断和 review 不触发。项目内容修改默认提醒一次：若需走 formal-gates 流程，可直接提出；大/复杂需求在需求与方案确认、准备开做时再额外强调一次。
---

# Formal Gates

主代理只负责需求语义、事实核实、完整材料展示和真实用户决定。程序独占下一步、派发、顺序、轮次、恢复、机械绑定与结果接纳；使用当前 `workflow next` / `workflow drive`，以返回的 Ask、阻塞或完成为边界。

## 受理与确认

<formal-gates:host-instructions:start>
本段受理与提醒义务仅适用于直接接收用户任务的主代理；已由 engine 派发的开发者、审查者和执行者只完成当前 action，不重复受理、提醒或自行触发正式流程。

普通任务：创建、编辑、移动或删除项目内容时，默认提醒一次“若需走 formal-gates 流程，可直接提出”。常规工作在用户已授权范围内继续。需求规模、耦合、风险或验证复杂度超过普通直接处理能力时，在需求澄清与方案确认完毕、准备开做之际再额外强调一次：“检测到复杂需求，建议走 formal-gates 流程”。非修改性的提问、解释、诊断和 review 不触发正式流程；主代理不得自行触发。

新对话检查：主代理必须检查工作区 `.gates/engine/*/state.json` 与 `.gates/tmp/*/state.json`。发现无法对应当前用户会话的残留状态时，告知文件路径、run 状态及可能影响写入，并说明若需走 formal-gates 流程可直接提出。由用户决定推进完成、通过当前 typed Ask 授权的 reset/abort 及 engine 清理结束或删除对应状态文件；不得自行处置，也不得把残留状态解释为用户已启动正式流程。

用户明确要求走正式流程（或明确要求触发 formal-gates）时，直接进入完整受理：先澄清需求与重大技术选择，能由事实和既有决定解决的自行处理；无待澄清问题时也明确说明。提出方案不等于确认。把完整需求与方案直接写入唯一正式需求文件；先做确定性结构预检，通过后展示该文件全文与精确 revision，再单独请求并等待用户明确确认。确认后才以 typed intake receipt 启动开发 run。确认前不选择路线或派发后续子代理。修订时更新同一文件并重新预检、展示和确认。

确认后：在已授权范围内继续，不逐步重复请求许可；只有影响范围、公开行为、验收或主要架构的未决事项才补充澄清，流程规定的确认节点仍须完成。未明确模式时再选择 lightweight 或 regular；full/custom 在拆分决定之后确认。engine/CLI 是下一步、派发、恢复和结果接纳的权威，主代理提交事实与明确用户决定。
<formal-gates:host-instructions:end>

正式文件写完整目标、边界、方案、需求块、验收条目与来源。新条目只写内容，预检分配编号；已有外部 ID 保留。

```bash
formal-gates workflow start --precheck --root <project> --requirement <file> --save-candidate <candidate>
```

展示程序返回的全部 materials 全文及精确 revisions，取得真实确认后将原 candidate 文件交给 `start --intake-receipt`。不抄取或重拼身份、摘要、全文和收据。完整调用参数见[受理公共合同](references/formal-flow.md#启动前受理)。lightweight 使用同一预检加 `--route lightweight`，记录 unverified；不把它当验证通过。

## 活动任务

`workflow drive` 消费原签发职责；主代理不另建任务、补排步骤或手工恢复。遇用户决定时，核实并展示程序当前候选与原因，只提交语义选择和必要理由；程序保存完整确认对象，确认后沿原文件提交。选择方式见[公共控制入口](references/formal-flow.md#启动与-run-控制)。

请求用户处置 finding 前，逐项展示问题、正常路径错误、后果、建议、等级及定级理由；原身份由程序候选携带。按[审查契约](prompts/reviewer-base.md)独立核真实性、范围、严重度、因果和复现，不能把候选或未执行项当正式 PASS。
需求变化先按当前 Ask 冻结受影响工作，再修同一正式文件、重新预检、完整展示和确认。只澄清影响范围、公开行为、验收或主要架构的未决选择。

报告程序实际进展、阻塞和证据，skip/unverified 如实保留；真实模型调用逐次取得用户授权。

安装或发布维护仅在相应请求下使用[安装与 Hook](references/install-and-hooks.md)和[本地验证](references/local-validation.md)。
