package edge

import (
	"fmt"
	"math"
	"unsafe"

	"golang.org/x/sys/windows"
)

type iCoreWebView2CookieVtbl struct {
	_IUnknownVtbl
	GetName       ComProc
	GetValue      ComProc
	PutValue      ComProc
	GetDomain     ComProc
	GetPath       ComProc
	GetExpires    ComProc
	PutExpires    ComProc
	GetIsHttpOnly ComProc
	PutIsHttpOnly ComProc
	GetSameSite   ComProc
	PutSameSite   ComProc
	GetIsSecure   ComProc
	PutIsSecure   ComProc
}

type ICoreWebView2Cookie struct{ vtbl *iCoreWebView2CookieVtbl }

func (i *ICoreWebView2Cookie) Release() uintptr {
	r, _, _ := i.vtbl.Release.Call(uintptr(unsafe.Pointer(i)))
	return r
}

func (i *ICoreWebView2Cookie) stringValue(call ComProc, field string) (string, error) {
	var value *uint16
	hr, _, _ := call.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&value)))
	if int32(hr) < 0 || value == nil {
		return "", fmt.Errorf("cookie %s: HRESULT %#x", field, hr)
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(value))
	return windows.UTF16PtrToString(value), nil
}

func (i *ICoreWebView2Cookie) Name() (string, error)  { return i.stringValue(i.vtbl.GetName, "name") }
func (i *ICoreWebView2Cookie) Value() (string, error) { return i.stringValue(i.vtbl.GetValue, "value") }
func (i *ICoreWebView2Cookie) Domain() (string, error) {
	return i.stringValue(i.vtbl.GetDomain, "domain")
}
func (i *ICoreWebView2Cookie) Path() (string, error) { return i.stringValue(i.vtbl.GetPath, "path") }

func (i *ICoreWebView2Cookie) boolValue(call ComProc, field string) (bool, error) {
	var value int32
	hr, _, _ := call.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&value)))
	if int32(hr) < 0 {
		return false, fmt.Errorf("cookie %s: HRESULT %#x", field, hr)
	}
	return value != 0, nil
}

func (i *ICoreWebView2Cookie) IsHTTPOnly() (bool, error) {
	return i.boolValue(i.vtbl.GetIsHttpOnly, "httpOnly")
}
func (i *ICoreWebView2Cookie) IsSecure() (bool, error) {
	return i.boolValue(i.vtbl.GetIsSecure, "secure")
}
func (i *ICoreWebView2Cookie) Expires() (float64, error) {
	var bits uint64
	hr, _, _ := i.vtbl.GetExpires.Call(uintptr(unsafe.Pointer(i)), uintptr(unsafe.Pointer(&bits)))
	if int32(hr) < 0 {
		return 0, fmt.Errorf("cookie expires: HRESULT %#x", hr)
	}
	return math.Float64frombits(bits), nil
}
