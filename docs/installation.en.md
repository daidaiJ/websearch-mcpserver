# Installation & Operations

[English](installation.en.md) | [中文](installation.md)

## Contents

- [Installation](#installation)
  - [Binary Download](#binary-download)
  - [Docker](#docker)
  - [Build from Source](#build-from-source)
- [Register MCP Client](#register-mcp-client)
  - [Claude Code](#claude-code)
  - [Qwen Code](#qwen-code)
  - [Cursor / Other Clients](#cursor--other-clients)
- [stdio CLI](#stdio-cli)
- [npm / dsh Plugin Distribution](#npm--dsh-plugin-distribution)
- [Agent Quick Deploy](#agent-quick-deploy)
- [Operations](#operations)
  - [Subcommands](#subcommands)
  - [Windows Auto-Start](#windows-auto-start)
  - [MCP Hooks Auto Start/Stop](#mcp-hooks-auto-startstop)
  - [Background Service](#background-service)
  - [Health Check & Admin Endpoints](#health-check--admin-endpoints)
- [Troubleshooting](#troubleshooting)
  - [Autostart fails with 0x800704C7 (antivirus / SmartScreen blocking)](#autostart-fails-with-0x800704c7-antivirus--smartscreen-blocking)

---

## Installation

### Binary Download

Download from the [Release page](https://github.com/daidaiJ/websearch-mcpserver/releases); every artifact ships with a matching `.sha256` checksum.

**Platform archive (recommended)**: one archive per platform, holding both binaries, the config samples, LICENSE, the READMEs, `skills/` (the WebSearch skill plus the Everything setup guide) and the optional PDFium dynamic library — no need to download file by file:

| Platform | Archive |
|----------|---------|
| Linux x86_64 | `websearch-mcpserver-linux-amd64.tar.gz` |
| Windows x86_64 | `websearch-mcpserver-windows-amd64.zip` |
| macOS Intel | `websearch-mcpserver-darwin-amd64.tar.gz` |
| macOS Apple Silicon | `websearch-mcpserver-darwin-arm64.tar.gz` |

Layout inside (`<platform>` is e.g. `windows-amd64`; both binaries carry `.exe` on Windows):

```
websearch-mcpserver-<platform>/
├── websearch-mcpserver[.exe]   # HTTP daemon (listens on /mcp after start)
├── websearch-mcp-cli[.exe]     # stdio CLI
├── config.example.yaml
├── dashboard.example.yaml
├── LICENSE
├── README.MD / README.EN.md
├── skills/                     # websearch-mcp + everything-http-server
└── third_party/
    └── pdfium/
        └── <arch>/             # e.g. win-x64, linux-x64, mac-arm64
            ├── pdfium.dll / libpdfium.so / libpdfium.dylib
            ├── LICENSE
            └── licenses/
```

After unpacking, run `./websearch-mcpserver[.exe] start` as in [Quick Start](../README.EN.md#quick-start); skill setup is covered under [Skill / Application/Project Attribution](#skill--applicationproject-attribution).

#### Optional PDFium local text backend

The platform archive ships one standard PDFium library for the current platform under `third_party/pdfium/<arch>/`. It is not loaded at startup; the first local PDF parse opens it on demand. When `pdf_parser.pdfium.enabled` is omitted the library is auto-detected: `fallback` activates when it exists, and a missing or unloadable library falls back to the pure Go parser with no behavior change. To prefer PDFium for every local PDF:

```yaml
pdf_parser:
  pdfium:
    mode: primary      # fallback (default) | primary
    # enabled: true    # optional: force on; false turns it off; omitted = auto-detect
```

- Platform-archive users only need to unpack; no download script or separate library fetch is required. `third_party/pdfium/fetch.sh` exists for release CI and local development.
- Supported: Windows x86_64, Linux x86_64/ARM64, macOS Intel/Apple Silicon. The library architecture must match the process. A `PDF parser: pdfium` line in the result means it was actually used; the startup log prints the configured library path.
- A relative `path` is searched in the executable directory, its `third_party/pdfium/<arch>/`, the config directory, then its `third_party/pdfium/<arch>/`. A manually replaced library at the executable root overrides the bundled copy.
- Bare-binary downloads have no archive directory; fetch a matching build from [pdfium-binaries](https://github.com/bblanchon/pdfium-binaries) and place it in that layout, or use the platform archive.
- PDFium is BSD-3-Clause; the archive includes `LICENSE` and `licenses/`. The main program stays pure Go, CGO-free and MIT.
- The GHCR image does not bundle the library; copy or mount the matching architecture into `/app/third_party/pdfium/linux-x64/` when needed.

**Single-file download** (when you only need one binary):

**HTTP daemon** (`start`, then listen on `/mcp`):

| Platform | File |
|----------|------|
| Linux x86_64 | `websearch-mcpserver-linux-amd64` |
| Windows x86_64 | `websearch-mcpserver-windows-amd64.exe` |
| macOS Intel | `websearch-mcpserver-darwin-amd64` |
| macOS Apple Silicon | `websearch-mcpserver-darwin-arm64` |

> **Split release matrices**: GitHub Release only ships the four platforms above (linux/windows amd64 + darwin amd64/arm64). There is no linux-arm64 binary — use the GHCR image below. Windows ARM64 is not published. MCP Registry `.mcpb` bundles match these four platforms.
>
> **Tagging** (two steps, do not push together): first push a plain version tag (e.g. `v3.4.0`) to publish the GitHub Release **and** the GHCR image; **after that Release is ready**, separately push a `-registry` suffix tag (e.g. `v3.4.0-registry`) to publish to the MCP Registry only (packs `.mcpb` from the existing `v3.4.0` Release). The follow-up registry tag stays on the same commit.

**stdio CLI** (spawned by the MCP client, no HTTP port):

| Platform | File |
|----------|------|
| Linux x86_64 | `websearch-mcp-cli-linux-amd64` |
| Windows x86_64 | `websearch-mcp-cli-windows-amd64.exe` |
| macOS Intel | `websearch-mcp-cli-darwin-amd64` |
| macOS Apple Silicon | `websearch-mcp-cli-darwin-arm64` |

### Docker

Official images are on GHCR as a **linux/amd64 + linux/arm64** manifest (Apple Silicon / ARM servers can pull directly):

```bash
docker pull ghcr.io/daidaij/websearch-mcpserver:latest
# or pin: ghcr.io/daidaij/websearch-mcpserver:3.6.0
```

```yaml
# docker-compose.yml
services:
  websearch:
    image: ghcr.io/daidaij/websearch-mcpserver:latest
    restart: always
    volumes:
      - ./config.yaml:/app/config.yaml               # main config (required)
      - ./dashboard.yaml:/app/dashboard.yaml         # control center config (recommended)
      - ./data:/app/data                             # control center data: telemetry DB + secrets overlay (recommended)
    ports:
      - "8338:8338"
```

**Port**: the image exposes **8338 only** (`EXPOSE 8338`) — MCP (`/mcp`) and the console (`/dashboard/`) share it. The v3.6.0 control center **adds no new port**, so no change to your port mappings. With `network_mode: host`, make sure port 8338 is free on the host.

**Volume mappings** (`/app` is the image workdir; relative paths in the config resolve against it):

| Host | Container | Purpose | What breaks without it |
|---|---|---|---|
| `./config.yaml` | `/app/config.yaml` | main config (required) | the bundled `config.example.yaml` is used; changing config means rebuilding the image |
| `./dashboard.yaml` | `/app/dashboard.yaml` | control center settings: admin password, allowed networks, quotas, branding | the first `start` auto-generates a `dashboard.yaml` with a **random password**; recreating the container loses the password, allowlist, quotas and branding |
| `./data` | `/app/data` | telemetry DB `dashboard.db` + secrets overlay `dashboard-secrets.json` | call history, quota usage and API keys written through the WebUI are wiped when the container is recreated |
| `./cache` (optional) | `/app/cache` | search cache (when `cache.enabled: true`) | cache is wiped when the container is recreated |
| `./fetchdata` (optional) | `/app/fetchdata` | page bodies saved by cleanfetch | saved files are wiped when the container is recreated |

> The control-center config can live elsewhere too: point `WEBSEARCH_DASHBOARD_CONFIG` or `dashboard.config_path` at another path (see [dashboard.en.md](dashboard.en.md)).

**Console access in Docker**: write operations (settings / API keys / restart / cache clear / quotas) require the request source to be **loopback inside the container**, but with bridge networking plus a published port the source is the bridge gateway (e.g. `172.17.0.1`) — so **the console is read-only in the default compose setup**: pages, call history and metrics all work, while any settings action returns 403.

- Read-only: to open the console from the host browser, add the bridge subnet to the allowlist (the default bridge sits inside `172.16.0.0/12`; for a custom network use its actual subnet, kept as narrow as possible):

  ```yaml
  # dashboard.yaml
  dashboard:
    allowed_networks: ["172.16.0.0/12"]
  ```

- Write operations (Linux hosts): switch to `network_mode: host` — the container shares the host network stack, so a host browser hitting `127.0.0.1:8338` is seen as loopback and writes are accepted (the `ports` mapping is ignored in that mode).
- Leaving the network alone works too: edit `config.yaml` / `dashboard.yaml` on the host and restart the container — write operations are convenience, the files are the source of truth.

Build locally:

```bash
git clone --depth 1 https://github.com/daidaiJ/websearch-mcpserver.git
cd websearch-mcpserver && docker build -t websearch:v1 .
```

### Build from Source

```bash
go build -o websearch ./cmd/
go build -o websearch-mcp-cli ./cmd/cli
# With version injection
go build -ldflags="-X main.version=v1.0.0" -o websearch ./cmd/
go build -ldflags="-X main.version=v1.0.0" -o websearch-mcp-cli ./cmd/cli
```

---

## Register MCP Client

After the service starts (HTTP daemon listens on `http://localhost:8338/mcp`), register it with your client. Both **Claude Code** and **Qwen Code** configurations are shown below.

> **Auth**: if `auth_token` is set (or env `WEBSEARCH_TOKEN`), clients must send `Authorization: Bearer <token>` to `/mcp`, otherwise 401. With no token, no header is needed.

### Claude Code

**CLI**:

```bash
claude mcp add --transport http websearch http://localhost:8338/mcp
# when auth_token is set:
claude mcp add --transport http websearch http://localhost:8338/mcp --header "Authorization: Bearer <token>"
```

**JSON** (`.claude.json` / `mcp.json`):

```json
{
  "mcpServers": {
    "websearch": {
      "type": "http",
      "url": "http://localhost:8338/mcp",
      "headers": {
        "Authorization": "Bearer <token>"
      }
    }
  }
}
```

### Qwen Code

> Qwen Code uses `httpUrl` for HTTP transport (not Claude Code's `type`+`url`), and `command`+`args` for stdio.

**CLI** (writes to user-level `~/.qwen/settings.json`; use `-s project` for project scope):

```bash
qwen mcp add --transport http websearch http://localhost:8338/mcp
```

**JSON** (`~/.qwen/settings.json` or project `.qwen/settings.json`):

```jsonc
{
  "mcpServers": {
    "websearch": {
      "httpUrl": "http://localhost:8338/mcp",
      "headers": {
        "Authorization": "Bearer <token>"
      }
    }
  }
}
```

> Omit `headers` when `auth_token` is not configured.

Qwen Code common flags:

| Flag | Description |
|------|-------------|
| `-s, --scope` | `user` (default, global) / `project` (current project only) |
| `--timeout` | Tool-call timeout in ms, default 600000 (10 min) |
| `--trust` | Trust the server, skip tool-call confirmations |
| `-e KEY=value` | Inject environment variables (e.g. `-e BAIDU_SK=xxx`) |

Restart Qwen Code (`/exit` then relaunch) for changes to take effect; check connection status with `/mcp`.

### Skill / Application/Project Attribution

Application identity uses standard MCP clientInfo. All five tools accept an optional per-call `project` argument over HTTP and stdio. It is a statistics label only: it does not change search scope, authentication, routing or quotas. Existing clients may omit it; new projects do not require their own client configuration.

Install the bundled [websearch-mcp skill](../skills/websearch-mcp/SKILL.md) in your host's supported skill location, or import the whole `skills/websearch-mcp` directory through your existing skill manager. Its `agents/openai.yaml` declares the local `http://127.0.0.1:8338/mcp` dependency. Hosts supporting this mechanism can use it; other hosts still need their usual one-time MCP connection. A dependency does not grant authorization. Change the URL for other deployments, not a global project label.

The skill uses only an explicit current workspace/project name or an already confirmed project root. Keep the same nickname within a task and reconsider it when switching workspaces. Labels accept up to 64 letters, digits, spaces and `._-`, including Chinese; paths, URLs, credentials and conversation identifiers are not labels. Omit unknown projects instead of guessing from a query or asking users solely to fill statistics. Example:

```json
{"url":"https://example.com","project":"Example"}
```

Refresh the tool list after upgrading, use the skill to fetch a public page, and inspect application/project in the collapsed call row. The skill omits `project` when an older tool schema does not expose it. Missing skill/context or omitted arguments remain unlabeled. This is self-reported attribution, not OS-verified identity, and cannot reconstruct old untagged calls.

Existing `X-WebSearch-Project` / `X-WebSearch-Client` headers remain available for advanced clients. A valid per-call `project` wins over the project header; missing/invalid labels fall back. Generic client names cannot identify their host. See [dashboard attribution rules](dashboard.en.md#search-call-usage) for precedence.

### Cursor / Other Clients

```json
{
  "mcpServers": {
    "websearch": {
      "type": "http",
      "url": "http://localhost:8338/mcp"
    }
  }
}
```

---

## stdio CLI

To skip the HTTP daemon, download `websearch-mcp-cli-*` and let the client spawn the process (stdin/stdout by default). With no config file it uses in-memory defaults (`mode: engine`).

```bash
# Optional: write an example config
./websearch-mcp-cli init
# Or a specific path
./websearch-mcp-cli -c ~/.config/websearch/config.yaml init
```

**Claude Code**:

```json
{
  "mcpServers": {
    "websearch": {
      "command": "/path/to/websearch-mcp-cli",
      "args": ["-c", "/path/to/config.yaml"]
    }
  }
}
```

**Qwen Code**:

```bash
qwen mcp add websearch /path/to/websearch-mcp-cli -c /path/to/config.yaml
```

```jsonc
{
  "mcpServers": {
    "websearch": {
      "command": "/path/to/websearch-mcp-cli",
      "args": ["-c", "/path/to/config.yaml"]
    }
  }
}
```

Omit `args` when running with defaults. Logs go to stderr and `websearch.log` under the config directory so they do not corrupt JSON-RPC on stdout.

CLI commands: no args = stdio; `init` writes an example config; `version` prints the version.

Config shares the same YAML as HTTP; see [configuration.en.md#stdio-cli-configuration-notes](configuration.en.md#stdio-cli-configuration-notes) for differences such as `port`.

---

## npm / dsh Plugin Distribution

Use npm if you do not want to manage Release binaries by hand. Once a version's Release assets are ready, a separate `vX.Y.Z-npm` tag publishes six packages at the base version (`v3.10.5-npm` -> `3.10.5`). Four platform packages are selected automatically through `optionalDependencies` by `os` / `cpu`; there are **no postinstall / prepare scripts**, so pnpm 10+ `allowBuilds` gating and install-time downloads are both avoided.

| Package | Purpose |
|---|---|
| `dsh-websearch-mcpserver` | DeepSeek Harness config-only Cordis bundle (plugin shell + launcher shim) |
| `websearch-mcp-cli` | Generic stdio CLI wrapper for any MCP client |
| `websearch-mcp-cli-<os>-<arch>` x 4 | Prebuilt Go binary + `.sha256` |

### Install

**dsh (web profile)**:

```bash
dsh plugin --profile web add dsh-websearch-mcpserver
dsh --profile web --dump-config       # the mcp-websearch row should appear
dsh web                               # web bundles have no HMR; restart required
```

If a mirror returns 404, use the official registry: `dsh plugin --profile web add dsh-websearch-mcpserver --registry=https://registry.npmjs.org`. Tools appear as `mcp__websearch__smartsearch` / `academicsearch` / `cleanfetch` / `pdf_parser` / `file_search`. Remove with `dsh plugin --profile web remove dsh-websearch-mcpserver`.

**Other MCP clients**:

```bash
npm i -g websearch-mcp-cli
```

```json
{
  "mcpServers": {
    "websearch": {
      "command": "websearch-mcp-cli",
      "env": { "WEBSEARCH_MCP_CONFIG": "/abs/path/to/config.yaml" }
    }
  }
}
```

The same `mcpServers` block in `.mcp.json` / `~/.dsh/mcp.json` gives dsh the same tools without installing the plugin.

### Personalize the config (important, do not skip)

With no config file the Go binary runs in-memory defaults (`mode: engine`): search works, but the zero-key hosted-MCP cascade, multi-provider key pools, academic weighting, fetch fallbacks, proxy handling, language and result budgets are all off. **A fresh install gets you "it runs", not the best experience.**

Either read the docs yourself — tuning path in [HUMAN_GUIDE.en.md](HUMAN_GUIDE.en.md), full reference in [configuration.en.md](configuration.en.md), plus `config.example.yaml` — or have your agent do it (recommended). An npm install ships only the Go binary, so the prompt below is self-contained: it names the repository and the exact doc URLs, safe to paste straight from the GitHub page. Drop the fetch step if you already work inside a checkout.

```text
I installed websearch-mcp-cli / dsh-websearch-mcpserver via npm and need a
personalized config for it.
The repository is https://github.com/daidaiJ/websearch-mcpserver ; the config
docs live only there and I may not have the source on this machine. First check
whether the current directory is a checkout of that repo: if so read it directly;
otherwise clone it shallowly with
git clone --depth 1 https://github.com/daidaiJ/websearch-mcpserver to a temp dir,
or fetch these raw files:
  https://raw.githubusercontent.com/daidaiJ/websearch-mcpserver/master/docs/AGENT_GUIDE.en.md
  https://raw.githubusercontent.com/daidaiJ/websearch-mcpserver/master/docs/configuration.en.md
  https://raw.githubusercontent.com/daidaiJ/websearch-mcpserver/master/config.example.yaml
Then ask me about my OS, network (do I need a proxy), which API keys I have, and
my main use case (Chinese web / academic papers / code docs / deep research).
Generate config.yaml at an absolute path on my machine
(Windows: %USERPROFILE%\.config\websearch\config.yaml;
 otherwise ~/.config/websearch/config.yaml) with only the necessary overrides,
explain each choice, skip engines I do not use, and never write secrets in plain text.
Finally tell me the exact absolute path to put in WEBSEARCH_MCP_CONFIG and how to
verify it took effect.
```

Then export an **absolute** path before launching dsh:

```bash
export WEBSEARCH_MCP_CONFIG=/abs/path/to/config.yaml
dsh web
```

The shim forwards it as `-c <path>`. The Go binary's native `WEBSEARCH_CONFIG` also works but has higher priority than `-c`; do not point both at different files. A missing `WEBSEARCH_MCP_CONFIG` target fails loudly instead of silently falling back to defaults.

Tune first: `mode` (`mcphub` zero key / `apipool` with keys / `engine` fully local), `mcp_hub.preset` and `fallback_engines`, `sk_list`, system proxy, `language`, `fetch_mode`, academic engine switches, result count and timeout budgets, `cache`, `dashboard.enabled`. The full field list lives in [configuration.en.md](configuration.en.md); do not copy someone else's config verbatim.

### Platform matrix and verification

Same four platforms as the GitHub Release: `linux-amd64`, `windows-amd64`, `darwin-amd64`, `darwin-arm64`. `linux-arm64` uses the GHCR image; `windows-arm64` needs a manually downloaded Release binary plus `WEBSEARCH_MCP_CLI_BINARY=/abs/path/to/websearch-mcp-cli`.

The hash in each platform package's `bin/websearch-mcp-cli*.sha256` matches the checksum attached to the same tag's GitHub Release:

```bash
cd $(npm root -g)/websearch-mcp-cli-linux-amd64/bin && sha256sum -c websearch-mcp-cli.sha256
```

### Non-web profiles / custom args

`cordis.patch.yml` builds the shim path from `$DSH_HOME/profiles/web/`. For another profile, `export DSH_PROFILE=<name>` before launching dsh, or override the whole row in that profile's own `cordis.patch.yml` (later layers **replace the entire config object**, so restate every key):

```yaml
- insert:
    - id: mcp-websearch
      name: '@deepseek-ai/dsh-mcp-client'
      config:
        serverName: websearch
        transport: stdio
        command: !!js process.execPath
        args:
          - !!js ((process.env.DSH_HOME || ((process.env.USERPROFILE || process.env.HOME) + '/.dsh')) + '/profiles/my-profile/node_modules/dsh-websearch-mcpserver/bin/websearch-mcp-cli.cjs')
          - '-c'
          - '/abs/path/to/config.yaml'
```

### Permissions and trust boundary

The npm shell is only a launcher shim: no network, no file writes. The Go binary **does** make network requests (search engines / web fetch / optional hosted MCP upstreams), **does** write a SQLite cache and logs, and `file_search` **does** read a local directory index. SSRF / DNS rebinding guards, a directory allowlist and privacy defaults (no raw queries or secrets in the database) are built in; review the allowlist before enabling `file_search`. The stdio child logs to stderr, opens no port and needs no `auth_token`.

### Troubleshooting

| Symptom | Fix |
|---|---|
| `dsh plugin add` returns 404 | Mirror has not synced; retry with `--registry=https://registry.npmjs.org` |
| Installed but no tools | `dsh --profile web --dump-config` to check the `mcp-websearch` row, then restart `dsh web` |
| `platform package ... is not installed` | Unsupported platform or incomplete mirror; reinstall from the official registry or set `WEBSEARCH_MCP_CLI_BINARY` |
| Poor results / no academic results | Usually the zero-config `mode: engine` default — personalize the config as above |
| `WEBSEARCH_MCP_CONFIG` ignored | Check whether higher-priority `WEBSEARCH_CONFIG` is also set, and that the path is absolute |

Maintainer notes on package layout, publish order and local dry-runs: [npm/README.md](../npm/README.md).

---

## Agent Quick Deploy

Agents can use these steps for automated installation:

```bash
# 1. Download latest binary
#    Linux:
curl -sL https://api.github.com/repos/daidaiJ/websearch-mcpserver/releases/latest \
  | grep "browser_download_url.*linux-amd64\"" \
  | cut -d '"' -f 4 \
  | xargs curl -sL -o /usr/local/bin/websearch-mcpserver
chmod +x /usr/local/bin/websearch-mcpserver

#    macOS (Intel):
# curl -sL https://api.github.com/repos/daidaiJ/websearch-mcpserver/releases/latest \
#   | grep "browser_download_url.*darwin-amd64\"" \
#   | cut -d '"' -f 4 \
#   | xargs curl -sL -o /usr/local/bin/websearch-mcpserver
# chmod +x /usr/local/bin/websearch-mcpserver

#    macOS (Apple Silicon):
# curl -sL https://api.github.com/repos/daidaiJ/websearch-mcpserver/releases/latest \
#   | grep "browser_download_url.*darwin-arm64\"" \
#   | cut -d '"' -f 4 \
#   | xargs curl -sL -o /usr/local/bin/websearch-mcpserver
# chmod +x /usr/local/bin/websearch-mcpserver

#    Windows (PowerShell):
# $release = Invoke-RestMethod https://api.github.com/repos/daidaiJ/websearch-mcpserver/releases/latest
# $asset = $release.assets | Where-Object { $_.name -match 'windows-amd64' }
# Invoke-WebRequest -Uri $asset.browser_download_url -OutFile "C:\tools\websearch-mcpserver.exe"

# 2. Write minimal config (zero keys needed)
# You may skip this step: the first `start` auto-generates a preset config.yaml
# (identical to config.example.yaml) next to the executable. With an explicit
# `-c` path, no file is auto-created and a missing file is an error.
mkdir -p ~/.config/websearch
cat > ~/.config/websearch/config.yaml << 'EOF'
port: 8338
mode: engine          # use mcphub for the zero-key hosted-MCP cascade
EOF

# 3. Start and register (see "Register MCP Client" above)
./websearch-mcpserver start
```

> **What "zero config" means**: `install` / `websearch-mcp-cli init` / first `start` (without `-c`) writes a preset `config.yaml` identical to `config.example.yaml`; edit that file for port/keys/mode. The daemon **never** runs on invisible in-memory defaults without a config file.

Windows auto-start (optional): run `websearch-mcpserver.exe install` after download.

---

## Operations

### Subcommands

HTTP daemon binary `websearch-mcpserver`:

| Command | Description |
|---------|-------------|
| `start` | Start service (ref=1 or ref+1) |
| `stop` | Decrease reference (ref-1, graceful exit at zero) |
| `kill` | Force terminate (ignores reference count) |
| `status` | Show status, port, reference count |
| `version` | Show version (injected at build time, default `dev`) |
| `install` | Install Windows auto-start |
| `uninstall` | Uninstall Windows auto-start |

CLI flags: `-c, --config` to specify config file path.

### Windows Auto-Start

```bash
./websearch-mcpserver.exe install   # Creates VBS script + Startup folder shortcut
./websearch-mcpserver.exe uninstall # Removes shortcut
```

Uses COM API (ole32.dll) to create shortcuts, no PowerShell dependency.

### MCP Hooks Auto Start/Stop

Recommended: use Hooks for automatic session lifecycle (Qwen Code example):

```json
{
  "hooks": {
    "SessionStart": [{ "matcher": "*", "hooks": [{ "type": "command", "command": "/path/to/websearch-mcpserver start", "timeout": 10000 }] }],
    "SessionEnd":   [{ "matcher": "*", "hooks": [{ "type": "command", "command": "/path/to/websearch-mcpserver stop",  "timeout": 10000 }] }]
  }
}
```

Reference counting ensures multi-session sharing; auto-exits when all sessions close.

### Background Service

| Platform | Solution |
|----------|----------|
| Windows | `nssm` register as Windows Service |
| Linux | systemd `Restart=always` |
| macOS | launchd plist |

With a background service, `start` once only — no hooks needed.

#### Linux Auto-Start (systemd)

```bash
# 1. Create systemd service file
sudo tee /etc/systemd/system/websearch.service << 'EOF'
[Unit]
Description=WebSearch MCP Server
After=network.target

[Service]
Type=simple
User=YOUR_USERNAME
ExecStart=/usr/local/bin/websearch-mcpserver start
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# 2. Enable and start
sudo systemctl daemon-reload
sudo systemctl enable websearch
sudo systemctl start websearch

# 3. Check status
sudo systemctl status websearch
```

#### macOS Auto-Start (launchd)

```bash
# 1. Create plist file
tee ~/Library/LaunchAgents/com.websearch.server.plist << 'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.websearch.server</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/websearch-mcpserver</string>
        <string>start</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>WorkingDirectory</key>
    <string>/tmp</string>
</dict>
</plist>
EOF

# 2. Load and start
launchctl load ~/Library/LaunchAgents/com.websearch.server.plist
launchctl start com.websearch.server

# 3. Check status
launchctl list | grep websearch
```

### Health Check & Admin Endpoints

| Item | Description |
|------|-------------|
| **Health Check** | `GET /__admin/health` — returns `{"ref_count": N, "message": "running"}`, remotely accessible |
| **Admin Endpoints** | `GET /__admin/status` · `POST /__admin/refcount` · `POST /__admin/shutdown` — local access only |
| **PID File** | `.websearch.pid` (JSON), in config file directory or executable directory |
| **Log File** | `websearch.log`, same directory, size-rotated (default 1MB, 1 day retention) |
| **Cache** | SQLite WAL mode, 6h expiry (based on last hit), 30min scheduled cleanup |

---

## Troubleshooting

| Problem | Solution |
|---------|----------|
| Tools unavailable after start | Check `mode` and corresponding keys; `cleanfetch` is always registered; URL fetching requires `cleanfetch.enabled: true` |
| Academic search timeout | Check `network` setting; overseas engines need proxy (auto-detected by default) |
| Port in use | `status` to check if already running, or `kill` then restart |
| Stale cache results | Cache auto-expires after 6h, or delete `cache.storage_path` file and restart |
| Docker container exits immediately | Confirm `config.yaml` is mounted, check log output |
| Console settings return 403 under Docker | Write operations require a loopback source; bridge networking is read-only — see the [Docker](#docker) section |
| Process still running after stop | Wait up to 10s; if still running use `kill` to force terminate |
| No results or rate-limited | Check `rate_limit` config (default 3/s, 60/min); Google etc. auto-skipped when proxy unavailable |

### Autostart fails with 0x800704C7 (antivirus / SmartScreen blocking)

**Symptom**: the autostart script (autostart.vbs / the scheduled task created by the `install` subcommand) reports error code `0x800704C7`, pointing at the `WshShell.Run` line that launches the exe, e.g.:

```vb
WshShell.Run """D:\Programs\websearch\websearch-mcpserver.exe"" start", 0, False
```

**Meaning**: `0x800704C7` = `ERROR_OPERATION_ABORTED` — the program was cancelled at launch by a popup or interception action. **The script itself is fine.** Likely causes, in order of probability:

1. **SmartScreen (Mark of the Web)**: an exe transferred via WeChat/QQ/cloud drive or downloaded via browser carries the "from the internet" mark; the SmartScreen prompt is dismissed or clicked "No" at launch. Fix: right-click the exe → Properties → check "**Unblock**" at the bottom; do the same for the `.vbs` script. Or via command line:

   ```powershell
   Unblock-File .\websearch-mcpserver.exe
   Unblock-File .\autostart.vbs
   ```

2. **Antivirus**: 360/Huorong/Kaspersky etc. may block or terminate unsigned exes. Check the antivirus quarantine/block log and whitelist the install directory.

3. **UAC elevation denied**: in exe Properties → Compatibility, uncheck "**Run this program as an administrator**" if set; or the UAC prompt was clicked "No".

**Locating the popup**: bypass the autostart script and run once in the foreground to see exactly which prompt is being "cancelled":

```powershell
cd D:\Programs\websearch
.\websearch-mcpserver.exe start
```

> Note: a corrupted transfer usually reports "not a valid Win32 application" instead of `0x800704C7`. Most likely it is cause #1 — the exe was not unblocked and the SmartScreen prompt got flashed away or denied during logon autostart.
