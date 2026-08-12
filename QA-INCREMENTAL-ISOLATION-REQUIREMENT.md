# QA-INCREMENTAL-ISOLATION 需求：黑盒用例隔离 worktree 合回 + QA 用例真增量

本文档登记 qa-incremental-isolation 正式 run 的需求与方案。需求由三处改动组成，用户已
完整确认（受理阶段澄清与方案确认于 2026-08-12 完成，确认后进入正式流程）：

- **改动 1**：黑盒 QA 用例文件隔离在隔离工作区（git/svn/p4 三 VCS 统一），到黑盒用例
  执行结束（seal 时点）再合回主干。
- **改动 2**：QA 用例增加改为真增量，不再清除已有用例。
- **改动 3**：write-block hook 写阻断收窄——只按真实写目标判定、只读命令放行（修正
  RQ-011 实现偏差：命令文本含 `.gates` 子串不再命中）。

## 需求背景

现状：黑盒 QA 用例是 run-state 里的结构化记录（`QACasesByMode`），黑盒
qa-design/qa-review 在隔离 worktree 中推进，但用例文件本身并不存在于隔离 worktree，
也没有"合回主干"的动作。qa-design 当前做**完整集合替换**：通过语义键（description/
procedure/oracle/test）保留既有 PASS 状态，一旦设计返回的集合不完整，未提及用例就会被
清除。

用户明确的两处改动：

- 黑盒用例文件要放到隔离工作区：**文件在隔离区里**、黑盒阶段主干上看不到，黑盒用例执行
  结束再合回主干。git 用 linked worktree、svn 用基线版本工作副本、p4 用基线 changelist
  客户端——三者没有统一的 worktree 概念，机制"类似处理"：隔离 + seal 合回的语义在
  git/svn/p4 三 VCS 上一致（复用现有隔离工作区登记机制 `qa-worktree`，后者已支持三
  VCS）。
- qa 用例增加要**真增量**：不再清除已有用例。默认只提交变更，未提及用例及其 PASS 状态
  自动保留；整体替换走显式逃生门。

## 需求 1：黑盒 QA 用例文件隔离在隔离工作区，seal 时合回主干（三 VCS 统一）

### 机制

- **隔离期间**：qa-design 记录时，CLI 从 run-state 派生 blackbox 用例 mirror 写入隔离
  工作区的 `.gates/cases/blackbox.md`（git 的 linked worktree、svn 的工作副本、p4 的
  client 各自的对应路径）。仅 blackbox mode、仅隔离工作区已登记（`qa-worktree` 注册）
  时写入。
- **黑盒审查**：黑盒 qa-review 派发读取隔离工作区里的用例文件进行审查（绑定基线的隔离
  工作区，恒等于基线、不含本次开发代码）。
- **合回时点**：seal。CLI 把已批准 blackbox 用例从 run-state 物化到主工作区
  `.gates/results/`（现有用例落盘处，与 seal ledger 同目录），文件名为
  `<run-name>.blackbox-cases.md`；与 ledger 同路径、同交付行为（三 VCS 一致）。
- **合回执行者**：CLI 完成，不经 agent 手动合回、不经 git merge、不在提示词里要求任何代
  理搬运文件。
- **三 VCS 统一**：git（linked worktree）、svn（基线版本工作副本）、p4（基线 changelist
  客户端）的隔离工作区均支持写入与 seal 物化。

### 细节

- mirror 是 run-state 的**派生视图**（单一来源，无双重 source drift），CLI 在 qa-design
  记录时从 run-state 写入。
- qa-design 在 review 开始前可反复补全（现有规则），每轮记录重写 mirror。
- 黑盒 review PASS 后清空隔离工作区（现有机制）时 mirror 随工作区清空；seal 物化读
  run-state，不依赖工作区残留。
- 分片实例封板不产独立 ledger 文件（既有行为）；黑盒用例的 seal 物化仅由主干实例在 seal
  时执行，分片实例不物化。非分片 run（本 run）直接物化。
- 黑盒执行仍在主 worktree（现有 `requireMainNative` 不变），执行读已批准用例。

## 需求 2：QA 用例增加改真增量

### 机制

- **默认增量**：qa-design 只返回变更（新增/修改/删除的用例 id + 新增/修改用例的完整规
  格），CLI 合并进 run-state；未提及用例及其 PASS 状态自动保留，不再清除。
- **显式删除**：`--remove-case <id>` 删除指定用例（CLI 校验该 id 存在，不存在即报错）。
- **显式整体替换**：`--replace-all` 整体替换该 mode 用例集（对应现有"整体工作流变更时换
  整套"语义；替换空集即清空该 mode）。
- **契约变化**：qa-design 契约改为"只返回你的变更"；qa-review 仍审查完整合并集——qa-review
  提示词注入本轮新增/修改/删除的用例 id 列表 + 完整合并集，保持审查全上下文。

### 细节

- rework 约束保留：无实质变更的 qa-design 记录仍被拒（必须新增/修订用例，或
  `--remove-case`/`--replace-all`）。
- 空用例集分支语义随增量调整：默认增量不会因"未提及"而清空该 mode；只有显式
  `--replace-all` 空集才清空。
- 语义键 PASS 保留逻辑（description/procedure/oracle/test 匹配保留 `normalized.ID` 与
  `ReviewStatus`）被增量合并语义取代：未提及即保留，无需逐条语义匹配。
