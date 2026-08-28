# 阶段记录汇编（阶段 0/1/2，§5 七项清单）

> 义务来源：`refactor-plan/incremental-seal-plan.md` §5"阶段记录至少保存"七项。本汇编为
> 阶段 0/1 交付义务补漏实物（2026-08-22）：内容从 git 历史、`.gates/results/phase-*.json`
> 与黑盒用例交付物逐项汇编，只登记已查证事实；已不可考的项显式标注"不可考 + 原因"，
> 不虚构。阶段 2 于开发收口批先登记可由提交、测试和命令证明的事实，Seal/集成后再补终态
> identity 与 receipt；阶段 3–7 封板时按同构逐项追加。
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
  `WorkflowDefinitionDigest = "sha256:3db87c9c6f3c0321ae55aa4d8196bc935b5a603a3948025352553d8ed1b9248f"`
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
- **阶段 1 记录形成时部分不可考 + 原因**——当时阶段 2 formal run 尚未启动
  （`.gates/results/` 无 phase-2 run JSON，openspec 无阶段 2 变更目录），其 worktree 的精确
  base 以之后阶段 2 run 受理时登记的 `baseSnapshot` 为准；当时可查证的最近 canonical 基线
  即上列 merge commit。

## 阶段 2：持久协议与恢复内核（封板与主线集成记录）

### 1. 阶段编号、run ID、sealed commit 与主线集成 commit

- 阶段编号：2（增量 Seal 阶段，对应总体方案工作包“2 持久协议与恢复内核”）。
- run ID：`phase-2-persistence-protocol`；现存
  `.gates/tmp/phase-2-persistence-protocol/state.json` 记录 status=`ACTIVE`、flow=`formal`、
  routeMode=`full`、split declaration=`no`、slicing=`no-split`。
- formal run base：`62419ed39ae3673323991cb2e28326b1fe4ff914`（“docs: 登记阶段 2
  （持久协议与恢复内核）阶段需求与受理清单处置”，父提交
  `637994a52a26d8b475f4b6c3ae769cafdd1fd374`）。开发收口前的实际实现候选 identity：
  `be2712def04ff2cc9e3f7212f08a6e2d37e55af1`（批次 3 testkit/独立进程验收，父提交
  `8b66ec55b04c19053d581da53807644e76bd5537`）。
- 已查证的阶段实现提交：1a persistence =
  `15fbbe60bc6e3716e4e067f1336b754533dd8664`；1b submit protocol =
  `397ce19cc3784c2b6be5590228ad2924b47dfea8`；1c unified admission =
  `ecc3983b1b67db754a7b28687824f149465094a9`；2a recovery =
  `0466bbc11700e16a13a9de0107d3133b2bca8d2a`，后续修复
  `6f6d5004d68a8d3a09dccd3601affb688506b952`、
  `8b66ec55b04c19053d581da53807644e76bd5537`；批次 3 testkit/harness =
  `be2712def04ff2cc9e3f7212f08a6e2d37e55af1`。
- sealed commit：主 run `phase-2-persistence-protocol` 的封板快照为
  `7d1e30ae862df5ba4d306abc2d21578241bcb217`；修复 run
  `phase-2-persistence-protocol-repair-v3` 的最终封板快照为
  `f95e1b1ccb9715c0c351bdf9314bee643883ef07`。两者分别由
  `.gates/results/phase-2-persistence-protocol.json` 与
  `.gates/results/phase-2-persistence-protocol-repair-v3.json` 记录，后者是修复后的阶段 2
  最终候选。
- 主线集成 commit：`f0cde67f0871b39485a50538c3fe82d7d31dfa12`（`integrate sealed phase 2
  candidate`，父提交 `a27b8d5c22f2b7b92a90a29cd76fed816e2c562d`，分支
  `codex/refactor`）。该提交将阶段 2 封板候选的持久化/协议/恢复内核、testkit、验收与记录
  纳入开发分支，并保留当前分支的安装与触发模型契约；关联机器记录见
  `.gates/results/phase-2-post-integration.json`。
