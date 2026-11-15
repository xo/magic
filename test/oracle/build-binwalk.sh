#!/usr/bin/env bash
# Build the oracle of the walker: binwalk-ng from its checkout (D49, D60).
# The test module runs it on each input, and compares its matches and its
# files with the walker.
#
# It builds the commit that the walker follows: the column matched of
# docs/upstream/upstreams.tsv, or base while matched is empty (D26).
# ORACLE_COMMIT builds another commit, for the work of following upstream.
#
#   test/oracle/build-binwalk.sh
#
# Run it from anywhere. It needs the checkout in upstream/binwalk-ng (D25),
# with its submodule of samples, and the toolchain of Rust. It writes only to
# test/.binwalk/, which git ignores. A second run does nothing, unless the
# commit or the patches changed.
#
# The patches in test/oracle/patches fix the bugs of binwalk-ng that the
# walker fixes too, by a decision of the maintainers (D59). The script applies
# them in order of their names to the exported tree.

set -euo pipefail

test_dir="$(cd "$(dirname "$0")/.." && pwd)"
root="$(cd "$test_dir/.." && pwd)"
checkout="$root/upstream/binwalk-ng"
out="$test_dir/.binwalk"
ledger="$root/docs/upstream/upstreams.tsv"
base="$(awk -F'\t' '$1 == "binwalk-ng" { print ($8 != "" ? $8 : $7) }' "$ledger")"
base="${ORACLE_COMMIT:-$base}"

if [ ! -d "$checkout/.git" ]; then
  echo "error: no checkout in $checkout. Make it with:" >&2
  echo "  git clone --recurse-submodules https://github.com/binwalk-ng/binwalk-ng.git upstream/binwalk-ng" >&2
  exit 1
fi
commit="$(git -C "$checkout" rev-parse "$base^{commit}")"
patches="$test_dir/oracle/patches"
stamp="$commit $({ cat "$patches"/*.patch 2>/dev/null || true; } | sha256sum | cut -c1-16)"

if [ "$(cat "$out/commit" 2>/dev/null || true)" != "$stamp" ]; then
  # Keep the target folder, so that cargo builds again only what changed.
  rm -rf "$out/src" "$out/bin" "$out/commit"
  mkdir -p "$out/src" "$out/bin"
  # Export the tree of the commit. The checkout itself is never touched.
  git -C "$checkout" archive "$commit" | tar -x -C "$out/src"
  for p in "$patches"/*.patch; do
    [ -e "$p" ] || continue
    patch -s -p1 -d "$out/src" <"$p"
  done
  (
    cd "$out/src"
    CARGO_TARGET_DIR="$out/target" cargo build --release --locked >"$out/build.log" 2>&1
  )
  cp "$out/target/release/binwalk" "$out/bin/binwalk"
  echo "$stamp" >"$out/commit"
fi
echo "binwalk-ng at $commit: $out/bin/binwalk"
