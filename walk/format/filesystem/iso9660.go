//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/iso9660.rs and
// src/structures/iso9660.rs of binwalk. binwalk extracts ISO 9660 with 7zz,
// from src/extractors/iso9660.rs, and extractISO9660 writes the files that
// 7zz writes. It copies the behavior of the handler of ISO in 7-Zip, in
// IsoIn.cpp, IsoItem.h and IsoHandler.cpp, not its code.

package filesystem

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"iter"
	"math/bits"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// ISO9660 is the signature "iso9660" of binwalk. The magic is the start of
// the primary volume descriptor, 32768 bytes after the start of the image.
var ISO9660 = &walk.Signature{
	Name:        "iso9660",
	Magic:       [][]byte{[]byte("\x01CD001\x01\x00")},
	Description: "ISO9660 primary volume",
	Parse:       parseISO9660,
	Extract:     extractISO9660,
}

// isoMagicOffset is the offset of the magic from the start of the image.
const isoMagicOffset = 32768

// parseISO9660 is iso_parser.
func parseISO9660(data []byte, off int) (walk.Match, error) {
	if off < isoMagicOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	start := off - isoMagicOffset
	size, ok := parseISOHeader(data[start:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      start,
		Size:        size,
		Confidence:  walk.High,
		Description: fmt.Sprintf("ISO9660 primary volume, total size: %d bytes", size),
	}, nil
}

var isoHeader = binwalk.Struct{
	{Name: "unused1", Type: "u64"},
	{Name: "volume_size_lsb", Type: "u32"},
	{Name: "volume_size_msb", Type: "u32"},
	{Name: "unused2", Type: "u64"},
	{Name: "unused3", Type: "u64"},
	{Name: "unused4", Type: "u64"},
	{Name: "unused5", Type: "u64"},
	{Name: "set_size_lsb", Type: "u16"},
	{Name: "set_size_msb", Type: "u16"},
	{Name: "sequence_number_lsb", Type: "u16"},
	{Name: "sequence_number_msb", Type: "u16"},
	{Name: "block_size_lsb", Type: "u16"},
	{Name: "block_size_msb", Type: "u16"},
	{Name: "path_table_size_lsb", Type: "u32"},
	{Name: "path_table_size_msb", Type: "u32"},
}

// parseISOHeader is parse_iso_header: it checks a part of the primary
// volume descriptor, and returns the size of the image.
func parseISOHeader(data []byte) (int, bool) {
	// The offset of the structure from the start of the image.
	const structStart = 32840
	b, ok := binwalk.From(data, structStart)
	if !ok {
		return 0, false
	}
	h, ok := isoHeader.Parse(b, false)
	if !ok {
		return 0, false
	}
	// The unused fields must be 0.
	for _, f := range []string{"unused1", "unused2", "unused3", "unused4", "unused5"} {
		if h[f] != 0 {
			return 0, false
		}
	}
	// Each field is in both byte orders, and the two must agree. binwalk
	// swaps the bytes of the second one, which it read in little endian.
	swap16 := func(v int) int { return int(bits.ReverseBytes16(uint16(v))) }
	swap32 := func(v int) int { return int(bits.ReverseBytes32(uint32(v))) }
	if h["set_size_lsb"] != swap16(h["set_size_msb"]) ||
		h["block_size_lsb"] != swap16(h["block_size_msb"]) ||
		h["volume_size_lsb"] != swap32(h["volume_size_msb"]) ||
		h["sequence_number_lsb"] != swap16(h["sequence_number_msb"]) ||
		h["path_table_size_lsb"] != swap32(h["path_table_size_msb"]) {
		return 0, false
	}
	return h["volume_size_lsb"] * h["block_size_lsb"], true
}

// extractISO9660 writes the files of the image as 7zz writes them. binwalk
// gives 7zz the bytes of the match.
func extractISO9660(data []byte, off int, out *walk.Output) walk.Extraction {
	m, err := parseISO9660(data, off+isoMagicOffset)
	if err != nil || m.Size > len(data)-off {
		return walk.Extraction{}
	}
	a, ok := openISO(data[off : off+m.Size])
	if !ok {
		return walk.Extraction{}
	}
	if out == nil {
		return walk.Extraction{OK: true}
	}
	return walk.Extraction{OK: writeSevenZip(out, a.items())}
}

// The constants of the handler of ISO in 7-Zip.
const (
	isoBlockSize     = 2048
	isoStartPos      = 16 * isoBlockSize
	isoMaxLevel      = 256
	isoMetadataSlack = 16 << 20

	isoFlagDir         = 0x02
	isoFlagNonFinal    = 0x80
	isoBootable        = 0x88
	isoNotBootable     = 0x00
	isoMoreHeaders     = 0x90
	isoFinalHeader     = 0x91
	isoMediaNoEmul     = 0
	isoExtensionRecord = 0x44
)

// isoArchive is an image as CInArchive of IsoIn.cpp opens it.
type isoArchive struct {
	img       []byte
	joliet    bool // the tree is the one of a Joliet volume
	susp      bool // the root has the record SP of SUSP, so Rock Ridge holds
	suspSkip  int
	processed int // the bytes of folders read, which 7-Zip limits
	refs      []isoRef
	boot      []isoBootEntry
	expand    int // the boot entry whose size reaches the end of the image
}

// isoRecord is CDirRecord, with the items of a folder.
type isoRecord struct {
	extent    int
	size      int
	flags     byte
	fileID    []byte
	systemUse []byte
	parent    *isoRecord
	children  []*isoRecord
}

// isoRef is CRef: an item, which is one record, or the records of the
// extents of one file.
type isoRef struct {
	rec     *isoRecord
	extents []*isoRecord
}

// isoBootEntry is CBootInitialEntry, an entry of the catalog of El Torito.
type isoBootEntry struct {
	bootable    bool
	mediaType   byte
	sectorCount int
	loadRBA     int
	vendorSpec  [20]byte
}

// isoError stops the open of an image, as an exception of 7-Zip does.
type isoError struct{}

// openISO is Open2 of IsoIn.cpp. 7-Zip fails to open an image whose
// descriptors or folders it cannot read, and then 7zz writes nothing.
func openISO(img []byte) (a *isoArchive, ok bool) {
	defer func() {
		if r := recover(); r != nil {
			if _, is := r.(isoError); !is {
				panic(r)
			}
			a, ok = nil, false
		}
	}()
	if len(img) < isoStartPos {
		return nil, false
	}
	a = &isoArchive{img: img, expand: -1}
	type volume struct {
		joliet bool
		root   []byte
		block  int
	}
	var vols []volume
	var bootCatalog = -1
	for pos := isoStartPos; ; pos += isoBlockSize {
		d := a.read(pos, isoBlockSize)
		if string(d[1:6]) != "CD001" || d[6] > 2 {
			return nil, false
		}
		typ := d[0]
		if typ == 255 {
			break
		}
		switch typ {
		case 0:
			// A boot record. The catalog of El Torito follows the name of
			// the specification.
			spec := make([]byte, 32)
			copy(spec, "EL TORITO SPECIFICATION")
			bootCatalog = -1
			if bytes.Equal(d[7:39], spec) {
				bootCatalog = int(binary.LittleEndian.Uint32(d[71:]))
			}
		case 1, 2:
			esc := d[88:91]
			v := volume{
				root:  d[156:190],
				block: int(binary.LittleEndian.Uint16(d[128:])),
			}
			// The escape sequences of a primary volume do not count.
			v.joliet = typ == 2 && d[7]&1 == 0 && esc[0] == 0x25 && esc[1] == 0x2F &&
				(esc[2] == 0x40 || esc[2] == 0x43 || esc[2] == 0x45)
			vols = append(vols, v)
		}
	}
	if len(vols) == 0 {
		return nil, false
	}
	// 7-Zip takes the last Joliet volume after the first, or the first.
	main := 0
	for i := len(vols) - 1; i > 0; i-- {
		if vols[i].joliet {
			main = i
			break
		}
	}
	if vols[main].block != isoBlockSize {
		return nil, false
	}
	a.joliet = vols[main].joliet
	if bootCatalog >= 0 {
		a.readBoot(bootCatalog)
	}
	root := a.record(vols[main].root)
	a.readDir(root, 0, nil)
	a.createRefs(root)
	for i, be := range a.boot {
		if be.sectorCount <= 1 && be.mediaType == isoMediaNoEmul &&
			(a.expand < 0 || be.loadRBA >= a.boot[a.expand].loadRBA) {
			a.expand = i
		}
	}
	return a, true
}

// read returns n bytes at pos, or stops the open when the image ends
// before them.
func (a *isoArchive) read(pos, n int) []byte {
	b, ok := binwalk.Range(a.img, pos, pos+n)
	if !ok {
		panic(isoError{})
	}
	return b
}

// record is ReadDirRecord: it reads the record r, whose first byte is its
// length.
func (a *isoArchive) record(r []byte) *isoRecord {
	const fixed = 33
	if len(r) < fixed || r[1] != 0 {
		panic(isoError{})
	}
	both := func(off int) int {
		le, be := binary.LittleEndian.Uint32(r[off:]), binary.BigEndian.Uint32(r[off+4:])
		if le != be {
			panic(isoError{})
		}
		return int(le)
	}
	rec := &isoRecord{extent: both(2), size: both(10), flags: r[25]}
	idLen := int(r[32])
	pad := (fixed + idLen) & 1
	if len(r) < fixed+idLen+pad {
		panic(isoError{})
	}
	rec.fileID = r[fixed : fixed+idLen]
	rec.systemUse = r[fixed+idLen+pad:]
	return rec
}

func (r *isoRecord) isDir() bool {
	return r.flags&isoFlagDir != 0
}

// isSystem reports whether the record is "." or "..", which 7-Zip skips.
func (r *isoRecord) isSystem() bool {
	return len(r.fileID) == 1 && r.fileID[0] < 2
}

// readDir is ReadDir: it reads the records of the folder d, and then the
// folders among them. stack holds the extents of the folders above d.
func (a *isoArchive) readDir(d *isoRecord, level int, stack []int) {
	if !d.isDir() {
		return
	}
	// 7-Zip skips a folder too deep, or one that holds itself.
	if level > isoMaxLevel {
		return
	}
	for _, e := range stack {
		if e == d.extent {
			return
		}
	}
	stack = append(stack, d.extent)
	start := d.extent * isoBlockSize
	for pos := 0; pos < d.size; {
		n := int(a.read(start+pos, 1)[0])
		if n == 0 {
			// The rest of the block is zeros.
			end := min(d.size, (pos/isoBlockSize+1)*isoBlockSize)
			for _, c := range a.read(start+pos, end-pos) {
				if c != 0 {
					panic(isoError{})
				}
			}
			pos = end
			continue
		}
		if n > d.size-pos || pos%isoBlockSize+n > isoBlockSize {
			panic(isoError{})
		}
		sub := a.record(a.read(start+pos, n))
		pos += n
		// The first record of the root, ".", tells whether Rock Ridge
		// holds.
		if level == 0 && pos == n {
			a.susp, a.suspSkip = sub.checkSUSP()
		}
		if !sub.isSystem() {
			sub.parent = d
			d.children = append(d.children, sub)
		}
	}
	a.processed += d.size
	if a.processed > len(a.img)+isoMetadataSlack {
		panic(isoError{})
	}
	for _, c := range d.children {
		a.readDir(c, level+1, stack)
	}
}

// checkSUSP is CheckSusp: whether the system use area starts with the
// record SP, there or 14 bytes later, and the bytes to skip that it gives.
func (r *isoRecord) checkSUSP() (bool, int) {
	check := func(p []byte) (bool, int) {
		if len(p) < 7 || !bytes.Equal(p[:6], []byte{'S', 'P', 7, 1, 0xBE, 0xEF}) {
			return false, 0
		}
		return true, int(p[6])
	}
	if ok, skip := check(r.systemUse); ok {
		return true, skip
	}
	const offset2 = 14
	if len(r.systemUse) >= offset2 {
		return check(r.systemUse[offset2:])
	}
	return false, 0
}

// createRefs is CreateRefs: the items of the tree, each before the items
// in it. The records of the extents of one file make one item.
func (a *isoArchive) createRefs(d *isoRecord) {
	for i := 0; i < len(d.children); i++ {
		r := d.children[i]
		ref := isoRef{rec: r, extents: []*isoRecord{r}}
		for r.flags&isoFlagNonFinal != 0 && i+1 < len(d.children) {
			next := d.children[i+1]
			if !bytes.Equal(next.fileID, r.fileID) || next.flags&^isoFlagNonFinal != r.flags&^isoFlagNonFinal {
				break
			}
			i++
			ref.extents = append(ref.extents, next)
			r = next
		}
		a.refs = append(a.refs, ref)
		a.createRefs(ref.rec)
	}
}

// readBoot is ReadBootInfo: the entries of the catalog of El Torito at the
// block catalog. An entry that does not parse ends the list.
func (a *isoArchive) readBoot(catalog int) {
	pos := catalog * isoBlockSize
	next := func() []byte {
		b := a.read(pos, 32)
		pos += 32
		return b
	}
	v := next()
	if v[0] != 1 || v[2] != 0 || v[3] != 0 || v[30] != 0x55 || v[31] != 0xAA {
		return
	}
	var sum uint16
	for i := 0; i < 32; i += 2 {
		sum += binary.LittleEndian.Uint16(v[i:])
	}
	if sum != 0 {
		return
	}
	be, ok := parseISOBootEntry(next())
	if !ok {
		return
	}
	a.boot = append(a.boot, be)
	for {
		h := next()
		if h[0] != isoMoreHeaders && h[0] != isoFinalHeader {
			break
		}
		n := int(binary.LittleEndian.Uint16(h[2:]))
		for range n {
			be, ok := parseISOBootEntry(next())
			if !ok {
				break
			}
			// Extension records follow an entry whose media type has the
			// bit 5 set.
			if be.mediaType&0x20 != 0 {
				for range 33 {
					e := next()
					if e[0] != isoExtensionRecord || e[1]&0x20 == 0 {
						break
					}
				}
			}
			a.boot = append(a.boot, be)
		}
		if h[0] != isoMoreHeaders {
			break
		}
	}
}

// parseISOBootEntry is CBootInitialEntry::Parse.
func parseISOBootEntry(p []byte) (isoBootEntry, bool) {
	if p[5] != 0 || (p[0] != isoBootable && p[0] != isoNotBootable) {
		return isoBootEntry{}, false
	}
	be := isoBootEntry{
		bootable:    p[0] == isoBootable,
		mediaType:   p[1],
		sectorCount: int(binary.LittleEndian.Uint16(p[6:])),
		loadRBA:     int(binary.LittleEndian.Uint32(p[8:])),
	}
	copy(be.vendorSpec[:], p[12:32])
	return be, true
}

// name is CBootInitialEntry::GetName, such as "Boot-NoEmul.img".
func (be isoBootEntry) name() string {
	mediaTypes := []string{"NoEmul", "1.2M", "1.44M", "2.88M", "HardDisk"}
	var b strings.Builder
	if be.bootable {
		b.WriteString("Boot-")
	} else {
		b.WriteString("NotBoot-")
	}
	if int(be.mediaType) < len(mediaTypes) {
		b.WriteString(mediaTypes[be.mediaType])
	} else {
		b.WriteString(strconv.Itoa(int(be.mediaType)))
	}
	if be.vendorSpec[0] == 1 && !bytes.ContainsFunc(be.vendorSpec[1:], func(r rune) bool { return r > 0x7F }) {
		b.WriteByte('-')
		for _, c := range binwalk.CStringBytes(be.vendorSpec[1:]) {
			if c == '\\' || c == '/' {
				c = '_'
			}
			b.WriteByte(c)
		}
	}
	b.WriteString(".img")
	return b.String()
}

// bootSize is GetBootItemSize: the sectors of the entry, or the size of a
// floppy, or the rest of the image.
func (a *isoArchive) bootSize(i int) int {
	be := a.boot[i]
	size := be.sectorCount * 512
	switch be.mediaType {
	case 1:
		size = 1200 << 10
	case 2:
		size = 1440 << 10
	case 3:
		size = 2880 << 10
	}
	if start := be.loadRBA * isoBlockSize; start < len(a.img) {
		if rem := len(a.img) - start; i == a.expand || rem < size {
			size = rem
		}
	}
	return size
}

// findSUSP is FindSuspRecord: the data of the first record id of SUSP in
// the system use area of r, after its version. 7-Zip does not follow a
// record CE to a continuation area.
func (a *isoArchive) findSUSP(r *isoRecord, id string) ([]byte, bool) {
	if len(r.systemUse) < a.suspSkip {
		return nil, false
	}
	p := r.systemUse[a.suspSkip:]
	for len(p) >= 5 {
		n := int(p[2])
		if n < 3 || n > len(p) {
			return nil, false
		}
		if p[0] == id[0] && p[1] == id[1] && p[3] == 1 {
			if n < 4 {
				return nil, false
			}
			return p[4:n], true
		}
		p = p[n:]
	}
	return nil, false
}

// name is the name of r: in UCS-2 for Joliet, or the name of the record NM
// of Rock Ridge, or the identifier of the record.
func (a *isoArchive) name(r *isoRecord) string {
	if a.joliet {
		// GetPathU reads the identifier, never Rock Ridge.
		u := make([]uint16, 0, len(r.fileID)/2)
		for i := 0; i+1 < len(r.fileID); i += 2 {
			c := binary.BigEndian.Uint16(r.fileID[i:])
			if c == 0 {
				break
			}
			u = append(u, c)
		}
		return string(utf16.Decode(u))
	}
	if a.susp {
		if nm, ok := a.findSUSP(r, "NM"); ok && len(nm) >= 1 {
			// The first byte holds the flags of the name.
			return binwalk.CString(nm[1:])
		}
	}
	return binwalk.CString(r.fileID)
}

// path is the path of an item, as kpidPath of IsoHandler.cpp: the names
// from the root, without a last ";1" and then a last ".".
func (a *isoArchive) path(r *isoRecord) string {
	var names []string
	for ; r.parent != nil; r = r.parent {
		names = append(names, a.name(r))
	}
	var b strings.Builder
	for i := len(names) - 1; i >= 0; i-- {
		b.WriteString(names[i])
		if i > 0 {
			b.WriteByte('/')
		}
	}
	s := strings.TrimSuffix(b.String(), ";1")
	return strings.TrimSuffix(s, ".")
}

// mode is the posix mode of r, from the record PX of Rock Ridge, whose
// mode is in both byte orders.
func (a *isoArchive) mode(r *isoRecord) (int, bool) {
	if !a.susp {
		return 0, false
	}
	px, ok := a.findSUSP(r, "PX")
	if !ok || len(px) < 8 {
		return 0, false
	}
	le, be := binary.LittleEndian.Uint32(px), binary.BigEndian.Uint32(px[4:])
	if le != be {
		return 0, false
	}
	return int(le), true
}

// symlink is GetSymLink: the target of the record SL of Rock Ridge. 7-Zip
// reads one record, and does not follow the flag CONTINUE.
func (a *isoArchive) symlink(r *isoRecord) (string, bool) {
	p, ok := a.findSUSP(r, "SL")
	if !ok || len(p) < 1 || p[0] != 0 {
		return "", false
	}
	p = p[1:]
	var b strings.Builder
	for len(p) != 0 {
		if len(p) < 2 {
			return "", false
		}
		flags, n := p[0], int(p[1])
		p = p[2:]
		if n > len(p) {
			return "", false
		}
		slash := false
		switch {
		case flags&(1<<1) != 0:
			b.WriteString("./")
		case flags&(1<<2) != 0:
			b.WriteString("../")
		case flags&(1<<3) != 0:
			b.WriteByte('/')
		default:
			slash = true
		}
		b.Write(binwalk.CStringBytes(p[:n]))
		p = p[n:]
		if len(p) == 0 {
			break
		}
		if slash {
			b.WriteByte('/')
		}
	}
	return b.String(), true
}

// items lists the items in the order that 7zz extracts them: the tree,
// then the boot images in the folder "[BOOT]".
func (a *isoArchive) items() []szItem {
	items := make([]szItem, 0, len(a.refs)+len(a.boot))
	for _, ref := range a.refs {
		r := ref.rec
		it := szItem{path: a.path(r), dir: r.isDir()}
		it.mode, it.hasMode = a.mode(r)
		if !it.dir {
			if it.hasMode && it.mode&0xF000 == 0xA000 {
				it.target, it.link = a.symlink(r)
			}
			if !it.link {
				it.data = a.extents(ref.extents)
			}
		}
		items = append(items, it)
	}
	for i, be := range a.boot {
		name := "[BOOT]/"
		if len(a.boot) != 1 {
			name += strconv.Itoa(i+1) + "-"
		}
		start := be.loadRBA * isoBlockSize
		items = append(items, szItem{path: name + be.name(), data: a.span(start, a.bootSize(i))})
	}
	return items
}

// extents gives the data of the extents of a file, up to the end of the
// image.
func (a *isoArchive) extents(recs []*isoRecord) iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		for _, r := range recs {
			if r.size == 0 {
				continue
			}
			for b := range a.span(r.extent*isoBlockSize, r.size) {
				if !yield(b) {
					return
				}
			}
			// 7-Zip stops the file at an extent that the image cuts.
			if r.extent > len(a.img)/isoBlockSize || r.extent*isoBlockSize+r.size > len(a.img) {
				return
			}
		}
	}
}

// span gives size bytes at start, in parts, up to the end of the image.
func (a *isoArchive) span(start, size int) iter.Seq[[]byte] {
	const part = 1 << 20
	return func(yield func([]byte) bool) {
		if start < 0 || start >= len(a.img) {
			return
		}
		end := min(len(a.img), start+size)
		for p := start; p < end; p += part {
			if !yield(a.img[p:min(end, p+part)]) {
				return
			}
		}
	}
}
