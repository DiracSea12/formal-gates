# Proposal

Expose the current formal-gates route candidates before a workflow run exists.
The universal modification intake needs to present `full` and `custom` choices
in the same user question, so it must be able to list built-in QA followed by
the package's dynamic gate catalog without creating temporary workflow state.

This slice adds only the read-only candidate query. It does not change route
modes, workflow state, or formal-run ordering.
