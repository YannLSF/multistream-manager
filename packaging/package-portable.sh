#!/bin/sh
set -eu

VERSION="0.5.0"

FF_VER="n9.0.2-17-g2a571b6068"
FF_TAG="autobuild-2026-09-30-13-08"

FF_LINUX_ARCHIVE="ffmpeg-${FF_VER}-linux64-lgpl-9.0.tar.xz"
FF_WINDOWS_ARCHIVE="ffmpeg-${FF_VER}-win64-lgpl-9.0.zip"

EXPECTED_LINUX_FFMPEG="953632dece404bb1461a4a2285f0191a6105d72c81f9da577eb54326a7fad49a"
EXPECTED_LINUX_FFPROBE="c0d4ee234730e7b083cc729108ac34f3b718cc4858c3cf8ed6e0dcec545f13c1"
EXPECTED_LINUX_MEDIAMTX="f152f85cc8401715a1eefacb561826b6b99ab54c046c9684d78c91016267979a"

EXPECTED_WINDOWS_FFMPEG="0e482b853f2fd4821055d20cdce510f0eea6027037c04b994f454a2136e8868e"
EXPECTED_WINDOWS_FFPROBE="b9627185ffbad1d4fc6ef953868ca9ad8df75fc8dd035fcab58aca78caa2d23e"
EXPECTED_WINDOWS_MEDIAMTX="042f05ab2f74a253a2a98f62000aba01c11b259ecda4b53781ea64c9afbbabf5"

EXPECTED_FF_LINUX_ARCHIVE="2d41cbea0ca1a15029b638330740f78d6f5aeb062355bf425433fc938705350a"
EXPECTED_FF_WINDOWS_ARCHIVE="6b264b9e6019103f601d98c292bd332fd87acf1c5e941ddff4fb71760fe63432"

SCRIPT_DIR="$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)"
ROOT="$(CDPATH= cd -- "$SCRIPT_DIR/.." && pwd)"

ASSETS="${RUNTIME_ASSETS:-$HOME/multistream-runtime-assets}"
FF_DOWNLOAD="${FFMPEG_DOWNLOAD_DIR:-$HOME/multistream-ffmpeg-download}"
MEDIAMTX_SOURCE="${MEDIAMTX_SOURCE_DIR:-$HOME/mediamtx-twitch}"
DIST="${DIST_DIR:-$ROOT/dist}"

LINUX_DIR="$DIST/Ylyxium-Multistream-Manager-v${VERSION}-linux-x64"
WINDOWS_DIR="$DIST/Ylyxium-Multistream-Manager-v${VERSION}-windows-x64"

TMP="$(mktemp -d)"

cleanup() {
  rm -rf "$TMP"
}

trap cleanup EXIT INT TERM

fail() {
  echo "ERREUR: $*" >&2
  exit 1
}

hash_file() {
  sha256sum "$1" | awk '{print $1}'
}

check_hash() {
  file="$1"
  expected="$2"

  [ -f "$file" ] || fail "fichier absent: $file"

  actual="$(hash_file "$file")"

  if [ "$actual" != "$expected" ]; then
    echo "Hash attendu : $expected" >&2
    echo "Hash obtenu  : $actual" >&2
    fail "SHA256 incorrect pour $file"
  fi
}

echo "=== PRECONDITIONS ==="

cd "$ROOT"

[ -z "$(git status --porcelain)" ] ||
  fail "le depot Git doit etre propre avant packaging"

BRANCH="$(git branch --show-current)"
COMMIT="$(git rev-parse HEAD)"

echo "Branche : $BRANCH"
echo "Commit  : $COMMIT"

grep -Fq 'appVersion     = "0.5.0"' main.go ||
  fail "main.go n'est pas en v0.5.0"

command -v docker >/dev/null 2>&1 ||
  fail "docker est requis pour compiler les binaires Go"

command -v zip >/dev/null 2>&1 ||
  fail "zip est requis"

command -v unzip >/dev/null 2>&1 ||
  fail "unzip est requis"

echo
echo "=== VERIFICATION ASSETS FIGES ==="

check_hash \
  "$ASSETS/linux-amd64/ffmpeg" \
  "$EXPECTED_LINUX_FFMPEG"

check_hash \
  "$ASSETS/linux-amd64/ffprobe" \
  "$EXPECTED_LINUX_FFPROBE"

check_hash \
  "$ASSETS/linux-amd64/mediamtx" \
  "$EXPECTED_LINUX_MEDIAMTX"

check_hash \
  "$ASSETS/windows-amd64/ffmpeg.exe" \
  "$EXPECTED_WINDOWS_FFMPEG"

