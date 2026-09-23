# G2b 单元合同 v2 —— 去仿真化重写

日期：2026-09-19。**取代 v1 及其收紧版的字节等同条款**；v1 的形态定夺（窄命令、单包、退役路径、退出码语义）继续有效，摘要于下。

依据：
1. 机主裁定原文："谁允许机械仿真了？用go实现功能就行了ok？重写不是搞形式主义！以简洁性优雅性维护性优先！"
2. 外部评审（对象 df33e9c→9d12645）：三项验收缺口＋两条 P3。
3. 方案 §13.1 G2b 行（573 行）：职责"在已验收监督器下移植 DSH 请求、身份、结果和错误映射；ZCode 保持原实现"，出口"新建、续用、只读约束、原生配置继承、根/子会话关联均通过"；§12.5（520 行）首次接入须跑通完整原生小流程；§14.2 表（655 行附近）"只读原生/子代理写入：真正尝试过并被约束"；§12.5（502/515/526 行）基线冒烟、回退保全（冻结版本、启动入口、只读核验及回退冒烟记录）、记录须含回退入口；§15 反过度工程。

## 0. 形态（承 v1，不变）

- `formal-gates harness native <dir>` 窄内部命令：一个原生作业一个进程，作为监督器子进程运行，监督器零改动。
- 全部落 `internal/harness` 单包；`native_dsh.py` 退役为 `tests/reference_native_dsh.py`（行为参照 oracle）；zcode 生产路径不动；`native_dsh.mjs` 原地不动。
- 4 个桥接操作（native-capture / verify-binding / verify-source / reconcile）的请求/应答/拒绝语义按 v1 表不变（结构化拒绝走 `kind`/`refusal`）。
- 退出码语义不变：0＝成功（结果 JSON 到 stdout，单行）；1＝任何执行期异常（落盘 `native-error.json`＋stderr）；2＝目录参数缺失。

## 1. 验收口径变更（本合同核心）

- 等价判据从"与退役 Python 参考**逐字节等同**"改为"**值级功能等价**"：输出按 JSON 值比较相等，可观察行为（时序、判定、拒绝、证据内容、错误分类）一致。
- 不复刻 CPython 可观察细节：默认分隔符、键插入序渲染、repr/KeyError/JSONDecodeError 文本与行列号、真值/数值相等/isinstance 运行时语义仿真。
- 简洁性、优雅性、可维护性优先于与旧实现的文本一致。
- 事实依据（已核实）：Python 驱动侧只按结构化字段分类错误（`workers.py:125`、`native_hosts.py:114` 均判 `error_kind`），**无任何代码字符串匹配 CPython 报错文本**。

## 2. 删除清单（无功能消费者）

- `internal/harness/pydump.go` 整体退役：CPython 词法解析器（`pythonJSONParser`/`JSONSyntaxError` 及报错行列号）、`PyDumps`/`PyDumpsRaw`（默认分隔符渲染）、`writePyRepr`、`DecodeOrdered*`。
- 键序结构 `OrdObj`/`Obj`：若无残余消费者则一并删除，改用普通值＋canonical 解码；逐点核查 `native_dsh.go`、`native_transport.go`、`bridge.go`、`command.go`。
- `RunNative` stdout：默认分隔符渲染 → 规范紧凑 JSON（与证据文件同一编码器，canonical 语义）。

## 3. 保留并显式化（决策语义是行为，不是仿真）

| 原仿真 | 显式实现 |
|---|---|
| 空绑定 `{}` 为假 → 跳过校验 | `len(binding)==0` 判断 |
| 数值等价 `1==1.0`/`True==1`（big.Rat） | 仅在实际可达混合类型处显式归一比较；先核实是否真会收到混合类型，不可达则删 |
| `write_answer` 比较 universal newlines | 显式 CRLF→LF 归一后比较 |
| `isinstance(True,int)` 各点 | 按该处业务决策显式类型判断 |
| `KeyError` 等异常文本 | 结构化错误＋Go 风格文本（`error_kind`/`identity`/`method`/`native_code` 字段不变） |

边界：**宿主/ACP 协议自身产生的文本**（DSH 宿主错误、权限文本）是功能数据，照旧保真匹配——与 CPython 仿真无关，不得误删。

## 4. 有界差异登记（相对已收口形态的故意变化）

1. `harness native` stdout 改规范紧凑 JSON（值相等，消费者按值解析）。
2. `native-error.json` 的 `error` 文本与 stderr 改 Go 风格；结构化字段不变。
3. host-prompt 内嵌 schema JSON 若改规范形，登记（值相等）。
4. 内部解析失败不再呈现 CPython 行列号文本。
5. **后果**：既有授权原生运行的运行时身份绑定失效；关口处需一次新的授权真跑（与第 6 节能力覆盖一次补齐）。

## 5. 随行修复（本单元内）

- **ADM-E15（评审 P2）**：`scripts/go-engine/admission-controls.py:308` 篡改值硬编码 `"dsh"` 对本记录恒等、控制项失效（已复现 BAD）。修为通用规格：**篡改值必须与记录原值不同**（如自导矛盾值 "control-contradiction"，已验证可行）。修复后全套 admission-controls 复验至全 OK。
- **sessionEvents 逐行流式（评审 P3 顺手项）**：恢复逐行消费，不再整读整存。

## 6. 评审缺口挂载（本单元收口条件）

- **原生能力覆盖**（§14.2"真正尝试过并被约束"）：`read_only_tools`（只读角色实际工具入口尝试写入被拒）与 `subagents`（子代理继承）需定向真机证据；需机主授权真实模型调用后执行。
- **回退保全证据**（§12.5）：补 G2a 冻结版本的启动入口声明＋上一版本独立启动的回退冒烟记录，落进 G2b 机器记录。

## 7. 验收门（重签）

gofmt（触及文件）→ `go build ./...` → `go test ./...` → `GOOS=windows`/`GOOS=linux` `go vet ./...` → Python 全套（`TMPDIR=/private/tmp PYTHONPATH=$PWD/runtime/langgraph`）→ 差分测试值级通过 → `admission-controls.py --record .gates/results/go-engine-G2b.json` 全 OK → （授权后）原生覆盖补证＋回退冒烟 → `acceptance.py --unit G2b --previous-baseline G2a` → 记录三元组 → admission GRANTED。

## 8. 范围排除

`canonical.go` 语义不动；`admission.py`/`acceptance.py` 行为不动（仅控制项脚本规格修复）；`GOFMT-WINDOWS-FILE-PREEXISTING` 仍为独立登记项；hook 平台接入（`internal/host`＋`internal/lifecycle`）不在本单元。
