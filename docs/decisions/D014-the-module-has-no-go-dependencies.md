# D14. The module has no Go dependencies

Status: Proposed. Amended by D28.

`go.mod` has no `require` line, and no commit since `7b14d3f` added one. The
package imports only the standard library and `C`. No commit, comment or
document says that this is a rule. The maintainers have not said.
