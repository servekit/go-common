# ratelimit: Stats and Reset

Date: 2026-06-12

## Background

`ratelimit.RedisLimiter` is a fixed-window rate limiter backed by Redis. A Lua
script atomically checks and increments counters across all configured rules
(global + per-purpose). Once a target hits a limit, the only recovery path
today is to wait for the window to expire.

Operations need two things the current API does not provide:

1. **Observability** — "how many times has this IP hit in the last 24h, and
   when does the window reset?" Support tickets and on-call investigations
   need this without dropping into `redis-cli`.
2. **Manual reset** — when a false positive or a customer-support case requires
   unblocking a target ahead of the natural window expiry.

The current `Allow(purpose, target)` signature also lacks a `context.Context`,
which blocks callers from enforcing timeouts or propagating trace spans.

## Goal

Add admin-facing methods to `*RedisLimiter` for inspecting and clearing
counters, and bring `Allow` in line with Go conventions by accepting a
context.

## Non-Goals

- No new abstract interface (`Manager`, `Admin`, etc.). Methods live on
  `*RedisLimiter` directly. Callers that want to mock these in their own tests
  can define their own interface at the consumer side.
- No persistence/audit log of resets. If a caller needs that, they wrap the
  call.
- No partial resets (e.g. "reset only the 1-minute window, keep the 24h one").
  Reset clears every window for the chosen scope.
- No subscription/streaming of counter changes. Stats is a point-in-time read.
- No key enumeration for purposes not present in the current `Config`. Reset
  only deletes keys derivable from `Config.Global` and `Config.Rules`. If a
  purpose was removed from config after a target was limited under it, that
  stale counter is not cleaned up by Reset.

## Design

### New type: `Stat`

```go
// Stat describes the current state of one rate-limit window for a target.
type Stat struct {
    Scope     string        // "global" or the purpose name
    Window    time.Duration // the rule's window length
    Count     int64         // current counter value (0 if no key in Redis)
    Max       int64         // configured max for this window
    Remaining int64         // Max - Count, clamped to >= 0
    ResetsAt  time.Time     // approximate expiry; zero value when Count == 0 or TTL missing
}
```

The slice returned by `Stats` is sorted for stable, predictable output:

1. By `Scope` ascending lexicographically (`"global"` always sorts first
   because lowercase `g` < any other lowercase ASCII letter — but the sort is
   purely alphabetic, not special-cased).
2. By `Window` ascending within the same `Scope`.

This makes test assertions and admin-UI rendering deterministic.

### New methods on `*RedisLimiter`

```go
// Stats returns the current counters for target across all configured rules
// (global + every purpose in Rules). Windows without a Redis key return
// Count = 0 and a zero ResetsAt.
func (l *RedisLimiter) Stats(ctx context.Context, target string) ([]Stat, error)

// Reset clears all rate-limit counters for target — both global rules and
// every purpose configured in Rules. Returns the number of Redis keys
// deleted.
func (l *RedisLimiter) Reset(ctx context.Context, target string) (int64, error)

// ResetPurpose clears counters for one scope: "global" or a purpose that
// exists in Rules. Returns an error if purpose is neither "global" nor a
// configured purpose, so caller typos surface immediately rather than
// silently no-op-ing.
func (l *RedisLimiter) ResetPurpose(ctx context.Context, purpose, target string) (int64, error)
```

The `Limiter` interface is unchanged beyond the `Allow` signature update
below; these methods are only on the concrete `*RedisLimiter`.

### Breaking change: `Allow` accepts `context.Context`

```go
// Limiter is unchanged in shape; only Allow's signature moves.
type Limiter interface {
    Allow(ctx context.Context, purpose, target string) (bool, error)
}
```

Internal caller impact: `captcha/captcha.go:182` is the only call site.
`Captcha.Generate` already receives a `ctx`; it will be forwarded directly.

### Redis operations

- **Stats**:
  - Build the full key list from `Config.Global` and `Config.Rules` (same
    order `Allow` uses).
  - Issue one `MGET` to fetch all counts.
  - For positions where `Count > 0`, issue `TTL` (pipelined) to compute
    `ResetsAt = time.Now().Add(ttl)`. Keys with `Count == 0` get a zero
    `ResetsAt`. If `TTL` returns `-1` (no expiry, e.g. an operator-set key
    without EXPIRE) or any non-positive value, `ResetsAt` is left zero —
    callers cannot reliably predict a reset time the limiter itself did not
    set.
  - Two Redis round trips in the common case (MGET + pipelined TTLs). No Lua
    script — the read paths are simple enough that Lua adds complexity
    without benefit.
- **Reset**: build the full key list (same as Stats), issue one `DEL` with
  all keys. Returns the count of deleted keys. A target that never hit any
  rule legitimately returns `(0, nil)` — this is not an error.
- **ResetPurpose**: build the key list for one scope only (`"global"` →
  `Config.Global`, otherwise `Config.Rules[purpose]`). Validate purpose
  before touching Redis.

### Error handling

- `ResetPurpose` on an unknown purpose returns
  `fmt.Errorf("ratelimit: unknown purpose %q", purpose)` without any Redis
  call.
- Redis errors propagate as `fmt.Errorf("ratelimit: <op>: %w", err)`, e.g.
  `ratelimit: stats: <redis error>`.
- A target with no configured rules (empty `Config`) yields
  `Stats → ([], nil)`, `Reset → (0, nil)`. No error.

## Testing

All tests use `redisx.NewTestClient` (miniredis). New cases:

- `TestRedisLimiter_Stats_empty` — fresh target, all counts 0, ResetsAt zero.
- `TestRedisLimiter_Stats_returnsCountsAndTTL` — Allow several times under
  multiple purposes/windows, assert Count, Remaining, Max, and that
  ResetsAt is within an acceptable window of `now + ttl`.
- `TestRedisLimiter_Stats_sorted` — Stats slice is sorted by (Scope, Window).
- `TestRedisLimiter_Reset_allPurposes` — exhaust a limit, call Reset, the
  next Allow succeeds for every previously-blocked purpose.
- `TestRedisLimiter_ResetPurpose_isolated` — exhaust purposes A and B,
  ResetPurpose("A", target), assert A's counter gone and B's intact.
- `TestRedisLimiter_ResetPurpose_global` — `ResetPurpose("global", target)`
  clears only global counters, leaving per-purpose counters intact.
- `TestRedisLimiter_ResetPurpose_unknownPurpose` — unknown purpose returns
  an error, no Redis mutation.
- `TestRedisLimiter_Stats_noConfig` — limiter with no Global and no Rules
  returns `([], nil)`.

Existing `Allow` tests are updated to pass `context.Background()`.

## Migration

1. Update `Allow` signature in `ratelimit.go` and the `Limiter` interface.
2. Update `captcha/captcha.go:182` to forward `ctx`.
3. Run `go test ./...` to confirm everything still passes.

No deprecation shim — `Allow` is a small internal API and a clean break is
cheaper than carrying two methods.

## 关联

**实现计划：** `docs/superpowers/plans/2026-06-12-ratelimit-stats-reset.md`
