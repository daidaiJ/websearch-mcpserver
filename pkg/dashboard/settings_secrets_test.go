package dashboard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"websearch/pkg/config"
)

func TestSaveSecretAllowsYuanbao(t *testing.T) {
	dir := t.TempDir()
	conf := config.Config{Dashboard: config.DashboardConfig{StoragePath: filepath.Join(dir, "dashboard.db")}}
	if err := saveSecret(conf, SecretRequest{Name: "TENCENTCLOUD_WSA_APIKEY", Value: "sk-test", Confirm: true}); err != nil {
		t.Fatalf("saveSecret: %v", err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "dashboard-secrets.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"TENCENTCLOUD_WSA_APIKEY":"sk-test"`) {
		t.Fatalf("saved secrets do not contain the Yuanbao key: %s", b)
	}
}
