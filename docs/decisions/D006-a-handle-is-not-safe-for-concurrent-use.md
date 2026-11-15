# D6. A handle is not safe for concurrent use

Status: Decided. Amended by D9.

Commit `7b14d3f` (2026-09-04).

The first comment on `Magic` said that a `magic_t` holds mutable state for
each lookup and is not safe for concurrent use. It told the caller to give
each goroutine its own handle, or to guard one with a mutex.
