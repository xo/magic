# Backlog

This document holds the work that is known and not done. Each item names
where it came from.

On 2026-09-27, the repository had no open issue, no pull request, open or
closed, and no `TODO` or `FIXME` comment. Every item below came from the CI
history, from the code or from the work that set up D15.

When you finish an item, remove it. If an item needs a decision, it becomes
an open question at the end of `docs/PLAN.md`, and the answer becomes a file
in `docs/decisions/`.

The cgo package and its archives left with the switch (D47), and with them
the items about the archives, the build script and the handle. The binary
walker is next (D31).

## CI

1. The `cross` job builds the targets that have no runner, such as
   freebsd/amd64 and linux/arm, and runs nothing on them. Source: D47.

## The package

2. The text of an error of the engine is the text of libmagic, which is not
   in the gerund form that `go-pedantry` asks for. The maintainers have not
   decided which one wins. Source: the switch, D47.
3. Add a test that reads the `go.mod` files and fails on a `require` line for
   a module that the maintainers did not approve, as dburl does. D28 and D45
   approve `github.com/ulikunitz/xz` and `github.com/klauspost/compress` for
   the decoder modules only. Source: D14, D28 and D45.
4. A `Result` comes from one walk of the engine for each of its forms (D46).
   One walk that records every form would be about four times faster, and
   must give the same answers to `TestAPIMatchesTheOracle`.
5. `DetectFile` leaves out the extension and the Apple code of a file that is
   not regular. libmagic, asked for them, reads a link as the file it points
   to, and fails on a directory. Source: `ExampleDetectFile`, D47.

6. A `sync.Pool` that reuses the buffers of a detection, as D42 expects. A
   file of 7 MiB takes a new buffer of that size on each call. Source:
   `docs/API.md`.

7. The engine is about as fast as libmagic on most files, and slower on
   text. Measured on 2026-11-15 on linux/amd64, one walk for the MIME type
   took, with the cgo package of `v0.4.1` and with the port: 5.1 µs and
   4.3 µs for a Word document, 20 µs and 26 µs for a GIF image, 35 µs and
   37 µs for an XLSX file, 252 µs and 334 µs for random bytes, and 334 µs and
   1.24 ms for UTF-8 text. Most of the time on text is in Go's `regexp`.
   The port now converts the bytes at each offset to Latin-1 once for a
   detection, not once for each rule (D44), which made text 13.5% faster, to
   1.09 ms. The rest is the matching of `regexp` itself. Source: a benchmark
   of both packages, D46.

## The tests

8. Some cases in `mimeTests` repeat. "kml 2.2" and "ogg" are each the
   name of two cases, so `go test` reports the second one as `ogg#01`.
   "rpm" has the same input as "rpm 1". "svg with comment prefix" has the
   same input as "svg no xml header" and holds no comment. Source:
   `magic_test.go`.
9. Three items of the testing plan of `docs/PLAN.md` are not done. Source:
   the testing plan, items 4 to 6.
   - A test made for each feature of the rule format, so that every type,
     relation and modifier runs at least once.
   - Go fuzzing of the reader of the rules, the engine and each parser, for
     panics and for limits on memory and time.
   - A report of the coverage of statements for each package.

10. `TestOracle` compares MIME types only with the `file` on `PATH`. The test
    module compares every output with the oracle, so this is a quick check,
    and not the gate.

## The port

Phases 2 to 4 found these. None of them changes an answer of the gates.

11. The decoders of Go do not name each fault of damaged compressed data as
    the libraries of C do. zlib prints "invalid distance too far back" where
    the port prints "data error", and libzstd and liblzma give other codes
    for some damaged frames. `TestPortMatchesTheOracle` lists these answers
    apart. A port of the error checks of inflate would close the zlib part.
    Source: `inftrees.c` and `inflate.c` of zlib.
12. The reason that libmagic prints after "Composite Document File V2
    Document" can come from `errno` of an earlier step, because a stream of
    length 0 sets no `errno`. The port does not copy this. Source:
    `cdf_read_long_sector_chain` and `file_trycdf`.
13. The oracle keeps one cookie for all questions. libmagic keeps the value
    of the last rule and the levels of continuation in the cookie, and a
    string compare reads past the end of a string. So an answer of libmagic
    can depend on the question before it. The port starts each detection
    from zero. No test has found a difference. Source: `ms_value` and `c.li`
    in `file.h`.
14. The decoded table holds every entry as a struct of 432 bytes, so a
    program holds about 10.8 MB of rules in memory, as libmagic does. A
    smaller form in memory would cost a change of the engine.
15. The xz decoder gives the data of the first block of a cut stream, where
    liblzma gives the data of every block that it reached.
