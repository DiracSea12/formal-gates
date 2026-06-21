# formal-gates

> 防止 AI 自写、自审、自测，最后自己宣布 PASS。

**formal-gates** 是给 AI 开发流程用的证据门禁系统。AI 动手前先对齐需求，写完后按顺序留下独立审查和机器可校验的证据。它不替你写代码，而是裁决"方向对不对、证据够不够、能不能放行"。

**内置安装目标：** Claude Code · Codex · Cursor

各宿主接入方式不同，具体行为以 live canary 为准。

**当前边界：** 这个仓库目前支持本地安装和本地验证。它还没有实现公开 registry、marketplace、`npx`、签名、provenance、checksum、attestation，或 release-trust 发行链路。

---

## 目录

- [我能做什么](#我能做什么)
- [解决什么问题](#解决什么问题)
- [四道门怎么走](#四道门怎么走)
- [核心机制](#核心机制)
- [安装](#安装)
- [环境要求](#环境要求)
- [跨平台校验](#跨平台校验)
- [包结构](#包结构)
- [许可证](#许可证)
- [更新日志](#更新日志)

---

## 一句话体验

在仓库根目录下，复制以下命令体验只读校验（需要 Go 1.22+，且 go 在 PATH 中）：

```powershell
go run ./cmd/formal-gates-validate package --root .
```

安装到当前 Claude Code 项目：`scripts\install-formal-gates.ps1 -HostName Claude -Scope Project -ProjectPath . -Force -RunCanary`

需要门禁时，告诉 AI："跑四门" 或 "封板前过一遍门禁"

---

## 我能做什么

| 你想做的事 | 走哪道门 |
|-----------|---------|
| 想在写 OpenSpec / PRD / SDD 之前先对齐需求 | **需求澄清门** |
| 写完代码，想验证测试用例够不够 | **qa-test-gate** |
| 担心改动做了太多、过度工程 | **complexity-gate** |
| 想检查模块边界和依赖方向 | **architecture-health-gate** |
| 想检查代码正确性、死代码、假测试 | **code-quality-gate** |
| 发版 / 封板前最终验收 | 跑完整四门 |

只有跟 AI 说"**跑四门**"、"**做 formal gate 审查**"或"**封板前过一遍门禁**"后，AI 才按 formal-gates 规则走门禁流程。机器层强制拦截仍要看目标宿主的 hook 配置和 live canary 是否通过。

| 场景 | 是否触发门禁 |
|------|------------|
| 大重构、新系统 | 否，除非用户要求跑门禁 |
| 封板前验收 | 是，但也要用户明确要求封板或跑四门 |
| 写 OpenSpec / PRD / SDD 前 | 否；需求澄清门是可选的开发前审查 |
| 改 UI 位置、修小 bug | 否 |
| 普通聊天、措辞调整 | 否 |

---

## 解决什么问题

AI 写代码有几个通病，这套门禁专门拦：

- **方向跑偏**——目标、范围、验收没对齐就开干，事后审得再严也是给做错的东西做精装修。
- **过度设计**——动不动造 Manager / Service / Provider / 各种抽象和"框架"。
- **假测试**——只断言"字段存在""非空字符串""日志里有某行"，而不是验证真实行为。
- **悄悄缩需求**——把用户要的范围改小，却不声明。
- **自我背书**——自己写完自己说"看起来不错"。

---

## 四道门怎么走

### 两种审查流程

**开发前审查（Pre-development）**：可选地审查 OpenSpec / PRD / 设计文档
- 流程：requirements-clarification → complexity → architecture → cold-water
- **不需要 QA 门**（还没有代码和测试）
- 目标：在用户要求时，确认需求清晰、方向正确、架构合理、可以开工

**开发后审查（Post-development）**：用户要求后，审查代码实现
- 流程：QA → complexity → architecture → code-quality
- **必须先过 QA 门**（验证测试和证据）
- 目标：确认实现正确、测试充分、代码质量达标

如果用户已经主动开启 formal-gates，系统会按当前 artifact 判断走开发前流程还是开发后流程。没有用户要求时，不自动进入门禁。

### 需求澄清门（动手前先走的门）

如果用户要求正式需求澄清，先对齐**目标、用户价值、范围、非目标、验收标准、架构边界、需求细节**。任何一项缺失到会让文档"靠猜"，就停在 `DRAFT_BLOCKED`，不许默默填默认值。

需求细节包括：具体业务规则、边界条件、异常情况、数据约束、场景细节、非功能指标。只对齐高层目标不够——开发到一半才发现细节理解不一致，返工成本更高。

这是最适合在 AI 动手之前执行的门——方向错了返工成本最高。但它仍是用户可选项，不是默认强制流程。

### 四道事后门（用户要求后才跑，按顺序，前一道不过不准进下一道）

1. **qa-test-gate** —— 用例和验收标准是否可信，有没有真实证据。
2. **complexity-gate** —— 改动有没有做大、是否是完成目标所需的最小实现、有没有过度工程或凭空造系统。
3. **architecture-health-gate** —— 模块边界、所有权、依赖方向、状态/缓存生命周期、性能形态有没有烂。
4. **code-quality-gate** —— 正确性、边界、性能、死代码、假测试、可维护性。

---

## 核心机制

- 通过结论必须由**零上下文的独立审查 AI** 给出——它不知道主 AI 的结论和怀疑点，避免回声。
- **Dispatch prompt 污染检测**——系统自动检测并阻止包含"上一轮发现""刚修了""重点复查""预期答案"等锚定模式的派发 prompt，保证审查者独立判断。检测规则包括 14 个英文 regex 模式和 13 个中文术语，配置在 `hooks/pollution-patterns.json`。
- **跨 workflow 隔离**——每个 workflow 的门禁链必须完整，不能复用其他 workflow 的门禁结果。系统会递归验证所有前置门和传递依赖是否属于同一个 workflowId 和 changeSnapshot。
- 每道门的结论落成 **artifact**，由 Go 校验器检查字段完整性。缺字段、占位符（`<...>`/`todo`/`tbd`）、复用过期快照的旧结论会被拒绝。
- 配好并在当前宿主实测通过的 hook，或使用 `gate-workflow.ps1` 记录时，主 AI 想"自己盖章放行"会被机器层挡住。

---

## 安装

本地安装入口在 `scripts\install-formal-gates.ps1`。不要只复制 `SKILL.md`；安装脚本会复制运行时需要的 skill 子集。

```powershell
# 装到全局 Claude Code
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Claude -Scope Global -Force -RunCanary

# 装到全局 Claude Code，并写入 command hook
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Claude -Scope Global -Force -RunCanary -ConfigureHook

# 装到全局 Codex
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Codex -Scope Global -Force -RunCanary

# 给某个项目安装 Cursor hook 支持
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Cursor -Scope Project -ProjectPath <project> -Force -RunCanary -ConfigureHook

# 装到某个 Claude Code 项目本地
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\install-formal-gates.ps1 -HostName Claude -Scope Project -ProjectPath <project> -Force -RunCanary
```

`-RunCanary` 会在复制后跑 canary，验证 skill 文档可读性和路径可达性；失败就别把这次安装当可用。

每个宿主必须单独安装、单独验证。一个宿主的 canary 通过，不代表另一个宿主也会执行 hook。

### Codex 注意

本包可以安装 Codex skill；加上 `-ConfigureHook` 时，安装脚本会写入 Codex `hooks.json`。Codex hook 只能当辅助 guardrail，不能当硬门禁。正式门禁必须显式跑 `gate-workflow.ps1` 并核对 artifact；Codex hook 是否真的闭环拦截，仍要用同宿主 live canary 证明。

---

## 环境要求

- **Go 1.22+**：跨平台包校验和 artifact 校验
- **Windows + PowerShell 5 或 7**：安装、hook、canary 脚本
- **Python 3.x 或 2.7**：复杂度分析脚本（可选）

macOS 和 Linux 做包结构和 artifact 校验时只需 Go；安装、hook、canary 当前仍是 Windows PowerShell 路径。

---

## 跨平台校验

> **前置要求**：Go 1.22+，且 `go` 在 PATH 中（运行 `go version` 确认）。

本地可复现 demo 见 [`examples/package-validation-demo.md`](examples/package-validation-demo.md)。它会跑 Go 包校验和 portable OpenSpec canary，并把本机输出写到被忽略的 `.artifacts/tmp/package-validation-demo-output.txt`。

> demo 输出是本机生成物，不随包跟踪。如需写到其他位置，可传 `-OutputPath`。

```bash
# 校验包结构
go run ./cmd/formal-gates-validate package --root .

# 校验单个 artifact
go run ./cmd/formal-gates-validate artifact \
  --root . \
  --file .claude/gates/artifacts/<artifact>.md \
  --gate complexity-gate \
  --workflow-id <workflow-id> \
  --change-snapshot <snapshot>
```

这个校验器只做确定性的包结构和 artifact 字段检查。它不是工作流引擎、agent 运行时或发版可信证明系统。

---

## 包结构

```
formal-gates/
  SKILL.md                  # 入口（给 AI 读）：分流、红线、四门顺序
  references/               # 各门细则（按需加载）
    requirements-clarification-gate.md
    qa-test-gate.md
    complexity-gate.md
    architecture-health-gate.md
    code-quality-gate.md
    install-and-hooks.md
  scripts/                  # PowerShell + Python 门禁脚本
  cmd/                      # Go 跨平台校验 CLI
  internal/                 # Go 校验实现
  hooks/                    # enforce-gate-sequence.ps1
  agents/                   # 独立门禁审查 agent 提示词
  examples/                 # GateWorkflow、行为检查 prompt 样例
  formal-gates.manifest.json # 包索引和安装配置
```

人看这个 README 上手；AI 从 `SKILL.md` 进入。各门具体判据按需读 `references/`。

> 当前只支持本地安装和本地验证；不提供公开 registry、marketplace、`npx`、签名、provenance、checksum、attestation 或 release-trust 发行证明。

---

## 许可证

本项目基于 **MIT 许可证**开源。详情见 [LICENSE](LICENSE) 文件。

---

## 更新日志

详细版本历史和变更记录见 [CHANGELOG.md](CHANGELOG.md)。
