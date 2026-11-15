# D40. The API is new, and `magic.Detect` is its entry

Status: Decided. Amended by D42.

The maintainers decided on 2026-11-15 that the port has a fresh, clean API,
designed for Go and not copied from the C API of libmagic (D39):

1. The high-level entry is `magic.Detect`.
2. The options of a detection are functional options, given to the call.
3. Each call that can read a file takes a `context.Context` as its first
   parameter.
4. The API uses the generics of Go 1.27 where they fit (D41).

[`../API.md`](../API.md) holds the design. Its details are a proposal until
the maintainers settle its open questions.
