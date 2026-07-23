package lifecycle

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// fakeComponent records Start/Stop invocations for Manager tests.
type fakeComponent struct {
	name     string
	startErr error
	stopErr  error
	startWg  *sync.WaitGroup // optional: Done() on Start
	stopWg   *sync.WaitGroup // optional: Done() on Stop
	startFn  func() error    // optional: custom Start logic
	stopFn   func() error    // optional: custom Stop logic
}

func (f *fakeComponent) Start() error {
	if f.startWg != nil {
		f.startWg.Done()
	}
	if f.startFn != nil {
		return f.startFn()
	}
	return f.startErr
}

func (f *fakeComponent) Stop() error {
	if f.stopWg != nil {
		f.stopWg.Done()
	}
	if f.stopFn != nil {
		return f.stopFn()
	}
	return f.stopErr
}

// waitGroupDone waits for wg up to 1s; fails the test on timeout.
func waitGroupDone(t *testing.T, wg *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("WaitGroup never done within 1s")
	}
}

func TestManager_AddService(t *testing.T) {
	m := &Manager{}
	m.Add("svc", &fakeComponent{name: "svc"})
	require.Len(t, m.entries, 1)
	require.Equal(t, "svc", m.entries[0].name)
}

func TestManager_AddStarter_WrapsAsService(t *testing.T) {
	m := &Manager{}
	m.AddStarter("starter-only", stubStarter{})
	require.Len(t, m.entries, 1)
	require.NoError(t, m.entries[0].svc.Stop()) // wrapped Stop is a no-op
}

func TestManager_AddStopper_WrapsAsService(t *testing.T) {
	m := &Manager{}
	m.AddStopper("stopper-only", stubStopper{})
	require.Len(t, m.entries, 1)
	require.NoError(t, m.entries[0].svc.Start()) // wrapped Start is a no-op
}

func TestManager_Start_SequentialTrigger(t *testing.T) {
	m := &Manager{}
	var wg sync.WaitGroup
	wg.Add(3)
	for _, name := range []string{"a", "b", "c"} {
		m.Add(name, &fakeComponent{name: name, startWg: &wg})
	}
	require.NoError(t, m.Start())
	waitGroupDone(t, &wg) // confirms all 3 starters were invoked
}

func TestManager_Start_SequentialOrder(t *testing.T) {
	var order []string
	m := &Manager{}
	for _, name := range []string{"first", "second", "third"} {
		n := name // capture
		m.Add(name, &fakeComponent{
			name:    name,
			startWg: nil,
			startFn: func() error { order = append(order, n); return nil },
		})
	}
	require.NoError(t, m.Start())
	require.Equal(t, []string{"first", "second", "third"}, order)
}

func TestManager_Start_FailStopsOnFirstError(t *testing.T) {
	var started []string
	m := &Manager{}
	m.Add("a", &fakeComponent{
		name:    "a",
		startFn: func() error { started = append(started, "a"); return nil },
	})
	m.Add("b", &fakeComponent{
		name:     "b",
		startErr: assertErr("b-failed"),
		startFn:  func() error { started = append(started, "b"); return assertErr("b-failed") },
	})
	m.Add("c", &fakeComponent{
		name:    "c",
		startFn: func() error { started = append(started, "c"); return nil },
	})

	err := m.Start()
	require.Error(t, err)
	require.Contains(t, err.Error(), "b: b-failed")
	require.Equal(t, []string{"a", "b"}, started) // c never started
}

func TestManager_Start_OnlyOnce(t *testing.T) {
	m := &Manager{}
	var wg sync.WaitGroup
	wg.Add(1)
	m.Add("once", &fakeComponent{name: "once", startWg: &wg})

	require.NoError(t, m.Start())
	waitGroupDone(t, &wg)

	// Second Start must be a no-op. If sync.Once failed, the starter would
	// run again and call wg.Done() a second time, panicking on a negative
	// WaitGroup counter.
	require.NotPanics(t, func() { m.Start() })
}

func TestManager_Stop_ConcurrentTrigger(t *testing.T) {
	m := &Manager{}
	var wg sync.WaitGroup
	wg.Add(3)
	for _, name := range []string{"a", "b", "c"} {
		m.Add(name, &fakeComponent{name: name, stopWg: &wg})
	}

	done := make(chan error, 1)
	go func() { done <- m.Stop() }()
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Stop did not trigger all stoppers within 1s")
	}
}

