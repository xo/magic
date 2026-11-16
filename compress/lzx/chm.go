package lzx

import (
	"fmt"
	"io"
)

// Section reads the content of a CHM file that LZX compresses, the section
// that the ITSF format names MSCompressed. The caller reads the fields from
// the control data of the section, LZXC, and from its reset table. Section
// is not safe for concurrent use.
//
// The reset table gives the offset of each frame in the compressed data, so
// each frame starts on a byte of its own. The decoder resets at each frame
// whose index is a multiple of ResetInterval. To read a frame, Section
// decodes from the reset before it. It keeps the decoder and the last frame,
// so that it reads on from there without a new start.
type Section struct {
	// Data is the compressed data of the section.
	Data []byte
	// WindowSize is the size of the window in bytes, a power of two from 2^15
	// to 2^21. Version 2 of the control data gives it in units of 32768
	// bytes.
	WindowSize int
	// ResetInterval is the number of frames from one reset to the next.
	// Version 2 of the control data gives it in units of 32768 bytes, which
	// is one frame. A call of Decode decodes up to ResetInterval frames that
	// it does not return, so a caller that wants to bound its work bounds
	// ResetInterval.
	ResetInterval int
	// ResetTable holds the offset in Data of each frame.
	ResetTable []uint64
	// Length is the size of the section after decoding.
	Length int64

	dec *Decoder
	// next is the frame that dec decodes next, or -1 when dec must reset.
	next int
	// buf holds frame last, when have is true.
	buf  []byte
	last int
	have bool
}

// Decode appends to dst the n bytes of the section from offset off. When the
// section ends first, it appends the bytes up to the end and returns io.EOF.
// When the data is damaged, it returns an error that wraps ErrCorrupt, and
// the bytes that it appended before the damaged frame.
func (s *Section) Decode(dst []byte, off int64, n int) ([]byte, error) {
	switch {
	case off < 0 || n < 0:
		return dst, fmt.Errorf("lzx: read of %d bytes at offset %d", n, off)
	case s.ResetInterval < 1:
		return dst, fmt.Errorf("%w: the reset interval is %d", ErrCorrupt, s.ResetInterval)
	case s.Length < 0:
		return dst, fmt.Errorf("%w: the length of the section is %d", ErrCorrupt, s.Length)
	}
	if s.dec == nil {
		d, err := NewDecoder(s.WindowSize)
		if err != nil {
			return dst, fmt.Errorf("%w: %w", ErrCorrupt, err)
		}
		s.dec, s.next = d, -1
	}
	want := off + int64(n)
	end := min(want, s.Length)
	for off < end {
		i := off / FrameSize
		f, err := s.frame(int(i))
		if err != nil {
			return dst, err
		}
		lo, hi := off-i*FrameSize, min(end-i*FrameSize, int64(len(f)))
		dst = append(dst, f[lo:hi]...)
		off = i*FrameSize + hi
	}
	if end < want {
		return dst, io.EOF
	}
	return dst, nil
}

// frame returns frame i, which is decoded from the reset before it, or from
// the frame that the decoder reached.
func (s *Section) frame(i int) ([]byte, error) {
	if s.have && s.last == i {
		return s.buf, nil
	}
	if start := i - i%s.ResetInterval; s.next > i || s.next < start {
		s.next = start
	}
	for s.next <= i {
		k := s.next
		if k%s.ResetInterval == 0 {
			s.dec.Reset()
		}
		src, err := s.src(k)
		if err != nil {
			s.next, s.have = -1, false
			return nil, err
		}
		size := int(min(FrameSize, s.Length-int64(k)*FrameSize))
		s.buf, err = s.dec.Decode(s.buf[:0], src, size)
		if err != nil {
			s.next, s.have = -1, false
			return nil, fmt.Errorf("frame %d: %w", k, err)
		}
		s.last, s.have = k, true
		s.next++
	}
	return s.buf, nil
}

// src returns the compressed bytes of frame k, from its offset to the offset
// of the next frame, or to the end of the data.
func (s *Section) src(k int) ([]byte, error) {
	if k >= len(s.ResetTable) {
		return nil, fmt.Errorf("%w: frame %d is not in the reset table of %d frames", ErrCorrupt, k, len(s.ResetTable))
	}
	lo, hi := s.ResetTable[k], uint64(len(s.Data))
	if k+1 < len(s.ResetTable) {
		hi = s.ResetTable[k+1]
	}
	if lo > hi || hi > uint64(len(s.Data)) {
		return nil, fmt.Errorf("%w: frame %d is from offset %d to %d, and the data has %d bytes", ErrCorrupt, k, lo, hi, len(s.Data))
	}
	return s.Data[lo:hi], nil
}
