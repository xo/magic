# D41. The module requires Go 1.27

Status: Decided. Amends D18.

The maintainers decided on 2026-11-15 that the root module requires Go 1.27,
so that the API can use the language of Go 1.27 (D40). The `go` directive of
the root `go.mod` is `go 1.27`. It was `go 1.25`.

Checked with Go 1.27.4 on 2026-11-15, these build: generic methods, generic
type aliases, iterators over functions (`iter.Seq`), self-referential
constraints, and `new` of an expression. A constraint cannot hold `io.Reader`
in a union with `[]byte`, because an interface with methods cannot be in a
union. So `Detect` can be generic over `[]byte` and `string`, and a reader or a
file needs a function of its own.

The modules of each target keep `go 1.21` (D18). They hold archives and one
line of cgo, and they leave with the cgo package (D22).
