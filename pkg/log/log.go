package log

import (
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog"
	"gopkg.in/natefinch/lumberjack.v2"
	"websearch/pkg/config"
)

var defaultlog *zerolog.Logger

// 提供 Zerolog Logger，同时输出到控制台和滚动日志文件
func NewLogger(logDir string, logConf config.LogConfig) *zerolog.Logger {
	consoleWriter := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}

	var writers []io.Writer
	writers = append(writers, consoleWriter)

	if logDir != "" {
		fileWriter := &lumberjack.Logger{
			Filename:   filepath.Join(logDir, "websearch.log"),
			MaxSize:    logConf.MaxSize, // MB
			MaxAge:     logConf.MaxAge,  // days
			MaxBackups: 0,
			Compress:   false,
			LocalTime:  true,
		}
		writers = append(writers, fileWriter)
	}

	multiWriter := zerolog.MultiLevelWriter(writers...)
	logger := zerolog.New(multiWriter).With().CallerWithSkipFrameCount(1).Timestamp().Logger()
	defaultlog = &logger
	return defaultlog
}

func SetLoggerLevel(lv string) {
	level := zerolog.InfoLevel
	switch lv {
	case "DEBUG", "debug":
		level = zerolog.DebugLevel
	default:

	}
	defaultlog.Level(level)
}

func Debug(msg string) {
	if defaultlog != nil {
		defaultlog.Debug().Msg(msg)
	}
}

func Debugf(pattern string, v ...any) {
	if defaultlog != nil {
		defaultlog.Debug().Msgf(pattern, v...)
	}
}

func Info(msg string) {
	if defaultlog != nil {
		defaultlog.Info().Msg(msg)
	}
}

func Infof(pattern string, v ...any) {
	if defaultlog != nil {
		defaultlog.Info().Msgf(pattern, v...)
	}
}

func Errf(pattern string, v ...any) {
	if defaultlog != nil {
		defaultlog.Error().Msgf(pattern, v...)
	}
}

func Error(msg string) {
	if defaultlog != nil {
		defaultlog.Error().Msg(msg)
	}
}
