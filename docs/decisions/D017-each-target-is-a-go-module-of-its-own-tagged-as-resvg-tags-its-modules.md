# D17. Each target is a Go module of its own, tagged as resvg tags its modules

Status: Decided. Amends D1. Amends D2. Amends D16.

The maintainers decided on 2026-10-14 that magic follows the tagging pattern of
resvg. That pattern is in D9 of resvg and in `libresvg/README.md` of resvg. One
module for all targets made every user download every archive, and each new
target made it worse.

What changed:

- Each folder `libmagic/<goos>_<goarch>/` is a module with its own `go.mod`,
  its archives and a generated `link.go` that holds the `#cgo` line. The name
  `linux_arm` is armv7. The new names replace `linux-armv7`, `macos-amd64` and
  `macos-arm64`.
- `magic.go` has no `#cgo LDFLAGS` line for a platform any more. A file
  `link_<goos>_<goarch>.go` in the root imports the module of its platform.
  Go reads the end of the file name as a build constraint. The script writes
  both files.
- The root `go.mod` requires each target module and has a `replace` line that
  points it at its folder. The replace lines make the repository build before
  the tags exist. A user of the root module ignores them.
- A folder that has a `go.mod` is not part of the module above it. The root
  module no longer holds the archives, so a user downloads the archives of one
  platform and no other.
- `magic.h` and `magic.mgc` stay in `libmagic/` in the root module, because
  they are the same for every target.

The tags follow resvg, with one difference that Go forces:

- The root tag is a plain semver, with no prefix and no upstream version. It
  tracks the API of the package. A change to the wrapper alone bumps the root.
  A new target bumps the root minor. The root tags that exist are `v0.1.0`,
  `v0.2.0` and `v0.2.1`.
- A target tag is the path of the folder and the version of file that the
  archives hold, such as `libmagic/linux_amd64/v5.48.0`. A new release of file
  means new tags for every target, then an edit of the `require` lines, then a
  new root tag.
- The module path ends in `/v5`, such as
  `github.com/xo/magic/libmagic/linux_amd64/v5`. Go wants `/vN` on a module at
  version 2 or higher. resvg has no suffix, because its versions start with
  `v0`. The folder and the tags do not change.
- The order of a release is: push the commit, tag and push the target modules,
  edit the `require` lines if a version changed, then tag and push the root.
  A published tag never moves, because the module proxy caches it.

`TestEveryTargetIsWired` and `TestNoLinkFileWithoutATarget` hold the layout.

Nothing is tagged by this decision, because no maintainer said to tag. The tests
pass with the replace lines. No one has run `go get` from an empty module
against real tags, because the tags do not exist.

On 2026-10-14, a maintainer said to tag and publish. The target tags and
`v0.3.0` are on the commit `5a87245`, where CI passed on every job that gets a
runner. Then a program in an empty module ran `go get
github.com/xo/magic@v0.3.0` and printed the version and a type, as the layout
intends. `go get` fetched all eight target modules, because it reads the imports
for every platform. A plain `go build` from an empty module cache then
downloaded the root module and the `linux_amd64` module only.
