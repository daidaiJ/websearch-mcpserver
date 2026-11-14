#!/usr/bin/env bash
# Download the pinned PDFium builds used by the optional local PDF backend.
#
# Usage:
#   bash third_party/pdfium/fetch.sh                 # all supported targets
#   bash third_party/pdfium/fetch.sh win-x64         # one or more targets
#   GH_TOKEN=... bash third_party/pdfium/fetch.sh    # use the authenticated gh client
#
# Output: third_party/pdfium/<target>/<library> plus LICENSE, licenses/ and VERSION.
set -euo pipefail

PDFIUM_TAG="${PDFIUM_TAG:-chromium/8152}"
PDFIUM_REPO="bblanchon/pdfium-binaries"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CACHE="${PDFIUM_CACHE_DIR:-${TMPDIR:-/tmp}/websearch-pdfium}"
mkdir -p "$CACHE"

# target|asset|sha256|library path inside the archive
ENTRIES=(
  "win-x64|pdfium-win-x64.tgz|cee2b0e9990bc67c0721774932961b9dbf2766046c10eec925acd4cfba6fdfe3|bin/pdfium.dll"
  "linux-x64|pdfium-linux-x64.tgz|384ab5c51882408a143a21f9c14dcd2caf1f75280ae41c554d0489b46c3dba9f|lib/libpdfium.so"
  "linux-arm64|pdfium-linux-arm64.tgz|c6ed1a91234aede0cfafdc09c401b83bd44ac69deafc4599fda75d511c36403e|lib/libpdfium.so"
  "mac-x64|pdfium-mac-x64.tgz|acb975e637ffa1f41430205c62baacac70e34b60811e22fc0e0274dfcd601fcc|lib/libpdfium.dylib"
  "mac-arm64|pdfium-mac-arm64.tgz|30b0c3f55448840f0947b54bedbe35185bbaccbadd35b8cd960270d6fa6caf80|lib/libpdfium.dylib"
)

sha256_of() {
  if command -v sha256sum >/dev/null 2>&1; then
    sha256sum "$1" | awk '{print $1}'
  elif command -v shasum >/dev/null 2>&1; then
    shasum -a 256 "$1" | awk '{print $1}'
  else
    echo "sha256sum or shasum is required" >&2
    exit 1
  fi
}

download() {
  local asset="$1"
  if [ -n "${GH_TOKEN:-}" ] && command -v gh >/dev/null 2>&1; then
    if gh release download "$PDFIUM_TAG" --repo "$PDFIUM_REPO" \
      --pattern "$asset" --dir "$CACHE" --clobber; then
      return
    fi
    echo "gh download failed for $asset; falling back to curl" >&2
  fi
  local tag_url="${PDFIUM_TAG//\//%2F}"
  curl -fL --retry 5 --retry-all-errors --retry-delay 2 \
    -o "$CACHE/$asset" \
    "https://github.com/$PDFIUM_REPO/releases/download/$tag_url/$asset"
}

for entry in "${ENTRIES[@]}"; do
  IFS='|' read -r target asset want_hash libpath <<<"$entry"
  if [ "$#" -gt 0 ]; then
    case " $* " in
      *" $target "*) ;;
      *) continue ;;
    esac
  fi

  if [ -f "$CACHE/$asset" ] && [ "$(sha256_of "$CACHE/$asset")" = "$want_hash" ]; then
    echo "using cached $asset ($PDFIUM_TAG)"
  else
    echo "fetching $asset ($PDFIUM_TAG)"
    rm -f "$CACHE/$asset"
    download "$asset"
  fi

  got_hash="$(sha256_of "$CACHE/$asset")"
  if [ "$got_hash" != "$want_hash" ]; then
    echo "checksum mismatch for $asset: got $got_hash, want $want_hash" >&2
    exit 1
  fi

  extract="$CACHE/extract-$target"
  rm -rf "$extract"
  mkdir -p "$extract"
  tar -xzf "$CACHE/$asset" -C "$extract"

  dest="$ROOT/$target"
  rm -rf "$dest"
  mkdir -p "$dest"
  cp "$extract/$libpath" "$dest/$(basename "$libpath")"
  [ -f "$extract/LICENSE" ] && cp "$extract/LICENSE" "$dest/LICENSE"
  [ -d "$extract/licenses" ] && cp -R "$extract/licenses" "$dest/licenses"
  [ -f "$extract/VERSION" ] && cp "$extract/VERSION" "$dest/VERSION"
  echo "wrote $dest"
done
