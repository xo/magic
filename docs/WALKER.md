# The binary walker

This is the design of the binary walker. The walker comes after the port
(D31), and the port is done. The maintainers answered the questions of the
design with D49 to D52, which the last section lists.

The walker does the work of binwalk. It reads a binary file, such as a
firmware image, finds the files that are embedded in it at any offset, and
extracts them. It can then walk each file that it extracts.

## What binwalk is

These facts come from the repository of binwalk, read on 2026-11-15. The
newest commit was `26713972e3`, of 2026-08-11. The newest release is v3.1.0.

1. binwalk 3 is a rewrite in Rust. Its license is MIT, the same as the
   license of the code of magic.
2. binwalk 2 is in Python, and does not use libmagic. Its `core/magic.py`
   says that it replaces libmagic. Its rule files use the format of libmagic,
   with words of its own such as `{invalid}`, `{size:...}` and `{jump:...}`.
3. binwalk 3 has no rule files. Each signature is Rust code: a list of magic
   bytes, and a parser that reads the bytes at a match. The parser accepts or
   rejects the match. If it accepts the match, it gives a description, a
   confidence and, when it can, the size of the embedded file.
4. binwalk 3 reads the whole file into memory before it scans.

The backlog said that two claims about binwalk were not confirmed. Items 1
and 2 answer them. The claim that binwalk 2 uses libmagic was wrong.

### How binwalk 3 scans

1. A "short" signature is a few bytes that match too often at random, such as
   the magic of zlib or of FAT. binwalk tries a short signature only at offset
   0, unless the option `--search-all` is set.
2. binwalk puts the magic bytes of every other signature into one
   Aho-Corasick matcher. An Aho-Corasick matcher finds many fixed patterns in
   one pass over the data. binwalk runs it over the file once.
3. At each match, binwalk runs the parser of that signature. The parser gives
   one of three confidences: low, medium or high.
4. If a match has medium or high confidence and a known size, binwalk starts
   the scan again at the end of that match. It does not look for signatures
   inside a file that it already identified.
5. After the scan, binwalk sorts the matches by offset. If two matches
   overlap, binwalk keeps the one with the higher confidence. If the
   confidences are the same, binwalk keeps the first one.
6. A match with no known size ends where the next match of medium or high
   confidence starts, or at the end of the file.

### What binwalk does with a match

| Option | What it does |
| --- | --- |
| none | prints the offset and the description of each match |
| `-e` | extracts each match that has an extractor |
| `-M` | extracts, then scans each extracted file, to any depth ("matryoshka") |
| `-c` | carves each match, and each gap between matches, to a file |
| `-a` | tries the short signatures at every offset |
| `-E` | draws a graph of the entropy of the file, in blocks |
| `-y`, `-x` | scans for only these signatures, or for all but these |
| `-l` | writes the results as JSON |
| `-t` | sets the number of threads, which work on separate files |

An extractor is one of two kinds. An internal extractor is Rust code in
binwalk. An external extractor runs a program of the system, such as `7zz`
or `sasquatch`. magic runs no outside program (D36). Each format that binwalk
extracts with a program needs Go code in magic, or magic only finds it.

### The formats

binwalk 3 has 111 signatures. Its own code extracts 39 of them, 18 outside
programs extract 33, and binwalk finds the other 39 and does not extract
them. In the tables, "binwalk" means that the code of binwalk extracts the
format, a program name means that program extracts it, and "none" means that
binwalk only finds it. A short signature has "(short)" after its name.

Compressed data:

| Signature | Extractor |
| --- | --- |
| `gzip`, `zlib` (short), `bzip2`, `xz` | binwalk |
| `lzma`, `compressd` (short) | `7zz` |
| `zstd`, `lz4`, `lzop`, `lzfse` | the program of the same name |
| `arcadyan` (LZMA with the bytes changed), `gpg_signed` (short, zlib inside) | binwalk |

Archives:

| Signature | Extractor |
| --- | --- |
| `zip`, `7zip`, `cpio`, `arj` | `7zz` |
| `tarball` | `tar` |
| `cab` | `cabextract` |
| `rar` | `unrar` |
| `dahua_zip` (a zip file with a changed header) | binwalk |
| `deb` | none |

File systems and disk images:

