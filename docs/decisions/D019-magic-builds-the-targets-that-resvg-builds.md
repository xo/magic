# D19. magic builds the targets that resvg builds

Status: Decided. Amends D1.

The maintainers decided on 2026-10-14 that magic covers only the targets of
resvg, and that it needs no linux/ppc64le and no linux/s390x. The targets are
linux/amd64, linux/arm64, linux/arm, darwin/amd64, darwin/arm64, windows/amd64,
windows/arm64 (D16) and freebsd/amd64.

freebsd/amd64 is new. The script builds it with `zig cc` for `x86_64-freebsd`
and the `noexec` field of D16. It has no CI job, because GitHub has no hosted
FreeBSD runner.

resvg also has `linux_amd64_musl` and `linux_arm64_musl`, which a user selects
with `-tags musl`. magic does not add them. Its Linux archives are built for
musl and are static, so a program links them under glibc and under musl. The
maintainers accepted this on 2026-10-14. On linux/amd64, the tests passed when
the program was linked with the glibc gcc of the host and with `zig cc` for
musl. On linux/arm and linux/arm64, they passed linked with `zig cc` for musl
and run under qemu. A glibc link of arm and arm64 was not tried.

On 2026-11-12, this turned out to be wrong for glibc. The same `linux_amd64`
archive gives the output of `file` on all 21 sample files when a program links
it with musl. When a program links it with glibc, the description of the three
Netpbm files is wrong, for example `size = 6 10\012000010...` where `file`
says `size = 6 x 10`. The archives are built against the headers of musl, and
the text is wrong where libmagic uses a regular expression, so the call into
the regex code of glibc is the suspect. No one confirmed that. The MIME types
and the tests are not affected. `v0.2.1` and `v0.3.0` have the same fault. The
reason for the decision, that one musl build serves both libcs, does not
hold for descriptions. Open question 9 asks what to do.

Archives for linux/riscv64, linux/ppc64le and linux/s390x were built on
2026-10-14 and deleted, and none was committed.

What ran on 2026-10-14:

- The tests passed on linux/amd64, linux/arm and linux/arm64, and on
  windows/amd64 under wine.
- The program and the test binary linked, and nothing ran them here, for
  windows/arm64 and freebsd/amd64. After the commit, windows/arm64 ran in
  CI and passed, as D16 says. freebsd/amd64 has not run.
- The package compiled, and was not linked, for darwin/amd64 and
  darwin/arm64. `zig` has no macOS SDK, so the CI jobs on macOS do the link.

The targets of resvg are not final. resvg staged windows/arm64 and freebsd/amd64
and did not commit them. If resvg drops one, the maintainers decide whether
magic does too.
