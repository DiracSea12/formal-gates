# G1 状态索引（供外部评审）

> 文件名沿用 `go-engine-G1a-index.md`（C4 记录的 `bookkeeping` 引用它），内容自 M1 起覆盖 G1b。

G1a 是分块替换的第一阶段：**只读计算**。方案要求解码与证据校验**分别验收**。

> **G1a 已完成。** 四个替换单元（C1 结果解码、C2 证据内容摘要原语、C3 规范化与内容摘要、C4 证据校验）各自通过并被固定为下一基线。
>
> **G1b 已完成。** 方案要求比较材料、反馈包装、工件发布**分别验收**，三项各自通过：**M1 比较材料**（候选 `f51d1ac`）、**M2 反馈包装**（候选 `dfa23e7`）、**M3 工件发布**（候选 `d71113e`）。
>
> **G2a 未收口（命令作业半通过，承载原生传输半受阻）。** G2a 有两半：**命令作业**（Go 监督器的直接 argv 路径）**通过**（候选 `63b0650`）；**验证它承载固定的原生传输**（方案 §13.1：G2a"先验命令监督，**再验其承载原生传输**"）在 2026-09-17 经授权执行一次真实 DSH 运行后**受阻**——运行在第一个角色处停在宿主权限请求上，宿主返回 `DSH_INTERACTION_REQUIRED`。按方案 §13.1"必要原生验证受权限、服务或费用授权阻塞时，当前单元保持未验收"与 §14.3"必需测试未结束、被跳过或原生验证受阻 | 保持未验收；不将缺测视为 PASS 或改标不适用"，**G2a 保持未验收，G2b 不得开始**。
>
> **命名更正**：本索引此前把 G2b 写作"原生定向验收关口"，这是错的。方案 §13.1 的表把 **G2b 定义为"DSH 薄适配"**；原生定向验收是 **G2a 自己的后半**。G2a 的记录也同错，已一并更正。**同一错误还在单元合同里**（`go-engine-G2a-contract.md` 第 178 行"授权关口在 G2b 之前"）：合同是派发前固定、与候选同批落地的历史件，改写它会动到候选，故**不改动**，更正以本索引与 G2a 记录为准。

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
| G2a | 命令作业（Go 监督器的直接 argv 路径）+ 验证它承载原生传输 | `63b0650` | 见 `go-engine-G2a.json` | `BLOCKED`（命令作业半 `PASS`；承载原生传输半受阻，见下） |

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

**G2a 查出 3 处（D1、D2、D3），三处都被子代理的自述报告为"全绿"。**

| 编号 | 缺陷 | 性质 |
| --- | --- | --- |
| D1 | PATH 走查在第一个"存在"的候选处停住（`os.Stat` 预筛），而 `execvp` 会继续搜索 | **实现缺陷**，自建 oracle 16 个场景中 5 个分歧 |
| D2 | 组信号取消时收据不确定：`130` 与子进程自身的 `-15` 交替 | **实现缺陷**，参考实现 20/20 恒定 |
| D3 | 四个取消用例等到 `process.json`（写在 handler 装载**之前**）就发信号，抢跑启动窗口 | **用例缺陷**，只在完整验收的负载下 20 轮中失败 1 轮 |

D1、D2 由自建 oracle 在验收前查出，修复由第二次派发完成。**D3 特殊之处在于它只在完整验收里才现形**：单独跑该包 5 次全过，而 `go test ./...` 的负载下失败——这正是"以完整验收为准、不以单包通过为准"的经验依据。D3 的处置是**修用例、不动实现**，因为那个窗口是参考实现固有的，在窗口里补写收据会让 Go 在参考实现根本没有收据的地方给出收据（`Store.job` 对缺失收据的处置是 `RuntimeBlocked(UNKNOWN_JOB)`，可观察地不同）。

**与既有模式的对应关系**：

