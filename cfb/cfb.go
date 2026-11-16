// Package cfb reads Compound Files, the format of Microsoft that Office 97 to
// 2003, Outlook, Windows Installer and encrypted Office documents use (D62).
// A Compound File holds a tree of storages and streams, like the folders and
// the files of a file system. The package follows the specification
// [MS-CFB]. It also reads property sets, which [MS-OLEPS] specifies, such as
// the stream "\x05SummaryInformation" that holds the title, the author and the
// thumbnail of a document.
//
// The package is written from the two specifications. The engine of magic
// keeps its own reader, which copies the behavior of libmagic (D34, D48).
// magic does not import this package, so a program that does not import it
// does not pay for it.
//
// A damaged or hostile file must not crash the reader or make it hang. Open
// and the methods of File stop at the values of Limits, follow each chain of
// sectors at most once, and return an error that wraps ErrCorrupt or ErrLimit.
//
// [MS-CFB]: https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-cfb/
// [MS-OLEPS]: https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-oleps/
package cfb

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"time"
	"unicode/utf16"
)

// The errors of the package. A function wraps them with the place of the
// fault.
var (
	// ErrCorrupt reports a file that [MS-CFB] or [MS-OLEPS] does not allow,
	// such as a chain of sectors that loops, or that ends before its stream.
	ErrCorrupt = errors.New("cfb: corrupt file")
	// ErrLimit reports a file that is over one of the values of Limits.
	ErrLimit = errors.New("cfb: file over the limit")
)

// The default values of Limits.
const (
	DefaultSectors    = 1 << 24
	DefaultEntries    = 1 << 16
	DefaultStreamSize = 1 << 28
)

// Limits holds the limits of a File. A field of 0 takes its default value.
//
// No limit applies to the length of a chain of sectors. A chain is never
// longer than the sectors of the file, and never longer than its stream
// needs, so Sectors and StreamSize also limit it.
type Limits struct {
	// Sectors is the most sectors that the file can have. The table of
	// sectors in memory takes 4 bytes for each sector. The default is
	// DefaultSectors, 8 GiB of sectors of 512 bytes.
	Sectors int
	// Entries is the most entries that the directory can have. The default is
	// DefaultEntries.
	Entries int
	// StreamSize is the most bytes that a stream can have. It applies to the
	// mini stream too, which holds the small streams. The default is
	// DefaultStreamSize, 256 MiB.
	StreamSize int64
}

// The special numbers of sectors of [MS-CFB] 2.1.
const (
	maxRegSect = 0xFFFFFFFA
	endOfChain = 0xFFFFFFFE
	freeSect   = 0xFFFFFFFF
	noStream   = 0xFFFFFFFF
)

// signature is the first 8 bytes of a Compound File.
const signature = 0xE11AB1A1E011CFD0

// entrySize is the size of an entry of the directory.
const entrySize = 128

// miniShift is the binary logarithm of the size of a mini sector.
const miniShift = 6

var le = binary.LittleEndian

// EntryType is the type of an entry of the directory.
type EntryType uint8

// The types of entries. Open leaves out an entry of another type.
const (
	StorageEntry EntryType = 1
	StreamEntry  EntryType = 2
	RootEntry    EntryType = 5
)

// String returns the name of t, such as "stream".
func (t EntryType) String() string {
	switch t {
	case StorageEntry:
		return "storage"
	case StreamEntry:
		return "stream"
	case RootEntry:
		return "root"
	}
	return fmt.Sprintf("type %d", uint8(t))
}

// GUID is a GUID, such as a CLSID or the FMTID of a property set, as the file
// stores it: the first three fields are little endian.
type GUID [16]byte

// String returns g in the form {F29F85E0-4FF9-1068-AB91-08002B27B3D9}.
func (g GUID) String() string {
	return fmt.Sprintf("{%08X-%04X-%04X-%X-%X}", le.Uint32(g[0:]), le.Uint16(g[4:]), le.Uint16(g[6:]), g[8:10], g[10:])
}

