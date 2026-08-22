# 阶段 1 开发批次日志（ADR-002 自测留痕）

基线 ba1f60d → 开发完成。每批边界 `go build ./... && go vet ./...` 干净、`go test -count=1 ./...` 全仓全绿、独立原子 commit。

| 批次 | Commit | 交付 | 自测要点 |
| --- | --- | --- | --- |
| 0A 测试隔离修复 | 54a449d | 3 处测试 HOME 隔离 + install_isolation_regression_test.go（2 回归用例）；生产代码零改动 | 真实安装 sha256 前后一致；事故形态桩污染在隔离 HOME 复现并证明不再外泄 |
| 0B compiler spike | 54a449d | 结论入档 refactor-plan/spike-notes-compiler.md（spike 代码 /tmp/fg-spike 不进 production） | 30 种 assembly 序/20 种注册序/round-trip/跨进程 digest 一致；ordinal 必须 compiler 派生；mutation 100% 拒绝 |
| 1a authoring | 488b62a | internal/engine/authoring/（5 文件 1116 行）：六变体+constructor+双维派生 | constructor 拒绝矩阵全覆盖；authority/runner 仅派生不可手填 |
| 1b compiler | 179a7a6 | internal/engine/compiler/（9 文件 2133 行）：registry/图校验/归一化/CompiledDefinition IR | 图校验 19 分支；registry 三类拒绝；ordinal 确定性；八类二次防线；marker 双路由（diagnostic-only / BLOCKED_BUG） |
| 1c encoder+制品 | 0de5542 | internal/engine/encoder|definition/ + cmd/gen-definition + definitions/workflow.json v2（263 行）+ identity_gen.go 同源常量 | freshness 零 diff；assembly/注册序不变；round-trip；LoadFutureDefinition 兼容实证（v2 被接受为新 future 候选） |
| 2a runtime 模型 | 5b31e03 | internal/engine/runtime/（6 文件 625 行）：RunPhase 27 边表/TaskKey/任务状态机/Batch 派生 | phase 边全表 golden + 14×14 穷举；batch 完成纯派生（无状态机） |
| 2b 决策核心 | 6f0f399 | internal/engine/decision/（8 文件 1893 行）：Observe/Decide/SelectIssued/NextResult 六类 Kind/PlanDigest | frontier 完整固定序；min(C,N)+自动补位；Plan 字节稳定；乱序/遗漏/重复拒绝；非终态无空结果 |
| 3 验收套件 | 8d7c0eb | internal/engine/acceptance/（7 文件 1149 行）+ gofmt 修复 4 文件 | digest 分离/敏感性、registry 完备、mutation 枚举+200 轮 fuzz、跨进程双进程一致、合法/非法完成序列、marker 扫描入口 |
| 4 shadow | beba742 | internal/engine/shadow/（5 文件 995 行）：只读预测+差异三分类+fixture 比对 | 只读性（sha256/mtime 不变）；telemetry 仅指定目录；确定性；fixture 期望 frontier 比对 |

批 0 说明（流程偏离留痕）：0A/0B 由主代理手写任务书派发、主代理代提交，未按 development-worker 规范文件的薄启动规则执行；1a 起已纠正为薄启动 + 规范文件。内容层面经主代理独立复核（build/vet/全量测试 + 真实安装指纹）无缺陷，开发后全量 diff 审查覆盖全部提交。

QA 线（与开发并行）：黑盒 19 用例登记（覆盖需求验收 1–9）→ qa-review 19/19 PASS → 3 条 P2 建议按 apply=resolved 吸收（CASE-010 补分支封闭显式断言、CASE-016 补 fixture 比对、CASE-003/004/006-008/010/012/013 钉定具体执行命令）。

## 开发后审查留痕（P3 建议，不构成修复义务）

- 黑盒执行 19/19 PASS（dispatch-998303fc）；差异报告证据 /tmp/fg-qa-blackbox/。
- 白盒 qa-review 41/41 PASS，3 条 P3：①Operator Kind/Classify 边界臂未入白盒集合（本批不可达，仅开发测试覆盖）——建议后续批次补手工构造 union 校验；②用例文字与测试行数三处轻微失配（覆盖为声明超集或同分支他行承载，无风险）。
- complexity-gate PASS，2 条 P3：compiler.Registry 六个 Resolve* 导出面无生产调用方可收缩；canonicalJSON 三处可中心化（分层重复系 enforcement matrix 要求，保留合理）。
- implementation-quality-gate PASS，4 条 P3：止损回归已由白盒用例 59 承载（本 commit 入候选）；验收第 5 条 package digest 变半段显式延后（引擎尚无 handler 实现，属安装事务/后续批次——阶段 2 承载 envelope PackageDigest 执行绑定，PackageDigest 实际计算随安装事务在阶段 3+ 交付）；encoder.checkCoherence 不校验带外 IR 的 IO 段（正常路径不可达的加固建议，留待后续）；whitebox 提交已随本 commit 补齐。
- 白盒测试质量归白盒 review（41/41 PASS）；P3 建议按项目规则不阻塞 PASS。
