# Stage 4a：用户问题与处理记录

更新时间：2026-09-11 22:28（Asia/Shanghai）。

这是用户反馈、证据和处理状态的交接记录，不替代活动 run 绑定的唯一正式需求文件。
后续接手先读本记录，再核对当前 engine 状态；不得把建议、已签发、已启动、已验证混为一谈。

## 当前尚未解决的核心问题

### FG-4A-F01：自动执行不能依赖主模型逐项转交

- 用户指出：并行调度应由引擎负责；宿主可以不转交或“忘记”转交，就不算执行路径闭合。
- 已核实：`Facade.Drive()` 推进并签发动作，但不会自动调用 `ExecuteNative` 消费全部已签发动作。
  B2 曾已签发但未启动；主代理漏转交，不能把这个责任说成需要主代理临时安排并行。
- 原生侧栏补丁 `0123113` 新增 `--subagent` 准备/绑定/采集接口，仍由对话模型调用
  `spawn_agent` 或 `send_input`。它解决了原生父子关联和结果回收，**没有解决自动执行闭合**。
- 用户要求的边界：engine 决定全部可运行工作及补位；程序执行器自动派发和收集；真正的确认点、
  阻塞或终态才交还控制。不能用提示词宣称已实现程序保证。
- 状态：**未解决**。已提出“默认程序自动执行，原生中转保留为非默认入口”的方案并询问；
  截至本记录尚未收到对这项架构取舍的明确选择。不要把别的问题的授权套用到这里。

### FG-4A-F02：已签发不等于运行中

- 用户追问开发子代理是否在并行运行，实际曾只有动作被签发。
- `TASKS_IN_FLIGHT` 当前包含已签发、未 claimed 的动作，不能据此报告“子代理正在跑”。
- 需要区分待启动、运行中、已结束待采集、受阻；运行结论应有真实身份和 SOURCE 证据。
- 状态：**未解决**。没有因补启动一次 B2 就解决状态表达与持续派发缺口。

### FG-4A-F03：原生侧栏展示与普通任务列表不是一回事

- 用户截图指的是当前对话侧栏中的原生子代理活动，不是普通任务列表、改名或分组。
- 先前“任务列表里能找到，所以能兼容侧栏”的结论错误，已纠正。
- 原生工具实测记录包含当前对话的 `parent_thread_id`；小型真实审查完成了 engine 身份验证、
  原生最终 JSON 回收和真实用量结算。不能据此声称已实现 F01 的自动执行保证。
- 本机 Codex 0.153.4 的稳定及实验 schema：`thread/start` 均无父任务参数；
  `threadSource` 是 analytics 分类，`parentThreadId` 只出现在查询过滤参数中。
  不得伪造父子元数据或用普通任务分组冒充原生子代理关联。
- 状态：**原生中转已接入；自动执行与原生展示的同时满足仍未解决**。

## 黑盒设计与错误处理

### FG-4A-F04：设计 FAIL 被错误套用审查 FAIL 合同

- 用户追问“格式具体错哪，有没有提示”。原始错误为
  `FAIL needs a complete QA review and concrete gap`。
- 两次原始返回都是 `blackbox-design` 的 FAIL，没有 `review`。schema 明确允许 PASS/FAIL，
  但接纳分支要求动作名以 `-review` 结尾；因此不是补一个字段就能解决的普通 JSON 格式问题。
- 第一次设计依赖自拟但不存在的 `scripts/stage4a-blackbox.py`；第二次表示公开调用合同不完整，
  保留覆盖义务而未形成 cases/edges。它们没有宣称实跑发现产品功能失败。
- 用户已明确授权：**先修复合同冲突和错误回传，再使用已授权的一次额外尝试**。
- 已实施：黑盒设计缺口作为语义设计 FAIL 接纳；校验当前绑定、覆盖判断和具体说明；部分设计
  不冻结、不当作 PASS；沿用有限语义轮和 QA recovery。旧拒收记录不改写、不事后改成通过。
- 状态：**代码和兼容安装已完成，定向回归通过；修正 Markdown 指令边界后全套测试、包校验及 44 项 portable 检查通过**。
  修复已提交为 `de2d631`，不能沿用原生接入提交号当作其提交证据。

