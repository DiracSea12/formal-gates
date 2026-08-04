# Two-Phase Pre-Development Review

Date: 2026-08-04
Status: confirmed
Route: full

## Complete confirmed requirements

1. **需求澄清拷问化。** `prompts/actions/requirements-clarification.md` 的受理澄清从
   "只问那些答案会改变范围、验收、公开行为或必需架构边界的问题"改为**拷问纪律**：逐项
   对齐所有有实质影响的细节，不留任何需要主代理猜测或假设的余地。不区分产品问题还是技
   术问题——范围、验收、公开行为、架构、产品合理性、细节、边界、异常、默认值、取舍，只
   要实质且含糊就追问到明确。防走过场的硬纪律：系统性逐维拷问（目标动机、用户场景、范
   围边界/非目标、细节与默认值、异常与失败路径、验收标准、约束与取舍、风险）；记录拷问
   轨迹（问了什么、用户答了什么）；记录"考虑过但放过"项（放过须有理由且不构成实质猜
   测）；返回 PASS 前置自检"是否还有任何有实质影响的细节是我不得不猜的"，有就不 PASS。
   仍然一次只问一个有实质影响的决策，用日常语言讲清背景与选项；拷问不是连珠炮，是不放
   过每个含糊点直到没有残留猜测。

2. **start-readiness 拆为两部分、顺序执行。** 正式流程开发前检查拆成两个独立动作：
   - **Part 1 产品审**（新动作 `product-review`）：独立零上下文代理从产品/策划视角审
     "需求本身是否合理、需求细节是否合理"，含原 start-readiness 的"是否针对真正的用户
     问题"判据。
   - **Part 2 技术审**（`start-readiness` 收敛为纯技术）：保留"是否有更简单的既有归属
     者/原生能力""是否足够且最简的做法"及全部开发就绪判据（遗漏需求、实质技术选择、验
     收、风险、方向错误、范围削减、架构阻塞、可验证性）。
   执行顺序：Part 1 先行；Part 1 全部通过后，Part 2 技术审与 QA 用例设计/用例审
   （qa-design → qa-review）并行推进。

3. **Part 1 发现项由用户逐项拍板。** 产品审返回的每个发现项由主代理转达给用户逐项处
   置：用户**接受**该发现项 → 该项视为通过（记录为已决策、不阻塞）；用户**未接受** →
   按用户指示修订需求/方案，然后用**全新的零上下文审查者**重新产品审，循环直到全部项
   通过或被接受。用户决策并入已确认需求，作为重审任务的合法上下文输入。产品审本身不产
   生终态 FAIL——需求是否成立由用户决定，不由代理决定。

4. **解锁条件升级。** 开发 worker 准备、开发快照、开发后门审、Seal 的前置条件由
   "start-readiness 必须 PASS"升级为"start-readiness 与 product-review 均必须 PASS"；
   product-review 未 PASS 时不得准备 start-readiness 与 qa-design（qa-review 经
   qa-design 传递性阻塞）。Part 2 技术审 FAIL 仍退回修方案后重审。

5. **提示词精简、改旧优先。** 提示词保持精简；优先修改现有提示词，新增是次要手段。
   `product-review.md` 是唯一新增文件，内容基本搬移 start-readiness 的产品判据并沿用现
   有审查结构，净新增文字最少。项目文件不得出现任何外部产品关键词。

## Confirmed technical solution

- 新增 CLI 动作 `product-review`：
  - `prompts/actions/product-review.md` 新提示词（产品/策划视角审查 + 项目范围边界，
    保持精简）；
  - `internal/validate/catalog.go` 的 `requiredActionIDs` 登记 `product-review`；
  - `internal/validate/runner.go` 的 `actionResultContract` 增加产品审结果契约；
  - `RecordAction` 允许列表加入 `product-review`；
  - `internal/validate/workflow.go` 的 `requireTransition` 增加 product-review 用例，
    并把 start-readiness、qa-design 的准备前置改为"product-review 已 PASS"，
    development/snapshot/gate/seal 的前置改为两者均 PASS。
- 改写 `prompts/actions/requirements-clarification.md`：拷问纪律，保持精简。
- 修订 `prompts/actions/start-readiness.md`：去掉"是否针对真正的用户问题"产品判据，收
  敛为纯技术/就绪检查。
- `SKILL.md`：受理段升级为"拷问并对齐所有有实质影响的细节、不留猜的余地"；正式流程第
  4 步改写为两段式开发前检查（Part 1 先行 → Part 2 + QA 并行）。
- `references/formal-flow.md`：增加 product-review 命令映射与顺序说明。
- 扩展 `internal/validate/canary.go`；更新 `catalog_test.go`、`workflow_test.go`、
  `cost_test.go` 及 canary 相关测试。

## Acceptance evidence

- `go test ./...`、`internal/validate` 与 `internal/cli` 的 race、`go vet ./...`、原生
  构建、package 校验、portable canary、`git diff --check` 全部通过。
- 新动作 product-review：目录校验（`requiredActionIDs` 与提示词文件精确匹配）、状态转
  换（product-review 未 PASS 时 start-readiness/qa-design/development/snapshot/gate/
  seal 均被拒绝，两者均 PASS 后放行）、canary 派发+PASS 均被测试覆盖。
- 需求澄清拷问化后，澄清提示词仍保持精简，且项目文件不出现外部产品关键词。
