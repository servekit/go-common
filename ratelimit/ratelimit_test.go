package ratelimit

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/servekit/go-common/redisx"
)

func testRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	return redisx.NewTestClient(t)
}

func TestRedisLimiter_Allow_firstRequest(t *testing.T) {
	limiter := NewRedisLimiter(testRedisClient(t), &Config{
		Prefix: "test",
		Global: []*Rule{{Window: 24 * time.Hour, Max: 100}},
		Rules: map[string][]*Rule{
			"register": {{Window: time.Minute, Max: 1}},
		},
	})

	ok, err := limiter.Allow(context.Background(), "register", "test@example.com")
	require.NoError(t, err)
	require.True(t, ok)
}

func TestRedisLimiter_Allow_rateExceeded(t *testing.T) {
	limiter := NewRedisLimiter(testRedisClient(t), &Config{
		Prefix: "test",
		Global: []*Rule{{Window: 24 * time.Hour, Max: 100}},
		Rules: map[string][]*Rule{
			"register": {{Window: time.Minute, Max: 1}},
		},
	})

	ok, err := limiter.Allow(context.Background(), "register", "ratelimit@example.com")
	require.NoError(t, err)
	require.True(t, ok)

	ok, err = limiter.Allow(context.Background(), "register", "ratelimit@example.com")
	require.NoError(t, err)
	require.False(t, ok)
}

func TestRedisLimiter_Allow_globalExceeded(t *testing.T) {
	limiter := NewRedisLimiter(testRedisClient(t), &Config{
		Prefix: "test",
		Global: []*Rule{{Window: 24 * time.Hour, Max: 2}},
		Rules:  map[string][]*Rule{},
	})

	ok, _ := limiter.Allow(context.Background(), "register", "global@example.com")
	require.True(t, ok)
	ok, _ = limiter.Allow(context.Background(), "login", "global@example.com")
	require.True(t, ok)
	ok, _ = limiter.Allow(context.Background(), "register", "global@example.com")
	require.False(t, ok)
}

func TestRedisLimiter_Allow_multipleGlobalRules(t *testing.T) {
	limiter := NewRedisLimiter(testRedisClient(t), &Config{
		Prefix: "test",
		Global: []*Rule{
			{Window: time.Minute, Max: 3},
			{Window: 24 * time.Hour, Max: 10},
		},
		Rules: map[string][]*Rule{},
	})

	for i := 0; i < 3; i++ {
		ok, _ := limiter.Allow(context.Background(), "register", "multi@example.com")
		require.True(t, ok)
	}
	ok, _ := limiter.Allow(context.Background(), "register", "multi@example.com")
	require.False(t, ok)
}

func TestRedisLimiter_Allow_noRules(t *testing.T) {
	limiter := NewRedisLimiter(testRedisClient(t), &Config{
		Prefix: "test",
	})

	ok, err := limiter.Allow(context.Background(), "register", "norules@example.com")
	require.NoError(t, err)
	require.True(t, ok)
}

func TestRedisLimiter_Allow_differentPrefixes(t *testing.T) {
	client := testRedisClient(t)

	captchaLimiter := NewRedisLimiter(client, &Config{
		Prefix: "captcha:rate",
		Rules: map[string][]*Rule{
			"login": {{Window: time.Minute, Max: 1}},
		},
	})
	loginLimiter := NewRedisLimiter(client, &Config{
		Prefix: "login:rate",
		Rules: map[string][]*Rule{
			"fail": {{Window: time.Minute, Max: 1}},
		},
	})

	// Captcha limit reached
	ok, _ := captchaLimiter.Allow(context.Background(), "login", "user@test.com")
	require.True(t, ok)
	ok, _ = captchaLimiter.Allow(context.Background(), "login", "user@test.com")
	require.False(t, ok)

	// Login limiter is independent
	ok, _ = loginLimiter.Allow(context.Background(), "fail", "user@test.com")
	require.True(t, ok)
}

func TestRedisLimiter_Stats_empty(t *testing.T) {
	limiter := NewRedisLimiter(testRedisClient(t), &Config{
		Prefix: "test",
		Global: []*Rule{{Window: 24 * time.Hour, Max: 100}},
		Rules: map[string][]*Rule{
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
	limiter := NewRedisLimiter(testRedisClient(t), &Config{
		Prefix: "test",
		Global: []*Rule{{Window: time.Hour, Max: 10}},
		Rules: map[string][]*Rule{
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
	limiter := NewRedisLimiter(testRedisClient(t), &Config{
		Prefix: "test",
		Global: []*Rule{
			{Window: 24 * time.Hour, Max: 100},
			{Window: time.Hour, Max: 20},
		},
		Rules: map[string][]*Rule{
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
	limiter := NewRedisLimiter(testRedisClient(t), &Config{
		Prefix: "test",
	})

	stats, err := limiter.Stats(context.Background(), "anyone@example.com")
	require.NoError(t, err)
	require.Empty(t, stats)
}

func TestRedisLimiter_Reset_allPurposes(t *testing.T) {
	limiter := NewRedisLimiter(testRedisClient(t), &Config{
		Prefix: "test",
		Global: []*Rule{{Window: time.Hour, Max: 2}},
		Rules: map[string][]*Rule{
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

func TestRedisLimiter_ResetPurpose_isolated(t *testing.T) {
	limiter := NewRedisLimiter(testRedisClient(t), &Config{
		Prefix: "test",
		Rules: map[string][]*Rule{
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
	limiter := NewRedisLimiter(testRedisClient(t), &Config{
		Prefix: "test",
		Global: []*Rule{{Window: time.Hour, Max: 1}},
		Rules: map[string][]*Rule{
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
	limiter := NewRedisLimiter(testRedisClient(t), &Config{
		Prefix: "test",
		Rules: map[string][]*Rule{
			"register": {{Window: time.Minute, Max: 1}},
		},
	})

	deleted, err := limiter.ResetPurpose(context.Background(), "nope", "x@example.com")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unknown purpose")
	require.Equal(t, int64(0), deleted)
}
