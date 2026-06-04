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

GitHub checkout 或显式传入的 `-SourcePath` 是主版本。全局 `.claude` 目录和可选 `.codex` 目录只是安装快照，不是维护源。

改包时先改主版本、验证、提交/推送，再按需要安装到指定 host。不要因为某个全局同名 skill 存在，就把它当成最新版；A/B 或旧版对比场景下，未被点名的 host 必须保持不动。

## PowerShell 入口

脚本支持 Windows PowerShell 5 和 PowerShell 7。下面示例用 `<ps>` 代表启动前缀：

- PowerShell 5：`powershell -NoProfile -ExecutionPolicy Bypass`
- PowerShell 7：`pwsh -NoProfile`

包内脚本会继续使用当前 PowerShell，不要求必须安装 PowerShell 7。

## 安装到 Claude（主路径）

推荐用包内安装脚本复制整个目录，不要手工挑文件：

```powershell
# 安装/替换全局 Claude skill
<ps> -File <formal-gates>\scripts\install-formal-gates.ps1 -HostName Claude -Scope Global -Force -RunCanary

# 安装到某个项目本地 Claude skill
<ps> -File <formal-gates>\scripts\install-formal-gates.ps1 -HostName Claude -Scope Project -ProjectPath <project> -Force -RunCanary
```

脚本会复制整个 `formal-gates` 目录，拒绝替换非 `skills\formal-gates` 目标，并清理 `__pycache__`。`-RunCanary` 会在复制后跑 portable canary；如果失败，不要把这次安装当成可用。

Claude 手工安装目标路径：

- 全局 Claude：`%USERPROFILE%\.claude\skills\formal-gates`
- 项目本地 Claude：`<project>\.claude\skills\formal-gates`

不要只复制 `SKILL.md`。少了 `scripts/gate-state.ps1` 或 `hooks/enforce-gate-sequence.ps1`，正式流程会失去机器检查。

给项目做 copy-then-verify 时，优先复制到项目本地 `.claude/skills/formal-gates`，不要测到全局同名旧包。

## Codex 可选兼容

Codex 不是主路径。只有明确要验证 Codex 兼容、A/B 对比或旧版对比时，才安装到 `.codex`：

```powershell
# 安装到全局 Codex skill
<ps> -File <formal-gates>\scripts\install-formal-gates.ps1 -HostName Codex -Scope Global -Force -RunCanary

# 需要同时安装 Claude 和 Codex 时
<ps> -File <formal-gates>\scripts\install-formal-gates.ps1 -HostName Both -Scope Project -ProjectPath <project> -Force -RunCanary
```

Codex 手工安装目标路径：

- 全局 Codex：`%USERPROFILE%\.codex\skills\formal-gates`
- 项目本地 Codex：`<project>\.codex\skills\formal-gates`

Claude 和 Codex 都安装时，两个镜像要么 byte-identical，要么在记录里写明版本/hash 差异。不要用 Codex 安装结果反向证明 Claude 主路径可用。

## 候选包 A/B 测试安装

测试候选包时，不要直接用全局同名 skill。先把用户给的候选路径复制到测试 workspace 的 `.claude/skills/formal-gates`，优先用 `install-formal-gates.ps1`。只有明确测试 Codex 兼容时，才复制到 `.codex/skills/formal-gates`。

每次 A/B 记录都必须写清楚：

```text
Skill source path: <candidate>\formal-gates
Copied skill path: <test-workspace>\.claude\skills\formal-gates
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

Claude Code 是主路径，从 Claude settings 加载 hook。具体客户端或托管网关是否真正执行 command hook，必须用目标机器的 live canary 证明；只看 settings 文件不算。

Codex 兼容路径从 `~/.codex/hooks.json`、`~/.codex/config.toml` 的 `[hooks]`、或项目本地 `.codex` 配置加载 hook。不要为了 hook 加载绕去做 Codex plugin。

Codex 非托管 command hook 需要在 `/hooks` 里审过并信任后，正常交互才会使用。`--dangerously-bypass-hook-trust` 只允许用于已经审过 hook 源码的自动化 canary，不准拿它当日常绕过。

## GateWorkflow 和 gate-state

正式门禁需要结构化 `GateWorkflow`。最少要有：

- `workflowId`
- `changeSnapshot`
- `worktree` 或 `statePath`
- `gate`
- QA gate 或 manifest 扩展 gate 的 `stage`；内置 complexity/architecture/code-quality 没有 stage 时可以省略。

`scripts/gate-state.ps1` 负责记录和校验 gate 状态。`scripts/gate-workflow.ps1` 是常用包装脚本，会调用同目录的 `gate-state.ps1`。

## Git / SVN / 非 VCS snapshot

git 项目继续用 commit range 生成 snapshot：

```powershell
<ps> -File <formal-gates>\scripts\gate-workflow.ps1 -Action snapshot -Worktree <repo> -BaseRef <base> -HeadRef HEAD -IncludeWorkingTree
```

SVN 或非 git 项目用文件树哈希 fallback，不需要 `BaseRef`：

```powershell
<ps> -File <formal-gates>\scripts\gate-workflow.ps1 -Action snapshot -Worktree <project> -Vcs auto
```

这会排除 `.git`、`.svn`、`.hg`、`node_modules`、`__pycache__` 和本地 gate state，生成 `svn-files.<hash>` 或 `files.<hash>`。它能保证 artifact 绑定同一文件快照，但不能替代人工确认 SVN 分支/版本来源。

## Portable canary

用这个脚本检查包是否能复制到临时项目并跑基本门禁：

```powershell
<ps> -File <formal-gates>\scripts\test-portable-openspec-canary.ps1 -SkillPath <formal-gates>
```

它验证的是包可搬运、脚本可运行、基本 gate-state 记录可用。它不等于真实项目的最终 QA。

## Codex hook canary（可选兼容）

只在验证 Codex 兼容时运行：

```powershell
<ps> -File <formal-gates>\hooks\test-codex-hook-client.ps1 -Worktree <repo> -KeepTemp
```

Codex hook canary 的 PASS 条件只有一个：真实 `codex exec` 至少写出一个 `PreToolUse` hook payload，包内 `hooks/enforce-gate-sequence.ps1` 拦住一个缺 artifact 的 formal PASS 命令，且 canary marker file 没被创建。`FAIL` 或 `TIMED_OUT` 就说明这个 Codex client/version 的 hook interception 没闭环，或者 formal-gates hook 本身没有正确接入。

不要用 script-direct 测试、Claude hook 成功、或 `codex exec` 里目标命令自己失败，冒充 Codex hook closed-loop。Codex hook 只是可选 guardrail；正式非交互流程仍要靠 `gate-workflow.ps1` / `gate-state.ps1` admission 和 artifact 记录。

## 快速结构校验

候选包修改后跑：

```powershell
$validator = "<path-to-skill-creator>\scripts\quick_validate.py"
if (-not (Test-Path $validator)) { throw "skill-creator quick_validate.py not found; pass the local validator path." }
py -3 -X utf8 $validator <formal-gates>
```

再用 PowerShell 读中文入口：

```powershell
Get-Content -Encoding UTF8 <formal-gates>\SKILL.md
```

如果中文乱码、BOM 异常、frontmatter 缺 name/description，先修包结构，不要继续谈 gate 质量。
