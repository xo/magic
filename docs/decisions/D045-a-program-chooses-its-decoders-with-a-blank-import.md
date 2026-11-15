# D45. A program chooses its decoders with a blank import

Status: Decided. Amends D28. Amends D38.

The maintainers decided on 2026-11-15 that the decoders of compressed data
are packages that register themselves, as the drivers of `database/sql` do.
A program chooses the formats that it decodes with a blank import:

```go
import (
	_ "github.com/xo/magic/compress/bzip2"
	_ "github.com/xo/magic/compress/zlib"
)
```

How it works:

1. The package `github.com/xo/magic/compress` holds the registry: `Register`,
   `Lookup`, the type `Decoder` and the methods `Gzip`, `Zlib`, `Bzip2`,
   `XZ`, `LZMA` and `Zstd`. It holds no decoder itself.
2. Each package of a decoder takes the place of one library of libmagic, and
   registers the formats that the library decodes. `compress/zlib` registers
   gzip and zlib, `compress/bzip2` registers bzip2, `compress/xz` registers xz
   and lzma, and `compress/zstd` registers zstd. The text of each error is
   the text that libmagic prints for its library.
3. `compress/zlib` and `compress/bzip2` use the standard library, and are in
   the root module (D27).
4. `compress/xz` and `compress/zstd` need `github.com/ulikunitz/xz` and
   `github.com/klauspost/compress` (D28). Each is a module of its own, so
   that the root module requires neither.
5. For a format with no decoder, the port answers as libmagic does when it
   was built without the library and finds no program that decodes it. For
   example, it prints ``ERROR:[zstd: Cannot posix_spawn `zstd', No such file
   or directory]``. The port runs no program (D36).

The reasons:

1. A program pays only for the formats that it decodes. Measured on
   2026-11-15 on linux/amd64, stripped: `compress/zlib` adds 45,056 bytes,
   `compress/bzip2` 73,728, `compress/xz` 442,368 and `compress/zstd`
   593,920.
2. The root module needs no outside module for xz and zstd.

D38 measures a program without decoders. A decoder that a program imports
counts against its own size, and the list above gives each one.

The decoders of Go do not name each fault of damaged data as the libraries
of C do. For example, zlib prints "invalid distance too far back", where the
port prints "data error". `TestPortMatchesTheOracle` counts these answers
apart and lists them. `docs/BACKLOG.md` holds the item.
