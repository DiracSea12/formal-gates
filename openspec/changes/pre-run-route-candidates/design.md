# Design

Add a small exported validation helper that loads `PromptCatalog`, prepends the
built-in `qa` ID to `GateIDs`, and returns the ordered slice. Reuse that helper
from the new package subcommand. The existing workflow candidate function may
also reuse the same ordering helper after enforcing its current run-bound
preconditions.

Extend `runPackage` to distinguish `validate` from `route-candidates` while
preserving the current default `package validate` behavior. Print the candidate
slice through the existing JSON output helper.

Test catalog ordering and invalid-package rejection at the validation layer.
Test only the new public command parsing/output behavior at the CLI layer.
