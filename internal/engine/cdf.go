// Copyright (c) 2008 Christos Zoulas
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE NETBSD FOUNDATION, INC. AND CONTRIBUTORS
// ``AS IS'' AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED
// TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR
// PURPOSE ARE DISCLAIMED.  IN NO EVENT SHALL THE FOUNDATION OR CONTRIBUTORS
// BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
// CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
// SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
// INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
// CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
// ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
// POSSIBILITY OF SUCH DAMAGE.
//
// Copyright (c) 2008, 2016 Christos Zoulas
// All rights reserved.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice, this list of conditions and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE NETBSD FOUNDATION, INC. AND CONTRIBUTORS
// ``AS IS'' AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED
// TO, THE IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR
// PURPOSE ARE DISCLAIMED.  IN NO EVENT SHALL THE FOUNDATION OR CONTRIBUTORS
// BE LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
// CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
// SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
// INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
// CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
// ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
// POSSIBILITY OF SUCH DAMAGE.
//
// This file translates src/cdf.c, src/readcdf.c, src/cdf_time.c of file to Go,
// at the commit that docs/upstream/upstreams.tsv names (D29, D48).

package engine

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"strings"
	"time"
)

// This file is cdf.c and readcdf.c: the reader of the Compound Document
// Format of Microsoft, which Office 97 to 2003, Outlook and Windows
// Installer use. A rule cannot read it, because its streams are chains of
// sectors anywhere in the file.
//
// The reader fails at the same step as libmagic on a damaged file, because
// the output names the step.

const (
	cdfLoopLimit    = 10000
	cdfMemoryLimit  = 16 * 1024 * 1024
	cdfElementLimit = 100000
	cdfSecIDFree    = -1
	cdfEndOfChain   = -2
	cdfDirSize      = 128
	cdfMagic        = 0xE11AB1A1E011CFD0

	cdfDirUserStorage = 1
	cdfDirUserStream  = 2
	cdfDirRootStorage = 5

	cdfNull         = 0x01
	cdfSigned16     = 0x02
	cdfSigned32     = 0x03
	cdfFloat        = 0x04
	cdfDouble       = 0x05
	cdfBool         = 0x0b
	cdfUnsigned32   = 0x13
	cdfSigned64     = 0x14
	cdfUnsigned64   = 0x15
	cdfString       = 0x1e
	cdfWString      = 0x1f
	cdfFiletime     = 0x40
	cdfClipboard    = 0x47
	cdfVector       = 0x1000
	cdfArray        = 0x2000
	cdfByRef        = 0x4000
	cdfReserved     = 0x8000
	cdfTypeMask     = 0xfff
	cdfPropertyApp  = 0x12
	cdfSecDeclOff   = 0x1c
	cdfPropLimit    = math.MaxUint32 / (64 * 24)
	cdfShLenLimit   = math.MaxUint32 / 64
	cdfSATLimit     = 16 * 1024 * 1024
	cdfMasterSATLen = 436 / 4
)

// cdfInfo is cdf_info_t: the data, and the file for a read past it.
type cdfInfo struct {
	buf []byte
	r   io.ReaderAt
}

// read is cdf_read: len(p) bytes at off, from the buffer if it holds them,
// and else from the file.
func (in *cdfInfo) read(off uint64, p []byte) bool {
	end := off + uint64(len(p))
	if end < off || end > math.MaxInt64 {
		return false
	}
	if uint64(len(in.buf)) >= end {
		copy(p, in.buf[off:end])
		return true
	}
	if in.r == nil {
		return false
	}
	n, _ := in.r.ReadAt(p, int64(off))
	return n == len(p)
}

type cdfHeader struct {
	secSizeP2, shortSecSizeP2 uint16
	numSectorsInSAT           uint32
	secIDFirstDirectory       int32
	minSizeStandardStream     uint32
	secIDFirstShortSAT        int32
	numSectorsInShortSAT      uint32
	secIDFirstMasterSAT       int32
	numSectorsInMasterSAT     uint32
	masterSAT                 [cdfMasterSATLen]int32
}

func (h *cdfHeader) secSize() uint64      { return 1 << h.secSizeP2 }
func (h *cdfHeader) shortSecSize() uint64 { return 1 << h.shortSecSizeP2 }

