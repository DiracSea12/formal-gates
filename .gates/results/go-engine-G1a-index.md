# G1 状态索引（供外部评审）

> 文件名沿用 `go-engine-G1a-index.md`（C4 记录的 `bookkeeping` 引用它），内容自 M1 起覆盖 G1b。

G1a 是分块替换的第一阶段：**只读计算**。方案要求解码与证据校验**分别验收**。

> **G1a 已完成。** 四个替换单元（C1 结果解码、C2 证据内容摘要原语、C3 规范化与内容摘要、C4 证据校验）各自通过并被固定为下一基线。
>
> **G1b 已完成。** 方案要求比较材料、反馈包装、工件发布**分别验收**，三项各自通过：**M1 比较材料**（候选 `f51d1ac`）、**M2 反馈包装**（候选 `dfa23e7`）、**M3 工件发布**（候选 `d71113e`）。下一单元为 **G2a 命令作业**。

## 基线与验收链

每个替换单元在上一已验收基线上实施，累计组合通过后固定为下一基线。源码提交与验收结论分开提交。

| 单元 | 内容 | 候选提交 | 记录提交 | 结论 |
| --- | --- | --- | --- | --- |
| B0 | 基线冻结与标定（含验收工具、SQLite 锁定） | `41d3375` | `284ceff` | `PASS` |
| C1 | 结果解码 | `20b0422` | `a756cde` | `PASS`（后被独立验收发现回归，见 C2 重签） |
| C2 | 证据内容摘要原语 + C1 回归修复 | `87895e4` | 见 `go-engine-C2.json` | `PASS` |
| C3 | 规范化序列化与内容摘要 | `7492c01` | `6ae01c2` | `PASS`（重签；见下） |
| C4 | 证据校验（G1a 收尾） | `9687945` | 见 `go-engine-C4.json` | `PASS`（重签；见下） |
| M1 | 比较材料（G1b 第一项） | `f51d1ac` | 见 `go-engine-M1.json` | `PASS` |
| M2 | 反馈包装（G1b 第二项） | `dfa23e7` | `72b5aba` | `PASS` |
| M3 | 工件发布（G1b 第三项，G1b 收口） | `d71113e` | 见 `go-engine-M3.json` | `PASS` |

**M2 行是本次补的**：M2 的记录提交改了本索引的两张表，却漏掉了上表的 M2 行。记录为 `INDEX-MISSING-M2-ROW`。

C3 记录取代了绑定 `04f343d` 的上版记录：该候选存在一处由独立验收发现的 P1 合同回归（大整数在桥接路径上被拒），修复后候选前移到 `7492c01`，原结论不再适用。**C4 记录取代了绑定 `0f1cd2b` 的上版记录**：该候选存在一处由独立验收发现的 P1 数值语义缺陷（证据大小按两种不一致的整数转换规则比较，既误拒绝又误接受），修复后候选前移到 `9687945`。C2 记录同样取代了绑定 `2ed75d4` 的上版记录。分支 `codex/go-engine-v1`，基线起点 `c6d49323`。

## 三次由独立验收发现、我未自查出的回归

| 单元 | 缺陷 | 我的测试为何没发现 |
| --- | --- | --- |
| C1 | 说明中提及 NaN/Infinity 会让合法结果被拒 | 语料偏向"结构变体"而非"语义相邻文本" |
| C3 | 超出 float64 的整数在桥接路径被拒 | 单测直接调编码器，绕过了前置扫描器 |
| C4 | 证据大小按两种整数转换规则比较（`7.0` 误拒、`7.75` 截断误接受） | 两套测试都只写整数大小；`0` vs `0.0` 的用例**两边都设成了 `0.0`** |

三次都在**公开调用链上**成立、而在**直接函数测试**下看不出来。C4 起已把"经桥接的端到端等价用例"作为纪律，并对新增用例补了**负控制**（撤回实现后确认用例确实失败）。

