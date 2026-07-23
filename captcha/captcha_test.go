package captcha

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/servekit/go-common/ratelimit"
	"github.com/servekit/go-common/redisx"
)

// testRedisClient starts an in-memory Redis (miniredis), cleaned up after the test.
func testRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	return redisx.NewTestClient(t)
}

// testConfig returns a basic working config for "register" purpose.
func testConfig(t *testing.T) *Config {
	t.Helper()
	return &Config{
		Prefix:      "captcha",
		MaxAttempts: 3,
		GlobalRules: []*ratelimit.Rule{{Window: 24 * time.Hour, Max: 100}},
		Purposes: map[string]*PurposeConfig{
			"register": {
				CodeFormat: FormatDigit6,
				RateRules:  []*ratelimit.Rule{{Window: 5 * time.Minute, Max: 10}},
			},
		},
	}
}

// newTestCaptcha creates a Captcha from testConfig.
func newTestCaptcha(t *testing.T) *Captcha {
	t.Helper()
	c, err := New(testConfig(t), WithRedisClient(testRedisClient(t)))
	require.NoError(t, err)
	return c
}

func TestNew_missingRedis(t *testing.T) {
	cfg := Config{
		Purposes: map[string]*PurposeConfig{
			"register": {CodeFormat: FormatDigit6},
		},
	}
	_, err := New(&cfg)
	require.Error(t, err)
	require.Contains(t, err.Error(), "redis addr is required")
}

