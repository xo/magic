# D29. The port keeps the license of file

Status: Decided.

The maintainers adopted this proposal on 2026-11-15, before the switch of
phase 4 (D47). The notices are in the code as the proposal says, and
`LICENSE.file` holds the license of file.

A translation keeps the license of its source. file is under a BSD license
with two conditions, in `COPYING` of the upstream:

1. A copy of the source keeps the copyright notice "at the beginning of the
   file, without modification", with the conditions and the disclaimer.
2. A binary reproduces the notice, the conditions and the disclaimer in its
   documentation.

The notice is "Copyright (c) Ian F. Darwin 1986, 1987, 1989, 1990, 1991,
1992, 1994, 1995. Software written by Ian F. Darwin and others; maintained
1994- Christos Zoulas." 13 files of the rules also carry a notice of their
own.

The proposal:

1. `LICENSE` in the root keeps the MIT license of magic (D8), for the code that
   is not a translation, such as the API and the tests.
2. `LICENSE.file` in the root holds the license of file, with its notice,
   because the root package holds most of the port.
3. Each Go file that translates a file of libmagic opens with the notice of
   that file, as the first condition asks, and then names the upstream file.
4. The tables made from the rules carry the notice of file, and the notices of
   the 13 rule files that have their own.
5. A package under `internal/` that holds a translation has the license in its
   folder, so that pkg.go.dev reads it.

The wording of condition 1 asks for the notice "at the beginning of the file".
Each Go file that translates a file of libmagic opens with the notice of that
file, written as Go comments, so the choice takes the strict reading. okage
D13 made the same choice for its BSD and MIT sources.

How it was done:

1. Each Go file opens with the notice of each C file that it translates,
   then a line that names those files, such as "This file translates
   src/is_json.c of file to Go, at the commit that
   docs/upstream/upstreams.tsv names (D29, D48)".
2. `LICENSE.file` in the root, and a `LICENSE` in `internal/engine`,
   `internal/rules` and each package of `compress/`, hold `COPYING` of file.
3. `internal/table/LICENSE` holds `COPYING` and the comments of the 21 rule
   files that name a copyright, with the name of each file.
