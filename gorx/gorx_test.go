package gorx

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestGoSafe(t *testing.T) {
	var ran atomic.Bool
	done := make(chan struct{})
	GoSafe(func() {
		ran.Store(true)
		close(done)
	})
	<-done
	assert.True(t, ran.Load())
}

func TestGoSafe_RecoverPanic(t *testing.T) {
	var panicked atomic.Bool
	orig := OnPanic
	OnPanic = func(r any, _ []byte) { panicked.Store(true) }
	t.Cleanup(func() { OnPanic = orig })

	GoSafe(func() { panic("boom") })

	assert.Eventually(t, panicked.Load, time.Second, 10*time.Millisecond)
}

func TestRoutineGroup_Run(t *testing.T) {
	g := NewRoutineGroup()
	var count atomic.Int32

	for i := range 100 {
		g.Run(func() {
			count.Add(1)
			_ = i
		})
	}
	g.Wait()

	assert.Equal(t, int32(100), count.Load())
}

func TestRoutineGroup_RunSafe(t *testing.T) {
	var panicked atomic.Int32
	orig := OnPanic
	OnPanic = func(r any, _ []byte) { panicked.Add(1) }
	t.Cleanup(func() { OnPanic = orig })

	g := NewRoutineGroup()
	var count atomic.Int32

	for i := range 10 {
		g.RunSafe(func() {
			count.Add(1)
			if i%3 == 0 {
				panic("boom")
			}
		})
	}
	g.Wait()

	assert.Equal(t, int32(10), count.Load())
	assert.Equal(t, int32(4), panicked.Load()) // i=0,3,6,9
}

func TestWorkGroup_Run(t *testing.T) {
	var count atomic.Int32

	w := NewWorkGroup(func() {
		count.Add(1)
	}, 50)
	w.Run()

	assert.Equal(t, int32(50), count.Load())
}

func TestTaskRunner_Concurrency(t *testing.T) {
	const limit = 3
	runner := NewTaskRunner(limit)

	var (
		maxRunning atomic.Int32
		current    atomic.Int32
		mu         sync.Mutex
	)

	for range 20 {
		runner.Schedule(func() {
			cur := current.Add(1)
			mu.Lock()
			if cur > maxRunning.Load() {
				maxRunning.Store(cur)
			}
			mu.Unlock()

			time.Sleep(10 * time.Millisecond)

			current.Add(-1)
		})
	}
	runner.Wait()

	assert.Equal(t, int32(limit), maxRunning.Load())
}

func TestTaskRunner_ScheduleAfterWait(t *testing.T) {
	runner := NewTaskRunner(5)

	var count atomic.Int32
	runner.Schedule(func() { count.Add(1) })
	runner.Wait()

	runner.Schedule(func() { count.Add(1) })
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, int32(1), count.Load(), "Schedule after Wait should be no-op")
}

func TestTaskRunner_DoubleWait(t *testing.T) {
	runner := NewTaskRunner(5)

	runner.Schedule(func() { time.Sleep(10 * time.Millisecond) })
	runner.Wait()
	runner.Wait() // should not block or panic
}
