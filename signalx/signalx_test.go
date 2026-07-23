package signalx

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"os/signal"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/servekit/go-common/lifecycle"
)

// fakeService is a configurable Service for testing Run.
// Each field is optional; nil fields are no-ops.
type fakeService struct {
	startFn func() error
	stopFn  func() error
}

func (f *fakeService) Start() error {
	if f.startFn != nil {
		return f.startFn()
	}
	return nil
}

func (f *fakeService) Stop() error {
	if f.stopFn != nil {
		return f.stopFn()
	}
	return nil
}

// registerSafetyNet installs a no-op handler for sig so the OS default action
// (terminate) cannot fire during the race window before Run installs its own
// handler. Returns a stop function that unregisters the handler. Always
// defer-call the stop function.
func registerSafetyNet(sig os.Signal) func() {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, sig)
	return func() { signal.Stop(ch) }
}

// pollSendSignal sends sig to the current process every 10ms until stop is
// closed. The caller must have a safety net for sig registered.
func pollSendSignal(sig syscall.Signal, stop <-chan struct{}) {
	for {
		select {
		case <-stop:
			return
		default:
		}
		_ = syscall.Kill(syscall.Getpid(), sig)
		time.Sleep(10 * time.Millisecond)
	}
}

// await waits for ch up to 1s; fails the test on timeout. Helper for tests
// that need to confirm a side-effect happened within Run.
func await(t *testing.T, name string, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("%s never happened within 1s", name)
	}
}

func TestDefaultSignals_Value(t *testing.T) {
	require.Equal(t, []os.Signal{syscall.SIGINT, syscall.SIGTERM}, DefaultSignals)
}

func TestRun_StartsService_BeforeBlocking(t *testing.T) {
	release := registerSafetyNet(syscall.SIGTERM)
	defer release()

	started := make(chan struct{})
	s := &fakeService{
		startFn: func() error { close(started); return nil },
	}

	done := make(chan error, 1)
	go func() { done <- Run(s) }()
	await(t, "Start", started)

	// Unblock Run so the test goroutine can exit cleanly.
	stop := make(chan struct{})
	defer close(stop)
	go pollSendSignal(syscall.SIGTERM, stop)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s of signal")
	}
}

func TestRun_StopCalled_AfterSignal(t *testing.T) {
	release := registerSafetyNet(syscall.SIGTERM)
	defer release()

	stopped := make(chan struct{})
	s := &fakeService{
		stopFn: func() error { close(stopped); return nil },
	}

	done := make(chan error, 1)
	go func() { done <- Run(s) }()

	stop := make(chan struct{})
	defer close(stop)
	go pollSendSignal(syscall.SIGTERM, stop)

	await(t, "Stop after SIGTERM", stopped)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s of Stop")
	}
}

func TestRun_ReturnsStopError(t *testing.T) {
	release := registerSafetyNet(syscall.SIGTERM)
	defer release()

	want := errors.New("boom")
	s := &fakeService{
		stopFn: func() error { return want },
	}

	done := make(chan error, 1)
	go func() { done <- Run(s) }()

	stop := make(chan struct{})
	defer close(stop)
	go pollSendSignal(syscall.SIGTERM, stop)

	select {
	case err := <-done:
		require.ErrorIs(t, err, want)
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s of signal")
	}
}

func TestRun_DefaultSignals_WhenEmpty(t *testing.T) {
	// Default signals are SIGINT/SIGTERM. Send SIGTERM and verify Stop runs.
	release := registerSafetyNet(syscall.SIGTERM)
	defer release()

	stopped := make(chan struct{})
	s := &fakeService{
		stopFn: func() error { close(stopped); return nil },
	}

	done := make(chan error, 1)
	go func() { done <- Run(s) }() // no sigs arg — uses defaults

	stop := make(chan struct{})
	defer close(stop)
	go pollSendSignal(syscall.SIGTERM, stop)

	await(t, "Stop after default SIGTERM", stopped)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s of default SIGTERM")
	}
}

func TestRun_CustomSignals(t *testing.T) {
	// Register safety nets for both signals so the OS default action cannot
	// fire for either during the race window.
	releaseHup := registerSafetyNet(syscall.SIGHUP)
	defer releaseHup()
	releaseTerm := registerSafetyNet(syscall.SIGTERM)
	defer releaseTerm()

	stopped := make(chan struct{})
	s := &fakeService{
		stopFn: func() error { close(stopped); return nil },
	}

	done := make(chan error, 1)
	go func() { done <- Run(s, syscall.SIGHUP) }()

	// Send SIGTERM first: Run registered only SIGHUP, so SIGTERM should be
	// absorbed by the safety net and Run must NOT return.
	stopTerm := make(chan struct{})
	defer close(stopTerm)
	go pollSendSignal(syscall.SIGTERM, stopTerm)

	select {
	case <-stopped:
		t.Fatal("Stop invoked by SIGTERM, but only SIGHUP was registered")
	case <-time.After(150 * time.Millisecond):
		// Expected: SIGTERM does not trigger shutdown.
	}

	// Now send SIGHUP — Run should stop the service.
	stopHup := make(chan struct{})
	defer close(stopHup)
	go pollSendSignal(syscall.SIGHUP, stopHup)

	await(t, "Stop after SIGHUP", stopped)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s of SIGHUP")
	}
}

