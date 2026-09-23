# M1 验收记录：比较材料（G1b 第一项）

## 结论

替换单元 M1 通过，被固定为下一基线。

| 检查 | 结果 |
| --- | --- |
| 本块合同（Go 单元测试 `comparison_test.go`） | 通过：8 个测试函数，覆盖切分、重命名、拷贝、两侧缺失、gitlink、空集合、Unicode、非法记录 |
| 本块合同（差分对照） | 通过：5 项，材料文件集合、材料字节、`comparison.json` 字节全部与迁移前实现一致 |
| 独立裁判比对（我自己构造，超出子代理用例） | 通过：17 个样本（含生产路径**产不出**的 `C` 拷贝记录）逐字节一致；2 个畸形输入两侧均拒绝 |
| **负控制** | 通过：5 处定向变异**全部被检出**；变异后源码逐字节还原 |
| 累计组合完整流程（fixture） | 实际达到 `DEMO_COMPLETE` |
| 累计自动化回归（Go 无缓存全量） | 通过：23 包执行、3 包无测试文件、**2001 用例通过事件、0 失败、`stall=None`** |
| 累计自动化回归（Python 全套） | 通过，**142 项**（连续两遍） |
| SQLite 探针 | 通过，CGO 开/关各 8/8 |
| DSH / ZCode `doctor` | 通过，接入探测 |
| 原生定向验收 | **不适用**：本单元为纯规则计算 + Python 侧真实 git 调用 |
| **总体** | **`PASS`** |

上一验收基线：`C4`（候选 `9687945`，记录提交 `dd93b9f`）。

## 本单元搬了什么

`feedback.materialize_comparison` 的**解析与表示规则**搬进 Go，经桥接操作 `comparison-plan` 暴露。合同依据是方案 §9.3。

| 归属 | 内容 |
| --- | --- |
| **Go（新）** | 把 `git diff --raw --no-abbrev -z` 的原始记录解析为变更清单；决定每一侧 `path`/`mode`/`object`/`material`；产出 `comparison.json` 的规范字节 |
| **Python（不变）** | 调用 git、读写文件与目录、`save()` 的存在性与篡改检查、`file_proof` 证明收集 |

边界与 C2/C4 已确立的一致：**Go 持有规则与格式，Python 持有进程与文件系统**。`materialize_comparison` 的签名、材料保存顺序（`full.diff` → 逐个材料 → `comparison.json`）、返回形状全部未变，`workers.Workers.run` 构造 `input_files` 的方式一字未动。不新增拒绝家族。

搬动的 8 条规则（逐条在差分对照与 Go 单元测试中各有用例）：

| 规则 | 内容 |
| --- | --- |
| 记录切分 | 以 NUL 切分，尾随空段丢弃 |
| 头部字段 | `:oldmode newmode oldsha newsha status`，按空白切分，`oldmode` 去前导 `:` |
| 路径条数 | `status[0]` 为 `R` 或 `C` 时读两条路径，其余一条、两侧同名 |
| 侧缺失 | mode `000000` → 该侧为 `null` |
| Gitlink | mode `160000` → 只记身份并附 `Gitlink only; nested repository content is not included.`，**不产生材料** |
| 材料名 | 普通 blob 为 `<side>/<path>` |
| 符号链接 | mode `120000` 按普通 blob 处理，材料内容是**目标文本**而非跟随链接 |
| 空集合 | 空 `raw` → `changes` 为空、`index` 仍为合法规范编码 |

**每次 `materialize_comparison` 少一次 `canonical` 跨进程调用**（原「逐材料一次 `cat-file` + 一次 `canonical`」变为「一次 `comparison-plan`」），净增一次桥接往返。

## 独立复核：我自己构造的裁判比对

