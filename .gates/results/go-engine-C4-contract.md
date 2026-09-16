# C4 替换单元合同：证据校验迁移（G1a 第四项，也是最后一项）

基线为 C3（候选 `6ae01c2`，记录提交 `6ae01c2`，准入已放行）。本单元把 Python `Store.verify_process` 与 `Store.verify_evidence` 里的证据校验搬进 Go，是 G1a 阶段的收尾项。

## 职责边界

**做**：在 Go 侧实现回执/请求绑定校验、字段比较、证明集合的逐文件校验，以及"哪条证据先不一致就报哪条"的有序判定；并通过桥接操作把这两段校验暴露给 Python。

**不做**：不读 `exit.json`/`request.json`、不做"这个 job 是否属于本 run"的路径判断——这些仍由 Python 负责；不引入第三方依赖；不改变规范序列化、摘要或帧格式；不为 `binding`/`missing`/`changed` 之外的拒绝家族新增措辞。

## 输入与输出

| 项 | 内容 |
| --- | --- |
| 输入（verify-process） | `directory`：job 目录；`process`/`receipt`/`request`：已解析 JSON 对象；`proofs`：有序证明列表，每项 `{name, expected}` |
| 输入（verify-files） | `files`：有序证明列表 `{name, expected}`；`label`：拒绝前缀 |
| 输出 | 成功返回 `{"ok": true}`；业务拒绝返回 `{"ok": false, "kind": <binding\|missing\|changed>, "refusal": <文本>}` |

桥接操作：

| 操作 | 载荷 | 返回 |
| --- | --- | --- |
| `verify-process` | `{"directory","process","receipt","request","proofs"}` | `{"ok": true}` 或 `{"ok": false, "kind", "refusal"}` |
| `verify-files` | `{"files","label"}` | `{"ok": true}` 或 `{"ok": false, "kind", "refusal"}` |

Python 侧 `refuse()` 把带 `kind` 的拒绝翻译回异常类：`binding` → `HarnessError`，`missing`/`changed` → `RuntimeBlocked`。这与迁移前参考实现 Raise 的异常类逐字一致。

## 设计要点（已确立，不得推翻）

- Python 负责读 `exit.json`/`request.json` 与"job 是否属于本 run"的路径判断；Go 负责回执/请求绑定、字段比较、证明集合与逐文件证明校验。
- 证明以**有序列表**传入而不是 map：多条不一致时，参照实现按文档顺序报第一条，Go 的 map 无序会让拒绝消息随机化。
- 无法读取的证据文件要原样传播 `EVIDENCE_MISSING: <path>`（不是 `EVIDENCE_CHANGED`）。
- 数字按值比较（`1` 与 `1.0` 相等），与 Python 的 `!=` 语义一致。
- **拒绝措辞必须逐字不变**：

| 家族 | 措辞 |
| --- | --- |
| binding | `EVIDENCE_BINDING: job is outside this run` |
| binding | `EVIDENCE_BINDING: request digest differs` |
| binding | `EVIDENCE_BINDING: process {field} differs` |
| missing | `EVIDENCE_MISSING: stdout/stderr proofs absent` |
| missing | `EVIDENCE_MISSING: {path}` |
| changed | `EVIDENCE_CHANGED: {name}` |
| changed | `EVIDENCE_CHANGED: comparison input {name}` |

## 受影响调用方

- `infra.Store.verify_process`：现读取两文档后把校验委派给 `verify-process`。
- `infra.Store.verify_evidence`：对 `input_files` 调用 `verify-files`，对 `process`/`source.process` 复用 `verify_process`；`refuse()` 统一翻译拒绝。
- `infra.Store.job`：在作业重连路径上调用 `verify_process`（此路径现在需要可用的驱动二进制）。
- 差分对照测试 `test_evidence_verification_differential.py` 与 Go 合同测试 `verify_test.go`。

## 不变量

