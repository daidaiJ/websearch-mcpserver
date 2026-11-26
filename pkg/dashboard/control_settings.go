package dashboard

import (
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"go.yaml.in/yaml/v3"

	"websearch/pkg/config"
	"websearch/pkg/i18n"
)

var settingsMu sync.Mutex
var errSettingsConflict = errors.New("configuration changed; refresh before saving")
var errSettingsUnreadable = errors.New("configuration file unavailable")
var errSettingsParse = errors.New("invalid settings configuration")

func settingsErrorMessage(loc *i18n.Localizer, err error) string {
	switch err {
	case errSettingsConflict:
		return loc.T("dashboard.settings.conflict")
	case errSettingsUnreadable:
		return loc.T("dashboard.config_error.unreadable")
	case config.ErrSettingsPathsChanged:
		return loc.T("dashboard.settings.paths_changed")
	case errSettingsParse:
		return loc.T("dashboard.config_error.parse")
	default:
		return err.Error()
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

func settingsFromDocument(c config.Config, b []byte) (map[string]any, error) {
	return projectSettingsDocuments(c, b, nil)
}

func mergeSettingLocks(dst, src map[string]string) {
	for key, reason := range src {
		dst[key] = reason
	}
}

func settingsFileView(c config.Config, path, overlayPath string, loc *i18n.Localizer) map[string]any {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	out := settingsView(c)
	locked := lockedSettings(c, loc)
	out["locked_fields"] = locked
	out["pending_changes"] = map[string]any{}
	out["saved_values"] = out["values"]
	out["revision"] = ""
	out["backups"] = settingsBackups(c)
	docs, err := readSettingsDocuments(path, overlayPath)
	if err != nil {
		out["config_error"] = loc.T("dashboard.config_error.unreadable")
		return out
	}
	saved, err := decodeSettingsAtPaths(docs.main.data, docs.overlay.data, c.SettingsFilePaths())
	if err != nil {
		out["config_error"] = settingsErrorMessage(loc, err)
		return out
	}
	mergeSettingLocks(locked, lockedSettings(saved, loc))
	for path := range locked {
		if saved.Dashboard.OverlayOverridesSetting(path) {
			delete(locked, path)
		}
	}
	values := settingsView(saved)["values"].(map[string]any)
	fieldFiles := map[string]string{}
	for path := range values {
		if isEditableSetting(path) {
			fieldFiles[path] = docs.fieldFile(path)
		}
	}
	out["field_files"] = fieldFiles
	out["revision"] = docs.revision()
	out["saved_values"] = values
	out["pending_changes"] = settingsDifference(out["values"].(map[string]any), values)
	return out
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

type settingsPatch struct {
	main, overlay map[string]any
}

func (patch settingsPatch) apply(docs settingsDocuments) (settingsDocuments, error) {
	next := docs
	for _, op := range []struct {
		file    *settingsFile
		changes map[string]any
	}{{&next.main, patch.main}, {&next.overlay, patch.overlay}} {
		if len(op.changes) == 0 {
			continue
		}
		doc, err := settingsYAMLDocument(op.file.data)
		if err != nil {
			return docs, err
		}
		keys := make([]string, 0, len(op.changes))
		for key := range op.changes {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := editYAMLSetting(doc, key, op.changes[key]); err != nil {
				return docs, err
			}
		}
		op.file.data, err = yaml.Marshal(doc)
		if err != nil {
			return docs, errSettingsParse
		}
	}
	return next, nil
}

func settingsRestore(c, currentConfig config.Config, docs settingsDocuments, before map[string]any, name string, loc *i18n.Localizer) (settingsPatch, map[string]any, map[string]string, error) {
	var patch settingsPatch
	newValues := map[string]any{}
	files := map[string]string{}
	hasMain, hasOverlay := backupFiles(name)
	if !hasMain && !hasOverlay {
		return patch, nil, nil, errors.New(loc.T("dashboard.settings.backup_invalid"))
	}
	saved := docs
	var err error
	if hasMain {
		saved.main.data, err = readBackup(backupDirectory(c), name, loc)
		if err != nil {
			return patch, nil, nil, err
		}
	}
	if hasOverlay {
		if docs.overlay.info == nil {
			return patch, nil, nil, errors.New(loc.T("dashboard.settings.overlay_unavailable"))
		}
		saved.overlay.data, err = readBackup(overlayBackupDirectory(c), name, loc)
		if err != nil {
			return patch, nil, nil, err
		}
	}
	old, err := decodeSettingsDocuments(c, saved.main.data, saved.overlay.data)
	if err != nil {
		return patch, nil, nil, errSettingsParse
	}
	difference := settingsDifference(before, settingsView(old)["values"].(map[string]any))
	locked := lockedSettings(old, loc)
	for _, file := range []struct {
		restore        bool
		before, backup []byte
		label          string
		dest           *map[string]any
	}{
		{hasMain, docs.main.data, saved.main.data, "config.yaml", &patch.main},
		{hasOverlay, docs.overlay.data, saved.overlay.data, "dashboard.yaml", &patch.overlay},
	} {
		if !file.restore {
			continue
		}
		activeDoc, err := settingsYAMLDocument(file.before)
		if err != nil {
			return patch, nil, nil, err
		}
		backupDoc, err := settingsYAMLDocument(file.backup)
		if err != nil {
			return patch, nil, nil, err
		}
		changes := map[string]any{}
		for key, desired := range difference {
			if !isEditableSetting(key) || (file.label == "dashboard.yaml" && !strings.HasPrefix(key, "dashboard.")) {
				continue
			}
			current, prior := yamlPath(activeDoc, key), yamlPath(backupDoc, key)
			if current == nil && prior == nil {
				continue
			}
			var value any
			if prior != nil {
				if anchoredSetting(prior) || prior.Decode(&value) != nil {
					return patch, nil, nil, errSettingsParse
				}
				value = canonicalValue(value)
			}
			if current != nil && prior != nil {
				var active any
				if current.Decode(&active) == nil && reflect.DeepEqual(canonicalValue(active), value) {
					continue
				}
			}
			if reason := locked[key]; reason != "" &&
				(file.label == "config.yaml" || !currentConfig.Dashboard.OverlayOverridesSetting(key)) {
				return patch, nil, nil, fmt.Errorf("%s: %s", key, reason)
			}
			// Backups can contain legacy zero or null values accepted by the loader.
			// Keep the original node and verify its effective value after patching.
			changes[key] = nil
			if prior != nil {
				changes[key] = prior
			}
			newValues[key] = desired
			files[key] = file.label
		}
		*file.dest = changes
	}
	return patch, newValues, files, nil
}

func applySettingsFile(c config.Config, req SettingsRequest, path, overlayPath string, clearCache func() error, loc *i18n.Localizer) (SettingsResult, error) {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	var out SettingsResult
	if len(req.Changes) == 0 && req.RestoreBackup == "" {
		return out, errors.New(loc.T("dashboard.settings.changes_required"))
	}
	if len(req.Changes) > 0 && req.RestoreBackup != "" {
		return out, errors.New(loc.T("dashboard.settings.restore_with_changes"))
	}
	docs, err := readSettingsDocuments(path, overlayPath)
	if err != nil {
		return out, err
	}
	saved, err := decodeSettingsAtPaths(docs.main.data, docs.overlay.data, c.SettingsFilePaths())
	if err == config.ErrSettingsPathsChanged {
		return out, err
	}
	out.Revision = docs.revision()
	if req.Revision != "" && req.Revision != out.Revision {
		return out, errSettingsConflict
	}
	if req.Confirm && req.Revision == "" {
		return out, errors.New(loc.T("dashboard.settings.revision_required"))
	}
	if err != nil {
		return out, err
	}
	before := settingsView(saved)["values"].(map[string]any)
	locked := lockedSettings(c, loc)
	mergeSettingLocks(locked, lockedSettings(saved, loc))
	files := map[string]string{}
	patch := settingsPatch{main: map[string]any{}, overlay: map[string]any{}}
	var wantRestored map[string]any
	if req.RestoreBackup != "" {
		patch, wantRestored, files, err = settingsRestore(c, saved, docs, before, req.RestoreBackup, loc)
		if err != nil {
			return out, err
		}
	} else {
		for key, value := range req.Changes {
			if !isEditableSetting(key) {
				return out, errors.New(loc.T("dashboard.settings.field_not_editable"))
			}
			if err := validateSetting(key, value, loc); err != nil {
				return out, err
			}
			files[key] = docs.fieldFile(key)
			if files[key] == "dashboard.yaml" {
				patch.overlay[key] = value
			} else {
				patch.main[key] = value
			}
		}
	}
	for key := range files {
		_, mainEdit := patch.main[key]
		if reason := locked[key]; reason != "" && (mainEdit || !saved.Dashboard.OverlayOverridesSetting(key)) {
			return out, fmt.Errorf("%s: %s", key, reason)
		}
	}
	next, err := patch.apply(docs)
	if err != nil {
		return out, err
	}
	candidate, err := decodeSettingsAtPaths(next.main.data, next.overlay.data, c.SettingsFilePaths())
	if err != nil {
		return out, err
	}
	after := settingsView(candidate)["values"].(map[string]any)
	for key, value := range wantRestored {
		if !reflect.DeepEqual(canonicalValue(after[key]), canonicalValue(value)) {
			return out, errSettingsParse
		}
	}
	// Reject effects outside the requested changes (for example YAML alias side effects).
	for key := range settingsDifference(before, after) {
		if _, changed := files[key]; !changed {
			return out, errSettingsParse
		}
	}
	validationChanges := req.Changes
	if req.RestoreBackup != "" {
		validationChanges = wantRestored
	}
	if err := validateSettingsCombination(candidate, validationChanges, loc); err != nil {
		return out, err
	}
	if err := validateSourceCombination(candidate, loc); err != nil {
		return out, err
	}
	out.Changes = map[string]any{}
	out.FieldFiles = files
	for key := range files {
		out.Changes[key] = after[key]
	}
	out.RestartRequired = next.revision() != out.Revision
	if !req.Confirm {
		return out, nil
	}
	if !settingsInvalidateSearchCache(settingsDifference(before, after)) {
		clearCache = nil
	}
	name, err := updateSettingsFiles(c, docs, next, clearCache, loc)
	if err != nil {
		return out, err
	}
	out.Applied, out.Backup, out.Revision = true, name, next.revision()
	return out, nil
}
