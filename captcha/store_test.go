package captcha

import (
	"context"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"

	"github.com/servekit/go-common/redisx"
)

// testStoreRedisClient starts an in-memory Redis for store tests.
func testStoreRedisClient(t *testing.T) *redis.Client {
	t.Helper()
	return redisx.NewTestClient(t)
}

func TestRedisStore_SetAndVerify(t *testing.T) {
	store := NewRedisStore(testStoreRedisClient(t), "captcha")

	err := store.Set(context.Background(), "register", "email", "user@example.com", &Record{
		CaptchaID: "s1", Code: "123456", Target: "user@example.com",
		Purpose: "register", MaxAttempts: 3,
	}, 5*time.Minute)
	require.NoError(t, err)

	result, err := store.Verify(context.Background(), "register", "email", "user@example.com", "123456", "")
	require.NoError(t, err)
	require.True(t, result.Matched)
	require.Equal(t, "s1", result.CaptchaID)
	require.Equal(t, "user@example.com", result.Target)
	require.Equal(t, "register", result.Purpose)
	require.Equal(t, -1, result.RemainingAttempts)
}

func TestRedisStore_Verify_wrongCode(t *testing.T) {
	store := NewRedisStore(testStoreRedisClient(t), "captcha")

	err := store.Set(context.Background(), "login", "sms", "user@example.com", &Record{
		CaptchaID: "s2", Code: "123456", Target: "user@example.com",
		Purpose: "login", MaxAttempts: 3,
	}, 5*time.Minute)
	require.NoError(t, err)

	result, err := store.Verify(context.Background(), "login", "sms", "user@example.com", "000000", "")
	require.NoError(t, err)
	require.False(t, result.Matched)
	require.Equal(t, "s2", result.CaptchaID)
	require.Equal(t, 2, result.RemainingAttempts)

	result, err = store.Verify(context.Background(), "login", "sms", "user@example.com", "000000", "")
	require.NoError(t, err)
	require.False(t, result.Matched)
	require.Equal(t, 1, result.RemainingAttempts)
}

func TestRedisStore_Verify_maxAttemptsExceeded(t *testing.T) {
	store := NewRedisStore(testStoreRedisClient(t), "captcha")

	err := store.Set(context.Background(), "register", "email", "user@example.com", &Record{
		CaptchaID: "s3", Code: "123456", Target: "user@example.com",
		Purpose: "register", MaxAttempts: 2,
	}, 5*time.Minute)
	require.NoError(t, err)

	result, err := store.Verify(context.Background(), "register", "email", "user@example.com", "000000", "")
	require.NoError(t, err)
	require.False(t, result.Matched)
	require.Equal(t, 1, result.RemainingAttempts)

	result, err = store.Verify(context.Background(), "register", "email", "user@example.com", "000000", "")
	require.NoError(t, err)
	require.False(t, result.Matched)
	require.Equal(t, 0, result.RemainingAttempts)

	result, err = store.Verify(context.Background(), "register", "email", "user@example.com", "123456", "")
	require.NoError(t, err)
	require.False(t, result.Matched)
}

func TestRedisStore_Verify_expired(t *testing.T) {
	store := NewRedisStore(testStoreRedisClient(t), "captcha")

	result, err := store.Verify(context.Background(), "register", "email", "nobody@example.com", "123456", "")
	require.NoError(t, err)
	require.False(t, result.Matched)
	require.Equal(t, 0, result.RemainingAttempts)
}

func TestRedisStore_Verify_successThenExpired(t *testing.T) {
	store := NewRedisStore(testStoreRedisClient(t), "captcha")

	err := store.Set(context.Background(), "login", "sms", "13800001111", &Record{
		CaptchaID: "s4", Code: "654321", Target: "13800001111",
		Purpose: "login", MaxAttempts: 3,
	}, 5*time.Minute)
	require.NoError(t, err)

	result, err := store.Verify(context.Background(), "login", "sms", "13800001111", "654321", "")
	require.NoError(t, err)
	require.True(t, result.Matched)
	require.Equal(t, "s4", result.CaptchaID)

	result, err = store.Verify(context.Background(), "login", "sms", "13800001111", "654321", "")
	require.NoError(t, err)
	require.False(t, result.Matched)
}

