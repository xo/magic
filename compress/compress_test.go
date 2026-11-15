package compress

import "testing"

func TestRegister(t *testing.T) {
	t.Parallel()
	d := func(src []byte, _ int) ([]byte, error) { return src, nil }
	Register(LZMA, "xzlib", d)
	name, got, ok := Lookup(LZMA)
	if !ok || name != "xzlib" || got == nil {
		t.Errorf("expected the decoder of xzlib, got: %q, %v", name, ok)
	}
	if _, _, ok := Lookup(Zstd); ok {
		t.Errorf("expected no decoder for zstd, got one")
	}
	defer func() {
		if recover() == nil {
			t.Errorf("expected a panic for a second Register, got none")
		}
	}()
	Register(LZMA, "xzlib", d)
}
