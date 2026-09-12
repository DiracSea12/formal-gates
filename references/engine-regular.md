# regular 受理、开发、验证与 Seal

公开入口从单文件受理推进到已确认需求、Batch 计划、no-split 拓扑与 full/custom 路线，
随后完成开发、Git、QA、唯一验证 wave、统一返修和候选 promotion/Seal。
已有 lightweight typed receipt 路径继续可重放。

运行时使用 schema 3、definition 5。旧 schema/definition 的 run 不迁移续写；历史
9 步纯协议 fixture 仍用于原协议测试，生产定义生成自 `RegularWorkflow()`。

完整载荷与可编码示例见本文末尾的 [事件字段参考与隔离前态示例](#事件字段参考与隔离前态示例)。

## 公开入口

在已完成 registry/bootstrap 的安装入口运行：

```sh
formal-gates workflow start --root <project> --run-id <id> --vcs git --requirement requirements.md
formal-gates workflow drive --root <project> --run-id <id>
formal-gates workflow show --root <project> --run-id <id>
formal-gates workflow next --root <project> --run-id <id>
formal-gates workflow diagnose --root <project> --config
```

start 只登记一个项目相对文件和当前 Git HEAD。drive 完成纯结构预检。确认前不选择
mode、route、topology 或调用模型。`regular.requirement` 包含全文、精确 revision 和
REQ/AC 投影；每次确认/修订都检查实际完整文件。初次确认同时登记 `requiredSources`，
默认是该正式需求文件，可在确认 REQUEST 中明确提供有限来源列表。技术计划确认绑定其
solution revision。配置见 [用户配置](user-configuration.md)。

唯一外部写入口是 `workflow submit`，stdin 传既有 Event union 的 REGULAR 变体：

```json
{
  "ID": "user-request-1",
  "Kind": "REGULAR",
  "regular": {
    "operation": "REQUEST",
    "expectedRevision": 2,
    "request": {"action": "confirm-requirement"}
  }
}
```

Event 外层沿用已有 `ID`/`Kind` 字段。REQUEST 生成
`regular.ask`，确认再提交 DECIDE，绑定 `requestId`、当前 `token`、`digest` 和
`choice: confirm`。每次从 show 读取当前 revision/token；同 ID 同内容重放返回原回执，
旧 revision、新内容复用 ID、错误绑定均拒绝。初次确认后 REQUEST `mode`，`mode` 值为
`regular` 或 `lightweight`；后者直达 UNVERIFIED，drive 自动终结清理。

regular 顺序：产品审 → Operator 裁决/必要的用户 finding 决定 → 技术审 → 裁决 →
Batch 计划及 no-split 原因确认 → full/custom 路线确认。拓扑 REQUEST 携带当前
`planDigest` 和 `reason`；路线 REQUEST 携带 `{kind, qa, gates}`。full 展开为 blackbox、
whitebox 和该 run `catalogGates`；custom 至少选择一个 QA 或普通门。含 QA 时原子激活
需求保证；没有 QA 时保持 INACTIVE。技术审的每个 Batch 必须有 ordered subtasks、依赖、
接口、状态边界、风险、DoD、验证、恢复、交接和上下文成本。


宿主由已注册安装目标选择，而不是 workflow start 的任意 provider 参数。Codex、
WorkBuddy、ZCode 和 DSH 的 regular 执行共用 `workflow drive --continuous`；规范
provider ID 分别为 codex、workbuddy、zcode、deepseek-harness，安装别名 dsh 不作为
可改写的 action 身份。安装能力不等于原生完整执行能力；缺少所需能力时启动前拒绝。
其他已安装但没有完整 runtime adapter 的宿主不能作为 native executor 被静默接受。

## 原生审查与结果

```sh
formal-gates workflow drive --root <project> --run-id <id> --action-id <issued-action-id>
formal-gates workflow drive --root <project> --run-id <id> --action-id <issued-action-id> --resume
```

支持已签发的 product-review/start-readiness、development-worker 及 blackbox/whitebox
的 design/review。adapter 在创建原生进程前通过同一
State/CAS 认领 action、TaskKey、Attempt、provider、需求 revision、candidate、payload
和前置审查 digest。未知、重复、过期、取消、不匹配及 guard 阻塞均在进程创建前拒绝。
开发前审查读取完整文档并禁用 shell、MCP、web 和子代理。开发和白盒设计只在登记的
隔离 workspace 编辑文件；QA Review 只读。角色工具权限由公共合同约束，禁用任意
子代理、外部 MCP 与 web；仅开放当前动作绑定的 engine 工具，Git 写入由 adapter
执行。各宿主的原生启动观察绑定身份，进程和输出回执用于中断对账。

resume 先检查登记 PID 和输出；仍运行则拒绝再调用，已有答案直接结算，没有身份/进程
回执时显示 UNKNOWN。明确已停止且有身份时最多认领一次同 thread 的原生 resume，
再次中断由已有结果或 RUNTIME_ERROR 结算，不无限恢复。

未完成 Batch 的正常返回使用 `CONTINUE`，自测失败使用 `FAIL`，并保留实际剩余项、
验证与 manifest。后续 attempt 的 `claim.operation` 为 `resume`，必须使用上述
`--resume` 入口；正常返回、RUNTIME_ERROR、授权增加 attempt 或重新确认改编号的计划
均不自行解除原生身份，也不重置已用额度。显式 executor-handoff 是另一项用户控制：
它保留同一未完成 TaskKey 的历史并授权新的 executor/provider/identity。完成 Batch 后的
增量优化则签发新的 TaskKey；两者都不能用旧会话身份冒充新执行者。

开发恢复先核对旧进程已停止及开发工作区 HEAD/工作视图，再实际执行 Codex
`thread/resume` 查询；查询只加载指定身份，不启动新任务。查询成功时继续原会话。
只有原生明确返回该身份不存在的 `no rollout found for thread id ...`，engine 才
签发绑定当前 action、身份和原文的单次替换 intent。超时、限流、网络错误和 UNKNOWN
保留原身份，按有限恢复/人工处理继续；宿主自填原因不能代替 adapter 原生回执。

Raw candidate 保留原文与 digest。REVIEW_RESULT 不替代 Operator：ADJUDICATE 必须逐条
提供 finding ID、VALID/DISMISSED、单独严重度理由、类别与正常使用证据。技术 P0/P1
只接受需求冲突、职责/依赖或验收入口类别；implementation-detail 最高 P2。
CORRECT 绑定当前 result digest、finding ID、旧/新 decision、旧/新 severity、revision
和 reason；冲突拒绝，历史保留，新增有效阻塞使依赖失效。

产品/技术 series 各自默认最多三次有效语义审查，当前失败额度为每个 TaskKey 三次。
开发正常 CONTINUE 保留已用 attempt 但不消耗失败额度；额外授权按该次 typed Ask 计算。
不完整、无效或 RUNTIME_ERROR 不增加语义次数。OPEN 重用当前任务，明确 review-again
才请求新审查；达到上限后 authorize-round 只加指定 action 一轮，authorize-retry 只加
当前 exhausted TaskKey 一次。改变需求、保留 PASS 或 adopt 不重置计数。

## 修订、控制与止损

所有控制由 REQUEST → typed Ask → DECIDE：

- requirement-change（origin USER_INITIATED_CHANGE/REVIEW_FINDING）立即暂停业务补位；
  apply-requirement 携带同一文件的新完整 Document、meaning、两个审查 action 的
  affected/unaffected/unknown impact 和 reason。keep 只能引用已有有效 PASS。
- REVIEW_FINDING 修订必须提供完整 Delta，逐 hunk 唯一 old/new 文本，source 格式为
  `action:resultDigest:findingId`，绑定已确认 finding，附验证 reason。合成后必须精确
  等于新文档。保义变更保留产品 PASS；其他受影响/unknown 结果回到新审查；已确认且
  完整解决的 P2/P3 可保留而不消耗新语义轮次。
- finding 请求当前 resultDigest/findingId，选择 confirm/dismiss/clarify。review-again、
  authorize-round、authorize-retry 指定 reviewAction 和 reason。
- route-add/route-reselect 在开发前重新确认路线；jump 只允许产品/技术审节点，不能跳过
  前置，必须先结算现有执行。reset 保留需求、base、原始结果、计数、成本与历史，清除
  当前计划/拓扑/路线/保证并重开审查，运行中执行先对账。
- adopt-external 绑定当前 Git HEAD `identity`、原始 base `lineage`，以及
  `git diff --binary <old-candidate> <new-head> --` 字节的 `sha256:` digest；要求 original
  base 是新 HEAD 祖先，显式影响分类后使受影响结果失效。原 base 不变。需求正文变化
  必须走需求流程，不能借 adopt 绕过确认。
- abort 登记 TERMINATE_AGENT intent，drive 终止本 run 创建的进程，结算 source/cost，
  写 terminal summary/receipt 后清理运行资源。UNKNOWN 不猜测成功；公开 cleanup 不删除
  活动 engine run。终态 show/drive 从只读 summary 重放。

SOURCE 与 REVIEW_RESULT 可先后到达。结果完成而 source 未结算时 COST_PENDING：
提交按结果/receipt → source resolve/backfill → usage → guard → refill 顺序，在一次
现有 persistence CAS 中提交；参与计算的 transcript 字节在写前/写后重验。停止来源
验证 session_meta 身份；确实不可用才用有原因的 UNAVAILABLE。复用 internal/cost，
没有第二本计量账。cap 到达后 continue Ask 只授权当前按容量裁剪的一批 TaskKey，逐项认领即消费。

## B1 验证与后续接入

`internal/engine/protocol/regular_test.go` 覆盖顺序、重复回放、source/cap、独立 retry、
并发认领、九个持久化故障窗口及终止历史；`facade/regular_test.go` 覆盖文件漂移与
lightweight 终态重放。`internal/cli/regular_test.go` 用隔离安装的真实 CLI 走完整 B1，
默认事件为明确标记的测试事实；设置 `FORMAL_GATES_REAL_CODEX=1` 后产品审调用真实 Codex。
原生新会话和进程中断后的同身份恢复位于 `facade/codex_test.go`，同样显式 opt-in。

B2 通过此 State/Event/TaskKey/Attempt/CAS 和原生认领边界接入准备流水线；确定性
外部义务列表由 guard 重算额度。下游 QA execution、wave、门和 Seal 的当前接口见下文。源码交付、正式 Seal 和实际全局发布按各自时点记录。

实跑原生身份、绑定、source 与计量摘要存于
[原生执行证据](phase4-b1-native-evidence.json)。该文件明确区分真实 native/安装 CLI
证据与确定性合同测试；它不是正式 QA 结论。恢复证据使用独立进程组，包含启动器及实际
原生进程的停止检查。


## 用户指定的执行者交接

未完成的开发 Batch 可以通过 `REQUEST executor-handoff` → typed Ask → `DECIDE confirm/cancel`
明确交给另一执行者。`target` 必须是 show 中当前未完成的 development-worker actionId；
`handoff.to` 为 `{kind, provider, label}`。`kind=native` 选择实际具备原生运行能力的宿主，
目标的配置和环境由其 adapter 捕获并校验；能力不足在写入 Ask 前拒绝，确认交接前再次
校验，拒绝时不签发目标执行动作。`kind=external` 仅表示
用户明确指定外部开发者（例如由当前对话直接编辑），不是完整自动平台支持，不能用于
独立产品审、技术审、黑盒/白盒或门审，也不会自动替代不可用的原生平台。

```json
{"operation":"REQUEST","expectedRevision":0,"request":{"action":"executor-handoff","target":"<当前开发 actionId>","reason":"<用户的明确交接决定>","handoff":{"to":{"kind":"external","provider":"chatgpt","label":"当前对话通过远程工具直接开发"}}}}
```

示例中的 revision/actionId 必须从本次 show 取得；这只是 REGULAR 事件的正文，仍使用
原信封提交，不自行编造 TaskKey 或 identity。程序在 Ask 中绑定当前 workspace view、
旧动作与来源、目标能力和影响；确认前不得领取新执行者，确认时工作区或目标环境漂移则
拒绝。先停止并结算旧执行者的实际调用及在途操作；交接不伪造失败，不重写旧结果或成本，
不使产品/技术审、早先 Batch 或独立 QA 默认失效。用户确认只授权这个目标的一次新执行，
不清零重试历史。旧动作的迟到结果不能覆盖新动作。

原生目标取得新的 provider/session，之后的正常续用仍保持这个新的身份；不要求另一个
平台复用旧平台的不可移植 session。外部开发目标通过：

```sh
formal-gates workflow drive --root <root> --run-id <run> --action-id <新签发的 action> --external-executor
```

领取并获得精确 binding、workspace、完整结构化任务 input 和结果 schema。结构化任务与
原生模型 prompt 使用同一个材料组装入口，包含当前 Batch、有序子任务和完整已确认需求。
`previousDevelopmentResult` 提供原动作链上最近的真实开发结果（没有则为空）；交接中被
替代但未执行的动作不会遮住已有 CONTINUE/FAIL 与交接事项。该历史结果仍保留旧 provider、
identity 和绑定，新执行者只使用当前 `binding` 提交自己的结果，不复制旧身份冒充续用。
外部开发者不要求宿主原生模型提示词配置，也不补造旧 run 不存在的提示词绑定。重复读取已领取输入不
新建动作；Ask、需求暂停或终态期间不继续。外部 identity 由 engine 生成，明确不是已观察
的原生会话，不提供虚构的 native spawn、transcript 或 token；计量标为 unavailable。
开发者按正常 PREPARATION_RESULT 返回实际 manifest、逐子任务验证和交接，CLI 校验工作区
全部变动，再由既有 Git intent/commit/snapshot 推进。开发结果不能替代独立 QA 或 Seal。

## 开发与 QA 准备（B2）

路线确认后调用 drive 登记并创建 detached Git 工作区。开发从当前精确候选开始，黑盒
工作区始终来自原始基线。开发与黑盒设计同时签发；可同时运行各已签发 action-id，容量
由 engine 裁剪。每批是一个全新原生会话，Subtask 按顺序留在同一会话。结果以
PREPARATION_RESULT 回传路径 manifest、逐 Subtask 的实际验证和不可从提交恢复的交接。
每个结果都绑定原 action/attempt/provider/requirement/candidate；原生结果和成本结算后
才允许补位。后续 drive 执行 status/diff/untracked → manifest 校验 → track → commit →
ancestry/snapshot。上一批提交后才签发下一批，批间没有代码门或 repair wave。

Batch 接口、依赖或 DoD 变化时使用 REQUEST `plan-change` + reason，确认后旧拓扑和
计划确认失效，重新技术审和计划确认；旧动作不能再执行。Git 操作使用已有 intent 与
独占 adapter 锁，重启对账同一 operation ID；提交已经完成时从唯一提交 trailer 和
parent/tree 恢复，不重复提交，不自动采纳未知 HEAD。

QA 的设计合同复用 coverage：requiredSources、manifest、map 和 source/point/case
逐项 review。标准 AC 使用 `BLACKBOX:AC-001` / `WHITEBOX:AC-001` 作为 point ID，
case ID 同样带 mode 前缀；每个 AC 必须从正式需求 source 连到一个批准 case。
case 的 `mode` 必须为 `BLACKBOX` 或 `WHITEBOX` 并与 review kind 一致，不能写自由描述。
其他明确 NON_QA 来源须有协议支持的验证方式和 PASS 证据。集合 PASS 不能补齐逐项决定。
共享步骤按 ID 审一次；修改 case 只作废该 case，修改共享步骤作废所有引用者。

QA 可选 P2/P3 建议通过 REQUEST `qa-suggestion`，携带 target（blackbox/whitebox）、
findingId、meaning（adopted/deferred/not-adopted）及 reason，再确认。全部处置后才能
FROZEN；采纳须落实修改并重审。需要修改时 REQUEST `qa-revise` + target/reason，保留
不受影响的逐项审批。真实 QA FAIL 停在 QA_REVISION_REQUIRED，不降格为建议。
`reviewScope` 首次为 FULL，已审用例修订后为 AFFECTED；State、派发 payload 和结果共同
绑定这一审查范围。它不表示后续 QA execution 的执行范围。
无效结果/运行错误有限重试，耗尽后 `preparation-retry` 的 target 绑定精确 TaskKey，
确认只增加一次 attempt；token-cap continue 沿用同一容量和次数限制。

全部开发完成且黑盒批准后，drive 从精确 DevelopmentSnapshot 创建白盒工作区。
白盒先判断覆盖、检查并独立运行已有有效测试，再补缺口。当前 Go resolver 复用
`go list` 的测试发现、测试 embed 与 testdata、生产编译/嵌入输入；unsupported 项目
返回 UNKNOWN。文件名包含 test 不足以归属；被生产 embed 的 fixture 也不属测试。
全部增删改及 mode/blob 参与完整 view 比较。仅 TEST_ONLY 才 track/commit，生产变化或
UNKNOWN 停在 PRODUCTION_CHANGED；`development-rework` 确认后回开发形成新 Dn。
白盒 Review 读取完整测试候选，统一审查开发者和 QA 测试质量。

所有选定 QA FROZEN 后，drive 把批准设计物化为
`.gates/results/<run-id>.<mode>-cases.json` 并提交，创建最终候选并在必需验证前 squash。
无白盒或无 QA 路线同样经过候选装配。`regular.preparation.candidate` 包含精确 identity、
canonical tree/blob/mode view、原基线/Dn/计划绑定、完整 coverage、批准 whitelist、
VCS 内 artifacts、VCS 外审批/建议/复用证据的 externalArtifacts digest，以及门审排除的
全部 testOwned 路径。缺开发提交、批准 case-set 或物化文件时不产生 reviewReady。
该冻结候选是唯一 wave 的输入；Seal 不再补文件。

黑盒有效审查 FAIL 不等待开发完成：前两次连续 FAIL 自动把完整原设计与逐项审查缺口
交回同模式设计，再派发 fresh 审查；开发中的原会话继续运行。第三次连续 FAIL 或同一
QA TaskKey 默认三次运行错误/无效结果额度耗尽后，立即返回 `qa-recovery` typed Ask，包含
`qa.mode/task/requirementRevision/candidate/caseSetDigest/resultDigest/consecutiveFailures/retryExhausted`。
黑盒与白盒连续 FAIL 计数独立；PASS 清除此模式连续失败数，运行错误不算语义失败。

该 Ask 的 DECIDE choice 为 `redesign`、`fresh-review`、`change-requirement`、`waiver`、
`skip`、`abort`；运行错误耗尽还提供 `retry`。每次处置必填非空 reason，并绑定当前
revision/token/digest。retry 仅追加当前 TaskKey 一次 attempt，再失败重新 Ask。
运行错误时 redesign/fresh-review 也只能消耗这一次追加 attempt，不能另换 TaskKey
规避额度。无已形成用例集时 fresh-review/waiver/skip 不合法；Ask 中的 caseSetDigest
明确绑定当时输入。waiver/skip 只豁免这个精确 pre-wave case-set 的审查，保留原 FAIL、
不增加批准 whitelist、不产生执行 PASS；后续仍逐例执行，需求保证状态显式为 WAIVED。
修改需求和 abort 进入共同的隔离/终止、来源结算及清理路径。

[B2 原生准备记录](phase4-b2-native-evidence.json) 保存两批开发与黑盒 design/review 的
真实会话、生命周期重叠区间、Git receipt 和 review-ready 候选。该验证的受理及产品/
技术审前态使用显式 fixture；开发、黑盒设计/审查与最终候选命令/测试来自实际执行。


## 验证 wave、checkpoint 与返修

候选准备完成后自动建立 `regular.verification.waves`。每个 wave 保存完整候选、需求
revision、不可修改的 `targets` 与 `expectedDigest`。QA 的 `scope` 为 FULL/AFFECTED，
普通门的 `reviewScope` 始终 FULL；门读取原始 base 至当前候选的完整实现 diff，排除
QA 用例工件与已登记测试归属文件。按 capacity 同时签发 QA execution 与普通门；单个
FAIL 不终止独立目标。由 `workflow drive --action-id` 执行已签发任务。

`VERIFICATION_RESULT` 通过既有 REGULAR event 回传 `verificationResult`，必须包含
`actionId/attemptId/provider/identity/requirementRevision/candidate/bindingDigest`。
QA 每项包括 `caseId/status/procedure/output/comparison/environment/tool/workspace`、
`dependencies` 与可选 `reusedPreparation`。独立用例使用干净副本；只阻止失败用例的
显式依赖者。PASS/FAIL 的 cases 必须与 target.expected 精确对账，remaining 为空。
每个失败用例有对应原始 finding，其 `requirement` 为失败 case ID。

中断结果使用 `status: CHECKPOINT`、已完成 cases、剩余 `remaining` 和中断原因
`message`；已完成 PASS/FAIL 保留，不增加 attempt，不结算未结束任务的 cost。
同一绑定恢复时原条目保持不变，只补剩余项。多次中断继续同一 native identity/attempt，
各次 native resume receipt 分开保留。实际 tool/environment 变化时，结果的 `invalidated`
逐项填写 `caseId/previousTool/previousEnvironment/tool/environment/reason`；程序验证精确
旧绑定，保留 `checkpointHistory` 中的原 FAIL，再运行受影响项及其依赖，未影响项继续复用。
候选、需求或 case/shared-step 改变仍通过前述绑定失效路径处理。结果缺项、重复、未知或无法解析走同一
TaskKey 的有限错误重试。source 到达后，按既有结果→source→cost→guard→refill 原子顺序结算。

完整 wave 收齐并结算来源后，Operator 提交 `WAVE_ADJUDICATE`，payload 为
`waveAdjudication: {waveId,target,resultDigest,decisions}`。decisions 对每条 finding
提供 `findingId/decision/problemId/severity/reason/evidence/affectedCases`；decision
只接受 NEW、SAME_AS、DISMISSED。SAME_AS 引用已存在的稳定 problem ID。纠正提供
`replacesRevision`，历史裁决和原始结果保留。范围外项只能是 P3。

确认后的 QA FAIL、P0/P1 和门审 P2 自动汇成唯一 repair。`repair-worker` 在登记的
隔离 worktree 执行；REPAIR_RESULT 绑定 `planId`，报告全部路径 manifest、resolved
problem ID、真实 build/projectTests，以及固定 affectedCases 的逐项实跑输出。
结果与 source 均结算后由 adapter 提交新候选。普通修复保持 QA FROZEN，采用 AFFECTED；
本轮必需 QA 与门全部有效 PASS（含合法继承）后即可封板，不因保证路线额外补 FULL。纯门审 P2 使用结构化 P2_CARRY 继承原 gate PASS，不新增 wave。

默认最多三次完成且裁决结束的普通验证 wave。额外 wave 使用 REQUEST `authorize-wave`，
显示完整当前 findings/problems、历史、范围、候选和 guard，并在 request.target/keep
绑定精确 wave 与 obligation IDs。其 DECIDE choice 恰好为 `additional-wave`、`retry-qa`、
`waiver`、`change-requirement`、`abort`，每次提供非空 reason。additional-wave 仅授权
一轮额外修复；retry-qa 仅授权一次当前候选的 QA 重验，保留未纠正的原义务；下一次仍有
义务时重新 Ask。waiver 单独记录精确候选、需求和问题 IDs，不伪装原执行 PASS。
决定绑定整个 preview digest 与 state revision，不能累计或挪用授权。`verification-retry` 的 target 必须是已耗尽的精确 TaskKey，仅增加一次
attempt；普通 continue 只授权当前 token-cap 下的已选任务，不新增验证范围。

## 非终态控制、保留与 promotion

requirement-change 立即使旧 wave 失效，撤销未完成 promotion，暂停业务签发；已隔离
任务的迟到结果保留为 quarantine。apply-requirement 的 keep 可引用已有产品/技术审、
`blackbox-review`/`whitebox-review`、`blackbox`/`whitebox` 执行或普通门 PASS。
保留决定与需求修订在同一事务内记录完整 USER_KEEP：原 PASS、原需求/候选、确认时的
实际目标候选、新需求及 state revision 均已绑定，不留下待填 candidate。后续仅物化
已授权需求及 QA 绑定文件时，finalize 原生回执必须证明其余 tree 和 case 内容未变，
才以独立 IMPACT_CARRY 绑定实际新 Git 候选；原 USER_KEEP 不改写。一般返修候选不能
消费这个物化授权。

原始实际执行与 `verification.inheritance` 中的 USER_KEEP/IMPACT_CARRY 分开显示；
每条授权只绑定本次新需求及新冻结候选。新增没有原 PASS 的用例继续验证。
开发输入可在 impact 中以 `development: unaffected` 明确保留已提交批次。

开发开始后 route-add 只允许追加普通门；不新增 QA、不移除义务。目标集改变建立新 wave，
旧 expected-set 保留原样。验证后 qa-revise 还须以 meaning 声明 `acceptance`、
`public-behavior` 或 `stable-execution`；对应 QA 设计、审查与候选重新绑定。

无开放义务、完整验证与最终保证满足后，adapter 提升整个已验证候选。Git 常规路径
fast-forward 保持 identity；目标已拥有完全相等的已提交验证 view 时，保留目标的不同
identity 并生成实际原生等价 receipt。外部采纳后的候选以明确采纳 identity 为 Git 父基线，
门审仍比较原始 base 至候选的完整 diff。所有等价映射 receipt 都核对原/目标 identity、预期 target
base、完整 tree/blob/mode/provider view、operation ID、ref、无冲突状态和相等摘要。
目标漂移、冲突、UNKNOWN 或不等价不签发 Seal。恢复先对账原生 HEAD 和已持久化 receipt。
Seal 只绑定最终候选、原执行/继承证据与 VCS 外产物摘要；完成后自动清理登记资源，
中断继续使用现有终态 intent/replay。

[发布时点与隔离演练](phase4-release.md) 和 [历史回归清单](phase4-release-regression.md)
分别记录源码、Seal、安装候选与实际全局发布，不能用某一时点代替另一时点的验证。

[B3 原生验证与返修记录](phase4-b3-native-evidence.json) 明确区分准备/裁决夹具与真实
QA、门、repair、来源结算、Git 和最终命令。该记录证明实际 FAIL 后的并发收集、返修及
新候选重验，不代表本阶段权威 Seal 或全局发布。

## 事件字段参考与隔离前态示例

本节是现有 regular 接口的使用契约。JSON 字段区分大小写；除注明的数字、布尔、数组、
对象外均为字符串。空列表使用 `[]`。下表列出可提交字段；程序计算的输出字段注明为
“回执”，调用者省略它们。`workflow show` 返回 Run；`workflow submit` 返回
`{run: Run, ...}`，下一次提交要用新 Run 的 revision。使用者不需要读取实现文件。

### 信封、动态绑定和执行边界

每个事件的信封固定为：

```json
{"ID":"client-unique-id","Kind":"REGULAR","regular":{"operation":"REQUEST","expectedRevision":2,"request":{"action":"confirm-requirement"}}}
```

`ID` 非空且每个新事件唯一；同 ID 同字节语义用于重放。`expectedRevision` 是 show 的
`revision`（正整数），不是需求文档 revision。`regular` 中除 operation、expectedRevision
外只放下表对应的一个载荷，不能同时放 request 和 decision。所有字符串 ID、digest、
原生 identity 都要从当前绑定复制，不能拿旧 run 的数值替换。

| operation | 唯一载荷键 | 产生者/用途 |
| --- | --- | --- |
| REQUEST | request | 用户/Operator 请求控制或确认 |
| DECIDE | decision | 对当前 typed Ask 的选择 |
| CLAIM | claim | 已签发执行的宿主认领；原生入口自动完成 |
| SOURCE | source | 原生身份、运行结束及成本来源观察 |
| REVIEW_RESULT | review | 产品审/技术审原始结果 |
| ADJUDICATE | adjudication | 产品审/技术审逐 finding 裁决 |
| CORRECT | correction | 产品审/技术审既有裁决纠正 |
| PREPARATION_RESULT | preparationResult | 开发、黑盒/白盒设计和审查结果 |
| VERIFICATION_RESULT | verificationResult | wave 内 QA execution/普通门结果或 checkpoint |
| WAVE_ADJUDICATE | waveAdjudication | 当前完整 wave 逐 finding 裁决/纠正 |
| REPAIR_RESULT | repairResult | 唯一修复计划的结果 |
| GIT_CLAIM | gitClaim | Git adapter 认领当前已签发副作用 |
| GIT_RECEIPT | gitReceipt | Git adapter 实跑后的回执 |
| DEVELOPER_RECOVERY | developerRecovery | adapter 对已停止开发会话的原生恢复观察 |
| TASK_OPERATION_CLAIM | taskOperation | 原动作内部 model/声明 QA 操作准入 |
| TASK_OPERATION_RECEIPT | taskOperationReceipt | adapter 实际内部操作的观察回执 |

正常原生执行使用 `workflow drive --action-id <id>`；续跑按原 claim 的 operation 使用
`--resume`。这会实际创建/恢复进程并提交 CLAIM、SOURCE 与结果。外部事件可用于宿主集成
及明确标记的隔离协议前态；手填 fixture 不是原生执行证据。GIT_* 与 DEVELOPER_RECOVERY
虽是同一事件格式，公开 CLI 使用者通过 drive 取得其实际回执，不用伪造回执推进真实项目。

| 所需值 | show 中的来源 |
| --- | --- |
| 最新提交版本 | `revision` |
| 当前确认 | `regular.ask` 的 `id/token/digest/options/revision/request`；复杂控制还有 preview，QA 恢复还有 qa |
| 已签发执行 | `executions` 中相应 `claim.task` 且 `finished=false` 的条目；复制完整 claim |
| 任务/action/attempt | `executions[键].claim` 的 `task/actionId/attemptId`；task 是 `node/step[/scope]` 字符串 |
| 原生身份 | 同条目的 `identity` 或实际 `thread.started` 观察；不能从 actionId 推导 |
| 需求 | `regular.requirement` 的 `path/text/revision/projection/confirmed` |
| 审查原结果及 digest | `regular.preDevelopmentReviewSeries[action]` 的 `currentDigest/rawCandidates` |
| 技术计划 | `regular.developmentPlan` 的 `digest/requirementRevision/batches` |
| 路线/普通门 ID | `regular.route` / `regular.catalogGates` |
| QA 来源与绑定 | `regular.requiredSources.binding/sources`；现有设计为 `regular.preparation.qa[mode].design.manifest.binding` |
| QA 设计/审查工作区 | `regular.preparation.resources`；开发为 development，QA 为 blackbox/whitebox |
| QA 审查范围 | `regular.preparation.qa[mode].reviewScope` |
| wave 与 expected | `regular.verification.current` 对应 `waves` 的 `id/targets/expectedDigest` |
| 目标绑定 | 对应 target 的 `bindingDigest`，也等于执行 claim 的 `prerequisiteDigest` |
| wave 原结果 | 当前 wave 的 `results[target]` 与其 `digest/findings` |
| 修复计划 | `regular.verification.repairs` 中与 `claim.prerequisiteDigest` 相等的 `id` |
| Git operation | `regular.preparation.operation`；回执见 preparation.receipts |

展示 Ask 后若其他事件使 revision 改变，应重新 show/drive 取得当前 Ask；不要自行改写旧
Ask 的 token。检查 `next`、`availableActions`、`regular.guard` 后推进，只认领已签发项。
提交成功可能只是保存无效结果或 quarantine；还须查看回执和 Run 中的错误/终结投影，
不能把 exit 0 等同于业务 PASS。

### REQUEST 与 DECIDE

request 全部可用字段为 `action/reason/reviewAction/findingId/resultDigest/mode/route/
planDigest/document/meaning/origin/impact/keep/delta/requiredSources/target/external`。
按下面对应动作填写适用字段，其余省略。除 confirm-requirement、mode、初始 route 外，
示例调用均提供非空 reason；topology 也必须提供 no-split 理由。

| action | 附加字段、合法值及前置条件 |
| --- | --- |
| confirm-requirement | 预检完成且尚未确认；可给 requiredSources 数组，省略则只有正式需求文件 |
| mode | 已确认需求；mode=`regular` 或 `lightweight`，只能初次选择 |
| topology | 两审有效 PASS；planDigest=当前 plan.digest，reason=不拆分理由；本阶段只有 no-split |
| route | topology 已确认；route 对象见下文 |
| route-add / route-reselect | route 对象与 reason；开发开始后只支持追加普通门，不能新增 QA 或减少义务 |
| finding | reviewAction=`product-review`/`start-readiness`，resultDigest=当前 digest，findingId=已经 VALID 裁决的 finding |
| review-again | reviewAction、reason；当前 series 有语义额度 |
| authorize-round | reviewAction、reason；当前 series 语义额度已经耗尽，只加该动作一轮 |
| authorize-retry | reviewAction、reason；该动作当前 TaskKey 已耗尽，只加一次 attempt |
| continue | 当前 guard 为 TOKEN_CAP；reason；仅批准 preview 中按容量选出的当前任务 |
| requirement-change | origin=`USER_INITIATED_CHANGE`/`REVIEW_FINDING`，reason；立即暂停受影响补位 |
| apply-requirement | 先进入 requirement-change；document、meaning、origin、impact、reason，可带 keep/delta |
| jump | target=`product-review`/`start-readiness`；不得越过前置；先结算已有任务及 source |
| adopt-external | external、impact、reason，可带 keep；精确新 HEAD 保持原始 base 祖先关系 |
| plan-change | 当前有计划且 Git operation 已对账；reason；使计划/拓扑确认失效 |
| qa-suggestion | target=`blackbox`/`whitebox`，findingId=未处置建议 ID，meaning=`adopted`/`deferred`/`not-adopted`，reason |
| qa-revise | target=QA mode，reason；候选已形成时 meaning 只能是 acceptance/public-behavior/stable-execution |
| development-rework | 当前 preparation.blocked=PRODUCTION_CHANGED 或 PRODUCTION_FAILURE；reason |
| preparation-retry | target=耗尽的准备 TaskKey 字符串，reason；仅加一次 |
| verification-retry | target=耗尽的验证/修复 TaskKey 字符串，reason；仅加一次 |
| authorize-wave | 当前完整 wave 已收齐、source 已结算、逐项裁决完成、已用完语义额度且仍有义务；reason。可带 target=current wave.id、keep=精确问题 ID 数组；省略时程序填入当前精确集合 |
| reset / abort | reason；通过既有对账/终止/清理收敛 |

route 为 `{kind,qa,gates}`：kind=`full` 时展开为两类 QA 和当前 catalogGates；kind=`custom`
时 qa 为 `blackbox`/`whitebox` 数组，gates 为 catalogGates 中 ID 数组，至少选一个。

requiredSources 的每项为 `{sourceId,category,applicability}`；category 为
PRODUCT_REQUIREMENT/SOLUTION_CONSTRAINT，applicability 为 QA/NON_QA。sourceId
是已确认有限来源，不接受虚构来源来缩小正式需求的 AC 义务。

document 为 `{path,text,revision}`，path 必须还是已绑定文件；revision 为 text 的完整
UTF-8 原始字节 SHA-256 小写十六进制（无 `sha256:` 前缀）。实际文件须包含同一全文。
输出中的 projection/confirmed 由程序生成，不作为确认输入。
meaning=`preserved`/`changed`；impact 是 action→unaffected/affected/unknown 对象，至少
明确产品审和技术审；可含 development、blackbox-review、whitebox-review、blackbox、
whitebox 及普通门。keep 是要保留的既有有效 PASS 的动作/目标名数组；不是任意 result ID。
REVIEW_FINDING 变更还须 delta 数组，每项 `{old,new,source,reason}`；old 在当前文本中
恰好出现一次，按数组顺序替换后的全文等于 document.text，source 为
`<reviewAction>:<resultDigest>:<已确认 findingId>`。
external 为 `{identity,lineage,diffDigest}`：identity 是实际新 Git HEAD，lineage 是
regular.baseSnapshot；diffDigest 是 `git diff --binary <old-candidate> <new-head> --`
原始输出字节的 SHA-256，带 `sha256:` 前缀。

decision 全部字段为 `{requestId,token,digest,choice,reason}`，前三项复制当前 ask。
常规 choice=confirm/cancel；finding=confirm/dismiss/clarify；qa-recovery 由程序直接产生，
提供 redesign/fresh-review/change-requirement/waiver/skip/abort，运行错误耗尽还提供 retry。
authorize-wave 的 choice 恰为 additional-wave/retry-qa/waiver/change-requirement/abort。
恢复处置的 decision.reason 必须非空，不能仅填写原 request.reason。以实际 ask.options
为当前可选集合，仍须满足相应 case-set、候选、额度条件。

### CLAIM、SOURCE 与共同结果绑定

claim 的完整字段为 `actionId/attemptId/task/provider/requirementRevision/candidate/
payloadDigest/operation`，可含 `prerequisiteDigest/identity/context`。`context` 由宿主 adapter
在首次认领时捕获，调用者不能另选或替换。后续从 executions 复制整个
对象，provider 是已签发的宿主规范 ID，operation 由签发结果给出（spawn/resume；
原生不可恢复时的 replace 由 adapter 控制，显式外部开发为 external）。调用者不编造
TaskKey、payloadDigest 或新身份。

source 为 `{actionId,attemptId,provider,identity,status,transcriptPath?,reason?}`。
status 为 RUNNING/STOPPED/UNAVAILABLE。RUNNING 绑定实际会话身份；STOPPED 提供完整
可读 transcriptPath，程序核对 session_meta 身份并读取用量；UNAVAILABLE 必须给非空
reason，已有 identity 不能改变。没有原生启动身份的失败可在 UNAVAILABLE 中留空 identity。
结果和终止来源可先后到达，source 未结算时可能 COST_PENDING；CHECKPOINT 不结束来源。

四类 worker 结果（review、preparationResult、verificationResult、repairResult）都以
`actionId/attemptId/provider/identity/requirementRevision/candidate` 六字段绑定已经 CLAIM
且已观察身份的执行。下文将这六字段称为 B。`message` 保存结论/错误说明。
结果 status 的正常闭集如下；RUNTIME_ERROR 是实际运行错误报告，按有限 attempt 处理，
不冒充语义 FAIL。原生输出里的 `NATIVE_IDENTITY` 占位由 adapter 换成观察身份，手动
SOURCE/结果不能把它当作真实 identity。

### 产品审、技术审及裁决

review = B + `action/status/message/complete/findings`，技术审还含 granularityReview。
action 为 product-review/start-readiness；status=PASS/FAIL；complete 必须为 true 才能
作为完整语义结果；FAIL 必须至少包含一条 finding。`digest/quarantined` 为回执字段，由程序计算。

finding 的全部字段为 `{id,severity,category,requirement,problem,normalPath,consequence,
severityRationale,remedy}`。id 在该结果内唯一；severity=P0/P1/P2/P3，分别写明问题、
正常使用复现、实际后果、定级理由与解决建议。技术审 category 仅允许 requirement-conflict、
responsibility-dependency、acceptance-entry、implementation-detail；最后一类最高 P2。
普通门/QA 范围外 finding 用 category=out-of-scope 且只能 P3。

granularityReview 为 `{batches,rationale,requirementRevision}`；digest/confirmedRevision
为程序回执。每个 Batch 为 `{id,task,subtasks,dependencies,interfaces,stateBoundary,risks,
definitionOfDone,validation,recovery,handoff,contextCost}`，subtasks 为有序
`[{id,task}]`，dependencies 只引用当前计划中的其他 Batch ID；无跨批依赖时填 `[]`。
同批 Subtask 的先后由有序 subtasks 表达，不把 Subtask ID、环境前提或说明句放入
dependencies。原生 review 的输出 schema 固定本次 claim 的 actionId、attemptId、
provider、action、requirementRevision 和 candidate；identity 仍填 `NATIVE_IDENTITY`
并由原生传输填入实际身份。错误绑定仍按原规则拒绝。所有文字都描述实际规格；Batch ID
唯一、依赖无环，至少一个 Batch/一个 Subtask，不能用空计划取得技术 PASS。

adjudication 为 `{action,resultDigest,findings,completenessEvidence}`，revision 为回执。
findings 与原结果逐条一一对应，每项 `{findingId,decision,severity,category,reason,evidence}`；
decision 仅 VALID/DISMISSED。即使 findings=[] 也提交非空 completenessEvidence。
reason 单独解释定级，evidence 是核实的正常使用证据；技术 category 必须与原项一致。

correction 为 `{action,resultDigest,findingId,oldDecision,newDecision,oldSeverity,newSeverity,
reason}`；revision 为回执。old 值要与当前有效裁决一致，newDecision 为
VALID/DISMISSED/RESOLVED，newSeverity 为 P0–P3；保留原结果和纠正历史。

### 准备结果、覆盖设计与逐项审查

preparationResult = B + `status/message/manifest`，按任务附以下字段；不适用字段省略。
manifest 是实际增加/修改/删除的项目相对路径数组，不是 QA coverage ID。只有开发与
白盒设计可有文件路径；黑盒设计和 QA Review 为 []。

| 执行 | 结果字段与要求 |
| --- | --- |
| development-worker | status=PASS/FAIL/CONTINUE；validation 为逐 Subtask 检查数组，handoff 为不可从提交恢复的剩余事项，没有则明确写无；PASS 必须完整覆盖 Batch |
| blackbox-design | status=PASS/FAIL；design 为覆盖设计 |
| whitebox-design | status=PASS/FAIL；design、reuse；manifest 仅测试所有权修改，生产变化/UNKNOWN 进入返工处置 |
| blackbox-review / whitebox-review | status=PASS/FAIL；review 为逐项审查，suggestions 为 P2/P3 可选建议数组 |

validation 每项 `{subtaskId,procedure,observation,result}`，result=PASS/FAIL；procedure
为实际命令/操作，observation 为实际输出或观察，不能只填写预计会通过。

design 为 `{coverageJudgment,manifest,alternatives}`。coverageJudgment 说明独立覆盖判断。
其 manifest 为 `{binding,sources,points,cases,edges,sharedSteps}`：

| 字段 | 完整嵌套结构 |
| --- | --- |
| binding | `{runId,childId,requirementRevision,solutionRevision,reviewKind,routeScope,topologyScope}`；childId 无切片为 `""`，前四项复制 requiredSources.binding；reviewKind=BLACKBOX/WHITEBOX，topologyScope=当前 topology |
| sources | sourceId 字符串数组 |
| points | `[{pointId,observableBehavior,oracle}]`；每个正式 AC 使用 `<MODE>:AC-001` 形式 |
| cases | `[{caseId,mode,oracle,command,dependencies?,sharedSteps,publicEntry?,preconditions?,steps?,testRef?}]`；mode=BLACKBOX/WHITEBOX，与审查种类相同；黑盒提供公开入口/前态/步骤，白盒提供唯一 testRef，caseId 以 mode 加冒号开头 |
| edges | `[{sourceId,pointId,caseId,reviewKind?}]`；明确来源→验收点→用例关系，reviewKind 省略时取 binding |
| sharedSteps | `[{id,procedure}]`，case.sharedSteps 引用这些 ID；不用则 [] |
| alternatives | `[{sourceId,reason,method,status,evidence}]`，仅明确 NON_QA 来源，status=PASS 且有实际非 QA 验证；普通 AC 不可转入此数组 |

首次 design.binding.routeScope 是当前 route 的规范摘要：按 `kind,qa,gates` 顺序序列化
对象，UTF-8、两个空格缩进、末尾换行、不把 `<>&` 转义，SHA-256 加 `sha256:` 前缀。
后续设计优先直接复制公开已有 design.manifest.binding。其他候选/结果/plan 摘要均读取
程序输出，不自行重算。

reuse 每项 `{caseId,testRef,coverageJudgment,procedure,observation,comparison,result}`，
result=EXECUTED_PASS/EXECUTED_FAIL；表示白盒独立判断并实际运行测试后的观察，不能只列文件名。
有效测试发现生产行为不符时，whitebox-design 返回 status=FAIL、具体 message 和完整 design/reuse，
至少一条已声明用例须有 EXECUTED_FAIL 及实际 procedure/observation/comparison。引擎接纳
失败证据并停在 PRODUCTION_FAILURE，不冻结候选；经既有 development-rework 请求和确认
回到开发。status=PASS 仍要求全部 EXECUTED_PASS，不得修改有效断言强行通过。
review 为 `{binding,scope,sourceDecisions,pointDecisions,caseDecisions,sharedStepDecisions}`；
binding 复制对应 design.manifest.binding，scope 复制公开 reviewScope。每个 decisions
数组为 `[{id,status}]`，status=PASS/FAIL，逐项覆盖当前需要审查的 ID，共享步骤只审一次。
首次 FULL 覆盖全部；AFFECTED 可复用未变化的已批准项。可选诊断字段
unboundSources/unboundPoints/unboundCases 是 ID 数组，setStatus 为集合诊断状态
（PASS/FAIL/PENDING/UNKNOWN/AUTHORIZED_SKIP）；这些字段都不能代替缺失逐项 PASS。
suggestions 每项 `{id,severity,problem,cases,changesAcceptance}`，severity=P2/P3，cases
为相关 caseId 数组，changesAcceptance 为布尔。decision/reason 是建议处置后的回执。

### 验证结果、checkpoint、wave 裁决与修复

verificationResult = B + `{bindingDigest,status,message,cases,remaining,findings,invalidated?}`；
status=PASS/FAIL/CHECKPOINT，bindingDigest 复制 claim.prerequisiteDigest，digest/quarantined
是回执。每个 cases 项的完整字段为 `{caseId,status,procedure,output,comparison,environment,
tool,workspace,dependencies,reusedPreparation?}`，status=PASS/FAIL/BLOCKED；dependencies
为显式依赖 caseId 数组。procedure/output/comparison 是实际步骤、输出、与 oracle 比较，
workspace 标明独立干净副本；tool/environment 标明实际执行版本和条件。reusedPreparation
只描述可复用共享前置，待测入口本身仍实跑。BLOCKED 必须有本结果中的失败/阻断依赖，
独立失败不能传播给其他用例。

PASS/FAIL 的 cases 精确等于 target.expected，remaining=[]；每个 FAIL case 有 finding
且 finding.requirement 等于该 caseId。普通门 expected=[]，cases=[]，只返回完整门审
findings。CHECKPOINT 仅用于 QA，cases 与 remaining 无重复并精确合为 expected，保留
已完成条目；续跑沿同一个 action/attempt/identity，不把已完成 FAIL 抹成未执行。
invalidated 每项 `{caseId,previousTool,previousEnvironment,tool,environment,reason}`：
真实工具/环境变化时精确引用旧项并重跑，程序保存原证据至 checkpointHistory，未影响项
保持原条目。候选/需求/用例定义改变走既有输入失效路径，不借环境失效绕过。

waveAdjudication 为 `{waveId,target,resultDigest,decisions,replacesRevision?}`，revision 为
回执。只有整个当前 wave 完整且来源结算后才可提交；target 从该 wave.targets 取值，
resultDigest 从该 wave.results[target].digest 取得。decisions 与每个 raw finding 精确
对账，项为 `{findingId,decision,problemId,severity,reason,evidence,affectedCases}`；decision
是 NEW/SAME_AS/DISMISSED。NEW 给新的稳定 problemId，SAME_AS 引用已有 problemId，
DISMISSED 可省略 problemId。affectedCases 是冻结候选内的 caseId 数组，不扩充 QA。
修订同一目标的裁决时，replacesRevision 必须等于上一条对应 wave/target 裁决的 revision。

repairResult = B + `{planId,status,message,manifest,resolved,build,projectTests,cases}`。
planId=claim.prerequisiteDigest；status=PASS 才能完成修复，FAIL/RUNTIME_ERROR 进入有限
错误恢复。manifest 是实际文件改动，resolved 是该计划全部 problem ID；build 与
projectTests 均为前述 BatchValidation 对象，必须实际 PASS。cases 与 plan.affectedCases
精确对账，使用前述 CaseExecution 字段且全部 PASS。drive 随后执行真正 Git 提交并绑定
新候选；不手填新候选 identity 来替代提交。

### adapter 回执的完整形状

以下字段供宿主集成读取/转交已经发生的操作；普通使用者调用 drive 生成。对象使用的
operation ID、before、bindingDigest、目标及路径必须精确匹配当时签发的 operation。

- gitClaim：`{revision,id,kind,bindingDigest,before,target,paths,claimed}`，原样复制
  preparation.operation；paths 为相对路径数组，revision 为数字，claimed 为布尔。
- gitReceipt：`{operation,resources?,git,ownership?,testOwned?,artifacts?,promotion?}`。
  operation 是上一对象；resources 是资源名→绝对路径对象；testOwned 为测试路径数组，
  artifacts 是工件路径→digest 对象。
- git：`{operationId,before,after,paths,view,ownership?}`；after 是真实原生提交身份。
- view：`{providerMetadata,identity,tree,entries,digest}`；providerMetadata 是字符串映射，
  entries 为 `[{path,mode,type,object}]`，分别是 Git 路径、模式、对象类型、对象 ID。
- ownership：`{status,paths,productionDigest,reason?}`；status 按实际归属为
  TEST_ONLY/PRODUCTION_CHANGED/UNKNOWN，不由文件名猜测。
- promotion：`{operationId,expectedTargetBase,source,target,ref,status,conflictFree}`；source/
  target 为完整 view，ref 是真实目标 ref，status=COMPLETE 且 conflictFree=true 才可接受，
  路径、blob、mode、metadata、digest 都必须等价。
- developerRecovery：`{claim,identity,stoppedAction,processId,workspaceView,nativeMethod,
  nativeResponse,transportError?}`。claim 复制当前开发 claim；processId 为已登记进程的
  正整数，workspaceView 为实测工作视图，nativeMethod=`thread/resume`，nativeResponse
  是原生 JSON 响应对象（不能写成转义字符串）。transportError 保存实际传输错误。
  同身份 result.thread.id 证明可恢复；只有原生 `no rollout found for thread id <id>`
  才能证明该身份不可恢复，其余错误/超时为 UNKNOWN。由 drive 执行检查及一次性替换认领。

### 最小可编码示例：以公开入口建立隔离前态

在专用 Git 测试项目和隔离 home/config/state/resources 中按
[安装与准入](install-and-hooks.md) 完成该精确安装包的安装/bootstrap。以下假定 FG 是
该安装回执登记的 launcher 绝对路径，ROOT 是已经 bootstrap 的独立 Git 项目，RUN 是新
run ID。脚本只调用公开 CLI，不复制或编辑 `.gates` 状态。项目的已提交 requirements.md
至少采用以下可预检结构（这里是示例规格，不是对真实任务的审查结论）：

```markdown
## 需求点
### REQ-001：hello 命令
#### 要求
命令输出 hello 和一个换行。
#### 验收条件
- AC-001：执行命令，标准输出严格等于 hello 加换行。
#### 来源
隔离协议示例。
```

以下 Python 是调用方小脚本，不是新增产品命令。每次操作重新读 revision；示例里给
所有确认附理由。实际用户决定由宿主收集后传给 choose，不能在真实任务里自动替用户确认。

```python
import hashlib, json, os, subprocess, uuid
FG, ROOT, RUN = os.environ['FG'], os.environ['ROOT'], os.environ['RUN']
def cli(sub, *args, body=None):
    p = subprocess.run([FG, 'workflow', sub, '--root', ROOT, '--run-id', RUN, *args],
                       input=None if body is None else json.dumps(body),
                       text=True, capture_output=True, check=True)
    return json.loads(p.stdout)
def show():
    return cli('show')
def send(op, key, value):
    r = show()
    e = {'ID': str(uuid.uuid4()), 'Kind': 'REGULAR', 'regular': {
        'operation': op, 'expectedRevision': r['revision'], key: value}}
    return cli('submit', body=e)['run']
def choose(request, choice='confirm'):
    r = send('REQUEST', 'request', request)
    a = r['regular']['ask']
    return send('DECIDE', 'decision', {'requestId': a['id'], 'token': a['token'],
        'digest': a['digest'], 'choice': choice, 'reason': 'Isolated example decision'})
cli('start', '--vcs', 'git', '--requirement', 'requirements.md')
cli('drive')
choose({'action': 'confirm-requirement'})
choose({'action': 'mode', 'mode': 'regular'})
```

如需构造产品/技术审之后的高成本隔离前态，可在这个新测试 run 使用明确的 fixture
身份；真正要验收的原生启动、安装、Git 或命令步骤必须实际运行。下面只声明隔离协议
前态的审查事实，不能带进正式项目作为独立审查结果。

```python
def execution(step):
    xs = [x for x in show()['executions'].values()
          if x['claim']['task'].split('/')[1] == step and not x['finished']]
    assert len(xs) == 1
    return xs[0]
def attach_fixture(step):
    c = execution(step)['claim']
    send('CLAIM', 'claim', c)
    src = {k: c[k] for k in ('actionId', 'attemptId', 'provider')}
    src.update(identity='isolated-fixture:' + c['actionId'], status='RUNNING')
    send('SOURCE', 'source', src)
    b = {k: c[k] for k in ('actionId', 'attemptId', 'provider',
                          'requirementRevision', 'candidate')}
    b['identity'] = src['identity']
    return c, src, b
def settle_fixture(src):
    send('SOURCE', 'source', dict(src, status='UNAVAILABLE',
         reason='Declared isolated protocol precondition; no native transcript'))
for step in ('product-review', 'start-readiness'):
    c, src, b = attach_fixture(step)
    raw = dict(b, action=step, status='PASS', complete=True, findings=[],
               message='Declared isolated precondition for the hello example')
    if step == 'start-readiness':
        raw['granularityReview'] = {
            'requirementRevision': c['requirementRevision'],
            'rationale': 'One command behavior', 'batches': [{
                'id': 'B1', 'task': 'Implement hello',
                'subtasks': [{'id': 'S1', 'task': 'Implement and check output'}],
                'dependencies': [], 'interfaces': 'Command stdout',
                'stateBoundary': 'One executable', 'risks': 'Incorrect output',
                'definitionOfDone': 'AC-001 satisfied', 'validation': 'Run command',
                'recovery': 'Restore previous Git commit', 'handoff': 'No unresolved items',
                'contextCost': 'One coherent command responsibility'}]}
    send('REVIEW_RESULT', 'review', raw)
    settle_fixture(src)
    series = show()['regular']['preDevelopmentReviewSeries'][step]
    send('ADJUDICATE', 'adjudication', {
        'action': step, 'resultDigest': series['currentDigest'], 'findings': [],
        'completenessEvidence': 'Declared full hello-spec review precondition'})
r = show()
choose({'action': 'topology', 'planDigest': r['regular']['developmentPlan']['digest'],
        'reason': 'One command behavior needs no split'})
choose({'action': 'route', 'route': {'kind': 'custom', 'qa': ['blackbox'], 'gates': []}})
cli('drive')  # Real adapter creates development and original-base blackbox workspaces.
```

到此可以直接用 `cli('drive','--action-id',c['actionId'])` 实跑所选已签发任务。若待测操作
在更后面，开发前态也必须在 preparation.resources.development 中真正写入代码、执行
build/test、报告精确 manifest 与 validation，再以 drive 实际提交；不能捏造 GIT_RECEIPT。
QA 的第一份设计绑定可按下例从公开值构造（Go 的规范 route 摘要保留这个字段顺序）：

```python
r = show()['regular']
route = {k: r['route'][k] for k in ('kind', 'qa', 'gates')}
route_bytes = (json.dumps(route, ensure_ascii=False, indent=2) + '\n').encode()
binding = dict(r['requiredSources']['binding'], childId='', reviewKind='BLACKBOX',
    routeScope='sha256:' + hashlib.sha256(route_bytes).hexdigest(),
    topologyScope=r['topology'])
design = {'coverageJudgment': 'AC-001 covers the exact public output', 'alternatives': [],
    'manifest': {'binding': binding, 'sources': [r['requirement']['path']],
      'points': [{'pointId': 'BLACKBOX:AC-001', 'observableBehavior': 'Command stdout',
                  'oracle': 'hello plus newline'}],
      'cases': [{'caseId': 'BLACKBOX:C1', 'mode': 'BLACKBOX', 'publicEntry': 'go run .',
                 'preconditions': 'Go available; isolated candidate copy',
                 'steps': 'Run command and compare stdout', 'oracle': 'hello plus newline',
                 'command': {'executable': 'go', 'args': ['run', '.'], 'expectedExitCode': 0}, 'dependencies': [],
                 'sharedSteps': []}],
      'edges': [{'sourceId': r['requirement']['path'], 'pointId': 'BLACKBOX:AC-001',
                 'caseId': 'BLACKBOX:C1'}], 'sharedSteps': []}}
c, src, b = attach_fixture('blackbox-design')
send('PREPARATION_RESULT', 'preparationResult', dict(b, status='PASS',
     message='Declared isolated design precondition', manifest=[], design=design))
settle_fixture(src)
```

对应 review 复制 design.manifest.binding、公开 reviewScope，sourceDecisions 给需求文件
PASS，pointDecisions 给 BLACKBOX:AC-001 PASS，caseDecisions 给 BLACKBOX:C1 PASS，
sharedStepDecisions=[]、suggestions=[]，由 PREPARATION_RESULT 提交并结算 SOURCE。
FAIL 前态只把实际需要表达的逐项 status 改为 FAIL，同时整体 status=FAIL；不漏填其他
独立项。完成开发与 QA 两侧屏障后 drive 冻结候选并签发 wave。

wave 的最小结果形状如下；B、bindingDigest、caseId 必须取本次执行，procedure/output/
comparison/environment/tool/workspace 必须换成待测操作的实跑观察。存在多个 expected
时逐个填写，不可只交一个代表项。

```json
{"actionId":"<claim.actionId>","attemptId":"<claim.attemptId>","provider":"codex","identity":"<observed identity>","requirementRevision":"<claim.requirementRevision>","candidate":"<claim.candidate>","bindingDigest":"<claim.prerequisiteDigest>","status":"PASS","message":"Actual command matched the oracle","cases":[{"caseId":"BLACKBOX:C1","status":"PASS","procedure":"go run . in the clean candidate copy","output":"hello\n","comparison":"Exact match including final newline","environment":"<actual OS/runtime>","tool":"<actual tool version>","workspace":"<actual clean copy>","dependencies":[]}],"remaining":[],"findings":[]}
```

要报告同一例的失败，改 overall/case.status 为 FAIL 并附完整 finding，requirement 为
BLACKBOX:C1；独立目标继续运行。完整 wave 后按上述 WAVE_ADJUDICATE 对该 finding
给 NEW、稳定 problemId 和 affectedCases，再通过真正 repair-worker 或明确的隔离 worker
执行实际修复/构建/受影响测试，提交 REPAIR_RESULT 并由 drive 创建新候选及所需重验。
任何 fixture 的身份与原因都保留在结果/source 中；它只用于省去非待测前态成本。

## 当前命令与旧状态边界

workflow 只保留以下八个子命令，无别名：`start`、`show`、`status`、`next`、`drive`、
`submit`、`diagnose`、`guard-preview`。`drive --resume` 是 drive 的选项。
lightweight 从 `start --intake-receipt <JSON> --route lightweight` 开始，或在单文件受理
确认后选择 lightweight；终态由 drive 推进。

以下 workflow 子命令已删除，统一返回 unknown workflow subcommand，不转发：
`resume`、`abort`、`reset`、`requirement`、`review-delta-preview`、
`requirement-impact-preview`、`authorize-retry`、`route-candidates`、`slicing`、
`settle-findings`、`route`、`route-add`、`qa-worktree`、`prepare-gate`、`prepare-action`、
`claim-dispatch`、`record-action`、`record-gate`、`qa-design`、`qa-review`、`qa-execution`、
`qa-execution-scope`、`qa-thaw`、`qa-suggestion`、`prepare-candidate`、
`prepare-review-wave`、`adjudicate-wave`、`resolve-wave-target`、`snapshot`、`cleanup`、
`future`（及其子命令）、`carry`、`authorize-repair`、`repair-preview`、
`complete-p2-repair`、`seal`。这些名字不再是本版本的状态读写入口。

同名业务控制使用上文 REQUEST/DECIDE 事件和 typed Ask。例如 abort/reset/需求修订/
追加路线/QA 修订/有限授权均以当前 request 合同提交；结果裁决用 ADJUDICATE 或
WAVE_ADJUDICATE。preview 取当前 Ask 的 preview；终态资源清理由 drive 自动完成。
`package route-candidates` 保留为已安装门目录的只读清单，与已删除的
`workflow route-candidates` 不同。包、registry、安装、配置、coverage 纯计算和
脱离 run 的 `gate run|report` 仍保留；脱离 run 的结果不能推进任何正式 run。

旧 run 仅可读取原始诊断：

```sh
formal-gates workflow diagnose --root /absolute/project --run-id old-run
formal-gates workflow diagnose --root /absolute/project --path /absolute/old-state.json
```

没有同名当前 engine 目录时，旧 `--run-id` 先找 `.gates/tmp/<id>/state.json`，无状态时找
`.gates/results/<id>.json`。输出固定为下列结构，raw 是原 JSON 对象；不补字段、不迁移、
不提供续跑建议或写入权限。文件不存在或 JSON 不可读取时命令失败。

```json
{"code":"UNSUPPORTED_RUN_VERSION","path":"/absolute/old-state.json","readOnly":true,"raw":{"runId":"old-run","status":"ACTIVE"}}
```

当前 engine 目录内的状态用 `diagnose --run-id` 读取完整版本诊断；`show/status/next/drive/submit`
只接受当前 engine 信封。旧运行时模式、authority handoff、旧状态 decoder/writer 和
旧 cleanup 均不在安装包中。

## 任务内部 model 与声明 QA 操作

外层 `drive --action-id` 使用 Codex app-server 的原生 JSON-RPC 桥接。真实调用顺序是
`initialize` → `model/list`（仅未显式设置模型时）→ `thread/start` 或 `thread/resume`
→ SOURCE attach → `turn/start` → 原生结果 → SOURCE 结算。未显式设置模型时使用 Codex
`model/list` 中的 `isDefault` 模型和默认 effort；显式 `model/reasoningEffort` 原样传递。

原生线程得到一个动态工具 `formal_gates_operation`：

```json
{"kind":"qa.prepare","caseId":"BLACKBOX:C1","prompt":""}
```

三个字段均须给出。QA execution 与 repair 的 kind 接受 `model.run`、`qa.prepare`、
`qa.target`、`qa.collect`；其他角色的动态 schema 仅公开 `model.run`，caseId 只能为空。
model.run 使用非空 prompt 和空 caseId；QA 三种操作使用 caseId 和空 prompt。
model.run 只接收调用者给出的文本，不隐式继承外层任务、需求文件或结果 schema。调用者须
在 prompt 内提供问题所需的全部正文；缺少正文时返回缺项说明，不通过文件查找补齐。
该文本辅助调用不代替外层已签发审查，也不自行登记正式结果。
bridge 将原生 callId 用作 requestId，复制当前 action 的完整绑定，并走下面同一个 API。
QA execution 线程不提供 shell 工具；目标调用只能消费冻结声明中的 argv。
白盒设计先在禁用 shell 的同一原生身份中，仅根据完整需求和生产源码形成行为/oracle 判断。
adapter 通过 `whitebox.coverage` 内部操作持久化判断回执，之后才续跑原身份并开放已有
可写 authoring 副本和原生 shell 工具发现、编写、实际试跑测试，
将观察记入 reuse；此时尚未冻结用例，不调用 QA 三步，也不产生正式 QA 执行结果。
即使绕过动态 schema 直接请求 QA 三步，该阶段仍在底层调用前拒绝。

首次原生认领由 adapter 捕获执行文件及宿主环境路径，保存在 execution claim 的 `context`。
同 TaskKey 的继续、恢复、transcript 查找及下级模型调用使用该绑定；调用方的环境变化
不覆盖它。不可用的绑定在消费动作前拒绝。新任务仍读取用户可编辑配置。
创建和续跑前读取对应 workspace 的宿主生效配置，逐项禁用普通 MCP server 和插件；
这些配置不能扩充该角色的工具授权。

CLI/API 使用同一 typed 请求：

```sh
formal-gates workflow drive --root /absolute/project --run-id run-1 --operation-request /absolute/request.json
formal-gates workflow drive --root /absolute/project --run-id run-1 --operation-request /absolute/request.json --resume
```

`--operation-request` 与 `--action-id` 互斥。请求文件完整结构：

```json
{
  "runId":"run-1",
  "expectedRevision":42,
  "binding":{
    "actionId":"<executions entry key>",
    "attemptId":"<claim.attemptId>",
    "task":"<claim.task>",
    "provider":"codex",
    "requirementRevision":"<claim.requirementRevision>",
    "candidate":"<claim.candidate>",
    "payloadDigest":"<claim.payloadDigest>",
    "prerequisiteDigest":"<claim.prerequisiteDigest, if present>",
    "operation":"<claim.operation>",
    "identity":"<execution.identity>"
  },
  "requestId":"qa-c1-prepare-1",
  "kind":"qa.prepare",
  "caseId":"BLACKBOX:C1"
}
```

runId 和 expectedRevision 复制本次 show；binding 复制 `executions[actionId].claim`，
并将 identity 设为同一 entry 的 `identity`。task 是字符串，不是拆开的 node/step 对象。
必须是已认领、已 attach、尚未结束且当前候选/需求/QA wave 仍有效的原动作。未签发、
provider/payload/候选不符、超出该动作精确 case set、旧 revision、暂停/取消、重复认领及
已完成步骤均在底层调用之前拒绝。每条内部请求沿用原 TaskKey、Attempt 和 guard；不创建
新任务或自动增加额度。model 的原生 transcript 写入同一 cost 投影，kind=task-operation，
以内部操作 ID 区分来源；不会变成新的派发 attempt。

QA 设计时，每条 regular case 还必须提供：

```json
{"command":{"executable":"go","args":["run","."],"expectedExitCode":0},"dependencies":[]}
```

command 随 case-set 冻结；执行请求中没有 command/argv/env/cwd 字段。executable 按宿主
正常可执行路径解析，args 每项作为独立参数，不作 shell 字符串拼接或插值。裸程序名
不能混入空白分隔的参数，例如 `go test` 须由设计者声明为 executable=`go`、args=`["test"]`；
程序不自动拆写。显式路径可含空格（例如 `./my tool`），冻结前不因当前工具未安装而拒绝
声明；运行环境缺工具仍按运行期失败处理。工作目录由 adapter 选定为该候选的独立 Git
副本。dependencies 可省略或为空；非空时只引用同 manifest
中先前声明的 case ID，引用的 case 必须采集完成且退出码符合其 expectedExitCode。
共享步骤 ID 只放在 sharedSteps，环境可用性写入用例前置说明；两者均不是 dependencies。
原生设计任务的 typed schema 要求该数组字段，独立用例返回 `[]`。

顺序与输出如下：

| kind | 前置 | 实际操作 | 后续 |
| --- | --- | --- | --- |
| qa.prepare | 当前 action/case binding，声明依赖已成功；QA execution 只接受其 expected 集，repair 只接受绑定 plan 的 affectedCases | QA execution 创建精确 candidate 的干净 Git 副本；repair 从已注册修复工作区复制实际修改／新增／删除到独立副本 | 完整副本的观察持久化且 COMPLETED 才可 target |
| qa.target | 本 case prepare 已完成 | 在该副本执行一次冻结 argv，保存 stdout/stderr 与实际 exit code | 进程创建/准备失败则 FAILED；已观察到的正常退出可 collect，非零退出保留为验收数据 |
| qa.collect | 本 case target 已完成 | 读取该次实际 stdout 和单独的 stderr，返回原 exit code 和 workspace | 已完成 stage 不可用新 requestId 重做 |
| model.run | 原动作有效，source/guard 允许，无未对账内部调用 | 调用原动作运行上下文绑定的 Codex exec，禁用 shell/多代理/MCP apps/web，返回文本和原生 identity/transcript | 结果与成本归入原动作；不自行产生正式审查结果 |

repair 同样使用上述三个 kind、原 action/Attempt 和动态绑定，不创建额外任务。修复者先完成
实现修改，再逐 case 准备、执行和采集；返回的 workspace 是含实际修复内容的副本，其
HEAD 仍绑定 repairPlan.candidate，不表示未经提交的修改已成为新候选。prepare 输出列出
原修复工作区及复制路径；缺少完整复制观察的恢复为 UNKNOWN，不重新复制或执行目标。
自测通过后仍须按原流程提交新候选及重验，不能替代正式 QA 结果。

内部 model 的 argv 由 bridge 构造：`codex exec --ignore-user-config --disable shell_tool
--disable multi_agent --disable multi_agent_v2 --disable enable_mcp_apps -c web_search="disabled"
--skip-git-repo-check --json --color never -s read-only -C <operation-dir> -o <answer.txt>`，
如有显式 model/effort 则追加 `-m <model>`/`-c model_reasoning_effort=<quoted value>`，
最后 `-` 从 stdin 接收“仅据所给正文回答、缺输入则说明”的固定边界说明与 prompt。不存在任意命令执行 API。

返回 TaskOperationReceipt：
`{id,actionId,requestDigest,status,workspace?,output?,exitCode,identity?,transcriptPath?,message?}`。
status=COMPLETED/FAILED；失败必须有 message，model 成功包含原生 identity/transcriptPath。
回执保留在 `executions[actionId].operations[id]`，该条目还包含原 request、冻结 command、
claimedRevision 和当时的 checkpointDigest；实际请求先持久化到既有 pending HostAction，再执行。

在 submit 的 REGULAR union 中，`TASK_OPERATION_CLAIM` 对应唯一载荷 `taskOperation`，
结构就是上述完整请求；外层 expectedRevision 与内部 expectedRevision 必须一致。
`TASK_OPERATION_RECEIPT` 对应唯一载荷 `taskOperationReceipt`，结构为上述回执。
它们由 drive/原生 bridge 自动提交。直接只提交 CLAIM 不会执行底层操作；恢复时若没有
已经完成的原生观察，将返回 UNKNOWN。RECEIPT 必须与 adapter 保存的实际观察相同。

最小可编码调用示例（沿用上文隔离前态，已有一个当前已 attach 的 QA execution）：

```python
import json, pathlib, subprocess, uuid
root = '/absolute/project'
run_id = 'run-1'
action_id = '<copy the current attached QA execution action ID>'
request_path = pathlib.Path('/absolute/qa-operation.json')

def current():
    return json.loads(subprocess.check_output([
        'formal-gates', 'workflow', 'show', '--root', root, '--run-id', run_id]))

for kind in ('qa.prepare', 'qa.target', 'qa.collect'):
    run = current()
    execution = run['executions'][action_id]
    binding = dict(execution['claim'], identity=execution['identity'])
    request = dict(runId=run_id, expectedRevision=run['revision'], binding=binding,
                   requestId=str(uuid.uuid4()), kind=kind, caseId='BLACKBOX:C1')
    request_path.write_text(json.dumps(request), encoding='utf-8')
    receipt = json.loads(subprocess.check_output([
        'formal-gates', 'workflow', 'drive', '--root', root, '--run-id', run_id,
        '--operation-request', str(request_path)]))
    if receipt['status'] != 'COMPLETED':
        raise RuntimeError(receipt)
print(receipt['output'], receipt['exitCode'], receipt['workspace'])
```

恢复使用最后一次原请求文件（保留原 expectedRevision），加 `--resume`。已保存观察时只
结算原回执；已完成请求可读取同一回执。无完成观察时返回 UNKNOWN，不发起第二次调用。
普通调用重放或用新 requestId 重复已完成 QA stage 都拒绝。 已完成例因真实 tool/environment 变化需要重跑时，先按既有 VERIFICATION_RESULT 合同提交 CHECKPOINT：invalidated 精确引用旧例和旧新工具/环境及理由，把该例及受影响依赖项放回 remaining，其他完成证据原样保留。接受该 checkpoint 后，每个被明确失效的例可在原动作下重新执行一次 prepare→target→collect；无失效声明或同一 checkpoint 的重复准备仍拒绝。若 UNKNOWN 无法经原生事实
对账，应使用当前 run 的正式控制/恢复路径处置，不改写 request 或 state 冒充新调用。

## 独立计数和中断窗口取证

以下是使用方可观察的公开产物，不是专门的计数/暂停测试接口：

- 外层任务目录：`.gates/engine/<run>/executions/<actionId 去掉 act:，斜杠换下划线>/`。
  有 `process.json`、原生 `stdout.jsonl`、`answer.json`；resume 位于对应 resume 子目录。
- 内部操作目录：`.gates/engine/<run>/task-operations/<id 去掉 hact:task:>/`。
  有 request.json、process.json、stdout.log、stderr.log，以及成功观察后的 observed.json。
- Git 操作 ID/claim 在 `regular.preparation.operation`；完成观察保存在
  `.gates/engine/<run>/git-operations/<id 去掉 git:sha256:>.json`。
- Codex 原生 transcript 路径由 SOURCE 或内部 model 回执返回。使用其 session_meta.id、
  thread/turn ID 和 task_complete 计数；重复镜像按原生 ID 去重。记录调用前后的原生来源
  集合，并使用独特的测试 prompt 标记关联本次 model 请求。仅统计本次已隔离输入。

目标程序的独立计数由其原生副作用确认。例如，对“在隔离仓库创建一个空 commit”的
已声明 QA 目标，可冻结 `git -c core.hooksPath=/dev/null commit --allow-empty -m <独特标记>`。
记录 prepare 回执的 workspace，再比较该仓库 Git log/reflog 中该标记的 commit 数。
成功路径应增加一次，非法/重复/前置失败请求前后增量应为零。这个计数来自真实 Git，
不能只用 bridge 自报的 claimed/receipt 数量代替。普通 QA 目标也可使用其公开可查的实际
产物或原生系统记录作独立计数；没有这种来源时明确说明计数证据缺口。

“原生调用成功、正式 receipt 未持久化”的命中证据必须同时保留：原生 Codex
`task_complete`/`turn/completed` 成功与对应 identity；尚未 Finished 的该 action，或尚无
receipt 的该内部 operation；以及终止 driver 的 PID/时间和上述原始文件。Git 的对应
窗口必须保留实际 HEAD/父提交/`Formal-Gates-Operation: <id>` trailer，且该 operation
仍 claimed、尚无 Git receipt。恢复后比较原生 identity/turn 集合或 Git commit 集合，
证明确为对账原成功结果，没有新增调用/提交。

可以用操作系统的进程/文件事件观察与终止能力捕获这个窗口；本版本没有公开的“停在
receipt 前”开关，也不保证轮询必然命中。只观察到认领、attach、运行中或已有完整 receipt
都不构成命中；未采到上述同时成立的原始证据就记录未命中，不推断成功。原生成功日志
已落盘而 observed.json 尚无时，内部 model/目标操作的 resume 会依据原 process 完成
记录、原生日志和输出文件恢复；证据不足维持 UNKNOWN。Git 恢复依据已存在的原生 commit
trailer、父关系、manifest 与快照，不重复 commit。

### 持续消费公开入口

`formal-gates workflow drive --root <project> --run-id <run> --continuous`
消费该 run 已签发的动作，不接受调用方自造 TaskKey、重试次数或执行顺序。原来的
单动作 `--action-id` 入口保持可用；两种入口不能混用。持续入口复用原生执行器、
任务操作、Git adapter、结果 writer 和 cost writer，不创建第二套调度或账本。
独立的已签发动作可并行；下一批次、返修及次数授权仍由 engine 决定。

返回对象包含 `run`、`boundary`，以及存在时的 `blocked`（actionId 到真实原因）和
`external`（外部执行者动作 ID）。`ASK` 需要完成当前 typed Ask；`OPERATOR` 需要
原流程的语义裁决，程序不代作审查结论；`EXTERNAL_EXECUTOR` 表示明确授权给外部
执行者的工作尚未提交。能力缺失或无法确认原生副作用时返回 `BLOCKED`，不降级为
主代理逐项转交。终态返回 `COMPLETE`。

重启时运行同一条命令。程序复用 run 级互斥及每个 action 的原生进程记录，先观察
已经发生的调用并采集结果/来源，再执行 engine 允许的原身份续用。已经完成且来源
已结算的动作不再次启动。中断消费不表示调用未发生，也不授权创建替代会话。
未能确定调用是否发生的 `UNKNOWN` 保留为明确阻塞，不能猜成一次新的调用。

### 需求变更的用户逐项处置

`requirement-change` 的 Ask 包含 engine 生成的 `impactItems`。每项有 `id`、
`canKeep` 和已有证据说明。`DECIDE.decision.dispositions` 必须对所有列出的 ID
逐项填写 `keep`、`rerun` 或 `delegate`；遗漏任何项不能继续。`keep` 要求已有可保留
证据；`development` 的保留只表示保留未完成工作和工作区，不是开发 PASS。
已完成批次使用 `batch:<Batch ID>` 单独列出，不能用笼统判断抹掉某批次的用户选择。

例如 decision 中的 `dispositions` 可含 `"batch:B1":"keep"`、
`"blackbox-review":"delegate"`。这些只是字段示例，实际完整 ID 集合以该次 Ask
为准，不得据示例省略其他项。用户决定和原 request、decision event、需求、candidate
及 revision 一起持久化。

随后 `apply-requirement` 只允许主代理为明确 `delegate` 的项提交影响分类。
用户固定 `keep`/`rerun` 的项可以省略，engine 会填入已确认选择；提交相反的
`impact` 或 `keep` 会被拒绝。不能在 apply 的 DECIDE 中替换前次逐项决定。
新需求正文还须通过原有精确文件 revision、影响预览及 typed Ask 确认。
`meaning=preserved` 不能覆盖用户明确要求的重跑。

保留的已完成 Batch 继续引用原 PASS、commit 和 TaskKey；旧原生 identity、调用、
attempt 与来源历史不变。重跑是在原工作区上重新完成和验证，不是删除工作区、
改写旧 PASS 或清零次数。未回答 Ask 时没有自动默认选择。


### 已完成批次的增量优化与追加批次

在同一个仍活动的 Git/no-split regular run 内使用 `REQUEST development-increment`
→ typed Ask → `DECIDE`。这是当前已确认需求内的增量实现；需求含义改变仍先走
requirement-change/apply-requirement。不能用该入口修改正式需求或已 Seal 的历史。

`request` 的必需字段为 `action`、`target`、`meaning:"preserved"`、`reason` 和
`increment:{kind,batch}`。`batch` 是完整 DevelopmentBatch，包含 id、task、subtasks、
dependencies、interfaces、stateBoundary、risks、definitionOfDone、validation、recovery、
handoff、contextCost；字段含义与技术计划中的 Batch 相同。workspaceView 由 facade
根据当前真实工作区填入并绑定 Ask，调用者不填历史路径、摘要或 TaskKey。

- `kind:"optimize"`：target 为已完成 Batch ID；batch.id 保持该 ID，dependencies
  保持原依赖。先完成当前在途任务、Git 收尾并结算 source，工作区不得有未提交的其他批次工作。
- `kind:"append"`：target 为现有计划的末尾 Batch ID；batch 使用新 ID，并明确依赖
  target。原有未完成批次仍按原依赖继续，新增批次排在它们之后。

Ask 的 `impactItems` 枚举其他已完成批次、已有 QA 设计/审查及验证证据。DECIDE 的
`dispositions` 对每项明确 keep/rerun/delegate；不得遗漏，也没有“默认重做前置审查”。
用户固定的 keep/rerun 优先，冲突请求被拒绝；只有 delegate 项可在 Ask 后通过
`decision.impact` 明确填写 affected/unaffected/unknown，也可沿用 request.impact 中
已展示的分类。decision.impact 不得修改用户固定的 keep/rerun 或增加未列出的项。
不能保留的条目有 canKeep=false 和具体原因，
包括尚不存在的 PASS、绑定旧执行候选而不能用于新实现的验证。需要修改预览或决定时，
取消原 Ask 后重新 REQUEST，不用旧 token 覆盖决定。

确认后，优化及显式重跑的已完成批次得到新的 TaskKey/action/attempt；追加批次获得自己的
新 TaskKey。`regular.developmentIncrements` 保存原计划、前后 TaskKey 对应、用户决定、
workspace view、需求 revision 和影响范围。旧 PASS、commit、attempt、cost 和 source
原样保留在历史；开发从已有 development snapshot 继续，不把优化伪造为原批次 FAIL。
未受影响的产品审、技术审和批次不重新执行；QA 的设计保留不等于新候选已执行 PASS。
保留白盒测试时仅携带测试工件，不能把旧生产代码覆盖到新 development snapshot。
新增或优化 Batch 的结果仍按正常 PREPARATION_RESULT 提交，由 engine 执行 commit/snapshot，
然后继续原 QA、验证 wave、修复及 promotion/Seal。
