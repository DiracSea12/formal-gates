# Architecture Health Gate

Review module boundaries, ownership, dependency direction, public surface,
state and cache lifecycle, failure semantics, performance shape, and coupling
introduced by the current change. Check that each responsibility has one clear
owner, dependencies point in the intended direction, state is created and
retired by the correct owner, and errors preserve the documented behavior.

Look for the same capability being maintained in multiple modules, leaked
internal details, hidden cross-module coordination, incompatible lifecycle
assumptions, and changes that leave old and new architecture paths active at
the same time. Do not redo general complexity or code-quality review unless a
concrete architecture defect depends on it.
