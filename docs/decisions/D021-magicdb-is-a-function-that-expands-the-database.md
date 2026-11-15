# D21. MagicDB is a function that expands the database

Status: Decided. Amends D3. Amended by D38.

The maintainers decided on 2026-11-15 that `MagicDB` stays in the API as a
function that returns the plain database, and that the release is `v0.4.0`.

`MagicDB` was an exported `[]byte` that held the plain database. D20 stores the
compressed bytes, so the plain bytes no longer exist in the program as a Go
value. Now `MagicDB() ([]byte, error)` expands the database and returns a new
slice of about 10 MiB on each call. `New` does not call it, so a program that
only identifies files never puts the database on the Go heap.

A program that read the variable no longer builds, and the error points at the
name. The fix is to call the function and check the error. That is a break of
the API, so the root takes a minor version.

No user is known. A search of GitHub code for `magic.MagicDB` gave no result,
which proves little, and no repository under `xo` on this host imports it.
The iv session, which asked for D20, checked its own Go files on 2026-11-15
and reports that it uses only `New`, `None`, `MimeType`, `Magic`, `Buffer`
and `BufferWith`.

The name stutters, as `magic.MagicDB`, which `go-pedantry` advises against. It
stays, so that a program that used the variable finds the function under the
same name. `TestMagicDB` holds the hash and that each call returns its own
slice.
