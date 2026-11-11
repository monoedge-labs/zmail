#!/bin/sh
# Builds the macOS app. Without MACOS_SIGNING_IDENTITY it is signed ad hoc,
# which runs on this Mac only. Extra arguments go to `mygo build`:
# `scripts/release.sh -upload` makes the draft GitHub release, and needs both
# variables below because a public release is signed and notarized.
#   MACOS_SIGNING_IDENTITY  "Developer ID Application: Name (TEAMID)"
#   NOTARY_PROFILE          a keychain profile made by `xcrun notarytool store-credentials`
# The build also signs the update files (what installed apps update from) when
# it has the key of `mygo keygen`: from MYGO_UPDATER_PRIVATE_KEY, else from the
# key file in mygo's configuration directory. Without it they are skipped.
# .env.release at the root of the repository, which is never committed, sets
# these variables for this machine: with it a bare `scripts/release.sh` is the
# signed and notarized build. Its values take the place of the environment's.
set -e
cd "$(dirname "$0")/.."

if [ -f .env.release ]; then
  set -a
  . ./.env.release
  set +a
fi

keyfile="$HOME/Library/Application Support/mygo/update-keys/mygo-update.key"
if [ -z "$MYGO_UPDATER_PRIVATE_KEY" ] && [ -f "$keyfile" ]; then
  MYGO_UPDATER_PRIVATE_KEY=$(cat "$keyfile")
fi
export MYGO_UPDATER_PRIVATE_KEY
if [ -z "$MYGO_UPDATER_PRIVATE_KEY" ]; then
  echo "release.sh: no update signing key, update files skipped" >&2
fi

for arg in "$@"; do
  case $arg in -upload | --upload | -upload=true | --upload=true)
    if [ -z "$MACOS_SIGNING_IDENTITY" ] || [ -z "$NOTARY_PROFILE" ]; then
      echo "release.sh: -upload needs MACOS_SIGNING_IDENTITY and NOTARY_PROFILE" >&2
      exit 1
    fi ;;
  esac
done

if [ -z "$MACOS_SIGNING_IDENTITY" ]; then
  exec go tool mygo build -platform darwin/arm64 -skip-notarize "$@"
fi
if [ -z "$NOTARY_PROFILE" ]; then
  exec go tool mygo build -platform darwin/arm64 -sign "$MACOS_SIGNING_IDENTITY" -skip-notarize "$@"
fi
# mygo reads the notarization profile from mygo.json: add it for this run only.
tmp=$(mktemp); cp mygo.json "$tmp"; trap 'cp "$tmp" mygo.json; rm -f "$tmp"' EXIT
sed "s|\"out\": \"build\"|\"out\": \"build\", \"macos\": {\"notarize\": {\"keychainProfile\": \"$NOTARY_PROFILE\"}}|" "$tmp" > mygo.json
go tool mygo build -platform darwin/arm64 -sign "$MACOS_SIGNING_IDENTITY" "$@"
