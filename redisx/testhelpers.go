package redisx

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

// NewTestClient creates a Redis client backed by miniredis for testing.
// The miniredis server is automatically cleaned up when the test finishes.
func NewTestClient(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	return redis.NewClient(&redis.Options{Addr: mr.Addr()})
}
