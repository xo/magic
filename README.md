<div align="center">
  <img src="docs/logo/magic.svg" alt="magic" width="480"/>
  <br/>
  <a href="#install" title="Install">Install</a> |
  <a href="#use" title="Use">Use</a> |
  <a href="#versions" title="Versions">Versions</a> |
  <a href="#documents" title="Documents">Documents</a> |
  <a href="#related-projects" title="Related Projects">Related Projects</a> |
  <a href="https://pkg.go.dev/github.com/xo/magic" title="Go Reference">Reference</a> |
  <a href="https://github.com/xo/magic/releases" title="Releases">Releases</a> |
  <a href="CONTRIBUTING.md" title="Contributing">Contributing</a>
</div>

<br/>

[![Unit Tests][magic-ci-status]][magic-ci]
[![Go Reference][goref-magic-status]][goref-magic]
[![Releases][release-status]][releases]
[![Discord Discussion][discord-status]][discord]

[magic-ci]: https://github.com/xo/magic/actions/workflows/test.yml "Test CI"
[magic-ci-status]: https://github.com/xo/magic/actions/workflows/test.yml/badge.svg "Test CI"
[goref-magic]: https://pkg.go.dev/github.com/xo/magic "Go Reference"
[goref-magic-status]: https://pkg.go.dev/badge/github.com/xo/magic.svg "Go Reference"
[release-status]: https://img.shields.io/github/v/release/xo/magic?display_name=tag "Latest Release"
[releases]: https://github.com/xo/magic/releases "Releases"
[discord]: https://discord.gg/WDWAgXwJqN "Discord Discussion"
[discord-status]: https://img.shields.io/discord/829150509658013727.svg?label=Discord&logo=Discord&colorB=7289da&style=flat-square "Discord Discussion"

# magic

