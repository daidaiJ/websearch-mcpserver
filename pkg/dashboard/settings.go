package dashboard

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"websearch/pkg/config"
)

type SettingsRequest struct {
	Changes       map[string]any `json:"changes"`
	Confirm       bool           `json:"confirm"`
	Revision      string         `json:"revision"`
	RestoreBackup string         `json:"restore_backup,omitempty"`
}

type SecretRequest struct {
	Name    string `json:"name"`
	Value   string `json:"value"`
	Confirm bool   `json:"confirm"`
}

type SettingsResult struct {
	Applied         bool              `json:"applied"`
	RestartRequired bool              `json:"restart_required"`
	Backup          string            `json:"backup,omitempty"`
	Changes         map[string]any    `json:"changes"`
	Revision        string            `json:"revision"`
	FieldFiles      map[string]string `json:"field_files,omitempty"`
}

func settingsView(c config.Config) map[string]any {
	fields := settingFields(c)
	values := make(map[string]any, len(fields))
	options := map[string][]string{}
	for path, field := range fields {
		values[path] = field.value
		if len(field.options) != 0 {
			options[path] = slices.Clone(field.options)
		}
	}
	return map[string]any{
		"values":  values,
		"options": options,
		"secrets": map[string]bool{
			"BAIDU_SK": len(c.Baidu.EffectiveSKList()) > 0, "TAVILY_SK": len(c.Tavily.EffectiveSKList()) > 0,
			"EXA_API_KEY": len(c.Exa.EffectiveSKList()) > 0, "ANYSEARCH_API_KEY": len(c.Anysearch.EffectiveSKList()) > 0,
			"DOUBAO_SEARCH_API_KEY": len(c.Doubao.EffectiveSKList()) > 0, "TENCENTCLOUD_WSA_APIKEY": len(c.Yuanbao.EffectiveSKList()) > 0, "JINA_API_KEY": c.Jina.APIKey != "",
			"MINERU_TOKEN": c.PDFParser.MinerUToken != "", "GITHUB_TOKEN": c.GitHub.HasToken(),
		},
		// 管理员口令只暴露“是否已配置”，值本身永不离开服务端，
		// 也不能通过本控制台修改（只能手改 dashboard.yaml 后重启）。
		"admin_password_configured": c.Dashboard.AdminPasswordConfigured(),
		"admin_username_configured": c.Dashboard.AdminUsername != "",
	}
}

var allowedSecrets = []string{"BAIDU_SK", "TAVILY_SK", "EXA_API_KEY", "ANYSEARCH_API_KEY", "DOUBAO_SEARCH_API_KEY", "TENCENTCLOUD_WSA_APIKEY", "JINA_API_KEY", "MINERU_TOKEN", "GITHUB_TOKEN"}

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
