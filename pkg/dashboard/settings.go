package dashboard

import (
	"slices"

	"websearch/pkg/config"
)

type SettingsRequest struct {
	Changes       map[string]any `json:"changes"`
	Confirm       bool           `json:"confirm"`
	Revision      string         `json:"revision"`
	RestoreBackup string         `json:"restore_backup,omitempty"`
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
		"secrets": c.EffectiveCredentialFlags(),
		// 管理员口令只暴露“是否已配置”，值本身永不离开服务端，
		// 也不能通过本控制台修改（只能手改 dashboard.yaml 后重启）。
		"admin_password_configured": c.Dashboard.AdminPasswordConfigured(),
		"admin_username_configured": c.Dashboard.AdminUsername != "",
	}
}
