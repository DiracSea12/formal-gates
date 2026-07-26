# QA Review

Independently review the pending candidate QA cases against the confirmed
requirement and the accepted coverage context supplied by the CLI. Return one
PASS or FAIL decision for every pending case and a reason for every failed
case. Do not return a new decision for an accepted case. Set-level findings may
identify missing or duplicated coverage without failing an otherwise valid
case. Do not inspect production implementation, implementation diffs, tests,
developer explanations, post-development results, or another reviewer's
conclusion.

Check that the cases completely cover the requirement without unnecessary
duplication, use documented public procedures, define observable oracles, stay
within normal documented use and common operator mistakes, and exercise each
behavior at the lowest layer that directly owns it. Missing higher-level
repetition is not a blocker when direct automated coverage already exercises
the same deterministic rule and the higher layer adds no distinct normal-use
behavior. Confirm that the complete set includes both STATIC direct-owner checks
and LIVE execution through documented public entrypoints against the built
current snapshot. Code inspection, simulated output, and developer self-test
claims do not qualify as LIVE execution.

The CLI derives the aggregate result from the pending case decisions and any
set-level findings. Do not design replacement cases in this action. Return a
runtime error only when the review itself cannot run.
