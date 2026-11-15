// Package magic identifies the type of a file or of bytes, as the file
// command does. It is a port of libmagic, the library of file, to pure Go
// (D22). It gives the output of libmagic 5.48 and the commits of file after
// it (D48), and needs no C compiler.
//
//	r, err := magic.Detect(ctx, buf)
//	if err != nil {
//		return err
//	}
//	fmt.Println(r.MIME)        // image/gif
//	fmt.Println(r.Description) // GIF image data, version 89a, 1375 x 825
//	fmt.Println(r.Extensions)  // [gif]
//
// The rules of file are compiled into the package (D35). A caller cannot add
// rules. Every function is safe to call from many goroutines at once.
package magic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"sync"
	"unsafe"

	"github.com/xo/magic/internal/engine"
	"github.com/xo/magic/internal/table"
)

// FileVersion is the version of file whose behavior the package copies. The
// package follows the commits of file after the release, to the commit that
// docs/upstream/upstreams.tsv names (D48), and file still calls that 5.48.
const FileVersion = "5.48"

// Source is the content that Detect reads: bytes, or a string of bytes.
type Source interface {
	~[]byte | ~string
}

// Result is what libmagic says of the content, in every form at once.
type Result struct {
	// Description is the text that file prints, such as
	// "GIF image data, version 89a, 1375 x 825". With WithAll, it holds each
	// match on a line of its own, after the first with the prefix "- ".
	Description string

	// MIME is the MIME type, such as "image/gif".
	MIME string

	// Charset is the MIME encoding, such as "binary", "us-ascii" or
	// "utf-8".
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

// Match is one match of the rules, or of a built-in test: one line that
// file prints with its option --keep-going.
type Match struct {
	Description string
	// MIME, Extensions and Apple are set for a match after the first only
	// when libmagic gives them for each match, as it does when every match
	// has one.
	MIME       string
	Extensions []string
	Apple      string
}

// ContentType returns MIME and Charset in the form of an HTTP header, such
// as "image/gif; charset=binary".
func (r Result) ContentType() string {
	if r.Charset == "" {
		return r.MIME
	}
	return r.MIME + "; charset=" + r.Charset
}

// String returns Description.
func (r Result) String() string {
	return r.Description
}

// load returns the engine with the compiled rules. It builds the engine
// once, because the engine holds about 10 MB of rules and the regular
// expressions that it compiles as it meets them. The engine is safe to share.
var load = sync.OnceValues(func() (*engine.Engine, error) {
	db, err := table.Load()
	if err != nil {
		return nil, fmt.Errorf("magic: loading the rules: %w", err)
	}
	return engine.New(db), nil
})

// bytesOf returns the bytes of src without a copy. The engine reads them
// and does not change them.
func bytesOf[S Source](src S) []byte {
	v := reflect.ValueOf(src)
	if v.Kind() == reflect.String {
		s := v.String()
		return unsafe.Slice(unsafe.StringData(s), len(s))
	}
	return v.Bytes()
}

// Detect identifies src. Bytes that match nothing are "data", and no bytes
// are "empty". It returns an error when ctx is done or an option is not
// valid, such as a negative limit. It does not keep src, and does not change
// it.
func Detect[S Source](ctx context.Context, src S, opts ...Option) (Result, error) {
	o, err := apply(opts)
	if err != nil {
		return Result{}, err
	}
	e, err := load()
	if err != nil {
		return Result{}, err
	}
	data := bytesOf(src)
	return o.detect(func(c engine.Call) (string, error) {
		return e.BufferContext(ctx, data, c)
	})
}

// DetectReader reads from r up to the limit (WithLimit), and identifies what
// it read. A stream has no end that a rule can read relative to, so a rule
// that reads from the end of the file sees the end of what was read. Use
// DetectReaderAt when the size is known.
func DetectReader(ctx context.Context, r io.Reader, opts ...Option) (Result, error) {
	o, err := apply(opts)
	if err != nil {
		return Result{}, err
	}
	data, err := io.ReadAll(io.LimitReader(r, int64(o.limit())))
	if err != nil {
		return Result{}, fmt.Errorf("magic: reading: %w", err)
	}
	return Detect(ctx, data, opts...)
}

// DetectReaderAt identifies the size bytes that r holds. It reads at the
// offsets that the rules ask for, including offsets from the end, and reads at
// most the limit from the start and the limit from the end. An *os.File, a
// *bytes.Reader and a *strings.Reader are each an io.ReaderAt.
func DetectReaderAt(ctx context.Context, r io.ReaderAt, size int64, opts ...Option) (Result, error) {
	o, err := apply(opts)
	if err != nil {
		return Result{}, err
	}
	if size < 0 {
		return Result{}, fmt.Errorf("magic: the size %d is negative", size)
	}
	e, err := load()
	if err != nil {
		return Result{}, err
	}
	return o.detect(func(c engine.Call) (string, error) {
		return e.ReaderAtContext(ctx, r, size, c)
	})
}

// DetectFile identifies the file at name. A directory, a link, a pipe or a
// device is named by its kind, as file names it. A file that cannot be found
// or opened is an error, which wraps the error of the os package.
func DetectFile(ctx context.Context, name string, opts ...Option) (Result, error) {
	o, err := apply(opts)
	if err != nil {
		return Result{}, err
	}
	stat := os.Lstat
	if o.flags&engine.Symlink != 0 {
		stat = os.Stat
	}
	fi, err := stat(name)
	if err != nil {
		return Result{}, fmt.Errorf("magic: %w", err)
	}
	if fi.Mode().IsRegular() {
		f, err := os.Open(name)
		if err != nil {
			return Result{}, fmt.Errorf("magic: %w", err)
		}
		f.Close()
	}
	e, err := load()
	if err != nil {
		return Result{}, err
	}
	// libmagic names a directory, a link, a pipe or a device by its kind. It
	// has no extension or Apple code for one, and with those flags it reads
	// the file as data, which fails for a directory.
	o.kindOnly = !fi.Mode().IsRegular()
	return o.detect(func(c engine.Call) (string, error) {
		return e.FileContext(ctx, name, c)
	})
}

// ErrStopped is the error of a detection that stopped because its context
// is done. The error also wraps the error of the context.
var ErrStopped = errors.New("magic: the detection stopped")

// wrap gives the error of the engine the prefix of the package.
func wrap(err error) error {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("%w: %w", ErrStopped, err)
	}
	return fmt.Errorf("magic: %w", err)
}

