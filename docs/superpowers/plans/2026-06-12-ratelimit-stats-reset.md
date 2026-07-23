# ratelimit: Stats and Reset Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add admin-facing methods to `*RedisLimiter` for inspecting and clearing counters (`Stats`, `Reset`, `ResetPurpose`), and migrate `Allow` to accept `context.Context`.

**Architecture:** `Stat` is a plain struct snapshot of one window's state. `Stats` builds the full key list from `Config.Global` + `Config.Rules`, issues one `MGET`, then a pipelined `TTL` for non-zero counts. `Reset` and `ResetPurpose` reuse the same key-building logic and issue one `DEL`. The existing Lua script for `Allow` is untouched. `Allow` gains a leading `context.Context` parameter; the single internal caller (`captcha.Generate`) forwards its existing `ctx`.

**Tech Stack:** Go stdlib (`context`, `sort`, `time`, `fmt`), `github.com/redis/go-redis/v9` (with `MGET`, pipelined `TTL`, `DEL`), `testify/require`, `redisx.NewTestClient` (miniredis). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-06-12-ratelimit-stats-reset-design.md`

---

## File Structure

**Modified files:**

| File | Responsibility |
|---|---|
| `ratelimit/ratelimit.go` | Add `Stat` type; add `Stats`/`Reset`/`ResetPurpose` methods; refactor key-building into a shared helper; change `Allow` signature to accept `ctx` |
| `ratelimit/ratelimit_test.go` | Update existing `Allow` calls to pass `context.Background()`; add 8 new tests |
| `captcha/captcha.go` | Update line 182 to forward `ctx` into `limiter.Allow` |

**Not modified:**
- `Limiter` interface shape — only `Allow`'s signature moves.
- The existing Lua script in `NewRedisLimiter` — untouched.
- No new files.

---

## Task 1: Migrate `Allow` to accept `context.Context`

This is the breaking change. Do it first so the new methods can be added cleanly on top.

**Files:**
- Modify: `ratelimit/ratelimit.go` (`Limiter` interface + `RedisLimiter.Allow`)
- Modify: `ratelimit/ratelimit_test.go` (12 call sites)
- Modify: `captcha/captcha.go:182` (1 call site)

- [ ] **Step 1: Update the `Limiter` interface and `Allow` signature in `ratelimit/ratelimit.go`**

Replace the interface declaration:

```go
// Limiter checks whether a request is allowed.
type Limiter interface {
	// Allow returns true if the request is within rate limits.
	Allow(ctx context.Context, purpose, target string) (bool, error)
}
```

Replace the `Allow` method signature and its internal `context.Background()` call:

```go
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
```

The only changes are the new `ctx context.Context` parameter on both the interface and the struct method, replacing the old `ctx := context.Background()` line with the parameter, and removing the now-unused `"context"` import only if nothing else in the file uses it (the new `Stats`/`Reset` methods added in later tasks will use `context`, so keep the import).

- [ ] **Step 2: Update test call sites in `ratelimit/ratelimit_test.go`**

Add `"context"` to the import block, then replace each `limiter.Allow(p, t)` / `captchaLimiter.Allow(p, t)` / `loginLimiter.Allow(p, t)` call with `limiter.Allow(context.Background(), p, t)` (and the same for the other two receiver names).

Concretely, every line of the form:

```go
ok, err := limiter.Allow("register", "test@example.com")
```

becomes:

```go
ok, err := limiter.Allow(context.Background(), "register", "test@example.com")
```

Apply the same transformation to all 12 call sites (lines around 27, 41, 45, 57, 59, 61, 76, 79, 88, 110, 112, 116).

- [ ] **Step 3: Update the captcha call site in `captcha/captcha.go:182`**

Change:

```go
ok, err := c.limiter.Allow(purpose, target)
```

to:

```go
ok, err := c.limiter.Allow(ctx, purpose, target)
```

`ctx` is already in scope from `Captcha.Generate`'s signature.

- [ ] **Step 4: Run the full test suite to verify nothing broke**

Run: `go test ./...`

Expected: all tests PASS, including `captcha` and `ratelimit`.

- [ ] **Step 5: Commit**

```bash
git add ratelimit/ratelimit.go ratelimit/ratelimit_test.go captcha/captcha.go
git commit -m "$(cat <<'EOF'
refactor(ratelimit)!: accept context.Context in Allow

