# Native VCS Snapshots

formal-gates stores only caller-supplied snapshot identities. The worker and
reviewers run the named VCS directly. Use the same native method for the base,
the completed development snapshot, and every post-repair snapshot.

Every delivery path must be tracked before fixing a snapshot. Add only explicit
paths; never add the whole worktree or unrelated untracked files.

## Git

Add new or previously untracked delivery paths before continuing:

```bash
git add -- <path> [<path> ...]
```

One non-destructive way to identify the complete tracked worktree is the tree
of a native stash object. `git stash create` does not modify the worktree:

```bash
snapshot_commit=$(git stash create)
if test -n "$snapshot_commit"; then
  git rev-parse "$snapshot_commit^{tree}"
else
  git rev-parse 'HEAD^{tree}'
fi
```

Record the returned tree object ID. Given two recorded tree IDs:

```bash
git diff --stat <base-tree> <current-tree> --
git diff --binary <base-tree> <current-tree> --
```

Use base-to-current for a fresh or rerun gate. Use immediate
pre-repair-to-current for Carry. Recompute the live tree ID immediately before
and after seal; it must equal the run's current identity.

This method includes tracked staged and unstaged changes. It includes a new
file only after that explicit path has been added to Git. Unrelated untracked
files remain outside the comparison.

## SVN

SVN's normal immutable identity is a committed repository revision. Add new
delivery paths explicitly, commit the development or repair to its working
branch, and update to one revision:

```bash
svn add -- <path> [<path> ...]
svn commit -m '<message>' <path> [<path> ...]
svn update
svnversion .
```

Use a single clean numeric revision as the snapshot identity. A mixed,
modified, or switched `svnversion` result is not an immutable current snapshot.
Compare the same branch or repository URL at the recorded revisions:

```bash
svn diff --notice-ancestry -r <base-revision>:<current-revision> <working-copy-or-url>
```

For Carry, replace the base revision with the immediate pre-repair revision.

## P4

Open new delivery files explicitly and keep the work in a dedicated changelist:

```bash
p4 add -c <change> <path> [<path> ...]
p4 reopen -c <change> <path> [<path> ...]
```

Use submitted changelist numbers as the simple immutable path. Submit the
development or repair changelist, sync the client, and record the returned
submitted number:

```bash
p4 submit -c <change>
p4 sync
```

Record the submitted changelist number printed by `p4 submit`. Compare depot
state at two submitted changelists with native `diff2`:

```bash
p4 diff2 -Od //depot/path/...@<base-change> //depot/path/...@<current-change>
```

Use immediate pre-repair-to-current for Carry. If project policy does not allow
an immutable submitted or otherwise natively comparable P4 snapshot, stop the
formal run instead of copying files or implementing a custom fallback.

## Other VCS

Another VCS is acceptable only when it can provide stable snapshot identities,
reproduce both comparisons, include every explicit delivery path, and verify
that live state still matches the recorded current identity. Otherwise the
action is `RUNTIME_ERROR` and seal is blocked. No-VCS mode is unsupported.
