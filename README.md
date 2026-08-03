# formal-gates

**一套基于实现者、审查者与测试者强制分离的轻量开发审查工作流：从需求对齐到封板，每步都有标准化流程、门禁与记录**

[English](README_EN.md) | [中文](README.md)

[![CI](https://github.com/DiracSea12/formal-gates/actions/workflows/portable-validation.yml/badge.svg)](https://github.com/DiracSea12/formal-gates/actions/workflows/portable-validation.yml)
[![Go 1.22+](https://img.shields.io/badge/go-1.22+-blue.svg)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

---

formal-gates 是一套完整的 AI 辅助开发工作流：需求对齐、路由选择、QA设计、开发、
快照、QA测试、独立审查、返修、封板，每步都有标准化流程、门禁与记录。

审查环节在相互不可见的独立会话里进行：审查者只拿需求原文和原生 VCS 的 diff
（零上下文），无法用写码时的记忆填补审查。CLI 是唯一记录，封板时把整轮结论压缩成
一份不依赖任何会话记忆的摘要。

```text
用户
  │
  ▼
主代理（编排者）──► CLI 记录（.gates/）
  │
  ├─► Dev Worker   独立会话 · 零上下文 · 只拿需求 + VCS diff
  ├─► Gate A / B   独立会话 · 零上下文 · 只看 VCS diff
  └─► QA Executor  独立会话 · 真实执行已批准用例
```

一句话：把「写的人」和「审的人」以及「测的人」从同一颗脑子里拆开，再把结论写成一份不依赖任何锚定记忆的记录。

---

## 目录

- [为什么需要它](#为什么需要它)
- [特性](#特性)
- [它是怎么工作的](#它是怎么工作的)
- [正式流程](#正式流程)
- [示例产物](#示例产物)
- [安装与卸载](#安装与卸载)
- [使用方式](#使用方式)
- [常见问题](#常见问题)
- [当前状态](#当前状态)
- [本地校验、贡献与许可](#本地校验贡献与许可)

---

## 为什么需要它

同一个 AI 既写代码、又测试、审查自己写的代码，会带来两个问题：

- **结构性盲区**。审查者和实现者共享同一份记忆与上下文。AI 可以用"我写的时候就是这么想的"来填补审查，而不是只依据变更本身判断——本该被发现的错误，就这样被当成理所当然。
- **不留记录**。没有独立的审查，也没有可追溯的记录。一个 AI会话结束，它的实现依据、审查结论和通过理由就全都丢失。

formal-gates 提供一套完整的ai-coding开发工作流，并把实现、QA 和审查放进**相互不可见**的独立会话。审查者会话只拿到需求原文和原生 VCS 的 diff（零上下文：看不到实现者的思考过程、观点、对话历史或任何现场状态），它无法用写码时的记忆填补审查，只能像一个陌生人那样面对这份变更。这正是合格审查者该有的状态。

CLI 是唯一的记录：每一步的语义结果——需求确认、路由选择、门结论、QA 用例、快照——都记录在 `.gates/` 下，封板（seal）时压缩成一份摘要。封板的意思是：把本轮结果冻结下来、写一份摘要、清掉临时状态。之后任何人打开仓库，都能看到这一轮到底发生了什么、每道门为什么通过。

一句话：**把"写的人"和"审的人"与"测的人"从同一颗脑子里拆开，再把结论写成一份不依赖任何会话记忆的记录。**

---

## 特性

- **一套完整流程** —— 需求对齐、路由选择、QA 先设计、开发、快照、独立审查、返修、封板，一步不缺、每步留记录。
- **实现与审查强制分离** —— dev worker、审查门、QA 各自运行在相互不可见的独立会话，审查者只拿需求 + VCS diff。体现在：审查结论只基于变更本身，无法被实现时的记忆污染。
- **CLI 是唯一记录** —— 运行期间只有 `.gates/tmp/<run-id>/state.json` 一份临时状态，封板后只剩 `.gates/results/<run-id>.json` 摘要；CLI 不存 diff、不复制项目文件。体现在：没有第二套版本数据要同步，关掉会话也不丢状态。
- **审查门即文件，可任意修改增减** —— 每个 `gates/*.md` 提示词文件就是一道门，想要多一道门，就加一个提示词文件、想少一道门，就删一个提示词文件，数量不限。体现在：门的集合完全由 `gates/` 目录决定，没有注册表、YAML 或权重表要改。
- **门与提示词都可自定义** —— 门的审查逻辑在 `gates/*.md`，各动作（需求对齐、QA 设计/审查/执行、开发 worker、继承判定）的提示词在 `prompts/actions/*.md`，都是安装包里的普通 Markdown 文件。通过修改提示词文件，每道门怎么审，审哪些内容，你可以完全自定义，包括预制的门。审查要怎么做，全权由你来定。
- **QA 先设计、独立评审后才开发** —— 先产出完整候选用例（STATIC 直接检查 + LIVE 真实执行），独立 QA Review 通过后才开始写代码；返工时保留未变 PASS 的用例。体现在：行为预期在写码之前就冻结了，返修不必重测没变的东西。
- **一次路由** —— 需求对齐后，lightweight / full / custom 只选一次，后续新增需求与任务切片沿用该路由。custom 从 QA 与全部已发现门里自由选择任意非空子集——可以不选 QA，也可以只选 QA。体现在：选一次，整个正式流程的执行范围就确定了。
- **原生 VCS** —— Git、SVN、P4 直接驱动快照与 diff。体现在：仓库本身就是全部真相，没有中间格式的版本数据。快照（snapshot）就是原生 VCS 里那次语义结果对应的提交身份（git 里就是一个 commit）。

---

## 它是怎么工作的

- **门即文件** —— 每一道独立审查门就是一个 `gates/*.md` 文件，文件名就是门 ID。想审查什么就写一个文件，不想审就删掉，想更改审查逻辑就改文件提示词。QA 是内建流程，不占门目录。
- **CLI 是唯一记录** —— 所有语义结论都写在 `.gates/` 下的状态里，封板后收敛成一份摘要。它不存 diff、不存证据，快照与 diff 全在仓库自己的 VCS 里。
- **QA 生命周期** —— 开发之前先把"怎么算对"写清楚：每个要验证的行为拆成候选用例（静态检查 + 真实执行），由独立 QA Review 通过后才动代码；返修只重核失败或变化的用例，没变的 PASS 直接保留。
- **一次路由** —— 需求确认后从 lightweight、full、custom 里选一次。lightweight 是最轻的流程；full 带完整 QA 和全部门；custom 从 QA 与门目录里自由选任意非空子集，可以不含 QA，也可以只含 QA。
- **状态与持久化** —— 运行中的 run 只有一份临时状态文件，任何一步中断都能从那里恢复；封板或中止后，这份临时状态被清掉，只留一份不可变摘要。
- **任务切片与总任务** —— 过大的正式工作按依赖、职责、风险和验证边界切成多个独立子任务，各自在自己的 VCS worktree 里开发；一个总任务实例保留原始基线、完整需求和路由，负责对合并结果做集成审查。
- **返修与继承** —— 发现项打回后可以返修重跑，已记录在不可变快照上的 PASS / FAIL 仍是权威结论；小范围返修若确定不影响任何已通过的验证，可以一次继承全部此前 PASS（含 QA），大范围或影响不确定时仍走独立继承判定。

---

## 正式流程

一次正式 run 从需求对齐到封板，按下面顺序推进。**门的集合是动态的**：`gates/*.md` 里的每个文件就是一道门，加文件就多一道、删文件就少一道，不在流程里写死。

1. **启动** —— 选定仓库的 VCS（Git / SVN / P4），冻结基线，登记需求与需求文档。
2. **需求对齐** —— 在向主代理说明需求后，主代理在开发前用一问一答的方式进行需求细节以及技术路线对齐：一次只问一个会实质改变范围、验收或架构的决策，用日常语言说清每个选项的后果与取舍；问完把整合后的完整需求与技术方案呈现给你，等你明确确认后才继续。已对齐的需求是后续所有判定的唯一依据。
3. **绑定路由** —— 从 lightweight / full / custom 选一次；custom 从 QA 与全部已发现门里自由组合。
4. **开发前** —— 先写清"怎么算对"：产出完整 QA 候选用例（STATIC 静态检查 + LIVE 真实执行），独立 QA Review 通过后才允许动代码。
5. **开发** —— 开发 worker 在独立 session 里按已确认范围实现，新增的交付路径先加入 VCS。
6. **固定快照** —— 用 VCS 创建一个不可变标识，后续审查只针对这个快照。
7. **开发后审查** —— QA 执行与各门审查并行；门从 `gates/*.md` 动态发现，每道门只看基线到当前的完整 diff。
8. **返修** —— P0/P1 发现项或 QA FAIL 时退回修复；返修只重核失败或变化的用例，未变的 PASS 保留；范围可判定的返修可直接继承此前 PASS。
9. **封板** —— 所有必需结果通过后，把本轮结论压成一份不可变摘要，清掉临时状态。

---

## 示例产物

**审查门**是 `gates/` 下的一个 Markdown 文件，文件名就是门 ID。加一道门 = 写一个文件、重新安装：

```markdown
# 命名门（示例）

审查改动中的命名与可读性，只报告含义模糊或误导的标识符。每个发现项给出
repository 相对位置。P0/P1 阻断 PASS，P2 仅作建议不阻塞。
```

P0/P1 是会阻断封板的缺陷严重级，P2 是仅建议、不阻断的轻微问题。

**封板后**，这一轮的结论压缩成一份不可变摘要 `.gates/results/<run-id>.json`：哪几道门通过、每条 finding 的严重级与位置、QA 用例的结果。要查的时候打开看就行，不需要记任何字段名。

---

## 安装与卸载

### 从 release 安装（推荐）

不需要 Go 工具链。下载最新 release 的源码包，解压后在包内运行安装脚本（macOS/Linux 用 `install.command`，Windows 用 `install.bat`）。脚本会下载匹配当前平台的正式二进制、canary 与 SHA256 校验和，校验后组装本地包并调用同一个原生安装器：

```bash
./install.command --host claude --scope global
# Windows: install.bat
```

### 从源码构建

在源码目录构建本机二进制，再选择宿主和范围：

```bash
go build -o bin/formal-gates ./cmd/formal-gates

bin/formal-gates install --source . --host claude --scope global --force
bin/formal-gates install --source . --host codex --scope project --project <project> --force
bin/formal-gates install --source . --host cursor --scope project --project <project> --force

bin/formal-gates uninstall --host claude --scope global
bin/formal-gates uninstall --host codex --scope project --project <project>
```

Windows 使用 `bin\formal-gates.exe`。

### 安装位置

| 宿主 | 全局（global） | 项目（project） |
| --- | --- | --- |
| Claude Code | `~/.claude/skills/formal-gates` | 所选项目下的对应目录 |
| Codex | `~/.codex/skills/formal-gates` | 所选项目下的对应目录 |
| Cursor | `~/.cursor/formal-gates` | 所选项目下的对应目录 |

安装会把本包自带的宿主 hook 合并进对应配置：Claude Code 写 `~/.claude/settings.json`，Codex 写 `~/.codex/hooks.json`，Cursor 写 `~/.cursor/hooks.json`（项目级安装写所选项目下的对应文件）。已有的非 formal-gates hook 不会被覆盖。

安装还会维护受理规则：Claude Code 使用全局 `~/.claude/CLAUDE.md` 或项目
`CLAUDE.md`，Codex 使用全局 `~/.codex/AGENTS.md` 或项目 `AGENTS.md`，Cursor 项目
使用 `.cursor/rules/formal-gates.mdc`。Cursor 全局不创建规则文件，只保留现有运行时和
hook 集成。重复安装会把所有已知旧版本和重复规则收敛为一个最新规则。

### 原生卸载

卸载使用相同的 host、scope 和 project 参数，并清理 formal-gates 运行时、安装器拥有
的 hook 条目和所有历史受管规则，同时保留其他文档内容与 hook：

```bash
bin/formal-gates uninstall --host claude --scope global
bin/formal-gates uninstall --host cursor --scope project --project <project>
```

如果运行时目录已经不存在，可增加 `--source <formal-gates>` 指向包含
`references/managed-rules.json` 的包。

### 参数语义

- `--force`：目标已存在时替换它。
- `--skip-hooks`：只安装包，不改宿主 hook 配置（只有当 hook 配置必须逐字节不变时才用）。
- `uninstall --source`：可选的规则目录来源，用于卸载已缺失运行时的目标。

---

## 使用方式

作为使用者，你只需要做三件事：

1. **安装** —— 按上一节把它装到你的 AI 宿主平台（claude / codex / cursor）。给对应AI 宿主平台说一句“帮我安装formal-gates“即可。
2. **让你的 AI 代理驱动正式流程** —— 安装的 skill（`SKILL.md` 与 `references/`）就是给 AI 代理的操作手册：它读取这些文件，在仓库里为你的需求跑正式流程——对齐需求、选路由、派发独立的 worker 与审查者、记录 QA，直到封板。你不用记住任何命令。
3. **审阅结果** —— 每轮结果由主代理汇总给你：哪些门通过、哪些 finding 需要处理、封板摘要长什么样。你随时可以打开 `.gates/results/<run-id>.json` 查看这一轮的完整结论。

`formal-gates workflow ...` 命令由流程驱动者（你的 AI 代理）执行。

---

## 常见问题

**我开一个新窗口去审查不就行了，为什么要用 formal-gates？**
开新窗口解决"独立零上下文审查"这一点。formal-gates 提供的是完整流程：需求对齐、确认、QA 先设计、开发、快照、独立审查、返修、封板，每次必走、每步留记录、返修只重测变化的用例。

**和"AI 写完再让 AI 审"的 review bot 有什么区别？**
review bot 通常运行在生成代码的同一个上下文里，AI 可以用写代码时的记忆填补审查盲区。formal-gates 把实现、QA 和审查放进相互不可见的独立会话，审查者只拿需求 + VCS diff，没有实现记忆可用，只能基于变更本身判断；同时留下一份 CLI 记录。

**需要什么前置条件？**
从源码构建需要 Go 1.22+，并选一个宿主（claude / codex / cursor）。正式流程需要一个 Git、SVN 或 P4 仓库；无 VCS 的项目不进入正式流程。

**如何增加或删除一道审查门？**
新建或删除 `gates/<id>.md` 并重新安装即可。文件名就是门 ID，不需要改注册表、YAML 或权重表。删除文件即移除门，数量不限。若要修改审查逻辑，修改门文件提示词即可。

**自定义路由可以不选 QA 吗？**
可以。custom 从 QA 和全部已发现门里自由选择任意非空子集：只跑一道门、只跑 QA、QA 加部分门都行。唯一限制是至少选一项，且不能把全部候选都选上（全选用 full）。

**审查结果一定是最终结论吗？**
不是。每个独立代理的结果只是候选输入：主代理在记录或展示前必须核对需求匹配、正常使用边界与结果格式；对 FAIL 或 blocker 还会独立复现其文档化路径再作判断。核对不通过就丢弃该结果。

**封板后留下什么？**
该 run 的整个临时目录被删除，只保留一份摘要 `.gates/results/<run-id>.json`。它不保留提示词副本、证据图或详细状态树。

---

## 当前状态
当前为 v0.1.0 prerelease，文档化流程以仓库为准。发布版本见 [GitHub Releases](https://github.com/DiracSea12/formal-gates/releases)。

---

## 本地校验、贡献与许可

**本地校验**（在仓库根目录运行）：

```bash
go test ./...
go test -race ./internal/validate ./internal/cli
go vet ./...
go build -o bin/formal-gates ./cmd/formal-gates
bin/formal-gates package validate --root .
bin/formal-gates canary portable --root . --format json
```

**贡献**：
- 新增或调整审查门：编辑或新建 `gates/*.md`，重新安装后生效。
- 行为变更请更新 [CHANGELOG.md](CHANGELOG.md)。
- Bug 或改进建议请通过 GitHub issues 提交。

**许可**：[MIT](LICENSE)。