| Signature | Extractor |
| --- | --- |
| `squashfs` | `sasquatch`, which reads the variants of vendors |
| `cramfs`, `iso9660`, `apfs` | `7zz` |
| `jffs2` | `jefferson` |
| `yaffs` | `unyaffs` |
| `ubi`, `ubifs` | `ubireader_extract_files` |
| `ext`, `fat` (short), `ntfs` | `tsk_recover`, of The Sleuth Kit |
| `romfs` | binwalk |
| `qnx_ifs` | `dumpifs` |
| `dmg` | `dmg2img` |
| `btrfs`, `logfs`, `luks`, `qcow` (short) | none |
| `mbr` (short), `android_sparse` | binwalk |
| `efigpt` | `7zz` |

Kernels, boot loaders and the headers of firmware:

| Signature | Extractor |
| --- | --- |
| `uimage`, `trx`, `dtb`, `jboot_sch2`, `wince`, `vxworks_symtab`, `mh01`, `csman` (short), `matter_ota` (short), `autel`, `dms` | binwalk |
| `encfw` (short), `encrpted_img` (short), `dlink_tlv`, `dlke`, `shrs`, `dkbs`, `openssl` | binwalk, which decrypts them with keys that are known for each vendor |
| `linux_kernel` | `vmlinux-to-elf` |
| `uefi_pi_volume`, `uefi_capsule`, `pchrom` | `uefi-firmware-parser` |
| `srecord`, `srecord_generic` (short) | `srec_cat` |
| `uboot`, `linux_boot_image`, `linux_arm_zimage`, `linux_arm64_boot_image`, `android_bootimg`, `wind_kernel`, `ecos`, `cfe`, `seama`, `tplink`, `tplink_rtos`, `chk`, `dlob`, `packimg`, `binhdr`, `rtk` (short), `jboot_arm`, `jboot_stag` | none |

Images, documents and executables:

| Signature | Extractor |
| --- | --- |
| `png`, `jpeg`, `gif`, `bmp`, `riff`, `svg`, `dxbc` | binwalk, which carves the file at its size |
| `pdf`, `pjl`, `elf`, `pe` | none |

Keys, tables of constants, captures and text:

| Signature | Extractor |
| --- | --- |
| `pem_certificate`, `pem_public_key`, `pem_private_key`, `pcapng` | binwalk, which carves the file |
| `pkcs_der_hash`, `rsa`, `dpapi` (short), `aes_sbox`, `aes_forward_table`, `aes_reverse_table`, `aes_rcon`, `aes_acceleration_table`, `crc32`, `sha256`, `md5`, `copyright` | none |

The last group finds tables of constants of AES, CRC32, SHA-256 and MD5 in
code. A table tells the reader which algorithms a firmware image contains.

## The design

### Three steps

The walker keeps three steps apart, as the backlog says:

1. Find a candidate. One Aho-Corasick matcher finds the magic bytes of every
   format in one pass.
2. Parse the candidate. The parser of the format accepts or rejects it, and
   gives its size when it can.
3. Unpack the match. A compressed stream gives one member. An archive or a
   file system gives a tree of members. A caller can read each member, write
   it to a folder, or walk it again.

A program that only lists what an image holds runs steps 1 and 2. It never
decodes or writes anything.

### The API

The walker is a package, `github.com/xo/magic/walk`. Its main function
returns an iterator, as `docs/API.md` expects:

```go
package walk

// Walk finds the files that are embedded in r, in the order of their offsets.
// With WithDepth, it also walks the members of each match, depth first.
func Walk(ctx context.Context, r io.ReaderAt, size int64, opts ...Option) iter.Seq2[*Entry, error]

// WalkFile opens name and walks it.
func WalkFile(ctx context.Context, name string, opts ...Option) iter.Seq2[*Entry, error]

// Entry is one match.
type Entry struct {
	Format      string     // the name of the signature, as binwalk names it, such as "squashfs"
	Description string     // the text of the parser
	Confidence  Confidence // Low, Medium or High
	Offset      int64      // the offset of the match in its input
	Size        int64      // the bytes that the match covers in its input
	SizeKnown   bool       // false when the match ends at the next match or at the end
	Parent      *Entry     // the match whose member holds this one, or nil
	Member      string     // the name of that member in its parent, or ""
	Depth       int        // 0 for the input itself
}

// Section returns the bytes of the match in its input.
func (e *Entry) Section() *io.SectionReader

// Members returns the members of the match: one for a compressed stream,
// and a tree for an archive or a file system. It is empty for a format that
// the walker only finds.
func (e *Entry) Members(ctx context.Context) iter.Seq2[*Member, error]

// SkipMembers tells Walk not to walk the members of this match.
func (e *Entry) SkipMembers()

// Member is one file, folder or link inside a match.
type Member struct {
	Name string      // a slash-separated path, which Extract cleans
	Mode fs.FileMode // the type and the permissions
	Link string      // the target of a link
	Size int64       // -1 when it is not known before it is read
}

// Open reads the content of a regular member.
func (m *Member) Open() (io.ReadCloser, error)

// Extract writes the members of e under root, inside the limits of opts.
func Extract(ctx context.Context, root *os.Root, e *Entry, opts ...Option) error
```

