// Package walk finds the files that are embedded in a binary file, such as a
// firmware image, as binwalk does (D31, D49, D50).
//
// The walker gives the matches of binwalk 3, at the commit that
// docs/upstream/upstreams.tsv names, with the names and the descriptions of
// binwalk. A program imports the formats that it needs, as it imports the
// decoders of compress:
//
//	import _ "github.com/xo/magic/walk/format/image"
//
// The package github.com/xo/magic/walk/format/all imports every format.
//
// The formats build only on a system where an int has 64 bits, as binwalk
// does. A parser of binwalk keeps each value in a usize of 64 bits, and a
// value of 32 bits changes some answers. On another system, this package
// builds and finds no format.
package walk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"iter"
	"os"
	"slices"
	"strings"
)

// Entry is one match of the walk.
type Entry struct {
	Format      string     // the name of the signature, as binwalk names it, such as "squashfs"
	Description string     // the text of the parser
	Confidence  Confidence // Low, Medium or High
	Offset      int64      // the offset of the match in its input
	Size        int64      // the bytes that the match covers in its input
	SizeKnown   bool       // false when the match ends at the next match or at the end
	Parent      *Entry     // the match whose member holds this one, or nil
	Member      string     // the name of that member in its parent, or ""
	Depth       int        // 0 for the input itself

	data  []byte // the input of the entry
	sig   *Signature
	match Match
	limit int64 // the limit of WithMaxOutput
	skip  bool
}

// Section returns the bytes of the match in its input.
func (e *Entry) Section() *io.SectionReader {
	b := e.data[e.Offset : e.Offset+e.Size]
	return io.NewSectionReader(readerAt(b), 0, int64(len(b)))
}

// SkipMembers tells Walk not to walk the members of this match.
func (e *Entry) SkipMembers() {
	e.skip = true
}

// readerAt reads a slice of bytes.
type readerAt []byte

func (r readerAt) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("walk: negative offset")
	}
	if off >= int64(len(r)) {
		return 0, io.EOF
	}
	n := copy(p, r[off:])
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

// Option sets an option of a walk.
type Option func(*options) error

type options struct {
	include, exclude []string
	all              bool
	maxInput         int64
	maxOutput        int64
	depth            int
}

// The defaults of the limits.
const (
	DefaultMaxInput  = 4 << 30 // the most bytes that Walk reads into memory, 4 GiB
	DefaultMaxOutput = 1 << 30 // the most bytes that one extraction writes, 1 GiB
)

// WithDepth walks the members of each match to depth n, as the option -M of
// binwalk, which has no limit. A member is a regular file that is not empty,
// of a match that the walker can extract and whose format does not stop the
// walk. The default is 0, which walks the input only.
func WithDepth(n int) Option {
	return func(o *options) error {
		if n < 0 {
			return fmt.Errorf("walk: WithDepth(%d): the depth must not be negative", n)
		}
		o.depth = n
		return nil
	}
}

// WithMaxOutput sets the most bytes that one extraction writes, in memory
// for the walk of members and Entry.Members, and on disk for Extract. An
// extraction that reaches it fails. A compression bomb stops there.
func WithMaxOutput(n int64) Option {
	return func(o *options) error {
		if n <= 0 {
			return fmt.Errorf("walk: WithMaxOutput(%d): the limit must be above 0", n)
		}
		o.maxOutput = n
		return nil
	}
}

// WithFormats walks for these formats only, by their names in binwalk, as the
// option -y of binwalk.
func WithFormats(names ...string) Option {
	return func(o *options) error {
		o.include = append(o.include, names...)
		return nil
	}
}

// WithoutFormats walks for every format but these, as the option -x of
// binwalk.
func WithoutFormats(names ...string) Option {
	return func(o *options) error {
		o.exclude = append(o.exclude, names...)
		return nil
	}
}

// WithSearchAll tries the short signatures at every offset, as the option -a
// of binwalk.
func WithSearchAll() Option {
	return func(o *options) error {
		o.all = true
		return nil
	}
}

// WithMaxInput sets the most bytes that Walk reads into memory. A larger
// input is an error. WalkFile maps a file into memory where the system can,
// and the limit holds only where it cannot.
func WithMaxInput(n int64) Option {
	return func(o *options) error {
		if n <= 0 {
			return fmt.Errorf("walk: WithMaxInput(%d): the limit must be above 0", n)
		}
		o.maxInput = n
		return nil
	}
}

func newOptions(opts []Option) (*options, error) {
	o := &options{maxInput: DefaultMaxInput, maxOutput: DefaultMaxOutput}
	for _, opt := range opts {
		if err := opt(o); err != nil {
			return nil, err
		}
	}
	return o, nil
}

// included is include_signature of src/binwalk.rs: a signature is in the
// walk when it is in the list of WithFormats, or there is no such list, and
// it is not in the list of WithoutFormats. Names match without regard to
// case.
func (o *options) included(name string) bool {
	match := func(s string) bool { return strings.EqualFold(s, name) }
	if len(o.include) > 0 {
		return slices.ContainsFunc(o.include, match)
	}
	return !slices.ContainsFunc(o.exclude, match)
}

