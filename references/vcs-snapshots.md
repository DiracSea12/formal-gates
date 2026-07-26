# Native VCS Snapshots

formal-gates stores native immutable identities, never diff bytes. The CLI uses
the VCS selected at run start to resolve and verify the base, current,
pre-repair, dispatch, and Seal identities. Snapshot flags are not transcribed on
later commands.

Every delivery path must be tracked before fixing a snapshot. Add only explicit
paths; never add the whole worktree or unrelated untracked files.

## Git

Add new or previously untracked delivery paths before continuing, then commit
the complete development or repair:

```bash
git add -- <path> [<path> ...]
git commit -m '<message>'
```

The CLI confirms the repository root with `git rev-parse --show-toplevel`,
resolves the current identity with `git rev-parse HEAD`, and verifies a recorded
commit with `git rev-parse --verify '<identity>^{commit}'`. Before recording a
snapshot, `git status --porcelain=v1 --untracked-files=no` must report no
tracked changes.

Reviewers use the recorded commit identities directly:

```bash
git diff --stat <base-commit> <current-commit> --
git diff --binary <base-commit> <current-commit> --
```

Use base-to-current for a fresh or rerun gate. Use immediate
pre-repair-to-current only for Carry.

## SVN

Add new paths explicitly, commit the development or repair to its working
branch, and update to one revision:

```bash
svn add -- <path> [<path> ...]
svn commit -m '<message>' <path> [<path> ...]
svn update
```

The CLI confirms the working-copy root with `svn info --show-item wc-root`,
resolves the numeric revision with `svn info --show-item revision`, and verifies
it with `svn info --show-item revision -r <revision>`. Before recording a
snapshot, `svn status --quiet <working-copy-root>` must report no versioned
changes. Reviewers compare the same branch or repository URL:

```bash
svn diff --notice-ancestry -r <base-revision>:<current-revision> <working-copy-or-url>
```

## P4

Open new delivery files explicitly and submit the development or repair:

```bash
p4 add -c <change> <path> [<path> ...]
p4 reopen -c <change> <path> [<path> ...]
p4 submit -c <change>
p4 sync
```

The CLI confirms the client root from tagged `p4 info`, resolves the current
submitted changelist from `p4 changes -m 1 ...#have`, and verifies a recorded
number within the client path using `p4 changes -m 1 ...@<change>`. Before
recording a snapshot, `p4 opened ...` must report no files opened on the client.
Reviewers compare depot state with native `diff2`:

```bash
p4 diff2 -Od //depot/path/...@<base-change> //depot/path/...@<current-change>
```

## Unsupported VCS

Git, SVN, and P4 are the supported resolvers. Any other VCS, an unavailable
native command, or an identity the selected VCS cannot reproduce stops the
workflow transition without changing semantic state. Do not copy files, guess
an identity, silently switch VCS, or implement a fallback snapshot engine.
