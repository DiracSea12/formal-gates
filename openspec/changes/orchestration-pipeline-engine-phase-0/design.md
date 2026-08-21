# Design

## 1. 三环境隔离

阶段使用固定 stable driver、阶段开发 worktree 和候选验证环境。stable driver 从冻结的已安装 artifact 读取 prompts/gates/runtime；开发 worktree 只承载本阶段授权改动；候选验证环境从已提交 candidate package 复制安装，拥有独立 host home/config、state/resource、registry 和测试 project。启动前对每个 canonical path 执行 `Lstat`/realpath 和 disjoint proof。

## 2. Registry 与 admission bridge

registry 是跨 global/project scope 共用的用户级登记根，不使用某个项目的 `.gates/tmp` 代替。每条 record 固定 target、scope、host、project root、state/resource root、runtime sibling 和 canonical paths。首次 bootstrap 由冻结 stable driver 从已安装 artifact 调用受支持的 `install --bootstrap` 维护入口：它只创建/登记 registry record、epoch/generation、lease/token 和 bootstrap receipt，不创建 workflow state；bootstrap receipt 提交成功后，stable launcher 才允许第一次 `workflow start`。registry 已存在但 record 缺失、冲突或无法对账时只留下 disabled/`UNREGISTERED_INSTALL` receipt 并停止，不能先创建 `.gates/tmp`。之后 bridge 在任何 workflow state 写入前取得 admission lock、校验 epoch/lease、record 与实际 root/target binding；无法登记或发现 `UNREGISTERED_INSTALL` 时硬拒绝。旧 target 要么迁移到 bridge 管理的 launcher，要么保留 disabled receipt；候选 binary 不得执行 bootstrap、驱动本 run 或写 stable registry。

## 3. 安装事务

Go native installer、registry bridge、Shell 和 PowerShell 共享同一 transaction owner、锁顺序和 journal/recovery receipt schema。owner 先取得 install/uninstall lock，再取得 registry lock；bootstrap、install、uninstall 和 registry admission 的 staged record 都由这一个 owner 对账。事务顺序固定为：

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

这里的 `switched` 只表示候选 release/installed target 已切换到临时可见状态，不表示公共 pointer/config 或 registry 已提交。post-switch smoke 必须从实际 installed path 启动且发生在共同 commit 之前；smoke 失败时 journal 保持 `switched`，owner 先恢复旧 runtime、pointer/config 和 registry bytes，再写 failure/recovery receipt。只有 smoke 和所有 manifest/identity 校验通过，才由同一 owner 原子提交 current/pointer/config 与 registry record。Shell/PowerShell 入口只解析参数并调用 native owner，不得自行删除 release、切换 pointer、写 registry 或绕过 journal。任何中间失败在 observe/reconcile 后恢复旧稳定 identity；旧 stable binary 的正常 smoke 是回退证明。

## 4. Version envelope 与 diagnose

未来版本化 engine/candidate surface 固定 `stateSchemaVersion`、`workflowDefinitionVersion`、definition digest 和 bump 规则。该 surface 的正常 loader 要求精确匹配；缺失/不匹配只返回 `UNSUPPORTED_RUN_VERSION`，不迁移、不写回。阶段 0 的 stable driver 与既有 legacy run 继续按当前 state 格式和写入语义运行。diagnose 只读取原始 bytes，报告 path、JSON readable、发现的版本、当前支持版本、summary、可安全判断的完整性和重建建议。

## 4.1 计划/方案一致性审查

产品审与 start-readiness 必须在零上下文条件下逐份比对增量计划、详细实施方案、阶段需求/方案和 OpenSpec，至少核对范围、版本适用面、稳定/legacy 与候选/engine 边界、证据和退出条件。提示词不得注入主代理解释、未解决的既有 finding、修复说明或预期结果；CLI 合法注入的已拍板 settled finding 及其 ID/digest 属于 `[Action input]`，不算锚定，仅用于防止已处置问题原样重提；冲突作为候选 finding 交由用户处置。

## 5. 故障注入与证据

每个非幂等边界暴露 deterministic fault point；测试记录 intent、observed external fact、reconcile action、recovered identity 和 receipt digest。候选验证从 installed path 启动实际 binary，记录 source identity、package/installed digest、host/config/state/resource/registry canonical paths、legacy regression、portable canary、安装 smoke 和 fault matrix。候选 run 的状态只在候选 namespace 保存，不能被 stable driver 读取为权威证据。

## 6. 受支持范围

阶段设计覆盖增量计划列出的文档化 global/project targets、host hooks、state/resource roots 和可用宿主；当前设备不可执行的宿主/平台证据记录为 P3 或 runtime limitation，不能用未执行的 smoke 冒充 PASS。
