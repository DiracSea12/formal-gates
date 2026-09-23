# M3 替换单元合同：工件发布（G1b 第三项）

基线为 M2（候选 `dfa23e7`，记录提交 `72b5aba`，准入已放行 M3）。G1b 按 §13.1 分三项：比较材料（M1，已通过）、反馈包装（M2，已通过）、**工件发布（本单元 M3）**。本单元通过后 G1b 收口。

合同依据：

- §13.1 G1b 行：职责为"工件发布"，**保持内容绑定及重复调用语义**；验收出口为"中途失败和重入不覆盖已绑定内容"。
- §9.3："材料只作角色输入，不混入交付候选；**重试不得覆盖已绑定的不同内容**。"
- §7："工件通过临时文件、完整写入、必要同步和**原子发布**生成，再由事务引用。文件已发布但事务未提交，允许留下未引用工件；**有效结果不得指向未完成文件**。"

## 职责边界

**做**：把 `infra.py::Store` 里**内容寻址规则**搬进 Go——也就是 `put` 的引用推导与 `get` 的两条判定。

| 归属 | 内容 |
| --- | --- |
| **Go（新）** | `put` 的引用推导 `ref = sha256(CanonicalJSON(value))`（连同 canonical 字节一并返回）；`get` 的**引用格式规则**（`invalid artifact reference`）；`get` 的**完整性规则**（`artifact integrity mismatch`） |
| **Python（不变）** | 工件位置 `artifacts/<ref>.json`（存储布局）；`path.exists()` 的**发布一次**判定；`atomic_bytes` 的临时文件 + fsync + `os.replace` 原子发布；`read_bytes()` 的读取与全部文件系统异常；`json.loads(body)` 解码 |

与 M1/M2 同一原则：**Go 持有规则与格式，Python 持有进程与文件系统**。不新增拒绝家族——两条拒绝仍是 `HarnessError`。

搬到 Go 后，Python 侧 `Store` 不再持有任何 `hashlib.sha256`；`internal/harness` 成为内容寻址的**唯一**实现（`infra.py` 的 `hashlib` 导入仍被 `runtime_identity` 使用，不在本单元范围）。

**明确不在本单元范围**（写给评审，不是遗漏）：

1. `Store.event`（追加 `events.jsonl` + stderr 镜像 + `at` 时间戳）**留在 Python**。它是流程事件台账，按 §7 是（未来 Go 内核）事件表的导出；G1～G3 期间 LangGraph 仍是流程权威。`event()` 内那次 `canonical` 调用本就不变。
2. `atomic_bytes` / `atomic` 不动：原子发布是文件系统职责。
3. 工件**位置** `artifacts/<ref>.json` **留在 Python**。§7 只说"正文、长日志和历史对话在工件目录中"，未规定命名；该位置是 Python 侧的存储布局而非寻址规则。因此 `artifact-plan` **不返回路径**（返回路径会让 `get` 在读文件前多一次必要调用）。列为评审问题。
4. `read()`（拒绝重复字段的 JSON 读取器）不动，且 `get` **不使用**它。两者语义不同：`get` 的参考实现用 `json.loads`。
5. `runtime_identity`（`infra.py`）、`retention.py`、`cli.py`、`workers.py` 里其余的 `sha256` 用法不属于 M3。

## 桥接操作

| 操作 | 载荷 | 返回 |
| --- | --- | --- |
| `artifact-plan` | `{"value": <json>}` | `{"canonical": "<text>", "ref": "<64 hex>"}` |
| `artifact-reference` | `{"ref": "<str>"}` | `{"ok": true}` 或 `{"ok": false, "kind": "binding", "refusal": "invalid artifact reference"}` |
| `artifact-binding` | `{"ref": "<str>", "body_b64": "<base64>"}` | `{"ok": true}` 或 `{"ok": false, "kind": "binding", "refusal": "artifact integrity mismatch"}` |

拒绝走 `value` 里的类型化结果（C4/M2 同款），由 Python `refuse()` 映射回异常类。`kind: "binding"` 沿用 `refuse()` 的**家族选择器**语义（不是语义标签）：它把该拒绝映射为 `HarnessError`。**两条拒绝的异常类必须恰好是 `HarnessError`，而不是 `RuntimeBlocked`**——后者是前者的子类，`assertRaises(HarnessError)` 无法区分，因此必须有断言类身份相等的用例。

`body_b64` 为标准带填充 base64（ASCII）。base64 解码失败属于调用方错误，走桥接失败形状（`{ok:false, code}` + 退出码 2），不是业务拒绝；Python 自身不会构造该情形。

