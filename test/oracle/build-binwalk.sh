#!/usr/bin/env bash
# Build the oracle of the walker: binwalk 3 from the checkout of binwalk
# (D49). The test module runs it on each input, and compares its matches with
# the walker.
#
# It builds the commit that the walker follows: the column matched of
# docs/upstream/upstreams.tsv, or base while matched is empty (D26).
# ORACLE_COMMIT builds another commit, for the work of following upstream.
#
#   test/oracle/build-binwalk.sh
#
# Run it from anywhere. It needs the checkout in upstream/binwalk (D25) and
# the toolchain of Rust. It writes only to test/.binwalk/, which git ignores.
# A second run does nothing, unless the commit changed.

set -euo pipefail

test_dir="$(cd "$(dirname "$0")/.." && pwd)"
root="$(cd "$test_dir/.." && pwd)"
checkout="$root/upstream/binwalk"
out="$test_dir/.binwalk"
ledger="$root/docs/upstream/upstreams.tsv"
base="$(awk -F'\t' '$1 == "binwalk" { print ($8 != "" ? $8 : $7) }' "$ledger")"
base="${ORACLE_COMMIT:-$base}"

if [ ! -d "$checkout/.git" ]; then
  echo "error: no checkout in $checkout. Make it with:" >&2
  echo "  git clone https://github.com/ReFirmLabs/binwalk.git upstream/binwalk" >&2
  exit 1
fi
commit="$(git -C "$checkout" rev-parse "$base^{commit}")"

if [ "$(cat "$out/commit" 2>/dev/null || true)" != "$commit" ]; then
  # Keep the target folder, so that cargo builds again only what changed.
  rm -rf "$out/src" "$out/bin" "$out/commit"
  mkdir -p "$out/src" "$out/bin"
  # Export the tree of the commit. The checkout itself is never touched.
  git -C "$checkout" archive "$commit" | tar -x -C "$out/src"
  (
    cd "$out/src"
    CARGO_TARGET_DIR="$out/target" cargo build --release --locked >"$out/build.log" 2>&1
  )
  cp "$out/target/release/binwalk" "$out/bin/binwalk"
  echo "$commit" >"$out/commit"
fi
echo "binwalk at $commit: $out/bin/binwalk"
