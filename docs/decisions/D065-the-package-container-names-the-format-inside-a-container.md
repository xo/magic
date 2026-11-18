# D65. The package container names the format inside a container

Status: Decided.

D64 decides that a package of its own names the formats that libmagic names
as their container, for ivy (ivy D8). D64 also gives the facts that the
package reads, and leaves its name and its API to the maintainers. A
maintainer asked on 2026-11-18 to write the package. This decision proposes
its name and its API, and the rules that the package uses where D64 gives
only the facts. The maintainers answer question 27 of `docs/PLAN.md` when
they accept or change it.

## What D64 decides

These parts are decided, and the package follows them:

1. magic does not import the package, so the package adds nothing to a
   program that does not import it (D38). `magic.Detect` keeps the output of
   libmagic (D48), and its API does not change (D42).
2. The package reads the containers with the standard library and `cfb`
   (D62). `go.mod` keeps no `require` line (D14).
3. The package reads the facts of D64: the relationship
   `fixedrepresentation` of `_rels/.rels` for XPS and OpenXPS, the streams
   EncryptionInfo and EncryptedPackage of an encrypted Office Open XML file,
   the version of the MOBI header and the EXTH record 121 for AZW3, the
   images of a CBZ file, the members of an APK and of an IPA, and the member
   `mimetype` of Krita and OpenRaster.

## The proposal

### The name and the API

The package is `container`, in the root module. It names the format inside
a container. Another name that fits is `inside`.

```go
func Detect(r io.ReaderAt, size int64, lim Limits) (Format, error)

type Limits struct {
	Members    int // the most members of a zip file, or entries of a Compound File
	MemberSize int // the most bytes that Detect reads from one member or record
}

type Format int

const (
	Unknown Format = iota
	XPS
	OpenXPS
	EncryptedOOXML
	AZW3
	CBZ
	APK
	IPA
	Krita
	OpenRaster
)

func (f Format) String() string      // "xps"
func (f Format) MIME() string        // "application/vnd.ms-xpsdocument"
func (f Format) Description() string // "XPS document"
```

`Detect` follows `cfb.Open` (D62): a reader, a size and limits, with no
context, because the limits bound the work. It finds the container by its
signature, so it does not need the `Result` of `magic.Detect`, and it does
not import the root package. It returns `Unknown` and no error for a file
that is not a zip file, a Compound File or a Mobipocket book, and for a
container that holds none of the formats. A program then keeps the answer of
`magic.Detect`. An error wraps `ErrCorrupt` or `ErrLimit`, and starts with
`container: `, as D61 says for the packages for ivy.

The package returns a format only where libmagic does not name it, or names
it only in the description. A MOBI book with no KF8 part, an EPUB or an
OpenDocument file is `Unknown`, because libmagic names it.

### The MIME types and the descriptions

A MIME type and a description are the ones of shared-mime-info of
freedesktop.org. Where shared-mime-info has none, they are the ones of
libmagic:

| Format | MIME type | Description |
| --- | --- | --- |
| XPS | application/vnd.ms-xpsdocument | XPS document |
| OpenXPS | application/oxps | OpenXPS document |
| EncryptedOOXML | application/encrypted | Microsoft Office Open XML document, encrypted |
| AZW3 | application/vnd.amazon.mobi8-ebook | Kindle book document (KF8) |
| CBZ | application/vnd.comicbook+zip | Comic book archive (zip container) |
| APK | application/vnd.android.package-archive | Android package |
| IPA | application/x-ios-app | iOS application package |
| Krita | application/x-krita | Krita document |
| OpenRaster | image/openraster | OpenRaster image |

shared-mime-info has no type for an encrypted Office Open XML file and none
for an IPA. libmagic gives `application/encrypted` and
`application/x-ios-app`. Only the key tells which Office format an encrypted
file holds, so `EncryptedOOXML` tells it apart from other encrypted Compound
Files, and its MIME type stays the one of libmagic.

### The rules

`Detect` reads the zip file in this order. The first rule that matches
names the format:

