# Design

## 1. 分层与依赖方向

```text
authoring（封闭变体 + constructor + 显式表，仅源码）
  -> closed-world compiler（registry 解析、图校验、归一化、authority/runner 派生）
  -> CompiledDefinition IR（具体结构、公共头 + 变体 payload、稳定 ID）
  -> 单一 canonical encoder
  -> definitions/workflow.json + 身份常量（同源生成、checked-in）
  -> runtime loader（版本 + digest 精确匹配）
  -> Observe → Decide → SelectIssued（纯决策，不写 state）
  -> NextResult / canonical Plan
```

依赖单向向下；runtime 不得绕过 compiled definition 直接调用 handler；compiler 不 import 业务节点包（止损规则的机械表达）。

## 2. 变体与 constructor

六种变体只经包内 constructor 构造：constructor 要求全部必填参数（AgentStep 必须带合法 nonProgrammableReason，DurableStep 必须带 idempotency/reconcile，HumanAskStep 必须带 typed request/codec，ParallelStep 必须带 join/failure policy），零值与未导出构造不可用。authority/runner 由变体推导并在 IR 物化，作者不可填。

## 3. Compiler 职责边界

只做机械可证的检查：ID 唯一性与 registry kind 匹配、依赖存在、可达性/循环、join 覆盖、版本绑定；输出稳定排序（全集合显式排序、默认值归一化、无 map 遍历序、无时间/路径）。它不证明 predicate 语义正确，"编译成功"不替代行为测试。`MISSING_ENGINE_ADAPTER` 与未解析 ID 一律 diagnostic-only，executable 激活要求同包 registry 完整解析。

## 4. Canonical encoder 与身份

encoder 唯一，只认识 CompiledDefinition。三类身份分离：DefinitionDigest（拓扑/ID/策略）由制品字节摘要得出；PackageDigest（实现字节）由安装事务计算；PlanDigest（决策结果）由 runtime 计算。Compiler 同一生成动作产出制品与身份常量（生成 Go 源或嵌入字节），CI 含 freshness（重新生成无 diff）、assembly-order、round-trip、跨进程四类测试。

## 5. Spike 先行

正式实现前以六种代表性 step 完成 compiler spike：验证 IR 字段集、registry 绑定方式、encoder 字节稳定性与止损指标（新增普通节点不改 compiler core）。spike 结论写入开发记录后删除或隔离，不进入 production 路径。

## 6. Shadow harness

Shadow 从 legacy run 状态文件与外部事实构造 observation，运行 Decide 输出预测 frontier 与差异报告；全部只读，telemetry 落独立目录。从 installed candidate binary 在独立测试项目执行，证明与 stable 环境 namespace 不重叠。

## 7. 测试隔离修复（阶段 0 缺陷）

所有触及用户级 registry/安装路径的测试改为临时 HOME/registry root 注入；故障注入的桩替换只在隔离 namespace 内落盘并断言恢复。回归用例复现本次污染（真实 launcher 被写 25 字节桩）并在隔离环境下证明不再发生。
