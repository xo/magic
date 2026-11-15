# D46. The port runs one walk for each form of a result

Status: Decided. Amends D42.

The maintainers decided on 2026-11-15, with the switch of phase 4, that a
`Result` is built from one walk of the engine for each of its forms: the
description, the MIME type, the charset, the extensions and the Apple code.

D42 chose one walk that records every form, and a walk of its own for a form
where one walk fails to give the answer of libmagic. A walk for each form
gives the answer of libmagic for every form by its construction, because
libmagic itself runs once for each flag:

1. With a MIME, extension or Apple flag, libmagic stops at the first entry
   that carries the annotation. With no such flag, it goes on, and prints the
   descriptions of every continuation.
2. The detectors print another text for each flag: JSON prints
   "JSON text data" or "application/json", and the reader of Compound
   Documents reads other properties for each.
3. The text test runs the rules for text a second time, and the charset comes
   from the encoding that `file_buffer` found.

One walk that records every form would have to copy each of these paths at
once. `TestPortMatchesTheOracle` checks each form against the oracle, so the
walk for each form is proven. A walk of a typical file takes far less than a
millisecond, so five walks cost little. Merging the walks is in
`docs/BACKLOG.md`.

Each entry of `Matches` holds one line of the description that `WithAll`
prints. libmagic prints a MIME type, an extension or an Apple code only for a
match whose rule has one, so these lines do not always pair with the lines of
the description. A match after the first gets them only when each form has as
many lines as the description.