- 用例 id 在所有 mode 全局唯一（`CASE-001` 格式）不变。
- 增量契约的 id 失配行为定死：**新增**用例不带 id、由 CLI 分配（全局唯一）；**修改/删除**
  按 id 引用，引用不存在的 id 或 id 给错 → CLI 拒绝并报错，不静默当新用例造成重复。无 id
  提交的规格与既有用例语义重复（description+procedure+oracle 一致）时同样报错拒绝并提示改
  用修改语义，不分配新 id。

## 需求 3：write-block hook 写阻断收窄——只拦真实写入，只读命令放行

### 背景

RQ-011（2026-08-09 提交 62057e6 引入，P1 QA 解耦与 Carry 继承修复 run）规定主代理与
审查类代理写阻断，且明确**只读命令放行**。当前实现 `commandWritesFiles` 与
`bashWriteTargetsCodeOrState` 以命令文本是否含 `.gates` 子串判写入，把只读查询
（grep/ls/rg/python3 读等，只要提到 `.gates`）也拦掉，违反 RQ-011 的"只读命令放行"。
本需求把实现拉回需求本意。

### 机制

- **判定原则**：主代理（主线程）写阻断只按"命令/编辑是否真实写代码或 run 状态
  （`.gates`）文件"判定，不以命令文本中是否出现 `.gates` 子串为依据。
- **只读放行**：只读命令（grep/rg/ls/cat/find/python3 读、只读 git 查询 git log/status/
  show/diff 等）即使命令文本提到 `.gates` 也放行。
- **真写仍拦**：git commit/push/merge/rebase/reset --hard/checkout --/clean/add；输出
  重定向到 `.gates` 或代码文件（`> .gates/...`、`>> main.go` 等）；文件变更工具
  （tee/rm/mv/cp/touch/mkdir/sed -i/install）指向 `.gates` 或代码文件——仍拦。
- **Edit/Write 工具**：按目标路径判定（`isCodeOrRunStatePath`）不变。

## 落地位置

1. `internal/validate/workflow_qa.go`：`RecordQADesign` 改为增量 merge 语义 + 黑盒
   worktree mirror 写入；`RecordQAReview` 黑盒派发读取 worktree 用例文件；rework 约束按
   新语义调整；`discardUnmatchedQADesign` 随"未提及即保留"调整。
2. `internal/validate/workflow_prompt.go`：qa-design 契约改为"只返回你的变更"；黑盒
   qa-review 提示词引用隔离工作区用例文件路径。
3. `internal/cli/cli.go`：`qa-design` 命令新增 `--case-id <id>`（修改既有用例，id 不存在
   报错）、`--remove-case`（可重复）与 `--replace-all` flags。
4. `internal/validate/workflow_carry_seal.go`（seal）：物化已批准黑盒用例到主工作区
   `.gates/results/`（与 ledger 同路径同交付行为）。
5. run-state：按需记录 blackbox case mirror 状态（写文件后可与 worktree 清空联动）。
6. 文档：SKILL.md（第 5/7/9 步与 QA 相关节）、references/formal-flow.md（qa-design/
   qa-review/seal 命令注释）、references/example-run.md（走查示例）同步增量与隔离语义。
7. 测试：`internal/validate/qa_scope_incremental_test.go`、`decoupled_qa_test.go`、CLI 测
   试适配；新增增量合并（未提及保留、--remove-case、--replace-all）与 mirror 写入/物化
   测试；三 VCS 解析路径单测覆盖。
8. `internal/validate/write_block.go` + `write_block_test.go`（及 hook 相关测试）：改动 3
   收窄——移除命令文本 `.gates` 子串命中，按真实写目标判定；新增只读放行/真写仍拦用例。
9. 文档：`references/install-and-hooks.md`（hook 行为说明，如有）同步改动 3。

## 明确不动（边界）

- 白盒 QA 用例不隔离（白盒设计/审查在主 worktree，绑定当前快照，现状不变）。
- 隔离 worktree 的创建/登记/校验/清空/重建机制本身不变（`qa-worktree` 现有机制）。
- 黑盒执行位置（主 worktree、`requireMainNative`）不变。
- 需求修订或 start-readiness FAIL 时黑盒用例增量修订规则不变（只增删改受影响用例）。
- 黑盒 review 连续 3 次 FAIL 的"长期不过"处置与显式放行机制不变。
- 成本计量、生命周期 hook、派发规范文件（hash 校验）机制不变。

## 验收

- 黑盒 qa-design 记录后，隔离工作区的 `.gates/cases/blackbox.md` 内容与 run-state 中该
  mode 用例一致；黑盒 qa-review 读取该文件审查。
- 默认增量：qa-design 只提交新增/修改/删除，未提及用例及其 PASS 状态保留；不再出现"未提
  及即清除"。
- `--remove-case <id>` 删除指定用例（id 不存在报错）；`--replace-all` 整体替换、空集清空
  该 mode。
- seal 后主工作区 `.gates/results/` 出现已批准黑盒用例物化文件
  `<run-name>.blackbox-cases.md`（与 ledger 同目录）。
- git/svn/p4 三 VCS 下机制一致（单测覆盖解析路径）。
- write-block hook：活动 run 下主线程只读命令（含 `.gates` 字样的 grep/ls/python3 读、
  只读 git 查询）放行；真写 `.gates`/代码（git add/commit、`> .gates/...`、`tee
  .gates/...` 等）仍被拦。
- 既有测试套件全部通过；正式流程（本 run 自身即 dogfooding）走到 seal 完成。
