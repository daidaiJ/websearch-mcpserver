package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/viper"
)

func TestAPISourceControls(t *testing.T) {
	t.Setenv("WEBSEARCH_CONFIG", "")
	t.Setenv("WEBSEARCH_DASHBOARD_CONFIG", "")
	for _, name := range []string{"baidu", "tavily", "exa", "anysearch", "doubao", "yuanbao"} {
		for _, value := range []string{"", "true", "false"} {
			t.Run(name+"/"+value, func(t *testing.T) {
				viper.Reset()
				t.Cleanup(viper.Reset)
				yaml := fmt.Sprintf("mode: hybrid\n%s:\n  api_key: stored-test-key\n", name)
				if value != "" {
					yaml += "  enabled: " + value + "\n"
				}
				path := filepath.Join(t.TempDir(), "config.yaml")
				if err := os.WriteFile(path, []byte(yaml), 0600); err != nil {
					t.Fatal(err)
				}
				conf, err := Load(path)
				if err != nil {
					t.Fatal(err)
				}
				if conf.APISourceEnabled(name) != (value != "false") {
					t.Fatal("source switch did not preserve the default or explicit value")
				}
				for _, other := range []string{"baidu", "tavily", "exa", "anysearch", "doubao", "yuanbao"} {
					if other != name && !conf.APISourceEnabled(other) {
						t.Fatalf("changing %s disabled unrelated %s", name, other)
					}
				}
				keys := map[string][]string{
					"baidu": conf.Baidu.EffectiveSKList(), "tavily": conf.Tavily.EffectiveSKList(),
					"exa": conf.Exa.EffectiveSKList(), "anysearch": conf.Anysearch.EffectiveSKList(), "doubao": conf.Doubao.EffectiveSKList(),
					"yuanbao": conf.Yuanbao.EffectiveSKList(),
				}
				if len(keys[name]) != 1 || keys[name][0] != "stored-test-key" {
					t.Fatal("source switch must not remove stored credentials")
				}
			})
		}
	}
	if (Config{}).APISourceEnabled("unknown") {
		t.Fatal("unknown API sources must not be enabled")
	}
}

func TestParseWriteAllowedNetworks(t *testing.T) {
	dash := DashboardConfig{AllowedNetworks: []string{"10.0.0.0/8"}}
	if nets, err := dash.ParseWriteAllowedNetworks(); err != nil || len(nets) != 0 {
		t.Fatal("read networks must not grant write access")
	}
	dash.WriteAllowedNetworks = []string{"192.168.1.0/24", "10.0.0.3", "::1", "2001:db8::/32"}
	nets, err := dash.ParseWriteAllowedNetworks()
	if err != nil || len(nets) != 4 {
		t.Fatalf("valid write networks: %v %v", nets, err)
	}
	if !nets[1].Contains(net.ParseIP("10.0.0.3")) || nets[1].Contains(net.ParseIP("10.0.0.4")) ||
		!nets[2].Contains(net.ParseIP("::1")) || nets[2].Contains(net.ParseIP("::2")) {
		t.Fatal("bare IP write entries must permit only that address")
	}
	dash.WriteAllowedNetworks = []string{"10.0.0.0/8", "invalid"}
	if nets, err := dash.ParseWriteAllowedNetworks(); err == nil || nets != nil {
		t.Fatal("invalid write entry must discard the complete allowlist")
	}
}

func TestWriteNetworksOverlay(t *testing.T) {
	t.Setenv("WEBSEARCH_CONFIG", "")
	t.Setenv("WEBSEARCH_DASHBOARD_CONFIG", "")
	for _, overlay := range []string{"  write_allowed_networks: [\"192.0.2.5\"]\n", "  write_allowed_networks: []\n", "  enabled: true\n"} {
		t.Run(overlay, func(t *testing.T) {
			viper.Reset()
			t.Cleanup(viper.Reset)
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte("dashboard:\n  write_allowed_networks: [\"10.0.0.3\"]\n  allowed_networks: [\"10.0.0.0/8\"]\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "dashboard.yaml"), []byte("dashboard:\n"+overlay), 0600); err != nil {
				t.Fatal(err)
			}
			conf, err := Load(path)
			if err != nil {
				t.Fatal(err)
			}
			want := "192.0.2.5"
			if overlay == "  enabled: true\n" {
				want = "10.0.0.3"
			}
			if overlay == "  write_allowed_networks: []\n" {
				if len(conf.Dashboard.WriteAllowedNetworks) != 0 {
					t.Fatal("explicit empty overlay must revoke main-config write networks")
				}
			} else if len(conf.Dashboard.WriteAllowedNetworks) != 1 || conf.Dashboard.WriteAllowedNetworks[0] != want {
				t.Fatal("write network overlay was not applied field-by-field")
			}
			if len(conf.Dashboard.AllowedNetworks) != 1 || conf.Dashboard.AllowedNetworks[0] != "10.0.0.0/8" {
				t.Fatal("write overlay must not change read networks")
			}
		})
	}
}
