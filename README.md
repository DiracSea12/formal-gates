# formal-gates

formal-gates 是一套轻量的 AI 开发审查流程。它把“写代码”和“判断是否
通过”交给不同代理，用项目现有的 Git、SVN 或 P4 提供 diff，最后由 CLI
保存用户实际选择执行的审查结果。

它只在你明确要求使用 formal-gates 进行需求对齐、开发前审查、正式开发、
开发后审查或正式 run 封板时启用。普通封板交付、普通开发、解释、随手
review 和小改动不会自动进入这套流程。

## 核心设计

- `prompts/reviewer-base.md`：所有独立审查门共用的规则。
- `gates/*.md`：每个文件就是一道独立审查门，文件名就是门 ID。
- `prompts/actions/*.md`：需求澄清、开发前审查、QA、开发 worker 和 Carry。
- `.gates/tmp/<run-id>/state.json`：运行期间唯一的临时状态文件。
- `.gates/results/<run-id>.json`：封板或中止后唯一保留的结果。

增加或删除审查门只需增加或删除一个有效的 `gates/*.md` 文件并重新安装。
不需要改 Go 注册表、manifest、YAML、权重、依赖关系或顺序表。需求对齐后，
用户从“QA 优先、其余门按文件名排序”的列表中一次选择 none、full 或 custom。

QA 不属于提示词门目录。开发完成后，用户选择的 QA Execution 和审查门可以
在同一批次并行执行。
full 和 custom 路由运行 Start Readiness，none 路由省略它。准备 development
worker 时即冻结开发前结果，并禁止在开发开始后再加入 QA。

## 安装

先在源码目录构建本机二进制：

```bash
go build -o bin/formal-gates ./cmd/formal-gates
```

然后选择宿主和范围：

```bash
bin/formal-gates install --source . --host claude --scope global --force
bin/formal-gates install --source . --host codex --scope project --project <project> --force
bin/formal-gates install --source . --host cursor --scope project --project <project> --force
```

安装默认合并 formal-gates 自己的宿主 hook。只有明确不想改 hook 时才加
`--skip-hooks`。现有非 formal-gates hook 不会被覆盖。

Windows 使用 `bin\formal-gates.exe`。也可以运行 `install.command` 或
`install.bat` 下载对应 release 产物并调用同一个安装器。

## 工作流命令

下面只展示命令入口；完整顺序由 [SKILL.md](SKILL.md) 唯一维护。

启动和查看：

```bash
formal-gates workflow start \
  --root <repo> --package-root <installed-formal-gates> \
  --run-id <id> --flow formal --requirement <requirement-file> \
  --vcs <git|svn|p4> --base-snapshot <base>

formal-gates workflow show --root <repo> --run-id <id>
formal-gates workflow resume --root <repo> --package-root <installed-formal-gates> --run-id <id>
formal-gates workflow abort --root <repo> --run-id <id>
```

准备 AI 任务时，CLI 把完整提示词写到 stdout。Requirements Clarification
由主代理直接执行；其余独立 action 和 gate 的 stdout 原样交给对应代理，不要
追加历史结论或其他门的结果：

```bash
formal-gates workflow prepare-action --root <repo> --package-root <package> \
  --run-id <id> --action <requirements-clarification|start-readiness|qa-design|development-worker|qa-execution|carry> \
  --live-snapshot <current>

formal-gates workflow prepare-gate --root <repo> --package-root <package> \
  --run-id <id> --gate <discovered-gate-id> --live-snapshot <current>
```

记录语义结果：

```bash
formal-gates workflow requirement --root <repo> --package-root <package> \
  --run-id <id> --source <requirement-file> --confirmed

formal-gates workflow route-candidates --root <repo> --package-root <package> \
  --run-id <id>
formal-gates workflow route --root <repo> --package-root <package> \
  --run-id <id> --mode <none|full|custom> [--gate <gate-id> ...]

formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action start-readiness --status PASS \
  --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt>

formal-gates workflow qa-design --root <repo> --package-root <package> --run-id <id> \
  --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt> \
  --case '<behavior>' --procedure '<public procedure>' --oracle '<expected result>'

formal-gates workflow record-gate --root <repo> --package-root <package> \
  --run-id <id> --gate <gate-id> --status <PASS|FAIL|RUNTIME_ERROR> \
  --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt> \
  --source-snapshot <snapshot-from-prepared-prompt> \
  --live-snapshot <current> \
  [--finding '<message>' --severity <P0|P1|P2> --location '<path:line>']

formal-gates workflow snapshot --root <repo> --package-root <package> \
  --run-id <id> --current-snapshot <new-current> --live-snapshot <new-current>
```

QA Execution、Carry、额外返修授权和 Seal 的参数以 `formal-gates help` 及
`SKILL.md` 为准。需求文件修订变化后，先用 `workflow requirement --meaning
preserved|changed` 明确语义影响；CLI 不自行猜测。

## Diff 与返修

formal-gates 不读取或保存 diff 内容，也不复制项目文件。worker、QA 和
reviewer 直接运行现场 VCS：

- 总 diff：开发前 base 到当前 snapshot，所有新跑或重跑的门都看它。
- 本轮返修 diff：返修前 snapshot 到当前 snapshot，只给 Carry 判断哪些门
  可以沿用。
- 新建文件或原本未跟踪但属于本次交付的文件，worker 必须先按路径加入 VCS，
  再继续修改；不要执行 `git add .` 或触碰无关未跟踪文件。

Git、SVN、P4 的命令见
[references/vcs-snapshots.md](references/vcs-snapshots.md)。无 VCS 项目不进入
正式流程。

## 结果与中断

每条审查门 finding 都带 P0、P1 或 P2。无 finding 或仅 P2 时为 `PASS`；
至少一条 P0/P1 时为 `FAIL`；`RUNTIME_ERROR` 不带 finding。已选择的
`PENDING` 会阻止 Seal；运行错误需要重试或用户明确跳过。QA FAIL 和 P0/P1
在共享三轮返修额度耗尽前必须返修，之后才可显式授权跳过。仅 P2 的建议会
保留展示，但不阻止 Seal。

封板成功或显式中止后，CLI 写入一个摘要并删除该 run 的整个临时目录。
它不会留下提示词副本、分层证据图或详细状态文件。

## 本地校验

```bash
go test ./...
go test -race ./internal/validate ./internal/cli
go vet ./...
go build -o bin/formal-gates ./cmd/formal-gates
bin/formal-gates package validate --root .
bin/formal-gates canary portable --root . --format json
```

宿主 hook 是否真正拦截必须在该宿主上跑 live canary。配置文件存在不等于
hook 已生效。安装和 canary 细节见
[references/install-and-hooks.md](references/install-and-hooks.md)。

## 范围

本项目只保证文档化正常使用和常见操作失误。除非需求明确提出，否则恶意
篡改内部状态、权限故障、不可变文件注入、攻击式输入和其他违反流程的场景
不属于阻断范围，也不应催生额外防御系统。

许可证：[MIT](LICENSE)。变更记录见 [CHANGELOG.md](CHANGELOG.md)。
