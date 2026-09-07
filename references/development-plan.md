# 开发批次登记

技术审的 `granularity_review` 确定批次划分后，把同一划分写成下面的 JSON。
用户或 AI 都可以直接编辑；它是工作安排，不是运行状态文件。一个批次也有效，
`reason` 说明为什么这样划分。批次不是 run 切片，不需要另建 run 或另走一轮受理。

```json
{
  "reason": "先交付解析能力，再接入调用入口；两批之间以解析接口交接",
  "subtasks": [
    {"id": "parse", "task": "实现输入解析"},
    {"id": "parse-test", "task": "验证正常输入与错误输入"},
    {"id": "entry", "task": "把解析器接入公开入口并验证"}
  ],
  "batches": [
    {
      "id": "parser",
      "task": "交付可独立验证的解析器",
      "subtaskIds": ["parse", "parse-test"],
      "dependsOn": [],
      "interfaces": "parse(input) 返回解析结果或输入错误",
      "stateBoundary": "只处理输入，不写业务状态",
      "risks": "错误输入被误接受",
      "dod": "正常输入可解析，错误输入有明确错误",
      "validation": ["go test ./internal/parser"],
      "recovery": "撤销本批提交；尚无入口依赖它",
      "handoff": "说明接口决定、已知问题和下一批接入任务",
      "contextCost": "一个解析模块，由同一开发代理完成"
    },
    {
      "id": "entry",
      "task": "交付使用解析器的公开入口",
      "subtaskIds": ["entry"],
      "dependsOn": ["parser"],
      "interfaces": "公开命令调用 parse(input)",
      "stateBoundary": "入口负责调用，解析器负责输入校验",
      "risks": "入口丢失解析错误信息",
      "dod": "从公开入口验证正常和错误输入均符合要求",
      "validation": ["go test ./..."],
      "recovery": "撤销入口接入提交，保留已完成解析器",
      "handoff": "说明待审查的交付物和已知问题",
      "contextCost": "读取上一批接口和交接信息后完成接入"
    }
  ]
}
```

`subtasks` 是完整子任务清单，`subtaskIds` 按批内执行顺序列出归属。每个子任务必须恰好
属于一个批次。每个批次都必须写出接口、状态边界、风险、DoD、验证、恢复、交接和上下文
成本；没有相关项时写出不适用的理由。未知字段报错，不会默默丢掉手改或 AI 生成的字段。

在已确认需求、产品审和技术审通过后，随拆分决定登记：

```bash
formal-gates workflow slicing --root <repo> --package-root <package> \
  --run-id <id> --decision no-split --note '<不拆 run 的理由>' \
  --batch-plan <plan.json> --user-confirm
formal-gates workflow route --root <repo> --package-root <package> \
  --run-id <id> --mode full
```

分片开发时，在各实现切片记录其 `split` 决定的命令中附上 `--batch-plan` 和
`--user-confirm`；保留总任务实例不拥有开发批次。已经记录同一拆分决定但尚未开始开发的
run，也可以用相同拓扑的 `slicing` 命令补登记，保留原路线。

当前驱动一次执行一个开发批次，按文件顺序运行。依赖必须指向前面的批次，因此依赖无环、
不会先运行消费者；没有依赖的批次也按登记顺序执行。每批用全新的开发代理，规范提示词
自动包含本批全部契约和上一批交接。子任务是同一派发内的步骤，不会另派代理；本次登记
功能不提供对子代理每次工具调用的步骤拦截，该能力由阶段 4 的执行引擎交付。

`Batch → Subtask` 映射在登记时冻结。CLI 准备派发时，将计划摘要和批次 ID 纳入现有
`development-worker` 的 TaskKey，结合当时的原生快照、需求和提示词修订机械生成。
用户不填写 TaskKey；所有成员子任务都归属于该派发。完成进度和 TaskKey 映射从派发记录
推导，`workflow show` 的计划保存契约，最终结果的 `developmentBatches` 保存派发及验证证据。

每批完成 DoD、自测、提交和交接后记录（包括最后一批）：

```bash
formal-gates workflow snapshot --root <repo> --package-root <package> \
  --run-id <id> --dispatch <dispatch-id> --batch-id <batch-id> \
  --verification '<实际 DoD 和验证结果>' --handoff '<决定、已知问题、下一任务>'
```

CLI 检查当前批次、派发身份、生命周期结束、原生提交和非空完成证据后才解锁下一批。
验证结果由执行者按事实提交，登记器不解析自然语言来判断测试是否通过。批间不启动门审；
最后一批进入既有开发快照流程，仍须满足已选黑盒 QA Review 的快照条件。

中断时先恢复原派发；原代理确实不能恢复时，使用既有替换派发路径。同一需求修订的
`workflow reset` 保留已完成批次，重过被重置的前置检查后，从未完成批次继续。
改变批次契约、接口、依赖或 DoD 时，将变化写入已绑定需求/方案，经现有
`workflow requirement --meaning changed` 路径重新确认和技术审查，再登记替代计划。
旧完成记录不会冒充新计划的完成。仅排版等含义不变的需求修订使用 `--meaning preserved`，
保留既有计划与完成记录。登记后再编辑输入 JSON 不会改变已冻结计划。

旧版已登记计划继续按原规则完成：非最终批次使用 `--batch-id`；最终批次使用普通
`workflow snapshot`。不要求用户补填历史任务的子任务和契约字段。
