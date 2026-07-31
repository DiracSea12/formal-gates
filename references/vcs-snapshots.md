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

新执行或重跑的门使用基线到当前。只有继承判定使用修复前紧邻快照到当前。

## SVN

显式添加新路径，把开发或修复提交到其工作分支，并更新到单一版本号：

```bash
svn add -- <path> [<path> ...]
svn commit -m '<message>' <path> [<path> ...]
svn update
```

CLI 用 `svn info --show-item wc-root` 确认工作副本根，并用
`svnversion <working-copy-root>` 要求整个工作副本处于单一数字版本号。混合版本、
已修改、已切换或不完整的结果都不是不可变的整工作区标识，会中止该状态转移。它用
`svn info --show-item revision -r <revision>` 验证版本号。记录快照之前，
`svn status --quiet <working-copy-root>` 必须报告没有受版本控制的改动。审查者比
较同一分支或仓库 URL：

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
