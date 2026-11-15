# D20. The embedded database is compressed with zstd, and expands into C memory

Status: Decided. Amends D3. Amended by D47.

The maintainers decided on 2026-11-15 that the embedded database is stored as a
compressed artifact. The iv session asked for it. The database is 10,793,952
bytes, and `//go:embed` put all of it in every program that imports the package.
In iv, that was 10.8 MB of a 117.5 MB build.

What changed:

- `libmagic/magic.mgc.zst` replaces `libmagic/magic.mgc`. It is 345,182
  bytes, from `zstd -19`. On this file, level 3 gives 408,107 bytes and
  `--ultra -22` gives 325,485. The saving of level 22 is 20 KB, and level 19 is
  enough.
- `libmagic/magic.mgc.sha256` holds the hash of the plain database.
  `TestEmbeddedDatabase` expands the embedded file and compares the hash.
- `libmagic/zstd.h` and `libmagic/zstd_errors.h` are the headers of the zstd
  that built the archives. The script copies them from the build, with
  `magic.h`. The first draft shipped `zstd.h` alone. It built on the build host
  only because the host has its own `zstd_errors.h`, and a cross build for
  darwin found it. `TestHeadersAreComplete` now fails on a header that includes
  a file that `libmagic/` does not hold.
- `magic.go` has `magic_expand` in its C preamble. It reads the size of the
  content from the frame header, refuses a size of zero or of 1 GiB or more,
  calls `malloc` for that size, and calls `ZSTD_decompress` into it. `New`
  hands that block to `magic_load_buffers`, so the Go heap never holds the
  database. This replaces the copy that `C.CBytes` made, and the memory that
  one handle uses stays the same.
- `libzstd.a` was already linked for libmagic, so the decompressor adds no
  library.
- The script keeps the plain database in `.build/shared` to compare it across
  targets, compresses it at the end, and no longer writes `libmagic/magic.mgc`.

`TestExpandRefusesBadInput` feeds the loader empty input, text, cut and damaged
copies of the real file, a frame with no recorded size, and frames that claim
1 TiB and zero bytes. Each gives an error. The huge size is refused before
`malloc`. A change to the window byte of the header does not change the
output, and the test does not expect an error for it.

Measured on 2026-11-15 on the build host, with `go build ./cmd/file` at
`v0.3.1` and at this change: the binary went from 14,554,368 to 4,111,496
bytes, which is 10.4 MB smaller. `BenchmarkNew` went from about 0.27 ms to
about 1.7 ms for each call, three runs of 200 each. The old time was a copy of
10.8 MB, and the new time is the expansion. A program that opens one handle
does not notice. A program that opens a handle for each file pays 1.4 ms more
each time, and can share one handle, which D9 makes safe.

The archives do not change, so no target needs a new tag, and the change is in
the root module only.

After the release of `v0.4.0`, the iv session reported its own release build,
`go build -trimpath -ldflags='-s -w'` for linux/amd64. It went from
117,480,312 to 107,039,736 bytes, which is 10.44 MB smaller, and every test of
iv passed.
