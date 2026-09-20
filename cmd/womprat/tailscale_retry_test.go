package main

import (
	"context"
	"errors"
	"strings"
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
	app.tsRetryStart = func(context.Context) error {
		calls.Add(1)
		return errNoTailscaleAuthKey
	}
	app.scheduleTailscaleRetry()
	waitRetryStopped(t, app)
	if calls.Load() != 1 {
		t.Fatalf("retry calls = %d, want 1", calls.Load())
	}
}

func TestShutdownStopsRetryWorker(t *testing.T) {
	app := newTestApp(t)
	entered := make(chan struct{})
	app.tsRetryStart = func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}
	app.scheduleTailscaleRetry()
	<-entered
	app.shutdown()
	app.tsRetryMu.Lock()
	running := app.tsRetrying
	app.tsRetryMu.Unlock()
	if running {
		t.Fatal("retry worker remains after shutdown")
	}
}

func TestUnconfiguredExitNodePreservesEffectiveTsnetRoute(t *testing.T) {
	app := newTestApp(t)
	app.exitNodeState = func(context.Context) (bool, error) { return true, nil }
	app.tsLastError = "old error"
	if err := app.refreshExitNodeActive(context.Background()); err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	active, lastError := app.exitNodeActive, app.tsLastError
	app.mu.Unlock()
	if !active || lastError != "" {
		t.Fatalf("preserved state: active=%v error=%q", active, lastError)
	}
}

func TestConfiguredExitNodeFailureIsExposedAndCleared(t *testing.T) {
	app := newTestApp(t)
	app.exitNodeApply = func(context.Context, string) error { return errors.New("route unavailable") }
	if err := app.applyConfiguredExitNode(context.Background(), "exit-a"); err == nil {
		t.Fatal("exit-node failure accepted")
	}
	app.mu.Lock()
	active, lastError := app.exitNodeActive, app.tsLastError
	app.mu.Unlock()
	if active || !strings.Contains(lastError, "route unavailable") {
		t.Fatalf("failure state: active=%v error=%q", active, lastError)
	}
	app.exitNodeApply = func(context.Context, string) error { return nil }
	if err := app.applyConfiguredExitNode(context.Background(), "exit-a"); err != nil {
		t.Fatal(err)
	}
	app.mu.Lock()
	active, lastError = app.exitNodeActive, app.tsLastError
	app.mu.Unlock()
	if !active || lastError != "" {
		t.Fatalf("success state: active=%v error=%q", active, lastError)
	}
}

func TestTailscaleReplacementClosesOldServerBeforeStart(t *testing.T) {
	// The concrete tsnet server cannot be replaced with a fake, so assert the
	// ordering contract directly and exercise retry scheduling separately.
	s := readFileForRegression(t, "main.go")
	closeAt := strings.Index(s, "_ = old.Close()")
	newAt := strings.Index(s, "ts := &tsnet.Server{")
	if closeAt < 0 || newAt < 0 || closeAt > newAt {
		t.Fatalf("old server must close before replacement starts: close=%d new=%d", closeAt, newAt)
	}
}

func TestTailscaleRetryRunsWhenOldServerStillExists(t *testing.T) {
	app := newTestApp(t)
	app.tsServer = &tsnet.Server{}
	var calls atomic.Int32
	app.tsRetryStart = func(context.Context) error {
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
	app.tsRetryStart = func(context.Context) error {
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

func TestTailscaleRetryStopCancelsInFlightStart(t *testing.T) {
	app := newTestApp(t)
	entered := make(chan struct{})
	app.tsRetryStart = func(ctx context.Context) error {
		close(entered)
		<-ctx.Done()
		return ctx.Err()
	}
	app.scheduleTailscaleRetry()
	<-entered
	start := time.Now()
	app.stopTailscaleRetry()
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("stop took %s", elapsed)
	}
}

func TestTailscaleRetryScheduleIsIdempotentAndStopWaits(t *testing.T) {
	app := newTestApp(t)
	entered := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	app.tsRetryInterval = time.Hour
	app.tsRetryStart = func(context.Context) error {
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
	running, cancel, done := app.tsRetrying, app.tsRetryCancel, app.tsRetryDone
	app.tsRetryMu.Unlock()
	if running || cancel != nil || done != nil {
		t.Fatalf("retry state not cleaned: running=%v cancel=%v done=%v", running, cancel != nil, done != nil)
	}
}
