package dashboard

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"go.yaml.in/yaml/v3"

	"websearch/pkg/config"
	"websearch/pkg/i18n"
)

var settingsMu sync.Mutex
var errSettingsConflict = errors.New("configuration changed; refresh before saving")

func configRevision(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}

func backupDirectory(c config.Config) string {
	return filepath.Join(filepath.Dir(c.GetDashboardStoragePath()), "dashboard-backups")
}

func lockedSettings(c config.Config, loc *i18n.Localizer) map[string]string {
	out := map[string]string{}
	for path := range settingsView(c)["values"].(map[string]any) {
		if _, exists := os.LookupEnv("APP_" + strings.ToUpper(path)); exists {
			out[path] = loc.T("dashboard.locked.env")
		}
		if strings.HasPrefix(path, "dashboard.") && config.GetDashboardOverlayFile() != "" {
			out[path] = loc.T("dashboard.locked.overlay")
		}
	}
	return out
}

func yamlPath(doc *yaml.Node, path string) *yaml.Node {
	if len(doc.Content) == 0 {
		return nil
	}
	n := doc.Content[0]
	for _, key := range strings.Split(path, ".") {
		if n.Kind != yaml.MappingNode {
			return nil
		}
		var child *yaml.Node
		for i := 0; i+1 < len(n.Content); i += 2 {
			if n.Content[i].Value == key {
				child = n.Content[i+1]
				break
			}
		}
		if child == nil {
			return nil
		}
		n = child
	}
	return n
}

func settingsFromDocument(c config.Config, b []byte) (map[string]any, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("parse active config: %w", err)
	}
	if len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("config must be a YAML mapping")
	}
	values := settingsView(c)["values"].(map[string]any)
	for path, value := range values {
		values[path] = canonicalValue(value)
	}
	// 这里只要来源的 ID 与开关事实，不消费名称/条件文案，故用进程默认语言即可。
	for _, source := range SourceCatalog(c, i18n.Default()) {
		// Missing source switches retain loader defaults, not a pending runtime value.
		values[source.ControlPath] = source.Category == "api" || source.ID == "bing" || source.ID == "duckduckgo"
		if source.ID == "keenable" && len(c.Keenable.EffectiveSKList()) == 0 {
			// public 免 Key 档需显式 enabled: true 才参与路由，缺省不能当 true 处理，
			// 否则保存任意设置都会把 public 档静默写进候选配置。
			values[source.ControlPath] = false
		}
		if source.Inverted {
			values[source.ControlPath] = false
		}
	}
	values["academic.enabled"] = true
	values["doubao.version"] = "global"
	values["doubao.icp_host_only"] = false
	for path := range values {
		if node := yamlPath(&doc, path); node != nil {
			var value any
			if err := node.Decode(&value); err != nil {
				return nil, fmt.Errorf("invalid setting %s", path)
			}
			// Canonical JSON values match the API and avoid YAML integer/array type drift.
			values[path] = canonicalValue(value)
		}
	}
	// 只要锁定字段的路径集合，不消费原因文案，故用进程默认语言即可。
	for path := range lockedSettings(c, i18n.Default()) {
		values[path] = settingsView(c)["values"].(map[string]any)[path]
	}
	return values, nil
}

func canonicalValue(value any) any {
	switch v := value.(type) {
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case []string:
		out := make([]any, len(v))
		for i := range v {
			out[i] = v[i]
		}
		return out
	default:
		return value
	}
}

func settingsDifference(before, after map[string]any) map[string]any {
	out := map[string]any{}
	for path, value := range after {
		if !reflect.DeepEqual(canonicalValue(before[path]), canonicalValue(value)) {
			out[path] = value
		}
	}
	return out
}

