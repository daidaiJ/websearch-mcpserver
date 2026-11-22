// Package redact removes credential-shaped substrings before text is logged,
// persisted, or returned to an MCP client.
package redact

import "regexp"

var (
	// reKeyValue matches key/value forms such as api_key=..., token: "...",
	// password=..., and username=.... The prefix (including any opening quote)
	// is preserved; only the value is replaced.
	reKeyValue = regexp.MustCompile(`(?i)\b((?:api[_-]?key|apikey|access[_-]?key|secret[_-]?key|client[_-]?secret|access[_-]?token|refresh[_-]?token|auth[_-]?token|secret|password|passwd|pwd|token|bearer|username|user)\s*[:=]\s*["']?)([^\s"',;)\]}]{6,})`)
	// reToken matches bare credential tokens such as as_sk_..., sk-proj-...,
	// and token-....
	reToken = regexp.MustCompile(`(?i)\b(?:as_sk|sk|pk|api|key|token|bearer)[-_][a-z0-9_-]{8,}\b`)
)

// Credentials replaces credential-shaped substrings with [secret].
func Credentials(s string) string {
	s = reKeyValue.ReplaceAllString(s, "${1}[secret]")
	return reToken.ReplaceAllString(s, "[secret]")
}