### FG-4A-F05：错误必须传回对应执行者

- 已核实旧重试提示没有携带 `PreparationErrors` 中的拒收原因。
- 已实施：只传同 TaskKey、同需求及候选的最新拒收原因、状态与说明；不给黑盒传开发者结果。
  本模式上轮设计缺口单独保留供修订。固定角色说明放入实际加载的 Markdown。
- 定向测试覆盖本动作错误回传、跨角色隔离、旧候选排除；Markdown 边界回归已修复并通过。
- 第三次黑盒派发已检查实际 prompt，确认含 `previousResultRejection` 和修正后的设计缺口合同。
- 状态：同 F04，**已安装；第三次结果已采集并接纳为语义设计 FAIL，未再报原合同格式错误**。

### FG-4A-F06：黑盒不以内部实现细节为前提

- 用户质疑“不是黑盒用例吗，为什么要知道内部细节”。
- 黑盒需要公开使用契约：入口、参数、可观察结果；不需要缓存、调度或宿主驱动的内部实现。
- 用户先前已决定：具体宿主接入路线在开发期确定，不能偷改为受理或设计阶段必须敲定内部路线。
- 已明确的行为先设计；确实未确定的公开调用方式精确列为待补项。不得虚构 argv，也不得因基线
  尚无新功能或测试侧自拟脚本尚不存在，就认定产品需求错误。
- 状态：**已澄清并写入 QA 设计指令。原生中转把全部 review 角色禁读工作区的过宽限制已修正，恢复各角色本已授权的读取范围；黑盒仍禁读开发代码和开发结论。新语义设计轮结果待采集**。

## 必须继续遵守的既有用户要求

- 相似黑盒 case 复用有效的共同准备和构建，隔离可变状态；不要每例全套独立准备。
  验收目标本身是安装、隔离或原生调用时，仍保留对应真实执行。
- 完整真实样本尽量小；不能为计量或报告重跑已有有效 PASS，不能用局部改善声称整轮省 token。
- 产品/技术审可适度考虑措辞闭合，但不要过度；原产品审原始 FAIL 与裁决后无阻塞必须分开描述。
- Headroom 用户级接入是用户要求；不得擅自在 AGENTS.md 加规则。当前配置有效性另依实际证据核验。
- 先前 B6 计划错误：把外层正式审查、Seal、集成和安装设成开发批完成前提。
  已修订为开发自测和交接；外层正式流程仍在全部开发后执行，不得重新引入这个循环依赖。

## 活动执行快照（接手时必须刷新）

- run：`phase-4a-git-regular-001`；工作树：`../formal-gates-stage-4a`；本次核对 revision **97**。
- B1 已接纳并提交 `484d37d6beb71c63df49945b20d1fdca24ecac23`。
- B2 已接纳 PASS 并提交 `b4968d534470340ff57107d4d5897fbcf5106d4a`；Einstein 已关闭。
- B3 已接纳 PASS 并提交 `1a702e2ca2366d630a8622712c72d62b38bc8eaa`；Schrodinger 已关闭。
- B4 已接纳 PASS 并提交 `84ac1c9792e4809ccf47cd68053f13cbbc9f3f1c`；Nash 已关闭。
- B5 两轮均已接纳 CONTINUE；B5-S1 通用协议已完成，B5-S2/S3/S4 未完成。Turing（`01a090f8-3294-7d63-bf6d-061b2a44edcf`）当前已停止，保留身份待续用；B6 尚未启动。
- 当前引擎 `ASK: RETRY_EXHAUSTED`：两次合法 CONTINUE 已用尽本 TaskKey 的默认 attempt 额度，不是两次运行错误。需要用户精确授权一次 B5 续轮，不能把“推进到 Seal”当成无限次数授权。
- 黑盒已接纳两次语义设计 FAIL，尚未冻结。第三语义轮 `qa-2-0/attempt-1` 已签发但未启动，等待补齐公开使用契约后合法传入。不能报告黑盒正在并行运行，不能凭相同缺口机械重派。
- 额外授权的一次运行失败重试已经消耗；语义修订轮不等于重新获得该授权。
- F04/F05 与原生 QA 读取权限修复已提交 `de2d631`，完成本地验证清单并安装旧 run 兼容包；保留原始拒收记录。
- F01/F02 仍未解决；默认程序自动执行与原生侧栏中转的架构取舍仍待用户决定。当前“推进到 Seal”不作为对该未决架构方案的额外确认。
- 后续集成须保留主分支热修复 `d3972ef`、`0123113`、`de2d631`。安装须保留旧 run 的全局 admission；不能盲目用新包替换兼容运行时。
- 用户在主仓库未提交的 `stage-4a-qa-efficiency.md` 修改不能混入热修复提交。