func TestNew_withRedisClient(t *testing.T) {
	client := testRedisClient(t)
	cfg := Config{
		Purposes: map[string]*PurposeConfig{
			"register": {CodeFormat: FormatDigit6},
		},
	}
	c, err := New(&cfg, WithRedisClient(client))
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestCaptcha_Generate_basic(t *testing.T) {
	c := newTestCaptcha(t)
	ctx := context.Background()

	captchaID, code, err := c.Generate(ctx, "test@example.com", "register", "email")
	require.NoError(t, err)
	require.NotEmpty(t, captchaID)
	require.Len(t, code, 6)
}

func TestCaptcha_Generate_sms(t *testing.T) {
	c := newTestCaptcha(t)
	ctx := context.Background()

	captchaID, code, err := c.Generate(ctx, "13800001111", "register", "sms")
	require.NoError(t, err)
	require.NotEmpty(t, captchaID)
	require.Len(t, code, 6)
}

func TestCaptcha_Generate_differentChannels(t *testing.T) {
	c := newTestCaptcha(t)
	ctx := context.Background()

	// Same target + purpose, different channels -> independent codes.
	id1, code1, err := c.Generate(ctx, "user@example.com", "register", "email")
	require.NoError(t, err)

	id2, code2, err := c.Generate(ctx, "user@example.com", "register", "sms")
	require.NoError(t, err)

	require.NotEqual(t, id1, id2)

	// Verify each code works with its own channel.
	result, err := c.Verify(ctx, "user@example.com", code1, "register", "email")
	require.NoError(t, err)
	require.True(t, result.Matched)

	result, err = c.Verify(ctx, "user@example.com", code2, "register", "sms")
	require.NoError(t, err)
	require.True(t, result.Matched)
}

func TestCaptcha_Verify_correct(t *testing.T) {
	client := testRedisClient(t)
	store := NewRedisStore(client, "captcha")
	ctx := context.Background()

	store.Set(ctx, "register", "email", "direct@example.com", &Record{
		CaptchaID: "test-captcha-id", Code: "123456",
		Target: "direct@example.com", Purpose: "register", MaxAttempts: 3,
	}, 5*time.Minute)

	c, err := New(&Config{
		Prefix: "captcha",
		Purposes: map[string]*PurposeConfig{
			"register": {CodeFormat: FormatDigit6},
		},
	}, WithRedisClient(client))
	require.NoError(t, err)

	result, err := c.Verify(ctx, "direct@example.com", "123456", "register", "email")
	require.NoError(t, err)
	require.True(t, result.Matched)
	require.Equal(t, "test-captcha-id", result.CaptchaID)
}

func TestCaptcha_Verify_wrongCode(t *testing.T) {
	client := testRedisClient(t)
	store := NewRedisStore(client, "captcha")
	ctx := context.Background()

	c, err := New(&Config{
		Prefix: "captcha",
		Purposes: map[string]*PurposeConfig{
			"register": {CodeFormat: FormatDigit6},
		},
	}, WithRedisClient(client))
	require.NoError(t, err)

	store.Set(ctx, "register", "email", "wrong@example.com", &Record{
		CaptchaID: "test-id", Code: "123456",
		Target: "wrong@example.com", Purpose: "register", MaxAttempts: 3,
	}, 5*time.Minute)

	result, err := c.Verify(ctx, "wrong@example.com", "000000", "register", "email")
	require.NoError(t, err)
	require.False(t, result.Matched)
	require.Equal(t, "test-id", result.CaptchaID)
	require.Equal(t, 2, result.RemainingAttempts)
}

func TestCaptcha_Verify_maxAttemptsExceeded(t *testing.T) {
	client := testRedisClient(t)
	store := NewRedisStore(client, "captcha")
	ctx := context.Background()

	c, err := New(&Config{
		Prefix:      "captcha",
		MaxAttempts: 2,
		Purposes: map[string]*PurposeConfig{
			"register": {CodeFormat: FormatDigit6},
		},
	}, WithRedisClient(client))
	require.NoError(t, err)

	store.Set(ctx, "register", "email", "max@example.com", &Record{
		CaptchaID: "max-id", Code: "123456",
		Target: "max@example.com", Purpose: "register", MaxAttempts: 2,
	}, 5*time.Minute)

	result, err := c.Verify(ctx, "max@example.com", "000000", "register", "email")
	require.NoError(t, err)
	require.False(t, result.Matched)
	require.Equal(t, 1, result.RemainingAttempts)

	result, err = c.Verify(ctx, "max@example.com", "000000", "register", "email")
	require.NoError(t, err)
	require.False(t, result.Matched)
	require.Equal(t, 0, result.RemainingAttempts)

	result, err = c.Verify(ctx, "max@example.com", "123456", "register", "email")
	require.NoError(t, err)
	require.False(t, result.Matched)
}

func TestCaptcha_Verify_expired(t *testing.T) {
	client := testRedisClient(t)

	c, err := New(&Config{
		Prefix: "captcha",
		Purposes: map[string]*PurposeConfig{
			"register": {CodeFormat: FormatDigit6},
		},
	}, WithRedisClient(client))
	require.NoError(t, err)

	result, err := c.Verify(context.Background(), "nobody@example.com", "123456", "register", "email")
	require.NoError(t, err)
	require.False(t, result.Matched)
	require.Equal(t, 0, result.RemainingAttempts)
}

func TestCaptcha_Generate_rateLimited(t *testing.T) {
	client := testRedisClient(t)

	c, err := New(&Config{
		Prefix:      "captcha",
		MaxAttempts: 3,
		GlobalRules: []*ratelimit.Rule{{Window: 24 * time.Hour, Max: 100}},
		Purposes: map[string]*PurposeConfig{
			"register": {CodeFormat: FormatDigit6, RateRules: []*ratelimit.Rule{{Window: time.Minute, Max: 1}}},
		},
	}, WithRedisClient(client))
	require.NoError(t, err)

	_, _, err = c.Generate(context.Background(), "ratelimit@example.com", "register", "email")
	require.NoError(t, err)

	_, _, err = c.Generate(context.Background(), "ratelimit@example.com", "register", "email")
	require.Error(t, err)
	require.Contains(t, err.Error(), "rate limit exceeded")
}

func TestCaptcha_Generate_invalidParams(t *testing.T) {
	c := newTestCaptcha(t)
	ctx := context.Background()

	_, _, err := c.Generate(ctx, "", "register", "email")
	require.Error(t, err)
	require.Contains(t, err.Error(), "target is empty")

	_, _, err = c.Generate(ctx, "test@example.com", "", "email")
	require.Error(t, err)
	require.Contains(t, err.Error(), "purpose is empty")

	_, _, err = c.Generate(ctx, "test@example.com", "register", "")
	require.Error(t, err)
	require.Contains(t, err.Error(), "channel is empty")

	_, _, err = c.Generate(ctx, "test@example.com", "unknown", "email")
	require.Error(t, err)
	require.Contains(t, err.Error(), "not configured")
}

func TestCaptcha_Generate_overwritesOldCode(t *testing.T) {
	c := newTestCaptcha(t)
	ctx := context.Background()

	id1, code1, err := c.Generate(ctx, "overwrite@example.com", "register", "email")
	require.NoError(t, err)
	require.NotEmpty(t, id1)

	id2, code2, err := c.Generate(ctx, "overwrite@example.com", "register", "email")
	require.NoError(t, err)
	require.NotEmpty(t, id2)
	require.NotEqual(t, id1, id2)

	// Old code no longer works.
	result, err := c.Verify(ctx, "overwrite@example.com", code1, "register", "email")
	require.NoError(t, err)
	require.False(t, result.Matched)

	// New code works.
	result, err = c.Verify(ctx, "overwrite@example.com", code2, "register", "email")
	require.NoError(t, err)
	require.True(t, result.Matched)
}

func TestCaptcha_Generate_withSend(t *testing.T) {
	c := newTestCaptcha(t)
	ctx := context.Background()

	var gotCode string
	captchaID, code, err := c.Generate(ctx, "test@example.com", "register", "email",
		WithSend(func(_ context.Context, _, c, _, _ string) error {
			gotCode = c
			return nil
		}),
	)
	require.NoError(t, err)
	require.NotEmpty(t, captchaID)
	require.Len(t, code, 6)
	require.Equal(t, code, gotCode)

	// Code is stored and can be verified.
	result, err := c.Verify(ctx, "test@example.com", code, "register", "email")
	require.NoError(t, err)
	require.True(t, result.Matched)
}

func TestCaptcha_Generate_withSendError(t *testing.T) {
	c := newTestCaptcha(t)
	ctx := context.Background()

	captchaID, code, err := c.Generate(ctx, "test@example.com", "register", "email",
		WithSend(func(_ context.Context, _, _, _, _ string) error {
			return fmt.Errorf("SMTP down")
		}),
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "send code")
	// Code is still returned even on send error.
	require.NotEmpty(t, captchaID)
	require.NotEmpty(t, code)

	// Code is still stored and can be verified despite send failure.
	result, err := c.Verify(ctx, "test@example.com", code, "register", "email")
	require.NoError(t, err)
	require.True(t, result.Matched)
}

func TestCaptcha_ttlForPurpose(t *testing.T) {
	tests := []struct {
		name    string
		purpose string
		config  Config
		want    time.Duration
	}{
		{
			name:    "shortest window",
			purpose: "register",
			config: Config{
				Purposes: map[string]*PurposeConfig{
					"register": {
						RateRules: []*ratelimit.Rule{
							{Window: 10 * time.Minute, Max: 100},
							{Window: 3 * time.Minute, Max: 5},
							{Window: 1 * time.Hour, Max: 500},
						},
					},
				},
			},
			want: 3 * time.Minute,
		},
		{
			name:    "no rules returns default",
			purpose: "login",
			config: Config{
				Purposes: map[string]*PurposeConfig{
					"login": {},
				},
			},
			want: defaultTTL,
		},
		{
			name:    "unknown purpose returns default",
			purpose: "unknown",
			config:  Config{},
			want:    defaultTTL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := testRedisClient(t)
			c, err := New(&tt.config, WithRedisClient(client))
			require.NoError(t, err)
			got := c.ttlForPurpose(tt.purpose)
			require.Equal(t, tt.want, got)
		})
	}
}

