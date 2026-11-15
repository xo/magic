# D49. The walker follows binwalk 3, and its names

Status: Decided. Amended by D58.

The maintainers decided on 2026-11-15 that the binary walker follows binwalk
3, as the port follows file. This answers questions 17 and 19 of
`docs/PLAN.md`.

How it works:

1. binwalk gets a row in `docs/upstream/upstreams.tsv` and a ledger of its
   commits (D25, D26). The walker gives the result of binwalk at the commit
   that the ledger names.
2. The test module builds binwalk from its source at that commit, as it
   builds the oracle of libmagic. CI installs the toolchain of Rust for it.
   The package itself does not need Rust.
3. The oracle runs binwalk with `-l` and compares its JSON with the entries
   of `walk.Walk`: the offset, the name, the size, the confidence and the
   description of each match.
4. The walker takes the names and the descriptions of binwalk as they are,
   so that the oracle compares them as text.

binwalk 3 is under the MIT license. Each Go file that translates a file of
binwalk opens with its notice, and names it, as D29 says for file.

binwalk extracts many formats with outside programs, which magic does not
run (D36). For those formats, the oracle compares the matches and not the
extracted files.
