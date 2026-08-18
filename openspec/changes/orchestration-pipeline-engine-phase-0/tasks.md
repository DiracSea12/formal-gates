# Tasks

## 基线与分发

- [ ] 记录初始 VCS/package/installed identity、digest、hook/config、managed-rule 和 realpath manifest。
- [ ] 冻结 stable driver，证明 stable/candidate/worktree canonical paths disjoint。
- [ ] 实现并验证 package `Lstat`/realpath/digest negative cases。
- [ ] 建立 registry bootstrap/admission bridge、launcher 和 unregistered rejection receipt。

## 安装与恢复

- [ ] 统一 Go installer、Shell、PowerShell 的 lock/journal/temporary sibling/backup/manifest/smoke/atomic pointer owner。
- [ ] 覆盖 intent、替换/删除、hook JSON、managed-rule、pointer、crash restart 和 post-switch smoke fault points。
- [ ] 记录每个故障点的 observed fact、reconcile action、recovered stable identity 和 receipt digest。

## 版本与证据

- [ ] 固定 schema/definition version、来源、definition digest 和 bump rules。
- [ ] 验证缺失/不匹配版本在写前返回 `UNSUPPORTED_RUN_VERSION`，diagnose 只读且 terminal summary 可回落。
- [ ] 从实际 installed candidate binary 执行 legacy regression、portable canary、安装 smoke 和 fault matrix。
- [ ] 建立 precedence/supersession 清单，确认 stable 文档不提前声明阶段 1 engine/Shadow 语义。

## 退出

- [ ] 完成独立产品/技术审、QA、选定 gates、必要修复和 Seal。
- [ ] Seal 证据绑定本阶段 VCS identity、candidate/package/installed digest、registry/recovery receipt 和无残留证明。
