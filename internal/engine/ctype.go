package engine

// The character classes of the C locale, which libmagic runs in (D33).

func isSpace(c byte) bool  { return c == ' ' || c >= '\t' && c <= '\r' }
func isDigit(c byte) bool  { return c >= '0' && c <= '9' }
func isUpper(c byte) bool  { return c >= 'A' && c <= 'Z' }
func isLower(c byte) bool  { return c >= 'a' && c <= 'z' }
func isAlpha(c byte) bool  { return isUpper(c) || isLower(c) }
func isAlnum(c byte) bool  { return isAlpha(c) || isDigit(c) }
func isPrint(c byte) bool  { return c >= 0x20 && c < 0x7f }
func isXDigit(c byte) bool { return isDigit(c) || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F' }

func toLower(c byte) byte {
	if isUpper(c) {
		return c + 32
	}
	return c
}

func toUpper(c byte) byte {
	if isLower(c) {
		return c - 32
	}
	return c
}

// at returns s[i], or 0 when i is out of s, as a read of a C string that
// ends in NUL.
func at(s []byte, i int) byte {
	if i < 0 || i >= len(s) {
		return 0
	}
	return s[i]
}

// cstr returns s up to its first NUL.
func cstr(s []byte) []byte {
	for i, c := range s {
		if c == 0 {
			return s[:i]
		}
	}
	return s
}