// scanner returns the scanner of the formats of o.
func (o *options) scanner() (*scanner, error) {
	var names []string
	var sigs []*Signature
	for _, s := range Signatures() {
		if o.included(s.Name) {
			sigs = append(sigs, s)
			names = append(names, s.Name)
		}
	}
	if len(sigs) == 0 {
		return nil, errors.New("walk: no format is registered or included. Import a package of github.com/xo/magic/walk/format")
	}
	key := fmt.Sprint(o.all, strings.Join(names, ","))
	if sc, ok := scanners.Load(key); ok {
		return sc.(*scanner), nil
	}
	sc, _ := scanners.LoadOrStore(key, newScanner(sigs, o.all))
	return sc.(*scanner), nil
}

// Walk finds the files that are embedded in r, in the order of their offsets.
// It reads the size bytes of r into memory first, up to the limit of
// WithMaxInput.
//
// A caller that stops the loop stops the walk. The walk stops with an error
// when ctx is done.
func Walk(ctx context.Context, r io.ReaderAt, size int64, opts ...Option) iter.Seq2[*Entry, error] {
	return func(yield func(*Entry, error) bool) {
		o, err := newOptions(opts)
		if err != nil {
			yield(nil, err)
			return
		}
		if size < 0 || size > o.maxInput {
			yield(nil, fmt.Errorf("walk: the input of %d bytes is above the limit of %d bytes", size, o.maxInput))
			return
		}
		data := make([]byte, size)
		if _, err := r.ReadAt(data, 0); err != nil && !errors.Is(err, io.EOF) {
			yield(nil, fmt.Errorf("walk: reading the input: %w", err))
			return
		}
		walkData(ctx, data, o, yield)
	}
}

// WalkBytes finds the files that are embedded in data. The entries refer to
// data, which the caller must not change during the walk.
func WalkBytes(ctx context.Context, data []byte, opts ...Option) iter.Seq2[*Entry, error] { //nolint:revive // D50 names the API after Walk
	return func(yield func(*Entry, error) bool) {
		o, err := newOptions(opts)
		if err != nil {
			yield(nil, err)
			return
		}
		walkData(ctx, data, o, yield)
	}
}

// WalkFile opens name and walks it. Where the system can, it maps the file
// into memory, and does not read it.
func WalkFile(ctx context.Context, name string, opts ...Option) iter.Seq2[*Entry, error] { //nolint:revive // D50 names the API after Walk
	return func(yield func(*Entry, error) bool) {
		o, err := newOptions(opts)
		if err != nil {
			yield(nil, err)
			return
		}
		f, err := os.Open(name)
		if err != nil {
			yield(nil, fmt.Errorf("walk: %w", err))
			return
		}
		defer func() { _ = f.Close() }()
		data, unmap, err := mapFile(f, o.maxInput)
		if err != nil {
			yield(nil, fmt.Errorf("walk: %w", err))
			return
		}
		defer unmap()
		walkData(ctx, data, o, yield)
	}
}

// walkData scans data and yields its entries, and walks the members of
// each one to the depth of o.
func walkData(ctx context.Context, data []byte, o *options, yield func(*Entry, error) bool) {
	sc, err := o.scanner()
	if err != nil {
		yield(nil, err)
		return
	}
	walkInput(ctx, sc, data, o, nil, "", yield)
}

// walkInput walks one input: the file, or a member of a match at depth.
// It returns false when the caller stopped the walk.
func walkInput(ctx context.Context, sc *scanner, data []byte, o *options, parent *Entry, member string, yield func(*Entry, error) bool) bool {
	depth := 0
	if parent != nil {
		depth = parent.Depth + 1
	}
	matches, sigs, known, err := sc.scan(ctx, data)
	if err != nil {
		return yield(nil, fmt.Errorf("walk: stopped: %w", err))
	}
	for i, m := range matches {
		e := &Entry{
			Format:      sigs[i].Name,
			Description: m.Description,
			Confidence:  m.Confidence,
			Offset:      int64(m.Offset),
			Size:        int64(m.Size),
			SizeKnown:   known[i],
			Parent:      parent,
			Member:      member,
			Depth:       depth,
			data:        data,
			sig:         sigs[i],
			match:       m,
			limit:       o.maxOutput,
		}
		if !yield(e, nil) {
			return false
		}
		if depth >= o.depth || e.skip || sigs[i].NoRecurse || !e.Extractable() {
			continue
		}
		s, err := e.members(ctx)
		if err != nil {
			if errors.Is(err, ErrNotExtracted) {
				// binwalk walks nothing of a failed extraction.
				continue
			}
			return yield(nil, err)
		}
		for _, name := range s.order {
			mem := s.members[name]
			if !mem.Mode.IsRegular() || len(mem.data) == 0 {
				continue
			}
			if !walkInput(ctx, sc, mem.data, o, e, name, yield) {
				return false
			}
		}
	}
	return true
}