// readHeader is cdf_read_header.
func (in *cdfInfo) readHeader(h *cdfHeader) bool {
	var buf [512]byte
	if !in.read(0, buf[:]) {
		return false
	}
	if le.Uint64(buf[0:]) != cdfMagic {
		return false
	}
	h.secSizeP2 = le.Uint16(buf[30:])
	h.shortSecSizeP2 = le.Uint16(buf[32:])
	h.numSectorsInSAT = le.Uint32(buf[44:])
	h.secIDFirstDirectory = int32(le.Uint32(buf[48:]))
	h.minSizeStandardStream = le.Uint32(buf[56:])
	h.secIDFirstShortSAT = int32(le.Uint32(buf[60:]))
	h.numSectorsInShortSAT = le.Uint32(buf[64:])
	h.secIDFirstMasterSAT = int32(le.Uint32(buf[68:]))
	h.numSectorsInMasterSAT = le.Uint32(buf[72:])
	for i := range h.masterSAT {
		h.masterSAT[i] = int32(le.Uint32(buf[76+4*i:]))
	}
	return h.secSizeP2 <= 20 && h.shortSecSizeP2 <= 20
}

// readSector is cdf_read_sector: sector id into p.
func (in *cdfInfo) readSector(p []byte, h *cdfHeader, id int32) bool {
	ss := h.secSize()
	if math.MaxUint64/ss < uint64(int64(id)) {
		return false
	}
	return in.read(ss+uint64(int64(id))*ss, p)
}

// cdfSAT is cdf_sat_t: a table of sector numbers, with sat_len, the number
// of sectors that it fills.
type cdfSAT struct {
	tab []byte
	n   uint64
}

func (s *cdfSAT) at(i int32) int32 { return int32(le.Uint32(s.tab[4*int64(i):])) }

// readSAT is cdf_read_sat.
func (in *cdfInfo) readSAT(h *cdfHeader, sat *cdfSAT) bool {
	ss := h.secSize()
	nsatpersec := ss/4 - 1
	i := uint64(0)
	for i < cdfMasterSATLen && h.masterSAT[i] != cdfSecIDFree {
		i++
	}
	limit := uint64(math.MaxUint32) / (64 * ss)
	if nsatpersec > 0 && uint64(h.numSectorsInMasterSAT) > limit/nsatpersec || i > limit {
		return false
	}
	sat.n = uint64(h.numSectorsInMasterSAT)*nsatpersec + i
	if sat.n > cdfSATLimit/ss {
		return false
	}
	sat.tab = make([]byte, max(sat.n, 1)*ss)
	for i = 0; i < cdfMasterSATLen; i++ {
		if h.masterSAT[i] < 0 {
			break
		}
		if ss*(i+1) > uint64(len(sat.tab)) || !in.readSector(sat.tab[ss*i:ss*(i+1)], h, h.masterSAT[i]) {
			return false
		}
	}
	msa := make([]byte, ss)
	mid := h.secIDFirstMasterSAT
	for j := uint64(0); j < uint64(h.numSectorsInMasterSAT); j++ {
		if mid < 0 {
			break
		}
		if j >= cdfLoopLimit {
			return false
		}
		if !in.readSector(msa, h, mid) {
			return false
		}
		for k := uint64(0); k < nsatpersec; k, i = k+1, i+1 {
			sec := int32(le.Uint32(msa[4*k:]))
			if sec < 0 {
				sat.n = i
				return true
			}
			if i >= sat.n {
				return false
			}
			if ss*(i+1) > uint64(len(sat.tab)) || !in.readSector(sat.tab[ss*i:ss*(i+1)], h, sec) {
				return false
			}
		}
		mid = int32(le.Uint32(msa[4*nsatpersec:]))
	}
	sat.n = i
	return true
}

// countChain is cdf_count_chain: the length of the chain of sectors from
// sid, or -1.
func countChain(sat *cdfSAT, sid int32, size uint64) int64 {
	if size == 0 || sat.n > math.MaxUint64/size {
		return -1
	}
	maxsector := int32(sat.n * size / 4)
	if sid == cdfEndOfChain {
		return 0
	}
	i := int64(0)
	for j := 0; sid >= 0; i, j = i+1, j+1 {
		if j >= cdfLoopLimit || uint64(i) >= cdfMemoryLimit/size || sid >= maxsector {
			return -1
		}
		sid = sat.at(sid)
	}
	if i == 0 {
		return -1
	}
	return i
}

// cdfStream is cdf_stream_t. tab is nil for an empty stream.
type cdfStream struct {
	tab    []byte
	n      uint64 // sst_len: the number of sectors
	dirlen uint64
	ss     uint64
}

func (s *cdfStream) size() uint64 { return s.ss * s.n }

