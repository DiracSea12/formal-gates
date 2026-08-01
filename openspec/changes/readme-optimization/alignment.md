# Requirements Alignment

Date: 2026-08-01
Status: confirmed

## 对齐结论

- **文档分层**：README 是唯一的人类文档；SKILL.md、references/*、prompts/、
  gates/ 是安装给 AI 代理使用的 skill 包，二者不耦合。README 不得要求人类读者
  阅读 AI 文档才能理解产品。
- **范围**：README.md 与 README_EN.md 两版同步重构 + 首屏真实 demo。不做 CI
  守卫作业，不切 release。
- **路线**：full（内置 QA + complexity-gate + implementation-quality-gate）。
- **实现方式**：开发由独立 dev worker session 完成，保持实现与审查分离。

## RQ-001 - README 对人类自足

README.md 与 README_EN.md SHALL 对纯人类读者完全自足：不依赖阅读 SKILL.md、
references/*、prompts/、gates/ 中的任何一份才能理解产品是什么、怎么用、会得到
什么。可提及这些是安装给 AI 代理使用的 skill 包，但不得作为理解 README 的前置。

## RQ-002 - 双语一致

README.md 与 README_EN.md SHALL 章节结构一致、内容互译忠实、术语一一对应。

## RQ-003 - AI 操作内容移出 README

README SHALL NOT 包含 AI 操作手册内容：完整 workflow 子命令 CLI 走查（20 个命
令及 flags）、P0/P1/P2 操作语义、carry 判定规则、任务切片机制、QA 生命周期操作
规则、result 校验规则、VCS 命令表。这些内容由 SKILL.md 与 references/* 持有。
README 只保留人类语言的「它是怎么工作的」概述。

## RQ-004 - 人类使用路径

README SHALL 清楚说明人类如何使用：安装 → 由 AI 代理（读取已安装的 skill）驱动
正式流程 → 人类审阅结果与封板摘要。SHALL NOT 再出现供人类手输的 workflow CLI 走
查序列。

## RQ-005 - 修复事实错误

README SHALL NOT 出现 `~/.config/formal-gates` 或其他不存在的安装/包路径。涉及
路径、命令、flags 的表述 SHALL 与真实 CLI 帮助与真实安装位置一致。

## RQ-006 - 安装 / 位置 / 卸载

README SHALL 包含：安装（release 与源码两条路径）、安装位置矩阵（claude / codex
/ cursor × global / project）、写入的文件（各 host 的 hook 配置）、手动卸载方法
（删除 skill 目录 + 清除 hook 条目）、`--force` 语义、既有非 formal-gates hook
不受影响。

## RQ-007 - 状态声明

README SHALL 声明当前 v0.1.0 prerelease 状态，注明文档化流程以仓库为准，并给出
GitHub Releases 链接。

## RQ-008 - 示例产物

README SHALL 保留自定义门 md 示例（门是用户自己会写的文件）。封板摘要 SHALL 用
人类语言说明其内容（哪些门通过、finding 的严重级与位置、QA 结果），SHALL NOT 展
示 JSON 结构字段。

## RQ-009 - 术语可理解

README SHALL 让首次出现的术语（封板、快照、路由、P0/P1/P2、零上下文等）在上下
文中即可理解，或以简短术语表提供。

## RQ-010 - 首屏与真实产物证据

README 首屏 SHALL 以价值句与紧凑流程示意开头，不以大段 CLI 转录或命令回放开场。
示例产物节 SHALL 以真实封板摘要 JSON 作证据，SHALL NOT 保留长命令转录。SHALL NOT
编造输出；示例字段必须来自真实执行。

## RQ-011 - 文字与排版

两版 SHALL 统一 H2 分隔策略；如保留命令示例，修 qa-review 的引号问题；中英文平
行结构、术语大小写统一、无翻译腔。

## RQ-012 - 本地校验与贡献

README SHALL 保留人类可执行的本地校验命令（go test ./... 等）与贡献、许可信息。

## RQ-013 - 价值机制

README 的 Why 节 SHALL 用人类语言解释「独立 session 消除盲区」的机制，而非仅陈
述痛点。