func settingsFileView(c config.Config, path string, loc *i18n.Localizer) map[string]any {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	out := settingsView(c)
	out["locked_fields"] = lockedSettings(c, loc)
	out["pending_changes"] = map[string]any{}
	out["saved_values"] = out["values"]
	out["revision"] = ""
	backups := []map[string]string{}
	entries, _ := os.ReadDir(backupDirectory(c))
	for _, entry := range entries {
		if entry.IsDir() || !validBackupName(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		backups = append(backups, map[string]string{"name": entry.Name(), "created_at": info.ModTime().Format(time.RFC3339)})
	}
	sort.Slice(backups, func(i, j int) bool { return backups[i]["name"] > backups[j]["name"] })
	out["backups"] = backups
	b, err := os.ReadFile(path)
	if err != nil {
		out["config_error"] = loc.T("dashboard.config_error.unreadable")
		return out
	}
	values, err := settingsFromDocument(c, b)
	if err != nil {
		out["config_error"] = loc.T("dashboard.config_error.parse")
		return out
	}
	out["revision"] = configRevision(b)
	out["saved_values"] = values
	out["pending_changes"] = settingsDifference(out["values"].(map[string]any), values)
	return out
}

// Apply only allowlisted leaves using the same identifiers as Viper, without mutating its globals.
func configWithSettings(c config.Config, values map[string]any) (config.Config, error) {
	for path, value := range values {
		// Dashboard defaults are not part of source eligibility; changed leaves were validated above.
		if strings.HasPrefix(path, "dashboard.") {
			continue
		}
		if err := validateSetting(path, value); err != nil {
			return c, err
		}
		n := reflect.ValueOf(&c).Elem()
		for _, key := range strings.Split(path, ".") {
			if n.Kind() != reflect.Struct {
				return c, fmt.Errorf("invalid config path %s", path)
			}
			var field reflect.Value
			for i := 0; i < n.NumField(); i++ {
				if n.Type().Field(i).Tag.Get("mapstructure") == key {
					field = n.Field(i)
					break
				}
			}
			if !field.IsValid() {
				return c, fmt.Errorf("unknown config field %s", path)
			}
			n = field
		}
		if n.Kind() == reflect.Pointer {
			replacement := reflect.New(n.Type().Elem())
			n.Set(replacement)
			n = replacement.Elem()
		}
		switch n.Kind() {
		case reflect.Bool:
			n.SetBool(value.(bool))
		case reflect.String:
			n.SetString(value.(string))
		case reflect.Int, reflect.Int64:
			v, ok := asFloat(value)
			if !ok || math.Trunc(v) != v {
				return c, fmt.Errorf("%s must be an integer", path)
			}
			n.SetInt(int64(v))
		case reflect.Float64:
			n.SetFloat(value.(float64))
		case reflect.Slice:
			items := value.([]any)
			next := reflect.MakeSlice(n.Type(), len(items), len(items))
			for i := range items {
				next.Index(i).SetString(items[i].(string))
			}
			n.Set(next)
		default:
			return c, fmt.Errorf("unsupported setting %s", path)
		}
	}
	return c, nil
}

func validateSourceCombination(c config.Config, loc *i18n.Localizer) error {
	web, academic := 0, 0
	proxyResolved := c.Proxy.GetProxyEndpoint() != ""
	for _, source := range SourceCatalog(c, loc) {
		if !proxyResolved && (source.ID == "duckduckgo" || source.ID == "wikipedia" || source.ID == "googlenews" || source.ID == "semantic_scholar" || source.ID == "google_scholar") {
			continue
		}
		if source.Active {
			if source.Group == "academic" {
				academic++
			} else {
				web++
			}
		}
	}
	if web == 0 {
		return errors.New(loc.T("dashboard.validate.need_web_source"))
	}
	if c.Academic.Enabled && academic == 0 {
		return errors.New(loc.T("dashboard.validate.need_academic_source"))
	}
	return nil
}

func validBackupName(name string) bool {
	return filepath.Base(name) == name && !strings.ContainsAny(name, `/\\`) && strings.HasPrefix(name, "config-") && strings.HasSuffix(name, ".yaml")
}

// applySettingsFile 写入配置；loc 决定校验错误等响应文案的语言。
func applySettingsFile(c config.Config, req SettingsRequest, path string, clearCache func() error, loc *i18n.Localizer) (SettingsResult, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	out := SettingsResult{Changes: req.Changes, RestartRequired: true}
	if len(req.Changes) == 0 && req.RestoreBackup == "" {
		return out, fmt.Errorf("no changes supplied")
	}
	if len(req.Changes) > 0 && req.RestoreBackup != "" {
		return out, fmt.Errorf("restore and changes are mutually exclusive")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return out, fmt.Errorf("read active config: %w", err)
	}
	out.Revision = configRevision(b)
	if req.Revision != "" && req.Revision != out.Revision {
		return out, errSettingsConflict
	}
	if req.Confirm && req.Revision == "" {
		return out, fmt.Errorf("revision required; refresh settings")
	}
	values, err := settingsFromDocument(c, b)
	if err != nil {
		return out, err
	}
	if req.RestoreBackup != "" {
		if !validBackupName(req.RestoreBackup) {
			return out, fmt.Errorf("invalid backup name")
		}
		backupPath := filepath.Join(backupDirectory(c), req.RestoreBackup)
		info, err := os.Lstat(backupPath)
		if err != nil || !info.Mode().IsRegular() {
			return out, fmt.Errorf("backup unavailable")
		}
		old, err := os.ReadFile(backupPath)
		if err != nil {
			return out, fmt.Errorf("read backup failed")
		}
		oldValues, err := settingsFromDocument(c, old)
		if err != nil {
			return out, err
		}
		out.Changes = settingsDifference(values, oldValues)
	}
	locked := lockedSettings(c, loc)
	for key, value := range out.Changes {
		if reason := locked[key]; reason != "" {
			return out, fmt.Errorf("%s: %s", key, reason)
		}
		if err := validateSetting(key, value); err != nil {
			return out, err
		}
		values[key] = value
	}
	candidate, err := configWithSettings(c, values)
	if err != nil {
		return out, err
	}
	if err := validateSourceCombination(candidate, loc); err != nil {
		return out, err
	}
	if !req.Confirm {
		return out, nil
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return out, err
	}
	for key, value := range out.Changes {
		if err := setYAMLPath(&doc, strings.Split(key, "."), value); err != nil {
			return out, err
		}
	}
	next, err := yaml.Marshal(&doc)
	if err != nil {
		return out, err
	}
	if err := os.MkdirAll(backupDirectory(c), 0o700); err != nil {
		return out, err
	}
	name := "config-" + time.Now().UTC().Format("20060102-150405.000000000") + ".yaml"
	if err := os.WriteFile(filepath.Join(backupDirectory(c), name), b, 0o600); err != nil {
		return out, fmt.Errorf("write backup: %w", err)
	}
	// A bind-mounted file cannot be replaced with rename; retain its mode and verify the actual readback.
	info, err := os.Stat(path)
	if err != nil {
		return out, err
	}
	rollback := func(cause error) (SettingsResult, error) {
		if err := os.WriteFile(path, b, info.Mode().Perm()); err != nil {
			return out, fmt.Errorf("%v; rollback failed, restore backup %s", cause, name)
		}
		check, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(check, b) {
			return out, fmt.Errorf("%v; rollback readback failed, restore backup %s", cause, name)
		}
		return out, cause
	}
	latest, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(latest, b) {
		return out, errSettingsConflict
	}
	if err := os.WriteFile(path, next, info.Mode().Perm()); err != nil {
		return rollback(fmt.Errorf("write active config: %w", err))
	}
	check, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(check, next) {
		return rollback(fmt.Errorf("written config readback mismatch"))
	}
	if _, err := settingsFromDocument(c, check); err != nil {
		return rollback(err)
	}
	sourceChange := false
	// 这里只要来源的 ID 与开关事实，不消费名称/条件文案，故用进程默认语言即可。
	for _, source := range SourceCatalog(c, i18n.Default()) {
		if _, exists := out.Changes[source.ControlPath]; exists {
			sourceChange = true
		}
	}
	for _, key := range []string{"mode", "network", "apipool.engines", "academic.enabled", "doubao.version", "doubao.icp_host_only"} {
		if _, exists := out.Changes[key]; exists {
			sourceChange = true
		}
	}
	if sourceChange && clearCache != nil {
		if err := clearCache(); err != nil {
			return rollback(fmt.Errorf("clear search cache: %w", err))
		}
	}
	out.Applied, out.Backup, out.Revision = true, name, configRevision(check)
	return out, nil
}