**M1、M2、M3 是前三条由子代理实施、我独立复核的单元。** M1 复核中未查出该级别回归；**M2 查出 3 处**（`role` 非字符串被改写、revision 绑定把数字字符串化、残留的内联 `__import__`），其中前两处与 C4 的 `7.0`／布尔大小**属同一模式**：类型边界上的静默收窄。这说明该模式被反复触发，而"经桥接的端到端等价用例 + 负控制"是**由我**在外部评审之前发现的——但不代表外部评审会找不到别的问题，本表的其余三条仍全部来自外部。M2 的这两处已各配回归，并以负控制证明回退后用例会失败。

**M3 查出 1 处（D1）**：交付的差分测试让两个实现共用同一个工件目录，使被测的写路径从未按值被差分、两棵工件树也从未被互相比较。它没有造成可观察行为偏差，但让"重入不覆盖已绑定内容"的唯一直接证据退化为顺带成立，故按缺陷处理。D1 同样是**我在外部评审之前**查出的；修复的第二次派发没有产生任何改动（子代理返回空），我因此自行实施修复，并已在 M3 记录中如实登记。

## 独立验收三项，逐项处置结果

| 项 | 结论 | 处置 |
| --- | --- | --- |
| P1 说明中提及 NaN/Infinity 会拒绝合法结果 | **真实回归，已复现** | 非有限数字判定从"整条消息预扫描"改为"在解析器所在位置检查正在解析的片段"；补齐"说明中提及→接受"与"对象内实际包含→拒绝"两个方向共 7 个用例 |
| P2 重复空字符串键未被拒绝 | **真实漏检，已复现** | "是否发现重复"与"重复键的值"分离；同处第二个隐患（非字符串键伪装成空键重复）一并修掉 |
| G1a 范围的理由不足 | **成立** | 撤回原理由，并给出实测依据（见下） |

## 范围更正：原"阻塞"不成立

原索引称"把 JSON 规范化搬到 Go 会改变 Python 侧每一个摘要，必须另作版本化迁移"。**该理由过强。**

真正的问题只是"Go 的默认编码器与 Python 不同"。我据此实现了复现 Python `json.dumps(ensure_ascii=False, sort_keys=True, separators=(",",":"))` 的 Go 编码器并逐字节对照：

| 样本 | 结果 |
| --- | --- |
| 448 个：200 个跨约 40 个数量级的随机浮点、大整数、次正规数 `5e-324`、`1e15/1e16/1e21` 阈值、Unicode 非 BMP／组合字符／RTL、控制字符、`<`/`&`、U+2028/U+2029、空键、150 个随机嵌套结构 | **逐字节不一致 0 个** |

关键难点是浮点格式化（Python `repr(float)` 的定点/科学计数法阈值与指数位数规则）与字符串转义规则，均可复现。

**因此证据校验可以在 G1a 内继续补齐。**

### C3 的逐字节等价证据

迁移前实现保存在仅测试使用的 `reference_canonical.py`，450 个样本（其中 300 个是固定种子、跨约 40 个数量级的随机浮点，另有次正规数、`1e15/1e16/1e21` 与 `1e-4/1e-5` 阈值、Unicode、控制字符、`<>&`、U+2028/9、空键、100 个随机嵌套结构）**字节与摘要 0 差异**。另有一层独立证据：122 项 Python 测试全过，工件引用、证据摘要、作业请求摘要与增量绑定路径全部仍然吻合。

关键边界：桥接信封原先用 `canonical` 序列化自己，会无限递归；现改用私有**传输编码**（紧凑、保留非 ASCII、**不排序**），并有测试断言它不等于规范字节——防止信封悄悄变成第二实现。

## G1b 之后的剩余工作

| 单元 | 内容 | 前置 |
| --- | --- | --- |
| G2a | 命令作业：Go 监督器的直接 argv 路径，再验证它承载原生传输 | 待准入核验 M3（本记录的复核命令第一条） |
| G2b | 原生定向验收关口 | G2a 之后；需显式授权费用 |

仍是宿主编配器生命周期策略、随 G2b/G2c 迁移的：`verify_source` / `validate_binding`。

## 本阶段搬动的七项

