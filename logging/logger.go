// Package logging provides logger initialization utilities.
package logging

import (
	"io"
	"log/slog"
	"os"

	"gopkg.in/natefinch/lumberjack.v2"
)

// Config holds logging settings.
type Config struct {
	Level   string      `default:"info"` // debug, info, warn, error
	Format  string      `default:"json"` // text | json
	Service string      // optional service name prefix, e.g. "pay-service" → [pay-service]
	File    *FileConfig // optional file output with rotation
}

// FileConfig holds file output and log rotation settings.
// All rotation fields map to lumberjack.Logger options.
type FileConfig struct {
	Path       string // log file path (required)
	MaxSizeMB  int    `default:"100"` // max size in MB before rotation
	MaxBackups int    `default:"3"`   // max number of old log files to keep
	MaxAgeDays int    `default:"7"`   // max days to keep old log files (0 = no limit)
	Compress   bool   // compress rotated log files with gzip
}

// prefixWriter wraps an io.Writer to prepend a prefix to each write.
// Since slog handlers emit one record per Write call, the prefix appears
// at the very start of each log line.
type prefixWriter struct {
	io.Writer
	prefix []byte
}

// Setup configures the global slog logger from Config.
func Setup(cfg *Config) {
	opts := &slog.HandlerOptions{}
	switch cfg.Level {
	case "debug":
		opts.Level = slog.LevelDebug
	case "warn":
		opts.Level = slog.LevelWarn
	case "error":
		opts.Level = slog.LevelError
	default:
		opts.Level = slog.LevelInfo
	}

	w := newWriter(cfg)

	var handler slog.Handler
	if cfg.Format == "text" {
		if cfg.Service != "" {
			w = &prefixWriter{Writer: w, prefix: []byte("[" + cfg.Service + "] ")}
		}
		handler = slog.NewTextHandler(w, opts)
	} else {
		handler = slog.NewJSONHandler(w, opts)
		if cfg.Service != "" {
			handler = handler.WithAttrs([]slog.Attr{slog.String("service", cfg.Service)})
		}
	}
	slog.SetDefault(slog.New(handler))
}

// newWriter builds the output writer.
// If File is configured, returns a multi-writer to both stdout and file;
// otherwise returns stdout only.
func newWriter(cfg *Config) io.Writer {
	if cfg.File == nil || cfg.File.Path == "" {
		return os.Stdout
	}

	file := &lumberjack.Logger{
		Filename:   cfg.File.Path,
		MaxSize:    cfg.File.MaxSizeMB,
		MaxBackups: cfg.File.MaxBackups,
		MaxAge:     cfg.File.MaxAgeDays,
		Compress:   cfg.File.Compress,
	}
	return io.MultiWriter(os.Stdout, file)
}

func (w *prefixWriter) Write(p []byte) (int, error) {
	if _, err := w.Writer.Write(w.prefix); err != nil {
		return 0, err
	}
	_, err := w.Writer.Write(p)
	return len(p), err
}
