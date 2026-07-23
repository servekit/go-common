package cronx

import (
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func captureSlog(t *testing.T) *strings.Builder {
	t.Helper()
	var buf strings.Builder
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	})
	return &buf
}

func TestSlogLogger_SilentDiscards(t *testing.T) {
	logger := newSlogLogger("silent")
	buf := captureSlog(t)

	logger.Info("should not appear", "key", "value")
	logger.Error(errors.New("err"), "should not appear either")

	assert.Empty(t, buf.String())
}

func TestSlogLogger_ErrorOnly(t *testing.T) {
	logger := newSlogLogger("error")
	buf := captureSlog(t)

	logger.Info("should not appear")
	logger.Error(errors.New("test error"), "error happened")

	assert.Contains(t, buf.String(), "error happened")
	assert.NotContains(t, buf.String(), "should not appear")
}

func TestSlogLogger_InfoOutputsBoth(t *testing.T) {
	logger := newSlogLogger("info")
	buf := captureSlog(t)

	logger.Info("info message", "key", "value")
	logger.Error(errors.New("test error"), "error message")

	assert.Contains(t, buf.String(), "info message")
	assert.Contains(t, buf.String(), "error message")
}

func TestSlogLogger_DefaultsToInfo(t *testing.T) {
	logger := newSlogLogger("unknown-level")
	buf := captureSlog(t)

	logger.Info("should appear")
	assert.Contains(t, buf.String(), "should appear")
}
