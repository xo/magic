# D1. The package links static libmagic archives that the repository carries

Status: Decided. Amended by D16. Amended by D17. Amended by D19. Amended by D22. Amended by D47.

Commit `7b14d3f` (2026-09-04).

`magic.go` has one `#cgo LDFLAGS` line for each target. Each line points at
`libmagic/<target>/` and links `libmagic.a` with the four compression
libraries. The archives are committed. There are six targets: linux/amd64,
linux/arm64, linux/arm (armv7), darwin/amd64, darwin/arm64 and
windows/amd64.

The reason is in two comments. The workflow says that the archives are
static, "so there is nothing to install". `build-libmagic.sh` says "These
archives get committed", and it filters out pre-release tags for that reason
(D13).

A consequence: a platform that has no line in `magic.go` does not build.
