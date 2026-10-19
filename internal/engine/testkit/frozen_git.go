package testkit

import (
	"fmt"
	"strconv"
	"strings"
)

// FrozenSnapshot 是冻结快照夹具身份：40 位 hex，单链冻结仓的 base。
const FrozenSnapshot = "0123456789abcdef0123456789abcdef01234567"

// FrozenTree 是 FrozenSnapshot 所指向提交的树身份（base 内容的冻结摘要）。
const FrozenTree = "0123456789abcdef0123456789abcdef01234568"

// FrozenGitRepo 是冻结场景的 Git 应答夹具：单链提交仓（链首即 base），按命令
// 与参数精确区分身份——rev-parse HEAD 是当前提交，<rev>^{tree} 是该提交的树
// 身份，merge-base 返回链上较早实参，区间计数＝链上距离（base==HEAD 如实为
// 0）。候选冻结生成独立提交，目标 HEAD 只随原生 merge/commit/reset 推进。
// 未推进时应答与单提交仓一致。真实 git 行为（含排除
// 规则与 squash）由 internal/engine/vcs 的真仓冒烟覆盖。
type FrozenGitRepo struct {
	commits []string
	head    string
	trees   map[string]string
	objects map[string]string
	// sequence 是身份发生的单调计数：squash 截断链后仍不复用已出现的提交
	// 身份（悬空提交与新提交是不同对象）。
	sequence int
	// pendingTree 是 reset --soft 后待重提交保留的树身份（squash 保持最终
	// 树不变）。
	pendingTree string
}

func NewFrozenGitRepo() *FrozenGitRepo {
	repo := &FrozenGitRepo{trees: map[string]string{}, objects: map[string]string{FrozenTree: "abcdef0123456789abcdef0123456789abcdef0123"}}
	repo.commits = append(repo.commits, FrozenSnapshot)
	repo.head = FrozenSnapshot
	repo.trees[FrozenSnapshot] = FrozenTree
	return repo
}

// Advance 提交一次内容变化：新提交与其树身份唯一入链并成为当前 HEAD，返回
// 新提交身份。
func (r *FrozenGitRepo) Advance() string {
	r.sequence++
	commit := fmt.Sprintf("%040d", r.sequence)
	r.commits = append(r.commits, commit)
	r.head = commit
	r.trees[commit] = fmt.Sprintf("%064d", r.sequence)
	r.objects[r.trees[commit]] = fmt.Sprintf("%040x", r.sequence+1000000)
	return commit
}

// CommitDetached 提交一次候选内容变化而不移动目标 HEAD：候选提交入链（对
// diff/树摘要可见），HEAD 保持原提交。
func (r *FrozenGitRepo) CommitDetached() string {
	target := r.head
	commit := r.Advance()
	r.head = target
	return commit
}

func (r *FrozenGitRepo) indexOf(rev string) (int, bool) {
	for i, commit := range r.commits {
		if strings.EqualFold(commit, rev) {
			return i, true
		}
	}
	return 0, false
}

