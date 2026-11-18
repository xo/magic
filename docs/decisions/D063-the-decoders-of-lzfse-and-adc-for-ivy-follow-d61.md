# D63. The decoders of LZFSE and ADC for ivy follow D61

Status: Decided.

On 2026-11-18, a maintainer asked for two more decoders for ivy. ivy D8
lists them, and ivy reads DMG files with them:

1. LZFSE, the compression of Apple. A DMG file uses it in the chunks of type
   0x80000007, and other formats of Apple use it too.
2. ADC, the Apple Data Compression. Older DMG files use it in the chunks of
   type 0x80000004.

## The decision

1. The decoders follow D61. Each one is a package of its own in the root
   module: `compress/lzfse` and `compress/adc`. Each one uses only the
   standard library, so `go.mod` keeps no `require` line (D14). Neither
   registers with `compress`, because libmagic decodes neither format.
   magic imports neither, so a program that does not import them does not
   pay for them (D38).
2. Each package has the API of `compress/palmdoc`:
   - `lzfse.Decode(dst, src []byte, limit int) ([]byte, error)` decodes a
     whole stream of LZFSE, up to its block of the end. It ignores the bytes
     after that block.
   - `adc.Decode(dst, src []byte, limit int) ([]byte, error)` decodes one
     chunk of ADC. A chunk has no end of its own, so it ends with `src`.

   A match can reach back to the start of the stream or of the chunk, never
   into the bytes of `dst` before it.
3. Hostile data must not crash a decoder or make it hang. Each decoder stops
   at the limit, and returns the first `limit` bytes and an error that wraps
   `ErrLimit`. On damaged data, it returns the bytes that it decoded before
   the fault and an error that wraps `ErrCorrupt`. It checks every match and
   every read of the input. An error starts with the name of its package, as
   D61 says. Each package has the fuzz test `FuzzDecode`.
4. The decoders do not read a DMG file. A program reads the table of blocks
   of the DMG file, and calls the decoder for each chunk.

## The sources

1. `compress/lzfse` is a port of the decoder of lzfse, the reference code of
   Apple, at commit `e634ca58` of github.com/lzfse/lzfse. lzfse is under the
   BSD 3-clause license. `compress/lzfse/LICENSE` holds it, and each Go file
   of the port opens with the copyright notice of Apple. lzfse is the fourth
   upstream in `docs/upstream/upstreams.tsv`, with its ledger in
   `docs/upstream/lzfse.tsv` and its checkout in `upstream/lzfse/`. Its last
   commit is from 2017-05-22.
2. The port differs from lzfse in four places. The comment at the top of
   `compress/lzfse/lzfse.go` lists them. One of them fixes a fault of lzfse:
   the opcodes of LZVN that take the distance of the match before them do
   not check it, so the first such opcode of a block copies bytes that the
   decoder did not write.
3. `compress/adc` is written from knowledge of the format, which has no
   public specification. The known decoders of ADC, in 7-Zip, dmg2img and
   libdmg-hfsplus, are under the LGPL or the GPL, so no code of them was
   read or copied.

## How the tests check the decoders

1. The lzfse program of the reference code, built from commit `e634ca58`,
   wrote the files of `compress/lzfse/testdata`. They hold each kind of
   block that the encoder writes: bytes that are not compressed, LZVN, and
   LZFSE of version 2, with matches from more than 65536 bytes back.
   `go test -update -lzfse <program> ./compress/lzfse` writes them again.
   The tests also write each block of version 2 with the header of version
   1, which the encoder does not write, and decode it.
2. On 2026-11-18, a program outside the repository compared the decoder with
   the lzfse program on 600 inputs that the program compressed, and on 4500
   damaged copies of them. The two gave the same bytes for each input that
   both decoded, and failed on the same inputs. The check ran once, by hand,
   because CI runs no outside program (D58).
3. A small compressor in the tests of `compress/adc` makes the data, with
   each of the three codes of ADC. `compress/adc/testdata/sample.dmg` is a
   DMG file that the tests wrote, with chunks of ADC, a raw chunk and a chunk
   of zeros. On 2026-11-18, 7z 26.06, with the option `-tdmg`, and dmg2img
   1.6.5 extracted the same disk from it as the tests. The check ran once,
   by hand. `go test -update ./compress/adc` writes the file again.
4. On 2026-11-18, each fuzz test ran for one minute and found no fault.
