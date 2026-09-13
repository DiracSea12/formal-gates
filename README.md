# formal-gates

formal-gates 在用户明确要求时，把需求确认、独立开发和审查、QA、返修与 Seal 按固定
流程推进。当前 Git、no-split regular 支持 **Codex、WorkBuddy、ZCode、DeepSeek Harness**；
各入口在启动前检查所需原生能力。split、其他 VCS 和其他宿主 regular 留待后续阶段。旧 workflow 命令、别名和 writer 已删除，
历史需求、原始结果与 Seal 保留原身份，旧 run 只提供原始只读 diagnose。

完整命令、typed 事件字段、动态绑定、任务内 model/QA 操作及恢复取证唯一见
[公开使用合同](references/engine-regular.md)。另见[配置](references/user-configuration.md)、
[正式流程](references/formal-flow.md)和[走查示例](references/example-run.md)。

使用者先把完整需求和方案写入一个文件，经结构预检后展示全文与精确 revision，单独确认。
产品审通过才做技术审，再确认 Batch 计划、no-split 和 full/custom 路线。开发与基线隔离的
黑盒设计/审查并行；黑盒失败通过有限 attempt、语义轮或恢复 Ask 继续，不搁置到开发末期。

QA 的 command 随用例冻结，程序执行准备→目标→采集。最终 QA 与门在唯一候选/wave
并行，一个目标失败不停止独立兄弟任务。收齐并逐 finding 裁决后统一返修；普通修复固定只跑受影响
AFFECTED 子集，本轮必需 QA 与门全部有效 PASS（含合法继承）即可 Seal。门审 P2 修复通过明确 carry 保留门 PASS，P3 只作
advisory。需求变化先暂停，review-delta 与影响预览经精确确认后更新。guard-preview 展示
唯一成本/额度投影；Seal 绑定实际 promotion 后的候选，不另造快照。

## 何时启用

主代理收到内容修改请求默认提醒一次：“若需走 formal-gates 流程，可直接提出”。在复杂需求
确认完毕、准备开做时，再提醒“检测到复杂需求，建议走 formal-gates 流程”。它不会自行触发，
你明确要求时才进入正式流程。需要只留记录时，确认同一正式文件后初选轻量路线 lightweight，
不验证并明确标记 unverified；之后不能把 regular 降为轻量。

## 安装与卸载

### 从 release 安装（推荐）

不需要 Go 工具链。下载最新 release 的源码包，解压后在包内运行安装脚本（macOS/Linux 用 `install.command`，Windows 用 `install.bat`）。脚本会下载匹配当前平台的正式二进制、canary 与 SHA256 校验和，校验后组装本地包并调用同一个原生安装器：

```bash
./install.command --host claude --scope global
# Windows: install.bat
```

### 从源码构建（只读验证）

源码构建产物是候选 binary，只用于包验证和隔离 canary，不能执行安装、bootstrap、
卸载或 workflow 写入。正式安装统一使用上面的 release 引导脚本，由脚本把校验后的
binary 放到固定 stable launcher 后再进入唯一原生事务：

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

安装会把本包自带的宿主 hook 合并进对应配置：Claude Code 写 `~/.claude/settings.json`，Codex 写 `~/.codex/hooks.json`，Cursor 写 `~/.cursor/hooks.json`（项目级安装写所选项目下的对应文件）；DeepSeek Harness 全局安装写 `$DSH_HOME/cordis.patch.yml` 并挂载包内最小 Cordis 插件，项目级安装只装 skill 与 `AGENTS.md` 指令（DSH 只自动加载 home 级补丁）；ZCode 全局安装写 `~/.zcode/cli/config.json`，项目级安装只装 skill 与 `AGENTS.md` 指令（当前 ZCode 忽略项目级 hook 配置）。已有的非 formal-gates hook/插件行不会被覆盖；多个宿主共享项目 `AGENTS.md` 时，只有最后一个仍在使用该文件的宿主卸载才会清理 managed marker。

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
- `--skip-hooks`：只安装包，不改宿主 hook 配置（只有当 hook 配置必须逐字节不变时才用）。

---

## 验证与发布时点

源码候选和隔离 canary 通过不表示全局发布。实际切换须完成独立 QA/Seal、集成、必要重验
和既有安装事务。构建、全量测试、race、phase0whitebox、vet、包验证、portable canary 与
完整 diff 检查见[本地验证](references/local-validation.md)。项目采用 [MIT](LICENSE)。
