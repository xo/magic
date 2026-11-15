module github.com/xo/magic/test

go 1.27

// The tests use the magic of the working tree, and the decoder modules of
// compress/ in it.
replace (
	github.com/xo/magic => ../
	github.com/xo/magic/compress/xz => ../compress/xz
	github.com/xo/magic/compress/zstd => ../compress/zstd
)

require (
	github.com/klauspost/compress v1.20.3
	github.com/ulikunitz/xz v0.5.17
	github.com/xo/magic v0.5.0
	github.com/xo/magic/compress/xz v0.0.0-00010101000000-000000000000
	github.com/xo/magic/compress/zstd v0.0.0-00010101000000-000000000000
)
