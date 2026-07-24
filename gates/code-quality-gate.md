# Code Quality Gate

Review correctness, documented edge cases, maintainability, local performance,
test quality, dead code, overfitting, encoding, and validation completeness in
the current change. Trace affected call, data, error, and observable-behavior
paths. Confirm tests exercise the behavior at the lowest layer that directly
owns it and that higher-level repetition is required only when that layer adds
distinct normal-use behavior.

Look for hidden branching, merged responsibilities, vague names, packed logic,
unhelpful deletion of comments or error handling, stale compatibility paths,
and changed files missing from the native VCS comparison. Prefer small local
repairs or deletion over new mechanisms. Do not use code quality to reopen an
accepted solution or architecture preference without a concrete correctness or
maintainability defect.
