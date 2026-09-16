# G1a 状态索引（供外部评审）

G1a 是分块替换的第一阶段：**只读计算**。方案要求解码与证据校验**分别验收**。

> **G1a 已完成。** 四个替换单元（C1 结果解码、C2 证据内容摘要原语、C3 规范化与内容摘要、C4 证据校验）各自通过并被固定为下一基线。按 §13.1，下一单元为 **G1b：材料生成**（比较材料、反馈包装、工件发布，分别验收）。

## 基线与验收链

每个替换单元在上一已验收基线上实施，累计组合通过后固定为下一基线。源码提交与验收结论分开提交。

| 单元 | 内容 | 候选提交 | 记录提交 | 结论 |
| --- | --- | --- | --- | --- |
| B0 | 基线冻结与标定（含验收工具、SQLite 锁定） | `41d3375` | `284ceff` | `PASS` |
| C1 | 结果解码 | `20b0422` | `a756cde` | `PASS`（后被独立验收发现回归，见 C2 重签） |
| C2 | 证据内容摘要原语 + C1 回归修复 | `87895e4` | 见 `go-engine-C2.json` | `PASS` |
| C3 | 规范化序列化与内容摘要 | `7492c01` | `6ae01c2` | `PASS`（重签；见下） |
| C4 | 证据校验（G1a 收尾） | `9687945` | 见 `go-engine-C4.json` | `PASS`（重签；见下） |

C3 记录取代了绑定 `04f343d` 的上版记录：该候选存在一处由独立验收发现的 P1 合同回归（大整数在桥接路径上被拒），修复后候选前移到 `7492c01`，原结论不再适用。**C4 记录取代了绑定 `0f1cd2b` 的上版记录**：该候选存在一处由独立验收发现的 P1 数值语义缺陷（证据大小按两种不一致的整数转换规则比较，既误拒绝又误接受），修复后候选前移到 `9687945`。C2 记录同样取代了绑定 `2ed75d4` 的上版记录。分支 `codex/go-engine-v1`，基线起点 `c6d49323`。

## 三次由独立验收发现、我未自查出的回归

| 单元 | 缺陷 | 我的测试为何没发现 |
| --- | --- | --- |
| C1 | 说明中提及 NaN/Infinity 会让合法结果被拒 | 语料偏向"结构变体"而非"语义相邻文本" |
| C3 | 超出 float64 的整数在桥接路径被拒 | 单测直接调编码器，绕过了前置扫描器 |
| C4 | 证据大小按两种整数转换规则比较（`7.0` 误拒、`7.75` 截断误接受） | 两套测试都只写整数大小；`0` vs `0.0` 的用例**两边都设成了 `0.0`** |

三次都在**公开调用链上**成立、而在**直接函数测试**下看不出来。C4 起已把"经桥接的端到端等价用例"作为纪律，并对新增用例补了**负控制**（撤回实现后确认用例确实失败）。

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

## G1a 之后的剩余工作

| 单元 | 内容 | 前置 |
| --- | --- | --- |
| G1b | 比较材料、反馈包装、工件发布（三项分别验收） | 待准入核验 C4 |
| G2a | 命令作业：Go 监督器的直接 argv 路径，再验证它承载原生传输 | G1b 之后 |

仍是宿主编配器生命周期策略、随 G2b/G2c 迁移的：`verify_source` / `validate_binding`。

## 本阶段搬动的四项

| 单元 | 新归属（Go） | 桥接操作 | Python 侧 |
| --- | --- | --- | --- |
| C1 | `internal/harness.FrameResult` | `frame-result` | `native_result.decode_result` 变为适配器 |
| C2 | `internal/harness.FileProof` | `file-proof` | `infra.file_proof` 变为适配器 |
| C3 | `internal/harness.CanonicalJSON` / `CanonicalDigest` | `canonical`、`digest` | `infra.canonical` / `infra.digest` 变为转发 |
| C4 | `internal/harness.VerifyProcess` / `VerifyFiles` | `verify-process`、`verify-files` | `Store.verify_process` / `verify_evidence` 保留路径判断与文件读取，校验委派 |

三处拒绝**不走桥接失败形状**，而走 `value` 里的类型化结果（`{"accepted": false, "refusal": ...}` / `{"ok": false, "refusal": ...}`）。C4 的拒绝另带 `kind`，由 Python `refuse()` 映射回原异常类。失败形状（`{ok:false, code}` + 退出码 2）保留原义，用于"桥接本身坏了"。

## 复核命令

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine

# 准入：读取记录并核验结果、身份、已安装依赖与公开基线冒烟
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-C4.json --next-unit G1b

# 完整验收（新尝试目录，不覆盖历史）
python3 scripts/go-engine/acceptance.py --unit C4 --previous-baseline C3

