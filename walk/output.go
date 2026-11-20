// Copyright (c) 2024 devttys0 and the contributors of binwalk-ng. Under the
// MIT license, in LICENSE.
//
// This file translates Chroot of src/extractors.rs of binwalk-ng.

package walk

import (
	"fmt"
	"io/fs"
	"math"
	"path"
	"slices"
	"strings"
)

// Output is where an extractor writes the files of a match, as the Chroot of
// binwalk-ng. Every path is inside the output: a ".." part never climbs above
// its root, a path follows the links that the output already holds, and a
// link points inside the output too.
//
// The walker gives an extractor an Output in memory for Entry.Members and
// for the walk of members, and an Output on an os.Root for Extract. An
// extractor that gets a nil Output does a dry run: it checks the data and
// finds its size, and writes nothing.
//
// Each method returns false when it fails, as in binwalk-ng, and the
// extractor then fails.
type Output struct {
	sink   sink
	budget *budget
	prefix string // the folder of a sub-output, with a slash, or ""
}

// budget is the bytes that an output and its sub-outputs can still write.
// A hole in a file is not written, so it does not count.
type budget struct {
	limit int64 // the most bytes that the output can write
	n     int64 // the bytes written
}

// sink holds the files of an output. A name is a path relative to the root
// of the output, already cleaned and resolved.
type sink interface {
	// exists reports whether name exists, and whether it is a link.
	exists(name string) (ok, link bool)
	// readlink returns the target of the link at name, and false when name
	// is not a link.
	readlink(name string) (string, bool)
	create(name string, data []byte) error
	append(name string, data []byte) error
	// writeAt writes data at off in the regular file name, which exists.
	// A write past the end leaves a hole.
	writeAt(name string, data []byte, off int64) error
	// truncate sets the length of the regular file name, which exists. A
	// larger length adds a hole at the end.
	truncate(name string, size int64) error
	mkdirAll(name string) error
	symlink(name, target string) error
	chmod(name string, add fs.FileMode) error
	setMode(name string, perm fs.FileMode) error
	// extracted reports whether the output holds something that is not an
	// empty file: a file with data, a folder or a link.
	extracted() bool
	// reset removes everything that the output holds.
	reset()
}

func newOutput(s sink, limit int64) *Output {
	return &Output{sink: s, budget: &budget{limit: limit}}
}

// Sub returns an output whose root is the folder dir of o, as an extractor
// of binwalk-ng that makes a second Chroot in its output folder. A path of
// the sub-output, and the target of its links, stay inside dir. The two
// outputs share the files and the limit.
func (o *Output) Sub(dir string) *Output {
	p, ok := o.resolve(dir, true)
	if !ok {
		p = o.rel(dir)
	}
	if p != "" {
		p += "/"
	}
	return &Output{sink: o.sink, budget: o.budget, prefix: o.prefix + p}
}

// reserve counts n more bytes, and reports false past the limit.
func (o *Output) reserve(n int) bool {
	if o.budget.n+int64(n) > o.budget.limit {
		return false
	}
	o.budget.n += int64(n)
	return true
}

// CreateFile is create_file: it writes a new file, through a link at its
// path, and fails when the path exists.
func (o *Output) CreateFile(name string, data []byte) bool {
	p, ok := o.full(name, true)
	if !ok {
		return false
	}
	if ok, _ := o.sink.exists(p); ok || !o.reserve(len(data)) {
		return false
	}
	return o.sink.create(p, data) == nil
}

// CarveFile is carve_file: it writes data[start:start+size] to a new file.
func (o *Output) CarveFile(name string, data []byte, start, size int) bool {
	if start < 0 || size < 0 || start+size > len(data) || start+size < start {
		return false
	}
	return o.CreateFile(name, data[start:start+size])
}

// AppendFile is append_to_file: it adds data to the end of a file, which it
// makes when it does not exist, through a link at its path.
func (o *Output) AppendFile(name string, data []byte) bool {
	p, ok := o.full(name, true)
	if !ok || !o.reserve(len(data)) {
		return false
	}
	return o.sink.append(p, data) == nil
}

// WriteFileAt writes data at the offset off of a file that exists, through a
// link at its path. An extractor of binwalk-ng does this when it seeks in
// the file of create_file_writer and then writes.
//
// A write past the end of the file leaves a hole before data, as a seek
// does. The hole reads as zeros and holds no bytes, in memory and on a disk
// that keeps sparse files. Only the bytes of data count against the limit
// of the output.
func (o *Output) WriteFileAt(name string, data []byte, off int64) bool {
	p, ok := o.full(name, true)
	if !ok || off < 0 || off > math.MaxInt64-int64(len(data)) || !o.reserve(len(data)) {
		return false
	}
	return o.sink.writeAt(p, data, off) == nil
}

// TruncateFile is set_len of a file that create_file_writer gave: it sets
// the length of a file that exists, through a link at its path. A larger
// length adds a hole at the end, as WriteFileAt does, and a hole does not
// count against the limit of the output.
func (o *Output) TruncateFile(name string, size int64) bool {
	p, ok := o.full(name, true)
	if !ok || size < 0 {
		return false
	}
	return o.sink.truncate(p, size) == nil
}

// CreateDirectory is create_directory: it makes a folder and its parents.
func (o *Output) CreateDirectory(name string) bool {
	p, ok := o.full(name, true)
	return ok && o.sink.mkdirAll(p) == nil
}

