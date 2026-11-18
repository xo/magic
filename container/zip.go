package container

import (
	"bufio"
	"compress/flate"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"
)

// The signatures and the sizes of the records of a zip file, as APPNOTE.TXT
// of PKWARE gives them.
const (
	zipLocalSig    = 0x04034b50
	zipCentralSig  = 0x02014b50
	zipEndSig      = 0x06054b50
	zip64LocSig    = 0x07064b50
	zip64EndSig    = 0x06064b50
	zipLocalLen    = 30
	zipCentralLen  = 46
	zipEndLen      = 22
	zip64LocLen    = 20
	zip64EndLen    = 56
	zipMaxComment  = 1<<16 - 1
	zip64ExtraID   = 0x0001
	zipEncrypted   = 0x0001
	zipStore       = 0
	zipDeflate     = 8
	zipPieceLimit  = 1 << 10
	zipCentralSize = 32 << 10
)

var le = binary.LittleEndian

// The values of the member mimetype of Krita and OpenRaster.
const (
	kritaMIME      = "application/x-krita"
	openRasterMIME = "image/openraster"
)

// The types of the relationship that names the sequence of the documents of
// an XPS document, and of an OpenXPS document.
const (
	xpsRelType  = "http://schemas.microsoft.com/xps/2005/06/fixedrepresentation"
	oxpsRelType = "http://schemas.openxps.org/oxps/v1.0/fixedrepresentation"
)

// zipMember is a member of the central directory that Detect can read.
type zipMember struct {
	method uint16
	flags  uint16
	csize  uint64 // the compressed size
	offset uint64 // the offset of the local header, from the base
}

// zipScan holds what the central directory tells about the format.
type zipScan struct {
	mimetype *zipMember
	rels     *zipMember
	pieces   map[int]*zipMember // the pieces of _rels/.rels, by number
	last     int                // the number of the last piece, or -1

	manifest bool // AndroidManifest.xml in the root
	dex      bool // classes.dex in the root
	arsc     bool // resources.arsc in the root
	app      bool // a member in Payload/<name>.app/

	images int // the members that are images
	others int // the members that are neither images nor allowed in a CBZ
}

// zipFile is a zip file that Detect reads.
type zipFile struct {
	r    io.ReaderAt
	size int64
	base int64 // the bytes before the zip file, such as a stub
	lim  Limits
}

// detectZip reads the central directory of the zip file, and the members
// mimetype and _rels/.rels when it holds them.
func detectZip(r io.ReaderAt, size int64, lim Limits) (Format, error) {
	z := &zipFile{r: r, size: size, lim: lim}
	start, n, err := z.directory()
	if err != nil {
		return Unknown, err
	}
	s := &zipScan{pieces: map[int]*zipMember{}, last: -1}
	if err := z.scan(s, start, n); err != nil {
		return Unknown, err
	}
	return z.format(s), nil
}

// format names the format from s. The member mimetype comes first, because
// it names the format of an Open Container. Then the relationship of XPS,
// then APK and IPA, which have their own members. CBZ comes last, because
// any zip file of images is one.
func (z *zipFile) format(s *zipScan) Format {
	if s.mimetype != nil {
		b := z.read(s.mimetype, int64(len(kritaMIME)+64))
		switch strings.TrimSpace(string(b)) {
		case kritaMIME:
			return Krita
		case openRasterMIME:
			return OpenRaster
		}
		// An EPUB or an OpenDocument file, which libmagic names.
		return Unknown
	}
	if f := relType(z.rels(s)); f != Unknown {
		return f
	}
	switch {
	case s.manifest && (s.dex || s.arsc):
		return APK
	case s.app:
		return IPA
	case s.images > 0 && s.others == 0:
		return CBZ
	}
	return Unknown
}

