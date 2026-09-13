# B4 message increment and validation

Bound candidate: `cf8764e46ab31a265755a896ec7a9366ffd7810e`.
Requirement revision: `14bccf5f1ac00fd612385207078a03cb7133a9788360303da15cdf03320b59fe`.

This increment removes repeated authoring/review instructions and selects blackbox
and whitebox review/execution sections through existing assembly. Historical runs
retain their captured unsplit instructions. Native structured-output instructions
now have one common owner; supplemental material instructions refer to actual
input paths rather than obsolete field names. Gate-specific copies of shared
reviewer obligations were removed. Contract tests now check the shared owner,
and actual assembly tests require each acceptance-surface obligation exactly once
in both gate inputs. No progress or cost implementation changes remain.

The candidate already had role-selected QA tool descriptions and a whitebox result
schema without `uniqueItems`; these were retained, not newly implemented here.

## Actual assembly measurements

One shared small fixture, production assembly, Unicode character counts. Before
and after use the same fixture and bound candidate. Temporary paths are freshly
allocated; their random suffix lengths can contribute a few characters to the raw counts. These are assembled inputs, not billed tokens or
real-host latency measurements. QA-review measurement uses the production prompt,
schema and tool assembly functions without issuing a second designer action.
Schema and tool columns show before/after separately; unchanged values are intentional.

| Stage | Message before | Message after | Schema before/after | Tool before/after |
|---|---:|---:|---:|---:|
| product-review | 3206 | 3150 | 1476/1476 | 983/983 |
| start-readiness | 4437 | 4330 | 2852/2852 | 983/983 |
| development-worker | 3601 | 3494 | 1536/1536 | 983/983 |
| blackbox-design | 6863 | 6792 | 5548/5548 | 983/983 |
| whitebox-design | 8691 | 7846 | 6375/6375 | 983/983 |
| whitebox-coverage | 1676 | 1676 | 355/355 | 0/0 |
| blackbox-review | 7047 | 6465 | 3281/3281 | 983/983 |
| whitebox-review | 8638 | 7811 | 3281/3281 | 983/983 |
| blackbox-execution | 5993 | 5900 | 2568/2568 | 2554/2554 |
| whitebox-execution | 5996 | 5881 | 2568/2568 | 2554/2554 |
| complexity-gate | 8537 | 8367 | 2568/2568 | 983/983 |
| implementation-quality-gate | 8416 | 8176 | 2568/2568 | 983/983 |
| repair-worker | 3859 | 3836 | 2263/2263 | 2554/2554 |

## Scope and continuation audit

`loadActionInstructions` binds the six action documents used by native workers;
`prepareNativeActionInput` selects review, preparation or verification assembly.
Both gates additionally receive the common reviewer and their own gate rules.
Coverage has its separate prompt/schema and no workflow tools. The matrix covers
all native step kinds dispatched through these assembly paths.

Requirement clarification is an owner/user confirmation operation in this regular
route (`RegularAvailableActions` and confirm-requirement REQUEST/DECIDE), not a
native worker invocation. `requirements-clarification.md`, `carry.md` and
`merge-gate.md` are not loaded by this native action loader. There is no corresponding
native message/schema/tool sample to count; no synthetic zero or model result is
reported for these operations. Existing confirmation behavior is covered by the
installed consumer fixture. These non-worker documents were inspected, not changed. The opt-in
`TestActualStagePromptPathsReadOnly` was skipped because no explicit formal-state
input was supplied; no active formal state was used as a fixture.

Development continuation uses the same developer section and schema, adding only
the captured previousDevelopmentResult. Design revision uses the same mode-specific
section with previousDesign/reviewGaps; review carries retainedReview. The existing
material binding tests cover exact paths, unrelated-body exclusion and retention.
QA continuation uses the same execution section and schema with a newly bound
remaining/checkpoint slice. The checkpoint replay fixture actually received three
turns, read each bound material file, retained one identity and executed C1/C2/C3
without rerunning previously completed cases. These are protocol-fixture results,
not independent semantic QA approvals or billed-token measurements.


