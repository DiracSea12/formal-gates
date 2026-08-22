# 阶段 1 封板后补充审计（2026-08-22，用户发起）

## 背景与动机

用户质疑封板速度与验证深度，事实核实：phase-0 黑盒 32 用例/9+6 轮审查修复 vs phase-1 黑盒 19 用例/1 轮 0 修复。且旧 19 条黑盒用例多数以 `go test -run` 运行交付内已有测试为主体——不是黑盒。根因（已修复，fbe51cf）：qa-design/review/execution 三提示词无"禁止已有测试充当用例"约束（qa-execution 原文甚至明文允许）。

本轮补充审计三项：提示词修订（已单独提交）、敌意复审、黑盒重设计/审查/执行（新准则）。本文件归档后两项结果。

## 一、敌意复审（对 ba1f60d..b9eb03c 全量 diff，命令实测验证）

**发现：**

| # | 严重度 | 缺陷 | 位置 |
| --- | --- | --- | --- |
| H1 | **P1** | compiler 接受"自己 join 自己"的并行步（JoinStep==自身使 joinDeps==children 自指绕过两层防线；Compile+Encode 通过并铸造 digest sha256:2a0555…，违反 ir.go:94"join 必须是组外分立步骤"） | authoring/constructor.go:334-338、compiler/graph.go checkParallelGroups |
| H2 | P2 | 两个并行组共享同 children+同 join 被接受（调度归属歧义） | compiler/graph.go |
| H3 | P2 | Decode 二次防线缺口：durable 缺 retry 键 → decode 零值接受 → re-encode 静默补 `"retry":{"maxAttempts":0}` 改写字节与 digest；decode 接受重复 step id/ordinal、悬空依赖 | encoder/decode.go |
| H4 | P2 | TaskKey 字符串形态可碰撞：`{n,a/b}` 与 `{n/a,b}` 同为 `n/a/b`（canonical 键与 actionID 坍缩）；ID 无字符集校验 | runtime/task.go:36-41、decision/state.go:148-170 |
| H5 | P2 | definition.Generate 非原子写（直写无 temp+rename/锁），并发实测捕获撕裂读；两文件顺序写无事务 | definition/generate.go:80-88 |
| H6 | P3 | shadow.Run 接受 runID `".."`（观测路径越出 .gates/tmp；只读未破） | shadow/shadow.go:137-149 |

**未发现缺陷的项（实测确认）**：canonical 字节稳定性（三组隔离环境全等）、digest 链完整（手改制品 freshness FAIL 兜底 + CI 四平台）、隔离修复有效（三轮全量测试真实安装指纹不变；残留：真实 registry 142 条阶段 0 测试记录属环境数据未清）、compiler/decision 边界（nil/空/环/marker 并存正确）、shadow 只读承诺（错误路径零写入）、legacy v1/v2 并存无坑。

## 二、黑盒重设计（新准则，45 条）

- 用例集：`blackbox-audit-cases.md`（QA-B01~B45，全部真实产品操作：公开 API 驱动、go run/go build 真实产物、外部观察；不引用交付内测试）。
- 审查（修订准则）：**45/45 PASS、新准则 0 违例**。
- 集合级 4 项 P1 覆盖缺口（属旧交付验证面，本轮以既有证据绑定或显式移交）：
  1. installed-binary shadow 执行——旧 CASE-016 执行证据存在（/tmp/fg-qa-blackbox/ 已验证隔离安装双跑字节稳定），绑定；
  2. package digest 变半面——**确无公开面**（引擎尚无 handler 实现；PackageDigest 属安装事务，阶段 2 envelope 执行绑定/阶段 3+ 计算），按审查者意见登记为阶段需求与实现范围的已裁决延后，移交阶段 2 受理确认；
  3. stable driver smoke/legacy CLI 回归——旧 CASE-018/019 证据存在且为真实 CLI 操作，绑定；
  4. 测试套件自身隔离——旧 CASE-015 证据（真实安装前后指纹）+ 白盒回归用例绑定。

## 三、黑盒重执行（45 条真实操作）

**44 PASS / 1 FAIL：**

- **QA-B13 FAIL**：未注册 ID 的 BLOCKED_BUG 错误消息缺 "use diagnostic compile" 提示——compile.go:66-70 的拼接分支在正常模式不可达（死代码）；功能路由正确，提示语缺失。证据 /tmp/fg-audit-bb/groupB.log。

对照意义：旧 CASE-013 因只跑已有测试（断言弱于 oracle）而"通过"了同一行为——新准则下暴露。

## 处置（待用户拍板后执行）

sealed run 不可重开。候选处置：修复项（H1 P1、QA-B13、及低成本 P2）作为阶段 2 前置修复批（独立验证 + 本审计用例子集回归），全部发现登记进阶段 2 受理的继承风险清单；或全部移交阶段 2 run 首批处理。
