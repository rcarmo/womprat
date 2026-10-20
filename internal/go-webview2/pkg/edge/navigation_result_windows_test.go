//go:build windows

package edge

import (
	"strings"
	"testing"
	"unsafe"
)

func TestNavigationCompletedRegistrationWritesRealToken(t *testing.T) {
	view := &ICoreWebView2{vtbl: &iCoreWebView2Vtbl{}}
	view.vtbl.AddNavigationCompleted = NewComProc(func(this, handler, token uintptr) uintptr {
		(*_EventRegistrationToken)(unsafe.Pointer(token)).Value = 73
		return 0
	})
	var token _EventRegistrationToken
	if err := view.AddNavigationCompleted(nil, &token); err != nil || token.Value != 73 {
		t.Fatal("token indirection wrong", token, err)
	}
	view.vtbl.AddNavigationCompleted = NewComProc(func(this, handler, token uintptr) uintptr { return 0x80004005 })
	if err := view.AddNavigationCompleted(nil, &token); err == nil || !strings.Contains(err.Error(), "0x80004005") {
		t.Fatal("registration HRESULT hidden", err)
	}
}

func TestNavigationResultDoesNotHideGetterFailure(t *testing.T) {
	args := &ICoreWebView2NavigationCompletedEventArgs{vtbl: &_ICoreWebView2NavigationCompletedEventArgsVtbl{}}
	args.vtbl.GetIsSuccess = NewComProc(func(this, out uintptr) uintptr { *(*int32)(unsafe.Pointer(out)) = 0; return 0 })
	args.vtbl.GetWebErrorStatus = NewComProc(func(this, out uintptr) uintptr { *(*int32)(unsafe.Pointer(out)) = 5; return 0 })
	if ok, status, err := args.Result(); ok || status != 5 || err != nil {
		t.Fatal("certificate result lost", ok, status, err)
	}
	args.vtbl.GetWebErrorStatus = NewComProc(func(this, out uintptr) uintptr { return 0x80004005 })
	if ok, _, err := args.Result(); ok || err == nil || !strings.Contains(err.Error(), "0x80004005") {
		t.Fatal("status HRESULT hidden", err)
	}
	args.vtbl.GetIsSuccess = NewComProc(func(this, out uintptr) uintptr { return 0x80070005 })
	if ok, _, err := args.Result(); ok || err == nil || !strings.Contains(err.Error(), "0x80070005") {
		t.Fatal("success HRESULT hidden", err)
	}
	var missing *ICoreWebView2NavigationCompletedEventArgs
	if ok, _, err := missing.Result(); ok || err == nil {
		t.Fatal("nil result accepted")
	}
}
