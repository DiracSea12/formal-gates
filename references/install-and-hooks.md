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

GitHub checkout，或用户显式传入的 `-SourcePath` 候选包源目录，才是维护源。全局 `.claude` 目录和可选 `.codex` 目录只是安装快照，不是维护源。

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

# 安装并写入/更新 Claude settings.json command hook
<ps> -File <formal-gates>\scripts\install-formal-gates.ps1 -HostName Claude -Scope Global -Force -RunCanary -ConfigureHook

# 安装到某个项目本地 Claude skill
<ps> -File <formal-gates>\scripts\install-formal-gates.ps1 -HostName Claude -Scope Project -ProjectPath <project> -Force -RunCanary
```

脚本会复制整个 `formal-gates` 目录，拒绝替换非 `skills\formal-gates` 目标，并清理 `__pycache__`。`-RunCanary` 会在复制后跑 portable canary；如果失败，不要把这次安装当成可用。`-ConfigureHook` 会自动写入 Claude `settings.json` 或 Cursor `hooks.json`；写前会备份为 `.bak`，且只合并或更新 formal-gates 自己的 hook，不覆盖其它 hook。

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

## Cursor hook 接入

Cursor 走官方 Hooks，不是 Claude-style skill loader。安装脚本可自动写 hook：

```powershell
# 项目级 Cursor hook
<ps> -File <formal-gates>\scripts\install-formal-gates.ps1 -HostName Cursor -Scope Project -ProjectPath <project> -Force -RunCanary -ConfigureHook

# 全局 Cursor hook
<ps> -File <formal-gates>\scripts\install-formal-gates.ps1 -HostName Cursor -Scope Global -Force -RunCanary -ConfigureHook
```

脚本会复制到 `.cursor\formal-gates`，并写入 `.cursor\hooks.json` 或 `%USERPROFILE%\.cursor\hooks.json` 的 `preToolUse` command hook。

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

Claude Code 是主路径，从 Claude settings 加载 hook。全局示例：

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "*",
        "hooks": [
          {
            "type": "command",
            "command": "powershell -NoProfile -ExecutionPolicy Bypass -File \"C:\\Users\\Administrator\\.claude\\skills\\formal-gates\\hooks\\enforce-gate-sequence.ps1\""
          }
        ]
      }
    ]
  }
}
```

项目本地安装时，把 `command` 里的路径改成 `<project>\\.claude\\skills\\formal-gates\\hooks\\enforce-gate-sequence.ps1`。具体客户端或托管网关是否真正执行 command hook，必须用目标机器的 live canary 证明；只看 settings 文件不算。

Codex 兼容路径从 `~/.codex/hooks.json`、`~/.codex/config.toml` 的 `[hooks]`、或项目本地 `.codex` 配置加载 hook。不要为了 hook 加载绕去做 Codex plugin。

Codex `config.toml` 最小形状：

```toml
[features]
hooks = true

[[hooks.PreToolUse]]
matcher = "*"
[[hooks.PreToolUse.hooks]]
type = "command"
command = "powershell -NoProfile -ExecutionPolicy Bypass -File \"C:/Users/Administrator/.codex/skills/formal-gates/hooks/enforce-gate-sequence.ps1\""
timeout = 30
```

Codex 非托管 command hook 需要在 `/hooks` 里审过并信任后，正常交互才会使用。`--dangerously-bypass-hook-trust` 只允许用于已经审过 hook 源码的自动化 canary，不准拿它当日常绕过。

Cursor 从 `~/.cursor/hooks.json` 或项目 `.cursor/hooks.json` 加载 hook。项目级最小形状：

```json
{
  "version": 1,
  "hooks": {
    "preToolUse": [
      {
        "command": "powershell -NoProfile -ExecutionPolicy Bypass -File \".cursor/formal-gates/hooks/enforce-gate-sequence.ps1\"",
        "timeout": 30,
        "failClosed": true
      }
    ]
  }
}
```

Cursor hook 脚本从 stdin 收 JSON，stdout 返回 `permission: "deny"` 或退出码 2 即拦截。这里不写 `matcher`，让脚本内部只处理四门相关命令，避免 Cursor 不同版本的 matcher 形状差异。只看 hooks.json 仍不算 live canary。

## GateWorkflow 和 gate-state

正式门禁需要结构化 `GateWorkflow`。最少要有：

- `workflowId`
- `changeSnapshot`
- `worktree` 或 `statePath`
- `gate`
- QA gate 或 manifest 扩展 gate 的 `stage`；内置 complexity/architecture/code-quality 没有 stage 时可以省略。

