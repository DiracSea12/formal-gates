# Design

## A. 接手路径

### A1. workflow start 允许祖先基线

`Start`（internal/validate/workflow.go:75-82）放开"base 必须等于当前快照"的 EqualFold
强校验：改为 supplied 经 resolver.Verify 通过、且是当前 HEAD 的祖先或相等。
BaseSnapshot = supplied，CurrentSnapshot = 当前 HEAD。其余逻辑（requireNativeCurrent、
快照推进、门审 base→current 比较）不变。

### A2. 接手协议（SKILL.md + references）

新增接管流程：便宜健全性检查（目标项目构建/测试）→ 重走需求澄清并把对齐结果写入需求
文件 → `workflow start --base-snapshot <B0>` → dev worker 继续 → QA（整功能用例）+ 门
审（完整 diff）→ 主代理按需求过滤无关 finding → seal。resume 降级为备用路径。改动以修
改 SKILL.md 既有章节与 references/formal-flow.md 为主，尽量不新增文件。

同时修正 SKILL.md 正式流程第 2 步措辞：该步只负责登记已对齐需求并记录 PASS；澄清问答、
呈现整合需求、取得用户最终确认、对齐结论持久化到需求文件，全部在受理阶段完成（非
catalog 改动）。

## B. 修改路径

### B1. 逐门 prompt 哈希

`RunState` 新增按门/action 的 prompt 内容哈希记录（runstate.go），启动时由
PromptCatalog 计算。旧状态文件无该字段时兼容加载。

### B2. 修改判定（不限于 prompt 文件）

run 中途任何项目文件修改（catalog 提示词、源码、需求产物、任意 VCS 提交）SHALL NOT 自
动全量失效。`requireCurrentCatalog`（workflow.go:1231）改为按逐门哈希报告 delta；只动未
选门/action → 允许继续；已选门变化由主代理按实际改动范围判定继承（记录理由）或要求该
门新派发。错误信息从"start a new run"改为可恢复分类。其他类型修改经 VCS diff 检视，由
主代理按同一规则判定。

### B3. 采纳外部改动命令

扩展 `workflow resume` 承载"采纳外部改动"：resume 时若 HEAD 已漂移，显式重绑
CurrentSnapshot 到当前 HEAD 并记录 origin+reason（需用户确认）；`requireNativeCurrent`
（workflow.go:1260）对已显式重绑的状态放行；主代理据此继承不受影响结果。

### B4. 开发后 meaning-preserved 重绑定

删除 workflow.go:180 的硬禁止；`UpdateRequirement` 在 semanticEffect=preserved 且开发
已开始时，要求主代理记录"语义未变"认定 + 用户确认，保留全部 PASS 并重绑快照。

### B5. 统一继承判定

复用 RecordCarry（workflow.go:938）与 carryOriginMainShortcut：允许在任何重绑定时刻
（采纳/中断恢复/修复）以 INHERIT/RERUN + reason 记录继承判定。主代理可继承（写明理
由）；否则独立判定或重跑。审查轮次上限对真实重跑生效。

### B6. 判定手册

SKILL.md 增加判定 rubric：每类 delta（catalog/需求/VCS）什么算"可证明不受影响"、什么
必须独立判定或重跑。修改既有章节，不新增文件。

## P. 返修重审范围

### P1. 提示词显式声明全量范围

`ComposeGatePrompt`/`currentChangeBlock`（internal/validate/runner.go:109）在重跑轮
（wave>1 或 PreRepairSnapshot 已设）增加显式声明："这是返修后第 N 轮重审；上一轮覆盖
base→pre-repair；你的范围是完整的 base→current；pre-repair 快照仅供参考，不要只审返
修增量。" 以修改 runner.go 既有 block 实现，不新增提示词文件。

### P2. 结果契约报告比较快照对

门审结果契约（runner.go:40）新增 `compared` 字段（如 `"base..current"`）。主代理记录前
校验：compared 与指定范围不匹配即丢弃结果；validate 层相应校验。

### P3. 范围区分与文档

carry 提示词保持 pre-repair→current 不变；vcs-snapshots.md 与 SKILL.md 更新"重跑门用
完整 base→current，审查者必须报告比较的快照对"。修改既有文档。

## 测试与边界

每个行为在最低层测试（workflow_test.go、catalog/runner 相关）。遵循 CLAUDE.md 项目边
界：不添加对抗性、改写内部状态、权限、不可变文件或不受支持平台用例。CHANGELOG 更新。