// CreateCharDevice is create_character_device. binwalk-ng makes no device:
// it writes a file that names the device, such as "c 1 3".
func (o *Output) CreateCharDevice(name string, major, minor int) bool {
	return o.CreateFile(name, fmt.Appendf(nil, "c %d %d", major, minor))
}

// CreateBlockDevice is create_block_device, a file such as "b 8 0".
func (o *Output) CreateBlockDevice(name string, major, minor int) bool {
	return o.CreateFile(name, fmt.Appendf(nil, "b %d %d", major, minor))
}

// CreateFIFO is create_fifo, a file that holds "fifo".
func (o *Output) CreateFIFO(name string) bool {
	return o.CreateFile(name, []byte("fifo"))
}

// CreateSocket is create_socket, a file that holds "socket".
func (o *Output) CreateSocket(name string) bool {
	return o.CreateFile(name, []byte("socket"))
}

// MakeExecutable is make_executable. It adds the bit of execution for
// others only, as binwalk-ng does.
func (o *Output) MakeExecutable(name string) bool {
	p, ok := o.full(name, true)
	return ok && o.sink.chmod(p, 0o001) == nil
}

// SetMode is set_mode: it sets the permissions of a file to the low nine
// bits of mode.
func (o *Output) SetMode(name string, mode uint32) bool {
	p, ok := o.full(name, true)
	return ok && o.sink.setMode(p, fs.FileMode(mode&0o777)) == nil
}

// SetOwnership is set_ownership. binwalk-ng changes the owner where the
// system lets it, and its extractors do not check the result. The walker
// keeps no owner, and only checks that the path stays inside the output.
func (o *Output) SetOwnership(name string, _, _ uint32) bool {
	_, ok := o.full(name, false)
	return ok
}

// CreateSymlink is create_symlink: it makes a link at name to target. An
// absolute target names a file from the root of the output, and a relative
// one is joined to the folder of the link and cleaned. The link that the
// output holds is relative, as "../../bin/busybox", so it cannot leave the
// output.
//
// binwalk-ng joins a relative target to the folder of the link on the
// system, so a target that climbs above the root of the output depends on
// where binwalk-ng writes it. The walker keeps such a target at the root.
func (o *Output) CreateSymlink(name, target string) bool {
	link, ok := o.resolve(name, false)
	if !ok || link == "" {
		return false
	}
	var base string
	if strings.HasPrefix(target, "/") {
		base = o.rel(target)
	} else {
		base = o.rel(path.Join("/", path.Dir("/"+link), target))
	}
	rel := strings.Repeat("../", strings.Count(link, "/")) + base
	rel = strings.TrimSuffix(rel, "/")
	if rel == "" {
		return false
	}
	return o.sink.symlink(o.prefix+link, rel) == nil
}

// path is chrooted_path: the name, cleaned and relative to the root of the
// whole output, without following any link. The root itself is "".
func (o *Output) path(name string) string {
	p := o.rel(name)
	if p == "" {
		return strings.TrimSuffix(o.prefix, "/")
	}
	return o.prefix + p
}

// full resolves name in o, and returns it relative to the root of the whole
// output. It reports false when a link takes the path out of o.
func (o *Output) full(name string, followFinal bool) (string, bool) {
	p, ok := o.resolve(name, followFinal)
	if !ok {
		return "", false
	}
	if p == "" {
		return strings.TrimSuffix(o.prefix, "/"), true
	}
	return o.prefix + p, true
}

// rel is sanitize_path: the name, cleaned, relative to the root of o. A "."
// part goes, and a ".." part removes the part before it, but never climbs
// above the root.
func (o *Output) rel(name string) string {
	return strings.Join(cleanParts(nil, name), "/")
}

// cleanParts adds the parts of p to parts, as sanitize_path cleans them.
func cleanParts(parts []string, p string) []string {
	for part := range strings.SplitSeq(p, "/") {
		switch part {
		case "", ".":
		case "..":
			if len(parts) > 0 {
				parts = parts[:len(parts)-1]
			}
		default:
			parts = append(parts, part)
		}
	}
	return parts
}

// resolve is resolve_in_chroot: it follows each link that a part of name
// names in o, as the system follows it, up to 40 links. A ".." of name stops
// at the root, and a ".." of a link that leaves the root, or an absolute
// link, fails. The final part is followed only with followFinal. It returns
// the path relative to the root of o.
func (o *Output) resolve(name string, followFinal bool) (string, bool) {
	const maxLinks = 40
	type seg struct {
		name     string
		fromLink bool
	}
	var pending []seg
	push := func(p string, fromLink bool) {
		parts := strings.Split(p, "/")
		for i := len(parts) - 1; i >= 0; i-- {
			if parts[i] != "" && parts[i] != "." {
				pending = append(pending, seg{parts[i], fromLink})
			}
		}
	}
	push(name, false)
	var out []string
	links := 0
	for len(pending) > 0 {
		s := pending[len(pending)-1]
		pending = pending[:len(pending)-1]
		if s.name == ".." {
			if len(out) == 0 {
				if s.fromLink {
					return "", false
				}
				continue
			}
			out = out[:len(out)-1]
			continue
		}
		candidate := append(slices.Clip(out), s.name) //nolint:gocritic // out stays as it is until the link is known
		if !followFinal && len(pending) == 0 {
			out = candidate
			break
		}
		target, ok := o.sink.readlink(o.prefix + strings.Join(candidate, "/"))
		if !ok {
			out = candidate
			continue
		}
		if links++; links > maxLinks || strings.HasPrefix(target, "/") {
			return "", false
		}
		push(target, true)
	}
	return strings.Join(out, "/"), true
}
