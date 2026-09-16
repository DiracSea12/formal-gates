# C1 替换单元合同：结果解码（G1a 第一项）

在实施之前固定本单元的边界。本单元是分块替换的第一个替换单元，基线为 B0（候选 `41d3375`，记录提交 `284ceff`，准入已放行）。

## 职责边界

**做**：把模型最终消息的原始文本判定为一个 JSON 对象，或给出一个带原因的拒绝。

**不做**：

- 不在多份结构化内容之间选择其一——存在竞争内容一律拒绝。
- 不判断角色结果的业务语义（`PASS`/`FAIL`/级别/检查归属）。那是 `internal/harness.ValidateWorker` 已经拥有的合同，本单元不重复。
- 不写文件、不读时钟或随机数、不访问网络、不接触流程状态。
- 不改动 `internal/harness.StrictDecode`：它是"固定类型的严格解码"（含未知字段拒绝），本单元是"通用对象的唯一性框架"。两者并列，不合并。

## 输入与输出

| 项 | 内容 |
| --- | --- |
| 输入 | `text`：模型最终消息的原始文本（DSH 的 `answer`、ZCode 的 `end.payload.response`） |
| 输出（接受） | 一个 JSON 对象 |
| 输出（拒绝） | 一条带原因的拒绝，原因字符串与迁移前逐字相同 |

桥接操作：`frame-result`，载荷 `{"text": "..."}`，返回 `{"accepted": true, "result": {...}}` 或 `{"accepted": false, "refusal": "NATIVE_RESULT: ..."}`。

**为什么拒绝走 `ok: true` 的载荷字段而不是桥接失败形状**：桥接的失败形状（`{ok:false, code:"INVALID_HARNESS_OPERATION"}`，退出码 2）是既有合同，本单元不改动它。而且"消息框架不合格"是**预期内的类型化业务结果**，不是传输/操作错误；把它混进失败形状会让调用方无法区分"Go 拒绝了这条消息"和"桥接本身坏了"。

## 受影响调用方

| 调用方 | 位置 |
| --- | --- |
| DSH 原生适配器 | `runtime/langgraph/formal_gates_harness/native_dsh.py`（收集完成后的结果落盘） |
| ZCode 原生适配器 | `runtime/langgraph/formal_gates_harness/native_zcode.py`（同一位置） |
| 既有测试 | `runtime/langgraph/tests/test_output_recovery.py`、`test_nonhappy_regressions.py` |

两处调用方在拒绝时的对外行为不变：抛出 `RuntimeBlocked`，消息逐字相同。`native_result.py` 保留 `decode_result(text)` 这一函数签名，变薄为"输入输出转换 + 调用 + 把 Go 的类型化拒绝翻译回 `RuntimeBlocked`"。

## 不变量

1. **拒绝原因逐字不变**。它们是可观测行为，被既有测试断言，也会出现在用户可见报告里。清单：
   - `NATIVE_RESULT: duplicate JSON field {key}`
   - `NATIVE_RESULT: non-finite number {literal}`
   - `NATIVE_RESULT: no unambiguous result object`
   - `NATIVE_RESULT: invalid result object`
   - `NATIVE_RESULT: final message is not one unambiguous JSON object`
   - `NATIVE_RESULT: invalid JSON result fence`
   - `NATIVE_RESULT: competing structured content outside result`
   - `NATIVE_RESULT: final result must be an object`
2. 角色结果字段、检查 argv、审批对象、业务代次与原生身份语义不变。
3. 本单元不改变桥接失败形状与退出码。

## 必需用例

| 用例 | 判据 |
| --- | --- |
| 直接 JSON 对象 | 接受，值等价 |
| JSON 代码围栏（含 `json` 标记、大小写、CRLF） | 接受，值等价 |
| 散文 + 一个裸对象 | 接受，值等价 |
| 重复字段（顶层与嵌套） | 拒绝，原因指名该字段 |
| 非有限数字（`NaN`/`Infinity`/`-Infinity`） | 拒绝，原因指名该字面量 |
| 多份结构化内容（两个围栏、对象外的括号） | 拒绝 |
| 顶层非对象（数组、字符串、数字、`null`） | 拒绝，原因指明必须是对象 |
| 空集合（`{}`、`[]`）与 Unicode（含非 BMP、组合字符、转义） | 接受/拒绝判定与参考实现一致 |
| 空输入、纯散文、未闭合围栏 | 拒绝 |

## 适用宿主

本单元是纯计算，**不涉及宿主协议、进程、连接、权限或工具继承**，因此不需要原生定向验收。原生验收的授权关口仍在 G2b 之前（`NATIVE-REVERIFY-AT-BASELINE`）。

## 差分对照

按方案 12.2：纯计算可以同时对照两个实现。迁移前的 Python 逻辑原样保留在**仅测试使用**的 `runtime/langgraph/tests/reference_result.py`（生产代码绝不导入），用固定样本对两个实现比较：接受/拒绝判定、拒绝原因、以及接受值的规范序列化。任何有界差异必须显式记录，不得静默忽略。

## 回退入口

```sh
git -C /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine \
  reset --hard 41d3375cf16f1bd411f102db2d9edfaeb28c6a29
```

## 验收出口

- 输入输出及拒绝行为对照通过（Go 单元测试 + 差分对照）。
- 累计组合完整流程与自动化回归通过（`acceptance.py` 九项必需项）。
- 基线、原始证据与回退入口固定。
