module github.com/xo/magic

go 1.25

require (
	github.com/xo/magic/libmagic/darwin_amd64/v5 v5.48.0
	github.com/xo/magic/libmagic/darwin_arm64/v5 v5.48.0
	github.com/xo/magic/libmagic/freebsd_amd64/v5 v5.48.0
	github.com/xo/magic/libmagic/linux_amd64/v5 v5.48.0
	github.com/xo/magic/libmagic/linux_arm/v5 v5.48.0
	github.com/xo/magic/libmagic/linux_arm64/v5 v5.48.0
	github.com/xo/magic/libmagic/windows_amd64/v5 v5.48.0
	github.com/xo/magic/libmagic/windows_arm64/v5 v5.48.0
)

// Each target is tagged after the commit that holds it, so until then the
// working tree is the only place that has it. A user of the root module
// ignores these lines. See D17 in docs/PLAN.md.
replace (
	github.com/xo/magic/libmagic/darwin_amd64/v5 => ./libmagic/darwin_amd64
	github.com/xo/magic/libmagic/darwin_arm64/v5 => ./libmagic/darwin_arm64
	github.com/xo/magic/libmagic/freebsd_amd64/v5 => ./libmagic/freebsd_amd64
	github.com/xo/magic/libmagic/linux_amd64/v5 => ./libmagic/linux_amd64
	github.com/xo/magic/libmagic/linux_arm/v5 => ./libmagic/linux_arm
	github.com/xo/magic/libmagic/linux_arm64/v5 => ./libmagic/linux_arm64
	github.com/xo/magic/libmagic/windows_amd64/v5 => ./libmagic/windows_amd64
	github.com/xo/magic/libmagic/windows_arm64/v5 => ./libmagic/windows_arm64
)
