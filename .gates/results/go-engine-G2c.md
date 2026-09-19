# G2c 验收记录（2026-09-19 深夜，ZCode 薄适配；原生关口待授权）

候选 `dfd3b75`（分支 `codex/go-engine-v1`，合同 `go-engine-G2c-contract.md`；首候选 `8289bb2` 因宿主漂移
事件前移，见下）。上一验收基线：`G2b` v2（候选 `3d769a0`）。九项验收全过（尝试
`.harness/validation/acceptance/G2c/20260919T220257Z-77445`，`overall=PASS`；运行时身份 `bc914748…`、驱动
sha256 按该次 identity.json）。

## 本单元搬了什么（薄层形态，机主预期"适配新平台＝只加一个薄文件"达成）

| 归属 | 内容 |
| --- | --- |
| **Go（新）** | `internal/harness/native_zcode.go`（单文件，1550 行）：636 行 Python 语义全量值级移植——常量组、catalogue 解析、capture、request_auth（机密只进协议应答）、respond 四路/classify/notification、选型机制（绝不替代）、账号供给全套（revision=目录版本＋解析路径 sha256、store 只读键名、setting.json family/kind 匹配、deliver 回读校验）、apply_selection（settings 权威回读）、**setMode 边界**（新会话声明一次 yolo＋回读不符即拒；resume 沿用快照绝不切换）、materialize_source 事件合同、reconcile（仅帧错误可重放）、execute 全流程（900s 有界等待、probe 报告）。测试 `native_zcode_test.go`（896 行）＋`native_zcode_unix_test.go`（879 行，假 app-server 驱动） |
| **公共传输（两处最小缺口）** | ①`newNativeClientWithEnv`：子进程环境覆盖（zcode 目录钉扎需要；DSH 传 nil 零变化）；②receive() 非请求入站消息进 notification 钩子（zcode session/event 恰是该形状；DSH 无该钩子不可观测）。**DSH 行为零变化**（差分与既有测试钉住） |
| **分派** | `executionAdapters={dsh,zcode}`＋`nativeImplementations` 表；四个桥接操作按 provider 路由；DSH 适配逻辑本体未动 |
| **Python（退役）** | `native_zcode.py`→`tests/reference_native_zcode.py`、`native_transport.py`→`tests/reference_native_transport.py`（逐字，仅 docstring＋导入，diff 亲验 38/30 行）；`native_entry.py` 删除（无进口方）；`native_hosts.py`：双 GoAdapter、`NativeAdapter` 整删、transport_files 按 provider；生产包零退役符号进口（grep 亲验） |
| **差分（新）** | `tests/test_native_zcode_differential.py`：22 例，假 app-server 镜像真宿主形状（含 projection 陈旧快照行为），reference 进程内 vs Go 独立进程，**值级比对**；凭证送达由 Go 单测＋Go 侧单边场景钉住（参照旁无 mjs，登记的侧不对称） |
| **准入机器** | `admission.py`：`UNITS_REQUIRING_NATIVE_VERIFICATION={G2a,G2b,G2c}`（Go zcode 实现＝该宿主执行实现首次接入累计组合）；控制项对 G2b 记录复跑 25/25 无回归（A 派发内），终签时对 G2c 记录全绿 |

## 主代理独立复核（不采信自述）

范围与还原（A 的临时 git 视图遮蔽已全部还原，`info/exleave`/assume-unchanged 清零亲验）；gofmt/build/
harness 全测/双平台 vet 亲跑；setMode 边界、凭证机密、有界等待、分派路由亲读；两处传输补齐 diff 亲审
（DSH 零变化）；参照副本逐字性 diff 亲验；Python 全套 **281 tests 亲跑 OK**；doctor-zcode 经注册表走
Go 探针（真宿主握手、`model_calls:0`、selection 只读宿主自报）。

## 回退保全（已闭合）

