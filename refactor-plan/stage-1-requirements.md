# 阶段 1：纯决策内核与只读 Shadow——阶段需求入口

状态：沿用用户已确认的阶段选择（2026-08-22）；本文件是本次 formal-gates run 的阶段化需求入口。

## 权威来源与边界

- 阶段切片、环境模型和退出条件以 `refactor-plan/incremental-seal-plan.md` 为准（第 2 节、第 3 节"阶段 1"）。
- 步骤模型与定义制品形态以 `refactor-plan/adr-001-typed-authoring-compiled-canonical-definitions.md` 为准（重大技术选择已冻结）。
- 详细目标架构以 `refactor-plan/final-implementation-draft.md` 为参考（第 3、11 节）；总需求由 `openspec/changes/orchestration-pipeline-engine/master-requirements.md` 持有。
- 本 run 同时正式登记 ADR-001 架构修订（提交 598839f/db1822b）的受审。
- 若阶段切片与架构文档表述差异，以本阶段确认需求和增量 Seal 计划阶段 1 条目为准。

## 环境基线（含阶段 0 缺陷修复记录）

- **固定 stable driver（2026-08-22 用户拍板重冻结）**：`~/.formal-gates/releases/0.1.0-macos-arm64` 内容与两个 launcher（含 `~/.local/bin/formal-gates`）已重建为 main HEAD `7929891`（git archive + go build；SKILL 指纹 46941e99 一致；`package validate` 与 `canary portable` PASS）。阶段 1–6 固定使用该驱动，不再随开发更新；阶段 7 按计划切换。
- **重冻结原因（阶段 0 契约缺口的显式修正）**：阶段 0 名义冻结的 5373c13（8 月 4 日）缺少 8 月中旬实现的现行流程命令（`--split`、`slicing`、`settle-findings`、`qa-worktree` 等），从未实际驱动过任何 run；phase-0 run（含 slicing 记录）实际由更新构建驱动，违反"全部正式 run 由固定 stable driver 驱动"的自狗粮规则。旧树备份于 `/tmp/stub-backup/release-old-tree-5373c13.tar`。
- 阶段 0 故障注入测试曾将旧 release launcher 与 `~/.local/bin/formal-gates` 写成 25 字节空桩（2026-08-21 01:34/20:54，与 `internal/validate/package_test.go:252` 桩内容逐字节一致）；该污染连同真实 `~/.formal-gates/registry.json` 的 142 条测试记录构成测试隔离缺陷，在本阶段修复并回归。
- 自本 run 起，每次会话首次调用驱动前执行 launcher smoke 并留证。

## 本 run 预定决策

- 拆分：no-split（单一强耦合内核单元；理由随 slicing 命令留痕）。
- 路线：full（黑盒 + 白盒 + 全部四道门，与阶段 0 一致）。
- 开发顺序：compiler spike 先行，确认边界后正式实现。

## 阶段退出条件

按 `incremental-seal-plan.md` 第 5 节共同条件与阶段 1 条目验收；canonical 制品十条验收、mutation/止损、installed binary 证据绑定候选 identity。
