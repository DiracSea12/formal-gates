# 阶段记录汇编（阶段 0/1，§5 七项清单）

> 义务来源：`refactor-plan/incremental-seal-plan.md` §5"阶段记录至少保存"七项。本汇编为
> 阶段 0/1 交付义务补漏实物（2026-08-22）：内容从 git 历史、`.gates/results/phase-*.json`
> 与黑盒用例交付物逐项汇编，只登记已查证事实；已不可考的项显式标注"不可考 + 原因"，
> 不虚构。阶段 2–7 封板时按同构逐项追加。
>
> 通用缩写：`P0-JSON` = `.gates/results/phase-0-distribution-002.json`，
> `P1-JSON` = `.gates/results/phase-1-decision-kernel.json`；`CASE-nnn` 指对应 JSON 的
> qaExecution 案例。git 对象均以全哈希登记。

## 阶段 0：分发安全与基线冻结

### 1. 阶段编号、run ID、sealed commit 与主线集成 commit

- 阶段编号：0（增量 Seal 阶段，对应总体方案工作包"0 冻结契约"，映射见
  `refactor-plan/incremental-seal-plan.md` §7 表）。
- run ID：`phase-0-distribution-002`（P0-JSON `runId`，status=SEALED）。
- sealed commit：P0-JSON `currentSnapshot` = `2a8e8864bf507a299d491ad3a4921960cce39de1`
  （"phase-0 distribution: unify workflow admission and close distribution gates"，父提交
  `6b8197931977a407eafe24e4f39af3d002854774`）。该对象存在于仓库对象库但不在任何分支
  引用上：Seal squash 后在重构线上以同主题、同父提交重建了集成副本
  `df9ba449b1ea3ab25f3d6224b0bd4d3eec9a62d8`。
- 主线集成 commit：集成链为 `df9ba449b1ea…`（阶段 0 交付 squash，重构线）→
  `2e730d099bd65173577d6de26b8eb1b533119a8a`（引擎需求同步 docs）→
  `dd2c1f388f85fec66e3e1bf96f45bfbe383d8018`（"落档 phase-0-distribution-002 封板产物
  （含黑盒用例交付物）"）→ 并入主线 merge `3a18377d2715d26c2e89137cdda8c63dc6595406`
  （"merge: 集成阶段 0+1 已封板交付与前置修复批（重构线首次落主线）"，父提交
  `c28d03fd15c61dd84500d7751cadff61a63f09b4` + `c2be4819acd59db2f7f5bae0bf4d374499994f7f`）。
  主线集成绑定 identity 取 `dd2c1f3…`（封板产物落档）与其所在 merge `3a18377…`。

### 2. 包摘要、installed-target digest、state schema version、workflow definition version、definition digest

- state schema version：legacy run 不带版本 envelope（阶段 0 明确不迁移、不回写旧状态，
  P0-JSON CASE-024）；未来面契约常量 `CurrentStateSchemaVersion = "1"` 已在
  `internal/validate/phase0.go:28` 冻结。
- workflow definition version / definition digest：阶段 0 冻结
  `CurrentWorkflowDefinitionVersion = "1"`、
  `CurrentWorkflowDefinitionDigest = "sha256:9ec68cd758cf9bad5bd8beefedac51755442c521ffe5e8276e805e82e66faa4c"`
  （`internal/validate/phase0.go:29-33`，来源 `definitions/workflow.json`）；缺失/不匹配写前
  返回 `UNSUPPORTED_RUN_VERSION` 的 fixtures 已建立（CASE-022/035）。
- 包摘要 / installed-target digest：**不可考 + 原因**——baseline receipt（221 项逐文件
  Lstat/realpath/digest manifest、root digest、installedTargetDigest、disjoint proof）落在
  隔离 QA namespace 的证据目录 `/tmp/fg-qa-ddc4/evidence/`，未随封板产物入库且已随 /tmp
  失效。可佐证其存在与绑定 identity 的留痕：P0-JSON CASE-001/026 记录 receipt 内容与冻结
  VCS `99dfef63…`（隔离候选构建源，git archive + go build）绑定。

### 3. 固定稳定插件摘要和候选安装摘要

- 固定稳定插件摘要：**不可考 + 原因**——stable characterization/baseline receipt 与
  installed-target digest 同落 `/tmp` 证据目录未入库；可佐证留痕：P0-JSON CASE-020
  （characterization receipt 绑定同一 VCS/package/installed identity）、CASE-026/027
  （冻结 stable installed path 的 validate/canary receipt 均绑定同一 installed identity）。
