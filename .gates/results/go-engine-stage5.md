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

## 批 1 返修（54a1965）：外部复核 FAIL（2 P1＋2 P2＋1 既有缺口＋1 过度限制）全数收口

复核方五探针留 /tmp（slice-main-oracle 等），主代理 overlay 亲跑全红坐实后派单代理六项返修；回收后亲读全部 13 文件 diff、重跑五探针（4 PASS＋#2 见裁定）、全量亲验。

- **F1/P1** child 无可执行计划：片内计划随路线决定内联确认（ADR-002 决策 1 原文出处；`DevelopmentPlanConfirmation.Proposal` 两形态不混携；载体步闩经继承结论）；开发派发断言到批次契约内容级。
- **F2/P1** 启动中断窗：完成判据统一 state.json（request.json 只是投影）；半启动残留经同一 Start 路径重建（认领同 ID 幂等）；成功路径 facade.go:724 删 intent 保证终态清理目录不被重建、重启仍被 Mkdir 拒。
- **F3/P2** 认领过 Phase A 屏障（复用 errWriteBarrier 单一闸）；**F4/P2** child 必须显式 regular（拒绝先于任何 master 写）；**F5** split Slices 逐项唯一（绑定点拒、零写）。
- **F6** 我方上轮引入的过度限制回退：§6.3.1（orchestration master-requirements:89）原文「开发前可重选或追加；开发后只可追加普通门…」——删一揽子 finality（保留 merge 拒绝）与「重走 readiness 必须同时重走路线」耦合守卫；validateRouteChange 恢复为绑定后变更唯一权威；finality 测试按 §6.3.1 语义重写＋直接通道正例。
- **探针 #2 裁定**：其"无计划路线决定也应产生开发"的期望先于设计存在，与 ADR-002「计划随路线确认」冲突；缺陷本体（合法路线后永久无任务）由携计划全程测试＋缺计划拒绝测试覆盖，探针按陈旧期望判红属预期。
- 验证：engine+coordination 全绿（主代理亲跑）；harness 满载一次 TestTransportPerMessageWaitHasNoTotalDeadline 红、隔离 -count=3 全绿＝计时 flake（无依赖路径，如实记录）；gofmt/vet 净；无过程标记。

费用：无真实付费调用；返修子代理派发 1 次。

## 批 1 补缝（683bd10）：child 计划换约纪律收敛

机主追问「是否根因级而非摞补丁」触发自查：主代理独立探针（/tmp/probe_child_plan_swap_test.go）坐实 F1 有一条缝没打穿——child 已确认计划、开发已派发后，追加普通门的路线重选携带内容不同的片内计划被静默接纳（普通 run 的逐批用户决定纪律被 child 分支提前 return 跳过）。修＝纪律收敛不分流：child 分支只留确认形态检查，prior-plan 换约比对对两种 run 形态同一条判定；连带闭合同源缝——child 计划确认的绑定 revision 此前返回空，派发绑定与 work-decision 形状校验断链，现回填确认决定时点的权威需求 revision。验证：探针 PASS（拒绝语与普通 run 同族）、protocol/facade 定向与全包绿（coordination 组合跑一次 FAIL 未捕获用例名、单独＋组合复跑两遍全绿，按满载 flike 如实记录）。教训：**新增形态的"特殊分支"每提前 return 一次，就是一条既有纪律的旁路——分支只许分流形态，不许分流纪律。**
