# 阶段 0：分发安全与基线冻结——阶段需求

状态：沿用用户已确认的阶段选择；本文件是本次 formal-gates run 的阶段化需求入口。

## 权威来源与边界

- 阶段切片、环境模型和退出条件以 `refactor-plan/incremental-seal-plan.md` 为准，重点是第 2 节和第 3 节“阶段 0：分发安全与基线冻结”。
- 详细目标架构以 `refactor-plan/final-implementation-draft.md` 为参考，重点是第 1–10 节及第 11 节“阶段 0：冻结契约”；它不改变本阶段切片范围。
- 总需求仍由 `openspec/changes/orchestration-pipeline-engine/master-requirements.md` 持有；本文件只把其中本阶段前置条件收敛成可交付单元。
- 若阶段切片与最终架构描述出现表述差异，以本阶段确认需求和增量 Seal 计划的阶段 0 条目为准；后续阶段语义不能倒灌到本阶段。

## 目标

在不改变 legacy workflow 文档化正常语义的前提下，冻结可复核的行为、分发、安装和版本基线，使后续阶段能够在稳定驱动、开发 worktree 与候选验证环境之间安全推进，并能从安装或进程故障恢复旧稳定包。

## 范围与验收

1. 建立 legacy 正常行为 characterization、package validation、portable canary 和安装 smoke 基线。
2. 记录初始 VCS identity、稳定 binary/package/installed-target digest、host hook/config、managed-rule canonical paths，以及阶段候选、测试项目、状态与证据目录格式。
3. 将稳定驱动冻结为不可变复制品；取消稳定安装指向开发 worktree 的 live symlink；package validation 使用 `Lstat`、realpath disjoint proof 和 digest 校验。
4. 建立 registry bootstrap/admission bridge，登记文档化 global/project target、host hook、project root、state/resource root 和 runtime sibling；未登记入口只能留下 machine-readable disabled/`UNREGISTERED_INSTALL` receipt，并在写入前硬拒绝。
5. 让 Go installer、`install.command`、`install.ps1` 共享 install/uninstall lock、recovery journal、临时 sibling、备份、manifest 校验、安装后 smoke 和原子 pointer/config 提交；复制、hook、managed-rule、pointer 或 smoke 失败必须可恢复旧稳定包。
6. 对 intent 前后、替换/删除前后、hook JSON 解析失败、managed-rule 写失败、pointer 换位失败、崩溃重启和 post-switch smoke 失败建立确定性 fault injection 与 recovery receipt。
7. 建立 requirements-precedence/supersession 清单，标记当前权威、正交、superseded 与历史文档；本阶段不把尚未实现的 `drive/submit` 语义写入 stable `SKILL.md`、README 或 prompts。
8. 为未来版本化 engine/candidate surface 固定 `stateSchemaVersion`、`workflowDefinitionVersion`、来源、definition digest 和 bump 规则；该 surface 缺失/不匹配时写前返回 `UNSUPPORTED_RUN_VERSION`。阶段 0 的 stable driver 与既有 legacy run 继续使用当前 state 格式和写入语义，不迁移或回写旧状态；`diagnose` 只读 raw/envelope parser 并保留 terminal summary fallback。

## 非目标

- 不实现阶段 1 的 RunPhase、TaskKey、definition compiler、Observe/Decide/SelectIssued 或 Shadow。
- 不新增第二个 workflow writer，不改变 legacy workflow 的公开入口和状态写入语义。
- 不执行最终全局 authority/cutover，不退役稳定 runtime，不宣称最终重构交付完成。
- 不以 source-tree 单测、稳定 binary 或未隔离的候选 run 冒充 installed-binary、隔离 namespace 或故障恢复证据。

## 环境约束

- 本 run 在独立 Git 分支/worktree 中开发；主分支的其他改动不属于本 run。
- 固定 stable driver 驱动本 run；阶段候选只能在独立安装、测试项目、host/config、state/resource 和 registry namespace 中作为被测对象运行。
- 候选 binary 不得驱动本 run、写稳定 registry 或签发权威 Seal；所有正式 run 的权威状态由 stable driver 写出。
- 需求、方案、开发快照、审查、QA、候选和 Seal 证据必须绑定同一 run 的 VCS identity 与 package/candidate identity。

## 一致性审查要求

- 开发前独立审查必须从零对照 `refactor-plan/incremental-seal-plan.md`、`refactor-plan/final-implementation-draft.md`、本阶段 requirements/solution 和 OpenSpec 文件，检查阶段范围、版本边界、stable/legacy 与 engine/candidate 适用面、证据和退出条件是否一致。
- 审查提示词不得注入主代理的解释、未解决的既有 finding、修复说明或预期结论；CLI 合法注入的已拍板 settled finding 及其 ID/digest 属于 `[Action input]`，不算锚定，只用于避免已处置问题原样重提；审查者仍只能依据本次登记文档形成新的发现项。
- 发现不一致时必须作为候选 finding 留痕，由用户逐项确认或驳回；不得由主代理静默替审查者对齐。

## 阶段退出条件

阶段 0 必须在一次完整 formal-gates run 内完成开发、候选实际 installed binary 验证、legacy 回归、安装 smoke、故障恢复证据、独立审查、QA、必要修复和 Seal；范围内未通过项不得以口头说明替代。
