module github.com/xo/magic/test

go 1.27

// The tests use the magic of the working tree. The root module points each
// target module at its folder, and a replace line of a dependency does not
// carry over, so the same lines are here.
replace (
	github.com/xo/magic => ../
	github.com/xo/magic/libmagic/darwin_amd64/v5 => ../libmagic/darwin_amd64
	github.com/xo/magic/libmagic/darwin_arm64/v5 => ../libmagic/darwin_arm64
	github.com/xo/magic/libmagic/freebsd_amd64/v5 => ../libmagic/freebsd_amd64
	github.com/xo/magic/libmagic/linux_amd64/v5 => ../libmagic/linux_amd64
	github.com/xo/magic/libmagic/linux_arm/v5 => ../libmagic/linux_arm
	github.com/xo/magic/libmagic/linux_arm64/v5 => ../libmagic/linux_arm64
	github.com/xo/magic/libmagic/windows_amd64/v5 => ../libmagic/windows_amd64
	github.com/xo/magic/libmagic/windows_arm64/v5 => ../libmagic/windows_arm64
)

require github.com/xo/magic v0.0.0-00010101000000-000000000000

require (
	github.com/xo/magic/libmagic/darwin_amd64/v5 v5.48.2 // indirect
	github.com/xo/magic/libmagic/darwin_arm64/v5 v5.48.2 // indirect
	github.com/xo/magic/libmagic/freebsd_amd64/v5 v5.48.1 // indirect
	github.com/xo/magic/libmagic/linux_amd64/v5 v5.48.2 // indirect
	github.com/xo/magic/libmagic/linux_arm/v5 v5.48.2 // indirect
	github.com/xo/magic/libmagic/linux_arm64/v5 v5.48.2 // indirect
	github.com/xo/magic/libmagic/windows_amd64/v5 v5.48.2 // indirect
	github.com/xo/magic/libmagic/windows_arm64/v5 v5.48.2 // indirect
)
