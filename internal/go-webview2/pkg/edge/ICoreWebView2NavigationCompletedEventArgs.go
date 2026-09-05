package edge

import "unsafe"

type _ICoreWebView2NavigationCompletedEventArgsVtbl struct {
	_IUnknownVtbl
	GetIsSuccess      ComProc
	GetWebErrorStatus ComProc
	GetNavigationId   ComProc
}

type ICoreWebView2NavigationCompletedEventArgs struct {
	vtbl *_ICoreWebView2NavigationCompletedEventArgsVtbl
}

func (i *ICoreWebView2NavigationCompletedEventArgs) AddRef() uintptr {
	r, _, _ := i.vtbl.AddRef.Call(uintptr(unsafe.Pointer(i)))
	return r
}

// IsSuccess reports whether the navigation completed successfully. A false
// result means the WebView reached an error state (DNS failure, connection
// refused, proxy rejection, etc.) and GetWebErrorStatus describes the cause.
func (i *ICoreWebView2NavigationCompletedEventArgs) IsSuccess() bool {
	var isSuccess int32
	// A failed getter must not claim that navigation succeeded.
	r, _, _ := i.vtbl.GetIsSuccess.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(&isSuccess)),
	)
	if int32(r) < 0 {
		return false
	}
	return isSuccess != 0
}

// WebErrorStatus returns the COREWEBVIEW2_WEB_ERROR_STATUS code for the
// navigation. 0 (COREWEBVIEW2_WEB_ERROR_STATUS_UNKNOWN) is returned when the
// status cannot be read.
func (i *ICoreWebView2NavigationCompletedEventArgs) WebErrorStatus() int32 {
	var status int32
	r, _, _ := i.vtbl.GetWebErrorStatus.Call(
		uintptr(unsafe.Pointer(i)),
		uintptr(unsafe.Pointer(&status)),
	)
	if r != 0 {
		return 0
	}
	return status
}
