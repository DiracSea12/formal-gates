# G2c 单元合同 —— ZCode 薄适配（同一公共边界，Python 原生路径退役）

日期：2026-09-19。基线为 **G2b v2**（候选 `3d769a0`，链头准入 `ADMISSION GRANTED ... for G2c`，身份 `cc213960…`）。

依据：§13.1 G2c 行——"使用**同一公共边界**移植 ZCode；**DSH 实现不同时重构**"，出口"**冷恢复、原生模式保留、工具继承和原生完成证据通过，无隐式供应商或模式切换**"；§12.5（520 行首次接入须真跑；502/515/526 基线冒烟、回退保全、记录含回退入口）；机主 2026-09-19 去仿真化裁定（同 G2b 合同 v2 头部，本单元从第一天起就按值级等价实施，无 Python 仿真）；机主 2026-09-19"适配新平台＝只加一个新平台自己的最薄文件"预期；G2b v2 全部先例（canonical 序列化、差分值级、结构化错误）。

## 0. 形态

- **Go**：`internal/harness/native_zcode.go` 单文件（同包，公共传输零改动为预期），经既有 `nativeHooks`（respond/classify/notification）挂 zcode 协议差异；`executionAdapters` 加 `"zcode"`；`harness native <dir>` 窄命令与监督器照旧。**若移植中发现公共传输确有 zcode 需要的缺口（如 DSH 的 UNBOUNDED 等待 vs zcode 的 900s 有界回合等待），最小补齐并登记；不得顺手重构 DSH 路径**。
- **Python 退役**：`native_zcode.py` 逐字移入 `tests/reference_native_zcode.py`（相对导入改绝对，沿 reference 先例）；`native_transport.py`、`native_entry.py` **删除**（无消费者）；`native_hosts.py`：`_ADAPTERS = {GoAdapter('dsh'), GoAdapter('zcode')}`，`NativeAdapter` 类删除，zcode `transport_files()==('zcode_credential.mjs',)`；`workers.py` 已走 `adapter.entry_argv` 无需改。
- **不动**：`zcode_credential.mjs`（宿主侧插件，机密只经它解密）、`native_dsh.mjs`、DSH 全链、hook 平台层。

## 1. 等价口径（承 v2 裁定）

- 值级功能等价；**无 CPython 仿真**。prompt 内嵌 schema 用 `CanonicalJSON`（有界差异，同 G2b 登记）；selection/advertised 的比较按 canonical 值。错误族前缀（RuntimeBlocked/SessionUnavailable/HarnessError）与 `ZCODE_*` 协议文本是功能数据，逐字保真。

## 2. 移植清单（`native_zcode.py` 636 行 → `native_zcode.go`）

