package main

import (
	"context"
	"log"
	"time"
)

// A connected backend with a failed saved route must not be restarted for every
// retry: reapply only the saved choice against its current peer snapshot.
func (a *App) scheduleExitNodeRecovery() {
	a.exitRetryMu.Lock()
	if a.exitRetryDone != nil {
		a.exitRetryMu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	a.exitRetryCancel = cancel
	a.exitRetryDone = done
	interval := a.exitRetryInterval
	if interval <= 0 {
		interval = tailscaleRetryInterval
	}
	a.exitRetryMu.Unlock()
	go func() {
		defer func() {
			a.exitRetryMu.Lock()
			a.exitRetryCancel = nil
			a.exitRetryDone = nil
			close(done)
			a.exitRetryMu.Unlock()
		}()
		for {
			timer := time.NewTimer(interval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			// Serialize with settings saves so an old retry can never overwrite
			// a newer selection or an explicit clear.
			// Never wait on this mutex while a settings handler is stopping us.
			if !a.configSaveMu.TryLock() {
				continue
			}
			a.mu.Lock()
			name := a.config.ExitNode
			server := a.tsServer
			locked := a.locked
			a.mu.Unlock()
			if ctx.Err() != nil || name == "" || server == nil || locked {
				a.configSaveMu.Unlock()
				return
			}
			err := a.applyConfiguredExitNode(ctx, name)
			a.configSaveMu.Unlock()
			if err == nil {
				log.Printf("tsnet: restored saved exit node %q", name)
				return
			}
			log.Printf("tsnet: saved exit node %q still unavailable: %v", name, err)
		}
	}()
}

func (a *App) stopExitNodeRecovery() {
	a.exitRetryMu.Lock()
	cancel, done := a.exitRetryCancel, a.exitRetryDone
	if cancel != nil {
		cancel()
	}
	a.exitRetryMu.Unlock()
	if done != nil {
		<-done
	}
}
