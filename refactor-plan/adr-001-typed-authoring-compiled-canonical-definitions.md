# ADR-001：typed Go authoring 与编译式 canonical 定义制品

> 状态：已由用户确认（2026-08-22），作为重大技术选择冻结。
> 范围：修订 `openspec/changes/orchestration-pipeline-engine/master-requirements.md` 第 5 节与 `refactor-plan/final-implementation-draft.md` §3.2/§3.3 中步骤模型（StepSpec）与定义编译器的形态；不改变公共入口、用户节点与验收口径。
> 决策依据：三轮独立架构评审的收敛结论。原始讨论对话为非权威参考，不随库保存；其中已被撤回的中间结论列于文末，不得被后续工作复活。

## 背景

原方案的 StepSpec 是一个同时承载拓扑依赖、前后置条件、双维权限、I/O schema、重试/超时、幂等、副作用/receipt、中断、并行/join、失败分类、定义版本与 agent/host 理由的扁平万能结构（约 20 个公共字段加一个条件必填 reason）。它对本项目——流程封闭、节点由项目自控、无用户自定义工作流——构成框架级过度设计：多数步骤只使用少数字段，其余为空值/默认值；合法组合要靠编译器逐项排除；新增一种步骤类型需同时修改定义结构、编译器、序列化、运行时与测试。另一极端（完全删除定义编译器与定义制品、只留具体 handler 加裸表）则丢失全局图不变量（可达性、循环、join 覆盖、版本绑定）的机械保证，且与阶段 0 已交付的定义身份契约（`definitions/workflow.json` + 字节摘要 + 版本信封）不兼容。

## 决策

1. **Authoring 形态**：封闭 Go 类型变体（sealed variants）+ constructor + 显式节点/步骤表是唯一定义编写形态。不引入 JSON/YAML 工作流 DSL、通用 schema 解释器、用户自定义节点插件、任意脚本/表达式扩展点，不为架构对照实现第二套完整内核。
2. **变体即权限**：`DecisionAuthority`/`RunnerKind` 由步骤变体自动派生并在编译制品中物化；作者不手填，无法写出 `HumanAskStep + ENGINE` 之类的非法组合。变体只暴露自己适用的字段：local step 看不到 receipt/join/agent reason，human step 填不了 retry/side-effect。
3. **编译器定位**：保留一个小型 closed-world compiler，职责限于：封闭 registry 解析（HandlerID/PredicateID/CodecID/ReconcileID 的存在性、唯一性、kind 匹配）、全局图不变量校验（可达性、非法循环、依赖存在、分支目标封闭、并行组 join/failure 覆盖、版本绑定）、归一化、authority/runner 派生与 canonical 编码。它不解释业务表达式、不理解具体业务节点。
4. **canonical 制品**：编译产物是唯一 definition 制品 `definitions/workflow.json`——由 compiler 生成、checked-in、字节级稳定；definition 身份即该制品字节摘要，不保留独立的手写 source digest。制品不含函数、闭包、内存地址、OS 绝对路径、当前时间、无序 map、任意脚本；执行引用一律使用封闭 registry 中的稳定 ID。
5. **单一 encoder**：canonical 编码只面向统一的 `CompiledDefinition` IR 实现一套；不为各 authoring 变体分别实现 MarshalCanonical，避免默认值处理、字段排序与新增变体漏编等漂移。
6. **常量同源生成**：compiler 的同一生成动作同时产出 `definitions/workflow.json` 与期望身份常量（生成的 Go 源或嵌入字节）；禁止人工把 digest 复制进 `phase0.go` 一类常量。
7. **三类身份职责分离**：

   | 身份 | 绑定内容 |
   | --- | --- |
   | DefinitionDigest | 拓扑、handler/predicate/codec ID、策略、schema ID、join/failure 语义 |
   | PackageDigest | 真正的 Go 实现字节、二进制与安装包资源 |
   | PlanDigest | 给定 state+observation 的确定性决策结果 |

   PackageDigest 是 owning-runtime 的执行绑定而非审计附注。活动 run 的写入前校验采用其一：(a) run 由其绑定的不可变 runtime sibling 执行并验证实际 package digest（推荐）；(b) loader 校验 envelope package digest 与当前安装身份一致；(c) 持有显式兼容/adopt receipt。不得仅凭 HandlerID 相同接受新实现接管旧 run。