- 过程留痕（不作合理化）：批次 4 任务书登记本 run 曾以手工状态方式续开发；对这次操作，
  现存临时状态只能显示续开发后的 snapshot/dispatch 投影，没有专门字段保存手工操作的
  命令、前后字节或授权 receipt，故这些细节不可考。本记录只承认该过程事实，不把它表述
  为协议设计、必要步骤或推荐路径。
- 批次 3 takeover/lifecycle 补录：state 记录 source snapshot 均为 `8b66ec55…` 的开发派发
  attempt 8（`dispatch-569c72ed140bc82e4f96b116`）、attempt 9
  （`dispatch-60f8967d861d61df5f085fa6`，未绑定 identity）与 attempt 10
  （`dispatch-6602cba8d3b7d9e20e55945f`）为 STALE；attempt 11
  （`dispatch-584e65dcd6dc2cb11fb28078`，identity
  `01a02e6d-335e-7d10-b610-64f5f2022751`）为 COMPLETED。attempt 8/10/11 的 binding 文件分别为
  `eb47f4ce15bc57dcc823052c79472017e4d0bb15aa5b1e80bc27f8efb3b9e2a8.json`、
  `e581633a79eda782dc15e962bdfd9a706e9da1216c253a8cc5db4e72c5fc105b.json`、
  `26b887ce6f03f4300b7fd68e033205fb2696aeba9a57a47ed7e60c49d0538726.json`。对应 event 文件还
  记录：attempt 8 原会话不可用、host 请求 fresh dispatch；attempt 10 worker 在产出代码
  结果前 shutdown，用户授权 takeover continuation；attempt 11 完成开发、提交 `be2712d` 并
  报告 build/vet/test 成功。attempt 9 未绑定 identity，也没有对应 binding/event 文件。这里只
  转录现存 lifecycle reason，不据此补写更完整的 takeover 因果或时间线。

### 2. 包摘要、installed-target digest、state schema version、workflow definition version、definition digest

- engine envelope：writer=`engine`、state schema version=`1`
  （`internal/engine/encoder.StateSchemaVersion`）、workflow definition version=`2`、definition
  digest=`sha256:3db87c9c6f3c0321ae55aa4d8196bc935b5a603a3948025352553d8ed1b9248f`
  （`internal/engine/definition/identity_gen.go` 与 `definitions/workflow.json` 同源）。
- PackageDigest envelope 校验侧已交付：`persistence.NewStore` 要求调用方注入非空
  `Config.PackageDigest`，读写时逐字段精确匹配；testkit fixture 使用测试值
  `sha256:testkit`。实际 owning-runtime package digest 计算与“只改实现 digest 分离”验证按
  阶段需求明确属于阶段 3+ 安装事务，不在本阶段伪造。
- 包摘要 / installed-target digest：开发 acceptance 现会把 candidate 构建到隔离
  `HOME/.local/bin/formal-gates`，实际执行 project-scope install + bootstrap，并以该固定 launcher
  和安装后的 package root 运行 legacy 前置流程；installer 的 source/installed digest 由该次
  临时 receipt 校验。receipt 和临时安装均由 `t.TempDir()` 持有、没有作为阶段工件入库，故本
  记录仍没有可供 Seal 引用的持久 installed-target digest。

### 3. 固定稳定插件摘要和候选安装摘要

- 固定 stable driver 的登记 source identity：
  `be6a787e50856c26689a77c7c3f4fa69c6a675fa`（阶段需求“环境约束”）；本阶段没有生成新的
  stable package digest receipt，故完整稳定插件摘要不可考。
- 候选 source identity：开发收口前为 `be2712def04ff2cc9e3f7212f08a6e2d37e55af1`；后续修复批的
  immutable identity 以对应 development-worker 提交为准，本开发中记录不预写尚未形成的
  commit。`TestAcceptanceInstalledProtocolHarness` 分别构建 candidate 与 test-only harness，
  candidate 走隔离 install/bootstrap receipt，harness 仍为不进入安装包的独立测试二进制。
  promotion receipt 尚未产生；本阶段的主线集成 receipt 是
  `.gates/results/phase-2-post-integration.json`，不把临时 acceptance 安装表述为 promotion。

