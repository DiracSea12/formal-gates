# Design

## 1. 三环境隔离

阶段使用固定 stable driver、阶段开发 worktree 和候选验证环境。stable driver 从冻结的已安装 artifact 读取 prompts/gates/runtime；开发 worktree 只承载本阶段授权改动；候选验证环境从已提交 candidate package 复制安装，拥有独立 host home/config、state/resource、registry 和测试 project。启动前对每个 canonical path 执行 `Lstat`/realpath 和 disjoint proof。

## 2. Registry 与 admission bridge

registry 是跨 global/project scope 共用的用户级登记根，不使用某个项目的 `.gates/tmp` 代替。每条 record 固定 target、scope、host、project root、state/resource root、runtime sibling 和 canonical paths。bridge 在任何 workflow state 写入前取得 admission lock、校验 epoch/lease 和 record；无法登记或发现 `UNREGISTERED_INSTALL` 时硬拒绝。旧 target 要么迁移到 bridge 管理的 launcher，要么保留 disabled receipt。

## 3. 安装事务

Go native installer 是 transaction owner。事务顺序固定为：

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

Shell/PowerShell 入口只解析参数并调用 native owner，不得自行删除 release、切换 pointer 或绕过 journal。任何中间失败在 observe/reconcile 后恢复旧 pointer/config/release；旧 stable binary 的正常 smoke 是回退证明。

## 4. Version envelope 与 diagnose

阶段固定 `stateSchemaVersion`、`workflowDefinitionVersion`、definition digest 和 bump 规则。正常 loader 要求精确匹配；缺失/不匹配只返回 `UNSUPPORTED_RUN_VERSION`，不迁移、不写回。diagnose 只读取原始 bytes，报告 path、JSON readable、发现的版本、当前支持版本、summary、可安全判断的完整性和重建建议。

## 5. 故障注入与证据

每个非幂等边界暴露 deterministic fault point；测试记录 intent、observed external fact、reconcile action、recovered identity 和 receipt digest。候选验证从 installed path 启动实际 binary，记录 source identity、package/installed digest、host/config/state/resource/registry canonical paths、legacy regression、portable canary、安装 smoke 和 fault matrix。候选 run 的状态只在候选 namespace 保存，不能被 stable driver 读取为权威证据。

## 6. 受支持范围

阶段设计覆盖增量计划列出的文档化 global/project targets、host hooks、state/resource roots 和可用宿主；当前设备不可执行的宿主/平台证据记录为 P3 或 runtime limitation，不能用未执行的 smoke 冒充 PASS。
