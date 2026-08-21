# 端到端走查示例

> 目标读者：主代理（编排者）。本文用一个虚构场景把正式流程完整走一遍，每一步给出
> 「读什么 reference、派发什么独立代理、认领时机、生命周期检查点」以及可照做的动
> 作。九步顺序以 `SKILL.md` 为唯一本体；命令形式以 `references/formal-flow.md` 为
> 准；本文只示范走法，不替代前两者。首次走完整流程前，先读完本文再动手。

## 目录

- [场景](#场景)
- [1. 请求与受理](#1-请求与受理)
- [2. 需求澄清与确认（受理阶段）](#2-需求澄清与确认受理阶段)
- [3. 是否进入正式流程](#3-是否进入正式流程)
- [4. 正式流程启动（Step 1）](#4-正式流程启动step-1)
- [5. 需求澄清登记（Step 2）](#5-需求澄清登记step-2)
- [6. 产品审（Step 4 Part 1）](#6-产品审step-4-part-1)
- [7. 技术审（Step 4 Part 2）](#7-技术审step-4-part-2)
- [8. 拆分决定（Step 3 前半）](#8-拆分决定step-3-前半)
- [9. 路线确认（Step 3 后半）](#9-路线确认step-3-后半)
- [10. 开发（Step 5）](#10-开发step-5)
- [11. 固定当前快照（Step 6）](#11-固定当前快照step-6)
- [12. 开发后 QA 与门（Step 7）](#12-开发后-qa-与门step-7)
- [13. 修复与继承判定（Step 8）](#13-修复与继承判定step-8)
- [14. Seal（Step 9）](#14-sealstep-9)

## 场景

`tdo` 是一个命令行任务清单工具，仓库用 Git，已有 `tdo add`、`tdo done`、`tdo
list`。本次请求：「给 tdo 增加 `tdo archive` 子命令，把已完成任务归档到独立文件；
再让 `tdo list --archived` 能查看归档内容」。

## 1. 请求与受理

1. 收到创建、编辑、移动或删除项目内容的请求时，默认提醒一次：若需走 formal-gates 流程，
   可直接提出。当需求过大、过复杂（规模、耦合、风险、验证复杂度足以超过普通直接处理的
   能力）时，在需求澄清与方案确认完毕、准备开做之际再额外强调一次：检测到复杂需求，建
   议走 formal-gates 流程。主代理不得自行触发正式流程。非修改性提问、解释、诊断、review不触发。
2. 用户明确要求走正式流程（或明确要求触发 formal-gates）时，照读 `SKILL.md` 开发受
   理流程操作。
3. 本步不读 `references/formal-flow.md`——受理阶段只决定是否进入正式流程（是 / 否），
   命令形式后面才需要。现在只按 `SKILL.md` 开发受理流程操作。

**默认提醒分支**：普通请求提醒一次（「若需走 formal-gates 流程，可直接提出」）后按常规
方式直接开发，不建 run、不问询、不审；大需求在需求与方案确认完毕、准备开做时额外强调
一次（「检测到复杂需求，建议走 formal-gates 流程」），用户仍不选择也按常规方式直接处理。
已有正式 run 进行中时的行为由主代理按情境决定。

**走正式流程分支**：用户明确要求时，进入完整受理流程。继续。

## 2. 需求澄清与确认（受理阶段）

按 `SKILL.md` 开发受理流程第 2-4 条执行：

1. 检视工作区，确定自己能直接判定的事实：先看 `tdo` 现有子命令、数据文件格式、已
   有测试，别让用户替你查。
2. 逐维拷问所有有实质影响的细节：目标动机（归档而非删除的意图）、用户场景、范围边
   界/非目标（是否动 `list` 默认行为）、细节与默认值（归档文件路径与格式）、异常与
   失败路径（归档文件不存在时）、验收标准（`--archived` 列出什么）、约束与取舍。记
   录拷问轨迹与「考虑过但放过」项（放过须有理由且不构成实质猜测）。一次只问一个有
   实质影响的决策，用日常语言讲清背景与选项；次要实现细节自行决定。
3. 返回 PASS 前自检「是否还有任何有实质影响的细节是我不得不猜的」；有就继续问。
4. 无待澄清问题时，显式说「无待澄清问题」，再呈现完整整合后的需求与技术方案，单独
   发起确认，等用户明确确认。「提出方案」或给出推荐都不等于确认。
5. 用户明确确认后，评估工作量——规模、耦合、风险、验证复杂度——并询问用户是否进
   入正式流程（是 / 否）。需求确认与「是否进入正式流程」的询问不得合并。

## 3. 是否进入正式流程

1. 询问用户是否进入正式流程（是 / 否）并给推荐、解释理由。本例请求跨两个子命令、改
   数据布局、需要验证，推荐「是」。
2. 路线不在这里确认：`full`/`custom` 在正式流程的拆分决定之后确认，单一 run 确认一
   次；`lightweight` 不经拆分决定与路线选择（在正式流程 start 时以 `--route lightweight`
   声明，不验证、只留记录）。本阶段只定「是否进入正式流程」——轻量选项在受理阶段不
   出现，它是正式流程内的路线，不是受理阶段选项。
3. 用户选「是」：把已确认的完整需求与技术方案持久化到环境中任一稳定文档格式（本
   例写一个需求文件），再进入正式流程。
4. 用户选「否」则到此为止：不建流程状态、不建正式任务切片，直接按常规方式开发，其
   余步骤全部跳过。

## 4. 正式流程启动（Step 1）

1. 先读 `references/formal-flow.md`「启动与 run 控制」，拿到 `workflow start` 的确切
   命令形式（基线解析与采纳外部改动也在该节）。
2. 用 Git 作外部 VCS，冻结原生基线标识。不指定 `--base-snapshot` 时，CLI 把当前 HEAD
   解析为基线。
3. 运行：
   ```bash
   formal-gates workflow start --root <tdo 仓库> --package-root <formal-gates 包> \
     --run-id tdo-archive-001 --flow formal --requirement <需求文件> \
     [--requirement-artifact <方案文档> ...] --vcs git --split no
   ```
   登记主需求和所有对应的需求/方案文档。不支持无 VCS 的正式开发流程。
4. 生命周期检查点：`workflow show` 确认 run 已启动、需求已登记、状态为预期初始值。

## 5. 需求澄清登记（Step 2）

1. 先读 `references/formal-flow.md`「需求、拆分决定与路线」。
2. 本步只登记受理阶段已对齐的需求（需求文件已在受理阶段写入），不重新澄清。运行
   `workflow prepare-action --action requirements-clarification` 产出登记任务，由主代理
   直接执行（不派零上下文子代理）。
3. 取得用户最终确认后，用
   `workflow record-action --action requirements-clarification --status PASS` 登记，
   与 `requirement --confirmed` 绑定。
4. 检查点：`workflow show` 确认该动作是 PASS、需求文件被 run 引用。
5. 之后需求被修订时按三分支分类：meaning-preserved → 保留既有 PASS、快照重绑新修订
   （开发开始后须同时传 `--confirmed`）；meaning changed → 作废全部结果、回需求澄清
   重新登记；需求修订**或 start-readiness FAIL 时**，黑盒 QA 用例增量修订——只增删改
   确实受影响的用例（未受影响保持 PASS）。

## 6. 产品审（Step 4 Part 1）

1. 出现发现项时读 `references/formal-flow.md`「开发之前」的处置/重审机制；Part 2 双
   速调度与快速路径细节也见该节。
2. 派发独立产品审代理：`workflow prepare-action --action product-review`。用全新零上
   下文审查者审已实例化并确认的需求文档（承接需求细节澄清）：需求本身是否合理、需
   求细节是否合理。
3. 认领时机：host 启动审查者后，立即 `workflow claim-dispatch --dispatch <id>
   --reviewer <host-agent-id>` 绑定 host 身份——这是生命周期检查点：claim 会先检查生
   命周期模块，要求 start 与 stop 事件配对。
4. 发现项分级 P0/P1/P2/P3，是候选输入不是终态。逐项转达用户处置：用户接受 → 该发现项
   作废、不阻塞、不改需求；用户认同问题 → 按指示修订需求/方案。用户已拍板的发现项
   不再重提。
5. 是否重审：没审出问题 → 直接通过；认同的问题里不存在 P0/P1 → 修订后不重审、直接
   进入下一步；存在 P0/P1 → 修订后重审。产品审不产生终态 FAIL，需求是否成立由用户
   决定。
6. 记录：`workflow record-action --action product-review --status PASS`（有候选发现项
   待处置时记 FAIL + 发现项）。Part 1 全部通过后进入 Part 2。

## 7. 技术审（Step 4 Part 2）

1. 承接技术方案选择与对齐。运行 `workflow prepare-action --action start-readiness`，
   派发全新零上下文审查者，`claim-dispatch` 认领（生命周期检查点同上）。
2. 处置与重审规则同产品审；start-readiness 的 FAIL 发现项同样只是候选输入。若
   start-readiness FAIL 触发黑盒 QA 用例增量修订，按第 5 步的修订规则执行。
3. 记录：`workflow record-action --action start-readiness --status PASS`。

## 8. 拆分决定（Step 3 前半）

1. 完整拆分流程读 `references/sliced-runs.md`；命令形式见 `references/formal-flow.md`
   「需求、拆分决定与路线」。
2. start-readiness PASS 后记录拆分决定。拆分建议对所有正式 run 必填呈现并留痕：拆分
   理由、如何拆、哪些子任务可并行、改拆后果说明；仅高置信要拆时才需用户确认拆分方
   案。命令：`workflow slicing --decision no-split --note '<原因>'`。
3. 本例 `archive` + `list --archived` 是大而连贯的单一 run：拆太细会让子任务小至不值
   得单独走完整流程，拆太粗会把可独立实现验收的逻辑单元塞进一个子任务，而这里都没
   有。所以 `no-split`，留痕原因。
4. 检查点：拆分决定记录后即为绑定点、不重切。分片 >= 2 时整体级产品审/技术审已足够，
   切片继承整体结果、不重跑（本示例不分片，后续均为单 run 行为）。

## 9. 路线确认（Step 3 后半）

1. 拆分决定之后确认路线，时序见 `SKILL.md` 正式流程开头。运行
   `workflow route-candidates` 查看可选：full = 黑盒 QA + 白盒 QA + 全部已发现门；
   custom 可任意组合黑盒 QA、白盒 QA 与各门，至少选一项。
2. 主代理给出统一默认推荐，用户拍板：`workflow route --mode full`。
3. 开发开始之后不能再加入 QA；custom 未选中的部分获得路线跳过授权，Seal 时不会补做。

## 10. 开发（Step 5）

1. 先读 `references/formal-flow.md`「开发与快照」，拿到 `prepare-action
   development-worker` 的确切命令形式、认领放宽与快照门机制。
2. 运行 `workflow prepare-action --action development-worker`，把输出作为完整任务发给
   独立开发工作者代理；任务内容与「派发即原样转发」等派发规则见 SKILL.md「独立派
   发」。
3. worker 启动后 `workflow claim-dispatch` 认领（生命周期检查点）。
4. 任务中断后可用 `workflow resume` 继续同一个 PREPARED 任务，只重新组装提示词，已
   记录的开发开始边界不变。

## 11. 固定当前快照（Step 6）

1. 先读 `references/formal-flow.md`「开发与快照」。
2. 实现完成后用 Git 创建不可变标识，运行
   `workflow snapshot --dispatch <development-dispatch-id>` 记录它；快照门机制与
   worker 已提交 HEAD 时认领放宽容纳该情形的细节见该节。
3. 生命周期检查点：snapshot 会先检查生命周期模块（claim 与 stop 已配对），通过后才
   改变状态。

## 12. 开发后 QA 与门（Step 7）

1. 先读 `references/formal-flow.md`「开发后审查」；分片 >= 2 时的合并验证细节也见该
   节（本示例不分片）。
2. 黑盒 QA 用例在隔离工作区按**增量**设计：`qa-design` 只返回变更（新增不带 id、修改
   用 `--case-id`、删除用 `--remove-case`、整体替换用 `--replace-all`），未提及的既有
   用例与其 PASS 状态自动保留；每轮记录后用例镜像到隔离工作区
   `.gates/cases/blackbox.md`（主干封板前不可见）。`qa-review` 审查完整合并集（提示词
   注入本轮变更清单）。
3. 并行准备：`workflow prepare-action --action qa-execution` 和对每个已发现门
   `prepare-gate --gate <id>`。开发后阶段并行推进黑盒 QA 执行、白盒 QA 与各门审查。
4. 每个门都用全新零上下文审查者，`claim-dispatch` 认领（生命周期检查点）。门任务组
   装、QA 审查者输入、`compared` 快照对校验等规则见 SKILL.md「独立派发」「结果校验
   与修复上限」与 formal-flow「开发后审查」。
5. 记录：`workflow qa-execution --case-result ...` 与
   `workflow record-gate --gate <id> --status PASS --compared <base>..<current>`。
6. 状态询问只能询问进度，并明确告知审查者继续直到所有分配检查完成。

## 13. 修复与继承判定（Step 8）

1. 先读 `references/formal-flow.md`「继承判定、修复授权与 Seal」。
2. 出现 QA FAIL 或 P0/P1 门发现项时，整个轮次退回修复，此时同轮的 P2/P3 一并处理；
   仅含 P2/P3 时同样进入修复（轮次预算内不问用户、修复不重审不计轮次，任务写明要求
   最高强度自测）；轮次上限、授权与修复规则见 SKILL.md「结果校验与修复上限」及第 8 步。
3. 派发独立门审/QA 前，先用目标项目自己的构建/测试做便宜健全性检查，明显失败如编译
   不过直接退回修复、不派发子代理；无校验入口则跳过。
4. 修复继承判定按 `references/formal-flow.md`「继承判定、修复授权与 Seal」判定：修复
   范围不影响任何此前已通过的被选验证 → 主代理直接做继承判定
   （`workflow carry --main-agent --main-reason '<原因>'`）；否则对每个此前通过的被选
   门单独派发继承判定（`prepare-action --action carry`）。
5. 修复完成后重新冻结快照并重跑审查，进入下一轮；继承判定与修复本身同样走认领与生
   命周期检查点。

## 14. Seal（Step 9）

1. 先读 `references/formal-flow.md`「继承判定、修复授权与 Seal」。
2. 汇总前后各确认一次当前 VCS 标识，再 `workflow seal --run-id tdo-archive-001`；只有
   当每个被选中的结果都通过、或已获得允许的授权之后才 Seal。Seal 跳过与跳过授权规
   则见该节与 SKILL.md 第 9 步。Seal 同时把已批准黑盒用例从隔离工作区落盘合并回主干
   `.gates/results/tdo-archive-001.blackbox-cases.md`（本 run 的黑盒用例交付物）。
3. Seal 后该快照的结论即最终。中断的 run 用 `workflow resume` 继续；需要作废则
   `workflow abort`。
