# Install And Hooks

这个文件只给安装、hook、canary 细节。平常跑四门流程不要读它。

## 包结构

候选包目录必须保持：

```text
formal-gates/
  SKILL.md
  agents/
  examples/
  hooks/
  references/
  scripts/
```

`scripts/` 和 `hooks/` 必须随包复制，不能依赖旧 loose skills 或全局原版路径兜底。

## 维护源

GitHub checkout 或显式传入的 `-SourcePath` 是主版本。全局 `.claude` / `.codex` 目录只是安装快照，不是维护源。

改包时先改主版本、验证、提交/推送，再按需要安装到指定 host。不要因为某个全局同名 skill 存在，就把它当成最新版；A/B 或旧版对比场景下，未被点名的 host 必须保持不动。

## 安装到 Claude 或 Codex

推荐用包内安装脚本复制整个目录，不要手工挑文件：

```powershell
# 安装/替换全局 Claude skill
pwsh <formal-gates>\scripts\install-formal-gates.ps1 -HostName Claude -Scope Global -Force -RunCanary

# 安装到某个项目本地 Claude skill
pwsh <formal-gates>\scripts\install-formal-gates.ps1 -HostName Claude -Scope Project -ProjectPath <project> -Force -RunCanary

# 需要同时安装 Claude 和 Codex 时
pwsh <formal-gates>\scripts\install-formal-gates.ps1 -HostName Both -Scope Project -ProjectPath <project> -Force -RunCanary
```

脚本会复制整个 `formal-gates` 目录，拒绝替换非 `skills\formal-gates` 目标，并清理 `__pycache__`。`-RunCanary` 会在复制后跑 portable canary；如果失败，不要把这次安装当成可用。

手工安装的目标路径：

- 全局 Claude：`%USERPROFILE%\.claude\skills\formal-gates`
- 全局 Codex：`%USERPROFILE%\.codex\skills\formal-gates`
- 项目本地 Claude：`<project>\.claude\skills\formal-gates`
- 项目本地 Codex：`<project>\.codex\skills\formal-gates`

不要只复制 `SKILL.md`。少了 `scripts/gate-state.ps1` 或 `hooks/enforce-gate-sequence.ps1`，正式流程会失去机器检查。

给项目做 copy-then-verify 时，优先复制到项目本地 `.claude/.codex/skills/formal-gates`，不要测到全局同名旧包。Claude 和 Codex 都安装时，两个镜像要么 byte-identical，要么在记录里写明版本/hash 差异。

## 候选包 A/B 测试安装

测试候选包时，不要直接用全局同名 skill。先把用户给的候选路径复制到测试 workspace 的 `.claude/.codex/skills/formal-gates`，优先用 `install-formal-gates.ps1`。

每次 A/B 记录都必须写清楚：

```text
Skill source path: <candidate>\formal-gates
Copied skill path: <test-workspace>\.codex\skills\formal-gates
```

如果只看到 `formal-gates` 这个名字，看不出测的是候选包还是全局原版，测试证据无效。

## Hook 规则

hook 入口是：

```text
hooks/enforce-gate-sequence.ps1
```

候选包的 hook 必须只解析同一包内的：

```text
scripts/gate-state.ps1
```

禁止回退到：

- `%USERPROFILE%\.codex\skills\formal-gates\scripts\gate-state.ps1`
- `%USERPROFILE%\.claude\skills\formal-gates\scripts\gate-state.ps1`
- 旧 loose skill 目录
- 任何全局原版路径

A/B 测试时，hook 偷用全局脚本会把旧包测成候选包。

Claude Code 从 Claude settings 加载 hook。Codex 从 `~/.codex/hooks.json`、`~/.codex/config.toml` 的 `[hooks]`、或项目本地 `.codex` 配置加载 hook。不要为了 hook 加载绕去做 Codex plugin。

Codex 非托管 command hook 需要在 `/hooks` 里审过并信任后，正常交互才会使用。`--dangerously-bypass-hook-trust` 只允许用于已经审过 hook 源码的自动化 canary，不准拿它当日常绕过。

## GateWorkflow 和 gate-state

正式门禁需要结构化 `GateWorkflow`。最少要有：

- `workflowId`
- `changeSnapshot`
- `worktree` 或 `statePath`
- `gate`
- QA gate 或 manifest 扩展 gate 的 `stage`；内置 complexity/architecture/code-quality 没有 stage 时可以省略。

`scripts/gate-state.ps1` 负责记录和校验 gate 状态。`scripts/gate-workflow.ps1` 是常用包装脚本，会调用同目录的 `gate-state.ps1`。

## Portable canary

用这个脚本检查包是否能复制到临时项目并跑基本门禁：

```powershell
pwsh <formal-gates>\scripts\test-portable-openspec-canary.ps1 -SkillPath <formal-gates>
```

它验证的是包可搬运、脚本可运行、基本 gate-state 记录可用。它不等于真实项目的最终 QA。

## Codex hook canary

Codex hook canary 用：

```powershell
pwsh <formal-gates>\hooks\test-codex-hook-client.ps1 -Worktree <repo> -KeepTemp
```

Codex hook canary 的 PASS 条件只有一个：真实 `codex exec` 至少写出一个 `PreToolUse` hook payload，包内 `hooks/enforce-gate-sequence.ps1` 拦住一个缺 artifact 的 formal PASS 命令，且 canary marker file 没被创建。`FAIL` 或 `TIMED_OUT` 就说明这个 Codex client/version 的 hook interception 没闭环，或者 formal-gates hook 本身没有正确接入。

不要用 script-direct 测试、Claude hook 成功、或 `codex exec` 里目标命令自己失败，冒充 Codex hook closed-loop。Codex hook 只是 guardrail；正式非交互流程仍要靠 `gate-workflow.ps1` / `gate-state.ps1` admission 和 artifact 记录。

## 快速结构校验

候选包修改后跑：

```powershell
$validator = "$env:USERPROFILE\.codex\skills\.system\skill-creator\scripts\quick_validate.py"
if (-not (Test-Path $validator)) { throw "skill-creator quick_validate.py not found; use the local skill-creator scripts/quick_validate.py path instead." }
python -X utf8 $validator <formal-gates>
```

再用 PowerShell 读中文入口：

```powershell
Get-Content -Encoding UTF8 <formal-gates>\SKILL.md
```

如果中文乱码、BOM 异常、frontmatter 缺 name/description，先修包结构，不要继续谈 gate 质量。
