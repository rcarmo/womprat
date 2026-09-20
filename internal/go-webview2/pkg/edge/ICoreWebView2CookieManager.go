package edge

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

type iCoreWebView2CookieManagerVtbl struct {
	_IUnknownVtbl
	CreateCookie                   ComProc
	CopyCookie                     ComProc
	GetCookies                     ComProc
	AddOrUpdateCookie              ComProc
	DeleteCookie                   ComProc
	DeleteCookies                  ComProc
	DeleteCookiesWithDomainAndPath ComProc
	DeleteAllCookies               ComProc
}

type ICoreWebView2CookieManager struct {
	vtbl *iCoreWebView2CookieManagerVtbl
}

func (i *ICoreWebView2CookieManager) Release() uintptr {
	r, _, _ := i.vtbl.Release.Call(uintptr(unsafe.Pointer(i)))
	return r
}

func (i *ICoreWebView2CookieManager) GetCookies(uri string, handler *ICoreWebView2GetCookiesCompletedHandler) error {
	ptr, err := windows.UTF16PtrFromString(uri)
	if err != nil {
		return err
	}
	hr, _, _ := i.vtbl.GetCookies.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(ptr)), uintptr(unsafe.Pointer(handler)))
	if int32(hr) < 0 {
		return fmt.Errorf("GetCookies failed: HRESULT %#x", hr)
	}
	return nil
}

func (i *ICoreWebView2) GetCookieManager() (*ICoreWebView2CookieManager, error) {
	var extended *ICoreWebView2_2
	iid := NewGUID("{9E8F0CF8-E670-4B5E-B2BC-73E061E3184C}")
	hr, _, _ := i.vtbl.QueryInterface.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&extended)))
	if int32(hr) < 0 || extended == nil {
		return nil, fmt.Errorf("ICoreWebView2_2: HRESULT %#x", hr)
	}
	defer extended.vtbl.Release.Call(uintptr(unsafe.Pointer(extended)))
	var manager *ICoreWebView2CookieManager
	hr, _, _ = extended.vtbl.GetCookieManager.Call(uintptr(unsafe.Pointer(extended)), uintptr(unsafe.Pointer(&manager)))
	if int32(hr) < 0 || manager == nil {
		return nil, fmt.Errorf("GetCookieManager: HRESULT %#x", hr)
	}
	return manager, nil
}
