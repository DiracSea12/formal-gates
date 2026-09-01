# 阶段 3.5a：QA 覆盖契约——技术方案

本方案实现 `stage-3-5a-qa-coverage-contract.md` 已确认的最小契约。它是阶段 4 Batch A 的
纯逻辑基座，并提供只做 JSON 编解码和函数转发的公开 `coverage` CLI，不接入现有 workflow、
legacy QA 或持久化状态。

## 实现边界

本方案中的下表是同一次确认内的方案约束 source，与需求文档中的 `REQ-3.5A-*` 共同组成
唯一的 `requiredSources`；没有额外 inventory。

| sourceID | 分类 | QA 适用性 | 交付义务 |
|---|---|---|---|
| `SOL-3.5A-001` | 方案约束 | QA | 以独立纯逻辑内部包实现封闭类型、validator、canonical digest、whitelist 投影和 execution 对账；由薄 CLI 适配层公开调用，不接入状态机或 legacy `QACase`。 |
| `SOL-3.5A-002` | 方案约束 | QA | 校验分层且确定，摘要对输入顺序稳定，错误能定位；不引入 registry、数据库、事件流或通用 schema/validation 框架。 |
| `SOL-3.5A-003` | 方案约束 | QA | 表驱动 fixtures 直接验证生产逻辑，并只更新阶段 4 的消费边界；实现保持解耦、简洁和最小充分。 |
| `SOL-3.5A-004` | 方案约束 | QA | 公开入口固定为 `formal-gates coverage validate`、`coverage project-whitelist`、`coverage reconcile-execution`；三者共用 JSON 输入/输出和稳定错误码，CLI 不维护第二份契约状态。 |

- 新增一个独立的 engine 内部包，承载封闭类型、结构校验、canonical digest、approved
  whitelist 投影和 execution 对账。
- 包不导入 `internal/validate`，不调用 VCS，不改变 `RunPhase`；CLI 只做输入解析、函数转发和
  JSON 输出，不把状态写入 workflow。
- 不修改 legacy `QACase`；阶段 4 负责把 regular QA/candidate 流程适配到本契约。
- 只实现已确认的 source→point→case→result 链，不增加 registry、数据库、事件流或通用
  schema/validation 框架。

## 类型与绑定

1. source binding 绑定 run/child、需求 revision 和方案 revision；`requiredSources` 本身就是
   已确认需求/方案中的权威义务列表，不实现第二份清单或自然语言解析器。
2. `RequiredSource` 区分产品需求与方案约束，并只记录 QA 或非 QA 分类。非 QA source 只能
   通过带 `PASS` 结果和证据摘要的替代验证处置闭环。
3. QA 设计直接在每个已选 review kind 的 `AcceptanceManifest` 中记录负责的 source，并将
   kind binding 与 route/topology scope 一起绑定。聚合校验要求每个 QA source 至少出现在一个
   已选 kind 的 manifest，拒绝未选 kind 或非 QA source 出现在 manifest。多 kind 同时出现时，
   各分支都进入完成条件。
4. `AcceptancePoint`、case 与显式 edge 构成 source↔point↔case 多对多关系；关系只保存一份，
   从 source 可查到全部 point/case，也可从 case 反查 point/source。一个 source 可有任意多个
   point/case。黑盒/合并 QA 使用公开执行入口变体，白盒使用测试引用变体；非法组合不可通过校验。
5. review 结果按 kind 逐 manifest source、逐 point、逐 case 返回。whitelist 只能由已校验输入
   投影生成，不提供独立可编辑来源。
6. execution 冻结 expected case set 并绑定当前 candidate。`AFFECTED` 的继承项必须带来源候选
   和有效复用证据摘要；没有证据的旧 PASS 不会提升为当前候选 PASS。

## 校验与摘要

- 校验按 source binding、manifest、ID/edge、review、whitelist、execution 六层顺序执行，
  返回稳定错误码和字段位置；不尝试修复输入。
- ID 与 edge 在复制后稳定排序，使用无 map 的 canonical JSON 计算 `sha256:` 摘要；调用方
  输入顺序不改变摘要，重复项在排序前后均被明确拒绝。
- `requiredSources`、manifest、map 和 whitelist 各有独立摘要；manifest 摘要包含该 kind 负责的
  source ID；execution 同时校验这些摘要、
  candidate identity 与 expected/actual case set。
- `AUTHORIZED_SKIP`、`PENDING`、`UNKNOWN` 和 `FAIL` 保留原语义，不能投影为 PASS。

## 验证与交付

- 用表驱动测试直接调用生产校验和摘要函数，覆盖需求文档列出的全部合法及拒绝 fixtures；另用
  CLI 黑盒用例验证三个公开入口的正常成功和常见校验失败输出。
- 执行 `gofmt`、`go test ./...`、race、phase0 whitebox、`go vet`、构建、package validation、
  portable canary 和 diff 检查；正式 run 中的独立 QA 与门审不由这些自测替代。
- 更新阶段 4 任务边界，使其在接入时消费本包的 `requiredSources` binding、manifest、map、whitelist
  与 candidate execution binding；不得把本 checkpoint 表述为 regular QA 已完成。
