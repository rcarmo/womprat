//go:build windows

package edge

import (
	"testing"
	"unsafe"
)

func TestAddRefPassesReceiver(t *testing.T) {
	var receiver uintptr
	args := &ICoreWebView2NavigationCompletedEventArgs{vtbl: &_ICoreWebView2NavigationCompletedEventArgsVtbl{}}
	args.vtbl.AddRef = NewComProc(func(this uintptr) uintptr { receiver = this; return 2 })
	if args.AddRef() != 2 || receiver != uintptr(unsafe.Pointer(args)) {
		t.Fatal("AddRef lost receiver")
	}
}
func TestNavigationGetterFailureIsNotSuccess(t *testing.T) {
	args := &ICoreWebView2NavigationCompletedEventArgs{vtbl: &_ICoreWebView2NavigationCompletedEventArgsVtbl{}}
	args.vtbl.GetIsSuccess = NewComProc(func(this, out uintptr) uintptr { return 0x80004005 })
	if args.IsSuccess() {
		t.Fatal("failed getter reported success")
	}
}
func TestResourceRequestChecksHRESULT(t *testing.T) {
	args := &ICoreWebView2WebResourceRequestedEventArgs{vtbl: &_ICoreWebView2WebResourceRequestedEventArgsVtbl{}}
	args.vtbl.GetRequest = NewComProc(func(this, out uintptr) uintptr { return 0x80004005 })
	if _, err := args.GetRequest(); err == nil {
		t.Fatal("failed HRESULT accepted")
	}
	args.vtbl.PutResponse = NewComProc(func(this, response uintptr) uintptr { return 0x80004005 })
	if err := args.PutResponse(nil); err == nil {
		t.Fatal("failed HRESULT accepted")
	}
}