子代理的差分用例基于真实 git 仓库，而**生产选项集只有 `--find-renames`，产不出 `C`／拷贝记录**。为覆盖这条分支，我另建了一个裁判脚本：把 `reference_comparison.py` 里的解析循环**逐字**取出当作 oracle，喂给它与桥接同一份 `raw`，比对 `changes` 与 `comparison.json` 字节。

| 样本 | 结果 |
| --- | --- |
| `C100` 拷贝、`C75` 新侧、`R087` 重命名、`R100`、多记录混合 | 逐字节一致 |
| gitlink 新增／删除／修改、mode `100644`→`100755`、`120000`→`100644` | 逐字节一致 |
| Unicode 重命名、Unicode 深路径、路径含空格、路径含制表符 | 逐字节一致 |
| 空 `raw` | 逐字节一致 |
| 手工构造的畸形输入（单个 NUL、缺尾随 NUL） | 两侧**均拒绝**，无静默放行 |

合计 17 个样本，**0 分歧**。

## 负控制：5 处定向变异，全部被检出

把变异施加在**出货实现**上，再跑本单元用例；变异未被检出即说明对应用例是空转的。跑完逐字节还原（`shasum` 校验）。

| 变异 | Go 用例 | 差分对照 | 说明 |
| --- | --- | --- | --- |
| 保留尾随空记录（撤回切分规则） | 失败 | 失败 | 两侧都检出 |
| 只有 `R` 读两条路径（丢掉 `C`） | 失败 | **通过** | 见下：差分语料无法产生 `C` |
| 材料名一律用 `base/` 前缀 | 失败 | 失败 | 两侧都检出 |
| gitlink 也导出材料 | 失败 | 失败 | 两侧都检出 |
| Python 侧改用非规范编码写 `comparison.json` | — | 失败 | Python 侧变异由差分层检出 |

第二行是**真实覆盖缺口**而非脚本瑕疵：生产只传 `--find-renames`，`C` 记录不可达，因此差分用例**不可能**检出该分支。该分支现由 Go 单元测试与我上面的独立裁判共同覆盖。已作为覆盖限制记入索引（`COPY-STATUS-UNREACHABLE`）。

第五行的 Go 一列显示通过属预期：该变异不触及 Go 源码。

## 过程问题：一次工作目录误判

本单元开工时，我按上一会话的上下文直接在 `/Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-dogfood-dsh` 侦察，那里的 HEAD 是 `codex/dogfood-dsh`／`c6d4932`、工作树干净、且**找不到任何 M1 产物**——看起来像工作丢失。

实际是本项目以**独立 worktree** 开发：`git worktree list` 显示 `/Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine` 绑定 `codex/go-engine-v1`，M1 的 7 个文件与契约都在那里、状态与会话记录一致。**没有丢失任何工作**，是我初始侦察方向错了。

留作纪律：本项目的「当前工作区」应以 `git worktree list` 中绑定 `codex/go-engine-v1` 的目录为准，而不是会话的默认工作目录。

## 候选身份

```text
工作树   /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine
分支     codex/go-engine-v1
提交     f51d1ace6f1e67942a3f53c7ca3dd9f8419cffb8
上一基线 9687945080bbad616fa00766896fe149a4ed6d0e（C4）
运行时身份 e2706673438ccef6603cd8a755c7879d933411fffb236c50a2d23a2ed85e161c
驱动二进制 2a45389f8b3f645bedd4693b49612ae182df0e37a7ea373a15cf60673fd5dbfe
证据清单 64 个文件
通过尝试 20260917T014058Z-87754
```

驱动二进制由候选源码重建后与已安装副本摘要一致（可复现），已单独核验，未使用陈旧二进制。

## 累计回归的实际结果

