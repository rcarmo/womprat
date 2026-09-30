//go:build windows

package edge

import (
	"net/http"
	"runtime"
	"sync/atomic"
	"testing"
	"unsafe"
)

func cookieHandlerIsRooted(h *ICoreWebView2GetCookiesCompletedHandler) bool {
	cookieHandlerRoots.Lock()
	defer cookieHandlerRoots.Unlock()
	_, ok := cookieHandlerRoots.active[h]
	return ok
}

// Contract references (WebView2 SDK 1.0.3537.50):
// https://learn.microsoft.com/en-us/microsoft-edge/webview2/reference/win32/icorewebview2getcookiescompletedhandler
// https://learn.microsoft.com/en-us/windows/win32/com/rules-for-managing-reference-counts
// Invoke receives an [in] list; GetValueAtIndex returns an owned [out] cookie.
//
// Emulate a COM caller which owns the result throughout Invoke and releases
// its list and callback references only AFTER Invoke has returned. This is
// independent of the converter and would catch its former double-release.
func TestCookieCompletionBorrowsResultUntilCallerReleasesIt(t *testing.T) {
	var refs int32 = 1
	list := &ICoreWebView2CookieList{vtbl: &iCoreWebView2CookieListVtbl{}}
	list.vtbl.GetCount = NewComProc(func(this, out uintptr) uintptr {
		*(*uint32)(unsafe.Pointer(out)) = 0
		return 0
	})
	list.vtbl.Release = NewComProc(func(this uintptr) uintptr {
		return uintptr(atomic.AddInt32(&refs, -1))
	})
	callbackCount := 0
	request := &cookieRequest{callback: func(c []*http.Cookie, err error) {
		callbackCount++
		if err != nil || len(c) != 0 || atomic.LoadInt32(&refs) != 1 {
			t.Errorf("callback: len=%d error=%v refs=%d", len(c), err, refs)
		}
	}}
	handler := newICoreWebView2GetCookiesCompletedHandler(request)
	request.handler = handler
	getCookiesAddRef(handler)  // WebView2 retains the asynchronous callback.
	getCookiesRelease(handler) // Caller returns from GetCookies.
	runtime.GC()
	if !cookieHandlerIsRooted(handler) {
		t.Fatal("callback collected before completion")
	}
	handler.vtbl.Invoke.Call(uintptr(unsafe.Pointer(handler)), 0, uintptr(unsafe.Pointer(list)))
	runtime.GC()
	if refs != 1 || !cookieHandlerIsRooted(handler) {
		t.Fatalf("borrowed result or callback released early: refs=%d", refs)
	}
	list.Release() // COM caller's release; not the converter's.
	if refs != 0 || callbackCount != 1 {
		t.Fatalf("double release/completion: refs=%d calls=%d", refs, callbackCount)
	}
	if getCookiesRelease(handler) != 0 || cookieHandlerIsRooted(handler) {
		t.Fatal("callback not released after final COM reference")
	}
	runtime.KeepAlive(list)
}

func TestCookieHandlerQueryInterfaceAndReferenceCounts(t *testing.T) {
	handler := newICoreWebView2GetCookiesCompletedHandler(&cookieRequest{})
	defer getCookiesRelease(handler)
	for _, iid := range []*GUID{cookieHandlerIUnknownIID, cookieHandlerIID} {
		var object uintptr
		hr, _, _ := handler.vtbl.QueryInterface.Call(uintptr(unsafe.Pointer(handler)), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&object)))
		if hr != 0 || object != uintptr(unsafe.Pointer(handler)) || atomic.LoadInt32(&handler.refs) != 2 {
			t.Fatalf("QueryInterface hr=%#x object=%#x refs=%d", hr, object, handler.refs)
		}
		getCookiesRelease(handler)
	}
	other := NewGUID("{12345678-1234-1234-1234-123456789012}")
	object := uintptr(123)
	hr := getCookiesQueryInterface(handler, uintptr(unsafe.Pointer(other)), uintptr(unsafe.Pointer(&object)))
	if hr != 0x80004002 || object != 0 || handler.refs != 1 {
		t.Fatalf("unsupported QI hr=%#x out=%x refs=%d", hr, object, handler.refs)
	}
	if getCookiesQueryInterface(handler, 0, 0) != 0x80004003 {
		t.Fatal("null out parameter accepted")
	}
}

func TestCookieCompletionGetterFailureDoesNotReleaseBorrowedList(t *testing.T) {
	for _, failed := range []string{"count", "item"} {
		t.Run(failed, func(t *testing.T) {
			released := false
			list := &ICoreWebView2CookieList{vtbl: &iCoreWebView2CookieListVtbl{}}
			list.vtbl.Release = NewComProc(func(this uintptr) uintptr { released = true; return 0 })
			list.vtbl.GetCount = NewComProc(func(this, out uintptr) uintptr {
				if failed == "count" {
					return 0x80004005
				}
				*(*uint32)(unsafe.Pointer(out)) = 1
				return 0
			})
			list.vtbl.GetItem = NewComProc(func(this, index, out uintptr) uintptr { return 0x80004005 })
			if _, err := cookieListToHTTP(0, list); err == nil || released {
				t.Fatalf("getter error=%v released=%t", err, released)
			}
		})
	}
}