### 4. 本阶段公开能力矩阵与唯一 writer

- 指向 `refactor-plan/route-matrix.md` 的“阶段 2”绑定补表和“阶段 2 隔离 engine / test-only
  协议面”表。
- 一句式结论：所有既有公开 workflow/维护入口维持阶段 1 绑定；公开 `workflow drive` /
  `workflow submit` 仍为 `unsupported`。新增 writer 只有显式隔离 state directory 内的
  `protocol.Engine` / `persistence.Store`；它由 internal API 或 test-only harness 调用，不是
  公开 CLI，不接管本 run 或 stable legacy run。
- 内部协议范围：版本 envelope、原子保存/锁/revision CAS/fingerprint、expected tasks/
  Attempt/pending action、typed request/event/action、幂等 Submit/freshness、SpawnReceipt/
  worker result/Ask/Operator/HostAction/lifecycle 统一接纳、失败分类及
  `RecoverAttempt`/`ReconcileUnknownReceipt`/`ReconcileHostAction`。公开 intake、审查、QA 与
  workflow façade 均未迁移。

### 5. 正常入口 smoke、新增能力 E2E、QA/gates 与 canary 证据

- 持久化/protocol/recovery 的直接机器证据：`internal/engine/persistence` 与
  `internal/engine/protocol` 单测覆盖 envelope 缺失/不匹配、完整性、锁/CAS/fingerprint、
  submit 幂等/freshness、provider/Attempt 绑定、统一接纳、UNKNOWN receipt/HostAction 对账、
  失败路由及旧结果。代表测试名见 route matrix 的内部协议表。
- testkit 证据：`internal/engine/testkit/testkit_test.go` 覆盖命名单次注入、spawn 响应中断不重复
  副作用、result-before-receipt 跨重启、submit 响应丢失幂等、HostAction UNKNOWN 对账不重做、
  并发 submit、旧 Attempt 结果、external fingerprint、fake VCS 与 namespace 隔离；
  `harness_test.go` 另外固化 envelope 写前屏障、capacity=1 自动补位、terminal summary 查询/
  重放以及安装 harness 的场景合同。
- 持久注入点（`persistence.Config.FaultInjector` 实际调用）：`intent_before`、
  `temp_sync_before`、`temp_sync_after`、`intent_after`、`replace_before`、`execute_before`、
  `replace_after`、`execute_after`、`observe_before`、`reconcile_before`、
  `commit_response_lost`。testkit fake host/worker 实际调用：`spawn_after_attach`、
  `result_before_receipt`、`submit_response_lost`、`host_observe_before`、
  `host_reconcile_before`、`host_commit_before`、`host_commit_after`。
- `concurrent_submit` 与 `stale_attempt` 常量虽已声明，但对应行为分别由 `concurrent-submit`
  的真实双调用与 `interruption --interruption nontransient` 后迟到结果驱动，不把这两个常量
  写成已接线的 `FaultPlan` 崩溃边界。
