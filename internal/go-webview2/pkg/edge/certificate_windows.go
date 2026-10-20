//go:build windows

package edge

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

// SDK 1.0.2903.40: ICoreWebView2_14 extends _13, whose last slot is 105.
// Keep the inherited slots opaque; this interface is obtained only by QI.
type certificateWebViewVtbl struct {
	Inherited                            [106]ComProc
	AddServerCertificateErrorDetected    ComProc
	RemoveServerCertificateErrorDetected ComProc
	ClearServerCertificateErrorActions   ComProc
}
type certificateWebView struct{ vtbl *certificateWebViewVtbl }

type certificateErrorArgsVtbl struct {
	_IUnknownVtbl
	GetErrorStatus       ComProc
	GetRequestURI        ComProc
	GetServerCertificate ComProc
	GetAction            ComProc
	PutAction            ComProc
	GetDeferral          ComProc
}
type certificateErrorArgs struct{ vtbl *certificateErrorArgsVtbl }
type serverCertificateVtbl struct {
	_IUnknownVtbl
	GetSubject        ComProc
	GetIssuer         ComProc
	GetValidFrom      ComProc
	GetValidTo        ComProc
	GetSerialNumber   ComProc
	GetDisplayName    ComProc
	ToPEMEncoding     ComProc
	GetPEMIssuerChain ComProc
}
type serverCertificate struct{ vtbl *serverCertificateVtbl }

// CertificateError contains copied values, never borrowed native event objects.
type CertificateError struct {
	URI    string
	PEM    string
	Status int32
	// Diagnostic is nonempty when event details or actions failed. Callers
	// must not offer an exception from incomplete certificate information.
	Diagnostic string
}

func comString(getter ComProc, receiver uintptr) (string, error) {
	var ptr *uint16
	r, _, _ := getter.Call(receiver, uintptr(unsafe.Pointer(&ptr)))
	if int32(r) < 0 {
		return "", fmt.Errorf("COM string getter: 0x%x", r)
	}
	if ptr == nil {
		return "", fmt.Errorf("COM string getter returned nil")
	}
	defer windows.CoTaskMemFree(unsafe.Pointer(ptr))
	return windows.UTF16PtrToString(ptr), nil
}

func (a *certificateErrorArgs) details() (CertificateError, error) {
	var out CertificateError
	self := uintptr(unsafe.Pointer(a))
	r, _, _ := a.vtbl.GetErrorStatus.Call(self, uintptr(unsafe.Pointer(&out.Status)))
	if int32(r) < 0 {
		return out, fmt.Errorf("certificate error status: 0x%x", r)
	}
	var err error
	out.URI, err = comString(a.vtbl.GetRequestURI, self)
	if err != nil {
		return out, err
	}
	var cert *serverCertificate
	r, _, _ = a.vtbl.GetServerCertificate.Call(self, uintptr(unsafe.Pointer(&cert)))
	if int32(r) < 0 || cert == nil {
		return out, fmt.Errorf("server certificate unavailable: 0x%x", r)
	}
	defer cert.vtbl.Release.Call(uintptr(unsafe.Pointer(cert)))
	out.PEM, err = comString(cert.vtbl.ToPEMEncoding, uintptr(unsafe.Pointer(cert)))
	return out, err
}

// COM retains callbacks and Go must retain their closures until the last Release.
type certificateEventVtbl struct {
	_IUnknownVtbl
	Invoke ComProc
}
type certificateEventHandler struct {
	vtbl     *certificateEventVtbl
	iid      GUID
	refs     int32
	invoke   func(uintptr)
	complete func(int32, string)
}

var certificateEventRoots = struct {
	sync.Mutex
	active map[*certificateEventHandler]struct{}
}{active: make(map[*certificateEventHandler]struct{})}

func (h *certificateEventHandler) addRef() uintptr { return uintptr(atomic.AddInt32(&h.refs, 1)) }
func (h *certificateEventHandler) release() uintptr {
	n := atomic.AddInt32(&h.refs, -1)
	if n == 0 {
		certificateEventRoots.Lock()
		delete(certificateEventRoots.active, h)
		certificateEventRoots.Unlock()
	}
	return uintptr(n)
}
func certificateEventQuery(h *certificateEventHandler, iid, object uintptr) uintptr {
	if object == 0 {
		return 0x80004003
	}
	*(*uintptr)(unsafe.Pointer(object)) = 0
	if iid == 0 {
		return 0x80004003
	}
	guid := *(*GUID)(unsafe.Pointer(iid))
	if guid != h.iid && guid != *NewGUID("{00000000-0000-0000-C000-000000000046}") {
		return 0x80004002
	}
	*(*uintptr)(unsafe.Pointer(object)) = uintptr(unsafe.Pointer(h))
	h.addRef()
	return 0
}
func certificateEventAddRef(h *certificateEventHandler) uintptr  { return h.addRef() }
func certificateEventRelease(h *certificateEventHandler) uintptr { return h.release() }
func certificateEventInvoke(h *certificateEventHandler, sender, args uintptr) uintptr {
	if args != 0 {
		h.invoke(args)
	}
	return 0
}

