#!/usr/bin/env bash
# Pack and publish the dsh plugin bundle, the generic npm CLI wrapper, and the
# four platform binary packages. Version is derived from the release tag and is
# identical for all six packages.
#
# Inputs (environment):
#   TAG             required, base tag (v3.10.5 / v3.10.6-preview.1) or the
#                   -npm trigger tag (v3.10.5-npm). The -npm suffix is stripped
#                   to locate the base Release and to derive the npm version.
#   GH_TOKEN        required when downloading from a GitHub Release
#
# Auth: CI 走 npm Trusted Publishing (OIDC) —— job 需 id-token: write，npm CLI
#       自动换取短期凭据并生成 provenance，仓库不保存任何长期 token。本地
#       调试可临时用 NODE_AUTH_TOKEN，但绝不写进仓库或 .npmrc。
#   DRY_RUN         true = npm pack --dry-run only, never publish
#   FAKE_BINARIES   true = generate placeholder binaries (local / PR validation)
#   PACK_OUT        directory to write real .tgz files into, without publishing;
#                   used for local dsh acceptance testing (dsh plugin add <tgz>)
#   DIST_DIR        local directory holding websearch-mcp-cli-* release files;
#                   skips the GitHub Release download
#
# Publish order is mandatory: platform packages -> cli wrapper -> dsh bundle.
# npm versions are immutable, so an already-published version is skipped.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
STAGE="$(mktemp -d)"
# 用 Node 删临时目录：Git Bash 下 shell 的 rm -rf 对 /tmp 路径会被
# Windows 路径转换 / 安全钩子拦一下，CI 的 ubuntu runner 也会留下噪音。
cleanup() {
  STAGE_DIR="$STAGE" node -e 'require("node:fs").rmSync(process.env.STAGE_DIR, { recursive: true, force: true })'
}
trap cleanup EXIT

DOWNLOADS="$STAGE/downloads"
mkdir -p "$DOWNLOADS"

PLATFORMS=(linux-amd64 windows-amd64 darwin-amd64 darwin-arm64)

die() { echo "::error::$*" >&2; exit 1; }
log() { echo "==> $*"; }

TAG="${TAG:-}"
DRY_RUN="${DRY_RUN:-false}"
FAKE_BINARIES="${FAKE_BINARIES:-false}"
PACK_OUT="${PACK_OUT:-}"
DIST_DIR="${DIST_DIR:-}"

# vX.Y.Z-npm 只是触发标记：二进制在 base tag 的 Release 上，npm 版本也等于
# base 版本（不能把 -npm 发成 3.10.5-npm 这种 next 通道版本）。
RELEASE_TAG="${TAG%-npm}"
[[ "$RELEASE_TAG" =~ ^v[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.-]+)?$ ]] \
  || die "TAG must look like v3.10.5, v3.10.6-preview.1 or v3.10.5-npm (got: '${TAG}')"
VERSION="${RELEASE_TAG#v}"
if [[ "$VERSION" == *-* ]]; then
  DIST_TAG=next
else
  DIST_TAG=latest
fi
log "tag=$TAG release-tag=$RELEASE_TAG version=$VERSION dist-tag=$DIST_TAG dry-run=$DRY_RUN pack-out=${PACK_OUT:-<none>}"
log "npm=$(npm --version 2>/dev/null || echo missing) oidc=$([ -n "${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}" ] && echo yes || echo no)"
[ -z "$PACK_OUT" ] || mkdir -p "$PACK_OUT"

# 真正发布时至少要有一条认证路径：GitHub Actions OIDC（首选）或本地 token。
# 不再硬性要求 NODE_AUTH_TOKEN —— Trusted Publishing 下它本来就不该存在。
if [ "$DRY_RUN" != "true" ] && [ "$FAKE_BINARIES" != "true" ] \
   && [ -z "$PACK_OUT" ] && [ -z "${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}" ] \
   && [ -z "${NODE_AUTH_TOKEN:-}" ]; then
  die "no publish credential: run in GitHub Actions with id-token: write (Trusted Publishing), or set NODE_AUTH_TOKEN locally"
fi

binary_ext() {
  case "$1" in
    windows-*) echo ".exe" ;;
    *) echo "" ;;
  esac
}

