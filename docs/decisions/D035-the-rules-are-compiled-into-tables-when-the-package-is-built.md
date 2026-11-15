# D35. The rules are compiled into tables when the package is built

Status: Decided. Amends D27.

The maintainers decided on 2026-11-15 that the rules in `magic/Magdir` are
compiled into tables by `go generate`, and the tables are embedded. A program
never parses the text of the rules.

The reasons:

1. The text is 1.9 MB, and its reader in C is 3,726 lines. Parsing it in every
   program at start costs time, and the text costs bytes in every program
   (D38).
2. A build step can reject a rule that is wrong before a release, with a test.
3. The form of the tables is free. It does not copy the `.mgc` format of
   libmagic (D34), and it can be as small as D38 needs.

The generator and the tables are committed. The form of the tables is settled
by the first code of the port, measured against D38.

D27 left the form of the rules open, and this decision settles it.