> **合同更正（2026-09-17，外部验收发现 P1 之后）**：本表 `artifact-binding` 的载荷已由 `{"ref", "body_b64"}` 改为 **`{"ref", "path"}`**。
>
> **为什么必须改**：`body_b64` 让读请求的大小**随工件体量增长**（约 4/3 × 工件体量），而桥接的输入天花板是**所有操作共用的固定 8 MiB**。于是发布侧接受、读取侧拒绝：约 6 MiB 以上的工件写进去却读不回来，一份已发布、已被引用的工件无法读回，已完成的工作无法重入。这是**合同自身的缺陷**，不是实现走样——原合同把一个**随输入增长**的字段放进了一个**固定上限**的信封，却没有规定两端必须共享同一容量。
>
> **改后的语义**：读取仍由 Python 做（`body = path.read_bytes()`，保留 `FileNotFoundError` / `IsADirectoryError` 与 `json.loads` 的帮助），Go 只按**同一路径**读取并比对字节（`VerifyArtifactBindingFile`）。Go 因此**不**负责返回读取结果，也就**不**需要复现 Python 的异常类型与消息——这正是本文第 48 行否决"让 Go 读文件"时未考虑到的第三条路。`artifacts/<ref>.json` 的命名**仍属 Python**（Go 只是被**告知**路径，不推导它）。`artifact-reference`（格式判定）与拒绝映射全部不变，两次调用的结构与顺序也不变。
>
> **未放宽天花板**：8 MiB 共用上限保留原样，并有专门用例（`TestBridgeStillRejectsInputOverTheSharedCeiling`）钉住。修的是"读请求不该随工件增长"，不是"把上限拿掉"。
>
> 返修候选**尚未验收**；本合同的这一处更正随返修候选一并待审。详见 `.harness/validation/m3-repair-capacity-20260917T2330Z/CLASSIFICATION.md` 与 `.gates/results/go-engine-M3.md`。

### 为什么 `get` 用两次调用

迁移前的 `get` 有两条**各自独立**的拒绝，顺序是：格式 → 读取 → 完整。两次调用把这个顺序变成**结构性**的而不是论证性的：`artifact-reference` 在任何文件系统访问之前给出判定，`artifact-binding` 只做比较。代价是每次 `get` 两次桥接调用（实测 5.8 ms/次，每次都会起一次进程）；`put` 仍是一次——替换掉它原本就有的那次 `canonical` 调用，净增 0。

**候选方案与被否原因**（写给评审）：把两条规则合成一次调用、把读取失败"捕获后重抛"，可以用一次调用换到同样可证的行为，但那样"格式优先于读取失败"就只靠参数顺序保证；反过来让 Go 自己读文件（C2 的 `file-proof` 先例），就得让 Go 复现 `FileNotFoundError`/`IsADirectoryError` 的异常类型与消息（合成异常或跨平台 errno 映射），失败面更大。两次调用是三者中唯一不需要为异常语义写例外的方案。

## 必须逐字保持的规则

### `put`

| 规则 | 内容 |
| --- | --- |
| 引用 | `ref = sha256(CanonicalJSON(value))` 的小写十六进制，与迁移前 `hashlib.sha256(canonical(value)).hexdigest()` 逐字相同。**复用** C3 的 `CanonicalJSON`，不新写第二份编码器 |
| 落盘字节 | 写入的正是 Go 返回的 canonical 字节本身；**不**再调 `canonical()`/`atomic()` 让同一个值第二次跨进程编码 |
| 发布一次 | `if not path.exists()`：已存在则**不写**。即使已存在的文件是损坏内容也**不修复**——内容寻址让文件名即字节的承诺，重入只保证"不覆盖已绑定内容"，不保证"修复" |
| 非有限数值 | 迁移前 `canonical({'x': float('nan')})` 即抛 `HarnessError: invalid character 'N' looking for beginning of value`（传输层发出 `NaN` 字面量，Go 侧解码拒绝）。`artifact-plan` 沿用同一载荷形状与解码路径（`decodeValue`/UseNumber），该行为逐字不变 |
| 不可编码值 | 非 JSON 类型（如 `set`）在 `_transport_bytes` 阶段即抛 `TypeError`，与迁移前同一处、同一类型 |

### `get`

参考实现的三步与 M3 后的归属：

| 参考实现 | M3 后 |
| --- | --- |
| `if len(ref) != 64 or any(c not in "0123456789abcdef" for c in ref): raise HarnessError("invalid artifact reference")` | Go `artifact-reference` 判定；Python `refuse()` 抛 `HarnessError`，消息逐字不变 |
| `body = (self.directory / "artifacts" / f"{ref}.json").read_bytes()` | Python 不变（异常类型与消息逐字不变） |
| `if hashlib.sha256(body).hexdigest() != ref: raise HarnessError("artifact integrity mismatch")` | Go `artifact-binding`，对**原始字节**比较 |
| `return json.loads(body)` | Python 不变；bytes 入参，**不**拒绝重复字段 |

**非 ASCII 引用**：参考实现按**码点**计长（`len('é'*32) == 32`），Go 的 `len(ref)` 是**字节**（64）。两者在本规则下等价——能通过的字符串必然是 64 个 ASCII 十六进制字符；所以"Go 通过 ⇔ Python 通过"。**必须**有用例覆盖多字节引用（如 `'a'*62 + 'é'`、`'é'*32`），并在实现注释里留下该等价性论证，避免它成为下一个类型边界缺陷。

### 字节为何以 base64 传递

