package mcpserver

import (
	"sync"
	"time"

	searchcore "websearch/pkg/search/core"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// provenanceSessionTTL bounds the per-session notice map. HTTP sessions time
// out after 5 minutes; an hour keeps the map tiny while surviving long calls.
const provenanceSessionTTL = time.Hour

type provenanceSessionTracker struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

var provenanceSessions = provenanceSessionTracker{seen: make(map[string]time.Time)}

// provenanceHeaderForRequest 只在一个 MCP 会话的首次搜索响应里输出完整
// usage_note；后续调用压缩为时间戳（缓存命中只保留 cache_age_seconds）。
// 无会话标识的模式（stdio / stateless HTTP）无法跨请求关联，使用短头。
func provenanceHeaderForRequest(req *mcp.CallToolRequest, retrievedAt time.Time, cacheAge time.Duration) string {
	return provenanceHeaderForSession(requestSessionID(req), retrievedAt, cacheAge)
}

func provenanceHeaderForSession(id string, retrievedAt time.Time, cacheAge time.Duration) string {
	if retrievedAt.IsZero() {
		return ""
	}
	if id == "" {
		return searchcore.CompactProvenanceHeader(retrievedAt, cacheAge)
	}
	if provenanceSessions.mark(id) {
		return searchcore.ProvenanceHeader(retrievedAt, cacheAge)
	}
	if cacheAge > 0 {
		return searchcore.CacheAgeHeader(cacheAge)
	}
	return ""
}

func requestSessionID(req *mcp.CallToolRequest) string {
	if req == nil || req.Session == nil {
		return ""
	}
	return req.Session.ID()
}

func (t *provenanceSessionTracker) mark(id string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for key, seenAt := range t.seen {
		if now.Sub(seenAt) > provenanceSessionTTL {
			delete(t.seen, key)
		}
	}
	if _, ok := t.seen[id]; ok {
		t.seen[id] = now
		return false
	}
	t.seen[id] = now
	return true
}

func (t *provenanceSessionTracker) reset() {
	t.mu.Lock()
	t.seen = make(map[string]time.Time)
	t.mu.Unlock()
}
