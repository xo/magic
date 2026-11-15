# The API of the port

This document holds the design of the API of the pure Go port (D22). Phase 4
built it, in `magic.go` and `options.go` (D47). The code differs from the
proposal below in five places:

1. A `Result` comes from one walk of the engine for each form, not from one
   walk (D46).
2. An `Option` returns an error, so that `WithLimit` can refuse a limit that
   is not positive.
3. `WithDecompress` decodes the formats whose decoder the program imports
   from `compress/` (D45).
4. `ErrStopped` is the error of a detection whose context is done.
5. No `sync.Pool` reuses buffers yet, which answer 2 below expects.
   `docs/BACKLOG.md` holds the item.

The maintainers decided on 2026-11-15 (D40) that:

1. The API is new and clean, and need not stay compatible with the cgo
   package (D39).
2. Its high-level entry is `magic.Detect`.
3. The options of a detection are functional options, given to the call.
4. Each call that can read a file takes a `context.Context` first.
5. It uses the generics of Go 1.27 where they fit (D41).

The rest was proposed, reviewed, and settled by D42. "The answers" at the end
says how.

The design copies the behavior of libmagic (D34), not its C API. libmagic
chooses its output with a flag for each call. The port gives every kind of
output in one result, and options change how it detects, not what it returns.

## What a caller does

```go
r, err := magic.Detect(ctx, buf)
if err != nil {
	return err
}
fmt.Println(r.MIME)        // image/gif
fmt.Println(r.Description) // GIF image data, version 89a, 1375 x 825
fmt.Println(r.Extensions)  // [gif]
```

```go
r, err := magic.DetectFile(ctx, "archive.tar.gz", magic.WithDecompress())
```

```go
r, err := magic.DetectReader(ctx, req.Body, magic.WithLimit(1<<20))
```

## The proposal

```go
package magic

// Source is the content that Detect reads: bytes, or a string of bytes.
type Source interface {
	~[]byte | ~string
}

// Detect identifies src. Bytes that match nothing are "data", and no bytes
// are "empty". It returns an error when ctx is done or an option is not
// valid, such as a negative limit. It does not keep src, and does not change
// it.
func Detect[S Source](ctx context.Context, src S, opts ...Option) (Result, error)

// DetectReader reads from r up to the limit (WithLimit), and identifies what
// it read. A stream has no end that a rule can read relative to, so a rule
// that reads from the end of the file sees the end of what was read. Use
// DetectReaderAt when the size is known.
func DetectReader(ctx context.Context, r io.Reader, opts ...Option) (Result, error)

// DetectReaderAt identifies the size bytes that r holds. It reads at the
// offsets that the rules ask for, including offsets from the end, and reads at
// most the limit in all. An *os.File, a *bytes.Reader and a *strings.Reader are
// each an io.ReaderAt.
func DetectReaderAt(ctx context.Context, r io.ReaderAt, size int64, opts ...Option) (Result, error)

// DetectFile identifies the file at name. A directory, a link, a pipe or a
// device is named by its kind, as file names it. A file that cannot be
// opened is an error, which wraps the error of the os package.
func DetectFile(ctx context.Context, name string, opts ...Option) (Result, error)

// Result is what libmagic says of the content, in every form at once.
type Result struct {
	// Description is the text that file prints, such as
	// "GIF image data, version 89a, 1375 x 825".
	Description string

	// MIME is the MIME type, such as "image/gif".
	MIME string

	// Charset is the MIME encoding, such as "binary", "us-ascii" or "utf-8".
	Charset string

	// Extensions are the usual extensions without the dot, such as
	// ["jpeg" "jpg" "jpe" "jfif"], or nil when no rule gives one.
	Extensions []string

	// Apple is the creator and type code of classic Mac OS, such as
	// "8BIMGIFf", or "" when no rule gives one.
	Apple string

	// Matches holds every match when WithAll is set, the first one included.
	// Without WithAll it holds the one match that the fields above describe.
	Matches []Match
}

// Match is one match of the rules, or of a built-in test.
type Match struct {
	Description string
	MIME        string
	Extensions  []string
	Apple       string
}

// ContentType returns MIME and Charset in the form of an HTTP header, such
// as "image/gif; charset=binary".
func (r Result) ContentType() string

// String returns Description.
func (r Result) String() string

// Option changes how a call detects.
type Option func(*options)

// WithAll keeps every match, as MAGIC_CONTINUE does. Description then holds
// each match on a line of its own, with the prefix "- " that file uses.
func WithAll() Option

// WithDecompress identifies what is inside a compressed file, as
// MAGIC_COMPRESS does, for gzip, zlib, bzip2, xz and zstd (D27, D28). magic
// runs no outside program (D36).
func WithDecompress() Option

// WithLimit sets how many bytes are read and examined. The default is
// 7 MiB, as in libmagic.
func WithLimit(n int) Option

// WithRaw keeps characters that cannot be printed, as MAGIC_RAW does. By
// default they are written as octal escapes, as file writes them.
func WithRaw() Option

// WithFollowSymlinks makes DetectFile identify the target of a link, as
// MAGIC_SYMLINK does. By default a link is named as a link.
func WithFollowSymlinks() Option

// WithDevices makes DetectFile read a device, as MAGIC_DEVICES does.
func WithDevices() Option

// WithoutCheck turns off one built-in test, as a MAGIC_NO_CHECK_* flag does.
func WithoutCheck(c Check) Option

// Check is one built-in test.
type Check int

const (
	CheckCompress Check = iota
	CheckTar
	CheckRules
	CheckAppType
	CheckELF
	CheckText
	CheckCDF
	CheckCSV
	CheckTokens
	CheckEncoding
	CheckJSON
	CheckSIMH
)

// FileVersion is the release of file whose behavior the port copies.
const FileVersion = "5.48"
```