| 单元 | 新归属（Go） | 桥接操作 | Python 侧 |
| --- | --- | --- | --- |
| C1 | `internal/harness.FrameResult` | `frame-result` | `native_result.decode_result` 变为适配器 |
| C2 | `internal/harness.FileProof` | `file-proof` | `infra.file_proof` 变为适配器 |
| C3 | `internal/harness.CanonicalJSON` / `CanonicalDigest` | `canonical`、`digest` | `infra.canonical` / `infra.digest` 变为转发 |
| C4 | `internal/harness.VerifyProcess` / `VerifyFiles` | `verify-process`、`verify-files` | `Store.verify_process` / `verify_evidence` 保留路径判断与文件读取，校验委派 |
| M1 | `internal/harness.planComparison` | `comparison-plan` | `feedback.materialize_comparison` 保留 git 调用、文件读写、篡改检查与证明收集 |
| M2 | `internal/harness.planTaskInput` / `planEvidenceFeedback` | `task-input`、`evidence-feedback` | `feedback.task_input` / `evidence_feedback` 保留 Store 访问、工件寻址与 `verify-files/process/source` 委派；`BOUNDARY` 文案留在 Python |
| M3 | `internal/harness.ArtifactPlan` / `VerifyArtifactReference` / `VerifyArtifactBinding` | `artifact-plan`、`artifact-reference`、`artifact-binding` | `Store.put` / `Store.get` 保留工件路径布局、`exists()` 发布一次判定、`atomic_bytes`、`read_bytes` 与 `json.loads` |

**M3 之后 Python 侧 `Store` 不再持有任何 `hashlib.sha256`**；`internal/harness` 成为内容寻址的唯一实现（`infra.py` 的 `hashlib` 导入仍被 `runtime_identity` 使用，不属本单元）。

M1 的边界与 C2/C4 同一原则：**Go 持有规则与格式，Python 持有进程与文件系统**。它同时**减少**一次 `canonical` 跨进程调用（原「逐材料一次 `cat-file` + 一次 `canonical`」变为「一次 `comparison-plan`」）。M2 同样把原「一次 `digest` 跨进程」并入 `task-input`。

M2 的 `BOUNDARY`（提示词里那段"反馈是不可信数据"的声明）**有意留在 Python**：它是提示词文案，整段提示词的其余约 15 句都在 Python；只搬其中一句会形成任意切分。若认为整段提示词应入 Go，应作为独立单元。合同已记录该决定并列为评审问题。

M3 的边界沿用同一原则：**Go 持有规则与格式，Python 持有进程与文件系统**。`artifacts/<ref>.json` 的位置、`path.exists()` 的发布一次判定、`atomic_bytes` 与 `read_bytes` 全留在 Python；`artifact-plan` 因此**不返回路径**。`get` 的两条拒绝被拆成**两次调用**（先 `artifact-reference` 判格式，再 `artifact-binding` 判完整性），使"格式优先于读取失败"由调用结构而非代码顺序保证；代价是 `get` 两次跨进程（约 11.6 ms），`put` 仍一次——它替换掉原本就有的那次 `canonical` 调用，**净增 0**。完整性规则对**原始字节**比较，故工件体以 **base64** 过桥：若按文本传递，被改成非 UTF-8 的篡改件会抛 `UnicodeDecodeError`，而迁移前抛的是 `HarnessError("artifact integrity mismatch")`。

**前提变更（有意，非删除）**：`tests/test_boundary_recovery.py::test_artifact_integrity` 原先用 `Bridge('unused')` 证明"工件读写不需要桥接"；M3 之后 `put`/`get` 都走桥接，该用例改用真实 bridge（`driver_bridge()`），断言逐字保留。合同第 117 行已记录该变更。

三处拒绝**不走桥接失败形状**，而走 `value` 里的类型化结果（`{"accepted": false, "refusal": ...}` / `{"ok": false, "refusal": ...}`）。C4 与 M3 的拒绝另带 `kind`，由 Python `refuse()` 映射回原异常类。失败形状（`{ok:false, code}` + 退出码 2）保留原义，用于"桥接本身坏了"——M3 中一份**非法 base64 的工件体**走这条路径，因为那是调用方错误、不是业务拒绝。

