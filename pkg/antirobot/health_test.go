package antirobot

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// withTempHealthStore 为单测启用落盘（临时文件），测试结束关闭并清空状态。
func withTempHealthStore(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "engine_health.json")
	SetHealthStore(path)
	t.Cleanup(func() { SetHealthStore("") })
	return path
}

func TestHealthEnterSnapshotClear(t *testing.T) {
	withTempHealthStore(t)
	until := time.Now().Add(time.Minute)

	EnterCooldown("duckduckgo", HealthKindRateLimit, "HTTP 202/429 限流", until, 20*time.Second)
	snap := HealthSnapshot()
	if len(snap) != 1 || snap[0].Engine != "duckduckgo" {
		t.Fatalf("快照应含 duckduckgo 一条: %+v", snap)
	}
	if snap[0].Kind != HealthKindRateLimit || snap[0].Cooldown != 20 {
		t.Errorf("条目字段不符: %+v", snap[0])
	}

	ClearCooldown("duckduckgo")
	if snap := HealthSnapshot(); len(snap) != 0 {
		t.Fatalf("清除后快照应为空: %+v", snap)
	}
}

func TestHealthSnapshotExcludesExpired(t *testing.T) {
	withTempHealthStore(t)
	EnterCooldown("arxiv", HealthKindRateLimit, "429", time.Now().Add(-time.Second), 30*time.Second)
	if snap := HealthSnapshot(); len(snap) != 0 {
		t.Fatalf("过期条目不应出现在活跃快照: %+v", snap)
	}
}

func TestAdoptCooldownActive(t *testing.T) {
	path := withTempHealthStore(t)
	until := time.Now().Add(10 * time.Minute)
	EnterCooldown("duckduckgo", HealthKindRateLimit, "202/429", until, 40*time.Second)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("登记后应落盘: %v", err)
	}

	gotUntil, cd, ok := AdoptCooldown("duckduckgo")
	if !ok {
		t.Fatal("应收养未过期冷却")
	}
	if time.Until(gotUntil) <= 0 {
		t.Errorf("收养的截止时间应指向未来: %v", gotUntil)
	}
	if cd != 40*time.Second {
		t.Errorf("档位应与登记一致, got %v", cd)
	}
}

func TestAdoptCooldownExpiredKeepsEscalation(t *testing.T) {
	withTempHealthStore(t)
	// 冷却已过期 1 小时、未超 24h 记忆窗：不避让，但翻倍档位续接
	EnterCooldown("arxiv", HealthKindRateLimit, "429", time.Now().Add(-time.Hour), 60*time.Second)

	until, cd, ok := AdoptCooldown("arxiv")
	if !ok {
		t.Fatal("记忆窗内应收养档位")
	}
	if !until.IsZero() {
		t.Errorf("过期条目不应要求避让, until=%v", until)
	}
	if cd != 60*time.Second {
		t.Errorf("档位应续接, got %v", cd)
	}
}

func TestAdoptCooldownBeyondRememberWindow(t *testing.T) {
	withTempHealthStore(t)
	// 超过 24h 记忆窗：落盘时即被剪除，收养不到任何状态
	EnterCooldown("arxiv", HealthKindRateLimit, "429", time.Now().Add(-25*time.Hour), 120*time.Second)

	if _, _, ok := AdoptCooldown("arxiv"); ok {
		t.Fatal("超过记忆窗不应收养")
	}
}

func TestAdoptCooldownMissingOrCorruptFile(t *testing.T) {
	path := withTempHealthStore(t)
	// 文件不存在：从零开始
	if _, _, ok := AdoptCooldown("duckduckgo"); ok {
		t.Fatal("无文件不应收养")
	}

	// 文件损坏：advisory 语义，静默视为无状态
	if err := os.WriteFile(path, []byte("{not json"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := AdoptCooldown("duckduckgo"); ok {
		t.Fatal("损坏文件不应收养")
	}
}

func TestHealthPersistRoundtrip(t *testing.T) {
	path := withTempHealthStore(t)
	EnterCooldown("bing", HealthKindChallenge, "验证码", time.Now().Add(5*time.Minute), 60*time.Second)

	rows, err := readHealthFile(path)
	if err != nil {
		t.Fatalf("读取落盘文件失败: %v", err)
	}
	row, ok := rows["bing"]
	if !ok {
		t.Fatalf("落盘应含 bing: %v", rows)
	}
	if row.Kind != HealthKindChallenge || row.Cooldown != 60 {
		t.Errorf("落盘字段不符: %+v", row)
	}
}

func TestHealthNoStoreKeepsMemoryOnly(t *testing.T) {
	SetHealthStore("") // 显式关闭（清空状态），确保无文件副作用
	EnterCooldown("duckduckgo", HealthKindRateLimit, "429", time.Now().Add(time.Minute), 20*time.Second)

	if _, _, ok := AdoptCooldown("duckduckgo"); ok {
		t.Fatal("未启用落盘时不应收养（进程内状态由引擎自持）")
	}
}

func TestHealthSortSnapshotStable(t *testing.T) {
	withTempHealthStore(t)
	for _, name := range []string{"duckduckgo", "arxiv", "baidu_web"} {
		EnterCooldown(name, HealthKindRateLimit, "429", time.Now().Add(time.Minute), time.Second)
	}
	snap := HealthSnapshot()
	if len(snap) != 3 {
		t.Fatalf("快照应含 3 条: %+v", snap)
	}
	for i := 1; i < len(snap); i++ {
		if snap[i-1].Engine > snap[i].Engine {
			t.Fatalf("快照应按引擎名排序: %+v", snap)
		}
	}
}

// TestHealthJSONShape 落盘格式为引擎名 → 条目对象（跨进程契约）。
func TestHealthJSONShape(t *testing.T) {
	path := withTempHealthStore(t)
	EnterCooldown("duckduckgo", HealthKindRateLimit, "202/429", time.Now().Add(time.Minute), 20*time.Second)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows map[string]json.RawMessage
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatalf("落盘应为 JSON 对象: %v", err)
	}
	if _, ok := rows["duckduckgo"]; !ok {
		t.Fatalf("落盘应含 duckduckgo 键: %s", data)
	}
}
