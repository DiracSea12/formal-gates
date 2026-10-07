---
name: formal-gates
description: 用户明确要求走正式流程时才进入完整受理流程；用于 formal-gates 对齐、审查、发布、Seal、安装或正式诊断，支持不验证只留记录的轻量路线。非修改性的提问、解释、诊断和 review 不触发。项目内容修改默认提醒一次：若需走 formal-gates 流程，可直接提出；大/复杂需求在需求与方案确认、准备开做时再额外强调一次。
---

# Formal Gates

本版本以 engine 的已签发动作、typed Ask 和单一状态为流程权威。公开入口、收据字段与
调用形式唯一持有在[正式流程](references/formal-flow.md)；安装维护见
[安装与 Hook](references/install-and-hooks.md)。宿主执行能力和真实验证结论分开记录，
启动前核对当前安装副本的原生能力。当前开发候选尚未完成最终 canary、发布或全局切换。

## 范围边界

正式 PASS 的唯一来源是独立代理经 CLI 校验接纳的结论；独立代理不可用则报告受阻。
项目只保证文档化正常使用和常见操作失误，审查边界唯一见
[prompts/reviewer-base.md](prompts/reviewer-base.md)。不发明需求、任务或公共入口。

## 开发受理流程

<formal-gates:host-instructions:start>
本段受理与提醒义务仅适用于直接接收用户任务的主代理；已由 engine 派发的开发者、审查者和执行者只完成当前 action，不重复受理、提醒或自行触发正式流程。

普通任务：创建、编辑、移动或删除项目内容时，默认提醒一次“若需走 formal-gates 流程，可直接提出”。常规工作在用户已授权范围内继续。需求规模、耦合、风险或验证复杂度超过普通直接处理能力时，在需求澄清与方案确认完毕、准备开做之际再额外强调一次：“检测到复杂需求，建议走 formal-gates 流程”。非修改性的提问、解释、诊断和 review 不触发正式流程；主代理不得自行触发。

新对话检查：主代理必须检查工作区 `.gates/engine/*/state.json` 与 `.gates/tmp/*/state.json`。发现无法对应当前用户会话的残留状态时，告知文件路径、run 状态及可能影响写入，并说明若需走 formal-gates 流程可直接提出。由用户决定推进完成、通过当前 typed Ask 授权的 reset/abort 及 engine 清理结束或删除对应状态文件；不得自行处置，也不得把残留状态解释为用户已启动正式流程。

用户明确要求走正式流程（或明确要求触发 formal-gates）时，直接进入完整受理：先澄清需求与重大技术选择，能由事实和既有决定解决的自行处理；无待澄清问题时也明确说明。提出方案不等于确认。把完整需求与方案直接写入唯一正式需求文件；先做确定性结构预检，通过后展示该文件全文与精确 revision，再单独请求并等待用户明确确认。确认后才以 typed intake receipt 启动开发 run。确认前不选择路线或派发后续子代理。修订时更新同一文件并重新预检、展示和确认。

确认后：在已授权范围内继续，不逐步重复请求许可；只有影响范围、公开行为、验收或主要架构的未决事项才补充澄清，流程规定的确认节点仍须完成。未明确模式时再选择 lightweight 或 regular；full/custom 在拆分决定之后确认。engine/CLI 是下一步、派发、恢复和结果接纳的权威，主代理提交事实与明确用户决定。
<formal-gates:host-instructions:end>

主代理先检视工作区，逐项澄清目标、场景、边界、默认值、失败路径、验收与主要技术选择。
能够由事实和既有决定解决的自行处理；有实质缺口才询问，每次一个决定。无剩余问题时明确
说明“无待澄清问题”。提出方案不替代用户确认。

把完整整合后的需求与技术方案直接写入唯一正式 UTF-8 Markdown 需求文件，先做确定性
结构预检，再展示该文件全文与精确 revision，单独发起确认提问并等待用户明确确认。
预检只核结构、字段、唯一性与边界，不替代用户确认或独立审查。修订时更新同一文件并
重新预检、展示和确认。确认前不启动开发 run、不选择路线、不派发后续独立任务。
确认后如模式尚未明确，再选择 regular 或 lightweight；后者只保留 unverified 记录。
以已确认 typed intake receipt 调用 start，首次 drive 自动登记该 receipt，不重复全文确认。
regular 路线 full/custom 在技术审和计划/拓扑确认后选择，不能降为 lightweight。