1. A member `mimetype` gives Krita for `application/x-krita`, OpenRaster
   for `image/openraster`, and `Unknown` for another value, such as the one
   of EPUB. The member can be anywhere and compressed, which libmagic does
   not accept.
2. A relationship in `_rels/.rels` whose type is the one of D64 gives XPS
   or OpenXPS. The names of the parts and the types compare without case,
   as the Open Packaging Conventions say. `Detect` also reads a
   `_rels/.rels` in pieces, such as `_rels/.rels/[0].piece`, which XPS
   allows. It reads the tags and their attributes, and it is not a parser
   of XML. A part of the relationships holds no other markup.
3. `AndroidManifest.xml` in the root, with `classes.dex` or
   `resources.arsc` in the root, gives APK. D64 names `classes.dex`. An APK
   with no code, such as a split APK, has only `resources.arsc`. An Android
   library, an AAR file, has `AndroidManifest.xml` and neither of the two,
   and gives `Unknown`.
4. A member in `Payload/<name>.app/` gives IPA. A folder of its own is not
   needed, because many programs write none.
5. A zip file whose members are images gives CBZ. An image has the
   extension `.avif`, `.bmp`, `.gif`, `.jpe`, `.jpeg`, `.jpg`, `.jxl`,
   `.png`, `.tif`, `.tiff` or `.webp`, in any case. A CBZ can also hold
   folders, `ComicInfo.xml`, notes and checksums (`.txt`, `.nfo`, `.sfv`),
   hidden files, `__MACOSX/`, `Thumbs.db` and `desktop.ini`, which tools
   add. Another member gives `Unknown`. The rule needs one image at least.
   Comic book readers read the same extensions. A zip file of photos is a
   CBZ by this rule, and a viewer of comic books shows it as one.

A Compound File with the streams EncryptionInfo and EncryptedPackage in its
root gives `EncryptedOOXML`. A binary Office file with the stream
EncryptedSummary has no EncryptedPackage, so it gives `Unknown`, as D64
says.

A Mobipocket book with a MOBI header of version 8 gives AZW3. A book with
both forms has version 6 and the EXTH record 121. The record gives the
number of the first record of the KF8 part. `Detect` reads that record, or
the record after it when it is `BOUNDARY`, and gives AZW3 when its MOBI
header has version 8.

### The limits

A damaged or hostile file must not crash `Detect` or make it hang:

1. `Detect` reads the central directory of a zip file one member at a time,
   and keeps no name. It reads at most `Members` members, 262,144 by
   default. It reads ZIP64 and a zip file after other bytes.
2. It reads at most two members, `mimetype` and `_rels/.rels`, or at most
   1,024 pieces of `_rels/.rels`. It reads at most `MemberSize` bytes of
   each, 64 KiB by default, and at most 4 times as many compressed bytes. A
   member that it cannot read, such as an encrypted one, names no format.
3. It opens a Compound File with `cfb.Open`, with `Members` as the most
   entries. An error of `cfb` also wraps `ErrCorrupt` or `ErrLimit`.
4. It reads at most `MemberSize` bytes of the first record of a book, and
   at most 40 bytes of the record of the KF8 part.
5. A fuzz test, `FuzzDetect`, starts from the samples of D64. On
   2026-11-18 it ran for one minute and found no fault.

## How the tests check the package

1. `TestSamples` reads the samples of `TestContainers` in `testdata/`, and
   the other Office, Java and Compound Files there. Each sample of D64 gives
   its format, the MOBI book gives `Unknown`, and the samples that are cut
   at 4096 bytes give `ErrCorrupt` or `Unknown`.
2. `TestDamaged` cuts each sample at each length, and changes each byte in
   three ways. Each copy gives no error, or an error that wraps
   `ErrCorrupt` or `ErrLimit`.
3. Other tests write zip files, Compound Files and books for each rule
   above: the parts in another order, UTF-16 relationships, pieces, ZIP64
   with more than 65,535 members, a ZIP64 extra field, an AAR file, a
   binary Office file with EncryptedSummary, and books with both forms.

On 2026-11-18 the maintainers accepted the name `container` and the API
above, and answered question 27.
