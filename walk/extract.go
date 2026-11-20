package walk

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
	"slices"
)

// Member is one file, folder or link that the extraction of a match makes.
// A device, a FIFO or a socket is a regular file that names it, as binwalk
// writes it.
type Member struct {
	Name string      // the path in the extraction, with slashes
	Mode fs.FileMode // the type and the permissions
	Link string      // the target of a link, relative to the folder of the link
	Size int64       // the bytes of a regular file, or of the target of a link

	// extents are the data of a regular file. A hole between them, or
	// after the last one up to Size, holds no memory and reads as zeros.
	extents []extent
}

// Open reads the content of a regular member. A hole in the member, where
// the extractor wrote nothing, reads as zeros.
func (m *Member) Open() (io.ReadCloser, error) {
	if !m.Mode.IsRegular() {
		return nil, fmt.Errorf("walk: %s is not a regular file", m.Name)
	}
	return io.NopCloser(io.NewSectionReader(memberReader{m}, 0, m.Size)), nil
}

// bytes returns the content of a regular member in one slice. It
// allocates Size bytes, unless the member holds no hole.
func (m *Member) bytes() []byte {
	if len(m.extents) == 1 && m.extents[0].off == 0 && int64(len(m.extents[0].b)) == m.Size {
		return m.extents[0].b
	}
	b := make([]byte, m.Size)
	for _, x := range m.extents {
		copy(b[x.off:], x.b)
	}
	return b
}

// memberReader reads a member whose extents are settled.
type memberReader struct {
	m *Member
}

func (r memberReader) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, errors.New("walk: negative offset")
	}
	if off >= r.m.Size {
		return 0, io.EOF
	}
	p = p[:min(int64(len(p)), r.m.Size-off)]
	x := r.m.extents
	i, _ := slices.BinarySearchFunc(x, off, func(x extent, off int64) int { return cmp.Compare(x.end(), off+1) })
	pos := off
	for n := 0; n < len(p); {
		if i == len(x) || x[i].off >= off+int64(len(p)) {
			clear(p[n:])
			break
		}
		if gap := x[i].off - pos; gap > 0 {
			clear(p[n : n+int(gap)])
			n += int(gap)
			pos += gap
		}
		c := copy(p[n:], x[i].b[pos-x[i].off:])
		n += c
		pos += int64(c)
		i++
	}
	if off+int64(len(p)) == r.m.Size {
		return len(p), io.EOF
	}
	return len(p), nil
}

// ErrNotExtracted is the error of an extraction that failed: the extractor
// rejected the data, or wrote nothing, or reached a limit.
var ErrNotExtracted = errors.New("walk: not extracted")

// extractor returns the extractor of e, or nil when e has none, or binwalk
// declines to extract it.
func (e *Entry) extractor() Extractor {
	if e.match.Declined {
		return nil
	}
	if e.match.Extract != nil {
		return e.match.Extract
	}
	if e.sig == nil {
		return nil
	}
	return e.sig.Extract
}

// Extractable reports whether the walker can extract the match: binwalk
// extracts the format, the walker has a translation of its extractor, and
// the match does not decline it.
func (e *Entry) Extractable() bool {
	return e.extractor() != nil
}

// carve reports whether the extractor of e stands for a program of
// binwalk-ng, which gets only the bytes of the match.
func (e *Entry) carve() bool {
	if e.match.Extract != nil {
		return e.match.Carve
	}
	return e.sig != nil && e.sig.Carve
}

// run runs the extractor of e to s, and reports ErrNotExtracted when it
// fails or writes nothing, as extract and execute of binwalk-ng. When a
// carved extraction fails, it runs the extractor once more on all the data
// to the end of the input, as extract does, after it removes what the
// first run wrote.
func (e *Entry) run(ctx context.Context, s sink, limit int64) error {
	ex := e.extractor()
	if ex == nil {
		return fmt.Errorf("%w: %s at %#x has no extractor", ErrNotExtracted, e.Format, e.Offset)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("walk: stopped: %w", err)
	}
	end := int(e.Offset + e.Size)
	if !e.carve() || end >= len(e.data) {
		return e.attempt(ex, e.data, s, limit)
	}
	err := e.attempt(ex, e.data[:end], s, limit)
	if err == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("walk: stopped: %w", err)
	}
	s.reset()
	return e.attempt(ex, e.data, s, limit)
}

// attempt runs ex once on data, as execute of binwalk-ng.
func (e *Entry) attempt(ex Extractor, data []byte, s sink, limit int64) error {
	out := newOutput(s, limit)
	r := ex(data, int(e.Offset), out)
	if out.budget.n >= limit && limit > 0 && !r.OK {
		return fmt.Errorf("%w: %s at %#x is above the limit of %d bytes", ErrNotExtracted, e.Format, e.Offset, limit)
	}
	if !r.OK || !s.extracted() {
		return fmt.Errorf("%w: %s at %#x", ErrNotExtracted, e.Format, e.Offset)
	}
	return nil
}

// Members returns the members of the match, in the order that the
// extractor made them, up to the limit of WithMaxOutput of the walk. It is
// empty for a match that is not extractable. A failed extraction yields
// one error that wraps ErrNotExtracted.
//
// The Size of a member counts its holes, which hold no memory. See
// WithMaxOutput.
func (e *Entry) Members(ctx context.Context) iter.Seq2[*Member, error] {
	return func(yield func(*Member, error) bool) {
		if !e.Extractable() {
			return
		}
		s, err := e.members(ctx)
		if err != nil {
			yield(nil, err)
			return
		}
		for _, name := range s.order {
			if !yield(s.members[name], nil) {
				return
			}
		}
	}
}

// members extracts e into memory.
func (e *Entry) members(ctx context.Context) (*memSink, error) {
	s := newMemSink()
	if err := e.run(ctx, s, e.limit); err != nil {
		return nil, err
	}
	s.settle()
	return s, nil
}

// Extract writes the members of e under root, as binwalk writes them under
// the folder of the match. It writes no device, and every link stays inside
// root. When the extraction fails, Extract removes what it wrote, and its
// error wraps ErrNotExtracted.
//
// A file with holes is written as a sparse file, as binwalk-ng writes it.
// WithMaxOutput does not count the holes, so a header of a few bytes can
// make a file of up to 16 GiB, see WithMaxOutput. Do not extract to a file
// system that does not keep sparse files, unless the disk has space for
// such a file. Its holes then take space as zeros. A program that copies
// the file also writes the zeros, unless it keeps the holes.
func Extract(ctx context.Context, root *os.Root, e *Entry, opts ...Option) error {
	o, err := newOptions(opts)
	if err != nil {
		return err
	}
	s := &rootSink{root: root}
	if err := e.run(ctx, s, o.maxOutput); err != nil {
		s.remove()
		return err
	}
	return nil
}
