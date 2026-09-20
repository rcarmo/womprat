package main

import (
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"tailscale.com/tsnet"
)

func waitRetryStopped(t *testing.T, app *App) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		app.tsRetryMu.Lock()
		running := app.tsRetrying
		app.tsRetryMu.Unlock()
		if !running {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("retry worker did not stop")
}

func TestTailscaleRetryStopsOnMissingAuthKey(t *testing.T) {
	app := newTestApp(t)
	var calls atomic.Int32
	app.tsRetryInterval = time.Millisecond
	app.tsRetryStart = func() error {
		calls.Add(1)
		return errNoTailscaleAuthKey
	}
	app.scheduleTailscaleRetry()
	waitRetryStopped(t, app)
	if calls.Load() != 1 {
		t.Fatalf("retry calls = %d, want 1", calls.Load())
	}
}

func TestTailscaleRetryRunsWhenOldServerStillExists(t *testing.T) {
	app := newTestApp(t)
	app.tsServer = &tsnet.Server{}
	var calls atomic.Int32
	app.tsRetryStart = func() error {
		calls.Add(1)
		return nil
	}
	app.scheduleTailscaleRetry()
	waitRetryStopped(t, app)
	if calls.Load() != 1 {
		t.Fatalf("replacement retry skipped because old server exists: calls=%d", calls.Load())
	}
}

func TestTailscaleRetryRetriesTransientFailureThenStopsOnSuccess(t *testing.T) {
	app := newTestApp(t)
	var calls atomic.Int32
	app.tsRetryInterval = time.Millisecond
	app.tsRetryStart = func() error {
		if calls.Add(1) < 3 {
			return errors.New("temporary failure")
		}
		return nil
	}
	app.scheduleTailscaleRetry()
	waitRetryStopped(t, app)
	if calls.Load() != 3 {
		t.Fatalf("retry calls = %d, want 3", calls.Load())
	}
}

func TestTailscaleRetryScheduleIsIdempotentAndStopWaits(t *testing.T) {
	app := newTestApp(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	app.tsRetryInterval = time.Hour
	app.tsRetryStart = func() error {
		if calls.Add(1) == 1 {
			close(entered)
		}
		<-release
		return errors.New("temporary failure")
	}
	app.scheduleTailscaleRetry()
	app.scheduleTailscaleRetry()
	<-entered
	close(release)
	app.stopTailscaleRetry()
	if calls.Load() != 1 {
		t.Fatalf("duplicate retry workers started: calls=%d", calls.Load())
	}
	app.tsRetryMu.Lock()
	running, stop, done := app.tsRetrying, app.tsRetryStop, app.tsRetryDone
	app.tsRetryMu.Unlock()
	if running || stop != nil || done != nil {
		t.Fatalf("retry state not cleaned: running=%v stop=%v done=%v", running, stop != nil, done != nil)
	}
}
