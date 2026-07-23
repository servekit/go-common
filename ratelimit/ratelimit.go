// Package ratelimit provides a Redis-backed fixed-window rate limiter.
package ratelimit

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Rule defines a single rate-limit window.
type Rule struct {
	Window time.Duration
	Max    int64
}

// Stat describes the current state of one rate-limit window for a target.
type Stat struct {
	Scope     string
	Window    time.Duration
	Count     int64
	Max       int64
	Remaining int64
	ResetsAt  time.Time
}

// Config holds rate-limit configuration.
type Config struct {
	// Prefix is the Redis key prefix, e.g., "captcha:rate", "login:rate".
	Prefix string
	// Global limits per target (applied across all purposes).
	Global []*Rule
	// Rules maps purpose to per-purpose rules.
	Rules map[string][]*Rule
}

// Limiter checks whether a request is allowed.
type Limiter interface {
	// Allow returns true if the request is within rate limits.
	Allow(ctx context.Context, purpose, target string) (bool, error)
}

// RedisLimiter is a Redis-backed rate limiter using fixed windows.
type RedisLimiter struct {
	client *redis.Client
	config Config
	script *redis.Script
}

// scopedRule pairs a scope name with a single Rule. Used so Stats/Reset can
// iterate global + per-purpose rules uniformly and know which scope each key
// belongs to.
type scopedRule struct {
	scope string
	rule  *Rule
}

// NewRedisLimiter creates a Redis rate limiter. Keys are hashed by target
// (see key()), so the limiter is safe to use against a Redis Cluster.
func NewRedisLimiter(client *redis.Client, config *Config) *RedisLimiter {
	return &RedisLimiter{
		client: client,
		config: *config,
		script: redis.NewScript(`
-- KEYS: one key per rule (global first, then purpose rules)
-- ARGV: one TTL per rule (same order as KEYS), followed by max count per rule

local n = #KEYS
local ttls = {}
local maxes = {}
for i = 1, n do
    ttls[i] = tonumber(ARGV[i])
    maxes[i] = tonumber(ARGV[n + i])
end

-- Phase 1: Check all rules without incrementing.
for i = 1, n do
    local count = tonumber(redis.call('GET', KEYS[i]) or '0')
    if count >= maxes[i] then
        return 0
    end
end

-- Phase 2: All checks passed, increment all counters.
for i = 1, n do
    local count = redis.call('INCR', KEYS[i])
    if count == 1 then
        redis.call('EXPIRE', KEYS[i], ttls[i])
    end
end

return 1
`),
	}
}

// Allow checks whether the target is within rate limits for the given purpose.
func (l *RedisLimiter) Allow(ctx context.Context, purpose, target string) (bool, error) {
	n := len(l.config.Global) + len(l.config.Rules[purpose])
	keys := make([]string, 0, n)
	argv := make([]any, 0, n*2)

	// Collect global rules.
	for _, rule := range l.config.Global {
		keys = append(keys, l.key("global", target, rule.Window))
		argv = append(argv, int(rule.Window.Seconds()))
	}

	// Collect purpose rules.
	rules, ok := l.config.Rules[purpose]
	if !ok && len(keys) == 0 {
		return true, nil
	}
	for _, rule := range rules {
		keys = append(keys, l.key(purpose, target, rule.Window))
		argv = append(argv, int(rule.Window.Seconds()))
	}

	// Append max counts after TTLs.
	for _, rule := range l.config.Global {
		argv = append(argv, rule.Max)
	}
	for _, rule := range rules {
		argv = append(argv, rule.Max)
	}

	if len(keys) == 0 {
		return true, nil
	}

	result, err := l.script.Run(ctx, l.client, keys, argv...).Int64()
	if err != nil {
		return false, fmt.Errorf("check rate limit: %w", err)
	}
	return result == 1, nil
}

