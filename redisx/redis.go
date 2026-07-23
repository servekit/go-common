// Package redisx provides Redis client initialization with standardized configuration.
package redisx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// Config holds Redis connection parameters. New() connects to either a
// standalone Redis instance (Addr) or a sentinel-managed master (MasterName
// + SentinelAddrs); sentinel takes precedence when both are set. Redis
// Cluster is intentionally not supported — it solves sharding (data volume
// or write throughput past one instance), not HA. Use sentinel for HA.
type Config struct {
	// Standalone mode — used when MasterName and SentinelAddrs are empty.
	Addr string

	// Sentinel mode — takes precedence over Addr when set.
	MasterName    string
	SentinelAddrs []string

	Username string // Redis 6.0+ ACL username
	Password string
	DB       int

	// ClientName is set via CLIENT SETNAME, visible in CLIENT LIST — useful
	// for tracing which app holds a connection when debugging pool issues.
	ClientName string

	// PoolSize caps open sockets to Redis. 0 = go-redis default
	// (10*runtime.GOMAXPROCS, ~80 on an 8-core host). Typical 50–200. Raise
	// when upstream reports redis.ErrPoolExhausted or "connection pool exhausted".
	PoolSize int

	// MinIdleConns keeps this many idle conns warm to avoid dial latency on
	// bursts. 0 = disabled (default). Typical 10–20% of PoolSize.
	MinIdleConns int

	// ConnMaxIdleTime closes a conn after it has been idle this long.
	// 0 = go-redis default (30m). Lower if a NAT/LB silently drops idle conns
	// (surfaces as EOF on next use).
	ConnMaxIdle time.Duration `default:"30m"`

	// ConnMaxLife closes a conn after this total age regardless of activity.
	// 0 = unlimited. Set when middleware enforces a max connection age
	// (some cloud Redis offerings cap at 1h).
	ConnMaxLife time.Duration

	// DialTimeout for TCP connect. 0 = go-redis default (5s). Typical 1–5s.
	// Validate() rejects values <100ms — normal jitter would cause flapping.
	DialTimeout time.Duration `default:"5s"`

	// ReadTimeout per Redis command. 0 = go-redis default (3s). Typical 1–3s
	// for fast commands. Long ops (BLPOP, big SCAN, Lua over large data) must
	// pass their own context deadline — don't raise this globally for them.
	ReadTimeout time.Duration `default:"3s"`

	// WriteTimeout per Redis command. 0 = go-redis default (3s). Rarely needs
	// tuning separately from ReadTimeout.
	WriteTimeout time.Duration `default:"3s"`

	// MaxRetries on transient failures (network resets, MOVED during failover).
	// 0 = go-redis default (3). Set to -1 when the caller handles retry
	// itself (e.g. non-idempotent writes).
	MaxRetries int `default:"3"`

	// MinRetryBackoff / MaxRetryBackoff bound exponential backoff between
	// retries. 0 = go-redis default (8ms–512ms). Almost never needs tuning.
	MinRetryBackoff time.Duration `default:"8ms"`
	MaxRetryBackoff time.Duration `default:"512ms"`
}

// Validate checks for missing required fields and obvious misconfigurations.
// Called by New(); callers may also call it directly at config-load time to
// fail fast.
func (c *Config) Validate() error {
	sentinelMode := c.MasterName != "" || len(c.SentinelAddrs) > 0
	switch {
	case sentinelMode && c.MasterName == "":
		return errors.New("redisx: MasterName is required when SentinelAddrs is set")
	case sentinelMode && len(c.SentinelAddrs) == 0:
		return errors.New("redisx: SentinelAddrs is required when MasterName is set")
	case !sentinelMode && c.Addr == "":
		return errors.New("redisx: Addr is required (or set MasterName + SentinelAddrs for sentinel mode)")
	}

	if c.PoolSize < 0 {
		return errors.New("redisx: PoolSize must be >= 0")
	}
	if c.MinIdleConns < 0 {
		return errors.New("redisx: MinIdleConns must be >= 0")
	}
	if c.PoolSize > 0 && c.MinIdleConns > c.PoolSize {
		return errors.New("redisx: MinIdleConns cannot exceed PoolSize")
	}

	// Thresholds below guard against foot-guns. 0 means "use go-redis default"
	// and is always allowed.
	const minDial = 100 * time.Millisecond
	const minRW = time.Second
	if c.DialTimeout > 0 && c.DialTimeout < minDial {
		return fmt.Errorf("redisx: DialTimeout must be 0 or >= %s (got %s)", minDial, c.DialTimeout)
	}
	if c.ReadTimeout > 0 && c.ReadTimeout < minRW {
		return fmt.Errorf("redisx: ReadTimeout must be 0 or >= %s (got %s)", minRW, c.ReadTimeout)
	}
	if c.WriteTimeout > 0 && c.WriteTimeout < minRW {
		return fmt.Errorf("redisx: WriteTimeout must be 0 or >= %s (got %s)", minRW, c.WriteTimeout)
	}

	if c.MaxRetries < -1 {
		return errors.New("redisx: MaxRetries must be >= -1 (-1 disables retry)")
	}
	return nil
}

// New creates a Redis client and verifies connectivity with a Ping. When
// MasterName and SentinelAddrs are both set, connects via sentinel; otherwise
// treats Addr as a standalone instance. Either path returns *redis.Client, so
// downstream callers don't care which mode is in use.
//
// Validate() runs first; see it for which configurations are rejected.
func New(cfg *Config) (*redis.Client, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	var (
		client     *redis.Client
		pingTarget string
	)
	if cfg.MasterName != "" && len(cfg.SentinelAddrs) > 0 {
		pingTarget = cfg.MasterName
		client = redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:      cfg.MasterName,
			SentinelAddrs:   cfg.SentinelAddrs,
			Username:        cfg.Username,
			Password:        cfg.Password,
			DB:              cfg.DB,
			ClientName:      cfg.ClientName,
			PoolSize:        cfg.PoolSize,
			MinIdleConns:    cfg.MinIdleConns,
			ConnMaxIdleTime: cfg.ConnMaxIdle,
			ConnMaxLifetime: cfg.ConnMaxLife,
			DialTimeout:     cfg.DialTimeout,
			ReadTimeout:     cfg.ReadTimeout,
			WriteTimeout:    cfg.WriteTimeout,
			MaxRetries:      cfg.MaxRetries,
			MinRetryBackoff: cfg.MinRetryBackoff,
			MaxRetryBackoff: cfg.MaxRetryBackoff,
		})
	} else {
		pingTarget = cfg.Addr
		client = redis.NewClient(&redis.Options{
			Addr:            cfg.Addr,
			Username:        cfg.Username,
			Password:        cfg.Password,
			DB:              cfg.DB,
			ClientName:      cfg.ClientName,
			PoolSize:        cfg.PoolSize,
			MinIdleConns:    cfg.MinIdleConns,
			ConnMaxIdleTime: cfg.ConnMaxIdle,
			ConnMaxLifetime: cfg.ConnMaxLife,
			DialTimeout:     cfg.DialTimeout,
			ReadTimeout:     cfg.ReadTimeout,
			WriteTimeout:    cfg.WriteTimeout,
			MaxRetries:      cfg.MaxRetries,
			MinRetryBackoff: cfg.MinRetryBackoff,
			MaxRetryBackoff: cfg.MaxRetryBackoff,
		})
	}
	if err := client.Ping(context.Background()).Err(); err != nil {
		return nil, fmt.Errorf("redis ping %s: %w", pingTarget, err)
	}
	return client, nil
}
