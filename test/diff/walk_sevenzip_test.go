package diff

import (
	"encoding/binary"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestWalkSevenZip makes 7-Zip archives with 7z, with each method that the
// walker decodes, and with the header compressed and not, and compares the
// files of the walker with what 7z writes. binwalk extracts 7-Zip with 7zz.
// The test needs the decoders of LZMA and xz, which the gates import. It
// skips without 7z.
func TestWalkSevenZip(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("no 7z: install p7zip")
	}
	src := filepath.Join(t.TempDir(), "src")
	makeSevenZipTree(t, src)
	for _, method := range []string{"Copy", "LZMA", "LZMA2", "Deflate", "BZip2"} {
		for _, hc := range []string{"off", "on"} {
			t.Run(method+"-hc-"+hc, func(t *testing.T) {
				t.Parallel()
				arc := filepath.Join(t.TempDir(), "a.7z")
				cmd := exec.Command("7z", "a", "-bd", "-snl", "-m0="+method, "-mhc="+hc, arc, ".")
				cmd.Dir = src
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("7z a: %v: %s", err, out)
				}
				b, err := os.ReadFile(arc)
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				got := walkOne(t, b, "7zip", 0x777)
				want, _ := sevenZipTree(t, arc)
				if !slices.Equal(got, want) {
					t.Errorf("expected %q,\n got: %q", want, got)
				}
			})
		}
	}
}

// TestWalkSevenZipFilters makes 7-Zip archives with 7z, with each filter
// that the walker decodes, under LZMA2 and LZMA, and compares the files of
// the walker with what 7z writes. The tree holds files of code that each
// filter changes. It skips without 7z.
func TestWalkSevenZipFilters(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("7z"); err != nil {
		t.Skip("no 7z: install p7zip")
	}
	src := filepath.Join(t.TempDir(), "src")
	makeSevenZipTree(t, src)
	code := sevenZipCode()
	for name, b := range map[string][]byte{
		"code/all.bin":  code,
		"code/odd.bin":  code[:len(code)/3+7],
		"code/tiny.bin": code[:3],
	} {
		p := filepath.Join(src, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		if err := os.WriteFile(p, b, 0o644); err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
	}
	for _, filter := range []string{"BCJ", "ARM", "ARMT", "ARM64", "PPC", "SPARC", "IA64", "Delta:4", "BCJ2"} {
		for _, method := range []string{"LZMA2", "LZMA"} {
			t.Run(filter+"-"+method, func(t *testing.T) {
				t.Parallel()
				arc := filepath.Join(t.TempDir(), "a.7z")
				cmd := exec.Command("7z", "a", "-bd", "-snl", "-m0="+method, "-mf="+filter, arc, ".")
				cmd.Dir = src
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("7z a: %v: %s", err, out)
				}
				// The method of the archive must name the filter, or the
				// test proves nothing.
				out, err := exec.Command("7z", "l", "-slt", arc).CombinedOutput()
				if err != nil {
					t.Fatalf("7z l: %v: %s", err, out)
				}
				name, _, _ := strings.Cut(filter, ":")
				if !strings.Contains(string(out), " "+name) {
					t.Fatalf("expected %q in the methods, got: %q", name, out)
				}
				b, err := os.ReadFile(arc)
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				got := walkOne(t, b, "7zip", 0x777)
				want, _ := sevenZipTree(t, arc)
				if !slices.Equal(got, want) {
					t.Errorf("expected %q,\n got: %q", want, got)
				}
			})
		}
	}
}

// sevenZipCode makes 256 KiB of words that the branch filters convert:
// calls and jumps of x86, BL of ARM, Thumb and ARM64, ADRP of ARM64,
// branches of PowerPC and SPARC, and bundles of IA-64, mixed with random
// bytes.
func sevenZipCode() []byte {
	rng := rand.New(rand.NewPCG(7, 8))
	b := make([]byte, 0, 256<<10)
	for len(b) < cap(b) {
		r := rng.Uint32()
		switch rng.IntN(12) {
		case 0:
			b = append(b, 0xE8, byte(r), byte(r>>8), 0, 0)
		case 1:
			b = append(b, 0xE9, byte(r), byte(r>>8), 0xFF, 0xFF)
		case 2:
			b = append(b, 0x0F, 0x85, byte(r), byte(r>>8), 0, 0)
		case 3:
			b = append(b, byte(r), byte(r>>8), byte(r>>16), 0xEB)
		case 4:
			b = append(b, byte(r), 0xF0|byte(r>>8)&7, byte(r>>16), 0xF8|byte(r>>24)&7)
		case 5:
			b = binary.LittleEndian.AppendUint32(b, 0x94000000|r&0x03FFFFFF)
		case 6:
			b = binary.LittleEndian.AppendUint32(b, 0x90000000|r&0x6001FFFF)
		case 7:
			b = binary.BigEndian.AppendUint32(b, 0x48000001|r&0x03FFFFFC)
		case 8:
			b = binary.BigEndian.AppendUint32(b, 0x40000000|r&0x003FFFFF)
		case 9:
			// A bundle of IA-64 with a template that has branches.
			bundle := make([]byte, 16)
			for i := range bundle {
				bundle[i] = byte(rng.Uint32())
			}
			bundle[0] = 0x10 | bundle[0]&0x0F
			b = append(b, bundle...)
		default:
			b = append(b, byte(r), byte(r>>8), byte(r>>16))
		}
	}
	return b[:cap(b)]
}
