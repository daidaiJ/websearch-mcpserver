package log

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"

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
