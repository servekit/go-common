package cronx

import (
	"log/slog"

	"github.com/robfig/cron/v3"
)

// slogLogger adapts log/slog to the cron.Logger interface.
type slogLogger struct {
	level string
}

// Compile-time interface check.
var _ cron.Logger = (*slogLogger)(nil)

func newSlogLogger(level string) *slogLogger {
	switch level {
	case "silent", "error", "info":
		return &slogLogger{level: level}
	default:
		return &slogLogger{level: "info"}
	}
}

func (l *slogLogger) Info(msg string, keysAndValues ...any) {
	if l.level != "info" {
		return
	}
	slog.Info(msg, keysAndValues...)
}

func (l *slogLogger) Error(err error, msg string, keysAndValues ...any) {
	if l.level == "silent" {
		return
	}
	kv := make([]any, len(keysAndValues), len(keysAndValues)+2)
	copy(kv, keysAndValues)
	kv = append(kv, "error", err)
	slog.Error(msg, kv...)
}
