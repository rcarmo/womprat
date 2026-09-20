//go:build windows

package edge

import (
	"math"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
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

func TestCookieListCallbackChecksHRESULTAndReleasesList(t *testing.T) {
	if _, err := cookieListToHTTP(0x80004005, nil); err == nil {
		t.Fatal("failed cookie callback accepted")
	}
	released := false
	list := &ICoreWebView2CookieList{vtbl: &iCoreWebView2CookieListVtbl{}}
	list.vtbl.GetCount = NewComProc(func(this, out uintptr) uintptr {
		*(*uint32)(unsafe.Pointer(out)) = 0
		return 0
	})
	list.vtbl.Release = NewComProc(func(this uintptr) uintptr { released = true; return 0 })
	cookies, err := cookieListToHTTP(0, list)
	if err != nil || len(cookies) != 0 || !released {
		t.Fatalf("cookies=%v err=%v released=%v", cookies, err, released)
	}
}

func comStringProc(value string) (ComProc, func()) {
	utf16, _ := windows.UTF16FromString(value)
	bytes := uintptr(len(utf16) * 2)
	ptr, _, _ := windows.NewLazySystemDLL("ole32.dll").NewProc("CoTaskMemAlloc").Call(bytes)
	for index, char := range utf16 {
		*(*uint16)(unsafe.Pointer(ptr + uintptr(index*2))) = char
	}
	return NewComProc(func(this, out uintptr) uintptr { *(*uintptr)(unsafe.Pointer(out)) = ptr; return 0 }), func() {}
}

func TestCookieListConvertsHttpOnlyMetadataAndReleasesInterfaces(t *testing.T) {
	cookieReleased, listReleased := false, false
	cookie := &ICoreWebView2Cookie{vtbl: &iCoreWebView2CookieVtbl{}}
	cookie.vtbl.GetName, _ = comStringProc("session")
	cookie.vtbl.GetValue, _ = comStringProc("secret")
	cookie.vtbl.GetDomain, _ = comStringProc("example.com")
	cookie.vtbl.GetPath, _ = comStringProc("/private")
	cookie.vtbl.GetIsHttpOnly = NewComProc(func(this, out uintptr) uintptr { *(*int32)(unsafe.Pointer(out)) = 1; return 0 })
	cookie.vtbl.GetIsSecure = NewComProc(func(this, out uintptr) uintptr { *(*int32)(unsafe.Pointer(out)) = 1; return 0 })
	cookie.vtbl.GetExpires = NewComProc(func(this, out uintptr) uintptr {
		*(*uint64)(unsafe.Pointer(out)) = math.Float64bits(2_000_000_000)
		return 0
	})
	cookie.vtbl.Release = NewComProc(func(this uintptr) uintptr { cookieReleased = true; return 0 })
	list := &ICoreWebView2CookieList{vtbl: &iCoreWebView2CookieListVtbl{}}
	list.vtbl.GetCount = NewComProc(func(this, out uintptr) uintptr { *(*uint32)(unsafe.Pointer(out)) = 1; return 0 })
	list.vtbl.GetItem = NewComProc(func(this, index, out uintptr) uintptr {
		*(*uintptr)(unsafe.Pointer(out)) = uintptr(unsafe.Pointer(cookie))
		return 0
	})
	list.vtbl.Release = NewComProc(func(this uintptr) uintptr { listReleased = true; return 0 })
	cookies, err := cookieListToHTTP(0, list)
	if err != nil || len(cookies) != 1 {
		t.Fatalf("cookies=%+v err=%v", cookies, err)
	}
	got := cookies[0]
	if got.Name != "session" || got.Value != "secret" || got.Domain != "example.com" || got.Path != "/private" || !got.HttpOnly || !got.Secure || got.Expires.Unix() != 2_000_000_000 {
		t.Fatalf("cookie=%+v", got)
	}
	if !cookieReleased || !listReleased {
		t.Fatalf("released cookie=%v list=%v", cookieReleased, listReleased)
	}
}

func TestCookiePairRejectsHeaderInjection(t *testing.T) {
	for _, tc := range []struct {
		name, value string
		valid       bool
	}{
		{"session", "secret", true},
		{"bad\r\nHeader", "secret", false},
		{"session", "bad\r\nHeader", false},
		{"bad=name", "secret", false},
	} {
		if got := validCookiePair(tc.name, tc.value); got != tc.valid {
			t.Fatalf("%q=%q valid=%v", tc.name, tc.value, got)
		}
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
