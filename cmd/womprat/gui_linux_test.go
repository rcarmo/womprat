//go:build linux && webkitgui

package main

import (
	"runtime/cgo"
	"testing"
)

func TestLinuxDispatchStopsBatchAfterClose(t *testing.T) {
	m := &linuxContentManager{}
	m.handle = cgo.NewHandle(m)
	defer m.handle.Delete()
	called := 0
	m.queue = []func(){
		m.closeQueue,
		func() { called++ },
	}
	m.drainQueue()
	m.dispatch(func() { called++ })
	m.drainQueue()
	if called != 0 || !m.closed || len(m.queue) != 0 {
		t.Fatalf("work ran after close: called=%d closed=%v queued=%d", called, m.closed, len(m.queue))
	}
}