G2b 冻结版 `3d769a0` 隔离 worktree 重建＋公开入口冒烟 **DEMO_COMPLETE**、exit 0、零模型调用
（run `harness-20260919-230430-b8d3df21`）。证据 `.harness/validation/g2c-rollback-g2b-20260919/`；
重建二进制与签押件之差＝go build 内嵌路径（承 G2a/G2b 同一登记解释）。

## 宿主漂移事件与修复（2026-09-20，如实记录）

- 首次授权运行（run `harness-20260920-051650-834b2890`，授权原文「授权」）未完成：product/technical 两角色
  PASS；technical 首跑因 **GLM 回显 schema 关键字 `additionalProperties` 进答案**被 `validate-worker`
  严格字段检查拒绝（G1a 已验收的既有行为），retry 重跑通过；development 角色撞上**宿主 zcode.cjs 于
  9-19 09:51 的自动更新**——该作业的账号 overlay 未投递（读宿主 credentials.json 的瞬时竞态；六个作业
  绑定一致、三有三无），空注册表下 setModel 只回报黑盒错 `Provider Registry 中不存在 Model: [object Object]`。
  中断机制全程按设计停在 WAITING_USER，无洗白；该 run 保留为证据（共消费 4 个角色执行）。
- **零模型调用探针**（/tmp/zcode_setmodel_probe.py）实测新宿主：对象＋advertised `options.reasoningLevel`
  是 setModel 唯一被接受的形状（字符串被 params schema 拒），我们的帧本来就对；修复＝空 available 快照
  在发射 setModel 前以本方 `ZCODE_CONFIG: ... not advertised; no fallback` 拒绝（`dfd3b75`，差分假宿主
  镜像实测契约，24 例含两个新场景）。Python 全套 283、harness 测试、双平台 vet、doctor（0 模型调用）
  亲跑全绿。

## 原生关口（已闭合，2026-09-20，轻装收尾）

机主三次逐字授权「授权」。前两次运行如实保留为证据（run `harness-20260920-051650-834b2890`：宿主 9-19
自动更新引发 setModel 协议漂移＋模型回显；run `harness-20260920-065125-9a6760a1`：模型连续回显
schema 首键）。对应修复：`dfd3b75`（零调用探针实测宿主协议；空注册表可归因拒绝）、`e3e3556`（prompt
内嵌 schema 将 additionalProperties 渲染至末位，值级等价，校验严格性不动）。

**第三次运行一次通过**（候选 `e3e3556`）：run `harness-20260920-070903-cc867616`，**7/7 角色 PASS、
HARNESS_COMPLETE、0 中断、exit 0**；7 个原生作业 argv 全为 Go 窄命令、exit 0。证据
`.harness/validation/g2c-native-zcode-v3-flow-20260920T070900/`。

按机主 2026-09-20 裁定，G2c 自本修复起**不再走迁移方案的自造单元仪式**（acceptance 电池重跑/记录
重签循环/admission 关口），本记录如实标注：`dfd3b75` 与 `e3e3556` 两个修复候选的九项验收电池与全套
控制项未按旧仪式重跑（两侧测试套件、差分、双平台 vet、doctor 均绿，主代理亲跑）。

## 原生关口（历史：重开过程）

`native_verification.status=NOT_RUN` 如实落盘：G2c 已入原生必需集合，G2a 的 zcode 证据绑定的是已退役
Python 路径。重取＝一次授权 `native_multihost --provider zcode`（7 角色完整流程，绑定身份
`3ce9dd2c…`；zcode 普通会话上下文直接可跑，无需 launchd）。**未获机主授权前不消费任何真实模型调用**。

## 有界差异

八条全量见 `go-engine-G2c.json`（prompt schema canonical、错误内嵌 JSON canonical、native-events 行
canonical、出站帧承前登记、两处传输缺口、参照侧无凭证不对称、source 证明按侧自证、运行时身份按设计变更）。

## 提交链

`3d769a0`（G2b v2 候选）→ …G2b 终签链… → `8289bb2`（G2c 候选：适配＋退役＋差分＋admission 集合）→
本次记录提交（G2c.{json,md}＋索引）。
