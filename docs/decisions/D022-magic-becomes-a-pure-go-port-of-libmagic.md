# D22. magic becomes a pure Go port of libmagic

Status: Decided. Amends D1. Amends D2. Amended by D34.

The maintainers decided on 2026-11-15 that magic becomes a pure Go port of
libmagic, the library of the [file](https://github.com/file/file) command,
and that the repository is set up for the port as okage is set up for its port
of tiny-skia and resvg (okage D1).

## What pure Go means

A program that imports magic needs the Go toolchain and nothing else. The
module does not use cgo and does not link a C library. It builds for every
`GOOS` and `GOARCH` that Go supports, with `CGO_ENABLED=0`. Only the test
module can use cgo (D30).

When the port is ready, the static archives of D1, the build script of D2 and
the module for each target of D17 leave the repository. Until then the cgo
package stays, and it is what a user gets.

## What a port means

The port translates the C code of libmagic and the rules of its database. It
is not a new design with the same purpose. The behavior is the same as file
5.48 on the reference that the plan names, including its faults. A deliberate
difference is a decision of its own, and `README.md` lists it.

The Go code follows the C code closely enough that a person can port an
upstream change by reading its diff (D26). Inside that limit, the code is
idiomatic Go: it uses Go types and errors, and not C idioms such as output
parameters and global state. How close the code follows the C code is open
question 11 in [`../PLAN.md`](../PLAN.md).

## What is not ported

Only libmagic and its database are ported. The compression libraries are
replaced (D27). The command line program of file is not ported, and
`cmd/file` stays a small example. The binary walker is a later project (D31).

The models of [`../PLAN.md`](../PLAN.md), under "The reviews", gave the
estimates for this decision.