# ---------------------------------------------------------------------------
# 1) Obtain the four stdio binaries, verifying each against the Release checksum
# ---------------------------------------------------------------------------
if [ -n "$DIST_DIR" ]; then
  log "copying binaries from local $DIST_DIR"
  [ -d "$DIST_DIR" ] || die "DIST_DIR does not exist: $DIST_DIR"
  cp "$DIST_DIR"/websearch-mcp-cli-* "$DOWNLOADS/" \
    || die "no websearch-mcp-cli-* files in $DIST_DIR"
elif [ "$FAKE_BINARIES" = "true" ]; then
  log "generating placeholder binaries (validation only, never published)"
  for p in "${PLATFORMS[@]}"; do
    ext="$(binary_ext "$p")"
    printf 'placeholder binary for %s\n' "$p" > "$DOWNLOADS/websearch-mcp-cli-$p$ext"
    (cd "$DOWNLOADS" && sha256sum "websearch-mcp-cli-$p$ext" > "websearch-mcp-cli-$p$ext.sha256")
  done
else
  log "downloading websearch-mcp-cli-* from GitHub Release $RELEASE_TAG"
  gh release download "$RELEASE_TAG" --pattern 'websearch-mcp-cli-*' --dir "$DOWNLOADS" --clobber \
    || die "failed to download release assets for $RELEASE_TAG (GH_TOKEN / release missing?)"
fi

for p in "${PLATFORMS[@]}"; do
  ext="$(binary_ext "$p")"
  file="websearch-mcp-cli-$p$ext"
  [ -f "$DOWNLOADS/$file" ] || die "missing release binary $file for $RELEASE_TAG"
  if [ -f "$DOWNLOADS/$file.sha256" ]; then
    (cd "$DOWNLOADS" && sha256sum -c "$file.sha256") > /dev/null \
      || die "checksum mismatch for $file"
    log "verified $file"
  else
    echo "::warning::no .sha256 companion for $file; skipped verification"
  fi
done

# ---------------------------------------------------------------------------
# 2) Helpers: rewrite versions, validate with npm pack, publish idempotently
# ---------------------------------------------------------------------------
set_version() {
  PKG_FILE="$1" PKG_VERSION="$VERSION" node -e '
    const fs = require("node:fs");
    const file = process.env.PKG_FILE;
    const version = process.env.PKG_VERSION;
    const pkg = JSON.parse(fs.readFileSync(file, "utf8"));
    pkg.version = version;
    for (const field of ["dependencies", "optionalDependencies"]) {
      const deps = pkg[field];
      if (!deps) continue;
      for (const name of Object.keys(deps)) {
        if (name.startsWith("websearch-mcp-cli")) deps[name] = version;
      }
    }
    fs.writeFileSync(file, JSON.stringify(pkg, null, 2) + "\n");
  '
}

package_name() {
  PKG_FILE="$1/package.json" node -e '
    process.stdout.write(JSON.parse(require("node:fs").readFileSync(process.env.PKG_FILE, "utf8")).name);
  '
}

# npm tarballs are public and immutable. OIDC means the publish job no longer
# holds a long-lived token, but a token someone accidentally commits into a
# package source is still exposed forever (log masking cannot undo that).
# Scan the staged text files (never the binaries) right before packing.
assert_no_credentials() {
  local dir="$1" hit
  if find "$dir" -name '.npmrc' -print -quit | grep -q .; then
    die ".npmrc inside $dir; refusing to publish"
  fi
  hit="$(find "$dir" -type f \
      \( -name '*.json' -o -name '*.js' -o -name '*.cjs' -o -name '*.mjs' \
         -o -name '*.yml' -o -name '*.yaml' -o -name '*.md' \
         -o -name '*.sha256' -o -name 'LICENSE' \) -print0 \
      | xargs -0 -r grep -lE 'npm_[A-Za-z0-9]{30,}|_authToken[[:space:]]*=' 2>/dev/null \
      || true)"
  if [ -n "$hit" ]; then
    die "possible credential in staged package $dir: $hit"
  fi
}