A caller lists an image like this:

```go
for e, err := range walk.WalkFile(ctx, "firmware.bin", walk.WithDepth(8)) {
	if err != nil {
		return err
	}
	fmt.Printf("%*s%#x %s\n", 2*e.Depth, "", e.Offset, e.Description)
}
```

`Walk` gives a flat sequence of entries, depth first. Each entry points to its
parent. A caller that stops the loop stops the walk, and a caller that calls
`SkipMembers` skips one subtree, as `fs.SkipDir` does for `fs.WalkDir`.

A match with no known size ends at the next match, as in binwalk. The walker
holds back one entry until it finds the next match of medium or high
confidence, or the end of the input. It then sets the size and yields the
entry. This keeps the results in order without a second pass.

### The options

| Option | What it does | Option of binwalk |
| --- | --- | --- |
| `WithDepth(n)` | walks the members of each match to depth n. The default is 0 | `-M` |
| `WithFormats(names...)` | looks only for these formats | `-y` |
| `WithoutFormats(names...)` | looks for every format but these | `-x` |
| `WithSearchAll()` | tries the short signatures at every offset | `-a` |
| `WithDescribe()` | adds the `magic.Result` of the engine of the port to each entry | none |
| `WithLimits(l)` | sets the limits below | none |

The gaps between matches (`-c`) and the entropy graph (`-E`) are not options.
A caller finds the gaps from the offsets and sizes of the entries. Entropy is
a separate function, `walk.Entropy(ctx, r, size, block)`, which returns the
entropy of each block. The walker draws no graph.

### The formats are packages

The formats register themselves, as the decoders of D45 do. A program
imports the formats that it needs:

```go
import (
	_ "github.com/xo/magic/walk/format/compressed" // gzip, zlib, bzip2, xz, lzma, zstd
	_ "github.com/xo/magic/walk/format/image"      // png, jpeg, gif, bmp, riff, svg
	_ "github.com/xo/magic/walk/format/squashfs"
)
```

`walk/format/all` imports every format. A format that needs another module
is a module of its own, as `compress/xz` is (D45). A format implements a
small interface:

```go
// Format finds and parses one kind of embedded file.
type Format interface {
	Name() string
	Magic() []Magic // the fixed bytes, and their offset from the start of the file
	Short() bool    // true when the magic matches too often to try at every offset
	Parse(ctx context.Context, src *Source, off int64) (Match, error)
}

// Unpacker is a Format whose matches have members.
type Unpacker interface {
	Members(ctx context.Context, src *Source, m Match) iter.Seq2[*Member, error]
}

func Register(f Format)
```

`Source` wraps the `io.ReaderAt` with a cache of the window around the
offset. A parser reads the bytes that it needs from it and does not read the
whole file. A parser returns an error to reject a candidate.

### The engine of the port

The walker uses the engine of the port in two places:

1. With `WithDescribe`, it runs `magic.DetectReaderAt` on the section of each
   match. A caller gets the description and the MIME type of libmagic beside
   those of binwalk. The API of the port needs no change for this.
2. The decoders of `compress/` decode the compressed formats. A decoder of
   that registry takes a whole buffer and does not say how many bytes of
   input it read. The walker needs that number, because it is the size of the
   match. The registry needs a second kind of decoder that reads from an
   `io.Reader` and counts the bytes of input. This is a choice in the engine
   that makes the walker harder, which D31 asks to record.

The rules of libmagic do not serve as the signatures of the walker. A rule
names a whole file from its first bytes, and it does not give the size of the
file. The signatures of binwalk find a file inside another, and give its size.

### Reading and memory