func TestRun_StartPanic_Propagates(t *testing.T) {
	want := "init failed"
	s := &fakeService{
		startFn: func() error { panic(want) },
	}

	require.PanicsWithValue(t, want, func() {
		_ = Run(s)
	})
}

func TestRun_StartError_PanicsWithWrap(t *testing.T) {
	startErr := errors.New("init failed")
	s := &fakeService{
		startFn: func() error { return startErr },
	}

	require.PanicsWithValue(t, "signalx: start failed: init failed", func() {
		_ = Run(s)
	})
}

func TestRun_AcceptsManager(t *testing.T) {
	// *lifecycle.Manager implements Start() error and Stop() error, which
	// satisfies the signalx.Service interface implicitly.
	release := registerSafetyNet(syscall.SIGTERM)
	defer release()

	stopped := make(chan struct{})
	m := &lifecycle.Manager{}
	m.Add("fake", &fakeService{
		stopFn: func() error { close(stopped); return nil },
	})

	done := make(chan error, 1)
	go func() { done <- Run(m) }()

	stop := make(chan struct{})
	defer close(stop)
	go pollSendSignal(syscall.SIGTERM, stop)

	await(t, "Stop after SIGTERM (via Manager)", stopped)

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(time.Second):
		t.Fatal("Run did not return within 1s of Stop")
	}
}

func TestRunWithForceQuit_StopsNormallyOnFirstSignal(t *testing.T) {
	release := registerSafetyNet(syscall.SIGTERM)
	defer release()

	stopped := make(chan struct{})
	s := &fakeService{
		stopFn: func() error { close(stopped); return nil },
	}

	go func() { _ = RunWithForceQuit(s) }()

	stop := make(chan struct{})
	defer close(stop)
	go pollSendSignal(syscall.SIGTERM, stop)

	await(t, "Stop", stopped)
}

func TestRunWithForceQuit_StartError_PanicsWithWrap(t *testing.T) {
	startErr := errors.New("init failed")
	s := &fakeService{
		startFn: func() error { return startErr },
	}

	require.PanicsWithValue(t, "signalx: start failed: init failed", func() {
		_ = RunWithForceQuit(s)
	})
}

func TestRunWithForceQuit_DefaultSignals_WhenEmpty(t *testing.T) {
	release := registerSafetyNet(syscall.SIGTERM)
	defer release()

	stopped := make(chan struct{})
	s := &fakeService{
		stopFn: func() error { close(stopped); return nil },
	}

	go func() { _ = RunWithForceQuit(s) }()

	stop := make(chan struct{})
	defer close(stop)
	go pollSendSignal(syscall.SIGTERM, stop)

	await(t, "Stop", stopped)
}

// TestRunWithForceQuit_SecondSignalSendsSIGKILL verifies the core contract
// of RunWithForceQuit: a second signal during Stop terminates the process
// via SIGKILL.
//
// Cannot run in-process — the test process would be killed. Parent mode
// spawns a child via os/exec with env SHUTDOWN_RELIABILITY_SUBPROCESS=1.
// Child mode constructs a lifecycle.Manager whose only Service blocks
// forever in Stop, then calls RunWithForceQuit. Parent sends SIGTERM
// twice and asserts the child's WaitStatus shows SIGKILL.
func TestRunWithForceQuit_SecondSignalSendsSIGKILL(t *testing.T) {
	if os.Getenv("SHUTDOWN_RELIABILITY_SUBPROCESS") == "1" {
		// Child mode: enter RunWithForceQuit with a blocking Stop.
		m := lifecycle.NewManager()
		m.Add("blocked", lifecycle.StopFunc(func() {
			select {} // block forever
		}))
		_ = RunWithForceQuit(m)
		return
	}

	// Parent mode: spawn a fresh child running this same test.
	cmd := exec.Command(os.Args[0],
		"-test.run=^TestRunWithForceQuit_SecondSignalSendsSIGKILL$",
		"-test.v")
	cmd.Env = append(os.Environ(), "SHUTDOWN_RELIABILITY_SUBPROCESS=1")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())

	// Give the child time to reach the select{} in Stop.
	// 500ms is generous; signal.Notify + Start + first signal dispatch
	// + goroutine entry to Stop is well under 100ms in practice.
	time.Sleep(500 * time.Millisecond)

	// First signal → triggers Stop (which blocks forever).
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))
	// Second signal a moment later → triggers SIGKILL.
	time.Sleep(100 * time.Millisecond)
	require.NoError(t, cmd.Process.Signal(syscall.SIGTERM))

	err := cmd.Wait()
	require.Error(t, err, "child should have been killed; stdout=%s stderr=%s",
		stdout.String(), stderr.String())

	exitErr, ok := err.(*exec.ExitError)
	require.True(t, ok, "expected *exec.ExitError, got %T: %v", err, err)

	ws, ok := exitErr.Sys().(syscall.WaitStatus)
	require.True(t, ok, "expected syscall.WaitStatus inside ExitError")
	require.True(t, ws.Signaled(),
		"expected child terminated by signal; exit status %d, stdout=%s stderr=%s",
		ws.ExitStatus(), stdout.String(), stderr.String())
	require.Equal(t, syscall.SIGKILL, ws.Signal(),
		"expected SIGKILL, got %v; stdout=%s stderr=%s",
		ws.Signal(), stdout.String(), stderr.String())
}