- 候选安装摘要：候选构建源 = `99dfef63358fc370a6f8d09eeba0fa4d45894641`（P0-JSON 全部
  CASE 程序栏统一登记"候选=99dfef63 git archive+go build"）；隔离安装 canonical
  paths/disjoint proof 在 CASE-025 逐项采集（Lstat/realpath/digest，无一指向开发 worktree、
  无 symlink）。receipt 本体同上不可考（/tmp 失效）。

### 4. 本阶段公开能力矩阵与唯一 writer

- 指向 `refactor-plan/route-matrix.md`（本 run 补交实物）"阶段 0/1 绑定结论"节及双面矩阵
  的"阶段 0 绑定"列。
- 一句式结论：公开 workflow 面全部为 legacy（stable driver 语义不变，start 仍带
  `--split` 属 legacy 维持项）；新增能力全部落在维护面 install-bootstrap（安装/registry
  事务、bootstrap、journal 恢复、future 契约常量）；`status`/`next`/`drive`/`submit`
  显式 unsupported。唯一 workflow writer = 冻结 stable driver 的 legacy runtime；
  install/bootstrap 无 workflow writer。

### 5. 正常入口 smoke、新增能力 E2E、QA/gates 与 canary 证据

- 正常入口 smoke（固定稳定插件文档化入口）：P0-JSON CASE-020（冻结 stable installed
  binary 全新 namespace bootstrap→start→show→resume→abort 收尾，全 rc=0）、CASE-012（真实
  宿主会话 claude -p / codex exec 驱动 allow/block 矩阵与 subagent 生命周期）、CASE-028
  （公开 install smoke：install+bootstrap+CLI/validate/canary）；安装内容未指向开发
  worktree：CASE-025/027（Lstat/realpath 无 symlink、开发区改动不影响 installed digest）。
- 新增能力 E2E（installed binary 驱动）：P0-JSON CASE-001~030、059、060 共 32 条黑盒
  （admission/bootstrap/事务恢复/故障注入/future envelope/diagnose/候选隔离等）+
  CASE-031~058、061~067 共 35 条白盒（`go:build phase0whitebox` 测试族，逐条登记测试名）。
- QA/gates：P0-JSON `qaExecution.status = PASS`（67 案例）；`complexity-gate` PASS、
  `implementation-quality-gate` PASS（P0/P1 范围内零阻塞，Windows 平台项按项目边界降
  P3）、`merge-gate` PENDING（主线集成时由 `3a18377…` merge 收口）；审查 9+6 轮
  （`completedReviewWaves: 9`、`extraReviewWaves: 6`）。
- canary 证据：CASE-001/027（隔离 test-project 内 installed binary 两次 canary portable，
  rc=0、0 FAIL、receipt 绑定同一 installed identity）；CASE-067（codex-hook canary 三模式）。
- 黑盒用例交付物：`.gates/results/phase-0-distribution-002.blackbox-cases.md`（228 行）。

### 6. 资源 cleanup receipt

- **不可考 + 原因**——阶段 0 未产生独立的资源 cleanup receipt 文件；候选/测试 namespace
  的资源登记清点证据（隔离安装清单、evidence 目录）落 `/tmp/fg-qa-ddc4/`，未入库已失效。
- 可佐证的残留核对留痕（P0-JSON）：CASE-006（uninstall 后无 temp/backup/journal 残留、
  registry record 转 disabled）、CASE-008（崩溃恢复后 journal 清理、无 temp/backup 残留）、
  CASE-016（并发事务后 journal/lock 残留 0）、CASE-028（install smoke 后
  temp/backup/journal/lock 残留 0）、CASE-010（候选攻击后 stable 侧无任何候选
  run/receipt/lease 落入）。

### 7. 下一阶段 worktree 的精确 post-integration canonical base 与关联 receipt

- 阶段 1 worktree 的 post-integration canonical base：`ba1f60d9a7c02c1e7afc2dfee2d8e10ed733df5f`
  （"docs: 阶段 1 驱动基线重冻结记录（5373c13→7929891，用户拍板）"），即 P1-JSON
  `baseSnapshot` 字段。
- 关联 receipt：P1-JSON `baseSnapshot` 字段本身（阶段 1 formal run 受理时绑定该基线的
  留痕）+ 提交 `ba1f60d…` 的提交记录（登记固定 stable driver 基线从 `5373c13…`
  （"Harden zero-context dispatch and code quality guidance"）重冻结至 `7929891…`
  （"docs: 重构需求补 P2 吸收/自测/门维度，split 绑定点后移至技术审后"）的用户拍板决定）。
- 链路核对：`dd2c1f3…`（阶段 0 封板产物落档）→ `598839f…`（ADR-001 冻结）→
  `db1822b…`（歧义清扫）→ `a853444…`（阶段 1 需求登记）→ `ba1f60d…`（驱动基线重冻结）。

