# Guard calculation preview (阶段 3.5b)

```sh
go build -o bin/formal-gates ./cmd/formal-gates
bin/formal-gates workflow guard-preview --input references/guard-preview-basis.json
```

`workflow guard-preview --input <basis.json>` reads one UTF-8 JSON object and prints
JSON. It does not need a run, repository, installation registration or transcript.
Success returns exit code 0; errors return nonzero with the input filename and the
invalid field or JSON diagnostic on stderr. It never creates or changes run state.

This is a **non-authoritative calculation preview**: success only means the supplied
declarations are self-consistent. It does not authenticate a real run, confirmation
or task's legitimacy. `runtimeIntegration` is always `NOT_INSTALLED` at stage 3.5b;
IssueFromPlan, refill and recovery are not guarded by this command. Stage 4 will
construct the same basis from authoritative engine state and report that integration.

中文：本命令只预览有限派发额度；输入自洽不等于真实确认或合法任务认证。阶段 3.5b 固定
输出 `NOT_INSTALLED`，不代表运行时止损已接入。文件不可读、JSON/schema 错误、缺身份、
确认 revision 不匹配、非法 retry 或冲突 TaskKey 均返回非零，且不写流程状态。

## Input schema, version 1

The complete runnable [example](guard-preview-basis.json) declares three tasks with
MaxAttempts 1, 2 (omitted default), and 4. Its result is exactly:
`distinctExternalTaskKeys=3`, `requestAllowance=3`, `retryAllowance=4`,
`totalAllowance=7`, plus a normalized `basis` and `budgetBasisDigest`.

The example's digest is
`sha256:692f7b9bb402a8762c0f1ccffdfbaab6c7e9f48964e2f4ca54b2236e45e1bcef`.

| Field | Contract |
| --- | --- |
| `schemaVersion` | Required integer `1`. Unknown fields and multiple JSON values are rejected. |
| `requirement`, `batch`, `route`, `topology` | Each requires nonempty `id`, `revision`, and `confirmedRevision` exactly equal to `revision`. These are declarations, not authentication. |
| `definition` | Requires nonempty `id`, `revision` and an explicit closed set of external `externalSteps` in canonical `node/step` form. An empty array is valid for zero external tasks; omitted/null is rejected. |
| `approvedQA`, `selectedGates`, `establishedObligations`, `establishedRepairs` | Sets of `{id, revision}` identities. Empty, omitted or null collections normalize to `[]`. Duplicate identical identities collapse; conflicting revisions for an ID are rejected. |
| `externalTasks` | Set of `{taskKey, provenance, maxAttempts?}`. Empty, omitted or null is a valid zero-task calculation. |
| `taskKey` | Existing engine canonical `node/step[/scope]`; node/step must occur in `definition.externalSteps`. Scope identifies a dynamic instance and may be absent. |
| `provenance` | `{kind, id, revision}` must match one of the exact declared sources below. |
| `maxAttempts` | Total finite executions for this TaskKey; omitted/null defaults to `2`. An explicit integer must be at least `1`; fractional values, strings and an unrepresentable total are rejected. |

Allowed provenance kinds and their source fields:

| Kind | Source |
| --- | --- |
| `DEFINITION` | `definition` |
| `BATCH` | `batch` |
| `ROUTE` | `route` |
| `TOPOLOGY` | `topology` |
| `APPROVED_QA` | `approvedQA` |
| `SELECTED_GATE` | `selectedGates` |
| `ESTABLISHED_OBLIGATION` | `establishedObligations` |
| `ESTABLISHED_REPAIR` | `establishedRepairs` |

An exact duplicate TaskKey with the same normalized retry and provenance contributes
once. A duplicate with conflicting retry or provenance is invalid. Request allowance
is the distinct TaskKey count; retry allowance is the sum of `MaxAttempts - 1` over
that set; total allowance is their sum. Retries after RUNTIME_ERROR or
REJECTED_INVALID_RESULT reuse the same TaskKey. A one-time user-authorized extra
attempt changes only its finite policy and basis digest, not the base slot count.

The normalized basis sorts identities by ID, external steps and tasks by canonical
TaskKey, collapses exact duplicates, materializes default maxAttempts, and uses empty
arrays for empty collections. `budgetBasisDigest` is `sha256:` followed by lowercase
hex SHA-256 of the Go `encoding/json.Marshal` bytes of that basis (struct field order
as displayed in the result, compact JSON, standard escaping, no trailing newline).
All identity/revision, provenance and retry fields participate. Input JSON field
order, collection order and exact duplicates do not affect the result or digest.

In stage 4 the Controller is responsible for accepting only confirmed plan expansion;
preauthorized repair slots may be added mechanically. Finding-derived sources require
completed Operator adjudication and, for wave findings, a collected wave and its unique
repair obligation. Raw findings, ordinary results, receipts and resume never add slots.
The pure function checks declared identities and cross-references only; it cannot
establish those workflow facts from an arbitrary JSON file.
