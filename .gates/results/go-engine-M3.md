# M3 验收记录：工件发布（G1b 第三项，G1b 收口）

## 结论

替换单元 M3 通过，被固定为下一基线。**G1b 三项（比较材料、反馈包装、工件发布）全部通过，G1b 收口**；下一单元为 G2a（命令作业）。

| 检查 | 结果 |
| --- | --- |
| 本块合同（Go 单元测试 `artifact_test.go`） | 通过：14 个测试函数，覆盖引用推导、格式规则、完整性规则、多字节引用等价、binding 不重查格式、base64 往返 |
| 本块合同（差分对照） | 通过：17 项，引用、落盘字节、拒绝类别与措辞与迁移前实现逐字一致 |
| 独立裁判比对（我自己构造，**不依赖**交付的测试与参考模块） | 通过：**87 行检查，0 分歧**（28 个值的 ref 与落盘字节双层比对、26 条 `get` 形态、3 条重入、2 条往返计数） |
| **独立复核发现 1 处交付缺陷（D1）** | 交付的差分测试让两侧**共用同一个工件目录**，与合同要求相悖。已修 |
| **负控制** | 通过：7 处定向变异**全部被检出**；变异后源码逐字节还原 |
| 累计组合完整流程（fixture） | 实际达到 `DEMO_COMPLETE` |
| 累计自动化回归（Go 无缓存全量） | 通过：26 包、23 包执行、3 包无测试文件、**2034 用例通过事件、0 失败、`stall=None`** |
| 累计自动化回归（Python 全套） | 通过，**174 项** |
| SQLite 探针 | 通过，CGO 开/关各 8/8 |
| DSH / ZCode `doctor` | 通过，接入探测 |
| 原生定向验收 | **不适用**：纯计算 |
| **总体** | **`PASS`** |

上一验收基线：`M2`（候选 `dfa23e7`，记录提交 `72b5aba`）。

## 本单元搬了什么

`Store.put` 的**引用推导**与 `Store.get` 的**两条判定**搬进 Go，经桥接操作 `artifact-plan`、`artifact-reference`、`artifact-binding` 暴露。合同依据是方案 §13.1 G1b 行（"工件发布…保持内容绑定及重复调用语义"，验收出口"中途失败和重入不覆盖已绑定内容"）、§9.3（"重试不得覆盖已绑定的不同内容"）与 §7（"工件通过临时文件、完整写入、必要同步和原子发布生成…有效结果不得指向未完成文件"）。

| 归属 | 内容 |
| --- | --- |
| **Go（新）** | `ref = sha256(CanonicalJSON(value))`（连同 canonical 字节一并返回）；引用**格式**规则（`invalid artifact reference`）；**完整性**规则（`artifact integrity mismatch`） |
| **Python（不变）** | 工件位置 `artifacts/<ref>.json`、`path.exists()` 的**发布一次**判定、`atomic_bytes` 的临时文件 + fsync + `os.replace`、`read_bytes()` 的读取与全部文件系统异常、`json.loads(body)` 解码 |

搬完后 Python 侧 `Store` 不再持有任何 `hashlib.sha256`；`internal/harness` 成为内容寻址的唯一实现（`infra.py` 的 `hashlib` 导入仍被 `runtime_identity` 使用，不属本单元）。

### 为什么 `get` 是两次调用

迁移前的 `get` 有两条各自独立的拒绝，顺序为**格式 → 读取 → 完整**。两次调用把这个顺序变成**结构性**的：`artifact-reference` 在任何文件系统访问之前给出判定，`artifact-binding` 只做比较（**明确不重查格式**，Go 侧有专门用例钉住这一点）。代价是每次 `get` 两次桥接调用（实测 5.8 ms/次），`put` 仍是一次——替换掉它原本就有的那次 `canonical` 调用，**净增 0**。

候选方案与被否原因：把两条规则合成一次调用、靠"读失败后重抛"保留顺序，可以省一次调用，但那样"格式优先于读取失败"就只靠代码顺序成立；让 Go 自己读文件（C2 的 `file-proof` 先例）则要 Go 侧复现 `FileNotFoundError`/`IsADirectoryError` 的类型与消息（合成异常或跨平台 errno 映射）。**两次调用是三者中唯一不需要为异常语义写例外的方案**，也是独立裁判 87 行里 3 条"格式优先"用例（畸形引用 + 文件存在 / 短引用 + 文件存在 / 畸形引用 + 路径是目录）能逐条对齐的原因。

