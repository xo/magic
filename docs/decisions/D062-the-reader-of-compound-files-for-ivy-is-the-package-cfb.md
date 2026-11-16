# D62. The reader of Compound Files for ivy is the package cfb

Status: Proposed.

A Compound File is a container of Microsoft. It holds a tree of storages and
streams, as a file system holds folders and files. On 2026-11-17, ivy needed
to read Compound Files for three purposes:

1. The binary formats of Office: Word (.doc), Excel (.xls) and PowerPoint
   (.ppt). Each one is a Compound File, with streams such as "WordDocument",
   "1Table" and "Workbook".
2. The encryption of [MS-OFFCRYPTO]. An encrypted .docx file is a Compound
   File with the streams "EncryptionInfo" and "EncryptedPackage".
3. The property sets of [MS-OLEPS]. The stream "\x05SummaryInformation"
   holds the title and the author of a document, and its property
   PIDSI_THUMBNAIL holds the preview. The preview is a WMF, an EMF, a DIB or
   a JPEG image.

The engine reads Compound Files in `internal/engine/cdf.go`, a translation of
cdf.c of libmagic, but no program outside magic can import it. The
maintainers decided in ivy D24 that magic makes its reader of Compound Files a
public package, and that the change waits in magic for the review of a
maintainer, as D61 did.

## The decision

1. The reader is the package `cfb` in the root module. Its name is the name
   of the specification, [MS-CFB]. It is not in `compress/`, because a
   Compound File is a container, not compressed data. It uses only the
   standard library, so `go.mod` keeps no `require` line (D14). magic does
   not import it, so a program that does not import it does not pay for it
   (D38).
2. The same package reads property sets. A property set is a stream of a
   Compound File, and ivy reads both together.
3. The API is small:
   - `cfb.Open(r io.ReaderAt, size int64, lim cfb.Limits) (*cfb.File, error)`
     reads the header, the table of sectors and the directory.
     `cfb.Limits` holds the most sectors, directory entries and bytes of a
     stream. A field of 0 takes its default.
   - `(*File).Root() *Entry` gives the root of the directory. An `Entry` has
     a name, a type, a CLSID, two times, a size and its children.
   - `(*File).Lookup(name string) (*Entry, error)` finds an entry by its
     path, such as "ObjectPool/_1234/\x01Ole". Names compare without case,
     as [MS-CFB] says.
   - `(*File).Open(name string) (*io.SectionReader, error)` and
     `(*File).OpenEntry(e *Entry) (*io.SectionReader, error)` read a stream,
     from the sectors of the file or from the mini stream.
     `(*File).ReadFile(name string) ([]byte, error)` reads all of it.
   - `cfb.ParsePropertySet(b []byte) (*cfb.PropertySet, error)` reads a
     property set: its sections, the code page and the dictionary of each
     section, and its properties. `(*PropertySet).Thumbnail()` gives the
     property PIDSI_THUMBNAIL.
4. A property keeps its value as a Go value of its type. A string of a code
   page is a `CodePageString`, which holds its bytes and its code page. Its
   method `Text` decodes UTF-16, UTF-8, the code page 1252 and ISO 8859-1. For
   another code page, `Text` decodes only ASCII, and the program converts the
   bytes itself. The data of the clipboard is a `ClipboardData`, with the
   format of the clipboard and the bytes of the image. The package keeps the
   bytes of a type that it does not read.
5. Hostile data must not crash the reader or make it hang:
   - The reader reads the table of sectors only for the sectors that the
     file holds, so the size of the file limits the memory of the table.
   - A chain of sectors that loops, names a sector past the end, or is
     shorter than its stream gives an error. The chain of a stream is never
     longer than its stream needs, so the limits of sectors and of the size
     of a stream also limit the length of a chain. `Limits` has no field for
     it.
   - An entry of the directory that a second link names is left out, so a
     loop in the tree of the directory ends.
   - A property whose value does not fit in its section is left out.
   - An error wraps `cfb.ErrCorrupt` or `cfb.ErrLimit`, and starts with
     `cfb: `, as D61 says for the decoders for ivy. The package has two fuzz
     tests, `FuzzOpen` and `FuzzParsePropertySet`.
6. The engine keeps its own reader in `internal/engine/cdf.go`, and does not
   use `cfb`. The engine must give the output of libmagic, and that output
   names the step at which the reader of libmagic fails, such as "Can't read
   SAT" (D34). The engine keeps the limits of libmagic and the bug of commit
   `bcbb6064`, which reads no property on Linux (D48, item 17 of
   `docs/BACKLOG.md`). `cfb` follows the specifications. It reads some files
   that libmagic refuses, and it refuses some that libmagic reads. If the
   engine uses `cfb`, the API of `cfb` must carry the faults of libmagic, or
   the output of the port changes.

## The sources

`cfb` is written from [MS-CFB] and [MS-OLEPS]. It is not a translation of
cdf.c, and it copies no code of `internal/engine/cdf.go`, so it carries the
MIT license of magic, and not the license of file (D29). libgsf and
LibreOffice are under the LGPL or the GPL, so no code of them was read or
copied.

The reader differs from the specifications in these places:

1. It takes the size of a sector from the header, and accepts 512 or 4096
   bytes with either version.
2. It takes the cutoff of the mini stream from the header, as the program
   that wrote the file gave it.
3. [MS-OLEPS] pads each string of a vector to a multiple of 4 bytes. Some
   programs do not. The reader skips padding only when the bytes of the
   padding are zero, so it reads both forms.

## How the tests check the reader

1. A small writer in the tests of the package makes Compound Files of
   version 3 and version 4. The files have streams in the mini stream and in
   sectors, a storage in a storage, an empty stream, a property set with a
   thumbnail, and a file with more than 109 sectors of the table, which needs
   a DIFAT sector. The tests also change the files in one place, to make
   loops and chains that are too short.
2. The tests read the Compound Files of `testdata/`. Each one is the first
   4096 bytes of a file. `doc.doc` and `msi.msi` open. The others give
   `cfb.ErrCorrupt`, because their directory is past the cut.
3. The tests read the whole Word and Excel files of `test/corpus/testdata/`,
   and the property sets of their summaries. A copy of the module from the
   module proxy has no test module, so this test skips there.
4. On 2026-11-17, each fuzz test ran for one minute and found no fault.

[MS-CFB]: https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-cfb/
[MS-OLEPS]: https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-oleps/
[MS-OFFCRYPTO]: https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-offcrypto/
