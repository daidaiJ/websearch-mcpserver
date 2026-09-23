package dashboard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"go.yaml.in/yaml/v3"

	"websearch/pkg/config"
)

type SettingsRequest struct {
	Changes map[string]any `json:"changes"`
	Confirm bool           `json:"confirm"`
}

type SecretRequest struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Confirm bool   `json:"confirm"`
}

type SettingsResult struct {
	Applied         bool           `json:"applied"`
	RestartRequired bool           `json:"restart_required"`
	Backup          string         `json:"backup,omitempty"`
	Changes         map[string]any `json:"changes"`
}

func settingsView(c config.Config) map[string]any {
	return map[string]any{
		"values": map[string]any{
			"mode": c.Mode, "network": c.Network, "upstream_timeout_sec": c.UpstreamTimeoutSec,
			"cache.enabled": c.CacheEnabled(), "cache.cleanup_interval": c.Cache.CleanupInterval,
			"bing.enabled": c.Bing.Enabled, "duckduckgo.enabled": c.DuckDuckGo.Enabled,
			"google.enabled": c.Google.Enabled, "academic.enabled": c.Academic.Enabled,
			"cleanfetch.enabled": c.CleanFetch.Enabled, "pdf_parser.enabled": c.PDFParser.Enabled,
			"smartsearch.max_size":            c.SmartSearch.MaxSize,
			"smartsearch.relevance_threshold": c.SmartSearch.RelevanceThreshold,
			"smartsearch.mmr.enabled":         c.SmartSearch.MMR.Enabled,
			"smartsearch.mmr.lambda":          c.SmartSearch.MMR.Lambda,
			"doubao.version":                  c.Doubao.GetVersion(), "apipool.engines": c.Apipool.GetEngines(),
			"dashboard.suspension.ban_time_on_fail":     c.Dashboard.Suspension.BanTimeOnFail,
			"dashboard.suspension.max_ban_time_on_fail": c.Dashboard.Suspension.MaxBanTimeOnFail,
			"dashboard.suspension.suspended_times":      c.Dashboard.Suspension.SuspendedTimes,
		},
		"secrets": map[string]bool{
			"BAIDU_SK": len(c.Baidu.EffectiveSKList()) > 0, "TAVILY_SK": len(c.Tavily.EffectiveSKList()) > 0,
			"EXA_API_KEY": len(c.Exa.EffectiveSKList()) > 0, "ANYSEARCH_API_KEY": len(c.Anysearch.EffectiveSKList()) > 0,
			"DOUBAO_SEARCH_API_KEY": len(c.Doubao.EffectiveSKList()) > 0, "JINA_API_KEY": c.Jina.APIKey != "",
			"MINERU_TOKEN": c.PDFParser.MinerUToken != "",
		},
		// 管理员口令只暴露“是否已配置”，值本身永不离开服务端，
		// 也不能通过本控制台修改（只能手改 config.yaml 后重启）。
		"admin_password_configured": c.Dashboard.AdminPasswordConfigured(),
		"admin_username_configured": c.Dashboard.AdminUsername != "",
		"networks": map[string]any{
			"allowed": c.Dashboard.AllowedNetworks,
		},
	}
}

func applySettings(c config.Config, req SettingsRequest) (SettingsResult, error) {
	out := SettingsResult{Changes: req.Changes, RestartRequired: len(req.Changes) > 0}
	if len(req.Changes) == 0 {
		return out, fmt.Errorf("no changes supplied")
	}
	for path, value := range req.Changes {
		if err := validateSetting(path, value); err != nil {
			return out, err
		}
	}
	if !req.Confirm {
		return out, nil
	}
	path := config.GetConfigFile()
	if path == "" {
		return out, fmt.Errorf("active config file is unknown")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return out, fmt.Errorf("read active config: %w", err)
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return out, fmt.Errorf("parse active config: %w", err)
	}
	for path, value := range req.Changes {
		if err := setYAMLPath(&doc, strings.Split(path, "."), value); err != nil {
			return out, err
		}
	}
	next, err := yaml.Marshal(&doc)
	if err != nil {
		return out, fmt.Errorf("encode config: %w", err)
	}
	backupDir := filepath.Join(filepath.Dir(c.GetDashboardStoragePath()), "dashboard-backups")
	if err := os.MkdirAll(backupDir, 0o755); err != nil {
		return out, err
	}
	backup := filepath.Join(backupDir, "config-"+time.Now().Format("20060102-150405")+".yaml")
	if err := os.WriteFile(backup, b, 0o600); err != nil {
		return out, fmt.Errorf("write backup: %w", err)
	}
	if err := os.WriteFile(path, next, 0o644); err != nil {
		return out, fmt.Errorf("write active config: %w", err)
	}
	// Parse again after writing so a broken YAML file is never reported as applied.
	var verify yaml.Node
	if err := yaml.Unmarshal(next, &verify); err != nil {
		_ = os.WriteFile(path, b, 0o644)
		return out, fmt.Errorf("verify written config: %w", err)
	}
	out.Applied, out.Backup = true, backup
	return out, nil
}

