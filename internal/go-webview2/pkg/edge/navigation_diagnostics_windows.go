//go:build windows

package edge

import (
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// NetworkFailure is copied from a top-level Document failure, never from page JS.
// No request/response headers, bodies, certificates or credentials are collected.
type NetworkFailure struct {
	URL       string
	ErrorText string
	Canceled  bool
}

type devToolsEventReceiverVtbl struct {
	_IUnknownVtbl
	AddEvent    ComProc
	RemoveEvent ComProc
}
type devToolsEventReceiver struct{ vtbl *devToolsEventReceiverVtbl }
type devToolsEventArgs struct {
	vtbl *struct {
		_IUnknownVtbl
		GetJSON ComProc
	}
}

func devToolsCompleted(h *certificateEventHandler, status uintptr, result *uint16) uintptr {
	// result is borrowed only until this callback returns.
	if h.complete != nil {
		var text string
		if result != nil {
			text = windows.UTF16PtrToString(result)
		}
		h.complete(int32(status), text)
	}
	return 0
}

var devToolsCompletedMethods = certificateEventVtbl{
	_IUnknownVtbl{NewComProc(certificateEventQuery), NewComProc(certificateEventAddRef), NewComProc(certificateEventRelease)},
	NewComProc(devToolsCompleted),
}

func (e *Chromium) callDevTools(method, parameters string, done func(int32, string)) error {
	name, err := windows.UTF16PtrFromString(method)
	if err != nil {
		return err
	}
	params, err := windows.UTF16PtrFromString(parameters)
	if err != nil {
		return err
	}
	h := newCertificateEvent("{5C4889F0-5EF6-4C5A-952C-D8F1B92D0574}", nil)
	h.vtbl = &devToolsCompletedMethods
	h.complete = done
	r, _, _ := e.webview.vtbl.CallDevToolsProtocolMethod.Call(uintptr(unsafe.Pointer(e.webview)), uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(params)), uintptr(unsafe.Pointer(h)))
	h.release()
	runtime.KeepAlive(name)
	runtime.KeepAlive(params)
	if int32(r) < 0 {
		return fmt.Errorf("%s registration failed: HRESULT 0x%08X", method, uint32(r))
	}
	return nil
}

