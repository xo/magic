package walk

import (
	"cmp"
	"errors"
	"io/fs"
	"os"
	"path"
	"slices"
	"strings"
)

var (
	errExists    = errors.New("walk: the path exists")
	errNoFolder  = errors.New("walk: the folder of the path does not exist")
	errNotFolder = errors.New("walk: a part of the path is not a folder")
	errNotFound  = errors.New("walk: the path does not exist")
	errNotFile   = errors.New("walk: the path is not a regular file")
)

// memSink is an output in memory. It keeps the members in the order that
// the extractor made them.
type memSink struct {
	members map[string]*Member
	order   []string
}

func newMemSink() *memSink {
	return &memSink{members: map[string]*Member{}}
}

func (s *memSink) exists(name string) (bool, bool) {
	if name == "" {
		return true, false
	}
	m, ok := s.members[name]
	return ok, ok && m.Mode&fs.ModeSymlink != 0
}

// parent checks that the folder of name exists, as a file system does.
func (s *memSink) parent(name string) error {
	dir := path.Dir(name)
	if dir == "." {
		return nil
	}
	m, ok := s.members[dir]
	switch {
	case !ok:
		return errNoFolder
	case !m.Mode.IsDir():
		return errNotFolder
	}
	return nil
}

func (s *memSink) add(m *Member) {
	s.members[m.Name] = m
	s.order = append(s.order, m.Name)
}

func (s *memSink) create(name string, data []byte) error {
	if ok, _ := s.exists(name); ok {
		return errExists
	}
	if err := s.parent(name); err != nil {
		return err
	}
	m := &Member{Name: name, Mode: 0o644}
	m.write(data, 0)
	s.add(m)
	return nil
}

func (s *memSink) append(name string, data []byte) error {
	m, ok := s.members[name]
	if !ok {
		return s.create(name, data)
	}
	if !m.Mode.IsRegular() {
		return errNotFolder
	}
	m.write(data, m.Size)
	return nil
}

func (s *memSink) writeAt(name string, data []byte, off int64) error {
	m, ok := s.members[name]
	switch {
	case !ok:
		return errNotFound
	case !m.Mode.IsRegular():
		return errNotFile
	}
	m.write(data, off)
	return nil
}

func (s *memSink) truncate(name string, size int64) error {
	m, ok := s.members[name]
	switch {
	case !ok:
		return errNotFound
	case !m.Mode.IsRegular():
		return errNotFile
	}
	m.truncate(size)
	return nil
}

func (s *memSink) mkdirAll(name string) error {
	if name == "" {
		return nil
	}
	var p string
	for part := range strings.SplitSeq(name, "/") {
		p = path.Join(p, part)
		m, ok := s.members[p]
		switch {
		case !ok:
			s.add(&Member{Name: p, Mode: fs.ModeDir | 0o755})
		case !m.Mode.IsDir():
			return errNotFolder
		}
	}
	return nil
}

func (s *memSink) symlink(name, target string) error {
	if ok, _ := s.exists(name); ok {
		return errExists
	}
	if err := s.parent(name); err != nil {
		return err
	}
	s.add(&Member{Name: name, Mode: fs.ModeSymlink | 0o777, Link: target, Size: int64(len(target))})
	return nil
}

func (s *memSink) chmod(name string, add fs.FileMode) error {
	m, ok := s.members[name]
	if !ok {
		return errNotFound
	}
	m.Mode |= add
	return nil
}

func (s *memSink) readlink(name string) (string, bool) {
	m, ok := s.members[name]
	if !ok || m.Mode&fs.ModeSymlink == 0 {
		return "", false
	}
	return m.Link, true
}

func (s *memSink) setMode(name string, perm fs.FileMode) error {
	m, ok := s.members[name]
	if !ok {
		return errNotFound
	}
	m.Mode = m.Mode&^fs.ModePerm | perm
	return nil
}

func (s *memSink) reset() {
	clear(s.members)
	s.order = s.order[:0]
}

func (s *memSink) extracted() bool {
	for _, m := range s.members {
		if !m.Mode.IsRegular() || m.Size > 0 {
			return true
		}
	}
	return false
}

// settle puts the data of each file in order, when the extraction is done,
// so that a Member does not change while it is read.
func (s *memSink) settle() {
	for _, m := range s.members {
		m.settle()
	}
}

// extent is a run of bytes at an offset of a file in memory. The bytes
// between the extents of a file are a hole, which reads as zeros.
type extent struct {
	off int64
	b   []byte
}

// end returns the offset after the last byte of x.
func (x extent) end() int64 {
	return x.off + int64(len(x.b))
}

// write writes b at off in the data of m. Until settle, the extents of m
// are in the order of the writes, so a write costs no copy of the file. A
// write at the end of the last write grows that extent.
func (m *Member) write(b []byte, off int64) {
	if len(b) == 0 {
		return
	}
	m.Size = max(m.Size, off+int64(len(b)))
	if n := len(m.extents); n > 0 && m.extents[n-1].end() == off {
		m.extents[n-1].b = append(m.extents[n-1].b, b...)
		return
	}
	m.extents = append(m.extents, extent{off: off, b: slices.Clone(b)})
}

