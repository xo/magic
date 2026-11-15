#!/usr/bin/env bash
# Build the oracle: libmagic from the checkout of file, against the C library
# of the host, and the program that answers the tests with it. See D30 and
# D33 in docs/decisions.
#
# It builds the commit that the port follows: the column matched of
# docs/upstream/upstreams.tsv, or base while matched is empty (D26).
# ORACLE_COMMIT builds another commit, for the work of following upstream.
#
#   test/oracle/build.sh
#
# Run it from anywhere. It needs the checkout in upstream/file (D25), gcc, the
# autotools, and the headers of zlib, bzip2, xz and zstd. It writes only to
# test/.oracle/, which git ignores. A second run rebuilds the program and keeps
# the library, unless the base commit changed.
#
# The reference of D33 is a build against glibc. On a host whose C library is
# not glibc, the script still builds, and says so.

set -euo pipefail

test_dir="$(cd "$(dirname "$0")/.." && pwd)"
root="$(cd "$test_dir/.." && pwd)"
checkout="$root/upstream/file"
out="$test_dir/.oracle"
# The commit from the ledger: matched, or base when matched is empty.
ledger="$root/docs/upstream/upstreams.tsv"
base="$(awk -F'\t' '$1 == "file" { print ($8 != "" ? $8 : $7) }' "$ledger")"
base="${ORACLE_COMMIT:-$base}"

if [ ! -d "$checkout/.git" ]; then
  echo "error: no checkout in $checkout. Make it with:" >&2
  echo "  git clone https://github.com/file/file.git upstream/file" >&2
  exit 1
fi
commit="$(git -C "$checkout" rev-parse "$base^{commit}")"

# The library depends only on the commit, so it is built once for each.
if [ "$(cat "$out/commit" 2>/dev/null || true)" != "$commit" ]; then
  rm -rf "$out"
  mkdir -p "$out/src"
  # Export the tree of the tag. The checkout itself is never touched.
  git -C "$checkout" archive "$commit" | tar -x -C "$out/src"
  (
    cd "$out/src"
    autoreconf -fi >"$out/autoreconf.log" 2>&1
    # The compression libraries are the ones of the host, built in. The
    # oracle program runs libmagic with a PATH where it finds no program, as
    # the port runs none (D36, D45). lzip, lrzip and lz4 have no decoder in
    # the port, so the oracle is built without their libraries.
    ./configure --prefix="$out/root" --enable-static --disable-shared \
      --enable-zlib --enable-bzlib --enable-xzlib --enable-zstdlib \
      --disable-lzlib --disable-lrziplib --disable-lz4lib --disable-libseccomp --disable-landlock \
      >"$out/configure.log" 2>&1
    make -j"$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)" >"$out/make.log" 2>&1
    make install >"$out/install.log" 2>&1
  )
  echo "$commit" >"$out/commit"
fi

# The oracle program links the library with cgo. The build tag keeps it out of
# go build ./... on a host that has no library.
(
  cd "$test_dir"
  CGO_ENABLED=1 CC="${ORACLE_CC:-gcc}" go build -tags oracle -o "$out/bin/oracle" ./cmd/oracle
)

# getconf prints "glibc 2.44" on glibc and fails elsewhere. It needs no pipe,
# which matters under pipefail: a reader that stops early, such as head, can
# make the writer fail with SIGPIPE and end the script.
libc="$(getconf GNU_LIBC_VERSION 2>/dev/null || echo unknown)"
echo "oracle: $out/bin/oracle"
echo "file:   $(git -C "$checkout" describe --tags "$commit") ($commit)"
echo "libc:   $libc"
if [ "${libc%% *}" != glibc ]; then
  echo "warning: D33 names glibc as the reference, and this host has $libc" >&2
fi
