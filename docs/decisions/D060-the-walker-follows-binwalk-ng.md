# D60. The walker follows binwalk-ng

Status: Decided. Amends D49. Amends D59.

The maintainers decided on 2026-11-16 that the walker follows binwalk-ng.

binwalk-ng/binwalk-ng is the maintained fork of binwalk. It forked from
ReFirmLabs/binwalk at `beda1ffb`, before the commit that the walker follows,
and holds every change of that commit. `docs/BINWALK-NG.md` holds the plan.

What the decision holds:

1. The upstream of the walker is binwalk-ng, with `beda1ffb` as the base of
   its ledger (D26). The walker gives the matches and the files of binwalk-ng
   at the commit that the ledger names, as D49 says for binwalk.
2. The oracle is built from binwalk-ng. binwalk-ng fixed the bug of xz of
   D59, so the patch of the oracle goes. The rule of D59 stays for other bugs.
3. The changes of binwalk-ng are ported up to its newest commit, and the
   three gates check them.
4. The walker writes no new decoder for the move. binwalk-ng extracts lzfse,
   rar and QNX IFS with its own code, through crates. The walker has no
   decoder of those, so it finds them and does not extract them, and the
   gate of the extractors leaves them out. Every fix of binwalk-ng that
   needs no new decoder is ported.
