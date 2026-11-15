//go:build amd64 || arm64 || loong64 || mips64 || mips64le || ppc64 || ppc64le || riscv64 || s390x || sparc64 || wasm

// Copyright (c) 2024 devttys0. Under the MIT license, in LICENSE.
//
// This file translates src/signatures/elf.rs and src/structures/elf.rs of
// binwalk.

package exec

import (
	"fmt"

	"github.com/xo/magic/walk"
	"github.com/xo/magic/walk/internal/binwalk"
)

// ELF is the signature "elf" of binwalk.
var ELF = &walk.Signature{
	Name:        "elf",
	Magic:       [][]byte{[]byte("\x7FELF")},
	Description: "ELF binary",
	Parse:       parseELF,
}

// parseELF is elf_parser.
func parseELF(data []byte, off int) (walk.Match, error) {
	h, ok := parseELFHeader(data[off:])
	if !ok {
		return walk.Match{}, walk.ErrNoMatch
	}
	return walk.Match{
		Offset:     off,
		Confidence: walk.Medium,
		Description: fmt.Sprintf("ELF binary, %s-bit %s, %s for %s, %s endian",
			h.class, h.exeType, h.machine, h.osabi, h.endianness),
	}, nil
}

// elfHeader is ELFHeader of structures/elf.rs.
type elfHeader struct {
	class      string
	osabi      string
	machine    string
	exeType    string
	endianness string
}

var (
	elfIdent = binwalk.Struct{
		{Name: "magic", Type: "u32"},
		{Name: "class", Type: "u8"},
		{Name: "endianness", Type: "u8"},
		{Name: "version", Type: "u8"},
		{Name: "osabi", Type: "u8"},
		{Name: "abiversion", Type: "u8"},
		{Name: "padding_1", Type: "u32"},
		{Name: "padding_2", Type: "u24"},
	}
	elfInfo = binwalk.Struct{
		{Name: "type", Type: "u16"},
		{Name: "machine", Type: "u16"},
		{Name: "version", Type: "u32"},
	}
)

var elfClasses = map[int]string{1: "32", 2: "64"}

var elfEndianness = map[int]string{1: "little", 2: "big"}

var elfOSABI = map[int]string{
	0:   "System-V (Unix)",
	1:   "HP-UX",
	2:   "NetBSD",
	3:   "Linux",
	4:   "GNU Hurd",
	5:   "86Open",
	6:   "Solaris",
	7:   "AIX",
	8:   "IRIX",
	9:   "FreeBSD",
	10:  "Tru64",
	11:  "Novell Modesto",
	12:  "OpenBSD",
	13:  "OpenVMS",
	14:  "NonStop Kernel",
	15:  "AROS",
	16:  "FenixOS",
	17:  "Nuxi CloudABI",
	18:  "OpenVOS",
	97:  "ARM ABI",
	102: "Cell LV2",
	202: "Cafe OS",
	255: "embedded",
}

var elfTypes = map[int]string{
	0: "no file type",
	1: "relocatable",
	2: "executable",
	3: "shared object",
	4: "core file",
}