// directory finds the end of the central directory, and returns the offset
// of the central directory and its size.
func (z *zipFile) directory() (int64, int64, error) {
	if z.size < zipEndLen {
		return 0, 0, fmt.Errorf("%w: no end of the central directory", ErrCorrupt)
	}
	// The end record is in the last 22 bytes and the comment that follows it.
	n := min(z.size, zipEndLen+zipMaxComment)
	buf := make([]byte, n)
	if err := readFull(z.r, buf, z.size-n, "the end of the zip file"); err != nil {
		return 0, 0, err
	}
	i := len(buf) - zipEndLen
	for ; i >= 0; i-- {
		if le.Uint32(buf[i:]) == zipEndSig && i+zipEndLen+int(le.Uint16(buf[i+20:])) <= len(buf) {
			break
		}
	}
	if i < 0 {
		return 0, 0, fmt.Errorf("%w: no end of the central directory", ErrCorrupt)
	}
	end := z.size - n + int64(i)
	e := buf[i:]
	cdSize, cdOff := uint64(le.Uint32(e[12:])), uint64(le.Uint32(e[16:]))
	if le.Uint16(e[10:]) == 0xFFFF || cdSize == 0xFFFFFFFF || cdOff == 0xFFFFFFFF {
		if end64, size64, off64, ok, err := z.directory64(end); err != nil {
			return 0, 0, err
		} else if ok {
			end, cdSize, cdOff = end64, size64, off64
		}
	}
	if cdSize > uint64(end) || cdOff > uint64(end) {
		return 0, 0, fmt.Errorf("%w: the central directory is past its end", ErrCorrupt)
	}
	// The central directory is where its offset says. If it is not, bytes
	// come before the zip file, and it ends at the end record.
	for _, base := range []int64{0, end - int64(cdSize) - int64(cdOff)} {
		start := base + int64(cdOff)
		if base < 0 || start+int64(cdSize) > end {
			continue
		}
		if cdSize == 0 {
			z.base = base
			return start, 0, nil
		}
		var sig [4]byte
		if err := readFull(z.r, sig[:], start, "the central directory"); err != nil {
			return 0, 0, err
		}
		if le.Uint32(sig[:]) == zipCentralSig {
			z.base = base
			return start, int64(cdSize), nil
		}
	}
	return 0, 0, fmt.Errorf("%w: no central directory", ErrCorrupt)
}

// directory64 reads the end of the central directory of ZIP64, from the
// locator before the end record at end. It returns false when the file
// has no locator.
func (z *zipFile) directory64(end int64) (int64, uint64, uint64, bool, error) {
	if end < zip64LocLen {
		return 0, 0, 0, false, nil
	}
	var loc [zip64LocLen]byte
	if err := readFull(z.r, loc[:], end-zip64LocLen, "the ZIP64 locator"); err != nil {
		return 0, 0, 0, false, err
	}
	if le.Uint32(loc[:]) != zip64LocSig {
		return 0, 0, 0, false, nil
	}
	off, last := le.Uint64(loc[8:]), end-zip64LocLen-zip64EndLen
	if last < 0 || off > uint64(last) {
		return 0, 0, 0, false, fmt.Errorf("%w: the ZIP64 end is past the locator", ErrCorrupt)
	}
	var e [zip64EndLen]byte
	if err := readFull(z.r, e[:], int64(off), "the ZIP64 end"); err != nil {
		return 0, 0, 0, false, err
	}
	if le.Uint32(e[:]) != zip64EndSig {
		return 0, 0, 0, false, fmt.Errorf("%w: no ZIP64 end", ErrCorrupt)
	}
	return int64(off), le.Uint64(e[40:]), le.Uint64(e[48:]), true, nil
}

