package mcpserver

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"websearch/pkg/antirobot"
	"websearch/pkg/config"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// listResourceURIs 经内存 transport 连接并列出 Resource URI。
func listResourceURIs(t *testing.T, conf config.Config) []string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	t1, t2 := mcp.NewInMemoryTransports()
	ss, err := NewMCPServer(conf, nil).Connect(ctx, t1, nil)
	if err != nil {
		t.Fatalf("server connect: %v", err)
	}
	defer ss.Close()

	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "0"}, nil).Connect(ctx, t2, nil)
	if err != nil {
		t.Fatalf("client connect: %v", err)
	}
	defer cs.Close()

	res, err := cs.ListResources(ctx, nil)
	if err != nil {
		t.Fatalf("ListResources: %v", err)
	}
	uris := make([]string, 0, len(res.Resources))
	for _, r := range res.Resources {
		uris = append(uris, r.URI)
	}
	return uris
}

func TestRegisterResources_DefaultEnabled(t *testing.T) {
	conf := config.Config{}
	conf.Bing.Enabled = true

	uris := listResourceURIs(t, conf)
	for _, want := range []string{"search://capabilities", "search://health"} {
		if !slices.Contains(uris, want) {
			t.Errorf("默认应注册 %s, got %v", want, uris)
		}
	}
}

func TestRegisterResources_DisabledByConfig(t *testing.T) {
	off := false
	conf := config.Config{MCPResources: &off}
	conf.Bing.Enabled = true

	if uris := listResourceURIs(t, conf); len(uris) != 0 {
		t.Errorf("mcp_resources: false 时不应注册任何 Resource, got %v", uris)
	}
}

func TestRenderCapabilities_NoSecrets(t *testing.T) {
	conf := config.Config{}
	conf.Bing.Enabled = true
	conf.Tavily.SKList = []string{"sk-secret-1", "sk-secret-2"}

	text := renderCapabilities(conf)
	if !strings.Contains(text, "smartsearch") {
		t.Errorf("capabilities 应列出已注册工具: %q", text)
	}
	if !strings.Contains(text, "2") {
		t.Errorf("provider 应输出 Key 数量: %q", text)
	}
	if strings.Contains(text, "sk-secret-1") || strings.Contains(text, "sk-secret-2") {
		t.Error("capabilities 泄漏了 Key 值（红线）")
	}
}

func TestRenderHealth_ShapeAndNoSecrets(t *testing.T) {
	antirobot.SetHealthStore("")
	t.Cleanup(func() { antirobot.SetHealthStore("") })

	data := renderHealth()
	var payload map[string]any
	if err := json.Unmarshal([]byte(data), &payload); err != nil {
		t.Fatalf("health 应为合法 JSON: %v", err)
	}
	for _, key := range []string{"version", "uptime_seconds", "engine_cooldowns", "cooldown_persist_enabled"} {
		if _, ok := payload[key]; !ok {
			t.Errorf("health 缺少字段 %s: %s", key, data)
		}
	}
	if strings.Contains(data, "sk-") || strings.Contains(data, "api_key") {
		t.Error("health 泄漏了密钥信息（红线）")
	}
}

func TestRenderHealth_ReflectsCooldowns(t *testing.T) {
	path := t.TempDir() + "/engine_health.json"
	antirobot.SetHealthStore(path)
	t.Cleanup(func() { antirobot.SetHealthStore("") })
	antirobot.EnterCooldown("duckduckgo", antirobot.HealthKindRateLimit, "202/429", time.Now().Add(time.Minute), 20*time.Second)

	data := renderHealth()
	if !strings.Contains(data, "duckduckgo") || !strings.Contains(data, "rate_limit") {
		t.Errorf("health 应含冷却引擎: %s", data)
	}
	if !strings.Contains(data, `"cooldown_persist_enabled": true`) {
		t.Errorf("health 应报告落盘已启用: %s", data)
	}
}
