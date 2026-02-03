package logger

import (
	"log/slog"
	"os"
	"strings"
)

func Setup(level slog.Level) {
	opts := &slog.HandlerOptions{
		Level: level,
	}
	handler := slog.NewJSONHandler(os.Stdout, opts)
	slog.SetDefault(slog.New(handler))
}

func ParseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
