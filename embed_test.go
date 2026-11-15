package magic

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strings"
	"testing"
	"unsafe"
)

// TestEmbeddedDatabase holds D20. The embedded database is compressed, and
// what it expands to must be the database that build-libmagic.sh compiled.
// The script records the SHA-256 of that database in libmagic/magic.mgc.sha256.
//
// A change to the compressor, to libzstd or to the loader that damages the
// bytes would still load, because libmagic reads what it can. It would then
// give wrong answers for some files only. The hash catches that at once.
func TestEmbeddedDatabase(t *testing.T) {
	t.Parallel()
	want, err := os.ReadFile("libmagic/magic.mgc.sha256")
	if err != nil {
		t.Fatal(err)
	}
	p, n, err := expand(magicDBZst)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	defer freeDB(p)
	if n < 1<<20 {
		t.Fatalf("expected a database of at least 1 MiB, got: %d bytes", n)
	}
	sum := sha256.Sum256(unsafe.Slice((*byte)(p), n))
	if got := hex.EncodeToString(sum[:]); got != strings.TrimSpace(string(want)) {
		t.Errorf("expected sha256 %s, got: %s", strings.TrimSpace(string(want)), got)
	}
}

// TestMagicDB checks that the exported function returns the same database as
// the one that New loads, and that each call returns a slice of its own.
func TestMagicDB(t *testing.T) {
	t.Parallel()
	want, err := os.ReadFile("libmagic/magic.mgc.sha256")
	if err != nil {
		t.Fatal(err)
	}
	db, err := MagicDB()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	sum := sha256.Sum256(db)
	if got := hex.EncodeToString(sum[:]); got != strings.TrimSpace(string(want)) {
		t.Errorf("expected sha256 %s, got: %s", strings.TrimSpace(string(want)), got)
	}
	other, err := MagicDB()
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	db[0] ^= 0xff
	if other[0] == db[0] {
		t.Error("expected each call to return a new slice, got a shared one")
	}
}

// TestExpandRefusesBadInput checks that damaged input gives an error. It must
// not crash, and it must not ask for a huge block of memory.
func TestExpandRefusesBadInput(t *testing.T) {
	t.Parallel()
	good := magicDBZst

	flip := func(i int) []byte {
		b := append([]byte(nil), good...)
		b[i] ^= 0xff
		return b
	}
	for _, test := range []struct {
		name string
		src  []byte
		want string
	}{
		{"empty", nil, "empty input"},
		{"not zstd", []byte("this is not a zstd frame"), "not a zstd frame"},
		{"header cut short", good[:6], ""},
		{"body cut short", good[:len(good)/2], ""},
		{"checksum cut off", good[:len(good)-2], ""},
		{"first byte changed", flip(0), ""},
		// Bytes 6 to 9 hold the size of the content. Byte 5 is the window
		// size, and a change to it can leave the output the same.
		{"size of content changed", flip(7), ""},
		{"middle changed", flip(len(good) / 2), ""},
		{"last byte changed", flip(len(good) - 1), ""},
		// A frame that does not record the size of its content.
		{"unknown size", []byte{0x28, 0xb5, 0x2f, 0xfd, 0x00, 0x00, 0x01, 0x00, 0x00}, "does not record"},
		// A frame that says its content is 1 TiB. The loader must refuse it
		// before it calls malloc.
		{"huge size", []byte{
			0x28, 0xb5, 0x2f, 0xfd, 0xe0,
			0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00, 0x00,
			0x01, 0x00, 0x00,
		}, "out of range"},
		// A frame that says its content is empty.
		{"zero size", []byte{0x28, 0xb5, 0x2f, 0xfd, 0x20, 0x00, 0x01, 0x00, 0x00}, "out of range"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			p, n, err := expand(test.src)
			if err == nil {
				freeDB(p)
				t.Fatalf("expected an error, got a block of %d bytes", n)
			}
			if !strings.HasPrefix(err.Error(), "magic: expanding the database: ") {
				t.Errorf("expected the error to name the database, got: %v", err)
			}
			if test.want != "" && !strings.Contains(err.Error(), test.want) {
				t.Errorf("expected an error with %q, got: %v", test.want, err)
			}
		})
	}
}