// Entry is a storage or a stream of the directory.
type Entry struct {
	Name      string    // the name, such as "WordDocument" or "\x05SummaryInformation"
	Type      EntryType // StorageEntry, StreamEntry or RootEntry
	CLSID     GUID      // the class of a storage. It is zero for a stream
	StateBits uint32    // the flags that the program that wrote the file gave
	Created   time.Time // the time the storage was made, or the zero time
	Modified  time.Time // the time the storage was changed, or the zero time
	Size      int64     // the bytes of a stream. The size of the mini stream for the root
	Children  []*Entry  // the entries of a storage, in the order of the directory tree

	start uint32
}

// File is an open Compound File. Its methods are safe for concurrent use
// when the io.ReaderAt is.
type File struct {
	r      io.ReaderAt
	lim    Limits
	shift  uint   // the binary logarithm of the size of a sector
	cutoff int64  // a stream smaller than this is in the mini stream
	nsect  uint32 // the sectors of the file after the header
	fat    []uint32
	root   *Entry

	minifat []uint32
	mini    *chain // the mini stream, or nil
	minierr error  // why the mini stream cannot be read
}

// Open reads the header, the table of sectors and the directory of the
// Compound File of size bytes in r. It reads no stream.
func Open(r io.ReaderAt, size int64, lim Limits) (*File, error) {
	if lim.Sectors <= 0 {
		lim.Sectors = DefaultSectors
	}
	if lim.Entries <= 0 {
		lim.Entries = DefaultEntries
	}
	if lim.StreamSize <= 0 {
		lim.StreamSize = DefaultStreamSize
	}
	var h [512]byte
	if size < int64(len(h)) {
		return nil, fmt.Errorf("%w: %d bytes, shorter than the header", ErrCorrupt, size)
	}
	if _, err := r.ReadAt(h[:], 0); err != nil {
		return nil, fmt.Errorf("cfb: reading the header: %w", err)
	}
	if le.Uint64(h[0:]) != signature {
		return nil, fmt.Errorf("%w: no signature", ErrCorrupt)
	}
	f := &File{r: r, lim: lim}
	major := le.Uint16(h[26:])
	f.shift = uint(le.Uint16(h[30:]))
	if f.shift != 9 && f.shift != 12 {
		return nil, fmt.Errorf("%w: sector shift %d", ErrCorrupt, f.shift)
	}
	if s := le.Uint16(h[32:]); s != miniShift {
		return nil, fmt.Errorf("%w: mini sector shift %d", ErrCorrupt, s)
	}
	f.cutoff = int64(le.Uint32(h[56:]))
	// The sectors after the header. The last one can be cut short.
	n := (size - 1) >> f.shift
	if n > int64(lim.Sectors) {
		return nil, fmt.Errorf("%w: %d sectors", ErrLimit, n)
	}
	f.nsect = uint32(n)
	if err := f.readFAT(h[:]); err != nil {
		return nil, err
	}
	if err := f.readDir(le.Uint32(h[48:]), major); err != nil {
		return nil, err
	}
	f.readMini(le.Uint32(h[60:]))
	return f, nil
}

// readFAT reads the table of sectors from the sectors that the DIFAT of the
// header and the chain of DIFAT sectors name. It reads only the sectors of
// the table that cover the sectors of the file.
func (f *File) readFAT(h []byte) error {
	per := uint32(1) << (f.shift - 2)
	need := (f.nsect + per - 1) / per
	if declared := le.Uint32(h[44:]); declared < need {
		need = declared
	}
	ids := make([]uint32, 0, need)
	for i := range 109 {
		if uint32(len(ids)) == need {
			break
		}
		id := le.Uint32(h[76+4*i:])
		if id == freeSect {
			break
		}
		ids = append(ids, id)
	}
	buf := make([]byte, 1<<f.shift)
	next := le.Uint32(h[68:])
	seen := newBitset(int(f.nsect))
	for uint32(len(ids)) < need && next <= maxRegSect {
		if err := f.mark(seen, next, "DIFAT"); err != nil {
			return err
		}
		if err := f.readSector(buf, next); err != nil {
			return err
		}
		for i := uint32(0); i < per-1 && uint32(len(ids)) < need; i++ {
			ids = append(ids, le.Uint32(buf[4*i:]))
		}
		next = le.Uint32(buf[4*(per-1):])
	}
	if uint32(len(ids)) < need {
		return fmt.Errorf("%w: %d sectors of the table of sectors, %d needed", ErrCorrupt, len(ids), need)
	}
	f.fat = make([]uint32, 0, need*per)
	for _, id := range ids {
		if err := f.readSector(buf, id); err != nil {
			return err
		}
		for i := range per {
			f.fat = append(f.fat, le.Uint32(buf[4*i:]))
		}
	}
	f.fat = f.fat[:min(uint32(len(f.fat)), f.nsect)]
	return nil
}

