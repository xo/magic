# Moving the walker to binwalk-ng

This was the plan to move the upstream of the walker from
ReFirmLabs/binwalk to binwalk-ng/binwalk-ng. The maintainers decided it with
D60 on 2026-11-16, and the move is done. The section The result says what
the walker has now.

## Why

The walker follows ReFirmLabs/binwalk at `26713972e3` (D49). That
repository has had two commits since 2026-05-27. binwalk-ng is the fork that
is maintained: it released 4.0.0 on 2026-09-29, and has commits up to
2026-10-31. It is under the MIT license, as binwalk is.

## What binwalk-ng is, as of 2026-11-16

These facts come from a read of its repository, cloned into
`upstream/binwalk-ng`:

1. It forked from ReFirmLabs/binwalk at `beda1ffb`, of 2026-05-27. Our commit
   `26713972e3` is two commits after it: a fix of a decompression bomb in
   csman, and a commit of format changes. binwalk-ng took the csman fix in a
   form of its own, so it holds everything that the walker follows.
2. It has 406 commits of its own after the fork, and 121 merges. 270 of them
   change `src/`.
3. It moved the code by format: `src/formats/` holds the parser, the
   structures and the extractor of each format, where binwalk had
   `src/signatures`, `src/structures` and `src/extractors`. 308 files
   changed.
4. It has 114 signatures: the 111 of binwalk, and `dlink_fw`, `eva` (Fritz!Box
   EVA) and `program_store` (Broadcom ProgramStore).
5. It extracts lzfse, zstd, lz4, rar, tar, Motorola S-records and QNX IFS
   with its own code, where binwalk ran a program. For rar it uses the crate
   `rars`, and for QNX IFS the crate `qnx-ifs`.
6. It fixed the bug of xz that D59 fixes, in the same way. So the patch of the
   oracle and the fix of D59 go away.
7. It changed the scan: it skips runs of zero bytes, and it builds the
   matcher once. It changed parsers: zip treats a cut archive as its end, the
   parsers check for overflow, and a symlink that cannot be made becomes a
   carved file.
8. Its test inputs moved to a submodule, binwalk-ng/binwalk-ng-samples.

## What changes for the walker

1. The ledger. `docs/upstream/upstreams.tsv` gets a row for binwalk-ng, with
   the fork commit `beda1ffb` as its base, and the row of binwalk goes. The
   ledger of binwalk-ng has one line for each of its 406 commits, each
   `ported` or `not-applicable`, as D26 asks. About 160 of them change only
   CI, the documents, the tests or the versions of crates.
2. The oracle. `test/oracle/build-binwalk.sh` builds binwalk-ng from
   `upstream/binwalk-ng`, at the commit of the ledger. `corpus.Walk` reads the
   samples from the submodule of binwalk-ng.
3. The changes of the 270 commits that change `src/`, ported in the order of
   upstream, each checked by the three gates:
   - the three new signatures, with their extractors;
   - the new extractors of binwalk-ng's own code: lzfse, zstd, lz4, rar, tar,
     srec and qnx_ifs. They become binwalk's own code, so the gate of the
     extractors compares them too, where today a test compares them with the
     programs. The walker needs decoders for lzfse, rar and QNX IFS. `rars`
     and `qnx-ifs` must be read, and their licenses checked, before any code
     is translated;
   - the changes of the scan and of the parsers, which can change matches,
     sizes and descriptions;
   - the fixes, such as the bounds and the overflow checks.
4. The names. The walker keeps the names of the signatures, which binwalk-ng
   keeps. The links of the documents to ReFirmLabs/binwalk change.
5. The decisions. D60 amends D49 and D59.

## The result

1. The walker follows binwalk-ng at `02835ae0f4`, of 2026-10-31. The three
   gates give no difference on 2301 inputs.
2. The scan is the scan of binwalk-ng, with the skip of runs of zero bytes.
   The output of an extractor is the Chroot of binwalk-ng: it cleans each
   path, follows the links that it holds, and writes relative links.
3. The walker has the 114 signatures, with `eva`, `program_store` and
   `dlink_fw`. It extracts `eva`, `program_store`, `dlink_fw`, `srecord`,
   `srecord_generic` and `dms` with the code of binwalk-ng.
4. It writes no new decoder (D60). It finds lzfse, rar and QNX IFS, and does
   not extract them. `docs/BACKLOG.md` lists what is left.
5. The ledger of binwalk-ng names each commit `ported` or `not-applicable`
   in the commit after the port, because a commit cannot hold its own hash.
