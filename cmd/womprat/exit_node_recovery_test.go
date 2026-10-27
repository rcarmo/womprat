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

func waitExitRecovery(t *testing.T, app *App) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		app.exitRetryMu.Lock()
		done := app.exitRetryDone
		app.exitRetryMu.Unlock()
		if done == nil {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("exit-node recovery did not stop")
}
func TestExitNodeRecoveryReappliesWithoutRestartingServer(t *testing.T) {
	app := newTestApp(t)
	app.config.ExitNode = "relay"
	server := &tsnet.Server{}
	app.tsServer = server
	app.exitRetryInterval = time.Millisecond
	var calls atomic.Int32
	app.exitNodeApply = func(context.Context, string) error {
		if calls.Add(1) == 1 {
			return errors.New("peer snapshot not ready")
		}
		return nil
	}
	app.scheduleExitNodeRecovery()
	app.scheduleExitNodeRecovery()
	waitExitRecovery(t, app)
	if calls.Load() != 2 || app.tsServer != server || !app.exitNodeActive {
		t.Fatal("route retry failed or restarted backend", calls.Load())
	}
	if app.tsLastError != "" {
		t.Fatal("success did not clear route error")
	}
}
func TestExitNodeRecoveryHonorsExplicitClearAndCancellation(t *testing.T) {
	app := newTestApp(t)
	app.config.ExitNode = "relay"
	app.tsServer = &tsnet.Server{}
	app.exitRetryInterval = time.Millisecond
	var calls atomic.Int32
	app.exitNodeApply = func(context.Context, string) error { calls.Add(1); return nil }
	app.configSaveMu.Lock()
	app.scheduleExitNodeRecovery()
	app.mu.Lock()
	app.config.ExitNode = ""
	app.mu.Unlock()
	app.configSaveMu.Unlock()
	waitExitRecovery(t, app)
	if calls.Load() != 0 {
		t.Fatal("old selection restored after clear")
	}
	app.config.ExitNode = "relay"
	entered := make(chan struct{})
	app.exitNodeApply = func(ctx context.Context, _ string) error { close(entered); <-ctx.Done(); return ctx.Err() }
	app.scheduleExitNodeRecovery()
	<-entered
	app.stopExitNodeRecovery()
	// Stopping while a settings handler owns the lock must not deadlock a
	// worker waiting to start another attempt.
	app.configSaveMu.Lock()
	app.scheduleExitNodeRecovery()
	app.stopExitNodeRecovery()
	app.configSaveMu.Unlock()
}
func TestStartupFailureSchedulesRouteRecovery(t *testing.T) {
	source := readFileForRegression(t, "main.go")
	start := strings.Index(source, "if err := a.applyConfiguredExitNode(parent, exitNode)")
	if start < 0 {
		t.Fatal("startup route application missing")
	}
	tail := source[start:]
	end := strings.Index(tail, "a.logTSNetRouting()")
	if end < 0 || !strings.Contains(tail[:end], "a.scheduleExitNodeRecovery()") {
		t.Fatal("startup error still abandons saved route")
	}
}