check_hash \
  "$ASSETS/windows-amd64/ffprobe.exe" \
  "$EXPECTED_WINDOWS_FFPROBE"

check_hash \
  "$ASSETS/windows-amd64/mediamtx.exe" \
  "$EXPECTED_WINDOWS_MEDIAMTX"

check_hash \
  "$FF_DOWNLOAD/$FF_LINUX_ARCHIVE" \
  "$EXPECTED_FF_LINUX_ARCHIVE"

check_hash \
  "$FF_DOWNLOAD/$FF_WINDOWS_ARCHIVE" \
  "$EXPECTED_FF_WINDOWS_ARCHIVE"

echo "Assets runtime : OK"

echo
echo "=== TESTS GO ==="

docker run --rm \
  --user "$(id -u):$(id -g)" \
  -e GOCACHE=/tmp/go-cache \
  -e GOPATH=/tmp/go \
  -v "$ROOT:/src" \
  -w /src \
  golang:1.23-alpine \
  sh -c 'go test ./...'

echo
echo "=== BUILD MANAGER ==="

mkdir -p "$TMP/out"

docker run --rm \
  --user "$(id -u):$(id -g)" \
  -e GOCACHE=/tmp/go-cache \
  -e GOPATH=/tmp/go \
  -v "$ROOT:/src" \
  -v "$TMP/out:/out" \
  -w /src \
  golang:1.23-alpine \
  sh -c '
    set -eu

    CGO_ENABLED=0 \
    GOOS=linux \
    GOARCH=amd64 \
      go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/YlyxiumMultistreamManager \
      .

    CGO_ENABLED=0 \
    GOOS=windows \
    GOARCH=amd64 \
      go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /out/YlyxiumMultistreamManager.exe \
      .
  '

echo
echo "=== EXTRACTION LICENCES FFMPEG ==="

mkdir -p "$TMP/ff-linux" "$TMP/ff-windows"

tar -xJf \
  "$FF_DOWNLOAD/$FF_LINUX_ARCHIVE" \
  -C "$TMP/ff-linux"

unzip -q \
  "$FF_DOWNLOAD/$FF_WINDOWS_ARCHIVE" \
  -d "$TMP/ff-windows"

FF_LINUX_LICENSE="$(
  find "$TMP/ff-linux" -type f -name LICENSE.txt -print -quit
)"

FF_WINDOWS_LICENSE="$(
  find "$TMP/ff-windows" -type f -name LICENSE.txt -print -quit
)"

[ -n "$FF_LINUX_LICENSE" ] ||
  fail "LICENSE.txt Linux FFmpeg introuvable"

[ -n "$FF_WINDOWS_LICENSE" ] ||
  fail "LICENSE.txt Windows FFmpeg introuvable"

echo
echo "=== PREPARATION DOSSIERS ==="

rm -rf "$DIST"

mkdir -p \
  "$LINUX_DIR/bin" \
  "$LINUX_DIR/data" \
  "$LINUX_DIR/licenses" \
  "$WINDOWS_DIR/bin" \
  "$WINDOWS_DIR/data" \
  "$WINDOWS_DIR/licenses"

chmod 700 "$LINUX_DIR/data"

cp "$TMP/out/YlyxiumMultistreamManager" \
  "$LINUX_DIR/YlyxiumMultistreamManager"

cp "$ASSETS/linux-amd64/ffmpeg" \
  "$LINUX_DIR/bin/ffmpeg"

cp "$ASSETS/linux-amd64/ffprobe" \
  "$LINUX_DIR/bin/ffprobe"

cp "$ASSETS/linux-amd64/mediamtx" \
  "$LINUX_DIR/bin/mediamtx"

chmod 755 \
  "$LINUX_DIR/YlyxiumMultistreamManager" \
  "$LINUX_DIR/bin/ffmpeg" \
  "$LINUX_DIR/bin/ffprobe" \
  "$LINUX_DIR/bin/mediamtx"

cp "$TMP/out/YlyxiumMultistreamManager.exe" \
  "$WINDOWS_DIR/YlyxiumMultistreamManager.exe"

cp "$ASSETS/windows-amd64/ffmpeg.exe" \
  "$WINDOWS_DIR/bin/ffmpeg.exe"

cp "$ASSETS/windows-amd64/ffprobe.exe" \
  "$WINDOWS_DIR/bin/ffprobe.exe"

cp "$ASSETS/windows-amd64/mediamtx.exe" \
  "$WINDOWS_DIR/bin/mediamtx.exe"

cp "$SCRIPT_DIR/README-portable.txt" \
  "$LINUX_DIR/README.txt"

cp "$SCRIPT_DIR/README-portable.txt" \
  "$WINDOWS_DIR/README.txt"