binwalk reads the whole file into memory. The walker reads from an
`io.ReaderAt` in windows of 1 MiB. Each window overlaps the next by the length
of the longest magic, minus one byte, so that no match falls between two
windows. A firmware image of several gigabytes then needs only a few MiB.

The members of a match are streams. To walk a member, the walker needs an
`io.ReaderAt`. It keeps a member in memory up to a limit, and writes a larger
member to a temporary file. Without a temporary folder, it skips a member
over the limit and reports an error for it.

### Safety

A hostile image can make a walker use all of the memory, the disk or the
time of the system. The limits hold from the first release:

1. A limit on the bytes that one member and one walk give. A walk stops a
   member at the limit and reports an error.
2. A limit on the ratio of output to input of a decoder, which stops a
   compression bomb.
3. A limit on the depth of a walk. The default depth is 0, and a caller sets
   a larger one.
4. A limit on time. The walk stops when its context is done, between two
   candidates and inside each decoder.
5. `Extract` writes only through an `os.Root`. An `os.Root` refuses a path
   that leaves its folder, so a member named `../../etc/passwd` fails.
6. `Extract` writes no device file. It writes a link only when the target
   stays inside the root, and otherwise writes nothing for it.

### How it is tested

The port follows libmagic and compares itself with libmagic. The walker can
follow binwalk 3 in the same way:

1. Add binwalk to `docs/upstream/upstreams.tsv`, with a ledger of its commits
   (D25, D26). The walker gives the result of binwalk at the commit of the
   ledger.
2. Build binwalk at that commit in the test module, as the oracle of libmagic
   is built. Run it with `-l`, and compare its JSON with the entries of
   `Walk`: the offset, the name, the size and the confidence of each match.
3. Collect a corpus of firmware images whose licenses allow a copy in the
   repository, and damaged copies of them.
4. Fuzz each parser. A parser reads hostile bytes, and must not panic or read
   past its limits.

binwalk extracts many formats with outside programs, so the oracle compares
the matches, and not the extracted files, for those formats.

## The order of the work

The phases go from what magic already has to the large parsers. The weeks are
for one engineer who knows the port. They are guesses, and the formats of
phase W4 are the least sure.

| Phase | The work | Formats | Weeks |
| --- | --- | --- | --- |
| W1 | the scanner, the iterator, the registry, `Source`, the limits, the oracle of binwalk, and the formats that only need a parser | the images, the keys and tables, `elf`, `pe`, `pdf`, `uimage`, `trx`, `dtb`, `mbr`, `efigpt`, the headers of vendors | 5 to 8 |
| W2 | members, `Extract`, the walk of members, and the decoders that count their input | `gzip`, `zlib`, `bzip2`, `xz`, `lzma`, `zstd`, `zip`, `tarball`, `cpio`, `romfs`, `android_sparse` | 4 to 6 |
| W3 | SquashFS, with the variants of vendors that `sasquatch` reads | `squashfs` | 3 to 6 |
| W4 | the other file systems of firmware | `cramfs`, `jffs2`, `yaffs`, `ubi`, `ubifs`, `iso9660`, `fat`, `ext` | 10 to 20 |
| W5 | the formats that binwalk decrypts or changes, and the rest | `encfw` and the others with keys, `7zip`, `cab`, `lz4`, `lzop`, `uefi_*`, `srecord`, `linux_kernel` | 5 to 10 |

`rar`, `ntfs`, `apfs`, `dmg`, `lzfse` and `qnx_ifs` stay "find only" until a
maintainer asks for them. Each one is a large parser, or needs a decoder that
Go does not have.

## The answers

The maintainers answered the questions of this design on 2026-11-15. They
were questions 17 to 22 of `docs/PLAN.md`.

| Question | Answer | Decision |
| --- | --- | --- |
| 17. Does the walker follow binwalk 3, with a ledger and an oracle? | Yes. CI builds binwalk with Rust for the test module | D49 |
| 18. Is the walker a package of this module? | Yes, `github.com/xo/magic/walk` | D50 |
| 19. Does the walker take the names and descriptions of binwalk? | Yes, as they are, so that the oracle compares them as text | D49 |
| 20. Is the API an iterator with `SkipMembers`? | Yes, as this document gives it | D50 |
| 21. Does the walker carry the keys of binwalk? | Yes, in a format package of its own | D51 |
| 22. Which formats come first? | The phases W1 to W5, in the order above | D52 |