BREAKING CHANGE: Limiter.Allow now takes context.Context as its first
argument so callers can enforce timeouts and propagate trace spans.
The single internal caller (captcha.Generate) forwards its existing
ctx.
EOF
)"
```

---

## Task 2: Add `Stat` type and `Stats` method

Introduces the read path. The shared key-building helper is extracted here so `Reset` and `ResetPurpose` can reuse it.

**Files:**
- Modify: `ratelimit/ratelimit.go` (new `Stat` type, new `Stats` method, new `allKeys` helper)
- Modify: `ratelimit/ratelimit_test.go` (4 new tests)

- [ ] **Step 1: Write the failing tests**

Append to `ratelimit/ratelimit_test.go`:

```go
func TestRedisLimiter_Stats_empty(t *testing.T) {
	limiter := NewRedisLimiter(testRedisClient(t), Config{
		Prefix: "test",
		Global: []Rule{{Window: 24 * time.Hour, Max: 100}},
		Rules: map[string][]Rule{
			"register": {{Window: time.Minute, Max: 1}},
		},
	})

	stats, err := limiter.Stats(context.Background(), "fresh@example.com")
	require.NoError(t, err)
	require.Len(t, stats, 2)

	// Sorted by Scope asc, then Window asc. "global" < "register".
	require.Equal(t, "global", stats[0].Scope)
	require.Equal(t, 24*time.Hour, stats[0].Window)
	require.Equal(t, int64(0), stats[0].Count)
	require.Equal(t, int64(100), stats[0].Max)
	require.Equal(t, int64(100), stats[0].Remaining)
	require.True(t, stats[0].ResetsAt.IsZero())

	require.Equal(t, "register", stats[1].Scope)
	require.Equal(t, time.Minute, stats[1].Window)
	require.Equal(t, int64(0), stats[1].Count)
	require.Equal(t, int64(1), stats[1].Max)
	require.Equal(t, int64(1), stats[1].Remaining)
	require.True(t, stats[1].ResetsAt.IsZero())
}

func TestRedisLimiter_Stats_returnsCountsAndTTL(t *testing.T) {
	limiter := NewRedisLimiter(testRedisClient(t), Config{
		Prefix: "test",
		Global: []Rule{{Window: time.Hour, Max: 10}},
		Rules: map[string][]Rule{
			"register": {{Window: time.Minute, Max: 5}},
		},
	})

	// Hit twice under register, once globally via another purpose.
	for i := 0; i < 2; i++ {
		ok, err := limiter.Allow(context.Background(), "register", "hit@example.com")
		require.NoError(t, err)
		require.True(t, ok)
	}
	ok, err := limiter.Allow(context.Background(), "login", "hit@example.com")
	require.NoError(t, err)
	require.True(t, ok)

	stats, err := limiter.Stats(context.Background(), "hit@example.com")
	require.NoError(t, err)
	require.Len(t, stats, 2)

	// global: 3 hits (2 register + 1 login) / 10
	require.Equal(t, "global", stats[0].Scope)
	require.Equal(t, int64(3), stats[0].Count)
	require.Equal(t, int64(10), stats[0].Max)
	require.Equal(t, int64(7), stats[0].Remaining)
	// TTL was set; ResetsAt should be roughly now + 1h.
	require.WithinDuration(t, time.Now().Add(time.Hour), stats[0].ResetsAt, 5*time.Second)

	// register: 2 hits / 5
	require.Equal(t, "register", stats[1].Scope)
	require.Equal(t, int64(2), stats[1].Count)
	require.Equal(t, int64(5), stats[1].Max)
	require.Equal(t, int64(3), stats[1].Remaining)
	require.WithinDuration(t, time.Now().Add(time.Minute), stats[1].ResetsAt, 5*time.Second)
}

