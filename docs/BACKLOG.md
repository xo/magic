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

## The binary walker

The maintainers want magic to do the work of binwalk too: walk a binary file,
such as a firmware image, find the files that are embedded in it at any
offset, and extract them. It comes after the port of libmagic is finished
(D31). `docs/WALKER.md` is its design, with the formats of binwalk 3.

Phases W1 to W5 of D52 ported the 111 signatures of binwalk, in the packages
under `walk/format/`, and the extractors of 39 of them that binwalk runs with
its own code. The walker also extracts most of the formats that binwalk
extracts with programs, and writes what those programs write. The three
gates in `test/diff` give the same matches, files and walks as binwalk on
1821 inputs, and a test for each file system and archive compares its files
with a tool or with the tree that made it. These are left:

1. Formats that the walker finds and does not extract, where binwalk runs a
   program: `lzfse`, `arj`, `rar`, `ntfs`, `pchrom`, `qnx_ifs`,
   `uefi_capsule` and `uefi_pi_volume`. binwalk extracts nothing from `arj`
   either, because it carves only the first header.
2. Parts of formats that the walker does not extract: SquashFS before 4.0
   and the variants of LZMA of vendors, which sasquatch reads; 7-Zip with
   filters (BCJ, BCJ2, Delta), AES or PPMd; cab with LZX or Quantum; lz4 in
   the legacy frame.
3. sasquatch, 7zz, lzop, jefferson, unyaffs, ubireader and tsk_recover are
   not installed, so no test compares the walker with them. The tests
   compare with the tree that made each image, and with mksquashfs,
   unsquashfs, 7z (p7zip), cabextract and lz4 where they are installed.
   Building sasquatch from its source needs the permission of a maintainer.
4. `FuzzWalk` in `walk/format/all` fuzzes every format with the walk of
   members. Two runs of 3 and 4 minutes found no panic and no input over 5
   seconds. CI runs only its seeds.
5. Under a heavy load, a gate has once in a while shown one difference that
   the next run did not show. binwalk can exit before its worker starts,
   and the clients retry, but not without limit.
6. A stream of lzma of unknown size makes the decoder of `compress/xz`
   allocate the whole dictionary that the header names, up to 512 MiB,
   where liblzma maps the memory and touches only what it uses.
7. `walk/format/vendor` holds a private copy of the parser of the JBOOT ARM
   header, which `walk/format/firmware` holds too.
8. delink tries the keys of the format DLK in the order of a HashMap of
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
