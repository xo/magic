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
   of LZX with `compress/lzx`, as one stream over its blocks, as cabextract
   reads it.
   `test/corpus/testdata/walk/cab-4.bin` holds
   `compress/lzx/testdata/sample.cab`, with the checksums of its blocks set.
   A folder of Quantum still fails, because `compress/` has no decoder of
   Quantum.

## How the walker reads LZX as cabextract reads it

On 2026-11-20, tests with cabextract 1.11 on cabinets made for them showed
how it reads LZX. The walker now reads it in the same way:

1. The data of the blocks of a folder is one stream of frames of 32768
   bytes. A frame can start in one block and end in another.
2. A frame ends at a boundary of a 16-bit word. After an uncompressed block
   of an odd length that ends with the frame, the next frame starts after
   the byte of padding. A frame that ends inside an uncompressed block goes
   on at the next byte.
3. A frame whose last match passes its end fails.
4. Only the sum of the uncompressed sizes of the blocks counts. A block
   fails when its uncompressed size is 0 or over 32768, or when it holds
   more than 38912 bytes of data.
5. cabextract reads 4096 bytes at a time across the ends of blocks, and
   fails on any bad block that it reads, even one that no frame needs.
   After a frame, it has read 2 bytes ahead after compressed bits, and none
   after uncompressed data.
6. After the last block, it reads 2 bytes of zeros, and fails if it needs
   more.
7. It knows the size of the last frame only when it has read the last
   block. Until then, every frame is 32768 bytes.

Three methods of `compress/lzx` give what the walker needs: `Used`, the
bytes of input that the last frame read; `Overrun`, the bytes that its last
match wrote past its end; and `Uncompressed`, whether it ended in an
uncompressed block. On damaged copies of `sample.cab`, of `cab-4.bin` with
its checksums cleared, and of 233 random cabinets with LZX, the walker and
cabextract differed 37, 37 and 741 times before, and 0 times after.

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