`scripts/gate-state.ps1` 负责记录和校验 gate 状态。`scripts/gate-workflow.ps1` 是常用包装脚本，会调用同目录的 `gate-state.ps1`。

`GateWorkflow` 是 JSON。Windows 路径里的反斜杠必须写成双反斜杠，或直接用正斜杠：

```text
GateWorkflow={"gate":"complexity-gate","workflowId":"wf","changeSnapshot":"snap","worktree":"C:\\Users\\me\\repo"}
GateWorkflow={"gate":"complexity-gate","workflowId":"wf","changeSnapshot":"snap","worktree":"C:/Users/me/repo"}
```

不要写 `"worktree":"C:\Users\me\repo"`；`\U` 不是合法 JSON 转义，hook 会报 `Malformed GateWorkflow JSON`。

## Manifest 扩展 gate

自定义扩展 gate 必须带 `manifestPath`，并在 manifest 的 `stages.<gate-id>` 里定义依赖。例如：`GateWorkflow={"gate":"security-gate","workflowId":"...","changeSnapshot":"...","worktree":"...","manifestPath":"gate-manifest.json"}`。

Manifest 只能定义扩展 gate，不能定义或覆盖 `qa-test-gate`、`complexity-gate`、`architecture-health-gate`、`code-quality-gate`。四个内置 gate 的顺序是固定流程，不允许用 manifest 改写。

Manifest 扩展 gate 会绑定 manifest hash。扩展 gate 的前置 gate 也必须用同一个 `-ManifestPath` 记录，旧记录或没有 `manifestHash` 的记录不能满足扩展 gate admission。给既有流程新增 manifest 后，要按该 manifest 重新记录前置 gate，不能复用旧内置 PASS。

## 正式 gate artifact 必备字段

缺机器元数据时不能记录正式 PASS，最多 advisory review。`singleGateAuthorized=true` 只允许显式单门 advisory，不能记录、复用或推进 release/seal。

所有正式 gate artifact 必须写明：

```text
Zero-context reviewer: YES
Independent agent: YES
Reviewer agent id:
Context bundle: <bundle-path> sha256=<bundle-sha256>
No-anchor prompt: YES
```

`Reviewer agent id` 不能是空值或占位符。`Context bundle` 必须是存在的文件并带 sha256；机器会校验 hash。

implementation 后的复杂度、架构、代码质量正式 PASS 还必须写明：

```text
Changed files artifact: <path>
Verification artifact: <path>
```

`Changed files artifact` 可用 `Raw diff artifact` 代替，`Verification artifact` 可用 `Developer self-test artifact` 代替。路径都必须存在。

`qa-test-gate` 正式 PASS 还必须写明：

```text
Approved case set:
QA-owned evidence:
Case-to-artifact binding:
```

每个正式 gate 输出都必须带机器可读路线：

```yaml
gate_route:
  workflow_id: ""
  change_snapshot: ""
  next_action: proceed | rework | blocked | seal
  rework_owner: none | implementation | tests | architecture | qa-cases | scope
  rerun_from: none | qa-design | qa-verification | qa-execution | complexity | architecture | code-quality | final-verification
```

`REVIEW`、`FAIL`、`BLOCKED` 不能路由到 `proceed`。

## 正式记录命令

最小机器检查命令：

```powershell
<ps> -File <formal-gates>\scripts\gate-workflow.ps1 -Action verify-admission -Worktree <repo> -Gate <gate-id> -WorkflowId <id> -ChangeSnapshot <snapshot>
<ps> -File <formal-gates>\scripts\gate-workflow.ps1 -Action record-stage -Worktree <repo> -Gate <gate-id> -Verdict PASS -Artifact <artifact> -Actor <reviewer> -WorkflowId <id> -ChangeSnapshot <snapshot>
```

`<ps>` 代表当前可用的 PowerShell：Windows PowerShell 5 用 `powershell -NoProfile -ExecutionPolicy Bypass`，PowerShell 7 用 `pwsh -NoProfile`。包内脚本会继续使用当前 PowerShell，不要求必须有 PowerShell 7。

`qa-test-gate` 正式记录必须加 `-Mode formal -Stage Execution`；最终 QA 用 `-Mode formal -Stage FinalExecution`。不要直接照抄泛化命令漏掉 stage。

如果同时安装 Claude 和 Codex，两边 skill 镜像和 hook 不是同一份时，不能声称“同一套 formal-gates 正在生效”。必须写清楚实际运行路径和 hash。

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
