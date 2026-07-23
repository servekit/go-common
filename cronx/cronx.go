// Package cronx provides cron scheduler initialization with standardized configuration.
//
// It wraps robfig/cron/v3 with a Config + New() pattern consistent with other
// go-common packages (redisx, dbx). Default behavior includes panic recovery
// and slog-based logging. Overlap policies (skip/delay) and second-level
// precision are configurable via Config.
package cronx

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

// Config holds cron scheduler parameters.
type Config struct {
	Timezone        string // IANA timezone, empty = Local
	WithSeconds     bool   // enable second-level precision
	LogLevel        string `default:"info"` // silent | error | info
	DisableRecovery bool   // disable panic recovery (default: false, recovery enabled)
	OverlapPolicy   string // skip | delay | empty
}

// Option configures additional cron behavior.
type Option func(*cronOptions)

// cronOptions collects Option-applied overrides.
type cronOptions struct {
	extraOpts []cron.Option
}

// New creates a *cron.Cron with the given config and options.
//
// Default behavior:
//   - Location: time.Local (override with Config.Timezone)
//   - Logger: slog adapter at "info" level (override with Config.LogLevel)
//   - Panic recovery: enabled by default (disable with Config.DisableRecovery = true)
//   - Overlap policy: none (configure with Config.OverlapPolicy)
//   - Seconds: disabled (enable with Config.WithSeconds)
func New(cfg *Config, opts ...Option) (*cron.Cron, error) {
	if cfg == nil {
		cfg = &Config{}
	}
	loc := time.Local
	if cfg.Timezone != "" {
		var err error
		loc, err = time.LoadLocation(cfg.Timezone)
		if err != nil {
			return nil, fmt.Errorf("cronx: invalid timezone %q: %w", cfg.Timezone, err)
		}
	}

	co := &cronOptions{}
	for _, opt := range opts {
		opt(co)
	}

	logLevel := cfg.LogLevel
	logger := newSlogLogger(logLevel)

	cronOpts := []cron.Option{
		cron.WithLocation(loc),
		cron.WithLogger(logger),
	}

	if cfg.WithSeconds {
		cronOpts = append(cronOpts, cron.WithSeconds())
	}

	wrappers := []cron.JobWrapper{}
	if !cfg.DisableRecovery {
		wrappers = append(wrappers, cron.Recover(logger))
	}

	switch cfg.OverlapPolicy {
	case "skip":
		wrappers = append(wrappers, cron.SkipIfStillRunning(logger))
	case "delay":
		wrappers = append(wrappers, cron.DelayIfStillRunning(logger))
	}

	cronOpts = append(cronOpts, cron.WithChain(wrappers...))
	cronOpts = append(cronOpts, co.extraOpts...)

	return cron.New(cronOpts...), nil
}

// WithCronOption passes through a raw cron.Option for advanced customization.
func WithCronOption(opt cron.Option) Option {
	return func(o *cronOptions) {
		o.extraOpts = append(o.extraOpts, opt)
	}
}
