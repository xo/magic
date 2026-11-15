//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

package archive

import (
	"bytes"
	"encoding/hex"
	"errors"
	"io"
	"testing"
)

// ppmdText is the text that ppmdData holds.
const ppmdText = "the walker reads a binary file, finds the files in it, and extracts them. " +
	"the walker reads a 7-Zip archive with PPMd, and the model of PPMd predicts each byte " +
	"from the bytes before it.\n"

// ppmdData is the packed stream of ppmdText that 7z a -m0=PPMd:o=6:mem=64k
// writes.
const ppmdData = "0073f26f56a9d681164b1a6d16aef06a152bf2fa9b9a2b55d4224412d827de766f2756" +
	"78247d7786fb7fb50d2d5dc8ca0eda27113704efa0967f394dfde02b0fccc99fdf8f7c7798d6de74" +
	"ef197b860bc8370952cd3ded9b9b4c1a281c25e4e348a0514612fbfd46f2c2034e96d16f00"

// ppmdProps are the properties of ppmdData: the order 6 and 64 KiB of
// memory.
var ppmdProps = []byte{6, 0, 0, 1, 0}

// TestPPMd decodes a stream that 7z writes, and makes sure that the decoder
// fails on damaged data and on properties that 7-Zip rejects or that are
// over the limit of memory.
func TestPPMd(t *testing.T) {
	t.Parallel()
	data, err := hex.DecodeString(ppmdData)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	var buf bytes.Buffer
	if err := szDecodePPMd(&szCoder{props: ppmdProps}, [][]byte{data}, len(ppmdText), &buf); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if got := buf.String(); got != ppmdText {
		t.Errorf("expected %q, got: %q", ppmdText, got)
	}
	for _, tt := range []struct {
		name  string
		props []byte
		data  []byte
		size  int
		want  error
	}{
		{"short data", ppmdProps, data[:len(data)/2], len(ppmdText), errPPMd},
		{"long size", ppmdProps, data, len(ppmdText) + 100, errPPMd},
		{"first byte", ppmdProps, append([]byte{1}, data[1:]...), len(ppmdText), errPPMd},
		{"order 1", []byte{1, 0, 0, 1, 0}, data, len(ppmdText), errSevenZip},
		{"order 65", []byte{65, 0, 0, 1, 0}, data, len(ppmdText), errSevenZip},
		{"small memory", []byte{6, 0xFF, 7, 0, 0}, data, len(ppmdText), errSevenZip},
		{"large memory", []byte{6, 1, 0, 0, 0x10}, data, len(ppmdText), errPPMd},
		{"short props", ppmdProps[:4], data, len(ppmdText), errSevenZip},
	} {
		err := szDecodePPMd(&szCoder{props: tt.props}, [][]byte{tt.data}, tt.size, io.Discard)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: expected %v, got: %v", tt.name, tt.want, err)
		}
	}
}

// FuzzPPMd decodes damaged streams. The decoder must not panic and must
// stop. The first byte of the input is the order, and the memory is small
// so that the model fills and starts again.
func FuzzPPMd(f *testing.F) {
	data, err := hex.DecodeString(ppmdData)
	if err != nil {
		f.Fatalf("expected no error, got: %v", err)
	}
	f.Add(append([]byte{6}, data...))
	f.Add(append([]byte{2}, data...))
	f.Add(append([]byte{64}, data...))
	f.Add([]byte{16, 0, 0, 0, 0, 0})
	f.Fuzz(func(_ *testing.T, b []byte) {
		if len(b) < 1 {
			return
		}
		order := uint32(b[0])%(ppmdMaxOrder-ppmdMinOrder+1) + ppmdMinOrder
		for _, mem := range []uint32{ppmdMinMem, 1 << 16} {
			_ = ppmdDecode(order, mem, b[1:], 1<<16, io.Discard)
		}
	})
}