16. With `MAGIC_NO_COMPRESS_FORK`, file 5.48 refuses its libraries and still
    runs outside programs. The port copies the first part and never runs a
    program (D36, D45).

17. Report two bugs of file to upstream (D48). Since `bcbb6064`,
    `cdf_read_property_info` checks the length of a section against its copy
    of the header on the stack, so on Linux it reads no property of a Compound
    Document. Since `7fdb7bf9`, `file_fsmagic` prints the name of each flag of
    the mode before its comma. The port copies both until upstream fixes them.
    Source: phase 5.

## The packages for ivy

18. No test reads a MOBI book that Mobipocket wrote, or a CHM file that the
    compiler of Microsoft wrote. The tests read data that the compressors of
    the tests wrote. cabextract and 7z read the CAB file and the CHM file of
    `compress/lzx/testdata` as the decoder does. With a book with HUFF/CDIC
    and a CHM file that the repository can hold, a test can compare the
    decoders with the programs that write the formats. The limit of
    64 phrases that hold one another, in `compress/huffcdic`, is not measured
    on such a book either. Source: D61.
19. lzxd moved from GitHub to codeberg.org on 2026-02-09, and its last commit
    on GitHub says so. The ledger follows the copy on GitHub, which gets no
    new commit. A maintainer decides whether the ledger follows codeberg.org.
    Source: D61.
20. `compress/lzx` does not decode LZX DELTA, the form of [MS-PATCH] with
    windows over 2 MiB and matches over 257 bytes. Source: D61.
21. The walker finds a cabinet with LZX and does not extract it, as item 2 of
    the binary walker says. `compress/lzx` can decode it now. Source: D61.
22. No test of `cfb` reads a document with a thumbnail, or an encrypted
    document, that Office wrote. The tests read property sets that the
    writer of the tests made, and the summaries of the corpus, which have
    no thumbnail. Source: D62.
23. `cfb` decodes the text of four code pages. A string in another code
    page, such as 932 for Japanese, keeps its bytes, and the program
    converts them. Source: D62.
24. `cfb` reads no value of type VT_ARRAY, and keeps its bytes. Source: D62.
25. No test reads a DMG file that Apple wrote. The tests of `compress/lzfse`
    read streams that the reference encoder wrote, and the tests of
    `compress/adc` read chunks that the compressor of the tests wrote. With
    a DMG file that the repository can hold, made by hdiutil with ADC and
    with LZFSE, a test can compare the decoders with the chunks of Apple.
    Source: D63.
26. The walker finds `lzfse` and `dmg` and does not extract them, as item 1
    of the binary walker says. `compress/lzfse` and `compress/adc` can
    decode their data now. Source: D63.
27. No test of `container` reads a file that the programs of its formats
    wrote, such as an XPS document of the XPS Document Writer, an APK of
    the Android Gradle plugin or an AZW3 book of KindleGen. The tests read
    the samples of D64 and the files that the tests write. With such files
    that the repository can hold, a test can check the rules of D65 on
    them. Source: D65.
28. On 2026-11-20 `magic.DetectFile` and `container.Detect` read the 292 files
    of the corpus of the review of ivy. They name most files as ivy does.
    Items 29 to 35 hold the files where they do not. The program that read
    them is a short loop over `test/review/corpus.tsv` of ivy, and the files
    are in the cache of that review. Source: the corpus of ivy.
29. `container` does not name an OpenDocument file whose first member is not
    `mimetype`. libmagic then says `application/zip`. In the corpus of ivy,
    `libreoffice/odt/ooo96040-2.odt`, `libreoffice/odt/landscape-a4-table.odt`
    and `libreoffice/odg/rhbz1870501.odg` are such files. The first member of
    `ooo96040-2.odt` is `Versions/`. The member `mimetype`, or the media type
    of the root in `META-INF/manifest.xml`, names the format. Source: the
    corpus of ivy.
30. `container` does not name an Office Open XML file whose first member is
    not `[Content_Types].xml`. libmagic then says "Microsoft OOXML" with
    `application/octet-stream`. `libreoffice/docx/landscape-a4-table.docx` and
    `libreoffice/docx/mixed-orientation.docx` start with `styles.xml` and
    `customXml/`. The content type of the main part in `[Content_Types].xml`
    names the format: a Word document, a sheet of Excel, a presentation or a
    drawing of Visio. ivy now finds them with rules of its own in
    `internal/ooxml`, which ivy D8 says belong in magic. Source: the corpus of
    ivy.
