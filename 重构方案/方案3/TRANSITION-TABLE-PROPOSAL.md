# 迁移表结构（推荐版 + 问题背景）

> 用途：**可独立分享的讨论稿**——给其他 AI 评估「formal-gates 硬流水线化的迁移表结构」。
> Part A 是问题背景（自包含，不依赖本项目仓库）；Part B 是推荐版结构；Part C 是待讨论问题。

---

## Part A 问题背景

### A.1 项目是什么

formal-gates 是一个个人项目：**LLM agent（AI 编程助手）驱动的正式开发流程门禁系统**。组成：

- **Go 编写的 CLI**：一个状态机，约 20 个 `workflow` 命令（start/resume/abort/reset、prepare-action/record-action、prepare-gate/record-gate、claim-dispatch、requirement、slicing、route、snapshot、carry、authorize-repair、seal、qa-*）。
- **流程定义**：SKILL.md 定义九步正式流程。
- **生命周期钩子**：活跃 run 时阻断非法状态操作。

运行模式：以 VCS（git）为依托。每个 run 冻结基线；流程各步派发**独立零上下文的子代理**（审查者/开发工作者）执行，审查结论由主代理（LLM 编排者）校验后经 CLI 记录，PASS/FAIL 留痕。审查者与开发完全隔离；派发内容写入规范文件、`claim-dispatch` 校验 hash 防篡改。

> **host 无关**：本设计不依赖任何具体 LLM host——接入层（钩子、独立会话通道）可换，本文件讨论的迁移表结构对任何 LLM agent CLI 同样成立。

### A.2 痛点：run 内编排错误导致整个流程阻塞

**问题主体**：run 内细节编排错误反复触发现有硬阻塞规则，而被阻后的 run **没有机械恢复路径**，整个 run 卡住无法继续——阻塞本身成为新的死点。次要后果：即使没被阻塞，乱编排也造成反复试错、上下文翻腾、不必要派发，开发效果系统性下降。

主代理（LLM 编排者）的错误形态是**开放的、非穷举**——主代理自由发挥的空间有多大，错误形态就有多丰富。下表只是已观察到的归因分组（实例取 run 内；受理阶段的轻量/正式错只是同根因的最轻实例，不是问题主体）：

| 错误形态 | 实例（run 内） | 根因 |
|---|---|---|
| 路线选择错 | 下一步靠主代理自己从文档/记忆拼读而非 CLI 状态：快照前 resume、start 的 base/current 设错、未认领就记录 | 「下一步是什么」无程序查表，靠模型判断 |
| 询问纪律错 | 该问的不问（修复授权、scope 决策、seal 确认未授权就推进）；不该问的反复问 | 该不该问无程序约束，靠模型自觉 |
| 授权推定错 | 把「没反对」当「同意」擅自推进；「看起来完成」当完成直接记录 | 流程推进无显式授权门，静默被当作同意 |
| 并行/分片拓扑错 | 该并行的串行执行；规则要求分片独立开发/QA/门，主代理把部分工作放主干 | 并行集与分片→worktree 绑定无程序持有 |

另有已知但归不进上类的形态（例）：手写派发提示词、需求改动后不重确认、测试污染仓库、上下文紧反复重启——**分类远未穷尽**。改造方向**不是逐类拦截错误**（追不完），而是消灭根因：把「下一步/问不问/授权有没有/并行分片」的决策权整体从主代理手里拿走。

### A.3 本方案拟定的核心方向

**流程程序驱动（硬流水线）**：CLI 持有「迁移表」，`下一步`、`必问点`、`授权判定`全部由 CLI 状态机机械决定；主代理只执行 CLI 发下的指令；hook 压缩到窄判定面。附带三条纪律：

- **该问才问**：必问点 = 编译期声明（流程定义里写死暂停点），清单外不提问；
- **授权不推断**：无显式确认记录 = 未授权，「没反对」不是状态；
- **每条硬阻断成对出现**：阻断必带机械恢复路径，卡死可检测、可恢复（watchdog/心跳）。

### A.4 当前进展

1. 已调研业界实现（详见 Part B 的证据引用）：AWS Step Functions ASL、XState、LangGraph、Prefect、Temporal、Airflow、LLM agent CLI 上层生态（claude-orchestrator / brimstone / claude-task-master）、MetaGPT。
2. 已画出**工作流现状拓扑**，节点编号 N0–N13（规则对齐锚点）：