// truncate sets the size of m. It drops the bytes past size, and a larger
// size adds a hole at the end.
func (m *Member) truncate(size int64) {
	kept := m.extents[:0]
	for _, x := range m.extents {
		if x.off >= size {
			continue
		}
		if x.end() > size {
			x.b = x.b[: size-x.off : size-x.off]
		}
		kept = append(kept, x)
	}
	clear(m.extents[len(kept):])
	m.extents = kept
	m.Size = size
}

// settle puts the extents of m in order of offset, with no overlap. Where
// two writes overlap, the later one wins, as on a file system.
func (m *Member) settle() {
	settled := true
	for i := 1; i < len(m.extents) && settled; i++ {
		settled = m.extents[i].off >= m.extents[i-1].end()
	}
	if settled {
		return
	}
	// order holds the index of each write, in order of offset. A group is
	// a run of order whose writes overlap, and becomes one extent.
	order := make([]int, len(m.extents))
	for i := range order {
		order[i] = i
	}
	slices.SortStableFunc(order, func(i, j int) int { return cmp.Compare(m.extents[i].off, m.extents[j].off) })
	out := make([]extent, 0, len(m.extents))
	for i := 0; i < len(order); {
		start, end := m.extents[order[i]].off, m.extents[order[i]].end()
		j := i + 1
		for ; j < len(order) && m.extents[order[j]].off < end; j++ {
			end = max(end, m.extents[order[j]].end())
		}
		if j == i+1 {
			out = append(out, m.extents[order[i]])
			i = j
			continue
		}
		group := slices.Clone(order[i:j])
		slices.Sort(group)
		b := make([]byte, end-start)
		for _, k := range group {
			copy(b[m.extents[k].off-start:], m.extents[k].b)
		}
		out = append(out, extent{off: start, b: b})
		i = j
	}
	m.extents = out
}

// rootSink writes an output under an os.Root, which refuses every path
// that leaves it. It keeps the names that it made at the top, to remove
// them when the extraction fails.
type rootSink struct {
	root *os.Root
	top  []string
}

func (s *rootSink) note(name string) {
	top, _, _ := strings.Cut(name, "/")
	if !slices.Contains(s.top, top) {
		s.top = append(s.top, top)
	}
}

func (s *rootSink) exists(name string) (bool, bool) {
	if name == "" {
		return true, false
	}
	fi, err := s.root.Lstat(name)
	if err != nil {
		return false, false
	}
	return true, fi.Mode()&fs.ModeSymlink != 0
}

func (s *rootSink) create(name string, data []byte) error {
	f, err := s.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err //nolint:wrapcheck // Output reports only false
	}
	s.note(name)
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err //nolint:wrapcheck // Output reports only false
}

func (s *rootSink) append(name string, data []byte) error {
	f, err := s.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return err //nolint:wrapcheck // Output reports only false
	}
	s.note(name)
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err //nolint:wrapcheck // Output reports only false
}

func (s *rootSink) writeAt(name string, data []byte, off int64) error {
	f, err := s.root.OpenFile(name, os.O_WRONLY, 0)
	if err != nil {
		return err //nolint:wrapcheck // Output reports only false
	}
	// A write past the end makes a hole on a file system that has them.
	_, err = f.WriteAt(data, off)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err //nolint:wrapcheck // Output reports only false
}

func (s *rootSink) truncate(name string, size int64) error {
	f, err := s.root.OpenFile(name, os.O_WRONLY, 0)
	if err != nil {
		return err //nolint:wrapcheck // Output reports only false
	}
	err = f.Truncate(size)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err //nolint:wrapcheck // Output reports only false
}

func (s *rootSink) mkdirAll(name string) error {
	if name == "" {
		return nil
	}
	s.note(name)
	return s.root.MkdirAll(name, 0o755) //nolint:wrapcheck // Output reports only false
}

func (s *rootSink) symlink(name, target string) error {
	if err := s.root.Symlink(target, name); err != nil {
		return err //nolint:wrapcheck // Output reports only false
	}
	s.note(name)
	return nil
}

func (s *rootSink) chmod(name string, add fs.FileMode) error {
	fi, err := s.root.Stat(name)
	if err != nil {
		return err //nolint:wrapcheck // Output reports only false
	}
	return s.root.Chmod(name, fi.Mode().Perm()|add) //nolint:wrapcheck // Output reports only false
}

func (s *rootSink) readlink(name string) (string, bool) {
	t, err := s.root.Readlink(name)
	if err != nil {
		return "", false
	}
	return t, true
}

func (s *rootSink) setMode(name string, perm fs.FileMode) error {
	return s.root.Chmod(name, perm) //nolint:wrapcheck // Output reports only false
}

func (s *rootSink) extracted() bool {
	for _, top := range s.top {
		fi, err := s.root.Lstat(top)
		if err == nil && (!fi.Mode().IsRegular() || fi.Size() > 0) {
			return true
		}
	}
	return false
}

// remove removes what the sink made.
func (s *rootSink) remove() {
	for _, top := range s.top {
		_ = s.root.RemoveAll(top)
	}
}

func (s *rootSink) reset() {
	s.remove()
	s.top = s.top[:0]
}
