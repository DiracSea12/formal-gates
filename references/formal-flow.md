# 正式流程与当前 CLI

流程顺序只由 `SKILL.md` 拥有。lightweight 的终态标注“本 run 未经任何验证”。

本版本 workflow 只保留 start、show、status、next、drive、submit、diagnose、guard-preview。
所有完整字段、动态绑定、各类结果、source、裁决、Git receipt 和任务内部 model/QA 请求见
[regular 公开使用合同](engine-regular.md)。旧推进命令和 aliases 已删除；不能按旧示例
调用。历史需求、原始结果和 Seal 保留其原版本，不构成当前接口。

## 启动与 run 控制

在已经完成安装 registry/bootstrap 的固定 launcher 上执行：

```sh
formal-gates workflow start --root /absolute/project --run-id example --vcs git --requirement requirements.md
formal-gates workflow drive --root /absolute/project --run-id example
formal-gates workflow show --root /absolute/project --run-id example
```

start 绑定一个正式文件和当前已提交 Git 基线；drive 完成结构预检，show 返回全文及精确
revision。确认前不派发。REQUEST(confirm-requirement) → Ask → DECIDE(confirm)，然后
REQUEST(mode) 选择 regular/lightweight。lightweight 终态 unverified，drive 自动固化清理。
控制 abort、reset、jump、adopt-external 都通过 REQUEST/DECIDE；不能用旧同名命令。

普通 drive 默认持续消费到用户决定、语义裁决或终态等真实边界；submit 接纳控制
决定后自动续推，不需主代理另发 drive。单步排障使用明确的 --single-step，
完整输出及中断合同见 [regular 公开使用合同](engine-regular.md)。

## 需求、计划与路线

技术审给 granularity_review，产品审有效通过后才进入技术审。确认 topology 携带实际
planDigest 和 no-split 理由；随后确认 full/custom route。full 取全部已选用例模式和当前
catalogGates；custom 明确 qa/gates 集合，至少一项。含 QA 时原子激活需求保证。

需求修订先 REQUEST(requirement-change) 暂停，再以 apply-requirement 绑定同一文件的
新全文、meaning preserved/changed、影响和 keep。review finding 导致修订时，编辑前完整
base 文本生成穷尽的 review-delta JSON，逐项给 base/candidate revision、finding 来源、
每个 hunk 的影响与核验。含义变化的 requirement-impact 由 typed Ask 展示将失效结果与
后续步骤；当前 state revision、Ask token/digest 共同防止过期确认。

## 开发之前

reviewer 只返回原始结论，Operator 以 ADJUDICATE 逐 finding 核实并裁决；CORRECT
需要旧/新决定、等级、result digest、revision 和理由。技术 P0/P1 只允许需求冲突、职责
或依赖根本错误、验收入口缺失；实现偏好最高 P2。向用户请求处置前，在同一条消息完整
展示问题、正常路径后果、建议及逐条定级理由。缺少任一部分，用户回复不能作为授权。

产品/技术 series 各三次有效语义轮，运行错误/无效结果仅消耗有限 attempt。追加 retry
或语义轮绑定当前动作、state revision 和理由；原记录保留，不自动重派已经 PASS 的动作。

## 开发与快照

route 确认后 drive 登记开发与黑盒基线隔离资源，并行签发 development-worker 和
blackbox-design。开发不等待黑盒 PASS。黑盒 design/review 失败沿自身有限重试和恢复
Ask 推进，不以开发尚未结束为等待理由。QA Review 逐点、逐来源、逐 case、逐共同步骤
核对，suggestion 的 P2/P3 逐项按理由处置；实质影响则重审受影响部分。

开发返回 PREPARATION_RESULT，给 manifest、每个 subtask 实际验证、handoff 和完整
状态。adapter 校验范围并跟踪/提交/取快照；缺前置的底层步骤不执行。未完成 Batch 的
开发者返回 CONTINUE；真实运行错误由既有机制记录为 RUNTIME_ERROR。两者保留原生身份，
同批续跑用 drive --action-id --resume。
全部 Batch 与黑盒冻结后，白盒独立判断覆盖再复用有效测试。中间 commit 只固定隔离待审快照；独立白盒 review 检查开发至当前白盒的完整 diff、确认仅测试改动后，才允许最终候选推进。有改动时 skip/waiver 不替代这一证明。
全部 FROZEN case-set 与最终测试快照形成 review-ready，drive 冻结唯一 review-wave。

## 开发后审查

按容量签发同一 wave 的 QA execution 与门，独立目标互不因另一目标 FAIL/运行错误停下。
source resolve/backfill → usage → guard → refill 与结果收集形成明确顺序。QA 用批准 case
的冻结 argv，经 formal_gates_operation 按 prepare→target→collect 执行。跳步、倒序、
重复或前置失败在调用前拒绝。每条请求绑定原 action/TaskKey/Attempt 和精确 case set。

同次 checkpoint 保留完整 PASS/FAIL 行、剩余项和中断原因，绑定 candidate、工具和环境。
变更工具/环境时先以 invalidated checkpoint 指明旧新值、理由及受影响依赖，再恢复执行。
既有有效行不能静默删除。最终结果必须覆盖 exact expected-set，FAIL 需对应 finding。

## 继承、修复授权与 Seal

全部 wave 目标终结、错误处置与逐项核实完成后，WAVE_ADJUDICATE 将 finding 按
NEW/SAME_AS/DISMISSED 归并，保留独立理由和证据。全部 OPEN 义务只形成一个 repair。
普通代码修复固定 AFFECTED；本轮必需 QA 与门全部有效 PASS（含合法继承）即可 Seal，不追加保证模式最终 FULL。纯门审 P2 修复后实跑构建、全量
测试和受影响用例，以 carry 保留门 PASS，不计复审轮。门审 P3 只在 Seal 前展示仍存在项。

三轮后仍有义务时，typed Ask 一次提供额外一轮、再试 QA、精确 waiver、需求变更和 abort。
preview 与授权在 Ask 中绑定，无独立 repair-preview 命令；未收齐不能提前返修。

promotion 校验实际候选和完整验证 view，已有实际提交/更新先 reconcile，不重复执行。
Seal 绑定同一实际候选和实际证据，不再 squash。drive 持久化终态 summary 并按登记资源
清理。完整过程和原始事件仍可由公开 show/diagnose 查询；旧 run 只提供原始只读诊断。

## 成本计量

全部模型来源进入同一 cost 投影，按 Codex 原生 response ID 去重。task timing 包含工具
等待，checkpoint 续跑只结算实际新增区间。owner 只报告本 run 的终态差值，重叠或缺失
来源标 unavailable。guard-preview 显示权威额度 basis/用量；source 未就绪不能自动补位。
