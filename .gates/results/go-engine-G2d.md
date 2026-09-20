# G2d 原生交互 —— 交付与验证记录（轻装模式）

日期：2026-09-20。链头 `74aec71`（G2c 收尾）→ `7cf58b6`（G2d 持有机械）→ `7c85357`（删除强制 yolo）。
单元合同：`.gates/results/go-engine-G2d-contract.md`（持有/展示/投递/继续；§12.4 红利＝不动 graph）。

## 交付

- `7cf58b6`：适配器"当场拒答"→真持有。`interaction-pending.json`（宿主选项原样、稳定请求身份、连接代次、pending→delivered→observed | invalidated）；`native-respond`/`native-show` CLI；一票制消费（rename）；断连作废；三事实分离；zcode 900s 回合预算持有期暂停；zcode 1s 倍增重发的换帧重瞄（帧 id 不稳、requestId 为准）。回帧形状按宿主 bundle 反读：zcode 权限答复协议无 optionId，合规回法＝原样回显所选选项自带的 `response` 对象，取消＝-32021 错误帧；DSH `cancelled` 是协议一等形状。
- `7c85357`：删除新建非只读会话的无条件 `setMode("yolo")`（G2d 前权限请求必死时代的临时绕过）。缺省＝驱动完全不设模式、证据只记宿主自身回读值（没报记空）；显式 `permission_mode` 才 setMode 并校验回读；续用会话语义不动。

## 离线验证（主代理亲跑）

- `go build ./...`、`go vet`＋GOOS=windows/linux 双平台、gofmt（仅基线已知 `replace_file_windows.go`）全绿。
- `go test ./internal/harness/ -count=1` 绿（两提交各自跑过）；`go test ./...` 全量绿（`7c85357` 后）。
- Python 283 测试 OK（`7cf58b6` 时点；`7c85357` 只改 Go）。
- 新假宿主场景 11 项（同回合继续含 prompt 不重发断言、拒选项、双宿主各自取消形状、错 id/非宿主选项拒答、断连作废、双请求竞态、预算暂停、重发重瞄）。差分测试登记有界分歧：参考实现仍拒答、Go 持有后同回合完成、零答案帧不变。

## 实机验证（zcode，机主 2026-09-20 授权；探针目录 `.harness/validation/g2d-zcode-probe-20260920T{1..4}`）

- **T1**：失败于绑定校验 `HOST_CHANGED` —— **主代理搭请求时漏写 file proof 的 `size` 字段**所致误报，宿主文件未变（sha256/mtime 复核一致），产品行为正确。教训归主代理，非产品缺陷。
- **T2（缺省路径）**：无 `permission_mode` → 全程**零 setMode 帧**（协议轨迹 grep=0 证实）；宿主自身快照回读本机新会话模式＝`yolo`，`tool-policy.json` 如实记录；bash 直跑无权限请求、同回合 `{"done":true}`。结论：**本机宿主给嵌入会话的默认模式就是 yolo**——缺省路径行为正确，且如实暴露了这台机器上权限请求不会自然发生的事实。
- **T3**：`permission_mode=default` 被宿主 setMode 参数 schema 在会话建立期拒绝（合法值 `plan|build|edit|yolo|auto`），零模型调用。教训：zcode bundle 里有两套模式枚举（UI 注册表含 default/acceptEdits 等），**setMode 参数 schema 才是权威**；已按权威枚举重选。
- **T4（`permission_mode=build`＝"Ask before changes"）——核心证据链全通**：
  1. 真实 `interaction/requestPermission` 到达（`Bash: High risk tools require explicit approval`，requestId `perm_c5e8d301…`），宿主三选项（allow_once/allow_project/deny，含各自 `response` 对象）**原样**落 pending 记录；
  2. **持有 ≥70 秒不死**（其中刻意空等 45s；期间宿主按 1s 倍增换帧 id 重发 10 次，传输层重瞄最新帧、pending 恒为单条）；
  3. `native-respond --option allow_once` 投递（exit 0；答案文件被 rename 消费）；
  4. **同回合继续**：投递后 ~5s 作业完成，流中 turnId 唯一、prompt 未重发、标记文件写入、最终 `{"done":true}`；
  5. pending 生命周期走全 `pending→delivered→observed`；`tool-policy.json` 记宿主回读 `build`；
  6. 消费：2 次模型请求、35,795 tokens（provider 侧证据）。

