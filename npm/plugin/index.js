/**
 * Runtime marker for config-only dsh bundles.
 *
 * This package contributes no Cordis plugin code: the integration lives in
 * cordis.patch.yml, which instantiates dsh's built-in @deepseek-ai/dsh-mcp-client
 * as a stdio server pointing at the bundled Go binary. A side-effect-free ESM
 * entry keeps package-root resolution (and marketplace validation) happy.
 */
export {}
