package redisx

import (
	"fmt"
	"log/slog"

	"github.com/redis/go-redis/v9"

	"github.com/servekit/go-common/lifecycle"
)

// Connect resolves the Redis client with the platform's inject-or-build
// contract: an injected client is returned as-is — the caller owns its
// lifecycle and nothing is registered with mgr; otherwise a client is built
// from cfg (connectivity verified by Ping, see New) and a Stopper is
// registered so mgr.Stop closes it.
func Connect(cfg *Config, injected *redis.Client, mgr *lifecycle.Manager) (*redis.Client, error) {
	if injected != nil {
		return injected, nil
	}
	if cfg == nil {
		return nil, fmt.Errorf("redisx: config required when no client injected")
	}
	rdb, err := New(cfg)
	if err != nil {
		return nil, err
	}
	mgr.AddStopper("redis", lifecycle.StopFunc(func() {
		if err := rdb.Close(); err != nil {
			slog.Warn("redisx: close redis", "error", err)
		}
	}))
	return rdb, nil
}