`magic` is a Go package that identifies the type of a file or of bytes, as
the `file` command does. It is a port of
[libmagic](https://astron.com/pub/file/), the library of `file`, to pure Go.

It gives the output of libmagic 5.48, with the commits of `file` after the
release, to the commit in `docs/upstream/upstreams.tsv`: the same
description, MIME type, charset, extensions and Apple code. The rules of
`file` are compiled into the package. You need only Go 1.27 or later. You do
not need a C compiler or libmagic, and a program that imports magic builds
for every target of Go.

## Install

```bash
go get github.com/xo/magic
```

## Use

`Detect` identifies bytes or a string:

```go
r, err := magic.Detect(ctx, buf)
if err != nil {
	return err
}
fmt.Println(r.Description)   // GIF image data, version 89a, 1375 x 825
fmt.Println(r.MIME)          // image/gif
fmt.Println(r.ContentType()) // image/gif; charset=binary
fmt.Println(r.Extensions)    // [gif]
```

A `Result` holds every form of the answer at once:

| Field | What it holds |
| --- | --- |
| `Description` | the text that `file` prints |
| `MIME` | the MIME type, such as `image/gif` |
| `Charset` | the MIME encoding, such as `binary` or `utf-8` |
| `Extensions` | the usual extensions, without the dot, or nil |
| `Apple` | the creator and type code of classic Mac OS, or "" |
| `Matches` | each match, when `WithAll` is set, and else the one match |

`DetectFile` identifies a file. A directory, a link, a pipe or a device is
named by its kind, as `file` names it. A file that cannot be found or opened
is an error that wraps the error of the `os` package:

```go
r, err := magic.DetectFile(ctx, "photo.jpg")
```

`DetectReader` reads from an `io.Reader`, and `DetectReaderAt` reads from an
`io.ReaderAt` of a known size. A rule can read from the end of a file, so
`DetectReaderAt` gives the same answer as a file, and `DetectReader` does
not when the data is longer than what it reads.

Each function takes a `context.Context` first. A detection stops between the
rules when the context is done, and its error wraps `magic.ErrStopped` and
the error of the context. Every function is safe to call from many
goroutines at once.

### Options

The options change how a call detects:

| Option | What it does | Flag of libmagic |
| --- | --- | --- |
| `WithAll()` | keeps every match, one on each line of `Description`, and one in each entry of `Matches` | `MAGIC_CONTINUE` |
| `WithDecompress()` | identifies what is inside compressed data | `MAGIC_COMPRESS` |
| `WithLimit(n)` | reads and examines n bytes. The default is 7 MiB | `bytes_max` |
| `WithRaw()` | keeps the characters that cannot be printed, which are octal escapes by default | `MAGIC_RAW` |
| `WithFollowSymlinks()` | identifies the target of a link | `MAGIC_SYMLINK` |
| `WithDevices()` | reads a device | `MAGIC_DEVICES` |
| `WithoutCheck(c)` | turns off one built-in test, such as `CheckJSON` | `MAGIC_NO_CHECK_*` |

### Compressed data

`WithDecompress` decodes a format only when the program imports its
decoder, as a program imports the driver of `database/sql`. Each decoder
adds to the size of the program, so a program imports the ones it needs:

```go
import (
	_ "github.com/xo/magic/compress/bzip2" // bzip2
	_ "github.com/xo/magic/compress/xz"    // xz and lzma
	_ "github.com/xo/magic/compress/zlib"  // gzip and zlib
	_ "github.com/xo/magic/compress/zstd"  // zstd
)
```

`compress/xz` and `compress/zstd` are Go modules of their own, because they
need other modules. Get them with `go get` as well. For a format with no
decoder, magic answers as libmagic does when it was built without the
library and finds no program that decodes it. magic never runs a program.

Five more packages decode formats of books, help files and disk images.
`WithDecompress` does not use them, because libmagic does not decode these
formats (D61, D63):
`compress/palmdoc` decodes the text of PalmDOC and MOBI books,
`compress/huffcdic` the Huffman compression of Mobipocket, and `compress/lzx`
the LZX of CAB and CHM files, `compress/lzfse` the LZFSE of Apple, and
`compress/adc` the ADC of older DMG files.

## Read a Compound File

The package `cfb` reads Compound Files, the container of Word, Excel and
PowerPoint files before 2007, of Outlook messages, of Windows Installer
packages and of encrypted Office documents. It reads the tree of storages
and streams, each stream, and the property sets that hold the title, the
author and the thumbnail of a document. magic does not import it (D62):

```go
f, err := cfb.Open(r, size, cfb.Limits{})
if err != nil {
	return err
}
b, err := f.ReadFile("\x05SummaryInformation")
if err != nil {
	return err
}
ps, err := cfb.ParsePropertySet(b)
if err != nil {
	return err
}
if thumb, ok := ps.Thumbnail(); ok && thumb.Format == cfb.CFEnhMetafile {
	// thumb.Data holds an EMF image.
}
```

## Walk a firmware image

The package `walk` finds the files that are embedded in a binary file, such
as a firmware image, as binwalk does, and extracts them. It follows
[binwalk-ng](https://github.com/binwalk-ng/binwalk-ng), the fork of binwalk
that is maintained. It has the 114 signatures of binwalk-ng, with the same
names and descriptions, and gives the same matches, at the commit of
binwalk-ng that `docs/upstream/upstreams.tsv` names:

```go
for e, err := range walk.WalkFile(ctx, "firmware.bin", walk.WithDepth(8)) {
	if err != nil {
		return err
	}
	fmt.Printf("%*s%#x %s\n", 2*e.Depth, "", e.Offset, e.Description)
}
```

A program imports the formats that it needs, or all of them:

```go
import _ "github.com/xo/magic/walk/format/all"
```

`Entry.Members` gives the files of a match in memory, and `walk.Extract`
writes them under an `os.Root`. binwalk extracts many formats with programs,
such as 7-Zip, sasquatch and tsk_recover. The walker runs no program, and
writes what those programs write. The formats of xz, lzma and zstd need the
decoders of `compress/xz` and `compress/zstd`, and the keys of D-Link firmware
are in `walk/format/encrypted`. `docs/WALKER.md` has the design, and
`cmd/walk` is an example program.

The formats build only where an `int` has 64 bits, as binwalk does.

## Size

A program that prints the MIME type of a file grows by about 980 KB with
magic, stripped, on linux/amd64. Most of it is the compiled rules. The
decoders add 45 KB for `zlib`, 74 KB for `bzip2`, 442 KB for `xz` and 594 KB
for `zstd`.

## How it is tested

The test module builds libmagic from the source of `file`, at the commit
that the port follows, and compares the package with it, on 2,471 files and
damaged copies of them, with 14 combinations of flags, as bytes and as files.
Every answer is the same, except 50 where a decoder of Go reads damaged
compressed data in another way than the library of C. All 113 tests of
`file` pass. `CONTRIBUTING.md` says how to run the tests.

## Versions

Up to `v0.4.1`, magic wrapped libmagic with cgo, and carried static archives
of it for eight targets. The port is the next minor version, and its API is
new (D39). `docs/API.md` gives the reasons.

To move from `v0.4.1`:

| `v0.4.1` | The port |
| --- | --- |
| `m, err := magic.New(magic.MimeType)`, then `m.Buffer(buf)` | `r, err := magic.Detect(ctx, buf)`, then `r.MIME` |
| `m.File(name)` | `magic.DetectFile(ctx, name)` |
| `magic.None` | `r.Description` |
| `magic.MimeEncoding` | `r.Charset` |
| `magic.Mime` | `r.ContentType()` |
| `magic.Extension` | `r.Extensions` |
| `magic.Apple` | `r.Apple` |
| `magic.Continue` | `magic.WithAll()` |
| `magic.Compress` | `magic.WithDecompress()`, and a decoder from `compress/` |
| `m.Close()` | nothing. The port has no handle |
| `magic.Version()` | `magic.FileVersion` |
| `magic.MagicDB()` | nothing. The port has no `.mgc` (D47) |

A file that cannot be opened is an error now, where libmagic gave the text
"cannot open".

## Documents

| Document | What it holds |
| --- | --- |
| `README.md` | this page, for a person who uses the package |
| `CONTRIBUTING.md` | how to change the package and test it |
| `AGENTS.md` | the rules for a coding agent. `CLAUDE.md` imports it |
| `docs/PLAN.md` | the plan of the port and its phases |
| `docs/API.md` | the design of the API |
| `docs/WALKER.md` | the design of the binary walker, and the formats of binwalk |
| `docs/decisions/` | every decision, one file each, with an index |
| `docs/UPSTREAM.md` | the rules for following `file`, the upstream of the port |
| `docs/BACKLOG.md` | the work that is known and not done, and the binary walker |

## Related projects

`magic` is one of the `xo` projects. Several of them, as `magic` does, port a
library of C or Rust to pure Go. Each one is a separate repository:

| Project | What it is |
| --- | --- |
| [okage](https://github.com/xo/okage) | A Go port of tiny-skia and resvg, which draws 2D graphics and SVG images |
| [transit](https://github.com/xo/transit) | A Go port of tree-sitter, a parser of source code |
| [rline](https://github.com/xo/rline) | A readline package for Go, which reads a line of text that a person edits. It started as a port of isocline |
| [zuzu](https://github.com/xo/zuzu) | Draws DOT graphs, Mermaid diagrams, and Vega and Vega-Lite charts |
| [ox](https://github.com/xo/ox) | Parses the flags and arguments of a command line |
| [usql](https://github.com/xo/usql) | A command line client for SQL and NoSQL databases |

[iv](https://github.com/kenshaw/iv), an image viewer for the terminal, uses
`magic` to find the type of each file that it shows.

## License

The code of magic that is not a translation, such as the API and the tests,
is under the MIT license, in `LICENSE`. The port translates `file`, which is
under its own BSD license, in `LICENSE.file`. Each translated Go file opens
with the notice of the file of `file` that it translates, and the compiled
rules carry the notices of `file` and of its rule files, in
`internal/table/LICENSE`. The binary walker in `walk/` translates binwalk-ng, which is under the MIT license, in `LICENSE.binwalk`,
and holds copies of two decoders of the Go standard library, under the BSD
license of Go, in `walk/internal/flate/LICENSE` and
`walk/internal/bzip2/LICENSE`.
A program that ships magic must reproduce these notices in its
documentation.
