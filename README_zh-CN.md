# formal-gates

> AI 代码质量门禁：1 道事前门管方向 + 4 道事后门卡质量。AI 动手前先对齐需求，写完之后派**独立的审查 AI**逐道关卡卡质量，过了才放行。

这是给 Claude Code、Codex、Cursor 使用的 Agent Skill 包。三个宿主都可以安装完整包；但任何一个宿主要声称 command hook 真的会拦截错误门禁流程，都必须在该宿主上跑 live canary（真实启动验证）。

它不替你写代码，而是裁决"AI 要做的方向对不对、写完的代码/文档能不能放行"。

## 解决什么问题

AI 写代码有几个通病，这套门禁专门拦：

- **方向跑偏**——目标、范围、验收没对齐就开干，事后审得再严也是给做错的东西做精装修。
- **过度设计**——动不动造 Manager / Service / Provider / 各种抽象和"框架"。
- **假测试**——只断言"字段存在""非空字符串""日志里有某行"，而不是验证真实行为。
- **悄悄缩需求**——把用户要的范围改小，却不声明。
- **自我背书**——自己写完自己说"看起来不错"。

## 事前门：需求澄清门（动手前唯一的门，最省返工）

写 OpenSpec / PRD / SDD 等规范文档前，先对齐**目标、用户价值、范围、非目标、验收标准、架构边界**。任何一项缺失到会让文档"靠猜"，就停在 `DRAFT_BLOCKED`，不许默默填默认值。

这是**唯一会被 skill 主动触发**的门（写规范文档时自动先走，无需用户开口），也是唯一在 AI 动手**之前**拦的门——方向错了返工成本最高，所以它最重要。

## 四道事后门（AI 写完后审，按顺序，前一道不过不准进下一道）

1. **qa-test-gate（测试门）**——用例和验收标准是否可信，有没有 QA 自己拥有的真实证据。
2. **complexity-gate（复杂度门）**——改动有没有做大、过度工程、凭空造系统。
3. **architecture-health-gate（架构门）**——模块边界、所有权、依赖方向、状态/缓存生命周期有没有烂。
4. **code-quality-gate（代码质量门）**——正确性、边界、死代码、假测试、过拟合、可维护性。

## 核心机制：防 AI 自我背书

- 通过结论必须由**零上下文的独立审查 AI** 给出——它不知道主 AI 的结论和怀疑点，避免回声。
- 每道门的结论要落成 **artifact**，由 PowerShell 脚本在机器侧**强制校验**：缺字段、占位符（`<...>`/`todo`/`tbd`）、复用过期快照的旧结论，一律被 hook 拦截。
- 配好 hook 或使用 `gate-workflow.ps1` 记录时，主 AI 想"自己盖章放行"会被机器层挡住；没接 hook 时仍要显式运行脚本校验。

## 什么时候用 / 不用

| ✅ 用 | ❌ 不用 |
|------|--------|
| 大重构、新系统、整模块开发 | 改 UI 位置、修小 bug |
| 发版 / 封板前最终验收 | 普通聊天、查代码、措辞调整 |
| 写规范文档前的需求澄清 | 单文件 typo |

日常小改它会静默，不干扰。

## 环境要求

- Windows + PowerShell 5 或 7
- 版本控制：Git / SVN / 无 VCS（文件哈希快照）均可
- 复杂度脚本需 Python 3.x 或 2.7

## 安装

用包内脚本复制整个目录（不要只挑 SKILL.md）：

```powershell
# 装到全局 Claude skill
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Claude -Scope Global -Force -RunCanary

# 装到全局 Claude skill，并写入/更新 command hook
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Claude -Scope Global -Force -RunCanary -ConfigureHook

# 装到全局 Codex skill
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Codex -Scope Global -Force -RunCanary

# 给某个项目安装 Cursor hook 支持
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Cursor -Scope Project -ProjectPath <project> -Force -RunCanary -ConfigureHook

# 或装到某个 Claude 项目本地
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Claude -Scope Project -ProjectPath <project> -Force -RunCanary
```

`-RunCanary` 会在复制后跑一次可搬运性自检；失败就别把这次安装当可用。
Claude Code、Codex、Cursor 的 hook/config 接入口不同，所以每个宿主都必须单独安装、单独验证。不能用某一个宿主的 canary 通过，反向证明另一个宿主也会执行 hook。Claude/Codex/Cursor 的 hook 或脚本接入口径见 `references/install-and-hooks.md`。

## 怎么开始

跟 AI 说"跑四门""做 formal gate 审查""封板前过一遍门禁"即可触发。写规范文档时它会主动先走需求澄清门。日常小改不用管。

## Skill 行为检查

`examples/skill-behavior-prompts.json` 放的是只读测试 prompt，用来检查这个 skill 有没有真的改变 agent 行为。它覆盖：OpenSpec 前先需求澄清、阻止主代理直接做非平凡实现、拒绝自封 PASS、focused evidence 不能冒充 Final QA PASS、普通聊天或极小改动不误触发四门。

这些 prompt 可以给 Darwin 式 skill review 或人工 reviewer 使用。它们只检查 skill 自身行为，不是正式发版门禁，也不能替代 `scripts\test-portable-openspec-canary.ps1`。

## 包结构

```
formal-gates/
  SKILL.md                  # 入口（给 AI 读）：分流、红线、四门顺序、GateWorkflow 最小信息
  references/               # 按需加载的各门细则
    requirements-clarification-gate.md   # 需求澄清门
    qa-test-gate.md                      # 测试门
    complexity-gate.md                   # 复杂度门（含 Complexity Contract、预算）
    architecture-health-gate.md          # 架构门
    code-quality-gate.md                 # 代码质量门
    install-and-hooks.md                 # 安装/hook/canary/artifact 字段/记录命令
  scripts/                  # PowerShell + Python 门禁脚本
  hooks/                    # enforce-gate-sequence.ps1（机器侧顺序与字段强制）
  agents/                   # host 接入配置
  examples/                 # GateWorkflow、行为检查 prompt 等样例
  formal-gates.manifest.json # 包索引、host 支持边界、安装和验证命令
```

人看这个 README 上手；AI 从 `SKILL.md` 进入。各门具体判据按需读 `references/`。
