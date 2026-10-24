package mcpserver

import (
	"testing"

	"websearch/pkg/config"
)

func TestNativeFetchProxyRespectsAutoProxy(t *testing.T) {
	conf := config.Config{Proxy: config.ProxyConfig{Enabled: true, Endpoint: "http://127.0.0.1:7897"}}
	if got := nativeFetchProxy(conf); got != "http://127.0.0.1:7897" {
		t.Fatalf("auto_proxy default should inherit the global proxy, got %q", got)
	}
	off := false
	conf.CleanFetch.AutoProxy = &off
	if got := nativeFetchProxy(conf); got != "" {
		t.Fatalf("auto_proxy=false must force a direct connection, got %q", got)
	}
}
