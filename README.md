# formal-gates

formal-gates 是一套轻量的 AI 开发审查流程。它把“写代码”和“判断是否
通过”交给不同代理，用项目现有的 Git、SVN 或 P4 提供 diff，最后由 CLI
保存用户实际选择执行的审查结果。

每个创建、编辑、移动或删除项目内容的请求都会启用它的 intake，不区分仓库、
产品、文件类型或预估规模。未要求修改的只读提问、解释、诊断和 review 不会
自动进入修改 intake，除非用户明确要求正式执行。

任何写入或实现派发前，主代理先自行检查工作区事实，逐个澄清目标和会影响
公开行为、验收或架构的技术选择，再展示完整需求和技术方案并等待用户明确
确认。随后根据总规模、耦合、风险和验证复杂度提出建议，并利用无状态候选
查询只问一次 lightweight、full 或 custom。lightweight 不创建正式 run；full
选择 QA 和全部动态门；custom 展示完整列表供用户选择非空子集。

## 核心设计

- `prompts/reviewer-base.md`：所有独立审查门共用的规则。
- `gates/*.md`：每个文件就是一道独立审查门，文件名就是门 ID；`qa` 保留给
  内建 QA 流程，不能作为文件 ID。
- `prompts/actions/*.md`：需求澄清、开发前审查、QA、开发 worker 和 Carry。
- `.gates/tmp/<run-id>/state.json`：运行期间唯一的临时状态文件。
- `.gates/results/<run-id>.json`：封板或中止后唯一保留的结果。

增加或删除审查门只需增加或删除一个有效的 `gates/*.md` 文件并重新安装。
不需要改 Go 注册表、manifest、YAML、权重、依赖关系或顺序表。需求对齐后，
用户从“QA 优先、其余门按文件名排序”的列表中一次选择 lightweight、full 或
custom；只有 full 或 custom 才启动正式工作流状态。

QA 不属于提示词门目录。选择 QA 后，QA Design 先产出完整候选用例，独立
QA Review 通过后才能开发；Review 失败会带着原用例返回 Design 修改，且不
占用开发后三轮 review-wave。开发完成后，QA Execution 和审查门可以在同一
批次并行执行。
每条正式路由都在开发前运行 Start Readiness。准备 development worker 时即
冻结开发前结果，并禁止在开发开始后再加入 QA。full 和 custom 还要求在开发前
用当前环境可用的任意稳定文档格式保存完整已确认需求和技术方案，不要求指定
文档插件。

同一次路由选择覆盖后续新增需求和任务切片。新增范围会暂停相关写入，重新澄清
并确认刷新后的完整摘要，但除非用户要求，不重复询问路由。过大的正式工作按
依赖、职责、风险和验证边界切分；一个总体 run 保留原始 base、完整需求和路由，
独立切片使用不同 VCS worktree 和正式 run，合并后的快照再由原总体 run 从原始
base 执行集成审查。

需求语义变化后，原有 QA 用例保留为未批准的完整覆盖复核输入。下一次 QA
Design 在影响边界明确时保留不受影响的用例，无法可靠确定边界时则替换完整
用例集，并重新经过独立 QA Review。正常中断后可以重新生成已准备的
development 任务；不可变快照上已记录的语义 PASS 或 FAIL 仍为权威结果。

返修后，主代理检查原生 VCS 的本轮返修 diff。只有能确定返修不会影响任何
此前已通过的已选验证时，才可执行 `workflow carry --main-agent --main-reason
'<reason>'`，一次继承包括 QA 在内的所有先前 PASS。涉及共享行为、配置、
依赖、跨门职责或影响链不确定时，仍按原流程使用独立 Carry，并正常重跑 QA。

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

启动 run 之前，可以只读查询当前安装包提供的路由候选；该命令不需要仓库、
需求、run ID、VCS 快照或工作流状态，也不会创建工作流状态：

```bash
formal-gates package route-candidates --root <package>
```

返回的 JSON 数组以 `qa` 开头，后接按文件名 ID 排序的动态审查门。run 启动
并确认需求后，仍使用下文的 `workflow route-candidates` 查询该 run 绑定的候选。

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
  --run-id <id> --action <requirements-clarification|start-readiness|qa-design|qa-review|development-worker|qa-execution|carry> \
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
  --run-id <id> --mode <full|custom> [--gate <gate-id> ...]

formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action start-readiness --status PASS \
  --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt>

formal-gates workflow qa-design --root <repo> --package-root <package> --run-id <id> \
  --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt> \
  --case '<behavior>' --procedure '<public procedure>' --oracle '<expected result>'

formal-gates workflow record-action --root <repo> --package-root <package> \
  --run-id <id> --action qa-review --status <PASS|FAIL|RUNTIME_ERROR> \
  --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt>

formal-gates workflow record-gate --root <repo> --package-root <package> \
  --run-id <id> --gate <gate-id> --status <PASS|FAIL|RUNTIME_ERROR> \
  --source-revision <revision-from-prepared-prompt> \
  --source-catalog-revision <catalog-revision-from-prepared-prompt> \
  --source-snapshot <snapshot-from-prepared-prompt> \
  --live-snapshot <current> \
  [--finding '<message>' --severity <P0|P1|P2> --location '<path:line>']

formal-gates workflow snapshot --root <repo> --package-root <package> \
  --run-id <id> --current-snapshot <new-current> --live-snapshot <new-current>

formal-gates workflow carry --root <repo> --package-root <package> \
  --run-id <id> --main-agent --main-reason '<bounded repair reason>' \
  --live-snapshot <current>
```

QA Execution、Carry、额外返修授权和 Seal 的参数以 `formal-gates help` 及
`SKILL.md` 为准。需求文件修订变化后，先用 `workflow requirement --meaning
preserved|changed --live-snapshot <current>` 明确语义影响并绑定包含该修订的
原生 VCS identity；CLI 不自行猜测。

## Diff 与返修

formal-gates 不读取或保存 diff 内容，也不复制项目文件。worker、QA 和
reviewer 直接运行现场 VCS：

- 总 diff：开发前 base 到当前 snapshot，所有新跑或重跑的门都看它。
- 本轮返修 diff：返修前 snapshot 到当前 snapshot。主代理只用它判断能否走
  一次继承全部 PASS 的小返修捷径；否则交给独立 Carry 判断哪些门可以沿用。
- 新建文件或原本未跟踪但属于本次交付的文件，worker 必须先按路径加入 VCS，
  再继续修改；不要执行 `git add .` 或触碰无关未跟踪文件。

Git、SVN、P4 的命令见
[references/vcs-snapshots.md](references/vcs-snapshots.md)。无 VCS 项目不进入
正式流程。

## 结果与中断

每条审查门 finding 都带 P0、P1 或 P2。无 finding 或仅 P2 时为 `PASS`；
至少一条 P0/P1 时为 `FAIL`；`RUNTIME_ERROR` 不带 finding。已选择的
`PENDING` 会阻止 Seal；运行错误需要重试或用户明确跳过。QA FAIL 和 P0/P1
在共享三轮 review-wave 额度耗尽前必须返修，之后才可显式授权跳过。仅 P2
的建议会保留展示，但不阻止 Seal。

每个独立代理结果都只是候选输入。记录或展示 blocker 前，主代理必须按完整已
确认需求和保留的工作流状态检查它，独立复现其文档化正常使用公开入口路径，
并核实证据、范围、严重度和因果关系。任一检查失败就丢弃该 finding，不改变
工作流状态、需求或实现。

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
