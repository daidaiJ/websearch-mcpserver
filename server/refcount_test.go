package server

import (
	"encoding/json"
	"net/http"
	"sync"
	"testing"
)

// ── B5：refcount 的「检查 + 更新」必须原子，重复 stop 必须幂等 ────────────────

// TestRefCount_RepeatedStopIsIdempotent 计数已为 0 时再次 -1 不得再触发关闭信号
// （旧实现在 0 上 Add(-1) 再 Store(0)，每次都重发 shutdown）。
func TestRefCount_RepeatedStopIsIdempotent(t *testing.T) {
	s, mux := newAdminMux(1)

	// 第一次归零：应触发关闭
	w := doReq(mux, http.MethodPost, "/__admin/refcount", `{"delta":-1}`, "")
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp["message"] == "" || resp["message"] == nil {
		t.Fatalf("归零时应返回 shutdown 消息, got %v", resp["message"])
	}
	select {
	case <-s.shutdownCh:
	default:
		t.Fatal("归零应触发关闭信号")
	}

	// 重复 stop：计数保持 0，且不得再发信号
	for i := range 3 {
		w = doReq(mux, http.MethodPost, "/__admin/refcount", `{"delta":-1}`, "")
		// 每次用新的 map：json.Unmarshal 到已有 map 会保留上一轮的键
		var again map[string]any
		if err := json.Unmarshal(w.Body.Bytes(), &again); err != nil {
			t.Fatalf("解析响应失败: %v", err)
		}
		if again["ref_count"] != float64(0) {
			t.Fatalf("第 %d 次重复 stop 后计数应为 0, got %v", i+2, again["ref_count"])
		}
		if again["message"] != nil {
			t.Errorf("第 %d 次重复 stop 不应再返回 shutdown 消息, got %v", i+2, again["message"])
		}
		select {
		case <-s.shutdownCh:
			t.Fatalf("第 %d 次重复 stop 不应重发关闭信号", i+2)
		default:
		}
	}
	if s.RefCount() != 0 {
		t.Errorf("计数应为 0, got %d", s.RefCount())
	}
}

// TestRefCount_ConcurrentDeltaNoDrift 并发加减不得丢更新：旧实现在 0 上
// Add(-1) 后 Store(0)，会覆盖并发请求刚加上的计数，使计数漂移。
// 起点取大值（远离 0），避免饱和语义介入，只考察「检查 + 更新」的原子性。
func TestRefCount_ConcurrentDeltaNoDrift(t *testing.T) {
	const initial = 500
	s, mux := newAdminMux(initial)

	const workers = 16
	const perWorker = 25
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range workers {
		wg.Add(1)
		go func(delta int) {
			defer wg.Done()
			<-start
			body := `{"delta":-1}`
			if delta%2 == 0 {
				body = `{"delta":1}`
			}
			for range perWorker {
				doReq(mux, http.MethodPost, "/__admin/refcount", body, "")
			}
		}(i)
	}
	close(start)
	wg.Wait()

	// 8 组 +1 与 8 组 -1，各 25 次 → 净变化 0
	if got := s.RefCount(); got != initial {
		t.Errorf("并发加减后计数应保持 %d, got %d（检查-使用非原子会漂移）", initial, got)
	}
}

// TestRefCount_NeverNegative 多次 -1 不得让计数为负。
func TestRefCount_NeverNegative(t *testing.T) {
	s, mux := newAdminMux(2)
	for range 5 {
		doReq(mux, http.MethodPost, "/__admin/refcount", `{"delta":-3}`, "")
	}
	if got := s.RefCount(); got != 0 {
		t.Errorf("计数应按饱和处理为 0, got %d", got)
	}
}
