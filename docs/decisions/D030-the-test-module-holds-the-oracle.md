# D30. The test module holds the oracle

Status: Decided. Amended by D33.

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

The maintainers adopted this decision on 2026-11-15, when they started phase
1. The harness has these parts:

1. `test/oracle/build.sh` exports the tree of `FILE5_48` from the checkout,
   builds libmagic with gcc and the compression libraries of the host, and
   builds the oracle program. It writes only to `test/.oracle/`, which git
   ignores.
2. `test/cmd/oracle` is the oracle program. It links the reference libmagic,
   runs in the C locale with `TZ=UTC`, sets `MAGIC_NO_COMPRESS_FORK` on every
   call (D36), and answers one JSON object a line. It is a process of its own,
   because the code under test can define the same C symbols.
3. `test/oracle` starts the program and asks it questions, and reads the tests
   of file from the exported tree. `TestOracleMatchesTheTestsOfFile` proves
   that the oracle gives each of the 88 expected results.
4. `test/corpus` holds the inputs: the 88 test files, the files of `testdata/`,
   and cut and damaged copies of each, from a fixed seed. That is 1,544
   inputs.
5. `test/diff` compares the code under test with the oracle, for each input and
   each output flag. Before the port, the code under test is the cgo package,
   built for musl. `TestCgoPackageMatchesTheOracle` is the gate of phase 1.
6. The job `oracle` of CI builds the oracle and runs the test module.
