package edge

type iCoreWebView2DocumentTitleChangedEventHandlerVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}

type ICoreWebView2DocumentTitleChangedEventHandler struct {
	vtbl *iCoreWebView2DocumentTitleChangedEventHandlerVtbl
	impl iCoreWebView2DocumentTitleChangedEventHandlerImpl
}

type iCoreWebView2DocumentTitleChangedEventHandlerImpl interface {
	_IUnknownImpl
	DocumentTitleChanged(sender *ICoreWebView2) uintptr
}

func documentTitleQueryInterface(this *ICoreWebView2DocumentTitleChangedEventHandler, refiid, object uintptr) uintptr {
	return this.impl.QueryInterface(refiid, object)
}
func documentTitleAddRef(this *ICoreWebView2DocumentTitleChangedEventHandler) uintptr {
	return this.impl.AddRef()
}
func documentTitleRelease(this *ICoreWebView2DocumentTitleChangedEventHandler) uintptr {
	return this.impl.Release()
}
func documentTitleInvoke(this *ICoreWebView2DocumentTitleChangedEventHandler, sender *ICoreWebView2, _ uintptr) uintptr {
	return this.impl.DocumentTitleChanged(sender)
}

var iCoreWebView2DocumentTitleChangedEventHandlerFn = iCoreWebView2DocumentTitleChangedEventHandlerVtbl{
	_IUnknownVtbl{NewComProc(documentTitleQueryInterface), NewComProc(documentTitleAddRef), NewComProc(documentTitleRelease)},
	NewComProc(documentTitleInvoke),
}

func newICoreWebView2DocumentTitleChangedEventHandler(impl iCoreWebView2DocumentTitleChangedEventHandlerImpl) *ICoreWebView2DocumentTitleChangedEventHandler {
	return &ICoreWebView2DocumentTitleChangedEventHandler{vtbl: &iCoreWebView2DocumentTitleChangedEventHandlerFn, impl: impl}
}