// readLongChain is cdf_read_long_sector_chain.
func (in *cdfInfo) readLongChain(h *cdfHeader, sat *cdfSAT, sid int32, length uint64, scn *cdfStream) bool {
	ss := h.secSize()
	n := countChain(sat, sid, ss)
	*scn = cdfStream{dirlen: max(uint64(h.minSizeStandardStream), length), ss: ss}
	if sid == cdfEndOfChain || length == 0 || n < 0 {
		*scn = cdfStream{}
		return false
	}
	scn.n = uint64(n)
	scn.tab = make([]byte, scn.n*ss)
	for i, j := uint64(0), 0; sid >= 0; i, j = i+1, j+1 {
		if j >= cdfLoopLimit || i >= scn.n || !in.readSector(scn.tab[i*ss:(i+1)*ss], h, sid) {
			*scn = cdfStream{}
			return false
		}
		sid = sat.at(sid)
	}
	return true
}

// readShortChain is cdf_read_short_sector_chain.
func readShortChain(h *cdfHeader, ssat *cdfSAT, sst *cdfStream, sid int32, length uint64, scn *cdfStream) bool {
	ss := h.shortSecSize()
	n := countChain(ssat, sid, h.secSize())
	*scn = cdfStream{dirlen: length, ss: ss}
	if n < 0 {
		*scn = cdfStream{}
		return false
	}
	scn.n = uint64(n)
	scn.tab = make([]byte, max(scn.n, 1)*ss)
	for i, j := uint64(0), 0; sid >= 0; i, j = i+1, j+1 {
		if j >= cdfLoopLimit || i >= scn.n {
			*scn = cdfStream{}
			return false
		}
		if math.MaxUint64/ss < uint64(int64(sid)) {
			*scn = cdfStream{}
			return false
		}
		pos := uint64(int64(sid)) * ss
		if pos+ss > h.secSize()*sst.n {
			*scn = cdfStream{}
			return false
		}
		copy(scn.tab[i*ss:(i+1)*ss], sst.tab[pos:pos+ss])
		sid = ssat.at(sid)
	}
	return true
}

// readChain is cdf_read_sector_chain: a short chain for a small stream, and
// a long one for the rest.
func (in *cdfInfo) readChain(h *cdfHeader, sat, ssat *cdfSAT, sst *cdfStream, sid int32, length uint64, scn *cdfStream) bool {
	if length < uint64(h.minSizeStandardStream) && sst.tab != nil {
		return readShortChain(h, ssat, sst, sid, length, scn)
	}
	return in.readLongChain(h, sat, sid, length, scn)
}

// cdfDir is one entry of the directory, cdf_directory_t.
type cdfDir struct {
	name        [32]uint16
	typ         uint8
	storageUUID [2]uint64
	firstSector int32
	size        uint32
}

// readDir is cdf_read_dir.
func (in *cdfInfo) readDir(h *cdfHeader, sat *cdfSAT) ([]cdfDir, bool) {
	ss := h.secSize()
	sid := h.secIDFirstDirectory
	ns := countChain(sat, sid, ss)
	if ns < 0 {
		return nil, false
	}
	nd := ss / cdfDirSize
	if nd != 0 && uint64(ns) > cdfMemoryLimit/nd/cdfDirSize {
		return nil, false
	}
	dir := make([]cdfDir, uint64(ns)*nd)
	buf := make([]byte, ss)
	for i := uint64(0); i < uint64(ns); i++ {
		// The C code reuses its loop limit as the index of the inner loop,
		// so the limit never stops it.
		if !in.readSector(buf, h, sid) {
			return nil, false
		}
		for j := uint64(0); j < nd; j++ {
			b := buf[j*cdfDirSize:]
			d := &dir[i*nd+j]
			for k := range d.name {
				d.name[k] = le.Uint16(b[2*k:])
			}
			d.typ = b[66]
			d.storageUUID[0] = le.Uint64(b[80:])
			d.storageUUID[1] = le.Uint64(b[88:])
			d.firstSector = int32(le.Uint32(b[116:]))
			d.size = le.Uint32(b[120:])
		}
		sid = sat.at(sid)
	}
	return dir, true
}

// readSSAT is cdf_read_ssat.
func (in *cdfInfo) readSSAT(h *cdfHeader, sat, ssat *cdfSAT) bool {
	ss := h.secSize()
	sid := h.secIDFirstShortSAT
	n := countChain(sat, sid, ss)
	if n < 0 {
		return false
	}
	ssat.n = uint64(n)
	ssat.tab = make([]byte, max(ssat.n, 1)*ss)
	for i, j := uint64(0), 0; sid >= 0; i, j = i+1, j+1 {
		if j >= cdfLoopLimit || i >= ssat.n || !in.readSector(ssat.tab[i*ss:(i+1)*ss], h, sid) {
			return false
		}
		sid = sat.at(sid)
	}
	return true
}

