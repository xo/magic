package walk

import (
	"errors"
	"slices"
	"strconv"
	"sync"
)

// Confidence is how sure a parser is of a match. binwalk has three levels.
type Confidence uint8

// The levels of confidence of binwalk.
const (
	Low    Confidence = 0
	Medium Confidence = 128
	High   Confidence = 250
)

// String returns the name of the level, or its number.
func (c Confidence) String() string {
	switch c {
	case Low:
		return "low"
	case Medium:
		return "medium"
	case High:
		return "high"
	}
	return "confidence(" + strconv.Itoa(int(c)) + ")"
}

// ErrNoMatch is the error that a parser returns to reject a candidate.
var ErrNoMatch = errors.New("walk: no match")

// Match is what a parser finds at a candidate.
type Match struct {
	Offset      int        // the start of the match, which can be before the magic
	Size        int        // the bytes of the match, or 0 when the parser cannot tell
	Confidence  Confidence // the level of the match
	Description string     // the text of the parser
	Declined    bool       // true when the match needs no extraction, as a whole file

	// Extract, when it is set, extracts this match in place of the
	// extractor of the signature, as the preferred extractor of binwalk.
	Extract Extractor
	// Carve is set when Extract stands for a program of binwalk-ng, as
	// Signature.Carve. It counts only with Extract, which then replaces
	// Signature.Carve too.
	Carve bool
}

// Extractor writes the files of the match at off in data to out, as an
// internal extractor of binwalk. With a nil out, it writes nothing, and only
// checks the data and finds its size, as a dry run of binwalk.
type Extractor func(data []byte, off int, out *Output) Extraction

// Extraction is what an extractor reports.
type Extraction struct {
	OK      bool // the data is valid, and every file was written
	Size    int  // the bytes of data that the extractor read, when HasSize is set
	HasSize bool
}

// Signature is one format that the walker finds, as a signature of binwalk.
type Signature struct {
	Name        string   // the name of binwalk, such as "png"
	Short       bool     // true when the magic matches too often to try past offset 0
	Magic       [][]byte // the fixed bytes that start a candidate
	MagicOffset int      // the offset of the magic of a short signature
	Description string   // the text of the format, for a list of formats

	// Parse accepts or rejects the candidate whose magic is at off in data.
	Parse func(data []byte, off int) (Match, error)

	// Extract writes the files of a match, or is nil when binwalk does not
	// extract the format, or extracts it with a program of the system that
	// the walker has no translation of.
	Extract Extractor

	// NoRecurse is set when the walk of members does not walk the files
	// of this format, as do_not_recurse of binwalk.
	NoRecurse bool

	// ZeroMagicNonZero is for a magic of only zero bytes: the distance
	// before the magic of a byte that the parser requires to be not zero.
	// It lets the scan skip a run of zeros, as binwalk-ng does for
	// program_store. A magic of only zeros with no such distance stops the
	// skip.
	ZeroMagicNonZero int

	// Carve is set when binwalk extracts the format with a program, to
	// which it gives only the bytes of the match. The extractor then gets
	// the data up to the end of the match, as binwalk carves it, and not
	// the rest of the input. When that fails, the extractor runs again on
	// all the data to the end of the input, as extract of binwalk-ng does,
	// because the size of a match can be too short.
	Carve bool
}

var registry struct {
	sync.Mutex
	sigs []*Signature
}

// Register adds the signatures of a format to the walker. A package of
// formats calls it from init, so that a blank import adds its formats. A
// signature that has the name of one that is already registered panics.
func Register(sigs ...*Signature) {
	registry.Lock()
	defer registry.Unlock()
	for _, s := range sigs {
		if s.Name == "" || s.Parse == nil || len(s.Magic) == 0 {
			panic("walk: Register of an incomplete signature")
		}
		for _, t := range registry.sigs {
			if t.Name == s.Name {
				panic("walk: Register called twice for " + s.Name)
			}
		}
		registry.sigs = append(registry.sigs, s)
	}
}

// Signatures returns the registered signatures, in the order of binwalk.
func Signatures() []*Signature {
	registry.Lock()
	defer registry.Unlock()
	out := slices.Clone(registry.sigs)
	slices.SortStableFunc(out, func(a, b *Signature) int {
		return rank(a.Name) - rank(b.Name)
	})
	return out
}

// rank is the place of a signature in the order of binwalk-ng, which decides
// the order of the patterns, and so the order in which the scan tries the
// candidates at one offset. A signature that binwalk does not have comes
// after all of them.
func rank(name string) int {
	if i := slices.Index(order, name); i >= 0 {
		return i
	}
	return len(order)
}

// order is the order of the signatures in src/magic.rs of binwalk-ng.
var order = []string{
	"gzip", "deb", "7zip", "xz", "tarball", "squashfs", "dlob", "lzma", "eva",
	"bmp", "bzip2", "uimage", "minifs", "packimg", "crc32", "sha256", "cpio", "iso9660",
	"linux_kernel", "linux_boot_image", "linux_arm_zimage", "zstd", "zip",
	"pchrom", "uefi_pi_volume", "uefi_capsule", "pdf", "elf", "cramfs",
	"qnx_ifs", "romfs", "ext", "cab", "jffs2", "yaffs", "lz4", "lzop", "pe",
	"zlib", "gpg_signed", "pem_certificate", "pem_public_key",
	"pem_private_key", "chk", "trx", "srecord", "srecord_generic",
	"android_sparse", "dtb", "ubi", "ubifs", "cfe", "seama", "compressd", "rar",
	"png", "jpeg", "arcadyan", "copyright", "wind_kernel", "vxworks_symtab",
	"ecos", "dmg", "riff", "openssl", "lzfse", "mbr", "tplink", "pjl",
	"jboot_arm", "jboot_stag", "jboot_sch2", "pcapng", "rsa", "gif", "svg",
	"linux_arm64_boot_image", "fat", "efigpt", "rtk", "aes_sbox",
	"aes_forward_table", "aes_reverse_table", "aes_rcon",
	"aes_acceleration_table", "luks", "tplink_rtos", "binhdr", "autel", "ntfs",
	"apfs", "btrfs", "wince", "dahua_zip", "mh01", "csman", "dxbc", "dlink_tlv",
	"dlink_fw", "dlke", "shrs", "pkcs_der_hash", "logfs", "encrpted_img",
	"android_bootimg", "uboot", "dms", "dkbs", "encfw", "matter_ota", "dpapi",
	"qcow", "arj", "md5", "program_store",
}
