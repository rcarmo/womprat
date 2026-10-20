//go:build windows

package edge

import (
	"runtime"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func TestNavigationDiagnosticsTracksOnlyMainDocumentAndCleansUp(t *testing.T) {
	handlers := map[string]*certificateEventHandler{}
	completions := map[string]*certificateEventHandler{}
	type eventArgs struct {
		vtbl *struct {
			_IUnknownVtbl
			GetJSON ComProc
		}
	}
	var receivers []*devToolsEventReceiver
	wv := &ICoreWebView2{vtbl: &iCoreWebView2Vtbl{}}
	var start *certificateEventHandler
	wv.vtbl.AddNavigationStarting = NewComProc(func(this, h, out uintptr) uintptr {
		start = (*certificateEventHandler)(unsafe.Pointer(h))
		start.addRef()
		(*_EventRegistrationToken)(unsafe.Pointer(out)).Value = 8
		return 0
	})
	removed := 0
	wv.vtbl.RemoveNavigationStarting = NewComProc(func(this, token uintptr) uintptr {
		if token != 8 {
			t.Error("start token mismatch")
		}
		start.release()
		removed++
		return 0
	})
	wv.vtbl.GetDevToolsProtocolEventReceiver = NewComProc(func(this, name, out uintptr) uintptr {
		event := windows.UTF16PtrToString((*uint16)(unsafe.Pointer(name)))
		r := &devToolsEventReceiver{vtbl: &devToolsEventReceiverVtbl{}}
		r.vtbl.AddEvent = NewComProc(func(this, h, token uintptr) uintptr {
			handler := (*certificateEventHandler)(unsafe.Pointer(h))
			handler.addRef()
			handlers[event] = handler
			(*_EventRegistrationToken)(unsafe.Pointer(token)).Value = 31
			return 0
		})
		r.vtbl.RemoveEvent = NewComProc(func(this, token uintptr) uintptr {
			if token != 31 {
				t.Error("event token mismatch")
			}
			handlers[event].release()
			removed++
			return 0
		})
		r.vtbl.Release = NewComProc(func(this uintptr) uintptr { return 0 })
		receivers = append(receivers, r)
		*(**devToolsEventReceiver)(unsafe.Pointer(out)) = r
		return 0
	})
	wv.vtbl.CallDevToolsProtocolMethod = NewComProc(func(this, name, params, h uintptr) uintptr {
		method := windows.UTF16PtrToString((*uint16)(unsafe.Pointer(name)))
		handler := (*certificateEventHandler)(unsafe.Pointer(h))
		handler.addRef()
		completions[method] = handler
		return 0
	})
	var failures []NetworkFailure
	closeEvents, err := (&Chromium{webview: wv}).EnableNavigationDiagnostics(nil, func(f NetworkFailure) { failures = append(failures, f) }, func(err error) { t.Error(err) })
	if err != nil {
		t.Fatal(err)
	}
	complete := func(name, raw string) {
		h := completions[name]
		text, _ := windows.UTF16PtrFromString(raw)
		devToolsCompleted(h, 0, text)
		h.release()
		runtime.KeepAlive(text)
	}
	emit := func(name, raw string) {
		a := &eventArgs{vtbl: &struct {
			_IUnknownVtbl
			GetJSON ComProc
		}{}}
		a.vtbl.GetJSON, _ = comStringProc(raw)
		certificateEventInvoke(handlers[name], 0, uintptr(unsafe.Pointer(a)))
		runtime.KeepAlive(a)
	}
	emit("Network.requestWillBeSent", `{"requestId":"main","frameId":"root","type":"Document","request":{"url":"https://flint.local:8443/vault/id"}}`)
	emit("Network.loadingFailed", `{"requestId":"main","errorText":"net::ERR_SSL_PROTOCOL_ERROR"}`)
	if len(failures) != 0 {
		t.Fatal("reported without main frame identity")
	}
	complete("Page.getFrameTree", `{"frameTree":{"frame":{"id":"root"}}}`)
	complete("Network.enable", `{}`)
	if len(failures) != 1 || failures[0].ErrorText != "net::ERR_SSL_PROTOCOL_ERROR" || failures[0].URL != "https://flint.local:8443/vault/id" {
		t.Fatal("main failure lost", failures)
	}
	emit("Network.requestWillBeSent", `{"requestId":"iframe","frameId":"child","type":"Document","request":{"url":"https://child/"}}`)
	emit("Network.loadingFailed", `{"requestId":"iframe","errorText":"net::ERR_CERT_AUTHORITY_INVALID"}`)
	emit("Network.requestWillBeSent", `{"requestId":"script","frameId":"root","type":"Script","request":{"url":"https://flint.local/script.js"}}`)
	emit("Network.loadingFailed", `{"requestId":"script","errorText":"net::ERR_CONNECTION_RESET"}`)
	if len(failures) != 1 {
		t.Fatal("subresource/iframe failure leaked", failures)
	}
	closeEvents()
	closeEvents()
	if removed != 4 {
		t.Fatal("event cleanup mismatch", removed)
	}
	runtime.KeepAlive(wv)
	runtime.KeepAlive(receivers)
}
