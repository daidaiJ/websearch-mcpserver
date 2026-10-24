//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"websearch/pkg/config"
)

func TestShortcutSpecMatches(t *testing.T) {
	want := shortcutSpec{
		Target:     `C:\Program Files\WebSearch\websearch-mcpserver.exe`,
		Arguments:  "open",
		WorkingDir: `C:\Program Files\WebSearch`,
		IconPath:   `C:\Program Files\WebSearch\websearch-green-1a2b3c4d.ico`,
	}
	cases := []struct {
		name string
		got  shortcutSpec
		ok   bool
	}{
		{"identical", want, true},
		{"case-insensitive paths", shortcutSpec{
			Target:     `c:\program files\websearch\WEBSEARCH-MCPSERVER.EXE`,
			Arguments:  " open ",
			WorkingDir: `c:\program files\websearch`,
			IconPath:   `C:\Program Files\WebSearch\WEBSEARCH-GREEN-1A2B3C4D.ICO`,
		}, true},
		{"renamed executable", shortcutSpec{
			Target:     `C:\Program Files\WebSearch\renamed.exe`,
			Arguments:  "open",
			WorkingDir: `C:\Program Files\WebSearch`,
			IconPath:   want.IconPath,
		}, false},
		{"changed working directory", shortcutSpec{
			Target:     want.Target,
			Arguments:  "open",
			WorkingDir: `C:\Temp`,
			IconPath:   want.IconPath,
		}, false},
		{"changed arguments", shortcutSpec{
			Target:     want.Target,
			Arguments:  "start",
			WorkingDir: want.WorkingDir,
			IconPath:   want.IconPath,
		}, false},
		{"changed icon", shortcutSpec{
			Target:     want.Target,
			Arguments:  "open",
			WorkingDir: want.WorkingDir,
			IconPath:   `C:\Program Files\WebSearch\old.ico`,
		}, false},
		{"changed icon index", shortcutSpec{
			Target:     want.Target,
			Arguments:  "open",
			WorkingDir: want.WorkingDir,
			IconPath:   want.IconPath,
			IconIndex:  3,
		}, false},
		{"missing icon location", shortcutSpec{
			Target:     want.Target,
			Arguments:  "open",
			WorkingDir: want.WorkingDir,
		}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.got.matches(want); got != tc.ok {
				t.Fatalf("matches() = %v, want %v", got, tc.ok)
			}
		})
	}
}

func TestShortcutIconPathHashesContent(t *testing.T) {
	dir := `C:\Program Files\WebSearch`
	a := shortcutIconPath(dir, "green", []byte("icon-v1"))
	b := shortcutIconPath(dir, "green", []byte("icon-v1"))
	c := shortcutIconPath(dir, "green", []byte("icon-v2"))
	d := shortcutIconPath(dir, "custom", []byte("icon-v1"))

	if a != b {
		t.Fatalf("same content produced different paths: %q vs %q", a, b)
	}
	if a == c {
		t.Fatal("changed icon content kept the same file name, Windows icon cache may go stale")
	}
	if a == d {
		t.Fatal("theme and custom icon paths must differ")
	}
	if base := filepath.Base(a); !strings.HasPrefix(base, "websearch-green-") || !strings.HasSuffix(base, ".ico") {
		t.Fatalf("unexpected icon file name %q", base)
	}
	if hash := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(a), "websearch-green-"), ".ico"); len(hash) != 8 {
		t.Fatalf("icon hash length = %d, want 8 (%q)", len(hash), filepath.Base(a))
	}
}

func TestShortcutLocationsByMode(t *testing.T) {
	cases := []struct {
		mode   string
		labels []string
		paths  []string
	}{
		{"desktop", []string{"desktop"}, []string{desktopShortcutPath()}},
		{"start", []string{"start menu"}, []string{startMenuShortcutPath()}},
		{"both", []string{"desktop", "start menu"}, []string{desktopShortcutPath(), startMenuShortcutPath()}},
		{"", []string{"desktop"}, []string{desktopShortcutPath()}},
		{"unknown", []string{"desktop"}, []string{desktopShortcutPath()}},
	}
	for _, tc := range cases {
		got := shortcutLocations(tc.mode)
		if len(got) != len(tc.labels) {
			t.Fatalf("mode %q returned %d locations, want %d", tc.mode, len(got), len(tc.labels))
		}
		for i := range got {
			if got[i].Label != tc.labels[i] || got[i].Path != tc.paths[i] {
				t.Fatalf("mode %q location %d = %+v, want label %q path %q", tc.mode, i, got[i], tc.labels[i], tc.paths[i])
			}
		}
	}
}

func TestDesiredShortcutMode(t *testing.T) {
	off := &config.Config{}
	off.Dashboard.Enabled = false
	off.Dashboard.Shortcut = "both"

	disabledOff := &config.Config{}
	disabledOff.Dashboard.Enabled = true
	disabledOff.Dashboard.Shortcut = "off"

	both := &config.Config{}
	both.Dashboard.Enabled = true
	both.Dashboard.Shortcut = "both"

	empty := &config.Config{}
	empty.Dashboard.Enabled = true

	cases := []struct {
		name string
		conf *config.Config
		want string
	}{
		{"nil config", nil, "off"},
		{"dashboard disabled", off, "off"},
		{"explicit off", disabledOff, "off"},
		{"both", both, "both"},
		{"empty defaults to desktop", empty, "desktop"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := desiredShortcutMode(tc.conf); got != tc.want {
				t.Fatalf("desiredShortcutMode() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestShortcutIconData(t *testing.T) {
	if len(themeICO("green")) == 0 {
		t.Fatal("green theme icon is empty")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "brand.ico")
	if err := os.WriteFile(path, []byte("custom-icon"), 0o644); err != nil {
		t.Fatal(err)
	}
	data, err := shortcutIconData("green", path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "custom-icon" {
		t.Fatalf("custom icon data = %q", data)
	}
	if _, err := shortcutIconData("green", filepath.Join(dir, "missing.ico")); err == nil {
		t.Fatal("missing brand.icon should return an error")
	}
}