## Checks completed

- `go build -o bin/formal-gates ./cmd/formal-gates`: passed with
  `GOCACHE=/tmp/formal-gates-b4-cache`.
- `bin/formal-gates package validate --root .`: passed.
- `bin/formal-gates canary portable --root . --format json`: exited successfully.
- `go vet ./...`: passed with the same cache.
- `go test ./internal/cost -count=1`: passed; existing incremental, owner interval,
  source-presence and unavailable serialization contracts retained.
- Targeted facade tests passed: stage assembly, Markdown selection, both QA design
  contracts, readiness/prestate checks, readonly review output, captured old rules,
  actual bound material paths, Codex coverage-to-authoring protocol, progress,
  wait timing, static checker, candidate source recovery and checkpoint material
  replay. The replay executed C1/C2/C3 in separate turns under one identity and read
  the remaining bound material without repeating prior cases.
- `git diff --check`: passed before this evidence note was added.

## Failure history and final verification

The first targeted run rejected the removal of native structured-output wording;
putting it in the shared section resolved that test. Early validate runs failed
because this checkout lacked `bin`; the documented build resolved that prerequisite.
The next tagged/race runs failed because two historical contract tests required
acceptance-surface clauses in both individual gate files. Tests now verify the
common owner and the actual assembled inputs, without weakening the obligation.
The repaired targeted tests, tagged validate suite and assembly measurement pass.

Early full facade runs reached Go's default ten-minute package timeout while still
progressing through tests. This is retained as a failed test run, not a product
failure. The final full regression uses `go test ./... -timeout 30m`; this affects
only Go's test-run limit and introduces no native model or workflow task budget.
The repaired race suite passed (`internal/validate` 64.271s, `internal/cli` 198.698s).
The first 30-minute-limit full run completed with failures (facade 576.218s):
three whitebox prompt assertions still expected wording removed by deduplication.
The mixed-file test now checks the retained Chinese clause; the short trial
instruction explicitly retains native-shell and not-formal-QA wording. The same
run's lifecycle test inherited this worker's CODEX_HOME and detected Codex despite
setting a ZCode marker. Running that test with CODEX_HOME/CODEX_CLI_PATH unset
passed (0.511s); no provider detection behavior was changed. The repaired whitebox/assembly targeted suite passed (30.654s). The final full run
uses this environment isolation. It passed with exit 0:
`env -u CODEX_HOME -u CODEX_CLI_PATH GOCACHE=/tmp/formal-gates-b4-cache go test ./... -timeout 30m`.
Packages without changes reused valid Go test cache entries; no fresh real-model
run or formal QA approval is implied.

`TestInstalledConsumerReadsBoundMaterialWithoutContext` passed through the normal
installed CLI. Its local Codex protocol endpoint receives actual turn/start input,
reads the referenced material and returns a result accepted by the engine. It
substitutes the model endpoint, so it does not establish a real model's reasoning
quality or a real-host full workflow PASS. No fresh full native workflow was added
solely for character measurement.

The existing public operations/script index remains in `references/engine-regular.md`:
installed package-root lookup, parameters, dependencies, version boundaries and
historical scripts requiring adaptation remain explicit. The static checker and
readiness tests passed; they parse scripts without importing or sourcing them and
exercise an unreachable terminal prestate. No new index or accounting system was
introduced.

Raw local logs are `/tmp/formal-gates-b4-*.log` (transient); character counts,
methods, sample boundaries and failure history are retained above.

## Final result

All required local checks passed: full tests, race tests for validate/CLI, tagged
phase0whitebox validate, vet, build, package validation, portable canary and diff
whitespace checks. The final targeted assembly suite also passed after the last
wording correction. The final canary initially reported a Cursor temporary
bootstrap identity mismatch while verification was in progress; a subsequent
run with files unchanged passed without any installation code change. The first
failure is retained and its root cause is not asserted.

The B4 development checks are complete. Formal review, independent QA, Seal and
release/install adoption remain engine-owned later actions; this document does
not claim they occurred. No unresolved implementation decisions remain.
