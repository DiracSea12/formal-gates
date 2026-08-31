# 阶段 3.5a：QA 覆盖契约——技术方案

本方案实现 `stage-3-5a-qa-coverage-contract.md` 已确认的最小契约。它是阶段 4 Batch A 的
纯逻辑基座，不接入现有 workflow、legacy QA 或持久化状态。

## 实现边界

- 新增一个独立的 engine 内部包，承载封闭类型、结构校验、canonical digest、approved
  whitelist 投影和 execution 对账。
- 包不导入 `internal/validate`，不读写文件、不调用 VCS、不改变 CLI 或 `RunPhase`。
- 不修改 legacy `QACase`；阶段 4 负责把 regular QA/candidate 流程适配到本契约。
- 只实现已确认的 source→point→case→result 链，不增加 registry、数据库、事件流或通用
  schema/validation 框架。

## 类型与绑定

1. 一个公共 binding 绑定 run/child、review kind、需求 revision、方案 revision、route 和
   topology scope；下游对象必须精确复用同一 binding。
2. `RequiredSource` 区分产品需求与方案约束，并携带已确认的 QA applicability。非 QA source
   只能通过带 `PASS` 结果和证据摘要的替代验证处置闭环。
3. `AcceptancePoint`、case 与显式 edge 构成 source↔point↔case 多对多关系。黑盒/合并 QA
   使用公开执行入口变体，白盒使用测试引用变体；非法组合不可通过校验。
4. review 结果逐适用 source、逐 point、逐 case 返回。whitelist 只能由已校验输入投影生成，
   不提供独立可编辑来源。
5. execution 冻结 expected case set 并绑定当前 candidate。`AFFECTED` 的继承项必须带来源候选
   和有效复用证据摘要；没有证据的旧 PASS 不会提升为当前候选 PASS。

## 校验与摘要

- 校验按 binding、ID/edge、review、whitelist、execution 五层顺序执行，返回稳定错误码和
  字段位置；不尝试修复输入。
- ID 与 edge 在复制后稳定排序，使用无 map 的 canonical JSON 计算 `sha256:` 摘要；调用方
  输入顺序不改变摘要，重复项在排序前后均被明确拒绝。
- source inventory、manifest、map 和 whitelist 各有独立摘要；execution 同时校验这些摘要、
  candidate identity 与 expected/actual case set。
- `AUTHORIZED_SKIP`、`PENDING`、`UNKNOWN` 和 `FAIL` 保留原语义，不能投影为 PASS。

## 验证与交付

- 用表驱动测试直接调用生产校验和摘要函数，覆盖需求文档列出的全部合法及拒绝 fixtures。
- 执行 `gofmt`、`go test ./...`、race、phase0 whitebox、`go vet`、构建、package validation、
  portable canary 和 diff 检查；正式 run 中的独立 QA 与门审不由这些自测替代。
- 更新阶段 4 任务边界，使其在接入时消费本包的 source inventory、manifest、map、whitelist
  与 candidate execution binding；不得把本 checkpoint 表述为 regular QA 已完成。