// detect runs the engine for each form of the result. libmagic finds each
// form with a walk of its own, and so does the port, so that each form is
// the one of libmagic (D46).
func (o *options) detect(run func(engine.Call) (string, error)) (Result, error) {
	call := func(f engine.Flags) ([]string, error) {
		f |= o.flags
		if o.all {
			f |= engine.Continue
		}
		out, err := run(engine.Call{Flags: f, Limit: o.limit()})
		if err != nil {
			return nil, wrap(err)
		}
		if !o.all {
			return []string{out}, nil
		}
		return o.split(out), nil
	}
	desc, err := call(engine.None)
	if err != nil {
		return Result{}, err
	}
	mime, err := call(engine.MimeType)
	if err != nil {
		return Result{}, err
	}
	ext, apple := []string{""}, []string{""}
	if !o.kindOnly {
		if ext, err = call(engine.Extension); err != nil {
			return Result{}, err
		}
		if apple, err = call(engine.Apple); err != nil {
			return Result{}, err
		}
	}
	charset, err := run(engine.Call{Flags: o.flags | engine.MimeEncoding, Limit: o.limit()})
	if err != nil {
		return Result{}, wrap(err)
	}
	sep := "\n- "
	r := Result{
		Description: strings.Join(desc, sep),
		MIME:        mime[0],
		Charset:     charset,
		Extensions:  extensions(ext[0]),
		Apple:       appleCode(apple[0]),
	}
	r.Matches = make([]Match, len(desc))
	for i, d := range desc {
		r.Matches[i].Description = d
		if len(mime) == len(desc) {
			r.Matches[i].MIME = mime[i]
		}
		if len(ext) == len(desc) {
			r.Matches[i].Extensions = extensions(ext[i])
		}
		if len(apple) == len(desc) {
			r.Matches[i].Apple = appleCode(apple[i])
		}
	}
	r.Matches[0].MIME, r.Matches[0].Extensions, r.Matches[0].Apple = r.MIME, r.Extensions, r.Apple
	return r, nil
}

// split cuts the output of a detection with WithAll into its matches. file
// separates them with a newline and "- ", which it writes as an octal escape
// unless WithRaw is set.
func (o *options) split(out string) []string {
	sep := `\012- `
	if o.flags&engine.Raw != 0 {
		sep = "\n- "
	}
	return strings.Split(out, sep)
}

// extensions splits the extensions that libmagic joins with "/". "???" is
// what libmagic prints when no rule gives one.
func extensions(s string) []string {
	if s == "" || s == "???" {
		return nil
	}
	return strings.Split(s, "/")
}

// appleCode returns the Apple code, or "" for "UNKNUNKN", which libmagic
// prints when no rule gives one.
func appleCode(s string) string {
	if s == "UNKNUNKN" {
		return ""
	}
	return s
}