- **D1 是"类型边界上的静默收窄"的第 7 次出现**（前六次：C1 的 NaN 预扫描、C3 的 float64 收窄、C4 的整数截断与 `7.0` 误拒、M2 的 `role` 改写与 revision 字符串化）。它把**内核对候选的可执行性判定**换成了 `os.Stat` 的存在性判定——动态语义被实现成更窄的静态形状，与前六次同型。
- **D2、D3 是新的一类**：不是类型收窄，而是**把间接证据当成直接证据**。D2 用一个并发边界上共享的标志代替"这次死亡是否由取消引起"的直接判定；D3 用一个更早写入的标记（`process.json`）代替"handler 确已装载"的就绪证据。两者都是"信号比证据先到"的同一个错误。
- 三处都不是笔误。三处已各配回归用例（D1：`path_search_oracle.py`；D2：`cancel_oracle.py` + `TestSuperviseCancelGroup`；D3：四个取消用例的 marker 等待）。

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

## G2 的剩余工作

| 单元 | 内容（方案 §13.1 的定义） | 状态 / 前置 |
| --- | --- | --- |
| G2a | 命令作业：**先**交付 Go 监督器的直接 argv 检查路径，**再**验证它承载固定的原生传输；不同时更换宿主协议 | **未收口**：命令作业半通过（候选 `63b0650`）；承载原生传输半 **受阻**，见下 |
| G2b | **DSH 薄适配**：在已验收监督器下移植 DSH 的请求、身份、结果和错误映射；ZCode 保持原实现 | 前置为 **G2a 收口**（当前未满足） |
| G2c | ZCode 薄适配：使用同一公共边界移植 ZCode；DSH 实现不同时重构 | G2b 之后 |
| G2d | 原生交互：单独交付原生请求的持有、展示、投递和原调用继续；按宿主逐个验收 | 按宿主逐个验收 |

**G2a 后半为什么受阻，以及为什么这不构成回归。** 2026-09-17 经授权执行的一次真实 DSH 运行在第一个角色（产品审）处停住：该角色在自己的 `bash` 参数里写 `sandbox_permissions: "danger-full-access"`（即**模型自己请求越出只读沙箱**），DSH 据此发出 `session/request_permission`，而本版本**没有**原生答复通路，故如实报告 `DSH_INTERACTION_REQUIRED`。这与既有的 `ZCODE-HEADLESS-BLOCK` 同型——先前验收记录对 ZCode 在真实 Edit 审批处的同类阻塞写的正是"这符合既定交互边界"。方案 §12.4/§13.1 把原生交互**单列为 G2d**，正是为了让 G2a/G2b 不顺手获得该能力；§6.2 与 §14.2 明令"不靠免审批模式让测试通过""测试不靠临时强制授权凑通过"。

对照同一宿主、同一版本下先前那次七个角色全部通过的运行（`live-20260915-2050`）：工具策略逐项相同（仅 job 目录路径不同），该次 32 次 `bash`（其中 **17 次**越出 job 目录、**13 次**运行同类检查脚本）**0 次**提权、七个作业**0 次**权限请求。因此触发者是**模型自己的提权请求**，不是路径范围、不是配置、也不是 Go 监督器。

**Go 监督器的那一半是被证明的**：它把原生入口 argv 当普通子进程执行、写出了六字段收据（`exit_code=1`、`error=""`、`request_digest` 与两份流证明齐备），且 `native-process.json` 显示**监督器是进程组组长**（pgid == 自身 pid）而原生子进程是它的子进程；迁移前的 Python 监督器已不可运行（模块已删），故那份收据只能来自 Go 二进制。**缺的是完成关联**——没有任何角色抵达完成的轮次，而方案 §13.1 给该检查规定的最小证据含"原生请求及**完成关联**"。证据在 `.harness/validation/g2a-native-20260917T103650Z/`。

`Store.job` 在本单元只改了**一行** spawn argv，其余（owner 锁、存活探测、收据轮询）与 `Store.cancel_jobs` 全部未动。原生传输本身由 Python 侧的 `formal_gates_harness.native_entry` 承载，Go 只负责把它当普通子进程执行——G2a 后半要验收的正是这一点。

仍是宿主编配器生命周期策略、随 G2b/G2c 迁移的：`verify_source` / `validate_binding`。

## 本阶段搬动的八项

