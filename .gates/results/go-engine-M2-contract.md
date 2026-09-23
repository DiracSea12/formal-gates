# M2 替换单元合同：反馈包装（G1b 第二项）

基线为 M1（候选 `f51d1ac`，记录提交 `0ebc9e0`，准入已放行 M2）。G1b 按 §13.1 分三项：比较材料（M1，已通过）、**反馈包装（本单元 M2）**、工件发布（M3）。

合同依据是方案 §9.4「正式结果与反馈」：反馈是**有来源的数据**，包含源工件、角色、revision、用途与摘要，**不具有授权能力**，且**不与角色、检查、候选或状态字段合并**。

## 职责边界

**做**：把 `feedback.py` 中两个包装构造器的**形状与判定规则**搬进 Go。

| 归属 | 内容 |
| --- | --- |
| **Go（新）** | `task_input` 的包装形状（含 `untrusted-feedback/v1` 包装、`sha256` 摘要、`feedback=None` 的处理、`control` 块）；`evidence_feedback` 的包装形状与 **revision 绑定判定** |
| **Python（不变）** | `store.verify_evidence(ref)` 的全部内容（工件寻址、`verify-files`/`verify-process`/`verify_source` 委派）、`store.manifest` 读取、异常类到拒绝家族的路由 |

与 M1 同一原则：**Go 持有规则与格式，Python 持有进程与文件系统**。不新增拒绝家族——绑定失败仍是 `HarnessError`。

**明确不在本单元范围**（写给评审，不是遗漏）：

1. `BOUNDARY` 文本**留在 Python**。它是**提示词文案**，而整段提示词（约 15 句）都是 Python 的职责；把其中一段搬去 Go 会形成任意切分。§9.4 对本单元真正构成合同要求的是「带来源、无授权地把不受信内容包起来」，这正是本单元搬动的部分。**若认为整段提示词应入 Go，那是另一个单元。** 列为评审问题。
2. `graph.py` 里「每轮只携带最近相关反馈」的选择逻辑（哪个 ref 进哪个角色）不变。

## 桥接操作

| 操作 | 载荷 | 返回 |
| --- | --- | --- |
| `task-input` | `{"role","revision","base","candidate","checks","feedback"}` | 任务对象本体（即 `task.json` 的内容） |
| `evidence-feedback` | `{"ref","purpose","revision","evidence"}` | 包装对象，或 `{"ok": false, "kind": "binding", "refusal": "FEEDBACK_BINDING: …"}` |

`evidence-feedback` 的拒绝走 `value` 里的类型化结果（同 C4 的 `verify-files`），由 Python `refuse()` 映射回 `HarnessError`。**不使用**桥接失败形状。

## 必须逐字保持的规则

### `task_input`

| 规则 | 内容 |
| --- | --- |
| 顶层字段 | `profile`=`"harness-v1"`、`role`、`revision`、`base`、`candidate`、`checks`、`feedback`、`control` |
| feedback 非 `None` | `{"kind":"untrusted-feedback/v1","authority":"none","sha256": digest(feedback),"content": feedback}` |
| feedback 为 `None` | `feedback` 为 `null`（不是省略、不是空对象） |
| `sha256` | Go 侧自行计算规范摘要（C3 的 `CanonicalDigest`），与迁移前 `digest(feedback)` 逐字相同 |
| `control` | `{"can_approve":false,"can_choose_next_stage":false,"can_edit_state":false,"result_schema_only":true}` |
| **不合并不变量** | `feedback` 的任何字段都不得进入 `role`/`revision`/`base`/`candidate`/`checks`/`control`；`goto`、`checks`（注入的）等不得出现在顶层 |
| 空值透传 | `role`/`base`/`candidate` 可为空串，原样透传；`checks` 原样透传（含空列表） |

### `evidence_feedback`