func TestRedisLimiter_Stats_sorted(t *testing.T) {
	limiter := NewRedisLimiter(testRedisClient(t), Config{
		Prefix: "test",
		Global: []Rule{
			{Window: 24 * time.Hour, Max: 100},
			{Window: time.Hour, Max: 20},
		},
		Rules: map[string][]Rule{
			"register": {
				{Window: 5 * time.Minute, Max: 5},
				{Window: time.Minute, Max: 1},
			},
			"login": {{Window: time.Minute, Max: 3}},
		},
	})

	stats, err := limiter.Stats(context.Background(), "sort@example.com")
	require.NoError(t, err)
	require.Len(t, stats, 5)

	// Expected sort: (global,1h),(global,24h),(login,1m),(register,1m),(register,5m)
	require.Equal(t, "global", stats[0].Scope)
	require.Equal(t, time.Hour, stats[0].Window)
	require.Equal(t, "global", stats[1].Scope)
	require.Equal(t, 24*time.Hour, stats[1].Window)
	require.Equal(t, "login", stats[2].Scope)
	require.Equal(t, time.Minute, stats[2].Window)
	require.Equal(t, "register", stats[3].Scope)
	require.Equal(t, time.Minute, stats[3].Window)
	require.Equal(t, "register", stats[4].Scope)
	require.Equal(t, 5*time.Minute, stats[4].Window)
}

func TestRedisLimiter_Stats_noConfig(t *testing.T) {
	limiter := NewRedisLimiter(testRedisClient(t), Config{
		Prefix: "test",
	})

	stats, err := limiter.Stats(context.Background(), "anyone@example.com")
	require.NoError(t, err)
	require.Empty(t, stats)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./ratelimit/ -run 'TestRedisLimiter_Stats' -v`

Expected: FAIL with `undefined: Stats` / `undefined: Stat`.

- [ ] **Step 3: Add `Stat` type, `allKeys` helper, and `Stats` method to `ratelimit/ratelimit.go`**

Add `"sort"` and `"time"` (already imported) to the import block:

```go
import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)
```

Add the `Stat` type after the `Rule` type declaration:

```go
// Stat describes the current state of one rate-limit window for a target.
type Stat struct {
	Scope     string
	Window    time.Duration
	Count     int64
	Max       int64
	Remaining int64
	ResetsAt  time.Time
}
```

Add the `allKeys` helper and the `Stats` method at the end of the file (after the existing `key` helper):

```go
// scopedRule pairs a scope name with a single Rule. Used so Stats/Reset can
// iterate global + per-purpose rules uniformly and know which scope each key
// belongs to.
type scopedRule struct {
	scope string
	rule  Rule
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

	pipe := l.client.Pipeline()
	ttlCmds := make([]*redis.DurationCmd, len(rules))

	// Round 1: MGET all counts in one shot.
	counts, err := l.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("ratelimit: stats: %w", err)
	}

	// Round 2: pipelined TTL for positions where count > 0.
	stats := make([]Stat, len(rules))
	now := time.Now()
	for i, c := range counts {
		var n int64
		if s, ok := c.(string); ok {
			n, _ = strconv.ParseInt(s, 10, 64)
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
		max := sr.rule.Max
		remaining := max - stats[i].Count
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
			Max:       max,
			Remaining: remaining,
			ResetsAt:  resetsAt,
		}
	}
	return stats, nil
}
```

Notes for the implementer:
- `MGet(...).Result()` returns `[]any` where each element is either a `string` (key exists) or `nil` (key missing). The type assertion + `strconv.ParseInt` handles both; missing keys yield count 0.
- TTL commands are only queued for positions where count > 0, so the second round trip's size scales with active windows, not configured rules.

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./ratelimit/ -run 'TestRedisLimiter_Stats' -v`

Expected: 4 new tests PASS.

- [ ] **Step 5: Commit**

```bash
git add ratelimit/ratelimit.go ratelimit/ratelimit_test.go
git commit -m "feat(ratelimit): add Stats method for inspecting counters"
```

---

## Task 3: Add `Reset` method

`Reset(target)` clears every counter for the target — global rules and every purpose in `Rules`.

