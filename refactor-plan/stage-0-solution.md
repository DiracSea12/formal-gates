# 阶段 0：分发安全与基线冻结——技术方案

## 方案依据

详细架构参考 `refactor-plan/final-implementation-draft.md`：第 1–10 节定义最终控制面、版本边界、VCS/副作用恢复和 canary 证据口径，第 11 节“阶段 0：冻结契约”定义先冻结契约再进入纯决策内核的顺序。

阶段 0 的可执行切片、三环境隔离和安装/registry 交付以 `refactor-plan/incremental-seal-plan.md` 第 2 节及第 3 节阶段 0 为准。本文件把这些内容收敛为本次 run 的实现方案；不提前实现阶段 1 的 engine/Shadow，也不把旧版 `重构方案/最终实施方案稿-初版.md` 当作来源。

## 总体方案

采用“固定 stable driver + 阶段候选隔离验证 + 最终一次全局切换”的增量路线。阶段 0 只建立分发安全、安装事务、登记和基线证据；候选安装与 stable driver 使用不同的 canonical paths、host/config、state/resource/registry namespace 和 runtime identity。

## 关键实现选择

1. 以原生 VCS identity、源码/二进制/package digest、installed-target digest、hook/config、managed-rule 和 realpath manifest 组成不可变 baseline receipt。
2. 用 registry admission bridge 作为文档化入口；runtime sibling 由 bridge 管理，未登记安装在 workflow state 写入前硬拒绝，并留下可机读 receipt。
3. 用跨进程 install/uninstall lock 与持久 recovery journal 保护 `prepare → copy/verify → smoke → pointer/config commit`；失败或崩溃通过 journal observe/reconcile 恢复旧稳定包。
4. Go installer、Shell 和 PowerShell 只共享同一 native transaction owner；脚本不得先删除 release、切换 pointer 或绕过 journal。
5. package validation 对每个安装输入执行 `Lstat`、realpath disjoint proof 和 digest 校验；候选安装不得读取开发 worktree 或 stable 区的可变内容。
6. 未来版本化 engine/candidate surface 的 schema/definition version 采用精确匹配；该 surface 在缺失或不匹配时返回 `UNSUPPORTED_RUN_VERSION`。阶段 0 的 stable driver 与既有 legacy run 继续按当前 state 格式和写入语义运行，不迁移、不回写旧状态。`diagnose` 是唯一 raw read 例外，不修复、不迁移、不清理。
7. 所有阶段 0 故障窗口使用可重复 fixture/fault injection，证据绑定 candidate identity，不把 source-tree 单测当作 installed-binary 证明。

## 事务与证据顺序

```text
admission/lock
  -> recovery journal intent
  -> sibling temp + old pointer/config backup
  -> copy runtime/prompts/gates/hooks/rules
  -> manifest, realpath and digest verification
  -> installed-binary package validation and post-switch smoke
  -> atomic current/pointer/config commit
  -> journal committed receipt
```

任一中间步骤失败时，先观察外部事实，再按 journal 对账；只有确认旧 pointer/config/release 与旧 binary smoke 可用，才提交 recovery receipt。候选验证必须从实际 installed path 启动 binary，并记录 source identity、package/installed digest、host/config/state/resource canonical paths、legacy regression、portable canary、安装 smoke 和 fault matrix。

## 验证安排

- stable driver 先启动本阶段正式 run；候选在独立 test project 中验证新增分发能力，候选 run 只写候选 namespace。
- 开发前进行产品审与 start-readiness/技术审；两者都必须独立比对登记的计划文件、详细方案文件、阶段 OpenSpec 与本阶段需求，且不使用既有 finding 或主代理解释形成锚定；开发后独立 QA 和选定 gate 审查完整 base→current diff。
- Seal 前复核 VCS identity、package/installed digest、registry/bootstrap receipt、fault-injection receipt、候选 path disjoint proof、QA 结果和无残留证明。

## 约束

阶段 1 的 engine/Shadow 只在后续独立阶段实现；本阶段不添加第二个 workflow writer，不让候选 binary 驱动本 run，不改变主分支或 stable 入口。
