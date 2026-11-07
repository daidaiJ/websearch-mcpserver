# Release workflow maintenance

The three publishing matrices remain separate: Release binaries and MCPB bundles
use linux/amd64, windows/amd64, darwin/amd64 and darwin/arm64; Docker uses
linux/amd64 and linux/arm64. Publish the base version tag first, then the matching
`-registry` tag after its Release assets are available.

## Go modules

`go-module-cache.yml` maintains a module-only cache in the default branch scope.
GitHub permits a tag run to restore default-branch caches, but sibling tags cannot
restore each other's caches. The producer and Release consumers use exactly the
same `~/go/pkg/mod` path and `Linux-go-mod-v1-<hash(go.mod, go.sum)>` key. Keep these
inputs aligned: cache version metadata also includes the path and compression
format. Change the `v1` key namespace in both workflows if that contract changes.

The helper runs on `master` when the dependency files or helper workflow change,
and on Mondays and Thursdays (UTC). It restores the archive to keep it active
within GitHub's seven-day idle eviction window. Only a miss installs Go, runs
`go mod download` and saves the cache; documentation pushes do not start it. A
manual dispatch on the default branch can seed the cache after merging this
workflow. Dispatches on other branches skip the job.

Release jobs restore modules without uploading another cache for each tag.
`setup-go`'s combined module/build cache is disabled. `GOCACHE` is never shared
across the four compilation targets; each job compiles independently. The registry
job installs MCPB once from immutable commit
`3cdeb94c257c12388fea255e3d0a0151a225e269`, then uses the same executable for both
`pack` and `serverjson`.

Cache restores still transfer an archive. Scheduled runs may be delayed, and
storage pressure, a new dependency hash, or an unseeded/expired entry can cause a
miss. Release jobs retain ordinary Go download behavior on misses; caching does
not eliminate module-proxy/network failures. No download retries are added.

## CI build cache

`ci.yml` disables the `setup-go` built-in cache and restores the two caches
explicitly. The built-in cache keeps its exact key after a hit and never writes
back, which froze `~/.cache/go-build` at its first build: the coverage-instrumented
binaries that `-fuzz` requires were recompiled from scratch on every run (~70s).
The module cache reuses the producer key above at the same path, restore-only. The
build cache uses a rolling key — `go-build-<os>-<hash(go.sum)>-<run_number>` with a
restore-key prefix of `go-build-<os>-<hash(go.sum)>-` — so each master push appends
one entry that the next run restores. Saves happen only on master; pull requests
restore read-only, and a re-run skips its own existing key. The entry is roughly
700MB–1GB against the 10GB repository quota, so history is bounded by LRU. Keep the
key prefix stable when editing, and keep the save guarded by `github.ref ==
'refs/heads/master'` and the `cache-hit != 'true'` check.

`ci.yml` also skips docs-only pushes through `paths-ignore` (a YAML anchor shared
by the `push` and `pull_request` triggers). Patterns are deliberately broad so new
documents do not require workflow edits: `**.md` / `**.MD`, `docs/**`, `skills/**`,
and root-level `*.png` / `*.svg` / `*.jpg` / `*.jpeg` / `*.gif` / `*.webp` / `*.txt`.
Two constraints apply when extending the list. First, image globs must stay
root-anchored: `*` does not cross directories, while `cmd/assets/*.ico` and
`pkg/dashboard/web/logo-*.png` are `go:embed` compressor inputs — adding `**/*.png`
would let an icon change skip the rebuild that embeds it. Second, `**.md` covers the
`npm/*.md` files that `check-credentials.sh` scans, so a markdown-only push skips
that PR guard; it is acceptable because `npm-plugin.yml` reruns the same script
before publishing, so a plaintext credential still cannot reach a published tarball.
GitHub cannot re-include a path under `paths-ignore` (negation requires the `paths`
form), and path filtering is per file rather than per job, so the `lint`, `actionlint`
and `npm-plugin-check` jobs share this filter and cannot be skipped selectively.
Those three finish well inside the test job's runtime; they are not on the critical
path. Skipped runs leave their checks `Pending`, which matters only if branch
protection is later enabled on master.

`docker.yml` caches builds with `type=registry` against
`ghcr.io/<repo>:buildcache`. `type=gha` never hit here — tag runs cannot see each
other's caches and master does not build images — while still consuming about
2.4GB of the repository cache quota across 69 buildkit entries. The registry
backend is shared across tags and leaves the Actions cache quota to Go.

## Docker tag filters and validation

Docker accepts `v*`, then excludes `v*-registry` and `v*-preview*` at trigger time.
The job-level exclusion remains as defense in depth. Other existing tags,
including `v1.2.3-rc.1` and `v1.2.3-beta.1`, retain their current Docker behavior.
Release continues to handle both base and registry tags.

Run `actionlint .github/workflows/*.yml` after workflow edits. Check stable,
preview, registry, rc and beta tag examples; check the shared module cache path,
key and default-branch scope; check the CI build-cache key prefix and its
master-only save condition; and preserve the four binary/MCPB targets. A normal
pull request does not execute these tag-triggered publishing workflows. Static
validation does not prove a GitHub cache hit, remote runner compilation, or a
successful registry/Docker publication. Verify the cache scope/hits and final
artifacts during the next authorized Release; no test tag or publication is
required just to validate this configuration change.

References: [GitHub cache scope and eviction](https://docs.github.com/en/actions/reference/workflows-and-actions/dependency-caching),
[separate cache restore/save](https://github.com/actions/cache/blob/v4/restore/README.md),
[MCPB pinned source](https://github.com/daidaiJ/mcpb-tool-cli/tree/3cdeb94c257c12388fea255e3d0a0151a225e269).
