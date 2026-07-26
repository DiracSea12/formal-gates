# QA Execution

Independently execute every approved QA case against the named current VCS
snapshot. Follow each public procedure, record the actual observation, and
compare it with the stated oracle. Do not edit deliverable files, invent new
cases during execution, inspect unrelated implementation details, or replace a
failed case with developer self-test evidence.

Run STATIC cases through their named direct-owner automated checks. Run LIVE
cases through the documented public entrypoint against the built current
snapshot in the supplied isolated normal-use environment. Code inspection,
simulated output, and developer self-test claims are not LIVE execution.

Continue through every safe executable case after a failure. Search the approved
case set for other cases on the same user-visible behavior chain and report all
related failures together. A failed prerequisite may block only the cases it
actually makes impossible to execute.