func TestRedisStore_Verify_wrongThenCorrect(t *testing.T) {
	store := NewRedisStore(testStoreRedisClient(t), "captcha")

	err := store.Set(context.Background(), "reset", "email", "user@test.com", &Record{
		CaptchaID: "s5", Code: "111222", Target: "user@test.com",
		Purpose: "reset", MaxAttempts: 3,
	}, 5*time.Minute)
	require.NoError(t, err)

	result, err := store.Verify(context.Background(), "reset", "email", "user@test.com", "000000", "")
	require.NoError(t, err)
	require.False(t, result.Matched)
	require.Equal(t, 2, result.RemainingAttempts)

	result, err = store.Verify(context.Background(), "reset", "email", "user@test.com", "111222", "")
	require.NoError(t, err)
	require.True(t, result.Matched)
	require.Equal(t, "user@test.com", result.Target)
}

func TestRedisStore_Verify_preExhausted(t *testing.T) {
	store := NewRedisStore(testStoreRedisClient(t), "captcha")

	err := store.Set(context.Background(), "login", "sms", "exhausted@test.com", &Record{
		CaptchaID: "s6", Code: "123456", Target: "exhausted@test.com",
		Purpose: "login", Attempts: 3, MaxAttempts: 3,
	}, 5*time.Minute)
	require.NoError(t, err)

	result, err := store.Verify(context.Background(), "login", "sms", "exhausted@test.com", "123456", "")
	require.NoError(t, err)
	require.False(t, result.Matched)
	require.Equal(t, "s6", result.CaptchaID)
	require.Equal(t, "exhausted@test.com", result.Target)
	require.Equal(t, "login", result.Purpose)
	require.Equal(t, 0, result.RemainingAttempts)
}

func TestRedisStore_keyPrefix(t *testing.T) {
	store := NewRedisStore(testStoreRedisClient(t), "myapp")

	err := store.Set(context.Background(), "login", "sms", "user@test.com", &Record{
		CaptchaID: "s7", Code: "123456", Target: "user@test.com",
		Purpose: "login", MaxAttempts: 3,
	}, 5*time.Minute)
	require.NoError(t, err)

	result, err := store.Verify(context.Background(), "login", "sms", "user@test.com", "123456", "")
	require.NoError(t, err)
	require.True(t, result.Matched)
}

func TestRedisStore_overwrite(t *testing.T) {
	store := NewRedisStore(testStoreRedisClient(t), "captcha")

	err := store.Set(context.Background(), "register", "email", "user@example.com", &Record{
		CaptchaID: "old", Code: "111111", Target: "user@example.com",
		Purpose: "register", MaxAttempts: 3,
	}, 5*time.Minute)
	require.NoError(t, err)

	err = store.Set(context.Background(), "register", "email", "user@example.com", &Record{
		CaptchaID: "new", Code: "222222", Target: "user@example.com",
		Purpose: "register", MaxAttempts: 3,
	}, 5*time.Minute)
	require.NoError(t, err)

	// Old code no longer works.
	result, err := store.Verify(context.Background(), "register", "email", "user@example.com", "111111", "")
	require.NoError(t, err)
	require.False(t, result.Matched)

	// New code works with new captchaID.
	result, err = store.Verify(context.Background(), "register", "email", "user@example.com", "222222", "")
	require.NoError(t, err)
	require.True(t, result.Matched)
	require.Equal(t, "new", result.CaptchaID)
}

