# D23. Each decision is a file of its own

Status: Decided. Amends D15.

dbmeta D111 says that a large project keeps each decision in a file of its
own under `docs/decisions/`, and that a small library keeps its decisions in
its plan. D15 kept the decisions of magic in `docs/PLAN.md`, because magic was
a small wrapper. The port makes it a large project: libmagic is 23,251 lines
of C, and its database is 28,161 lines of rules. okage made the same choice
for the same reason (okage D4).

Each decision is `docs/decisions/D<nnn>-<title>.md`, with the number in three
digits so that a listing sorts in order. The file opens with its number and
title, a blank line, and its status:

    # D22. magic becomes a pure Go port of libmagic

    Status: Decided. Amends D1. Amends D2.

[`README.md`](README.md) in this folder is the index. D1 to D21 were cut out
of `docs/PLAN.md` on 2026-11-15 with their text unchanged, except that the
wording that named a maintainer became impersonal (D32).

`TestTheDecisionIndexIsComplete`, `TestAnAmendmentPointsBothWays` and
`TestEveryDecisionReferenceExists` read this folder.