31. `container` does not name the files of iWork: Pages, Numbers and Keynote.
    libmagic says `application/zip`. A file of iWork 2013 and later holds
    `Index/Document.iwa` and other members under `Index/`, and an older bundle
    holds `index.apxl` or `index.xml`. The corpus of ivy has
    `tika/pages/sample-2013.pages`, `tika/numbers/checking-2013.numbers`,
    `tika/key/sample-2013.key` and `tika/key/bundle-2018.key`.
    shared-mime-info gives the MIME types `application/vnd.apple.pages`,
    `application/vnd.apple.numbers` and `application/vnd.apple.keynote`.
    Source: the corpus of ivy.
32. `container` does not name a drawing of Visio 2003 to 2010 (`.vsd`) or a
    file of Publisher (`.pub`). Both are Compound Files. The port says
    `application/x-ole-storage` and "Composite Document File V2 Document,
    Cannot read section info". The `file` 5.48 of the machine of the review
    says `application/vnd.ms-office` for the same files, probably because of
    the fault of item 17. The class ID of the root storage, or the stream
    `VisioDocument` of Visio and the streams `Contents` and `Quill` of
    Publisher, name the format. The corpus of ivy has `poi/vsd/44501e.vsd`,
    `poi/vsd/visio-with-embedded.vsd` and three files under `poi/pub/`.
    Source: the corpus of ivy.
33. The 12 FictionBook files of the corpus of ivy (`.fb2`) are `text/xml`.
    libmagic has no rule for the root element `FictionBook` of the namespace
    `http://www.gribuser.ru/xml/fictionbook/2.0`. shared-mime-info gives
    `application/x-fictionbook+xml`. Such a rule is not a container, so it
    needs a decision of the maintainers on where magic puts a rule that
    libmagic does not have. Source: the corpus of ivy.
34. An EMF file is `application/octet-stream`, with the description "Windows
    Enhanced Metafile (EMF) image data", as in libmagic. The MIME type
    `image/emf` is registered with IANA. The corpus of ivy has 5 EMF files.
    This change also needs a decision, because it differs from libmagic.
    Source: the corpus of ivy.
35. An XPS file whose first member is not `[Content_Types].xml` is
    `application/octet-stream` in `magic.Detect`, and `container.Detect` names
    it `xps`. A program that asks both gets the right answer, as D65 says. The
    corpus of ivy has `poi/xps/test-ppt.xps`, `microsoft/xps/mb07.xps` and
    `repo/xps/example.xps`. A note in the documentation of `DetectFile` can
    say that a program asks `container` for a file of
    `application/octet-stream` with the description "Microsoft OOXML". Source:
    the corpus of ivy.

## The binary walker

The maintainers want magic to do the work of binwalk too: walk a binary file,
such as a firmware image, find the files that are embedded in it at any
offset, and extract them. It comes after the port of libmagic is finished
(D31). `docs/WALKER.md` is its design, with the formats of binwalk-ng.

Phases W1 to W5 of D52 ported the 111 signatures of binwalk 3, in the
packages under `walk/format/`. D60 moved the walker to binwalk-ng, the fork
that is maintained, with its 114 signatures and its fixes, and with no new
decoder. The walker extracts 48 of the 51 formats that binwalk-ng extracts
with its own code, and 15 of the 24 formats that it extracts with
programs. The three gates in `test/diff` give the same
matches, files and walks as binwalk-ng on 2301 inputs, and a test for each
file system and archive compares its files with a tool or with the tree
that made it. These are left:

1. Formats that the walker finds and does not extract:
   - `lzfse`, `rar` and `qnx_ifs`, which binwalk-ng extracts with crates of
     its own. They need decoders, which D60 leaves out. A bare QNX imagefs,
     and an image that is not compressed, need no decoder.
   - `arj`, `apfs` and `efigpt`, which binwalk-ng extracts with 7z.
     binwalk-ng runs 7z again on all the data to the end of the file when
     the first run fails, so it extracts `arj` now.
   - `ntfs`, `dmg`, `linux_kernel`, `pchrom`, `uefi_capsule` and
     `uefi_pi_volume`, which binwalk-ng extracts with other programs.
2. Parts of formats that the walker does not extract: 7-Zip with AES or
   the filter RISCV; cab with LZX or Quantum. The walker reads SquashFS 1.x,
   2.x and 3.x in both byte orders, with each decompressor that sasquatch
   tries, in its order: gzip, lzo, lz4, xz, zstd, LZMA, and the LZMA of
   vendors (adaptive, Broadcom and DD-WRT). sasquatch skips a file whose
   data it cannot decompress, or stops, as the timing of its threads
   decides. The walker skips the file once a decompressor other than gzip
   worked, and stops otherwise. Thus the gate runs binwalk-ng again when its
   SquashFS files differ. The walker decodes
   7-Zip with PPMd, and with the filters BCJ, BCJ2, ARM, ARMT, ARM64, PPC,
   SPARC, IA64 and Delta. binwalk-ng does not find lz4 in the legacy frame,
   because its signature has only the magic of the frame format. Thus the
   walker does not decode the legacy frame either.
