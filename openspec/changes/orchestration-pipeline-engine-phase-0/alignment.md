# Requirements Alignment

本文件把阶段需求映射回增量计划、详细实施方案和总需求；不扩大总需求。

| 阶段需求 | 增量计划依据 | 详细方案依据 | 最小证据 |
| --- | --- | --- | --- |
| RQ-0-01 基线 identity | `incremental-seal-plan.md` §3 阶段 0 | `final-implementation-draft.md` §11 阶段 0 | baseline/inventory receipt、VCS identity、digest manifest |
| RQ-0-02 稳定驱动冻结 | `incremental-seal-plan.md` §2.1、§3 阶段 0 | §1、§10 | stable/candidate/worktree realpath disjoint proof |
| RQ-0-03 package 安全校验 | `incremental-seal-plan.md` §2.1、§3 阶段 0 | §9 | Lstat/realpath/digest negative tests |
| RQ-0-04 admission registry | `incremental-seal-plan.md` §2.4、§3 阶段 0 | §2、§9 | bootstrap/register receipt、unregistered rejection |
| RQ-0-05 安装事务 | `incremental-seal-plan.md` §3 阶段 0 | §9 | lock/journal/atomic pointer smoke |
| RQ-0-06 故障恢复 | `incremental-seal-plan.md` §3 阶段 0 | §9、§12.2 | deterministic fault matrix and recovery receipts |
| RQ-0-07 版本/diagnose | `incremental-seal-plan.md` §2.3、§3 阶段 0 | §10、§12 | unsupported-version fixtures、raw diagnose read-only proof |
| RQ-0-08 候选验证 | `incremental-seal-plan.md` §2.1、§2.2 | §12.3 | actual installed binary and isolated namespace evidence |
| RQ-0-09 文档/证据 | `incremental-seal-plan.md` §3 阶段 0、§7 | §11 | precedence/supersession list、package/candidate receipt format |

## 范围边界

总需求中关于 engine 决策、持久协议、完整流程迁移、split/VCS 全矩阵和最终切换的条目保留为后续增量阶段的权威输入；本阶段只冻结它们所依赖的分发与基线前提。