| 规则 | 内容 |
| --- | --- |
| 绑定判定 | `evidence.get("revision", revision) != revision` → 拒绝。**默认值是本 run 的 revision**，故证据未带 `revision` 时通过 |
| 拒绝措辞 | `FEEDBACK_BINDING: evidence is from a different requirement`，家族 `HarnessError` |
| `kind` / `authority` | `"evidence-data/v1"` / `"none"` |
| `purpose` | 原样透传 |
| `source` | `{"artifact": ref, "role": evidence.get("role","verification"), "revision": <本 run 的 revision>}` |
| `content` | `evidence.get("result", evidence)`——有 `result` 取它，否则取**整个证据对象** |
| 不合并不变量 | 证据里即使带 `control`/`goto`/`revision` 也不得泄漏到包装顶层 |

## 受影响调用方

- `feedback.evidence_feedback`（`graph.py` 共 9 处调用）、`feedback.task_input`（`workers.py`）
- `workers.py` 的 `task.json` 落盘与「已保存任务材料被改动」判定
- `test_session_feedback.py` 的两个既有用例

## 不变量

1. **`task.json` 逐字节不变**：同一输入下，迁移前后的 `task.json` 规范字节完全相同（含 `feedback=null` 的形态）。
2. **摘要一致**：`task["feedback"]["sha256"]` 与迁移前 `digest(feedback)` 逐字相同。
3. **拒绝家族与措辞不变**：绑定失败仍是 `HarnessError`，消息逐字不变。
4. **不合并不变量成立**：注入的 `control`/`goto`/`revision` 不出现在顶层，`checks` 保持调用方传入的原值。
5. **net 往返次数不增加**：`task-input` 一次调用内含原 `digest` 的一次跨进程调用（Python 侧不再单独调 `digest`）；`evidence-feedback` 为新增一次调用，与 M1 同量级（约 6 ms）。

## 必需用例

| 用例 | 判据 |
| --- | --- |
| 无反馈 | `feedback=null`；`task.json` 字节与参考实现一致 |
| 有反馈（普通） | 摘要一致、`authority="none"`、`content` 为原值 |
| 注入的反馈 | `control`/`goto`/`revision` 不出现在顶层；`task["revision"]` 仍为绑定的 revision；`task["checks"]` 仍为调用方传入值 |
| 证据带 `result` | `content` 取 `result` |
| 证据不带 `result` | `content` 取整个证据对象 |
| 证据带 `role` | `source["role"]` 取其值 |
| 证据不带 `role` | `source["role"]` 为 `"verification"` |
| 证据带别的 revision | `HarnessError`，措辞 `FEEDBACK_BINDING` 逐字一致 |
| 证据不带 revision | 通过（默认取本 run revision） |
| Unicode / 嵌套 | 摘要与内容逐字一致 |
| Go 合同 | `feedback_test.go` 覆盖上述形状与判定 |

样本量：差分对照覆盖上表全部形态 + Go 合同测试；语料含注入字段、Unicode、空值与拒绝项（§12.2）。

## 适用宿主

纯计算；不涉及宿主协议、权限或工具继承。不需要原生定向验收。

## 已测量的开销

`task-input` 把原「一次 `digest` 跨进程」并入自身，**不增加往返**；`evidence-feedback` 新增一次，与同类原语同量级（约 6 ms/次）。`evidence_feedback` 在 `graph.py` 中每次角色调用最多 2 次，非高频路径。

## 回退入口

```sh
git -C /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine \
  reset --hard f51d1ace6f1e67942a3f53c7ca3dd9f8419cffb8
```

## 验收出口

- 差分对照用例全部通过（`task.json` 字节与包装形状与参考实现逐字一致）。
- Go 侧 `feedback_test.go` 通过；`go vet ./internal/harness/` 与 `gofmt -l internal/harness/` 干净。
- 累计组合完整流程达 `DEMO_COMPLETE`；完整 Python 套件稳定通过（连续两遍）。
- 新增用例附**负控制**（定向变异后确认用例确实失败）。
- 基线、原始证据与回退入口固定。
