package edge

type iCoreWebView2GetCookiesCompletedHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type ICoreWebView2GetCookiesCompletedHandler struct {
	vtbl *iCoreWebView2GetCookiesCompletedHandlerVtbl
	impl _ICoreWebView2GetCookiesCompletedHandlerImpl
}

type _ICoreWebView2GetCookiesCompletedHandlerImpl interface {
	_IUnknownImpl
	GetCookiesCompleted(errorCode uintptr, result *ICoreWebView2CookieList) uintptr
}

func getCookiesQueryInterface(this *ICoreWebView2GetCookiesCompletedHandler, refiid, object uintptr) uintptr {
	return this.impl.QueryInterface(refiid, object)
}
func getCookiesAddRef(this *ICoreWebView2GetCookiesCompletedHandler) uintptr {
	return this.impl.AddRef()
}
func getCookiesRelease(this *ICoreWebView2GetCookiesCompletedHandler) uintptr {
	return this.impl.Release()
}
func getCookiesInvoke(this *ICoreWebView2GetCookiesCompletedHandler, result uintptr, list *ICoreWebView2CookieList) uintptr {
	return this.impl.GetCookiesCompleted(result, list)
}

var iCoreWebView2GetCookiesCompletedHandlerFn = iCoreWebView2GetCookiesCompletedHandlerVtbl{
	_IUnknownVtbl{NewComProc(getCookiesQueryInterface), NewComProc(getCookiesAddRef), NewComProc(getCookiesRelease)},
	NewComProc(getCookiesInvoke),
}

func newICoreWebView2GetCookiesCompletedHandler(impl _ICoreWebView2GetCookiesCompletedHandlerImpl) *ICoreWebView2GetCookiesCompletedHandler {
	return &ICoreWebView2GetCookiesCompletedHandler{vtbl: &iCoreWebView2GetCookiesCompletedHandlerFn, impl: impl}
}
