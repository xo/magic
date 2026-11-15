// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates Chroot of src/extractors/common.rs of binwalk.

package walk

import (
	"fmt"
	"io/fs"
	"slices"
	"strings"
)

// Output is where an extractor writes the files of a match, as the Chroot of
// binwalk. Every path is inside the output: a path that goes up with ".." is
// cleaned as binwalk cleans it, and a link points inside the output too.
//
// The walker gives an extractor an Output in memory for Entry.Members and
// for the walk of members, and an Output on an os.Root for Extract. An
// extractor that gets a nil Output does a dry run: it checks the data and
// finds its size, and writes nothing.
//
// Each method returns false when it fails, as in binwalk, and the extractor
// then fails.
type Output struct {
	sink   sink
	budget *budget
	prefix string // the folder of a sub-output, with a slash, or ""
}

// budget is the bytes that an output and its sub-outputs can still write.
type budget struct {
	limit int64 // the most bytes that the files of the output can hold
	n     int64 // the bytes written
}

// sink holds the files of an output. A name is a path relative to the root
// of the output, already cleaned.
type sink interface {
	// exists reports whether name exists, and whether it is a link.
	exists(name string) (ok, link bool)
	create(name string, data []byte) error
	append(name string, data []byte) error
	mkdirAll(name string) error
	symlink(name, target string) error
	chmod(name string, add fs.FileMode) error
	// extracted reports whether the output holds something that is not an
	// empty file: a file with data, a folder or a link.
	extracted() bool
}

func newOutput(s sink, limit int64) *Output {
	return &Output{sink: s, budget: &budget{limit: limit}}
}

// Sub returns an output whose root is the folder dir of o, as an extractor
// of binwalk that makes a second Chroot in its output folder. A path of the
// sub-output, and the target of its links, stay inside dir. The two outputs
// share the files and the limit.
func (o *Output) Sub(dir string) *Output {
	p := o.path(dir)
	if p != "" {
		p += "/"
	}
	return &Output{sink: o.sink, budget: o.budget, prefix: p}
}

// reserve counts n more bytes, and reports false past the limit.
func (o *Output) reserve(n int) bool {
	if o.budget.n+int64(n) > o.budget.limit {
		return false
	}
	o.budget.n += int64(n)
	return true
}

// CreateFile is create_file: it writes a new file, and fails when the path
// exists.
func (o *Output) CreateFile(name string, data []byte) bool {
	p := o.path(name)
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
// makes when it does not exist. It fails on a link.
func (o *Output) AppendFile(name string, data []byte) bool {
	p := o.path(name)
	if _, link := o.sink.exists(p); link || !o.reserve(len(data)) {
		return false
	}
	return o.sink.append(p, data) == nil
}

// CreateDirectory is create_directory: it makes a folder and its parents.
func (o *Output) CreateDirectory(name string) bool {
	return o.sink.mkdirAll(o.path(name)) == nil
}

// CreateCharDevice is create_character_device. binwalk makes no device: it
// writes a file that names the device, such as "c 1 3".
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
// others only, as binwalk does.
func (o *Output) MakeExecutable(name string) bool {
	return o.sink.chmod(o.path(name), 0o001) == nil
}

// CreateSymlink is create_symlink: it makes a link at name to target. An
// absolute target is inside the output, and a relative one is joined to the
// folder of the link and cleaned. The link that the output holds is
// relative, as "../../bin/busybox", so it cannot leave the output.
func (o *Output) CreateSymlink(name, target string) bool {
	link := "/" + o.rel(name)
	var abs string
	if strings.HasPrefix(target, "/") {
		abs = "/" + o.rel(target)
	} else {
		dir := "/"
		if i := strings.LastIndex(link, "/"); i > 0 {
			dir = link[:i]
		}
		abs = "/" + o.rel(dir+"/"+target)
	}
	rel := abs
	for range strings.Count(link, "/") - 1 {
		rel = "../" + rel
	}
	if strings.HasPrefix(rel, "/") {
		rel = "." + rel
	}
	rel = stripDoubleSlash(rel)
	return o.sink.symlink(o.prefix+link[1:], rel) == nil
}

// path is chrooted_path: the name, cleaned and relative to the root of the
// whole output. The root itself is "".
func (o *Output) path(name string) string {
	p := o.rel(name)
	if p == "" {
		return strings.TrimSuffix(o.prefix, "/")
	}
	return o.prefix + p
}

// rel is the name, cleaned and relative to the root of o. binwalk keeps a
// "." part, which the file system then reads as the folder itself. The
// walker drops it, so that a member has one name.
func (o *Output) rel(name string) string {
	p := strings.TrimPrefix(sanitizePath(name+"/", true), "/")
	if !strings.Contains("/"+p+"/", "/./") {
		return p
	}
	var parts []string
	for part := range strings.SplitSeq(p, "/") {
		if part != "." {
			parts = append(parts, part)
		}
	}
	return strings.Join(parts, "/")
}

// sanitizePath is sanitize_path. A ".." part goes, with the part before it
// that has not gone yet, and so does an empty part. The search for the part
// before stops at the first part, even when the first part has gone, as in
// binwalk.
func sanitizePath(p string, keepRoot bool) string {
	parts := strings.Split(p, "/")
	var exclude []int
	for i, part := range parts {
		switch part {
		case "..":
			exclude = append(exclude, i)
			if i > 0 {
				j := i - 1
				for j > 0 && slices.Contains(exclude, j) {
					j--
				}
				exclude = append(exclude, j)
			}
		case "":
			exclude = append(exclude, i)
		}
	}
	out := ""
	if keepRoot && strings.HasPrefix(p, "/") {
		out = "/"
	}
	for i, part := range parts {
		if !slices.Contains(exclude, i) {
			out += "/" + part
		}
	}
	return stripDoubleSlash(out)
}

// stripDoubleSlash is strip_double_slash.
func stripDoubleSlash(p string) string {
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	return p
}