1. 常量组：`READ_ONLY_TOOLS`、`DEV_PERMISSION_MODE='yolo'`、`PROTOCOL_NAME/VERSION`、`SESSION_UNAVAILABLE=-32004`、`DEFAULT_SCRIPT`、catalogue/personal 两个 env 名、credentials store 路径与键前后缀、`CREDENTIAL_HELPER`、`RUNTIME_HEADERS_METHOD`、`ACCOUNT_CONFIG_METHOD`、`setting.json` 键组、`RESULT_FRAME_ERROR`。
2. catalogue 解析：`bundled_provider_catalogue`/`provider_environment`/`pinned_catalogue`（宿主自身解析可用时返回 None 保持现状）。
3. `capture_binding`（node＋script；catalogue 进 launch env 但不作执行证明）。
4. `request_auth`（派生 node＋`zcode_credential.mjs` 读宿主凭证；**机密只进协议应答，绝不落任何证据/日志/trace**）。
5. Client hooks：argv `[node,script,'--cwd',workspace,'app-server']`；`respond` 四路（requestRuntimePreferences→-32601 原生回退、requestOfficialMcpAuthHeaders→unavailable、requestProviderRuntimeHeaders→凭证应答/缺证如实拒绝、requestPermission/requestUserInput→`ZCODE_INTERACTION_REQUIRED`）；`classify`（resume `-32004`+`Session not found:`→SessionUnavailable）；`notification`（session/event 按绑定 identity 过滤后落 native-events.jsonl；子会话证据不入根）。
6. 选型机制：`reference`/`selected_model`/`selection`/`advertised_models`/`advertised_reasoning`/`bound_model`——只读宿主自报值，**绝不替代选择**；不可达/未广播即拒。
7. 账号供给全套：`builtin_provider_revision`（revision＋解析路径 sha256）、`catalogue_account_rules/providers`、`host_selected_account_provider`（setting.json family/kind 匹配）、`provisioned_account_providers`（credentials.json 键索引，值不读）、`account_provider_configuration`、`deliver_account_providers`（回读 status/providerCount 校验＋`account-provider.json` 证据）。
8. `apply_selection`（setModel/setThoughtLevel 带宿主回读；**settings 是权威确认，projection 块可为陈旧快照**——G2a 实测教训，必须沿）。
9. **setMode 边界**（`2d36375` 修订逐语义保留）：仅 NEW dev/返修会话在首 turn 前声明一次 `yolo`，settings 回读不符即 `ZCODE_CONFIG` 拒绝；resume 会话沿用宿主快照模式**绝不切换**；绝不在询问/超时/失败后改模式；只读/探针不设。
10. `materialize_source`（zcode 事件合同：按 inputId/turnId 关联 turn.started/failed/completed，seq 单调，resultType=success 才算完成；`write_answer` 前缀 `ZCODE`；`usage_evidence` 透传）。
11. `reconcile_completed_output`（仅 `RuntimeBlocked: NATIVE_RESULT:` 帧错误可重放；运行时/传输失败不得洗白）。
12. `execute` 全流程：probe 路径（`titleGenerationEnabled=false`、close、返回探针报告形状含 `model_calls:0`）；read_only `toolAllowlist`＋`tool-policy.json`；900 秒有界回合等待（receive 循环）；`native-source.json` 转绑定（含 permission_mode 实况非常量）；完成后 `source.json`。

## 3. Python 侧与测试

- 新增 `tests/test_native_zcode_differential.py`：假 app-server 夹具（镜像真宿主协议形状：handshake、snapshot settings、runtimeHeaders 反向请求、-32004 resume 拒绝、session/event 流），驱动 reference 与 Go 两侧独立目录，**值级比对**（identity/selection/account-provider/answer/tool-policy/source 等证据按 JSON 值；协议行归一后按值）。
- 既有 zcode 用例（`test_native_hosts.py`/`test_native_inheritance.py`）按新归属收窄或移入差分；`native_missing_session.py`/`native_multihost.py` 经注册表自动走 Go 路径须仍通过。
- `test_native_hosts.py` 对 GoAdapter 的 zcode 断言（entry_argv、transport_files）同步。

## 4. 准入机器改动

- `admission.py`：`UNITS_REQUIRING_NATIVE_VERIFICATION` 增 `"G2c"`（Go zcode 适配器＝该宿主执行实现**首次接入累计组合**，§12.5 语义；G2a 的 zcode 证据绑定的是已退役 Python 路径）。
- **改 admission 必跑全套 admission-controls**（G2a 教训），含一次性 fixture 正控。

## 5. 验收门

gofmt（触及文件）→ `go build ./...` → `go test ./...` → `GOOS=windows`/`GOOS=linux` vet → Python 全套（`TMPDIR=/private/tmp PYTHONPATH=$PWD/runtime/langgraph`）→ zcode 差分值级通过 → `doctor --provider zcode`（0 模型调用，经 Go 路径）→ admission-controls 全 OK → **原生关口（唯一预定停下点）**：`native_multihost --provider zcode` 一次授权真跑绑定新身份（zcode 在普通会话上下文直接可跑，无需 launchd）；未授权前 `native_verification.status=NOT_RUN` 如实落盘 → `acceptance.py --unit G2c --previous-baseline G2b` → 记录三元组（**含回退保全段**：G2b 冻结版 `3d769a0` 隔离重建＋公开入口冒烟，零模型调用）→ admission GRANTED。

## 6. 范围排除

DSH 实现重构（§13.1 明禁）、G2d 原生交互答复通路（requestPermission 仍如实 `ZCODE_INTERACTION_REQUIRED`）、`*.mjs`、hook 平台层、`GOFMT-WINDOWS-FILE-PREEXISTING`（独立登记项）。
