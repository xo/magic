module github.com/xo/magic/compress/zstd

go 1.27

require (
	github.com/klauspost/compress v1.20.3
	github.com/xo/magic v0.6.0
)

// In this repository, the module builds against the working tree. Go ignores
// this line in a program that imports the module, which gets v0.6.0 or later.
replace github.com/xo/magic => ../..