| 单元 | 新归属（Go） | 桥接操作 | Python 侧 |
| --- | --- | --- | --- |
| C1 | `internal/harness.FrameResult` | `frame-result` | `native_result.decode_result` 变为适配器 |
| C2 | `internal/harness.FileProof` | `file-proof` | `infra.file_proof` 变为适配器 |
| C3 | `internal/harness.CanonicalJSON` / `CanonicalDigest` | `canonical`、`digest` | `infra.canonical` / `infra.digest` 变为转发 |
| C4 | `internal/harness.VerifyProcess` / `VerifyFiles` | `verify-process`、`verify-files` | `Store.verify_process` / `verify_evidence` 保留路径判断与文件读取，校验委派 |
| M1 | `internal/harness.planComparison` | `comparison-plan` | `feedback.materialize_comparison` 保留 git 调用、文件读写、篡改检查与证明收集 |
| M2 | `internal/harness.planTaskInput` / `planEvidenceFeedback` | `task-input`、`evidence-feedback` | `feedback.task_input` / `evidence_feedback` 保留 Store 访问、工件寻址与 `verify-files/process/source` 委派；`BOUNDARY` 文案留在 Python |
| M3 | `internal/harness.ArtifactPlan` / `VerifyArtifactReference` / `VerifyArtifactBinding` | `artifact-plan`、`artifact-reference`、`artifact-binding` | `Store.put` / `Store.get` 保留工件路径布局、`exists()` 发布一次判定、`atomic_bytes`、`read_bytes` 与 `json.loads` |
| G2a | `internal/harness.Supervise` | **无**（改用 §12.1 的**同一二进制内部窄命令** `harness supervise <dir>`） | `Store.job` **只改 spawn argv 一行**；owner 锁、存活探测、收据轮询与 `Store.cancel_jobs` 全部未动 |

**M3 之后 Python 侧 `Store` 不再持有任何 `hashlib.sha256`**；`internal/harness` 成为内容寻址的唯一实现（`infra.py` 的 `hashlib` 导入仍被 `runtime_identity` 使用，不属本单元）。

M1 的边界与 C2/C4 同一原则：**Go 持有规则与格式，Python 持有进程与文件系统**。它同时**减少**一次 `canonical` 跨进程调用（原「逐材料一次 `cat-file` + 一次 `canonical`」变为「一次 `comparison-plan`」）。M2 同样把原「一次 `digest` 跨进程」并入 `task-input`。

M2 的 `BOUNDARY`（提示词里那段"反馈是不可信数据"的声明）**有意留在 Python**：它是提示词文案，整段提示词的其余约 15 句都在 Python；只搬其中一句会形成任意切分。若认为整段提示词应入 Go，应作为独立单元。合同已记录该决定并列为评审问题。

M3 的边界沿用同一原则：**Go 持有规则与格式，Python 持有进程与文件系统**。`artifacts/<ref>.json` 的位置、`path.exists()` 的发布一次判定、`atomic_bytes` 与 `read_bytes` 全留在 Python；`artifact-plan` 因此**不返回路径**。`get` 的两条拒绝被拆成**两次调用**（先 `artifact-reference` 判格式，再 `artifact-binding` 判完整性），使"格式优先于读取失败"由调用结构而非代码顺序保证；代价是 `get` 两次跨进程（约 11.6 ms），`put` 仍一次——它替换掉原本就有的那次 `canonical` 调用，**净增 0**。完整性规则对**原始字节**比较，故工件体以 **base64** 过桥：若按文本传递，被改成非 UTF-8 的篡改件会抛 `UnicodeDecodeError`，而迁移前抛的是 `HarnessError("artifact integrity mismatch")`。

G2a **是这条原则的第一次边界位移，而不是它的例外**。前七项都在"Go 持有规则与格式，Python 持有进程与文件系统"下划界；G2a 搬动的恰恰**就是进程所有权**，与桥接操作的前提（请求/响应、短调用）直接冲突——监督器要长期存活、要在收到信号时自己改写收据，子进程必须是**监督器的**子进程而非 Python 适配器的。因此改用方案 §12.1 的另一条路径：**同一二进制的内部窄命令**。代价是引入第二类进程间接口（argv + 退出码，无信封），收益是进程所有权与信号语义不必被翻译。

边界随之变为：**Go 持有"取得所有权到写出收据"的全过程**，Python 只保留"何时启动、如何轮询、如何取消"。

这也意味着 §12.1 的两条路径在本项目里都已实际使用（桥接操作 C1–M3、内部窄命令 G2a），**"新单元该走哪条路径"本身成为需要论证的设计决定**，已列入下方评审问题。

