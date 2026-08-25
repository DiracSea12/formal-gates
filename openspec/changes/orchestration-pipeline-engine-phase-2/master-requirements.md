# 阶段 2：持久协议与恢复内核——阶段需求

状态：需求与方案已由用户确认（2026-08-22）；本文件是本次 formal-gates run 的唯一阶段化需求入口。

## 权威来源与边界

- 阶段切片、环境模型和退出条件以 `refactor-plan/incremental-seal-plan.md` 为准，重点是第 2 节和第 3 节"阶段 2：持久协议与恢复内核"。
- 持久化/恢复协议的技术形态以 `refactor-plan/final-implementation-draft.md` §2.2（主循环与 NextResult）、§9（宿主动作、中断与本地副作用）、§10（旧 run、终态与诊断）为准；总需求仍由 `openspec/changes/orchestration-pipeline-engine/master-requirements.md` 第 5 节持有。
- 继承输入以 `openspec/changes/orchestration-pipeline-engine-phase-1/post-seal-audit.md` §五（阶段 2 受理登记清单 5 项）为准，处置见下文"继承项处置"。
- ADR-001（typed authoring + 编译式 canonical 制品）与 ADR-002（开发分批与验证分层）继续有效；本阶段无新增 ADR。
- 后续阶段语义不倒灌本阶段；表述差异以上述权威文档原文为准，本文件只是阶段化登记。

## 目标

在不产生第二个公开 workflow 写入权威、不改变 stable driver 公开语义的前提下，在隔离 namespace/test harness 中交付 engine 的持久化写入与恢复协议（版本 envelope、原子保存、文件锁、revision/CAS、幂等 submit、失败分类路由、崩溃恢复），使阶段 3 可以在此基础上实现最小纵向 engine 闭环。

## 范围与验收

1. **版本 envelope**：engine state 带四字段 envelope（writer、stateSchemaVersion、workflowDefinitionVersion、packageDigest）；缺失或不精确匹配一律 `UNSUPPORTED_RUN_VERSION` 写前拒绝、绝不写状态；`diagnose` 的最小 raw/envelope parser 只读报告是唯一例外。`show/status/next` 只从带版本绑定的 terminal summary 回落。
2. **持久化基座**：原子保存（persist intent → execute → observe/reconcile → commit result）、完整性摘要、文件锁、revision/CAS、external fingerprint 重验。
3. **提交协议**：expected tasks、Attempt、pending action、typed request/event/action、幂等 `submit`、freshness 校验。
4. **统一接纳**：SpawnReceipt、worker result、Ask/Operator event、HostAction receipt、lifecycle event 的统一接纳。
5. **失败分类路由与恢复**：副作用 UNKNOWN、result-before-receipt、旧 Attempt、重复 submit、中断恢复按草案 §9.1 分派（客观瞬态且 bindings 未变 → 自动 resume 原 Attempt；任务/snapshot/责任变化或已知非瞬态 → 新 Attempt、旧 Attempt terminate/stale；bindings 未变但原因未知 → Ask；receipt UNKNOWN → 先查 lifecycle，唯一匹配自动 attach，多重/无匹配进 Operator）；engine 故障不得动态降级为 agent。
6. **确定性故障注入**：fake host、fake worker、fake VCS 在每个持久边界做确定性注入验证。
7. **隔离与回归**：engine 写入只进入隔离状态目录；正常公开插件仍由 legacy runtime 完整驱动；从阶段候选的实际 installed binary 启动独立 test project，执行 legacy 回归与声明的 protocol/recovery harness；验证候选 host/config/state/resource namespace 不污染固定稳定环境。
8. **负向测试**：缺失版本字段、schema/definition mismatch、旧版本写入、raw `diagnose`。
9. **envelope PackageDigest 执行绑定**（继承项 2 的 envelope 校验侧）：envelope 校验侧在本阶段交付；PackageDigest 的实际计算与"只改实现 digest 分离"的另一半验证（只改实现时 PackageDigest 必变而 DefinitionDigest 不变）随安装事务在阶段 3+ 交付（总需求 §10.16 保留至终验）。
10. **交付义务**：`refactor-plan/route-matrix.md` 活文档更新阶段 2 绑定列（本阶段无新公开面：公开 `drive/submit` 仍 unsupported，submit 协议面在矩阵中如实标注为 harness 内部协议面）；`refactor-plan/stage-records.md` 按同构追加阶段 2 节。
11. **成比例性影响评估**（继承项 4，用户拍板口径）：针对重构完成后的目标架构（受限 submit 事件、`availableActions` 带 freshness token、新 run 机制）评估"封板后小额修复走正门的成比例性"问题的影响性，产出评估文档；不改引擎行为。