### 字节为什么用 base64

完整性规则是**对字节**的比较。若以文本传递，被改成非 UTF-8 的篡改件会在解码处抛 `UnicodeDecodeError`，而迁移前抛的是 `HarnessError("artifact integrity mismatch")`。base64 使该情形仍报完整性拒绝，并有用例与负控制各钉一次。

## 独立复核发现的 1 处缺陷

| 编号 | 缺陷 | 症状 | 处置 |
| --- | --- | --- | --- |
| D1 | 交付的差分测试令**两个实现共用同一个工件目录**，与合同"各自使用独立临时目录"的明确要求相悖 | ① `compare_put` 里参考实现先写文件，被测 `Store.put` 随后发现文件已存在而**跳过写入**——被测的写路径从未按值被差分；② 两棵工件树从未被互相比较，"引用与字节一致"只是顺带成立；③ 原生 OS 异常的消息相等只因为两侧路径字面相同，并没有证明**消息形状**在不同根下等价 | 两侧各自独立目录；比较前把各自根替换为 `<root>` 再比对消息；新增"两棵树逐字节相同"用例；`compare_put` 由死代码改为被实际使用 |

D1 是我在验收前复核交付物时查出的，**不是外部评审发现的**。它没有造成可观察行为偏差（写错字节会被 `get` 的完整性判定拦下），但它让"字节一致"这项判据退化为顺带成立——而它正是 G1b 验收出口"重入不覆盖已绑定内容"的直接证据，因此按缺陷处理而不是按风格问题放过。

**过程记录**：该修复的第二次派发**没有产生任何改动**（子代理返回空），我因此自行实施 D1 的修复。此处如实登记，避免"已派发"被读成"已由另一执行者独立完成"。

**同批发现（文档，非本单元）**：更新索引时发现 M2 的记录提交改了索引的两张表，却**漏掉"基线与验收链"表的 M2 行**。已在本次索引更新中补上（见下）。

## 负控制：7 处定向变异，全部被检出

| 变异 | Go 用例 | 差分对照 |
| --- | --- | --- |
| 引用格式丢掉 64 字符长度检查 | 失败 | 失败 |
| 引用格式接受大写十六进制 | 失败 | 失败 |
| **binding 重新检查引用格式**（两操作塌成一个） | 失败 | — |
| **Python 在读文件之前不做格式判定**（顺序反转） | — | 失败 |
| `put` 覆盖已存在工件（去掉发布一次） | — | 失败 |
| 落盘字节多一个换行 | — | 失败 |
| 工件体按文本而非 base64 传递 | — | 失败 |

"binding 重查格式"与"顺序反转"两项分别钉住了本单元的两个设计决定：前者证明两个操作**确实是分开的**，后者证明拒绝顺序**由结构而非代码顺序保证**。全部变异已逐字节还原（`shasum` 校验：`artifact.go` `8e338e2717e4827a`、`infra.py` `3a8b1abbbe2f2a5f`），还原后两层套件均恢复通过。

## 候选身份

```text
工作树   /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
分支     codex/go-engine-v1
提交     d71113e5ada1f84c9f6f5d94b022e33dfba477e3
上一基线 dfa23e77d4518976b0b2c0909e845344bae15d58（M2）
运行时身份 e15fde533671693613af3fd968a65e9da3d3bec86f1075be57cc90e642ad6530
驱动二进制 be75b3cf899e26ede32d4afe63f2521c8d173e10c3beb1a91b603f6cdc0f803a
证据清单 64 个文件
通过尝试 20260917T045618Z-80157
```

驱动二进制由候选源码重建后与已安装副本摘要一致，且**另建到不同输出路径也得同一摘要**——即 `BUILD-PATH-IN-RUNTIME-IDENTITY` 影响的是运行时身份字符串，不影响二进制摘要。

## 累计回归的实际结果

| 项 | 耗时 | 说明 |
| --- | --- | --- |
| `identity` | 0.5s | |
| `go-vet` | 0.5s | |
| `go-test-full` | 414.4s | 26 包、23 包执行、3 包无测试文件、**2034 用例通过事件**、0 失败、`stall=None`；最慢 `internal/validate` 407.1s、`internal/cli` 93.4s |
| `sqlite-probe` | 2.5s | CGO 开/关各 8/8 |
| `python-unittest` | 62.0s | **174 项** |
| `demo-fixture` | 3.6s | 达 `DEMO_COMPLETE` |
| `doctor-dsh` / `doctor-zcode` | 2.7s / 3.1s | 接入探测 |
| 合计 | 489.3s | |

