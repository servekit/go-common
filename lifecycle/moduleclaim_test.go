package lifecycle

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestModuleClaim_SecondClaimFails(t *testing.T) {
	var m ModuleClaim
	require.NoError(t, m.Claim("test-svc"))

	err := m.Claim("test-svc")
	require.ErrorContains(t, err, "test-svc")
	require.ErrorContains(t, err, "already active")
}

func TestModuleClaim_ReleaseAllowsReclaim(t *testing.T) {
	var m ModuleClaim
	require.NoError(t, m.Claim("test-svc"))
	m.Release()
	require.NoError(t, m.Claim("test-svc"))
}

func TestModuleClaim_WrapReleasesOnStop(t *testing.T) {
	var m ModuleClaim
	require.NoError(t, m.Claim("test-svc"))

	svc := &fakeLifecycleService{}
	wrapped := m.Wrap(svc)

	// Still claimed while the instance runs.
	require.Error(t, m.Claim("test-svc"))

	require.NoError(t, wrapped.Stop())
	require.True(t, svc.stopped, "Wrap must delegate Stop to the service")
	require.NoError(t, m.Claim("test-svc"), "Stop must release the claim")
}

func TestModuleClaim_WrapReleasesEvenWhenStopFails(t *testing.T) {
	var m ModuleClaim
	require.NoError(t, m.Claim("test-svc"))

	wrapped := m.Wrap(&failingStopService{})
	require.Error(t, wrapped.Stop())
	require.NoError(t, m.Claim("test-svc"), "a failed Stop must still free the slot")
}

type fakeLifecycleService struct {
	started bool
	stopped bool
}

func (f *fakeLifecycleService) Start() error { f.started = true; return nil }
func (f *fakeLifecycleService) Stop() error  { f.stopped = true; return nil }

type failingStopService struct{}

func (failingStopService) Start() error { return nil }
func (failingStopService) Stop() error  { return assertErr("stop failed") }