## 本次推进到 Seal（2026-09-11）

- 用户明确要求继续推进到 Seal；沿用既有 run、已确认需求与路线。
- B2 attempt-2 已恢复 Einstein 并完成原生绑定；首次绑定因输入记录尚未就绪被拒，随后同一身份、同一动作重新核对并绑定成功，没有重派任务。
- 黑盒 qa-1-0/attempt-1 已由 engine 准备并绑定原生子代理 Curie（01a0909d-2267-7281-9470-a1a1482686b2）。这是已签发语义修订轮；没有额外增加运行错误重试授权。
- Gauss 上轮结果已采集，代理已关闭。当前 B2 与黑盒均已实际转交和绑定，但程序自动调度缺口仍未因此解决。
- 原生 QA 读取权限修正已通过主源码及旧 run 兼容包的定向测试，并安装兼容包；最终全套回归另记。

- 最终验证：权限修正后的全套测试、race 指定包、phase0whitebox、vet、包校验和 44 项 portable 检查均通过。
- revision 69：Curie 已完成并被采集为第二次语义设计 FAIL，未冻结。它已实际读取基线公开文档，撤回“现有操作/恢复等全部缺失”的判断；剩余是 4a 批次具体使用方式、新宿主使用合同和新增字段取证说明。不给相同输入机械重派下一轮；B2 继续。

- revision 74：B2 attempt-2 已正式接纳 PASS，四子任务完成；engine 提交 `b4968d534470340ff57107d4d5897fbcf5106d4a`。Einstein 已关闭。
- B3 attempt-1 已签发、派发并绑定 Schrodinger（01a090c5-e629-7ab1-a266-a3daf144486c）；负责资源生命周期。黑盒 qa-2-0/attempt-1 仍未启动，避免用尚不完整的相同公开输入耗尽第三语义轮。
- B2 已交付公开使用说明 `scripts/qa-preparation.md`，明确 qa.run、qa.checkpoint、qaCapacity 等用法；它不是开发验证 PASS 的替代品，也未擅自写入黑盒的基线隔离区。后续补齐其他公开契约后，须通过合法的输入绑定路径供 QA 读取。

- revision 81：B3 已接纳 PASS 并提交 `1a702e2ca2366d630a8622712c72d62b38bc8eaa`；Schrodinger 已关闭。B4 attempt-1 已派发并绑定 Nash（01a090db-c9b7-7792-ba76-623279e1140c），处理交接、进度、计量与脚本索引。

- revision 88：B4 已接纳并提交 `84ac1c9792e4809ccf47cd68053f13cbbc9f3f1c`。随后 B5 派发并绑定 Turing；不以安装或生命周期观察代替三个宿主的实际支持证据。

## FG-4A-F07：B5 原生入口发现与受控样例前提

- B5 首轮返回 CONTINUE、无文件修改、无自测，不是 FAIL/PASS。它只确认 PATH 未找到 workbuddy/zcode，未断言未安装。
- 主代理独立核实 `/Applications/WorkBuddy.app` 与 `/Applications/ZCode.app` 均存在。
- WorkBuddy 自带 `/Applications/WorkBuddy.app/Contents/Resources/app.asar.unpacked/cli/bin/codebuddy`；真实 `--help` 输出保存在 `/tmp/4a-workbuddy-help.txt`，公布 print、json/stream-json、session-id、resume、ACP 等入口。帮助输出不等于实际模型执行成功。
- ZCode 自带 `/Applications/ZCode.app/Contents/Resources/glm/zcode.cjs`；真实 `node ... --help` 输出保存在 `/tmp/4a-zcode-help.txt`，版本 0.16.5，公布 app-server、resume、JSON 等入口。
- DSH 本机版本 0.1.5-rc.1；已安装包的 README.zh.md 明确提供 sdk（JSON-RPC stdio）、sdk-minimal、acp profile。不能仅凭 headless 帮助判定无可恢复接口。
- 当前受控 model.run 仍绑定 Codex provider；取得其他宿主真实样例的合法路径尚待完成。没有直接通过 shell 启动模型，没有据帮助内容宣称完整支持。
- B5 attempt-1 已采集；attempt-2 已由 engine 签发，并以同一 Turing 身份恢复和绑定。未替换开发者、未修改其原始结果、未绕过引擎添加任务消息。