**前提变更（有意，非删除）**：`tests/test_boundary_recovery.py::test_artifact_integrity` 原先用 `Bridge('unused')` 证明"工件读写不需要桥接"；M3 之后 `put`/`get` 都走桥接，该用例改用真实 bridge（`driver_bridge()`），断言逐字保留。合同第 117 行已记录该变更。

三处拒绝**不走桥接失败形状**，而走 `value` 里的类型化结果（`{"accepted": false, "refusal": ...}` / `{"ok": false, "refusal": ...}`）。C4 与 M3 的拒绝另带 `kind`，由 Python `refuse()` 映射回原异常类。失败形状（`{ok:false, code}` + 退出码 2）保留原义，用于"桥接本身坏了"——M3 中一份**非法 base64 的工件体**走这条路径，因为那是调用方错误、不是业务拒绝。

## 复核命令

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine

# 准入：读取最新一条记录（G2a）并尝试放行 G2b
# **预期被拒绝**：G2a 的总体结论是 BLOCKED（后半受阻），故不得放行 G2b。
# 实际输出：`ADMISSION REFUSED: G2a: recorded overall conclusion is 'BLOCKED'`，退出码 1。
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-G2a.json --next-unit G2b

# 完整验收（新尝试目录，不覆盖历史）
python3 scripts/go-engine/acceptance.py --unit G2a --previous-baseline M3

# 两个工具自身的控制项（需要干净工作树）
sh scripts/go-engine/negative-controls.sh "" .gates/results/go-engine-G2a.json

