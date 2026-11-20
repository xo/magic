# D67. Rules beyond libmagic are the option WithExtraRules

Status: Proposed.

On 2026-11-20 the maintainers decided that magic holds rules that libmagic
does not have, as an option. `magic.Detect` and the other entries keep the
output of libmagic by default, so the gates of D11 and D48 still hold. Items
33 and 34 of `docs/BACKLOG.md` give the first two rules. This decision
proposes the option, how it changes each form of a `Result`, and how a rule
is added.

## The option

```go
func WithExtraRules() Option
```

`WithExtraRules` takes no argument, as `WithAll` and `WithDecompress` take
none. It turns on every rule of magic. A rule of magic is Go code in
`extra.go`, not a rule in the format of libmagic.

Each entry runs the walks of libmagic first, as it does without the option
(D46). Then it tries the rules of magic in their order, and the first rule
that matches changes the result. A rule reads the first match of libmagic,
and can read the start of the content: at most 64 KiB, and at most the limit
of `WithLimit`. An entry reads these bytes only when a rule asks for them.
For `Detect` and `DetectReader` they are already in memory. `DetectReaderAt`
reads them from its reader, and `DetectFile` opens the file again.

The option keeps the rules in a function value, so a program that does not
call `WithExtraRules` does not link them (D38). On 2026-11-20, on
linux/amd64, the program of `TestPortSize` kept its size, and grew by
12,288 bytes, stripped, when it called the option.

## How a rule changes a result

A rule gives a value for some of the forms. A form that the rule gives
replaces the one of libmagic. A form that the rule does not give stays as
libmagic gives it:

| Form | What the rule does |
| --- | --- |
| `Description` | replaces the first line, if the rule gives one. With `WithAll`, the other lines stay |
| `MIME` | replaces the MIME type, if the rule gives one |
| `Charset` | never changes. The charset tells how the bytes are encoded, which a rule of magic does not change |
| `Extensions` | replaces the extensions, if the rule gives them |
| `Apple` | replaces the Apple code, if the rule gives one |
| `Matches` | the first entry changes as the fields above. The other entries stay |

A file that is not regular, such as a directory, keeps the answer of
libmagic, because libmagic names it by its kind.

## The first two rules

| Rule | Matches when | MIME type | Description | Extensions |
| --- | --- | --- | --- | --- |
| FictionBook 2 | the MIME type of libmagic starts with `text/`, and the root element is `FictionBook` in the namespace `http://www.gribuser.ru/xml/fictionbook/2.0` | `application/x-fictionbook+xml` | FictionBook document | `fb2` |
| EMF | the description of libmagic starts with "Windows Enhanced Metafile (EMF) image data" | `image/emf` | the one of libmagic | `emf` |

The MIME type and the extension of FictionBook are the ones of
shared-mime-info, and the description is its comment. libmagic names a
FictionBook book "XML 1.0 document" with `text/xml`, or text with
`text/plain` when the book has no XML declaration. The rule reads the root
element with a small reader, not with `encoding/xml`, which adds to the size
of every program that imports magic (D38). The reader skips a byte order
mark, the XML declaration, comments, processing instructions and a document
type declaration. It reads an encoding that holds ASCII, such as UTF-8 or
windows-1251. It does not read UTF-16.

The MIME type and the extension of EMF are the ones that IANA registers, in
RFC 7903. libmagic names an EMF image, and gives no MIME type, so the
description of libmagic stays.

## How a rule is added

1. The format has an item in `docs/BACKLOG.md`, and the maintainers accept
   the rule.
2. The rule matches only a format that libmagic names in general terms,
   such as XML, text or `application/octet-stream`. It does not replace a
   specific answer of libmagic.
3. The MIME type is the one of IANA. If IANA has none, it is the one of
   shared-mime-info. If shared-mime-info has none, the maintainers choose.
4. The rule is an entry of `extraRules` in `extra.go`. Its comment names the
   source of the MIME type.
5. A sample that the tests make goes in `testdata/`, and in `extraSamples`
   of `extra_test.go`, with the answer of libmagic and the answer of the
   rule. The gates of the test module compare the answer of libmagic with
   the oracle, because the corpus holds the files of `testdata/`.
6. The doc comment of `WithExtraRules` lists the rule.

When upstream adds a rule for the format, the port of that commit removes
the rule of magic, if the answer of libmagic is the same. If it is not the
same, the maintainers choose which stays.

## The alternatives

1. Rules in the format of libmagic, in a file of magic that the compiler
   adds to the table, or to a second table. The rules of libmagic cannot
   read the namespace of the root element of an XML document. A rule in the
   same table also competes with the rules of libmagic by its strength, so
   it can change the answer for other files. The compiler, `go generate`
   and the gates would each need a second path. Go code is smaller, and it
   starts from the answer of libmagic, so it cannot change that answer for
   another format.
2. An exported type `Rule`, and an option `WithRules(...Rule)`, so that a
   program adds its own rules, or chooses some of the rules of magic. A
   caller cannot add rules (D35, and the reviews in `docs/API.md`), and the
   exported type makes the API larger for every later rule. One option with
   no argument stays the same when a rule is added.

The package `container` (D64, D65) is not an alternative. It names the
format inside a container, and magic does not import it. FictionBook and
EMF are not in a container.
