#!/usr/bin/env bash
#
# Build and upload both Pine variants as drafts of the current version:
#
#   bundled-v<version>  Pine with the pi runtime embedded (zero setup, larger)
#   system-v<version>   Pine without it; runs the user's own pi (tiny)
#
# Each variant is its own GitHub release and its own update feed, using
# updates.tagPrefix, so the two never update into each other. Release them,
# both, in ascending version order: an app resolves its feed through the
# GitHub API, which lists releases newest first. Review both drafts and
# publish them to make them the versions apps update to.
#
#   scripts/release.sh                     # darwin/arm64, both variants
#   PLATFORM=darwin/universal scripts/release.sh
#   DRY_RUN=1 scripts/release.sh           # build locally, do not upload
#
# Requires:
#   - gh authenticated:  gh auth login
#   - the update signing key from `go tool mygo keygen`, in
#     MYGO_UPDATER_PRIVATE_KEY or the default location below
#
# Signing and notarizing never touch the repository: pass them in the
# environment and they are injected into mygo.json only for the build.
#
#   MACOS_SIGNING_IDENTITY="Developer ID Application: Name (TEAMID)" \
#   MACOS_NOTARY_PROFILE=notary \
#     scripts/release.sh
#
# Naming: by default both variants are the app "Pine" and share one identity
# and one settings directory, so switching variants keeps sessions. Their DMG
# files then have the same name, one per release. For distinct names and DMG
# files, pass a name for the bundled variant, e.g.:
#
#   BUNDLED_NAME="Pine Complete" scripts/release.sh
#
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="$(python3 -c 'import json;print(json.load(open("mygo.json"))["version"])')"
PLATFORM="${PLATFORM:-darwin/arm64}"
BUNDLED_NAME="${BUNDLED_NAME:-}"
BUNDLED_PREFIX="${BUNDLED_PREFIX:-bundled-v}"
SYSTEM_PREFIX="${SYSTEM_PREFIX:-system-v}"
KEY_FILE="$HOME/Library/Application Support/mygo/update-keys/mygo-update.key"

if [[ -z "${MYGO_UPDATER_PRIVATE_KEY:-}" && -f "$KEY_FILE" ]]; then
  MYGO_UPDATER_PRIVATE_KEY="$(cat "$KEY_FILE")"
fi
if [[ -z "${MYGO_UPDATER_PRIVATE_KEY:-}" ]]; then
  echo "No update key. Set MYGO_UPDATER_PRIVATE_KEY or run: go tool mygo keygen" >&2
  exit 1
fi
export MYGO_UPDATER_PRIVATE_KEY
export GH_TOKEN="${GH_TOKEN:-$(gh auth token)}"

# The bundled variant needs the pi runtime; fetch it if it is not there.
if [[ ! -x resources/pi/pi ]]; then
  echo "No resources/pi; fetching the pi runtime first."
  scripts/bundle-pi.sh
fi

# Keep the real configuration and resources, and put them back on exit.
ORIG_CONFIG="$(mktemp)"
cp mygo.json "$ORIG_CONFIG"
PI_SAVED=""
restore() {
  cp "$ORIG_CONFIG" mygo.json
  rm -f "$ORIG_CONFIG"
  if [[ -n "$PI_SAVED" && -d "$PI_SAVED" ]]; then
    rm -rf resources/pi
    mv "$PI_SAVED" resources/pi
  fi
}
trap restore EXIT

# write_config sets the update tag prefix (and optionally the app name) in
# mygo.json, and injects the signing identity and notary profile for this
# build only.
write_config() {
  local prefix="$1" name="${2:-}"
  PREFIX="$prefix" NAME="$name" python3 - <<'PY'
import json, os
c = json.load(open("mygo.json"))
c.setdefault("updates", {})["tagPrefix"] = os.environ["PREFIX"]
if os.environ.get("NAME"):
    c["name"] = os.environ["NAME"]
macos = c.get("macos", {})
if os.environ.get("MACOS_SIGNING_IDENTITY"):
    macos["signingIdentity"] = os.environ["MACOS_SIGNING_IDENTITY"]
if os.environ.get("MACOS_NOTARY_PROFILE"):
    macos["notarize"] = {"keychainProfile": os.environ["MACOS_NOTARY_PROFILE"]}
if macos:
    c["macos"] = macos
json.dump(c, open("mygo.json", "w"), indent=2)
open("mygo.json", "a").write("\n")
PY
}

DRY_RUN_BUILD=""
if [[ "${DRY_RUN:-}" == "1" ]]; then
  DRY_RUN_BUILD="1"
fi

run_build() {
  local out="$1"
  if [[ -n "$DRY_RUN_BUILD" ]]; then
    go tool mygo build -platform "$PLATFORM" -o "$out"
  else
    go tool mygo build -platform "$PLATFORM" -o "$out" -upload
  fi
}

echo "== bundled variant (prefix $BUNDLED_PREFIX) =="
write_config "$BUNDLED_PREFIX" "$BUNDLED_NAME"
run_build build/bundled

echo
echo "== system variant (prefix $SYSTEM_PREFIX) =="
# The system variant ships no runtime, so the app stays small and uses the
# pi the user has. Move the runtime aside for this build.
PI_SAVED="$(mktemp -d)/pi"
mv resources/pi "$PI_SAVED"
write_config "$SYSTEM_PREFIX"
run_build build/system

echo
if [[ -n "$DRY_RUN_BUILD" ]]; then
  echo "Dry run: built build/bundled and build/system, nothing uploaded."
  exit 0
fi
echo "Draft releases $BUNDLED_PREFIX$VERSION and $SYSTEM_PREFIX$VERSION uploaded."
echo "Review them, then publish both (in version order):"
echo "  gh release edit $SYSTEM_PREFIX$VERSION --draft=false"
echo "  gh release edit $BUNDLED_PREFIX$VERSION --draft=false"