// readSector reads the sector id into p, which has the size of a sector. The
// last sector of the file can be short, and readSector fills the rest of p
// with zeros.
func (f *File) readSector(p []byte, id uint32) error {
	if id >= f.nsect {
		return fmt.Errorf("%w: sector %d past the end of the file", ErrCorrupt, id)
	}
	n, err := f.r.ReadAt(p, (int64(id)+1)<<f.shift)
	if err != nil && (!errors.Is(err, io.EOF) || n == 0) {
		return fmt.Errorf("cfb: reading sector %d: %w", id, err)
	}
	clear(p[n:])
	return nil
}

// mark marks the sector id in seen, and fails when it is marked already.
func (f *File) mark(seen bitset, id uint32, what string) error {
	if id >= f.nsect {
		return fmt.Errorf("%w: %s sector %d past the end of the file", ErrCorrupt, what, id)
	}
	if seen.has(id) {
		return fmt.Errorf("%w: %s chain loops at sector %d", ErrCorrupt, what, id)
	}
	seen.set(id)
	return nil
}

// follow returns the chain of sectors from start in table, which is the
// table of sectors or of mini sectors. It returns maxn sectors, and an error
// when the chain is shorter. For maxn < 0, it follows the chain to its end.
// Each sector is less than len(table), and appears once.
func follow(table []uint32, start uint32, maxn int, what string) ([]uint32, error) {
	var ids []uint32
	if maxn >= 0 {
		ids = make([]uint32, 0, min(maxn, len(table)))
	}
	seen := newBitset(len(table))
	for id := start; id != endOfChain && (maxn < 0 || len(ids) < maxn); id = table[id] {
		if id >= uint32(len(table)) {
			return nil, fmt.Errorf("%w: %s chain names sector %#x", ErrCorrupt, what, id)
		}
		if seen.has(id) {
			return nil, fmt.Errorf("%w: %s chain loops at sector %d", ErrCorrupt, what, id)
		}
		seen.set(id)
		ids = append(ids, id)
	}
	if maxn >= 0 && len(ids) < maxn {
		return nil, fmt.Errorf("%w: %s chain has %d sectors, %d needed", ErrCorrupt, what, len(ids), maxn)
	}
	return ids, nil
}

// readDir reads the directory, and builds the tree of entries from the root.
func (f *File) readDir(start uint32, major uint16) error {
	ids, err := follow(f.fat, start, -1, "directory")
	if err != nil {
		return err
	}
	if len(ids) == 0 {
		return fmt.Errorf("%w: no directory", ErrCorrupt)
	}
	per := 1 << (f.shift - 7)
	if len(ids)*per > f.lim.Entries {
		return fmt.Errorf("%w: %d entries in the directory", ErrLimit, len(ids)*per)
	}
	buf := make([]byte, 1<<f.shift)
	type raw struct {
		e                  *Entry
		left, right, child uint32
	}
	ents := make([]raw, 0, len(ids)*per)
	for _, id := range ids {
		if err := f.readSector(buf, id); err != nil {
			return err
		}
		for i := range per {
			b := buf[i*entrySize : (i+1)*entrySize]
			e := &Entry{
				Type:      EntryType(b[66]),
				StateBits: le.Uint32(b[96:]),
				Created:   Filetime(le.Uint64(b[100:])).Time(),
				Modified:  Filetime(le.Uint64(b[108:])).Time(),
				start:     le.Uint32(b[116:]),
			}
			copy(e.CLSID[:], b[80:96])
			size := le.Uint64(b[120:])
			if major == 3 {
				// [MS-CFB] 2.6.3: a file of version 3 can hold any value in
				// the high 32 bits.
				size &= 0xFFFFFFFF
			}
			e.Size = int64(size & (1<<63 - 1))
			e.Name = entryName(b[:64], le.Uint16(b[64:]))
			ents = append(ents, raw{e, le.Uint32(b[68:]), le.Uint32(b[72:]), le.Uint32(b[76:])})
		}
	}
	if ents[0].e.Type != RootEntry {
		return fmt.Errorf("%w: entry 0 is a %v, not the root", ErrCorrupt, ents[0].e.Type)
	}
	f.root = ents[0].e
	// Each entry can be in the tree once. An entry that a second link names
	// is left out, so a loop of links ends.
	seen := newBitset(len(ents))
	seen.set(0)
	queue := []int{0}
	var stack []uint32
	for len(queue) > 0 {
		p := ents[queue[0]]
		queue = queue[1:]
		// An in-order walk of the red-black tree of the children.
		id := p.child
		for {
			for id < uint32(len(ents)) && !seen.has(id) && validChild(ents[id].e.Type) {
				seen.set(id)
				stack = append(stack, id)
				id = ents[id].left
			}
			if len(stack) == 0 {
				break
			}
			c := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			p.e.Children = append(p.e.Children, ents[c].e)
			if ents[c].e.Type == StorageEntry {
				queue = append(queue, int(c))
			}
			id = ents[c].right
		}
	}
	return nil
}