func TestCaptcha_Verify_withCaptchaIDMatch(t *testing.T) {
	c := newTestCaptcha(t)
	ctx := context.Background()

	captchaID, code, err := c.Generate(ctx, "bind@example.com", "register", "email")
	require.NoError(t, err)
	require.NotEmpty(t, captchaID)

	result, err := c.Verify(ctx, "bind@example.com", code, "register", "email",
		WithCaptchaID(captchaID),
	)
	require.NoError(t, err)
	require.True(t, result.Matched)
	require.Equal(t, captchaID, result.CaptchaID)
}

func TestCaptcha_Verify_withCaptchaIDMismatch(t *testing.T) {
	c := newTestCaptcha(t)
	ctx := context.Background()

	captchaID, code, err := c.Generate(ctx, "mismatch@example.com", "register", "email")
	require.NoError(t, err)
	require.NotEmpty(t, code)

	// Correct code, wrong captchaID -> mismatch, one attempt consumed.
	result, err := c.Verify(ctx, "mismatch@example.com", code, "register", "email",
		WithCaptchaID("00000000-0000-0000-0000-000000000000"),
	)
	require.NoError(t, err)
	require.False(t, result.Matched)
	require.Equal(t, captchaID, result.CaptchaID)
	require.Equal(t, 2, result.RemainingAttempts)

	// Same code with the correct captchaID now succeeds (one attempt left).
	result, err = c.Verify(ctx, "mismatch@example.com", code, "register", "email",
		WithCaptchaID(captchaID),
	)
	require.NoError(t, err)
	require.True(t, result.Matched)
}

func TestCaptcha_Verify_withCaptchaIDEmpty(t *testing.T) {
	c := newTestCaptcha(t)
	ctx := context.Background()

	_, code, err := c.Generate(ctx, "empty@example.com", "register", "email")
	require.NoError(t, err)

	// Empty captchaID is equivalent to not passing the option at all.
	result, err := c.Verify(ctx, "empty@example.com", code, "register", "email",
		WithCaptchaID(""),
	)
	require.NoError(t, err)
	require.True(t, result.Matched)
}

func TestCaptcha_Verify_captchaIDEmptyEquivalence(t *testing.T) {
	c := newTestCaptcha(t)
	ctx := context.Background()

	// Two identical setups — one Verify omits the option, one passes "".
	_, code1, err := c.Generate(ctx, "eq1@example.com", "register", "email")
	require.NoError(t, err)

	_, code2, err := c.Generate(ctx, "eq2@example.com", "register", "email")
	require.NoError(t, err)

	// No option.
	r1, err := c.Verify(ctx, "eq1@example.com", code1, "register", "email")
	require.NoError(t, err)

	// With WithCaptchaID("").
	r2, err := c.Verify(ctx, "eq2@example.com", code2, "register", "email",
		WithCaptchaID(""),
	)
	require.NoError(t, err)

	require.Equal(t, r1.Matched, r2.Matched)
	require.True(t, r2.Matched) // sanity: both should actually match
}