## 复核命令

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine

# 准入：读取 M3 记录并放行 G2a
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-M3.json --next-unit G2a

# 完整验收（新尝试目录，不覆盖历史）
python3 scripts/go-engine/acceptance.py --unit M3 --previous-baseline M2

# 两个工具自身的控制项（需要干净工作树）
sh scripts/go-engine/negative-controls.sh "" .gates/results/go-engine-M3.json
```

> 手工跑 Python 套件请同时设两件事：**已解析**的临时目录 `TMPDIR=/private/tmp`，以及 `PYTHONPATH="$PWD/runtime/langgraph"`。前者避开 `test_output_recovery.py` 的一个用例报错（未解析符号链接），后者覆盖掉宿主注入的 `sitecustomize` 垫片（见 `HOST-SHIM-BREAKS-PATH-OPS`）。两者都与本迁移无关，验收器自身已按此设置。

| 文件 | 内容 |
| --- | --- |
| `.gates/results/go-engine-B0.md` / `.json` | 基线冻结记录 |
| `.gates/results/go-engine-C1-contract.md` … `C4-contract.md`、`M1-contract.md`、`M2-contract.md`、`M3-contract.md` | 实施前固定的单元合同 |
| `.gates/results/go-engine-C1.md` … `C4.md`、`M1.md`、`M2.md`、`M3.md` 与同名 `.json` | 单元验收记录 |
| `scripts/go-engine/README.md` | 验收与准入工具的约定、判定条件、控制项清单 |
| `scripts/go-engine/dependency-lock.json` | Go/SQLite 驱动与依赖锁定 |

原始证据在记录的 `attempt` 字段所指目录，含 `evidence.json`（逐文件摘要）、各必需项 `summary.json`、每个命令的 stdout/stderr/exit/cmd。每次尝试使用全新目录，已存在即拒绝，**不被后续运行覆盖**。

## 明确未验证的事项

- **真实原生模型流水线**：`NATIVE-REVERIFY-AT-BASELINE` 未执行，需显式授权费用。C1–C4 与 M1–M3 都是纯计算（M1 另有 Python 侧本地 git 调用，M3 另有 Python 侧文件系统读写），**不需要**原生定向验收；授权关口在 G2b 之前。`live-20260915-2050` 的原生证据绑定更早的提交与运行时，不可当作当前候选的原生验收。
- **跨平台**：只在本地 macOS 实测。

## 已知有界差异与风险

| 编号 | 内容 |
| --- | --- |
| `LONE-SURROGATE-ESCAPE` | 文本含转义 `\ud800` 时两侧都接受，但 Python 保留代理项码位、Go 为 U+FFFD |
| `NON-FINITE-CROSSES-TRANSPORT` | 合法 JSON 不允许 `Infinity`，故含非有限浮点的值可经桥接进入编码器、但信封载不动它（C3 起）；不可达——结果路径不会产出非有限值 |
| `INPUT-PATH-NOT-NORMALISED` | C4：参考实现按 `Path` 规范化形式命名缺失的输入文件，Go 用传入的原样字符串；不可达——键由 `str(Path(...))` 写出且工件内容寻址 |
| `BOOL-SIZE-IS-A-NUMBER-IN-PYTHON` | C4：证据 `size` 为布尔值时两侧判定可能不同（Python 中 `True == 1`）；仅手工改写可达，无法由正常写入者产生。经评审定为 P3、非阻塞 |
| `COPY-STATUS-UNREACHABLE` | **M1（覆盖限制）**：生产只传 `--find-renames`，`C`／拷贝记录不可达，故差分语料**不可能**覆盖该分支。该分支由 Go 单元测试与一份逐字取自迁移前解析循环的独立裁判（17 个样本、0 分歧）覆盖 |
| `MALFORMED-RECORD-ERROR-SHAPE` | **M1（P3，不可达）**：畸形记录在迁移前触发**未捕获异常**（`ValueError`／`StopIteration`），现在返回受控的桥接错误。git 正常输出不产生该输入 |
| `TMPDIR-UNRESOLVED-SYMLINK` | **M2（环境陷阱，非迁移缺陷）**：手工跑套件时 `TMPDIR=/tmp`（未解析符号链接）会让 `test_output_recovery.py::test_explicit_adoption_keeps_manifest_and_checkpoint_bytes` 报 1 个错误，`TMPDIR=/private/tmp` 时全过。**已证与本迁移无关**（把 `feedback.py` 回退到 M2 之前，同样报错）。验收器自身使用已解析目录，不影响判定 |
| `HOST-SHIM-BREAKS-PATH-OPS` | **M3（环境陷阱，非迁移缺陷）**：本机宿主经 `PYTHONPATH` 注入 `sitecustomize` 垫片，令 `Path.mkdir(exist_ok=True)` 在目录已存在时抛 `PermissionError`、令 `shutil.rmtree` 走回收站守卫。直接探针复现；用它跑全套报 10–15 个错误（集中在 `test_retention`、`test_comparison_inputs`、`test_checkpoint_startup`）。**以 `PYTHONPATH=<repo>/runtime/langgraph` 覆盖后 174 项全过**；验收器自建 `PYTHONPATH`，判定不受影响。任何手工跑套件的人都会撞上 |
| `ARTIFACT-READ-COSTS-TWO-CALLS` | **M3（有界代价）**：`get` 由 0 次跨进程调用变为 2 次（约 11.6 ms），`put` 净增 0。`graph.py` 中 `get` 会被调用数十次（含列表推导内多次），单次 run 量级为百毫秒、远小于模型调用。合并为一次须接受"格式优先"只靠代码顺序成立，或让 Go 读文件并复现 `FileNotFoundError`/`IsADirectoryError` 的类型与消息。经评审定为三项方案中唯一不需要为异常语义写例外者 |
| `ARTIFACT-LOCATION-STAYS-IN-PYTHON` | **M3（待评审）**：`artifacts/<ref>.json` 的命名未入 Go。§7 只说"正文…在工件目录中"、未规定命名，故按"存储布局属 Python"处理，`artifact-plan` 不返回路径。若认为该命名属寻址格式，应作为独立单元 |
| `INDEX-MISSING-M2-ROW` | **M3（文档，已修）**：M2 的记录提交改了索引的两张表，却漏掉"基线与验收链"表的 M2 行；已在 M3 索引更新中补上。它说明"索引已更新"这句自述不足以证明更新完整 |
| `WORKTREE-LOCATION-AMBIGUOUS` | **过程项**：本项目以独立 worktree 开发，绑定 `codex/go-engine-v1` 的目录才是当前工作区；按会话默认目录侦察会误判为"工作丢失"。以 `git worktree list` 为准 |
| `ENV-THROUGHPUT-UNIDENTIFIED` | 独立环境整体吞吐约为本机 1/4，成因未查明；回归判定已改为不依赖吞吐 |
| `BUILD-PATH-IN-RUNTIME-IDENTITY` | `go build` 不加 `-trimpath` 时运行时身份随构建路径变化 |
| `NODE-PATH-AMBIGUOUS` | 两份 node 摘要不同，跨 shell 恢复可能触发 `HOST_CHANGED` |
| `CANONICAL-CALL-CROSSES-PROCESS` / `PROOF-CALL-CROSSES-PROCESS` | 细粒度原语跨进程约 6 ms/次，已测量 |
| `PYTHON-SUITE-FLAKY-UNREPRODUCED` | 上一轮评审看到 1 FAIL + 1 ERROR，本机未复现；该启动链上的 0.53s CLI 导入已移除，其后 6 次全套（C4 期间）全部通过、0 复现 |
| `ZCODE-HEADLESS-BLOCK` | ZCode 无头在原生 Edit 审批处按合同返回 `ZCODE_INTERACTION_REQUIRED` |
| `GOFMT-WINDOWS-FILE-PREEXISTING` | **既有，非本迁移**：`internal/engine/definition/replace_file_windows.go` 不满足 `gofmt`。已在 M2 基线提交 `dfa23e7` 上复现（将该版本取出后 `gofmt -l` 仍报它），故与 G1a/G1b 无关；各单元合同的 gofmt 要求只覆盖 `internal/harness/`，该目录**干净**。留作独立修复项 |

## 评审时值得重点看的点

1. **C4 的边界划得对不对**：哪些属证据校验（已入 Go）、哪些属存储层（路径判断与文件读取留在 Python），以及"有序证明列表"是否为最小充分的表达方式。
2. **两处不可达差异的记录方式**：`NON-FINITE-CROSSES-TRANSPORT` 与 `INPUT-PATH-NOT-NORMALISED` 都以"实测差异 + 不可达论证"记录，而非加机制。是否认可这种处置，或应改为结构性消除。
3. **跨进程原语的取舍**：`file_proof` 从 0.036 ms 变为 6.1 ms；C4 未新增高频路径，但校验仍逐文件跨进程。是否应在此阶段就引入批量操作。
4. **类型边界上的静默收窄为何反复出现**：C1 的 NaN 预扫描、C3 的 float64 收窄、C4 的整数截断与 `7.0` 误拒、M2 的 `role` 改写与 revision 字符串化——**六处，同一个模式**：Go 侧把"参考实现的动态类型语义"实现成了更窄的静态形状。C4 起补的"经桥接的端到端等价用例 + 负控制"在 M2 确实生效（两处由我在外部评审前查出），但模式仍在复发，说明**逐例修补不够**。是否需要一条结构性纪律（例如：凡从 JSON 取出的值，禁止在 Go 侧做类型断言式的默认值替换，只能按参考实现的取值语义原样透传），是本阶段最值得质疑的一点。
5. **G1b 的拆分与各项边界**：M1 把"解析+表示规则"入 Go、把"git 调用与文件系统"留在 Python；M2 把两个包装形状入 Go、把 Store 访问与 `BOUNDARY` 文案留在 Python；M3 把内容寻址与两条判定入 Go、把工件路径布局与原子写留在 Python。这三个切法是否合理；`COPY-STATUS-UNREACHABLE` 该按"已由单元测试与独立裁判覆盖"接受，还是应改为结构性消除（例如断言选项集不可能产生 `C`，或让 Go 拒绝未知状态）。
6. **`BOUNDARY` 留在 Python 是否可接受**：它是否算"反馈包装"的一部分（§9.4 的措辞），还是纯提示词文案。若应入 Go，范围是整段提示词而非其中一句。
7. **M3 的"两次调用"是否为正确取舍**：`get` 拆成 `artifact-reference` + `artifact-binding`，使"格式优先于读取失败"成为结构性质，代价是每次读 2 次跨进程（约 11.6 ms，`graph.py` 内单次 run 数十次调用）。被否的两案是"合成一次调用、靠代码顺序保序"与"让 Go 读文件并复现异常语义"。若评审认为该代价不可接受，需要指认一个不会为异常语义写例外的替代。
8. **工件路径为何不入 Go**：`artifact-plan` 返回引用与规范字节、不返回 `artifacts/<ref>.json`。依据是 §7 只规定"正文在工件目录中"、未规定命名。是否应把该命名视为寻址格式的一部分而搬入 Go（即 `ARTIFACT-LOCATION-STAYS-IN-PYTHON` 是否应升级为阻塞项）。
9. **`test_boundary_recovery::test_artifact_integrity` 的前提变更是否可接受**：该用例原先断言"工件读写不需要桥接"，M3 后改用真实 bridge 而断言逐字保留。这是有意的前提变更（合同已登记），但它是**唯一一处**为迁移而改动既有测试前提的地方，值得单独审视。
10. **D1 的处置是否恰当**：交付的差分测试令两侧共用同一工件目录（写路径未被按值差分、两棵树未被互比）。我按缺陷处理并重写，但也可以论证它"没有造成可观察行为偏差"故应算测试质量问题。此外，修复的第二次派发返回空、由我自行实施——是否需要在流程上要求"派发失败必须显式回报"。

本索引只汇总各单元记录中已观测的结果；不含密钥或认证材料。