// validChild reports whether an entry of type t can be the child of a
// storage.
func validChild(t EntryType) bool {
	return t == StorageEntry || t == StreamEntry
}

// entryName decodes the name of an entry: n bytes of UTF-16, with the NUL at
// the end.
func entryName(b []byte, n uint16) string {
	n = min(n, 64) / 2
	u := make([]uint16, 0, n)
	for i := range n {
		c := le.Uint16(b[2*i:])
		if c == 0 {
			break
		}
		u = append(u, c)
	}
	return string(utf16.Decode(u))
}

// readMini reads the table of mini sectors and finds the chain of the mini
// stream. A fault leaves the directory usable, and the error comes back when
// a small stream is opened.
func (f *File) readMini(start uint32) {
	if f.root.Size == 0 {
		return
	}
	mini, err := f.chain(f.root.start, f.root.Size)
	if err != nil {
		f.minierr = fmt.Errorf("mini stream: %w", err)
		return
	}
	ids, err := follow(f.fat, start, -1, "mini table")
	if err != nil {
		f.minierr = err
		return
	}
	buf := make([]byte, 1<<f.shift)
	per := 1 << (f.shift - 2)
	n := int((f.root.Size + 63) >> miniShift)
	f.minifat = make([]uint32, 0, min(len(ids)*per, n))
	for _, id := range ids {
		if len(f.minifat) >= n {
			break
		}
		if err := f.readSector(buf, id); err != nil {
			f.minierr = err
			return
		}
		for i := 0; i < per && len(f.minifat) < n; i++ {
			f.minifat = append(f.minifat, le.Uint32(buf[4*i:]))
		}
	}
	f.mini = mini
}

// chain returns the stream of size bytes from the sector start.
func (f *File) chain(start uint32, size int64) (*chain, error) {
	if size > f.lim.StreamSize {
		return nil, fmt.Errorf("%w: stream of %d bytes", ErrLimit, size)
	}
	n := (size + 1<<f.shift - 1) >> f.shift
	if n > int64(f.nsect) {
		return nil, fmt.Errorf("%w: stream of %d bytes in a file of %d sectors", ErrCorrupt, size, f.nsect)
	}
	ids, err := follow(f.fat, start, int(n), "stream")
	if err != nil {
		return nil, err
	}
	return &chain{src: f.r, ids: ids, shift: f.shift, base: 1 << f.shift, size: size}, nil
}

// Root returns the root storage.
func (f *File) Root() *Entry {
	return f.root
}

// Lookup returns the entry of the path name. A slash separates the names of
// the storages and the stream, such as "ObjectPool/_1234/\x01Ole". The root
// has the empty path. Names compare without case, as [MS-CFB] 2.6.4 says.
// When no entry has the path, the error wraps fs.ErrNotExist.
func (f *File) Lookup(name string) (*Entry, error) {
	e := f.root
	for elem := range strings.SplitSeq(name, "/") {
		if elem == "" {
			continue
		}
		var next *Entry
		for _, c := range e.Children {
			if strings.EqualFold(c.Name, elem) {
				next = c
				break
			}
		}
		if next == nil {
			return nil, fmt.Errorf("cfb: %q: %w", name, fs.ErrNotExist)
		}
		e = next
	}
	return e, nil
}

