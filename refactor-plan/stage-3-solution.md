# 阶段 3：最小纵向 engine 闭环——技术方案

本文件承接 `refactor-plan/final-implementation-draft.md` 的总体方案，只冻结阶段 3 所需的最小
实现面，不提前实现阶段 4–7 的完整迁移。

## 方案骨架

1. 以阶段 2 已封板的 `persistence.Store`、typed protocol、recovery 和 `NextResult` 为唯一
   engine 写入基座。
2. 在 engine façade 层增加 run envelope 解析与 runtime 选择：版本 envelope 明确选择 engine
   或 legacy；选择后整条 run 固定，不允许中途跨 runtime。
3. `drive` 只执行确定性 `Observe → Decide → SelectIssued`；`submit` 接纳唯一外部事件，
   持久化接纳结果后立即继续决策；`show/status/next` 只读投影当前状态或 terminal summary。
4. lightweight 首条路径在终结阶段持久化 cleanup intent/summary，由 engine handler 清理登记
   资源、核对无残留，再提交 terminal summary，最后 `next` 返回 `Complete`。
5. Ready/HostAction 只签发薄指针和 action ID，宿主回 typed receipt/result；Ask/Operator 只接收
   合法 typed 决定或 observation；Wait/Complete 不产生隐藏动作。
6. 所有 engine 状态只写隔离 candidate namespace。候选以实际 installed binary 启动独立 test
   project，stable driver 和 legacy run 仅做回归对照。

## 验证策略

- 先用最小 smoke/sequence harness 验证六类 `NextResult` 和 start→Complete 主链，再补确定性
  故障、版本 mismatch、终结 replay、旧入口/公开 cleanup negative cases。
- 每个 Batch 在边界执行 `go build ./...`、`go vet ./...`、`go test ./...` 及阶段声明的
  installed-candidate harness；Subtask 使用同一开发代理上下文连续完成。
- 任何改变 Batch 契约、接口、依赖或 DoD 的实现发现都暂停当前 Batch，回到技术审重新输出
  `granularity_review`；不因实现编号或提交数量自动新增 Batch。

## 后续阶段隔离

regular/full/custom、Git child/merge、SVN/P4、宿主完整 canary、最终 façade cutover 和 stable
退役均留给增量计划的后续阶段，本阶段不得以占位实现宣称完成。
