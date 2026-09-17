# M1 替换单元合同：比较材料（G1b 第一项）

基线为 C4（候选 `9687945`，记录提交 `dd93b9f`，准入已放行 G1b）。G1b 按 §13.1 分三项：**比较材料（本单元 M1）**、反馈包装（M2）、工件发布（M3）。

本单元把 `feedback.materialize_comparison` 的**材料表示规则**搬进 Go。合同依据是方案 §9.3「只读审查材料」。

## 职责边界

**做**：解析 `git diff --raw --no-abbrev -z` 的原始记录为变更清单；决定每个变更两侧的表示（路径、mode、object、材料名、gitlink 备注）；产出规范序列化的 `comparison.json` 字节。经桥接操作 `comparison-plan` 暴露。

**不做**：不运行 git、不读不写任何文件、不做存在性判断。这些仍由 Python 负责（与 C2/C4 已确立的边界一致：**Go 持有规则与格式，Python 持有进程与文件系统**）。不新增拒绝家族；不改变 `materialize_comparison` 的返回形状。

## 输入与输出

| 项 | 内容 |
| --- | --- |
| 输入 | `base`：基线对象名；`candidate`：候选对象名；`diff`：完整差异的材料名（`full.diff`）；`raw`：`git diff --raw --no-abbrev -z` 的原始输出，作为字符串传入 |
| 输出 | `{changes: [...], index: <字节>}` |
| `changes[i]` | `{status, base: <侧或 null>, candidate: <侧或 null>}`，**按原始记录顺序** |
| 侧对象 | `{path, mode, object}`；普通 blob 另带 `material: "<side>/<path>"`；mode `160000` 另带 `note`；mode `000000` 时该侧为 `null` |
| `index` | `canonical({base, candidate, diff, changes})` 的字节，即 `comparison.json` 的内容 |

桥接操作：

| 操作 | 载荷 | 返回 |
| --- | --- | --- |
| `comparison-plan` | `{"base","candidate","diff","raw"}` | `{"changes":[...], "index": <字符串字节>}` |

## 必须逐字保持的规则（由 Python 现状实测确定）

| 规则 | 内容 |
| --- | --- |
| 记录切分 | 以 NUL 切分；最后一段（尾随 NUL 之后）丢弃 |
| 头部字段 | `:oldmode newmode oldsha newsha status`，按空白切分；`oldmode` 去掉前导 `:` |
| 路径条数 | `status[0]` 为 `R` 或 `C` 时读两条路径（旧、新），其余读一条，两侧同名 |
| 侧缺失 | mode `000000` → 该侧为 `null`（新增的 base 侧、删除的 candidate 侧） |
| Gitlink | mode `160000` → 只记 `{path, mode, object}` + `note`，**不产生材料**；note 文本逐字为 `Gitlink only; nested repository content is not included.` |
| 材料名 | 普通 blob 为 `<side>/<path>`，`side` ∈ {`base`, `candidate`} |
| 符号链接 | mode `120000` 按普通 blob 处理：材料内容是**目标文本**，不跟随链接 |
| 二进制 | 不受本单元影响；`full.diff` 由 Python 以 `--binary --full-index` 生成 |
| 空集合 | 空 `raw` → `changes` 为空列表、`index` 仍为合法规范编码 |
| Unicode | 路径按 UTF-8 解码，与 Python 现状一致（`-z` 不引用路径） |

## 受影响调用方

- `feedback.materialize_comparison`：保留签名与返回形状；改为调用 `comparison-plan` 取回 `changes` 与 `index` 字节，随后按计划逐个取 blob、写材料并收集证明。
- `workers.Workers.run`（`complexity-gate` / `implementation-quality-gate` 角色）：经 `materialize_comparison` 的返回值构造 `input_files`，形状不变。
- 差分对照测试 `test_comparison_material_differential.py` 与 Go 合同测试。

## 不变量

1. **变更清单与材料名逐字一致**：同一 `raw` 下，Go 的 `changes` 与迁移前实现完全相同（含顺序、`null` 侧、gitlink 备注）。
2. **`index` 字节一致**：即 `canonical({base, candidate, diff, changes})`，与迁移前的 `canonical` 输出逐字节相同。
3. 材料**不静默截断**：每个非 `000000`、非 `160000` 的侧都必须有材料，且内容是 `git cat-file blob <object>` 的原始字节。
4. 重试语义不变：`materialize_comparison` 重复调用返回相同结果；已绑定内容被改动时仍以 `RuntimeBlocked` 拒绝，措辞不变（`Saved comparison material changed: <name>`）。
5. 不改变 git 调用方式、材料目录布局与证明收集顺序。

## 必需用例

| 用例 | 判据 |
| --- | --- |
| 修改文件 | 两侧 path 相同、mode/object 不同、各有材料 |
| 新增文件 | base 侧 `null`、candidate 侧有材料 |
| 删除文件 | candidate 侧 `null`、base 侧有材料 |
| 重命名 | `status` 以 `R` 开头、两侧 path 不同（`rename.txt` → `renamed.txt`） |
| 拷贝 | `status` 以 `C` 开头、两侧 path 不同 |
| 二进制内容 | 两侧材料字节含 `\x00`/非 UTF-8，与源一致；`full.diff` 含 `GIT binary patch` |
| 符号链接 | mode `120000`，材料内容是目标文本而非链接 |
| Gitlink | mode `160000`、有 `note`、该侧**无** `material` |
| 空变更 | 同一对象 diff 自身 → `changes` 空、`index` 可解析 |
| Unicode 路径 | 非 ASCII 路径两侧可用、材料内容正确 |
| 逐字节等价 | 上述全部样本：Go 的 `changes` 与 `index` 与参考实现一致 |
| 重试与篡改 | 重复调用结果相同；改动材料后 `RuntimeBlocked`，措辞不变 |
| Go 合同 | `comparison_test.go` 覆盖切分、重命名、两侧缺失、gitlink、空集合 |

样本量：差分对照 8 个用例（覆盖上表全部形态）+ Go 合同测试；语料含 Unicode、空集合与拒绝项（§12.2）。

## 适用宿主

纯计算 + Python 侧真实 git 调用；不涉及宿主协议、权限或工具继承。需要工作区可运行 `git`（既有依赖）。

## 已测量的开销

`comparison-plan` 为跨进程调用，与同类原语同量级（约 6 ms/次）。每次 `materialize_comparison` 由「逐材料一次 `cat-file` + 一次 `canonical`」变为「一次 `comparison-plan`」，**不再单独调用 `canonical`**，净增一次桥接往返。

## 回退入口

```sh
git -C /Users/suxinyang/Documents/Codex/2026-06-28/nin/work/formal-gates-go-engine \
  reset --hard 9687945080bbad616fa00766896fe149a4ed6d0e
```

## 验收出口

- 8 个差分对照用例全部通过（Go 与参考实现逐字一致）。
- Go 侧 `comparison_test.go` 通过；`go vet ./internal/harness/` 与 `gofmt -l internal/harness/` 干净。
- 累计组合完整流程达 `DEMO_COMPLETE`；完整 Python 套件稳定通过（连续两遍）。
- 新增用例附**负控制**（撤回实现后确认用例失败）。
- 基线、原始证据与回退入口固定。
