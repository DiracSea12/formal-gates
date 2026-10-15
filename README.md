# formal-gates

**从需求确认到 Seal 的 AI 辅助开发工作流。**

[English](README_EN.md) | [中文](README.md)

formal-gates 把写、审、测放在互相隔离的会话中，由 engine 执行确定性推进、保存原始
结果与接纳记录。主代理澄清需求、核实证据并转达用户决定；不会替用户选择主要方案，
也不会靠对话记忆决定下一步。

普通项目修改先非阻塞提醒一次：“若需走 formal-gates 流程，可直接提出。”复杂需求在
需求与方案确认完毕、准备开做时再建议一次。只有用户明确要求时才启动正式受理。

## 正式流程

1. **澄清与确认。** 主代理读取仓库事实，解决重大未决选择，把完整需求与方案写入唯一
   正式文件。先确定性结构预检，再展示全文和精确 revision。
2. **启动与登记。** 明确 regular/lightweight，通过原生表单确认完整启动操作，程序直接接纳 typed intake 并启动 run。
   首次 drive 自动登记确认；后续产品审、技术审、开发与 QA 消费同一确认 revision。
3. **独立双审。** 先产品审、再技术审。Operator 逐项核实 finding，用户处置有效问题。
   技术审形成 Batch→Subtask、依赖、DoD 与恢复计划。
4. **计划与路线。** 技术审后绑定 split 精确拓扑或 no-split 理由，再确认所选门与开发计划。
   命名流程配置同时保存流程节点开关和各执行组的平台、模型、推理强度，黑盒与白盒各为一组；
   初始仅有“默认”配置：主流程节点启用、无普通 QA、全部普通门，执行选择继承宿主。
   当前候选门可选任意子集（含空集）或全集，每个 child 独立确认；child 不选普通 QA。
   master 按所选职责执行合并 QA 与合并门。
5. **并发开发。** 选择黑盒时，开发与其设计/审查并发。黑盒只读基线，开发者与审查者隔离。
   每个 Batch 新建开发会话，批内重试与恢复沿用原身份。
6. **冻结候选。** adapter 校验 manifest 后跟踪、提交和快照；选择白盒时独立核对覆盖与测试。
   全部已选 QA 的 FROZEN 用例与交付文件纳入最终候选，Git 压缩在最后必需验证前完成。
7. **并行验证。** engine 冻结完整 expected-set，按容量并行执行 QA 与各门，及时补位。
   门看基线到最终候选完整 diff，独立 QA 的用例与执行证据不作为门审输入。
