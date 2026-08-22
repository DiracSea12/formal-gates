# 阶段 0/1 交付义务补漏 · 主需求

> 状态：已由用户确认（2026-08-22）。
> 义务来源：`refactor-plan/incremental-seal-plan.md` §2.3（唯一写入者与版本绑定）、
> §5（每阶段退出条件第 11 项与阶段记录清单）。
> 背景：反向普查证实阶段 0/1 封板时，§2.3 的命令路由矩阵与 §5 的阶段记录汇编
> 两类交付实物缺失；根因是阶段需求五件套（openspec alignment）只做了正向映射、
> 未做"计划义务 → 去向"的反向普查，且无机器校验。本 run 补交实物并固化防线。

## 1. 目标

补齐阶段 0/1 欠交的计划交付实物，并把"义务漏装"这一缺陷类别变成机器可查，
使后续阶段（2–7）封板前无法再静默漏掉同类交付。

## 2. 范围

### RQ-1 命令路由矩阵（§2.3 两段口径，补交 + 活文档）

新建 `refactor-plan/route-matrix.md`，格式按 §2.3 全列名：

- workflow 面：`start`、`show`、`status`、`next`、`diagnose`、`resume`、`abort`、
  `reset`、`requirement`、`route-candidates`、`slicing`、`settle-findings`、
  `route`、`route-add`、`qa-worktree`、`prepare-gate`、`prepare-action`、
  `claim-dispatch`、`record-action`、`record-gate`、`qa-design`、`qa-review`、
  `qa-execution`、`qa-execution-scope`、`snapshot`、`cleanup`、`carry`、
  `authorize-repair`、`seal` 以及 `drive/submit`，逐项明确：runtime、唯一 writer、
  schema/definition 版本绑定、允许的状态变化、错误码、是否只读。
- 维护/transport 面：`hook`、`lifecycle capture/verify`、`canary`、`gate`、
  `install`、`uninstall`、`package` 以及 registry `admission/register/reconcile`、
  cutover、rollback，逐项标明：只读 / 只写外部 observation/receipt / 委托 engine
  `submit`，及 owner、generation/token、receipt schema、恢复入口、权限边界。
- 每行按 §5.11 口径标注阶段 0/1 实际绑定：legacy（stable driver 语义不变）、
  install/bootstrap（阶段 0 事务）、Shadow/diagnostic（阶段 1 只读）、
  unsupported（drive/submit 等未实现面——必须显式拒绝或不存在，不得缺省冒充）。
- 补充行规则（用户拍板 2026-08-22）：实际存在但 §2.3 未枚举的公开入口
  （如 `workflow future`、`registry show`）以"计划未枚举"标注补行，绑定仅允许
  legacy/unsupported。**先覆盖后实现**：任何新公共面在代码实现落地前必须先
  进入矩阵；实现落地后矩阵与实际公开面一致，实际入口在矩阵中缺席即缺陷。
- 矩阵的 `start` 行注明无 `--split` 拆分意向声明约束的当前事实（legacy start
  仍带 `--split`，属 legacy 维持项，阶段 3 迁移时改写并留修订记录）。
- §2.3 要求的 negative tests 项：矩阵逐行引用既有机器证据（如
  `UNSUPPORTED_RUN_VERSION` fixtures、install journal 恢复测试、shadow 只读
  测试），不新增行为；确无既有机器证据可引用的行，标注"证据缺口 + 原因"，
  不得静默留空（与 RQ-2 的不可考机制对齐）。

### RQ-2 阶段记录汇编（§5 七项清单，阶段 0/1 两节）

新建 `refactor-plan/stage-records.md`，阶段 0 与阶段 1 各一节，逐项包含：

1. 阶段编号、run ID、sealed commit 与主线集成 commit；
2. 包摘要、installed-target digest、state schema version、workflow definition
   version、definition digest；
3. 固定稳定插件摘要和候选安装摘要；
4. 本阶段公开能力矩阵与唯一 writer（指向 RQ-1 矩阵的阶段节，并给一句式结论）；
5. 正常入口 smoke、新增能力 E2E、QA/gates 与 canary 证据指针；
6. 资源 cleanup receipt；
7. 下一阶段 worktree 的精确 post-integration canonical base 与关联 receipt。

内容从 git 历史、`.gates/results/phase-*.json`、黑盒用例交付物汇编，逐项给
可溯源指针；已不可考的项显式标注"不可考 + 原因"，不得虚构。

### RQ-3 反向普查固化（机器测试）

新增 Go 测试（`internal/validate`），机械断言：

- `refactor-plan/route-matrix.md` 存在且覆盖 §2.3 枚举的全部 workflow 子命令
  （含 drive/submit）与维护/registry 面条目；
- **实际公开面 ⊆ 矩阵**：从 `internal/cli/cli.go` 的 workflow 子命令注册表
  （源码解析或导出 API）派生实际子命令清单，断言实际清单中的每个入口都有
  矩阵行——机械落实"实现落地后不允许实际入口缺席矩阵"，新增子命令必须与
  矩阵同批变更（用户拍板 2026-08-22）；
- 矩阵中超出 §2.3 枚举的行必须带"计划未枚举"标注，否则测试失败；
- 每个矩阵行具备必需列（workflow 面：runtime/writer/版本/状态变化/错误码/
  只读；维护面：分类/owner/token/receipt schema/恢复入口/权限边界）；
- `refactor-plan/stage-records.md` 存在且阶段 0/1 两节各含七项必需段；
- §2.3 的命令枚举与测试内固定清单一致（计划文档增删命令时测试同步失败，
  强制双向维护）。

## 3. 验收标准

1. §2.3/§5.11 对阶段 0/1 的交付义务逐项有实物对应：矩阵两面全列名、阶段
   记录七项齐全；
2. RQ-3 测试在 `go test ./internal/validate/...` 下 PASS，人为删改矩阵/记录
   任一必需项时测试 FAIL（用例自证）；
3. 矩阵内容与仓库事实一致：每行的绑定结论可被既有代码/测试/封板产物佐证，
   不得出现与实现相反的行；
4. 不修改 `incremental-seal-plan.md`、`final-implementation-draft.md`、
   `openspec/changes/orchestration-pipeline-engine/master-requirements.md` 原文；
5. 不修改引擎代码（internal/engine）、不改 stable 安装。

## 4. 非目标

- 不追溯改写阶段 0/1 封板结论（SEALED 事实不动，本 run 是补交付物）；
- 不为阶段 2–7 预填矩阵内容（行保留、绑定列留待各阶段封板时更新）；
- 不新增常设敌意复审、信号面板或新流程规则（用户既定决策）。

## 5. run 参数

- 单 run、no-split（三个交付物同一因果链，拆分只会增加流程开销）；
- full 路线（黑盒 QA + 白盒 QA + 全部门），沿 fix-existing-defects 先例；
- main 检出驱动 stable driver；Seal 后提交 main；重构线分支不动。