var elfMachines = map[int]string{
	0:   "no machine",
	1:   "AT&T WE 32100",
	2:   "SPARC",
	3:   "x86",
	4:   "Motorola 68k",
	5:   "Motorola 88k",
	6:   "Intel MCU",
	7:   "Intel 80860",
	8:   "MIPS",
	9:   "IBM System/370",
	10:  "MIPS RS3000",
	11:  "RS6000",
	15:  "HP PA-RISC",
	16:  "nCUBE",
	17:  "Fujitsu VPP500",
	18:  "SPARC32PLUS",
	19:  "Intel 80960",
	20:  "PowerPC",
	21:  "PowerPC 64-bit",
	22:  "S390",
	23:  "IBM SPU/SPC",
	24:  "cisco SVIP",
	25:  "cisco 7200",
	36:  "NEC V800",
	37:  "Fujitsu FR20",
	38:  "TRW RH-32",
	39:  "Motorola RCE",
	40:  "ARM",
	41:  "Digital Alpha",
	42:  "SuperH",
	43:  "SPARCv9",
	44:  "Siemens TriCore embedded processor",
	45:  "Argonaut RISC Core",
	46:  "Hitachi H8/300",
	47:  "Hitachi H8/300H",
	48:  "Hitachi H8S",
	49:  "Hitachi H8/500",
	50:  "IA-64",
	51:  "Stanford MIPS-X",
	52:  "Motorola ColdFire",
	53:  "Motorola M68HC12",
	54:  "Fujitsu MMA Multimedia Accelerator",
	55:  "Siemens PCP",
	56:  "Sony nCPU embedded RISC processor",
	57:  "Denso NDR1 microprocessor",
	58:  "Motorola StarCore",
	59:  "Toyota ME16",
	60:  "STMicroelectronics ST100",
	61:  "Advanced Logic TinyJ embedded processor",
	62:  "AMD X86-64",
	63:  "Sony DSP processor",
	64:  "PDP-10",
	65:  "PDP-11",
	66:  "Siemens FX66",
	67:  "STMicroelectronics ST9+",
	68:  "STMicroelectronics ST7",
	69:  "Motorola MC68HC16",
	70:  "Motorola MC68HC11",
	71:  "Motorola MC68HC08",
	72:  "Motorola MC68HC05",
	73:  "Silicon Graphics SVx",
	74:  "STMicroelectonrics ST19",
	75:  "Digital VAX",
	76:  "Axis Communications 32-bit CPU",
	77:  "Infineon Technologies 32-bit CPU",
	78:  "Element 14 64-bit DSP",
	79:  "LSI Logic 16-bit DSP",
	80:  "MMIX",
	81:  "Harvard machine-independent",
	82:  "SiTera Prism",
	83:  "Atmel AVR 8-bit",
	84:  "Fujitsu FR30",
	85:  "Mitsubishi D10V",
	86:  "Mitsubishi D30V",
	87:  "NEC v850",
	88:  "Renesas M32R",
	89:  "Matsushita MN10300",
	90:  "Matsushita MN10200",
	91:  "picoJava",
	92:  "OpenRISC",
	93:  "Synopsys ARCompact ARC700 cores",
	94:  "Tensilica Xtensa",
	95:  "Alphamosaic VideoCore",
	96:  "Thompson Multimedia",
	97:  "NatSemi 32k",
	98:  "Tenor Network TPC",
	99:  "Trebia SNP 1000",
	100: "STMicroelectronics ST200",
	101: "Ubicom IP2022",
	102: "MAX Processor",
	103: "NatSemi CompactRISC",
	104: "Fujitsu F2MC16",
	105: "TI msp430",
	106: "Analog Devices Blackfin",
	107: "S1C33 Family of Seiko Epson",
	108: "Sharp embedded",
	109: "Arca RISC",
	110: "PKU-Unity Ltd.",
	111: "eXcess: 16/32/64-bit",
	112: "Icera Deep Execution Processor",
	113: "Altera Nios II",
	114: "NatSemi CRX",
	115: "Motorola XGATE",
	116: "Infineon C16x/XC16x",
	117: "Renesas M16C series",
	118: "Microchip dsPIC30F",
	119: "Freescale RISC core",
	120: "Renesas M32C series",
	131: "Altium TSK3000 core",
	132: "Freescale RS08",
	134: "Cyan Technology eCOG2",
	135: "Sunplus S+core7 RISC",
	136: "New Japan Radio (NJR) 24-bit DSP",
	137: "Broadcom VideoCore III processor",
	138: "LatticeMico32",
	139: "Seiko Epson C17 family",
	140: "TMS320C6000",
	141: "TMS320C2000",
	142: "TMS320C55x",
	144: "TI Programmable Realtime Unit",
	160: "STMicroelectronics 64bit VLIW DSP",
	161: "Cypress M8C",
	162: "Renesas R32C series",
	163: "NXP TriMedia family",
	164: "Qualcomm DSP6",
	165: "Intel 8051 and variants",
	166: "STMicroelectronics STxP7x family",
	167: "Andes embedded RISC",
	168: "Cyan eCOG1X family",
	169: "Dallas MAXQ30",
	170: "New Japan Radio (NJR) 16-bit DSP",
	171: "M2000 Reconfigurable RISC",
	172: "Cray NV2 vector architecture",
	173: "Renesas RX family",
	174: "META",
	175: "MCST Elbrus e2k",
	176: "Cyan Technology eCOG16 family",
	177: "NatSemi CompactRISC",
	178: "Freescale Extended Time Processing Unit",
	179: "Infineon SLE9X",
	180: "Intel L1OM",
	181: "Intel K1OM",
	183: "ARM 64-bit",
	185: "Atmel 32-bit family",
	186: "STMicroeletronics STM8 8-bit",
	187: "Tilera TILE64",
	188: "Tilera TILEPro",
	189: "Xilinx MicroBlaze 32-bit RISC",
	190: "NVIDIA CUDA architecture",
	191: "Tilera TILE-Gx",
	195: "Synopsys ARCv2/HS3x/HS4x cores",
	197: "Renesas RL78 family",
	199: "Renesas 78K0R",
	200: "Freescale 56800EX",
	201: "Beyond BA1",
	202: "Beyond BA2",
	203: "XMOS xCORE",
	204: "Microchip 8-bit PIC(r)",
	210: "KM211 KM32",
	211: "KM211 KMX32",
	212: "KM211 KMX16",
	213: "KM211 KMX8",
	214: "KM211 KVARC",
	215: "Paneve CDP",
	216: "Cognitive Smart Memory",
	217: "iCelero CoolEngine",
	218: "Nanoradio Optimized RISC",
	219: "CSR Kalimba architecture family",
	220: "Zilog Z80",
	221: "Controls and Data Services VISIUMcore processor",
	222: "FTDI Chip FT32 high performance 32-bit RISC architecture",
	223: "Moxie processor family",
	224: "AMD GPU architecture",
	243: "RISC-V",
	244: "Lanai 32-bit processor",
	245: "CEVA Processor Architecture Family",
	246: "CEVA X2 Processor Family",
	247: "Berkeley Packet Filter",
	248: "Graphcore Intelligent Processing Unit",
	249: "Imagination Technologies",
	250: "Netronome Flow Processor",
	251: "NEC Vector Engine",
	252: "C-SKY processor family",
	253: "Synopsys ARCv3 64-bit ISA/HS6x cores",
	254: "MOS Technology MCS 6502 processor",
	255: "Synopsys ARCv3 32-bit",
	256: "Kalray VLIW core of the MPPA family",
	257: "WDC 65C816",
	258: "LoongArch",
	259: "ChipON KungFu32",
}

