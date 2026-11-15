# D5. The API follows the libmagic C API

Status: Decided. Amended by D39.

Commit `7b14d3f` (2026-09-04). Commit `7d90219` (2026-09-04) added the
`-mime` flag to `cmd/file`.

`New` takes an `int` of flags. The exported constants mirror the `MAGIC_*`
constants and take their values from `magic.h`. `Buffer` and `File` return
the string that libmagic returns. An error from libmagic carries the name of
the C function and the text of `magic_error`.

`cmd/file` is an example. The commit message of `7d90219` calls it "cmd/file
example". It prints the type of each file that it gets, and `-mime` selects
`MimeType`.