// readShortStream is cdf_read_short_stream: the stream of the root
// storage, which holds the short sectors. It returns the root, or -1 when
// the root has a stream that cannot be read.
func (in *cdfInfo) readShortStream(h *cdfHeader, sat *cdfSAT, dir []cdfDir, scn *cdfStream) (*cdfDir, bool) {
	*scn = cdfStream{}
	for i := range dir {
		if dir[i].typ != cdfDirRootStorage {
			continue
		}
		d := &dir[i]
		if d.firstSector < 0 {
			return d, true
		}
		return d, in.readLongChain(h, sat, d.firstSector, uint64(d.size), scn)
	}
	return nil, true
}

// namecmp is cdf_namecmp: whether the l characters of name, with its NUL,
// equal the name of a directory entry.
func namecmp(name string, s *[32]uint16) bool {
	for i := 0; i <= len(name); i++ {
		var c uint16
		if i < len(name) {
			c = uint16(name[i])
		}
		if i >= len(s) || s[i] != c {
			return false
		}
	}
	return true
}

// findStream is cdf_find_stream: the index of the entry, plus 1, or 0.
func findStream(dir []cdfDir, name string, typ uint8) int {
	for i := range dir {
		if dir[i].typ == typ && namecmp(name, &dir[i].name) {
			return i + 1
		}
	}
	return 0
}

// readUserStream is cdf_read_user_stream. found is false when the stream
// is not in the directory.
func (in *cdfInfo) readUserStream(h *cdfHeader, sat, ssat *cdfSAT, sst *cdfStream, dir []cdfDir, name string, scn *cdfStream) (ok, found bool) {
	i := findStream(dir, name, cdfDirUserStream)
	if i <= 0 {
		*scn = cdfStream{}
		return false, false
	}
	d := &dir[i-1]
	return in.readChain(h, sat, ssat, sst, d.firstSector, uint64(d.size), scn), true
}

// checkOffset is cdf_check_stream_offset: whether tail bytes at off are in
// the stream.
func (s *cdfStream) checkOffset(off, tail uint64) bool {
	e := off + tail
	return e >= off && e <= s.size()
}

// cdfProperty is cdf_property_info_t, with what the MIME type needs.
type cdfProperty struct {
	id, typ uint32
	str     []byte
	val     uint64 // the value of a number or a time, as cdf_copy_info reads it
}

// readPropertyInfo is cdf_read_property_info.
func readPropertyInfo(sst *cdfStream, offs uint32, info *[]cdfProperty) bool {
	if offs > math.MaxUint32/4 {
		return false
	}
	sho := uint64(offs)
	if !sst.checkOffset(sho, 8) {
		return false
	}
	shLen := uint64(le.Uint32(sst.tab[sho:]))
	if shLen > cdfShLenLimit {
		return false
	}
	// From commit bcbb6064 on, file checks the length of the section from
	// its copy of the header on the stack, not from the stream. On Linux
	// the stack is far above the heap, so the check always fails, and file
	// reads no property. The port gives the same answer (D33).
	if followsStackCheck {
		return false
	}
	if !sst.checkOffset(sho, shLen) {
		return false
	}
	nprops := uint64(le.Uint32(sst.tab[sho+4:]))
	if nprops > cdfPropLimit || uint64(len(*info))+nprops > cdfPropLimit {
		return false
	}
	base := len(*info)
	*info = append(*info, make([]cdfProperty, nprops)...)
	inp := (*info)[base:]
	p := sho + 8
	e := sho + shLen
	if p >= e {
		return false
	}
	tab := sst.tab
	u32 := func(at uint64, i uint64) uint32 { return le.Uint32(tab[at+4*i:]) }
	for i := uint64(0); i < nprops; i++ {
		// cdf_get_property_info_pos
		tail := i<<1 + 1
		if p >= e || !sst.checkOffset(p, (tail+1)*4) {
			return false
		}
		ofs := uint64(u32(p, tail))
		if ofs < 8 {
			return false
		}
		ofs -= 8
		if ofs > e-p {
			return false
		}
		q := p + ofs
		inp[i].id = u32(p, i<<1)
		left := e - q
		if left < 4 {
			return false
		}
		inp[i].typ = u32(q, 0)
		nelements, slen := uint64(1), uint64(1)
		if inp[i].typ&cdfVector != 0 {
			if left < 8 {
				return false
			}
			nelements = uint64(u32(q, 1))
			if nelements > cdfElementLimit || nelements == 0 {
				return false
			}
			slen = 2
		}
		o4 := slen * 4
		if inp[i].typ&(cdfArray|cdfByRef|cdfReserved) != 0 {
			continue
		}
		// cdf_copy_info: a number of 2, 4 or 8 bytes after the type. A vector
		// or a short section leaves it 0.
		copyInfo := func(n uint64) {
			if inp[i].typ&cdfVector == 0 && e-(q+o4) >= n {
				switch n {
				case 2:
					inp[i].val = uint64(le.Uint16(tab[q+o4:]))
				case 4:
					inp[i].val = uint64(le.Uint32(tab[q+o4:]))
				case 8:
					inp[i].val = le.Uint64(tab[q+o4:])
				}
			}
		}
		switch inp[i].typ & cdfTypeMask {
		case cdfSigned16:
			copyInfo(2)
		case cdfSigned32, cdfBool, cdfUnsigned32, cdfFloat:
			copyInfo(4)
		case cdfSigned64, cdfUnsigned64, cdfDouble, cdfFiletime:
			copyInfo(8)
		case cdfString, cdfWString:
			if nelements > 1 {
				*info = append(*info, make([]cdfProperty, nelements)...)
				inp = (*info)[base:]
			}
			for j := uint64(0); j < nelements && i < nprops; j, i = j+1, i+1 {
				if o4+4 > left {
					return false
				}
				l := uint64(u32(q, slen))
				o4 += 4
				if o4+l > left {
					return false
				}
				inp[i].str = tab[q+o4 : q+o4+l]
				if l&1 != 0 {
					l++
				}
				slen += l >> 1
				o4 = slen * 4
			}
			i--
		}
	}
	// The C code counts the properties of the section, and fills the slots
	// of a vector of strings past them, which it does not count.
	*info = (*info)[:base+int(nprops)]
	return true
}