// parseELFHeader is parse_elf_header.
func parseELFHeader(data []byte) (elfHeader, bool) {
	const (
		identSize       = 16
		infoSize        = 8
		expectedVersion = 1
	)
	// The order of the bytes does not matter for the ident.
	id, ok := elfIdent.Parse(data, false)
	if !ok || id["padding_1"] != 0 || id["padding_2"] != 0 || id["version"] != expectedVersion {
		return elfHeader{}, false
	}
	class, ok1 := elfClasses[id["class"]]
	osabi, ok2 := elfOSABI[id["osabi"]]
	endian, ok3 := elfEndianness[id["endianness"]]
	if !ok1 || !ok2 || !ok3 {
		return elfHeader{}, false
	}
	raw, ok := binwalk.Range(data, identSize, identSize+infoSize)
	if !ok {
		return elfHeader{}, false
	}
	info, ok := elfInfo.Parse(raw, endian == "big")
	if !ok || info["version"] != expectedVersion {
		return elfHeader{}, false
	}
	typ, ok := elfTypes[info["type"]]
	if !ok {
		return elfHeader{}, false
	}
	machine, ok := elfMachines[info["machine"]]
	if !ok {
		machine = "Unknown"
	}
	return elfHeader{class: class, osabi: osabi, machine: machine, exeType: typ, endianness: endian}, true
}
