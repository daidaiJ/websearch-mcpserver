package log

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
	"websearch/pkg/config"
	"websearch/pkg/i18n"
)

var (
	defaultlog *zerolog.Logger
	fileWriter io.Closer
)

// NewLogger 同时输出到 stdout 控制台和滚动日志文件（HTTP daemon 使用）。
func NewLogger(logDir string, logConf config.LogConfig) *zerolog.Logger {
	return NewLoggerTo(os.Stdout, logDir, logConf)
}

// NewLoggerTo 将控制台日志写到 console（stdio CLI 应传 os.Stderr，避免污染 JSON-RPC）。
// console 为 nil 时不写控制台；logDir 为空时不写文件。
func NewLoggerTo(console io.Writer, logDir string, logConf config.LogConfig) *zerolog.Logger {
	_ = CloseFile()

	var writers []io.Writer
	if console != nil {
		writers = append(writers, zerolog.ConsoleWriter{
			Out:        console,
			TimeFormat: time.RFC3339,
		})
	}

	if logDir != "" {
		fw := &lumberjack.Logger{
			Filename:   filepath.Join(logDir, "websearch.log"),
			MaxSize:    logConf.MaxSize, // MB
			MaxAge:     logConf.MaxAge,  // days
			MaxBackups: 0,
			Compress:   false,
			LocalTime:  true,
		}
		fileWriter = fw
		writers = append(writers, fw)
	}

	if len(writers) == 0 {
		writers = append(writers, io.Discard)
	}

	multiWriter := zerolog.MultiLevelWriter(writers...)
	// CallerWithSkipFrameCount(3)：zerolog caller hook 自带 2 层内部帧，
	// 本仓库日志一律经包级 helper（Info/Infof/…）再进 zerolog，多 1 层；
	// 3 才能让 caller 指向真实调用点，1 会落进 zerolog 内部（event.go）。
	logger := zerolog.New(multiWriter).With().CallerWithSkipFrameCount(3).Timestamp().Logger()
	defaultlog = &logger
	return defaultlog
}

// CloseFile 关闭滚动日志文件句柄（Windows 上 TempDir/进程退出前需调用，否则文件会被占用）。
func CloseFile() error {
	if fileWriter == nil {
		return nil
	}
	err := fileWriter.Close()
	fileWriter = nil
	return err
}

func SetLoggerLevel(lv string) {
	if defaultlog == nil {
		return
	}
	level := zerolog.InfoLevel
	switch lv {
	case "DEBUG", "debug":
		level = zerolog.DebugLevel
	default:

	}
	defaultlog.Level(level)
}

// 日志文案走 pkg/i18n 的符号化 key：文案住在 pkg/i18n/locales/log.<语言>.json，
// 语言取进程默认（入口在 config.Load 之后经 i18n.Configure 注入一次）。
// kv 是成对出现的模板数据（名称, 值），对应文案里的 {{.名称}}；无占位符时省略。
// key 漏登记由 pkg/i18n 的目录审计测试拦截，不在运行期兜底。

func DebugT(key string, kv ...any) {
	if defaultlog != nil {
		defaultlog.Debug().Msg(i18n.Default().T(key, kv...))
	}
}

func InfoT(key string, kv ...any) {
	if defaultlog != nil {
		defaultlog.Info().Msg(i18n.Default().T(key, kv...))
	}
}

func WarnT(key string, kv ...any) {
	if defaultlog != nil {
		defaultlog.Warn().Msg(i18n.Default().T(key, kv...))
	}
}

func ErrorT(key string, kv ...any) {
	if defaultlog != nil {
		defaultlog.Error().Msg(i18n.Default().T(key, kv...))
	}
}
