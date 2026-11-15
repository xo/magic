# D10. A lookup can use other flags and leave the flags of the handle alone

Status: Decided. Amended by D47.

Commit `dbcd068` (2026-09-12).

`BufferWith` and `FileWith` identify with the flags that they get.
`BufferTypes` and `FileTypes` identify once for each flag set, under one
lock, so that every result describes the same input. `SetFlags` changes the
flags of the handle. The others do not. `TestFlagsRestored` holds this.

The handle remembers the flags that are on the cookie now, in `cur`, and
calls `magic_setflags` only when they change. The comment in `each` says that
this is "Purely an optimization".