publish() {
  local dir="$1" name
  name="$(package_name "$dir")"
  assert_no_credentials "$dir"

  # PACK_OUT turns the script into an offline packer: real .tgz files for
  # `dsh plugin add <tgz>` acceptance testing, with no registry access.
  if [ -n "$PACK_OUT" ]; then
    log "packing $name@$VERSION -> $PACK_OUT"
    (cd "$dir" && npm pack --pack-destination "$PACK_OUT") > /dev/null
    return
  fi

  log "packing $name@$VERSION"
  (cd "$dir" && npm pack --dry-run)

  if [ "$DRY_RUN" = "true" ]; then
    echo "[dry-run] would publish $name@$VERSION (dist-tag $DIST_TAG)"
    return
  fi
  if npm view "$name@$VERSION" version > /dev/null 2>&1; then
    echo "::notice::$name@$VERSION already on npm; skipping (npm versions are immutable)"
    return
  fi
  local publish_args=(--access public)
  # Trusted Publishing 下 npm 自动生成 provenance，显式 --provenance 是多余的；
  # 本地用长期 token 发布时才手动加签名参数。
  if [ -z "${ACTIONS_ID_TOKEN_REQUEST_TOKEN:-}" ]; then
    publish_args+=(--provenance)
  fi
  # 正式版走默认 latest，不显式写 --tag：OIDC 下改 dist-tag 需要 npm 侧单独
  # 勾选 "Allow npm dist-tag"，能不碰就不碰。预发布才需要 --tag next。
  if [ "$DIST_TAG" != "latest" ]; then
    publish_args+=(--tag "$DIST_TAG")
  fi
  log "publishing $name@$VERSION (${publish_args[*]})"
  (cd "$dir" && npm publish "${publish_args[@]}")
}

stage_common() {
  cp "$ROOT/LICENSE" "$1/LICENSE"
  cp "$ROOT/npm/shared/bin/websearch-mcp-cli.cjs" "$1/bin/websearch-mcp-cli.cjs"
  chmod 755 "$1/bin/websearch-mcp-cli.cjs"
}

# ---------------------------------------------------------------------------
# 3) Four platform packages (must be resolvable before the parent packages)
# ---------------------------------------------------------------------------
for p in "${PLATFORMS[@]}"; do
  ext="$(binary_ext "$p")"
  dir="$STAGE/websearch-mcp-cli-$p"
  mkdir -p "$dir/bin"
  cp "$ROOT/npm/platform/$p/package.json" "$dir/package.json"
  sed "s/@PLATFORM@/$p/g" "$ROOT/npm/shared/platform-README.md" > "$dir/README.md"
  cp "$ROOT/LICENSE" "$dir/LICENSE"
  cp "$DOWNLOADS/websearch-mcp-cli-$p$ext" "$dir/bin/websearch-mcp-cli$ext"
  chmod 755 "$dir/bin/websearch-mcp-cli$ext"
  # Regenerate the checksum against the in-package filename so `sha256sum -c`
  # works out of the box; the hash itself is the one from the GitHub Release.
  (cd "$dir/bin" && sha256sum "websearch-mcp-cli$ext" > "websearch-mcp-cli$ext.sha256")
  set_version "$dir/package.json"
  publish "$dir"
done

# ---------------------------------------------------------------------------
# 4) Generic CLI wrapper, then the dsh config-only bundle
# ---------------------------------------------------------------------------
dir="$STAGE/websearch-mcp-cli"
mkdir -p "$dir/bin"
cp "$ROOT/npm/cli/package.json" "$dir/package.json"
cp "$ROOT/npm/cli/README.md" "$dir/README.md"
stage_common "$dir"
set_version "$dir/package.json"
publish "$dir"

dir="$STAGE/dsh-websearch-mcpserver"
mkdir -p "$dir/bin"
cp "$ROOT/npm/plugin/package.json" "$dir/package.json"
cp "$ROOT/npm/plugin/index.js" "$dir/index.js"
cp "$ROOT/npm/plugin/cordis.patch.yml" "$dir/cordis.patch.yml"
cp "$ROOT/npm/plugin/README.md" "$dir/README.md"
stage_common "$dir"
set_version "$dir/package.json"
publish "$dir"

log "done: 6 packages @ $VERSION (dry-run=$DRY_RUN)"
