# Compiler Spike 结论（批次 0B）

> 状态：阶段 1 批次 0B 探路产物，结论供批次 1 消费；spike 代码不进入仓库（留存在
> `/tmp/fg-spike/`，独立 Go module `fgspike`，`go test ./...` 全绿，go1.26.4 darwin/arm64）。
> 依据：ADR-001、master-requirements §5.6–5.9、ADR-002（探路首批、结论留痕、代码不进交付）。
> 规模：核心 1817 行（step 622 / registry 100 / compile 593 / encode 323 / testdef 179），
> 测试 1035 行。golden 定义 10 步覆盖全部六变体，制品 6344 字节，
> digest `sha256:02b84510d3d64f0a14509dc83109749aeb3b3e45fe6b242a796ecfc82585d3e2`。

## 0. 实证总览

spike 实证了四件事，每件配可运行测试：

| 命题 | 测试 | 结果 |
| --- | --- | --- |
| IR 字段集 + constructor 使非法组合不可构造 | `step` 包 constructor 负例表 + `TestVariantFieldClosure`（反射证明不适用字段不存在） | 全部按预期拒绝/通过 |
| registry 绑定（稳定 ID + 封闭解析 + 完备性） | `registry` 包重复/跨 kind 复用 + `compile` 包 6 类解析拒绝 | 全部拒绝 |
| encoder 字节稳定性 | assembly 顺序 30 seeds、注册顺序 20 seeds、decode→re-encode、未知字段拒绝、跨进程（exec 自身两次）+ 三次独立 `go build`/`go run` | digest/字节全部一致 |
| 止损指标 | `ext` 包仅用公开 API 新增业务节点；mutation 表 20 类 + fuzz 1500 轮 | 937 次有效变异 100% 拒绝，新增节点 0 处核心改动 |

## 1. IR 字段集最终建议

结构采用三段式：**公共头（compiler 物化段）+ 共享 IO 段 + 封闭变体 payload**。

### 公共头（所有变体相同，制品中物化）

| 字段 | 来源 | 说明 |
| --- | --- | --- |
| `id` | authoring | 步骤稳定 ID |
| `nodeId` | authoring | 所属节点 |
| `ordinal` | **compiler 派生** | 确定性拓扑序（见 §5.1，authoring API 不暴露此字段） |
| `kind` | 变体类型 | 六值封闭枚举 |
| `definitionVersion` | compiler 绑定 | 恒等于 definition 的 version；loader 校验相等 |
| `dependencies` | authoring | 编译期排序 + 去重 |
| `authority` / `runner` | **变体派生物化** | 作者不填，类型上无填写入口 |

### 共享 IO 段（仅 local/durable/host_action/agent 四个可执行变体）

`inputCodec` / `outputCodec`（封闭 codec ID）、`preconditions` / `postconditions`
（`PredicateRef{id, negated}`，结构上不存在自然语言字段）、`inputs`
（`InputBinding{from, output, to}` typed source bindings）。

并行控制步与 human 步的 IO 段必须为零（loader 校验 `IO.IsZero()`）：
human 的 typed I/O 就是 payload 里的 request/response schema；parallel 是纯调度语义。

### 变体 payload（每变体确切字段）

| 变体 | authority/runner（派生） | payload 字段 |
| --- | --- | --- |
| `LocalStep` | ENGINE / ENGINE_LOCAL | `handler`；`timeout`（可选）；`retry`（可选） |
| `DurableStep` | ENGINE / DURABLE_ACTIVITY | `handler`；`idempotency`（必填枚举）；`protocol`（必填枚举）；`reconcileId`（IDEMPOTENT_RECEIPT 时必填，ATOMIC 可选）；`timeout`；`retry`（可选） |
| `HostActionStep` | ENGINE / HOST_ADAPTER | `handler`；`boundary`（三值封闭枚举，必填）；`operation`（注册的 adapter operation ID，必填）；`timeout` |
| `AgentStep` | AGENT / AGENT_WORKER | `handler`；`nonProgrammableReason`（三值封闭枚举，必填）；`timeout`；`retry`（可选） |
| `HumanAskStep` | HUMAN / HOST_ADAPTER | `askKind`；`requestSchema`；`responseSchema`（均必填）；`freshnessTtl`。**无 handler/retry/timeout/side-effect 字段** |
| `ParallelStep` | ENGINE / ENGINE_LOCAL（控制步） | `members`（fan-out 集合）；`join{joinStep, mode ALL\|ANY}`；`failure{mode FAIL_FAST\|WAIT_ALL, escalate FailureClass}`。**无 handler/codec 字段** |

