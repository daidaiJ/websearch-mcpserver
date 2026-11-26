package dashboard

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"websearch/pkg/config"
	"websearch/pkg/history"
)

func TestStorageHistoryClear(t *testing.T) {
	conf := config.Config{}
	conf.CleanFetch.FileOutputDir = t.TempDir()
	conf.SearchHistory.StoragePath = filepath.Join(t.TempDir(), "history.db")
	conf.Dashboard.AdminPassword = "pw"
	store, err := history.New(conf.GetSearchHistoryStoragePath(), 30)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(store.Close)
	if err := store.Insert("storageprivatefixture", "web", nil); err != nil {
		t.Fatal(err)
	}
	handler, mux := newWriteTestHandler(t, conf)
	handler.history = store

	const stats = "/__admin/api/storage"
	const clear = "/__admin/api/history/clear"
	read := doWrite(t, mux, http.MethodGet, stats, "", "192.168.1.9:5", "")
	if read.Code != http.StatusOK || !strings.Contains(read.Body.String(), `"history":{"enabled":true,"count":1,"bytes":`) || strings.Contains(read.Body.String(), "storageprivatefixture") {
		t.Fatalf("aggregate read: %d %s", read.Code, read.Body)
	}
	for _, tc := range []struct {
		method, body, remote, password string
		want                           int
	}{
		{http.MethodGet, "", "", "pw", http.StatusMethodNotAllowed},
		{http.MethodPost, `{"confirm":true}`, "192.168.1.9:5", "pw", http.StatusForbidden},
		{http.MethodPost, `{"confirm":false}`, "", "pw", http.StatusBadRequest},
	} {
		res := doWrite(t, mux, tc.method, clear, tc.body, tc.remote, tc.password)
		if res.Code != tc.want {
			t.Fatalf("%+v: got %d %s", tc, res.Code, res.Body)
		}
	}
	if matches, err := store.Search("storageprivatefixture", 10, 0); err != nil || len(matches) != 1 {
		t.Fatalf("blocked clear changed history: %v %v", matches, err)
	}
	res := doWrite(t, mux, http.MethodPost, clear, `{"confirm":true}`, "", "")
	if res.Code != http.StatusOK || !strings.Contains(res.Body.String(), `"cleared":1`) {
		t.Fatalf("clear: %d %s", res.Code, res.Body)
	}
	if matches, err := store.Search("storageprivatefixture", 10, 0); err != nil || len(matches) != 0 {
		t.Fatalf("cleared search records still visible: %v %v", matches, err)
	}
}

