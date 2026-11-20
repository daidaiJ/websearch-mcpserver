package mojeek

import (
	"crypto/pbkdf2"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"websearch/internal/testenv"
	"websearch/pkg/antirobot"
)

const mojeekFixtureHTML = `<!DOCTYPE html><html><body>
<ul class="results-standard">
  <li><a class="ob" href="https://example.com/a"></a><h2><a href="https://example.com/a">Title A</a></h2><p class="s">Snippet A</p></li>
  <li><a class="ob" href="/b"></a><h2><a href="/b">Title B</a></h2><p class="s">Snippet B</p></li>
  <li><h2><a href="https://example.com/no-ob">No ob link</a></h2></li>
</ul>
</body></html>`

func newTestEngine(blocked []string) antirobot.Engine {
	return NewMojeek(MojeekOpts{Enabled: true, Blocked: blocked})
}

func TestMojeek_ParseResults(t *testing.T) {
	e := newTestEngine(nil).(*mojeekEngine)
	results := e.parseResults(mojeekFixtureHTML)

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d: %+v", len(results), results)
	}
	if results[0].URL != "https://example.com/a" || results[0].Title != "Title A" || results[0].Content != "Snippet A" {
		t.Errorf("first result mismatch: %+v", results[0])
	}
	if results[1].URL != "https://www.mojeek.com/b" {
		t.Errorf("relative URL should resolve against base, got %q", results[1].URL)
	}
	if results[0].Engine != "mojeek" || results[0].Type != antirobot.ResultWeb {
		t.Errorf("engine/type mismatch: %+v", results[0])
	}
}

func TestMojeek_BuildURL(t *testing.T) {
	e := newTestEngine(nil).(*mojeekEngine)

	u := e.buildURL("golang", 1, antirobot.TimeRangeNone)
	if !strings.HasPrefix(u, "https://www.mojeek.com/search?") {
		t.Fatalf("base url mismatch: %s", u)
	}
	if !strings.Contains(u, "q=golang") || !strings.Contains(u, "safe=0") {
		t.Errorf("query/safe param mismatch: %s", u)
	}
	if strings.Contains(u, "s=") || strings.Contains(u, "since=") {
		t.Errorf("第一页不应带 s/since: %s", u)
	}

	u2 := e.buildURL("golang", 2, antirobot.TimeRangeWeek)
	if !strings.Contains(u2, "s=10") || !strings.Contains(u2, "since=") {
		t.Errorf("分页/时间范围参数缺失: %s", u2)
	}

	e2 := NewMojeek(MojeekOpts{Enabled: true, SafeSearch: 2}).(*mojeekEngine)
	if u3 := e2.buildURL("q", 1, antirobot.TimeRangeNone); !strings.Contains(u3, "safe=1") {
		t.Errorf("SafeSearch 开启应带 safe=1: %s", u3)
	}
}

func TestSolveAltcha(t *testing.T) {
	nonce := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	salt := []byte{0x11, 0x12, 0x13, 0x14, 0x15, 0x16, 0x17, 0x18}
	const wantCounter = 3
	const cost, keyLength = 10, 16

	counterBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(counterBytes, wantCounter)
	secret := append(append([]byte{}, nonce...), counterBytes...)
	key, err := pbkdf2.Key(sha256.New, string(secret), salt, cost, keyLength)
	if err != nil {
		t.Fatal(err)
	}

	params := altchaParameters{
		Algorithm: "PBKDF2/SHA-256",
		Cost:      cost,
		KeyLength: keyLength,
		KeyPrefix: hex.EncodeToString(key[:8]),
		Nonce:     hex.EncodeToString(nonce),
		Salt:      hex.EncodeToString(salt),
	}
	derived, counter, err := solveAltcha(params)
	if err != nil {
		t.Fatalf("solveAltcha: %v", err)
	}
	if counter != wantCounter {
		t.Errorf("counter = %d, want %d", counter, wantCounter)
	}
	if derived != hex.EncodeToString(key) {
		t.Errorf("derivedKey mismatch")
	}
}