结构性不可表示（编译期消失的组合，spike 用反射测试固化）：
`LocalStep` 无 join/reason/receipt 字段；`HumanAskStep` 无 retry/handler 字段；
`ParallelStep` 无 handler 字段。枚举值缺失（AGENT 缺 reason、durable 缺 idempotency）
无法用类型消除，由 constructor 返回 error——这是 Go 下可达的最强拦截层。

## 2. registry 绑定方式建议

- **单一命名空间**：handler/predicate/codec/reconciler/schema 五类条目共用一个 ID 空间。
  收益实测有效：把 handler ID 填进 predicate 槽报的是 `id "engine.x" registered as
  handler, want predicate`（kind mismatch），而不是含糊的 not found。命名建议
  `domain.family.name`（如 `engine.persist.intent`、`reconcile.intent.persist`、
  `schema.ask.decision.request`）。
- **注册机制**：`RegisterHandler(id, runner, fn)` 等五个方法，fn 为 nil 拒绝；
  同 ID 二次注册（无论同类还是跨类复用）一律拒绝——重复检测在注册期而非编译期。
  handler 条目携带 runner，供编译期 kind 匹配。
- **完备性检查**：编译期对每个步骤的每个引用 ID 逐一 `Resolve(id, wantKind)`；
  缺失（closed world not found）、kind 不匹配、handler 的 runner 与变体派生 runner
  不匹配，三者全部拒绝。spike 验证了 6 类解析拒绝场景（见 §4）。

```go
func (r *Registry) Resolve(id string, want EntryKind) (step.Runner, error) {
    e, ok := r.entries[id]
    if !ok {
        return "", fmt.Errorf("registry: %s %q not found (closed world)", want, id)
    }
    if e.kind != want {
        return "", fmt.Errorf("registry: id %q registered as %s, want %s", id, e.kind, want)
    }
    return e.runner, nil
}
```

- **锁步激活**的落点：`Compile(def, reg)` 内完成全部解析；`Decode(bytes, reg)`
  同样以 ValidateCompiled 收尾。未注册实现（MISSING_ENGINE_ADAPTER 场景）在 spike
  中表现为编译失败本身；批次 1 应把"diagnostic-only 定义可编译诊断但不可执行"
  做成 compile 产物上的 marker 字段，而不是放松解析规则。

## 3. encoder 字节稳定性实现要点

- **标准库足够**：`encoding/json` 对 struct 字段按声明序输出，无需手写 encoder。
  前提是 IR 与 wire 结构完全无 map、无 float、无 `time.Time`——duration 一律
  int64 纳秒（`timeoutNs`）。
- **排序**：dependencies/inputs/predicates/members 在编译期 sort+dedup；
  steps 按 `(nodeID, ordinal, id)` 排序；ordinal 由确定性 Kahn 拓扑序派生，
  ready 集每轮取 `(nodeID, stepID)` 字典序最小。
- **归一化**：零值省略（`omitempty`）；可选策略（retry）指针化；空集合不输出。
- **坑（spike 实际踩到/验证的）**：
  1. `omitempty` 对嵌套 struct 值不生效——可选策略必须用指针；
  2. `MarshalIndent` 关不掉 HTML 转义，必须 `json.Encoder` + `SetEscapeHTML(false)`
     + `SetIndent("", "  ")`；`Encode` 自带尾随换行，canonical 形态 = 2 空格缩进
     + 恰一个 `\n`；
  3. decode 必须两级严格（外层与 payload 各自 `DisallowUnknownFields`），
     否则 closed world 会被未知字段穿透（spike 有负例测试）；
  4. IR 结构不带 json tag，wire 结构独占 canonical 形态定义，避免 tag 漂移出
     第二套编码语义（ADR-001 决策 5"单一 encoder"的落实方式）。