## 未覆盖（如实）

- DSH 实机（计划二批，需另行授权）；拒绝/取消路径仅离线假宿主证据，实机未跑。
- 实机未验：zcode `requestUserInput` 真请求、多问题/多选（合同本就只呈递不代答）。

## 外部评审驳回与修复（2026-09-20，`43e03e4`）

评审（对象 `1bbcdba`）判 FAIL：3 P1＋2 P2。五项全部主代理先亲复现再派发修复（Go/Python 两子代理并行，文件集不相交）：

- **R1/P1 答复后误报宿主超时**（已复现：预算 3s/用户等 5.2s/宿主 1.2s 继续仍报 `ZCODE_NATIVE_TIMEOUT`）。修复＝`remainingBudget()` 惰性把持有暂停折回 deadline，超时分支先重算、预算未尽续等。评审夹具 `TestReviewBudgetAfterUserAnswer` 现通过。
- **R2/P1 无关消息伪造 observed**（已复现：telemetry 心跳即翻 `delivered→observed`）。修复＝到达序号严格晚于投递＋适配器归属判定（zcode：`session/event` 且命名被持有请求的 session/turn/tool 之一；DSH：该 session 的 `session/update` 或 prompt 响应；不可归属诚实停在 delivered）。**评审夹具 `TestReviewUnrelatedTrafficMustNotConfirmDecision` 修复后仍红，经定时探针（`/tmp/g2d-probe`，telemetry-only 3 秒窗口采样）实证为夹具自身误报**：其 `waitStatusContent` 30 秒窗口不检查时序，抓到的是 +1 秒**合法** turn 事件的观察（修复前 telemetry 0.44s 即翻转＝真缺陷；修复后 1.67s 由合法事件触发＝正确行为）。仓库内移植回归 `TestInteractionZcodeUnrelatedTrafficMustNotConfirm`（显式 2 秒 delivered 窗口）钉住真实语义。
- **R3/P1 问题正文丢失**（已复现："Pick one" 顶替了实际问题）。修复＝宿主原样持久化（userInput 的 prompt+完整 questions；zcode 权限 toolName+input；DSH toolCall），summary 用实际问题正文。评审夹具 `TestReviewQuestionMustBeVisible` 现通过。
- **R4/P2 run 级 show 未接入**（已核实投影只走 LangGraph interrupts）。修复＝`status_projection` 只读扫描 `jobs/*/interaction-pending.json`，新键 `native_interactions`，有待决时状态 WAITING_USER 与 LangGraph interrupt 同级；纯只读（投影前后整树字节不变有测试钉）。
- **R5/P2 差分 4 失败**（已亲跑复现，58 项恰 4 失败）。修复＝4 处断言改按新行为＋有界分歧登记（参考实现冻结强制 yolo vs Go 缺省不设模式）；新增差分 `test_default_new_session_runs_the_hosts_own_mode`。未恢复强制 yolo。
- 注释按评审可读性意见降级为只述真实不变量（"任意流量＝原 turn 已继续"删除；connection 代次改口为进程内记录标签）。

修复后统一亲验（重建二进制后）：评审夹具 R1/R3 过、R2 由探针＋仓库回归钉住；差分 59 全绿；Python 全量 287 全绿；`go test ./internal/harness/`＋race 过；`go test ./...` 全仓两遍零失败；gofmt/双平台 vet 干净。未消费模型调用。未覆盖项同前（DSH 实机、实机 deny/cancel、实机 requestUserInput）。

