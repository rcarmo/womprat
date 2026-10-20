//go:build windows

package edge

import (
	"runtime"
	"testing"
	"unsafe"
)

func TestCertificateSDKLayout(t *testing.T) {
	var v certificateWebViewVtbl
	word := unsafe.Sizeof(ComProc(0))
	if unsafe.Offsetof(v.AddServerCertificateErrorDetected) != 106*word ||
		unsafe.Offsetof(v.RemoveServerCertificateErrorDetected) != 107*word ||
		unsafe.Offsetof(v.ClearServerCertificateErrorActions) != 108*word {
		t.Fatal("ICoreWebView2_14 SDK slots differ")
	}
	var a certificateErrorArgsVtbl
	if unsafe.Offsetof(a.PutAction) != 7*word || unsafe.Offsetof(a.GetDeferral) != 8*word {
		t.Fatal("certificate event args slots differ")
	}
	var c serverCertificateVtbl
	if unsafe.Offsetof(c.ToPEMEncoding) != 9*word {
		t.Fatal("certificate PEM slot differs")
	}
}

func TestCertificateEventIdentityAndRootLifetime(t *testing.T) {
	const iid = "{969B3A26-D85E-4795-8199-FEF57344DA22}"
	called := false
	h := newCertificateEvent(iid, func(raw uintptr) { called = raw == 17 })
	var object uintptr
	unknown := NewGUID("{00000000-0000-0000-C000-000000000046}")
	if got := certificateEventQuery(h, uintptr(unsafe.Pointer(unknown)), uintptr(unsafe.Pointer(&object))); got != 0 || object != uintptr(unsafe.Pointer(h)) {
		t.Fatal("IUnknown failed")
	}
	if h.release() != 1 {
		t.Fatal("QI ref not owned")
	}
	other := NewGUID("{012193ED-7C13-48FF-969D-A84C1F432A14}")
	if certificateEventQuery(h, uintptr(unsafe.Pointer(other)), uintptr(unsafe.Pointer(&object))) != 0x80004002 || object != 0 {
		t.Fatal("wrong IID accepted")
	}
	if certificateEventQuery(h, 0, uintptr(unsafe.Pointer(&object))) != 0x80004003 {
		t.Fatal("nil IID accepted")
	}
	certificateEventInvoke(h, 0, 17)
	if !called {
		t.Fatal("invoke not forwarded")
	}
	runtime.GC()
	certificateEventRoots.Lock()
	_, rooted := certificateEventRoots.active[h]
	certificateEventRoots.Unlock()
	if !rooted {
		t.Fatal("native callback not rooted")
	}
	h.release()
	certificateEventRoots.Lock()
	_, rooted = certificateEventRoots.active[h]
	certificateEventRoots.Unlock()
	if rooted {
		t.Fatal("final release leaked root")
	}
}

func TestCertificateRegistrationCancelFirstAndCleanup(t *testing.T) {
	for _, mode := range []string{"deny", "allow", "unreadable"} {
		decide := mode == "allow"
		t.Run(mode, func(t *testing.T) {
			var certHandler, startHandler *certificateEventHandler
			var removedCert, removedStart, viewReleased, certificateReleased int
			var actions []uintptr
			view := &certificateWebView{vtbl: &certificateWebViewVtbl{}}
			view.vtbl.Inherited[2] = NewComProc(func(this uintptr) uintptr { viewReleased++; return 0 })
			view.vtbl.AddServerCertificateErrorDetected = NewComProc(func(this, handler, token uintptr) uintptr {
				certHandler = (*certificateEventHandler)(unsafe.Pointer(handler))
				certHandler.addRef()
				(*_EventRegistrationToken)(unsafe.Pointer(token)).Value = 73
				return 0
			})
			view.vtbl.RemoveServerCertificateErrorDetected = NewComProc(func(this, token uintptr) uintptr {
				if token != 73 {
					t.Error("wrong cert event token", token)
				}
				removedCert++
				certHandler.release()
				return 0
			})
			wv := &ICoreWebView2{vtbl: &iCoreWebView2Vtbl{}}
			wv.vtbl.QueryInterface = NewComProc(func(this, iid, out uintptr) uintptr {
				if *(*GUID)(unsafe.Pointer(iid)) != *NewGUID("{6DAA4F10-4A90-4753-8898-77C5DF534165}") {
					return 0x80004002
				}
				*(**certificateWebView)(unsafe.Pointer(out)) = view
				return 0
			})
			wv.vtbl.AddNavigationStarting = NewComProc(func(this, handler, token uintptr) uintptr {
				startHandler = (*certificateEventHandler)(unsafe.Pointer(handler))
				startHandler.addRef()
				(*_EventRegistrationToken)(unsafe.Pointer(token)).Value = 91
				return 0
			})
			wv.vtbl.RemoveNavigationStarting = NewComProc(func(this, token uintptr) uintptr {
				if token != 91 {
					t.Error("wrong navigation token", token)
				}
				removedStart++
				startHandler.release()
				return 0
			})
			cert := &serverCertificate{vtbl: &serverCertificateVtbl{}}
			cert.vtbl.Release = NewComProc(func(this uintptr) uintptr { certificateReleased++; return 0 })
			cert.vtbl.ToPEMEncoding, _ = comStringProc("certificate-PEM")
			args := &certificateErrorArgs{vtbl: &certificateErrorArgsVtbl{}}
			args.vtbl.GetErrorStatus = NewComProc(func(this, out uintptr) uintptr { *(*int32)(unsafe.Pointer(out)) = 5; return 0 })
			if mode == "unreadable" {
				args.vtbl.GetErrorStatus = NewComProc(func(this, out uintptr) uintptr { return 0x80004005 })
			}
			args.vtbl.GetRequestURI, _ = comStringProc("https://flint.local:8443/")
			args.vtbl.GetServerCertificate = NewComProc(func(this, out uintptr) uintptr { *(**serverCertificate)(unsafe.Pointer(out)) = cert; return 0 })
			args.vtbl.PutAction = NewComProc(func(this, action uintptr) uintptr { actions = append(actions, action); return 0 })
			edge := &Chromium{webview: wv}
			closeEvents, err := edge.EnableCertificateErrors(nil, func(info CertificateError) bool {
				if mode == "unreadable" {
					t.Fatal("decision called for unreadable event")
				}
				if len(actions) != 1 || actions[0] != 1 {
					t.Fatal("decision made before cancel")
				}
				if info.URI != "https://flint.local:8443/" || info.PEM != "certificate-PEM" || info.Status != 5 {
					t.Fatal("wrong details", info)
				}
				return decide
			})
			if err != nil {
				t.Fatal(err)
			}
			certificateEventInvoke(certHandler, 0, uintptr(unsafe.Pointer(args)))
			if mode == "unreadable" {
				if certificateReleased != 0 {
					t.Fatal("unfetched certificate released")
				}
			} else if certificateReleased != 1 {
				t.Fatal("owned certificate not released")
			}
			if decide {
				if len(actions) != 2 || actions[1] != 0 {
					t.Fatal("approved event not allowed", actions)
				}
			} else if len(actions) != 1 {
				t.Fatal("unapproved event allowed", actions)
			}
			closeEvents()
			closeEvents()
			if removedCert != 1 || removedStart != 1 || viewReleased != 1 {
				t.Fatal("cleanup mismatch")
			}
			runtime.KeepAlive(args)
			runtime.KeepAlive(cert)
			runtime.KeepAlive(view)
			runtime.KeepAlive(wv)
		})
	}
}