- **变体判别只存在于 encoder 的 payload type switch**（encode/decode 各一处）。
  新增变体*实例*不触碰 encoder；新增变体*种类*才加 case。
- **实测**：同一定义 30 种 assembly 顺序、20 种注册顺序、decode→re-encode、
  同进程重复编码、跨进程（exec 测试二进制两次）+ 三次独立构建，digest 与字节
  全部一致。

## 4. 止损指标验证结果

**新增普通业务节点**（`ext` 包，只用公开 API）：
1 个 handler 函数 + 1 行 `RegisterHandler` + 1 个 `NewLocal` 实例 + append 进步骤表，
**compiler/encoder 核心 0 行改动**——`ext` 包物理上 import 不到 compiler 内部
（payload seal 见 §6），编译通过即证明。新定义 digest 不同于 golden
（拓扑变化 ⇒ DefinitionDigest 变化，符合 ADR-001 验收 6），且自身的 assembly
顺序无关性与 round-trip 稳定性同样成立。

**mutation 拒绝**（对解码后制品施加变异，ValidateCompiled 必须拒绝）：

| 变异类 | 拒绝依据 |
| --- | --- |
| 删普通依赖 | inputs source bindings 与 deps 集合不相等 |
| 删 join 步依赖 / 删 parallel 成员 | join 依赖集合 ≠ 成员集合（fan-out 覆盖） |
| 删输入绑定 | 同第一行，方向相反 |
| 删 join / failure policy | parallel payload 必填校验 |
| 删 reconcile | IDEMPOTENT_RECEIPT 协议必填 reconciler |
| 删 parallel 锚点依赖 | 并行组必须挂在已完成步骤后 |
| 删 human request/response schema | human wait 必填 schema |
| 悬空依赖 / 未知 handler / 未知 predicate / 未知 codec | closed-world 解析 |
| handler kind 不匹配（跨 runner 换绑） | handler runner ≠ 变体派生 runner |
| 篡改 authority / ordinal | 与变体派生物不一致 / 与确定性拓扑序不一致 |
| 加孤儿步骤 | 从入口节点不可达 |
| 造环 | Kahn 定序不收敛 |
| 清空步骤/定义 version | 版本绑定校验 |

表驱动 20 类全部拒绝；fuzz 1500 轮（固定种子）中 937 次有效变异 **100% 拒绝**
（其余为目标不含该变异前提的跳过轮）。注意：mutation 直接攻击制品层是必要的——
authoring constructor 已使多数变异不可构造，这正是"局部靠类型、全局靠 compiler、
制品靠 loader 复用同一 ValidateCompiled"分层有效性的证据。

## 5. 发现的问题与对批次 1 的建议

1. **"缺 reason 编译不过"的字面度**：Go 无穷举 sum type。字段级不可表示
   （human 无 retry）是编译期的；枚举值缺失只能 constructor 运行期报错。
   enforcement matrix 应如实标注这一边界，不要在文档里许诺编译期拦截一切。
2. **共享 IO 段 vs ADR-001 §5.6 措辞**：§5.6 说"每个 StepSpec 在自身变体内包含
   pre/postcondition、codec 引用…"。spike 把它们放公共头旁的共享段而不是每变体
   重复，避免六个 payload 复制同四字段（encoder 漂移风险）。建议批次 1 拍板为
   "公共头 + 共享 IO 段 + 变体 payload"三段式，并回写 ADR 措辞；这是措辞级偏差，
   不改变"变体不适用的字段不得存在"的约束。
3. **ParallelStep 的 runner 归属**：四值 RunnerKind 没有 CONTROL。spike 物化为
   ENGINE/ENGINE_LOCAL 且 payload 无 handler 字段（制品自解释为控制步）。批次 1
   需确认：保持此约定，或为控制步引入显式 runner 值（后者动需求枚举，成本更高）。
4. **ordinal 必须是派生值**：若 authoring 允许填 ordinal，assembly 顺序立即泄漏进
   字节。§5.6 公共头的正确读法：ordinal 只存在于制品，authoring API 不暴露。