## Why each part is so

1. Package functions and no `Detector` type. The options are given to each
   call (D40), and the tables of the rules are shared and do not change
   (D35), so a type that holds options and tables adds nothing. A caller who
   uses the same options often keeps them in a slice:
   `opts := []magic.Option{magic.WithDecompress()}`. The cgo package needed a
   handle with a C cookie, a mutex and `Close` (D7, D9). The port needs none.
2. `Detect` is generic over `[]byte` and `string`, so a caller with either
   passes it without a copy. Inside, a string is read as bytes without a copy,
   and the engine is one function, not one for each type. Two instances of the
   whole engine cost bytes in every program (D38).
3. A reader and a file have functions of their own. A constraint cannot hold
   `io.Reader` beside `[]byte` (D41), and a file has more to say than its
   bytes: it can be a directory, a link or a device.
4. One `Result` with every form. libmagic runs once for each flag. The
   draft hoped to find the forms in one walk, because a rule carries its
   text, its MIME type and its extensions together. The code runs one walk
   for each form, which gives the answer of libmagic for each (D46).
5. A file that cannot be opened is an error, not a description. libmagic
   prints "cannot open" unless `MAGIC_ERROR` is set. Go returns an error.
6. `context.Context` is first in each call. The port checks it once for
   every 64 rules at the top level, so a caller can stop a detection that
   runs long on hostile bytes. It cannot interrupt a `Read` that blocks, and the port starts no
   goroutine to hide one.
7. The flags that only debug libmagic, `MAGIC_DEBUG` and `MAGIC_CHECK`, have no
   option. `MAGIC_PRESERVE_ATIME` has none: Go has no way to set the access
   time back that works on every system.
8. The limits of `magic_setparam`, other than the number of bytes, are fixed at
   the values of libmagic 5.48. They stop a hostile file from using too much
   time, and a caller has no need to change them.
9. No `MagicDB`, no numeric `Version` and no flags as numbers (D38, D39).

## What the binary walker needs

The walker comes after the port (D31). It needs the engine at an offset other
than 0, and the length of what it finds. Nothing in this API has to change for
that. The walker can add a function such as `magic.Walk`, which returns the
results with an offset each, possibly as an `iter.Seq2`.

## The reviews

On 2026-11-15 OpenAI Pro (`gpt-6-astra`) and Gemini Pro
(`gemini-3.1-pro-preview`) reviewed the draft. DeepSeek Pro timed out twice,
and gave no review. Each got the same draft, the decisions above, and the fact
from `softmagic.c` that the rules match the same way under every output flag,
and that with a MIME, extension or Apple flag the walk stops at the first
matched entry that carries that annotation.

Both agree on these, and the draft above takes them:

1. A caller cannot add rules at run time. A compiler of rules in every program
   breaks the size of D38.
2. An `io.Reader` cannot serve a rule that reads from the end of a file. The
   draft adds `DetectReaderAt`.
3. Document that the input is not kept or changed, and what is safe to share.
4. Generics stay out of the engine, because each instance of a generic engine
   costs bytes. Only the entry is generic, if anything is.

They disagree on these, and the questions below ask the maintainers:

1. The shape of `Result`. OpenAI Pro: one output for each call, chosen by an
   option such as `WithOutput(MIMEType)`, because a walk that goes on past
   the point where libmagic stops can meet a limit or an error that libmagic
   never meets. Gemini Pro: a `Result` whose methods each run the walk for
   their own output when called, so a caller who wants the MIME type pays only
   for it.
2. A `Detector` type. OpenAI Pro: none, because no resource or state needs
   one. Gemini Pro: a `Detector` to reuse buffers, with the package functions
   backed by a `sync.Pool`. A pool needs no exported type, and D40 puts the
   options in each call.
3. `context.Context` on `Detect` of bytes. OpenAI Pro keeps it. Gemini Pro
   drops it, because no file is read. D40 asks for it on each call that can
   read a file, which `Detect` of bytes cannot.
4. The generic `Source`. OpenAI Pro keeps it as a thin entry, and warns that a
   type switch on `[]byte` and `string` misses named types such as
   `json.RawMessage`. Gemini Pro drops it for plain `[]byte`.
5. `Matches`. OpenAI Pro: remove it until it is clear what one match is,
   because the continuations of one rule make one line. Gemini Pro: return
   them as an `iter.Seq[Match]`.
6. `WithoutCheck`. Gemini Pro wants a set of bits, so that one option turns off
   several tests. As a functional option, one call for each test does the same.

OpenAI Pro also asks for a table of what `DetectFile` says for each kind of
file: a link that leads nowhere, a directory, a pipe, a socket, a device, a
file that cannot be read, and an empty file. libmagic names each, and the port
must name each the same way.

## The answers

The maintainers took the recommendation on each of the five questions that the
reviews left open (D42):

1. One walk records the text of each match and the first annotation of each
   kind. The differential test (D30) proves it for each output, and an output
   where it fails gets a walk of its own.
2. No `Detector` type. A `sync.Pool` inside the package reuses the buffers.
3. A `context.Context` on every entry, `Detect` of bytes included.
4. The generic `Source`, measured against D38 when the first code exists.
5. `Matches` is a slice, with one entry for each line that `WithAll` prints.

No question of the API is open.