func TestStorageTemporaryFilesClear(t *testing.T) {
	dir := t.TempDir()
	conf := config.Config{}
	conf.CleanFetch.FileOutputDir = dir
	conf.CleanFetch.FileTTL = 2
	conf.Dashboard.AdminPassword = "pw"
	_, mux := newWriteTestHandler(t, conf)

	const expired = "20260101_010101_article_123abc.md"
	const unexpired = "search-20260101_010101-1234abcd.md"
	const recent = "20260101_010101_recent_123abc.md"
	const unrelated = "my-notes.md"
	const nested = "mineru-local-tasks/search-20260101_010101-abcdef12.md"
	now := time.Now()
	files := map[string]time.Duration{expired: -3 * time.Hour, unexpired: -3 * time.Hour, recent: 0, unrelated: -24 * time.Hour, nested: -24 * time.Hour}
	writeFixture := func(dir, name string, age time.Duration) {
		t.Helper()
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(path, now.Add(age), now.Add(age)); err != nil {
			t.Fatal(err)
		}
	}
	for name, age := range files {
		writeFixture(dir, name, age)
	}

	outside := t.TempDir()
	writeFixture(outside, expired, -24*time.Hour)
	linkName := filepath.Join(dir, "search-20260101_010101-deadbeef.md")
	if err := os.Symlink(filepath.Join(outside, expired), linkName); err == nil {
		t.Cleanup(func() {
			if info, err := os.Lstat(linkName); err != nil || info.Mode()&os.ModeSymlink == 0 {
				t.Errorf("linked entry was altered: %v", err)
			}
		})
	} else {
		t.Logf("symlink fixture unavailable: %v", err)
	}

	res := doWrite(t, mux, http.MethodGet, "/__admin/api/storage", "", "", "")
	var stats map[string]struct {
		Enabled               bool
		Count, Bytes, Expired int64
		Clearable             int64
	}
	if err := json.Unmarshal(res.Body.Bytes(), &stats); err != nil || res.Code != http.StatusOK {
		t.Fatalf("stats: %d %s %v", res.Code, res.Body, err)
	}
	if got := stats["fetchdata"]; !got.Enabled || got.Count != 3 || got.Bytes != 9 || got.Expired != 1 || got.Clearable != 2 {
		t.Fatalf("stats: %s", res.Body)
	}
	const path = "/__admin/api/fetchdata/clear"
	for _, tc := range []struct {
		body, pass string
		code       int
		want       string
	}{
		{`{"confirm":false,"scope":"all"}`, "", http.StatusBadRequest, ""},
		{`{"confirm":true,"scope":"unknown"}`, "pw", http.StatusBadRequest, ""},
		{`{"confirm":true,"scope":"expired"}`, "", http.StatusOK, `"cleared":1`},
		{`{"confirm":true,"scope":"expired"}`, "", http.StatusOK, `"cleared":0`},
		{`{"confirm":true,"scope":"all"}`, "", http.StatusOK, `"cleared":1`},
	} {
		got := doWrite(t, mux, http.MethodPost, path, tc.body, "", tc.pass)
		if got.Code != tc.code || !strings.Contains(got.Body.String(), tc.want) {
			t.Fatalf("clear: %d %s", got.Code, got.Body)
		}
	}
	for _, filename := range []string{expired, unexpired} {
		if _, err := os.Lstat(filepath.Join(dir, filename)); !os.IsNotExist(err) {
			t.Errorf("expected deletion of %s: %v", filename, err)
		}
	}
	for _, filename := range []string{recent, unrelated, nested} {
		if _, err := os.Stat(filepath.Join(dir, filename)); err != nil {
			t.Errorf("unexpected change to %s: %v", filename, err)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, expired)); err != nil {
		t.Fatalf("external link target changed: %v", err)
	}
}

func TestStorageRejectsLinkedOutputRoots(t *testing.T) {
	target := t.TempDir()
	linked := filepath.Join(t.TempDir(), "linked")
	if err := os.Symlink(target, linked); err != nil {
		t.Skipf("directory symlink fixture unavailable: %v", err)
	}
	nested := filepath.Join(target, "nested")
	if err := os.Mkdir(nested, 0700); err != nil {
		t.Fatal(err)
	}
	const filename = "search-20260101_010101-1234abcd.md"
	for _, dir := range []string{target, nested} {
		path := filepath.Join(dir, filename)
		if err := os.WriteFile(path, []byte("abc"), 0600); err != nil {
			t.Fatal(err)
		}
		old := time.Now().Add(-8 * 24 * time.Hour)
		if err := os.Chtimes(path, old, old); err != nil {
			t.Fatal(err)
		}
	}
	for _, dir := range []string{linked, filepath.Join(linked, "nested")} {
		conf := config.Config{}
		conf.CleanFetch.FileOutputDir = dir
		conf.Dashboard.AdminPassword = "pw"
		_, mux := newWriteTestHandler(t, conf)
		res := doWrite(t, mux, http.MethodPost, "/__admin/api/fetchdata/clear", `{"confirm":true,"scope":"all"}`, "", "pw")
		if res.Code != http.StatusInternalServerError || !strings.Contains(res.Body.String(), `"cleared":0`) {
			t.Fatalf("linked root: %d %s", res.Code, res.Body)
		}
	}
	for _, dir := range []string{target, nested} {
		if _, err := os.Stat(filepath.Join(dir, filename)); err != nil {
			t.Errorf("file behind linked directory was altered: %v", err)
		}
	}
}
