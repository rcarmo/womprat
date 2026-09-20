package edge

import (
	"fmt"
	"unsafe"
)

type iCoreWebView2CookieListVtbl struct {
	_IUnknownVtbl
	GetCount ComProc
	GetItem  ComProc
}

type ICoreWebView2CookieList struct{ vtbl *iCoreWebView2CookieListVtbl }

func (i *ICoreWebView2CookieList) Release() uintptr {
	r, _, _ := i.vtbl.Release.Call(uintptr(unsafe.Pointer(i)))
	return r
}

func (i *ICoreWebView2CookieList) Count() (uint32, error) {
	var count uint32
	hr, _, _ := i.vtbl.GetCount.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&count)))
	if int32(hr) < 0 {
		return 0, fmt.Errorf("cookie count: HRESULT %#x", hr)
	}
	return count, nil
}

func (i *ICoreWebView2CookieList) Item(index uint32) (*ICoreWebView2Cookie, error) {
	var cookie *ICoreWebView2Cookie
	hr, _, _ := i.vtbl.GetItem.Call(uintptr(unsafe.Pointer(i)), uintptr(index), uintptr(unsafe.Pointer(&cookie)))
	if int32(hr) < 0 || cookie == nil {
		return nil, fmt.Errorf("cookie item %d: HRESULT %#x", index, hr)
	}
	return cookie, nil
}
