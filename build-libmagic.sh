#!/usr/bin/env bash
# Cross-compile libmagic and its compression dependencies as static archives,
# using `zig cc` as the toolchain.
#
#   ./build-libmagic.sh                 # build every enabled target
#   ./build-libmagic.sh linux_arm64     # build just these
#   ./build-libmagic.sh modules         # rewrite go.mod and link.go, build nothing
#
# Dependency versions are resolved from upstream on each run, so an unmodified
# invocation builds against the current releases. Override per dependency, or
# turn the lookups off entirely:
#
#   FILE_VER=5.46 ./build-libmagic.sh   # pin one, resolve the rest
#   PINNED=1 ./build-libmagic.sh        # no lookups; use the fallbacks below
#
# Bumping file(1) changes magic.mgc and the strings libmagic reports, so the
# Go tests that assert exact descriptions may need regenerating afterwards.
#
# A target is named <goos>_<goarch>, as Go names it, and linux_arm is armv7.
#
# Layout produced, ready for cgo:
#
#   libmagic/magic.h                    (shared: no target-conditional content)
#   libmagic/magic.mgc                  (shared: identical for every target)
#   libmagic/<target>/libmagic.a
#   libmagic/<target>/libz.a  libbz2.a  liblzma.a  libzstd.a
#   libmagic/<target>/libgnurx.a        (windows only)
#   libmagic/<target>/go.mod            (each target is a Go module of its own)
#   libmagic/<target>/link.go           (the #cgo line that links the archives)
#
# Each target is a module so that a user who builds for one platform downloads
# the archives of that platform only. The script prints the tag commands at
# the end. See D17 in docs/PLAN.md.
#
# Requires: zig 0.13+, curl, tar, make, and a native C compiler. Resolving
# versions also wants git and a sort(1) with -V; without them the lookups fail
# soft and the fallback pins below are used.
#
# NOTE: verify tarball checksums yourself before using this for anything real.

set -euo pipefail

# Known-good versions: what the archives currently committed under libmagic/
# were built from. Used when PINNED is set, and whenever a lookup fails.
ZLIB_FALLBACK=1.3.2
BZIP2_FALLBACK=1.0.8
XZ_FALLBACK=5.8.4
ZSTD_FALLBACK=1.5.7
FILE_FALLBACK=5.48

# MinGW only: mingw-w64's CRT has no POSIX regex and file(1) requires it.
# libgnurx is glibc's regex engine packaged for Windows -- one tarball, one
# translation unit, no build system to fight. It's also the name file's
# configure probes for, so nothing needs patching or aliasing.
# Note it's LGPL 2.1, unlike everything else here.
#
# SourceForge has reorganised this project's file tree more than once, so
# resolve_versions builds a list of candidate URLs that are tried in order. If
# they all 404, find a working mirror and override:
#   GNURX_URL=https://... ./build-libmagic.sh windows_amd64
#
# There is no "latest" to look up: upstream last shipped in 2007 and the
# tarball is the only release there has ever been, so this one stays pinned.
GNURX_FALLBACK=2.5.1

ROOT="$PWD"
SRC="$ROOT/src"
OUT="$ROOT/libmagic"
JOBS="$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)"
ORIG_PATH="$PATH"

export ZIG_GLOBAL_CACHE_DIR="${ZIG_GLOBAL_CACHE_DIR:-$ROOT/.zig-cache}"