**Files:**
- Modify: `ratelimit/ratelimit.go` (new `Reset` method)
- Modify: `ratelimit/ratelimit_test.go` (1 new test)

- [ ] **Step 1: Write the failing test**

Append to `ratelimit/ratelimit_test.go`:

```go
func TestRedisLimiter_Reset_allPurposes(t *testing.T) {
	limiter := NewRedisLimiter(testRedisClient(t), Config{
		Prefix: "test",
		Global: []Rule{{Window: time.Hour, Max: 2}},
		Rules: map[string][]Rule{
			"register": {{Window: time.Minute, Max: 1}},
			"login":    {{Window: time.Minute, Max: 1}},
		},
	})

	// Exhaust register + login + global.
	ok, err := limiter.Allow(context.Background(), "register", "reset@example.com")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = limiter.Allow(context.Background(), "login", "reset@example.com")
	require.NoError(t, err)
	require.True(t, ok)

	// All three are now blocked.
	ok, err = limiter.Allow(context.Background(), "register", "reset@example.com")
	require.NoError(t, err)
	require.False(t, ok)

	// Reset.
	deleted, err := limiter.Reset(context.Background(), "reset@example.com")
	require.NoError(t, err)
	require.Equal(t, int64(3), deleted) // global + register + login

	// All three windows are clear again.
	ok, err = limiter.Allow(context.Background(), "register", "reset@example.com")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = limiter.Allow(context.Background(), "login", "reset@example.com")
	require.NoError(t, err)
	require.True(t, ok)
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./ratelimit/ -run 'TestRedisLimiter_Reset_allPurposes' -v`

Expected: FAIL with `undefined: Reset`.

- [ ] **Step 3: Add `Reset` method to `ratelimit/ratelimit.go`**

Add after the `Stats` method:

```go
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
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./ratelimit/ -run 'TestRedisLimiter_Reset_allPurposes' -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add ratelimit/ratelimit.go ratelimit/ratelimit_test.go
git commit -m "feat(ratelimit): add Reset method to clear all counters for a target"
```

---

## Task 4: Add `ResetPurpose` method

`ResetPurpose(purpose, target)` clears one scope only. `purpose == "global"` clears global counters; any other value must exist in `Rules` or the call returns an error.

**Files:**
- Modify: `ratelimit/ratelimit.go` (new `ResetPurpose` method, new `scopedRulesFor` helper)
- Modify: `ratelimit/ratelimit_test.go` (3 new tests)

- [ ] **Step 1: Write the failing tests**

Append to `ratelimit/ratelimit_test.go`:

```go
func TestRedisLimiter_ResetPurpose_isolated(t *testing.T) {
	limiter := NewRedisLimiter(testRedisClient(t), Config{
		Prefix: "test",
		Rules: map[string][]Rule{
			"register": {{Window: time.Minute, Max: 1}},
			"login":    {{Window: time.Minute, Max: 1}},
		},
	})

	// Exhaust both purposes.
	ok, err := limiter.Allow(context.Background(), "register", "iso@example.com")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = limiter.Allow(context.Background(), "login", "iso@example.com")
	require.NoError(t, err)
	require.True(t, ok)

	// Reset only register.
	deleted, err := limiter.ResetPurpose(context.Background(), "register", "iso@example.com")
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted)

	// Register is clear, login is still blocked.
	ok, err = limiter.Allow(context.Background(), "register", "iso@example.com")
	require.NoError(t, err)
	require.True(t, ok)
	ok, err = limiter.Allow(context.Background(), "login", "iso@example.com")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRedisLimiter_ResetPurpose_global(t *testing.T) {
	limiter := NewRedisLimiter(testRedisClient(t), Config{
		Prefix: "test",
		Global: []Rule{{Window: time.Hour, Max: 1}},
		Rules: map[string][]Rule{
			"register": {{Window: time.Minute, Max: 5}},
		},
	})

	// Exhaust global.
	ok, err := limiter.Allow(context.Background(), "register", "g@example.com")
	require.NoError(t, err)
	require.True(t, ok)

	// register counter exists (1/5), global counter exists (1/1).
	stats, err := limiter.Stats(context.Background(), "g@example.com")
	require.NoError(t, err)
	require.Len(t, stats, 2)

	deleted, err := limiter.ResetPurpose(context.Background(), "global", "g@example.com")
	require.NoError(t, err)
	require.Equal(t, int64(1), deleted) // only global key

	// Verify via Stats: global count back to 0, register still 1.
	stats, err = limiter.Stats(context.Background(), "g@example.com")
	require.NoError(t, err)
	require.Len(t, stats, 2)
	for _, s := range stats {
		switch s.Scope {
		case "global":
			require.Equal(t, int64(0), s.Count)
		case "register":
			require.Equal(t, int64(1), s.Count)
		}
	}
}

func TestRedisLimiter_ResetPurpose_unknownPurpose(t *testing.T) {
	limiter := NewRedisLimiter(testRedisClient(t), Config{
		Prefix: "test",
		Rules: map[string][]Rule{
			"register": {{Window: time.Minute, Max: 1}},
		},
	})

	deleted, err := limiter.ResetPurpose(context.Background(), "nope", "x@example.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown purpose")
	require.Equal(t, int64(0), deleted)
}
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./ratelimit/ -run 'TestRedisLimiter_ResetPurpose' -v`

