# 原生 VCS 快照

formal-gates 只保存原生不可变标识，从不保存 diff 字节。CLI 使用 run 启动时选定
的 VCS，来解析并验证基线、当前、修复前、派发和 Seal 各个标识。快照参数不会在后
续命令上重复传递。

固定快照之前，每条交付路径都必须已被跟踪。只添加显式路径；绝不添加整个工作区或
无关的未跟踪文件。

## Git

继续之前先添加新的或此前未跟踪的交付路径，然后提交完整的开发或修复：

```bash
git add -- <path> [<path> ...]
git commit -m '<message>'
```

CLI 用 `git rev-parse --show-toplevel` 确认仓库根，用 `git rev-parse HEAD` 解析
当前标识，并用 `git rev-parse --verify '<identity>^{commit}'` 验证已记录的提交。
记录快照之前，`git status --porcelain=v1 --untracked-files=no` 必须报告没有已跟
踪改动。

审查者直接使用已记录的提交标识：

```bash
git diff --stat <base-commit> <current-commit> --
git diff --binary <base-commit> <current-commit> --
```

新执行或重跑的门一律使用完整的基线到当前交付——返修重跑绝不只审返修增量；审查者必须
在结果契约里报告它实际比较的快照对（`compared`），报告与指定范围不匹配的结果被丢弃。
只有继承判定使用修复前紧邻快照到当前，并在提示词里与门重跑范围明确区分。

Seal 时若基线→当前含 >1 条提交，git run 会把该范围压缩为单条提交（`git reset --soft
<base>` + 以 `--squash-message` 重新提交，保留最终树），作为 seal 的最后一步 VCS 操
作；压缩前工作树必须干净。压缩提交是新的最终 candidate，CLI 保留旧 QA/门结果的原始
快照绑定并停止本次 Seal；全部选中 QA mode 与门在最终 candidate 上重验后，再次 Seal 才
完成。单条提交或空范围不操作；SVN/P4 不压缩。压缩后的中间提交成为 dangling（接受此审
计性影响），durable 审计证据绑定最终压缩提交及其重验结果。

## SVN

显式添加新路径，把开发或修复提交到其工作分支，并更新到单一版本号：

```bash
svn add -- <path> [<path> ...]
svn commit -m '<message>' <path> [<path> ...]
svn update
```

CLI 用 `svn info --show-item wc-root` 确认工作副本根，并用
`svnversion <working-copy-root>` 取其 **BASE 版本级**作为不可变标识：工作树修改
（svnversion 的 M 已修改 / S 已切换 / P 稀疏后缀）不改变 BASE 版本、不影响身份校验
（QA 隔离工作区注入当前需求文档即属此类）；混合版本范围（如 `2:3`）仍是非均匀工作
区、会中止该状态转移。它用 `svn info --show-item revision -r <revision>` 验证版本
号。记录快照之前，`svn status --quiet <working-copy-root>` 必须报告没有受版本控制
的改动。审查者比较同一分支或仓库 URL：

```bash
svn diff --notice-ancestry -r <base-revision>:<current-revision> <working-copy-or-url>
```

## P4

显式 open 新的交付文件，并提交开发或修复：

```bash
p4 add -c <change> <path> [<path> ...]
p4 reopen -c <change> <path> [<path> ...]
p4 submit -c <change>
p4 sync
```

CLI 从带标签的 `p4 info` 确认 client 根，用 `p4 changes -m 1 ...#have` 获取候选
的已提交 changelist，并要求带标签的 `p4 sync -n ...@<change>` 预览报告没有文件。
这证明整个 client 视图与该 changelist 一致，而不是把混合的 `#have` 状态坍缩为它
的最新变更。它用 `p4 changes -m 1 ...@<change>` 在 client 路径内验证已记录的编
号。记录快照之前，`p4 opened ...` 必须报告 client 上没有处于 open 状态的文件。
审查者用原生 `diff2` 比较 depot 状态：

```bash
p4 diff2 -Od //depot/path/...@<base-change> //depot/path/...@<current-change>
```

## 不受支持的 VCS

Git、SVN 和 P4 是受支持的解析器。任何其他 VCS、不可用的原生命令，或选定 VCS 无
法复现的标识，都会在不改变语义状态的前提下中止流程转移。不要复制文件、不要猜测
标识、不要静默切换 VCS，也不要实现回退快照引擎。