```
N0 受理（澄清→确认→轻量/正式）
 → N1 启动（--split yes/no 强制声明）
 → N2 需求澄清登记（修订: --meaning preserved|changed）
 → N3 产品审 Part1（仅 P2/P3 可 PASS；FAIL 由用户逐项处置：确认→重审、驳回→作废回 N2）
 → N4 start-readiness Part2（FAIL 回 N2，黑盒用例增量修订）
 → N5 拆分决定（须 N4 PASS 之后；split=yes 时总实例+各切片实例）
 → N6 绑定路线（full/custom，拆后逐切片各确认一次）
 → N7 开发 ────────────┐
 → N8 黑盒 QA 准备（隔离 worktree，与开发并行）── 快照门 N10（开发完成 ∧ 黑盒 qa-review PASS）
 → N9 白盒 QA 准备（开发之前段完成）
 → N10 快照门（snapshot --dispatch；--user-requested 手动放行）
 → N11 开发后审查（黑盒 QA 执行 ‖ 白盒 QA 执行 ‖ 各已选门，全并行）
      ├─ 全部 PASS → N13 Seal
      └─ 有 FAIL/P0/P1 → N12 修复（carry 继承判定 → 修复派发 → 新快照 → 回 N10；轮次上限 3，用尽后 authorize-repair 逐轮授权）
 → N13 Seal（git >1 提交压缩单条；--skip/--user-requested 需授权）
```

横切（挂在所有节点上）：resume/abort、claim-dispatch（hash 校验）、生命周期 hook、并行性检测（stderr 提示，目前无强制）、结果核查、中途需求修订回 N2。旁路：gate run/gate report（脱离 run 的快速检查，不持久化）。

3. 正在做：**定迁移表结构**（本文件）。

---

## Part B 推荐版迁移表结构

### B.1 五个决策点的推荐（含理由）

| # | 决策点 | 推荐 | 理由 |
|---|---|---|---|
| 1 | 状态粒度 | **节点 × 子状态两级**（如 `N10_WAIT`、`N11_IN_FLIGHT`） | 拓扑节点给流程形状；子状态给机械校验粒度（PREPARED/PENDING/CLAIMED/PASS/FAIL 是现有 CLI 已有状态，直接继承、无信息损失）。业界一致是两级以上：XState 层级状态机、ASL 状态、LangGraph 节点。 |
| 2 | confirm 字段 | **合并为一列** `confirm: none \| ask`，值类型化 approve/reject/edit | 必问点（流程声明暂停）与授权哨兵（用户主动授权）**机械语义完全相同**：迁移前必须有用户确认记录。区分只在值来源，分两列会引入「两列都写了怎么办」的歧义。LangGraph 也只有 interrupt 一个通道。 |
| 3 | parallel | **显式声明**：进入并行段的迁移上声明并行组（成员列表），汇合迁移前置 = 组内全部终态 | ASL `Parallel/Branches`、XState `parallel` 均显式。关键原因：指令下发（A1，CLI 输出下一步唯一指令）需要完整并行集列表，从 pre 隐式推导会产生第二处真相源。 |
| 4 | 表形态 | **Go 代码内静态声明 + 启动时自检**（无环、状态可达、终态唯一、指令模板可渲染） | 类型安全 + 编译期错误；自检对齐 ASL 部署期校验（InvalidDefinition 无法部署）。运行时零猜测。 |
| 5 | 条件迁移 | **允许，但限定声明式**：`next: 固定 \| [{if: 条件, then: 状态}, ...]`，条件只做字段值匹配（结果状态、并行集完成度、轮次计数） | 所有领先项目都有分支机制（ASL Choice、LangGraph 条件边、XState guard）。限定形态保住「表是唯一真相源」——判定逻辑不散落到代码里。 |

### B.2 字段定义（一行 = 一条合法迁移）

