# Proposal

当前 README.md 与 README_EN.md 是一份「人类 + AI 混合文档」：既承载人类着陆页
（价值、特性、安装），又把大量 AI 操作手册内容内联进人类文档——20 个 workflow
子命令的完整 CLI 走查、P0/P1/P2 操作语义、carry 判定规则、任务切片机制、result
校验规则、本地校验命令。评审（联网调研 + 四维评估）确认其中存在开箱即用失败的事
实错误（`--package-root ~/.config/formal-gates` 等）与结构缺陷。

本次变更把 README 重塑为对纯人类读者完全自足的着陆页。对齐结论：README 是唯一的
人类文档；SKILL.md、references/*、prompts/、gates/ 是 AI 代理使用的 skill 包，
二者不耦合。AI 操作内容移回这些 AI 文档（内容已存在，无信息丢失），人类只看到价
值、特性、安装/卸载、使用方式、示例产物、FAQ 与范围；同时修复已确认的事实错误、
补齐卸载说明与状态声明、补首屏真实 demo。