## 正式流程

1. **受理与确认。** 唯一正式文件包含完整需求/方案和 REQ/AC/来源；先结构预检，
   再展示全文和精确 revision，等待用户确认。lightweight 不要求结构化正式需求案。
2. **启动与登记。** start 绑定已确认 intake receipt 和当前安装身份，不接受 split 意向。
   首次 drive 自动登记该确认。regular 的产品审、技术审、开发与 QA 只消费确认 revision。
3. **产品审、技术审。** 先独立产品审，通过后才独立技术审；每次 fresh review 读取完整
   最新文档。审查者返回 raw candidate，Operator 逐条核实、定级、裁决，用户处置有效 finding。
   技术审形成 granularity_review，包括 Batch → Subtask、依赖、接口、状态边界、DoD、
   验证、恢复和交接。不把文件数、commit 数或编号当作拆分理由。
4. **计划与路线。** 用户确认当前 planDigest 与 split 拓扑或 no-split 理由，再确认 full/custom。
   需求保证绑定已确认单一结构化需求案，split 的 child 由 engine 创建；
   使用当前安装实际提供的 VCS/宿主能力，不能把安装成功当作全流程验证通过。
5. **开发与黑盒并发。** 开发只读已确认任务；黑盒在基线隔离工作区独立设计/审查，不读
   实现、白盒、开发解释。黑盒未 PASS 不挡开发，但挡最终候选。设计失败后继续按原动作的
   有限 attempt/语义轮或 typed Ask 处理，不能搁置到开发末期。每 Batch 新原生代理，批内
   正常返回、自测失败、重试和恢复沿用原身份；替换须真实恢复失败事实与 engine 签发。
6. **固定候选。** adapter 校验交付 manifest 与范围，track → commit → snapshot。
   全部 Batch 与黑盒屏障完成后接入独立白盒覆盖/测试；只有测试变动推进快照。全部
   FROZEN case-set 装配到 review-ready 候选，在最后必需验证之前压缩 Git 多提交范围。
7. **完整验证 wave。** 冻结唯一 expected-set，按容量并行推进 QA execution 与各门。
   QA 按批准步骤消费准备→目标→采集操作；门只读基线至最终候选完整实现 diff。
   一个 FAIL/运行错误不停止独立兄弟任务。每项 source 结算、usage、guard 后自动补位。
8. **统一修复。** 全部目标终结、错误处置和 finding 裁决完成后，NEW/SAME_AS/DISMISSED
   归并 canonical problems，只建一个 repair。普通修复固定 AFFECTED；开启 requirement guarantee 的最终候选 Seal 前须 FULL，
   必需 QA 与门全部有效 PASS（含合法继承）后才可 Seal。
   门审 P2 必须修复，以实跑自测/carry 保留门 PASS，不增加复审轮；门审 P3 只作 Seal advisory。
9. **Promotion 与 Seal。** 固定同一实际候选的 tree/blob/mode/ref 和需求/QA/验证绑定，
   promotion 后核对等价 receipt，Seal 不再生成候选。drive 先持久化可恢复终结 intent，再清理全部登记资源；无残留 receipt 后固化终态。
   正式 QA、Seal、集成与实际全局切换是各自时点；开发/隔离 canary 通过不表示已发布。

## 轻量路线

lightweight 是正式流程内的路线（非受理阶段选项）。用户明确确认完整需求与方案后进入正式流程，
在 start 选择 mode lightweight，不经拆分决定与路线选择，不选 QA/门路线、不快照。
记录“本 run 未经任何验证”和 unverified；drive 完成终态。已有 typed receipt 入口必须同时
给 --intake-receipt 和 --route lightweight。不能将已绑定 regular 的 run 降为轻量。

## 宿主执行纪律

