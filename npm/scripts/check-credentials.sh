#!/usr/bin/env bash
# Fail if any publishable package source contains a plaintext credential.
#
# GitHub masks Actions secrets in logs, but npm tarballs are public and versions
# are immutable: a token that reaches a published tarball is exposed forever.
# This script never reads NPM_TOKEN and never prints the matched line -- only
# file names, so a real leaked value is not echoed into CI logs either.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

fail() { echo "::error::$*" >&2; exit 1; }

SOURCES=(npm/plugin npm/cli npm/platform npm/shared npm/README.md)

# -l lists file names only; -I skips binaries. The pattern is written so that
# this script's own source (under npm/scripts/) is not a scan target.
hits="$(grep -rIlE 'npm_[A-Za-z0-9]{30,}|_authToken[[:space:]]*=' "${SOURCES[@]}" || true)"
[ -z "$hits" ] || fail "plaintext npm credential in package sources: $hits"

if find . -path ./.git -prune -o -name '.npmrc' -print | grep -q .; then
  fail ".npmrc must never be committed (may contain an auth token)"
fi

echo "  ok  no plaintext credentials in package sources"