# 两个工具自身的控制项（需要干净工作树）
sh scripts/go-engine/negative-controls.sh "" .gates/results/go-engine-C4.json
```

| 文件 | 内容 |
| --- | --- |
| `.gates/results/go-engine-B0.md` / `.json` | 基线冻结记录 |
| `.gates/results/go-engine-C1-contract.md` … `C4-contract.md` | 实施前固定的单元合同 |
| `.gates/results/go-engine-C1.md` … `C4.md` 与同名 `.json` | 单元验收记录 |
| `scripts/go-engine/README.md` | 验收与准入工具的约定、判定条件、控制项清单 |
| `scripts/go-engine/dependency-lock.json` | Go/SQLite 驱动与依赖锁定 |

原始证据在记录的 `attempt` 字段所指目录，含 `evidence.json`（逐文件摘要）、各必需项 `summary.json`、每个命令的 stdout/stderr/exit/cmd。每次尝试使用全新目录，已存在即拒绝，**不被后续运行覆盖**。

## 明确未验证的事项

- **真实原生模型流水线**：`NATIVE-REVERIFY-AT-BASELINE` 未执行，需显式授权费用。G1a 两项都是纯计算，**不需要**原生定向验收；授权关口在 G2b 之前。`live-20260915-2050` 的原生证据绑定更早的提交与运行时，不可当作当前候选的原生验收。
- **跨平台**：只在本地 macOS 实测。

## 已知有界差异与风险

| 编号 | 内容 |
| --- | --- |
| `LONE-SURROGATE-ESCAPE` | 文本含转义 `\ud800` 时两侧都接受，但 Python 保留代理项码位、Go 为 U+FFFD |
| `NON-FINITE-CROSSES-TRANSPORT` | 合法 JSON 不允许 `Infinity`，故含非有限浮点的值可经桥接进入编码器、但信封载不动它（C3 起）；不可达——结果路径不会产出非有限值 |
| `INPUT-PATH-NOT-NORMALISED` | C4：参考实现按 `Path` 规范化形式命名缺失的输入文件，Go 用传入的原样字符串；不可达——键由 `str(Path(...))` 写出且工件内容寻址 |
| `BOOL-SIZE-IS-A-NUMBER-IN-PYTHON` | C4：证据 `size` 为布尔值时两侧判定可能不同（Python 中 `True == 1`）；仅手工改写可达，无法由正常写入者产生。经评审定为 P3、非阻塞 |
| `ENV-THROUGHPUT-UNIDENTIFIED` | 独立环境整体吞吐约为本机 1/4，成因未查明；回归判定已改为不依赖吞吐 |
| `BUILD-PATH-IN-RUNTIME-IDENTITY` | `go build` 不加 `-trimpath` 时运行时身份随构建路径变化 |
| `NODE-PATH-AMBIGUOUS` | 两份 node 摘要不同，跨 shell 恢复可能触发 `HOST_CHANGED` |
| `CANONICAL-CALL-CROSSES-PROCESS` / `PROOF-CALL-CROSSES-PROCESS` | 细粒度原语跨进程约 6 ms/次，已测量 |
| `PYTHON-SUITE-FLAKY-UNREPRODUCED` | 上一轮评审看到 1 FAIL + 1 ERROR，本机未复现；该启动链上的 0.53s CLI 导入已移除，其后 6 次全套（C4 期间）全部通过、0 复现 |
| `ZCODE-HEADLESS-BLOCK` | ZCode 无头在原生 Edit 审批处按合同返回 `ZCODE_INTERACTION_REQUIRED` |

## 评审时值得重点看的点

1. **C4 的边界划得对不对**：哪些属证据校验（已入 Go）、哪些属存储层（路径判断与文件读取留在 Python），以及"有序证明列表"是否为最小充分的表达方式。
2. **两处不可达差异的记录方式**：`NON-FINITE-CROSSES-TRANSPORT` 与 `INPUT-PATH-NOT-NORMALISED` 都以"实测差异 + 不可达论证"记录，而非加机制。是否认可这种处置，或应改为结构性消除。
3. **跨进程原语的取舍**：`file_proof` 从 0.036 ms 变为 6.1 ms；C4 未新增高频路径，但校验仍逐文件跨进程。是否应在此阶段就引入批量操作。
4. **回归为何三次都由外部发现**：C1 的 NaN 预扫描、C3 的 float64 收窄、C4 的整数截断与 `7.0` 误拒，都出在公开调用链上、而在直接函数测试下看不出来。C4 已补经桥接的端到端等价用例，并对新增用例执行了**负控制**（撤回实现后确认用例确实失败）。该纪律是否已足够，是本阶段最值得质疑的一点。
5. **G1b 的拆分**：比较材料、反馈包装、工件发布三项的边界与验收顺序。

本索引只汇总各单元记录中已观测的结果；不含密钥或认证材料。
