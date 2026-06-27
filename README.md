# formal-gates

> 防止 AI 自写、自审、自测，最后自己宣布 PASS。

**formal-gates** 是给 AI 开发流程用的证据门禁系统。AI 动手前先对齐需求，写完后按顺序留下独立审查和机器可校验的证据。它不替你写代码，而是裁决"方向对不对、证据够不够、能不能放行"。

**内置安装目标：** Claude Code · Codex · Cursor

各宿主接入方式不同，具体行为以 live canary 为准。

**当前边界：** 这个仓库目前支持本地安装和本地验证。它还没有实现公开 registry、marketplace、`npx`、签名、provenance、checksum、attestation，或 release-trust 发行链路。

---

## 目录

- [我能做什么](#我能做什么)
- [它到底拦什么？](#它到底拦什么)
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

防止 AI 自写、自审、自测代码，最后自己宣布 PASS。

## 它到底拦什么？

AI 最容易犯的一个错：代码是它写的，测试是它说跑了，最后还是它自己宣布“PASS”。

**formal-gates 做的事很简单：没有证据，就不能记录 PASS。**

![No evidence, no PASS](assets/showcase/no-evidence-no-pass.svg)

### 这里的几个词是什么意思？

- **PASS**：某一道门允许继续往下走的结论。
- **Evidence**：真实测试、审查或验证留下的证据。
- **Artifact**：保存这份证据的文件，比如 QA 报告、代码质量审查报告、最终验证记录。
- **Gate**：一道审查门，比如 QA、复杂度、架构健康、代码质量。

换句话说，formal-gates 不相信一句“我测过了”。它要求 AI 把证据写成文件，再由命令校验这个文件能不能支撑 PASS。

### 为什么这有用？

因为它把“AI 自己觉得可以了”改成了三件可检查的事：

1. 有没有证据文件？
2. 证据文件字段是否完整？
3. 当前 workflow 和 snapshot 是否匹配？

如果缺证据、证据不完整、或者拿旧结论冒充新结论，PASS 记录会被拒绝。

想看最小例子，可以跑这个 demo：[最小 Self-PASS 阻断 Demo](examples/minimal-self-pass-block-demo.md)。

> 注意：hook decision 允许命令继续，不等于正式 PASS 已经成立。artifact 仍然必须真实存在，并通过 formal-gates 的 artifact 校验。

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

只有跟 AI 说"**跑四门**"、"**做 formal gate 审查**"或"**封板前过一遍门禁**"后，AI 才按 formal-gates 规则走门禁流程。机器层是否能拦截命令，必须看目标宿主的 hook 配置和同宿主 live canary 是否通过。

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
- **Dispatch prompt 污染检测**——系统自动检测并阻止包含"上一轮发现""刚修了""重点复查""预期答案"等锚定模式的派发 prompt，保证审查者独立判断。检测规则在 `hooks/pollution-patterns.json` 内按英文 regex 组和中文术语组配置，由 `formal-gates prompt validate` 执行。
- **跨 workflow 隔离**——每个 workflow 的门禁链必须完整，不能复用其他 workflow 的门禁结果。系统会递归验证所有前置门和传递依赖是否属于同一个 workflowId 和 changeSnapshot；扩展门还必须绑定同一个 manifest 路径和哈希。
- 每道门的结论落成 **artifact**，由 Go 校验器检查字段完整性。缺字段、占位符（`<...>`/`todo`/`tbd`）、复用过期快照的旧结论会被拒绝。
- 配好并在当前宿主实测通过的 hook 可以拦截违规命令；使用 `formal-gates workflow` / `formal-gates gate` 记录时，机器层会校验证据并拒绝不合格的门禁记录。

---

## 可见证据

第一次验证这个包时，先看两类结果：

```bash
# 本地结构、prompt、hook decide、workflow、receipt、install 自检
bin/formal-gates canary portable --root . --format json

# 只在验证 Codex 宿主拦截能力时运行；失败不代表 native 校验失败
bin/formal-gates canary codex-hook --worktree .
```

`portable canary` 是项目自身可控能力的主要证明。`codex-hook` 只证明当前 Codex 客户端是否真的调用 hook；它不通过时，仍然必须用显式的 `formal-gates workflow` / `formal-gates gate` 命令校验证据，不能宣称 Codex hook blocking proven。

同宿主 live canary 目前证明到这里：

| 宿主 | 已实测到的结果 | 还不能宣称 |
|------|----------------|------------|
| Claude Code 2.1.193 | 项目本地 hook 会拦截缺 artifact 的 PASS 记录命令；带 artifact 的命令会通过 hook decision。 | 不代表全局安装路径在 Windows 上已经无问题，也不代表 Codex。 |
| Cursor headless 2026.06.26-7079533 | 项目本地 hook 会拦截缺 artifact 的 PASS 记录命令；带真实 QA artifact 的记录命令可以成功写入 gate-state。 | 不代表所有 Cursor 版本，也不代表公开发行链路已经具备。 |
| Codex CLI 0.142.0 | 本地 native 校验可用；Codex hook 闭环拦截还没有被证明。 | 不能宣称 Codex hook blocking proven。 |

---

## 发行信任边界

当前包适合本地安装、本地校验和候选包验证；还不是带公开信任链的发行物。不要把本仓库当前状态描述成已经具备：

- 公开 registry 或 marketplace 分发；
- `npx` 一键远程安装；
- 二进制签名、checksum、provenance 或 attestation；
- 可由第三方独立验证的 release-trust 链路。

对外发布前，至少要补齐 release artifact、校验和、签名或来源证明，并把 `portable canary` 结果作为 release 证据保存。

---

## 安装

优先使用 native CLI 安装。不要只复制 `SKILL.md`；安装命令会复制运行时需要的 skill 子集。

```bash
# 装到全局 Claude Code
bin/formal-gates install --source . --host claude --scope global --force

# 装到全局 Claude Code，并写入 native command hook
bin/formal-gates install --source . --host claude --scope global --force --configure-hooks

# 装到某个项目的 Codex，并写入 native hook
bin/formal-gates install --source . --host codex --scope project --project <project> --force --configure-hooks

# 给某个项目安装 Cursor hook 支持
bin/formal-gates install --source . --host cursor --scope project --project <project> --force --configure-hooks
```

Windows 下命令名是 `bin/formal-gates.exe`。安装后可用 `bin/formal-gates(.exe) canary portable --root <formal-gates>` 做原生自检。

每个宿主必须单独安装、单独验证。一个宿主的 canary 通过，不代表另一个宿主也会执行 hook。

### Codex 注意

本包可以安装 Codex skill；加上 `-ConfigureHook` 时，安装脚本会写入 Codex `hooks.json`。Codex 的 hook 文件顶层必须只保留 `hooks`，不要写 `version`、`description` 等额外字段。

Codex hook 只能当辅助 guardrail，不能当硬门禁。当前本机 Windows + Codex CLI 0.142.0 实测中，`PreToolUse` 在 `/hooks` 里显示为 active/trusted，但 `codex exec` 和脚本启动的 Codex 命令执行仍走 `command_execution`，没有证明能闭环拦截命令。正式门禁必须显式跑 `formal-gates workflow` / `formal-gates gate` 并核对 artifact；只有同宿主 live canary 看到 `PreToolUse` payload 且非法命令被阻止，才能声明 Codex hook blocking proven。

---

## 环境要求

- **用户运行时**：只需要对应平台的 `formal-gates` 二进制和宿主应用；核心命令不要求 PowerShell、Bash、Python、Node 或 Git Bash。
- **开发 / CI**：需要 Go 1.22+ 来构建、测试和打包原生二进制。

---

## 跨平台校验

> **前置要求**：Go 1.22+，且 `go` 在 PATH 中（运行 `go version` 确认）。

本地可复现 demo 见 [`examples/package-validation-demo.md`](examples/package-validation-demo.md)。它先构建 `bin/formal-gates(.exe)`，再用这个二进制跑包校验和原生 portable canary。

```bash
# 校验包结构
bin/formal-gates package validate --root .

# 跑原生 portable canary
bin/formal-gates canary portable --root .

# 校验单个 artifact
bin/formal-gates artifact validate \
  --root . \
  --file .claude/gates/artifacts/<artifact>.md \
  --gate complexity-gate \
  --workflow-id <workflow-id> \
  --change-snapshot <snapshot>

# 校验 dispatch prompt 污染
bin/formal-gates prompt validate --root . --file <prompt.md>

# 基础 gate state 记录和准入检查
bin/formal-gates gate record --worktree <repo> --gate qa-test-gate --verdict PASS --mode formal --stage Execution --artifact <artifact.md> --workflow-id <workflow-id> --change-snapshot <snapshot>
bin/formal-gates gate verify-admission --worktree <repo> --gate complexity-gate --workflow-id <workflow-id> --change-snapshot <snapshot>
bin/formal-gates gate show --worktree <repo> --format json

# workflow 基础封装：snapshot、record-stage、verify-admission、final-verification、cleanup
bin/formal-gates workflow snapshot --worktree <repo> --vcs file-hash
bin/formal-gates workflow record-stage --worktree <repo> --gate qa-test-gate --verdict PASS --mode formal --stage Execution --artifact <artifact.md> --workflow-id <workflow-id> --change-snapshot <snapshot>
bin/formal-gates workflow verify-admission --worktree <repo> --gate complexity-gate --workflow-id <workflow-id> --change-snapshot <snapshot>
bin/formal-gates workflow final-verification --worktree <repo> --attempts-file <attempts.json> --output .claude/gates/artifacts/final-verification.json --workflow-id <workflow-id> --change-snapshot <snapshot>
bin/formal-gates workflow final-verification --worktree <repo> --attempts-file <attempts.json> --output .claude/gates/artifacts/final-verification.json --record-final-qa --final-qa-artifact .claude/gates/artifacts/final-qa-execution.md --actor <qa-reviewer> --workflow-id <workflow-id> --change-snapshot <snapshot>
bin/formal-gates workflow cleanup --worktree <repo> --dry-run
```

Windows 下命令名是 `bin/formal-gates.exe`。源码 checkout 做开发测试时，可临时用 `go run ./cmd/formal-gates`；安装后的 hook 和校验路径必须使用 `bin/formal-gates(.exe)`。

这个 native CLI 已有包结构校验、artifact 字段校验、dispatch prompt 污染校验、native install、hook decide、基础 gate state 检查、workflow snapshot / record-stage / verify-admission / final-verification / cleanup、从已有 artifact 记录 FinalExecution、receipt 注册 / 捕获 / finalize / validate / preflight、portable canary 和 Codex hook canary。它仍不是完整工作流引擎、agent 运行时、持久报告系统、缓存系统或发版可信证明系统；receipt-sensitive 端到端编排、宿主 hook 闭环证明和 release-trust 链路仍需单独证据。

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
  bin/                      # 本地构建出的 native CLI，不提交到 git
  cmd/                      # Go native CLI 源码
  internal/                 # Go 核心实现
  hooks/                    # dispatch prompt 污染检测规则
  agents/                   # 独立门禁审查 agent 提示词
  examples/                 # GateWorkflow、行为检查 prompt 样例
  formal-gates.manifest.json # 包索引和安装配置
```

人看这个 README 上手；AI 从 `SKILL.md` 进入。各门具体判据按需读 `references/`。
`examples/sample-*.json` 和 `examples/sample-*.md` 只作结构参考；正式记录必须由 `formal-gates gate` / `formal-gates workflow` 命令生成，不能直接复制样例文件。

> 当前只支持本地安装和本地验证；不提供公开 registry、marketplace、`npx`、签名、provenance、checksum、attestation 或 release-trust 发行证明。

---

## 许可证

本项目基于 **MIT 许可证**开源。详情见 [LICENSE](LICENSE) 文件。

---

## 更新日志

详细版本历史和变更记录见 [CHANGELOG.md](CHANGELOG.md)。
