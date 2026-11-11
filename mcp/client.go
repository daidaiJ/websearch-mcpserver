package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"unicode"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"websearch/pkg/telemetry"
)

// clientFromUserAgent is only a fallback when protocol identity is unavailable.
func clientFromUserAgent(ua string) string {
	ua = strings.ToLower(strings.TrimSpace(ua))
	if ua == "" {
		return "unknown"
	}
	known := []struct{ match, client string }{
		{"claude", "claude-code"}, {"qwen", "qwen-code"}, {"cursor", "cursor"},
		{"windsurf", "windsurf"}, {"cherry", "cherrystudio"}, {"cline", "cline"},
		{"roo", "roo-code"}, {"trae", "trae"}, {"zed", "zed"}, {"vscode", "vscode"},
		{"visual-studio", "vscode"}, {"gemini", "gemini-cli"}, {"codex", "codex"}, {"workbuddy", "workbuddy"},
	}
	for _, k := range known {
		if strings.Contains(ua, k.match) {
			return k.client
		}
	}
	token := strings.Fields(ua)[0]
	if i := strings.IndexAny(token, "/ ;,("); i > 0 {
		token = token[:i]
	}
	token = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' || r == '.' {
			return r
		}
		return -1
	}, token)
	if token == "" || len(token) > 32 {
		return "unknown"
	}
	return token
}

// ClientAttributionMiddleware uses SDK identity across HTTP and stdio transports.
func ClientAttributionMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		if method == "tools/call" {
			client, project := "unknown", ""
			if call, ok := req.(*mcp.CallToolRequest); ok {
				// ClientInfo reads per-call standard metadata before session initialization.
				if info := call.ClientInfo(); info != nil {
					client = normalizeClientName(info.Name)
				}
			}
			if extra := req.GetExtra(); extra != nil {
				ctx = telemetry.WithClientUserAgent(ctx, extra.Header.Get("User-Agent"))
				if explicit := explicitClientName(extra.Header.Get("X-WebSearch-Client")); explicit != "" {
					client = explicit
				} else if client == "unknown" || client == "websearch" {
					fallback := clientFromUserAgent(extra.Header.Get("User-Agent"))
					if client == "unknown" || telemetry.Application(fallback) != "unknown" {
						client = fallback
					}
				}
				project = normalizeProjectLabel(extra.Header.Get("X-WebSearch-Project"))
			}
			if call, ok := req.(*mcp.CallToolRequest); ok && call.Params != nil {
				var args struct {
					Project string `json:"project"`
				}
				if json.Unmarshal(call.Params.Arguments, &args) == nil {
					if label := normalizeProjectLabel(args.Project); label != "" {
						project = label
					}
				}
			}
			// Project labels belong to this call, never the initialize context.
			ctx = telemetry.WithProject(telemetry.WithClient(ctx, client), project)
		}
		return next(ctx, method, req)
	}
}

func normalizeProjectLabel(label string) string {
	if strings.ContainsFunc(label, unicode.IsControl) {
		return ""
	}
	label = telemetry.NormalizeProject(label)
	if label == "." || label == ".." {
		return ""
	}
	return label
}

func explicitClientName(name string) string {
	if strings.ContainsFunc(name, unicode.IsControl) {
		return ""
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "." || name == ".." || len(name) > 32 || telemetry.NormalizeProject(name) != name {
		return ""
	}
	return name
}

// normalizeClientName keeps protocol-provided client aliases stable in telemetry.
func normalizeClientName(name string) string {
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return "unknown"
	}
	if len(name) > 32 {
		name = name[:32]
	}
	return name
}
