module github.com/xo/magic/compress/zstd

go 1.27

require (
	github.com/klauspost/compress v1.20.3
	github.com/xo/magic v0.0.0-00010101000000-000000000000
)

// The module uses the registry of the working tree until a release of magic
// holds it.
replace github.com/xo/magic => ../..