```
Transition = {
  from:      State              // 节点×子状态，如 N10_WAIT
  trigger:   Trigger            // CLI 命令事件（命令名 + 参数 schema 校验）
                               //  或生命周期事件（subagent_stop，驱动并行集成员计数）
  pre:       [Precondition]     // 前置条件，全机械校验、全过才允许触发
  effect:    [Effect]           // 副作用声明：追加事件日志、状态推进、产物生成
  next:      State | [Cond]     // 固定目标 或 条件迁移列表（见决策点 5）
  confirm:   none | ask         // ask = 迁移前必须已收集类型化确认并写入事件日志
  parallel:  ParallelGroup?     // 并行组声明：{ id, members: [...], join: 全部终态 }
  output:    OutputTemplate     // 下一步唯一指令渲染模板（确切命令+参数、并行集、恢复路径）
  on_error:  ErrorRoute         // 错误路由（第二期完整化：错误名 → 重试/硬停）
}

Cond = { if: <字段> <op> <值>, then: <State> }
Precondition（机械校验维度）:
  - 状态匹配          from == 当前投影状态
  - 产物存在          dispatch 已 CLAIMED / 快照已记录 / 用例已批准
  - 确认记录          confirm=ask 时：事件日志存在类型化确认（approve/reject/edit）
  - 并行约束          触发对象 ∈ 已声明并行组；汇合迁移 → 组内全部终态
  - VCS 身份          分片场景：快照记录只能落在分片 worktree（分片→worktree 绑定 CLI 持有）
  - 幂等              终态不可重写；同触发去重
  - 轮次计数          authorize-repair 授权数 ≥ 1（每授权只允许一轮）
```

表外机制（不属于表、但表依赖它们）：

- **事件日志（append-only）= 唯一真相源**；状态 = 日志投影；恢复点 = 日志尾部最后一个成功步骤（Temporal 模式）。
- **指令渲染 = 同令牌纪律**：状态名、门条件名、输出指令文本从同一个源（迁移行）生成，杜绝对不上号。
- **分片 = 同一张表多实例化**：总实例 + 各切片实例各自跑同一迁移表，靠 `pre` 里的 VCS 身份区分实例归属；切片继承整体审查结果由表外规则持有。
- **非法迁移 = 硬停 + 恢复路径**：触发不匹配任何一行 → 阻断信息输出「违反的规则 + 唯一恢复指令」（阻断点没有恢复路径 = 设计缺陷）。

### B.3 真实实例（三行）

```
┌ from: N10_WAIT
│ trigger: snapshot --dispatch <id> [--user-requested]
│ pre: 开发完成记录存在；黑盒 qa-review=PASS（或 user-requested 确认记录）；
│      dispatch 已 CLAIMED；VCS 身份 = 本分片 worktree
│ next: N11_PREPARE
│ confirm: none
│ output: "并行准备：prepare-action qa-execution；prepare-gate <每已选门>"
│ parallel: { id: review-set, members: [qa-execution, whitebox-execution, gate:<每已选门>] }

┌ from: N11_IN_FLIGHT
│ trigger: record-gate <gate> --status <PASS|FAIL> [--compared <base>..<current>]
│ pre: gate ∈ review-set；dispatch 已 CLAIMED；--compared 匹配基线→当前；该 gate 无已记录终态
│ next: [ { if: review-set 全终态 ∧ 全部 PASS, then: N13_READY },
│         { if: review-set 全终态 ∧ 存在 FAIL/P0/P1, then: N12_REPAIR } ]
│ confirm: none

┌ from: N12_REPAIR_LIMIT（轮次上限 3 已耗尽）
│ trigger: authorize-repair --cycles 1
│ pre: 无未使用的授权（每授权只允许一轮、不能累积）
│ next: N12_REPAIR_ALLOWED
│ confirm: ask（approve/reject，记录进事件日志）
```

---

## Part C 待讨论问题（给其他 AI）

1. **结构完备性**：Transition 字段是否完备/冗余？有没有我们漏掉的迁移场景表达不了？（候选：需求修订回退 N2、abort/reset、adopt-external、scope 决策、gate run 旁路）
2. **决策点 2（confirm 合并）**：必问点（流程声明暂停）与授权哨兵（用户主动授权）合并成一列是否合理？分两列有没有我们没想到的必要？
3. **决策点 3（parallel 显式）**：并行组声明放「进入并行段的迁移」上，汇合靠 pre——有没有更好的表达？（Airflow 的 trigger_rule 是下游声明式，ASL 是 Parallel 容器，XState 是 parallel 状态）
4. **决策点 5（条件迁移）**：限定声明式条件够不够？record-gate 的结果分支之外，还有哪些场景需要条件迁移？
5. **分片实例化**：同一张表多实例 + VCS 身份 pre 的方案有没有坑？（切片间共享审查结果、总实例集成快照的归属）
6. **恢复路径**：on_error 第二期完整化的边界——错误路由表（错误名→重试/硬停）要不要本期就进表？
7. **与业界对齐**：这个结构相比 ASL/XState/LangGraph 最大的差异是什么？这些差异是必要的吗？
