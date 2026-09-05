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

func TestPopupSuppressedEvenWhenURIReadFails(t *testing.T) {
	handled := false
	args := &ICoreWebView2NewWindowRequestedEventArgs{vtbl: &newWindowRequestedArgsVtbl{}}
	args.vtbl.PutHandled = NewComProc(func(this, value uintptr) uintptr { handled = value == 1; return 0 })
	args.vtbl.GetUri = NewComProc(func(this, out uintptr) uintptr { return 0x80004005 })
	e := &Chromium{NewWindowRequestedCallback: func(string) { t.Error("callback called with failed URI") }}
	e.NewWindowRequested(nil, args)
	if !handled {
		t.Fatal("default popup was not suppressed")
	}
}

func TestProcessFailureGetterErrorIsReportedAsUnknown(t *testing.T) {
	args := &ICoreWebView2ProcessFailedEventArgs{vtbl: &processFailedArgsVtbl{}}
	args.vtbl.GetProcessFailedKind = NewComProc(func(this, out uintptr) uintptr { return 0x80004005 })
	got := int32(99)
	e := &Chromium{ProcessFailedCallback: func(kind int32) { got = kind }}
	e.ProcessFailed(nil, args)
	if got != -1 {
		t.Fatalf("failure kind=%d", got)
	}
}

func TestUnreadablePermissionKindCannotUseGlobalGrant(t *testing.T) {
	args := &iCoreWebView2PermissionRequestedEventArgs{vtbl: &iCoreWebView2PermissionRequestedEventArgsVtbl{}}
	args.vtbl.GetPermissionKind = NewComProc(func(this, out uintptr) uintptr { return 0x80004005 })
	var state uintptr
	args.vtbl.PutState = NewComProc(func(this, value uintptr) uintptr { state = value; return 0 })
	grant := CoreWebView2PermissionStateAllow
	e := &Chromium{globalPermission: &grant}
	e.PermissionRequested(nil, args)
	if state != uintptr(CoreWebView2PermissionStateDeny) {
		t.Fatalf("state=%d", state)
	}
}
