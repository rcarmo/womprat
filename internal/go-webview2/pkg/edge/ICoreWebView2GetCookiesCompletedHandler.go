package edge

import (
	"sync"
	"sync/atomic"
	"unsafe"
)

type iCoreWebView2GetCookiesCompletedHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type ICoreWebView2GetCookiesCompletedHandler struct {
	vtbl      *iCoreWebView2GetCookiesCompletedHandlerVtbl
	impl      _ICoreWebView2GetCookiesCompletedHandlerImpl
	refs      int32
	onRelease func()
}

type _ICoreWebView2GetCookiesCompletedHandlerImpl interface {
	GetCookiesCompleted(errorCode uintptr, result *ICoreWebView2CookieList) uintptr
}

var (
	cookieHandlerIUnknownIID = NewGUID("{00000000-0000-0000-C000-000000000046}")
	cookieHandlerIID         = NewGUID("{5A4F5069-5C15-47C3-8646-F4DE1C116670}")
	// Native COM references are invisible to the Go collector. Keep callback
	// objects rooted until WebView2 releases its final reference, including
	// after Invoke returns or the originating tab closes.
	cookieHandlerRoots = struct {
		sync.Mutex
		active map[*ICoreWebView2GetCookiesCompletedHandler]struct{}
	}{active: make(map[*ICoreWebView2GetCookiesCompletedHandler]struct{})}
)

func getCookiesQueryInterface(this *ICoreWebView2GetCookiesCompletedHandler, refiid, object uintptr) uintptr {
	if object == 0 {
		return 0x80004003
	} // E_POINTER
	out := (*uintptr)(unsafe.Pointer(object))
	*out = 0
	if refiid == 0 {
		return 0x80004003
	}
	iid := (*GUID)(unsafe.Pointer(refiid))
	if *iid != *cookieHandlerIUnknownIID && *iid != *cookieHandlerIID {
		return 0x80004002 // E_NOINTERFACE
	}
	getCookiesAddRef(this)
	*out = uintptr(unsafe.Pointer(this))
	return 0 // S_OK
}
func getCookiesAddRef(this *ICoreWebView2GetCookiesCompletedHandler) uintptr {
	return uintptr(atomic.AddInt32(&this.refs, 1))
}
func getCookiesRelease(this *ICoreWebView2GetCookiesCompletedHandler) uintptr {
	refs := atomic.AddInt32(&this.refs, -1)
	if refs == 0 {
		if this.onRelease != nil {
			this.onRelease()
		}
		cookieHandlerRoots.Lock()
		delete(cookieHandlerRoots.active, this)
		cookieHandlerRoots.Unlock()
	}
	return uintptr(refs)
}
func getCookiesInvoke(this *ICoreWebView2GetCookiesCompletedHandler, result uintptr, list *ICoreWebView2CookieList) uintptr {
	// COM owns its reference for the duration of Invoke. This method neither
	// adopts nor releases the borrowed [in] result list.
	return this.impl.GetCookiesCompleted(result, list)
}

var iCoreWebView2GetCookiesCompletedHandlerFn = iCoreWebView2GetCookiesCompletedHandlerVtbl{
	_IUnknownVtbl{NewComProc(getCookiesQueryInterface), NewComProc(getCookiesAddRef), NewComProc(getCookiesRelease)},
	NewComProc(getCookiesInvoke),
}

func newICoreWebView2GetCookiesCompletedHandler(impl _ICoreWebView2GetCookiesCompletedHandlerImpl) *ICoreWebView2GetCookiesCompletedHandler {
	handler := &ICoreWebView2GetCookiesCompletedHandler{vtbl: &iCoreWebView2GetCookiesCompletedHandlerFn, impl: impl, refs: 1}
	cookieHandlerRoots.Lock()
	cookieHandlerRoots.active[handler] = struct{}{}
	cookieHandlerRoots.Unlock()
	return handler
}
