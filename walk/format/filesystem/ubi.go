//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/ubi.rs and src/structures/ubi.rs of
// binwalk. binwalk extracts an UBI image with ubireader_extract_images, and
// an UBIFS image with ubireader_extract_files, from src/extractors/ubi.rs.
// extractUBI in this file and extractUBIFS in ubifs.go write the files that
// those programs write. They copy the behavior of ubi_reader, not its code.

package filesystem

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// UBI is the signature "ubi" of binwalk.
var UBI = &walk.Signature{
	Name:        "ubi",
	Magic:       [][]byte{[]byte("UBI#\x01")},
	Description: "UBI image",
	Parse:       parseUBI,
	Extract:     extractUBI,
}

// UBIFS is the signature "ubifs" of binwalk. The magic matches every node
// of UBIFS, and the parser accepts only a superblock.
var UBIFS = &walk.Signature{
	Name:        "ubifs",
	Magic:       [][]byte{[]byte("\x31\x18\x10\x06")},
	Description: "UBIFS image",
	Parse:       parseUBIFS,
	Extract:     extractUBIFS,
}

// parseUBIFS is ubifs_parser. The size is the number of logical erase
// blocks times their size, which can be more than the data holds.
func parseUBIFS(data []byte, off int) (walk.Match, error) {
	lebSize, lebCount, ok := ubifsSuperblock(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	size := lebCount * lebSize
	return walk.Match{
		Offset:      off,
		Size:        size,
		Confidence:  walk.High,
		Description: fmt.Sprintf("UBIFS image, total size: %d bytes", size),
	}, nil
}

// parseUBI is ubi_parser.
func parseUBI(data []byte, off int) (walk.Match, error) {
	h, ok := ubiECHeader(data[off:])
	if !ok || len(data) <= off+h.dataOffset || len(data) <= off+h.vidOffset {
		return walk.Match{}, walk.ErrNoMatch
	}
	size, ok := ubiImageSize(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:      off,
		Size:        size,
		Confidence:  walk.High,
		Description: fmt.Sprintf("UBI image, version: %d, image size: %d bytes", h.version, size),
	}, nil
}

// ubiImageSize is get_ubi_image_size. It finds each valid header of a
// volume, and takes the distance that is most common between two of them
// as the size of an erase block. The size of the image is that distance
// times the number of headers.
//
// binwalk counts the distances in a HashMap, and of two distances with the
// same count, it takes the one that the map gives first, which is random.
// The walker takes the smaller one.
func ubiImageSize(data []byte) (int, bool) {
	counts := map[int]int{}
	blocks, prev := 0, 0
	for i := 0; ; {
		j := bytes.Index(data[i:], []byte("UBI!\x01"))
		if j < 0 {
			break
		}
		at := i + j
		i = at + 1
		if !ubiVolumeHeader(data[at:]) {
			continue
		}
		blocks++
		if prev != 0 {
			counts[at-prev]++
		}
		prev = at
	}
	leb, best := 0, 0
	for _, size := range slices.Sorted(maps.Keys(counts)) {
		if counts[size] > best {
			leb, best = size, counts[size]
		}
	}
	if leb == 0 || blocks == 0 {
		return 0, false
	}
	return blocks * leb, true
}

var (
	ubifsSuperblockStruct = binwalk.Struct{
		{Name: "magic", Type: "u32"},
		{Name: "header_crc", Type: "u32"},
		{Name: "sequence_number", Type: "u64"},
		{Name: "node_len", Type: "u32"},
		{Name: "node_type", Type: "u8"},
		{Name: "group_type", Type: "u8"},
		{Name: "padding1", Type: "u32"},
		{Name: "key_hash", Type: "u8"},
		{Name: "key_format", Type: "u8"},
		{Name: "flags", Type: "u32"},
		{Name: "min_io_size", Type: "u32"},
		{Name: "leb_size", Type: "u32"},
		{Name: "leb_count", Type: "u32"},
		{Name: "max_leb_count", Type: "u32"},
		{Name: "max_bud_bytes", Type: "u64"},
		{Name: "log_lebs", Type: "u32"},
		{Name: "lpt_lebs", Type: "u32"},
		{Name: "orph_lebs", Type: "u32"},
		{Name: "jhead_count", Type: "u32"},
		{Name: "fanout", Type: "u32"},
		{Name: "lsave_count", Type: "u32"},
		{Name: "fmt_version", Type: "u32"},
		{Name: "default_compression", Type: "u16"},
		{Name: "padding2", Type: "u16"},
		{Name: "rp_uid", Type: "u32"},
		{Name: "rp_gid", Type: "u32"},
		{Name: "rp_size", Type: "u64"},
		{Name: "time_gran", Type: "u32"},
		{Name: "uuid_p1", Type: "u64"},
		{Name: "uuid_p2", Type: "u64"},
		{Name: "ro_compat_version", Type: "u32"},
	}
	ubiECStruct = binwalk.Struct{
		{Name: "magic", Type: "u32"},
		{Name: "version", Type: "u8"},
		{Name: "padding1", Type: "u24"},
		{Name: "ec", Type: "u64"},
		{Name: "volume_id_header_offset", Type: "u32"},
		{Name: "data_offset", Type: "u32"},
		{Name: "image_sequence_number", Type: "u32"},
		{Name: "padding2", Type: "u64"},
		{Name: "padding3", Type: "u64"},
		{Name: "padding4", Type: "u64"},
		{Name: "padding5", Type: "u64"},
		{Name: "header_crc", Type: "u32"},
	}
	ubiVolumeStruct = binwalk.Struct{
		{Name: "magic", Type: "u32"},
		{Name: "version", Type: "u8"},
		{Name: "volume_type", Type: "u8"},
		{Name: "copy_flag", Type: "u8"},
		{Name: "compat_type", Type: "u8"},
		{Name: "volume_id", Type: "u32"},
		{Name: "logical_erase_block_number", Type: "u32"},
		{Name: "padding1", Type: "u32"},
		{Name: "data_size", Type: "u32"},
		{Name: "used_erase_block_count", Type: "u32"},
		{Name: "data_padding_size", Type: "u32"},
		{Name: "data_crc", Type: "u32"},
		{Name: "padding2", Type: "u32"},
		{Name: "sequence_number", Type: "u64"},
		{Name: "padding3", Type: "u64"},
		{Name: "padding4", Type: "u32"},
		{Name: "header_crc", Type: "u32"},
	}
)

// ubifsSuperblock is parse_ubi_superblock_header. The CRC covers the
// superblock node from its byte 8 to its byte 4096.
func ubifsSuperblock(data []byte) (lebSize, lebCount int, ok bool) {
	const (
		maxGroupType  = 2
		crcStart      = 8
		superblock    = 6
		extraFieldLen = 3968
	)
	f, ok := ubifsSuperblockStruct.Parse(data, false)
	if !ok || f["padding1"] != 0 || f["padding2"] != 0 || f["node_type"] != superblock || f["group_type"] > maxGroupType {
		return 0, 0, false
	}
	crcData, ok := binwalk.Range(data, crcStart, ubifsSuperblockStruct.Size()+extraFieldLen)
	if !ok || ubiCRC(crcData) != uint32(f["header_crc"]) {
		return 0, 0, false
	}
	return f["leb_size"], f["leb_count"], true
}

// ubiEC is what binwalk takes from an erase count header.
type ubiEC struct {
	version, dataOffset, vidOffset int
}

// ubiECHeader is parse_ubi_ec_header.
func ubiECHeader(data []byte) (ubiEC, bool) {
	size := ubiECStruct.Size()
	f, ok := ubiECStruct.Parse(data, true)
	if !ok || f["data_offset"] < size || f["volume_id_header_offset"] < size {
		return ubiEC{}, false
	}
	if ubiCRC(data[:size-4]) != uint32(f["header_crc"]) {
		return ubiEC{}, false
	}
	return ubiEC{version: f["version"], dataOffset: f["data_offset"], vidOffset: f["volume_id_header_offset"]}, true
}

// ubiVolumeHeader is parse_ubi_volume_header.
func ubiVolumeHeader(data []byte) bool {
	size := ubiVolumeStruct.Size()
	f, ok := ubiVolumeStruct.Parse(data, true)
	if !ok || f["padding1"] != 0 || f["padding2"] != 0 || f["padding3"] != 0 || f["padding4"] != 0 {
		return false
	}
	return ubiCRC(data[:size-4]) == uint32(f["header_crc"])
}

// ubiCRC is ubi_crc: the CRC-32 of zlib, with no final inversion, as UBI
// and UBIFS compute it.
func ubiCRC(data []byte) uint32 {
	return ^binwalk.CRC32(data)
}

// The constants of ubi_reader for an UBI image.
const (
	ubiChunk         = 5 << 20    // FILE_CHUNK_SZ, the size of a read of the guesses
	ubiInternalStart = 0x7FFFEFFF // UBI_INTERNAL_VOL_START, the first internal volume
	ubiMaxVolumes    = 128        // UBI_MAX_VOLUMES, the records of the table of volumes
	ubiVtblRecSize   = 172        // UBI_VTBL_REC_SZ
	ubiStatic        = 2          // UBI_VID_STATIC
)

// ubiBlock is one physical erase block of an UBI image, as the description
// of a block of ubi_reader.
type ubiBlock struct {
	peb        int // the index of the block, which is its key
	fileOffset int

	// The erase count header.
	vidOffset, dataOffset int
	imageSeq              uint32

	// The volume header, when its CRC is valid.
	vidValid bool
	volType  uint8
	copyFlag uint8
	volID    uint32
	lnum     int
	dataSize int
	dataPad  int
	vidCRC   uint32 // the CRC of the data that the header gives
	sqnum    uint64

	dataCRC  uint32 // the CRC of the data that the block holds
	valid    bool
	vtblRecs []ubiVtblRec
}

// ubiVtblRec is one valid record of the table of volumes.
type ubiVtblRec struct {
	index int
	name  []byte
}

// ubiPEBSize is guess_peb_size of ubi_reader. It keeps the offsets of the
// magic of each erase count header and takes the most common difference
// between two of them. ubi_reader adds the offset of the second header to
// each offset after it, and it compares the first offset with the last. The
// walker does the same, so that it gets the same size: with fewer than four
// headers, the size is wrong, and the extraction fails as it does in
// ubi_reader.
func ubiPEBSize(img []byte) int {
	fileOffset := 0
	var offsets []int
	for c := 0; c < len(img)+1; c += ubiChunk {
		buf := img[min(c, len(img)):min(c+ubiChunk, len(img))]
		for i := 0; ; {
			j := bytes.Index(buf[i:], []byte("UBI#"))
			if j < 0 {
				break
			}
			start := i + j
			i = start + 4
			idx := start + fileOffset
			if fileOffset == 0 {
				fileOffset, idx = start, start
			}
			offsets = append(offsets, idx)
		}
		fileOffset += ubiChunk
	}
	var order []int
	counts := map[int]int{}
	for i := range offsets {
		prev := offsets[len(offsets)-1]
		if i > 0 {
			prev = offsets[i-1]
		}
		d := offsets[i] - prev
		if _, ok := counts[d]; !ok {
			order = append(order, d)
		}
		counts[d]++
	}
	size, best := 0, 0
	for _, d := range order {
		if counts[d] > best {
			size, best = d, counts[d]
		}
	}
	return size
}

// ubiBlocks is extract_blocks of ubi_reader: it reads each erase block that
// starts with the magic of an erase count header. It fails where ubi_reader
// stops with an error: a header that is cut off, or an erase count header
// whose CRC is not valid.
func ubiBlocks(img []byte, peb int) ([]*ubiBlock, bool) {
	var blocks []*ubiBlock
	for i := 0; i < len(img); i += peb {
		buf := img[i:min(i+peb, len(img))]
		if !bytes.HasPrefix(buf, []byte("UBI#")) {
			continue
		}
		b, ok := ubiDescribe(buf)
		if !ok {
			return nil, false
		}
		b.peb, b.fileOffset = i/peb, i
		blocks = append(blocks, b)
	}
	return blocks, true
}

// ubiDescribe reads the headers of one erase block.
func ubiDescribe(buf []byte) (*ubiBlock, bool) {
	if len(buf) < 64 || ubiCRC(buf[:60]) != binary.BigEndian.Uint32(buf[60:]) {
		return nil, false
	}
	b := &ubiBlock{
		vidOffset:  int(binary.BigEndian.Uint32(buf[16:])),
		dataOffset: int(binary.BigEndian.Uint32(buf[20:])),
		imageSeq:   binary.BigEndian.Uint32(buf[24:]),
		lnum:       -1,
	}
	v, ok := binwalk.Range(buf, b.vidOffset, b.vidOffset+64)
	if !ok {
		return nil, false
	}
	b.volType, b.copyFlag = v[5], v[6]
	b.volID = binary.BigEndian.Uint32(v[8:])
	b.dataSize = int(binary.BigEndian.Uint32(v[20:]))
	b.dataPad = int(binary.BigEndian.Uint32(v[28:]))
	b.vidCRC = binary.BigEndian.Uint32(v[32:])
	b.sqnum = binary.BigEndian.Uint64(v[40:])
	b.vidValid = ubiCRC(v[:60]) == binary.BigEndian.Uint32(v[60:])
	if b.vidValid {
		b.lnum = int(binary.BigEndian.Uint32(v[12:]))
		if b.volID >= ubiInternalStart {
			b.vtblRecs = ubiVtbl(buf[min(b.dataOffset, len(buf)):])
		}
	}
	b.valid = b.vidValid
	b.dataCRC = ubiCRC(buf[min(b.dataOffset, len(buf)):min(b.dataOffset+b.dataSize, len(buf))])
	return b, true
}

// ubiVtbl reads the records of the table of volumes. It keeps a record whose
// CRC is valid and whose name is not empty.
func ubiVtbl(buf []byte) []ubiVtblRec {
	var recs []ubiVtblRec
	for i := range ubiMaxVolumes {
		r, ok := binwalk.Range(buf, i*ubiVtblRecSize, (i+1)*ubiVtblRecSize)
		if !ok {
			continue
		}
		n := int(binary.BigEndian.Uint16(r[14:]))
		if n == 0 || ubiCRC(r[:168]) != binary.BigEndian.Uint32(r[168:]) {
			continue
		}
		recs = append(recs, ubiVtblRec{index: i, name: r[16 : 16+min(n, 128)]})
	}
	return recs
}

// ubiRemoveOld is rm_old_blocks of ubi_reader. Of two valid blocks of one
// logical block and one image, it keeps the newer one, unless its copy flag
// is set and its data does not match its CRC.
func ubiRemoveOld(blocks map[int]*ubiBlock, list []int) []int {
	del := map[int]bool{}
	for _, i := range list {
		if del[i] {
			continue
		}
		if !blocks[i].valid {
			del[i] = true
			continue
		}
		for _, k := range list {
			if i == k || del[k] {
				continue
			}
			bi, bk := blocks[i], blocks[k]
			if !bk.valid {
				del[k] = true
				continue
			}
			if bi.lnum != bk.lnum || bi.imageSeq != bk.imageSeq {
				continue
			}
			newer, older := bi, k
			if bk.sqnum > bi.sqnum {
				newer, older = bk, i
			}
			if newer.copyFlag != 0 && newer.dataCRC != newer.vidCRC {
				del[newer.peb] = true
			} else {
				del[older] = true
			}
			break
		}
	}
	var out []int
	for _, i := range list {
		if !del[i] {
			out = append(out, i)
		}
	}
	return out
}

// ubiVolume is one volume of an image: its name and its blocks.
type ubiVolume struct {
	name   string
	blocks []int
}

// ubiImage is one image of an UBI file, with its volumes in the order of
// the table of volumes.
type ubiImage struct {
	seq     uint32
	volumes []ubiVolume
}

// ubiImages is the class ubi of ubi_reader. It finds the layout blocks,
// which hold the table of volumes, makes an image of each image sequence
// number, and gives each volume its blocks. It fails where ubi_reader stops
// with an error.
func ubiImages(blocks []*ubiBlock) ([]ubiImage, bool) {
	byPEB := map[int]*ubiBlock{}
	all := make([]int, 0, len(blocks))
	var layout []int
	for _, b := range blocks {
		byPEB[b.peb] = b
		all = append(all, b.peb)
		if len(b.vtblRecs) > 0 && b.valid {
			layout = append(layout, b.peb)
		}
	}
	layout = ubiRemoveOld(byPEB, layout)
	if len(layout) < 2 {
		return nil, false
	}
	// group_pairs: the layout blocks by image sequence number, in the order
	// that each number first comes.
	var groups [][]int
	for _, p := range layout {
		i := slices.IndexFunc(groups, func(g []int) bool { return byPEB[g[0]].imageSeq == byPEB[p].imageSeq })
		if i < 0 {
			groups = append(groups, []int{p})
		} else {
			groups[i] = append(groups[i], p)
		}
	}
	var images []ubiImage
	for _, g := range groups {
		first := byPEB[g[0]]
		// associate_blocks: every other block of the image sequence number.
		var data []int
		for _, p := range all {
			if byPEB[p].imageSeq == first.imageSeq && !slices.Contains(g, p) {
				data = append(data, p)
			}
		}
		// The image takes the smallest and the largest of these blocks, and
		// ubi_reader stops when there is none.
		if len(data) == 0 {
			return nil, false
		}
		// by_vol_id: the valid blocks of each volume.
		vols := map[uint32][]int{}
		for _, p := range data {
			if byPEB[p].valid {
				vols[byPEB[p].volID] = append(vols[byPEB[p].volID], p)
			}
		}
		img := ubiImage{seq: first.imageSeq}
		for _, r := range first.vtblRecs {
			name := strings.Trim(string(r.name), "\x00")
			if !utf8.ValidString(name) {
				return nil, false
			}
			v := ubiVolume{name: name, blocks: ubiRemoveOld(byPEB, vols[uint32(r.index)])}
			// A later record of the same name replaces the volume, in the
			// place of the first one, as a dict of Python does.
			if i := slices.IndexFunc(img.volumes, func(u ubiVolume) bool { return u.name == name }); i >= 0 {
				img.volumes[i] = v
			} else {
				img.volumes = append(img.volumes, v)
			}
		}
		images = append(images, img)
	}
	return images, true
}

// extractUBI writes the volumes of the image, as ubireader_extract_images
// writes them. binwalk runs it on a copy of the match, named
// "ubi_<offset>.img" with the offset in upper case hexadecimal, and
// ubireader_extract_images writes each volume of each image in the folder
// "ubifs-root/<name of that copy>", as
// "img-<image sequence number>_vol-<name of the volume>.ubifs". A volume
// holds its logical erase blocks in order, and a missing one is a block of
// bytes 0xFF. A volume that has no block gives no file.
//
// The walker then finds the UBIFS image in each file, which binwalk
// extracts in a walk of members with ubireader_extract_files.
func extractUBI(data []byte, off int, out *walk.Output) walk.Extraction {
	m, err := parseUBI(data, off)
	if err != nil {
		return walk.Extraction{}
	}
	if out == nil {
		return walk.Extraction{OK: true}
	}
	// binwalk writes the bytes of the match to a file, and fails when the
	// match is past the end of the data.
	img, ok := binwalk.Range(data, off, off+m.Size)
	if !ok {
		return walk.Extraction{}
	}
	peb := ubiPEBSize(img)
	if peb <= 0 {
		return walk.Extraction{}
	}
	blocks, ok := ubiBlocks(img, peb)
	if !ok || len(blocks) == 0 {
		return walk.Extraction{}
	}
	lebSize := peb - blocks[0].dataOffset
	images, ok := ubiImages(blocks)
	if !ok {
		return walk.Extraction{}
	}
	byPEB := map[int]*ubiBlock{}
	for _, b := range blocks {
		byPEB[b.peb] = b
	}
	dir := "ubifs-root/ubi_" + strings.ToUpper(strconv.FormatInt(int64(off), 16)) + ".img"
	for _, im := range images {
		if !out.CreateDirectory(dir) {
			return walk.Extraction{}
		}
		for _, v := range im.volumes {
			if len(v.blocks) == 0 {
				continue
			}
			// ubi_reader cannot open a file whose name has a slash or a NUL.
			if strings.ContainsAny(v.name, "/\x00") {
				return walk.Extraction{}
			}
			name := fmt.Sprintf("%s/img-%d_vol-%s.ubifs", dir, im.seq, v.name)
			if !out.CreateFile(name, nil) || !ubiWriteVolume(out, name, img, byPEB, v.blocks, peb, lebSize) {
				return walk.Extraction{}
			}
		}
	}
	return walk.Extraction{OK: true}
}

// ubiWriteVolume writes the logical erase blocks of a volume, as reader of
// a volume of ubi_reader. The list has a place for each block of the
// volume, and more when a logical number is larger. A place with no block
// is a block of 0xFF bytes. Of two blocks with one logical number, the last
// one wins.
func ubiWriteVolume(out *walk.Output, name string, img []byte, byPEB map[int]*ubiBlock, list []int, peb, lebSize int) bool {
	slots := map[int]int{}
	n := len(list)
	for _, p := range list {
		l := byPEB[p].lnum
		n = max(n, l+1)
		slots[l] = p
	}
	var empty []byte
	for l := range n {
		p, ok := slots[l]
		if !ok {
			if empty == nil {
				empty = bytes.Repeat([]byte{0xFF}, max(lebSize, 0))
			}
			if len(empty) > 0 && !out.AppendFile(name, empty) {
				return false
			}
			continue
		}
		b := byPEB[p]
		start := b.fileOffset + b.dataOffset
		size := peb - b.dataOffset - b.dataPad
		if b.volType == ubiStatic {
			size = b.dataSize
		}
		d := img[min(start, len(img)):min(start+max(size, 0), len(img))]
		if len(d) > 0 && !out.AppendFile(name, d) {
			return false
		}
	}
	return true
}
