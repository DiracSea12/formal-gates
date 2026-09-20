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

## 有界差异登记

- zcode bundle 双模式枚举（UI 注册表 vs setMode 参数 schema），以参数 schema 为权威。
- 本机宿主默认模式＝yolo：缺省路径作业在本机不触发权限请求（宿主模式说了算，属设计内行为）。
