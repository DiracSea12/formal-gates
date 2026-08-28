# 阶段 3：最小纵向 engine 闭环——阶段需求

状态：沿用已确认的重构总需求与增量阶段计划；本文件是阶段 3 formal-gates run 的阶段化需求入口。

## 权威来源与边界

- 阶段切片、环境模型和退出条件以 `refactor-plan/incremental-seal-plan.md` 第 2、3 节为准。
- 公共循环、NextResult、runtime 选择、版本屏障和终结清理以
  `refactor-plan/final-implementation-draft.md` §1–§3、§9–§10 为准。
- 总体行为契约仍由 `openspec/changes/orchestration-pipeline-engine/master-requirements.md`
  持有；阶段 2 已封板的持久协议与恢复内核是本阶段输入，不在本阶段重做。
- ADR-001（typed authoring + 编译式 canonical 制品）与 ADR-002（开发分批与验证分层）继续有效。

## 目标

在不改变固定 stable driver 的 legacy 公开语义、且不产生第二个 workflow 写入权威的前提下，
交付第一条可独立验证、可 Seal 的 engine 端到端路径：engine lightweight run 从 start 经需求
登记、内部终结清理到 Complete；公开 façade 按 run envelope 选择整条 engine 或 legacy runtime，
同一 run 不跨 runtime。

## 范围与验收

1. 实现候选 engine 的 `workflow start`、`drive`、`submit`、`show`、`status`、`next`、
   `diagnose` 路由；`submit` 是唯一外部事件写入口。
2. 跑通 lightweight 的 `start → requirements-clarification PASS → requirement --confirmed`
   → engine 自动终结清理 → `Complete`；活动 engine run 不得由公开 `workflow cleanup` 删除或推进。
3. 覆盖 `Ask`、`Ready`、`HostAction`、`Wait`、`Operator`、`Complete` 六类 `NextResult` 外部边界，
   每次事件接纳后继续确定性决策并返回下一边界。
4. engine loader 在任何写入前校验 `workflowDefinitionVersion`、`stateSchemaVersion`、
   `definitionDigest` 和 owning-runtime `packageDigest`；缺失或不匹配稳定返回
   `UNSUPPORTED_RUN_VERSION`，仅 `diagnose` 可使用最小 raw/envelope parser 只读报告。
5. 通过实际 installed candidate 在隔离 test project 中验证 façade/runtime 选择、engine 状态
   namespace、终结 summary、自动 cleanup、legacy 回归和稳定环境不污染。
6. 通过确定性测试覆盖：旧入口不能绕过 engine handler，公开 cleanup 不能删除活动 engine run，
   同一 run 不可跨 runtime，terminal replay 只读且返回 `Complete`，版本屏障发生在首个写入前。
7. 批次计划、`Batch → Subtask` 映射及代理边界由 Part 2 技术审的 `granularity_review` 给出并
   留痕；一个 Batch 对应一次开发代理派发，Batch 内 Subtask 不换代理。

## 非目标

- 不在本阶段完成 regular、full/custom、split/child/merge、SVN/P4 或五宿主完整迁移。
- 不删除 stable driver 的 legacy 入口，不做最终全局切换或 stable 退役。
- 不新增公开 `drive --event`、`submit-result`、公开 cleanup 或第二写通道。
- 不把范围外对抗性输入、权限/不可变文件失败或不受支持平台扩展成阻塞验收项。

## 阶段退出条件

阶段 3 必须在一次完整 formal-gates run 内完成已绑定 Batch 的开发、候选 installed binary 验证、
legacy 回归、独立审查、QA、必要修复和 Seal；六类边界、版本写前屏障、runtime 选择、自动清理
和禁止旧入口绕过的证据均绑定候选 identity；范围内未通过项不得以口头说明替代。
