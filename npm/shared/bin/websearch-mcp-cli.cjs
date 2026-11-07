#!/usr/bin/env node
'use strict'

// Thin launcher for the prebuilt Go stdio binary shipped in the matching
// platform npm package. No download, no chmod, no lifecycle scripts at install
// time: pnpm 10+ blocks install/postinstall scripts via allowBuilds.
//
// Published in two packages from this single source file:
//   websearch-mcp-cli            -> bin/
//   dsh-websearch-mcpserver      -> bin/ (config-only Cordis bundle)
//
// Env:
//   WEBSEARCH_MCP_CLI_BINARY   absolute path to a manually downloaded binary,
//                              for platforms outside the release matrix
//   WEBSEARCH_MCP_CONFIG       absolute path to a config.yaml; forwarded as
//                              "-c <path>" (the Go binary also reads the
//                              native WEBSEARCH_CONFIG env var, which wins)

const { spawn } = require('node:child_process')
const fs = require('node:fs')
const path = require('node:path')

const PLATFORM_NAME = { linux: 'linux', darwin: 'darwin', win32: 'windows' }
const ARCH_NAME = { x64: 'amd64', arm64: 'arm64' }
const SUPPORTED = 'linux-amd64, windows-amd64, darwin-amd64, darwin-arm64'

function fail(message) {
  process.stderr.write(`[websearch-mcp-cli] ${message}\n`)
  process.exit(1)
}

function resolveBinary() {
  const override = process.env.WEBSEARCH_MCP_CLI_BINARY
  if (override) {
    if (!fs.existsSync(override)) {
      fail(`WEBSEARCH_MCP_CLI_BINARY points to a missing file: ${override}`)
    }
    return override
  }

  const os = PLATFORM_NAME[process.platform]
  const arch = ARCH_NAME[process.arch]
  if (!os || !arch) {
    fail(
      `unsupported platform ${process.platform}/${process.arch}. Supported: ${SUPPORTED}. ` +
        'Use the GHCR image or a Release binary with WEBSEARCH_MCP_CLI_BINARY.',
    )
  }

  const packageName = `websearch-mcp-cli-${os}-${arch}`
  let packageDir
  try {
    packageDir = path.dirname(require.resolve(`${packageName}/package.json`))
  } catch (error) {
    fail(
      `platform package "${packageName}" is not installed. Reinstall against the official registry ` +
        '(npm config set registry https://registry.npmjs.org), or set WEBSEARCH_MCP_CLI_BINARY ' +
        `to a downloaded Release binary. (${error && error.code ? error.code : error})`,
    )
  }

  const binaryName = process.platform === 'win32' ? 'websearch-mcp-cli.exe' : 'websearch-mcp-cli'
  const binary = path.join(packageDir, 'bin', binaryName)
  if (!fs.existsSync(binary)) {
    fail(`binary is missing inside ${packageName}: ${binary}`)
  }
  return binary
}

const extraArgs = []
const configPath = process.env.WEBSEARCH_MCP_CONFIG
if (configPath) {
  if (!fs.existsSync(configPath)) {
    fail(`WEBSEARCH_MCP_CONFIG points to a missing file: ${configPath}`)
  }
  extraArgs.push('-c', configPath)
}

const child = spawn(resolveBinary(), [...extraArgs, ...process.argv.slice(2)], {
  stdio: 'inherit',
  windowsHide: true,
})

child.on('error', (error) => fail(`failed to start the Go binary: ${error.message}`))

for (const signal of ['SIGINT', 'SIGTERM']) {
  process.on(signal, () => child.kill(signal))
}

child.on('exit', (code, signal) => {
  // Drop our forwarding handlers before re-raising, otherwise the signal
  // would be swallowed by this process and the MCP client would hang.
  process.removeAllListeners('SIGINT')
  process.removeAllListeners('SIGTERM')
  if (signal) {
    process.kill(process.pid, signal)
  } else {
    process.exit(code === null ? 1 : code)
  }
})