8. **HandlerID 版本规则**：HandlerID 标识可恢复执行合同，不标识每一版实现。合同不兼容变化（输入/输出 schema、side-effect protocol、receipt/reconcile 语义、idempotency key 计算、failure-class 合同、持久化/恢复前提、authority/runner 或外部能力边界）必须晋升 ID；其中**声明的输入/输出接受域收紧也属合同不兼容**——以前接受、现在拒绝同一合法输入即改变了输入合同。合同兼容的实现变化（内部重构、性能、日志、依赖库升级，以及不改变已声明合同的 bug 修复与内部校验加固——加固仅指拒绝定义上本就非法、此前被错误接受的输入）保持 ID，由 PackageDigest 标识。保持合同的 bug 修复不改变可观察行为合同，属后者。
9. **锁步激活**：executable definition 只有在同一候选包 registry 能完整、唯一解析其全部 ID 时才能激活。分层边界：源码开发层允许暂时存在未完整实现的 diagnostic-only 定义（可输出诊断、不可执行、不可签发 Ready/HostAction、不可进入 promotion），与 `MISSING_ENGINE_ADAPTER` 原则一致；候选激活层严格 definition + binary registry 锁步；正式运行层精确版本与双摘要绑定。
10. **八类拒绝结果全保留**：不可达 step/非法循环、无类型 I/O、自然语言-only pre/postcondition、无幂等/reconcile 的副作用、无 request/schema 的 human wait、无 join/failure policy 的并行组、缺合法 reason 的 AGENT/HOST、未绑定 definition version——以 enforcement matrix 标注主要拦截层（封闭类型/constructor、closed-world compiler、runtime loader）与二次防线；需求不因实现分层而减少约束。

## 增补（2026-08-22，阶段 1 封板后补漏）

两项增补不改变已冻结的决策，只补齐落地对照与封闭面：

1. **NodeExecutionPlan 落地对照**：master-requirements §5 与 final-implementation-draft §3.2 的"每个节点编译为有序 `NodeExecutionPlan`"由 `CompiledDefinition` 直接落地，不设独立结构——制品步骤按 (nodeId, ordinal) 稳定排序（ordinal 由确定性 Kahn 拓扑序全局唯一派生），某节点的 NodeExecutionPlan 即其步骤按 ordinal 的有序子序列，节点内机械顺序、前后置条件与 fan-out/join 语义全部由该子序列承载。与拍平 StepSpec 被拒同理，不为"节点"再投影出第二套并行结构。阶段 3 真实业务节点迁入时沿用本对照，不新增节点级重写层。
2. **registry 槽位扩至七类**：AskKindID（human ask 的合法类型，如 `decision`）加入封闭 registry，与 OperationID 同纪律（独立命名类型、单一命名空间、缺失走 MISSING_ENGINE_ADAPTER 路由）。此前 authoring 注释承诺的"由 compiler 对注册表校验"自本增补起生效。同步补齐 compiled IR 二次防线对 agent 步 postconditions 非空（worker result 合同）的复核，enforcement matrix 的 constructor 主拦项至此全部有 compiler 层二次防线。

## 已拒绝的替代方案

- 扁平万能 StepSpec 单结构体（原方案 §3.2 字段清单形态）；
- 通用工作流 DSL / 运行时 schema 解释层 / 用户自定义节点插件；
- 完全删除定义编译器与定义制品（裸 handler 表）；
- 为架构对照而实现两套完整内核。

## 阶段 1 落地要求（摘要）

- 开工前先做六种代表性 step（engine local、durable side effect、host action、agent task、human ask、parallel/join）的小型 compiler spike，确认 compiled IR、registry 与 canonical encoder 边界；spike 不进入 production。
- 独立验收测试（除既有 golden/property 外）：
  1. authoring source 重新生成 checked-in `definitions/workflow.json` 字节无 diff（generated-artifact freshness，独立于 round-trip 的 CI 检查）；
  2. 任意 assembly/注册顺序编译产生相同字节与 digest；
  3. decode → encode 字节不变（round-trip）；
  4. 多进程、重复构建产生相同字节；
  5. 仅改 handler 实现、不改 ID 与定义语义时 PackageDigest 变、DefinitionDigest 不变；
  6. 改变 dependency/policy/reason/schema ID/handler ID/join 语义时 DefinitionDigest 必变；
  7. registry 对每个 ID 精确解析一次，缺失/重复/kind 不匹配拒绝；
  8. constructor 层证明不能构造 authority/runner/reason 不匹配的步骤；
  9. mutation test：随机删除依赖、join、failure edge、version 或 reconcile，compiler 必须拒绝；
  10. 复杂度止损：新增普通业务节点不得要求修改 compiler core；若需要，必须先证明这是新的控制语义而非 compiler 开始理解业务。
- 失控触发器（出现即收缩架构并重走需求变化流程）：authoring 层大量使用 `any`/map/反射；compiled IR 含自由脚本或表达式；runtime 可绕过 compiled definition 直接调用 handler；validator/compiler 比实际 planner 本体更复杂；一种新变体要求同时修改五六个无关模块；相同语义定义不能稳定产生同一 digest。

## 已撤回的中间结论（不得复活）

- “砍掉定义编译器，Go 编译器可免费替代”——局部不变量可交给类型系统，全局图性质不可；
- “阶段 0 的 `future.go` 证明定义必须以数据为 authoring source”——它只证明最终必须存在可序列化、可摘要、可版本绑定的定义制品；
- 3–5k / 4–6k / 5–8k 等行数估计——无 WBS 依据，仅为对话中的体量比较，不构成任何验收或决策依据；
- “以 `REQUEST_REQUIREMENT_CHANGE` 事件登记本次变更”——该 typed 事件属于未来 engine 公共面；本次变更按当前冻结 stable driver 的既有需求变更流程登记。