主代理只提交事实和明确用户决定。使用 show/next 中实际签发的 actionId、TaskKey、Attempt、
候选和原生身份；不能手造任务、次数、wave 或扩大授权。唯一事件写入口为 submit；drive
执行程序拥有的机械动作和实际宿主调用。重复结果重放不重复副作用，UNKNOWN 先对账。

任务内声明 QA 操作也经过相同 CAS、intent、guard 和原动作绑定。不得绕过
formal_gates_operation 使用自由 shell 创建模型或调用 QA 目标。设计冻结用例步骤与判据；未声明 command 时由执行者经 qa.target 提交实际命令，
已声明的 command 不得替换。共同步骤、依赖和 checkpoint 按公开合同使用；实际工具/环境变化先
提交精确 invalidated checkpoint，再继续原动作的未完成部分。

独立派发按宿主容量并行，仅从已授权 Ready 集合取任务。同功能在途规则保持；依赖等待
不产生新角色或审查。开发者与审查者相互隔离。黑盒不接收实现及开发验证结论，白盒/门
依据其各自授权输入工作。开发/返修返回交付路径 manifest；adapter 校验后跟踪、提交和快照，并提供完整 diff。

使用 `workflow drive --root <project> --run-id <run>` 推进已签发动作及恢复；
派发、绑定和采集由同一引擎驱动完成，见[原生运行时派发](references/formal-flow.md#原生运行时派发)。
不得调用已删除的手工子代理入口或自行追加派发内容。需求澄清由主代理依据受理规则完成，
不创建澄清子代理；其实际需求文件和确认记录是受理材料，不能冒称原生模型调用记录。

请求用户处置 finding 前，必须在同一条消息逐项展示问题、正常路径错误、后果、建议、
finding ID、等级和逐条定级理由。缺少任一部分，用户回复不能作为授权。主代理逐条核实
真实性、范围、严重度、根因和影响；没有独立证据不能冒充正式 PASS。

需求变化先暂停受影响推进，再澄清、展示完整文件和确认。review finding 修订先从编辑前
完整文本制作穷尽 review-delta；meaning preserved / meaning changed 采用对应影响和保留
合同。REQUEST/Ask 展示完整预览，DECIDE 精确绑定。CLI 不猜文本含义；保留既有 PASS
需要明确清单，其他受影响证据失效。历史 raw result 和原授权保留，轮数不清零。

产品/技术/QA 各自有限语义轮，外部派发默认同 TaskKey MaxAttempts=2（首次执行和一次自动重试），
compiled definition 可声明其他有限值。超限进入对应 Ask，
仅增加当前动作的一轮/一次或处置精确义务，不累计授权。黑白盒三连 FAIL/长期不可用必须
进入完整 qa-recovery Ask，用户可重设、fresh review、需求变更、精确 waiver/skip 或 abort。

## 维护与成本

安装、hooks、打包和 canary 见 [安装维护](references/install-and-hooks.md)；修改本包时
执行 [本地验证](references/local-validation.md)。安装 hooks 配置不等于实机已生效。

source 可就绪才以原生 transcript 结算同一 cost 投影；source 未就绪 Wait，明确不可用
标 unavailable。guard 使用实际派发次数和模型用量；owner 终态区间差值只报告，不参与
in-flight guard。Codex 按原生 response ID 去重，记录真实 token 与 task timing，包含工具
等待、不含 turn 间空闲。多 run 的 owner 区间重叠或基线不可靠时不猜测数字。

## 内部节点与宿主扩展

普通业务节点使用既有 typed authoring、handler/codec/predicate/reconcile registry 和定义
装配。扩展须实现、注册并接入编译定义；执行与恢复共用提交和接纳路径。新的控制语义
须按其实际边界设计，不能把任意控制节点视作即插即用插件。

宿主适配器只持有原生启动、执行、观测、恢复与证据传输。业务任务、原始证据接纳、
身份绑定和 UNKNOWN 对账由同一 engine 路径管理。配置、doctor、模拟或安装通过
不能代替同一候选与实际安装副本上的真实宿主 canary。
