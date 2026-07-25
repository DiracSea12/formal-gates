# Design

Add a small exported validation helper that loads `PromptCatalog`, prepends the
built-in `qa` ID to `GateIDs`, and returns the ordered slice. Reuse that helper
from the new package subcommand. The existing workflow candidate function may
also reuse the same ordering helper after enforcing its current run-bound
preconditions.

Make an empty gate set valid in `LoadPromptCatalog`, so every catalog consumer
shares the QA-only behavior. Keep gate discovery in the existing directory
loader: ignore ordinary subdirectories and non-Markdown files, while rejecting
`.md` entries that are not regular files, invalid gate IDs, and the reserved
`qa` ID. Do not add a query-specific scanner or validation exception.

Extend `runPackage` to distinguish `validate` from `route-candidates` while
preserving the current default `package validate` behavior. Print the candidate
slice through the existing JSON output helper.

Test zero-gate catalogs, direct gate ordering, ignored unrelated entries, and
invalid gate-like entries at the validation layer. Test only the new public
command parsing/output behavior at the CLI layer.