// unpackSummaryInfo is cdf_unpack_summary_info.
func unpackSummaryInfo(sst *cdfStream) ([]cdfProperty, bool) {
	if !sst.checkOffset(0, 28) || !sst.checkOffset(cdfSecDeclOff, 20) {
		return nil, false
	}
	var info []cdfProperty
	if !readPropertyInfo(sst, le.Uint32(sst.tab[cdfSecDeclOff+16:]), &info) {
		return nil, false
	}
	return info, true
}

// app2mime and the tables after it are the tables of readcdf.c.
var (
	app2mime = [][2]string{
		{"Word", "msword"},
		{"Excel", "vnd.ms-excel"},
		{"Powerpoint", "vnd.ms-powerpoint"},
		{"Crystal Reports", "x-rpt"},
		{"Advanced Installer", "vnd.ms-msi"},
		{"InstallShield", "vnd.ms-msi"},
		{"Microsoft Patch Compiler", "vnd.ms-msi"},
		{"NAnt", "vnd.ms-msi"},
		{"Windows Installer", "vnd.ms-msi"},
	}
	name2mime = [][2]string{
		{"Book", "vnd.ms-excel"},
		{"Workbook", "vnd.ms-excel"},
		{"WordDocument", "msword"},
		{"PowerPoint", "vnd.ms-powerpoint"},
		{"DigitalSignature", "vnd.ms-msi"},
	}
	name2desc = [][2]string{
		{"Book", "Microsoft Excel"},
		{"Workbook", "Microsoft Excel"},
		{"WordDocument", "Microsoft Word"},
		{"PowerPoint", "Microsoft PowerPoint"},
		{"DigitalSignature", "Microsoft Installer"},
	}
	msiCLSID = [2]uint64{0x00000000000c1084, 0x46000000000000c0}
)

// sectionInfo is sectioninfo of readcdf.c.
var sectionInfo = []struct {
	name, mime string
	sections   []string
	types      []uint8
}{
	{"Encrypted", "encrypted", []string{"EncryptedPackage", "EncryptedSummary"}, []uint8{cdfDirUserStream, cdfDirUserStream}},
	{"QuickBooks", "quickbooks", []string{"mfbu_header"}, []uint8{cdfDirUserStream}},
	{"Microsoft Excel", "vnd.ms-excel", []string{"Book", "Workbook"}, []uint8{cdfDirUserStream, cdfDirUserStream}},
	{"Microsoft Word", "msword", []string{"WordDocument"}, []uint8{cdfDirUserStream}},
	{"Microsoft PowerPoint", "vnd.ms-powerpoint", []string{"PowerPoint Document"}, []uint8{cdfDirUserStream}},
	{"Microsoft Outlook Message", "vnd.ms-outlook", []string{"__properties_version1.0", "__recip_version1.0_#00000000"}, []uint8{cdfDirUserStream, cdfDirUserStorage}},
}

