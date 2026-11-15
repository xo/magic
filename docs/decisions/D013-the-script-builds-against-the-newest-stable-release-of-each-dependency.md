# D13. The script builds against the newest stable release of each dependency

Status: Decided.

Commit `a629036` (2026-09-13).

On each run, `build-libmagic.sh` looks up the newest release of zlib, bzip2,
xz, zstd and file. A variable such as `FILE_VER` pins one version. `PINNED=1`
turns off every lookup. If a lookup fails, the script prints a warning and
uses the fallback version. The script ignores a pre-release tag, because the
archives are committed (D1). libgnurx stays at 2.5.1, because upstream
shipped its last release in 2007.

The comment on the fallback versions says that they are what the committed
archives were built from. The same commit rebuilt the archives, and every
`liblzma.a` changed. The archives now hold xz 5.8.4, and `XZ_FALLBACK` still
said 5.8.3. On 2026-10-14, a maintainer asked for a check of every dependency.
Each was at its newest stable release, and `XZ_FALLBACK` was set to 5.8.4.

On 2026-11-12, zlib 1.4 was out, with fixes for two CVEs from an audit. They are
a buffer overflow in a non-blocking `gzwrite()` and an out-of-bounds pointer in
`contrib/infback9`. A maintainer asked for a rebuild of all eight targets. The
file version stays 5.48, so each target module takes the next patch number,
`v5.48.1`, as D18 says, and the root takes `v0.3.1`. The script now prints the
patch number from `MODULE_PATCH`. The rebuild used zig 0.17.1, where the
archives of `v0.3.0` came from zig 0.16, so all five libraries differ in bytes.

The clang of zig 0.17 stops with an error on a line of file 5.48 that zig 0.16
only warned about. In `readcdf.c`, `&ts.tv_sec` is a `long *` where a
`const time_t *` is expected, and the two differ in size on Windows. The
Windows targets pass `-Wno-incompatible-pointer-types`, which gives the code
that zig 0.16 built. It does not fix the mismatch. On the 21 sample files, the
Windows build gave the same output as the `file` of the host. The samples
cannot test the timestamp code, because `file` stops on them with "Cannot read
short stream".