| 项 | 耗时 | 说明 |
| --- | --- | --- |
| `identity` | 0.9s | 解释器、已安装依赖、源码与二进制摘要 |
| `go-vet` | 0.4s | |
| `go-test-full` | 385.9s | 23 包执行、3 包无测试文件、2001 用例通过事件、0 失败、`stall=None`；最慢 `internal/validate` 378.6s、`internal/cli` 81.3s |
| `sqlite-probe` | 2.8s | CGO 开/关各 8/8 |
| `python-unittest` | 51.6s | 142 项 |
| `demo-fixture` | 3.1s | 达 `DEMO_COMPLETE` |
| `doctor-dsh` / `doctor-zcode` | 2.4s / 2.9s | 接入探测 |
| 合计 | 448s | |

相对 C4：Go 用例通过事件 `1993 → 2001`（+8，即本单元新增的 8 个 Go 用例），Python 用例 `137 → 142`（+5，即本单元新增的差分模块）。**两处增量都与本单元新增用例数精确对应，没有被改写的既有用例。**

M1 只跑了**一个**尝试（`20260917T014058Z-87754`），`overall=PASS`、`failed=[]`。

## 未解决问题

| 编号 | 状态 |
| --- | --- |
| `COPY-STATUS-UNREACHABLE` | **新增（覆盖限制）**：`C`／拷贝记录在生产选项集下不可达，差分语料无法覆盖该分支；由 Go 单元测试与独立裁判覆盖 |
| `MALFORMED-RECORD-ERROR-SHAPE` | **新增（P3，不可达）**：畸形记录在迁移前是**未捕获异常**（`ValueError`／`StopIteration`），现在返回受控的桥接错误。git 的正常输出不会产生该输入 |
| `CANONICAL-CALL-CROSSES-PROCESS` / `PROOF-CALL-CROSSES-PROCESS` | 保留（6 ms 量级）。本单元**减少**一次 `canonical` 跨进程调用 |
| `PYTHON-SUITE-FLAKY-UNREPRODUCED` | 保留：本单元全套 3 次（2 次手工 + 1 次验收内）全过，仍未复现 |
| `BOOL-SIZE-IS-A-NUMBER-IN-PYTHON`、`ENV-THROUGHPUT-UNIDENTIFIED`、`BUILD-PATH-IN-RUNTIME-IDENTITY`、`NODE-PATH-AMBIGUOUS`、`ZCODE-HEADLESS-BLOCK`、`LONE-SURROGATE-ESCAPE`、`NON-FINITE-CROSSES-TRANSPORT`、`INPUT-PATH-NOT-NORMALISED` | 不变 |
| G1b | **进行中**。本单元为第一项；按 §13.1 余下 **M2 反馈包装**、**M3 工件发布**，分别验收 |

## 回退入口

```sh
git -C /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine \
  reset --hard 9687945080bbad616fa00766896fe149a4ed6d0e
```

与 C4 记录同一约定：指向上一基线的**候选**提交。若需同时保留 C4 的验收记录，改用其记录提交 `dd93b9f`（同树，多一份记录文件）。

## 复核入口

```sh
cd /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine

# 准入：核验本记录并放行 M2
python3 scripts/go-engine/admission.py --record .gates/results/go-engine-M1.json --next-unit M2

# 本块合同
go test ./internal/harness -run 'TestPlanComparison' -v

# 差分对照（Go 与迁移前实现逐字节一致）
PYTHONDONTWRITEBYTECODE=1 PYTHONPATH="$PWD/runtime/langgraph" \
  .harness/venv/bin/python -m unittest discover -s runtime/langgraph/tests \
  -p 'test_comparison_material_differential.py' -v

# 桥接操作直查
printf '%s' '{"operation":"comparison-plan","payload":{"base":"B","candidate":"C","diff":"full.diff","raw":":100644 100644 0000000000000000000000000000000000000000 1111111111111111111111111111111111111111 M\u0000a.txt\u0000"}}' \
  | .harness/bin/formal-gates harness bridge
```

单元合同：`.gates/results/go-engine-M1-contract.md`。
本记录只汇总已观测结果；日志保留原始失败，不以最终通过覆盖中间尝试。
