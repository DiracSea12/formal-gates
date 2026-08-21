# Proposal

本阶段交付确定性决策内核与只读 Shadow：按 ADR-001 以封闭 Go 类型变体编写流程定义，由小型 closed-world compiler 校验并编译为字节级稳定的 canonical 制品，`Observe → Decide → SelectIssued` 计算完整 eligible frontier 与 canonical Plan；Shadow 只读对比 legacy 状态与引擎预测，不写权威 workflow 状态、不产生第二写入者。

阶段开始前完成六种代表性 step 的 compiler spike 以确认 compiled IR、registry 与 canonical encoder 边界；spike 不进入 production。本阶段同时登记 ADR-001 架构修订（typed authoring + compiled canonical artifact，提交 598839f/db1822b）的正式受审，并修复阶段 0 遗留的环境缺陷：phase-0 故障注入测试污染真实用户级安装（registry 142 条测试记录、stable launcher 与全局 bin 被写成空桩），stable driver 已从冻结源 5373c13 重建恢复（package validate + canary portable PASS，桩备份 /tmp/stub-backup/），本阶段交付测试隔离修复。

## 参考关系

- 切片与退出条件：`refactor-plan/incremental-seal-plan.md` 第 2、3 节（阶段 1）。
- 架构决策：`refactor-plan/adr-001-typed-authoring-compiled-canonical-definitions.md`。
- 详细架构：`refactor-plan/final-implementation-draft.md` 第 3、11 节（阶段 1）。
- 总需求：`openspec/changes/orchestration-pipeline-engine/master-requirements.md` 第 5 节。

## 非目标

- 不实现第二个 workflow writer、不写权威 state、不接管公开流程（阶段 2）。
- 不实现 `drive/submit` 公共面或业务流程迁移（阶段 3+）。
- 不改变 stable driver 的公开语义；`MISSING_ENGINE_ADAPTER` 只作为 diagnostic-only marker。
- 不以 source-tree 单测代替 installed-binary、隔离 namespace 或 shadow 证据。
