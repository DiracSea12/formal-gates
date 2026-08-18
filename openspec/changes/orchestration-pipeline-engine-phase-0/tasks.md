# Tasks

## 基线与分发

- [ ] 记录初始 VCS/package/installed identity、digest、hook/config、managed-rule 和 realpath manifest。
- [ ] 冻结 stable driver，证明 stable/candidate/worktree canonical paths disjoint。
- [ ] 实现并验证 package `Lstat`/realpath/digest negative cases。
- [ ] 由冻结 stable driver 的 `install --bootstrap` 维护入口完成首次 registry bootstrap；登记 target/host/root/state/resource/runtime、epoch/generation、lease/token 和 bootstrap receipt，且 bootstrap 不创建 workflow state。
- [ ] 建立 registry admission bridge、launcher 和 unregistered rejection receipt；bootstrap 后每次 workflow state 写入前重新 admission，候选和裸旧 binary 不得绕过 bridge。

## 安装与恢复

- [ ] 统一 Go installer、registry bridge、Shell、PowerShell 的 lock/journal/temporary sibling/backup/manifest/smoke/atomic runtime+registry owner；固定 `switched -> installed-path post-switch/pre-commit smoke -> pointer/config+registry commit` 顺序。
- [ ] 覆盖 intent、替换/删除、hook JSON、managed-rule、pointer/registry commit、crash restart 和 post-switch/pre-commit smoke fault points。
- [ ] 记录每个故障点的 observed fact、reconcile action、recovered stable identity 和 receipt digest。

## 版本与证据

- [ ] 固定 schema/definition version、来源、definition digest 和 bump rules。
- [ ] 为未来版本化 engine/candidate surface 验证缺失/不匹配版本在写前返回 `UNSUPPORTED_RUN_VERSION`；确认 stable driver 与既有 legacy run 继续按当前格式写入；diagnose 只读且 terminal summary 可回落。
- [ ] 从实际 installed candidate binary 执行 legacy regression、portable canary、安装 smoke 和 fault matrix。
- [ ] 建立 precedence/supersession 清单，确认 stable 文档不提前声明阶段 1 engine/Shadow 语义。
- [ ] 由无上下文独立审查者逐份比对增量计划、详细实施方案、阶段 requirements/solution 和 OpenSpec；记录一致性结论与所有候选冲突，不复述主代理预期。

## 退出

- [ ] 完成独立产品/技术审、QA、选定 gates、必要修复和 Seal。
- [ ] Seal 证据绑定本阶段 VCS identity、candidate/package/installed digest、registry/recovery receipt 和无残留证明。
