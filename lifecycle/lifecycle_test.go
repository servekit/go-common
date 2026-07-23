package lifecycle

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// errString is a simple error type shared across lifecycle tests.
type errString string

func (e errString) Error() string { return string(e) }

func assertErr(s string) error { return errString(s) }

// stubStarter is a no-op Starter for wrapper tests.
type stubStarter struct{}

func (stubStarter) Start() error { return nil }

// stubStopper is a no-op Stopper for wrapper tests.
type stubStopper struct{}

func (stubStopper) Stop() error { return nil }

func TestStartFunc_SatisfiesService(t *testing.T) {
	var _ Service = StartFunc(func() error { return nil })
}

func TestStopFunc_SatisfiesService(t *testing.T) {
	var _ Service = StopFunc(func() {})
}

func TestStarterOnly_SatisfiesService(t *testing.T) {
	var _ Service = starterOnly{stubStarter{}}
}

func TestStopperOnly_SatisfiesService(t *testing.T) {
	var _ Service = stopperOnly{stubStopper{}}
}

func TestStartFunc_StartInvokesFunction(t *testing.T) {
	called := false
	fn := StartFunc(func() error {
		called = true
		return nil
	})
	require.NoError(t, fn.Start())
	require.True(t, called)
}

func TestStartFunc_Stop_IsNoOp(t *testing.T) {
	require.NoError(t, StartFunc(func() error { return nil }).Stop())
}

func TestStartFunc_StartPropagatesError(t *testing.T) {
	fn := StartFunc(func() error { return assertErr("boom") })
	require.EqualError(t, fn.Start(), "boom")
}

func TestStopFunc_StopInvokesFunction(t *testing.T) {
	called := false
	fn := StopFunc(func() { called = true })
	require.NoError(t, fn.Stop())
	require.True(t, called)
}

func TestStopFunc_Start_IsNoOp(t *testing.T) {
	require.NoError(t, StopFunc(func() {}).Start())
}