# 历史单元的复核入口仍有效，逐条读取各自记录即可
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-M3.json --next-unit G2a
```

> G2a 之前的单元（B0、C1–C4、M1–M3）的准入命令只在其记录中的候选仍是 `HEAD` 祖先时通过；`admission.py` 会因"候选已移动"拒绝，这是**预期行为**，不是故障。要复核历史单元请用其记录内的"复核入口"在对应的候选提交上操作。

> 手工跑 Python 套件请同时设两件事：**已解析**的临时目录 `TMPDIR=/private/tmp`，以及 `PYTHONPATH="$PWD/runtime/langgraph"`。前者避开 `test_output_recovery.py` 的一个用例报错（未解析符号链接），后者覆盖掉宿主注入的 `sitecustomize` 垫片（见 `HOST-SHIM-BREAKS-PATH-OPS`）。两者都与本迁移无关，验收器自身已按此设置。

| 文件 | 内容 |
| --- | --- |
| `.gates/results/go-engine-B0.md` / `.json` | 基线冻结记录 |
| `.gates/results/go-engine-C1-contract.md` … `C4-contract.md`、`M1-contract.md`、`M2-contract.md`、`M3-contract.md`、`G2a-contract.md` | 实施前固定的单元合同 |
| `.gates/results/go-engine-C1.md` … `C4.md`、`M1.md`、`M2.md`、`M3.md`、`G2a.md` 与同名 `.json` | 单元验收记录 |
| `.gates/results/g2a-probes/` | **G2a 的独立裁判与实测脚本**：`reference_semantics.py`（迁移前语义的重新实测）、`path_search_oracle.py`（D1）、`exec_semantics_oracle.py`（`-N` 退出码与空 PATH 分量）、`cancel_oracle.py`（D2）。四者都**不依赖**交付的差分测试与参考模块的调用方式 |
| `scripts/go-engine/README.md` | 验收与准入工具的约定、判定条件、控制项清单 |
| `scripts/go-engine/dependency-lock.json` | Go/SQLite 驱动与依赖锁定 |

原始证据在记录的 `attempt` 字段所指目录，含 `evidence.json`（逐文件摘要）、各必需项 `summary.json`、每个命令的 stdout/stderr/exit/cmd。每次尝试使用全新目录，已存在即拒绝，**不被后续运行覆盖**。

## 明确未验证的事项

- **真实原生模型流水线**：`NATIVE-REVERIFY-AT-BASELINE` 未执行。**G2a 需要有原生定向验收**——它搬的是**进程所有权**，按方案 §13.1 第 513 行属"改动涉及…进程…时，实测受影响宿主的对应路径"，按同章第 520 行属"涉及宿主或进程行为的单元必须增加对应真实调用，不能用录制数据代替"。2026-09-17 经授权执行过一次，**受阻**于宿主权限请求（见"G2 的剩余工作"），故仍是未验证事项。C1–C4、M1–M3 则确实**不需要**：C1–C4 是纯计算，M1 另有 Python 侧本地 git 调用，M3 另有 Python 侧文件系统读写；它们的差分与独立裁判全部在本地子进程上完成，`demo-fixture` 达到 `DEMO_COMPLETE` 也只是夹具流程，不算原生验收。
  - **本索引此前在这里写错了一句**：原文称"G2a 也**不需要**原生定向验收……授权关口在 G2b 之前"。那是把 G2a 的后半误记成了 G2b 的关口（见文首命名更正），并且与 §13.1 第 513/520 行冲突。更正后的读法见上。
  - `live-20260915-2050` 的原生证据绑定更早的提交与运行时，**不可**当作当前候选的原生验收；它在本索引中的用途只有一个——作为"同一宿主同版本下七个角色可以跑通"的对照，用来定位本次阻塞的成因（见"G2 的剩余工作"）。
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
| `CANCEL-WINDOW-REFERENCE-INHERITED` | **G2a（有意保留的继承行为）**：`process.json` 写在 SIGTERM handler 装载**之前**（Go 与 `reference_job.py` 第 31/38 行同序），落在该窗口的组信号会让监督器**无收据**地以 `signal: terminated` 结束。两侧一致，故**不关闭**——在窗口里补写收据会让 Go 在参考实现根本没有收据处给出收据，而 `Store.job` 对缺失收据的处置是 `RuntimeBlocked(UNKNOWN_JOB)`，可观察地不同。实测 6 轮中两侧均未落进窗口（0/6），但窗口客观存在；G2a 的 D3 即由此而来 |
| `CANCEL-POSTCHILD-WINDOW` | **G2a（已知有界差异，未构造用例）**：参考实现的取消 handler 以 `raise KeyboardInterrupt` 实现，会在**任意**执行点抛出；若信号落在外层 `try` 结束之后、写 `exit.json` 之前，参考实现带 traceback 退出且**不写收据**，而 Go 在等待循环返回后不再理会已缓冲的信号、会**正常写收据**。窗口极窄（子进程收割与收据落盘之间），Go 侧更稳定；按"实测差异 + 不可达论证"记录，未加机制 |
| `SUPERVISE-NATIVE-FORM-DIGEST` | **G2a（测试约定，非差异）**：原生入口形态下，两侧 `argv` 里嵌的是**各自**的作业目录，故 `request_digest` **合法地不同**；差分用例只断言**形状**相同（退出 1、`error=""`、写出 `native-error.json`、两份流证明齐备）。这与 M3 的 D1 同属"两侧不能共享目录"的约束 |
| `EMPTY-PATH-COMPONENT-ORACLE-BLIND` | **G2a（覆盖边界）**：`PATH` 空分量（表示当前目录）这一分支在 macOS 上**不被**执行语义 oracle 区分——见下条；它只由 Go 单元测试覆盖 |
| `EQUIVALENT-MUTANT-NOT-CAUGHT` | **G2a（负控制方法论）**：一次定向变异（把空 PATH 分量分支简化为 `filepath.Join(dir, name)`）**未被** oracle 检出，因为它实际是**等价变异**：`filepath.Join("", "tool")` 得到 `"tool"`，而 `execve` 对不含分隔符的相对路径按 cwd 解析（PATH 搜索是 `execvp` 的行为，不是 `execve` 的）。记录它是为了区分"oracle 失效"与"变异等价"——两者都表现为"没被检出"，但处置完全不同 |
| `NATIVE-GATE-BLOCKED-BY-MODEL-ESCALATION` | **G2a（本单元真实阻塞项）**：G2a 后半的真实原生运行停在宿主权限请求上。触发者是模型自己在 `bash` 参数里写 `sandbox_permissions: "danger-full-access"`。同一宿主、同一版本、同一工具策略下，先前那次七个角色的 32 次 `bash`（16 次进入 `workspaces/…`、13 次运行同类检查脚本）**0 次**提权、**0 次**权限请求。**保持未验收** |
| `NATIVE-ANSWER-PATH-ABSENT` | **G2a（既定的能力缺口，非本单元回归）**：本版本**不能**回答宿主的原生权限请求；既有工作的记录是 `permission_bridge: BLOCKED`、`completion: PARTIAL … live permission forwarding not implemented`、`model_calls: 0`。方案 §12.4/§13.1 把该项单列为 **G2d：原生交互**。与 `ZCODE-HEADLESS-BLOCK` 同型 |
| `NATIVE-TRANSPORT-CARRIED-BUT-NO-COMPLETION` | **G2a（后半的精确状态）**：Go 监督器**确实**承载了原生传输（原生入口 argv 被当普通子进程执行、`exit.json` 六字段齐备、流证明正确、监督器是进程组组长），但**没有完成关联**——无角色抵达完成的轮次。§13.1 第 513 行要求的最小证据含"完成关联"，故该半为**受阻**而非通过 |
| `MODEL-ESCALATION-JUSTIFICATION-UNSUPPORTED` | **G2a（写给宿主评审）**：模型为提权给出的理由"只读沙箱在本机没有可用后端"**与证据不符**——先前那次在同一只读沙箱里 13 次运行检查脚本均成功且未提权，更早的 DSH 专项验收还记录过只读会话写被 `Operation not permitted` 拒绝。这不改变处置（harness 仍不得自行授予提权），但说明"模型自述的阻塞原因"也不可直接当作环境事实 |
| `G2B-MISLABELLED-AS-NATIVE-GATE` | **G2a（文档，已修）**：G2a 的记录与本索引都把"验证监督器承载原生传输"写成了"下一单元 G2b 的关口"，而方案 §13.1 定义 **G2b 为 DSH 薄适配**、原生传输是 **G2a 自己的后半**。该错误让一个**属于本单元**的必需关口被顺延到了下一个单元，是"记录写错会改变流程"的实例。已连同记录一并更正 |
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
4. **类型边界上的静默收窄为何反复出现**：C1 的 NaN 预扫描、C3 的 float64 收窄、C4 的整数截断与 `7.0` 误拒、M2 的 `role` 改写与 revision 字符串化、G2a 的 PATH 走查以 `os.Stat` 存在性替代内核可执行性判定——**七处，同一个模式**：Go 侧把"参考实现的动态类型/动态语义"实现成了更窄的静态形状。C4 起补的"经桥接的端到端等价用例 + 负控制"在 M2 确实生效（两处由我在外部评审前查出），G2a 的三处也都由独立 oracle 或完整验收查出，但模式仍在复发，说明**逐例修补不够**。是否需要一条结构性纪律（例如：凡从 JSON 取出的值，禁止在 Go 侧做类型断言式的默认值替换，只能按参考实现的取值语义原样透传；凡"某候选能否运行"这类判定，禁止用预筛替代内核的最终判定），是本阶段最值得质疑的一点。
5. **G1b 的拆分与各项边界**：M1 把"解析+表示规则"入 Go、把"git 调用与文件系统"留在 Python；M2 把两个包装形状入 Go、把 Store 访问与 `BOUNDARY` 文案留在 Python；M3 把内容寻址与两条判定入 Go、把工件路径布局与原子写留在 Python。这三个切法是否合理；`COPY-STATUS-UNREACHABLE` 该按"已由单元测试与独立裁判覆盖"接受，还是应改为结构性消除（例如断言选项集不可能产生 `C`，或让 Go 拒绝未知状态）。
6. **`BOUNDARY` 留在 Python 是否可接受**：它是否算"反馈包装"的一部分（§9.4 的措辞），还是纯提示词文案。若应入 Go，范围是整段提示词而非其中一句。
7. **M3 的"两次调用"是否为正确取舍**：`get` 拆成 `artifact-reference` + `artifact-binding`，使"格式优先于读取失败"成为结构性质，代价是每次读 2 次跨进程（约 11.6 ms，`graph.py` 内单次 run 数十次调用）。被否的两案是"合成一次调用、靠代码顺序保序"与"让 Go 读文件并复现异常语义"。若评审认为该代价不可接受，需要指认一个不会为异常语义写例外的替代。
8. **工件路径为何不入 Go**：`artifact-plan` 返回引用与规范字节、不返回 `artifacts/<ref>.json`。依据是 §7 只规定"正文在工件目录中"、未规定命名。是否应把该命名视为寻址格式的一部分而搬入 Go（即 `ARTIFACT-LOCATION-STAYS-IN-PYTHON` 是否应升级为阻塞项）。
9. **`test_boundary_recovery::test_artifact_integrity` 的前提变更是否可接受**：该用例原先断言"工件读写不需要桥接"，M3 后改用真实 bridge 而断言逐字保留。这是有意的前提变更（合同已登记），但它是**唯一一处**为迁移而改动既有测试前提的地方，值得单独审视。
10. **D1 的处置是否恰当**：交付的差分测试令两侧共用同一工件目录（写路径未被按值差分、两棵树未被互比）。我按缺陷处理并重写，但也可以论证它"没有造成可观察行为偏差"故应算测试质量问题。此外，修复的第二次派发返回空、由我自行实施——是否需要在流程上要求"派发失败必须显式回报"。
11. **G2a 为什么走内部窄命令而不是桥接操作**：理由是"搬动的对象就是进程所有权，与请求/响应的桥接前提冲突"。§12.1 的两条路径因此在本项目里都被使用了，这意味着**"新单元走哪条路径"本身成为一项需要论证的设计决定**。判据是否应写成明文规则（例如：凡需要跨调用存活的状态、或需要接收信号者，走内部窄命令），还是继续逐单元论证。
12. **D3 的处置：修用例、不关闭窗口，是否可接受**：关闭窗口（把 handler 提前到写 `process.json` 之前）会让 Go 在参考实现**没有收据**的地方产出收据，因此我选择保持忠实并改用例。但也可以论证"在该窗口多写一份收据"是对调用方更有利的行为、应作为**有意的行为改进**单独提出（那将是一次显式的语义变更，而非迁移）。若认可后者，它应作为独立单元而不是塞进 G2a。
13. **`childSignalGrace = 250 ms` 这个有界宽限是否该存在**：它是 D2 的修复手段——子进程被信号打死时，先给信号通道 250 ms 确认组信号是否**同样**到达监督器，以区分"取消"与"子进程自己死于 SIGTERM"。它引入了一个时间参数。替代方案是让监督器在信号到达时**无条件**优先报告取消（不设宽限），代价是失去"子进程自杀 vs 被取消"的区分能力。是否应改用不依赖时间常数的结构性做法，值得评审。
14. **"只在完整验收的负载下才复现"是否应升级为流程纪律**：D3 单独跑该包 5 次全过，只在 `go test ./...` 下失败。这提示"单包通过"不足以作为放行依据。是否需要把"竞态类缺陷必须由完整验收复现一次"写成明文要求（本次是碰巧撞上的，不是设计出来的）。
15. **G2a 是否应读作两个验收单元**：这是本次把 G2a 判为"未收口"的核心分歧。本索引按 §13.1 的正文（"G2a **先验命令监督，再验其承载原生传输**"）与第 513 行给"原生定向验收"规定的最小证据（含"**完成关联**"）判定：后半受阻 ⇒ 本单元保持未验收。**另一种读法**是 §13.1 把 G2a 列为**一行**、表内出口条件只有命令作业的三条属性，故后半只由合同第 178 行的结构性证据（原生入口 argv 逐字跑通 + 监督器不读 `kind`）支撑即可，G2a 可收口。若评审采后一读法，需要明确裁定，并说明该裁定与第 513/520 行"涉及进程行为的单元必须增加对应真实调用"如何并存。
16. **模型的提权请求是否应在适配层就转成"能力不足"**：当前实现把 `sandbox_permissions: danger-full-access` 当一次普通原生请求送达上层，由上层报 `DSH_INTERACTION_REQUIRED`（已建立会话、已消耗模型调用）。另一案是在适配层识别该字段并**直接**报能力不足，不建立会话、不消耗调用。两者可观察行为不同，费用也不同。是否要引入这条路径，还是保持"如实转述宿主请求"。
17. **"模型自述的阻塞原因"该占什么证据地位**：本次模型为提权写的理由是"只读沙箱在本机没有可用后端"，而同一沙箱在先前那次运行里正常执行了 13 次同类检查脚本、更早还真的拒绝过只读写入。**理由与证据不符。** 这提示在原生验收里，"模型为什么停下来"的自述既不能当作环境事实，也不能直接当作裁决依据；可核验的只有协议记录、实际文件与进程事实。是否应把这一条写成原生验收的明文纪律（例如：任何"宿主不可用"的结论必须由独立探针复现，不得引用模型自述）。

本索引只汇总各单元记录中已观测的结果；不含密钥或认证材料。
