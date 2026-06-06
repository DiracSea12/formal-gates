# GitHub 仓库描述

别让 AI 给自己的代码盖章。5 道质量门 + 独立审查 AI，彻底杜绝自我背书。

---

# 项目简介

AI 代码质量门禁，专治方向跑偏、过度设计、假测试、悄悄缩需求。1 道事前门在动手前对齐需求，4 道事后门通过独立 AI 审查卡质量——禁止自我背书。支持 Claude Code、Codex 和 Cursor。

---

# 详细介绍

## 你遇到过的问题

让 AI 做个功能，它写完代码说"看起来不错！"你合并了，然后发现：
- 实现的根本不是你要的东西
- 本来 10 行能搞定，它造了 5 个新抽象
- 测试只检查变量存不存在，不验证功能对不对
- 一半需求悄悄没了

核心问题：**AI 在审查自己的代码，而 AI 和人一样，很难发现自己的错误。**

## formal-gates 怎么解决

强制执行一条铁律：**AI 不能批准自己的工作。**

- ✅ **事前拦截** 需求不清楚就不让开工（需求澄清门）
- ✅ **独立审查** 派不知道编码 AI 在想什么的零上下文审查 AI
- ✅ **四道关卡** 依次验证：测试、复杂度、架构、代码质量
- ✅ **机器强制** PowerShell 脚本校验门禁 artifact，假批准过不了

## 需求澄清门怎么工作

这是唯一在写代码**之前**运行的门，也是唯一会**自动触发**的门（写 OpenSpec/PRD/设计文档时）。

**检查 6 个关键点：**
1. **目标** - 要达成什么业务目标？
2. **用户价值** - 为谁解决什么问题？
3. **范围** - 包括什么？
4. **非目标** - 明确不做什么？
5. **验收标准** - 怎样算完成？
6. **架构边界** - 影响哪些模块？

**如果任何一项缺失到"只能靠猜"：**
- 状态：`DRAFT_BLOCKED`（草稿阻塞）
- 不允许开工
- 不允许默默填默认值

**输出：**
- 已确认的答案
- 未决问题清单
- 草稿/封板状态

**为什么重要：** 方向错了返工成本最高。在浪费 token 写错代码之前，先把目标对齐。

## 谁该用

**适合：** 用 AI 开发生产系统、大重构、新系统、整模块开发、发版前验证

**不适合：** 快速原型、UI 调整、小 bug 修复、单文件改 typo

## 实际效果

**之前：**
- "加个认证" → 15 个文件，3 个新抽象，测试只检查字段存不存在
- "重构 API" → 范围扩散到重新设计半个系统

**现在：**
- 需求澄清门在浪费 token 前抓住不清楚的目标
- 复杂度门拦住不必要的抽象
- QA 门要求真实证据
- 代码质量门抓 bug 和维护性问题

---

# 快速开始

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Claude -Scope Global -Force -RunCanary -ConfigureHook
```

然后告诉 AI："跑四门"或"封板前过一遍门禁"

---

# 微博 / Twitter

别让 AI 给自己的代码盖章。formal-gates 通过 5 道质量门强制独立审查——1 道在写代码前，4 道在写完后。支持 Claude Code、Codex 和 Cursor。https://github.com/DiracSea12/formal-gates

---

# 知乎 / 技术社区

AI 写代码很强，AI 审查自己的代码很弱。

formal-gates 用独立 AI 审查解决这个问题，通过 5 道质量门：
• 需求澄清（写代码前）
• 测试质量（真实证据，不是假断言）
• 复杂度控制（拦住过度设计）
• 架构健康（边界和所有权）
• 代码质量（正确性和可维护性）

核心规则：写代码的 AI 不能批准代码。零上下文审查 AI 验证每道门。PowerShell 脚本强制要求证据——不接受占位符批准。

支持 Claude Code、Codex 和 Cursor。开源。

https://github.com/DiracSea12/formal-gates

---

# V2EX / Reddit 帖子

**标题：** formal-gates：用独立审查门禁阻止 AI 给自己的代码盖章

**正文：**

如果你用 AI 写代码，大概见过这个套路：让 AI 做点东西 → AI 写完说"看起来不错！" → 你合并了 → 后来发现它做错了、过度设计了、或者测试是假的。

核心问题：AI 在审查自己的代码。而 AI（和人类一样）不擅长发现自己的错误。

**formal-gates** 强制独立审查：
- 写代码的 AI 永远不能判断代码是否通过
- 独立的"零上下文" AI 通过 5 道门验证
- PowerShell 脚本强制要求证据
- 机器层校验防止假批准

**1 道事前门：** 需求澄清——在开始写代码前对齐目标/范围/验收标准

**4 道事后门（按顺序）：**
- QA：真实的测试证据，不是断言作秀
- 复杂度：拦住范围扩散和过度设计
- 架构：验证边界和所有权
- 代码质量：抓 bug、边界情况、可维护性问题

支持 Claude Code、Codex 和 Cursor。为生产系统、重构和发版验证而生。

https://github.com/DiracSea12/formal-gates

---

# GitHub Topics

`ai-code-review` `code-quality` `claude-code` `quality-gates` `ai-development` `code-validation` `software-quality` `ci-cd` `development-tools` `cursor` `codex` `testing` `architecture` `refactoring`