- 独立进程 harness 精确构建/调用：

  ```sh
  mkdir -p "$BIN" "$PROJECT/host-config" "$PROJECT/state" "$PROJECT/resources" \
    "$PROJECT/stable/state" "$PROJECT/stable/run"
  BIN=$(cd "$BIN" && pwd)
  PROJECT=$(cd "$PROJECT" && pwd)
  go build -o "$BIN/formal-gates-candidate" ./cmd/formal-gates
  go build -o "$BIN/engine-harness" ./internal/engine/testkit/cmd/harness
  export FORMAL_GATES_TEST_PROJECT="$PROJECT"
  export FORMAL_GATES_HOST_CONFIG="$PROJECT/host-config"
  export FORMAL_GATES_ENGINE_STATE="$PROJECT/state"
  export FORMAL_GATES_ENGINE_RESOURCES="$PROJECT/resources"
  (
    cd "$PROJECT"
    "$BIN/formal-gates-candidate" --help
    "$BIN/engine-harness" --scenario smoke
    "$BIN/engine-harness" --scenario smoke
    "$BIN/engine-harness" --scenario next-sequence
    "$BIN/engine-harness" --scenario full --project-root "$PROJECT-full"
    "$BIN/engine-harness" --scenario query-terminal --project-root "$PROJECT-full"
    "$BIN/engine-harness" --scenario terminal-replay --project-root "$PROJECT-full"
  )
  ```

  `NewIsolatedProject` 创建 `<project>/host-config`、`state`、`resources`、`stable/state`、
  `stable/run`。candidate `--help` 前后 project snapshot 无差异；harness 只在
  `FORMAL_GATES_TEST_PROJECT`/`--project-root` 下写入，首进程写
  `<project>/engine-state/state.json` 并报告 `phase=initial`，第二进程从同一路径恢复并报告
  `phase=restart`、`recovery.outcome=clean`，两次 summary/paths 相同。`FORMAL_GATES_HOST_CONFIG`、
  `FORMAL_GATES_ENGINE_STATE`、`FORMAL_GATES_ENGINE_RESOURCES` 由 `RunInstalled` 传给子进程，
  harness 不读取后三者作为 writer namespace。登记场景和参数见 route matrix；持久窗口可由
  `--scenario fault --fault <point>` 命名注入，随后以 `--scenario recover` 从独立进程对账。
- 产物布局：活动 run 保留 `<project>/engine-state/state.json`；终态 run 删除活动 state，只保留
  带完整 envelope、最后 request/event acceptance 及 canonical digest 的
  `terminal-summary.json`。事务中间文件 `state.json.intent`、`.state.json.<random>.tmp` 与
  `write.lock` 在完成/恢复后清除。harness stdout 报告 envelope、snapshot/summary、acceptance、
  NextResult、revision/freshness、故障调用、路径和副作用计数，不另写报告文件。
- legacy 回归边界：acceptance 以隔离固定 launcher 实际执行 project-scope install/bootstrap，
  然后运行 legacy `start -> requirements-clarification PASS -> requirement --confirmed -> abort`；
  这证明正常前置顺序未被 engine harness 改写，但不是完整 formal run、QA、canary 或 Seal。
- 本修复批开发自测命令与结果在 development-worker 返回中按实际执行登记；此前批次 4 的
  2026-08-23 自测记录保持历史事实，不由本段重写。
- QA/gates/canary：**尚未产生**——本批任务明确禁止运行 workflow snapshot、QA、门与 Seal；
  本节登记的是开发自测和直接测试证据，不是正式 PASS 或 gate verdict。

### 6. 资源 cleanup receipt

- **不可考 + 原因**——当前无阶段 2 独立 cleanup receipt；Go 测试资源由 `t.TempDir()` 在测试
  进程结束时清理，harness 正常完成仅核对 project-local stable/state 与 stable/run 为空、
  未生成可持久引用的资源清单或 cleanup receipt。
- 可查证的协议清理行为：persistence 测试断言成功提交只留 `state.json`，Recover 清扫
  intent/temp/orphan；`TestAcceptanceInstalledProtocolHarness` 断言 project snapshot 的差异仅为
  `engine-state/state.json` 与 `workspace/` 下 FakeVCS ledger/假宿主副作用文件，并单独断言
  project-local `stable/state`、`stable/run` 为空。它没有观察真实 stable registry，不能扩张成
  该环境未污染的证明。

### 7. 下一阶段 worktree 的精确 post-integration canonical base 与关联 receipt

- 阶段 3 worktree 的 post-integration canonical base：
  `f0cde67f0871b39485a50538c3fe82d7d31dfa12`（阶段 2 主线集成提交；实现树已通过
  `go test ./...`、`go test -tags phase0whitebox ./...`、`go build ./...` 与 `go vet ./...`）。
- 关联 receipt：`.gates/results/phase-2-post-integration.json`，其中同时绑定阶段 2 两个封板
  candidate snapshot 与该集成 identity，并列出验证命令及结果。阶段 3 从该实现基线开始；本
  文件本次修订只补录事实，不改变阶段 2 实现。
