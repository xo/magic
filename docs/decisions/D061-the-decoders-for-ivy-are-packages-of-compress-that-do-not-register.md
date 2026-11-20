# D61. The decoders for ivy are packages of compress that do not register

Status: Decided.

On 2026-11-16, ivy needed three decoders to read MOBI books and CHM help
files:

1. PalmDOC, the compression of the text records of PalmDOC and MOBI books.
2. HUFF/CDIC, the Huffman compression of Mobipocket. It is compression type
   17480 of a MOBI book.
3. LZX as CHM uses it. A CHM file resets the decoder at a fixed interval of
   frames, and a reset table gives the offset of each frame.

The maintainers decided in ivy D21 that magic holds these decoders, and that
each change waits in magic for the review of a maintainer. ivy D8 says that
ivy takes its decoders of compressed data from magic. This decision says
where the decoders go and what they look like. The maintainers accepted it
on 2026-11-17, as the answer to question 24.

## The decision

1. Each decoder is a package of its own in the root module:
   `compress/palmdoc`, `compress/huffcdic` and `compress/lzx`. Each one uses
   only the standard library, so `go.mod` keeps no `require` line (D14).
2. No decoder registers with `compress`. The registry of D45 holds one
   library of libmagic for each format, and libmagic decodes none of these
   three formats. magic imports none of them, so a program that does not
   import them does not pay for them (D38).
3. Each package has a small API:
   - `palmdoc.Decode(dst, src []byte, limit int) ([]byte, error)` decodes one
     text record.
   - `huffcdic.New(huff []byte, cdics [][]byte) (*Decoder, error)` reads the
     HUFF record and the CDIC records of a book.
     `(*Decoder).Decode(dst, src []byte, limit int) ([]byte, error)` decodes
     one text record.
   - `lzx.NewDecoder(windowSize int) (*Decoder, error)` makes a decoder of
     one LZX stream. `(*Decoder).Decode(dst, src []byte, n int)` decodes the
     next frame, and `(*Decoder).Reset()` starts the stream again.
     `lzx.Section` holds the fields of the content of a CHM file, and
     `(*Section).Decode(dst []byte, off int64, n int)` decodes any part of
     it, from the reset before that part. `(*Decoder).Used`,
     `(*Decoder).Overrun` and `(*Decoder).Uncompressed` describe the last
     frame: the bytes of input that it read, the bytes that its last match
     wrote past its end, and whether it ended in an uncompressed block. A
     caller then reads a stream of frames that spans blocks, as the walker
     reads a folder of a CAB file (D68).
4. Hostile data must not crash a decoder or make it hang. Each decoder stops
   at a limit of output, or decodes at most one frame of 32768 bytes. Each
   one checks every copy from earlier output and every index of a table. On
   damaged data, it returns an error that wraps the `ErrCorrupt` of its
   package. Each package has a fuzz test.
5. An error starts with the name of its package, such as `lzx: `, as the
   errors of the Go standard library do. The rule that an error starts with
   `magic: ` holds for the root package.

## The sources

1. PalmDOC follows the PalmDOC page of the MobileRead wiki.
2. HUFF/CDIC is written from knowledge of the format. The MOBI page of the
   MobileRead wiki names the format and the fields of the MOBI header, and
   points to the code of calibre for the algorithm. calibre is under the GPL,
   so no code of calibre was read or copied.
3. LZX is a port of the crate lzxd, which is under the MIT license or the
   Apache license 2.0, and which follows the specification of LZX DELTA,
   [MS-PATCH]. lzxd decodes the LZX of CAB too, because it leaves out the
   long matches of LZX DELTA. `compress/lzx/LICENSE` holds the MIT license
   of lzxd. lzxd is the third upstream in `docs/upstream/upstreams.tsv`, with
   its ledger in `docs/upstream/lzxd.tsv` and its checkout in
   `upstream/lzxd/`. libmspack, chmlib and the LZX of 7-Zip are under the
   LGPL, so no code of them was read or copied.

The port differs from lzxd in five places. The comment at the top of
`compress/lzx/lzx.go` lists them. One of them fixes a fault of lzxd: E8
translation changes a value of 0 to the negative of the position, and lzxd
adds the translation size to it.

## How the tests check the decoders

1. A small compressor in the tests of each package makes the data, and the
   tests decode it again.
2. No tool on the machine wrote CAB or CHM files with LZX on 2026-11-16. So
   the compressor of the tests of `compress/lzx` wrote
   `compress/lzx/testdata/sample.cab` and `compress/lzx/testdata/sample.chm`.
   cabextract and 7z extract the same file from the cabinet, and 7z extracts
   it from the CHM file, which has resets and E8 translation. The check ran
   once, by hand, because CI runs no outside program (D58).
   `go test -update ./compress/lzx` writes the two files again.
3. The test of E8 translation decodes the frame of the tests of lzxd.

7z showed two facts about CHM:

1. E8 translation counts the position from the last reset, as lzxd does.
2. 7z reads a CHM file only when the reset table has an entry for the end of
   the data, the reset interval is a power of two, and the content fills its
   last frame. `lzx.Section` reads such files, and the other forms too.
