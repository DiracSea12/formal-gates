# go-engine 阶段 5（Git 分片、child 与 merge 闭环）执行账

前置：阶段 4 已于 0edf3b1 收口（三轮机主复核通过）。本阶段依据
refactor-plan/incremental-seal-plan.md:373-392 与机主 2026-10-03 三轮更正
裁定（分片流程普通 QA 不选不执行；门每片各跑；路线机制照旧；记录见
memory/slice-verification-once-at-end.md）。

## 批 1：分片身份层（45b97f1）

范围：CONFIRM_TOPOLOGY split 分支补全（AC 责任映射校验＋同事务锁定
merge 路线）、child 创建（facade.Start 携 MasterRunID/SliceID，master 账
sliceID→childRunID 认领）、继承（child 不自增审查轮，相位门/账本 floor
等价放行，不伪造账本）、child 路线候选无 QA 项、merge master 不进开发段。

派发：单实施子代理（三节 7 步简洁法逐字入合同）＋两轮小修，均 fork 自
同一代理会话。无真实付费模型调用。

### 主代理独立复核

- 亲读全部 33 文件 diff（含测试）。
- 独立探针（/tmp overlay，非树内）：TestProbeChildBoundaryAfterRouteSelection
  复现 child 选完路线后边界提示停留"选路线"的缺陷 → 返修一后 PASS。
- 复核中发现第二缺陷（阶段 4 残留，非本批引入）：SELECT_ROUTE 在路线已
  绑定时仍被接纳，同 run 二次选择静默替换已生效路线，违反需求 7「路线
  只问一次」→ 返修二收口（Ask/决定双守卫、零写、Ask 存活；合法重选唯一
  通道＝需求变化勾选重走）。
- 返修二连带两处超合同明示的引擎改动（已亲验成立）：
  1. requirement.go 停等闭环守卫——pipeline 形态下绑定后只重走
     start-readiness 不重走路线的需求变化包被拒（开发计划确认搭载路线
     决定，finality 后无独立确认通道）；
  2. RedoRoute 保留 SelectedGates 作 add-only 基线（原清空会使同集重选
     被误判为开发后新增内建 QA 而拒绝）。
- 被重构既有测试 36 处逐条清单见子代理报告；抽读语义重构三例
  （CandidateMaterialization/ExplicitDisposition/DevelopedRouteRejects），
  断言未削弱，仅改走合法重走通道。

### 验证

- gofmt 空；go vet ./internal/engine/... ./internal/coordination/ 通过。
- go test ./internal/engine/... ./internal/coordination/ ./internal/harness/
  -count=1 全绿（facade 403s、protocol 83s、harness 389s 等，主代理亲跑）。
- 独立探针终态 PASS。

### 已知中间态（后续批次收口）

- merge 门/合并 QA 已绑定未执行（批 3）；SLICE_READY/回执/费用汇总（批
  2）；master 需求变化与 children 级联一致性（批 4——含认领后 master 修订
  需求导致同 id 重入被 revision 检查拒绝的占位滞留边）；分片 canary（批
  5，模型调用逐次授权）。
- split master 须在 CONFIRM_TOPOLOGY 前提交 REQUIREMENT 观察（AC 宇宙以
  冻结投影为权威）——canary 驱动注意此顺序。
- validateRouteChange 的直接二次选择防护已被 finality 守卫吸并，仅在重走
  重选路径上以基线形式生效——保持现状，不在返修轮里过度收敛。

费用：本批无真实付费调用；子代理派发 3 次（1 实施＋2 返修）。
