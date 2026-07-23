package redisx

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestLock(t *testing.T) (*redis.Client, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()}), mr
}

// syncClock advances miniredis time in step with real time,
// so that key expiry works naturally with time.Ticker and time.Sleep.
func syncClock(t *testing.T, mr *miniredis.Miniredis) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-done:
				return
			case <-time.After(5 * time.Millisecond):
				mr.FastForward(5 * time.Millisecond)
			}
		}
	}()
	t.Cleanup(func() { close(done) })
}

func TestNewLock_Validation(t *testing.T) {
	client, _ := newTestLock(t)

	_, err := NewLock(client, &LockConfig{Prefix: "lock", TTL: 0})
	assert.EqualError(t, err, "redisx: lock TTL must be positive")

	_, err = NewLock(client, &LockConfig{Prefix: "", TTL: time.Second})
	assert.EqualError(t, err, "redisx: lock prefix is required")

	_, err = NewLock(client, &LockConfig{Prefix: "lock", TTL: time.Second})
	assert.NoError(t, err)
}

func TestNewLock_Defaults(t *testing.T) {
	client, _ := newTestLock(t)

	lock, err := NewLock(client, &LockConfig{Prefix: "lock", TTL: time.Second})
	require.NoError(t, err)
	assert.Equal(t, 16, lock.cfg.Tries)
	assert.Equal(t, 100*time.Millisecond, lock.cfg.Wait)
}

func TestLock_AcquireAndRelease(t *testing.T) {
	client, _ := newTestLock(t)
	lock, err := NewLock(client, &LockConfig{
		Prefix: "lock",
		TTL:    10 * time.Second,
		Tries:  1,
	})
	require.NoError(t, err)

	id, err := lock.Acquire(context.Background(), "order:123")
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	err = lock.Release(context.Background(), "order:123", id)
	assert.NoError(t, err)
}

func TestLock_Acquire_FailsWhenLocked(t *testing.T) {
	client, _ := newTestLock(t)
	lock, _ := NewLock(client, &LockConfig{
		Prefix: "lock",
		TTL:    10 * time.Second,
		Tries:  1,
	})

	_, err := lock.Acquire(context.Background(), "order:123")
	require.NoError(t, err)

	_, err = lock.Acquire(context.Background(), "order:123")
	assert.ErrorIs(t, err, ErrLockFailed)
}

func TestLock_Acquire_RetrySuccess(t *testing.T) {
	client, _ := newTestLock(t)
	lock, _ := NewLock(client, &LockConfig{
		Prefix: "lock",
		TTL:    10 * time.Second,
		Tries:  10,
		Wait:   10 * time.Millisecond,
	})

	id1, err := lock.Acquire(context.Background(), "order:123")
	require.NoError(t, err)

	released := make(chan struct{})
	go func() {
		time.Sleep(30 * time.Millisecond)
		lock.Release(context.Background(), "order:123", id1)
		close(released)
	}()

	id2, err := lock.Acquire(context.Background(), "order:123")
	assert.NoError(t, err)
	assert.NotEqual(t, id1, id2)
	<-released
}

func TestLock_Release_WrongIdentifier(t *testing.T) {
	client, _ := newTestLock(t)
	lock, _ := NewLock(client, &LockConfig{
		Prefix: "lock",
		TTL:    10 * time.Second,
	})

	_, err := lock.Acquire(context.Background(), "order:123")
	require.NoError(t, err)

	err = lock.Release(context.Background(), "order:123", "wrong-id")
	assert.ErrorIs(t, err, ErrUnlockFailed)
}

func TestLock_Release_ExpiredLock(t *testing.T) {
	client, mr := newTestLock(t)
	lock, _ := NewLock(client, &LockConfig{
		Prefix: "lock",
		TTL:    5 * time.Second,
	})

	id, err := lock.Acquire(context.Background(), "order:123")
	require.NoError(t, err)

	mr.FastForward(6 * time.Second)

	err = lock.Release(context.Background(), "order:123", id)
	assert.ErrorIs(t, err, ErrUnlockFailed)
}

func TestLock_Acquire_AfterExpiry(t *testing.T) {
	client, mr := newTestLock(t)
	lock, _ := NewLock(client, &LockConfig{
		Prefix: "lock",
		TTL:    5 * time.Second,
		Tries:  1,
	})

	_, err := lock.Acquire(context.Background(), "order:123")
	require.NoError(t, err)

	mr.FastForward(6 * time.Second)

	id, err := lock.Acquire(context.Background(), "order:123")
	assert.NoError(t, err)
	assert.NotEmpty(t, id)
}

