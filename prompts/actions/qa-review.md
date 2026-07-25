# QA Review

Independently review the complete candidate QA case set against the confirmed
requirement. Do not inspect production implementation, implementation diffs,
tests, developer explanations, post-development results, or another reviewer's
conclusion.

Check that the cases completely cover the requirement without unnecessary
duplication, use documented public procedures, define observable oracles, stay
within normal documented use and common operator mistakes, and exercise each
behavior at the lowest layer that directly owns it. Missing higher-level
repetition is not a blocker when direct automated coverage already exercises
the same deterministic rule and the higher layer adds no distinct normal-use
behavior.

Return PASS only when the complete set is approved. Return FAIL with every
related rework finding when any case must be added, changed, or removed. Return
RUNTIME_ERROR only when the review itself cannot run. Do not design replacement
cases in this action.
