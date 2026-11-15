# D38. The port adds no more to a program than the cgo package

Status: Decided. Amends D21.

The maintainers decided on 2026-11-15 that a program built with the port must
grow by as few bytes as possible, and by no more than the cgo package makes it
grow today. Smaller is better.

The cgo package is the baseline. Measured on 2026-11-15 with Go 1.27.4 on
linux/amd64: a program that reads a file and prints its MIME type with
`v0.4.0` is 995,488 bytes larger than the same program without magic, when
both are built with `go build -trimpath -ldflags='-s -w'`. Without
`-ldflags='-s -w'` the difference is 1,662,342 bytes.

The port measures the same two programs in a test, and fails when the
difference is larger than the baseline. The rules are most of the size, so
their tables (D35) and how they are stored matter most. A decoder that only the
tables need, such as one for zstd, counts against the budget.

`MagicDB` goes when the port replaces the cgo package. Nothing in the package
needs the database as bytes, and a program that does not call it must not pay
for it. D21 kept it for the cgo package, and the port has no `.mgc` to give.