## 阶段 1：纯决策内核与只读 Shadow

### 1. 阶段编号、run ID、sealed commit 与主线集成 commit

- 阶段编号：1（增量 Seal 阶段，对应总体方案工作包"1 纯决策内核"，映射见
  `refactor-plan/incremental-seal-plan.md` §7 表）。
- run ID：`phase-1-decision-kernel`（P1-JSON `runId`，status=SEALED）。
- sealed commit：P1-JSON `currentSnapshot` = `b9eb03c0cbfa8e66d235074ff4d5520f5b0e3535`
  （"阶段 1：纯决策内核与只读 Shadow（ADR-001/002 落地）"，父提交 = 阶段 1 基线
  `ba1f60d9a7c02c1e7afc2dfee2d8e10ed733df5f`）。
- 主线集成 commit：封板产物落档 `0e2b784bf20edba975814ff632d9b359bacfa1fc`
  （"落档 phase-1-decision-kernel 封板产物（含黑盒用例交付物）"，父提交即 sealed commit
  `b9eb03c…`）；并入主线 merge `3a18377d2715d26c2e89137cdda8c63dc6595406`（与阶段 0 同一
  集成批，重构线首次落主线）。封板后补充审计与修复批
  （`2e53a14348a8e168f57e0df82dc38b7bc0364901` →
  `c2be4819acd59db2f7f5bae0bf4d374499994f7f`）先于该 merge 进入集成侧父链。
- QA 时候选 HEAD：`221e75b68fc196925e702ecbbaf5d98d0680b1e9`（"docs: 阶段 1 开发批次日志
  （批 0-4 自测留痕 + 流程偏离记录）"，P1-JSON CASE-014/017 的候选构建源）；Seal squash
  收敛为 `b9eb03c…`。

### 2. 包摘要、installed-target digest、state schema version、workflow definition version、definition digest

- workflow definition version / definition digest（本阶段新增的 canonical 制品身份）：
  `definitions/workflow.json` 编译产物 version = "2"、stateSchemaVersion = "1"、
  writer = "engine"；与 `internal/engine/definition/identity_gen.go` 生成的
  `WorkflowDefinitionVersion = "2"`、
  `WorkflowDefinitionDigest = "sha256:e342a5f4a766d153682275f96e7df378da035bcdb06cb27c1cab772d50d938a7"`
  同源（compiler 同一生成动作产出制品与期望身份常量，禁止人工双写；当前工作树
  `definitions/workflow.json` 的 sha256 复算值与该常量一致）。阶段 0 冻结的
  `phase0.go` 契约常量（"1" @ sha256:9ec68cd7…）按阶段 0 契约保持不变，两版本并存且
  legacy run 不受影响（P1-JSON CASE-002 版本提升、CASE-015/019 legacy 共存）。
- state schema version：legacy run 仍无版本 envelope（阶段 1 不产生第二个状态写入权威）；
  制品 envelope 的 `stateSchemaVersion = "1"`。
- 包摘要 / installed-target digest：**不可考 + 原因**——`PackageDigest` 执行绑定与计算
  属已裁决延后项（引擎尚无 handler 实现；见
  `openspec/changes/orchestration-pipeline-engine-phase-1/post-seal-audit.md` §二 集合级
  缺口 2，移交阶段 2 受理确认）；隔离安装的清单/disjoint proof 证据落 `/tmp/fg-qa-blackbox/`
  未入库已失效，可佐证留痕为 P1-JSON CASE-017（git archive `221e75b…` 构建 + 隔离 HOME
  bootstrap/install，canonical path 全不重叠、0 symlink、真实 registry 字节不变）。

### 3. 固定稳定插件摘要和候选安装摘要

- 固定稳定插件摘要：stable 安装 SKILL.md 指纹 `46941e99`（P1-JSON CASE-019："stable
  launcher 只读 smoke+指纹+Lstat"，安装树 0 symlink、无指向开发 worktree 的链接）；完整
  digest receipt 同上不可考（/tmp 失效）。
- 候选安装摘要：候选构建源 = `221e75b68fc196925e702ecbbaf5d98d0680b1e9`（CASE-014/017）；
  隔离安装与 namespace disjoint proof 见 CASE-017；shadow telemetry 仅落候选目录
  （CASE-016）。receipt 本体不可考（/tmp 失效，同上）。

### 4. 本阶段公开能力矩阵与唯一 writer

