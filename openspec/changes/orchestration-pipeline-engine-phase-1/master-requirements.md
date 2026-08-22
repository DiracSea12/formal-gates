# 阶段 1：纯决策内核与只读 Shadow——阶段需求

状态：需求与方案已由用户确认（2026-08-22）；本文件是本次 formal-gates run 的唯一阶段化需求入口。

## 权威来源与边界

- 阶段切片、环境模型和退出条件以 `refactor-plan/incremental-seal-plan.md` 为准，重点是第 2 节和第 3 节"阶段 1：纯决策内核与只读 Shadow"。
- 步骤模型与定义制品形态以 `refactor-plan/adr-001-typed-authoring-compiled-canonical-definitions.md` 为准（重大技术选择已冻结）。
- 详细目标架构以 `refactor-plan/final-implementation-draft.md` 为参考（第 3、11 节）；总需求仍由 `openspec/changes/orchestration-pipeline-engine/master-requirements.md` 第 5 节持有。
- 本 run 同时正式登记 ADR-001 架构修订（598839f/db1822b）的受审：产品审/技术审覆盖修订后四份文档的一致性。
- 后续阶段语义不倒灌本阶段；表述差异以本阶段确认需求和增量 Seal 计划阶段 1 条目为准。

## 目标

在不产生第二 workflow writer、不改变 stable driver 公开语义的前提下，交付可独立验证的确定性决策内核与只读 Shadow，并修复阶段 0 遗留的测试隔离缺陷，使阶段 2 可以在此基础上实现可靠写入协议。

## 范围与验收

1. **Authoring**：封闭 Go 类型变体 + constructor + 显式节点/步骤表为唯一定义编写形态（LocalStep、DurableStep、AgentStep、HumanAskStep、HostActionStep、ParallelStep）；变体只暴露适用字段；authority/runner 由变体派生并物化，非法组合在正常 authoring API 下不可构造。
2. **Closed-world compiler**：registry ID 解析（存在、唯一、kind 匹配）、全局图不变量（可达性、非法循环、依赖存在、分支目标封闭、并行组 join/failure 覆盖、版本绑定）、归一化、canonical 编码；compiler 不解释业务表达式、不证明 predicate 语义。八类非法定义拒绝结果全保留，按 enforcement matrix 分层拦截。
3. **Canonical 制品**：compiler 同一生成动作产出 `definitions/workflow.json` 与期望身份常量，禁止人工双写；制品为公共头 + 封闭变体 payload，不含函数、闭包、内存地址、绝对路径、当前时间或无序 map。
4. **决策核心**：RunPhase、TaskKey、TaskTransitionTable、Observe/Decide/SelectIssued、NextResult 六类 Kind 校验；相同 state+observation 产出字节级稳定的 canonical Plan；eligible frontier 完整、固定顺序。
5. **十条独立验收**：freshness CI（重新生成无 diff，独立于 round-trip）、assembly 顺序不变、round-trip、跨进程/重复构建确定性、definition/package digest 分离（只改 handler 实现不改 ID 与定义语义时 definition digest 不变而 package digest 变）、digest 语义敏感性（改 dependency/policy/reason/schema ID/handler ID/join 语义时 definition digest 必变）、registry 完备性、constructor 非法状态、mutation tests、复杂度止损（新增普通业务节点不得要求修改 compiler core）。
6. **Shadow**：只读 legacy 状态与外部事实，输出 eligible frontier 预测与差异；不写权威 state、不触发副作用；从阶段候选 installed binary 在独立测试项目执行。
7. **`MISSING_ENGINE_ADAPTER`** 仅 diagnostic-only；正常 compile/drive 路由 `BLOCKED_BUG` 并拒绝签发；最终候选必须有 marker 扫描证明不存在该技术债。
8. **环境缺陷修复**：phase-0 测试隔离修复——测试不得写真实用户级 registry/安装路径（含故障注入桩替换）；stable driver 重冻结记录（2026-08-22 用户拍板：main HEAD `7929891` 构建、SKILL 指纹一致、package validate + canary portable PASS；取代从未实际驱动过 run 的 5373c13）作为本阶段基线证据。
9. **Legacy 回归**：stable driver 文档化正常入口 smoke 与 legacy 正常路径回归通过；本 run 全程由重冻结后的 stable launcher 驱动并留证。

## 非目标

- 不新增 workflow writer、不写权威 state、不改变公开入口（阶段 2 起）。
- 不实现 `drive/submit`、不迁移 intake/审查/QA 业务流程（阶段 3+）。
- 不实现 split、SVN/P4 adapter、宿主 canary（阶段 5/6）。
- 不做最终全局切换或 stable 退役（阶段 7）。

## 环境约束

- 本 run 在 `codex/refactor-phase-1` worktree 开发，基线为 db1822b；主分支其他改动不属于本 run。
- 固定 stable driver（2026-08-22 用户拍板重冻结）：`~/.formal-gates/releases/0.1.0-macos-arm64` 与 `~/.local/bin/formal-gates` 已重建为 main HEAD `7929891`（git archive + go build；SKILL 指纹 46941e99 一致；package validate 与 canary portable PASS）。阶段 1–6 固定使用该驱动，不再随开发更新；阶段 7 按计划切换。重冻结原因：阶段 0 名义冻结的 5373c13 缺少现行流程命令（`--split`、`slicing`、`settle-findings`、`qa-worktree`），从未实际驱动过任何 run；phase-0 run 实际由更新构建驱动，偏离自狗粮规则，如实留痕。旧树备份 `/tmp/stub-backup/`。
- phase-0 故障注入测试曾将旧 launcher 写成 25 字节空桩（与 `package_test.go:252` 桩逐字节一致）并污染真实 registry（142 条测试记录）；测试隔离缺陷在本阶段修复并回归。自本 run 起每次会话首次调用前 launcher smoke 留证。
- 阶段候选从已提交快照构建隔离安装，在独立测试项目/host config/state namespace 验证；候选不得驱动本 run、写 stable registry 或签发权威 Seal。
- 开发前先完成六种代表性 step 的 compiler spike（engine local、durable side effect、host action、agent task、human ask、parallel/join），确认 IR/registry/encoder 边界；spike 代码不进入 production。

## 本 run 预定决策

- 拆分：no-split（单一强耦合内核单元；理由随 slicing 命令留痕）。
- 路线：full（黑盒 + 白盒 + 全部四道门，与阶段 0 一致）。
- 开发顺序：compiler spike 先行，确认边界后正式实现。

## 一致性审查要求

- 开发前独立审查必须从零对照 `incremental-seal-plan.md`、`final-implementation-draft.md`、ADR-001、本阶段 requirements/design 和修订后的总需求，检查阶段范围、版本边界、authoring/compiler/制品契约、证据和退出条件是否一致。
- 审查提示词不得注入主代理解释、未解决 finding、修复说明或预期结论；CLI 合法注入的已拍板 settled finding 属 `[Action input]`。
- 发现不一致时作为候选 finding 留痕，由用户逐项确认或驳回。

## 阶段退出条件

阶段 1 在一次完整 formal-gates run 内完成 spike、开发、installed binary 验证、legacy 回归、独立审查、QA、必要修复和 Seal；canonical 制品十条验收与 mutation/止损证据绑定候选 identity；范围内未通过项不得以口头说明替代。
