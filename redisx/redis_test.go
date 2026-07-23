package redisx

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"
)

func TestConfig_Validate(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(*Config)
		wantErr string // substring; empty means no error expected
	}{
		{
			name:   "standalone minimal",
			mutate: func(c *Config) { c.Addr = "localhost:6379" },
		},
		{
			name: "sentinel complete",
			mutate: func(c *Config) {
				c.MasterName = "mymaster"
				c.SentinelAddrs = []string{"sentinel-0:26379", "sentinel-1:26379"}
			},
		},
		{
			name:    "nothing set",
			mutate:  func(c *Config) {},
			wantErr: "Addr is required",
		},
		{
			name: "MasterName without SentinelAddrs",
			mutate: func(c *Config) {
				c.MasterName = "mymaster"
			},
			wantErr: "SentinelAddrs is required",
		},
		{
			name: "SentinelAddrs without MasterName",
			mutate: func(c *Config) {
				c.SentinelAddrs = []string{"sentinel-0:26379"}
			},
			wantErr: "MasterName is required",
		},
		{
			name: "PoolSize negative",
			mutate: func(c *Config) {
				c.Addr = "localhost:6379"
				c.PoolSize = -1
			},
			wantErr: "PoolSize must be >= 0",
		},
		{
			name: "MinIdleConns negative",
			mutate: func(c *Config) {
				c.Addr = "localhost:6379"
				c.MinIdleConns = -1
			},
			wantErr: "MinIdleConns must be >= 0",
		},
		{
			name: "MinIdleConns exceeds PoolSize",
			mutate: func(c *Config) {
				c.Addr = "localhost:6379"
				c.PoolSize = 10
				c.MinIdleConns = 20
			},
			wantErr: "MinIdleConns cannot exceed PoolSize",
		},
		{
			name: "MinIdleConns equal to PoolSize is allowed",
			mutate: func(c *Config) {
				c.Addr = "localhost:6379"
				c.PoolSize = 10
				c.MinIdleConns = 10
			},
		},
		{
			name: "DialTimeout zero uses default",
			mutate: func(c *Config) {
				c.Addr = "localhost:6379"
				c.DialTimeout = 0
			},
		},
		{
			name: "DialTimeout below 100ms rejected",
			mutate: func(c *Config) {
				c.Addr = "localhost:6379"
				c.DialTimeout = 50 * time.Millisecond
			},
			wantErr: "DialTimeout must be 0 or >=",
		},
		{
			name: "ReadTimeout below 1s rejected",
			mutate: func(c *Config) {
				c.Addr = "localhost:6379"
				c.ReadTimeout = 500 * time.Millisecond
			},
			wantErr: "ReadTimeout must be 0 or >=",
		},
		{
			name: "WriteTimeout below 1s rejected",
			mutate: func(c *Config) {
				c.Addr = "localhost:6379"
				c.WriteTimeout = 500 * time.Millisecond
			},
			wantErr: "WriteTimeout must be 0 or >=",
		},
		{
			name: "MaxRetries negative two rejected",
			mutate: func(c *Config) {
				c.Addr = "localhost:6379"
				c.MaxRetries = -2
			},
			wantErr: "MaxRetries must be >= -1",
		},
		{
			name: "MaxRetries minus one allowed (caller-managed retry)",
			mutate: func(c *Config) {
				c.Addr = "localhost:6379"
				c.MaxRetries = -1
			},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := &Config{}
			tc.mutate(c)
			err := c.Validate()
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), tc.wantErr)
		})
	}
}

// TestNew_Standalone_PingSuccess drives New() against a real miniredis to
// cover the standalone branch through Ping.
func TestNew_Standalone_PingSuccess(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)

	client, err := New(&Config{Addr: mr.Addr()})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	// Round-trip a command to be sure the returned client is usable.
	require.NoError(t, client.Ping(t.Context()).Err())
}

// TestNew_Standalone_PingFailure covers the ping-error branch: Validate
// passes (Addr is set), but the target refuses, so Ping fails fast.
func TestNew_Standalone_PingFailure(t *testing.T) {
	// Port 1 is reserved and refuses connections on every sane host.
	_, err := New(&Config{
		Addr:        "127.0.0.1:1",
		DialTimeout: time.Second, // >= 100ms floor enforced by Validate
	})
	require.Error(t, err)
	require.Contains(t, err.Error(), "redis ping")
}