var certificateEventMethods = certificateEventVtbl{
	_IUnknownVtbl{NewComProc(certificateEventQuery), NewComProc(certificateEventAddRef), NewComProc(certificateEventRelease)},
	NewComProc(certificateEventInvoke),
}

func newCertificateEvent(iid string, invoke func(uintptr)) *certificateEventHandler {
	h := &certificateEventHandler{vtbl: &certificateEventMethods, iid: *NewGUID(iid), refs: 1, invoke: invoke}
	certificateEventRoots.Lock()
	certificateEventRoots.active[h] = struct{}{}
	certificateEventRoots.Unlock()
	return h
}

// EnableCertificateErrors observes top-level navigation and certificate errors.
// Returning true explicitly sets ALWAYS_ALLOW: WebView2 remembers that host/leaf
// certificate for this browser session (shared profile), not across restarts.
// Otherwise CANCEL is explicit; no engine error page can bypass app consent.
// Callbacks and cleanup must run on the owning UI thread.
func (e *Chromium) EnableCertificateErrors(starting func(string), decide func(CertificateError) bool) (func(), error) {
	if e.webview == nil {
		return nil, fmt.Errorf("WebView2 unavailable")
	}
	var view *certificateWebView
	iid := NewGUID("{6DAA4F10-4A90-4753-8898-77C5DF534165}")
	r, _, _ := e.webview.vtbl.QueryInterface.Call(uintptr(unsafe.Pointer(e.webview)), uintptr(unsafe.Pointer(iid)), uintptr(unsafe.Pointer(&view)))
	if int32(r) < 0 || view == nil {
		return nil, fmt.Errorf("certificate exceptions need WebView2 _14: 0x%x", r)
	}
	certHandler := newCertificateEvent("{969B3A26-D85E-4795-8199-FEF57344DA22}", func(raw uintptr) {
		a := (*certificateErrorArgs)(unsafe.Pointer(raw))
		result, _, _ := a.vtbl.PutAction.Call(raw, 1) // CANCEL before reading anything
		if int32(result) < 0 {
			if decide != nil {
				decide(CertificateError{Diagnostic: fmt.Sprintf("Cancel certificate error: HRESULT 0x%08X", uint32(result))})
			}
			return
		}
		info, err := a.details()
		if err != nil {
			info.Diagnostic = err.Error()
			if decide != nil {
				decide(info)
			}
			return
		}
		if decide != nil && decide(info) {
			result, _, _ = a.vtbl.PutAction.Call(raw, 0)
			if int32(result) < 0 {
				decide(CertificateError{Diagnostic: fmt.Sprintf("Allow certificate error: HRESULT 0x%08X", uint32(result))})
			}
		}
	})
	startHandler := newCertificateEvent("{9ADBE429-F36D-432B-9DDC-F8881FBD76E3}", func(raw uintptr) {
		// ICoreWebView2NavigationStartingEventArgs: IUnknown, get_Uri.
		args := (*struct {
			vtbl *struct {
				_IUnknownVtbl
				GetURI ComProc
			}
		})(unsafe.Pointer(raw))
		uri, err := comString(args.vtbl.GetURI, raw)
		if starting != nil {
			if err != nil {
				starting("")
			} else {
				starting(uri)
			}
		}
	})
	var certToken, startToken _EventRegistrationToken
	r, _, _ = view.vtbl.AddServerCertificateErrorDetected.Call(uintptr(unsafe.Pointer(view)), uintptr(unsafe.Pointer(certHandler)), uintptr(unsafe.Pointer(&certToken)))
	certHandler.release() // registration owns the reference, if successful
	if int32(r) < 0 {
		startHandler.release()
		view.vtbl.Inherited[2].Call(uintptr(unsafe.Pointer(view)))
		return nil, fmt.Errorf("register certificate handler: 0x%x", r)
	}
	r, _, _ = e.webview.vtbl.AddNavigationStarting.Call(uintptr(unsafe.Pointer(e.webview)), uintptr(unsafe.Pointer(startHandler)), uintptr(unsafe.Pointer(&startToken)))
	startHandler.release()
	if int32(r) < 0 {
		view.vtbl.RemoveServerCertificateErrorDetected.Call(uintptr(unsafe.Pointer(view)), uintptr(certToken.Value))
		view.vtbl.Inherited[2].Call(uintptr(unsafe.Pointer(view)))
		return nil, fmt.Errorf("register navigation handler: 0x%x", r)
	}
	var once sync.Once
	return func() {
		once.Do(func() {
			e.webview.vtbl.RemoveNavigationStarting.Call(uintptr(unsafe.Pointer(e.webview)), uintptr(startToken.Value))
			view.vtbl.RemoveServerCertificateErrorDetected.Call(uintptr(unsafe.Pointer(view)), uintptr(certToken.Value))
			view.vtbl.Inherited[2].Call(uintptr(unsafe.Pointer(view)))
			runtime.KeepAlive(e)
		})
	}, nil
}
