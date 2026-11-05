package log

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/rs/zerolog"

	"websearch/pkg/config"
)

// 测试用未登记 key：i18n 对未登记 key 原样返回 key 本身（漏登记由 pkg/i18n 的
// 目录审计拦截），因此这些标记串既能当断言锚点，也顺带钉住该回退行为。

// TestCallerPointsAtCallSite caller 字段必须指向真实调用点，而非 zerolog 内部帧。
func TestCallerPointsAtCallSite(t *testing.T) {
	dir := t.TempDir()
	NewLoggerTo(io.Discard, dir, config.LogConfig{MaxSize: 1, MaxAge: 1})
	defer func() { _ = CloseFile() }()
	_, thisFile, _, _ := runtime.Caller(0)
	InfoT("log.test.caller_check")
	data, err := os.ReadFile(filepath.Join(dir, "websearch.log"))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(data, []byte("zerolog")) {
		t.Fatalf("caller points into zerolog internals: %s", data)
	}
	if !bytes.Contains(data, []byte(filepath.Base(thisFile))) {
		t.Fatalf("caller does not point at the call site (%s): %s", thisFile, data)
	}
}

func TestNewLoggerTo_DoesNotWriteStdout(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()

	var buf bytes.Buffer
	NewLoggerTo(&buf, "", config.LogConfig{})
	InfoT("log.test.stdio_safe")

	_ = w.Close()
	os.Stdout = old
	dumped, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(dumped, []byte("log.test.stdio_safe")) {
		t.Fatalf("console logger wrote to stdout: %s", dumped)
	}
	if !bytes.Contains(buf.Bytes(), []byte("log.test.stdio_safe")) {
		t.Fatalf("expected log in console writer, got %q", buf.Bytes())
	}
}

func TestNewLoggerTo_NilConsoleNoPanic(t *testing.T) {
	NewLoggerTo(nil, "", config.LogConfig{})
	InfoT("log.test.discard_ok")
	SetLoggerLevel("debug")
	DebugT("log.test.also_ok")
}

func TestNewLoggerTo_WritesFile(t *testing.T) {
	dir := t.TempDir()
	NewLoggerTo(io.Discard, dir, config.LogConfig{MaxSize: 1, MaxAge: 1})
	InfoT("log.test.file_line")
	p := filepath.Join(dir, "websearch.log")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("log.test.file_line")) {
		t.Fatalf("log file missing line: %s", data)
	}
	if err := CloseFile(); err != nil {
		t.Fatal(err)
	}
}

func TestSetLoggerLevel_NilSafe(t *testing.T) {
	defaultlog = nil
	SetLoggerLevel("debug")
	InfoT("log.test.no_panic")
}

// TestSetLoggerLevel_FiltersOutput 级别必须真的生效：zerolog 的 Level() 返回副本，
// 丢弃返回值会让进程一直按 debug 输出（含每次搜索的响应 dump）。
func TestSetLoggerLevel_FiltersOutput(t *testing.T) {
	var buf bytes.Buffer
	NewLoggerTo(&buf, "", config.LogConfig{})
	t.Cleanup(func() { defaultlog = nil })

	SetLoggerLevel("error")
	if got := defaultlog.GetLevel(); got != zerolog.ErrorLevel {
		t.Fatalf("log_level=error 后 level = %v, want %v", got, zerolog.ErrorLevel)
	}
	InfoT("log.test.level_info_filtered")
	DebugT("log.test.level_debug_filtered")
	ErrorT("log.test.level_error_kept")

	out := buf.String()
	if strings.Contains(out, "log.test.level_info_filtered") || strings.Contains(out, "log.test.level_debug_filtered") {
		t.Fatalf("info/debug 未被 error 级别过滤: %s", out)
	}
	if !strings.Contains(out, "log.test.level_error_kept") {
		t.Fatalf("error 行被误过滤: %s", out)
	}
}

// TestSetLoggerLevel_ParsesDocumentedLevels 配置文档承诺 debug/info/warn/error 四档，
// 且未配置（空串）必须落到 info —— ParseLevel("") 返回 NoLevel，等于全静音。
func TestSetLoggerLevel_ParsesDocumentedLevels(t *testing.T) {
	for lv, want := range map[string]zerolog.Level{
		"debug":   zerolog.DebugLevel,
		"DEBUG":   zerolog.DebugLevel,
		"info":    zerolog.InfoLevel,
		"warn":    zerolog.WarnLevel,
		"WARN":    zerolog.WarnLevel,
		"error":   zerolog.ErrorLevel,
		"":        zerolog.InfoLevel, // 未配置 = info，不能静音
		"verbose": zerolog.InfoLevel, // 未知值回落 info
	} {
		t.Run(lv, func(t *testing.T) {
			NewLoggerTo(io.Discard, "", config.LogConfig{})
			t.Cleanup(func() { defaultlog = nil })
			SetLoggerLevel(lv)
			if got := defaultlog.GetLevel(); got != want {
				t.Fatalf("log_level=%q → %v, want %v", lv, got, want)
			}
		})
	}
}