// Open returns a reader of the stream of the path name, as Lookup finds it.
func (f *File) Open(name string) (*io.SectionReader, error) {
	e, err := f.Lookup(name)
	if err != nil {
		return nil, err
	}
	return f.OpenEntry(e)
}

// OpenEntry returns a reader of the stream e. For the root, it reads the mini
// stream. It reads a stream that is smaller than the cutoff of the header
// from the mini stream, and a larger one from the sectors of the file.
func (f *File) OpenEntry(e *Entry) (*io.SectionReader, error) {
	if e.Type != StreamEntry && e.Type != RootEntry {
		return nil, fmt.Errorf("cfb: %q is a %v, not a stream", e.Name, e.Type)
	}
	if e.Size == 0 {
		return io.NewSectionReader(eof{}, 0, 0), nil
	}
	if e.Size > f.lim.StreamSize {
		return nil, fmt.Errorf("%w: %q of %d bytes", ErrLimit, e.Name, e.Size)
	}
	if e.Type == RootEntry || e.Size >= f.cutoff {
		c, err := f.chain(e.start, e.Size)
		if err != nil {
			return nil, fmt.Errorf("%w, in %q", err, e.Name)
		}
		return io.NewSectionReader(c, 0, e.Size), nil
	}
	if f.mini == nil {
		if f.minierr != nil {
			return nil, fmt.Errorf("%w, in %q", f.minierr, e.Name)
		}
		return nil, fmt.Errorf("%w: no mini stream for %q", ErrCorrupt, e.Name)
	}
	n := int((e.Size + 63) >> miniShift)
	ids, err := follow(f.minifat, e.start, n, "mini stream")
	if err != nil {
		return nil, fmt.Errorf("%w, in %q", err, e.Name)
	}
	c := &chain{src: f.mini, ids: ids, shift: miniShift, size: e.Size}
	return io.NewSectionReader(c, 0, e.Size), nil
}

// ReadFile returns the bytes of the stream of the path name, as Lookup finds
// it.
func (f *File) ReadFile(name string) ([]byte, error) {
	r, err := f.Open(name)
	if err != nil {
		return nil, err
	}
	b := make([]byte, r.Size())
	if len(b) == 0 {
		return b, nil
	}
	if _, err := r.ReadAt(b, 0); err != nil {
		return nil, fmt.Errorf("cfb: reading %q: %w", name, err)
	}
	return b, nil
}

// chain reads a stream from its chain of sectors. Sector id is at
// base + id<<shift in src.
type chain struct {
	src   io.ReaderAt
	ids   []uint32
	shift uint
	base  int64
	size  int64
}

// ReadAt reads the stream at off.
func (c *chain) ReadAt(p []byte, off int64) (int, error) {
	if off < 0 {
		return 0, fmt.Errorf("cfb: negative offset %d", off)
	}
	if off >= c.size {
		return 0, io.EOF
	}
	var short bool
	if int64(len(p)) > c.size-off {
		p, short = p[:c.size-off], true
	}
	mask := int64(1)<<c.shift - 1
	n := 0
	for n < len(p) {
		pos := off + int64(n)
		i, in := pos>>c.shift, pos&mask
		m := min(int64(len(p)-n), mask+1-in)
		k, err := c.src.ReadAt(p[n:n+int(m)], c.base+int64(c.ids[i])<<c.shift+in)
		n += k
		if k < int(m) {
			if err == nil || errors.Is(err, io.EOF) {
				err = io.ErrUnexpectedEOF
			}
			return n, fmt.Errorf("cfb: reading sector %d: %w", c.ids[i], err)
		}
	}
	if short {
		return n, io.EOF
	}
	return n, nil
}

// eof is an empty io.ReaderAt.
type eof struct{}

func (eof) ReadAt([]byte, int64) (int, error) { return 0, io.EOF }

// bitset is a set of numbers of sectors or entries.
type bitset []uint64

func newBitset(n int) bitset { return make(bitset, (n+63)/64) }

func (b bitset) has(i uint32) bool { return b[i/64]&(1<<(i%64)) != 0 }

func (b bitset) set(i uint32) { b[i/64] |= 1 << (i % 64) }
