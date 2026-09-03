#!/usr/bin/env bash
# Cross-compile libmagic and its compression dependencies as static archives,
# using `zig cc` as the toolchain.
#
#   ./build-libmagic.sh                 # build every enabled target
#   ./build-libmagic.sh linux-arm64     # build just these
#
# Layout produced, ready for cgo:
#
#   libmagic/magic.h                    (shared: no target-conditional content)
#   libmagic/magic.mgc                  (shared: identical for every target)
#   libmagic/<target>/libmagic.a
#   libmagic/<target>/libz.a  libbz2.a  liblzma.a  libzstd.a
#   libmagic/<target>/libgnurx.a        (windows only)
#
# Requires: zig 0.13+, curl, tar, make, and a native C compiler.
#
# NOTE: verify tarball checksums yourself before using this for anything real.

set -euo pipefail

ZLIB_VER=1.3.2
BZIP2_VER=1.0.8
XZ_VER=5.8.3
ZSTD_VER=1.5.7
FILE_VER=5.48

# MinGW only: mingw-w64's CRT has no POSIX regex and file(1) requires it.
# libgnurx is glibc's regex engine packaged for Windows -- one tarball, one
# translation unit, no build system to fight. It's also the name file's
# configure probes for, so nothing needs patching or aliasing.
# Note it's LGPL 2.1, unlike everything else here.
#
# SourceForge has reorganised this project's file tree more than once, so the
# candidates below are tried in order. If they all 404, find a working mirror
# and override:  GNURX_URL=https://... ./build-libmagic.sh windows-amd64
GNURX_VER=2.5.1
GNURX_URLS=(
  "https://downloads.sourceforge.net/mingw/mingw-libgnurx-$GNURX_VER-src.tar.gz"
  "https://downloads.sourceforge.net/project/mingw/Other/UserContributed/regex/mingw-regex-$GNURX_VER/mingw-libgnurx-$GNURX_VER-src.tar.gz"
)
[ -n "${GNURX_URL:-}" ] && GNURX_URLS=("$GNURX_URL")

ROOT="$PWD"
SRC="$ROOT/src"
OUT="$ROOT/libmagic"
JOBS="$(getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)"
ORIG_PATH="$PATH"

export ZIG_GLOBAL_CACHE_DIR="${ZIG_GLOBAL_CACHE_DIR:-$ROOT/.zig-cache}"

# name|zig target|autotools --host|extra cflags
#
# --host only needs to LOOK like a GNU triplet -- it just flips configure into
# cross-compile mode. The real target comes from the wrapper scripts. Some
# vendored config.sub copies reject musl-flavoured triplets, hence the gnu
# spelling for arm.
TARGETS=(
  "linux-amd64|x86_64-linux-musl|x86_64-linux-musl|"
  "linux-arm64|aarch64-linux-musl|aarch64-linux-musl|"
  "linux-armv7|arm-linux-musleabihf|arm-linux-gnueabihf|-mcpu=generic+v7a"
  "macos-amd64|x86_64-macos|x86_64-apple-darwin|"
  "macos-arm64|aarch64-macos|aarch64-apple-darwin|"
  "windows-amd64|x86_64-windows-gnu|x86_64-w64-mingw32|"
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
    *)           echo linux ;;
  esac
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
  echo "       Set GNURX_URL=<working mirror> and re-run." >&2
  return 1
}

fetch_all() {
  fetch "https://zlib.net/zlib-$ZLIB_VER.tar.gz" "zlib-$ZLIB_VER"
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
  cp -r "$SRC/file-$FILE_VER" "$dir"
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

# ----------------------------------------------------------- components ----
# Everything installs into a staging prefix under .build; the flattened,
# Go-friendly copy is assembled at the end by stage_out.

build_zlib() {
  local prefix="$1" build="$2" os="$3"
  rm -rf "$build"; cp -r "$SRC/zlib-$ZLIB_VER" "$build"
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
  rm -rf "$build"; cp -r "$SRC/bzip2-$BZIP2_VER" "$build"
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
  fetch_any "mingw-libgnurx-$GNURX_VER" "${GNURX_URLS[@]}"
  rm -rf "$build"; cp -r "$SRC/mingw-libgnurx-$GNURX_VER" "$build"
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
  rm -rf "$build"; cp -r "$SRC/xz-$XZ_VER" "$build"
  ( cd "$build"
    ./configure --host="$host" --prefix="$prefix" \
      --enable-static --disable-shared \
      --disable-xz --disable-xzdec --disable-lzmadec --disable-lzmainfo \
      --disable-lzma-links --disable-scripts --disable-doc --disable-nls
    make -j"$JOBS"
    make install
  )
}

build_zstd() {
  local prefix="$1" build="$2"
  rm -rf "$build"; cp -r "$SRC/zstd-$ZSTD_VER" "$build"
  ( cd "$build/lib"
    # Single-threaded is the default for the static lib and is all libmagic
    # needs; keeps pthreads out of the link.
    make -j"$JOBS" libzstd.a
    make install-static install-includes PREFIX="$prefix"
  )
}

build_libmagic() {
  local prefix="$1" build="$2" host="$3" os="$4" file_compile="$5"
  rm -rf "$build"; cp -r "$SRC/file-$FILE_VER" "$build"

  local -a cache=()
  case "$os" in
    linux|macos)
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
    ./configure --host="$host" --prefix="$prefix" \
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

# ----------------------------------------------------------------- main ----

fetch_all
FILE_COMPILE="$(build_native_file)"
echo "native magic compiler: $FILE_COMPILE"

wanted=("$@")
built=()

for entry in "${TARGETS[@]}"; do
  IFS='|' read -r name ztarget host extra <<< "$entry"

  if [ ${#wanted[@]} -gt 0 ]; then
    match=0
    for w in "${wanted[@]}"; do [ "$w" = "$name" ] && match=1; done
    [ $match -eq 1 ] || continue
  fi

  os="$(os_of "$ztarget")"
  echo
  echo "=== $name ($ztarget, $os) ==="

  bin="$ROOT/.toolchains/$name"
  build="$ROOT/.build/$name"
  prefix="$build/prefix"

  rm -rf "$build"
  mkdir -p "$prefix"
  make_toolchain "$ztarget" "$bin" "$extra"

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