Expected: FAIL with `undefined: ResetPurpose`.

- [ ] **Step 3: Add `ResetPurpose` method to `ratelimit/ratelimit.go`**

Add after the `Reset` method:

```go
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
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `go test ./ratelimit/ -run 'TestRedisLimiter_ResetPurpose' -v`

Expected: 3 new tests PASS.

- [ ] **Step 5: Commit**

```bash
git add ratelimit/ratelimit.go ratelimit/ratelimit_test.go
git commit -m "feat(ratelimit): add ResetPurpose method for scope-scoped clears"
```

---

## Task 5: Coverage check, formatting, lint

Final verification gate per CLAUDE.md (85% coverage target, gofmt/goimports, golangci-lint).

**Files:** none modified unless a check fails.

- [ ] **Step 1: Run gofmt and goimports**

Run:

```bash
gofmt -w ratelimit/ratelimit.go ratelimit/ratelimit_test.go
goimports -w ratelimit/ratelimit.go ratelimit/ratelimit_test.go
```

Expected: no output (no formatting changes). If `goimports` is not installed, run `gofmt -w` only and note that goimports was skipped.

- [ ] **Step 2: Run golangci-lint**

Run: `golangci-lint run ./ratelimit/... ./captcha/...`

Expected: no issues. If any issues surface, fix them and re-run.

- [ ] **Step 3: Run full test suite with coverage**

Run: `go test ./... -cover`

Expected: all tests PASS, `ratelimit` coverage ≥ 85%. If coverage is below 85%, add tests targeting the uncovered lines before proceeding.

- [ ] **Step 4: Verify the whole tree still builds and passes**

Run: `go build ./... && go test ./...`

Expected: build succeeds, all tests pass.

- [ ] **Step 5: Commit (only if any formatting/lint fixes touched files)**

If Steps 1–2 modified any files:

```bash
git add -A
git commit -m "style(ratelimit): apply gofmt/goimports"
```

If nothing changed, skip the commit and note "no formatting changes needed" in the task tracker.

---

## Self-Review Notes

- **Spec coverage**: every method in the spec (`Stat`, `Stats`, `Reset`, `ResetPurpose`, `Allow` ctx migration) maps to a task. All 8 spec test cases are present.
- **Type consistency**: `Stat` field names (`Scope`, `Window`, `Count`, `Max`, `Remaining`, `ResetsAt`) match between the type declaration and every test assertion. `scopedRule` is used by all three new methods and the helper, with consistent `scope`/`rule` field names.
- **No placeholders**: every code step contains the full code an engineer needs; no "TBD" or "similar to above".
- **DRY**: `allScopedRules` (Stats, Reset) and `scopedRulesFor` (ResetPurpose) factor out key/scope construction. ResetPurpose does not reuse `allScopedRules` because it only wants one scope, and reusing the full list then filtering would obscure the unknown-purpose error path.