// appToMime is cdf_app_to_mime: the first entry of nv whose pattern is in
// s, with the case of ASCII ignored.
func appToMime(s string, nv [][2]string) string {
	ls := asciiLower(s)
	for _, e := range nv {
		if strings.Contains(ls, asciiLower(e[0])) {
			return e[1]
		}
	}
	return ""
}

// cdf is file_trycdf.
func (st *state) cdf(b *buffer) int {
	if st.flags&(Apple|Extension) != 0 {
		return 0
	}
	in := &cdfInfo{buf: b.data}
	if b.file != nil && b.file.r != nil {
		in.r = b.file.r
	}
	notMime := st.flags&Mime == 0
	var (
		h    cdfHeader
		sat  cdfSAT
		ssat cdfSAT
		sst  cdfStream
		scn  cdfStream
		dir  []cdfDir
		root *cdfDir
		expn string
		i    = -1
	)
	if !in.readHeader(&h) {
		return 0
	}
	switch {
	case !in.readSAT(&h, &sat):
		expn = "Can't read SAT"
	case !in.readSSAT(&h, &sat, &ssat):
		expn = "Can't read SSAT"
	default:
		var ok bool
		if dir, ok = in.readDir(&h, &sat); !ok {
			expn = "Can't read directory"
			break
		}
		if root, ok = in.readShortStream(&h, &sat, dir, &sst); !ok {
			expn = "Cannot read short stream"
			break
		}
		i = st.cdfContents(in, &h, &sat, &ssat, &sst, dir, root, &scn, &expn)
	}
	if i != -1 {
		return i
	}
	// The default when the reader failed.
	if notMime {
		st.printf("Composite Document File V2 Document")
		if expn != "" {
			st.printf(", %s", expn)
		}
	} else if st.flags&MimeType != 0 {
		st.printf("application/x-ole-storage")
	}
	return 1
}

// cdfContents is the part of file_trycdf after the reader found the
// directory: it names the document from its streams.
func (st *state) cdfContents(in *cdfInfo, h *cdfHeader, sat, ssat *cdfSAT, sst *cdfStream, dir []cdfDir, root *cdfDir, scn *cdfStream, expn *string) int {
	notMime := st.flags&Mime == 0
	if ok, _ := in.readUserStream(h, sat, ssat, sst, dir, "FileHeader", scn); ok {
		const hwp = "HWP Document File"
		if scn.n*scn.ss >= uint64(len(hwp)) && bytes.HasPrefix(scn.tab, []byte(hwp)) {
			if notMime {
				st.printf("Hancom HWP (Hangul Word Processor) file, version 5.0")
			} else if st.flags&MimeType != 0 {
				st.printf("application/x-hwp")
			}
			return 1
		}
	}
	// catalog is cdf_file_catalog_info: the list of names of a Thumbs.db.
	catalog := func() int {
		var cat cdfStream
		if ok, _ := in.readUserStream(h, sat, ssat, sst, dir, "Catalog", &cat); !ok {
			return -1
		}
		return st.cdfCatalog(&cat)
	}
	i := -1
	ok, found := in.readUserStream(h, sat, ssat, sst, dir, "\x05SummaryInformation", scn)
	if ok {
		i = st.cdfCheckSummary(dir, scn, root, expn, catalog)
	} else if found {
		*expn = "Cannot read summary info"
	}
	if i <= 0 {
		ok, found = in.readUserStream(h, sat, ssat, sst, dir, "\x05DocumentSummaryInformation", scn)
		if ok {
			i = st.cdfCheckSummary(dir, scn, root, expn, catalog)
		} else if found {
			*expn = "Cannot read summary info"
		}
	}
	if i <= 0 {
		i = st.cdfDirInfo(dir)
		if i < 0 {
			*expn = "Cannot read section info"
		}
	}
	return i
}

// cdfCheckSummary is cdf_check_summary_info.
func (st *state) cdfCheckSummary(dir []cdfDir, scn *cdfStream, root *cdfDir, expn *string, catalog func() int) int {
	notMime := st.flags&Mime == 0
	i := st.cdfSummaryInfo(scn, root)
	if i < 0 {
		*expn = "Can't expand summary_info"
		return i
	}
	if i == 1 {
		return i
	}
	str := ""
	for j := range dir {
		if str != "" {
			break
		}
		var name []byte
		for _, c := range dir[j].name {
			if byte(c) == 0 {
				break
			}
			name = append(name, byte(c))
		}
		if notMime {
			str = appToMime(string(name), name2desc)
		} else {
			str = appToMime(string(name), name2mime)
		}
	}
	if notMime {
		if str != "" {
			st.printf("%s", str)
			i = 1
		}
	} else if st.flags&MimeType != 0 {
		if str == "" {
			str = "vnd.ms-office"
		}
		st.printf("application/%s", str)
		i = 1
	}
	if i <= 0 {
		i = catalog()
	}
	return i
}

