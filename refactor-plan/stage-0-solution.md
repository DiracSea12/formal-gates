# 阶段 0：分发安全与基线冻结——技术方案

## 方案依据

详细架构参考 `refactor-plan/final-implementation-draft.md`：第 1–10 节定义最终控制面、版本边界、VCS/副作用恢复和 canary 证据口径，第 11 节“阶段 0：冻结契约”定义先冻结契约再进入纯决策内核的顺序。

阶段 0 的可执行切片、三环境隔离和安装/registry 交付以 `refactor-plan/incremental-seal-plan.md` 第 2 节及第 3 节阶段 0 为准。本文件把这些内容收敛为本次 run 的实现方案；不提前实现阶段 1 的 engine/Shadow，也不把旧版 `重构方案/最终实施方案稿-初版.md` 当作来源。

## 总体方案

采用“固定 stable driver + 阶段候选隔离验证 + 最终一次全局切换”的增量路线。阶段 0 只建立分发安全、安装事务、登记和基线证据；候选安装与 stable driver 使用不同的 canonical paths、host/config、state/resource/registry namespace 和 runtime identity。

## 关键实现选择

1. 以原生 VCS identity、源码/二进制/package digest、installed-target digest、hook/config、managed-rule 和 realpath manifest 组成不可变 baseline receipt。
2. 用 registry admission bridge 作为文档化入口；runtime sibling 由 bridge 管理，未登记安装在 workflow state 写入前硬拒绝，并留下可机读 receipt。首次 bootstrap 也走同一 native owner：固定 stable driver 从冻结已安装 artifact 调用受支持的 `install --bootstrap` 维护入口，先登记 target/host/root/state/resource/runtime record，完成 bootstrap receipt 后才允许第一次 `workflow start` 写 state；bootstrap 本身不创建 workflow state。
3. 用跨进程 install/uninstall lock、registry lock 与持久 recovery journal 保护同一笔 `prepare → copy/verify → switched → installed-path smoke → atomic runtime+registry commit` 事务。`switched` 只表示候选 release/installed target 已切到临时可见状态，current/pointer/config 和 registry record 仍保持旧版本；实际 installed binary 的 smoke 在此状态执行，失败时 journal 保持 `switched` 并回滚，成功后才共同提交 runtime 与 registry。
4. Go installer、Shell 和 PowerShell 以及 admission bridge 共享同一 native transaction owner、锁顺序、journal schema、generation/token 和 recovery receipt；脚本不得先删除 release、切换 pointer、写 registry 或绕过 journal。任一 runtime、pointer/config 或 registry commit 失败，都由同一 owner observe/reconcile，恢复旧 runtime、旧 pointer/config 和旧 registry bytes。
5. package validation 对每个安装输入执行 `Lstat`、realpath disjoint proof 和 digest 校验；候选安装不得读取开发 worktree 或 stable 区的可变内容。
6. 未来版本化 engine/candidate surface 的 schema/definition version 采用精确匹配；该 surface 在缺失或不匹配时返回 `UNSUPPORTED_RUN_VERSION`。阶段 0 的 stable driver 与既有 legacy run 继续按当前 state 格式和写入语义运行，不迁移、不回写旧状态。`diagnose` 是唯一 raw read 例外，不修复、不迁移、不清理。
7. 所有阶段 0 故障窗口使用可重复 fixture/fault injection，证据绑定 candidate identity，不把 source-tree 单测当作 installed-binary 证明。

## 首次 bootstrap 与 admission 顺序

正式 run 的首个写入也必须可执行，不能依赖一个尚未登记的 bridge：

1. 冻结 stable driver 从已安装 artifact 调用 `install --bootstrap`；该维护入口只负责发现并登记文档化的 global/project target、host hook、project root、state/resource root 和 runtime sibling，不创建 workflow state。
2. bootstrap owner 取得 install/uninstall lock，再按固定顺序取得 registry lock，写入 `bootstrap-intent` journal，逐项生成 record、epoch/generation、lease/token 和 canonical-path receipt；所有已知 target 登记成功后，原子提交 registry 与 bootstrap receipt。
3. 若 registry 不存在，允许此一次性 bootstrap 创建它；若已有 record 缺失、冲突或不可对账，留下 disabled/`UNREGISTERED_INSTALL` receipt 并停止，不得先写 `.gates/tmp`。bootstrap 成功后，stable launcher 对每次 workflow 写入重新 admission；候选 binary 不能执行 bootstrap、驱动本 run 或写 stable registry。

## 事务与证据顺序

```text
admission/install lock + registry lock
  -> recovery journal intent
  -> sibling temp + old runtime/pointer/config/registry backup
  -> copy runtime/prompts/gates/hooks/rules
  -> manifest, realpath and digest verification
  -> switch release/installed target (journal: switched; old registry remains authoritative)
  -> installed-binary package validation and post-switch/pre-commit smoke
  -> atomic current/pointer/config + registry record commit
  -> journal committed receipt
```

这里的“post-switch”特指 release/installed target 已切换、但公共 current/pointer/config 与 registry 尚未提交；它不是“提交之后再做 smoke”。任一中间步骤失败时，先观察外部事实，再按同一 journal 对账；runtime、pointer/config 或 registry 任一提交失败都回滚到旧稳定 identity，只有恢复核验完成才提交 recovery receipt。候选验证必须从实际 installed path 启动 binary，并记录 source identity、package/installed digest、host/config/state/resource canonical paths、legacy regression、portable canary、安装 smoke 和 fault matrix。

## 验证安排

- stable driver 先启动本阶段正式 run；候选在独立 test project 中验证新增分发能力，候选 run 只写候选 namespace。
- 开发前进行产品审与 start-readiness/技术审；两者都必须独立比对登记的计划文件、详细方案文件、阶段 OpenSpec 与本阶段需求，且不得使用主代理解释、未解决的既有 finding、修复说明或预期结论形成锚定。CLI 合法注入的已拍板 settled finding 及其 ID/digest 仅用于避免已处置问题原样重提，不作为本轮结论依据；开发后独立 QA 和选定 gate 审查完整 base→current diff。
- Seal 前复核 VCS identity、package/installed digest、registry/bootstrap receipt、fault-injection receipt、候选 path disjoint proof、QA 结果和无残留证明。

## 约束

阶段 1 的 engine/Shadow 只在后续独立阶段实现；本阶段不添加第二个 workflow writer，不让候选 binary 驱动本 run，不改变主分支或 stable 入口。