// EnableNavigationDiagnostics captures Chromium's actual network error text.
// All callbacks and cleanup are confined to the WebView's owning UI thread.
func (e *Chromium) EnableNavigationDiagnostics(started func(string), failed func(NetworkFailure), problem func(error)) (func(), error) {
	if e.webview == nil {
		return nil, fmt.Errorf("WebView2 unavailable")
	}
	type request struct {
		url, frame string
		failure    *NetworkFailure
	}
	requests := map[string]request{}
	mainFrame := ""
	closed := false
	reportProblem := func(err error) {
		if !closed && problem != nil {
			problem(err)
		}
	}
	flush := func() {
		if closed || mainFrame == "" {
			return
		}
		for id, req := range requests {
			if req.failure != nil {
				delete(requests, id)
				if req.frame == mainFrame && failed != nil {
					failed(*req.failure)
				}
			}
		}
	}
	var remove []func()
	closeAll := func() {
		if closed {
			return
		}
		closed = true
		for i := len(remove) - 1; i >= 0; i-- {
			remove[i]()
		}
		clear(requests)
	}
	startHandler := newCertificateEvent("{9ADBE429-F36D-432B-9DDC-F8881FBD76E3}", func(raw uintptr) {
		if closed {
			return
		}
		args := (*struct {
			vtbl *struct {
				_IUnknownVtbl
				GetURI ComProc
			}
		})(unsafe.Pointer(raw))
		clear(requests) // discard failures from a superseded navigation/retry
		uri, err := comString(args.vtbl.GetURI, raw)
		if err != nil {
			reportProblem(fmt.Errorf("NavigationStarting: %w", err))
			uri = ""
		}
		if started != nil {
			started(uri)
		}
	})
	var startToken _EventRegistrationToken
	r, _, _ := e.webview.vtbl.AddNavigationStarting.Call(uintptr(unsafe.Pointer(e.webview)), uintptr(unsafe.Pointer(startHandler)), uintptr(unsafe.Pointer(&startToken)))
	startHandler.release()
	if int32(r) < 0 {
		return nil, fmt.Errorf("NavigationStarting registration: HRESULT 0x%08X", uint32(r))
	}
	remove = append(remove, func() {
		e.webview.vtbl.RemoveNavigationStarting.Call(uintptr(unsafe.Pointer(e.webview)), uintptr(startToken.Value))
	})
	listen := func(event string, consume func(string)) error {
		name, _ := windows.UTF16PtrFromString(event)
		var receiver *devToolsEventReceiver
		r, _, _ := e.webview.vtbl.GetDevToolsProtocolEventReceiver.Call(uintptr(unsafe.Pointer(e.webview)), uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&receiver)))
		runtime.KeepAlive(name)
		if int32(r) < 0 || receiver == nil {
			return fmt.Errorf("%s receiver: HRESULT 0x%08X", event, uint32(r))
		}
		h := newCertificateEvent("{E2FDA4BE-5456-406C-A261-3D452138362C}", func(raw uintptr) {
			if closed {
				return
			}
			args := (*devToolsEventArgs)(unsafe.Pointer(raw))
			text, err := comString(args.vtbl.GetJSON, raw)
			if err != nil {
				reportProblem(fmt.Errorf("%s payload: %w", event, err))
				return
			}
			consume(text)
		})
		var token _EventRegistrationToken
		r, _, _ = receiver.vtbl.AddEvent.Call(uintptr(unsafe.Pointer(receiver)), uintptr(unsafe.Pointer(h)), uintptr(unsafe.Pointer(&token)))
		h.release()
		if int32(r) < 0 {
			receiver.vtbl.Release.Call(uintptr(unsafe.Pointer(receiver)))
			return fmt.Errorf("%s event registration: HRESULT 0x%08X", event, uint32(r))
		}
		remove = append(remove, func() {
			receiver.vtbl.RemoveEvent.Call(uintptr(unsafe.Pointer(receiver)), uintptr(token.Value))
			receiver.vtbl.Release.Call(uintptr(unsafe.Pointer(receiver)))
		})
		return nil
	}
	if err := listen("Network.requestWillBeSent", func(raw string) {
		var event struct {
			RequestID string `json:"requestId"`
			FrameID   string `json:"frameId"`
			Type      string `json:"type"`
			Request   struct {
				URL string `json:"url"`
			} `json:"request"`
		}
		if json.Unmarshal([]byte(raw), &event) != nil || event.Type != "Document" || event.RequestID == "" {
			return
		}
		if mainFrame != "" && event.FrameID != mainFrame {
			return
		}
		if len(requests) >= 64 {
			clear(requests)
		}
		requests[event.RequestID] = request{url: event.Request.URL, frame: event.FrameID}
	}); err != nil {
		closeAll()
		return nil, err
	}
	if err := listen("Network.loadingFinished", func(raw string) {
		var event struct {
			RequestID string `json:"requestId"`
		}
		if json.Unmarshal([]byte(raw), &event) == nil {
			delete(requests, event.RequestID)
		}
	}); err != nil {
		closeAll()
		return nil, err
	}
	if err := listen("Network.loadingFailed", func(raw string) {
		var event struct {
			RequestID string `json:"requestId"`
			ErrorText string `json:"errorText"`
			Canceled  bool   `json:"canceled"`
		}
		if json.Unmarshal([]byte(raw), &event) != nil {
			return
		}
		req, ok := requests[event.RequestID]
		if !ok {
			return
		}
		req.failure = &NetworkFailure{URL: req.url, ErrorText: event.ErrorText, Canceled: event.Canceled}
		requests[event.RequestID] = req
		flush()
	}); err != nil {
		closeAll()
		return nil, err
	}
	if err := e.callDevTools("Page.getFrameTree", "{}", func(status int32, raw string) {
		if closed {
			return
		}
		if status < 0 {
			reportProblem(fmt.Errorf("Page.getFrameTree: HRESULT 0x%08X", uint32(status)))
			return
		}
		var tree struct {
			FrameTree struct {
				Frame struct {
					ID string `json:"id"`
				} `json:"frame"`
			} `json:"frameTree"`
		}
		if err := json.Unmarshal([]byte(raw), &tree); err != nil || tree.FrameTree.Frame.ID == "" {
			reportProblem(fmt.Errorf("Page.getFrameTree returned no main frame"))
			return
		}
		mainFrame = tree.FrameTree.Frame.ID
		flush()
	}); err != nil {
		closeAll()
		return nil, err
	}
	if err := e.callDevTools("Network.enable", "{}", func(status int32, _ string) {
		if status < 0 {
			reportProblem(fmt.Errorf("Network.enable: HRESULT 0x%08X", uint32(status)))
		}
	}); err != nil {
		closeAll()
		return nil, err
	}
	var once sync.Once
	return func() { once.Do(closeAll) }, nil
}