## 非目标

- 不实现公开 `workflow drive/submit` 命令路由、不迁移 intake/审查/QA 业务流程（阶段 3/4）。
- 不实现 split、SVN/P4 adapter、宿主 canary（阶段 4/5/6）。
- 不做最终全局切换或 stable 退役（阶段 7）。
- Windows 平台分叉（gen-definition rename ReplaceFileW、perm 断言平台感知）维持延期（用户拍板，总需求 §2.6 边界：不支持平台只作 P3 建议、不阻塞 PASS）；本阶段设计保留平台分层，实现与验证不依赖 Windows CI。

## 环境约束

- 基线：公用线 `codex/refactor` @ `637994a`（阶段 1 封板后补漏批已集成 + stage0-1-delivery-repair 交付物：路由矩阵/阶段记录汇编/反向普查机器测试）；开发 worktree `formal-gates-refactor`；main 其他改动不属于本 run。
- 固定 stable driver（重冻结，main `be6a787` 构建，三路径一致）：本 run 全程由其驱动；每次会话首次调用前 smoke 留证。
- 阶段候选从已提交快照构建隔离安装，在独立测试项目/host config/state namespace 验证；候选不得驱动本 run、写 stable registry 或签发权威 Seal。
- 开发形态按 ADR-002 分批：每批独立原子 commit、批边界 `go build && go vet && go test` 全绿、批间 `workflow snapshot` 推进快照；黑盒 QA 与开发并行推进。

## 继承项处置（post-seal-audit §五）

1. 验证深度参照（52 用例底线）：纳入验收——本阶段黑盒 QA 规模以 52 用例为底线参照、按新准则设计（禁已有测试充当用例主体或证据）。
2. PackageDigest 执行绑定：envelope 校验侧在本阶段（范围 9）；安装事务侧计算与"只改实现 digest 分离"的另一半验证随计算侧在阶段 3+ 交付。
3. Windows 延期项：维持延期（见非目标）。
4. 正门成比例性：转为"重构完成后影响性评估"，在本阶段交付评估文档（范围 11）。
5. registry 142 条残留清理：已在阶段 2 受理前由用户环境处置闭环（registry 现存 2 条记录、epoch=2），本 run 无剩余义务。

## 本 run 预定决策

- 拆分：no-split（单一强耦合协议内核；理由随 slicing 命令留痕）。
- 路线：full（黑盒 + 白盒 + 全部四道门，与阶段 0/1 一致）。
- 开发顺序：持久化基座（envelope/锁/CAS/原子保存）先行，提交协议与接纳在其上分批推进，故障注入与隔离验证随批落地。

## 一致性审查要求

- 开发前独立审查（产品审/技术审）必须**从零对照原始方案**：`incremental-seal-plan.md`（第 2 节 + 阶段 2 条目）、`final-implementation-draft.md`（§2.2/§9/§10 + §11 阶段 2 条目）、ADR-001/002、`post-seal-audit.md` §五，逐项核查本文件相对原始方案**有无偏差或遗漏**（用户 2026-08-22 明确要求），以及阶段范围、版本边界、协议契约、证据和退出条件是否一致。
- 审查提示词不得注入主代理解释、未解决 finding、修复说明或预期结论；CLI 合法注入的已拍板 settled finding 属 `[Action input]`。
- 发现不一致时作为候选 finding 留痕，由用户逐项确认或驳回。

## 阶段退出条件

阶段 2 在一次完整 formal-gates run 内完成分批开发、installed binary 验证、legacy 回归、独立审查（含对照原始方案的偏差/遗漏核查）、QA（黑盒底线参照 52 用例）、必要修复和 Seal；协议/恢复证据绑定候选 identity；范围内未通过项不得以口头说明替代。
