//go:build windows

package edge

import (
	"sync/atomic"
	"testing"
)

// HRESULT is signed 32-bit even on ARM64/AMD64. Non-nil dummy objects must
// never be dereferenced when the callback reports a failed HRESULT.
func TestInitializationRejectsFailedHRESULT(t *testing.T) {
	for _, hr := range []uintptr{0x80004005, 0x80070005} {
		e := &Chromium{}
		e.EnvironmentCompleted(hr, &ICoreWebView2Environment{})
		if atomic.LoadUintptr(&e.inited) != 2 {
			t.Fatal("environment failure did not terminate initialization")
		}
		e = &Chromium{}
		e.CreateCoreWebView2ControllerCompleted(hr, &ICoreWebView2Controller{})
		if atomic.LoadUintptr(&e.inited) != 2 {
			t.Fatal("controller failure did not terminate initialization")
		}
	}
}

func TestEvalRejectsEmbeddedNULWithoutTerminatingProcess(t *testing.T) {
	e := &Chromium{}
	e.Eval("invalid\x00script")
	e.Eval("valid script with no initialized webview")
}
