#!/usr/bin/env bash
#
# Build Pine locally and upload its installers and signed updates to the
# GitHub release of the current version, as a draft. Review and publish the
# draft to make it the version apps update to.
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
#     scripts/release.sh -platform darwin/universal
#
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="$(python3 -c 'import json;print(json.load(open("mygo.json"))["version"])')"
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

# Inject the signing identity and notary profile for this build only.
if [[ -n "${MACOS_SIGNING_IDENTITY:-}" || -n "${MACOS_NOTARY_PROFILE:-}" ]]; then
  cp mygo.json mygo.json.bak
  trap 'mv -f mygo.json.bak mygo.json' EXIT
  python3 - <<'PY'
import json, os
c = json.load(open("mygo.json"))
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
fi

go tool mygo build "$@" -upload

echo
echo "Draft release v$VERSION uploaded. Review it, then publish with:"
echo "  gh release edit v$VERSION --draft=false"
