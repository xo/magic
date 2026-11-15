# D18. A target version has a patch number, and a target module declares go 1.21

Status: Decided. Amended by D41.

These two details follow resvg and a limit of Go. The maintainers accepted them
on 2026-10-14.

A version of file has two parts, such as 5.48, and a Go version has three. The
script adds `.0`, so the tag is `v5.48.0`. A published tag never moves. If a
rebuild keeps the same version of file, for example for a new xz, the tag
needs the next patch number, `v5.48.1`. The script prints `.0` and does not
know about a patch.

The `go.mod` of a target module says `go 1.21`, as the platform modules of
resvg do. The root says `go 1.25`. Nothing in a target module needs more than
1.21.