# name|zig target|autotools --host|extra cflags|cannot run on the build host
#
# --host only needs to LOOK like a GNU triplet -- it just flips configure into
# cross-compile mode. The real target comes from the wrapper scripts. Some
# vendored config.sub copies reject musl-flavoured triplets, hence the gnu
# spelling for arm.
#
# The last field is "noexec" for a target whose programs cannot run here. A
# configure script with only --host decides whether it is cross-compiling by
# trying to run a test program. On a host with wine, wine tries to run the
# windows_arm64 program and hangs. Passing --build as well makes configure
# assume a cross build and run nothing. It is not set for the first six
# targets, because their test programs run (under qemu or wine) and the
# archives already committed were built that way. Every later target sets it.
TARGETS=(
  "linux_amd64|x86_64-linux-musl|x86_64-linux-musl|"
  "linux_arm64|aarch64-linux-musl|aarch64-linux-musl|"
  "linux_arm|arm-linux-musleabihf|arm-linux-gnueabihf|-mcpu=generic+v7a"
  "darwin_amd64|x86_64-macos|x86_64-apple-darwin|"
  "darwin_arm64|aarch64-macos|aarch64-apple-darwin|"
  "windows_amd64|x86_64-windows-gnu|x86_64-w64-mingw32|"
  "windows_arm64|aarch64-windows-gnu|aarch64-w64-mingw32||noexec"
  "freebsd_amd64|x86_64-freebsd|x86_64-unknown-freebsd||noexec"
)

# ----------------------------------------------------------------- utils ----

inst() { mkdir -p "$(dirname "$2")"; cp -f "$1" "$2"; chmod 644 "$2"; }

have() { command -v "$1" >/dev/null 2>&1; }

# Install a file that should be identical for every target, and complain if a
# later target disagrees rather than silently letting the first one win.
share_once() {
  local label="$1" src="$2" dest="$3"
  if [ -f "$dest" ]; then
    cmp -s "$src" "$dest" || echo "WARNING: $label differs from $dest" >&2
  else
    inst "$src" "$dest"
  fi
}

os_of() {
  case "$1" in
    *-windows-*) echo windows ;;
    *-macos*)    echo macos ;;
    *-freebsd*)  echo freebsd ;;
    *)           echo linux ;;
  esac
}

# --------------------------------------------------------------- versions ----
# Each dependency resolves to its newest upstream release. Precedence is
# explicit env var, then the lookup, then the fallback pin -- a lookup that
# fails is a warning, not a build failure, since there is always a known-good
# version to fall back to.
#
# Pre-release tags are filtered out deliberately. These archives get committed,
# so the script must never quietly pick up an alpha or a beta.

# Highest vX.Y[.Z...] tag on a git remote. Uses the git protocol rather than
# the GitHub API, which avoids the unauthenticated rate limit.
latest_git_tag() {
  git ls-remote --tags --refs "$1" 2>/dev/null \
    | sed 's#.*refs/tags/##' \
    | grep -E '^v[0-9]+(\.[0-9]+)+$' \
    | sed 's/^v//' \
    | sort -V | tail -1
}

# Highest version of <name>-X.Y.tar.gz in an Apache-style directory index.
# The name is matched and stripped literally rather than by "leading
# non-digits", which would mangle a package whose name contains one (bzip2).
latest_index() {
  local url="$1" name="$2"
  curl -fsSL "$url" 2>/dev/null \
    | grep -oE "$name-[0-9]+(\.[0-9]+)*\.tar\.gz" \
    | sed -E "s/^$name-//; s/\.tar\.gz\$//" \
    | sort -V | tail -1
}

