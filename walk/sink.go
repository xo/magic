package walk

import (
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
	s.add(&Member{Name: name, Mode: 0o644, Size: int64(len(data)), data: slices.Clone(data)})
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
	m.data = append(m.data, data...)
	m.Size = int64(len(m.data))
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
		if !m.Mode.IsRegular() || len(m.data) > 0 {
			return true
		}
	}
	return false
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