func (r *FrozenGitRepo) Run(dir, command string, args ...string) (string, error) {
	joined := strings.Join(args, " ")
	if command != "git" {
		return "", fmt.Errorf("frozen git fixture: unexpected command %s %s", command, joined)
	}
	head := r.head
	switch {
	case joined == "rev-parse --show-toplevel":
		return dir, nil
	case joined == "rev-parse HEAD":
		return head, nil
	case len(args) == 3 && args[0] == "rev-parse" && args[1] == "--verify" && strings.HasSuffix(args[2], "^{commit}"):
		rev := strings.TrimSuffix(args[2], "^{commit}")
		if _, ok := r.indexOf(rev); ok {
			return rev, nil
		}
		return "", fmt.Errorf("frozen git fixture: unknown commit %s", rev)
	case len(args) == 2 && args[0] == "rev-parse" && strings.HasSuffix(args[1], "^{tree}"):
		if tree, ok := r.trees[strings.ToLower(strings.TrimSuffix(args[1], "^{tree}"))]; ok {
			return tree, nil
		}
		return "", fmt.Errorf("frozen git fixture: unknown commit %s", args[1])
	case len(args) == 3 && args[0] == "merge-base":
		a, aok := r.indexOf(args[1])
		b, bok := r.indexOf(args[2])
		if !aok || !bok {
			return "", fmt.Errorf("frozen git fixture: unknown commit in %s", joined)
		}
		if a <= b {
			return r.commits[a], nil
		}
		return r.commits[b], nil
	case joined == "status --porcelain=v1 --untracked-files=all":
		if r.pendingTree != "" && r.pendingTree != r.trees[head] {
			return "M  app.go", nil
		}
		return "", nil
	case joined == "status --porcelain=v1 --branch --untracked-files=all":
		if r.pendingTree != "" && r.pendingTree != r.trees[head] {
			return "## HEAD (no branch)\nM  app.go", nil
		}
		return "## HEAD (no branch)", nil
	case len(args) == 8 && strings.Join(args[:5], " ") == "diff --raw --no-abbrev --no-renames -z" && args[7] == "--":
		object := func(identity string) (string, bool) {
			if tree, ok := r.trees[identity]; ok {
				identity = tree
			}
			entry, ok := r.objects[identity]
			return entry, ok
		}
		base, baseExists := object(args[5])
		next, nextExists := object(args[6])
		if !baseExists || !nextExists {
			return "", fmt.Errorf("frozen git fixture: unknown diff endpoint %v", args)
		}
		if base == next {
			return "", nil
		}
		return ":100644 100644 " + base + " " + next + " M\x00app.go\x00", nil
	case joined == "write-tree":
		// 暂存区树：reset --soft 后 index 保留截链前内容（pendingTree）；干净
		// index 的暂存树＝HEAD 提交的树。
		if r.pendingTree != "" {
			return r.pendingTree, nil
		}
		return r.trees[head], nil
	case len(args) == 3 && args[0] == "merge" && args[1] == "--ff-only":
		if _, ok := r.indexOf(args[2]); !ok {
			return "", fmt.Errorf("frozen git fixture: unknown merge source %s", args[2])
		}
		r.head = args[2]
		return "", nil
	case len(args) == 3 && args[0] == "reset" && args[1] == "--soft":
		// squash 第一步：区间压回 base，当前树留给重提交。
		bi, ok := r.indexOf(args[2])
		if !ok {
			return "", fmt.Errorf("frozen git fixture: unknown commit %s", args[2])
		}
		r.pendingTree = r.trees[r.head]
		r.head = args[2]
		r.commits = r.commits[:bi+1]
		return "", nil
	case len(args) == 3 && args[0] == "commit" && args[1] == "-m":
		// squash 第二步：以保留树重新提交——内容不变、身份更新。
		r.sequence++
		commit := fmt.Sprintf("%040d", r.sequence)
		tree := r.pendingTree
		if tree == "" {
			tree = fmt.Sprintf("%064d", r.sequence)
			r.objects[tree] = fmt.Sprintf("%040x", r.sequence+1000000)
		}
		r.pendingTree = ""
		r.commits = append(r.commits, commit)
		r.trees[commit] = tree
		r.head = commit
		return "", nil
	case len(args) == 3 && args[0] == "rev-list" && args[1] == "--count":
		base, tip, ok := strings.Cut(args[2], "..")
		if !ok {
			return "", fmt.Errorf("frozen git fixture: unexpected range %s", args[2])
		}
		bi, bok := r.indexOf(base)
		hi, hok := r.indexOf(tip)
		if !bok || !hok || hi < bi {
			return "", fmt.Errorf("frozen git fixture: not an ancestor range %s", args[2])
		}
		return strconv.Itoa(hi - bi), nil
	}
	return "", fmt.Errorf("frozen git fixture: unexpected command git %s", joined)
}

func (r *FrozenGitRepo) RunRaw(dir, command string, input []byte, args ...string) ([]byte, error) {
	out, err := r.Run(dir, command, args...)
	return []byte(out), err
}