## 第二轮评审返修（2026-09-20，`c438713`）

第二轮评审（对象 `c754dc6`）确认 R1/R3/R5 关闭，剩两项未修完整，均主代理先亲复现（评审 r2 夹具 `/private/tmp/g2d-review-r2/`，zcode titleUpdated 与 DSH available_commands_update 双双翻 observed 坐实；F2 投影证据亲核）再派发：

- **F1/P1 同会话元数据伪造 observed**：判据收紧到回执/进度语义——zcode 仅认 ①`permission.resolved` 且 `payload.requestId`＝被持有 requestId（宿主回执，字段形状抄 T4 真迹 seq 57）②`tool.updated` 且 `payload.toolCallId` 匹配 ③`turn.completed`/`turn.failed` 且顶层 `turnId` 匹配；DSH 仅认在途 prompt 的响应或 `session/update` 内容命名被持有 `toolCallId`（工具卡进度）；titleUpdated/available_commands/遥测一律停在 delivered。夹具词汇对齐真宿主，新增双宿主判据真值表单测。
- **F2/P2 run 级投影裁正文**：`native_pending_interactions` 改排除法透传（只剔 status/connection/updated_at，正文键由 Go 适配器单一出处决定、缺省不出现），权限 input 与逐题 questions 在 `harness show --run` 可见。

**r1 夹具旧 R2 用例说明（如实）**：`/private/tmp/g2d-review/` 的 `TestReviewUnrelatedTrafficMustNotConfirmDecision` 在任何正确实现下都不可能通过——其回调在 observed 出现时无条件报错、永不出现则 30 秒 Fatal，两条路都是失败；评审自己在 r2 已用 150ms 时序窗版本重写同一检查（该版本通过）。此为夹具结构缺陷非回归，上轮定时探针与仓库内回归（telemetry+titleUpdated 后 500ms 窗口断言 delivered）继续钉住真实语义。

统一亲验（`c438713`）：r2 夹具 **4/4 全绿**（含 titleUpdated 150ms 时序与 DSH 元数据两新用例）；r1 夹具 3/4（唯一红即上述结构性缺陷用例）；主代理定时探针过；差分 59 全绿；Python 全量 288 全绿；harness 包＋race 过；gofmt/双平台 vet 干净；`go test ./...` 全仓零失败。零模型调用。未覆盖项不变（DSH 实机、实机 deny/cancel、实机 requestUserInput）。

## 有界差异登记

- zcode bundle 双模式枚举（UI 注册表 vs setMode 参数 schema），以参数 schema 为权威。
- 本机宿主默认模式＝yolo：缺省路径作业在本机不触发权限请求（宿主模式说了算，属设计内行为）。
- 差分登记（test_native_zcode_differential.py 模块清单）：参考实现冻结"强制 yolo"历史行为 vs Go 缺省不设模式只记宿主自报；显式 permission_mode 且回读不符双侧同拒；resumed/read-only 双侧都不切模式。

## 第三轮复审（2026-09-20，PASS 附一项 P2，已修 `692dab3`→amend 收录记录）

第三轮（对象 `6fa5a63`）结论 PASS，附非阻塞 P2：DSH 工具进度判据读 `update.content.toolCallId`，真实宿主（dsh-acp toolCallUpdate/toolResultUpdate）把 `toolCallId` 放 update 顶层——真实进度永不匹配、observed 滞后到 prompt 响应。主代理 10 秒 grep 亲核真实源码坐实后最小范围派发，3 分钟交付：判据改读顶层 `toolCallId` 并限定 `sessionUpdate∈{tool_call, tool_call_update}`（逐一核对真实宿主，未放宽），两处测试夹具与注释同步改真实形状。窄验证：r3 夹具 5/5 全绿（含 TestReviewDSHRealToolProgress）、harness 包全绿、差分 59 全绿、gofmt 干净。零模型调用。DSH 实机/实机分支覆盖缺口不变。
