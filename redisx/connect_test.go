package redisx

import (
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/stretchr/testify/require"

	"github.com/servekit/go-common/lifecycle"
)

func TestConnect_InjectedReturnsAsIs(t *testing.T) {
	mgr := lifecycle.NewManager()
	injected := NewTestClient(t)

	rdb, err := Connect(nil, injected, mgr)

	require.NoError(t, err)
	require.Same(t, injected, rdb)
	require.NoError(t, mgr.Stop())
	// The caller owns the injected client: mgr.Stop must not have closed it.
	require.NoError(t, injected.Ping(t.Context()).Err())
}

func TestConnect_NilCfgWithoutInjectionFails(t *testing.T) {
	_, err := Connect(nil, nil, lifecycle.NewManager())

	require.ErrorContains(t, err, "config required")
}

func TestConnect_BuildsAndRegistersStopper(t *testing.T) {
	mr, err := miniredis.Run()
	require.NoError(t, err)
	t.Cleanup(mr.Close)
	mgr := lifecycle.NewManager()

	rdb, err := Connect(&Config{Addr: mr.Addr()}, nil, mgr)

	require.NoError(t, err)
	require.NoError(t, rdb.Ping(t.Context()).Err())
	require.NoError(t, mgr.Stop())
	// mgr.Stop closed the self-built client: further use must fail.
	require.Error(t, rdb.Ping(t.Context()).Err())
}