相对 M2：Go 用例通过事件 `2020 → 2034`（+14 = 本单元 14 个 Go 测试），Python 用例 `157 → 174`（+17 = 本单元交付的 17 项差分用例）。**两处增量都与新增用例数精确对应，既有用例未被改写。**

M3 只跑了**一个**尝试（`20260917T045618Z-80157`），`overall=PASS`、`failed=[]`。

## 未解决问题

| 编号 | 状态 |
| --- | --- |
| `HOST-SHIM-BREAKS-PATH-OPS` | **新增（环境陷阱，非迁移缺陷）**：本机宿主通过 `PYTHONPATH` 注入一个 `sitecustomize` 垫片，它令 `Path.mkdir(exist_ok=True)` 在目录已存在时抛 `PermissionError`、令 `rmtree` 走回收站守卫。直接探针复现；用它跑全套会报 10–15 个错误（集中在 `test_retention`、`test_comparison_inputs`、`test_checkpoint_startup`）。**以 `PYTHONPATH=<repo>/runtime/langgraph` 覆盖后 174 项全过**，验收器本身就是这样设置的（`acceptance.py` 自建 `PYTHONPATH`），故不影响判定。任何手工跑套件的人都会撞上它 |
| `ARTIFACT-READ-COSTS-TWO-CALLS` | **新增（有界代价）**：`get` 由 0 次跨进程调用变为 2 次（约 11.6 ms），`put` 净增 0。`graph.py` 中 `get` 会被调用数十次（含 `graph.py:213-214`、`236-237` 的列表推导内多次调用），单次 run 量级为百毫秒、远小于模型调用。若要合并为一次，须接受"格式优先"只靠代码顺序成立，或让 Go 读文件并复现异常语义 |
| `ARTIFACT-LOCATION-STAYS-IN-PYTHON` | **新增（待评审）**：`artifacts/<ref>.json` 未入 Go。§7 只说"正文…在工件目录中"、未规定命名，故按"存储布局属 Python"处理；`artifact-plan` 因此不返回路径。若认为该命名属寻址格式，应作为独立单元 |
| `TMPDIR-UNRESOLVED-SYMLINK`、`COPY-STATUS-UNREACHABLE`、`MALFORMED-RECORD-ERROR-SHAPE`、`BOOL-SIZE-IS-A-NUMBER-IN-PYTHON`、`NON-FINITE-CROSSES-TRANSPORT`、`INPUT-PATH-NOT-NORMALISED`、`WORKTREE-LOCATION-AMBIGUOUS` 等 | 保留，不变 |
| `INDEX-MISSING-M2-ROW` | **新增（文档，已修）**：M2 记录提交漏掉索引"基线与验收链"表的 M2 行；本次补上。它说明"索引已更新"这句自述不足以证明更新完整 |
| G1b | **已完成**：M1、M2、M3 全部通过。下一单元 G2a（命令作业） |

## 回退入口

```sh
git -C /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine \
  reset --hard dfa23e77d4518976b0b2c0909e845344bae15d58
```

与 M1、M2、C4 记录同一约定：指向上一基线的**候选**提交。

## 复核入口

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine

# 准入：核验本记录并放行 G2a
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-M3.json --next-unit G2a

# 本块合同
go test ./internal/harness -run 'Artifact' -v

# 差分对照（PYTHONPATH 覆盖掉宿主的 sitecustomize 垫片，见上）
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$PWD/runtime/langgraph" TMPDIR=/private/tmp \
  .harness/venv/bin/python -m unittest discover -s runtime/langgraph/tests \
  -p 'test_artifact_publication_differential.py' -v

# 桥接操作直查：非 UTF-8 的工件体必须报完整性拒绝，而不是解码错误
printf '%s' '{"operation":"artifact-binding","payload":{"ref":"'"$(printf 'x' | shasum -a 256 | cut -d' ' -f1)"'","body_b64":"//4Abm90LWpzb24="}}' \
  | .harness/bin/formal-gates harness bridge
```

单元合同：`.gates/results/go-engine-M3-contract.md`。
本记录只汇总已观测结果；日志保留原始失败，不以最终通过覆盖中间尝试。
