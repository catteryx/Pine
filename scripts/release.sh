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
# Signing and notarizing on this Mac: set macos.signingIdentity and
# macos.notarize in mygo.json, or pass -sign and the like through to the
# build, e.g.  scripts/release.sh -platform darwin/universal
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

go tool mygo build "$@" -upload

echo
echo "Draft release v$VERSION uploaded. Review it, then publish with:"
echo "  gh release edit v$VERSION --draft=false"