- 公开资料已持久保存至 `refactor-plan/evidence/stage-4a-host-discovery/`，包含两份真实 CLI 帮助和 DSH 启动/SDK/ACP 官方随附说明。DSH ACP 文档明确列出 session/list、session/resume、session/close；尚无本轮真实模型执行结论。

## FG-4A-F08：已核实补充材料无法随原生续轮交付

- 当前引擎的原生入口此前只有固定任务 prompt，没有绑定新公开资料的输入入口。主代理不能自由改写派发正文，也不能把开发结果冒充黑盒依据。
- 已新增 `--subagent-sources`：认领前读取明确的资料清单，校验角色范围，将正文、原路径和 digest 固定到当前 action；消息仅给全文快照入口。已认领动作不能替换输入，相同重放只 observe。
- environment-observation 仅供开发/修复；public-contract 可供开发/修复和 QA 设计/审查。资料不是指令、授权或正式验证结果。未改变调度默认、既有次数、原生身份或已完成结果。
- 主源码与旧 run 兼容包的定向验证通过；主源码全套测试、race 指定包、白盒标签、vet、package validate、44 项 portable 检查均通过。兼容包已安装，未修改 AGENTS.md。
- B5 的资料清单已准备为 `refactor-plan/evidence/stage-4a-host-discovery/b5-sources.json`。尚未消耗新的 B5 续轮；等待引擎规定的单次授权后由 CLI 固定资料并恢复原身份。

- 已生成 B5 单次续轮确认：revision 97，request `request-b5-single-continuation-96`，kind `preparation-retry`，仅当前 B5 TaskKey 一次，confirm/cancel；尚未收到用户决定，不能启动第三次。

## 2026-09-12 用户确认的开发续做计数调整

用户要求：已接纳的 CONTINUE 不消耗失败额度，regular 默认失败 attempt 上限由 2 改为 3。实现保留原始执行、动作编号、原生身份与实际成本；无效 CONTINUE 仍消耗失败额度。恢复依据历史重新计算耗尽投影，不手改 state.json。B5 原请求 request-b5-single-continuation-96 已经 CLI DECIDE cancel 接纳（revision 98），没有增加旧的单次重试额度。真实安装需先构建兼容源二进制；仅重新安装源码会复制旧二进制，不能据此声称已生效。

生效证据：兼容源重新构建并经稳定入口安装后，旧预算通过精确旧规则校验再重新投影；当前 run 在 revision 99 自动签发 B5 attempt-3，operation=resume，identity=01a090f8-3294-7d63-bf6d-061b2a44edcf，regular.ask 为空。没有增加 ExtraAttempts，没有重写原始结果。

派发证据：revision 101，B5 attempt-3 claimed=true、finished=false，仍为 Turing 原身份；动作使用已捕获的宿主公开资料。稳定入口、项目安装包及兼容源构建产物二进制 SHA256 一致。验证中真实安装与注册表隔离测试并发引发环境干扰，按串行重跑受影响检查，不将干扰报告为产品缺陷。

最终验证：全项目测试、所需 race（隔离串行）、phase0whitebox、vet、构建、包验证和 44 项 portable 检查通过。B5 attempt-3 原始 CONTINUE 已采集；revision 103 无 Ask，自动签发同 Turing 的 attempt-4，未追加失败次数。attempt-3 完成 Codex bridge 文件隔离增量；跨宿主受控实跑入口/协议正文仍有缺口，不能将计数调整或本次增量宣称为 B5 完成。
