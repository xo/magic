# D59. The walker fixes a bug of binwalk with a decision, and so does the oracle

Status: Decided. Amends D49. Amended by D60.

The maintainers decided on 2026-11-15 that the walker fixes a bug of binwalk
when the bug makes the walker miss what binwalk found before it, and that
the oracle carries the same fix, so that the gates still compare the two.

The walker gives the matches of binwalk at the commit that the ledger names
(D49), bugs included, as the port of libmagic does (D48). A fix is the
exception, and each one needs a decision of the maintainers:

1. The walker fixes the bug, and a comment names this decision.
2. `test/oracle/patches/` holds a patch that fixes the same bug in the source
   of binwalk. `test/oracle/build-binwalk.sh` applies each patch, in order of
   its name, before cargo builds binwalk.
3. The bug is reported to upstream. When upstream fixes it, the ledger takes
   the fix, and the patch goes.

The first fix: since commit `e43c70c` of 2024-11-24, after the release 3.1.0,
`xz_parser` compares the absolute offset of the next stream with the bytes
that follow the match. It misses every xz stream that starts further into
the data than its own length. A program that used the walker missed such
streams, which binwalk 3.1.0 found. `0001-xz-offset.patch` compares the
offset with the bytes of the data, as the walker now does. It is reported to
upstream as issue 982 of ReFirmLabs/binwalk.
