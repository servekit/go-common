package cronx

import (
	"testing"
	"time"

	"github.com/robfig/cron/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNew_DefaultConfig(t *testing.T) {
	c, err := New(&Config{})
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, time.Local, c.Location())
}

func TestNew_WithTimezone(t *testing.T) {
	c, err := New(&Config{
		Timezone: "Asia/Shanghai",
	})
	require.NoError(t, err)
	require.NotNil(t, c)

	loc, _ := time.LoadLocation("Asia/Shanghai")
	assert.Equal(t, loc, c.Location())
}

func TestNew_InvalidTimezone(t *testing.T) {
	c, err := New(&Config{
		Timezone: "Invalid/Zone",
	})
	assert.Nil(t, c)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "cronx")
}

func TestNew_WithSeconds(t *testing.T) {
	c, err := New(&Config{
		WithSeconds: true,
	})
	require.NoError(t, err)

	_, err = c.AddFunc("*/1 * * * * *", func() {})
	assert.NoError(t, err)
}

func TestNew_WithoutSeconds_RejectsSixFields(t *testing.T) {
	c, err := New(&Config{})
	require.NoError(t, err)

	_, err = c.AddFunc("*/1 * * * * *", func() {})
	assert.Error(t, err)
}

func TestNew_PanicRecover_DefaultEnabled(t *testing.T) {
	c, err := New(&Config{WithSeconds: true})
	require.NoError(t, err)

	// A panicking job should be recovered and the scheduler keeps running.
	// Verify by registering a second job that signals completion.
	fired := make(chan struct{}, 1)
	_, err = c.AddFunc("* * * * * *", func() {
		panic("test panic")
	})
	require.NoError(t, err)

	_, err = c.AddFunc("* * * * * *", func() {
		select {
		case fired <- struct{}{}:
		default:
		}
	})
	require.NoError(t, err)

	c.Start()
	defer c.Stop()

	select {
	case <-fired:
		// second job fired — scheduler survived the panic
	case <-time.After(2 * time.Second):
		t.Fatal("expected scheduler to survive panic and fire subsequent job")
	}
}

func TestNew_PanicRecover_Disabled(t *testing.T) {
	c, err := New(&Config{
		DisableRecovery: true,
	})
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNew_OverlapSkip(t *testing.T) {
	c, err := New(&Config{
		OverlapPolicy: "skip",
	})
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNew_OverlapDelay(t *testing.T) {
	c, err := New(&Config{
		OverlapPolicy: "delay",
	})
	require.NoError(t, err)
	require.NotNil(t, c)
}

func TestNew_WithCronOption(t *testing.T) {
	loc, _ := time.LoadLocation("UTC")
	c, err := New(&Config{},
		WithCronOption(cron.WithLocation(loc)),
	)
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, loc, c.Location())
}

func TestNew_NilConfig(t *testing.T) {
	c, err := New(nil)
	require.NoError(t, err)
	require.NotNil(t, c)
	assert.Equal(t, time.Local, c.Location())
}

func TestNew_Integration_FiresCallback(t *testing.T) {
	c, err := New(&Config{
		WithSeconds: true,
	})
	require.NoError(t, err)

	fired := make(chan struct{}, 1)
	_, err = c.AddFunc("* * * * * *", func() {
		select {
		case fired <- struct{}{}:
		default:
		}
	})
	require.NoError(t, err)

	c.Start()
	defer c.Stop()

	select {
	case <-fired:
	case <-time.After(2 * time.Second):
		t.Fatal("expected cron job to fire within 2 seconds")
	}
}
