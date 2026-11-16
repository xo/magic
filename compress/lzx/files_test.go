package lzx

import (
	"bytes"
	"encoding/binary"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"unicode/utf16"
)

// update writes the files of testdata again, from the encoder of the tests.
var update = flag.Bool("update", false, "write the files of testdata again")

// The sample that the files of testdata hold, and how they compress it. The
// CHM file resets every 2 frames.
const (
	fileLength   = 150000
	fileSeed     = 23
	fileWindow   = 1 << 16
	fileInterval = 2
	fileE8Size   = 4000000
)

// fileEncoder returns the encoder of the files of testdata.
func fileEncoder(interval int) encoder {
	return encoder{
		windowSize: fileWindow,
		interval:   interval,
		e8size:     fileE8Size,
		blockSize:  20001,
		kinds:      []int{verbatim, alignedBlock, uncompressed},
	}
}

// TestCAB decodes testdata/sample.cab, a cabinet with one file that LZX
// compresses. cabextract and 7z extract the same file from it. With -update,
// the test writes it again.
func TestCAB(t *testing.T) {
	t.Parallel()
	data := sample(fileLength, fileSeed)
	path := filepath.Join("testdata", "sample.cab")
	if *update {
		enc := fileEncoder(0)
		if err := os.WriteFile(path, cab(enc.encode(data), len(data)), 0o644); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	// The folder starts after the header, and its first frame where the
	// folder says.
	pos := int(binary.LittleEndian.Uint32(b[36:]))
	count := int(binary.LittleEndian.Uint16(b[40:]))
	d, err := NewDecoder(1 << (b[43] & 0x1F))
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	var got []byte
	for range count {
		n := int(binary.LittleEndian.Uint16(b[pos+4:]))
		size := int(binary.LittleEndian.Uint16(b[pos+6:]))
		if got, err = d.Decode(got, b[pos+8:pos+8+n], size); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		pos += 8 + n
	}
	if !bytes.Equal(got, data) {
		t.Errorf("the file of the cabinet differs from the sample at byte %d", diff(got, data))
	}
}

// cab returns a cabinet that holds one file, sample.txt, in one folder of
// LZX, with the frames of the file.
func cab(frames [][]byte, length int) []byte {
	const name = "sample.txt\x00"
	header := 36 + 8
	files := header + 16 + len(name)
	size := files
	for _, f := range frames {
		size += 8 + len(f)
	}
	le16 := binary.LittleEndian.AppendUint16
	le32 := binary.LittleEndian.AppendUint32
	b := []byte("MSCF")
	b = le32(b, 0)
	b = le32(b, uint32(size))
	b = le32(b, 0)
	b = le32(b, uint32(header)) // the first file
	b = le32(b, 0)
	b = append(b, 3, 1) // version 1.3
	b = le16(b, 1)      // folders
	b = le16(b, 1)      // files
	b = le16(b, 0)      // flags
	b = le16(b, 0x1234) // set
	b = le16(b, 0)      // index in the set
	// The folder: its first frame, the number of frames, and LZX with its
	// window.
	b = le32(b, uint32(files))
	b = le16(b, uint16(len(frames)))
	b = le16(b, 3|16<<8)
	// The file: its size, its offset in the folder, its folder, a date, a
	// time, and the attribute of an archive.
	b = le32(b, uint32(length))
	b = le32(b, 0)
	b = le16(b, 0)
	b = le16(b, 0x5A21)
	b = le16(b, 0x6000)
	b = le16(b, 0x20)
	b = append(b, name...)
	for i, f := range frames {
		b = le32(b, 0) // no checksum
		b = le16(b, uint16(len(f)))
		b = le16(b, uint16(min(FrameSize, length-i*FrameSize)))
		b = append(b, f...)
	}
	return b
}

// TestCHM decodes testdata/sample.chm, a CHM file with one file, which LZX
// compresses with a reset every 2 frames. 7z extracts the same file from it.
// With -update, the test writes it again.
//
// 7z reads the CHM file only when the reset table has an entry for the end
// of the data, the reset interval is a power of two, and the content fills
// its last frame. So the content is the sample and zeros up to the end of
// its last frame. Section reads the other forms too.
func TestCHM(t *testing.T) {
	t.Parallel()
	data := sample(fileLength, fileSeed)
	path := filepath.Join("testdata", "sample.chm")
	if *update {
		frames := (len(data) + FrameSize - 1) / FrameSize
		content := append(bytes.Clone(data), make([]byte, frames*FrameSize-len(data))...)
		enc := fileEncoder(fileInterval)
		if err := os.WriteFile(path, chm(enc.encode(content), len(content), len(data)), 0o644); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	// The test reads the parts of section 0 at the offsets that chm writes
	// them.
	lzxc := int(binary.LittleEndian.Uint64(b[0x58:]))
	if string(b[lzxc+4:lzxc+8]) != "LZXC" {
		t.Fatalf("expected %q, got: %q", "LZXC", b[lzxc+4:lzxc+8])
	}
	table := lzxc + 28
	count := int(binary.LittleEndian.Uint32(b[table+4:]))
	s := &Section{
		WindowSize:    int(binary.LittleEndian.Uint32(b[lzxc+16:])) * FrameSize,
		ResetInterval: int(binary.LittleEndian.Uint32(b[lzxc+12:])),
		Length:        int64(binary.LittleEndian.Uint64(b[table+16:])),
	}
	for i := range count {
		s.ResetTable = append(s.ResetTable, binary.LittleEndian.Uint64(b[table+40+8*i:]))
	}
	start := table + 40 + 8*count
	s.Data = b[start : start+int(binary.LittleEndian.Uint64(b[table+24:]))]
	got, err := s.Decode(nil, 70000, 50000)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !bytes.Equal(got, data[70000:120000]) {
		t.Errorf("the part of the CHM file differs from the sample at byte %d", diff(got, data[70000:120000]))
	}
	got, err = s.Decode(nil, 0, fileLength)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if !bytes.Equal(got, data) {
		t.Errorf("the file of the CHM file differs from the sample at byte %d", diff(got, data))
	}
}

// chm returns a CHM file that holds one file, /sample.txt, of fileLen bytes,
// in the section MSCompressed. frames are the frames of the content, which
// has length bytes, with a reset every fileInterval frames. The reset table
// has an entry for each frame and one for the end. Section 0, which is not
// compressed, holds the control data, the reset table and the compressed
// data, in this order, at its start, and then the name list, the span
// information and the list of transforms.
func chm(frames [][]byte, length, fileLen int) []byte {
	le32 := binary.LittleEndian.AppendUint32
	le64 := binary.LittleEndian.AppendUint64
	guid := func(b []byte, s string) []byte {
		// s is a GUID as {01234567-89AB-CDEF-0123-456789ABCDEF}.
		hex := func(s string) []byte {
			var out []byte
			for i := 0; i < len(s); i += 2 {
				var v byte
				for _, c := range s[i : i+2] {
					v <<= 4
					switch {
					case c >= '0' && c <= '9':
						v |= byte(c - '0')
					default:
						v |= byte(c - 'A' + 10)
					}
				}
				out = append(out, v)
			}
			return out
		}
		d1, d2, d3 := hex(s[1:9]), hex(s[10:14]), hex(s[15:19])
		b = append(b, d1[3], d1[2], d1[1], d1[0], d2[1], d2[0], d3[1], d3[0])
		b = append(b, hex(s[20:24])...)
		return append(b, hex(s[25:37])...)
	}
	utf16le := func(s string) []byte {
		var out []byte
		for _, u := range utf16.Encode([]rune(s)) {
			out = binary.LittleEndian.AppendUint16(out, u)
		}
		return out
	}

	// Section 0.
	var compressed []byte
	offsets := make([]uint64, 0, len(frames)+1)
	for _, f := range frames {
		offsets = append(offsets, uint64(len(compressed)))
		compressed = append(compressed, f...)
	}
	offsets = append(offsets, uint64(len(compressed)))
	lzxc := le32(nil, 6)
	lzxc = append(lzxc, "LZXC"...)
	lzxc = le32(lzxc, 2)
	lzxc = le32(lzxc, fileInterval)
	lzxc = le32(lzxc, fileWindow/FrameSize)
	lzxc = le32(lzxc, 1)
	lzxc = le32(lzxc, 0)
	table := le32(nil, 2)
	table = le32(table, uint32(len(offsets)))
	table = le32(table, 8)
	table = le32(table, 0x28)
	table = le64(table, uint64(length))
	table = le64(table, uint64(len(compressed)))
	table = le64(table, FrameSize)
	for _, o := range offsets {
		table = le64(table, o)
	}
	names := []string{"Uncompressed", "MSCompressed"}
	var nameList []byte
	nameList = binary.LittleEndian.AppendUint16(nameList, 0)
	nameList = binary.LittleEndian.AppendUint16(nameList, uint16(len(names)))
	for _, n := range names {
		nameList = binary.LittleEndian.AppendUint16(nameList, uint16(len(n)))
		nameList = append(nameList, utf16le(n)...)
		nameList = binary.LittleEndian.AppendUint16(nameList, 0)
	}
	binary.LittleEndian.PutUint16(nameList, uint16(len(nameList)/2))
	span := le64(nil, uint64(length))
	transforms := utf16le("{7FC28940-9D31-11D0-9B27-00A0C91E9C7C}")
	const prefix = "::DataSpace/Storage/MSCompressed/"
	type entry struct {
		name      string
		section   int
		off, size int
	}
	var section0 []byte
	add := func(name string, data []byte) entry {
		e := entry{name, 0, len(section0), len(data)}
		section0 = append(section0, data...)
		return e
	}
	entries := []entry{
		add(prefix+"ControlData", lzxc),
		add(prefix+"Transform/{7FC28940-9D31-11D0-9B27-00A0C91E9C7C}/InstanceData/ResetTable", table),
		add(prefix+"Content", compressed),
		add("::DataSpace/NameList", nameList),
		add(prefix+"SpanInfo", span),
		add(prefix+"Transform/List", transforms),
		{"/sample.txt", 1, 0, fileLen},
	}
	// A directory chunk lists its entries in the order of their names,
	// without case.
	slicesSortByName(entries, func(e entry) string { return e.name })

	// The directory: the header ITSP, then one chunk PMGL.
	const chunkSize = 0x1000
	encint := func(b []byte, v int) []byte {
		var tmp []byte
		for {
			tmp = append([]byte{byte(v & 0x7F)}, tmp...)
			if v >>= 7; v == 0 {
				break
			}
		}
		for i := range len(tmp) - 1 {
			tmp[i] |= 0x80
		}
		return append(b, tmp...)
	}
	chunk := append([]byte("PMGL"), make([]byte, 16)...)
	for _, e := range entries {
		chunk = encint(chunk, len(e.name))
		chunk = append(chunk, e.name...)
		chunk = encint(chunk, e.section)
		chunk = encint(chunk, e.off)
		chunk = encint(chunk, e.size)
	}
	free := chunkSize - len(chunk)
	binary.LittleEndian.PutUint32(chunk[4:], uint32(free))
	binary.LittleEndian.PutUint32(chunk[12:], 0xFFFFFFFF)
	binary.LittleEndian.PutUint32(chunk[16:], 0xFFFFFFFF)
	chunk = append(chunk, make([]byte, free)...)
	binary.LittleEndian.PutUint16(chunk[chunkSize-2:], uint16(len(entries)))
	itsp := []byte("ITSP")
	for _, v := range []uint32{1, 0x54, 0x0A, chunkSize, 2, 1, 0xFFFFFFFF, 0, 0, 0xFFFFFFFF, 1, 0x409} {
		itsp = le32(itsp, v)
	}
	itsp = guid(itsp, "{5D02926A-212E-11D0-9DF9-00A0C922E6EC}")
	itsp = le32(itsp, 0x54)
	for range 3 {
		itsp = le32(itsp, 0xFFFFFFFF)
	}
	itsp = append(itsp, chunk...)
	directory := itsp

	// The header ITSF, then header section 0, the directory and section 0.
	const itsfSize, hs0Size = 0x60, 0x18
	dirOff := itsfSize + hs0Size
	content := dirOff + len(directory)
	total := content + len(section0)
	b := []byte("ITSF")
	b = le32(b, 3)
	b = le32(b, itsfSize)
	b = le32(b, 1)
	b = binary.BigEndian.AppendUint32(b, 0x12345678)
	b = le32(b, 0x409)
	b = guid(b, "{7C01FD10-7BAA-11D0-9E0C-00A0C922E6EC}")
	b = guid(b, "{7C01FD11-7BAA-11D0-9E0C-00A0C922E6EC}")
	b = le64(b, itsfSize)
	b = le64(b, hs0Size)
	b = le64(b, uint64(dirOff))
	b = le64(b, uint64(len(directory)))
	b = le64(b, uint64(content))
	b = le32(b, 0x01FE)
	b = le32(b, 0)
	b = le64(b, uint64(total))
	b = le32(b, 0)
	b = le32(b, 0)
	b = append(b, directory...)
	return append(b, section0...)
}

// slicesSortByName sorts entries by the lower case of their names.
func slicesSortByName[E any](entries []E, name func(E) string) {
	for i := 1; i < len(entries); i++ {
		for j := i; j > 0 && bytes.Compare(bytes.ToLower([]byte(name(entries[j-1]))), bytes.ToLower([]byte(name(entries[j])))) > 0; j-- {
			entries[j-1], entries[j] = entries[j], entries[j-1]
		}
	}
}
