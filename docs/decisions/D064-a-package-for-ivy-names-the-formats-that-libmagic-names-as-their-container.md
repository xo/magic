# D64. A package for ivy names the formats that libmagic names as their container

Status: Decided.

On 2026-11-18, a maintainer asked that magic tell these formats apart from
the containers that hold them, for ivy (ivy D8): XPS and OpenXPS from zip, an
encrypted Office Open XML file from other Compound Files, AZW3 from MOBI, CBZ
from zip, APK and IPA from zip, and Krita and OpenRaster from zip.

magic gives the output of libmagic at the commit of the ledger (D48). No
decision lets it report a type that libmagic does not. This decision says how
magic does it, and answers question 26 of `docs/PLAN.md`.

## What libmagic reports

`TestContainers` in `containers_test.go` holds a sample of each format, in
`testdata/`. The oracle at the commit of the ledger, and file 5.48, report
the same for each:

| Sample | Description | MIME type | Told apart |
| --- | --- | --- | --- |
| XPS, `[Content_Types].xml` first | Microsoft OOXML | application/octet-stream | no |
| XPS, `FixedDocumentSequence.fdseq` first | Zip archive data, ... | application/zip | no |
| OpenXPS | Zip archive data, ... | application/zip | no |
| encrypted Office Open XML | CDFV2 Encrypted | application/encrypted | yes, by the stream EncryptedPackage |
| MOBI | Mobipocket E-book ..., version 6 | application/x-mobipocket-ebook | by the description only |
| AZW3 | Mobipocket E-book ..., version 8 | application/x-mobipocket-ebook | by the description only |
| CBZ | Zip archive data, ... | application/zip | no |
| APK, `AndroidManifest.xml` first | Android package (APK), with AndroidManifest.xml | application/vnd.android.package-archive | yes |
| APK, `res/` first | Zip archive data, ... | application/zip | no |
| IPA, `Payload/` first | IOS/iPadOS IPA file ... iOS App | application/x-ios-app | yes |
| Krita | Zip data (MIME type "application/x-krita"?) | application/zip | by the description only |
| OpenRaster | Zip data (MIME type "image/openraster"?) | application/zip | by the description only |

These are the gaps:

1. libmagic reads the first members of a zip file, and the last 512 bytes
   for some types. It finds no XPS, no OpenXPS and no CBZ. It finds an APK
   only when a member that it knows comes first or near the end, and an IPA
   only when `Payload/` comes first.
2. "CDFV2 Encrypted" also names a binary Office file whose properties are
   encrypted, with the stream EncryptedSummary. It does not say which Office
   format the encrypted package holds. Only the key can tell.
3. The version of a MOBI book is the version that a reader needs. A book
   with both forms, MOBI and KF8, says version 6, and holds its KF8 part
   after a boundary that the EXTH record 121 gives.
4. For Krita and OpenRaster, the type is in the description, between the
   quotes, and the MIME type is application/zip.

## The choices

1. A package of its own, as `cfb` is (D62). After `magic.Detect` names a zip
   file, a Compound File or a Mobipocket book, a program calls the package
   with the file. The package reads the central directory of the zip file,
   the directory of the Compound File, or the records of the book, and
   names the format, with a MIME type of its own. `Detect` keeps the output
   of libmagic, and magic does not import the package (D38).
2. An option of `Detect`, such as `WithContainers`, that fills a new field
   of `Result` with the same answer. The other fields keep the output of
   libmagic. The API changes, which D42 settled, and the root package grows
   for every program.
3. Rules of magic, which `Detect` runs after the rules of libmagic. The
   output of `Detect` then differs from libmagic, and the gates of the test
   module must leave these types out (D48).
4. Rules for upstream. A maintainer sends rules to file, and magic takes
   them when the ledger moves. Rules can find XPS and OpenXPS by their
   first members, but they cannot read the list of members of a zip file,
   so they cannot find a CBZ file or every APK file.

## The decision

On 2026-11-18 the maintainers chose choice 1. It keeps D48, it costs nothing
to a program that does not import it, and it reads the containers with the
standard library and `cfb`. The maintainers choose its name and its API when
it is written. The package needs these facts:

1. XPS and OpenXPS: the relationship of type `fixedrepresentation` in
   `_rels/.rels`. Its namespace is the one of XPS,
   `http://schemas.microsoft.com/xps/2005/06`, or the one of OpenXPS,
   `http://schemas.openxps.org/oxps/v1.0`.
2. An encrypted Office Open XML file: the streams EncryptionInfo and
   EncryptedPackage ([MS-OFFCRYPTO]).
3. AZW3: a MOBI header of version 8, or the EXTH record 121 of a book with
   both forms.
4. CBZ: a zip file whose members are images, with ComicInfo.xml or not.
5. APK: the members AndroidManifest.xml and classes.dex. IPA: a member
   `Payload/<name>.app/`.
6. Krita and OpenRaster: the member `mimetype`, stored first, with
   `application/x-krita` or `image/openraster`.

[MS-OFFCRYPTO]: https://learn.microsoft.com/en-us/openspecs/office_file_formats/ms-offcrypto/
