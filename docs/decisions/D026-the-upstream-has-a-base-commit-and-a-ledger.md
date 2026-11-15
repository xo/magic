# D26. The upstream has a base commit and a ledger

Status: Decided. Amended by D34.

1. `docs/upstream/upstreams.tsv` lists the upstream repository: its URL, its
   checkout (D25), what magic ports from it, the magic package, its policy
   (D27), its base commit and the last commit that magic matches.
2. The base commit is the tag `FILE5_48`, commit
   `eb754ace19fed5481d8142426543100a2d6bae4e` of 2026-06-07. It is the
   release that the cgo package wraps, so the tests of today and the port
   have one reference. The tarball `file-5.48.tar.gz` has the same `src/` as
   the tag.
3. The ledger is `docs/upstream/file.tsv`, with the columns `upstream`,
   `date`, `status`, `magic` and `note`. It has one line for each commit after
   the base commit, in the order of upstream, with no gap.
   `TestTheLedgerIsComplete` makes sure of that when the checkout exists.
4. Each new upstream commit gets a status as it arrives: `ported`,
   `not-applicable` or `pending`. Before the base port, every commit that
   changes the library or the rules is `pending`.
5. After the base port, magic ports the commits in the order of upstream, one
   magic commit for each upstream commit that it ports.
6. The column `matched` never moves past a commit that is `pending`. It stays
   empty until the base port is done.
7. magic follows upstream when a maintainer asks. No scheduled task fetches or
   ports upstream.

On 2026-11-15 the upstream has 90 commits after the base commit, from
2026-06-07 to 2026-11-14. 42 change the library in `src/`, 40 change the rules
in `magic/`, 9 change the tests, and 6 change none of these.

This follows okage D12, with one upstream in place of several.