// cdfSummaryInfo is cdf_file_summary_info with cdf_file_property_info: the
// header of the summary and its properties, or for MIME the type of the
// application that wrote the document.
func (st *state) cdfSummaryInfo(scn *cdfStream, root *cdfDir) int {
	info, ok := unpackSummaryInfo(scn)
	if !ok {
		return -1
	}
	notMime := st.flags&Mime == 0
	if notMime {
		st.printf("Composite Document File V2 Document")
		order := "Big"
		if le.Uint16(scn.tab) == 0xfffe {
			order = "Little"
		}
		st.printf(", %s Endian", order)
		ver, os := uint32(le.Uint16(scn.tab[4:])), le.Uint16(scn.tab[6:])
		switch os {
		case 2:
			st.printf(", Os: Windows, Version %d.%d", ver&0xff, ver>>8)
		case 1:
			st.printf(", Os: MacOS, Version %d.%d", ver>>8, ver&0xff)
		default:
			st.printf(", Os %d, Version: %d.%d", os, ver&0xff, ver>>8)
		}
		if root != nil && root.storageUUID == msiCLSID {
			st.printf(", %s", "Microsoft Installer")
		}
	}
	str := ""
	if !notMime && root != nil && root.storageUUID == msiCLSID {
		str = "vnd.ms-msi"
	}
	for _, p := range info {
		name := cdfPropertyName(p.id)
		switch p.typ {
		case cdfNull:
		case cdfSigned16:
			if notMime {
				st.printf(", %s: %d", name, int16(p.val))
			}
		case cdfSigned32:
			if notMime {
				st.printf(", %s: %d", name, int32(p.val))
			}
		case cdfUnsigned32:
			if notMime {
				st.printf(", %s: %d", name, uint32(p.val))
			}
		case cdfFloat:
			if notMime {
				st.put(cprintf([]byte(", %s: %g"), cstringS(name), cfloat(float64(math.Float32frombits(uint32(p.val))))))
			}
		case cdfDouble:
			if notMime {
				st.put(cprintf([]byte(", %s: %g"), cstringS(name), cfloat(math.Float64frombits(p.val))))
			}
		case cdfString, cdfWString:
			n := len(p.str)
			if n <= 1 {
				continue
			}
			k := 1
			if p.typ == cdfWString {
				k = 2
			}
			var vbuf []byte
			for s := 0; s < n && len(vbuf) < 1024; s += k {
				c := p.str[s]
				if c == 0 {
					break
				}
				if isPrint(c) {
					vbuf = append(vbuf, c)
				}
			}
			if len(vbuf) == 1024 {
				vbuf = vbuf[:1023]
			}
			if notMime {
				if len(vbuf) > 0 {
					st.printf(", %s: %s", name, vbuf)
				}
			} else if str == "" && p.id == cdfPropertyApp {
				str = appToMime(string(vbuf), app2mime)
			}
		case cdfFiletime:
			tp := int64(p.val)
			if tp == 0 || !notMime {
				continue
			}
			if tp < 1000000000000000 {
				st.printf(", %s: %s", name, cdfElapsed(tp))
				continue
			}
			sec, _ := cdfTimestamp(tp)
			st.printf(", %s: %s", name, cdfCtime(sec))
		case cdfClipboard:
		default:
			return -2
		}
	}
	if st.flags&MimeType != 0 {
		if str == "" {
			return 0
		}
		st.printf("application/%s", str)
	}
	return 1
}

// cdfCatalog is cdf_file_catalog.
func (st *state) cdfCatalog(sst *cdfStream) int {
	if st.flags&Mime != 0 {
		if st.flags&MimeType != 0 {
			st.printf("application/CDFV2")
		}
		return 1
	}
	st.printf("Microsoft Thumbs.db [")
	names, ok := unpackCatalog(sst)
	if !ok {
		return -1
	}
	// The first entry has a comma or a parenthesis, so libmagic skips it.
	for i := 1; i < len(names); i++ {
		end := ", "
		if i == len(names)-1 {
			end = "]"
		}
		st.printf("%s%s", names[i], end)
	}
	return 1
}

