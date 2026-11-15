# D48. The port follows the commit of the ledger, and its bugs

Status: Decided. Amends D11. Amends D33.

The maintainers decided on 2026-11-15, with phase 5, that the port gives the
output of file at the commit that the ledger names, and not of the release
5.48 alone.

How it works:

1. `docs/upstream/upstreams.tsv` names the commit: the column `matched`, or
   `base` while `matched` is empty (D26). After phase 5, `matched` is the
   newest commit of upstream, `bbffb78d`, 90 commits after `FILE5_48`.
2. `test/oracle/build.sh` builds the oracle at that commit, and
   `go generate` in `internal/table` compiles the rules of that commit. CI
   checks that the table is current.
3. An expected value in a test is what libmagic reports at that commit, with
   the reference build of D33 (D11). The version of file at the commit is
   still 5.48, and `magic.FileVersion` says so.
4. The oracle is built without liblz4, as it is built without liblzip and
   liblrzip, because the port has no decoder for these formats (D45).

The port copies what file does on the reference, glibc on Linux, bugs
included, because the reference is the answer (D33, D34). Phase 5 found two:

1. Since `bcbb6064`, `cdf_read_property_info` checks the length of a section
   from its copy of the header on the stack, not from the stream. On Linux
   the stack is far above the heap, so the check always fails, and file reads
   no property of a Compound Document. It names a Word document "CDFV2
   Microsoft Word", not by its summary. The port does the same, with
   `followsStackCheck` in `internal/engine/cdf.go`.
2. Since `7fdb7bf9`, `file_fsmagic` prints each flag of the mode before its
   comma, so a file with setuid and setgid starts "setuidsetgid, ".

Both are worth a report to upstream. When upstream fixes one, the port takes
the fix with the commit that makes it.