func TestRedisStore_differentChannels(t *testing.T) {
	store := NewRedisStore(testStoreRedisClient(t), "captcha")
	ctx := context.Background()

	// Store codes for same target+purpose but different channels.
	err := store.Set(ctx, "login", "email", "user@example.com", &Record{
		CaptchaID: "email-id", Code: "111111", Target: "user@example.com",
		Purpose: "login", MaxAttempts: 3,
	}, 5*time.Minute)
	require.NoError(t, err)

	err = store.Set(ctx, "login", "sms", "user@example.com", &Record{
		CaptchaID: "sms-id", Code: "222222", Target: "user@example.com",
		Purpose: "login", MaxAttempts: 3,
	}, 5*time.Minute)
	require.NoError(t, err)

	// Each channel verifies independently.
	result, err := store.Verify(ctx, "login", "email", "user@example.com", "111111", "")
	require.NoError(t, err)
	require.True(t, result.Matched)
	require.Equal(t, "email-id", result.CaptchaID)

	result, err = store.Verify(ctx, "login", "sms", "user@example.com", "222222", "")
	require.NoError(t, err)
	require.True(t, result.Matched)
	require.Equal(t, "sms-id", result.CaptchaID)

	// Cross-channel code does not work.
	result, err = store.Verify(ctx, "login", "email", "user@example.com", "222222", "")
	require.NoError(t, err)
	require.False(t, result.Matched)
}

func TestRedisStore_Verify_captchaIDMismatch(t *testing.T) {
	store := NewRedisStore(testStoreRedisClient(t), "captcha")
	ctx := context.Background()

	err := store.Set(ctx, "login", "sms", "user@example.com", &Record{
		CaptchaID: "correct-id", Code: "123456", Target: "user@example.com",
		Purpose: "login", MaxAttempts: 3,
	}, 5*time.Minute)
	require.NoError(t, err)

	// Correct code, wrong captchaID -> mismatch, one attempt consumed.
	result, err := store.Verify(ctx, "login", "sms", "user@example.com", "123456", "wrong-id")
	require.NoError(t, err)
	require.False(t, result.Matched)
	require.Equal(t, "correct-id", result.CaptchaID)
	require.Equal(t, 2, result.RemainingAttempts)

	// Now verify with correct captchaID + correct code -> matched, despite the
	// earlier mismatch having consumed one attempt.
	result, err = store.Verify(ctx, "login", "sms", "user@example.com", "123456", "correct-id")
	require.NoError(t, err)
	require.True(t, result.Matched)
	require.Equal(t, "correct-id", result.CaptchaID)
}

func TestRedisStore_Verify_captchaIDMatch(t *testing.T) {
	store := NewRedisStore(testStoreRedisClient(t), "captcha")
	ctx := context.Background()

	err := store.Set(ctx, "register", "email", "user@example.com", &Record{
		CaptchaID: "id-1", Code: "654321", Target: "user@example.com",
		Purpose: "register", MaxAttempts: 3,
	}, 5*time.Minute)
	require.NoError(t, err)

	result, err := store.Verify(ctx, "register", "email", "user@example.com", "654321", "id-1")
	require.NoError(t, err)
	require.True(t, result.Matched)
	require.Equal(t, "id-1", result.CaptchaID)
}

func TestRedisStore_Verify_captchaIDEmptySkipsCheck(t *testing.T) {
	store := NewRedisStore(testStoreRedisClient(t), "captcha")
	ctx := context.Background()

	// Stored captchaID is "real-id" but caller passes "" -> skip check -> match.
	err := store.Set(ctx, "register", "email", "user@example.com", &Record{
		CaptchaID: "real-id", Code: "111222", Target: "user@example.com",
		Purpose: "register", MaxAttempts: 3,
	}, 5*time.Minute)
	require.NoError(t, err)

	result, err := store.Verify(ctx, "register", "email", "user@example.com", "111222", "")
	require.NoError(t, err)
	require.True(t, result.Matched)
	require.Equal(t, "real-id", result.CaptchaID)
}

func TestRedisStore_Verify_captchaIDAndCodeBothWrong(t *testing.T) {
	store := NewRedisStore(testStoreRedisClient(t), "captcha")
	ctx := context.Background()

	err := store.Set(ctx, "login", "sms", "user@example.com", &Record{
		CaptchaID: "id-2", Code: "123456", Target: "user@example.com",
		Purpose: "login", MaxAttempts: 3,
	}, 5*time.Minute)
	require.NoError(t, err)

	// Both wrong: attempts must increment exactly once, not twice.
	result, err := store.Verify(ctx, "login", "sms", "user@example.com", "000000", "wrong-id")
	require.NoError(t, err)
	require.False(t, result.Matched)
	require.Equal(t, 2, result.RemainingAttempts)
}
