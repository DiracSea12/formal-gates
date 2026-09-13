# B8 runtime increment

The adopted runtime changes share action planning in `runtime_input.go` and
material capture in `action_material_files.go`. Review and verification inputs
carry bound paths instead of source bodies and full state. Accepted QA design
sources are inherited from canonical captures, including retained designs;
missing captures do not authorize reading live provenance or legacy dispatches.
Reviewer instructions reject manually modified or contaminated task inputs.

External projects use separate baseline and development workspaces. Independent
whitebox review approves test-only changes and exact pure-test file exclusions;
mixed production/test files remain visible to implementation review. There is no
per-file read allowlist or custom reader protocol. This follows the updated
REQ-011, which supersedes the older path-whitelist proposal in the B8 input.

Host differences remain in the adapters:

- WorkBuddy reviewers use `dontAsk`, with Read/Glob/Grep permitted and Bash left
  to native command permission checks rather than pre-approved through allowedTools.
- ZCode reviewers use `plan`, including when resuming a session. Writer tools and
  tools that leave plan mode are absent from the reviewer tool list.
- DSH maps reviewer sessions to native `read-only` and development to
  `workspace-write`; its policy and approval configuration remain host-owned.

Local installed code was inspected on 2026-09-13: WorkBuddy describes `dontAsk`
as denying actions requiring approval; ZCode's `checkPlanMode` rejects non-read-only
destructive tools; DSH sandbox-policy 0.1.5-rc.1 resolves the workspace from the
session cwd. These observations establish API semantics, not successful live
model sessions. Full WorkBuddy/ZCode/DSH native-session read/write demonstrations
remain deferred; fake transport tests must not be reported as those demonstrations.

The obsolete phase-4 installer maintenance patch and its upgrade recipe were
removed. Historical receipts and release evidence remain unchanged.

## Local validation

On 2026-09-13, using `GOCACHE=/tmp/formal-gates-b8-go-cache`:

- `go test ./internal/engine/facade ./internal/engine/protocol ./internal/engine/gitprep ./internal/cli -count=1`
  passed (544.209s, 118.546s, 28.503s, 105.788s respectively).
- Hook/read-only targeted tests in validate and CLI passed. Additional hook,
  VCS-command/search-argument, write-block and terminal-cleanup tests passed.
- `go test ./internal/engine/decision -count=1` passed (0.249s).
- `go build -o /tmp/formal-gates-b8-candidate ./cmd/formal-gates` passed.
- The built candidate's `package validate --root .` passed, including after
  removing the obsolete standalone QA preparation document.
- `git diff --check` passed.

The package tests cover canonical source inheritance, role material selection,
non-Go workspace preparation, retained test review, transport mode changes and
continuous consumption. No real-model opt-in was enabled. These are development
self-tests, not frozen QA receipts or a claim that the candidate was installed.

### Additional local checklist verification

The portable canary still expected the removed legacy workflow command guard.
Adopted the staging canary update to verify ordinary `git diff --name-only`
reading instead. The rebuilt candidate passed all 40 portable canary checks.

`go vet ./...`, `go test -race ./internal/validate ./internal/cli`, and
`go test -tags=phase0whitebox ./internal/validate` passed. Changed Go files are
gofmt-clean. The lifecycle package initially failed its ZCode environment
detection fixture because this worker inherits `CODEX_HOME`; rerunning that
package with `env -u CODEX_HOME -u CODEX_CLI_PATH` passed. Production provider
selection was not changed for the fixture.

The package checklist was run in two disjoint package groups. All packages
finished successfully except the lifecycle fixture above and facade's aggregate
10-minute timeout. Facade had finished the first 170 listed tests without an
assertion failure; the timeout interrupted
`TestVerificationRequirementKeepPromotesNewDocument` after seven seconds.
Only that test and the final listed test were resumed: the former passed in
12.84s; `TestVerificationNativeFailRepairToSeal` retained its real-Codex opt-in
skip. Thus the initial command remains a timeout, not an all-green full-suite
receipt. No real-model tests were enabled to fill the deferred host evidence.