1. **与参考实现逐字一致**：同一 job 目录下，Go 路径与 `reference_evidence.py` 的参考实现在拒绝的异常类与消息上完全相同（含多条不一致时按文档顺序报第一条）。
2. 数字按值比较：整型与浮点型表示同一数值时判为相等。
3. 不可读证据传播 `EVIDENCE_MISSING` 而非 `EVIDENCE_CHANGED`。
4. 桥接拒绝形状不变：业务拒绝以 `ok:false` + `kind` 承载，不触发 `Bridge` 的失败路径（退出码与失败消息语义保持"桥本身失败"）。
5. 规范序列化/摘要（`canonical`/`digest`）的既有行为不变；`canonical.go` 仅顺带修正：`strconv.ParseFloat` 的 `ErrRange`（如 `1e400` 解析为 `inf`/`Infinity`）不再当作拒绝。

## 必需用例

| 用例 | 判据 |
| --- | --- |
| 完整回执通过（Go 与参考实现均接受） | 异常类/消息一致 |
| 数值相等（exit_code 0 与 0.0） | 两者均接受 |
| request digest 不匹配 | `HarnessError: EVIDENCE_BINDING: request digest differs` |
| 字段不一致（exit_code/error/streams/request_digest） | `HarnessError: EVIDENCE_BINDING: process {field} differs` |
| 进程记录缺 request_digest | `HarnessError: EVIDENCE_BINDING: process request_digest differs` |
| job 不在本 run | `HarnessError: EVIDENCE_BINDING: job is outside this run` |
| 证明集合缺一条 | `RuntimeBlocked: EVIDENCE_MISSING: stdout/stderr proofs absent` |
| 流内容被篡改 | `RuntimeBlocked: EVIDENCE_CHANGED: {name}` |
| 流被删除（不可读） | `RuntimeBlocked: EVIDENCE_MISSING: <path>`，而非 CHANGED |
| 多条不一致按文档顺序报第一条 | 两者报同一条（stderr.log 先于 stdout.log） |
| 比较输入文件：通过/篡改/缺失 | 拒绝类与消息一致（含 `comparison input ` 前缀） |
| 拒绝家族映射到异常类 | binding→HarnessError，missing/changed→RuntimeBlocked |
| Go 合同：整型/数值相等/各拒绝家族/逐文件校验 | `verify_test.go` 全过 |

样本量：差分对照 12 个用例 + Go 合同 6 个测试函数；覆盖上述全部判据。

## 适用宿主

需要可用的驱动二进制（`.harness/bin/formal-gates`）来完成跨进程校验；其余为纯计算，不涉及宿主协议、权限或工具继承，不需要原生定向验收以外的额外宿主适配。

## 已测量的边界

与 C3 的 `Infinity` 同类，如实记录一条**不可达**的差异，不为此增加机制：

参考实现读输入文件时用 `file_proof(Path(name))`，因此缺失文件的消息里路径是 `Path` 规范化后的形式；Go 侧按调用方传入的原样字符串命名。当存储的 `input_files` 键**非规范化**（如含 `./` 或重复斜杠）且文件缺失时，两者措辞不同（实测：参考 `…/missing.txt`，Go `…/./missing.txt`）。

该差异不可达：键由 `workers.py` 以 `str(Path(comparison['directory']) / name)` 写出，构造 `Path` 时即已规范化；且 `verify_evidence` 只接收 `Store.get(ref)` 的返回值，工件内容寻址——手工改写键会先触发完整性校验失败，根本到不了 `verify-files`。篡改分支（`EVIDENCE_CHANGED`）不涉及路径规范化，已由差分用例覆盖。

## 已测量的开销

校验为跨进程桥接调用（每次 `job` 重连、每次 `verify_evidence`）。与 C3 同类原语同量级（约 6 ms/次）。本单元复用既有 `file-proof`/`canonical` 原语，不新增高频路径。

## 回退入口

```sh
git -C /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine \
  reset --hard 7492c01fa6b9d680ca540f8f04437a0884a0a12c
```

## 验收出口

- 12 个差分对照用例全部通过（Go 路径与参考实现逐字一致，连续两遍稳定）。
- Go 侧合同测试 `verify_test.go` 通过；`go vet ./internal/harness/` 与 `gofmt -l internal/harness/` 干净。
- 完整 Python 套件稳定通过（连续两遍，0 失败）。
- 基线、原始证据与回退入口固定。