// scan reads the n bytes of the central directory at start into s. It keeps
// no name, so the memory that it takes does not grow with the members.
func (z *zipFile) scan(s *zipScan, start, n int64) error {
	br := bufio.NewReaderSize(io.NewSectionReader(z.r, start, n), zipCentralSize)
	var h [zipCentralLen]byte
	name := make([]byte, 0, 256)
	extra := make([]byte, 0, 64)
	for count := 0; ; count++ {
		if _, err := io.ReadFull(br, h[:]); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return zipError(err, "the central directory")
		}
		if le.Uint32(h[:]) != zipCentralSig {
			return fmt.Errorf("%w: member %d of the central directory has no signature", ErrCorrupt, count)
		}
		if count >= z.lim.Members {
			return fmt.Errorf("%w: more than %d members", ErrLimit, z.lim.Members)
		}
		name = slices.Grow(name[:0], int(le.Uint16(h[28:])))[:le.Uint16(h[28:])]
		extra = slices.Grow(extra[:0], int(le.Uint16(h[30:])))[:le.Uint16(h[30:])]
		if _, err := io.ReadFull(br, name); err != nil {
			return zipError(err, "the name of a member")
		}
		if _, err := io.ReadFull(br, extra); err != nil {
			return zipError(err, "the extra field of a member")
		}
		if _, err := br.Discard(int(le.Uint16(h[32:]))); err != nil {
			return zipError(err, "the comment of a member")
		}
		m := &zipMember{
			flags:  le.Uint16(h[8:]),
			method: le.Uint16(h[10:]),
			csize:  uint64(le.Uint32(h[20:])),
			offset: uint64(le.Uint32(h[42:])),
		}
		zip64(m, uint64(le.Uint32(h[24:])), extra)
		s.add(strings.ReplaceAll(string(name), `\`, "/"), m)
	}
}

// zipError returns the error of a read of what in the central directory. A
// read that ends early wraps ErrCorrupt.
func zipError(err error, what string) error {
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return fmt.Errorf("%w: %s ends after the central directory", ErrCorrupt, what)
	}
	return fmt.Errorf("container: reading %s: %w", what, err)
}

// zip64 reads the sizes and the offset of ZIP64 from the extra field of m,
// for each value of the central directory that is 0xFFFFFFFF. The extra
// field gives them in this order: the size, the compressed size and the
// offset.
func zip64(m *zipMember, usize uint64, extra []byte) {
	for len(extra) >= 4 {
		id, n := le.Uint16(extra), int(le.Uint16(extra[2:]))
		if 4+n > len(extra) {
			return
		}
		if id != zip64ExtraID {
			extra = extra[4+n:]
			continue
		}
		b := extra[4 : 4+n]
		next := func(v *uint64) {
			if *v == 0xFFFFFFFF && len(b) >= 8 {
				*v, b = le.Uint64(b), b[8:]
			}
		}
		next(&usize)
		next(&m.csize)
		next(&m.offset)
		return
	}
}

// add adds the member name to s.
func (s *zipScan) add(name string, m *zipMember) {
	switch name {
	case "mimetype":
		s.mimetype = m
	case "AndroidManifest.xml":
		s.manifest = true
	case "classes.dex":
		s.dex = true
	case "resources.arsc":
		s.arsc = true
	}
	// The names of the parts of an XPS document compare without case
	// (Open Packaging Conventions).
	lower := strings.ToLower(name)
	if lower == "_rels/.rels" {
		s.rels = m
	} else if rest, ok := strings.CutPrefix(lower, "_rels/.rels/["); ok {
		s.addPiece(rest, m)
	}
	if rest, ok := strings.CutPrefix(name, "Payload/"); ok {
		if dir, _, ok := strings.Cut(rest, "/"); ok && len(dir) > len(".app") && strings.HasSuffix(dir, ".app") {
			s.app = true
		}
	}
	switch comicMember(name) {
	case comicImage:
		s.images++
	case comicOther:
		s.others++
	}
}

// addPiece adds a piece of an interleaved _rels/.rels, whose name after
// "_rels/.rels/[" is rest, such as "0].piece" or "3].last.piece".
func (s *zipScan) addPiece(rest string, m *zipMember) {
	num, kind, ok := strings.Cut(rest, "]")
	i, err := strconv.Atoi(num)
	if !ok || err != nil || i < 0 || i >= zipPieceLimit {
		return
	}
	switch kind {
	case ".last.piece":
		s.last = i
	case ".piece":
	default:
		return
	}
	s.pieces[i] = m
}

// rels returns the bytes of _rels/.rels, or of its pieces in order, at most
// MemberSize bytes.
func (z *zipFile) rels(s *zipScan) []byte {
	limit := int64(z.lim.MemberSize)
	if s.rels != nil {
		return z.read(s.rels, limit)
	}
	var b []byte
	for i := 0; int64(len(b)) < limit; i++ {
		m := s.pieces[i]
		if m == nil {
			break
		}
		b = append(b, z.read(m, limit-int64(len(b)))...)
		if i == s.last {
			break
		}
	}
	return b
}

// read returns at most limit bytes of the data of m. It reads at most
// 4*limit+4096 compressed bytes, so a member of empty blocks of deflate
// ends. It returns the bytes that it can read and no error, because a
// member that it cannot read names no format.
func (z *zipFile) read(m *zipMember, limit int64) []byte {
	if m.flags&zipEncrypted != 0 || m.offset > uint64(z.size) {
		return nil
	}
	off := z.base + int64(m.offset)
	var h [zipLocalLen]byte
	if readFull(z.r, h[:], off, "a local header") != nil || le.Uint32(h[:]) != zipLocalSig {
		return nil
	}
	data := off + zipLocalLen + int64(le.Uint16(h[26:])) + int64(le.Uint16(h[28:]))
	if data > z.size {
		return nil
	}
	csize := int64(min(m.csize, uint64(z.size-data)))
	var src io.Reader
	switch m.method {
	case zipStore:
		src = io.NewSectionReader(z.r, data, min(csize, limit))
	case zipDeflate:
		// The reader of flate holds no resource, so it needs no Close.
		src = flate.NewReader(io.NewSectionReader(z.r, data, min(csize, 4*limit+4096)))
	default:
		return nil
	}
	// A member that ends early keeps the bytes before the fault.
	b, _ := io.ReadAll(io.LimitReader(src, limit))
	return b
}

// relType returns XPS or OpenXPS when b, the relationships of the package,
// holds the relationship of the sequence of the documents.
func relType(b []byte) Format {
	for _, t := range relTypes(b) {
		switch {
		case strings.EqualFold(t, xpsRelType):
			return XPS
		case strings.EqualFold(t, oxpsRelType):
			return OpenXPS
		}
	}
	return Unknown
}

// The kinds of members of comicMember.
const (
	comicAllowed = iota
	comicImage
	comicOther
)

// comicImages are the extensions of the images that a comic book archive
// holds, which comic book readers show.
var comicImages = []string{
	".avif", ".bmp", ".gif", ".jpe", ".jpeg", ".jpg", ".jxl", ".png", ".tif", ".tiff", ".webp",
}

// comicExtras are the extensions of the text files that a comic book archive
// can hold next to its images: notes and checksums.
var comicExtras = []string{".nfo", ".sfv", ".txt"}

// comicMember returns whether the member name is an image of a comic book
// archive, a member that a comic book archive can hold, or another member.
// A comic book archive can hold folders, ComicInfo.xml, notes, checksums,
// and the hidden files that macOS and Windows write.
func comicMember(name string) int {
	if name == "" || strings.HasSuffix(name, "/") || strings.HasPrefix(name, "__MACOSX/") {
		return comicAllowed
	}
	base := path.Base(name)
	ext := strings.ToLower(path.Ext(base))
	switch {
	case slices.Contains(comicImages, ext):
		return comicImage
	case strings.HasPrefix(base, "."),
		strings.EqualFold(base, "ComicInfo.xml"),
		strings.EqualFold(base, "Thumbs.db"),
		strings.EqualFold(base, "desktop.ini"),
		slices.Contains(comicExtras, ext):
		return comicAllowed
	}
	return comicOther
}
