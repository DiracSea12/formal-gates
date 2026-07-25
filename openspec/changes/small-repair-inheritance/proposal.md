# Proposal

Add a main-agent fast path for a narrowly bounded post-development repair. When
the immediate repair diff cannot affect any previously passing verification,
the main agent may skip the independent Carry dispatch, record its reasoning,
inherit every prior PASS, and rerun only results that had not passed.

Keep independent Carry as the fallback for nontrivial, partially affecting, or
uncertain repairs. Reuse the existing Carry transition and snapshot rebinding
owners instead of adding a parallel repair state machine.
