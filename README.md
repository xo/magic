# magic

`magic` is a Go package that identifies the type of a file or a buffer with
[libmagic](https://astron.com/pub/file/), the library of the `file` command.

The repository carries libmagic 5.48 as static archives, and the package
embeds the magic database. You do not install libmagic, and the package does
not read a magic file from your system. You need Go and a C compiler, because
the package uses cgo.

## Install

```bash
go get github.com/xo/magic
```

## Use

```go
m, err := magic.New(magic.MimeType)
if err != nil {
	return err
}
defer m.Close()

typ, err := m.File("photo.gif")
if err != nil {
	return err
}
fmt.Println(typ) // image/gif
```

`Buffer` identifies a byte slice in the same way. The flags are the `MAGIC_*`
flags of libmagic: `None` gives a description, `MimeType` gives a MIME type
and `Extension` gives the usual file extensions.

`FileTypes` and `BufferTypes` identify one input with several flags in one
call. `FileWith` and `BufferWith` use other flags for one call and do not
change the flags of the handle.

A handle is safe to share across goroutines. Each call holds a lock, so calls
on one handle run one at a time. If you want calls to run in parallel, give
each worker its own handle.

`cmd/file` is a small example program:

```bash
go run ./cmd/file -mime testdata/gif.gif
```

## Platforms

The repository has archives for these targets:

| GOOS | GOARCH |
| --- | --- |
| linux | amd64, arm64, arm (armv7) |
| darwin | amd64, arm64 |
| windows | amd64, arm64 |
| freebsd | amd64 |

A build for any other target fails.

Each platform is a Go module of its own, such as
`github.com/xo/magic/libmagic/linux_amd64/v5`. Go downloads the module of your
platform and no other. The Linux archives are built for musl and are static, so
a program links them under glibc and under musl.

These targets are tested less than the others. The archives for windows/arm64
and freebsd/amd64 link into a program, and nothing has run them yet. On
windows/arm64, only `zig cc` is tested as the C compiler.

## Documents

| Document | What it holds |
| --- | --- |
| `README.md` | this page, for a person who uses the package |
| `CONTRIBUTING.md` | how to change the package, rebuild the archives and install the agent skills |
| `AGENTS.md` | the rules for a coding agent. `CLAUDE.md` imports it |
| `docs/PLAN.md` | the plan, every decision and the open questions |
| `docs/BACKLOG.md` | the work that is known and not done |

## License

The code is under the MIT license. See `LICENSE`. The archives hold libmagic,
zlib, bzip2, xz and zstd, each under its own license. The Windows archives
also hold libgnurx, which is under LGPL 2.1.
