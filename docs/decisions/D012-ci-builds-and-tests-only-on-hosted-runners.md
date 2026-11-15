# D12. CI builds and tests only on hosted runners

Status: Decided. Amended by D16. Amended by D47.

Commit `dbcd068` (2026-09-12) added `.github/workflows/test.yml`. Commit
`dedf132` (2026-09-12) removed two parts of it.

The workflow runs on a push to `main`, on a pull request and by hand. Each of
five hosted runners builds, runs `go vet`, runs `go test -race -count=1 -v
./...` and runs `cmd/file` on three files in `testdata/`. The runners are
linux/amd64, linux/arm64, macOS amd64 (`macos-13`), macOS arm64 and
windows/amd64.

`dedf132` removed the `gofmt` step and the `cross` job. The `cross` job linked
every target with `zig cc`, which was the only CI build of linux/armv7.
Reason not recorded. The commit message is "More cleanup + fixes for
workflows".
