# D16. windows/arm64 is a target

Status: Decided. Amends D1. Amends D2. Amends D12. Amended by D17. Amended by D47.

The maintainers decided on 2026-10-14 to add windows/arm64, after they asked
Gemini Pro and DeepSeek Pro which platforms to add. Gemini Pro recommended it,
because Windows on arm64 hardware is growing and GitHub has a hosted runner for
it. DeepSeek Pro put it on request and said that no hosted runner exists. The
label list of `actionlint` holds `windows-11-arm`, so that claim was wrong.

What changed:

- `libmagic/windows-arm64/` holds the same six archives as `windows-amd64`,
  8.2 MB in all. `build-libmagic.sh` built them with `zig cc` for
  `aarch64-windows-gnu`. All six are `coff-arm64`.
- `magic.go` has a `#cgo` line for windows/arm64.
- The `test` workflow has a `windows/arm64` job on `windows-11-arm`. That
  runner has no arm64 mingw-w64, so the job uses `zig cc` as the C compiler.
  It does not pass `-race`, because Go does not support the race detector on
  windows/arm64.

Two things differ from the other targets in the script, and neither changes
an archive of another target:

- A configure script with only `--host` runs a test program to find out
  whether it is cross-compiling. On this host, wine tries to run the arm64
  program and hangs. The `noexec` field in `TARGETS` makes the script pass
  `--build` as well, so that configure runs nothing.
- xz builds a version resource for Windows, and its configure looks for
  `<host>-windres`. The host has `windres` only for x86_64 and i686, so the
  script links `llvm-windres` under the name of the host and gives it the
  mingw headers of zig. The resource is in `liblzma.a` for both Windows
  targets.

The same change copies each source tree with `cp -rp`. Without `-p`, the
copy gets new timestamps, and `make` then tried to regenerate `aclocal.m4`
in xz and failed.

Test status: the build host links a `file.exe` and a test binary for
windows/arm64 with `zig cc`. It cannot run them, because it has no arm64
Windows. The first run of the CI job is the first time that the tests run on
windows/arm64.

The first run failed, because zig for arm64 Windows crashed on the runner
with exit 0xc0000005 while it built `runtime/cgo`. The job now installs
llvm-mingw 20261006 and uses its clang as the C compiler. On that run, every
test passed on windows/arm64, and `TestOracle` passed. So the archives that
zig built link and work under a second compiler.

The maintainers also asked magic to match the targets of resvg. The list of
resvg is not final, and open question 8 holds what is left.
