package edge

import "unsafe"

type processFailedArgsVtbl struct {
	_IUnknownVtbl
	GetProcessFailedKind ComProc
}
type ICoreWebView2ProcessFailedEventArgs struct{ vtbl *processFailedArgsVtbl }

func (a *ICoreWebView2ProcessFailedEventArgs) Kind() int32 {
	var kind int32
	hr, _, _ := a.vtbl.GetProcessFailedKind.Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(&kind)))
	if int32(hr) < 0 {
		return -1
	}
	return kind
}
