# D36. magic runs no outside program

Status: Decided. Amends D27.

The maintainers decided on 2026-11-15 that magic does not run a program of
the system. libmagic runs outside programs, such as `lz4`, `brotli` and
`lrzip`, for formats that it has no library for, when `MAGIC_COMPRESS` is set.
The port does not.

For such a file, the port names the compressed file and does not look inside
it. Where that differs from the reference, the difference is accepted, and
`README.md` lists it.

A format that a pure Go decoder handles can still be read inside, as D27 and
D28 allow for gzip, zlib, bzip2, xz and zstd.
