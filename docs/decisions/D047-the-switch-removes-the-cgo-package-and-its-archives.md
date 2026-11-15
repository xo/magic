# D47. The switch removes the cgo package and its archives

Status: Decided. Amends D1. Amends D2. Amends D3. Amends D5. Amends D7. Amends D9. Amends D10. Amends D12. Amends D13. Amends D16. Amends D17. Amends D18. Amends D19. Amends D20. Amends D21. Amends D43.

The maintainers decided on 2026-11-15 to make the switch of phase 4: the
pure Go port replaces the cgo package (D22, D39).

What changed:

1. The root package is the API of `docs/API.md` (D40, D42, D46). It has no
   cgo, and builds with `CGO_ENABLED=0` for every target of Go.
2. `magic.go` of the cgo package, its handle `Magic`, its flags, `Version`,
   `MagicDB` and the embedded `magic.mgc.zst` are gone (D3, D5, D7, D9, D10,
   D20, D21).
3. `libmagic/`, with the static archives and the module of each target, is
   gone, with `build-libmagic.sh` and the generated `link_*.go` files (D1, D2,
   D13, D16, D17, D18, D19, D43). The tags of the target modules stay
   published, and nothing will move them.
4. `go.mod` of the root has no `require` line. The decoders of xz and zstd
   are modules of their own (D45).
5. CI tests each runner with cgo off, builds for the targets that have no
   runner, and runs the race detector where it can (D12).
6. The test module is pure Go. Only the oracle, a program of its own, links
   libmagic, which `test/oracle/build.sh` builds from the checkout of file.

The gates of phase 4 hold on the build host: the gates of phase 3, now with
cgo off, and `TestAPIMatchesTheOracle`, which checks each form of a `Result`
of the public API against the oracle.

The release waits for phase 5. The maintainers chose one release, after the
port follows the newest commit of file, over a release of the switch alone.