func TestLock_Acquire_ContextCancelled(t *testing.T) {
	client, _ := newTestLock(t)
	lock, _ := NewLock(client, &LockConfig{
		Prefix: "lock",
		TTL:    time.Minute,
		Tries:  1000,
		Wait:   100 * time.Millisecond,
	})

	_, err := lock.Acquire(context.Background(), "order:123")
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err = lock.Acquire(ctx, "order:123")
	elapsed := time.Since(start)

	assert.True(t, errors.Is(err, context.DeadlineExceeded))
	assert.Less(t, elapsed, 500*time.Millisecond)
}

func TestLock_DifferentTargets(t *testing.T) {
	client, _ := newTestLock(t)
	lock, _ := NewLock(client, &LockConfig{
		Prefix: "lock",
		TTL:    10 * time.Second,
		Tries:  1,
	})

	id1, err := lock.Acquire(context.Background(), "order:1")
	require.NoError(t, err)

	id2, err := lock.Acquire(context.Background(), "order:2")
	require.NoError(t, err)
	assert.NotEqual(t, id1, id2)

	assert.NoError(t, lock.Release(context.Background(), "order:1", id1))
	assert.NoError(t, lock.Release(context.Background(), "order:2", id2))
}

func TestLock_Renew(t *testing.T) {
	client, mr := newTestLock(t)
	lock, _ := NewLock(client, &LockConfig{
		Prefix: "lock",
		TTL:    5 * time.Second,
	})

	id, err := lock.Acquire(context.Background(), "order:123")
	require.NoError(t, err)

	// Advance close to expiry
	mr.FastForward(4 * time.Second)

	// Renew extends the TTL
	err = lock.Renew(context.Background(), "order:123", id)
	assert.NoError(t, err)

	// Advance past the original TTL — lock should still be held
	mr.FastForward(4 * time.Second)
	err = lock.Release(context.Background(), "order:123", id)
	assert.NoError(t, err)
}

func TestLock_Renew_WrongIdentifier(t *testing.T) {
	client, _ := newTestLock(t)
	lock, _ := NewLock(client, &LockConfig{
		Prefix: "lock",
		TTL:    5 * time.Second,
	})

	_, err := lock.Acquire(context.Background(), "order:123")
	require.NoError(t, err)

	err = lock.Renew(context.Background(), "order:123", "wrong-id")
	assert.ErrorIs(t, err, ErrRenewFailed)
}

func TestLock_Renew_ExpiredLock(t *testing.T) {
	client, mr := newTestLock(t)
	lock, _ := NewLock(client, &LockConfig{
		Prefix: "lock",
		TTL:    5 * time.Second,
	})

	id, err := lock.Acquire(context.Background(), "order:123")
	require.NoError(t, err)

	mr.FastForward(6 * time.Second)

	err = lock.Renew(context.Background(), "order:123", id)
	assert.ErrorIs(t, err, ErrRenewFailed)
}

func TestLock_KeepAlive_ExtendsLock(t *testing.T) {
	client, mr := newTestLock(t)
	syncClock(t, mr)
	lock, _ := NewLock(client, &LockConfig{
		Prefix: "lock",
		TTL:    200 * time.Millisecond,
	})

	id, err := lock.Acquire(context.Background(), "order:123")
	require.NoError(t, err)

	cancel := lock.KeepAlive(context.Background(), "order:123", id)
	defer cancel()

	// Sleep past original TTL — KeepAlive should have renewed it
	time.Sleep(400 * time.Millisecond)

	err = lock.Release(context.Background(), "order:123", id)
	assert.NoError(t, err, "lock should still be held after KeepAlive renewal")
}

func TestLock_KeepAlive_StopsOnCancel(t *testing.T) {
	client, mr := newTestLock(t)
	syncClock(t, mr)
	lock, _ := NewLock(client, &LockConfig{
		Prefix: "lock",
		TTL:    200 * time.Millisecond,
	})

	id, err := lock.Acquire(context.Background(), "order:123")
	require.NoError(t, err)

	cancel := lock.KeepAlive(context.Background(), "order:123", id)

	// Let one renewal happen
	time.Sleep(100 * time.Millisecond)
	cancel()

	// Wait for TTL to expire — no more renewals
	time.Sleep(300 * time.Millisecond)

	err = lock.Release(context.Background(), "order:123", id)
	assert.ErrorIs(t, err, ErrUnlockFailed)
}