func TestMojeek_SearchWithAltcha(t *testing.T) {
	origBase, origJitter := mojeekBaseDelay, mojeekJitter
	mojeekBaseDelay, mojeekJitter = time.Millisecond, time.Millisecond
	t.Cleanup(func() {
		mojeekBaseDelay, mojeekJitter = origBase, origJitter
	})

	nonce := []byte{0x21, 0x22, 0x23, 0x24, 0x25, 0x26, 0x27, 0x28}
	salt := []byte{0x31, 0x32, 0x33, 0x34, 0x35, 0x36, 0x37, 0x38}
	const cost, keyLength = 10, 16
	secret := append(append([]byte{}, nonce...), 0, 0, 0, 0)
	key, err := pbkdf2.Key(sha256.New, string(secret), salt, cost, keyLength)
	if err != nil {
		t.Fatal(err)
	}

	challenge := map[string]any{
		"parameters": map[string]any{
			"algorithm": "PBKDF2/SHA-256",
			"cost":      cost,
			"keyLength": keyLength,
			"keyPrefix": hex.EncodeToString(key[:8]),
			"nonce":     hex.EncodeToString(nonce),
			"salt":      hex.EncodeToString(salt),
		},
		"signature": "test-signature",
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/captcha/challenge":
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(challenge)
		case "/captcha/verify":
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				t.Errorf("multipart parse: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			raw, err := base64.StdEncoding.DecodeString(r.FormValue("altcha"))
			if err != nil {
				t.Errorf("altcha base64: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			var sol altchaSolution
			if err := json.Unmarshal(raw, &sol); err != nil {
				t.Errorf("altcha json: %v", err)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			if sol.Solution.Counter != 0 || sol.Solution.DerivedKey != hex.EncodeToString(key) {
				t.Errorf("bad solution: %+v", sol.Solution)
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			http.SetCookie(w, &http.Cookie{Name: "chllg", Value: "test-token", Path: "/"})
			w.WriteHeader(http.StatusOK)
		case "/search":
			if !strings.Contains(r.Header.Get("Cookie"), "chllg=test-token") {
				_, _ = io.WriteString(w, `<html><head><title>Captcha</title></head><body>altcha</body></html>`)
				return
			}
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = io.WriteString(w, mojeekFixtureHTML)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	e := NewMojeek(MojeekOpts{Enabled: true, PerSec: 1000, PerMin: 10000}).(*mojeekEngine)
	e.baseURL = srv.URL
	e.client = srv.Client()

	resp, err := e.Search("golang", 1, antirobot.TimeRangeNone)
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(resp.Results))
	}
	if e.token != "test-token" {
		t.Errorf("token should be cached, got %q", e.token)
	}
}

func TestMojeek_ChallengeDetection(t *testing.T) {
	if !isMojeekChallenge(`<html><head><title>Captcha</title></head><body>altcha</body></html>`) {
		t.Error("altcha page should be detected")
	}
	if isMojeekChallenge(mojeekFixtureHTML) {
		t.Error("normal results must not be treated as challenge")
	}
}

// TestMojeek_SearchIntegration 真网集成：altcha 求解 + 解析契约对真实结果页生效。
// 网络场景自动跳过；挑战未通过或出口被拦截时归一为场景错误。
func TestMojeek_SearchIntegration(t *testing.T) {
	testenv.Require(t, testenv.Mojeek)
	e := NewMojeek(MojeekOpts{Enabled: true, PerSec: 1000, PerMin: 100000})
	resp, err := e.Search("golang goroutine tutorial", 1, antirobot.TimeRangeNone)
	if testenv.HandleSearchError(t, err) {
		return
	}
	if err != nil {
		t.Fatalf("搜索失败: %v", err)
	}
	if resp == nil || len(resp.Results) == 0 {
		t.Fatalf("expected results, got %+v", resp)
	}
	for _, r := range resp.Results {
		if r.Title == "" || r.URL == "" {
			t.Fatalf("结果字段不完整: %+v", r)
		}
	}
	t.Logf("mojeek 真网返回 %d 条结果，首条: %s %s", len(resp.Results), resp.Results[0].Title, resp.Results[0].URL)
}