func TestManager_Stop_JoinsErrors(t *testing.T) {
	m := &Manager{}
	m.Add("a", &fakeComponent{name: "a", stopErr: assertErr("a-failed")})
	m.Add("b", &fakeComponent{name: "b", stopErr: assertErr("b-failed")})

	err := m.Stop()
	require.Error(t, err)
	require.Contains(t, err.Error(), "a: a-failed")
	require.Contains(t, err.Error(), "b: b-failed")
}

func TestManager_Stop_OnlyOnce_ReturnsSameError(t *testing.T) {
	m := &Manager{}
	m.Add("a", &fakeComponent{name: "a", stopErr: assertErr("boom")})

	err1 := m.Stop()
	err2 := m.Stop()
	require.Same(t, err1, err2) // same instance captured by stopOnce
	require.Contains(t, err1.Error(), "boom")
}

func TestManager_Stop_EmptyManager(t *testing.T) {
	m := &Manager{}
	require.NoError(t, m.Stop())
}

func TestManager_Run_BlocksUntilContextCancelThenStops(t *testing.T) {
	m := &Manager{}
	var startWg, stopWg sync.WaitGroup
	startWg.Add(1)
	stopWg.Add(1)
	m.Add("a", &fakeComponent{name: "a", startWg: &startWg, stopWg: &stopWg})

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- m.Run(ctx) }()

	waitGroupDone(t, &startWg) // Start triggered

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not return after context cancel")
	}

	waitGroupDone(t, &stopWg) // Stop triggered
}

func TestNewManager_DefaultStopTimeout(t *testing.T) {
	m := NewManager()
	require.Equal(t, 10*time.Second, m.stopTimeout)
}

func TestWithStopTimeout_Applies(t *testing.T) {
	m := NewManager(WithStopTimeout(5 * time.Second))
	require.Equal(t, 5*time.Second, m.stopTimeout)
}

func TestWithStopTimeout_ZeroDisables(t *testing.T) {
	m := NewManager(WithStopTimeout(0))
	require.Equal(t, time.Duration(0), m.stopTimeout)
}

func TestManager_Stop_TimesOut(t *testing.T) {
	m := NewManager(WithStopTimeout(50 * time.Millisecond))
	m.Add("slow", &fakeComponent{
		name:   "slow",
		stopFn: func() error { time.Sleep(time.Second); return nil },
	})

	start := time.Now()
	err := m.Stop()
	elapsed := time.Since(start)

	require.Error(t, err)
	require.Contains(t, err.Error(), "timeout after")
	require.Less(t, elapsed, 500*time.Millisecond, "Stop should return shortly after timeout, not wait for the slow service")
}

func TestManager_Stop_NoTimeoutWhenZero(t *testing.T) {
	m := &Manager{} // zero-value, stopTimeout == 0
	done := make(chan struct{})
	m.Add("slow", &fakeComponent{
		name:   "slow",
		stopFn: func() error { time.Sleep(100 * time.Millisecond); close(done); return nil },
	})

	err := m.Stop()
	require.NoError(t, err)
	select {
	case <-done:
		// Stop waited for the slow service to finish — no timeout.
	default:
		t.Fatal("Stop returned before slow service finished; zero-value timeout was not respected")
	}
}

func TestManager_Stop_AggregatesErrorsAndTimeout(t *testing.T) {
	m := NewManager(WithStopTimeout(50 * time.Millisecond))
	m.Add("fast-err", &fakeComponent{
		name:    "fast-err",
		stopErr: assertErr("fast failure"),
	})
	m.Add("slow", &fakeComponent{
		name:   "slow",
		stopFn: func() error { time.Sleep(time.Second); return nil },
	})

	err := m.Stop()
	require.Error(t, err)
	require.Contains(t, err.Error(), "fast-err: fast failure")
	require.Contains(t, err.Error(), "timeout after")
}

func TestManager_Stop_NoTimeoutWhenAllFast(t *testing.T) {
	m := NewManager(WithStopTimeout(time.Second))
	m.Add("a", &fakeComponent{name: "a"})
	m.Add("b", &fakeComponent{name: "b"})

	err := m.Stop()
	require.NoError(t, err)
}
