package magic

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"sync"
)

// WithExtraRules adds the rules of magic that libmagic does not have (D67).
// A rule of magic starts from the answer of libmagic, and gives a more exact
// MIME type, description or extensions for a format that libmagic names in
// general terms. For example, libmagic names a FictionBook book as XML. A
// form that a rule does not give, and the charset, stay as libmagic gives
// them. By default, and for a file that is not regular, the answer is the
// one of libmagic.
//
// The rules are:
//
//   - FictionBook 2: text whose root element is FictionBook in the namespace
//     of FictionBook 2.0 is "application/x-fictionbook+xml", the type of
//     shared-mime-info, with the extension "fb2".
//   - EMF: what libmagic names a Windows Enhanced Metafile is "image/emf",
//     the type of IANA, with the extension "emf".
func WithExtraRules() Option {
	return func(o *options) error {
		o.extra = applyExtra
		return nil
	}
}

// headMax is how many bytes a rule of magic reads at most, from the start
// of the content, 64 KiB. WithLimit can make it smaller.
const headMax = 64 * 1024

// extraRule is a rule of magic that libmagic does not have (D67).
type extraRule struct {
	// match reports whether the rule names the content. m is the first match
	// of libmagic. head returns the first bytes of the content, at most
	// the size that headSize gives, and reads them only when a rule calls it.
	match func(m Match, head func() ([]byte, error)) (bool, error)

	// The forms that the rule gives. An empty form keeps the one of
	// libmagic.
	desc  string
	mime  string
	ext   []string
	apple string
}

// extraRules are the rules of WithExtraRules, in the order that they are
// tried. The first that matches gives the answer. A rule matches only a
// format that libmagic names in general terms, so that it does not
// contradict a rule of libmagic. A rule leaves when libmagic gets one for
// the format (D67).
var extraRules = []extraRule{
	{
		match: matchFictionBook,
		desc:  "FictionBook document",
		mime:  "application/x-fictionbook+xml",
		ext:   []string{"fb2"},
	},
	{
		match: matchEMF,
		mime:  "image/emf",
		ext:   []string{"emf"},
	},
}

// applyExtra changes r with the first rule of magic that matches it.
func applyExtra(r *Result, head func() ([]byte, error)) error {
	head = sync.OnceValues(head)
	for _, x := range extraRules {
		ok, err := x.match(r.Matches[0], head)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		m := &r.Matches[0]
		if x.desc != "" {
			m.Description = x.desc
		}
		if x.mime != "" {
			m.MIME = x.mime
		}
		if x.ext != nil {
			m.Extensions = slices.Clone(x.ext)
		}
		if x.apple != "" {
			m.Apple = x.apple
		}
		r.MIME, r.Extensions, r.Apple = m.MIME, m.Extensions, m.Apple
		desc := make([]string, len(r.Matches))
		for i, m := range r.Matches {
			desc[i] = m.Description
		}
		r.Description = strings.Join(desc, "\n- ")
		return nil
	}
	return nil
}

// readHead reads the first n bytes of r, or fewer when r ends before.
func readHead(r io.Reader, n int) ([]byte, error) {
	b := make([]byte, n)
	m, err := io.ReadFull(r, b)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return nil, fmt.Errorf("magic: reading: %w", err)
	}
	return b[:m], nil
}

// fictionBookNS is the namespace of FictionBook 2.0.
const fictionBookNS = "http://www.gribuser.ru/xml/fictionbook/2.0"

// matchFictionBook matches text whose root element is FictionBook in the
// namespace of FictionBook 2.0, as the rule of shared-mime-info does.
// libmagic names it XML, or text when it has no XML declaration.
func matchFictionBook(m Match, head func() ([]byte, error)) (bool, error) {
	if !strings.HasPrefix(m.MIME, "text/") {
		return false, nil
	}
	b, err := head()
	if err != nil {
		return false, err
	}
	name, attrs, ok := rootElement(string(b))
	if !ok {
		return false, nil
	}
	prefix, local, found := strings.Cut(name, ":")
	if !found {
		prefix, local = "", name
	}
	if local != "FictionBook" {
		return false, nil
	}
	key := "xmlns"
	if prefix != "" {
		key += ":" + prefix
	}
	return attrs[key] == fictionBookNS, nil
}

// emfDescription is how libmagic starts the description of an EMF image. Its
// rule in Magdir/msdos gives no MIME type.
const emfDescription = "Windows Enhanced Metafile (EMF) image data"

// matchEMF matches what libmagic names an EMF image.
func matchEMF(m Match, _ func() ([]byte, error)) (bool, error) {
	return strings.HasPrefix(m.Description, emfDescription), nil
}

// rootElement returns the name and the attributes of the root element of
// the XML document that s starts. It skips a byte order mark of UTF-8, the
// XML declaration, comments, processing instructions and a document type
// declaration. It reads an encoding that ASCII is a part of, such as UTF-8
// or windows-1251, and not UTF-16. ok is false when s does not start an XML
// document, or ends before the start tag of the root element ends.
func rootElement(s string) (name string, attrs map[string]string, ok bool) {
	s = strings.TrimPrefix(s, "\ufeff")
	for {
		s = strings.TrimLeft(s, xmlSpace)
		var end int
		switch {
		case strings.HasPrefix(s, "<?"):
			end = index(s, "?>")
		case strings.HasPrefix(s, "<!--"):
			end = index(s, "-->")
		case strings.HasPrefix(s, "<!"):
			end = doctypeEnd(s)
		case strings.HasPrefix(s, "<"):
			return startTag(s[1:])
		default:
			return "", nil, false
		}
		if end < 0 {
			return "", nil, false
		}
		s = s[end:]
	}
}

// xmlSpace is the white space of XML.
const xmlSpace = " \t\r\n"

// index returns the offset after the first sep in s, or -1.
func index(s, sep string) int {
	i := strings.Index(s, sep)
	if i < 0 {
		return -1
	}
	return i + len(sep)
}

// doctypeEnd returns the offset after the document type declaration that s
// starts, or -1. The declaration can hold an internal subset in brackets,
// whose declarations hold ">", and strings in quotes.
func doctypeEnd(s string) int {
	var quote byte
	depth := 0
	for i := 2; i < len(s); i++ {
		c := s[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '"' || c == '\'':
			quote = c
		case c == '[':
			depth++
		case c == ']':
			depth--
		case c == '>' && depth <= 0:
			return i + 1
		}
	}
	return -1
}

// startTag reads the start tag that s starts, after its "<": the name, and
// the attributes up to ">" or "/>".
func startTag(s string) (name string, attrs map[string]string, ok bool) {
	i := strings.IndexAny(s, xmlSpace+"/>")
	if i <= 0 {
		return "", nil, false
	}
	name, s = s[:i], s[i:]
	attrs = make(map[string]string)
	for {
		s = strings.TrimLeft(s, xmlSpace)
		if strings.HasPrefix(s, ">") || strings.HasPrefix(s, "/>") {
			return name, attrs, true
		}
		eq := strings.IndexByte(s, '=')
		if eq <= 0 {
			return "", nil, false
		}
		key := strings.TrimRight(s[:eq], xmlSpace)
		s = strings.TrimLeft(s[eq+1:], xmlSpace)
		if s == "" || s[0] != '"' && s[0] != '\'' {
			return "", nil, false
		}
		end := strings.IndexByte(s[1:], s[0])
		if end < 0 {
			return "", nil, false
		}
		attrs[key] = s[1 : 1+end]
		s = s[2+end:]
	}
}