- 指向 `refactor-plan/route-matrix.md` "阶段 0/1 绑定结论"节及双面矩阵的"阶段 1 绑定"列。
- 一句式结论：公开 workflow 面维持全部 legacy（候选 installed binary legacy 正常路径
  回归，CASE-018）；新增能力为只读 Shadow/诊断——engine decision/shadow 不经任何公开
  CLI 入口写权威 state（Shadow harness 为候选安装内测试入口，
  `internal/engine/shadow` `TestShadowReadOnlyObservesWithoutWriting`）；唯一 workflow
  writer 仍是 stable legacy runtime，不存在第二个状态写入权威。

### 5. 正常入口 smoke、新增能力 E2E、QA/gates 与 canary 证据

- 正常入口 smoke：P1-JSON CASE-019（固定 stable driver 只读 smoke + SKILL 指纹
  `46941e99` + 安装树 Lstat 检查）；CASE-018（候选 installed binary 的 legacy 正常路径
  回归：lightweight 三步闭环、只读命令、package validate、canary portable）。
- 新增能力 E2E：CASE-016（隔离安装候选 shadow harness 双跑：只读 legacy 状态、预测与
  fixture 期望 frontier 一致、权威状态字节/mtime 不变、telemetry 仅候选目录、同输入输出
  字节稳定）；CASE-001~014（canonical 制品独立性：freshness/装配序/round-trip/跨进程/
  digest 语义敏感/registry 完备/constructor/mutation/止损/marker）。
- QA/gates：P1-JSON `qaExecution.status = PASS`（60 案例 = 黑盒 CASE-001~019 + 白盒
  CASE-020~060，白盒即 `go test ./internal/engine/whitebox_qa/ -count=1` 41 用例函数、
  54 子测试全绿）；`complexity-gate` PASS（2×P3 非阻塞）、`implementation-quality-gate`
  PASS（4×P3 非阻塞，两条随白盒测试提交收口）、`merge-gate` PENDING（主线集成时由
  `3a18377…` merge 收口）；审查 1 轮 0 修复（`completedReviewWaves: 1`）。
- canary 证据：阶段 1 无新 canary 面；回归沿用阶段 0 canary portable 于候选树
  （CASE-018 内 validate/canary 全过）。
- 封板后补充审计（追加证据，非 Seal 组成部分）：
  `openspec/changes/orchestration-pipeline-engine-phase-1/post-seal-audit.md`（敌意复审
  6 项发现、黑盒重设计 45+7 条、52/52 全量执行 PASS 零回归）、
  `blackbox-audit-execution-results.md`（52 条逐条执行结果表）、
  `blackbox-audit-cases.md`（QA-B01~B52 用例集）。
- 黑盒用例交付物：`.gates/results/phase-1-decision-kernel.blackbox-cases.md`（137 行）。

### 6. 资源 cleanup receipt

- **不可考 + 原因**——阶段 1 未产生独立的资源 cleanup receipt 文件；候选 namespace
  证据目录（`/tmp/fg-qa-blackbox/` 等）未入库已失效。
- 可佐证的残留核对留痕：P1-JSON CASE-015（全量 go test 前后真实安装
  registry/launcher/release 逐字节一致——测试隔离修复验证）、CASE-017（候选与 stable
  canonical paths 不重叠、真实 registry 字节不变）。
- 已登记未清的环境残留（非候选资源，属真实用户级 registry 环境数据）：真实
  `~/.formal-gates/registry.json` 内 142 条阶段 0 测试残留记录，无现行代码读写，登记于
  `post-seal-audit.md` §五.5（阶段 2 受理继承输入第 5 项）。

### 7. 下一阶段 worktree 的精确 post-integration canonical base 与关联 receipt

- 阶段 0+1 集成后的主线 post-integration canonical identity：
  `3a18377d2715d26c2e89137cdda8c63dc6595406`（"merge: 集成阶段 0+1 已封板交付与前置
  修复批（重构线首次落主线）"），sealed candidate identity（`b9eb03c…` 封板 + `0e2b784…`
  落档）经该 merge 绑定到主线。
- 关联 receipt：阶段 2 受理登记见
  `openspec/changes/orchestration-pipeline-engine-phase-1/post-seal-audit.md` §五
  "阶段 2 受理登记清单（继承输入）"（5 项），受理登记提交
  `e1c4c5df415cb133e938a534b30bc306b17a3451`（"merge: 返修与非Windows修复、审计补充用例、
  阶段2受理登记（封板后审计收尾）"）。
- **部分不可考 + 原因**——阶段 2 的 formal run 尚未启动（`.gates/results/` 无 phase-2
  run JSON，openspec 无阶段 2 变更目录），其 worktree 的精确 base 以阶段 2 run 受理时
  登记的 `baseSnapshot` 为准；当前可查证的最近 canonical 基线即上列 merge commit。
