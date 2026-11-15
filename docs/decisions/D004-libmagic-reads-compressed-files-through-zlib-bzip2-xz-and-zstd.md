# D4. libmagic reads compressed files through zlib, bzip2, xz and zstd

Status: Decided. Amended by D43.

Commit `7b14d3f` (2026-09-04). Commit `dbcd068` (2026-09-12) added
`-lshlwapi` to the Windows line.

The script configures file with `--enable-zlib --enable-bzlib --enable-xzlib
--enable-zstdlib` and with `--disable-lzlib --disable-libseccomp`. On
Windows, libgnurx gives libmagic the POSIX regex functions that MinGW does
not have. `src/magic.c` in file 5.48 includes `shlwapi.h`, so the Windows
line also links `shlwapi`.

The comment on `GNURX_FALLBACK` says that libgnurx is under LGPL 2.1, "unlike
everything else here". Reason not recorded for the choice of the four
compression libraries.
