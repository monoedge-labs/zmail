#!/bin/sh
# Builds the macOS app. Without MACOS_SIGNING_IDENTITY it is signed ad hoc,
# which runs on this Mac only.
#   MACOS_SIGNING_IDENTITY  "Developer ID Application: Name (TEAMID)"
#   NOTARY_PROFILE          a keychain profile made by `xcrun notarytool store-credentials`
set -e
cd "$(dirname "$0")/.."
if [ -z "$MACOS_SIGNING_IDENTITY" ]; then
  exec go tool mygo build -platform darwin/arm64 -skip-notarize
fi
if [ -z "$NOTARY_PROFILE" ]; then
  exec go tool mygo build -platform darwin/arm64 -sign "$MACOS_SIGNING_IDENTITY" -skip-notarize
fi
# mygo reads the notarization profile from mygo.json: add it for this run only.
tmp=$(mktemp); cp mygo.json "$tmp"; trap 'cp "$tmp" mygo.json; rm -f "$tmp"' EXIT
sed "s|\"out\": \"build\"|\"out\": \"build\", \"macos\": {\"notarize\": {\"keychainProfile\": \"$NOTARY_PROFILE\"}}|" "$tmp" > mygo.json
go tool mygo build -platform darwin/arm64 -sign "$MACOS_SIGNING_IDENTITY"
