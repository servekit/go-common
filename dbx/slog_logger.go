package dbx

import (
	"context"
	"log/slog"
	"time"

	gormlogger "gorm.io/gorm/logger"
)

// slogLogger implements gorm.Logger using log/slog.
type slogLogger struct {
	level         gormLogLevel
	slowThreshold time.Duration
}

// gormLogLevel controls the verbosity of slogLogger.
type gormLogLevel int

const (
	gormLogLevelSilent gormLogLevel = iota
	gormLogLevelError
	gormLogLevelWarn
	gormLogLevelInfo
)

func newSlogLogger(level gormLogLevel, slowThreshold time.Duration) *slogLogger {
	return &slogLogger{level: level, slowThreshold: slowThreshold}
}

func (l *slogLogger) LogMode(level gormlogger.LogLevel) gormlogger.Interface {
	var newLevel gormLogLevel
	switch level {
	case gormlogger.Silent:
		newLevel = gormLogLevelSilent
	case gormlogger.Error:
		newLevel = gormLogLevelError
	case gormlogger.Warn:
		newLevel = gormLogLevelWarn
	case gormlogger.Info:
		newLevel = gormLogLevelInfo
	default:
		newLevel = gormLogLevelWarn
	}
	return &slogLogger{level: newLevel, slowThreshold: l.slowThreshold}
}

func (l *slogLogger) Info(ctx context.Context, msg string, args ...any) {
	if l.level >= gormLogLevelInfo {
		slog.InfoContext(ctx, msg, args...)
	}
}

func (l *slogLogger) Warn(ctx context.Context, msg string, args ...any) {
	if l.level >= gormLogLevelWarn {
		slog.WarnContext(ctx, msg, args...)
	}
}

func (l *slogLogger) Error(ctx context.Context, msg string, args ...any) {
	if l.level >= gormLogLevelError {
		slog.ErrorContext(ctx, msg, args...)
	}
}

func (l *slogLogger) Trace(ctx context.Context, begin time.Time, fc func() (sql string, rowsAffected int64), err error) {
	if l.level <= gormLogLevelSilent {
		return
	}

	elapsed := time.Since(begin)
	sql, rows := fc()

	switch {
	case err != nil && l.level >= gormLogLevelError:
		slog.ErrorContext(ctx, "database error",
			"error", err,
			"duration", elapsed,
			"rows", rows,
			"sql", sql,
		)
	case elapsed > l.slowThreshold && l.level >= gormLogLevelWarn:
		slog.WarnContext(ctx, "slow query",
			"duration", elapsed,
			"rows", rows,
			"sql", sql,
		)
	case l.level >= gormLogLevelInfo:
		slog.InfoContext(ctx, "query",
			"duration", elapsed,
			"rows", rows,
			"sql", sql,
		)
	}
}
