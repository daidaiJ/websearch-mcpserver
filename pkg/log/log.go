package log

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

var defaultlog *zerolog.Logger

// 提供 Zerolog Logger
func NewLogger() *zerolog.Logger {
	// 使用 ConsoleWriter 美化输出（开发环境）
	output := zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339,
	}
	logger := zerolog.New(output).With().Timestamp().Logger()
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
