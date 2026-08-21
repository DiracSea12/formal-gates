# 阶段 1：纯决策内核与只读 Shadow——阶段需求入口

状态：沿用用户已确认的阶段选择（2026-08-22）；本文件是本次 formal-gates run 的阶段化需求入口。

## 权威来源与边界

- 阶段切片、环境模型和退出条件以 `refactor-plan/incremental-seal-plan.md` 为准（第 2 节、第 3 节"阶段 1"）。
- 步骤模型与定义制品形态以 `refactor-plan/adr-001-typed-authoring-compiled-canonical-definitions.md` 为准（重大技术选择已冻结）。
- 详细目标架构以 `refactor-plan/final-implementation-draft.md` 为参考（第 3、11 节）；总需求由 `openspec/changes/orchestration-pipeline-engine/master-requirements.md` 持有。
- 本 run 同时正式登记 ADR-001 架构修订（提交 598839f/db1822b）的受审。
- 若阶段切片与架构文档表述差异，以本阶段确认需求和增量 Seal 计划阶段 1 条目为准。

## 环境基线（含阶段 0 缺陷修复记录）

- 固定 stable driver：`~/.formal-gates/releases/0.1.0-macos-arm64`。阶段 0 故障注入测试曾将其 launcher 与 `~/.local/bin/formal-gates` 写成 25 字节空桩（2026-08-21 01:34/20:54，与 `internal/validate/package_test.go:252` 桩内容逐字节一致）；2026-08-22 从冻结源 commit `5373c13`（release 树 SKILL/manifest 指纹匹配）以 git archive + go build 重建恢复，`package validate` 与 `canary portable` 均 PASS，桩备份于 `/tmp/stub-backup/`。本 run 全程由该修复后 launcher 驱动。
- phase-0 run（phase-0-distribution-002）执行期间 launcher 已是桩，其实际驱动二进制不可考，与"全部正式 run 由冻结 stable driver 驱动"的自狗粮规则存在偏离；如实留痕，自本 run 起每次会话首次调用前 smoke 留证。
- 测试隔离缺陷（污染真实 `~/.formal-gates/registry.json`，142 条测试记录）在本阶段修复并回归。

## 本 run 预定决策

- 拆分：no-split（单一强耦合内核单元；理由随 slicing 命令留痕）。
- 路线：full（黑盒 + 白盒 + 全部四道门，与阶段 0 一致）。
- 开发顺序：compiler spike 先行，确认边界后正式实现。

## 阶段退出条件

按 `incremental-seal-plan.md` 第 5 节共同条件与阶段 1 条目验收；canonical 制品十条验收、mutation/止损、installed binary 证据绑定候选 identity。
