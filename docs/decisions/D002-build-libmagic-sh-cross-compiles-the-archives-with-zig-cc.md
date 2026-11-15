# D2. build-libmagic.sh cross-compiles the archives with zig cc

Status: Decided. Amended by D16. Amended by D17. Amended by D22.

Commit `7b14d3f` (2026-09-04). Commit `a629036` changed how the versions are
chosen (D13).

One script on one machine builds the archives for all six targets. `zig cc`
is the C compiler for every target. The Linux targets use musl. The script
first builds a native copy of `file`, because the build of file runs `file`
to compile `magic.mgc`, and the cross-compiled one does not run on the host.

The comments in the script give the reason for each workaround, such as the
bzip2 Makefile that ignores `CC` and the zlib `configure` that does not work
for MinGW.
