package edge

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

func (i *ICoreWebView2) DocumentTitle() (string, error) {
	if i == nil {
		return "", fmt.Errorf("nil WebView2 title source")
	}
	var value *uint16
	hr, _, _ := i.vtbl.GetDocumentTitle.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&value)))
	if int32(hr) < 0 || value == nil {
		return "", fmt.Errorf("GetDocumentTitle: HRESULT %#x", hr)
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(value))
	return windows.UTF16PtrToString(value), nil
}