// Fake only the SDK call boundaries. Both immediate failure and asynchronous
// completion must balance the caller's initial reference and WebView2's ref.
func TestGetCookiesCallbackLifetimeAcrossNativeReturn(t *testing.T) {
	for _, mode := range []string{"async", "sync", "start-error", "pending-limit", "completion-error"} {
		t.Run(mode, func(t *testing.T) {
			view := &ICoreWebView2{vtbl: &iCoreWebView2Vtbl{}}
			extended := &ICoreWebView2_2{vtbl: &iCoreWebView2_2Vtbl{}}
			manager := &ICoreWebView2CookieManager{vtbl: &iCoreWebView2CookieManagerVtbl{}}
			var pending *ICoreWebView2GetCookiesCompletedHandler
			var managerReleases, extendedReleases, calls int
			view.vtbl.QueryInterface = NewComProc(func(this, iid, out uintptr) uintptr {
				*(*uintptr)(unsafe.Pointer(out)) = uintptr(unsafe.Pointer(extended))
				return 0
			})
			extended.vtbl.GetCookieManager = NewComProc(func(this, out uintptr) uintptr {
				*(*uintptr)(unsafe.Pointer(out)) = uintptr(unsafe.Pointer(manager))
				return 0
			})
			extended.vtbl.Release = NewComProc(func(this uintptr) uintptr { extendedReleases++; return 0 })
			manager.vtbl.Release = NewComProc(func(this uintptr) uintptr { managerReleases++; return 0 })
			manager.vtbl.GetCookies = NewComProc(func(this, uri, h uintptr) uintptr {
				pending = (*ICoreWebView2GetCookiesCompletedHandler)(unsafe.Pointer(h))
				if mode == "start-error" {
					return 0x80004005
				}
				getCookiesAddRef(pending)
				if mode == "sync" {
					getCookiesInvoke(pending, 0x80004005, nil)
					getCookiesRelease(pending)
				}
				return 0
			})
			e := &Chromium{webview: view}
			if mode == "pending-limit" {
				e.cookieRequests = make(map[*ICoreWebView2GetCookiesCompletedHandler]*cookieRequest)
				for i := 0; i < maxPendingCookieRequests; i++ {
					e.cookieRequests[&ICoreWebView2GetCookiesCompletedHandler{}] = &cookieRequest{}
				}
			}
			cookieHandlerRoots.Lock()
			rootsBefore := len(cookieHandlerRoots.active)
			cookieHandlerRoots.Unlock()
			err := e.GetCookies("https://example.test/download", func(_ []*http.Cookie, err error) {
				calls++
				if err == nil {
					t.Error("expected callback failure for nil list")
				}
			})
			if mode == "pending-limit" {
				cookieHandlerRoots.Lock()
				rootsAfter := len(cookieHandlerRoots.active)
				cookieHandlerRoots.Unlock()
				if err == nil || pending != nil || calls != 0 || rootsBefore != rootsAfter || managerReleases != 1 || extendedReleases != 1 || len(e.cookieRequests) != maxPendingCookieRequests {
					t.Fatalf("request limit leaked references: err=%v pending=%p calls=%d roots=%d/%d", err, pending, calls, rootsBefore, rootsAfter)
				}
				return
			}
			if (err != nil) != (mode == "start-error") {
				t.Fatalf("start error=%v", err)
			}
			if mode == "async" || mode == "completion-error" {
				if pending.refs != 1 || !cookieHandlerIsRooted(pending) || len(e.cookieRequests) != 1 {
					t.Fatal("async request lost after API returned")
				}
				runtime.GC()
				getCookiesInvoke(pending, 0x80004005, nil)
				if !cookieHandlerIsRooted(pending) || len(e.cookieRequests) != 1 {
					t.Fatal("Invoke dropped native reference")
				}
				getCookiesRelease(pending)
			}
			wantCalls := 1
			if mode == "start-error" {
				wantCalls = 0
			}
			if pending.refs != 0 || cookieHandlerIsRooted(pending) || len(e.cookieRequests) != 0 || calls != wantCalls || managerReleases != 1 || extendedReleases != 1 {
				t.Fatalf("unbalanced lifecycle: refs=%d pending=%d calls=%d manager=%d extended=%d", pending.refs, len(e.cookieRequests), calls, managerReleases, extendedReleases)
			}
			runtime.KeepAlive(extended)
			runtime.KeepAlive(manager)
		})
	}
}

func TestGetCookiesRejectsUnavailableBrowser(t *testing.T) {
	e := &Chromium{}
	if e.GetCookies("https://example.test/", func([]*http.Cookie, error) {}) == nil {
		t.Fatal("nil WebView accepted")
	}
}
