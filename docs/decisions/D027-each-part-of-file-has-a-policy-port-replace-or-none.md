# D27. Each part of file has a policy: port, replace or none

Status: Decided. Amended by D36. Amended by D35.

The maintainers decided on 2026-11-15 that the compression libraries move to
the Go standard library where it has them, and to other repositories where it
does not (D28). Each part that the cgo package uses today has one policy:

1. Port: magic translates the code, with the ledger of D26.
2. Replace: the Go standard library, or an outside package that the
   maintainers approve (D28), does the work. magic ports none of its code.
3. None: Go does the work without the part, or magic does not need it.

| Part | Today | Policy | Go form |
| --- | --- | --- | --- |
| libmagic, `src/` of file, but not `file.c` and `seccomp.c` | `libmagic.a` | Port | the root package and `internal/` |
| the rules, `magic/Magdir` | `magic.mgc.zst` | Port | tables made from the rules (open question 12) |
| the tests of file, `tests/` | none | Port | test data of the Go tests |
| zlib | `libz.a` | Replace with `compress/zlib`, `compress/gzip` and `compress/flate` | none |
| bzip2 | `libbz2.a` | Replace with `compress/bzip2` | none |
| xz and lzma | `liblzma.a` | Replace with an outside package (D28) | none |
| zstd | `libzstd.a` | Replace with an outside package (D28) | none |
| libgnurx | `libgnurx.a`, Windows only | Replace with `regexp` and its POSIX mode | none |
| `file.c`, the command line program | `cmd/file`, an example | None. `cmd/file` stays an example | none |
| `seccomp.c` | not built | None | none |

Two parts of libmagic are not settled:

1. libmagic runs outside programs, such as `lz4` and `brotli`, for formats that
   it has no library for, when `MAGIC_COMPRESS` is set. Whether the port runs
   outside programs is open question 13.
2. libmagic matches each regular expression in the C locale, on bytes. Go's
   `regexp` matches UTF-8, so the port must keep the byte semantics. The plan
   describes how, under "The hard parts".
