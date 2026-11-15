# D34. The port copies the behavior of libmagic, not its code

Status: Decided. Amends D22. Amends D26.

The maintainers decided on 2026-11-15 that the port does not follow the C code.
It ports what libmagic does, not how its functions are written.

D22 said that the Go code follows the C code closely enough that a person can
port an upstream change by reading its diff. That no longer holds. The Go code
is designed as Go: its own types, its own functions and its own structure.
What must match is the behavior: for the same bytes and the same flags, the
port gives the output of the reference (D33).

The C code stays the specification, with the rules and the tests of file. When
the behavior is not clear, the C code decides it, and the oracle shows it.

For following upstream (D26), a commit of upstream is ported by its effect.
The question for each commit is how the behavior of libmagic changed, and the
Go code changes to match. The ledger still has one line for each commit. One
magic commit can port several upstream commits, and the column `magic` names
it on each line.