# Named latest_<var> so resolve_ver can find them from the variable name.
latest_ZLIB_VER()  { latest_git_tag https://github.com/madler/zlib; }
latest_XZ_VER()    { latest_git_tag https://github.com/tukaani-project/xz; }
latest_ZSTD_VER()  { latest_git_tag https://github.com/facebook/zstd; }
latest_BZIP2_VER() { latest_index https://sourceware.org/pub/bzip2/ bzip2; }
latest_FILE_VER()  { latest_index https://astron.com/pub/file/ file; }

resolve_ver() {
  local var="$1" fallback="$2" val how
  # Separate statement: bash expands every word of a `local` line before any
  # of its assignments take effect, so ${!var} here would not see var yet.
  val="${!var:-}"
  if [ -n "$val" ]; then
    how="pinned via \$$var"
  elif [ -n "${PINNED:-}" ]; then
    val="$fallback"; how="pinned via \$PINNED"
  elif val="$("latest_$var")" && [ -n "$val" ]; then
    how="latest"
  else
    val="$fallback"; how="fallback -- lookup failed"
    echo "WARNING: could not resolve latest $var, using $val" >&2
  fi
  printf -v "$var" '%s' "$val"
  printf '  %-6s %-10s %s\n' "${var%_VER}" "$val" "$how" >&2
}

resolve_versions() {
  echo "dependency versions:" >&2
  resolve_ver ZLIB_VER  "$ZLIB_FALLBACK"
  resolve_ver BZIP2_VER "$BZIP2_FALLBACK"
  resolve_ver XZ_VER    "$XZ_FALLBACK"
  resolve_ver ZSTD_VER  "$ZSTD_FALLBACK"
  resolve_ver FILE_VER  "$FILE_FALLBACK"

  # No lookup for gnurx -- see the note by GNURX_FALLBACK.
  GNURX_VER="${GNURX_VER:-$GNURX_FALLBACK}"
  printf '  %-6s %-10s %s\n' GNURX "$GNURX_VER" "pinned (no upstream releases)" >&2

  GNURX_URLS=(
    "https://downloads.sourceforge.net/mingw/mingw-libgnurx-$GNURX_VER-src.tar.gz"
    "https://downloads.sourceforge.net/project/mingw/Other/UserContributed/regex/mingw-regex-$GNURX_VER/mingw-libgnurx-$GNURX_VER-src.tar.gz"
  )
  [ -n "${GNURX_URL:-}" ] && GNURX_URLS=("$GNURX_URL")
  return 0
}

# ---------------------------------------------------------------- fetch ----

fetch() {
  local url="$1" dir="$2" file="${1##*/}"
  mkdir -p "$SRC"
  [ -f "$SRC/$file" ] || curl -fsSL -o "$SRC/$file" "$url"
  [ -d "$SRC/$dir" ]  || tar xf "$SRC/$file" -C "$SRC"
}

# Try several URLs for the same tarball, for sources whose hosting moves.
fetch_any() {
  local dir="$1"; shift
  local url file
  [ -d "$SRC/$dir" ] && return 0
  mkdir -p "$SRC"
  for url in "$@"; do
    file="${url##*/}"
    echo "trying $url" >&2
    if [ -f "$SRC/$file" ] || curl -fsSL -o "$SRC/$file" "$url"; then
      tar xf "$SRC/$file" -C "$SRC" && return 0
    fi
    rm -f "$SRC/$file"
  done
  echo "error: could not fetch $dir from any known URL." >&2
  return 1
}

fetch_all() {
  # zlib.net serves only the current release at the top level and moves
  # everything else to fossils/, so an explicitly pinned older version needs
  # the second URL.
  fetch_any "zlib-$ZLIB_VER" \
    "https://zlib.net/zlib-$ZLIB_VER.tar.gz" \
    "https://zlib.net/fossils/zlib-$ZLIB_VER.tar.gz"
  fetch "https://sourceware.org/pub/bzip2/bzip2-$BZIP2_VER.tar.gz" "bzip2-$BZIP2_VER"
  fetch "https://github.com/tukaani-project/xz/releases/download/v$XZ_VER/xz-$XZ_VER.tar.gz" "xz-$XZ_VER"
  fetch "https://github.com/facebook/zstd/releases/download/v$ZSTD_VER/zstd-$ZSTD_VER.tar.gz" "zstd-$ZSTD_VER"
  fetch "https://astron.com/pub/file/file-$FILE_VER.tar.gz" "file-$FILE_VER"
}

# ------------------------------------------------- native magic compiler ----
# file's build runs the `file` binary it just built to compile magic.mgc.
# The cross-built one won't run on the host, so build a throwaway native copy
# and point FILE_COMPILE at it.

build_native_file() {
  local dir="$ROOT/.native/file-$FILE_VER"
  if [ -x "$dir/src/file" ]; then echo "$dir/src/file"; return; fi

  mkdir -p "$ROOT/.native"
  rm -rf "$dir"
  cp -rp "$SRC/file-$FILE_VER" "$dir"
  ( cd "$dir"
    # env -u: this one must be a genuine host build, so scrub any cross vars.
    env -u CC -u CXX -u AR -u RANLIB -u NM -u STRIP \
        -u CFLAGS -u LDFLAGS -u CPPFLAGS \
      ./configure -q --disable-shared \
        --disable-zlib --disable-bzlib --disable-xzlib \
        --disable-zstdlib --disable-lzlib --disable-libseccomp >/dev/null
    env -u CC -u CXX -u AR -u RANLIB -u NM -u STRIP \
        -u CFLAGS -u LDFLAGS -u CPPFLAGS \
      make -j"$JOBS" >/dev/null
  )
  echo "$dir/src/file"
}

# ------------------------------------------------------------ toolchain ----

make_toolchain() {
  local zt="$1" bin="$2" extra="$3"
  rm -rf "$bin"; mkdir -p "$bin"

  # cc AND gcc AND clang: plenty of hand-written Makefiles hardcode a compiler
  # name. Since $bin goes on the front of PATH, they all resolve to the cross
  # compiler.
  for n in cc gcc clang; do
    cat > "$bin/$n" <<EOF
#!/bin/sh
exec zig cc -target $zt $extra "\$@"
EOF
  done

  for n in c++ g++; do
    cat > "$bin/$n" <<EOF
#!/bin/sh
exec zig c++ -target $zt $extra "\$@"
EOF
  done

  # zig ar/ranlib are llvm-ar/llvm-ranlib; they take no -target.
  for t in ar ranlib; do
    cat > "$bin/$t" <<EOF
#!/bin/sh
exec zig $t "\$@"
EOF
  done

  # libtool wants nm and strip. The host's GNU binutils can't read Mach-O or
  # COFF, so prefer llvm-* when present and degrade gracefully when not.
  if have llvm-nm; then
    printf '#!/bin/sh\nexec llvm-nm "$@"\n' > "$bin/nm"
  fi
  if have llvm-strip; then
    printf '#!/bin/sh\nexec llvm-strip "$@"\n' > "$bin/strip"
  else
    printf '#!/bin/sh\nexit 0\n' > "$bin/strip"
  fi

  chmod +x "$bin"/*
}

# xz compiles a version resource for Windows, and its configure looks for
# <host>-windres. The host has windres only for x86_64 and i686, so any other
# Windows target needs one. llvm-windres picks its target from the name that
# it runs as, so it is linked under that name. It needs the mingw headers
# that zig carries to preprocess a .rc file, so a wrapper passes the path.
make_windres() {
  local host="$1" bin="$2" zig_lib
  have llvm-windres || { echo "error: llvm-windres is required for $host" >&2; return 1; }
  zig_lib="$(zig env | sed -n 's/.*\.lib_dir = "\(.*\)".*/\1/p')"
  mkdir -p "$bin/real"
  ln -sf "$(command -v llvm-windres)" "$bin/real/$host-windres"
  printf '#!/bin/sh\nexec "%s/real/%s-windres" -I "%s/libc/include/any-windows-any" "$@"\n' \
    "$bin" "$host" "$zig_lib" > "$bin/$host-windres"
  chmod +x "$bin/$host-windres"
}

# ----------------------------------------------------------- components ----
# Everything installs into a staging prefix under .build; the flattened,
# Go-friendly copy is assembled at the end by stage_out.

build_zlib() {
  local prefix="$1" build="$2" os="$3"
  rm -rf "$build"; cp -rp "$SRC/zlib-$ZLIB_VER" "$build"
  ( cd "$build"
    if [ "$os" = windows ]; then
      # zlib's ./configure is a Unix-only shell script that mangles the mingw
      # case. win32/Makefile.gcc is the documented path for Windows builds.
      make -f win32/Makefile.gcc -j"$JOBS" libz.a \
        CC="$CC" AR="$AR" RANLIB="$RANLIB" ARFLAGS=rcs
    else
      # Hand-rolled configure; honours CC/AR/RANLIB from the environment.
      # --static skips the shared-library naming logic, which would otherwise
      # go wrong whenever host OS != target OS.
      ./configure --static --prefix="$prefix"
      make -j"$JOBS"
    fi
    inst libz.a  "$prefix/lib/libz.a"
    inst zlib.h  "$prefix/include/zlib.h"
    inst zconf.h "$prefix/include/zconf.h"
  )
}

build_bzip2() {
  local prefix="$1" build="$2"
  rm -rf "$build"; cp -rp "$SRC/bzip2-$BZIP2_VER" "$build"
  ( cd "$build"
    # Compile directly rather than driving bzip2's Makefile, which has two
    # problems here:
    #
    #  1. It ASSIGNS the toolchain (CC=gcc, AR=ar, RANLIB=ranlib) rather than
    #     defaulting it, so environment vars are ignored and you silently get
    #     host-gcc objects. On a native-arch target those link far enough to
    #     fail libmagic's configure with "bzlib support requested but not
    #     found", because glibc fortify symbols don't resolve against musl.
    #
    #  2. Its compile rules omit -o. Clang -- and therefore zig cc -- names
    #     the output foo.obj instead of foo.o when the target is Windows, so
    #     the archive step dies with "blocksort.o: No such file or directory".
    #
    # The source list is fixed; bzip2 1.0.x has not changed since 2019.
    local objs=""
    for s in blocksort huffman crctable randtable compress decompress bzlib; do
      "$CC" -O2 -D_FILE_OFFSET_BITS=64 -c "$s.c" -o "$s.o"
      objs="$objs $s.o"
    done
    # shellcheck disable=SC2086
    "$AR" rcs libbz2.a $objs
    "$RANLIB" libbz2.a
    inst libbz2.a "$prefix/lib/libbz2.a"
    inst bzlib.h  "$prefix/include/bzlib.h"
  )
}

build_gnurx() {
  local prefix="$1" build="$2"
  fetch_any "mingw-libgnurx-$GNURX_VER" "${GNURX_URLS[@]}" \
    || { echo "       Set GNURX_URL=<working mirror> and re-run." >&2; return 1; }
  rm -rf "$build"; cp -rp "$SRC/mingw-libgnurx-$GNURX_VER" "$build"
  ( cd "$build"
    # regex.c #includes regcomp.c/regexec.c/regex_internal.c, so one
    # translation unit is the whole library -- don't compile the others
    # separately or you'll get duplicate symbols. Compiling by hand also
    # sidesteps the upstream Makefile, which is built around producing a DLL
    # plus import library.
    "$CC" -O2 -I. -c regex.c -o regex.o
    "$AR" rcs libgnurx.a regex.o
    "$RANLIB" libgnurx.a
    inst libgnurx.a "$prefix/lib/libgnurx.a"
    inst regex.h    "$prefix/include/regex.h"
  )
}

build_xz() {
  local prefix="$1" build="$2" host="$3"
  rm -rf "$build"; cp -rp "$SRC/xz-$XZ_VER" "$build"
  ( cd "$build"
    ./configure --host="$host" "${CONFIGURE_BUILD[@]}" --prefix="$prefix" \
      --enable-static --disable-shared \
      --disable-xz --disable-xzdec --disable-lzmadec --disable-lzmainfo \
      --disable-lzma-links --disable-scripts --disable-doc --disable-nls
    make -j"$JOBS"
    make install
  )
}

build_zstd() {
  local prefix="$1" build="$2"
  rm -rf "$build"; cp -rp "$SRC/zstd-$ZSTD_VER" "$build"
  ( cd "$build/lib"
    # Single-threaded is the default for the static lib and is all libmagic
    # needs; keeps pthreads out of the link.
    make -j"$JOBS" libzstd.a
    make install-static install-includes PREFIX="$prefix"
  )
}

build_libmagic() {
  local prefix="$1" build="$2" host="$3" os="$4" file_compile="$5"
  rm -rf "$build"; cp -rp "$SRC/file-$FILE_VER" "$build"

  local -a cache=()
  case "$os" in
    linux|macos|freebsd)
      # AC_FUNC_MMAP uses a run-time test, which always reports failure under
      # cross-compilation and silently degrades libmagic to read(). (On a
      # native-arch musl target configure can actually run the test, so this
      # is a no-op there.)
      cache+=(ac_cv_func_mmap_fixed_mapped=yes)
      ;;
    windows)
      cache+=(ac_cv_func_mmap_fixed_mapped=no)
      ;;
  esac

  ( cd "$build"
    ./configure --host="$host" "${CONFIGURE_BUILD[@]}" --prefix="$prefix" \
      --enable-static --disable-shared \
      --enable-zlib --enable-bzlib --enable-xzlib --enable-zstdlib \
      --disable-lzlib --disable-libseccomp \
      CPPFLAGS="-I$prefix/include" \
      LDFLAGS="-L$prefix/lib" \
      "${cache[@]}"
    make -j"$JOBS" FILE_COMPILE="$file_compile"
    make install FILE_COMPILE="$file_compile"
  )
}

# --------------------------------------------------------------- output ----
# Flatten the staging prefix into the layout cgo wants: archives directly in
# libmagic/<target>/. magic.h and magic.mgc are target-independent and live one
# level up, so the Go package needs a single -I and a single //go:embed.

stage_out() {
  local prefix="$1" dest="$2"
  rm -rf "$dest"; mkdir -p "$dest"
  cp -f "$prefix"/lib/*.a "$dest/"
  chmod 644 "$dest"/*.a
}

# Each target folder is a Go module of its own, with one file that links the
# archives. The root module imports it for its platform, in link_<target>.go.
# The file is generated, so the link line is the same everywhere but for
# Windows, which also needs libgnurx and shlwapi.
#
# Go wants a module at major version 2 or higher to end its path in /vN, so
# the file version 5.48 gives the module path .../libmagic/<target>/v5. The
# module is still in the folder libmagic/<target>, and its tags are
# libmagic/<target>/v5.48.0.
write_module() {
  local name="$1" os="$2" libs="-lmagic -lz -lbz2 -llzma -lzstd"
  local major="v${FILE_VER%%.*}"
  if [ "$os" = windows ]; then
    libs="-lmagic -lgnurx -lz -lbz2 -llzma -lzstd -lshlwapi"
  fi
  printf 'module github.com/xo/magic/libmagic/%s/%s\n\ngo 1.21\n' "$name" "$major" \
    > "$OUT/$name/go.mod"
  cat > "$OUT/$name/link.go" <<EOF
// Code generated by build-libmagic.sh. DO NOT EDIT.

// Package archive holds the static libmagic archives for $name. The root
// module imports it for that platform, and the line below links them.
package archive

/*
#cgo LDFLAGS: -L\${SRCDIR} $libs
*/
import "C"
EOF
  # The file name ends in _<goos>_<goarch>, which Go reads as a build
  # constraint, so the module is imported on that platform only.
  cat > "$ROOT/link_$name.go" <<EOF
// Code generated by build-libmagic.sh. DO NOT EDIT.

package magic

import _ "github.com/xo/magic/libmagic/$name/$major"
EOF
}

# ----------------------------------------------------------------- main ----

# "modules" rewrites the module files of every target that has a folder, and
# builds nothing. It needs no network.
if [ "${1:-}" = modules ]; then
  FILE_VER="${FILE_VER:-$FILE_FALLBACK}"
  for entry in "${TARGETS[@]}"; do
    IFS='|' read -r name ztarget _ <<< "$entry"
    if [ -d "$OUT/$name" ]; then write_module "$name" "$(os_of "$ztarget")"; fi
  done
  exit 0
fi

resolve_versions
fetch_all
FILE_COMPILE="$(build_native_file)"
echo "native magic compiler: $FILE_COMPILE"

wanted=("$@")
built=()

for entry in "${TARGETS[@]}"; do
  IFS='|' read -r name ztarget host extra noexec <<< "$entry"

  if [ ${#wanted[@]} -gt 0 ]; then
    match=0
    for w in "${wanted[@]}"; do [ "$w" = "$name" ] && match=1; done
    [ $match -eq 1 ] || continue
  fi

  os="$(os_of "$ztarget")"
  CONFIGURE_BUILD=()
  if [ "$noexec" = noexec ]; then
    CONFIGURE_BUILD=(--build="$(PATH="$ORIG_PATH" cc -dumpmachine)")
  fi
  echo
  echo "=== $name ($ztarget, $os) ==="

  bin="$ROOT/.toolchains/$name"
  build="$ROOT/.build/$name"
  prefix="$build/prefix"

  rm -rf "$build"
  mkdir -p "$prefix"
  make_toolchain "$ztarget" "$bin" "$extra"
  if [ "$noexec" = noexec ] && [ "$os" = windows ]; then
    make_windres "$host" "$bin"
  fi

  # Reset from ORIG_PATH every iteration -- prepending inside the loop would
  # otherwise leave every previous target's wrappers on PATH.
  export PATH="$bin:$ORIG_PATH"
  export CC="$bin/cc" CXX="$bin/c++" AR="$bin/ar" RANLIB="$bin/ranlib"
  export STRIP="$bin/strip"
  if [ -x "$bin/nm" ]; then export NM="$bin/nm"; else unset NM || true; fi

  build_zlib     "$prefix" "$build/zlib"  "$os"
  build_bzip2    "$prefix" "$build/bzip2"
  build_xz       "$prefix" "$build/xz"    "$host"
  build_zstd     "$prefix" "$build/zstd"
  if [ "$os" = windows ]; then
    build_gnurx  "$prefix" "$build/gnurx"
  fi
  build_libmagic "$prefix" "$build/file"  "$host" "$os" "$FILE_COMPILE"

  stage_out "$prefix" "$OUT/$name"
  write_module "$name" "$os"

  # magic.h is generated from magic.h.in by substituting the version string;
  # it has no target-conditional content. magic.mgc is produced by the native
  # FILE_COMPILE from the same sources, and its format is endian-independent
  # (libmagic byte-swaps on load). Both are shared by every target.
  share_once "magic.h"   "$prefix/include/magic.h"     "$OUT/magic.h"
  share_once "magic.mgc" "$prefix/share/misc/magic.mgc" "$OUT/magic.mgc"

  echo "--> $OUT/$name"
  ls -1 "$OUT/$name"
  built+=("$name")
done

export PATH="$ORIG_PATH"

echo
if [ ${#built[@]} -eq 0 ]; then
  echo "no targets matched: ${wanted[*]:-<none specified>}"
  exit 1
fi
echo "done: ${built[*]}"

# A module version is the file version with a patch number. Never move a tag
# that is published, because the module proxy caches it. A rebuild at the same
# file version, such as one for a new xz, needs the next patch number.
tagver="v$FILE_VER"
case "$FILE_VER" in *.*.*) ;; *) tagver="$tagver.0" ;; esac
echo
echo "After the commit is pushed, tag each module, push the tags, edit the"
echo "require block of go.mod to name them, and then tag the root:"
for n in "${built[@]}"; do echo "  git tag libmagic/$n/$tagver"; done