5. **建议采纳"inputs 集合 == dependencies 集合"为 compiler 机械不变量**：它使
   "删依赖必被拒"成立（ADR-001 验收 9），且与 §5.15 submit 的 source bindings
   校验同源。spike 已验证其可实现与足够的拒绝覆盖。
6. **新增变体种类的改动面**：第七种 step kind 需动 4 处（step 包类型+constructor、
   materialize 的 derive switch、validate 的 payload switch、encode/decode 各一个
   case），全部是集中的一行 case，不触五六个无关模块——未触发 ADR-001 失控
   触发器；新增变体实例 0 处。可以在批次 1 把这 4 处作为 checklist 固化。
7. **json 转义与尾随换行**：仓库现有 `definitions/workflow.json` 若由不同工具
   生成，批次 1 必须统一到同一 encoder 产物（SetEscapeHTML(false)、2 空格、
   单尾换行），否则"重新生成无 diff"验收会被空白差异击穿。
8. **重复构建/多进程稳定性成本极低**：exec 自身两次 + 三次独立构建验证digest
   相同；正式 CI 直接照抄 spike 的 TestMain/env 手法即可。

## 6. 关键代码片段摘录

封闭变体（字段不存在即编译期不可表示；`seal()` 使外部包不能新增变体）：

```go
type Step interface{ seal() }

type HumanAskStep struct {
	Header                                   // id/nodeId/dependencies
	AskKind        string                    // 无 Handler/Retry/Timeout/IO 字段
	RequestSchema  string
	ResponseSchema string
	FreshnessTTL   time.Duration
}
func (HumanAskStep) seal() {}
```

constructor 拒绝非法枚举（八类拒绝的 AGENT/HOST 层）：

```go
if !spec.Reason.Valid() { // 零值与 "实现麻烦" 一律非法
	return AgentStep{}, fmt.Errorf(
		"agent step %q: nonProgrammableReason required "+
			"(SEMANTIC_JUDGMENT|CREATIVE_IMPLEMENTATION|INDEPENDENT_REVIEW)", h.ID)
}
```

确定性拓扑定序（assembly 顺序不泄漏进字节的关键）：

```go
// ready 集合按 (nodeID, stepID) 字典序取最小者；ordinal 是图性质的函数
if pick == nil || stepKeyLess(cs.Header, pick.Header) { pick = cs }
...
return nil, fmt.Errorf("graph: dependency cycle or unresolvable order among %v", stuck)
```

typed source bindings == dependencies（"删依赖必拒"的机械不变量）：

```go
for _, in := range cs.IO.Inputs {
	if !depSet[in.From] {
		return fmt.Errorf("validate: step %q input binding from non-dependency %q", h.ID, in.From)
	}
	fromSet[in.From] = true
}
for d := range depSet {
	if !fromSet[d] {
		return fmt.Errorf("validate: step %q dependency %q has no typed input binding", h.ID, d)
	}
}
```

canonical 编码核心（单一形态：不转义 HTML、2 空格缩进、尾随换行）：

```go
var buf bytes.Buffer
enc := json.NewEncoder(&buf)
enc.SetEscapeHTML(false)
enc.SetIndent("", "  ")
if err := enc.Encode(w); err != nil { return nil, err }
return buf.Bytes(), nil
```

compiled payload 封闭接口（外部不能伪造 IR；encoder 变体判别唯一入口）：

```go
type Payload interface{ payloadSeal() }
func (LocalPayload) payloadSeal()    {}
...
switch pp := p.(type) { // encode.go 内唯一的变体感知点
case LocalPayload:
	v = localWire{Handler: pp.Handler, TimeoutNs: int64(pp.Timeout), Retry: toRetryWire(pp.Retry)}
```

止损证据（ext 包新增业务节点的全部增量，走公开 API）：

```go
r.RegisterHandler(testdef.HandlerCostReport, step.RunnerEngineLocal,
	func(in string) (string, error) { return "cost:" + in, nil })
cost, _ := step.NewLocal(
	step.Header{ID: "report.cost", NodeID: "report",
		Dependencies: []step.StepID{"slices.join", "ask.approval"}}, io,
	step.LocalSpec{Handler: testdef.HandlerCostReport})
// compiler/encoder 核心：0 行改动
```
