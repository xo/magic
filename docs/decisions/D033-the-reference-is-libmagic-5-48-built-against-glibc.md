# D33. The reference is libmagic 5.48 built against glibc

Status: Decided. Amends D30. Amended by D48.

The maintainers decided on 2026-11-15 that the reference for the output of the
port is libmagic 5.48, built from the checkout at `FILE5_48` (D25, D26)
against glibc, in the C locale, with `TZ=UTC`.

A build against musl gives the same output when it is linked against musl too.
The fault of the cgo package of today, which is built against musl and can be
linked against glibc, does not touch the reference, because the reference is
built and linked against one C library (`docs/BACKLOG.md`).

D30 proposes the oracle in a test module, and this decision names the C
library of that oracle.