// Stats returns the current counters for target across all configured rules
// (global + every purpose in Rules). Windows without a Redis key return
// Count = 0 and a zero ResetsAt.
func (l *RedisLimiter) Stats(ctx context.Context, target string) ([]Stat, error) {
	rules := l.allScopedRules()
	if len(rules) == 0 {
		return nil, nil
	}

	keys := make([]string, len(rules))
	for i, sr := range rules {
		keys[i] = l.key(sr.scope, target, sr.rule.Window)
	}

	// Round 1: MGET all counts in one shot.
	counts, err := l.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("ratelimit: stats: %w", err)
	}

	// Round 2: pipelined TTL for positions where count > 0.
	stats := make([]Stat, len(rules))
	ttlCmds := make([]*redis.DurationCmd, len(rules))
	now := time.Now()
	pipe := l.client.Pipeline()
	for i, c := range counts {
		var n int64
		if s, ok := c.(string); ok {
			if parsed, err := strconv.ParseInt(s, 10, 64); err == nil {
				n = parsed
			}
		}
		stats[i].Count = n
		if n > 0 {
			ttlCmds[i] = pipe.TTL(ctx, keys[i])
		}
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return nil, fmt.Errorf("ratelimit: stats ttl: %w", err)
	}

	for i, sr := range rules {
		ruleMax := sr.rule.Max
		remaining := ruleMax - stats[i].Count
		if remaining < 0 {
			remaining = 0
		}
		var resetsAt time.Time
		if stats[i].Count > 0 && ttlCmds[i] != nil {
			if ttl := ttlCmds[i].Val(); ttl > 0 {
				resetsAt = now.Add(ttl)
			}
		}
		stats[i] = Stat{
			Scope:     sr.scope,
			Window:    sr.rule.Window,
			Count:     stats[i].Count,
			Max:       ruleMax,
			Remaining: remaining,
			ResetsAt:  resetsAt,
		}
	}
	return stats, nil
}

// Reset clears all rate-limit counters for target — both global rules and
// every purpose configured in Rules. Returns the number of Redis keys
// deleted.
func (l *RedisLimiter) Reset(ctx context.Context, target string) (int64, error) {
	rules := l.allScopedRules()
	if len(rules) == 0 {
		return 0, nil
	}
	keys := make([]string, len(rules))
	for i, sr := range rules {
		keys[i] = l.key(sr.scope, target, sr.rule.Window)
	}
	deleted, err := l.client.Del(ctx, keys...).Result()
	if err != nil {
		return 0, fmt.Errorf("ratelimit: reset: %w", err)
	}
	return deleted, nil
}

// ResetPurpose clears counters for one scope: "global" or a purpose that
// exists in Rules. Returns an error if purpose is neither "global" nor a
// configured purpose, so caller typos surface immediately rather than
// silently no-op-ing.
func (l *RedisLimiter) ResetPurpose(ctx context.Context, purpose, target string) (int64, error) {
	rules, ok := l.scopedRulesFor(purpose)
	if !ok {
		return 0, fmt.Errorf("ratelimit: unknown purpose %q", purpose)
	}
	if len(rules) == 0 {
		return 0, nil
	}
	keys := make([]string, len(rules))
	for i, sr := range rules {
		keys[i] = l.key(sr.scope, target, sr.rule.Window)
	}
	deleted, err := l.client.Del(ctx, keys...).Result()
	if err != nil {
		return 0, fmt.Errorf("ratelimit: reset purpose: %w", err)
	}
	return deleted, nil
}

func (l *RedisLimiter) key(purpose, target string, window time.Duration) string {
	// {target} is a Redis Cluster hash tag: all rules for one target land in
	// the same slot, so multi-key ops below (Lua, MGet, Del) stay cluster-safe.
	suffix := strconv.FormatInt(int64(window.Seconds()), 10)
	return fmt.Sprintf("%s:%s:{%s}:%s", l.config.Prefix, purpose, target, suffix)
}

// allScopedRules returns every configured rule with its scope, in
// (scope-asc, window-asc) order. Global rules carry scope "global".
func (l *RedisLimiter) allScopedRules() []scopedRule {
	out := make([]scopedRule, 0, len(l.config.Global)+len(l.config.Rules))
	for _, r := range l.config.Global {
		out = append(out, scopedRule{scope: "global", rule: r})
	}
	for purpose, rules := range l.config.Rules {
		for _, r := range rules {
			out = append(out, scopedRule{scope: purpose, rule: r})
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].scope != out[j].scope {
			return out[i].scope < out[j].scope
		}
		return out[i].rule.Window < out[j].rule.Window
	})
	return out
}

// scopedRulesFor returns the scoped rules for one scope. Returns ok=false if
// purpose is neither "global" nor present in Rules.
func (l *RedisLimiter) scopedRulesFor(purpose string) ([]scopedRule, bool) {
	if purpose == "global" {
		out := make([]scopedRule, 0, len(l.config.Global))
		for _, r := range l.config.Global {
			out = append(out, scopedRule{scope: "global", rule: r})
		}
		sort.Slice(out, func(i, j int) bool {
			return out[i].rule.Window < out[j].rule.Window
		})
		return out, true
	}
	rs, ok := l.config.Rules[purpose]
	if !ok {
		return nil, false
	}
	out := make([]scopedRule, 0, len(rs))
	for _, r := range rs {
		out = append(out, scopedRule{scope: purpose, rule: r})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].rule.Window < out[j].rule.Window
	})
	return out, true
}
