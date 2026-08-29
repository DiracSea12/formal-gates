# 阶段 3：最小纵向 engine 闭环——技术方案

本文件承接 `refactor-plan/final-implementation-draft.md` 的总体方案，只冻结阶段 3 所需的最小
实现面，不提前实现阶段 4–7 的完整迁移。

> 本文的“阶段 3”按 `incremental-seal-plan.md` 编号，指最小纵向闭环；最终方案 §11 的“阶段 3：
> 完整流程迁移”不属于本批次。

## 方案骨架

1. 以阶段 2 已封板的 `persistence.Store`、typed protocol、recovery 和 `NextResult` 为唯一
   engine 写入基座。
2. 在 engine façade 层增加 run envelope 解析与 runtime 选择：`start` 从当前 installed target 的
   admission/launcher registry 解析 target identity/package digest（不接受用户手工 runtime、identity
   或 digest 参数）并选择 owning runtime，候选 target 创建 engine envelope，stable driver target
   继续创建 legacy state；后续版本 envelope 明确选择 engine 或 legacy，选择后整条 run 固定，不
   允许中途跨 runtime。缺失或冲突的 admission 在写前以 `UNREGISTERED_INSTALL` 拒绝。envelope 必须同时绑定
   `definitionSource`、`definitionDigest`、owning runtime 的 `packageDigest` 和
   `installedTargetIdentity`，loader 写前精确校验四者与当前实际 runtime。
3. `start` 只接收宿主在受理阶段已经确认的 typed `intakeConfirmationReceipt` 与其绑定的
   `StartRequest`；回执包含确认来源、requirement source/revision、solution revision/digest 和
   完整登记工件集。有效性只按当前确认 binding 的 source、revision、digest 和 artifact 集精确
   校验，不采用 wall-clock expiry；按 `(path, revision)` 的路径序 canonical 编码生成
   `intakeDigest` 并写入 run envelope。首次 `drive` 只执行确定性
   `Observe → Decide → SelectIssued`，自动把同一绑定登记为 intake receipt，不再发 Ask 或等待
   确认事件；`submit` 仍是 start 后唯一外部事件写入口，接纳事件后立即继续决策；
   `show/status/next` 只读投影当前状态或 terminal summary。
   回执的权威来源是阶段 0 冻结 stable driver 已写入的确认状态；固定 launcher 从该状态和登记集
   工件派生并落到 formal run evidence 的 `intakeConfirmationReceipt.json`，再把回执和工件按原相对
   路径注入候选 test project。候选 `start` 只通过 `--intake-receipt <path>`（或固定 host config
   指针）读取，不改动 stable driver 的 writer 或公共入口。
   候选以注入工件重算当前 binding 与 `intakeDigest`，因此“旧回执 + 当前工件”在首写前稳定返回
   `INVALID_INTAKE_CONFIRMATION`；候选不生成回执、不读 stable run state，也不驱动正式 run。
4. lightweight 首条路径在首次 drive 登记 intake receipt 后进入终结阶段，持久化 cleanup
   intent/summary，由 engine handler 清理登记资源、核对无残留，再提交 terminal summary（标记
   `unverified=true`），最后 `next` 返回 `Complete`。candidate façade 对所有未迁移旧 workflow
   写入口（包括 `requirement`、`cleanup`、`resume`、`abort`、`reset`、`route`、`slicing` 等）
   明确返回 `UNSUPPORTED_ENGINE_ENTRY` 且不写状态；stable legacy 回归单独验证旧命令仍按 legacy
   语义工作。candidate `start` 不接受 `--split`、`--retained-overall` 或 `--master`。
5. Ready/HostAction 只签发薄指针和 action ID，宿主回 typed receipt/result；Ask/Operator 只接收
   合法 typed 决定或 observation；Wait/Complete 不产生隐藏动作。
6. 所有 engine 状态只写隔离 candidate namespace。固定 stable driver 驱动 formal-gates run；候选
   以实际 installed binary 启动独立 test project，stable driver 和 legacy run 仅做回归对照，候选
   不驱动自己的正式 run/Seal。

## 验证策略

- 先用 whitebox/test-only smoke/sequence harness 验证六类 `NextResult` 和 start→Complete 主链；
  installed-candidate 黑盒只验证 lightweight 可达的 `Complete` 与 façade/legacy/negative 面，
  regular 才能建立的五类公开路径留到阶段 4。再补确定性故障、确认回执缺失或 binding 不一致、
  definitionSource/definitionDigest/packageDigest/installed-target mismatch、终结 replay、旧入口/
  公开 cleanup negative cases，以及 `unverified=true` 终态投影。
- 每个 Batch 在边界执行 `go build ./...`、`go vet ./...`、`go test ./...` 及阶段声明的
  installed-candidate harness；同时执行 package validation、portable canary、stable-driver smoke
  和 source/installed/package digest、host-path namespace disjoint、registry/fencing、cleanup
  receipt 证据；Subtask 使用同一开发代理上下文连续完成。
- 任何改变 Batch 契约、接口、依赖或 DoD 的实现发现都暂停当前 Batch，回到技术审重新输出
  `granularity_review`；不因实现编号或提交数量自动新增 Batch。

### 本阶段已登记的 `granularity_review`

阶段 3 采用单一 Batch `phase-3-engine-vertical-loop`，其 Subtask 顺序为：S1
façade/admission/runtime 选择；S2 `StartRequest`、intake receipt、canonical digest、
envelope；S3 `drive`/`submit` 与六类 `NextResult`；S4 terminal cleanup、`Complete`、
replay；S5 installed 隔离、legacy 回归与 negative fixtures。S1–S5 共享 façade、
`state.json`、`Store/CAS`、typed protocol、envelope 和 cleanup 边界，不能独立验收或回滚，
因此不拆成多个 Batch。一个 Batch 只派发一个全新的零上下文开发代理，Subtask 间连续执行、
不换代理；本 Batch 采用 full 档位并执行已选四个 gates 及上段列出的批边界检查。

## 后续阶段隔离

regular/full/custom、Git child/merge、SVN/P4、宿主完整 canary、最终 façade cutover 和 stable
退役均留给增量计划的后续阶段，本阶段不得以占位实现宣称完成。
