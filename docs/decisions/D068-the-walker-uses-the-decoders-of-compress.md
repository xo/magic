# D68. The walker uses the decoders of compress

Status: Decided. Amends D60.

A maintainer decided on 2026-11-20 that the walker uses the decoders of
`compress/` that exist. The walker writes no new decoder.

D60 said that the walker writes no new decoder. So the walker found lzfse
data, and did not extract it. After D60, D61 and D63 added three decoders
for ivy: `compress/lzx`, `compress/lzfse` and `compress/adc`. binwalk-ng
needs these formats too.

## The decision

1. When binwalk-ng extracts a format, and a decoder of `compress/` decodes
   its data, the walker uses that decoder. The walker writes the files of
   binwalk-ng, as D49 and D60 say.
2. The walker writes no new decoder. The rule of D60 stays for each format
   that needs a decoder that `compress/` does not have, such as rar and QNX
   IFS.
3. The decoders of D61 and D63 use only the standard library, so `go.mod`
   keeps no `require` line (D14). They stay out of the registry of D45.

## What the walker extracts now

1. `lzfse`. The walker decodes the data with `compress/lzfse`, as
   `lzfse_decompress` of binwalk-ng decodes it with the crate lzfse. The
   crate calls the reference code of Apple, which `compress/lzfse` ports.
   binwalk-ng adds the sizes that the headers of the blocks give, and decodes
   into a buffer one byte larger than that sum. The data is valid when it
   decodes to exactly that sum. The extraction gate compares lzfse with
   binwalk-ng. `test/corpus/testdata/walk/lzfse-3.bin` holds two blocks of
   version 2, from `compress/lzfse/testdata/text.lzfse`.
2. `cab` with LZX. binwalk-ng runs cabextract. The walker decodes a folder
   of LZX with `compress/lzx`, and decodes each CFDATA block as one frame.
   `test/corpus/testdata/walk/cab-4.bin` holds
   `compress/lzx/testdata/sample.cab`, with the checksums of its blocks set.
   A folder of Quantum still fails, because `compress/` has no decoder of
   Quantum.

## Where the walker and cabextract differ on LZX

On 2026-11-20, tests with cabextract 1.11 showed that it reads the LZX data
of a folder as one stream over all its blocks, in frames of 32768 bytes. A
frame can start in one block and end in the next. Thus the walker writes the
files of cabextract only when each block but the last holds one whole frame
of 32768 bytes and nothing more. The walker fails on a block other than the last that holds fewer than 32768 bytes. It
cannot find a block that holds more bytes than its frame, because
`compress/lzx` does not tell how many bytes of input a frame reads. For such
a block, the walker writes other bytes, or fails, where cabextract decodes the
bytes of the next frame. On 2000 damaged copies of `sample.cab` with no
checksums, the files differed 64 times. The checksums of `cab-4.bin` make
cabextract and the walker fail on each damaged block, so the gate shows no
difference. A method of `compress/lzx` that gives the bytes that the last
frame read lets the walker follow the stream.

## Why the walker does not extract dmg

binwalk-ng runs dmg2img, and accepts its exit codes 0 and 1. The walker
cannot write the same bytes as dmg2img with the decoders that it has:

1. dmg2img writes what it decodes from a damaged chunk, and exits with code
   0. On 10 copies of `compress/adc/testdata/sample.dmg` with one byte
   changed in the ADC data, dmg2img 1.6.7 wrote a shorter disk 3 times, and
   crashed 2 times. `compress/adc` stops at the fault. To write the same
   bytes, the walker must copy the faults of the ADC decoder of dmg2img. That
   decoder is under the GPL, and D63 does not read it.
2. dmg2img stops with exit code 1 when it reads past the end of the file,
   and keeps the part of the disk that it wrote. binwalk-ng keeps that part.
3. dmg2img decodes LZFSE only when it is built with the library lzfse. The
   dmg2img of Arch Linux has no lzfse. It writes a warning that the data
   will be corrupted, and continues. So the file of binwalk-ng depends on
   how dmg2img was built.
4. With one byte of the property list changed, dmg2img wrote a disk of
   2 MiB in place of 200 KiB, with exit code 0.

The walker still finds a DMG and does not extract it. `compress/adc` and
`compress/lzfse` decode the chunks of a DMG for a program that reads the
table of blocks itself (D63).
