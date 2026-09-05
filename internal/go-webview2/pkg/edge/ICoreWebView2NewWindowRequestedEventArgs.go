package edge

import (
	"fmt"
	"golang.org/x/sys/windows"
	"unsafe"
)

// Base ICoreWebView2NewWindowRequestedEventArgs ABI, in SDK order.
type newWindowRequestedArgsVtbl struct {
	_IUnknownVtbl
	GetUri             ComProc
	PutNewWindow       ComProc
	GetNewWindow       ComProc
	PutHandled         ComProc
	GetHandled         ComProc
	GetIsUserInitiated ComProc
	GetDeferral        ComProc
	GetWindowFeatures  ComProc
}
type ICoreWebView2NewWindowRequestedEventArgs struct{ vtbl *newWindowRequestedArgsVtbl }

func (a *ICoreWebView2NewWindowRequestedEventArgs) URI() (string, error) {
	var value *uint16
	hr, _, _ := a.vtbl.GetUri.Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(&value)))
	if int32(hr) < 0 || value == nil {
		return "", fmt.Errorf("popup URI: HRESULT %#x", hr)
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(value))
	return windows.UTF16PtrToString(value), nil
}
func (a *ICoreWebView2NewWindowRequestedEventArgs) SetHandled() error {
	hr, _, _ := a.vtbl.PutHandled.Call(uintptr(unsafe.Pointer(a)), 1)
	if int32(hr) < 0 {
		return fmt.Errorf("popup handled: HRESULT %#x", hr)
	}
	return nil
}
