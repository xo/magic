// Copyright (c) Ian F. Darwin 1986-1995.
// Software written by Ian F. Darwin and others;
// maintained 1995-present by Christos Zoulas and others.
//
// Redistribution and use in source and binary forms, with or without
// modification, are permitted provided that the following conditions
// are met:
// 1. Redistributions of source code must retain the above copyright
//    notice immediately at the beginning of the file, without modification,
//    this list of conditions, and the following disclaimer.
// 2. Redistributions in binary form must reproduce the above copyright
//    notice, this list of conditions and the following disclaimer in the
//    documentation and/or other materials provided with the distribution.
//
// THIS SOFTWARE IS PROVIDED BY THE AUTHOR AND CONTRIBUTORS ``AS IS'' AND
// ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
// IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
// ARE DISCLAIMED. IN NO EVENT SHALL THE AUTHOR OR CONTRIBUTORS BE LIABLE FOR
// ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR CONSEQUENTIAL
// DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF SUBSTITUTE GOODS
// OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS INTERRUPTION)
// HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN CONTRACT, STRICT
// LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE) ARISING IN ANY WAY
// OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE POSSIBILITY OF
// SUCH DAMAGE.
//
// This file translates src/file.h of file to Go, at the commit that
// docs/upstream/upstreams.tsv names (D29, D48).

package engine

import (
	"encoding/binary"
	"math"
)

// value is the value that a rule reads, as union VALUETYPE of file.h: 128
// bytes, read as a number in the byte order of the machine, which is
// little-endian in the reference (D33), or as a string.
//
// Like ms_value of libmagic, one value lives for a whole detection, and a
// rule that reads fewer bytes leaves the rest as they were. A string
// compare reads past the NUL, so the rest can count.
type value [128]byte

var le = binary.LittleEndian

func (v *value) b() uint8   { return v[0] }
func (v *value) h() uint16  { return le.Uint16(v[:]) }
func (v *value) l() uint32  { return le.Uint32(v[:]) }
func (v *value) q() uint64  { return le.Uint64(v[:]) }
func (v *value) f() float32 { return math.Float32frombits(v.l()) }
func (v *value) d() float64 { return math.Float64frombits(v.q()) }

func (v *value) setB(x uint8)   { v[0] = x }
func (v *value) setH(x uint16)  { le.PutUint16(v[:], x) }
func (v *value) setL(x uint32)  { le.PutUint32(v[:], x) }
func (v *value) setQ(x uint64)  { le.PutUint64(v[:], x) }
func (v *value) setF(x float32) { v.setL(math.Float32bits(x)) }
func (v *value) setD(x float64) { v.setQ(math.Float64bits(x)) }
func (v *value) clear()         { *v = value{} }
func (v *value) str() []byte    { return cstr(v[:]) }
func (v *value) strlen() int    { return len(v.str()) }
func be16(b []byte) uint16      { return binary.BigEndian.Uint16(b) }
func be32(b []byte) uint32      { return binary.BigEndian.Uint32(b) }
func be64(b []byte) uint64      { return binary.BigEndian.Uint64(b) }
func me32(b []byte) uint32 {
	return uint32(b[1])<<24 | uint32(b[0])<<16 | uint32(b[3])<<8 | uint32(b[2])
}
func entryQ(v *[128]byte) uint64 { return le.Uint64(v[:]) }
