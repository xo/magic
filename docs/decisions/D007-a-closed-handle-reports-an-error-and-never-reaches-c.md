# D7. A closed handle reports an error and never reaches C

Status: Decided.

Commit `7b14d3f` (2026-09-04). Commit `dbcd068` (2026-09-12) extended it.

`Close` releases the handle and the copy of the database, and a second call
does nothing. A finalizer calls `Close` if the caller does not. The comment
calls it "Backstop only".

At first, `Buffer` and `File` returned an error on a closed handle.
`dbcd068` made every method return `errClosed`, and made `Close` take the
lock of the handle (D9). The comment on `Close` gives the reason: a method
that runs at the same time as `Close` reports a closed handle "rather than
touching a freed cookie". `TestClosed` holds this.

`Buffer` also refuses an empty buffer, because it passes `&buf[0]` to C.
`TestEmptyBuffer` holds this.
