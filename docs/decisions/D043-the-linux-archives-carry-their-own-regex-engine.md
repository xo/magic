# D43. The Linux archives carry their own regex engine

Status: Decided. Amends D4.

The maintainers decided on 2026-11-15 that the Linux archives link libgnurx,
the regex engine of glibc that the Windows archives already use (D4), so that
libmagic never calls the regex functions of the C library.

The fault that this fixes: the Linux archives were compiled against the
headers of musl, and a program on Linux usually links glibc. `regmatch_t` is 16
bytes in musl, with 8-byte offsets, and 8 bytes in glibc, with 4-byte offsets.
Under glibc, libmagic read the offsets of a match wrongly. It printed wrong
text, for example for the Netpbm files, and it crashed with a segmentation
fault on the test file `cmd1` of file, a script of 17 bytes. The harness of
phase 1 found the crash (D30). Every release up to `v0.4.0` had the fault.

How it works:

1. `build-libmagic.sh` builds libgnurx for each Linux target, and compiles it
   and libmagic with `-Dregcomp=magic_regcomp`, and the same for `regexec`,
   `regfree` and `regerror`. libmagic then calls only the renamed functions,
   which the archive holds, and finds `regex.h` of libgnurx before the one of
   the C library.
2. The names are renamed so that the engine of the archive never meets the one
   of the C library in a link, whether that is glibc, shared, or musl, static.
3. The `#cgo` line of each Linux target links `-lgnurx`.

On 2026-11-15, linux/amd64 was checked both ways: the test file `cmd1` and the
Netpbm files give the text of the oracle with gcc and glibc, and with zig and
musl, and `TestCgoPackageMatchesTheOracle` passes both ways on all 11,571
answers. The gate no longer skips a glibc build.

The other choice was a glibc build and a musl build of each Linux target, with
a build tag, as resvg has. It needs more modules, more tags and a choice by
every user. This one needs none, and the port ends the question (D22).

libgnurx is under LGPL 2.1, and is now in the Linux archives too. Open question
6 in `docs/PLAN.md` asks what the README tells a user about it.
