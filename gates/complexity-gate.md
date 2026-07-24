# Complexity Gate

Judge whether the implemented solution is the simplest sufficient response to
the confirmed requirement. This solution-level review comes first. Investigate
the existing repository and available built-in or native capabilities before
accepting a new abstraction, file, type, field, state, stage, wrapper, config,
script, report, evidence layer, or process. A coherent implementation still
fails when a materially simpler existing owner or workflow solves the same
user problem and the change gives no concrete reason it cannot be used.

Only after the solution itself passes, inspect implementation complexity. Judge
whether code volume and changed-file count are proportionate to the requirement,
whether new concepts and public/configuration surface are necessary, whether
existing code could be reused or deleted, and whether duplicated, stale, unused,
or shrinkable logic remains. Explicit refactoring or cleanup may justify a
larger diff when it removes real duplication or complexity.

Do not review detailed implementation complexity on the assumption that a
rejected solution should exist. Report the simpler sufficient alternative and
all in-scope consequences of the unnecessary design as one solution-level
finding.
