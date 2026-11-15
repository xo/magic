# D25. The upstream checkout is in `upstream/`, and git ignores it

Status: Decided.

The upstream repository has a checkout in `upstream/file/`, in the root of
this repository. `.gitignore` holds `/upstream/`. The column `checkout` of
`docs/upstream/upstreams.tsv` names the folder.

The checkout is the reference for the port. A person or an agent reads it,
runs `git log` and `git diff` in it, and builds the oracle from it (D30).
Nobody edits a file in it.

The repository is <https://github.com/file/file>, the public mirror of the
CVS repository of file. Upstream holds no Go file on 2026-11-15, so
`go build ./...` finds nothing in the checkout.

`src/`, which `build-libmagic.sh` fills with release tarballs, is a different
folder with a different purpose. It holds the sources that build the archives
of the cgo package, and it leaves with them (D22).

This follows okage D11.