cp "$SCRIPT_DIR/THIRD-PARTY-NOTICES.txt" \
  "$LINUX_DIR/THIRD-PARTY-NOTICES.txt"

cp "$SCRIPT_DIR/THIRD-PARTY-NOTICES.txt" \
  "$WINDOWS_DIR/THIRD-PARTY-NOTICES.txt"

cp "$FF_LINUX_LICENSE" \
  "$LINUX_DIR/licenses/FFMPEG-LICENSE.txt"

cp "$FF_WINDOWS_LICENSE" \
  "$WINDOWS_DIR/licenses/FFMPEG-LICENSE.txt"

cp "$MEDIAMTX_SOURCE/LICENSE" \
  "$LINUX_DIR/licenses/MEDIAMTX-LICENSE.txt"

cp "$MEDIAMTX_SOURCE/LICENSE" \
  "$WINDOWS_DIR/licenses/MEDIAMTX-LICENSE.txt"

echo
echo "=== MANIFESTES ==="

FF_CONFIG="$(
  "$LINUX_DIR/bin/ffmpeg" -version 2>&1 |
  sed -n 's/^configuration: //p'
)"

MEDIAMTX_COMMIT="$(
  git -C "$MEDIAMTX_SOURCE" rev-parse HEAD
)"

GORTMPLIB_COMMIT="$(
  git -C "$MEDIAMTX_SOURCE/gortmplib-local" rev-parse HEAD
)"

write_manifest() {
  dir="$1"
  platform="$2"

  {
    echo "Ylyxium Multistream Manager v$VERSION"
    echo "Build manifest"
    echo "=============="
    echo
    echo "Platform: $platform"
    echo
    echo "Ylyxium Multistream Manager"
    echo "-------------------"
    echo "Version: $VERSION"
    echo "Commit: $COMMIT"
    echo
    echo "MediaMTX"
    echo "--------"
    echo "Version: v1.21.1-enhanced-rtmp.1"
    echo "Commit: $MEDIAMTX_COMMIT"
    echo "gortmplib commit: $GORTMPLIB_COMMIT"
    echo
    echo "FFmpeg"
    echo "------"
    echo "Build tag: $FF_TAG"
    echo "Version: $FF_VER"
    echo "Variant: LGPL / FFmpeg 9.0"
    echo
    echo "FFmpeg configure:"
    echo "$FF_CONFIG"
    echo
    echo "Files"
    echo "-----"

    (
      cd "$dir"
      find . -type f \
        ! -name BUILD-MANIFEST.txt \
        -print |
        LC_ALL=C sort |
        while IFS= read -r file; do
          sha256sum "$file"
        done
    )
  } >"$dir/BUILD-MANIFEST.txt"
}

write_manifest \
  "$LINUX_DIR" \
  "linux-x64 (glibc >= 2.28, Linux >= 4.18)"

write_manifest \
  "$WINDOWS_DIR" \
  "windows-x64"

echo
echo "=== CONTROLES PACKAGES ==="

"$LINUX_DIR/bin/ffmpeg" -version | head -1
"$LINUX_DIR/bin/ffprobe" -version | head -1
"$LINUX_DIR/bin/mediamtx" --version

file \
  "$LINUX_DIR/YlyxiumMultistreamManager" \
  "$WINDOWS_DIR/YlyxiumMultistreamManager.exe"

grep -Fq "Version: $VERSION" \
  "$LINUX_DIR/BUILD-MANIFEST.txt"

grep -Fq "Version: $VERSION" \
  "$WINDOWS_DIR/BUILD-MANIFEST.txt"

echo
echo "=== ARCHIVES ==="

LINUX_ARCHIVE="Ylyxium-Multistream-Manager-v${VERSION}-linux-x64.tar.gz"
WINDOWS_ARCHIVE="Ylyxium-Multistream-Manager-v${VERSION}-windows-x64.zip"

(
  cd "$DIST"
  tar -czf "$LINUX_ARCHIVE" \
    "$(basename "$LINUX_DIR")"

  zip -qr "$WINDOWS_ARCHIVE" \
    "$(basename "$WINDOWS_DIR")"
)

(
  cd "$DIST"

  sha256sum \
    "$LINUX_ARCHIVE" \
    "$WINDOWS_ARCHIVE" \
    > SHA256SUMS.txt
)

echo
echo "=== RESULTAT ==="

ls -lh \
  "$DIST/$LINUX_ARCHIVE" \
  "$DIST/$WINDOWS_ARCHIVE" \
  "$DIST/SHA256SUMS.txt"

echo

cat "$DIST/SHA256SUMS.txt"

echo
echo "Packaging v$VERSION : OK"
