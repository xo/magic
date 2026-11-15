# D3. The magic database is embedded, and one copy serves every target

Status: Decided. Amended by D20. Amended by D21.

Commit `7b14d3f` (2026-09-04).

`//go:embed libmagic/magic.mgc` puts the database in the Go binary as
`MagicDB`. `New` copies it into C memory once for each handle and loads it
with `magic_load_buffers`. The package never reads a magic file from the
system.

The comment in `New` gives the reason for the copy. `magic_load_buffers`
keeps the pointer for the life of the handle, and it can byte-swap the
contents in place. A Go pointer cannot be kept past the call, and the
embedded bytes can be in a read-only section.

`libmagic/magic.h` and `libmagic/magic.mgc` are shared by all targets. The
comment on `MagicDB` says that the format does not depend on byte order.
`share_once` in the script prints a warning if one target builds a different
file.