func validateSetting(path string, value any) error {
	boolPaths := []string{"cache.enabled", "bing.enabled", "duckduckgo.enabled", "google.enabled", "academic.enabled", "cleanfetch.enabled", "pdf_parser.enabled", "smartsearch.mmr.enabled"}
	if slices.Contains(boolPaths, path) {
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be boolean", path)
		}
		return nil
	}
	switch path {
	case "mode":
		v, ok := value.(string)
		if !ok || !slices.Contains([]string{"hybrid", "engine", "apipool", "baidu", "tavily", "exa", "anysearch", "doubao"}, v) {
			return fmt.Errorf("invalid mode")
		}
	case "network":
		v, ok := value.(string)
		if !ok || !slices.Contains([]string{"china", "international"}, v) {
			return fmt.Errorf("invalid network")
		}
	case "doubao.version":
		v, ok := value.(string)
		if !ok || !slices.Contains([]string{"global", "custom"}, v) {
			return fmt.Errorf("invalid doubao.version")
		}
	case "upstream_timeout_sec", "cache.cleanup_interval", "smartsearch.max_size":
		v, ok := asFloat(value)
		if !ok || v < 0 || v > 360 {
			return fmt.Errorf("%s is out of range", path)
		}
	case "smartsearch.relevance_threshold", "smartsearch.mmr.lambda":
		v, ok := asFloat(value)
		if !ok || v < 0 || v > 1 {
			return fmt.Errorf("%s must be between 0 and 1", path)
		}
	case "apipool.engines":
		arr, ok := value.([]any)
		if !ok || len(arr) == 0 {
			return fmt.Errorf("apipool.engines must be a non-empty array")
		}
		allowed := []string{"anysearch", "baidu", "tavily", "exa", "doubao"}
		for _, raw := range arr {
			v, ok := raw.(string)
			if !ok || !slices.Contains(allowed, v) {
				return fmt.Errorf("invalid apipool engine")
			}
		}
	case "dashboard.suspension.ban_time_on_fail", "dashboard.suspension.max_ban_time_on_fail":
		v, ok := value.(string)
		if !ok || strings.TrimSpace(v) == "" {
			return fmt.Errorf("%s must be a duration string such as 5s, 10m or 24h", path)
		}
		if _, err := parseSuspensionDuration(v); err != nil {
			return fmt.Errorf("%s: %v", path, err)
		}
	default:
		return fmt.Errorf("setting %s is not editable", path)
	}
	return nil
}

func asFloat(v any) (float64, bool) { f, ok := v.(float64); return f, ok }

func setYAMLPath(doc *yaml.Node, path []string, value any) error {
	if len(doc.Content) == 0 {
		return fmt.Errorf("empty yaml document")
	}
	n := doc.Content[0]
	for _, key := range path {
		if n.Kind != yaml.MappingNode {
			return fmt.Errorf("%s is not a mapping", key)
		}
		var child *yaml.Node
		for i := 0; i < len(n.Content); i += 2 {
			if n.Content[i].Value == key {
				child = n.Content[i+1]
				break
			}
		}
		if child == nil {
			n.Content = append(n.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key}, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
			child = n.Content[len(n.Content)-1]
		}
		n = child
	}
	var replacement yaml.Node
	b, _ := yaml.Marshal(value)
	if err := yaml.Unmarshal(b, &replacement); err != nil {
		return err
	}
	*n = *replacement.Content[0]
	return nil
}

var allowedSecrets = []string{"BAIDU_SK", "TAVILY_SK", "EXA_API_KEY", "ANYSEARCH_API_KEY", "DOUBAO_SEARCH_API_KEY", "JINA_API_KEY", "MINERU_TOKEN"}

func saveSecret(c config.Config, req SecretRequest) error {
	if !req.Confirm {
		return fmt.Errorf("confirm=true required")
	}
	if !slices.Contains(allowedSecrets, req.Name) {
		return fmt.Errorf("unsupported secret name")
	}
	if strings.ContainsAny(req.Value, "\r\n") {
		return fmt.Errorf("secret contains a newline")
	}
	path := c.GetDashboardSecretsPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	values := map[string]string{}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &values)
	}
	if req.Value == "" {
		delete(values, req.Name)
	} else {
		values[req.Name] = req.Value
	}
	b, err := json.Marshal(values)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o600)
}