完整性规则是**对字节**的比较，而被篡改的工件不保证是合法 UTF-8。若以文本传递，一个非 UTF-8 的篡改件会在解码处抛 `UnicodeDecodeError`，而参考实现抛的是 `HarnessError("artifact integrity mismatch")`。base64 使该情形仍报完整性拒绝。

## 不变量

1. **引用与字节一致**：同一输入下 `put` 返回的 ref 与迁移前逐字相同，落盘工件逐字节相同。
2. **发布一次**：同一 ref 重复 `put` 不重写文件（inode 与 mtime 不变）；已存在的损坏文件不被修复。
3. **拒绝顺序恒为** 格式 → 读取 → 完整；两条业务拒绝的异常类恰为 `HarnessError`，消息逐字不变。
4. **读取失败仍是原生 OS 异常**（`FileNotFoundError`/`IsADirectoryError`/`PermissionError`…），不得被包装成 `HarnessError`。
5. **`get` 用 `json.loads`**（bytes、允许重复字段，最后者胜），不是 `read()`。
6. **非 UTF-8 篡改报完整性拒绝**，绝不变成 `UnicodeDecodeError`。
7. **往返次数**：`put` 1 次（原 `canonical` 1 次，净增 0）；`get` 2 次（原 0 次）。其他操作次数不变。
8. **中途失败不留下可见的未完成工件**：`atomic_bytes` 的临时文件与 `os.replace` 语义不变，最终路径要么是旧内容要么是新内容。

## 必需用例

| 用例 | 判据 |
| --- | --- |
| 引用与字节一致（Unicode/空集合/嵌套） | 与 `reference_artifacts` 的 ref 逐字相同，工件字节逐字节相同 |
| 数值保真 | `7` 与 `7.0` 得到**不同** ref；各自与参考实现一致 |
| 非有限数值 | `NaN`/`Infinity` 与参考实现抛同一异常、同一消息 |
| 重复 `put` | 同一 ref，工件 inode/mtime 不变 |
| 已绑定内容损坏后重入 | `put` **不修复**：文件字节不变；随后 `get` 报完整性拒绝 |
| 两个不同值 | 两个不同工件，内容各自正确 |
| 发布中途失败（打桩 `os.replace` 抛错） | `put` 抛错，最终路径不存在，无 `.tmp` 残留 |
| `get` 正常路径 | 返回解码值 |
| 畸形引用矩阵（空/63/65/大写/非十六进制/空格/多字节） | 与参考实现同为 `HarnessError`，**类身份相同**，消息逐字相同 |
| 合法引用 + 文件缺失 | 与参考实现同消息的 `FileNotFoundError`（不是 `HarnessError`） |
| 合法引用 + 路径是目录 | 与参考实现同消息的 `IsADirectoryError` |
| 内容被改写为其他 JSON | 双方完整性拒绝，消息逐字相同 |
| 内容被改写为非 UTF-8 字节 | 双方完整性拒绝（**不得** `UnicodeDecodeError`） |
| 工件体含重复字段（`ref = sha256(b'{"a":1,"a":2}')`） | 双方 `json.loads` 成功、结果 `{'a': 2}`（**不**报错） |
| 空文件 + 其 sha256 作 ref | 双方通过完整性后同报 `JSONDecodeError` |

用例须与迁移前实现做**差分**（新增 `tests/reference_artifacts.py`，保持迁移前 `put`/`get` 逐字；生产代码不得导入它），并各自使用独立临时目录。

## 受影响调用方

- `Store.put` / `Store.get`：`graph.py` 约 20 处（含 `graph.py:213-214`、`236-237` 的列表推导内多次调用）、`workers.py` 2 处、`cli.py`、`handover.py`
- `Store.verify_evidence`（内部 `self.get(ref)`）→ `feedback.evidence_feedback`（M2 适配器）
- `tests/test_driver.py::test_artifact_digest_and_duplicate_json`：已用真实 bridge，无需改
- **`tests/test_boundary_recovery.py::test_artifact_integrity` 的前提被本单元终结**：它用 `Bridge('unused')` 证明"工件读写不需要桥接"。M3 之后 `put`/`get` 都走桥接，该用例必须改用真实 bridge（`driver_bridge()`），**不是删除**。这是有意的前提变更，须在记录与索引中显式登记。

## 交付物

- `internal/harness/artifact.go`：`ArtifactPlan`（值 → canonical 字节 + ref）、`VerifyArtifactReference`（格式规则）、`VerifyArtifactBinding`（完整性规则），全部复用 `CanonicalJSON`
- `internal/harness/artifact_test.go`
- `internal/harness/bridge.go`：新增三个 case，沿用 `StrictDecode` + `decodeValue`
- `runtime/langgraph/formal_gates_harness/infra.py`：`Store.put` / `Store.get` 改为适配器
- `runtime/langgraph/tests/reference_artifacts.py`、`runtime/langgraph/tests/test_artifact_publication_differential.py`
- `tests/test_boundary_recovery.py`：前提变更（真实 bridge）

手工跑 Python 套件时须用 `TMPDIR=/private/tmp`（既有已知差异 `TMPDIR-UNRESOLVED-SYMLINK`）。
