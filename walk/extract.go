package walk

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"iter"
	"os"
)

// Member is one file, folder or link that the extraction of a match makes.
// A device, a FIFO or a socket is a regular file that names it, as binwalk
// writes it.
type Member struct {
	Name string      // the path in the extraction, with slashes
	Mode fs.FileMode // the type and the permissions
	Link string      // the target of a link, relative to the folder of the link
	Size int64       // the bytes of a regular file, or of the target of a link

	data []byte
}

// Open reads the content of a regular member.
func (m *Member) Open() (io.ReadCloser, error) {
	if !m.Mode.IsRegular() {
		return nil, fmt.Errorf("walk: %s is not a regular file", m.Name)
	}
	return io.NopCloser(bytes.NewReader(m.data)), nil
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

// run runs the extractor of e to s, and reports ErrNotExtracted when it
// fails or writes nothing, as execute of binwalk.
func (e *Entry) run(ctx context.Context, s sink, limit int64) error {
	ex := e.extractor()
	if ex == nil {
		return fmt.Errorf("%w: %s at %#x has no extractor", ErrNotExtracted, e.Format, e.Offset)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("walk: stopped: %w", err)
	}
	out := newOutput(s, limit)
	data := e.data
	if e.sig != nil && e.sig.Carve {
		data = data[:e.Offset+e.Size]
	}
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
	return s, nil
}

// Extract writes the members of e under root, as binwalk writes them under
// the folder of the match. It writes no device, and every link stays inside
// root. When the extraction fails, Extract removes what it wrote, and its
// error wraps ErrNotExtracted.
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
