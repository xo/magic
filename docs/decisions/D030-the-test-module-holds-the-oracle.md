# D30. The test module holds the oracle

Status: Proposed. Amended by D33.

An oracle is a second program that gives the right answer for a test. The
oracle of the port is libmagic itself, built from the checkout at the base
commit (D25, D26).

magic gets a module in `test/` with its own `go.mod`, as okage, dbmeta and
transit have. No program that imports magic imports it, so it can use cgo.
It holds:

1. The oracle: libmagic built from `upstream/file` at `FILE5_48`, against one
   C library, in the C locale, with `TZ=UTC`. Open question 10 asks which C
   library.
2. The differential test. It gives the same bytes to the oracle and to the
   port, with the same flags, and fails on any difference in the text, the
   MIME type, the encoding or the error.
3. The corpus: the tests of file, the test data of this repository, cut and
   damaged copies of them, and other files that the maintainers approve.

The oracle must be built from the checkout, not taken from the system. The
`file` of a system can be another release, with other patches, and the cgo
package of today mixes musl and glibc (`docs/BACKLOG.md`).

The cgo package that users have today can be the oracle until the port
replaces it, as long as it is linked against the same C library that it was
built for.
