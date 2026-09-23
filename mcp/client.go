package mcpserver

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"

	mcp "github.com/modelcontextprotocol/go-sdk/mcp"
	"websearch/pkg/telemetry"
)

// MCP 客户端识别（用量归组，被动、只读展示）：
//  1. 权威来源：initialize 握手体里的 clientInfo.name（MCP 规范必带），
//     按响应分配的 Mcp-Session-Id 登记会话，后续调用按会话归属客户端；
//  2. 兜底：无会话（stateless / 未握手）的请求回退 User-Agent 归一化；
//  3. 两者都拿不到时记为 unknown。
// 识别只影响控制台展示分组，不影响任何搜索行为。

const maxTrackedSessions = 4096

var sessionClients = struct {
	mu sync.RWMutex
	m  map[string]string
}{m: make(map[string]string)}

func sessionClientSet(sid, client string) {
	if sid == "" || client == "" {
		return
	}
	sessionClients.mu.Lock()
	if len(sessionClients.m) >= maxTrackedSessions {
		sessionClients.mu.Unlock()
		return // 防御性上限：会话映射超限即停止登记（不影响搜索）
	}
	sessionClients.m[sid] = client
	sessionClients.mu.Unlock()
}

func sessionClientGet(sid string) string {
	if sid == "" {
		return ""
	}
	sessionClients.mu.RLock()
	defer sessionClients.mu.RUnlock()
	return sessionClients.m[sid]
}

// clientFromUserAgent 从 HTTP User-Agent 归一化客户端标识（无会话时的兜底）。
func clientFromUserAgent(ua string) string {
	ua = strings.ToLower(strings.TrimSpace(ua))
	if ua == "" {
		return "unknown"
	}
	known := []struct{ match, client string }{
		{"claude", "claude-code"}, {"qwen", "qwen-code"}, {"cursor", "cursor"},
		{"windsurf", "windsurf"}, {"cherry", "cherrystudio"}, {"cline", "cline"},
		{"roo", "roo-code"}, {"trae", "trae"}, {"zed", "zed"}, {"vscode", "vscode"},
		{"visual-studio", "vscode"}, {"gemini", "gemini-cli"}, {"codex", "codex"},
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

type initializeProbe struct {
	Method string `json:"method"`
	Params struct {
		ClientInfo struct {
			Name string `json:"name"`
		} `json:"clientInfo"`
	} `json:"params"`
}

// sidCapturingWriter 在响应头写出的瞬间捕获服务器分配的 Mcp-Session-Id，
// 用于把 initialize 请求的 clientInfo 登记到该会话。
type sidCapturingWriter struct {
	http.ResponseWriter
	onWriteHeader func()
	once          sync.Once
}

func (w *sidCapturingWriter) WriteHeader(status int) {
	w.once.Do(func() { w.onWriteHeader() })
	w.ResponseWriter.WriteHeader(status)
}

// withClientContext 识别请求所属的 MCP 客户端并注入上下文，供遥测按客户端
// 归组。会话登记只在 initialize 握手时窥探请求体（原样回填，不影响协议）。
// stateless 模式没有稳定会话可登记，仅按 UA 兜底。
func withClientContext(stateless bool, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		client := sessionClientGet(r.Header.Get("Mcp-Session-Id"))
		if client == "" && r.Method == http.MethodPost {
			if name, req2 := peekInitializeClient(r); name != "" {
				client = name
				r = req2
				if sid := r.Header.Get("Mcp-Session-Id"); sid != "" {
					sessionClientSet(sid, client)
				} else if !stateless {
					// 会话 id 由服务器在响应头分配：包一层 WriteHeader 捕获
					sidW := &sidCapturingWriter{ResponseWriter: w}
					sidW.onWriteHeader = func() {
						sessionClientSet(sidW.Header().Get("Mcp-Session-Id"), client)
					}
					w = sidW
				}
			}
		}
		if client == "" {
			client = clientFromUserAgent(r.UserAgent())
		}
		// initialize 也要注入：会话上下文派生自握手请求，后续调用继承归属
		next.ServeHTTP(w, r.WithContext(telemetry.WithClient(r.Context(), client)))
	})
}

// peekInitializeClient 探测 POST 体是否为 initialize 握手并返回归一化的
// clientInfo.name；读完原样回填 body。
func peekInitializeClient(r *http.Request) (string, *http.Request) {
	if r.Body == nil {
		return "", r
	}
	buf, err := io.ReadAll(io.LimitReader(r.Body, 512<<10))
	_ = r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(buf))
	if err != nil {
		return "", r
	}
	var probe initializeProbe
	if err := json.Unmarshal(buf, &probe); err != nil || probe.Method != "initialize" {
		return "", r
	}
	name := strings.ToLower(strings.TrimSpace(probe.Params.ClientInfo.Name))
	if name == "" {
		return "unknown", r
	}
	if len(name) > 32 {
		name = name[:32]
	}
	return name, r
}

// ClientAttributionMiddleware 是 go-sdk 接收侧中间件（权威归组路径）：
// initialize 握手时把规范必带的 clientInfo.name 登记到会话；
// tools/call 时按会话把客户端标识注入 ctx，工具层与 provider 层事件据此归组。
func ClientAttributionMiddleware(next mcp.MethodHandler) mcp.MethodHandler {
	return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
		session, okS := req.GetSession().(*mcp.ServerSession)
		if okS {
			switch method {
			case "initialize":
				if init, ok := req.GetParams().(*mcp.InitializeParams); ok {
					c := normalizeClientName(init.ClientInfo.Name)
					sessionClientSet(session.ID(), c)
				} else {
				}
			case "tools/call":
				if client := sessionClientGet(session.ID()); client != "" {
					ctx = telemetry.WithClient(ctx, client)
				} else {
				}
			}
		}
		return next(ctx, method, req)
	}
}

// normalizeClientName 归一化客户端名：小写、去空白、限长；空值回退 unknown。
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
