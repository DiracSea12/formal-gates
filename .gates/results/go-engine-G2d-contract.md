# G2d 单元合同（轻装）—— 原生交互：请求持有、展示、投递、原调用继续

日期：2026-09-20。基线：G2c（HEAD `74aec71`）。依据：方案 §13.1 G2d 行（"单独交付原生请求持有、展示、投递和原调用继续；按宿主逐个验收"；出口"允许/拒绝/断连/超时/多请求竞态有证据；前端恢复不重发模型任务"）、§6.2/6.3/6.4（交互合同全文）、§12.4（交互通路与宿主迁移分别验收；`native-respond` 回答；不算流程批准；重入只重附着不重发 prompt）。

## 形态（§12.4 的关键红利：不改 graph/不建中断）

原生作业进程（监督器子进程，G2a 已支持接回）内完成全部四段：

1. **持有**：respond 钩子遇权限请求不再拒——落 `interaction-pending.json`（请求 ID、方法、连接代次、正文摘要、宿主原始选项、status=pending），不发帧，receive 循环继续排水（心跳/通知/关闭）。
2. **展示**：`harness show --run` 增列活动 job 目录的 pending 原生交互（读上述文件，只读）。
3. **投递**：新 CLI `harness native-respond --job <dir> --request <id> (--option <optionId> | --cancel)`——校验请求仍 pending 且选项是宿主提供的，原子写 `interaction-answer.json`。作业侧轮询到答案→按原请求 id 回帧（携带确切 optionId/取消语义）→pending 转 delivered→宿主继续原 turn。
4. **继续**：同一 turn 出结果，prompt 不重发；`interaction-status.json` 照记（现有机制扩展）。

## 语义红线（§6.2/6.3，逐条落地）

- 一个连接代次一个请求只投递一票；答案严格对 request_id，A 的答案不应用于 B。
- 三事实分开记录：用户决定已持久化（answer 文件）／已向原连接投递（帧已发）／原生后续已观察（turn 事件）；无协议确认能力时如实报"已投递"。
- 不自动同意/拒绝/替选"记住此决定"；只呈现宿主给的选项，不加选项。
- 断连/宿主关闭→pending 置 invalidated（原请求失效，旧 optionId 不再是授权）；用户等待不计失败，宿主自身超时记真实原因；zcode 的 900s 回合预算在 pending 期间暂停。
- requestUserInput 以独立交互类型同路径呈递（§6.4），不当流程批准。
- 监督器零改动；graph/workers 的节点等待不动（作业长跑是既有语义）。

## 宿主接线与验收顺序（先一后二）

- 公共机械落 `native_transport.go`；两个适配器各自只声明其权限方法名与回帧合同（回帧字段形状从 `zcode.cjs` 与 `native_dsh.mjs` 本地反读实证，不猜）。
- **先验 zcode**（已知真实权限路径：非 yolo 新会话的 build 模式审批；离线差分＋真实探针——探针会话显式不设 yolo，属测试配置而非"处理权限时改模式"）。DSH 实机验证随后第二批；两宿主离线差分都先行。

## 测试与门（轻装）

假宿主差分用例：pending→答复→同 turn 继续；拒绝选项；取消；错误 request_id/非宿主选项被拒；断连失效；双请求竞态（答 A 不误 B）；zcode 预算暂停。Go 单测＋unix 假宿主＋既有套件全绿；gofmt/双平台 vet；`doctor` 0 模型调用。真实探针（zcode build 会话真实权限请求→答复→原 turn 完成）到关口单独请授权。零仪式：一次提交＋末尾记录。
