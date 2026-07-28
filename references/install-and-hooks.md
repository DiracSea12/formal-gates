# 安装与 Hook

这份参考只用于包维护、安装、hook 和 canary。流程顺序只由 `SKILL.md` 拥有。

- [构建与验证](#构建与验证)
- [原生安装](#原生安装)
- [安装位置](#安装位置)
- [Hook 边界](#hook-边界)
- [候选包检查](#候选包检查)
- [Release 边界](#release-边界)

## 构建与验证

已安装的包使用原生 Go 二进制。从源码检出安装之前先构建它：

```bash
go build -o bin/formal-gates ./cmd/formal-gates
bin/formal-gates package validate --root .
bin/formal-gates canary portable --root . --format json
```

Windows 上构建并调用 `bin\formal-gates.exe`。

`package validate` 检查所维护的包结构和基于文件的提示词目录。`canary portable`
检查不需要实机 AI host 的包、流程、hook 决策和安装行为。这两个命令都不是独立审查
或正式 Seal 结果。

## 原生安装

```bash
bin/formal-gates install --source <formal-gates> \
  --host <claude|codex|cursor> --scope global --force

bin/formal-gates install --source <formal-gates> \
  --host <claude|codex|cursor> --scope project --project <project> --force
```

安装器复制一份运行时包，包含 `SKILL.md`、CLI、`prompts/`、`gates/` 和所维护的参
考文档。它默认配置所选 host 的 formal-gates 命令和子代理生命周期 hook。既有的无
关 hook 条目会被保留。只有当 host 的 hook 配置必须逐字节保持不变时，才使用
`--skip-hooks`。

只有带 `--force` 时，安装才会替换一个已存在的 formal-gates 目标。它不得把另一个
host 的全局安装当作回退。

引导文件 `install.command` 和 `install.bat` 会下载匹配的 release 源码与二进制、
校验已发布的 checksum、组装本地包，并调用同一个原生安装器。它们不是第二个安装器。

## 安装位置

典型的全局目标是：

- Claude Code：`~/.claude/skills/formal-gates`
- Codex：`~/.codex/skills/formal-gates`
- Cursor：`~/.cursor/formal-gates`

项目级安装使用所选项目下的对应目录。当 host 需要时，安装器会把原生二进制的绝对
路径写入 hook 配置。

其他兼容 Agent Skill 的 host 可以手工阅读这些 Markdown，但本包不声明为它们提供
安装器或 hook 集成。

## Hook 边界

原生 hook 入口是：

```bash
bin/formal-gates hook decide
```

它从 stdin 接收 host 的 JSON 载荷，并返回与 host 兼容的 allow/block 决策。它是围
绕 formal-gates 命令的护栏，既不是代码质量的证明，也不能替代显式的流程状态检查。

安装器还会为 Claude Code 和 Codex 配置 `SubagentStart` 与 `SubagentStop`，为
Cursor 配置 `subagentStart` 与 `subagentStop`。这些 hook 把 host 载荷经 stdin 发
送给已安装的原生二进制：

```bash
bin/formal-gates lifecycle capture \
  --provider <claude-code|codex|cursor> --event <provider-event-name>
```

capture 命令从正常的 host 载荷推导项目根（必要时使用 host 的项目目录环境变量），
因此全局 hook 不依赖其配置目录作为工作目录。`--root` 仍然可用，作为显式的命令行
覆盖。

生命周期观测只在该项目中至少有一个正式 run 处于活动状态时保留。每个 run 在
`.gates/tmp/<run-id>/lifecycle` 下拥有自己的待定与已认领观测，因此正常的 Seal 或
Abort 清理会把它们与该 run 的其余部分一并退役。在没有活动正式 run 时触发的 hook
不会创建生命周期日志。

在 `workflow claim-dispatch` 绑定 host 身份之后，可以在不改变流程状态的情况下检
视推导出的结果：

```bash
bin/formal-gates lifecycle verify --root <repo> --run-id <id> \
  --dispatch <dispatch-id>
```

Claude Code 和 Cursor 要求 start 与 stop 事件配对。Codex 报告 `UNAVAILABLE`，因
此既有的派发与身份检查仍然是权威依据。

一个设置文件或直接的 `hook decide` 单元测试，只能证明本地决策逻辑。只有当实际目
标 host 发送了实机 `PreToolUse` 载荷、并且 hook 阻塞了测试命令之后，才可以声明自
动阻塞。在一个 host 上做的 canary 不能证明另一个 host。

Codex 实机检查：

```bash
bin/formal-gates canary codex-hook --worktree <repo>
```

失败意味着该客户端/版本没有证明闭环的自动拦截。显式的 `formal-gates workflow ...`
命令仍然是该 run 的正常权威。

## 候选包检查

测试候选版本时，把该确切源码安装进测试项目，并记录两个路径：

```text
source: <candidate>/formal-gates
installed: <test-project>/<host-path>/formal-gates
```

不要一边测试过期的全局包，一边把结果报告成候选版本的结果。包、提示词目录、安装
和实机 hook 的所有声明，都必须指明实际使用的那份副本。

## Release 边界

CI 流程构建 Windows、macOS 和 Linux 二进制、portable-canary 输出和 SHA256
checksum 文件，并可以把它们附加到 GitHub Release。checksum 说明下载到的字节与已
发布的 CI 产物一致。它们不提供签名、attestation、provenance、registry、
marketplace 或 `npx` 分发路径。

仓库维护命令在 `references/local-validation.md`。