8. **统一返修。** wave 收齐、错误处置与问题裁决完成后，只建一份 repair。普通返修
   固定只跑受影响 QA；按[公共合同](references/formal-flow.md#继承判定修复授权与-seal)核齐本轮义务后完成，不追加最终 FULL。门审 P2
   修复后 carry、不复审；门审 P3 只作 Seal advisory。
9. **Promotion 与 Seal。** 核对同一候选的等价 receipt，清理全部登记资源并证明无残留
   后才 Complete。Seal、主线集成、真实 canary 和全局切换分别留证。

lightweight 只留下明确“本 run 未经任何验证”的 unverified 记录，不开发、不快照、
不选 QA/门路线。regular 不在开发中降为 lightweight。

[正式流程公共合同](references/formal-flow.md) 唯一持有收据字段与调用形式；
[完整示例](references/example-run.md)、[拆分与主线](references/sliced-runs.md) 和
[VCS 边界](references/vcs-snapshots.md) 展开对应机制。

## 使用方式

装好后，对 AI 说：“帮我用 formal-gates 处理这次改动。”用户只需回答实际未决选择，
并在规定节点确认。公开 workflow 入口穷举为：

`start` / `drive` / `submit` / `show` / `status` / `next` / `progress` / `message` /
`replan` / `handoff` / `diagnose` / `guard-preview` / `serve`。

主宿主的 `confirm_start`、`confirm_control`、`confirm_native` 由 `workflow serve` 提供。
自然语言只提出申请；服务显示绑定具体操作的原生确认，用户点击后直接提交原对象。
模型不能填写批准值、制作确认收据或以普通 CLI 参数代替真人响应。
Codex、ZCode 安装控制 MCP；DSH global 复用根代理原生问题服务，其他宿主的接入边界见[安装与 Hook](references/install-and-hooks.md)。

start 是 bootstrap，drive 执行确定性推进与恢复，submit 接纳业务结果与已确认决定；
message 追加同 action 临时消息，handoff 请求原恢复 Ask，replan 准备并保存计划重审确认载荷。
`settings` 可保存、修改和启用用户命名配置；`confirm_start.configuration` 仅选择本次配置。
运行中可用 `submit --configuration <名称或序号>`，或 `--flow-node <节点> --enabled true|false`、
`--qa-mode <blackbox|whitebox> --enabled true|false` 请求调整本次流程，沿原 Ask 确认完整配置与影响。
启用过去或跳过的节点后可从原节点继续；修改保存默认不会自动影响已有 run。

show/status/diagnose/guard-preview 只读；next 的语义选择准备确认载荷，progress 可显式保存进度载荷。准备或保存不代表用户确认。设置见[用户与项目设置](references/user-configuration.md)。需求变化、路线调整、reset、abort、waiver/skip 和 HUMAN 合法节点跳转通过当前
availableActions 的 typed Ask 提交。UNKNOWN 先对账，不能盲目重派或重做 VCS 副作用。

版本、定义/包摘要或安装身份不匹配时拒绝正常读取与写入；diagnose 可原始只读检查。
不迁移或续写不受支持的 run，使用新 ID 创建当前版本 run。终态摘要可只读查看。

`workflow guard-preview --input <basis.json>` 是生产 guard 的非权威只读计算，不创建
run、不认证用户确认或任务合法性。[输入与算法](references/guard-preview.md)说明完整合同。

## 节点、宿主与证据

普通内部节点要实现 handler、注册 typed 能力并接入编译定义，执行与恢复共用提交和
接纳路径。它不是任意动态插件 SDK；新的控制语义仍须按实际边界设计。

宿主适配器只持有原生启动、执行、观测、恢复与证据传输，业务决策由 engine 与用户
typed Ask 管理。安装支持和执行支持分别核对；doctor、模拟与配置存在不能替代真实
canary。发布与全局切换分别保留独立验收证据；六宿主与 Git/SVN/P4 非分片/分片
的完整验收须绑定同一候选和实际安装副本，未运行项如实标记未验证或 skip。


## 安装与卸载

### 从 release 安装（推荐）

不需要 Go 工具链。下载目标 release 的源码包，解压后在包内运行安装脚本（macOS/Linux 用 `install.command`，Windows 用 `install.bat`）。macOS/Linux 用 `--version <目标 tag>`，Windows 用 `-Version <目标 tag>` 指定要安装的版本；未指定时读取 `FORMAL_GATES_VERSION`，再缺省为 `v0.1.0`。运行某个源码包内的脚本不会自动选择该包版本。脚本下载所选 tag 的源码及匹配当前平台的正式二进制、canary 与 SHA256 校验和，校验后组装本地包并调用同一个原生安装器：

```bash
./install.command --host claude --scope global
# Windows: install.bat
```

### 从源码构建（只读验证）

源码构建产物是候选 binary，用于包验证和隔离 canary，不接受普通安装、bootstrap、
卸载或 workflow 写入。release 引导脚本在固定 stable launcher 不存在时放入校验后的 binary，
由该 launcher 执行唯一原生事务。只有既有 launcher 拒绝 release 安装的新参数且显式传
`--force`（Windows 为 `-Force`）时，脚本才由已校验 release 源码包内的 binary 执行同一事务，
更新默认固定 launcher；替换前备份由原 journal 持有。详见[安装与 Hook](references/install-and-hooks.md#原生安装)。源码验证：

```bash
go build -o bin/formal-gates ./cmd/formal-gates
bin/formal-gates package validate --root .
bin/formal-gates canary portable --root . --format json
```

Windows 使用 `bin\formal-gates.exe`。`bin\formal-gates.exe` 是 Windows 资产：在
macOS/Linux 上不要试图运行它（会报 `exec format error`），请使用各自平台的
`bin/formal-gates`。

### 安装位置

| 宿主 | 全局（global） | 项目（project） |
| --- | --- | --- |
| Claude Code | `~/.claude/skills/formal-gates` | 所选项目下的对应目录 |
| Codex | `~/.codex/skills/formal-gates` | 所选项目下的对应目录 |
| Cursor | `~/.cursor/formal-gates` | 所选项目下的对应目录 |
| DeepSeek Harness | `~/.dsh/skills/formal-gates`（`$DSH_HOME` 下） | 所选项目 `.dsh/skills/formal-gates` |
| ZCode | `~/.zcode/skills/formal-gates` | 所选项目 `.zcode/skills/formal-gates` |
| WorkBuddy | `~/.codebuddy/skills/formal-gates` | 所选项目 `.codebuddy/skills/formal-gates` |

安装会把本包自带的宿主 hook 合并进对应配置：Claude Code 写 `~/.claude/settings.json`，Codex 写 `~/.codex/hooks.json`，Cursor 写 `~/.cursor/hooks.json`（项目级安装写所选项目下的对应文件）；DeepSeek Harness 全局安装写 `$DSH_HOME/cordis.patch.yml` 并挂载包内最小 Cordis 插件，项目级安装只装 skill 与 `AGENTS.md` 指令（DSH 只自动加载 home 级补丁）；ZCode 全局安装写 `~/.zcode/cli/config.json`，项目级安装写入 skill、`AGENTS.md` 指令与 `.zcode/config.json` 中的 MCP 配置（当前 ZCode 忽略项目级 hook 配置）。WorkBuddy 只安装 skill 与 CODEBUDDY.md，原生工具/生命周期由 engine 接入，不写 global hook。已有的非 formal-gates hook/插件行不会被覆盖；多个宿主共享项目 `AGENTS.md` 时，只有最后一个仍在使用该文件的宿主卸载才会清理 managed marker。

安装还会维护受理规则：Claude Code 使用全局 `~/.claude/CLAUDE.md` 或项目
`CLAUDE.md`，Codex 使用全局 `~/.codex/AGENTS.md` 或项目 `AGENTS.md`，Cursor 项目
使用 `.cursor/rules/formal-gates.mdc`，DeepSeek Harness 使用 `$DSH_HOME/AGENTS.md`
或项目 `AGENTS.md`，ZCode 使用全局 `~/.zcode/AGENTS.md` 或项目 `AGENTS.md`。Cursor 全局不创建规则文件，只保留现有运行时和
hook 集成。当前规则写在宿主指令区块 `<formal-gates:host-instructions:start>` 与
`<formal-gates:host-instructions:end>` 之间；重复安装会替换区块内容并把重复区块收敛为一个。
从旧版（包括旧 target/bin hook 或旧 marker）升级时，直接用 release 引导脚本加
`--force` 安装；唯一事务会把安装器拥有的旧 hook 迁移到固定 launcher，并收敛规则区块。

### 原生卸载

卸载使用相同的 host、scope 和 project 参数，并清理 formal-gates 运行时、安装器拥有
的 hook 条目和完整 marker 规则区块，同时保留区块外文档内容与其他 hook：

```bash
$HOME/.local/bin/formal-gates uninstall --host claude --scope global
$HOME/.local/bin/formal-gates uninstall --host cursor --scope project --project <project>
$HOME/.local/bin/formal-gates uninstall --host dsh --scope global
$HOME/.local/bin/formal-gates uninstall --host dsh --scope project --project <project>
$HOME/.local/bin/formal-gates uninstall --host zcode --scope global
$HOME/.local/bin/formal-gates uninstall --host zcode --scope project --project <project>
# Windows: %LOCALAPPDATA%\formal-gates\bin\formal-gates.exe uninstall ...
```

规则区块按 marker 删除，即使运行时目录已经不存在也不需要规则源码。

### 参数语义

- `--force`：目标已存在时替换它。
- `--skip-hooks`：只安装包，不改宿主 hook 配置和受理规则（只有当这些内容必须逐字节不变时才用）。

---

## 本地验证与贡献

包结构、提示词与确定性检查见 [本地验证](references/local-validation.md)。原生 canary
必须逐宿主取得真实执行、恢复、receipt 和清理证据；未执行项不能标 PASS。

新增门的审查正文在 `gates/*.md`，可执行任务仍须注册并接入当前定义。行为变更更新
CHANGELOG；问题或建议使用 GitHub issues。

[MIT](LICENSE) · [GitHub Releases](https://github.com/DiracSea12/formal-gates/releases)
