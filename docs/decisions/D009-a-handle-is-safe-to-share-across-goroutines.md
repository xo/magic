# D9. A handle is safe to share across goroutines

Status: Decided. Amends D6. Amended by D47.

Commit `dbcd068` (2026-09-12).

`Magic` has a mutex, and every method holds it for the whole call. The
comment on `Magic` says that a single handle is safe to share, "at the cost
of serializing lookups", and that a caller who wants parallel lookups gives
each worker its own handle.

`TestConcurrent` calls one handle from 16 goroutines. The workflow runs the
tests with `-race`, and its comment says that this is "what backs the 'safe
to share a handle' claim".
