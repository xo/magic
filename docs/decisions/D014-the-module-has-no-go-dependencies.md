# D14. The module has no Go dependencies

Status: Decided. Amended by D28.

When this decision was written, `go.mod` had no `require` line, and no
commit since `7b14d3f` had added one. The package imported only the standard
library and `C`. No commit, comment or document said that this was a rule.

The maintainers decided on 2026-11-20 that it is a rule. The root module has
no Go dependencies, and its `go.mod` has no `require` line.

D28 makes an exception for the decoders of xz and zstd, and D45 puts them in
modules of their own, so that the root module requires neither. Any other
module from outside this repository needs the approval of the maintainers,
and a decision records it.

`TestEveryRequireIsApproved`, in `gomod_test.go`, holds this rule. It reads
each `go.mod` of the repository, and fails on a `require` line for a module
that no decision approves.