3. On damaged LZMA data, 7z writes what it decoded before the error. The
   walker writes nothing for a stream with an error, so the extraction gate
   accepts fewer members on a damaged copy of `7zip` or `xz` data. To write
   the same bytes, the walker needs a decoder that stops where 7z stops.
4. Where binwalk-ng leaves a hole in a file, the walker writes zeros. The
   bytes are the same, but the zeros count against `WithMaxOutput`, so a
   large hole can stop an extraction that binwalk-ng completes. This holds
   for the chunks of `android_sparse` that hold nothing or only zeros, and
   for the gaps between the records of `srecord`. `Output` needs a write
   at an offset, and a sink that keeps holes, to do better.
5. binwalk-ng loops without end on a `dtb` whose node or property does not
   fit in the data. The walker rejects the match. No input of the corpus
   has such a node, so the oracle carries no patch for it (D59).
6. lzop, jefferson, unyaffs, ubireader and tsk_recover are not installed,
   so no test compares the walker with them. The tests compare with the
   tree that made each image, and with mksquashfs, unsquashfs, 7z,
   cabextract and lz4 where they are installed. Where sasquatch is
   installed, `TestWalkExtractsAsBinwalk` compares SquashFS with binwalk-ng,
   which runs it.
7. `FuzzWalk` in `walk/format/all` fuzzes every format with the walk of
   members. Runs of 3 and 4 minutes found no panic and no input over 5
   seconds, and a run of 2 minutes no input over 1 second. The rate drops at
   times while the fuzzer minimizes a new input. CI runs only its seeds.
   It did not run again after the move to binwalk-ng.
8. Under a heavy load, sasquatch fails more often, and binwalk-ng can exit
   before its worker starts. The clients retry, and the extraction gate
   runs each input that differs once more, alone, but not without limit.
9. delink tries the keys of the format DLK in the order of a HashMap of
   Rust, which is random, and the walker in the order of their names. No
   sample of `dlke`, `dkbs` or `encfw` decrypts yet.

Before the work started, on 2026-11-15, the three models of the plan said
this:

1. Do not run the whole rule engine at every byte. Collect the fixed bytes of
   each signature into one matcher for many patterns, such as Aho-Corasick,
   and run the engine only where it finds a candidate. All three models said
   this.
2. Keep a signature set for the walker. The rules of libmagic name a whole
   file, and the signatures of binwalk find a file inside another. Gemini Pro
   suggests translating the signatures of binwalk 2 into the rule format, so
   that one engine reads both. Gemini Pro also said that binwalk 2 uses
   libmagic inside. That is wrong: binwalk 2 replaces libmagic with code of
   its own. binwalk 3 is a rewrite in Rust under the MIT license, and its
   signatures are code, not rules. `docs/WALKER.md` gives the details.
3. Keep three steps apart: find a candidate, find where it ends, and extract
   it. A match does not say how long the embedded file is, so each format needs
   code that finds its end.
4. Work from an `io.ReaderAt`, and stop when a `context.Context` is cancelled.

The formats, from easy to hard:

| Group | Formats | Work |
| --- | --- | --- |
| In the standard library | gzip, zlib, bzip2, zip, tar, ELF headers | find the end, then extract |
| Simple headers | uImage, cpio, common images such as PNG, JPEG and GIF | a small parser each |
| Outside the standard library | xz, raw LZMA, zstd | the packages of D28 |
| Firmware file systems | SquashFS, CramFS, JFFS2, UBIFS | a large parser each. SquashFS has vendor variants. Gemini Pro names `github.com/sylabs/squashfs`, and knows no library for the other three |

Safety limits are needed from the first release: limits on the size of the
output, on the ratio of compression, on the depth of recursion and on time, a
check that no path leaves the output folder, and no device files or links that
point out of it.

The estimates, in engineer-weeks for one experienced Go engineer, after the
port:

| Source | Weeks |
| --- | --- |
| Gemini Pro | 6 to 11, without parsers for CramFS, JFFS2 and UBIFS |
| DeepSeek Pro | 9 to 14 |
| OpenAI Pro | 36 to 76, of which the firmware file systems are 18 to 40 |
| The plan | 23 to 48: scanning 5 to 9, common extraction 5 to 8, SquashFS 3 to 6, CramFS, JFFS2 and UBIFS 10 to 25 |

The maintainers answered the questions of the design with D49 to D52: the
walker follows binwalk 3 with an oracle, it is the package `walk`, it carries
the keys of binwalk, and it is built in the phases W1 to W5. The license of
binwalk is MIT, so its signatures are translated with its notice.
