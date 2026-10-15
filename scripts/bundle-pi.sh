#!/usr/bin/env bash
#
# Download the prebuilt pi runtime into resources/pi so a release build can
# embed it. A development build without it falls back to the user's own pi,
# chosen in Settings.
#
#   scripts/bundle-pi.sh [platform]
#
# platform defaults to the host, in pi's release naming: darwin-arm64,
# darwin-x64, linux-arm64, linux-x64, windows-arm64, windows-x64.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${PI_VERSION:-1.2.0}"
PLATFORM="${1:-}"
if [[ -z "$PLATFORM" ]]; then
  case "$(uname -s)/$(uname -m)" in
    Darwin/arm64)              PLATFORM=darwin-arm64 ;;
    Darwin/x86_64)             PLATFORM=darwin-x64 ;;
    Linux/aarch64 | Linux/arm64) PLATFORM=linux-arm64 ;;
    Linux/x86_64)              PLATFORM=linux-x64 ;;
    *) echo "unsupported host architecture; pass a platform" >&2; exit 1 ;;
  esac
fi

base="https://github.com/earendil-works/pi/releases/download/v$VERSION"
ext=tar.gz
[[ "$PLATFORM" == windows-* ]] && ext=zip
archive="pi-$PLATFORM.$ext"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "Downloading pi $VERSION for $PLATFORM…"
curl -fsSL "$base/$archive" -o "$tmp/$archive"
curl -fsSL "$base/SHA256SUMS" -o "$tmp/SHA256SUMS"

# Verify against the published sums before anything is unpacked.
( cd "$tmp"
  line="$(grep " $archive\$" SHA256SUMS || true)"
  [[ -n "$line" ]] || { echo "no checksum for $archive" >&2; exit 1; }
  echo "$line" > verify.txt
  if command -v sha256sum >/dev/null; then sha256sum -c verify.txt
  else shasum -a 256 -c verify.txt
  fi )

rm -rf resources/pi
mkdir -p "$tmp/out"
if [[ "$ext" == zip ]]; then
  unzip -q "$tmp/$archive" -d "$tmp/out"
else
  tar xzf "$tmp/$archive" -C "$tmp/out"
fi
# The archive holds one top-level pi/ directory; move it into place. The pi
# executable then sits next to the assets it reads at runtime (theme,
# export-html, docs, the image wasm), which is the layout pi expects.
src="$tmp/out/pi"
[[ -d "$src" ]] || { echo "unexpected archive layout: no pi/ directory" >&2; exit 1; }
mv "$src" resources/pi
chmod +x resources/pi/pi 2>/dev/null || true

echo "Bundled pi $VERSION ($PLATFORM) into resources/pi ($(du -sh resources/pi | cut -f1))"