// unpackCatalog is cdf_unpack_catalog: the names of the records of a
// catalog. It copies libmagic, which writes record k+1 in entry k, so that
// the first record is lost.
func unpackCatalog(sst *cdfStream) ([]string, bool) {
	b := sst.tab
	eb := int(sst.size())
	p, nr := 0, 0
	for ; ; nr++ {
		if p+2 > eb {
			break
		}
		reclen := int(le.Uint16(b[p:]))
		if reclen == 0 {
			break
		}
		p += reclen
		if p > eb {
			break
		}
	}
	if nr == 0 {
		return nil, false
	}
	nr--
	entries := make([]string, nr)
	j := 0
	p = 0
	// libmagic can loop for ever on a record shorter than 14 bytes. The
	// port stops after as many steps as the stream has bytes.
	for i, steps := 0, 0; i < nr && steps <= eb; steps++ {
		if p+0+2 > eb || p+4+4 > eb || p+8+8 > eb {
			entries = entries[:max(j, 0)]
			break
		}
		reclen := int(le.Uint16(b[p:]))
		if reclen == 0 {
			break
		}
		if reclen < 14 {
			if j < len(entries) {
				entries[j] = ""
			}
			p += reclen
			continue
		}
		namlen := min(255, reclen-14)
		np := p + 16
		if np+2*namlen > eb {
			if j < len(entries) {
				entries[j] = ""
			}
			break
		}
		name := make([]byte, 0, namlen)
		for k := 0; k < namlen; k++ {
			c := le.Uint16(b[np+2*k:])
			if c == 0 {
				break
			}
			name = append(name, byte(c))
		}
		entries[j] = string(name)
		j = i
		i++
		p += reclen
	}
	return entries[:j], true
}

// cdfPropertyNames is vn of cdf.c.
var cdfPropertyNames = map[uint32]string{
	0x01: "Code page", 0x02: "Title", 0x03: "Subject", 0x04: "Author",
	0x05: "Keywords", 0x06: "Comments", 0x07: "Template", 0x08: "Last Saved By",
	0x09: "Revision Number", 0x0a: "Total Editing Time", 0x0b: "Last Printed",
	0x0c: "Create Time/Date", 0x0d: "Last Saved Time/Date", 0x0e: "Number of Pages",
	0x0f: "Number of Words", 0x10: "Number of Characters", 0x11: "Thumbnail",
	0x12: "Name of Creating Application", 0x13: "Security", 0x80000000: "Locale ID",
}

// cdfPropertyName is cdf_print_property_name.
func cdfPropertyName(id uint32) string {
	if n, ok := cdfPropertyNames[id]; ok {
		return n
	}
	return cprintfS("%#x", cint(uint64(id)))
}

// cdfElapsed is cdf_print_elapsed_time: a time in 100 ns as a duration.
func cdfElapsed(ts int64) string {
	ts /= 10000000
	secs := int32(ts % 60)
	ts /= 60
	mins := int32(ts % 60)
	ts /= 60
	hours := int32(ts % 24)
	ts /= 24
	days := int32(ts)
	var b strings.Builder
	if days != 0 {
		fmt.Fprintf(&b, "%dd+", days)
	}
	if days != 0 || hours != 0 {
		fmt.Fprintf(&b, "%.2d:", hours)
	}
	fmt.Fprintf(&b, "%.2d:", mins)
	fmt.Fprintf(&b, "%.2d", secs)
	return b.String()
}

// cdfCtime is cdf_ctime without its newline: ctime_r, in local time, or
// a mark for a time that asctime cannot print.
func cdfCtime(sec int64) string {
	if sec > maxCtime {
		return cprintfS("*Bad* %#16.16llx", cint(uint64(sec)))
	}
	return asctime(time.Unix(sec, 0).Local())
}

// cdfDirInfo is cdf_file_dir_info: it names the document from the names
// of its streams.
func (st *state) cdfDirInfo(dir []cdfDir) int {
	for i := range dir {
		for _, si := range sectionInfo {
			for j, name := range si.sections {
				if si.types[j] != dir[i].typ || !namecmp(name, &dir[i].name) {
					continue
				}
				if st.flags&Mime == 0 {
					st.printf("CDFV2 %s", si.name)
				} else if st.flags&MimeType != 0 {
					st.printf("application/%s", si.mime)
				}
				return 1
			}
		}
	}
	return -1
}

// asciiLower folds the ASCII letters of s, as tolower in the C locale.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		b[i] = toLower(c)
	}
	return string(b)
}

// followsStackCheck is set while the port follows the commits of file that
// check a section of properties against the stack. readPropertyInfo says
// why.
const followsStackCheck = true
