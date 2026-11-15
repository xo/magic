# D11. The tests expect what libmagic 5.48 reports, and file(1) is the reference

Status: Decided. Amended by D48.

Commit `dbcd068` (2026-09-12). Commit `a629036` (2026-09-13) added the note
about new file releases to the script.

Each case in `mimeTests` expects the MIME type that libmagic 5.48 reports.
The comment on `mimeTest` says that some values are less specific than the
real type, and calls that "the library's behaviour, not a gap in the test".

`TestOracle` writes each case to a file and compares the result of the
package with `file --mime-type` of the system. It runs only when `file` of
the same version is on `PATH`. Its comment gives the reason: the expected
values alone prove only that the wrapper agrees with itself. The script says
that a new release of file changes the output, "so the Go tests that assert
exact descriptions may need regenerating afterwards".

Found on 2026-09-27: the names of the cases and 16 of the 21 files in
`testdata/` come from the tests of `github.com/gabriel-vasile/mimetype`. The
16 files are byte for byte the same as those in mimetype v1.4.15. The case
names "json issue#239" and "jar in zip #639" name issues of mimetype, not of
this repository. Nothing in this repository says so.
