package llm

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"websearch/pkg/config"
)

// sseChunk 构造一个 OpenAI-compatible SSE data 行。
func sseChunk(content string) string {
	return fmt.Sprintf(`data: {"id":"x","object":"chat.completion.chunk","choices":[{"delta":{"content":%q},"index":0}]}`, content)
}

func TestChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"choices":[{"message":{"role":"assistant","content":"hello"}}]}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key", "test-model")
	got, err := c.Chat("sys", "user")
	if err != nil {
		t.Fatalf("Chat 应成功, got err=%v", err)
	}
	if got != "hello" {
		t.Errorf("expected hello, got %q", got)
	}
}

func TestChatContextTimeout(t *testing.T) {
	// 用不 Accept 的原始 listener 模拟黑洞上游：连接进入 backlog 后永不响应，
	// 避免 httptest 的 Close 等待挂起的 handler（POST+body 超时后连接不会关闭）。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	c := NewClient("http://"+ln.Addr().String(), "test-key", "test-model")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = c.ChatWithContext(ctx, "sys", "user")
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("ctx 超时后应返回错误")
	}
	if elapsed >= time.Second {
		t.Errorf("ctx 取消应 < 1s, got %v", elapsed)
	}
}

func TestChatStream(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 验证请求体带 stream: true
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		for _, line := range []string{
			sseChunk("Hello"),
			sseChunk(" world"),
			sseChunk("，流式"),
			`data: {"id":"x","object":"chat.completion.chunk","choices":[{"delta":{},"finish_reason":"stop"}]}`,
			"data: [DONE]",
		} {
			fmt.Fprintf(w, "%s\n\n", line)
			flusher.Flush()
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key", "test-model")
	ch := make(chan string, 16)
	errCh := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go c.ChatStream(ctx, "sys", "user", ch, errCh)

	var tokens []string
	for tok := range ch {
		tokens = append(tokens, tok)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("ChatStream 应成功, got err=%v", err)
	}
	got := strings.Join(tokens, "")
	if got != "Hello world，流式" {
		t.Errorf("token 拼接结果错误: %q", got)
	}
}

func TestChatStreamHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key", "test-model")
	ch := make(chan string, 16)
	errCh := make(chan error, 1)
	go c.ChatStream(context.Background(), "sys", "user", ch, errCh)

	for range ch {
	} // 消费直到关闭
	err := <-errCh
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("应返回 500 错误, got %v", err)
	}
}

func TestChatStreamContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		// 持续输出（不结束），等待客户端取消
		for i := 0; i < 100; i++ {
			fmt.Fprintf(w, "%s\n\n", sseChunk("tick"))
			flusher.Flush()
			time.Sleep(20 * time.Millisecond)
			select {
			case <-r.Context().Done():
				return
			default:
			}
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key", "test-model")
	ch := make(chan string, 16)
	errCh := make(chan error, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go c.ChatStream(ctx, "sys", "user", ch, errCh)

	// 收到若干 token 后取消
	received := 0
	for range ch {
		received++
		if received >= 2 {
			cancel()
			break
		}
	}
	// 消费剩余直到关闭
	for range ch {
	}
	err := <-errCh
	if err == nil {
		t.Fatal("ctx 取消后应返回错误")
	}
}

func TestChatStreamMalformedLine(t *testing.T) {
	// 含非 JSON data 行应被跳过而非中断
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher, _ := w.(http.Flusher)
		fmt.Fprintf(w, "%s\n\n", "data: not-json")
		fmt.Fprintf(w, "%s\n\n", sseChunk("ok"))
		fmt.Fprintf(w, "%s\n\n", "data: [DONE]")
		flusher.Flush()
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "test-key", "test-model")
	ch := make(chan string, 16)
	errCh := make(chan error, 1)
	go c.ChatStream(context.Background(), "sys", "user", ch, errCh)

	var tokens []string
	for tok := range ch {
		tokens = append(tokens, tok)
	}
	if err := <-errCh; err != nil {
		t.Fatalf("ChatStream 应成功, got err=%v", err)
	}
	if len(tokens) != 1 || tokens[0] != "ok" {
		t.Errorf("非 JSON 行应被跳过, got %v", tokens)
	}
}

func TestClientWithDeadline(t *testing.T) {
	// 与 TestChatContextTimeout 同款黑洞上游：验证 NewClientWithTimeout 在
	// 调用方 ctx 无 deadline 时也会按时失败（N2 有界 deadline）。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	c := NewClientWithTimeout("http://"+ln.Addr().String(), "test-key", "test-model", 150*time.Millisecond)
	start := time.Now()
	if _, err = c.ChatWithContext(context.Background(), "sys", "user"); err == nil {
		t.Fatal("client 超时后应返回错误")
	}
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Errorf("client 超时应 < 1s, got %v", elapsed)
	}

	// 流式路径同样受 deadline 约束
	ch := make(chan string, 4)
	errCh := make(chan error, 1)
	start = time.Now()
	go c.ChatStream(context.Background(), "sys", "user", ch, errCh)
	for range ch {
	}
	if err = <-errCh; err == nil {
		t.Fatal("流式 client 超时后应返回错误")
	}
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Errorf("流式 client 超时应 < 1s, got %v", elapsed)
	}
}

func TestClientDeadlineRespectsCallerCtx(t *testing.T) {
	// 调用方 ctx 已带更早 deadline 时不被覆盖：取更早者。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	c := NewClientWithTimeout("http://"+ln.Addr().String(), "test-key", "test-model", time.Hour)
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	start := time.Now()
	if _, err = c.ChatWithContext(ctx, "sys", "user"); err == nil {
		t.Fatal("调用方 deadline 触发后应返回错误")
	}
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Errorf("应遵循调用方更早的 deadline, got %v", elapsed)
	}
}

func TestClientDeadlineOverridesLaterCallerCtx(t *testing.T) {
	// 调用方 ctx deadline 晚于本端超时时，本端超时仍生效（两者取更早者）。
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	c := NewClientWithTimeout("http://"+ln.Addr().String(), "test-key", "test-model", 150*time.Millisecond)
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	start := time.Now()
	if _, err = c.ChatWithContext(ctx, "sys", "user"); err == nil {
		t.Fatal("本端超时触发后应返回错误")
	}
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Errorf("本端 150ms 超时应生效, got %v", elapsed)
	}
}

func TestLLMConfigGetTimeout(t *testing.T) {
	if got := (config.LLMConfig{}).GetTimeout(); got != 60*time.Second {
		t.Fatalf("default timeout = %v, want 60s", got)
	}
	if got := (config.LLMConfig{TimeoutSec: 90}).GetTimeout(); got != 90*time.Second {
		t.Fatalf("timeout = %v, want 90s", got)
	}
	if got := (config.LLMConfig{TimeoutSec: -1}).GetTimeout(); got != 60*time.Second {
		t.Fatalf("negative timeout should fall back to default, got %v", got)
	}
}
