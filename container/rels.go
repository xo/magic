package container

import (
	"bytes"
	"strings"
	"unicode/utf16"
)

// relTypes returns the attribute Type of each element Relationship in b, the
// relationships of a package of the Open Packaging Conventions. It does not
// parse XML. It reads the tags of b and their attributes, and skips the
// declaration, comments and other markup that starts with "<?" or "<!". A
// part of the relationships holds no other markup. It skips the prefix of a
// namespace, so "r:Relationship" is an element Relationship. It decodes
// UTF-16 with a byte order mark, which the Open Packaging Conventions allow.
// It decodes no entity, because no type of relationship holds one.
func relTypes(b []byte) []string {
	b = utf8Text(b)
	var types []string
	for {
		i := bytes.IndexByte(b, '<')
		if i < 0 {
			return types
		}
		b = b[i+1:]
		switch {
		case bytes.HasPrefix(b, []byte("!--")):
			b = skipPast(b, "-->")
			continue
		case len(b) > 0 && (b[0] == '?' || b[0] == '!'):
			b = skipPast(b, ">")
			continue
		}
		var name string
		name, b = tagName(b)
		if localName(name) != "Relationship" {
			continue
		}
		for {
			var attr, value string
			var ok bool
			attr, value, b, ok = nextAttr(b)
			if !ok {
				break
			}
			if localName(attr) == "Type" {
				types = append(types, strings.TrimSpace(value))
			}
		}
	}
}

// skipPast returns b after the first end, or nil when b has none.
func skipPast(b []byte, end string) []byte {
	i := bytes.Index(b, []byte(end))
	if i < 0 {
		return nil
	}
	return b[i+len(end):]
}

// tagName returns the name at the start of b, and the rest of b.
func tagName(b []byte) (string, []byte) {
	i := bytes.IndexAny(b, " \t\r\n/>")
	if i < 0 {
		return string(b), nil
	}
	return string(b[:i]), b[i:]
}

// localName returns name without the prefix of its namespace.
func localName(name string) string {
	if i := strings.LastIndexByte(name, ':'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// nextAttr returns the next attribute of the tag at the start of b, and the
// rest of b. It returns false at the end of the tag.
func nextAttr(b []byte) (string, string, []byte, bool) {
	b = bytes.TrimLeft(b, " \t\r\n")
	if len(b) == 0 || b[0] == '/' || b[0] == '>' {
		return "", "", b, false
	}
	eq := bytes.IndexByte(b, '=')
	if eq < 0 {
		return "", "", nil, false
	}
	attr := string(bytes.TrimSpace(b[:eq]))
	b = bytes.TrimLeft(b[eq+1:], " \t\r\n")
	if len(b) == 0 || (b[0] != '"' && b[0] != '\'') {
		return "", "", nil, false
	}
	end := bytes.IndexByte(b[1:], b[0])
	if end < 0 {
		return "", "", nil, false
	}
	return attr, string(b[1 : 1+end]), b[2+end:], true
}

// utf8Text returns b in UTF-8. It decodes UTF-16 when b starts with a byte
// order mark of UTF-16, and drops the byte order mark of UTF-8.
func utf8Text(b []byte) []byte {
	var big bool
	switch {
	case bytes.HasPrefix(b, []byte("\xEF\xBB\xBF")):
		return b[3:]
	case bytes.HasPrefix(b, []byte("\xFE\xFF")):
		big = true
	case bytes.HasPrefix(b, []byte("\xFF\xFE")):
	default:
		return b
	}
	u := make([]uint16, 0, len(b)/2)
	for i := 2; i+1 < len(b); i += 2 {
		if big {
			u = append(u, uint16(b[i])<<8|uint16(b[i+1]))
		} else {
			u = append(u, uint16(b[i+1])<<8|uint16(b[i]))
		}
	}
	return []byte(string(utf16.Decode(u)))
}
