package redisx

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// LockConfig configures the distributed lock behavior.
type LockConfig struct {
	Prefix string        // key prefix, e.g., "lock:order" (required)
	TTL    time.Duration // auto-release duration (required)
	Tries  int           // max acquisition attempts (default: 16)
	Wait   time.Duration // wait between attempts (default: 100ms)
}

// Lock provides a distributed lock via Redis.
// Safe for concurrent use by multiple goroutines.
type Lock struct {
	client *redis.Client
	cfg    LockConfig
}

var (
	// ErrLockFailed is returned when Acquire cannot obtain the lock within Tries.
	ErrLockFailed = errors.New("redisx: failed to acquire lock")
	// ErrUnlockFailed is returned when Release is called against a lock that is no longer held.
	ErrUnlockFailed = errors.New("redisx: failed to release lock")
	// ErrRenewFailed is returned when Renew is called against a lock that is no longer held.
	ErrRenewFailed = errors.New("redisx: failed to renew lock")
)

const (
	unlockScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("DEL", KEYS[1])
else
	return 0
end
`

	renewScript = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
	return redis.call("PEXPIRE", KEYS[1], ARGV[2])
else
	return 0
end
`
)

// NewLock creates a distributed lock with the given configuration.
func NewLock(client *redis.Client, cfg *LockConfig) (*Lock, error) {
	if cfg.Prefix == "" {
		return nil, errors.New("redisx: lock prefix is required")
	}
	if cfg.TTL <= 0 {
		return nil, errors.New("redisx: lock TTL must be positive")
	}
	c := *cfg
	if c.Tries <= 0 {
		c.Tries = 16
	}
	if c.Wait <= 0 {
		c.Wait = 100 * time.Millisecond
	}
	return &Lock{client: client, cfg: c}, nil
}

// Acquire attempts to acquire the lock for the given target.
// Returns a unique identifier that must be passed to Release.
func (l *Lock) Acquire(ctx context.Context, target string) (string, error) {
	id := uuid.NewString()
	key := l.key(target)

	for i := 0; i < l.cfg.Tries; i++ {
		ok, err := l.client.SetNX(ctx, key, id, l.cfg.TTL).Result()
		if err != nil {
			return "", fmt.Errorf("redisx: setnx %s: %w", key, err)
		}
		if ok {
			return id, nil
		}
		if i < l.cfg.Tries-1 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(l.cfg.Wait):
			}
		}
	}
	return "", ErrLockFailed
}

// Release releases the lock. Returns ErrUnlockFailed if the lock is no longer held.
func (l *Lock) Release(ctx context.Context, target, id string) error {
	key := l.key(target)
	res, err := l.client.Eval(ctx, unlockScript, []string{key}, id).Int64()
	if err != nil {
		return fmt.Errorf("redisx: unlock %s: %w", key, err)
	}
	if res == 0 {
		return ErrUnlockFailed
	}
	return nil
}

// Renew extends the lock TTL. Returns ErrRenewFailed if the lock is no longer held.
func (l *Lock) Renew(ctx context.Context, target, id string) error {
	key := l.key(target)
	res, err := l.client.Eval(ctx, renewScript, []string{key}, id, l.cfg.TTL.Milliseconds()).Int64()
	if err != nil {
		return fmt.Errorf("redisx: renew %s: %w", key, err)
	}
	if res == 0 {
		return ErrRenewFailed
	}
	return nil
}

// KeepAlive starts a background goroutine that periodically renews the lock TTL.
// The renewal interval is TTL/3. The goroutine stops when:
//   - The returned cancel function is called
//   - The parent context is cancelled
//   - Renew fails (lock lost or expired)
func (l *Lock) KeepAlive(ctx context.Context, target, id string) context.CancelFunc {
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		ticker := time.NewTicker(l.cfg.TTL / 3)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := l.Renew(ctx, target, id); err != nil {
					return
				}
			}
		}
	}()
	return cancel
}

func (l *Lock) key(target string) string {
	return fmt.Sprintf("%s:{%s}", l.cfg.Prefix, target)
}
