# 阶段 0 OpenSpec：分发安全与基线冻结

本变更是 `openspec/changes/orchestration-pipeline-engine/master-requirements.md` 的阶段化登记，不替代总需求。阶段范围和退出条件来自 `refactor-plan/incremental-seal-plan.md` 第 2、3 节；详细方案参考 `refactor-plan/final-implementation-draft.md` 第 1–11 节。

## RQ-0-01 基线 identity

系统必须从冻结提交/安装输入记录 VCS identity、source/package/installed-target digest、hook/config、managed-rule 和 canonical realpath manifest，并使 baseline receipt 可复核。

## RQ-0-02 稳定驱动冻结

系统必须提供不可变 stable driver；stable prompts、gates、binary 和配置不得 live symlink 回开发 worktree。候选与 stable、开发区的 canonical paths 必须有 disjoint proof。

## RQ-0-03 package 安全校验

package validation 必须逐输入执行 `Lstat`、realpath 和 digest 校验；发现回指开发区/stable 区、未知 target 或 digest 不符时不得安装为受支持候选。

## RQ-0-04 registry admission

所有文档化 global/project target、host hook、project root、state/resource root 和 runtime sibling 必须经 registry/admission bridge 登记；无法登记或发现 `UNREGISTERED_INSTALL` 时，workflow state 写入前硬拒绝并保留 machine-readable receipt。

## RQ-0-05 安装事务

Go installer、Shell 和 PowerShell 必须共享 install/uninstall lock、recovery journal、临时 sibling、备份、manifest 校验、安装后 smoke 和原子 pointer/config commit；失败必须可恢复旧 stable package。

## RQ-0-06 故障恢复

必须为 intent、替换/删除、hook JSON、managed-rule、pointer、崩溃重启和 post-switch smoke 边界提供确定性 fault injection 与 recovery receipt。

## RQ-0-07 版本与诊断

必须固定 `stateSchemaVersion`、`workflowDefinitionVersion`、来源、definition digest 和 bump 规则；正常写入对缺失/不匹配返回 `UNSUPPORTED_RUN_VERSION`，`diagnose` 仅 raw/envelope 只读并提供 terminal summary fallback。

## RQ-0-08 候选验证

候选验证必须执行实际 installed binary，在独立 test project、host/config、state/resource/registry namespace 中覆盖 legacy regression、portable canary、安装 smoke 和 fault matrix；不得以 source-tree 单测替代。

## RQ-0-09 文档与证据优先级

必须建立 requirements-precedence/supersession 清单，标记当前权威、正交、superseded 与 historical 文档；本阶段不得把未实现的 `drive/submit` 语义写入 stable SKILL、README 或 prompts。

## 非目标

- 不实现阶段 1 的纯决策内核、definition compiler、Observe/Decide/SelectIssued 或 Shadow。
- 不改变 legacy workflow 正常入口，不新增第二个 workflow writer，不执行最终 authority/cutover。
