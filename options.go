package magic

import (
	"errors"
	"fmt"

	"github.com/xo/magic/internal/engine"
)

// Option changes how a call detects.
type Option func(*options) error

// options are the options of one call.
type options struct {
	flags engine.Flags
	all   bool
	bytes int
	// extra applies the rules of WithExtraRules, or is nil. A func, not a
	// bool, so that a program that does not call WithExtraRules does not
	// link the rules (D38).
	extra func(r *Result, head func() ([]byte, error)) error
	// kindOnly is set for a file that is not regular, which libmagic names
	// by its kind.
	kindOnly bool
}

// limitDefault is how many bytes libmagic reads and examines, 7 MiB.
const limitDefault = 7 * 1024 * 1024

func (o *options) limit() int {
	if o.bytes == 0 {
		return limitDefault
	}
	return o.bytes
}

// headSize returns how many bytes a rule of WithExtraRules reads at most.
func (o *options) headSize() int {
	return min(o.limit(), headMax)
}

// apply returns the options of a call.
func apply(opts []Option) (*options, error) {
	o := &options{}
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(o); err != nil {
			return nil, err
		}
	}
	return o, nil
}

// WithAll keeps every match, as MAGIC_CONTINUE does. Description then holds
// each match on a line of its own, and Matches holds one Match for each.
func WithAll() Option {
	return func(o *options) error {
		o.all = true
		return nil
	}
}

// WithDecompress identifies what is inside compressed data, as
// MAGIC_COMPRESS does. It decodes the formats whose decoder a program
// imports from github.com/xo/magic/compress (D45). magic runs no outside
// program (D36).
func WithDecompress() Option {
	return func(o *options) error {
		o.flags |= engine.Compress
		return nil
	}
}

// errLimit is the error of a limit that is not positive.
var errLimit = errors.New("magic: the limit must be positive")

// WithLimit sets how many bytes are read and examined. The default is 7 MiB,
// as in libmagic.
func WithLimit(n int) Option {
	return func(o *options) error {
		if n <= 0 {
			return fmt.Errorf("%w, not %d", errLimit, n)
		}
		o.bytes = n
		return nil
	}
}

// WithRaw keeps characters that cannot be printed, as MAGIC_RAW does. By
// default they are written as octal escapes, as file writes them.
func WithRaw() Option {
	return func(o *options) error {
		o.flags |= engine.Raw
		return nil
	}
}

// WithFollowSymlinks makes DetectFile identify the target of a link, as
// MAGIC_SYMLINK does. By default a link is named as a link.
func WithFollowSymlinks() Option {
	return func(o *options) error {
		o.flags |= engine.Symlink
		return nil
	}
}

// WithDevices makes DetectFile read a device, as MAGIC_DEVICES does.
func WithDevices() Option {
	return func(o *options) error {
		o.flags |= engine.Devices
		return nil
	}
}

// Check is one built-in test of libmagic.
type Check int

// The built-in tests, as the flags MAGIC_NO_CHECK_* name them.
const (
	CheckCompress Check = iota
	CheckTar
	CheckRules
	CheckAppType
	CheckELF
	CheckText
	CheckCDF
	CheckCSV
	CheckTokens
	CheckEncoding
	CheckJSON
	CheckSIMH
)

var checkFlags = [...]engine.Flags{
	CheckCompress: engine.NoCheckCompress,
	CheckTar:      engine.NoCheckTar,
	CheckRules:    engine.NoCheckSoft,
	CheckAppType:  engine.NoCheckAppType,
	CheckELF:      engine.NoCheckELF,
	CheckText:     engine.NoCheckText,
	CheckCDF:      engine.NoCheckCDF,
	CheckCSV:      engine.NoCheckCSV,
	CheckTokens:   engine.NoCheckTokens,
	CheckEncoding: engine.NoCheckEncoding,
	CheckJSON:     engine.NoCheckJSON,
	CheckSIMH:     engine.NoCheckSIMH,
}

// WithoutCheck turns off one built-in test, as a MAGIC_NO_CHECK_* flag does.
func WithoutCheck(c Check) Option {
	return func(o *options) error {
		if c < 0 || int(c) >= len(checkFlags) {
			return fmt.Errorf("magic: no check %d", c)
		}
		o.flags |= checkFlags[c]
		return nil
	}
}
