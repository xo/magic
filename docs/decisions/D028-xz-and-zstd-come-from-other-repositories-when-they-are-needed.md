# D28. xz and zstd come from other repositories when they are needed

Status: Decided. Amends D14.

The maintainers decided on 2026-11-15 that the port can use other
repositories for xz and zstd if they are necessary. The Go standard library
of Go 1.27 has gzip, zlib, flate and bzip2, and it has no xz and no zstd.

D14 proposed that the module has no Go dependencies. This decision makes an
exception for these two formats only. Any other dependency still needs the
approval of the maintainers.

They are necessary where the output depends on them. With `MAGIC_COMPRESS`,
libmagic decompresses a file and names what is inside it. Without a decoder,
the port names the file as compressed and stops, and its output differs from
libmagic. The database of D20 is also stored with zstd, and the port can store
its tables without zstd (open question 12).

The candidates, which the three models of the plan all named, are:

| Format | Package | License |
| --- | --- | --- |
| xz and lzma | `github.com/ulikunitz/xz` | BSD-3-Clause |
| zstd | `github.com/klauspost/compress/zstd` | BSD-3-Clause, with parts under Apache-2.0 and MIT |

The choice of package, its version and its license are checked when the code
needs it, and a decision records the choice. The licenses in this table come
from the models and are not checked.
